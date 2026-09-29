# Phase 3 final verification entry - September 29, 2026

Status: **executed and closed within the revised functional scope**. Results,
original failures and affected-package repeats are recorded in the
[functional closeout](phase3-functional-closeout-20260929.md), followed by its
[publication](phase3-functional-publication-20260929.md). The commands below are
the retained execution recipe, not a request to repeat the completed campaign.

This was the next step after the accepted
[blocked-scan and guest recovery increment](phase3-scan-guest-recovery-20260929.md).
The recipe uses one clean archive of the final committed source on `test-env`. Do not copy
unrelated dirty packaging/notices work from the local checkout. Local work may
compile; the commands below execute remotely.

## Environment

Provision one ordinary integration database and one disposable source/target
pair, with three independent non-administrative owner roles. Use fresh owned
database names and retain the previously accepted CLI/guest databases unchanged.
The two backup databases and roles must have different `goby_backup_` names.
Provide explicit loopback host/port and `sslmode=disable` in their URLs.

Required environment:

- `GOBY_TEST_DATABASE_URL`
- `GOBY_TEST_BACKUP_SOURCE_DATABASE_URL`
- `GOBY_TEST_BACKUP_TARGET_DATABASE_URL`
- `GOBY_TEST_BACKUP_DISPOSABLE_DATABASES=1`
- `GOBY_TEST_RECOVERY_PORT`, matching the private PostgreSQL listener
- `GOBY_TEST_PG_DUMP` and `GOBY_TEST_PG_RESTORE`, from PostgreSQL 17
- `GOBY_FFMPEG` and `GOBY_FFPROBE`, using the verified 9.0.1 tools
- `GOBY_INTRO_FINGERPRINT`, using the previously verified native helper
- Owned `TMPDIR` and `GOTMPDIR`

Use Go 1.27.1 and a Node version supporting type stripping and the frontend's
engine constraint. Do not inherit destructive `GOBY_PHASE3_*` opt-in flags.
Version 9.0.1 alone is insufficient: preflight the software encoders `libx264`,
`libx265`, and `libaom-av1`, plus `zscale`, `tonemap`, and subtitle filters.
The recorded complete `9.0.1-goby-cb8b6d298456` build is suitable; the smaller
`/opt/goby-toolchains/ffmpeg-9.0.1` build lacks modern encoders and `zscale`.
Keep the selected prefix first on PATH as well as setting both explicit tool
variables, and record executable hashes with the results.
With packages and test cases serialized and fixture cleanup successful, the
backup pair can be used in the order `backuppg`, `recovery`, then `recoverydb`.
The last package intentionally retains its restored target even after success;
do not subsequently reuse that pair for an empty-target fixture. A failed
fixture also remains available for diagnosis. The old runner's seven-database
layout is not a prerequisite for this ordered sequence.

## Commands

Create `artifacts/` first and retain command exit statuses and raw output.

```sh
npm --prefix web/admin ci --no-audit --no-fund
npm --prefix web/admin run build
node --experimental-strip-types --test web/admin/src/libraryOptions.test.ts web/admin/src/sortingSettings.test.ts web/admin/src/mediaAnalysis.test.ts web/admin/build/frontend-contributions.test.ts
node --test scripts/build-release-contributions.test.mjs
python3 -I -B scripts/test-env/test-media-analysis-phase3-workload.py
python3 -m py_compile scripts/test-env/phase3-cli-supervisor-recovery.py scripts/test-env/phase3-guest-lifecycle-recovery.py
go test -json -p=1 -parallel=1 -count=1 -timeout=90m ./...
go test -json -tags=goby_embed_admin -p=1 -parallel=1 -count=1 -timeout=15m ./cmd/goby
go build -trimpath -o artifacts/goby ./cmd/goby
go build -trimpath -tags=goby_embed_admin -o artifacts/goby-embedded ./cmd/goby
```

The frontend build already performs TypeScript checking. The ordinary Go suite
includes migrations and migration rollback/timeouts, real dump/restore and
historical schema restoration, the temporary-relation regression, whole-database
recovery binding/lease boundaries, and encrypted recovery apply/restart/rollback.
These provide the representative migration/backup/recovery composition.
Its existing `TestCatalogRealMediaCapacityScanAndCachedRescan` also creates
10,688 catalog items. That is part of the ordinary suite and is distinct from
rerunning the already accepted standalone Phase 3 capacity campaigns.

## Closeout rules

Ordinary PostgreSQL, media, and backup integration tests must not be silently
skipped because configuration is missing. Separate explicit opt-in skips from
passes and cite the existing actual fault/guest evidence for their scope. Do
not rerun the accepted 10k media, 100k catalog, or guest campaigns merely to
recreate old controller labels.

Do not run the complete Playwright directory against one generic instance:
those tests have different fixture requirements. With no UI change in the
current repairs, retain the earlier browser evidence within its original scope;
if a new UI-affecting change appears, select and provision its actual scenario.

If the sequence passes, close its owned services and database runtime, preserve
results, and write the final source/build/acceptance summary with explicit
remaining historical performance and packaging limits. A failure should trigger
a focused diagnosis and only the affected repeats. The VM106/C12/final54
controller chain is not required to execute this plan.
