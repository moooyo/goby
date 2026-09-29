# Native media analysis workflow

Status: the September 30 automatic library workflow is **in implementation and
verification**. See the [increment record](library-intro-automation-20260930.md).
Earlier Phase 2 verification is complete at its recorded source; that accepted
manual-decision UI and client lifecycle do not establish a pass for this changed
workflow. See the
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

Library creation and editing expose **Automatic intro detection** only for TV
libraries. `LibraryOptions.EnableIntroDetection` defaults to false, including
old libraries that lack the field. Enabling it requests background analysis;
completed scans of enabled libraries request another pass. The normal workflow
requires no separate intro-start request or candidate approval.

Qualified current matches become available to playback automatically. `review`
and `no_result` mean that no automatic marker is applied; neither asks the user
to approve uncertain content. Disabling the option immediately withdraws detected
markers. Re-enabling waits for new work. Historical Manual/Import markers and
explicit intro chapters retain their compatibility.

Saved-library notices link to Tasks for progress and errors. The default task
has a daily interval and a dedicated durable event trigger. Existing custom,
cleared or disabled task choices remain respected. The Media analysis intro
section links to library settings and Tasks. `AutoPublishIntros` has no UI
toggle; profile writes emit its canonical true value.

The preview interval is the requested minimum. Long sources can use a larger
interval to keep each preview file within the 4096-frame budget. Preview rows
report the server's actual dimensions, size, frame count, state and failure code;
they do not infer successful generation from an accepted run request.

Preview generation remains manual and independent of the TV intro switch.
Library checkboxes create an explicit preview scope bounded to 64 libraries;
individual item actions use the current page's selection of at most 25 items.
The HTTP API's legal empty-array all-library selection is not triggered by an
empty UI selection. Previews support movies, episodes and videos. Backend
admission retains authority over supported media and current source state.
Force is explicit and included in the immutable preview-start request.

An unconfirmed admission retains the complete request and its UUID under a
user-scoped browser receipt. The receipt survives in-app navigation and, when
session storage is available, a reload. Checking that result repeats the same
request; it does not create a new UUID or silently change its scope or Force
choice. No run is shown as completed by this receipt.
Retained receipts from the prior UI can still be resolved; they do not add an
intro-start button to the current normal workflow.

## Task progress, results and cache

The admitted RunId is read using the existing task-run endpoint, with TaskId
binding checked before display. The page reuses RunProgress and RunStatusChip.
Polling pauses when the document is hidden and stops on a request error. The
existing cancel endpoint requests a stop; the page continues to show the task's
actual state until the service reports a terminal outcome. The Tasks navigation
opens the existing task workspace. No invented run deep link is used.

Result detail is loaded separately from the list. It shows:

- Recorded intro status, stale/unavailable information and update time.
- The effective playback interval when one is available.
- Recorded preview outputs and their explicit failure states.

Accept, reject, reset and manual intro-edit entry points are removed. The result
view does not present uncertain candidates as a human-review queue. Historical
data, audit records and compatibility APIs remain available in the backend.

Cache cleanup requires confirmation and the saved configuration revision. The
receipt reports removed entries/bytes, remaining bytes and retained busy entries.
Active readers and in-progress builds/publications are displayed. The UI never
claims that busy entries were immediately removed or that original media changed.

## Verification coverage

Current remote verification must cover the TV-library switch, automatic status,
progress/errors, absence of candidate-decision/manual-intro controls, retained
preview actions, revision conflicts and uncertain request recovery. No new
browser pass is claimed here while this increment is in progress.

The earlier Phase 2 seven mocked browser cases included manual decisions that
belonged to that historical UI. Its actual media/skip/cancellation/prune/restart
journey remains valid at its recorded source. The
[original cancellation-fixture failure](media-analysis-resilience-phase2-20260921.md#first-fourteen-source-run-and-cancellation-fixture-correction)
remains recorded. The successful successor used the same frozen product and
labels after the assertion correction; it was not a new unseen accuracy trial.
The new workflow reuses FH1–FH3 and N1/N2 for automation checks. These are
previously evaluated sources, not a new unseen accuracy population.
