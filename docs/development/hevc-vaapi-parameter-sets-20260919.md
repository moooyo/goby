# HEVC VAAPI parameter-set compatibility

This record covers the phase 1 HEVC MP4 correction on September 19, 2026.
It is a focused diagnosis, not acceptance of the complete media phase.

## Output contract

HEVC produced by VAAPI uses the `hev1` sample entry in progressive MP4 and
fragmented MP4 HLS. This preserves the encoder's in-band parameter sets.
Copied HEVC also uses `hev1`. Software `libx265` output retains `hvc1`, with
in-band parameter-set repetition explicitly disabled for MP4.

The selected sample entry is derived from the final plan through
[`VideoMP4Tag`](../../internal/transcode/video_codec.go). Server projections and
HLS codec signaling must use that same final plan, including after an encoder
fallback. Codec, profile, bit depth, exact dimensions, bitrate bounds, GOP
boundaries, and output decode checks remain unchanged.

`hev1` and `hvc1` are not interchangeable labels. FFmpeg's
[`movenc.c`](https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/movenc.c)
sets `filter_ps` for `hvc1`, removing in-band HEVC parameter sets during
Annex-B-to-MP4 conversion. A client requiring `hvc1` cannot be promised a
VAAPI `hev1` stream solely because it accepts the HEVC codec.

## Device-bound evidence

The diagnosis ran inside the designated test guest, CT 104, against
`/dev/dri/renderD128`: AMD PCI ID `1002:150E`, `gfx1150`, `amdgpu`, Mesa
`25.0.7-2+deb13u1`, libva `2.22.0-3`, and kernel `6.17.13-3-pve`.
The FFmpeg build was `9.0.1-goby-65af9bed5365`, executable SHA-256
`b5f635e5f89a397fbf16d6a88ba8a1f4789f35d17c5f0d1cb25d7b072cfef2b7`.
Each diagnostic command ran as the worker account in its own systemd unit
with one CPU, 512 MiB memory, no swap, and a 60-second runtime limit.

The retained source was a moving `testsrc2` pattern, 320 by 192 pixels, at
12 FPS for three seconds. HEVC Main 10 output selected the final 2.5 seconds,
30 frames, with a 768000-bit/s target and maximum, and a 1536000-bit buffer.

| Encoding variant | Decoder result |
| --- | --- |
| Product arguments with `hvc1` | CABAC and invalid `cu_qp_delta` errors |
| Product arguments with only the tag changed to `hev1` | 30 decoded frames; no probe or strict-decode diagnostics |
| Standard VAAPI arguments with `hvc1` | Invalid `cu_qp_delta` errors |
| Standard VAAPI arguments with `hev1` | 30 decoded frames; no probe or strict-decode diagnostics |
| Product arguments, explicit VBR, unchanged bitrate ceiling, `hvc1` | Same decoder errors |
| Product arguments with inline headers permitted and `hev1` | 30 decoded frames; no probe or strict-decode diagnostics |

An independent `trace_headers` inspection established the actual conflict:
the initialization PPS had `init_qp_minus26=4` and
`diff_cu_qp_delta_depth=3`, whereas the driver's first in-band PPS had
`init_qp_minus26=0` and `diff_cu_qp_delta_depth=0`. The `hvc1` muxing path
removed the latter. The `hev1` path retained it and decoded correctly.

Raw commands, return codes, timings, probe output, encoder logs, and header
traces remain in CT 104 at
`/opt/goby-amd-media-20260919/codec-hevc-diagnostic04/`, with the command index
in `result.json`. This evidence does not establish that every VAAPI driver
has the same discrepancy. The product uses `hev1` for VAAPI HEVC because its
contract permits the real parameter-set placement without changing content
or weakening bitrate requirements.
