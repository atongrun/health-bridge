-- Migration 0001: shortcuts-1 schema (S03a).
-- Kept in sync with docs/schema.sql; that file stays the reviewed reference.
CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at_us INTEGER NOT NULL) STRICT;
CREATE TABLE instance_state(
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), generation TEXT NOT NULL,
 observation_revision INTEGER NOT NULL DEFAULT 0, projection_revision INTEGER NOT NULL DEFAULT 0,
 policy_version INTEGER NOT NULL DEFAULT 1, queries_paused INTEGER NOT NULL DEFAULT 1 CHECK(queries_paused IN(0,1)),
 report_timezone TEXT NOT NULL
) STRICT;
CREATE TABLE publishers(
 id TEXT NOT NULL, epoch TEXT NOT NULL, active INTEGER NOT NULL CHECK(active IN(0,1)),
 created_at_us INTEGER NOT NULL, revoked_at_us INTEGER, PRIMARY KEY(id,epoch)
) STRICT;
CREATE UNIQUE INDEX one_active_publisher ON publishers(active) WHERE active=1;
CREATE TABLE credentials(
 id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, role TEXT NOT NULL CHECK(role IN('upload','query')),
 scopes_json TEXT NOT NULL CHECK(json_valid(scopes_json)), policy_json TEXT NOT NULL CHECK(json_valid(policy_json)),
 publisher_id TEXT, publisher_epoch TEXT, created_at_us INTEGER NOT NULL, expires_at_us INTEGER NOT NULL, revoked_at_us INTEGER,
 FOREIGN KEY(publisher_id,publisher_epoch) REFERENCES publishers(id,epoch),
 CHECK(role='query' OR (publisher_id IS NOT NULL AND publisher_epoch IS NOT NULL))
) STRICT;
CREATE TABLE collection_profiles(
 id TEXT PRIMARY KEY, version INTEGER NOT NULL CHECK(version>0), metric TEXT NOT NULL,
 window_seconds INTEGER NOT NULL CHECK(window_seconds IN(900,86400)),
 mode TEXT NOT NULL DEFAULT 'scheduled' CHECK(mode IN('scheduled','on_demand')),
 mapping_json TEXT NOT NULL CHECK(json_valid(mapping_json)), evidence_ref TEXT,
 verified INTEGER NOT NULL DEFAULT 0 CHECK(verified IN(0,1)), enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN(0,1)),
 CHECK(enabled=0 OR (verified=1 AND evidence_ref IS NOT NULL)),
 CHECK(window_seconds<>900 OR mode='on_demand')
) STRICT;
CREATE UNIQUE INDEX one_enabled_profile_per_metric ON collection_profiles(metric) WHERE enabled=1;
CREATE TABLE collection_windows(
 id TEXT PRIMARY KEY, profile_id TEXT NOT NULL REFERENCES collection_profiles(id), start_us INTEGER NOT NULL,
 end_us INTEGER NOT NULL CHECK(end_us>start_us), latest_issued_order INTEGER,
 current_snapshot_id TEXT, last_state TEXT NOT NULL DEFAULT 'pending' CHECK(last_state IN('pending','observed','no_visible_data','failed','overflow','excluded')),
 last_attempt_at_us INTEGER, last_committed_at_us INTEGER, next_due_at_us INTEGER,
 UNIQUE(profile_id,start_us,end_us),
 FOREIGN KEY(current_snapshot_id,id) REFERENCES observation_snapshots(id,job_id) DEFERRABLE INITIALLY DEFERRED
) STRICT;
CREATE TABLE collection_plans(
 id TEXT PRIMARY KEY, publisher_id TEXT NOT NULL, publisher_epoch TEXT NOT NULL, server_generation TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN('active','closed','expired')), expires_at_us INTEGER NOT NULL,
 FOREIGN KEY(publisher_id,publisher_epoch) REFERENCES publishers(id,epoch)
) STRICT;
CREATE UNIQUE INDEX one_active_plan ON collection_plans(publisher_id,publisher_epoch) WHERE state='active';
CREATE TABLE collection_attempts(
 issued_order INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE,
 plan_id TEXT NOT NULL REFERENCES collection_plans(id), job_id TEXT NOT NULL REFERENCES collection_windows(id),
 expires_at_us INTEGER NOT NULL, state TEXT NOT NULL CHECK(state IN('issued','committed','failed','expired','superseded','purged')),
 captured_at_us INTEGER, received_at_us INTEGER, failure_code TEXT, UNIQUE(plan_id,job_id)
) STRICT;
CREATE TABLE observation_snapshots(
 id TEXT PRIMARY KEY, job_id TEXT NOT NULL REFERENCES collection_windows(id), content_digest TEXT NOT NULL CHECK(length(content_digest)=64),
 read_state TEXT NOT NULL CHECK(read_state IN('observed','no_visible_data')), record_count INTEGER NOT NULL,
 created_at_us INTEGER NOT NULL, UNIQUE(job_id,content_digest), UNIQUE(id,job_id),
 CHECK((read_state='observed' AND record_count>0 AND record_count<=1000) OR (read_state='no_visible_data' AND record_count=0))
) STRICT;
CREATE TABLE observation_items(
 snapshot_id TEXT NOT NULL REFERENCES observation_snapshots(id) ON DELETE CASCADE, ordinal INTEGER NOT NULL CHECK(ordinal>=0),
 kind TEXT NOT NULL CHECK(kind IN('quantity','category','workout')), start_us INTEGER NOT NULL, end_us INTEGER,
 time_precision TEXT NOT NULL CHECK(time_precision IN('second','millisecond','microsecond','unknown')),
 source_label TEXT, native_id TEXT, value REAL, unit TEXT, category_label TEXT, workout_json TEXT,
 source_json TEXT NOT NULL CHECK(json_valid(source_json)), PRIMARY KEY(snapshot_id,ordinal),
 CHECK(end_us IS NULL OR end_us>=start_us),
 CHECK((kind='quantity' AND value IS NOT NULL AND unit IS NOT NULL AND category_label IS NULL AND workout_json IS NULL)
 OR (kind='category' AND value IS NULL AND category_label IS NOT NULL AND workout_json IS NULL)
 OR (kind='workout' AND value IS NULL AND category_label IS NULL AND workout_json IS NOT NULL AND json_valid(workout_json)))
) STRICT;
CREATE INDEX observation_items_time ON observation_items(start_us,snapshot_id,ordinal);
CREATE TABLE observation_receipts(
 attempt_id TEXT PRIMARY KEY REFERENCES collection_attempts(id), snapshot_id TEXT NOT NULL REFERENCES observation_snapshots(id) ON DELETE CASCADE,
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64), ack_json TEXT NOT NULL CHECK(json_valid(ack_json)), committed_at_us INTEGER NOT NULL
) STRICT;
CREATE TABLE dirty_days(day TEXT NOT NULL, timezone TEXT NOT NULL, observation_revision INTEGER NOT NULL, PRIMARY KEY(day,timezone)) STRICT;
CREATE TABLE daily_metrics(
 day TEXT NOT NULL, timezone TEXT NOT NULL, metric TEXT NOT NULL, profile_id TEXT NOT NULL REFERENCES collection_profiles(id),
 method_version TEXT NOT NULL, value_json TEXT NOT NULL CHECK(json_valid(value_json)), quality_json TEXT NOT NULL CHECK(json_valid(quality_json)),
 observation_revision INTEGER NOT NULL, computed_at_us INTEGER NOT NULL, PRIMARY KEY(day,timezone,metric,profile_id,method_version)
) STRICT;
CREATE TABLE owner_exclusions(
 id TEXT PRIMARY KEY, metric TEXT NOT NULL, start_us INTEGER NOT NULL, end_us INTEGER NOT NULL CHECK(end_us>start_us),
 created_at_us INTEGER NOT NULL, reason TEXT NOT NULL
) STRICT;
-- Optional OAuth tables (S07); no external identity service required.
CREATE TABLE oauth_clients (
  client_id TEXT PRIMARY KEY, secret_hash TEXT, metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)),
  active INTEGER NOT NULL CHECK(active IN (0,1))
) STRICT;
CREATE TABLE oauth_requests (
  id TEXT PRIMARY KEY, browser_binding_hash TEXT NOT NULL, client_id TEXT NOT NULL REFERENCES oauth_clients(client_id),
  request_json TEXT NOT NULL CHECK(json_valid(request_json)), state TEXT NOT NULL CHECK(state IN ('pending','approved','denied','consumed')),
  expires_at_us INTEGER NOT NULL
) STRICT;
CREATE TABLE oauth_grants (
  id TEXT PRIMARY KEY, client_id TEXT NOT NULL REFERENCES oauth_clients(client_id),
  policy_json TEXT NOT NULL CHECK(json_valid(policy_json)), revoked_at_us INTEGER, created_at_us INTEGER NOT NULL
) STRICT;
CREATE TABLE oauth_sessions (
  kind TEXT NOT NULL CHECK(kind IN ('code','pkce','access','refresh')),
  signature TEXT NOT NULL, request_id TEXT NOT NULL, grant_id TEXT NOT NULL REFERENCES oauth_grants(id),
  payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), expires_at_us INTEGER NOT NULL,
  active INTEGER NOT NULL CHECK(active IN (0,1)), related_access_signature TEXT,
  PRIMARY KEY(kind,signature)
) STRICT;
CREATE INDEX oauth_sessions_grant ON oauth_sessions(grant_id);
CREATE INDEX oauth_sessions_request ON oauth_sessions(request_id);
