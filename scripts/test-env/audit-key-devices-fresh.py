#!/usr/bin/env python3
"""Recover the completed fresh key-device study from immutable offline evidence.

Run only through authorized root SSH while the operator retains the fresh
instance and its files. No HTTP, service-manager request, process launch, or
original evidence write is permitted. The original finalization failure and
all existing exports remain unchanged. This produces one new derived audit.
"""

from __future__ import annotations

import contextlib
import datetime as dt
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
from unittest.mock import patch
from urllib.parse import parse_qs, quote, urlsplit

sys.dont_write_bytecode = True
EVIDENCE = Path("/opt/goby-test/exec-scratch/emby-key-devices-fresh-m5e-20260910-02")
CAPTURE = EVIDENCE / "runtime/key-devices-capture-2"
PRIVATE, RAW, EXPORT = CAPTURE / "private", CAPTURE / "private/raw", CAPTURE / "export"
PREFIX = "key-devices-fresh-m5e-2-attempt-2-"
OUTPUT = EVIDENCE / "runtime/key-devices-capture-2-offline-audit"
OUTPUT_MARKER = "goby-key-devices-fresh-m5e-2-offline-recovery-owned-v1"
OUTPUT_NAME = PREFIX + "recovery-audit.json"
UNIT = "goby-emby-key-devices-fresh-m5e-20260910-02.service"
SERVER_ID = "00475e7660c6499e9065f988188e7a57"
HTTP_COUNT, SETUP_COUNT, ORIGINAL_CORPUS_COUNT = 109, 17, 1538
APP_PREFIX = "Goby Fresh Key Devices M5e 20260910 05 "
DEVICE_PREFIX = "goby-key-devices-fresh-m5e-20260910-05-"
MAX_FILE_BYTES, MAX_CAPTURE_BYTES = 4 * 1024 * 1024, 64 * 1024 * 1024


class AuditFailure(RuntimeError):
    """An authored diagnostic containing no credential or response value."""


def require(condition: object, reason: str) -> None:
    if not condition:
        raise AuditFailure(reason)


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(65536), b""):
            result.update(block)
    return result.hexdigest()


def regular(path: Path, *, private: bool = True, hard_links: bool = False) -> None:
    info = path.lstat()
    require(path.resolve(strict=True) == path and stat.S_ISREG(info.st_mode) and
            (hard_links or info.st_nlink == 1), "Evidence path is not canonical and regular")
    if private:
        require(info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600, "Private evidence ownership or mode differs")


def load_json(path: Path) -> object:
    regular(path)
    require(path.stat().st_size <= MAX_FILE_BYTES, "Private JSON evidence exceeds its bound")
    return json.loads(path.read_text())


def snapshot_tree(root: Path) -> dict:
    info = root.lstat()
    require(root.resolve(strict=True) == root and stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
            stat.S_IMODE(info.st_mode) == 0o700, "Evidence root ownership or mode differs")
    files, directories, size = {}, [], 0
    for path in sorted(root.rglob("*")):
        require(not path.is_symlink(), "Evidence tree contains a symbolic link")
        entry = path.lstat()
        if stat.S_ISDIR(entry.st_mode):
            require(entry.st_uid == 0 and stat.S_IMODE(entry.st_mode) == 0o700, "Private evidence directory mode differs")
            directories.append(str(path))
        else:
            regular(path)
            size += entry.st_size
            files[str(path)] = digest(path)
        require(len(files) + len(directories) < 2048 and size <= MAX_CAPTURE_BYTES, "Capture evidence exceeds its finite bound")
    return {"files": files, "directories": directories, "totalBytes": size}


def verify_baseline(baseline: dict) -> dict:
    expected_groups = {"records", "media", "privateFiles", "authorityFiles", "previousFailureFiles", "previousCaptureFailureFiles"}
    require(isinstance(baseline, dict) and set(baseline) == expected_groups and
            all(isinstance(value, dict) for value in baseline.values()), "Capture preservation baseline shape differs")
    require(len(baseline["records"]) == (ORIGINAL_CORPUS_COUNT + SETUP_COUNT) * 2 and len(baseline["media"]) == 240,
            "Capture preservation baseline membership differs")
    counts = {}
    for group, mapping in baseline.items():
        require(len(mapping) <= 8192, "A preservation group exceeds its finite bound")
        total = 0
        for name, expected_hash in mapping.items():
            path = Path(name)
            regular(path, private=False, hard_links=group == "media")
            total += path.stat().st_size
            require(digest(path) == expected_hash, "A preserved evidence or source file changed")
        require(total <= 192 * 1024 * 1024, "A preservation group exceeds its finite byte bound")
        counts[group] = {"files": len(mapping), "bytes": total, "hashesUnchanged": True}
    # Reconstruct original and setup private membership, rather than proving
    # only that the previously named files still exist.
    private_roots = {Path(name).parent.parent for name in baseline["records"] if Path(name).parent.name == "raw"}
    actual_private = set()
    for folder in private_roots:
        for path in folder.rglob("*"):
            require(not path.is_symlink(), "A protected private tree contains a symbolic link")
            if path.is_file():
                actual_private.add(str(path))
            require(len(actual_private) <= 8192, "Protected private membership exceeds its bound")
    require(actual_private == set(baseline["privateFiles"]), "Protected private-file membership changed")
    actual_records = set()
    for folder in private_roots:
        actual_records.update(str(path) for path in (folder / "raw").glob("*.json"))
        actual_records.update(str(path) for path in (folder.parent / "export").glob("*.json"))
    require(actual_records == set(baseline["records"]), "Protected raw/export record membership changed")
    failed_root = Path("/opt/goby-test/exec-scratch/emby-key-devices-fresh-m5e-20260910-01")
    require({str(path) for path in failed_root.rglob("*") if path.is_file()} == set(baseline["previousFailureFiles"]),
            "Prior initialization-failure evidence membership changed")
    first_capture = EVIDENCE / "runtime/key-devices-capture"
    marker = first_capture / ".goby-managed"
    require(set(first_capture.rglob("*")) == {marker, first_capture / "private", first_capture / "private/raw", first_capture / "export"},
            "Prior constructor-failure capture no longer contains only its marker and empty directories")
    runner = Path("/opt/goby-test/exec-scratch/devices-m5e-runner/fresh-capture-failure-1-private")
    require({str(path) for path in runner.rglob("*")} | {str(marker)} == set(baseline["previousCaptureFailureFiles"]),
            "Prior constructor source or console membership changed")
    return counts


def process_identities(manifest: dict) -> list[dict]:
    expected = [*manifest["oldServices"].values(), manifest]
    result = []
    for row in expected:
        folder = Path("/proc") / str(row["pid"])
        actual = {"pid": row["pid"], "uid": folder.stat().st_uid,
                  "startTicks": (folder / "stat").read_text().rsplit(")", 1)[1].split()[19],
                  "networkNamespace": os.readlink(folder / "ns/net"), "exe": os.readlink(folder / "exe"),
                  "cmdline": [part.decode() for part in (folder / "cmdline").read_bytes().split(b"\0") if part],
                  "cgroup": (folder / "cgroup").read_text()}
        require(all(actual[name] == row[name] for name in actual), "An attested process identity changed")
        result.append({"unit": row["unit"], "pid": actual["pid"], "uid": actual["uid"],
                       "startTicks": actual["startTicks"], "networkNamespace": actual["networkNamespace"], "unchanged": True})
    return result


def load_legacy_sanitizer(records: dict, bootstrap: list[dict]):
    path = Path(__file__).with_name("reference-api-keys.py")
    regular(path, private=False)
    spec = importlib.util.spec_from_file_location("offline_legacy_key_sanitizer", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.ROOT = CAPTURE
    sanitizer = module.Recorder.__new__(module.Recorder)
    sanitizer.secrets = set()
    for item in [*records.values(), *bootstrap]:
        sanitizer.collect_secrets(item)
    for account in ("admin", "viewer"):
        path = EVIDENCE / "private" / (account + "-credentials.env")
        regular(path)
        require(path.stat().st_size <= 8192, "Bootstrap credential file exceeds its bound")
        for line in path.read_text().splitlines():
            field, value = line.split("=", 1)
            if field in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value:
                sanitizer.secrets.add(value)
    return module, sanitizer


def legacy_label_diagnostic(module) -> dict:
    replay = module.Recorder.__new__(module.Recorder)
    replay.secrets = set()
    labels = ("alpha", "sibling")
    sample = {"keyProtectedRouteObservations": [{"key": label} for label in labels],
              "keyInvalidityProofs": {"owned-credential:" + label: {"status": 401} for label in labels}}
    replay.collect_secrets(sample)
    cleaned = replay.sanitize(sample)
    require(set(labels) <= replay.secrets, "Legacy logical-label collection did not reproduce the finalization failure")
    matches = []
    for index, field in enumerate(cleaned["keyInvalidityProofs"]):
        for label in labels:
            if label in field:
                matches.append({"sourceField": "keyProtectedRouteObservations[].key", "stringClass": "logical-credential-label",
                                "stringLength": len(label), "matchKind": "dictionary-key",
                                "location": "keyInvalidityProofs/<dictionary-key:" + str(index) + ">"})
    require(len(matches) == 2 and all(item["key"] == "[REDACTED_SECRET]" for item in cleaned["keyProtectedRouteObservations"]),
            "Legacy value-only sanitization did not reproduce both dictionary-key matches")
    return {"syntheticReproductionPassed": True, "matches": matches,
            "explanation": "Logical credential labels under the generic key field become secret values; legacy sanitization leaves dictionary keys unchanged",
            "originalHTTPExportsRequireModification": False}


def sanitize_new_audit(value: object, sanitizer, field: str = "") -> object:
    if isinstance(value, dict):
        result = {}
        for name, child in value.items():
            clean_name = sanitizer.sanitize(name)
            require(clean_name not in result, "New audit sanitization would collide dictionary keys")
            result[clean_name] = sanitize_new_audit(child, sanitizer, name)
        return result
    if isinstance(value, list):
        return [sanitize_new_audit(child, sanitizer, field) for child in value]
    return sanitizer.sanitize(value, field)


def credential(record: dict) -> str:
    headers = record["request"]["headers"]
    pairs = headers.items() if isinstance(headers, dict) else headers
    values = [value for name, value in pairs if name.lower() == "x-emby-token"]
    require(len(values) <= 1, "A captured request has ambiguous credential headers")
    return values[0] if values else ""


def timestamp(record: dict) -> dt.datetime:
    value = dt.datetime.fromisoformat(record["reference"]["capturedAt"])
    require(value.tzinfo is not None, "A captured request has no UTC offset")
    return value


def verify_bootstrap_credentials(bootstrap: list[dict], study: dict) -> dict:
    require(len(bootstrap) == SETUP_COUNT, "Bootstrap record membership differs")
    incomplete = [index for index, row in enumerate(bootstrap) if row.get("observation", {}).get("completeHTTP") is not True]
    readiness = []
    if incomplete:
        require(incomplete == [0] and len(bootstrap) >= 2, "An authentication, proof, or noninitial bootstrap record is incomplete")
        failed, succeeded = bootstrap[:2]
        expected_request = {"method": "GET", "path": "/emby/System/Info/Public",
                            "headers": [["Accept", "application/json"]], "body": None}
        require(failed.get("request") == expected_request and failed.get("response") ==
                {"status": None, "headers": [], "bodyType": "incomplete", "body": None} and failed.get("observation") ==
                {"completeHTTP": False, "captureIncomplete": True, "failureType": "ConnectionRefusedError", "wireBytes": 0, "freshFixtureOnly": True},
                "The initial readiness record is not the observed anonymous zero-byte connection refusal")
        require(succeeded.get("request") == expected_request and succeeded.get("response", {}).get("status") == 200 and
                isinstance(succeeded["response"].get("body"), dict) and succeeded["response"]["body"].get("Id") == SERVER_ID and
                succeeded["response"]["body"].get("Version") == "4.9.5.0" and timestamp(failed) < timestamp(succeeded),
                "The initial connection refusal lacks the immediate successful fresh public-info response")
        readiness.append({"recordOrdinal": 1, "completeHTTP": False, "failureType": "ConnectionRefusedError", "wireBytes": 0,
                          "scope": "Anonymous initial public readiness request", "followingPublicInfoStatus": 200})
    complete = [row for index, row in enumerate(bootstrap) if index not in incomplete]
    require(all(row.get("observation", {}).get("completeHTTP") is True and row["observation"].get("captureIncomplete") is False and
                row["observation"].get("failureType") is None and isinstance(row.get("response", {}).get("status"), int) and
                not isinstance(row["response"]["status"], bool) and 100 <= row["response"]["status"] <= 599 for row in complete),
            "A remaining bootstrap HTTP record is incomplete or has an inconsistent response")
    tokens = {}
    for record in bootstrap:
        if record["request"]["method"] == "POST" and urlsplit(record["request"]["path"]).path == "/emby/Users/AuthenticateByName":
            body = record["response"]["body"]
            require(record["response"]["status"] == 200 and isinstance(body.get("AccessToken"), str) and body["AccessToken"],
                    "Bootstrap login response is not acknowledged")
            role = body["User"]["Policy"]["IsAdministrator"]
            require(isinstance(role, bool), "Bootstrap login role is ambiguous")
            tokens[body["AccessToken"]] = "admin" if role else "viewer"
    require(len(tokens) == 2 and set(tokens.values()) == {"admin", "viewer"}, "Bootstrap credentials are not two independent owned logins")
    proofs = []
    for token, label in tokens.items():
        uses = [record for record in bootstrap if credential(record) == token]
        require(uses and uses[-1]["request"]["method"] == "GET" and urlsplit(uses[-1]["request"]["path"]).path == "/emby/Sessions" and
                uses[-1]["response"]["status"] == 401 and all(credential(record) != token for record in study.values()),
                "A bootstrap credential lacks its final 401 proof or was reused by the study")
        proofs.append({"credentialLabel": "bootstrap-" + label, "finalStatus": 401, "studyHTTPUses": 0})
    return {"credentials": proofs, "independentRawTokenMappingVerified": True, "setupRecords": len(bootstrap),
            "completeHTTPRecords": len(complete), "nonHTTPReadinessObservations": readiness}


def analyze_http(records: dict, private: dict, manifest: dict) -> dict:
    require(len(records) == HTTP_COUNT, "Expected exactly the completed 109 HTTP captures")
    expected_transport = {"unit": UNIT, "port": 18098, "pid": manifest["pid"],
                          "startTicks": manifest["startTicks"], "networkNamespace": manifest["networkNamespace"]}
    for record in records.values():
        require(record.get("transportAuthority") == expected_transport and record.get("observation", {}).get("completeHTTP") is True and
                record["observation"].get("captureIncomplete") is False and isinstance(record.get("request"), dict) and
                isinstance(record.get("response"), dict) and isinstance(record["response"].get("status"), int),
                "A recorded HTTP exchange is incomplete or belongs to another authority")
        require(record.get("reference", {}).get("product") == "Emby Server" and record["reference"].get("version") == "4.9.5.0",
                "Captured reference product differs")
    ordered = sorted(records, key=lambda name: timestamp(records[name]))
    require(len({timestamp(record) for record in records.values()}) == HTTP_COUNT, "Capture chronology is ambiguous")
    order = {name: index for index, name in enumerate(ordered)}

    def expect(label: str, method: str, route: str, status: int) -> dict:
        require(label in records, "A required capture is missing: " + label)
        record = records[label]
        require(record["request"]["method"] == method and urlsplit(record["request"]["path"]).path == route and
                record["response"]["status"] == status, "Captured contract differs: " + label)
        return record["response"]["body"]

    def items(label: str) -> list:
        body = records[label]["response"]["body"]
        require(isinstance(body, dict) and isinstance(body.get("Items"), list), "Captured item-list shape differs: " + label)
        return body["Items"]

    def owned_token(label: str) -> str:
        body = expect(label + "-login", "POST", "/emby/Users/AuthenticateByName", 200)
        require(body == private[label + "-login-response"] and isinstance(body.get("AccessToken"), str) and body["AccessToken"],
                "Acknowledged login differs from its private response")
        return body["AccessToken"]

    control, viewer = owned_token("control"), owned_token("viewer")
    public = expect("public-before", "GET", "/emby/System/Info/Public", 200)
    require(public.get("Id") == manifest["serverId"] == SERVER_ID and private["old-keys"] == [],
            "Fresh public identity or empty key baseline differs")
    key_rows = items("keys-created-sibling")
    require(len(key_rows) == 2, "Both created credentials are not present")
    by_label = {}
    for label in ("alpha", "sibling"):
        matches = [row for row in key_rows if row.get("AppName") == APP_PREFIX + label.title()]
        require(len(matches) == 1 and isinstance(matches[0].get("AccessToken"), str) and matches[0]["AccessToken"],
                "Created credential ownership is ambiguous")
        by_label[label] = matches[0]
        create = records["create-key-" + label]
        expect("create-key-" + label, "POST", "/emby/Auth/Keys", 204)
        require(credential(create) == control and parse_qs(urlsplit(create["request"]["path"]).query) == {"App": [matches[0]["AppName"]]},
                "Credential creation did not use its owned administrator and application label")
    token_labels = {control: "control-login", viewer: "viewer-login", **{row["AccessToken"]: label for label, row in by_label.items()}}
    require(len(token_labels) == 4 and all(row.get("DeviceId") == 5 and row.get("ReportedDeviceId") == SERVER_ID and
            row.get("UserId") == 0 for row in key_rows), "The two independent credentials do not share the owned numeric server device")
    # Every unpaginated key list is cross-checked against the real bounded
    # paging total; the known unpaginated zero counter is never emptiness proof.
    key_list_proofs = []
    for label, record in records.items():
        parsed = urlsplit(record["request"]["path"])
        if record["request"]["method"] == "GET" and parsed.path == "/emby/Auth/Keys" and not parsed.query:
            body = expect(label, "GET", "/emby/Auth/Keys", 200)
            page = expect(label + "-bounded-page", "GET", "/emby/Auth/Keys", 200)
            require(parse_qs(urlsplit(records[label + "-bounded-page"]["request"]["path"]).query) == {"StartIndex": ["0"], "Limit": ["8"]},
                    "Key membership page is not the bounded complete query")
            require(isinstance(page.get("TotalRecordCount"), int) and not isinstance(page["TotalRecordCount"], bool) and
                    0 <= page["TotalRecordCount"] <= 8 and isinstance(page.get("Items"), list) and
                    len(page["Items"]) == page["TotalRecordCount"] and isinstance(body.get("Items"), list),
                    "Bounded key membership count differs")
            one, two = [row["AccessToken"] for row in body["Items"]], [row["AccessToken"] for row in page["Items"]]
            require(len(one) == len(set(one)) and len(two) == len(set(two)) and set(one) == set(two), "Complete key membership differs across the two lists")
            key_list_proofs.append({"capture": label, "items": len(one), "pagedTotal": page["TotalRecordCount"]})
    require(items("keys-baseline") == [], "The complete key baseline was not empty")

    journal = private["mutation-results"]
    mutations = [name for name in ordered if records[name]["request"]["method"] != "GET"]
    require(isinstance(journal, list) and len(journal) == len(mutations) == 10 and
            [row["label"] for row in journal] == mutations, "Mutation journal does not cover every captured mutation in order")
    for row in journal:
        record = records[row["label"]]
        require(row["request"] == record["request"] and row.get("responseStatus") == record["response"]["status"] and
                row.get("transportFailure") is None, "A captured mutation lacks its acknowledged journal result")
    allowed_get = {"/emby/System/Info/Public", "/emby/Users", "/emby/Devices", "/emby/Devices/Info", "/emby/Devices/Options",
                   "/emby/Auth/Keys", "/emby/Sessions"}
    expected_mutations = {"control-login", "viewer-login", "create-key-alpha", "create-key-sibling", "key-rename-viewer",
                          "key-delete-viewer", "delete-owned-server-device", "cleanup-delete-key-0", "cleanup-delete-key-1", "cleanup-logout-control"}
    require(set(mutations) == expected_mutations, "An unexpected media, user, or device mutation exists")
    latest = {}
    for label in ordered:
        record, token = records[label], credential(records[label])
        require(not token or token in token_labels, "A captured request used an unowned credential")
        if token:
            latest[token] = label
        if record["request"]["method"] == "GET":
            require(urlsplit(record["request"]["path"]).path in allowed_get, "A captured GET is outside the bounded device study")
    latest_proofs = []
    for token, label in token_labels.items():
        last = latest[token]
        expect(last, "GET", "/emby/Sessions", 401)
        latest_proofs.append({"credentialLabel": label, "capture": last, "status": 401})

    old_devices = private["old-devices"]
    baseline_devices = items("devices-baseline")
    final_devices = items("devices-final-before-control-logout")
    require(set(old_devices) == {"1", "2"} and set(old_devices) == {row["Id"] for row in manifest["bootstrapDevices"]},
            "Protected bootstrap device membership differs")
    control_device = next(row for row in baseline_devices if row.get("ReportedDeviceId") == DEVICE_PREFIX + "control")
    require(control_device["Id"] == "3" and {row["Id"] for row in baseline_devices} == set(old_devices) | {"3"} and
            {row["Id"] for row in final_devices} == set(old_devices) | {"3"}, "Final listed devices do not retain only the owned control history")
    final_by_id = {row["Id"]: row for row in final_devices}
    require(all(final_by_id[device_id] == row for device_id, row in old_devices.items()), "A protected device field or activity date changed")
    for index, device_id in enumerate(sorted(old_devices)):
        before, after = records["old-options-before-" + str(index)], records["old-options-final-" + str(index)]
        expected = private["old-options"][device_id]
        require({"status": before["response"]["status"], "body": before["response"]["body"]} == expected ==
                {"status": after["response"]["status"], "body": after["response"]["body"]}, "A protected device option changed")
    protected_aliases = {str(row[field]) for row in [*old_devices.values(), control_device] for field in
                         ("Id", "ReportedDeviceId", "InternalId") if row.get(field) is not None}
    write_proofs = []
    for label, target in (("key-rename-viewer", "4"), ("key-delete-viewer", "4"), ("delete-owned-server-device", "5")):
        proof_label = label + "-owned-proof"
        info = expect(proof_label, "GET", "/emby/Devices/Info", 200)
        proof = records[label]["request"].get("ownedDeviceWriteProof")
        require(info.get("Id") == target and isinstance(info.get("ReportedDeviceId"), str) and isinstance(info.get("AppName"), str) and
                all(str(info[field]) not in protected_aliases for field in ("Id", "ReportedDeviceId", "InternalId") if info.get(field) is not None),
                "A device write collides with protected identity")
        require(isinstance(proof, dict) and proof.get("label") == proof_label and proof.get("targetId") == target and
                proof.get("matchedOwnedIdentity") is True and proof.get("expected") == proof.get("observed") ==
                {field: info.get(field) for field in ("Id", "ReportedDeviceId", "AppName")} and order[proof_label] < order[label] and
                parse_qs(urlsplit(records[label]["request"]["path"]).query) == {"Id": [target]}, "Positive device write proof is incomplete")
        write_proofs.append({"capture": label, "targetId": target, "positiveInfoCapture": proof_label, "identityMatched": True})
    expect("key-rename-viewer", "POST", "/emby/Devices/Options", 204)
    expect("key-delete-viewer", "DELETE", "/emby/Devices", 204)
    require(credential(records["key-rename-viewer"]) == credential(records["key-delete-viewer"]) == by_label["alpha"]["AccessToken"],
            "Viewer management was not performed with the owned application credential")
    renamed = expect("viewer-after-key-rename-options", "GET", "/emby/Devices/Options", 200)
    require(renamed == records["key-rename-viewer"]["request"]["body"], "Viewer rename was not observable")
    expect("viewer-after-key-delete-protected", "GET", "/emby/Sessions", 401)
    expect("control-after-key-delete-protected", "GET", "/emby/Sessions", 200)
    for label in ("alpha", "sibling"):
        expect("viewer-delete-key-protected-" + label, "GET", "/emby/Sessions", 200)

    server_info = records["delete-owned-server-device-owned-proof"]["response"]["body"]
    require(server_info["Id"] == "5" and server_info["ReportedDeviceId"] == SERVER_ID and
            {row["AccessToken"] for row in items("server-delete-key-ownership")} == {row["AccessToken"] for row in key_rows} and
            all(row.get("DeviceId") == 5 and row.get("ReportedDeviceId") == SERVER_ID for row in items("server-delete-key-ownership")),
            "Shared server deletion lacks both current credential mappings")
    expect("delete-owned-server-device", "DELETE", "/emby/Devices", 204)
    require(credential(records["delete-owned-server-device"]) == control, "Shared server deletion did not use the independent control login")
    for label in ("server-delete-keys-after", "cleanup-keys-discover", "cleanup-keys-final", "server-delete-registration-after-probes-keys"):
        require(items(label) == [], "An application credential remained in a complete post-deletion list")
    scope_proofs = []
    contexts = (("alpha", "owned-key-1", ""), ("alpha", "owned-key-1", "alpha"), ("alpha", "owned-key-1", "beta"),
                ("sibling", "owned-key-2", ""), ("sibling", "owned-key-2", "sibling"))
    for label, owned_label, client in contexts:
        suffix = owned_label + ("-" + client if client else "")
        after, final = "server-delete-key-protected-" + suffix, "cleanup-key-invalid-" + suffix
        expect(after, "GET", "/emby/Sessions", 401)
        expect(final, "GET", "/emby/Sessions", 401)
        require(credential(records[after]) == credential(records[final]) == by_label[label]["AccessToken"] and
                order["delete-owned-server-device"] < order[after] < order[final], "Credential-context invalidity proof uses another token or chronology")
        metadata = records[after]["request"].get("clientMetadata")
        require((metadata is None and not client) or isinstance(metadata, dict) and metadata.get("DeviceId") == DEVICE_PREFIX + "key-" + client,
                "Credential-context invalidity proof has another client identity")
        scope_proofs.append({"credentialLabel": label, "clientLabel": client or "token-only", "afterDeleteCapture": after,
                             "finalCapture": final, "afterDeleteStatus": 401, "finalStatus": 401})
    expect("server-delete-control-protected", "GET", "/emby/Sessions", 200)
    expect("server-delete-sessions-before-key-probes", "GET", "/emby/Sessions", 200)
    require(credential(records["server-delete-control-protected"]) == credential(records["server-delete-sessions-before-key-probes"]) == control,
            "Post-deletion session projections were not read by the independent control")
    expect("cleanup-invalid-viewer", "GET", "/emby/Sessions", 401)
    expect("cleanup-logout-control", "POST", "/emby/Sessions/Logout", 204)
    expect("cleanup-invalid-control", "GET", "/emby/Sessions", 401)
    require(order["delete-owned-server-device"] < order["server-delete-sessions-before-key-probes"] <
            min(order[row["afterDeleteCapture"]] for row in scope_proofs) and
            max(order[row["afterDeleteCapture"]] for row in scope_proofs) < order["server-delete-devices-after-key-probes"] <
            order["server-delete-control-protected"], "Post-deletion projections were not observed before and after credential probes")
    for index in range(2):
        label = "cleanup-delete-key-" + str(index)
        record = records[label]
        require(record["request"]["method"] == "DELETE" and record["response"]["status"] == 204 and credential(record) == control and
                urlsplit(record["request"]["path"]).path in {"/emby/Auth/Keys/" + quote(row["AccessToken"], safe="") for row in key_rows},
                "Credential cleanup did not target an acknowledged owned token")
    require({urlsplit(records["cleanup-delete-key-" + str(index)]["request"]["path"]).path for index in range(2)} ==
            {"/emby/Auth/Keys/" + quote(row["AccessToken"], safe="") for row in key_rows},
            "Cleanup did not acknowledge deletion of both distinct owned credentials")
    old_users, final_users = {row["Id"]: row for row in private["old-users"]}, {row["Id"]: row for row in records["users-final"]["response"]["body"]}
    participants = {private[label + "-login-response"]["User"]["Id"] for label in ("control", "viewer")}
    require(set(old_users) == set(final_users) == participants, "Fresh user membership changed")
    dates = {"LastLoginDate", "LastActivityDate"}
    require(all({name: value for name, value in row.items() if name not in dates} ==
                {name: value for name, value in final_users[user_id].items() if name not in dates} for user_id, row in old_users.items()),
            "User fields changed beyond attributed login/activity dates")

    metadata_sessions = []
    before_rows = records["server-delete-sessions-before-key-probes"]["response"]["body"]
    after_rows = records["server-delete-control-protected"]["response"]["body"]
    for client in ("alpha", "beta", "sibling"):
        initial = records["key-" + client + "-sessions-metadata"]["response"]["body"]
        matches = [row for row in initial if row.get("DeviceId") == DEVICE_PREFIX + "key-" + client]
        require(len(matches) == 1 and isinstance(matches[0].get("Id"), str), "Initial metadata context is ambiguous")
        row = matches[0]
        require(any(current.get("Id") == row["Id"] for current in before_rows) and any(current.get("Id") == row["Id"] for current in after_rows),
                "Observed cached metadata session projection differs")
        metadata_sessions.append({"clientLabel": client, "sessionId": row["Id"], "internalDeviceId": row.get("InternalDeviceId"),
                                  "retainedBeforeCredentialProbes": True, "retainedAfterCredentialProbes": True, "authenticationStatus": 401})
    defaults = []
    for label in ("alpha", "sibling"):
        initial = records["key-" + label + "-sessions-token-only"]["response"]["body"]
        matches = [row for row in initial if row.get("Client") == by_label[label]["AppName"] and row.get("DeviceId") == SERVER_ID]
        require(len(matches) == 1 and all(row.get("Id") != matches[0]["Id"] for row in [*before_rows, *after_rows]),
                "Default credential session was not removed from the observed projection")
        defaults.append({"credentialLabel": label, "sessionId": matches[0]["Id"], "removedFromProjection": True})
    header_candidates = items("header-delete-candidates")
    require(not any(row.get("ReportedDeviceId") == DEVICE_PREFIX + "key-alpha" for row in header_candidates),
            "The unexecuted header-device deletion branch had an unexpected listed target")
    for label in ("server-delete-info-after", "cleanup-fresh-server-info-0"):
        require(records[label]["response"]["status"] == 204 and records[label]["response"]["body"] == "",
                "Post-deletion numeric server lookup differs from the recorded empty response")
    return {"checks": {"all109HTTPCompleteAndFreshAuthorityMatched": True, "allMutationsAcknowledgedAndOwned": True,
            "allFourCredentialsLatestRequestUnauthorized": True, "bothCredentialsAndAllFiveContextsUnauthorized": True,
            "completePostDeletionCredentialListsEmpty": True, "oldDeviceFieldsActivityAndOptionsUnchanged": True,
            "oldUsersUnchangedExceptParticipantLoginActivity": True, "onlyRevokedControlListedHistoryRetained": True},
        "httpCount": HTTP_COUNT, "mutationCount": len(journal), "firstCapturedAt": records[ordered[0]]["reference"]["capturedAt"],
        "lastCapturedAt": records[ordered[-1]]["reference"]["capturedAt"], "chronologySource": "Recorded UTC capturedAt timestamps",
        "completeCredentialListProofs": key_list_proofs, "latestCredentialInvalidityProofs": latest_proofs,
        "deviceWriteProofs": write_proofs, "credentialContextInvalidityProofs": scope_proofs,
        "sharedServerDevice": {"id": "5", "reportedDeviceId": SERVER_ID, "credentialNumericIds": sorted(row["Id"] for row in key_rows),
            "deleteStatus": 204, "controlStatusAfterDeletion": 200, "numericLookupAfterDeletion": {"status": 204, "bodyBytes": 0},
            "absenceNotInferredFromEmptyNumericResponseAlone": True},
        "sessionProjection": {"defaultContexts": defaults, "staleMetadataContexts": metadata_sessions,
            "interpretation": "Three metadata session DTOs remained cached while every associated authentication probe returned 401"},
        "headerDeviceDeletion": {"executed": False, "reason": "No Alpha metadata device was present in the bounded administrator device list",
                                  "hiddenMetadataDeviceInfoNotQueried": True},
        "retainedControlDeviceId": "3", "protectedOldDeviceIds": sorted(old_devices), "sourceWrites": 0,
        "mediaOrEncoderRequests": 0, "userPolicyWrites": 0}


def save(path: Path, value: object) -> None:
    require(path.parent.resolve(strict=True) == path.parent and OUTPUT in path.parents, "Offline output path escaped its new root")
    text = json.dumps(value, indent=2, ensure_ascii=False) + "\n" if not isinstance(value, str) else value
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "w", encoding="utf-8") as stream:
        stream.write(text)
        stream.flush()
        os.fsync(stream.fileno())


def recover() -> dict:
    require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")), "Run offline recovery only through authorized root SSH")
    require(not OUTPUT.exists() and not OUTPUT.is_symlink(), "Refusing to overwrite a previous offline recovery")
    frozen = snapshot_tree(CAPTURE)
    require(not (PRIVATE / "failure.txt").exists() and not list(PRIVATE.glob("cleanup-error-*.txt")),
            "The original capture has an earlier capture or cleanup-step failure")
    regular(PRIVATE / "cleanup-failure.txt")
    require((PRIVATE / "cleanup-failure.txt").read_text().strip() == "RuntimeError: Secret survived export redaction",
            "Original finalization failure differs")
    regular(PRIVATE / "recorder-source.py")
    manifest = load_json(EVIDENCE / "private/manifest.json")
    require(manifest.get("state") == "READY" and manifest.get("referenceRun") == 2 and manifest.get("serverId") == SERVER_ID and
            manifest.get("unit") == UNIT and manifest.get("setupRecordCount") == SETUP_COUNT, "Fresh READY manifest differs")
    baseline = load_json(PRIVATE / "baseline.json")
    preservation = verify_baseline(baseline)
    identities_before = process_identities(manifest)
    raw_paths, export_paths = sorted(RAW.glob("*.json")), sorted(EXPORT.glob("*.json"))
    require(len(raw_paths) == len(export_paths) == HTTP_COUNT and {path.name for path in raw_paths} == {path.name for path in export_paths} and
            all(path.name.startswith(PREFIX) for path in raw_paths), "Original HTTP raw/export membership differs")
    records = {path.name[len(PREFIX):-5]: load_json(path) for path in raw_paths}
    bootstrap_paths = sorted((EVIDENCE / "private/raw").glob("*.json"))
    require(len(bootstrap_paths) == SETUP_COUNT, "Fresh setup record membership differs")
    bootstrap = [load_json(path) for path in bootstrap_paths]
    legacy, sanitizer = load_legacy_sanitizer(records, bootstrap)
    for path in raw_paths:
        original = records[path.name[len(PREFIX):-5]]
        exported = load_json(EXPORT / path.name)
        require(sanitizer.sanitize(original) == exported and not any(secret in json.dumps(exported, ensure_ascii=False)
                for secret in sanitizer.secrets if secret), "An original export differs from its deterministic redaction or contains a credential")
    private = {name: load_json(PRIVATE / (name + ".json")) for name in
               ("old-devices", "old-options", "old-users", "old-keys", "mutation-results", "control-login-response", "viewer-login-response")}
    require(len(list((PRIVATE / "mutations").glob("*-intent.json"))) == 10, "Mutation intent evidence membership differs")
    for index, row in enumerate(private["mutation-results"], 1):
        require(load_json(PRIVATE / "mutations" / (str(index).zfill(3) + "-intent.json")) == row["request"], "Persisted mutation intent differs from its acknowledged request")
    result = analyze_http(records, private, manifest)
    bootstrap_proofs = verify_bootstrap_credentials(bootstrap, records)
    report = {"kind": "capture-audit-observation", "verificationMode": "offline-recovery", "cleanupPassed": True,
        "originalFinalization": {"outcome": "failed", "failureType": "RuntimeError", "reason": "Secret survived export redaction",
            "captureFailurePresent": False, "rawHTTPRecordsPreserved": HTTP_COUNT, "existingExportsModified": False,
            "recorderSourceSha256": digest(PRIVATE / "recorder-source.py"), "failureFileSha256": digest(PRIVATE / "cleanup-failure.txt")},
        "diagnosis": legacy_label_diagnostic(legacy), "recoveredVerification": result,
        "preservation": preservation, "processIdentityProofs": identities_before, "bootstrapCredentialInvalidityProofs": bootstrap_proofs,
        "original109ExportsDeterministicallyVerified": True, "sourceCaptureTree": frozen,
        "httpRequestsIssuedByAnalyzer": 0, "processLaunchesByAnalyzer": 0, "serviceManagerQueriesByAnalyzer": 0,
        "serviceConfigurationAndFinalTeardown": "Separate operator verification remains required; this analyzer reads process identity directly from procfs",
        "freshServiceLeftAliveForOperatorTeardown": True,
        "reference": {"product": "Emby Server", "version": "4.9.5.0", "recoveredAt": dt.datetime.now(dt.timezone.utc).isoformat()}}
    cleaned = sanitize_new_audit(report, sanitizer)
    require(not any(secret in json.dumps(cleaned, ensure_ascii=False) for secret in sanitizer.secrets if secret),
            "New offline audit contains an unredacted credential")
    require(snapshot_tree(CAPTURE) == frozen and verify_baseline(baseline) == preservation and process_identities(manifest) == identities_before,
            "Read-only recovery changed or raced preserved evidence")
    os.umask(0o077)
    OUTPUT.mkdir(mode=0o700)
    for path in (OUTPUT / "private", OUTPUT / "private/raw", OUTPUT / "export"):
        path.mkdir(mode=0o700)
    save(OUTPUT / ".goby-managed", OUTPUT_MARKER + "\n")
    save(OUTPUT / "private/raw" / OUTPUT_NAME, report)
    save(OUTPUT / "export" / OUTPUT_NAME, cleaned)
    require(snapshot_tree(CAPTURE) == frozen, "Original capture evidence changed while writing the separate audit")
    return {"result": "recovered", "httpRecords": HTTP_COUNT, "newAuditRecords": 1, "originalExportsModified": False,
            "httpRequests": 0, "output": str(OUTPUT / "export" / OUTPUT_NAME)}


def main() -> None:
    forbidden = RuntimeError("Offline recovery forbids network and process operations")
    try:
        with contextlib.ExitStack() as guards:
            for owner, name in ((socket, "socket"), (socket, "create_connection"), (http.client.HTTPConnection, "__init__"),
                                (subprocess, "Popen"), (subprocess, "run"), (subprocess, "check_output"), (os, "system"), (os, "execvp")):
                guards.enter_context(patch.object(owner, name, side_effect=forbidden))
            result = recover()
        print(json.dumps(result, sort_keys=True))
    except Exception as error:
        result = {"result": "failed", "failureType": type(error).__name__, "httpRequests": 0}
        if isinstance(error, AuditFailure):
            # Only authored require diagnostics and public capture labels are
            # exposed. Arbitrary parser, filesystem, or response errors are not.
            result["reason"] = str(error)
        print(json.dumps(result))
        raise SystemExit(1)


if __name__ == "__main__":
    main()
