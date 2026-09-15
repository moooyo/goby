# Retained Library theme failure timeline

The original scan-observer error was recorded about **19 seconds before** the
OOM time recorded in the incident summary. The retained logs do not establish
the cause of that error. Earlier resource pressure remains possible, and no
product correction or successful reproduction follows from this inspection.

The [bounded projection](library-theme-retained-timeline.json) reads three
members of the original failed-run archive: the supervisor report, Library
test stdout and the worker-owned PostgreSQL log. It performs no test, database
query, application request or service operation. The complete archive was not
rehashed in this pass; the report and stdout match their original saved hashes,
and the PostgreSQL member has a new content binding retained in the projection.

The projection is 9,523 bytes, SHA256
`3cb580420dc59105fabac65b72ea2f9b51d90f86d47038e855157921d3b92056`, retained at
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/library-theme-retained-timeline-20260916-01.json`.
Its source revision is `74a69abacdd9206e51f4e166df2346b5555b5cd9`.
The [original incident](programs-final-regression-incident.json), failed receipts,
package counts, source archives and recovery results remain unchanged.

## Recorded sequence

Go JSON events carry an explicit `+08:00` offset and are shown below in UTC;
the PostgreSQL log explicitly uses GMT. The warning's timezone-free logger text
is retained as printed and kept distinct from its containing Go output event.

| Recorded time | Observation | Limit |
| --- | --- | --- |
| 11:57:08.929 | The original `theme` subcase starts | Fixture construction is included; the original test has no phase markers |
| 11:57:22.476 | PostgreSQL backend 1208112 records `auxiliary catalog commit rejected` | The expected rejection message exists; the projection contains no complete transaction or query trace |
| 19:57:43, logger text with no printed zone | `Retrying scan job finalization`, with `scan_finalize_failed` | Its containing Go output event is recorded at 11:58:15.704 UTC; do not substitute one timestamp for the other |
| 11:57:54.250 | PostgreSQL backend 1208883 records a client connection reset | This PID has no recorded query or pool-slot assignment in the selected evidence |
| 11:57:55.785 | The test reports `read scan job: get scan job: timeout: context deadline exceeded` | Frozen source line 522 is the second, recovery scan; no last-status or read-duration diagnostic was present |
| 11:58:15, incident summary precision | The separately retained incident records a global OOM | The earlier observer error does not establish or exclude resource pressure preceding this event |
| 11:58:15.704 | Cleanup reports failure to persist final scan status, ownership-session loss, and a schema-removal deadline | These are reported cleanup outcomes, not timestamps of the original ownership loss |
| 11:58:16.013 | Backend 1208112 records user cancellation, a broken pipe and lost client connection | These archived PID records are not current runtime authority or proof of the first failure's cause |
| 11:58:27.304 | The subsequent `extra` subcase passes in 11.62 seconds | It does not turn the failed `theme` or parent test into a passing result |

The Library command returned exit 1 after 1,125.635 seconds; its supervisor
records `interrupted_or_timed_out: false`. Go's package interval is separately
1,119.13 seconds. Neither duration is the theme wait budget. The package failed,
and its partial passing events remain excluded from completed-package evidence.

## Source interpretation

Two independent static reviews found no demonstrated connection leak, unreleased
transaction or reuse of a cancelled scan context in this specific path:

- `libraryIntegrationWaitJobWithTimeout` gives the observer its own 15-second
  context. `GetJob` reads through the ordinary pool, while the scan task has a
  separate context derived from the Store. A failed read does not by itself
  identify the running scan's context or database wait.
- `ownedTx.Commit` marks the transaction finished and releases the ownership
  mutex even when commit returns an error. The theme publisher closes its
  witnesses/resources, and the inspected fixture queries close rows or finish
  their `Scan` calls. No specific release defect was identified.
- `persist final scan status` comes from database task finalization in
  `jobs.go`, not a filesystem-root probe. The subsequent ownership-session-loss
  message supports a database-ownership failure boundary, without locating when
  that ownership was first lost.
- The retained `Store.Close` error is its ownership-loss outcome, not a
  `Store.Close` deadline. It does not establish that fixture deletion raced a
  still-running worker after a close timeout.

Independent review of the saved projection and source interpretation found no
material issue; it did not rerun the case or reread the remote archive.
The earlier Query-context hypothesis remains withdrawn: both relevant snapshot
calls already pass `owned.ctx`. No timeout increase, blanket retry, swallowed
error or guessed product fix is justified by this readback.

## Next diagnostic action

Use the already prepared `codex/programs-theme-diagnosis` snapshot `3d6b79b`
once the coordinated execution window is available. Its existing phase markers,
last observed status, read duration and pre-cleanup ownership flags distinguish
the remaining timing questions. Keep the original assertions and budgets and
run only the original theme subcase first; retain non-reproduction as such.

The key comparison is recovery-scan entry, ordinary-pool read progress and
ownership loss before versus during cleanup. The present logs do not justify
starting a filesystem-root investigation solely from the shared unavailable
error, or attributing the earlier observer timeout to the later recorded OOM.
Do not repeat this archive lookup without an identified additional evidence
question. Heavy execution and the final Programs/client gates remain pending.
