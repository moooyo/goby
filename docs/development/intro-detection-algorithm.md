# Pure episode intro matching

`internal/introdetect` implements `introdetect-v2`, a pure Go, deterministic matcher. It does not open
media, run tools, access a database, infer episode identity from titles, or
publish an intro. `Analyze(ctx, Cohort, Options)` returns observations for a
complete explicit cohort window. The caller owns authorization, extraction,
source integrity, calibration, persistence and publication precedence.

This is the third fixed, unreleased v2 candidate. The failed calibration and
diagnostics of candidate 01 at source
`26c2e24af21f1b865beacc06df52a5bf63973709` and candidate 02 at source
`d52cddd8ed30ee664c7fc9ce3d2f70722db8c7d7` remain retained separately. No v2
job admission or result had been written to the database before this revision.
The source build and complete Options fingerprint distinguish these candidates;
historical v1 DTOs and thresholds remain unchanged. This revision is not an
accuracy acceptance claim.

Candidate 03 changes only two defaults from candidate 02: complete five-second
bands need 400/1000 matched time, and the maximum unconfirmed gap is five
seconds. This explicitly calibrates secondary visual corroboration; it does not
round a measured 497/1000 band up to 500/1000. The joint policy retains strong
full audio evidence, at least 850/1000 full visual matched time, and a real
one-second continuous anchor within every complete band. It allows a short
changing shot and sampling-phase differences without asserting picture
equality. Starting and ending anchor gaps remain limited to three seconds;
audio guards, scene-state corroboration, clique and conflict rules are unchanged.

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
6. Confirm visual observations under the same fixed audio offset and one-to-one
   nearest timestamp correspondence. Keep the original actual-time, contrast,
   sample-coverage, hash-distance and adjacent-change diagnostics. The adjacent
   500 ms change rate is no longer a qualification condition. The production
   visual basis remains `gray32-dhash9x8-rms-v1`; the private DCT experiment is
   not a production feature change.
7. Partition the complete acoustic interior into matched, contradicted and
   unobservable time on each source. An edge needs consecutive original
   observations in both sources, no gap above one second, and informative
   endpoints. Both cross-source endpoint comparisons must match to confirm the
   edge; an informative disagreement is contradicted, and missing/low-contrast
   observations remain unobservable. Black frames and missing time stay in the
   denominator. No isolated frame can represent an arbitrarily long gap.
   The published time partition comes from the side with less matched time;
   neither source borrows the other's coverage.
8. Require uninterrupted matched anchor runs, conservative edge anchors and a
   maximum unconfirmed gap. Complete internal five-second bands, fixed on each
   absolute source clock, must each meet their matched fraction and contain an
   anchor. Partial bands at the two ends remain in the complete time partition
   and gap checks and use the separate edge-anchor condition. They are not
   discarded or moved to a convenient scene. No complete band yields a minimum
   band metric of zero, never an invented 1000-permille success.
9. Independently cluster each source's informative observations against fixed
   first representatives. Choose the nearest representative within radius eight,
   breaking ties by its stable first-observation order. Representatives never
   move or union through a neighbor chain. At least four different states must
   have actual cross-source matched observations inside continuous matched
   anchors of at least `MinVisualStateAnchorTicks` on both source clocks. The
   state may change while the audiovisual correspondence continues: remaining
   in one hash neighborhood for a second is not a measure of dynamic scene
   diversity. An isolated matching point cannot borrow an anchor elsewhere, and
   unmatched, dark or gap-separated observations do not contribute states.
   Dominance counts every informative observation
   in the complete source interval, including transitions, rather than only
   favorable matches. The cluster radius is distinct from the cross-source
   matching radius and from the source contrast threshold.
10. Repeated state-entry times propose bounded constant visual periods. Each
    period is checked against the whole observed window, with one-to-one nearest
    timestamps and no per-frame best phase. Observed contradictory states reject
    that period; a hole or black frame cannot erase otherwise observed periodic
    evidence. A supported repeated four-state sequence requires review. Four
    static panels are not categorically declared non-intro content: the policy
    measures observed diversity and ambiguity, not narrative meaning.
11. Intersect acoustic interior and confirmed visual time. Grow bounded
   candidate groups from every matched edge, requiring a matching edge between
   every pair of members. A connected chain is not a consensus. Different
   opening versions can produce different groups. Final per-source bounds are
   intersections of the supporting intervals, never an extrapolated union.
   Retain each actual pair offset and check a fixed global source-clock map,
   using the smallest source identity as reference and the configured audio
   alignment tolerance. This is a bounded consistency check, not a time warp.
   Recheck audio, anchors, states and boundaries on the final intersection,
   without applying the extraction uncertainty guard a second time.

Equivalent pair hypotheses are sorted canonically and grouped against a fixed
first anchor. Selecting a stronger complete witness never moves that anchor,
so three nearby offsets cannot bridge two incompatible phases. Selection ranks
joint eligibility first, then audio and visual evidence, with actual coordinates
as stable ties. It never combines the best fields from separate observations or
adds a weaker duplicate alignment's reasons to a stronger witness. Group-level
deduplication likewise retains one whole proof. Distinct supporting pairs still
use conservative worst metrics. Initial uncertainty survives final cropping.

Group IDs bind the current version, options, exact chosen members, source-clock
map and fixed phase anchors. The clock map is internal matching evidence, not
worker authority. Exact complete-witness projections are cached within one
Analyze call so different seed edges do not repeatedly re-evaluate the same
dense clique; cache keys include original intervals, offsets, phase anchors,
metrics, reasons and acoustic evidence.

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
| Visual | Hamming distance at most 24; automatic sample agreement at least 850/1000 and integer mean similarity at least 750/1000 |
| Full visual time | At least 850/1000 matched time, retaining contradicted and unobservable time in the denominator |
| Visual observations | At least eight usable observations; contrast at least 40/1000 |
| Complete internal bands | Absolute five-second bands; at least 400/1000 matched time (two seconds) and a continuous anchor |
| Anchors and gaps | Anchor duration at least one second; maximum unconfirmed gap five seconds; starting-anchor and ending-anchor gaps each three seconds |
| Visual states | Fixed representative radius eight; at least four different observed states within continuous matched anchors of at least one second on both sources |
| State dominance | No state exceeds 600/1000 of all informative source observations |
| State representatives | At most 256 per source/candidate window; exhaustion is an explicit limit |
| Period hypotheses | At most the window's visual observation count, within the configured feature bound |
| Final projection | At most `2*MaxEpisodes+2` boundary rounds; exact-witness cache at most `MaxGroups*MaxCandidatesPerPair` entries |

Counts and logical memory budgets are independent of any serialized feature
cache size. The caller must also bound actual encoded bytes. The visual default
admits the extraction layer's 500 ms sampling over a complete 600 second window.
The feature budget is not a total heap/RSS limit: bounded indexes, vote maps,
group search and results also occupy memory. The comparison counter does not
count every map lookup or Go operation and is not a wall-clock promise. The
worker must provide a cancellation/deadline context and measured load limits.

The old fourteen metrics retain their v1 definitions. In particular,
`VisualTransitions`, `VisualChangeCoveragePermille` and
`VisualDominancePermille` still describe adjacent strong changes and exact
matched-hash dominance. They are not relabeled as anchors or clusters and can
remain low on a qualified slow-moving intro. Their three legacy Options slots
are retained and range-checked for an explicit profile, but do not determine v2
qualification.

The ten new metrics report anchor count, minimum matched fraction among complete
internal bands, the complete matched/contradicted/unobservable time partition,
maximum unconfirmed gap, the two edge-anchor gaps, supported distinct states and
full-source state dominance. `VisualDistinctStates` counts different near-template
identities actually observed inside sufficiently long continuous matched anchors; it
does not count stationary scenes or sum disconnected same-state dwell times.
The former unreleased `MinVisualStateSupportTicks` option is replaced by
`MinVisualStateAnchorTicks`, explicitly describing the evidence interval rather
than a requirement that movement stop. Group metrics use minima for positive support and
maxima for gaps, contradiction, unknown time and dominance. These worst values
may come from different pairs, so the three group time fractions need not sum
to 1000. `ValidateCandidateEvidence` shares the actual current qualification
predicate with the library's persisted-result validator.

The pure API's Options are tunable within hard bounds. Partial nonzero Options are not merged
with defaults; start with `DefaultOptions` and modify explicit values. An
oversized cohort, input feature budget, vote-bin budget, interval/group budget
or comparison budget returns `ErrLimit`. Invalid timestamps, keys or options
return `ErrInvalidInput`. Cancellation and all errors return no partial result
that could accidentally be published. The caller processes longer seasons in
explicit windows rather than quietly truncating them.

Weak audio/visual agreement, an observed repeat touching the extracted suffix,
short intervals, insufficient anchored states, dominant imagery, incomplete
anchor coverage, periodic evidence and limited
offset search require review. Search truncation in
any pair remains source-wide uncertainty: every retained group using that source,
and all corresponding candidates and episode results, are marked for review.
Distinct supported
intervals sharing an episode are competing hypotheses; affected groups and
episodes require review rather than selecting the earliest interval. Eligibility
is snapshotted before conflict marking, so marking one pair of competing groups
cannot leave a third phase qualified. A weak extra phase is not itself contrary
evidence against an otherwise established compatible interval. A
long animated logo or identically repeated narrative can still resemble an
intro in these features: the matcher has no semantic model that can prove
otherwise. Real labeled negatives and an accepted calibration profile are
required before operational publication claims.

## Acceptance boundary

Historical v1 JSON and its qualification rules remain explicitly versioned in
the storage layer. New admission fingerprints bind v2 and its full Options;
old audit facts are neither rewritten nor decoded against current defaults.
The extraction hash/feature codec is unchanged. V2 adds bounded result metrics,
not a migration that rewrites old schemas or manual decisions.

Synthetic unit fixtures exercise cold-open offsets, distributed fingerprint
noise, irregular timestamps, variant groups, duplicate identities, pairwise
consensus, silence/logo/visual negatives, competing intervals, conservative
boundaries, cancellation and resource limits. Additional v2 fixtures cover slow
and fast state changes inside continuous matched anchors, isolated states that
cannot borrow an anchor, sparse shared-title flashes, black-frame denominators,
state corroboration, dominance dilution, loops with missing evidence, immutable phase
anchors, three-way clock conflicts and final-intersection reprojection. They are
not evidence of real intro accuracy. This third fixed profile is informed by
the preceding candidates' retained calibration diagnostics. It must be evaluated as
one declared profile against the unchanged calibration labels and predeclared
controls; its source changes still require unified remote verification and
fresh held-out acceptance after the final algorithm freeze.

The integrated phase must use licensed, independent real episodes with labels
derived from independently reviewed source evidence and separate
calibration/holdout material. Disclose the review method, including whether a
human reviewed the labels or audio was directly listened to; automated source
review must not be described as human annotation. Report false positives,
misses, abstentions and boundary errors by category. The library layer must
recheck the target and supporting source/hierarchy revisions, suppression,
current authority and worker fencing before publication. Valid manual/import
overrides and explicit reserved chapters remain above detected results.
