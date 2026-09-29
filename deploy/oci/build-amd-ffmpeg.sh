#!/usr/bin/env bash
# Build FFmpeg only, using the separately captured native libplacebo ABI.
set -euo pipefail

jobs=${GOBY_FFMPEG_BUILD_JOBS:-2}
case "$jobs" in 1|2) ;; *) echo 'FFmpeg build jobs must be 1 or 2.' >&2; exit 1 ;; esac
prefix=/opt/goby-amd-ffmpeg
work=/build/amd-media
evidence="$prefix/metadata/amd-oci-build"
patches=/build/toolchain-patches
headers=/build/libplacebo-include/libplacebo
strict_patch=ffmpeg-libplacebo-strict-dovi-mel.patch
wakeup_patch=ffmpeg-decoder-queue-wakeup.patch
progress_patch=ffmpeg-progress-copyts-nopts.patch
for patch in "$strict_patch" "$wakeup_patch" "$progress_patch"; do
  [[ -s "$patches/$patch" ]] || { echo "Missing patch: $patch" >&2; exit 1; }
done
for harness in decoder-queue-wakeup progress-copyts-nopts; do
  for file in native-regression.c native-regression.mk run-native-regression.py README.md; do
    [[ -s "$patches/$harness/$file" ]] || { echo "Missing native regression input: $harness/$file" >&2; exit 1; }
  done
done
[[ -s "$headers/config.h" && -s "$headers/vulkan.h" && -s "$prefix/lib/libplacebo.so.351" ]] || {
  echo 'The captured libplacebo headers and private ABI are required.' >&2; exit 1;
}

mkdir -m 0700 "$work"
# Preserve native v3 evidence as input history, not as this new FFmpeg's receipt.
mv "$prefix/metadata" "$work/native-v3-metadata"
mkdir -p "$evidence/licenses" "$evidence/sources" "$evidence/toolchain-patches"
mv "$work/native-v3-metadata" "$prefix/metadata/native-v3-input"
cp /build/build-amd-ffmpeg.sh "$evidence/"
cp -a "$headers" "$evidence/libplacebo-headers"
cp "$patches/$strict_patch" "$patches/$wakeup_patch" "$patches/$progress_patch" "$evidence/toolchain-patches/"
cp -a "$patches/decoder-queue-wakeup" "$patches/progress-copyts-nopts" "$evidence/toolchain-patches/"
(
  cd "$prefix"
  find lib -type f -print0 | sort -z | xargs -0 sha256sum > metadata/amd-oci-build/retained-libraries.sha256
  find lib -type l -printf '%p\t%l\n' | sort > metadata/amd-oci-build/retained-library-links.tsv
)
(
  cd "$evidence"
  find libplacebo-headers toolchain-patches -type f -print0 | sort -z | xargs -0 sha256sum > build-inputs.sha256
)
mkdir -p /build/amd-pkgconfig
cat > /build/amd-pkgconfig/libplacebo.pc <<'EOF'
prefix=/opt/goby-amd-ffmpeg
libdir=${prefix}/lib
includedir=/build/libplacebo-include

Name: libplacebo
Description: Captured Goby native libplacebo ABI for the AMD OCI extension
Version: 7.351.0
Libs: -L${libdir} -l:libplacebo.so.351
Cflags: -I${includedir}
EOF
cp /build/amd-pkgconfig/libplacebo.pc "$evidence/libplacebo.pc"
export PKG_CONFIG_PATH=/build/amd-pkgconfig:/opt/nv-codec-headers/lib/pkgconfig
[[ $(pkg-config --modversion libplacebo) == 7.351.0 ]]
pkg-config --cflags --libs libplacebo > "$evidence/libplacebo-build-flags.txt"
cd "$work"

curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/releases/ffmpeg-9.0.1.tar.xz -o ffmpeg.tar.xz
printf '%s  ffmpeg.tar.xz\n' cf38e0e28c7e5605942c4a77755349b0145804a397af37eb1fb4c77cb237f635 | sha256sum --check --strict -
curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/releases/ffmpeg-9.0.1.tar.xz.asc -o ffmpeg.tar.xz.asc
curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/ffmpeg-devel.asc -o ffmpeg-devel.asc
mkdir -m 0700 gnupg
printf '%s\n' no-autostart > gnupg/gpg.conf
chmod 600 gnupg/gpg.conf
cp gnupg/gpg.conf "$evidence/gpg-public-verification.conf"
gpg --homedir "$work/gnupg" --batch --import ffmpeg-devel.asc
gpg --homedir "$work/gnupg" --batch --with-colons --fingerprint FCF986EA15E6E293A5644F10B4322F04D67658D8 > fingerprints.txt
grep -Fq 'fpr:::::::::FCF986EA15E6E293A5644F10B4322F04D67658D8:' fingerprints.txt
gpg --homedir "$work/gnupg" --batch --status-fd 1 \
  --verify ffmpeg.tar.xz.asc ffmpeg.tar.xz > "$evidence/ffmpeg-signature-status.txt"
grep -Fq '[GNUPG:] VALIDSIG FCF986EA15E6E293A5644F10B4322F04D67658D8 ' "$evidence/ffmpeg-signature-status.txt"

curl --fail --location --silent --show-error --retry 3 \
  https://codeload.github.com/FFmpeg/nv-codec-headers/tar.gz/0a6fba9a2820628b8103464f4c8753ee05838baa -o nv-codec-headers.tar.gz
printf '%s  nv-codec-headers.tar.gz\n' 1d2070546de622fd6074a99d4b283e727988b7c3624ef85f97b88962264314d2 | sha256sum --check --strict -
mkdir nv-codec-headers
tar -xzf nv-codec-headers.tar.gz --strip-components=1 -C nv-codec-headers
make -C nv-codec-headers PREFIX=/opt/nv-codec-headers install
tar -xJf ffmpeg.tar.xz
cd ffmpeg-9.0.1
patched_files=(libavfilter/vf_libplacebo.c fftools/ffmpeg.c fftools/thread_queue.h fftools/thread_queue.c fftools/ffmpeg_sched.c)
sha256sum "${patched_files[@]}" > "$evidence/ffmpeg-patch-input.sha256"
patch --batch --forward --fuzz=0 -p1 < "$evidence/toolchain-patches/$strict_patch" > "$evidence/$strict_patch.log" 2>&1
# Both negative controls retain the identical strict Dolby Vision implementation.
# Only the scheduler and progress fixes differ from their candidate counterparts.
sha256sum "${patched_files[@]}" > "$evidence/ffmpeg-negative-control.sha256"
cp -a "$work/ffmpeg-9.0.1" "$work/ffmpeg-baseline"
for patch in "$wakeup_patch" "$progress_patch"; do
  patch --batch --forward --fuzz=0 -p1 < "$evidence/toolchain-patches/$patch" > "$evidence/$patch.log" 2>&1
done
sha256sum "${patched_files[@]}" > "$evidence/ffmpeg-patch-output.sha256"
mkdir "$work/ffmpeg-build"
cd "$work/ffmpeg-build"
"$work/ffmpeg-9.0.1/configure" --prefix="$prefix" \
  --enable-gpl --enable-libx264 --enable-libx265 --enable-libaom --enable-libzimg \
  --enable-libass --enable-libmp3lame --enable-libopus --enable-libvorbis \
  --enable-libplacebo --enable-vulkan --enable-libdrm --enable-vaapi --enable-libvpl \
  --enable-ffnvcodec --enable-cuvid --enable-nvenc \
  --extra-ldflags="-Wl,-rpath,'\$\$ORIGIN/../lib' -Wl,-rpath-link,$prefix/lib" \
  --disable-debug --disable-doc --disable-ffplay > "$evidence/ffmpeg-configure.txt" 2>&1
make -j "$jobs"
python3 "$evidence/toolchain-patches/decoder-queue-wakeup/run-native-regression.py" \
  --baseline "$work/ffmpeg-baseline" --candidate "$work/ffmpeg-9.0.1" --build "$work/ffmpeg-build" \
  --binaries "$work/wakeup-regression-binaries" --evidence "$evidence/wakeup-regression"
python3 "$evidence/toolchain-patches/progress-copyts-nopts/run-native-regression.py" \
  --baseline "$work/ffmpeg-baseline" --candidate "$work/ffmpeg-9.0.1" --build "$work/ffmpeg-build" \
  --binaries "$work/progress-regression-binaries" --evidence "$evidence/progress-regression"
make install-progs install-data
# Enforce the final relative search path independently of configure/make quoting.
for binary in "$prefix/bin/ffmpeg" "$prefix/bin/ffprobe"; do
  patchelf --set-rpath '$ORIGIN/../lib' "$binary"
  [[ $(patchelf --print-rpath "$binary") == '$ORIGIN/../lib' ]]
  ldd "$binary" >> "$evidence/runtime-linkage.txt"
done
if grep -Fq 'not found' "$evidence/runtime-linkage.txt"; then
  echo 'The rebuilt FFmpeg runtime has missing shared libraries.' >&2
  exit 1
fi
cp ffbuild/config.mak "$evidence/ffmpeg-config.mak"
cp ffbuild/config.log "$evidence/ffmpeg-config.log"
cp "$work/ffmpeg-9.0.1"/COPYING.* "$work/ffmpeg-9.0.1/LICENSE.md" "$evidence/licenses/"
cp "$work/ffmpeg.tar.xz" "$work/ffmpeg.tar.xz.asc" "$work/ffmpeg-devel.asc" \
  "$work/nv-codec-headers.tar.gz" "$evidence/sources/"
dpkg-query -W -f='${binary:Package}\t${Version}\t${Architecture}\n' > "$evidence/build-packages.tsv"
sha256sum "$evidence"/sources/* > "$evidence/source-archives.sha256"
printf 'profile=amd-oci-ffmpeg-v4\nffmpeg=9.0.1\nlibplacebo=7.351.0\nlibplacebo_rebuilt=false\n' > "$evidence/build-profile.txt"
(
  cd "$prefix"
  sha256sum --check --strict metadata/amd-oci-build/retained-libraries.sha256
  sha256sum bin/ffmpeg bin/ffprobe > metadata/amd-oci-build/rebuilt-executables.sha256
  find . -type f ! -path ./metadata/installed-files.sha256 -print0 \
    | sort -z | xargs -0 sha256sum > metadata/installed-files.sha256
)
