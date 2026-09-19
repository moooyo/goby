#!/usr/bin/env python3
"""Bundle trusted FFmpeg build dependencies without replacing host libraries."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess


# Keep the loader and glibc together with the target OS. GPU drivers are loaded
# dynamically and must be installed for the target device on that OS as well.
SYSTEM_LIBRARIES = {
    "libc.so.6",
    "libm.so.6",
    "libmvec.so.1",
    "libdl.so.2",
    "libpthread.so.0",
    "librt.so.1",
    "libresolv.so.2",
    "libutil.so.1",
    "libanl.so.1",
    "libBrokenLocale.so.1",
    "libthread_db.so.1",
}
DEPENDENCY = re.compile(r"^\s*(\S+)\s+=>\s+(/\S+)\s+\(0x[0-9a-fA-F]+\)\s*$")


def run(*command: str, clean: bool = False) -> str:
    environment = dict(os.environ, LC_ALL="C")
    if clean:
        environment.pop("LD_LIBRARY_PATH", None)
    return subprocess.run(
        command,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        env=environment,
    ).stdout


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def dependencies(binary: Path, *, clean: bool = False) -> dict[str, Path]:
    # ldd is restricted to the executables produced from verified source by the
    # calling installer; this helper must not be used on untrusted executables.
    output = run("ldd", str(binary), clean=clean)
    if "not found" in output:
        raise RuntimeError(f"Unresolved dependencies for {binary}:\n{output}")
    result: dict[str, Path] = {}
    for line in output.splitlines():
        match = DEPENDENCY.match(line)
        if match:
            result[match[1]] = Path(match[2]).resolve(strict=True)
        elif "/" in line and not re.search(r"/ld-linux[^\s]*\s+\(0x[0-9a-fA-F]+\)", line):
            raise RuntimeError(f"Unrecognized dependency line for {binary}: {line}")
    return result


def system_library(name: str) -> bool:
    return name in SYSTEM_LIBRARIES or name.startswith("libnss_") or name.startswith("ld-linux")


def package_metadata(path: Path, copyright_dir: Path) -> list[dict[str, str]]:
    try:
        owners = run("dpkg-query", "-S", str(path)).splitlines()
    except subprocess.CalledProcessError:
        return []
    packages = []
    for owner in owners:
        package, separator, _ = owner.partition(": ")
        if not separator:
            continue
        version = run("dpkg-query", "-W", "-f=${Version}", package).strip()
        packages.append({"name": package, "version": version})
        # Debian multiarch package names end in :architecture, while their
        # copyright documents use the package name without that suffix.
        copyright_file = Path("/usr/share/doc") / package.split(":", 1)[0] / "copyright"
        if copyright_file.is_file():
            shutil.copy2(copyright_file, copyright_dir / f"{package.replace(':', '_')}.copyright")
    return packages


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("prefix", type=Path)
    parser.add_argument("--required-library", action="append", default=[])
    args = parser.parse_args()
    prefix = args.prefix.resolve(strict=True)
    if prefix == Path("/"):
        raise ValueError("The staging prefix must not be the filesystem root.")
    binaries = [prefix / "bin" / name for name in ("ffmpeg", "ffprobe")]
    if any(not binary.is_file() or binary.is_symlink() for binary in binaries):
        raise ValueError("The staging prefix must contain regular ffmpeg and ffprobe binaries.")
    library_dir = prefix / "lib"
    library_dir.mkdir(exist_ok=True)
    metadata_dir = prefix / "metadata"
    metadata_dir.mkdir(exist_ok=True)
    copyright_dir = metadata_dir / "licenses" / "runtime"
    copyright_dir.mkdir(parents=True, exist_ok=True)
    selected: dict[str, Path] = {}
    for binary in binaries:
        for name, source in dependencies(binary).items():
            if name in selected and digest(selected[name]) != digest(source):
                raise RuntimeError(f"Conflicting runtime libraries share the SONAME {name}.")
            selected[name] = source
    missing = sorted(set(args.required_library) - selected.keys())
    if missing:
        raise RuntimeError(f"Required runtime libraries are missing from the dependency closure: {', '.join(missing)}")
    records = []
    for name, source in sorted(selected.items()):
        external = system_library(name)
        record = {
            "soname": name,
            "source": str(source),
            "source_sha256": digest(source),
            "external_system_library": external,
            "packages": package_metadata(source, copyright_dir),
        }
        if not external:
            target = library_dir / name
            if target.exists() or target.is_symlink():
                raise RuntimeError(f"The staging library already exists: {target}")
            shutil.copy2(source, target)
            # Old-style RPATH is intentional: it also applies to transitive
            # dependencies and does not require global loader configuration.
            run("patchelf", "--force-rpath", "--set-rpath", "$ORIGIN", str(target))
            record["bundled_sha256"] = digest(target)
        records.append(record)
    for binary in binaries:
        run("patchelf", "--force-rpath", "--set-rpath", "$ORIGIN/../lib", str(binary))
        for name, resolved in dependencies(binary, clean=True).items():
            if not system_library(name) and resolved.parent != library_dir:
                raise RuntimeError(f"A bundled dependency still resolves outside the prefix: {name}: {resolved}")
    (metadata_dir / "runtime-libraries.json").write_text(
        json.dumps(records, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )
    (metadata_dir / "runtime-requirements.txt").write_text(
        "Move bin/, lib/ and metadata/ together. No global LD_LIBRARY_PATH is required.\n"
        "The dynamic loader and glibc remain provided by a compatible target Linux system.\n"
        "Install target GPU VAAPI drivers and Vulkan ICDs separately; they are not bundled.\n"
        "Fontconfig configuration and fonts remain provided by the target system.\n"
        "Bundled library versions and source hashes are recorded in runtime-libraries.json.\n",
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
