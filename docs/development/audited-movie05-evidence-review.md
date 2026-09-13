# Movie05 evidence review

The single [bounded execution](audited-movie05-execution-decision.md) ran on
`test-env` on 2026-09-13. The movie UI completed in 28,134 ms: playback, pause,
forward and backward seek, Stop, logout, login, resume, another Stop and logout.
The adapter then reported `candidate_playback_chain_incomplete` and exited 1.
The original outcome and all raw artifacts remain unchanged. This is observed
workflow progress, **not completed client acceptance**.

The source remains product binary
`477d26adced672371707fdf9bb2b0b5e54014487dd2c962d145506887420cd9f`.
No candidate, PostgreSQL or original-client host restart, account creation or
seed reset occurred. Both owned workers exited, their PIDs are absent and their
cgroups are empty; the gateway exited 0. The controller completed its post-run
source/runtime capture before reporting the browser failure.

## Saved evidence

All paths below are beneath
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/` on `test-env`.

| Artifact | SHA-256 |
| --- | --- |
| `candidate-core-client-movie-05/private/failure.json` | `f6f9e2f869bc02689b0673f7623d803b8d36cb6022474df9e1caec4ff55bf249` |
| `candidate-core-client-movie-05/browser/observation.json` | `bc5ee78f25e2bdb2dbac1c0d5ae19c39bd7370a3829285d11745792aaf8045ef` |
| `candidate-core-client-movie-05/private/source-before.json` | `9a3735122520974a40b86056d878a888bc1aa22bccbe654438c08a9380f3b8aa` |
| `candidate-core-client-movie-05/private/source-after.json` | `be0dbd70d4f7ea7d4343a3ea6259f80be216b9239e7acfa6552b2c7858b33bb0` |
| `candidate-core-client-movie-05/gateway/index.json` | `0d004a59b253c9a665190785a79741f32998213123b6c8988648d640fc23ad18` |
| `candidate-movie05-entry-review-01/ledger-framing-review.json` | `3a97aae5684170819e4fa247fcbd3d12ac57c7e6019b6ad5434b6e8d5afaf00d` |
| `candidate-movie05-entry-review-01/physical-inspection.json` | `44ac7d140c64ed1955f3385589b553ca4f569648db704f2617264dd0ffa9f756` |
| `candidate-core-movie05-owned-state-closeout.json` | `7c38d00bcb4ece5e056e06a22be848cbd5e76c77f7d5b642d06f656be3dc27ab` |

The unchanged closeout verifier's physical framing routine passed all 355
gateway exchanges, their exact file membership and hashes, request budgets,
headers and byte-count relationships. Six external attempts were rejected
before upstream connection. This narrower result does not claim that the full
closeout passed.

Both authentication chains have complete login 200, logout 204 and same-token
401 evidence. The first rejection precedes the second login. Two physical
Playing/Progress/Stopped chains have complete 204 responses. All thirteen stored
authentication sessions are revoked. The two started play rows are Stopped and
counted; userdata has play count 2 and resume position 1,217,878,390 ticks.
There are no client playback references or encoding jobs. The old movie04
Prepared row is now Expired. Two additional unstarted preparations remain in
Expired and Prepared states under revoked sessions; they are not counted plays.
The independent [owned-state closeout](audited-core-movie05-owned-state-closeout.json)
reconciles all 35 tables and sequences against 29 complete critical API exchanges.
Thirty tables are exact, no old row was deleted, and only the expected owned
session/device/activity/play/userdata changes remain. It reads saved artifacts
only and preserves the browser failure; no additional business cleanup was needed.

## Why acceptance remains open

1. The adapter requires a completed BrowserContext media event when assembling
   its preliminary evidence. All seven context media events ended with
   `net::ERR_ABORTED`, so it reported zero media evidence. The gateway separately
   records eight media GETs, seven with actual delivered bytes; physical ordinal
   292 is even a fully delivered 206 response of 4,816,896 bytes. Browser event
   completion and physical response completion are different observations.
2. The logical-to-physical request association needs a complete review. One
   context event matches two physical requests with the same Range. Some resume
   requests have different context and wire Range values. Physical ordinal 339
   is a complete 200 with zero body bytes and cannot prove media delivery.
   These records must remain explicit; they cannot be silently dropped,
   deduplicated, marked complete or attributed to a harmless cancellation.
3. The saved observation and eleven retained playback report bodies contain
   `PlaybackStartTimeTicks` above JavaScript's safe integer limit. The existing
   generic closeout JSON reader rejects them. The exact integer tokens are
   retained on disk and in the Python inspection. A future correction must
   handle this specific protocol field losslessly in both observation and wire
   readers, with generic JSON, identity and position checks unchanged.
4. Both original-client login POSTs are UTF-8 URLencoded `Username`/`Pw` forms.
   The current closeout's physical login path calls its JSON-only body reader.
   It must dispatch by the recorded content type with strict field and duplicate
   checks, matching the existing server/adapter contract. The saved-state review
   decodes these exact two forms without converting them into fictional JSON
   wire bodies.
5. Four page-error events were recorded at 16,877, 16,993, 24,867 and 24,902 ms.
   The adapter retained only their timestamps. Their causes cannot be recovered
   from those entries, and successful visible playback does not establish that
   the errors were harmless. The present zero-page-error gate therefore remains
   unmet. A targeted error diagnostic is needed before a future execution.

## Revised next increment

Keep browser execution paused and use movie05's complete saved artifacts as one
whole-run regression fixture. Review preliminary browser evidence, physical
transfer attribution, actual form/JSON body representations, the specific large protocol integer and page-error
diagnostics together. Reuse the existing operators; do not build another
scenario runner, change original reports, manufacture a successful worker exit,
or reopen product-wide verification for these tooling findings.

The recorded page errors occur within 0-3 ms of external registration request
failures. This is a useful diagnostic lead, not a causal finding or permission
to expand network access. Preserve error name and bounded, credential-redacted
message/location evidence in the next diagnostic revision so that a future
error can be attributed without reading vendor source or changing the client.

The complete owned-state delta is now closed and preserved. Next make one
bounded offline implementation/verification increment for the evidence
contract and missing diagnostics. State precisely which remaining claims need
new observation. A fresh browser decision must name that discriminating
observation and bind the now-occupied source baseline; neither movie04 nor
movie05 can be reused or reset. Full movie acceptance, the other five core
scenarios, main upgrade/recovery rehearsal and the wider M2-M6 work remain open.
