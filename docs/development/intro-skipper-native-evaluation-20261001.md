# Native Intro Skipper evaluation

## Status update: 2026-10-01 Go port

The subsequent integration decision selects the pinned upstream Introduction
raw-candidate behavior in Goby, implemented as a mechanical Go port with
dashboard options and no production Jellyfin/C# dependency. The intrinsic
at-most-five-second start snap remains; optional chapter, silence, keyframe and
end adjustments are excluded. The prior raw-matcher parity comparison on these
19 episodes reproduced six candidates, thirteen empty results and all twelve
candidate endpoints exactly at 100 ns resolution.

The three protected-content overlap cases and all measurements below remain
unchanged. They describe the stricter reference-policy assessment and are not
an additional runtime veto on the selected upstream behavior. Production
extraction parity and integrated verification are recorded separately in the
[Go port record](intro-skipper-port-20261001.md); earlier matcher parity does not
establish those results. The remainder of this document retains the native
evaluation and its decision at that earlier checkpoint.

## Historical evaluation outcome

Intro Skipper can run without a detector fork, but the two evaluated
configurations do not meet Goby's current automatic-skip acceptance criteria.
The official plugin returned six introductions in the selected 19-episode
corpus. Three crossed frozen protected-content ranges by substantial amounts.
Disabling configurable boundary adjustment reduced those crossings but did
not remove them. At that evaluation checkpoint, no Intro Skipper integration
or Goby algorithm change had been made.

This is a deliberately difficult, already observed development corpus, not a
random sample or an estimate of Intro Skipper's quality on general libraries.
The [result manifest](intro-skipper-native-results-20261001.json) binds runtime
artifacts, source identity, labels, native responses, scoring and closure.

## Unmodified upstream execution

The evaluation used official binaries on `test-env`:

| Component | Version and binding |
| --- | --- |
| Intro Skipper | [12.0/v12.0.4.0](https://github.com/intro-skipper/intro-skipper/releases/tag/12.0/v12.0.4.0), source commit `6e0cb179007ac4c16cd9f358e9a617e791e9bf06` |
| Loaded plugin DLL | `12.0.4.0`, SHA256 `f5c10f3453d48bfa210c6c487c67fc3ee7ce4c96c51121b2a6608f8a923399e8` |
| Jellyfin | Official Linux x64 portable `12.0.0`, including .NET `10.0.11` |
| FFmpeg | Official Jellyfin `8.1.3-1`, reporting `8.1.3-Jellyfin`, with Chromaprint |

The plugin and FFmpeg archives match digests published in their GitHub release
metadata. The official Jellyfin distribution directory did not provide an
independent checksum; its downloaded digest and complete extracted-file
inventory are retained without claiming signature verification. The DLL's
version resource and loaded plugin API establish the release binary version;
the source csproj's older assembly-version literal is not substituted for it.

Jellyfin ran as a new, task-owned, loopback-only instance on port 28196. The
plugin's original scheduled task performed analysis and wrote its own SQLite
records. Detection thresholds, audio selection, search windows and boundary
settings remained at upstream defaults for the first run. Only automatic
startup/timer triggering and non-introduction analysis modes were disabled to
make the run controlled. The full source files were used, not shortened clips.

The default intro minimum is 15 seconds and maximum is 120 seconds. The native
search window is the whole file below five minutes; otherwise it is the first
25 percent, capped at ten minutes. Those rules were not replaced with Goby's
120-second regional-research window.

No local tests, runtime probes, builds or decoding ran. Local operations only
downloaded/transferred public artifacts, read source and wrote orchestration
or documentation. During that native evaluation, no upstream matching code was
modified or copied into Goby.

## Frozen corpus and admission

Nineteen unique episodes were frozen before native analysis:

| Native comparison pool | Episodes | Reference role |
| --- | --- | --- |
| Beverly Hillbillies | B1-B7 | Seven positive openings; retain old B1/B2/B7 and newer B3-B6 strata |
| Robin Hood | R19-R21 | Three positive openings |
| One Step Beyond | O1-O3 | Three semantic opening targets, with ASR-assisted source interpretation |
| Hubblecast | H114/H116/H118 | Three short-opening challenges, not negative examples |
| NASA Space to Ground | N388/N389/N390 | Three no-automatic-intro controls |

The five native season pools were execution groupings. A synthetic season
number did not establish an unknown original broadcast season. Local NFO files
contained only episode identity and numbering, never target boundaries or
labels. Internet metadata/image providers were disabled. All 19 native items
were checked for episode type, source path, duration, series and shared season
identity before analysis.

Source hashes and filesystem identities were checked remotely before admission
and after execution; all were unchanged. Original complete targets, protected
ranges and annotation qualifications were retained mechanically. Hubblecast's
roughly ten-second title block includes a shorter recurring bumper and an
episode-specific title card; it is reported separately from the 13 ordinary
positive targets. Visual annotations are sampled assistant reviews, not human
or continuous audiovisual ground truth. OSB speech interpretation includes
previous machine ASR. The new Beverly reviewers' differing black-separator
conventions and source-start censoring remain explicit.

## Default first result

The task completed and all 19 items have native `AnalyzedItems` records for
Introduction with the same configuration hash. Empty outputs therefore count
as completed abstentions or misses, not infrastructure failures. Native HTTP
responses and SQLite tick values agree.

| Measurement | Default result |
| --- | --- |
| Unique episodes completed / blocked | 19 / 0 |
| Episodes with an introduction | 6, all in Beverly |
| Historical endpoint-rule hits, ordinary positive targets | 1 / 13 |
| Historical endpoint-rule hits, short-opening challenges | 0 / 3 |
| Strict complete-target matches | 0 / 16 positive targets |
| Correct negative abstentions | 3 / 3 NASA controls |
| Outputs overlapping protected content | 3 / 6 output episodes |

The historical rule requires both endpoint errors within five seconds and no
protected overlap above one microsecond. Strict full coverage is a separate
measurement. B7 passes the historical rule: its returned `[0,23.2693797]`
misses only 0.2502203 seconds of the midpoint-based target. It is not described
as a wholly useless result merely because strict full coverage fails.

The more consequential failures are these protected-range crossings:

| Source | Native default interval | Protected content begins | Overlap, seconds |
| --- | --- | --- | ---: |
| B2 | `[5.4489494,30.7756681]` | 23.019183 | 7.7564851 |
| B5 | `[0,39.2827493]` | 29.524604 | 9.7581453 |
| B6 | `[0,40.492076]` | 30.025021 | 10.4670550 |

The combined protected overlap is 27.9816854 seconds. These guards include
post-opening establishing/narrative scenes and episode credits that the
existing reference policy requires preserving. This result does not establish
new audio-only ground truth or resolve every possible definition of an opening.

B1 also misses 6.176814 seconds of its target's head; B4 misses 31.801528
seconds of its origin-story opening. B3, all three Robin episodes, all three
OSB episodes and all three short Hubblecast challenges have no output. Finding
six repeated-audio candidates is therefore not six accepted complete intros.

## Configurable boundary-adjustment ablation

After preserving the entire default result, a separately frozen diagnostic
changed only these native settings:

```json
{
  "AdjustIntroBasedOnChapters": false,
  "AdjustIntroBasedOnSilence": false,
  "SnapToKeyframe": false,
  "EndSnapThreshold": 0
}
```

The native analysis configuration hash changed from `6908FA7CE6CEDB25` to
`35C9FF6C5530A5E2`, and all 19 items were reanalyzed. Fingerprint caches were
reused as the upstream design permits. The default result and labels were not
overwritten. Matching thresholds, source pools and minimum durations stayed
unchanged.

| Measurement | Default | Adjustment ablation |
| --- | ---: | ---: |
| Output episodes | 6 | 6 |
| Historical endpoint-rule hits | 1 | 1 |
| Strict complete-target matches | 0 | 0 |
| Protected-overlap episodes | 3 | 3 |
| Combined protected overlap, seconds | 27.9816854 | 22.1996274 |

The ablation still overlaps protection by 5.0924421 seconds in B2, 8.7418813
in B5 and 8.3653040 in B6. Configurable postprocessing contributes to the
overreach but does not fully cause it. This is not a raw-audio-boundary result:
the unchanged matcher still has an internal at-most-five-second start snap,
and intrinsic validation/clamping remains.

No further label-driven threshold search was performed. The optional
pre-registered Beverly pool-sensitivity runs were not needed to establish the
current protection failures and were not executed.

## Runtime, integration and decision

The first native scheduled task took approximately 8.09 seconds with affinity
to two CPUs. Sampled resident memory peaked at approximately 494.16 MiB for
the Jellyfin host and its children. This excludes download/setup time and is
not a pure matcher-memory measurement. The warm-cache diagnostic's task took
about 0.043 seconds; one-second polling observed about 1.02 seconds, so it is
not a second cold-performance measurement.

Plugin results were subsequently observed in Jellyfin's native MediaSegments
API, with its asynchronous projection queue empty. The immediate first
snapshot had pending projection work and was not misrepresented as completed
playback integration. Player UI behavior and Goby consumption were not tested.

The unmodified release has no standalone intro CLI or Go library. A direct
upstream reuse architecture would host Jellyfin plus the official plugin and
consume its HTTP segments, with a small library/identity adapter. That avoids
maintaining a detector fork but still adds a host and integration surface.

Under that frozen reference policy, neither evaluated configuration
supports direct automatic adoption. The substantial protection crossings are
the primary blocker, not B7's quarter-second shortfall. The user's preference
for upstream reuse is retained: this finding does not authorize resuming a
custom detector rewrite. A future adoption decision should first resolve the
confirmed boundary cases with upstream behavior or a separately justified
product-boundary definition, followed by frozen evaluation. Existing results
must not be relabeled retroactively to turn this run into a success.

The private benchmark scorer passed 15 remote fixture tests and independent
read-only review. These are scorer checks, not an upstream unit-test-suite
claim. Initial harness listener, legacy-auth-header and GUID-format assumptions
were corrected before analysis; upstream code and the first algorithm outputs
were unchanged. The native service and port are now closed. Media, SQLite,
raw responses and private recovery evidence are retained, with no global
package installation, shared-cache deletion, production deployment or push.
