# Programs capture02: state and closure accepted

Capture02 completed once and passed independent review of its captured state
and owned-resource closure. Exact result, state, source snapshot, startup
summary and closure pins are in the [checkpoint](programs-state-capture-second-attempt-20260917.json).
The large state bodies are retained privately rather than copied here.

The original SSH, dispatcher and CLI exits were all 0. Sixteen read-only SQL
frontends, including two lease transactions, were acknowledged and closed.
The source and inactive snapshots each retain 35 tables and five sequences;
their 230 and 147 rows respectively match the reviewed history exactly.
Candidate/PostgreSQL/lease/hosting projections matched before and after.

All 72 activity rows meet the 30-day retention condition at actual database
time `2026-09-16T16:18:54.947417+00:00`. After the required 1800-second
transition/recovery budget, the minimum recorded slack is 2,300,732.160822
seconds. The retained generation key uses the six historical identity fields
under authority `047e68ff...` and current stat-only facts. No key body was
read/hashed or historical key hash projected as current proof.

The independent review read 85 capture files and applied the bounded pure
functions to 106 records totaling 5,286,583 bytes in about 0.107 seconds.
It did not invoke the actual product loader or captured-state CLI check.
Seven outer commands/14 streams closed; 26 recorded PIDs, 25 groups and the
owned unit/SSH-session cgroups were absent. Lock metadata remained exact and
the lock was released after closure. Database backend disappearance was not
measured; the application lease backend is expected to remain alive.

The exact 12-field Pin2 startup summary has status
`captured_state_supports_bounded_transition_contract`. It supports the
contract at the captured instant only. Fresh entry checks and recovery-route
admission remain necessary. The real 23-field
input and summary were subsequently consumed by the [first saved-file reader](programs-saved-product-first-attempt-20260917.md).
`--check-captured` passed once, but `runtime.load_programs_product` failed once
at `programs_artifact_independent_review`: the canonical review says `passed`,
while the complete-profile consumer expected `verified`. The correction later
passed a separate 152-check scope and was integrated at `8b1dead`. The
[second actual product readback](programs-saved-product-second-attempt-20260917.md)
now has composite acceptance; its caller failure remains separately preserved.
The captured-state check was not replayed. No transition has run; the hold remains.

Capture01 remains failed and consumed. The 138, 142 and 151 component results
retain separate scopes, as does the earlier two-query runtime observation.
This capture is not client acceptance or a new successor epoch.
