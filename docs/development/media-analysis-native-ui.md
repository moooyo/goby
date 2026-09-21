# Native media analysis workflow

Status: verification of the declared Phase 2 scope is complete. The corrected
full client-lifecycle run passed and its worker and observer independently
closed. Mainline publication remains pending. See the
[Phase 2 execution record](media-analysis-resilience-phase2-20260921.md) and
[recorded results](media-analysis-resilience-phase2-results-20260922.json).

## Entry and supported controls

The existing MUI administration shell adds **Media analysis** at
`/admin/media-analysis`. It uses the existing native session and CSRF request
boundary. The page does not expose tool paths, command fragments or a fabricated
processing result.

The page loads the analysis configuration and actual runtime availability. An
unavailable runtime displays its reported reasons and disables affected starts;
configuration remains editable. Profile defaults come from the server. The
complete profile uses a decimal revision string, including revisions beyond the
JavaScript safe-number boundary. A conflict or uncertain write preserves the
draft and requires a fresh revision before another save. Reload keeps the draft
for review rather than silently overwriting it.

The preview interval is the requested minimum. Long sources can use a larger
interval to keep each preview file within the 4096-frame budget. Preview rows
report the server's actual dimensions, size, frame count, state and failure code;
they do not infer successful generation from an accepted run request.

Library checkboxes create an explicit scope. Selection is bounded to 64 libraries;
individual item actions use the current page's selection of at most 25 items.
The HTTP API's legal empty-array all-library selection is not triggered by an
empty UI selection. Intro actions require episodes; previews support movies,
episodes and videos. Backend admission retains authority over supported media,
current source state and same-series/same-season comparison cohorts. Force is
explicit and included in the immutable start request.

An unconfirmed admission retains the complete request and its UUID under a
user-scoped browser receipt. The receipt survives in-app navigation and, when
session storage is available, a reload. Checking that result repeats the same
request; it does not create a new UUID or silently change its scope or Force
choice. No run is shown as completed by this receipt.

## Task progress, decisions and cache

The admitted RunId is read using the existing task-run endpoint, with TaskId
binding checked before display. The page reuses RunProgress and RunStatusChip.
Polling pauses when the document is hidden and stops on a request error. The
existing cancel endpoint requests a stop; the page continues to show the task's
actual state until the service reports a terminal outcome. The Tasks navigation
opens the existing task workspace. No invented run deep link is used.

Result detail is loaded separately from the list before a decision. It shows:

- Recorded status, reasons, source suppression and current effective provenance.
- Candidate interval, boundary uncertainty and comparison similarity scores.
- The support set's episode identities and intervals, without displaying content
  hashes as user-facing explanations or labeling similarity as probability.
- Recorded preview outputs and their explicit failure states.

Accept requires a current qualified/review episode candidate. Confirmation
explains that acceptance writes a manual marker and can replace an existing
manual interval. Reject suppresses detection for the current source while
retaining manual/import/chapter priority. Reset clears the detection decision and
suppression without promoting stale evidence. Every decision sends the analysis
revision, current source revision and manual revision returned by the same detail
read. A conflict blocks another decision until detail is reloaded.

Cache cleanup requires confirmation and the saved configuration revision. The
receipt reports removed entries/bytes, remaining bytes and retained busy entries.
Active readers and in-progress builds/publications are displayed. The UI never
claims that busy entries were immediately removed or that original media changed.

## Verification coverage

`web/admin/src/mediaAnalysis.test.ts` contains source-level checks for exact
profile drafts, bounds, large revisions, malformed/inconsistent response rejection
and stale/unsupported candidate admission. The remote source-check and mocked
browser results, with their source bindings, are recorded in the
[Phase 2 execution record](media-analysis-resilience-phase2-20260921.md).

`web/admin/e2e/media-analysis.spec.ts` contains seven synthetic-API browser cases:

1. Configuration conflict, preserved draft and exact-revision retry.
2. Explicit library scope, Force, real task-response progress and stopping state.
3. Candidate acceptance, dual revision values, manual priority, reject and reset.
4. Source conflict, reload and stale-candidate rejection.
5. Cache CAS and retained busy-entry reporting.
6. Missing tools with disabled starts and visible configuration/results.
7. Unconfirmed admission across reload with exact request reuse.

These synthetic cases cover the UI and protocol boundary; actual media and the
complete client lifecycle were also verified within the declared scope,
including cancellation, pruning and restart. The
[original cancellation-fixture failure](media-analysis-resilience-phase2-20260921.md#first-fourteen-source-run-and-cancellation-fixture-correction)
remains recorded. The successful successor used the same frozen product and
labels after the assertion correction; it was not a new unseen accuracy trial.
