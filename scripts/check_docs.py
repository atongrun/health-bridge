#!/usr/bin/env python3
"""Validate design artifacts, never read HealthKit or run a business service."""
from __future__ import annotations
import argparse
import copy
import datetime as dt
import hashlib
import json
import re
import sqlite3
from pathlib import Path
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]


def unique_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f'duplicate JSON key: {key}')
        result[key] = value
    return result


def invalid_constant(value):
    raise ValueError(f'non-finite JSON constant: {value}')


def read_json(path):
    return json.loads(path.read_text(), object_pairs_hook=unique_keys, parse_constant=invalid_constant)


def check_ddl():
    db = sqlite3.connect(':memory:')
    try:
        db.executescript((ROOT / 'docs/schema.sql').read_text())
        tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")}
        assert len(tables) == 18
        assert not tables.intersection({'samples', 'sync_streams', 'tombstones', 'quantity_series'})
        db.execute("INSERT INTO publishers VALUES('synthetic-p','synthetic-e',1,0,NULL)")
        db.execute("INSERT INTO collection_profiles(id,version,metric,window_seconds,mapping_json,mode) VALUES('synthetic-profile',1,'heart_rate',900,'{}','on_demand')")
        db.execute("INSERT INTO collection_windows(id,profile_id,start_us,end_us,next_due_at_us) VALUES('synthetic-job','synthetic-profile',0,900000000,0)")
        db.execute("INSERT INTO observation_snapshots VALUES('synthetic-snapshot','synthetic-job',?,'observed',2,0)", ('a' * 64,))
        item_sql = "INSERT INTO observation_items(snapshot_id,ordinal,kind,start_us,time_precision,value,unit,source_json) VALUES('synthetic-snapshot',?,'quantity',0,'second',60,'count/min','{}')"
        db.execute(item_sql, (0,))
        db.execute(item_sql, (1,))
        assert db.execute('SELECT count(*) FROM observation_items').fetchone()[0] == 2
        bad = [
            "INSERT INTO publishers VALUES('p2','e2',1,0,NULL)",
            "UPDATE collection_profiles SET enabled=1",
            "INSERT INTO collection_windows(id,profile_id,start_us,end_us,next_due_at_us) VALUES('j2','synthetic-profile',0,900000000,0)",
            "INSERT INTO observation_snapshots VALUES('bad','missing','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','observed',1,0)",
            "INSERT INTO observation_snapshots VALUES('bad','synthetic-job','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','no_visible_data',1,0)",
        ]
        for sql in bad:
            try:
                db.execute(sql)
            except sqlite3.IntegrityError:
                pass
            else:
                raise AssertionError('DDL accepted invalid synthetic row')
        assert db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
        assert db.execute('PRAGMA foreign_key_check').fetchall() == []
        print(f'PASS: {len(tables)} design tables; duplicate observations preserved; 5 negative DDL cases; SQLite {sqlite3.sqlite_version}')
    finally:
        db.close()


def check_schema(fixture):
    from jsonschema import Draft202012Validator, FormatChecker
    schema = read_json(ROOT / 'contracts/shortcuts-observation.schema.json')
    Draft202012Validator.check_schema(schema)
    formats = FormatChecker()

    @formats.checks('date-time', raises=(ValueError, TypeError))
    def valid_datetime(value):
        if not isinstance(value, str):
            return True  # The schema's type rule rejects non-strings.
        return dt.datetime.fromisoformat(value.replace('Z', '+00:00')).tzinfo is not None

    validator = Draft202012Validator(schema, format_checker=formats)
    validator.validate(fixture)
    empty = copy.deepcopy(fixture)
    empty.update(read_state='no_visible_data', items=[])
    validator.validate(empty)
    variants = []
    for changes in [dict(items=[]), dict(read_state='no_visible_data'), dict(limit_reached=True),
                    dict(protocol='1.0'), dict(deletions=[]), dict(items=fixture['items'] * 501),
                    dict(profile_version=0), dict(query_limit=1000),
                    dict(captured_at='2026-02-30T00:00:00Z')]:
        value = copy.deepcopy(fixture)
        value.update(changes)
        variants.append(value)
    invalid = copy.deepcopy(fixture)
    invalid['items'][0]['category'] = {'label': 'Asleep'}
    variants.append(invalid)
    for value in variants:
        assert list(validator.iter_errors(value)), 'Schema accepted invalid synthetic fixture'
    print('PASS: JSON Schema 2020-12 meta-schema, date-time validation, 2 positive / 10 negative structural cases')


def main(full):
    required = ['README.md', 'AGENTS.md', 'HANDOFF.md', 'docs/architecture.md', 'docs/roadmap.md',
                'docs/ios-sync.md', 'docs/shortcuts-probe.md', 'docs/research.md', 'docs/reuse.md',
                'docs/api.md', 'docs/security.md', 'docs/data-schema.md', 'docs/deployment.md',
                'docs/review.md', 'docs/testing.md', 'docs/decisions/007-shortcuts-first.md']
    for name in required:
        assert (ROOT / name).is_file(), f'missing: {name}'
    markdowns = list(ROOT.rglob('*.md'))
    links = 0
    for path in markdowns:
        for target in re.findall(r'\]\(([^)]+)\)', path.read_text()):
            if re.match(r'[a-zA-Z][a-zA-Z0-9+.-]*:', target) or target.startswith('#'):
                continue
            relative = unquote(target.split('#', 1)[0].split('?', 1)[0])
            assert not relative.startswith('/')
            assert (path.parent / relative).exists(), f'broken link: {path.name}: {relative}'
            links += 1
    for path in ROOT.rglob('*.json'):
        read_json(path)
    registry = read_json(ROOT / 'contracts/metrics.json')['metrics']
    assert len({m['metric'] for m in registry}) == len(registry) == 22
    assert all(m['shortcuts_status'] == 'pending' and m['enabled'] is False for m in registry)
    capability = read_json(ROOT / 'docs/evidence/shortcuts-capabilities.json')
    assert capability['device_verified'] is False and capability['status'] == 'not_run'
    config = read_json(ROOT / 'config/config.example.json')
    assert config['collection']['enabled_profiles'] == [] and config['query_enabled'] is False
    assert config['listen_address'].startswith('127.0.0.1:') and config['oauth']['enabled'] is False
    for old in ['contracts/sync-batch.schema.json', 'tests/fixtures/synthetic-samples-batch.json']:
        assert not (ROOT / old).exists(), f'withdrawn native contract still active: {old}'
    assert 'Superseded' in (ROOT / 'docs/decisions/001-native-client.md').read_text()
    assert 'Superseded' in (ROOT / 'docs/decisions/002-durable-sync.md').read_text()
    assert not re.search(r'^## P0[1-9]|^## P1[01]', (ROOT / 'docs/roadmap.md').read_text(), re.M)
    fixture_path = ROOT / 'tests/fixtures/synthetic-shortcuts-observation.json'
    fixture = read_json(fixture_path)
    assert fixture['protocol'] == 'shortcuts-1' and len(fixture['items']) == 2
    assert fixture['items'][0] == fixture['items'][1], 'multiplicity fixture changed'
    parse = lambda value: dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    for item in fixture['items']:
        assert parse(fixture['window']['start']) <= parse(item['start']) < parse(fixture['window']['end'])
        assert item['native_id'] is None and item['source_label'] is None
    print(f'PASS: {len(markdowns)} Markdown files, {links} local links; 22 pending candidates; no enabled profiles or native contract')
    check_ddl()
    if full:
        check_schema(fixture)
    else:
        print('NOT RUN: full JSON Schema validation (install requirements-design.txt and use --full)')
    print('Fixture SHA-256:', hashlib.sha256(fixture_path.read_bytes()).hexdigest())
    print('NOT RUN: iPhone capability/transport probes, Go service tests, real health upload or AI integration')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--full', action='store_true')
    main(parser.parse_args().full)
