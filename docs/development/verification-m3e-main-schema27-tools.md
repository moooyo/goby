# Main schema27 deployment tooling verification

The separate [primary deployment](m3e-main-schema27-completed.json) passed on
2026-09-11 after the candidate protocol, original-client and scoped database
acceptance gates. Both services now run source32/schema27. The primary is
`goby-foundation-test.service`, PID `762090`, start ticks `7637121`, with binary
SHA-256 `af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
An independent service read confirmed active/running and the same PID.

The execution directory is
`/opt/goby-test/backups/main-schema27-v1/run-20260911T190452Z-94ef1ef5242d2335018f8dac`.
Its terminal SHA-256 is
`0b0695556716bf78f414f146c2ab5f84fa273455679969050d69cf086af46b5a`.
Preflight and the schema26 backup completed before an independent
[restore and migration rehearsal](m3e-main-schema27-rehearsal.json).
The [rehearsal cleanup](m3e-main-schema27-rehearsal-disposal.json) confirmed the
owned database and role absent, without force or backend termination.

The [primary migration](m3e-main-schema27-migration.json) committed schema27
while preserving the old 33 tables' rows, columns, relation OIDs, ACLs and
sequences. The before/preserved-state SHA-256 is
`dd7f4564c121fbe088390a3d6a179b2048a5089a4fc9e3d8d5af7b1be49e175a`.
The two new Extra tables are empty on the primary. Private runtime state,
media and historical archives were retained. The primary database was not
restored and no old-binary rollback or automatic retry occurred.

The [native smoke receipt](m3e-main-schema27-native-smoke.json) records 13
successful expected responses, including logout204 and rejection of the exact
session with401. It used one newly owned session and made no backup creation,
archive deletion or restore request. Authentication and audit history added
by smoke is retained; whole-table equality after smoke is not claimed.
The completed execution and earlier failed trees must not be replayed.

Latest consumer revision: [tool04](m3e-main-schema27-tool04-verification.json)
passed two remote syntax checks, 27 pure guards and an independent helper build.
It accepts the explicit version3 finalization inspection and requires both
retained failure trees, the scan proof and the final one-viewer media proof.
Its manifest SHA-256 is
`785666813f965c26de253b421773135eeba36224700d25c968b09fa4040b371a`.
Helper bytes remain unchanged. The source, live resource-limit observations,
build and guard receipts are preserved in `main-schema27-build-04`.
The actual main service input bundle was prepared after candidate finalization,
inspection, original-client UI and scoped database comparison passed, and was
consumed by the separate successful deployment above.

Earlier consumer revision: [tool03](m3e-main-schema27-tool03-verification.json)
passed two remote syntax checks, 26 pure guards and the helper build. Its
manifest SHA-256 is
`b58163bd0fedace1ea2d2339cc6813ef369f619fb5e3b3493c3ecaf2c3486cb4`.
It requires the independent continuation inspection and explicitly binds the
original failed creation, its 201 acknowledgment and the later completion.
The migration helper source and binary are unchanged from tool02. Actual
runtime limits were captured while the tool03 unit was live. No primary
preflight, candidate action or database command ran in this verification.
At that historical tooling boundary, positive continuation and client evidence
were still required before deployment. Those gates subsequently passed.

The earlier tool02 result remains preserved below.

The independent schema26-to27 deployment tools passed remote syntax checks,
24 pure guard groups and the migration-helper build in
`/opt/goby-test/exec-work-m3e/main-schema27-build-02`.
The [complete verification receipt](m3e-main-schema27-tool02-verification.json)
has SHA-256
`5b1bd21ff53d5d17c534af5a84a3d6942afb5f257d591b6818cd44049494fd18`.
The helper executable has SHA-256
`4ba5866c8aa874ec230d5adcddce0f339860c35f3cde6cff9ef4ab32698e3044`.

The frozen tool source is `tool-build-main-schema27-02`, with tool manifest
SHA-256 `4bfc09d99ed411b66bf6e0f147b5f17e1f9f8c4eaef27019e9a813d1039a5b31`.
Its 4006 members contain the unchanged 4002 source32 files, their original
manifest and the three new tools. Only the new helper was formatted remotely;
those exact bytes were returned to the worktree. No local verification ran.

The [first attempt](m3e-main-schema27-tool01-build-failed.json) passed two syntax
checks and 23 guards but failed compilation because two removed repair-root
constants remained referenced. Its source and output remain retained.
Independent review also found an incorrect extension-report digest; the second
attempt fixes that pin and adds a guard against the independently observed
actual receipt hash. The original failed attempt was not restarted or relabeled.

The successful systemd launch accepted CPU, memory, process and runtime limits
and disabled network access. The journal records successful deactivation.
The transient unit was subsequently collected, so its runtime resource-property
readback is not retained; default properties after collection are not proof
of the limits used. Both that boundary and the accepted launch parameters are
recorded in the verification receipt.

The tool02 run verified tooling only; it performed no main operator preflight,
helper execution, database command, migration, service stop or installation.
At that historical boundary the primary remained source28/schema26. The later
nonempty candidate inspection, positive original-client report, scoped database
comparison, fresh service pin, startup plan and release attestation preceded
the successful deployment recorded above. Historical candidate upgrade and
root-extension process identities remain separate.
