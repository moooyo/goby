# Client02 Suggestions continuation

Client01 completed its zero-history Home and default TV Shows journey in
10,586 milliseconds with 34 recorder requests, zero playback, complete token
cleanup, and preserved owned state. It made no physical `Shows/NextUp` request.
Its actual LA/LB DOM records each contain one visible Suggestions button beside
Shows. This is an unobserved global request, not an empty global response.

Client02 continues that single bounded discovery through the observed controls:
Home, LA TV, LA Suggestions, Home, LB TV, LB Suggestions. It clicks exactly one
visible Suggestions button in each owned library. It does not construct an API
query, modify client flags, negotiate playback, or play media.

The original client01 source, tool directory, output, and receipts remain
immutable. New source is `scripts/test-env/client-browser-nextup-discovery-02.mjs`
with its own pure guard file. The dedicated browser runtime and session proof
are copied byte-for-byte from client01. All execution and verification remain
remote through `ssh test-env`.

## Exact changes to the client01 input

Use the [client01 contract](nextup-client-discovery-input.md), with these exact
changes and no further scope extension:

- `runId`: `nextup-reference-client-discovery-02`.
- `root`: `W/reference-nextup-client-discovery-02`, precreated empty, mode 0700.
- Input: `W/nextup-client-discovery-inputs-02/input.json`.
- Source directory: `W/nextup-client-discovery-tool-02/revision-01`.
- Driver/source-closure key: `client-browser-nextup-discovery-02.mjs`.
- Other source-closure keys remain `client-browser-nextup-discovery-runtime.mjs`
  and `client-browser-session-proof.mjs`, under the new source directory.
- Execution root: `W/reference-nextup-client-discovery-execution-02`.
- Unit: `goby-nextup-client-discovery-02.service`.
- State-observer device IDs use `goby-nextup-client-discovery-02-before` and
  `goby-nextup-client-discovery-02-after`; current existing device IDs include
  every acknowledged client01 recorder/browser device.
- `budgets.maximumSeconds`: 1189; `normalSeconds`: 829;
  `browserNavigationActions`: 5. All other budget fields remain unchanged,
  including zero permitted playback attempts and 34 recorder requests.

`W` is `/opt/goby-test/exec-work-m3e`. Add this private `continuation` object:

```json
{
  "priorReport": {
    "path": "/opt/goby-test/exec-work-m3e/reference-nextup-client-discovery-01/private/report.json",
    "sha256": "EXACT_CLIENT01_PRIVATE_REPORT_SHA256"
  },
  "priorIndependentTerminal": {
    "path": "EXACT_CLIENT01_INDEPENDENT_TERMINAL_PATH",
    "sha256": "EXACT_CLIENT01_INDEPENDENT_TERMINAL_SHA256"
  },
  "priorElapsedMilliseconds": 10586,
  "totalMaximumSeconds": 1200
}
```

The driver validates the prior report's exact bytes, run ID, outcome, zero
playback, 34 requests, preserved state, and closed recorder/browser tokens.
The controller validates the independent terminal's semantics; the driver
also binds its exact bytes. The cumulative allowance is not reset:
`floor((1200000 - 10586) / 1000) = 1189` seconds. The final 360 seconds remain
reserved for cleanup. All runtime deadline checks derive from the validated
input, and the final report records both runs' summed elapsed milliseconds.

## Suggestions evidence

For each library, first verify the actual public Views identity and visible TV
destination. Require exactly one visible, enabled button whose exact text is
Suggestions. Preserve only visible-control metadata: text, tag, role, class,
selection/accessibility attributes, tab index, data index, and bounding box;
never read event-handler attributes or vendor source.

Before clicking, journal the action ordinal, library/view ID, phase, document,
route, actual Shows/Suggestions state, and transport sequence. After the one
click, wait for the response inside a fixed 20-second actor-clock window. Record the selection marker or a
transferred active/selected/current class, actual completed global NextUp
responses, physical/frame IDs and matching decoded body hashes, screenshot,
DOM facts, and whether the request remained unobserved. Do not infer a query
from the Suggestions label. Completed requests outside that window stay in the
report with `within_suggestions_window=false` and cannot satisfy the discovery
outcome. A later bounded screenshot/transport drain does not extend the
response window. Missing response evidence remains explicit.

The same complete pre/post state reads, original UI logout, exact-token 401
proof, logout-view DOM/screenshot, resource closure, private raw receipts, and
sanitized export rules remain in force. This remains zero-history parameter
discovery; it does not compare the client's behavior with watched matrix
R1-R4 or establish real playback and refresh acceptance.

## Remote verification

The exact source passed five remote `node --check` invocations and all 22 pure
guards with no failures or skips. The [verification receipt](nextup-client-discovery-verification-03.json)
binds the source hashes and Node executable. The frozen guard artifacts and
source copies are under `W/nextup-client-discovery-verification-03`; this
verification started no browser and made no business HTTP request.

The driver SHA-256 is
`aa69812f5cea621fd8544049eeb50b9374f3ff6e809c1bcd8338d5aaf6dfe23d`.
The unchanged runtime SHA-256 remains
`bfce485955fece8beb9a0ebde500b649a349a06b26d1288e6cb73948d48c2a8d`.
The guard TAP SHA-256 is
`636ca9fbf7e3025ecd59b301fd45de8a6125c60df07d41b89cf6e1414f7a7c6d`.
