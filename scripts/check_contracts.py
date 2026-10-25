#!/usr/bin/env python3
"""S02 synthetic contract checks for shortcuts-1 plan/ACK/failure/query/error contracts.

Validates machine schemas and synthetic fixtures only. Semantic rules here cover
cross-field invariants JSON Schema cannot express (window membership/alignment,
unit registry, ACK count equality, plan/job/attempt chain coherence); the real
request_digest/content_digest, lease and transaction behavior is S03+.
Never reads HealthKit, runs a Go service, or performs real HTTP.
"""
from __future__ import annotations
import datetime as dt
import hashlib
import json
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / 'tests/fixtures'

SCHEMAS = {
    'plan-request': 'contracts/plan-request.schema.json',
    'plan-response': 'contracts/plan-response.schema.json',
    'commit-ack': 'contracts/commit-ack.schema.json',
    'failure-report': 'contracts/failure-report.schema.json',
    'query-response': 'contracts/query-response.schema.json',
    'error': 'contracts/error.schema.json',
    'observation': 'contracts/shortcuts-observation.schema.json',
}

VALIDATORS = {}
REGISTRY = {}


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


def parse_datetime(value):
    return dt.datetime.fromisoformat(value.replace('Z', '+00:00'))


def load_validators():
    from jsonschema import Draft202012Validator, FormatChecker
    import referencing
    registry = referencing.Registry()
    formats = FormatChecker()

    @formats.checks('date-time', raises=(ValueError, TypeError))
    def valid_datetime(value):
        if not isinstance(value, str):
            return True  # The schema's type rule rejects non-strings.
        return parse_datetime(value).tzinfo is not None

    # Register every schema resource first: validators capture the registry at
    # construction, so cross-file $refs only resolve once all $ids are loaded.
    contents = {}
    for name, rel in SCHEMAS.items():
        schema = read_json(ROOT / rel)
        Draft202012Validator.check_schema(schema)
        contents[name] = schema
        registry = registry.with_resources([(schema['$id'], referencing.Resource.from_contents(schema))])
    for name, schema in contents.items():
        VALIDATORS[name] = Draft202012Validator(schema, format_checker=formats, registry=registry)
    for entry in read_json(ROOT / 'contracts/metrics.json')['metrics']:
        REGISTRY[entry['metric']] = entry


def check(name, value):
    errors = list(VALIDATORS[name].iter_errors(value))
    return not errors, errors


def resolve_ref(ref):
    path, _, fragment = ref.partition('#')
    document = read_json(ROOT / path)
    for part in fragment.strip('/').split('/'):
        if part:
            document = document[part]
    return document


def build_case(base, changes):
    value = json.loads(json.dumps(resolve_ref(base) if isinstance(base, str) else base))
    for dotted, raw in changes.items():
        parts = dotted.split('.')
        target = value
        for part in parts[:-1]:
            target = target[int(part)] if isinstance(target, list) else target[part]
        key = parts[-1]
        if isinstance(raw, str) and raw.startswith('@repeat:'):
            _, count, ref = raw.split(':', 2)
            target[key] = json.loads(json.dumps(resolve_ref(ref))) * int(count)
        elif raw == ':delete':
            if isinstance(target, list):
                del target[int(key)]
            else:
                del target[key]
        elif isinstance(target, list):
            target[int(key)] = raw
        else:
            target[key] = raw
    return value


# Semantic rules return True when the value VIOLATES the invariant.
def _window_misaligned(start, end):
    width = (parse_datetime(end) - parse_datetime(start)).total_seconds()
    return width not in (900, 86400) or parse_datetime(start).timestamp() % width != 0


SEMANTIC_RULES = {
    'obs_window_membership': lambda v: any(
        not (parse_datetime(v['window']['start']) <= parse_datetime(item['start']) < parse_datetime(v['window']['end']))
        for item in v['items']),
    'obs_window_alignment': lambda v: _window_misaligned(v['window']['start'], v['window']['end']),
    'obs_unit_matches_registry': lambda v: any(
        item['quantity'] is not None and item['quantity']['unit'] != REGISTRY[v['metric']]['unit']
        for item in v['items']),
    'plan_window_alignment': lambda v: any(
        _window_misaligned(job['window']['start'], job['window']['end']) for job in v['jobs']),
    # Field-to-field equality is not expressible in JSON Schema; classify it here.
    'ack_counts_equal': lambda v: v['received_count'] != v['stored_count'],
}

# Positive fixtures each semantic rule must ACCEPT (no violation).
RULE_POSITIVES = {
    'obs_window_membership': ('observation', 'synthetic-shortcuts-observation.json'),
    'obs_window_alignment': ('observation', 'synthetic-shortcuts-observation.json'),
    'obs_unit_matches_registry': ('observation', 'synthetic-shortcuts-observation.json'),
    'plan_window_alignment': ('plan-response', 'plan-response.json'),
    'ack_counts_equal': ('commit-ack', 'commit-ack.json'),
    # ack-empty reuses a later attempt of the same job; count equality still applies.
}


def run_counterexamples(path, schema_of):
    cases = read_json(FIXTURES / path)['cases']
    accepted = []
    rejected = {'schema': [], 'semantic': []}
    for case in cases:
        base = case['base'] if 'base' in case else case['based_on']
        value = build_case(base, case['changes'])
        check_spec = case.get('check', 'schema')
        if check_spec.startswith('semantic:'):
            rule = SEMANTIC_RULES[check_spec.split(':', 1)[1]]
            if rule(value):
                rejected['semantic'].append(case['id'])
            else:
                accepted.append(case['id'])
        else:
            ok, _ = check(schema_of(case), value)
            (rejected['schema'] if not ok else accepted).append(case['id'])
    return cases, rejected, accepted


CATEGORIES = {
    'OBS-NEG-01': 'empty-vs-observed', 'OBS-NEG-02': 'empty-vs-observed',
    'OBS-NEG-03': 'overflow-cap', 'OBS-NEG-06': 'overflow-cap', 'OBS-NEG-08': 'overflow-cap',
    'SVC-NEG-05': 'overflow-cap',
    'OBS-NEG-04': 'protocol-version', 'SVC-NEG-07': 'protocol-version',
    'OBS-NEG-05': 'legacy-extra-fields', 'SVC-NEG-01': 'legacy-extra-fields', 'SVC-NEG-12': 'legacy-extra-fields',
    'SVC-NEG-14': 'legacy-extra-fields', 'SVC-NEG-19': 'legacy-extra-fields', 'SVC-NEG-21': 'legacy-extra-fields',
    'SVC-NEG-22': 'legacy-extra-fields',
    'OBS-NEG-10': 'typed-payload-shape', 'OBS-NEG-11': 'typed-payload-shape',
    'OBS-NEG-07': 'id-format', 'SVC-NEG-06': 'id-format',
    'OBS-NEG-09': 'date-time',
    'OBS-NEG-12': 'required-fields',
    'OBS-NEG-13': 'window-semantics', 'OBS-NEG-14': 'window-semantics', 'SVC-NEG-26': 'window-semantics',
    'OBS-NEG-15': 'unit-semantics',
    'SVC-NEG-02': 'plan-bounds', 'SVC-NEG-04': 'plan-bounds',
    'SVC-NEG-03': 'vocab-whitelist', 'SVC-NEG-13': 'vocab-whitelist', 'SVC-NEG-15': 'vocab-whitelist',
    'SVC-NEG-08': 'ack-consistency', 'SVC-NEG-09': 'ack-consistency', 'SVC-NEG-10': 'ack-consistency',
    'SVC-NEG-11': 'ack-consistency',
    'SVC-NEG-16': 'error-shape',
    'SVC-NEG-17': 'quality-honesty', 'SVC-NEG-18': 'quality-honesty', 'SVC-NEG-25': 'quality-honesty',
    'SVC-NEG-20': 'dto-shape', 'SVC-NEG-23': 'dto-shape', 'SVC-NEG-24': 'dto-shape',
}


def classification_report(rejected_ids):
    by_category = Counter(CATEGORIES[i] for i in rejected_ids)
    print(f'Classification report ({len(rejected_ids)} rejected cases):')
    for category, count in sorted(by_category.items()):
        print(f'  {category}: {count}')
    print('Deferred to S03+: request_digest/content_digest, lease expiry, issued_order, single transaction, snapshot reuse')


def verify_duplicate_key_rejection():
    raw = (FIXTURES / 'synthetic-shortcuts-observation.json').read_text()
    duplicated = raw.replace('"read_state": "observed",', '"read_state": "observed", "read_state": "observed",')
    try:
        json.loads(duplicated, object_pairs_hook=unique_keys, parse_constant=invalid_constant)
    except ValueError:
        print('PASS: duplicate JSON keys rejected at parse time')
        return
    raise AssertionError('duplicate JSON keys were accepted')


def check_chain_coherence():
    plan = read_json(FIXTURES / 'plan-response.json')
    obs = read_json(FIXTURES / 'synthetic-shortcuts-observation.json')
    ack = read_json(FIXTURES / 'commit-ack.json')
    ack_empty = read_json(FIXTURES / 'commit-ack-empty.json')
    failure = read_json(FIXTURES / 'failure-report.json')
    jobs = {job['job_id']: job for job in plan['jobs']}
    assert obs['plan_id'] == plan['plan_id'] and obs['job_id'] in jobs
    job = jobs[obs['job_id']]
    for field in ('attempt_id', 'profile_id', 'profile_version', 'metric'):
        assert obs[field] == job[field], f'observation {field} disagrees with the dispatched job'
    assert obs['window'] == job['window']
    assert obs['server_generation'] == plan['server_generation'] and obs['publisher_epoch'] == plan['publisher_epoch']
    for field in ('plan_id', 'job_id', 'attempt_id'):
        assert ack[field] == obs[field], f'ACK {field} disagrees with the observation'
    assert ack['server_generation'] == plan['server_generation'] and ack['publisher_epoch'] == plan['publisher_epoch']
    assert ack['received_count'] == ack['stored_count'] == len(obs['items'])
    assert ack_empty['plan_id'] == plan['plan_id'] and ack_empty['job_id'] == obs['job_id']
    assert failure['attempt_id'] in {job['attempt_id'] for job in plan['jobs']}
    print('PASS: plan -> job -> attempt -> observation -> ACK chain coherent across fixtures (generation/epoch consistent)')


def check_multiset_multiplicity():
    obs = read_json(FIXTURES / 'synthetic-shortcuts-observation.json')
    item_b = json.loads(json.dumps(obs['items'][0]))
    item_b['quantity'] = {'value': 61, 'unit': obs['items'][0]['quantity']['unit']}
    aab = json.loads(json.dumps(obs))
    aab['items'] = obs['items'] + [item_b]  # [A, A, B]
    baa = json.loads(json.dumps(aab))
    baa['items'] = [aab['items'][2], aab['items'][0], aab['items'][1]]  # [B, A, A]
    for value in (aab, baa):
        ok, errors = check('observation', value)
        assert ok, f'[A,A,B] variant rejected: {[e.message for e in errors]}'
    canonical = lambda value: sorted(json.dumps(item, sort_keys=True) for item in value['items'])
    assert len(aab['items']) == 3, 'multiset lost elements'
    assert canonical(aab) == canonical(baa), 'reordered same multiset compared unequal'
    counts = Counter(canonical(aab))
    assert sum(counts.values()) == 3 and sorted(counts.values()) == [1, 2], \
        'multiplicity not preserved: expected two records of one kind and one of another'
    print('PASS: [A,A,B] kept as 3 records; reorder [B,A,A] is the same multiset (C08/C17 fixture-level)')


def main():
    load_validators()
    positives = [
        ('plan-request', {'protocol': 'shortcuts-1'}),
        ('plan-response', read_json(FIXTURES / 'plan-response.json')),
        ('plan-response', read_json(FIXTURES / 'plan-response-not-configured.json')),
        ('commit-ack', read_json(FIXTURES / 'commit-ack.json')),
        ('commit-ack', read_json(FIXTURES / 'commit-ack-empty.json')),
        ('failure-report', read_json(FIXTURES / 'failure-report.json')),
        ('error', read_json(FIXTURES / 'error-run-busy.json')),
        ('query-response', read_json(FIXTURES / 'query-response.json')),
        ('query-response', read_json(FIXTURES / 'query-response-empty.json')),
        ('observation', read_json(FIXTURES / 'synthetic-shortcuts-observation.json')),
    ]
    for name, value in positives:
        ok, errors = check(name, value)
        assert ok, f'positive case failed for {name}: {[e.message for e in errors]}'
    for rule, (name, fixture) in RULE_POSITIVES.items():
        value = next(v for n, v in positives if n == name and v == read_json(FIXTURES / fixture))
        assert not SEMANTIC_RULES[rule](value), f'semantic rule {rule} rejected a valid fixture'
    print(f'PASS: {len(positives)} positive cases across {len(SCHEMAS)} contracts; semantic rules accept all valid fixtures')

    obs_cases, obs_rejected, accepted = run_counterexamples(
        'observation-counterexamples.json', lambda case: 'observation')
    assert not accepted, f'observation counterexamples accepted: {accepted}'
    print(f"PASS: {len(obs_cases)} observation counterexamples rejected "
          f"({len(obs_rejected['schema'])} schema / {len(obs_rejected['semantic'])} semantic)")

    svc_cases, svc_rejected, accepted = run_counterexamples(
        'service-counterexamples.json',
        lambda case: Path(case['schema']).name.removesuffix('.schema.json'))
    assert not accepted, f'service counterexamples accepted: {accepted}'
    print(f"PASS: {len(svc_cases)} service counterexamples rejected "
          f"({len(svc_rejected['schema'])} schema / {len(svc_rejected['semantic'])} semantic)")

    classification_report(obs_rejected['schema'] + obs_rejected['semantic']
                          + svc_rejected['schema'] + svc_rejected['semantic'])
    verify_duplicate_key_rejection()
    check_chain_coherence()
    check_multiset_multiplicity()

    digest = hashlib.sha256((FIXTURES / 'synthetic-shortcuts-observation.json').read_bytes()).hexdigest()
    print(f'Fixture SHA-256 (synthetic-shortcuts-observation.json): {digest}')
    print('NOT RUN: request_digest/content_digest, lease/issued_order, transactions, Go service, iPhone probes (S03/S04)')


if __name__ == '__main__':
    main()
