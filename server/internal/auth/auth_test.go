package auth

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/atongrun/health-bridge/server/internal/store"
)

func setup(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestTokenHashing(t *testing.T) {
	raw, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 32 {
		t.Fatalf("token too short: %d", len(raw))
	}
	if HashToken(raw) != hash {
		t.Fatal("HashToken is not deterministic")
	}
	if raw == hash {
		t.Fatal("token must not equal its own hash")
	}
}

func TestUploadCredentialRequiresPublisher(t *testing.T) {
	db := setup(t)
	_, _, err := CreateCredential(db, "upload", "{}", "{}", "", "", time.Hour, time.Now())
	if err == nil {
		t.Fatal("upload credential without publisher was accepted")
	}
}

func TestCredentialLifecycle(t *testing.T) {
	db := setup(t)
	now := time.Now()
	_, raw, err := CreateCredential(db, "query", `{"scope":"summary"}`, `{}`, "", "", time.Hour, now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	c, err := Authenticate(db, raw, now)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if c.Role != "query" {
		t.Fatalf("role = %q, want query", c.Role)
	}
	if _, err := Authenticate(db, "not-a-token", now); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
	if err := Revoke(db, raw, now); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := Authenticate(db, raw, now); err != ErrRevoked {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
	if err := Revoke(db, raw, now); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken on re-revoke, got %v", err)
	}
}

func TestExpiredCredential(t *testing.T) {
	db := setup(t)
	now := time.Now()
	_, raw, err := CreateCredential(db, "query", "{}", "{}", "", "", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Authenticate(db, raw, now.Add(2*time.Hour)); err != ErrExpired {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestSingleActivePublisher(t *testing.T) {
	db := setup(t)
	now := time.Now()
	epoch1, err := ReplacePublisher(db, "publisher-a", now)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	id, epoch, err := ActivePublisher(db)
	if err != nil || id != "publisher-a" || epoch != epoch1 {
		t.Fatalf("active publisher = %q/%q (%v), want publisher-a/%s", id, epoch, err, epoch1)
	}
	if _, err := ReplacePublisher(db, "publisher-b", now); err != nil {
		t.Fatalf("second replace: %v", err)
	}
	var activeCount int
	if err := db.QueryRow("SELECT count(*) FROM publishers WHERE active=1").Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 1 {
		t.Fatalf("active publisher count = %d, want 1", activeCount)
	}
}

func TestUploadCredentialBoundToPublisher(t *testing.T) {
	db := setup(t)
	now := time.Now()
	epoch, err := ReplacePublisher(db, "publisher-a", now)
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := CreateCredential(db, "upload", "{}", "{}", "publisher-a", epoch, time.Hour, now)
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	c, err := Authenticate(db, raw, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Role != "upload" || c.PublisherID.String != "publisher-a" || c.PublisherEpoch.String != epoch {
		t.Fatalf("upload credential not bound: %+v", c)
	}
}
