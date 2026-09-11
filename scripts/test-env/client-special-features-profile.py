"""Pure validation for one receipted, nonempty Movie-extras candidate profile.

The legacy schema27 fixture validator intentionally remains an empty-profile
validator. This module validates the complete catalog independently and then
accepts only the explicitly owned additions below; it never removes that gate,
projects away current rows, or mutates a helper module's validation functions.
"""

from __future__ import annotations

from collections import Counter
import datetime as dt
import re

MARKER = 'goby-client-special-features-fixture-v1'
LIBRARY_NAME = 'M3e Client Special Features Movies'
MEDIA = '/opt/goby-fixtures/client-special-features-m3e-v1'
ROOT = MEDIA + '/Movies'
POSITIVE = 'M3e Special Features Positive (2026)'
EMPTY = 'M3e Special Features Empty (2026)'
FILES = {
    'positive': (POSITIVE + '/' + POSITIVE + '.mp4', 'Movie', 'M3e Special Features Positive'),
    'empty': (EMPTY + '/' + EMPTY + '.mp4', 'Movie', 'M3e Special Features Empty'),
    'alpha': (POSITIVE + '/featurettes/Alpha Bonus.mp4', 'Video', 'Alpha Bonus'),
    'deleted': (POSITIVE + '/deleted scenes/Middle Deleted Scene.mp4', 'Video', 'Middle Deleted Scene'),
    'zeta': (POSITIVE + '/featurettes/Zeta Bonus.mp4', 'Video', 'Zeta Bonus'),
    'trailer': (POSITIVE + '/trailers/Delta Local Trailer.mp4', 'Video', 'Delta Local Trailer'),
}
KINDS = {'alpha': 'clip', 'deleted': 'deleted_scene', 'zeta': 'clip', 'trailer': 'trailer'}
MARKERS = tuple(POSITIVE + '/' + name for name in ('deleted scenes', 'featurettes', 'trailers'))
ADDITIVE_TABLES = {'libraries', 'library_roots', 'items', 'item_metadata_state', 'theme_owner_ids',
    'extra_reserved_paths', 'item_extra_resources', 'scan_jobs', 'sessions', 'devices', 'activity_entries'}


class ProfileError(Exception):
    """The complete snapshot differs from this one explicit owned profile."""


def require(value, message):
    if not value:
        raise ProfileError(message)


def timestamp(value):
    require(isinstance(value, str), 'A witnessed timestamp is missing.')
    try:
        result = dt.datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError:
        raise ProfileError('A witnessed timestamp is malformed.') from None
    require(result.tzinfo is not None, 'A witnessed timestamp has no timezone.')
    return result


def in_window(value, receipt):
    return timestamp(receipt['window']['before']) <= timestamp(value) <= timestamp(receipt['window']['after'])


def keyed(rows, key):
    require(isinstance(rows, list) and all(isinstance(row, dict) and key in row for row in rows), 'A complete row inventory is malformed.')
    result = {row[key]: row for row in rows}
    require(len(result) == len(rows), 'A complete row inventory repeats an identity.')
    return result


def validate_structure(op, snapshot, state):
    """Keep the full 35-table schema/ownership/sequence checks, with no row projection."""
    require(snapshot.get('schema') == state.get('schema') == 27, 'This profile requires schema27.')
    database = snapshot['database']
    baseline = op.trusted_schema_baseline(27, op.schema27_binding(state))
    op.trusted_schema_baseline(25, op.schema25_binding(state))
    op.trusted_schema_baseline(26, op.schema26_binding(state))
    metadata, tables = database.get('metadata', {}), database.get('tables', {})
    require(metadata.get('database') == op.ROLE and type(metadata.get('server_version_num')) is int and
            metadata['server_version_num'] // 10000 == 17 and metadata.get('schemas') == ['public'] and database.get('unsupported') is False,
            'The complete database belongs to another catalog or contains unsupported objects.')
    require(op.equal_json(database.get('catalog'), baseline['objects']), 'The complete schema27 catalog changed.')
    columns = op.baseline_table_columns(baseline)
    require(len(columns) == 35 and set(tables) == set(columns) and op.equal_json(metadata.get('columns'), columns),
            'The complete table/column population changed.')
    require(all(isinstance(rows, list) and all(isinstance(row, dict) and set(row) == set(columns[name]) for row in rows)
                for name, rows in tables.items()), 'A complete table omitted or added fields.')
    require([{'version': row['version'], 'name': row['name']} for row in sorted(tables['schema_migrations'], key=lambda row: row['version'])] ==
            [{'version': row['version'], 'name': row['name']} for row in baseline['migrations']], 'The migration history changed.')
    expected_users = {state['admin_id'], state['viewer_id'], state['added_viewer']['user_id']}
    users = keyed(tables['users'], 'id')
    require(set(users) == expected_users and users[state['admin_id']]['is_administrator'] is True and
            users[state['added_viewer']['user_id']]['is_administrator'] is False and
            users[state['added_viewer']['user_id']]['is_disabled'] is False and
            any(row['key'] == 'server_id' and row['value'] == state['server_id'] for row in tables['server_settings']),
            'The owned three-user/server population changed.')
    relation_names = {row['name'] for row in baseline['objects'] if row['kind'] == 'relation'}
    require(set(metadata.get('relations', {})) == relation_names and all(row.get('owner') == op.ROLE for row in metadata['relations'].values()),
            'A complete relation ownership fact changed.')
    op.validate_theme_state(tables)
    sequences = {row['Name']: row for row in baseline['catalog']['Sequences']}
    require(set(database.get('sequences', {})) == set(sequences), 'The complete sequence population changed.')
    for name, definition in sequences.items():
        sequence = database['sequences'][name]
        require(set(sequence) == {'last_value', 'is_called'} and type(sequence['last_value']) is int and type(sequence['is_called']) is bool and
                definition['MinValue'] <= sequence['last_value'] <= definition['MaxValue'], 'A sequence state is malformed.')
        next_id = sequence['last_value'] + (definition['Increment'] if sequence['is_called'] else 0)
        require(next_id <= definition['MaxValue'] and all(all(type(row[consumer['Column']]) is int and row[consumer['Column']] < next_id
                for row in tables[consumer['Table']]) for consumer in definition['Consumers']), 'A sequence may collide with a retained identity.')
    require(snapshot.get('runtime_sha256') == state['runtime_sha256'] and snapshot.get('browser_sha256') == state['browser_sha256'] and
            op.equal_json(snapshot.get('added_viewer_credentials'), op.added_viewer_credentials(state)), 'A private credential/runtime binding changed.')


def old_rows_and_additions(op, before, after):
    left, right = before['database'], after['database']
    require(set(left['tables']) == set(right['tables']) and op.equal_json(left['catalog'], right['catalog']), 'The complete catalog changed.')
    for key in ('database', 'server_version_num', 'schemas', 'public_schema', 'relations', 'columns'):
        require(op.equal_json(left['metadata'][key], right['metadata'][key]), 'A database/catalog identity changed.')
    for key in ('runtime_sha256', 'browser_sha256', 'recovery', 'added_viewer_credentials'):
        require(op.equal_json(before[key], after[key]), 'A private credential, runtime or recovery value changed.')
    additions = {}
    for name, rows in left['tables'].items():
        old = Counter(op.canonical_json(row) for row in rows)
        current = Counter(op.canonical_json(row) for row in right['tables'][name])
        require(not old - current, 'An old business row changed or disappeared: ' + name)
        remaining = current - old
        additions[name] = [row for row in right['tables'][name] if remaining[op.canonical_json(row)] > 0]
        if name not in ADDITIVE_TABLES:
            require(not additions[name], 'An unapproved table received new rows: ' + name)
    return additions


def validate_catalog_profile(op, before, after, receipt, media):
    additions = old_rows_and_additions(op, before, after)
    tables = after['database']['tables']
    library_id, root_id = receipt['library_id'], receipt['root_id']
    require(re.fullmatch('[0-9a-f]{32}', library_id) and re.fullmatch('[0-9a-f]{32}', root_id), 'A new library/root identity is malformed.')
    libraries = additions['libraries']
    require(len(libraries) == 1 and libraries[0]['id'] == library_id and libraries[0]['name'] == LIBRARY_NAME and
            libraries[0]['collection_type'] == 'movies' and in_window(libraries[0]['created_at'], receipt) and
            in_window(libraries[0]['last_scan_at'], receipt), 'The one new Movies library differs from its intent.')
    require(additions['library_roots'] == [{'id': root_id, 'library_id': library_id, 'path': ROOT, 'allowed_path': ROOT, 'relative_path': '.'}],
            'The new registered root is not the exact approved leaf directory.')
    new_items = keyed(additions['items'], 'id')
    require(len(new_items) == 9 and len(before['database']['tables']['items']) == 13 and len(tables['items']) == 22 and
            all(row['library_id'] == library_id for row in new_items.values()), 'The exact nine-item addition changed.')
    roots = [row for row in new_items.values() if row['id'] == library_id]
    require(len(roots) == 1 and roots[0]['type'] == 'CollectionFolder' and roots[0]['is_folder'] is True and
            roots[0]['root_id'] is None and roots[0]['parent_id'] is None and roots[0]['path'] == roots[0]['relative_path'] == '' and
            roots[0]['name'] == LIBRARY_NAME and roots[0]['media'] is None, 'The new virtual library item changed.')
    physical = {row['relative_path']: row for row in new_items.values() if row['id'] != library_id}
    require(len(physical) == 8 and set(physical) == {POSITIVE, EMPTY, *(spec[0] for spec in FILES.values())},
            'An unapproved, nested, text or reserved-directory item entered the catalog.')
    mapping = {'library': library_id}
    for key, relative in (('positive_folder', POSITIVE), ('empty_folder', EMPTY)):
        row = physical[relative]
        require(row['type'] == 'Folder' and row['is_folder'] is True and row['root_id'] == root_id and
                row['parent_id'] == library_id and row['path'] == ROOT + '/' + relative and row['name'] == relative and row['media'] is None,
                'A new ordinary directory changed its shape or ownership.')
        mapping[key] = row['id']
    for key, (relative, item_type, name) in FILES.items():
        row, source = physical[relative], media['special']['files']['Movies/' + relative]
        expected_parent = mapping['positive_folder' if key == 'positive' else 'empty_folder'] if key in ('positive', 'empty') else physical[FILES['positive'][0]]['id']
        require(row['type'] == item_type and row['is_folder'] is False and row['root_id'] == root_id and
                row['parent_id'] == expected_parent and row['path'] == ROOT + '/' + relative and row['name'] == name and
                row['file_size'] == source['bytes'] and row['file_identity'] == str(source['identity']['device']) + ':' + str(source['identity']['inode']),
                'A new media item lost its exact path, kind, parent or file identity.')
        info = row['media']
        require(isinstance(info, dict) and info.get('ProbeVersion') == 6 and info.get('FileChangeTimeNs') == source['identity']['ctimeNs'] and
                info.get('Size') == source['bytes'] and isinstance(info.get('Streams'), list) and
                {stream.get('CodecType') for stream in info['Streams']} == {'video', 'audio'}, 'A resource lacks its actual guarded media facts.')
        if key in KINDS:
            require(row['local_metadata'] is None and row['local_metadata_hash'] == row['local_metadata_path'] == '' and
                    row['sort_name'] == name and row['overview'] == '', 'An extra inherited unapproved owner metadata or naming.')
        else:
            nfo = 'Movies/' + (POSITIVE if key == 'positive' else EMPTY) + '/movie.nfo'
            require(row['local_metadata_path'] == (POSITIVE if key == 'positive' else EMPTY) + '/movie.nfo' and
                    row['local_metadata_hash'] == media['special']['files'][nfo]['sha256'], 'A main movie does not bind its exact NFO.')
        mapping[key] = row['id']
    require(all(in_window(row['created_at'], receipt) and in_window(row['updated_at'], receipt) for row in new_items.values()),
            'A new item timestamp escaped the operation window.')
    expected_links = [{'resource_item_id': mapping[key], 'owner_item_id': mapping['positive'], 'kind': kind, 'active': True} for key, kind in KINDS.items()]
    expected_markers = [{'root_id': root_id, 'relative_path': path, 'is_directory': True} for path in MARKERS]
    require(sorted(op.canonical_json(row) for row in tables['item_extra_resources']) == sorted(op.canonical_json(row) for row in expected_links) and
            sorted(op.canonical_json(row) for row in tables['extra_reserved_paths']) == sorted(op.canonical_json(row) for row in expected_markers),
            'The nonempty profile does not have exactly its three reservations and four valid active relationships.')
    require(tables['item_theme_resources'] == before['database']['tables']['item_theme_resources'] == [] and
            tables['theme_reserved_paths'] == before['database']['tables']['theme_reserved_paths'] == [], 'The explicit profile cannot adopt Theme history.')
    owners = additions['theme_owner_ids']
    require(len(owners) == 9 and {row['item_id'] for row in owners} == set(new_items) and
            all(row['virtual_root'] is False for row in owners), 'New Theme owner-number mappings do not cover exactly the new items.')
    metadata = keyed(additions['item_metadata_state'], 'item_id')
    require(set(metadata) == set(new_items), 'Metadata state is not limited to the nine new items.')
    for item_id, row in metadata.items():
        item = new_items[item_id]
        require(row['overrides'] == row['locked_values'] == row['music_source'] == {} and row['last_edited_by'] is None and
                row['last_edited_at'] is None and type(row['revision']) is int and 1 <= row['revision'] <= 2 and
                in_window(row['updated_at'], receipt) and isinstance(row['automatic'], dict) and isinstance(row['source_key'], dict) and
                row['automatic'].get('Name') == item['name'] and row['automatic'].get('SortName') == item['sort_name'] and
                row['source_key'].get('Path') == item['path'] and row['source_key'].get('RootId') == item['root_id'] and
                row['source_key'].get('ParentId') == item['parent_id'] and row['source_key'].get('RelativePath') == item['relative_path'],
                'A new metadata row contains manual edits, unrelated ownership or unexplained revisions.')
    jobs = additions['scan_jobs']
    require(len(jobs) == 1 and jobs[0]['id'] == receipt['job_id'] and jobs[0]['library_id'] == library_id and
            jobs[0]['status'] == 'Completed' and jobs[0]['error'] == '' and jobs[0]['force_probe'] is False and
            jobs[0]['cancel_requested'] is False and (jobs[0]['scanned'], jobs[0]['added'], jobs[0]['updated']) == (6, 6, 0) and
            all(in_window(jobs[0][field], receipt) for field in ('created_at', 'started_at', 'finished_at')),
            'The single normal scan did not complete exactly six new media files without warnings.')
    return mapping, additions


def validate_auth_audit(op, additions, receipt, state):
    auth = receipt['authentication']
    sessions = additions['sessions']
    require(len(sessions) == 2 and set(auth) == {'admin', 'viewer'} and all(
        auth[role]['login_status'] == 200 and auth[role]['logout_status'] == 204 and auth[role]['exact_status'] == 401 for role in auth),
        'Both new authentication scopes must close with exact-token evidence.')
    owned = {}
    for role, user_id, kind in (('admin', state['admin_id'], 'admin'), ('viewer', state['added_viewer']['user_id'], 'emby')):
        matches = [row for row in sessions if row['token_hash'] == '\\x' + auth[role]['token_sha256']]
        require(len(matches) == 1, 'A new session is not bound to its exact acknowledged token digest.')
        row = matches[0]
        require(row['user_id'] == user_id and row['kind'] == kind and row['revoked_at'] is not None and
                all(in_window(row[field], receipt) for field in ('created_at', 'last_seen_at', 'revoked_at')) and
                timestamp(row['expires_at']) > timestamp(row['created_at']), 'A new authentication row has unowned identity or time.')
        if role == 'admin':
            require(row['device_registry_id'] is None and row['client_name'] == 'Goby Dashboard' and row['device_id'] == 'goby-dashboard',
                    'The native administrator unexpectedly registered another device.')
        else:
            require(row['id'] == auth[role]['session_id'] and row['device_id'] == receipt['device_id'] and
                    row['client_name'] == receipt['client_name'] and row['device_name'] == 'Linux Recorder' and row['client_version'] == '1.0',
                    'The ordinary session changed its received identity or client metadata.')
        owned[role] = row
    devices = additions['devices']
    require(len(devices) == 1 and devices[0]['id'] == owned['viewer']['device_registry_id'] and
            devices[0]['reported_device_id'] == receipt['device_id'] and devices[0]['last_user_id'] == state['added_viewer']['user_id'] and
            devices[0]['reported_name'] == 'Linux Recorder' and devices[0]['app_name'] == receipt['client_name'] and
            devices[0]['app_version'] == '1.0' and devices[0]['custom_name'] is None and devices[0]['deleted_at'] is None and
            devices[0]['revision'] == 1 and devices[0]['ip_address'] == '127.0.0.1' and
            in_window(devices[0]['created_at'], receipt) and in_window(devices[0]['last_seen_at'], receipt), 'The new device registry row is not exactly owned.')
    expected = []
    for role, row in owned.items():
        expected.extend((action, 'native' if role == 'admin' else 'emby', 'user', row['user_id'], row['id'], 'session', row['id'])
                        for action in ('session.login', 'session.revoked'))
    administrator = owned['admin']
    expected.extend((action, 'native', 'user', administrator['user_id'], administrator['id'], resource, target)
                    for action, resource, target in (('library.created', 'library', receipt['library_id']), ('scan.requested', 'scan', receipt['job_id'])))
    expected.append(('scan.finished', 'system', 'system', '', '', 'scan', receipt['job_id']))
    events = additions['activity_entries']
    fields = ('action', 'source', 'actor_kind', 'actor_id', 'actor_credential_id', 'resource_kind', 'resource_id')
    require(len(events) == 7 and Counter(tuple(row[field] for field in fields) for row in events) == Counter(expected) and
            all(row['severity'] == 'Info' and row['revision'] == 0 and row['changed_fields'] == [] and
                row['state'] == ('completed' if row['action'] == 'scan.finished' else '') and
                row['affected_count'] == (1 if row['action'] in ('library.created', 'session.revoked') else 0) and
                in_window(row['created_at'], receipt) for row in events), 'The audit increment is not the seven owned committed actions.')
    return owned


def validate_sequences(op, before, after, additions):
    increments = {'theme_owner_ids_id_seq': ('theme_owner_ids', 9), 'devices_id_seq': ('devices', 1),
                  'activity_entries_id_seq': ('activity_entries', 7)}
    left, right = before['database']['sequences'], after['database']['sequences']
    require(set(left) == set(right), 'The sequence inventory changed.')
    for name, sequence in left.items():
        if name not in increments:
            require(op.equal_json(sequence, right[name]), 'An unrelated sequence changed.')
            continue
        table, count = increments[name]
        start = sequence['last_value'] + int(sequence['is_called'])
        require(right[name] == {'last_value': start + count - 1, 'is_called': True} and
                sorted(row['id'] for row in additions[table]) == list(range(start, start + count)), 'A sequence increment is not explained by its exact owned rows.')


def validate_transition(op, before, after, state, receipt, media):
    validate_structure(op, before, state)
    validate_structure(op, after, state)
    mapping, additions = validate_catalog_profile(op, before, after, receipt, media)
    owned = validate_auth_audit(op, additions, receipt, state)
    validate_sequences(op, before, after, additions)
    return {'item_ids': mapping, 'new_item_count': 9, 'old_item_count': 13, 'table_count': 35,
            'native_session_id': owned['admin']['id'], 'viewer_session_id': owned['viewer']['id'],
            'new_device_id': additions['devices'][0]['id'], 'old_rows_preserved': True, 'expected_increment_preserved': True}
