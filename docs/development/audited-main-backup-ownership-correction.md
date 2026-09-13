# Main backup ownership correction

Status: **native backup created; HTTP/data/files/service and policy cleanup closed**.
This correction follows the consumed [startup-only scope](audited-main-native-backup-plan.md).
Its private output directory is the child `after-ownership-review` under that
same remote experiment. The original two controller failures, one service
start/stop, zero HTTP requests and zero backup creates remain explicit inputs.
No prior executable, receipt, database or private store is replaced or reset.

## Actual completion

The [completed checkpoint](audited-main-native-backup-completed.json) binds one
native create and full download, 13 complete HTTP calls, logout204 and rejection
of the same cookie with401. Archive `69f5e597c99f65099647cd3322d3174c` contains
schema27, is 196,310 bytes and has SHA256
`0a61cbd6c6ff23543ba873ed1a44dd33ecce2f8702f956e24096862996c7874f`.
The native source-key witness passed with four application-key rows. The actual
archive snapshot contains 405 rows; the operational database contains 408 rows,
including exactly one revoked administrator session and five ordered audits.
All 402 original rows and the other four sequences remain exact.

The native stores published one verified ready object and control revision9;
the cache and eleven old logs remained exact and one new log closed. The owned
invocation stopped normally. Final policy cleanup first rejected a metadata
change on the shared `/run/systemd/system` parent, after removing the fence and
reloading. Its failure path restored the exact fence and retained the original
failed execution receipt. No business request or service start was repeated.

One independently reviewed cleanup then checked the shared parent's historical
device/inode/UID/GID/mode and current directory type, while retaining exact
ownership and file checks for the restored fence and child directory. Shared
parent timestamps, size and link count were not treated as exclusive ownership.
The exact fence and empty child directory were removed, the manager reloaded,
and the original restart policy restored. Capacity remains installed, main and
source55 are inactive, and the deployment lock was released unchanged. Final
archive bytes and protected external state were checked again.

| Actual result | SHA256 |
| --- | --- |
| `checkpoint.json` | `3a9ff1ddf45213331aa4f23d66d1beedd44da9f18df8e168e9e56badc6a925cf` |
| `execution.json` with preserved cleanup failure | `2f19b9790d836a9f7548fbd949b670cac4156d6144edd0344affd1e99ad09de5` |
| `policy-closeout.json` | `8c05d6628961f34ca398534ca83dce3099e8f8205e6cd38107b2678e9edb16d4` |
| `controller-data-closeout.json` | `606bcac3d1ac56026ac6c3e5781f64028829582981cd48e33e0ce9e3f67b4f7d` |
| `controller-file-closeout.json` | `633d296c9f62c96d3839e16c00190da6c8343fd46b3a654152080d1ccd02ed66` |

The rest of this document preserves the admitted behavior and bounds. This
scope is consumed. The archive is ready to become an input to the distinct
source32/schema27 and selected/schema28 restoration proofs; neither restoration
or archive-passphrase restoration authentication has run yet.

## Corrected behavior

The service adapter first verifies that the submitted start owns the actual
PID, start ticks, systemd invocation and start-time window, boot, UID/GID,
executable bytes/inode, command, cgroup and network namespace. It stores that
stop authority in memory before writing the binding receipt or checking the
complete environment and listener readiness. A receipt or configuration failure
therefore retains authority to stop that same process. Configuration evidence
is separate and is cleared on rejection; HTTP still requires a fresh, exact
configuration and identity check.

The two observed memory-pressure variables are accepted only at their exact
values: this unit's cgroup `memory.pressure` path and the base64-encoded
`some 200000 2000000` plus NUL setting. Unknown or changed environment values
remain rejected. Stop rechecks ownership without depending on a successful
environment read. A different PID, start tick or invocation remains foreign;
neither a start-intent slot alone nor failed readiness authorizes adoption.

Twelve remote synthetic groups passed, including actual adapter start paths
that reject environment or binding-receipt storage and then dispatch one owned
stop. Foreign identities dispatch no stop. Eleven controller groups and ten
retained-file groups also passed. All three guard frontends exited zero and
closed their process groups and output. The unchanged HTTP and catalog guards
remain bound to their original source pins. No new product build or full suite
is warranted by these operator-only changes.

## Retained state and the next finite action

The actual [post-stop state](audited-main-native-backup-closeout.json) is the
baseline: main/source55 inactive; 35 tables, 402 rows and five sequences; original
lifecycle/control/backup/master metadata; two cache marker files; eleven closed
diagnostic logs. The former empty-cache/ten-log baseline is not reused.

The controller inherits ownership of exactly the three installed capacity/fence
files and the original runtime drop-in directory through the pinned original
preparation and closed execution chain. Preparation compares their bytes and
metadata and the complete retained unit projections. It publishes no new
configuration. The historical unfenced input remains the reference for computing
the original policy after removal; it is not reported as current installed state.
Only the original fixed fence source may be republished during failed cleanup.

Admission permits one additional source32 start and one native login/create/full
download/logout workflow. The unused original request ID and private passphrase
remain pinned; neither was previously sent. Existing per-request, object-size,
space, 40-minute service and 2700-second controller bounds remain in force.
The parent start/stop counts remain recorded rather than reset. Before mutation,
fresh database/files/external-boundary checks must match the retained state.

A successful archive snapshot contains 405 rows: the original 402, the new
administrator session, login audit and requested audit. The operational
post-workflow state contains 408 rows after completion, download and revocation
audits. All old rows, devices and the other four sequences remain exact. Native
creation must authenticate the four application keys in its own dump snapshot.

The expected file delta is control revision5 to9, one new ready object and one
new closed diagnostic log. Both cache markers and all eleven old logs remain
exact; the old 413-byte registry recovery must not happen again. Only after
HTTP, SQL, native-file, external-boundary and owned-service closure may the
exact inherited runtime fence and owned empty directory be removed. Capacity
remains installed. Failure preserves the fence and evidence, with no automatic
start or business retry.

## Frozen verification

| Artifact in the private child scope | SHA256 |
| --- | --- |
| `run-main-native-backup.py` | `957f6fa87a5d32384bbad8e437a71c03331ea5c7d5e8e574ff0e68a76b5cb25e` |
| `main-native-backup-service.py` | `25a379b00e70b52198a98e372e7b9a8a0a51d8a31db46ad6df26cc7ce9b85567` |
| `main-native-backup-files.py` | `a4228698e60da2d004047056b5c6f780af07d24aa2912e65c2137ed3bbb3348c` |
| `service-synthetic-guard-report.json` | `bfebf04eb1ab15286711a4153ced36d7aeb1a898b6b30cfebbc3d65579250d37` |
| `files-synthetic-guard-report.json` | `1cc84dbd3ec4fadddb2550d5d1204ea69b65fef2491e5c0d972922b7f50f974c` |
| `controller-synthetic-guard-report.json` | `94c5b73b4d36d4002fe767bcd9fb4e61dc662d0922b3c684f9a658730a31d2bc` |

Independent review accepted the retained-policy handoff and deferred fence
removal. The exact external admission was
`a153b0630b49aec09f02ab9cdb8e6e081a8229d3abd3549f024956587a78254e`.
Synthetic verification is not an archive, live key authentication, isolated
restoration, core-video acceptance or main promotion. The full M2-M6 goal and
the two distinct [restoration proofs](audited-main-isolated-restore-plan.md)
remain unchanged.
