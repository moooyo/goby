# Embedded subtitle removal

This contract covers the `remove_embedded_subtitle` media operation. It removes
one absolute embedded subtitle stream from a local source selected by an
authenticated native administrator. It does not delete sidecars, silently edit
every subtitle track, or accept an input/output pathname from an HTTP request.

## Source and result boundaries

Admission binds the item, media source, complete source revision, absolute stream
index, persistent root binding, and trusted execution configuration. Stage reads
the current source again and creates an exclusive private sibling directory
named `.goby-edit-<operation-id>`. Its `payload` file is on the source filesystem.
No source name or byte is changed during queued, running, or ready states.

The initial profiles are `.mkv`, `.mka`, and a conservative `.mp4` profile. The
media proof admits a container only when its structure and retained content can
be demonstrated to survive the selected removal engine. An extension alone is
not proof.
Unsupported container features, encrypted data, unrepresentable metadata,
unknown packet side data, or preservation mismatches leave the original intact.

Matroska uses FFmpeg to copy retained streams and metadata with explicit
disposition passthrough. The restricted MP4 engine instead copies the complete
source through held descriptors, replaces the selected `tx3g` track declaration
with a same-size `free` box, and clears that box's body. All other bytes,
including `mdat` and `mvhd`, remain identical. If removing a track would require
changing the movie duration, the operation is rejected. The removed subtitle's
unreferenced `mdat` bytes are retained; this is track removal, not forensic
erasure. `Engine` distinguishes `structural_edit` from `ffmpeg_remux`, and a
separate preserved-range SHA-256 proves the structural edit's unchanged bytes.

FFprobe independently verifies retained packet payloads, exact rational
presentation clocks, codec extradata, dispositions, chapters, and metadata.
The Matroska writer's generator provenance may change and is recorded in the
evidence; the MP4 structural engine preserves it. User-authored metadata is not
treated as disposable provenance.
The ready witness also contains whole-file source/candidate SHA-256 digests and
the freshly probed candidate catalog facts.

A Matroska stream explicitly typed as `attachment` may omit `codec_name` when
FFmpeg does not decode its MIME type. It still requires a filename, a MIME
declaration, and a complete bounded extradata SHA-256. Each retained attachment's
bytes and metadata are compared independently. This exception never admits an
unknown ordinary audio, video, subtitle, or data stream.

Nonzero Matroska `CodecDelay` has a separate AAC-only proof. Raw track identity,
codec declaration, audio settings and codec-private bytes must bind to the
probe's stream inventory and `initial_padding`. The integer sample/nanosecond
conversion must round-trip to the original value, and each retained track must
keep its exact delay in the candidate. Complete packet timing and `Skip Samples`
side-data comparison remain required; permitting this declared AAC priming does
not permit an unproven timestamp shift, Opus preroll, or arbitrary delay fields.

Matroska stream `DURATION` tags remain metadata that must match exactly. The
FFmpeg writer can replace a source tag with a value computed from demuxed packet
clocks; a 24 FPS source can consequently lose one millisecond in that tag even
when every packet is unchanged. A narrow repair restores an affected video's
canonical source text only through independently bound track/tag identities,
unchanged raw `DefaultDuration`, and an equal-size existing text extent. All
covering CRC-32 values are verified before the edit and updated afterward. No
other candidate byte may change during the repair. It never changes a packet
to compensate for this rounding. The complete candidate metadata, packet, raw
delay, and file-hash checks still run after preparation.

The source owner, group, ordinary permission bits, and supported extended
attributes are copied and checked. Content-bound `security.ima` and
`security.evm` signatures, special permission bits, or failed attribute copying
reject the operation. Linux and a trusted `/usr/bin/prlimit` enforce subprocess
limits, including the FFmpeg remux process's hard output-file ceiling. MP4's Go
structural copier instead enforces exact source and destination extents through
bounded descriptor reads/writes; its full source size must fit the scratch
budget before any copy or probe begins. Input is limited to 1 TiB and work to
two hours, with separately bounded subprocess diagnostics and proof records.
The captured `WritableProfiles` list is enforced by the worker. The actual
candidate limit is the smaller of captured `MaxScratchBytes` and the bounded
source-size allowance; the actual deadline is the smaller of captured
`MaxRuntimeSeconds` and the two-hour implementation limit. These effective
limits are retained in the result. Executable paths are resolved absolute files
in administratively protected directories, with captured SHA-256 checked before
and after generation. `ScratchDirectory` is used by OCR; container edits always
stage beside the source to preserve the atomic same-filesystem boundary.

## Apply and publication

Apply is a separate administrator action using the current operation revision,
source revision, and ready result hash. It revalidates authority, root topology,
host identity, source facts, candidate facts, complete digests, and filesystem
attributes. A scan or another conflicting source publication prevents admission.

The journal transitions through these boundaries:

1. `none`: a candidate may exist; the source is freely readable and scannable.
2. `prepared`: a durable intent reserves the source and blocks conflicting
   scans, deletion, root rebinding, and new playback admission.
3. `catalog_committed`: Linux `RENAME_EXCHANGE` has exchanged the candidate and
   source names, both directories are synchronized, and the owned catalog
   transaction has recorded the new file/probe snapshot.
4. `done`: old playback resources have been retired and the reservation can be
   released.

The item ID, hierarchy, metadata overrides, sidecar identities, favorites,
played state, and resume position are retained. Source-bound intro overrides,
owned OCR subtitles, and remembered stream selections naturally become stale.
All previous `Prepared`, `Playing`, and `Paused` play-session IDs for the edited
item/source become `Expired` in the same catalog transaction. Existing stop
timestamps and already-terminal history are retained. A stable media source ID
therefore cannot let an old play-session ID acquire the newly published source.
Seek indexes generated from the candidate are rebound only after complete byte
equality proves that publication changed the same candidate inode's ctime.

An attempted exchange or database commit with uncertain outcome retains the
journal as `recovery_required`. No background startup, restore, cancellation, or
cleanup routine rewrites an original media file. Explicit recovery observes
both known names and identities: it can continue an unperformed exchange,
finish catalog publication after an observed exchange, or finish resource
retirement after a catalog commit. A missing, replaced, mismatched, or foreign-
host object remains blocked for administrator inspection.

## Original-media responsibility

After a successful exchange the original inode and all its bytes remain at
`.goby-edit-<operation-id>/payload` beside the media file. Goby does not unlink
this retained original as part of completion or task cancellation. The result
reports `BackupRetained` when publication has occurred. Operators must retain
their normal media backup and may archive or remove this private original only
after independently accepting the edited file. The retained file consumes
additional space and is not a substitute for a separately managed backup.

Database backup archives include operation evidence and catalog state, not
these media filesystem bytes. Restoring a database never replays an exchange.
Recovery still requires the matching host, root binding, original, and candidate
objects. An interrupted stage without a durable ready witness can leave an
unpublished private candidate; it must be inspected explicitly. A normal failed
or cancelled stage joins its processes and removes only its held unpublished
candidate. It never uses that cleanup path after publication was attempted.
Candidate creation installs its compensation as soon as the exclusive output
descriptor exists, covering subsequent Stat and directory-sync failures. If
independent descriptor/name checks cannot prove safe cleanup, a
`media-edit-incomplete-staging-v1` journal retains the original source, host,
private directory, and any captured candidate identity. It requires manual
inspection and cannot be replayed as a validated, publishable candidate.
If ready-result persistence fails, the generation worker can abandon its
validated candidate only while its original running claim still owns the job.
An uncertain ready commit never authorizes deletion of a possibly accepted
candidate. Cancellation of a hard-interrupted stage records
`UnpublishedCandidateMayRemain` when no durable candidate witness exists.

## Verification obligations

The filesystem tests cover retained originals, strict candidate identity,
cancellation before exchange, partial-candidate cleanup, replacement refusal,
and read-only observation of interrupted publication. Media integration tests
must establish retained payloads, timestamps, chapters, tags, attachments and
the exact removed stream using actual tools. The phase acceptance run must also
exercise administrator revocation, stale source/result CAS, scan/rebind/deletion
barriers, restart on each journal boundary, stream retirement, and database
backup/restore without automatic filesystem mutation. These obligations are not
proven merely by a ready result or a zero FFmpeg exit status.
