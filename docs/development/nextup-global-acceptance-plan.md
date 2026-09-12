# Global NextUp reference and client acceptance plan

Status: **planned; not executed**. Prepared on 2026-09-12 from Goby source and
retained public API observations. This document creates no fixture, session,
request, migration, or service operation. It does not extend the active
LibraryChanged acceptance run or claim global NextUp compatibility.

## Objective and execution order

Establish a reproducible public reference contract for global
`GET /emby/Shows/NextUp`, implement only an observed difference, and verify that
the original client consumes the result and refreshes it after real playback.
Keep three conclusions separate: reference observations, Goby policy/regression
results, and original-client acceptance.

Execute the following gates in order:

1. After the current LibraryChanged work releases its fixture, bind a new run
   manifest to the then-current reference, isolated candidate, source, and
   fixture identities. Do not copy an old PID, source digest, schema, row count,
   or after-snapshot into a new precondition.
2. Prepare two owned series and two new ordinary accounts per target using a
   separate, explicit preparation manifest. Complete the zero-state baseline
   and freeze the exact item mapping before playback.
3. Execute the bounded reference API matrix below. Capture a positive global
   response before claiming a positive-selection rule or changing Goby to
   match it. A positive response alone does not establish ordering.
4. Review the public results and freeze their exact requests, state facts, and
   expected responses. Resolve any candidate-policy difference in the isolated
   NextUp implementation, then run the Store and HTTP regressions remotely.
5. Run original-client global browsing, real playback, and refresh acceptance
   against the reference and candidate with equivalent owned states.
6. Verify cleanup, retain sanitized receipts, update the NextUp evidence
   boundary, and report each gate independently. An unresolved reference or
   client gate remains open even if all Goby tests pass.

All tests, syntax checks, builds, runtime probes, and acceptance execution must
run through `ssh test-env`. If that environment is unavailable, verification
is blocked; there is no local fallback. No step in this plan is being executed
as part of the document-writing task.

## Existing evidence and open questions

| Behavior | Current evidence | Consequence for this increment |
| --- | --- | --- |
| A completely unstarted series yields no series-directed result | Captured with short and 600-second synthetic episodes | Retain as a SeriesId control. |
| After S01E01 is played, SeriesId returns S01E02 and S02E01 | Public reference controls, including actual playback reports | Retain the complete ordered sequence rather than one item per series. |
| A later watched episode skips an earlier unplayed gap | Public short-episode control | Retain the watched-cursor control; do not infer every rewatch case. |
| With none watched, only S01E01 partial returns all three episodes; only S01E02 partial returns S01E02 and S02E01 | Public API controls; the later-partial run verified all three full episode details | This proves the single-partial SeriesId cursor, not global selection. |
| Global results for the captured synthetic samples | Empty after manual marking, playback reports, a 30-second wait, 600-second media, and Video/Audio capability declaration | Neither an always-empty rule nor a positive global rule is established. Do not repeat these factors as an assumed explanation. |
| One global candidate per active series, highest-watched cursor, first episode for incomplete-only history, most-recent series activity ordering | Implemented Goby policy and Goby tests | Label assertions as policy until an equivalent public reference control supports them. |
| Multiple partials, rewatch, a newer earlier partial before a later watched episode, count-only/date-only history, multiple versions | No sufficient independent reference evidence | Keep outside the minimum compatibility claim. Use separately bounded follow-ups if needed. |
| EnableResumable and EnableRewatching | Earlier combined/no-op controls did not isolate their effects; the pinned SDK parameter list does not declare them | Do not guess defaults, accepted values, or behavior, and do not add them to Goby based on their names. |
| TV browse and episode-detail client acceptance | Existing original-client journeys did not stream media or send playback reports | Those reports do not prove a global request, global card, or playback-driven refresh. |

Evidence sources are [the NextUp guide](next-up.md),
[M3b verification](verification-m3b-sessions-nextup.md),
[public reference controls](../research/reference-server.md),
[fresh control observations](m3e-reference-nextup-v1.json),
[fresh control cleanup](m3e-reference-nextup-safety.json), and
[later-partial limitations](m3e-reference-nextup-later-partial-safety.json).
The source11 [26-test receipt](m3e-profile-nextup-source11-tests.json) is a
historical bounded regression result, not proof of this plan.

The fresh recorder's `series-parent` label means `ParentId={series}` with no
`SeriesId` parameter. Do not report it as a combined SeriesId/ParentId query.
The later-partial continuation did not repeat global, ParentId, or pagination
queries, even though the original uncompleted driver branch contains such calls.
Use completed response artifacts as evidence, not intended script coverage.

## New run identity and owned data

Use symbolic identities in this plan; resolve all actual IDs through public
responses during the new preparation gate. Reference and Goby IDs need not
match. Freeze a semantic mapping, not an assumption about numeric ID order.

| Symbol | Required owned fixture |
| --- | --- |
| `LA`, `LB` | Two dedicated TV libraries, each with exactly one study series. |
| `A`, `B` | Series in LA and LB respectively, with stable distinct names. |
| `A1`, `A2`, `A3` | A's S01E01, S01E02, S02E01. |
| `B1`, `B2`, `B3` | B's S01E01, S01E02, S02E01. |
| `P`, `Q` | Two newly owned ordinary accounts, initially allowed only LA and LB, with independent credentials and no playback history. |

Use six ordinary 600-second, 30 fps synthetic episodes with known immutable
bytes. Record runtime, season/episode numbers, parent and series relations,
media hashes, and library/root ownership. Existing approved synthetic bytes
may be copied into new owned roots by the preparation gate; do not modify or
rescan unrelated reference libraries. No alternate versions, specials,
metadata-provider downloads, or unrelated auxiliary media belong to this gate.

Record each target's process identity, version, endpoint, network namespace or
equivalent existing isolation identity, and the new evidence root. Bind Goby
to its source manifest, executable hash, current schema, and latest accepted
fixture observation. Use the existing fixture coordination mechanism without
replacing its lock. Do not overlap state-changing runs on the same fixture.

Record LA/LB's library, view, source-root, series, and season IDs separately.
Discover the IDs returned by `Views`, item details, and the actual client; do
not substitute a library-management ID for an item/view ID. A displayed name
alone does not prove identity. Preserve both the raw ID and mapped symbol in
evidence.

Before the first state mutation, read full details for all six episodes for
both P and Q. Require Played=false, zero position and count, and no playback
date; retain the complete UserData object and field presence. Also retain
series/season UserData, both account policies/configurations/preferences, and
the owned catalog mapping. List projections cannot prove absence of
LastPlayedDate or PlayCount changes. Construct the cleanup route set and
request reserve before dispatching playback.

## Public API matrix

The protocol matrix uses one authenticated recorder session per ordinary
actor. Playback state must be established with public PlaybackInfo and
Started/Progress/Stopped reports and then confirmed in full item details.
Reporting a requested position is not proof that it persisted or completed.
Do not inject reference SQL or infer state from response codes alone.

Define a core observation bundle for actor X:

- Six full episode-detail GETs for X, including untouched episodes.
- Global `Shows/NextUp?UserId={X}&Limit=10&Fields=UserData,ParentId&EnableImages=false`.
- The same request with `SeriesId={A}`, then with `SeriesId={B}`.
- Preserve exact response status, body types, ordered IDs, count, and full
  returned UserData; record request parameters and response timestamps.

These query paths use `/emby/`. The reference recorder must enumerate the
allowed route/query combinations; it must not forward arbitrary URLs. Do not
add speculative flags. Missing UserId, authentication errors, or broad SDK
parameter fuzzing are outside the reference core matrix.

| Step | Public mutation and full-detail precondition | Observe | Supported SeriesId control / global question |
| --- | --- | --- | --- |
| R0 | No mutation; all six episodes are untouched for P and Q | Core bundle for P and Q | Both series empty. Capture the two-account global baseline. |
| R1 | P completes A1 at its authoritative 600-second duration | Core P; global Q and Q's A1 detail | A returns A2,A3; B remains empty. Does global become positive for one active series? Q must remain untouched. |
| R2 | P completes B1 after A1; require distinguishable persisted playback dates | Core P; global Q and Q's B1 detail | A and B each have a remaining sequence. Does global contain one candidate per series, and in which order? |
| R3 | P completes A2 after B1; require distinguishable persisted playback dates | Core P; global Q | A returns A3. Does a fresh A activity change the global order without duplicating or dropping B? |
| R4 | Q completes B1, then A1, with distinguishable persisted dates; do not mutate P | Core Q, then core P | Compare independently created histories and target-user isolation. Do not borrow P's history for Q. |
| R5 | After the positive-result gate below, restore P's six episodes to the verified zero baseline, then report only A2 at 120 seconds | Core P; global Q and Q's A1/B1 details | A returns A2,A3; B empty. Global partial selection is an observation, not a prefilled expected A1 or A2. |
| R6 | P reports only B1 at 120 seconds later than the R5 activity; both A2 and B1 remain unplayed | Core P; core Q | Two partially started series establish a second ranking/eligibility control. Record whether global includes resumable items. |

To separate timestamps, use bounded real time between distinct playback
lifecycles and read the resulting full LastPlayedDate fields. Do not write
reference timestamps or use request order as a substitute for persisted date
order. If timestamps tie, report the ranking control as inconclusive; a new
playback lifecycle is an additional mutation requiring the run's existing
enumerated reserve, not an unrecorded retry.

Run R0-R4 first. If every global response remains empty, preserve the result
and proceed only to the bounded original-client discovery described below.
Do not execute R5-R6, expand hypotheses indefinitely, or change Goby to always
return empty. If the client also produces no positive global result, close the
run as `reference_global_positive_unresolved` and record the next missing
evidence. Goby policy regressions may still pass independently.

After the first positive global state, freeze that state and execute this
query extension before any further state mutation:

| Query variant | Required observation |
| --- | --- |
| Omitted SeriesId; omitted Limit, Limit=0, Limit=1, StartIndex=1/Limit=1, and an offset beyond the measured count | Exact IDs, totals, defaults, and out-of-range behavior. Do not assume the reference count semantics from Goby. |
| ParentId=LA, LB, A, and a real season of A, one at a time | Exact scope and count; record any distinction between library/view/source-root identities. |
| SeriesId=A plus ParentId=LA, then ParentId=A's later season | Combined scope behavior. Separate this from ParentId-only queries. |
| EnableUserData=false and EnableImages=false; a separate requested Fields projection | Field presence and primitive types, not just item IDs. |
| Same global read by the other actor | Independent current state with the same catalog access. |

Use no more than 300 recorder HTTP requests per target for the API gate,
including login and cleanup, with 80 reserved for reconciliation and cleanup.
The frozen manifest must count its exact planned route sequence before use;
stop normal work before spending the cleanup reserve. Preparation and browser
traffic have separate manifests and cannot silently consume this budget.
Do not restart a completed recorder or overwrite an evidence directory.

## Ordered Store and durable global HTTP regressions

Review the captured contract before editing the candidate selector. The
minimum product scope is [library/nextup.go](../../internal/library/nextup.go)
and, only if a demonstrated wire difference requires it,
[server/nextup.go](../../internal/server/nextup.go). Existing history columns
already expose played, count, position, date, and update time; this plan has no
identified schema change. Preserve the authorization snapshot, recursive
traversal boundaries, and batched projection while changing selection.

Extend [Store tests](../../internal/library/nextup_integration_test.go) with
explicit ordered assertions. The existing `nextUpAssertIDs` compares a set and
does not establish global ordering; keep membership assertions where useful,
but compare `Items[index].ID` against an ordered expected list for ranking.

| Store assertion | Required coverage |
| --- | --- |
| Two-series activity ordering | Distinct A/B activity dates, then an activity swap; exact full order before and after. |
| Stable ties | Equal activity dates with controlled series sort names and IDs; exact deterministic ordering. Label it Goby policy unless independently observed. |
| Candidate identity | Highest watched ordinal, earlier gap, first/second-only partial, favorite-only exclusion, and all-complete series exclusion. Distinguish captured behavior from current policy. |
| History-only inputs | Separate count-only and date-only state; fallback update time; multiple partials and watched precedence. These are policy regressions until sampled publicly. |
| Pagination and totals | Exact ordered full result, concatenated one-item pages, total before pagination, default/bounded size, and beyond-end pages. |
| Current authorization | Another user's different history; hidden series filtered before count/page; missing versus inaccessible explicit scope; current playback policy reflected in CanPlay. |
| Catalog boundaries | Parent scope with history outside that season, nested Series, cycles, cross-library links, nullable/special numbering, and ordinary-item exclusions. Retain existing regressions. |

Add a dedicated global HTTP integration test alongside
[series HTTP tests](../../internal/server/nextup_integration_test.go) and
[durable partial tests](../../internal/server/nextup_partial_integration_test.go).
The latter always sends SeriesId and therefore does not establish global
durable behavior. The new test must:

1. Index two real synthetic series through the existing Goby test fixture;
   authenticate two users and establish state through the actual HTTP
   PlaybackInfo/Started/Progress/Stopped path. Confirm the stored effect with
   full item details and the test's own Goby database assertions.
2. Issue global HTTP requests with no SeriesId and compare exact ordered IDs,
   totals, positions/counts/played flags, Limit=0, page offsets, ParentId, and
   requested projection. An HTTP 200 or nonempty array alone is insufficient.
3. Reorder history through a second lifecycle and confirm a subsequent global
   request changes as expected. Include partial-to-complete progression,
   duplicate terminal reports, and unchanged independent-user state.
4. Reuse the same token after a current library restriction; hidden candidates
   must disappear before counting/paging, explicit hidden scopes must fail,
   and another ordinary user's UserId must not be accepted. Restore only the
   test-owned policy and assert the resulting candidates again.
5. Cover application credentials with an explicit target user's history and
   the current application/user catalog scope. Retain existing key revocation
   regressions; a positive SeriesId key test does not cover global selection.

Run targeted tests and necessary operator/driver guards on `test-env`, then
the required full race suite/build for the frozen implementation snapshot.
Record the exact source, test names/counts, failures/skips, and output digests.
Run the deployed HTTP matrix against the exact verified candidate bytes. Do
not treat a historical suite receipt or a local static read as execution.

## Original-client global acceptance

Use the original client as a running application through the existing browser
automation surface. Inspect public JSON responses and visible UI only. Do not
read vendor JavaScript, decompiled code, application resource source, or the
reference database to explain selection. Do not intercept or replace API
responses, manually call playback-report endpoints from the UI driver, inject
history into the page, or manufacture a NextUp component.

Create a fresh client phase with the exact reference/candidate process and
fixture bindings. Reconcile the chosen account's state before browser login;
protocol and browser activity must never run concurrently for that account.
Record the client version, browser/driver inputs, account symbol, and initial
full episode details. Use at most two playback attempts and 20 minutes per
target; retain failure evidence rather than looping until the desired UI
appears.

1. Navigate through the actual Home/TV surfaces. Identify a completed client
   `Shows/NextUp` request with **no SeriesId**; retain its actual query,
   authenticated actor binding, ordered response IDs, and the visible cards.
   Record the real screen label instead of assuming it is named Next Up. A
   Series details request or injected fetch does not satisfy this step.
2. If the API gate had no positive global response, use this one bounded UI
   discovery to observe the client's real parameters and initialization. If
   it remains empty, stop with the unresolved reference result. Any new
   parameter hypothesis requires a separately frozen public control; do not
   guess EnableResumable or EnableRewatching behavior.
3. In a verified positive state, select the card whose ID maps to the expected
   episode. Prove actual media delivery and a progressing player timeline,
   plus the client's own Started/Progress/Stopped or terminal reports. A
   PlaybackInfo request from episode-detail loading is not playback proof.
4. Exercise partial stop and completion in the bounded attempts. Use visible
   player controls for any seek, and record it; seeking near the end is not
   proof that the entire 600-second file was played. Confirm the resulting
   durable episode state with full public details after the UI reports.
5. Observe the next client global request and visible candidate change. Bind
   the response to the completed playback lifecycle and the current source.
   Distinguish automatic refresh from returning Home, revisiting TV, or using
   a visible refresh control. Do not claim automatic push refresh when a user
   navigation caused the request. Check the other series remains correctly
   present and ordered under the observed contract.
6. Repeat equivalent states and actions against Goby, mapping IDs by fixture
   identity. Compare public response behavior and visible outcomes. Record
   original-client logout separately from recorder API cleanup.

If client behavior changes unrelated preferences or capabilities, retain the
complete before/after facts and classify them as this owned run's effects.
Do not weaken a frozen preservation assertion or restore an old profile over
unexplained drift merely to obtain a passing result.

## Cleanup, preservation, and evidence receipt

Keep all credentials, cookies, tokens, raw sensitive responses, and native
paths private. Export only sanitized request/response facts, semantic item
mapping, necessary digests, lifecycle facts, and conclusions. Store failures
and successes in distinct fresh evidence roots. Preserve existing receipts,
synthetic source bytes, user state, libraries, and unrelated service state.

Journal every owned state-changing intent before dispatch, including playback
negotiation and reports. After a lost response, reconcile the known owned
state before retrying; no blind duplicate playback or restore writes. Unknown
session/token ownership or foreign state drift requires a retained
recovery-required result, not global logout, account resets, or broad deletion.

For the reference, use public APIs only. For Goby, own-database durable
assertions and scoped read-only observations are permitted during the future
remote verification gate. Never inspect the reference database or modify it
to establish or clean up evidence.

Stop each known owned playback session, then restore only this run's changed
episodes to their measured initial zero baseline. The retained reference
control showed that `LastPlayedDate:null` in a UserData POST did not clear the
date; individual episode DELETE PlayedItems cleared the measured zero-state
history. Reuse that narrow restoration only after its baseline/preconditions
hold, and verify every affected episode with a full detail GET. Do not use a
whole-series reset or a nonzero-history account as a shortcut. Compare
series/season summaries as well, without treating them as episode-detail
substitutes.

Restore only explicitly changed owned policy/configuration fields using a
fresh revision/current-state check where the Goby API requires it. Confirm the
complete scoped baseline afterward. Revoking each exact owned recorder token
requires its own logout and a rejection check with that same token. Browser
logout requires its actual UI transition, corresponding request, and exact
token rejection. An acknowledgement without a follow-up check is not the
complete cleanup receipt.

Newly owned accounts, login/device records, playback/audit history, and fixture
catalog/media may remain as explicitly declared run artifacts. Do not delete
them automatically or claim whole-database equality. Restoring projected
UserData does not erase legitimate audit, sequence, or authentication history.
Report account/library/media disposition and any residual session or playback
responsibility separately.

The final safe receipt must contain:

- The exact input and process/source bindings, actor/item mapping, request
  budgets consumed, and hashes of immutable reports.
- Per-step observed full state, ordered global/series responses, and any
  positive-result, pagination, parent-scope, or ranking evidence gaps.
- A clear separation of public reference facts, Goby policy assertions,
  remote regression results, deployed API results, and real-client results.
- Actual client request/response and visible-state correspondence before and
  after real playback, including whether refresh was automatic or navigated.
- Cleanup facts, retained intentional artifacts, preserved scope, and any
  unresolved recovery responsibility.

Only update [next-up.md](next-up.md) and acceptance summaries after the relevant
gate has actual evidence. This plan alone establishes no result for global
selection, flags, multiple versions, broader client behavior, or complete
M2-M6 compatibility.
