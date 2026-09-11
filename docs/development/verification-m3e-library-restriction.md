# M3e library restriction and restoration verification

Status: **the candidate API matrix and independent inspection passed**. The
[implementation plan](m3e-library-restriction-plan.md) targets the isolated
source32/schema27 candidate. It removes only B's original Movies access while
retaining the separate Extras Movies, Music and TV libraries, then restores
the complete supported account and policy values using a fresh revision.

The [operator](../../scripts/test-env/verify-client-library-restriction.py) and
[pure guards](../../scripts/test-env/test-verify-client-library-restriction.py)
are implemented. Static review covered native/Emby error envelopes, revision
reconciliation, exact owned-session cleanup, separate business/restoration/
cleanup budgets, preservation of authenticated snapshots and complete database
differences. Two remote syntax checks, 22 pure guards, a fresh read-only
preflight, the actual matrix and the independent inspection all passed.

## Passed API execution

The [actual report](m3e-library-restriction-api.json), SHA-256
`193a5cc5ddaa806575d630a03de1268abed2418347b4e57eb5cc57610a04e8eb`,
records one successful run in
`/opt/goby-test/exec-work-m3e/client-library-restriction-v1`.
Its 61 HTTP exchanges comprise 42 matrix GETs, eight native inventory/account
GETs, three logins, two Policy PUTs, three logouts and three exact-token checks.
There were no playback preparations, scans, service replacements or retries.

| Check | Baseline | B restricted | Restored |
| --- | --- | --- | --- |
| B original Movie detail, ParentId, Similar, ThemeMedia, SpecialFeatures and LocalTrailers | 200 | 404 `not_found` | 200 |
| B explicit original Movie ID list | 200, movie present | 200, empty | 200, movie present |
| B Views | Four libraries | Original Movies absent; Extras, Music and TV retained | Original four libraries |
| A original Movie detail | 200 | 200 | 200 |
| Cross-user A/B detail routes | 403 `access_denied` | 403 `access_denied` | 403 `access_denied` |
| B positive Extras Movie, three SpecialFeatures and one LocalTrailer | 200, exact identities | 200, same identities | 200, same identities |

Each phase completed all 14 cases. The same A/B tokens were used throughout.
The two writes changed B's revision from `1` to `3`; its complete supported
account and Policy values were restored. The raw empty Policy was materialized
to the six supported defaults and two account-role flags, exactly as the native
update contract requires. No raw `{}` equality is claimed.

All old database rows were preserved except B's declared Policy, revision and
`updated_at` changes. Exactly three sessions, two devices, six session audit
facts and two owned `user.updated` facts were added. The old 26 play rows,
seven UserData rows, 64 authentication rows and 56 device rows were retained
exactly. Only the two corresponding sequence increments occurred. All three
new sessions closed with logout204 and rejection of the exact token with401.
No journal or cleanup failure was recorded.

The three private snapshots stay on test-env, under the execution directory:

| Snapshot | SHA-256 |
| --- | --- |
| `before-full.json` | `b265e59aaad795bd688756e5642aa6b87eea50ba3e570726e188cff54f8175fa` |
| `authenticated-full.json` | `646fa69fb00fc577607d025c5510a7241f1e38d22d17321df82b2939ef27637c` |
| `after-full.json` | `a09e42a404b9aa0251e2341e7ffa85c93528b7b141d81e11df6791802d6e92fd` |

The [independent inspection](m3e-library-restriction-inspection.json), SHA-256
`e3dcbc0c21edbc484baab89762e11857a7cbc46889b78701af8dd309f12c3441`,
took a fresh complete snapshot with zero HTTP requests. Stored rows, sequences,
catalog and private state matched the recorded after-snapshot exactly; only
the capture timestamp advanced. The independent old-row/Policy checks and
media comparison also passed. Its private snapshot is
`/opt/goby-test/exec-work-m3e/client-library-restriction-inspection-01/current-full.json`,
SHA-256 `1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3`.
The candidate remains source32/schema27, PID748513/start ticks6996875, with
22 items, four libraries, 26 play rows, seven UserData rows, 67 global sessions,
58 devices and 149 audits. References and encoding rows remain zero.

This closes the declared API gate. It does not establish original-client UI
behavior during a policy change or complete M3/M4/M5/M6 compatibility. Future
work must bind this new after-state rather than replaying the completed
operator or requiring the historical empty Policy and 64-session baseline.

## Remote tool verification

The [tool verification](m3e-library-restriction-tool01-verification.json), SHA-256
`4e58767ac35f4e9edeaa7b9b3088ffbcb27ecf6c57f36f5527901632d6ca39f1`,
records two passed syntax checks and 22 passed pure tests, with no failures or
errors. Source and input identities remained unchanged. The frozen directory is
`/opt/goby-test/exec-work-m3e/client-library-restriction-tool-01`.

| Input | SHA-256 |
| --- | --- |
| Operator | `99cb5b89f0da94940bed34a86607a545278da914750ae09ac7b52d458810004e` |
| Guards | `c92056da224e145dc1039b03496e8df3e768638fde1d343e91ec0fdda65253b5` |
| Tool manifest | `8f8f1613688250bc4871dd29723321465696d262d0777be51185a9098078ad1a` |

The guard worker's live CPU150%, 512 MiB memory, zero swap, 64-task and
three-minute limits were observed, with a private network. No business HTTP,
database command or application-service action occurred in that verification.
The separate actual preflight passed with zero HTTP and no evidence-directory
creation by the operator; its stdout SHA-256 is
`1eb0580449e2e8fbf98913c254831ec2d27015e732112b7ceda1406a50ec2c81`.
Execution controller records are retained in
`/opt/goby-test/exec-work-m3e/client-library-restriction-execution-01`.
The API worker's live CPU150%, 768 MiB memory, zero swap, 128-task and
15-minute limits were captured. Its journal records successful deactivation.
The transient unit was later collected; default properties from the missing
unit are not presented as an exit-code or resource-limit observation.
No local verification ran.

## Historical pre-execution baseline

The [read-only baseline review](m3e-library-restriction-baseline-review.json)
passed with zero HTTP requests and zero candidate mutations. Its SHA-256 is
`f678f5797f8da7b4536079698e7ee4070636d90a17047c5b6f6824e86f0cb3bf`.
The candidate retains PID `748513`, start ticks `6996875`, source32 binary
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`,
and fixture-state SHA-256
`5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1`.

The private complete snapshot is retained only on test-env at
`/opt/goby-test/exec-work-m3e/client-library-restriction-baseline-review-02/current-full.json`,
SHA-256 `3320e35d1ad9ce4a3583ac264954dd097e03a8c8a2fa28a772eb0a57524f5f7b`.
It records 35 tables, 22 items, four libraries, 26 play rows, seven UserData
rows, 64 global authentication rows, 56 devices and 141 activity entries.
The A/B projection equals the accepted positive-client after-observation
`74640ff8ce353ad61b41364fbeedaff4fa2c14e55075ce55869e8be85963a10b`
row for row. Its 57 authentication rows are a scoped subset of the global 64.
Catalog, ownership, ACLs, runtime configuration and private fixture state match
the earlier setup snapshot. This does not establish whole-database historical
equality outside the compared scope.

B has management revision `1`, remains an enabled ordinary user, and has raw
Policy `{}` at this snapshot. Native GET projects supported defaults. A
successful two-write restriction/restoration should preserve those defaults
and all unknown keys while materializing the supported Policy and account-role
fields; raw Policy must be compared to that exact merge, not exempted from
comparison or incorrectly required to remain `{}`. Actual execution still
requires a fresh preflight and a new baseline before login.

## Retained baseline checker failures

The first read-only review recorded an unlocalized assertion failure. The
second retained a complete snapshot and localized the failure to the metadata
comparison. An independent offline review of those immutable snapshots proved
that only `metadata.captured_at` differed; every other metadata field matched.
The initial comparison had treated the observation timestamp as database
identity. The first two result trees remain failed and unchanged; the separate
third report records the successful review. None of these steps logged in,
changed a policy, restarted a service or replayed a completed acceptance flow.

The verified operator validates snapshot timestamps as observation times and
compares the remaining metadata exactly. The later API matrix, restoration,
owned-session cleanup and database-difference proof passed separately, as
recorded above. Broader M3/M4/M5/M6 work remains open.
