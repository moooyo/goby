# Main startup and capacity preparation

Reviewed on 2026-09-14. The [checkpoint](audited-main-startup-preparation.json)
extends the [main identity observation](audited-main-readonly-preparation.md).
Current configuration parsing and the bounded startup-data read are complete.
One missing, empty runtime directory has been prepared. The capacity profile
is staged; main remains inactive and its installed configuration is unchanged.
No main startup, backup creation, migration or restoration is admitted here.

## Configuration and unit policy

A small observer called the real `config.Load` using the captured environment
and `/var/lib/goby-test` working directory. The actual parsing/default functions
and their relevant dependency methods match source32
`b9bb7b1cf11e07e011a6e1726ddb9d53ef8e1fe9`. Five Goby package source copies and
the required module inventory were checked against the selected source and
module manifests, then built with Go 1.27.1 in a fresh private tmpfs workspace.
No old source, module cache or build cache was changed. The build and both
observer processes exited successfully; their temporary workspace was removed.

The observer started with an empty process environment and accepted only the
configuration package's declared keys through stdin. Passwords and setup token
values were not returned. Both the current and proposed configurations passed;
an exact comparison allows only the three backup-policy differences below.
This validates parsing and defaults, not credential authentication, executable
access, key material, store opening or startup safety.

Fresh unit reads match the existing fragment, drop-ins, inline environment and
ordered environment-file references. Verified source files contain one
`ExecStart=/opt/goby-dev/goby` and no other execution commands. This conclusion
does not infer empty hooks from omitted `systemctl show` arrays. The current
`Restart=on-failure` and `RestartSec=3s` require a separately frozen one-invocation
policy before any backup start. Current application startup and recovery
operation timeouts are five and thirty minutes respectively.

The first unit projection mishandled repeated `EnvironmentFiles` properties as
a scalar and failed. Its failure is retained; the corrected read preserves
both entries in order. Neither projection changed the unit or environment.

## Capacity decision

Use the separately reviewed [main fixture profile](audited-main-backup-capacity.env.example)
when an authorized configuration transition is admitted:

| Setting | Installed value | Prepared value |
| --- | --- | --- |
| Maximum object | 8 GiB | 64 MiB |
| Maximum total | 32 GiB | 256 MiB |
| Minimum free | 512 MiB | 128 MiB |

Keep the 128-object limit and all other configuration unchanged. This is an
explicit profile for this small test deployment, not an inherited candidate
setting or a change to product defaults. It remains outside application
configuration files and has not been installed.

Source32's open check requires the minimum-free policy plus 33,619,968 bytes
of metadata reserve. Backup creation also reserves scratch space up to the
object limit. The installed defaults therefore require 570,490,880 bytes to
open and 9,160,425,472 bytes for the initial scratch reservation. Merely making
the open check pass would not make the backup workflow fit.

For the prepared profile, require at least 302,055,424 available bytes before
dispatch: two maximum-size objects, the 128 MiB free-space floor and metadata
reserve. The final observation had 512,126,976 available bytes. The database's
observed physical size is 47,494,835 bytes; this is not a proof of dump or
encrypted archive size. The actual native archive must complete within its
limits, and capacity must be checked again at admission. Preserve the old
schema23 object and all historical evidence.

## Actual startup candidates

One fixed read-only repeatable-read connection observed the following after
checking the required ordinary tables, owners and referenced built-in column
types. No password, token, application-key value, media path or activity body
was selected. All result lists fit their declared bounds.

| Area | Observation |
| --- | --- |
| Bootstrap | Setup is complete; ten users include four enabled administrators with passwords. Credentials were not authenticated |
| Managed settings | One singleton at revision 8, deployment server-name mode and no four-limit overrides |
| Scan recovery | No queued/running scan or linked active-child candidates |
| Task recovery | No pending/running/stopping run or linked-child candidates |
| Encoding recovery | No queued/running encoding jobs; transcoding is enabled |
| Scheduling | The enabled `library.scan` definition matches compiled presentation values; no eligible triggers; five libraries |
| Activity retention | Thirty-day policy confirmed; no rows expired at the observed database clock |

These observations narrow expected startup changes. They do not make normal
startup read-only: `ServerID()` still upserts its existing value, diagnostics
opens a new active file, and newly changed data would need fresh review.

The first startup observation stopped before opening a database connection
because socket timestamps differed from the earlier record. All process,
socket identity, ownership and listener fields remained exact. PostgreSQL 17.11
periodically refreshes those timestamps through
[the postmaster loop](https://github.com/postgres/postgres/blob/REL_17_11/src/backend/postmaster/postmaster.c#L1825)
and [the socket touch function](https://github.com/postgres/postgres/blob/REL_17_11/src/backend/libpq/pqcomm.c#L829).
The interval observed here is consistent with that mechanism; the exact syscall
cause was not traced. The correction excludes only socket mtime/ctime from
cross-time identity equality, retains complete observations and checks every
other field exactly. Seventeen remote saved-snapshot checks passed, including
rejections for changed inode, owner, mode, listener and missing timestamps.
The corrected observation committed and its frontend/backend exited; the
existing deployment lock was released unchanged. The first failure remains.

## Runtime directory and diagnostics

`/dev/shm/goby-transcodes-test` was absent, although the existing non-optional
`ReadWritePaths` refers to it. The installed systemd execution manual describes
this namespace path requirement. A bounded prerequisite operation exclusively
created an empty directory with owner/group `goby:goby` (995:986), mode `0700`,
device 26 and inode 5215. It created no cache marker, lock or job files and
started no service. Final checks confirm the directory is still empty and
owned. This prepares the current boot; tmpfs provisioning after a later reboot
remains an operational requirement, not a proven reboot result.

The diagnostic registry and file identities match: nine files are closed and
one is registered unclosed. Its complete 413-byte tail ends at a record
boundary, so the reviewed recovery branch would mark it closed at size 413
without truncation. Only that bounded tail was read privately; no log body was
published. Current count and age imply no pruning on the next open at this
observation. The expected old-registry update and new active log must remain
explicit in a future startup write scope.

## Next boundary

The subsequent [material review](audited-main-recovery-materials.md) preserves
the old installation and inventories private material. Its separate key observer
was rejected before execution and is retired; the native backup must perform
the witness in its own archive snapshot. Next freeze one concrete source32
backup input and finite invocation policy. Apply the staged capacity profile
only through that admitted transition. Old-binary backup and isolated recovery
rehearsals follow their own safety gates; new-binary main promotion still
requires core acceptance and complete recovery proof. The fresh schema27 point,
both distinct restoration proofs and all remaining M2-M6 obligations remain
open. This increment did not change product code or rerun the full product suite.
