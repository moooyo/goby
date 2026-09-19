# Video bit-depth facts

Status: phase 1 implementation; integrated remote verification is required.

`media.Stream.BitDepth` preserves the sample-depth value reported by FFprobe.
The raw-sample field takes precedence over the sample field, and absent or
unknown reports remain zero in the catalog. The parser and stored metadata do
not substitute a value from the codec name, profile, or pixel format.

HEVC and AV1 decoders can omit those sample-depth fields while reporting an
exact decoded pixel format. `media.EffectiveVideoBitDepth` supplies a separate
read-only interpretation for compatibility checks and public projections:

| Eligible video pixel format | Effective depth when the sample report is zero |
| --- | --- |
| `yuv420p`, `nv12` | 8 |
| `yuv420p10le`, `yuv420p10be`, `p010le`, `p010be` | 10 |

This fallback applies only to HEVC/AV1 video streams, excluding attached
pictures. Other formats, partial format names, hardware surface names, and
profile names do not establish precision. H.264 retains its existing reported
sample-depth contract. Audio continues to use its reported sample depth.

A nonzero sample report is preserved and is never overwritten; invalid values
are not repaired by the fallback. When a positive report conflicts with an exact eligible
pixel formats, `media.VideoBitDepthConflict` records that contradiction:
required compatibility conditions cannot treat either value as proved, and
video copy or processing is declined. The DTO retains the reported value and
pixel format so the contradiction is not silently repaired. Copied HLS output
does not advertise a precision constraint for such inconsistent facts.

The same effective interpretation is used by video profile conditions,
explicit progressive copy precision requests, MediaStream DTOs, copied HLS
URL signaling, and ten-bit GPU input selection. Packet-copy seek proof retains
its separate narrower pixel-format and runtime evidence requirements; adding
metadata interpretation does not widen that proof contract.

Neither an eight-bit nor a ten-bit result establishes SDR/HDR colorimetry,
Dolby Vision validity, codec profile support, or GPU execution. Those facts
retain their independent checks. Output encoding specifies its own measured
or controlled precision rather than changing the original source metadata.

Regression coverage includes probe-shaped missing reports, unknown formats,
reported-value precedence, conflicting declarations, unchanged catalog
round trips, HEVC/AV1 compatibility/copy planning, and GPU input selection.
The real HLS HTTP tests require an eight-bit codec condition, inspect the
actual probed source, check DTO and URL precision, and reread the catalog to
ensure public projection did not rewrite the stored report.
