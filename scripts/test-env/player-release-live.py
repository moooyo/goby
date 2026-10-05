#!/usr/bin/env python3
"""Bounded host operations for an explicitly owned Docker player release check.

Run on the Docker host. The browser driver can call this helper through an SSH
argument vector; credentials never need to be present in that vector. Provision
``release-host.json`` and the media before using this helper. It does not create,
remove, or replace containers, source media, databases, or toolchains.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys


OWNER = "goby-player-release-165c-20261005"
KEYS = ("p5", "p84", "p82")


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def command(arguments, timeout=30):
    value = subprocess.run(arguments, text=True, capture_output=True, timeout=timeout)
    if value.returncode:
        # Docker errors can contain environment details. Keep private diagnostics
        # out of stdout, which is copied into the browser acceptance evidence.
        raise RuntimeError("Owned Docker operation failed: " + arguments[1])
    return value.stdout


class Host:
    def __init__(self, requested):
        require(os.name == "posix", "Use the designated remote Linux host")
        self.root = requested.resolve(strict=True)
        require(self.root == requested.absolute(), "The owned root must not use symlinks")
        require((self.root / ".owner").read_text().strip() == OWNER, "Unexpected owner")
        self.config = json.loads(self.path("release-host.json", existing=True).read_text())
        require(self.config.get("owner") == OWNER, "Unexpected configuration owner")
        self.docker = self.config.get("dockerCommand", ["docker"])
        require(isinstance(self.docker, list) and all(isinstance(value, str) and value for value in self.docker), "Invalid Docker command")
        require(len(self.docker) == 1 or len(self.docker) == 3 and self.docker[1:] == ["-H", "unix://" + str(self.root / "runtime/docker.sock")], "Use only the owned Docker socket")
        require(self.docker[0] == "docker" or self.docker[0].startswith("/opt/") and self.docker[0].endswith("/docker"), "Invalid Docker CLI")
        self.container = self.config["container"]
        require(re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,100}", self.container), "Invalid container name")
        self.media = self.path(self.config.get("mediaRoot", "media"), existing=True)
        self.evidence = self.path("artifacts", existing=True)
        self.mount = self.config.get("containerMediaRoot", "/media")
        require(self.mount.startswith("/") and ".." not in self.mount.split("/"), "Invalid media mount")
        self.ffprobe = self.config["ffprobe"]
        require(self.ffprobe.startswith("/opt/") and self.ffprobe.endswith("/ffprobe"), "Invalid ffprobe path")
        self.fixtures = {item["key"]: item for item in self.config["fixtures"]}
        require(set(self.fixtures) == set(KEYS), "Provide exactly P5, P8.4, and P8.2")
        image = self.run(["inspect", "--format={{.Image}}", self.container]).strip()
        owner = self.run(["inspect", '--format={{index .Config.Labels "goby.owner"}}', self.container]).strip()
        require(owner == OWNER and image == self.config["imageId"], "Container owner or image changed")

    def run(self, arguments, timeout=30):
        return command(self.docker + arguments, timeout)

    def path(self, value, existing=False):
        result = self.root / value
        require(result.is_relative_to(self.root), "A path escaped the owned root")
        require(result.resolve(strict=existing) == result, "Symlinks are not allowed in the owned paths")
        return result

    def fixture(self, key):
        require(key in self.fixtures, "Unknown fixture")
        fixture = self.fixtures[key]
        source = self.media / fixture["relativePath"]
        require(source.is_relative_to(self.media) and source.resolve(strict=True) == source and source.is_file(), "Invalid source path")
        require(re.fullmatch(r"[a-f0-9]{64}", fixture["sha256"]), "Invalid source digest")
        return fixture, source

    def sidecar(self, key):
        _, source = self.fixture(key)
        directory = source.parent / "backdrops" / "goby" / hashlib.sha256(source.name.encode()).hexdigest()
        require(directory.resolve() == directory, "Invalid sidecar path")
        return directory

    def snapshot(self, key):
        fixture, source = self.fixture(key)
        require(digest(source) == fixture["sha256"], "A source fixture changed")
        directory = self.sidecar(key)
        if not directory.exists():
            return None
        require(directory.is_dir() and not directory.is_symlink(), "Invalid sidecar directory")
        owner = json.loads((directory / ".owner.json").read_text())
        require(owner["Format"] == "goby-background-clip-v1" and owner["SourceName"] == source.name, "Unexpected sidecar owner")
        manifest_path = directory / "manifest.json"
        if not manifest_path.exists():
            return None
        require(manifest_path.resolve() == manifest_path and manifest_path.is_file(), "Invalid manifest")
        manifest = json.loads(manifest_path.read_text())
        require(manifest["SourceName"] == source.name and re.fullmatch(r"gen-[a-f0-9]{32}\.mp4", manifest["Generation"]), "Invalid generation")
        video = directory / manifest["Generation"]
        require(video.resolve(strict=True) == video and video.is_file(), "Invalid published file")
        sha256 = digest(video)
        require(sha256 == manifest["SHA256"] and video.stat().st_size == manifest["Size"], "Published bytes differ from manifest")
        return {"generation": video.name, "sha256": sha256, "size": video.stat().st_size,
                "fileMtimeNs": str(video.stat().st_mtime_ns), "manifestMtimeNs": str(manifest_path.stat().st_mtime_ns),
                "manifestSHA256": digest(manifest_path), "relativeDirectory": str(directory.relative_to(self.media)),
                "temporaryFiles": sorted(path.name for path in directory.iterdir() if path.name.endswith(".part")),
                "manifest": manifest}

    def processes(self):
        rows = self.run(["top", self.container, "-eo", "pid,comm,args"]).splitlines()[1:]
        media = []
        for row in rows:
            values = row.strip().split(None, 2)
            if len(values) != 3:
                continue
            pid, name, arguments = values
            try:
                executable = os.readlink(Path("/proc") / pid / "exe")
            except FileNotFoundError:
                continue
            executable_name = Path(executable.removesuffix(" (deleted)")).name
            # Production executes validated tool descriptors, so Linux comm may
            # be "3" or "4" rather than the original executable's basename.
            if executable_name not in ("ffmpeg", "ffprobe"):
                continue
            render_fds = []
            try:
                for fd in (Path("/proc") / pid / "fd").iterdir():
                    try:
                        target = os.readlink(fd)
                        if re.fullmatch(r"/dev/dri/renderD[0-9]+", target):
                            render_fds.append(target)
                    except (FileNotFoundError, PermissionError):
                        pass
            except FileNotFoundError:
                pass
            media.append({"pid": int(pid), "name": executable_name, "comm": name, "encoder": "libx264" in arguments and "-frames:v" in arguments,
                          "strictDolby": "apply_dolbyvision=1" in arguments and "strict_dolbyvision=1" in arguments and "libplacebo" in arguments,
                          "vulkan": "vulkan=gobyvk" in arguments, "renderNodes": sorted(set(render_fds))})
        return {"media": media, "encoding": any(item["encoder"] for item in media),
                "gpuEncoding": any(item["encoder"] and item["strictDolby"] and item["vulkan"] and item["renderNodes"] for item in media)}

    def probe(self, key):
        snapshot = self.snapshot(key)
        require(snapshot is not None, "No publication to probe")
        video = self.sidecar(key) / snapshot["generation"]
        inside = self.mount.rstrip("/") + "/" + str(video.relative_to(self.media))
        raw = self.run(["exec", "--user=10001:10001", self.container, self.ffprobe,
                       "-v", "error", "-count_frames", "-show_streams", "-show_format", "-of", "json", inside], timeout=60)
        value = json.loads(raw)
        value.get("format", {}).pop("filename", None)
        return value

    def restrict(self, key):
        directory = self.sidecar(key)
        require(self.snapshot(key) is not None, "A publication is required before restricting writes")
        record = self.evidence / ("restricted-" + key + ".json")
        mode = stat.S_IMODE(directory.stat().st_mode)
        with record.open("x") as handle:
            json.dump({"owner": OWNER, "path": str(directory), "mode": mode}, handle)
        directory.chmod(0o500)
        return {"restricted": True, "key": key}

    def restore(self, key):
        record = self.evidence / ("restricted-" + key + ".json")
        if not record.exists():
            return {"restored": False, "key": key}
        require(record.resolve() == record, "Invalid permission receipt")
        receipt = json.loads(record.read_text())
        directory = self.sidecar(key)
        require(receipt["owner"] == OWNER and receipt["path"] == str(directory), "Invalid permission receipt owner")
        directory.chmod(receipt["mode"])
        record.unlink()
        return {"restored": True, "key": key}

    def describe(self):
        fixtures = []
        for key in KEYS:
            fixture, source = self.fixture(key)
            require(digest(source) == fixture["sha256"], "A source fixture changed")
            fixtures.append({**fixture, "path": self.mount.rstrip("/") + "/" + str(source.relative_to(self.media))})
        return {"owner": OWNER, "container": self.container, "imageId": self.config["imageId"],
                "libraryPath": self.config["libraryPath"], "fixtures": fixtures}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("action", choices=["describe", "snapshot", "probe", "processes", "restrict", "restore", "restart"])
    parser.add_argument("--fixture", choices=KEYS)
    args = parser.parse_args()
    host = Host(args.root)
    if args.action in ("snapshot", "probe", "restrict", "restore"):
        require(args.fixture is not None, "A fixture key is required")
        value = getattr(host, args.action)(args.fixture)
    elif args.action == "restart":
        require(not host.processes()["media"], "Wait for owned media helpers before restarting")
        host.run(["restart", "--time=30", host.container], timeout=60)
        value = {"restarted": True, "container": host.container}
    else:
        value = getattr(host, args.action)()
    print(json.dumps(value))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        raise SystemExit(1)
