# Movie05 evidence contract correction

This records the 84-check checkpoint before version-3 integration. The later
[v3 contract and verification](audited-client-v3-verification.json) integrated
the cancellation receipt and movie05 baseline, and its single movie06 attempt
is consumed. The [movie06 plan review](audited-movie06-plan-review.md) controls
current work; the pending steps described at this historical boundary are not
a second execution queue.

The [offline verification](audited-movie05-offline-evidence-verification.json)
passed 84 remote checks: 48 adapter, 24 closeout and 12 whole-run saved-evidence
checks. No browser, business HTTP, SQL or service operation was performed.
The product binary and five unchanged client helpers remain at their previously
verified revisions; the 16 movie-lifecycle checks are explicitly reused.

## Corrected evidence handling

The adapter now distinguishes owned BrowserContext media candidates from the
physical gateway's delivery proof. A candidate requires the selected item,
owned token, allowed target origin and GET 200/206. Each paired login needs a
media candidate. Original completion/failure flags are preserved, including
`ERR_ABORTED`; the result explicitly says physical validation is pending.
Completed, identity-bound Started/Stopped reports remain mandatory.

The closeout reader dispatches original-client login forms and JSON by the
recorded content type. Form handling follows the server's UTF-8, field, duplicate,
percent-escape and bound rules; it does not merge URL query credentials or
rewrite a form as JSON wire evidence. The opaque `PlaybackStartTimeTicks` hint
has a dedicated signed-int64 decoder for the exact playback report routes.
The observation reader re-derives the route from its URL and method and compares
the complete body with its retained payload. The physical reader uses the same
decoder. Generic JSON, identity fields and position integers keep their existing
guards, and request payload association still compares original bytes.

Media association prefers an exact Range match. If none exists, a fallback
requires an allowed physical GET 200/206, the same full URL and token, an allowed
logical media GET 200/206, containment of the single physical byte range within
the logical range, and recorded `ERR_ABORTED`. Its physical request cannot start
after the logical terminal timestamp, allowing only the one-millisecond timestamp
truncation interval. Terminal times must agree within the existing two-second
bound, and there must be only one candidate per frame/Service Worker scope.
The output records the association type and context ordinals. It does not infer
a browser cache implementation or collapse multiple physical requests into one.

The complete saved movie05 fixture now associates all six physical partial
responses while preserving their incomplete flags. In particular, physical
342's `bytes=11038080-48786887` is contained in logical 336's
`bytes=10682368-`, with one matching terminal event. Physical 288 and 289 remain
separate exchanges associated with logical event 286. Foreign tokens, disallowed
or non-2xx exchanges, malformed/suffix/multipart/unsafe ranges, earlier terminal
events and ambiguous candidates are rejected in targeted checks.

## Cancellation evidence recovered without replaying playback

The candidate writes to an append file, not the system journal. A bounded read
of its actual stdout file matched all eight media `X-Request-Id` values from the
saved gateway responses. The candidate process and its stdout descriptor were
checked before and after capture. The immutable captured prefix and the
[review receipt](audited-movie05-offline-evidence-verification.json) are retained.

Physical 339's exact request ID maps to a server event with `outcome=cancelled`,
status 200, zero bytes and a two-millisecond duration. Cancellation is now an
observed fact, replacing the earlier hypothesis. This response still contributes
zero media bytes. The current complete-physical-analysis entry point continues
to reject it until an explicit supplemental cancellation receipt is part of the
input contract; the saved regression asserts that this boundary remains closed.

## Page errors and the next execution boundary

The new page-error collector retains bounded error name, redacted message,
query-free page/source locations, phase/operation and nearby request metadata.
It records synchronously and redacts at finalization using the complete observed
credential set, including tokens from rejected login identities. Raw diagnostic
fields are never placed in the public summary. Unknown, oversized, unreadable,
overflowed or incompletely redacted evidence remains marked for review.
Diagnostic exceptions cannot suppress Stop, Logout or browser cleanup. Proximity
to an external request failure does not classify an error as harmless.

Movie05's four historical errors contain only timestamps. Their causes cannot
be reconstructed by the new collector. The old browser exit code 1, failed
outcome and original snapshots remain unchanged, and full client acceptance is
still open. The new source set has not been selected by the live controller;
its old frozen input and version-2 movie04 baseline remain historical.

Before another browser decision, extend the existing input contract to consume
the request-ID-bound server cancellation evidence and the
[closed, occupied movie05 state](audited-core-movie05-owned-state-closeout.json).
Only the remaining unstarted preparation can become Expired under the next
owned prepare; existing counted/Stopped history must remain protected. Keep the
ordinary actor and preserve its count/history rather than recreating the seed.
Define the missing page-error observation, expected outcomes, unchanged budgets
and cleanup ownership for one fresh run. Do not weaken the unknown-error gate,
reuse a consumed output, add another scenario runner or rerun product-wide tests
for these evidence-tool changes.
