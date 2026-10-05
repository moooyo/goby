# Pinned default CreditsPass

This package mechanically ports the default chapter, current black-frame,
low-entropy fallback, candidate-combination, and time-adjustment branches of
[Intro Skipper 12.0.4.0](https://github.com/intro-skipper/intro-skipper/tree/6e0cb179007ac4c16cd9f358e9a617e791e9bf06),
commit `6e0cb179007ac4c16cd9f358e9a617e791e9bf06`.
The derived source and golden tests are GPL-3.0-only. See
[the dedicated notice](../../third_party/licenses/intro-skipper/CREDITS-NOTICE.md),
[source pin](../../third_party/licenses/intro-skipper/credits-source-pin.json),
and [license](../../third_party/licenses/intro-skipper/LICENSE).

`Detect` accepts a source duration, movie/episode category, ordered source
chapters, and at most one caller-admitted same-season raw Chromaprint candidate.
The independent audio matcher remains in `internal/introskipper`. Movies reject
shared-audio candidates: unrelated movies are never compared for repeated audio.
The caller owns source authorization, exact stream selection and source identity,
finite process/output limits, cancellation, and actual process joining. Playback
reads do not call this package.

The fixed defaults are a final 450-second episode or 900-second movie window,
15-second minimum credits, blackframe threshold 28, minimum black percentage 85,
current boundary refinement, and non-black card detection. Whole-source named
chapter matching retains the native reverse/single-match/adjacent-match policy
and 450/900-second chapter-duration bounds. The default .NET chapter expression
is evaluated in Go with its Unicode whitespace and negative-lookahead semantics;
arbitrary configured .NET expressions and the legacy black-frame analyzer are
outside this fixed-default port.

The visual stage retains the capped first-percentile threshold normalization,
50% frame density, cadence-based scene construction, targeted blackdetect
interval recovery, sparse-scene fallback, interval-first candidate ranking,
full-frame boundary refinement, and latest sustained low-entropy card fallback.
Entropy uses the original exclusive `0.35` and `96` thresholds, lower-quartile
cadence trimming, and 50% card density. A sparse accepted black scene remains
accepted if an optional interval probe completes without supporting intervals.

Every applicable analyzer contributes before combination. A chapter end bounds
combination, candidates within 20 seconds merge where no such boundary blocks
them, and separated intervals remain separated. Native `TimeAdjustmentHelper`
then runs once per combined interval, with chapter snapping, silence adjustment,
and nearest-keyframe snapping enabled; start/end offsets remain zero. The inward
window is 5 seconds, outward window 2 seconds, and endpoint snap threshold
2 seconds. Silence uses the upstream -50 dB probe and 0.33-second acceptance.

A combination boundary is **not** a final immutable playback boundary. The
pinned algorithm adjusts afterwards, so nearest-keyframe adjustment can move a
final end beyond an authored chapter end. Also, `-skip_frame nokey` may emit a
keyframe after a requested `-to` boundary: the native nearest-frame operation
retains these source-bounded values. The port tests both behaviors instead of
quietly adding a new final clamp. Original, combined, and adjusted intervals and
the combination boundaries are retained in `Result.Evidence`.

Multiple returned segments do not classify their intervening content. In
particular, the algorithm does not prove that a gap is a post-credit scene or
that everything after the first start is safe to skip. A caller must preserve
segment ends and must not replace them with an assumed end-of-file interval.

The `Probe` interface documents all timestamp coordinates. Whole-window black
and visual metadata are relative to the tail-window start; targeted black
intervals and full-frame boundary scans are relative to their requested start;
silence and final keyframe adjustment timestamps are absolute source seconds.
The two whole-window filter graphs must remain independent so an entropy
`format=yuv420p` conversion cannot alter blackframe format negotiation on gray
sources. Input evidence must remain finite, ordered, and within the source.

Goby intentionally fails the whole request on a probe error or cancellation,
with no partial result. The upstream plugin may swallow an opportunistic
interval/silence error; this error-handling difference prevents failed source
reads being persisted as completed no-match results. It does not change a
successful probe's detection decisions. Known absence of an audio track may
produce an empty silence result without a process attempt.

`ValidateStoredResult` checks fixed provenance/defaults, source bounds, closed
source names, bounded evidence, exact combination replay, hard-boundary replay,
the adjustment plan, and final adjustment replay. It does not re-prove pixels,
chapter names, or audio identity. The publisher additionally binds the exact
audio candidate and source facts, converts native seconds to ticks once, and
enforces a serialized result limit. Summary counts and probe limits are Goby
execution/storage bounds, not additional credit-detection heuristics.

Golden cases come from the pinned upstream chapter, black-frame, entropy,
combination, and time-adjustment tests. All execution and validation belongs on
the designated remote verification environment; no local tests are required.
