# Selected native transcoding closeout

The four controls selected by the product owner are implemented in the current
administrator UI, management contract, persistence, and execution consumers.
Local compilation and focused verification passed. The final production UI
completed two consecutive visually reviewed Loop rounds without new modification
recommendations against the supplied Design 2 reference.

This document extends the [November 8 decisions](admin-native-transcoding-decisions-20261108.md)
and supersedes the historical unimplemented statements for these four controls
in the [November 7 closeout](admin-native-transcoding-20261107.md). That earlier
source, eleven-group contract, verification results, and exclusions remain
historical evidence. The current contract is documented in
[Managed transcoding options](../api/transcoding-options.md).

## Implemented controls

| Selected control | Current behavior |
| --- | --- |
| Automatic throttling | `Runtime.EnableThrottling` controls new file-backed HLS video encoding admissions. Continuous producers retain their process identity across pause/resume; published finite windows defer unnecessary subsequent producers. Initial demand, required unpublished output, current permissions, and bounded requested delivery remain supported. Buffer thresholds are internal policy. |
| Independent extraction threads | `Runtime.ExtractionThreadCount` is a nullable preview-image decoder preference, `0..16`, with default preference 1. A positive value below the available logical processor count emits the thread argument; zero or a value at least that count delegates selection to FFmpeg. A multi-pass build freezes one preference. |
| Existing codec profile ceilings | `Runtime.CodecProfileLimits` has H.264 `baseline/main/high` and HEVC `main/main10`. Defaults `high/main10` preserve the complete existing executable range. The ceiling filters eligible new encoding candidates; unchanged source playback, copy, and admitted tuples retain their behavior. |
| ASS text negotiation | `Runtime.SubtitleOptions.AllowAssToText` controls new ASS/SSA negotiation into existing SRT/VTT delivery. Disabling it avoids style-losing candidates and retains eligible ASS or burn delivery. Explicit conversion GETs remain available. Existing admitted delivery remains fixed. |

Each new group has its own deployment-default/custom source, nullable reset,
current/default summary, validation, and existing revision/CSRF protections.
Unrelated edits preserve all four saved groups. Historical Goby defaults are
retained, including `AllowAssToText=true`.

Migration 78 extends the persisted runtime projection from eleven to fifteen
groups while retaining historical seven- and eleven-group reading. Its SQL SHA256
is `352ac2837596565caf430b12530e5de29aa16df6eab4a8e814ae2a6f3078c67c`.
The generated PostgreSQL 17 schema-78 catalog SHA256 is
`14aed14e07aa0d5a69462376ca8871785321ef8d4853c9a120ed9127a9d6cdc2`.
Published historical SQL/catalogs remain intact. Frozen schema-77 eleven-group
and current schema-78 fifteen-group archive/restore compatibility passed.

## Native reference and remaining boundaries

The official SDK declaration is 4.10.2.0; the isolated stock server actually
observed is 4.9.5.0. Factory defaults and runtime observations are attributed to
4.9.5.0, rather than inferred to be the behavior of a newer server.

Native automatic throttling uses the Emby FFmpeg stdin interface to select a
100 ms wait per input cycle. The observed default is a 120-second ahead buffer
with 8-second hysteresis, checked every two seconds. Application and release were
observed through continuing encoder output and reduced/resumed progress. The
observer missed a background-thread-spawned FFmpeg process, so no numeric native
FFmpeg PID/proc-state observation is claimed. Goby's file-HLS process/window
policy is an architecture-specific adaptation; progressive and live transports
keep their existing behavior. The two mechanisms are not claimed to be identical.

The native extraction-thread consumer is image extraction, established from the
inspected implementation. Fresh subtitle and font commands did not consume that
setting. Three native image requests returned 500 before image extraction, so
they do not prove successful native image parameter application. Goby's actual
four-pass image execution and frozen preference are verified separately.

Native ASS behavior was observed with a known-duration VTT client: false selects
burn delivery, true permits external VTT, and an ASS-capable client retains ASS
under both preferences. Explicit SRT/VTT conversion GETs succeeded under both
values. Native stock defaults false; Goby retains its existing true default.
The Goby SRT/VTT and dynamic negotiation contract does not claim the complete
native subtitle editor. Plain text cannot preserve all ASS appearance and
positioning; retaining ASS delivery or burning subtitles preserves those visual
effects when the corresponding playback path is eligible.

There is no newly blocked selected control or additional required owner decision.
The preceding choices to retain automatic deinterlacing, existing CPU tone
algorithms, the validated x264 subset, and CPU/AMD paths remain effective.
Previously declined product extensions, plugin functions, hardware diagnosis,
codec-combination qualification, and generic FFmpeg editors remain outside scope.

## Local verification

Verification used the authorized local Windows/WSL environment, shared Go build
and module caches, PostgreSQL 17, and retained ext4 media fixtures. The final
administrator build and embedded `linux/amd64` server build succeeded.

The final focused Go result is 141 parent tests and 333 subtests: 474 unique
passing results, zero failing results, and zero skips. These counts are
deduplicated by package/test after focused successful reruns. The original log
and rerun log are retained. Vet remained enabled. The image-test argument audit
was changed to a FIFO/in-memory reader to respect the existing `RLIMIT_FSIZE=0`;
the production resource limit and frame/PTS/argument assertions were preserved.

Actual worker checks include:

- `TestActualHLSPlaybackBufferThrottleMedia`: paced file-HLS media, zero-start
  cancellation, nonzero-start tail delivery, retained process identity, pause,
  resume, and published media/frame/PTS checks. This is a Goby fixture, not a
  claim that native Emby uses OS suspension.
- `TestImageExtractionThreadsActualPreviewsPreserveFramesAndFrozenBuildPolicy`:
  four actual FFmpeg preview passes with JPEG, PTS, decoder arguments, and one
  frozen extraction preference.
- Existing admission, direct/copy preservation, ASS policy, exact persistence,
  migration, and archive/catalog compatibility checks for the selected groups.

The separately bound actual-service HTTP/profile/subtitle/output receipt has
19 passing checks. It does not claim throttle-worker or image-worker execution;
those results come from the Go evidence above. The functional receipt SHA256 is
`d2c32d47854a67538c8cab39ffb87218cbd8a1a96c62c6b3d45e27c289d5efe5`.

Only two presentation sources and their rebuilt administrator assets changed
between that functional build and the final UI build: the CPU processing-state
label and the Chinese temporary HTTP-listener restart notice. The complete
2,006-file input comparison confirms unchanged backend and player production
inputs. Both immutable builds and the explicit presentation-successor binding
are retained; functional evidence is not relabeled as a new execution.

## Visual Loop

The first round found two actionable copy defects: CPU mode claimed hardware
availability, and the Chinese listener restart notice remained English. Both
were fixed and the final production UI was rebuilt before the clean rounds.

| Round | Scoped browser cases passed | Actual UI checks passed | Original PNGs individually reviewed | New recommendations |
| --- | ---: | ---: | ---: | ---: |
| 01 | 21 | 12 | 89 | 2 |
| 02 | 21 | 13 | 89 | 0 |
| 03 | 21 | 13 | 89 | 0 |

Each round includes 88 actual-service PNGs and one separately labeled fixture.
The clean rounds use new independent reviewers for 24 English desktop, 24 Chinese
desktop, and 30 Chinese mobile captures. Root separately reviews ten actual
invalid/reset/conflict/default-state captures and the fixture. Every original
PNG is individually viewed and SHA256-checked; automated passing results alone
do not count as visual review.

Both themes and all nine native runtime panels are covered at desktop
1440x1000 and Chinese mobile 390x844. Selected mobile controls have measured
44-pixel hit-area height, including 44x44 switch inputs. This closeout does not
claim a 360-pixel actual capture, mobile-specific invalid/conflict captures,
hardware qualification, or a repeated full worker execution during UI rounds.

Rounds 02 and 03 share the final production entry and Loop source SHA256
`294905764ba696c386b70035944111c6c19329391566625dc5b9f08019832cce`.
The separate production-input SHA256 is
`c04a9e03faa12c6317eccd991c255e7f5579cacea619710e609cbda0f60ae369`;
these inventories are explicitly different. `status --require-complete` returned
success with two consecutive clear rounds and `exitConditionMet=true`.

Final binary SHA256:
`970285436f35b94b8c1fa94c43c000844837f1a939bee5394a2ed44bd3b700cc`.
Final administrator index SHA256:
`af72eaf0596fc5d2039e8ddbaee9b7b1069d595426750089014df9059c8ffc6e`.
Both bytes were independently rehashed at closeout. Design-reference HTML SHA256:
`7adb88423afb214d3a08dc9091211f96d123d26a9efa93271d6064a38b21a5f9`.

## Evidence and lifecycle

Local raw evidence is retained under the task's
`goby-native-controls-20261108` directory, including `final-closeout.json`, build
manifests, both Go JSONL logs, functional and UI receipts, the presentation bridge,
all Loop originals and review records. Native reference evidence remains in its
separate `emby-native-execution-20261108-70a8132f` research directory. No private
credentials are included in this document.

The final application was stopped through its executable/birth-time-bound pidfd.
PostgreSQL was stopped after confirming zero other client connections; its data
was retained. Inactive task compiler scratch was removed, while shared caches,
source, data, ext4 media fixtures, immutable builds, and raw evidence were kept.
The isolated native reference processes and mounts were already closed.
Unknown remote resources were not cleaned or modified.
