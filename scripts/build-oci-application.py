#!/usr/bin/env python3
"""Export an application update over an explicit immutable Linux amd64 runtime.

This keeps inherited toolchain evidence and pins every selected executable.
It never starts Goby, publishes images, or marks runtime acceptance complete.
"""

import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import uuid


ROOT = Path(__file__).resolve().parents[1]
RECIPE = ROOT / "deploy" / "oci"
ANALYSIS_SHA256 = "e8a8d46f5225f3062cec7c07fb145d58ae73c603cb740dcd5bad34bfb54e455a"


def need(value, message):
    if not value:
        raise ValueError(message)


def record(path):
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
            size += len(block)
    return {"bytes": size, "sha256": digest.hexdigest()}


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def copy_file(source, destination):
    need(source.is_file() and not source.is_symlink(), f"Missing regular input: {source}")
    before = record(source)
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, destination)
    destination.chmod(0o644)
    need(record(source) == before == record(destination), f"Input changed: {source}")


def copy_tree(source, destination):
    need(source.is_dir() and not source.is_symlink(), f"Missing input directory: {source}")
    for entry in sorted(source.rglob("*")):
        need(not entry.is_symlink(), f"Symbolic link in build inputs: {entry}")
        if entry.is_file():
            copy_file(entry, destination / entry.relative_to(source))


def run(command, log):
    print(json.dumps({"command": command, "log": str(log)}), flush=True)
    with log.open("xb") as output:
        result = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, check=False)
    need(result.returncode == 0, f"Command exited {result.returncode}; inspect {log}")


def source_archive(archive, receipt_path, revision):
    receipt = json.loads(receipt_path.read_text(encoding="utf-8"))
    need(receipt.get("kind") == "goby-source-archive" and receipt.get("version") == 1
         and receipt.get("revision") == revision, "Source receipt revision or kind mismatch.")
    before = record(archive)
    need(receipt.get("archive") == {"name": archive.name, **before}, "Source archive binding mismatch.")
    files = {}
    with tarfile.open(archive, "r:") as stream:
        for entry in stream:
            path = PurePosixPath(entry.name)
            need(not path.is_absolute() and ".." not in path.parts
                 and str(path) == entry.name.rstrip("/"), "Noncanonical source archive path.")
            if entry.isdir():
                continue
            need(entry.isfile() and entry.name not in files, "Source archive requires unique regular files.")
            digest = hashlib.sha256()
            size = 0
            with stream.extractfile(entry) as content:
                for block in iter(lambda: content.read(1024 * 1024), b""):
                    digest.update(block)
                    size += len(block)
            files[entry.name] = {"bytes": size, "sha256": digest.hexdigest()}
        need(stream.pax_headers.get("comment") == revision, "Git archive commit marker mismatch.")
    need(record(archive) == before, "Source archive changed while reading.")
    return receipt, files


def bind_manifest_source(manifest, archive_files):
    inventory = manifest.get("sourceInventory", {})
    entries = inventory.get("files")
    need(isinstance(entries, list) and entries, "Release manifest has no source inventory.")
    names = set()
    for entry in entries:
        name = entry.get("name")
        need(isinstance(name, str) and name not in names, "Duplicate or invalid manifest source path.")
        names.add(name)
        need(archive_files.get(name) == {"bytes": entry.get("bytes"), "sha256": entry.get("sha256")},
             f"Release source does not match the archive: {name}")
    expected = {name for name in archive_files
                if name.startswith(("cmd/", "internal/")) or name == "web/admin/embedded.go"}
    need(names == expected, "Release source inventory does not cover the archive's application scope.")
    for name in ("go.mod", "go.sum"):
        need(manifest.get("moduleInputs", {}).get(name) == archive_files.get(name),
             f"Release module input does not match the archive: {name}")


def bind_repository_inputs(paths, archive_files):
    for path in paths:
        name = path.relative_to(ROOT).as_posix()
        need(record(path) == archive_files.get(name), f"Recipe or notice differs from source archive: {name}")


def inspect_image(image_id):
    value = json.loads(subprocess.check_output(["docker", "image", "inspect", image_id]))[0]
    need(value["Id"] == image_id and value["Os"] == "linux" and value["Architecture"] == "amd64"
         and value["Config"]["User"] == "10001:10001", "Base image identity, platform, or user mismatch.")
    return value


def tool_paths(details, profile):
    environment = dict(value.split("=", 1) for value in details["Config"]["Env"])
    prefix = "/opt/goby-amd-ffmpeg" if profile == "amd" else "/opt/ffmpeg/9.0.1"
    expected = {"GOBY_FFMPEG": prefix + "/bin/ffmpeg", "GOBY_FFPROBE": prefix + "/bin/ffprobe",
                "GOBY_PG_DUMP": "/usr/lib/postgresql/17/bin/pg_dump",
                "GOBY_PG_RESTORE": "/usr/lib/postgresql/17/bin/pg_restore"}
    need(all(environment.get(key) == value for key, value in expected.items()), "Unexpected base tool paths.")
    result = list(expected.values()) + ["/opt/goby-intro-fingerprint/bin/goby-intro-fingerprint"]
    if profile == "amd":
        result.extend(["/opt/goby-amd-ffmpeg/lib/libplacebo.so.351",
                       "/usr/lib/x86_64-linux-gnu/dri/radeonsi_drv_video.so",
                       "/usr/lib/x86_64-linux-gnu/libvulkan_radeon.so"])
    return result


def image_files(image_id, paths):
    # A stopped, uniquely named container permits filesystem inspection without
    # executing its entrypoint or trusting stale inherited checksum records.
    name = "goby-application-inspect-" + uuid.uuid4().hex
    container = subprocess.check_output(["docker", "create", "--name", name,
                                        "--label", "goby.owner=" + name, image_id]).decode().strip()
    result = {}
    try:
        with tempfile.TemporaryDirectory(prefix=name + "-") as temporary:
            for index, path in enumerate(paths):
                target = Path(temporary) / str(index)
                subprocess.run(["docker", "cp", "-L", container + ":" + path, str(target)], check=True,
                               stdout=subprocess.DEVNULL)
                need(target.is_file() and not target.is_symlink(), f"Image input is not a regular file: {path}")
                result[path] = {**record(target), "content": target.read_bytes() if target.stat().st_size <= 16 << 20 else None}
    finally:
        subprocess.run(["docker", "rm", container], check=True, stdout=subprocess.DEVNULL)
    return result


def bind_analysis_configuration(content, pins):
    fingerprint = "/opt/goby-intro-fingerprint/bin/goby-intro-fingerprint"
    expected = {"enabled": True, "cacheDirectory": "/var/cache/goby/analysis",
                "fingerprintPath": fingerprint, "fingerprintSHA256": pins[fingerprint],
                "introFFmpegPath": "/usr/bin/ffmpeg", "introFFmpegSHA256": ANALYSIS_SHA256}
    need(json.loads(content) == expected, "Output media analysis configuration does not bind the selected tools.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release-dir", required=True, type=Path)
    parser.add_argument("--source-archive", required=True, type=Path)
    parser.add_argument("--source-receipt", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--base-image", required=True)
    parser.add_argument("--image", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--profile", required=True, choices=("software", "amd"))
    parser.add_argument("--install-analysis-ffmpeg", action="store_true")
    parser.add_argument("--prepare-only", action="store_true")
    args = parser.parse_args()
    need(sys.platform == "linux", "Build images on Linux; project verification uses test-env.")
    need(re.fullmatch(r"[0-9a-f]{40}", args.revision), "Use a full source commit.")
    need(re.fullmatch(r"sha256:[0-9a-f]{64}", args.base_image), "Use an immutable local base image ID.")
    need(re.fullmatch(r"[a-z0-9][a-z0-9._/-]*:[A-Za-z0-9_][A-Za-z0-9_.-]*", args.image),
         "Use an explicit local repository:tag.")
    release = args.release_dir.resolve(strict=True)
    archive = args.source_archive.resolve(strict=True)
    source_receipt = args.source_receipt.resolve(strict=True)
    output = args.output_dir.absolute()
    need(not output.exists(), "Output already exists; use a fresh output directory.")
    output.parent.mkdir(parents=True, exist_ok=True)
    output = output.parent.resolve() / output.name
    for path in (release, RECIPE, ROOT / "scripts", ROOT / "third_party" / "licenses", archive, source_receipt):
        need(not output.is_relative_to(path) and not path.is_relative_to(output), "Output and inputs must not overlap.")
    source, archive_files = source_archive(archive, source_receipt, args.revision)
    manifest = json.loads((release / "manifest.json").read_text(encoding="utf-8"))
    bind_manifest_source(manifest, archive_files)
    inputs = [RECIPE / name for name in ("Dockerfile.application", "configure-application.sh", "check-artifact.py")]
    inputs.extend([Path(__file__).resolve(), ROOT / "THIRD_PARTY_NOTICES.md",
                   ROOT / "docs/development/third-party-notices-inventory.json"])
    inputs.extend(path for path in (ROOT / "third_party/licenses").rglob("*") if path.is_file())
    bind_repository_inputs(inputs, archive_files)
    base = inspect_image(args.base_image)
    paths = tool_paths(base, args.profile)
    if not args.install_analysis_ffmpeg:
        paths.append("/usr/bin/ffmpeg")
    inherited = image_files(args.base_image, paths)
    inherited = {name: {key: value[key] for key in ("bytes", "sha256")} for name, value in inherited.items()}
    if not args.install_analysis_ffmpeg:
        need(inherited["/usr/bin/ffmpeg"]["sha256"] == ANALYSIS_SHA256, "Distribution analysis FFmpeg pin mismatch.")
    output.mkdir(mode=0o700)
    context = output / "context"
    context.mkdir()
    for name in ("Dockerfile.application", "configure-application.sh", "check-artifact.py"):
        copy_file(RECIPE / name, context / name)
    for name in ("goby", "manifest.json"):
        copy_file(release / name, context / name)
    binary, manifest_record = record(context / "goby"), record(context / "manifest.json")
    run([sys.executable, str(context / "check-artifact.py"), str(context), binary["sha256"], manifest_record["sha256"]],
        output / "artifact-check.log")
    notices = context / "application-notices"
    copy_file(ROOT / "THIRD_PARTY_NOTICES.md", notices / "THIRD_PARTY_NOTICES.md")
    copy_file(ROOT / "docs/development/third-party-notices-inventory.json", notices / "docs/development/third-party-notices-inventory.json")
    copy_tree(ROOT / "third_party/licenses", notices / "third_party/licenses")
    write_json(context / "application-source.json", {"kind": "goby-application-source", "version": 1,
               "revision": args.revision, "sourceArchive": source["archive"], "baseImageId": args.base_image,
               "inheritedTools": inherited, "analysisFFmpeg": {"path": "/usr/bin/ffmpeg", "sha256": ANALYSIS_SHA256,
               "packageVersion": "7:7.1.5-0+deb13u1", "snapshot": "20260915T000000Z"}})
    pins = {name: value["sha256"] for name, value in inherited.items()}
    pins.update({"/usr/local/bin/goby": binary["sha256"], "/usr/bin/ffmpeg": ANALYSIS_SHA256})
    (context / "expected-runtime-executables.sha256").write_text(
        "".join(f"{digest}  {name}\n" for name, digest in sorted(pins.items())), encoding="utf-8")
    receipt = {"kind": "goby-oci-application-build", "version": 1, "status": "prepared",
               "applicationRevision": args.revision, "sourceArchive": source["archive"],
               "profile": args.profile, "baseImageId": args.base_image, "imageReference": args.image,
               "applicationBinary": binary, "applicationManifest": manifest_record, "inheritedTools": inherited,
               "installAnalysisFFmpeg": args.install_analysis_ffmpeg, "runtimeAccepted": False, "registryPublished": False,
               "contextFiles": [{"name": path.relative_to(context).as_posix(), **record(path)}
                                for path in sorted(context.rglob("*")) if path.is_file()]}
    receipt_path = output / "build-receipt.json"
    write_json(receipt_path, receipt)
    if args.prepare_only:
        return
    alias = "goby-application-base:" + args.base_image.split(":")[1]
    existing = subprocess.run(["docker", "image", "inspect", alias], capture_output=True, check=False)
    if existing.returncode == 0:
        need(json.loads(existing.stdout)[0]["Id"] == args.base_image, "Private base alias has a different owner.")
    else:
        subprocess.run(["docker", "image", "tag", args.base_image, alias], check=True)
    try:
        run(["docker", "buildx", "build", "--platform", "linux/amd64", "--load", "--progress", "plain",
             "--file", str(context / "Dockerfile.application"), "--build-arg", "BASE=" + alias,
             "--build-arg", "BASE_IMAGE_ID=" + args.base_image, "--build-arg", "REVISION=" + args.revision,
             "--build-arg", "APPLICATION_SHA256=" + binary["sha256"], "--build-arg",
             "INSTALL_ANALYSIS_FFMPEG=" + str(int(args.install_analysis_ffmpeg)), "--tag", args.image,
             "--iidfile", str(output / "image-id.txt"), str(context)], output / "image-build.log")
        need(json.loads(subprocess.check_output(["docker", "image", "inspect", alias]))[0]["Id"] == args.base_image,
             "Base alias changed during the build.")
        image_id = (output / "image-id.txt").read_text().strip()
        need(re.fullmatch(r"sha256:[0-9a-f]{64}", image_id), "Invalid output image identity.")
        details = inspect_image(image_id)
        labels = details["Config"].get("Labels", {})
        need(labels.get("org.opencontainers.image.revision") == args.revision
             and labels.get("io.goby.application.sha256") == binary["sha256"]
             and labels.get("io.goby.application.base-image-id") == args.base_image, "Output image labels mismatch.")
        write_json(output / "image-inspect.json", details)
        copied = image_files(image_id, list(pins) + ["/usr/share/goby/media-analysis.json",
                             "/usr/share/goby/build-manifest.json", "/usr/share/goby/runtime-executables.sha256"])
        need(all(copied[name]["sha256"] == digest for name, digest in pins.items()), "Output tool or application pin mismatch.")
        need(copied["/usr/share/goby/build-manifest.json"]["sha256"] == manifest_record["sha256"], "Output manifest mismatch.")
        bind_analysis_configuration(copied["/usr/share/goby/media-analysis.json"]["content"], pins)
        for name in ("media-analysis.json", "runtime-executables.sha256"):
            (output / name).write_bytes(copied["/usr/share/goby/" + name]["content"])
        receipt["runtimeFiles"] = {name: {key: value[key] for key in ("bytes", "sha256")} for name, value in copied.items()}
        filename = "goby-linux-amd64" + ("-amd" if args.profile == "amd" else "") + "-image.tar"
        exported = output / filename
        run(["docker", "image", "save", "--output", str(exported), image_id], output / "image-save.log")
        receipt.update(status="built_and_exported", imageId=image_id, archive={"name": filename, **record(exported)})
        (output / "SHA256SUMS").write_text(receipt["archive"]["sha256"] + "  " + filename + "\n", encoding="utf-8")
        write_json(receipt_path, receipt)
        print(json.dumps({"status": receipt["status"], "imageId": image_id, "output": str(output)}))
    except BaseException as failure:
        receipt.update(status="failed", failure=str(failure))
        write_json(receipt_path, receipt)
        raise


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError, tarfile.TarError) as error:
        print(f"Application image build failed: {error}", file=sys.stderr)
        sys.exit(1)
