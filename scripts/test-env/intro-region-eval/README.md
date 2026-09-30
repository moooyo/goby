# Intro region research evaluator

This is a read-only, development research CLI. Evaluation accepts a frozen
cohort of three source-only frame extractions and uses generic full-prefix
search. A separate score mode compares retained research reports with frozen
source labels. It does not connect to a database, write player markers or run
media extraction. Every report has `productionResult: false` and
`independentHeldout: false`. Returned groups are **research candidates with
observed bounds**, not production intro decisions or proof of continuous
semantic content.

Run this tool and its verification on the designated Linux `test-env`, using
Go 1.27.1. Non-Linux builds return an explicit unsupported IO error. The Linux
restriction provides descriptor-relative, no-symlink file reads using only the
standard library. No dependencies or OpenCV implementation are added.

```sh
go run ./scripts/test-env/intro-region-eval \
  -manifest /private/data/cohort.json \
  -output /private/reports/new-evaluation.json
```

The default operational timeout is two minutes. `-timeout 5m` is permitted;
timeouts must be greater than zero and at most ten minutes. This changes only
the wall-clock deadline. Descriptor, geometry, clock, support, motion, boundary,
and work-budget settings are fixed and have no CLI overrides. Interrupt and
termination signals cancel the context. Canceled, timed-out, invalid-input, and
budget-exhausted evaluations return a nonzero status. Ordinary failures use
errors, not panic recovery.

## Frozen input contract

The top-level JSON must contain exactly three sources:

```json
{
  "sources": [
    {
      "id": "episode-01",
      "episodeKey": "series-name:episode-01",
      "manifestPath": "/private/data/episode-01.manifest.json",
      "sourceSha256": "<lowercase SHA256 of the original media container>",
      "manifestSha256": "<lowercase SHA256 of the exact extraction JSON bytes>",
      "orchestratorSha256": "<lowercase SHA256 of the extraction orchestrator>",
      "driverSha256": "<lowercase SHA256 of the extraction driver>"
    }
  ]
}
```

The example shows one entry; provide three independently established episodes
with distinct source IDs, original content hashes, and `episodeKey` values.
`episodeKey` is a mandatory caller-asserted original-work identity, separate
from the extraction/case ID. It must be 1-256 lowercase ASCII characters,
begin with an alphanumeric character, and otherwise use alphanumerics, `.`,
`_`, `:`, `/`, or `-`. Multiple encodes of one episode must use the same key
and are rejected as a cohort even when their container hashes differ.
IDs are 1-64 ASCII
characters, begin with an alphanumeric character, and otherwise use
alphanumerics, `.`, `_`, or `-`. Case aliases are rejected. Distinct hashes are
necessary input identity evidence; they do not prove that files represent
independent episodes rather than remuxes, alternate encodes, or related cuts.
The caller must establish those episode facts upstream; the evaluator never
infers them from filenames or visual similarity. Older manifests without
`episodeKey` are rejected rather than silently treating the case ID as an
episode identity. An explicit adapted manifest can contain
`"derivedFrom": {"manifestPath": "/private/old-input.json", "sha256": "<SHA256>"}`;
the upstream bytes are bounded, read, and verified before source admission,
and the verified binding appears in the report. No new independent
held-out recall or precision result is claimed by this CLI.

Absolute paths and relative paths are supported. Extraction manifest paths are
relative to the top-level manifest; gzip paths are relative to their extraction
manifest. Cleaned paths can reference data outside the repository or manifest
directory. Every resolved path component must be free of symlinks. FIFOs,
devices, directories, and other non-regular input files are rejected without
blocking on a FIFO open.

Each extraction must satisfy the existing schema-version-1 source-only audit:

- Complete 120-second prefix, 1,200 ordered slots at 100 ms, actual source PTS
  inside its own nominal slot, and strictly increasing source-frame ownership.
- Full-frame 96 x 96 `gray` raster using `area` scaling; 9,216 bytes per frame.
- No matcher execution or detector-output use, equal source identities before
  and after extraction, successful exit, and complete raw readback. Source
  identities require integer `device`, `inode`, `size`, `mode`, `mtimeNs`, and
  `ctimeNs` fields, a nonnegative size, and regular-file mode.
- The frozen container-image, FFmpeg, and FFprobe identities in `input.go`, plus
  the caller-bound orchestrator and driver hashes.
- Exact original-media, extraction-manifest, gzip, raw-raster, and individual
  frame SHA256 bindings. The original media is not reopened by this evaluator.

These checks validate the frozen extraction contract and its byte bindings.
They do not independently repeat the upstream decoder, media-container audit,
or verification of the declared extractor executable/script hashes. Keep that
upstream evidence alongside the report.

Reads are bounded to 1 MiB for the top-level manifest, 2 MiB for each extraction
manifest, 12 MiB for each gzip, and exactly 11,059,200 decoded bytes per source.
Duplicate JSON keys, missing/null contract fields, invalid UTF-8, extra JSON
values, additional gzip members, trailing gzip bytes, truncated trailers,
checksum errors, and excess decompression are rejected. Additional extraction
audit metadata is allowed and remains bound by the manifest digest.

## Report and evidence retention

The output directory must already exist, belong to the current user, and not
be writable by group or others. The output path must be new. A same-directory
temporary file is created with `O_EXCL` and mode `0600`, written and synchronized,
then published by an atomic no-replace hard link. Unsupported hard links fail;
there is no overwrite fallback. Existing successful and failed reports are
never overwritten. Before publication, failure leaves no partial final report.

Reports identify the protocol and schema version, the exact top-level input
SHA256, and every non-test Go source file embedded at build time. The aggregate
implementation digest includes sorted filenames and content, each prefixed by
its decimal byte length and a colon. `admittedSources` contains only completely
validated sources with extraction/gzip bindings and per-frame verification.
If a later source or computation fails, earlier admissions and available stage
diagnostics remain, while all result groups are removed. `completed: false`
must never be interpreted as abstention. Exit zero means the bounded research
evaluation completed; it does not mean a candidate was found or approved.

`status`, `stage`, `work`, `limits`, and the prefix diagnostics distinguish
completion, ordinary abstention, ambiguity, cancellation, deadline, and budget
failure. Completed candidates carry actual observed source bounds and the
fixed-support audit evidence. Disjoint passing source components or intervals,
including evidence from non-anchor sources or other clocks, cause cohort-wide
abstention. Rejected ambiguity witnesses are diagnostics, never accepted groups.

The finite heuristic search is not exhaustive. It chooses one nominated fixed
geometry per target, considers finite affine clocks, and remeasures unchanged
fixed support within observed components. Strict frame ownership and missing
observations have a recall cost: real-cadence 0.98/1.02 speed cases currently
abstain when ownership breaks split support. Interior gaps are not permission
to extend outer boundaries, and moving islands or multiple distinct segments
can cause abstention. Sparse observations cannot establish semantic continuity
between sampled frames. These limitations are not evidence of new accuracy or
production suitability.

## Frozen source-target scoring

The separate `score` mode reads an existing research report and source-only
labels. It never calls the matcher, extracts frames or passes labels into
evaluation. The original command remains compatible (`-mode evaluate` is
optional). Run scoring on `test-env`:

```sh
go run ./scripts/test-env/intro-region-eval -mode score \
  -report /private/reports/frozen-evaluation.json \
  -report-sha256 EXACT_REPORT_SHA256 \
  -labels /private/labels/frozen-source-labels.json \
  -labels-sha256 EXACT_LABEL_SHA256 \
  -output /private/reports/new-source-target-score.json
```

The label document is bounded to 1 MiB, contains exactly three cases, rejects
unknown/duplicate/missing fields, and uses this schema:

```json
{
  "schemaVersion": 1,
  "sourceOnlyReview": true,
  "detectorOutputsUsed": false,
  "reviewerKind": "assistant-source-only-visual",
  "audioReviewed": false,
  "independentHeldout": false,
  "cases": [
    {
      "id": "episode-01",
      "episodeKey": "series-name:episode-01",
      "sourceSha256": "<original media SHA256>",
      "labelClass": "positive",
      "targetSeconds": [20, 32],
      "protectedRanges": [
        {"kind": "episode-title", "seconds": [33, 40]}
      ],
      "sourceLabelEvidenceSha256": "<original source-label evidence SHA256>",
      "variant": {
        "key": "",
        "support": "unknown",
        "evidenceSha256": "<source-only variant-review evidence SHA256>"
      },
      "notes": "Source-only visual review; audio was not assessed."
    }
  ]
}
```

Provide all three cases; every shown field is mandatory. Positive labels
contain one complete semantic target, never a matched interior substituted
after seeing output. Negative labels require `targetSeconds: null` and variant
support `not-applicable`. An empty protected array is explicit. Preserve all
original sponsor, narrative, episode-title and excluded-segment ranges when
mechanically adapting older labels. Overlapping target/protected evidence is
retained and protection still wins; scoring does not repair conflicting labels.
Up to 256 protected ranges per case are permitted. Ranges require two increasing
nonnegative decimal JSON numbers; labels may extend beyond the 120-second
analysis prefix. Numeric text is bounded to 128 characters and a two-digit
exponent. Notes and provenance remain bound by the complete label digest.

Variant support is `full-target`, `partial-target`, `unknown`, or
`not-applicable`. The first two require a canonical nonempty key using the
episode-key syntax; the others require an empty key. `sameFullTargetVariantDeclared`
is true only when all three positive cases declare full-target support for the
same key. This records a caller assertion, not verified independence or an
inferred common extent. Unknown or partial support never removes positive
targets from the score. Distinct episode keys and media hashes remain required;
separate encodes of one original must retain the same key and are rejected.

The scorer checks exact report/label bytes and the source ID, episode key and
media hash correspondence. It retains the reported input and implementation
identities separately from its own implementation identity. It validates
required report fields, source/group array lengths, completion/ambiguity state,
observed bounds and cheap fixed-protocol invariants. It does not remeasure
descriptors or authenticate a report's origin, original media, review text or
provenance hashes. Freeze source selection, labels and candidate implementation
before first evaluation; a digest or a timestamp alone cannot prove that order.

Every candidate is scored independently. `targetCoverageRatio`, head/tail
deficits, endpoint errors, outside-target duration and every positive raw
protected overlap concern the frozen semantic target, not patch coverage.
Decimal endpoint differences and the one-microsecond protection comparison are
exact; repeating coverage ratios are rendered to 18 decimal places. Two or more
candidates are retained and never unioned or reduced to the best label fit.
The case-level coverage is null for multiple candidates.

`legacyEndpointRuleHit` preserves the historical rule: exactly one candidate,
both endpoint errors at most five seconds, and no protected overlap above one
microsecond. `strictFullTargetCovered` is the separate coverage fact;
`strictContainedInTarget` is the separate no-overreach fact.
`fullTargetCoverageMatch` requires full coverage within serialization epsilon
and the legacy endpoint rule. Its outcome is
`full_target_covered_with_legacy_bounds`, not a claim of exact target boundaries.
For example, `[20, 31.9]` against `[20, 32]` can pass the legacy endpoint rule
while remaining an incomplete-target miss with a 0.1-second tail deficit.
`missReason` identifies missing candidates, protected overlap, multiple
candidates, incomplete targets or endpoint errors; protected overlap takes
priority over other miss reasons.

A completed positive abstention is a miss with zero target coverage and null
endpoint errors. A completed negative abstention is a correct abstention.
An incomplete evaluation can retain zero to three validated admissions; every
case is then `blocked`, with no coverage or negative success. The scoring tool
distinguishes `completed` (its own processing), `evaluationCompleted`, and
`scoreAvailable`. An invalid input produces no case scores. Score mode exits
nonzero when inputs are invalid or the original evaluation is blocked. Output
uses the same private, atomic no-replace writer, and remains research-only with
`productionResult: false` and `independentHeldout: false`.

## Verification

Run on `test-env` only. The default suite covers descriptor, cache, clock,
boundary, ambiguity, input-admission, cancellation, and atomic evidence IO
behavior. The full-prefix fixture suite is deliberately gated because it runs
the entire search. Passing only primitive tests does not verify full-prefix
discovery.

```sh
go test -count=1 ./scripts/test-env/intro-region-eval

GOBY_INTRO_REGION_FULL_PIPELINE=1 \
GOBY_PRIVATE_BOUNDARY_OUTPUT=/private/reports/new-pipeline-fixtures.json \
go test -count=1 -run '^TestBoundaryPipelineFrozenFixtures$' \
  ./scripts/test-env/intro-region-eval
```

The original deterministic eight-second fixture helper is preserved byte for
byte. Its observation hull is shorter than eight seconds, so the current test
requires abstention. A separate twelve-second moving fixture must return at
least one fully remeasured candidate contained in every known synthetic source
interval; shared-static and unrelated controls must abstain. The historical
test that required the eight-second fixture to succeed is not included.

The engine was imported from the frozen `observed-common-component-v2`
research implementation. Algorithm bodies, geometry, clocks, thresholds, and
budgets are preserved; integration adds context propagation and stage reporting.
Private paths, media, reports, and previous failure evidence are not repository
inputs. Reproduction of a real cohort requires its separately retained frozen
extraction manifests and frame gzip files.
