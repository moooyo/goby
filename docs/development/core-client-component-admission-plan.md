# Core client component admission plan

Status: **implemented, independently reviewed and admitted for the single
TV diagnostic input**. The [verification record](reviewed-tv-baseline-verification.json)
binds Python 61, JavaScript 53, final-source log/history 12 and runtime 18 checks.
Both implementations loaded the actual retained TV baseline, including the one
revoked admission05 P credential omitted by the first input. This is the
small source-admission step after checkpoint `ddffba4`, supporting the
[one planned TV observation](core-tv-response-observation-decision.md).
No controller, helper, runtime identity or test result is changed by this file.

## Fixed scope

Add one branch for run-input version 5 and `scenario=tv-browse`. Reuse its
existing `avVerification` descriptor for a new component receipt. Keep v1-v4
on their original validation paths with the historical `FROZEN`,
`AV_VERIFICATION` and reused-verification meanings. Movie v4 remains an offline
contract; enabling a new movie input needs its own later component decision.

The v5 baseline is `audited-candidate-reviewed-tv-baseline`, version 1,
`reviewed_closed_state`, `scenario=tv-browse`; its loaders are
`load_reviewed_tv_baseline` and `readReviewedTVBaseline`. Preserve its current
complete snapshot separately from `tvProvenance`. Do not reuse the movie type.

Both v4 and v5 must retain the early rejection of closer SHA256
`94910123694fe6f772cb0cfcc80696ed1691a46d33b0669ef8e969f7d81cbb94`.
For v5, also reject the old `AV_VERIFICATION` descriptor before loading runtime
state or creating output/workers. A different closer hash alone is not admission.

## Minimal receipt interface

The new v5 input explicitly pins a receipt with exactly these fields. A `Pin`
is the existing strict `{path, sha256}` descriptor; use the current protected
read, ownership, size and byte-integrity rules. Private evidence and source files
retain their distinct mode policies.

| Field | Required value or binding |
| --- | --- |
| `kind` | `audited-candidate-client-component-admission` |
| `version` | Integer `1` |
| `scope` | `retained_tv_browse_v5_components` |
| `controller` | Pin equal to the actual executing controller's `self.source_pin` |
| `sources` | Exactly the existing nine `SOURCE_FILES` roles, equal to the run input's selected pins |
| `runtimeHelper` | Pin equal to the new wrapper selected by the run input; its actual bytes must match before import |
| `retainedBaseline` | Pin equal to the run input's exact typed TV baseline |
| `currentRuntime` | Pin equal to the run input's separately reviewed current-runtime record |
| `ancestor` | The exact old native-rejection component receipt pin below |
| `evidence` | Exactly `{dispatch, javascriptResult, cases, runtimeVerification}`, all Pins |
| `independentReview` | Pin to the source/evidence review that binds these selected descriptors; it must not refer to this receipt's future hash |

`evidence.dispatch` binds the final source archive, selected files, actual check
commands and their saved stdout/stderr/result pins. `javascriptResult` is the
existing closer result; `cases` identifies the real typed TV baseline cases.
`runtimeVerification` is the new wrapper's separate source/contract verification,
not a fresh live identity claim. The exact executable source set in those records
must agree with `controller`, `sources` and `runtimeHelper`.

Require actual zero exits, reaped check children, no timeout, matching output
hashes, unchanged sources, no unexpected skips/failures and the required named
cases. Python and JavaScript saved-case results must identify the selected typed
TV baseline and preserve its real inventory. Bind totals to the frozen test
inventory; neither a positive count nor a self-declared `approved`/`passed` flag
is sufficient. The independent review must name the same source/evidence pins.

## Reuse the known ancestor without changing its claim

The fixed ancestor is:

`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/native-rejection-observer-verification-02/verification.json`

SHA256 `17c2b2aafb751417cc98827e642c6f6aac30e3202723d424d96f38cf22d3e9dd`.
Its existing reused ancestor remains
`tv-parent-client-component-verification-03/verification.json`, SHA256
`85f831e66d1b2f3574141ebcb3c369156b71673066e31802c62f1398280e80df`.

Reuse their already reviewed scope/source fields by these exact pins; do not
rerun their tests or repeat full archive/ancestor reviews. Keep the old validator
and historical hash set unchanged. In particular, the old native receipt's
`freshComponents` stays exactly `adapter, subtitles`; it did not verify the new
controller, closer or wrapper.

For v5, all eight roles except `closer` must match the validated old native
source hashes. This includes the adapter/native hook, TV helper, gateway/proxy,
session proof and other unchanged journey helpers. The hook is part of the
adapter, not another independently selectable script. The current adapter hash
is `7b8a399d1179dbf4326ebbb860734d3a7355a040a67b50e6e086930bacbd6635`,
and the TV helper hash is
`5acb53c7fd5852f501e6590d09974913e888b12bd64ac8aef233953dc7cdfb65`.
Any change to these reused roles needs its own affected evidence; the new branch
must not automatically accept it by reading a replacement hash from the receipt.

## Bootstrap and preflight order

The existing `main` imports `runtimeHelper` before reading the run input. That
order cannot grant authority to a newly selected wrapper. Validate its source
before executing it:

1. Leave the old v1-v4 bootstrap path unchanged. For the new path, use the known,
   fixed old runtime's `read_bootstrap` only as a protected file reader to obtain
   the root-pinned input and new component receipt. Do not call its environment
   decoder, runtime pinning or service methods.
2. Perform v5 structural checks, the old-closer/old-receipt refusals, exact source
   membership and the receipt/input cross-bindings. Read the actual controller,
   nine component sources and selected new wrapper; match every hash before
   importing the new wrapper. Retain the existing source-path, sibling-directory,
   output-exclusion and budget restrictions.
3. At the existing component-verification point in `ClientRun.preflight`, use one
   finite v5 branch to finish receipt/evidence validation through the protected
   readers. Older versions keep their original native-rejection branch. Require
   the same outer input and self-source pins after bootstrap; no mutable global
   replacement of `FROZEN` is needed.
4. Load the typed TV baseline through its production loader. Independently
   validate the selected `currentRuntime` and wrapper contract, then perform the
   current candidate/hosting/catalog/state/capacity checks. Only after all these
   gates may the existing `EpochIO.open` and worker path run.

The wrapper's v5 path must not call old `verify_environment_epoch_files` if it
would decode retained environment content. Use its separately reviewed hash-only
replacement. Keep old epoch/seed/admission provenance unchanged; current process,
listener and lease checks belong to the explicit current-runtime branch.

The pure component validator can have the bounded interface
`verify_av(receipt, input, controller_pin, read_pin)`. It checks only the fixed
receipt/source/evidence contract above. It returns no permission to bypass the
subsequent runtime or business gates.

## Small verification set

Reuse unchanged adapter/native-hook/TV and transport evidence by source identity.
Do not launch another browser or rerun product regression merely to admit this
source selection. Run the existing controller, closer and log/history checks on
the final frozen helper sources, including the new typed TV and branch cases:

- Reject the old closer or old receipt for v5 before output, bootstrap-state,
  helper import or worker dispatch; preserve all legacy version checks.
- Reject a changed controller/self pin, wrapper pin or selected closer pin;
  reject missing/extra roles and any changed supposedly reused role.
- Reject the wrong ancestor pin/scope, incomplete or failed evidence, mismatched
  test-source hashes and a review naming different evidence.
- Reject movie baseline types, substituted TV baseline/current-runtime pins,
  and unbound baseline provenance. Exercise the actual saved TV/latest state
  through both production loaders, including all 35 tables, five sequences and
  both foreign audio references; preserve no-media/no-Playing/no-UserData-write
  constraints and the explicitly authorized Prepared exception.
- Prove a new wrapper is not imported before receipt/source validation and that
  the v5 path never invokes the old environment decoder.
- After the receipt exists, perform one saved-only integration using its real
  final shape/source pins. Stop deliberately at the next unavailable runtime
  prerequisite and require zero output/worker/HTTP/SQL/service dispatch. Record
  the stage reached; do not replace the real receipt with a synthetic approved
  object and call that final integration.

Record the resulting actual counts; the earlier v4 47/46/12 totals remain their
own evidence. Avoid a new general test or approval framework.

## Acyclic construction and separate runtime gate

Freeze controller, closer, wrapper and checks first. Execute the bounded checks,
close their workers, independently review the exact saved source/results, then
write the new component receipt. Only afterward freeze the root-controlled run
input that pins it. The code contains schemas, rules and known ancestor pins,
not a hash of the new receipt. The receipt's controller pin is compared with the
actual `source_pin`, not with a future constant embedded in that source.

The review must bind source/evidence, not the future receipt; a final saved-only
integration of the completed receipt is recorded separately without rewriting
it. This avoids both source/receipt and receipt/integration hash cycles.

The [current-runtime resolution](core-client-current-runtime-resolution.md)
remains a separate prerequisite. The recovery changed
process/listener/lease identities while preserving the prior source/data; do not
rewrite old epoch identities, rerun seed/admission, or treat component checks as
fresh live admission. `runtimeHelper` in this receipt proves the selected wrapper
source; `currentRuntime` supplies separately checked recovery/current-state facts.
The two pins must agree with the run input, and both contracts must pass.

No component result waives pageerrors, explains unmatched physical media, admits
a playback request or transfers historical client acceptance to E11. This file
was written by source/document reading only; no test or live action was executed.
