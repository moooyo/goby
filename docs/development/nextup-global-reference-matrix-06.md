# Reference NextUp matrix 06 target identity

Checkpoint: `2026-09-12T23:44:52Z`. Matrix06 passed the actual restricted
operator admission and replayed all 269 preparation requests. Transport
initialization then rejected the target process metadata before creating the
matrix journal or sending business HTTP. The original operator commit remains
`recovery_required`; it is not rewritten into success. All 176 protected roots
and complete Goby state match before/after captures. No reference matrix or
client acceptance is established.

## Verified observer repair

The new operator preserves exact observer ExecStart equality and additionally
accepts only the observed reset suffix when the original completed-command
suffix binds the verified former PID. Its command prefix, all other unit
properties, empty cgroup and absent former PID remain mandatory.

| Verified source or record | SHA-256 |
| --- | --- |
| Operator, `nextup-global-reference-operator-tool-04/revision-01/run-nextup-global-reference.py` | `e392591224e909d7f48b6af731d2808bde8641674a67f48dbd305464eca060e2` |
| Operator guards | `ab34252d4206cf410b89d7181a9d344cbdc6eb8c583686956eb525c828eaa36d` |
| [80 passing guards](nextup-global-reference-operator-verification-04.json) | `2c0987704504e0bfc33cc776b77eab14eda2239dda7f117c2302b5cf74f570d4` |
| [Two compile checks](nextup-global-reference-operator-compile-04.json) | `7d14cf4d1d1f736ce0160ff6d80fb7e1c782454c35825c7f944a8c058f20b119` |
| [Verification summary](nextup-global-reference-operator-summary-04.json) | `6ea1d9f3fd7ec97d147718da0c80b6379a718d48d45dc0a469567abde33c1014` |
| [Actual read-only preparation replay](nextup-global-reference-operator-04-actual-replay.json) | `952f0e68d90927884dacec9b5dea30bff0a1c3b58e1415624728c5772dbeea5b` |

The separate diagnostic reproduced all 269 requests and all 829 authoritative
private files byte-for-byte, including the exact private filename set. Media
checks also passed. It does not claim complete export-file replay. It then
rejected its real diagnostic process at the runtime gate, as required; it did
not substitute a simulated runtime identity for a live admission.

## Actual unit and outcome

Paths use `/opt/goby-test/exec-work-m3e` as `W`.
The new execution root is `W/reference-nextup-global-matrix-execution-06`;
the operator output is `W/nextup-global-reference-operator-runs-06/operator-06`.
The matrix output bound by the immutable preparation05 draft remains
`W/nextup-global-reference-runs-05/matrix-05`. It is still absent and has never
been consumed by a matrix run. Preparation05 was not rerun.

| Actual record | Value or SHA-256 |
| --- | --- |
| Unit | `goby-nextup-global-reference-matrix-06.service` |
| Invocation | `6e9846ad7a6a4c9987f55e5c9aab5441` |
| Former PID / exit | `1680741` / `2` |
| Attestation, `reference-nextup-global-matrix-attestation-06/attestation.json` | `a16f833b942cbd0f7020a9eb2350ec86b7b7ffa3dac57831f0763799c6317de7` |
| [Assembly](nextup-global-reference-matrix-06-assembly.json) | `570caff12db5bb27ac6263e6686a2869ea1ccc53056c8914d9dcc7e75d037252` |
| [Live operator admission](nextup-global-reference-matrix-06-admission.json) | `b4545edd44ca0c5f3ebb2beba0a0825755727a53b9e89e775157152ba0d72e20` |
| [Original operator commit](nextup-global-reference-matrix-06-operator-commit.json) | `45eca7afa6fdbbbb6b5075bb46008822123d8304a43241e7c6fe4f1b99fa5825` |
| [Original export terminal](nextup-global-reference-matrix-06-operator-terminal.json) | `d9ccbdf387ffbf17dcb4f7f8209b12457237c68e502e3949129023a6471bd149` |
| [Independent target diagnostic](nextup-global-reference-matrix-06-diagnostic.json) | `76d5b9413f28562439015fa0c81ad8405eaac6c012b84f84666d0a122e5eb5c0` |

The retained terminal has `status=awaiting_operator_commit` and
`completionCommitted=false` by design. The durable commit, whose terminal
hashes bind those exact files, records `recovery_required`. The failure is
`TransportError: The bound target process or namespace changed.` and the
transport result is null. The unit exited2/MainPID0, with absent former PID
and cgroup. Source-bound initialization order, the missing matrix root and
the no-deletion journal implementation prove zero business HTTP from this
invocation; this is not a packet capture or reference database comparison.

Before preservation SHA-256 is
`ab4d775cb938cc3d7cb97ee3f7d9a045b82a6a30f92dffcf148cd726da80fe51`;
after is `03fb3fd3eb30523964e605e3cdcca67dcffd5532a86a726edac41a21a4851610`.
All 176 roots, old service/main-file records and complete Goby state at
83 sessions, 70 devices and 185 activity entries agree.

## Exact metadata difference and next action

Only the proxy endpoint's `exe` observation changed:

| Fact | Expected or original object | Current observation |
| --- | --- | --- |
| Proxy executable display path | `/usr/bin/python3.13` | `/usr/bin/python3.13 (deleted)` |
| Running executable object | device `2049`, inode `397` | same device/inode, regular file, link count `0`, 6,828,688 bytes |
| Object currently at `/usr/bin/python3.13` | Not the running object | device `2049`, inode `368`, link count `1`, 6,832,784 bytes |

PID334022, start ticks, boot ID, UID, command line, namespaces, cgroups and
the owned listener remain equal to the declaration. Application metadata
also remains exact. The existing proxy source retains SHA-256
`256017d62eb98b39a1e026516e6c29ced0a3f599fd961a3c9e0ee9b4d878d99b`.
The diagnostic reads metadata only and does not claim that unread executable
bytes are identical. It does not silently ignore the deleted annotation.

The next operator revision will need an explicit, versioned identity rule:
retain raw observations and metadata-only file-object proof, permit only the
exact endpoint display suffix with unchanged bound identity, and durably bind
the proof to the operator terminal/commit before returning a clearly labeled
canonical identity to the unchanged legacy transport. Other metadata changes,
unstable observations and evidence-write failures must remain failures. This
design is not yet implemented or verified. No proxy or failed unit is restarted.
The complete reference matrix, Goby comparison, positive client gate, primary
upgrade and full M2-M6 remain open; M7 remains deferred.
