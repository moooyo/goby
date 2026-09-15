# Core client current runtime resolution

Status: runtime source checks, the bounded current observation, independent
result review and saved-envelope validation passed on 2026-09-15. The reviewed disk-full recovery can
bridge the unchanged audited candidate to its replacement application process.
The historical runtime epoch, seed binding and admission remain immutable.
No new core-client input is admitted by this document.

The initial metadata task read saved recovery receipts and selected current systemd, process,
socket and file metadata through `ssh test-env`. It made no SQL, HTTP or browser
request and no service, configuration, seed or admission change. Configuration
files were hashed without decoding; the protected master was stat-only. No
vendor source, vendor database or raw database snapshot was read. No tests or
builds ran in that initial task. Subsequent runtime implementation and its
separate remote source checks are recorded below.

## Historical authority and the actual identity change

Use these path prefixes only to shorten the tables:

- `R`: `/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14`
- `D`: `/opt/goby-test/candidate-disk-full-recovery-20260914`
- `A`: `/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b`
- `B`: `/opt/goby-audited-candidate-20260914T083143Z-9b73ad46f2e6`
- `H`: `/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-reconcile-01`

| Record | Exact SHA256 | Role |
| --- | --- | --- |
| `R/candidate-tv-parent-transition-01/private/runtime-epoch.json` | `76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac` | Immutable A product/configuration epoch, version 3; 27,074 bytes |
| `R/candidate-tv-parent-transition-01/private/seed-runtime-binding.json` | `94bd35e5523a56c60a9b712684d02785b05d6924820bb25f60c48ec8d3496c43` | Immutable actors, catalog and seed lineage; 21,725 bytes |
| `R/candidate-live-admission-05/private/report.json` | `b73a2d30926c68886bd1674a356e6330eab2072afb53fb1fa1f695c5337f8535` | Completed affected-TV-parent admission on the historical process |
| `R/candidate-live-admission05-closeout.json` | `86b224298601ff922d6fe136c026b87628282c675b925dae4831ce78068f139b` | Historical admission closeout |
| `R/candidate-successor-tool-verification-01/audited-candidate-runtime.py` | `bbb89c798e92b2821b7fe450b11783abeb0fe4526e27bcdbe420bf43e558d922` | Historical epoch helper; do not replace its pin with a new resolver |
| `H/hosting.json` | `2100142b83941e24503838fdf92942aef862bc785bbcfcbe8f93223dd30c53c0` | Original client host identity; 5,135 bytes |
| `R/client-host-startup-01/private/report.json` | `3b951bbee3f124321ca6f8f05b90b3a3243969aa27fff6bc348d157b044b6a8d` | Completed historical public hosting initialization |

The epoch and binding above were reread and their exact pins checked. Admission
and startup pins are retained authorities from the existing controller and
published closeouts; this task did not replay their actors. The product remains
A binary `b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42`,
from source manifest
`/opt/goby-test/audit-fixes-20260913-20260913T141732Z-a393812c3356/source-manifest.json`
with SHA256 `bce4d22a4c51dacca4660a6c8e8e3fac816141cd612a7b32b87367799e495cff`.
The source archive SHA256 is
`b363afdcf707471c3a95288d04441bb7be89699010b09ca89c4c783e10436177`.
The future E11 transition is a separate decision; B is also a protected,
different artifact, not an alternate identity for A.

The [disk-full recovery checkpoint](candidate-disk-full-recovery.json) records
two application start requests, zero application stops, zero PostgreSQL
restarts, no login or business HTTP, four successful health/readiness requests,
eight post-restart read-only SQL sessions and independent resource closure.
The source and retained recovery databases of both candidates kept all logical
table rows and sequences. This is not a physical integrity, xmin or durability
proof and does not erase the original disk-full exits.

| Identity | Historical failed application | Reviewed replacement application |
| --- | --- | --- |
| A PID / start ticks | `486706` / `3260806` | `884062` / `11731536` |
| A invocation | `9f936a88b545499a99d024437e0b57c5` | `e40d4098e85c4a3b9e0a315bba0a9193` |
| A HTTP listener | port `28498`, inode `692538` | port `28498`, inode `1546402` |
| A lease backend / client socket | PID `486714`, port `39346`, inode `692533` | PID `884070`, port `43602`, inode `1541410` |
| B PID / invocation | `749711` / `1207d4e0a2a14545b504932db0e6d6c4` | `884123` / `57e60d938686483fb2cdcd7d9f420047` |
| B replacement start / HTTP listener | Historical values are not current authority | ticks `11731606`, port `28698`, inode `1530698` |
| B replacement lease | Historical values are not current authority | PID `884131`, port `46100`, inode `1545101` |

A's unit prefix is `goby-audited-20260913T073217Z-ef77f9ffcf0b`; B's is
`goby-audited-20260914T083143Z-9b73ad46f2e6`. Each has `-server.service` and
`-postgres.service`. A PostgreSQL remains PID `363520`, ticks `447753`,
invocation `450637ff9d32447eb68ba2f8eeee2fd7`, port `25498`, cluster system ID
`7684919710846761936`. B PostgreSQL remains PID `749626`, ticks `9460307`,
invocation `1a318afbe70f4e5893ecfc3e94febab3`, port `25698`, cluster system ID
`7685306796939927562`. The cluster IDs are saved reviewed database facts; no
fresh SQL or control-data read was performed here.

## Exact recovery receipts to consume

The following files were reread and hashed during this task. The result and
review bind the private query receipts, closures and snapshots; raw snapshots
stay remote and were not read for this document.

| Path beneath `D` | SHA256 | Bytes |
| --- | --- | ---: |
| `restart-execution.json` | `3dfb3639001f63a1b7545022e648480db7f777f8d81222e26d49dd8b340c2f8f` | 87954 |
| `restart-independent-review.json` | `8c97fe729fbde2694ba1a9354f6b276bfa8f675c87575a05fcb2b18213a954dc` | 8589 |
| `restart-configuration-observation.json` | `40df2510717645c3fb1c6ea683b3bad55ec9a7096a113d0827af067174d47335` | 2696 |
| `restart-preflight.json` | `45ba778204812f0ba389745b9c11ac668ea1aa2803406dc2f4ed95a400ea076f` | 24903 |
| `readonly-execution.json` | `591da28976ec05c7898a7aafd50e6d754f8fdc75d611a12d11cc5e11013cb25f` | 29303 |
| `native-execution.json` | `d2721d724669395eb6be4c981f222700b5f43e8a9086153d95c89e01e08d2f3d` | 29548 |
| `native-01/result.json` | `ac90853f59701a22b47722b5b9846227d4b55bbaab4be3b9ea617fe9f483a0cc` | 4186 |
| `diagnostics-restart-review.json` | `20ddfe1296eb01809619bce6e93020b4a07b1dc38bedf2c2c6244ae38e7de0ea` | 5831 |
| `restart-01/old/start-intent.json` | `406eed6d4e05c1bbade24cbd4b5a1712c2299d78381da660cc25180ca9d8a7b6` | 1995 |
| `restart-01/old/result.json` | `a024667a2267c5cb9a93ca906f91456665f50a2d43241b34ececb825cbc6cb55` | 14865 |
| `restart-01/fresh/start-intent.json` | `308ebb0df48ccd6d721bf0a4aede5afc489a3458fe622a2d208db0270627261f` | 1998 |
| `restart-01/fresh/result.json` | `1bc6ec338510cd3b213f6c815963a5b952f4aebb1c100479856d697e8a4f203b` | 14947 |

The recovery mappings remain useful indexes, not replacement live authority:
`D/private/old-candidate-recovery-bindings.json` is 19,784 bytes, SHA256
`4c7f1b3fe3516f962247c075eff40e98fab6f96f06da893136281cebf0bc8554`;
`D/private/fresh-candidate-recovery-bindings.json` is 16,192 bytes, SHA256
`369d09925e65147f6c5b973f8e7759800ab9937206a7a6a35fb0bbe65717cb11`.
Both were reread without following their raw database or configuration inputs.

A's reviewed source snapshot after restart is
`D/restart-01/old/private/source-after-restart.json`, SHA256
`5dfb5e7549448a17d3036b0043c8d612964ba79a84f9ac8fe0304896d7d3c959`;
its retained recovery snapshot is
`D/restart-01/old/private/recovery-after-restart.json`, SHA256
`d3adea716028f98fd1118eeccb8f7b3f098d22d33a4051a638bcd10e57331ee0`.
These pins were read from the reviewed result, not independently rehashed here.
They bridge the recovery event; they do not replace the newest full closed-state
baseline required by the separate TV actor contract.

## Current metadata observation and its limits

The remote metadata read began at `2026-09-15T08:33:46.105193+00:00`.
Before/after application and hosting process/unit samples were equal. A and B
still had the replacement identities in the table, UID 995, the original
executable inode/device, original one-argument application command line and
network namespace `net:[4026531840]`. Both PostgreSQL units and the protected
system PostgreSQL PID `893` were running. Main/control units
`goby-foundation-test.service` and `goby-client-m3e.service` were inactive.
The shared boot ID remained `4de83999-7586-4716-83d1-0d81c9343126`.

Fresh binary, environment and unit hashes were:

| File | SHA256 |
| --- | --- |
| `A/install/goby` | `b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42` |
| `A/private/runtime.env` | `877ce946814fef63a240171b7a60c4ad6b4505bf2051be815266ed9744627d3d` |
| A server unit under `/run/systemd/system` | `f1cc1e40c9a3219c17ebc6a545720d52c93917eff361d07a638331d45b05c2bb` |
| A PostgreSQL unit under `/run/systemd/system` | `144ab164cfc336d6024bd63b2faa85b4e7e7251e93fe0f4c03e1b2d8bb385333` |
| `B/install/goby` | `59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312` |
| `B/private/runtime.env` | `51d185d92a5adc8ebb6cf3943958be876ef7909e0ec0576cdc35d4383e1f32e3` |
| B server unit under `/run/systemd/system` | `e1265233a4706ced8dd2a21aed360d490c813ade19034cda622b48b049eefc27` |
| B PostgreSQL unit under `/run/systemd/system` | `3ee71db6814588978eb5ae562a342a4e8e9de4c879fefbcf615d55c3b2323714` |

The protected master at
`/var/lib/goby-test/application-key-vault/master.key` was a nonsymlink regular
32-byte file, UID 995, mode 0600, device 2049, inode 400975. No master bytes or
master hash were read. This does not establish every candidate's default master
path; retain the recovery checkpoint's original limit.

The recorded lease backend PIDs were still present under the matching
PostgreSQL cgroups: A ticks `11731542`, B ticks `11731612`. Their app-owned
established sockets still had the recorded client ports and inodes. Other pool
connections were also present and are not additional proven deployment leases.
OS socket/process continuity does not establish the current unique advisory
lock or its grant. The saved recovery result lacks the `backendStart` field
required by `EpochReader.deployment_lease`; do not invent it or copy the old
epoch's value. A future bounded entry must perform that existing read-only
lease query and socket binding before it freezes the current lease.

The hosting process still matched `H/hosting.json`: PID `366598`, ticks
`506485`, invocation `c883bdf04e2f4d7ea6526fcd858fc342`, UID 0, executable
device/inode `2049/2692744`, namespace `net:[4026532544]`, and exact saved argv.
Its executable SHA256 remained
`c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2`.
The original listener inode `330881` was held by fd 276 and appeared as a
listening wildcard TCP6 socket on port `28497`. An IPv4-only `/proc/net/tcp`
listing would miss it. This task did not repeat the historical IPv4 reachability
or web response observation.

All ten `HOST_UNIT_FIELDS` used by the existing client contract still matched.
Of the 28 saved hosting unit properties, only systemd's rendered `ExecStart`
runtime fields differed: its printed start time became `[n/a]` and printed PID
became 0, while `MainPID`, `ExecMainPID`, start ticks and invocation stayed exact.
Retain this observed difference without inferring a restart or its cause.
`ExecStart` is not in `HOST_UNIT_FIELDS`; no weakened process check is required.
The current hosting unit file
`/etc/systemd/system/goby-core-av-original-client-01.service` hashed to
`ea816341f994018afea6efccc0cf086c36d5f96730a4040572dbbb3b8320451a`.
This last digest is a current observation, not a newly reconstructed historical
unit-file proof.

These interactive readbacks are recorded in this document, not a standalone
admitted private input. Refresh and retain the necessary metadata in the
existing bounded entry preparation before dispatch; do not reuse this timestamp
as an execution reservation or treat it as a new health window.

## Minimal current runtime contract

Keep the existing version-3 product epoch and seed binding byte-for-byte.
Add one optional, strictly validated `currentRuntime` descriptor to the new
version-5 TV client input and the existing reader/closeout path. It represents an already
reviewed unchanged-binary recovery, not another transition or actor execution.
Old inputs without it retain their exact historical behavior.

The new private record must bind:

1. The exact historical epoch, seed binding, admission and admission-closeout
   pins above, plus the separately reviewed TV retained-state input. Product
   source, schema, actor/catalog identities and configuration lineage stay in
   those existing records.
2. The recovery execution, independent review, configuration observation and
   A-specific start-intent/result pins. Require their successful reviewed
   unchanged-binary recovery and completed reader/lock closure. Cross-bind the
   selected candidate/root, original failed invocation, replacement invocation,
   unchanged binary/configuration and preservation result. A filename or a
   boolean `recovered=true` alone is not authority.
3. Explicit fresh candidate process metadata, selected server unit properties,
   server identity, listener and lease; continuous PostgreSQL metadata and unit
   identity; the existing hosting descriptor and current hosting observations.
   Use an exact field schema, not unrestricted dictionary overrides.
4. Private entry evidence pins and its observation interval. The same descriptor
   must flow through controller input, emitted boundary, server-log receipt and
   offline closeout input/result. A new PID must never appear to be a member of
   the historical epoch merely because the boundary retains its old epoch pin.
5. The component receipt must bind the new execution `runtimeHelper` source and
   this exact `currentRuntime` descriptor, separately from the immutable
   historical `epoch.runtimeHelper`. Do not add a second resolver field hierarchy.
   Component guards do not establish the current runtime, actor baseline or a
   successful client journey.

The proposed envelope has exactly these fields; no ready envelope exists yet:

| Field | Required content |
| --- | --- |
| `kind`, `version`, `status` | `audited-candidate-current-runtime-binding`, integer `1`, `reviewed_current_runtime` |
| `runtimeEpoch`, `seedBinding`, `admission`, `admissionCloseout`, `hosting` | The five exact historical descriptors used by the corresponding new client input |
| `recovery` | Exactly `execution`, `independentReview`, `configuration`, `selectedStartIntent`, `selectedResult`, each a descriptor to the pinned recovery records above |
| `current` | Exactly `candidateProcess`, `serverProperties`, `serverIdentity`, `listener`, `postgresProcess`, `postgresProperties`, `lease`; their field sets match the existing gateway/provision/lease contracts |
| `preserved` | Exactly `binary`, `runtime`, `units`; require equality with the historical candidate descriptors and fresh hash/metadata evidence |
| `observation` | One descriptor to the separately retained bounded entry readback establishing these current facts and its observation interval |
| `observationReview` | An independent saved-only review of `observation`, its actual source and its source-verification receipt; it does not refer to this envelope or the future component receipt |

The selected A lease needs its actual fresh `backendStart`, not an inferred
timestamp. Source/contract checks and a separately admitted bounded observation
precede an independent observation review and the current-runtime envelope.
The component receipt can then bind the envelope and final execution helper,
followed by the final root client input. This order has no component/envelope
cycle. The observation record must make
zero start/restart, seed, provision and admission actor calls. Its necessary
read-only SQL is a separate reviewed entry action, not something performed by
the present metadata-only document task.

The implemented observation schema is
`kind`, `version`, `runtimeEpoch`, `recoveryExecution`, `hosting`, `source`,
`verification`, `capturedAt`, `before`, `after`, `preserved`, `hostingBefore`,
`hostingAfter`, `leaseQueryResult`, `calls`. Its kind is
`audited-candidate-current-runtime-observation`, version 1. Before and after
contain the exact current identity structure; both hosting views contain
`process`, `listener` and the selected `HOST_UNIT_FIELDS` as `unit`.
`leaseQueryResult` pins the actual one-row JSON result, including `backendStart`
but excluding derived `candidateConnection`. Calls contain exactly `sql=2` and
`http=browser=service=seed=admission=provision=0`.

The independent observation-review schema is `kind`, `version`, `status`,
`observation`, `source`, `verification`, `checks`, `calls`. Its kind is
`audited-candidate-current-runtime-observation-review`, version 1, status
`passed`. All checks must be true: `recordPins`, `sourceAndVerification`,
`currentIdentity`, `recoveryBinding`, `preservedHashes`, `hostingContinuity`,
`uniqueLease`, `privateEvidence`. Every review call counter is zero. The source
and verification pins must equal the observation's pins; the loader hashes the
actual Python observer source and reads its pinned verification receipt. The
old recovery's independent review cannot substitute for this new review.

For the separately source/input-admitted observation producer only,
`RecoveredEpochReader.for_observation(product_epoch, modules, private,
current_facts, preserved)` accepts the reviewed recovery identity facts with
`lease.backendStart` explicitly absent. It cannot create a ready envelope and
allows one call to the existing lease method, even if that call fails. Its
unchanged parent path performs one cluster-identity SELECT and one lease SELECT,
so the reviewed budget is two read-only SQL calls. The actual
backend start appears only in the returned lease; all other recorded lease
facts and socket ownership must match. The final ClientRun must use
`load_current_runtime` on the complete envelope and must not use this producer
factory. No observer, browser or source verification was executed while writing
this implementation and its test source.

Allow only the reviewed application restart identity delta: application PID,
start ticks, invocation and their derived server metadata, listener inode and
lease connection identity. Require unchanged candidate root, binary/source,
runtime/configuration and unit digests, UID, argv, cgroup, namespace, boot,
database/role/cluster identity and PostgreSQL postmaster. No substitution of B,
E11, a new candidate, a new source build or a different configuration fits this
contract. Unexpected drift stops this entry for a separate concrete decision.

The input's `runtimeEpoch` continues to mean historical product/configuration
lineage. Do not mutate that object or wrap a changed copy that later passes the
old epoch validator. Resolve a separate current identity view and use it only
where the live workflow expects current identities.

## Reuse and required changes in the existing implementation

Line numbers below describe the source inspected for this decision; select the
final sources by their explicit verification pins after concurrent work lands.

| Existing branch | Reuse or minimum change |
| --- | --- |
| `audited-candidate-runtime.py:430`, `:462`, `:485`, `:554`, `:586` | Reuse epoch v3, seed and product/configuration lineage validation unchanged. No new binary-successor epoch and no transition replay are needed. |
| `audited-candidate-runtime.py:666`, `:732` (`EpochReader`, `EpochIO`) | Keep the old reader for historical inputs. Select a narrow `RecoveredEpochReader` in this same module for v5, holding `productEpoch` and a separate current identity view after historical lineage validation. Reuse provision metadata and bounded read methods without invoking the provision entry point. `pin()` at `:678` and `deployment_lease()` at `:712` must use the resolved current application identity. |
| `run-audited-candidate-client.py:180` (`admitted`) | Reuse the version-3 affected-TV-parent branch and its ADMISSION04 reuse contract. They validate saved admission evidence; they do not require another health, backup, seed or admission actor. |
| `run-audited-candidate-client.py:219`, `:802` | Reuse historical hosting initialization through the validated successor lineage and existing `verify_hosting`. The host has no replacement identity requiring a recovery branch. Keep historical startup pins and public server identity checks. |
| `run-audited-candidate-client.py:104`, `:226`, `:761` | Admit the new record and separate historical helper authority from the new execution helper/component source pin. The current `runtimeHelper == epoch.runtimeHelper` equality cannot attest upgraded execution code; do not satisfy it by rewriting old records. The v5 bootstrap must validate the input, component receipt and new source digest through the fixed trusted bootstrap before executing the new helper; a check after import is too late. |
| `run-audited-candidate-client.py:810`, `:824`, `:841`, `:873` | Pass the same current identity view into EpochIO, current PG/lease samples and server-log process checks. Keep before/after equality and full source-state checks. |
| `run-audited-candidate-client.py:910`, `:964`, `:1030` | Use resolved current boot/process authority for owned worker checks. Gateway upstream construction already uses the captured `candidate_before`; preserve that path and the existing network policy. |
| `run-audited-candidate-client.py:1079` | Carry `currentRuntime` explicitly through the boundary, closeout and server-log authority. Historical epoch alone is insufficient to explain replacement identities. |
| `close-audited-candidate-client.mjs:440`, `:1296`, `:1310`, `:1314` | Consume the same current record for current log, manifest, gateway, PG and lease comparisons. Otherwise successful live work would still fail against the old epoch PID. Preserve the old historical fixture and retained-state lineage branches, including their original source epoch. |

`RecoveredEpochReader.__init__` must not call `super().__init__` as currently
implemented. The old constructor calls `verify_environment_epoch_files` at
`:670`; its version-3 branch recurses to version 2 at `:650-657`, reads both
environment files and calls `validate_environment_append` at `:659-663`, decoding
their contents. For the new path, reuse the already verified historical
configuration proof and require only exact current file hashes and metadata.
Initialize the existing metadata/SQL reader adapter explicitly without that
decoding path. Do not alter old historical inputs or call any transition/revision
actor to refresh metadata. The new reader must not broaden master/config access.

## Fresh facts required before the one TV observation

Before a browser or gateway starts, the existing bounded entry must retain and
independently review these current facts:

- Exact A application and PostgreSQL unit/process identities, executable and
  configuration/unit digests, owned listener, unchanged host identity and socket,
  and protected B/main/control/system PostgreSQL state. Read masters by metadata
  only and configuration by digest only. Compare properties by their defined
  schema, not complete rendered `ExecStart` strings or access times.
- One unique current granted deployment lease from the existing read-only query,
  including backend start and exact app socket ownership. This remains unqueried
  by the present task. A reconnect or different lease is not accepted merely
  because the application is still running.
- The newest full closed-state baseline and the TV actor's reviewed retained
  history, revoked credentials, foreign rows and explicit Prepared-row policy.
  The existing source capture must compare that baseline before any UI action;
  recovery-era row counts are insufficient. This task made no new data claim.
- The new output/worker absence, active deployment-lock availability, and current
  disk/RAM/cgroup/request/cleanup limits under the original TV budget. This task
  did not acquire the lock or reserve resources.
- The final verified controller, runtime resolver, closer and component receipt
  pins together with the reviewed observation decision. Keep these distinct
  from the historical helpers and from the new current-runtime record.

Only after these prerequisites pass may the single
[TV Response observation](core-tv-response-observation-decision.md) use the
replacement A runtime. Close its owned workers and state through the existing
contract. Recovery reuse does not pass core acceptance, E11 admission or main
promotion, and does not authorize another application restart to refresh a PID.

## Remote source checks before current observation

The final runtime and test sources were checked under
`R/core-current-runtime-20260915T085726Z/private/checks-02/`.
`verification.json`, SHA256
`ec6310baee45980df5701708864723c39d6f6d322bb6506fa6f7a621817c121e`,
records 18 passing tests and zero failures. The runtime source SHA256 is
`4004c9fb2c727632b331d4eedc569ee236123659e9fafeda42a5a4c1f9038329`;
the test source SHA256 is
`6deb5d17780bab4597f2ffb67858c51aa6c2adb57c2f6620e1ba4b326c526254`.
Both source files compiled remotely. One
`unshare --net -- python3 -I -B test-audited-candidate-runtime.py -v` process
exited zero and was reaped. Its stdout/stderr and dispatcher are pinned in the
receipt. There were no live SQL, HTTP, service or browser calls.

These checks combine the seven retained runtime guards with eleven new recovery
checks. The new observations in the fixtures are explicitly synthetic, even
though their recovery inputs are the pinned real saved records. They do not
constitute current runtime admission or add to product regression counts.

The earlier retained receipt
`R/core-current-runtime-20260915T085726Z/private/verification.json`, SHA256
`744e0467e32a46933bdb03e1022830a6a1626399d1ee0308799085e2f3a2058c`,
passed 17 tests. Its lease test substituted `sql_json` and therefore did not
prove the parent method's actual query count. Static inspection then identified
the existing cluster-identity SELECT before the lease SELECT. The reviewed
observation budget was corrected to two read-only SQL calls without replacing
the existing SQL path. The final test exercises the actual retained
`Provision.cluster` and current `EpochReader.sql_json` methods, substituting
only the psql execution leaf. It proves two calls on success, one call when
cluster identity is rejected, no later lease query in that rejection case, and
no automatic retry of the observation method.

The prospective 152-line observer source is
`R/core-current-runtime-20260915T085726Z/private/observe-current-runtime.py`,
SHA256 `1f3a3fa26b74321d63b8b998545d3a0f74cfc42191fdd576a10469416169212d`.
Its compile-only `observer-source-verification.json` has SHA256
`467bca27d53275ab0899ff21b4a6456e3a4b28e0cfd4875968784a7621c1df15`.
The exact `observer-input.json` in that directory has SHA256
`7a91b923a0087181a8ae8f88f54fe2193df7f3fce47d09acf31b0fe33acc2720`.
It allows one lease-method invocation through the original two-query path,
180 seconds total, and the absolute preparation deadline
`2026-09-15T09:28:00+00:00`. These source/input records were frozen before the
independent static review and the single execution described below.

## Observed current runtime and completed binding

The independently reviewed source/input ran exactly once. Its observation was
captured at `2026-09-15T09:11:00.404853+00:00`; the process exited zero, was reaped
and was absent at dispatch closure. Stderr was empty. No browser, HTTP, service,
seed, provision or admission actor ran. One `deployment_lease()` invocation used
the unchanged cluster-identity SELECT followed by the lease SELECT. Both psql
commands exited zero with acknowledged outcomes and closed process groups.

A remained PID `884062`, start ticks `11731536`, with its recorded nonroot
process, invocation and listener. The lease was one granted `ExclusiveLock` for
`goby_candidate_ef77f9ffcf0b`, backend PID `884070`, actual backend start
`2026-09-14T14:54:01.050097+00:00`, client port `43602`, and app-owned socket inode
`1541410`. Hosting remained PID `366598`. Before/after current metadata,
hosting, protected unit/process state and selected file hash/stat evidence
matched. The unchanged lease row is attached to both surrounding metadata
samples; this does not claim a second lease query or uninterrupted lock sampling.

All paths in the following table are beneath
`R/core-current-runtime-20260915T085726Z/private/`:

| Record | SHA256 |
| --- | --- |
| `observer-execution.json` | `c5685a8137f7df36879b52852182cf19e68afc2b37a4cc5692cd2a2eb514bf45` |
| `observation-01/observation.json` | `09a2c45163b7ff142cc4ed0355da99b973874311276d6f2cba8649e22992fa9c` |
| `observation-01/metadata-and-closure.json` | `43879ad9656f84a667d2f66f68bf005cae517b44a761290d063cf892c61a6c40` |
| `observation-01/lease-query-result.json` | `b677de8c5fa3bff0b62f10941e33726246e7ca63b20e0fe5f45e86d4d923cde0` |
| `observation-review.json` | `2feccecce8e4fddf090076c447eefdc20b8f2ce5b93dbd7b54334418e4182814` |
| `current-runtime-binding.json` | `38b906d090cf1ae6cf1e6679d68e92772e60e4e5f396ef085b232983a35de60c` |
| `saved-envelope-verification.json` | `21ff9ec9b1e04fb13c482c85505253c53286fcdb377808631ef16d2daff824f4` |

The root reviewer independently checked the actual source/input/test pins,
retained recovery authority, current process/lease and hosting identities,
protected file hashes/stat evidence, both original psql intent/results and empty
stderr, zero exits and absent process identities. The lease JSON is byte-equal
to `002-epoch-read-only.stdout` and contains one row. The observer was closed,
and the existing deployment lock was reacquired nonblocking and released during
that review. The reviewer made no new SQL or HTTP request and wrote the separate
observation-review receipt above.

Only after that review was the current-runtime envelope created. Its exact bytes
passed `load_current_runtime` using the r02 source SHA256 `4004c9fb...38329` and
the actual saved records. This saved-envelope validation made no live runtime,
SQL, HTTP, service or browser call and decoded no environment file. It is the
binding to consume alongside the separate final component receipt and reviewed
TV retained-state input. The eventual client controller must still pin current
identities at entry; this checkpoint does not itself admit or pass a TV journey,
an E11 transition or main promotion.
