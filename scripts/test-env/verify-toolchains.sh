#!/usr/bin/env bash
# Run only on the Linux verification host, after provisioning.
set -euo pipefail

set -a
source /opt/goby-test/test.env
set +a

[[ $(uname -s) == Linux ]] || { echo 'Verification requires Linux.' >&2; exit 1; }
go version
"$GOBY_FFMPEG" -version | head -3
"$GOBY_FFPROBE" -version | head -1
psql "$GOBY_TEST_DATABASE_URL" -v ON_ERROR_STOP=1 -Atqc \
  'SELECT current_user, current_database(), current_setting('\''server_version'\'')'

work=$(mktemp -d /opt/goby-test/toolchain-smoke.XXXXXXXX)
trap 'rm -rf -- "$work"' EXIT

"$GOBY_FFMPEG" -hide_banner -loglevel error \
  -f lavfi -i 'testsrc2=size=160x90:rate=24' \
  -f lavfi -i 'sine=frequency=440:sample_rate=48000' \
  -t 2 -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart \
  "$work/sample.mp4"
"$GOBY_FFPROBE" -v error -show_streams -show_format -of json \
  "$work/sample.mp4" > "$work/sample.json"
python3 - "$work/sample.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    media = json.load(source)
codecs = {stream["codec_type"]: stream["codec_name"] for stream in media["streams"]}
assert codecs["video"] == "h264", codecs
assert codecs["audio"] == "aac", codecs
assert float(media["format"]["duration"]) >= 1.9, media["format"]
print("Software H.264/AAC encoding and ffprobe stream extraction passed.")
PY

"$GOBY_FFMPEG" -hide_banner -loglevel error -i "$work/sample.mp4" \
  -c:v libx264 -c:a aac -f hls -hls_time 1 -hls_list_size 0 \
  -hls_segment_filename "$work/segment-%03d.ts" "$work/master.m3u8"
"$GOBY_FFMPEG" -hide_banner -loglevel error -i "$work/master.m3u8" -f null -
echo 'Software decode, HLS generation, and HLS decode passed.'

"$GOBY_FFMPEG" -hide_banner -hwaccels
encoders=$("$GOBY_FFMPEG" -hide_banner -encoders 2>/dev/null)
decoders=$("$GOBY_FFMPEG" -hide_banner -decoders 2>/dev/null)
for encoder in h264_nvenc h264_qsv h264_vaapi; do
  grep -E "[[:space:]]$encoder[[:space:]]" <<< "$encoders"
done
for decoder in h264_cuvid h264_qsv; do
  grep -E "[[:space:]]$decoder[[:space:]]" <<< "$decoders"
done
if ! compgen -G '/dev/dri/renderD*' > /dev/null && ! compgen -G '/dev/nvidia[0-9]*' > /dev/null; then
  echo 'No GPU device is exposed. Hardware codec enumeration passed; hardware execution is unverified.'
fi
