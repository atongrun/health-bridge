package aggregate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/collection"
	"github.com/atongrun/health-bridge/server/internal/store"
)

func migrate(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustUs(t *testing.T, s string) int64 {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts.UnixMicro()
}

func insertProfile(t *testing.T, db *sql.DB, id, metric string, windowSeconds int, enabled bool) {
	t.Helper()
	mode := "scheduled"
	if windowSeconds == 900 {
		mode = "on_demand"
	}
	_, err := db.Exec(`INSERT INTO collection_profiles(id,version,metric,window_seconds,mode,mapping_json,evidence_ref,verified,enabled)
		VALUES(?,1,?,?,?,'{}','probe://s01',1,?)`, id, metric, windowSeconds, mode, enabled)
	if err != nil {
		t.Fatal(err)
	}
}

func insertWindow(t *testing.T, db *sql.DB, id, profileID string, startUs, endUs int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO collection_windows(id,profile_id,start_us,end_us,last_state) VALUES(?,?,?,?,'pending')`,
		id, profileID, startUs, endUs); err != nil {
		t.Fatal(err)
	}
}

func markObserved(t *testing.T, db *sql.DB, windowID string, readState string, items []gItem, committedAt, attemptAt string) {
	t.Helper()
	snapshotID := store.NewUUID()
	committedUs := mustUs(t, committedAt)
	attemptUs := mustUs(t, attemptAt)
	if _, err := db.Exec(`INSERT INTO observation_snapshots(id,job_id,content_digest,read_state,record_count,created_at_us)
		VALUES(?,?,?,?,?,?)`, snapshotID, windowID, strings.Repeat("a", 64), readState, len(items), committedUs); err != nil {
		t.Fatal(err)
	}
	for i, it := range items {
		var src, native any
		if it.Source != "" {
			src = it.Source
		}
		if it.Native != "" {
			native = it.Native
		}
		if _, err := db.Exec(`INSERT INTO observation_items(snapshot_id,ordinal,kind,start_us,time_precision,source_label,native_id,value,unit,source_json)
			VALUES(?,?,'quantity',?,'second',?,?,?,?,'{}')`,
			snapshotID, i, mustUs(t, it.Start), src, native, it.Value, it.Unit); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE collection_windows SET current_snapshot_id=?, last_state=?, last_committed_at_us=?, last_attempt_at_us=? WHERE id=?`,
		snapshotID, readState, committedUs, attemptUs, windowID); err != nil {
		t.Fatal(err)
	}
}

// --- golden fixtures ---

type gProfile struct {
	ID            string `json:"id"`
	Metric        string `json:"metric"`
	WindowSeconds int    `json:"window_seconds"`
	Enabled       bool   `json:"enabled"`
}

type gItem struct {
	Start  string  `json:"start"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Source string  `json:"source"`
	Native string  `json:"native"`
}

type gWindow struct {
	Profile     string  `json:"profile"`
	Start       string  `json:"start"`
	ReadState   string  `json:"read_state"`
	CommittedAt string  `json:"committed_at"`
	AttemptAt   string  `json:"attempt_at"`
	Items       []gItem `json:"items"`
}

type gExpect struct {
	Metric    string         `json:"metric"`
	ProfileID string         `json:"profile_id"`
	Value     map[string]any `json:"value"`
	Quality   map[string]any `json:"quality"`
}

type golden struct {
	Name      string      `json:"name"`
	Day       string      `json:"day"`
	Timezone  string      `json:"timezone"`
	Profiles  []gProfile  `json:"profiles"`
	Windows   []gWindow   `json:"windows"`
	Expect    []gExpect   `json:"expect"`
}

func loadGolden(t *testing.T, name string) golden {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func seedGolden(t *testing.T, db *sql.DB, g golden) {
	t.Helper()
	profiles := map[string]gProfile{}
	for _, p := range g.Profiles {
		profiles[p.ID] = p
		insertProfile(t, db, p.ID, p.Metric, p.WindowSeconds, p.Enabled)
	}
	for _, w := range g.Windows {
		p := profiles[w.Profile]
		winID := store.NewUUID()
		startUs := mustUs(t, w.Start)
		endUs := startUs + int64(p.WindowSeconds)*1e6
		insertWindow(t, db, winID, p.ID, startUs, endUs)
		switch w.ReadState {
		case "observed", "no_visible_data":
			committed := w.CommittedAt
			if committed == "" {
				committed = "2026-01-02T12:00:00Z"
			}
			attempt := w.AttemptAt
			if attempt == "" {
				attempt = committed
			}
			markObserved(t, db, winID, w.ReadState, w.Items, committed, attempt)
		}
	}
	if _, err := db.Exec("INSERT INTO dirty_days(day,timezone,observation_revision) VALUES(?,?,0)", g.Day, g.Timezone); err != nil {
		t.Fatal(err)
	}
}

func assertGolden(t *testing.T, db *sql.DB, g golden) {
	t.Helper()
	svc := &Service{DB: db}
	if _, err := svc.RecomputeDirty(time.Now()); err != nil {
		t.Fatalf("recompute: %v", err)
	}
	rows, err := db.Query(`SELECT metric, profile_id, value_json, quality_json FROM daily_metrics WHERE day=? AND timezone=?`,
		g.Day, g.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]map[string]any{}
	for rows.Next() {
		var metric, profile, vj, qj string
		if err := rows.Scan(&metric, &profile, &vj, &qj); err != nil {
			t.Fatal(err)
		}
		var v, q map[string]any
		_ = json.Unmarshal([]byte(vj), &v)
		_ = json.Unmarshal([]byte(qj), &q)
		got[metric+"|"+profile] = map[string]any{"value": v, "quality": q}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(g.Expect) {
		t.Fatalf("rows=%d expect=%d", len(got), len(g.Expect))
	}
	for _, e := range g.Expect {
		key := e.Metric + "|" + e.ProfileID
		entry, ok := got[key]
		if !ok {
			t.Fatalf("missing daily row for %s", key)
		}
		if !reflect.DeepEqual(entry["value"], e.Value) {
			t.Fatalf("%s value mismatch:\n got %v\nwant %v", key, entry["value"], e.Value)
		}
		if !reflect.DeepEqual(entry["quality"], e.Quality) {
			t.Fatalf("%s quality mismatch:\n got %v\nwant %v", key, entry["quality"], e.Quality)
		}
	}
}

func TestGoldenFixtures(t *testing.T) {
	names := []string{
		"discrete_full.json", "cumulative_single_source.json", "cumulative_multi_source.json",
		"missing_window.json", "partial_coverage.json", "percent_unit.json", "empty_window.json",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			g := loadGolden(t, name)
			db := migrate(t)
			seedGolden(t, db, g)
			assertGolden(t, db, g)
		})
	}
}

func TestDayBoundsDST(t *testing.T) {
	cases := []struct {
		day, tz string
		hours   time.Duration
	}{
		{"2026-03-08", "America/New_York", 23 * time.Hour}, // spring forward
		{"2026-11-01", "America/New_York", 25 * time.Hour}, // fall back
		{"2026-01-02", "Asia/Shanghai", 24 * time.Hour},    // no DST
	}
	for _, c := range cases {
		start, end, err := DayBounds(c.day, c.tz)
		if err != nil {
			t.Fatal(err)
		}
		if got := end - start; got != int64(c.hours/time.Microsecond) {
			t.Fatalf("%s %s length = %d us, want %d us", c.day, c.tz, got, int64(c.hours/time.Microsecond))
		}
	}
}

func stepItem(start string, value float64) collection.Item {
	src := "iPhone"
	return collection.Item{
		Start: start, TimePrecision: "second", SourceLabel: &src,
		Quantity: &collection.Quantity{Value: value, Unit: "count"},
	}
}

func TestDuplicateRereadNoDouble(t *testing.T) {
	db := migrate(t)
	now := time.Now()
	epoch, err := auth.ReplacePublisher(db, "p", now)
	if err != nil {
		t.Fatal(err)
	}
	insertProfile(t, db, "p_steps", "step_count", 86400, true)
	svc := &collection.Service{DB: db}

	plan, err := svc.Plan("p", epoch, now, 15*time.Minute, 7)
	if err != nil {
		t.Fatal(err)
	}
	job := plan.Jobs[0]
	start, _ := time.Parse(time.RFC3339, job.Window.Start)
	a := stepItem(start.Add(time.Hour).Format(time.RFC3339), 1000)
	b := stepItem(start.Add(2*time.Hour).Format(time.RFC3339), 2000)

	obs := collection.Observation{
		Protocol: "shortcuts-1", PlanID: plan.PlanID, JobID: job.JobID, AttemptID: job.AttemptID,
		ServerGeneration: plan.ServerGeneration, PublisherEpoch: epoch, ProfileID: job.ProfileID,
		ProfileVersion: job.ProfileVersion, Metric: job.Metric, Window: job.Window,
		CapturedAt: now.UTC().Format(time.RFC3339), ReadState: "observed", QueryLimit: 1001,
		Items: []collection.Item{a, a, b},
	}
	raw, _ := json.Marshal(obs)
	digest, _ := collection.RequestDigest(raw)
	if _, err := svc.SubmitObservation("p", epoch, obs, digest, now); err != nil {
		t.Fatal(err)
	}
	day := time.UnixMicro(mustUs(t, job.Window.Start)).UTC().Format("2006-01-02")

	ag := &Service{DB: db}
	if _, err := ag.RecomputeDirty(now); err != nil {
		t.Fatal(err)
	}
	sum1 := dailySum(t, db, day, "step_count", "p_steps")
	if sum1 != 4000 {
		t.Fatalf("first aggregate sum = %v, want 4000", sum1)
	}

	// Re-read the same window with the same multiset reordered: a fresh attempt,
	// but the content digest matches, so storage is reused and the sum unchanged.
	plan2 := plan
	plan2.PlanID = store.NewUUID()
	job2 := job
	job2.AttemptID = store.NewUUID()
	if _, err := db.Exec("UPDATE collection_plans SET state='expired' WHERE publisher_id='p' AND state='active'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO collection_plans(id,publisher_id,publisher_epoch,server_generation,state,expires_at_us) VALUES(?,?,?,?,'active',?)`,
		plan2.PlanID, "p", epoch, plan.ServerGeneration, now.Add(time.Hour).UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO collection_attempts(id,plan_id,job_id,expires_at_us,state) VALUES(?,?,?,?,'issued')`,
		job2.AttemptID, plan2.PlanID, job.JobID, now.Add(time.Hour).UnixMicro()); err != nil {
		t.Fatal(err)
	}
	obs2 := obs
	obs2.PlanID = plan2.PlanID
	obs2.AttemptID = job2.AttemptID
	obs2.Items = []collection.Item{b, a, a}
	raw2, _ := json.Marshal(obs2)
	digest2, _ := collection.RequestDigest(raw2)
	if _, err := svc.SubmitObservation("p", epoch, obs2, digest2, now); err != nil {
		t.Fatal(err)
	}
	if _, err := ag.RecomputeDirty(now); err != nil {
		t.Fatal(err)
	}
	if sum2 := dailySum(t, db, day, "step_count", "p_steps"); sum2 != 4000 {
		t.Fatalf("re-read aggregate sum = %v, want 4000 (no doubling)", sum2)
	}
}

func dailySum(t *testing.T, db *sql.DB, day, metric, profile string) float64 {
	t.Helper()
	var vj string
	if err := db.QueryRow(`SELECT value_json FROM daily_metrics WHERE day=? AND timezone='UTC' AND metric=? AND profile_id=?`,
		day, metric, profile).Scan(&vj); err != nil {
		t.Fatal(err)
	}
	var v struct {
		Sum *float64 `json:"sum"`
	}
	if err := json.Unmarshal([]byte(vj), &v); err != nil {
		t.Fatal(err)
	}
	if v.Sum == nil {
		t.Fatal("sum is nil")
	}
	return *v.Sum
}

func TestProfileSwitchSeparateRows(t *testing.T) {
	db := migrate(t)
	insertProfile(t, db, "p1", "heart_rate", 86400, true)
	insertProfile(t, db, "p2", "heart_rate", 86400, false)
	w1, w2 := store.NewUUID(), store.NewUUID()
	insertWindow(t, db, w1, "p1", mustUs(t, "2026-01-02T00:00:00Z"), mustUs(t, "2026-01-03T00:00:00Z"))
	insertWindow(t, db, w2, "p2", mustUs(t, "2026-01-02T00:00:00Z"), mustUs(t, "2026-01-03T00:00:00Z"))
	markObserved(t, db, w1, "observed", []gItem{{Start: "2026-01-02T08:00:00Z", Value: 60, Unit: "count/min", Source: "Apple Watch"}}, "2026-01-02T12:00:00Z", "2026-01-02T12:00:00Z")
	markObserved(t, db, w2, "observed", []gItem{{Start: "2026-01-02T09:00:00Z", Value: 70, Unit: "count/min", Source: "Apple Watch"}}, "2026-01-02T12:00:00Z", "2026-01-02T12:00:00Z")
	if _, err := db.Exec("INSERT INTO dirty_days(day,timezone,observation_revision) VALUES('2026-01-02','UTC',0)"); err != nil {
		t.Fatal(err)
	}
	ag := &Service{DB: db}
	if _, err := ag.RecomputeDirty(time.Now()); err != nil {
		t.Fatal(err)
	}
	// p1 only (p2 disabled).
	var n1 int
	if err := db.QueryRow("SELECT count(*) FROM daily_metrics WHERE day='2026-01-02'").Scan(&n1); err != nil {
		t.Fatal(err)
	}
	if n1 != 1 {
		t.Fatalf("rows after p1 = %d, want 1", n1)
	}
	// Switch: disable p1, enable p2, recompute.
	if _, err := db.Exec("UPDATE collection_profiles SET enabled=0 WHERE id='p1'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE collection_profiles SET enabled=1 WHERE id='p2'"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT OR REPLACE INTO dirty_days(day,timezone,observation_revision) VALUES('2026-01-02','UTC',0)"); err != nil {
		t.Fatal(err)
	}
	if _, err := ag.RecomputeDirty(time.Now()); err != nil {
		t.Fatal(err)
	}
	var n2 int
	if err := db.QueryRow("SELECT count(*) FROM daily_metrics WHERE day='2026-01-02'").Scan(&n2); err != nil {
		t.Fatal(err)
	}
	if n2 != 2 {
		t.Fatalf("rows after switch = %d, want 2 (separate profile rows)", n2)
	}
	if sum1 := dailyMin(t, db, "2026-01-02", "heart_rate", "p1"); sum1 != 60 {
		t.Fatalf("p1 min = %v, want 60 (unchanged)", sum1)
	}
	if sum2 := dailyMin(t, db, "2026-01-02", "heart_rate", "p2"); sum2 != 70 {
		t.Fatalf("p2 min = %v, want 70", sum2)
	}
}

func dailyMin(t *testing.T, db *sql.DB, day, metric, profile string) float64 {
	t.Helper()
	var vj string
	if err := db.QueryRow(`SELECT value_json FROM daily_metrics WHERE day=? AND timezone='UTC' AND metric=? AND profile_id=?`,
		day, metric, profile).Scan(&vj); err != nil {
		t.Fatal(err)
	}
	var v struct {
		Min *float64 `json:"min"`
	}
	if err := json.Unmarshal([]byte(vj), &v); err != nil {
		t.Fatal(err)
	}
	return *v.Min
}

func TestRevisionCAS(t *testing.T) {
	db := migrate(t)
	insertProfile(t, db, "p_hr", "heart_rate", 86400, true)
	w := store.NewUUID()
	insertWindow(t, db, w, "p_hr", mustUs(t, "2026-01-02T00:00:00Z"), mustUs(t, "2026-01-03T00:00:00Z"))
	markObserved(t, db, w, "observed", []gItem{{Start: "2026-01-02T08:00:00Z", Value: 60, Unit: "count/min", Source: "Apple Watch"}}, "2026-01-02T12:00:00Z", "2026-01-02T12:00:00Z")
	if _, err := db.Exec("INSERT INTO dirty_days(day,timezone,observation_revision) VALUES('2026-01-02','UTC',0)"); err != nil {
		t.Fatal(err)
	}
	ag := &Service{DB: db}
	rev, err := ag.currentRevision()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a concurrent observation landing during computation.
	if _, err := db.Exec("UPDATE instance_state SET observation_revision = observation_revision + 1 WHERE singleton=1"); err != nil {
		t.Fatal(err)
	}
	if err := ag.recomputeDay("2026-01-02", "UTC", rev, time.Now()); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("expected ErrRevisionChanged, got %v", err)
	}
	rev2, _ := ag.currentRevision()
	if err := ag.recomputeDay("2026-01-02", "UTC", rev2, time.Now()); err != nil {
		t.Fatalf("recompute with fresh revision: %v", err)
	}
}

func TestSleepWorkoutSkipped(t *testing.T) {
	db := migrate(t)
	insertProfile(t, db, "p_sleep", "sleep", 86400, true)
	w := store.NewUUID()
	insertWindow(t, db, w, "p_sleep", mustUs(t, "2026-01-02T00:00:00Z"), mustUs(t, "2026-01-03T00:00:00Z"))
	if _, err := db.Exec("INSERT INTO dirty_days(day,timezone,observation_revision) VALUES('2026-01-02','UTC',0)"); err != nil {
		t.Fatal(err)
	}
	ag := &Service{DB: db}
	if _, err := ag.RecomputeDirty(time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM daily_metrics").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("sleep profile produced %d daily rows, want 0 (S05b)", n)
	}
}
