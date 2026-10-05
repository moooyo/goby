# Pinned Intro Skipper matcher

This package mechanically ports the Introduction and Credits raw-candidate
matchers from
[Intro Skipper 12.0.4.0](https://github.com/intro-skipper/intro-skipper/tree/6e0cb179007ac4c16cd9f358e9a617e791e9bf06),
commit `6e0cb179007ac4c16cd9f358e9a617e791e9bf06`. It is GPL-3.0-only derived
code. Attribution, source references, and the complete license are retained in
`third_party/licenses/intro-skipper`.

`Analyze` accepts already-admitted raw Chromaprint sequences in caller-supplied
queue order. The caller owns source authorization, stable complete-content and
episode identities, extraction configuration, source duration, and proof that the
sequence came from the configured prefix. The package cannot establish these
facts from fingerprint words. It does not mutate the supplied fingerprints.

`AnalyzeCredits` retains the same cohort and option contracts but requires each
complete fingerprint to come from the final 450 seconds of its own episode, or
the whole file when shorter. This is the pinned episode credits-window default;
intro analysis percentage and prefix duration do not apply. The credits matcher
keeps the same minimum shared duration, longest-region selection, intrinsic start
snap, and first-valid-pair rule. Its RHS maximum is the native truncated
`duration - creditsFingerprintStart - 1` rather than `MaximumIntroDuration`.
Each accepted pair is shifted into its own source's absolute file clock before
candidate replacement and storage. `ValidateCreditsCandidate` and
`ValidateCreditsEpisodeResult` additionally require every stored candidate and
support interval to remain in its own admitted tail window. Source identities
still describe the complete file, not only its tail.

The port preserves the upstream sample duration expression, ordered inverted
index enumeration, duplicate-key last position, uint32 wrapping, gap rule,
unequal-length upper bound, strict duration ties, first-valid-pair break, intrinsic
`<= 5` second start snap, and RHS-only maximum-duration check. A snapped LHS may
therefore exceed `MaximumIntroDuration`. Tick conversion uses midpoint-to-even
rounding, as in upstream `TickConversions`.

Each candidate stores the actual accepted pair when it won. The peer's support
interval may differ from that peer's final candidate after a later comparison.
There is no fabricated visual evidence, confidence score, or three-episode clique.
`qualified` means that the pinned matcher returned a candidate; it is not an
independent accuracy or safe-boundary guarantee.

The seven exported `Options` retain upstream names, defaults, and units. Bounds
on analysis minutes, duration, time skip, index shift, cohort size, fingerprint
count, and comparisons are Goby execution limits. They are not claims about all
configurations accepted by the full upstream plugin. A zero `Options` value is
invalid: callers must start from `DefaultOptions`. Resource exhaustion,
cancellation, malformed identity, and malformed intervals fail the entire call
without returning partial results.

The following are outside this package: Jellyfin scheduling and queue admission,
chapter analyzers, recap detection, the credits black-frame analyzer and candidate
combiner, FFmpeg invocation, configurable chapter/silence/keyframe/end-snap
boundary adjustments, and playback offsets.
Upstream's intrinsic start snap remains. No independent Goby offset or visual
qualification rule is added.

The audio-only credits entry point requires at least two independently admitted
episodes with shared tail audio. It does not detect standalone movie credits,
classify post-credit scenes, or infer that everything after its candidate is
safe to skip. Its interval end is the matched audio end, not a fabricated
end-of-file boundary. The full upstream credits pass has separate chapter,
black-frame, combination, and boundary-adjustment stages; these are not implied
by the raw matcher port.

`ValidateCandidate` and `ValidateEpisodeResult` validate stored provenance and
interval structure against the original admitted cohort. They do not rerun audio
matching. Cohort fingerprints may be omitted for this purpose; the original
ordered source identities, durations, and extraction profile must be retained.

All verification must run on the designated remote test environment. The native
oracle test is opt-in because its audited corpus fingerprints are private test
artifacts, not repository fixtures.
