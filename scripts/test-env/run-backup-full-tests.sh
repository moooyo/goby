#!/bin/bash
# Run only inside the pair operator's exclusive bounded Linux unit.
set -euo pipefail
test "${GOBY_TEST_BACKUP_DISPOSABLE_DATABASES:-}" = 1
test -n "${GOBY_TEST_BACKUP_SOURCE_DATABASE_URL:-}"
test -n "${GOBY_TEST_BACKUP_TARGET_DATABASE_URL:-}"
test "${GOBY_TEST_DATABASE_URL:-}" = "$GOBY_TEST_BACKUP_SOURCE_DATABASE_URL"
export GOBY_FFMPEG=/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg
export GOBY_FFPROBE=/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe
go=/opt/goby-toolchains/go1.27.1/bin/go
mapfile -t packages < <("$go" list ./...)
test "${#packages[@]}" -ge 24
printf '%s\n' "${packages[@]}" > "$GOTMPDIR/packages.txt"
ordinary=()
for package in "${packages[@]}"; do
    if [ "$package" != github.com/moooyo/goby/internal/recoverydb ]; then
        ordinary+=("$package")
    fi
done
# recoverydb intentionally retains its complete public fixture pair for the
# surrounding operator's exact cleanup, so it runs after every other package.
"$go" test -race -p=1 -count=1 -timeout=20m -json "${ordinary[@]}"
"$go" test -race -p=1 -count=1 -timeout=12m -json ./internal/recoverydb
CGO_ENABLED=0 "$go" build -p=1 -trimpath -buildvcs=false -o "$GOTMPDIR/goby-linux-amd64" ./cmd/goby
chmod 0755 "$GOTMPDIR/goby-linux-amd64"
