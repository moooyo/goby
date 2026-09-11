"""Exact three-stage proof for the retained create-only Extras attempt.

The original snapshot and failed attempt remain immutable. This module accepts
one narrowly described last_scan_at update on the newly created library; it
does not trim snapshots or reuse the original two-session/seven-audit model.
"""

from collections import Counter

MARKER = 'goby-client-special-features-fixture-continuation-v1'
LIBRARY_ID = '57a85c1ca5b6c7ae602c587755250b2f'
ROOT_ID = '604d2c0f5c78919a6ee360cda2048066'
PINS = {
    'origin_failure': '9ce73d72d9ce002dce06230a73213294903800b3b49b06e9b9aa33ad69eda2c0',
    'origin_state': '513d971260e18d24ada666a3ec942391d4679bf2a98c5c852742cc70033fccf1',
    'origin_before_state': '6d719ab6f3cd6c13ebf9fe3d6a927abaf5040e6e87e84be5d81a57318440cefe',
    'origin_before_snapshot': 'd65097b9916f99723b670faec5d58a01a5380a31c2a369c19db8fbb067f67376',
    'origin_current_snapshot': '86cbffbc1b3c0d623ea894aab12843273adcce5c7b097f3b9878dfc276ee8275',
    'origin_library_ack': '998dffd8122cc7921df1a21092a357881e676950c6b858cd0f7332a3b8ecb94d',
    'origin_setup_source': '84af95f1d34227dc9c465b73545c6de6939963d44fb3fc2d2df50e7f0f759465',
    'origin_profile_source': '6bbc0bfd17b1c7eef44028fe34481436d39a3e6645442340189e2da35c382252',
}
PHASE_FILES = {phase: 'phase-' + name + '.json' for phase, name in (
    ('continuation_prepared', 'continuation-prepared'), ('scan_acknowledged', 'scan-acknowledged'),
    ('scan_complete', 'scan-complete'), ('protocol_complete', 'protocol-complete'))}


class ContinuationError(Exception):
    """The exact retained creation or authorized continuation differs."""


def require(value, message):
    if not value:
        raise ContinuationError(message)


def sequences(op, left, right, additions, increments):
    require(set(left) == set(right), 'The sequence population changed.')
    for name, previous in left.items():
        if name not in increments:
            require(op.equal_json(previous, right[name]), 'An unrelated sequence changed.')
            continue
        table, count = increments[name]
        start = previous['last_value'] + int(previous['is_called'])
        require(right[name] == {'last_value': start + count - 1, 'is_called': True} and
                sorted(row['id'] for row in additions[table]) == list(range(start, start + count)),
                'A sequence increment is not the exact owned set.')


def session(base, rows, auth, user_id, kind, window):
    require([auth.get(key) for key in ('login_status', 'logout_status', 'exact_status')] == [200, 204, 401],
            'An owned authentication scope lacks login/logout/exact-credential evidence.')
    found = [row for row in rows if row['token_hash'] == '\\x' + auth['token_sha256']]
    require(len(found) == 1, 'An acknowledged credential does not identify one new session.')
    row = found[0]
    require(row['user_id'] == user_id and row['kind'] == kind and row['revoked_at'] is not None and
            all(base.in_window(row[key], {'window': window}) for key in ('created_at', 'last_seen_at', 'revoked_at')) and
            base.timestamp(row['expires_at']) > base.timestamp(row['created_at']), 'A session has an unowned identity or time.')
    if kind == 'admin':
        require(row['device_registry_id'] is None and row['client_name'] == 'Goby Dashboard' and row['device_id'] == 'goby-dashboard',
                'A native setup session acquired an unexpected device identity.')
    return row


def audits(base, rows, expected, window):
    fields = ('action', 'source', 'actor_kind', 'actor_id', 'actor_credential_id', 'resource_kind', 'resource_id')
    require(len(rows) == len(expected) and Counter(tuple(row[key] for key in fields) for row in rows) == Counter(expected) and
            all(row['severity'] == 'Info' and row['revision'] == 0 and row['changed_fields'] == [] and
                row['state'] == ('completed' if row['action'] == 'scan.finished' else '') and
                row['affected_count'] == (1 if row['action'] in ('library.created', 'session.revoked') else 0) and
                base.in_window(row['created_at'], {'window': window}) for row in rows), 'The audit set is not the exact owned committed actions.')


def auth_audits(row, source):
    return [(action, source, 'user', row['user_id'], row['id'], 'session', row['id']) for action in ('session.login', 'session.revoked')]


def validate_creation(op, base, original, failed, state, failure):
    base.validate_structure(op, original, state)
    base.validate_structure(op, failed, state)
    require(failure.get('result') == 'retained_for_review' and failure.get('phase') == 'library_acknowledged' and
            failure.get('library_id') == LIBRARY_ID and failure.get('root_id') is None and failure.get('job_id') is None and
            failure.get('old_rows_preserved') is True and failure.get('retry_permitted') is False and
            all(failure['authentication']['viewer'].get(key) is None for key in ('login_status', 'logout_status', 'exact_status')),
            'The origin is not the retained create-only failure.')
    delta = base.old_rows_and_additions(op, original, failed)
    counts = {'libraries': 1, 'library_roots': 1, 'items': 1, 'item_metadata_state': 1,
              'theme_owner_ids': 1, 'sessions': 1, 'activity_entries': 3}
    require(all(len(rows) == counts.get(table, 0) for table, rows in delta.items()), 'The original attempt changed more than its acknowledged creation.')
    window = {'before': original['database']['metadata']['captured_at'], 'after': failed['database']['metadata']['captured_at']}
    library = delta['libraries'][0]
    require(library['id'] == LIBRARY_ID and library['name'] == base.LIBRARY_NAME and library['collection_type'] == 'movies' and
            library['last_scan_at'] is None and base.in_window(library['created_at'], {'window': window}), 'The acknowledged library was scanned or changed.')
    require(delta['library_roots'] == [{'id': ROOT_ID, 'library_id': LIBRARY_ID, 'path': base.ROOT, 'allowed_path': base.ROOT,
                                      'relative_path': '.'}], 'The original creation has another registered root.')
    item = delta['items'][0]
    require(item['id'] == item['library_id'] == LIBRARY_ID and item['type'] == 'CollectionFolder' and item['is_folder'] is True and
            item['name'] == base.LIBRARY_NAME and item['root_id'] is None and item['parent_id'] is None and
            item['path'] == item['relative_path'] == '' and item['media'] is None and
            delta['item_metadata_state'][0]['item_id'] == LIBRARY_ID and delta['theme_owner_ids'][0]['item_id'] == LIBRARY_ID and
            delta['theme_owner_ids'][0]['virtual_root'] is False, 'The original attempt published anything beyond its CollectionFolder.')
    administrator = session(base, delta['sessions'], failure['authentication']['admin'], state['admin_id'], 'admin', window)
    expected = auth_audits(administrator, 'native') + [('library.created', 'native', 'user', state['admin_id'], administrator['id'], 'library', LIBRARY_ID)]
    audits(base, delta['activity_entries'], expected, window)
    sequences(op, original['database']['sequences'], failed['database']['sequences'], delta,
              {'theme_owner_ids_id_seq': ('theme_owner_ids', 1), 'activity_entries_id_seq': ('activity_entries', 3)})
    return {'creation_admin_session_id': administrator['id'], 'creation_admin_revoked': True, 'creation_audit_count': 3}


def continuation_delta(op, base, failed, final, receipt):
    """Compare every old row, allowing only this new library's scan timestamp."""
    left, right = failed['database'], final['database']
    require(set(left['tables']) == set(right['tables']) and op.equal_json(left['catalog'], right['catalog']), 'The complete catalog changed.')
    for key in ('database', 'server_version_num', 'schemas', 'public_schema', 'relations', 'columns'):
        require(op.equal_json(left['metadata'][key], right['metadata'][key]), 'A complete catalog identity changed.')
    for key in ('runtime_sha256', 'browser_sha256', 'recovery', 'added_viewer_credentials'):
        require(op.equal_json(failed[key], final[key]), 'An existing private credential or recovery value changed.')
    result = {}
    for name, rows in left['tables'].items():
        if name == 'libraries':
            previous = base.keyed(rows, 'id')
            current = base.keyed(right['tables'][name], 'id')
            require(set(current) == set(previous) and previous[LIBRARY_ID]['last_scan_at'] is None, 'A library was created, removed or already scanned.')
            for key, row in previous.items():
                for field, value in row.items():
                    if key == LIBRARY_ID and field == 'last_scan_at':
                        require(base.in_window(current[key][field], {'window': receipt['continuation_window']}),
                                'The owned library scan timestamp is outside this continuation.')
                    else:
                        require(op.equal_json(value, current[key][field]), 'An existing library field changed.')
            result[name] = []
            continue
        previous = Counter(op.canonical_json(row) for row in rows)
        current = Counter(op.canonical_json(row) for row in right['tables'][name])
        require(not previous - current, 'An existing row changed during continuation: ' + name)
        added = current - previous
        result[name] = [row for row in right['tables'][name] if added[op.canonical_json(row)] > 0]
    counts = {'items': 8, 'item_metadata_state': 8, 'theme_owner_ids': 8, 'extra_reserved_paths': 3,
              'item_extra_resources': 4, 'scan_jobs': 1, 'sessions': 2, 'devices': 1, 'activity_entries': 6}
    require(all(len(rows) == counts.get(table, 0) for table, rows in result.items()), 'The continuation received an unapproved table increment.')
    return result


def validate_transition(op, base, original, failed, final, state, failure, receipt, media):
    origin = validate_creation(op, base, original, failed, state, failure)
    base.validate_structure(op, final, state)
    # The new library is absent from the original snapshot, so the aggregate
    # catalog proof accepts its final timestamp as a new row. It still compares
    # all original libraries/items and business rows without modification.
    item_ids, aggregate = base.validate_catalog_profile(op, original, final, receipt, media)
    delta = continuation_delta(op, base, failed, final, receipt)
    window = receipt['continuation_window']
    administrator = session(base, delta['sessions'], receipt['authentication']['admin'], state['admin_id'], 'admin', window)
    viewer = session(base, delta['sessions'], receipt['authentication']['viewer'], state['added_viewer']['user_id'], 'emby', window)
    require(administrator['id'] != origin['creation_admin_session_id'] and viewer['id'] == receipt['authentication']['viewer']['session_id'] and
            viewer['device_id'] == receipt['device_id'] and viewer['client_name'] == receipt['client_name'] and
            viewer['device_name'] == 'Linux Recorder' and viewer['client_version'] == '1.0', 'A continued actor reused or changed its owned identity.')
    device = delta['devices'][0]
    require(device['id'] == viewer['device_registry_id'] and device['reported_device_id'] == receipt['device_id'] and
            device['last_user_id'] == state['added_viewer']['user_id'] and device['reported_name'] == 'Linux Recorder' and
            device['app_name'] == receipt['client_name'] and device['app_version'] == '1.0' and device['custom_name'] is None and
            device['deleted_at'] is None and device['revision'] == 1 and device['ip_address'] == '127.0.0.1' and
            all(base.in_window(device[key], {'window': window}) for key in ('created_at', 'last_seen_at')), 'The one new registry row changed.')
    expected = auth_audits(administrator, 'native') + auth_audits(viewer, 'emby') + [
        ('scan.requested', 'native', 'user', state['admin_id'], administrator['id'], 'scan', receipt['job_id']),
        ('scan.finished', 'system', 'system', '', '', 'scan', receipt['job_id'])]
    audits(base, delta['activity_entries'], expected, window)
    sequences(op, failed['database']['sequences'], final['database']['sequences'], delta,
        {'theme_owner_ids_id_seq': ('theme_owner_ids', 8), 'devices_id_seq': ('devices', 1), 'activity_entries_id_seq': ('activity_entries', 6)})
    sequences(op, original['database']['sequences'], final['database']['sequences'], aggregate,
        {'theme_owner_ids_id_seq': ('theme_owner_ids', 9), 'devices_id_seq': ('devices', 1), 'activity_entries_id_seq': ('activity_entries', 9)})
    return {'item_ids': item_ids, 'table_count': 35, 'old_item_count': 13, 'new_item_count': 9, **origin,
            'native_session_id': administrator['id'], 'viewer_session_id': viewer['id'], 'new_device_id': device['id'],
            'continuation_audit_count': 6, 'aggregate_session_count': 3, 'aggregate_audit_count': 9,
            'old_rows_preserved': True, 'expected_increment_preserved': True}
