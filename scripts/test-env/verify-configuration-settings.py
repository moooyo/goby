#!/usr/bin/env python3
"""Verify configuration-compatible settings in an owned Linux database.

The frozen managed-user runner retains resource ownership and final cleanup.
The frozen settings runner supplies native sessions, browser execution, private
snapshots, and secret handling. This adapter changes only the scenario contract.
"""

import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import sys


MARKER = "goby-configuration-settings-browser-v1"
RUN_ENV = "GOBY_CONFIGURATION_SETTINGS_RUN_ID"
FIXTURE_MARKER = "goby-configuration-settings-fixtures-v1"
RESULT_MARKER = "goby-configuration-settings-result-v1"
SETTINGS_FIELDS = ("ServerName", "MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels")
RESET_FIELDS = SETTINGS_FIELDS + ("TranscodingMaxWidth",)
FINAL_OVERRIDES = {"ServerName": None, "MaxBitrate": 1234567, "MaxWidth": None,
                   "MaxHeight": 720, "MaxAudioChannels": None}
FINAL_NAME_MODE = "unset"
FINAL_ENCODING = {"TranscodingMaxWidth": 640}
REQUIRED_BROWSER_CHECKS = frozenset((
    "InitialSourcesDefaultsAndReadonlyDeployment", "UnsetNamePresentedAsHostName",
    "UnsetNameSurvivesOtherFieldEdits", "PositiveAndZeroAdditionalWidthPreserveNativeWidth",
    "FiveOverridesAndExactMbpsInteger", "ServerNamePublishesToPublicInfoAndOverview",
    "SubsetResetPreservesUnselectedDraft", "ResetCancellationAndFullReset",
    "AdditionalWidthSubsetResetPreservesNativeDraftAndSavedLimit", "AllSixSettingsResetTogether",
    "ThreeNameChoicesAndFourStoredModes", "RevisionConflictRequiresExplicitReload",
    "CommittedResponseLossRequiresReloadWithoutRewrite", "MobileLayoutAndDirtyNavigationGuard",
    "MixedOverridesPreparedForRestartChecks", "UnsetAndPositiveAdditionalWidthPreparedForRestartChecks",
))
SOURCE_PATHS = ("e2e/settings.spec.ts", "src/api.ts", "src/settingsDraft.ts", "src/SettingsPage.tsx",
                "src/App.tsx", "package.json", "package-lock.json", "playwright.config.ts")


def load_settings_runner(path):
    # Importing the adapter is inert: neither frozen CLI is invoked and no
    # bytecode is written into the operator's prepared source snapshot.
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_configuration_settings_shared", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared settings verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def create_runner(core, settings, args):
    # The frozen factory exposes its nested class through an inert instance.
    # Construction creates only Python state; allocation, HTTP, subprocesses,
    # database access, and the old main-service PID check are never invoked.
    shared_runner = type(settings.create_runner(core, args))
    core.MARKER = MARKER
    core.RUN_ENV = RUN_ENV
    # This module was privately imported for one scenario. Its browser result
    # reader and safe result projection now consume this scenario's marker.
    settings.RESULT_MARKER = RESULT_MARKER

    class ConfigurationSettingsRunner(shared_runner):
        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m5h_configuration_" + self.run_id
            self.role = "goby_m5h_role_" + self.run_id
            self.tag = MARKER + ":" + self.run_id
            self.pg_app_name = "goby_m5h_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-configuration-settings-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-configuration-settings-" + self.run_id)
            self.admin_name = "m5h-admin-" + self.run_id
            self.host_name = None
            self.initial_settings = None
            self.report.update({"scenario": "configuration_settings", "database": self.database, "role": self.role})

        def allocate(self):
            # Deliberately bypass only the frozen SettingsRunner.allocate:
            # its M5g service identity is unrelated to the approved M5h run.
            core.require(core.MARKER == MARKER and core.RUN_ENV == RUN_ENV
                         and core.BASE_ENV.get("PGAPPNAME") == self.pg_app_name,
                         "The private scenario ownership markers changed before allocation.")
            core.Runner.allocate(self)
            core.require(self.goby.pw_uid == 995 and os.fstat(self.binary_fd).st_mode & 0o001,
                         "The isolated executable must be runnable by the reviewed UID 995.")
            core.require(self.report["shared_service_before"] == {"MainPID": "3614026", "ActiveState": "active"},
                         "The reviewed shared application service identity changed; no shared service was modified.")
            core.require(self.args.assets.is_file() and core.file_digest(self.args.assets) == self.args.assets_sha256,
                         "The prepared administrator asset archive does not match its supplied SHA-256.")
            core.require(core.free_bytes(core.EXEC_ROOT) >= 128 * 1024 * 1024 and
                         core.free_bytes(Path("/dev/shm")) >= 96 * 1024 * 1024,
                         "Insufficient headroom for the configuration settings verification.")
            self.host_name = os.uname().nodename
            core.require(self.valid_name(self.host_name), "The isolated host-name fallback is invalid.")
            core.require(all((self.args.snapshot / "web/admin" / relative).is_file() for relative in SOURCE_PATHS),
                         "The prepared source snapshot lacks a required configuration settings input.")
            self.report["provenance"] = {
                "wrapper_sha256": core.file_digest(Path(__file__).resolve()),
                "shared_runner_sha256": core.file_digest(self.args.core_runner.resolve()),
                "settings_runner_sha256": core.file_digest(self.args.settings_runner.resolve()),
                "binary_sha256": self.args.binary_sha256,
                "assets_archive_sha256": self.args.assets_sha256,
                "source_inputs_sha256": {
                    relative: core.file_digest(self.args.snapshot / "web/admin" / relative)
                    for relative in SOURCE_PATHS
                },
            }
            self.report["checks"]["provided_binary_and_assets_hashes_verified"] = True

        @staticmethod
        def valid_name(value):
            return (isinstance(value, str) and bool(value.strip()) and "\0" not in value
                    and not any(0xD800 <= ord(character) <= 0xDFFF for character in value)
                    and len(value.encode("utf-8")) <= 128)

        def assert_settings(self, value, defaults, overrides, *, revision=None, updated_at=None,
                            name_mode=FINAL_NAME_MODE, encoding=FINAL_ENCODING):
            core.require(isinstance(value, dict) and set(value) == {
                "Revision", "Defaults", "Overrides", "Effective", "Sources", "UpdatedAt", "Deployment",
                "ServerNameMode", "Encoding"}, "The configuration settings response lost its exact safe projection.")
            current_revision = value["Revision"]
            core.require(isinstance(current_revision, str) and re.fullmatch(r"[1-9][0-9]{0,18}", current_revision)
                         and int(current_revision) <= 9223372036854775807
                         and isinstance(value["UpdatedAt"], str) and value["UpdatedAt"],
                         "The configuration settings revision or update timestamp is invalid.")
            for group in ("Defaults", "Overrides", "Effective", "Sources"):
                core.require(isinstance(value[group], dict) and set(value[group]) == set(SETTINGS_FIELDS),
                             "The configuration settings field inventory is incomplete.")
            core.require(name_mode in ("deployment", "custom", "empty", "unset")
                         and value["ServerNameMode"] == name_mode
                         and self.valid_name(value["Defaults"]["ServerName"])
                         and self.valid_name(value["Effective"]["ServerName"]),
                         "The configuration settings name mode or public name is invalid.")
            configured = value["Overrides"]["ServerName"]
            core.require((name_mode in ("deployment", "unset") and configured is None)
                         or (name_mode == "empty" and configured == "")
                         or (name_mode == "custom" and self.valid_name(configured)),
                         "The configured name is inconsistent with its persisted mode.")
            for field in SETTINGS_FIELDS[1:]:
                maximum = 1000000000 if field == "MaxBitrate" else 8 if field == "MaxAudioChannels" else 8192
                for group in ("Defaults", "Overrides", "Effective"):
                    entry = value[group][field]
                    if group == "Overrides" and entry is None:
                        continue
                    core.require(type(entry) is int and 1 <= entry <= maximum,
                                 "A native output planning limit is invalid.")
            effective = {field: defaults[field] if overrides[field] is None else overrides[field]
                         for field in SETTINGS_FIELDS}
            sources = {field: "deployment" if overrides[field] is None else "database"
                       for field in SETTINGS_FIELDS}
            if name_mode in ("empty", "unset"):
                effective["ServerName"] = self.host_name
            sources["ServerName"] = "deployment" if name_mode == "deployment" else "database"
            deployment = dict(settings.DEPLOYMENT, HostName=self.host_name)
            core.require(value["Defaults"] == defaults and value["Overrides"] == overrides
                         and value["Effective"] == effective and value["Sources"] == sources
                         and isinstance(value["Deployment"], dict) and value["Deployment"] == deployment
                         and type(value["Deployment"].get("TranscodingEnabled")) is bool
                         and all(type(value["Deployment"].get(field)) is int
                                 for field in ("Threads", "MaxJobs", "MaxUserJobs", "MaxSessionJobs")),
                         "Configuration settings did not retain the expected defaults, overrides, fallback, or sources.")
            core.require(isinstance(value["Encoding"], dict) and set(value["Encoding"]) == {"TranscodingMaxWidth"}
                         and type(value["Encoding"]["TranscodingMaxWidth"]) is int
                         and 0 <= value["Encoding"]["TranscodingMaxWidth"] <= 8192
                         and value["Encoding"] == encoding,
                         "The separate encoding width did not retain its exact configured value.")
            core.require((revision is None or current_revision == revision)
                         and (updated_at is None or value["UpdatedAt"] == updated_at),
                         "A restart changed the stored settings revision or update timestamp.")
            serialized = settings.canonical(value)
            core.require(not any(secret and secret in serialized for secret in self.secrets),
                         "The safe settings projection unexpectedly contained an authentication secret.")
            return value

        def bootstrap(self):
            # Bootstrap creates no login. Exactly one native control login is
            # issued here, and the browser creates the second native session.
            core.Runner.bootstrap(self)
            self.session_cleanup_required = True
            self.control_login()
            core.require(len(self.list_sessions()) == 1,
                         "The fixture must start with exactly one native control login.")
            empty_overrides = dict.fromkeys(SETTINGS_FIELDS)
            initial = self.http_json("/admin/v1/settings", administrator=True)
            self.assert_settings(initial, self.initial_defaults, empty_overrides,
                                 name_mode="deployment", encoding={"TranscodingMaxWidth": 0})
            seed = self.http_json("/admin/v1/settings", method="PUT", administrator=True,
                                  body={"Revision": initial["Revision"], "Overrides": empty_overrides,
                                        "ServerNameMode": "unset", "Encoding": {"TranscodingMaxWidth": 0}})
            self.assert_settings(seed, self.initial_defaults, empty_overrides,
                                 revision=str(int(initial["Revision"]) + 1),
                                 name_mode="unset", encoding={"TranscodingMaxWidth": 0})
            core.require(self.http_json("/admin/v1/settings", administrator=True) == seed,
                         "The initial unset name mode was not committed before browser handoff.")
            self.verify_public_name(self.host_name)
            self.initial_settings = seed
            self.fixture = {
                "Marker": FIXTURE_MARKER, "RunId": self.run_id, "Origin": self.origin,
                "Administrator": self.administrator, "InitialDefaults": self.initial_defaults,
                "ReplacementDefaults": self.replacement_defaults, "InitialSettings": seed,
                "FinalOverrides": FINAL_OVERRIDES, "FinalServerNameMode": FINAL_NAME_MODE,
                "FinalEncoding": FINAL_ENCODING, "ResultPath": str(self.result_path),
            }
            core.private_write(self.manifest, (settings.canonical(self.fixture) + "\n").encode())
            self.report["fixtures"] = {
                "dedicated_administrators": 1, "initial_native_sessions": 1,
                "initial_defaults": self.initial_defaults, "replacement_defaults": self.replacement_defaults,
                "initial_settings": seed, "final_overrides": FINAL_OVERRIDES,
                "final_server_name_mode": FINAL_NAME_MODE, "final_encoding": FINAL_ENCODING,
                "private_manifest": True, "media_files_created": 0, "playback_requests": 0,
            }
            self.report["checks"]["initial_unset_mode_seeded_by_native_cas_put"] = True

        def browser_environment(self):
            # Keep generic URL and administrator inputs from the inert core,
            # exposing only the reviewed scenario's affirmative guard flags.
            environment = core.Runner.browser_environment(self)
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({"GOBY_SMOKE_CONFIGURATION_SETTINGS_DISPOSABLE_DATABASE": "1",
                                "GOBY_SMOKE_CONFIGURATION_SETTINGS_DEDICATED_ADMIN": "1",
                                "GOBY_SMOKE_CONFIGURATION_SETTINGS_FIXTURE_MANIFEST": str(self.manifest)})
            return environment

        def load_browser_result(self):
            # The shared reader collects private credentials before validating
            # identity, including partial results that still require cleanup.
            super().load_browser_result()
            if self.browser_result.get("Complete") is True:
                checks = self.browser_result.get("Checks")
                core.require(isinstance(checks, dict) and set(checks) == REQUIRED_BROWSER_CHECKS
                             and all(value is True for value in checks.values()),
                             "The browser did not complete every required configuration settings acceptance check.")

        def owned_database_read(self, statement):
            core.require(self.database_oid is not None and self.lock is not None
                         and re.fullmatch(r"goby_m5h_configuration_[0-9]{8}_[0-9]{6}_[0-9a-f]{10}", self.database)
                         and self.database == "goby_m5h_configuration_" + self.run_id
                         and self.role == "goby_m5h_role_" + self.run_id,
                         "Configuration persistence inspection requires this run's canonical owned database.")
            identity = core.pg_json(f"""SELECT jsonb_build_object('oid', d.oid::bigint,
                'owner', pg_get_userbyid(d.datdba), 'tag', shobj_description(d.oid, 'pg_database'),
                'role_tag', shobj_description(r.oid, 'pg_authid'))
                FROM pg_database d JOIN pg_roles r ON r.oid = d.datdba WHERE d.datname = '{self.database}';""")
            core.require(identity == {"oid": self.database_oid, "owner": self.role,
                                      "tag": self.tag, "role_tag": self.tag},
                         "The owned configuration settings database or role identity changed.")
            guarded = f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
                SET LOCAL statement_timeout = '10s';
                DO $guard$ BEGIN
                    IF current_database() <> '{self.database}' OR current_user <> '{self.role}' THEN
                        RAISE EXCEPTION 'Unexpected configuration fixture database or role';
                    END IF;
                END $guard$;
                {statement}
                COMMIT;
            """
            return core.command([core.PG_BIN / "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-At",
                                 "-h", "127.0.0.1", "-p", str(core.PG_PORT), "-U", self.role, "-d", self.database],
                                text=guarded, environment=dict(core.BASE_ENV, PGPASSWORD=self.role_password))

        def full_private_snapshot(self):
            rows = super().full_private_snapshot()
            history = rows["schema_migrations"]
            core.require(len(history) == 21 and all(type(row.get("version")) is int for row in history)
                         and sorted(row["version"] for row in history) == list(range(1, 22))
                         and next(row for row in history if row["version"] == 21).get("name")
                         == "0021_configuration_compatibility.sql",
                         "The private snapshot did not retain the complete schema-21 migration history.")
            self.report["checks"]["schema_version"] = 21
            return rows

        def assert_stored_settings(self, rows, *, revision, name_mode, overrides, encoding):
            stored = rows["managed_settings"]
            columns = {"ServerName": "server_name", "MaxBitrate": "max_bitrate", "MaxWidth": "max_width",
                       "MaxHeight": "max_height", "MaxAudioChannels": "max_audio_channels"}
            expected_columns = {"id", "revision", "created_at", "updated_at", "server_name_mode",
                                "compatibility_max_width", *columns.values()}
            core.require(len(stored) == 1 and set(stored[0]) == expected_columns and stored[0].get("id") == 1
                         and stored[0].get("revision") == int(revision)
                         and stored[0].get("server_name_mode") == name_mode
                         and stored[0].get("compatibility_max_width") == encoding["TranscodingMaxWidth"]
                         and all(stored[0].get(column) == overrides[field] for field, column in columns.items()),
                         "The private settings singleton did not retain the exact configured mode, nulls, and values.")

        def verify_restart(self):
            final = self.browser_result["FinalSettings"]
            self.stop_app()
            before = self.full_private_snapshot()
            self.assert_stored_settings(before, revision=final["Revision"], name_mode=FINAL_NAME_MODE,
                                        overrides=FINAL_OVERRIDES, encoding=FINAL_ENCODING)
            core.require(all(not before[table] for table in (
                "libraries", "items", "scan_jobs", "play_sessions", "encoding_jobs", "application_keys")),
                "Configuration verification unexpectedly created media, playback, conversion, or application keys.")
            self.start_app()
            # The inherited readiness probe is unauthenticated. No authenticated
            # request may advance session metadata before either row comparison.
            core.require(self.full_private_snapshot() == before,
                         "The first isolated restart changed rows in the complete 28-table snapshot.")
            self.verify_browser_session()
            unchanged = self.http_json("/admin/v1/settings", administrator=True)
            self.assert_settings(unchanged, self.initial_defaults, FINAL_OVERRIDES,
                                 revision=final["Revision"], updated_at=final["UpdatedAt"])
            core.require(unchanged == final,
                         "The same-default restart changed the confirmed configuration settings projection.")
            self.verify_public_name(self.host_name)
            self.report["checks"].update({"first_restart_preserved_all_28_public_tables_exactly": True,
                                          "first_restart_preserved_name_mode_encoding_and_native_settings": True,
                                          "original_browser_login_survives_first_restart": True})

            self.stop_app()
            replacement_before = self.full_private_snapshot()
            core.require(replacement_before["managed_settings"] == before["managed_settings"],
                         "Read-only settings checks changed the stored managed settings row.")
            changed = {field for field in SETTINGS_FIELDS
                       if self.initial_defaults[field] != self.replacement_defaults[field]}
            core.require(changed == {"ServerName", "MaxWidth", "MaxAudioChannels"}
                         and all(FINAL_OVERRIDES[field] is None for field in changed),
                         "Replacement startup values changed an unapproved field or an active numeric override.")
            self.startup_defaults = dict(self.replacement_defaults)
            self.start_app()
            core.require(self.full_private_snapshot() == replacement_before,
                         "The replacement-default restart wrote defaults or changed rows in the complete 28-table snapshot.")
            self.verify_browser_session()
            replaced = self.http_json("/admin/v1/settings", administrator=True)
            self.assert_settings(replaced, self.replacement_defaults, FINAL_OVERRIDES,
                                 revision=final["Revision"], updated_at=final["UpdatedAt"])
            self.verify_public_name(self.host_name)
            reset = self.http_json("/admin/v1/settings/reset", administrator=True,
                                   body={"Revision": replaced["Revision"], "Fields": list(RESET_FIELDS)})
            empty_overrides = dict.fromkeys(SETTINGS_FIELDS)
            self.assert_settings(reset, self.replacement_defaults, empty_overrides,
                                 revision=str(int(replaced["Revision"]) + 1), name_mode="deployment",
                                 encoding={"TranscodingMaxWidth": 0})
            core.require(self.http_json("/admin/v1/settings", administrator=True) == reset,
                         "The six-field configuration reset did not remain committed.")
            self.verify_browser_session()
            self.verify_public_name(self.replacement_defaults["ServerName"])
            self.assert_stored_settings(self.full_private_snapshot(), revision=reset["Revision"],
                                        name_mode="deployment", overrides=empty_overrides,
                                        encoding={"TranscodingMaxWidth": 0})
            assets = self.runtime / "admin"
            core.require({path.relative_to(assets).as_posix(): core.file_digest(path)
                          for path in sorted(assets.rglob("*")) if path.is_file()} == self.ui_files,
                         "The loaded administrator UI files changed during configuration verification.")
            self.assert_private_logs()
            self.report["replacement_settings"] = replaced
            self.report["reset_settings"] = reset
            self.report["checks"].update({"second_restart_preserved_all_28_public_tables_exactly": True,
                                          "second_restart_changed_only_null_override_default_fields": True,
                                          "unset_name_keeps_host_fallback_after_deployment_name_changes": True,
                                          "native_null_width_and_channels_use_new_startup_defaults": True,
                                          "database_bitrate_height_and_encoding_width_survive_restart": True,
                                          "deployment_defaults_never_written_to_managed_settings": True,
                                          "original_browser_login_survives_second_restart": True,
                                          "six_field_reset_restores_deployment_mode_and_zero_encoding_width": True,
                                          "public_info_and_overview_use_effective_server_name": True,
                                          "loaded_ui_assets_unchanged": True})

    return ConfigurationSettingsRunner(args)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", required=True)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--assets-sha256", required=True)
    parser.add_argument("--core-runner", type=Path, default=Path(__file__).with_name("verify-managed-users.py"))
    parser.add_argument("--settings-runner", type=Path, default=Path(__file__).with_name("verify-settings.py"))
    args = parser.parse_args()
    settings = load_settings_runner(args.settings_runner)
    core = settings.load_core(args.core_runner)
    core.require(all(re.fullmatch(r"[0-9a-f]{64}", value) for value in (args.binary_sha256, args.assets_sha256)),
                 "Supply the prepared binary and administrator asset SHA-256 values.")

    def interrupted(signum, frame):
        raise core.VerificationError("The configuration settings verifier was interrupted; owned resources are being cleaned up.")

    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, settings, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "failure": "Configuration settings verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
