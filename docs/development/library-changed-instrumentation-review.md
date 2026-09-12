# LibraryChanged instrumentation review

Date: 2026-09-13. Scope: static review of owned browser instrumentation used by
reference UI v4 and Goby source55 v7. No tests, browser runs, runtime probes,
business HTTP, service changes, or experiments in consumed execution roots were
performed by this static review. Separate remote verification by the root task
is recorded below. No reference application JavaScript, CSS, executable, or database was
read. This document is the only new artifact from the review.

## Conclusion

No page-side WebSocket wrapper or MessageEvent redispatch was found in either
reviewed execution path. Both observe Playwright WebSocket events outside the
page and forward admitted network frames. There is no identified shared mutation
of `onmessage`, `addEventListener`, `this`, `target`, or `currentTarget` that explains
the sealed results. This is a source-level finding, not proof that the complete
instrumented browser behaves identically to an uninstrumented browser.

At the time of this static review, the existing guards did not establish the
end-to-end property: a native
page message callback runs, initiates its own HTTP request, consumes the response,
and updates visible DOM while the actual production transport and repeated DOM
sampling are active. The subsequent [owned-HTML control](library-changed-browser-transparency.md)
passed four cases, establishing this bounded functional chain without reading
or rerunning the reference client. It does not establish universal transparency.

One concrete transport concern was found in the frozen reference runtime: an interleaved
control frame is held behind an incomplete data message. It is not shared with
the Goby transport and does not explain a LibraryChanged message already received
in full by the page.

Follow-up status: the root task corrected the working runtime and verified the
new frozen tool copy remotely with 32 passing pure guards. Independent static
review of the runtime and guard diff found no issue within that correction's
scope. The original finding still describes runtime `b720d7bb...`; reference v4's
frozen sources, tools, and consumed actual scope remain unchanged and were not
replayed. The correction does not establish the cause of v4's negative
observation. The subsequent four-case owned-HTML control passed for the native
baseline, reference actor, reference actor with repeated production sampling,
and Goby v7 actor/collectors. It establishes the exercised functional chain,
not zero side effects on every aspect of page state.

## Source identity and boundary

The input `source_closure` records identify the owned deployed files. Read-only SSH
file reads confirmed these frozen tool-file digests; no reference application process was launched or restarted.

| Owned file | SHA256 |
| --- | --- |
| `reference-library-changed-ui-tool-04/client-browser-library-changed-reference-runtime.mjs` | `b720d7bbdcf08ce59ae966a3f9d97eb0ebb946e790e563b46b8c50beec1c03e5` |
| `client-library-changed-source55-tool-07/client-browser-cross-user.mjs` | `60dea05efc3091cc8574f08867d92eff8472a256747fb9a521cb667190197d4e` |
| `client-library-changed-source55-tool-07/client-browser-library-changed-source55.mjs` | `39e4aa077c6007b3964469f4791174875b280d99659282a6656d77846bf485d0` |

Paths in that table are relative to `/opt/goby-test/exec-work-m3e`. The repository
source references below also cover the directly related owned driver, Home
observer, session-proof adapters, and guard definitions. This reviewer edited no
source; the root task's separate correction is identified below. Linked runtime
line numbers outside the explicitly frozen finding follow the corrected working
file, whose change adds five lines below the parser.
This review does not replace the existing frozen source-closure or terminal
attestations.

The sealed behavioral observations remain unchanged: reference v4 has one paired
LibraryChanged message and zero physical/frame HTTP requests in each complete
120-second window; both sets of 225 DOM samples retain the original title. Goby
v7 has the corresponding paired message, zero window HTTP, and 237 old-title
samples in its completed forward window. Page-frame receipt establishes arrival
at the browser observation surface, not execution of an internal application
handler. Neither negative result is changed into client acceptance by this review.

## Native browser event path

The reference runtime's `observePageSocket` attaches Playwright
`framereceived`, `framesent`, `socketerror`, and `close` listeners at
[`client-browser-library-changed-reference-runtime.mjs:1178`](../../scripts/test-env/client-browser-library-changed-reference-runtime.mjs#L1178).
Its `observe` function copies `event.payload` into a Node Buffer, decodes the copy,
and adds evidence at lines 1156-1175. `publish` changes only evidence binding
fields at lines 1134-1137. A bounded pending queue handles browser observations
that precede physical-handshake binding, and listeners are installed before the
initial binding attempt. No page WebSocket constructor or prototype is replaced.

Goby's `BrowserActor.observeLibraryChangedSockets` uses the same Playwright event
surface at
[`client-browser-cross-user.mjs:1510`](../../scripts/test-env/client-browser-cross-user.mjs#L1510).
It attaches before navigation, copies the payload at line 1528, requires a text
LibraryChanged message, and passes a separate copy to `notifyHome`. The buffer
cleared at line 1539 belongs to the observer; it is not the page's `event.data`.
`notifyHome` at lines 1459-1475 creates another owned Buffer copy. The source55
factory at lines 2124-2128 changes the capture scope and lifetime of this actor,
not its page event dispatch.

Neither path calls page-side `dispatchEvent`, assigns `onmessage`, replaces
`addEventListener`, installs an initialization script, or wraps WebSocket or
EventTarget prototypes. The Node callback's `event` is the Playwright observation
object, not a browser `MessageEvent` whose `target` or `this` is being forwarded.
The lifecycle hooks in
[`LibraryChangedObserver.attachPage:926`](../../scripts/test-env/client-browser-library-changed-source55.mjs#L926)
record navigation/document/socket changes and set observer failure state; they do
not cancel or redispatch page events.

There is a recording-order difference. Goby's initial socket binding occurs
before its frame listener is attached, and an unbound LibraryChanged frame is
counted as an observer error rather than queued. The reference path explicitly
queues early observations. This could affect evidence collection around a new
handshake, but neither path removes an application listener. The already paired
page receipts in the sealed windows are inconsistent with a missing observation
of those particular messages. Existing tests do not exercise both binding orders
with a real browser callback.

## Wire forwarding and a concrete control-frame concern

Both parsers retain separate original wire buffers and decoded payload buffers.
They do not serialize the projected LibraryChanged object back onto the wire.
Both reject compression negotiation and enforce bounded framing; reference
requires text data frames, while Goby specifically requires a text LibraryChanged
message when its capture callback is enabled. These are deliberate admission
constraints, not a universally transparent proxy contract.

Frozen reference runtime `b720d7bb...`, `decodeWebSocketFrames` at
[`runtime:549`](../../scripts/test-env/client-browser-library-changed-reference-runtime.mjs#L549)
holds every complete wire frame in `state.held` at line 573 and releases that list
only when `state.opcode === null` at line 594. During a fragmented text message,
an interleaved ping or pong therefore remains held until the final data fragment.
If an endpoint waits for a pong before sending the remaining fragment, this
creates a plausible transport stall. A close inside an unfinished fragment is
also explicitly rejected at line 592. This is a concrete source behavior; no
such sequence was established in the sealed run.

Goby `inspectWebSocketFrames` returns each complete original frame at
[`cross-user:723`](../../scripts/test-env/client-browser-cross-user.mjs#L723),
including a control frame between data fragments. Its library-message evidence
is emitted only after the final wire write completes in `forwardFrames` at
lines 916-927. Reference `transfer` at current lines 1026-1059 records write completion
after writing the admitted original frames. Buffer cleanup and evidence
projection do not rewrite their outgoing payloads.

The reference concern is not a common explanation for the observed zero-HTTP
windows: the matching messages at issue completed and reached the page. It is a
separate, testable transparency limit, not a demonstrated product defect or a
reason to change the existing acceptance result.

## Verified correction in a new tool scope

The correction keeps incomplete data frames in `state.held` but returns complete
ping and pong frames directly through `output` at
[`runtime:586`](../../scripts/test-env/client-browser-library-changed-reference-runtime.mjs#L586).
A valid close first passes the existing frame-length, code, and UTF-8 checks,
then marks the direction closed and clears `held`, `parts`, `messageBytes`, and
`opcode` before returning the original close frame. Unapproved data is neither
returned nor passed to the message callback. Invalid close payloads still reject
before the closed-state change. Subsequent continuation frames after closure
remain rejected by the existing frame admission guard.

The complete-data path still calls `messageJSON` and, for the client direction,
`requireReferenceClientMessage` before incrementing the message count, invoking
the message callback, or releasing held data frames. An interleaved permitted
control frame therefore does not approve a fragmented forbidden application
message. The static diff review found no bypass of the existing command checks.
The correction follows the control-frame requirement discussed in
[RFC 6455 section 5.4](https://www.rfc-editor.org/rfc/rfc6455#section-5.4), which
the root task consulted from the official RFC text.

| New frozen artifact | SHA256 |
| --- | --- |
| TOOL05 runtime | `209f72fa812647742003b50c311ee358ffc6f1279e29a2e142eec506245fed7c` |
| TOOL05 runtime guard | `af867a2daf3f957e7a2c8d93a1f7ead2be42290ece4568c2f2181ed352dd849b` |
| [Successful verification 05b](reference-library-changed-ui-runtime-verification-05b.json) | `c4cd6e02b647da9e322411e79f46d2ce238a437187e542fa28751c73f2768189` |
| [Retained initial verification 05](reference-library-changed-ui-runtime-verification-05.json) | `5f6fdd872c37cca7878f821a8379413c6a0452835debf93c6a5a61ae177d3c70` |

TOOL05 is `reference-library-changed-ui-runtime-tool-05` under the existing work
root. The root task reported that all four new cases fail against the old
`b720d7bb...` copy: Node's name-filtered execution reported exactly four tests,
four failures, and no skipped tests. The initial wrapper incorrectly expected a
32-test total for that filtered negative run and stopped; verification 05 retains
that failed wrapper result. Verification 05b reused the already produced negative
regression log without rerunning it, then ran the corrected full suite for the
first time: 32 tests passed. This review inspected the diff and guard assertions;
it did not execute or independently repeat those remote runs.

The four added cases at
[`runtime guards:394`](../../scripts/test-env/test-client-browser-library-changed-reference-runtime.mjs#L394)
cover both directions with the appropriate masking, interleaved ping and pong,
a control frame split across input chunks, forbidden-message non-release,
valid-close abandonment of pending data, and invalid-close rejection. The split
case uses the boundary before the ping's last byte. These are meaningful parser
regressions, not native-browser callback or end-to-end HTTP transparency tests.

## Active controls and DOM sampling

These tools include active network controls. Both configure headless Chromium,
the same three launch flags, a forward proxy, a fresh context, and
`serviceWorkers: 'allow'` at
[`reference runtime:1413`](../../scripts/test-env/client-browser-library-changed-reference-runtime.mjs#L1413)
and [`cross-user:1809`](../../scripts/test-env/client-browser-cross-user.mjs#L1809).
The context route handlers continue admitted HTTP and abort denied HTTP at
reference lines 1431-1448 and Goby lines 1816-1831. Proxy admission, buffering,
write callbacks, and synchronous observation also add timing work. The fixed
policies and launch configuration are shared test conditions; this review does
not establish whether any affects an unknown application's refresh decisions.

Reference `fail` and `notify` at runtime lines 733-756 can poison the actor and
destroy sockets after an observation failure. The actual reference driver passes
`observer: {}` at
[`reference driver:887`](../../scripts/test-env/client-browser-library-changed-reference.mjs#L887),
so there is no custom `onBrowserMessage` dispatcher callback in that run.
Goby records observer errors and can take the context offline after identity
loss at cross-user lines 1596-1602. These explicit failure paths can affect
traffic, but they are not silent equivalents of a normal completed observation.
They must be checked through their recorded failure/closure evidence before
being proposed as the explanation for any particular run.

The two DOM samplers are
[`observeReferenceDOM:439`](../../scripts/test-env/client-browser-library-changed-reference.mjs#L439)
and [`observeLibraryChangedDOM:994`](../../scripts/test-env/client-browser-library-changed-source55.mjs#L994).
They inspect elements, styles, geometry, text, identity attributes, and media
state. No explicit DOM, focus, selection, scroll, location, or application-state
write was found. `Range.selectNodeContents` operates on a newly created Range;
the code does not add that Range to the window Selection. No sampler-triggered
click, reload, media playback, or synthetic client event was found.

Geometry and style reads can require synchronous layout, and repeated text walks
consume page execution time. The approximately 500 ms sampling cadence is
therefore not a zero-timing-impact proof. Reference capture also waits for
transport settling and pin checks before sampling. The shared timeout pattern
uses `Promise.race` and does not cancel an already-started page evaluation after
a timeout. An evaluation can finish after its caller rejects. Those are bounded
future noninterference cases to test; no evidence connects them to the sealed
zero-HTTP result.

## What the existing guards do and do not establish

The following statements describe inspected guard assertions. Previous reports
record successful remote guard runs; no guard was run again for this review.

| Existing guard | Established assertion boundary | Missing browser behavior |
| --- | --- | --- |
| [`reference runtime tests:375`](../../scripts/test-env/test-client-browser-library-changed-reference-runtime.mjs#L375) | Original complete/fragmented text bytes and projected payload are retained; BOM remains observable. | Native page callback and subsequent HTTP are not involved. |
| [`reference runtime tests:479`](../../scripts/test-env/test-client-browser-library-changed-reference-runtime.mjs#L479) and [`491`](../../scripts/test-env/test-client-browser-library-changed-reference-runtime.mjs#L491) | Exact allowed subscription bytes pass; malformed/forbidden frames fail; an independent ping passes. | These older cases do not exercise interleaving; the separate TOOL05 regressions now cover that parser behavior. |
| [`new reference runtime tests:394`](../../scripts/test-env/test-client-browser-library-changed-reference-runtime.mjs#L394) through `455` | Both-direction control interleaving, split ping, forbidden data, valid close, and invalid close are asserted; the root's remote 05b suite passed 32/32. | No real browser callback, fetch, or DOM transition is exercised. |
| [`cross-user tests:2092`](../../scripts/test-env/test-client-browser-cross-user.mjs#L2092) | Fragmented message bytes, interleaved ping, borrowed-copy erasure, and original wire preservation are checked. | No real Chromium `MessageEvent` callback. |
| [`cross-user tests:2109`](../../scripts/test-env/test-client-browser-cross-user.mjs#L2109) through `2145` | Fake-socket final-write delivery, write failure, handshake-head ordering, and observer failure are checked. | Real page scheduling and listener semantics remain outside the test. |
| [`cross-user tests:2168`](../../scripts/test-env/test-client-browser-cross-user.mjs#L2168) | EventEmitter socket/document/close evidence binding is checked. | This case does not emit `framereceived` or run a page handler. |
| [`cross-user tests:2209`](../../scripts/test-env/test-client-browser-cross-user.mjs#L2209) and `2231` | Source55 retains wire capture and scope selection with fake sockets/manual calls. | The tests do not open the complete source55 actor. |
| [`reference DOM cases:500`](../../scripts/test-env/test-client-browser-library-changed-reference.mjs#L500) and [`source55 DOM cases:92`](../../scripts/test-env/test-client-browser-library-changed-source55-dom.mjs#L92) | Authored HTML covers geometry, identity, duplicate/orphan cards, and visible titles using production samplers. | These static cases do not run native application message handlers, repeated sampling, callback-triggered HTTP, or noninterference assertions. Source55 fixture CSP disables script. |

Passing these guards supports their stated byte/capture/DOM properties. It does
not prove handler invocation, native listener receiver identity, unrestricted
client behavior, or complete runtime transparency.

## Follow-up verification plan and current coverage

The original follow-up plan uses only newly authored HTML and a synthetic server
in a fresh isolated remote scope. It must not use the reference application,
real credentials, consumed execution roots, or existing service listeners. If
the unchanged production transports require fixed loopback origins, provide
those origins inside a separate network namespace with synthetic endpoints.
Do not silently change the frozen production scope constants to obtain a pass.

1. Use one scripted page whose native `WebSocket` installs both a function
   `onmessage` and an `addEventListener('message', function ...)` callback. Record
   callback counts, order, payload type/value, and synchronous `this`, `target`,
   and `currentTarget` identities. Have callbacks issue uniquely identified
   same-origin GETs and update visible target/anchor elements from real synthetic
   responses. The synthetic server must independently record each GET.
2. Run that same fixture directly, through the reference production transport,
   and through the Goby production transport. Exercise the actual browser
   observer attachment path. Compare native callback records, physical requests,
   browser requests, response bodies, and resulting DOM. Include an early
   handshake-head message and a later ordinary text message to separate binding
   order from steady-state dispatch. This proves a harness property only.
3. Repeat the ordinary message with the production DOM samplers active at their
   normal cadence. The owned page should record MutationObserver results,
   focus/blur, selection, scroll, and a bounded timer heartbeat. Assert no
   sampler-attributable state change, callback loss, or HTTP loss relative to the
   unsampled fixture. Treat latency as measured evidence, not an exact-equality
   requirement.
4. If browser-level control-frame transparency is needed, add one narrow
   fragmented-message case with an interleaved ping and a server
   that waits for its pong before releasing the final fragment. This isolates
   the corrected reference control-frame path. The returned-frame pure guards
   are already covered by TOOL05 and do not need repetition merely for this
   review. Keep any browser result separate from the already delivered
   LibraryChanged cases.

The subsequent [attempt 02](library-changed-browser-transparency-02-summary.json)
completed the steady-state callback, real HTTP, visible transition, reference
sampler, and resource-closure controls in four cases. Focus, selection, scroll,
and timer counts are retained as diagnostics rather than assumed to be zero.
Early handshake-head delivery, browser-level fragmented-message/ping ordering,
and universal page-state noninterference remain outside that result. The
completed functional control does not establish reference-client refresh
behavior, Goby LibraryChanged acceptance, or deployment admission.
