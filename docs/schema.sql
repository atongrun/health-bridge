-- Health Bridge design reference, not an applied production migration.
-- Optional OAuth tables are scheduled for P10. UTC timestamps are microseconds.
PRAGMA foreign_keys = ON;
CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at_us INTEGER NOT NULL
) STRICT;
CREATE TABLE instance_state (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1), generation TEXT NOT NULL,
  data_revision INTEGER NOT NULL DEFAULT 0 CHECK(data_revision>=0),
  queries_paused INTEGER NOT NULL DEFAULT 1 CHECK(queries_paused IN (0,1)),
  report_timezone TEXT NOT NULL, policy_version INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE TABLE publishers (
  id TEXT NOT NULL, epoch TEXT NOT NULL, active INTEGER NOT NULL CHECK(active IN (0,1)),
  created_at_us INTEGER NOT NULL, revoked_at_us INTEGER,
  PRIMARY KEY(id,epoch)
) STRICT;
CREATE UNIQUE INDEX one_active_publisher ON publishers(active) WHERE active=1;
CREATE TABLE credentials (
  id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL CHECK(role IN ('upload','query')), scopes_json TEXT NOT NULL CHECK(json_valid(scopes_json)),
  metric_allowlist_json TEXT NOT NULL CHECK(json_valid(metric_allowlist_json)),
  publisher_id TEXT, publisher_epoch TEXT, min_date TEXT, max_date TEXT,
  max_days INTEGER NOT NULL CHECK(max_days>0), max_points INTEGER NOT NULL CHECK(max_points>0),
  created_at_us INTEGER NOT NULL, expires_at_us INTEGER NOT NULL, revoked_at_us INTEGER,
  FOREIGN KEY(publisher_id,publisher_epoch) REFERENCES publishers(id,epoch),
  CHECK(role='query' OR (publisher_id IS NOT NULL AND publisher_epoch IS NOT NULL))
) STRICT;
CREATE TABLE sync_streams (
  id TEXT PRIMARY KEY, publisher_id TEXT NOT NULL, publisher_epoch TEXT NOT NULL,
  server_generation TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('samples','enrichment','daily')),
  hk_type TEXT NOT NULL, predicate_version INTEGER NOT NULL DEFAULT 1,
  last_seq INTEGER NOT NULL DEFAULT 0 CHECK(last_seq>=0), last_query_at_us INTEGER,
  last_ack_at_us INTEGER, scan_state TEXT NOT NULL DEFAULT 'pending',
  FOREIGN KEY(publisher_id,publisher_epoch) REFERENCES publishers(id,epoch)
) STRICT;
CREATE TABLE batch_receipts (
  stream_id TEXT NOT NULL REFERENCES sync_streams(id), seq INTEGER NOT NULL CHECK(seq>0),
  batch_id TEXT NOT NULL UNIQUE, body_sha256 TEXT NOT NULL CHECK(length(body_sha256)=64),
  committed_at_us INTEGER NOT NULL, data_revision INTEGER NOT NULL,
  ack_json TEXT NOT NULL CHECK(json_valid(ack_json)), PRIMARY KEY(stream_id,seq)
) STRICT;
CREATE TABLE sources (
  id TEXT PRIMARY KEY, bundle_id TEXT, name TEXT, version TEXT, os_version TEXT, product_type TEXT,
  device_json TEXT NOT NULL CHECK(json_valid(device_json))
) STRICT;
CREATE TABLE samples (
  uuid TEXT PRIMARY KEY, hk_type TEXT NOT NULL, metric TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN ('quantity','category','workout')),
  start_us INTEGER NOT NULL, end_us INTEGER NOT NULL CHECK(end_us>=start_us),
  source_id TEXT NOT NULL REFERENCES sources(id), source_timezone TEXT, start_offset_seconds INTEGER,
  value REAL, unit TEXT, sample_count INTEGER, category_value INTEGER, category_label TEXT,
  series_state TEXT NOT NULL DEFAULT 'not_applicable' CHECK(series_state IN ('not_applicable','pending','complete','unavailable')),
  metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)),
  raw_json TEXT NOT NULL CHECK(json_valid(raw_json)), content_sha256 TEXT NOT NULL,
  received_at_us INTEGER NOT NULL, data_revision INTEGER NOT NULL,
  CHECK((kind='quantity' AND value IS NOT NULL AND unit IS NOT NULL AND sample_count IS NOT NULL AND sample_count>=1 AND category_value IS NULL)
     OR (kind='category' AND value IS NULL AND category_value IS NOT NULL)
     OR (kind='workout' AND value IS NULL AND category_value IS NULL))
) STRICT;
CREATE INDEX samples_metric_time ON samples(metric,start_us,uuid);
CREATE INDEX samples_source_time ON samples(source_id,metric,start_us,uuid);
CREATE INDEX samples_end_time ON samples(end_us,uuid);
CREATE TABLE tombstones (
  uuid TEXT PRIMARY KEY, hk_type TEXT NOT NULL, observed_at_us INTEGER NOT NULL,
  origin TEXT NOT NULL CHECK(origin IN ('healthkit','owner')), batch_id TEXT,
  reason TEXT NOT NULL, data_revision INTEGER NOT NULL
) STRICT;
CREATE TABLE quantity_series (
  sample_uuid TEXT NOT NULL REFERENCES samples(uuid) ON DELETE CASCADE,
  generation TEXT NOT NULL, expected_count INTEGER CHECK(expected_count>=0),
  expected_chunks INTEGER CHECK(expected_chunks>=0), manifest_sha256 TEXT,
  state TEXT NOT NULL CHECK(state IN ('pending','complete','unavailable','superseded')),
  reason TEXT, revision INTEGER NOT NULL, PRIMARY KEY(sample_uuid,generation)
) STRICT;
CREATE UNIQUE INDEX one_complete_series ON quantity_series(sample_uuid) WHERE state='complete';
CREATE TABLE series_chunks (
  sample_uuid TEXT NOT NULL, generation TEXT NOT NULL, chunk_index INTEGER NOT NULL CHECK(chunk_index>=0),
  digest TEXT NOT NULL, point_count INTEGER NOT NULL CHECK(point_count>=0),
  PRIMARY KEY(sample_uuid,generation,chunk_index),
  FOREIGN KEY(sample_uuid,generation) REFERENCES quantity_series(sample_uuid,generation) ON DELETE CASCADE
) STRICT;
CREATE TABLE quantity_points (
  sample_uuid TEXT NOT NULL, generation TEXT NOT NULL, ordinal INTEGER NOT NULL CHECK(ordinal>=0),
  start_us INTEGER NOT NULL, end_us INTEGER NOT NULL CHECK(end_us>=start_us), value REAL NOT NULL,
  PRIMARY KEY(sample_uuid,generation,ordinal),
  FOREIGN KEY(sample_uuid,generation) REFERENCES quantity_series(sample_uuid,generation) ON DELETE CASCADE
) STRICT;
CREATE INDEX quantity_points_time ON quantity_points(start_us,sample_uuid);
CREATE TABLE workouts (
  sample_uuid TEXT PRIMARY KEY REFERENCES samples(uuid) ON DELETE CASCADE,
  activity_type INTEGER NOT NULL, duration_seconds REAL NOT NULL CHECK(duration_seconds>=0),
  active_energy_kcal REAL, distance_m REAL, pool_length_m REAL CHECK(pool_length_m>0),
  statistics_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(statistics_json)),
  detail_state TEXT NOT NULL DEFAULT 'pending', detail_revision INTEGER NOT NULL DEFAULT 0,
  detail_captured_at_us INTEGER, raw_detail_json TEXT CHECK(raw_detail_json IS NULL OR json_valid(raw_detail_json))
) STRICT;
CREATE TABLE workout_events (
  workout_uuid TEXT NOT NULL REFERENCES workouts(sample_uuid) ON DELETE CASCADE,
  event_index INTEGER NOT NULL, event_type INTEGER NOT NULL, event_label TEXT NOT NULL,
  start_us INTEGER NOT NULL, end_us INTEGER NOT NULL CHECK(end_us>=start_us),
  metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)),
  PRIMARY KEY(workout_uuid,event_index)
) STRICT;
CREATE TABLE workout_activities (
  workout_uuid TEXT NOT NULL REFERENCES workouts(sample_uuid) ON DELETE CASCADE,
  activity_index INTEGER NOT NULL, hk_activity_uuid TEXT, activity_type INTEGER NOT NULL,
  start_us INTEGER NOT NULL, end_us INTEGER, duration_seconds REAL,
  statistics_json TEXT NOT NULL CHECK(json_valid(statistics_json)),
  PRIMARY KEY(workout_uuid,activity_index), CHECK(end_us IS NULL OR end_us>=start_us)
) STRICT;
CREATE TABLE workout_links (
  workout_uuid TEXT NOT NULL REFERENCES workouts(sample_uuid) ON DELETE CASCADE,
  related_uuid TEXT NOT NULL, relation TEXT NOT NULL CHECK(relation IN ('healthkit','temporal_overlap','recovery_window')),
  method_version TEXT NOT NULL, PRIMARY KEY(workout_uuid,related_uuid,relation)
) STRICT;
CREATE INDEX workout_links_related ON workout_links(related_uuid);
CREATE VIEW sleep_stages AS SELECT uuid,start_us,end_us,source_id,category_value,category_label,source_timezone
 FROM samples WHERE kind='category' AND hk_type='HKCategoryTypeIdentifierSleepAnalysis';
CREATE TABLE daily_snapshots (
  day TEXT NOT NULL, timezone TEXT NOT NULL, metric TEXT NOT NULL, producer TEXT NOT NULL,
  value REAL, unit TEXT NOT NULL, captured_at_us INTEGER NOT NULL,
  basis_stream_id TEXT NOT NULL REFERENCES sync_streams(id), basis_seq INTEGER NOT NULL,
  writer_stream_id TEXT NOT NULL REFERENCES sync_streams(id), writer_seq INTEGER NOT NULL,
  stale INTEGER NOT NULL CHECK(stale IN (0,1)), quality_json TEXT NOT NULL CHECK(json_valid(quality_json)),
  PRIMARY KEY(day,timezone,metric,producer)
) STRICT;
CREATE TABLE dirty_days (
  day TEXT NOT NULL, timezone TEXT NOT NULL, data_revision INTEGER NOT NULL,
  PRIMARY KEY(day,timezone)
) STRICT;
CREATE TABLE daily_metrics (
  day TEXT NOT NULL, timezone TEXT NOT NULL, metric TEXT NOT NULL,
  source_policy TEXT NOT NULL, method_version TEXT NOT NULL,
  value_json TEXT NOT NULL CHECK(json_valid(value_json)), quality_json TEXT NOT NULL CHECK(json_valid(quality_json)),
  input_revision INTEGER NOT NULL, computed_at_us INTEGER NOT NULL,
  PRIMARY KEY(day,timezone,metric,source_policy,method_version)
) STRICT;
CREATE TABLE reconciliation_candidates (
  sample_uuid TEXT NOT NULL, scan_id TEXT NOT NULL, first_seen_missing_us INTEGER NOT NULL,
  second_scan_id TEXT, status TEXT NOT NULL CHECK(status IN ('suspect_missing','dismissed','owner_deleted')),
  PRIMARY KEY(sample_uuid,scan_id)
) STRICT;
CREATE TABLE reconciliation_scans (
  id TEXT PRIMARY KEY, publisher_id TEXT NOT NULL, publisher_epoch TEXT NOT NULL,
  hk_type TEXT NOT NULL, start_date TEXT NOT NULL, end_date TEXT NOT NULL,
  inventory_revision INTEGER NOT NULL, chunk_count INTEGER NOT NULL CHECK(chunk_count>0),
  query_completed_at_us INTEGER NOT NULL, limited_history_start_us INTEGER,
  state TEXT NOT NULL CHECK(state IN ('pending','complete','invalidated')),
  FOREIGN KEY(publisher_id,publisher_epoch) REFERENCES publishers(id,epoch)
) STRICT;
CREATE TABLE reconciliation_chunks (
  scan_id TEXT NOT NULL REFERENCES reconciliation_scans(id) ON DELETE CASCADE,
  chunk_index INTEGER NOT NULL CHECK(chunk_index>=0), body_sha256 TEXT NOT NULL,
  missing_uuids_json TEXT NOT NULL CHECK(json_valid(missing_uuids_json)),
  PRIMARY KEY(scan_id,chunk_index)
) STRICT;
-- Optional OAuth adapter storage; JSON is a versioned allowlisted DTO, never gob/arbitrary objects.
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
