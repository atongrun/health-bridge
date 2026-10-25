#!/usr/bin/env python3
"""Check design artifacts only; this is not a production ingest validator."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import sqlite3
import sys
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]


def reject_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def read_json(path):
    return json.loads(path.read_text(), object_pairs_hook=reject_duplicate_keys)


def check():
    required = [
        "README.md", "AGENTS.md", "HANDOFF.md", "LICENSE", ".gitignore",
        "docs/research.md", "docs/reuse.md", "docs/architecture.md",
        "docs/ios-sync.md", "docs/metrics.md", "docs/data-schema.md",
        "docs/api.md", "docs/security.md", "docs/deployment.md",
        "docs/roadmap.md", "docs/testing.md", "docs/review.md",
    ]
    for relative in required:
        assert (ROOT / relative).is_file(), f"missing required artifact: {relative}"

    markdowns = list(ROOT.rglob("*.md"))
    links = 0
    for path in markdowns:
        # This checks local file targets, not remote URLs or Markdown headings.
        for target in re.findall(r"\]\(([^)]+)\)", path.read_text()):
            if re.match(r"[a-zA-Z][a-zA-Z0-9+.-]*:", target) or target.startswith("#"):
                continue
            relative = unquote(target.split("#", 1)[0].split("?", 1)[0])
            assert not relative.startswith("/"), f"nonportable link in {path.name}"
            assert (path.parent / relative).exists(), f"broken link in {path.name}: {relative}"
            links += 1

    json_files = list(ROOT.rglob("*.json"))
    for path in json_files:
        read_json(path)

    registry = read_json(ROOT / "contracts/metrics.json")["metrics"]
    identifiers = {m["healthkit_identifier"]: m for m in registry}
    assert len(identifiers) == len(registry) == 22
    assert len({m["metric"] for m in registry}) == len(registry)
    config = read_json(ROOT / "config/config.example.json")
    assert config["listen_address"].startswith("127.0.0.1:")
    assert config["query_enabled"] is False
    assert config["oauth"]["enabled"] is False

    fixture = ROOT / "tests/fixtures/synthetic-samples-batch.json"
    body = read_json(fixture)
    assert body["type"] in identifiers
    assert len(body["samples"]) + len(body["deletions"]) <= 500
    assert body["samples"] or body["deletions"] or body["scan_state"] == "drained"
    for sample in body["samples"]:
        assert sample["type"] == body["type"]
        assert sample["source"]["bundle_id"] == "com.example.synthetic"
        assert sample["quantity"]["unit"] == identifiers[sample["type"]]["unit"]
        assert sample["quantity"]["count"] >= 1
        start = dt.datetime.fromisoformat(sample["start"].replace("Z", "+00:00"))
        end = dt.datetime.fromisoformat(sample["end"].replace("Z", "+00:00"))
        assert start <= end

    db = sqlite3.connect(":memory:")
    try:
        db.executescript((ROOT / "docs/schema.sql").read_text())
        assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
        assert db.execute("PRAGMA foreign_key_check").fetchall() == []
        table_count = db.execute("SELECT count(*) FROM sqlite_master WHERE type='table'").fetchone()[0]
        assert table_count >= 20
        db.execute("INSERT INTO publishers(id,epoch,active,created_at_us) VALUES('test-p','test-e',1,0)")
        db.execute("INSERT INTO sources(id,device_json) VALUES('test-source','{}')")
        invalid_statements = [
            "INSERT INTO publishers VALUES('test-p2','test-e2',1,0,NULL)",
            "INSERT INTO workouts(sample_uuid,activity_type,duration_seconds) VALUES('missing',46,1)",
            "INSERT INTO samples(uuid,hk_type,metric,kind,start_us,end_us,source_id,value,unit,"
            "metadata_json,raw_json,content_sha256,received_at_us,data_revision) "
            "VALUES('test-s','t','m','quantity',0,0,'test-source',1,'count','{}','{}','test',0,0)",
        ]
        for statement in invalid_statements:
            try:
                db.execute(statement)
            except sqlite3.IntegrityError:
                pass
            else:
                raise AssertionError("reference DDL accepted an invalid synthetic row")
    finally:
        db.close()

    print(f"PASS: {len(markdowns)} Markdown files, {links} local links, {len(json_files)} JSON files")
    print(f"PASS: {len(registry)} metric definitions and synthetic fixture basic invariants")
    print(f"PASS: reference DDL, {table_count} tables; SQLite {sqlite3.sqlite_version}; integrity/FK checks")
    print("PASS: DDL rejects multiple active publishers, orphan workouts, and null quantity counts")
    print(f"Fixture SHA-256: {hashlib.sha256(fixture.read_bytes()).hexdigest()}")
    print("NOT RUN: full JSON Schema validation, Go/iOS code, upstream tests, device or MCP integration")


if __name__ == "__main__":
    try:
        check()
    except (AssertionError, ValueError, OSError, sqlite3.Error) as error:
        print(f"FAIL: {error}", file=sys.stderr)
        raise SystemExit(1)
