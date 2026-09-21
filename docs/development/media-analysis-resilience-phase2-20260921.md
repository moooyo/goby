# Phase 2: Automatic intro analysis and seek previews

Status: **v3 repair in progress; source `00047ac` passed original calibration but failed controlled cold-open qualification; complete acceptance is pending**.
Fresh remote admission, independent PostgreSQL setup, the actual schema-50
catalog, full product builds and an initial 13-package regression have completed.
The initial failed attempt remains retained. Targeted repair verification has
passed, including the source-geometry compatibility repair and native mocked
browser coverage. The first real-content run missed all six positive intros;
automatic skip and the remaining composed consumer journey are not accepted.

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
unsafe cache state. These observations describe the initial source freeze;
runtime results and subsequent repairs are recorded separately below.

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
the initial repair was frozen at `c3d3e7ab88eb68871378d4da91fd358fe376773e`.
Its second full product build passed all 20 steps. The affected full media,
server and command packages and three failed migration parents then passed:
1,563 parent tests, no failures and five explicit hardware-profile skips. All
five test process groups closed normally. The fingerprint binary is identical
to the previously verified helper. Unchanged package results retain their
original scope; the initial failures are not erased.

Actual source review then identified a separate compatibility gap: all six
historical episode sources omit the stream sample aspect ratio, and their
decoded frames report `0/1`. The previous geometry policy rejected them. The
new versioned display policy retains this unknown-source fact while using the
square-pixel display fallback documented by FFplay; it does not fabricate a
declared source ratio. Strict per-frame consistency, rotation, timestamps and
resource bounds remain required. Its full media regression passed except for
one new AVI fixture that lacked original packet PTS. The test-only repair at
`92577f23dc4f81cafc26bd5799ce99c7fb9f7018` uses an independently probed MP4 with
stored packet PTS and unknown SAR; that exact real-media test passed. Archive
comparison confirmed that production source is identical to the preceding
`e7d3fb601483f69cc9f9cf5ba4e436565c0f8ecd` full build. The affected 56 library
and 54 server parents, one real recovery-profile parent, and five Python
evaluator methods passed. The seven mocked native browser cases also passed.

## First actual consumer and accuracy attempt

Eight unchanged real sources and independent assistant-reviewed labels passed
admission. Labels were frozen before detector output. Review used actual decoded
frames, exact PTS, independent machine transcription and PCM signal observations;
direct listening and human review are explicitly false. A C1-only preview
calibration passed 33 samples and two browser decode rounds under predeclared
MAE 5 and P95/browser-point 16 budgets, including wrong-frame controls. Observed
maxima were 2.0511 MAE, 6 P95 and 1 browser-point error. The failed initial
browser identity query remains preserved; the correction observed the actual
owned browser process without changing those budgets or regenerating images.

The actual native configuration/CAS flow completed, all four intro work units
and all eight preview work units completed, and 24 preview variants were stored.
C1 at width 240 and the color N1 source at width 400 passed named-consumer visible
first/middle/last hovers, BIF parsing, source-frame timing and pixel comparisons.
Their range and anonymous-access checks also passed. This is partial preview
evidence, not completion of the entire consumer or restart journey.

Automatic intro accuracy failed: all six positive cases abstained, producing
six misses; the two singleton negative works correctly abstained. C1 reported
no repeated interval; C2/C3 reported insufficient audio coverage and low visual
diversity. The actual skip journey stopped because H1 had no published markers.
The original labels and failed result remain intact. The first matching
diagnostics used only C1/C2/C3 calibration features. Three additional, unseen original
episodes have been acquired and sealed for final holdout verification after the
algorithm is frozen; their content has not been decoded or reviewed. Existing holdout
outcomes must not be used to tune thresholds or relabel positives.

The six skips are the existing Dolby Vision, VAAPI/AMD and mount-helper profiles.
They are not passes, real preview-client evidence or phase-3 fault acceptance.

The initial eight licensed sources totaling 922,230,546 bytes were acquired using
ordinary workstation HTTPS and SCP after the remote direct route failed before
saving media bytes. The successful remote import independently measured every
source and checked the preserved permission/checksum records. It does not claim
successful remote HTTP. The originals are unchanged and the failed attempt is
retained. Remote decoding produced the timestamped visual-review artifacts used
for the frozen labels and failed accuracy attempt above. The current assistant's
audio input tool cannot consume the exported listening clip, so direct listening
is not claimed. Timestamped transcript and signal evidence were reviewed
alongside actual frames, with that method disclosed in the private labels.

Calibration diagnostics reproduced the failure without reading holdout features.
Dense audio anchors and fixed visual phase offsets did not repair it. C2/C3
show sustained audio similarity, but the current visual policy rejects their
slowly changing opening. C1 has only partial acoustic similarity to that pair;
its whole-opening mean does not establish continuous support or a reliable end.
A single private DCT hash substitution also failed and was not applied to the
product. The three additional calibration episode identities were fixed before
content review to investigate independent support for opening variants. Two
assistants independently reviewed all fifteen contact sheets and original
selected transition frames, with machine transcription as supporting evidence.
Their conservative endpoints agree. These three positive labels were frozen
before feature extraction or detector output and remained unchanged during the
two v2 calibration attempts below.
Together with the three sealed holdout episodes, fourteen independent originals
total 1,646,688,512 bytes. None of these additions removes the original misses.

An explicit v2 repair is in progress. Its first candidate profile was selected
before the new calibration features were extracted. It retains the strong
continuous-audio requirements and uses distributed visual support and distinct
visual states instead of requiring rapid changes between adjacent frames. The
original motion measurements remain diagnostic observations. Historical v1 JSON
and canonical admission fingerprints have their own frozen storage decoder;
current execution and automatic publication require current evidence. The new
acceptance manifest separates the six calibration episodes, the original
regression holdout and the three still-sealed fresh holdout episodes. Frontend
contracts expose confirmed visual coverage and the longest unconfirmed gap.
These source changes have not yet passed their consolidated verification.

## V2 calibration and mechanical regression

The first complete v2 candidate, `26c2e24af21f1b865beacc06df52a5bf63973709`,
passed its full product build. Its six-source calibration produced no candidate
groups. A fixed-profile observer then established two three-source families
with strong full-interval audio evidence: C1/C4/C5 and C2/C3/C6. The visual rule
incorrectly required an individual near-hash state to remain stationary for a
second before counting it as scene diversity. Other visual coverage conditions
also failed. These are calibration observations, not evidence from the sealed
holdout and not permission to change the source-derived labels.

The second unreleased candidate,
`d52cddd8ed30ee664c7fc9ce3d2f70722db8c7d7`, counts distinct observed states
inside continuous cross-source matching anchors. Its declared profile changes
the visual matching radius to 24 and minimum mean similarity to 750/1000 while
retaining the full-audio, complete-support, boundary, full-time and conflict
requirements. Insufficient diversity remains reviewable evidence. The old v1
storage contract and schemas 49/50 are unchanged; neither v2 candidate had been
admitted to a product database before this source revision.

The complete 20-step product build passed, with 17 binary artifacts and 73
frontend files independently read back. Its closure SHA-256 is
`006961bcd19453309263b61f0d16324d761baabe1e221d334b5e1024511fc0ce`.
The full matcher mechanical package then passed 53 parent tests with no failed
or skipped parents. Its receipt SHA-256 is
`df0e08e9cc9915b0bb8c31ef17a3325f959e4eca2004d4b9cb962f87b9a19134`.
An earlier wrapper attempt lacked `GOCACHE` and started no product tests; that
operational failure and its closed process group remain retained separately.

One subsequent fixed calibration invocation reused all six original feature
bundles without decoding media again. It performed exactly five analyses: the
original cohort and four predeclared two-minute narrative feature windows.
All six positives now have one candidate within the original safe interval and
five-second inward boundary tolerance, organized into the two expected groups.
All six remain `review`, so the required automatic positive score is still
0/6. The sole remaining reason is insufficient visual anchors: one group has
a 4.004004-second unconfirmed gap against the three-second limit, and the other
has a minimum complete-band fraction of 497/1000 against 500/1000. Aggregate
metrics alone do not identify every failing pair or anchor intersection.

Each of the four derived narrative controls actually ran and returned
`no_result` for all six members. These controls are feature slices from the
same six originals, not native edited-media or fresh-episode acceptance. They
do not increase the main corpus's independent-source denominator. The combined
calibration receipt is
`6ab41565392279deb21750b8c3f6f0147e58a8e070b712704a2c80beaa116ad3`;
the independent closure is
`0058b42cc49ea0a4c0515fc292cf2d14496c618ef3e2504e322534f3f2e58eda`.
The failed automatic score, unchanged labels, raw results and all closed worker
identities remain retained. The three fresh holdout originals remain unopened.

A subsequent fixed-profile observer reproduced the exact public result and
2,723,193 comparisons. Every complete band on both sides of all six pairs had
a sufficient continuous anchor. The actual remaining rejections were C1/C4's
4.004004-second gap, C1/C5's 3.003003-second gap, and C2/C6's 497/1000 band
with a valid 1.986987-second anchor. Original and final-projection reasons
agreed. The observation SHA-256 is
`9048f97198f649556ae13093865dbff6a766caf07b97199a8c99eec5bb0c05e9`;
its independent closure is
`a8d50bbf62ae5088bd7e5eacf4356b291b6617321acc35b7dd592ff1df0cb7da`.
The third fixed candidate therefore explicitly calibrates only the secondary
visual band's minimum to 400/1000 and the maximum internal gap to five seconds.
It retains full visual matched time of 850/1000, real one-second band anchors,
three-second edge limits, full audio support and all boundary/conflict checks.
This candidate still requires a new build and the same five analyses; no
accuracy or regression result from candidate 02 is relabeled as a new pass.

At that candidate's preparation point, integrated v2 library, task, HTTP,
backup/recovery, Python, Node and mocked-browser regressions were still pending.
Controlled real-media cold-open, recap and no-intro scenarios were being prepared
separately. V2 database provisioning and the fresh-holdout consumer run had not
occurred. Later scoped results follow below; no phase-2 publication has occurred.
Four superseded builds' reproducible `node_modules` directories
were retired after their workers closed; source archives, source files, built
frontend assets, binary artifacts, media and evidence remain retained.

## Controlled cold-open failure and v3 repair

The later source `00047acfe0e1bf0fa0ec4b4ab3ab6b61b0a8e152` fixed incomplete
visual sampling at short source tails. Its affected Go run passed 284 parent
tests with no skips; its six-original calibration qualified all six positives
and returned no result for all 24 derived body-window observations. These are
scoped results, not complete phase acceptance.

Controlled run 02 extracted all 15 native derivatives and completed all three
analyses. Recap ambiguity and body abstention checks passed, but all six
cold-open positives remained Review despite safe, source-labeled boundaries.
The frozen labels, five-second boundary tolerance and all 40 Options remain
unchanged. The failed score is retained with SHA-256
`6412a79d0cf0e49c577f9c37a88be0938e20b11d13f5f90bae11193a75912586`.

One private observational replay of the existing cold features produced exactly
the original public result and cohort. It confirmed that metrics from the raw
and guarded intervals were retained after final intersection. Some final pairs
remained genuinely weak; removing stale metrics alone does not prove accuracy.
The observer's independent closure is
`19dae4d7671cac217e4cfc83ee683e978d0e0d4a4371942deb7acfaa24411d3e`.

The v3 repair was frozen at product source `b88c528`. Guarded audio evidence now measures
only the original matched bins fully inside both intervals. Final group metrics
describe the current complete pairwise intersection; boundary, periodicity and
search-limit facts survive cropping. Historical v2 admissions, results and
archive validation have independent frozen wire semantics and cannot become
current automatic publication or worker authority.

The composed build succeeded using ten Go artifacts from the same product
source, one unchanged native helper and the retained frontend assets. Earlier
resource-limited build attempts remain recorded failures. The first Go run
exposed a test-fixture alias: unmarshaling into a shallow-copied `json.RawMessage`
modified the original admission payload. Test-only commit `21eff7e` detaches
those buffers; no production code, Options or labels changed. The accepted
composition contains 423 passing parent tests across seven packages, with no
skips or failures in that composition. Its receipt SHA-256 is
`ccd3f166032ec2aeadda79b4d1d9413e0968dadee58b4ffe847f4cf48fee6e24`.
The original post-run resource check refused a per-path WAL-growth charge and
remains false; a separate retained reconciliation measures allocated WAL-subtree
growth from the same original baseline. It does not rewrite the failed check.

Calibration 05 performed all six extractions and five analyses successfully,
but the required automatic positive score failed at 3/6. C2/C3/C6 each qualified
with one safe candidate within the original five-second tolerance. C1/C4/C5
returned no result. All four predeclared body-window controls passed. The score
SHA-256 is `f73fa4af790c6724d81441fd26ecf9ef10e142af1f8883045c79c1609219d5be`;
the independent closure is
`eeee6e74631d25a58e1df4da06cdceaa8d32965f847f1d7863dd6589e5fb43bd`.
The public reason `insufficient_audio_coverage` can originate from another
rejected pair offset and does not identify the final group rejection. One
observational replay of the unchanged detector and saved original features
completed, with exact typed result/cohort equality and the same 3,413,597
comparisons. Its 822 trace records show that all three A-family pairs were
retained without reasons. The admitted and refined clocks were identical.
Projection then repeatedly cropped the visual boundaries for 24 rounds. The
last C4/C5 audio measurement still had 957/1000 agreement, but visual trimming
left only 14.5145145 seconds, below the original 15-second minimum, and discarded
the entire group. The trace SHA-256 is
`b9eb0f56745613b402b7f34564e0f76a0ed846d41202ce7284eaacd06ff58a1d`;
the independent closure is
`69b1a30390176f72d2558c1affd5fde7dbf34a142662446fd4a3c7135d30782d`.
The original closure is supplemented by the same-invocation successful systemd
journal proof, SHA-256
`caf769a800dd7a1f9968f542fb66ac156a6380fde59e6058277aff86ced5d496`.
The diagnostic opened no labels or media and is not an acceptance run.

The subsequent source repair freezes each selected visual correspondence in
its original window, intersects the original confirmed bounds once, and
remeasures all evidence in the fixed final windows. Excluded mates remain
unobservable; current audio corridors, full time denominators, absolute bands,
states, anchors and all hard gates remain enforced. Zero phase and the one
hash-cost winner are measured as whole observations before pair selection.
The original admitted clock map is projected first; a qualified original is
retained, and at most one independent refinement may replace a compatible
weaker complete result. Refinement is a separately proven mechanism risk,
not the cause of calibration 05. All alternatives share one budget and fail
closed on errors. Thirteen new mechanical parent tests cover these mechanisms.
The unreleased v3 wire version, Options, labels and extraction implementation
are unchanged. This complete source repair still requires consolidated remote
verification. Controlled run 03 has not started. The
three fresh holdout sources remain sealed, and the named real consumer journey
has not been accepted.

## Required consolidated evidence

Acceptance must cover real labeled intro accuracy and boundary error, actual
skip consumption, BIF decoding and visible seek frames, full management flows,
source replacement and revocation, concurrent manual edits, configuration CAS,
task cancellation/restart, interrupted publication, bounded storage and resource
return, migration 49 to 50, and backup/restore of every new state category.
Synthetic mechanics do not replace real independent episodes. The initial real
corpus run established six misses and two correct insufficient-evidence
abstentions. Later calibration success does not erase that failure or establish
the still-pending controlled, fresh-holdout and consumer acceptance.

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
