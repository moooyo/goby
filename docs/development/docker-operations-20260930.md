# Docker operations verification - September 30, 2026

Status: **COMPLETE within the selected Docker operations scope**. Fourteen helper
unit tests, the selected actual software Docker journey and owned-resource
closure passed on `test-env`. Git integration is separate; no merge or push is
claimed here. Final toolkit size/hash and evidence references are recorded in the
[machine-readable result](docker-operations-results-20260930.json).

The [operations plan](../planning/docker-operations-plan-20260930.md) adds a small
Python 3.10+ standard-library helper and [quick start](../../deploy/oci/QUICKSTART.md).
It reuses existing Docker images and external PostgreSQL 17. There is no new Go
binary, application image, bundled database or native installer. Further scraping
and provider work is explicitly deferred; this task needs no provider credentials.

## Reused delivery and toolkit

| Input or artifact | Identity or state |
| --- | --- |
| Operations baseline | `553c9d4` |
| Unchanged application source | `76d64bf0087f3cd2e40addde8067f4e6e65b2bac` |
| Software image | `sha256:fa13611dbb940a2a876d9f52606d3bc05ccc6364b4f5fc8de19bec86204c3cb4` |
| AMD image | `sha256:a35b7653d262afcf006b95f31f450b58d89ce11b886279e2685a8a2ea0d3178e` |
| Release catalog | `deploy/oci/current-release.json`, binding both existing profile archives and their immutable image IDs |
| Toolkit packaging | `goby-docker-operations.zip`, containing helper, catalog, quick start, both profiles' Compose/seccomp companions and required local guides |
| Local handoff destination | `D:/Code/goby/.artifacts/docker-operations-20260930/goby-docker-operations.zip` |
| Toolkit size and SHA-256 | Recorded in the machine-readable result |

The [provider delivery record](online-providers-20260930.md) retains the image
archive sizes, hashes and prior acceptance. Existing archives from
`D:/Code/goby/.artifacts/oci-providers-20260930` are reused; they do not need to be
downloaded again, and the small toolkit does not duplicate them. It serves only
the current catalog's accepted images. Companion integrity belongs to each
selected profile's `companionHashes`, not the catalog root. Static review caught
that lookup boundary and the corrected behavior has unit coverage.

## Focused checks

All **14 remote unit tests passed**. The actual negative checks also established:

- An archive with unchanged byte count but incorrect content is rejected.
- A mismatched companion is rejected before creating the installation target.
- Repeating `prepare` refuses the existing installation and preserves its files.
- A check-name collision preserves the pre-existing file.
- Temporary check containers are cleaned up; none remained after these checks.

## Actual software Docker journey

Preparation consumed the existing 388,846,080-byte software archive and matched
SHA-256 `f35b29ffb4257f04d607d6d67d61b1dfda290a84f7b83e5db4b0509de0ba92e1`.
The release catalog was read and checked. The installation path contained spaces,
private configuration files had mode `0600`, and all three state/cache/log
directories had UID/GID `10001:10001` and mode `0700`. Database CA and recovery
database bindings were exercised. Media ownership was unchanged. Preparation
did not start the application.

The installed helper then completed these observed steps:

1. `check` used the actual application UID. Unreadable media produced
   `media_not_readable`; after correcting the fixture's original permissions it
   passed. Its result explicitly did not claim database authentication.
2. `start` reached ready, administrator bootstrap and the UI response succeeded,
   and one movie was scanned. Static playback matched the original bytes with
   SHA-256 `c618a834014ffb1cbae9d14ef6e33d2bd1dada2b60a12f43e3d4759bc063015f`.
3. Changing only the desired host port without recreating the container produced
   `container_port_mismatch`. Status inspected the actual loopback `8096/tcp`
   mapping before any readiness probe, preventing diagnosis of another instance.
4. PostgreSQL on port `55995` was stopped. The application may either remain
   available with HTTP `503`/`not_ready` or exit with code 1 after losing its
   catalog lease. The observed exit was diagnosed using the sanitized
   `server.stopped` event's `error_class`, producing `database_connection_lost`.
5. After PostgreSQL recovered, `start` restored readiness. The catalog item ID
   and exact playback bytes persisted. A subsequent normal `stop` exited 0.

Status-query exit zero means the query itself succeeded; callers must inspect
the returned status for readiness. A failed start retains the container and
data. The helper does not restart an unrelated project or attempt database
rollback. `installation.json` retains the initial delivery receipt, while
operations use the explicitly selected immutable ID in `deployment.env`.

## Retained failed attempts

`runtime01` incorrectly expected a nested error field in the fixture's response
assertion. `runtime02` used a fresh installation prefix, then incorrectly required
a PostgreSQL stop to produce HTTP `503` in every case. The real application exited
after catalog lease loss, so that assertion failed.

The helper gained a safe terminal database-loss diagnosis and the driver accepted
the actual production outcomes. `runtime03` reran the affected PostgreSQL outage
and recovery portion successfully. Earlier successful preparation/playback
evidence is reused explicitly; the failed attempts are retained and are not
silently counted as passing full runs.

## Scope and completed closure

The actual journey establishes the software installation/operations path. The
existing AMD image and its prior hardware acceptance remain valid; this increment
does not claim a new AMD GPU run or a new browser E2E campaign. Default read-only
root/media, non-root application UID, resource limits and `restart: "no"` remain.
Writable subtitles are an explicit optional override.

| Closure item | State |
| --- | --- |
| Owned containers and networks | Zero remaining |
| Isolated PostgreSQL on port `55995` | Zero application/restore clients before normal fast stop; no `postmaster.pid` remains |
| Three protected containers | IDs, PIDs and start times unchanged |
| Database, installations, configuration, media and failed evidence | Retained under `/opt/goby-docker-operations-20260930-01` |
| Closure evidence | `/opt/goby-docker-operations-20260930-01/artifacts/closure.json` |
| Toolkit ZIP identity and handoff | Recorded in the machine-readable result |
| Git integration | Handled separately; no completion claim |

The helper's normal `stop` retains its container, network and persistent data.
The zero-container/network closure was a separate acceptance-fixture retirement
using operator `compose down`; it is not an additional effect of helper `stop`.

Manual updates retain the existing backup-first process: preserve the old image
ID, back up the database and matching state/master key, load a verified archive,
edit `deployment.env`, and check/start/readiness. The helper has no upgrade
command and makes no automatic database rollback promise.
