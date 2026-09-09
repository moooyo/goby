#!/usr/bin/env bash
set -euo pipefail

# Build as the test operator, then execute real conversion tests as the same
# unprivileged account used by the service. The dedicated executable tmpfs
# avoids exposing root-only verification directories or filling the root disk.
if [[ "$(id -u)" != 0 || -z "${SSH_CONNECTION:-}" ]]; then
  echo "Run through SSH as the authorized test-env operator." >&2
  exit 1
fi
repository=${1:-/opt/goby-test/verify-m4a-20260909}
work=/opt/goby-m4a-check
owner=/opt/goby-test/m4a-nonroot.owner
marker=goby-m4a-nonroot-verification-v1
exec 9>/opt/goby-test/m4a-nonroot.lock
flock -n 9 || { echo "A Goby non-root verification is already running." >&2; exit 1; }
if [[ -e "$work" || -L "$work" ]]; then
  echo "The temporary verification mount path already exists." >&2
  exit 1
fi
if [[ -e "$owner" ]] && [[ "$(cat "$owner")" != "$marker" ]]; then
  echo "The non-root verification ownership marker does not match." >&2
  exit 1
fi
id goby >/dev/null
set -a
source /opt/goby-test/test.env
set +a
export GOCACHE=/dev/shm/goby-go-cache GOMODCACHE=/dev/shm/goby-go-mod
export GOTMPDIR=/opt/goby-test/exec-scratch TMPDIR=/opt/goby-test/exec-scratch
mkdir -m 755 "$work"
mounted=false
cleanup() {
  cd /
  if [[ "$mounted" == true ]]; then
    if [[ "$(cat "$owner")" != "$marker" ]] || ! umount "$work"; then
      echo "The owned verification mount could not be released." >&2
      return 1
    fi
  fi
  rmdir "$work"
}
trap cleanup EXIT
umask 077
printf '%s\n' "$marker" > "$owner"
mount -t tmpfs -o size=128m,nosuid,nodev,mode=0755 tmpfs "$work"
mounted=true
install -d -o goby -g goby -m 700 "$work/tmp"
cd "$repository"
go test -race -c -o "$work/transcode.test" ./internal/transcode
chmod 755 "$work/transcode.test"
cd "$work"
export TMPDIR="$work/tmp" GOTMPDIR="$work/tmp"
echo "Running conversion verification as the unprivileged goby account."
/usr/sbin/runuser --user goby --preserve-environment -- "$work/transcode.test" -test.v \
  -test.run '^(TestManagerPersistsAndDecodesPlannedMedia|TestRunActualFFmpegEncodeRemuxAndAudioOnly|TestRunCancellationKillsEntireProcessGroup|TestRunSuccessfulParentExitRetiresSurvivingChildren)$'
echo "Unprivileged conversion verification passed."
