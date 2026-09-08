#!/usr/bin/env bash
# Install the project toolchains on a Debian Linux verification host.
set -euo pipefail

[[ $(id -u) == 0 ]] || { echo 'Run this script as root.' >&2; exit 1; }
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || {
  echo 'This bootstrap currently supports Linux amd64.' >&2; exit 1;
}

GO_VERSION=1.27.1
GO_SHA256=63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445
FFMPEG_VERSION=9.0.1
FFMPEG_KEY=FCF986EA15E6E293A5644F10B4322F04D67658D8
NV_CODEC_VERSION=n13.1.15.0
TOOLCHAINS=/opt/goby-toolchains
GO_PREFIX="$TOOLCHAINS/go$GO_VERSION"
FFMPEG_PREFIX="$TOOLCHAINS/ffmpeg-$FFMPEG_VERSION"
BUILD_ROOT=${GOBY_BUILD_ROOT:-/var/tmp}
JOBS=${GOBY_BUILD_JOBS:-4}

mkdir -p "$TOOLCHAINS" "$BUILD_ROOT"
work=$(mktemp -d "$BUILD_ROOT/goby-toolchain-build.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
chmod 700 "$work"
export TMPDIR="$work"

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install --no-install-recommends -y \
  build-essential ca-certificates curl gnupg libass-dev libdrm-dev libmp3lame-dev libnuma-dev libopus-dev \
  libssl-dev libva-dev libvorbis-dev libvpl-dev libx264-dev nasm pkg-config \
  python3 xz-utils yasm

if [[ ! -x "$GO_PREFIX/bin/go" ]]; then
  curl --fail --location --silent --show-error --retry 3 \
    "https://go.dev/dl/go$GO_VERSION.linux-amd64.tar.gz" -o "$work/go.tar.gz"
  printf '%s  %s\n' "$GO_SHA256" "$work/go.tar.gz" | sha256sum --check -
  mkdir -p "$GO_PREFIX"
  tar -xzf "$work/go.tar.gz" --strip-components=1 -C "$GO_PREFIX"
fi

if [[ ! -x "$FFMPEG_PREFIX/bin/ffmpeg" ]] || \
  ! "$FFMPEG_PREFIX/bin/ffmpeg" -buildconf 2>/dev/null | grep -Fq -- '--enable-libass'; then
  curl --fail --location --silent --show-error --retry 3 \
    "https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz" -o "$work/ffmpeg.tar.xz"
  curl --fail --location --silent --show-error --retry 3 \
    "https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz.asc" -o "$work/ffmpeg.tar.xz.asc"
  curl --fail --location --silent --show-error --retry 3 \
    https://ffmpeg.org/ffmpeg-devel.asc -o "$work/ffmpeg-devel.asc"
  mkdir -m 700 "$work/gnupg"
  gpg --homedir "$work/gnupg" --batch --import "$work/ffmpeg-devel.asc"
  gpg --homedir "$work/gnupg" --batch --with-colons --fingerprint "$FFMPEG_KEY" \
    | grep -Fq "fpr:::::::::$FFMPEG_KEY:"
  gpg --homedir "$work/gnupg" --batch --status-fd 1 \
    --verify "$work/ffmpeg.tar.xz.asc" "$work/ffmpeg.tar.xz" \
    | grep -Fq "[GNUPG:] VALIDSIG $FFMPEG_KEY "
  sha256sum "$work/ffmpeg.tar.xz" > "$TOOLCHAINS/ffmpeg-$FFMPEG_VERSION.sha256"

  curl --fail --location --silent --show-error --retry 3 \
    "https://github.com/FFmpeg/nv-codec-headers/archive/refs/tags/$NV_CODEC_VERSION.tar.gz" \
    -o "$work/nv-codec-headers.tar.gz"
  mkdir "$work/nv-codec-headers"
  tar -xzf "$work/nv-codec-headers.tar.gz" --strip-components=1 -C "$work/nv-codec-headers"
  make -C "$work/nv-codec-headers" \
    PREFIX="$TOOLCHAINS/nv-codec-headers-$NV_CODEC_VERSION" install
  export PKG_CONFIG_PATH="$TOOLCHAINS/nv-codec-headers-$NV_CODEC_VERSION/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
  tar -xJf "$work/ffmpeg.tar.xz" -C "$work"
  (
    cd "$work/ffmpeg-$FFMPEG_VERSION"
    ./configure --prefix="$FFMPEG_PREFIX" \
      --enable-gpl --enable-libx264 --enable-libass --enable-libmp3lame \
      --enable-libopus --enable-libvorbis --enable-vaapi --enable-libvpl \
      --enable-ffnvcodec --enable-cuvid --enable-nvenc \
      --disable-debug --disable-doc --disable-ffplay \
      > "$TOOLCHAINS/ffmpeg-configure.log" 2>&1
    make -j "$JOBS" > "$TOOLCHAINS/ffmpeg-build.log" 2>&1
    make install-progs install-data > "$TOOLCHAINS/ffmpeg-install.log" 2>&1
    cp ffbuild/config.mak "$TOOLCHAINS/ffmpeg-config.mak"
  )
fi

"$GO_PREFIX/bin/go" version
"$FFMPEG_PREFIX/bin/ffmpeg" -version | head -3
"$FFMPEG_PREFIX/bin/ffprobe" -version | head -1
"$FFMPEG_PREFIX/bin/ffmpeg" -hide_banner -hwaccels
"$FFMPEG_PREFIX/bin/ffmpeg" -hide_banner -encoders 2>/dev/null \
  | grep -E 'libx264|[[:space:]]aac[[:space:]]|h264_(nvenc|qsv|vaapi)'
echo 'Toolchain installation complete. Hardware execution still requires matching devices and drivers.'
