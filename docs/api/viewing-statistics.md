# Estimated viewing statistics

`GET /emby/Users/{UserId}/ViewingStatistics` returns estimated content watched
for the target user's currently visible Movies and Episodes. This implements
the handoff's hours concept; it does not measure elapsed playback-session time.
The player shows hours alongside its watched movie/episode counts and explains
the estimate in the tooltip and accessible label.

Example successful response:

```json
{
  "EstimatedContentHours": 2,
  "EstimatedContentTicks": "54000000000",
  "IsEstimate": true
}
```

The example is 1.5 hours of content, rounded to two display hours. The response
uses `Cache-Control: private, no-store`. It has no pagination because aggregation
runs over the whole currently authorized result set before returning one total.

Each visible, non-folder Movie or Episode contributes once:

- A watched item contributes its known positive runtime.
- An unfinished item contributes its saved position, clamped between zero and
  that runtime. This corresponds to runtime multiplied by progress fraction.
- Unknown, malformed, or out-of-range runtimes, other item types, and items
  outside the caller's effective library/content access contribute nothing.

Repeated plays do not multiply the contribution. Seeking, pausing, playback
speed, and time spent replaying are not accumulated. Changing watched state,
progress, metadata, or access can change the estimate. There is no new analytics
event history or background analysis task.

`EstimatedContentTicks` is a canonical non-negative decimal string. Ticks use
100-nanosecond units, and 36,000,000,000 ticks equal one hour. Database numeric
aggregation and integer rounding preserve precision for large libraries.
`EstimatedContentHours` is a non-negative exact JavaScript integer, rounded to
the nearest hour with half an hour rounding upward. `IsEstimate` is always true.

The route uses normal Emby authentication, target-user selection, and effective
library/content policy. A user identifier does not grant additional access.
Unauthorized cross-user projection and invalid target selection are rejected;
the endpoint does not expose hidden-library statistics through totals. It is a
read-only Goby extension, not a claim that original Emby has an identical route.

The player cancels abandoned requests and verifies the active account before
accepting a response. It validates the numeric/string fields and their rounding
consistency. An unavailable or malformed estimate remains visibly unknown;
the client does not substitute zero or total library runtime.

The original handoff's `meVals` function sums full runtimes of watched items
and runtime times progress for unfinished items before displaying hours. Exact
elapsed-time analytics would be a separate optional upgrade and is not a
requirement of this implementation.

Implementation: [aggregation](../../internal/library/viewing_statistics.go),
[HTTP route](../../internal/server/viewing_statistics.go), and
[player adapter](../../web/player/src/lib/viewingStatistics.ts). Focused HTTP
and browser verification is recorded in the
[player acceptance record](../../web/player/ACCEPTANCE.md).
