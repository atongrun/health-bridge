package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func openTemp(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path, false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func countTables(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMigrateEmptyDB(t *testing.T) {
	db, _ := openTemp(t)
	v, err := Migrate(db)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if v != 1 {
		t.Fatalf("version = %d, want 1", v)
	}
	if n := countTables(t, db); n != 18 {
		t.Fatalf("table count = %d, want 18", n)
	}
	var generation string
	if err := db.QueryRow("SELECT generation FROM instance_state WHERE singleton=1").Scan(&generation); err != nil {
		t.Fatalf("instance_state: %v", err)
	}
	if generation == "" {
		t.Fatal("instance generation is empty")
	}
	if err := RequireCurrentSchema(db); err != nil {
		t.Fatalf("schema not current after migrate: %v", err)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db, _ := openTemp(t)
	if _, err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	v, err := Migrate(db)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if v != 1 {
		t.Fatalf("second migrate version = %d, want 1", v)
	}
}

func TestMigrateChecksumTamper(t *testing.T) {
	db, _ := openTemp(t)
	if _, err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE schema_migrations SET checksum='deadbeef' WHERE version=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(db); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestFailedMigrationRollsBack(t *testing.T) {
	db, _ := openTemp(t)
	good := Migration{Version: 1, Name: "001_good.sql", SQLText: "CREATE TABLE t1(x INTEGER);"}
	bad := Migration{Version: 2, Name: "002_bad.sql", SQLText: "CREATE TABLE t2(x INTEGER); INSERT INTO missing VALUES(1);"}
	if _, err := Apply(db, []Migration{good, bad}); err == nil {
		t.Fatal("expected failing migration to return an error")
	}
	var n int
	_ = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='t1'").Scan(&n)
	if n != 0 {
		t.Fatal("failed migration left partial tables behind")
	}
}

func TestReadOnlyRejectsWrite(t *testing.T) {
	db, path := openTemp(t)
	if _, err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	ro, err := Open(path, true)
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer ro.Close()
	if _, err := ro.Exec("INSERT INTO publishers(id,epoch,active,created_at_us) VALUES('p','e',1,0)"); err == nil {
		t.Fatal("write on read-only connection succeeded")
	}
}

func TestEnableProfileGate(t *testing.T) {
	db, _ := openTemp(t)
	if _, err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	ins := `INSERT INTO collection_profiles(id,version,metric,window_seconds,mode,mapping_json) VALUES(?,1,'heart_rate',900,'on_demand','{}')`
	if _, err := db.Exec(ins, "p1"); err != nil {
		t.Fatal(err)
	}
	if err := EnableProfile(db, "p1"); err == nil || !strings.Contains(err.Error(), "S01-verified") {
		t.Fatalf("expected unverified gate, got %v", err)
	}
	if _, err := db.Exec("UPDATE collection_profiles SET verified=1 WHERE id='p1'"); err != nil {
		t.Fatal(err)
	}
	if err := EnableProfile(db, "p1"); err == nil || !strings.Contains(err.Error(), "evidence_ref") {
		t.Fatalf("expected evidence gate, got %v", err)
	}
	if _, err := db.Exec("UPDATE collection_profiles SET evidence_ref='probe://s01' WHERE id='p1'"); err != nil {
		t.Fatal(err)
	}
	if err := EnableProfile(db, "p1"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// A second enabled profile for the same metric is rejected by the DDL guard.
	if _, err := db.Exec(ins, "p2"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE collection_profiles SET verified=1, evidence_ref='probe://s01' WHERE id='p2'"); err != nil {
		t.Fatal(err)
	}
	if err := EnableProfile(db, "p2"); err == nil {
		t.Fatal("second enabled profile for one metric was accepted")
	}
}

func Test900SecondScheduledProfileRejected(t *testing.T) {
	db, _ := openTemp(t)
	if _, err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	_, err := db.Exec(`INSERT INTO collection_profiles(id,version,metric,window_seconds,mode,mapping_json) VALUES('p',1,'heart_rate',900,'scheduled','{}')`)
	if err == nil {
		t.Fatal("900s scheduled profile was accepted; must be on_demand only")
	}
	if _, err := db.Exec(`INSERT INTO collection_profiles(id,version,metric,window_seconds,mode,mapping_json) VALUES('p',1,'heart_rate',86400,'scheduled','{}')`); err != nil {
		t.Fatalf("86400s scheduled profile rejected: %v", err)
	}
}
