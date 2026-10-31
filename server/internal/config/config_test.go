package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	base := map[string]any{
		"config_version":  1,
		"listen_address":  "127.0.0.1:8080",
		"public_origin":   "https://health.example.com",
		"database_path":   filepath.Join(t.TempDir(), "health.db"),
		"admin_socket":    "/run/healthbridge/admin.sock",
		"report_timezone": "Asia/Shanghai",
		"query_enabled":   false,
		"limits": map[string]any{
			"body_bytes": 2097152, "query_days": 366, "query_points": 1000,
			"response_bytes": 1048576, "query_concurrency": 4,
			"writer_queue_capacity": 2, "observation_records": 1000,
		},
		"oauth": map[string]any{
			"enabled": false, "secret_file": "/etc/healthbridge/oauth-secret",
			"resource": "https://health.example.com/mcp", "dynamic_registration": false,
		},
		"backups": map[string]any{
			"directory": "/var/lib/healthbridge/backups",
			"age_recipient_file": "/etc/healthbridge/backup-recipient",
			"keep_daily": 7, "keep_weekly": 4,
		},
		"logging": map[string]any{"level": "info", "http_access_log": false},
		"collection": map[string]any{
			"protocol": "shortcuts-1", "enabled_profiles": []any{},
			"hot_lookback_days": 7, "max_backfill_days": 31, "jobs_per_plan": 8,
			"lease_seconds": 900, "recent_refresh_hours": 24,
			"older_hot_refresh_days": 7, "stale_grace_hours": 12,
			"dense_metrics_mode": "on_demand",
		},
	}
	if mutate != nil {
		mutate(base)
	}
	return marshal(t, base)
}

func marshal(t *testing.T, m map[string]any) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadOK(t *testing.T) {
	if _, err := Load(writeConfig(t, nil)); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	_, err := Load(writeConfig(t, func(m map[string]any) { m["surprise"] = 1 }))
	if err == nil || !strings.Contains(err.Error(), "surprise") {
		t.Fatalf("expected unknown-field error, got %v", err)
	}
}

func TestNonLoopbackRejected(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", "example.com:443"} {
		_, err := Load(writeConfig(t, func(m map[string]any) { m["listen_address"] = addr }))
		if err == nil {
			t.Fatalf("expected %q to be rejected", addr)
		}
	}
}

func TestStagingGates(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"query_enabled", func(m map[string]any) { m["query_enabled"] = true }},
		{"enabled_profiles", func(m map[string]any) { m["collection"].(map[string]any)["enabled_profiles"] = []any{"p1"} }},
		{"bad_protocol", func(m map[string]any) { m["collection"].(map[string]any)["protocol"] = "native-1" }},
		{"jobs_per_plan", func(m map[string]any) { m["collection"].(map[string]any)["jobs_per_plan"] = 9 }},
		{"max_backfill", func(m map[string]any) { m["collection"].(map[string]any)["max_backfill_days"] = 40 }},
		{"dense_mode", func(m map[string]any) { m["collection"].(map[string]any)["dense_metrics_mode"] = "scheduled" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, tc.mutate)); err == nil {
				t.Fatalf("expected %s to be rejected", tc.name)
			}
		})
	}
}

func TestBadTimezoneRejected(t *testing.T) {
	_, err := Load(writeConfig(t, func(m map[string]any) { m["report_timezone"] = "Not/AZone" }))
	if err == nil {
		t.Fatal("expected invalid timezone to be rejected")
	}
}
