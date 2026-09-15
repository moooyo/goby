"""Fixed seven-file HTTP journey; import only, with process ownership outside.

Keep one CatalogJourney instance across the two real server starts. The caller
owns namespace, database, media, process deadlines, and raw SQL evidence. This
module never starts a process, scans a filesystem, plays media, or retries a
mutation. Authentication material lives only in this instance's memory.
"""

import base64
import copy
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
import re
import stat
import time
from urllib.parse import urlencode, urlsplit


BODY_LIMIT = 1 << 20
DATE_PLAYED = "2024-02-03T04:05:06Z"
ROLES = ("admin", "visible", "hidden")
LEAF_PATHS = {
    "movie": "mixed/Film.mp4",
    "episode": "mixed/Show.S01E01.mp4",
    "t1": "mixed/A/01 Played.flac",
    "t2": "mixed/A/02 Moving.flac",
    "t3": "mixed/B/03 Played.flac",
    "h1": "hidden/Hidden Album/01 Hidden.flac",
    "h2": "hidden/Hidden Album/02 Hidden.flac",
}
ALBUM_PATHS = {"a": "mixed/A", "b": "mixed/B", "h": "hidden/Hidden Album"}
VISIBLE_KEYS = {
    "admin": tuple(LEAF_PATHS),
    "visible": ("movie", "episode", "t1", "t2", "t3"),
    "hidden": ("h1", "h2"),
}
POLICY_FIELDS = {
    "EnableAllFolders", "EnabledFolders", "EnableMediaPlayback",
    "EnablePlaybackRemuxing", "EnableAudioPlaybackTranscoding",
    "EnableVideoPlaybackTranscoding",
}
ITEM_FIELDS = (
    "Id", "Name", "SortName", "Type", "IsFolder", "ServerId", "ParentId",
    "Path", "IndexNumber", "ParentIndexNumber", "SeriesId", "SeriesName",
    "SeasonId", "SeasonName", "AlbumId", "Album", "Artists", "ArtistItems",
    "AlbumArtists", "AlbumArtist", "ChildCount", "MediaType", "RunTimeTicks",
)


class JourneyError(RuntimeError):
    """Only a fixed, secret-free error code may leave the HTTP adapter."""


def require(condition, code):
    if not condition:
        raise JourneyError(code)


def identifier(value):
    require(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{32}", value),
            "invalid_catalog_identifier")
    return value


def token_value(value):
    require(isinstance(value, str) and re.fullmatch(r"[A-Za-z0-9_-]{43}", value),
            "invalid_credential_shape")
    try:
        decoded = base64.urlsafe_b64decode(value + "=")
    except Exception:
        raise JourneyError("invalid_credential_encoding") from None
    require(len(decoded) == 32 and base64.urlsafe_b64encode(decoded).decode().rstrip("=") == value,
            "invalid_credential_encoding")
    return value


class CatalogJourney:
    def __init__(self, origin, media_root, setup_token, passwords, nonce, record_dir):
        parsed = urlsplit(origin)
        require(parsed.scheme == "http" and parsed.hostname == "127.0.0.1" and
                parsed.username is None and parsed.password is None and
                parsed.path == "" and not parsed.query and not parsed.fragment and
                parsed.port is not None and 1024 <= parsed.port <= 65535 and
                origin == "http://127.0.0.1:" + str(parsed.port), "invalid_fixture_origin")
        require(isinstance(media_root, str) and media_root.startswith("/") and
                media_root != "/" and not media_root.endswith("/") and
                all(part not in ("", ".", "..") for part in media_root.split("/")[1:]) and
                all(ord(character) >= 32 for character in media_root), "invalid_media_root")
        require(isinstance(nonce, str) and re.fullmatch(r"[a-z0-9]{8,40}", nonce), "invalid_nonce")
        require(isinstance(passwords, dict) and set(passwords) == set(ROLES) and
                all(isinstance(passwords[role], str) and 12 <= len(passwords[role]) <= 72
                    for role in ROLES), "invalid_private_password_inputs")
        require(isinstance(setup_token, str) and 16 <= len(setup_token) <= 512, "invalid_setup_token_input")
        require(isinstance(record_dir, str) and os.path.isabs(record_dir), "invalid_record_directory")
        directory = os.lstat(record_dir)
        require(stat.S_ISDIR(directory.st_mode) and not stat.S_ISLNK(directory.st_mode) and
                directory.st_uid == os.geteuid() and directory.st_mode & 0o077 == 0,
                "record_directory_not_private")
        self.origin, self.port, self.media_root = origin, parsed.port, media_root
        self.setup_token, self.passwords, self.nonce = setup_token, dict(passwords), nonce
        self.names = {role: "catalog-" + nonce + "-" + role for role in ROLES}
        self.trace_path = os.path.join(record_dir, "native-catalog-http.jsonl")
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
        descriptor = os.open(self.trace_path, flags, 0o600)
        try:
            os.fsync(descriptor)
            self.trace_identity = os.fstat(descriptor)
        finally:
            os.close(descriptor)
        self.credentials, self.users, self.libraries, self.jobs = [], {}, {}, {}
        self.phase, self.serial, self.writes = "prepared", 0, set()
        self.before = None
        self.dashboard_before = None
        self.admin = None
        self.emby = {}
        self.events = []
        self.trace_persisted = True
        self._record({"event": "created", "profile": "seven-file-native-catalog"})

    def _record(self, value):
        encoded = (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()
        require(len(encoded) < 8192, "trace_record_too_large")
        self.events.append(value)
        descriptor = os.open(self.trace_path, os.O_WRONLY | os.O_APPEND | getattr(os, "O_NOFOLLOW", 0))
        try:
            current = os.fstat(descriptor)
            require(stat.S_ISREG(current.st_mode) and current.st_nlink == 1 and
                    (current.st_dev, current.st_ino, current.st_uid, stat.S_IMODE(current.st_mode)) ==
                    (self.trace_identity.st_dev, self.trace_identity.st_ino, os.geteuid(), 0o600),
                    "trace_identity_changed")
            require(current.st_size < 4 << 20, "trace_budget_exceeded")
            offset = 0
            while offset < len(encoded):
                written = os.write(descriptor, encoded[offset:])
                require(written > 0, "trace_write_incomplete")
                offset += written
            os.fsync(descriptor)
        finally:
            os.close(descriptor)

    def _safe_path(self, path):
        parsed = urlsplit(path)
        safe = re.sub(r"(?<=/)[0-9a-f]{32}(?=/|$)", "{id}", parsed.path)
        if parsed.query:
            safe += "?" + "&".join(part.split("=", 1)[0] + "={value}" for part in parsed.query.split("&"))
        return safe

    def _headers(self, credential):
        headers = {"Accept": "application/json", "Accept-Encoding": "identity", "Connection": "close"}
        if credential is None:
            return headers
        if credential["kind"] == "admin":
            headers.update({"Cookie": "goby_session=" + credential["value"],
                            "Origin": self.origin, "X-CSRF-Token": credential["csrf"]})
        else:
            headers.update(self._client_headers(credential["role"]))
            headers["X-Emby-Token"] = credential["value"]
        return headers

    def _client_headers(self, role):
        return {"X-Emby-Client": "Goby Native Catalog Audit", "X-Emby-Client-Version": "1",
                "X-Emby-Device-Id": "catalog-" + self.nonce + "-" + role,
                "X-Emby-Device-Name": "Isolated catalog fixture"}

    def _request(self, method, path, expected, *, body=None, credential=None,
                 label, json_body=True, headers=None, on_headers=None, on_json=None, deadline=None,
                 cleanup_request=False):
        require(method in ("GET", "POST", "PUT", "DELETE") and path.startswith("/") and
                not path.startswith("//") and "\r" not in path and "\n" not in path,
                "invalid_request")
        require(self.serial < 500, "request_budget_exceeded")
        mutation = method != "GET"
        key = (self.phase, label)
        if mutation:
            require(key not in self.writes, "mutation_already_attempted")
            self.writes.add(key)
        payload = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        require(payload is None or len(payload) <= BODY_LIMIT, "request_body_too_large")
        self.serial += 1
        evidence = {"sequence": self.serial, "phase": self.phase, "label": label,
                    "method": method, "path": self._safe_path(path), "expectedStatus": expected,
                    "actualStatus": None, "responseBytes": 0, "requestId": None}
        try:
            self._record(dict(evidence, event="request-intent"))
        except Exception:
            self.trace_persisted = False
            if not cleanup_request:
                raise JourneyError("request_intent_persistence_failed") from None
            self.events.append(dict(evidence, event="request-intent-memory-only"))
        connection, response, transport = None, None, None
        dispatched, complete = False, False
        parsed = None
        try:
            outgoing = self._headers(credential)
            if headers:
                outgoing.update(headers)
            if payload is not None:
                outgoing["Content-Type"] = "application/json"
            deadline = min(time.monotonic() + 10.0, deadline) if deadline is not None else time.monotonic() + 10.0
            timeout = deadline - time.monotonic()
            require(timeout > 0, "request_deadline_exceeded")
            connection = http.client.HTTPConnection("127.0.0.1", self.port, timeout=timeout)
            dispatched = True
            connection.request(method, path, body=payload, headers=outgoing)
            transport = connection.sock
            require(time.monotonic() < deadline, "request_deadline_exceeded")
            if connection.sock is not None:
                connection.sock.settimeout(max(0.001, deadline - time.monotonic()))
            response = connection.getresponse()
            evidence["actualStatus"] = response.status
            request_id = response.getheader("X-Request-ID")
            if isinstance(request_id, str) and re.fullmatch(r"[0-9a-f]{32}", request_id):
                evidence["requestId"] = request_id
            # Capture an owned cookie before body/status/schema validation.
            if on_headers is not None:
                on_headers(response.getheaders())
            chunks, count = [], 0
            while True:
                remaining = deadline - time.monotonic()
                require(remaining > 0, "request_deadline_exceeded")
                if transport is not None:
                    transport.settimeout(max(0.001, remaining))
                chunk = response.read1(min(65536, BODY_LIMIT + 1 - count))
                if not chunk:
                    break
                chunks.append(chunk)
                count += len(chunk)
                evidence["responseBytes"] = count
                require(count <= BODY_LIMIT, "response_body_too_large")
                if response.isclosed():
                    break
            raw = b"".join(chunks)
            require(response.length in (None, 0), "response_body_incomplete")
            complete = True
            if json_body:
                try:
                    parsed = json.loads(raw)
                except Exception:
                    raise JourneyError("invalid_json_response") from None
                # Capture an owned access token before validating other fields.
                if on_json is not None:
                    on_json(parsed)
            require(evidence["requestId"] is not None, "missing_request_id")
            require(response.status == expected, "unexpected_http_status")
            require(response.getheader("Content-Encoding") in (None, "identity"), "unexpected_response_encoding")
            if json_body:
                require(response.getheader("Content-Type", "").split(";", 1)[0] == "application/json",
                        "unexpected_json_content_type")
            if expected == 204:
                require(not raw, "logout_response_not_empty")
            response.close()
            response = None
            connection.close()
            connection = None
            try:
                self._record(dict(evidence, event="request-complete", unknownOutcome=False,
                                  connectionClosed=True))
            except Exception:
                self.trace_persisted = False
                if not cleanup_request:
                    raise JourneyError("response_evidence_persistence_failed") from None
                self.events.append(dict(evidence, event="request-complete-memory-only", unknownOutcome=False,
                                        connectionClosed=True))
            return parsed if json_body else raw
        except Exception as error:
            code = str(error) if isinstance(error, JourneyError) else "http_exchange_failed"
            closed = True
            try:
                if response is not None:
                    response.close()
                    response = None
                if connection is not None:
                    connection.close()
                    connection = None
            except Exception:
                closed = False
            event = dict(evidence, event="request-failed", error=code,
                         unknownOutcome=mutation and dispatched,
                         responseComplete=complete, connectionClosed=closed)
            try:
                self._record(event)
            except Exception:
                self.trace_persisted = False
                self.events.append(dict(event, evidencePersistenceFailed=True))
            raise JourneyError(code) from None
        finally:
            if response is not None:
                response.close()
            if connection is not None:
                connection.close()

    def _query(self, path, values=None):
        return path if not values else path + "?" + urlencode(values)

    def _admin_json(self, method, path, expected=200, *, body=None, label, deadline=None):
        require(self.admin is not None and not self.admin["attempted"], "administrator_session_unavailable")
        return self._request(method, path, expected, body=body, credential=self.admin, label=label, deadline=deadline)

    def _emby_json(self, role, path, values=None, *, method="GET", label):
        credential = self.emby[role]
        require(not credential["attempted"], "emby_session_unavailable")
        return self._request(method, self._query(path, values), 200, credential=credential, label=label)

    def _remember(self, kind, role, value):
        credential = {"kind": kind, "role": role, "phase": self.phase,
                      "value": token_value(value), "attempted": False, "logout204": False,
                      "rejected401": False}
        if kind == "admin":
            credential["csrf"] = hashlib.sha256(("goby:admin:csrf:" + value).encode()).hexdigest()
        self.credentials.append(credential)
        return credential

    def _login_admin(self):
        require(self.admin is None or self.admin["rejected401"], "administrator_login_not_closed")

        def capture(headers):
            for name, value in headers:
                if name.lower() != "set-cookie":
                    continue
                parsed = SimpleCookie()
                parsed.load(value)
                if "goby_session" in parsed and parsed["goby_session"].value:
                    require(self.admin is None or self.admin["rejected401"], "duplicate_admin_cookie")
                    self.admin = self._remember("admin", "admin", parsed["goby_session"].value)

        reply = self._request("POST", "/admin/v1/session", 200,
                              body={"Name": self.names["admin"], "Password": self.passwords["admin"]},
                              headers={"Origin": self.origin}, label="admin-login", on_headers=capture)
        require(self.admin is not None and reply.get("CSRFToken") == self.admin["csrf"], "admin_login_csrf_mismatch")
        self._check_native_user(reply.get("User"), "admin")
        current = self._admin_json("GET", "/admin/v1/session", label="admin-session")
        require(current.get("CSRFToken") == self.admin["csrf"], "admin_session_csrf_mismatch")
        self._check_native_user(current.get("User"), "admin")

    def _check_native_user(self, user, role):
        require(isinstance(user, dict), "invalid_native_user")
        user_id = identifier(user.get("Id"))
        require(user.get("Name") == self.names[role] and user.get("IsAdministrator") is (role == "admin") and
                user.get("IsDisabled") is False and user.get("HasPassword") is True, "native_user_mismatch")
        if role in self.users:
            require(self.users[role] == user_id, "user_identity_changed")
        self.users[role] = user_id
        return user

    def _login_emby(self, role):
        require(role not in self.emby or self.emby[role]["rejected401"], "emby_login_not_closed")

        def capture(reply):
            if isinstance(reply, dict) and isinstance(reply.get("AccessToken"), str):
                self.emby[role] = self._remember("emby", role, reply["AccessToken"])

        reply = self._request("POST", "/emby/Users/AuthenticateByName", 200,
                              body={"Username": self.names[role], "Pw": self.passwords[role]},
                              headers=self._client_headers(role), label="emby-login-" + role, on_json=capture)
        require(role in self.emby, "emby_token_missing")
        require(reply.get("User", {}).get("Id") == self.users[role] and
                reply.get("User", {}).get("Name") == self.names[role], "emby_login_user_mismatch")
        identifier(reply.get("ServerId"))
        require(reply.get("SessionInfo", {}).get("UserId") == self.users[role], "emby_session_user_mismatch")

    def _logout(self, credential):
        require(not credential["attempted"], "logout_already_attempted")
        credential["attempted"] = True
        suffix = credential["phase"] + "-" + credential["kind"] + "-" + credential["role"]
        if credential["kind"] == "admin":
            method, path, check = "DELETE", "/admin/v1/session", "/admin/v1/session"
        else:
            method, path = "POST", "/emby/Sessions/Logout"
            check = "/emby/Users/" + self.users[credential["role"]] + "/Items?Limit=0"
        self._request(method, path, 204, credential=credential, label="logout-" + suffix, json_body=False,
                      cleanup_request=True)
        credential["logout204"] = True
        self._request("GET", check, 401, credential=credential, label="revoked-check-" + suffix, json_body=False,
                      cleanup_request=True)
        credential["rejected401"] = True
        credential["value"] = ""
        credential.pop("csrf", None)

    def cleanup(self):
        """Attempt each currently held, unattempted logout once; never re-login."""
        failures = []
        for credential in reversed(self.credentials):
            if not credential["attempted"]:
                try:
                    self._logout(credential)
                except Exception:
                    failures.append(credential["kind"] + "-" + credential["role"])
        return {"status": "closed" if all(c["rejected401"] for c in self.credentials) else "incomplete",
                "credentials": [{key: credential[key] for key in
                                 ("kind", "role", "phase", "attempted", "logout204", "rejected401")}
                                for credential in self.credentials], "cleanupFailures": failures,
                "tracePersisted": self.trace_persisted}

    def _finish(self, catalog, dashboard):
        closure = self.cleanup()
        require(closure["status"] == "closed", "credential_closure_incomplete")
        require(self.trace_persisted, "http_trace_not_fully_persisted")
        result = {"status": "passed", "phase": self.phase, "catalog": catalog,
                  "scanJobs": copy.deepcopy(self.jobs), "dashboard": dashboard,
                  "credentialClosure": closure, "requestCount": self.serial}
        self._record({"event": "stage-passed", "phase": self.phase, "requestCount": self.serial})
        return copy.deepcopy(result)

    def _dashboard(self):
        raw = self._request("GET", "/admin/", 200, label="dashboard-index", json_body=False)
        require(len(raw) > 0 and b"<html" in raw.lower(), "dashboard_index_missing")
        return {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest(),
                "claim": "served index bytes; embedded provenance is verified by the caller"}

    def _scan(self, key, expected_count):
        library_id = self.libraries[key]
        reply = self._admin_json("POST", "/admin/v1/libraries/" + library_id + "/scan", 202,
                                 body={"ForceProbe": False}, label="scan-" + key)
        job = reply.get("Job", {})
        job_id = identifier(job.get("Id"))
        require(job.get("LibraryId") == library_id and job.get("ForceProbe") is False, "scan_identity_mismatch")
        self.jobs[key] = {"Id": job_id, "LibraryId": library_id}
        deadline = time.monotonic() + 60.0
        while True:
            require(time.monotonic() < deadline, "scan_readiness_timeout")
            page = self._admin_json("GET", "/admin/v1/jobs", label="scan-readiness-" + key, deadline=deadline)
            require(isinstance(page.get("Items"), list) and len(page["Items"]) <= 2 and
                    page.get("TotalRecordCount") == len(page["Items"]), "unexpected_scan_inventory")
            matches = [value for value in page["Items"] if value.get("Id") == job_id]
            require(len(matches) == 1, "scan_job_missing")
            job = matches[0]
            require(job.get("LibraryId") == library_id and job.get("ForceProbe") is False, "scan_identity_changed")
            if job.get("Status") == "completed":
                require(job.get("Error") == "" and job.get("Scanned") == expected_count and
                        job.get("Added") == expected_count and job.get("Updated") == 0 and
                        job.get("StartedAt") and job.get("FinishedAt"), "scan_result_mismatch")
                self.jobs[key] = {field: job[field] for field in
                                  ("Id", "LibraryId", "ForceProbe", "Status", "Error", "Scanned", "Added",
                                   "Updated", "CreatedAt", "StartedAt", "FinishedAt")}
                return
            require(job.get("Status") in ("pending", "running"), "scan_terminal_failure")
            time.sleep(min(0.5, max(0.0, deadline - time.monotonic())))

    def _create_users(self):
        for role, key in (("visible", "mixed"), ("hidden", "hidden")):
            reply = self._admin_json("POST", "/admin/v1/users", 201,
                                     body={"Name": self.names[role], "Password": self.passwords[role],
                                           "IsAdministrator": False}, label="create-user-" + role)
            self._check_native_user(reply.get("User"), role)
            path = "/admin/v1/users/" + self.users[role]
            current = self._admin_json("GET", path, label="managed-user-before-" + role)["User"]
            self._check_native_user(current, role)
            revision = current.get("Revision")
            require(isinstance(revision, str) and re.fullmatch(r"[1-9][0-9]*", revision), "invalid_user_revision")
            require(set(current.get("Policy", {})) == POLICY_FIELDS, "managed_policy_contract_changed")
            policy = {"EnableAllFolders": False, "EnabledFolders": [self.libraries[key]],
                      "EnableMediaPlayback": True, "EnablePlaybackRemuxing": False,
                      "EnableAudioPlaybackTranscoding": False, "EnableVideoPlaybackTranscoding": False}
            update = {"Revision": revision, "Name": self.names[role], "IsAdministrator": False,
                      "IsDisabled": False, "Policy": policy}
            result = self._admin_json("PUT", path, body=update, label="set-user-policy-" + role)
            require(result.get("CurrentSessionRevoked") is False, "administrator_unexpectedly_revoked")
            self._check_native_user(result.get("User"), role)
            require(result["User"].get("Policy") == policy and
                    result["User"].get("Revision") == str(int(revision) + 1), "managed_policy_update_mismatch")

    def _list(self, role, values=None, *, label):
        return self._emby_json(role, "/emby/Users/" + self.users[role] + "/Items", values, label=label)

    def _items(self, page, count):
        require(isinstance(page, dict) and isinstance(page.get("Items"), list) and
                page.get("TotalRecordCount") == count and len(page["Items"]) == count, "item_count_mismatch")
        ids = [identifier(item.get("Id")) for item in page["Items"]]
        require(len(set(ids)) == count, "duplicate_item_ids")
        return page["Items"]

    def _detail(self, role, item_id, label):
        return self._emby_json(role, "/emby/Users/" + self.users[role] + "/Items/" + item_id, label=label)

    def _discover(self):
        rows = self._items(self._list("admin", {"Recursive": "true", "Fields": "Path", "Limit": 64},
                                      label="discover-catalog"), 12)
        by_path = {item["Path"]: item for item in rows if item.get("Path")}
        result = {}
        for key, relative in dict(LEAF_PATHS, **ALBUM_PATHS).items():
            item = by_path.get(self.media_root + "/" + relative)
            require(isinstance(item, dict), "fixture_item_path_missing")
            result[key] = identifier(item.get("Id"))
        episode = next(item for item in rows if item["Id"] == result["episode"])
        result["series"] = identifier(episode.get("SeriesId"))
        result["season"] = identifier(episode.get("SeasonId"))
        require(set(result.values()) == {item["Id"] for item in rows} and len(set(result.values())) == 12,
                "unexpected_catalog_hierarchy")
        return result

    def _write_user_data(self, items):
        for role, key, action in (("visible", "movie", "FavoriteItems"),
                                  ("visible", "episode", "PlayedItems"),
                                  ("visible", "t1", "PlayedItems"),
                                  ("visible", "a", "FavoriteItems"),
                                  ("hidden", "h1", "FavoriteItems"),
                                  ("hidden", "h1", "PlayedItems")):
            path = "/emby/Users/" + self.users[role] + "/" + action + "/" + items[key]
            values = {"DatePlayed": DATE_PLAYED} if action == "PlayedItems" else None
            reply = self._emby_json(role, path, values, method="POST", label="userdata-" + role + "-" + key + "-" + action)
            expected = self._expected_data(role, key)
            if role == "hidden" and key == "h1" and action == "FavoriteItems":
                expected = {"PlaybackPositionTicks": 0, "PlayCount": 0, "IsFavorite": True, "Played": False}
            require(reply == expected, "userdata_mutation_projection_mismatch")

    def _expected_data(self, role, key):
        favorite = (role, key) in (("visible", "movie"), ("visible", "a"), ("hidden", "h1"))
        played = (role, key) in (("visible", "episode"), ("visible", "t1"), ("hidden", "h1"))
        value = {"PlaybackPositionTicks": 0, "PlayCount": int(played), "IsFavorite": favorite, "Played": played}
        if played:
            value["LastPlayedDate"] = DATE_PLAYED
        children = {"a": ("t1", "t2"), "b": ("t3",), "h": ("h1", "h2"),
                    "series": ("episode",), "season": ("episode",)}
        if key in children:
            unplayed = sum(not self._expected_data(role, child)["Played"] for child in children[key])
            value.update({"PlayCount": 0, "Played": unplayed == 0, "UnplayedItemCount": unplayed})
        return value

    def _snapshot(self, items):
        snapshot = {"users": dict(self.users), "libraries": dict(self.libraries), "items": dict(items),
                    "subjects": {}, "managedUsers": {}}
        for role in ROLES:
            user = self._admin_json("GET", "/admin/v1/users/" + self.users[role], label="snapshot-user-" + role)["User"]
            self._check_native_user(user, role)
            snapshot["managedUsers"][role] = user
            allowed_libraries = [self.libraries[key] for key in
                                 (("mixed", "hidden") if role == "admin" else (("mixed",) if role == "visible" else ("hidden",)))]
            views = self._emby_json(role, "/emby/Users/" + self.users[role] + "/Views", label="views-" + role)
            view_rows = self._items(views, len(allowed_libraries))
            require({row["Id"] for row in view_rows} == set(allowed_libraries) and
                    all(row.get("Type") == "CollectionFolder" and row.get("IsFolder") is True for row in view_rows),
                    "views_acl_mismatch")
            for row in view_rows:
                require(row.get("CollectionType") == (None if row["Id"] == self.libraries["mixed"] else "music"),
                        "view_collection_type_mismatch")
            leaf_ids = {items[key] for key in VISIBLE_KEYS[role]}
            leaf_query = {"Recursive": "true", "IncludeItemTypes": "Movie,Episode,Audio", "Fields": "Path", "Limit": 64}
            leaves = self._items(self._list(role, leaf_query, label="leaf-list-" + role), len(leaf_ids))
            require({row["Id"] for row in leaves} == leaf_ids, "leaf_acl_mismatch")
            count_page = self._list(role, dict(leaf_query, Limit=0), label="leaf-count-only-" + role)
            require(count_page.get("Items") == [] and count_page.get("TotalRecordCount") == len(leaf_ids),
                    "count_only_contract_mismatch")
            keys = tuple(VISIBLE_KEYS[role]) + (("a", "b", "series", "season") if role == "visible" else
                                              (("h",) if role == "hidden" else ("a", "b", "h", "series", "season")))
            full = self._items(self._list(role, {"Recursive": "true", "Fields": "Path", "Limit": 64},
                                         label="full-list-" + role), len(keys))
            by_id = {row["Id"]: row for row in full}
            require(set(by_id) == {items[key] for key in keys}, "hierarchy_acl_mismatch")
            subject = {"views": sorted(allowed_libraries), "leafIds": sorted(leaf_ids), "catalog": {}}
            for key in keys:
                detail = self._detail(role, items[key], "detail-" + role + "-" + key)
                require(identifier(detail.get("Id")) == items[key], "detail_identity_mismatch")
                projection = {field: detail[field] for field in ITEM_FIELDS if field in detail}
                listed = {field: by_id[items[key]][field] for field in ITEM_FIELDS if field in by_id[items[key]]}
                require(projection == listed, "list_detail_projection_mismatch")
                self._check_item(key, projection, items)
                expected_data = self._expected_data(role, key)
                require(detail.get("UserData") == expected_data and by_id[items[key]].get("UserData") == expected_data,
                        "userdata_read_projection_mismatch")
                subject["catalog"][key] = dict(projection, UserData=expected_data)
            # Music filters use actual persisted artist IDs, not entity-kind listing.
            audio = [by_id[items[key]] for key in VISIBLE_KEYS[role] if key not in ("movie", "episode")]
            require(audio and all(row.get("ArtistItems") for row in audio), "accepted_music_artist_missing")
            artist_ids = {pair["Id"] for row in audio for pair in row["ArtistItems"]}
            require(len(artist_ids) == 1, "fixture_shared_artist_mismatch")
            artist_id = next(iter(artist_ids))
            require(isinstance(artist_id, str) and re.fullmatch(r"[1-9][0-9]*", artist_id), "invalid_artist_id")
            expected_audio = {row["Id"] for row in audio}
            for field in ("ArtistIds", "AlbumArtistIds"):
                filtered = self._items(self._list(role, {"Recursive": "true", "IncludeItemTypes": "Audio",
                                                       field: artist_id, "Limit": 64},
                                                 label="music-filter-" + role + "-" + field), len(expected_audio))
                require({row["Id"] for row in filtered} == expected_audio, "music_filter_acl_mismatch")
            subject["artistId"] = artist_id
            snapshot["subjects"][role] = subject
        for role, forbidden_key, other_role in (("visible", "h1", "hidden"), ("hidden", "movie", "visible")):
            path = "/emby/Users/" + self.users[role] + "/Items/" + items[forbidden_key]
            self._request("GET", path, 404, credential=self.emby[role], label="hidden-item-" + role)
            path = "/emby/Users/" + self.users[other_role] + "/Items?Limit=0"
            self._request("GET", path, 403, credential=self.emby[role], label="cross-user-" + role)
        require(len({snapshot["subjects"][role]["artistId"] for role in ROLES}) == 1, "shared_artist_identity_changed")
        return snapshot

    def _check_item(self, key, item, items):
        expected_type = "Movie" if key == "movie" else ("Episode" if key == "episode" else
                        ("Series" if key == "series" else ("Season" if key == "season" else
                         ("MusicAlbum" if key in ALBUM_PATHS else "Audio"))))
        require(item.get("Type") == expected_type and item.get("IsFolder") is (key not in LEAF_PATHS),
                "item_kind_mismatch")
        parents = {"movie": self.libraries["mixed"], "series": self.libraries["mixed"],
                   "season": items["series"], "episode": items["season"],
                   "a": self.libraries["mixed"], "b": self.libraries["mixed"], "h": self.libraries["hidden"],
                   "t1": items["a"], "t2": items["a"], "t3": items["b"], "h1": items["h"], "h2": items["h"]}
        require(item.get("ParentId") == parents[key], "item_parent_mismatch")
        if key in LEAF_PATHS:
            require(item.get("Path") == self.media_root + "/" + LEAF_PATHS[key] and
                    type(item.get("RunTimeTicks")) is int and item["RunTimeTicks"] > 0,
                    "real_media_projection_missing")
        if key in ALBUM_PATHS:
            require(item.get("Path") == self.media_root + "/" + ALBUM_PATHS[key] and
                    item.get("ChildCount") == {"a": 2, "b": 1, "h": 2}[key], "album_child_count_mismatch")
        if key in ("t1", "t2", "t3", "h1", "h2"):
            require(item.get("AlbumId") == parents[key] and item.get("MediaType") == "Audio", "audio_album_mismatch")
        if key == "episode":
            require(item.get("SeriesId") == items["series"] and item.get("SeasonId") == items["season"] and
                    item.get("IndexNumber") == 1 and item.get("ParentIndexNumber") == 1, "episode_hierarchy_mismatch")
        if key == "season":
            require(item.get("SeriesId") == items["series"] and item.get("IndexNumber") == 1, "season_hierarchy_mismatch")

    def first_start(self):
        require(self.phase == "prepared", "first_stage_already_attempted")
        self.phase = "first_start"
        dashboard = self._dashboard()
        status = self._request("GET", "/admin/v1/bootstrap", 200, label="bootstrap-status")
        require(status == {"Initialized": False}, "fixture_already_initialized")
        reply = self._request("POST", "/admin/v1/bootstrap", 201, headers={"Origin": self.origin},
                              body={"SetupToken": self.setup_token, "Name": self.names["admin"],
                                    "Password": self.passwords["admin"]}, label="bootstrap")
        self._check_native_user(reply.get("User"), "admin")
        self._login_admin()
        empty = self._admin_json("GET", "/admin/v1/libraries", label="initial-libraries")
        require(empty == {"Items": [], "TotalRecordCount": 0}, "fixture_libraries_not_empty")
        empty = self._admin_json("GET", "/admin/v1/jobs", label="initial-jobs")
        require(empty == {"Items": [], "TotalRecordCount": 0}, "fixture_jobs_not_empty")
        for key, kind, count in (("mixed", "mixed", 5), ("hidden", "music", 2)):
            reply = self._admin_json("POST", "/admin/v1/libraries", 201,
                                     body={"Name": "Catalog " + key, "CollectionType": kind,
                                           "Paths": [self.media_root + "/" + key], "Scan": False},
                                     label="create-library-" + key)
            require(set(reply) == {"Library"}, "implicit_scan_not_allowed")
            library = reply["Library"]
            self.libraries[key] = identifier(library.get("Id"))
            require(library.get("CollectionType") == kind and library.get("Paths") == [self.media_root + "/" + key],
                    "library_creation_mismatch")
            self._scan(key, count)
        self._create_users()
        for role in ROLES:
            self._login_emby(role)
        items = self._discover()
        self._write_user_data(items)
        catalog = self._snapshot(items)
        result = self._finish(catalog, dashboard)
        self.before, self.dashboard_before = copy.deepcopy(catalog), copy.deepcopy(dashboard)
        self.phase = "first_complete"
        return result

    def second_start(self):
        require(self.phase == "first_complete" and self.before is not None, "second_stage_requires_closed_first_stage")
        self.phase = "second_start"
        dashboard = self._dashboard()
        require(dashboard == self.dashboard_before, "served_index_changed_after_restart")
        status = self._request("GET", "/admin/v1/bootstrap", 200, label="bootstrap-status")
        require(status == {"Initialized": True}, "initialization_lost_after_restart")
        self._login_admin()
        for role in ROLES:
            self._login_emby(role)
        jobs = self._admin_json("GET", "/admin/v1/jobs", label="jobs-after-restart")
        actual = self._items(jobs, 2)
        require({job["Id"] for job in actual} == {job["Id"] for job in self.jobs.values()}, "restart_created_scan")
        for job in actual:
            prior = next(value for value in self.jobs.values() if value["Id"] == job["Id"])
            require({field: job.get(field) for field in prior} == prior, "scan_state_changed_after_restart")
        items = self._discover()
        require(items == self.before["items"], "catalog_identity_changed_after_restart")
        catalog = self._snapshot(items)
        require(catalog == self.before, "catalog_or_userdata_changed_after_restart")
        result = self._finish(catalog, dashboard)
        self.phase = "complete"
        return result
