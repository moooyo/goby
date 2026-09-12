# Library change notifications

Committed library creation/deletion and native metadata edits now reach the
bounded notifier and outbound permission filter. Source35 passed both targeted
producer checks and its full remote suite. Later ordinary scan and move changes
passed pure logic checks and the source37 PostgreSQL/HTTP/WebSocket targeted
suite. Source37 full-suite results and original-client UI acceptance remain
pending. Auxiliary and derived scan notifications, persistent root binding and safe
missing-file reconciliation are not implemented. LibraryChanged
support and client acceptance remain incomplete, and this work is not deployed.

## Reference observations

The [controlled continuation](m3e-library-changed-reference-continuation.json)
completed metadata editing, directory removal and re-addition. Each stage had
75 quiet seconds before its action and 120 continuous observation seconds after
its catalog result stabilized. Each window contained one LibraryChanged event.

| Action | Observed ID arrays |
| --- | --- |
| Administrator edits Movie98 Name/Overview | ItemsUpdated `[98]`; all other arrays empty |
| Move folder97 outside library93 | ItemsRemoved `[97]`, FoldersRemovedFrom `[94]`, ItemsUpdated `[93,94,95,96]`; other arrays empty |
| Move the same files back | ItemsAdded `[99,100]`, FoldersAddedTo `[94,99]`, CollectionFolders `[93]`, ItemsUpdated `[93,94,95,96]`; other arrays empty |

The individual public transcripts are [metadata](m3e-library-changed-reference-metadata-window.json),
[removal](m3e-library-changed-reference-remove-window.json) and
[re-addition](m3e-library-changed-reference-readd-window.json).

IsEmpty was false in all three messages. Removal left only IDs94–96 in the
scoped catalog query: Movie98 disappeared from that query but was not separately
listed in ItemsRemoved. Re-addition created folder99/movie100. These are public
API observations, not claims about the reference database's physical deletion.
The reference currently has eight libraries and six users, with the owned
re-added folder99/movie100 at the original paths. The observed delays are not a
general protocol timing guarantee.

The continuation report confirms all 333 HTTP exchanges, both fresh-session
logout204/exact401 barriers, the original seven-library/five-user public
projections, all three old media trees, the existing viewer's configuration/policy
and the original failed evidence tree. The separate
[controlled fixture five-file preservation](m3e-library-changed-reference-media-preservation.json)
covers the new fixture's regular files across removal/re-addition. The
[initial NFO-update failure](m3e-library-changed-reference-initial.json)
and separate [negative tail](m3e-library-changed-reference-tail.json) remain
historical evidence; neither repairs the failed initial gate. Their consumed
capture scopes and the completed continuation must not be replayed against the
current reference state. See the [research record](../research/library-changed-reference.md).

## Committed production and bounded delivery

`CreateLibrary` and `DeleteLibrary` record their CollectionFolder root facts
inside the owned catalog transaction. `UpdateItemMetadata` records an update
only when its native override or lock state actually changes. The transaction
retains copied item, library, parent and role facts, including the former scope
needed after deletion. No-op edits, rollback and failed commits publish nothing.

The listener runs only after the underlying commit succeeds, while the ownership
mutex still preserves commit order. A request cancellation does not suppress a
write that committed successfully. Listener panic cannot change that successful
database result; one bounded Resync attempt follows a failed callback. Invalid
facts, more than 1,024 facts or more than 256 KiB replace the whole transaction
batch with Resync rather than a partial change list.

The server's single `libraryNotifier` worker copies and delivers these committed
batches in order. Its queue includes active work in the 64-batch/256-KiB budget;
each batch is bounded to 512 facts and 64 KiB. Explicit Resync, ambiguous facts,
queue overflow or publication failure discards pending work and disconnects all
current subscriptions so clients must obtain a fresh snapshot. Generation checks
prevent an invalidated active batch from reaching a subsequently reconnected
client. Shutdown detaches the listener, rejects captured late callbacks and
waits for the worker; it does not change an already committed operation.

## Outbound implementation

`events.Hub.PublishAll` shares one immutable publication and MessageId across
ordinary and application subscriptions. Private CatalogScopes retain ItemID and
LibraryID for delivery-time policy checks, including historical library IDs.
They are copied and never encoded in the client message. Their bounded storage
counts toward both the message and per-subscriber queue byte limits.

`library.Store.AllowedCatalogLibraries` reuses the current Subject authority and
library policy snapshot. It intersects the requested trusted library IDs with
that policy without requiring an item or library row to survive. Empty input
still authenticates the subject. The method does not prove resource existence
and must not replace ordinary item authorization.

The WebSocket sender retains its existing session revalidation. LibraryChanged
then projects only the six known ID arrays and IsEmpty. An ID is retained when
at least one of its trusted library scopes is currently permitted. Other library
IDs remain private, and subsequent resource reads enforce their own current
visibility rules. Array order, repeated IDs and the publication's MessageId are
preserved. Unscoped IDs and unknown fields cannot acquire authority from JSON;
an empty recipient projection sends no frame. Application credentials retain
their independent authority rather than borrowing a fabricated user identity.

## Remote verification

- [Hub verification](m3e-library-changed-events-unit-verification.json): 20 package
  tests with race detection; immutable snapshots, queue accounting, typed
  broadcast, existing directed delivery and concurrent lifecycle checks passed.
- [Catalog authorization](m3e-library-changed-catalog-access-verification.json):
  five PostgreSQL integration tests passed on source33, including a genuinely
  deleted Goby library, current policy changes and revoked application keys.
- Source34 passed 12 WebSocket/remote-control tests, including real HTTP upgrades
  and framed delivery for different users, missing scopes, policy changes after
  publication, historical library IDs, application revocation and existing
  UserDataChanged/remote-command behavior. The test publisher supplies trusted
  events; it is not evidence of scan-triggered production.
- [Source35 producer verification](m3e-library-changed-producer-verification.json)
  passed 43 targeted tests, with zero failures and zero skips. The checks cover
  real library root creation/deletion, committed native metadata changes,
  cancellation after transaction admission, rollback/no-op/failed-commit
  suppression, bounded notifier loss/recovery, shutdown and real HTTP/WebSocket
  delivery. All six cleanup flags are true. They do not verify scan-triggered
  production or missing-file deletion.

Source35 is bound to manifest
`3eadc5606b89c8ec7887bee760bb9309641d407638bb03d26c30344b250846eb`.
Its targeted remote report is
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_001248_002cceaba1e8/report.json`,
SHA-256 `9f0595a1d9fb9c97e03d8aed8cd7fcb2fdb33dcdd5ae23f29a8fa8a79c73ebe6`.
The disposable databases/roles were removed, HBA bytes restored exactly and the
pre-existing catalog preserved. Formatting and test execution were confined to
test-env.

The [full source35 verification](m3e-library-changed-full-verification.json)
passed 24 packages and 1,908 top-level tests, with zero failures, zero skips and
all six PostgreSQL cleanup flags true. The original 43-test targeted result
remains a separate record. The full run ID is `20260912_002059_ab4a943fc81b`, and
its remote report is
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_002059_ab4a943fc81b/report.json`.
The report SHA-256 is
`bb32b90b18b5c564f865734991d6cb50b4d17c023fe6687c9ee32181182fb808`.
Its `tmp/goby-linux-amd64` artifact is 28,248,725 bytes, SHA-256
`8c91a392d7e339e38a6fdb8be7ce41cc4fd8c20f1b5492ac38734871aa166377`.

The full controller `goby-library-changed-full-controller-v1.service` completed
with exit0, MainPID0 and an empty worker cgroup. Its execution directory is
`/opt/goby-test/exec-work-m3e/library-changed-full-execution-01`, InvocationId
`7e3faf01d865410aa5799c3239105558`; the terminal record SHA-256 is
`e1677db514cc6d3e8fb086ce9145ecf18753fc87bb353619bcbd215fbea87bb7`.
Source35 does not cover the subsequent ordinary scan or move edits. No product
binary from this change has been deployed; candidate and primary remain on the
accepted source32/schema27 deployment.

## Ordinary scan changes after source35

Later, unstaged code connects ordinary file and folder additions/updates to the
post-commit producer. Folder comparison uses effective properties after metadata
synchronization rather than timestamps or source bookkeeping. Successful file
ForceProbe refreshes can invalidate identical accepted facts; cached unchanged
visits stay quiet. Same-library moves retain PreviousParentID in one update fact,
and earlier commits are not gated on the eventual whole-scan result. Source37's
targeted PostgreSQL and real HTTP/WebSocket checks now cover these changes;
source35's earlier result does not. Original-client UI acceptance remains open.

The [source36 pure verification](m3e-library-changed-scan-pure-verification.json)
passed 14 tests with race detection and no failures or skips. Its report SHA-256
is `8933a1df780bae0b1ccd3c728865dfcbd75b0b82fe2cb32aa169cbba19b98be3`.
It covers pure notification logic and package compilation, not PostgreSQL,
actual scan execution, client acceptance or deployment.

The [first scan-controller failure](m3e-library-changed-scan-manifest-failure.json)
is retained: the PostgreSQL runner rejected source36 because `manifest.files`
was a list instead of the reviewed object format. Database setup was not
reached, and the pair receipt still described the completed source35 run.

[Source37 preflight](m3e-library-changed-scan-source-preflight.json) passed after
only that manifest representation was corrected. All 4,154 source members and
their bytes match source36. Its manifest SHA-256 is
`153018fb7e0b5308d727dcc7b1794bc9769ba71e24c2b783a23941595291813a`.
Preflight performed no database setup and is not a PostgreSQL test pass.

The [source37 targeted verification](m3e-library-changed-scan-verification.json)
passed 80 top-level tests, with zero failures, zero skips and all six cleanup
flags true. It covers ordinary scanning and real HTTP/WebSocket delivery; it
does not establish original-client UI acceptance or a source37 full-suite pass.
The run ID is `20260912_005317_90f76937a793`; its remote report is
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_005317_90f76937a793/report.json`.
The report SHA-256 is
`60c5b3b69f234cb52201ed17531a8015a351f86bac9d00b08b59b03198ce911d`.
Controller `goby-library-changed-scan-controller-v2.service` completed with exit0
and MainPID0, retaining InvocationId `b9f73f0231604d758bfaf6fd3c104dde` and
execution directory
`/opt/goby-test/exec-work-m3e/library-changed-scan-execution-02`.
Its terminal record SHA-256 is
`962a578e7e5fae442e4ca2b1b9bf0d9613e3ba1c15d23fb7e5f42da85b9ea244`.

The source37 full run is active under
`goby-library-changed-scan-full-controller-v1.service`, PID `979794`, InvocationId
`d477192e74604dd290af3e7d6c3d212a`, execution directory
`/opt/goby-test/exec-work-m3e/library-changed-scan-full-execution-01`.
The run ID is `20260912_005623_3e09ff8ff85d`; the pending report path is
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_005623_3e09ff8ff85d/report.json`.
It has no terminal result yet; the completed 1,908-test full run belongs to
source35. SSH access has recovered, with no current access blocker.

## Remaining integration

Complete source37 full-suite verification and original-client UI acceptance of
the ordinary scan and move changes, then connect auxiliary scans and derived
metadata updates. Preserve earlier committed changes when a later scan step
fails, and distinguish explicit
refresh invalidation from a proven visible metadata change. Source35's native
metadata and library-root coverage does not establish these scan semantics.

Persistent root identity binding and safe missing-file reconciliation remain
unimplemented. Deletion requires stable directory observations across all roots,
completed cross-root move matching and preservation on inaccessible, replaced or
incomplete roots. A held directory descriptor alone does not prove that the
configured pathname or mount still identifies the same root.

The [storage root binding plan](storage-root-bindings-plan.md),
[initial capability observation](root-binding-capability-v1.json) and
[unprivileged capability observation](root-binding-unprivileged-capability-v1.json)
establish bounded API feasibility only. Schema28, persisted bindings and product
deletion authorization are not implemented; reboot, cross-system portability,
nested mounts and network filesystems remain unverified.

After the remaining scan production and reconciliation are implemented, verify
those real events end to end and repeat the original-client refresh and
permission journeys.
Full M3/M4/M5/M6 acceptance remains open; M7 remains deferred.
