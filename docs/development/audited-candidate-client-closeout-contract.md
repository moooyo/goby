# Core client closeout input

`close-audited-candidate-client.mjs` closes one `movie`, `episode`, `mp3`, `flac`,
`subtitles`, or `tv-browse` run using retained evidence only. It performs no SQL,
HTTP, browser, process, or service operation. The normal version-1 input uses
each scenario's separately seeded ordinary actor once, with no preexisting
playback sessions or client references. Other actors' completed history remains
protected. The narrowly bound version-2 movie exception follows the
[retained movie04 baseline](audited-movie-retained-baseline.md); its consumed
movie05 execution does not authorize replacing that baseline with a new hash.

## Input

Use `--input <path> --input-sha256 <sha256>` under authorized root SSH. The exact
input fields are `kind`, `version`, `manifest`, `observation`, `summary`,
`gatewayAttestation`, `gatewayIndex`, `runtimeEpoch`, `admission`, `seedBinding`,
`sourceBefore`, `sourceAfter`, `boundary`, `sources`, and `output`.
`kind` is `audited-candidate-client-closeout-input`; the normal `version` is
integer `1`. Version `2` additionally requires the exact `retainedBaseline`
descriptor and its fixed movie-only authority from the linked contract.
Every field from `manifest` through `boundary` is exactly a descriptor
`{"path":"/absolute/path","sha256":"64 lowercase hex digits"}`.
`output` is a fresh, empty, root-owned 0700 directory beneath `/opt/goby-test/`.

| Descriptor | Actual retained artifact |
| --- | --- |
| `manifest` | The input consumed by the candidate browser adapter |
| `observation`, `summary` | Its private `observation.json` and `summary.json` |
| `gatewayAttestation`, `gatewayIndex` | The ready attestation and closed index of this run's gateway |
| `runtimeEpoch`, `seedBinding` | Current runtime epoch and seed runtime binding, with original seed provenance intact |
| `admission` | Successful version-2 live admission for that exact epoch/binding |
| `sourceBefore`, `sourceAfter` | Fresh full `admission.snapshot_sql` results: exactly `capturedAt`, all 35 `tables`, and `sequences` |
| `boundary` | Actual runtime/database sampling and worker closure facts described below |

`sources` has exactly `closer`, `adapter`, `gateway`, `proxy`, `sessionProof`,
`movie`, `audio`, `subtitles`, and `tv`, each a source descriptor. The MJS files
must be the matching local siblings imported by the closer. The two Python
descriptors must match the browser gateway's own source pins. The runtime epoch
continues to cite its independently frozen transition gateway; do not substitute
the later browser gateway into a historical transition input.

Exactly two runtime forms are supported: the reviewed version-1 binary
transition and its single version-2 `environment_revision` successor. The
successor retains the product/source and PostgreSQL identity, links its previous
epoch/binding and failed admission03 closure, and only adds the two reviewed
backup-limit environment keys. Its seed binding retains the original business
mapping and six closed historical sessions. `admission` must still be a **new
successful admission of the current epoch/binding**; failed admission03 is only
provenance and cannot admit the successor. The closer resolves these already
pinned historical files without rerunning their operations.

## Boundary version 1

The boundary is `kind: audited-candidate-client-boundary`, `version: 1`.
The following mapping specifies the fields without supplying invented runtime
facts. The caller records real before/after samples and completed worker state.

```javascript
{
  kind: "audited-candidate-client-boundary", version: 1,
  runId: manifest.runId,
  runtimeEpoch: input.runtimeEpoch,
  sourceBefore: input.sourceBefore, sourceAfter: input.sourceAfter,
  candidateBefore: manifest.processes.candidate,
  candidateAfter: manifest.processes.candidate,
  postgresBefore: epoch.postgresProcess, postgresAfter: epoch.postgresProcess,
  leaseBefore: epoch.lease, leaseAfter: epoch.lease,
  database: epoch.candidate.database,
  beforeMonotonicNs: "decimal nanoseconds after the before capture",
  afterMonotonicNs: "decimal nanoseconds after all workers and the after capture",
  clientWorker: {
    exitCode: 0, mainPID: 0, workerPidAbsent: true, remainingBrowserPids: []
  },
  gatewayWorker: {
    exitCode: 0, mainPID: 0, workerPidAbsent: true, index: input.gatewayIndex
  }
}
```

The candidate fields each contain the adapter's **11-field object**: `bootId`,
`pid`, `startTicks`, `uid`, `exe`, `exeDevice`, `exeInode`, `cmdline`, `cgroup`,
`networkNamespace`, and `listener`. The nested listener is exactly `host`,
`port`, and `socketInode`. Removing only `listener` yields the epoch's **10-field
`candidateProcess`**. PostgreSQL samples equal the epoch's own process object;
do not add a candidate listener to them. `database` and `lease` retain the epoch
representations without field renaming. Capture the source through the existing
epoch reader between matching process/database/lease checks; a copied old
snapshot is not a fresh before/after observation.

## Evidence interpretation

The closer reconstructs the complete gateway filename/hash/ordinal set and
transfer framing, with bounded gzip/deflate/Brotli decoding for retained JSON.
`index.complete` only establishes paired receipts. Every critical successful
API requires complete request/response and delivery evidence independently.
Fixed playback POST routes may retain `text/plain` JSON without rewriting it.
Login bodies are dispatched by their recorded content type: strict UTF-8
URLencoded forms and the existing JSON representation are both supported.
The opaque signed-int64 `PlaybackStartTimeTicks` field is decoded losslessly only
for actual Playing/Progress/Stopped requests and their body-bound observations.
All other generic JSON and business integer checks retain their original limits.
The 401 verifier's `text/plain` response is intentionally not retained; complete
status/header/write-count evidence, the same token, logical logout proof and the
revoked database row establish rejection, not exact response-body reconstruction.

Each movie login needs its own physical PlaybackInfo/Started/Progress/Stopped
chain and actual GET media bytes. HEAD cannot establish delivery. Physical
ordinals are never deduplicated; frame and Service Worker observations can both
describe one physical request. An interrupted media response remains partial.
It needs an unambiguous context association, recorded `net::ERR_ABORTED` and
failure time, plus a nearby owned Stop or changed Range during an actually
recorded UI seek phase. Unexplained
partial requests fail closure. The adapter now records its monotonic start and
the actual failure time/reason/UI phase privately. Gateway interruption records
can conservatively mark the request incomplete even when its entire GET header
was written; the closer derives header delivery from byte counts without
changing any recorded response completeness flag.

Exact Range association is preferred. A single physical byte range can also be
associated with a containing logical range only with the same full URL/token,
allowed GET 200/206 evidence, one candidate per frame/Service Worker scope and
the bounded terminal-time checks. A logical event can describe multiple physical
exchanges; each physical ordinal and its original completion flags remain in
the result. This establishes an explicit association, not a claim about internal
browser caching. Empty completed media responses remain rejected by the full
analysis path until an independently bound cancellation receipt is admitted.

BrowserContext media observations are preliminary candidates, not byte-delivery
proof. Page errors retain bounded, redacted diagnostics for review. New metadata
cannot retroactively explain an old timestamp-only error or make it harmless;
the full UI gate still requires no unexplained page errors and currently retains
its zero-page-error predicate.

Playback reports bind token, authentication session, user, device, item, media
source, canonical play and any retained client-reference mapping. Every observed
Started chain must be durably Stopped and each login revoked. Unplayed,
uncounted Prepared/Expired rows are listed only when tied to actual preparation;
they are not claimed to be played or removed. Counts derive from proven counted
plays, not HTTP event totals. Source userdata uses the selected product's exact
stop normalization; final ordering uses durable timestamps rather than gateway
intent order. Q, unrelated items, preexisting rows and unrelated tables/sequences
must remain unchanged. Owned encoding jobs may only remain completed/cancelled.

Outputs are a metadata-only `closeout.json` and `summary.json`. A failed check
produces `status: incomplete`; it cannot trigger another browser run or cleanup
operation. This closes only the named core scenario, not NextUp, automatic
refresh, general codec/audible-output support, or the release matrix.
