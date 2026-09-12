# Reference NextUp preparation 04 and bounded UserData recovery

Preparation 04 created its owned fixture and passed the Guid-based own-token
baseline checks, then stopped during the first partial playback calibration.
The direct episode response added `UserData.PlayedPercentage=20`, which the
frozen four-field playback-change guard rejected before any DELETE. The
original preparation terminal remains `recovery_required`; no complete
calibration set, matrix fixture release, or client acceptance is established.

A separate bounded recovery has been independently accepted after its
six-request sequence. The actual raw receipts prove exact zero-state
restoration and closure of its new token. The original failed preparation
record remains unchanged. The full parent scope has also completed independent
seal-03 verification, including exact source replay of all 172 requests.

## Frozen tooling and prior verification

Preparation used
`/opt/goby-test/exec-work-m3e/nextup-global-preparation-tool-08/revision-02/prepare-nextup-global-reference.py`,
SHA-256 `347d71f310182e3258dc2f0b51142dbf31088292ee6ec5d3689621971deb152a`.
That source passed 154 remote guards and two compilation checks. Its completed
Guid-release evidence check reconstructed 109 retained request/response pairs
and 86 snapshot GETs, with no new business HTTP or process probes.

The paired matrix operator in `nextup-global-reference-operator-tool-03`
has source SHA-256
`3a07ff2fdd6817a38e98a205d3b44fb0f1231d4d1b58d785cf6b1cc3eba3dd8a`
and passed 65 remote guards plus two compilation checks. These tooling results
do not establish successful execution of preparation 04 or its matrix.

| Retained tooling report | Recorded result | SHA-256 |
| --- | --- | --- |
| [Initial preparation verification](nextup-global-preparation-verification-05-initial.json) | 154 tests, one error; preserved historical attempt | `f69c3c020a7468c02960d32b8b087f25e5aa095896615286c1fb29c73e582091` |
| [Final preparation verification](nextup-global-preparation-verification-05.json) | 154 passed, no failures/errors/skips | `8808952ebde9c9ab1f8ee64671df20fed3e66abdf676ba06dde2ca05364b1f77` |
| [Actual Guid-release reconstruction](nextup-global-preparation-real-release-verification-01.json) | 109 raw pairs, 86 snapshot GETs, two closed tokens verified | `aa648c170bc01132cd02d6f5d995d080bca933cfb4d23a369851c8767bd53c38` |
| [Operator verification](nextup-global-reference-operator-verification-03.json) | 65 passed, no failures/errors/skips or real I/O attempts | `9974c4816ad5e680b7f435082b4590261545c226eaa825e9bed86234b6553a69` |

## Actual preparation outcome

The consumed output root is
`/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-04`;
the execution evidence root is
`/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-execution-04`.

The actual ledger contains 172 requests: 115 normal requests and 57 cleanup
requests. It records two newly created ordinary accounts, two TV libraries,
and six independently copied media files. The own-token baseline checks
using the measured Guid folder grants passed.

Requests 0111 through 0114 completed the first partial playback negotiation,
reports, and acknowledged STOP. Request 0115 read the direct full episode
before the planned DELETE. Its `UserData` added `PlayedPercentage` in addition
to the permitted playback-history changes. The frozen guard allowed changes
only to `Played`, `PlayCount`, `PlaybackPositionTicks`, and `LastPlayedDate`;
the extra percentage field therefore stopped normal calibration.

There were zero DELETE requests and zero completed calibrations in the parent
run. The retained `plays.P.stopped` value is true. All three parent tokens
were closed with logout HTTP 204 and exact-token HTTP 401 proofs, but those
closures do not establish restoration of the changed episode UserData.

The outstanding direct state was actor P from preparation 04, episode A1,
item ID `125`:

| Direct `UserData` field | Observed value before independent recovery |
| --- | --- |
| `Played` | `false` |
| `PlayCount` | `1` |
| `PlaybackPositionTicks` | `1200000000` |
| `IsFavorite` | `false` |
| `PlayedPercentage` | `20` |
| `LastPlayedDate` | `2026-09-12T20:52:59.0000000Z` |

The authoritative episode runtime was `6000000000` ticks, so the observed
percentage agrees with `1200000000 / 6000000000 * 100 = 20`. This explains the
new field; it does not authorize ignoring arbitrary UserData changes or
normalizing a later response into an invented zero state. Catalog projections
are not substituted for the direct full episode UserData witness.

| Retained parent evidence | SHA-256 |
| --- | --- |
| `reference-nextup-global-preparation-04/private/terminal.json` | `a770fff3346b64657b9338b8a1f985b03fe5208464f8f236c4e7c1649da8cb8e` |
| `reference-nextup-global-preparation-04/private/state.json` | `4bc7ff2286d0cbdd61735eee2a18fd30db752da080206cbcc91aec4d6b7380b7` |
| `reference-nextup-global-preparation-execution-04/preservation-after.json` | `3e600756381ece032bbe8b20ca7a0df2f5cf2e17c657322ca92ec14f3b021077` |

These relative paths use `/opt/goby-test/exec-work-m3e` as their base. Private
state, raw responses, tokens, and credentials remain on the remote host.
The parent after-preservation comparison retained 148 old roots and the
complete Goby fixture state: 83 sessions, 70 devices, and 185 activity entries.
That preservation result does not negate the separately observed reference
episode mutation.

## Independently accepted UserData recovery

The independent recovery input is
`nextup-preparation04-userdata-recovery-inputs-01/input.json`, SHA-256
`73ebe46ffebfe2731f8f6be61ad006f5306568d59f3435ae58c04bbc695df0df`.
Its worker is
`nextup-preparation04-userdata-recovery-tool-01/revision-01/recover-preparation04-userdata-01.py`,
SHA-256 `0fba1ac06517aac5a5b3e7977df564839439b1523c7559ab44be18fe408a81c4`.
All paths in this section are relative to `/opt/goby-test/exec-work-m3e`.

The worker passed 24 guards, two compilation checks, and a plan check against
the actual private input with zero business HTTP. Its execution was gated on
a fresh before-preservation capture covering 152 retained roots. The plan
allows at most six requests and one DELETE: new owned authentication, direct
full precondition read, the one conditional DELETE, direct full restoration
read, logout, and same-token rejection. DELETE is eligible only when the
current full item matches the exact retained partial witness.

Independent raw-receipt verification confirmed all six completed requests,
comprising four normal requests and two closure requests, with exactly one
DELETE acknowledged by HTTP 200. The direct full post-DELETE response matches
the original complete four-key zero UserData, with `PlayedPercentage` and
`LastPlayedDate` absent. The same new token returned logout HTTP 204 followed
by HTTP 401. The worker input and run identity were also verified; this was
not inferred from a catalog projection or the DELETE acknowledgment alone.

The recovery unit completed with exit status 0 and MainPID 0, retaining
ExecMainPID `1513281` and invocation
`f047e7fe5af1499a8cbb3e45940a812c`. Independent verification confirmed that
the original worker PID was absent and its cgroup was recursively empty.

The [independent recovery terminal](nextup-preparation04-userdata-recovery-independent-terminal.json)
has SHA-256 `0c2380b1fb0ee0f78b2e6fb8ad21995b779cfaad26c9ef318af93215121ce312`
and status `owned_userdata_restored_and_exact_token_closed`. The independent
audit issued zero business HTTP requests. Its original remote location is
`reference-nextup-preparation04-userdata-recovery-execution-01/independent-terminal.json`.

The recovery after-preservation comparison has completed independently:
`reference-nextup-preparation04-userdata-recovery-execution-01/preservation-after.json`
has SHA-256 `7ba5770390db0fcebc16cd2c4d26341d1629bbf5882048ce481726df82f1b3dd`.
All 152 retained roots, service identities, and main files matched the before
capture; the complete Goby fixture remained at 83 sessions, 70 devices, and
185 activity entries.

The existing proxy was reused unchanged. This document makes no assertion
that its internal reads of original implementation bytes were zero.

## Completed independent parent seal

The full preparation-04 seal is complete in
`/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-seal-03`.
The [independent parent terminal](nextup-global-reference-preparation-04-independent-terminal.json)
has SHA-256
`33f40ce85fc0934bc88a12c4e34cbf4cd8a8b9a682e446d9b1e1feb85e28841a`
and status `failed_preparation_independently_sealed_with_separate_recovery`.

The audit executed the exact frozen producer source against all 172 retained
attempts, with their actual requests, reserved states, raw responses, payloads,
token contexts, and timestamps. The historical output-existence gate was
substituted in memory; verified media receipts and an in-memory journal avoided
new fixture writes, and the replay transport returned only the indexed recorded
responses. Actual source/input/release, lock, and process authority checks were
retained. No data, status, error obligation, or report was normalized.

All **526 private files and 173 export files matched byte for byte**, including
the final state, terminal, and preservation reports. There were zero byte
differences. The source reproduced 115 normal and 57 cleanup requests, zero
parent DELETEs, four playback requests, zero completed calibrations, and all
three exact token closures. It reproduced the percentage-field rejection in
both calibration and cleanup. The parent terminal remains `recovery_required`
with `cleanupComplete=false`; independent recovery is a separately linked fact.

| Seal-03 evidence, relative to its root | SHA-256 |
| --- | --- |
| `seal-01.py` | `bbdc47ed398b96cafdbe109204b3dee970569a2d027ddec013609b5e19849943` |
| `exact-replay-comparison.json` | `106aa6a144d3b9ce37d086d82b7f98582e648b43f6c22f910f8daaa62db4e056` |
| `public-reconstruction.json` | `6f8c8e13bf4e443c1da474baff40efd37b9c13c8e94e30b9dc5dc12ccf369f93` |
| `completed-scope-files.json` | `d76b6ef06d69f91d824aa02ae140f638b2e536372d240e99f8359ef885947708` |
| `preservation-after.json` | `cf631148519136bcdbd20238532dab99cf3943f344cd90aa6747686045c252fe` |

The parent unit retained invocation `fec8b8d4fc054a8fa5ecb63b4c92d8ac`,
exit status 2, MainPID 0, and former PID `1511969`. The accepted recovery unit
retained its recorded invocation and exit status 0. Both former PIDs were absent
and both exact service cgroups were empty. The seal additionally reconciled the
recovery's six raw requests, one DELETE, exact full zero restoration, and new
token closure against the accepted independent recovery terminal.

Fresh before/after preservation captures matched all **154 roots**: the previous
152 protected roots plus the accepted recovery output and execution roots.
The prior 148-root preparation preservation remained intact. Service identities,
main files, and the complete approved Goby fixture matched, including the
historical v7 state except its capture timestamp and counts of 83 sessions,
70 devices, and 185 activity entries. The seal issued zero business HTTP and
read no reference database or original implementation bytes.

The parent cleanup snapshot recorded ten users, twelve libraries, and 96 devices
at that time. The seal does not turn it into a new complete reference baseline
after recovery, and establishes no matrix fixture release or client acceptance.

## Required next evidence

Preparation 04 and the independent recovery attempt are consumed scopes.
Neither authorizes additional business HTTP or rewriting its original outcome.
The full preparation-04 seal is complete. A fresh observer and release remain
required for the changed retained environment.

That fresh baseline must capture the retained population of ten users and
twelve libraries, the recovery-created device, P's new authentication dates,
and the restored direct UserData. The complete current reference device
population has not yet been recaptured, so no device total is inferred here.
The old eight-user/ten-library Guid baseline cannot be reused to start
preparation 05 directly.

The [revised playback contract](nextup-playback-percentage-contract.md) now
validates the observed episode percentage against the actual full DTO/runtime
while retaining unrelated fields and exact zero-state equality. Its four
remote suites passed 379 guards and eight compile checks. This development
bundle still uses the older release-v2 population contract, so it cannot start
a fresh actual preparation until the new observer and release are accepted.
Complete four-calibration evidence, matrix execution and actual client
acceptance remain outstanding.

The accepted episode recovery establishes only that scoped restoration and
token closure. It does not publish a NextUp matrix fixture, complete the four
required calibrations, or establish client acceptance. The parent preparation
terminal remains `recovery_required`.
