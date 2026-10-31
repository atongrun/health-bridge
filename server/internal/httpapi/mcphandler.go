package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/mcpsrv"
	"github.com/atongrun/health-bridge/server/internal/oauth"
	"github.com/atongrun/health-bridge/server/internal/query"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ory/fosite"
)

// mcpHandler authenticates every MCP request with either a query-role bearer
// token or an OAuth access token (introspected on every request) before
// handing it to the SDK. There is no anonymous mode.
type mcpHandler struct {
	db      *sql.DB // read-only connection
	handler http.Handler
	oauth   *oauth.Handler // may be nil / disabled
}

// NewMCPHandler mounts the read-only MCP surface at /mcp (stateless JSON).
func NewMCPHandler(roDB *sql.DB, cursorSecret string, oauthHandler *oauth.Handler) http.Handler {
	svc := &mcpsrv.Service{Query: &query.Service{DB: roDB, Secret: cursorSecret}}
	sdk := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return svc.Server() },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: bodyLimitBytes},
	)
	return &mcpHandler{db: roDB, handler: sdk, oauth: oauthHandler}
}

func (h *mcpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token", requestID(), false)
		return
	}
	grant, limiterKey, err := h.resolveAccess(token)
	if err != nil {
		if errors.Is(err, errForbidden) {
			writeError(w, http.StatusForbidden, "forbidden", "token lacks query scope", requestID(), false)
		} else {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credential", requestID(), false)
		}
		return
	}
	if !queryLimiter.allow(limiterKey, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "query rate limit exceeded", requestID(), true)
		return
	}
	ctx := mcpsrv.WithAccess(r.Context(), mcpsrv.Access{Grant: grant})
	h.handler.ServeHTTP(w, r.WithContext(ctx))
}

// resolveAccess accepts either the project's query bearer (credentials table)
// or an OAuth access token. OAuth introspection runs on every request, so a
// revoked grant takes effect immediately.
func (h *mcpHandler) resolveAccess(token string) (query.Grant, string, error) {
	if h.oauth != nil && h.oauth.Enabled {
		if _, ar, err := h.oauth.Provider.IntrospectToken(context.Background(), token, fosite.AccessToken, &fosite.DefaultSession{Subject: "owner"}); err == nil {
			return query.Grant{
				Scopes:  ar.GetGrantedScopes(),
				MaxDays: 366, // metric/day policy rides in the grant policy JSON (S07b)
			}, "oauth:" + ar.GetClient().GetID(), nil
		}
	}
	cred, err := auth.Authenticate(h.db, token, time.Now())
	if err != nil {
		return query.Grant{}, "", err
	}
	if cred.Role != "query" {
		return query.Grant{}, "", errForbidden
	}
	grant, err := query.ParseGrant(cred.ScopesJSON)
	if err != nil {
		return query.Grant{}, "", err
	}
	return grant, cred.ID, nil
}
