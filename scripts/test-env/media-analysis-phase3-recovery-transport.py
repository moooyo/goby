#!/usr/bin/env python3
"""Pinned SSH transport and durable artifact transfer for Phase 3 adapters.

This module deliberately supplies transport, not acceptance conclusions. The
oracle must combine real state, HTTP and process observations. It never turns
a transport timeout or a caller-provided boolean into a passing fault result.
"""

from __future__ import annotations

import base64
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import shlex
import signal
import stat
import subprocess
import time


MAX_JSON = 16 << 20
MAX_ARTIFACT = 1 << 30
SHA = re.compile(r"[0-9a-f]{64}\Z")


class TransportError(RuntimeError):
    """A fixed error code; remote disposition is unknown after timeout."""


def need(condition, code):
    if not condition:
        raise TransportError(code)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False).encode()


def decode(raw):
    def pairs(items):
        value = {}
        for key, item in items:
            need(key not in value, "duplicate_json_key")
            value[key] = item
        return value
    need(len(raw) <= MAX_JSON, "json_budget")
    return json.loads(raw, object_pairs_hook=pairs,
                      parse_constant=lambda _: (_ for _ in ()).throw(TransportError("nonfinite_json")))


def pinned(reference, private=True, maximum=MAX_JSON):
    need(set(reference) == {"path", "sha256"} and SHA.fullmatch(reference["sha256"]), "invalid_reference")
    path = Path(reference["path"])
    need(path.is_absolute() and path.resolve(strict=True) == path, "noncanonical_file")
    info = path.lstat()
    need(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1 and info.st_size <= maximum,
         "unsafe_file")
    need(not private or stat.S_IMODE(info.st_mode) & 0o077 == 0, "private_file_permissions")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        raw = bytearray()
        while len(raw) < info.st_size:
            block = os.read(fd, min(1 << 20, info.st_size - len(raw)))
            need(bool(block), "pinned_file_truncated")
            raw.extend(block)
        need(not os.read(fd, 1), "pinned_file_grew")
        same = lambda record: (record.st_dev, record.st_ino, record.st_size, record.st_mtime_ns, record.st_ctime_ns)
        need(same(info) == same(os.fstat(fd)) == same(path.lstat()) and
             hashlib.sha256(raw).hexdigest() == reference["sha256"], "pinned_file_changed")
        return bytes(raw)
    finally:
        os.close(fd)


# The fixed bootstrap is the only remote command body. All variable arguments
# are base64-encoded JSON, then shell-quoted as individual arguments. No RPC may
# supply Python, a shell fragment, an executable, an SQL statement, or a route.
BOOTSTRAP = r'''
import base64,hashlib,json,os,pathlib,stat,sys
c=json.loads(base64.b64decode(sys.argv[1],validate=True))
def need(v,m):
 if not v: raise RuntimeError(m)
def read(r,private=False):
 p=pathlib.Path(r['path']);s=p.lstat()
 need(p.is_absolute() and p.resolve(strict=True)==p and stat.S_ISREG(s.st_mode) and s.st_uid==0 and s.st_nlink==1 and s.st_size<=16777216,'file_scope')
 need(not private or stat.S_IMODE(s.st_mode)&63==0,'file_privacy')
 b=p.read_bytes();a=p.lstat()
 need((s.st_dev,s.st_ino,s.st_size,s.st_mtime_ns,s.st_ctime_ns)==(a.st_dev,a.st_ino,a.st_size,a.st_mtime_ns,a.st_ctime_ns) and hashlib.sha256(b).hexdigest()==r['sha256'],'file_changed')
 return b
need(os.geteuid()==0 and sys.platform=='linux','guest_root_required')
need(pathlib.Path('/etc/machine-id').read_text().strip()==c['guest']['machine_id'],'machine_changed')
need(pathlib.Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower()==c['guest']['smbios_uuid'],'smbios_changed')
m=json.loads(read(c['owner_marker'],True))
need(m.get('owner_id')==c['owner_id'] and m.get('vmid')==106 and m.get('machine_id')==c['guest']['machine_id'] and m.get('smbios_uuid')==c['guest']['smbios_uuid'],'owner_changed')
read(c['binding'],True)
if c.get('release'): read(c['release'],True)
b=read(c['helper']);sys.argv=[c['helper']['path']]+c['arguments']
exec(compile(b,c['helper']['path'],'exec'),{'__name__':'__main__','__file__':c['helper']['path']})
'''

TRANSFER = r'''
import base64,hashlib,json,os,pathlib,stat,sys
c=json.loads(base64.b64decode(sys.argv[1],validate=True))
def need(v,m):
 if not v: raise RuntimeError(m)
def pinned(r):
 p=pathlib.Path(r['path']);s=p.lstat();need(p.is_absolute() and p.resolve(strict=True)==p and stat.S_ISREG(s.st_mode) and s.st_uid==0 and s.st_nlink==1 and stat.S_IMODE(s.st_mode)==384 and s.st_size<=16777216,'private_pin')
 b=p.read_bytes();a=p.lstat();need((s.st_dev,s.st_ino,s.st_size,s.st_mtime_ns,s.st_ctime_ns)==(a.st_dev,a.st_ino,a.st_size,a.st_mtime_ns,a.st_ctime_ns) and hashlib.sha256(b).hexdigest()==r['sha256'],'private_pin_changed');return json.loads(b)
need(os.geteuid()==0 and pathlib.Path('/etc/machine-id').read_text().strip()==c['machine_id'],'machine_changed')
need(pathlib.Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower()==c['smbios_uuid'],'smbios_changed')
m=pinned(c['owner_marker']);need(m.get('owner_id')==c['owner_id'] and m.get('vmid')==106 and m.get('machine_id')==c['machine_id'] and m.get('smbios_uuid')==c['smbios_uuid'],'owner_changed')
b=pinned(c['binding']);need(b.get('run_id')==c['run_id'] and b.get('private_directory')==c['private_directory'],'artifact_binding_changed')
root=pathlib.Path(c['private_directory']);s=root.lstat()
need(root.is_absolute() and root.resolve(strict=True)==root and stat.S_ISDIR(s.st_mode) and s.st_uid==0 and stat.S_IMODE(s.st_mode)==448,'private_directory')
p=pathlib.Path(c['artifact']['path']);need(p.parent==root and p.name not in ('.','..'),'artifact_scope')
need(0<c['artifact']['bytes']<=1073741824 and len(c['artifact']['sha256'])==64,'artifact_budget')
def identity(s): return (s.st_dev,s.st_ino,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
def export():
 fd=os.open(p,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC);s=os.fstat(fd)
 need(stat.S_ISREG(s.st_mode) and s.st_uid==0 and s.st_nlink==1 and stat.S_IMODE(s.st_mode)==384 and s.st_size==c['artifact']['bytes'],'artifact_identity')
 h=hashlib.sha256();total=0
 while total<s.st_size:
  b=os.read(fd,min(1048576,s.st_size-total));need(bool(b),'artifact_truncated');h.update(b);total+=len(b);sys.stdout.buffer.write(b)
 need(not os.read(fd,1) and identity(s)==identity(os.fstat(fd))==identity(p.lstat()) and h.hexdigest()==c['artifact']['sha256'],'artifact_changed')
 os.close(fd);sys.stdout.buffer.flush()
def restore():
 if p.exists():
  fd=os.open(p,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC);s=os.fstat(fd);h=hashlib.sha256()
  need(stat.S_ISREG(s.st_mode) and s.st_uid==0 and s.st_nlink==1 and stat.S_IMODE(s.st_mode)==384 and s.st_size==c['artifact']['bytes'],'existing_artifact_identity')
  while True:
   b=os.read(fd,1048576)
   if not b: break
   h.update(b)
  need(h.hexdigest()==c['artifact']['sha256'] and identity(s)==identity(os.fstat(fd))==identity(p.lstat()),'existing_artifact_changed');os.fsync(fd);os.close(fd)
  # Drain the exact external copy and verify it independently. This is a
  # read-only match of immutable bytes, never an overwrite of guest evidence.
  h=hashlib.sha256();total=0
  while total<c['artifact']['bytes']:
   b=sys.stdin.buffer.read(min(1048576,c['artifact']['bytes']-total));need(bool(b),'external_copy_truncated');h.update(b);total+=len(b)
  need(not sys.stdin.buffer.read(1) and h.hexdigest()==c['artifact']['sha256'],'external_copy_changed')
 else:
  stage=p.with_name(p.name+'.external-partial');fd=os.open(stage,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW|os.O_CLOEXEC,384)
  h=hashlib.sha256();total=0
  while total<c['artifact']['bytes']:
   b=sys.stdin.buffer.read(min(1048576,c['artifact']['bytes']-total));need(bool(b),'external_copy_truncated');h.update(b);total+=len(b);v=memoryview(b)
   while v: v=v[os.write(fd,v):]
  need(not sys.stdin.buffer.read(1) and h.hexdigest()==c['artifact']['sha256'],'external_copy_changed');os.fsync(fd);os.close(fd);os.link(stage,p);stage.unlink()
 d=os.open(root,os.O_DIRECTORY|os.O_RDONLY);os.fsync(d);os.close(d);sys.stdout.write('{"restored":true}\n')
export() if c['operation']=='export' else restore()
'''


def execute(argv, input_bytes=None, input_file=None, output_file=None, expected_output=None,
            timeout=120, output_limit=MAX_JSON):
    """Bound a local SSH child; remote timeout disposition remains unknown."""
    need((input_bytes is None) or (input_file is None), "ambiguous_input")
    process = subprocess.Popen(argv, stdin=input_file if input_file is not None else
                               subprocess.PIPE if input_bytes is not None else subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True,
                               env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8"})
    deadline = time.monotonic() + timeout
    result, errors, total, checksum = bytearray(), bytearray(), 0, hashlib.sha256()
    failure = None
    try:
        pending = memoryview(input_bytes or b"")
        with selectors.DefaultSelector() as selector:
            for pipe, role in ((process.stdout, "stdout"), (process.stderr, "stderr")):
                os.set_blocking(pipe.fileno(), False)
                selector.register(pipe, selectors.EVENT_READ, role)
            if input_bytes is not None:
                if pending:
                    os.set_blocking(process.stdin.fileno(), False)
                    selector.register(process.stdin, selectors.EVENT_WRITE, "stdin")
                else:
                    process.stdin.close()
            while selector.get_map():
                remaining = deadline - time.monotonic()
                need(remaining > 0, "ssh_observation_timeout_no_retry")
                for key, _events in selector.select(min(remaining, 0.25)):
                    if key.data == "stdin":
                        size = os.write(key.fileobj.fileno(), pending)
                        pending = pending[size:]
                        if not pending:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                        continue
                    block = os.read(key.fileobj.fileno(), 65536)
                    if not block:
                        selector.unregister(key.fileobj)
                        key.fileobj.close()
                        continue
                    if key.data == "stderr":
                        errors.extend(block)
                        need(len(errors) <= 65536, "ssh_diagnostics_budget")
                    else:
                        total += len(block)
                        need(total <= output_limit, "ssh_output_budget")
                        checksum.update(block)
                        if output_file is not None:
                            view = memoryview(block)
                            while view:
                                count = os.write(output_file, view)
                                need(count > 0, "artifact_short_write")
                                view = view[count:]
                        else:
                            result.extend(block)
        remaining = deadline - time.monotonic()
        need(remaining > 0, "ssh_observation_timeout_no_retry")
        process.wait(timeout=remaining)
        need(process.returncode == 0, "ssh_remote_operation_failed")
        if expected_output:
            need(total == expected_output["bytes"] and checksum.hexdigest() == expected_output["sha256"], "transferred_artifact_changed")
    except BaseException as error:
        failure = error
    finally:
        if failure is not None or process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                failure = TransportError("ssh_child_closure_unproven")
        for pipe in (process.stdin, process.stdout, process.stderr):
            if pipe is not None and not pipe.closed:
                pipe.close()
    if failure is not None:
        raise failure
    return bytes(result)


class Transport:
    """The semantic adapter owns decisions; this class owns pinned transport."""

    def __init__(self, configuration):
        self.config = configuration
        required = {"schema_version", "run_id", "owner_id", "guest", "owner_marker", "ssh", "remote", "artifacts_root"}
        need(isinstance(configuration, dict) and set(configuration) == required and configuration["schema_version"] == 1,
             "transport_configuration_fields")
        need(configuration["guest"]["vmid"] == 106, "transport_guest_scope")
        need(set(configuration["remote"]) == {"python", "private_directory", "guest", "state", "probe", "workload"}, "remote_transport_fields")
        ssh = configuration["ssh"]
        need(set(ssh) == {"executable", "config", "known_hosts", "target"} and
             re.fullmatch(r"goby-phase3-[A-Za-z0-9-]{1,64}", ssh["target"]), "ssh_scope")
        root = Path(configuration["artifacts_root"])
        info = root.lstat()
        need(root.is_absolute() and root.resolve(strict=True) == root and stat.S_ISDIR(info.st_mode) and
             info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700, "external_artifact_root")
        self.root = root

    def argv(self, source, arguments):
        ssh = self.config["ssh"]
        pinned(ssh["executable"], private=False, maximum=32 << 20)
        pinned(ssh["config"])
        pinned(ssh["known_hosts"])
        python = self.config["remote"]["python"]
        need(python in ("/usr/bin/python3", "/usr/bin/python3.13", "/usr/bin/python3.12"), "remote_python_not_admitted")
        encoded = base64.b64encode(canonical(arguments)).decode("ascii")
        remote_command = shlex.join([python, "-I", "-B", "-c", source, encoded])
        return [ssh["executable"]["path"], "-F", ssh["config"]["path"], "-o", "BatchMode=yes",
                "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + ssh["known_hosts"]["path"],
                "-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ConnectTimeout=10",
                "--", ssh["target"], remote_command]

    def call(self, role, request, timeout=120):
        need(role in {"guest", "state", "probe", "workload"}, "unsupported_remote_role")
        remote = self.config["remote"][role]
        need(set(remote) in ({"helper", "binding"}, {"helper", "binding", "release"}), "remote_role_fields")
        arguments = ["--binding", remote["binding"]["path"]]
        if role == "guest":
            arguments.extend(["--binding-sha256", remote["binding"]["sha256"]])
            if "release" in remote:
                arguments.extend(["--release", remote["release"]["path"], "--release-sha256", remote["release"]["sha256"]])
        bootstrap = {"guest": self.config["guest"], "owner_id": self.config["owner_id"],
                     "owner_marker": self.config["owner_marker"], "helper": remote["helper"],
                     "binding": remote["binding"], "release": remote.get("release"), "arguments": arguments}
        raw = execute(self.argv(BOOTSTRAP, bootstrap), input_bytes=canonical(request) + b"\n", timeout=timeout)
        result = decode(raw)
        need(result.get("request_id") == request["request_id"], "remote_request_identity_mismatch")
        return result

    def artifact_request(self, reference, operation):
        need(set(reference) == {"path", "bytes", "sha256"} and type(reference["bytes"]) is int and
             0 < reference["bytes"] <= MAX_ARTIFACT and SHA.fullmatch(reference["sha256"]), "artifact_reference")
        private = Path(self.config["remote"]["private_directory"])
        path = Path(reference["path"])
        need(path.parent == private and path.name not in (".", ".."), "remote_artifact_outside_private_directory")
        return {"operation": operation, "artifact": reference, "private_directory": str(private),
                "machine_id": self.config["guest"]["machine_id"], "smbios_uuid": self.config["guest"]["smbios_uuid"],
                "owner_marker": self.config["owner_marker"], "owner_id": self.config["owner_id"],
                "run_id": self.config["run_id"], "binding": self.config["remote"]["state"]["binding"]}

    def export_artifact(self, reference):
        configuration = self.artifact_request(reference, "export")
        final = self.root / (reference["sha256"] + "-" + Path(reference["path"]).name)
        need(not final.exists(), "external_artifact_already_exists")
        temporary = final.with_name(final.name + ".partial")
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            execute(self.argv(TRANSFER, configuration), output_file=fd, expected_output=reference,
                    timeout=180, output_limit=reference["bytes"])
            os.fsync(fd)
        finally:
            os.close(fd)
        os.link(temporary, final)
        temporary.unlink()
        directory = os.open(self.root, os.O_DIRECTORY | os.O_RDONLY | os.O_CLOEXEC)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        return {"path": str(final), "sha256": reference["sha256"], "bytes": reference["bytes"], "fsynced": True}

    def return_artifact(self, guest_reference, external_reference):
        need(guest_reference["sha256"] == external_reference["sha256"] and
             guest_reference["bytes"] == external_reference["bytes"], "external_baseline_mismatch")
        path = Path(external_reference["path"])
        need(path.parent == self.root and path.resolve(strict=True) == path, "external_baseline_scope")
        info = path.lstat()
        need(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1 and
             stat.S_IMODE(info.st_mode) == 0o600 and info.st_size == external_reference["bytes"], "external_baseline_identity")
        with path.open("rb") as source:
            need(hashlib.file_digest(source, "sha256").hexdigest() == external_reference["sha256"], "external_baseline_digest")
            source.seek(0)
            reply = decode(execute(self.argv(TRANSFER, self.artifact_request(guest_reference, "restore")),
                                   input_file=source, timeout=180, output_limit=4096))
        need(reply == {"restored": True}, "external_baseline_not_restored")
        return reply
