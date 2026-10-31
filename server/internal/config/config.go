// Package config loads the server configuration strictly: unknown fields are
// rejected, required values are validated, and features not yet implemented
// (query, OAuth, non-empty enabled profiles) fail fast rather than silently
// degrade.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Config mirrors config/config.example.json exactly; a field present in the
// file but absent here is an error (DisallowUnknownFields).
type Config struct {
	ConfigVersion  int        `json:"config_version"`
	ListenAddress  string     `json:"listen_address"`
	PublicOrigin   string     `json:"public_origin"`
	DatabasePath   string     `json:"database_path"`
	AdminSocket    string     `json:"admin_socket"`
	ReportTimezone string     `json:"report_timezone"`
	QueryEnabled   bool       `json:"query_enabled"`
	Limits         Limits     `json:"limits"`
	OAuth          OAuth      `json:"oauth"`
	Backups        Backups    `json:"backups"`
	Logging        Logging    `json:"logging"`
	Collection     Collection `json:"collection"`
}

type Limits struct {
	BodyBytes           int `json:"body_bytes"`
	QueryDays           int `json:"query_days"`
	QueryPoints         int `json:"query_points"`
	ResponseBytes       int `json:"response_bytes"`
	QueryConcurrency    int `json:"query_concurrency"`
	WriterQueueCapacity int `json:"writer_queue_capacity"`
	ObservationRecords  int `json:"observation_records"`
}

type OAuth struct {
	Enabled             bool   `json:"enabled"`
	SecretFile          string `json:"secret_file"`
	Resource            string `json:"resource"`
	DynamicRegistration bool   `json:"dynamic_registration"`
}

type Backups struct {
	Directory       string `json:"directory"`
	AgeRecipientFile string `json:"age_recipient_file"`
	KeepDaily       int    `json:"keep_daily"`
	KeepWeekly      int    `json:"keep_weekly"`
}

type Logging struct {
	Level         string `json:"level"`
	HTTPAccessLog bool   `json:"http_access_log"`
}

type Collection struct {
	Protocol            string   `json:"protocol"`
	EnabledProfiles     []string `json:"enabled_profiles"`
	HotLookbackDays     int      `json:"hot_lookback_days"`
	MaxBackfillDays     int      `json:"max_backfill_days"`
	JobsPerPlan         int      `json:"jobs_per_plan"`
	LeaseSeconds        int      `json:"lease_seconds"`
	RecentRefreshHours  int      `json:"recent_refresh_hours"`
	OlderHotRefreshDays int      `json:"older_hot_refresh_days"`
	StaleGraceHours     int      `json:"stale_grace_hours"`
	DenseMetricsMode    string   `json:"dense_metrics_mode"`
}

// Load reads and validates the config at path.
func Load(path string) (*Config, error) {
	if path == "" {
		return nil, fmt.Errorf("config path is empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("decode config: trailing data after top-level object")
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.ConfigVersion != 1 {
		return fmt.Errorf("config_version must be 1, got %d", c.ConfigVersion)
	}
	host, _, err := net.SplitHostPort(c.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen_address %q is not host:port: %w", c.ListenAddress, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("listen_address host %q must be loopback (127.0.0.1 or ::1)", host)
	}
	if c.DatabasePath == "" {
		return fmt.Errorf("database_path is required")
	}
	if c.AdminSocket == "" {
		return fmt.Errorf("admin_socket is required")
	}
	if _, err := time.LoadLocation(c.ReportTimezone); err != nil {
		return fmt.Errorf("report_timezone %q is not a valid IANA zone: %w", c.ReportTimezone, err)
	}
	// Staging gates: these features are implemented in later stages and must
	// not be silently enabled by configuration.
	if c.QueryEnabled {
		return fmt.Errorf("query_enabled must be false until the owner enables queries explicitly")
	}
	if c.OAuth.Enabled && c.OAuth.SecretFile == "" {
		return fmt.Errorf("oauth.enabled requires oauth.secret_file")
	}
	if len(c.Collection.EnabledProfiles) != 0 {
		return fmt.Errorf("collection.enabled_profiles must be empty until profiles pass S01 verification")
	}
	if c.Collection.Protocol != "shortcuts-1" {
		return fmt.Errorf("collection.protocol must be shortcuts-1, got %q", c.Collection.Protocol)
	}
	if c.Collection.JobsPerPlan < 1 || c.Collection.JobsPerPlan > 8 {
		return fmt.Errorf("collection.jobs_per_plan must be 1..8, got %d", c.Collection.JobsPerPlan)
	}
	if c.Collection.LeaseSeconds <= 0 {
		return fmt.Errorf("collection.lease_seconds must be positive")
	}
	if c.Collection.MaxBackfillDays < 1 || c.Collection.MaxBackfillDays > 31 {
		return fmt.Errorf("collection.max_backfill_days must be 1..31, got %d", c.Collection.MaxBackfillDays)
	}
	if c.Collection.DenseMetricsMode != "on_demand" {
		return fmt.Errorf("collection.dense_metrics_mode must be on_demand (900s profiles are on-demand only)")
	}
	if c.Limits.BodyBytes <= 0 || c.Limits.ResponseBytes <= 0 || c.Limits.ObservationRecords <= 0 {
		return fmt.Errorf("limits.body_bytes, response_bytes and observation_records must be positive")
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level %q is not one of debug|info|warn|error", c.Logging.Level)
	}
	return nil
}
