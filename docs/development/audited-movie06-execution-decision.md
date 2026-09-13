# One bounded movie06 diagnostic execution

**Consumed.** Movie06 failed before playback at `movie_detail_title`. Its
[owned state is closed](audited-core-movie06-owned-state-closeout.json), but the
diagnostic target was not reached. Follow the [plan review](audited-movie06-plan-review.md);
this decision cannot be reused for another browser run. The frozen entry and
original limits below record the decision actually executed.

Execute one fresh `movie-06` scenario under the existing revised goal. The v3
controller and closeout are now integrated: 49 component checks, 22 controller
checks, and a real read-only stdout capture passed on `test-env`. The unchanged
48 adapter and 16 movie-lifecycle checks are explicitly reused. No product or
dependency change is part of this decision.

The new observation is the bounded page-error diagnostic, collected together
with complete before/after source state and the actual candidate stdout. Earlier
movie05 error timestamps were adjacent to rejected external registration
requests; that is a lead, not a causal conclusion. This run should distinguish
an error tied to the restricted external request from an error in an internal
API or the core playback workflow. Error name/message, source location, phase
and nearby request ordinals remain redacted and marked for review. Successful
playback alone does not classify an error as harmless.

## Frozen entry

All remote paths below are beneath
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/`.

| Artifact | SHA-256 |
| --- | --- |
| `client-v3-controller-verification-01/movie-input-06.json` | `74327836695b69b44f547556fed494a6627d577240f059093a54d78d44671d08` |
| `client-v3-controller-verification-01/run-audited-candidate-client.py` | `8e32fe7b73457059c978b33eff91b5b790a6682061b7780ab6863a4977d24545` |
| `client-v3-controller-verification-01/verification.json` | `8f5bcbc15b04be86943b0722c34a029ed748d75909e61689e39329c46d2627b5` |
| `client-v3-component-verification-01/verification.json` | `994c013949005f3de6171588f1bf234ac13c4026f0cafdd3bf562871a34a0366` |
| `candidate-movie06-entry-review-01/review.json` | `43377d17196c82a33b8d89f25710e67ffaad043bc6562a1b84afc8345710d9d7` |

The entry capture at `2026-09-13T13:25:12.792361Z` matched all 35 tables and
sequences to the closed movie05 source-after. It confirmed the candidate,
PostgreSQL, lease and original-client hosting identities, the absent new output
and worker units, and the pruning deadline for every old play. The original
movie actor retains five play rows, two counted Stopped plays, play count 2 and
thirteen revoked sessions. The sole unstarted Prepared row may become Expired
under the [v3 contract](audited-client-v3-input.md). No account or history is
reset. Saved movie05 UI controls already show the existing From Beginning
button for an item with resume history.

## Limits and outcomes

Keep the existing 1,200-second outer limit with 240 seconds reserved for cleanup,
600/120-second browser limits and 1,000 gateway requests with 64 reserved for
cleanup. Use only the two new movie06 worker units. Do not widen external network
access, add smoke requests, restart the candidate/hosting or reuse prior output.
The runner repeats live admission immediately before work, saves stdout before
startup and after worker closure, and preserves exit codes and source state even
if the browser or closeout fails.

A full pass still requires the strict UI, physical delivery, authentication,
durable-state and cleanup checks. Otherwise retain the original failure and
close owned state first. Evaluate the newly captured error evidence offline;
do not automatically patch and launch another movie attempt. A still-unattributed
error stays an open acceptance condition. The other core scenarios and main
upgrade remain behind their existing gates.
