# Global NextUp reference preparation 02

Status: **the actual preparation stopped at a Devices decoding mismatch and was
independently sealed after known cleanup**. The preparation did not produce a
usable matrix fixture. Its directory and request ledger are consumed and must
not be resumed or replayed.

## Actual execution and retained artifacts

The [input assembly](nextup-global-preparation-assembly-02.json),
[frozen plan](nextup-global-preparation-02-plan.json), and
[read-only admission](nextup-global-preparation-02-admission.json) completed
before business execution. The repaired TOOL05 producer was bound to SHA-256
`5abaa7893631f9ee63ee89f5a8d05a4c24c77e803d66421c7ac101dd6a3abc3e`.
This is the published 114-guard version; later source corrections do not change
the bytes or result of this actual run.

Run `nextup-global-reference-20260913-02` used these remote owned paths:

| Purpose | Path |
| --- | --- |
| Private input | `/opt/goby-test/exec-work-m3e/nextup-global-reference-preparation-inputs-02/input.json` |
| Preparation output | `/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-02` |
| Independent execution evidence | `/opt/goby-test/exec-work-m3e/reference-nextup-global-preparation-execution-02` |
| Unused future matrix root | `/opt/goby-test/exec-work-m3e/reference-nextup-global-matrix-02` |

The raw input SHA-256 is
`2611adf729ec18cb5c738e6f9099142d247bbca26af4aaca0a60a07c30958c87`.
The canonical plan digest is
`501ae7a97586bfd263f61b90a28e53c3360716a647d9a45cb4b602ccbdabb5f2`.
The plan retained the 320-request limit, 240 normal and 80 cleanup allowance,
with maxima of 232 normal, 238 successful including logout, and 77 failure
cleanup requests.

The single unit `goby-nextup-global-reference-preparation-02.service` ran as
`Type=oneshot` with `TimeoutStartSec=1900`, `RemainAfterExit=yes`, a 1 GiB memory
limit, one CPU quota and a 64-task limit. Its original invocation was
`b72b2ba008114665a2c8f34fb09d0cb0`, initially PID1503850. It exited with code 2;
the independent terminal confirms the original invocation, `MainPID=0`, the
original PID absent, and an empty cgroup. The failed unit is retained.

The [producer report](nextup-global-preparation-02.json) records 33 completed
HTTP attempts: 31 normal and two cleanup. These consist of one administrator
login, 30 public GET observations, one logout and one exact-token rejection
GET. The normal phase ended at `before-devices`; configuration was never read.
There were no library-creation, user-creation, scan, metadata-update, or playback
requests. All response bytes and request/reservation records are retained.

Before login, the producer created six independent copies of the approved
48,786,888-byte synthetic MP4, plus the declared NFO and ownership files. These
files remain owned artifacts. One new administrator authentication device and
the closed authentication history remain. No new P/Q accounts, libraries,
series, episode catalog entries or playback calibrations were created.

## Observed Devices contract and failure

`GET /emby/Devices` returned HTTP 200 with exactly `Items` and
`TotalRecordCount`. There were 86 items, while `TotalRecordCount` was integer 0.
The retained response SHA-256 is
`9b06672040960b1c8d4bdc466d7ad93c83dc62fcff405e836c27263d4e62498e`.
The frozen producer's generic `page` function rejected this as an incomplete or
truncated collection. Its response was complete; no request remained pending.

The earlier reference v4 `after-devices-result.json`, SHA-256
`431be8903ae39e93eeb68627bf0197b4adbf428486338246299b87e0b8631f8f`,
also retained the zero-count sentinel with 85 actual items. Both collections
have unique registry and reported device IDs. This evidence supports a
Devices-specific decoder. Catalog and other paged queries retain their strict
total-count contract.

The repair must retain the complete known old registry and require the new
population to equal the devices acknowledged by actual preparation logins.
Missing, duplicate, reassigned or unowned identities cannot become acceptable
because the count field is zero. A later corrected producer needs a new frozen
source, plan, input and output scope.

## Independent cleanup and preservation

The [independent terminal](nextup-global-preparation-02-terminal.json), SHA-256
`588c4f09fc817ed555d7f2711f692f94e35f1f41bd288748626fffca12546111`, records
`failed_preparation_independently_sealed_after_known_cleanup`. The separate
[audit execution receipt](nextup-global-preparation-02-independent-seal.json)
records success without additional business HTTP.

The audit checked all 33 private intent/reservation/raw-response triples,
including ordinal order, exact requests, complete bounded bodies, and the
acknowledged administrator token. Logout returned 204 and the same token then
returned 401. State and terminal have no pending request, unresolved ownership,
uncertainty, playback context, or cleanup errors. The producer's
`stopped_with_known_cleanup` result remains failed preparation, with
`matrixInputsUsable=false` and no draft matrix outputs.

The independent private closure proof has SHA-256
`4368f372df547786289dda978d2aa09e826a0efd3d106ed77f74189be6aa28fe`.
The wire index is
`86b9116edade40c68a948eb9c25affc6c9b2e38e4dfef2d37c80afe1ba3eb526`;
the complete sealed preparation inventory is
`fcd4be7080b60ddd2d562312aa43315b47fc6828f1aaf0c4fcced4755a18ae15`.
Credentials, tokens, raw requests and private database snapshots are not
published in this document or its safe JSON reports.

The [before preservation receipt](nextup-global-preparation-02-preservation-before.json)
and [after preservation receipt](nextup-global-preparation-02-preservation-after.json)
confirm all 109 recorded old roots unchanged, including the preceding 87-root
baseline and the sealed reference v4 scopes. The main files and recorded old
services remain equal. The full Goby state remains equal to sealed v7 except
capture time, at 83 sessions, 70 devices and 185 activity entries. The approved
source and every retained new media file were independently checked by identity
and hash. Application and existing proxy metadata remain unchanged.

The [partial public comparison](nextup-global-preparation-02-partial-comparison.json)
found no differences in the observed server, libraries, catalogs, per-user
projections, preferences or six details. The old roster is equal except the
administrator's authentication dates. All old 85 device rows are exactly equal;
the one additional row belongs to the acknowledged preparation administrator.
Because configuration was not observed, this is explicitly not a complete new
public baseline or complete reference preservation claim. No original
implementation bytes or reference database were read by these tools. The
existing proxy and its internal executable checks remain unchanged.

## Earlier assembly and next work

The [first assembly attempt](nextup-global-preparation-assembly-01.json) failed
before writing new input files or issuing HTTP. It incorrectly selected the
top-level historical viewer entry in `reference-browser.json` when the required
administrator was in `accounts.admin`. Its script and logs remain under the
separate execution01 root. Corrected assembly02 selected and checked the actual
administrator account in a new input/execution scope.

The next step is a new bounded public-baseline observer: one new administrator
session, the complete 31-GET snapshot, logout204 and exact-token401, at most 34
HTTP attempts. It must account for the retained preparation02 device and its
closed-token proof, include the configuration observation, and publish its own
independent closure evidence. It must not combine this partial snapshot with an
older unobserved configuration value and call that a new complete capture.

The corrected TOOL06 producer passed [140 remote guards](nextup-global-preparation-verification-03.json)
and two compile checks; its SHA-256 is
`2c1d4d31fd2dfaf01fac0969d89acc881774aa2005b97b647442e625e634c98d`.
After the observer passes its checks and the new baseline receives independent
attestation, preparation requires another fresh scope. The full reference matrix,
client playback and positive automatic
refresh gate remain unexecuted or unmet. Candidate source55/schema28, main
source32/schema27, and product publication authority `16d75c3...` are unchanged.
M2-M6 remain open; M7 remains deferred. No local verification was performed.
