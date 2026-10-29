// Package admin implements owner operations (S06b): consistent backup,
// metric/window-scoped deletion with exclusions, private export, and restore.
// Every operation runs on synthetic databases only in tests; nothing here is
// exposed over HTTP.
package admin

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/atongrun/health-bridge/server/internal/store"
)

// Manifest accompanies a backup file; it never contains health values.
type Manifest struct {
	SchemaVersion       int            `json:"schema_version"`
	Generation          string         `json:"generation"`
	ObservationRevision int64          `json:"observation_revision"`
	ProjectionRevision  int64          `json:"projection_revision"`
	CreatedAt           string         `json:"created_at"`
	TableCounts         map[string]int `json:"table_counts"`
	FileSHA256          string         `json:"file_sha256"`
}

// Backup writes a consistent copy of db to destDir using VACUUM INTO (which
// includes WAL content), verifies integrity and foreign keys on the copy, and
// writes a sidecar manifest. Plaintext is 0600; encryption is S08.
func Backup(db *sql.DB, destDir string, now time.Time) (backupPath, manifestPath string, err error) {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", "", err
	}
	name := "health-" + now.UTC().Format("20060102T150405Z") + ".db"
	backupPath = destDir + "/" + name
	_ = os.Remove(backupPath)
	quoted := "'" + strings.ReplaceAll(backupPath, "'", "''") + "'"
	if _, err := db.Exec("VACUUM INTO " + quoted); err != nil {
		return "", "", fmt.Errorf("vacuum into: %w", err)
	}
	if err := os.Chmod(backupPath, 0o600); err != nil {
		return "", "", err
	}
	copyDB, err := store.Open(backupPath, true)
	if err != nil {
		return "", "", err
	}
	defer copyDB.Close()
	if err := Verify(copyDB); err != nil {
		return "", "", fmt.Errorf("verify backup: %w", err)
	}
	if err := store.RequireCurrentSchema(copyDB); err != nil {
		return "", "", fmt.Errorf("backup schema: %w", err)
	}
	man, err := buildManifest(copyDB, backupPath, now)
	if err != nil {
		return "", "", err
	}
	manifestPath = destDir + "/" + name + ".manifest.json"
	b, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(manifestPath, b, 0o600); err != nil {
		return "", "", err
	}
	return backupPath, manifestPath, nil
}

// Verify runs integrity_check and foreign_key_check.
func Verify(db *sql.DB) error {
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("integrity_check: %s", integrity)
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign_key_check reported violations")
	}
	return rows.Err()
}

func buildManifest(db *sql.DB, path string, now time.Time) (Manifest, error) {
	var m Manifest
	if err := db.QueryRow("SELECT coalesce(max(version),0) FROM schema_migrations").Scan(&m.SchemaVersion); err != nil {
		return m, err
	}
	if err := db.QueryRow("SELECT generation, observation_revision, projection_revision FROM instance_state WHERE singleton=1").
		Scan(&m.Generation, &m.ObservationRevision, &m.ProjectionRevision); err != nil {
		return m, err
	}
	m.CreatedAt = now.UTC().Format(time.RFC3339)
	m.TableCounts = map[string]int{}
	for _, t := range []string{"collection_profiles", "collection_windows", "observation_snapshots",
		"observation_items", "observation_receipts", "daily_metrics", "owner_exclusions", "credentials"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + t).Scan(&n); err != nil {
			return m, err
		}
		m.TableCounts[t] = n
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return m, err
	}
	m.FileSHA256 = sum
	return m, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// DeleteOptions scopes a deletion.
type DeleteOptions struct {
	Metric string
	StartUs int64
	EndUs   int64
	Reason  string
}

// DeleteCounts is returned by both dry-run and applied deletion.
type DeleteCounts struct {
	Windows   int `json:"windows"`
	Snapshots int `json:"snapshots"`
	Items     int `json:"items"`
	Receipts  int `json:"receipts"`
	Attempts  int `json:"attempts"`
}

// Delete removes stored observations for one metric in [start,end): it clears
// current pointers, deletes snapshots (items and receipts cascade), marks
// attempts purged so they can never re-execute, and records an exclusion that
// blocks future collection of the same range. dryRun=true only counts.
func Delete(db *sql.DB, opts DeleteOptions, dryRun bool, now time.Time) (DeleteCounts, error) {
	var c DeleteCounts
	if opts.Metric == "" || opts.EndUs <= opts.StartUs {
		return c, errors.New("metric and a positive range are required")
	}
	if opts.Reason == "" {
		return c, errors.New("reason is required")
	}
	windowIDs, err := scopedWindows(db, opts)
	if err != nil {
		return c, err
	}
	c.Windows = len(windowIDs)
	if c.Windows == 0 {
		return c, nil
	}
	in := placeholders(len(windowIDs))
	args := toAny(windowIDs)

	count := func(q string) (int, error) {
		var n int
		err := db.QueryRow(q, args...).Scan(&n)
		return n, err
	}
	if c.Snapshots, err = count("SELECT count(*) FROM observation_snapshots WHERE job_id IN (" + in + ")"); err != nil {
		return c, err
	}
	if c.Items, err = count(`SELECT count(*) FROM observation_items WHERE snapshot_id IN
		(SELECT id FROM observation_snapshots WHERE job_id IN (` + in + `))`); err != nil {
		return c, err
	}
	if c.Receipts, err = count(`SELECT count(*) FROM observation_receipts WHERE attempt_id IN
		(SELECT id FROM collection_attempts WHERE job_id IN (` + in + `))`); err != nil {
		return c, err
	}
	if c.Attempts, err = count("SELECT count(*) FROM collection_attempts WHERE job_id IN (" + in + ") AND state <> 'purged'"); err != nil {
		return c, err
	}
	if dryRun {
		return c, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback() //nolint:errcheck
	// 1. clear pointers first so no reader can resolve a snapshot being deleted
	if _, err := tx.Exec("UPDATE collection_windows SET current_snapshot_id=NULL, last_state='excluded' WHERE id IN ("+in+")", args...); err != nil {
		return c, err
	}
	// 2. delete snapshots; items and receipts cascade
	if _, err := tx.Exec("DELETE FROM observation_snapshots WHERE job_id IN ("+in+")", args...); err != nil {
		return c, err
	}
	// 3. attempts become permanently non-executable
	if _, err := tx.Exec("UPDATE collection_attempts SET state='purged' WHERE job_id IN ("+in+")", args...); err != nil {
		return c, err
	}
	// 4. record the owner exclusion
	if _, err := tx.Exec(`INSERT INTO owner_exclusions(id,metric,start_us,end_us,created_at_us,reason) VALUES(?,?,?,?,?,?)`,
		store.NewUUID(), opts.Metric, opts.StartUs, opts.EndUs, now.UnixMicro(), opts.Reason); err != nil {
		return c, err
	}
	// 5. mark affected days dirty so projections drop the removed data
	var tz string
	if err := tx.QueryRow("SELECT report_timezone FROM instance_state WHERE singleton=1").Scan(&tz); err != nil {
		return c, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return c, err
	}
	day := func(us int64) string { return time.UnixMicro(us).In(loc).Format("2006-01-02") }
	for d := day(opts.StartUs); d <= day(opts.EndUs-1); d = nextDay(d, loc) {
		if _, err := tx.Exec("INSERT OR REPLACE INTO dirty_days(day,timezone,observation_revision) VALUES(?,?,?)", d, tz, 0); err != nil {
			return c, err
		}
	}
	if _, err := tx.Exec("UPDATE instance_state SET observation_revision = observation_revision + 1 WHERE singleton=1"); err != nil {
		return c, err
	}
	if err := tx.Commit(); err != nil {
		return c, err
	}
	return c, nil
}

func nextDay(d string, loc *time.Location) string {
	t, err := time.ParseInLocation("2006-01-02", d, loc)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

func scopedWindows(db *sql.DB, opts DeleteOptions) ([]string, error) {
	rows, err := db.Query(`SELECT w.id FROM collection_windows w
		JOIN collection_profiles p ON p.id = w.profile_id
		WHERE p.metric = ? AND w.start_us < ? AND w.end_us > ?`, opts.Metric, opts.EndUs, opts.StartUs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// RestoreSummary reports what restore changed.
type RestoreSummary struct {
	NewGeneration string `json:"new_generation"`
	RevokedCreds  int    `json:"revoked_credentials"`
	AbandonedPlan int    `json:"abandoned_plans"`
	ExpiredAttempt int   `json:"expired_attempts"`
}

// Restore installs backupPath as targetPath, verifies it, then rotates the
// instance: new generation, all credentials revoked, active plans/issued
// attempts abandoned, and queries paused. Restoring an older backup that lacks
// the latest owner exclusions therefore never opens queries automatically.
func Restore(backupPath, targetPath string, now time.Time) (RestoreSummary, error) {
	var sum RestoreSummary
	src, err := store.Open(backupPath, true)
	if err != nil {
		return sum, fmt.Errorf("open backup: %w", err)
	}
	if err := Verify(src); err != nil {
		src.Close()
		return sum, fmt.Errorf("backup rejected: %w", err)
	}
	if err := store.RequireCurrentSchema(src); err != nil {
		src.Close()
		return sum, fmt.Errorf("backup schema: %w", err)
	}
	src.Close()

	raw, err := os.ReadFile(backupPath)
	if err != nil {
		return sum, err
	}
	if err := os.WriteFile(targetPath, raw, 0o600); err != nil {
		return sum, err
	}
	os.Remove(targetPath + "-wal")
	os.Remove(targetPath + "-shm")

	db, err := store.Open(targetPath, false)
	if err != nil {
		return sum, err
	}
	defer db.Close()
	if err := Verify(db); err != nil {
		return sum, fmt.Errorf("restored database failed verification: %w", err)
	}

	sum.NewGeneration = store.NewUUID()
	tx, err := db.Begin()
	if err != nil {
		return sum, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec("UPDATE instance_state SET generation=?, queries_paused=1 WHERE singleton=1", sum.NewGeneration); err != nil {
		return sum, err
	}
	res, err := tx.Exec("UPDATE credentials SET revoked_at_us=? WHERE revoked_at_us IS NULL", now.UnixMicro())
	if err != nil {
		return sum, err
	}
	n, _ := res.RowsAffected()
	sum.RevokedCreds = int(n)
	res, err = tx.Exec("UPDATE collection_plans SET state='expired' WHERE state='active'")
	if err != nil {
		return sum, err
	}
	n, _ = res.RowsAffected()
	sum.AbandonedPlan = int(n)
	res, err = tx.Exec("UPDATE collection_attempts SET state='expired' WHERE state='issued'")
	if err != nil {
		return sum, err
	}
	n, _ = res.RowsAffected()
	sum.ExpiredAttempt = int(n)
	if err := tx.Commit(); err != nil {
		return sum, err
	}
	return sum, nil
}

// ExportOptions bounds an owner export.
type ExportOptions struct {
	StartDay string
	EndDay   string // exclusive
}

// Export writes daily rows, window statuses, snapshot metadata and exclusions
// as JSON to path (0600). It never includes credentials, token hashes, source
// JSON or raw item values; raw export is a separate, explicit owner action.
func Export(db *sql.DB, path string, opts ExportOptions) error {
	if opts.StartDay == "" || opts.EndDay == "" {
		return errors.New("start and end day are required")
	}
	out := map[string]any{
		"schema_version": 1,
		"range":          []string{opts.StartDay, opts.EndDay},
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
	}
	days, err := queryMaps(db, `SELECT day, timezone, metric, value_json, quality_json, method_version
		FROM daily_metrics WHERE day >= ? AND day < ? ORDER BY day, metric`, opts.StartDay, opts.EndDay)
	if err != nil {
		return err
	}
	endUs, err := time.Parse("2006-01-02", opts.EndDay)
	if err != nil {
		return fmt.Errorf("end_day: %w", err)
	}
	windows, err := queryMaps(db, `SELECT p.metric, w.start_us, w.end_us, w.last_state, w.last_committed_at_us
		FROM collection_windows w JOIN collection_profiles p ON p.id = w.profile_id
		WHERE w.start_us < ? ORDER BY w.start_us DESC LIMIT 10000`, endUs.UnixMicro())
	if err != nil {
		return err
	}
	exclusions, err := queryMaps(db, `SELECT metric, start_us, end_us, created_at_us, reason FROM owner_exclusions ORDER BY created_at_us`)
	if err != nil {
		return err
	}
	out["daily_metrics"] = days
	out["windows"] = windows
	out["owner_exclusions"] = exclusions
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func queryMaps(db *sql.DB, q string, args ...any) ([]map[string]any, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
