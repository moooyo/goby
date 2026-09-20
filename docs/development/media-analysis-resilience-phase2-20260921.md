# Phase 2: Automatic intro analysis and seek previews

Status: **initial consolidated verification completed; targeted repairs await verification**.
Fresh remote admission, independent PostgreSQL setup, the actual schema-50
catalog, full product builds and an initial 13-package regression have completed.
The initial regression failed and is not accepted. Targeted repair verification,
native browser coverage and real-media accuracy/consumer acceptance remain pending.

This phase implements the second delivery in the
[approved three-phase plan](../planning/media-analysis-resilience-plan-20260920.md).
Phase 1 is closed and published. The current baseline is
`2b284c3e0852c7aec3f984779eaa54301143618a`. Changes belong to the isolated
`codex/media-analysis-resilience` checkout. Schema 49 is published and immutable;
all new persisted state belongs to schema 50.

## Implementation contract

- Two scoped task definitions, `media.intro_analysis` and
  `media.preview_generation`, share one analysis execution slot. Admission records
  target selection, current administrator or explicit scheduled-system authority,
  an immutable settings/tool profile, and bounded children. A waiting analysis
  task must not consume a generic worker slot or block other task definitions.
- Intro children publish at most 16 selected episode identities while reading a
  window of at most 32 same-library, same-series and same-season episodes. The
  entire selection is partitioned rather than silently truncated. Source bytes,
  hierarchy, supporting episodes, configuration and administrator decisions are
  checked again at publication. Complete-file content hashes prevent duplicate
  encodes or aliases from becoming independent support.
- Extraction uses bounded processes and original presentation-time evidence.
  Audio fingerprints and visual confirmation feed a deterministic, versioned
  matcher. Similarity metrics are not probabilities. Its initial thresholds are
  provisional until evaluated on labeled real positive, negative and holdout
  media. An unsupported or ambiguous source is an explicit abstention.
- Automatic results remain separate from manual/import overrides and reserved
  chapters. Uncertain results remain reviewable; administrator corrections and
  source-specific suppression are durable. Config changes withdraw publications
  that depended on the previous profile.
- Preview children process one media source and produce 240, 320 and 400 pixel
  variants. JPEG scratch files, final BIF bytes and live readers count against
  bounded storage. A generation has a random cache key, immutable manifest and
  seal. Filesystem publication is pinned until the database reference is resolved.
  Reference reconciliation and publication share a lock so a stale snapshot
  cannot remove a newly committed generation.
- Preview HTTP requests read existing results only. They authorize the current
  principal and actual source before conditional responses or bytes. BIF,
  ThumbnailSet and JPEG delivery use the named-consumer contract, bounded ranges,
  source-bound tags, and truthful missing-result semantics.
- Process shutdown cancels analysis admission and work, waits for actual workers
  and readers, then closes the derivative store. A request deadline does not prove
  that a blocked filesystem syscall or child process has terminated.
- Backups validate the new rows, typed payloads and relationships. Recovery keeps
  settings, manual state and audit history, but invalidates automatic publication
  proofs and discards feature/preview references that depend on the old runtime.

## Current source and remaining integration

The BIF codec, owned derivative store, matcher, scoped task extensions, schema 50
persistence, native management, backup/recovery and server integration now have
authored source and checks. Runtime wiring includes actual extraction, publication,
current-source preview leases, cache reuse/pruning, single-item/playback intro
projection and ordered shutdown. These are implementation observations, not
passing test results. The [runtime contract](media-analysis-runtime.md) records
deployment inventory and resource behavior.

Static integration review resolved an unbounded aggregate intro-child lifetime,
new-source intervals being projected onto earlier response snapshots, database
preview references outliving physical cache eviction, and a BIF header layout
that the selected fixed-interval Video.js consumer could not consume. Each intro
child now has an overall two-hour deadline; item/playback snapshots carry a
private source stamp; native inventory includes registered cache residency; and
generated BIFs use frame ordinals plus the actual sampling-interval multiplier.
Credential expiry remains a hard preview deadline, including when a later
observation shortens it or an authorization read blocks.

The preview-specific preceding-frame/EOF-hold sampler now covers ordinary A/V
tail differences and VFR long frames in source. It preserves actual source PTS
for repeated thumbnails and keeps every nominal slot. Each width uses two passes
under cumulative source/log/runtime budgets; this cost still needs measurement.
Semantic archive checks and real-consumer fixture source are complete. The last
cache review also retained charges for unresolved trash directories, preserved
missing ownership proof, and distinguished verified missing derivatives from
unsafe cache state. There is no runtime proof of these authored checks yet.

The source candidate was frozen at
`6ab55e26b7c65aadfce11b60f07ff14526daf839`. The fresh remote profile admits a
14 GiB free-disk reserve, at least 3,200 MiB available memory, one 2 GiB worker
and a separate 1 GiB PostgreSQL service. Tool identities and three unrelated
running services were captured before setup. PostgreSQL 17.11 owns six fresh
databases on port 55450; no phase 1 database or consumed profile is reused.

The actual PostgreSQL schema-50 catalog was exported from that candidate into
the fresh catalog database. Its 1,000,166 bytes have SHA-256
`f71492625688ab99145ce82bd2dd04098091a58f55cb0576f707f7302091f1be`.
The exporter and export process groups exited successfully without forced
termination. This proves catalog generation, not product regression acceptance.
The final product freeze includes these actual bytes and the schema-50 task-child
column expectation; full builds and consolidated tests follow that freeze.

Remote capacity was recovered by retiring one closed, reproducible Go build
cache and preserving 130 historical artifact files in three operator-private
archives. Every archived member was read and compared remotely; the copied
archives were checked by whole-file SHA-256 before their remote originals and
temporary transfer archives were released. Manifests, restoration instructions,
the one-attempt retirement journal and original closure records are retained.
Historical databases, logs, source trees, browser evidence and runtime
dependencies remain intact. Independent readback found all four archival workers
closed and the unrelated service identities unchanged.

## Initial verification and repair scope

Candidate `a3a74a32c8196e3f76e55ace6c799ec34e79d707` includes the actual catalog.
Its full product build completed all 20 steps, producing 17 binaries and 73
frontend assets. The compiled fingerprint helper passed seven protocol tests;
the three frontend source-test files passed 12 tests without skips.

The initial complete Go regression recorded 2,898 passing parents, nine failed
parents and six explicit skips. All 13 child process groups closed without a
timeout or forced termination. The backup, recovery-database and recovery
packages passed 128, 12 and 37 parents respectively. These results do not erase
the failures or constitute overall phase acceptance.

The failures identified two implementation defects: a substring check mistook
an interleaved `duration:` log field for a new PCM frame, and asynchronous
context propagation let analysis shutdown return before every admitted operation
had observed cancellation. Repairs preserve the strict timestamp/checksum proof
and retain operation ownership until its actual release. Other repairs address
procfs `ESRCH` observation, a moving-window fixture race, explicit new-table,
sequence and task-count expectations, a recovery test's fixed migration target,
and a missing OCR fixture environment variable. Published SQL and the actual
schema-50 catalog remain unchanged. Original failed outputs remain retained;
the repaired code has not yet been rebuilt or tested.

The six skips are the existing Dolby Vision, VAAPI/AMD and mount-helper profiles.
They are not passes, real preview-client evidence or phase-3 fault acceptance.

Eight licensed source candidates totaling 922,230,546 bytes were acquired using
ordinary workstation HTTPS and SCP after the remote direct route failed before
saving media bytes. The successful remote import independently measured every
source and checked the preserved permission/checksum records. It does not claim
successful remote HTTP. The originals are unchanged and the failed attempt is
retained. Remote decoding produced timestamped visual-review artifacts; no
detector has consumed this corpus yet and labels are not frozen. The current
assistant's audio input tool cannot consume the exported listening clip, so
direct listening is not claimed. Timestamped transcript/signal evidence will
be reviewed alongside actual frames, with that method disclosed in the private
label evidence and delivery record.

## Required consolidated evidence

Acceptance must cover real labeled intro accuracy and boundary error, actual
skip consumption, BIF decoding and visible seek frames, full management flows,
source replacement and revocation, concurrent manual edits, configuration CAS,
task cancellation/restart, interrupted publication, bounded storage and resource
return, migration 49 to 50, and backup/restore of every new state category.
Synthetic mechanics do not replace real independent episodes. No current corpus
has yet established a positive or negative accuracy result.

Publicly licensed, distinct episodes are research candidates only until their
actual media is viewed and labeled. Labels must identify their real provenance:
assistant source review is not human annotation. Freeze source-derived labels and
thresholds before detector output, retain visual/timing evidence, and report the
actual split and annotation method. An episode-level holdout does not establish
unseen-series or unseen-season generalization. Separate runtime cohorts must not
borrow calibration support for holdout execution. Do not invent historical season
identities merely to satisfy a fixture layout.

All tests, builds and runtime probes execute on `test-env`; local verification
is not authorized. Retain original failures and actual source/artifact identities.
Merge and push only after this phase's complete acceptance and resource closure.
Phase 3 compound capacity, storage blocking and isolated-guest reboot/reset
acceptance remain required after phase 2 closes.
