// Package oauth wires Ory Fosite (limit: authorize-code + refresh + PKCE +
// revocation) to SQLite. It implements the Fosite storage interfaces with a
// versioned JSON payload (no gob, no arbitrary interface serialization) and a
// single serialized writer transaction shared across one token exchange.
package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ory/fosite"
	"github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
	"golang.org/x/crypto/bcrypt"
)

// Interface assertions: compile-time proof the adapter matches Fosite.
var (
	_ fosite.ClientManager           = (*Store)(nil)
	_ oauth2.AuthorizeCodeStorage    = (*Store)(nil)
	_ oauth2.AccessTokenStorage      = (*Store)(nil)
	_ oauth2.RefreshTokenStorage     = (*Store)(nil)
	_ oauth2.TokenRevocationStorage  = (*Store)(nil)
	_ pkce.PKCERequestStorage        = (*Store)(nil)
	_ interface {
		BeginTX(context.Context) (context.Context, error)
		Commit(context.Context) error
		Rollback(context.Context) error
	} = (*Store)(nil)
)

// Store persists Fosite sessions in the oauth_* tables of the main database.
type Store struct {
	DB  *sql.DB // serialized writer connection
	Now func() time.Time
}

const payloadVersion = 1

// payloadDTO is the versioned, allowlisted request/session representation.
type payloadDTO struct {
	V                 int                 `json:"v"`
	ID                string              `json:"id"`
	RequestedAt       time.Time           `json:"requested_at"`
	ClientID          string              `json:"client_id"`
	RequestedScope    []string            `json:"requested_scope"`
	GrantedScope      []string            `json:"granted_scope"`
	RequestedAudience []string            `json:"requested_audience"`
	GrantedAudience   []string            `json:"granted_audience"`
	Form              map[string][]string `json:"form"`
	Subject           string              `json:"subject"`
	ExpiresAt         map[string]time.Time `json:"expires_at"`
}

// allowlisted form keys only; the rest of the request form is discarded.
var allowedFormKeys = map[string]bool{
	"code_challenge":        true,
	"code_challenge_method": true,
	"resource":              true,
	"redirect_uri":          true,
	"state":                 true,
	"scope":                 true,
}

func encodePayload(r fosite.Requester) ([]byte, error) {
	dto := payloadDTO{
		V:                 payloadVersion,
		ID:                r.GetID(),
		RequestedAt:       r.GetRequestedAt(),
		RequestedScope:    r.GetRequestedScopes(),
		GrantedScope:      r.GetGrantedScopes(),
		RequestedAudience: r.GetRequestedAudience(),
		GrantedAudience:   r.GetGrantedAudience(),
		Form:              map[string][]string{},
	}
	if c := r.GetClient(); c != nil {
		dto.ClientID = c.GetID()
	}
	if s := r.GetSession(); s != nil {
		dto.Subject = s.GetSubject()
		dto.ExpiresAt = map[string]time.Time{}
		for _, tt := range []fosite.TokenType{fosite.AuthorizeCode, fosite.AccessToken, fosite.RefreshToken} {
			if exp := s.GetExpiresAt(tt); !exp.IsZero() {
				dto.ExpiresAt[string(tt)] = exp
			}
		}
	}
	for key, vals := range r.GetRequestForm() {
		if allowedFormKeys[key] {
			dto.Form[key] = append([]string(nil), vals...)
		}
	}
	return json.Marshal(dto)
}

func (s *Store) decodePayload(ctx context.Context, raw []byte) (fosite.Requester, error) {
	var dto payloadDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	if dto.V != payloadVersion {
		return nil, fmt.Errorf("unsupported payload version %d", dto.V)
	}
	client, err := s.GetClient(ctx, dto.ClientID)
	if err != nil {
		return nil, err
	}
	req := fosite.NewRequest()
	req.ID = dto.ID
	req.RequestedAt = dto.RequestedAt
	req.Client = client
	req.RequestedScope = dto.RequestedScope
	req.GrantedScope = dto.GrantedScope
	req.RequestedAudience = dto.RequestedAudience
	req.GrantedAudience = dto.GrantedAudience
	req.Form = url.Values{}
	for k, v := range dto.Form {
		req.Form[k] = append([]string(nil), v...)
	}
	sess := &fosite.DefaultSession{Subject: dto.Subject, ExpiresAt: map[fosite.TokenType]time.Time{}}
	for _, tt := range []fosite.TokenType{fosite.AuthorizeCode, fosite.AccessToken, fosite.RefreshToken} {
		if exp, ok := dto.ExpiresAt[string(tt)]; ok {
			sess.ExpiresAt[tt] = exp
		}
	}
	req.Session = sess
	return req, nil
}

// --- transaction handling ---

type txKey struct{}

type txState struct {
	tx    *sql.Tx
	depth int
}

// BeginTX starts the outer serialized transaction, or increments nesting.
func (s *Store) BeginTX(ctx context.Context) (context.Context, error) {
	if st, ok := ctx.Value(txKey{}).(*txState); ok {
		st.depth++
		return ctx, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, txKey{}, &txState{tx: tx, depth: 1}), nil
}

// Commit commits only when the outermost scope ends.
func (s *Store) Commit(ctx context.Context) error {
	st, ok := ctx.Value(txKey{}).(*txState)
	if !ok {
		return nil
	}
	st.depth--
	if st.depth > 0 {
		return nil
	}
	return st.tx.Commit()
}

// Rollback rolls back only when the outermost scope ends.
func (s *Store) Rollback(ctx context.Context) error {
	st, ok := ctx.Value(txKey{}).(*txState)
	if !ok {
		return nil
	}
	st.depth--
	if st.depth > 0 {
		return nil
	}
	return st.tx.Rollback()
}

type queryer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func (s *Store) q(ctx context.Context) queryer {
	if st, ok := ctx.Value(txKey{}).(*txState); ok && st.tx != nil {
		return st.tx
	}
	return s.DB
}

// --- client manager (pre-registered clients only) ---

// ClientSpec is a pre-registered client's allowlisted metadata.
type ClientSpec struct {
	RedirectURIs           []string `json:"redirect_uris"`
	GrantTypes             []string `json:"grant_types"`
	ResponseTypes          []string `json:"response_types"`
	Scopes                 []string `json:"scopes"`
	Audience               []string `json:"audience"`
	Public                 bool     `json:"public"`
	SecretHash             string   `json:"secret_hash,omitempty"`
	TokenEndpointAuthMethod string  `json:"token_endpoint_auth_method"`
}

// RegisterClient stores a pre-registered client and returns its id.
func (s *Store) RegisterClient(id, secret string, dto ClientSpec) error {
	if len(dto.RedirectURIs) == 0 {
		return errors.New("at least one redirect URI is required")
	}
	for _, u := range dto.RedirectURIs {
		parsed, err := url.Parse(u)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Fragment != "" || parsed.RawQuery != "" || parsed.User != nil || strings.Contains(u, "*") {
			return fmt.Errorf("redirect URI %q must be an exact HTTPS URL without fragment", u)
		}
	}
	if dto.Public {
		if dto.TokenEndpointAuthMethod == "" {
			dto.TokenEndpointAuthMethod = "none"
		}
		if dto.TokenEndpointAuthMethod != "none" {
			return errors.New("public clients must use token_endpoint_auth_method=none")
		}
	} else {
		if secret == "" {
			return errors.New("confidential clients require a secret")
		}
		if dto.TokenEndpointAuthMethod == "" {
			dto.TokenEndpointAuthMethod = "client_secret_basic"
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		dto.SecretHash = string(hash)
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO oauth_clients(client_id,secret_hash,metadata_json,active) VALUES(?,?,?,1)`,
		id, nullIfEmpty(dto.SecretHash), string(raw))
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// GetClient implements fosite.ClientManager.
func (s *Store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	var raw string
	var active int
	err := s.q(ctx).QueryRowContext(ctx, "SELECT metadata_json, active FROM oauth_clients WHERE client_id=?", id).
		Scan(&raw, &active)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && active != 1) {
		return nil, fosite.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var dto ClientSpec
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		return nil, err
	}
	return &fosite.DefaultOpenIDConnectClient{
		DefaultClient: &fosite.DefaultClient{
			ID:            id,
			Secret:        []byte(dto.SecretHash),
			RedirectURIs:  dto.RedirectURIs,
			GrantTypes:    dto.GrantTypes,
			ResponseTypes: dto.ResponseTypes,
			Scopes:        dto.Scopes,
			Audience:      dto.Audience,
			Public:        dto.Public,
		},
		TokenEndpointAuthMethod: dto.TokenEndpointAuthMethod,
	}, nil
}

// ClientAssertionJWTValid rejects private_key_jwt (not enabled).
func (s *Store) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	return fosite.ErrInvalidGrant.WithHint("client assertions are not supported")
}

// SetClientAssertionJWT is unused because assertions are rejected above.
func (s *Store) SetClientAssertionJWT(ctx context.Context, jti string, exp time.Time) error {
	return nil
}

// --- session storage ---

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) createSession(ctx context.Context, kind, signature, requestID string, requester fosite.Requester, expires time.Time) error {
	raw, err := encodePayload(requester)
	if err != nil {
		return err
	}
	// oauth_sessions.grant_id is a foreign key; the grant row doubles as the
	// request family anchor for revocation and 30-day absolute expiry.
	if _, err := s.q(ctx).ExecContext(ctx, `INSERT OR IGNORE INTO oauth_grants(id,client_id,policy_json,created_at_us)
		VALUES(?,?, '{}', ?)`, requestID, requester.GetClient().GetID(), s.now().UnixMicro()); err != nil {
		return err
	}
	_, err = s.q(ctx).ExecContext(ctx, `INSERT OR REPLACE INTO oauth_sessions(kind,signature,request_id,grant_id,payload_json,expires_at_us,active)
		VALUES(?,?,?,?,?,?,1)`, kind, signature, requestID, requestID, string(raw), expires.UnixMicro())
	return err
}

func (s *Store) loadSession(ctx context.Context, kind, signature string) (fosite.Requester, error) {
	var raw string
	err := s.q(ctx).QueryRowContext(ctx, "SELECT payload_json FROM oauth_sessions WHERE kind=? AND signature=?",
		kind, signature).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fosite.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.decodePayload(ctx, []byte(raw))
}

// CreateAuthorizeCodeSession implements oauth2.AuthorizeCodeStorage.
func (s *Store) CreateAuthorizeCodeSession(ctx context.Context, code string, requester fosite.Requester) error {
	exp := s.now().Add(authorizeCodeLifetime)
	return s.createSession(ctx, "code", code, requester.GetID(), requester, exp)
}

// GetAuthorizeCodeSession returns ErrInvalidatedAuthorizeCode with the original
// requester when a code was already used.
func (s *Store) GetAuthorizeCodeSession(ctx context.Context, code string, session fosite.Session) (fosite.Requester, error) {
	req, err := s.loadSession(ctx, "code", code)
	if err != nil {
		return nil, err
	}
	if !s.sessionActive(ctx, "code", code) {
		return req, fosite.ErrInvalidatedAuthorizeCode
	}
	return req, nil
}

func (s *Store) sessionActive(ctx context.Context, kind, signature string) bool {
	var active int
	var expiresUs int64
	if err := s.q(ctx).QueryRowContext(ctx, "SELECT active, expires_at_us FROM oauth_sessions WHERE kind=? AND signature=?",
		kind, signature).Scan(&active, &expiresUs); err != nil {
		return false
	}
	return active == 1 && s.now().UnixMicro() <= expiresUs
}

// InvalidateAuthorizeCodeSession marks a code used.
func (s *Store) InvalidateAuthorizeCodeSession(ctx context.Context, code string) error {
	_, err := s.q(ctx).ExecContext(ctx, "UPDATE oauth_sessions SET active=0 WHERE kind='code' AND signature=?", code)
	return err
}

// CreatePKCERequestSession implements pkce.PKCERequestStorage.
func (s *Store) CreatePKCERequestSession(ctx context.Context, signature string, requester fosite.Requester) error {
	return s.createSession(ctx, "pkce", signature, requester.GetID(), requester, s.now().Add(authorizeCodeLifetime))
}

// GetPKCERequestSession loads the PKCE request bound to the code.
func (s *Store) GetPKCERequestSession(ctx context.Context, signature string, session fosite.Session) (fosite.Requester, error) {
	return s.loadSession(ctx, "pkce", signature)
}

// DeletePKCERequestSession removes the PKCE request after exchange.
func (s *Store) DeletePKCERequestSession(ctx context.Context, signature string) error {
	_, err := s.q(ctx).ExecContext(ctx, "DELETE FROM oauth_sessions WHERE kind='pkce' AND signature=?", signature)
	return err
}

// CreateAccessTokenSession implements oauth2.AccessTokenStorage.
func (s *Store) CreateAccessTokenSession(ctx context.Context, signature string, request fosite.Requester) error {
	return s.createSession(ctx, "access", signature, request.GetID(), request, s.now().Add(accessTokenLifetime))
}

// GetAccessTokenSession loads an access token; inactive tokens are invalid.
func (s *Store) GetAccessTokenSession(ctx context.Context, signature string, session fosite.Session) (fosite.Requester, error) {
	req, err := s.loadSession(ctx, "access", signature)
	if err != nil {
		return nil, err
	}
	if !s.sessionActive(ctx, "access", signature) {
		return nil, fosite.ErrInactiveToken
	}
	return req, nil
}

// DeleteAccessTokenSession removes an access token.
func (s *Store) DeleteAccessTokenSession(ctx context.Context, signature string) error {
	_, err := s.q(ctx).ExecContext(ctx, "UPDATE oauth_sessions SET active=0 WHERE kind='access' AND signature=?", signature)
	return err
}

// CreateRefreshTokenSession implements oauth2.RefreshTokenStorage.
func (s *Store) CreateRefreshTokenSession(ctx context.Context, signature string, accessSignature string, request fosite.Requester) error {
	return s.createSession(ctx, "refresh", signature, request.GetID(), request, s.now().Add(refreshTokenLifetime))
}

// GetRefreshTokenSession returns ErrInactiveToken for a reused/rotated token,
// which makes Fosite revoke the whole grant.
func (s *Store) GetRefreshTokenSession(ctx context.Context, signature string, session fosite.Session) (fosite.Requester, error) {
	req, err := s.loadSession(ctx, "refresh", signature)
	if err != nil {
		return nil, err
	}
	if !s.sessionActive(ctx, "refresh", signature) {
		return nil, fosite.ErrInactiveToken
	}
	return req, nil
}

// DeleteRefreshTokenSession removes a refresh token.
func (s *Store) DeleteRefreshTokenSession(ctx context.Context, signature string) error {
	_, err := s.q(ctx).ExecContext(ctx, "UPDATE oauth_sessions SET active=0 WHERE kind='refresh' AND signature=?", signature)
	return err
}

// RotateRefreshToken deactivates every refresh token of the family. Fosite
// calls it before inserting the replacement, so the presented token dies here;
// access tokens are left to their own expiry (the grant's absolute 30-day
// limit is enforced by refresh expiry and owner revocation).
func (s *Store) RotateRefreshToken(ctx context.Context, requestID string, refreshTokenSignature string) error {
	_, err := s.q(ctx).ExecContext(ctx,
		"UPDATE oauth_sessions SET active=0 WHERE kind='refresh' AND request_id=?", requestID)
	return err
}

// RevokeRefreshToken invalidates every refresh token of a grant.
func (s *Store) RevokeRefreshToken(ctx context.Context, requestID string) error {
	_, err := s.q(ctx).ExecContext(ctx, "UPDATE oauth_sessions SET active=0 WHERE kind='refresh' AND request_id=?", requestID)
	return err
}

// RevokeAccessToken invalidates every access token of a grant.
func (s *Store) RevokeAccessToken(ctx context.Context, requestID string) error {
	_, err := s.q(ctx).ExecContext(ctx, "UPDATE oauth_sessions SET active=0 WHERE kind='access' AND request_id=?", requestID)
	return err
}

// Token lifetimes fixed by docs/oauth.md.
const (
	authorizeCodeLifetime = 60 * time.Second
	accessTokenLifetime   = 15 * time.Minute
	refreshTokenLifetime  = 30 * 24 * time.Hour
)
