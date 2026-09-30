#!/usr/bin/env bash
# Executed only inside the admitted image build stage, never as a host installer.
set -euo pipefail

jobs=${GOBY_FFMPEG_BUILD_JOBS:-2}
case "$jobs" in 1|2) ;; *) echo 'FFmpeg build jobs must be 1 or 2.' >&2; exit 1 ;; esac
work=/build/media
evidence=/opt/goby-toolchain-evidence
progress_patch=/build/toolchain-patches/ffmpeg-progress-copyts-nopts.patch
progress_harness=/build/toolchain-patches/progress-copyts-nopts
wakeup_patch=/build/toolchain-patches/ffmpeg-decoder-queue-wakeup.patch
wakeup_harness=/build/toolchain-patches/decoder-queue-wakeup
[[ -s "$progress_patch" ]] || { echo 'Missing FFmpeg progress patch.' >&2; exit 1; }
[[ -s "$wakeup_patch" ]] || { echo 'Missing FFmpeg decoder wakeup patch.' >&2; exit 1; }
for file in native-regression.c native-regression.mk run-native-regression.py README.md; do
  [[ -s "$progress_harness/$file" ]] || { echo "Missing progress regression input: $file" >&2; exit 1; }
  [[ -s "$wakeup_harness/$file" ]] || { echo "Missing decoder wakeup regression input: $file" >&2; exit 1; }
done
mkdir -m 0700 "$work"
mkdir -p "$evidence/licenses" "$evidence/sources" "$evidence/toolchain-patches"
cp "$progress_patch" "$wakeup_patch" "$evidence/toolchain-patches/"
cp -a "$progress_harness" "$evidence/toolchain-patches/"
cp -a "$wakeup_harness" "$evidence/toolchain-patches/"
progress_patch="$evidence/toolchain-patches/ffmpeg-progress-copyts-nopts.patch"
progress_harness="$evidence/toolchain-patches/progress-copyts-nopts"
wakeup_patch="$evidence/toolchain-patches/ffmpeg-decoder-queue-wakeup.patch"
wakeup_harness="$evidence/toolchain-patches/decoder-queue-wakeup"
sha256sum "$progress_patch" "$progress_harness"/* > "$evidence/progress-patch-inputs.sha256"
sha256sum "$wakeup_patch" "$wakeup_harness"/* > "$evidence/wakeup-patch-inputs.sha256"
cd "$work"

curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/releases/ffmpeg-9.0.1.tar.xz -o ffmpeg.tar.xz
printf '%s  ffmpeg.tar.xz\n' cf38e0e28c7e5605942c4a77755349b0145804a397af37eb1fb4c77cb237f635 | sha256sum --check --strict -
curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/releases/ffmpeg-9.0.1.tar.xz.asc -o ffmpeg.tar.xz.asc
curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/ffmpeg-devel.asc -o ffmpeg-devel.asc
mkdir -m 0700 gnupg
# Public-key verification does not need an automatically started private-key agent.
printf '%s\n' no-autostart > "$work/gnupg/gpg.conf"
chmod 600 "$work/gnupg/gpg.conf"
cp -- "$work/gnupg/gpg.conf" "$evidence/gpg-public-verification.conf"
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
export PKG_CONFIG_PATH=/opt/nv-codec-headers/lib/pkgconfig
tar -xJf ffmpeg.tar.xz
cp -a ffmpeg-9.0.1 ffmpeg-baseline
cd ffmpeg-9.0.1
sha256sum fftools/ffmpeg.c fftools/thread_queue.h fftools/thread_queue.c fftools/ffmpeg_sched.c > "$evidence/ffmpeg-patch-input.sha256"
patch --batch --forward --fuzz=0 -p1 < "$wakeup_patch" > "$evidence/ffmpeg-wakeup-patch.log" 2>&1
patch --batch --forward --fuzz=0 -p1 < "$progress_patch" > "$evidence/ffmpeg-progress-patch.log" 2>&1
sha256sum fftools/ffmpeg.c fftools/thread_queue.h fftools/thread_queue.c fftools/ffmpeg_sched.c > "$evidence/ffmpeg-patch-output.sha256"
mkdir "$work/ffmpeg-build"
cd "$work/ffmpeg-build"
"$work/ffmpeg-9.0.1/configure" --prefix=/opt/ffmpeg/9.0.1 \
  --enable-gpl --enable-libx264 --enable-libx265 --enable-libaom --enable-libzimg \
  --enable-libass --enable-libmp3lame --enable-chromaprint \
  --enable-libopus --enable-libvorbis --enable-vaapi --enable-libvpl \
  --enable-ffnvcodec --enable-cuvid --enable-nvenc \
  --disable-debug --disable-doc --disable-ffplay > "$evidence/ffmpeg-configure.txt" 2>&1
make -j "$jobs"
python3 "$wakeup_harness/run-native-regression.py" \
  --baseline "$work/ffmpeg-baseline" --candidate "$work/ffmpeg-9.0.1" --build "$work/ffmpeg-build" \
  --binaries "$work/wakeup-regression-binaries" --evidence "$evidence/wakeup-regression"
python3 "$progress_harness/run-native-regression.py" \
  --baseline "$work/ffmpeg-baseline" --candidate "$work/ffmpeg-9.0.1" --build "$work/ffmpeg-build" \
  --binaries "$work/progress-regression-binaries" --evidence "$evidence/progress-regression"
make install-progs install-data
pkg-config --modversion libchromaprint > "$evidence/chromaprint-version.txt"
cp /usr/share/doc/libchromaprint1/copyright "$evidence/licenses/chromaprint.copyright"
/opt/ffmpeg/9.0.1/bin/ffmpeg -hide_banner -muxers > "$evidence/ffmpeg-muxers.txt" 2>&1
grep -Eq '[[:space:]]chromaprint[[:space:]]' "$evidence/ffmpeg-muxers.txt"
cp ffbuild/config.mak "$evidence/ffmpeg-config.mak"
cp "$work/ffmpeg-9.0.1"/COPYING.* "$work/ffmpeg-9.0.1/LICENSE.md" "$evidence/licenses/"
cp "$work/ffmpeg.tar.xz" "$work/ffmpeg.tar.xz.asc" "$work/ffmpeg-devel.asc" \
  "$work/nv-codec-headers.tar.gz" "$evidence/sources/"
dpkg-query -W -f='${binary:Package}\t${Version}\t${Architecture}\n' > "$evidence/build-packages.tsv"
sha256sum "$evidence"/sources/* > "$evidence/source-archives.sha256"
