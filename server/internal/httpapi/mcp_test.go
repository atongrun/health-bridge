package httpapi_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atongrun/health-bridge/server/internal/auth"
	"github.com/atongrun/health-bridge/server/internal/httpapi"
	"github.com/atongrun/health-bridge/server/internal/store"
)

// mcpJSON posts one JSON-RPC message to /mcp and returns status + decoded body.
func mcpJSON(t *testing.T, url, token string, id int, method string, params any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	req, _ := http.NewRequest(http.MethodPost, url+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

type mcpEnv struct {
	srv      *httptest.Server
	rw       *sql.DB
	queryTok string
	upload   string
}

func newMCPEnv(t *testing.T) *mcpEnv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	rw, err := store.Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(rw); err != nil {
		t.Fatal(err)
	}
	ro, err := store.Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	epoch, err := auth.ReplacePublisher(rw, "p", now)
	if err != nil {
		t.Fatal(err)
	}
	_, upload, err := auth.CreateCredential(rw, "upload", "{}", "{}", "p", epoch, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	_, queryTok, err := auth.CreateCredential(rw, "query",
		`{"scopes":["health:summary","health:raw"],"metrics":["heart_rate"],"max_days":30}`,
		"{}", "", "", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rw.Exec(`INSERT INTO collection_profiles(id,version,metric,window_seconds,mode,mapping_json,evidence_ref,verified,enabled)
		VALUES('p_hr',1,'heart_rate',86400,'scheduled','{}','probe://s01',1,1)`); err != nil {
		t.Fatal(err)
	}
	vj, _ := json.Marshal(map[string]any{"unit": "count/min", "min": 60.0, "mean": 60.0, "max": 60.0, "sum": nil, "latest": nil, "n": 1})
	qj, _ := json.Marshal(map[string]any{"status": "observed_partial", "collection_method": "shortcuts_window_observation"})
	if _, err := rw.Exec(`INSERT INTO daily_metrics(day,timezone,metric,profile_id,method_version,value_json,quality_json,observation_revision,computed_at_us)
		VALUES('2026-01-02','UTC','heart_rate','p_hr','sc-window-v1',?,?,1,0)`, string(vj), string(qj)); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", httpapi.NewMCPHandler(ro, "test-secret"))
	mux.Handle("/mcp/", httpapi.NewMCPHandler(ro, "test-secret"))
	srv := httptest.NewServer(mux)
	t.Cleanup(func() { srv.Close(); ro.Close(); rw.Close() })
	return &mcpEnv{srv: srv, rw: rw, queryTok: queryTok, upload: upload}
}

func TestMCPRejectsAnonymousAndUploadTokens(t *testing.T) {
	e := newMCPEnv(t)
	var initParams any = map[string]any{
		"protocolVersion": "2026-07-28",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	}
	if status, _ := mcpJSON(t, e.srv.URL, "", 1, "initialize", initParams); status != http.StatusUnauthorized {
		t.Fatalf("anonymous initialize status = %d, want 401", status)
	}
	if status, _ := mcpJSON(t, e.srv.URL, e.upload, 2, "initialize", initParams); status != http.StatusForbidden {
		t.Fatalf("upload token initialize status = %d, want 403", status)
	}
}

func TestMCPHandshakeToolsAndCall(t *testing.T) {
	e := newMCPEnv(t)
	initParams := map[string]any{
		"protocolVersion": "2026-07-28",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test-client", "version": "0"},
	}
	status, initResp := mcpJSON(t, e.srv.URL, e.queryTok, 1, "initialize", initParams)
	if status != http.StatusOK {
		t.Fatalf("initialize status = %d body=%v", status, initResp)
	}
	if _, ok := initResp["result"].(map[string]any)["serverInfo"].(map[string]any); !ok {
		t.Fatalf("initialize result missing serverInfo: %v", initResp)
	}

	status, listResp := mcpJSON(t, e.srv.URL, e.queryTok, 2, "tools/list", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("tools/list status = %d body=%v", status, listResp)
	}
	tools := listResp["result"].(map[string]any)["tools"].([]any)
	names := map[string]map[string]any{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = tool
	}
	for _, want := range []string{"get_health_summary", "get_samples"} {
		tool, ok := names[want]
		if !ok {
			t.Fatalf("missing tool %q; got %v", want, names)
		}
		ann, _ := tool["annotations"].(map[string]any)
		if ann["readOnlyHint"] != true {
			t.Fatalf("tool %q lacks readOnlyHint: %v", want, ann)
		}
	}
	for _, forbidden := range []string{"sql", "query_sql", "write", "delete", "fetch_url"} {
		if _, ok := names[forbidden]; ok {
			t.Fatalf("unexpected tool %q exposed", forbidden)
		}
	}
	// The database starts paused: the tool must say so, not fabricate data.
	status, callResp := mcpJSON(t, e.srv.URL, e.queryTok, 3, "tools/call", map[string]any{
		"name": "get_health_summary",
		"arguments": map[string]any{
			"metrics": []string{"heart_rate"}, "start_date": "2026-01-01", "end_date": "2026-01-04",
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call (paused) status = %d body=%v", status, callResp)
	}
	result := callResp["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("paused call should be a tool error: %v", result)
	}
	if !strings.Contains(toJSON(result), "paused") {
		t.Fatalf("paused call should mention paused: %v", result)
	}

	// Owner enables queries; the same call returns the DTO.
	if _, err := e.rw.Exec("UPDATE instance_state SET queries_paused=0 WHERE singleton=1"); err != nil {
		t.Fatal(err)
	}
	status, okResp := mcpJSON(t, e.srv.URL, e.queryTok, 4, "tools/call", map[string]any{
		"name": "get_health_summary",
		"arguments": map[string]any{
			"metrics": []string{"heart_rate"}, "start_date": "2026-01-01", "end_date": "2026-01-04",
		},
	})
	if status != http.StatusOK {
		t.Fatalf("tools/call status = %d body=%v", status, okResp)
	}
	res := okResp["result"].(map[string]any)
	if res["isError"] == true {
		t.Fatalf("call failed: %v", res)
	}
	encoded := toJSON(res)
	if !strings.Contains(encoded, "2026-01-02") || !strings.Contains(encoded, "heart_rate") {
		t.Fatalf("summary payload missing expected data: %s", encoded)
	}
	for _, leak := range []string{"source_json", "token_hash", "profile_id"} {
		if strings.Contains(encoded, leak) {
			t.Fatalf("MCP payload leaked %q: %s", leak, encoded)
		}
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
