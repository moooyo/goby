#!/usr/bin/env python3
"""Accept one provisioned OCI schema-29 -> schema-50 upgrade and restore rollback.

Required private JSON: base_url, control_script, setup_token, admin_name,
admin_password, sample_path. Optional: movie_root (/media/movies),
expected_movies (2), timeout_seconds (180). sample_path is the host-side MP4;
movie_root is its library directory inside both application containers.

The control executable accepts exactly one operation: start-old, snapshot-old,
upgrade, rollback, status, or stop. Every operation must exit zero on success.
Only status writes a JSON object to stdout; its required fields are running
(bool), pid (int), image_id (str), master_key_sha256 (str), schema_version (int),
and database_name (str). Stopped status has running=false and pid=0; identity
fields may be empty and schema_version may be zero while stopped.
The first ready old image may have an empty master_key_sha256 because the
application creates its vault lazily. This driver issues a real application
key before recording the master-key baseline and taking the database dump.

start-old starts the old image against a fresh private schema-29 database.
snapshot-old retains a successful pre-upgrade pg_dump of the confirmed state.
upgrade starts the new image against that same database. rollback restores the
retained dump into a separate empty database and starts the old image against
it, retaining the same application master key. stop closes every application
container owned by this acceptance. The controller owns Docker/PostgreSQL
provisioning, dump/restore evidence, and database cleanup; this driver does not
construct shell commands or print configuration secrets.

After migration, this driver verifies persisted state before explicitly
rescanning the two movies once. The old image stores probe version 6 and the
new image requires version 8; playback rejects that obsolete cache until the
rescan. The rescan must preserve every confirmed ID and UserData field.
"""

import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


class Failure(Exception):
    pass


def need(condition, message):
    if not condition:
        raise Failure(message)


def emit(event, **values):
    print(json.dumps({"event": event, **values}, sort_keys=True), flush=True)


def load_config(path):
    path = Path(path)
    need(path.is_absolute(), "config_path_must_be_absolute")
    metadata = path.lstat()
    need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0,
         "config_must_be_a_private_regular_file")
    need(0 < metadata.st_size <= 16384, "config_size_is_invalid")
    value = json.loads(path.read_text(encoding="utf-8"))
    required = {"base_url", "control_script", "setup_token", "admin_name",
                "admin_password", "sample_path"}
    optional = {"movie_root", "expected_movies", "timeout_seconds"}
    need(type(value) is dict and required <= value.keys()
         and value.keys() <= required | optional, "invalid_config_fields")
    need(all(type(value[key]) is str and value[key] for key in required),
         "config_fields_must_be_nonempty_strings")
    origin = urllib.parse.urlsplit(value["base_url"])
    need(origin.scheme == "http" and origin.hostname in {"127.0.0.1", "::1", "localhost"}
         and origin.port is not None and origin.username is None and origin.password is None
         and origin.path in {"", "/"} and not origin.query and not origin.fragment,
         "base_url_must_be_one_explicit_loopback_origin")
    value["base_url"] = value["base_url"].rstrip("/")
    control = Path(value["control_script"])
    need(control.is_absolute() and control.is_file() and os.access(control, os.X_OK),
         "control_script_must_be_an_absolute_executable")
    sample = Path(value["sample_path"])
    need(sample.is_absolute() and sample.is_file() and sample.suffix.lower() == ".mp4",
         "sample_path_must_be_an_existing_absolute_mp4")
    need(0 < sample.stat().st_size <= 32 << 20, "sample_exceeds_bounded_http_profile")
    movie_root = value.get("movie_root", "/media/movies")
    need(type(movie_root) is str and PurePosixPath(movie_root).is_absolute()
         and ".." not in PurePosixPath(movie_root).parts, "invalid_container_movie_root")
    value["movie_root"] = movie_root
    expected = value.get("expected_movies", 2)
    need(type(expected) is int and expected == 2, "this_acceptance_requires_exactly_two_movies")
    value["expected_movies"] = expected
    timeout = value.get("timeout_seconds", 180)
    need(type(timeout) in {int, float} and 30 <= timeout <= 1800, "invalid_timeout_seconds")
    value["timeout_seconds"] = timeout
    return value


class Driver:
    def __init__(self, config):
        self.config = config
        self.timeout = config["timeout_seconds"]
        self.cookies = http.cookiejar.CookieJar()
        self.http = urllib.request.build_opener(
            urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(self.cookies))
        self.csrf = None
        self.token = None
        self.user_id = None
        self.library_id = None
        self.sample_id = None
        self.baseline_catalog = None
        self.baseline_userdata = None
        self.application_key_id = None
        self.application_key_token = None
        self.phase = "initialization"
        self.completed = []

    def control(self, operation):
        need(operation in {"start-old", "snapshot-old", "upgrade", "rollback", "status", "stop"},
             "unsupported_control_operation")
        try:
            result = subprocess.run([self.config["control_script"], operation],
                                    stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE, check=False,
                                    timeout=min(10, self.timeout) if operation == "status" else self.timeout)
        except subprocess.TimeoutExpired:
            raise Failure("control_timeout_" + operation) from None
        need(result.returncode == 0, "control_failed_" + operation)
        if operation != "status":
            emit("control", phase=self.phase, operation=operation)
            return None
        need(len(result.stdout) <= 65536, "control_status_too_large")
        value = json.loads(result.stdout)
        fields = {"running", "pid", "image_id", "master_key_sha256", "schema_version", "database_name"}
        need(type(value) is dict and fields <= value.keys(), "invalid_control_status_fields")
        need(type(value["running"]) is bool and type(value["pid"]) is int and value["pid"] >= 0
             and type(value["schema_version"]) is int and value["schema_version"] >= 0,
             "invalid_control_status_numbers")
        need(all(type(value[key]) is str for key in ("image_id", "master_key_sha256", "database_name")),
             "invalid_control_status_identity")
        need((value["running"] and value["pid"] > 0) or (not value["running"] and value["pid"] == 0),
             "inconsistent_control_process_status")
        return {key: value[key] for key in sorted(fields)}

    def raw_request(self, method, path, body=None, admin=False, public=False, timeout=10):
        need(path.startswith("/") and not path.startswith("//"), "invalid_http_route")
        headers = {"Origin": self.config["base_url"]}
        if admin and method != "GET":
            need(self.csrf is not None, "admin_session_is_missing")
            headers["X-CSRF-Token"] = self.csrf
        if public:
            need(self.token is not None, "public_session_is_missing")
            headers["X-Emby-Token"] = self.token
        if path == "/emby/Users/AuthenticateByName":
            headers["Authorization"] = ('Emby Client="OCIUpgrade", DeviceId="oci-upgrade-recovery", '
                                        'Device="Linux", Version="1.0"')
        data = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            data = json.dumps(body, separators=(",", ":")).encode()
        request = urllib.request.Request(self.config["base_url"] + path,
                                         data=data, headers=headers, method=method)
        try:
            response = self.http.open(request, timeout=timeout)
        except urllib.error.HTTPError as error:
            response = error
        except (urllib.error.URLError, TimeoutError, OSError):
            return 0, b""
        with response:
            content = response.read((32 << 20) + 1)
            need(len(content) <= 32 << 20, "http_response_exceeds_fixture_limit")
            return response.status, content

    def request(self, method, path, body=None, expected=200, admin=False, public=False, binary=False):
        status, content = self.raw_request(method, path, body, admin, public)
        if status != expected:
            error_code = None
            try:
                response = json.loads(content)
                if type(response) is dict:
                    for wrapper, key in (("Error", "Code"), ("ResponseStatus", "ErrorCode")):
                        error = response.get(wrapper)
                        value = error.get(key) if type(error) is dict else None
                        if type(value) is str and re.fullmatch(r"[A-Za-z0-9_.-]{1,128}", value):
                            error_code = value
                            break
            except (ValueError, UnicodeError):
                pass
            emit("http_failure", phase=self.phase, method=method, route=path.split("?", 1)[0],
                 status=status, expected=expected, error_code=error_code)
        need(status == expected, "http_status_" + str(status) + "_expected_" + str(expected))
        if binary:
            return content
        try:
            value = json.loads(content)
        except (ValueError, UnicodeError):
            raise Failure("http_response_is_not_json") from None
        need(type(value) is dict, "http_response_is_not_an_object")
        return value

    def wait(self, label, predicate):
        deadline = time.monotonic() + self.timeout
        while True:
            result = predicate()
            if result:
                return result
            need(time.monotonic() < deadline, "deadline_" + label)
            time.sleep(0.2)

    def ready(self, schema, require_master_key=True):
        def check():
            state = self.control("status")
            if not state["running"]:
                return None
            code, _ = self.raw_request("GET", "/readyz", timeout=3)
            if code != 200:
                return None
            # Startup can migrate the database between the first status read
            # and readiness. Assert against a fresh observation of that process.
            confirmed = self.control("status")
            if not confirmed["running"] or any(confirmed[key] != state[key]
                                               for key in ("pid", "image_id", "database_name")):
                return None
            return confirmed
        state = self.wait("application_ready", check)
        need(state["schema_version"] == schema, "unexpected_database_schema")
        valid_key_hash = re.fullmatch(r"[0-9a-f]{64}", state["master_key_sha256"]) is not None
        need(state["image_id"] and state["database_name"]
             and (valid_key_hash or (not require_master_key and state["master_key_sha256"] == "")),
             "running_identity_is_incomplete")
        emit("ready", phase=self.phase, **state)
        return state

    def login(self):
        # Obtain new credentials after each image switch; an existing cookie or
        # token must not substitute for a successful login against restored data.
        self.cookies.clear()
        self.csrf = None
        self.token = None
        status = self.request("GET", "/admin/v1/bootstrap")
        need(status["Initialized"] is True, "confirmed_setup_was_lost")
        admin = self.request("POST", "/admin/v1/session", {
            "Name": self.config["admin_name"], "Password": self.config["admin_password"],
        })
        self.csrf = admin["CSRFToken"]
        session = self.request("POST", "/emby/Users/AuthenticateByName", {
            "Username": self.config["admin_name"], "Pw": self.config["admin_password"],
        })
        self.token = session["AccessToken"]
        need(session["User"]["Id"] == self.user_id, "authenticated_user_identity_changed")

    def catalog(self):
        query = urllib.parse.urlencode({"ParentId": self.library_id, "Recursive": "true",
                                        "IncludeItemTypes": "Movie", "SortBy": "SortName", "Limit": 10})
        page = self.request("GET", "/emby/Items?" + query, public=True)
        expected = self.config["expected_movies"]
        need(page["TotalRecordCount"] == expected and len(page["Items"]) == expected,
             "catalog_population_changed")
        result = {item["Name"]: item["Id"] for item in page["Items"]}
        need(len(result) == expected and len(set(result.values())) == expected,
             "catalog_identity_not_unique")
        return result

    def userdata(self):
        result = {}
        for item_id in self.baseline_catalog.values():
            item = self.request("GET", "/emby/Users/" + self.user_id + "/Items/" + item_id, public=True)
            need(item["Id"] == item_id and type(item.get("UserData")) is dict and item["UserData"],
                 "confirmed_item_or_userdata_is_missing")
            # Compare the persisted schema-29 state rather than newer optional
            # DTO fields such as Rating, Likes, and HideFromResume.
            fields = {"PlaybackPositionTicks", "PlayCount", "IsFavorite", "Played", "LastPlayedDate"}
            need(fields <= item["UserData"].keys(), "confirmed_userdata_fields_are_missing")
            if "UnplayedItemCount" in item["UserData"]:
                fields.add("UnplayedItemCount")
            result[item_id] = {key: item["UserData"][key] for key in sorted(fields)}
        return result

    def finish_scan(self, job_id):
        def scan_finished():
            page = self.request("GET", "/admin/v1/jobs", admin=True)
            jobs = [job for job in page["Items"] if job["Id"] == job_id]
            need(len(jobs) == 1, "scan_job_missing_or_duplicated")
            job = jobs[0]
            if job["Status"] in {"pending", "running"}:
                return False
            need(job["Status"] == "completed" and not job["Error"]
                 and job["Scanned"] == self.config["expected_movies"], "scan_did_not_complete_exact_population")
            return True
        self.wait("scan_completion", scan_finished)

    def rescan_after_upgrade(self):
        result = self.request("POST", "/admin/v1/libraries/" + self.library_id + "/scan",
                              {"ForceProbe": True}, expected=202, admin=True)
        job_id = result["Job"]["Id"]
        self.finish_scan(job_id)
        emit("upgrade_media_rescan_completed", phase=self.phase, job_id=job_id,
             scanned=self.config["expected_movies"], force_probe=True,
             old_source_probe_version=6, current_source_probe_version=8)

    def assert_durable(self, state, verify_media=True):
        libraries = self.request("GET", "/admin/v1/libraries", admin=True)
        need(libraries["TotalRecordCount"] == 1 and len(libraries["Items"]) == 1
             and libraries["Items"][0]["Id"] == self.library_id
             and libraries["Items"][0]["Paths"] == [self.config["movie_root"]],
             "confirmed_library_identity_or_path_changed")
        need(self.catalog() == self.baseline_catalog, "confirmed_catalog_ids_changed")
        need(self.userdata() == self.baseline_userdata, "confirmed_userdata_changed")
        revealed = self.request("POST", "/admin/v1/api-keys/" + self.application_key_id + "/reveal",
                                {}, admin=True)
        need(revealed["Id"] == self.application_key_id
             and revealed["AccessToken"] == self.application_key_token,
             "confirmed_application_key_could_not_be_decrypted")
        media_evidence = {"direct_media_verified": verify_media}
        if verify_media:
            actual = self.request("GET", "/emby/Videos/" + self.sample_id + "/stream.mp4?Static=true",
                                  public=True, binary=True)
            expected = Path(self.config["sample_path"]).read_bytes()
            need(actual == expected, "direct_media_bytes_changed")
            media_evidence.update(direct_media_bytes=len(actual),
                                  direct_media_sha256=hashlib.sha256(actual).hexdigest())
        checkpoint = {"user_id": self.user_id, "library_id": self.library_id,
                      "catalog": self.baseline_catalog, "userdata": self.baseline_userdata}
        digest = hashlib.sha256(json.dumps(checkpoint, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
        emit("durable_checkpoint", phase=self.phase, checkpoint=checkpoint, checkpoint_sha256=digest,
             application_key_id=self.application_key_id, application_key_reveal_matches=True,
             **media_evidence, **state)

    def initialize(self):
        status = self.request("GET", "/admin/v1/bootstrap")
        need(status["Initialized"] is False, "a_fresh_private_database_is_required")
        created = self.request("POST", "/admin/v1/bootstrap", {
            "SetupToken": self.config["setup_token"], "Name": self.config["admin_name"],
            "Password": self.config["admin_password"],
        }, expected=201)
        self.user_id = created["User"]["Id"]
        self.login()
        issued = self.request("POST", "/admin/v1/api-keys", {"AppName": "OCI upgrade recovery"},
                              expected=201, admin=True)
        self.application_key_id = issued["Key"]["Id"]
        self.application_key_token = issued["AccessToken"]
        need(type(self.application_key_id) is str and self.application_key_id.isdecimal()
             and int(self.application_key_id) > 0
             and type(self.application_key_token) is str and self.application_key_token,
             "application_key_was_not_issued")
        library = self.request("POST", "/admin/v1/libraries", {
            "Name": "OCI upgrade recovery", "CollectionType": "movies",
            "Paths": [self.config["movie_root"]], "Scan": True,
        }, expected=201, admin=True)
        self.library_id = library["Library"]["Id"]
        self.finish_scan(library["Job"]["Id"])
        self.baseline_catalog = self.catalog()
        self.sample_id = self.baseline_catalog.get(Path(self.config["sample_path"]).stem)
        need(self.sample_id is not None, "sample_filename_did_not_match_one_catalog_movie")
        for item_id in self.baseline_catalog.values():
            base = "/emby/Users/" + self.user_id
            self.request("POST", base + "/FavoriteItems/" + item_id, public=True)
            self.request("POST", base + "/PlayedItems/" + item_id
                         + "?DatePlayed=2026-09-01T03%3A04%3A05Z", public=True)
        self.baseline_userdata = self.userdata()
        need(all(data.get("IsFavorite") is True and data.get("Played") is True
                 and data.get("PlayCount") == 1 and data.get("LastPlayedDate") == "2026-09-01T03:04:05Z"
                 for data in self.baseline_userdata.values()), "nondefault_userdata_not_persisted")

    def execute(self):
        self.phase = "old_image_baseline"
        self.control("start-old")
        initial = self.ready(29, require_master_key=False)
        self.initialize()
        old = self.ready(29)
        need(all(old[key] == initial[key] for key in ("pid", "image_id", "database_name")),
             "old_container_changed_during_initialization")
        self.assert_durable(old)
        self.control("snapshot-old")
        self.completed.append(self.phase)

        self.phase = "upgraded_image_schema50"
        self.control("upgrade")
        upgraded = self.ready(50)
        need(upgraded["image_id"] != old["image_id"], "upgrade_did_not_change_the_image")
        need(upgraded["database_name"] == old["database_name"], "upgrade_did_not_migrate_the_original_database")
        need(upgraded["master_key_sha256"] == old["master_key_sha256"], "upgrade_changed_the_master_key")
        self.login()
        self.assert_durable(upgraded, verify_media=False)
        self.rescan_after_upgrade()
        self.assert_durable(upgraded)
        self.completed.append(self.phase)

        self.phase = "restored_old_image_schema29"
        self.control("rollback")
        restored = self.ready(29)
        need(restored["image_id"] == old["image_id"], "rollback_did_not_restore_the_old_image")
        need(restored["database_name"] != old["database_name"], "rollback_did_not_use_a_separate_database")
        need(restored["master_key_sha256"] == old["master_key_sha256"], "rollback_changed_the_master_key")
        self.login()
        self.assert_durable(restored)
        self.completed.append(self.phase)

    def close(self):
        self.phase = "closure"
        self.control("stop")

        def stopped():
            state = self.control("status")
            return state if not state["running"] and state["pid"] == 0 else None
        self.wait("application_container_stop", stopped)
        code, _ = self.raw_request("GET", "/readyz", timeout=2)
        need(code == 0, "owned_http_listener_remained_open_after_stop")
        emit("closed", application_running=False, pid=0, http_listener_closed=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, help="Absolute path to private acceptance JSON")
    args = parser.parse_args()
    driver = None
    failure = None
    failed_phase = None

    def interrupted(_signum, _frame):
        raise Failure("driver_interrupted")

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        driver = Driver(load_config(args.config))
        driver.execute()
    except Failure as error:
        failure = str(error)
        failed_phase = None if driver is None else driver.phase
    except Exception as error:
        failure = "unexpected_" + type(error).__name__
        failed_phase = None if driver is None else driver.phase
    finally:
        if driver is not None:
            try:
                driver.close()
            except Exception as error:
                emit("closure_failed", reason=str(error) if isinstance(error, Failure)
                     else "unexpected_" + type(error).__name__)
                failure = failure or "closure_failed"
                failed_phase = failed_phase or "closure"
    emit("result", passed=failure is None, failure=failure, failed_phase=failed_phase,
         completed=[] if driver is None else driver.completed,
         scope="two_movie_actual_oci_schema29_to50_and_preupgrade_dump_restore_rollback")
    return 0 if failure is None else 1


if __name__ == "__main__":
    sys.exit(main())
