#!/usr/bin/env python3
"""Build a pinned Linux amd64 AMD extension and export a Docker-loadable archive.

The software image, complete private library prefix, and matching libplacebo
headers are explicit inputs. This command never starts Goby or publishes images.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
RECIPE = ROOT / "deploy" / "oci"
PATCHES = ROOT / "scripts" / "test-env" / "toolchain-patches"
BASE = "sha256:ff9beeb782aea48dbb19630712b67824c6ef775c5886b7bac9ef856fb5c5dcab"


def need(value, message):
    if not value:
        raise ValueError(message)


def record(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return {"bytes": path.stat().st_size, "sha256": digest.hexdigest()}


def copy_file(source, destination):
    need(source.is_file() and not source.is_symlink(), f"Missing regular input: {source}")
    before = record(source)
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination)
    need(record(source) == before == record(destination), f"Changed input: {source}")


def copy_tree(source, destination):
    need(source.is_dir() and not source.is_symlink(), f"Missing input directory: {source}")
    for item in source.rglob("*"):
        if item.is_symlink():
            need(item.resolve(strict=True).is_relative_to(source), f"External input link: {item}")
        else:
            need(item.is_file() or item.is_dir(), f"Unsupported input: {item}")
    shutil.copytree(source, destination, symlinks=True)


def inventory(root):
    result = []
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            result.append({"name": path.relative_to(root).as_posix(), "link": os.readlink(path)})
        elif path.is_file():
            result.append({"name": path.relative_to(root).as_posix(), **record(path)})
    return result


def run(command, log):
    print(json.dumps({"command": command, "log": str(log)}), flush=True)
    with log.open("xb") as output:
        result = subprocess.run(command, stdout=output, stderr=subprocess.STDOUT, check=False)
    need(result.returncode == 0, f"Command exited {result.returncode}; inspect {log}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--toolchain", required=True, type=Path)
    parser.add_argument("--libplacebo-headers", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--image", required=True)
    parser.add_argument("--recipe-revision", required=True)
    parser.add_argument("--jobs", type=int, choices=(1, 2), default=2)
    parser.add_argument("--prepare-only", action="store_true")
    args = parser.parse_args()
    need(sys.platform == "linux", "Build this image on Linux; project verification uses test-env.")
    need(re.fullmatch(r"[0-9a-f]{40}", args.recipe_revision), "Use a full recipe source commit.")
    need(re.fullmatch(r"[a-z0-9][a-z0-9._/-]*:[A-Za-z0-9_][A-Za-z0-9_.-]*", args.image),
         "Use an explicit local repository:tag.")
    toolchain = args.toolchain.resolve(strict=True)
    headers = args.libplacebo_headers.resolve(strict=True)
    need((headers / "config.h").is_file(), "Select the installed libplacebo header directory.")
    output = args.output_dir.absolute()
    need(not output.exists(), "Output already exists; use a fresh directory.")
    output.parent.mkdir(parents=True, exist_ok=True)
    output = output.parent.resolve() / output.name
    for source in (toolchain, headers, RECIPE, PATCHES):
        need(not output.is_relative_to(source) and not source.is_relative_to(output),
             "Output must not overlap build inputs.")
    details = json.loads(subprocess.check_output(["docker", "image", "inspect", BASE]))[0]
    need(details["Id"] == BASE and details["Architecture"] == "amd64"
         and details["Os"] == "linux" and details["Config"]["User"] == "10001:10001",
         "The selected software base image is unavailable or does not match.")
    alias = "goby-amd-build-base:" + BASE.split(":")[1][:16]
    # The private tag is only a Dockerfile input; its actual identity is checked
    # before and after the build, and both IDs enter the receipt.
    existing = subprocess.run(["docker", "image", "inspect", alias], stdout=subprocess.PIPE,
                              stderr=subprocess.DEVNULL, check=False)
    if existing.returncode == 0:
        need(json.loads(existing.stdout)[0]["Id"] == BASE, "Private base tag has another owner.")
    else:
        subprocess.run(["docker", "image", "tag", BASE, alias], check=True)
    output.mkdir(mode=0o700)
    context = output / "context"
    context.mkdir()
    for name in ("Dockerfile.amd", "build-amd-ffmpeg.sh", "source-pins.amd.json"):
        copy_file(RECIPE / name, context / name)
    copy_tree(toolchain, context / "amd-toolchain")
    copy_tree(headers, context / "libplacebo-headers")
    copy_tree(PATCHES, context / "toolchain-patches")
    receipt = {"kind": "goby-oci-amd-build", "version": 1, "status": "prepared",
               "baseImageId": BASE, "baseImageAlias": alias, "recipeRevision": args.recipe_revision,
               "imageReference": args.image, "contextFiles": inventory(context),
               "runtimeAccepted": False, "registryPublished": False}
    receipt_path = output / "build-receipt.json"

    def save():
        receipt_path.write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    save()
    if args.prepare_only:
        return
    try:
        run(["docker", "buildx", "build", "--platform", "linux/amd64", "--load", "--progress", "plain",
             "--file", str(context / "Dockerfile.amd"), "--build-arg", "GOBY_AMD_BASE_IMAGE=" + alias,
             "--build-arg", "GOBY_AMD_RECIPE_REVISION=" + args.recipe_revision,
             "--build-arg", "FFMPEG_BUILD_JOBS=" + str(args.jobs), "--tag", args.image,
             "--iidfile", str(output / "image-id.txt"), str(context)], output / "image-build.log")
        need(json.loads(subprocess.check_output(["docker", "image", "inspect", alias]))[0]["Id"] == BASE,
             "The base tag changed during the build.")
        image_id = (output / "image-id.txt").read_text().strip()
        details = json.loads(subprocess.check_output(["docker", "image", "inspect", image_id]))[0]
        need(details["Architecture"] == "amd64" and details["Os"] == "linux"
             and details["Config"]["User"] == "10001:10001", "Wrong final platform or user.")
        need(details["Config"]["Labels"]["io.goby.oci.base-image-id"] == BASE, "Wrong base label.")
        need(details["Config"]["Labels"]["io.goby.oci.amd-recipe-revision"] == args.recipe_revision,
             "Wrong recipe label.")
        (output / "image-inspect.json").write_text(json.dumps(details, indent=2) + "\n", encoding="utf-8")
        archive = output / "goby-linux-amd64-amd-image.tar"
        run(["docker", "image", "save", "--output", str(archive), image_id], output / "image-save.log")
        receipt.update(status="built_and_exported", imageId=image_id,
                       archive={"name": archive.name, **record(archive)})
        (output / "SHA256SUMS").write_text(receipt["archive"]["sha256"] + "  " + archive.name + "\n", encoding="utf-8")
        for name in ("compose.yaml", "compose.amd.yaml", "compose.subtitles.yaml", "goby.env.example",
                     "README.md", "README.amd.md", "README.providers.md",
                     "source-pins.amd.json", "seccomp.amd.json"):
            copy_file(RECIPE / name, output / name)
        save()
        print(json.dumps({"status": receipt["status"], "imageId": image_id, "output": str(output)}))
    except BaseException as error:
        receipt.update(status="failed", failure=str(error))
        save()
        raise


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"AMD image build failed: {error}", file=sys.stderr)
        sys.exit(1)
