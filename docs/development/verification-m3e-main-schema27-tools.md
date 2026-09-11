# Main schema27 deployment tooling verification

Current consumer revision: [tool03](m3e-main-schema27-tool03-verification.json)
passed two remote syntax checks, 26 pure guards and the helper build. Its
manifest SHA-256 is
`b58163bd0fedace1ea2d2339cc6813ef369f619fb5e3b3493c3ecaf2c3486cb4`.
It requires the independent continuation inspection and explicitly binds the
original failed creation, its 201 acknowledgment and the later completion.
The migration helper source and binary are unchanged from tool02. Actual
runtime limits were captured while the tool03 unit was live. No primary
preflight, candidate action or database command ran in this verification.
Positive continuation and client evidence remain required before deployment.

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

This verifies tooling only. No main operator preflight, helper execution,
database command, migration, service stop or installation ran. The primary
remains source28/schema26. Deployment still requires the actual nonempty
candidate inspection, positive original-client report and database comparison,
followed by a fresh service pin, startup plan and release attestation. Historical
candidate upgrade and root-extension process identities remain separate.
