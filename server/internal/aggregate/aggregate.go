// Package aggregate computes daily projections from the latest successful
// observation per window (S05a). It buckets by IANA local day (23/25h at DST),
// keeps per-record multiplicity, refuses to blind-sum multi-source cumulative
// metrics, and commits only when the observation revision has not moved (CAS).
package aggregate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MethodVersion identifies this projection algorithm (docs/aggregation.md).
const MethodVersion = "sc-window-v1"

// CollectionMethod is the fixed collection method recorded in every quality.
const CollectionMethod = "shortcuts_window_observation"

// ErrRevisionChanged means an observation landed while a day was computing.
var ErrRevisionChanged = errors.New("observation revision changed during aggregation")

type Service struct {
	DB *sql.DB
}

// DayValue is the stored value_json (docs/data-schema.md).
type DayValue struct {
	Unit   *string  `json:"unit"`
	Min    *float64 `json:"min"`
	Mean   *float64 `json:"mean"`
	Max    *float64 `json:"max"`
	Sum    *float64 `json:"sum"`
	Latest *Latest  `json:"latest"`
	N      int      `json:"n"`
}

// Latest is the newest visible record.
type Latest struct {
	Value      float64 `json:"value"`
	MeasuredAt string  `json:"measured_at"`
}

// Coverage is the observed/expected window count for a day.
type Coverage struct {
	ObservedWindows  int `json:"observed_windows"`
	ExpectedWindows  int `json:"expected_windows"`
}

// DayQuality is the stored quality_json.
type DayQuality struct {
	Status           string   `json:"status"`
	CollectionMethod string   `json:"collection_method"`
	IdentityQuality  string   `json:"identity_quality"`
	SourceQuality    string   `json:"source_quality"`
	WindowCoverage   Coverage `json:"window_coverage"`
	LastObservedAt   *string  `json:"last_observed_at"`
	LastAttemptAt    *string  `json:"last_attempt_at"`
	MethodVersion    string   `json:"method_version"`
	Limitations      []string `json:"limitations"`
}

// metricClass decides how a metric aggregates. Sleep/workout are S05b and
// produce no daily row here.
func metricClass(metric string) string {
	switch metric {
	case "step_count", "active_energy", "apple_exercise_time", "swimming_stroke_count",
		"distance_swimming", "distance_walking_running", "distance_cycling",
		"distance_wheelchair", "distance_downhill_snow_sports":
		return "cumulative"
	case "body_mass", "height", "vo2_max":
		return "latest"
	case "sleep", "workout":
		return "skip"
	default:
		return "discrete"
	}
}

// DayBounds returns the [start,end) microsecond bounds of an IANA day; DST
// makes these 23h or 25h, never a fixed 86400s.
func DayBounds(day, tz string) (int64, int64, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return 0, 0, fmt.Errorf("load location %q: %w", tz, err)
	}
	start, err := time.ParseInLocation("2006-01-02", day, loc)
	if err != nil {
		return 0, 0, fmt.Errorf("parse day %q: %w", day, err)
	}
	end := start.AddDate(0, 0, 1)
	return start.UnixMicro(), end.UnixMicro(), nil
}

type window struct {
	id              string
	profileID       string
	metric          string
	startUs         int64
	endUs           int64
	lastState       string
	currentSnapshot sql.NullString
	lastAttempt     sql.NullInt64
	lastCommitted   sql.NullInt64
}

type exclusion struct {
	metric  string
	startUs int64
	endUs   int64
}

// RecomputeDirty processes every dirty day with a fresh revision read and a
// bounded retry on CAS conflict.
func (s *Service) RecomputeDirty(now time.Time) (int, error) {
	type dirty struct{ day, tz string }
	rows, err := s.DB.Query("SELECT day, timezone FROM dirty_days ORDER BY day, timezone")
	if err != nil {
		return 0, err
	}
	var days []dirty
	for rows.Next() {
		var d dirty
		if err := rows.Scan(&d.day, &d.tz); err != nil {
			rows.Close()
			return 0, err
		}
		days = append(days, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	processed := 0
	for _, d := range days {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			rev, e := s.currentRevision()
			if e != nil {
				return processed, e
			}
			err = s.recomputeDay(d.day, d.tz, rev, now)
			if !errors.Is(err, ErrRevisionChanged) {
				break
			}
		}
		if err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (s *Service) currentRevision() (int64, error) {
	var rev int64
	if err := s.DB.QueryRow("SELECT observation_revision FROM instance_state WHERE singleton=1").Scan(&rev); err != nil {
		return 0, err
	}
	return rev, nil
}

// recomputeDay reads snapshots, computes one daily_metrics row per enabled
// profile with involved windows, and commits under a revision CAS.
func (s *Service) recomputeDay(day, tz string, revRead int64, now time.Time) error {
	startUs, endUs, err := DayBounds(day, tz)
	if err != nil {
		return err
	}
	windows, err := s.loadWindows(startUs, endUs)
	if err != nil {
		return err
	}
	excls, err := s.loadExclusions(startUs, endUs)
	if err != nil {
		return err
	}
	byProfile := map[string][]window{}
	for _, w := range windows {
		if excluded(w, excls) {
			continue
		}
		byProfile[w.profileID] = append(byProfile[w.profileID], w)
	}

	type row struct {
		metric  string
		profile string
		value   DayValue
		quality DayQuality
	}
	var rows []row
	for profileID, ws := range byProfile {
		if len(ws) == 0 {
			continue
		}
		class := metricClass(ws[0].metric)
		if class == "skip" {
			continue
		}
		v, q, err := s.computeProfile(ws, class, now)
		if err != nil {
			return err
		}
		rows = append(rows, row{metric: ws[0].metric, profile: profileID, value: v, quality: q})
	}

	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var rev int64
	if err := tx.QueryRow("SELECT observation_revision FROM instance_state WHERE singleton=1").Scan(&rev); err != nil {
		return err
	}
	if rev != revRead {
		return ErrRevisionChanged
	}
	for _, r := range rows {
		vj, _ := json.Marshal(r.value)
		qj, _ := json.Marshal(r.quality)
		if _, err := tx.Exec(`INSERT OR REPLACE INTO daily_metrics(day,timezone,metric,profile_id,method_version,value_json,quality_json,observation_revision,computed_at_us)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			day, tz, r.metric, r.profile, MethodVersion, string(vj), string(qj), revRead, now.UnixMicro()); err != nil {
			return err
		}
	}
	if _, err := tx.Exec("DELETE FROM dirty_days WHERE day=? AND timezone=?", day, tz); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE instance_state SET projection_revision = projection_revision + 1 WHERE singleton=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) loadWindows(startUs, endUs int64) ([]window, error) {
	rows, err := s.DB.Query(`SELECT w.id, w.start_us, w.end_us, w.last_state, w.current_snapshot_id, w.last_attempt_at_us, w.last_committed_at_us,
		p.id, p.metric FROM collection_windows w JOIN collection_profiles p ON p.id = w.profile_id
		WHERE p.enabled = 1 AND w.start_us < ? AND w.end_us > ?`, endUs, startUs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []window
	for rows.Next() {
		var w window
		if err := rows.Scan(&w.id, &w.startUs, &w.endUs, &w.lastState, &w.currentSnapshot, &w.lastAttempt, &w.lastCommitted, &w.profileID, &w.metric); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Service) loadExclusions(startUs, endUs int64) ([]exclusion, error) {
	rows, err := s.DB.Query("SELECT metric, start_us, end_us FROM owner_exclusions WHERE start_us < ? AND end_us > ?", endUs, startUs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []exclusion
	for rows.Next() {
		var e exclusion
		if err := rows.Scan(&e.metric, &e.startUs, &e.endUs); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func excluded(w window, excls []exclusion) bool {
	for _, e := range excls {
		if e.metric == w.metric && e.startUs < w.endUs && e.endUs > w.startUs {
			return true
		}
	}
	return false
}

type sample struct {
	value  float64
	unit   string
	start  int64
	source sql.NullString
	native sql.NullString
}

func (s *Service) computeProfile(ws []window, class string, now time.Time) (DayValue, DayQuality, error) {
	expected := len(ws)
	observed := 0
	stale := 0
	var samples []sample
	var lastCommitted, lastAttempt int64
	allObservedEmpty := true
	for _, w := range ws {
		if w.lastCommitted.Valid && w.lastCommitted.Int64 > lastCommitted {
			lastCommitted = w.lastCommitted.Int64
		}
		if w.lastAttempt.Valid && w.lastAttempt.Int64 > lastAttempt {
			lastAttempt = w.lastAttempt.Int64
		}
		if w.currentSnapshot.Valid && (w.lastState == "observed" || w.lastState == "no_visible_data") {
			observed++
			snapshotSamples, readState, err := s.loadSamples(w.currentSnapshot.String)
			if err != nil {
				return DayValue{}, DayQuality{}, err
			}
			if readState != "no_visible_data" {
				allObservedEmpty = false
			}
			samples = append(samples, snapshotSamples...)
		} else if w.currentSnapshot.Valid {
			stale++ // a snapshot exists but the latest read failed/overflowed
		}
	}

	value, limitations := aggregate(class, samples)
	unit := unitOf(samples)

	q := DayQuality{
		Status:           status(expected, observed, stale, len(samples), allObservedEmpty),
		CollectionMethod: CollectionMethod,
		IdentityQuality:  identityQuality(samples),
		SourceQuality:    sourceQuality(samples),
		WindowCoverage:   Coverage{ObservedWindows: observed, ExpectedWindows: expected},
		MethodVersion:    MethodVersion,
		Limitations:      []string{"shortcuts_window_observation_is_not_full_healthkit_coverage"},
	}
	if observed < expected {
		q.Limitations = append(q.Limitations, "incomplete_window_coverage")
	}
	if stale > 0 {
		q.Limitations = append(q.Limitations, "stale_windows_present")
	}
	if lastCommitted != 0 {
		t := formatUs(lastCommitted)
		q.LastObservedAt = &t
	}
	if lastAttempt != 0 {
		t := formatUs(lastAttempt)
		q.LastAttemptAt = &t
	}
	q.Limitations = append(q.Limitations, limitations...)

	value.Unit = unit
	return value, q, nil
}

func (s *Service) loadSamples(snapshotID string) ([]sample, string, error) {
	rows, err := s.DB.Query(`SELECT s.read_state, i.value, i.unit, i.start_us, i.source_label, i.native_id
		FROM observation_snapshots s LEFT JOIN observation_items i ON i.snapshot_id = s.id AND i.kind = 'quantity'
		WHERE s.id = ? ORDER BY i.ordinal`, snapshotID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []sample
	readState := ""
	for rows.Next() {
		var rs string
		var value sql.NullFloat64
		var unit sql.NullString
		var startUs sql.NullInt64
		var src, native sql.NullString
		if err := rows.Scan(&rs, &value, &unit, &startUs, &src, &native); err != nil {
			return nil, "", err
		}
		readState = rs
		if value.Valid {
			out = append(out, sample{value: value.Float64, unit: unit.String, start: startUs.Int64, source: src, native: native})
		}
	}
	return out, readState, rows.Err()
}

// aggregate computes value_json fields and extra limitations by metric class.
func aggregate(class string, samples []sample) (DayValue, []string) {
	v := DayValue{N: len(samples)}
	var limits []string
	if len(samples) == 0 {
		return v, limits
	}
	switch class {
	case "discrete":
		min, max := samples[0].value, samples[0].value
		var sum float64
		latest := samples[0]
		for _, sm := range samples {
			if sm.value < min {
				min = sm.value
			}
			if sm.value > max {
				max = sm.value
			}
			if sm.start > latest.start {
				latest = sm
			}
			sum += sm.value
		}
		mean := sum / float64(len(samples))
		v.Min, v.Mean, v.Max = &min, &mean, &max
		v.Latest = &Latest{Value: latest.value, MeasuredAt: formatUs(latest.start)}
	case "latest":
		latest := samples[0]
		ambiguous := false
		for _, sm := range samples[1:] {
			if sm.start > latest.start {
				latest = sm
				ambiguous = false
			} else if sm.start == latest.start && sm.value != latest.value {
				ambiguous = true
			}
		}
		v.Latest = &Latest{Value: latest.value, MeasuredAt: formatUs(latest.start)}
		if ambiguous {
			limits = append(limits, "ambiguous_latest_value")
		}
	case "cumulative":
		var daySum float64
		for _, sm := range samples {
			daySum += sm.value
		}
		if sourceQuality(samples) == "unknown" {
			limits = append(limits, "multi_source_or_unknown_source_sum_null")
		} else {
			v.Sum = &daySum
			limits = append(limits, "observed_sum_not_official_total")
		}
	}
	return v, limits
}

func unitOf(samples []sample) *string {
	if len(samples) == 0 {
		return nil
	}
	u := samples[0].unit
	return &u
}

func status(expected, observed, stale, n int, allObservedEmpty bool) string {
	switch {
	case expected == 0:
		return "pending"
	case observed == 0 && stale == 0:
		return "pending"
	case observed == 0 && stale > 0:
		return "stale"
	case stale > 0:
		return "stale"
	case n == 0 && allObservedEmpty:
		return "no_visible_data"
	default:
		return "observed_partial"
	}
}

func sourceQuality(samples []sample) string {
	if len(samples) == 0 || !samples[0].source.Valid {
		return "unknown"
	}
	label := samples[0].source
	for _, sm := range samples[1:] {
		if !sm.source.Valid || sm.source != label {
			return "unknown"
		}
	}
	return "label_only"
}

func identityQuality(samples []sample) string {
	if len(samples) == 0 {
		return "unverified"
	}
	for _, sm := range samples {
		if !sm.native.Valid {
			return "unverified"
		}
	}
	return "native_id_verified"
}

func formatUs(us int64) string {
	return time.UnixMicro(us).UTC().Format(time.RFC3339)
}
