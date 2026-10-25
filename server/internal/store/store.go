// Package store opens the SQLite database, applies versioned migrations, and
// exposes the owner-side gates (profile enable) that S03a implements. HTTP
// collection endpoints arrive in S03b; nothing here performs business upload.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver; CGO=0, no Xcode/Swift/GRDB
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration is one versioned, checksummed schema step.
type Migration struct {
	Version  int
	Name     string
	SQLText  string
	Checksum string
}

// Open returns a single-writer connection pool. readonly=true opens a
// read-only handle used by the query path (S06); writes on it must fail.
func Open(path string, readonly bool) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	dsn := "file:" + path
	if readonly {
		dsn += "?mode=ro"
	} else {
		dsn += "?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)" +
			"&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	// One connection serializes every writer; SQLite + WAL already assume a
	// single writer per database, so this encodes that assumption.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping %q: %w", path, err)
	}
	return db, nil
}

func builtinMigrations() []Migration {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		panic(err) // embed contents are compile-time constant
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	ms := make([]Migration, 0, len(names))
	for _, name := range names {
		prefix := strings.SplitN(name, "_", 2)[0]
		version, err := strconv.Atoi(prefix)
		if err != nil {
			panic(fmt.Sprintf("migration %q has no numeric version prefix", name))
		}
		content, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			panic(err)
		}
		sum := sha256Sum(content)
		ms = append(ms, Migration{Version: version, Name: name, SQLText: string(content), Checksum: sum})
	}
	return ms
}

func sha256Sum(b []byte) string {
	// sha256 over the exact migration bytes; a change to a committed file is a
	// checksum mismatch, never silently re-applied.
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Migrate applies every unapplied builtin migration in one transaction and
// seeds the singleton instance row. It is idempotent and refuses to touch a
// database whose applied checksums do not match the binary.
func Migrate(db *sql.DB) (int, error) {
	version, err := Apply(db, builtinMigrations())
	if err != nil {
		return 0, err
	}
	if _, err := db.Exec(
		"INSERT OR IGNORE INTO instance_state(singleton,generation,report_timezone) VALUES(1,?,'UTC')",
		NewUUID()); err != nil {
		return 0, fmt.Errorf("seed instance_state: %w", err)
	}
	return version, nil
}

// Apply runs the given migrations transactionally and records checksums. The
// list is an argument so tests can exercise failure rollback; production uses
// builtinMigrations via Migrate.
func Apply(db *sql.DB, ms []Migration) (int, error) {
	if len(ms) == 0 {
		return 0, errors.New("no migrations to apply")
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	applied, err := appliedMigrations(tx)
	if err != nil {
		return 0, err
	}
	if len(applied) > len(ms) {
		return 0, fmt.Errorf("database schema version %d is newer than binary (have %d migrations)",
			applied[len(applied)-1].Version, len(ms))
	}
	for i, a := range applied {
		if a.Version != ms[i].Version || a.Checksum != ms[i].Checksum {
			return 0, fmt.Errorf("migration %d checksum mismatch: have %q want %q",
				a.Version, a.Checksum, ms[i].Checksum)
		}
	}
	for _, m := range ms[len(applied):] {
		for _, stmt := range splitStatements(m.SQLText) {
			if _, err := tx.Exec(stmt); err != nil {
				return 0, fmt.Errorf("migration %d (%s): %w", m.Version, m.Name, err)
			}
		}
		if _, err := tx.Exec(
			"INSERT INTO schema_migrations(version,checksum,applied_at_us) VALUES(?,?,?)",
			m.Version, m.Checksum, time.Now().UnixMicro()); err != nil {
			return 0, fmt.Errorf("record migration %d: %w", m.Version, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return ms[len(ms)-1].Version, nil
}

// RequireCurrentSchema fails when the database is not exactly at the builtin
// migration level, so `serve` never runs against an unknown schema.
func RequireCurrentSchema(db *sql.DB) error {
	ms := builtinMigrations()
	applied, err := appliedMigrations(db)
	if err != nil {
		return err
	}
	if len(applied) != len(ms) {
		return fmt.Errorf("schema not current: applied %d of %d migrations; run healthbridge migrate",
			len(applied), len(ms))
	}
	for i, a := range applied {
		if a.Version != ms[i].Version || a.Checksum != ms[i].Checksum {
			return fmt.Errorf("migration %d checksum mismatch: have %q want %q",
				a.Version, a.Checksum, ms[i].Checksum)
		}
	}
	return nil
}

type appliedRow struct{ Version int; Checksum string }

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

func appliedMigrations(q querier) ([]appliedRow, error) {
	var exists int
	if err := q.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, nil // fresh database
	}
	rows, err := q.Query("SELECT version, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appliedRow
	for rows.Next() {
		var r appliedRow
		if err := rows.Scan(&r.Version, &r.Checksum); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// splitStatements strips comment lines and splits on top-level semicolons.
// The migration files contain no semicolons inside string literals.
func splitStatements(text string) []string {
	var body []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		body = append(body, line)
	}
	var out []string
	for _, stmt := range strings.Split(strings.Join(body, "\n"), ";") {
		if s := strings.TrimSpace(stmt); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// EnableProfile flips a collection profile to enabled only when it is already
// S01-verified and has an evidence_ref. The DDL CHECK and partial unique index
// provide a second, independent guard against enabling unverified profiles or
// two enabled profiles for one metric.
func EnableProfile(db *sql.DB, id string) error {
	var verified int
	var evidence sql.NullString
	err := db.QueryRow("SELECT verified, evidence_ref FROM collection_profiles WHERE id=?", id).
		Scan(&verified, &evidence)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("profile %q not found", id)
	}
	if err != nil {
		return err
	}
	if verified != 1 {
		return fmt.Errorf("profile %q is not S01-verified; enabling is blocked", id)
	}
	if !evidence.Valid || evidence.String == "" {
		return fmt.Errorf("profile %q has no evidence_ref; enabling is blocked", id)
	}
	res, err := db.Exec("UPDATE collection_profiles SET enabled=1 WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("profile %q could not be enabled", id)
	}
	return nil
}

// DisableProfile turns a profile off; it does not delete any stored data.
func DisableProfile(db *sql.DB, id string) error {
	res, err := db.Exec("UPDATE collection_profiles SET enabled=0 WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("profile %q not found", id)
	}
	return nil
}

// NewUUID returns a random RFC 4122 version-4 UUID, used for instance
// generation and publisher epochs. No external dependency.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
