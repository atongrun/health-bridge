// Package query implements the read-only health query surface (S06a): fixed
// scopes, grant-bounded metrics/date ranges, versioned cursors and explicit
// quality. It runs on a read-only SQLite connection; upload credentials are
// rejected by the handler. No history-snapshot traversal is exposed.
package query

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	ScopeSummary  = "health:summary"
	ScopeSleep    = "health:sleep"
	ScopeWorkouts = "health:workouts"
	ScopeRaw      = "health:raw"

	MaxLimit     = 1000
	DefaultLimit = 100
	CursorTTL    = 15 * time.Minute
)

var (
	ErrForbidden     = errors.New("grant exceeds query scope")
	ErrCursorStale   = errors.New("cursor stale")
	ErrQueriesPaused = errors.New("queries paused")
)

// Grant bounds what a query credential may read.
type Grant struct {
	Scopes  []string `json:"scopes"`
	Metrics []string `json:"metrics"`
	MaxDays int      `json:"max_days"`
}

// ParseGrant decodes the credential's scopes_json into a Grant.
func ParseGrant(scopesJSON string) (Grant, error) {
	var g Grant
	if err := json.Unmarshal([]byte(scopesJSON), &g); err != nil {
		return Grant{}, fmt.Errorf("parse grant: %w", err)
	}
	if len(g.Scopes) == 0 {
		return Grant{}, fmt.Errorf("%w: grant has no scopes", ErrForbidden)
	}
	if g.MaxDays <= 0 {
		g.MaxDays = 366
	}
	return g, nil
}

func (g Grant) hasScope(scope string) bool {
	for _, s := range g.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (g Grant) metricAllowed(metric string) bool {
	if len(g.Metrics) == 0 {
		return true
	}
	for _, m := range g.Metrics {
		if m == metric {
			return true
		}
	}
	return false
}

// Service answers read-only queries.
type Service struct {
	DB     *sql.DB // must be a read-only connection
	Secret string  // cursor HMAC secret
}

type revisionPair struct{ observation, projection int64 }

func (s *Service) revisions() (revisionPair, error) {
	var r revisionPair
	err := s.DB.QueryRow("SELECT observation_revision, projection_revision FROM instance_state WHERE singleton=1").
		Scan(&r.observation, &r.projection)
	return r, err
}

func (s *Service) queriesPaused() (bool, error) {
	var paused int
	if err := s.DB.QueryRow("SELECT queries_paused FROM instance_state WHERE singleton=1").Scan(&paused); err != nil {
		return false, err
	}
	return paused == 1, nil
}

func (s *Service) timezone() (string, error) {
	var tz string
	err := s.DB.QueryRow("SELECT report_timezone FROM instance_state WHERE singleton=1").Scan(&tz)
	return tz, err
}

// checkGrant enforces scope, metric whitelist and the day-span bound.
func checkGrant(g Grant, scope string, metrics []string, start, end string) error {
	if !g.hasScope(scope) {
		return fmt.Errorf("%w: token lacks scope %s", ErrForbidden, scope)
	}
	for _, m := range metrics {
		if !g.metricAllowed(m) {
			return fmt.Errorf("%w: metric %s not granted", ErrForbidden, m)
		}
	}
	sd, err := time.Parse("2006-01-02", start)
	if err != nil {
		return fmt.Errorf("%w: start_date %q", ErrForbidden, start)
	}
	ed, err := time.Parse("2006-01-02", end)
	if err != nil {
		return fmt.Errorf("%w: end_date %q", ErrForbidden, end)
	}
	if !ed.After(sd) {
		return fmt.Errorf("%w: end_date must be after start_date", ErrForbidden)
	}
	if int(ed.Sub(sd).Hours()/24) > g.MaxDays {
		return fmt.Errorf("%w: range exceeds max_days %d", ErrForbidden, g.MaxDays)
	}
	return nil
}

// Cursor binds pagination state to the revisions it was computed against.
type Cursor struct {
	Scope       string   `json:"s"`
	Metrics     []string `json:"m"`
	Start       string   `json:"a"`
	End         string   `json:"b"` // exclusive
	LastKey     string   `json:"k"`
	Limit       int      `json:"l"`
	Observation int64    `json:"o"`
	Projection  int64    `json:"p"`
	ExpiresAt   int64    `json:"e"`
}

func (s *Service) signCursor(c Cursor) string {
	b, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, []byte(s.Secret))
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) parseCursor(token string) (Cursor, error) {
	var c Cursor
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return c, ErrCursorStale
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return c, ErrCursorStale
	}
	mac := hmac.New(sha256.New, []byte(s.Secret))
	mac.Write(raw)
	want, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(mac.Sum(nil), want) {
		return c, ErrCursorStale
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, ErrCursorStale
	}
	if time.Now().UnixMicro() > c.ExpiresAt {
		return c, ErrCursorStale
	}
	r, err := s.revisions()
	if err != nil {
		return c, err
	}
	if c.Observation != r.observation || c.Projection != r.projection {
		return c, ErrCursorStale
	}
	return c, nil
}

// Daily is one daily_metrics result row (DTO; no source_json, no profile leak).
type Daily struct {
	Day     string         `json:"day"`
	Metric  string         `json:"metric"`
	Unit    *string        `json:"unit"`
	Min     *float64       `json:"min"`
	Mean    *float64       `json:"mean"`
	Max     *float64       `json:"max"`
	Sum     *float64       `json:"sum"`
	Latest  *string        `json:"latest"`
	N       int            `json:"n"`
	Quality map[string]any `json:"quality"`
}

// Sample is one raw quantity record (DTO; no source_label/native_id/source_json).
type Sample struct {
	Start         string  `json:"start"`
	End           *string `json:"end"`
	TimePrecision string  `json:"time_precision"`
	Value         float64 `json:"value"`
	Unit          string  `json:"unit"`
}

// Response is the query envelope.
type Response struct {
	SchemaVersion string         `json:"schema_version"`
	Data          []any          `json:"data"`
	NextCursor    *string        `json:"next_cursor"`
	Truncated     bool           `json:"truncated"`
	Quality       map[string]any `json:"quality"`
	Range         [2]string      `json:"range"`
	Timezone      string         `json:"timezone"`
}

// QueryDaily serves the summary scope: daily rows for allowed metrics in
// [start, end). rows beyond limit produce an HMAC-bound next_cursor.
func (s *Service) QueryDaily(g Grant, metrics []string, start, end string, limit int, cursorToken string, now time.Time) (Response, error) {
	var resp Response
	if paused, err := s.queriesPaused(); err != nil {
		return resp, err
	} else if paused {
		return resp, ErrQueriesPaused
	}
	scope := ScopeSummary
	metrics, start, end, limit, lastKey := metrics, start, end, limit, ""
	if cursorToken != "" {
		cur, err := s.parseCursor(cursorToken)
		if err != nil {
			return resp, err
		}
		if cur.Scope != scope || cur.Start != start || cur.End != end || cur.Limit != limit {
			return resp, ErrCursorStale
		}
		metrics, lastKey = cur.Metrics, cur.LastKey
	}
	if len(metrics) == 0 {
		return resp, fmt.Errorf("%w: metrics whitelist required", ErrForbidden)
	}
	if err := checkGrant(g, scope, metrics, start, end); err != nil {
		return resp, err
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	sort.Strings(metrics)

	args := []any{}
	where := []string{"1=1"}
	if len(metrics) == 1 {
		where = append(where, "metric = ?")
		args = append(args, metrics[0])
	} else {
		where = append(where, "metric IN ("+strings.TrimSuffix(strings.Repeat("?,", len(metrics)), ",")+")")
		for _, m := range metrics {
			args = append(args, m)
		}
	}
	where = append(where, "day >= ?", "day < ?")
	args = append(args, start, end)
	if lastKey != "" {
		where = append(where, "(day, metric, profile_id) > (?)")
		args = append(args, lastKey)
	}
	q := "SELECT day, metric, value_json, quality_json FROM daily_metrics WHERE " +
		strings.Join(where, " AND ") + " ORDER BY day, metric LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return resp, err
	}
	defer rows.Close()
	data := []any{}
	lastKeyOut := ""
	count := 0
	for rows.Next() {
		var day, metric, vj, qj string
		if err := rows.Scan(&day, &metric, &vj, &qj); err != nil {
			return resp, err
		}
		var v struct {
			Unit   *string  `json:"unit"`
			Min    *float64 `json:"min"`
			Mean   *float64 `json:"mean"`
			Max    *float64 `json:"max"`
			Sum    *float64 `json:"sum"`
			Latest *struct {
				Value      float64 `json:"value"`
				MeasuredAt string  `json:"measured_at"`
			} `json:"latest"`
			N int `json:"n"`
		}
		if err := json.Unmarshal([]byte(vj), &v); err != nil {
			return resp, err
		}
		var quality map[string]any
		_ = json.Unmarshal([]byte(qj), &quality)
		var latest *string
		if v.Latest != nil {
			l := v.Latest.MeasuredAt
			latest = &l
		}
		data = append(data, Daily{Day: day, Metric: metric, Unit: v.Unit, Min: v.Min, Mean: v.Mean, Max: v.Max, Sum: v.Sum, Latest: latest, N: v.N, Quality: quality})
		lastKeyOut = day + "," + metric
		count++
		if count == limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return resp, err
	}
	// Close before any further query: this pool has a single connection.
	if err := rows.Close(); err != nil {
		return resp, err
	}
	truncated := count == limit
	tz, err := s.timezone()
	if err != nil {
		return resp, err
	}
	resp = Response{
		SchemaVersion: "1",
		Data:          data,
		Truncated:     truncated,
		Range:         [2]string{start, end},
		Timezone:      tz,
		Quality: map[string]any{
			"status":            "observed_partial",
			"collection_method": "shortcuts_window_observation",
		},
	}
	if truncated {
		r, err := s.revisions()
		if err != nil {
			return resp, err
		}
		nc := Cursor{Scope: scope, Metrics: metrics, Start: start, End: end, LastKey: lastKeyOut, Limit: limit,
			Observation: r.observation, Projection: r.projection, ExpiresAt: now.Add(CursorTTL).UnixMicro()}
		tok := s.signCursor(nc)
		resp.NextCursor = &tok
	}
	return resp, nil
}

// QuerySamples serves the raw scope: quantity records in [start, end) for one
// granted metric, at most maxDays of span, bounded rows.
func (s *Service) QuerySamples(g Grant, metric, start, end string, limit int, now time.Time) (Response, error) {
	var resp Response
	if paused, err := s.queriesPaused(); err != nil {
		return resp, err
	} else if paused {
		return resp, ErrQueriesPaused
	}
	if err := checkGrant(g, ScopeRaw, []string{metric}, start, end); err != nil {
		return resp, err
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	startUs, err := time.Parse("2006-01-02", start)
	if err != nil {
		return resp, ErrForbidden
	}
	endUs, err := time.Parse("2006-01-02", end)
	if err != nil {
		return resp, ErrForbidden
	}
	rows, err := s.DB.Query(`SELECT i.start_us, i.end_us, i.time_precision, i.value, i.unit
		FROM observation_items i
		JOIN observation_snapshots s ON s.id = i.snapshot_id
		JOIN collection_windows w ON w.id = s.job_id
		JOIN collection_profiles p ON p.id = w.profile_id
		WHERE p.metric = ? AND p.enabled = 1 AND i.kind = 'quantity'
		  AND w.current_snapshot_id = s.id
		  AND i.start_us >= ? AND i.start_us < ?
		ORDER BY i.start_us LIMIT ?`,
		metric, startUs.UnixMicro(), endUs.UnixMicro(), limit+1)
	if err != nil {
		return resp, err
	}
	defer rows.Close()
	data := []any{}
	count := 0
	for rows.Next() {
		var precision, unit string
		var value float64
		var startUsVal int64
		var endUsVal sql.NullInt64
		if err := rows.Scan(&startUsVal, &endUsVal, &precision, &value, &unit); err != nil {
			return resp, err
		}
		sm := Sample{TimePrecision: precision, Value: value, Unit: unit, Start: time.UnixMicro(startUsVal).UTC().Format(time.RFC3339)}
		if endUsVal.Valid {
			e := time.UnixMicro(endUsVal.Int64).UTC().Format(time.RFC3339)
			sm.End = &e
		}
		data = append(data, sm)
		count++
		if count == limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return resp, err
	}
	// Close before any further query: this pool has a single connection.
	if err := rows.Close(); err != nil {
		return resp, err
	}
	tz, err := s.timezone()
	if err != nil {
		return resp, err
	}
	resp = Response{
		SchemaVersion: "1",
		Data:          data,
		Truncated:     count == limit,
		Range:         [2]string{start, end},
		Timezone:      tz,
		Quality: map[string]any{
			"status":            "observed_partial",
			"collection_method": "shortcuts_window_observation",
		},
	}
	return resp, nil
}
