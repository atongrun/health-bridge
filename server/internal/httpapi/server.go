// Package httpapi exposes the shortcuts-1 upload endpoints over HTTP.
package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/collection"
	"github.com/atongrun/health-bridge/server/internal/store"
)

const (
	bodyLimitBytes = 2 << 20 // 2 MiB (config.limits.body_bytes)
	maxJSONDepth   = 8
	uploadPerMin   = 30
	uploadBurst    = 5
	lease          = 15 * time.Minute
	lookbackDays   = 7
)

// Handler serves the upload surface. Auth runs before any body is read.
type Handler struct {
	DB       *sql.DB
	service  *collection.Service
	limiter  *rateLimiter
	bodyMax  int64
	lookback int
}

// NewHandler builds the upload handler over an existing database handle.
func NewHandler(db *sql.DB) *Handler {
	return &Handler{
		DB:       db,
		service:  &collection.Service{DB: db},
		limiter:  newRateLimiter(uploadPerMin, uploadBurst),
		bodyMax:  bodyLimitBytes,
		lookback: lookbackDays,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.Method == http.MethodPost && r.URL.Path == "/v1/shortcuts/plan":
		h.requireUpload(w, r, h.handlePlan)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/shortcuts/observations":
		h.requireUpload(w, r, h.handleObservations)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/shortcuts/attempts/") && strings.HasSuffix(r.URL.Path, "/failure"):
		h.requireUpload(w, r, h.handleFailure)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/shortcuts/status":
		h.requireUpload(w, r, h.handleStatus)
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown route", requestID(), false)
	}
}

type uploadHandler func(w http.ResponseWriter, r *http.Request, cred auth.Credential)

// requireUpload authenticates (before reading the body) and rate-limits.
func (h *Handler) requireUpload(w http.ResponseWriter, r *http.Request, next uploadHandler) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token", requestID(), false)
		return
	}
	cred, err := auth.Authenticate(h.DB, token, time.Now())
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid credential", requestID(), false)
		return
	}
	if cred.Role != "upload" {
		writeError(w, http.StatusForbidden, "forbidden", "token lacks upload scope", requestID(), false)
		return
	}
	if !h.limiter.allow(cred.PublisherID.String, time.Now()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "upload rate limit exceeded", requestID(), true)
		return
	}
	next(w, r, cred)
}

func (h *Handler) handlePlan(w http.ResponseWriter, r *http.Request, cred auth.Credential) {
	if err := checkMediaType(r); err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error(), requestID(), false)
		return
	}
	raw, err := readBody(r, h.bodyMax)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "body exceeds limit", requestID(), false)
		return
	}
	if err := validateJSON(raw, maxJSONDepth); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), requestID(), false)
		return
	}
	var req struct {
		Protocol string `json:"protocol"`
	}
	if err := decodeStrict(raw, &req); err != nil || req.Protocol != collection.Protocol {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid plan request", requestID(), false)
		return
	}
	resp, err := h.service.Plan(cred.PublisherID.String, cred.PublisherEpoch.String, time.Now(), lease, h.lookback)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleObservations(w http.ResponseWriter, r *http.Request, cred auth.Credential) {
	if err := checkMediaType(r); err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error(), requestID(), false)
		return
	}
	raw, err := readBody(r, h.bodyMax)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "body exceeds limit", requestID(), false)
		return
	}
	if err := validateJSON(raw, maxJSONDepth); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), requestID(), false)
		return
	}
	var obs collection.Observation
	if err := decodeStrict(raw, &obs); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid observation", requestID(), false)
		return
	}
	digest, err := collection.RequestDigest(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid observation", requestID(), false)
		return
	}
	ack, err := h.service.SubmitObservation(cred.PublisherID.String, cred.PublisherEpoch.String, obs, digest, time.Now())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ack)
}

func (h *Handler) handleFailure(w http.ResponseWriter, r *http.Request, cred auth.Credential) {
	if err := checkMediaType(r); err != nil {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error(), requestID(), false)
		return
	}
	raw, err := readBody(r, h.bodyMax)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "body exceeds limit", requestID(), false)
		return
	}
	if err := validateJSON(raw, maxJSONDepth); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), requestID(), false)
		return
	}
	attemptID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/shortcuts/attempts/"), "/failure")
	var req struct {
		Protocol  string `json:"protocol"`
		AttemptID string `json:"attempt_id"`
		Reason    string `json:"reason"`
	}
	if err := decodeStrict(raw, &req); err != nil || req.AttemptID != attemptID {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid failure report", requestID(), false)
		return
	}
	if err := h.service.RecordFailure(cred.PublisherID.String, cred.PublisherEpoch.String, attemptID, req.Reason, time.Now()); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request, cred auth.Credential) {
	resp, err := h.service.Status(time.Now())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- helpers ---

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	return tok, tok != ""
}

func checkMediaType(r *http.Request) error {
	if enc := r.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return errors.New("compressed bodies are not accepted")
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		return errors.New("only application/json is accepted")
	}
	return nil
}

func readBody(r *http.Request, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("too large")
	}
	return raw, nil
}

func requestID() string { return store.NewUUID() }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorInfo struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Retryable bool   `json:"retryable"`
}

func writeError(w http.ResponseWriter, status int, code, message, rid string, retryable bool) {
	writeJSON(w, status, map[string]errorInfo{"error": {Code: code, Message: message, RequestID: rid, Retryable: retryable}})
}

// writeServiceError maps collection/auth errors onto HTTP status and codes.
func writeServiceError(w http.ResponseWriter, err error) {
	status, code, retryable := http.StatusInternalServerError, "unavailable", true
	switch {
	case errors.Is(err, collection.ErrRunBusy):
		status, code, retryable = http.StatusConflict, "run_busy", true
	case errors.Is(err, collection.ErrLeaseExpired):
		status, code, retryable = http.StatusConflict, "lease_expired", true
	case errors.Is(err, collection.ErrAttemptSuperseded):
		status, code, retryable = http.StatusConflict, "attempt_superseded", false
	case errors.Is(err, collection.ErrAttemptConflict):
		status, code, retryable = http.StatusConflict, "attempt_conflict", false
	case errors.Is(err, collection.ErrGenerationChanged):
		status, code, retryable = http.StatusConflict, "generation_changed", false
	case errors.Is(err, collection.ErrCollectionExcl):
		status, code, retryable = http.StatusForbidden, "collection_excluded", false
	case errors.Is(err, collection.ErrInvalid):
		status, code, retryable = http.StatusUnprocessableEntity, "unprocessable", false
	case errors.Is(err, auth.ErrInvalidToken), errors.Is(err, auth.ErrRevoked), errors.Is(err, auth.ErrExpired):
		status, code, retryable = http.StatusUnauthorized, "unauthorized", false
	}
	writeError(w, status, code, err.Error(), requestID(), retryable)
}
