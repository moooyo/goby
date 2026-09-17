#!/usr/bin/env bash
# Executed only inside the admitted image build stage, never as a host installer.
set -euo pipefail

jobs=${GOBY_FFMPEG_BUILD_JOBS:-2}
case "$jobs" in 1|2) ;; *) echo 'FFmpeg build jobs must be 1 or 2.' >&2; exit 1 ;; esac
work=/build/media
evidence=/opt/goby-toolchain-evidence
mkdir -m 0700 "$work"
mkdir -p "$evidence/licenses" "$evidence/sources"
cd "$work"

curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/releases/ffmpeg-9.0.1.tar.xz -o ffmpeg.tar.xz
printf '%s  ffmpeg.tar.xz\n' cf38e0e28c7e5605942c4a77755349b0145804a397af37eb1fb4c77cb237f635 | sha256sum --check --strict -
curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/releases/ffmpeg-9.0.1.tar.xz.asc -o ffmpeg.tar.xz.asc
curl --fail --location --silent --show-error --retry 3 \
  https://ffmpeg.org/ffmpeg-devel.asc -o ffmpeg-devel.asc
mkdir -m 0700 gnupg
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
cd ffmpeg-9.0.1
./configure --prefix=/opt/ffmpeg/9.0.1 \
  --enable-gpl --enable-libx264 --enable-libass --enable-libmp3lame \
  --enable-libopus --enable-libvorbis --enable-vaapi --enable-libvpl \
  --enable-ffnvcodec --enable-cuvid --enable-nvenc \
  --disable-debug --disable-doc --disable-ffplay > "$evidence/ffmpeg-configure.txt" 2>&1
make -j "$jobs"
make install-progs install-data
cp ffbuild/config.mak "$evidence/ffmpeg-config.mak"
cp COPYING.* LICENSE.md "$evidence/licenses/"
cp "$work/ffmpeg.tar.xz" "$work/ffmpeg.tar.xz.asc" "$work/ffmpeg-devel.asc" \
  "$work/nv-codec-headers.tar.gz" "$evidence/sources/"
dpkg-query -W -f='${binary:Package}\t${Version}\t${Architecture}\n' > "$evidence/build-packages.tsv"
sha256sum "$evidence"/sources/* > "$evidence/source-archives.sha256"
