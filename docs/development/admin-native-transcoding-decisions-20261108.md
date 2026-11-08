# Selected native transcoding controls

The product owner selected the following scope on November 8, 2026 after the
preceding native transcoding increment and its visual closeout.

| Decision | Selected scope |
| --- | --- |
| Automatic throttling | Confirm native behavior, then expose one automatic enable switch. Buffer policy remains internal; no advanced threshold editor. |
| Independent extraction threads | Confirm the native execution scope, then implement the independent native setting and actual consumers. Do not confuse threads with concurrent extraction jobs. |
| Deinterlace methods | Retain existing automatic processing. Do not add a method selector or new hardware deinterlacing paths. |
| CPU Bt2390 | Retain the existing Hable, Reinhard and Mobius implementations. Do not add a new CPU backend. |
| Tone refinement | Retain algorithm and height controls. Do not add temporary or persistent parameter/desaturation editors. |
| Codec profile limits | Add limits only for existing AVC baseline/main/high and HEVC main/main10 outputs. Apply eligible-profile restrictions without claiming new encoder or pixel-format support. |
| x264 parameters | Retain the existing seven validated options. Do not open arbitrary fragments or extend the whitelist in this increment. |
| Additional hardware and encoders | Retain current CPU and AMD paths. Do not add vendor interfaces, diagnosis UI or combination qualification. |
| Native advanced subtitle/FFmpeg details | The owner specifically selected subtitle format conversion and preservation of ASS styling. Prioritize the confirmed native AllowAssToText behavior through a fixed form; do not build the generic editor or add tool paths, environment controls, appearance, OCR or high-frame-rate options. |

The excluded Design 2 product extensions remain excluded: restore comparisons,
automatic updates and channels, pre-install backups, collection extensions,
exact skipped counts, metadata source ordering and NFO/image writeback, and a
fixed 24-hour rollback promise. Plugin-only functionality remains out of scope.

## Implementation and verification boundaries

Native declarations alone do not establish extraction scope, buffer units or
pause/resume behavior. Use the isolated reference's actual media requests,
encoder arguments, process/output observations and configuration readback to
confirm those behaviors before exposing functional settings. Attribute runtime
observations to the inspected native server version.

Preserve prior managed values, execution captures and admitted playback choices.
Existing archive/schema versions and published SQL files must remain intact.
Current profile ceilings high/main10 represent the full existing executable
profile range. Direct/copy eligibility is separate from new encoding restrictions.

The local build/test/review authorization in the original administrator task
continues for this selected increment. Root coordinates PostgreSQL verification,
new catalog generation, final production builds, actual runtime evidence, and
two consecutive clear visual rounds on the same source and production entry.
Earlier successful transcoding checks remain historical evidence and do not
substitute for verification of these new controls.

Reuse shared build/module caches. Keep compiler scratch separate from retained
media fixtures and evidence, and preserve the selected ext4 fixture filesystem.
Stop task-owned application/reference processes and reclaim inactive private
compiler output at closeout; retain source, data and raw evidence separately.
