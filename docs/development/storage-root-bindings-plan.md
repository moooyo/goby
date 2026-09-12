# Storage root bindings and missing-file reconciliation

Status: the identity/topology adapters, shared model and real isolated mount
observations have bounded remote verification. Schema28 persistence, structural
backup checks and audit fields are drafted; the actual PostgreSQL catalog was
generated. Source48 feature/migration/archive acceptance passed all 143 targeted
tests after two fixture corrections; source49 full regression is running.
Native binding/rebind, per-root
anchor publication and administrator UI are drafted; source45 passed 36 selected
non-database race checks and the UI passed 68 decoder/mocked-browser checks.
New-registration automatic binding passed its selected non-database checks;
ordinary missing-file deletion remains unimplemented. Source44's repair of the
source41 scan-throughput regression passed its 2,002-test full run and is published.

The [first remote capability observation](root-binding-capability-v1.json)
established that `FS_IOC_GETFSUUID` and `name_to_handle_at` both work for one
owned fixture on test-env's Linux 6.12.107 environment as uid 0. The combined
identity stayed equal across separate observer processes and ordinary content
changes, distinguished a replacement empty directory at the same pathname,
and matched again after the original directory was restored. This is a bounded
API feasibility result. A [separate unprivileged observation](root-binding-unprivileged-capability-v1.json)
also obtained both identifiers on an owned ext4 fixture as the actual `goby`
service uid, with no supplementary groups, an empty capability bounding set,
and NoNewPrivileges. System reboot, nested mounts, network filesystems, the
complete service sandbox, and product deletion authorization remain unverified.

The frozen source38 change set covers 19 Go files, including the implemented
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
These results advance the earlier API feasibility observations to a tested
adapter; system reboot, nested mounts, other filesystems and the complete
service sandbox remain unverified. Source38 also passed its separate
[95-test PostgreSQL notification/lock checks](m3e-library-changed-aux-verification.json);
its full-suite regression passed 1,953 tests across 24 packages with no failures
or skips and all cleanup checks true. The persistence and reconciliation
steps below remain planned and are not deletion authorization.

The subsequent root topology observer is implemented in eight separate files,
but [its first remote race run](root-topology-go-verification-failed.json) failed:
15 tests passed and four real filesystem tests rejected a valid external nsfs
mountinfo root name. Source39 and its failed test scope remain immutable; the
parser fix subsequently passed [22 tests on source41](root-topology-go-verification.json),
with zero failures/skips and unchanged source files. Report SHA-256:
`52881360e7561cbc8f38da4c724396cdbeab2d33e8d4b7fbe1e89582e70a7677`.
External nsfs namespace names are parsed narrowly; related namespace dentries
still fail explicitly. Source41 full regression later failed two capacity cases.
Source44 removes unnecessary empty-theme snapshot work and passed both cases plus
real auxiliary HTTP/WebSocket acceptance in its
[targeted run](m3e-library-changed-capacity-verification.json). Full regression
remains pending.

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
[generation](storage-binding-schema28-catalog-verification.json). Source47 includes
it together with the archive integration test, source44 repair, binding workflow,
initial registration and verified UI. Database feature acceptance is running.

Source45 adds the read/write services, native routes, root-specific retained
anchor installation and archive/reset-proof tests. Its
[36 selected race checks](storage-binding-workflow-pure-verification.json)
passed without database access; this verifies bounded projections, directory
observations, input parsing and handle isolation/lifetime, not the binding write
transaction or migration/archive workflows. The administrator dialog passed
[68 decoder/mocked browser checks](storage-binding-workflow-web-accessibility-verification.json)
and desktop/narrow-screen visual review. Current work still needs PostgreSQL
integration, complete-scan reconciliation, live browser
acceptance and deployment before this plan can be marked complete.

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

## Pending scanner integration details

The current scanner neither reads persisted binding columns nor retains a
complete ordinary-item seen set. Keep additions and updates in their existing
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
