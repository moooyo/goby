# Development handoff

Current verified and published source: **source55 passed 2,173 full-suite tests
across 25 packages, zero failures/skips, the Linux build and all six cleanup
checks. Its 98 product-file changes are committed and pushed to `origin/main`,
and the candidate now runs source55/schema28 after a successful independently
attested upgrade. The primary remains source32/schema27.** The real storage-binding management UI gate also
passed its fifth run with all 15 checks, ten IPC stages and complete cleanup.
The latest source55 original-client v2 run passed setup and viewer B login but
failed to observe the target card within 25 seconds after opening Movies.
Its failed scope is now independently sealed, including the exact owned-session
ledger and browser closure. The earlier v1 remains independently sealed.
TOOL03/v3 is being prepared with complete continuation evidence and bounded
discovery diagnostics; original-client acceptance is still open.
Source55 adds the two-file Movies CollectionFolder direct-detail `Subviews`
repair. Remote gofmt made no changes and formal source preflight passed. The
[preparation receipt](collection-folder-source55-preparation.json)
binds 4,243 files to manifest
`7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a`.
The preceding source54 full verification checkpoint passed 2,173 tests across
25 packages, the Linux build, six cleanup checks and separate real private-mount
recovery. Schema28 and the scanner are published and deployed to the candidate;
their scoped real management UI acceptance is complete. All four preceding
failed UI scopes were independently disposed and remain immutable history.
The [source55 targeted report](collection-folder-source55-target.json), SHA-256
`7e13a8cef6c97451311dbb51dd36401d94d2e67e51db12fdeb6b562abc77ee95`,
records two passes, zero failures/skips and six cleanup checks. Its
[terminal](collection-folder-source55-target-terminal.json), SHA-256
`d6ebb651b174caf2712ae247ad28e23bbcaaa8610a001e08e20fa34d04c6aa64`,
is passed. The separate [full report](collection-folder-source55-full.json),
SHA-256 `2c0b5a54b51f66ffa0b11f3160cf50325df520322ec79af1fd92fc9bff93596c`,
and [independent terminal](collection-folder-source55-full-terminal.json),
SHA-256 `7d43a77d904bb212f9231bf427133eb0034a9f74a2187004e18775ccd334880c`,
prove full success. Controller `goby-collection-folder-source55-full-controller-v1.service`
retained invocation `20f0d83200c34228aa5862e07ce24c2c` and exited0/MainPID0
with an empty cgroup. Run `20260912_084241_db776aacc1a7` used worker
`goby-client-backup-20260912-084241-db776aacc1a7.service` and output directory
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_084241_db776aacc1a7`.
Its finished receipt and shared HISTORY record confirm all six cleanup checks.
This scope is terminal and consumed; do not rerun it. Execution evidence is in
`/opt/goby-test/exec-work-m3e/collection-folder-source55-full-execution-01`.
The accepted Linux binary is 29,337,989 bytes, SHA-256
`6a8c46cdd0dcff56af28f11084eabcf2497daf5ce11ac072eaad7a5dbf486e81`.
The 96 storage/scanner files were pushed as
`13b21d60c8bdc4caf8d59abdddc9e2b96dc52775`; the two Subviews files followed as
`16d75c38064008680fa60839c637efee2f12f2ae`. The
[final Git reconciliation](collection-folder-source55-git-reconciliation.json),
SHA-256 `70551338dfd13741f6679944c25de8bcb4463a088c7b048a7eeb55c360136af8`,
checked 849 inputs: 848 were byte-exact, while the historical
`internal/server/testdata/emby-4.9.5.0-playback-video-index-zero.json` differs
only by source CRLF versus Git LF. The [independent EOL verification](collection-folder-source55-git-eol.json),
SHA-256 `5f277f60f87842ccaf6b2b9b89a2117f8ddec31b63822b7335b6979e02442785`,
confirmed equal JSON values and passed
`TestPlaybackInfoRetainsRecordedVideoIndexZeroExtensionCompatibility` in a
separate remote copy with 807 Go inputs. This supplementary single test is not
added to the 2,173 full-suite count, and the 849 inputs are not all raw-byte-exact.
The nine accepted live-verification tools and 47 evidence/documentation files
were subsequently pushed as `ec186aa46291125d2f5d9839019d5bf232b88fb2`.
The four final TOOL05 upgrade source files and 13 safe upgrade evidence files
were then pushed as `212dc387581d9fffddfc7337bbfee1e77c2d04f4`; all 17 index
files matched their frozen raw bytes. This commit contains tools/evidence only;
the product publication authority remains `16d75c38064008680fa60839c637efee2f12f2ae`.
The eight frozen v2 client tools and six safe evidence files were subsequently
pushed as `93e88dc2250d8ccfc094cee6a24e2e5e0da25740`; product authority is unchanged.
The candidate schema28 upgrade **passed** in TOOL05 run
`20260912_100845_47bff13c329b`, with evidence under
`/opt/goby-test/exec-work-m3e/client-schema28-source55-upgrade-20260912_100845_47bff13c329b`.
The [controller report](client-schema28-accepted.json), SHA-256
`328bc6abbf4d2fe0559a34ed62d9e7d9cf7b77c075c583f9070d7aa9761c8832`,
retains `awaiting_outer_attestation`; the independent
[passed attestation](client-schema28-accepted-attestation.json), SHA-256
`ec20d286a1998f0b27667603819853129e88b91579a97a25141bd6258e450032`,
is the final authority. Controller invocation
`3821d3052fc24aa0ad23a3194300cd6b` exited0/MainPID0 with an empty cgroup.

Current candidate: source55/schema28, PID1458051/start ticks13067360,
invocation `d29ea64c63274346a79c7b3a7938f36e`, using the 29,337,989-byte binary
SHA-256 `6a8c46cdd0dcff56af28f11084eabcf2497daf5ce11ac072eaad7a5dbf486e81`.
Current fixture-state SHA-256 is
`bb78a846d2d4b69b7e2550ed9e367549cbe2d0b763e2b69270ea98bed6570d82`;
runtime SHA-256 is
`1f245cd8f8c19dbe0b96b803dc8c60dd0cd4b7b7e2a8d99ccc2541e9430c4a7b`.
The accepted upgrade authority is the successful run's `after-full.json`, SHA-256
`7b61f5c94c440e5f6fefc58f0ef04ee2bd3c8f118510ea727f6741c46488412d`.
The upgrade preserved 75 sessions, 64 devices and 167 audits. The later v2
client attempt added one closed session, one device and two audits, so current
counts are **76 sessions, 65 devices and 169 audits**. Its actual private
`/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v2/after-full.json`
has SHA-256 `a13f976b7097e33337527ef2cf10ad9203d755e0fd2cec43307edfa6efbfe8bc`;
the independently sealed current snapshot is
`/opt/goby-test/exec-work-m3e/client-library-changed-source55-execution-02/independent-after-full.json`,
SHA-256 `6dfe6cbbf2288b21a66466719067b2c0d063bb588e693e06479d20da4feb9cd4`.
These snapshots differ only in `captured_at`. `7b61f5c9...` remains upgrade
evidence, not a current database snapshot with zero session/device/audit delta. The older
`2659f45d...` schema27 snapshot is pre-upgrade history.

TOOL05 passed [94 Python guards](client-schema28-tool05-guards.json),
[seven Go guards](client-schema28-tool05-go-guards.json), its
[helper build](client-schema28-tool05-helper-build.json) and
[read-only preflight](client-schema28-tool05-preflight.json). The actual
[backup](client-schema28-accepted-backup.json) and independent
[schema27-to28 rehearsal](client-schema28-accepted-rehearsal.json) passed, and
the rehearsal pair was ordinarily dropped before the pre-stop baseline was
rechecked. One stop, the [transactional in-place migration](client-schema28-accepted-migration.json),
installation of 58 assets at `/opt/goby-client-m3e/admin`, and one start followed.
Five anonymous direct GETs checked readiness, public info, the index, entry JS
and CSS. All existing columns of the 35 old tables, five sequences, ACLs,
credentials and recovery state were preserved. Four roots begin at revision1
with other binding fields NULL; historical audits have their new-column defaults.
The verified absent-master/zero-application-key state is retained. The primary
at source32/schema27/PID762090, its shared web tree and the proxy are unchanged.
This deployment does not establish original-client acceptance.

Historical upgrade attempts remain consumed and preserved. TOOL01 passed 60 guards and then
[rejected its read-only preflight](client-schema28-tool01-preflight.json) because
it expected complete test lists in the independent full terminal, which retains
strict count summaries. TOOL02 fixed that consumer and passed
[64 guards](client-schema28-tool02-guards.json), its
[helper build](client-schema28-tool02-helper-build.json), and
[actual preflight](client-schema28-tool02-preflight.json).
Its first [actual run](client-schema28-attempt1.json),
`20260912_093201_12f03b28b9a1`, stopped during private backup-material preparation.
The fixture config points to `/var/lib/goby-test/client-m3e/master.key`, but the
file is absent and the complete candidate has zero application keys, key-device
rows and application-key sessions. The operator incorrectly required a copied
master key for this existing empty-key state.
The [independent failed terminal](client-schema28-attempt1-terminal.json),
SHA-256 `d3005e2fb7c3e751770edc7ac242fee66ac5752d13e56303a1865d0986697bfb`,
confirms exit1/MainPID0 and an empty cgroup for invocation
`3740096ebffd4c1b9854ec676fef1a2d`. All rows, sequences, catalog, credentials,
recovery state, candidate/primary processes, media, host, HBA and HISTORY remain
unchanged. No helper execution, pair creation or service action occurred. The
recorded pair is `{database_oid: null, role_oid: null, phase: "absent"}`; no
disposal is needed. Preserve TOOL01, TOOL02 and the partial private backup
materials; never replay the failed actual scope.
Its private terminal, after-state and complete failed-file inventory are under
`/opt/goby-test/exec-work-m3e/client-schema28-source55-execution-02`.
TOOL03 implemented explicit absent-master preservation, passed
[82 guards](client-schema28-tool03-guards.json), a
[fresh helper build](client-schema28-tool03-helper-build.json) and
[actual preflight](client-schema28-tool03-preflight.json). Its second
[actual attempt](client-schema28-attempt2.json),
`20260912_094448_c961207bcea3`, reached the Go backup helper and failed with
`helper_snapshot_dump_failed`. The helper passed a custom `dumpWriter` to
`Snapshot.Dump`, but the existing `backuppg` command accepts only an owned
regular `*os.File` or `*bytes.Buffer`. The sink was rejected before `pg_dump`;
both the partial dump and pending helper report are zero bytes.
The [second failed terminal](client-schema28-attempt2-terminal.json), SHA-256
`2fa536f22ffbcc4ed957fe3fbf1d4fab2b9653056e460113dffc03b5c3f1f5a4`,
confirms invocation `f5e92a8eaea040afb14270ec0949e93f`, exit1/MainPID0 and an
empty cgroup. Complete candidate, primary, media, host, catalog, HBA and HISTORY
preservation passed again; no rehearsal pair or service action occurred. The
first failed scope also remains byte-exact. Preserve this second scope and
its incomplete private files; they are not a usable backup and must not be
adopted or replayed. Independent evidence is under
`/opt/goby-test/exec-work-m3e/client-schema28-source55-execution-03`.
TOOL04 used the owned `*os.File` directly and derived its size/hash afterward;
its [guards](client-schema28-tool04-guards.json) and
[preflight](client-schema28-tool04-preflight.json) passed. The
[third actual attempt](client-schema28-attempt3.json),
`20260912_095333_ca09c36b93db`, completed backup and rehearsal but rejected
equivalent role-property JSON after `RawMessage` whitespace changed during
report serialization. Its [failed terminal](client-schema28-attempt3-terminal.json)
proves no service actions, removed rehearsal resources and preservation of
prior scopes. TOOL05 canonicalizes the exact ten `RoleProperties` fields without
discarding values or weakening the writer/empty-key boundaries. All three failed
actual attempts and their private materials remain separate from the final success.

The source55 original-client v1 tooling completed its final TOOL05 authority
binding. Its [final guards](client-library-changed-source55-v1-guards.json),
SHA-256 `07e7474a223940e9e46083f6616a72ea7a68598c4080afa3277d0231db53e036`,
passed 315 loader, 52 driver and 59 controller cases; the 127 shared-actor checks
belong to the earlier verified layer. The
[Python preflight](client-library-changed-source55-v1-preflight.json), SHA-256
`7191f43ee8f1a70fe6cfc90667a848d3b3cf918c7c834bd99b71a73e6f22f4a7`,
passed with zero HTTP/evidence/service writes against the accepted schema28
authority. These prerequisites did not establish live client acceptance.

The [actual v1 run](client-library-changed-source55-v1.json), SHA-256
`d436def998f500903ebc92ff48e20af2616257cfc8ab61f332bb7449583e3daf`,
failed in discovery before `browser/` creation or login. Its ledger records zero
new sessions, devices, audits or metadata revisions; restoration was not
required. The [independent failed terminal](client-library-changed-source55-v1-terminal.json),
SHA-256 `ecdc5fdf48bf7d471676e6786d52ed61ce1be3053537ce36fde5d44a3d1552a6`,
now records `failed_scope_sealed`, with `client_acceptance: false`. Both exact
controller/worker invocations exited with status1, MainPID0 and empty recursive
cgroups. The browser directory is absent; cleanup was not required. Old rows,
sequences, private state, media, candidate and primary are preserved, with all
four business deltas zero. The failed-file inventory SHA-256 is
`62cc954df4141a3ddb3faf9e99772a9055f7c5f864f68ee1d30728ae00144872`.
The independent private snapshot SHA-256 is
`569bb3779cc634e22b0af29994231105fb6d23f296106cbf1ef30255a7d689bd`;
only capture timestamps differ from the logical upgrade state. The accepted
upgrade authority remains `7b61f5c9...`. This v1 is closed and consumed and must
not be replayed.

Follow-up [setup diagnosis](client-library-changed-source55-v1-setup-diagnosis.json),
SHA-256 `614ab4a5839426a33ca4db287202f10bf0417dca866d271addc2c88360c9a905`,
identified loader assumptions that rejected the frozen catalog/manifest's
actual `0600` mode by expecting `0644`, and treated legitimate index/sequence
columns as ordinary-table columns. The driver's `checkedHomeFile` path also
uses ordinary `JSON.parse`, which rounds large integers. A separate lossless
diagnostic exposed incompatible BigInt cloning; that is not evidence that the
actual v1 failed with a BigInt exception. TOOL02/v2 repaired those setup paths
and passed [348 loader / 54 driver guards](client-library-changed-source55-v2-guards.json),
SHA-256 `bb73a096ccdcd2e46fec871e7eac91007c2fcc334bff43cf97c11d528ff8426f`,
[127 actor guards](client-library-changed-source55-v2-actor-guards.json), SHA-256
`c69caf2ad88daf339e414709634f800b2263b6750a8296c8d183cf2a5576a1a7`, and
[59 controller guards](client-library-changed-source55-v2-controller-guards.json),
SHA-256 `3029eedce04297c4a10a2ec1a786a9168a4dccd00fbfdecb94c6f8417a75ad10`.
The [seven real-document checks](client-library-changed-source55-v2-real-documents.json),
SHA-256 `f27ff9d00c683b4dae080a8e19b81f6d46b76459ba387a1575a234a666816f8a`,
passed against immutable records, the restricted current-file reader and `0600`
files. That check only rebound the v2 scope envelope; it was not an actual new
before-snapshot. The [fresh Python preflight](client-library-changed-source55-v2-preflight.json),
SHA-256 `c0e6b13db1dbdc045bdd41e6fe620c14e633d8dc4a6bcbc2cbd3e4e7003e8068`,
also passed.

The [actual v2 report](client-library-changed-source55-v2.json) and
[browser report](client-library-changed-source55-v2-browser.json) record successful
setup and viewer B login, followed by `library_changed_target_card_not_observed`
after a 25-second Movies discovery wait. Catalog observations counted two
physical requests and two browser frames. The retained report lacks the final
DOM and catalog-response projections, so it does not establish the root cause
or a DTO defect. No native operation or metadata write occurred. The ledger is
one new session, one device, two audits and zero metadata revision delta; B
logged out, the worker/browser/proxy closed, and old rows, sequences and private
state were preserved. The [independent v2 terminal](client-library-changed-source55-v2-terminal.json),
SHA-256 `b23a1a156e781c771e3bb4b1ba31bfb048e29e77504569d129397c79445265d6`,
records `failed_scope_sealed` and `client_acceptance: false`. It independently
confirms the exact ledger, session revocation, UI logout and token rejection,
capability registration and browser/context/proxy closure. Both exact unit
invocations exited with status1, MainPID0 and empty recursive cgroups. The v2
scope/tool files and old v1 files are unchanged; candidate, primary, media and
old rows/sequences/private state are preserved. No additional cleanup was
required. The complete inventory SHA-256 is
`3ef622b6f1724c996177ab7a2673b34c9d16a41608957f8546c0ac24d2085eaf`.
Both failed client scopes are sealed and consumed, without client acceptance.

TOOL03/v3 is being developed to bind the complete v2 terminal and exact
1-session/1-device/2-audit continuation ledger, rather than changing counts
alone. Its bounded discovery diagnostics will retain the last DOM, reads,
public API projections and a safe screenshot without relaxing target-card
matching. Remaining verification and a fresh live run are still required.
The [main schema28 upgrade plan](main-schema28-upgrade-plan.md) was reviewed and
published as `933257c913cd36a57c26c72bb324f73b57441ecd`. Its four tools are being
implemented; their verification and main-environment deployment have not occurred.
The consumed source44 browser scope and its older population counts cannot be
reused by changing only process or binary pins.
The preceding source44 product checkpoint passed 2,002 tests;
its exact 27-file increment was published as
`35ae3d000f812fa18d234921cedb3c33d88190e0`.
Source41's failed staging was replaced only after its repair passed full
verification. Source38 (`c3fb084d2740cbeebc3499bacc8513f5077cd5ea`, 1,953 passes)
is an earlier product checkpoint. Schema28 publication and candidate deployment
are recorded above; the primary remains on its separate source32/schema27 baseline.
Source48 passed all 143 targeted PostgreSQL checks after correcting source47's
two fixture assertions. Source49 full regression is terminal: 2,105 top-level
passes and two failures in the schema23/schema24 encrypted recovery transition
tests, both at historical administrator bootstrap. The current audit INSERT
uses schema28-only columns against the historical fixture. Source50 isolates
the test adapter fix and passed source preflight. The exact failed pair has
subsequently been disposed with original evidence preserved. A combined
source54 PostgreSQL target passed all 74 tests; it includes that fixture fix and
the new scanner implementation. Source54 full regression/build subsequently
passed, with its exact pair removed. A separate actual mount attempt stopped
before namespace dispatch because its host-witness reader assumed a missing
sysfs block directory. Its exact empty pair was independently disposed, with
the original receipt and evidence preserved. The corrected operator then
passed a fresh actual mount run and all six cleanup checks. All three scopes
are terminal and consumed.

The historical [source44 candidate continuation](m3e-source44-candidate-continuation.json)
**passed** at 2026-09-12 04:42:59 UTC. The candidate then ran source44/schema27,
PID1264063/start ticks11104222, service invocation
`c0a5244ae25646c8bd92c3e3c1636575`, binary SHA-256
`cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d`.
Its fixture state was ready/complete, SHA-256
`d6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4`.
Report SHA-256 is `4b426981fa6cc188ca0401c8b28005aeb871246390f1594ef66cd39ab550798e`;
completed receipt SHA-256 is
`5ade36722b8e2af81f71ae80006039c230b574ca54b8cf3c144a3ff2216fc596`.
The [controller terminal](m3e-source44-candidate-continuation-terminal.json)
confirms exit0/MainPID0 and an empty cgroup; SHA-256
`08d91a6611b5d2f1cac425528c60054235770fde57a4950414416dcc92a362b0`.
All business rows, sequences, catalog, credentials, recovery files and three
media groups were preserved. The original failed upgrade evidence is immutable.
Primary remains source32/schema27, PID762090; SSH is restored. The later schema28
candidate deployment is recorded above. Original-client automatic-refresh acceptance
remains open. Its first live attempt is now terminal: the client raised a
TypeError on opening Movies, before any native login or metadata edit. Normal
UI logout and exact-token 401 passed; the complete ledger permits exactly one
new session, one device and two audit rows. That attempt ended with
74 sessions, 63 devices and 165 audits. Its private snapshot
is `client-library-changed-ui-source44-v1/after-full.json`, SHA-256
`1277a8034d738ed451cf99e71b88c53e31c9731576bca7fbae16085e573ce870`.
The later public CollectionFolder capture below advances this baseline again.
Do not replay either consumed v1 scope or compare a new snapshot directly with
the older 73/62/163 or 74/63/165 totals.

The [public CollectionFolder capture](collection-folder-contract-capture.json)
passed with 18 HTTP requests after 57 remote memory guards and a zero-HTTP
preflight. Both fresh ordinary sessions completed logout204 and exact-token401;
candidate business rows, sequences, private state, media and primary were
preserved, with exactly one added closed session, one device and two audit rows.
That capture left **75 sessions, 64 devices and 167 audits**, which the later
upgrade preserved. Its historical schema27 authority is
`collection-folder-contract-v1/after-full.json`,
SHA-256 `2659f45dfa82d8568b07375d04c08cd4ba216defcfa2a4a1c269867b176573be`.
The accepted schema28 upgrade and subsequent v2 continuation are recorded above;
new candidate work must account for both, rather than reuse this historical baseline.
The [independent terminal](collection-folder-contract-terminal.json), SHA-256
`289b58f779489f9af488bc14e5075c9f0d4b120797b9daa9bffb273d491a100e`,
confirms exit0, MainPID0, the original invocation and an empty recursive cgroup.
The service is `goby-collection-folder-contract-v1.service`, invocation
`ee935d93ff98437f8f392ef450a2fbe2`; this scope is terminal and consumed.
Report SHA-256 is
`d2260c109eaba6859e3291f01096c9a8db68bb5521c88ab7a8a8b5597d1fb384`.
Reference research used the existing public proxy and did not read reference
implementation or database content; the proxy's existing internal identity
checks were unchanged. No reference-database preservation or full-browser
profile equivalence is claimed.

The retained [reference projection](collection-folder-contract-reference.json)
has `Subviews: ["movies", "movies", "folders"]` on the direct Movies
CollectionFolder response; the [candidate projection](collection-folder-contract-candidate.json)
omits it and other reference fields. These are complete public responses for
default and field/switch requests. Source55 implements only the observed Movies
direct-detail array, preserving its order and duplicate value. Views, ordinary
item lists, other collection types and generic projection switches are unchanged.
Other DTO differences and the specific cause of the prior client `includes`
error remain unproven. Source55 targeted/full verification, publication and
candidate deployment passed. Client v1 failed before login and is independently
sealed. The later v2 passed setup/login but did not observe the target Movies
card; its failed scope is independently sealed. Complete TOOL03/v3 continuation
binding and diagnostics before a separately verified new live scope.

Later scan reconciliation work is separate from frozen source49. Source53's
original-storage recovery and bounded directory-evidence helpers passed 20
selected non-database race checks. Source54 integrates all-roots orchestration,
cache-hit tracking, positive absence, cascade validation, transactional deletion
and notifications. A shared music-readiness gate checks proposed surviving
members before deletion; actual album publication remains afterward. Its 28
selected race checks and related test-package compilation passed without
database access, followed by 74 passing PostgreSQL checks and the complete
2,173-test regression/build and actual private-mount scan recovery. The separate
real management UI gate below also passed; publication and candidate deployment
are complete. Original-client acceptance and broader release work remain open.
This does not establish recovery
across a system reboot or on other filesystem types.
The [real management UI gate](storage-binding-live-ui-acceptance-plan.md) passed
run `20260912_083703_5a77d18d2ae0` with the existing source54 binary and built
administrator UI, a fresh schema28 database and a private mount namespace.
The [browser result](storage-binding-live-ui-accepted-browser.json), SHA-256
`a812dbdfe16dfd4183e5319182ed1c3ee3371beb2538e2ab6914e62ae625e479`,
proves 15 checks and ten ordered IPC stages. One real Playwright test completed
in 16.133 seconds, with zero skips or retries. UI logout/exact401 and the
controller's separate session cleanup passed. All four worker cleanup checks
and all ten controller cleanup checks passed: database, role and runtime were
removed, while HBA, global catalog, host, candidate, primary, history and inputs
were preserved.

The [controller report](storage-binding-live-ui-accepted.json), SHA-256
`a28c62e56630df9a7bff0dcfac0b00ecd57450f235d7f9e2fbdd5b69901c2d95`,
retains its `awaiting_outer_attestation` publication state. The independent
[accepted terminal](storage-binding-live-ui-accepted-terminal.json), SHA-256
`7d5755df17cd4f10b869628897dce0a496cd99df51a5ffc3c842faa8b5b49b38`,
closes that gate with `passed`. Controller invocation
`a7af29564e974d41aac27bdf546cc9c5` and worker invocation
`f7ada25209944cfbaa4a92c39ebba535` both retain exit0, MainPID0, active/exited
state and empty recursive cgroups. The retained [desktop](storage-binding-live-ui-desktop.png)
and [narrow](storage-binding-live-ui-narrow.png) screenshots were visually
checked: long paths wrap, action buttons remain visible and the narrow layout
has no horizontal overflow. The narrow capture is a scrolled view.

TOOL05 passed [90 guards and two syntax checks](storage-binding-live-ui-tool05-guards.json),
[single-test discovery](storage-binding-live-ui-tool05-web-list.json) and
[input preflight](storage-binding-live-ui-tool05-preflight.json). Runtime mode
is `0750`; the application directory remains `0700`. The four preceding failed
attempts and their ordinary disposals remain separate immutable evidence:
[attempt1 disposal](storage-binding-live-ui-attempt1-disposal.json) followed the
failed `0710`/`O_RDONLY` assumption; [attempt2 disposal](storage-binding-live-ui-attempt2-disposal.json)
followed the sequence `to_jsonb(s)` failure, repaired with explicit
`last_value`, `log_cnt` and `is_called`. [Attempt3 disposal](storage-binding-live-ui-attempt3-disposal.json)
preserves the unsupported-storage fixture failure: tmpfs actually supported the
tested identity operations, while the later [ramfs probe](storage-binding-live-ui-ramfs-capability.json)
returned ioctl `ENOTTY` and handle `EOPNOTSUPP`. [Attempt4 disposal](storage-binding-live-ui-attempt4-disposal.json)
preserves the ambiguous `Unavailable` text lookup, subsequently scoped to its
alert. Its original catch did not retain the stack, so that ambiguity is not
claimed as the unique cause.

The accepted UI scope is consumed. Later source55 Go verification may legally
advance the shared HISTORY file. Its earlier `78cb...` state is retained in
`history-before.json` in the source55 target execution evidence; do not rerun
the old UI attestation against the newer history. Fresh client tooling and
fresh original-client navigation/automatic-refresh acceptance are still open.

The source44 automatic-refresh tooling passed 124 shared transport guards and
342 TOOL02 guards: 49 controller, 248 fixture and 45 browser checks. Its corrected
zero-HTTP preflight passed before the first live attempt. That attempt exposed
a real client library-navigation failure; the pure checks do not establish
automatic-refresh acceptance. The observed failing page consumed a 200 JSON
response for the virtual Movies CollectionFolder, then raised
`Cannot read properties of undefined (reading 'includes')`. Existing public
Views fixtures showed missing CollectionFolder fields. The later direct public
capture above now supplies both complete DTO variants, although it does not
identify the particular causal field without a repaired real-client run.

The accepted source37 ordinary-scan increment contains
transactional Added/Updated facts, effective folder comparisons, explicit probe
invalidation, and same-library moves retaining both parent scopes through
`PreviousParentID`. Source36 passed [14 pure race tests](m3e-library-changed-scan-pure-verification.json),
report SHA-256 `8933a1df780bae0b1ccd3c728865dfcbd75b0b82fe2cb32aa169cbba19b98be3`.
Its [PostgreSQL runner manifest failure](m3e-library-changed-scan-manifest-failure.json)
occurred because `files` was a list instead of the required object; the runner
stopped before database setup. Preserve that failed scope. Source37 retains
identical source bytes and corrects only the manifest representation: 4,154
files, manifest SHA-256
`153018fb7e0b5308d727dcc7b1794bc9769ba71e24c2b783a23941595291813a`.
The [formal source preflight](m3e-library-changed-scan-source-preflight.json)
passed before its fresh PostgreSQL run.

The [source37 scan verification](m3e-library-changed-scan-verification.json)
passed 80 top-level tests, zero failures, zero skips and all six cleanup checks.
Its report is
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_005317_90f76937a793/report.json`,
SHA-256 `60c5b3b69f234cb52201ed17531a8015a351f86bac9d00b08b59b03198ce911d`.
Controller `goby-library-changed-scan-controller-v2.service`, invocation
`b9f73f0231604d758bfaf6fd3c104dde`, exited0 with MainPID0; its former PID978105
is historical. Execution records remain under
`/opt/goby-test/exec-work-m3e/library-changed-scan-execution-02`.
Terminal receipt SHA-256 is
`962a578e7e5fae442e4ca2b1b9bf0d9613e3ba1c15d23fb7e5f42da85b9ea244`.
These are ordinary-scan and real HTTP/WebSocket integration checks, not
original-client UI acceptance. The separate full-suite result follows.

The [source37 full regression](m3e-library-changed-scan-full-verification.json)
passed 24 packages and 1,920 top-level tests, with zero failures, zero skips and
all six cleanup checks true. Its run ID is `20260912_005623_3e09ff8ff85d`; report:
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_005623_3e09ff8ff85d/report.json`.
Report SHA-256 is
`3c99ecc06a5d8f0d184fc50e7dcc5a6737af80f3a532e8225db7c051236c064b`.
The run's `tmp/goby-linux-amd64` is 28,266,227 bytes, SHA-256
`8172549d40cf5ddf36d756baa411adb291cfd3b2e41a827cc19aa12678e693d3`.
Controller `goby-library-changed-scan-full-controller-v1.service`, invocation
`d477192e74604dd290af3e7d6c3d212a`, completed with exit0, MainPID0 and an empty
cgroup; its former PID979794 is historical. Execution records remain under
`/opt/goby-test/exec-work-m3e/library-changed-scan-full-execution-01`.
Terminal receipt SHA-256 is
`a76176e12c7fdc87d4016bac7311f4743a443d2632ee9c9285e6e7fb8966b194`.

The accepted ten-file source37 checkpoint is published on main as
`7e2e9af8c86fbfbb0b5d557d0390c4ef98fe052f`. Source38's 19 Go files form a
separate accepted increment. Source38 adds post-commit image/subtitle projection changes,
derived album changes and inherited Audio/theme references, an extras scan
lock-order lease fix, and the low-level root-identity adapter. Its remote
formatting and [formal source preflight](m3e-library-changed-aux-source-preflight.json)
passed for 4,167 files, manifest SHA-256
`e411a04c478055f99f34cbef1dde3c05a885c5131681152e3f382f4fcc3c665b`.
These source checks do not establish PostgreSQL or full-suite success.

The [source38 targeted checks](m3e-library-changed-aux-verification.json) passed
95 top-level tests with zero failures, zero skips and all six cleanup checks.
Report SHA-256 is `3645b4e6d066a2b303b0baa7486869dd899e746ec3d955026e1ff976fe1ecd01`:
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_012842_0cfb5473113e/report.json`.
The controller `goby-library-changed-aux-controller-v1.service`, invocation
`9314e7571edf43ad81ec062b88d9a147`, exited0 with MainPID0 and an empty cgroup.
Its terminal receipt SHA-256 is
`43a58b42154d8365d2f06d9e08c046f6580214c559971f9d4e492eaa3700c7b8`.

Source38 [full regression](m3e-library-changed-aux-full-verification.json)
**passed**: 1,953 tests across 24 packages, zero failures, zero skips and all six
cleanup checks true. Report SHA-256:
`d090026061703c819c15cf9c7e6fd6d44a29e13beba633266b4f6b19588ecfc0`.
Controller `goby-library-changed-aux-full-controller-v1.service`, invocation
`d7760f6de7de42638b47348df631dac9`, with execution records under
`/opt/goby-test/exec-work-m3e/library-changed-aux-full-execution-01`.
Run ID: `20260912_013304_de10a2ccd535`; report:
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_013304_de10a2ccd535/report.json`.
The controller exited0 with MainPID0 and an empty cgroup; PID1013204 is
historical. Terminal SHA-256:
`d8d6afc174b9f925bcc0fd4ee20af827313bf55b0088453290624ee95779e9b4`.
Its 28,297,205-byte `tmp/goby-linux-amd64` has SHA-256
`2928ff156a17ad94c7e78d8edd0f9b4484090c5d8aa6894e8de9fca37110ca81`.
The 19 staged Git blobs matched this verified source before publication.
No deployment is claimed.

Source40 is a separate 11-file theme/extra notification increment based on
source38, including marker visibility, resource/owner changes, inherited Genres,
ordinary-owner child retirement and native metadata edits. It separates semantic
owners from browse parents and merges compatible folder membership invalidations.
Its [preflight](m3e-library-changed-resources-source-preflight.json) covers 4,172
files, manifest `e4eb47609e273343850f799809f00142fa2f1bde31315ef01caaadf69d5e35a6`.
The [pure race checks](m3e-library-changed-resources-pure-verification.json) passed
12 tests across library/server, report SHA-256
`fd32af6957d67a57f85f26dedc296e69425b67bcf6bea5577a8529ac2a947732`.
The [PostgreSQL targeted run](m3e-library-changed-resources-verification.json)
passed 169 tests, zero failures/skips and all six cleanup checks true. Run ID:
`20260912_020339_b6e473591059`; report SHA-256:
`0b91c60e2649eddf2e9edf5afd692d4f4dd2a3256feae1d4561cf39d4f1dc106`.
Controller `goby-library-changed-resources-controller-v1.service`, invocation
`88f2bf72b124444c9784ef228aa9e51f`, execution directory
`/opt/goby-test/exec-work-m3e/library-changed-resources-execution-01`.
completed with exit0, MainPID0 and an empty cgroup. PID1044627 is historical.
Terminal SHA-256: `9e79a456f6d63289786f0872cbff6ac04bc61de85d32558dd30314ebd0c1e212`.

Source39 separately froze the eight root topology files on source38. Its
[preflight](root-topology-source-preflight.json) passed, but its
[first race run](root-topology-go-verification-failed.json) failed: 15 passed and
four filesystem tests rejected a valid external nsfs mountinfo root name.
Report SHA-256: `e0cd3435405b21fdd550980045d4e9ccbf38bd689029c83707ceff7f25572e0f`.
The failed unit `goby-root-topology-go-v1.service`, invocation
`1862792e16c6410fb0d234f09c647bf9`, is terminal with exit1, MainPID0 and empty
cgroup. Keep its evidence at `root-topology-go-verification-01`. Source40
excludes these files.

The parser fix recognizes only canonical namespace dentry names for nsfs.
External namespace mounts are excluded; related namespace dentries still fail
explicitly. Source41 combines source40 with corrected topology: 4,180 files,
[manifest](root-topology-retry-source-preflight.json)
`7f43c30bcd7dcfb81523e9c683030f6d4a3c0e869d3dadfa6cef3bf2727cbc26`.
The [fresh topology race run](root-topology-go-verification.json) passed 22 tests,
zero failures/skips, report SHA-256
`52881360e7561cbc8f38da4c724396cdbeab2d33e8d4b7fbe1e89582e70a7677`.
Unit `goby-root-topology-go-v2.service`, invocation
`f0d224f5695148d88ce96ace4368f12b`, exited0/MainPID0 with an empty cgroup;
terminal SHA-256 `311113cb569f57454904c7f4087186b0eaed11a38b83ff748e41f4dd5fe20e66`.
Real nested-mount, system-reboot and binding acceptance remain open.

The 19 source41 Go files were staged but never published in that form because
its [full report](m3e-library-changed-resources-full-failure.json) is **failed**.
Controller `goby-library-changed-resources-full-controller-v1.service`,
invocation `ac1f59aa96004757acd921e1469241db`, execution directory
`/opt/goby-test/exec-work-m3e/library-changed-resources-full-execution-01`.
Run ID `20260912_021111_a558c977634d`; report:
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_021111_a558c977634d/report.json`.
The controller is terminal with exit1, MainPID0 and an empty cgroup; PID1050857
is historical. Report SHA-256:
`c32a2114c1ee5dda25dee095d60c230cae13db6b9fd893352829fc365df57118`.
Terminal SHA-256: `46dbe57ab11e1ebe01be92740e57a39fa754ecfb6258d345a41b137f6a1b5d87`.
The [failure summary](m3e-library-changed-resources-full-failure-summary.json)
records 1,981 passed and two failed top-level events across the first 23
packages. The follow-on recoverydb run and Linux build were not reached.
`TestHTTPLibraryRefreshDurablyQueuesEveryLibraryBeyondScannerCapacity` and
`TestManagerRetriesCapacityAndIndependentScansBeyondTerminalPage` timed out.
The original report, final Go log, receipt and evidence trees are retained.
No full-suite success or deployment is claimed.

Inspection found two unnecessary complete auxiliary snapshot queries for every
empty collection-theme publication. Source44 skips that work only when both
the expected and retired resource populations are empty; existing transaction
checks and deadlines remain intact. It also adds bounded, failure-only task
diagnostics. Its [preflight](m3e-library-changed-capacity-source-preflight.json)
passed for 4,186 files, schema27, manifest
`c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b`.
Source44 is based on source42, not the newer schema28 draft. Its
[targeted PostgreSQL run](m3e-library-changed-capacity-verification.json) passed
all three tests, zero failures/skips and all six cleanup checks. Run ID:
`20260912_031154_10da2d3df9f3`; report SHA-256:
`565da128365d8eebd994c81255ace550c4bb6b54c4704c456707496e414bf403`.
The capacity cases completed in 6.40 and 8.09 seconds without changing deadlines;
the real HTTP/WebSocket case completed in 19.41 seconds. The
[terminal record](m3e-library-changed-capacity-terminal.json) confirms controller
invocation `787656ba3d9b449f8cd8ccd4204fcb1a`, exit0, MainPID0 and empty cgroups.
Its SHA-256 is `2a6d5426dfd519909b456f1583b33b9a06fd631770bd819a4042cec55f219c65`.
An independent static review found no actionable issue in the empty-theme guard
or failure-only diagnostics. The later full pass allowed replacement of the old
source41 staging by all 27 exact source44 Git blobs before publication.

Source41's retained empty test pair was removed by its independently reviewed
operator; the original failure evidence and receipt were preserved. Disposal
report SHA-256: `ff5b1b9d602bcbfc7f33f52770616edee2d5feff838ce781a7be8c9784b5c65a`;
control attestation SHA-256:
`976c1a877bafa2a2b46e8603099965a21c1bebbf392343acc4d1022a05dc42bb`.
Do not replay that disposal scope.

Source44 [full verification](m3e-library-changed-capacity-full-verification.json) passed under
`goby-library-changed-capacity-full-controller-v2.service`, invocation
`d18910a201d44bbfa9bbb77cd9cb8c25`, execution directory
`/opt/goby-test/exec-work-m3e/library-changed-capacity-full-execution-02`.
Run ID: `20260912_032033_aeb954fd9c60`; 2,002 top-level passes across 25 packages,
zero failures/skips and all six cleanup checks true. Report SHA-256:
`2d82c22f5d335314373cd042de5f8a75f66706e5dae87d7126ec05427c79fe16`.
The controller is exit0/MainPID0 with an empty cgroup; terminal SHA-256:
`46eeeda58a44b691a05346f9db9c38a6b8f363dc0e7deb4ba0d8db8b01c86a86`.
Its 28,357,495-byte Linux binary has SHA-256
`cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d`.
It is retained at the run's `tmp/goby-linux-amd64`.
The first launch in `library-changed-capacity-full-execution-01` failed before
database preparation because its unit did not inherit `SSH_CONNECTION`.
That failed launch is terminal and preserved; v2 forwards the authentic SSH
connection environment. Both launch scopes are terminal. Publication is complete;
deployment remains pending. Candidate and primary still run source32/schema27.

Source42's [shared model and alias checks](storage-binding-model-go-verification.json)
passed 46 race tests; report SHA-256
`b7427669617a66cc283fce6b011198746c5d9b98da5cefae58fda9f6ebe680c4`.
Its [real isolated bind-mount acceptance](root-topology-mount-verification.json)
passed six phases: nested capture, loss, replacement, original-source remount,
addition and stacked-mount rejection. Host mountinfo and namespace were unchanged
and the owned fixture was removed. Report SHA-256:
`8e3588a232a9838523485c587769e8d8a57bef28cacd31f6cefc6f96a800eb40`.
System reboot and other filesystems remain unverified.

Source43 is the separate schema28 bootstrap draft, [4,198 files](storage-binding-schema28-bootstrap-source.json),
manifest `b6f987c100ff28dc2bc1600fcdb778e7bd52691b7d20dd407ddd09cb18e97585`.
It adds binding columns, strict shared document validation, backup validation,
dedicated audit facts and current-schema test updates. Its [20 pure tests](storage-binding-schema28-pure-verification.json),
[frontend typecheck/build and 31 mocked API tests](storage-binding-audit-web-verification.json),
and [112 runner guards](storage-binding-schema28-runner-guards.json) passed.
The subsequent catalog generation below completed; this draft's pure/frontend
checks alone did not establish PostgreSQL acceptance.
Source45 subsequently includes the archive integration test, source44 fix,
read/write binding services, native routes, root-specific anchor publication and
dedicated tests. Its [bootstrap preflight](storage-binding-workflow-bootstrap-source.json)
covers 4,215 files, manifest
`e4ff472e027ff87e50b6e1a4f97b2d2d34ff2cdc31a29fb6e5cf6ba120882d48`.
It still has no catalog28 and is not a PostgreSQL-accepted final snapshot.

Source45 [passed 36 selected race checks](storage-binding-workflow-pure-verification.json)
for read projections, real named-directory observations, input validation,
per-root anchor lifetime/isolation and HTTP decoding/DTOs. The backuppg and
recoverydb test packages compiled without running database tests. This run used
no database and an isolated network namespace. Report SHA-256:
`830b08477a777c2f08a35371d138071ba271ae6e25930f595c39ef405e9bcb81`.
Controller `goby-storage-binding-workflow-pure-v1.service`, invocation
`bf74d340826741ef98a731b2e5078279`, is exit0/MainPID0 with an empty cgroup;
terminal SHA-256 `67adb7a2d8ebd791e97a3907b92626f15f17df7b4da3fc6eb355e97b150075ad`.

The implemented native update independently captures current storage, compares
an exact revision/fingerprint, rejects active scans, rechecks current native
administrator authority, and commits binding plus audit atomically. It installs
the retained anchor for only that root while scan/media admission remains locked;
other roots sharing the configured path retain their original anchors. This
code and its database/real HTTP integration tests still await PostgreSQL acceptance.
Automatic binding during new-library registration is a later source46 increment,
described below. Ordinary missing-file reconciliation remains unimplemented.

Two post-source45 test corrections must enter the final snapshot: native
authorization failures after middleware expect the existing `401/unauthorized`
mapping, and the recovery reset identity case changes only `storage_binding`
so a revision change cannot mask incomplete fingerprint coverage. Source45's
36 selected checks did not execute these corrected database cases.

The administrator dialog and separate API decoder passed the first
[68-check frontend run](storage-binding-workflow-web-verification.json): 58
decoder cases and 10 mocked browser workflows, with typecheck/build success.
Visual review then found an excessively tall selected-value control on narrow
screens with long paths. The control was clamped, complete details retained,
and storage status plus refresh moved before those details. The second
[run failed one new accessibility assertion](storage-binding-workflow-web-accessibility-failure.json):
MUI exposes the field label as its accessible name. Complete selected values are
now linked through the accessible description, preserving the standard label.

The final [frontend and accessibility verification](storage-binding-workflow-web-accessibility-verification.json)
passed all 68 checks, zero failures/flaky/skipped, typecheck/build, unchanged
source/input hashes and desktop/narrow-screen visual inspection. Controller
`goby-storage-binding-workflow-web-v3.service`, invocation
`f8598a81c74545c49c06e5ddb79b54b3`, is exit0/MainPID0 with an empty cgroup.
Execution directory: `/opt/goby-test/exec-work-m3e/storage-binding-workflow-web-03`.
Report SHA-256: `2b33e2805afb7422eeb8486db9c4f531cd074a636e4dcddc08c91c8d0808a9d7`;
terminal SHA-256: `223053d30128dae16fa1bb96fc0577492ce2c33aa613a8a13ee2e05808f1a61b`.
The verified external web distribution is that directory's `dist`; Goby's Go
binary does not embed it. This used an isolated network namespace and mocked
every native API request; live binding, scan deletion and deployment remain open.

Source46 adds initial registration capture at revision1, typed historical actors,
fresh per-root handles and atomic commit/publication to native, Emby and trusted
system registration. Only explicitly classified unsupported capability or
topology limits can remain unbound; unrelated I/O, cancellation, changed names
and invalid mappings cannot enter that fallback. Existing unbound fixtures were
made explicit. Its [bootstrap preflight](storage-binding-registration-bootstrap-source.json)
covers 4,226 files, manifest
`d901d94b1bfb0f369c1119d3238cf20e486af448ae04c15bb5cc1e4a6a760a15`.
The [46 selected non-database race checks](storage-binding-registration-pure-verification.json)
passed, with both backup test packages compiled only. Report SHA-256:
`fe6f01e47899fb49ab30cd8d3ff78757920c2d6070d49ba48c7ddc0f22798292`;
terminal SHA-256 `5cd512592a355060be2ab55c16c22c74790d5ac0a451e80588675b0db2df9f86`.

The actual schema28 [catalog generation](storage-binding-schema28-catalog-verification.json)
passed using source43's unchanged migration chain, with all six cleanup checks.
Run `20260912_035604_e246403236db`, controller
`goby-storage-binding-schema28-catalog-controller-v1.service`, invocation
`da709ecc7b2b40dfaa7a1a55e3c73ae4`, is terminal exit0/MainPID0/empty cgroup.
Report SHA-256: `a3dfa24749d8003739b8f51e5db1070a72f45ffb989cbaa47a0ca12b81baa786`;
terminal SHA-256: `97051b65b30f51fb2d47fe3e5be0fb6b33d37750e4e86e7feb25cf003901d3fa`.
Generated `internal/backuppg/catalogs/schema-28-postgresql-17.json` SHA-256:
`8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b`.
Historical schema23-27 catalogs remain unchanged.

Source47 combines source46 with that exact catalog and passed the final
[source preflight](storage-binding-schema28-final-source.json): 4,227 files,
manifest `a0bb8413680da27cee7f2159bf34eda1405ddff4ae57f2564747717f03bf8831`.
Its PostgreSQL integration run failed under
`goby-storage-binding-schema28-controller-v1.service`, invocation
`6693db24aa2d4c778d8872713701a4ec`, execution directory
`/opt/goby-test/exec-work-m3e/storage-binding-schema28-execution-01`.
Run `20260912_040039_8ec465c99d0b` is terminal exit1/MainPID0/empty cgroup.
The [failure report](storage-binding-schema28-targeted-failure.json) and
[summary](storage-binding-schema28-targeted-failure-summary.json) retain 141
passes and two failures. Database, backuppg, activity, server and tasks packages
passed; the library package failed its old schema27 metadata-migration assertion
and a new media-open fixture lacking the current probe version and ctime.
The complete original log SHA-256 is
`88ebe7f72a95f10de52fe422ebb99e6475825080845ce416c4f794e7126d9870`;
report SHA-256 `9bcf5a32a626b1aea89216b6f809e859ac9e758032dc0db5bc63bf433ca2efe1`;
terminal SHA-256 `8a76222c25bf9c6dfecbd34d070df25b30edc44cb790fc2239ebca291921bfb9`.
The retained pair receipt SHA-256 is
`57ade271512651ff8a9179f1453385f684c877e5a64febcf9c58f28e4c79b423`.
Its [reviewed disposal](storage-binding-schema28-disposal.json) subsequently
completed after [14 memory guards](storage-binding-schema28-disposal-guards.json).
Disposal report SHA-256:
`ff4524bfdebc72f15ff507b88ccd665bafa1d565c9dc64f4929557e64b3697cd`;
attestation SHA-256: `5b45ab5d2fb3f0e59ad0aa1e7338385c26c755e3b6ef2032031bc87134ef9ac0`;
terminal SHA-256: `564fbbab5015372d9a7c3bd7ff15988725a69d11ec811f2c35b73f5116ba9b17`.
Controller `goby-source47-disposal-v1.service`, invocation
`704330898a7447d18d4f4bb9668f52e1`, exited0/MainPID0/empty cgroup.
Original receipt and failure evidence were preserved. Do not replay disposal.

Source48 changes only the two test fixtures: the historical migration comparison
now expects schema28, preserves every old root field and checks the new unbound
defaults; the rebind/media fixture uses the existing mediaSourceTestProber to
record the current version and real file ctime. Production source is unchanged.
Its [preflight](storage-binding-schema28-fixture-fix-source.json) passed for
4,227 files, manifest
`128201c5e098d0253c52566901a12d7461543f758b76b06f35d857b7b28a8e96`.
The [same targeted scope passed](storage-binding-schema28-targeted-verification.json)
all 143 tests, zero failures/skips and all six cleanup checks. Run ID:
`20260912_041811_d761451e139a`; report SHA-256:
`9df8655cf130c78cc7b22dc4a62392a0e1d62c9b54849674afe77b46507938c9`.
Controller `goby-storage-binding-schema28-controller-v2.service`, invocation
`1d5f06c1194e44dfa24ea5cd8538c0ac`, exited0/MainPID0/empty cgroup;
terminal SHA-256 `3da416ae32d84b50bacdef7ddcf8a4e70015e4be63ce01d7c1f0d0d08b6a47f5`.

Before full regression, a static sweep found the same outdated latest-schema
expectation in settings/compatibility_migration_integration_test.go. Source49
updates that test's terminal version/history count and separately verifies new
unbound root defaults while preserving schema20 data. Its
[preflight](storage-binding-schema28-full-source.json) passed for 4,227 files,
manifest `a22a89b460a1bf5d4033336ea57fd7a8e69975c01bf883ad54fccf1cd5c6ee3a`.
Production source is unchanged from the targeted-accepted source48.
The [full regression](storage-binding-schema28-source49-full-failed.json) ended under
`goby-storage-binding-schema28-full-controller-v1.service`, invocation
`769e7b698560419d906a7df1838f8781`, execution directory
`/opt/goby-test/exec-work-m3e/storage-binding-schema28-full-execution-01`.
Run ID: `20260912_042841_89ef5954034a`. It passed 2,105 top-level tests, failed
the two historical encrypted recovery cases and had no skips. There were 23
passing packages and one failing package. Recoverydb was not executed and the
Linux build artifact does not exist. The [terminal receipt](storage-binding-schema28-source49-full-terminal.json)
has SHA-256 `a6dde10cb27f1408a28faaaf12c7d0904f6fa72758bbaf68fe8777a808db92c5`;
the report SHA-256 is
`4d61ae8737438ccf03a11b43983898cb23a60a0f3ec2b64781b7ea1fc49f03be`.
Both controllers have MainPID0, exit1 and empty cgroups; inner invocation is
`f9cf1ffaf283452ebf58d23e2b1a6b8c`. HBA restoration passed; pair evidence remains.
The subsequent [read-only inspection](storage-binding-schema28-source49-disposal-inspection.json)
established that both exact databases were empty. Source role/database OIDs
were 16410704/16410705 and target OIDs were 16410706/16410707. The target public
namespace had been recreated as OID17186575, while source remained OID2200;
the disposal used these actual identities without normalization.

The separate source49 disposal [guards passed all 27 cases](storage-binding-schema28-source49-guards-03.json)
and two syntax checks, with zero live database commands or filesystem mutations.
The first guard launch omitted its required operator argument and rejected
before any test. The second ran 26 cases but one malformed-JSON case hit
CPython's lazy decoder import under the strict effect fence. The corrected
guard uses a private standard-library Python scanner/string/object decoder;
no import allowance or production operator change was introduced. Both earlier
guard records remain preserved. Final operator SHA-256 is
`ceccddd403adb48a7dadacc08da46cde4e026603710a3c93a135b3b889a508c4`;
guard SHA-256 `e78c27cb2e11faabbae397d48b87cac97412c8189cfea4574ead4a462f5d332c`.

The [actual disposal](storage-binding-schema28-source49-disposal.json) passed,
report SHA-256 `1caceb68ca04571dae791d9aaafe57022872ec2561a60b6589bb8696fbb3f0cc`.
Both identities are absent, all preexisting catalog metadata is unchanged, and
all original evidence and retained receipt bytes are preserved. The separate
[attestation](storage-binding-schema28-source49-disposal-attestation.json) has
SHA-256 `818e71ddc6a4f393d308ba06ae9d2c3045829f48d58e49a35872a6d21e691aed`.
The [disposal controller terminal](storage-binding-schema28-source49-disposal-terminal.json)
has SHA-256 `6fe2bc625b54fe68884e24c1fade54f8dae368a4c01c1b12d103c567c21b5c66`,
invocation `0460fe72ee9e44ddbe34e69ce7a1046e`, exit0/MainPID0/empty cgroup.
Never replay that consumed disposal. A subsequent runner may replace the live
control receipt only after validating its separate attestation.

Source50 changes only manager_schema23_transition_integration_test.go. A private
fixture view and INSERT trigger route neutral current audit fields into the
unchanged historical public table. The dedicated pool and adapter objects are
removed before catalog inspection and encryption. The [preflight](storage-binding-schema28-source50-preflight.json)
passed for 4,227 files, manifest
`42ab2be14e56832f9af70d6c0abef2a6d033bb7d15254b46a7fb137f654d4e5c`;
test-file SHA-256 `c6466aebd0e9dd32353fba6873c1a368962a6cceeb88fd920a78dcc8a174369a`.
Production code is unchanged from source49. Both historical recovery cases
subsequently passed in the combined source54 PostgreSQL target.

Source51's [format failure](storage-binding-scan-source51-format-failed.json)
occurred before manifest publication, tests or database access: one test loop
was missing its closing brace. Source52 fixed it but its [pure run](storage-binding-scan-source52-pure-failed.json)
passed 18 tests and failed two test assumptions about ctime advancing within a
single kernel timestamp tick. Source53 adds a controlled 20ms test separation,
retaining every production identity and timestamp check. Its [preflight](storage-binding-scan-source53-preflight.json)
covers 4,235 files, manifest
`b2135a8c8f4448e7ee5ad2d0366f1b76330b400a41649201c1f52c52e49a3078`.
The [20 selected race tests](storage-binding-scan-source53-pure.json) passed,
without failures or skips, in a private network namespace and without database
access. Recovery test code compiled only. Report SHA-256:
`658ff36e528e607147202264f9060d9e274ebf794c2dedf39493450e0859f88e`;
[terminal](storage-binding-scan-source53-pure-terminal.json) SHA-256:
`759259ebe8b93584cc6ab2b538d0d28ec5724f0861ba7538c2574c802c8d4c05`.
The full scanner and deletion transaction are not part of frozen source53.
Repeated stat observations do not reveal an intervening change if the final
identity and timestamps are identical; they are not an atomic filesystem lock.

Source54 adds ten scanner/transaction/music files to source53. Its
[source preflight](storage-binding-scan-source54-preflight.json) covers 4,243
files, manifest `c5a5cf6bf0afa973fbd2c087d300dbe6bd3079f92c7237b5a108d76ef8ae6fc3`.
Five Go files were formatted on test-env and copied back only after confirming
their original local hashes had not changed. The [28 selected race checks](storage-binding-scan-source54-pure.json)
passed with zero failures/skips; recovery/server test packages compiled only.
Report SHA-256 `3c5c5e750f7329cb77dfa166fdde137ade8622e8818b6d1e6da6bbfb0a9c712b`;
[terminal](storage-binding-scan-source54-pure-terminal.json) SHA-256
`2a6410ae2f16b80ad7cde61914db007ac1cb98b3d62b71fa5615883a9759ee47`.
The combined PostgreSQL target passed source50's historical archive repair,
source54 root recovery/scanner/cascade/music tests, existing scan notifications
and both unchanged capacity regressions. Controller:
`goby-scan-reconciliation-postgresql-controller-v1.service`, invocation
`84e777b5356d45adbb834ea393a08039`, historical PID1292882; execution directory
`/opt/goby-test/exec-work-m3e/scan-reconciliation-postgresql-execution-01`.
Run ID `20260912_053050_ec4b750c19e5`, inner unit
`goby-client-backup-20260912-053050-ec4b750c19e5.service`, passed 74 top-level
tests across four packages with zero failures/skips and all six cleanup checks.
The [report](storage-binding-scan-source54-postgresql.json) SHA-256 is
`2e926040ee48d0f0a33435da7882f6047516472c524637b863fa20613f9b714f`;
[terminal](storage-binding-scan-source54-postgresql-terminal.json) SHA-256 is
`31e653a2c9d57abcf8039313cde2e3eb2227bc99e760b20697a3192a85e60917`.
The controller exited0/MainPID0 with an empty cgroup. The inert mount helper was
excluded; this is not actual private mount-namespace scan acceptance.

Full regression and Linux build passed from the same immutable source54:
2,173 top-level tests across 25 packages, zero failures, zero skips, and all six
cleanup checks true. The [full report](storage-binding-scan-source54-full.json)
SHA-256 is `50f8543b1b11cf7a3e00d3a0a83684080835fdad0f54ca4c983650f0f1e8892e`.
The 29,338,037-byte Linux binary has SHA-256
`5f88c432d96825d5c3f8c2faccc64be673dab85849670707571c20fddd71594e`.
Controller `goby-scan-reconciliation-full-controller-v1.service`, invocation
`c8641f6597f0411ca832d052b885499d`, exited0/MainPID0 with an empty recursive
cgroup; PID1294649/start monotonic114182310421 is historical. The
[terminal receipt](storage-binding-scan-source54-full-terminal.json) SHA-256 is
`afd29fa99b10f2c4333e8482926d52cc0005cd4143c04bf1e7de360c189d235c`;
execution directory `/opt/goby-test/exec-work-m3e/scan-reconciliation-full-execution-01`.
Run `20260912_053517_9cb0074fc731` and inner unit
`goby-client-backup-20260912-053517-9cb0074fc731.service` are complete, including
the separate final recoverydb package. Its removed pair and prior evidence are
historical and must not be replayed.

The actual private-mount operator passed [65 memory guards](storage-binding-scan-mount-tool-verification.json),
then its [first real attempt](storage-binding-scan-mount-attempt1.json) failed
before dispatching unshare or the Go mount helper. Run
`20260912_061505_b2b453a716a9` compiled a fresh race test binary successfully,
but `host_witness` raised FileNotFoundError because
`/sys/devices/virtual/block` does not exist on this environment. The fixture
directory is empty and there is no helper output. The
[failed terminal receipt](storage-binding-scan-mount-attempt1-terminal.json)
SHA-256 is `737c70c0919491a559f0b79457849e34d6b0a19e394b955bf0ff5310ae88de60`.
Outer `goby-bound-scan-root-mount-controller-v1.service`, invocation
`6c741a464ae74b31839355d3ed2e9f06`, and its exact inner unit are terminal with
empty cgroups. HBA is restored exactly; the dedicated pair is retained, not
removed at that failed checkpoint. Its retained receipt SHA-256 is
`190c202ca956a52b1914fabe7363ea821bd691f0a6f47869edc8785bc3fe288f`.
The subsequent [exact disposal](storage-binding-scan-mount-attempt1-disposal.json)
passed after [27 memory guards](storage-binding-scan-mount-disposal-tool-verification.json).
Report SHA-256 is `1edf41f74cf1866a0a4fde54f9a516b3a2e1906e795774d2c1b459dfeb6619cf`;
[attestation](storage-binding-scan-mount-attempt1-disposal-attestation.json)
SHA-256 is `752baec4e675d6dcb210bde2df0658f46e24d4cc9b890691d8b6a69e5bd76667`.
Both exact role/database pairs were removed, while the original retained
receipt, source and failure trees remained unchanged. Its
[terminal](storage-binding-scan-mount-attempt1-disposal-terminal.json) SHA-256 is
`5fc929bbe1149a7d405878633bf0bd4178c509dc9f3cf4da77ee70200fb09e88`.

The corrected mount reader requires complete bounded `/sys/class/block`
enumeration, including every block device and partition. It never interprets
a missing directory as an empty inventory. Its [72 guards](storage-binding-scan-mount-tool-v2-verification.json)
passed, followed by [two actual read-only inventories](storage-binding-scan-mount-block-inventory.json)
of six devices. The [new actual run](storage-binding-scan-mount-v2.json)
`20260912_063021_cfe0f03b5112` then passed the real race-enabled Go mount helper:
original remount recovered; replacement storage learned no approval; approval
rows and sibling anchors remained unchanged; old leases still read the original
storage; fresh live mount identity was observed. Host mountinfo, namespace and
complete block inventory were unchanged, and all six pair/HBA cleanup checks
passed. This is private ext4 bind-mount recovery, not a system reboot test.
Report SHA-256: `295257dc500ef6067e4811aa4bb682e6b0eca68d67bfdd943434e764b6d266b1`.
Controller `goby-bound-scan-root-mount-controller-v2.service`, invocation
`d2e6d3eba8b74a7196e26e6ce95edd15`, exited0/MainPID0 with empty recursive groups;
[terminal](storage-binding-scan-mount-v2-terminal.json) SHA-256:
`5174cc48f8ae1d2a34bf0e9208fcef73653faa5c085bda78eaa5075b3a9d32ac`.
The live pair receipt now belongs to this finished and cleaned run; previous
FULL, failed mount and disposal evidence remain historical and immutable.

The new automatic-refresh [shared transport guards](m3e-library-changed-transport-guards.json)
passed 124 cases, report SHA-256
`94167fabfe0b11f552e0b971906a5e5f044efeade4799aee5522de18e041256a`.
The [source44 fixture guards](m3e-library-changed-source44-fixture-guards.json)
passed 248 cases, report SHA-256
`fbae697f3ec03835dfcf9b5fa3c2699ed2761585fc049e3d4ad84b8a59c3f106`.
Both included two syntax checks and no live browser/HTTP. The initial controller
also passed [48 memory checks](m3e-library-changed-client-tool-v1-controller-guards.json).
Two zero-HTTP preflights then stopped before creating the business scope:
[preflight01](m3e-library-changed-client-preflight01.json) rejected the original
Node installation's non-root ownership; [preflight02](m3e-library-changed-client-preflight02.json)
found a tuple in the primary file-identity JSON projection. A private root-owned
copy retains the exact reviewed Node bytes without changing the original
installation. TOOL02 changes only the tool path and that projection to a list;
JSON type and ownership requirements remain strict. Its
[342 pure checks](m3e-library-changed-client-tool-v2-guards.json), 11 syntax
checks and [zero-HTTP preflight03](m3e-library-changed-client-preflight03.json)
passed. The TOOL02 guard report SHA-256 is
`468afff94623d456add555004e635af3139353b412c4434dfff51a647655ddd1`;
preflight03 SHA-256 is
`b9fd175a7242e85055b36a9104e41fff927a65b9e835792c548abf832905b503`.
The repository's LF rule normalizes one CRLF separator in the controller only.
The [published-byte check](m3e-library-changed-controller-published-bytes.json)
proved every other byte identical and passed two syntax checks plus all 49
controller guards. Published controller SHA-256 is
`b64cbcc87c1a24da5cc2c390e65c1166d7da620e69991285a7c84f8e72135d1e`;
the immutable TOOL02 runtime retains
`331164bb828152169130a578d53aedae495b62ea79d18ae70d86c7eb12141f9c`.
The published-byte report SHA-256 is
`d6b598a479a93318f1072bf8fd823175f16212a58e91a7b59332b791c2baba3d`.

The [first actual client run](m3e-library-changed-client-attempt1.json) stopped
in discovery, with exactly one page TypeError after clicking Movies. The
recorded last API path hash identifies
`GET /emby/Users/ecbbe4cb82403879bc4b4f78894c5738/Items/a9993591e72f0f2e7babcbf8b9c50790`:
200, application/json, 430 bytes. No target movie list, LibraryChanged event,
native request or metadata mutation followed. The B session completed normal
UI logout and exact401; no fallback was used. Full row/sequence/private-state,
media and primary comparisons passed with the exact +1/+1/+2 session/device/audit
allowance and zero metadata revision change. The original client response body
was not retained by that unselected request observer, so do not invent its
contents or infer a particular missing field from the JavaScript error alone.
Controller report SHA-256:
`471c86db295cda6741582f318577cf1c075a18ab4bf603b81b91b92f485c7208`.
The [independent terminal](m3e-library-changed-client-attempt1-terminal.json)
SHA-256 is `7fc5c52d33c5ae4044da0a5fe761a23bbb4dbced9c82ca4bba1018e8fd802134`.
Both controller and browser worker are terminal with empty cgroups. Preserve
TOOL01, TOOL02, both preflight failures and the consumed client-v1 scope.

Candidate deployment preparation found that old prepare-client-fixture.py
upgrade paths require empty Extras and a 24-package product report. Preserve
those historical guards. A separate source44 candidate-only same-schema operator
is being prepared for the existing positive Extras and the accepted 25-package
report, retaining full database/media/credential/primary-state comparisons.
That new tool passed [52 memory guards](m3e-source44-candidate-tool-verification.json)
and a read-only live preflight, but its first
[deployment attempt failed](m3e-source44-candidate-upgrade-failure.json) while
saving stop_requested.json: the filename guard rejected the underscore.
No stop/start action was reserved or dispatched. Candidate PID748513/invocation
`b7a9ae3d00364e0993d2af2c7d3e1063` and primary PID762090/invocation
`bb94d74b475f4382a6ec6f6df181dd74` remain running unchanged.
The failed outer controller is terminal exit1/MainPID0/empty cgroup, invocation
`29ee7565d2d1487bab232c8b0b5b0780`; terminal SHA-256:
`33f875dc0aa69c00ab8928083f7f17da3deced3f33343d314bd10005a532ea12`.
Its durable fixture state remains phase=upgrading/stage=notifications_staged
from the preceding successful staged phase, SHA-256
`5d528680edffc64c4720d6024de0db3ad3bc0846eede6040302bef63941cc04a`.
The original accepted before-state SHA-256 is
`5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1`.
Do not replay or edit v1's tool/output/execution, and do not blindly reset the
control state. The subsequent continuation passed [22 memory guards](m3e-source44-candidate-continuation-guards.json),
including actual phase-publication simulation, and completed the candidate
upgrade as recorded at the top of this handoff. The first failed scope and its
staged-state evidence remain historical and unchanged.

Original-client automatic-refresh acceptance needs a fresh black-box candidate
scope with a real catalog trigger; the older policy gate depended on reloads
and policy updates do not emit this catalog notification. Do not inspect original
Emby/client source, JavaScript handlers or reference databases, including frozen
assets. Use existing public protocol/UI recordings and permitted black-box UI.
The [concrete candidate-only acceptance plan](library-changed-client-acceptance-plan.md)
defines native title change/restoration and the unbroken WebSocket, automatic
HTTP and visible-DOM chain. Its first execution failed during Movies discovery,
before the native edit; the completed public DTO capture and source55 repair
above supply the current follow-up boundary. A fresh original-client scope is
still required after verified publication/deployment.

The historical [storage binding plan](storage-root-bindings-plan.md) separated
adapter/persistence checks from the later scanner, live UI and deployment gates.
Scanner and scoped live UI acceptance have since passed as recorded at the head;
deployment and broader recovery coverage remain open.
The adapter passed [14 remote race tests](root-identity-go-verification.json),
report SHA-256 `e8c56113fb8f9acc8d9824d58698fa5bb9f93da3aad8c86b1229f79638215207`.
The [actual Go helper](root-identity-go-unprivileged.json) also obtained the same
identity as uid995 with empty capabilities and NoNewPrivileges, report SHA-256
`0ce31db107330c124a758c806feb59051f382bc66e460cdf41037030d8e2885b`.
Schema28 persistence and native rebind passed targeted PostgreSQL checks, while
full regression remains failed and missing-file deletion is a later unverified
draft. System reboot, other filesystems
and the complete service sandbox remain unverified. The user's untracked
`scripts/test-env/upgrade-main-schema25.py` remains untouched and excluded.

Historical verification increment: **source35 passed the full 24-package run
with 1,908 top-level tests, zero failures, zero skips and all six PostgreSQL
cleanup checks true.** The earlier [producer verification](m3e-library-changed-producer-verification.json)
records 43 passed tests, zero failures, zero skips and all six cleanup checks
true. Source manifest SHA-256 is
`3eadc5606b89c8ec7887bee760bb9309641d407638bb03d26c30344b250846eb`.
The retained report is
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_001248_002cceaba1e8/report.json`,
SHA-256 `9f0595a1d9fb9c97e03d8aed8cd7fcb2fdb33dcdd5ae23f29a8fa8a79c73ebe6`.

Library creation, library deletion and changed native metadata edits now capture
trusted facts inside their transactions and notify only after successful commit.
Request cancellation does not suppress an already committed notification;
rollback, failed commit and no-op edits do not publish. The bounded notifier
feeds the existing broadcast and current-permission delivery path. Source35
does not include ordinary scan production. Source37 subsequently passed its
ordinary scan/move targeted checks and full suite. Source38's image/subtitle and
derived-album producers passed their PostgreSQL targeted checks and full suite;
original-client UI acceptance remains pending. See
[implementation boundaries](library-change-notifications.md).

The [completed source35 full report](m3e-library-changed-full-verification.json)
is retained at
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_002059_ab4a943fc81b/report.json`,
SHA-256 `bb32b90b18b5c564f865734991d6cb50b4d17c023fe6687c9ee32181182fb808`.
Its Linux executable is
`/opt/goby-test/exec-work-m3e/client-backup-run-20260912_002059_ab4a943fc81b/tmp/goby-linux-amd64`,
28,248,725 bytes, SHA-256
`8c91a392d7e339e38a6fdb8be7ce41cc4fd8c20f1b5492ac38734871aa166377`.
The controller `goby-library-changed-full-controller-v1.service`, invocation
`7e3faf01d865410aa5799c3239105558`, is terminal with exit0, MainPID0 and an empty
cgroup. Its earlier PID947396 is historical. Execution records remain under
`/opt/goby-test/exec-work-m3e/library-changed-full-execution-01`.
Terminal receipt SHA-256 is
`e1677db514cc6d3e8fb086ce9145ecf18753fc87bb353619bcbd215fbea87bb7`.
This closes source35 verification, not deployment or the newer scan increment.
Primary and candidate remain on source32/schema27.

Latest reference work: **the separate continuation completed metadata editing,
removal and re-addition, with one LibraryChanged event in each controlled
window.** The [continuation report](m3e-library-changed-reference-continuation.json)
and [research record](../research/library-changed-reference.md) retain metadata
Movie98, removal folder97 and re-added folder99/movie100 observations. The
[controlled fixture five-file preservation record](m3e-library-changed-reference-media-preservation.json)
separately covers the new fixture across removal/re-addition. The continuation
report confirms both fresh-session logout204/exact401 barriers, the scoped
original seven-library/five-user public state and all three old media trees.
These observations do not establish complete client compatibility.

Reference PID332054 and its binary remain unchanged. Current reference state
has eight libraries and six users: library93, root folder94, anchor
folder95/movie96, re-added folder99/movie100 and ordinary user
`c5f36699a54f4971a891682cd9de410f`. The owned media root remains
`/opt/goby-fixtures/client-library-changed-v1`. IDs97/98 are historical and
were absent from the scoped catalog after removal. The continuation, initial
attempt and passive tail are all consumed scopes; none may be replayed.

Historical initial reference attempt: **the first controlled LibraryChanged attempt indexed
the new movie, then stopped at an unproven NFO metadata update. Removal did not
run.** The [reference study](../research/library-changed-reference.md) and
[initial report](m3e-library-changed-reference-initial.json) retain this failed
scope. A setup notification arrived during the pre-action quiet period. The
controlled add window had no event; the subsequent FullRefresh produced an
ItemsUpdated message while all five requested catalog DTOs stayed equal.
Do not assume that equal DTOs prohibit reference refresh notifications.

At that historical checkpoint, the added identities were folder97/movie98.
The changed NFO did not produce the requested Name/Overview change. The initial
scope's 334 requests preserved the captured old seven-library/five-user public
state and all three old media trees. Both new sessions completed logout204 and
exact401. Its unit `goby-reference-library-changed-v1.service` is terminal with
exit1, MainPID0 and an empty cgroup; terminal SHA-256 is
`2586b86a3ea0087be67c397f3c3c3dca2e75b7210d90c969d72225f2a1171b2f`.

Historical inputs consumed by the completed continuation under
`/opt/goby-test/exec-work-m3e/reference-library-changed-v1` are the export report
SHA-256 `f502223df8e7de2afeb49b5a6285498d967f5c95e1435121b455d7e8ee8b0a33`,
`private/final-old-state.json`
`5898de2a2e99754d34a0b4402f8803a2aca07332781feba62e7d0dbb8b4d6710`,
and `private/update-media-after.json`
`ecba10ca770ad5d021e98b2a8f6f1748eb06316833322376d1419b3f8a26a370`.
Preserve the entire consumed scope and its credentials; do not rerun setup.

The separate four-minute read-only catalog tail completed with zero events and
its fresh viewer session revoked. Its report at
`reference-library-changed-tail-v1/export/report.json` has SHA-256
`2dae87e06e3beda64421cc10c26c14889124f9885943cb29b960ccaf253d4aff`.
Its retained unit `goby-reference-library-changed-tail-v1.service` is exited0,
MainPID0 with an empty cgroup. Terminal SHA-256 is
`2441846e7ae8923f5fcdcfac8e4a9f8d6aab53eb3e4034577391e2c6910c6dba`.
It began after the first controller terminated and made no catalog mutations.

Historical next-work note, superseded by the current head: complete the
reviewed retained-pair disposal, source44 capacity/auxiliary
HTTP checks and a fresh full regression. Generate the actual schema28 catalog
and verify binding/audit/archive behavior before wiring native rebind and safe
missing-file reconciliation. Deployment and original-client UI acceptance remain open.
Finish complete theme/extra resource-change
notifications and global entity projection invalidation. Safe missing-file
reconciliation requires durable
root/storage binding, complete cross-root move matching and stable directory
observations. That binding and reconciliation workflow is not implemented or
verified yet. A legacy or changed root
must not gain deletion authority from repeated empty scans after a restart;
unproved storage continuity requires explicit binding/rebinding. Complete
M3/M4/M5/M6 remain open and M7 remains deferred.
The candidate's source32/schema27 and 73-session/62-device/163-audit authority
below are unchanged by this independent reference work.

Historical candidate checkpoint: **the original-client library permission UI gate passed once
after explicit browser reloads. Policy restoration and both owned session
cleanups passed. Automatic refresh was not observed in either bounded window.**
The [controller report](m3e-library-permission-ui.json), SHA-256
`4755b35026c47b692d5f3e68f57e7f2e6e4846b53643bd714cc92f493fe62f2d`,
and [browser report](m3e-library-permission-ui-browser.json), SHA-256
`ed6de30665561a552a1737af5f7856dfcfe2910bc675c7bcb878e27986d7822d`,
are retained under `/opt/goby-test/exec-work-m3e/client-library-permission-ui-v1`.
One new B token covered baseline, restriction and restoration. Each phase had
one fresh, completed frame/physical Views200 and matching correct-ID cards:
four libraries, then three retaining Extras/Music/TV, then the original four.
The restricted original Movies title, card and matching card-ID counts were
all zero after its explicit reload.

The two action-free ten-second windows contained zero Views requests. The
restricted window retained the old Movies card; the restored window still
lacked it until reload. Both remain `not_observed_within_window`, not a claim
that automatic refresh can never occur. This closes only the declared Home
permission behavior after explicit reloads: `permission_ui_acceptance=true`,
`client_acceptance=false`, with full M3/M4/M5/M6 still open.

Nine native exchanges performed one new administrator login, four managed-user
reads, two exact Policy updates, logout and exact-token rejection. B advanced
from revision3 to5 with its complete eight-key raw Policy restored exactly.
Browser UI logout204/exact401, administrator logout204/exact401, WebSocket3
opened/3 closed and empty worker process trees passed with no fallback or
page/cleanup errors. Three external-resource blocks and three console warnings
were retained. The independently hosted outer controller exited0 and was
released only after its exact invocation and empty cgroup were confirmed.

Authority at this historical checkpoint was the run's private `after-full.json`, SHA-256
`12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd`:
73 global auth rows, 62 devices, 163 audits, 63 selected A/B auth rows,
26 play rows, seven UserData rows, four libraries, 22 items and B revision5.
The increment was two sessions, one device, four authentication audits and
two owned user-update audits. Every other old row, unrelated sequence, private
input and media file was preserved. No PlaybackInfo, playback, scan, preference
or UserData write occurred. Do not replay this consumed run or use the old
revision3/71-session baseline for new work. Remaining client/event, subtitle,
NextUp and media/administration/release work now uses the current schema28
authority recorded at the head, through the preserved intervening transitions.
See [permission UI verification](verification-m3e-library-permission-ui.md).

Established prerequisite: **the preparatory original-client Home and one normal browser
reload passed in v3, including exact UI logout and complete state preservation.**
The [v3 controller](m3e-library-home-v3.json), SHA-256
`a8117846d80eeed8e714b8951103632acfc05105d8a14c362e99f0f9d9e62270`,
and [v3 browser report](m3e-library-home-v3-browser.json), SHA-256
`a5e4cd3a16c625c85b459c20c2349375a22bf63d889d7ac2e89f0e9237a91e28`,
are retained under `/opt/goby-test/exec-work-m3e/client-library-ui-baseline-v3`.
Both initial Home and the explicit reload displayed exactly one correct-ID
card per library and each produced a fresh, completed frame/physical Views200
with the four expected IDs and names. The same B token survived the reload;
there was one login. UI logout204/exact401, WebSocket2 opened/2 closed and
complete browser/proxy/worker closure passed with no fallback or page errors.
Two external-resource blocks and two associated console warnings were retained.

The historical v3 prerequisite's private `after-full.json` has SHA-256
`8095db0dd2e96c7f3a8e194b3f66d71c7366e756b12f1d1f549a19c336acb29a`:
71 global sessions, 61 devices, 157 audits, 26 play rows, seven UserData rows,
four libraries, 22 items and B revision3. Exactly one new B session, one device
and two audits were added; all old rows, Policy, unrelated sequences, private
state and media were preserved. The new session is revoked. Final Node log
descriptors now match the actual closed-worker bytes. Seven JavaScript syntax
checks, 35 Home guards, 114 shared-core guards, two Python syntax checks,
33 Python guards and a zero-HTTP preflight preceded the actual run.

This is `baseline_observation`, with both `client_acceptance` and
`permission_ui_acceptance` false. It supplied the prerequisite for the separate
permission-change flow now completed above. Its narrower flags are unchanged.
Full M3/M4/M5/M6 remain open.
See the coordination contract in the [permission UI plan](m3e-library-permission-ui-plan.md).

Historical v2 state: **login, four-library Views, exact UI logout and complete
data preservation passed, but the DOM predicate rejected duplicate page text.**
The [v2 controller](m3e-library-home-v2-failed.json), SHA-256
`54c02dbc41273a5043853d31126dec7455641d5efbf73ee20b97b13d8826ec23`,
retains a passed state-difference proof and the explicit UI failure. Its
[browser report](m3e-library-home-v2-browser-failed.json), SHA-256
`900c1ba1279bdd2c32eeb5f1a81a261d8302fb019b2dc912d5a0f495a74a33a3`,
records two visible text matches per library, but exactly one visible card with
the correct DOM ID. All four Views memberships and transfers completed200.
No reload ran. UI logout204/exact401, WebSocket1 opened/1 closed and complete
worker termination passed; no separate recovery was needed.

The historical v2 after-snapshot is
`/opt/goby-test/exec-work-m3e/client-library-ui-baseline-v2/after-full.json`,
SHA-256 `27e4627e774d96ab87fdb51fd5093dafcb01c529b0a8753fd50db566aadbfd26`:
70 global sessions, 60 devices, 155 audits, 26 play rows, seven UserData rows,
four libraries, 22 items and B revision3. The v2 increment was one new B session,
one device and two audits; all old rows, Policy, sequences outside this increment,
private state and media were preserved. The new B session is revoked.
Retain v2 as failed. The corrected v3 used this state and a new root;
its ID-aware card predicate still rejects wrong IDs and multiple cards and
retains unique-title requirements when no DOM ID is present.

Established v1 recovery: **the original failed worker is terminal, and both
credentials from its failure and independent recovery are revoked.**
The [native recovery report](m3e-library-home-session-recovery.json), SHA-256
`09b2fad0de6dcce6e1d6ecb85a700e94673642f3c5641d3d7dff6bab43e66ffb`,
is retained under
`/opt/goby-test/exec-work-m3e/client-library-ui-session-recovery-01`.
Its four complete exchanges were one new administrator login200, exact failed
B session revocation200, administrator logout204 and that administrator's
exact-token401. The lost B token was not available for a new401 check; its
native acknowledgement and persisted `revoked_at` prove the exact revocation.
Only that field changed on the target; all other old rows were preserved.
Recovery added one administrator session and three native audit entries, with
no new device, Policy, play, UserData, reference or encoding changes. Media and
the entire original failed evidence tree retained identical before/after
identities and hashes. Two remote syntax checks, 12 pure recovery guards and
a zero-HTTP preflight preceded this single actual recovery.

The recovery's historical private `after-full.json` has SHA-256
`5d0f3818abf5617a817541fc4d08cca5492a579b9ce5af99d0cec45b7d5f1ee0`:
69 global sessions, 59 devices, 153 audits, 26 play rows, seven UserData rows,
four libraries, 22 items and B revision3. Candidate source32/schema27,
PID748513/start ticks6996875 and the primary deployment remain unchanged.
This recovery does not promote the failed Home flow to passed.

The [controller failure](m3e-library-home-failed.json), SHA-256
`ba5c314aeb8d408e61ebaac10c69ed743c376a89ab542f326acc9ade8b8cdea8`,
and [browser failure](m3e-library-home-browser-failed.json), SHA-256
`447297492853d3c69e8fc0ae042f6386177a112f4e3eda817c93678e433c37df`,
remain under `/opt/goby-test/exec-work-m3e/client-library-ui-baseline-v1`.
The actual lower-case login route was missed by the new observer's
case-sensitive selector. Login200 and a complete Views200 containing all four
libraries were observed, but no complete login proof, Home DOM acceptance or
reload was established. The tool's proof gate blocked UI logout with502;
the following token check returned200. No Policy or playback action occurred.

An uncancelled login-wait timer kept Node alive until its six-minute service
limit. The controller collected terminal MainPID0, signal15 and an empty
cgroup, then retained the final complete database/media snapshots. The failed
scope added one B session, one device and one login audit: its failed-after counts were
68 global sessions, 59 devices and 150 audits, with 26 play rows, seven UserData
rows and B revision3 unchanged. The private after snapshot is
`client-library-ui-baseline-v1/after-full.json`, SHA-256
`6d00f9cd99313762702c212ffdcc612cf88a426da67cfbcb0d1b403e8ef2d4d8`.
The original API inspection snapshot below is now historical.

The login selector and finite, cancellable wait have been fixed. The
[new frozen JavaScript verification](m3e-library-home-js02-verification.json)
passed seven syntax checks, 31 Home guards and 114 shared-core guards on
test-env, with no browser or business execution. These fixes have not been
rerun against the candidate. Preserve the failed tree and original tool01;
any later UI scope needs a new output root and the post-recovery authority.
Original-client permission changes and full M3/M4/M5/M6 remain open. See the
[Home verification](verification-m3e-library-home.md) and
[permission UI plan](m3e-library-permission-ui-plan.md).

Established API state: **the M3 library-restriction/restore API matrix passed in one
attempt, with no failure or retry. This is API evidence, not original-client
UI permission-change acceptance or complete M3 acceptance.**
The [actual API report](m3e-library-restriction-api.json) is retained at
`/opt/goby-test/exec-work-m3e/client-library-restriction-v1/report.json`,
SHA-256 `193a5cc5ddaa806575d630a03de1268abed2418347b4e57eb5cc57610a04e8eb`.
It completed 61 HTTP exchanges with 14 cases in each of the baseline, restricted
and restored phases, keeping the same A/B tokens throughout all three phases.
Only B's original Movies library was restricted: detail, ParentId,
SpecialFeatures, LocalTrailers, Similar and Theme reads returned404; explicit
Ids returned200 with an empty list; Views omitted original Movies while
retaining Extras, Music and TV. A's owned reads stayed200 and cross-user reads
stayed403. The positive Movie, three SpecialFeatures and one Trailer remained
readable with200 throughout. The restored phase recovered the supported
effective policy.

The two PUTs advanced B's revision from 1 to 3. Native normalization materialized
six supported policy keys and two role flags from the original raw `{}`;
effective supported-policy restoration does not mean raw-JSON equality.
All old rows except B's explicitly declared account-field changes were
preserved, including all 26 existing play rows, seven UserData rows, 64 auth rows
and 56 device rows. Exactly three new auth rows, two devices and eight audit
entries were added, producing 67 auth rows, 58 devices and 149 audit entries.
There was no new PlaybackInfo preparation, UserData write, reference or
encoding work. All three owned sessions completed logout204/exact-token401.

The [tool verification](m3e-library-restriction-tool01-verification.json)
passed two remote syntax checks and 22 pure guards; the preflight also passed
with zero HTTP. Those checks are separate from the actual 61-exchange matrix.
The retained private before/authenticated/after snapshots are respectively
`b265e59aaad795bd688756e5642aa6b87eea50ba3e570726e188cff54f8175fa`,
`646fa69fb00fc577607d025c5510a7241f1e38d22d17321df82b2939ef27637c` and
`a09e42a404b9aa0251e2341e7ffa85c93528b7b141d81e11df6791802d6e92fd`.
Candidate PID748513/start ticks6996875, source32/schema27 and fixture-state
SHA-256 `5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1`
are unchanged; the primary independently remains source32/schema27.

The [independent persisted-state/media inspection](m3e-library-restriction-inspection.json)
passed with zero HTTP calls, report SHA-256
`e3dcbc0c21edbc484baab89762e11857a7cbc46889b78701af8dd309f12c3441`,
retained at `/opt/goby-test/exec-work-m3e/client-library-restriction-inspection-01/report.json`.
Its current private snapshot SHA-256 is
`1aca0670c3d6f1cd2f45082df89cf4df9930058f9658eb439deef289b39207a3`.
Persisted state matches the API after snapshot exactly; only capture time is
new. B is revision 3 with the exact materialized policy merge, all three new
owned auth rows are revoked, and media/source/process/fixture state are unchanged.
The inspection confirms 67 auth rows, 58 devices, 149 audits, 26 play rows,
seven UserData rows, four libraries, 22 items, four Extra resources/three markers,
zero references and zero encoding jobs.

At API gate completion, authority continued from this API after snapshot and
inspection. The later Home failure and its separate recovery now advance that
authority; use the latest state described at the top of this handoff.
The prior positive-client after snapshot `74640ff8ce353ad61b41364fbeedaff4fa2c14e55075ce55869e8be85963a10b` belongs to a
consumed historical scope: do not require current B raw policy `{}` or 64
global auth rows. Original-client UI permission changes and the remaining
compatibility/M3/M4/M5/M6 requirements stay open. See
[library-restriction verification](verification-m3e-library-restriction.md).
Do not replay previous UI, scan, upgrade or inspection operations.

Established deployment: **the primary schema26-to27 upgrade completed successfully
in one attempt. Both primary and candidate run source32/schema27.**
The [primary completion](m3e-main-schema27-completed.json) records active/running
PID762090/start ticks7637121; an independent systemctl read matched that state.
The installed executable SHA-256 is
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
The run is
`/opt/goby-test/backups/main-schema27-v1/run-20260911T190452Z-94ef1ef5242d2335018f8dac`,
terminal SHA-256
`0b0695556716bf78f414f146c2ab5f84fa273455679969050d69cf086af46b5a`.

Preflight passed, followed by one 307,429-byte schema26 backup dump, an
independent restore/schema26-to27 rehearsal, owned database/role cleanup,
the committed primary26-to27 migration, installation/start and 13 native smoke
calls including logout204/exact401. The migration preserved all old 33 tables'
rows, columns, relation OIDs, ACLs and sequences, plus private credentials,
runtime, media and historical archives. Its before/preserved-state SHA-256 is
`dd7f4564c121fbe088390a3d6a179b2048a5089a4fc9e3d8d5af7b1be49e175a`.
The primary's two new Extra tables are empty. The candidate remains PID748513/
start ticks6996875 with 22 items/four libraries and its indexed positive extras;
these are separate databases and acceptance scopes.

Smoke used only one new owned session, with no archive create/delete or restore.
Legitimate authentication and audit history is retained; post-smoke whole-table
byte equality is not claimed. No primary restore, old-binary rollback, automatic
retry, force cleanup or backend termination occurred. All older failed attempts
remain unchanged historical evidence.

The candidate protocol/media finalization, independent inspection and positive
Movie A/B flow below remain passed within their recorded scope.
The [protocol finalization](m3e-positive-protocol-finalization.json) used one
fresh ordinary AV token/device for exactly 11 HTTP exchanges: login, four
complete-file200 responses, four Range206 responses, logout204 and exact401.
Complete bytes, prefix SHA-256 and Content-Range matched. It added one session,
one device and two audit entries, reused the original 15 protocol proofs and
performed zero library creates, rescans or service actions. Both failed trees
remain unchanged. The prior three-session/nine-audit scan prefix is preserved;
aggregate setup history is four sessions/eleven audits.
The [independent inspection](m3e-positive-finalization-inspection.json) passed
with zero HTTP calls. Fixture state is ready/complete, SHA-256
`5319bc49b2753b84ca04f279523f2482a49a94fabd9944dc369347b6d87224e1`;
candidate PID748513/start ticks6996875, schema27 and source32 binary are unchanged.

The [positive original-client run](m3e-positive-original-client.json) passed
for A and B: each completed Home-to-Movie-to-Home, one genuine PlaybackInfo200
with finished transfer, and SpecialFeatures200 containing all three resources
with three visible cards. Ten item-UserData projections, preferences,
Configuration and Policy were unchanged; own200/foreign403, zero page errors,
UI logout204/exact401 and WebSocket1 opened/1 closed/0 active with no cleanup
failures all passed. Each user retained one blocked-resource console warning/error.
Cards were matched by unique title; the report records `id_present=false`,
so this is not DOM card-ID proof. LocalTrailers UI was `not_observed` for both
users: API and media evidence exists, but no original-client trailer-playback
claim is made.

The [first comparison failure](m3e-positive-client-comparison-marker-failed.json)
is retained: an old cross-user marker was hardcoded. Only that marker changed;
the observer, original snapshots, UI report and failed comparison were preserved.
After 29 remote guards, the [repaired comparison](m3e-positive-client-comparison.json)
passed: 24 to 26 play rows, 55 to 57 selected A/B auth rows, the two eligible old
Prepared rows became Expired and the other 22 old play rows stayed exact.
Two new Prepared rows and two new revoked auth rows were added. The five old
UserData rows stayed exact; two strictly default rows bring the total to seven.
References/encoding stayed zero and the other declared scope checks passed.
This is a bounded preparation comparison, not whole-database preservation.

Main consumer04's two remote syntax checks, 27 guards and build preceded the
separate successful primary execution above. The subsequent
[library-restriction/restore API matrix](m3e-library-restriction-api.json)
also passed. Its original-client UI counterpart and other compatibility cases
remain open. The complete M3/M4/M5/M6 milestones are unfinished; completed
operations and failed trees must not be replayed or relabeled.

Historical tool03 state: **it scanned the existing library successfully
(`Completed`, 6/6/0), then failed an incorrect parent-count assertion.** Job
`d7aa0acaee023dd4c82ea7a303c354ca` kept the library/root IDs below. The inherited
`84af.capture` demanded `SpecialFeatureCount=3`; the correct sampled response
omits that field and has `LocalTrailerCount=1`, matching the reference 20-case
capture. Do not change the product for this tool error. The [continuation failure](m3e-positive-continuation-count-check-failed.json)
remains failed; both new administrator/viewer tokens were revoked with 204/401.
Full/range media requests had not started.

The independent [15-response proof](m3e-positive-protocol-response-proof.json)
passed six direct reads, eight lists and the parent read, all complete200 with
correct membership, paths, owners and sources. The [snapshot review](m3e-positive-protocol-snapshot-review.json)
passed original 13-item/all-old-row preservation, new 9 items/four resources/
three markers, all three actor sessions revoked, exactly nine new audit entries
and exact sequences. At that failure the counts were 35 tables, 22 items, 4 libraries,
three markers/four Extra resources, 5 UserData rows, 24 plays, zero encoding,
61 global sessions and 135 audits. PID748513/start ticks6996875, schema27,
source32 binary and runtime configuration were unchanged. The retained failed phase is
`continuing_special_features_fixture`, stage `scan_complete`, state SHA-256
`0897f2bec4723d5a69df8b35978bc4be32e7ea91f1f11aebfda4c500c2116e83`;
the retained full-snapshot SHA-256 is
`550b6f83cd804485cf227ee3514fb6dec0af550d61ec5926303ce589047877f8`.

The subsequent successful finalization used the independent scopes
`/opt/goby-test/exec-work-m3e/client-special-features-protocol-finalization-v1`
and `/opt/goby-test/exec-work-m3e/client-special-features-finalization-inspection-v1`.
It reused the 15 responses, added one session/device and two audits, and
preserved the old snapshot rows. The four full200/four Range206 reads and
login/logout/exact-token proof completed in 11 HTTP exchanges. The earlier
62-session/137-audit expectation belongs to this pre-UI setup boundary, not
to the later UI comparison. Positive UI and its scoped comparison subsequently
passed as recorded above. Main consumer03's 26 guards/build remain historical;
consumer04 was verified before the separately recorded successful primary deployment.

Historical setup01 failed after create201 and before its scan. Subsequent
continuation read-only preflight exposed two separate guard-model defects in the
[current verification](verification-m3e-extras.md): login audit Count must be
one, and the already-created fixture requires its actual 14-item/four-library
quiescence check. Neither preflight diagnostic made a business request; both
remain retained alongside the setup01 and tool03 execution failure trees.

The created library ID is
`57a85c1ca5b6c7ae602c587755250b2f`, root ID
`604d2c0f5c78919a6ee360cda2048066`, with the exact permitted Movies path and
relative path `.`. The [12 pure guards](m3e-positive-fixture-tool01-guards.json),
two syntax checks and preflight had passed. The [failure](m3e-positive-fixture-create-failed.json)
occurred after `checkpoint('library_acknowledged')` saved state: the private
filename rule `[a-z0-9-]+` then rejected `phase-library_acknowledged.json`.
The [diagnosis](m3e-positive-fixture-phase-diagnosis.json) confirms every phase
containing an underscore is rejected. This is an operator checkpoint failure,
not a product scan failure. The original administrator logged out 204 with
exact-token401; no viewer login or new scan occurred.

The original v1 profile remains frozen at phase `preparing_special_features_fixture`, stage
`library_acknowledged`, state SHA-256
`513d971260e18d24ada666a3ec942391d4679bf2a98c5c852742cc70033fccf1`.
PID748513/start ticks6996875, runtime configuration and binary remain unchanged.
Counts at the setup01 failure were 35 tables, 14 items, 4 libraries,4 roots,14 metadata rows,
15 Theme owner rows, 59 whole-database auth rows and 129 activity entries.
The new rows are one CollectionFolder/library/root/metadata/owner, one revoked
administrator session and three audit entries. All old rows and media were
preserved; both Extra tables were empty at that checkpoint. The earlier root-extension 13-item/
three-library result and original-Movie UI success remain valid history.

The actual tool03 continuation used the independently authorized scopes
`/opt/goby-test/exec-work-m3e/client-special-features-fixture-continuation-v1`
and `/opt/goby-test/exec-work-m3e/client-special-features-continuation-inspection-v1`.
The [continuation profile](m3e-positive-continuation-profile-guards.json) passed
two remote syntax checks and seven pure guard groups. The [revised positive
ledger](m3e-positive-prepare-scope-tool02-guards.json) passed three syntax checks
and 25 pure guards. These guard results are separate from the actual tool03
scan and protocol evidence above.
Bind the failed attempt, create201 acknowledgment and all old trees; do not
recreate/delete the library, replay setup01 or mark its old v1 tree successful.
A new administrator scanned the library and a viewer made the 15 protocol
reads. The three-actor session/audit/sequence transition has now passed the
independent snapshot review, but the tool's incorrect count assertion stopped
full/range checks in that attempt. The separate finalization later passed;
neither failed execution was replayed or relabeled. The primary was source28/schema26 at that historical checkpoint; its source32/schema27 deployment is recorded above.
Exact failure/tool/diagnosis pins are in
[extras verification](verification-m3e-extras.md); frozen tool01 stays unchanged.

Current implementation increment: persistent Movie SpecialFeatures and
LocalTrailers are published at schema27 in the source32 product checkpoint. The corrected
[source31 targeted run](m3e-source31-extras-targeted.json) passed 105 race
tests with zero failures/skips and complete owned cleanup. The subsequent
full source31 run was stopped after real fixture failures and a separately
confirmed Trailer metadata-display defect; its frozen manifest remains
`0468c7e557c82249a866b81ae7b22924c2dc456bf5cd333af45b9a6d1ad7aa92`.
The [verification record](verification-m3e-extras.md) retains source29's real
35-table catalog, source30's two fixture failures and exact empty-pair disposal.
Source31's full-run failures and stop evidence remain retained. The source32 product
fixes its old table/cleanup inventories and honors native Trailer Name/SortName
overrides and locks. The interrupted run's exact residual objects were removed
by the independently [verified repair](m3e-source31-extras-disposal.json),
preserving both failed attempts and the preexisting cluster.
The verified product snapshot is `source-attempt-32`, 4002 files, manifest
`a65070315ce3b31dd70143267cbf774a838bc0e5b1ed99f34c3c759d392daa65`.
Its 53 changed Go files were formatted remotely and copied back; source files
and manifest use 0644 within the 0700 source root. Candidate schema27 tooling
passed 55 remote guards. Verified tooling and reference/failed-run evidence
were published separately at `d1f0c69`. The [source32 product checkpoint](source32-product-publication.json)
was committed and pushed at `b9bb7b1`; nine verified harness/operator files
were published separately at `347e12d`. The [source32 expanded
targeted run](m3e-source32-extras-targeted.json) passed 137 race tests with zero
failures/skips and complete cleanup. The [source32 full regression and build](m3e-source32-extras-full.json)
also passed: 1,873 top-level race tests across 24 packages, zero failures/skips,
all six cleanup checks true and unit exit code 0. The terminal report at
`client-backup-run-20260911_163635_51b90bc25d4b/report.json` has SHA-256
`408c49ff73e66c505865494e8e2e843954fd682a4b09fb5287835c027718ee78`.
Its `tmp/goby-linux-amd64` build is 28,172,723 bytes, SHA-256
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
The [candidate upgrade](m3e-source32-candidate-upgrade.json) from schema26 to27
passed with ready/complete status. Its original source32 process was PID746709/start ticks
6930051 with the verified executable above. All preexisting rows, columns,
relation OIDs, ACLs and sequences in the old 33-table schema were preserved,
along with credentials, media and recovery state. The only addition to an old
table is the expected schema27 migration row; the two new tables are empty.
That upgrade left 35 tables, 13 items and three libraries, with unchanged
runtime configuration and two empty Extra tables. Its receipt retains the
original PID746709; the later extension's current process is recorded below.
The new five-hop Music lineage passed six live guards, and input07 passed
114 pure browser guards plus two syntax checks, all on test-env.
The [original Movie dual-user flow](m3e-source32-cross-user-original-movie.json)
passed: each user completed Home-to-Movie-to-Home, one real PlaybackInfo200
with finished transfer, complete SpecialFeatures200 `[]`, zero page errors,
own200/foreign403, unchanged four item-UserData projections/preferences/
Configuration/Policy, and UI logout204/exact-token401. Both WebSockets closed
with no active socket or cleanup failure. One blocked-resource console
warning/error per user remains recorded. The [scoped database comparison](m3e-source32-cross-user-original-movie-comparison.json)
passed: 22 to 24 play rows, 51 to 53 selected A/B auth rows, two eligible old
Prepared-toExpired transitions, two new Prepared rows and two new revoked
auth rows; five UserData rows stayed unchanged and references/encoding stayed zero.
This is bounded preparation evidence, not whole-database preservation.

The [exact media-root extension](m3e-source32-extra-root-extension.json) then
passed, appending only `/opt/goby-fixtures/client-special-features-m3e-v1/Movies`.
It preserved all 35 table rows/sequences, credentials, recovery state and media,
with zero HTTP calls, library creation or scans. The candidate now runs as
PID748513/start ticks6996875. At extension completion it had 13 items, three
libraries and two empty Extra tables. That historical fixture-state SHA-256 is
`6d719ab6f3cd6c13ebf9fe3d6a927abaf5040e6e87e84be5d81a57318440cefe` and runtime
configuration SHA-256 is
`d8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df`.
The [first preflight failure](m3e-source32-extra-root-preflight-failed.json)
is retained: tool01 and its mock used nonexistent `encoding_states` instead
of `encoding_jobs`; it stopped before output-directory creation and left
state/environment/service unchanged. Tool02 fixed that name, added an actual
catalog regression, and passed two syntax checks, 11 pure guards and execution.
See the [verification record](verification-m3e-extras.md) for exact report/tool
and completion-receipt hashes. Full/range finalization and the positive client
flow have now passed within their declared scope. The primary source32/schema27
deployment also passed; completed media/reference phases must not be replayed.
The independent [nonempty-profile guards](m3e-schema27-positive-profile-guards.json)
and [actual read-only baseline check](m3e-schema27-positive-profile-baseline.json)
passed before setup01 created the new library and failed at its checkpoint.
The [main27 deployment tools](verification-m3e-main-schema27-tools.md)
passed two syntax checks, 24 pure guards and the helper build. Their real
deployment prerequisites were subsequently satisfied by the positive candidate
evidence and consumer04. Main consumer03's 26-guard/build result stays separate
from the successful actual primary deployment recorded at the top.
The additional [20-case reference capture](m3e-reference-special-features-projections-v2.json)
completed with state/media preservation and token revocation. It proves known-ID
extra retrieval, positive LocalTrailerCount, field switches and sampled error
responses. The cross-user403 policy remains an intentional Goby requirement,
despite the sampled reference returning200 for another existing user path.
Primary schema27 deployment and the scoped positive-extra candidate client run passed.

Current operational state: the primary service is **active/running** at
source32/schema27, PID762090/start ticks7637121, executable SHA-256
`af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620`.
The [schema27 primary completion](m3e-main-schema27-completed.json) passed in
one attempt. At that completion checkpoint the candidate had its independent
PID748513 process and positive fixture database; the primary's new Extra tables
were empty. The candidate has since reached the independently accepted
source55/schema28 PID1458051 state recorded at the head; the primary is unchanged.

Historical source28 primary state was source28/schema26, PID688833/start ticks5620918, executable SHA-256
`83757e79a1694573e4c1fab83e18c67be5f0c2d8696246daccb91f009ab2efae`.
The historical [schema26 deployment completion](m3e-main-schema26-completed.json) **passed**,
closing the main-service gate. The independent completion made 13 native calls
using one new owned session, including logout204 and the paired session401.
It performed zero service writes, migrations or restores and preserved all
three historical failure trees. Activity increased from 15 to 17 only through
legitimate login/logout history; post-start whole-table equality is not claimed.
Tool04 completed the real rehearsal and cleanup, main migration, installation
and start. Its [terminal result remains failed](m3e-main-schema26-start-verification-failed-01.json)
at `start-requested`, after 71 intents, with
`The candidate process has an unexpected effective identity.` No smoke ran.
A later `/proc` observation confirms UID995/GID986, matching the fixed operator's
expectations. A transient startup observation is possible but is not a proven
cause. The unchanged tool04 identity check, installed-state check and private
preservation check subsequently passed in an independent read-only review.
The subsequent formal completion and owned-session smoke passed without
repeating migration or restart. The original failed terminal remains unchanged.

The first main-schema26 attempt stopped the service, then failed in baseline
capture with `column_acl_capture_failed`. The subsequent
[tool03 repair attempt](m3e-main-schema26-baseline-repair-failed-01.json) completed
a fresh baseline and dump from one snapshot, then failed in `observe_rehearsal`
before any rehearsal because its SELECT references absent PostgreSQL17 column
`pg_authid.rolconfig`. No main migration ran in either of those earlier attempts.
The main then remained schema25 with 15 activity rows; that read-only observation found zero other database
connections and zero rehearsal databases or roles. These are not authentication
session row counts. Both earlier failed trees remain preserved alongside the
later tool04 start-verification failure; none is relabeled as a successful run.

The isolated candidate now runs source32/schema27, PID748513/start ticks6996875.
Its previous [source28 product checkpoint](source28-product-publication.json) is committed and
pushed to `origin/main` at `608e2088ca6aef150833d3f9bbc954c03e5aef5b`, tree
`da8fcd68b41c691c17c5dd0b4d760e38d3606ec3`. The commit contains only 73 staged
internal product/test/catalog files; active tools and documentation are separate.
Six verified harness files are recorded in `07183be`, and ten main/disposal
tool files in `b4a7f0c`. These tooling commits preserve the same product
checkpoint and are in the published history. The main-deployment/evidence
checkpoint `53144e6` has been pushed to `origin/main`.
The exact executed input05 harness and its original comparator were subsequently
committed and pushed as `7099a49`. The input06 diagnostic and comparator were
published separately at `1da3e28`; media mains were published at `6a5ff63`.
The corrected Theme product source28 passed 72 targeted
race regressions and all 1,830 tests across 24 packages, with zero failures or
skips, complete owned cleanup and a successful application build in
`client-backup-run-20260911_114525_fe88ae8d9829`. The run's source
manifest is `72e9301ba4405c6bddea15697e101dd62b157048f4c3b2e3e307da07ec0eb5df`.
The candidate 25-to26 upgrade, six live Music lineage guards and original-client
auxiliary-album flow passed. Similar and ThemeMedia both completed HTTP200
transfers, Home was reached, four UserData projections and preferences stayed
unchanged, and UI logout204 was followed by exact-token401. No page error was
observed; one blocked-resource console error remains recorded. The first new
dual-user browser attempt stopped before submitting A's login and closed its
browser. Input03's subsequent anonymous prelogin diagnostic passed with all
202 observed network requests completed and normal Service Worker startup;
no credentials were submitted. The historical
[input04 dual-user run](m3e-source28-cross-user-failed-02.json) then passed both
users' UI login/detail reads, own/foreign authority checks, state preservation,
WebSocket transport closure and exact UI logout proofs, but the overall run
still failed at `browse_A` before return Home. Automatic PlaybackInfo was
blocked. The later [input05 preparation run](m3e-source28-cross-user-preparation-01.json)
passed its explicitly scoped Home-to-Movie-to-Home flow, actual PlaybackInfo200,
own/foreign checks, four state projections and both UI logout204/exact-token401
proofs after 100 pure guards. Its private database comparison also passed.
However, each client recorded one unclassified `ui_movie` page error. The driver
did not reject page errors, so its passed result does not close full client
acceptance. [Input06](m3e-source28-special-features-blocker.json) passed 107
guards, then correctly failed its strict page-error gate. Both exact request
hashes identify `GET /emby/Users/{UserId}/Items/{Id}/SpecialFeatures` returning
404 alongside the `Response` errors. Its separate database comparison passed.
The first [positive reference capture](m3e-special-features-positive-contracts.md)
has now completed. The [persistent SpecialFeatures implementation](special-features-implementation-plan.md)
has passed source32 remote regression/build verification and candidate deployment;
the original empty-extra and positive-extra Movie flows also passed. Broader
projection/access coverage remains open.
The later library-restriction/restore API matrix and independent persisted-state
inspection passed; original-client UI permission-change acceptance remains open.

The two synthetic SpecialFeatures main movies were
[generated and probed](m3e-special-features-media-mains.json) in the new root
`/opt/goby-fixtures/client-special-features-m3e-v1`. Their mains manifest SHA-256
is `d53dde28a3c5ed5a67c310e0520639bd8f54233f93b368cf2539b88091ae426a`;
the completed media receipt SHA-256 is
`b753494e27902d62bfc6021f232c38c5e69fd4374f8e4a3a92b92fe5622a1f97`.
Both files are 12-second H264/AAC clips at 320x180/30fps. The six-file mains
stage preserved the original and auxiliary media and performed no HTTP or
library scan. The new reference operator subsequently indexed library `83`
with positive Movie `87` and empty Movie `88`, then completed the separately
receipted extras stage. The reference now has **seven libraries**, with all
five users and the old six libraries preserved. The generator's extras stage
completed with 13 files, and all 24 ordinary-viewer protocol cases returned
complete HTTP200 responses. SpecialFeatures returns three Video attachments
(Clip, DeletedScene, Clip); LocalTrailers separately returns Trailer `89`.
Both stages proved logout204/exact-token401 for each new admin/viewer token.
See the [exact checkpoint](m3e-special-features-positive-checkpoint.json) for
report and media receipt hashes. Neither media nor reference phase may be rerun.
That initial reference capture left parent counts, projection switches and
delivery/client checks open. Later bounded reference and candidate evidence
above covers the sampled fields, full/range media and positive A/B UI; broader
access/layout coverage remains open. The unfiltered new-library
recursive query returned only main Movies and Folders. The separate old-state
preservation query excluded Trailer by type and is not a trailer-visibility proof.
Main-schema26 tooling's original build and 15 memory guards, its earlier
read-only retention rejection, and its later stopped-service baseline failure
are separate evidence. One real PostgreSQL17 read-only ACL regression passed.
Tool03 then passed its helper build, 26 memory guards and repair preflight;
its successful fresh backup and later rehearsal-query failure remain distinct.
Primary schema26 deployment and its formal smoke/finalization are complete.
Complete dual-user and M3/M4/M5/M6 acceptance remain open.

Development resumed on 2026-09-11 at the user's request. At that historical
checkpoint, the next increment was [M3e real-client acceptance](client-acceptance-m3e.md),
following the priorities below. The previous round closed after M5j native backup/recovery, deployment,
documentation and publication to `origin/main` at commit `4a840fb`.
The partial [source18 M3e checkpoint](verification-m3e-source18-checkpoint.md) was
deployed and published to `origin/main` at `f339b69`; source28 later replaced
that installation, followed by the current source32 primary recorded above. The complete planned server
and full Emby compatibility remain unfinished. The
[Similar and ThemeMedia contract capture](client-auxiliary-reads-plan.md) used
separate synthetic comparison fixtures while retaining the source18 deployment.
Similar was subsequently published at `61be0dd`; the Theme product checkpoint
is now published at `608e2088`, and primary deployment completed separately.
Tooling publication and broader client acceptance remain current work.

The initial [20-request auxiliary reference capture](m3e-reference-auxiliary-reads-v1.json)
passed, preserving the old catalog, user state and media and revoking its new
recorder token. Similar on the original MP3 returned the real FLAC peer; theme
defaults and independent enable flags were recorded. The
[new auxiliary media](m3e-auxiliary-media.json) also passed generation and remote
profile verification at `/opt/goby-fixtures/client-aux-m3e-v1`, manifest SHA-256
`dad99c4883fde8bba00c9061179341b1a5869dd20212de55b92e0a53353703a7`.
The [three-library reference attachment](m3e-reference-auxiliary-libraries.json)
passed: new Movies/TV/Music IDs are 20/57/66. All five existing accounts and their
old visible media data were preserved. The reference now has six libraries;
historical three-library bootstrap/capture inputs must not be rerun.
Positive movies/music/themes/music-roles/artist-exclusion captures passed with
separate recorder tokens and immutable evidence roots. Two controlled metadata
combinations were restored after capture: one genre plus one tag qualifies,
but two shared People do not. The preliminary equal-weight Person score is
therefore contradicted. The subsequent eligibility/ranking variants passed:
one genre plus studio qualifies, one genre plus actor does not, two genres plus
studio outranks the two-feature peers, and two genres plus actor exchanges order
with those peers. Every temporary metadata change was restored; only explicitly
recorded ETag differences remained. The working scorer removes Person credit.
Neither generator nor any completed reader may be rerun or adopt an existing path.

Source19 is a preliminary frozen development snapshot at
`/opt/goby-test/exec-work-m3e/source-attempt-19`, manifest SHA-256
`cdf8775d7ffd0973046d83fef48d8ab3a813105164f807755ae9783a18545708`.
Its [30 targeted music tests](m3e-source19-music-targeted.json) passed remotely
with both disposable pairs removed and the original HBA/catalog preserved.
Music metadata probe version 2 accepts explicit album_artist, while technical
probe version 6 and stored music_source version 1 remain compatible. This is
not a Similar scoring pass, complete regression, new binary or deployment.
At that historical checkpoint, the main and isolated candidate were source18/schema25.
The [separate source19 Similar regression](m3e-source19-similar-targeted-failed.json)
finished with 11 top-level passes and one timeout failure. The 1,050-candidate
whole-catalog test reached its existing 90-second deadline. The complete workload
remains required while the SQL is optimized. Its unit is terminal and HBA was
restored. The independently reviewed empty pair was subsequently
[removed by its exact owned disposal operator](m3e-source19-similar-disposal.json),
preserving the original failed report, log, receipt snapshots, credentials and
output directory. The preexisting cluster catalog and original HBA remain intact.
Do not restart the completed run or reduce its dataset/timeout to obtain a pass.

[Source20](m3e-source20-snapshot.json) is frozen at
`/opt/goby-test/exec-work-m3e/source-attempt-20`, manifest SHA-256
`456f90464a0c18d8f1a31e268a67a5c65dcf1264a7ee69fc45be4d085d94e945`.
It changes the Similar implementation and its reference-constrained expectations;
the music metadata increment is identical to the source19 targeted pass.
Source20 passed [43 targeted remote race tests](m3e-source20-targeted.json), with
zero failures/skips and complete owned cleanup. The unchanged 1,050-candidate
fixture was seeded in 377.86 ms and queried in 85.96 ms. The [complete-source run](m3e-source20-full.json)
`client-backup-run-20260911_081348_169f35f30b8a` passed 1,763 race tests across
all 24 packages, with zero failures/skips and complete owned cleanup. Its
26,866,941-byte executable SHA-256 is
`a5028f865d3638f020c758a77bf1d431ca755699b7767a25b711509a8f8c12a9`.
The [same-schema candidate upgrade](m3e-source20-candidate-upgrade.json) passed,
preserving every existing row/sequence across 30 tables and all old runtime,
recovery and credential files. That upgrade installed PID 581075/start ticks
3900536, schema25, subsequently replaced by source28 below. The main service
then remained source18/PID 539535/ticks 3115871; that main process is now stopped.
The source15-to16-to18-to20 Music chain is
`client-music-upgrade-chain-source20.json`, SHA-256
`996117c1df9e6a609905e6f1ed539829b5a61bb94eae4c6a02560ed52e0f0497`.

The [source20 client checkpoint](verification-m3e-source20-similar.md) is partial.
Final browser input07 passed [11 pure mock guards and six lineage guards](m3e-source20-browser-guards-final.json).
The complete current 17-file harness closure is independent of the older frozen
source20 operator copies. [Three initial UI attempts](m3e-source20-auxiliary-album-failed-01-03.json)
stopped before auxiliary requests while correcting synthetic-album Path
omission and the original client's absent card data attributes. All logged out
and proved exact-token rejection; none was relabeled or reset.
The [fourth original-client observation](m3e-source20-auxiliary-album.json) binds
the actual album URL, two track rows and Request objects. Similar returned 200
and completed transfer. Four item UserData projections and preferences remained
unchanged; there was no playback and logout returned 204 followed by exact-token
401. ThemeMedia returned 404 without an observed transfer completion, and one
page error remains. The original workflow stayed failed in album-wait and did
not return Home. This is scoped Similar evidence, not a complete auxiliary or
M3 acceptance pass. All four browsers are closed.
The [publication comparison](m3e-source20-staged-source-check.json) establishes
3,186 byte-identical product/test files plus one explicitly reviewed test-only
CRLF-to-LF JSON normalization. All production source, 17 browser files and eight
executed reference/operator files matched. The original reference provenance
inside that fixture was retained; no additional runtime change was made.
The [input02 failure](m3e-source20-browser-guards-failed-02.json) remains preserved;
its mock wait assertion and nonprivate input-directory mode are not accepted
browser preparation. Do not run a browser from that directory.

The Theme product changes are outside source20 and are now published in the
source28 product checkpoint. Schema26 adds a separate
positive numeric owner namespace, permanent reserved paths, and active/inactive
resource associations. It does not create Item aliases or consume old identity
sequences. Shared predicates now distinguish ordinary catalog visibility from
authorized direct access to active resources. Library queries, aggregates,
folder UserData, media/subtitle/image access and current-session projections
use those boundaries. ThemeMedia query/HTTP drafts implement independent song
and video inheritance, genre-only owner fallback and the separate OwnerId
namespace. Backup/restore checks complete owner coverage and cross-row resource
validity. Scanner integration and its regressions are included in the accepted
source28 build and scoped candidate/client results below. Primary deployment
has now passed separately; broader client and milestone completion remain open.

The restored SSH session confirmed both service processes and the original
verification-cluster HBA. Sixty stable Go files were formatted remotely.
`/opt/goby-test/exec-work-m3e/source-attempt-21` is frozen exclusively for the
schema26 catalog bootstrap, with 3,906 source files and manifest SHA-256
`d37af7f30fa18cbbd71157393c575886528ab49c800af6db401a3939596b7b2b`.
Its unfinished scanner copy is not an accepted product snapshot. The extended
runner explicitly supports schema26/33 tables while retaining immutable
schema23/24/25 catalogs. Its 97 remote memory-only guards passed, followed by
[successful catalog generation](m3e-source21-theme-catalog.json): artifact SHA-256
`e02c46a49dd4200bb67954f70ca0bbe90b3ef99d8821ea56a97e3dcb97e696de`.
Both disposable pairs were removed and the original HBA and preexisting cluster
catalog were preserved. No service was upgraded.
The separate database/backup-only snapshot `source-attempt-22` has 3,907 files,
manifest SHA-256
`a310305aebe670ca8e613943f64b83386bcf41d64afa9befbb2d37a4d9bf0238`.
Its [first targeted run](m3e-source22-theme-database-failed.json) passed 11 tests and failed three tests in their shared
sequence snapshot helper: PostgreSQL does not give sequences a composite row
type for `to_jsonb(v)`. The helper now explicitly captures all three sequence
fields. The failed run and its original evidence remain terminal and retained.
Its separately reviewed empty pair was disposed without changing the original
receipt, logs, credentials, HBA or preexisting catalog; disposal report SHA-256
`2381bbab9058d3a7112b64775fb41926da24653cc59eac50a9d1267156e5a15b`.
Do not rerun either completed operator or relabel the original failure.
The corrected database-only `source-attempt-23` retains 3,907 files with manifest
SHA-256 `b0c1a07d0a0204536d5e89ec6ffe39315ef8fd96ece4577fab26e51f40c6029f`.
Only the sequence test helper changed from source22; its identical 14-test
selection [passed remotely](m3e-source23-theme-database.json), with zero failures
or skips and complete owned cleanup. It is not an integrated product snapshot.

The first library/server Theme snapshot is `source-attempt-24`, 3,908 files,
manifest `3df8ab5eb5f9d641de302e457fa0fe5a61c6fbe681223929e087fc95c9bfeffc`.
Both race-test binaries compiled remotely. The 48-test run
`client-backup-run-20260911_101731_c0db70c07036` passed 46 and failed two test
fixtures: the legacy metadata test omitted three new tables from its exact
addition list, and a foreign-root authorization fixture collided with another
valid item's unique path before reaching authorization. Both fixtures are now
corrected. The [terminal failure](m3e-source24-theme-targeted-failed.json) retains
its original evidence. Its exact empty pair was separately disposed, preserving
the original receipt, logs, credentials, HBA and preexisting catalog; disposal
report SHA-256 is `85a6763e16da66eb24020bd8dd9d245a8fa6d8ba75e156522f8869e6fd678c01`.
`source-attempt-25` contains all 638 current Go inputs, 3,909 files overall,
manifest `8ec6ffbe1675bc7db036d4741e0952641dbc444bc23ff046b3f4aeac5656baed`.
Relative to source24, it changes only the two test fixtures and adds the final
three scanner review regressions plus one real-media HTTP case. Its two race-test
binaries compiled remotely, and [all 52 targeted regressions](m3e-source25-theme-targeted.json)
passed with zero failures/skips and complete owned cleanup. This includes actual
ffprobe scanning and TCP complete/range byte delivery, retirement denial and
revoked-token denial in the synthetic real-media case. Full race regression and
the build workflow started against the same immutable source25, run
`client-backup-run-20260911_102522_aa52920473bf`. The [completed failed attempt](m3e-source25-theme-full-failed.json) found
`TestStoreSimilarAuthorizationScoringAndUserDataShareOneReadSnapshot` still
matching the old `SELECT type FROM items` query. Its intended permission writer
never ran after the production query gained the `i` alias. The test now matches
the actual query and explicitly asserts that the writer ran. The main package
group completed with 1,813 passing tests and that one failure; the final
`recoverydb` package and application build did not run. This attempt cannot
authorize an upgrade. Its exact empty pair was independently disposed, with
the recreated target public namespace pinned to OID 5791149; the original
receipt remains unchanged. Disposal report SHA-256 is
`68c0db5a658342ce7d5778565c6c3c5234a309008497e0e68e355469e53a2d69`.
`source-attempt-26` is frozen with 3,909 files and manifest
`df27faddb9864a9fa7fce00112c373261c8c4f08f32cfacaaf0e7bcbb72783a1`.
Only that Similar test differs from source25; production bytes are identical.
The [corrected Similar regression](m3e-source26-similar-snapshot-fix.json) passed
in `client-backup-run-20260911_105855_454c0c7f4aaa`, with complete owned cleanup.
The [complete-source run](m3e-source26-theme-full-failed.json)
`client-backup-run-20260911_105904_88799d500ae6` is terminal: 1,824 tests passed,
and `TestRecoveryDatabaseStoreIntegration` failed when the Theme semantic
validator replaced a cancelled finalizer's context error with `ErrDatabase`.
The other 23 packages passed; the application build did not run. The failed
report, log, receipt and restored HBA are retained. Read-only observation saved
both disposable databases as private custom dumps and captured their unchanged
rows, sequences and object identities. The source has the exact 33-table catalog
and one explicit owner-default ACL on `public.users`, left by the test's
GRANT/REVOKE sequence; the target is empty. Its independently reviewed operator
passed 12 remote ACL guards and disposed both pairs while preserving the original
evidence, dumps, HBA and preexisting catalog. Disposal report SHA-256 is
`bc6a6c11e2b1e31efaa2f96172641c3421d465c107cf03d260426010e6ced8bf`.
This attempt cannot authorize candidate deployment.
A later static review also found that
`containsAudio` reclassified basenames after the walker had already filtered
root-relative Theme paths. Ordinary Linux `Album/C:Track.mp3` therefore lost
its MusicAlbum boundary. Source27 removes that redundant classification and
adds root/subdirectory music/mixed scanner regressions plus canonical Theme
controls. Source28 additionally preserves cancellation and deadline errors
before and during backup semantic validation, with three focused test groups.
It is frozen at `source-attempt-28`, with 3,911 files and manifest SHA-256
`72e9301ba4405c6bddea15697e101dd62b157048f4c3b2e3e307da07ec0eb5df`.
Its [targeted recovery/Theme run](m3e-source28-theme-targeted.json)
`20260911_114314_f0eb92b8c61a` passed all 72 race regressions with zero
failures/skips and complete owned cleanup. The complete recovery integration
test passed, including refused, cancelled, lease-lost and committed finalizers.
The [complete-source race suite and build](m3e-source28-full.json) passed in
`client-backup-run-20260911_114525_fe88ae8d9829`: 1,830 tests across all 24
packages, zero failures/skips and complete owned cleanup. Report SHA-256 is
`8b2b755020d333de114458b064edc9d3f5e9df048d0a4d356a49f0cb80d2eab1`;
the 27,570,299-byte binary SHA-256 is
`83757e79a1694573e4c1fab83e18c67be5f0c2d8696246daccb91f009ab2efae`.
The [candidate upgrade](m3e-source28-candidate-upgrade.json) preserved all old
30-table rows, sequences and private/recovery files and installed schema26 at
PID 682417/start ticks 5168373. Its completed evidence is
`client-upgrade-92afabdded84813d3af44b6b0f86ff47/completed.json`, SHA-256
`954c4c8885e379ab70bdeb3cda84bdd47247768a6053e66cc43dec9566d04975`;
report `client-fixture-report-16937ed58a2985fd63d76c38.json`, SHA-256
`1c07e1eee7e9c00c9e44df78212e733a6915bb7a04e0e816461927e133d1c14c`.
The four-hop Music chain is `client-music-upgrade-chain-source28.json`, SHA-256
`b8d00674951487aa541dcd8d648d77f91293cc496c4f5002ea288af56660a63a`.
All six live lineage guards passed at `source28-music-lineage-guards-01`.
The [original-client album flow](m3e-source28-auxiliary-album.json) passed at
`source28-auxiliary-album-ui-01`, observation SHA-256
`0db4a10867673030e5fb5c730f245d92b9ab9e945f111fccd3386e25dd6e8ab6`.
Both auxiliary transfers completed HTTP200, the real UI returned Home, all
four UserData projections and preferences were unchanged, and logout204 plus
exact-token401 completed. There was no playback or page error. One console
error for a client-blocked resource is retained. The browser is closed.
The retained source26 database evidence is at
`/opt/goby-test/exec-work-m3e/source26-retained-database-evidence-01/observations.json`,
SHA-256 `b145cd7ab9f609bb9f8bf7448e2303bdd1384e65657f216496360d1eb85f2291`.
Its two custom dumps preserve the nonempty source and empty target before
disposal; no ACL or database repair was performed. The subsequent targeted
run uses source28/schema26 and all of `internal/database`,
`internal/backuppg`, `internal/library`, `internal/server`, then
`internal/recoverydb`, selecting
`Theme|TestRecoveryDatabaseStoreIntegration|TestPostgreSQL.*Schema(23|24|25)|TestMetadataMigrationFromThirteen`.
The whole recovery integration test ran, including finalizer cancellation,
lease loss, refusal and commit. The complete-source run and build follow
successful targeted verification; no source26 or source27 binary is eligible.
Its embedded operator/browser copies remain historical; the then-pending
schema26 deployment operator and lineage checker were verified separately.

The [schema26 candidate guards](m3e-schema26-upgrade-guards.json) passed 50
tests and 207 pure lineage checks. An initial 120-second guard timeout is
retained; a test-only local-scope reuse of already-validated catalogs removed
redundant reads while keeping production checks and the deadline unchanged.
The final Python guard run took 70.31 seconds. Operator SHA-256 is
`df9f0d33b9ff3136a6958a91d855faf47d703e7f5368a953cb0d182aa12bbd3b`;
its [read-only candidate inspection](m3e-schema26-preupgrade-inspect.json)
passed against the existing source20/schema25 process. The [operator code was
installed](m3e-schema26-operator-installed.json) under the fixture lock, retaining
the old code at `fixture-operator-schema26-replacement-01/previous.py` and
preserving the fixture state bytes. This did not replace the candidate binary
or restart its service. The [new browser input](m3e-schema26-browser-input.json)
at `source26-browser-input-01` passed all 17 syntax checks and retains the 15
unchanged source20 input07 dependencies. It replaces only the fixture loader
and lineage guard. It was subsequently used by the successful source28 flow.
Source21 remains the historical catalog-only bootstrap; source26 is a failed
baseline, and source28 is the corrected product snapshot with complete
regression, candidate upgrade and scoped auxiliary client acceptance passed.
Do not mutate any frozen source.

A draft
`upgrade-main-schema25.py` contains only unverified preflight/comparison
primitives; its mutating entry point is deliberately unavailable and must not
be used as deployment evidence.

Independent main-schema26 tooling was corrected through the retained failures
and completed deployment below. `schema24_test_gates` owns `migrate-main-schema26.go`;
`persistent_test_cluster` owns `upgrade-main-schema26.py` and coordinates its
memory guards. These tools are outside the frozen source28 product snapshot.
Their [remote build and 15 memory guards](m3e-main-schema26-tool-verification.json)
passed. Tool03 later completed the fresh backup described below, and tool04
completed rehearsal, migration, installation and start. Independent read-only
verification and formal owned-session smoke/finalization subsequently passed. Their scope is
the existing primary PostgreSQL5432/goby_test identity, with a fresh schema25
backup, separate receipted rehearsal and a protected 25-to26 migration. They
must preserve old rows, sequences, relation OIDs, ACLs and private/archive files;
the main database is never restored. The old schema25 tools remain immutable.
The source28 full run, protected candidate upgrade, Music chain and original
auxiliary-album workflow have now passed as recorded above.
The completed primary upgrade used the separately verified tooling and
successful product/client evidence linked below. Remaining unfinished inputs
must stay outside accepted checkpoint publication.
The Go helper was remotely formatted and imported back into the workspace,
with source SHA-256 `621306bfd3068ea40b49d0c74de3e61b54a5b9e3cd210cb29ce74b11dc4fe32d`.
Its built binary is `main-schema26-build-01/migrate-main-schema26`, SHA-256
`832487e56c62ff187fe069f9c5fc8144508d1f3e062654d5efdd7ad0c897e1a5`.
The new operator SHA-256 is
`b930b6474eebf5571a76a2a14083cad200493ccb9a1f9748fbfb603ec0193237`;
the 15-test guard SHA-256 is
`619fc02d64c50c164f81eb3fe732e35c4fffef80a71d86fee8da9aff45f24076`.
The independent frozen tool snapshot is at
`/opt/goby-test/exec-work-m3e/tool-build-main-schema26-01` (device 2049, inode
3177253), with evidence in `main-schema26-build-01/base-snapshot.json`.
It contains source28 plus its original manifest and the three new tools.
`main-schema26-tool-inputs.json` binds all 3,915 files; its SHA-256 is
`c522bf32b52af8e5aab3ac128eb5fa32a6845c7627f65e5a8fd3a776b85e59cf`.
Both Python files passed remote syntax compilation. The helper build and
all 15 guarded memory tests passed; source membership and bytes remained intact.
Build report SHA-256 is
`a96450f5f0c1d8e5804fd964e891d5672b09aeaf9d736a7214213342fe28e0f8`;
guard report SHA-256 is
`b7490b7d5adda3d6e949e2d5bbcfa98723a4b81fcd64c4cc8b870e4641edb432`.
Both reports are under `main-schema26-build-01`. The guards' matching unit
stdout was recovered from the private system journal into
`guards-01-journal.json`; empty systemd wrapper stdout was not treated as a pass.
The snapshot's published dependency is
`tool-build-source18-schema25-03/scripts/test-env/deploy-client-schema25.py`,
SHA-256 `6622a3b8d38d3956cbf7104ca88dd4f4227d67788c3b350be692d8f19df443ad`.
That initial tool preparation read the original published receipts and product
source without accessing the primary database or service. Later execution
reached the retained failures below. Backup, rehearsal, migration and start
subsequently completed, followed by the separate passing formal smoke/finalization; the old
primary tools must not be rerun.

The subsequent [first actual main preflight](m3e-main-schema26-preflight-failed-01.json)
is retained in `main-schema26-preflight-input-01`. The source18 asset archive
`schema25-tool-build-source18-03/candidate-assets.tar.gz`, SHA-256
`98b3cbc869bf9369464b8cdbb961b845118197999be6f9c934599507ab0f6cee`,
matches all 57 published asset members; source18/source28 web inputs are equal.
The exact main service pin SHA-256 is
`6b2904c4525aaf40d7d68cf0eda967fd395944b6d459079be282379eac364c8f`,
and the explicitly scoped release attestation SHA-256 is
`3fece4bbebaced7caff0eb5a5eba4314ee69334d2fb5d37bccdb3f7582076d01`.
Both are private files in that input directory; the release scope excludes
dual-user acceptance and full compatibility. Preflight arguments SHA-256 is
`9ff217273c19b09cf556e551d44d59ae67145af59263559d5ae285293d57388a`.
That historical preflight stopped in legacy `quiescent()` before any mutation. All five
active-work/trigger counts are zero. Activity has 15 rows, six older than one
day, zero older than 30 days, and earliest timestamp
`2026-09-10T10:26:41.150555Z`. At that preflight, the main process, manager, unit and both fixed
environment files have no retention override; PassEnvironment and
UnsetEnvironment also do not name the key. The actual default is 30 days, and
the three relevant retention source files match source18/source28 exactly.
Tool02 subsequently used a separately pinned startup plan with at most six
hours of validity and a 900-second startup reserve. Configuration, database
time, active-work counts and retention eligibility remain independent gates;
no retention setting or historical activity was changed to bypass them.
Keep tool01 and its failed preflight evidence immutable.

The later actual main attempt is retained at
`/opt/goby-test/backups/main-schema26-v1/run-20260911T125911Z-a3ceb3e924e2ea287959135c`.
Its terminal evidence SHA-256 is
`c0048eec237292cbda89e7b43c2c6b112bf74d22d44cc1b38419662af36b714f`.
The recorded transition is `stopped` to failed `baseline`, with
`column_acl_capture_failed`. At that failure the primary was **STOPPED** with
the old source18 executable and schema25 database, retaining 15 activity rows.
No main migration occurred in that run. The independently scoped
`/opt/goby-test/exec-work-m3e/main-schema26-acl-regression-01` passed one actual
PostgreSQL17 read-only regression. This is scoped ACL-capture evidence, not a
successful deployment. Preserve this failed run independently of the later
repair attempt below.

Tool03's helper build, 26 memory guards and repair preflight passed. Its
[actual repair-baseline attempt](m3e-main-schema26-baseline-repair-failed-01.json)
is retained at
`/opt/goby-test/backups/main-schema26-baseline-repair-v1/run-20260911T131730Z-85526803ca1a8b52a26f0ab7`.
It reached `backed-up`: the fresh baseline and custom dump share one snapshot,
with SHA-256 values
`5366745c3c6dbd6259cb7988d8d2d1986065a16e738f5ebe488c6f4f14ca5082`
and `c821a16a2e2c56c1d094fda822c5bfce32cecb6cc523ddb5706a950c9d6ac7e5`.
Before any rehearsal, `observe_rehearsal` failed because its SELECT reads
`r.rolconfig` from `pg_authid`, which has no such column in PostgreSQL17.
The terminal evidence SHA-256 is
`acc7b4d173f3975399cf58dc1c6b1e9a27658941d2d028bad9424cb287f61107`.
Read-only reproduction confirmed that query failure. The preserved observation
`/opt/goby-test/exec-work-m3e/main-schema26-repair-failure-review-01/failure-observation.json`,
SHA-256 `7ac34458ccea3b168d76a1981d717996d8ba050a48fb01fdc6c673565ec53764`,
records the still-stopped schema25 main, 15 activity rows, no other
`pg_stat_activity` connections to database OID16385 excluding the observer,
and zero rehearsal databases or roles. It does not report an empty business
`sessions` table. Tool04 subsequently performed the distinct forward attempt
below. Both earlier failed trees remain immutable. The retained startup deadline is
`2026-09-11T18:56:44.739406Z`; forward work must recheck that gate and its startup
reserve. Full private baselines and material inventories are not published.

The [tool04 attempt](m3e-main-schema26-start-verification-failed-01.json) is retained at
`/opt/goby-test/backups/main-schema26-rehearsal-repair-v1/run-20260911T132850Z-7f17a39a6e1e08ce9639d6a1`.
The rehearsal completed and was removed. Main schema26 migration is recorded
in `main-migrated.json`, with its post-migration capture completed. Installation
and start completed: the primary was then active/running as PID688833 with the
source28 executable. The original terminal still reports failure at
`start-requested`, after 71 intents and before smoke. Its effective-identity
error is retained verbatim in the public summary. Later observed UID995/GID986
match the fixed operator's expected values; the cause remains unresolved.
The later independent read-only review passed the original tool04
`exact_service`, `verify_installed` and `preserved_private` checks, allowing only
normal log append. It binds PID688833/start ticks5620918, schema26, 15 activity
rows, 22 theme-owner rows, zero reserved paths/resources and zero rehearsal
databases/roles. Its observation is
`/opt/goby-test/exec-work-m3e/main-schema26-post-start-review-02/failure-observation.json`,
SHA-256 `003928387a5ee8a656c34410beae89a7159ed50fac374ecc07f37ce730c12365`.
The original terminal SHA-256 is
`6a250c98ad0baf5e770e565e7790967f9be9465e62eadc9e4a0f0075d6bd6c43`;
`main-migrated.json` SHA-256 is
`2cdd286ddf9012d8acc5423250967098a8d42180927cf935491904e2e193f5db`,
and the completed post-migration capture SHA-256 is
`e95c284b08f18cba6417f17cf7d724799d22e75012b4a3fea35cfa65dcefc2ec`.
Preserve this third failed tree. Its later formal completion used only a new
owned-session smoke and final evidence, with no further migration or restart.

The [tool04 build and 27 memory guards](m3e-main-schema26-tool04-verification.json),
[helper root/ACL regression](m3e-main-schema26-helper-regression-02.json) and
[read-only PostgreSQL17 catalog regression](m3e-main-schema26-catalog-regression-01.json)
provide separate tool evidence. The final [primary completion](m3e-main-schema26-completed.json),
public summary SHA-256
`99a990f10a465c5cee88361a142e6f848f28b9757cc8a18da0008833632b3c69`,
passed in the new root
`/opt/goby-test/backups/main-schema26-post-start-v1/run-20260911T134029Z-ffa629d4c596db46c07beeeb`.
Its terminal SHA-256 is
`f77348323a7040d1cbe2c67a96055e1961fe88f1c4ffc73058cc154dbbcb65b9`.
All 13 native calls passed: health/readiness, administrator index, new session
login/readback, overview, capabilities, libraries, tasks, backups/status,
logout204 and the final session401. No backup creation, archive deletion or
restore was requested. The process remained PID688833/start ticks5620918;
schema26 retained 22 theme-owner rows, zero reserved paths/resources and no
rehearsal database or role. Old state was preserved at migration commit, and
the new login/logout audit history remains, raising activity from 15 to 17.
The completion performed zero service mutations, migrations and restores.
All three original failed trees remain unchanged and retain failed status.
This closes the primary deployment gate, not full dual-user or M3 acceptance.

The reviewed product files were committed and pushed to `origin/main` as the
[source28 product checkpoint](source28-product-publication.json), commit
`608e2088ca6aef150833d3f9bbc954c03e5aef5b`. Only the 73 staged internal
product/test/catalog changes were included. The published tree
`da8fcd68b41c691c17c5dd0b4d760e38d3606ec3` was exported to
`theme21-transfer-01/source28-product-index-01.tar.gz`. Its remote comparison
with source28 found no missing or extra product/test files: 3,212 are byte
identical, and the same source20 JSON fixture has only the already-reviewed
CRLF-to-LF normalization. The fixture and its consumer are unchanged from
source20, parsed JSON is equal, and no production literal/embed consumer exists.
Review report: `theme21-transfer-01/source28-product-index-01-reviewed.json`,
SHA-256 `46d192f31fc1644b9009e7a4f1dfc6220813a46e3c0c5b53f67ac41cbec798f6`.
This comparison excludes documentation and operator/browser scripts. The
published tree exactly matches this reviewed tree and reuses the successful
1,830-test source28 full run and matching candidate build. Publication closes
the product checkpoint only; primary deployment later passed through its own
completion evidence. Active tooling/documentation and complete M3 acceptance
remain separate. Later checkpoints must separately bind
those other accepted inputs.

The separate dual-viewer original-client read-isolation harness is frozen.
Its [25 pure mock guards and two syntax checks](m3e-cross-user-input-guards.json)
passed remotely. Input closure: `cross-user-input-01`, containing the two new
scripts plus unchanged `client-browser-goby-fixture.mjs` and
`client-browser-session-proof.mjs` from `source26-browser-input-01`.
Harness SHA-256 is `c2622a85c02deadc2f27a6e466e20931b98bff2ae7492e1bdabd9703dcb8f80f`;
guard SHA-256 is `d12c33f50c50ce652d410e671f0ced5e21db1bb0f83539eac6db7ee99bb0fcfa`.
The [first actual browser attempt](m3e-source28-cross-user-failed-01.json) at
`client-cross-user-source28-01` failed in `login_A` before any login request
was submitted. A had three successful GET reads, zero network guard counters
and no page errors; it closed with no cleanup failure. B never started and no
token was captured. The report SHA-256 is
`6224b7e1753ee91c0f06ba16acc090d0bfb2c04937e883d9524415599c396b61`.
Elapsed time was 15.82 seconds, Playwright1.63.0/Chrome153.0.8010.12. This failed
attempt and input01 remain immutable. Input02 added safe diagnostics and an
explicit prelogin mode; both new syntax checks and all 35 pure guards passed.
Its harness SHA-256 is
`9ccbcf5fbfabd2635b2a724b1c65d87d2cc7824aac3b7e7d8c344475125c3554`,
and guard SHA-256 is
`c340f354b9ca9778d450d4b6e209a287b67641278c00eda23c5d3e3bddada646`.
The [fresh anonymous startup diagnostic](m3e-source28-prelogin-diagnostic-01.json)
at `client-cross-user-prelogin-source28-01` reproduced the empty document and
login-control timeout, with no credential fill or submission. It observed a
Service Worker warning, zero controller/registrations, an empty body and only
three successful reads; report SHA-256 is
`48dd4d537d17190a8563437bd13a9b5605441b60b37e21181d111a727fac0bed`.
The earlier [client acceptance record](client-acceptance-m3e.md) already
establishes that the normal client requires its Service Worker at startup.
Input03 preserves normal Service Workers and uses an enforced forwarding policy
proxy for browser/worker HTTP traffic without a same-origin bypass. Its fresh
anonymous prelogin run at
`/opt/goby-test/exec-work-m3e/client-cross-user-prelogin-source28-02/report.json`
passed, SHA-256
`94f3034e0ac4ccb4c5c4bf0f05ba36bea36c55d7684d0ead49180a2e2a32791d`.
All 202 observed network requests completed and Service Worker startup worked.
No credentials were submitted. This is diagnostic-only evidence, not login,
dual-user isolation or playback acceptance. At that stage actual acceptance
still awaited explicit WebSocket handling. The backend's
inbound envelopes are inert, but outbound controls can affect the client, so
an HTTP GET classification alone is insufficient. Preserve all old input/output
directories and never relabel diagnostics as user-isolation acceptance.

The [input04 actual run](m3e-source28-cross-user-failed-02.json), public summary
SHA-256 `acb8adac9aa30c9b671f36b9f8a2d316c70220ec8dabe3d598ed31624cda44a6`,
followed 92 passing guards. Both ordinary accounts completed UI login200 and
owned Movie detail UI200 transfers. Own API reads200, admin/foreign reads403,
four unchanged UserData projections and preserved preferences/configuration/policy
are recorded. Both browsers completed UI logout204 with exact-token401 and
closed. Each WebSocket CONNECT200/upstream101 handshake was delivered and the
connection closed; this is transport evidence, not a full event/control matrix.
The immutable actual report is `client-cross-user-source28-02/report.json`,
SHA-256 `71d16d2df285637ea67497dce78bfaa6e20ebbacca0559c7af731919475c42a8`.
It remains failed at `browse_A`: return Home was not completed. Each original
client also attempted automatic PlaybackInfo on the detail page, which the
observer blocked. That endpoint performs database preparation writes and must
not be classified as read-only. No causal explanation for the missing Home
step is asserted. Input05 subsequently added Home diagnostics and an explicit
`acceptance-preparation` mode, whose actual scoped result is recorded below.
Default acceptance retains the prior read-only restriction.

The reviewed input05 scope permitted one physical PlaybackInfo POST per actor only
for the receipted Movie, during its detail-page phase, using that browser's
freshly proven ordinary-user token. It required a real200 response without
rewriting the request or fabricating a response. Media delivery, Playing,
remote control and unrelated writes remain blocked. Both users had no live
playback, and their Movie UserData rows already existed in the read-only
candidate observations. The permitted old changes were B's expired Prepared row
becoming Expired and deletion of A's reference to a revoked authentication
session. No old play-session deletion or encoding job is eligible. New rows
must belong to the fresh browser authentication and the bound Movie/source;
all four UserData projections, preferences, configuration and policy must
remain unchanged. This is preparation acceptance, not a read-only or whole-DB
preservation claim. That allowance is specific to the recorded input05 run.

Input05 used the frozen read-only
`inspect-client-prepare-scope.py`, SHA-256
`2533c8804843eb5bff542f88829bd85196f0ad297586d9f8e8e5cdb6fb241ef8`,
at `/opt/goby-test/exec-work-m3e/prepare-scope-tool-01`. Its remote syntax and
complete PostgreSQL query check passed, observing 18 plays, one reference,
five UserData rows, 47 authentication sessions and zero encoding jobs. That
initial check created no before/after ledger; it is historical evidence, not
authority for a later run. The actual input05 run has its own retained private
before/after comparison, summarized below.

The [input05 public summary](m3e-source28-cross-user-preparation-01.json), SHA-256
`91a8d3bc7a2e604cd62f5041b7a9403463baacb88c53901649c11a52f8b7295a`,
records 100 passing pure guards and the actual `acceptance-preparation` workflow.
Both users completed Home to owned Movie detail, a genuine PlaybackInfo200
transfer, return Home, UI logout204 and exact-token401. Own/foreign isolation,
four UserData projections, preferences, Configuration and Policy checks passed.
The raw report at `client-cross-user-source28-03/report.json`, SHA-256
`023e5169f9f7c5e9a7310733be24e94a9de91c5175f1d7f63a14195702bb6849`,
retains its driver-level passed result. Each user nevertheless recorded one
unclassified page error in `ui_movie`; that driver did not reject page errors.
The accepted result is therefore scoped flow/state evidence, not complete
client acceptance, and the raw report is not rewritten.

The private database comparison retained all 18 old play rows: 17 unchanged
and B's eligible row moved to Expired. A's one reference to revoked
authentication was removed. Two new Prepared rows belong to two new Emby
authentication rows, both now revoked after UI logout. All 47 old authentication
rows and all five UserData rows were unchanged; encoding remained zero. These
are the explicitly bounded preparation effects, not whole-database equality.

The input06 starting point was 20 play rows, 49 authentication
rows and zero client-playback references. Input06 explicitly selected
`--preparation-scope source28-page-error-01`. Its only permitted old cleanup is
the two input05 Prepared rows, whose authentication is revoked, becoming
Expired. No old reference may be deleted; the other 18 old play rows, all 49
old auth rows, five UserData rows and zero encoding rows must remain exact.
One new Prepared row and fresh authentication per actor are separately bound.
The immediately preceding fresh snapshot must match the private input05 after
image row by row, whose SHA-256 is
`4aff79e9798fcab89652a5bdc3b83b408cdc67a1a52b8d086a6ff99c8e3f6a63`.
Neither that old image nor the original 18-play/47-auth comparator is itself
fresh authority. The new comparator was separately frozen and verified.

Input06 added the strict `page_error_count === 0` gate and bounded, sanitized
message diagnostics from normal browser events. The observed reproduction
contrast is input04's blocked PlaybackInfo with zero page errors versus
input05's real PlaybackInfo200 with one error per client; it does not establish
the error's mechanism. Keep normal preparation in the explicitly bound
diagnostic scope. The original input05 comparator remains frozen and must not
be reused for that new baseline.

The actual input06 report is `client-cross-user-source28-04/report.json`,
SHA-256 `0de99523578ae67df05c424940e12b1d42b3a2d457196e77c8d55081bf88ee70`.
It correctly remains failed with `page_errors_observed`; both users completed
navigation, preparation200 and exact UI logout204/session401. The missing
SpecialFeatures user-item GET returned404 for each actor, with a complete
104-byte proxy response and a matching `Response` page error. These exact
paths were identified by matching their SHA-256 against the recorded request
hashes, without inspecting client implementation code.

The input06 before/after records in `prepare-scope-observation-source28-02`
have SHA-256 `ab0333aa939c3193aa6348a845badcb0805e162674568bebff4806818f17a01a`
and `0c005aeb0a2ea4a0b8362650a7e62102f1b938828be7c0cbe735ea92d3cf279c`.
Comparator02, SHA-256 `ae6cce79aabb157e790fea6fe47d06c82b9e0fd0b7b684c2ea0a85cf94d32e68`,
passed with output SHA-256
`381f921c92314807f96cc82e857ec18e703d5133e1a18738f8b56ed3cf851641`.
All 20 old play rows remain, with only the two approved Prepared rows becoming
Expired. All 49 old auth rows and five UserData rows remain exact. There are
two new Prepared rows and two new, now-revoked auth rows; references and
encoding jobs remain zero. That historical input06 result had 22 plays and
51 auth rows. Input07 subsequently consumed that baseline and reached 24 plays/
53 selected A/B auth rows; input05/input06 before images and scopes must not
be reused for a new run.
This result does not authorize replaying either earlier preparation scope.
That input06 result does not claim error-free acceptance; library restriction
was then only planned. The later API matrix and inspection passed separately.
The harness uses existing AV viewer A and initial
viewer B. It is designed to prove own-user reads and cross-user denial, compare four media
UserData projections and preferences after UI login and before logout, and
require each exact logout token to be rejected. Normal authentication history
may change; the report does not claim whole-database equality. Required CLI
inputs are candidate SHA, source path and manifest SHA, original Music receipt
SHA, complete Music chain path/SHA and a new direct WORK child output directory.
No policy mutation or playback was admitted in these attempts. Temporary library
policy restriction/restore was a separate [planned gate](m3e-library-restriction-plan.md)
at that checkpoint. Its later API matrix proved hidden-library404 and cross-user403;
the original-client UI permission-change gate remains open.

The first resumed observation found a rebooted `test-env`: the persistent primary
PostgreSQL cluster was active, but Goby was stopped and the transient reference
service and memory-backed test cluster were absent. The installed Goby binary
still matched the accepted M5j SHA-256. Historical process IDs below are no longer
live identities. M3e records the new isolated verification environment; never
reuse a historical PID or recreate missing scratch data from an old owner record.

The previous isolated M3e candidate was source18/schema25, executable SHA-256
`665df2d3851dc1b4a251012805678559e17e274c08f2548aead560e593a08d2b`,
PID 506532/start ticks 2681316. Its [19 targeted regressions, build and protected
schema25 replacement](m3e-source18-targeted-upgrade.json) passed. Source is
`/opt/goby-test/exec-work-m3e/source-attempt-18`, manifest SHA-256
`fa46e1547416afcb65c15f97aa5a1b7d3c344579386a424478ec310f0e01660b`.
All 30 existing tables were preserved, including nonempty music metadata, four
music-role links, user data and preferences. Source18 client validation now uses
the explicit two-hop source15-to16-to18 upgrade chain, SHA-256
`68571d8ca7f418f5dbab4f41d233f00ed8a3e462829ea8ac93e8ec29c2b8283f`,
at `client-music-upgrade-chain-source18.json`. Source17 was never installed.
The [source18 audio record](verification-m3e-source18-audio.md) now establishes
both core journeys: actual playback, pause, both seeks, resume, stop, eight
Playing/Progress/Stopped HTTP 204 responses per format, persisted play count
and position, and UI logout followed by exact-token HTTP 401. MP3 retains its
original return_home harness failure: the saved DOM had already returned Home,
where new Continue Listening and Latest Music sections legitimately duplicated
the album link; its old SPA route was not captured. No MP3 replay or historical
reset was performed. FLAC completed the corrected full flow, including the
actual `/web/index.html#!/home` route and receipted Music library card. Two
auxiliary 404 responses and two page errors remain per run. Both browser sessions
are closed. [Full source18 regression](m3e-source18-full.json) passed 1,741 top-level race
tests across all 24 packages with no failures or skips in
`client-backup-run-20260911_052641_fbc90d022811`. The final build matched the
candidate, both disposable pairs were removed and the original HBA/catalog
were preserved. That source snapshot remains immutable; its process was later
replaced by the source20 candidate above.

The historical [main source18 deployment](m3e-source18-main-deployment.json) passed
and ran schema25 with that same executable SHA-256, PID 539535/start ticks 3115871.
That historical primary process was stopped during the main-schema26 work;
source28/PID688833 succeeded it and is now historical after the source32/schema27 primary deployment.
The isolated candidate is now source32/schema27, PID748513/start ticks6996875;
the former source28 process was PID682417/start ticks5168373.
All old business columns and sequences were preserved through the
29-to-30-table schema25 migration, together with the old archives.
Health, readiness, administration, login and seven reads passed, then logout
returned 204 and the exact token was rejected with 401. The empty transcode cache
passed seven independent guards and actual creation. No main restore or old
rollback was performed. Deployment evidence SHA-256 is
`02ed027b353488ab31cb9e4ac3e7cfc4547422bb1a57e7f9cfdfdd945aad0bf3`.
This historical deployment is a partial M3e checkpoint, not completion of the
full milestones. Its auxiliary failures remain retained separately from the
later source28 candidate's scoped success. The checkpoint records the product
and its verification evidence together.

Historical schema25 main-service preparation passed with its tool03. The earlier
[two attempts stopped safely](m3e-schema25-preparation-failed-01-02.json)
before any Go helper execution, database dump, rehearsal or migration. The
first retained 423 material files and exposed recursive mkdir's non-private
intermediate directory; tool revision 02 now creates every component as 0700
and passed 44 guards. The second completed all 444 material copies but rejected
the one-second difference between the PID-file time and SQL postmaster start.
Tool03 separated the SQL and OS timestamp identity checks and passed 47 remote
guards and its build. The subsequent
[fresh preparation](m3e-schema25-preparation-source18.json) passed the same-snapshot
schema23 dump, independent schema23-to25 restore rehearsal and owned cleanup,
with the old main state preserved exactly. Its retained receipt is
`/opt/goby-test/backups/client-schema25-v1/run-20260911T062405Z-975ef4c2e0c2b3078d517dc5/prepared.json`,
SHA-256 `b1d686b1c8aed83aa545dd02618c22793cc8b371f6659a0adc1f40710ddfd0ce`.
Both historical failed runs remain under
`/opt/goby-test/backups/client-schema25-v1`; do not resume, delete or chmod them.
That preparation preserved the then-stopped M5j/schema23 main state. The later
successful deployment above migrated and started source18/schema25 while
preserving the recorded old data and archives; it did not restore the main
database or apply a historical rollback.

The preceding [full-source16 verification](m3e-source16-full.json)
passed 1,739 top-level race tests across all 24 packages with no failures or skips
in `client-backup-run-20260911_043127_5d9524c1f6be`. Its final build matched the
then-installed source16 candidate, both disposable pairs were removed, HBA was restored and
the preexisting cluster catalog remained unchanged. Original-client audio acceptance
uses the exact completed upgrade to connect the existing scan receipt to the
new candidate process. The source16 audio attempts below remain historical
failure evidence, superseded for the scoped audio gate by source18 above.

The historical [source16 MP3 and FLAC UI runs](verification-m3e-source16-audio.md) received
HTTP 206 with the expected audio MIME types and complete actual playback
advancement, pause, forward/backward seeking and resume. Both still fail the
strict stop gate: Playing, six Progress requests and Stopped each return HTTP
404, and user playback data remains unchanged. Both UI logouts returned 204,
showed the login page and rejected the exact token with 401. The input archives
and process pins are preserved. The playback-report correlation investigation
below explained the source18 fix. These source16 runs do not establish audio
journey or progress-persistence acceptance; source18 supplies that later evidence
and its full regression has passed.
The [single bounded diagnostic](verification-m3e-source16-audio-report-diagnostic.md)
confirmed matching nonce, item, token and device values after media HTTP 206,
while MediaSourceId was the bare item ID rather than its `mediasource_` form.
The small error response bodies could not be read, so the diagnostic remains
incomplete for ErrorCode collection; the independent ID comparisons are retained.
Its UI logout and exact-token rejection passed.

[Source17](m3e-source17-snapshot.json) adds only a narrow owned correlated Audio
source alias after the current item authorization lock, retaining the stored
canonical source and all other rejection boundaries. Its [targeted attempt](m3e-source17-targeted-failed.json)
had 18 passes and one new test-stage snapshot failure: denied reports were
correctly rejected, but the assertion also covered the subsequent existing
preparation cleanup that expires inaccessible sessions. Source17 was never
built or installed. Its independently reviewed empty pair was removed and all
failure evidence retained. [Source18](m3e-source18-snapshot.json) changes only
that test's stage boundaries and exact cleanup assertions, manifest SHA-256
`fa46e1547416afcb65c15f97aa5a1b7d3c344579386a424478ec310f0e01660b`.
The same targeted suite passed all 19 tests on source18, followed by its build
and protected replacement. The source17 failure is retained as a test-boundary
finding; no production change was made between source17 and source18.

The preceding source15 [schema24-to-25 replacement](m3e-source15-targeted-upgrade.json)
preserved every old business column and added only the two reviewed default
columns. [Real subtitle and ordinary TV checks](verification-m3e-source15-subtitle-tv.md)
passed on source15, including UI stop/logout and exact-token HTTP 401.
Both browser runs are closed. The [controlled Music scan](m3e-music-scan.json)
then passed using [42 verified guards](m3e-music-scan-guards.json). Album ID
`002c2ea2c77769ae9b0b84b6e788998b` now displays `M3e Synthetic Album`; the unchanged
MP3/FLAC IDs display `M3e MP3` and `M3e FLAC`. MusicArtist ID 1 and four real role
relationships were indexed. Original media bytes, item IDs/paths/parents, all
user state and both unstarted revoked-credential Prepared records were preserved.
The scan's native administrator cookie was revoked and independently rejected.
The [source15 MP3 attempt](m3e-source15-mp3-blocked-summary.json) selected the
exact track row, but the client's Audio/universal media request returned HTTP
400 before decoding. Its subsequent Playing/Progress/Stopped reports returned
404. User data and preferences were unchanged. The error dialog prevented UI
logout; exact owned-token API cleanup returned 204 followed by 401, which is
cleanup evidence rather than UI acceptance. The [bounded FLAC run](verification-m3e-source15-scanned-music.md) also received
HTTP 400 with `invalid_audio_request`; its observed Container capability list
includes `container|codec` items rejected by the source15 parser. FLAC UI logout
returned 204 and the exact token was subsequently rejected with 401. Source16
adds explicit qualified capability parsing without weakening output
selectors, permission checks or bitrate limits.
The [full source15 suite](m3e-source15-full-failed.json) finished with 1,722
top-level passes, two failures and no skips across 23 completed packages in
`client-backup-run-20260911_034930_0b4570e66109`. It failed
`TestQueryLatestProjectsSourceAndRepresentativeEntities` and
`TestItemQueriesProjectEntityDisplayNamesAndCreditOrder`. Both old expected
structs omitted the new empty Artist/AlbumArtist collections. Source16 made
those expectations explicit while retaining every legacy entity
ID, name and order assertion. The final recoverydb package and full build did
not run. The retained pair was independently confirmed empty with no active
connections or external dependencies and [removed by its reviewed operator](m3e-source15-full-disposal.json),
preserving the failed receipt, log, report and preexisting cluster catalog.
Source15 is not a full-regression pass. The subsequent runner also records the explicit
source manifest digest in its full report; all [73 existing guards](m3e-runner-manifest-guards.json)
passed remotely.

[Source16](m3e-source16-snapshot.json) is frozen as a complete source15 clone with
seven reviewed overlays: the qualified Universal capability parser and its
tests, the two corrected library expectations, the report manifest field, and
the audio contract. Its manifest SHA-256 is
`066aad0a220342e01428dedd359a65d5822c790b9f7dd0d3facc4d7799f38a72`
across 3,796 files. Every migration and trusted catalog is unchanged. All 28
targeted audio and library regressions passed in
`client-backup-run-20260911_042847_73f4d02c4437`, with exact pair cleanup; the build
and same-schema upgrade also passed. In-flight client-lineage and
deployment tooling are separate inputs and were not copied into source16.

The independent schema25 deployment operator is implemented in
`scripts/test-env/deploy-client-schema25.py`, with
`test-deploy-client-schema25.py` and `migrate-client-schema25.go`. The old
`deploy-backup-recovery.py` remains unchanged and must not be rerun.
Its [42 memory-only guards](m3e-schema25-deployment-tool-guards.json) and
[separate Go helper build](m3e-schema25-deployment-helper-build.json) passed
through SSH. At that historical tool checkpoint the helper had not executed and
no fresh main backup or rehearsal had run; tool03's later successful preparation
and deployment are recorded above. The preliminary tool source is
`/opt/goby-test/exec-work-m3e/tool-build-source16-schema25-01`, manifest SHA-256
`562d35476b1602284ceaccc1da30271d7497542fb06a49e5ed0f9e63b57f9097`.
Artifacts are in `schema25-tool-build-01`; helper binary SHA-256 is
`a5f8afb49ee610c31d5f70260c5fcf306ebdd74103e11344acb5a20a469bfd60`.
This tool build is bound to source16. If the final product source changes,
create a new complete tool-source manifest/build against that final source;
do not relabel this report. Source18's client and full-regression evidence and
the later preparation and main deployment now satisfy those completed gates;
broader compatibility remains open.
The source16-bound [read-only deployment preflight](m3e-schema25-preflight-source16.json)
also passed: artifact/source/guard/build bindings and the protected stopped M5j
state matched, with no operator writes. Its archived assets are a new private
copy of the preserved M5j artifact, SHA-256
`98b3cbc869bf9369464b8cdbb961b845118197999be6f9c934599507ab0f6cee`.
This preflight did not create a fresh operational backup, run the helper,
rehearse a restore, migrate a database, install assets or start the service.
The independent [source18 helper build](m3e-schema25-deployment-helper-source18-build.json)
was recorded in `schema25-tool-build-source18-01`, from
`tool-build-source18-schema25-01`, manifest SHA-256
`8f6c37edaae54c8359a11a76b869cc4fdd191278f48841ee808998143f14e7f8`.
Its binary matches the preceding helper bytes; its new build report is bound
to the actual source18 manifest. The unchanged Python operator/guard inputs
reused the exact prior 42-case guard report. This historical build precedes the
tool02/tool03 revisions and the successful fresh preparation above.
The bounded [post-source18 auxiliary-read plan](client-auxiliary-reads-plan.md)
records the remaining Similar/ThemeMedia evidence and contract gaps. It is a
read-only follow-on audit, not a product change or an empty-response workaround.

The [full source12 suite](m3e-source12-full.json) passed 1,693 race tests across
24 packages, including the final recoverydb package, with no failures or skips.
That build matched the earlier source12 candidate. Both disposable pairs
were removed, HBA was restored and the preexisting catalog was unchanged. That
run is terminal; it is not complete-source15 regression evidence.

Source11 established the original client home, core movie lifecycle
and display-preference workflow. Movie play/pause/forward/backward seek,
stop and UI logout/login/resume completed with real HTTP 204 state reports and
two exact-token logout checks. Auxiliary movie reads still return 404 and cause
recorded page errors, so complete Emby compatibility is not claimed. The
nondefault preference process-restart gate passed and restored the original
viewer's explicit `genreLimitOnDetails=1`. See the active record for evidence.

Source11 passed 26 targeted profile/NextUp regressions and built successfully.
Its [full repository attempt failed](m3e-source11-full-failed.json): 1,668
top-level tests passed, but one legacy device assertion expected HTTP 400 for a
malformed query that the credential parser now rejects with HTTP 401. The final
recoverydb package and full build did not run. The assertion is corrected in the
source12 inputs. The [reviewed empty disposable pair was removed](m3e-source11-full-disposal.json),
with its failed receipt, report and log preserved; that failed run is terminal.
Source10's single canonical JSON test failure is retained; its corrected test
separates empty-slice representation from semantic equality. Source10 was never
deployed. M5j is the previous published baseline; the main retains the
historical source18/schema25 checkpoint; source28/schema26 subsequently ran with
primary acceptance completed separately as recorded above.

The fixture now has one explicitly added ordinary AV account,
`34b4c24f6568659af7ce17938fae7f81`, with a protected creation receipt and private
`goby-av-browser.json` alias in the work directory. The updated fixture operator
SHA-256 is now `85b1c7ac16646d0af19cd67d649b72f3df6cb7e08eab4357b94d5b6e6ab818de`;
all 37 guards and the real schema25 upgrade passed. The earlier three-account
operator is retained in `fixture-operator-schema25-replacement-01/previous.py`.
Subsequent inspect/upgrade must use the schema25 operator or a verified successor;
the older operators cannot accept the current fixture. Original users, preferences, playback state and
credentials were preserved. The original viewer's legitimate movie position is
now approximately 122.86 seconds with play count 2. AV/TV client runs must use
the added account. The source11 AV/TV runs finished and closed their owned
sessions. [Reference AV](verification-m3e-reference-av.md)
and [TV browsing](verification-m3e-reference-tv.md) passed within their documented
scope. The historical [Goby source11 AV/TV](verification-m3e-goby-av-source11.md) was blocked:
the music album page is blank, subtitle labels are both `en`, and TV browsing
shows repeated Specials. Source12 changes address the missing music
collections/actual child count, external subtitle labels and observed TV query
filters. A separate static review found that the newly strict PlaybackInfo
decoder rejects existing successful reference requests containing the unmodeled
`VideoStreamIndex: 0` extension. Source12 restores the previous bounded
extension-tolerance contract; it does not implement nonzero video-track
selection. Its targeted/full regression and isolated upgrade passed; music and
subtitle acceptance were still open at that source12 checkpoint and were later
resolved within the source15/source18 scopes above. The initial
source12 upgrade preflight rejected a 0600 nonsecret manifest where its reviewed
contract requires 0644; it stopped before service mutation. Correcting that
file's mode left every source byte unchanged and allowed the protected upgrade.

The [source12 original-client TV flow](verification-m3e-goby-source12-tv.md) has completed: two seasons, three ordinary
episodes, detail and Home navigation, unchanged user data, and UI logout followed
by exact-token HTTP 401 (`goby-av-source12-tv-01`). Auxiliary HTTP/page errors
remain recorded; this is ordinary-fixture browsing acceptance. Source12 music stopped at a blank
album page: the four collections and real count now arrive, but the caught
exception changed from reading `0` to reading `Name`. Subtitle labels now work;
selecting SRT delivers the native SRT URL/text and no opening cue, while the
reference delivers VTT. [Controlled UI response observations](verification-m3e-goby-source12-av.md)
confirmed that three PlaybackInfo responses retained native SRT candidate URLs
before local selection without renegotiation. The source15 candidate now projects
the explicit profile onto all supported original-source candidates while retaining
Off and default item descriptions. The source15 UI subsequently rendered SRT
and VTT opening and seek-window cues, restored Off and stopped successfully.
SRT's converted VTT matched the reference bytes; native VTT retains its documented
formatting difference while displaying the same fixture cues.

The [schema25 music increment](client-music-metadata-plan.md) added real music tag indexing and MusicArtist
relations on schema25, plus a separately evidenced subtitle-candidate negotiation
fix. Schema25 is installed on the isolated candidate. The music design keeps technical probe
version 6 and adds independent music metadata version 1, preserves physical item
IDs and existing metadata overrides/locks, and adds MusicArtist identities with
Artist/AlbumArtist roles. A new `item_metadata_state.music_source` JSONB column
defaults exactly to `{}` for old rows; there are still 30 tables. The entity
relationship `credit_group` defaults to 0 for old rows and uses bounded
groups 1/2 for Artist/AlbumArtist. This group enters the new primary key while
unbounded legacy `credit_type` text remains unchanged. Both new-column defaults
need explicit preservation checks. [73 new runner guards](m3e-schema25-runner-guards.json)
and [trusted catalog25 generation](m3e-schema25-catalog.json) passed. The bootstrap
source13 is catalog-only, with manifest SHA-256
`e927174f3da71e16cc21e92148183178df892c3ff51c831f160cf8adda65c14b`;
its generated catalog SHA-256 is
`e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b`.
The owned generation pair was removed and HBA restored. The independent
[source14 migration/recovery gates](m3e-schema25-targeted.json) passed 12 tests,
including the real schema23/24 encrypted transitions and long legacy credits.
[All 37 candidate upgrade guards](m3e-schema25-fixture-guards.json) also passed
against the actual catalog25; no candidate mutation was performed. Source14 is
a schema/backup verification source, with manifest
`abbb5dee98e3546a4b70b333bd91874a0f730d02a698f19b744d65e425313785`.
Source15's targeted regression and build passed before its historical install;
its full-suite and original-client audio failures are preserved above. Source18
is now installed, its full suite passed, and its scoped audio outcomes are recorded.
Existing
migrations and catalogs23/24 stay immutable.
Only the owned Music library was scanned, through the separately verified
one-shot operator at `music-scan-review-02/scan-client-music.py`, SHA-256
`dca0a057c82b0ac29734c320cdfe6aab57a31abae9469ef8dc10c3749a13658e`.
Its fixed output `client-music-scan-v1` is complete and must not be rerun or
adopted. Receipt SHA-256 is
`e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b`.
The two scan runtime scripts are separate from the immutable source15 build
inputs. Review01's [preflight rejection](m3e-music-scan-preflight-01.json) occurred
before any login or scan intent; the revised guard preserved the two legitimately
unstarted records instead of deleting or relabeling them.

## Project and accepted baseline

Goby is an open-source, independently implemented Emby-compatible backend for
Linux, using Go and PostgreSQL exclusively. React/MUI provides a Material Design
administrator dashboard; an end-user web playback client is outside scope.
Use Chinese for user-facing communication and English for code and documentation.
Work directly on `main` and commit/push accepted increments. Toolchain pins are
Go 1.27.1, FFmpeg 9.0.1 and PostgreSQL 17.11.

Before M5j, the accepted deployment is M5i `58f98a6`, schema 22/probe 6,
PID 3668655, UID 995, start ticks `26912384`; original Emby PID 3131777.
These recorded identities must be rechecked before acting on a service.
[Progress](progress.md) and the linked per-increment reports establish:

- M0: local API inventory, 2,462 Emby 4.9.5.0 reference records and 240 preserved
  media sources. Research captures are not product/client acceptance.
- M1/M2: identity/setup, non-root service, safe ingestion, catalog/ACL browsing,
  local NFO, entities and indexed local artwork within their documented scopes.
- M3/M4: original playback/state, sessions/NextUp, external SRT/WebVTT, selected
  events/control, HLS and progressive conversion, profiles, restart and refresh.
- M5a–M5i: managed users/metadata/sessions/keys/devices, library task scheduling,
  settings, bounded configuration compatibility, and transactional activity/logs.
  M5i passed 1,380 race tests plus its separate browser, restart and deployment gates.

## M5j final acceptance record

The following results belong to the final source and accepted deployment.
Historical and narrower test counts are not added to the full-suite count.

| Gate | Final result and durable evidence |
| --- | --- |
| Final source30 regression and executable | Passed: 1605 race tests / 24 packages; Linux amd64, CGO disabled — [full suite](m5j-final-full-race.json), [build](m5j-final-build.json) |
| Complete browser/process/offline CLI, transitions/restarts and cleanup | Passed: three browser journeys, six generation checks and three restarts — [runtime acceptance](m5j-runtime-acceptance.json) |
| Joined source, executable, assets and acceptance inputs | Passed: 576 installable source inputs, 40 production web inputs and 57 assets — [final source gate](m5j-final-source-gate.json) |
| Protected deployment and native create/download workflow | Passed; 29-table restore rehearsal before upgrade, then a retained encrypted backup and verified logout — [deployment](m5j-deployment-evidence.json), [deployed workflow](m5j-deployed-backup-workflow.json) |
| Final binary/asset identity, schema/probe and PID/start ticks | Schema 23/probe 6; PID 3750313, UID 995, ticks `28911319`; binary SHA-256 `a4abba7b289ceb74ca0916484d714dc6baa1b3c5230b06964b89d363a64e8b81`; 57 current assets |
| Publication and resource cleanup | Published on `origin/main`; owned verification databases/roles/cgroups removed and HBA restored. [Final source reconciliation](m5j-final-git-reconciliation.json) and [service/backup state](m5j-final-state.json) preserve the final checks. See Git history for this closeout commit. |

M5j product code is implemented: encrypted archives, durable operations, native
HTTP/UI, inactive-slot ownership/reuse, generation activation/rollback and offline
CLI. A real HTTP cleanup race was found: normal-EOF deadline cleanup cancelled a
keep-alive request. The fix and focused regression are recorded in
[HTTP body lifetime evidence](m5j-http-body-lifetime.json). The new whole source30
suite and complete real-runtime journey passed. Source25 and
command26 results are now historical scoped evidence, not final-source acceptance.
UI a6 remains the same 27 mocked-API browser cases and 57 production assets.
The first deployment installed the candidate but could not finalize its report:
systemd emitted repeated `EnvironmentFiles` lines and the original parser kept
only the last one. [That failed attempt](m5j-deployment-failed-attempt-1.json)
is retained. [54 deployment guards](m5j-deployment-remediation-tests.json) passed
before read-only forward finalization. That finalization rechecked all installed
artifacts, original data, master/media/logs, recovery ownership and process
identity; it did not restart or restore the service. The main workflow then
passed in 2.373 seconds, preserving all old rows and retaining seven audit records
and one new revoked administrator session. Its downloaded archive was checked
by size, SHA-256 and age header; that main-service archive was not restored.

## Native operator workflow and locations

The [backup/recovery guide](backup-recovery.md) and [native API](../api/backups.md)
are the operational contract. Emby BackupRestore/plugin routes and its archive
format remain unsupported. The archive includes the database, five allowed
logical defaults and the matching application-key master; media and deployment
credentials are excluded. Preserve original media mounts and the database/master pair.

The accepted M5j deployment uses these paths. Recheck current ownership and
generation before an operator action; these paths are not scratch space.

| Purpose | Linux path |
| --- | --- |
| Lifecycle/generations (`GOBY_RECOVERY_STATE_DIR`) | `/var/lib/goby-test/recovery-m5j` |
| Encrypted store (`GOBY_BACKUP_DIR`) | `/var/lib/goby-test/backups-m5j` |
| Operation journal (`GOBY_RECOVERY_OPERATIONS_DIR`) | `/var/lib/goby-test/recovery-operations-m5j` |
| Private recovery database environment | `/opt/goby-test/recovery-m5j.env` |
| Inactive recovery database / role | `goby_recovery_m5j` on the primary PostgreSQL cluster at port 5432 |
| Main runtime / existing administrator credentials | `/opt/goby-test/runtime.env` / `/opt/goby-test/browser.env` |
| Private verification evidence | `/opt/goby-test/exec-work-m5j/` |
| Operational pre-upgrade backup | `/opt/goby-test/backups/m5j-20260910/` |

`verify-backup-recovery-deployed.py` checks the accepted candidate/deployment,
logs in with the existing native administrator, creates one backup, waits for its
completed job, verifies HEAD/range/full attachment bytes and SHA, then proves
logout by HTTP 401 and SQL. It does not restore, delete, change settings or touch media.
A successful run retains the encrypted object in the native store and a copy plus
its passphrase outside all three strict stores:

- `/var/lib/goby-test/operator-secrets-m5j/attempt-01/`: UID 995, mode 0700;
  `<RequestId>.passphrase`, `<BackupId>.age`, `request.json` and `backup.json`: mode 0600.
- `backup.json` pairs the accepted file, digest and passphrase filename. Preserve
  `request.json` and the sole passphrase even when admission is uncertain.
- Safe before/after hashes and the report are under
  `/opt/goby-test/exec-work-m5j/deployed-backup-workflow-01/`, root-only mode 0700.
  Later explicitly selected attempts use their own numbered directories.

For offline recovery, stop the service and use the installed binary with the
same deployment UID/environment. Follow the documented import → plan → apply
sequence; obtain IDs/revisions from actual output. Passphrases use a protected
0600 file or stdin, never command arguments or logs. Start the normal service
after accepted CLI output and sign in with a restored account. The CLI's
`--accept-no-rollback` acknowledges its documented offline boundary, not permission
to erase an unrelated database. Do not add arbitrary files inside the strict stores.

`deploy-backup-recovery.py` is a **one-shot M5i-to-M5j deployment operator** pinned
to that baseline. Its recovery mode is for an unpublished interrupted attempt,
not a general rollback tool. After publication, do not rerun it or restore a
historical M5i/M5j operational backup over newer accepted business state.
Future upgrades need newly reviewed ownership, baseline and preservation gates.

## Resume and later-round priorities

Start with this final record, [progress](progress.md), the backup guide and the
tracked reports above. They are the durable authority for a cloned repository;
no ignored checkpoint file or old live-session handle is required. If a final
M5j record conflicts with current state, reconcile the exact evidence before
editing or operating the service. The resumed scope follows these priorities;
each increment still needs its own source, runtime and publication evidence.

| Suggested later priority | Still open |
| --- | --- |
| P1 — M2/M3 / catalog and client acceptance | Source55 verification, publication and schema28 candidate deployment passed; candidate remains PID1458051, now with 76 sessions/65 devices/169 audits at independently sealed snapshot `6dfe6cbb...`; primary stays source32/schema27. Upgrade authority `7b61f5c9...` is historical state evidence. Client v1 and v2 are independently sealed; v2 passed guards/preflight/setup/login but failed target-card discovery with +1 session/+1 device/+2 audits and no metadata write. Bind TOOL03/v3 to the complete v2 terminal and continuation ledger, and verify bounded DOM/API/screenshot diagnostics before a fresh navigation/automatic-refresh run. The [main upgrade plan](main-schema28-upgrade-plan.md) is published, but its four tools are still in implementation without verification or deployment. Broader M2-M6 work remains open. Consumed scopes cannot be replayed; other DTO fields, global projections, events, subtitles and NextUp remain open. |
| P2 — M4 | Nonzero copied-video seeking, efficient audio I/O, more tracks/formats, aggregate isolation and actual GPU decode **and** encode. |
| P3 — remaining M5 / metadata | More task executors, full policies, providers, broader configuration fields/sections and metadata/artwork reconciliation. |
| P4 — M6 | Differential client/reference coverage, Linux distribution/architecture/GPU matrix, large-catalog upgrades, operations and recovery coverage. |

M7 remains deferred. Software success is not GPU verification; matching routes
alone is not third-party playback compatibility. These priorities remain open
after M5j acceptance.

All tests, validation, builds, runtime/media probes and browser checks run through
`ssh test-env`. The resumed task does not authorize local verification. If SSH is
unavailable, report verification blocked. Keep main PostgreSQL `5432` protected;
use owned fixtures on isolated `15432`, serialize HBA and memory-heavy work, and
give every fixture three unique private recovery directories. Never weaken KDF
strength, clear an arbitrary populated slot, or delete retained evidence to pass a gate.
