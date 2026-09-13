#!/usr/bin/env python3
"""Complete one owned original-client host wizard through public APIs only."""
import argparse
import base64
import hashlib
import http.client
import io
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import sys
import time
import types
from urllib.parse import urlencode

R = Path("/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14")
OUTPUT = R / "client-host-startup-01"
EPOCH = {"path": str(R / "candidate-backup-limits-revision-01/private/runtime-epoch.json"), "sha256": "72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e"}
BINDING = {"path": str(R / "candidate-backup-limits-revision-01/private/seed-runtime-binding.json"), "sha256": "92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f"}
RUNTIME = {"path": str(R / "backup-limits-tool-verification-01/audited-candidate-runtime.py"), "sha256": "1650d6267ab78a07f8c5b77130ad009eeb7212a0bb16251322936792a58dbe66"}
HOSTING = {"path": "/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-reconcile-01/hosting.json", "sha256": "2100142b83941e24503838fdf92942aef862bc785bbcfcbe8f93223dd30c53c0"}
SOURCE_SHA = {"gateway": "b343f522389bbcb6f704ab3b09d2592ed06573b90961f7b9c8f3eb45b7ab0070", "proxy": "388965fc772dff82ff13d2a641ecc0e9383e20b9f2a9bc929f48edcf6f394874"}
BUDGETS = {"maximumSeconds": 300, "cleanupSeconds": 60, "normalRequests": 16, "cleanupRequests": 4}
INPUT_KEYS = {"kind", "version", "output", "runtimeEpoch", "seedBinding", "runtimeHelper", "hosting", "gateway", "proxy", "compiledCatalog", "budgets"}
ADMIN_NAME = "goby-client-host-admin-01"
UNIT_FIELDS = "Id LoadState ActiveState SubState MainPID InvocationID Result ExecMainStatus ControlGroup NRestarts".split()
NETWORK = {"HttpServerPortNumber": 28497, "PublicPort": 28497,
    "ServerName": "Goby Core AV Original Client Host 01", "LocalNetworkAddresses": ["127.0.0.1"],
    **dict.fromkeys("EnableHttps EnableUPnP EnableRemoteAccess EnableAutoUpdate EnableAutomaticRestart AutoRunWebApp".split(), False)}
ROUTES = {"public-before": ("GET", "/emby/System/Info/Public", 200), "startup-user-read": ("GET", "/emby/Startup/User", 200),
    "startup-user": ("POST", "/emby/Startup/User", 200), "startup-remote": ("POST", "/emby/Startup/RemoteAccess", 204),
    "startup-complete": ("POST", "/emby/Startup/Complete", 204), "login": ("POST", "/emby/Users/AuthenticateByName", 200),
    "users": ("GET", "/emby/Users", 200), "configuration": ("GET", "/emby/System/Configuration", 200),
    "libraries": ("GET", "/emby/Library/VirtualFolders/Query", 200), "web-index": ("GET", "/web/index.html", 200),
    "public-after": ("GET", "/emby/System/Info/Public", 200), "logout": ("POST", "/emby/Sessions/Logout", 204), "logout-check": ("GET", "/emby/Sessions", 401)}


class StartupError(ValueError):
    pass


def need(value, code):
    if not value:
        raise StartupError(code)


def validate_input(value):
    need(set(value) == INPUT_KEYS and value["kind"] == "audited-original-client-host-startup-input" and type(value["version"]) is int and value["version"] == 1 and
         value["output"] == str(OUTPUT) and value["budgets"] == BUDGETS, "startup_input_schema")
    for name, expected in (("runtimeEpoch", EPOCH), ("seedBinding", BINDING), ("runtimeHelper", RUNTIME), ("hosting", HOSTING)):
        need(value[name] == expected, "startup_authority_changed")
    for name in ("gateway", "proxy", "compiledCatalog"):
        pin = value[name]
        need(isinstance(pin, dict) and set(pin) == {"path", "sha256"} and isinstance(pin["path"], str) and Path(pin["path"]).is_absolute() and
             ".." not in Path(pin["path"]).parts and re.fullmatch(r"[0-9a-f]{64}", pin["sha256"]), "startup_descriptor_invalid")
        if name in SOURCE_SHA:
            need(Path(pin["path"]).is_relative_to(R) and Path(pin["path"]).name == "client-acceptance-" + name + ".py" and pin["sha256"] == SOURCE_SHA[name], "startup_transport_source_changed")
    return value


def public_identity(value, hosting):
    expected = {"id": hosting["serverId"], "version": hosting["version"], "serverName": hosting["serverName"]}
    need(isinstance(value, dict) and {"id": value.get("Id"), "version": value.get("Version"), "serverName": value.get("ServerName")} == expected, "hosting_public_identity_changed")
    return expected


def validate_login(value, hosting):
    need(isinstance(value, dict), "owned_admin_login_mismatch")
    user = value.get("User", {}) if isinstance(value, dict) else {}
    need(value.get("ServerId") == hosting["serverId"] and user.get("Name") == ADMIN_NAME and re.fullmatch(r"[0-9a-f]{32}", user.get("Id", "")) and
         user.get("Policy", {}).get("IsAdministrator") is True, "owned_admin_login_mismatch")
    return user


def validate_configuration(value):
    need(isinstance(value, dict) and value.get("IsStartupWizardCompleted") is True and
         all(type(value.get(key)) is type(expected) and value[key] == expected for key, expected in NETWORK.items()), "hosting_network_configuration_changed")
    return {key: value[key] for key in NETWORK}


def headers_only(sock, remaining):
    """Stop exactly at the header terminator without consuming HTML entity bytes."""
    raw = bytearray()
    while not raw.endswith(b"\r\n\r\n"):
        need(len(raw) < 65536, "web_headers_oversized")
        sock.settimeout(min(10, remaining()))
        byte = sock.recv(1)
        need(byte, "web_headers_truncated")
        raw.extend(byte)
    first, tail = bytes(raw).split(b"\r\n", 1)
    match = re.fullmatch(rb"HTTP/1\.[01] ([0-9]{3}) [^\r\n]*", first)
    need(match, "web_status_line_invalid")
    return int(match[1]), list(http.client.parse_headers(io.BytesIO(tail)).items()), bytes(raw)


def compare_samples(before, after):
    need(before["source"]["tables"] == after["source"]["tables"] and before["source"]["sequences"] == after["source"]["sequences"] and
         len(before["source"]["tables"]) == 35, "goby_logical_state_changed")
    for key in ("candidate", "postgres", "lease", "hosting"):
        need(before[key] == after[key], "startup_changed_" + key)
    return {"ownedTablesExact": 35, "sequencesExact": True, "candidateContinuous": True, "postgresContinuous": True, "leaseExact": True, "hostingContinuous": True}


class Startup:
    def __init__(self, runtime, value, input_pin, source_pin):
        self.r, self.value, self.input_pin, self.source_pin = runtime, validate_input(value), input_pin, source_pin
        self.output, self.private = OUTPUT, OUTPUT / "private"
        self.started, self.created, self.stage = time.monotonic(), False, "preflight"
        self.requests, self.states, self.cleanup, self.cleanup_failures = {"normal": 0, "cleanup": 0}, [], [], []
        self.token, self.endpoint, self.last_response = None, None, None
        self.public_attested = False
        self.result, self.failure, self.before = {}, None, None

    def remaining(self, cleanup=False):
        left = 300 - (0 if cleanup else 60) - (time.monotonic() - self.started)
        need(left > 0, "startup_deadline")
        return left

    def save(self, name, value):
        return self.s.write_json_once(self.private / name, value)

    def pin_host(self):
        host = self.hosting
        self.g.verify_listener(host["process"]["pid"], host["listener"])
        process = self.g.metadata(host["process"]["pid"])
        result = subprocess.run(["/usr/bin/systemctl", "show", host["unit"], "--property=" + ",".join(UNIT_FIELDS)], capture_output=True, check=True, timeout=10)
        need(len(result.stdout) <= 65536, "hosting_unit_metadata_bound")
        unit = dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)
        need(process == host["process"] and unit == {key: host["unitProperties"][key] for key in UNIT_FIELDS}, "hosting_runtime_changed")
        return {"process": process, "listener": host["listener"], "unit": unit, "packageSha256": host["packageSha256"], "executableSha256": host["executableSha256"]}

    def open(self):
        need(not os.path.lexists(self.output), "startup_output_collision")
        self.epoch = self.r.validate_epoch(json.loads(self.r.read_bootstrap(EPOCH)))
        self.modules = {key: self.r.load_helper(key, pin) for key, pin in self.epoch["helpers"].items()}
        self.s = self.modules["seed"]
        need(self.s.descriptor(self.input_pin) == self.value and self.epoch["runtimeHelper"] == RUNTIME, "startup_input_changed")
        self.r.validate_seed_runtime_binding(self.s.descriptor(BINDING), EPOCH, self.epoch, self.s.descriptor(self.r.SEED))
        self.g = self.r.load_helper("startup_gateway", self.value["gateway"])
        self.base = self.g.load_proxy(self.value["proxy"])
        self.hosting = self.s.descriptor(HOSTING)
        need(self.hosting["kind"] == "core-av-original-client-hosting" and self.hosting["version"] == "4.9.5.0" and self.hosting["process"]["pid"] == 366598 and
             self.hosting["invocationId"] == "c883bdf04e2f4d7ea6526fcd858fc342" and self.hosting["listener"]["port"] == 28497, "hosting_authority_invalid")
        self.pin_host()
        self.s.read_checked(self.hosting["process"]["exe"], self.hosting["executableSha256"])
        self.catalog = self.s.descriptor(self.value["compiledCatalog"])
        self.modules["admission"].validate_catalog(self.catalog, self.s.descriptor(self.epoch["currentSource"]["sourceManifest"]), self.value["compiledCatalog"])
        for pin in [self.epoch["candidate"]["runtime"], self.epoch["candidate"]["binary"], *self.epoch["candidate"]["units"].values()]:
            self.s.read_checked(pin["path"], pin["sha256"])
        self.s.write_json_once(self.output.with_name(self.output.name + "-intent.json"), {"input": self.input_pin, "helper": self.source_pin, "operation": "create-fresh-startup-evidence"})
        os.mkdir(self.output, 0o700)
        self.created = True
        self.s.sync_dir(self.output.parent)
        os.mkdir(self.private, 0o700)
        self.s.sync_dir(self.output)
        self.reader = self.r.EpochReader(self.epoch, self.modules, self.private)
        self.reader.pin()
        self.endpoint = self.g.BoundEndpoint({key: self.hosting[key] for key in ("process", "listener", "executableSha256")}, self.base)

    def sample(self, name):
        self.remaining(cleanup=name == "after")
        candidate, postgres, lease, host = self.reader.pin(), self.g.metadata(self.epoch["postgresProcess"]["pid"]), self.reader.deployment_lease(), self.pin_host()
        need(postgres == self.epoch["postgresProcess"] and lease == self.epoch["lease"], "startup_candidate_epoch_changed")
        source = self.reader.sql_json(self.epoch["candidate"]["database"], self.modules["admission"].snapshot_sql(self.catalog))
        need(set(source) == {"capturedAt", "tables", "sequences"} and set(source["tables"]) == self.r.TABLES, "startup_source_inventory_invalid")
        need(self.reader.pin() == candidate and self.reader.deployment_lease() == lease and self.g.metadata(postgres["pid"]) == postgres and self.pin_host() == host, "startup_capture_changed")
        self.s.read_checked(self.hosting["process"]["exe"], self.hosting["executableSha256"])
        value = {"source": source, "candidate": candidate, "postgres": postgres, "lease": lease, "hosting": host}
        return value, self.save("source-" + name + ".json", source)

    def adopt_token(self, raw):
        try:
            value = self.s.parse(raw)
            token = value.get("AccessToken") if isinstance(value, dict) else None
            if isinstance(token, str) and re.fullmatch(r"[A-Za-z0-9_-]{16,4096}", token):
                need(self.token in (None, token), "startup_multiple_tokens")
                self.token = token
        except (ValueError, TypeError):
            pass

    def request(self, label, body=None, token=None):
        need(label in ROUTES and not any(row["label"] == label for row in self.states), "startup_action_repeated_or_unknown")
        cleanup = label in ("logout", "logout-check")
        slot = "cleanup" if cleanup else "normal"
        need(self.requests[slot] < BUDGETS[slot + "Requests"], "startup_request_budget")
        self.remaining(cleanup)
        self.pin_host()
        method, route, expected = ROUTES[label]
        need(method == "GET" or cleanup or self.public_attested, "startup_public_identity_required")
        headers = {"Connection": "close", "Accept-Encoding": "identity", "Accept": "text/html" if label == "web-index" else "application/json",
            "Authorization": 'Emby Client="GobyOwnedHostStartup", Device="Linux", DeviceId="goby-client-host-startup-01", Version="1.0"'}
        if token is not None:
            need(token == self.token and label in ("users", "configuration", "libraries", "logout", "logout-check"), "startup_token_scope")
            headers["X-Emby-Token"] = token
        raw_body = None
        if body is not None:
            form = label in ("startup-user", "startup-remote")
            raw_body = urlencode(body).encode() if form else self.s.encoded(body)
            headers["Content-Type"] = "application/x-www-form-urlencoded" if form else "application/json"
        wanted = {"startup-user": {"Name": ADMIN_NAME, "Password": self.password}, "startup-remote": {"EnableAutomaticPortMapping": "false"}, "login": {"Username": ADMIN_NAME, "Pw": self.password}}
        need(body == wanted.get(label), "startup_body_scope")
        base = "%02d-%s" % (sum(self.requests.values()) + 1, label)
        self.save(base + "-intent.json", {"label": label, "method": method, "route": route, "headers": headers, "bodyBase64": base64.b64encode(raw_body or b"").decode(), "cleanup": cleanup, "maximumResponseBytes": 2 << 20})
        state = {"label": label, "outcome": "unknown", "response": None}
        self.states.append(state)
        self.requests[slot] += 1
        connection = http.client.HTTPConnection("127.0.0.1", 28497)
        connection.auto_open = 0
        response, chunks, result = None, [], None
        self.last_response = None
        try:
            connection.sock = self.endpoint.connect()
            connection.sock.settimeout(min(10, self.remaining(cleanup)))
            connection.request(method, route, body=raw_body, headers=headers)
            if label == "web-index":
                status, response_headers, raw = headers_only(connection.sock, lambda: self.remaining(cleanup))
                raw_pin = self.s.write_once(self.private / (base + "-headers.bin"), raw)
                result = {"status": status, "headers": response_headers, "headersComplete": True, "bodyRead": False, "bodyComplete": None, "rawHeaders": raw_pin}
            else:
                response = connection.getresponse()
                result = {"status": response.status, "headers": response.getheaders(), "complete": False, "bodyRead": True}
                self.last_response = {"label": label, **result}
                received = 0
                while received <= 2 << 20:
                    if response.fp is not None:
                        response.fp.raw._sock.settimeout(min(10, self.remaining(cleanup)))
                    chunk = response.read1(min(65536, (2 << 20) + 1 - received))
                    if not chunk:
                        break
                    chunks.append(chunk)
                    received += len(chunk)
                raw = b"".join(chunks)
                if label == "login":
                    self.adopt_token(raw)
                result.update(complete=self.s.response_complete(response, method, raw, 2 << 20), body=self.s.write_once(self.private / (base + "-body.bin"), raw), bytes=len(raw))
            self.last_response = {"label": label, **result}
            state["response"] = self.save(base + "-response.json", result)
            state["outcome"] = "headers_received_body_not_consumed" if label == "web-index" else "response_received" if result["complete"] else "incomplete_response"
            need(label == "web-index" or result["complete"], "startup_response_incomplete")
            self.pin_host()
            need(result["status"] == expected, "startup_response_status")
            return (None if label == "web-index" or cleanup else self.s.parse(raw) if raw else None), result, state["response"]
        except Exception:
            raw = b"".join(chunks)
            if label == "login":
                self.adopt_token(raw)
            if state["response"] is None:
                try:
                    partial = self.s.write_once(self.private / (base + "-partial.bin"), raw)
                    state["partialResponse"] = self.save(base + "-partial-response.json", {"observed": result, "body": partial, "complete": False})
                except Exception as error:
                    state["receiptErrorType"] = type(error).__name__
            raise
        finally:
            if response is not None:
                response.close()
            connection.close()

    def cleanup_token(self):
        if self.token is None:
            return
        record = {"tokenSha256": hashlib.sha256(self.token.encode()).hexdigest(), "logoutStatus": None, "sameTokenStatus": None}
        for label, field in (("logout", "logoutStatus"), ("logout-check", "sameTokenStatus")):
            try:
                unused, response, pin = self.request(label, token=self.token)
                record[field] = response["status"]
                record[field + "Response"] = pin
            except Exception as error:
                self.cleanup_failures.append({"stage": label, "code": str(error) if isinstance(error, StartupError) else "startup_cleanup_failed", "errorType": type(error).__name__})
        self.cleanup.append(record)
        self.save("cleanup.json", {"cleanup": self.cleanup, "failures": self.cleanup_failures})

    def workflow(self):
        public, unused, unused_pin = self.request("public-before")
        identity = public_identity(public, self.hosting)
        self.public_attested = True
        startup, unused, unused_pin = self.request("startup-user-read")
        need(isinstance(startup, dict) and isinstance(startup.get("Name"), str), "startup_user_unavailable")
        created, unused, unused_pin = self.request("startup-user", {"Name": ADMIN_NAME, "Password": self.password})
        need(created == {}, "startup_user_not_acknowledged")
        self.request("startup-remote", {"EnableAutomaticPortMapping": "false"})
        self.request("startup-complete")
        login, unused, unused_pin = self.request("login", {"Username": ADMIN_NAME, "Pw": self.password})
        need(self.token is not None, "startup_token_unavailable")
        admin = validate_login(login, self.hosting)
        self.save("token.json", {"token": self.token, "adminId": admin["Id"], "serverId": identity["id"]})
        users, unused, unused_pin = self.request("users", token=self.token)
        need(isinstance(users, list) and len(users) == 1 and users[0].get("Id") == admin["Id"] and users[0].get("Name") == ADMIN_NAME and users[0].get("Policy", {}).get("IsAdministrator") is True, "hosting_users_not_one_owned_admin")
        config, unused, unused_pin = self.request("configuration", token=self.token)
        configuration = validate_configuration(config)
        libraries, unused, unused_pin = self.request("libraries", token=self.token)
        need(isinstance(libraries, dict) and libraries.get("Items") == [], "hosting_library_not_empty")
        unused, web, web_pin = self.request("web-index")
        need(not any(name.lower() == "location" for name, value in web["headers"]), "hosting_index_redirect")
        final_public, unused, unused_pin = self.request("public-after")
        need(public_identity(final_public, self.hosting) == identity, "hosting_identity_after_startup_changed")
        return {"publicIdentity": identity, "wizardCompleted": True, "networkConfiguration": configuration,
            "users": {"count": 1, "adminId": admin["Id"], "adminName": ADMIN_NAME}, "libraries": {"count": 0},
            "webIndex": {"status": web["status"], "location": None, "bodyRead": False, "response": web_pin}}

    def run(self):
        self.open()
        try:
            self.stage = "source_before"
            self.before, before_pin = self.sample("before")
            self.result.update(sourceBefore=before_pin, candidateBefore=self.before["candidate"], postgresBefore=self.before["postgres"], leaseBefore=self.before["lease"], hostingBefore=self.before["hosting"])
            self.password = secrets.token_urlsafe(32)
            self.result["credentials"] = self.save("credentials.json", {"username": ADMIN_NAME, "password": self.password, "serverId": self.hosting["serverId"]})
            self.stage = "public_startup"
            self.result.update(self.workflow())
        except Exception as error:
            self.failure = {"stage": self.stage, "code": str(error) if isinstance(error, StartupError) else "startup_operation_failed", "errorType": type(error).__name__}
        finally:
            try:
                self.cleanup_token()
            except Exception as error:
                self.cleanup_failures.append({"stage": "cleanup_receipt", "errorType": type(error).__name__})
            if self.before is not None:
                try:
                    after, after_pin = self.sample("after")
                    self.result.update(sourceAfter=after_pin, candidateAfter=after["candidate"], postgresAfter=after["postgres"], leaseAfter=after["lease"], hostingAfter=after["hosting"])
                    self.result["preservation"] = compare_samples(self.before, after)
                except Exception as error:
                    self.failure = self.failure or {"stage": "source_after", "code": str(error) if isinstance(error, StartupError) else "startup_preservation_failed", "errorType": type(error).__name__}
            if self.endpoint is not None:
                self.endpoint.close()
        successful = self.failure is None and not self.cleanup_failures and len(self.cleanup) == 1 and self.cleanup[0]["logoutStatus"] == 204 and self.cleanup[0]["sameTokenStatus"] == 401
        return {"kind": "audited-original-client-host-startup", "version": 1, "status": "ready_for_core_client" if successful else "startup_incomplete_resources_retained",
            "input": self.input_pin, "helper": self.source_pin, "hosting": HOSTING, "runtimeEpoch": EPOCH, "seedBinding": BINDING, **self.result,
            "cleanup": self.cleanup, "cleanupFailures": self.cleanup_failures, "failure": self.failure, "requests": self.requests, "requestStates": self.states,
            "elapsedMilliseconds": int((time.monotonic() - self.started) * 1000), "budgets": BUDGETS, "automaticWriteRetry": False,
            "servicesStartedOrStopped": 0, "vendorImplementationInspected": False, "hostingExecutableHashChecked": True, "vendorDatabaseAccessed": False, "gobyBusinessHttpRequests": 0}


def main():
    need(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode, "remote_isolated_root_required")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ("input", "input-sha256", "source-sha256", "runtime-helper", "runtime-helper-sha256"):
        parser.add_argument("--" + key, required=True)
    args = parser.parse_args()
    need({"path": args.runtime_helper, "sha256": args.runtime_helper_sha256} == RUNTIME, "runtime_helper_not_frozen")
    raw = Path(args.runtime_helper).read_bytes()
    need(hashlib.sha256(raw).hexdigest() == RUNTIME["sha256"], "runtime_helper_changed")
    runtime = types.ModuleType("startup_epoch")
    runtime.__file__ = args.runtime_helper
    exec(compile(raw, args.runtime_helper, "exec"), runtime.__dict__)
    runtime.read_bootstrap(RUNTIME)
    input_pin, source_pin = {"path": args.input, "sha256": args.input_sha256}, {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    runtime.read_bootstrap(source_pin)
    job = Startup(runtime, json.loads(runtime.read_bootstrap(input_pin)), input_pin, source_pin)
    def expired(unused_signal, unused_frame):
        raise StartupError("startup_absolute_deadline")
    for number in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
        signal.signal(number, expired)
    signal.setitimer(signal.ITIMER_REAL, 300)
    try:
        report = job.run()
        pin = job.save("report.json", report)
        print(json.dumps({"status": report["status"], "report": pin, "requests": report["requests"], "failure": report["failure"], "cleanupFailures": report["cleanupFailures"]}))
        return 0 if report["status"] == "ready_for_core_client" else 2
    except Exception as error:
        failure = {"status": "startup_incomplete_resources_retained", "stage": job.stage, "errorType": type(error).__name__, "code": str(error) if isinstance(error, StartupError) else "startup_recording_failed",
            "requests": job.requests, "requestStates": job.states, "knownToken": job.token is not None, "cleanup": job.cleanup, "cleanupFailures": job.cleanup_failures, "automaticWriteRetry": False}
        try:
            if job.created:
                failure["receipt"] = job.save("failure.json", failure)
        except Exception as receipt_error:
            failure["receiptErrorType"] = type(receipt_error).__name__
        print(json.dumps(failure))
        return 2
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


if __name__ == "__main__":
    raise SystemExit(main())
