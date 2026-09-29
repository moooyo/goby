#!/usr/bin/env python3
"""Build and export the Linux amd64 software image from an embedded release.

Run on a Linux Docker host. This command never pushes an image, starts an
application container, prunes Docker storage, or changes an existing output.
"""

import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys


REPOSITORY = Path(__file__).resolve().parents[1]
RECIPE = REPOSITORY / "deploy" / "oci"
PATCHES = REPOSITORY / "scripts" / "test-env" / "toolchain-patches"


def need(condition, message):
    if not condition:
        raise ValueError(message)


def record(path):
    digest = hashlib.sha256()
    size = 0
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
            size += len(block)
    return {"bytes": size, "sha256": digest.hexdigest()}


def copy_file(source, destination):
    need(source.is_file() and not source.is_symlink(), f"Missing regular input: {source}")
    before = record(source)
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, destination)
    destination.chmod(0o644)
    need(record(source) == before == record(destination), f"Input changed: {source}")


def copy_tree(source, destination):
    need(source.is_dir() and not source.is_symlink(), f"Missing source directory: {source}")
    for entry in sorted(source.rglob("*")):
        need(not entry.is_symlink(), f"Symbolic link in build inputs: {entry}")
        if entry.is_file():
            copy_file(entry, destination / entry.relative_to(source))


def run(command, log):
    print(json.dumps({"command": command, "log": str(log)}), flush=True)
    with log.open("xb") as output:
        result = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, check=False)
    need(result.returncode == 0, f"Command exited {result.returncode}; inspect {log}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release-dir", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--image", required=True, help="Local tagged image reference")
    parser.add_argument("--revision", required=True, help="Verified application source commit")
    parser.add_argument("--jobs", type=int, choices=(1, 2), default=2)
    parser.add_argument("--prepare-only", action="store_true")
    args = parser.parse_args()
    need(sys.platform == "linux", "Build OCI images on Linux; project verification uses test-env.")
    need(re.fullmatch(r"[0-9a-f]{40}", args.revision), "Revision must be a full commit SHA.")
    need(re.fullmatch(r"[a-z0-9][a-z0-9._/-]*:[A-Za-z0-9_][A-Za-z0-9_.-]*", args.image),
         "Use an explicit local repository:tag image reference.")
    release = args.release_dir.resolve(strict=True)
    output = args.output_dir.absolute()
    need(not output.exists(), "Output already exists; use a fresh output directory.")
    output.parent.mkdir(parents=True, exist_ok=True)
    output = output.parent.resolve() / output.name
    for source in (release, RECIPE, PATCHES, REPOSITORY / "tools" / "intro-fingerprint",
                   REPOSITORY / "third_party" / "licenses"):
        need(not output.is_relative_to(source) and not source.is_relative_to(output),
             "Output and build input directories must not overlap.")
    output.mkdir(mode=0o700)
    context = output / "context"
    context.mkdir()

    for name in ("Dockerfile", ".dockerignore", "build-ffmpeg.sh", "build-fingerprint.sh",
                 "check-artifact.py", "entrypoint.sh", "source-pins.json"):
        copy_file(RECIPE / name, context / name)
    for name in ("goby", "manifest.json"):
        copy_file(release / name, context / name)
    for patch in ("ffmpeg-progress-copyts-nopts.patch", "ffmpeg-decoder-queue-wakeup.patch"):
        copy_file(PATCHES / patch, context / "toolchain-patches" / patch)
    for directory in ("progress-copyts-nopts", "decoder-queue-wakeup"):
        copy_tree(PATCHES / directory, context / "toolchain-patches" / directory)
    copy_tree(REPOSITORY / "tools" / "intro-fingerprint", context / "intro-fingerprint")
    notices = context / "application-notices"
    copy_file(REPOSITORY / "THIRD_PARTY_NOTICES.md", notices / "THIRD_PARTY_NOTICES.md")
    copy_file(REPOSITORY / "docs" / "development" / "third-party-notices-inventory.json",
              notices / "docs" / "development" / "third-party-notices-inventory.json")
    copy_tree(REPOSITORY / "third_party" / "licenses", notices / "third_party" / "licenses")

    binary = record(context / "goby")
    manifest = record(context / "manifest.json")
    # Keep the checker output outside the Docker context. The Docker artifact
    # stage repeats its input check before compiling the media toolchain.
    checked = output / "checked-release"
    checked.mkdir()
    for name in ("goby", "manifest.json"):
        copy_file(context / name, checked / name)
    run([sys.executable, str(context / "check-artifact.py"), str(checked),
         binary["sha256"], manifest["sha256"]], output / "artifact-check.log")
    inputs = [{"name": str(path.relative_to(context)), **record(path)}
              for path in sorted(context.rglob("*")) if path.is_file()]
    receipt = {"kind": "goby-oci-build", "version": 1, "status": "prepared",
               "applicationRevision": args.revision, "platform": "linux/amd64",
               "imageReference": args.image, "applicationBinary": binary,
               "applicationManifest": manifest, "contextFiles": inputs,
               "runtimeAccepted": False, "registryPublished": False}
    receipt_path = output / "build-receipt.json"

    def save_receipt():
        receipt_path.write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    save_receipt()
    if args.prepare_only:
        print(json.dumps({"status": "prepared", "output": str(output)}))
        return
    command = ["docker", "buildx", "build", "--platform", "linux/amd64", "--load",
               "--progress", "plain", "--build-arg", "GOBY_BINARY_SHA256=" + binary["sha256"],
               "--build-arg", "GOBY_BUILD_MANIFEST_SHA256=" + manifest["sha256"],
               "--build-arg", "GOBY_REVISION=" + args.revision,
               "--build-arg", "FFMPEG_BUILD_JOBS=" + str(args.jobs),
               "--tag", args.image, "--iidfile", str(output / "image-id.txt"), str(context)]
    try:
        run(command, output / "image-build.log")
        image_id = (output / "image-id.txt").read_text().strip()
        need(re.fullmatch(r"sha256:[0-9a-f]{64}", image_id), "Invalid built image identity.")
        details = json.loads(subprocess.check_output(["docker", "image", "inspect", image_id]))[0]
        need(details["Id"] == image_id and details["Os"] == "linux"
             and details["Architecture"] == "amd64", "Image platform or identity mismatch.")
        need(details["Config"]["User"] == "10001:10001", "Image must use the configured nonroot identity.")
        need(details["Config"].get("Labels", {}).get("org.opencontainers.image.revision") == args.revision,
             "Image revision label does not match the selected application source.")
        (output / "image-inspect.json").write_text(json.dumps(details, indent=2) + "\n", encoding="utf-8")
        archive = output / "goby-linux-amd64-image.tar"
        # Save the immutable image ID so a concurrent tag change cannot change
        # archive bytes. Consumers load it, then use the recorded image ID.
        run(["docker", "image", "save", "--output", str(archive), image_id], output / "image-save.log")
        receipt.update(status="built_and_exported", imageId=image_id,
                       archive={"name": archive.name, **record(archive)})
        (output / "SHA256SUMS").write_text(
            receipt["archive"]["sha256"] + "  " + archive.name + "\n", encoding="utf-8")
        for name in ("compose.yaml", "goby.env.example", "README.md", "source-pins.json"):
            copy_file(RECIPE / name, output / name)
        save_receipt()
        print(json.dumps({"status": receipt["status"], "imageId": image_id, "output": str(output)}))
    except BaseException as error:
        receipt.update(status="failed", failure=str(error))
        save_receipt()
        raise


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as failure:
        print(f"OCI build failed: {failure}", file=sys.stderr)
        sys.exit(1)
