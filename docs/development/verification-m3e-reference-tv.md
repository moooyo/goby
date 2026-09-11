# M3e original-client TV browsing on the reference

Status: **reference-only checkpoint with a recorded layout difference**. This
does not establish Goby TV client acceptance. The safe
[machine-readable summary](m3e-reference-tv-summary.json) includes exact input
source hashes, report hashes, browser versions, state comparisons, and logout
proof. Validation ran only through `ssh test-env`.

The successful run is `reference-av-tv-09/observation.json` under
`/opt/goby-test/exec-work-m3e/`. It used the original Emby Web 4.9.5.0 UI,
Chromium 153.0.8010.12, Playwright 1.63.0, and fresh reference PID 332054 with
start ticks 357218. The dedicated `m3e-reference-av-client` account is the same
new, retained account described in the
[reference AV checkpoint](verification-m3e-reference-av.md). Existing fixture
accounts were not used or changed.

The actual UI journey was TV library, `M3e Client Series`, season selector 1,
season selector 2, `S2:E1 - Episode 2-1` details, Home, and Sign Out. The client
uses a lazy season menu: its select initially has an empty placeholder, while
the visible label opens two real menu buttons. The driver observes these
controls before clicking; it does not invoke client functions or construct
navigation or playback API calls.

| Selected season label | Matching visible episode labels | Actual list layout |
| --- | --- | --- |
| Season 1 | `S1:E1 - Episode 1-1`, `S1:E2 - Episode 1-2` | All three owned episode cards remain visible |
| Season 2 | `S2:E1 - Episode 2-1` | All three owned episode cards remain visible |

The selected label changes, and real season/episode requests complete with HTTP
200. The Series view retains a cross-season list. The report explicitly records
`strict_season_filtering_observed: false`; it does not claim that selecting a
season hides the other season. The detail check requires the unique visible
non-card heading, its exact `S2:E1` prefix and episode name, its detail content
container, and an actual page transition.

Opening episode details makes one original-client `POST .../PlaybackInfo`
request for the displayed media metadata. That request is recorded separately
from playback. The complete request set after browser shutdown contained no
Playing, Progress, Stopped, or media-stream request. Read-only media-element
observations remained inactive. Independent item-detail reads before and after
matched UserData for the series, both seasons, all three episodes, and the three
AV items. Configuration, Policy, and UserSettings hashes also matched.

The final run had no local HTTP errors or page errors. It ended with actual UI
Sign Out, HTTP 204, the visible login screen, and an independent HTTP 401 using
the exact observed logout token. Five instance checks passed, including before
logout and after browser shutdown. State reads and the independent rejection
probe are setup/cleanup evidence, not UI actions. The proof helper has no atomic
cancellation interface for a delayed independent request after an instance
change; this result applies to the continuously frozen recorded instance.

Eight earlier attempts remain as failed harness-calibration evidence. They
cover ambiguous home labels, transition waits, lazy-menu glyphs, the cross-season
layout, and the prefixed detail heading. TV05 left an owned session when its
open season menu blocked UI logout. Its exact private saved token was separately
logged out with HTTP 204 and then rejected with HTTP 401; that API cleanup is
not a successful UI logout. Other failed attempts ended with UI logout and token
rejection. Retained recovery files remain private and contain revoked tokens.

The seven exact successful source files were hash-checked and archived under
`/opt/goby-test/exec-work-m3e/verified-reference-tv-source/` before subsequent
Goby preflight changes. The executed entry point was:

```powershell
ssh test-env 'env PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright node /opt/goby-test/exec-work-m3e/client-browser-av.mjs --backend reference --check tv --output /opt/goby-test/exec-work-m3e/reference-av-tv-09'
```

The output path is retained evidence and must not be reused. The exported TV
workflow is `runTVBrowseUI({page, context, target, report, snapshot, library})`.
Its explicit library name permits only `M3e Reference TV` or the separately
owned Goby fixture's `M3e Client Television`; choosing the latter is not a
claim that the Goby journey has passed.
