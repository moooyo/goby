# Owned browser callback transparency

This bounded test uses authored HTML and a synthetic HTTP/WebSocket server. It
does not load an original client, business database, reference asset, or product
executable. Synthetic HTTP is real network traffic within a new systemd private
network namespace; `business_http: false` does not mean that no HTTP occurred.

Final result: attempt 02 passed all four cases in 22,044 ms and is independently
sealed. The exercised native callback-to-fetch-to-visible-DOM chain remained
functional with the actual reference actor, repeated production DOM sampling,
and actual Goby v7 actor/collectors. This is not a claim of zero side effects on
every aspect of page state or proof of original-client acceptance.

The [safe summary](library-changed-browser-transparency-02-summary.json),
[independent terminal](library-changed-browser-transparency-02-terminal.json),
and [exact-invocation lifecycle](library-changed-browser-transparency-02-lifecycle.json)
retain the completed result. The first
[failed report](library-changed-browser-transparency-01.json) and
[failed terminal](library-changed-browser-transparency-01-terminal.json)
remain separate immutable evidence.

The harness is
[`test-library-changed-browser-transparency.mjs`](../../scripts/test-env/test-library-changed-browser-transparency.mjs).
It checks the exact systemd cgroup, invocation identifier, Linux root identity,
entrypoint, pinned source closure, Node executable, Chromium executable, and
Playwright package before creating any fixture listener or browser. It requires
its network namespace to differ from `/proc/1/ns/net`. The fixed production
loopback origins therefore address the synthetic server, not a host service.

## Test design

At most four cases run serially in one new isolated scope:

1. A fresh native browser without a production proxy, event recorder, or DOM
   sampler provides the baseline. The authored application itself records its
   native callbacks and issues its own requests.
2. The same page logic runs through the actual corrected reference browser actor,
   including its production HTTP proxy, WebSocket relay, and Playwright recorder.
3. The same reference actor runs while the unchanged production
   `observeReferenceDOM` sampler repeatedly reads the page at a 500 ms cadence.
4. The same authored page logic runs through the actual source55 v7 BrowserActor,
   `HomeViewsObserver`, and `LibraryChangedObserver`. Fixed synthetic identity
   parameters differ to satisfy the genuine actor contracts. The complete
   source55 acceptance workflow and historical fixture loader are not invoked.

Two real server LibraryChanged messages exercise function `onmessage` and
function `addEventListener` callbacks. The callbacks synchronously record
`this`, `target`, `currentTarget`, native MessageEvent identity, trusted status,
data type, and listener order. The first callback performs its own allowed Movie
DTO GET and renders the returned changed title; the second performs its own GET
and renders the returned original title. No assertion incorrectly requires
`currentTarget` to remain set after awaiting fetch.

The synthetic server independently records each callback GET and exact response.
Production cases additionally require paired physical/browser message evidence,
matching body hashes, complete HTTP responses, and request sequence numbers
after the received message. Normal cleanup requires native close evidence,
synthetic logout, actual actor session-proof 401, and removal of browser, proxy,
fixture HTTP, and WebSocket resources. Failure cleanup records what completed
without inventing a normal logout or native close.

Each case is bounded to 60 seconds and the normal suite to 300 seconds. The unit
uses `RuntimeMaxSec=340`, `TimeoutStopSec=10`, and `KillMode=control-group`, inside
the 360-second outer allowance. A failed scope is sealed and never replayed.
The unchanged reference runtime is the corrected version from commit
`7ae35af5d6b823d4dbda126c00f90e328dec19e0`, SHA256
`209f72fa812647742003b50c311ee358ffc6f1279e29a2e142eec506245fed7c`.

## Attempt 01: failed and sealed

The first execution failed in the uninstrumented baseline after both callback
GETs returned HTTP 200. The combined `transparency_page_noninterference`
assertion assumed zero focus, selection, and scroll events plus a minimum timer
count. The page telemetry was passed directly into an assertion before being
assigned to the report, so the failed report does not preserve which term caused
that combined assertion to fail. It would be incorrect to infer a production
instrumentation defect from this baseline failure.

| Retained identity | Value |
| --- | --- |
| Tool | `/opt/goby-test/exec-work-m3e/library-changed-browser-transparency-tool-01` |
| Output | `/opt/goby-test/exec-work-m3e/library-changed-browser-transparency-execution-01` |
| Unit | `goby-library-changed-browser-transparency-01.service` |
| Invocation | `18b4684094764d8a904af30c8ea354f4` |
| Main process | PID `1501239`, exit `1`, final MainPID `0` |
| Private network namespace | `net:[4026533402]` |
| Host network namespace | `net:[4026531840]` |
| Harness SHA256 | `da69dc98d9c7961051aeb1ffa9e75d6422a9391fc81879244701c3b72f87b84b` |
| Input manifest SHA256 | `585ba770f6132643a44f7e040fbaf9d7f0f3ff70df03fd94a0cd85a59a53d8be` |
| Suite report SHA256 | `ea7b8e6f5f7c9c4ad4ed4013457fe8a83b2fbecf9122c61118ee16209ed14fc9` |
| Evidence inventory SHA256 | `4f3e55f1ec9abf341185f37fef6127cddccefbcfdf183930fc4b2f98794e6e66` |
| Failed terminal SHA256 | `9b7e240a106789c13f9dd8d6e4edaf3b5146754d28b74c2a9b88727d7bc55aff` |

The suite elapsed 3,528 ms; the failed baseline elapsed 2,833 ms. The synthetic
server recorded five HTTP requests: authored HTML, login, Views, and two callback
GETs. No production actor case ran. Business HTTP remained zero.

The terminal binds the exact invocation, exited main process, empty recursive
cgroup, zero fixture sockets, stopped fixture listener, and zero pending resource
factories. The ordinary synthetic logout path was not reached. The baseline
session-revocation field remains false, and native close proof was not obtained;
the synthetic server and its containing process/namespace were removed instead.
Those missing proofs are not rewritten as successful cleanup assertions.

## Interpretation boundary

Attempt 01 does not establish end-to-end production instrumentation transparency.
The follow-up correction retains native page telemetry before asserting it and
compares the requested callback/HTTP/DOM properties against the native baseline,
rather than treating incidental baseline focus or scheduling counts as a
predetermined zero. Focus, selection, scroll, and timer counts are diagnostic
telemetry, not a claim that every aspect of page state is unaffected. Attempt 01,
reference v4, and Goby v7 remain consumed and unchanged.

## Attempt 02

A fresh tool/output/unit scope was created after independently confirming that
all 22 files in attempt 01's inventory and its failed terminal still match their
sealed hashes. The harness now persists separate `before`, `forward`, `restored`,
and `observed` page/server checkpoints before assertions. It separately reads
the actual visible title after each callback response. Native close and resource
closure also have retained checkpoint records. The case work and cleanup bounds
were tightened, and late asynchronous resource creation is rejected after abort.

| Execution identity | Value |
| --- | --- |
| Tool | `/opt/goby-test/exec-work-m3e/library-changed-browser-transparency-tool-02` |
| Output | `/opt/goby-test/exec-work-m3e/library-changed-browser-transparency-execution-02` |
| Unit | `goby-library-changed-browser-transparency-02.service` |
| Invocation | `e0374f7c53204acc9380aea576e49d8c` |
| Main process | PID `1501658`; verified absent after completion |
| Harness SHA256 | `b1d01d539b1cc5446000130983e046facb64fde12e16957d46771b758853fe03` |
| Input manifest SHA256 | `f9dfc3c8a05b5787a8b134565ddac0d6d82583a42a39aad3a6da67c5f8eabbf6` |
| Suite report SHA256 | `71ed4172734e87da88239ff4125b5231a406c3c8307ac1c1218191d6ecce874f` |
| Safe summary SHA256 | `13934a636d0a5395b03d25c561d0630e76de84acaac86ebb71af5b723250d638` |
| Evidence inventory SHA256 | `575eb103bc7b609013c082cc2bc163f0661b2ebc2b42847f747a11d00dfa1db4` |
| Independent terminal SHA256 | `8d88c9b7dcac16ae53d562a488a2c0831a3b65f3b8ac35326b86e339465c0843` |

| Case | Elapsed ms | Native callback records | Callback GETs | All synthetic HTTP | Production sampler reads | Result |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Uninstrumented baseline | 5,188 | 4 | 2 | 6 | 0 | Passed |
| Actual reference actor | 5,176 | 4 | 2 | 7 | 0 | Passed |
| Actual reference actor with sampler | 5,624 | 4 | 2 | 7 | 6 | Passed |
| Actual Goby v7 actor and collectors | 5,611 | 4 | 2 | 7 | 0 | Passed |

Both native listeners ran once per message in their recorded registration order.
Synchronous `this`, `target`, and `currentTarget` all referred to the native
WebSocket. Each observed event was a trusted native MessageEvent with the native
constructor, string payload, matching origin, and expected LibraryChanged data.
Each case visibly changed the target to `Owned Movie After`, then back to
`Owned Movie Before`, through the callback's own real GET and response consumption.
The three production cases each retained two complete physical GETs, two complete
browser GETs, and matching physical/browser message body hashes, with GET
sequences following message receipt. The baseline deliberately has no production
capture arrays; its independent synthetic server still recorded the requests.

The sampled case performed six unchanged `observeReferenceDOM` calls, including
both displayed names and the unchanged visible anchor. All cases recorded exactly
the two application title mutations. Diagnostic focus counts were 1, 1, 1, and 0;
timer counts were 52, 52, 61, and 52. Selection and scroll counts were zero. These
incidental differences are retained and are not presented as absolute page-state
equivalence or as evidence of a protocol fault.

All four cases retained native CloseEvent evidence with code 1000 and
`wasClean: true`. Every fixture listener stopped, all fixture sockets closed, and
the synthetic session was revoked. The three production actors additionally
completed their real session-proof path: UI logout 204 followed by rejection of
the same synthetic token with 401. The suite generated 27 real synthetic HTTP
requests and zero business HTTP requests. The source closure and all 22 pinned
attempt 01 files were rechecked unchanged during independent sealing.

The successful unit became inactive and systemd recycled its dynamic invocation
and ExecMain properties before the final query. The terminal does not treat the
subsequent default `ExecMainStatus=0` as an original process exit record. Instead,
it binds the retained `started.json` and suite report to the originally observed
PID/invocation, verifies PID 1501658 is absent and the recursive cgroup is empty,
and retains PID 1's exact-invocation lifecycle records. The journal selector was
`journalctl --unit=goby-library-changed-browser-transparency-02.service --no-pager --output=json`.
The matching fields are `UNIT`, `INVOCATION_ID`, and `_PID=1`; the success event
has MESSAGE_ID `7ad2d189f7e94e70a38c781354912448` and reports successful
deactivation of that same invocation. Its timestamp is
`2026-09-12T17:26:02.636927Z`.

| Retained unit evidence | SHA256 |
| --- | --- |
| `unit-journal.jsonl` | `195fdccc527bdc2fd4b1e3814c4979abb4d538898e9322af789831d0b7768075` |
| `unit-lifecycle.json` | `6246e53ccfc25f9a719cf2d785ddb712290bca467c0bf7c12eb62ec5ef918169` |
| `unit-final.json` | `7d06fbaa69092ac4b0d4610094f11d1a1513320876ec54704d8ee2436bdb32b8` |

The unit started once with `PrivateNetwork=yes`; it was not restarted to recover
recycled metadata. Future new units should use `RemainAfterExit=yes` to retain
their invocation properties until independent closure. No consumed scope should
be restarted to improve its evidence.

This successful authored-HTML result establishes only the exercised
instrumentation behavior. It does not prove that the original application's
internal LibraryChanged handler executes, that its Movies page must refresh for
a Name-only update, or that the existing client/deployment acceptance gate has
passed.
