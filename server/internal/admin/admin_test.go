package admin_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atongrun/health-bridge/server/internal/admin"
	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/collection"
	"github.com/atongrun/health-bridge/server/internal/store"
)

type env struct {
	dir     string
	db      *sql.DB
	path    string
	pubID   string
	epoch   string
	revoked string
	startUs int64
	endUs   int64
	jobID   string
	attempt string
}

// newEnv seeds one committed observation for heart_rate in a synthetic day.
func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "health.db")
	db, err := store.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	epoch, err := auth.ReplacePublisher(db, "p", now)
	if err != nil {
		t.Fatal(err)
	}
	_, uploadTok, err := auth.CreateCredential(db, "upload", "{}", "{}", "p", epoch, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO collection_profiles(id,version,metric,window_seconds,mode,mapping_json,evidence_ref,verified,enabled)
		VALUES('p_hr',1,'heart_rate',86400,'scheduled','{}','probe://s01',1,1)`); err != nil {
		t.Fatal(err)
	}
	svc := &collection.Service{DB: db}
	plan, err := svc.Plan("p", epoch, now, 15*time.Minute, 7)
	if err != nil {
		t.Fatal(err)
	}
	job := plan.Jobs[0]
	start, _ := time.Parse(time.RFC3339, job.Window.Start)
	item := collection.Item{Start: start.Add(time.Hour).Format(time.RFC3339), TimePrecision: "second",
		Quantity: &collection.Quantity{Value: 60, Unit: "count/min"}}
	obs := collection.Observation{
		Protocol: "shortcuts-1", PlanID: plan.PlanID, JobID: job.JobID, AttemptID: job.AttemptID,
		ServerGeneration: plan.ServerGeneration, PublisherEpoch: epoch, ProfileID: job.ProfileID,
		ProfileVersion: job.ProfileVersion, Metric: job.Metric, Window: job.Window,
		CapturedAt: now.UTC().Format(time.RFC3339), ReadState: "observed", QueryLimit: 1001,
		Items: []collection.Item{item},
	}
	raw, _ := json.Marshal(obs)
	digest, _ := collection.RequestDigest(raw)
	if _, err := svc.SubmitObservation("p", epoch, obs, digest, now); err != nil {
		t.Fatal(err)
	}
	startUs, _ := time.Parse(time.RFC3339, job.Window.Start)
	endUs, _ := time.Parse(time.RFC3339, job.Window.End)
	t.Cleanup(func() { db.Close() })
	return &env{dir: dir, db: db, path: path, pubID: "p", epoch: epoch, revoked: uploadTok,
		startUs: startUs.UnixMicro(), endUs: endUs.UnixMicro(), jobID: job.JobID, attempt: job.AttemptID}
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBackupIncludesWALAndVerifies(t *testing.T) {
	e := newEnv(t)
	// Deliberately do not checkpoint; VACUUM INTO must still capture WAL data.
	backupDir := filepath.Join(e.dir, "backups")
	bpath, mpath, err := admin.Backup(e.db, backupDir, time.Now())
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	info, err := os.Stat(bpath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup perms = %o, want 600", info.Mode().Perm())
	}
	copyDB, err := store.Open(bpath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	if n := count(t, copyDB, "observation_items"); n != 1 {
		t.Fatalf("backup items = %d, want 1 (WAL content missing)", n)
	}
	if _, err := os.Stat(mpath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	raw, _ := os.ReadFile(mpath)
	var man admin.Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatal(err)
	}
	if man.SchemaVersion != 1 || man.TableCounts["observation_items"] != 1 || len(man.FileSHA256) != 64 {
		t.Fatalf("manifest unexpected: %+v", man)
	}
	// Manifest carries counters only, never health values.
	if contains(raw, "count/min") {
		t.Fatal("manifest leaked a health unit/value")
	}
}

func contains(haystack []byte, needle string) bool { return strings.Contains(string(haystack), needle) }

func TestRestoreRotatesInstance(t *testing.T) {
	e := newEnv(t)
	backupDir := filepath.Join(e.dir, "backups")
	bpath, _, err := admin.Backup(e.db, backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var oldGen string
	if err := e.db.QueryRow("SELECT generation FROM instance_state WHERE singleton=1").Scan(&oldGen); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(e.dir, "restored.db")
	sum, err := admin.Restore(bpath, target, time.Now())
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if sum.NewGeneration == oldGen || sum.NewGeneration == "" {
		t.Fatalf("generation not rotated: %q", sum.NewGeneration)
	}
	if sum.RevokedCreds < 1 {
		t.Fatalf("revoked credentials = %d, want >=1", sum.RevokedCreds)
	}
	db, err := store.Open(target, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var paused, live int
	if err := db.QueryRow("SELECT queries_paused FROM instance_state WHERE singleton=1").Scan(&paused); err != nil {
		t.Fatal(err)
	}
	if paused != 1 {
		t.Fatal("queries not paused after restore")
	}
	if err := db.QueryRow("SELECT count(*) FROM credentials WHERE revoked_at_us IS NULL").Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 0 {
		t.Fatalf("live credentials after restore = %d, want 0", live)
	}
	// Restored history stays history; data itself is intact.
	if n := count(t, db, "observation_items"); n != 1 {
		t.Fatalf("restored items = %d, want 1", n)
	}
}

func TestRestoreRejectsCorruptBackup(t *testing.T) {
	e := newEnv(t)
	backupDir := filepath.Join(e.dir, "backups")
	bpath, _, err := admin.Backup(e.db, backupDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(bpath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 100; i < 200 && i < len(raw); i++ {
		raw[i] ^= 0xff
	}
	if err := os.WriteFile(bpath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(e.dir, "restored.db")
	if _, err := admin.Restore(bpath, target, time.Now()); err == nil {
		t.Fatal("restore accepted a corrupt backup")
	}
}

func TestDeleteDryRunThenApply(t *testing.T) {
	e := newEnv(t)
	opts := admin.DeleteOptions{Metric: "heart_rate", StartUs: e.startUs, EndUs: e.endUs, Reason: "owner request"}

	dry, err := admin.Delete(e.db, opts, true, time.Now())
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dry.Windows != 1 || dry.Snapshots != 1 || dry.Items != 1 || dry.Attempts != 1 {
		t.Fatalf("dry-run counts unexpected: %+v", dry)
	}
	if count(t, e.db, "observation_snapshots") != 1 {
		t.Fatal("dry run mutated data")
	}

	got, err := admin.Delete(e.db, opts, false, time.Now())
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got.Snapshots != 1 {
		t.Fatalf("deleted snapshots = %d", got.Snapshots)
	}
	if n := count(t, e.db, "observation_snapshots"); n != 0 {
		t.Fatalf("snapshots remain: %d", n)
	}
	if n := count(t, e.db, "observation_items"); n != 0 {
		t.Fatalf("items remain: %d", n)
	}
	if n := count(t, e.db, "observation_receipts"); n != 0 {
		t.Fatalf("receipts remain: %d", n)
	}
	var ptr sql.NullString
	var state string
	if err := e.db.QueryRow("SELECT current_snapshot_id, last_state FROM collection_windows WHERE id=?", e.jobID).
		Scan(&ptr, &state); err != nil {
		t.Fatal(err)
	}
	if ptr.Valid || state != "excluded" {
		t.Fatalf("window pointer/state = %v/%q, want NULL/excluded", ptr, state)
	}
	var attemptState string
	if err := e.db.QueryRow("SELECT state FROM collection_attempts WHERE id=?", e.attempt).Scan(&attemptState); err != nil {
		t.Fatal(err)
	}
	if attemptState != "purged" {
		t.Fatalf("attempt state = %q, want purged", attemptState)
	}
	if n := count(t, e.db, "owner_exclusions"); n != 1 {
		t.Fatalf("exclusions = %d, want 1", n)
	}
	if n := count(t, e.db, "dirty_days"); n == 0 {
		t.Fatal("affected days not marked dirty")
	}
}

func TestPurgedAttemptCannotReexecuteEvenAfterExclusionRemoved(t *testing.T) {
	e := newEnv(t)
	var planID, gen string
	if err := e.db.QueryRow("SELECT id FROM collection_plans WHERE state='active'").Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := e.db.QueryRow("SELECT generation FROM instance_state WHERE singleton=1").Scan(&gen); err != nil {
		t.Fatal(err)
	}
	opts := admin.DeleteOptions{Metric: "heart_rate", StartUs: e.startUs, EndUs: e.endUs, Reason: "owner request"}
	if _, err := admin.Delete(e.db, opts, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Owner lifts the exclusion; the old attempt must still stay dead.
	if _, err := e.db.Exec("DELETE FROM owner_exclusions"); err != nil {
		t.Fatal(err)
	}
	svc := &collection.Service{DB: e.db}
	start := time.UnixMicro(e.startUs).UTC().Format(time.RFC3339)
	obs := collection.Observation{
		Protocol: "shortcuts-1", PlanID: planID, JobID: e.jobID, AttemptID: e.attempt,
		ServerGeneration: gen, PublisherEpoch: e.epoch, ProfileID: "p_hr", ProfileVersion: 1,
		Metric: "heart_rate", Window: collection.Window{Start: start, End: time.UnixMicro(e.endUs).UTC().Format(time.RFC3339)},
		CapturedAt: time.Now().UTC().Format(time.RFC3339), ReadState: "no_visible_data", QueryLimit: 1001,
	}
	if _, err := svc.SubmitObservation(e.pubID, e.epoch, obs, "deadbeef", time.Now()); !errors.Is(err, collection.ErrAttemptSuperseded) {
		t.Fatalf("purged attempt re-executed or wrong error: %v", err)
	}
}

func TestIngestBlockedByExclusion(t *testing.T) {
	e := newEnv(t)
	opts := admin.DeleteOptions{Metric: "heart_rate", StartUs: e.startUs, EndUs: e.endUs, Reason: "owner request"}
	if _, err := admin.Delete(e.db, opts, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	// A fresh attempt on the excluded window must be refused.
	var gen string
	if err := e.db.QueryRow("SELECT generation FROM instance_state WHERE singleton=1").Scan(&gen); err != nil {
		t.Fatal(err)
	}
	planID := store.NewUUID()
	attemptID := store.NewUUID()
	now := time.Now().Add(time.Hour)
	// Close the plan left active by newEnv so the single-active index allows a new one.
	if _, err := e.db.Exec("UPDATE collection_plans SET state='expired' WHERE state='active'"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec("INSERT INTO collection_plans(id,publisher_id,publisher_epoch,server_generation,state,expires_at_us) VALUES(?,?,?,?,'active',?)",
		planID, e.pubID, e.epoch, gen, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec("INSERT INTO collection_attempts(id,plan_id,job_id,expires_at_us,state) VALUES(?,?,?,?,'issued')",
		attemptID, planID, e.jobID, now.UnixMicro()); err != nil {
		t.Fatal(err)
	}
	svc := &collection.Service{DB: e.db}
	obs := collection.Observation{
		Protocol: "shortcuts-1", PlanID: planID, JobID: e.jobID, AttemptID: attemptID,
		ServerGeneration: gen, PublisherEpoch: e.epoch, ProfileID: "p_hr", ProfileVersion: 1,
		Metric: "heart_rate",
		Window: collection.Window{
			Start: time.UnixMicro(e.startUs).UTC().Format(time.RFC3339),
			End:   time.UnixMicro(e.endUs).UTC().Format(time.RFC3339),
		},
		CapturedAt: time.Now().UTC().Format(time.RFC3339), ReadState: "no_visible_data", QueryLimit: 1001,
	}
	if _, err := svc.SubmitObservation(e.pubID, e.epoch, obs, "feedface", time.Now()); !errors.Is(err, collection.ErrCollectionExcl) {
		t.Fatalf("expected ErrCollectionExcl, got %v", err)
	}
}

func TestExportHasNoCredentials(t *testing.T) {
	e := newEnv(t)
	path := filepath.Join(e.dir, "export.json")
	day := time.UnixMicro(e.startUs).UTC().Format("2006-01-02")
	next := time.UnixMicro(e.endUs).UTC().Format("2006-01-02")
	if err := admin.Export(e.db, path, admin.ExportOptions{StartDay: day, EndDay: next}); err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("export perms = %o", info.Mode().Perm())
	}
	for _, forbidden := range []string{"token_hash", "source_json", "credentials"} {
		if contains(raw, forbidden) {
			t.Fatalf("export leaked %q", forbidden)
		}
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["owner_exclusions"]; !ok {
		t.Fatal("export missing owner_exclusions")
	}
}
