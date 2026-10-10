# Backend design review after the continuation, 2026-10-10

## Scope and method

This review covers permission complexity, excessive defensive work, backend
performance architecture, and the Linux-only server boundary. The target is
HEAD `737c382d` plus the existing dirty working tree. Pre-existing scan, media,
playback, release and research changes are preserved. This report does not
implement the recommendations or change production code.

Four parallel tracks inspect authority, defensive resource ownership,
performance, and platform boundaries. Findings are checked against current
callers and deduplicated against the R/N/L/Q/S/F/C/T/U/V reviews and their
implementation records. A new caller of a previously reported issue is not
counted as a new root cause.

Evidence is static source and call-path inspection. Operation counts below are
structural counts, not measured latency or throughput. No local tests, builds,
validation suites or runtime probes were run. The remote capacity policy was
read before a read-only `test-env` inspection. The remote was reachable, with
approximately 26 GiB free on its persistent root. Its default executable was
`/usr/bin/go`, reporting Go 1.26.7, rather than the repository's 1.27.1 target.
No tests or builds were run for this static review. No verification environment
or compiler scratch was created, and the shared build/module caches were not
modified. The installed toolchain observation is not a verification blocker
claim or a tested-source receipt.

## Findings

| ID | Priority | Area | Recommendation |
| --- | --- | --- | --- |
| W01 | P2 | Defensive process ownership | Give initial process-identity capture failure a safe recovery path. |
| W02 | P2 | Catalog write amplification | Update only collection entries whose positions change. |
| W06 | P2 | Storage observation | Bound the actual filesystem work behind the storage-roots endpoint. |
| W05 | P3 | Permission model | Make effective-permission summaries consistent and expose the blocking rule. |
| W03 | P3 | Permission parsing | Construct the fixed policy-field schema once. |
| W04 | P3 | Linux diagnostic cleanup | Remove the obsolete platform explanation from the transcode sentinel. |
| W07 | P3, conditional | Provider image delivery | Measure and separate rendered-preview transfer from image processing admission. |

W01 is a liveness defect under an exceptional resource failure. W02 is the
strongest ordinary-workload performance opportunity in this pass. W03 is a
small allocation/CPU simplification. W04 is a diagnostic correction, not a
remaining Windows implementation.

W05 is a permission-model simplification with a concrete misleading summary;
W06 is a resource-admission gap on a management route. Neither requires a new
generic authorization framework or a separate storage-monitoring service.
W07 concerns an implemented optional TMDB path; it does not expand the accepted
online-provider delivery scope or establish a measured production bottleneck.

### W01: Recover from the initial double process-capture failure

`internal/media/process_retirement_owner_linux.go:66-74` opens a pidfd and
reads `/proc/<pid>/stat` after `command.Start()` succeeds. If both operations
fail, the owner can retain `pin == -1` and `identity.start == 0`. Concurrent
file-descriptor exhaustion is one possible trigger.

`retryConventionalProcessCapture` at lines 120-123 immediately rejects that
state. It performs no operation that could change either field.
`internal/media/process.go:322-324` nevertheless retries it once per second
without an exit. Restoring descriptor availability therefore cannot repair
this owner. `ensureConventionalOwner` is guarded by `sync.Once`
(`process_retirement_owner.go:31-49`), so another call does not redo the initial
capture. The retirement callback uses WNOWAIT and signals the group; it neither
reaps the leader nor repairs the owner's identity.

The consequence is a permanently retained process permit, unreaped direct
child and blocked cleanup caller. The synchronous `process.Close()` in
`process.go:94-100` means the ordinary probe operation does not return after
its configured timeout in this state. This is different from N14's legitimate
indefinite wait for actual retirement: the retry cannot make progress even
after the original environmental failure has disappeared.

The path is used by production probes. The scan pipeline reaches
`primary_scan_read.go` and `Prober.ProbeFileOwned`, which reaches `ProbeFile`
and `runLimitedFilesOutput`. Without a command-domain scope,
`process.go:180-184` selects the conventional implementation. The current
production entry points do not install such a scope for these probes.

Acquire a usable identity handle as part of process creation, or design a
recovery path grounded in exclusive ownership of the still-unreaped child.
Do not recapture an arbitrary numeric PID, kill a possibly reused process
group, time out into claimed success, or release capacity before actual
retirement. The concrete Go/Linux mechanism needs separate implementation
review and remote fault injection; merely deleting the zero-identity guard is
unsafe.

Verification should force both initial captures to fail after successful
Start, restore capacity, and prove eventual group retirement, exec Wait/pipe
join, registry removal and exactly-once permit return. Also cover the two
single-capture failures, cancellation, a leader that already exited, and
descendant readers. The unreachable-progress state is established statically;
no fault-injection result is claimed here.

### W02: Exclude unchanged collection positions from UPDATE

`normalizeCollectionPositions` at `internal/library/collections.go:771-773`
updates every entry after computing its ordinal, even when the stored position
already equals that ordinal. `appendCollectionMembers`
(`collections_members.go:135-136`) always calls it before appending.

`MoveCollectionEntry` first normalizes at `collections.go:795`, then executes
another UPDATE at line 832 whose WHERE selects the entire collection. The
CASE expression's `ELSE position` leaves a value unchanged but does not
exclude its row from UPDATE.

The collection limit is 10,000. Moving an entry by one physical position in an
already contiguous, fully visible full playlist executes two UPDATEs, each
matching 10,000 rows, although only two positions need to change. Moving an entry to its
current position still performs both full updates. This creates avoidable
database tuple/WAL work under catalog write ownership; it is not a measured
20,000-row latency result.

Add `e.position IS DISTINCT FROM ordered.ordinal` to normalization. Restrict
the move UPDATE to the moved entry and the actual shifted interval, and omit
that position UPDATE when the source and destination coincide. Keep
`touchCollection`, catalog Resync and the final authority/commit checks.

Do not remove normalization itself: catalog item deletion can leave position
gaps. Retain full-collection ordinal calculation, the existing position/ID
ordering, hidden entries' relative order, duplicate playlist media entries,
and the deferred unique `(collection_id, position)` constraint. The schema
has no entry UPDATE trigger whose effect requires same-value updates.

Remote regressions should cover contiguous and gapped positions, adjacent
and first/last moves, same-position requests, ACL-hidden entries, duplicate
media IDs and repeated BoxSet additions. Measure changed-row count, WAL and
owner hold time separately from the remaining ordinal scan and authorization.

### W03: Precompute the fixed policy-field lookup

`internal/identity/policy.go:117-122` reflects over `ManagedPolicy` and builds
the same lowercase-to-canonical field-name map for every policy parse.
Authentication and current-session revalidation use this parser, as do policy
projections. The schema depends on the Go type, not the current user or JSON.

Generate this private lookup once during package initialization using the
same type, then only read it. Continue decoding and validating each raw policy
document independently. Preserve duplicate/case-aliased key rejection, size
and structure bounds, exact types, unknown-field behavior, server-managed
login fields and the runtime playback-boolean fallback.

This caches only immutable schema metadata. It must not cache user policy,
role, expiry, revocation, device permission or an authorization decision.
Keep request-owned defaults and result slices independent. R01 concerned
repeated parsing of one policy observation; this finding concerns repeated
construction of a fixed schema inside each remaining parse. The benefit is
small and should be measured as parser allocations/CPU, not advertised as an
end-to-end authentication speedup.

### W04: Correct the remaining platform-specific error explanation

`internal/transcode/command.go:19` defines `ErrUnsupported` with the message
`transcoding requires Linux`. Its current production return at
`seal_production.go:27-29` rejects output plans that do not meet the finite
legacy VOD sealing contract, including while running on Linux.

Use a message such as `unsupported transcode operation`, retaining the error
sentinel and `errors.Is` behavior. This does not require adding a platform
fallback or changing unsupported-plan behavior. It is a small diagnostic
cleanup, not evidence that Windows server support remains.

### W05: Use one effective permission in the native management surface

`internal/identity/features.go:8-15` defines built-in feature restrictions for
playback, downloads, preferences and subtitle operations. Several overlap with
an existing `ManagedPolicy` Boolean at specific operation checks. The library
playback permission is the intersection of
`EnableMediaPlayback` and `AllowsFeature(FeaturePlayback)`
(`internal/library/policy_access.go:232`). Related operations have analogous
additional deny-only gates.

The native management form exposes both sources independently
(`web/admin/src/UserPolicyFields.tsx:22,34-36`). Its permission overview at
`ManagedUserDialog.tsx:369` reports playback as Allowed using only
`EnableMediaPlayback`. When `RestrictedFeatures` contains `goby_playback`, the
overview therefore disagrees with effective server policy.

Centralize the existing intersection in a small effective-permission
projection, and use that meaning for the overview. Group the related native
controls and expose the blocking rule rather than presenting a partial Boolean
as effective access. Do not introduce another persisted authority field.
Preserve both raw fields, independent patch semantics and every current denial;
do not automatically synchronize or normalize the two representations.
Label the summary as configured permission: item ACLs, access schedules,
account state and service availability still determine actual playability.

Do not assume every feature restriction is equivalent to one Boolean:
remote-control, library visibility, ownership and resource-specific permissions
still have separate meanings. Even `EnableMediaPlayback` has independent
consumers in HLS user limits, transcode authorization, playback-session
handling and wire DTOs. Paired checks at some sites do not establish global
equivalence of the fields. The useful simplification is the effective
projection for each operation, not deletion of the feature catalog or a
rewrite of saved policies. Verify the combinations of both fields, legacy
updates, draft previews and untouched denied operations. This finding claims
less configuration ambiguity, not a measured authorization speedup.

### W06: Bound storage-root availability observations

`GET /admin/v1/storage/roots` calls `storageRoots` in
`internal/server/libraries.go:195-209`. After entry authentication it loops
over configured roots and performs `os.OpenRoot`, `Open(".")` and `ReadDir(1)`
directly in the HTTP goroutine. There is no context observation, per-operation
deadline or finite filesystem-worker admission on that path. Connection-header
and idle timeouts do not bound an executing directory read.

A slow or unavailable network mount can hold the entire response. Cancelling
the request does not stop later loop iterations once a blocked call returns;
repeated requests can accumulate actual filesystem work. The native library
page waits for this request before clearing its storage loading state
(`web/admin/src/LibrariesPage.tsx:239-247`). Its library list may already be
visible, but storage status, new-library availability and storage refresh remain
affected.

Route this observation through existing configured-anchor/primary-I/O
facilities and a finite worker budget. `internal/library/server_directories.go`
already demonstrates admission, request cancellation, final authority and
shutdown ownership of filesystem workers. Reuse that ownership model rather
than introducing an unbounded goroutine behind a timeout.
Preserve the existing order of root/domain admission before worker acquisition,
so one queued root does not occupy the whole worker budget. Bound the request
as a whole, and distinguish admission pressure from a proven unreadable directory.

Retain the actual worker and descriptor/I/O accounting until blocked syscalls
finish, even if the HTTP response has already ended. Keep configured ordering
and path scope. Availability currently includes real directory read permission;
plain metadata, a successful binding lookup, or validate-only directory opening
does not replace `ReadDir`. A small availability-specific observation can share
the admission/lifetime code without inheriting a full directory listing.

Remote verification should gate a real/injected directory read, cancel and
repeat requests, mix normal and blocked roots, then release the gate. Check
bounded actual workers/FDs, no new work after cancellation, accurate
availability and recovery after release. No mount-failure runtime test was
performed in this review.

### W07: Transfer rendered provider previews through the existing output budget

`internal/server/providers_images.go:34-35` acquires one of the four shared
image-processing slots and defers its release until the entire handler returns.
It retains the slot through provider download, rendering, final metadata
authorization and HTTP output at lines 55-66. Ordinary image and avatar paths
already transfer completed output to `imageCache.beginTransfer` and release
processing capacity before writing the response (`images.go:408-416` and
`artwork_images_http.go:88-95`). The transfer budget is eight responses and
128 MiB of retained byte capacity.

With TMDB configured, four provider previews held in response output can
therefore occupy all processing admission for unrelated ordinary images.
Actual transport backpressure and frequency require a controlled remote
measurement. The 320-by-480 pixel bounds are not proof of a small retained
JPEG: `artwork/artwork.go:225-230` can pass through the original bytes of an
already suitable JPEG, including its metadata, within the source input limit.

After rendering and the final authority check, attempt the existing nonwaiting
transfer admission, then release processing only after the handoff succeeds.
Charge the capacity of every output buffer still retained by the response;
end the downloaded source buffer's lifetime before the handoff where it is
distinct. HEAD may have zero body charge only after body buffers are actually
no longer retained; otherwise conservatively charge their capacity. Preserve
exactly-once release on admission failure, writer setup failure, HEAD, write
failure and cancellation, and retain the existing idle-write deadline.

Keep download and render under their current bounded ownership. Releasing
processing before download completes would require a separate bounded input
queue and is not this recommendation. Do not add a new cache or change the
provider's network/security contract. Measure shared-slot occupancy and
ordinary-image latency under controlled slow preview responses before
prioritizing this low-frequency optimization.

## Known follow-through: single-query authorization transactions

R02 already recommended a pure read authorization path. Its redundant shared
locks and immediate recheck are gone, but these current wrappers still open an
explicit transaction for one `CheckAdministrator(..., false)` operation:

- `internal/server/admin_media_analysis.go:46-63`
- `internal/server/observability.go:73-89`
- `internal/server/media_diagnostic_runtime.go:137-145`
- `internal/server/media_operations_runtime.go:160-168`
- `internal/recovery/manager.go:317-322`

An explicit independent-read identity API could run the same statement without
the surrounding BEGIN and COMMIT/ROLLBACK. Keep the current `AuthorizationTx`
contract for business transactions; it explicitly forbids substituting a pool.
Preserve every revalidation point, deadline, audience, peer/device/key context,
fresh database clock and error distinction. Do not remove transactions around
business reads/writes or replace final mutation authority with an independent
pool query. This is tracked as R02 follow-through rather than a new finding.

## Linux-only status and retained boundaries

Commit `270714319e2a01fd0e2a671c6651f4c099447fd3` already removed the Windows
telemetry implementation and non-Linux backend stubs. The server and command
launcher entries have Linux build tags; release compilation selects Linux.
No further Windows server implementation was found in `cmd` or `internal`.

`commanddomain/launcher_other.go` is a `linux && !amd64` architecture rejection,
not a Windows implementation. `systemstatus/status.go:133` reports the actual
OS. Retain Windows client protocol fields, development-host helpers, historical
data formats, the private evaluator's non-Linux refusal and valid Linux kernel
capability checks. No production deletion is justified by this inventory.

An initial candidate to accept the literal Linux filename `a\..\b` was
rejected during round 3. Both `library.hasTraversal` and recovery root
validation reject that spelling; `internal/recovery/root_path_acceptance_test.go`
explicitly preserves the rejection. This is not the ordinary `a\b` mismatch
fixed by T08. Changing only registration would introduce a restore mismatch.
Broadening that accepted naming contract is separate work, not removal of
Windows server support.

The authority model does not show an unnecessary generic RBAC/ABAC framework.
Native administrator and Emby/application-principal boundaries serve different
contracts. Keep current authorization after waits, actor/target separation,
last-administrator protection and the management locks that actually protect
cross-record invariants.

Full lifecycle-history integrity, anchored file opening, publication checks,
authenticated backup completion and real resource retirement remain required.
Their complexity alone is not evidence of over-defensive design. Likewise,
do not narrow user-state media decoding while its existing contract requires
malformed stream structures to fail.

## Review-loop ledger

The stopping condition is three consecutive completed rounds with no new
supported recommendation. Refinements and duplicate findings do not create
new IDs. All four tracks completed each of the following rounds; the
coordinating review checked their counterexamples and the combined report.

| Round | Focus and outcome | New IDs | Consecutive empty rounds |
| --- | --- | --- | --- |
| 1 | Initial four-track source review and deduplication; checked fixed schema construction, process capture ownership, collection updates and Linux boundaries. | W01-W04 | 0 |
| 2 | Followed process capture through production scans; checked collection constraints, permission control overlap and storage-root consumers. Raised a Linux literal-name candidate for counterexample review; excluded an unproven Scratch shutdown race. | W05-W06 | 0 |
| 3 | Rejected the literal-name candidate against recovery tests, narrowed W05 to effective projection, checked actual directory-read semantics, and traced the provider image path. Conservatively reset the counter for the conditional W07 opportunity. | W07, conditional | 0 |
| 4 | Cross-checked W01 ownership and field writes, W02 constraints, W03 immutable schema, W05 independent policy consumers, W06 cancellation and W07 successful-result ownership/HEAD accounting. Refined wording only. | None | 1 |
| 5 | Traced HTTP consumers and error contracts, source/transfer lifetimes, background media queues, shutdown ownership, platform files, historical restore formats and prior findings. No new independent recommendation. | None | 2 |
| 6 | Final four-track pass over production callers, authority snapshots, all collection-entry writes, root/domain-before-worker admission, image handoff, static-evidence claims and Linux/client boundaries. No new supported recommendation or counterexample. | None | 3 - stop |

Rounds 4, 5 and 6 satisfy the requested exit condition. The only artifact added
by this task is this report. No production code, existing workspace changes,
remote source/data, shared cache or dependency was changed by the review.

Convergence is limited to the inspected backend scope. It does not establish
that the repository has no further defects or provide runtime acceptance.
