#!/usr/bin/env python3
"""Run one independently attested reference matrix through the frozen transport.

Admission replays the actual preparation ledger with injected memory-only
interfaces. This module never prepares a fixture, creates a service, resumes a
journal, or changes a producer draft. Live execution requires root SSH, Python
-I -B, and the already-created restricted systemd unit named by the attestation.
"""

from __future__ import annotations

import sys

if __name__ == "__main__" and (not sys.flags.isolated or not sys.flags.dont_write_bytecode):
    print("Use /usr/bin/python3 -I -B for the matrix operator.", file=sys.stderr)
    raise SystemExit(2)

import argparse
import base64
from copy import deepcopy
from datetime import datetime
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
from types import SimpleNamespace

sys.dont_write_bytecode = True
TRANSPORT_SHA256 = "d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1"
MATRIX_SHA256 = "da3ed22ce15a3cf82bce81be31db1a1d03a202c00d44e9ac8ef124a93a2d5259"
MAX_JSON_BYTES = 64 * 1024 * 1024
SUCCESSFUL_LOGOUT_REQUESTS = 6
RECEIPTS = {"preparation", "coordination", "catalog", "cleanup", "policy-P", "policy-Q", "media-LA", "media-LB"}
PREPARATION_FILES = {"manifest": "manifest.json", "plan": "frozen-plan.json", "terminal": "terminal.json",
    "state": "state.json", "draftExecution": "draft-execution.json", "draftMatrix": "draft-matrix.json",
    "mediaTree": "media-tree.json", "baselinePreservation": "baseline-preservation.json", "finalPreservation": "final-preservation.json"}
RUNTIME_PROPERTIES = {"Type", "RemainAfterExit", "Restart", "User", "UMask", "NoNewPrivileges", "ProtectSystem",
    "ProtectHome", "PrivateTmp", "PrivateNetwork", "TimeoutStartUSec", "MemoryMax", "TasksMax", "LimitNOFILE", "ReadWritePaths"}
SHUTDOWN_PROPERTIES = {"ActiveState", "SubState", "MainPID", "Result", "ExecMainCode", "ExecMainStatus", "ControlGroup", "RemainAfterExit", "ExecStart"}


class OperatorError(ValueError):
    """Admission or execution evidence did not meet the frozen operator contract."""


def require(condition, message):
    if not condition:
        raise OperatorError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False)


def encoded(value):
    return (canonical(value) + "\n").encode()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def sha(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def same(left, right):
    return canonical(left) == canonical(right)


def successful_preparation_limits(plan):
    """Read success bounds only after the producer source and plan are bound."""
    keys = ("normalMaximum", "successMaximumIncludingLogout", "normalLimit", "cleanupReserve", "maximumRequests")
    require(isinstance(plan, dict) and all(type(plan.get(key)) is int and plan[key] > 0 for key in keys),
            "The source-bound preparation plan needs finite integer request limits.")
    normal, total = plan["normalMaximum"], plan["successMaximumIncludingLogout"]
    require(normal <= plan["normalLimit"] and plan["cleanupReserve"] >= SUCCESSFUL_LOGOUT_REQUESTS and
            total == normal + SUCCESSFUL_LOGOUT_REQUESTS and total <= plan["maximumRequests"] and
            plan["maximumRequests"] == plan["normalLimit"] + plan["cleanupReserve"],
            "The source-bound preparation plan does not reserve exactly six successful logout/rejection requests.")
    return normal, total


def instant(value):
    require(isinstance(value, str), "A completed timestamp is required.")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(parsed.utcoffset() is not None, "A completed timestamp must include its timezone.")
    return parsed


def absolute(value):
    require(isinstance(value, str) and value.startswith("/") and ".." not in Path(value).parts and
            not any(char in value for char in ("\x00", "\r", "\n")), "An explicit normalized Linux path is required.")
    return Path(value)


def strict_json(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            require(key not in result, "Duplicate JSON keys are forbidden.")
            result[key] = value
        return result
    def constant(value):
        raise OperatorError("Nonfinite JSON is forbidden.")
    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_constant=constant)


def identity(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode),
            info.st_nlink, info.st_mtime_ns, info.st_ctime_ns)


def protected(path, *, directory=False, private=True, links=False):
    path = absolute(str(path))
    for candidate in (path, *path.parents):
        info = candidate.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == info.st_gid == 0,
                "Evidence must be root-owned and cannot traverse symlinks.")
        require(not info.st_mode & 0o022 or candidate != path and bool(info.st_mode & stat.S_ISVTX), "An authority path is writable by another owner.")
        if candidate == path:
            require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), "An authority path has the wrong type.")
            require(not private or not info.st_mode & 0o077, "Private evidence must be owner-only.")
            require(directory or links or info.st_nlink == 1, "Evidence has unexpected hard links.")
        else:
            require(stat.S_ISDIR(info.st_mode), "An authority ancestor is not a directory.")
    return path.lstat()


def read_unit(name, fields):
    require(re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", name) is not None, "An exact systemd service name is required.")
    result = subprocess.run(["/usr/bin/systemctl", "show", name, *[part for field in sorted(fields) for part in ("-p", field)]],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10, check=False,
        env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8"})
    require(result.returncode == 0 and len(result.stdout) <= 65536, "The exact owned unit could not be observed.")
    rows = result.stdout.decode("utf-8", errors="strict").splitlines()
    values = dict(row.split("=", 1) for row in rows if "=" in row)
    require(set(values) == set(fields), "A required systemd property is missing.")
    return values


def empty_cgroup(path):
    root = absolute(path)
    require(root.parent == Path("/sys/fs/cgroup/system.slice") and root.name.endswith(".service"), "Only one explicit service cgroup may be observed.")
    if not root.exists():
        return True
    remaining, seen = [root], 0
    while remaining:
        current = remaining.pop()
        seen += 1
        require(seen <= 128 and not stat.S_ISLNK(current.lstat().st_mode), "The explicit cgroup tree exceeded its bound.")
        raw = (current / "cgroup.procs").read_bytes()
        require(len(raw) <= 8192, "The cgroup process list exceeded its bound.")
        if raw.strip():
            return False
        for child in current.iterdir():
            info = child.lstat()
            require(not stat.S_ISLNK(info.st_mode), "The explicit cgroup contains a symlink.")
            if stat.S_ISDIR(info.st_mode):
                remaining.append(child)
    return True


def runtime_identity():
    return {"pid": os.getpid(), "invocationId": os.environ.get("INVOCATION_ID", ""),
            "cgroup": Path("/proc/self/cgroup").read_text(), "ssh": bool(os.environ.get("SSH_CONNECTION")),
            "isolated": bool(sys.flags.isolated), "noBytecode": bool(sys.flags.dont_write_bytecode), "uid": os.geteuid()}


def duration_seconds(value):
    require(isinstance(value, str) and value and value != "infinity", "The execution unit needs a finite start timeout.")
    pieces = re.findall(r"([0-9]+(?:\.[0-9]+)?)(us|ms|s|min|h|d)", value)
    require(pieces and "".join(number + unit for number, unit in pieces) == value.replace(" ", ""), "The systemd timeout format is not recognized.")
    factors = {"us": 0.000001, "ms": 0.001, "s": 1, "min": 60, "h": 3600, "d": 86400}
    return sum(float(number) * factors[unit] for number, unit in pieces)


class MemoryJournal:
    """Replay output sink: preserve exact producer bytes without touching its root."""
    def __init__(self, root):
        self.root = Path(root)
        self.records = {"private": {}, "export": {}}

    def save(self, name, value, *, export=False):
        target = self.records["export" if export else "private"]
        require(name not in target, "Producer replay attempted to overwrite an exclusive receipt.")
        raw = encoded(value)
        target[name] = raw
        return digest(raw)

    def state(self, value):
        self.records["private"]["state.json"] = encoded(value)

    def check(self):
        return None

    def close(self):
        return None


class Admission:
    """Read and continuously pin an external attestation and real producer ledger."""
    def __init__(self, path, checksum, *, unit_probe=read_unit, cgroup_probe=empty_cgroup, self_probe=runtime_identity):
        self.unit_probe, self.cgroup_probe, self.self_probe = unit_probe, cgroup_probe, self_probe
        self.files, self.directories = {}, {}
        self.attestation_path = absolute(path)
        require(self.attestation_path.suffix == ".json" and sha(checksum), "An exact private attestation JSON descriptor is required.")
        raw = self._read(path)
        require(digest(raw) == checksum, "The external attestation digest differs.")
        self.attestation_sha256 = checksum
        self.value = strict_json(raw)
        value = self.value
        require(isinstance(value, dict) and set(value) == {"schemaVersion", "kind", "runId", "attestedAt", "execution", "sources", "preparation", "shutdown", "runtime", "scope", "sealedRoots", "forbiddenOriginalRoots"} and
                type(value["schemaVersion"]) is int and value["schemaVersion"] == 1 and value["kind"] == "nextup-global-reference-matrix-attestation" and
                re.fullmatch(r"[A-Za-z0-9_-]{1,128}", value["runId"]), "The exact matrix attestation contract is required.")
        instant(value["attestedAt"])
        require(set(value["scope"]) == {"attestationRoot", "sourceRoot", "operatorEvidenceRoot"}, "The independent attestation, source, and operator scopes are required.")
        for key in ("attestationRoot", "sourceRoot"):
            root = absolute(value["scope"][key])
            info = protected(root, directory=True, private=key == "attestationRoot")
            self.directories[str(root)] = (info.st_dev, info.st_ino, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode))
        self.attestation_root = Path(value["scope"]["attestationRoot"])
        require(self.attestation_root in self.attestation_path.parents, "The attestation is outside its independent private root.")
        require(isinstance(value["sealedRoots"], list) and value["sealedRoots"] and isinstance(value["forbiddenOriginalRoots"], list) and value["forbiddenOriginalRoots"], "Sealed and original root exclusions must be explicit.")
        self.excluded = [absolute(item) for item in value["forbiddenOriginalRoots"]]
        self.sealed = [absolute(item) for item in value["sealedRoots"]]
        self._outside_original(self.attestation_path)
        require(set(value["sources"]) == {"operator", "preparation", "transport", "matrix"}, "Four exact owned Python sources are required.")
        names = {"operator": "run-nextup-global-reference.py", "preparation": "prepare-nextup-global-reference.py", "transport": "nextup-global-transport.py", "matrix": "nextup-global-matrix.py"}
        for role, row in value["sources"].items():
            source = self._descriptor(row, [Path(value["scope"]["sourceRoot"])], private=False)
            require(Path(row["path"]).name == names[role], "An owned source filename differs from the reviewed interface.")
            if role == "operator":
                require(Path(row["path"]) == Path(__file__).absolute(), "Operator source authority must bind this file.")
            elif role in ("transport", "matrix"):
                require(row["sha256"] == (TRANSPORT_SHA256 if role == "transport" else MATRIX_SHA256), "A frozen matrix dependency differs.")
            if role != "operator":
                name = "matrix_entry_" + role + "_" + row["sha256"]
                module = importlib.util.module_from_spec(importlib.util.spec_from_file_location(name, row["path"]))
                sys.modules[name] = module
                exec(compile(source, row["path"], "exec"), module.__dict__)
                setattr(self, role, module)
        self.support = self.transport
        self._producer_inputs()
        self._runtime_contract()
        self._replay()
        self._shutdown(full=True)
        self._runtime(full=True)
        self._fresh_outputs()
        self.frozen_inputs = canonical({"attestation": self.value, "execution": self.execution, "manifest": self.manifest})

    def _outside_original(self, path):
        require(all(path != root and root not in path.parents for root in self.excluded), "An input would read original implementation or database bytes.")

    def _read(self, path, *, private=True, links=False, maximum=MAX_JSON_BYTES):
        path = absolute(str(path))
        if hasattr(self, "excluded"):
            self._outside_original(path)
        before = protected(path, private=private, links=links)
        require(before.st_size <= maximum, "A pinned file exceeds its finite capture bound.")
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "rb") as stream:
            require(identity(os.fstat(stream.fileno())) == identity(before), "A file changed during open.")
            raw = stream.read(maximum + 1)
            require(len(raw) <= maximum and identity(os.fstat(stream.fileno())) == identity(before), "A file changed during its bounded read.")
        require(identity(path.lstat()) == identity(before), "A pinned path changed during reading.")
        self.files[str(path)] = (identity(before), private, links)
        return raw

    def _descriptor(self, row, roots, *, private=True):
        require(isinstance(row, dict) and set(row) == {"path", "sha256"} and sha(row["sha256"]), "An actual path and SHA-256 descriptor is required.")
        path = absolute(row["path"])
        require(any(root in path.parents for root in roots), "A descriptor escaped its explicit owned root.")
        raw = self._read(path, private=private)
        require(digest(raw) == row["sha256"], "A pinned evidence digest differs.")
        return raw

    def _producer_inputs(self):
        value, preparation = self.value, self.value["preparation"]
        require(set(preparation) == {"root", "inputManifest", "wireIndex", *PREPARATION_FILES}, "The complete producer output descriptor set is required.")
        self.producer_root = absolute(preparation["root"])
        self.private_root = self.producer_root / "private"
        require(self.producer_root != self.attestation_root and self.producer_root not in self.attestation_root.parents and self.attestation_root not in self.producer_root.parents,
                "Independent attestation cannot be placed inside the producer output.")
        for root in (self.producer_root, self.private_root):
            info = protected(root, directory=True)
            self.directories[str(root)] = (info.st_dev, info.st_ino, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode))
        self.documents = {}
        for key, filename in PREPARATION_FILES.items():
            require(preparation[key]["path"] == str(self.private_root / filename), "A producer document has another exact output path.")
            self.documents[key] = strict_json(self._descriptor(preparation[key], [self.private_root]))
        self.manifest = self.preparation.validate_manifest(self.documents["manifest"])
        require(self.manifest["runId"] == value["runId"] and self.manifest["scope"]["outputRoot"] == str(self.producer_root) and
                same(self.manifest["forbiddenOriginalRoots"], value["forbiddenOriginalRoots"]), "The attestation and actual producer run differ.")
        require(set(self.manifest["sealedRoots"]) <= set(value["sealedRoots"]), "The independent attestation omitted a producer sealed-root exclusion.")
        for role in ("preparation", "transport", "matrix"):
            require(same(value["sources"][role], self.manifest["sources"][role]), "The actual producer source pin differs from the attestation.")
        input_root = Path(self.manifest["scope"]["inputRoot"])
        raw_input = self._descriptor(preparation["inputManifest"], [input_root])
        require(same(strict_json(raw_input), self.manifest), "The original CLI input differs from the retained producer manifest.")
        source_plan = self.preparation.frozen_plan(self.manifest)
        require(same(self.documents["plan"], source_plan), "The retained plan is not the source-bound actual plan.")
        self.producer_normal_maximum, self.producer_success_maximum = successful_preparation_limits(source_plan)
        self.plan_sha256 = digest(canonical(self.documents["plan"]).encode())
        self.inputs = {}
        for key, row in self.manifest["inputs"].items():
            roots = [input_root, *[Path(root) for root in self.manifest["sealedRoots"]]]
            self.inputs[key] = strict_json(self._descriptor(row, roots))
        checker = object.__new__(self.preparation.Authority)
        checker.manifest, checker.records = self.manifest, self.inputs
        checker.release, checker.baseline = self.inputs["release"], self.inputs["publicBaseline"]
        checker.support, checker.planner = self.support, self.matrix
        def evidence(row, *, sealed=False):
            roots = [Path(root) for root in self.manifest["sealedRoots"]]
            if not sealed:
                roots.insert(0, input_root)
            require(Path(row["path"]).suffix == ".json", "Referenced release evidence must be an owned JSON record.")
            return strict_json(self._descriptor(row, roots))
        checker._evidence = evidence
        self._descriptor(self.manifest["media"]["approvedReceipt"], [Path(self.manifest["media"]["ownedRoot"])], private=False)
        checker._inputs()
        self.execution = strict_json(self._descriptor(value["execution"], [self.attestation_root]))
        draft = self.documents["draftExecution"]
        require(same(draft["matrix"], self.documents["draftMatrix"]) and draft["matrix"]["binding"]["fixtureReleased"] is False,
                "An immutable unpublished producer draft is required.")
        published = deepcopy(draft)
        published["matrix"]["binding"]["fixtureReleased"] = True
        require(same(self.execution, published), "Only independently attested fixtureReleased publication may differ from the producer draft.")
        require(self.execution["matrix"]["target"] == "reference" and self.execution["ownerUid"] == 0 and
                self.execution["endpoint"] == {"scheme": "http", "host": "127.0.0.1", "port": 18197}, "Only the bound reference proxy target is supported.")
        terminal, state = self.documents["terminal"], self.documents["state"]
        require(terminal.get("status") == "awaiting_independent_attestation" and terminal.get("completed") is True and
                terminal.get("cleanupComplete") is True and terminal.get("matrixInputsUsable") is False and
                terminal.get("independentAttestationRequired") is True and terminal.get("uncertain") is False and
                all(terminal.get(key) is None for key in ("failure", "pending", "ownershipPending")) and terminal.get("cleanupErrors") == [],
                "The actual producer did not complete safely awaiting independent attestation.")
        require(state.get("runId") == value["runId"] and terminal.get("runId") == value["runId"] and state.get("phase") == "cleanup" and
                state.get("uncertain") is False and all(state.get(key) is None for key in ("failure", "pending", "ownershipPending")) and
                set(state.get("tokens", {})) == set(state.get("sessions", {})) == set(state.get("revoked", [])) == {"admin", "P", "Q"},
                "The final producer state has unresolved actor or playback responsibility.")
        require(type(terminal.get("requestCount")) is int and 0 < terminal["requestCount"] <= self.producer_success_maximum and
                type(terminal.get("normalRequestCount")) is int and 0 < terminal["normalRequestCount"] <= self.producer_normal_maximum and
                terminal.get("cleanupRequestCount") == SUCCESSFUL_LOGOUT_REQUESTS and
                type(terminal.get("phaseRequestCounts", {}).get("cleanup")) is int and
                terminal.get("phaseRequestCounts", {}).get("cleanup") == SUCCESSFUL_LOGOUT_REQUESTS and
                terminal["requestCount"] == terminal["normalRequestCount"] + SUCCESSFUL_LOGOUT_REQUESTS and
                all(type(state.get(key)) is int and type(terminal.get(key)) is int and state[key] == terminal[key]
                    for key in ("requestCount", "normalRequestCount", "cleanupRequestCount", "chargedResponseBytes")),
                "The actual successful preparation request budget does not close.")
        require(set(terminal.get("outputs", {})) == RECEIPTS | {"matrix", "execution"}, "The producer terminal output set is incomplete.")
        for key in ("matrix", "execution"):
            require(same(terminal["outputs"][key], preparation["draft" + key.title()]), "A terminal draft descriptor differs.")
        require(same(draft["receipts"], {key: terminal["outputs"][key] for key in RECEIPTS}), "Draft receipt descriptors differ from the successful producer outputs.")
        self.receipts = {key: strict_json(self._descriptor(row, [self.private_root])) for key, row in draft["receipts"].items()}
        self.media = {key: {name: deepcopy(item) for name, item in self.receipts["media-" + key]["facts"].items() if name != "sourceRootId"} for key in ("LA", "LB")}
        self._media_files()
        self.index = strict_json(self._descriptor(preparation["wireIndex"], [self.attestation_root]))
        self._ledger()

    def _media_files(self):
        tree = self.documents["mediaTree"]
        require(isinstance(tree, dict) and set(tree) == {"source", "files", "originalRootsModified"} and tree["originalRootsModified"] is False and
                same(tree["source"], self.manifest["media"]["source"]), "The actual independent-copy tree receipt differs.")
        expected, expected_media, metadata_bytes = set(), {}, {}
        source = self.manifest["media"]["source"]
        source_raw = self._read(source["path"], private=False, links=True, maximum=128 * 1024 * 1024)
        require(digest(source_raw) == source["sha256"] and same(self.preparation.file_identity(Path(source["path"]).stat()),
                {key: value for key, value in source.items() if key not in ("path", "sha256")}), "The approved owned synthetic source changed after preparation.")
        for library in ("LA", "LB"):
            root = Path(self.manifest["media"]["roots"][library])
            series_name = self.manifest["libraries"][library]["seriesName"]
            series = root / series_name
            files = {}
            metadata_bytes[str(root / ".goby-managed")] = ("nextup-global-" + self.manifest["runId"] + "-" + library + "\n").encode()
            metadata_bytes[str(series / "tvshow.nfo")] = ("<tvshow><title>" + series_name + "</title></tvshow>\n").encode()
            for index, (season, episode) in enumerate(((1, 1), (1, 2), (2, 1)), 1):
                path = series / ("Season %02d" % season) / ("%s S%02dE%02d.mp4" % (series_name, season, episode))
                files[library[1] + str(index)] = {"path": str(path), "sha256": source["sha256"], "sizeBytes": source["sizeBytes"]}
                metadata_bytes[str(path.with_suffix(".nfo"))] = ("<episodedetails><title>Episode %d-%d</title><season>%d</season><episode>%d</episode></episodedetails>\n" %
                    (season, episode, season, episode)).encode()
            expected_media[library] = {"rootPath": str(root), "files": files}
            expected.update((str(root / ".goby-managed"), str(series / "tvshow.nfo")))
            for row in files.values():
                expected.update((row["path"], str(Path(row["path"]).with_suffix(".nfo"))))
        require(same(self.media, expected_media), "The draft media receipts do not describe the exact independent approved copies.")
        require(set(tree["files"]) == expected and len(expected) == 16, "The prepared media and metadata tree is not the exact sixteen-file population.")
        self.media_files_expected = {Path(path) for path in expected}
        self.media_directories_expected = {self.producer_root / "media"}
        for path in self.media_files_expected:
            parent = path.parent
            while parent != self.producer_root / "media":
                require(self.producer_root / "media" in parent.parents, "A media ancestor escaped its exact root.")
                self.media_directories_expected.add(parent)
                parent = parent.parent
        require(len(self.media_directories_expected) == 9, "The two-series media tree must have exactly nine required directories.")
        self.media_directory_ids = None
        self._media_membership()
        media_ids = set()
        for path, row in tree["files"].items():
            require(self.producer_root / "media" in Path(path).parents, "A copied media/tree file escaped the preparation root.")
            raw = self._read(path, private=False, maximum=128 * 1024 * 1024)
            info = Path(path).stat()
            require(row.get("sha256") == digest(raw) and same({key: item for key, item in row.items() if key != "sha256"}, self.preparation.file_identity(info)),
                    "A prepared media or metadata file changed after the producer copy proof.")
            if Path(path).suffix == ".mp4":
                require(digest(raw) == source["sha256"], "A copied episode no longer matches the approved synthetic bytes.")
                media_ids.add((info.st_dev, info.st_ino))
            else:
                require(raw == metadata_bytes[path], "A prepared ownership marker or NFO differs from its exact source-bound bytes.")
        require(len(media_ids) == 6, "The six media files are not independent copies.")

    def _media_membership(self):
        """Lstat one bounded exact tree; never follow or traverse an extra node."""
        root = self.producer_root / "media"
        expected_nodes = self.media_files_expected | self.media_directories_expected
        expected_count = len(expected_nodes)
        seen_files, seen_directories, directory_ids = set(), set(), {}
        pending = [(root, os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW))]
        try:
            while pending:
                path, descriptor = pending.pop()
                try:
                    named, opened = path.lstat(), os.fstat(descriptor)
                    require(stat.S_ISDIR(named.st_mode) and not stat.S_ISLNK(named.st_mode) and identity(named) == identity(opened) and
                            named.st_uid == named.st_gid == 0 and not named.st_mode & 0o022,
                            "A media directory changed type, identity, or protected ownership.")
                    require(path in self.media_directories_expected and path not in seen_directories, "An unlisted or repeated media directory was encountered.")
                    seen_directories.add(path)
                    directory_ids[str(path)] = identity(named)
                    with os.scandir(descriptor) as entries:
                        for entry in entries:
                            child = path / entry.name
                            require(len(seen_files) + len(seen_directories) + len(pending) < expected_count and child in expected_nodes,
                                    "The real media tree contains an additional file or directory.")
                            info = entry.stat(follow_symlinks=False)
                            require(not stat.S_ISLNK(info.st_mode), "Media symlinks are forbidden, including unlisted or dangling links.")
                            if stat.S_ISREG(info.st_mode):
                                require(child in self.media_files_expected and child not in seen_files and info.st_uid == info.st_gid == 0 and
                                        not info.st_mode & 0o022 and info.st_nlink == 1, "An unlisted, replaced, or unprotected media file was encountered.")
                                seen_files.add(child)
                            elif stat.S_ISDIR(info.st_mode):
                                require(child in self.media_directories_expected, "The media tree contains an unnecessary directory.")
                                child_fd = os.open(entry.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
                                try:
                                    require(identity(os.fstat(child_fd)) == identity(info), "A media child changed while opening its directory.")
                                    pending.append((child, child_fd))
                                except BaseException:
                                    os.close(child_fd)
                                    raise
                            else:
                                raise OperatorError("Special filesystem nodes are forbidden in the immutable media tree.")
                    require(identity(path.lstat()) == identity(named), "The media directory changed while its membership was inspected.")
                finally:
                    os.close(descriptor)
            require(seen_files == self.media_files_expected and seen_directories == self.media_directories_expected,
                    "The real media tree is missing a required file or directory.")
            if self.media_directory_ids is None:
                self.media_directory_ids = directory_ids
            else:
                require(directory_ids == self.media_directory_ids, "A previously admitted media directory was replaced or modified.")
        finally:
            for unused_path, descriptor in pending:
                os.close(descriptor)

    def _ledger(self):
        index, terminal = self.index, self.documents["terminal"]
        require(isinstance(index, dict) and set(index) == {"schemaVersion", "kind", "runId", "producerRoot", "requests"} and
                type(index["schemaVersion"]) is int and index["schemaVersion"] == 1 and index["kind"] == "nextup-global-preparation-wire-index" and
                index["runId"] == self.value["runId"] and index["producerRoot"] == str(self.producer_root) and
                isinstance(index["requests"], list) and len(index["requests"]) == terminal["requestCount"], "The complete actual preparation wire index is required.")
        self.ledger, labels, expected_names = [], set(), set()
        previous_time = None
        for ordinal, row in enumerate(index["requests"], 1):
            require(isinstance(row, dict) and set(row) == {"ordinal", "label", "intent", "reserved", "response"} and type(row["ordinal"]) is int and
                    row["ordinal"] == ordinal and re.fullmatch(r"[A-Za-z0-9_-]{1,88}", row["label"]) and row["label"] not in labels,
                    "Wire index ordinals and unique labels must cover the actual ordered request sequence.")
            labels.add(row["label"])
            values = {}
            for kind in ("intent", "reserved", "response"):
                name = "%04d-%s-%s.json" % (ordinal, row["label"], kind)
                require(row[kind]["path"] == str(self.private_root / name), "A wire descriptor is not its exact producer record path.")
                expected_names.add(name)
                values[kind] = strict_json(self._descriptor(row[kind], [self.private_root]))
            intent, reserved, response = values["intent"], values["reserved"], values["response"]
            pending = reserved.get("pending")
            require(isinstance(pending, dict) and all(document.get("ordinal") == ordinal and document.get("label") == row["label"] for document in (intent, pending, response)) and
                    intent.get("actor") == pending.get("actor") == response.get("actor") and
                    same(intent.get("request"), pending.get("request")) and same(intent["request"], response.get("request")) and
                    pending.get("intentSha256") == row["intent"]["sha256"] and intent.get("planSha256") == self.plan_sha256 and
                    reserved.get("planSha256") == self.plan_sha256 and reserved.get("manifestSha256") == digest(canonical(self.manifest).encode()) and
                    reserved.get("requestCount") == ordinal, "Intent, reserved state, and raw response do not identify the same attempted request.")
            require(type(response.get("status")) is int and 100 <= response["status"] <= 599 and response.get("completeHttp") is True and
                    response.get("failure") is None and response.get("retainedRawTruncated") is False and
                    same(response.get("payloadBase64"), intent.get("payloadBase64")), "A producer response is incomplete, failed, or bound to another payload.")
            raw = base64.b64decode(response["rawBase64"], validate=True)
            require(type(response.get("observedRawBytes")) is int and len(raw) == response["observedRawBytes"] <= self.manifest["budgets"]["responseBytes"], "A producer raw response exceeds or disagrees with its captured bound.")
            completed = instant(response["completedAt"])
            require((previous_time is None or completed >= previous_time) and completed <= instant(self.value["attestedAt"]), "The actual response/attestation time chain differs.")
            previous_time = completed
            self.ledger.append({**values, "raw": raw})
        actual_names = {path.name for path in self.private_root.iterdir() if re.fullmatch(r"[0-9]{4}-[A-Za-z0-9_-]+-(?:intent|reserved|response)\.json", path.name)}
        require(actual_names == expected_names, "An actual request record is missing from or additional to the complete wire index.")
        self.last_preparation_response = previous_time

    def _replay(self):
        memory = MemoryJournal(self.producer_root)
        count = 0
        support = self.support
        ledger = self.ledger
        outer = self
        class ReplayTransport:
            def send(self, request, headers, payload, *, timeout_seconds, max_bytes):
                nonlocal count
                require(count < len(ledger), "Producer replay requested an unrecorded HTTP attempt.")
                row = ledger[count]
                intent, response = row["intent"], row["response"]
                require(request.label == intent["label"] and request.actor == intent["actor"] and
                        request.method == intent["request"]["method"] and request.route == intent["request"]["route"] and
                        same([[key, value] for key, value in headers.items()], intent["request"]["headers"]) and
                        (None if payload is None else base64.b64encode(payload).decode()) == intent["payloadBase64"],
                        "The frozen producer would not dispatch this recorded request or credential context.")
                require(0 < timeout_seconds <= outer.manifest["budgets"]["requestSeconds"] and max_bytes == outer.manifest["budgets"]["responseBytes"] + 1,
                        "Producer replay changed the bounded HTTP envelope.")
                count += 1
                return support.WireResponse(response["status"], response["headers"], row["raw"], True, response["completedAt"], None)
        tick = 0.0
        def monotonic():
            nonlocal tick
            tick += 0.001
            return tick
        def sleeper(seconds):
            nonlocal tick
            require(0 < seconds <= 5, "Producer replay exceeded its bounded wait allowance.")
            tick += seconds
        public = [strict_json(self._read(self.private_root / (prefix + "-public.json"))) for prefix in ("before", "after")]
        captured = iter(row["captured_at"] for row in public)
        def stage_media(journal):
            journal.save("media-tree.json", self.documents["mediaTree"])
            return deepcopy(self.media)
        authority = SimpleNamespace(support=support, planner=self.matrix, credentials=deepcopy(self.inputs["credentials"]["accounts"]),
            baseline=deepcopy(self.inputs["publicBaseline"]), release=deepcopy(self.inputs["release"]), acquire=lambda: None, check=lambda: None,
            close=lambda: None, stage_media=stage_media)
        replay = self.preparation.PreparationRunner(self.manifest, authority=authority, transport=ReplayTransport(),
            journal_factory=lambda root, uid: memory, monotonic=monotonic, sleeper=sleeper, utc_now=lambda: next(captured))
        terminal = replay.run()
        require(count == len(ledger) and terminal.get("status") == "awaiting_independent_attestation" and same(terminal, self.documents["terminal"]),
                "The actual raw ledger does not reproduce the successful preparation terminal.")
        for name, expected in memory.records["private"].items():
            actual = self._read(self.private_root / name)
            require(actual == expected, "A producer private output cannot be reproduced from its source, input, and actual raw responses: " + name)
        actual_private = {path.name for path in self.private_root.iterdir()}
        require(actual_private == set(memory.records["private"]), "The producer private scope contains unaccounted files.")
        self.replay_count = count
        self.secrets = set(replay.secrets)
        self.secrets.update(str(path) for path in self.files)
        self.secrets.update(str(root) for root in (self.attestation_root, self.producer_root, *self.sealed, *self.excluded))

    def _runtime_contract(self):
        runtime = self.value["runtime"]
        require(isinstance(runtime, dict) and set(runtime) == {"unitName", "properties"} and
                re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", runtime["unitName"]) and set(runtime["properties"]) == RUNTIME_PROPERTIES,
                "The existing restricted matrix unit and exact static properties are required.")
        properties = runtime["properties"]
        require(all(properties[key] == expected for key, expected in {"Type": "oneshot", "RemainAfterExit": "yes", "Restart": "no", "UMask": "0077",
                "NoNewPrivileges": "yes", "ProtectSystem": "strict", "ProtectHome": "yes", "PrivateTmp": "yes", "PrivateNetwork": "no"}.items()) and
                properties["User"] in ("", "root") and 0 < duration_seconds(properties["TimeoutStartUSec"]) <= 3600,
                "The matrix unit lacks the reviewed isolation, no-restart, or finite start deadline.")
        for key, maximum in (("MemoryMax", 2 * 1024 * 1024 * 1024), ("TasksMax", 256), ("LimitNOFILE", 65536)):
            require(isinstance(properties[key], str) and properties[key].isdigit() and 0 < int(properties[key]) <= maximum, "A matrix runtime resource limit is not finite or bounded.")
        roots = shlex.split(properties["ReadWritePaths"])
        parents = {absolute(str(Path(self.value["scope"]["operatorEvidenceRoot"]).parent)), absolute(self.execution["scope"]["evidenceParent"])}
        readonly = {self.producer_root, self.attestation_root, *self.sealed, *self.excluded,
            Path(self.manifest["scope"]["inputRoot"]), Path(self.execution["scope"]["receiptRoot"]),
            Path(self.execution["scope"]["credentialRoot"]), Path(self.value["scope"]["sourceRoot"]),
            Path(self.manifest["scope"]["sourceRoot"]), Path(self.manifest["scope"]["proxySourceRoot"]),
            Path(self.execution["scope"]["sourceRoot"]), Path(self.execution["scope"]["proxySourceRoot"]),
            Path(self.execution["credentials"]["path"]), *[Path(path) for path in self.files],
            *[Path(row["path"]) for row in self.value["sources"].values()], *[Path(row["path"]) for row in self.execution["sources"].values()]}
        require(all(parent != protected_path and parent not in protected_path.parents for parent in parents for protected_path in readonly),
                "A writable evidence parent would cover a read-only source, input, credential, preparation, attestation, sealed, or original authority.")
        permitted = {str(parent) for parent in parents}
        lock_path = Path(self.execution["lock"]["path"])
        if not any(Path(root) in lock_path.parents for root in permitted):
            permitted.add(str(lock_path))
        require(set(roots) == permitted, "Runtime writable roots must be exactly the evidence parents and, when needed, the existing lock file.")

    def _shutdown(self, *, full):
        shutdown = self.value["shutdown"]
        require(isinstance(shutdown, dict) and set(shutdown) == {"observedAt", "units"} and isinstance(shutdown["units"], list) and len(shutdown["units"]) == 1,
                "One exact completed preparation unit proof is required.")
        require(self.last_preparation_response <= instant(shutdown["observedAt"]) <= instant(self.value["attestedAt"]), "Preparation shutdown and attestation times are out of order.")
        row = shutdown["units"][0]
        require(set(row) == {"name", "invocationId", "properties", "cgroupPath"} and re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", row["name"]) and
                re.fullmatch(r"[0-9a-f]{32}", row["invocationId"]) and set(row["properties"]) == SHUTDOWN_PROPERTIES and
                row["name"] != self.value["runtime"]["unitName"], "The producer and future matrix unit identities must be explicit and distinct.")
        properties = row["properties"]
        require((properties["ActiveState"], properties["SubState"]) in (("active", "exited"), ("inactive", "dead")) and properties["MainPID"] == "0" and
                properties["Result"] == "success" and properties["ExecMainCode"] in ("1", "exited") and properties["ExecMainStatus"] == "0" and
                properties["RemainAfterExit"] == "yes" and properties["ControlGroup"] in ("", "/system.slice/" + row["name"]) and
                row["cgroupPath"] == "/sys/fs/cgroup/system.slice/" + row["name"], "The actual preparation unit did not finish successfully.")
        command = properties["ExecStart"]
        require(isinstance(command, str) and command.count("argv[]=") == 1, "The completed unit must retain its actual producer command.")
        argv = shlex.split(command.split("argv[]=", 1)[1].split(";", 1)[0].strip())
        expected = ["/usr/bin/python3", "-I", "-B", self.value["sources"]["preparation"]["path"], "prepare", "--manifest",
            self.value["preparation"]["inputManifest"]["path"], "--manifest-sha256", self.value["preparation"]["inputManifest"]["sha256"], "--plan-sha256", self.plan_sha256]
        require(argv == expected, "The completed unit did not execute this exact preparation source/input/plan.")
        if full:
            actual = self.unit_probe(row["name"], SHUTDOWN_PROPERTIES | {"InvocationID"})
            require(same(actual, {"InvocationID": row["invocationId"], **properties}), "The current preparation unit differs from its attested completion.")
        require(self.cgroup_probe(row["cgroupPath"]), "The completed preparation cgroup is not empty.")

    def _runtime(self, *, full):
        current = self.self_probe()
        name = self.value["runtime"]["unitName"]
        require(current.get("uid") == 0 and current.get("ssh") is True and current.get("isolated") is True and current.get("noBytecode") is True and
                type(current.get("pid")) is int and current["pid"] > 1 and re.fullmatch(r"[0-9a-f]{32}", current.get("invocationId", "")) and
                current.get("cgroup") == "0::/system.slice/" + name + "\n", "The operator is not inside its exact root SSH isolated systemd invocation.")
        if hasattr(self, "runtime_observed"):
            require(same(current, self.runtime_observed), "The operator process or invocation identity changed.")
        if full:
            extra = {"InvocationID", "MainPID", "ActiveState", "SubState", "ControlGroup"}
            actual = self.unit_probe(name, RUNTIME_PROPERTIES | extra)
            require(all(actual[key] == expected for key, expected in self.value["runtime"]["properties"].items()) and
                    actual["InvocationID"] == current["invocationId"] and actual["MainPID"] == str(current["pid"]) and
                    (actual["ActiveState"], actual["SubState"]) in (("activating", "start"), ("active", "running")) and
                    actual["ControlGroup"] == "/system.slice/" + name, "The currently running matrix unit does not own this exact operator process.")
        self.runtime_observed = deepcopy(current)

    def _fresh_outputs(self):
        roots = [absolute(self.value["scope"]["operatorEvidenceRoot"]), absolute(self.execution["matrix"]["binding"]["evidenceRoot"])]
        require(roots[0] != roots[1] and roots[0] not in roots[1].parents and roots[1] not in roots[0].parents, "Operator and matrix evidence roots must be distinct.")
        for root in roots:
            self._outside_original(root)
            require(not os.path.lexists(root) and all(root != old and old not in root.parents and root not in old.parents for old in [self.producer_root, self.attestation_root, *self.sealed]),
                    "A fresh execution root overlaps existing, sealed, or preparation evidence.")
            info = protected(root.parent, directory=True, private=False)
            self.directories[str(root.parent)] = (info.st_dev, info.st_ino, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode))

    def checkpoint(self, *, full=True):
        require(canonical({"attestation": self.value, "execution": self.execution, "manifest": self.manifest}) == self.frozen_inputs,
                "In-memory admission inputs changed after verification.")
        for path, (expected, private, links) in self.files.items():
            require(identity(protected(path, private=private, links=links)) == expected, "A pinned admission source or evidence record changed.")
        for path, expected in self.directories.items():
            info = protected(path, directory=True, private=False)
            require((info.st_dev, info.st_ino, info.st_uid, info.st_gid, stat.S_IMODE(info.st_mode)) == expected, "An admission directory identity changed.")
        self._media_membership()
        self._shutdown(full=full)
        self._runtime(full=full)


def execute(admission, *, target_probe=None, runner_factory=None):
    """Run once; the operator owns a separate terminal journal and never retries."""
    admission.checkpoint(full=True)
    journal = admission.support.Journal(admission.value["scope"]["operatorEvidenceRoot"], uid=0)
    runner = None
    result = None
    status = "recovery_required"
    failure = None
    terminal = None
    pins = {"attestationSha256": admission.attestation_sha256, "executionSha256": admission.value["execution"]["sha256"],
            "producerTerminalSha256": admission.value["preparation"]["terminal"]["sha256"], "wireIndexSha256": admission.value["preparation"]["wireIndex"]["sha256"],
            "sources": admission.value["sources"], "runtime": admission.runtime_observed}
    try:
        journal.save("admission.json", {"schemaVersion": 1, "runId": admission.value["runId"], "pins": pins,
            "replayedPreparationRequests": admission.replay_count, "producerOutputsReproduced": True})
        def combined_probe(expected):
            admission.checkpoint(full=True)
            return (target_probe or admission.support.process_identity)(expected)
        factory = runner_factory or admission.support.TransportRunner
        runner = factory(admission.execution, probe=combined_probe)
        result = runner.run()
        admission.checkpoint(full=True)
        matrix_root = Path(admission.execution["matrix"]["binding"]["evidenceRoot"])
        private_state = strict_json(admission._read(matrix_root / "private/state.json"))
        safe_result_raw = admission._read(matrix_root / "export/result.json")
        safe_result = strict_json(safe_result_raw)
        require(same(safe_result, admission.support.sanitized(result, runner.secrets)), "The transport result does not match its actual retained result receipt.")
        clean = (result.get("cleanupComplete") is True and result.get("evidenceComplete") is True and private_state.get("blocked") is False and
            private_state.get("finished") is True and private_state.get("unverifiedLogins") == {} and private_state.get("unresolvedResponses") == [] and
            private_state.get("matrix", {}).get("pending") is None and set(private_state.get("matrix", {}).get("revokedActors", [])) == {"P", "Q"})
        if clean and result.get("mode") == "closed" and result.get("failure") is None:
            status = "matrix_protocol_complete"
        elif clean and result.get("mode") == "closed-with-observation-failure":
            status = "matrix_observation_failed_cleanup_complete"
    except BaseException as error:
        failure = {"type": type(error).__name__, "message": str(error)}
    finally:
        secrets = admission.secrets | (set(runner.secrets) if runner is not None and hasattr(runner, "secrets") else set())
        terminal = {"schemaVersion": 1, "runId": admission.value["runId"], "status": "awaiting_operator_commit", "candidateStatus": status, "pins": pins,
            "transportResult": result, "failure": failure, "cleanupComplete": status in ("matrix_protocol_complete", "matrix_observation_failed_cleanup_complete"),
            "completionCommitted": False, "independentRuntimeClosureRequired": True, "clientAcceptanceClaim": False, "resumeOrRetryAllowed": False}
        try:
            private_sha = journal.save("terminal.json", terminal)
            export_sha = journal.save("terminal.json", admission.support.sanitized(terminal, secrets), export=True)
            commit = {"schemaVersion": 1, "kind": "nextup-global-reference-matrix-operator-commit", "runId": admission.value["runId"],
                "status": status, "terminalPrivateSha256": private_sha, "terminalExportSha256": export_sha,
                "attestationSha256": admission.attestation_sha256, "runtime": admission.runtime_observed,
                "independentRuntimeClosureRequired": True, "clientAcceptanceClaim": False, "resumeOrRetryAllowed": False}
            commit_sha = journal.save("commit.json", commit)
            terminal = {**terminal, "status": status, "completionCommitted": True, "commitSha256": commit_sha}
        except BaseException as error:
            terminal = {**terminal, "status": "recovery_required", "cleanupComplete": False, "completionCommitted": False,
                        "terminalPersistenceFailure": type(error).__name__}
        try:
            journal.close()
        except BaseException as error:
            terminal = {**terminal, "status": "recovery_required", "cleanupComplete": False, "completionCommitted": False,
                        "terminalPersistenceFailure": type(error).__name__}
    return terminal


def main(arguments=None):
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode,
            "Use authorized root SSH with /usr/bin/python3 -I -B.")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--attestation", required=True)
    parser.add_argument("--attestation-sha256", required=True)
    args = parser.parse_args(arguments)
    admission = Admission(args.attestation, args.attestation_sha256)
    terminal = execute(admission)
    result = terminal.get("transportResult") or {}
    print(canonical({"status": terminal["status"], "requestCount": result.get("httpAttempts", 0),
        "cleanupComplete": terminal["cleanupComplete"], "completionCommitted": terminal["completionCommitted"],
        "commitSha256": terminal.get("commitSha256"), "independentRuntimeClosureRequired": True, "clientAcceptanceClaim": False}))
    return 0 if terminal["status"] == "matrix_protocol_complete" else 2


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(canonical({"status": "operator_failed", "failureType": type(error).__name__, "businessHttpStatus": "requires_independent_review", "resumeOrRetryAllowed": False}), file=sys.stderr)
        raise SystemExit(2)
