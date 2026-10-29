package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/query"
)

// queryHandler serves the read-only query surface over a separate read-only
// DB connection; the write path never touches it.
type queryHandler struct {
	db  *sql.DB // read-only connection
	svc *query.Service
}

// queryLimiter: 60 requests/min burst 10 (docs/security.md).
var queryLimiter = newRateLimiter(60, 10)

func newQueryHandler(db *sql.DB, cursorSecret string) *queryHandler {
	return &queryHandler{db: db, svc: &query.Service{DB: db, Secret: cursorSecret}}
}

func (h *queryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "bad_request", "GET only", requestID(), false)
		return
	}
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
	switch {
	case strings.HasPrefix(r.URL.Path, "/v1/query/summary"):
		h.serveSummary(w, r, grant)
	case strings.HasPrefix(r.URL.Path, "/v1/query/samples"):
		h.serveSamples(w, r, grant)
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown query route", requestID(), false)
	}
}

func (h *queryHandler) serveSummary(w http.ResponseWriter, r *http.Request, grant query.Grant) {
	q := r.URL.Query()
	metrics := q["metric"]
	start := q.Get("start_date")
	end := q.Get("end_date")
	limit := atoiDefault(q.Get("limit"), 100)
	resp, err := h.svc.QueryDaily(grant, metrics, start, end, limit, q.Get("cursor"), time.Now())
	writeQueryResponse(w, err, resp)
}

func (h *queryHandler) serveSamples(w http.ResponseWriter, r *http.Request, grant query.Grant) {
	q := r.URL.Query()
	metric := q.Get("metric")
	start := q.Get("start_date")
	end := q.Get("end_date")
	limit := atoiDefault(q.Get("limit"), 100)
	resp, err := h.svc.QuerySamples(grant, metric, start, end, limit, time.Now())
	writeQueryResponse(w, err, resp)
}

func writeQueryResponse(w http.ResponseWriter, err error, resp query.Response) {
	if err != nil {
		switch {
		case errors.Is(err, query.ErrQueriesPaused):
			writeError(w, http.StatusServiceUnavailable, "queries_paused", "queries are paused", requestID(), false)
		case errors.Is(err, query.ErrCursorStale):
			writeError(w, http.StatusConflict, "cursor_stale", "cursor no longer valid; re-run the query", requestID(), false)
		case errors.Is(err, query.ErrForbidden):
			writeError(w, http.StatusForbidden, "forbidden", err.Error(), requestID(), false)
		default:
			writeError(w, http.StatusBadRequest, "bad_request", err.Error(), requestID(), false)
		}
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func atoiDefault(s string, d int) int {
	if s == "" {
		return d
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return d
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return d
	}
	return n
}
