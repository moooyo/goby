# Backend design review follow-up, 2026-10-09

Implementation follow-up:
[backend-followup-implementation-20261009.md](backend-followup-implementation-20261009.md).
The review below retains its review-time findings and status.

## Scope and method

This review starts from `d2952a3f` and the current working tree. The R01-R30,
N01-N22 and L01-L08 findings in the previous review and implementation records
are the deduplication baseline. Additional examples of an existing finding do
not count as new recommendations. Existing unrelated edits are preserved.

Four parallel reviewers inspect permissions, defensive work, architectural
performance and the Linux-only boundary in sequential rounds. The primary
reviewer checks the call chains and proposed safety boundaries. A round ends
only after all four areas report; three consecutive complete rounds without
a new accepted recommendation are required to stop. Six rounds completed;
rounds 4, 5 and 6 each added no new recommendation across all four areas,
satisfying that condition. This is convergence within the inspected scope,
not proof that every possible future optimization has been exhausted.

Only residual Linux-only cleanup is implemented. Other entries below are
recommendations, with costs inferred from code rather than measured latency
or throughput. The previous changes already removed production Windows server
support; this review does not attribute those changes to the current task.

## Assessment

Eight recommendations remain: Q04/Q06 are P2, and Q01/Q02/Q05/Q07/Q08/Q09
are P3. Q03 is completed platform cleanup. Q09 is an ancillary concurrency
contract issue, not evidence of overengineered permissions.

| Requested area | Current result |
| --- | --- |
| Permission complexity | Q02 identifies duplicate playback-context routing within one request. Current resource authority and revocation checks remain necessary. |
| Excessive defensive work | Q01 applies service configuration too early; Q07/Q08 repeat validation/decoding work whose successful result can be passed directly. |
| Architectural performance | Q04 reduces sequential database statements; Q05 avoids unused snapshots under a shared mutex; Q06 avoids serial connection timeout stalls. |
| Linux-only backend | Q03 removes obsolete non-Linux test scaffolding. Production platform removal was already delivered in the preceding commits. |

Prioritize Q04 for its clear full-size transaction cost. Q06 matters when a
provider has an unreachable address before a reachable one. Q02/Q07/Q08 are
narrow data-flow changes; Q05 needs careful publication-result atomicity.
No natural-load performance improvement is claimed for any recommendation.

## Findings

### Q01 - P3: Dispatch static CLI help before loading service configuration

`cmd/goby/main.go:67-73` loads the complete configuration before calling
`runCLI`. The help branches in `cmd/goby/recovery_cli.go:41-44` only emit static
text, yet a missing database URL fails validation first
(`internal/config/config.go:134-136`). Invalid unrelated listener or media
configuration can prevent an operator from viewing recovery instructions.
The OCI entrypoint forwards arguments to the same executable.

Recognize and print `help`, `--help`, `-h` and `recovery help` before loading
service configuration. Keep actual serving and maintenance commands subject
to complete configuration, storage, ownership and recovery checks. This needs
a small entrypoint separation, not another configuration framework.

Verification should exercise the executable entrypoint with absent and invalid
configuration. Direct `runCLI` tests do not cover the ordering defect. Actual
`serve` and recovery operations must continue rejecting invalid configuration.

### Q02 - P3: Restore the same application-key playback context once per request

For a metadata-free application-key request to
`GET /emby/Items/{Id}/PlaybackInfo?CurrentPlaySessionId=...`,
`internal/server/auth.go:106-119` restores the context and touches its activity.
`internal/server/playback_info.go:124-135` then restores the same context again.
Each successful restoration uses a transaction containing three SELECTs in
`internal/identity/application_key_clients.go:166-197`.

Carry request-local transport/binding facts so the same validated identifier
does not trigger a second routing transaction. Keep fresh resource authority
and play ownership checks in `OpenMediaFor` and `PreparePlayback`, including
the credential checks, owned session read and commit checks in
`internal/library/play_sessions.go:361-430`. No cross-request permission cache
is proposed.

Reuse only a successful binding for the same credential/key, play ID and
resulting client context. A prior call or a prior not-found result alone is
not a reusable authorization proof. Body reads and activity writes may wait;
the request-local result must not replace checks after those waits.

Preserve body-only requests, query/body conflict detection, explicit client
precedence, missing/foreign resource responses, trusted peer context and
activity attribution to the actual client. Do not simply remove every handler
binding: POST bodies can carry information absent from middleware. A play ID
remains a correlation hint, never a credential. Test revocation between stages
and after waits, body/query variants, and actual SQL statement counts.
Stopped or expired play sessions may still restore a client context; resource
validity is checked later. Do not add a live-play filter to routing as part of
this optimization.

### Q03 - Completed: Remove the final explicit non-Linux contract test

`internal/commanddomain/executable_capability_test.go` still tested the removed
non-Linux implementation through `TestExecutableCapabilityUnavailableOutsideLinux`.
On Linux it always skipped. The function and its unused imports are removed;
the nil/zero capability and JSON trust-transfer tests remain.

No remaining production Windows server branch was found. Retain the
`linux && !amd64` command-launcher rejection, OS reporting, unsafe-path
rejection, persisted historical error values, Windows playback clients and
Windows developer-host orchestration. Vendored platform macros preserve
upstream source and are not a Goby Windows server implementation.

### Q04 - P2: Batch episode-roster entry upserts

`internal/library/expected_episodes_store.go:246-258` executes one UPSERT per
entry while holding the catalog owner transaction. The input allows 2,000
entries (`internal/library/expected_episodes.go:18`), making 2,000 sequential
entry statements possible through the administrator episode-roster PUT route.
The owner mutex serializes catalog writes until this transaction finishes
(`internal/library/ownership.go:159-170,226-244`).

Use one typed input relation or bounded batches for the validated entries.
Keep duplicate-input rejection, stable expected-episode IDs, series locking,
revision compare-and-swap, source revision/content conflicts, historical
imports, retirement/reactivation semantics, final actor checks and atomic
rollback. Existing OCR and collection batching did not change this path.
Prefer the protected `ownedTx.Exec` path; blindly using the embedded
`SendBatch` with the HTTP context would bypass its write-context and ownership
handling. Bounded batches must still commit as one transaction.

Verify replacement, withdrawal, reactivation, duplicate and concurrent edits,
date nullability and fault rollback. Measure statements and owner hold time
with a full-size roster before making a latency claim.

### Q05 - P3: Return only the needed result from subtitle-free live publication

`internal/timeshift/store.go:428` returns `snapshotLocked(window)` after every
successful publication. Lines 634-643 deep-copy all retained segments and
epoch artifacts while holding the store mutex. The sole production caller,
`internal/server/dynamic_publication.go:151-179`, needs only the latest segment
when the session has no subtitles. Full history is consumed by subtitle
pruning only when subtitles exist.

Offer a compact publication result and construct full history only for callers
that need it. Select the result after expiration under the same publication
lock, keep an owned copy of returned artifacts, and preserve empty-result,
scope, generation, revision, retention and subtitle behavior. A separate later
`Snapshot` call would introduce a race and is not an equivalent replacement.
Determine subtitle needs under the session lock or from stable plan data;
do not introduce an unsynchronized read of `session.subtitles`.

This removes unused copying and allocations. It does not make all publication
work constant-time: expiration and other window maintenance remain separate.
Compare allocations and mutex hold time across window sizes, with subtitle,
generation-change and post-expiration cases.

### Q06 - P2: Avoid serial full-timeout dialing across validated provider addresses

`internal/providers/http.go:54-84` validates all resolved addresses, then dials
them one at a time with a ten-second timeout. If the first permitted address
silently drops connections while a later address works, the successful route
waits for the earlier timeout. Several unreachable addresses can exhaust the
thirty-second HTTP budget. `requestBytes` holds one of four shared network
slots throughout this wait (`http.go:114-142`), affecting unrelated providers.

Use bounded, staggered connection attempts over already validated literal
addresses, with one total deadline, prompt cancellation and closure of losing
connections. Preserve whole-answer public-address rejection, literal-address
pinning, host allowlisting, TLS hostname verification and redirect rules.
Do not delegate unchecked hostname resolution back to the dialer.

This is connection establishment only; do not replay HTTP operations, notably
quota-consuming subtitle POSTs. Keep provider concurrency and MusicBrainz rate
limits. Verify a stalled first address and working later address, mixed
public/private answers, cancellation, all failures and late successful losers
with a controlled resolver/dialer rather than relying on public DNS behavior.

The same sequential literal-address pattern also occurs in
`internal/notifications/http.go:79-87`, with a three-second per-address timeout.
This is additional Q06 coverage, not another recommendation. Preserve that
transport's post-TLS/pre-body authorization and actual worker/connection
retirement if a small shared dialing helper is introduced.

### Q07 - P3: Skip the second JPEG decode for an untransformed BIF thumbnail

`internal/server/analysis_preview_http.go:185-188` reads a frame through
`index.JPEG` and then calls `artwork.Render`. BIF validation already checks the
complete current JPEG, its framing and dimensions, and decodes its pixels
(`internal/bif/bif.go:475-510`). Artwork then copies and decodes the same bytes
again before returning them unchanged when maximum width and quality are zero
(`internal/artwork/artwork.go:226-230,294,364`).

For the explicit no-transform case, return the already validated JPEG bytes.
Keep `Render` for resize/quality requests. The BIF default limits fit within
the artwork output limits, so this branch does not need to trust stored
metadata dimensions or add a general decoded-image abstraction.

Preserve the first full decode, bounded preview admission, current source/hash
checks, lease lifetime, final `Revalidate`, cancellation and conditional HTTP
behavior. Do not move conditional responses ahead of representation validation.
Verify unchanged JPEG bytes and headers, corrupt/truncated frames, cancellation
and retained transform behavior. Measure allocations/CPU separately before
claiming a speedup; this is not a new cross-request image cache.

### Q08 - P3: Return the validated waveform level without rereading the payload

`internal/library/audio_waveform_files.go:303-332` reads the complete GAWF
payload, checks its SHA256, parses all tracks/levels and checks file identity,
then returns only a summary with the descriptor. The level HTTP handler opens
that validated artifact and repeats the full read and parse
(`internal/server/audio_waveforms.go:257-272`) before selecting one level.
The payload is bounded at 4 MiB and is already decoded into owned arrays.

Provide a narrow level-read operation or hand off the owned decoded result
after final authorization and source checks. Preserve the complete initial
payload/hash/format checks, manifest-summary agreement, path/descriptor
identity, source revision and stale checks, tag/stream/bucket validation and
the existing GAWL/ETag/Range representation. A manifest alone is not proof of
current payload integrity.

Keep cancellation and real media-worker retirement semantics. In particular,
do not publish captured results that a cancelled worker is still modifying
or release admission before its descriptors close. Verify tampering, source
replacement, cancellation and all selected track/level cases, then compare
read bytes and allocations. This is an intra-request data handoff, distinct
from Q07's no-transform JPEG renderer branch and the earlier image cache.

### Q09 - P3, ancillary reliability: Publish snapshot failure before reader retirement

`internal/diagnostics/store_linux.go:853-875` checks degradation before joining
its snapshots, but does not fold newly reported degradation into its cached
close result afterward. A snapshot read can be waiting to report an I/O error
while `Store.Close` holds the store mutex. Once that mutex is released, the
read marks degradation, the descriptor closes successfully, and store close
can still cache a nil error.

There is a related gap in `internal/diagnostics/snapshot.go:119-125`: snapshot
close removes the reader before reporting its own close failure. A store close
between those callbacks can miss both the reader and the failure. A final
degraded-state read alone does not close this second gap.

Consolidate failure publication and reader retirement under the store mutex,
and decide the terminal store result after the required readers have joined.
Preserve snapshot/store lock ordering, actual descriptor closure and idempotent
concurrent Close results. Deterministic tests should force both interleavings.

This is an existing concurrent package-contract issue, not a performance or
authorization finding. Normal application shutdown drains generation/HTTP
work before closing diagnostics, substantially narrowing production exposure;
it is therefore lower priority than Q04/Q06. No data-loss exploit is claimed.

## Review-loop ledger

The ledger records completed rounds; the counter resets after every round
with a new accepted recommendation.

| Round | Completed inspection | New recommendations | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Current authorization chains, startup/recovery boundaries, roster transactions, timeshift publication, and residual server-platform code. | Q01-Q05, including completed Q03 | 0 |
| 2 | Asynchronous delivery authority, provider networking, scheduler/cancellation, diagnostic writer, image/derivative reads, release targets and native helpers. | Q06-Q07 | 0 |
| 3 | Cross-review of proposal boundaries, waveform data handoff, backup/recovery streaming, snapshot shutdown interleavings, and executable-capability callers. | Q08-Q09 | 0 |
| 4 | End-to-end user-policy mutations, live publication/advertisement, provider and roster consumers, cancellation/close chains, and deployment/evidence correspondence. | None | 1 |
| 5 | Maximum inputs, revoked/expired actors, conflicting client hints, stale sources, partial failures, cancellation and late worker/connection completion. | None | 2 |
| 6 | Final production call-chain review, permission/data handoffs, structural-cost claims, retained failure boundaries, platform diff and raw-evidence consistency. | None | 3 - stop |

## Rejected simplifications and retained boundaries

- Do not remove current authority checks across database waits, asynchronous
  event delivery, publication or commit. They observe different states.
- Keep CSRF, remote-peer policy, source identity, path containment and complete
  payload validation. Reusing a successful local result is narrower than
  caching trust across requests.
- Retain actual process/descriptor retirement, snapshot revision/advertisement
  checks and bounded queues/workers. They protect concrete ownership and
  resource-lifetime boundaries.
- Do not add raw `ReaderFrom`/`WriterTo` shortcuts to media adapters merely to
  reduce copying: they can bypass chunk admission, byte accounting and idle
  deadline refresh.
- Additional read-only inventory owner-gate use is existing R22 coverage;
  extra count/page reuse is R17 coverage. Neither is a new finding here.

## Verification and resource closeout

All verification ran through `ssh test-env` on pinned Go 1.27.1, Linux amd64,
with `CGO_ENABLED=0`, `GOMAXPROCS=2` and one package compiler at a time. No local
test, build, validation suite or runtime probe was run.

The complete commanddomain package test binary compiled. The two retained
tests in the changed file passed: `TestExecutableCapabilityZeroAndNilRejectOpen`
and `TestExecutableCapabilityRejectsJSONTrustTransfer`. Remote `gofmt -l`
reported no formatting changes. This is targeted cleanup verification, not a
full backend suite or performance measurement of the recommendations.

The task reused `/root/.cache/go-build` and `/root/go/pkg/mod`. The budget
allowed 16 MiB source/evidence, 32 MiB binary, 256 MiB shared-cache growth,
128 MiB compiler scratch and 32 MiB fixtures while preserving 1.5 GiB of disk.
Both native `GOTMPDIR` and `TMPDIR` used the task-owned persistent fixture
directory; compiler scratch used a separate task-owned `/tmp` directory.

Fresh closeout inspection found no Go/compiler/linker/test worker. The exact
empty, root-owned compiler scratch directory was removed. No private cache
was created, and no shared cache or unrelated resource was cleared. Source,
binary and evidence remain under `/opt/goby-backend-design-review-20261009-04`.
The exported closeout log records persistent availability of 2,583,937,024
bytes and 7,618,560 allocated bytes for the retained task. Removing empty
scratch is not claimed as a capacity gain.

Raw logs and the source manifest are also retained locally under
`.artifacts/backend-design-review-followup-20261009/evidence/`.
