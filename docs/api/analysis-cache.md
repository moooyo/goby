# Analysis derivative file cache

`internal/analysiscache` owns a dedicated Linux directory for disposable BIF
variants, a small caller manifest, and temporary preview JPEGs. It does not
authorize a user, validate a media source, commit database rows, decode media,
serve HTTP, or back up derivatives. Those decisions remain with the caller.

## API and publication sequence

```go
store, err := analysiscache.Open(ctx, analysiscache.Config{Root: cacheDirectory})
builder, err := store.Begin(ctx, generationKey, reservationBytes)

artifact, err := builder.WriteTemporary(ctx, "frame-000001.jpg", producer)
source, err := builder.OpenTemporary(ctx, "frame-000001.jpg")
// source implements io.ReadCloser and exposes Artifact and a read-only File.
err = source.Close()

artifact, err = builder.WriteFile(ctx, "240.bif", bifProducer)
err = builder.DeleteTemporary(ctx, "frame-000001.jpg")
publication, err := builder.Publish(ctx)
```

The producer signature is `func(context.Context, io.Writer) error`. The package
owns the writable descriptor, byte accounting, SHA-256 calculation, sync, and
close. The callback receives no output pathname or writable file descriptor.
After a producer returns, retained writer references reject further writes.
An ignored write error still fails the entire output operation.

An output producer can call `OpenTemporary` while running inside `WriteFile`.
This supports `bif.Write` with one temporary JPEG reader per `FrameSource.Open`;
it does not require all previews or a complete BIF to reside in memory.
Temporary reads have independent reference counts. Explicit scratch deletion
returns `ErrBusy` while a reader exists. Publication also returns `ErrBusy`
while any scratch reader is open, then deletes all remaining closed scratch
files before sealing the ready directory. Scratch files never enter the ready
manifest.

`Publish` returns a `Publication` containing an `Entry` snapshot. The ready
directory remains pinned until the caller makes its database decision:

1. Arrange `Discard` for every non-nil publication, including error returns.
2. Recheck the caller's current source, authorization, cancellation and database
   publication fences.
3. Commit the database reference to `Entry.Key`, `Entry.Seal`, and applicable
   artifact metadata.
4. Call `Keep` to release the publication pin. A deferred `Discard` after a
   successful `Keep` is harmless. `Keep` records a cache-lifetime decision and
   does not grant business authorization or prove a database commit.

Discard is appropriate only when the database write was never attempted, or
when rollback and absence of a reference are established. If a commit result
is uncertain, conservatively call `Keep`: the entry may be a harmless orphan,
or it may already be referenced by a committed row. Reconcile it through a
fresh database read and explicit pruning after the database is available.
Deleting it merely because the commit response was lost could break a valid
reference.

The rename may succeed before a final directory sync or descriptor close
reports an error. In that case `Publish` returns both a non-nil publication and
an error. Its bytes remain charged and protected; the caller must discard it.
An error never means that already written filesystem state ceased to exist.

`Builder.Abort(ctx)` cancels an unfinished builder and waits for its real writes
and temporary readers before removing its owned workspace. After publication,
the publication object owns the keep/discard decision instead of the builder.
`Keep` after a completed discard returns `ErrClosed`.

## Reading and maintenance

```go
lease, err := store.Acquire(ctx, key, expectedSeal, "240.bif")
// lease.File is an independent read-only *os.File positioned at byte zero.
// lease.Artifact contains Name, Size, SHA256, and filesystem Identity.
err = lease.Close()

err = store.Delete(ctx, key, expectedSeal)
err = store.PruneUnreferenced(ctx, liveDatabaseKeys)
pruned, err := store.PruneUnreferencedResult(ctx, liveDatabaseKeys)
stats := store.Stats()
resident := store.HasArtifact(key, expectedSeal, "320.bif", expectedSHA256, expectedBytes)
err = store.Close(ctx)
```

Acquisition checks the expected generation seal, the current canonical on-disk
manifest, owned file identities, selected file size, and the complete selected
file hash before returning its descriptor. It also rechecks the root and entry
directory identity. The lease pins the whole entry until its descriptor has
actually closed. Calling `File.Close` directly does not release that pin;
callers must always close the lease. Private descriptor ownership is retained
separately from the public convenience field.

`HasArtifact` reads only the registered generation index for inventory displays.
It distinguishes eviction from a still-current database reference without hashing
every BIF on a management page. It never replaces `Acquire` or source authorization;
the underlying filesystem may have changed since that entry was registered.
`ArtifactSnapshot` exposes a detached metadata snapshot for the same purpose.
Neither lookup acquires a reader or grants delivery authority.

A missing payload is classified as `ErrNotFound` only after its generation's
owner record, sealed manifest, remaining file identities and complete remaining
inventory have been checked under the unchanged cache root. Missing control
proof, links, unexpected files and identity mismatches remain errors. A known
missing generation stops appearing as resident, but keeps its conservative
logical byte charge until retirement. Startup may reclaim a verified owned
generation with missing declared payloads; it never treats missing ownership
proof as permission to delete. A wholly absent previously registered directory
can release its logical charge only after its existing readers have closed.

Explicit deletion returns `ErrBusy` for readers or undecided publications.
Admission may evict the least recently used unpinned ready entry. Builders,
readers and undecided publications are never eviction candidates.

`PruneUnreferenced` considers only known, owned ready entries absent from the
supplied live-key snapshot; busy entries are skipped. It does not inspect or
delete arbitrary directories. The caller must serialize the complete
`database publication + Keep` operation against the complete
`read live database keys + PruneUnreferenced` operation, for example with one
publication mutex. Locking only the database write or only the prune call
leaves a stale-snapshot race after the publication pin is released.
An entry sealed before a process crash
but never referenced by the database can therefore be explicitly pruned after
restart. Each generation uses a new random 64-hexadecimal-character key.
The database locates an existing ready generation for the same source/profile
and supplies its saved key and seal to `Acquire`. The cache does not derive
authority or reuse semantics from a deterministic pathname or mutable item ID.

`PruneUnreferencedResult` returns this invocation's successful removals, their
complete owned byte counts, a final total-byte snapshot, and unique busy entry
count. Multiple readers of one generation count as one busy entry. Builders and
undecided publications are included. These are direct removal receipts rather
than differences between global snapshots that could include concurrent LRU
eviction. A partial failure retains the counts already observed.

## Limits and names

Keys and seals are exactly 64 lowercase hexadecimal characters. Output names
are exactly `240.bif`, `320.bif`, `400.bif`, and `manifest.json`. Temporary
names match `frame-[0-9]{6}.jpg`; there are no caller-created subdirectories or
arbitrary relative paths.

| Configuration | Default | Accepted range |
| --- | --- | --- |
| `MaxBytes` | 2 GiB | 4096 bytes plus the root marker through 16 GiB |
| `MaxEntries` | 512 | 1 through 65536 |
| `MaxEntryBytes` | 512 MiB | 4096 bytes through 1 GiB; no more than `MaxBytes` |
| `MaxFileBytes` | 128 MiB | 1 byte through 512 MiB; no more than `MaxEntryBytes` |
| `MaxTemporaryFiles` | 8192 | 1 through 65536 per builder |

The total budget must also leave room for the fixed root ownership marker and
at least one 4096-byte reservation. Zero selects a field's default. Related budgets must still be consistent when
one field is tightened. Caller `manifest.json` is independently limited to
64 KiB. Each temporary JPEG is limited to the smaller of `MaxFileBytes` and
8 MiB. The cache treats these files as opaque bytes; the BIF/media layer owns
their format validation.

`Begin` reserves one entry and its requested byte budget before creating a
workspace. A zero requested reservation selects `MaxEntryBytes`; a nonzero
reservation must be at least 4096 bytes and no more than that limit. A 4096-byte
control allowance is kept available for the owner record, sealed manifest and
seal. Temporary files, partial output and completed variants share the same
entry reservation. Deleting a scratch file restores local workspace capacity
only after actual unlink and directory sync succeed. It does not release the
global reservation while the builder still owns the workspace.

Published byte counts include both content and private control files. Unused
reserved capacity is released when an entry becomes a protected publication.
Failed or interrupted cleanup retains its charged reservation or entry bytes;
no logical cancellation pretends that the corresponding disk data disappeared.
These are logical file-length budgets, not a filesystem block quota. Directory
inodes, allocation-unit rounding and other filesystem metadata consume
additional storage; the caller must retain an appropriate free-space margin.

`Stats` reports ready bytes, reserved builder bytes, fixed root `ControlBytes`,
their total, entry counts,
readers, pending publications and closing state. A retained workspace whose
cleanup failed remains in the builder count and reserved-byte total even when
its producer has stopped. The cleanup error is reported by `Abort` and `Close`.

## Filesystem boundary and restart

The configured root must be a canonical absolute path. Existing ancestors are
walked without following links; only a missing final root directory may be
created. The root and child directories must belong to the process user with
mode `0700`. Every cache file must be a single-link regular file owned by that
user with mode `0600`. Files are opened with `O_NOFOLLOW`, `O_NONBLOCK` and
close-on-exec. Symlinks, hard links, special files and unexpected permissions
are rejected before data can be truncated or read.

An ownership marker identifies the cache namespace. A nonblocking kernel file
lock enforces one writer until all owned operations and leases have ended.
The root path, lock inode and marker identity/content are rechecked during
operations; replacing a root pathname does not redirect existing authority to
another directory. Other platforms return `ErrUnsupported`.

Each workspace has a durable owner record before artifact writes begin. A ready
manifest records every final filename, byte count, SHA-256 and filesystem
identity. Its canonical JSON is protected by a separate SHA-256 seal. Final
file syncs and directory sync precede an atomic no-replace directory rename;
the cache then syncs the parent directory. An existing destination is never
silently replaced.

On restart the writer lock is acquired first. The package removes only valid
owned temporary/trash namespaces, restores verified sealed ready metadata,
and enforces the configured budgets. Empty remnants are removable only within
this root's private temporary/trash namespace. Cleanup checks the complete
single-level inventory before unlinking and keeps its owner marker until the
end. Interrupted partial cleanup can therefore be retried.

Unknown names, foreign or malformed ownership markers, changed permissions,
unsealed ready entries, and unsafe file identities are retained and reported.
The package does not recursively erase such a tree to make startup appear
successful. It provides no guarantee against a privileged administrator
modifying files after a verified read lease is handed out; external tools must
not mutate an active cache directory.

## Shutdown and failure accounting

`Close` rejects new admissions, cancels unfinished builders, and waits for the
actual producer/file operations, reader leases and publication decisions.
Ownership is released only after those operations have ended. A context timeout
only ends the caller's wait. It leaves the same shutdown running, retains the
lock and charged live data, and allows a later `Close` to wait for completion.

Filesystem calls and a producer that ignores cancellation are not forcibly
terminated or replaced by a new worker. A blocked callback or forgotten lease
can keep shutdown pending. This is reported as unfinished work instead of
claiming that an underlying operation stopped. Once operations have ended,
retained cleanup failures are returned without claiming that their files were
removed.
