# Storage root bindings and missing-file reconciliation

Status: the underlying Go root identity adapter is implemented and has bounded
remote verification. Persistent binding and reconciliation remain an
implementation plan: schema28, the native binding/rebind API and ordinary
missing-file deletion have not been implemented or verified. Those integrations
are required before a completed scan can remove catalog rows for absent files.

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
its full-suite regression is running. The persistence and reconciliation
steps below remain planned and are not deletion authorization.

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
