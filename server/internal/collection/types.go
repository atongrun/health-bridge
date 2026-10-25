// Package collection implements the shortcuts-1 ingestion protocol: limited
// planning and atomic snapshot receipt. It owns the transaction that commits
// snapshot/items/receipt/window pointer/dirty/revision together (invariant 1).
package collection

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// Protocol and window invariants.
const (
	Protocol        = "shortcuts-1"
	QueryLimit      = 1001 // cap+1 probe
	MaxRecords      = 1000
	Window900s      = 900
	Window86400s    = 86400
	ErrCodeNone     = ""
)

var (
	ErrNotConfigured     = errors.New("no enabled profile")
	ErrRunBusy           = errors.New("another plan is active")
	ErrAttemptSuperseded = errors.New("attempt superseded")
	ErrAttemptConflict   = errors.New("attempt conflict")
	ErrLeaseExpired      = errors.New("lease expired")
	ErrGenerationChanged = errors.New("generation changed")
	ErrCollectionExcl    = errors.New("collection excluded")
	ErrInvalid           = errors.New("invalid request")
)

// Window is a UTC-aligned [start, end) span in RFC3339 strings.
type Window struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// Quantity is the typed numeric payload of a record.
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Category is the typed category payload (e.g. sleep stage).
type Category struct {
	Label string `json:"label"`
}

// Workout is the typed workout payload; optional numeric fields stay null.
type Workout struct {
	Activity            string   `json:"activity"`
	DurationSeconds     *float64 `json:"duration_seconds"`
	ActiveEnergyKcal    *float64 `json:"active_energy_kcal"`
	DistanceM           *float64 `json:"distance_m"`
	AverageHeartRateBpm *float64 `json:"average_heart_rate_bpm"`
	MaximumHeartRateBpm *float64 `json:"maximum_heart_rate_bpm"`
}

// Item is one element of a bounded observation; exactly one typed payload is set.
type Item struct {
	Start         string    `json:"start"`
	End           *string   `json:"end"`
	TimePrecision string    `json:"time_precision"`
	SourceLabel   *string   `json:"source_label"`
	NativeID      *string   `json:"native_id"`
	Quantity      *Quantity `json:"quantity"`
	Category      *Category `json:"category"`
	Workout       *Workout  `json:"workout"`
}

// Observation is a full bounded window read for one job/attempt.
type Observation struct {
	Protocol         string  `json:"protocol"`
	PlanID           string  `json:"plan_id"`
	JobID            string  `json:"job_id"`
	AttemptID        string  `json:"attempt_id"`
	ServerGeneration string  `json:"server_generation"`
	PublisherEpoch   string  `json:"publisher_epoch"`
	ProfileID        string  `json:"profile_id"`
	ProfileVersion   int     `json:"profile_version"`
	Metric           string  `json:"metric"`
	Window           Window  `json:"window"`
	CapturedAt       string  `json:"captured_at"`
	ReadState        string  `json:"read_state"`
	QueryLimit       int     `json:"query_limit"`
	LimitReached     bool    `json:"limit_reached"`
	Items            []Item  `json:"items"`
}

// parseTime parses an RFC3339 timestamp into Unix microseconds.
func parseTime(s string) (int64, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0, err
	}
	return t.UnixMicro(), nil
}

// formatTime renders Unix microseconds as RFC3339 UTC.
func formatTime(us int64) string {
	return time.UnixMicro(us).UTC().Format(time.RFC3339)
}

// canonicalJSON re-encodes raw JSON with sorted keys and -0 normalized to 0,
// so two byte-different but semantically identical requests hash identically.
func canonicalJSON(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(normalizeNumber(v))
}

func normalizeNumber(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeNumber(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = normalizeNumber(t[i])
		}
		return t
	case json.Number:
		if t == "-0" {
			return json.Number("0")
		}
		return t
	default:
		return v
	}
}

// RequestDigest is the sha256 of the canonical request body. Two retries of the
// same attempt with the same body produce the same digest.
func RequestDigest(raw []byte) (string, error) {
	return requestDigest(raw)
}

// requestDigest is the unexported implementation (see RequestDigest).
func requestDigest(raw []byte) (string, error) {
	canon, err := canonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

// contentDigest is the multiset fingerprint of a snapshot: profile version,
// window, read_state and the sorted canonical record bytes with multiplicity.
func contentDigest(obs Observation) (string, error) {
	var itemBytes [][]byte
	for _, it := range obs.Items {
		it = normalizeItem(it)
		b, err := json.Marshal(it)
		if err != nil {
			return "", err
		}
		itemBytes = append(itemBytes, b)
	}
	sort.Slice(itemBytes, func(i, j int) bool { return bytes.Compare(itemBytes[i], itemBytes[j]) < 0 })

	h := sha256.New()
	for _, field := range []string{
		strconv.Itoa(obs.ProfileVersion), obs.Window.Start, obs.Window.End, obs.ReadState,
	} {
		writeLenPrefixed(h, []byte(field))
	}
	for _, b := range itemBytes {
		writeLenPrefixed(h, b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// normalizeItem copies an item, turning -0 into 0 so identical records compare
// equal even if one came in with a negative zero.
func normalizeItem(it Item) Item {
	if it.Quantity != nil && it.Quantity.Value == 0 {
		cp := *it.Quantity
		cp.Value = 0
		it.Quantity = &cp
	}
	return it
}

// writeLenPrefixed writes an 8-byte big-endian length then the bytes, so that
// concatenation is unambiguous regardless of record content.
func writeLenPrefixed(h interface{ Write([]byte) (int, error) }, b []byte) {
	var lb [8]byte
	n := uint64(len(b))
	for i := 7; i >= 0; i-- {
		lb[i] = byte(n)
		n >>= 8
	}
	_, _ = h.Write(lb[:])
	_, _ = h.Write(b)
}

// Validate performs the semantic checks the server enforces beyond JSON
// well-formedness; the machine schema remains the design contract.
func (o Observation) Validate() error {
	if o.Protocol != Protocol {
		return fmt.Errorf("protocol must be %s", Protocol)
	}
	if o.QueryLimit != QueryLimit {
		return fmt.Errorf("query_limit must be %d", QueryLimit)
	}
	if o.LimitReached {
		return errors.New("limit_reached must be false; overflow is reported via the failure endpoint")
	}
	ws, err := parseTime(o.Window.Start)
	if err != nil {
		return fmt.Errorf("window.start: %w", err)
	}
	we, err := parseTime(o.Window.End)
	if err != nil {
		return fmt.Errorf("window.end: %w", err)
	}
	if we <= ws {
		return errors.New("window.end must be after window.start")
	}
	if o.ReadState == "observed" {
		if len(o.Items) == 0 {
			return errors.New("observed read_state requires at least one item")
		}
	} else if o.ReadState == "no_visible_data" {
		if len(o.Items) != 0 {
			return errors.New("no_visible_data read_state requires zero items")
		}
	} else {
		return fmt.Errorf("unknown read_state %q", o.ReadState)
	}
	if len(o.Items) > MaxRecords {
		return fmt.Errorf("items exceed %d", MaxRecords)
	}
	for i, it := range o.Items {
		if err := o.validateItem(i, it, ws, we); err != nil {
			return err
		}
	}
	return nil
}

func (o Observation) validateItem(i int, it Item, ws, we int64) error {
	s, err := parseTime(it.Start)
	if err != nil {
		return fmt.Errorf("items[%d].start: %w", i, err)
	}
	if s < ws || s >= we {
		return fmt.Errorf("items[%d].start outside window", i)
	}
	if it.End != nil {
		e, err := parseTime(*it.End)
		if err != nil {
			return fmt.Errorf("items[%d].end: %w", i, err)
		}
		if e < s {
			return fmt.Errorf("items[%d].end before start", i)
		}
	}
	switch it.TimePrecision {
	case "second", "millisecond", "microsecond", "unknown":
	default:
		return fmt.Errorf("items[%d].time_precision %q", i, it.TimePrecision)
	}
	payloads := 0
	if it.Quantity != nil {
		payloads++
		if it.Quantity.Unit == "" {
			return fmt.Errorf("items[%d].quantity.unit is empty", i)
		}
	}
	if it.Category != nil {
		payloads++
		if it.Category.Label == "" {
			return fmt.Errorf("items[%d].category.label is empty", i)
		}
	}
	if it.Workout != nil {
		payloads++
		if it.Workout.Activity == "" {
			return fmt.Errorf("items[%d].workout.activity is empty", i)
		}
	}
	if payloads != 1 {
		return fmt.Errorf("items[%d] must have exactly one of quantity/category/workout", i)
	}
	return nil
}
