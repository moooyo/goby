# Phase 3 compound workload actor

Status: **source only; no profile admitted or executed**. The checked-in profile
values are proposals to freeze after the isolated guest's baseline is reviewed.
They are not capacity claims. Do not execute this source on a developer machine
or on the shared `test-env` host. Phase 3 must finish implementation before its
consolidated remote verification begins.

The three-client authentication correction requires a fresh owned namespace
and newly frozen source and input pins. It does not retroactively accept a
failed historical run or allow rewriting its private context or evidence.

## Boundaries

`media-analysis-phase3-workload.py` is an unprivileged Linux guest-local actor.
The external controller owns VM 106, the native Goby and PostgreSQL processes,
credentials, source admission, prepared catalog, media roots, deployment files,
external fault injection, recovery acknowledgements and final physical closure.
The actor never launches Goby, seeds database rows, reboots a guest, mounts a
filesystem, or substitutes HTTP responses. Its SQL is limited to observations
and a bounded transaction holding the declared sorting witness's real item row
lock. It does not block the global journal used by unrelated scan publications.

Run **one fresh process and owned output directory per catalog tier/profile**.
Prepare real media on owned roots and an already indexed seed large enough to
serve a deep page at `StartIndex >= tier / 2`. Its queried population must contain
at least `tier / 2` entries plus the complete expected nonempty page before the
first workload request. The driver's initial catalog count range is
`tier / 2 + 1` through `tier + 999`; each settled phase must contain the selected
10,000 or 100,000 tier, with up to 999 explicitly accounted supporting items.
Freeze all four exact counts in `catalog_expected`. Report file-backed and
non-file-backed rows separately, including any supporting seeded fixtures.

`cold` means the production scan is requested with `ForceProbe: true` over that
prepared, partly indexed corpus, including real new files whose expected
`Added` count is positive. It does not mean an empty database or a cold operating
system page cache. Inventory hashing and probing already read source bytes, and
the actor never drops caches. `cached` and `incremental` use `ForceProbe: false`;
the cached scan must have zero `Added` and `Updated` counts. Keep preindexed
analysis/playback sources stable and outside the changing scan population.
Prepare query expectations for each phase from a stable subset unaffected by
concurrent scanning, metadata edits and playback user-state updates; Resume and
Latest must be nonempty.

The actor issues cold, cached and incremental production scans. During each
window it concurrently issues Unicode/filter/count/shallow/deep/Resume/Latest
queries, consumes direct original bytes and real progressive remux/transcode
outputs with seeks, starts intro and preview jobs, and exercises sorting rebuild
and metadata CAS writes. A real sorting-witness item row lock must make a sorting request
fail with the product's protected statement timeout. The same owner PID and
backend start, unchanged settings and generated sort digest must survive, and
the same settings CAS must then succeed concurrently with metadata editing.
The rollback digest covers the preexisting item scope captured before the
request, so legitimate new scan rows cannot be mistaken for partial commits.
The successful sorting and metadata requests must both overlap a running scan.

Prepared media and scheduling must make this a meaningful compound workload.
A small corpus whose cached scan finishes before another lane runs is a failed
overlap measurement; the actor does not turn that into a passing capacity claim.
Do not add sleeps to production handlers or mock task state to manufacture an
active window. A single running sample is insufficient. Two consecutive
observations of the same running job, separated by no more than four sampling
periods, bound an observed active interval. Media process evidence requires the
same PID and process start identity, advancing CPU ticks between adjacent
observations within that gap limit, and ancestry rooted in the owned Goby app.
A process merely existing, a queued child or a nonterminal parent run is
insufficient evidence of productive analysis work.

Intro and preview share one production analysis execution slot. Acceptance
requires their parent runs to have overlapping nonterminal admission intervals,
then checks each feature separately: a running child and its feature's real,
CPU-advancing app descendant must share an interval with a running scan,
successful query traffic and consumed playback bytes. Each feature's measured
intersection must meet `min_all_lane_overlap_ms`. This proves both analyses
participate in the compound workload while respecting shared capacity one; it
does not claim simultaneous intro and preview decoding or five physically
concurrent workers. Each query kind and each playback mode's start and seek must
also overlap scanning. Pairwise intersections alone cannot satisfy either
feature's compound intersection. A scan spool generation must be observed.

## Frozen public manifest

Select one object from `media-analysis-phase3-workload-profiles.example.json`.
Set the actual immutable source revision, run/owner identities, guest machine
identity, exact PostgreSQL version and scan deployment configuration hash.
Freeze the measured resource envelope and thresholds before acceptance. Change
`admission` from `candidate` to `approved` only when the external controller has
admitted that exact profile. A digest change requires a new run identity. Store
the runtime copy as a canonical absolute, owner-only `0600` file; its contents
are safe for public evidence even though the actor requires private file mode.

The manifest contains finite request/event/artifact/process/time budgets,
declared query and playback concurrency, latency thresholds, resource ceilings,
minimum samples and required overlap. These are acceptance thresholds, separate
from emergency execution deadlines. All three playback modes are mandatory.
`playback_workers` is exactly three, with one independent item binding per mode;
`query_workers` is independently bounded from one through sixteen.
No partial tier can be relabeled as an accepted lower tier in the same run.

`scan_evidence.configuration_sha256` binds the actual deployment JSON passed to
`GOBY_SCAN_EVIDENCE_FILE`, not a self-reported HTTP feature flag. There currently
is no production HTTP endpoint for scan evidence. The controller must set
`enabled: true`, provide an owned spool directory, and preserve that directory
for independent observation. Use the production lower-camel configuration:

```json
{"enabled":true,"directory":"/owned/private/scan-evidence","maxBytes":1073741824,"maxDirectories":131072,"maxEntries":1048576,"maxFallbackHandles":4096}
```

## Private preparation context

The context is a fresh canonical absolute `0600` JSON file, owner UID, one link,
at most 16 MiB. Keep it and every raw receipt out of published evidence. It has
exactly these fields (identifiers/paths below are structural examples):

Private Actor input is `CONTEXT_VERSION=2`; public manifests, checkpoints and
results remain `VERSION=1`. Each playback row requires `client` with exactly
`emby_token`, `auth_session_id` and `device_id`. Its authentication session ID is
the native login's `SessionInfo.Id`, distinct from a later play-session ID.

```json
{
  "schema_version":2,
  "manifest_sha256":"<hash of exact admitted manifest bytes>",
  "driver_sha256":"<hash of exact driver bytes>",
  "run_id":"<same as manifest>","owner_id":"<same as manifest>",
  "origin":"http://127.0.0.1:18099",
  "admin_cookie":"<native session cookie>","csrf_token":"<native CSRF>",
  "emby_token":"<primary viewer token>","user_id":"<same viewer id for all three clients>","device_id":"phase3-workload",
  "owner_file":"/owned/private/owner.json",
  "app_pid":1234,"app_start_ticks":123456,
  "cgroup_path":"/sys/fs/cgroup/owned-phase3-profile",
  "postgres":{"cgroup_path":"/sys/fs/cgroup/owned-phase3-postgres","postmaster_pid":2345,"postmaster_start_ticks":234567,"postmaster_started_at":"2026-09-22T00:00:00.000000Z"},
  "process_observer":{"argv":["/usr/bin/sudo","-n","/usr/bin/python3.13","-I","-B","/opt/goby-phase3-runtime-setup-20260922-01/process-observer.py","--binding","/var/lib/goby-phase3/tier10k/control/process-observer.json","--binding-sha256","<binding SHA256>"],"binding_sha256":"<binding SHA256>","source_sha256":"<root observer source SHA256>"},
  "pg_env":{"PGHOST":"/owned/postgres/socket","PGPORT":"5432","PGDATABASE":"owned_phase3","PGUSER":"owned_observer","PGPASSWORD":"<private>"},
  "tools":{"psql":"/usr/bin/psql","ffmpeg":"/usr/bin/ffmpeg","ffprobe":"/usr/bin/ffprobe"},
  "roots":[{"id":"root-a","library_id":"<same-library>","path":"/owned/media/a"},{"id":"root-b","library_id":"<same-library>","path":"/owned/media/b"}],
  "inventory_path":"/owned/private/inventory.jsonl","inventory_sha256":"<hash>",
  "scan_libraries":[{"id":"<prepared scan library>","expected":{"cold":{"Scanned":9999,"Added":4000,"Updated":5999},"cached":{"Scanned":9999,"Added":0,"Updated":0},"incremental":{"Scanned":9999,"Added":1,"Updated":1}}}],
  "catalog_expected":{"initial":6000,"cold":10000,"cached":10000,"incremental":10000},
  "queries":{
    "cold":[{"id":"unicode","kind":"unicode","params":{"ParentId":"<stable library>","Recursive":"true","SearchTerm":"\u7535\u5f71","Limit":"20","SortBy":"SortName","SortOrder":"Ascending"},"total":20,"ids":["<frozen ordered cold ids>"]}],
    "cached":[{"id":"unicode","kind":"unicode","params":{"ParentId":"<stable library>","Recursive":"true","SearchTerm":"\u7535\u5f71","Limit":"20","SortBy":"SortName","SortOrder":"Ascending"},"total":20,"ids":["<frozen ordered cached ids>"]}],
    "incremental":[{"id":"unicode","kind":"unicode","params":{"ParentId":"<stable library>","Recursive":"true","SearchTerm":"\u7535\u5f71","Limit":"20","SortBy":"SortName","SortOrder":"Ascending"},"total":20,"ids":["<frozen ordered incremental ids>"]}]
  },
  "playback":[
    {"mode":"direct","client":{"emby_token":"<primary viewer token>","auth_session_id":"<primary native SessionInfo.Id>","device_id":"phase3-workload"},"item_id":"<direct item>","path":"/owned/media/a/direct.mp4","body":{"IsPlayback":true,"DeviceProfile":{"DirectPlayProfiles":[{"Type":"Video","Container":"mp4","VideoCodec":"h264","AudioCodec":"aac"}]}},"seek_ticks":300000000,"expected_codecs":["h264","aac"]},
    {"mode":"remux","client":{"emby_token":"<distinct remux viewer token>","auth_session_id":"<distinct remux SessionInfo.Id>","device_id":"phase3-workload-remux"},"item_id":"<distinct remux item>","path":"/owned/media/a/remux.mkv","body":{"IsPlayback":true,"EnableDirectPlay":false,"EnableTranscoding":true,"DeviceProfile":{}},"seek_ticks":300000000,"expected_codecs":["h264","aac"]},
    {"mode":"transcode","client":{"emby_token":"<distinct transcode viewer token>","auth_session_id":"<distinct transcode SessionInfo.Id>","device_id":"phase3-workload-transcode"},"item_id":"<distinct transcode item>","path":"/owned/media/a/transcode.mkv","body":{"IsPlayback":true,"EnableDirectPlay":false,"EnableTranscoding":true,"DeviceProfile":{}},"seek_ticks":300000000,"expected_codecs":["h264","aac"]}
  ],
  "analysis_item_ids":["<first episode>","<second same-cohort episode>"],
  "mutation":{"delete_path":"/owned/media/a/delete.mp4","quarantine_path":"/owned/private/staging/deleted.mp4","move_from":"/owned/media/a/move.mp4","move_to":"/owned/media/b/move.mp4","add_from":"/owned/private/staging/add.mp4","add_to":"/owned/media/a/add.mp4","metadata_item_id":"<stable automatic-sort witness item>","sort_words":["The"]},
  "scan_evidence_path":"/owned/private/scan-evidence.json","scan_evidence_sha256":"<actual deployment file hash>"
}
```

The illustrative context above intentionally abbreviates query lists and device
profiles and cannot be used as an admitted fixture. Replace every placeholder
and every example count with independently prepared truth. `catalog_expected`
contains exactly `initial`, `cold`, `cached` and `incremental`; these are exact
counts of all `items` rows, checked before work and after each settled phase.
They are separate from each library's exact `Scanned`, `Added` and `Updated`
scan-job counters. Supporting folders and other non-file items must be included.

`queries` is an object with exactly the three phase keys, each holding 7 through
32 query objects. Each phase must include every kind `unicode`, `filter`,
`exact_total`, `shallow`, `deep`, `resume`, `latest`, with unique query IDs within
that phase. The actor chooses `queries[phase]`, compares each exact total and
ordered ID list, and evaluates query latency per phase. Kinds are explicit;
labels do not select routes. Deep paging must start at or beyond half the tier
and return a nonempty page from the initial indexed population. Shallow, Resume
and Latest must also return nonempty results. Expected IDs and totals come from
independent prepared-fixture truth, never copied from the response under test.
Latest returns an array, so its expected `total` is the exact returned array
length; the other routes must return their exact `TotalRecordCount`. Keep the
selected rows and ordering stable within each phase, including across the
sorting-policy toggle, or use a separate stable query scope.

`mutation.metadata_item_id` also supplies the snapshot's `sort_witness`; there
is no separate context key for that witness. Choose a stable item whose
automatically generated `SortName` changes when `mutation.sort_words` is applied
and changes back when that word list is cleared. For example, a title beginning
with `The` can exercise `sort_words: ["The"]`. A fixed manual or locked
`SortName`, or an explicit source sort title that bypasses automatic generation,
cannot demonstrate this rebuild. The actor alternates the supplied word list
and an empty list across phases, changes only `Overview` in the metadata edit,
and requires the witness's stored sort key to change after every successful CAS.

Supply exactly three playback bindings, one each for `direct`, `remux` and
`transcode`, with three distinct `item_id` values and three native authenticated
clients for the same viewer `user_id`. The preparer creates the primary
query/direct login plus separate remux and transcode logins before capturing
the fixture baseline. Their native `SessionInfo.Id`, device IDs and tokens must
all be distinct. The direct client's token/device pair equals the top-level
default used by queries and overload. Each mode retains its fixed client for
all three phases; neither rotating tokens nor reusing an item establishes
independent concurrency. Native `Users/Me` and read-only session, play-session
and encoding-job SQL observations bind the real user/session/device chain.

`PlaybackInfo`, `Sessions/Playing` (Started), initial and seek media reads,
Progress, item/UserData reads, Stopped, `ActiveEncodings` requests and cleanup
all explicitly use that mode's same client. Request-specific credentials pass
through the existing `http(headers=...)` interface; parallel lanes must not
mutate shared `self.c` authentication fields. Retain the admitted q2/p3 workload,
all SLOs, and encoding caps of global two, per-user two and per-session one.
The broker's UID, app identity and cgroup remain unchanged.

`remux` and `transcode` must negotiate
progressive MP4 output (not HLS); set `EnableDirectPlay:false`,
`EnableTranscoding:true` and the real suitable device profile. Select codecs
that actually exercise stream copy versus encoding; the retained database plan
and child process are checked separately from output decoding. Conversion
responses must fit the complete-stream byte budget; an oversized response is a
failure, never silently truncated. A video seek compares a decoded output frame
with the actual source at the requested time. Direct seek checks exact range
bytes and does not claim browser presentation-time behavior. Audio-only media
is accounted in the corpus, while this workload's required remux/transcode
consumer profiles must contain video so frame correspondence can be checked.
Playback progress must be visible through the real item/UserData projection.

Each phase also submits forced intro and preview runs for `analysis_item_ids`.
Supply 2 through 200 IDs, keeping every preview child within the complete
200-child observation page. Prepare a real supported episode cohort with enough
independent indexed support episodes to reach media extraction rather than
immediately abstaining; a completed parent alone cannot satisfy overlap.
All children must be observed and complete without error. Every selected item must
have an intro detection status of `qualified`, `review` or `no_result`, and a
ready 240-pixel preview with frames. The actor fetches the first selected item's
BIF, checks its complete index and sentinel, and decodes its first, middle and
last JPEG entries. That finite BIF sample is distinct from checking every
selected item's ready preview status.

Every media path on every admitted root must have one inventory JSONL row:

```json
{"root_id":"root-a","relative_path":"Movie.mp4","sha256":"<actual bytes hash>","bytes":12345,"origin":"generated","original_id":"fixture-template-01"}
```

`origin` is exactly `generated` or `licensed`. Copies/hardlinks share the
original identity; do not invent 100,000 licensed original IDs. The actor counts
pathnames, distinct `(device,inode)` identities, content hashes, allocated and
apparent bytes, generated/licensed path counts and probed distinct-content
formats separately. It hashes each inode and probes each distinct hash.
Hardlinks are permitted but never reported as distinct originals. Mutation
targets require a single link so deletion/move identity is unambiguous.

`owner_file` is controller-written private JSON with matching `owner_id`,
`run_id`, `source_revision`, `guest`, `app_pid`, `app_start_ticks`, `roots`, and
`cgroup_path` and `postgres`. It is reread before every HTTP/SQL/child/mutation operation. The
app PID's `/proc` start time and membership in the dedicated cgroup are checked.
Goby and PostgreSQL use distinct, nonnested cgroups.
Enable `IOAccounting=yes` in both owned service definitions before startup and
verify that each live cgroup exposes a readable `io.stat`. Controller support
in the cgroup root alone is insufficient. Missing I/O accounting is a deployment
failure, not zero I/O; preserve the broker's original response before asserting
that its sample succeeded.

The `postgres` object binds
the actual postmaster PID/start ticks and `pg_postmaster_start_time()`. The real
advisory-lock owner backend must be in that PG cgroup and have a revalidated
parent chain to the pinned postmaster. Its backend start identity must survive
the sorting timeout. Samples report each service and their sum. The actor stays
outside both measured subtrees. Its FFmpeg/FFprobe commands never supply production
process evidence or inflate product resource measurements. Media evidence is
bound to descendants of the identified Goby app, including the app's process
start identity; an executable name or shared cgroup membership alone does not
establish that relationship. The controller accounts for observer costs
separately. Use dedicated, owned PG credentials with observation and the
sorting-witness-row-lock privilege; never use a shared DB. The observer connection must
use the same database and current schema as the app's catalog owner lock.

Goby and the actor have different UIDs. `process_observer` is a source-pinned,
root-owned read-only broker with an exact sudo argv authorized for one fixed
scope and binding hash. Its only stdin operation is
`{"schema_version":1,"operation":"sample"}`. The actor checks returned
run/owner/app/binding identities, monotonic time, CPU ticks, app ancestry and
source descriptor links. The broker validates the actual scan-configuration
path/hash/environment and counts spool generations. The actor does not read
Goby-private configuration or `/proc` descriptors. Raw bounded broker receipts
remain private. A broker `complete` flag alone cannot establish workload success.
The root supervisor owns its worker group and enforces a 12-second wall deadline
plus 3-second kill/reap grace. On actor-side failure, the actor drains bounded
output and waits up to 16 seconds for that supervisor; it never claims its
unprivileged `killpg` stopped a root worker. A nonzero or unresolved supervisor
requires external closure review and prevents a successful workload result.

The optional `Actor(..., request_scope="profile")` parameter is for the existing
fault wrapper. Each immutable case binding SHA is a distinct scope. Analysis
request IDs hash run/scope/phase/kind, so a later case cannot reuse an earlier
completed run while retries within one case remain idempotent. Case-specific
broker bindings use the closed `control/cases/<safe_case_id>/process-observer.json`
path form; old PID bindings are retained rather than overwritten.

Incremental deletion moves one source to quarantine outside all roots, adds one
prepared source, and moves another between two roots of the same library while
preserving its inode and basename. Renaming the basename would exercise a
different identity contract and is rejected. The move item must have a seeded
user-state witness. The actor verifies catalog removal, new item insertion,
stable move ID, changed root ID, stable file identity and identical user data.
Cleanup can reverse only the exact recorded owned filesystem moves after all
potentially conflicting producers have drained. The controller owns post-run
catalog disposal or restoration scans and verifies physical closure separately.

Before scan, analysis and playback admission requests, the actor retains a
private admission intent. It resolves that intent only after obtaining the
durable job or playback identity. A timeout or lost response can leave real
server work admitted without a known identity. Such pending intents require
controller reconciliation using the retained private scope; they cannot be
treated as evidence that no work started. Unresolved intents, active jobs,
remaining playback sessions or undrained encoders block conflicting filesystem
restoration and prevent a successful closure claim. The actor retains
`unresolved-admissions.json` for this handoff when necessary.

Cancellation and playback-stop HTTP responses, including `204`, acknowledge
requests rather than prove producer exit. Cleanup must observe terminal owned
jobs, drain queued/running encoding records, and establish that the matching
live media producers have exited before restoring files they could still read.
Keep the original failure and any cleanup failure visible; the external
controller remains responsible for independent physical closure.

Playback cleanup preserves authentication sessions for later admitted
after-compound and fault consumers. Recovery derives contexts by deep copy and
must transport all three `client` objects unchanged. The fault sentinel's
probe/state identity remains separate. Keep the three viewer sessions until
the final consumer and independent cleanup have closed; only then may the
controller perform their native Logout operations. Tokens and raw credentials
remain in private `0600` artifacts and never enter public evidence.

## Execution and evidence

After Phase 3 source closure and explicit remote profile admission, the external
controller invokes the fixed argv below inside the isolated guest:

```text
/usr/bin/python3 -I -B media-analysis-phase3-workload.py --manifest /owned/private/profile.json --context /owned/private/context.json --output /owned/results/new-run
```

No shell fragments, user-supplied executable snippets or arbitrary SQL slots
are accepted. Exit 0 means the workload's selected profile passed its own
checks; it does not mean recovery/fault acceptance passed or resources closed.
Exit 1 preserves failed evidence. Admission errors print only a bounded code.

`manifest.json`, `events.jsonl`, `checkpoint.json` and `result.json` contain safe
labels, source/manifest identity, timings, counters, distributions and bounded
failure codes. All files are initially private; publication is an explicit
separate step. `private/` contains raw HTTP body/headers, paths, SQL statements
and results, process command lines and real task IDs, and must never be copied
into a PR or issue. Auth tokens in generated media URLs can appear there.

Within one fresh evidence directory, identical captured HTTP body bytes may reuse
the same private `{name,sha256,bytes}` reference. Every request still executes,
retains its own HTTP receipt and timed event, and performs all response, media
decoding and correctness checks. Failed requests retain their entire captured
partial body and independent failure receipt. Reuse never samples, truncates,
compresses or discards captured bytes. The index holds at most 4096 body
identities and no payload bytes; once full, previously unseen bodies follow the original charged,
exclusive-file write path. It is not shared across runs.

Before reuse, the actor checks the original private-directory identity and the
same regular file's identity, ownership, `0600` mode, single link and complete
bytes through an `O_NOFOLLOW` descriptor, including empty response bodies. A
missing, replaced, linked, changed or inaccessible file fails closed. References
are shared directly; no hard links or symbolic links are created. Each stored
body is charged once, while every receipt, event and checkpoint write remains
charged as before. The artifact ceiling and cleanup reserve remain unchanged;
cleanup may reuse the same verified files. This storage optimization does not
guarantee that a workload fits its finite evidence budget.

The atomically replaced checkpoint contract is:

```text
schema_version, run_id, owner_id, profile_id, source_revision, tier,
phase, at_monotonic_ns,
active_lanes: {name: {begun, completed, last_progress_monotonic_ns}},
active_jobs: [{reference: SHA256(real ID), kind, active}]
```

The checkpoint is progress evidence, not a durability receipt. The external
fault controller must obtain acknowledged state through the independent state
observer, fsync that receipt outside the guest, and only then inject a fault.
It must also observe actual overlap from private task/process evidence. A lane
flag cannot establish that a media worker was running.

The result reports latency p50/p95/p99/max and sample counts per operation,
including `phase_metrics`, failure counts, playback/seek first-byte
distributions, resource peaks, admitted concurrency only on success, and
retained limits. The declared playback concurrency is exactly three independent
modes; it does not change the shared analysis execution capacity. HTTP
first-byte measurements are conservative first-body-chunk completion times,
not browser first frame. The `scan_seconds` threshold covers the whole phase's
compound work and lane completion, rather than only one scan job's timestamps.
`test-media-analysis-phase3-workload.py` is pure parser/accounting test source;
run it only during consolidated remote verification. It never substitutes for
the actual native compound workload.

Pool measurements come only from the production native
`GET /admin/v1/runtime/resources` `DatabasePool` snapshot, sampled at most once
per second plus explicit idle/phase boundaries. Connection gauges are JSON
integers; acquisition/wait/cancellation counters and nanoseconds are canonical
decimal strings and remain exact above JavaScript's safe-integer range. The
result retains per-phase cumulative before/after/delta values and gauge peaks.
Missing samples, malformed fields, capacity changes or counter resets cannot
be reported as zero waiting. Empty-acquire wait includes connection construction;
the endpoint's native authentication adds real pool work and is included in the
reported counters. These observations are distinct from PostgreSQL lock waits
and are not inferred from `pg_stat_activity`. Actual SQL plans are captured by
the externally frozen PostgreSQL `auto_explain` profile, not an Actor-supplied
`EXPLAIN` query or a hand-built statement substitute.
