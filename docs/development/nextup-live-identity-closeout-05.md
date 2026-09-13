# NextUp live identity resolver closeout

Checkpoint: `2026-09-13T00:16:48Z`. The repair and independent diagnostic closure
are complete. The source, reports and handoff are published together in the
Git commit containing this checkpoint on main.

The current round ends after the proxy identity repair, remote verification,
the single metadata diagnostic, and publication to main. This is the user's
requested closeout boundary. The reference matrix, Goby comparison, positive
client gate, primary upgrade, and remaining M2-M6 work are deferred to the next
round. M7 remains deferred. No matrix acceptance is claimed by this repair.

## Repair and verification

The previous matrix06 invocation failed when the same running proxy executable
was reported as `/usr/bin/python3.13 (deleted)`. The operator now records an
explicit `owned-proxy-executable-file-object-v1` policy. It accepts that exact
endpoint display difference only while the bound PID, process metadata,
namespace, listener, and executable device/inode remain exact. Application
identity is not relaxed.

The default CLI path uses `LiveIdentityResolver`. It opens the exact proxy's
`/proc/PID/exe` with `O_PATH | O_CLOEXEC` and compares metadata without reading
executable bytes. Both descriptor and named stat records retain device, inode,
mode, link count, UID, GID, size, mtime and ctime. The deleted display requires a
regular file with zero links. Every callback checks stable raw observations
and kernel metadata before and after durable receipt persistence; the first
successful observation anchors all subsequent callbacks. Other changes,
unstable observations, or persistence failures stop canonical returns.

A successful callback takes three raw and three kernel snapshots. The same
`O_PATH` descriptor stays open only for that callback, including the third
check after persistence, and closes before return. The run anchor stores the
observed values, not a descriptor retained across callbacks or runs. A later
linked-to-deleted transition also fails the anchor check. All nine stat fields
must be strict integers; Boolean values are rejected.

Raw observations remain raw. The separately labeled `canonicalBoundIdentity`
is the unchanged binding passed to the existing transport. The bounded
`identity-index.json` records receipt hashes, decisions, runtime/source context,
and canonical/alias counts; operator terminal and commit records bind it through
`liveIdentity`. A canonical value is never presented as a raw observation.

All verification ran through `ssh test-env`; no local tests or runtime probes
were run. The frozen operator passed 101 guards, with zero failures, errors or
skips, and both compilation checks. The 21 added guards cover exact identity,
the deleted display, metadata/type differences, resampling races, persistence,
transport/cleanup integration and the native metadata-only open flags. The
producer, transport and matrix sources were unchanged.

| Frozen source or report | SHA-256 |
| --- | --- |
| Operator | `ece90b7ff8a0d56a1b08825bd0dd7ec5b4603da432af6410a4af8ebe4dd968ce` |
| Operator guards | `d5f5d1b9558a104b2209e8c69034696491eeeee6ca6a8ced1081b1de76628b62` |
| [101 guards](nextup-global-reference-operator-verification-05.json) | `ce04ecc0cc956c901723dde04216e40403669a2292633e037761a43819d11ed0` |
| [Two compilation checks](nextup-global-reference-operator-compile-05.json) | `ac423c06182fd805271849ba045a5b61458c2d3d5e68806b62fe7870a2ff7184` |
| [Verification summary](nextup-global-reference-operator-summary-05.json) | `a56814b7f12ec96570ca3dcd0269d61701ca2f7d063f1fc82f61cb1b02efd0a8` |

The source is frozen under
`/opt/goby-test/exec-work-m3e/nextup-global-reference-operator-tool-05/revision-01`.
These synthetic guards establish the implementation contract; the actual
diagnostic record below establishes the bounded live observation.

## Actual diagnostic04

The single actual diagnostic ran in
`goby-nextup-global-matrix-identity-diagnostic-04.service`, invocation
`e7da22e551f74efbbae4aff2034292e4`, former PID `1701431`. It completed with
exit status zero, MainPID zero, and an empty cgroup. Admission checked the real
restricted runtime and replayed the 269 preparation requests and authoritative
private outputs. Two calls through the default resolver returned the canonical
binding only after recording the actual deleted display and same regular
device `2049` / inode `397` / link count `0`. Both observations and the index
remain private and are hash-bound by the terminal and commit.

The diagnostic made no business HTTP requests and did not execute a matrix.
Its audit hook was configured to reject network attempts, proxy executable byte
reads, original implementation reads, unexpected subprocesses and writes
outside the output.
All corresponding attempt counters were zero; two metadata-only executable
opens occurred. The write-scope conclusion also depends on the frozen Journal
control flow and the actual restricted unit: audit counters alone do not cover
every directory operation or resolve every `dir_fd` case. No executable-byte
identity or reference database comparison is claimed. The existing proxy was
reused without restart; this statement does not describe its internal hashing
as metadata-only.

Before and after captures preserve all 181 protected roots, including the old
176-root set, and the complete Goby state at 83 sessions, 70 devices and 185
activity entries. Only capture time is excluded from the Goby comparison.
Failed matrix05/matrix06 records remain unchanged.

Paths below use `/opt/goby-test/exec-work-m3e` as `W`. Diagnostic input and
execution roots are `W/reference-nextup-global-matrix-diagnostic-04` and
`W/reference-nextup-global-matrix-diagnostic-04-execution`; output is
`W/reference-nextup-global-matrix-diagnostic-04-output/operator-04`.

| Actual record | SHA-256 |
| --- | --- |
| Harness, diagnostic input `audit.py` | `506612b6551cdfe7ddb9de53eff7d2a140aeebe9dcbee5b9e2fe9b516f043150` |
| Diagnostic `config.json` | `bdc54e6048357bd254f5a5b84365b55ac55b51c17cd1a493c732da98cca164e7` |
| Diagnostic `attestation.json` | `9e3934143f0614e279a3afa7973b52058e8696aa32e1c7d36496c46d88903ea4` |
| Actual unit file | `d2a8155b3456df588c6f7665b989f69be4b8005081dc27585d331425b6ab0345` |
| [Private/export terminal](nextup-live-identity-diagnostic-04-terminal.json) | `5716af32a80fdd624744b502b7b73053d1b59f00c04db80a3e2a0a0bfe419896` |
| [Diagnostic commit](nextup-live-identity-diagnostic-04-commit.json) | `536f9bd7f3a5591052ba8a95f3e4b6134301b5b4fd89d2fa7eb11c04ead7b532` |
| [Independent diagnostic terminal](nextup-live-identity-diagnostic-04-independent-terminal.json) | `540e1d6b544b94e669007d2efdc488335f26d009e3d202eef5b730acfb137ab7` |
| Private identity index | `3fb840562d6c976b578177fb742eb4513cdab7f046ef41698632e40a882b7d18` |
| `preservation-before.json` | `758b5e9f1d23d55d071bb5cecf10ffdbce425080d5db1b272ded8d5752df4ef1` |
| `preservation-after.json` | `8937b0f0c83342cf8a07a4a3ba82e74e7f08bc50311e478f6d25f6d0216a5c18` |
| `goby-before.json` | `45b915e130d6c735da2e3e69baedb1245c16b08b0f422ff7c4923469015fa3ea` |
| `goby-after.json` | `e610f37657261ec2d53b4cec52c5182314fdf87b6f03496d31bea861b1d6f183` |

Independent closure passed once. It verified the actual unit/argv/invocation,
commit, both terminal files, index, complete diagnostic output filename sets,
two raw receipt chains, strict metadata types and stable anchors. A fresh
metadata-only observation matched the receipts, including the actual application,
endpoint and worker namespaces. It compared the complete before/after protected
records and Goby state, including the historical v7 Goby snapshot. It also
reviewed the frozen diagnostic's zero-HTTP control flow and network-denying
audit hook instead of relying only on worker status fields. The closure helper
is retained at the execution root as `independent-closure.py`, SHA-256
`32df2feab69f87254f776103e04b3fa85dfd7ba32fca9cc2d178ea7a006e4d7b`;
its single remote compile passed before execution.

The worker's `independentClosureRequired=true` stays unchanged. The completed
independent closure is a separate record; neither diagnostic success nor that closure
releases a matrix fixture or establishes client acceptance. This actual
diagnostic exercised admission and the default resolver, not a complete live
transport/cleanup lifecycle; those integration paths are covered by the guards
and remain subject to the future actual matrix run.

## Resume boundary

Preserve all consumed preparation, observer, matrix05/matrix06, diagnostic and
verification scopes. Never rerun their business operations, resume their
journals, or rewrite their original outcomes. Read-only evidence replay remains
permitted.
Preparation05 remains consumed and its worker draft remains unchanged. The
bound matrix output
`/opt/goby-test/exec-work-m3e/nextup-global-reference-runs-05/matrix-05`
remains unused; a later round must use fresh operator/attestation/execution
scopes and recheck current identities and preconditions before any matrix run.
The existing proxy must be preserved.

The next round must finish the reference NextUp matrix and independently
reconstruct its complete actual wire evidence, then define the separate Goby
comparison contract. The positive original-client refresh gate is still unmet.
Candidate source55/schema28 remains deployed; primary source32/schema27 remains
unchanged. Product publication authority remains
`16d75c38064008680fa60839c637efee2f12f2ae`.

Remaining M2-M6 work includes metadata/artwork and restart/filesystem recovery,
real client navigation/refresh/events/subscriptions/subtitles, nonzero copied
video seek and audio efficiency, executor/provider/policy/config coverage,
distribution/architecture/large-catalog upgrades and operational recovery.
Actual GPU validation requires a remote environment with usable GPU hardware.

All verification remains remote-only. Invoke remote Python with
`/usr/bin/python3 -I -B`. Do not read original implementation assets or reference
database bytes. Do not use `prepare-client-reference.preconditions`, restart the
existing proxy, or inspect, execute or stage
`scripts/test-env/upgrade-main-schema25.py`. Preserve the two unrelated
untracked source41 disposal scripts.
