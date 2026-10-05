#!/usr/bin/env python3
"""Download pinned, attributed Dolby Vision source videos into a new directory."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import shutil
import sys
from urllib.parse import quote
from urllib.request import Request, urlopen


REVISION = "957c53ac160e11ae581aa1f4fe811a8ad6cf27df"
REPOSITORY = "https://github.com/DolbyLaboratories/dolby-vision-contents"
OWNER = "goby-dv-profiles-fixtures-20261005-165c"
LICENSE_PATH = "SolLevante_Netflix/SolLevante by Netflix.docx"
LICENSE_SIZE = 14351
LICENSE_SHA256 = "4b6690d1e90d9ca86659a382d2cf5aa7218a2b5abac1154bcdd10f56924dc37a"
LICENSE_DOWNLOAD_URL = (
    "https://api.github.com/repos/DolbyLaboratories/dolby-vision-contents/contents/"
    f"{quote(LICENSE_PATH, safe='/')}?ref={REVISION}"
)
SOURCES = {
    "p5": {
        "upstream_path": "SolLevante_Netflix/BL_RPU_dvhe-05_1920x1080@24fps_0_6313.mp4",
        "filename": "sollevante-p5-1080p.mp4",
        "size_bytes": 141871521,
        "sha256": "87fe0115f3002a621d2380a9f91852ef91a15854a446efe26de8744f77ef5346",
        "declared_profile": "5",
    },
    "p84": {
        "upstream_path": "SolLevante_Netflix/BL_RPU_dvhe-08-84_1920x1080@24fps_0_6313.mp4",
        "filename": "sollevante-p84-1080p.mp4",
        "size_bytes": 143530356,
        "sha256": "d81d4c17958946796f30ca28a57776cee187a421649058767c8496cbc1e469bf",
        "declared_profile": "8.4",
    },
}


def upstream_url(path: str, *, media: bool = False) -> str:
    host = "media.githubusercontent.com/media" if media else "raw.githubusercontent.com"
    return f"https://{host}/DolbyLaboratories/dolby-vision-contents/{REVISION}/{quote(path, safe='/')}"


def fetch_verified(url: str, target: Path, size: int, digest: str, *, accept: str | None = None) -> dict:
    """Bound each download and publish it only after its size and SHA-256 match."""
    partial = target.with_name(target.name + ".part")
    received = 0
    checksum = hashlib.sha256()
    headers = {"User-Agent": "Goby-Dolby-Vision-Fixtures/1.0"}
    if accept:
        headers["Accept"] = accept
    request = Request(url, headers=headers)
    try:
        with partial.open("xb") as output, urlopen(request, timeout=60) as response:
            while chunk := response.read(1024 * 1024):
                received += len(chunk)
                if received > size:
                    raise ValueError(f"Download exceeds pinned size: {url}")
                output.write(chunk)
                checksum.update(chunk)
        actual_digest = checksum.hexdigest()
        if received != size or actual_digest != digest:
            raise ValueError(
                f"Source mismatch for {url}: size={received}, sha256={actual_digest}"
            )
        if target.exists():
            raise FileExistsError(target)
        partial.rename(target)
    except BaseException:
        partial.unlink(missing_ok=True)
        raise
    return {
        "url": url,
        "absolute_path": str(target),
        "filename": target.name,
        "size_bytes": received,
        "sha256": actual_digest,
    }


def write_json(path: Path, value: dict) -> None:
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path, help="A new absolute output directory")
    parser.add_argument("--profile", choices=("p5", "p84", "both"), default="p5")
    args = parser.parse_args()
    requested = args.output
    if not requested.is_absolute():
        parser.error("--output must be absolute")
    if requested.exists() or requested.is_symlink():
        parser.error("--output must not already exist; existing files are never reused")
    if not requested.parent.is_dir():
        parser.error("The output parent directory must already exist")
    output = requested.resolve()
    selected = tuple(SOURCES) if args.profile == "both" else (args.profile,)
    required_bytes = sum(SOURCES[key]["size_bytes"] for key in selected) + LICENSE_SIZE + 128 * 1024 * 1024
    if shutil.disk_usage(output.parent).free < required_bytes:
        parser.error(f"Insufficient free space; need at least {required_bytes} bytes")

    output.mkdir(mode=0o755, exist_ok=False)
    (output / ".owner").write_text(OWNER + "\n", encoding="utf-8")
    attribution = (
        "SolLevante by Netflix, Inc., modified by Dolby, Inc.\n"
        "Licensed under Creative Commons Attribution 4.0 International (CC BY 4.0).\n"
        "https://creativecommons.org/licenses/by/4.0/\n"
        f"Source: {REPOSITORY}/tree/{REVISION}\n"
        "The downloaded MP4 files are unchanged copies of the Dolby modifications.\n"
        "If creating clips, resized images, or other derivatives, record those changes and retain attribution.\n"
        "No endorsement by Netflix or Dolby is implied.\n"
    )
    (output / "ATTRIBUTION.txt").write_text(attribution, encoding="utf-8")
    provenance = {
        "schema_version": 1,
        "owner": OWNER,
        "repository": REPOSITORY,
        "revision": REVISION,
        "profile_evidence_url": f"{REPOSITORY}/blob/{REVISION}/README.md",
        "profile_evidence_kind": "Publisher README; bitstream verification is a separate step",
        "license": "CC-BY-4.0",
        "license_url": "https://creativecommons.org/licenses/by/4.0/",
        "license_evidence_url": upstream_url(LICENSE_PATH),
        "selected_profiles": list(selected),
        "state": "downloading",
        "sources": [],
    }
    manifest = output / "provenance.json"
    write_json(manifest, provenance)
    try:
        provenance["license_document"] = fetch_verified(
            LICENSE_DOWNLOAD_URL, output / "SolLevante-by-Netflix.docx", LICENSE_SIZE, LICENSE_SHA256,
            accept="application/vnd.github.raw+json",
        )
        write_json(manifest, provenance)
        for key in selected:
            source = SOURCES[key]
            print(f"Downloading {key}: {source['size_bytes']} bytes", flush=True)
            record = fetch_verified(
                upstream_url(source["upstream_path"], media=True),
                output / source["filename"], source["size_bytes"], source["sha256"],
            )
            record.update({
                "id": f"sollevante-{key}",
                "profile": key,
                "declared_profile": source["declared_profile"],
                "upstream_path": source["upstream_path"],
                "lfs_pointer_url": upstream_url(source["upstream_path"]),
                "bitstream_profile_verified": False,
                "content_modified": False,
            })
            provenance["sources"].append(record)
            write_json(manifest, provenance)
            print(f"Verified {record['absolute_path']}", flush=True)
        provenance["state"] = "complete"
        write_json(manifest, provenance)
        print(f"Manifest: {manifest}", flush=True)
        return 0
    except Exception as error:
        provenance["state"] = "failed"
        provenance["error"] = str(error)
        write_json(manifest, provenance)
        print(f"Download failed: {error}", file=sys.stderr, flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
