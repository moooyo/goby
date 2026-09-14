# Storage root bindings and missing-file reconciliation

Reviewed on 2026-09-14. Status: **implemented with bounded PostgreSQL,
real-mount and live administrator-UI acceptance; joint real-media full-scan
mount experiment paused after two launcher failures; complete M2 remains open**.

## Current evidence boundary

Schema28 persistence, structural archive validation, audit fields, native
binding/rebind, automatic registration, per-root anchor recovery and scanner
reconciliation are implemented. Source54's [74 PostgreSQL checks](storage-binding-scan-source54-postgresql.json)
and [full regression/Linux build](storage-binding-scan-source54-full.json)
passed; the latter recorded 2,173 tests across 25 packages. The later selected
product passed [2,270 tests/25 packages and a Linux build](tv-parent-metadata-full-verification.json).
These frozen product proofs supersede the historical running/draft statuses
below, but do not turn unexecuted privileged helpers into runtime acceptance.

The [real private-mount recovery](storage-binding-scan-mount-v2-terminal.json)
passed independently on source54. Its actual helper uses ext4 bind mounts,
calls `prepareRootBindingScan`, rejects detached/replacement storage, recovers
the original source without rebind and preserves catalog/approval rows and
held descriptors. It does not execute the complete media scan/delete pass.
The separate PostgreSQL full-scan tests cover real directory rename/unavailable
paths and retained identity/UserData using a fixture prober. The mount helper
returns without action in an ordinary test run unless its explicit helper
environment is supplied; a passing test name alone is not that mount proof.

The [live UI Run5 independent terminal](storage-binding-live-ui-accepted-terminal.json)
passed 15 browser checks and 10 IPC stages against the real schema28 backend.
It includes actual uid995 permission-unavailable observation, a stale-revision409,
explicit rebind/initial bind and exact session/resource closure. Its controller
retains `awaiting_outer_attestation` by design; the terminal is the acceptance
authority. The [accepted UI contract](storage-binding-live-ui-acceptance-plan.md)
uses `Scan:false` and creates no scan jobs, so it does not establish full-scan
mass-removal prevention. Its 68 earlier mocked-browser checks retain their own
scope and are not additional live cases.

The new `TestRootBindingFullScanMountNamespaceHelper` preparation is recorded
in the [M2 preparation/failure checkpoint](m2-fullscan-mount-preparation.json).
Its 4,069-byte MP4 and frozen 842-file source manifest exist. The first launcher
rejected `module_cache_binding_prerequisite` before any unit, PostgreSQL, build
or test: `/root/go/pkg/mod` was the assumed Go default, not an existing cache.
The original empty RAM mount and failed receipt were retained.

After an independent review, the continuation reused that same RAM scope and
bound the actual frozen module cache read-only, with all 2,119 files checked by
size/hash. Race-instrumented `library.test` compilation succeeded with exit0 and
empty stderr. This is a component compilation result only. The next `initdb`
frontend failed with `runuser: cannot set user id: Operation not permitted`.
No PostgreSQL daemon or helper test started; all seven planned scan stages
remain unexecuted, and the PostgreSQL data/socket directories remain empty.
The unit is failed with MainPID0 and an empty ControlGroup; its owned processes
are closed. The RAM mount, compiled helper, working files and evidence are
retained, and protected host/main state is unchanged.

Two launcher failures pause this experiment under the existing execution rule.
There is no automatic retry or third renamed attempt. The observed unit
CapabilityBoundingSet includes SETUID and SETGID; the failed UID transition's
cause is not yet attributed, and NoNewPrivileges is not established as its
cause. The joint scenario has **not passed**. Preserve both failed scopes and
all previously accepted source54/UI evidence. Independent M6 embedded-asset
functional verification is the next work item; this pause does not require a
new generic runner or reopen consumed recovery operations.

Remaining boundaries include representative movie/TV/music rescan and actual
service-restart/ACL list-count coverage, representative capacity/concurrent
scan-query measurements, truly blocked I/O and bounded shutdown, and supported
filesystem/profile rows beyond the observed ext4 scope. Fast EACCES or a missing
path does not prove blocked-I/O handling. Physical binding across a host reboot
and host-reboot/power-loss durability remain separate: process/Store restarts,
same-mount PG restarts, live device/inode/mount witnesses and tmpfs evidence
cannot establish them. These limits do not undo the accepted narrower cases.

## Historical implementation record: source38-source54

The following sequence preserves the original implementation and failure
history. Its receipt identities and scopes are unchanged. The current boundary
above governs progress; the later sections preserve the design and acceptance
contract rather than implying that implemented work is still only a draft.

The [first remote capability observation](root-binding-capability-v1.json)
established that `FS_IOC_GETFSUUID` and `name_to_handle_at` both work for one
owned fixture on test-env's Linux 6.12.107 environment as uid 0. The combined
identity stayed equal across separate observer processes and ordinary content
changes, distinguished a replacement empty directory at the same pathname,
and matched again after the original directory was restored. This is a bounded
API feasibility result. A [separate unprivileged observation](root-binding-unprivileged-capability-v1.json)
also obtained both identifiers on an owned ext4 fixture as the actual `goby`
service uid, with no supplementary groups, an empty capability bounding set,
and NoNewPrivileges. At that observation, system reboot, nested mounts, network
filesystems, the complete service sandbox and product deletion authorization
were outside its scope; later topology and product receipts are separate.

The frozen source38 change set covered 19 Go files, including the implemented
`root_identity` adapter and its `linux-fsuuid-filehandle-v1` identity profile.
The adapter separates the complete filesystem UUID and opaque directory handle
from the live device/inode/mount witness; it does not persist or approve a root
binding. [Go adapter verification](root-identity-go-verification.json) passed
14 top-level tests, including one process-helper entry, with race detection,
zero failures and zero skips. Coverage includes real ext4 directory content
changes, same-path replacement/restoration and a separate observer process.
The verification record SHA-256 is
`e8c56113fb8f9acc8d9824d58698fa5bb9f93da3aad8c86b1229f79638215207`.

The [actual unprivileged Go helper](root-identity-go-unprivileged.json) also
matched the identity and live witness on an owned ext4 fixture as service uid
995, with an empty capability bounding set and NoNewPrivileges. Its record
SHA-256 is
`0ce31db107330c124a758c806feb59051f382bc66e460cdf41037030d8e2885b`.
These results advanced the earlier API feasibility observations to a tested
adapter; they did not verify system reboot, nested mounts, other filesystems or
the complete service sandbox. Source38 also passed its separate
[95-test PostgreSQL notification/lock checks](m3e-library-changed-aux-verification.json);
its full-suite regression passed 1,953 tests across 24 packages with no failures
or skips and all cleanup checks true. Persistence and reconciliation were later
implemented and verified; these source38 adapter receipts alone were not
deletion authorization.

The subsequent root topology observer was implemented in eight separate files,
but [its first remote race run](root-topology-go-verification-failed.json) failed:
15 tests passed and four real filesystem tests rejected a valid external nsfs
mountinfo root name. Source39 and its failed test scope remain immutable; the
parser fix subsequently passed [22 tests on source41](root-topology-go-verification.json),
with zero failures/skips and unchanged source files. Report SHA-256:
`52881360e7561cbc8f38da4c724396cdbeab2d33e8d4b7fbe1e89582e70a7677`.
External nsfs namespace names are parsed narrowly; related namespace dentries
still fail explicitly. Source41 full regression later failed two capacity cases.
Source44 removed unnecessary empty-theme snapshot work and passed both cases plus
real auxiliary HTTP/WebSocket acceptance in its
[targeted run](m3e-library-changed-capacity-verification.json). Its subsequent
2,002-test full regression passed and the repair was published.

Source42 passed [46 shared-model/alias race tests](storage-binding-model-go-verification.json).
Its [real mount namespace helper](root-topology-mount-verification.json) verified
owned ext4 bind-mount capture, loss, replacement, original-source remount,
additional boundaries and stacked-mount rejection. The host mountinfo and
namespace were unchanged; all owned fixture mounts and files were cleaned up.
This is separate from system reboot and other-filesystem evidence.

Source43's [bootstrap preflight](storage-binding-schema28-bootstrap-source.json),
[20 pure validation/audit tests](storage-binding-schema28-pure-verification.json),
[frontend checks](storage-binding-audit-web-verification.json) and
[112 runner guards](storage-binding-schema28-runner-guards.json) passed.
The actual schema28 catalog subsequently passed
[generation](storage-binding-schema28-catalog-verification.json). Source47 included
it together with the archive integration test, source44 repair, binding workflow,
initial registration and the verified mocked UI. Source48 feature/migration/archive
acceptance subsequently passed all 143 targeted tests after two fixture corrections.

Source45 added the read/write services, native routes, root-specific retained
anchor installation and archive/reset-proof tests. Its
[36 selected race checks](storage-binding-workflow-pure-verification.json)
passed without database access; this verifies bounded projections, directory
observations, input parsing and handle isolation/lifetime, not the binding write
transaction or migration/archive workflows. The administrator dialog passed
[68 decoder/mocked browser checks](storage-binding-workflow-web-accessibility-verification.json)
and desktop/narrow-screen visual review. Later PostgreSQL integration and live
UI acceptance are recorded separately above; these mocked checks did not prove them.

Source49 full regression ended with 2,105 passes and two historical recovery
fixture failures. The exact failed pair was disposed with evidence preserved;
source50's fixture repair passed in source54's 74-test PostgreSQL target.
New-registration automatic binding passed its selected non-database checks.
Source53's original-root recovery and directory-evidence helpers passed 20
selected race checks without database access. Source54 integrated the full
scanner/delete path and shared music-completeness gate, passing 28 selected
non-database race checks and related compilation, then 74 PostgreSQL checks,
the 2,173-test full regression/Linux build and the independent private-mount
helper gate. Live UI Run5 subsequently passed its separate acceptance. None of
these results closes the pending joint real-media full-scan mount or broader
M2-M6 evidence requirements.

## Problem and acceptance boundary

Schema 27 records a root's logical paths but no durable physical storage
binding. A process can restart after a mount disappears and open a new empty
directory at the same pathname. Repeated empty scans do not establish that the
original storage is present. A registered root can also retain its own identity
while a nested mount disappears and exposes an empty mount-point directory.

`openApprovedRoot` retains a directory descriptor for safe relative traversal.
That descriptor alone does not prove that the current configured pathname,
registered root, and nested storage boundaries still identify the same storage.
Normal additions and updates may continue on accessible paths; deletion needs
an independent, explicitly approved binding and complete scan evidence.

## Persisted approval

The next schema migration should add `binding_revision`, nullable
`storage_binding`, `bound_at`, and `bound_by` to `library_roots`. Existing roots
start unbound. The versioned binding document records the approved path mapping,
approved anchor, registered root, and all nested mount boundaries. Each boundary
has a relative path, identity profile, validity domain, filesystem identity, and
directory identity. Apply explicit size and boundary-count limits; exceeding a
limit disables deletion instead of silently truncating the topology.

The implemented adapter combines a complete filesystem UUID with an opaque
directory file handle and its type. Its current ext4 and observer-process
evidence does not establish reboot persistence, mount topology or support for
other filesystems; those guarantees still need remote verification before the
profile can authorize persisted root bindings and deletion. Device/inode
numbers, mount IDs, mount namespaces, and `f_fsid` alone are not a durable volume
identity. A weaker profile must expose its boot/process validity domain and
expire explicitly;
process-scoped identity must retain the original descriptor. Directory change
timestamps and link counts belong to scan stability evidence, not durable
identity, because normal content changes can alter them.

New library registration captures all supported identities while holding the
opened directories through the registration transaction. Scanning never writes
or learns a replacement binding. Neither one nor many empty scans can bind an
old root. Topology additions, removals, replacement, or unreadable boundaries
disable missing-file deletion while retaining the original binding. If the
approved topology and identity become verifiable again, deletion eligibility
can recover without rebind. Accepting changed storage/topology, or recovering
an expired validity domain without stable identity proof, requires explicit
rebind.

## Native administrator workflow

Add a binding read endpoint under
`/admin/v1/libraries/{id}/roots/{rootId}/binding`. Return the binding revision,
an observation fingerprint, a bounded boundary summary, and an actionable
`verified`, `unbound`, `mismatch`, `unavailable`, or `expired` state.

The corresponding update accepts `Revision`, `ObservedFingerprint`, and an
explicit acknowledgement that missing records may be removed after a subsequent
complete scan. Submitted identity values never become trusted binding data.
The server reopens and observes the full root and nested topology, revalidates
the administrator inside the owned transaction, rejects active scans and stale
revisions, and saves the binding with an audit record. Filesystem handles are
acquired before the owned transaction; code holding its ownership mutex must
not acquire `Store.mu`. Audit only the root ID, revisions, and fingerprint,
without opaque handles. The administrator page must make the affected root and
storage-boundary changes reviewable before the rebind request.

The existing Store caches the initial approved-root descriptor. Binding reads
and explicit rebind must observe the current configured pathname independently
of a stale cached anchor. Restoring or replacing an approved-root mount can
leave the old descriptor attached to a detached mount. The implementation must
also ensure subsequent scans and media opens use the accepted current storage,
while preserving descriptor lifetime, active-scan exclusion and lock ordering.
An API that can only accept a replacement after a process restart is incomplete.

## Reconciliation sequence

1. Validate the approved binding and current pathname chain, and retain live
   root/storage witnesses and bounded directory snapshots while walking.
2. Finish all roots and cross-root move matching in the library. Preserve item
   IDs and UserData for accepted moves. Any incomplete root, permission error,
   warning, cancellation, or changing directory prevents this library's delete
   pass; earlier committed additions and updates remain valid notifications.
3. Reopen and compare the named paths and complete mount topology against the
   held descriptors before entering the owned transaction. Under that
   transaction, verify the binding revision and recheck the held witnesses
   without acquiring `Store.mu`.
4. Re-read candidate rows after move matching. Require positive absence evidence
   for every physical candidate and validate the whole cascade before deleting;
   reject seen descendants, foreign scopes, and unproven children. Treat
   synthetic hierarchy nodes separately, and never delete CollectionFolder as
   an absent media path.
5. Retain removed item/library/parent/root/role facts before deletion and publish
   only after its commit. Add affected audio parents to the music refresh set;
   run reconciliation before `refreshScannedMusicAlbums` so derived albums do
   not retain removed tracks.

Music completeness must be checked before the deletion commits, including
warnings that ordinary walking does not yet produce. Reuse the album aggregator's
read-only rules against the proposed surviving member set, excluding every
independently proven deletion member. A not-ready album prevents the whole delete
pass. Actual album publication remains after deletion; do not make an absent
legacy member permanently block its own otherwise proven removal.

## Backup, restore, and evidence

Generate and verify the actual next-schema backup catalog on test-env while
preserving all historical schema 23–27 catalog fingerprints. Current archives
must round-trip the binding fields exactly; older archives migrate to unbound
roots. Restore validates the binding document structurally without requiring
media to be mounted, and neither startup nor restore finalization silently
rebinds it. Deletion resumes only if the original approved binding matches the
actual restored environment. Wrong or unavailable storage preserves catalog
rows and exposes its mismatch/unavailable state. Returning original storage
can verify against the preserved binding; accepting replacement storage requires
rebind. Binding updates must invalidate
previous recovery reset proofs.

Remote verification must cover process restart, same-path empty-directory
replacement across restart, repeated empty scans, original storage recovery,
nested mount loss/replacement/addition, scan-time topology changes, cross-root
moves, stale or unauthorized rebind, active-scan conflicts, and old/current
archive restoration. Cross-system-reboot identity is a separate evidence gate;
a same-process descriptor comparison cannot satisfy it. Reuse the unavailable
root, cross-root rename, theme directory replacement, and backup round-trip
fixtures before adding privileged mount fixtures.

## Scanner integration contract

The scanner must read persisted binding columns and retain a complete
ordinary-item seen set. Keep additions and updates in their existing
transactions; build a separate bounded reconciliation evidence object per
library. Capture every root's approved row, revision and mapping before walking,
retain its independent handles/topology and directory observations until the
final phase, and disable the entire library's deletion pass on any incomplete
root, warning, cancellation, changing directory or evidence-budget overflow.
Cache hits must enter the seen set. Re-read candidates only after all roots have
finished, because existing per-file identity matching can accept a cross-root
move in a later root.

Run the final phase after collection-theme completion and warning aggregation,
before derived music-album refresh. Preserve surviving MusicAlbum ancestors
before deleting intermediate audio parents. Candidate selection must exclude
CollectionFolder and synthetic hierarchy paths. Expand every parent_id cascade
without a library filter, then validate each descendant's scope, root, role,
seen state and independent absence evidence: the SQL foreign key itself can
otherwise cascade into corrupt cross-library children hidden by a filtered
validation query. Include theme/extra owner relationships in that inspection.

Owned transactions protect their own context from request cancellation. The
delete pass must explicitly check the original scan context and roll back on
cancellation; use the protected context for SQL without acquiring Store.mu
inside ownership.mu. Retained descriptors and repeated observations are not an
atomic filesystem/PostgreSQL lock; retain that limitation in evidence claims.
Tests should control changes throughout walking and final validation rather
than substituting a same-process descriptor comparison for complete scan proof.

Original-storage recovery also needs a dedicated path: an old cached anchor may
remain on a detached mount after the original source is remounted. Only a fresh
complete snapshot matching the persisted approval may install the current anchor
without changing revision or approval. A replacement identity must still require
explicit rebind. Verify this in the isolated mount namespace, in addition to
ordinary directory rename/restore tests.
