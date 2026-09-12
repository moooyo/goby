# Storage root bindings and missing-file reconciliation

Status: implementation plan. No binding migration, native binding API, or
ordinary missing-file deletion has been implemented or verified. This work is
required before a completed scan can remove catalog rows for absent files.

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

The preferred identity profile combines a verified stable filesystem identity
with an opaque directory file handle and its type. The actual Linux APIs,
filesystem support, and reboot guarantees need remote capability verification
before selecting or implementing that profile. Device/inode numbers, mount IDs,
mount namespaces, and `f_fsid` alone are not a durable volume identity. A weaker
profile must expose its boot/process validity domain and expire explicitly;
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
