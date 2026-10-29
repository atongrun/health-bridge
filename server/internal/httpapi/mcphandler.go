package httpapi

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/mcpsrv"
	"github.com/atongrun/health-bridge/server/internal/query"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpHandler authenticates every MCP request with a query-role bearer token
// before handing it to the SDK. There is no anonymous mode and no OAuth yet.
type mcpHandler struct {
	db      *sql.DB // read-only connection
	handler http.Handler
}

// NewMCPHandler mounts the read-only MCP surface at /mcp (stateless JSON).
func NewMCPHandler(roDB *sql.DB, cursorSecret string) http.Handler {
	svc := &mcpsrv.Service{Query: &query.Service{DB: roDB, Secret: cursorSecret}}
	sdk := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return svc.Server() },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: bodyLimitBytes},
	)
	return &mcpHandler{db: roDB, handler: sdk}
}

func (h *mcpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token", requestID(), false)
		return
	}
	cred, err := auth.Authenticate(h.db, token, time.Now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credential", requestID(), false)
		return
	}
	if cred.Role != "query" {
		writeError(w, http.StatusForbidden, "forbidden", "token lacks query scope", requestID(), false)
		return
	}
	if !queryLimiter.allow(cred.ID, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "query rate limit exceeded", requestID(), true)
		return
	}
	grant, err := query.ParseGrant(cred.ScopesJSON)
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", err.Error(), requestID(), false)
		return
	}
	ctx := mcpsrv.WithAccess(r.Context(), mcpsrv.Access{Grant: grant})
	h.handler.ServeHTTP(w, r.WithContext(ctx))
}
