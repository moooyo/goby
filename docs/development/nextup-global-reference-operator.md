# Independently attested reference matrix operator

Status: **57 remote fake guards and two compile checks passed after media
membership and writable-parent review fixes; no live matrix executed**.

[run-nextup-global-reference.py](../../scripts/test-env/run-nextup-global-reference.py)
is the outer entry for the unchanged `TransportRunner`. It does not prepare a
fixture, publish an attestation, modify a producer draft, create a proxy, enter
a namespace, restart a service, support Goby, or resume a failed run. Its only
live HTTP path is the frozen reference matrix after complete admission.

The independent root task must first review successful real preparation and
write a new owner-only attestation outside the preparation output. A failed
preparation, an administrator-only cleanup, or a boolean-edited execution file
cannot satisfy this entry. Root also creates the bounded systemd unit; this
operator only reads its properties and current process identity.

## Entry and attestation

Run the reviewed source inside the existing unit named by the attestation:

```text
/usr/bin/python3 -I -B /absolute/owned/run-nextup-global-reference.py --attestation /absolute/private/matrix-attestation.json --attestation-sha256 EXACT_FILE_SHA256
```

The attestation file, all referenced private JSON, and their ancestors must be
root-owned with no symlink traversal. Private files and private input roots
must be owner-only. The independently supplied SHA-256 is the hash of actual
attestation file bytes, including its final newline if present.

The exact top-level attestation keys are:

```text
schemaVersion, kind, runId, attestedAt, execution, sources, preparation,
shutdown, runtime, scope, sealedRoots, forbiddenOriginalRoots
```

`schemaVersion` is integer 1; `kind` is
`nextup-global-reference-matrix-attestation`. `runId` matches the real producer
and matrix. `attestedAt` is the independent observation time with a timezone.
It follows every preparation response and the shutdown observation.

`scope` contains exactly:

- `attestationRoot`: an existing private directory outside preparation output.
- `sourceRoot`: an existing protected root containing the explicitly pinned
  operator, producer, transport, and matrix Python sources.
- `operatorEvidenceRoot`: a new unused root for this operator's admission,
  terminal, and completion commit; distinct from the matrix evidence root.

`sealedRoots` must cover every root excluded by the actual producer manifest.
New operator and matrix outputs cannot equal, contain, or be inside a sealed,
preparation, attestation, or original implementation/data root.
`forbiddenOriginalRoots` must exactly match the real producer manifest.

`sources` contains exactly `operator`, `preparation`, `transport`, and `matrix`,
each using `{path, sha256}`. The first source must be the executing operator
file. Producer bytes are pinned by this independent attestation so a separately
reviewed repair with the same output contract can be consumed. The operator
never invents that source approval. Transport and matrix additionally require
their fixed reviewed digests:

```text
transport d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1
matrix    da3ed22ce15a3cf82bce81be31db1a1d03a202c00d44e9ac8ef124a93a2d5259
```

`execution` is `{path, sha256}` for a separately written owner-only JSON under
attestationRoot. It must equal the retained `draft-execution.json` byte meaning
except for `matrix.binding.fixtureReleased: false -> true`. No receipt, ID,
credential, source, target, budget, or other field may change. The eight original
receipt files retain their pending wrapper and immutable bytes; the outer
attestation, not a rewritten wrapper, authorizes this one execution.

## Actual preparation descriptors

`preparation` contains `root`, `inputManifest`, `wireIndex`, and the following
exact output descriptors. All descriptors use `{path, sha256}`.

| Key | Exact path relative to preparation root |
| --- | --- |
| manifest | private/manifest.json |
| plan | private/frozen-plan.json |
| terminal | private/terminal.json |
| state | private/state.json |
| draftExecution | private/draft-execution.json |
| draftMatrix | private/draft-matrix.json |
| mediaTree | private/media-tree.json |
| baselinePreservation | private/baseline-preservation.json |
| finalPreservation | private/final-preservation.json |

`inputManifest` is the actual owner-only file supplied to the producer CLI,
inside its inputRoot. Its JSON must equal the retained private manifest. The
file digest and canonical plan digest also bind the completed unit's ExecStart.
Canonical manifest/plan hashes exclude a trailing newline; file hashes do not.

The producer terminal must be the actual successful
`awaiting_independent_attestation` result. It must retain completed and cleanup
true, matrixInputsUsable false, independentAttestationRequired true, no failure,
no pending HTTP or ownership, no uncertainty, and no cleanup errors. The final
state remains phase cleanup with the three tokens/sessions retained and all
three actors revoked. Successful cleanup is exactly six requests. Counts must
close within the 232-normal/238-success contract.

The entry reuses the frozen producer's static input validation. This reads and
pins its actual release terminal/inventory, closed-token intent/result proofs,
media approval, and approved synthetic media manifest. Missing or changed
nested evidence is rejected, rather than trusting a preservation boolean.
The original implementation or reference database is never read.

The copied media tree must contain exactly six independent MP4 files, eight
NFOs, and two ownership markers. Their bytes, hashes, file identities, and exact
source-bound names/content are checked. The explicitly approved owned synthetic
source is rechecked; it is not an original application executable or asset.

The operator also enumerates the actual media tree, rather than only opening
the sixteen receipt-listed files. The only permitted directories are their
nine necessary ancestors, including the media root. A bounded lstat and
O_NOFOLLOW directory-descriptor walk rejects the first extra entry and every
symlink or special node without traversing its target. Missing files or
directories are rejected. Every later checkpoint repeats exact membership and
compares the admitted directory identities, so a same-name directory replacement
or an added-then-removed entry cannot silently preserve admission.

## Complete raw wire index and replay

The independent root writes `wireIndex` under attestationRoot after preparation
has stopped. It must contain:

```json
{
  "schemaVersion": 1,
  "kind": "nextup-global-preparation-wire-index",
  "runId": "{actual runId}",
  "producerRoot": "{actual preparation root}",
  "requests": [
    {
      "ordinal": 1,
      "label": "login-admin",
      "intent": {"path": "{root}/private/0001-login-admin-intent.json", "sha256": "{actual file digest}"},
      "reserved": {"path": "{root}/private/0001-login-admin-reserved.json", "sha256": "{actual file digest}"},
      "response": {"path": "{root}/private/0001-login-admin-response.json", "sha256": "{actual file digest}"}
    }
  ]
}
```

This illustrates shape only. The actual list covers every request, in contiguous
ordinal order, without missing or extra triples. All labels and file paths must
be the producer's actual names. Intent, reserved pending state, raw response,
payload, token context, and receipt digests must identify the same attempt.
Complete bounded Base64 response bytes and response timestamps are validated.

Admission then runs the exact pinned producer with an in-memory journal, fake
authority callbacks, a bounded synthetic clock, and a transport that can only
return the already indexed raw responses. It performs no HTTP or filesystem
writes. Every generated request must match the actual recorded method, route,
headers, payload, actor, label, and bounds. Every private file generated by this
replay must reproduce the existing producer bytes exactly. Unaccounted private
files and unconsumed wire records are rejected.

This rechecks the producer's actual catalog/policy/own-token/zero-state and
preservation decisions, four calibration lifecycles, and P/Q/admin logout plus
same-token rejection sequence. Summary flags cannot replace these responses.
The replay naturally preserves legitimate output details: tokens and stopped
plays remain in final state, library receipt digests are added during draft
creation, and the one permitted library inventory reconciliation retains its
ownership-pending reserved state.

## Completed producer unit and running matrix unit

`shutdown` contains `observedAt` and one `units` entry:

```text
name, invocationId, properties, cgroupPath
```

Properties must include exactly ActiveState, SubState, MainPID, Result,
ExecMainCode, ExecMainStatus, ControlGroup, RemainAfterExit, and ExecStart.
The completed unit is active/exited or inactive/dead, has MainPID 0, Result
success, exit code 0, and RemainAfterExit yes. Its actual ExecStart argv must
be the documented `/usr/bin/python3 -I -B producer.py prepare` invocation with
the exact original input path/file hash and canonical plan hash. The current
InvocationID and properties are checked; the exact recursive service cgroup
must be empty. No other service or cgroup can substitute for that proof.

`runtime` contains `unitName` and the exact planned static `properties` of the
matrix unit. InvocationID is intentionally not required before the unit starts.
At entry the current PID, ENV INVOCATION_ID, `/proc/self/cgroup`, and systemd
MainPID/InvocationID/ControlGroup must agree. The unit must be actively executing
this operator, and the runtime identity is recorded in the admission receipt.

Required static runtime properties are:

```text
Type=oneshot
RemainAfterExit=yes
Restart=no
User=root (or the systemd empty root default)
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateNetwork=no
TimeoutStartUSec={finite value, at most one hour}
MemoryMax={finite bytes, at most 2 GiB}
TasksMax={finite integer, at most 256}
LimitNOFILE={finite integer, at most 65536}
ReadWritePaths={exact permitted paths}
```

ReadWritePaths contains exactly the operator and matrix evidence parents. If
neither parent covers the existing fixture lock, include that exact lock file
as well: the frozen transport must open it with O_RDWR for flock. Do not grant
the entire fixture root just to open that file. Root owns any additional
read-only sandbox restrictions and chooses sufficient timeout/resource values.
TimeoutStartSec is compatible with a retained oneshot; RuntimeMaxSec is not
required after successful exit.

Both evidence parents themselves must be safe writable scopes. Neither may
equal or contain any original/source, producer, attestation, sealed, input,
credential, or other pinned read-only authority. This rejects `/`, the shared
work root, and an otherwise disjoint output child whose parent also contains
protected evidence. Source paths are checked individually as well as through
their declared lookup scopes. A shared lookup ancestor does not itself become
writable merely because a dedicated output subtree sits below it.

For the planned preparation-03 integration, use a fresh dedicated parent such
as `W/nextup-global-reference-runs-03`, with matrix-03 and operator-03 as its
separate children. Keep the independent attestation and all source, preparation,
credential, input, and sealed evidence outside that writable subtree. The
existing lock remains an explicitly named file exception. The failed
preparation-02 layout does not authorize a writable shared work root.

The outer operator does not pre-acquire the fixture flock. TransportRunner
acquires its own existing lock; a composition of its supported process probe
then rechecks the outer admission under that lock. Every checkpoint rereads
current producer/runtime unit properties, verifies empty producer cgroups,
checks the executing operator identity, and checks pinned file/directory
identities. It then calls the unchanged metadata-only application/proxy probe.

## Execution result and persistence responsibility

Operator evidence is separate from the matrix evidence. `admission.json`
binds the external attestation, published execution, actual producer terminal,
wire index, source descriptors, runtime identity, and replay request count.
Only then is TransportRunner constructed and run once.

The operator compares the returned transport result with the actual retained
matrix state and result file. Success requires closed mode, full token
rejection proofs, no pending/unknown responsibility, and complete evidence.
A closed observation failure remains a failure with cleanup complete. All
other results require recovery; the operator never launches cleanup or retries
on its own after TransportRunner ends.

`matrix_protocol_complete` means the bounded protocol and cleanup finished.
The retained `transportResult.outcome` may still be
`reference_global_positive_unresolved`; a clean protocol ending establishes
neither a positive global rule nor original-client acceptance.

Both operator `terminal.json` files are written first with
`status: awaiting_operator_commit`, `candidateStatus`, and
`completionCommitted: false`. A final exclusive private `commit.json` binds
the actual private/export terminal digests and the runtime identity. Only a
successful commit permits the returned result to set completionCommitted true.
Persistence failure returns recovery and never overwrites a receipt.

Root must additionally confirm the actual matrix unit exit status and empty
cgroup. A private candidate terminal or commit alone is not independent runtime
closure. A post-dispatch exception is never reported as proof of zero HTTP.
CLI output contains only safe status/count/digest fields; detailed private
failures and token responsibilities remain in the protected evidence roots.

## Verification boundary

[The dedicated new guards](../../scripts/test-env/test-run-nextup-global-reference.py)
use only temporary synthetic preparation files,
fake unit/cgroup/process observations, an indexed fake response source, and
fake matrix HTTP. All Python runs use `/usr/bin/python3 -I -B` through
`ssh test-env`; no local verification is allowed.

Initial operator verification used the independent remote scope
`/opt/goby-test/exec-work-m3e/nextup-global-reference-operator-tool-01/verification-01`.
All **43 guards passed**, with zero failures, errors, or skips. Both new Python
sources also passed remote compile checks using `/usr/bin/python3 -I -B`.
The source-bound fixture dependency is the reviewed TOOL06 producer repair;
its old independent test suites were not rerun.

The [retained guard receipt](nextup-global-reference-operator-verification-01.json)
reports zero actual business HTTP requests, zero actual process probes, and
zero blocked real I/O attempts across HTTP, process, original-read, and
authority categories. Original implementation bytes were not read.

| Frozen artifact | SHA-256 |
| --- | --- |
| Operator source | `4471154408ed09f4c12434e12928c8f37fd37f7ee231207615089f2529ccd01e` |
| Operator guards | `d976f0bb0796222fec616ba9e89430ae51cc26c88f80e5240beda14d8b24213e` |
| TOOL06 producer dependency | `2c1d4d31fd2dfaf01fac0969d89acc881774aa2005b97b647442e625e634c98d` |
| TOOL06 fixture-helper dependency | `dcafaf8ff644ab4cccb2686a0f15c808b095de62672ba2449942757a9ab1dc70` |
| Guard report | `d2ab303ad38ac6384d95a662b2ce8afa870fda28ab6c3861329022ced564bf44` |
| Remote guards.log | `a413da7a577e6fbc931eee483eabcf0dc2dd54d2f7eec78039d3e52f66ca0875` |
| Remote compile.log (empty successful output) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

The guards cover complete private-byte replay, the actual frozen
TransportRunner's 116-normal-request all-empty branch, known observation
failure with cleanup, incomplete wire and result persistence, outer
terminal/export/commit/close failures, current unit changes, evidence
permissions/source pins, sealed-root exclusions, raw ledger/token tampering,
and rejection of a forged successful preparation summary.

### Media membership and writable-parent review verification

Independent review found that opening only receipt-listed media files did not
detect an additional filesystem node, and that a disjoint output child could
still grant write access to a parent containing protected authorities. Both
guards are now enforced directly in the operator.

The new
`/opt/goby-test/exec-work-m3e/nextup-global-reference-operator-tool-02/verification-01`
run passed all **57 guards** and both compile checks. There were zero failures,
errors, or skips. The [new retained receipt](nextup-global-reference-operator-verification-02.json)
records zero business HTTP, real process probes, original-byte reads, and
blocked real-I/O attempts. All Python verification used `/usr/bin/python3 -I -B`
through `ssh test-env`.

The fourteen additional tests cover initial and checkpoint media additions
(MP4, NFO, directory, symlink, and special node), missing/replaced files and
directories, directory replacement while all sixteen file identities remain
unchanged, overly broad operator parents, and a genuinely producer-generated
unsafe matrix parent. The safe positive fixture uses a dedicated runs-03 parent
with matrix-03/operator-03 siblings, an external attestation root, and only the
exact existing lock file as its additional writable permission.

| Current artifact | SHA-256 |
| --- | --- |
| Operator source | `624c70b5a0217c94feab72eb18e5b7c760c12a6341851560efeee73c1b9a2720` |
| Operator guards | `780dc1478bd5747182bcb8f6eaf675ad5bb8ce1aebc5c1c3785bebc481added2` |
| TOOL02 guard report | `6268e5114f85b69e6f3ebd58236655d3f31bfd6f500f88a8fb32473d22d722c3` |
| TOOL02 guards.log | `c830922c800fc36ce2f377c29ed5a8a54e29367387c07fdc6baf396efbd9a515` |
| TOOL02 compile.log (empty successful output) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |

The producer, producer fixture-helper, transport, and matrix dependency bytes
are unchanged from the initial operator run. TOOL01 and its 43-case report remain
intact. No original or failed preparation scope was written during this repair.

There is currently no newly published fixture established by this document.
The failed preparation-02 scope and all previous sources/failure scopes remain
read-only. No business HTTP, original executable/assets/database read, or
service mutation is part of operator development verification.
