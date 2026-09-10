# Backup object storage

`backupstore` owns encrypted object bytes and a private, durable job catalog. It
does not perform encryption, validate an age stream, inspect a database, publish
an audit event, or authenticate a caller. The coordinator performs those steps.

The default directory is `/var/lib/goby/backups`. It must be separate from the
deployment lifecycle/recovery directory. The final directory is owned by the
effective service user with mode `0700`; object and metadata files use `0600`.
Existing parent directories must be real directories, not symlinks. The final
directory may be created by `Open`, but missing parents are not created.

Linux is the only supported runtime. The local filesystem must support
`O_TMPFILE`, hard links, `renameat2(RENAME_NOREPLACE)`, and directory `fsync`, as
provided by common local ext4, XFS, Btrfs, and tmpfs deployments. A dedicated
local directory is required; there is no unsafe fallback for filesystems that
lack these operations. Unprivileged linking uses the standard
`/proc/self/fd/<owned descriptor>` fallback when `AT_EMPTY_PATH` is unavailable.
This follows only the service's already-open anonymous descriptor. Other
platforms provide compile-only stubs and return `ErrUnavailable`.

## Publication contract

1. Call `Begin(ctx, BeginOptions{Kind, CreatorID, SessionID})`. The store generates
   an unpredictable 32-character lowercase hexadecimal object ID. A `writing`
   job immediately appears in the catalog. It cannot be downloaded.
2. Stream encrypted bytes through `Writer.Write`. Always close the writer. Byte,
   object-count, total-space, and free-space limits are enforced. A write error
   is sticky; call `Abort` with a fixed semantic error code when appropriate.
3. Call `Prepare(ctx)`. It seals writing, syncs the file, re-reads every byte,
   checks the digest against bytes accepted by `Write`, and durably records the
   size, SHA-256, inode identity, and modification time. The returned `Prepared`
   value is bound to its writer and cannot be synthesized or altered by callers.
4. Commit the coordinator's authorized external audit transaction using the
   returned ID, size, and digest. This package cannot make a PostgreSQL
   transaction and a filesystem rename atomic.
5. Call `Publish(ctx, prepared, summary)`. The store first persists a publishing
   intention, then installs the immutable object, syncs the directory, and
   publishes `ready` metadata. A nil summary leaves `Verified` false, including
   for imported objects. Supplying a summary asserts that the coordinator has
   already completed full archive and PostgreSQL inspection.

`Verify(ctx, id, expectedDigest, summary)` can mark a ready imported object as
verified after that inspection. It rehashes the same fixed object while holding
a reader reference, validates the expected digest, and then persists a bounded
whitelist of source facts. A wrong expected digest is a normal conflict and does
not degrade the store. Actual byte/identity mismatches fail closed.

`SourceSummary` permits only the archive ID, format/application/schema versions,
opaque source server ID, UTC creation time, and bounded SQL table row counts.
Archive IDs are 32 lowercase hexadecimal characters. Application versions and
server IDs follow the bounded UTF-8, no-control-character contract of
`backupformat`. Never pass passwords, master keys, database URLs, file paths, or
arbitrary error text. `Metadata.SessionID` is private: HTTP adapters must project
an explicit public DTO instead of serializing this type directly.

## Recovery and deletion

The immutable ownership marker is linked only after all of its bytes are synced.
An anonymous object is registered by device/inode before its generated partial
name is linked. A crash cannot leave an unregistered visible object. Catalog
updates use a fixed reserved metadata staging file and atomic replacement. The
store takes an exclusive process lock and rejects unknown files, symlinks,
multiple hard links, special files, wrong permissions, and replaced identities.

Interrupted, unprepared streams are removed only by their registered identity
and generated name. Their jobs become `interrupted`. A crash after a partial has
already been removed is recoverable. Completed `ready` objects are retained.

Prepared and publishing objects retain their complete bytes and digest. An
unfinished job becomes `interrupted`; previously explicit failed or cancelled
results retain their terminal classification. After independently proving its
external audit record, the coordinator may call
`ReopenPrepared(ctx, id, expectedDigest)`, which rechecks and rehashes the retained
object, then obtain its bound descriptor with `Prepare` and explicitly
`Publish`. Storage recovery never guesses whether an external transaction
committed. If catalog replacement succeeded but its directory sync failed, the
store degrades and retains recovery evidence; close and reopen it before
reconciling that result. The catalog may already show `ready` after reopening.

`Delete` requires the expected digest and rejects an active writer, snapshot, or
`Protect` reference. Empty failed jobs use an empty expected digest. It persists
a deletion intention before unlinking the exact registered file, then removes
the catalog entry. Both sides of an interrupted deletion are recoverable. It
never traverses or recursively removes directories and cannot address media,
database files, current encryption keys, or lifecycle generations.

`Protect` is an in-process reference only. Before deletion, the coordinator must
also check durable restore plans and other references after every restart.

## Anonymous working files

`Scratch(ctx, maximum)` creates a plaintext working file for trusted archive and
PostgreSQL processing. Its maximum must be between 1 and `MaxObjectBytes`, and
the complete maximum is reserved against `MaxTotalBytes` until the scratch is
closed. Up to eight scratch handles may exist. They are neither catalog jobs
nor durable objects, and they do not consume `MaxObjects`. `Status.ScratchBytes`
reports their reserved capacity separately from `Status.Bytes` object bytes.

The file is created through the same pinned private root using
`O_TMPFILE | O_EXCL`, with mode `0600` and link count zero. The kernel prevents it
from being linked into a directory. It never appears in the catalog, inventory,
or download API. There is no named plaintext cleanup path or new directory
manager. An anonymous inode is released after its last descriptor is closed,
including kernel cleanup after the owning processes exit.

`Scratch.File()` exposes an ordinary `*os.File` for trusted tools. The raw file
does **not** enforce the reserved byte limit: the coordinator and producer must
constrain output before writing. Write it once without truncating or punching
holes, then seek, read, or hash it as needed. Allocation must only grow until
close. The coordinator stops and reaps any owned child that inherited a read
descriptor before explicitly closing the scratch. Context cancellation closes
this process's file immediately; it cannot revoke a child's already inherited
descriptor. Arbitrary retained duplicates are not supported.

Logical quota always includes the complete scratch reservations plus object
bytes. Free-space admission counts only each scratch's remaining, unallocated
reservation, plus the new scratch reservation or impending object write, the
minimum free space, and metadata reserve. Existing object bytes and physically
allocated scratch blocks are already reflected in `statfs` and are not deducted
again. Allocations are sampled before free space, so concurrent growth makes
the check conservative. Sparse, unallocated regions remain reserved. Sampling
uses `RawConn.Control` to pin each descriptor during `fstat`; it never calls a
cached integer descriptor that could refer to a reused file after close.

`Close` first closes the Go file and then releases the full reservation. Every
concurrent close waits for that release to complete. `File()` keeps returning
the same closed Go object, whose later operations fail with `os.ErrClosed`;
it does not return nil or wrap a reused descriptor. `Store.Close` closes all
scratch handles before releasing its root directory. Kernel I/O completion has
the same shutdown limitation as regular object files.

## Limits and shutdown

Defaults are 8 GiB per object, 32 GiB in total, 128 catalog objects/jobs, a 512 MiB
free-space reserve, and eight simultaneous snapshots. Catalog jobs count toward
the object limit even when their partial bytes have been removed. Prepared
objects and in-progress writes count toward the byte limit. The free-space
check additionally reserves bounded metadata-replacement space. Requests and
integer policy ranges are finite and checked before arithmetic.

`Snapshot` pins a file descriptor and a fixed size. Its `Read`, `Seek`, and `Close`
support range downloads without following a request-supplied path. Concurrent
`Close` calls wait until the reader slot has actually been released. Context
cancellation closes the snapshot descriptor. HTTP handlers must independently
apply response write deadlines to interrupt a blocked network client.

`Store.Close` prevents new work, closes writers and snapshots, waits for their
owned resources to be released, and finally releases the process lock and root
directory descriptor. Copy/hash work checks cancellation in bounded chunks;
filesystem calls still depend on the Linux filesystem completing its syscall.
No goroutine timeout pretends that a stuck kernel I/O operation has completed.

Inode, size, and modification-time checks prevent accidental replacement and
ordinary races. They are not a claim of protection against a malicious process
running as the same Unix user that can rewrite bytes and restore timestamps.
The coordinator rehashes and authenticates an entire archive before restoring.
