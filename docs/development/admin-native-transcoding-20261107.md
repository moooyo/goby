# Native transcoding alignment and verification

The product owner selected applicable native Emby transcoding controls after the
Design 2 review. Restore comparisons, automatic updates and channels, pre-install
backups, collection extensions, exact skipped-file counts, metadata ordering and
NFO/image writeback, and additional hardware diagnosis or combination qualification
are outside this increment. A fixed 24-hour rollback guarantee is not a product
commitment. The [Design 2 scope decisions](admin-design2-20261107.md#product-scope-decisions)
remain authoritative.

## Implemented native controls

The [managed contract](../api/transcoding-options.md) documents the exact eleven
runtime groups and their consumers. The increment implements native software
encoder presets and CRF bounds, independent inherited/automatic/manual codec
threads, a validated x264 analysis subset, H.264/HEVC software encoder enablement
and priority, HEVC/hardware encoding gates, surround-to-stereo gain, embedded
subtitle and font extraction gates, three CPU tone-mapping algorithms and height
limits, and desired/active transcoding cache locations with restart semantics.

The native reference combines the official SDK 4.10.2.0 declaration at commit
`45e73012eddf985dcfe878ca031b500085e16496` with read-only stock Emby Server
4.9.5.0 editor observations. The relevant static declarations agree between the
inspected snapshots. Observed factory values and dynamic editor values are
attributed to 4.9.5.0, rather than asserted as newer server behavior.

- [EncodingOptions](https://dev.emby.media/reference/pluginapi/MediaBrowser.Model.Configuration.EncodingOptions.html)
- [Native codec parameter API](https://dev.emby.media/reference/RestAPI/CodecParameterService/getEncodingCodecparameters.html)
- [Official transcoding guide](https://emby.media/support/articles/Transcoding.html)

EncodingOptions belongs to the native core even though its SDK documentation is
under the Plugin API namespace. Plugin-only functionality is excluded. The native
global EncodingThreadCount is obsolete; the implemented codec ThreadCount is a
separate current control. Historical Goby defaults remain intact, including bitrate
control, HEVC enablement and downmix multiplier 1. The administrator can explicitly
select the inspected native reference preferences.

Execution capture version 2 retains version 0/1 command and durable identity
semantics. Migration 0077 extends the closed runtime constraint and future INSERT
default without rewriting the existing singleton, its revision, or its timestamps.
Published migration 0047 remains unchanged. The PostgreSQL 17 schema-77 catalog
was generated from a new empty database owned by a task-specific nonprivileged role.

## Remaining native capability differences

These are native capabilities or editor surfaces that Goby does not fully match.
They must not be described as absent upstream or represented by successful no-op
controls. The product owner can remove the corresponding design controls or
authorize a separate extension.

| Native area | Current Goby boundary |
| --- | --- |
| Broader x264 parameters | Seven bounded analysis/quality parameters are supported, including the inspected native factory string. Arbitrary file, GOP, profile, scheduling and rate-control fragments are rejected. |
| ProfileLimit | Existing output negotiation supports AVC baseline/main/high and HEVC main/main10. A broader native profile-limit editor is not implemented. |
| CPU Bt2390 and tone refinement | Hable, Reinhard and Mobius are implemented. CPU Bt2390 is unavailable in the selected CPU tonemap filter. Native temporary parameter/desaturation controls are not implemented as persistent settings. |
| Native throttling | Native EnableThrottling and two method declarations exist. Exact units, hysteresis and execution behavior were not established by the inspected public/editor evidence; equivalent execution is not implemented. |
| ExtractionThreadCount and DeinterlaceMethod | Native properties exist. Their precise execution scope and method semantics were not established; Goby rejects these managed/native writes rather than storing inactive settings. |
| Additional encoder and hardware interfaces | Managed software identities are V-E-libx264 and V-E-libx265; the existing hardware paths are the implemented VA-API and Vulkan paths. Other vendor interfaces and encoder implementations are not fully equivalent. |
| Broader native subtitle/FFmpeg editors | The generic EditObjectContainer framework and all dynamic editor details are not implemented. Goby's extraction controls and validated execution configuration remain separate. |

## Local verification

Local verification continues the explicit authorization in the original
administrator task. Windows builds and reviews the production frontend; local WSL
Debian supplies Go 1.27.1, PostgreSQL 17.11, FFmpeg 9.0.1 and ext4 media fixtures.
Shared compiler and module caches are reused. No additional GPU qualification is
claimed from the CPU worker or from synthetic capability fixtures.

The final Go evidence contains 134 distinct parent tests and 532 subtests after
package/test-name deduplication: 666 passes, no failures and no skips. Its layers
include 282 execution checks, 222 managed settings/API/consumer checks, 137 catalog
and migration checks, and 25 real archive checks. Repeated tone tests are not
counted again. The archive witness preserves all eleven runtime groups, automatic
and manual threads, source cache path, and exact revision 9007199254740995 through
actual pg_dump and restore.

Actual execution evidence includes PCM/AAC/MP3 surround gain and frame clocks,
high-gain saturation without integer wrapping, CPU encoder presets/CRF/threads,
and the three tone algorithms with BT709 output and preserved frame timestamps.
Independent native HTTP verification additionally confirms complete HLS outputs
for an old CRF 26/thread 2 admission and a subsequent CRF 21/thread 3 admission.
Both contain ten segments, 298 video frames and 1,398 audio frames, and decode
completely. The settings and actual UI checks exercise CSRF/CAS rejection,
whole-group validation, omission of untouched groups, zero versus null threads,
explicit empty/null/reset semantics, and correction of x264 errors without closing
the editor. A new process opens the desired cache location and executes a complete
progressive MP4 while preserving settings.

The complete before-restart and after-restart functional phases passed 24 and 15
check groups respectively. Earlier unsuccessful helper attempts remain raw
evidence: incorrect widget/HTTP-status expectations and an interrupted local
process were corrected without relaxing production behavior or discarding data.
The earlier runtime, private credentials, media and database remain retained.

The final production build uses embedded administrator assets. Its Linux/amd64
binary SHA-256 is
`875d67d448f5af816227932a5951f6d9e45c78be59ea65a10d916b0ccaca0061`;
its administrator entry is
`e14a356a0a528ff3c5bf85ab72f6dbf6a9c019a0638a5eee0c166be54e66c4df`.
All 2,000 production input records were unchanged during this build. The changes
from the first verified binary are three presentation files and regenerated
administrator assets; backend and player inputs remain identical. Functional
execution evidence and presentation evidence are bound separately.

## Actual visual loop

The scoped loop checks the new transcoding contract and production assets, with
separately bound actual-service desktop and phone captures. It preserves the full
administrator scope as the default and requires two clear rounds with the same
scope, source and production entry. Source inspection alone cannot satisfy it.

Round 1 passed seventeen automated cases and reviewed all 66 actual PNGs plus the
fixture PNG. It found two presentation issues: untranslated Priority summaries
and switch thumb/track appearance differing from the reference. Both were fixed
in three presentation files. Actual enabled/disabled switch visuals now match the
reference, with scoped 44px input wrappers; mobile field/source-control heights
are also separately checked.

Rounds 2 and 3 each passed all seventeen automated cases, with no failures or
skips, and reviewed all 64 fresh actual PNGs plus the fixture PNG. Their own actual
UI phases each passed eleven check groups. Every assigned original desktop/mobile
PNG was inspected and its hash checked. Partial and image-limit-interrupted
reviews were not counted; fresh reviewers supplied complete coverage. Both rounds
recorded no new modification recommendations. Their source/reference/asset
fingerprint is
`3da8c198d57a5c6858f2453a96d2b6f47fa4d75c64d9842029c34512b16cb04f`.
The loop journal and final status check confirm the two-round exit condition.

The visual scope is all nine runtime panels and their changed interactions,
including long path/code wrapping, source and current/default explanations,
unavailable/disabled preferences, error correction and sticky save bars. Adjacent
unchanged output-limit content at a slice edge does not imply target truncation.
The broader unsupported native editor surfaces are capability decisions, rather
than simulated controls or recurring unresolved visual recommendations.

Raw logs, PNGs, manifests, successful/unsuccessful receipts and the loop journal
are retained outside the repository at
`C:\Users\moooyo\.codex\tmp\goby-emby-transcoding-20261107`.

The final task-owned application, PostgreSQL cluster and temporary WSL holder
were retired after verifying that no other PostgreSQL client remained. The
previous preview was already absent. No private compiler cache was created;
shared caches, immutable binaries, source, databases, media and raw evidence
remain retained. The outside-repository final closeout records the completed
process retirement and accepted loop status.
