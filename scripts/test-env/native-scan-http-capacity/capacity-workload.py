"""Fixed native scan/HTTP workload; importing this module performs no work.

make_workload returns one narrow CatalogJourney subclass instance. The parent
owns the single APP, namespace, capture connection factory, SQL transport,
workers, corpus, deadlines, resource evidence and final physical closure. It
must install its frozen connection factory in the private journey module before
execute, without patching process-global http.client or sharing it with readers.
Each single-threaded request temporarily sets that private module's BODY_LIMIT
to 512 KiB for catalog GETs or 1 MiB for control responses, then restores it in
finally. The parent's raw capture must enforce the same per-request body cap.

Confirmed plan correction: three users require four credentials: one native
administrator cookie and three Emby tokens. The administrator Emby token is
needed for the actual settled 1,000-leaf HTTP count. Only the two restricted
tokens go to read workers. All four issued tokens are closed and SQL-bound.
The required prior_setup_requests input is the parent's actual pre-workload
readiness HTTP dispatch count, an integer from 0 through 44. It consumes setup
quota without changing the inherited journey.serial, which counts only this
instance's HTTP requests. The setup request count includes that parent number.

callbacks has exactly these callables:
  assert_owned() -> None
  sample_resources() -> None
  snapshot(slot) -> the already read private SQL JSON object described below
  start_readers(phase, run, mappings) -> private birth receipt
  finish_readers(phase, absolute_monotonic_deadline) -> private joined receipt
  record(event) -> persist the complete private event, or raise
  remaining() -> positive finite work-phase seconds remaining

assert_owned checks the current APP/PG/namespace and protected ownership before
each request. sample_resources also checks both reader handles/receipts while
business work runs and raises on a failed or partial worker, so an early reader
failure stops progression without waiting for task completion. A worker's
normal window-end exit is not itself failure. Cleanup must retain the first
business failure without making an already-joined worker's old failure prevent
otherwise owned cancellation or credential closure.

Each mappings entry is restricted to role visible/A or hidden/B and contains
userId, libraryId, libraryLabel, token, clientHeaders, expectedItems and
expectedFavoriteIds. expectedItems is None for cold; for cached it maps all 500
authorized leaf IDs to their selected catalog rows. The caller converts this
data to its frozen worker input. No administrator credential is included.
finish_readers must stop further dispatch and join both owned groups, returning
{phase, runId, joined: true, workers: [{role, userId, libraryId, requestCount,
observedItemIds, status, pages, ...}, ...], ...}. Each pages element is
{shape: "page"|"count", totalRecordCount, items: [{Id, Name, Type, IsFolder,
UserData}, ...]}, selected from the hash-verified actual decoded HTTP body.
status="passed" requires the parent's successful raw evidence and wait checks;
it must never normalize partial/failed exchanges into success. It must retain incomplete overlap as
a metric boundary, rather than treating absent overlap as product failure.

The four workload SQL slots are empty-catalog, cold-catalog,
post-favorite-baseline and cached-catalog, once each. The parent separately owns
the first prestart identity slot and sixth post-APP-stop slot. No SQL is run
here, and no extra observer is requested during cleanup. SQL objects have:
  items: the 12 selected columns from catalogCapacityReadCatalog, with
    root_id and parent_id COALESCE'd to ''. These are id, library_id, root_id,
    parent_id, type, path, relative_path, name, sort_name, is_folder,
    index_number, parent_index_number. Order is irrelevant to this reader.
  library_roots: full rows, including id, library_id and path (all strings).
  libraries: full rows, including id, name and collection_type (all strings).
  user_item_data: every full row, with rowVersion = xmin::text added.
  task_runs, task_run_children, scan_jobs, task_run_requests, task_triggers,
    task_occurrences, play_sessions: every full row of each table.
  sessions: every row's id, user_id, kind, revoked_at and token_hash, where
    token_hash is encode(token_hash,'hex'), not PostgreSQL bytea text.
All table results are complete bounded arrays, not filtered to expected IDs.
SQL timestamps must be emitted from a UTC transaction (SET LOCAL TIME ZONE
'UTC') as PostgreSQL JSON timestamp strings, with a UTC offset, or null. Native
HTTP uses RFC3339 UTC strings. timestamp() accepts either spelling and compares
the actual instant; it does not compare SQL and HTTP timestamp spellings.
All SQL counters/index/ordinal values are JSON integers, never strings/bools.
The SQL history fields consumed are precisely listed by sql_contract().
The parent retains the actual read-only transaction, typed identity, SQL text,
raw output and bounds independently of these pure checks.

execute never closes APP/PG or calls another application start. It leaves the
four credentials open for cleanup. cleanup first joins readers, cancels each
identified active owned task at most once, drains within 60 seconds, then tries
each issued logout once. It accepts the parent's absolute closure deadline and
never resets it. The parent calls validate_final_snapshot after normal APP stop
using the sixth real SQL slot. A failed execution must remain failed even when
cleanup or final responsibility checks succeed.
"""

import copy
from datetime import datetime, timezone
import hashlib
import math
import re
import time


E = '/opt/goby-test/native-scan-http-capacity-20260915'
F = '/opt/goby-native-scan-http-capacity-20260915'
ORIGIN = 'http://127.0.0.1:18099'
MEDIA_ROOT = F + '/media'
ROLES = ('visible', 'hidden')
LABEL_FOR_ROLE = {'visible': 'A', 'hidden': 'B'}
LEAF_TYPES = {'Movie', 'Episode', 'Audio'}
ITEM_COLUMNS = ('id', 'library_id', 'root_id', 'parent_id', 'type', 'path',
                'relative_path', 'name', 'sort_name', 'is_folder',
                'index_number', 'parent_index_number')
TABLES = ('items', 'library_roots', 'libraries', 'user_item_data', 'task_runs',
          'task_run_children', 'scan_jobs', 'task_run_requests', 'task_triggers',
          'task_occurrences', 'sessions', 'play_sessions')
COUNTER_LIMITS = {'setup': 112, 'task-poll': 184, 'cleanup': 64}
TERMINAL = {'completed', 'failed', 'cancelled', 'interrupted'}
ACTIVE = {'pending', 'running', 'stopping'}
CHILD_STATES = {'waiting', 'queued', 'running', 'completed', 'failed',
                'cancelled', 'unavailable', 'interrupted'}
DETAIL_POLL_SLOTS = 91
SKIPPED_DETAIL_SLOTS = frozenset((30, 60))
JOB_FIELDS = {'Id', 'LibraryId', 'ForceProbe', 'Status', 'Error', 'Scanned', 'Added',
              'Updated', 'CreatedAt', 'StartedAt', 'FinishedAt'}


class WorkloadError(RuntimeError):
    """Only a fixed, secret-free error code leaves this module."""


def need(value, code):
    if not value:
        raise WorkloadError(code)


def identifier(value):
    need(isinstance(value, str) and re.fullmatch('[0-9a-f]{32}', value),
         'workload_identifier_contract')
    return value


def timestamp(value):
    need(isinstance(value, str) and len(value) <= 40, 'workload_timestamp_contract')
    try:
        parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    except (ValueError, TypeError):
        raise WorkloadError('workload_timestamp_contract') from None
    need(parsed.tzinfo is not None and parsed.utcoffset().total_seconds() == 0,
         'workload_timestamp_not_utc')
    return parsed.timestamp()


def detail_poll_requested(slot):
    """Keep the original observation slots while reallocating two HTTP calls."""
    need(type(slot) is int and 1 <= slot <= DETAIL_POLL_SLOTS, 'workload_poll_slot_contract')
    return slot not in SKIPPED_DETAIL_SLOTS


def scan_observation_positions(slot, state, recorded):
    """Use existing detail slots, never wait merely to fill measurement gaps."""
    need(type(slot) is int and 1 <= slot <= DETAIL_POLL_SLOTS and
         type(recorded) is int and 0 <= recorded <= 2 and state in ACTIVE | TERMINAL,
         'workload_scan_observation_slot')
    if state not in ('pending', 'running', 'completed') or recorded == 2:
        return ()
    if state == 'completed':
        return ('first', 'second')[recorded:]
    if slot == 2 and recorded == 0:
        return ('first',)
    if slot == 3 and recorded == 1:
        return ('second',)
    need(slot < 3 or recorded == 2, 'workload_scan_observation_slot_missing')
    return ()


def scan_observation_jobs(value, phase, libraries, retained):
    """Allow not-yet-created current jobs, without accepting foreign history."""
    need(phase in ('cold', 'cached') and type(value) is dict and
         set(value) == {'Items', 'TotalRecordCount'} and type(value['Items']) is list and
         type(value['TotalRecordCount']) is int and value['TotalRecordCount'] == len(value['Items']) and
         len(value['Items']) <= (2 if phase == 'cold' else 4) and
         type(libraries) is set and len(libraries) == 2 and type(retained) is dict and
         len(retained) == (0 if phase == 'cold' else 2), 'workload_scan_observation_inventory')
    jobs = {}
    for job in value['Items']:
        need(type(job) is dict and set(job) == JOB_FIELDS, 'workload_scan_observation_job_shape')
        job_id = identifier(job['Id'])
        need(job_id not in jobs and job['LibraryId'] in libraries and job['ForceProbe'] is False and
             job['Status'] in TERMINAL | {'pending', 'running'} and type(job['Error']) is str and
             len(job['Error']) <= 8192 and all(type(job[key]) is int and 0 <= job[key] <= 500
                                            for key in ('Scanned', 'Added', 'Updated')),
             'workload_scan_observation_job_scope')
        created = timestamp(job['CreatedAt'])
        started = None if job['StartedAt'] is None else timestamp(job['StartedAt'])
        finished = None if job['FinishedAt'] is None else timestamp(job['FinishedAt'])
        need((started is None or created <= started) and
             (finished is None or max(created, started if started is not None else created) <= finished),
             'workload_scan_observation_job_times')
        if job['Status'] == 'pending':
            need(started is None and finished is None, 'workload_scan_observation_pending_times')
        elif job['Status'] == 'running':
            need(started is not None and finished is None and job['Error'] == '',
                 'workload_scan_observation_running_times')
        else:
            need(finished is not None, 'workload_scan_observation_terminal_time')
        jobs[job_id] = copy.deepcopy(job)
    need(all(jobs.get(job_id) == job for job_id, job in retained.items()),
         'workload_scan_observation_retained_changed')
    current = [job for job_id, job in jobs.items() if job_id not in retained]
    need(len(current) <= 2 and len({job['LibraryId'] for job in current}) == len(current),
         'workload_scan_observation_current_libraries')
    return jobs


def rows(snapshot, table, maximum):
    need(isinstance(snapshot, dict) and table in TABLES and
         isinstance(snapshot.get(table), list) and len(snapshot[table]) <= maximum and
         all(isinstance(row, dict) for row in snapshot[table]),
         'workload_sql_table_contract')
    return snapshot[table]


def indexed(values, key='id'):
    result = {}
    for row in values:
        identity = identifier(row.get(key))
        need(identity not in result, 'workload_duplicate_sql_identity')
        result[identity] = row
    return result


def selected_catalog(snapshot):
    result = []
    for row in rows(snapshot, 'items', 1076):
        need(all(field in row for field in ITEM_COLUMNS), 'workload_catalog_columns')
        result.append({field: row[field] for field in ITEM_COLUMNS})
    return sorted(result, key=lambda row: row['id'])


def sql_contract():
    """Return required fields and exact full-table bounds without querying."""
    return {
        'items': {'maximum': 1076, 'fields': list(ITEM_COLUMNS)},
        'library_roots': {'maximum': 2, 'fields': ['id', 'library_id', 'path'], 'fullRow': True},
        'libraries': {'maximum': 2, 'fields': ['id', 'name', 'collection_type'], 'fullRow': True},
        'user_item_data': {'maximum': 2, 'fields': ['user_id', 'item_id', 'playback_position_ticks',
            'play_count', 'is_favorite', 'played', 'last_played_at', 'updated_at', 'rowVersion'],
            'fullRow': True, 'rowVersionExpression': 'xmin::text'},
        'task_runs': {'maximum': 2, 'fields': ['id', 'task_id', 'task_key', 'request_id', 'source',
            'actor_user_id', 'actor_session_id', 'actor_kind', 'state', 'trigger_id', 'trigger_revision',
            'scheduled_for', 'scanned', 'added', 'updated', 'total_children', 'terminal_children',
            'completed_children', 'error_code', 'error_message', 'created_at', 'started_at', 'finished_at'],
            'fullRow': True},
        'task_run_children': {'maximum': 4, 'fields': ['id', 'run_id', 'library_id', 'library_name',
            'ordinal', 'state', 'scan_job_id', 'scanned', 'added', 'updated', 'error_code', 'error_message',
            'created_at', 'started_at', 'finished_at'], 'fullRow': True},
        'scan_jobs': {'maximum': 4, 'fields': ['id', 'task_child_id', 'library_id', 'status', 'force_probe',
            'cancel_requested', 'error', 'scanned', 'added', 'updated', 'created_at', 'started_at', 'finished_at'],
            'fullRow': True},
        'task_run_requests': {'maximum': 2, 'fields': ['task_id', 'request_id', 'run_id'], 'fullRow': True},
        'task_triggers': {'maximum': 0, 'fields': [], 'fullRow': True},
        'task_occurrences': {'maximum': 0, 'fields': [], 'fullRow': True},
        'play_sessions': {'maximum': 0, 'fields': [], 'fullRow': True},
        'sessions': {'maximum': 4, 'fields': ['id', 'user_id', 'kind', 'revoked_at', 'token_hash'],
                     'tokenHashExpression': "encode(token_hash,'hex')"},
    }


def corpus_expectations(corpus):
    """Derive physical/synthetic folder counts from the fixed actual recipe."""
    files = corpus.get('files') if isinstance(corpus, dict) else None
    need(isinstance(files, dict) and len(files) == 1020, 'workload_corpus_manifest_contract')
    result = {}
    for label in ('A', 'B'):
        directories, albums, series, seasons = set(), set(), set(), set()
        kinds = {'Movie': 0, 'Episode': 0, 'Audio': 0}
        for relative, source in files.items():
            need(isinstance(source, dict) and source.get('library') in ('A', 'B') and
                 isinstance(source.get('relativePath'), str) and
                 relative == source['library'] + '/' + source['relativePath'] and
                 source.get('path') == MEDIA_ROOT + '/' + relative,
                 'workload_corpus_path_contract')
            if source['library'] != label:
                continue
            parent = source['relativePath'].rsplit('/', 1)[0]
            while parent:
                directories.add(parent)
                parent = parent.rsplit('/', 1)[0] if '/' in parent else ''
            expected = source.get('expected')
            need(isinstance(expected, dict), 'workload_corpus_expected_fields')
            if source.get('kind') == 'AlbumNfo':
                albums.add(expected['album']['relativePath'])
            else:
                need(source.get('kind') in LEAF_TYPES, 'workload_corpus_kind')
                kinds[source['kind']] += 1
                if source['kind'] == 'Episode':
                    series.add(expected['series']['name'])
                    seasons.add((expected['series']['name'], expected['season']['indexNumber']))
        need(kinds == {'Movie': 200, 'Episode': 200, 'Audio': 100} and
             albums.issubset(directories) and len(albums) == 10,
             'workload_corpus_declared_profile')
        counts = dict(kinds, CollectionFolder=1, Folder=len(directories - albums),
                      MusicAlbum=len(albums), Series=len(series), Season=len(seasons))
        result[label] = {'counts': counts, 'physicalDirectories': directories,
                         'albumDirectories': albums}
    need(sum(sum(row['counts'].values()) for row in result.values()) == 1076,
         'workload_corpus_catalog_total')
    return result


def check_catalog(snapshot, libraries, corpus, *, empty=False):
    """Check actual SQL rows against the independently prepared physical map."""
    need(set(libraries) == {'A', 'B'}, 'workload_library_map_contract')
    library_by_id = {identifier(value['Id']): label for label, value in libraries.items()}
    need(len(library_by_id) == 2, 'workload_library_identity_duplicate')
    sql_libraries = indexed(rows(snapshot, 'libraries', 2))
    roots = indexed(rows(snapshot, 'library_roots', 2))
    need(set(sql_libraries) == set(library_by_id) and len(roots) == 2,
         'workload_sql_library_inventory')
    root_for_library = {}
    for root in roots.values():
        library_id = root.get('library_id')
        need(library_id in library_by_id and library_id not in root_for_library and
             root.get('path') == MEDIA_ROOT + '/' + library_by_id[library_id],
             'workload_sql_root_binding')
        root_for_library[library_id] = root['id']
    for library_id, label in library_by_id.items():
        row = sql_libraries[library_id]
        need(row.get('name') == libraries[label]['Name'] and
             row.get('collection_type') == 'mixed', 'workload_sql_library_fields')
    catalog = selected_catalog(snapshot)
    by_id = indexed(catalog)
    need(len(catalog) == (2 if empty else 1076), 'workload_catalog_size')
    by_path, by_relative = {}, {}
    counts = {identity: {} for identity in library_by_id}
    for row in catalog:
        library_id = row['library_id']
        need(library_id in library_by_id and isinstance(row['type'], str) and
             type(row['is_folder']) is bool and
             type(row['index_number']) is int and type(row['parent_index_number']) is int and
             all(isinstance(row[key], str) for key in
                 ('root_id', 'parent_id', 'path', 'relative_path', 'name', 'sort_name')),
             'workload_catalog_row_contract')
        counts[library_id][row['type']] = counts[library_id].get(row['type'], 0) + 1
        if row['type'] == 'CollectionFolder':
            need(row['id'] == library_id and row['is_folder'] is True and
                 row['parent_id'] == '' and row['root_id'] == '',
                 'workload_collection_folder_contract')
            continue
        need(row['root_id'] == root_for_library[library_id] and
             row['parent_id'] in by_id and
             by_id[row['parent_id']]['library_id'] == library_id and
             by_id[row['parent_id']]['is_folder'] is True,
             'workload_catalog_hierarchy_scope')
        relative_key = (library_id, row['relative_path'])
        need(relative_key not in by_relative, 'workload_catalog_duplicate_relative_path')
        by_relative[relative_key] = row
        if row['path']:
            need(row['path'] not in by_path and
                 row['path'] == MEDIA_ROOT + '/' + library_by_id[library_id] + '/' + row['relative_path'],
                 'workload_catalog_physical_path_scope')
            by_path[row['path']] = row
        else:
            need(row['type'] in ('Series', 'Season') and row['is_folder'] is True,
                 'workload_unexpected_synthetic_item')
    expected = None if empty else corpus_expectations(corpus)
    need(all(value == ({'CollectionFolder': 1} if empty else expected[library_by_id[identity]]['counts'])
             for identity, value in counts.items()),
         'workload_catalog_type_counts')
    if empty:
        return {'items': catalog, 'leaves': {}, 'representatives': {}, 'counts': counts}
    files = corpus.get('files') if isinstance(corpus, dict) else None
    need(isinstance(files, dict) and len(files) == 1020, 'workload_corpus_manifest_contract')
    for library_id, label in library_by_id.items():
        physical = {row['relative_path']: row for row in catalog if row['library_id'] == library_id and
                    row['is_folder'] is True and row['path']}
        need(set(physical) == expected[label]['physicalDirectories'], 'workload_physical_folder_map')
        for relative, folder in physical.items():
            kind = 'MusicAlbum' if relative in expected[label]['albumDirectories'] else 'Folder'
            parent_relative = relative.rsplit('/', 1)[0] if '/' in relative else ''
            need(folder['type'] == kind and folder['name'] == relative.rsplit('/', 1)[-1] and
                 folder['sort_name'] == folder['name'].lower() and
                 (folder['parent_id'] == library_id if not parent_relative else
                  by_id[folder['parent_id']]['relative_path'] == parent_relative),
                 'workload_physical_folder_hierarchy')
    leaf_by_id, representatives = {}, {}
    nfo_count = 0
    for relative, source in files.items():
        need(isinstance(source, dict) and source.get('library') in ('A', 'B') and
             source.get('path') == MEDIA_ROOT + '/' + relative and
             relative == source['library'] + '/' + source.get('relativePath', ''),
             'workload_corpus_path_contract')
        if source.get('kind') == 'AlbumNfo':
            nfo_count += 1
            need(source['path'] not in by_path, 'workload_nfo_became_catalog_item')
            continue
        need(source.get('kind') in LEAF_TYPES and isinstance(source.get('expected'), dict),
             'workload_corpus_leaf_contract')
        expected = source['expected']
        item = by_path.get(source['path'])
        label = source['library']
        library_id = libraries[label]['Id']
        need(isinstance(item, dict) and item['library_id'] == library_id and
             item['type'] == source['kind'] and item['is_folder'] is False and
             item['relative_path'] == source['relativePath'] and
             item['name'] == expected.get('name') and item['sort_name'] == expected['name'].lower() and
             item['index_number'] == expected.get('indexNumber') and
             item['parent_index_number'] == expected.get('parentIndexNumber'),
             'workload_leaf_projection_mismatch')
        parent = by_id[item['parent_id']]
        if item['type'] == 'Movie':
            need(parent['type'] == 'Folder' and parent['relative_path'] == 'Movies',
                 'workload_movie_parent_mismatch')
        elif item['type'] == 'Audio':
            album = expected['album']
            need(parent['type'] == 'MusicAlbum' and parent['relative_path'] == album['relativePath'] and
                 parent['name'] == album['name'] and
                 by_id[parent['parent_id']]['relative_path'] == 'Music',
                 'workload_audio_album_mismatch')
        else:
            series = by_id.get(parent['parent_id'])
            need(parent['type'] == 'Season' and parent['path'] == '' and
                 parent['name'] == expected['season']['name'] and
                 parent['index_number'] == expected['season']['indexNumber'] and
                 isinstance(series, dict) and series['type'] == 'Series' and series['path'] == '' and
                 series['name'] == expected['series']['name'] and series['parent_id'] == library_id and
                 series['relative_path'] == expected['series']['relativePath'] and
                 parent['relative_path'] == expected['season']['relativePathPattern'].replace('{seriesId}', series['id']),
                 'workload_episode_hierarchy_mismatch')
        leaf_by_id[item['id']] = copy.deepcopy(item)
        representative_key = label + '-' + item['type']
        prior = representatives.get(representative_key)
        if prior is None or item['relative_path'] < by_id[prior]['relative_path']:
            representatives[representative_key] = item['id']
    need(nfo_count == 20 and len(leaf_by_id) == 1000 and
         set(leaf_by_id) == {row['id'] for row in catalog if row['is_folder'] is False} and
         len(representatives) == 6, 'workload_corpus_catalog_bijection')
    return {'items': catalog, 'leaves': leaf_by_id, 'representatives': representatives,
            'counts': counts}


def check_user_data(snapshot, users, favorites):
    """Preserve all real UserData fields, timestamps and xmin, without filtering."""
    data = rows(snapshot, 'user_item_data', 2)
    expected = {(users[role], item_id) for role, item_id in favorites.items()}
    need(len(data) == len(expected) and len(expected) == len(favorites),
         'workload_userdata_cardinality')
    seen = set()
    for row in data:
        key = (row.get('user_id'), row.get('item_id'))
        need(key in expected and key not in seen and row.get('is_favorite') is True and
             row.get('played') is False and type(row.get('play_count')) is int and row['play_count'] == 0 and
             type(row.get('playback_position_ticks')) is int and row['playback_position_ticks'] == 0 and
             row.get('last_played_at') is None and
             isinstance(row.get('rowVersion'), str) and re.fullmatch('[1-9][0-9]*', row['rowVersion']),
             'workload_userdata_fields')
        timestamp(row.get('updated_at'))
        seen.add(key)
    return sorted(copy.deepcopy(data), key=lambda row: (row['user_id'], row['item_id']))


def make_workload(journey_module, *, origin, media_root, setup_token, passwords,
                  nonce, record_dir, request_ids, corpus, callbacks, prior_setup_requests):
    """Instantiate the reviewed journey only when the parent admits execution."""
    need(origin == ORIGIN and media_root == MEDIA_ROOT and
         record_dir == E + '/private/workload' and
         isinstance(request_ids, dict) and set(request_ids) == {'cold', 'cached'} and
         all(isinstance(value, str) and re.fullmatch('[0-9a-f]{32}', value)
             for value in request_ids.values()) and len(set(request_ids.values())) == 2,
         'workload_frozen_input_contract')
    need(type(prior_setup_requests) is int and 0 <= prior_setup_requests <= 44,
         'workload_prior_setup_request_count')
    expected_callbacks = {'assert_owned', 'sample_resources', 'snapshot',
                          'start_readers', 'finish_readers', 'record', 'remaining'}
    need(isinstance(callbacks, dict) and set(callbacks) == expected_callbacks and
         all(callable(value) for value in callbacks.values()) and
         isinstance(corpus, dict) and corpus.get('root') == MEDIA_ROOT,
         'workload_parent_contract')

    class CapacityWorkload(journey_module.CatalogJourney):
        def __init__(self):
            self.callbacks = callbacks
            self.budget = {key: 0 for key in COUNTER_LIMITS}
            self.budget['setup'] = prior_setup_requests
            self.bucket = 'setup'
            self.capacity_libraries = {}
            self.runs, self.admissions, self.observations = {}, {}, {}
            self.scan_observations = []
            self.child_bindings, self.run_bindings = {}, {}
            self.reader_births, self.reader_joins = {}, {}
            self.active_reader_phase = None
            self.cancel_attempted = set()
            self.favorites = {}
            self.sql_slots = []
            self.last_catalog = self.favorite_baseline = self.cached_snapshot = None
            self.cleanup_result = None
            self.cleanup_deadline = None
            self.task_id = None
            self.executed = self.failed = False
            self.first_failure = None
            self.issued_hashes = []
            super().__init__(origin, media_root, setup_token, passwords, nonce, record_dir)
            self.phase = 'setup'
            self._record({'event': 'capacity-profile', 'scope': E, 'users': 3,
                          'credentialLimit': 4, 'applicationStartsOwnedByParent': 1,
                          'controllerRequestLimit': 360, 'workerRequestLimit': 240,
                          'priorSetupRequests': prior_setup_requests})

        def _record(self, value):
            # The inherited compact trace remains separate from larger private
            # result records, so a 1,076-item map never exceeds its 8 KiB row cap.
            if value.get('event') == 'created':
                value = dict(value, profile='native-scan-http-capacity')
            super()._record(value)

        def _private(self, event, **values):
            return self.callbacks['record'](dict(values, event=event, scope=E,
                                                 observedMonotonic=time.monotonic(),
                                                 observedWall=datetime.now(timezone.utc).isoformat()))

        def _remaining(self):
            value = self.callbacks['remaining']()
            need(type(value) in (int, float) and math.isfinite(value) and value > 0,
                 'workload_work_deadline')
            return float(value)

        def _request(self, method, path, expected, **options):
            need(self.bucket in COUNTER_LIMITS and
                 self.budget[self.bucket] < COUNTER_LIMITS[self.bucket] and self.serial < 360,
                 'workload_controller_request_budget')
            self.callbacks['assert_owned']()
            if self.bucket == 'cleanup':
                need(self.cleanup_deadline is not None and time.monotonic() < self.cleanup_deadline,
                     'workload_cleanup_deadline')
                limit = self.cleanup_deadline
                options['cleanup_request'] = True
            else:
                need(not self.failed, 'workload_stopped_after_failure')
                limit = time.monotonic() + self._remaining()
            supplied_deadline = options.get('deadline')
            need(supplied_deadline is None or
                 (type(supplied_deadline) in (int, float) and math.isfinite(supplied_deadline)),
                 'workload_request_deadline_contract')
            options['deadline'] = limit if supplied_deadline is None else min(limit, supplied_deadline)
            prior_headers = options.get('on_headers')

            def capture_headers(values):
                if prior_headers is not None:
                    prior_headers(values)
                if expected == 401:
                    media_types = [value.split(';', 1)[0].strip().lower()
                                   for key, value in values if key.lower() == 'content-type']
                    want = 'text/plain' if path.startswith('/emby/') else 'application/json'
                    need(media_types == [want], 'workload_401_media_type')

            options['on_headers'] = capture_headers
            self.budget[self.bucket] += 1
            prior_body_limit = journey_module.BODY_LIMIT
            catalog_read = method == 'GET' and path.startswith('/emby/Users/') and '/Items' in path
            journey_module.BODY_LIMIT = (512 << 10) if catalog_read else (1 << 20)
            try:
                return super()._request(method, path, expected, **options)
            finally:
                journey_module.BODY_LIMIT = prior_body_limit

        def _remember(self, kind, role, value):
            credential = super()._remember(kind, role, value)
            credential['tokenHash'] = hashlib.sha256(value.encode('ascii')).hexdigest()
            need(len(self.credentials) <= 4 and credential['tokenHash'] not in self.issued_hashes,
                 'workload_issued_credential_inventory')
            self.issued_hashes.append(credential['tokenHash'])
            self._private('credential-issued', kind=kind, role=role,
                          tokenHash=credential['tokenHash'], phase=self.phase)
            return credential

        def first_start(self):
            raise WorkloadError('workload_legacy_journey_forbidden')

        def second_start(self):
            raise WorkloadError('workload_second_application_start_forbidden')

        def _scan(self, *args, **kwargs):
            raise WorkloadError('workload_per_library_admission_forbidden')

        def _write_user_data(self, *args, **kwargs):
            raise WorkloadError('workload_legacy_userdata_forbidden')

        def _snapshot(self, slot):
            expected = ('empty-catalog', 'cold-catalog', 'post-favorite-baseline', 'cached-catalog')
            need(len(self.sql_slots) < 4 and slot == expected[len(self.sql_slots)],
                 'workload_sql_slot_order')
            self.callbacks['assert_owned']()
            self._remaining()
            self.sql_slots.append(slot)
            value = self.callbacks['snapshot'](slot)
            need(isinstance(value, dict) and set(value) == set(TABLES), 'workload_sql_snapshot_contract')
            self._private('sql-snapshot-read', slot=slot, tables=list(TABLES))
            return value

        def _task_definition(self, *, label, deadline=None):
            value = self._admin_json('GET', '/admin/v1/tasks/' + self.task_id,
                                     label=label, deadline=deadline).get('Task')
            need(isinstance(value, dict) and value.get('Id') == self.task_id and
                 value.get('Key') == 'library.scan' and value.get('Enabled') is True and
                 value.get('Triggers') == [], 'workload_task_definition_changed')
            return value

        def _jobs(self, expected_count, *, label, deadline=None):
            value = self._admin_json('GET', '/admin/v1/jobs', label=label, deadline=deadline)
            jobs = value.get('Items')
            need(isinstance(jobs, list) and len(jobs) == expected_count and
                 value.get('TotalRecordCount') == expected_count,
                 'workload_scan_job_inventory')
            by_id = {}
            libraries = {row['Id'] for row in self.capacity_libraries.values()}
            for job in jobs:
                job_id = identifier(job.get('Id'))
                need(job_id not in by_id and job.get('LibraryId') in libraries and
                     job.get('ForceProbe') is False and job.get('Status') in
                     (TERMINAL | {'pending', 'running'}), 'workload_scan_job_scope')
                by_id[job_id] = job
            return by_id

        def _observe_scan_jobs(self, phase, position, detail, slot, *, deadline):
            expected = [(name, place) for name in ('cold', 'cached')
                        for place in ('first', 'second')]
            need(len(self.scan_observations) < 4 and
                 (phase, position) == expected[len(self.scan_observations)] and self.bucket == 'task-poll',
                 'workload_scan_observation_order')
            label = 'scan-state-' + phase + '-' + position
            before = time.monotonic_ns()
            value = self._admin_json('GET', '/admin/v1/jobs', label=label, deadline=deadline)
            after = time.monotonic_ns()
            jobs = scan_observation_jobs(value, phase,
                {row['Id'] for row in self.capacity_libraries.values()},
                {} if phase == 'cold' else self.jobs['cold'])
            observation = {'phase': phase, 'position': position, 'label': label,
                'detailPollSlot': slot, 'detailState': detail['Run']['State'],
                'collectionPoint': 'terminal-detail' if detail['Run']['State'] == 'completed' else 'periodic-detail',
                'taskId': self.task_id, 'requestId': request_ids[phase],
                'runId': self.admissions[phase]['runId'],
                'children': copy.deepcopy(detail['Children']['Items']),
                'jobs': jobs, 'beforeMonotonicNs': before, 'afterMonotonicNs': after}
            saved = self._private('scan-job-state-observation', phase=phase, observation=observation)
            need(type(saved) is dict and set(saved) == {'observation', 'record'} and
                 type(saved['observation']) is dict and
                 {key: value for key, value in saved['observation'].items() if key != 'controlReceipt'} == observation and
                 type(saved['observation'].get('controlReceipt')) is dict and type(saved['record']) is dict,
                 'workload_scan_observation_receipt_binding')
            self.scan_observations.append(dict(saved['observation'], record=saved['record']))

        def _scheduled_scan_observations(self, phase, slot, detail, *, deadline):
            recorded = sum(row['phase'] == phase for row in self.scan_observations)
            for position in scan_observation_positions(slot, detail['Run']['State'], recorded):
                self._observe_scan_jobs(phase, position, detail, slot, deadline=deadline)

        def _setup(self):
            status = self._request('GET', '/admin/v1/bootstrap', 200, label='bootstrap-status')
            need(status == {'Initialized': False}, 'workload_fixture_already_initialized')
            reply = self._request('POST', '/admin/v1/bootstrap', 201,
                                 headers={'Origin': self.origin},
                                 body={'SetupToken': self.setup_token, 'Name': self.names['admin'],
                                       'Password': self.passwords['admin']}, label='bootstrap')
            self._check_native_user(reply.get('User'), 'admin')
            self._login_admin()
            initial = self._admin_json('GET', '/admin/v1/libraries', label='initial-libraries')
            need(initial == {'Items': [], 'TotalRecordCount': 0}, 'workload_initial_libraries_not_empty')
            initial_jobs = self._admin_json('GET', '/admin/v1/jobs', label='initial-jobs')
            need(initial_jobs == {'Items': [], 'TotalRecordCount': 0}, 'workload_initial_jobs_not_empty')
            for label in ('A', 'B'):
                name, path = 'Capacity ' + label, MEDIA_ROOT + '/' + label
                reply = self._admin_json('POST', '/admin/v1/libraries', 201,
                                         body={'Name': name, 'CollectionType': 'mixed',
                                               'Paths': [path], 'Scan': False},
                                         label='create-library-' + label)
                need(set(reply) == {'Library'}, 'workload_implicit_scan_not_allowed')
                library = reply['Library']
                identifier(library.get('Id'))
                need(library.get('Name') == name and library.get('CollectionType') == 'mixed' and
                     library.get('Paths') == [path], 'workload_library_creation_mismatch')
                self.capacity_libraries[label] = copy.deepcopy(library)
            # The original user-policy method uses these two fixed aliases.
            self.libraries = {'mixed': self.capacity_libraries['A']['Id'],
                              'hidden': self.capacity_libraries['B']['Id']}
            self._create_users()
            for role in ('admin', 'visible', 'hidden'):
                self._login_emby(role)
            need(len(self.users) == 3 and len(self.credentials) == 4,
                 'workload_user_credential_count')
            definitions = self._admin_json('GET', '/admin/v1/tasks', label='discover-library-scan')
            values = definitions.get('Items')
            need(isinstance(values, list) and 1 <= len(values) <= 32 and
                 definitions.get('TotalRecordCount') == len(values) and
                 len({identifier(row.get('Id')) for row in values}) == len(values) and
                 all(row.get('Triggers') == [] and row.get('CurrentRun') is None for row in values),
                 'workload_task_inventory_not_idle')
            matches = [row for row in values if row.get('Key') == 'library.scan']
            need(len(matches) == 1 and matches[0].get('Enabled') is True,
                 'workload_native_scan_task_missing')
            self.task_id = identifier(matches[0]['Id'])
            self._task_definition(label='scan-definition-before-cold')
            self._jobs(0, label='jobs-before-cold')
            self._http_boundary(None, empty=True)
            snapshot = self._snapshot('empty-catalog')
            check_catalog(snapshot, self.capacity_libraries, corpus, empty=True)
            check_user_data(snapshot, self.users, {})
            self._sql_history(snapshot, completed=0)
            self._sessions(snapshot, revoked=False)
            self._private('workload-setup-complete', users=self.users,
                          libraries=self.capacity_libraries, taskId=self.task_id,
                          requestIds=request_ids, requestCounts=self.budget)

        def _capture_admission(self, phase, reply):
            if isinstance(reply, dict) and isinstance(reply.get('Run'), dict):
                run = reply['Run']
                run_id = run.get('Id')
                if isinstance(run_id, str) and re.fullmatch('[0-9a-f]{32}', run_id):
                    need(run.get('TaskId') == self.task_id and run.get('Source') == 'manual' and
                         run.get('RequestId') == request_ids[phase], 'workload_admission_identity_mismatch')
                    self.admissions[phase]['runId'] = run_id
                    self._private('task-admission-identity', phase=phase, run=run)

        def _run_detail(self, phase, *, deadline):
            run_id = self.admissions[phase]['runId']
            before_wall, before_mono = time.time(), time.monotonic()
            value = self._admin_json('GET', '/admin/v1/task-runs/' + run_id + '?StartIndex=0&Limit=2',
                                     label='run-detail-' + phase, deadline=deadline)
            after_mono, after_wall = time.monotonic(), time.time()
            self._check_run(value, phase, terminal=False)
            observation = {'runId': run_id, 'state': value['Run']['State'],
                           'beforeMonotonic': before_mono, 'afterMonotonic': after_mono,
                           'beforeWall': before_wall, 'afterWall': after_wall,
                           'runStartedAt': value['Run'].get('StartedAt'),
                           'runFinishedAt': value['Run'].get('FinishedAt'),
                           'children': copy.deepcopy(value['Children']['Items'])}
            self.observations.setdefault(phase, []).append(observation)
            self._private('task-state-observation', phase=phase, observation=observation)
            return value

        def _check_run(self, value, phase, *, terminal):
            need(isinstance(value, dict) and isinstance(value.get('Run'), dict) and
                 isinstance(value.get('Children'), dict), 'workload_task_detail_contract')
            run, page = value['Run'], value['Children']
            need(run.get('Id') == self.admissions[phase]['runId'] and run.get('TaskId') == self.task_id and
                 run.get('Source') == 'manual' and run.get('RequestId') == request_ids[phase] and
                 run.get('TotalChildren') == 2 and run.get('State') in (ACTIVE | TERMINAL) and
                 page.get('StartIndex') == 0 and page.get('Limit') == 2 and
                 page.get('TotalRecordCount') == 2 and isinstance(page.get('Items'), list) and
                 len(page['Items']) == 2, 'workload_task_detail_identity')
            numeric_fields = ('TotalChildren', 'TerminalChildren', 'CompletedChildren', 'FailedChildren',
                              'CancelledChildren', 'InterruptedChildren', 'UnavailableChildren',
                              'Scanned', 'Added', 'Updated')
            need(all(type(run.get(key)) is int and run[key] >= 0 for key in numeric_fields),
                 'workload_task_counter_types')
            binding = {key: run.get(key) for key in
                       ('Id', 'TaskId', 'Source', 'RequestId', 'CreatedAt', 'TotalChildren')}
            if phase in self.run_bindings:
                need(binding == self.run_bindings[phase], 'workload_run_binding_changed')
            else:
                self.run_bindings[phase] = binding
            children = page['Items']
            libraries = {row['Id']: row for row in self.capacity_libraries.values()}
            need({row.get('LibraryId') for row in children} == set(libraries) and
                 {row.get('Ordinal') for row in children} == {0, 1} and
                 len({identifier(row.get('Id')) for row in children}) == 2,
                 'workload_task_child_inventory')
            for child in children:
                need(child.get('RunId') == run['Id'] and
                     child.get('LibraryName') == libraries[child['LibraryId']]['Name'] and
                     child.get('State') in CHILD_STATES and
                     all(type(child.get(key)) is int and child[key] >= 0 for key in
                         ('Ordinal', 'Scanned', 'Added', 'Updated')), 'workload_task_child_scope')
                if child.get('ScanJobId') is not None:
                    identifier(child['ScanJobId'])
                if child['State'] == 'waiting':
                    need(child.get('ScanJobId') is None and child.get('StartedAt') is None,
                         'workload_waiting_child_contract')
                if child['State'] in ('queued', 'running', 'completed'):
                    identifier(child.get('ScanJobId'))
                child_binding = {key: child.get(key) for key in
                                 ('Id', 'RunId', 'LibraryId', 'LibraryName', 'Ordinal', 'CreatedAt')}
                prior = self.child_bindings.setdefault(phase, {}).get(child['LibraryId'])
                if prior is not None:
                    need(prior['identity'] == child_binding and
                         (prior['scanJobId'] is None or prior['scanJobId'] == child.get('ScanJobId')),
                         'workload_child_binding_changed')
                self.child_bindings[phase][child['LibraryId']] = {
                    'identity': child_binding, 'scanJobId': child.get('ScanJobId')}
            if not terminal:
                return
            need(run['State'] == 'completed' and all(run.get(field) == 2 for field in
                 ('TotalChildren', 'TerminalChildren', 'CompletedChildren')) and
                 all(run.get(field) == 0 for field in ('FailedChildren', 'CancelledChildren',
                     'InterruptedChildren', 'UnavailableChildren')) and run.get('Scanned') == 1000 and
                 run.get('ErrorCode') == '' and run.get('ErrorMessage') == '' and
                 run.get('StopRequestedAt') is None and run.get('StopReason') == '',
                 'workload_task_not_normally_completed')
            need(timestamp(run.get('CreatedAt')) <= timestamp(run.get('StartedAt')) <=
                 timestamp(run.get('FinishedAt')), 'workload_task_time_order')
            expected_added = 1000 if phase == 'cold' else 0
            need(run.get('Added') == expected_added and run.get('Updated') == 0,
                 'workload_task_scan_counters')
            for child in children:
                need(child.get('State') == 'completed' and child.get('Scanned') == 500 and
                     child.get('Added') == expected_added // 2 and child.get('Updated') == 0 and
                     child.get('ErrorCode') == '' and child.get('ErrorMessage') == '' and
                     timestamp(child.get('CreatedAt')) <= timestamp(child.get('StartedAt')) <=
                     timestamp(child.get('FinishedAt')), 'workload_child_not_normally_completed')
            need(len({row['ScanJobId'] for row in children}) == 2,
                 'workload_duplicate_child_scan_job')

        def _reader_mappings(self, phase):
            result = {}
            for role in ROLES:
                label = LABEL_FOR_ROLE[role]
                library_id = self.capacity_libraries[label]['Id']
                result[role] = {'userId': self.users[role], 'libraryId': library_id,
                                'libraryLabel': label, 'token': self.emby[role]['value'],
                                'clientHeaders': self._client_headers(role),
                                'expectedItems': None if phase == 'cold' else
                                {identity: row for identity, row in self.last_catalog['leaves'].items()
                                 if row['library_id'] == library_id},
                                'expectedFavoriteIds': [] if phase == 'cold' else [self.favorites[role]]}
            return result

        def _join_readers(self, phase, deadline, *, normal=True):
            if phase in self.reader_joins:
                return self.reader_joins[phase]
            joined = self.callbacks['finish_readers'](phase, deadline)
            need(isinstance(joined, dict) and joined.get('phase') == phase and
                 joined.get('runId') == self.admissions[phase]['runId'] and joined.get('joined') is True and
                 isinstance(joined.get('workers'), list) and len(joined['workers']) <= 2,
                 'workload_reader_join_contract')
            self.reader_joins[phase] = copy.deepcopy(joined)
            self.active_reader_phase = None
            self._private('reader-phase-joined', phase=phase, result=joined)
            need(not normal or len(joined['workers']) == 2, 'workload_reader_pair_incomplete')
            return joined

        def _phase(self, phase):
            need(phase in ('cold', 'cached') and phase not in self.admissions and
                 self.active_reader_phase is None and not self.failed and
                 (phase == 'cold' or set(self.runs) == {'cold'}), 'workload_phase_order')
            self.phase = phase
            self.bucket = 'setup'
            definition = self._task_definition(label='task-idle-before-' + phase)
            need(definition.get('CurrentRun') is None, 'workload_task_active_before_admission')
            need(self._remaining() > 470, 'workload_task_and_join_reservation')
            before_wall, before_mono = time.time(), time.monotonic()
            deadline = before_mono + 450.0
            self.admissions[phase] = {'requestId': request_ids[phase], 'runId': None,
                                      'attempted': True, 'beforeMonotonic': before_mono,
                                      'beforeWall': before_wall, 'deadline': deadline}
            self._private('task-admission-intent', phase=phase, admission=self.admissions[phase])
            reply = self._request('POST', '/admin/v1/tasks/' + self.task_id + '/runs', 202,
                                  body={'RequestId': request_ids[phase]}, credential=self.admin,
                                  label='admit-' + phase, on_json=lambda value: self._capture_admission(phase, value),
                                  deadline=deadline)
            need(reply.get('Admitted') is True and self.admissions[phase]['runId'] is not None and
                 reply.get('Run', {}).get('TotalChildren') == 2 and
                 len({row['runId'] for row in self.admissions.values()}) == len(self.admissions),
                 'workload_task_not_freshly_admitted')
            self.admissions[phase]['accepted'] = True
            self.bucket = 'task-poll'
            detail = self._run_detail(phase, deadline=deadline)
            need(detail['Run']['State'] in ('pending', 'running', 'completed'),
                 'workload_no_readers_after_task_failure')
            self.active_reader_phase = phase
            birth = self.callbacks['start_readers'](phase, copy.deepcopy(detail['Run']),
                                                    self._reader_mappings(phase))
            self.reader_births[phase] = copy.deepcopy(birth)
            self._private('reader-phase-started', phase=phase, receipt=birth)
            poll_count, poll_slot_count, last_poll = 1, 1, time.monotonic()
            self._scheduled_scan_observations(phase, poll_slot_count, detail, deadline=deadline)
            skipped_slots = []
            while detail['Run']['State'] not in TERMINAL:
                need(detail['Run']['State'] in ('pending', 'running') and poll_slot_count < DETAIL_POLL_SLOTS,
                     'workload_task_unexpected_active_state')
                due = last_poll + 5.0
                while time.monotonic() < due:
                    self.callbacks['assert_owned']()
                    self.callbacks['sample_resources']()
                    need(time.monotonic() < deadline, 'workload_task_deadline')
                    delay = min(2.0, due - time.monotonic(), deadline - time.monotonic())
                    if delay > 0:
                        time.sleep(delay)
                need(time.monotonic() < deadline, 'workload_task_deadline')
                poll_slot_count += 1
                if detail_poll_requested(poll_slot_count):
                    detail = self._run_detail(phase, deadline=deadline)
                    poll_count += 1
                    last_poll = time.monotonic()
                    self._scheduled_scan_observations(phase, poll_slot_count, detail, deadline=deadline)
                else:
                    self.callbacks['assert_owned']()
                    self.callbacks['sample_resources']()
                    skipped_slots.append(poll_slot_count)
                    last_poll = time.monotonic()
            self._join_readers(phase, min(time.monotonic() + 15.0,
                                         time.monotonic() + self._remaining()))
            self._check_run(detail, phase, terminal=True)
            jobs = self._jobs(2 if phase == 'cold' else 4, label='terminal-jobs-' + phase,
                              deadline=time.monotonic() + self._remaining())
            expected_jobs = {child['ScanJobId'] for child in detail['Children']['Items']}
            if phase == 'cached':
                expected_jobs |= {child['ScanJobId'] for child in self.runs['cold']['Children']['Items']}
            need(set(jobs) == expected_jobs, 'workload_unowned_or_missing_scan_job')
            for child in detail['Children']['Items']:
                job = jobs[child['ScanJobId']]
                need(job.get('LibraryId') == child['LibraryId'] and job.get('Status') == 'completed' and
                     all(type(job.get(key)) is int for key in ('Scanned', 'Added', 'Updated')) and
                     job.get('Scanned') == 500 and job.get('Added') == (500 if phase == 'cold' else 0) and
                     job.get('Updated') == 0 and job.get('Error') == '' and
                     timestamp(job.get('CreatedAt')) <= timestamp(job.get('StartedAt')) <=
                     timestamp(job.get('FinishedAt')), 'workload_job_not_normally_completed')
            self.runs[phase] = copy.deepcopy(detail)
            self.jobs[phase] = copy.deepcopy(jobs)
            self.bucket = 'setup'
            need(self._task_definition(label='task-idle-after-' + phase).get('CurrentRun') is None,
                 'workload_task_still_current_after_completion')
            self.callbacks['sample_resources']()
            self._private('task-phase-complete', phase=phase, detail=detail, jobs=jobs,
                          pollCount=poll_count, pollSlotCount=poll_slot_count,
                          skippedDetailSlots=skipped_slots, scanJobObservationCount=2,
                          durationSeconds=timestamp(detail['Run']['FinishedAt']) -
                                          timestamp(detail['Run']['StartedAt']))

        def _page(self, role, *, count_only, label):
            values = {'Recursive': 'true', 'IncludeItemTypes': 'Movie,Episode,Audio'}
            if count_only:
                values['Limit'] = 0
            else:
                values.update({'SortBy': 'SortName', 'StartIndex': 0, 'Limit': 64})
            return self._list(role, values, label=label)

        def _check_page(self, page, role, catalog, *, count_only, empty):
            count = 0 if empty else (1000 if role == 'admin' else 500)
            need(isinstance(page, dict) and isinstance(page.get('Items'), list) and
                 type(page.get('TotalRecordCount')) is int and page['TotalRecordCount'] == count and
                 len(page['Items']) == (0 if count_only or empty else 64),
                 'workload_settled_http_count')
            seen = set()
            for item in page['Items']:
                item_id = identifier(item.get('Id'))
                need(item_id not in seen and item.get('Type') in LEAF_TYPES and item.get('IsFolder') is False and
                     catalog is not None and item_id in catalog['leaves'], 'workload_settled_http_item')
                row = catalog['leaves'][item_id]
                allowed = role == 'admin' or row['library_id'] == self.capacity_libraries[LABEL_FOR_ROLE[role]]['Id']
                need(allowed and item.get('Name') == row['name'] and item.get('Type') == row['type'],
                     'workload_settled_http_acl_or_identity')
                self._check_http_user_data(item, role, item_id)
                seen.add(item_id)

        def _check_http_user_data(self, item, role, item_id):
            data = item.get('UserData')
            need(isinstance(data, dict) and data.get('IsFavorite') is (self.favorites.get(role) == item_id) and
                 data.get('Played') is False and type(data.get('PlayCount')) is int and data['PlayCount'] == 0 and
                 type(data.get('PlaybackPositionTicks')) is int and data['PlaybackPositionTicks'] == 0 and
                 data.get('LastPlayedDate') is None, 'workload_http_userdata_mismatch')

        def _http_boundary(self, catalog, *, empty=False):
            for role in ('admin', 'visible', 'hidden'):
                for count_only in (False, True):
                    label = 'boundary-' + role + ('-count' if count_only else '-page')
                    self._check_page(self._page(role, count_only=count_only, label=label), role, catalog,
                                     count_only=count_only, empty=empty)
            for role in ROLES:
                other = 'hidden' if role == 'visible' else 'visible'
                path = '/emby/Users/' + self.users[other] + '/Items?Limit=0'
                self._request('GET', path, 403, credential=self.emby[role], label='cross-user-' + role)
                if empty:
                    continue
                label = LABEL_FOR_ROLE[role]
                hidden_id = catalog['representatives'][LABEL_FOR_ROLE[other] + '-Movie']
                self._request('GET', '/emby/Users/' + self.users[role] + '/Items/' + hidden_id,
                              404, credential=self.emby[role], label='hidden-item-' + role)
                for kind in ('Movie', 'Episode', 'Audio'):
                    item_id = catalog['representatives'][label + '-' + kind]
                    item = self._detail(role, item_id, 'representative-' + role + '-' + kind)
                    row = catalog['leaves'][item_id]
                    need(item.get('Id') == item_id and item.get('Type') == kind and
                         item.get('Name') == row['name'] and item.get('ParentId') == row['parent_id'],
                         'workload_http_representative_mismatch')
                    self._check_http_user_data(item, role, item_id)
                    if kind == 'Episode':
                        all_items = {value['id']: value for value in catalog['items']}
                        season = all_items[row['parent_id']]
                        need(item.get('SeasonId') == season['id'] and
                             item.get('SeriesId') == season['parent_id'] and
                             item.get('IndexNumber') == row['index_number'] and
                             item.get('ParentIndexNumber') == row['parent_index_number'],
                             'workload_http_episode_hierarchy')
                    elif kind == 'Audio':
                        need(item.get('AlbumId') == row['parent_id'] and
                             item.get('IndexNumber') == row['index_number'], 'workload_http_audio_hierarchy')

        def _seed_favorites(self):
            need(not self.favorites and self.last_catalog is not None, 'workload_favorites_already_attempted')
            self.phase = 'favorites'
            for role in ROLES:
                item_id = self.last_catalog['representatives'][LABEL_FOR_ROLE[role] + '-Movie']
                # Mark the intended owned write before dispatch; never retry it.
                self.favorites[role] = item_id
                reply = self._emby_json(role, '/emby/Users/' + self.users[role] + '/FavoriteItems/' + item_id,
                                        method='POST', label='favorite-' + role)
                need(reply == {'PlaybackPositionTicks': 0, 'PlayCount': 0,
                               'IsFavorite': True, 'Played': False}, 'workload_favorite_projection')
            snapshot = self._snapshot('post-favorite-baseline')
            catalog = check_catalog(snapshot, self.capacity_libraries, corpus)
            need(catalog['items'] == self.last_catalog['items'], 'workload_favorites_changed_catalog')
            self.favorite_baseline = check_user_data(snapshot, self.users, self.favorites)
            self._sql_history(snapshot, completed=1)
            self._private('favorite-baseline', favorites=self.favorites,
                          userData=self.favorite_baseline, catalog=catalog)

        def _reconcile_readers(self, phase, catalog):
            joined = self.reader_joins[phase]
            workers = joined['workers']
            need({row.get('role') for row in workers} == set(ROLES), 'workload_reader_roles')
            total = 0
            for worker in workers:
                role = worker['role']
                library_id = self.capacity_libraries[LABEL_FOR_ROLE[role]]['Id']
                need(worker.get('userId') == self.users[role] and worker.get('libraryId') == library_id and
                     type(worker.get('requestCount')) is int and 0 <= worker['requestCount'] <= 60 and
                     worker.get('status') == 'passed' and isinstance(worker.get('observedItemIds'), list) and
                     isinstance(worker.get('pages'), list) and len(worker['pages']) == worker['requestCount'],
                     'workload_reader_result_contract')
                need(all(identity in catalog['leaves'] and catalog['leaves'][identity]['library_id'] == library_id
                         for identity in worker['observedItemIds']), 'workload_reader_observation_acl')
                observed = set()
                for page in worker['pages']:
                    need(isinstance(page, dict) and page.get('shape') in ('page', 'count') and
                         type(page.get('totalRecordCount')) is int and 0 <= page['totalRecordCount'] <= 500 and
                         isinstance(page.get('items'), list) and
                         len(page['items']) == (0 if page['shape'] == 'count' else
                                               min(64, page['totalRecordCount'])),
                         'workload_reader_page_contract')
                    ids = []
                    for item in page['items']:
                        item_id = identifier(item.get('Id'))
                        need(item_id not in ids and item_id in catalog['leaves'],
                             'workload_reader_page_identity')
                        row = catalog['leaves'][item_id]
                        need(row['library_id'] == library_id and item.get('Type') == row['type'] and
                             item.get('Name') == row['name'] and item.get('IsFolder') is False,
                             'workload_reader_terminal_projection')
                        self._check_http_user_data(item, role, item_id)
                        ids.append(item_id)
                    if phase == 'cached':
                        need(page['totalRecordCount'] == 500, 'workload_cached_reader_count')
                    observed.update(ids)
                need(observed == set(worker['observedItemIds']), 'workload_reader_observed_union')
                total += worker['requestCount']
            need(total <= 120, 'workload_reader_phase_budget')
            self._private('reader-catalog-reconciliation', phase=phase, requestCount=total,
                          observedIdsMatched=True)

        def _sessions(self, snapshot, *, revoked):
            sessions = indexed(rows(snapshot, 'sessions', 4))
            need(len(sessions) == len(self.credentials) and
                 {row.get('token_hash') for row in sessions.values()} == set(self.issued_hashes),
                 'workload_session_inventory')
            for credential in self.credentials:
                found = [row for row in sessions.values() if row['token_hash'] == credential['tokenHash']]
                need(len(found) == 1 and found[0].get('user_id') == self.users[credential['role']] and
                     found[0].get('kind') == credential['kind'] and
                     ((found[0].get('revoked_at') is not None) if revoked else
                      found[0].get('revoked_at') is None), 'workload_session_scope_or_revocation')
                if revoked:
                    timestamp(found[0]['revoked_at'])
            return sessions

        def _sql_history(self, snapshot, *, completed):
            need(completed in (0, 1, 2) and not rows(snapshot, 'task_triggers', 0) and
                 not rows(snapshot, 'task_occurrences', 0) and not rows(snapshot, 'play_sessions', 0),
                 'workload_unexpected_scheduling_or_playback')
            runs = indexed(rows(snapshot, 'task_runs', 2))
            children = indexed(rows(snapshot, 'task_run_children', 4))
            jobs = indexed(rows(snapshot, 'scan_jobs', 4))
            receipts = rows(snapshot, 'task_run_requests', 2)
            phases = ('cold', 'cached')[:completed]
            need(len(runs) == completed and len(children) == len(jobs) == completed * 2 and
                 len(receipts) == completed and
                 set(runs) == {self.admissions[phase]['runId'] for phase in phases},
                 'workload_sql_task_history_inventory')
            administrators = [row for row in rows(snapshot, 'sessions', 4)
                              if row.get('kind') == 'admin' and row.get('user_id') == self.users['admin'] and
                              row.get('token_hash') == self.admin['tokenHash']]
            need(len(administrators) == 1, 'workload_sql_actor_session_missing')
            for phase in phases:
                run_id = self.admissions[phase]['runId']
                run = runs[run_id]
                wire = self.runs[phase]['Run']
                need(run.get('task_id') == self.task_id and run.get('task_key') == 'library.scan' and
                     run.get('request_id') == request_ids[phase] and run.get('source') == 'manual' and
                     run.get('actor_user_id') == self.users['admin'] and run.get('actor_kind') == 'admin' and
                     run.get('actor_session_id') == administrators[0].get('id') and
                     run.get('state') == 'completed' and
                     all(type(run.get(field)) is int and run[field] >= 0 for field in
                         ('scanned', 'added', 'updated', 'total_children', 'terminal_children', 'completed_children')) and
                     all(run.get(field) is None for field in ('trigger_id', 'trigger_revision', 'scheduled_for')),
                     'workload_sql_run_scope')
                for sql, dto in (('scanned', 'Scanned'), ('added', 'Added'), ('updated', 'Updated'),
                                 ('total_children', 'TotalChildren'), ('terminal_children', 'TerminalChildren'),
                                 ('completed_children', 'CompletedChildren'), ('error_code', 'ErrorCode'),
                                 ('error_message', 'ErrorMessage')):
                    need(run.get(sql) == wire.get(dto), 'workload_sql_run_wire_mismatch')
                for sql, dto in (('created_at', 'CreatedAt'), ('started_at', 'StartedAt'), ('finished_at', 'FinishedAt')):
                    need(timestamp(run.get(sql)) == timestamp(wire.get(dto)), 'workload_sql_run_time_binding')
                matches = [row for row in receipts if row.get('request_id') == request_ids[phase]]
                need(len(matches) == 1 and matches[0].get('task_id') == self.task_id and
                     matches[0].get('run_id') == run_id, 'workload_sql_request_receipt')
                phase_children = [row for row in children.values() if row.get('run_id') == run_id]
                expected_children = {row['Id']: row for row in self.runs[phase]['Children']['Items']}
                need({row['id'] for row in phase_children} == set(expected_children),
                     'workload_sql_child_identity')
                for child in phase_children:
                    wire_child = expected_children[child['id']]
                    need(all(type(child.get(field)) is int and child[field] >= 0 for field in
                             ('ordinal', 'scanned', 'added', 'updated')), 'workload_sql_child_counter_types')
                    for sql, dto in (('library_id', 'LibraryId'), ('library_name', 'LibraryName'),
                                     ('ordinal', 'Ordinal'), ('state', 'State'), ('scan_job_id', 'ScanJobId'),
                                     ('scanned', 'Scanned'), ('added', 'Added'), ('updated', 'Updated'),
                                     ('error_code', 'ErrorCode'), ('error_message', 'ErrorMessage')):
                        need(child.get(sql) == wire_child.get(dto), 'workload_sql_child_wire_mismatch')
                    for sql, dto in (('created_at', 'CreatedAt'), ('started_at', 'StartedAt'), ('finished_at', 'FinishedAt')):
                        need(timestamp(child.get(sql)) == timestamp(wire_child.get(dto)),
                             'workload_sql_child_time_binding')
                    job = jobs.get(child.get('scan_job_id'))
                    need(isinstance(job, dict) and job.get('task_child_id') == child['id'] and
                         job.get('library_id') == child['library_id'] and job.get('status') == 'Completed' and
                         all(type(job.get(field)) is int for field in ('scanned', 'added', 'updated')) and
                         job.get('force_probe') is False and job.get('cancel_requested') is False and
                         job.get('error') == '' and job.get('scanned') == 500 and
                         job.get('added') == (500 if phase == 'cold' else 0) and job.get('updated') == 0,
                         'workload_sql_child_job_relation')
                    wire_job = self.jobs[phase][job['id']]
                    for sql, dto in (('created_at', 'CreatedAt'), ('started_at', 'StartedAt'), ('finished_at', 'FinishedAt')):
                        need(timestamp(job.get(sql)) == timestamp(wire_job.get(dto)),
                             'workload_sql_job_time_binding')
            need({row.get('scan_job_id') for row in children.values()} == set(jobs),
                 'workload_sql_orphan_scan_job')

        def execute(self):
            need(not self.executed, 'workload_execution_already_attempted')
            self.executed = True
            try:
                self._setup()
                self._phase('cold')
                cold = self._snapshot('cold-catalog')
                self.last_catalog = check_catalog(cold, self.capacity_libraries, corpus)
                check_user_data(cold, self.users, {})
                self._sql_history(cold, completed=1)
                self._reconcile_readers('cold', self.last_catalog)
                self._http_boundary(self.last_catalog)
                self._private('cold-catalog', catalog=self.last_catalog)
                self._seed_favorites()
                self._phase('cached')
                cached = self._snapshot('cached-catalog')
                after = check_catalog(cached, self.capacity_libraries, corpus)
                need(after['items'] == self.last_catalog['items'], 'workload_cached_catalog_changed')
                need(check_user_data(cached, self.users, self.favorites) == self.favorite_baseline,
                     'workload_cached_userdata_changed')
                self._sql_history(cached, completed=2)
                self._sessions(cached, revoked=False)
                self._reconcile_readers('cached', after)
                self._http_boundary(after)
                self.cached_snapshot = copy.deepcopy(cached)
                need(set(self.runs) == {'cold', 'cached'} and len(self.favorites) == 2 and
                     len(self.sql_slots) == 4, 'workload_final_inventory')
                result = {'status': 'workload-passed-pending-closure', 'users': copy.deepcopy(self.users),
                          'libraries': copy.deepcopy(self.capacity_libraries), 'taskId': self.task_id,
                          'runs': copy.deepcopy(self.runs), 'requestCounts': copy.deepcopy(self.budget),
                          'workerRequestCount': sum(row['requestCount'] for value in self.reader_joins.values()
                                                    for row in value['workers']),
                          'favorites': copy.deepcopy(self.favorites), 'credentialCount': len(self.credentials),
                          'priorSetupRequests': prior_setup_requests,
                          'journeyRequestCount': self.serial,
                          'sqlSlots': list(self.sql_slots), 'catalog': after,
                          'userData': copy.deepcopy(self.favorite_baseline),
                          'tracePersisted': self.trace_persisted}
                need(sum(self.budget.values()) + result['workerRequestCount'] <= 600 and
                     sum(self.budget.values()) == self.serial + prior_setup_requests,
                     'workload_combined_request_budget')
                self._private('workload-complete', result=result)
                return result
            except Exception as error:
                self.failed = True
                self.first_failure = (str(error) if isinstance(error, (WorkloadError, journey_module.JourneyError))
                                      else 'workload_operation_failed')
                try:
                    self._private('workload-failed', error=self.first_failure,
                                  admissions=self.admissions, requestCounts=self.budget)
                except Exception:
                    pass
                raise WorkloadError(self.first_failure) from None

        def _logout(self, credential):
            need(not credential['attempted'], 'workload_logout_already_attempted')
            credential['attempted'] = True
            suffix = credential['kind'] + '-' + credential['role']
            if credential['kind'] == 'admin':
                method, path, check = 'DELETE', '/admin/v1/session', '/admin/v1/session'
            else:
                method, path = 'POST', '/emby/Sessions/Logout'
                check = '/emby/Users/' + self.users[credential['role']] + '/Items?Limit=0'
            self._request(method, path, 204, credential=credential, label='logout-' + suffix,
                          json_body=False, cleanup_request=True)
            credential['logout204'] = True
            reply = self._request('GET', check, 401, credential=credential, label='revoked-check-' + suffix,
                                  json_body=credential['kind'] == 'admin', cleanup_request=True)
            if credential['kind'] == 'admin':
                need(isinstance(reply, dict) and reply.get('Error', {}).get('Code') == 'invalid_credentials',
                     'workload_admin_revoked_error')
            else:
                need(reply == b'Access token is invalid or expired.', 'workload_emby_revoked_error')
            credential['rejected401'] = True
            credential['value'] = ''
            credential.pop('csrf', None)

        def _drain(self, deadline):
            if self.task_id is None or not self.admissions:
                return {'status': 'not-admitted', 'cancelAttempted': []}
            page = self._admin_json('GET', '/admin/v1/tasks/' + self.task_id + '/runs?StartIndex=0&Limit=4',
                                    label='cleanup-reconcile-runs', deadline=deadline)
            values = page.get('Items')
            need(isinstance(values, list) and len(values) <= 2 and
                 page.get('TotalRecordCount') == len(values) and page.get('StartIndex') == 0 and
                 page.get('Limit') == 4, 'workload_cleanup_run_inventory')
            by_id = {}
            for run in values:
                run_id = identifier(run.get('Id'))
                matches = [phase for phase, admission in self.admissions.items()
                           if admission['requestId'] == run.get('RequestId')]
                need(len(matches) == 1 and run.get('TaskId') == self.task_id and run.get('Source') == 'manual' and
                     run.get('TotalChildren') == 2 and run.get('State') in (ACTIVE | TERMINAL) and
                     run_id not in by_id, 'workload_cleanup_unowned_run')
                phase = matches[0]
                known = self.admissions[phase]['runId']
                need(known in (None, run_id), 'workload_cleanup_run_identity_changed')
                self.admissions[phase]['runId'] = run_id
                by_id[run_id] = run
            unresolved = [phase for phase, admission in self.admissions.items() if admission['runId'] is None]
            # A bounded empty history does not prove a lost POST was never admitted.
            need(not unresolved, 'workload_unknown_admission_unresolved')
            need({admission['runId'] for admission in self.admissions.values()} == set(by_id),
                 'workload_cleanup_admitted_run_missing')
            for run_id, run in by_id.items():
                if run['State'] in ACTIVE and run_id not in self.cancel_attempted:
                    self.cancel_attempted.add(run_id)
                    reply = self._admin_json('POST', '/admin/v1/task-runs/' + run_id + '/cancel', 202,
                                             body={}, label='cancel-' + run_id, deadline=deadline).get('Run')
                    need(isinstance(reply, dict) and reply.get('Id') == run_id and
                         reply.get('TaskId') == self.task_id and reply.get('State') in (TERMINAL | {'stopping'}),
                         'workload_cleanup_cancel_not_acknowledged')
            while True:
                active = []
                for phase, admission in self.admissions.items():
                    need(time.monotonic() < deadline, 'workload_cleanup_drain_timeout')
                    detail = self._run_detail(phase, deadline=deadline)
                    if detail['Run']['State'] not in TERMINAL:
                        active.append(admission['runId'])
                if not active:
                    definition = self._task_definition(label='cleanup-task-drained', deadline=deadline)
                    jobs = self._admin_json('GET', '/admin/v1/jobs', label='cleanup-jobs-drained', deadline=deadline)
                    values = jobs.get('Items')
                    need(definition.get('CurrentRun') is None and isinstance(values, list) and len(values) <= 4 and
                         jobs.get('TotalRecordCount') == len(values) and
                         all(row.get('LibraryId') in {value['Id'] for value in self.capacity_libraries.values()} and
                             row.get('Status') in TERMINAL for row in values), 'workload_cleanup_scan_not_drained')
                    return {'status': 'drained', 'cancelAttempted': sorted(self.cancel_attempted)}
                delay = min(5.0, deadline - time.monotonic())
                need(delay > 0, 'workload_cleanup_drain_timeout')
                time.sleep(delay)

        def cleanup(self, deadline=None):
            if self.cleanup_result is not None:
                return copy.deepcopy(self.cleanup_result)
            need(type(deadline) in (int, float) and math.isfinite(deadline) and
                 deadline > time.monotonic(), 'workload_parent_cleanup_deadline_required')
            self.cleanup_deadline = deadline
            self.bucket, self.phase = 'cleanup', 'cleanup'
            failures, drain = [], None
            if self.active_reader_phase is not None:
                try:
                    self._join_readers(self.active_reader_phase, min(deadline, time.monotonic() + 15.0),
                                       normal=False)
                except Exception:
                    failures.append('readers-not-joined')
            if self.active_reader_phase is None and self.admin is not None and not self.admin['attempted']:
                try:
                    drain = self._drain(min(deadline, time.monotonic() + 60.0))
                except Exception:
                    failures.append('owned-task-drain-incomplete')
            elif self.admissions:
                failures.append('owned-task-drain-unavailable')
            if self.active_reader_phase is None:
                for credential in reversed(self.credentials):
                    if not credential['attempted']:
                        try:
                            self._logout(credential)
                        except Exception:
                            failures.append('credential-' + credential['kind'] + '-' + credential['role'])
            else:
                failures.append('credential-closure-waiting-for-reader-join')
            closed = all(row['logout204'] and row['rejected401'] for row in self.credentials)
            if not self.trace_persisted:
                failures.append('http-trace-not-fully-persisted')
            result = {'status': 'closed' if not failures and closed else 'incomplete',
                      'failures': failures, 'taskDrain': drain, 'credentials': [
                          {key: row[key] for key in ('kind', 'role', 'phase', 'tokenHash',
                                                    'attempted', 'logout204', 'rejected401')}
                          for row in self.credentials], 'requestCounts': copy.deepcopy(self.budget),
                      'tracePersisted': self.trace_persisted, 'firstFailure': self.first_failure}
            result['priorSetupRequests'] = prior_setup_requests
            result['journeyRequestCount'] = self.serial
            self.cleanup_result = result
            try:
                self._private('workload-cleanup', result=result)
            except Exception:
                self.cleanup_result['status'] = 'incomplete'
                self.cleanup_result['failures'].append('cleanup-record-not-persisted')
            return copy.deepcopy(self.cleanup_result)

        def validate_final_snapshot(self, snapshot):
            need(self.cached_snapshot is not None and self.cleanup_result is not None and
                 self.cleanup_result['status'] == 'closed' and not self.failed and self.trace_persisted,
                 'workload_final_snapshot_requires_normal_closure')
            need(set(snapshot) == set(TABLES), 'workload_final_snapshot_contract')
            catalog = check_catalog(snapshot, self.capacity_libraries, corpus)
            need(catalog['items'] == self.last_catalog['items'] and
                 check_user_data(snapshot, self.users, self.favorites) == self.favorite_baseline,
                 'workload_post_stop_catalog_or_userdata_changed')
            self._sql_history(snapshot, completed=2)
            sessions = self._sessions(snapshot, revoked=True)
            for table in ('task_runs', 'task_run_children', 'scan_jobs', 'task_run_requests',
                          'task_triggers', 'task_occurrences', 'library_roots', 'libraries', 'play_sessions'):
                need(sorted(snapshot[table], key=lambda row: repr(sorted(row.items()))) ==
                     sorted(self.cached_snapshot[table], key=lambda row: repr(sorted(row.items()))),
                     'workload_post_stop_history_changed')
            result = {'status': 'normal-workload-and-credential-closure-checked',
                      'catalogItems': len(catalog['items']), 'mediaLeaves': len(catalog['leaves']),
                      'favoriteRows': len(self.favorite_baseline), 'sessionsRevoked': len(sessions),
                      'runCount': 2, 'childCount': 4, 'scanJobCount': 4,
                      'fullCapacityOrDeliveryAccepted': False}
            self._private('final-workload-sql-check', result=result)
            return result

    return CapacityWorkload()
