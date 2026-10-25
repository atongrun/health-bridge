package collection

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atongrun/health-bridge/server/internal/store"
)

// Service runs the shortcuts-1 planning and atomic-ingestion protocol over a
// single SQLite connection pool (single writer).
type Service struct {
	DB *sql.DB
}

// PlanJob is one dispatched window in a plan.
type PlanJob struct {
	JobID          string `json:"job_id"`
	AttemptID      string `json:"attempt_id"`
	ProfileID      string `json:"profile_id"`
	ProfileVersion int    `json:"profile_version"`
	Metric         string `json:"metric"`
	Window         Window `json:"window"`
	MaxRecords     int    `json:"max_records"`
}

// PlanResponse is the plan endpoint response (planned or not_configured).
type PlanResponse struct {
	Status           string    `json:"status"`
	Protocol         string    `json:"protocol"`
	PlanID           string    `json:"plan_id"`
	ServerGeneration string    `json:"server_generation"`
	PublisherEpoch   string    `json:"publisher_epoch"`
	ExpiresAt        string    `json:"expires_at"`
	Jobs             []PlanJob `json:"jobs"`
}

// ACK is the commit acknowledgement returned by the observations endpoint.
type ACK struct {
	Status              string `json:"status"`
	Protocol            string `json:"protocol"`
	ServerGeneration    string `json:"server_generation"`
	PublisherEpoch      string `json:"publisher_epoch"`
	PlanID              string `json:"plan_id"`
	JobID               string `json:"job_id"`
	AttemptID           string `json:"attempt_id"`
	SnapshotID          string `json:"snapshot_id"`
	ReceivedCount       int    `json:"received_count"`
	StoredCount         int    `json:"stored_count"`
	ReadState           string `json:"read_state"`
	ObservationRevision int64  `json:"observation_revision"`
	ProjectionState     string `json:"projection_state"`
}

// StatusResponse is the owner's collection status (no health values).
type StatusResponse struct {
	LastAttemptAt   string `json:"last_attempt_at"`
	LastCommittedAt string `json:"last_committed_at"`
	PendingWindows  int    `json:"pending_windows"`
	BlockedWindows  int    `json:"blocked_windows"`
}

type enabledProfile struct {
	ID            string
	Version       int
	Metric        string
	WindowSeconds int
}

type planRow struct {
	ID               string
	PublisherID      string
	PublisherEpoch   string
	ServerGeneration string
	State            string
	ExpiresAtUs      int64
}

type attemptRow struct {
	IssuedOrder int64
	PlanID      string
	JobID       string
	State       string
	ExpiresAtUs int64
}

// Plan dispatches a fresh plan for the publisher: at most 8 jobs, a bounded
// lease, single active plan. Returns ErrRunBusy when one is already active and
// a not_configured response when no profile is enabled.
func (s *Service) Plan(pubID, pubEpoch string, now time.Time, lease time.Duration, lookbackDays int) (PlanResponse, error) {
	gen, err := s.generation()
	if err != nil {
		return PlanResponse{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return PlanResponse{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	if err := s.checkPublisher(tx, pubID, pubEpoch); err != nil {
		return PlanResponse{}, err
	}
	if _, err := tx.Exec(`UPDATE collection_plans SET state='expired'
		WHERE publisher_id=? AND publisher_epoch=? AND state='active' AND expires_at_us<=?`,
		pubID, pubEpoch, now.UnixMicro()); err != nil {
		return PlanResponse{}, err
	}

	profiles, err := s.enabledScheduledProfiles(tx)
	if err != nil {
		return PlanResponse{}, err
	}
	resp := PlanResponse{
		Status:           "planned",
		Protocol:         Protocol,
		PlanID:           store.NewUUID(),
		ServerGeneration: gen,
		PublisherEpoch:   pubEpoch,
		ExpiresAt:        formatTime(now.Add(lease).UnixMicro()),
	}
	if len(profiles) == 0 {
		resp.Status = "not_configured"
		resp.Jobs = []PlanJob{}
		return resp, nil
	}

	jobs, err := s.selectJobs(tx, profiles, now, lookbackDays)
	if err != nil {
		return PlanResponse{}, err
	}
	expires := now.Add(lease).UnixMicro()
	if _, err := tx.Exec(`INSERT INTO collection_plans(id,publisher_id,publisher_epoch,server_generation,state,expires_at_us)
		VALUES(?,?,?,?,'active',?)`, resp.PlanID, pubID, pubEpoch, gen, expires); err != nil {
		if isUniqueViolation(err) {
			return PlanResponse{}, ErrRunBusy
		}
		return PlanResponse{}, err
	}
	for _, j := range jobs {
		if _, err := tx.Exec("INSERT INTO collection_attempts(id,plan_id,job_id,expires_at_us,state) VALUES(?,?,?,?,'issued')",
			j.AttemptID, resp.PlanID, j.JobID, expires); err != nil {
			return PlanResponse{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return PlanResponse{}, err
	}
	resp.Jobs = jobs
	return resp, nil
}

type candidate struct {
	profile enabledProfile
	startUs int64
	endUs   int64
	jobID   string
	state   string
}

// selectJobs picks up to 8 windows across enabled scheduled profiles,
// prioritising failed/overflow then oldest windows, and issues one attempt each.
func (s *Service) selectJobs(tx *sql.Tx, profiles []enabledProfile, now time.Time, lookbackDays int) ([]PlanJob, error) {
	var candidates []candidate
	for _, p := range profiles {
		widthUs := int64(p.WindowSeconds) * 1e6
		base := (now.UnixMicro() / widthUs) * widthUs
		windowCount := lookbackDays * 86400 / p.WindowSeconds
		for k := 1; k <= windowCount; k++ {
			startUs := base - int64(k)*widthUs
			endUs := startUs + widthUs
			jobID, state, err := s.ensureWindow(tx, p.ID, startUs, endUs)
			if err != nil {
				return nil, err
			}
			if state == "observed" || state == "no_visible_data" || state == "excluded" {
				continue
			}
			candidates = append(candidates, candidate{profile: p, startUs: startUs, endUs: endUs, jobID: jobID, state: state})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if rank(candidates[i].state) != rank(candidates[j].state) {
			return rank(candidates[i].state) < rank(candidates[j].state)
		}
		return candidates[i].startUs < candidates[j].startUs
	})
	if len(candidates) > 8 {
		candidates = candidates[:8]
	}
	jobs := make([]PlanJob, 0, len(candidates))
	for _, c := range candidates {
		jobs = append(jobs, PlanJob{
			JobID:          c.jobID,
			AttemptID:      store.NewUUID(),
			ProfileID:      c.profile.ID,
			ProfileVersion: c.profile.Version,
			Metric:         c.profile.Metric,
			Window:         Window{Start: formatTime(c.startUs), End: formatTime(c.endUs)},
			MaxRecords:     MaxRecords,
		})
	}
	return jobs, nil
}

func rank(state string) int {
	if state == "failed" || state == "overflow" {
		return 0
	}
	return 1
}

// ensureWindow returns the id of an existing window or inserts a fresh one.
func (s *Service) ensureWindow(tx *sql.Tx, profileID string, startUs, endUs int64) (id, state string, err error) {
	err = tx.QueryRow("SELECT id, last_state FROM collection_windows WHERE profile_id=? AND start_us=? AND end_us=?",
		profileID, startUs, endUs).Scan(&id, &state)
	if err == nil {
		return id, state, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	id = store.NewUUID()
	_, err = tx.Exec("INSERT INTO collection_windows(id,profile_id,start_us,end_us,last_state) VALUES(?,?,?,?,'pending')",
		id, profileID, startUs, endUs)
	if err != nil {
		return "", "", err
	}
	return id, "pending", nil
}

// SubmitObservation atomically commits a validated observation. It re-verifies
// identity/epoch/generation inside the write transaction, enforces the latest
// issued attempt, and replays the original ACK for an idempotent retry.
func (s *Service) SubmitObservation(pubID, pubEpoch string, obs Observation, reqDigest string, now time.Time) (ACK, error) {
	if err := obs.Validate(); err != nil {
		return ACK{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return ACK{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	if err := s.checkPublisher(tx, pubID, pubEpoch); err != nil {
		return ACK{}, err
	}
	att, err := s.loadAttempt(tx, obs.AttemptID)
	if err != nil {
		return ACK{}, err
	}
	plan, err := s.loadPlan(tx, att.PlanID)
	if err != nil {
		return ACK{}, err
	}
	if plan.PublisherID != pubID || plan.PublisherEpoch != pubEpoch || plan.PublisherEpoch != obs.PublisherEpoch {
		return ACK{}, ErrAttemptSuperseded
	}
	if plan.ServerGeneration != obs.ServerGeneration {
		return ACK{}, ErrGenerationChanged
	}
	gen, err := s.generationTx(tx)
	if err != nil {
		return ACK{}, err
	}
	if plan.ServerGeneration != gen {
		return ACK{}, ErrGenerationChanged
	}

	// A stale attempt (a newer one was issued for this job) is rejected before
	// any state is touched.
	var latest int64
	if err := tx.QueryRow("SELECT coalesce(max(issued_order),0) FROM collection_attempts WHERE job_id=?", att.JobID).Scan(&latest); err != nil {
		return ACK{}, err
	}
	if att.IssuedOrder < latest {
		return ACK{}, ErrAttemptSuperseded
	}

	switch att.State {
	case "committed":
		var storedDigest, storedAck string
		err := tx.QueryRow("SELECT request_digest, ack_json FROM observation_receipts WHERE attempt_id=?", obs.AttemptID).
			Scan(&storedDigest, &storedAck)
		if errors.Is(err, sql.ErrNoRows) {
			return ACK{}, ErrAttemptSuperseded
		}
		if err != nil {
			return ACK{}, err
		}
		if storedDigest != reqDigest {
			return ACK{}, ErrAttemptConflict
		}
		var ack ACK
		if err := json.Unmarshal([]byte(storedAck), &ack); err != nil {
			return ACK{}, err
		}
		return ack, nil
	case "issued":
		// proceed
	default:
		return ACK{}, ErrAttemptSuperseded
	}

	// A committed attempt replays even if the lease has since lapsed; the lease
	// gate applies only to a not-yet-committed attempt.
	if plan.State != "active" || plan.ExpiresAtUs <= now.UnixMicro() {
		return ACK{}, ErrLeaseExpired
	}
	winStart, winEnd, err := s.loadWindow(tx, att.JobID, obs)
	if err != nil {
		return ACK{}, err
	}
	if err := s.checkExcluded(tx, obs.Metric, winStart, winEnd); err != nil {
		return ACK{}, err
	}

	digest, err := contentDigest(obs)
	if err != nil {
		return ACK{}, err
	}
	var snapshotID string
	err = tx.QueryRow("SELECT id FROM observation_snapshots WHERE job_id=? AND content_digest=?", att.JobID, digest).
		Scan(&snapshotID)
	if errors.Is(err, sql.ErrNoRows) {
		snapshotID = store.NewUUID()
		if err := s.insertSnapshot(tx, snapshotID, att.JobID, digest, obs); err != nil {
			return ACK{}, err
		}
	} else if err != nil {
		return ACK{}, err
	}

	capUs, err := parseTime(obs.CapturedAt)
	if err != nil {
		return ACK{}, err
	}
	revision, err := s.bumpRevision(tx)
	if err != nil {
		return ACK{}, err
	}
	tz, err := s.reportTimezone(tx)
	if err != nil {
		return ACK{}, err
	}
	if err := s.markDirty(tx, winStart, winEnd, revision, tz); err != nil {
		return ACK{}, err
	}
	ack := ACK{
		Status:              "committed",
		Protocol:            Protocol,
		ServerGeneration:    plan.ServerGeneration,
		PublisherEpoch:      pubEpoch,
		PlanID:              plan.ID,
		JobID:               att.JobID,
		AttemptID:           obs.AttemptID,
		SnapshotID:          snapshotID,
		ReceivedCount:       len(obs.Items),
		StoredCount:         len(obs.Items),
		ReadState:           obs.ReadState,
		ObservationRevision: revision,
		ProjectionState:     "pending",
	}
	ackJSON, err := json.Marshal(ack)
	if err != nil {
		return ACK{}, err
	}
	if _, err := tx.Exec(`UPDATE collection_windows SET current_snapshot_id=?, last_state=?, last_attempt_at_us=?, last_committed_at_us=?, latest_issued_order=?, next_due_at_us=NULL WHERE id=?`,
		snapshotID, obs.ReadState, now.UnixMicro(), now.UnixMicro(), att.IssuedOrder, att.JobID); err != nil {
		return ACK{}, err
	}
	if _, err := tx.Exec("INSERT INTO observation_receipts(attempt_id,snapshot_id,request_digest,ack_json,committed_at_us) VALUES(?,?,?,?,?)",
		obs.AttemptID, snapshotID, reqDigest, string(ackJSON), now.UnixMicro()); err != nil {
		return ACK{}, err
	}
	if _, err := tx.Exec("UPDATE collection_attempts SET state='committed', received_at_us=?, captured_at_us=? WHERE id=?",
		now.UnixMicro(), capUs, obs.AttemptID); err != nil {
		return ACK{}, err
	}
	if err := tx.Commit(); err != nil {
		return ACK{}, err
	}
	return ack, nil
}

// RecordFailure records a fixed-reason attempt failure without creating a
// snapshot; the window becomes failed/overflow and old data is kept stale.
func (s *Service) RecordFailure(pubID, pubEpoch, attemptID, reason string, now time.Time) error {
	switch reason {
	case "overflow", "missing_field", "query_failed", "profile_mismatch":
	default:
		return fmt.Errorf("%w: %q", ErrInvalid, reason)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := s.checkPublisher(tx, pubID, pubEpoch); err != nil {
		return err
	}
	att, err := s.loadAttempt(tx, attemptID)
	if err != nil {
		return err
	}
	if att.State == "committed" {
		return ErrAttemptConflict
	}
	windowState := "failed"
	if reason == "overflow" {
		windowState = "overflow"
	}
	if _, err := tx.Exec("UPDATE collection_attempts SET state='failed', failure_code=?, received_at_us=? WHERE id=?",
		reason, now.UnixMicro(), attemptID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE collection_windows SET last_state=?, last_attempt_at_us=? WHERE id=?",
		windowState, now.UnixMicro(), att.JobID); err != nil {
		return err
	}
	return tx.Commit()
}

// Status reports owner-global collection state with no health values.
func (s *Service) Status(now time.Time) (StatusResponse, error) {
	var resp StatusResponse
	var lastAttempt, lastCommitted sql.NullInt64
	if err := s.DB.QueryRow("SELECT max(last_attempt_at_us), max(last_committed_at_us) FROM collection_windows").
		Scan(&lastAttempt, &lastCommitted); err != nil {
		return resp, err
	}
	if lastAttempt.Valid {
		resp.LastAttemptAt = formatTime(lastAttempt.Int64)
	}
	if lastCommitted.Valid {
		resp.LastCommittedAt = formatTime(lastCommitted.Int64)
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM collection_windows WHERE last_state IN ('pending','failed','overflow','excluded')").
		Scan(&resp.PendingWindows); err != nil {
		return resp, err
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM collection_windows WHERE last_state='overflow'").
		Scan(&resp.BlockedWindows); err != nil {
		return resp, err
	}
	return resp, nil
}

// --- internal helpers ---

func (s *Service) generation() (string, error) {
	var g string
	if err := s.DB.QueryRow("SELECT generation FROM instance_state WHERE singleton=1").Scan(&g); err != nil {
		return "", err
	}
	return g, nil
}

func (s *Service) generationTx(tx *sql.Tx) (string, error) {
	var g string
	if err := tx.QueryRow("SELECT generation FROM instance_state WHERE singleton=1").Scan(&g); err != nil {
		return "", err
	}
	return g, nil
}

func (s *Service) reportTimezone(tx *sql.Tx) (string, error) {
	var tz string
	if err := tx.QueryRow("SELECT report_timezone FROM instance_state WHERE singleton=1").Scan(&tz); err != nil {
		return "", err
	}
	return tz, nil
}

func (s *Service) checkPublisher(tx *sql.Tx, pubID, pubEpoch string) error {
	var active int
	err := tx.QueryRow("SELECT active FROM publishers WHERE id=? AND epoch=?", pubID, pubEpoch).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && active != 1) {
		return ErrAttemptSuperseded
	}
	return err
}

func (s *Service) enabledScheduledProfiles(tx *sql.Tx) ([]enabledProfile, error) {
	rows, err := tx.Query("SELECT id, version, metric, window_seconds FROM collection_profiles WHERE enabled=1 AND mode='scheduled'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []enabledProfile
	for rows.Next() {
		var p enabledProfile
		if err := rows.Scan(&p.ID, &p.Version, &p.Metric, &p.WindowSeconds); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) loadAttempt(tx *sql.Tx, attemptID string) (attemptRow, error) {
	var a attemptRow
	err := tx.QueryRow("SELECT issued_order, plan_id, job_id, state, expires_at_us FROM collection_attempts WHERE id=?", attemptID).
		Scan(&a.IssuedOrder, &a.PlanID, &a.JobID, &a.State, &a.ExpiresAtUs)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrAttemptSuperseded
	}
	return a, err
}

func (s *Service) loadPlan(tx *sql.Tx, planID string) (planRow, error) {
	var p planRow
	err := tx.QueryRow("SELECT id, publisher_id, publisher_epoch, server_generation, state, expires_at_us FROM collection_plans WHERE id=?", planID).
		Scan(&p.ID, &p.PublisherID, &p.PublisherEpoch, &p.ServerGeneration, &p.State, &p.ExpiresAtUs)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrAttemptSuperseded
	}
	return p, err
}

// loadWindow verifies the observation's window and profile against the
// dispatched job and returns the window span in microseconds.
func (s *Service) loadWindow(tx *sql.Tx, jobID string, obs Observation) (int64, int64, error) {
	var profileID string
	var startUs, endUs int64
	err := tx.QueryRow("SELECT profile_id, start_us, end_us FROM collection_windows WHERE id=?", jobID).
		Scan(&profileID, &startUs, &endUs)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrAttemptSuperseded
	}
	if err != nil {
		return 0, 0, err
	}
	ws, err := parseTime(obs.Window.Start)
	if err != nil {
		return 0, 0, err
	}
	we, err := parseTime(obs.Window.End)
	if err != nil {
		return 0, 0, err
	}
	if ws != startUs || we != endUs {
		return 0, 0, ErrAttemptSuperseded
	}
	var version, windowSeconds int
	var metric string
	err = tx.QueryRow("SELECT version, metric, window_seconds FROM collection_profiles WHERE id=?", profileID).
		Scan(&version, &metric, &windowSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrAttemptSuperseded
	}
	if err != nil {
		return 0, 0, err
	}
	if profileID != obs.ProfileID || version != obs.ProfileVersion || metric != obs.Metric {
		return 0, 0, ErrAttemptSuperseded
	}
	if int64(windowSeconds)*1e6 != endUs-startUs {
		return 0, 0, ErrAttemptSuperseded
	}
	return startUs, endUs, nil
}

func (s *Service) checkExcluded(tx *sql.Tx, metric string, startUs, endUs int64) error {
	var n int
	if err := tx.QueryRow("SELECT count(*) FROM owner_exclusions WHERE metric=? AND start_us<=? AND end_us>?",
		metric, startUs, endUs).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrCollectionExcl
	}
	return nil
}

func (s *Service) insertSnapshot(tx *sql.Tx, snapshotID, jobID, digest string, obs Observation) error {
	nowUs := time.Now().UnixMicro()
	if _, err := tx.Exec("INSERT INTO observation_snapshots(id,job_id,content_digest,read_state,record_count,created_at_us) VALUES(?,?,?,?,?,?)",
		snapshotID, jobID, digest, obs.ReadState, len(obs.Items), nowUs); err != nil {
		return err
	}
	for i, it := range obs.Items {
		kind, value, unit, categoryLabel, workoutJSON := itemColumns(it)
		startUs, err := parseTime(it.Start)
		if err != nil {
			return err
		}
		var endVal any
		if it.End != nil {
			e, err := parseTime(*it.End)
			if err != nil {
				return err
			}
			endVal = e
		}
		src, err := json.Marshal(normalizeItem(it))
		if err != nil {
			return err
		}
		var srcLabel, nativeID any
		if it.SourceLabel != nil {
			srcLabel = *it.SourceLabel
		}
		if it.NativeID != nil {
			nativeID = *it.NativeID
		}
		if _, err := tx.Exec(`INSERT INTO observation_items(snapshot_id,ordinal,kind,start_us,end_us,time_precision,source_label,native_id,value,unit,category_label,workout_json,source_json)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			snapshotID, i, kind, startUs, endVal, it.TimePrecision, srcLabel, nativeID, value, unit, categoryLabel, workoutJSON, string(src)); err != nil {
			return err
		}
	}
	return nil
}

// itemColumns flattens a typed item into the observation_items column set as
// driver-compatible values (nil or concrete, never pointers).
func itemColumns(it Item) (kind string, value, unit, categoryLabel, workoutJSON any) {
	switch {
	case it.Quantity != nil:
		return "quantity", it.Quantity.Value, it.Quantity.Unit, nil, nil
	case it.Category != nil:
		return "category", nil, nil, it.Category.Label, nil
	default:
		b, _ := json.Marshal(it.Workout)
		return "workout", nil, nil, nil, string(b)
	}
}

func (s *Service) bumpRevision(tx *sql.Tx) (int64, error) {
	if _, err := tx.Exec("UPDATE instance_state SET observation_revision = observation_revision + 1 WHERE singleton=1"); err != nil {
		return 0, err
	}
	var rev int64
	if err := tx.QueryRow("SELECT observation_revision FROM instance_state WHERE singleton=1").Scan(&rev); err != nil {
		return 0, err
	}
	return rev, nil
}

func (s *Service) markDirty(tx *sql.Tx, startUs, endUs int64, revision int64, tz string) error {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	day := func(us int64) string { return time.UnixMicro(us).In(loc).Format("2006-01-02") }
	days := map[string]bool{day(startUs): true, day(endUs - 1): true}
	for d := range days {
		if _, err := tx.Exec("INSERT OR REPLACE INTO dirty_days(day,timezone,observation_revision) VALUES(?,?,?)", d, tz, revision); err != nil {
			return err
		}
	}
	return nil
}

// isUniqueViolation reports whether the driver error is a UNIQUE constraint
// violation (used to translate the single-active-plan index into run_busy).
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
