# Intro Skipper Go integration

Current intro execution uses `internal/introskipper`, a mechanical Go port of
the Introduction raw-candidate matcher from Intro Skipper **12.0.4.0**, commit
`6e0cb179007ac4c16cd9f358e9a617e791e9bf06`. Goby runs the matcher in process and
extracts its audio fingerprints with FFmpeg. A Jellyfin host, plugin DLL and
C# runtime are not production dependencies.

This integration selects the upstream raw matching behavior. It does not add a
custom detector, source-dependent offset or label-based publication veto.
Goby continues to own source authorization, independent episode identity,
bounded execution, persistence and effective-marker precedence. The derived
package is GPL-3.0-only; its
[notice](../../third_party/licenses/intro-skipper/NOTICE.md),
[source pin](../../third_party/licenses/intro-skipper/source-pin.json) and
[license text](../../third_party/licenses/intro-skipper/LICENSE) retain the
upstream provenance. The notice does not relicense unrelated project code.

## Matching behavior

`Analyze(ctx, Cohort, Options)` accepts an ordered, already-admitted cohort of
2–32 independent episodes with complete raw `uint32` Chromaprint sequences.
The caller establishes distinct episode, source and complete-content identities
and a common extraction profile. Alternative encodes of one episode cannot
provide independent support. The pure package performs no media, filesystem,
database or network access.

The translation preserves these upstream decisions:

- The fingerprint clock is the binary64 calculation `4096 / 11025 / 3`
  seconds per point. Complete arrays are retained; no leading-word trimming,
  source-PTS compensation or rescaled clock is introduced.
- Inverted-index insertion order, duplicate-key replacement and unsigned-key
  wrapping remain observable parts of candidate discovery.
- Matching points form runs under the configured Hamming difference and gap
  rules. Equal-length ties retain the first observed range.
- The Introduction queue stops at the first valid remaining pair for each
  episode. A later candidate replaces a saved candidate only when its post-snap
  duration is strictly longer. Goby does not sort the cohort inside the matcher.
- A selected start at or before five seconds becomes zero. This intrinsic
  upstream rule remains enabled. The maximum-duration check applies to the
  right-hand candidate before saving a pair; a snapped left-hand candidate may
  exceed that configured maximum, as it can upstream.
- Tick conversion preserves midpoint-to-even rounding to 100 ns units.

`TimeAdjustmentHelper` is outside the port. Chapter adjustment, silence
adjustment, keyframe snapping and configurable end snapping are omitted. There
are no extra start/end offsets, visual confirmation, three-source clique,
boundary padding or learned confidence values. Context cancellation, input
bounds and the 200-million charged-work limit fail the execution without
returning partial results; they do not select a different successful candidate.

A qualified result records its actual winning pair, including the target
itself. The other member's later final candidate need not equal the interval
recorded in this pair. `IntroSkipperCandidate` exposes the interval, pinned
upstream commit and both source-bound support records. Legacy `Candidate`
evidence remains separately typed and is not filled with synthetic visual or
multi-source metrics.

## Extraction and configuration

The default fingerprint window is the full source when its duration is below
five minutes. Otherwise it is the first 25 percent. Both are capped at ten
minutes. Extraction selects a local audio stream by the upstream channel-count
and original-index policy; a preferred-language request first uses matching
language streams when present. The current task uses no preferred language and
prefers the greatest channel count. A default-track flag adds no priority.

The extraction recipe uses stereo audio and FFmpeg's `chromaprint` muxer with
`-fp_format raw`. Its raw little-endian words retain the complete sequence,
bounded to 5,000 points. Availability requires the muxer's algorithm-1 default,
disabled silence threshold, raw output support and the actual `pcm_s16le`
audio encoder. The admitted executable
hash participates in the extraction profile and immutable job identity.
The existing source descriptor, full-content hash, before/after source checks
and process cancellation remain in force.

The media-analysis dashboard edits the complete `Profile.IntroSkipper` object:

| Option | Default | Goby input bound |
| --- | ---: | --- |
| `AnalysisPercent` | 25 | Integer 1–50 percent |
| `AnalysisLengthLimit` | 10 | Integer 1–10 minutes |
| `MinimumIntroDuration` | 15 | Integer 1–600 seconds |
| `MaximumIntroDuration` | 120 | Integer from the minimum through 600 seconds |
| `MaximumFingerprintPointDifferences` | 6 | Integer 0–32 differing bits |
| `MaximumTimeSkip` | 3.5 | Finite 0–30 seconds between matching points |
| `InvertedIndexShift` | 2 | Integer 0–32 neighboring fingerprint keys |

These bounds are Goby's execution limits, not claims about the upstream UI's
entire configurable range. All seven fields are required; zero does not mean
an omitted default. `MaximumTimeSkip` is a matching gap, not a boundary offset.
Configuration uses the existing decimal-string revision and whole-profile CAS.
A real change invalidates profile-bound derived references and requests fresh
automatic work; a no-op preserves the revision and references.

`introFFmpegPath` optionally selects a dedicated deployment executable, with
`introFFmpegSHA256` pinning it. Omitting the path falls back to the main FFmpeg
path. Executable paths remain deployment inventory and cannot be supplied by
HTTP configuration requests. The legacy fingerprint helper is not required by
this raw extraction route. Preview execution retains its separate tool and
artifact requirements. See [runtime inventory](media-analysis-runtime.md).

## Storage and publication

New admissions use execution version **6**, detector identity
`intro-skipper-v1` and the exact admitted Intro Skipper options. GAFB **v4**
stores the raw fingerprint array and exact binary64 extraction horizon with the
existing source/profile identity. It rejects mixed legacy feature arrays and
reads historical GAFB v1–v3 without manufacturing raw fingerprints.

Schema **54** adds the seven-option JSONB configuration with upstream defaults.
The migration does not advance the configuration revision or publication epoch,
rewrite old jobs, clear existing derivatives or withdraw source-valid v5
detected markers. Historical execution versions 1–5 preserve their original
serialization and admission hashes. New work cannot reuse their old execution
authority or treat their visual features as raw fingerprints.

Publication uses the existing library policy, task fencing, source/cohort
revalidation, profile revision and suppression state. Manual/import intervals
and explicit chapter markers keep precedence. The new pair contract changes
matching support; it does not weaken source authorization or allow a worker to
write markers for support-only sources.

The prior native assessment reported three protected-content overlap cases,
including after configurable adjustment was disabled. Those measurements and
the stricter reference labels remain unchanged in the
[native evaluation](intro-skipper-native-evaluation-20261001.md). Current
publication deliberately follows upstream raw candidates; the offline quality
assessment is not an additional runtime rejection gate.

## Verification boundary

The prior raw-candidate parity comparison used the same retained native
fingerprints and ordered 19-episode corpus as the adjustment-disabled native
run. It reproduced six candidate episodes, thirteen empty episodes and all
twelve candidate endpoints exactly at 100 ns resolution. This is matcher
parity for that frozen corpus, not new accuracy evidence.

The production extraction path also reproduced all 19 native raw arrays and
binary64 extraction horizons exactly. After tightening encoder capability and
final-source deadline checks, four representative sources were checked again
with identical output. The successful extraction recipe did not change.

All verification ran on `test-env`:

| Scope | Result |
| --- | --- |
| Go port | Default tests passed; race plus the frozen native oracle passed all 27 parent tests |
| Extraction | Complete media package passed; final extraction regressions passed 13 parent tests and six subcases under default and race execution |
| Library and tasks | Analysis, native result, cache, publication, historical v1–v5 compatibility and task admission checks passed |
| HTTP and configuration | Strict parsing, authentication, field errors, durable settings and revision conflicts passed |
| PostgreSQL 17 | Normal/recovery migration, schema54 catalog, mixed historical restore, raw cache restore and malformed native evidence rejection passed |
| Production pipeline | Real task manager, FFmpeg, GAFB4, matching, publication and Item/PlaybackInfo markers passed; cold/warm/force behavior, stale configuration, rejection and manual precedence passed |
| Concurrency | Four production/projection parent tests passed with the race detector; final production pipeline passed again after the extraction review fixes |
| Compilation | `go test ./... -run '^$'` passed for every project package |

Receipts are retained under `/tmp/goby-intro-port-20261001`, including
`engine/engine-verification.json`, `extraction/extraction-verification.json`,
`extraction/extraction-review-fix-verification.json`,
`evidence/fullpipeline-summary.json`, `evidence/library03.log`,
`evidence/tasks01.log`, `configuration-tests.log`,
`backup-analysis-tests.log` and `evidence/compile-final.log`.

The desktop and narrow-screen dashboard use the existing Material UI design.
The final frontend build/typecheck and ten decoder tests passed. Browser runs
covered 67 distinct scenarios: the initial run passed 64, three test-selector or
dialog-expansion issues were corrected, and the affected cases passed targeted
reruns. Later field-error disclosure and narrow-label changes also passed their
targeted checks; a complete final 67-case rerun was not performed. The checks
cover the seven-option form, decimal values, defaults, draft preservation,
inline server errors and revision conflicts. Details and all original failures
are retained in `evidence/frontend/frontend-summary-final.json`.

These checks establish implementation and frozen-corpus behavior, not an
accuracy improvement over upstream. The three historical protected-content
overlap cases remain recorded. The optional upstream boundary-adjustment
pipeline is still outside this port. Updated FFmpeg recipes passed syntax and
reference-tool checks; new software/AMD images were not built or deployed.
Their Debian Chromaprint build still needs artifact-specific verification
before release. No release, deployment or root-project licensing decision was
made by this local implementation.
