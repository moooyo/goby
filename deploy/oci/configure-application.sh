#!/bin/sh
# Bind inherited tools and the optional distribution fingerprint tool to this
# application. These build checks are not runtime or GPU acceptance.
set -eu

case "$1" in
  0) ;;
  1)
    # Keep the established primary FFmpeg and private libplacebo unchanged.
    # Only fingerprint analysis selects the independent distribution FFmpeg.
    rm -f /etc/apt/sources.list.d/*
    printf '%s\n' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/20260915T000000Z trixie main' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian/20260915T000000Z trixie-updates main' \
      'deb [check-valid-until=no] http://snapshot.debian.org/archive/debian-security/20260915T000000Z trixie-security main' \
      > /etc/apt/sources.list
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends 'ffmpeg=7:7.1.5-0+deb13u1'
    rm -rf /var/lib/apt/lists/*
    ;;
  *) exit 2 ;;
esac

test "$(dpkg-query -W -f='${Version}' ffmpeg)" = '7:7.1.5-0+deb13u1'
sha256sum --check --strict /usr/share/goby/application-expected-executables.sha256
evidence=/usr/share/goby/application-runtime
install -d -m 0755 "$evidence"
dpkg-query -W -f='${binary:Package}\t${Version}\t${Architecture}\n' > "$evidence/runtime-packages.tsv"
cp /etc/apt/sources.list "$evidence/apt-sources.list"
cp /usr/share/doc/ffmpeg/copyright "$evidence/analysis-ffmpeg.copyright"
"$GOBY_FFMPEG" -version > "$evidence/ffmpeg-version.txt"
"$GOBY_FFPROBE" -version > "$evidence/ffprobe-version.txt"
/usr/bin/ffmpeg -version > "$evidence/analysis-ffmpeg-version.txt"
/usr/bin/ffmpeg -hide_banner -h muxer=chromaprint > "$evidence/analysis-chromaprint-options.txt" 2>&1
grep -Eq '[[:space:]]-?fp_format[[:space:]]' "$evidence/analysis-chromaprint-options.txt"
"$GOBY_FFMPEG" -hide_banner -filters > "$evidence/ffmpeg-filters.txt" 2>&1
for filter in blackframe blackdetect entropy signalstats silencedetect; do
  grep -Eq "[[:space:]]$filter[[:space:]]" "$evidence/ffmpeg-filters.txt"
done
fingerprint=/opt/goby-intro-fingerprint/bin/goby-intro-fingerprint
fingerprint_sha256=$(sha256sum "$fingerprint" | cut -d ' ' -f 1)
analysis_sha256=$(sha256sum /usr/bin/ffmpeg | cut -d ' ' -f 1)
printf '{"enabled":true,"cacheDirectory":"/var/cache/goby/analysis","fingerprintPath":"%s","fingerprintSHA256":"%s","introFFmpegPath":"/usr/bin/ffmpeg","introFFmpegSHA256":"%s"}\n' \
  "$fingerprint" "$fingerprint_sha256" "$analysis_sha256" > /usr/share/goby/media-analysis.json
sha256sum /usr/local/bin/goby "$GOBY_FFMPEG" "$GOBY_FFPROBE" /usr/bin/ffmpeg "$fingerprint" \
  "$GOBY_PG_DUMP" "$GOBY_PG_RESTORE" > /usr/share/goby/runtime-executables.sha256
if test -d /usr/share/goby/amd-runtime; then
  cp /usr/share/goby/runtime-executables.sha256 /usr/share/goby/amd-runtime/runtime-executables.sha256
fi
sha256sum /usr/share/goby/build-manifest.json /usr/share/goby/oci-artifact-binding.json \
  /usr/share/goby/application-source.json /usr/share/goby/media-analysis.json \
  /usr/share/goby/runtime-executables.sha256 > "$evidence/application-files.sha256"
find /usr/share/doc/goby "$evidence" -type f -exec chmod 0444 {} +
find /usr/share/doc/goby "$evidence" -type d -exec chmod 0755 {} +
chmod 0444 /usr/share/goby/media-analysis.json /usr/share/goby/runtime-executables.sha256
