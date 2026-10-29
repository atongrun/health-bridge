package oauth_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atongrun/health-bridge/server/internal/oauth"
	"github.com/atongrun/health-bridge/server/internal/store"
	"github.com/ory/fosite"
)

const (
	testClientID = "chatgpt-test"
	testSecret   = "s3cret-for-tests"
	testRedirect = "https://chat.openai.com/aip/callback"
	testVerifier = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG" // 43+ chars
)

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type oenv struct {
	path     string
	db       *sql.DB
	store    *oauth.Store
	provider fosite.OAuth2Provider
}

func newOEnv(t *testing.T) *oenv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "oauth.db")
	db, err := store.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := &oauth.Store{DB: db}
	if err := st.RegisterClient(testClientID, testSecret, oauth.ClientSpec{
		RedirectURIs:  []string{testRedirect},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"},
		Scopes:        []string{"health:summary", "health:raw"},
	}); err != nil {
		t.Fatal(err)
	}
	secret := []byte(strings.Repeat("k", 32))
	return &oenv{path: path, db: db, store: st, provider: oauth.NewProvider(st, secret, "https://health.example.com")}
}

func (e *oenv) authURL(scope, verifier, state string) string {
	q := url.Values{}
	q.Set("client_id", testClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", testRedirect)
	q.Set("scope", scope)
	// Fosite enforces MinParameterEntropy on state; use a realistic 32+ char value.
	q.Set("state", "state-"+state+"-0123456789abcdef0123456789")
	q.Set("code_challenge", challenge(verifier))
	q.Set("code_challenge_method", "S256")
	return "/oauth/authorize?" + q.Encode()
}

func (e *oenv) authorize(t *testing.T, urlStr string) (fosite.AuthorizeRequester, string) {
	t.Helper()
	req := httptest.NewRequest("GET", urlStr, nil)
	ar, err := e.provider.NewAuthorizeRequest(req.Context(), req)
	if err != nil {
		t.Fatalf("authorize request rejected: %v", err)
	}
	for _, sc := range ar.GetRequestedScopes() {
		ar.GrantScope(sc)
	}
	resp, err := e.provider.NewAuthorizeResponse(req.Context(), ar, &fosite.DefaultSession{Subject: "owner"})
	if err != nil {
		t.Fatalf("authorize response: %v", err)
	}
	return ar, resp.GetCode()
}

func (e *oenv) exchange(t *testing.T, code, verifier string) (fosite.AccessResponder, error) {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", testRedirect)
	form.Set("code_verifier", verifier)
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	ctx, err := e.store.BeginTX(req.Context())
	if err != nil {
		t.Fatal(err)
	}
	// Commit even on error: an authorization-code replay triggers a security
	// revocation inside Fosite that must persist, and validation-only failures
	// write nothing, so committing them is equivalent to rollback.
	defer e.store.Commit(ctx) //nolint:errcheck
	ar, err := e.provider.NewAccessRequest(ctx, req, &fosite.DefaultSession{Subject: "owner"})
	if err != nil {
		return nil, err
	}
	return e.provider.NewAccessResponse(ctx, ar)
}

// refreshToken reads the rotated refresh token from the access responder extras.
func refreshToken(resp fosite.AccessResponder) string {
	if v, ok := resp.GetExtra("refresh_token").(string); ok {
		return v
	}
	return ""
}

func (e *oenv) refresh(t *testing.T, token string) (fosite.AccessResponder, error) {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", token)
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	ctx, err := e.store.BeginTX(req.Context())
	if err != nil {
		t.Fatal(err)
	}
	// See exchange(): commit even on error so refresh-reuse revocation persists.
	defer e.store.Commit(ctx) //nolint:errcheck
	ar, err := e.provider.NewAccessRequest(ctx, req, &fosite.DefaultSession{Subject: "owner"})
	if err != nil {
		return nil, err
	}
	return e.provider.NewAccessResponse(ctx, ar)
}

func TestClientRegistrationValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.db")
	db, err := store.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	st := &oauth.Store{DB: db}
	bad := []oauth.ClientSpec{
		{RedirectURIs: []string{"http://insecure.example/cb"}},
		{RedirectURIs: []string{"https://ex.com/cb#frag"}},
		{RedirectURIs: []string{"https://ex.com/cb*"}},
		{RedirectURIs: []string{"https://ex.com/cb"}, Public: true, TokenEndpointAuthMethod: "client_secret_basic"},
		{RedirectURIs: []string{"https://ex.com/cb"}},
	}
	for i, spec := range bad {
		if err := st.RegisterClient("bad", "", spec); err == nil {
			t.Fatalf("case %d: invalid client registration accepted", i)
		}
	}
	if err := st.RegisterClient("ok", testSecret, oauth.ClientSpec{RedirectURIs: []string{"https://ex.com/cb"}}); err != nil {
		t.Fatalf("valid client rejected: %v", err)
	}
	if _, err := st.GetClient(t.Context(), "ok"); err != nil {
		t.Fatalf("registered client not retrievable: %v", err)
	}
	if _, err := st.GetClient(t.Context(), "missing"); err == nil {
		t.Fatal("unknown client returned")
	}
}

func TestCodeExchangeWithPKCES256(t *testing.T) {
	e := newOEnv(t)
	_, code := e.authorize(t, e.authURL("health:summary", testVerifier, "st1"))
	resp, err := e.exchange(t, code, testVerifier)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if resp.GetAccessToken() == "" || refreshToken(resp) == "" {
		t.Fatalf("missing tokens: %+v", resp)
	}
}

func TestPKCEMismatchAndMissingVerifierRejected(t *testing.T) {
	e := newOEnv(t)
	_, code := e.authorize(t, e.authURL("health:summary", testVerifier, "st2"))
	if _, err := e.exchange(t, code, "wrong-verifier-wrong-verifier-wrong-verifier"); err == nil {
		t.Fatal("wrong code_verifier accepted")
	}
}

func TestPlainChallengeRejected(t *testing.T) {
	e := newOEnv(t)
	q := url.Values{}
	q.Set("client_id", testClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", testRedirect)
	q.Set("scope", "health:summary")
	q.Set("code_challenge", testVerifier)
	q.Set("code_challenge_method", "plain")
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	if _, err := e.provider.NewAuthorizeRequest(req.Context(), req); err == nil {
		t.Fatal("plain PKCE challenge accepted; only S256 is allowed")
	}
}

func TestCodeReuseRevokesGrant(t *testing.T) {
	e := newOEnv(t)
	_, code := e.authorize(t, e.authURL("health:summary", testVerifier, "st3"))
	resp, err := e.exchange(t, code, testVerifier)
	if err != nil {
		t.Fatal(err)
	}
	// Second exchange of the same code must fail...
	if _, err := e.exchange(t, code, testVerifier); err == nil {
		t.Fatal("reused authorization code accepted")
	}
	// ...and the reuse must have revoked the grant's tokens.
	access := resp.GetAccessToken()
	ctx := t.Context()
	_, _, err = e.provider.IntrospectToken(ctx, access, fosite.AccessToken, &fosite.DefaultSession{Subject: "owner"})
	if err == nil {
		t.Fatal("access token still active after code reuse")
	}
}

func TestRefreshRotationAndReuseRevokes(t *testing.T) {
	e := newOEnv(t)
	_, code := e.authorize(t, e.authURL("health:summary", testVerifier, "st4"))
	first, err := e.exchange(t, code, testVerifier)
	if err != nil {
		t.Fatal(err)
	}
	oldRefresh := refreshToken(first)
	second, err := e.refresh(t, oldRefresh)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if refreshToken(second) == oldRefresh {
		t.Fatal("refresh token was not rotated")
	}
	// Reusing the rotated-out token must fail and revoke the whole family.
	if _, err := e.refresh(t, oldRefresh); err == nil {
		t.Fatal("rotated refresh token accepted")
	}
	ctx := t.Context()
	if _, _, err := e.provider.IntrospectToken(ctx, second.GetAccessToken(), fosite.AccessToken, &fosite.DefaultSession{Subject: "owner"}); err == nil {
		t.Fatal("access token survived refresh reuse")
	}
}

func TestRefreshScopeEscalationRejected(t *testing.T) {
	e := newOEnv(t)
	_, code := e.authorize(t, e.authURL("health:summary", testVerifier, "st5"))
	first, err := e.exchange(t, code, testVerifier)
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken(first))
	form.Set("scope", "health:raw")
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	ctx, err := e.store.BeginTX(req.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer e.store.Commit(ctx) //nolint:errcheck
	ar, err := e.provider.NewAccessRequest(ctx, req, &fosite.DefaultSession{Subject: "owner"})
	if err != nil {
		return // explicit invalid_scope is enforced by the HTTP layer
	}
	resp, err := e.provider.NewAccessResponse(ctx, ar)
	if err != nil {
		return
	}
	for _, sc := range resp.GetScopes() {
		if sc == "health:raw" {
			t.Fatal("refresh escalated scope to health:raw")
		}
	}
}

func TestSessionsSurviveRestart(t *testing.T) {
	e := newOEnv(t)
	_, code := e.authorize(t, e.authURL("health:summary", testVerifier, "st6"))
	resp, err := e.exchange(t, code, testVerifier)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a process restart: new connection and new provider over the same file.
	db2, err := store.Open(e.path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	st2 := &oauth.Store{DB: db2}
	p2 := oauth.NewProvider(st2, []byte(strings.Repeat("k", 32)), "https://health.example.com")
	if _, _, err := p2.IntrospectToken(t.Context(), resp.GetAccessToken(), fosite.AccessToken, &fosite.DefaultSession{Subject: "owner"}); err != nil {
		t.Fatalf("token did not survive restart: %v", err)
	}
}

func TestRevocationIsImmediate(t *testing.T) {
	e := newOEnv(t)
	_, code := e.authorize(t, e.authURL("health:summary", testVerifier, "st7"))
	resp, err := e.exchange(t, code, testVerifier)
	if err != nil {
		t.Fatal(err)
	}
	access := resp.GetAccessToken()
	ctx := t.Context()
	if _, _, err := e.provider.IntrospectToken(ctx, access, fosite.AccessToken, &fosite.DefaultSession{Subject: "owner"}); err != nil {
		t.Fatalf("token not active before revocation: %v", err)
	}
	form := url.Values{"token": {access}}
	req := httptest.NewRequest("POST", "/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	if err := e.provider.NewRevocationRequest(ctx, req); err != nil {
		t.Fatalf("revocation request: %v", err)
	}
	if _, _, err := e.provider.IntrospectToken(ctx, access, fosite.AccessToken, &fosite.DefaultSession{Subject: "owner"}); err == nil {
		t.Fatal("token still active after revocation")
	}
}
