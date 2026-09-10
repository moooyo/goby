# Recovery coordinator control record

This Linux-only package stores one opaque coordinator record in the dedicated
directory configured by its caller, normally `/var/lib/goby/recovery-operations`.
It is separate from lifecycle generations, backup archives, media, caches and
logs. The caller supplies the existing lowercase 32-character hexadecimal
deployment ID and owns the business JSON schema.

The public API is:

```go
Open(ctx, directory, deploymentID) (*Store, error)
store.Read(ctx) (Snapshot, error)
store.CompareAndSwap(ctx, expectedDigest, payload) (Snapshot, error)
store.Close() error
```

`Snapshot` contains `Revision`, `Digest`, and an independent `Payload` byte slice.
Revision zero has an empty payload. Later payloads must be UTF-8 JSON objects
with at most 1 MiB of input and at most 64 levels of nesting. Duplicate keys at
every object level are rejected. Insignificant whitespace is removed; numeric
representations and object member order are preserved. The business schema and
any decision to exclude credentials from payloads remain the caller's contract.

The digest identifies the complete canonical record, including the deployment,
a random store ID, revision, previous digest and payload. It is not only a hash
of the JSON payload. Every successful CAS advances the revision, including an
identical-payload write. A stale or foreign digest returns `ErrConflict`.

## Filesystem ownership and process fencing

`Open` walks every path component without following symlinks. Only the final
directory component may be created. Existing ancestors are never claimed or
chmodded. The final directory must have mode `0700` and the effective process
UID. Initial claim requires an empty directory.

The store holds exclusive nonblocking flock locks on both its pinned directory
descriptor and its fixed mutex file until `Close`. The immutable ownership
marker binds the deployment, random store ID and mutex device/inode identity.
Every regular file must have mode `0600`, one hard link and the effective UID.
Requests recheck the named root, marker, mutex and fixed file identities and
hashes. Current-record identities are also registered in the publication proof.
Another cooperative process using the same directory is rejected with
`ErrBusy`, including when the mutex pathname has been replaced while the first
process still holds its directory lock.

These locks do not coordinate another directory, another host, or a database
used through different paths. The caller must retain the separate deployment
and PostgreSQL fences where its operation requires them.

## Bounded publication proof

The store has four fixed files: its immutable marker, mutex, `current.json`, and
`cas-proof.json`. The proof contains only the most recent before and candidate
record revisions, whole-record digests, previous digests, and device/inode
identities. It does not contain a payload body. There is no accumulating history
or automatic operation replay.

For each CAS:

1. Read and validate the actual current record, then compare its complete digest
   with the caller's expected digest.
2. Write a fresh candidate inode and sync it.
3. Atomically replace and sync the proof, binding the actual current record as
   `before` and the new record as `candidate`.
4. Atomically rename the candidate to `current.json`, then sync the directory.

The current file must match exactly one of the proof's references. If an earlier
CAS published its proof but not its candidate, the next CAS uses the actual old
current record as its predecessor. It never advances from an uncommitted
candidate. Revision and previous-digest relationships are checked along with
record bytes and inode identity.

If a rename succeeds but its parent-directory sync fails, the store returns
`ErrRecoveryRequired` and becomes unavailable until closed and reopened. The
caller must reopen and `Read` to inspect the visible revision and digest. A
candidate may already be current; blindly repeating the old business mutation
is not justified. The package never restores the old payload automatically.

Before the first marker is published, initialization durably creates both the
revision-zero current record and its proof. Consequently, a published marker
with a missing current or proof file always fails closed; it cannot silently
become a new empty store.

## Interrupted initialization and temporary files

A normal failed initial claim can remove only exact fixed-file inodes created
by that call, while no marker was published and its directory lock is held. If
the marker was published, all files are retained even when its sync failed.

A process crash before marker publication can leave a mutex, initial current,
proof, or temporary file without a published ownership marker. Reopening that
nonempty directory returns `ErrRecoveryRequired`; an operator must inspect the
specific incomplete claim. The package neither claims nor deletes those files.
It does not claim that initial creation has fully automatic crash recovery.

After ownership is established, `.next-` files with safe regular-file ownership
may remain from interrupted writes. They are inert, are never adopted as a
current record, and are never deleted just because their names match. Normal
call cleanup can remove only a temporary inode that the same call created.
Enumeration permits at most 128 directory entries; excessive retained debris
fails closed and requires explicit operator maintenance. This is a bounded
control record, not an unbounded recovery log or garbage collector.

## Cancellation and trust boundary

Context cancellation is honored before operations, while waiting for the
serialization gate, and between bounded reads and writes. It cannot interrupt
a kernel filesystem call, such as an `fsync`, already in progress. Cancellation
before the current rename leaves the prior record selected. Once a rename has
occurred, the call completes durability work or reports an uncertain outcome;
late cancellation never rolls back a published record. `Close` waits for an
in-flight operation before releasing process locks, and is concurrency-safe and
idempotent.

The service UID is a trusted boundary. These checks reject observed path, mode,
link and content changes, and fence cooperative deployment processes. They do
not isolate the service from a malicious same-UID process racing pathname
operations or modifying all ownership metadata. The retained before/candidate
proof also does not prevent an external actor from restoring an old disk image
or the exact previous inode and bytes. It provides crash-consistent local CAS,
not anti-rollback storage or proof that a business recovery operation succeeded.

Non-Linux builds expose the same API and return `ErrUnavailable` for compilation
compatibility. Linux operation needs no capability or privileged system call
beyond normal ownership of the configured directory.
