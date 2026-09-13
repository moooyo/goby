# Audited movie UI lifecycle

`client-browser-playback.mjs` keeps the existing
`runMovieWorkflow({ page, report, snapshot, repeatLogin, target })` contract.
It drives the original UI and observes original responses. It does not issue
substitute API requests, change media time, or provide a consumer player.

## State and completion boundaries

| State | UI action | Required completion |
| --- | --- | --- |
| Home | Open the first visible exact movie title through its title action or card image action | The selected control is visible and unique. The original UI reaches `#!/item?id=...`, and the movie title is visible. |
| Initial detail | Select From Beginning, otherwise Play | At least one original control is visible; every candidate remains unique. The movie04 sequence of an empty detail area followed by one Play control is waited through. |
| Started | Observe before the single Play or Resume click | A same-origin main-frame Playing response has HTTP 204, a bounded valid identity body, and a successful `response.finished()` result. |
| Playing and pause | Observe advancing frames; click Pause | Media time and decoded frames advance. Pause completes only when the visible video is paused. |
| Seek and continue | Use the unique visible seek slider; click Play if paused | The actual video leaves seeking state, moves in the requested direction, and reaches the requested fraction within the existing tolerance. Subsequent frames advance. |
| Stop | Observe before one Back click | A new Stopped response matches the current Started item, media source, and play session. HTTP 204 and response completion are required, followed by all media being paused or removed. |
| Logout | Open Settings if needed, then click the unique visible Sign Out | The original Logout returns a complete HTTP 204 and the login view becomes visible. |
| Relogin | Invoke the caller's existing login callback and reuse the same card navigation | The detail item ID equals the first visit. Resume establishes a new play session and receives the same complete Stop treatment. |

Both visits use the same title-action/card-image selection path. The first
observed detail URL establishes the UI item ID; the second must match it. The
caller and physical closeout retain responsibility for binding that ID to the
declared catalog and actor. No unavailable DOM attribute is invented.

The existing snapshot and media labels remain available. Additional control
records contain only the fixed phase, operation, candidate name, and actual
count. `playback.lifecycles` contains the necessary observed item, media source,
play session, optional owner fields, position, and response-completion flags.
Raw bodies, authorization headers, tokens, and delivery URLs are not added.

## Response and failure ownership

Response waiters are installed before their UI actions and immediately handle
rejection. `response.finished()` has a separate ten-second bound using the
provided page's wait, so the audited adapter can shorten it to the remaining
execution budget. Returned errors, rejection, failed requests, missing responses,
and wrong status remain failures. Budget exhaustion retains its original code.

The two callers have different report-row schemas. This workflow therefore does
not infer completion from `report.requests[].status`, depend on optional caller
body fields, or write guessed `body` or `completed` fields into those rows.
The response identity body is parsed only for fixed Playing and Stopped routes,
with a 64 KiB limit; only selected identity fields are retained.

Failure preserves the original phase, operation, and safe error code before
diagnostic snapshots or cleanup run. Stop and logout cleanup are independent:
an observation or Stop failure cannot suppress the logout attempt. An already
armed Stop or Logout is awaited again without issuing another Back or Sign Out.
An already requested account menu is awaited without toggling Settings again
after a diagnostic failure. No missing Stop is fabricated. If logout succeeds
after incomplete Stop evidence, the report does not claim private state is still
required. The current logout state is reset before the
relogin callback, since that callback can create a session and then fail.

This layer proves logical UI response completion. The caller still owns exact
token rejection, browser closure, and observer cleanup. The physical ledger and
SQL closeout independently establish request cardinality, Progress evidence,
media delivery, session revocation, and durable playback state.

## Offline regression boundary

`test-client-browser-playback.mjs` uses synthetic controls, response objects,
and a small synthetic DOM interface. It covers the retained movie04 control
ordering, both complete lifecycles, response completion failures, identity
mismatches, cleanup after observation failure, and relogin failure ownership.
It reads no vendor JavaScript or HTML and starts no browser, network listener,
API request, SQL operation, or service. These tests do not authorize another
live attempt or establish real-client acceptance by themselves.

The 16 lifecycle checks passed on `test-env`. A separate
[saved-control replay](audited-movie-control-readiness-replay.json) used the actual
movie04 empty detail and later unique Play observations. The
[offline closeout](audited-movie-offline-alignment.json) binds this result to the
verified retained-baseline controller and lossless durable-state reader. No new
browser run is part of that verification.
