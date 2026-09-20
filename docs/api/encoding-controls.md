# Managed encoding execution controls

The managed encoding settings apply to new conversion admissions. They are
resolved with the caller's current authorization, source facts, output profile
and existing bitrate/dimension ceilings. Saving settings does not mutate an
already queued or running plan. The settings API's revision is not itself an
encoding cache key.

| Control | Default | Accepted values | Consumer |
| --- | --- | --- | --- |
| Threads | Deployment value, normally 2 | Integer 1..64 | FFmpeg input, filter and output thread options; x265 frame threads remain capped at 16 |
| H264.Preset | veryfast | veryfast, fast, medium, slow | Software H.264 (`libx264`) output |
| HEVC.Preset | veryfast | veryfast, fast, medium, slow | Software HEVC (`libx265`) output |
| H264.RateControl / HEVC.RateControl | bitrate | bitrate, capped_crf | The corresponding software output codec |
| H264.CRF | 23 | Integer 18..35 | Active only for software H.264 capped_crf |
| HEVC.CRF | 28 | Integer 18..35 | Active only for software HEVC capped_crf |
| SoftwareToneMapping | true | Boolean | A selected tone-mapping graph with the software filter backend |
| VulkanToneMapping | true | Boolean | A selected Vulkan tone-mapping graph, including admitted Dolby Vision processing |

Threads is an FFmpeg setting per job, not a hard upper bound on the process's
total thread count. Controls accept closed scalar values; arbitrary FFmpeg
arguments, codec parameter dictionaries and filter expressions are not accepted.
Presence, partial/full replacement, resets and atomic revision checks belong to
the [configuration API](configuration.md).

## Rate control and output boundaries

All three argument builders share one rate-control implementation: legacy HLS,
generated HLS (including each adaptive rendition), and progressive video. For
the already resolved video budget `B`, bitrate mode preserves the existing
`-b:v B -maxrate B -bufsize 2B` arguments. Capped CRF uses
`-crf C -maxrate B -bufsize 2B` and has no simultaneous target `-b:v`.

The quality setting does not increase `B`. Existing source, client, user and
server constraints still resolve output bitrate and transport overhead, audio
reservation, dimensions, profile, bit depth, output codec and permissions. VBV
rate control is not a packet-by-packet network traffic shaper. Existing forced
keyframes, closed GOPs, segment boundaries, stream clocks and subtitle composition
remain part of the output contract.

These quality groups describe the final output codec. An AV1 source converted to
H.264 uses the H.264 group. Software AV1 output retains libaom's existing realtime,
cpu-used, row-mt, lag and bitrate policy. VAAPI output retains its existing bitrate
policy. Neither receives x264/x265 presets or CRF controls. Stored CPU settings
may be inactive while a VAAPI encoder succeeds; an authorized software fallback
captures its CPU quality from the same complete admission settings snapshot.
Existing non-AMD branches are unchanged and are not newly selectable profiles.

## Tone mapping and unavailable devices

A required disabled tone-mapping backend rejects that conversion candidate with
`conversion_tone_mapping_disabled`. It cannot strip HDR metadata and relabel
unconverted pixels as SDR. Source metadata requirements and Dolby Vision rules
are unchanged. Disabling tone mapping does not disable unrelated deinterlacing,
scaling, subtitle composition, SDR encoding, audio or compatible stream copy.

A managed hardware selection that no longer resolves to an authorized device
rejects video encoding candidates while the selected execution profile is
unavailable, including a CPU profile with an unavailable Vulkan filter device.
It does not silently switch that profile to CPU or select the default render node.
Copy and audio paths with no device dependency remain
available. The transcode manager invokes the server's bounded hardware identity
check again after a queue wait, immediately before calling the runner; failure
produces the fixed `hardware_unavailable` job code and closes owned inputs.
This identity check does not run a hardware capability probe.

## Immutable execution identity and historical plans

Current plans record `ExecutionVersion: 1` and a comparable `Execution` value
before entering the queue. The manager, FFmpeg builder and actual seek preflight
use its Threads value. A later settings save or a legacy runner argument cannot
reinterpret it. Hardware remains a separate captured plan field.

Only choices that can affect the admitted output remain in its execution
identity. Other codec groups and tone gates irrelevant to the selected graph
normalize to defaults. CRF in bitrate mode also normalizes to its codec default.
The complete stored settings remain unchanged. This lets an otherwise identical
job reuse its cache after a change to an inactive preference. Active preset,
rate-control, CRF and thread changes produce a different comparable Plan/Spec.
An admitted tone-mapping graph necessarily records an enabled applicable gate.

The entirely absent execution value is legacy version zero. It remains valid for
reading and updating historical records, and serialization omits both new fields.
It does not prove a historical thread count. Repository recovery interrupts old
queued/running records without reconstructing or restarting their work; terminal
records are not rewritten with invented defaults. A new manager admission from a
legacy caller captures the process's explicit default before creating its new
record. Unsupported versions and partially supplied unversioned contexts fail
closed.

## Verification sources

The unit sources cover closed validation, canonical cache identity, captured
threads, all output argument paths, unchanged default arguments, inactive codec
controls, actual tone-backend refusal, hardware identity checks after queue waits,
and legacy serialization. The Linux media source
`TestExecutionCPUQualityActualOutputsPreserveFramesAndSegmentClocks` uses the
configured `GOBY_FFMPEG` and `GOBY_FFPROBE` tools to encode H.264/HEVC in both rate
modes, fully decode progressive output and every HLS segment independently, and
check frame counts and continuous segment clocks. Test source is not acceptance
evidence; the phase delivery record identifies the executed scopes and results.
`TestExecutionCapturedThreadsReachActualSeekPreflight` separately records real
seek-proof and producer arguments while deliberately passing a different legacy
thread argument, then compares decoded frame/sample clocks and source content.
