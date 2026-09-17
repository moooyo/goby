# Programs finalization generated V4; client admission pending

The actual finalizer completed once and generated the V4 runtime epoch and
seed-runtime binding. Original PowerShell session `51813` (`f7f112` to `677620`),
native SSH, caller and CLI all exited 0. Finalizer PID `2065204` completed in
1.932600314 seconds with 16 read-only SQL commands and 32 metadata commands.
It performed zero stop, replace, start or HTTP calls.

Candidate A remains PID `1907978`, with backend `1907986`, the same recorded
identity and lease, and binary `ead67c8f...`. The new result is
`running_awaiting_live_acceptance`; `candidateAdmissionComplete=false` and
`clientAcceptance=false`. Original S2 CLI/caller/SSH exit 2 and original closer
evidence false remain unchanged. Finalization did not replay D000 or the transition.

The seven artifacts below are retained under
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-programs-finalization-01/private`:

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| `result.json` | 345597 | `65367bc01981356f062fc10f770aa6b45480ea1cb35ef5a25b44a9f4399c3c87` |
| `runtime-epoch.json` | 28883 | `ae728a2a4e7d329b536e7c3a755946dd6eefd2234d60b4ef9c9c1499c32fea86` |
| `seed-runtime-binding.json` | 23981 | `5d687e1815b6405abb486cfd428935515d4a2a41e4fc7eb2e74b2d01485787a7` |
| `fresh.json` | 1182355 | `37b144cae20b044468c97e62dbec4af87ce038f87b763c3aa0766f0078847a34` |
| `source-fresh.json` | 554089 | `bbf7a0aafbe72fcf0941d62d85667996cdd5cec3c00458da552b78fe200b68e8` |
| `preservation.json` | 3997 | `3f821d4b91dd9e46085bb14e091539fd428a58a0e7970f82ba419ed03afb045d` |
| `antecedent.json` | 20268 | `1443e03f840ff6fac43c5767894da5df2c0de5c560c8f1d158593fb4a368ae27` |

The original closer completed once with exit 0 (`f22131`) and accepted both
evidence and resources. Its record is
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/programs-finalization-dispatch-20260917-r01/private/session-closure.json`,
1082702 bytes, SHA-256 `8f961209e35bdc7618b903934b3c3d4122d90297499afc655c30c513333d25d3`.
The finalizer's own result retains its original `outerUnitClosure=null`;
the later caller/closer records supply their separate scope. Independent review
then accepted the completed read-only finalization, V4 references, fresh-state
preservation and owned closure. It confirmed 77 original owned PIDs, their groups
and three cgroups closed. Its reader's 664 read descriptors and own process scope
also closed. The server unit log was unchanged; the PostgreSQL unit log added
6821 bytes with its old full prefix preserved, without a content-cause claim.

These later records are in the same dispatch `private/` directory:

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| `independent-finalization-review.json` | 133472 | `4fd14846d85e1821947850c651b6f9b4840c38c83be1e27a7af9b083b5623e62` |
| `independent-finalization-reader-closure.json` | 2515 | `054c7629ebaf0a96f3a0bf3d5e3fd59c41a73d9715fc55107dee992256949132` |
| `independent-transition-closeout.json` | 8471 | `5923b53a6c3c2829d422390578b21bc49e0d08367c3477891edf420400c8d6a9` |

Review `d7abf4`, reader closure `43a500` and closeout publication `f0e7ff` each
exited 0. The admission-facing closeout records
`successor_running_awaiting_affected_live_admission` and supports installed-binary,
current-process, old-process-absence and staged-binary-absence facts. It retains
`candidateAdmissionComplete=false` and `clientAcceptance=false`. Historical epoch
calls remain stop/replace/start once each; the finalizer added none. No new SQL,
HTTP, service action, product test or affected admission ran in these reviews.

The [21-method component scope and saved-only check](programs-finalization-component-result-20260917.md)
have completed independent review. Their source changes were merged into main
at `3f609b6`; those component results remain separate from actual finalization.
Next is Programs client-admission preparation and updating the M5 full-run guard
for the recorded successor, followed by their required verification. V4 creation
does not establish client admission, client acceptance or the remaining M5
full/build and M2-M6 obligations. This documentation pass read local saved
artifacts only; it ran no tests, validators, SQL, HTTP or service actions.
