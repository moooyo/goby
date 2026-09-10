# M5h configuration verification

The M5h configuration increment is complete and deployed on the Linux test host.
It adds the supported ConfigurationService fields and extends native settings;
it does not establish complete Emby configuration or official-client compatibility.
The deployed schema is 21, and the probe cache version remains 6.

## Completed checks

All execution took place through `ssh test-env` on Linux. Local work was limited
to source editing and the separately authorized Go and React compilation.
PostgreSQL integration tests used the isolated test cluster on port 15432.

| Check | Actual result | Evidence |
| --- | --- | --- |
| Complete regression | 1252 top-level tests passed across 14 tested packages, with no test skips or race findings | [Report](m5h-full-race.json) |
| Settings domain and database migrations | 41 top-level tests passed with the race detector; no skips or race warning | [Report](m5h-settings-database.json) |
| Native settings and compatibility HTTP/DTO/media | 37 top-level tests passed with the race detector; no skips or race warning | [Report](m5h-configuration-http.json) |
| Browser and persistence | 16 check groups passed in 7.808 seconds, followed by two exact 28-table restart checks and complete fixture cleanup | [Report](m5h-configuration-browser.json) |
| Fresh official width-study guards | Final 25 pure guards passed; no HTTP or media execution in this guard suite | [Report](m5h-encoding-width-guards-attempt-2.json) |
| Actual official width comparison | Complete software-encoded 1280x720 and 3840x2160 outputs, followed by baseline restoration and fixture teardown | [Report](m5h-encoding-width-reference.json) |
| Deployment operator guards | 22 synthetic guards passed; no database restore or deployment | [Report](m5h-deployment-operator-tests.json) |
| Actual deployment | Complete 28-table isolated restore rehearsal, protected backup, migration, and installation passed | [Report](m5h-deployment-evidence.json) |
| Deployed configuration workflow | Compatibility writes, native CAS restoration, two revoked-credential barriers, and exact old-row preservation passed | [Report](m5h-deployed-configuration.json) |
| Go cache relocation | 1,058,799,879 bytes copied, persisted, and verified before original paths became symlinks | [Report](m5h-go-cache-relocation.json) |
| Inactive dependencies | 353,563,205 logical file bytes copied with original content, ownership, permissions, and internal symlinks preserved | [Report](m5h-dependency-relocation.json) |

The media test produced and decoded new HLS output at widths 160, 96, and 160
while changing the independent compatibility ceiling from zero to 96 and back
to zero. Native `MaxWidth` remained 160. The previously registered plan and
previous output remained unchanged. This validates Goby's additional-ceiling
policy. The separate [official execution study](../research/encoding-width-reference.md)
observed a configured width of 1280 producing 1280x720, and zero producing the
source's 3840x2160 dimensions under its fixed software MP4 profile. Both complete
outputs contained eight decoded video frames. This supports the scoped
additional-ceiling interpretation; native Goby limits continue to apply.

The source snapshot contains 429 Go/module/SQL inputs, all ten internal testdata
files, and the 2305 previously captured reference JSON records. The second
snapshot differs only in the corrected HTTP test helper described below.
The [candidate source gate](m5h-final-go-source-gate.json) additionally binds 53
browser inputs and 50 built assets to the accepted executable and reports.
The official study subsequently added 61 sanitized observations, bringing the
research corpus to 2366; these are separate from the regression test count.

The browser exercises three visible name choices while preserving all four
stored modes, independent width updates, selective and full reset, exact Mbps
conversion, concurrent revision conflicts, committed response loss, dirty
navigation, and a mobile layout. Before each restart's first authenticated
request, all 28 table snapshots match exactly. The second restart changes only
owned startup defaults; persisted unset mode, explicit overrides, and the
independent encoding width remain intact. Both native credentials, the database,
role, HBA changes, and runtime processes are cleaned up. The four reviewed
screenshots are stored under [screenshots/m5h](screenshots/m5h/).

## Retained unsuccessful attempt

The first HTTP run passed 12 top-level tests and failed one concurrency test.
The new test helper copied a literal `X-CSRF-Token` map key without canonicalizing
HTTP header names. `Header.Get` therefore could not find the native CSRF value,
and the application correctly returned 403. The helper now uses `Header.Add`,
matching the established request fixture, and the native request also supplies
its expected Origin. Production CSRF checks and the concurrency/CAS assertions
were not weakened. The [failed attempt](m5h-configuration-http-attempt-1.json)
is retained separately from the subsequent 37-test result.

The first browser invocation was refused because its candidate executable was
outside the verifier's required executable directory. The same verified bytes
were placed in `exec-scratch`, after which the complete workflow passed. Neither
the browser source nor the application binary changed for this correction.

The first official fixture was stopped before source generation or login because
the package's relative ELF interpreter could not resolve from the launcher cwd.
Its [failure and successful cleanup](m5h-encoding-width-startup-attempt-1.json)
are retained. Fresh attempt 02 keeps systemd's runtime working directory and
changes the launcher to the read-only package directory before execution. It
preserves the first fixture's 14 files and its 25-file source/evidence bundle.

## Deployment and retained state

The deployment ran after the full suite, browser checks, and completed official
study teardown. The complete schema-20 snapshot and dump were actually restored
into a new disposable database and all 28 old tables compared before stopping
the old service. The rehearsal database was removed. The backup at
`/opt/goby-test/backups/m5h-20260910` is complete and records successful deployment;
it must not be restored over subsequent business changes.

Migration 21 retains every old managed-settings field, revision, and timestamp,
maps null names to deployment mode, and initializes the extra width to zero.
All preceding business rows, original media, master-key bytes, service unit,
and runtime configuration remain preserved. The new service runs as UID 995,
PID 3641418, with start ticks 26048863. Its accepted executable SHA-256 is
`62729fa1ba6b7d5f191d598c79a14606cb139bcdec6346ff5c3f1676551afd54`.

The deployed workflow completed in 0.901 seconds using 23 GET, five POST, one
PUT, one DELETE, and 25 read-only SQL inspections. One fresh native credential
and one fresh ordinary Emby credential performed a Partial name write and one
named encoding write. After revoking the non-CAS Emby writer and proving 401
plus its committed revocation barrier, the native client restored the complete
original overrides, deployment name mode, and zero extra width with CAS.
Native logout, an independent 401, and the final database barrier followed.

Only the managed-settings revision and update time remain advanced. Every old
row in the other 27 tables remains exact. Two new revoked sessions and one new
ordinary device remain as history, bringing the totals to 96 sessions and 13
devices. No old history was deleted; no playback, planning, scan, task, key
creation, or service restart was performed by this workflow.

## Remaining project scope

M4, M5, M6, and the complete planned server remain unfinished. Broader settings
consumers, policies, providers, audit/log browsing, product backup/restore,
additional task executors, GPU execution, and real-client acceptance retain
their separate requirements. These results do not claim equivalence for all
upstream configuration fields, every codec/profile, or A/V timing behavior.
