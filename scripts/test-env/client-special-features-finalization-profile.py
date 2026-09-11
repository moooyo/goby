"""Exact fourth-stage proof after the fully indexed, retained protocol failure."""

MARKER = 'goby-client-special-features-protocol-finalization-v1'
PINS = {
    'continuation_failure': '5696a8f7b57f2fec0b2a6bc4d056426f02faaa860ef830c4abc7989e49a54f4d',
    'continuation_state': '0897f2bec4723d5a69df8b35978bc4be32e7ea91f1f11aebfda4c500c2116e83',
    'continuation_snapshot': '550b6f83cd804485cf227ee3514fb6dec0af550d61ec5926303ce589047877f8',
    'continuation_setup_source': 'f5b448b5715456e0539b79afa84fab0ad53acee00bc6e5fb77051ff49131fca8',
    'continuation_profile_source': '71d549fd60193d6813ad97a4cc587460c14d47fc0a96ce74c0367b4acf45019a',
    'retained_structure_proof': '3a6b4d35b27944335bf9838f64618dc41254a79447b7cb9bceb540be1ca540c0',
    'retained_protocol_proof': 'db65b1016efed40297ab3014105d4748bd8febc5ba4a3d8284721872c3045c21',
}
SCAN_JOB_ID = 'd7aa0acaee023dd4c82ea7a303c354ca'


class FinalizationError(Exception):
    """The finalization exceeds its one fresh viewer and original-byte scope."""


def require(value, message):
    if not value:
        raise FinalizationError(message)


def require_quiescent(snapshot):
    """Activity gates only; complete layout identity is proved separately."""
    tables = snapshot['database']['tables']
    require(snapshot.get('schema') == 27 and len(tables) == 35 and len(tables['items']) == 22 and len(tables['libraries']) == 4,
            'The finalizer requires the complete scanned profile.')
    require(tables.get('encoding_jobs') == [], 'Encoding work exists.')
    for table, field in (('scan_jobs', 'status'), ('task_runs', 'state'), ('task_run_children', 'state')):
        require(not any(str(row.get(field, '')).lower() in {'waiting', 'pending', 'queued', 'running', 'stopping'} for row in tables[table]),
                'A scan or scheduled task remains active.')
    sessions = {row['id']: row for row in tables['sessions']}
    for row in tables['play_sessions']:
        require(row.get('state') not in ('Playing', 'Paused'), 'Playback remains active.')
        if row.get('state') == 'Prepared':
            credential = sessions.get(row.get('auth_session_id'), {})
            require(isinstance(row.get('user_id'), str) and row['user_id'] and isinstance(row.get('auth_session_id'), str) and row['auth_session_id'] and
                    'started_at' in row and row['started_at'] is None and row.get('counted') is False and
                    'application_client_id' in row and row['application_client_id'] is None and credential.get('kind') == 'emby' and
                    credential.get('user_id') == row['user_id'] and credential.get('revoked_at') is not None,
                    'Prepared history lacks an unstarted, revoked ordinary credential.')


def validate(op, original_profile, scan_profile, original, created, scanned, final, state, creation_failure, scan_receipt, receipt, media):
    retained = scan_profile.validate_transition(op, original_profile, original, created, scanned, state, creation_failure, scan_receipt, media)
    require(scan_receipt['job_id'] == SCAN_JOB_ID, 'The retained scan is another job.')
    original_profile.validate_structure(op, final, state)
    delta = original_profile.old_rows_and_additions(op, scanned, final)
    require(all(len(rows) == {'sessions': 1, 'devices': 1, 'activity_entries': 2}.get(table, 0) for table, rows in delta.items()),
            'Finalization changed the scanned catalog or created an unapproved business row.')
    require(set(receipt['authentication']) == {'viewer'}, 'Only one fresh ordinary finalizer is permitted.')
    auth = receipt['authentication']['viewer']
    viewer = scan_profile.session(original_profile, delta['sessions'], auth, state['added_viewer']['user_id'], 'emby', receipt['window'])
    require(viewer['id'] == auth['session_id'] and viewer['id'] not in {retained['creation_admin_session_id'], retained['native_session_id'], retained['viewer_session_id']} and
            viewer['device_id'] == receipt['device_id'] and viewer['client_name'] == receipt['client_name'] and viewer['client_version'] == '1.0' and
            viewer['device_name'] == 'Linux Recorder' and viewer['client_capabilities'] == {}, 'The fresh finalizer session differs from its owned login.')
    device = delta['devices'][0]
    require(device['id'] == viewer['device_registry_id'] and device['reported_device_id'] == receipt['device_id'] and
            device['reported_device_id'] not in {row['reported_device_id'] for row in scanned['database']['tables']['devices']} and
            device['last_user_id'] == state['added_viewer']['user_id'] and device['reported_name'] == 'Linux Recorder' and
            device['app_name'] == receipt['client_name'] and device['app_version'] == '1.0' and device['custom_name'] is None and
            device['deleted_at'] is None and device['revision'] == 1 and device['ip_address'] == '127.0.0.1' and
            all(original_profile.in_window(device[key], receipt) for key in ('created_at', 'last_seen_at')), 'The one new finalizer registry row differs.')
    scan_profile.audits(original_profile, delta['activity_entries'], scan_profile.auth_audits(viewer, 'emby'), receipt['window'])
    scan_profile.sequences(op, scanned['database']['sequences'], final['database']['sequences'], delta,
        {'devices_id_seq': ('devices', 1), 'activity_entries_id_seq': ('activity_entries', 2)})
    aggregate = original_profile.old_rows_and_additions(op, original, final)
    require(len(aggregate['sessions']) == 4 and len(aggregate['activity_entries']) == 11, 'The four-stage authentication or audit total differs.')
    scan_profile.sequences(op, original['database']['sequences'], final['database']['sequences'], aggregate,
        {'theme_owner_ids_id_seq': ('theme_owner_ids', 9), 'devices_id_seq': ('devices', 2), 'activity_entries_id_seq': ('activity_entries', 11)})
    proof = {**retained, 'aggregate_session_count': 4, 'aggregate_audit_count': 11,
        'finalization_session_count': 1, 'finalization_audit_count': 2, 'finalizer_session_id': viewer['id'],
        'finalizer_device_id': device['id'], 'all_scanned_rows_preserved': True}
    return retained, proof
