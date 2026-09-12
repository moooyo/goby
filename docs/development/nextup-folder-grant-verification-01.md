# Actual original folder Guid grant verification01

Status: **independently verified and fully restored. This scope is consumed.**

The original server accepted the observed top-level library Guids as P's
restricted EnabledFolders grants. Its own-token Views then contained exactly
the two intended libraries, both catalogs matched, and all six episodes had
complete zero playback history. Restoring the full original numeric-ID policy
made the same P token's Views empty again. This resolves the preparation03
permission-identifier failure without an all-folders grant.

## Actual behavior and scope

| Observation | Actual result |
| --- | --- |
| Guid Policy write | HTTP 204; only EnabledFolders changed in P's 45-field policy |
| Complete Policy readback | Exact Guid policy, including AuthenticationProviderId and all other original fields |
| Own-token profile | Ordinary account; EnableAllFolders remains false |
| Own-token Views | Exactly 101 and 103 |
| Own-token catalogs | Seven rows per library, including native roots 102 and 104 |
| Own-token episode details | Six complete zero-history DTOs with correct series/season/number/runtime/path facts |
| Own-token selectable inventory | HTTP 200, bare array with all ten libraries |
| Original Policy restore | HTTP 204, all original 45 fields restored and read back exactly |
| Same-token Views after restore | Empty Items and TotalRecordCount 0 |
| P and administrator sessions | Each logout204 followed by same-token401 |

SelectableMediaFolders inventory is not a browse-access proof: the ordinary P
token saw ten selectable rows while Views and catalog access were restricted
to two libraries. Its actual response receipt SHA-256 is
`5c89ffee3b994d112846f6e847e078b6c3e4c032f98dabe011ad7bf0813cad4d`.

Guid grants were LA101 -> `b953914417ce42b5a5e07a3e8f6fd386` and
LB103 -> `38fbd09cb8af4731993dd77e1750457f`. Native subfolder IDs 102/104
were observed but were not tested as grant values. No new user/library/media
creation, playback, PlayedItems cleanup or all-folders policy was performed.

## Frozen execution

- Run: `nextup-folder-grant-verification-20260913-01`.
- Root: `/opt/goby-test/exec-work-m3e/reference-nextup-folder-grant-verification-01`.
- Execution: `/opt/goby-test/exec-work-m3e/reference-nextup-folder-grant-execution-01`.
- Input SHA-256: `b7006c08f9995c522351923075243cfc9ad757e7f8c17dff0e26eebeae719896`.
- Worker SHA-256: `70087cdaeae927c3091b5abcfc1df0c9203cdf35e17d28e5222f491bad46a971`.
- Unit: `goby-nextup-folder-grant-verification-01.service`.
- Invocation: `630e6194e8c84127a0176dcebc65939f`; former PID `1510226`.

The [worker](../../scripts/test-env/verify-nextup-folder-grant.py) and
[guards](../../scripts/test-env/test-verify-nextup-folder-grant.py) are frozen
from TOOL01/draft-02. Its [27 synthetic guards](nextup-folder-grant-verification-guards.json)
and two remote compile checks passed with no business HTTP or actual process
probes. The earlier draft-01's twenty-guard result remains retained separately.
The [actual plan](nextup-folder-grant-verification-01-plan.json) and
[read-only admission](nextup-folder-grant-verification-01-admission.json) passed
before dispatch. The successful run used exactly 59 normal and 50 cleanup
requests, within fixed 70/60/130 caps.

The [original terminal](nextup-folder-grant-verification-01-terminal.json)
retains awaiting_independent_attestation. It is not rewritten to represent
the independent result. All runtime verification used ssh test-env and
/usr/bin/python3 -I -B; no local verification ran.

## Independent reconstruction and preservation

The [independent terminal](nextup-folder-grant-verification-01-independent-terminal.json),
SHA-256 `68b0e1d5ff13b985d21fcfbcb1a54d2792c216b79bf27aa5b07ff5316cd76684`,
binds every actual input, source, policy, before/after snapshot, closure record
and complete raw ledger. The [replay result](nextup-folder-grant-verification-01-replay.json)
confirms all 109 request methods, routes, payloads, actor/token contexts and
responses reconstruct the exact terminal. All 227 private files and all
110 export files match byte for byte.

The replay only substitutes the historical fresh-output existence check for
the already consumed root; the actual pre-dispatch admission is separately
retained. All source/input/evidence parsing still runs. Its journal is memory
only and its transport only returns recorded raw responses. It cannot issue
HTTP or write into the consumed output.

The real unit is exit0/MainPID0, the original PID is absent, and its cgroup is
empty. All acknowledgements and the two exact token closures are verified from
raw evidence. The complete original Policy SHA-256 is
`dba7e371d02fe2d219dd33141c8d8f103897635690f1b2f56b74e2fa9160a881`;
the candidate Policy is `c0ba697f5d845bf393ba448e997eee9642f41f60f6e2c298443e52f75d9595b2`.

The [before](nextup-folder-grant-verification-01-preservation-before.json)
and [after](nextup-folder-grant-verification-01-preservation-after.json)
checks preserve all 143 historical roots, main files/services and the complete
Goby v7 state except capture time: 83 sessions, 70 devices and 185 activity
entries. Reference before/after public comparisons preserve full policies,
configuration, preferences, catalog projections and details, allowing only
the exact owned authentication dates and devices.

The independently confirmed after snapshot is
`/opt/goby-test/exec-work-m3e/reference-nextup-folder-grant-verification-01/private/after-public.json`,
SHA-256 `2e222c6c79a4e24725510a6234a29f88ba673bb310d180a3bb6c5c21ac07bf54`.
It contains eight users, ten libraries, 93 devices, 77 unique catalog IDs and
twelve detail witnesses: admin4, viewer2 and grant-P6. The six added witnesses
are actual administrator-token subject-P before/after observations; P's
separate own-token reads match them. They are not invented historical own-token
baselines. The scope inventory SHA-256 is
`2bb01ced363348bcb2d8ed414aa358edf5eacd5833389b6fa08d803275a6f736`.

No original implementation or reference database bytes were read by the
worker or independent audit. The existing proxy was reused unchanged; its
internal original executable reads are not claimed to be zero.

## Next preparation

Keep all twelve actual detail witnesses for fresh preparation. With eight old
users and ten old libraries, then two new users/libraries, the snapshot formula
is S(U,L,D)=5+L+2U+D. D12 produces before43 and after49 GETs. One new selectable
mapping GET yields maximum normal257, success263 including six logout/rejection
requests, and failure cleanup89. A two-round stable scan uses 233 total.
Source adapters are being implemented with normal280/cleanup100/total380 caps;
matrix transport budgets remain unchanged.

Release v2 must retain the actual v4 sealed evidence and independently bind
this successful Guid run, its complete after snapshot, real raw closure chain
and current closed unit. The outer operator must derive request maxima from
the actual recomputed producer plan. Four playback calibrations, a released
matrix fixture, global NextUp observations and real client acceptance remain
open. This grant experiment does not authorize a main upgrade.

