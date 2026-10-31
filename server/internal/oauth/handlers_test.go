package oauth_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/atongrun/health-bridge/server/internal/oauth"
	"github.com/atongrun/health-bridge/server/internal/store"
	"github.com/ory/fosite"
)

const issuer = "https://health.example.com"

type henv struct {
	srv     *httptest.Server
	db      *sql.DB
	handler *oauth.Handler
}

func newHEnv(t *testing.T) *henv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "oauth-http.db")
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
	handler := &oauth.Handler{
		Store:    st,
		Provider: oauth.NewProvider(st, []byte(strings.Repeat("k", 32)), issuer),
		DB:       db,
		Issuer:   issuer,
		Enabled:  true,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/authorize", handler.Authorize)
	mux.HandleFunc("/oauth/token", handler.Token)
	mux.HandleFunc("/oauth/revoke", handler.Revoke)
	mux.HandleFunc("/oauth/continue", handler.Continue)
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		handler.Discovery(w, r, "authorization-server")
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		handler.Discovery(w, r, "protected-resource")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &henv{srv: srv, db: db, handler: handler}
}

// startAuthorize performs the authorize request and returns the pending id,
// the binding cookie value and its hash (for CSRF derivation).
func startAuthorize(t *testing.T, e *henv, state string) (pendingID, cookieValue, cookieHash string) {
	t.Helper()
	q := url.Values{}
	q.Set("client_id", testClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", testRedirect)
	q.Set("scope", "health:summary")
	q.Set("state", state)
	q.Set("code_challenge", challenge(testVerifier))
	q.Set("code_challenge_method", "S256")
	q.Set("resource", issuer+"/mcp")
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/oauth/authorize?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("authorize status = %d", resp.StatusCode)
	}
	var body struct {
		RequestID string `json:"request_id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	for _, c := range resp.Cookies() {
		if c.Name == "hb_oauth_binding" {
			cookieValue = c.Value
		}
	}
	if body.RequestID == "" || cookieValue == "" {
		t.Fatalf("authorize response incomplete: %+v", body)
	}
	return body.RequestID, cookieValue, hashOf(cookieValue)
}

func hashOf(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func continueAuthorize(t *testing.T, e *henv, pendingID, cookieValue, cookieHash, origin string) (*http.Response, map[string]any) {
	t.Helper()
	form := url.Values{}
	form.Set("request_id", pendingID)
	form.Set("csrf", cookieHash[:32])
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/oauth/continue", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "hb_oauth_binding", Value: cookieValue})
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	return resp, body
}

func exchangeCode(t *testing.T, e *henv, code string) (int, map[string]any) {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", testRedirect)
	form.Set("code_verifier", testVerifier)
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	return doRequest(t, req)
}

func doRequest(t *testing.T, req *http.Request) (int, map[string]any) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

// runConsent drives the whole browser+CLI consent flow and returns the code.
func runConsent(t *testing.T, e *henv, state string) string {
	t.Helper()
	pendingID, cookieValue, cookieHash := startAuthorize(t, e, state)
	if err := e.handler.Approve(t.Context(), pendingID, true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	resp, body := continueAuthorize(t, e, pendingID, cookieValue, cookieHash, issuer)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("continue status = %d body=%v", resp.StatusCode, body)
	}
	loc := resp.Header.Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in redirect %q", loc)
	}
	if u.Query().Get("state") != state {
		t.Fatalf("state mismatch: %q", loc)
	}
	return code
}

func TestFullConsentFlowToToken(t *testing.T) {
	e := newHEnv(t)
	code := runConsent(t, e, "state-fullflow-0123456789abcdef")
	status, body := exchangeCode(t, e, code)
	if status != http.StatusOK {
		t.Fatalf("token status = %d body=%v", status, body)
	}
	if body["access_token"] == nil || body["refresh_token"] == nil {
		t.Fatalf("missing tokens: %v", body)
	}
}

func TestContinueWithoutApprovalRefused(t *testing.T) {
	e := newHEnv(t)
	pendingID, cookieValue, cookieHash := startAuthorize(t, e, "state-noapprove-0123456789ab")
	resp, _ := continueAuthorize(t, e, pendingID, cookieValue, cookieHash, issuer)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("unapproved continue status = %d, want 409", resp.StatusCode)
	}
}

func TestCrossBrowserApprovalRejected(t *testing.T) {
	e := newHEnv(t)
	// Browser A starts the request...
	pendingID, _, _ := startAuthorize(t, e, "state-cross-0123456789abcdef")
	if err := e.handler.Approve(t.Context(), pendingID, true); err != nil {
		t.Fatal(err)
	}
	// ...but browser B (different binding cookie) tries to consume it.
	form := url.Values{}
	form.Set("request_id", pendingID)
	form.Set("csrf", strings.Repeat("x", 32))
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/oauth/continue", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "hb_oauth_binding", Value: strings.Repeat("b", 64)})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-browser continue status = %d, want 403", resp.StatusCode)
	}
}

func TestCSRFAndOriginChecks(t *testing.T) {
	e := newHEnv(t)
	pendingID, cookieValue, cookieHash := startAuthorize(t, e, "state-csrf-0123456789abcdef")
	if err := e.handler.Approve(t.Context(), pendingID, true); err != nil {
		t.Fatal(err)
	}
	// Wrong origin.
	resp, _ := continueAuthorize(t, e, pendingID, cookieValue, cookieHash, "https://evil.example")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin continue status = %d, want 403", resp.StatusCode)
	}
	// Wrong CSRF token.
	form := url.Values{}
	form.Set("request_id", pendingID)
	form.Set("csrf", strings.Repeat("z", 32))
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/oauth/continue", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "hb_oauth_binding", Value: cookieValue})
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("bad csrf status = %d, want 403", resp2.StatusCode)
	}
}

func TestConcurrentCodeExchangeOnlyOneWins(t *testing.T) {
	e := newHEnv(t)
	code := runConsent(t, e, "state-concurrent-0123456789a")
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _ := exchangeCode(t, e, code)
			results <- status
		}()
	}
	wg.Wait()
	close(results)
	okCount := 0
	for status := range results {
		if status == http.StatusOK {
			okCount++
		}
	}
	if okCount != 1 {
		t.Fatalf("concurrent exchange produced %d successes, want exactly 1", okCount)
	}
}

func TestConsentRequestNotReusableAfterConsumption(t *testing.T) {
	e := newHEnv(t)
	pendingID, cookieValue, cookieHash := startAuthorize(t, e, "state-reuse-0123456789abcde")
	if err := e.handler.Approve(t.Context(), pendingID, true); err != nil {
		t.Fatal(err)
	}
	resp, _ := continueAuthorize(t, e, pendingID, cookieValue, cookieHash, issuer)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("first continue status = %d", resp.StatusCode)
	}
	// Same pending id cannot be consumed twice.
	resp2, body2 := continueAuthorize(t, e, pendingID, cookieValue, cookieHash, issuer)
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second continue status = %d body=%v, want 409", resp2.StatusCode, body2)
	}
}

func TestDiscoveryDocuments(t *testing.T) {
	e := newHEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/.well-known/oauth-authorization-server", nil)
	status, body := doRequest(t, req)
	if status != http.StatusOK {
		t.Fatalf("as metadata status = %d", status)
	}
	if body["grant_types_supported"].([]any)[0] != "authorization_code" {
		t.Fatalf("unexpected grants: %v", body["grant_types_supported"])
	}
	if body["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Fatalf("unexpected pkce methods: %v", body["code_challenge_methods_supported"])
	}
	req2, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/.well-known/oauth-protected-resource/mcp", nil)
	status2, body2 := doRequest(t, req2)
	if status2 != http.StatusOK || body2["resource"] != issuer+"/mcp" {
		t.Fatalf("pr metadata: %d %v", status2, body2)
	}
}

func TestAuthorizeRequiresCanonicalResource(t *testing.T) {
	e := newHEnv(t)
	q := url.Values{}
	q.Set("client_id", testClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", testRedirect)
	q.Set("scope", "health:summary")
	q.Set("state", "state-res-0123456789abcdefghij")
	q.Set("code_challenge", challenge(testVerifier))
	q.Set("code_challenge_method", "S256")
	q.Set("resource", "https://other.example.com/mcp")
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/oauth/authorize?"+q.Encode(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong resource status = %d, want 400", resp.StatusCode)
	}
}

func TestDisabledOAuth404(t *testing.T) {
	e := newHEnv(t)
	e.handler.Enabled = false
	q := url.Values{}
	q.Set("client_id", testClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", testRedirect)
	q.Set("scope", "health:summary")
	q.Set("state", "state-off-0123456789abcdefghijk")
	q.Set("code_challenge", challenge(testVerifier))
	q.Set("code_challenge_method", "S256")
	q.Set("resource", issuer+"/mcp")
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+"/oauth/authorize?"+q.Encode(), nil)
	status, _ := doRequest(t, req)
	if status != http.StatusNotFound {
		t.Fatalf("disabled oauth status = %d, want 404", status)
	}
}

func TestOAuthTokenIntrospectionViaFosite(t *testing.T) {
	e := newHEnv(t)
	code := runConsent(t, e, "state-introspect-0123456789ab")
	status, body := exchangeCode(t, e, code)
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, body)
	}
	access := body["access_token"].(string)
	if _, _, err := e.handler.Provider.IntrospectToken(t.Context(), access, fosite.AccessToken, &fosite.DefaultSession{Subject: "owner"}); err != nil {
		t.Fatalf("introspection: %v", err)
	}
	// Revoke and confirm immediate invalidity.
	form := url.Values{"token": {access}}
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/oauth/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(testClientID, testSecret)
	if status, _ := doRequest(t, req); status != http.StatusOK {
		t.Fatalf("revoke status = %d", status)
	}
	if _, _, err := e.handler.Provider.IntrospectToken(t.Context(), access, fosite.AccessToken, &fosite.DefaultSession{Subject: "owner"}); err == nil {
		t.Fatal("token active after revocation")
	}
}
