# Main recovery materials and execution-order review

Reviewed on 2026-09-14. The [checkpoint](audited-main-recovery-materials.json)
extends [startup preparation](audited-main-startup-preparation.md). Old
installation preservation is complete. Key authentication and a fresh native
schema27 recovery point remain open. Main is inactive with PID0; no capacity
profile, service start, migration or restore was applied in this scope.

## Preserved installation

The old executable remains `/opt/goby-dev/goby`, 28,172,723 bytes, SHA256
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
All 415 installed administrator files and their two directories match the
saved source32 installation inventory. This includes retained asset versions;
the selected candidate's 57-asset inventory does not replace the old inventory.
Only Goby's own installation files were inspected.

A new private installation archive contains only the executable and these
administrator assets. Its size is 16,590,013 bytes, SHA256
`f24ad82c66d7a8a1ba2128ed547210a95804f8cd62448ba5ffdb2a14fcac66fb`.
Every member was read back and checked for bytes/hash, type, mode, UID/GID and
whole-second modification time; the gzip stream was also closed and verified.
The separate inventory retains nanosecond metadata. Before/after source
metadata and path sets were exact. No archive was extracted or installed.

The archive is root-owned, mode0600, inside the new root-owned mode0700 scope
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/main-recovery-materials-01`.
It excludes database data, keys, environment, lifecycle state, logs, media and
native backup objects. It is installation material, not a database recovery
point or demonstrated rollback.

## Private material inventory

The five files in the existing operator-secret directory are present as regular
single-link mode0600 files. Their bodies, including the historical archive and
passphrase, were not read. The first metadata comparison incorrectly compared
`mtime_ns`/`ctime_ns` with `mtimeNs`/`ctimeNs`. A separate saved reconciliation
maps only those field names and decimal representations; every historical
file metadata value then matches. The first report is retained unchanged.
Metadata equality does not authenticate the archive or its passphrase.

The existing browser credential environment contains its four nonempty expected
keys and matches the saved historical content. No values or individual secret
hashes were published, and no login was attempted. Captured runtime/unit files,
lifecycle/control/backup metadata and the selected default master's metadata
remain exact. The master is an existing 32-byte, mode0600 file owned by UID995.
Its contents were not read in this scope. The retained native archive is still
schema23 and does not satisfy the required fresh schema27 recovery point.

## Retired standalone key observation

A small native witness executable was built successfully on `test-env` with
Go 1.27.1 against copied, pinned sources and modules. Both build workers closed
and the temporary build workspace was removed. The controller was independently
reviewed and syntax-parsed remotely, but its actual preflight rejected the
executable mode: the build left `0500`, while the privilege-dropping launcher
required `0555`. The handoff had incorrectly described the executable as `0555`.
The executable's bytes, owner and size matched; its mode was not changed.

The failure `witness_binary_metadata_changed` happened before starting the Go
process. There were zero observer executions, database connections, master reads
or ACKs. The existing deployment lock was released unchanged, and the final
runtime and protected metadata checks passed. `backendExitObserved=false` in
this failed receipt does not mean a backend was left running: no frontend or
backend was created. Preserve this failure rather than claiming a key result.

The separate observer will not be retried. Source review found that the initial
primary/revision0/default-generation startup only constructs the
[vault](../../internal/identity/application_key_vault.go); it does not load or
create its master. Ordinary administrator login does not require application-key
decryption. Both source32 and the current [native engine](../../internal/recovery/engine.go)
already call `WitnessBackup` before `Facts` and `Dump` in the same exported,
read-only repeatable-read snapshot. Its [witness](../../internal/identity/application_key_backup.go)
authenticates sealed history, including revoked keys, and never creates a
master. This is the required archive authentication point. Safe master metadata
and correct generation selection remain pre-start inputs; a changed generation
or pending transition requires fresh review. An empty sealed-key history would
not prove a positive cryptographic match to existing encrypted records.

This consolidation preserves the key gate and removes duplicate preparation.
It does not infer successful authentication from metadata or from the failed
observer. A failed native create can already have operation, writer and audit
effects. The actual source32 failure path and resource terminal states must be
checked, including errors not propagated by abort/terminal persistence. Source32
also predates the current engine's `ValidateDump` call; current guarantees are
not automatically old-binary guarantees.

## Corrected dependency order

The previous plan paused the same video/diagnostic cycle while making source32
backup wait for core video acceptance. It also mixed the archive output into
general pre-backup inputs and placed all remaining M2-M6 work after promotion.
Those dependencies were unnecessarily restrictive and could stall delivery.

The [execution plan](../planning/current-execution-plan.md) and
[upgrade contract](audited-main-upgrade-plan.md) now specify:

1. Freeze and independently review one source32 native-backup input, using the
   existing metadata, installation archive, private material locations and
   startup observations. Admit the capacity transition, one invocation, one
   owned administrator session and one create request with finite budgets and
   declared writes. This checkpoint itself admits none of those actions.
2. Obtain native key authentication, one complete schema27 archive and its
   SourceFacts, then reconcile the later operational deltas and return main to
   inactive. These are workflow outputs, not prerequisites for creating it.
3. Rehearse that same archive in distinct isolated schema28 and actual
   source32/schema27 targets. Neither old-binary backup nor these rehearsals
   depend on new-product video acceptance; their own safety gates still apply.
4. Require both supported core acceptance and complete safety/recovery evidence
   before new-binary main promotion. Independent M2-M6 obligations proceed under
   their own prerequisites and close only their actual scope.

The final available-space observation is 475,693,056 bytes. The prepared backup
admission floor remains 302,055,424 bytes and must be rechecked at admission;
installed defaults still do not fit. All verification in this increment ran
through `ssh test-env`. No product code changed or full product suite was
repeated. MP3/FLAC acceptance remains valid; video failures, unresolved media
timing, the fresh recovery point, both restoration proofs and M2-M6 remain open.
