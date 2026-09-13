# Native rejection observation and subtitle readiness

Status: **targeted remote verification passed**. This follows the
[Episode01 review](audited-episode-client01-review.md). Product bytes, candidate
epoch, media, hosting and the network boundary are unchanged.

The adapter now captures native `unhandledrejection` reason metadata before
application navigation. It distinguishes primitive undefined from the string
`"undefined"` and reads native Response URL/status/type/redirected and request
ID metadata without consuming its body or invoking arbitrary object getters.
The synchronous bridge uses only a page CDP session and Runtime binding; it
does not enable Debugger or collect vendor source, stacks or additional network
traffic. The existing pageerror channel and rejection rule remain authoritative.

Collection is capped at sixteen events and 32 KiB. Final output uses the full
collected credential set for redaction; locations omit query, fragment and
userinfo. Response association requires one recorded request ID with the same
URL and status. Ambiguous IDs and mismatches remain explicit. Association does
not establish a pageerror's cause or harmlessness. The stated coverage includes
installed document contexts in the current page session; workers and
out-of-process frames are not automatically covered.

Bridge setup/teardown is independently bounded to one second. Diagnostic errors
mark collection incomplete and do not join the business observer queue or
replace its original failure. Collection continues through owned Stop/Logout
and page disposal. The unchanged original browser and token cleanup remain
responsible for client state.

The subtitle entry also reuses the already verified movie detail URL parser and
control-readiness helper. It waits for the declared movie item and one visible
From Beginning/Play action instead of checking after a fixed 500 ms. From
Beginning retains priority when present; duplicate controls reject selection.
Subtitle selection, cue checks, seeking, reporting and cleanup are unchanged.
The new entry tests execute this production path and stop after choosing the
play action, avoiding a simulated full subtitle player.

The controller's component guard distinguishes fresh adapter/native/subtitle
checks from unchanged closer/version3/movie checks reused through the original
component03 receipt. A synthetic Chromium verification is recorded as a browser
run, with zero original-client runs. Old adapter results cannot attest the new
source, and old source pins remain fixed historical values.

Verification uses the existing runtime and dependencies on `test-env`. It must
cover native propagation, safe projection and limits, cleanup failures, strict
JSON compatibility, unchanged pageerror rejection, subtitle readiness and the
actual new component receipt consumed by the controller. The synthetic browser
runs in a bounded unit with a private network; it cannot access the real client
or candidate services. No full Go suite is required for these tool-only changes.

The [verification checkpoint](native-rejection-observer-verification.json)
records 83 passing component checks: 55 existing adapter checks, seven saved TV
response checks, three subtitle-entry checks and eighteen native-rejection
checks. The selected controller passed 35 guards and three actual saved-receipt
integrations for component selection, current admission and historical hosting.
Source hashes remained unchanged during verification. The final synthetic
browser, context, temporary HTTP server and verification unit are closed.

The first synthetic attempt passed 82 checks and failed its redirected-response
fixture. Playwright's route handler only handles the initial redirected URL;
the saved counts show the final response was not supplied by that fixture.
Its original failure remains retained. The corrected fixture uses a private
loopback server for exactly two GETs, a 302 and final 404, while preserving the
same production code and eighteen test cases. It also preserves safe diagnostic
state when a case fails. There were two synthetic browser starts across these
two verification scopes and zero original-client runs. No exact DNS error is
claimed from the original generic failure.

The first subtitle input can now bind these selected sources and the current
closed state after fresh entry review. This checkpoint establishes neither a
passing original-client scenario nor a main deployment.
