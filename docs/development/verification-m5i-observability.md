# M5i activity and diagnostic administration verification

Status: the M5i product increment passed acceptance and is deployed on
`test-env`, schema 22/probe 6. This document does not claim the full project
complete. The final Git tree is checked after these documents are finalized.

## Implemented scope

M5i adds committed administrative activity, bounded safe JSONL diagnostics, four
native administrator read routes, four Emby-compatible read routes, and a React
and MUI Activity and logs page. It does not add a browser player. The precise
API and compatibility limits are in [the contract](../api/observability.md);
storage, configuration and lifecycle details are in [the implementation guide](observability.md).

Activity insertion shares the business transaction. The integration covers
users, sessions, application keys, devices, libraries, scanning, metadata,
settings and scheduled tasks. A failed activity insert rolls back the business
change; existing final authorization checks still follow the insert. No-op and
terminal-transition rules retain their domain semantics. Background work uses
an explicit system actor rather than a fabricated administrator.

Diagnostic files contain allowlisted event and attribute values. The same
filter protects both persistent files and the process fallback. The logger
records the registered route pattern and generated request ID, excluding raw
URLs, query values, headers, request bodies and arbitrary error text. Files are
owned by a dedicated directory and served through fixed-size snapshots. The
download wrapper retains transport deadlines and live authorization checks,
including blocked writes and final flushing. Native downloads support HEAD and
Range; the Emby download HEAD response is explicitly 404.

## Completed preliminary checks

All tests, validators, runtime requests and media/browser probes execute on
`test-env` through SSH. Local Go and administrator production builds are
authorized separately and are compilation checks only.

| Check | Observed result | Evidence |
| --- | --- | --- |
| Activity, diagnostics, database, configuration and process-entry race tests | 79 top-level passes, no failures | [Core report](m5i-core-race.json) |
| Business activity and selected native HTTP race tests | 68 top-level passes, no failures | [Domain and HTTP report](m5i-domain-http-race.json) |
| New browser runner's effect-fenced guards | 29 passes, no failures, no live browser acceptance | [Browser guards](m5i-browser-guards.json) |
| New deployment operator's synthetic contracts | 33 passes, no failures, no real deployment or restore | [Deployment guards](m5i-deployment-operator-tests.json) |
| Local administrator build | TypeScript compilation and Vite production build succeeded; 54 generated files | Final browser acceptance is pending |
| Local Linux/amd64 Go build | Compilation succeeded | Final candidate is separately built from the frozen remote source |

These early test reports describe their own snapshots. Later Emby adapters and
the final diagnostic route-pattern adjustment are covered by the completed full
suite, not retroactively by these earlier passes. The selected HTTP expression
did not include three tests whose names contain `NativeLogs`; the full suite
includes them without filtering.

Two preliminary failures remain visible. The first domain attempt encountered
a new fixture expiring a session before its creation timestamp. The fixture now
sets its creation time one day earlier, while preserving actual post-insert
expiry, final authorization failure and transaction rollback assertions; see
[the retained failed attempt](m5i-domain-audit-attempt-1.json). The first reference
guard attempt rejected a lazy standard UTF-16 codec import through its strict
effect fence. Preloading the required standard codecs fixed the test harness
without relaxing that fence; see [the retained guard failure](m5i-observability-reference-guards-attempt-1.json).

## Official reference evidence

The separate [official observability study](../research/observability-reference.md)
completed on an exclusively owned Emby 4.9.5.0 instance and was torn down. Its
96 new safe records consist of 94 complete HTTP responses, one initial startup
connection refusal, and one audit observation. All 76 recorder HTTP attempts
obtained complete response bytes. The existing 2366 records and 240 media
entries remained unchanged; the reference corpus now contains 2462 JSON files.

The study proved a real user-creation activity cause, observed administrator,
viewer, anonymous, invalid-token and owned-key behavior, and sampled paging,
date filtering, lines, downloads and HEAD. Both ordinary credentials and the
single owned key were revoked with independent unauthorized responses. The
new program-data directory was removed and its process exited; private wire
bytes and safe exports remain retained. [The aggregate report](m5i-observability-reference.json)
binds all 96 safe exports and the reviewed script hashes.

Reference capture is evidence about Emby, not product acceptance. Goby's
bounded paging, fixed errors, always-safe JSONL, true associated user IDs and
microsecond date boundary are documented deviations. Unproved query parameters,
arbitrary upstream log access and whole-client parity are not advertised.

## Frozen candidate and completed product acceptance

The final full-suite snapshot contains 470 Go/module/migration inputs, ten
test-data files and all 2462 reference JSON files. It is isolated from ongoing
documentation edits. Go 1.27.1 builds the Linux/amd64 candidate with CGO disabled
and trimmed paths; FFmpeg and ffprobe 9.0.1 are used by remote verification.

| Required acceptance | Current state |
| --- | --- |
| Unfiltered `go test -race -p 2 -count=1 -timeout 20m -json ./...` | 1380 top-level passes across 17 tested packages, zero skips and no race findings; [full report](m5i-full-race-summary.json) |
| Real browser activity filtering/paging/details, log preview/download, mobile layout and revocation | Passed in 12.400443 seconds, without retries or skips; [browser report](m5i-observability-browser.json) |
| Immediate header/query/path/body sentinel checks against all then-retained diagnostic bytes | Passed before subsequent requests could rotate the evidence away |
| Two actual restarts preserving all 29 public tables and surviving diagnostic bytes | Passed, with 66 activity rows preserved at each restart and actual downloads of recovered files |
| Isolated restore of the complete old 28-table backup before main-service stop | Passed; [deployment report](m5i-deployment-evidence.json) |
| Exact schema 21 to 22 upgrade, empty initial activity table and dedicated log directory | Passed; original rows and protected files preserved |
| Deployed native and Emby observations, native CAS name change/restore and credential retirement | Passed in 0.776 seconds; [deployed workflow](m5i-deployed-observability.json) |
| Source, fixture, browser-input and candidate binding | Passed for 535 inputs and 54 assets; [source gate](m5i-final-go-source-gate.json) |
| Final staged Git tree and local links | Checked after document finalization and before commit |

The accepted executable has SHA-256
`1ead2fcaa22df227d3d8b6b607978887ccfee7868139fc38f40523dbe24752ef`.
The administrator asset archive has SHA-256
`20ed0e16b721515f1e56dddeef818e3102a7c92f25f2a1d8d08aa1d2b756f85b`.
The published candidate manifest binds both to the full-suite and browser
reports. The deployment runs as UID 995 with a dedicated `/var/log/goby-test`
directory in mode `0700`, provided by the new observability systemd drop-in.

The browser also completed sixteen native downloads without exhausting reader
slots. It revoked its own session while the independent control session
survived both restarts. Both credentials were ultimately revoked and independently
rejected with `401`; the temporary database, role, process tree and runtime were
removed, and the original PostgreSQL HBA bytes were restored.

The first browser attempt downloaded and validated real JSONL successfully but
failed its network-event observation assertion. The installed Playwright
Chromium adapter does not construct a normal Request for intercepted downloads
without a network ID. The corrected test uses an exact-URL CDP Fetch observer,
continues the original request unchanged, and requires successful observer
cleanup. Product code and generated UI assets did not change. The
[failed attempt](m5i-observability-browser-attempt-1.json) remains available.

The first deployment invocation rejected a `.tar` versus `.tar.gz` filename
contract mismatch before service mutation. Only that exact operator and fixture
literal changed; all 33 guards passed again before successful deployment.
[The rejected invocation](m5i-deployment-attempt-1.json) is retained.

The deployed workflow issued 17 GETs, two HEADs, three POSTs, two PUTs and one
DELETE, with twenty read-only SQL observations. It used one fresh native and
one fresh ordinary Emby credential, restored the complete original settings,
and preserved every old row. The six new activity entries describe the two
settings changes and four session transitions; two revoked sessions and one
ordinary device remain as real history. No scan, task, media mutation or
physical history deletion was performed.

All four accepted screenshots were visually reviewed:
[activity](screenshots/m5i/observability-activity-desktop.png),
[activity detail](screenshots/m5i/observability-details-desktop.png),
[server logs](screenshots/m5i/observability-logs-desktop.png), and
[mobile log preview](screenshots/m5i/observability-mobile.png).

The full M4/M5/M6 roadmap and broader third-party-client compatibility remain
open beyond this increment.
