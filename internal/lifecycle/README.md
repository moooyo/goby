# Local deployment lifecycle

This Linux-only package owns a dedicated state directory, normally
`/var/lib/goby/recovery`, configured with `GOBY_RECOVERY_STATE_DIR`. The directory
must not also contain media, cache, diagnostics,
database files, or an existing application key. Configuration integration must
reject such path overlap before opening the directory. Existing deployment
configuration and the default master key stay at their original locations.

`Open` walks all path components with `O_NOFOLLOW`, requires the final directory
to have mode `0700` and the effective process UID, and keeps an exclusive lock
on both its pinned directory descriptor and its registered `0600` mutex file.
The first claim requires an empty directory. Existing files are never imported.
Both locks must remain held through database selection, migration, process
operation, and shutdown. A separate PostgreSQL lifecycle lock is still required
to fence processes that use different state directories with the same database.

All regular files must have mode `0600`, one link, and the process UID. An owned
marker identifies the deployment and mutex inode. The generation registry
records immutable directory and file device/inode identities and SHA-256
digests. Every public operation rechecks the named directory, mutex, metadata,
and registered generation files. Fixed generation basenames are derived only
from lowercase 32-character hexadecimal IDs. An active manifest contains
references and digests, never a database URL, password, configuration body, or
master-key bytes.

## Publication contract

1. `Current` returns revision zero, the primary database slot, and the default
   master only for a valid deployment with no previously published manifest.
2. `StageGeneration` stores nonempty opaque configuration bytes, up to 1 MiB,
   and an optional 32-byte master. The caller validates the configuration schema
   and excludes secrets that do not belong in this immutable generation. An
   absent staged master cannot be selected with `MasterGeneration`. A caller
   restoring a snapshot without encrypted-key data can explicitly generate and
   stage a new key; the file package does not invent one.
3. `Plan` accepts an exact current `State` CAS token and a complete registered
   generation. It writes a durable journal containing both old and candidate
   manifests before activation. Only one plan may be pending.
4. `Activate` checks the exact before-manifest, writes and syncs a temporary
   manifest, renames it atomically, then syncs the state directory. Repeating the
   same already activated plan returns its existing result. It never rewrites
   primary data, default configuration, or the default master.
5. `Pending` distinguishes prepared and activated plans after restart by exact
   manifest hashes. It never activates or rolls back a plan automatically.
6. `Finish` records the new baseline digest before removing the journal.
   `Abort` can remove a journal only while its before-manifest remains current.
   Both retain every generation. `Finish` acknowledges filesystem publication,
   not database health, successful restore verification, or permission to
   overwrite later business state. The restore coordinator owns those decisions.

`ReadGeneration` returns independent sensitive byte slices. `MasterKeyPath` is
an adapter for the existing vault loader, not a filesystem authority token. The
vault loader must retain its own no-follow and ownership checks, and the caller
must keep this lifecycle store open while using the path.

## Failure and trust boundaries

If a rename has happened but the directory sync fails, the outcome is uncertain.
The store returns `ErrRecoveryRequired`, preserves the manifest and journal,
and refuses subsequent operations until closed and reopened. Reopening accepts
only a coherent old or candidate state. Losing an active manifest after a
completed publication cannot silently select the primary database again.

An interrupted stage stays incomplete and is never exposed or activated. A
complete, identical stage retry is idempotent; an incomplete ID cannot be
reused. If a crash occurs between creating a directory and persisting its inode
registration, the existing directory is not claimed on restart. This narrow
case fails closed with `ErrRecoveryRequired` and retains the evidence. A crash
before the first ownership marker is published can similarly leave an unknown
mutex or temporary file and require operator review. A normal failed initial
claim removes only the exact mutex inode created by that call when no marker
was published. A complete marker with no registry and no business files can
finish empty initialization on the next open.

Temporary metadata files left after an interrupted atomic write are inert;
they are neither adopted as manifests nor automatically deleted. The package
has no generation cleanup API. Recovery tooling must not delete incomplete or
unregistered paths based only on a basename. The registry admits at most 4096
generations and directory enumeration is also bounded. Sufficient accumulated
incomplete generations or temporary debris therefore requires explicit operator
maintenance before new work can be admitted; it is not permission for automatic
garbage collection.

Public context-taking operations use a cancellable serialization gate and
check cancellation between bounded reads and writes. A kernel filesystem call,
including `fsync`, already in progress cannot be interrupted by Go context
cancellation. Publication past rename is completed or reported uncertain; it
is never undone because cancellation arrived late. `Current` uses an internal
background context; `Close` waits for an operation already in progress.

The same UID is a trusted boundary. Descriptor-relative operations, inode
checks, and flock fence cooperative deployment processes and reject observed
replacement. They cannot provide adversarial isolation from another process
with the same UID that ignores locks and deliberately races kernel pathname
operations. The directory must stay private to the deployment service UID.

Non-Linux builds expose the same API and return `ErrUnavailable`; this is only
for cross-platform compilation, not a second supported runtime.
