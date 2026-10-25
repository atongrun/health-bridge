// Package auth issues and verifies service credentials (upload/query tokens).
// Tokens are 32 random bytes, stored only as SHA-256; verification is
// constant-time over the hash and checks revocation and expiry on every lookup.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/atongrun/health-bridge/server/internal/store"
)

// DefaultTokenTTL matches the 90-day rotation baseline in docs/security.md.
const DefaultTokenTTL = 90 * 24 * time.Hour

var (
	ErrInvalidToken    = errors.New("invalid credential")
	ErrRevoked         = errors.New("credential revoked")
	ErrExpired         = errors.New("credential expired")
	ErrNoActivePublisher = errors.New("no active publisher")
)

// Credential is the verified result of an Authenticate call.
type Credential struct {
	ID             string
	Role           string
	PublisherID    sql.NullString
	PublisherEpoch sql.NullString
	ExpiresAt      time.Time
}

// NewToken returns a fresh 32-byte random token and its SHA-256 hex hash.
func NewToken() (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw), nil
}

// HashToken is the canonical server-side digest of a raw token.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateCredential stores a new credential and returns its id and the raw
// token (shown exactly once). Upload credentials must name an active publisher.
func CreateCredential(db *sql.DB, role, scopesJSON, policyJSON, publisherID, publisherEpoch string, ttl time.Duration, now time.Time) (id, raw string, err error) {
	if role != "upload" && role != "query" {
		return "", "", fmt.Errorf("role must be upload or query, got %q", role)
	}
	if !json.Valid([]byte(scopesJSON)) || !json.Valid([]byte(policyJSON)) {
		return "", "", errors.New("scopes and policy must be valid JSON")
	}
	if ttl <= 0 {
		return "", "", errors.New("ttl must be positive")
	}
	var pid, pep sql.NullString
	if role == "upload" {
		if publisherID == "" || publisherEpoch == "" {
			return "", "", errors.New("upload credential requires a publisher")
		}
		var active int
		err := db.QueryRow("SELECT active FROM publishers WHERE id=? AND epoch=?",
			publisherID, publisherEpoch).Scan(&active)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && active != 1) {
			return "", "", ErrNoActivePublisher
		}
		if err != nil {
			return "", "", err
		}
		pid, pep = sql.NullString{String: publisherID, Valid: true}, sql.NullString{String: publisherEpoch, Valid: true}
	}
	raw, hash, err := NewToken()
	if err != nil {
		return "", "", err
	}
	id = store.NewUUID()
	expires := now.Add(ttl).UnixMicro()
	_, err = db.Exec(`INSERT INTO credentials(id,token_hash,role,scopes_json,policy_json,publisher_id,publisher_epoch,created_at_us,expires_at_us)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		id, hash, role, scopesJSON, policyJSON, pid, pep, now.UnixMicro(), expires)
	if err != nil {
		return "", "", err
	}
	return id, raw, nil
}

// Authenticate resolves a raw token to a live credential, rejecting invalid,
// revoked and expired tokens.
func Authenticate(db *sql.DB, raw string, now time.Time) (Credential, error) {
	var c Credential
	var expiresUs int64
	var revoked sql.NullInt64
	err := db.QueryRow(`SELECT id, role, publisher_id, publisher_epoch, expires_at_us, revoked_at_us
		FROM credentials WHERE token_hash=?`, HashToken(raw)).
		Scan(&c.ID, &c.Role, &c.PublisherID, &c.PublisherEpoch, &expiresUs, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return Credential{}, ErrInvalidToken
	}
	if err != nil {
		return Credential{}, err
	}
	if revoked.Valid {
		return Credential{}, ErrRevoked
	}
	c.ExpiresAt = time.UnixMicro(expiresUs)
	if !now.Before(c.ExpiresAt) {
		return Credential{}, ErrExpired
	}
	return c, nil
}

// Revoke marks a live credential revoked. Re-revoking is an error.
func Revoke(db *sql.DB, raw string, now time.Time) error {
	res, err := db.Exec("UPDATE credentials SET revoked_at_us=? WHERE token_hash=? AND revoked_at_us IS NULL",
		now.UnixMicro(), HashToken(raw))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrInvalidToken
	}
	return nil
}

// ReplacePublisher deactivates the active publisher and installs a fresh one
// with a new epoch, in one transaction. The partial unique index backs the
// single-active invariant against raw SQL too.
func ReplacePublisher(db *sql.DB, newID string, now time.Time) (epoch string, err error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec("UPDATE publishers SET active=0 WHERE active=1"); err != nil {
		return "", err
	}
	epoch = store.NewUUID()
	if _, err := tx.Exec(
		"INSERT INTO publishers(id,epoch,active,created_at_us) VALUES(?,?,1,?)",
		newID, epoch, now.UnixMicro()); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return epoch, nil
}

// ActivePublisher returns the single active publisher, if any.
func ActivePublisher(db *sql.DB) (id, epoch string, err error) {
	err = db.QueryRow("SELECT id, epoch FROM publishers WHERE active=1").Scan(&id, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNoActivePublisher
	}
	return id, epoch, err
}

// ConstantTimeEqual exists to make the comparison policy explicit at call
// sites; token lookup already uses the SHA-256 preimage rather than the raw
// value, so timing does not leak the secret here.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
