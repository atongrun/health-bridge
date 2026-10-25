package collection_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/collection"
	"github.com/atongrun/health-bridge/server/internal/httpapi"
	"github.com/atongrun/health-bridge/server/internal/store"
)

type env struct {
	db        *sql.DB
	token     string
	pubID     string
	pubEpoch  string
	profileID string
	srv       *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	epoch, err := auth.ReplacePublisher(db, "publisher-test", now)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := auth.CreateCredential(db, "upload", "{}", "{}", "publisher-test", epoch, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	profileID := "synthetic_hr_daily"
	_, err = db.Exec(`INSERT INTO collection_profiles(id,version,metric,window_seconds,mode,mapping_json,evidence_ref,verified,enabled)
		VALUES(?,1,'heart_rate',86400,'scheduled','{}','probe://s01',1,1)`, profileID)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewHandler(db))
	t.Cleanup(func() { srv.Close(); db.Close() })
	return &env{db: db, token: token, pubID: "publisher-test", pubEpoch: epoch, profileID: profileID, srv: srv}
}

func (e *env) doPlan(t *testing.T) collection.PlanResponse {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/v1/shortcuts/plan", strings.NewReader(`{"protocol":"shortcuts-1"}`))
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var plan collection.PlanResponse
	if err := json.NewDecoder(resp.Body).Decode(&plan); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || plan.Status != "planned" || len(plan.Jobs) == 0 {
		t.Fatalf("plan status=%d status=%q jobs=%d", resp.StatusCode, plan.Status, len(plan.Jobs))
	}
	return plan
}

// reissue simulates a lease expiry and re-read: it expires the active plan and
// issues a fresh attempt for the same job.
func (e *env) reissue(t *testing.T, plan collection.PlanResponse, job collection.PlanJob) (collection.PlanResponse, collection.PlanJob) {
	t.Helper()
	if _, err := e.db.Exec("UPDATE collection_plans SET state='expired' WHERE publisher_id=? AND state='active'", e.pubID); err != nil {
		t.Fatal(err)
	}
	newPlan := store.NewUUID()
	newAttempt := store.NewUUID()
	now := time.Now().Add(time.Hour).UnixMicro()
	if _, err := e.db.Exec("INSERT INTO collection_plans(id,publisher_id,publisher_epoch,server_generation,state,expires_at_us) VALUES(?,?,?,?,'active',?)",
		newPlan, e.pubID, e.pubEpoch, plan.ServerGeneration, now); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec("INSERT INTO collection_attempts(id,plan_id,job_id,expires_at_us,state) VALUES(?,?,?,?,'issued')",
		newAttempt, newPlan, job.JobID, now); err != nil {
		t.Fatal(err)
	}
	np, nj := plan, job
	np.PlanID = newPlan
	nj.AttemptID = newAttempt
	return np, nj
}

func (e *env) submit(t *testing.T, obs collection.Observation) (int, collection.ACK, map[string]any) {
	t.Helper()
	body, err := json.Marshal(obs)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/v1/shortcuts/observations", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	var ack collection.ACK
	if resp.StatusCode == http.StatusOK {
		b, _ := json.Marshal(decoded)
		_ = json.Unmarshal(b, &ack)
	}
	return resp.StatusCode, ack, decoded
}

func qtyItem(start string, value float64) collection.Item {
	return collection.Item{
		Start: start, TimePrecision: "second",
		Quantity: &collection.Quantity{Value: value, Unit: "count/min"},
	}
}

func buildObs(plan collection.PlanResponse, job collection.PlanJob, items []collection.Item, readState string) collection.Observation {
	return collection.Observation{
		Protocol:         "shortcuts-1",
		PlanID:           plan.PlanID,
		JobID:            job.JobID,
		AttemptID:        job.AttemptID,
		ServerGeneration: plan.ServerGeneration,
		PublisherEpoch:   plan.PublisherEpoch,
		ProfileID:        job.ProfileID,
		ProfileVersion:   job.ProfileVersion,
		Metric:           job.Metric,
		Window:           job.Window,
		CapturedAt:       time.Now().UTC().Format(time.RFC3339),
		ReadState:        readState,
		QueryLimit:       1001,
		LimitReached:     false,
		Items:            items,
	}
}

func (e *env) snapshotCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow("SELECT count(*) FROM observation_snapshots").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) itemCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow("SELECT count(*) FROM observation_items").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func errCode(decoded map[string]any) string {
	if e, ok := decoded["error"].(map[string]any); ok {
		if c, ok := e["code"].(string); ok {
			return c
		}
	}
	return ""
}

func TestEndToEndCommitAndIdempotentReplay(t *testing.T) {
	e := newEnv(t)
	plan := e.doPlan(t)
	job := plan.Jobs[0]
	start, _ := time.Parse(time.RFC3339, job.Window.Start)
	item := qtyItem(start.Add(time.Hour).Format(time.RFC3339), 60)
	obs := buildObs(plan, job, []collection.Item{item}, "observed")

	status, ack, _ := e.submit(t, obs)
	if status != http.StatusOK || ack.Status != "committed" || ack.ReceivedCount != 1 || ack.StoredCount != 1 {
		t.Fatalf("first submit: status=%d ack=%+v", status, ack)
	}
	// ACK lost, retry identical request -> original ACK, no new rows.
	status2, ack2, _ := e.submit(t, obs)
	if status2 != http.StatusOK || ack2.SnapshotID != ack.SnapshotID {
		t.Fatalf("replay: status=%d snapshot %q != %q", status2, ack2.SnapshotID, ack.SnapshotID)
	}
	if e.snapshotCount(t) != 1 || e.itemCount(t) != 1 {
		t.Fatalf("replay grew storage: snapshots=%d items=%d", e.snapshotCount(t), e.itemCount(t))
	}
	// Same attempt, different body -> 409 attempt_conflict.
	obs.Items[0].Quantity.Value = 61
	status3, _, decoded := e.submit(t, obs)
	if status3 != http.StatusConflict || errCode(decoded) != "attempt_conflict" {
		t.Fatalf("conflict status=%d code=%q", status3, errCode(decoded))
	}
}

func TestReorderedMultisetReusesSnapshot(t *testing.T) {
	e := newEnv(t)
	plan := e.doPlan(t)
	job := plan.Jobs[0]
	start, _ := time.Parse(time.RFC3339, job.Window.Start)
	a := qtyItem(start.Add(time.Hour).Format(time.RFC3339), 60)
	b := qtyItem(start.Add(2*time.Hour).Format(time.RFC3339), 61)

	_, ack1, _ := e.submit(t, buildObs(plan, job, []collection.Item{a, a, b}, "observed"))
	plan2, job2 := e.reissue(t, plan, job)
	_, ack2, _ := e.submit(t, buildObs(plan2, job2, []collection.Item{b, a, a}, "observed"))
	if ack1.SnapshotID != ack2.SnapshotID {
		t.Fatalf("reorder produced a new snapshot: %q vs %q", ack1.SnapshotID, ack2.SnapshotID)
	}
	if e.snapshotCount(t) != 1 || e.itemCount(t) != 3 {
		t.Fatalf("storage grew: snapshots=%d items=%d", e.snapshotCount(t), e.itemCount(t))
	}
}

func TestMultiplicityChangeIsNewSnapshot(t *testing.T) {
	e := newEnv(t)
	plan := e.doPlan(t)
	job := plan.Jobs[0]
	start, _ := time.Parse(time.RFC3339, job.Window.Start)
	a := qtyItem(start.Add(time.Hour).Format(time.RFC3339), 60)
	b := qtyItem(start.Add(2*time.Hour).Format(time.RFC3339), 61)

	_, ack1, _ := e.submit(t, buildObs(plan, job, []collection.Item{a, a, b}, "observed"))
	plan2, job2 := e.reissue(t, plan, job)
	_, ack2, _ := e.submit(t, buildObs(plan2, job2, []collection.Item{a, b}, "observed"))
	if ack1.SnapshotID == ack2.SnapshotID {
		t.Fatal("multiplicity change reused the same snapshot")
	}
	if e.snapshotCount(t) != 2 || e.itemCount(t) != 5 {
		t.Fatalf("snapshots=%d items=%d, want 2/5", e.snapshotCount(t), e.itemCount(t))
	}
}

func TestEmptyWindowKeepsHistory(t *testing.T) {
	e := newEnv(t)
	plan := e.doPlan(t)
	job := plan.Jobs[0]
	start, _ := time.Parse(time.RFC3339, job.Window.Start)
	item := qtyItem(start.Add(time.Hour).Format(time.RFC3339), 60)
	if status, _, _ := e.submit(t, buildObs(plan, job, []collection.Item{item}, "observed")); status != http.StatusOK {
		t.Fatal("observed submit failed")
	}
	empty := buildObs(plan, job, nil, "no_visible_data")
	status, ack, _ := e.submit(t, empty)
	if status != http.StatusOK || ack.ReadState != "no_visible_data" || ack.ReceivedCount != 0 {
		t.Fatalf("empty submit: status=%d ack=%+v", status, ack)
	}
	if e.snapshotCount(t) != 2 {
		t.Fatalf("history was deleted: snapshots=%d", e.snapshotCount(t))
	}
}

func TestStaleAttemptSuperseded(t *testing.T) {
	e := newEnv(t)
	plan := e.doPlan(t)
	job := plan.Jobs[0]
	start, _ := time.Parse(time.RFC3339, job.Window.Start)
	item := qtyItem(start.Add(time.Hour).Format(time.RFC3339), 60)
	obs := buildObs(plan, job, []collection.Item{item}, "observed")
	if status, _, _ := e.submit(t, obs); status != http.StatusOK {
		t.Fatalf("initial commit failed: %d", status)
	}
	// A newer attempt supersedes the committed one; the old attempt arrives late.
	_, _ = e.reissue(t, plan, job)
	status, _, decoded := e.submit(t, obs)
	if status != http.StatusConflict || errCode(decoded) != "attempt_superseded" {
		t.Fatalf("stale attempt status=%d code=%q", status, errCode(decoded))
	}
}

func TestSingleActivePlan(t *testing.T) {
	e := newEnv(t)
	_ = e.doPlan(t)
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/v1/shortcuts/plan", strings.NewReader(`{"protocol":"shortcuts-1"}`))
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("second plan status = %d, want 409", resp.StatusCode)
	}
}

func TestFailureEndpoint(t *testing.T) {
	e := newEnv(t)
	plan := e.doPlan(t)
	job := plan.Jobs[0]
	body, _ := json.Marshal(map[string]string{"protocol": "shortcuts-1", "attempt_id": job.AttemptID, "reason": "overflow"})
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/v1/shortcuts/attempts/"+job.AttemptID+"/failure", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("failure status = %d, want 200", resp.StatusCode)
	}
	var state string
	if err := e.db.QueryRow("SELECT last_state FROM collection_windows WHERE id=?", job.JobID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "overflow" {
		t.Fatalf("window last_state = %q, want overflow", state)
	}
}

func TestStorageFailureNoPartialACK(t *testing.T) {
	e := newEnv(t)
	plan := e.doPlan(t)
	job := plan.Jobs[0]
	start, _ := time.Parse(time.RFC3339, job.Window.Start)
	obs := buildObs(plan, job, []collection.Item{qtyItem(start.Add(time.Hour).Format(time.RFC3339), 60)}, "observed")
	digest, _ := collection.RequestDigest(mustJSON(obs))
	e.db.Close() // storage gone
	svc := &collection.Service{DB: e.db}
	if _, err := svc.SubmitObservation(e.pubID, e.pubEpoch, obs, digest, time.Now()); err == nil {
		t.Fatal("expected storage failure to surface an error, got nil")
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
