# Pure episode intro matching

`internal/introdetect` is a pure Go, deterministic matcher. It does not open
media, run tools, access a database, infer episode identity from titles, or
publish an intro. `Analyze(ctx, Cohort, Options)` returns observations for a
complete explicit cohort window. The caller owns authorization, extraction,
source integrity, calibration, persistence and publication precedence.

## Input contract

All times are signed 64-bit, 100 ns media ticks relative to the original media
timeline. Audio samples contain a Chromaprint-like `uint32` fingerprint and
explicit disjoint `StartTicks`/`EndTicks` bins. Visual samples contain actual
presentation ticks, a `uint64` perceptual hash and integer contrast: grayscale
luminance standard deviation divided by 255 and multiplied by 1000. The caller
must use consistent hash orientation, normalization and sampling policy.
Frame indices multiplied by a nominal rate are not a substitute for actual
presentation timestamps, especially with variable frame rate.

The acoustic support of a fingerprint may be wider than its output-time bin.
`AudioBoundaryUncertaintyTicks` is the extractor's conservative uncertainty in
that mapping. The matcher shrinks both interval ends by the greater uncertainty
of the two supporting sources before intersecting visual confirmation. Zero is
not permission to guess that an unknown extractor delay is absent. The initial
Chromaprint extraction profile must derive this value from its actual pinned
step/delay semantics. Feature extraction records those facts separately.

Each episode supplies:

- `EpisodeKey`: the same episode identity across alternative encodes.
- `SourceKey`: an immutable indexed source/selection revision.
- `ContentIdentity`: verified identity of the complete media, not its first
  600 seconds or its opening. The library caller uses a complete-file digest.
- `AlgorithmProfile`: comparable extraction algorithm/version, normalization,
  sample and stream-selection policy. Actual per-file stream indices, start
  timestamps and other source-specific coordinates belong in provenance,
  not in this equality key.
- Duration, uncertainty and both feature sequences.

Identities are trusted caller facts, not authentication tokens or assertions
the matcher can independently authenticate without the media. Any shared
EpisodeKey, SourceKey or ContentIdentity joins a transitive duplicate group.
Only one deterministic representative can contribute support; selection favors
complete informative features and the profile available in the most independent
identity components, then analyzed span/usable feature count and canonical
identity. An unavailable earlier-sorting alias cannot displace a usable one.
Unused aliases are reported as `duplicate_identity`. Contradictory snapshots with the same
SourceKey are invalid. Different profiles are not compared.

## Search and confirmation

1. Sort episode identities canonically. Partition identity aliases and check
   profile-compatible independent support. No title or filename heuristic is
   used.
2. Index informative audio words by four disjoint byte bands. A word is
   informative only when its set-bit count is between 4 and 28 and it changes
   by at least two bits from a neighboring sample. Select an informative
   anchor from each of at most 192 deterministic sampling blocks.
3. Probe each byte's Hamming neighborhood out to `floor(MaxAudioHamming/4)`.
   This includes distributed noise such as one flipped bit in every byte.
   Check each retrieved full word once per anchor and vote for actual media
   offsets. Dense posting lists are retained; expensive inputs exhaust a
   comparison budget instead of silently losing competing evidence.
4. Offset bins are no wider than one quarter of the shortest actual input
   audio bin. Four distinct anchors are required. Keep bounded strongest
   offsets; if further distinct peaks are omitted, retained candidates require
   review. Offset suppression is limited to that fine bin width, rather than
   merging shifts that cannot align the same sequence.
5. Compare ordered audio bins using nearest actual timestamps and one-to-one
   target matches. Join only short bounded gaps in both sources. Reject overlong repeats
   instead of cutting their first 180 seconds into an alleged intro. Require
   12 distinct matched words, changing adjacent matched words, and enough
   matched informative duration. Sparse noise cannot make predominantly silent
   matches informative. Agreement uses each source's matched time divided by
   its own interval time, taking the worse source. Information fractions and
   adjacent matched-word changes are also checked on both sides; a sparse
   source cannot borrow a dense source's temporal coverage.
6. Independently confirm nearby visual timestamps. Require contrast, coverage,
   close hashes, at least four distinct matched frames and temporally
   corresponding frame changes. Only adjacent correspondences whose two ends
   both pass the cross-episode hash check contribute change time. Require enough
   changing time and reject a dominant repeated frame, so a few flickers cannot
   legitimize an otherwise static logo. Static logos and blank frames fail. Sparse
   observations with large time gaps cannot imply continuous visual support.
   Audio similarity without visual confirmation is not a candidate.
7. Intersect acoustic interior and confirmed visual time. Grow bounded
   candidate groups from every matched edge, requiring a matching edge between
   every pair of members. A connected chain is not a consensus. Different
   opening versions can produce different groups. Final per-source bounds are
   intersections of the supporting intervals, never an extrapolated union.

Clique growth is deterministic and conservative, not an exhaustive
maximum-clique solver. It can abstain even when a more expensive search could
find additional support. Each retained group nevertheless has direct pairwise
evidence for all of its members.

## Results and provisional defaults

`qualified` means the admitted feature profile met the configured gates; it is
not a calibrated probability or proof of narrative semantics. `review` retains
a supported but uncertain interval. `no_result` includes source-specific
reasons such as missing features, duplicate identity, incompatible profile,
insufficient consensus, low entropy or absent visual confirmation.

The result includes resolved Options, detector version, group/source identities,
intervals, supporting source revisions and measured similarity/count metrics.
Similarity is `1000 - normalized Hamming distance`; agreement and information
fractions are per-mille observations. Group metrics conservatively report the
worst supporting pair, and PairCount counts the complete pairwise witnesses.
These fields must not be labeled as a probability or statistical confidence.

| Parameter | Starting default |
| --- | --- |
| Complete cohort window | At most 32 episodes |
| Analyzed prefix | First 600 seconds, or the shorter source duration |
| Source feature counts | At most 6000 audio and 2400 visual samples |
| Feature memory budget | 256 KiB per source, counting each feature as 24 bytes plus identity strings |
| Work | At most 100 million full-word/alignment/group-edge comparisons |
| Temporary vote bins | At most 16384 for a pair |
| Candidate limits | Six offsets, 12 intervals per pair, 128 groups |
| Independent support | At least three episodes |
| Candidate duration after conservative trimming | 15 through 180 seconds |
| Automatic duration gate | At least 30 seconds; shorter accepted candidates require review |
| Audio | Hamming distance at most 6; automatic agreement at least 900/1000 and similarity at least 850/1000 |
| Matched informative audio time | At least 600/1000 |
| Visual | Hamming distance at most 10; automatic agreement at least 850/1000 and similarity at least 850/1000 |
| Visual structure | At least eight observations and three corresponding changes; contrast at least 40/1000 |
| Visual change and dominance | At least 300/1000 changing time; no matched hash exceeds 600/1000 of matched observations |

Counts and logical memory budgets are independent of any serialized feature
cache size. The caller must also bound actual encoded bytes. The visual default
admits the extraction layer's 500 ms sampling over a complete 600 second window.
The feature budget is not a total heap/RSS limit: bounded indexes, vote maps,
group search and results also occupy memory. The comparison counter does not
count every map lookup or Go operation and is not a wall-clock promise. The
worker must provide a cancellation/deadline context and measured load limits.

Options are tunable within hard bounds. Partial nonzero Options are not merged
with defaults; start with `DefaultOptions` and modify explicit values. An
oversized cohort, input feature budget, vote-bin budget, interval/group budget
or comparison budget returns `ErrLimit`. Invalid timestamps, keys or options
return `ErrInvalidInput`. Cancellation and all errors return no partial result
that could accidentally be published. The caller processes longer seasons in
explicit windows rather than quietly truncating them.

Weak audio/visual agreement, an observed repeat touching the extracted suffix,
short intervals and limited offset search require review. Search truncation in
any pair remains source-wide uncertainty: every retained group using that source,
and all corresponding candidates and episode results, are marked for review.
Distinct supported
intervals sharing an episode are competing hypotheses; affected groups and
episodes require review rather than selecting the earliest interval. A
long animated logo or identically repeated narrative can still resemble an
intro in these features: the matcher has no semantic model that can prove
otherwise. Real labeled negatives and an accepted calibration profile are
required before operational publication claims.

## Acceptance boundary

Synthetic unit fixtures exercise cold-open offsets, distributed fingerprint
noise, irregular timestamps, variant groups, duplicate identities, pairwise
consensus, silence/logo/visual negatives, competing intervals, conservative
boundaries, cancellation and resource limits. They are not evidence of real
intro accuracy. No tests have been run as part of writing this module.

The integrated phase must use licensed, independent real episodes with human
labels and separate calibration/holdout material. Report false positives,
misses, abstentions and boundary errors by category. The library layer must
recheck the target and supporting source/hierarchy revisions, suppression,
current authority and worker fencing before publication. Valid manual/import
overrides and explicit reserved chapters remain above detected results.
