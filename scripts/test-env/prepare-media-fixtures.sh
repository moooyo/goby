#!/usr/bin/env bash
set -euo pipefail

# Synthetic media belongs only to this test deployment. No user library is read.
root=/opt/goby-fixtures
marker=goby-generated-media-fixtures
if [[ -d "$root" && ! -f "$root/.goby-managed" ]]; then
  echo "Refusing to write into an unmanaged media directory." >&2
  exit 1
fi
if [[ -f "$root/.goby-managed" && "$(cat "$root/.goby-managed")" != "$marker" ]]; then
  echo "Media fixture ownership marker does not match." >&2
  exit 1
fi
set -a
source /opt/goby-test/test.env
set +a
install -d -m 755 "$root" "$root/movies" "$root/tv/Example Series/Season 01" "$root/tv/Example Series/Season 02" "$root/music/Example Album"
printf '%s\n' "$marker" > "$root/.goby-managed"
"$GOBY_FFMPEG" -nostdin -hide_banner -loglevel error -y \
  -f lavfi -i 'testsrc2=size=160x90:rate=24' -f lavfi -i 'sine=frequency=440:sample_rate=48000' \
  -t 2 -c:v libx264 -preset ultrafast -pix_fmt yuv420p -c:a aac \
  -metadata title='Goby sample movie' -movflags +faststart "$root/movies/Goby Sample (2026).mp4"
cp "$root/movies/Goby Sample (2026).mp4" "$root/tv/Example Series/Season 01/Example Series S01E01.mp4"
cp "$root/movies/Goby Sample (2026).mp4" "$root/tv/Example Series/Season 01/Example Series S01E02.mp4"
cp "$root/movies/Goby Sample (2026).mp4" "$root/tv/Example Series/Season 02/Example Series S02E01.mp4"
"$GOBY_FFMPEG" -nostdin -hide_banner -loglevel error -y \
  -f lavfi -i 'sine=frequency=330:sample_rate=48000' -t 2 -c:a flac \
  -metadata title='First song' "$root/music/Example Album/01 - First Song.flac"
cp "$root/music/Example Album/01 - First Song.flac" "$root/music/Example Album/02 - Second Song.flac"
chmod -R a+rX "$root"
python3 - <<'PY'
from pathlib import Path

runtime = Path('/opt/goby-test/runtime.env')
lines = [line for line in runtime.read_text().splitlines() if not line.startswith('GOBY_MEDIA_ROOTS=')]
lines.append("GOBY_MEDIA_ROOTS='/opt/goby-fixtures'")
runtime.write_text('\n'.join(lines) + '\n')
browser = Path('/opt/goby-test/browser.env')
lines = [line for line in browser.read_text().splitlines() if not line.startswith(('GOBY_SMOKE_MEDIA_PATH=', 'GOBY_SMOKE_MEDIA_FILE='))]
lines += ["GOBY_SMOKE_MEDIA_PATH='/opt/goby-fixtures/movies'", "GOBY_SMOKE_MEDIA_FILE='/opt/goby-fixtures/movies/Goby Sample (2026).mp4'"]
browser.write_text('\n'.join(lines) + '\n')
PY
chmod 600 /opt/goby-test/runtime.env /opt/goby-test/browser.env
echo 'Synthetic movie, series, and music fixtures are ready; protected test configuration was updated.'
