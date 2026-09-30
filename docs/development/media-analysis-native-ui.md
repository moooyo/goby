# Native media analysis workflow

Status: the automatic BIF workflow is **verified within its selected scope**:
the final dashboard-integration campaign passed 37 browser checks, 13 Node checks
and the administrator build. Two browser cases were skipped: the historical
full-Phase-3 case and a separate real dashboard-delivery fixture not configured
for that campaign. The real Docker BIF workflow passed its four selected phases;
both final software and AMD images separately passed actual UI and retained-BIF
verification, followed by owned-resource closure. The expanded
intro assessment is complete, but broader recognition is not accepted: all 12
source-reviewed openings were missed, with three correct short-ident negatives
among 15 evaluable originals. See the
[current checkpoint](bif-intro-expansion-20260930.md).
The earlier automatic-intro workflow retains its completed verification and
resource-closure scope in the [previous increment record](library-intro-automation-20260930.md).
Earlier Phase 2 verification retains its original source and manual-decision
UI/client-lifecycle scope. See the
[Phase 2 execution record](media-analysis-resilience-phase2-20260921.md) and
[recorded results](media-analysis-resilience-phase2-results-20260922.json).

## Entry and supported controls

The existing MUI administration shell adds **Media analysis** at
`/admin/media-analysis`. It uses the existing native session and CSRF request
boundary. The page does not expose tool paths, command fragments or a fabricated
processing result.

The page loads the analysis configuration and actual runtime availability. An
unavailable runtime displays its reported reasons; configuration remains editable
and backend admission retains the execution checks. Profile defaults come from
the server. The
complete profile uses a decimal revision string, including revisions beyond the
JavaScript safe-number boundary. A conflict or uncertain write preserves the
draft and requires a fresh revision before another save. Reload keeps the draft
for review rather than silently overwriting it.

Library creation and editing expose **Automatic intro detection** only for TV
libraries. `LibraryOptions.EnableIntroDetection` defaults to false, including
old libraries that lack the field. Enabling it requests background analysis;
completed scans of enabled libraries request another pass. Movie, TV and mixed
video libraries also expose the independent **Automatic seek previews** switch,
backed by default-false `LibraryOptions.EnablePreviewGeneration`. It requests
background BIF and thumbnail generation on enable and after completed scans.
Music libraries do not expose this preview option. The normal workflow requires
no separate intro/preview-start request or candidate approval.

Qualified current matches become available to playback automatically. `review`
and `no_result` mean that no automatic marker is applied; neither asks the user
to approve uncertain content. Disabling the option immediately withdraws detected
markers. Re-enabling waits for new work. Historical Manual/Import markers and
explicit intro chapters retain their compatibility.
An explicit false-to-true transition retires legacy rejection flags while keeping
their decision/audit history, so the new flow does not require a hidden reset step.
Turning off automatic seek previews has a different purpose: it stops new
automatic generation/publication while preserving existing valid previews.
The UI explains that behavior beside the library switch.

Saved-library notices link to Tasks for progress and errors. Each untouched
analysis task receives a daily interval and its own durable event trigger.
Existing custom, cleared or disabled task choices remain respected. The Media
analysis page presents both automatic features and links to library settings
and Tasks. `AutoPublishIntros` has no UI toggle; profile writes emit its canonical
true value. An actual analysis-profile edit requests new background work for
enabled libraries, including BIF rebuilding after a preview interval or quality
change. A no-op save does not request another run.

Interrupted automatic intro and preview work receives a durable replacement
request after graceful shutdown or crash recovery. New admission checks current
library policy. Manual work, explicit stops and maximum-runtime stops are not
automatically replayed; their actual outcomes remain visible in Tasks.

The preview interval is the requested minimum. Long sources can use a larger
interval to keep each preview file within the 4096-frame budget. Preview rows
report the server's actual dimensions, size, frame count, state and failure code;
they do not infer successful generation from an accepted run request.

Manual library/item preview-start buttons and the Force rebuild control are
removed from the normal page. Its results list retains filters, preview status
and details. Compatible explicit start APIs remain available for existing callers;
they retain their bounded selection, immutable receipt and Force semantics.
Automatic admission uses enabled libraries only, and an empty enabled set never
expands to all libraries. Backend admission still checks supported media and
current source state.

An unconfirmed admission retained by the prior UI keeps its complete request and
UUID in a user-scoped browser receipt. Checking that result repeats the original
request; it does not create a new UUID or change the scope or Force choice. The
page shows this only when a previous receipt exists. It does not restore manual
start controls or infer completion from that receipt.

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

The initial remote UI campaign passed 33 checks and retained one historical
full-Phase-3 skip; 13 Node checks passed. It covers the independent library
switches, supported collection types, saved notices, both automation descriptions,
and removal of normal manual preview-start controls. Progress, failure and
retained-request handling remain visible. These counts describe selected checks,
not independent accuracy samples. After dashboard `main` snapshot `4c4ed60`
was integrated, the final application source
`33445db2e2e64b6871116332c44605261a1bf2d4` passed 13 Node checks, the
administrator build and 37 browser checks. The two browser skips retain their
explicit scopes above; repeated campaigns are not summed as new coverage.

The separate real Docker preview journey used library-option changes, ordinary
scans and profile edits, with no manual analysis start or Force request. All four
phases passed: initial automatic generation, rescan/cache reuse, a 10-to-20-second
interval rebuild, and disabling with scan/container-recreation persistence. It
verified all three BIF widths, JPEG structure and sampled decoding, ranges,
ThumbnailSet positions, image tags and rejection of obsolete tags. The
[current checkpoint](bif-intro-expansion-20260930.md) records that scope separately
from intro recognition. The completed intro assessment produced 15 `no_result`
outcomes and no qualified intervals. A visible automatic workflow and successful
BIF delivery do not imply that short or variant episode openings are recognized.
The production detector and quality gates remain unchanged. Final software and
AMD images each passed a separate actual UI and exact retained-BIF check.
Their owned containers and Compose networks were removed and the private
PostgreSQL instance stopped with zero client sessions; protected services were
unchanged. These are application/UI results, not a new GPU campaign or improved
intro-recognition evidence. Short and variant openings remain to be improved.

In the previous automatic-intro increment, the actual AMD-image UI check passed
login, visibility of the TV-library
switch and absence of human-correction controls. Its first two attempts timed
out because the fixture's exact Username label omitted the required-field
asterisk. Using the project's existing `/^Username/` locator passed without a
product change; the original logs remain retained. This application/UI check
used software axes and did not repeat GPU verification.

The earlier Phase 2 seven mocked browser cases included manual decisions that
belonged to that historical UI. Its actual media/skip/cancellation/prune/restart
journey remains valid at its recorded source. The
[original cancellation-fixture failure](media-analysis-resilience-phase2-20260921.md#first-fourteen-source-run-and-cancellation-fixture-correction)
remains recorded. The successful successor used the same frozen product and
labels after the assertion correction; it was not a new unseen accuracy trial.
That previous automatic-intro workflow reused FH1–FH3 and N1/N2 for automation
checks. Those sources remain regression evidence; they are not counted as new
episodes in the current expanded accuracy evaluation.
