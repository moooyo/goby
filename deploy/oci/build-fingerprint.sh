#!/usr/bin/env bash
# Build the vendored native-rate helper without external audio libraries.
set -euo pipefail

jobs=${GOBY_FFMPEG_BUILD_JOBS:-2}
case "$jobs" in 1|2) ;; *) echo 'Fingerprint build jobs must be 1 or 2.' >&2; exit 1 ;; esac
source=/build/intro-fingerprint
build=/build/fingerprint-build
prefix=/opt/goby-intro-fingerprint
evidence=/opt/goby-toolchain-evidence/fingerprint
mkdir -p "$evidence"
cmake --version > "$evidence/cmake-version.txt"
c++ --version > "$evidence/compiler-version.txt"
cmake -S "$source" -B "$build" -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$prefix"
cmake --build "$build" --target goby-intro-fingerprint --parallel "$jobs"
python3 "$source/tests/test_protocol.py" --helper "$build/goby-intro-fingerprint" > "$evidence/protocol-tests.txt" 2>&1
cmake --install "$build"
"$prefix/bin/goby-intro-fingerprint" --describe > "$evidence/description.json"
sha256sum "$prefix/bin/goby-intro-fingerprint" > "$evidence/executable.sha256"
cp "$source/vendor/SHA256SUMS" "$evidence/vendor-SHA256SUMS"
cp "$build/CMakeCache.txt" "$evidence/CMakeCache.txt"
