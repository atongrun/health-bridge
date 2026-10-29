package query_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/httpapi"
	"github.com/atongrun/health-bridge/server/internal/store"
)

type qenv struct {
	rw        *sql.DB
	ro        *sql.DB
	uploadTok string
	queryTok  string
	srv       *httptest.Server
}

func newQEnv(t *testing.T) *qenv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	rw, err := store.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(rw); err != nil {
		t.Fatal(err)
	}
	ro, err := store.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	epoch, err := auth.ReplacePublisher(rw, "p", now)
	if err != nil {
		t.Fatal(err)
	}
	_, uploadTok, err := auth.CreateCredential(rw, "upload", "{}", "{}", "p", epoch, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	_, queryTok, err := auth.CreateCredential(rw, "query",
		`{"scopes":["health:summary","health:raw"],"metrics":["heart_rate","step_count"],"max_days":30}`,
		"{}", "", "", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", httpapi.NewHandler(rw))
	mux.Handle("/v1/query/", httpapi.NewQueryHandler(ro, "test-cursor-secret"))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close(); ro.Close(); rw.Close() })
	return &qenv{rw: rw, ro: ro, uploadTok: uploadTok, queryTok: queryTok, srv: srv}
}

func (e *qenv) get(t *testing.T, path, token string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func (e *qenv) seedDaily(t *testing.T, day, metric, profile string, min, sum float64) {
	t.Helper()
	vj, _ := json.Marshal(map[string]any{"unit": "count/min", "min": min, "mean": min, "max": min, "sum": nil, "latest": nil, "n": 1})
	qj, _ := json.Marshal(map[string]any{
		"status": "observed_partial", "collection_method": "shortcuts_window_observation",
		"identity_quality": "unverified", "source_quality": "label_only",
		"window_coverage": map[string]int{"observed_windows": 1, "expected_windows": 1},
		"last_observed_at": nil, "last_attempt_at": nil, "method_version": "sc-window-v1",
		"limitations": []string{},
	})
	_, err := e.rw.Exec(`INSERT INTO daily_metrics(day,timezone,metric,profile_id,method_version,value_json,quality_json,observation_revision,computed_at_us)
		VALUES(?, 'UTC', ?, ?, 'sc-window-v1', ?, ?, 1, 0)`, day, metric, profile, string(vj), string(qj))
	if err != nil {
		t.Fatal(err)
	}
}

func TestQueryDailyHappyPath(t *testing.T) {
	e := newQEnv(t)
	e.seedDaily(t, "2026-01-02", "heart_rate", "p1", 60, 0)
	e.seedDaily(t, "2026-01-03", "heart_rate", "p1", 62, 0)
	status, body := e.get(t, "/v1/query/summary?metric=heart_rate&start_date=2026-01-01&end_date=2026-01-04", e.queryTok)
	if status != http.StatusOK {
		t.Fatalf("status = %d body=%v", status, body)
	}
	if body["schema_version"] != "1" {
		t.Fatalf("schema_version = %v", body["schema_version"])
	}
	data := body["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("data rows = %d, want 2", len(data))
	}
	// No source/profile leak in DTO.
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "profile") || strings.Contains(string(raw), "source_json") {
		t.Fatalf("response leaks profile/source fields: %s", raw)
	}
}

func TestUploadTokenCannotQuery(t *testing.T) {
	e := newQEnv(t)
	status, body := e.get(t, "/v1/query/summary?metric=heart_rate&start_date=2026-01-01&end_date=2026-01-04", e.uploadTok)
	if status != http.StatusForbidden {
		t.Fatalf("upload token query status = %d (%v), want 403", status, body)
	}
}

func TestGrantBoundary403(t *testing.T) {
	e := newQEnv(t)
	_, restricted, err := auth.CreateCredential(e.rw, "query",
		`{"scopes":["health:summary"],"metrics":["body_mass"],"max_days":30}`, "{}", "", "", time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Metric not granted.
	status, body := e.get(t, "/v1/query/summary?metric=heart_rate&start_date=2026-01-01&end_date=2026-01-04", restricted)
	if status != http.StatusForbidden {
		t.Fatalf("ungranted metric status = %d (%v), want 403", status, body)
	}
	// Range exceeds max_days.
	status, _ = e.get(t, "/v1/query/summary?metric=body_mass&start_date=2026-01-01&end_date=2026-03-01", restricted)
	if status != http.StatusForbidden {
		t.Fatalf("over-max-days status = %d, want 403", status)
	}
}

func TestCursorPaginationAndStale(t *testing.T) {
	e := newQEnv(t)
	for i := 1; i <= 3; i++ {
		e.seedDaily(t, "2026-01-0"+string(rune('0'+i)), "heart_rate", "p1", float64(60+i), 0)
	}
	status, body := e.get(t, "/v1/query/summary?metric=heart_rate&start_date=2026-01-01&end_date=2026-01-31&limit=2", e.queryTok)
	if status != http.StatusOK {
		t.Fatalf("page1 status = %d", status)
	}
	data := body["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("page1 rows = %d, want 2", len(data))
	}
	cursor := body["next_cursor"].(string)
	status2, body2 := e.get(t, "/v1/query/summary?metric=heart_rate&start_date=2026-01-01&end_date=2026-01-31&limit=2&cursor="+cursor, e.queryTok)
	if status2 != http.StatusOK {
		t.Fatalf("page2 status = %d (%v)", status2, body2)
	}
	if rows := len(body2["data"].([]any)); rows != 1 {
		t.Fatalf("page2 rows = %d, want 1", rows)
	}
	// Mutate revisions -> cursor stale (409).
	if _, err := e.rw.Exec("UPDATE instance_state SET observation_revision = observation_revision + 1 WHERE singleton=1"); err != nil {
		t.Fatal(err)
	}
	status3, body3 := e.get(t, "/v1/query/summary?metric=heart_rate&start_date=2026-01-01&end_date=2026-01-31&limit=2&cursor="+cursor, e.queryTok)
	if status3 != http.StatusConflict {
		t.Fatalf("stale cursor status = %d (%v), want 409", status3, body3)
	}
}

func TestQueriesPaused503(t *testing.T) {
	e := newQEnv(t)
	if _, err := e.rw.Exec("UPDATE instance_state SET queries_paused=1 WHERE singleton=1"); err != nil {
		t.Fatal(err)
	}
	status, _ := e.get(t, "/v1/query/summary?metric=heart_rate&start_date=2026-01-01&end_date=2026-01-04", e.queryTok)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("paused status = %d, want 503", status)
	}
}

func TestReadOnlyConnectionRejectsWrite(t *testing.T) {
	e := newQEnv(t)
	if _, err := e.ro.Exec("UPDATE instance_state SET observation_revision=999 WHERE singleton=1"); err == nil {
		t.Fatal("write on read-only query connection succeeded")
	}
}

func TestSamplesNoSensitiveFields(t *testing.T) {
	e := newQEnv(t)
	// Seed an enabled profile + window + snapshot with a quantity item.
	if _, err := e.rw.Exec(`INSERT INTO collection_profiles(id,version,metric,window_seconds,mode,mapping_json,evidence_ref,verified,enabled)
		VALUES('p_hr',1,'heart_rate',86400,'scheduled','{}','probe://s01',1,1)`); err != nil {
		t.Fatal(err)
	}
	startUs := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC).UnixMicro()
	endUs := startUs + 86400*1e6
	if _, err := e.rw.Exec(`INSERT INTO collection_windows(id,profile_id,start_us,end_us,last_state) VALUES('w1','p_hr',?,?,'observed')`,
		startUs, endUs); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rw.Exec(`INSERT INTO observation_snapshots(id,job_id,content_digest,read_state,record_count,created_at_us)
		VALUES('s1','w1',?,'observed',1,0)`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rw.Exec(`INSERT INTO observation_items(snapshot_id,ordinal,kind,start_us,time_precision,source_label,native_id,value,unit,source_json)
		VALUES('s1',0,'quantity',?,'second','Apple Watch','native-uuid',60,'count/min','{"secret":"x"}')`,
		startUs+3600*1e6); err != nil {
		t.Fatal(err)
	}
	if _, err := e.rw.Exec(`UPDATE collection_windows SET current_snapshot_id='s1' WHERE id='w1'`); err != nil {
		t.Fatal(err)
	}
	status, body := e.get(t, "/v1/query/samples?metric=heart_rate&start_date=2026-01-02&end_date=2026-01-03", e.queryTok)
	if status != http.StatusOK {
		t.Fatalf("samples status = %d (%v)", status, body)
	}
	raw, _ := json.Marshal(body)
	for _, leak := range []string{"source_label", "native_id", "source_json", "Apple Watch", "secret"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("samples response leaks %q: %s", leak, raw)
		}
	}
	data := body["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("samples rows = %d, want 1", len(data))
	}
}
