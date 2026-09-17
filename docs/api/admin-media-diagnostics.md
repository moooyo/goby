# Administrator media diagnostics

The media-diagnostics branch implements this native administrator API and its
Settings panel. The source has not yet passed Go, database, browser, real media
or deployment verification. No runtime or M5 acceptance is claimed here.

All routes require the native administrator cookie. Emby logins and application
keys are rejected. Writes additionally require the existing same-origin and
CSRF checks. Responses use `Cache-Control: no-store`. Query parameters, including
an empty `?`, are rejected. JSON bodies are case-sensitive, reject duplicate,
missing and unknown fields, and are limited to 4 KiB.

## Status and retained history

`GET /admin/v1/media-diagnostics` returns:

- `InstanceId`: a fresh 32-character lowercase hexadecimal identifier for this
  server generation.
- `Available` and `UnavailableReason`: whether the deployment permits requesting
  diagnostics. This is not a probe of the actual tool, device or resource profile.
- `HardwareConfigured`: whether a hardware backend is selected in deployment.
- `StartToken` and `StartTokenExpiresAt`: an opaque start-window value and its
  informational wall-clock expiry. The token is not a credential and must not
  be persisted by the dashboard.
- `RetentionSeconds: 1800`, `MaxRetainedRuns: 32`, and `Items`: newest-first run
  summaries. Unfinished resource owners are retained beyond the normal expiry.

A summary contains `Id`, `InstanceId`, `Revision`, `Mode`, `State`, `Code`,
`CreatedAt`, `UpdatedAt`, and nullable `FinishedAt`. Revision is a positive
decimal string that increases on each visible update for the same instance/run.
Clients must compare revisions rather than relying on wall-clock timestamps to
merge index, detail, and mutation responses. A delayed response must not restore
a completed run to an earlier running state.

History is held in this process/generation only. A restart clears it and changes
the instance identifier. Unexpired entries are never silently evicted to admit
a new request; a full history rejects new admission until an entry expires.

## Start and request identity

`POST /admin/v1/media-diagnostics/runs` accepts exactly:

```json
{
  "InstanceId": "00000000000000000000000000000000",
  "RequestId": "11111111111111111111111111111111",
  "StartToken": "opaque-value-from-the-status-response",
  "Mode": "software"
}
```

The example identifiers are placeholders. The client generates a new random
32-character lowercase hexadecimal `RequestId` only after an explicit new-run
decision. `Mode` is `software` or `configured`; configured mode includes the
software video/audio baseline and the configured hardware video stages. Paths,
devices, codecs, FFmpeg arguments and resource limits are never request fields.

Initial admission returns 202 and `{ "Run": ... }`. Repeating the exact request
with the same current native credential returns 200 and the original run, without
starting another worker. A different body or credential under the same ID returns
`request_conflict`. `Run.Id` is exactly the submitted request ID.

The token is bound to the instance, user and native session. Its five-minute
window uses process-local monotonic time, including a fresh check after capacity
reservation. A retained result can be retrieved by the identical request after
token expiry, but the original body cannot create another run after its history
entry has expired. Old instance IDs are rejected after restart.

An interrupted or invalid POST response is an unknown outcome, including when
the client detects a session change after the server may have accepted it. Keep
the instance, request ID and mode; query the original result before another
decision. Never automatically replay the POST or silently replace its ID. A 404
or restart does not prove the original operation never ran.

## Read and cancel

`GET /admin/v1/media-diagnostics/runs/{instance}/{id}` returns `{ "Run": ... }`.
Detail adds nullable `Report`, containing fixed preparation and stage facts from
the media pipeline. Raw media, stderr, credentials and loader environment values
are excluded. Every administrator can inspect the retained diagnostic results.

`POST /admin/v1/media-diagnostics/runs/{instance}/{id}/cancel` accepts exactly `{}`.
It rechecks native authority and requests cancellation without discarding passed
stages. Repeated cancellation is safe; a terminal run is returned unchanged.
GET is the authority for subsequent completion or cleanup status.

Run states are `queued`, `running`, `cancelling`, `cleanup_pending`, `passed`,
`failed`, `cancelled`, and `unavailable`. Stage facts additionally distinguish
`unverified` and `not_run`. A stage pass does not turn a resource-cleanup failure
into a successful run. Final run and report state/code agree; previously observed
stage facts remain intact after a late cancellation or authorization failure.

The dashboard polls active details every five seconds while visible, stops
automatic polling after a read error, and retains the last observed result for
manual refresh. Instance changes invalidate old start/cancel actions. Unknown
request references may be kept per administrator in session storage; tokens are
never stored there.

## Execution and shutdown

Only one diagnostic is active in a generation. When the live transcode manager
exists, it atomically reserves one global job slot. Ordinary conversion scheduling
accounts for that slot, and manager shutdown waits until it is released. No
playback identities, encoding rows or scan-task children are fabricated.

Native authority is checked in a read-only database transaction before admission,
before resource creation, before each command and before a successful final
outcome. A two-second watcher cancels in-flight work after authority becomes
unavailable; confirmed logout, session/device revocation, password reset,
demotion, disablement and account deletion also cancel the applicable run immediately.

The same execution owner is retained across partial initialization and failed
cleanup. Its resource slot is released only after `Close` confirms all owned
resources have closed. `cleanup_pending` retries closure of that owner without
starting media work. Shutdown starts playback cancellation promptly and waits
for diagnostics and conversion work before releasing the catalog/database.

Resource delegation and the borrowed non-writable scratch directory require
explicit deployment setup; see [installation guidance](../../deploy/linux/INSTALL.md).
Missing prerequisites yield an unavailable run rather than unbounded execution.
Actual codec-log compatibility, output tolerances, cgroup behavior, browser flows
and the complete software/hardware profiles still require remote verification.
