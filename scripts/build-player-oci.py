#!/usr/bin/env python3
"""Build and export the standalone player with digest-pinned Node and nginx.

Run on Linux. Inputs are frozen into a fresh context; no Go source is embedded.
The output includes an immutable image archive, bundle and notice inventories,
and a build receipt. This command never starts an application or pushes images.
"""

import argparse
import hashlib
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
import uuid


REPOSITORY = Path(__file__).resolve().parents[1]
PLAYER = REPOSITORY / "web" / "player"
RECIPE = REPOSITORY / "deploy" / "oci"
SOURCE_FILES = (".dockerignore", "Dockerfile", "package.json", "package-lock.json", "index.html",
                "tsconfig.json", "vite.config.ts", "nginx.conf", "build-notices.mjs", "THIRD-PARTY.md")
MAXIMUM_INPUT = 32 * 1024 * 1024
MAXIMUM_CONTEXT = 128 * 1024 * 1024
PIN = re.compile(r"[a-z0-9][a-z0-9._:/-]*@sha256:[0-9a-f]{64}")


def need(value, message):
    if not value:
        raise ValueError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def sha256(value):
    return hashlib.sha256(value).hexdigest()


def record(path, maximum=None):
    before = path.lstat()
    need(stat.S_ISREG(before.st_mode) and path.resolve(strict=True) == path,
         f"Expected an unlinked regular input: {path}")
    need(maximum is None or before.st_size <= maximum, f"Input exceeds its size limit: {path}")
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
            size += len(block)
    after = path.lstat()
    fields = ("st_dev", "st_ino", "st_size", "st_mtime_ns", "st_ctime_ns", "st_mode")
    need(size == before.st_size and all(getattr(before, key) == getattr(after, key) for key in fields),
         f"Input changed while being read: {path}")
    return {"bytes": size, "sha256": digest.hexdigest()}


def copy_file(source, destination, epoch=None):
    before = record(source, MAXIMUM_INPUT)
    destination.parent.mkdir(parents=True, exist_ok=True)
    with source.open("rb") as stream, destination.open("xb") as output:
        shutil.copyfileobj(stream, output)
    destination.chmod(0o644)
    if epoch is not None:
        os.utime(destination, (epoch, epoch))
    need(record(source, MAXIMUM_INPUT) == before == record(destination, MAXIMUM_INPUT), f"Input changed: {source}")


def inventory(root):
    need(root.is_dir() and root.resolve(strict=True) == root, f"Expected an unlinked directory: {root}")
    result = []
    for path in sorted(root.rglob("*")):
        need(not path.is_symlink(), f"Linked directory member is unsupported: {path}")
        if path.is_dir():
            continue
        result.append({"name": path.relative_to(root).as_posix(), **record(path, MAXIMUM_INPUT)})
    need(sum(entry["bytes"] for entry in result) <= MAXIMUM_CONTEXT, "Input inventory exceeds its size limit.")
    return result


def source_inventory(source):
    entries = [{"name": name, **record(source / name, MAXIMUM_INPUT)} for name in SOURCE_FILES]
    for directory in ("src", "public"):
        path = source / directory
        if directory == "public" and not path.exists():
            continue
        entries.extend({**entry, "name": directory + "/" + entry["name"]} for entry in inventory(path))
    need(any(entry["name"].startswith("src/") for entry in entries), "Player sources are empty.")
    need(sum(entry["bytes"] for entry in entries) <= MAXIMUM_CONTEXT, "Player build inputs exceed their size limit.")
    return sorted(entries, key=lambda entry: entry["name"])


class EntryParser(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.references = []

    def handle_starttag(self, tag, attrs):
        if tag not in ("script", "link"):
            return
        values = dict(attrs)
        need(len(values) == len(attrs), "Duplicate player entry attributes are unsupported.")
        if tag == "script":
            need(values.get("type") == "module", "Player entry scripts must be Vite modules.")
            self.references.append(("module-script", values.get("src")))
        else:
            selected = set((values.get("rel") or "").split()) & {"stylesheet", "modulepreload"}
            need(len(selected) <= 1, "Ambiguous player entry link relation.")
            if selected:
                self.references.append((next(iter(selected)), values.get("href")))


def bundle_inventory(root):
    files = inventory(root)
    by_name = {entry["name"]: entry for entry in files}
    need(by_name.get("index.html", {}).get("bytes", 0) > 0, "Player bundle lacks index.html.")
    parser = EntryParser()
    parser.feed((root / "index.html").read_text(encoding="utf-8"))
    parser.close()
    entries = []
    for kind, url in parser.references:
        need(isinstance(url, str) and re.fullmatch(r"/assets/[A-Za-z0-9_.-]+\.(?:js|css)", url),
             f"Player {kind} does not reference a local production asset: {url}")
        need(url.endswith(".css" if kind == "stylesheet" else ".js"), "Unexpected player entry asset type.")
        asset = by_name.get(url[1:])
        need(asset is not None and asset["bytes"] > 0, f"Missing player entry asset: {url}")
        entries.append({"kind": kind, "url": url, **asset})
    need(any(entry["kind"] == "module-script" for entry in entries), "No player Vite module entry was found.")
    return {"files": files, "entryReferences": entries}


def run(command, log):
    print(json.dumps({"command": command, "log": str(log)}), flush=True)
    with log.open("xb") as output:
        result = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, check=False)
    need(result.returncode == 0, f"Command exited {result.returncode}; inspect {log}")


def save_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def image_readback(image_id, output, source, expected_lock):
    name = "goby-player-package-" + uuid.uuid4().hex
    container_id = subprocess.check_output(["docker", "create", "--name", name, "--network", "none",
        "--label", "io.goby.package-readback=" + name, "--entrypoint", "/bin/true", image_id], text=True).strip()
    need(re.fullmatch(r"[0-9a-f]{64}", container_id), "Invalid readback container identity.")
    try:
        run(["docker", "cp", container_id + ":/usr/share/nginx/html", str(output / "player-dist")],
            output / "bundle-readback.log")
        run(["docker", "cp", container_id + ":/usr/share/doc/goby-player", str(output / "player-notices")],
            output / "notices-readback.log")
        run(["docker", "cp", container_id + ":/etc/nginx/templates/nginx.conf.template", str(output / "nginx.conf")],
            output / "nginx-readback.log")
        need(record(output / "nginx.conf") == record(source / "nginx.conf"), "Packaged nginx template differs from its source.")
        assets = bundle_inventory(output / "player-dist")
        notices = inventory(output / "player-notices")
        manifest = json.loads((output / "player-notices" / "manifest.json").read_text(encoding="utf-8"))
        need(manifest.get("kind") == "goby-player-package-notices" and manifest.get("version") == 1
             and manifest.get("packageLock") == expected_lock and len(manifest.get("packages", [])) > 0,
             "Packaged notices do not match the selected player lockfile.")
        actual = {entry["name"]: entry for entry in notices}
        for package in manifest["packages"]:
            for item in package["files"]:
                need(actual.get(item["name"]) == item, f"Packaged license content changed: {item['name']}")
        return {"assets": assets, "notices": notices, "noticePackages": manifest["packages"]}
    finally:
        details = json.loads(subprocess.check_output(["docker", "container", "inspect", container_id]))[0]
        need(details["Name"] == "/" + name and details["Image"] == image_id and not details["State"]["Running"]
             and details["Config"]["Labels"].get("io.goby.package-readback") == name,
             "Readback container ownership or stopped state changed; it was retained for inspection.")
        subprocess.run(["docker", "container", "rm", container_id], check=True, stdout=subprocess.DEVNULL)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-dir", type=Path, default=PLAYER)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--image", required=True, help="Local repository:tag for the completed player")
    parser.add_argument("--revision", required=True, help="Full source commit SHA for the selected release")
    parser.add_argument("--node-image", required=True, help="Node image reference ending in @sha256:<digest>")
    parser.add_argument("--nginx-image", required=True, help="nginx-unprivileged reference ending in @sha256:<digest>")
    parser.add_argument("--source-date-epoch", type=int, required=True, help="Fixed build timestamp, in Unix seconds")
    parser.add_argument("--prepare-only", action="store_true")
    args = parser.parse_args()
    need(sys.platform == "linux", "Build player images on Linux; project verification uses test-env.")
    need(re.fullmatch(r"[0-9a-f]{40}", args.revision), "Revision must be a full commit SHA.")
    need(re.fullmatch(r"[a-z0-9][a-z0-9._/-]*:[A-Za-z0-9_][A-Za-z0-9_.-]*", args.image),
         "Use an explicit local repository:tag image reference.")
    need(PIN.fullmatch(args.node_image) and PIN.fullmatch(args.nginx_image), "Node and nginx images must be digest-pinned.")
    need(0 <= args.source_date_epoch <= 4102444800, "Invalid fixed build timestamp.")
    source = args.source_dir.absolute()
    need(source.resolve(strict=True) == source, "The player source directory cannot have symbolic-link components.")
    inputs = source_inventory(source)
    source_digest = sha256(canonical(inputs))
    output = args.output_dir.absolute()
    need(not output.exists(), "Output already exists; choose a fresh output directory.")
    for directory in (source, RECIPE, REPOSITORY / "scripts"):
        need(not output.is_relative_to(directory) and not directory.is_relative_to(output), "Output and build inputs must not overlap.")
    output.parent.mkdir(parents=True, exist_ok=True)
    output = output.parent.resolve() / output.name
    for directory in (source, RECIPE, REPOSITORY / "scripts"):
        need(not output.is_relative_to(directory) and not directory.is_relative_to(output), "Output and build inputs must not overlap.")
    output.mkdir(mode=0o700)
    context = output / "context"
    context.mkdir()
    for entry in inputs:
        copy_file(source / entry["name"], context / entry["name"], args.source_date_epoch)
    need(inventory(context) == inputs == source_inventory(source), "Player sources changed while freezing the build context.")
    for directory in sorted(context.rglob("*"), reverse=True):
        if directory.is_dir():
            os.utime(directory, (args.source_date_epoch, args.source_date_epoch))
    os.utime(context, (args.source_date_epoch, args.source_date_epoch))
    receipt = {"kind": "goby-player-oci-build", "version": 1, "status": "prepared", "platform": "linux/amd64",
        "sourceRevision": args.revision, "sourceDateEpoch": args.source_date_epoch,
        "sourceInventory": {"files": inputs, "sha256": source_digest, "encoding": "UTF-8 canonical JSON: sorted keys, compact separators, ASCII escapes."},
        "builder": {"name": "scripts/build-player-oci.py", **record(Path(__file__).resolve())},
        "baseImages": {"node": args.node_image, "nginx": args.nginx_image}, "imageReference": args.image,
        "runtimeAccepted": False, "registryPublished": False, "embeddedInGo": False,
        "boundary": "A build receipt does not establish deployment or browser acceptance."}
    receipt_path = output / "build-receipt.json"
    save_json(receipt_path, receipt)
    if args.prepare_only:
        print(json.dumps({"status": "prepared", "output": str(output), "sourceSha256": source_digest}))
        return
    command = ["docker", "buildx", "build", "--platform", "linux/amd64", "--load", "--progress", "plain",
        "--provenance=false", "--sbom=false", "--build-arg", "NODE_IMAGE=" + args.node_image,
        "--build-arg", "NGINX_IMAGE=" + args.nginx_image,
        "--build-arg", "SOURCE_DATE_EPOCH=" + str(args.source_date_epoch),
        "--build-arg", "GOBY_PLAYER_REVISION=" + args.revision,
        "--build-arg", "GOBY_PLAYER_SOURCE_SHA256=" + source_digest,
        "--tag", args.image, "--iidfile", str(output / "image-id.txt"), str(context)]
    receipt["buildCommand"] = command
    try:
        run(command, output / "image-build.log")
        need(inventory(context) == inputs == source_inventory(source), "Player sources changed during the image build.")
        image_id = (output / "image-id.txt").read_text(encoding="ascii").strip()
        need(re.fullmatch(r"sha256:[0-9a-f]{64}", image_id), "Invalid built image identity.")
        details = json.loads(subprocess.check_output(["docker", "image", "inspect", image_id]))[0]
        labels = details["Config"].get("Labels", {})
        need(details["Id"] == image_id and details["Os"] == "linux" and details["Architecture"] == "amd64"
             and details["Config"]["User"] == "101:101", "Player image platform, identity or user mismatch.")
        need(labels.get("org.opencontainers.image.revision") == args.revision
             and labels.get("io.goby.player.source.sha256") == source_digest, "Player image source labels do not match.")
        save_json(output / "image-inspect.json", details)
        lock = next({key: item[key] for key in ("bytes", "sha256")} for item in inputs if item["name"] == "package-lock.json")
        readback = image_readback(image_id, output, context, lock)
        save_json(output / "player-bundle-manifest.json", {"kind": "goby-player-bundle", "version": 1, **readback})
        archive = output / "goby-player-linux-amd64-image.tar"
        run(["docker", "image", "save", "--output", str(archive), image_id], output / "image-save.log")
        for name in ("compose.player.yaml", "README.player.md"):
            copy_file(RECIPE / name, output / name)
        need(inventory(context) == inputs == source_inventory(source), "Player build inputs changed before export completed.")
        receipt.update(status="built_and_exported", imageId=image_id, archive={"name": archive.name, **record(archive)},
            bundleManifest={"name": "player-bundle-manifest.json", **record(output / "player-bundle-manifest.json")},
            sourceInputsUnchanged=True, imageFilesReadBack=True,
            companions=[{"name": name, **record(output / name)} for name in ("compose.player.yaml", "README.player.md")])
        save_json(receipt_path, receipt)
        checksum_files = [archive.name, "build-receipt.json", "player-bundle-manifest.json", "compose.player.yaml", "README.player.md"]
        (output / "SHA256SUMS").write_text("".join(record(output / name)["sha256"] + "  " + name + "\n" for name in checksum_files), encoding="ascii")
        print(json.dumps({"status": receipt["status"], "imageId": image_id, "archive": receipt["archive"], "output": str(output)}))
    except BaseException as error:
        receipt.update(status="failed", failure=str(error))
        save_json(receipt_path, receipt)
        raise


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as failure:
        print(f"Player OCI build failed: {failure}", file=sys.stderr)
        sys.exit(1)
