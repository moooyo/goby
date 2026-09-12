# Reference baseline observer 01 and independent recovery 01

The first real baseline observer completed one administrator login and then
stopped before any public-state GET or cleanup request. Its original state
and terminal remain `recovery_required`. A separate, independently attested
recovery subsequently observed Devices and closed that exact original token.
It did not produce a complete reference baseline or release a NextUp fixture.

## Original observer outcome

Run `nextup-global-reference-baseline-20260913-01` used observer source
`89bc2841a3217cecbdb1815bb74ebef6e79f5e3ad951ed2dbd5d755f1d0fb7ee`.
The input assembly, 34-attempt plan, and read-only authority admission preceded
the actual execution. The actual execution recorded one complete HTTP 200
login response; it made zero snapshot reads and zero cleanup requests.

The frozen transport returned ordered response-header tuples. The observer
passed those in-memory tuples to a decoder requiring JSON-style list pairs.
That rejected the response after its raw receipt and unresolved ownership
state had been persisted. JSON serialization had already converted the
saved tuples to lists, so rereading the saved response alone did not
reproduce the original in-memory failure. Independent inspection confirmed
the complete response and the acknowledged administrator, server, session,
and device before authorizing the separate recovery.

The consumed original records are retained unchanged:

| Private original record | SHA-256 |
| --- | --- |
| Observer state | `3f4df82c27a851973f5578eb19a7ecf164d0840c000e23bb237fd6ab0382729b` |
| Observer private terminal | `6078ce6918bef052b478f958f2fc49e054f6a74704c4322f058098d47244b9a7` |
| Complete raw login response | `3b778da8a61a5e538cd1108e0bffe12739b8ff3b1a42b0291214347edc79ee0f` |

These private records, raw requests/responses, and credentials are not
copied into the repository. The public observer terminal below reports
`requestCount=1`, `cleanupRequestCount=0`, `completeSnapshotObserved=false`,
`cleanupComplete=false`, `uncertain=true`, and `baselineReleased=false`.

## Independent exact-token recovery

Run `nextup-global-reference-baseline-recovery-20260913-01` used the original
login's exact token and identical client/device authorization metadata.
It performed exactly three requests, with no new login:

1. GET `/emby/Devices` returned HTTP 200 with 87 actual device rows.
2. POST `/emby/Sessions/Logout` returned HTTP 204.
3. GET `/emby/Sessions` using that same token returned HTTP 401.

The new observer device's actual internal ID was 88 and matched the original
login's `SessionInfo.InternalDeviceId`. The 87-row registry retained the
original 85 devices, the preparation-02 device, and this one observer device.
No device row was synthesized or deleted. The independent recovery summary
records `exactTokenClosed=true` and
`status=observer_login_independently_recovered_and_closed`; that disposition
does not change the original failed observer's terminal.

| Scoped unit | Invocation ID | Terminal result |
| --- | --- | --- |
| `goby-nextup-global-reference-baseline-01.service` | `110a814f22ff4d4b83df233b14c22b49` | Failed, exit status 2, MainPID 0 |
| `goby-nextup-global-reference-baseline-recovery-01.service` | `19802d574a4f4685bd5d7bd7ebac63ef` | Active/exited, exit status 0, MainPID 0 |

The independent closure confirmed both original process IDs absent and both
cgroups recursively empty. The recovery terminal capture was
`2026-09-12T18:57:21.797216+00:00`.

The full independent recovery terminal and complete inventory remain private
on `test-env`:

| Private recovery evidence | SHA-256 |
| --- | --- |
| `reference-nextup-global-baseline-recovery-execution-01/independent-terminal.json` | `41392c5ff86192dce4e90eee8026b188aa6def14cf90a92aa0920edb4c8eb8f1` |
| `reference-nextup-global-baseline-recovery-execution-01/completed-scope-files.json` | `cce8eb5bf809282d7111525231989573883c7e7f533d97099a866f05e3dc3eab` |

Both paths are relative to `/opt/goby-test/exec-work-m3e`. Only the designated
safe independent summary is included below.

## Preservation and remaining observation

The after-preservation capture occurred after the independent recovery.
The 117 retained owned roots were preserved, and the Goby fixture's complete
preservation comparison remained equal with 83 sessions, 70 devices, and
185 activity entries. These counts describe the preserved Goby fixture;
the authentication activity above belongs to the separate reference server.

Neither attempt created a library, a P/Q account, or playback state, and
neither mutated metadata. The six owned media copies from preparation 02
remain retained. System/Configuration and the complete reference baseline
were not captured by observer 01 or its three-request recovery.

The next step is a new schema-version-two observer scope admitting this
independently closed recovery record. Its complete observation remains
bounded to 34 attempts and must actually observe 88 device rows: the 87
retained predecessor rows plus its own new device. It must obtain all 31
snapshot GETs, including System/Configuration, then prove its own logout
and same-token rejection. The [observer contract](nextup-global-baseline-observer.md)
defines that subsequent admission and independent-attestation requirement.

## Byte-exact safe report copies

All eight files below were copied only from the explicitly designated safe
remote reports. Their local SHA-256 values match the source bytes. The two
preservation `.stdout` files contain JSON and are retained verbatim under
`.json` names. No test or business HTTP request was performed while copying
and recording these artifacts.

| Local safe report | SHA-256 |
| --- | --- |
| [Observer terminal](nextup-global-baseline-observer-01-terminal.json) | `964aec4e849a7e0e55a851f0ffbabacddc59706863632767e80e597af0c95b75` |
| [Input assembly](nextup-global-baseline-observer-01-input-assembly.json) | `5267bed8551d36fdb35a1a882ba2bc49703d9b5881e10fe5a97c6f15d376811d` |
| [Frozen plan](nextup-global-baseline-observer-01-plan.json) | `c4da06685e7fc90d1c5bf6e76a8bbccde35980c450e3f1e9b50924dda1e3fbab` |
| [Read-only admission](nextup-global-baseline-observer-01-admission.json) | `ebaffb31ec4184761a4ce475501b1344c3cb5baf6e6b79ea5d6648fc185deb38` |
| [Before-preservation summary](nextup-global-baseline-observer-01-preservation-before.json) | `b7a616ec31e78c5454bf1c79640cb76c818e43d22f79c6b14dca2563d9a3db43` |
| [After-preservation summary](nextup-global-baseline-observer-01-preservation-after.json) | `a11a35ff52dfa5790d30d0c8be946c0169f8498645f39532451ea7687a16fe1b` |
| [Recovery terminal](nextup-global-baseline-observer-recovery-01-terminal.json) | `c33802ad7b189ea21b5047d55cb5fb4266eaaa8dd27ad540e19837861c18b374` |
| [Independent recovery summary](nextup-global-baseline-observer-recovery-01-independent-summary.json) | `072424ca9ec11e96ed8a163c5457cade8919301eff83d7958993f3d7eb880f75` |
