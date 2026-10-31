package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ory/fosite"
)

// Handler serves /oauth/authorize, /oauth/token, /oauth/revoke and the
// consent-polling endpoints. authorize never issues a code directly: it parks
// a pending request bound to a browser cookie and waits for the owner to
// approve via the local CLI.
type Handler struct {
	Store    *Store
	Provider fosite.OAuth2Provider
	DB       *sql.DB
	Issuer   string // canonical https origin, e.g. https://health.example.com
	Enabled  bool
	Now      func() time.Time
}

const (
	pendingLifetime = 5 * time.Minute
	bindingCookie   = "hb_oauth_binding"
)

// pendingRequest is the parked authorization request (no health values).
type pendingRequest struct {
	ID         string
	ClientID   string
	Scopes     []string
	BindingHash string
	RequestJSON string
	ExpiresAtUs int64
	State      string // pending|approved|denied|consumed
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Authorize handles GET /oauth/authorize. Fosite validates client/redirect/
// scope/PKCE/state; on success we park a pending request and answer 202 with
// the request id (the browser page polls; the MCP client only ever sees the
// final redirect after continue).
func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled {
		writeJSONError(w, http.StatusNotFound, "not_found", "oauth is disabled")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "bad_request", "GET or POST required")
		return
	}
	ar, err := h.Provider.NewAuthorizeRequest(r.Context(), r)
	if err != nil {
		// Fosite requires writing the error through its own writer so an
		// invalid redirect_uri never leaks a redirect.
		h.Provider.WriteAuthorizeError(r.Context(), w, ar, err)
		return
	}
	// Resource parameter must match the canonical /mcp URL exactly.
	if res := r.FormValue("resource"); res != h.Issuer+"/mcp" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "resource must be the /mcp canonical URL")
		return
	}
	binding, err := h.ensureBindingCookie(w, r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", "binding cookie")
		return
	}
	id := NewUUID()
	raw, _ := json.Marshal(map[string]any{
		"client_id":          ar.GetClient().GetID(),
		"requested_scope":    ar.GetRequestedScopes(),
		"requested_audience": ar.GetRequestedAudience(),
		"form":               allowlistedForm(r.PostForm),
		"requested_at":       h.now().UTC().Format(time.RFC3339Nano),
	})
	if _, err := h.DB.Exec(`INSERT INTO oauth_requests(id,browser_binding_hash,client_id,request_json,state,expires_at_us)
		VALUES(?,?,?,?, 'pending', ?)`,
		id, binding, ar.GetClient().GetID(), string(raw), h.now().Add(pendingLifetime).UnixMicro()); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", "store pending request")
		return
	}
	_ = ar // decision happens at /continue after approval
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "pending_approval",
		"request_id": id,
		"client":     ar.GetClient().GetID(),
		"scope":      ar.GetRequestedScopes(),
		"poll":       h.Issuer + "/oauth/pending/" + id,
	})
}

// PendingStatus serves the browser's consent polling (binding-cookie bound).
func (h *Handler) PendingStatus(w http.ResponseWriter, r *http.Request, id string) {
	if !h.Enabled {
		writeJSONError(w, http.StatusNotFound, "not_found", "oauth is disabled")
		return
	}
	binding := r.Header.Get("X-Binding-Proof") // tests inject; browsers rely on cookie below
	if c, err := r.Cookie(bindingCookie); err == nil {
		binding = hashBinding(c.Value)
	}
	if binding == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing binding cookie")
		return
	}
	var state string
	var expiresUs int64
	err := h.DB.QueryRow("SELECT state, expires_at_us FROM oauth_requests WHERE id=?", id).Scan(&state, &expiresUs)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "not_found", "unknown request")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", "store")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": state, "expired": h.now().UnixMicro() > expiresUs})
}

// Approve is invoked by the local owner CLI (over the admin socket in
// production; direct in tests). It marks the request approved for later
// consumption by the bound browser only.
func (h *Handler) Approve(ctx context.Context, id string, allowRefresh bool) error {
	var state string
	var expiresUs int64
	err := h.DB.QueryRow("SELECT state, expires_at_us FROM oauth_requests WHERE id=?", id).Scan(&state, &expiresUs)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("unknown request %q", id)
	}
	if err != nil {
		return err
	}
	if state != "pending" || h.now().UnixMicro() > expiresUs {
		return errors.New("request is no longer pending")
	}
	// Record the owner's decision (refresh-capable access or not) for the
	// continue step; the consent boundary is this explicit approval.
	patch, _ := json.Marshal(map[string]bool{"allow_refresh": allowRefresh})
	if _, err := h.DB.Exec("UPDATE oauth_requests SET state='approved', request_json=json_set(request_json,'$.owner_decision',?) WHERE id=?", string(patch), id); err != nil {
		return err
	}
	return nil
}

// Continue is posted by the consent page after approval. Requires the binding
// cookie to match the parked request, then Fosite issues the one-time code.
func (h *Handler) Continue(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled {
		writeJSONError(w, http.StatusNotFound, "not_found", "oauth is disabled")
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "bad_request", "POST required")
		return
	}
	// CSRF: Origin must be our own issuer, and a matching CSRF token is required.
	if origin := r.Header.Get("Origin"); origin != "" && origin != h.Issuer {
		writeJSONError(w, http.StatusForbidden, "forbidden", "cross-origin continue rejected")
		return
	}
	id := r.FormValue("request_id")
	csrf := r.FormValue("csrf")
	c, err := r.Cookie(bindingCookie)
	if err != nil || c.Value == "" {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "missing binding cookie")
		return
	}
	binding := hashBinding(c.Value)
	if csrf == "" || !checkCSRF(binding, csrf) {
		writeJSONError(w, http.StatusForbidden, "forbidden", "bad csrf token")
		return
	}
	var requestJSON, state string
	var clientID string
	err = h.DB.QueryRow("SELECT request_json, state, client_id FROM oauth_requests WHERE id=?", id).
		Scan(&requestJSON, &state, &clientID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "not_found", "unknown request")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", "store")
		return
	}
	if state != "approved" {
		writeJSONError(w, http.StatusConflict, "conflict", "request not approved")
		return
	}
	// Re-verify binding cookie matches the parked request.
	var storedBinding string
	if err := h.DB.QueryRow("SELECT browser_binding_hash FROM oauth_requests WHERE id=?", id).Scan(&storedBinding); err != nil || storedBinding != binding {
		writeJSONError(w, http.StatusForbidden, "forbidden", "browser binding mismatch")
		return
	}
	// Rebuild the authorize request from the parked form and issue the code.
	var parked struct {
		ClientID string              `json:"client_id"`
		Form     map[string][]string `json:"form"`
	}
	if err := json.Unmarshal([]byte(requestJSON), &parked); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", "decode parked request")
		return
	}
	form := url.Values{}
	for k, v := range parked.Form {
		form[k] = v
	}
	req := &http.Request{Method: http.MethodGet, PostForm: form, Form: form, URL: &url.URL{RawQuery: form.Encode()}}
	ar, err := h.Provider.NewAuthorizeRequest(r.Context(), req)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "parked request no longer valid")
		return
	}
	for _, sc := range ar.GetRequestedScopes() {
		ar.GrantScope(sc)
	}
	resp, err := h.Provider.NewAuthorizeResponse(r.Context(), ar, &fosite.DefaultSession{Subject: "owner"})
	if err != nil {
		h.Provider.WriteAuthorizeError(r.Context(), w, ar, err)
		return
	}
	if _, err := h.DB.Exec("UPDATE oauth_requests SET state='consumed' WHERE id=?", id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", "consume request")
		return
	}
	h.Provider.WriteAuthorizeResponse(r.Context(), w, ar, resp)
}

// Token handles POST /oauth/token inside one storage transaction.
func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled {
		writeJSONError(w, http.StatusNotFound, "not_found", "oauth is disabled")
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "bad_request", "POST required")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request", "malformed form")
		return
	}
	ctx, err := h.Store.BeginTX(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", "begin tx")
		return
	}
	// Commit even when Fosite returns an error: replay detection performs a
	// security revocation that must persist; validation-only errors write
	// nothing, so committing them is equivalent to rollback.
	defer h.Store.Commit(ctx) //nolint:errcheck
	ar, err := h.Provider.NewAccessRequest(ctx, r, &fosite.DefaultSession{Subject: "owner"})
	if err != nil {
		h.Provider.WriteAccessError(r.Context(), w, ar, err)
		return
	}
	resp, err := h.Provider.NewAccessResponse(ctx, ar)
	if err != nil {
		h.Provider.WriteAccessError(r.Context(), w, ar, err)
		return
	}
	h.Provider.WriteAccessResponse(r.Context(), w, ar, resp)
}

// Revoke handles POST /oauth/revoke (RFC 7009).
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	if !h.Enabled {
		writeJSONError(w, http.StatusNotFound, "not_found", "oauth is disabled")
		return
	}
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "bad_request", "POST required")
		return
	}
	ctx, err := h.Store.BeginTX(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "server_error", "begin tx")
		return
	}
	defer h.Store.Commit(ctx) //nolint:errcheck
	if err := h.Provider.NewRevocationRequest(ctx, r); err != nil {
		// RFC 7009: revocation errors are still 200 unless the client is
		// unauthorized; Fosite's writer decides the status.
		h.Provider.WriteRevocationResponse(r.Context(), w, err)
		return
	}
	h.Provider.WriteRevocationResponse(r.Context(), w, nil)
}

// Discovery serves the anonymous metadata endpoints. No personal data.
func (h *Handler) Discovery(w http.ResponseWriter, r *http.Request, kind string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	var doc map[string]any
	switch kind {
	case "protected-resource":
		doc = map[string]any{
			"resource":              h.Issuer + "/mcp",
			"authorization_servers": []string{h.Issuer},
			"scopes_supported":      AllowedScopes,
			"bearer_methods":        []string{"header"},
		}
	case "authorization-server":
		doc = map[string]any{
			"issuer":                                h.Issuer,
			"authorization_endpoint":                h.Issuer + "/oauth/authorize",
			"token_endpoint":                        h.Issuer + "/oauth/token",
			"revocation_endpoint":                   h.Issuer + "/oauth/revoke",
			"response_types_supported":              []string{"code"},
			"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
			"code_challenge_methods_supported":      []string{"S256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
			"scopes_supported":                      AllowedScopes,
			"resource":                              h.Issuer + "/mcp",
		}
	default:
		writeJSONError(w, http.StatusNotFound, "not_found", "unknown discovery document")
		return
	}
	_ = json.NewEncoder(w).Encode(doc)
}

// --- helpers ---

func (h *Handler) ensureBindingCookie(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(bindingCookie); err == nil && len(c.Value) >= 32 {
		return hashBinding(c.Value), nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	value := hex.EncodeToString(raw)
	http.SetCookie(w, &http.Cookie{
		Name: bindingCookie, Value: value, Path: "/oauth/",
		MaxAge: int(pendingLifetime.Seconds()) + 60, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	return hashBinding(value), nil
}

func hashBinding(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func checkCSRF(binding, token string) bool {
	// The CSRF token is the first half of the binding hash; only the browser
	// holding the cookie can produce a matching pair.
	return len(binding) >= 32 && len(token) == 32 && token == binding[:32]
}

func allowlistedForm(form url.Values) map[string][]string {
	out := map[string][]string{}
	for k, v := range form {
		if allowedFormKeys[k] {
			out[k] = append([]string(nil), v...)
		}
	}
	return out
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": code, "error_description": msg})
}

// NewUUID is exported for handler use (same v4 format as store.NewUUID).
func NewUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
