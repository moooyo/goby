# Actual global NextUp reference preparation03

Status: **failed at the ordinary-user Views gate and independently sealed after
known cleanup. No usable matrix fixture was produced.**

The frozen TOOL06 producer created two owned TV libraries, two ordinary
accounts and six independent synthetic episode copies. It stopped before any
playback calibration because P could not see the two granted libraries through
its own token. This run provides no usable matrix input or client acceptance.
All consumed input/output bytes remain immutable and the run must not resume.

## Actual execution

- Run: `nextup-global-reference-20260913-03`.
- Root: `/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-03`.
- Execution: `/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-execution-03`.
- Input SHA-256: `b8c9c03d6571a759b8e7d0a61d1751f0c43114a8b61cdd30c4b7009973300b1e`.
- Producer SHA-256: `2c1d4d31fd2dfaf01fac0969d89acc881774aa2005b97b647442e625e634c98d`.
- Canonical plan SHA-256: `a76e81d0f8e66c6ab651097aca4cb3f6ce1f984b1ef6b45d79ea9ae2fd715ef5`.
- Unit: `goby-nextup-global-reference-preparation-03.service`.
- Invocation: `f7050bfe84fc410e814aa86f498a428a`; former PID `1508049`.

Assembly, plan and actual read-only Authority admission passed before dispatch.
The input bound the independently accepted observer02 baseline, its exact
closed-authentication row, completed predecessor units and 132 sealed roots.
The future matrix parent was frozen as
`/opt/goby-test/exec-work-m3e/nextup-global-reference-runs-03`, with separate
`matrix-03` and `operator-03` children. Both children remain absent.

The [actual terminal](nextup-global-reference-preparation-03-terminal.json)
retains `stopped_with_known_cleanup`, completed false, cleanupComplete true,
matrixInputsUsable false, no uncertainty, no pending request or ownership,
and no cleanup errors. Its 107 actual requests comprise 64 normal and 43
cleanup requests: before 32, libraries 15, mapping 5, accounts 8, baseline 4,
cleanup 43. There were zero calibration or playback requests and no drafts.

## Exact visibility failure

Request 64, `baseline-views-P`, received complete HTTP 200 with
`{"Items":[],"TotalRecordCount":0}`. The empty page passes the normal page
decoder. The following assertion requires the own-token view IDs to equal the
two acknowledged view IDs, `101` and `103`, so it correctly rejects the empty
set. The full response receipt SHA-256 is
`ea76c05a8eabb86779921bd8354c2b57d36f53f6a7ade076c59ae28ddf893991`.

P's preceding own-token profile and preferences reads both returned HTTP 200.
The profile retained IsAdministrator false, IsDisabled false,
EnableAllFolders false, EnableMediaPlayback true and EnabledFolders
`["101","103"]`; MyMediaExcludes was empty. The administrator's earlier
Views observation contained both new CollectionFolder IDs. Their mapped
native source-root IDs are `102` and `104`, but those values have not been
proven to be the correct grants. No substitution or new HTTP probe has been
performed on that assumption.

## Independently checked cleanup and preservation

The [closure audit](nextup-global-reference-preparation-03-independent-closure.json),
SHA-256 `6046e1538630d3e882ee67332b365215e0cc514ce48cb08c47fb22e3129100c0`,
checks all 107 contiguous intent/reservation/complete-response triples, their
request and payload bindings, response byte counts, time order and exact actor
token contexts. Both acknowledged logins, P and administrator, have their
own logout204 followed by same-token401 proofs. Q was created but never logged
in. The audit confirms exit2/MainPID0, absent original PID and empty cgroup.
It makes zero business requests.

The [before snapshot result](nextup-global-reference-preparation-03-preservation-before.json)
and [after snapshot result](nextup-global-reference-preparation-03-preservation-after.json)
confirm all 132 sealed roots, the old services and main files remain identical.
The complete Goby state still matches sealed v7 except capture time, with
83 sessions, 70 devices and 185 activity entries. The after preservation
record SHA-256 is
`029a0a3da689bf776f6935c2bff79d7d2e0259f4365725ea38d86af6aabd938a`.

The two new accounts, two libraries and six copies remain owned retained
artifacts. Session closure does not mean their deletion. The complete reference
population after cleanup is eight users, ten libraries and ninety devices.

The subsequent [independent terminal](nextup-global-reference-preparation-03-independent-terminal.json),
SHA-256 `eda2a0b9a8263ef6048a2f81bab6de8bbd54a214ba9801254a975ddac1bc0535`,
supersedes the earlier closure audit's pending reconstruction/seal flags without
rewriting it. Its evidence is retained under
`/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-seal-02`.
The [complete reconstruction](nextup-global-reference-preparation-03-public-reconstruction.json)
replays all 107 actual requests against the frozen producer with an in-memory
journal and a transport that only returns recorded responses. It checks all
331 private files and 108 export files, both full public snapshots and both
preservation comparisons. All requests, final state and terminal bytes match.
All sixteen media/NFO/marker files and their nine directories match the exact
approved tree; the six MP4 files remain independent copies.

One private comparison report has an allowedAuthenticationChanges list-order
difference caused by the producer iterating a two-element set. Its exact list
entries and every other JSON field match; sorting preserves duplicate counts.
The audit explicitly records this difference and sets allProducerBytesMatched
false. The other 330 private files and all 108 exports are byte-exact. The first
strict byte-comparison attempt remains failed in the preparation execution
directory. A separate audit source and scope perform the narrowly documented
comparison; the original producer output is not rewritten. A producer repair
for deterministic future report ordering is tracked separately.

The final scope inventory SHA-256 is
`834ebeddc9a2d64df7f93a9fbf6ea7df66d60dde47fb82150b25814fae394e54`.

All runtime work ran through `ssh test-env` with `/usr/bin/python3 -I -B`.
No local verification ran. No original application or reference database bytes
were read by the independent audit. The existing proxy was reused unchanged;
this statement does not claim its internal executable reads are zero.

## Remaining work

Establish the actual public folder-grant semantics before preparing a fresh
bounded scope. The previous six-user/eight-library baseline is historical
after these retained creations and must not be reused as the current baseline.
Four playback/cleanup calibrations, a usable matrix fixture, global NextUp
observations and real client playback remain incomplete. The positive client
automatic-refresh gate and main upgrade also remain open.
