#!/bin/bash
# Run only inside the paired verifier's bounded Linux unit.
set -euo pipefail
test "${GOBY_TEST_BACKUP_DISPOSABLE_DATABASES:-}" = 1
test -n "${GOBY_TEST_BACKUP_SOURCE_DATABASE_URL:-}"
test -n "${GOBY_TEST_BACKUP_TARGET_DATABASE_URL:-}"
test "${GOBY_TEST_DATABASE_URL:-}" = "$GOBY_TEST_BACKUP_SOURCE_DATABASE_URL"
/opt/goby-toolchains/go1.27.1/bin/go test -race -p=1 -count=1 -timeout=10m -json ./internal/backuppg
/opt/goby-toolchains/go1.27.1/bin/go test -race -p=1 -count=1 -timeout=10m -json -run '^TestDeploymentLease' ./internal/database
# This final package deliberately owns both complete public schemas. No later
# suite may silently reuse its retained active database within the same pair.
/opt/goby-toolchains/go1.27.1/bin/go test -race -p=1 -count=1 -timeout=10m -json ./internal/recoverydb
