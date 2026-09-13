# Version-3 client evidence input

This extends the existing controller and closeout; it does not create another
runner or authorize a browser attempt. Versions 1 and 2 retain their historical
contracts and frozen inputs.

This verified revision was consumed by movie06. Its [owned-state closeout](audited-core-movie06-owned-state-closeout.json)
changes the retained snapshot, so this exact implementation cannot admit another
movie run. The [plan review](audited-movie06-plan-review.md) specifies the pending
baseline adjustment; no per-attempt constant substitution is authorized here.

## Input and retained state

Controller version 3 has the existing base fields plus `retainedBaseline`.
For `movie`, this must be the descriptor of
`candidate-core-movie05-owned-state-closeout.json`, SHA-256
`7c38d00bcb4ece5e056e06a22be848cbd5e76c77f7d5b642d06f656be3dc27ab`.
For the other five scenarios it must be null, preserving their ordinary fresh
actor gate. Movie05's source-after descriptor is fixed at SHA-256
`be0dbd70d4f7ea7d4343a3ea6259f80be216b9239e7acfa6552b2c7858b33bb0`.

The movie baseline contains thirteen revoked sessions, five play rows, two
counted Stopped plays, two Expired unstarted rows and one unstarted Prepared row.
The user has play count 2 and position 1,217,878,390 ticks. Its sole Prepared ID
is `play_b2977a52015916374e8a8af16188c191`, under revoked authentication
`e5649a6dc8451164243a4213f12d0ba2`. The complete 35-table/sequence before snapshot
must match the saved source-after, except its capture timestamp. The ordinary
actor, complete prior history, ownership and zero refs/encoding state remain
bound to the saved closeout. Only that Prepared row may transition to Expired,
with only `state`, `stopped_at`, `updated_at` changed in the first successful
owned PlaybackInfo window. All older rows remain exact and excluded from new
play counts. Guard the pruning deadline for every old actor play and bound total
possible creations below the 256-row pruning threshold. New userdata counts
start from 2; they are not reset to zero.

Closeout version 3 has its existing fields plus `retainedBaseline` and
`serverLog`. The former follows the same movie/null rule; the latter is a required
descriptor of the receipt below. The browser manifest remains version 1.

## Owned server log receipt

The controller captures the same candidate's actual stdout file before worker
startup and after both workers close and the source-after sample is saved. It
must perform the latter capture even when the browser exits unsuccessfully.
Log reads issue no HTTP, SQL or service action. Both full captures are bounded
to 32 MiB, read through `O_NOFOLLOW`, preserved privately, and end in a complete
newline. The after bytes must start with the complete before bytes. All request
analysis uses only the appended portion.

The receipt has exactly these fields:

```javascript
{
  kind: 'audited-candidate-client-server-log', version: 1,
  runId, runtimeEpoch, input, controller, before, after
}
```

`runtimeEpoch`, `input` and `controller` are descriptors. Each `before`/`after`
snapshot has exactly `capturedAt`, `candidateBefore`, `candidateAfter`, `file`,
`length` and `content`. Both candidate objects are the epoch's exact ten-field
process identity, checked around the read. `content` is the raw-file descriptor;
`length` is the captured prefix length, not a claim that the append file stopped
growing during the read. `file` has exactly `path`, `device`, `inode`, `uid`,
`mode`, `links`; device/inode are positive decimal strings. It must identify
the root-owned 0600, single-link regular file at
`/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b/private/server-unit.log`.
The file and `/proc/<candidate-pid>/fd/1` must have the same device/inode, and
both snapshots must retain the same file identity. Rotation, truncation,
prefix rewriting or process drift rejects the receipt.

The closeout validates descriptor hashes, exact process/file/baseline binding,
capture order around the browser run, prefix preservation and bounded structured
JSON for each appended log line. It indexes only `request completed` events with
a valid request ID, recording duplicate IDs as a failure rather than silently
choosing one. The input descriptor must match the manifest approval's
`authorizedRunInput`; the controller descriptor is retained as provenance.

## Cancelled empty media responses

Every physical exchange remains present with its original status and flags.
A complete owned media GET 200 with zero delivered/body bytes can be classified
as a cancelled empty response only when its unique `X-Request-Id` matches one
appended event from the bound candidate with method GET, the corresponding media
route family, status 200, bytes 0, outcome `cancelled`, and an event time inside
that physical exchange's wall-clock window (with an exact 1 ms tolerance using
the retained fractional timestamps). That request ID must appear in only one
physical exchange; a log event cannot justify multiple responses.
The request must still be delivered completely. Its response must have a zero
Content-Length, no transfer encoding and exactly one `X-Request-Id` field; it
must fall before the owning token's logout. It proves neither media delivery nor
a started/counted lifecycle. Any unstarted preparation, including a Universal
audio preparation before delivery, still needs separate complete ownership and
request evidence under the durable-state rules. Each successful
lifecycle still requires its own positive delivered media bytes and all prior
identity, report, durable-state and cleanup checks. Missing, contradictory,
duplicate, foreign, out-of-window or non-cancelled log evidence rejects the case.

The unchanged zero-page-error gate remains in force. The new diagnostics can
explain a future event; they cannot reconstruct the four historical movie05
errors or turn its original failed result into a successful browser exit.
