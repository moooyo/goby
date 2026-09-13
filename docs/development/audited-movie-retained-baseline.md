# Retained movie04 baseline

Status: offline implementation and targeted remote verification passed.
This document does not authorize movie05 or another browser run. The movie
lifecycle and baseline [closeout](audited-movie-offline-alignment.json) binds the
verified source set; a new bounded execution decision remains separate.

Movie04 completed authentication and cleanup but never delivered media or sent
Playing, Progress or Stopped. Its immutable
[failure closeout](audited-core-movie04-failure-closeout.json) binds one unstarted
Prepared row and an initialized, zero-history user-data row. All eleven
authentication sessions are revoked. The existing candidate, eight accounts,
three libraries, media, runtime epoch and seed provenance remain authoritative.

The input authority is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-core-movie04-failure-closeout.json`,
SHA-256 `5843a790e3c4ba09109d145b64fbda58f94c7ee94092e898ab584bab37880e8d`.
Its `sourceAfter` must be the saved movie04 `private/source-after.json`, SHA-256
`7209b3845d8290cd3883c76f5a95f00066d85d61103b8c5765493472196ad01c`.
These are existing receipts; neither file is rewritten.

## Input and baseline contract

The outer `audited-candidate-client-run-input` version 2 adds exactly one field,
`retainedBaseline`, containing the fixed closeout descriptor above. Version 2 is
accepted only for `scenario: "movie"`. The controller forwards that descriptor
into `audited-candidate-client-closeout-input` version 2. Browser manifest version
1 and all six seed role mappings remain unchanged. Version 1 and the other five
scenarios retain the original empty actor play/reference precondition.

Both readers load and hash-check the same saved closeout and source snapshot.
The fresh source-before snapshot must have exactly the saved 35 table contents
and sequences; only `capturedAt` advances. The Python gate uses canonical JSON
comparison so nested booleans cannot compare equal to numeric zero or one.
The original ordinary movie user,
its policy and every other persisted field therefore remain bound. The semantic
checks additionally require:

- Exactly one prior play, `play_40969548543a02735b847a89bc34671b`, owned by the
  original movie actor and selected movie, with its original device and source.
- Authentication `aadd2636638e28cc6ceaf5bb8f2cb132` belongs to that actor/device,
  is an Emby credential and is revoked. All eleven old sessions remain revoked.
- The play is Prepared, not counted, at position zero, with null started/stopped
  timestamps. References and encoding jobs are empty.
- The selected actor has one initialized user-data row with zero position/count,
  false favorite/played flags and null `last_played_at`. Its actual saved row,
  including timestamps, is the initial value; it is not replaced with a default.

Unknown rows, changed or missing fields, live credentials, nonzero history and
different authority descriptors reject the new input before browser startup.

## The only prior-play transition

`internal/library/play_sessions.go` calls `cleanupPlayback` before preparation.
For an ordinary user this cleanup covers that user's earlier credentials.
Revocation makes the old Prepared row eligible for expiration even before its
time limit. A later authentication cannot reuse the old play because canonical
selection also binds the authentication and device.

The closeout permits exactly this old row to become Expired. Only `state`,
`stopped_at` and `updated_at` may differ. Both timestamps must independently fall
inside the first actual successful PlaybackInfo request window for the selected
movie and a new owned authentication. They need not be identical. All other old
play fields must remain exact, and the row must remain present. The old row and
old credential cannot participate in a new physical play chain or new-play
count. All other existing-row protections continue unchanged.

New play, authentication, reference, device, activity, encoding and user-data
changes still pass the existing complete durable comparison. User-data counts
start at the saved zero row and increase only for newly proved counted plays.
The final closeout records `retainedBaselineExpiration` separately from new
counted plays and from newly retained preparations.

## Pruning boundary and failure handling

Before execution, the saved state has only one play. The full 1,200-second outer
window must finish before `old.expires_at + 7 days`. The closer also checks the
actual after time and requires `1 + potential creation requests <= 256`, with no
more than 256 retained actor plays. PlaybackInfo and Started-report requests are
counted conservatively even if they reused an existing row. These checks prove
that the seven-day and terminal-capacity pruning rules do not explain deletion
of this prior row in an accepted run; they do not change the gateway budget or
authorize a cleanup request against the old credential.

Any unexpected transition, deletion, extra row, out-of-window timestamp or
capacity breach is a failed closeout. There is no automatic browser retry,
account replacement, database reset, scan, old-token reuse or history deletion.
Cleanup commits in a separate transaction before preparation. A failed prepare
can therefore leave the old row expired; a failed request or missing successful
window does not imply that this change rolled back. Preserve and reconcile that
failed attempt independently rather than replaying the business operation.

## Targeted offline verification

The first offline component run retained a failed saved-snapshot replay:
`json_unsafe_number`. The SQL snapshot contains eight exact, 19-digit file
change-time integers: two `tables.item_subtitles[*].change_time_ns` values and
six `tables.items[*].media.FileChangeTimeNs` values. The older binary epoch also
contains large `preservation.oldBinaryFacts.ctimeNs` and `mtimeNs` integers.
These are metadata representation boundaries, not playback counters or a
product regression. The failed verification scope remains preserved.

`sourceSnapshotJSON` now decodes only the two declared SQL paths as canonical
decimal strings in memory. Subtitle change time follows the PostgreSQL
`bigint NOT NULL CHECK (change_time_ns >= 0)` constraint. The media field follows
Go `int64`; its enclosing `media` object may be null. A present numeric leaf
must be an integer token within its range: strings, null, objects, fractions
and exponents are rejected. Unknown unsafe numbers, including business counts
and duration fields, remain rejected.

`previousBinaryEpochJSON` is a separate role restricted to the fixed predecessor
descriptor with SHA-256
`7bcdbc529fd1ba3f6a62f66585e6788cc9efa1aac22a4accc8d339d69ccf6ae2`.
It permits only the two `oldBinaryFacts` stat timestamps to use the same exact
signed-int64 decimal-string representation. It retains every other provenance
field; general provenance and public/API `strictJSON` parsing are unchanged.
When that same fixed binary epoch is selected directly as a version 1 current
epoch, `runtimeEpochJSON` uses the identical restricted decoder. All other
current-epoch descriptors retain strict parsing. This preserves the existing
version 1/version 2 read contract without granting arbitrary provenance an
unsafe-number exception.

The shared scanner tracks complete JSON paths, validates duplicate keys, UTF-8,
size, node and depth limits, and replaces only approved numeric tokens before
`JSON.parse`. No unsafe value first passes through a JavaScript Number. Original
file bytes and descriptor hashes are verified and retained without alteration.
The live closeout reader uses these explicit roles for source-before,
source-after, retained source state and the fixed previous epoch; the saved
replay exercises those same decoders and validates the actual runtime lineage.

Only `ssh test-env` may execute verification. The Python guards exercise input
version separation, exact baseline matching, wrong/live authentication, unknown
rows, nonzero history and the seven-day boundary. The JavaScript guards run the
complete two-login movie durable comparison and reject changed/deleted old
fields, wrong ownership, count changes, wrong preparation windows and capacity
overflow.

Both test entry points accept `--replay-retained-snapshot`. This reads only the
fixed closeout, saved source snapshot and existing seed binding. The JavaScript
replay preserves the actual old 35-table snapshot, synthesizes two new playback
lifecycles and related rows in memory, and runs the complete durable comparison
plus negative mutations. It performs no HTTP, SQL, service or browser action and
does not claim that the simulated after-state occurred.

Run the Python test file with `python3 -I -B`. Run the JavaScript test file with
`--source <frozen closer.mjs> --output <fresh private report.json>` and optionally
the replay flag. Fresh tool verification receipts and the final source-pin set
must be integrated before any later execution; this change deliberately does
not guess future verification hashes or revise product, runtime or seed pins.
