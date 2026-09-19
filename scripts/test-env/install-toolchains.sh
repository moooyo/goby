#!/usr/bin/env bash
# Install isolated project toolchains on a Debian Linux verification host.
# GOBY_TOOLCHAIN_REUSE_ROOT may point to a saved earlier build's source snapshot.
# It must retain the FFmpeg archive/signature/keys and both Git repositories,
# including libplacebo's initialized fast_float submodule. Sources are verified
# again; no previous private binaries, development headers or object files are
# reused. Installed system headers remain normal recorded build dependencies.
# GOBY_TOOLCHAIN_SKIP_PACKAGE_INSTALL=1 verifies them without updating packages.
set -euo pipefail

[[ $(id -u) == 0 ]] || { echo 'Run this script as root.' >&2; exit 1; }
[[ $(uname -s) == Linux && $(uname -m) == x86_64 ]] || {
  echo 'This bootstrap currently supports Linux amd64.' >&2; exit 1;
}

GO_VERSION=1.27.1
GO_SHA256=63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445
FFMPEG_VERSION=9.0.1
FFMPEG_KEY=FCF986EA15E6E293A5644F10B4322F04D67658D8
LIBPLACEBO_VERSION=7.351.0
LIBPLACEBO_COMMIT=3188549fba13bbdf3a5a98de2a38c2e71f04e21e
LIBPLACEBO_KEY=1DDB8076B14D5B4832FC99D9EB52DA9C02BA6FB4
LIBPLACEBO_SIGNER=2B5C251978C2FFAF812DA60436C67C41D4205B9D
NV_CODEC_VERSION=n13.1.15.0
NV_CODEC_COMMIT=0a6fba9a2820628b8103464f4c8753ee05838baa
TOOLCHAINS=${GOBY_TOOLCHAINS:-/opt/goby-toolchains}
BUILD_ROOT=${GOBY_BUILD_ROOT:-/var/tmp}
REUSE_ROOT=${GOBY_TOOLCHAIN_REUSE_ROOT:-}
JOBS=${GOBY_BUILD_JOBS:-2}
SKIP_PACKAGE_INSTALL=${GOBY_TOOLCHAIN_SKIP_PACKAGE_INSTALL:-0}
SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
RUNTIME_HELPER="$SCRIPT_DIR/bundle-toolchain-runtime.py"
FFMPEG_PATCH="$SCRIPT_DIR/toolchain-patches/ffmpeg-libplacebo-strict-dovi-mel.patch"
FFMPEG_WAKE_PATCH="$SCRIPT_DIR/toolchain-patches/ffmpeg-decoder-queue-wakeup.patch"
NATIVE_HARNESS="$SCRIPT_DIR/toolchain-patches/decoder-queue-wakeup"
native_inputs=(native-regression.c native-regression.mk run-native-regression.py)

[[ "$JOBS" =~ ^[1-9][0-9]*$ ]] || { echo 'GOBY_BUILD_JOBS must be positive.' >&2; exit 1; }
[[ "$SKIP_PACKAGE_INSTALL" == 0 || "$SKIP_PACKAGE_INSTALL" == 1 ]] || {
  echo 'GOBY_TOOLCHAIN_SKIP_PACKAGE_INSTALL must be 0 or 1.' >&2; exit 1;
}
for path in "$TOOLCHAINS" "$BUILD_ROOT"; do
  [[ "$path" =~ ^/[A-Za-z0-9._/-]+$ && "$path" != / ]] || {
    echo 'Toolchain and build roots must be absolute non-root paths using letters, digits, dot, underscore, slash or hyphen.' >&2
    exit 1
  }
done
[[ -f "$RUNTIME_HELPER" ]] || { echo "Missing runtime helper: $RUNTIME_HELPER" >&2; exit 1; }
[[ -s "$FFMPEG_PATCH" ]] || { echo "Missing or empty FFmpeg patch: $FFMPEG_PATCH" >&2; exit 1; }
[[ -s "$FFMPEG_WAKE_PATCH" ]] || { echo "Missing or empty FFmpeg patch: $FFMPEG_WAKE_PATCH" >&2; exit 1; }
for file in "${native_inputs[@]}" README.md; do
  [[ -s "$NATIVE_HARNESS/$file" ]] || { echo "Missing native regression input: $NATIVE_HARNESS/$file" >&2; exit 1; }
done
if [[ -n "$REUSE_ROOT" ]]; then
  [[ "$REUSE_ROOT" =~ ^/[A-Za-z0-9._/-]+$ && "$REUSE_ROOT" != / && -d "$REUSE_ROOT" ]] || {
    echo 'GOBY_TOOLCHAIN_REUSE_ROOT must reference an existing absolute source snapshot.' >&2
    exit 1
  }
  REUSE_ROOT=$(realpath -- "$REUSE_ROOT")
  [[ "$REUSE_ROOT" =~ ^/[A-Za-z0-9._/-]+$ && "$REUSE_ROOT" != / ]] || {
    echo 'The resolved source snapshot must use a supported non-root path.' >&2
    exit 1
  }
fi
mkdir -p "$TOOLCHAINS" "$BUILD_ROOT"
TOOLCHAINS=$(realpath -- "$TOOLCHAINS")
BUILD_ROOT=$(realpath -- "$BUILD_ROOT")
for path in "$TOOLCHAINS" "$BUILD_ROOT"; do
  [[ "$path" =~ ^/[A-Za-z0-9._/-]+$ && "$path" != / ]] || {
    echo 'Resolved roots must use supported absolute non-root paths.' >&2
    exit 1
  }
done
GO_PREFIX="$TOOLCHAINS/go$GO_VERSION"
exec 9>"$TOOLCHAINS/.install-toolchains.lock"
flock -n 9 || { echo 'Another toolchain installation holds this root.' >&2; exit 1; }

work=$(mktemp -d "$BUILD_ROOT/goby-toolchain-build.XXXXXXXX")
publish=''
cleanup() {
  result=$?
  if (( result != 0 )); then
    echo "Toolchain preparation failed; retained build files and logs: $work" >&2
    if [[ -n "$publish" ]]; then
      echo "Unpublished staging directory retained: $publish" >&2
    fi
    return
  fi
  rm -rf -- "$work"
  if [[ -n "$publish" ]]; then
    rm -rf -- "$publish"
  fi
}
trap cleanup EXIT
chmod 700 "$work"
export TMPDIR="$work"

ffmpeg_flags=(
  --enable-gpl --enable-libx264 --enable-libx265 --enable-libaom
  --enable-libass --enable-libmp3lame --enable-libopus --enable-libvorbis
  --enable-libzimg --enable-libplacebo --enable-vulkan --enable-libdrm
  --enable-vaapi --enable-libvpl --enable-ffnvcodec --enable-cuvid --enable-nvenc
  --disable-debug --disable-doc --disable-ffplay
)
placebo_flags=(
  --buildtype=release --default-library=shared --libdir=lib --wrap-mode=nodownload
  -Dvulkan=enabled -Dvk-proc-addr=enabled -Dglslang=enabled -Dshaderc=disabled
  -Ddovi=enabled -Dlibdovi=disabled -Dlcms=enabled -Dopengl=disabled -Dd3d11=disabled
  -Dunwind=disabled -Dxxhash=disabled -Ddemos=false -Dtests=false -Dbench=false -Dfuzz=false
)
ffmpeg_patch_hash=$(sha256sum "$FFMPEG_PATCH" | cut -d ' ' -f 1)
ffmpeg_wake_patch_hash=$(sha256sum "$FFMPEG_WAKE_PATCH" | cut -d ' ' -f 1)
declare -A native_hashes
for file in "${native_inputs[@]}"; do
  native_hashes[$file]=$(sha256sum "$NATIVE_HARNESS/$file" | cut -d ' ' -f 1)
done
{
  printf 'recipe=amd-media-v3\nffmpeg=%s\nlibplacebo=%s\nlibplacebo_commit=%s\nnv_codec=%s\nnv_codec_commit=%s\n' \
    "$FFMPEG_VERSION" "$LIBPLACEBO_VERSION" "$LIBPLACEBO_COMMIT" "$NV_CODEC_VERSION" "$NV_CODEC_COMMIT"
  printf 'installer_sha256=%s\n' "$(sha256sum "${BASH_SOURCE[0]}" | cut -d ' ' -f 1)"
  printf 'runtime_helper_sha256=%s\n' "$(sha256sum "$RUNTIME_HELPER" | cut -d ' ' -f 1)"
  printf 'ffmpeg_patch_sha256=%s\n' "$ffmpeg_patch_hash"
  printf 'ffmpeg_decoder_wakeup_patch_sha256=%s\n' "$ffmpeg_wake_patch_hash"
  for file in "${native_inputs[@]}"; do
    printf 'native_input=%s sha256=%s\n' "$file" "${native_hashes[$file]}"
  done
  printf 'ffmpeg_flag=%s\n' "${ffmpeg_flags[@]}"
  printf 'libplacebo_flag=%s\n' "${placebo_flags[@]}"
} > "$work/build-recipe.txt"
recipe_hash=$(sha256sum "$work/build-recipe.txt" | cut -d ' ' -f 1)
# A source or recipe change creates a successor; published toolchains stay intact.
FFMPEG_PREFIX="$TOOLCHAINS/ffmpeg-$FFMPEG_VERSION-goby-${recipe_hash:0:12}"

build_ffmpeg=1
if [[ -e "$FFMPEG_PREFIX" || -L "$FFMPEG_PREFIX" ]]; then
  if [[ -d "$FFMPEG_PREFIX" && ! -L "$FFMPEG_PREFIX" &&
        -x "$FFMPEG_PREFIX/bin/ffmpeg" && -x "$FFMPEG_PREFIX/bin/ffprobe" &&
        -f "$FFMPEG_PREFIX/metadata/build-recipe.txt" &&
        -f "$FFMPEG_PREFIX/metadata/installed-files.sha256" ]] &&
      cmp -s "$work/build-recipe.txt" "$FFMPEG_PREFIX/metadata/build-recipe.txt" &&
      (cd "$FFMPEG_PREFIX" && sha256sum --quiet --check metadata/installed-files.sha256); then
    build_ffmpeg=0
  else
    echo "Refusing to overwrite an existing or incomplete toolchain: $FFMPEG_PREFIX" >&2
    echo 'Select a new GOBY_TOOLCHAINS root; retained services may still reference this prefix.' >&2
    exit 1
  fi
fi
if [[ -e "$GO_PREFIX" || -L "$GO_PREFIX" ]]; then
  [[ -d "$GO_PREFIX" && ! -L "$GO_PREFIX" && -x "$GO_PREFIX/bin/go" ]] || {
    echo "Refusing to overwrite an incomplete Go installation: $GO_PREFIX" >&2
    exit 1
  }
  [[ $("$GO_PREFIX/bin/go" version) == "go version go$GO_VERSION linux/amd64" ]] || {
    echo "The existing Go installation does not match go$GO_VERSION linux/amd64." >&2
    exit 1
  }
fi

packages=(
  build-essential ca-certificates curl git gnupg glslang-dev libaom-dev libass-dev
  libdrm-dev liblcms2-dev libmp3lame-dev libnuma-dev libopus-dev libssl-dev
  libva-dev libvorbis-dev libvpl-dev libvulkan-dev libx264-dev libx265-dev libzimg-dev
  meson nasm ninja-build patch patchelf pkg-config python3 python3-jinja2 python3-markupsafe
  spirv-headers xz-utils yasm
)
if [[ "$SKIP_PACKAGE_INSTALL" == 1 ]]; then
  for package in "${packages[@]}"; do
    [[ $(dpkg-query -W -f='${db:Status-Status}' "$package" 2>/dev/null) == installed ]] || {
      echo "Required build package is not installed: $package" >&2
      exit 1
    }
  done
elif (( build_ffmpeg )) || [[ ! -x "$GO_PREFIX/bin/go" ]]; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install --no-install-recommends -y "${packages[@]}"
fi

download() {
  curl --fail --location --silent --show-error --retry 3 "$1" -o "$2"
}

if [[ ! -x "$GO_PREFIX/bin/go" ]]; then
  download "https://go.dev/dl/go$GO_VERSION.linux-amd64.tar.gz" "$work/go.tar.gz"
  printf '%s  %s\n' "$GO_SHA256" "$work/go.tar.gz" | sha256sum --check -
  publish=$(mktemp -d "$TOOLCHAINS/.go$GO_VERSION.XXXXXXXX")
  tar -xzf "$work/go.tar.gz" --strip-components=1 -C "$publish"
  chmod 755 "$publish"
  mv -T -- "$publish" "$GO_PREFIX"
  publish=''
fi

if (( build_ffmpeg )); then
  evidence="$work/evidence"
  dependencies="$work/private-dependencies"
  mkdir -p "$evidence" "$dependencies"
  cp "$work/build-recipe.txt" "$evidence/build-recipe.txt"
  cp /etc/os-release "$evidence/build-os-release.txt"
  printf 'skip_package_install=%s\nbuild_jobs=%s\n' "$SKIP_PACKAGE_INSTALL" "$JOBS" > "$evidence/build-controls.txt"
  dpkg-query -W -f='${binary:Package}\t${Version}\t${Architecture}\n' "${packages[@]}" \
    > "$evidence/build-packages.tsv"
  cp -- "${BASH_SOURCE[0]}" "$RUNTIME_HELPER" "$evidence/"
  mkdir "$evidence/toolchain-patches"
  cp -- "$FFMPEG_PATCH" "$FFMPEG_WAKE_PATCH" "$evidence/toolchain-patches/"
  mkdir "$evidence/toolchain-patches/decoder-queue-wakeup"
  for file in "${native_inputs[@]}" README.md; do
    cp -- "$NATIVE_HARNESS/$file" "$evidence/toolchain-patches/decoder-queue-wakeup/"
  done
  if [[ -f "$(dirname -- "$FFMPEG_PATCH")/README.md" ]]; then
    cp -- "$(dirname -- "$FFMPEG_PATCH")/README.md" "$evidence/toolchain-patches/"
  fi
  FFMPEG_PATCH="$evidence/toolchain-patches/$(basename -- "$FFMPEG_PATCH")"
  FFMPEG_WAKE_PATCH="$evidence/toolchain-patches/$(basename -- "$FFMPEG_WAKE_PATCH")"
  NATIVE_HARNESS="$evidence/toolchain-patches/decoder-queue-wakeup"
  [[ $(sha256sum "$FFMPEG_PATCH" | cut -d ' ' -f 1) == "$ffmpeg_patch_hash" ]]
  [[ $(sha256sum "$FFMPEG_WAKE_PATCH" | cut -d ' ' -f 1) == "$ffmpeg_wake_patch_hash" ]]
  for file in "${native_inputs[@]}"; do
    [[ $(sha256sum "$NATIVE_HARNESS/$file" | cut -d ' ' -f 1) == "${native_hashes[$file]}" ]]
  done

  if [[ -n "$REUSE_ROOT" ]]; then
    # Only source objects are reused. Rebuild development headers and libraries
    # instead of trusting an earlier build's unrecorded intermediate files.
    for file in ffmpeg.tar.xz ffmpeg.tar.xz.asc ffmpeg-devel.asc libplacebo-maintainer.asc; do
      [[ -f "$REUSE_ROOT/$file" ]] || { echo "Missing reusable source: $REUSE_ROOT/$file" >&2; exit 1; }
      cp -- "$REUSE_ROOT/$file" "$work/$file"
    done
    printf '%s\n' "$REUSE_ROOT" > "$evidence/reused-source-root.txt"
  else
    download "https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz" "$work/ffmpeg.tar.xz"
    download "https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz.asc" "$work/ffmpeg.tar.xz.asc"
    download https://ffmpeg.org/ffmpeg-devel.asc "$work/ffmpeg-devel.asc"
    download https://github.com/haasn.gpg "$work/libplacebo-maintainer.asc"
  fi
  mkdir -m 700 "$work/gnupg"
  export GNUPGHOME="$work/gnupg"
  gpg --batch --import "$work/ffmpeg-devel.asc" "$work/libplacebo-maintainer.asc"
  gpg --batch --with-colons --fingerprint "$FFMPEG_KEY" > "$evidence/ffmpeg-key.txt"
  grep -Fq "fpr:::::::::$FFMPEG_KEY:" "$evidence/ffmpeg-key.txt"
  gpg --batch --status-fd 1 --verify "$work/ffmpeg.tar.xz.asc" "$work/ffmpeg.tar.xz" \
    > "$evidence/ffmpeg-signature.status"
  grep -Fq "[GNUPG:] VALIDSIG $FFMPEG_KEY " "$evidence/ffmpeg-signature.status"
  gpg --batch --with-colons --fingerprint "$LIBPLACEBO_KEY" > "$evidence/libplacebo-key.txt"
  grep -Fq "fpr:::::::::$LIBPLACEBO_KEY:" "$evidence/libplacebo-key.txt"
  cp "$work/ffmpeg.tar.xz.asc" "$work/ffmpeg-devel.asc" "$work/libplacebo-maintainer.asc" "$evidence/"

  if [[ -n "$REUSE_ROOT" ]]; then
    git clone -q --no-hardlinks --no-checkout "$REUSE_ROOT/libplacebo" "$work/libplacebo"
  else
    git init -q "$work/libplacebo"
    git -C "$work/libplacebo" remote add origin https://github.com/haasn/libplacebo.git
    git -C "$work/libplacebo" fetch -q --depth 1 origin "refs/tags/v$LIBPLACEBO_VERSION:refs/tags/v$LIBPLACEBO_VERSION"
  fi
  [[ $(git -C "$work/libplacebo" rev-parse "v$LIBPLACEBO_VERSION^{commit}") == "$LIBPLACEBO_COMMIT" ]]
  git -C "$work/libplacebo" -c gpg.format=openpgp -c gpg.program=gpg verify-tag --raw "v$LIBPLACEBO_VERSION" \
    > "$evidence/libplacebo-signature.status" 2>&1
  grep -Fq "[GNUPG:] VALIDSIG $LIBPLACEBO_SIGNER " "$evidence/libplacebo-signature.status"
  git -C "$work/libplacebo" cat-file tag "v$LIBPLACEBO_VERSION" > "$evidence/libplacebo-signed-tag.txt"
  git -C "$work/libplacebo" checkout -q --detach "$LIBPLACEBO_COMMIT"
  # The signed parent commit pins the only source submodule used by this build.
  fast_float_commit=$(git -C "$work/libplacebo" rev-parse HEAD:3rdparty/fast_float)
  if [[ -n "$REUSE_ROOT" ]]; then
    git -C "$work/libplacebo" submodule init -- 3rdparty/fast_float
    git clone -q --no-hardlinks --no-checkout "$REUSE_ROOT/libplacebo/3rdparty/fast_float" \
      "$work/libplacebo/3rdparty/fast_float"
    git -C "$work/libplacebo/3rdparty/fast_float" checkout -q --detach "$fast_float_commit"
  else
    git -C "$work/libplacebo" submodule update --init --depth 1 -- 3rdparty/fast_float
  fi
  [[ $(git -C "$work/libplacebo/3rdparty/fast_float" rev-parse HEAD) == "$fast_float_commit" ]]
  git -C "$work/libplacebo" submodule status > "$evidence/libplacebo-submodules.txt"

  if [[ -n "$REUSE_ROOT" ]]; then
    git clone -q --no-hardlinks --no-checkout "$REUSE_ROOT/nv-codec-headers" "$work/nv-codec-headers"
  else
    git init -q "$work/nv-codec-headers"
    git -C "$work/nv-codec-headers" remote add origin https://github.com/FFmpeg/nv-codec-headers.git
    git -C "$work/nv-codec-headers" fetch -q --depth 1 origin "refs/tags/$NV_CODEC_VERSION:refs/tags/$NV_CODEC_VERSION"
  fi
  [[ $(git -C "$work/nv-codec-headers" rev-parse "$NV_CODEC_VERSION^{commit}") == "$NV_CODEC_COMMIT" ]]
  git -C "$work/nv-codec-headers" checkout -q --detach "$NV_CODEC_COMMIT"
  # This upstream tag is unsigned; the expected commit is pinned above.
  make -C "$work/nv-codec-headers" PREFIX="$dependencies" install \
    > "$evidence/nv-codec-install.log" 2>&1
  export PKG_CONFIG_PATH="$dependencies/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
  export LD_LIBRARY_PATH="$dependencies/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
  meson setup "$work/libplacebo-build" "$work/libplacebo" --prefix="$dependencies" \
    "${placebo_flags[@]}" > "$evidence/libplacebo-configure.log" 2>&1
  meson compile -C "$work/libplacebo-build" -j "$JOBS" > "$evidence/libplacebo-build.log" 2>&1
  meson install -C "$work/libplacebo-build" > "$evidence/libplacebo-install.log" 2>&1
  [[ $(pkg-config --modversion libplacebo) == "$LIBPLACEBO_VERSION" ]]
  for feature in vulkan vk_proc_addr glslang dovi; do
    [[ $(pkg-config --variable="pl_has_$feature" libplacebo) == 1 ]]
  done
  cp "$work/libplacebo-build/meson-info/intro-buildoptions.json" "$evidence/libplacebo-buildoptions.json"
  cp "$work/libplacebo-build/meson-info/intro-dependencies.json" "$evidence/libplacebo-dependencies.json"
  {
    printf 'ffmpeg_archive_sha256=%s\n' "$(sha256sum "$work/ffmpeg.tar.xz" | cut -d ' ' -f 1)"
    printf 'ffmpeg_archive_url=https://ffmpeg.org/releases/ffmpeg-%s.tar.xz\n' "$FFMPEG_VERSION"
    printf 'ffmpeg_signer=%s\nlibplacebo_commit=%s\nlibplacebo_signer=%s\n' \
      "$FFMPEG_KEY" "$LIBPLACEBO_COMMIT" "$LIBPLACEBO_SIGNER"
    printf 'ffmpeg_patch_sha256=%s\n' "$ffmpeg_patch_hash"
    printf 'ffmpeg_decoder_wakeup_patch_sha256=%s\n' "$ffmpeg_wake_patch_hash"
    printf 'ffmpeg_local_changes=strict_dolbyvision_zero_residual_mel,decoder_queue_receive_wakeup\n'
    for file in "${native_inputs[@]}"; do
      printf 'native_input=%s sha256=%s\n' "$file" "${native_hashes[$file]}"
    done
    printf 'libplacebo_fast_float_commit=%s\n' "$fast_float_commit"
    printf 'libplacebo_archive_sha256=%s\n' "$(git -C "$work/libplacebo" archive HEAD | sha256sum | cut -d ' ' -f 1)"
    printf 'nv_codec_commit=%s\nnv_codec_tag_signature=unsigned\n' "$NV_CODEC_COMMIT"
    printf 'nv_codec_archive_sha256=%s\n' "$(git -C "$work/nv-codec-headers" archive HEAD | sha256sum | cut -d ' ' -f 1)"
  } > "$evidence/sources.txt"
  for dependency in aom x264 x265 libass libdrm lcms2 opus vorbis vorbisenc libva vpl vulkan zimg ffnvcodec libplacebo; do
    dependency_version=$(pkg-config --modversion "$dependency")
    printf '%s\t%s\n' "$dependency" "$dependency_version"
  done > "$evidence/pkg-config-versions.tsv"

  tar -xJf "$work/ffmpeg.tar.xz" -C "$work"
  ffmpeg_source="$work/ffmpeg-$FFMPEG_VERSION"
  ffmpeg_baseline="$work/ffmpeg-baseline"
  ffmpeg_build="$work/ffmpeg-build"
  patched_sources=(libavfilter/vf_libplacebo.c fftools/thread_queue.h fftools/thread_queue.c fftools/ffmpeg_sched.c)
  (
    cd "$ffmpeg_source"
    sha256sum "${patched_sources[@]}" > "$evidence/ffmpeg-patch-input.sha256"
    patch --batch --forward --fuzz=0 -p1 < "$FFMPEG_PATCH" > "$evidence/ffmpeg-patch.log" 2>&1
    sha256sum "${patched_sources[@]}" > "$evidence/ffmpeg-baseline-source.sha256"
  )
  # Preserve a complete strict-DV baseline before applying the independent
  # scheduler repair. Generated headers and objects live outside either source.
  cp -a "$ffmpeg_source" "$ffmpeg_baseline"
  (
    cd "$ffmpeg_source"
    patch --batch --forward --fuzz=0 -p1 < "$FFMPEG_WAKE_PATCH" > "$evidence/ffmpeg-wakeup-patch.log" 2>&1
    sha256sum "${patched_sources[@]}" > "$evidence/ffmpeg-patch-output.sha256"
  )
  mkdir "$ffmpeg_build"
  (
    cd "$ffmpeg_build"
    "$ffmpeg_source/configure" --prefix="$FFMPEG_PREFIX" "${ffmpeg_flags[@]}" \
      --extra-ldflags="-Wl,-rpath,$dependencies/lib" > "$evidence/ffmpeg-configure.log" 2>&1
    make -j "$JOBS" > "$evidence/ffmpeg-build.log" 2>&1
    python3 "$NATIVE_HARNESS/run-native-regression.py" \
      --baseline "$ffmpeg_baseline" --candidate "$ffmpeg_source" --build "$ffmpeg_build" \
      --binaries "$work/native-regression-binaries" --evidence "$evidence/native-regression"
    make DESTDIR="$work/install-root" install-progs install-data > "$evidence/ffmpeg-install.log" 2>&1
    cp ffbuild/config.mak "$evidence/ffmpeg-config.mak"
    cp ffbuild/config.log "$evidence/ffmpeg-config.log"
  )
  staged="$work/install-root$FFMPEG_PREFIX"
  mkdir -p "$staged/metadata/licenses/ffmpeg" "$staged/metadata/licenses/libplacebo"
  cp -a "$evidence/." "$staged/metadata/"
  for license in "$work/ffmpeg-$FFMPEG_VERSION"/COPYING*; do
    [[ ! -f "$license" ]] || cp "$license" "$staged/metadata/licenses/ffmpeg/"
  done
  for license in "$work/libplacebo"/LICENSE*; do
    [[ ! -f "$license" ]] || cp "$license" "$staged/metadata/licenses/libplacebo/"
  done
  python3 "$RUNTIME_HELPER" "$staged" \
    --required-library libvulkan.so.1 --required-library libplacebo.so.351
  # Verify the relocated closure without depending on the temporary build libs.
  env -u LD_LIBRARY_PATH "$staged/bin/ffmpeg" -buildconf > "$staged/metadata/ffmpeg-buildconf.txt" 2>&1
  for flag in "${ffmpeg_flags[@]}"; do
    grep -Fq -- "$flag" "$staged/metadata/ffmpeg-buildconf.txt"
  done
  env -u LD_LIBRARY_PATH "$staged/bin/ffmpeg" -version > "$staged/metadata/ffmpeg-version.txt"
  env -u LD_LIBRARY_PATH "$staged/bin/ffprobe" -version > "$staged/metadata/ffprobe-version.txt"
  [[ $(head -n 1 "$staged/metadata/ffmpeg-version.txt") == "ffmpeg version $FFMPEG_VERSION "* ]]
  [[ $(head -n 1 "$staged/metadata/ffprobe-version.txt") == "ffprobe version $FFMPEG_VERSION "* ]]
  env -u LD_LIBRARY_PATH "$staged/bin/ffmpeg" -hide_banner -encoders > "$staged/metadata/encoders.txt" 2>&1
  env -u LD_LIBRARY_PATH "$staged/bin/ffmpeg" -hide_banner -filters > "$staged/metadata/filters.txt" 2>&1
  env -u LD_LIBRARY_PATH "$staged/bin/ffmpeg" -hide_banner -hwaccels > "$staged/metadata/hwaccels.txt" 2>&1
  for encoder in libx264 libx265 libaom-av1 h264_vaapi hevc_vaapi av1_vaapi; do
    grep -Eq "[[:space:]]$encoder[[:space:]]" "$staged/metadata/encoders.txt"
  done
  for filter in libplacebo subtitles zscale tonemap bwdif overlay; do
    grep -Eq "[[:space:]]$filter[[:space:]]" "$staged/metadata/filters.txt"
  done
  env -u LD_LIBRARY_PATH "$staged/bin/ffmpeg" -hide_banner -h filter=libplacebo \
    > "$staged/metadata/libplacebo-filter.txt" 2>&1
  for option in inputs apply_dolbyvision strict_dolbyvision strict_dolbyvision_profile deinterlace; do
    grep -Eq "[[:space:]]$option[[:space:]]" "$staged/metadata/libplacebo-filter.txt"
  done
  (
    cd "$staged"
    find . -type f ! -name installed-files.sha256 -print0 \
      | sort -z | xargs -0 sha256sum > metadata/installed-files.sha256
  )
  publish=$(mktemp -d "$TOOLCHAINS/.ffmpeg-$FFMPEG_VERSION.XXXXXXXX")
  cp -a "$staged/." "$publish/"
  chmod 755 "$publish"
  mv -T -- "$publish" "$FFMPEG_PREFIX"
  publish=''
fi

"$GO_PREFIX/bin/go" version
sed -n '1,3p' "$FFMPEG_PREFIX/metadata/ffmpeg-version.txt"
sed -n '1p' "$FFMPEG_PREFIX/metadata/ffprobe-version.txt"
echo "Toolchain installation complete: $FFMPEG_PREFIX"
echo 'The FFmpeg prefix can be moved as a unit; it requires a compatible system glibc and external GPU drivers.'
echo 'Encoder/filter enumeration is build evidence only. Hardware and media acceptance require separate execution.'
