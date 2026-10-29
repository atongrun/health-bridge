// Package mcpsrv exposes the read-only query surface over MCP (S07a, bearer
// only). Tools mirror the REST scopes and share the same query.Service and
// grant checks; there is no SQL, file, URL or write tool, and requests are
// never anonymous.
package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/atongrun/health-bridge/server/internal/query"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Access is the per-request authorization resolved by the HTTP wrapper.
type Access struct {
	Grant query.Grant
}

type accessKey struct{}

// WithAccess attaches the resolved grant to a request context.
func WithAccess(ctx context.Context, a Access) context.Context {
	return context.WithValue(ctx, accessKey{}, a)
}

func accessFrom(ctx context.Context) (Access, bool) {
	a, ok := ctx.Value(accessKey{}).(Access)
	return a, ok
}

// Service builds the MCP server over a read-only query service.
type Service struct {
	Query *query.Service
}

// Server constructs the MCP server with its fixed tool set.
func (s *Service) Server() *mcp.Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "health-bridge", Version: "0.1.0-s07a",
			Description: "Read-only personal health observations collected by system Shortcuts."},
		&mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}},
	)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_health_summary",
		Description: "Daily observed statistics for granted metrics over a date range. Returns null where data is missing; not an official Health total.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, Title: "Health summary"},
	}, s.healthSummary)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_samples",
		Description: "Raw shortcut observation records (start, end, value, unit) for one granted metric. No source identifiers.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, Title: "Health samples"},
	}, s.samples)
	return srv
}

// SummaryInput is the get_health_summary argument object.
type SummaryInput struct {
	Metrics   []string `json:"metrics" jsonschema:"granted metric names, e.g. heart_rate"`
	StartDate string   `json:"start_date" jsonschema:"inclusive ISO date (YYYY-MM-DD)"`
	EndDate   string   `json:"end_date" jsonschema:"exclusive ISO date (YYYY-MM-DD)"`
	Limit     int      `json:"limit,omitempty" jsonschema:"max rows, default 100, max 1000"`
}

// SamplesInput is the get_samples argument object.
type SamplesInput struct {
	Metric    string `json:"metric" jsonschema:"one granted metric name"`
	StartDate string `json:"start_date" jsonschema:"inclusive ISO date (YYYY-MM-DD)"`
	EndDate   string `json:"end_date" jsonschema:"exclusive ISO date (YYYY-MM-DD)"`
	Limit     int    `json:"limit,omitempty" jsonschema:"max rows, default 100, max 1000"`
}

// Result is the tool payload; it is the same DTO the REST API returns.
type Result struct {
	Response query.Response `json:"response"`
}

func (s *Service) healthSummary(ctx context.Context, _ *mcp.CallToolRequest, in SummaryInput) (*mcp.CallToolResult, Result, error) {
	access, ok := accessFrom(ctx)
	if !ok {
		return toolError("unauthenticated"), Result{}, nil
	}
	resp, err := s.Query.QueryDaily(access.Grant, in.Metrics, in.StartDate, in.EndDate, in.Limit, "", time.Now())
	if err != nil {
		return toolError(mcpErrorMessage(err)), Result{}, nil
	}
	return nil, Result{Response: resp}, nil
}

func (s *Service) samples(ctx context.Context, _ *mcp.CallToolRequest, in SamplesInput) (*mcp.CallToolResult, Result, error) {
	access, ok := accessFrom(ctx)
	if !ok {
		return toolError("unauthenticated"), Result{}, nil
	}
	resp, err := s.Query.QuerySamples(access.Grant, in.Metric, in.StartDate, in.EndDate, in.Limit, time.Now())
	if err != nil {
		return toolError(mcpErrorMessage(err)), Result{}, nil
	}
	return nil, Result{Response: resp}, nil
}

// toolError reports a tool-level failure inside Content so the model can see
// it, without pretending it is a protocol error.
func toolError(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

func mcpErrorMessage(err error) string {
	switch {
	case errors.Is(err, query.ErrQueriesPaused):
		return "queries are paused by the owner"
	case errors.Is(err, query.ErrForbidden):
		return "forbidden: " + err.Error()
	default:
		return fmt.Sprintf("query failed: %s", err.Error())
	}
}
