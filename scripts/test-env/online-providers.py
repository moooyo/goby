#!/usr/bin/env python3
"""Exercise credential-free MusicBrainz through an owned Docker Goby instance.

The private JSON configuration supplies base_url, control_script, output_dir,
setup_token, admin_name, admin_password, and music_root. Optional timeout_seconds
defaults to 300. The fixture contains one synthetic FLAC in a Kind of Blue album
directory, tagged with MusicBrainzReleaseGroup 8e8a594f-2175-38c7-a871-abb68ec363e7.
The controller follows oci-delivery.py's start/stop/status/recreate contract.
The baseline phase records the known typed-ID rejection without online acceptance.
Acceptance resumes the same owned catalog, uses real MusicBrainz requests, and
checks administrator precedence, provenance, rescanning and container recreation.
TMDB/OpenSubtitles remain unconfigured and are never labeled online successes.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import re
import stat
import sys
import urllib.parse

spec = importlib.util.spec_from_file_location('oci_delivery', Path(__file__).with_name('oci-delivery.py'))
oci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(oci)
need, emit = oci.need, oci.emit
ALBUM_ID = '8e8a594f-2175-38c7-a871-abb68ec363e7'

def load_config(filename):
    path = Path(filename)
    info = path.lstat()
    need(path.is_absolute() and stat.S_ISREG(info.st_mode) and info.st_mode & 0o077 == 0
         and 0 < info.st_size <= 16384, 'private_config_required')
    value = json.loads(path.read_bytes())
    required = {'base_url', 'control_script', 'output_dir', 'setup_token', 'admin_name', 'admin_password', 'music_root'}
    need(type(value) is dict and required <= value.keys() <= required | {'timeout_seconds'}, 'invalid_config_fields')
    need(all(type(value[key]) is str and value[key] for key in required), 'invalid_config_values')
    origin = urllib.parse.urlsplit(value['base_url'])
    need(origin.scheme == 'http' and origin.hostname == '127.0.0.1' and origin.port is not None
         and not origin.username and not origin.password and not origin.query and not origin.fragment
         and origin.path in ('', '/'), 'explicit_loopback_required')
    for key in ('control_script', 'output_dir', 'music_root'):
        need(Path(value[key]).is_absolute(), key + '_must_be_absolute')
    value.setdefault('timeout_seconds', 300)
    need(type(value['timeout_seconds']) is int and 30 <= value['timeout_seconds'] <= 600, 'invalid_timeout')
    return value

class ProviderJourney(oci.Delivery):
    def provider_call(self, operation, payload, expected=200):
        return self.request('POST', self.provider_path + operation, payload, expected=expected, admin=True)

    def detail(self):
        return self.request('GET', '/admin/v1/items/' + self.album + '/metadata', admin=True)

    def enabled(self, value):
        current = self.request('GET', '/admin/v1/settings', admin=True)
        management = current['Management']
        management['Metadata']['EnableInternetProviders'] = value
        self.request('PUT', '/admin/v1/settings', {'Revision': current['Revision'], 'Overrides': current['Overrides'], 'Management': management}, admin=True)
        status = self.request('GET', '/admin/v1/providers', admin=True)
        states = {entry['Id']: entry['Configured'] for entry in status['Items']}
        need(status['Enabled'] is value and states == {'tmdb': False, 'musicbrainz': value, 'opensubtitles': False}, 'unexpected_provider_configuration')
        emit('provider_configuration_verified', enabled=value, musicbrainz=states['musicbrainz'], tmdb=False, opensubtitles=False)

    def scan(self):
        result = self.request('POST', '/admin/v1/libraries/' + self.library_id + '/scan', {}, expected=202, admin=True)
        self.scan_complete(result['Job']['Id'] if 'Job' in result else result['Id'])

    def initialize_fixture(self):
        if not self.request('GET', '/admin/v1/bootstrap')['Initialized']:
            self.request('POST', '/admin/v1/bootstrap', {'SetupToken': self.config['setup_token'], 'Name': self.config['admin_name'], 'Password': self.config['admin_password']}, expected=201)
        self.authenticate()
        page = self.request('GET', '/admin/v1/libraries', admin=True)
        need(page['TotalRecordCount'] <= 1, 'unexpected_library_population')
        if not page['Items']:
            created = self.request('POST', '/admin/v1/libraries', {'Name': 'Online provider music fixture', 'CollectionType': 'music', 'Paths': [self.config['music_root']], 'Scan': True}, expected=201, admin=True)
            self.library_id = created['Library']['Id']
            self.scan_complete(created['Job']['Id'])
        else:
            need(page['Items'][0]['Name'] == 'Online provider music fixture', 'unexpected_retained_library')
            self.library_id = page['Items'][0]['Id']
        items = self.request('GET', '/admin/v1/libraries/' + self.library_id + '/items?Limit=100', admin=True)['Items']
        albums = [item for item in items if item['Type'] == 'MusicAlbum']
        audio = [item for item in items if item['Type'] == 'Audio']
        need(len(albums) == len(audio) == 1, 'unexpected_music_fixture')
        self.album, self.audio = albums[0]['Id'], audio[0]['Id']
        self.provider_path = '/admin/v1/items/' + self.album + '/providers/'
        need(self.detail()['Effective']['ProviderIds'].get('MusicBrainzReleaseGroup') == ALBUM_ID, 'typed_fixture_id_missing')
        self.enabled(True)

    def provenance(self):
        page = self.request('GET', self.provider_path + 'provenance', admin=True)
        rows = [row for row in page['Items'] if row['Provider'] == 'musicbrainz']
        need(len(rows) == 1 and rows[0]['ProviderId'] == ALBUM_ID
             and rows[0]['SourceUrl'] == 'https://musicbrainz.org/release-group/' + ALBUM_ID
             and rows[0]['FetchedAt'] and 'Name' in rows[0]['Fields'], 'online_provenance_missing')
        return rows[0]

    def assert_controls(self):
        detail = self.detail()
        need(detail['Effective']['Name'] == 'Kept local album label'
             and detail['Effective']['Overview'] == 'Pinned local provider-verification overview.'
             and 'Overview' in detail['LockedFields']
             and detail['Effective']['ProviderIds']['MusicBrainzReleaseGroup'] == ALBUM_ID,
             'administrator_controls_or_typed_id_changed')
        self.provenance()
        query = urllib.parse.urlencode({'ParentId': self.library_id, 'Recursive': 'true', 'IncludeItemTypes': 'MusicAlbum,Audio', 'Fields': 'ProviderIds'})
        page = self.request('GET', '/emby/Items?' + query, public=True)
        items = {item['Type']: item for item in page['Items']}
        need(page['TotalRecordCount'] == len(page['Items']) == 2 and set(items) == {'MusicAlbum', 'Audio'}
             and items['MusicAlbum']['Id'] == self.album and items['Audio']['Id'] == self.audio
             and items['MusicAlbum']['Name'] == 'Kept local album label', 'client_catalog_projection_changed')
        return detail

    def execute_provider(self, phase):
        self.phase = 'fixture'
        self.control('start')
        self.ready()
        self.initialize_fixture()
        detail = self.detail()
        provenance = self.request('GET', self.provider_path + 'provenance', admin=True)
        need(not provenance['Items'], 'first_refresh_requires_no_provider_history')
        need('MusicBrainz' not in detail['Effective']['ProviderIds'], 'generic_id_would_mask_regression')
        payload = {'Provider': 'musicbrainz', 'Language': 'en', 'Revision': detail['Revision']}
        self.phase = 'typed_id_first_refresh'
        if phase == 'baseline':
            rejected = self.provider_call('refresh', payload, expected=409)
            need(rejected['Error']['Code'] == 'provider_match_required', 'baseline_did_not_reproduce_typed_id_defect')
            emit('baseline_defect_reproduced', error='provider_match_required', typed_id=True, provider_history=False, online_acceptance=False)
            return
        refreshed = self.provider_call('refresh', payload)
        need(refreshed['Effective']['Name'] == 'Kind of Blue'
             and refreshed['Effective']['ProviderIds']['MusicBrainzReleaseGroup'] == ALBUM_ID,
             'real_lookup_did_not_apply_expected_album')
        self.save('first-online-refresh.json', json.dumps({'detail': refreshed, 'provenance': self.provenance()}, indent=2).encode())
        emit('real_typed_id_refresh_verified', provider='musicbrainz', id=ALBUM_ID, prior_history=False)
        self.phase = 'search_and_administrator_precedence'
        matches = self.provider_call('search', {'Provider': 'musicbrainz', 'Name': 'Kind of Blue', 'Year': 1959, 'Language': 'en'})['Items']
        need(any(match['Id'] == ALBUM_ID and match['Name'] == 'Kind of Blue' for match in matches), 'real_album_search_missing_expected_match')
        overrides = dict(refreshed['Overrides'])
        overrides.update(Name='Kept local album label', Overview='Pinned local provider-verification overview.')
        edited = self.request('PUT', '/admin/v1/items/' + self.album + '/metadata', {'Revision': refreshed['Revision'], 'Overrides': overrides, 'LockedFields': ['Overview']}, admin=True)
        overrides = dict(edited['Overrides'])
        overrides.pop('Overview')
        edited = self.request('PUT', '/admin/v1/items/' + self.album + '/metadata', {'Revision': edited['Revision'], 'Overrides': overrides, 'LockedFields': ['Overview']}, admin=True)
        selection = {'Provider': 'musicbrainz', 'Id': ALBUM_ID, 'Language': 'en', 'Revision': edited['Revision']}
        self.provider_call('apply', selection)
        self.assert_controls()
        stale = self.provider_call('apply', selection, expected=409)
        need(stale['Error']['Code'] == 'revision_conflict', 'stale_selection_failed_for_wrong_reason')
        self.enabled(False)
        disabled = self.provider_call('search', {'Provider': 'musicbrainz', 'Name': 'Kind of Blue', 'Language': 'en'}, expected=409)
        need(disabled['Error']['Code'] == 'providers_disabled', 'disabled_provider_was_not_rejected')
        self.enabled(True)
        detail = self.detail()
        self.provider_call('refresh', {'Provider': 'musicbrainz', 'Language': 'en', 'Revision': detail['Revision']})
        self.assert_controls()
        emit('real_search_apply_refresh_verified', overrides_preserved=True, locks_preserved=True, stale_revision_rejected=True, managed_disable_effective=True)
        self.phase = 'scan_and_restart'
        self.scan()
        before = self.assert_controls()
        old = self.control('status')
        self.control('stop')
        self.control('recreate')
        current = self.ready()
        need(current['pid'] != old['pid'] and current['image_id'] == old['image_id'], 'same_image_recreation_not_observed')
        self.authenticate()
        after = self.assert_controls()
        need(before['Effective'] == after['Effective'] and before['Overrides'] == after['Overrides']
             and before['LockedValues'] == after['LockedValues'], 'metadata_state_changed_across_restart')
        self.save('final-musicbrainz-state.json', json.dumps({'metadata': after, 'provenance': self.provenance(), 'imageId': current['image_id']}, indent=2).encode())
        emit('musicbrainz_scan_restart_verified', durable_state=True, item_ids_preserved=True, source_url_verified=True)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--config', required=True)
    parser.add_argument('--phase', choices=['baseline', 'acceptance'], required=True)
    args = parser.parse_args()
    driver, failure = None, None
    try:
        driver = ProviderJourney(load_config(args.config))
        driver.execute_provider(args.phase)
    except Exception as error:
        failure = str(error) if isinstance(error, oci.Failure) else 'unexpected_' + type(error).__name__
        emit('journey_failure', phase=None if driver is None else driver.phase, reason=failure)
    finally:
        if driver is not None:
            try:
                driver.close()
            except Exception as error:
                failure = failure or 'closure_failed'
                emit('closure_failed', reason=str(error) if isinstance(error, oci.Failure) else type(error).__name__)
    emit('result', passed=failure is None, phase=args.phase, failure=failure,
         real_online_provider='musicbrainz' if args.phase == 'acceptance' and failure is None else None,
         pending_credentials=['tmdb', 'opensubtitles'])
    return 1 if failure else 0

if __name__ == '__main__':
    sys.exit(main())
