# Phase 3 bounded original-stream overload check

Status: **source only, unexecuted**. This helper is separate from the complete
10k/100k cold/cached/incremental capacity journey. Run it once per admitted tier
under the normal deployment after that journey, with the actual fault fixture's
independent sentinel viewer already prepared. It changes no scan expectation,
catalog row, profile threshold, account, service configuration or media source.

The scope is deliberately narrow: original-response per-user admission, truthful
HTTP refusal, another user's remaining allowance, slot return, local socket
closure and production lease completion. It does not decode media, measure
streaming throughput, establish general concurrent playback capacity, verify
the conversion queue, or replace either tier's full workload acceptance.

The production contract is `maxOriginalOwnerStreams = 8`. Account ownership is
the user ID, so another login/device for that user shares the limit. The ninth
original request maps `ErrBusy` to HTTP **429**, Emby
`ResponseStatus.ErrorCode = "stream_limit"`, and `Retry-After: 2`. The shared
global HTTP stream limit is 64; using only eight owner streams and one independent
viewer distinguishes the per-user quota from global exhaustion. No test-only
limit or handler is added.

## Frozen private binding

Invoke only after source closure and remote execution admission:

```text
/usr/bin/python3 -I -B media-analysis-phase3-overload.py --binding /owned/actor-private/overload.json --output /owned/results/new-overload
```

The binding is a canonical absolute, actor-owned `0600` file with one link and
at most 2 MiB. All three references below are `{path,sha256}` pins of exact
actor-readable private bytes. Runtime/exporter produces the context from actual
normal service/PG identities and the fault fixture receipt; do not type business
IDs or credentials by hand.

```json
{
  "schema_version":1,
  "run_id":"<same run>","owner_id":"<same owner>",
  "source_revision":"<40 lowercase hex>","tier":10000,
  "profile_id":"<same admitted profile>",
  "manifest":{"path":"/owned/private/workload-manifest.json","sha256":"<sha256>"},
  "context":{"path":"/owned/private/current-normal-context.json","sha256":"<sha256>"},
  "fault_fixture":{"path":"/owned/results/fault-preparation/fault-fixture-private.json","sha256":"<sha256>"},
  "source":{"item_id":"<actual licensed item>","media_source_id":"<actual source id>","library_id":"<actual library>","root_id":"<actual root>","path":"/owned/media/licensed/actual.mp4","bytes":33554432,"sha256":"<actual full source sha256>"},
  "limits":{"admission_seconds":180,"active_seconds":20,"cleanup_seconds":15,"request_seconds":3,"max_reject_ms":3000,"max_recover_ms":3000,"max_source_bytes":2147483648,"max_output_bytes":16777216}
}
```

The source must be one actual indexed licensed video in the context's frozen
inventory. Its full content hash/byte count, item/root/library binding, production
media-source ID and current source stat identity are verified before pressure.
At least 16 MiB is required, with a fixed upper source budget of at most 2 GiB.
Select a retained licensed source large enough for actual TCP backpressure. The
helper never sparse-extends or modifies it. Size alone does not prove a held
response: all eight production leases must actually remain active. A source
that finishes into socket buffers cannot pass by counting completed requests.
Hashing warms the source cache; no cold-cache claim is made.

The first viewer is the original context's `user_id`/`emby_token`/`device_id`.
The second is `fault_fixture.sentinel`; both are reidentified through actual
`GET /emby/Users/Me` and must be different nonadministrator users. There is no
new login, new user or credential mutation. Export a fresh context after a
service restart; old broker/app/postmaster identities are not overwritten.

## Actual finite exchanges

The initial production `OriginalStreams` snapshot must have no active lease.
Each of eight direct TCP connections requests the complete authenticated
original source with a 32 KiB receive buffer. It consumes only the first 32
bytes, compares them with the verified local source, then stops reading. After
each open, the real runtime endpoint must report exactly one new active lease
for the exact item/source. The fixed active window is at most 20 seconds, below
the product's 30-second idle-write timeout.

Only while all eight sockets remain locally open and all eight leases remain
active does the helper send request nine. The response must have the exact
429/code/retry contract, no media validator/range body, meet the frozen refusal
latency, and create no new lease. The owner-specific error message must also
match, distinguishing this handler from the global stream-slot error. It then opens the independent viewer's stream,
verifies actual source bytes and a ninth active lease, closes it and observes its
production completion. One original owner's TCP connection is closed next; the
helper waits for that exact completed lease before requiring a new same-owner
request to succeed with source bytes within the frozen recovery latency.

Every close shuts down TCP before closing the HTTP response, so Python cannot
silently drain a large body during cleanup. The final production snapshot must
return to the original active set and contain each observed lease's completion.
Local TCP descriptors must all be closed. A lease completion is a production
leave callback, not proof that every process FD has already closed; the external
controller retains responsibility for independent process/resource closure.

## Evidence and failure handling

`overload-result.json` contains safe identities/hashes, source size, fixed
thresholds, timing, exact refusal/recovery facts and closure status. Its
`accepted` flag is only this quota result. `capacity_accepted`,
`decoding_accepted` and `throughput_accepted` are always false. The binding digest
in this result hashes its documented canonical JSON representation; the exact
original binding bytes are retained privately with their artifact hash.

All HTTP bodies/headers, exact lease IDs, credential-bearing paths, source facts
and the original private binding remain in `private/`. The helper returns
nonzero for transport errors, missing/changed leases, unexpected foreign work,
source changes, runtime restart, quota mismatch, oversized evidence or incomplete
cleanup. It preserves the original failure even if cleanup succeeds. It does
not reinterpret a failed overload measurement as an accepted smaller profile.

The small test source covers closed input parsing and lease lifecycle accounting
only. It is not a fake server or a substitute for actual HTTP acceptance. All
execution, including those tests, belongs to consolidated remote verification.
