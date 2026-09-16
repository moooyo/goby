# Session handoff: native capacity preparation

Status: **paused at the user's request before native execution admission** on
2026-09-15. Save this checkpoint on `main`, push it, and continue in another
session. The next session starts here and in the
[current execution plan](../planning/current-execution-plan.md). All M2-M6
obligations remain in scope; M7 is deferred. This handoff does not complete the
overall goal or authorize an automatic resume.

## Repository and saved work

The starting commit was `01e579311ec2283bdf252bb357ceb88635ae0bf7` on `main`,
also present at `origin/main`. There was no separate branch to merge. This
handoff's containing commit saves the new work directly on `main`.

The complete candidate source snapshot is now versioned under
[`scripts/test-env/native-scan-http-capacity`](../../scripts/test-env/native-scan-http-capacity/README.md).
The [manifest](native-scan-http-capacity-source-snapshot.json) binds all eighteen
saved files, including seventeen Python sources and the original support
adaptation note. This replaces dependence on the session's untracked `.git`
scratch directory. Python and adaptation-note bytes are preserved by a local
`.gitattributes`; copying them must not normalize line endings.

The snapshot contains support, units, corpus, preparation, application,
observers, reader, reader-pool, workload, transport, closure, the integrated
controller, three synthetic check scripts, the read-only environment observer,
and the unchanged original journey helper. It contains no live credentials, private
runtime context, database snapshots or raw business responses.

Three unrelated untracked files remain out of scope and were not staged. Do not
inspect, execute, modify or stage them:

- `scripts/test-env/dispose-source41-resource-full-failed-pair.py`
- `scripts/test-env/test-dispose-source41-resource-full-failed-pair.py`
- `scripts/test-env/upgrade-main-schema25.py`

## Accepted progress and retained gaps

The fixed M5 refresh increment is accepted. Its focused coverage is 109 ordinary
tests plus one later browser test across their recorded source scopes. The final
ordinary run passed 25 packages, 2,295 top-level tests, zero failures and one
declared mount-profile skip; the Linux amd64 build and independent closure
passed. Reuse this unchanged product evidence instead of rerunning the suite
for operator-only changes.

The E11 internal amd64 systemd package is built and independently closed. Its
binary remains `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1`
(30,691,123 bytes); package archive SHA256 is
`2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a`.
The 864 tracked source inputs and 57 generated assets retain their separate
source/build bridges. No rebuild or product change was made for this handoff.

All four installation attempts are consumed and closed. The
[fourth runtime](systemd-installation-fourth-attempt.json) passed two nonroot
starts/stops, 272 requests, six observers and three snapshots. Its sealer then
rejected a valid plain-text Emby 401; the final sealing SQL observer never ran.
Failure preservation, archive readback and resource closure passed. Overall
installation acceptance remains open; no fifth attempt is admitted.

The [JavaScript attribution checkpoint](systemd-package-javascript-attribution.md)
records 46 emitted chunks and 73 exact glyph correspondences. It is not a full
npm contribution graph or proof of historical Google glyph inputs. The project
license question is still pending: do not choose a license or ask it again.
External distribution, hardware/native arm64/GPU/OCI, blocked-storage and host
durability, remaining media/transcode/administration work and full M2-M6 remain
open. Core movie/episode/subtitle client acceptance still blocks main promotion.

## Capacity state at pause

Remote scope names on `test-env`:

| Name | Exact path or value |
| --- | --- |
| E, business evidence | `/opt/goby-test/native-scan-http-capacity-20260915` |
| F, prospective fixture | `/opt/goby-native-scan-http-capacity-20260915` |
| EC, verification evidence | `/opt/goby-test/native-scan-http-capacity-checks-20260915` |
| APP / PG / anchor prefix | `goby-native-capacity-20260915` |
| Unit suffixes | `-app.service`, `-postgres.service`, `-net.service`, all under `/run/systemd/system` |
| Database and role | `goby_native_capacity` |
| PG port / HTTP origin | `25499` / `http://127.0.0.1:18099` |

SSH works again. E contains only saved preparation/source observations; F has
never been created. There is no capacity APP, PG or anchor invocation, no
controller input/decision/intent, no runtime context and no cleanup reserve.
The component check processes have ended; all twelve pool-fixture children
were waited for and were absent at the pause readback. No native workload,
SQL, service operation or business HTTP was dispatched by that readback.

The saved [preparation checkpoint](native-scan-http-capacity-preparation.json)
contains the original environment observation and earlier failures. Initial
capacity was about 5.07 GB free root and 6.63 GB MemAvailable on twelve allowed
CPUs. Those values are observations, not current reservations. A future
admission must obtain fresh capacity and absence evidence.

## Latest verification and exact limits

All paths below are beneath `EC/private/` unless stated otherwise. Raw records
remain remote root-private: directories 0700 and files 0600. Do not download
or publish raw records containing credentials or runtime data.

| Evidence | SHA256 | Bytes | Meaning |
| --- | --- | ---: | --- |
| `candidate-03/source-checks.json` | `4c389f6845e15713f4c29a3f0310ffa227ecbc57a60a127a8f3fc60309998177` | 2789 | Eight modules passed compile/global checks; no native action. The old transport emitted a warning. |
| `transport-checks-01/result.json` | `363c0c97e560d4c2aba60829bf9c1b23b404a1d5110bdff4c2e8fdbe2e0eb3e8` | 31036 | Eight groups passed on retained transport candidate-03. |
| `transport-checks-01-dispatch.json` | `8f0a2b26c68f4db47d97634bad55cbf907701f3e9603bb97608ddf97f1265465` | 1773 | One execution, exit zero; private stdout/stderr pinned. |
| `pool-checks-01/receipt.json` | `940498c38edbd98a3213861a8e79b4bb2d55d5eb0eaf3532bf339b73066ccafd` | 6083 | Six child-lifecycle fixture groups passed. |
| `pool-checks-01-dispatch.json` | `943612bc7898d61cfdbebe26d7b57af0e6af8236870c6c8c57d491ce5c487f95` | 1599 | One execution, exit zero, empty stderr. |
| `handoff-02/readback.json` | `8f770e6eb88a662d0134ee1047b179a6db86b5dfbbe06ac4712348e55b99e6ad` | 6126 | Source/result pins and six nested pool receipts reread; twelve children and prospective native paths absent. |

Transport groups cover control bodies larger than 512 KiB, the catalog cap,
complete and incomplete chunk framing, original credential callbacks before
later failures, first-error preservation through close errors, PUT/mutation
guards, and actual positive partial-send accounting. They use real transport,
Journey methods and AF_UNIX socketpairs with explicit ownership/namespace
substitutes. They do not prove native TCP, Goby responses or scan capacity.

Pool groups cover a normal zero-request pair, one child failing while its peer
is cancelled, cancellation observed before start, an owned TERM/KILL failure
close, controller namespace-description drift and bad source/ready bindings.
They exercise actual short subprocesses, WNOWAIT, waitpid and process-group
signals. The worker executable and identities are explicit fixtures; no reader
main, setns, Goby, SQL or HTTP executes. Stub timeouts or harness fallback cannot
count as passing cleanup. These results are separate from the earlier twelve
reader protocol groups; do not add them to product regression totals.

The root performed the pause readback. Independent full result review was
pending at handoff and has since passed in the [September 16 scoped review](native-capacity-transport-source-bridge-20260916.md),
without replaying the eight transport or six pool groups. The first handoff reader
failed an overstrict metadata comparison that included access time. Its source
is retained under `handoff-01`; the corrected reader excludes access time and
produced the `handoff-02` receipt. Neither reader reran component tests or made
business/service calls.

### One-byte transport change, subsequently bridged

The tested transport at `EC/private/candidate-03/capacity-transport.py` is
51,898 bytes, SHA256
`21c90bff5bdbbcd6f8562e6562c7fbbaf8f67b1f3433db80abd5995d5a5ddf4e`.
It emitted eight Python 3.13 `SyntaxWarning` messages for the same non-raw regex
literal during the eight fixture imports. Its stderr is 1,944 bytes, SHA256
`03890beb2a068b882c13f53b1e1bee8dd8b644c8ba15cf197efcc1a5f9612b56`.

The saved repository source only adds `r` to that regex literal. It is 51,899
bytes, SHA256
`a87681751f1eea4881fa549be4eb7fa5e6eb06aee030051271eab4f163190928`.
The [September 16 source bridge](native-capacity-transport-source-bridge-20260916.json)
has now verified AST/string-value equivalence and warning-free compilation for
this exact correction on test-env, with independent review. Candidate-03 remains
unchanged. This bridge does not cover the subsequent scan-overlap or clock-domain
changes; their affected checks remain separate.

## Integrated controller and remaining work

The controller is written (42,282 bytes,
`7fda93c6dd3849e78b44b1573c94c7699f547fe34d8c72df306980a54e78bbdd`),
compiled remotely and statically reviewed, but never executed. It binds one
source set and decision, holds the existing deployment lock, prepares one
isolated fixture, starts one APP, runs workload callbacks, then closes readers,
credentials, APP, observers, PG, anchor, archives, mount and lock. A successful
execution still returns pending independent review, not capacity acceptance.

The final static fixes bind the workload directory and absence/account baseline,
consume cleanup reserve without reserving it twice, preserve first failures if
receipt writes fail, recognize actual physically joined pool results even when
the first join raises, require explicit zero-child proof, and keep PG/anchor
closure reachable after optional-summary failures. Preparation fallback is
allowed only before any closure infrastructure stop method has been entered;
all paths retain the original deadlines. These fixes have targeted static
review, not synthetic orchestration or native lifecycle proof.

`capacity-controller-checks.py` does **not** exist. The user paused work before
it was written. Its proposed CLI is `--controller-source`,
`--controller-sha256`, `--workload-source`, `--workload-sha256`, with output
`EC/private/controller-checks-01/result.json`. Implement only the needed
failure-routing checks using real Controller methods and real workload
cleanup, with finite leaf substitutes for service/SQL/HTTP actions. Cover
first-error retention, actual join evidence, refusal of unknown zero-child
state, credential-close ordering, summary failure followed by infrastructure
closure, fallback eligibility, fixed deadlines and cleanup quota consumption.

Other remaining risks to resolve before admission include preparation/APP/
closure changed paths, admission/constructor/signal failures, deadline-expired
mandatory cleanup and conservative write-before-dispatch resource reservations.
The closure transport currently checks raw output growth after capture; retain
this write-before-limit concern rather than declaring a hard limit proven.
The final metrics reader is also unfinished: it must derive latency quantiles,
actual scan/reader overlap and resource-sampling gaps from real private records.

## Fixed prospective contract

Keep the [measurement plan](native-scan-http-capacity-plan.md) as the detailed
contract. The agreed profile has 1,000 tiny valid media leaves across A/B (each
200 Movie, 200 Episode, 100 Audio), twenty album NFOs and 53 physical directories.
Generate two fresh templates from the retained exact recipe and bind their
actual metadata/provenance; historical templates were not retained.

Use one nonroot APP invocation, two sequential normal `library.scan` admissions
and two restricted reader processes per phase, at most four readers total.
Three users require four credentials: native admin cookie plus admin, visible
and hidden Emby tokens. There are exactly two favorite writes and four
logout204/same-token401 pairs with final stored revocation checks.

HTTP quotas are 600 overall: 240 worker reads, 184 task polls, 112 setup/settled/
readiness and 64 cleanup. Workload setup consumes 68 of its bucket, leaving at
most 44 readiness requests. Each worker is limited to sixty GETs, 120 seconds,
two-second minimum spacing and one outstanding request. Control requests have
ten-second absolute limits; catalog bodies cap at 512 KiB and other control
bodies at 1 MiB.

There are six fixed read-only workload SQL slots and three separate
infrastructure setup SQL connections. Each observer reserves command failure
group closure and backend wait (26 seconds total reserve) inside its phase.
Preparation/business/closure are 300/1200/900 seconds with handoffs/reserve
within 2700 seconds from the original anchor; the anchor ceiling is 3600 and
cannot reset. APP RuntimeMax is 1500 seconds. Raw evidence is capped at 384 MiB
including 64 MiB cleanup reserve; resource and transport evidence each have
32 MiB caps, and each reader reserves up to 72 MiB. PG uses 768 MiB tmpfs.
New root allocation is at most 2.5 GiB while retaining at least 1 GiB free;
initial admission requires 4 GiB free root and 6 GiB available memory.

No SLO is invented. Insufficient active-scan or two-reader overlap means that
metric is incomplete; do not rerun, slow down scans, or enlarge the corpus to
manufacture overlap. Do not claim operating-system cold cache, representative
media throughput, playback, browser, main promotion or host durability.

## Resume sequence

1. Read this handoff, current plan, source manifest and preparation checkpoint.
   Inspect Git state and preserve unrelated work. Use the tracked snapshot as
   the next working source; old `.git` copies are historical duplicates.
2. Use `ssh test-env`; no local tests, syntax checks, builds or runtime probes.
   Reuse the completed independent review of the eight transport and six pool
   results, and the exact one-byte source bridge. Verify subsequent changed
   behavior in its own scope rather than transferring these results to it.
3. Complete the bounded controller checks and the remaining concrete changed-risk
   checks/reviews. Preserve failures; avoid another generic operator framework.
   Reuse verified unchanged code and product tests. Finish the post-close
   metrics/readback design before a fixture is live.
4. Freeze the exact final helpers, a new input and a reviewed execution decision.
   Inspect current environment/absence/capacity and bind template preparation,
   accounts, original lifetimes and cleanup reserves. No valid decision/input
   exists at this checkpoint; do not use a consumed installer input.
5. Only after that admission, run one bounded native profile. Do not keep a live
   fixture waiting for more implementation or independent review. Close owned
   resources, then independently review measurements, archives and protected
   state. Report incomplete metrics separately from product failures.
6. Update the active queue with the actual result and continue the remaining
   M2-M6 gates. Do not restart the old installation series or promote main
   while core video/subtitle acceptance remains open.

## Protected dependencies and operating rules

The original main/control remain inactive; three existing PG instances and two
candidate APPs are protected. The current guard is
`/opt/goby-test/m5-refresh-browser-20260915/private/m5-refresh-browser-prepare.py`
(SHA256 `8c682ccd5e616e44b32db52dbd9325e66d358f820ae75b1af5a0a1c5b2100927`).
Use `support.protected`; never call stale `base.protected`.

The base is the retained
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/main-isolated-restore-01/private/prepare-isolated-infrastructure.py`
(SHA256 `ce20e4dbe21add70f90370b84e1debcf136313db6b98e8fb62ebe47433e6a9d0`).
Its existing lock is
`/opt/goby-test/exec-work-m3e/main-deployment-schema25.lock`.
The reused old preparation definitions and input are pinned under
`/opt/goby-test/m6-systemd-install-20260915-r04/private/` in controller constants;
importing definitions does not permit replay of their old actor entry points.
Original E12 tool receipts and the retained D-Bus reference API receipt keep
their original paths. Read source pins instead of moving receipts to new paths.

Never access vendor/reference media-server code or databases, fabricate upstream
responses for compatibility claims, change egress, replay consumed seed/reset/
provision actors, or borrow the paused M2 mount/RAM. Master/key files are
stat-only; existing environment/config files are hash-only and never decoded.
Keep raw evidence private. New fixture context may be parsed privately. Use
English for code/comments/docs and Chinese for user communication; Windows
shell commands must use PowerShell syntax.
