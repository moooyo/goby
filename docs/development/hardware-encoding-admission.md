# Exact hardware encoding admission

Status: selected unit, actual AMD tuple and real HTTP fallback scopes passed;
the phase 1 regression, builds and closeout are complete in their recorded scope. See the
[phase 1 execution record](amd-media-phase1-20260919.md).

VAAPI capability enumeration does not establish that an encoder emits an exact
requested output. The AMD worker exposed a concrete example: AV1 encoding could
exit successfully while padding a requested picture to a different canvas. The
server now resolves this before registering a new immutable output revision.

## Admission contract

After authentication, source authorization, playback-reference validation,
subtitle access checks and output planning, the server checks the exact codec,
profile, bit depth, width, height, frame rate and bitrate tuple. Every adaptive
HLS rendition is checked. A rejected rendition changes the entire new plan to
the same codec's software encoder; output dimensions and client-facing format
are preserved. The fallback is advertised only when that software implementation
is compiled into the current FFmpeg toolchain. Unsupported fallback availability
declines the output before any conversion revision or job is registered.

HEVC VAAPI output uses `hev1` to preserve its authoritative in-band parameter
sets; the software x265 path uses `hvc1`. Encoder fallback updates both projected
codec-tag fields and rechecks the original client conditions for every rendition.
A client requiring `hev1` cannot receive an incompatible `hvc1` fallback. The
private validation snapshot contains only detached, bounded evaluation inputs;
later request mutations cannot weaken it. Dynamic producer reauthorization keeps
the framing selected by its immutable revision.

The check generates three synthetic red/blue frames and performs VAAPI encoding.
FFprobe must report the exact codec, profile, decoded pixel format, dimensions
and three frames. Strict software decoding must produce the expected spatial
color pattern in all three frames. Error-level stderr, failed execution,
incomplete frames, changed tool/device identity and silent output padding all
reject admission. No user media or client-provided filter expression is used.

This proves only this small encoder tuple. It does not prove source decoding,
scaling, deinterlacing, HDR processing, subtitle composition, rate-control
accuracy, performance, or all hardware formats. `Hardware.Verified` remains
false. GPU decoding and Vulkan processing retain their required device when
only the encoder falls back. Software processing is used for a VAAPI VPP-only
configuration that cannot retain a device-bearing codec stage.

## Bounds and lifecycle

- Each server has one admission process slot and at most 128 tuple entries.
  Equal concurrent requests share the completed result; different tuples also
  serialize through this slot.
- A new plan has a six-second admission budget, including time waiting for the
  slot. Each synthetic check has a five-second deadline, three video frames,
  one CPU/filter thread, a 256 MiB single-allocation limit, 8 MiB encoded output,
  16 KiB FFprobe output, and 576 bytes of decoded RGB evidence. Process groups
  are terminated on cancellation; bounded stderr uses the media process limit.
- Usable results expire after 15 minutes; rejected results expire after one
  minute. LRU eviction bounds retained tuples. Cancelled callers do not create
  negative cache entries, and an individual probe timeout remains uncached.
  Tool/DRM/driver and relevant library identities are
  part of the cache key; identity is rechecked after execution.
- Software encoder enumeration is cached separately as one bounded set for the
  current tool identity. It shares the same process slot and plan deadline.
- HEAD never starts a synthetic encoder or capability enumeration. A new HEAD
  can use cached evidence, use a known software fallback, or decline an unknown
  output. Existing HLS and dynamic revisions retain their admitted plan.
- Dynamic reauthorization still replans current source facts and permissions.
  It can match the precise original software fallback without probing again or
  promoting an existing software job after hardware support changes.

## Verification targets

Unit tests cover exact output evidence, nonblank pattern checks, all adaptive
renditions, software fallback contracts, GPU device preservation, bounded cache
behavior, cancellation, HEAD and permission denial. Route tests cover ordinary
PlaybackInfo, progressive HTTP, manual HLS and dynamic revision reauthorization.

The optional remote hardware test uses `GOBY_FFMPEG`, `GOBY_FFPROBE`,
`GOBY_VAAPI_DEVICE` and `GOBY_VAAPI_VIDEO_CODECS`. A worker whose AV1 padding was
independently established can additionally set `GOBY_VAAPI_EXPECT_AV1_PADDING=1`
to require explicit rejection of 320x180 and 318x190 AV1 outputs. These tests
must run only on the authorized remote environments. Passing an admission test
does not close the broader AMD media acceptance gates.

`TestHTTPHardwareAV1PaddingFallsBackToExactSoftwareOutput` uses the actual
default admission probe, PostgreSQL and standard PlaybackInfo/GET routes.
It requires `GOBY_TEST_VAAPI_DEVICE` and the distinct explicit test marker
`GOBY_TEST_VAAPI_EXPECT_AV1_PADDING=1`. On the sampled AMD worker, it passed:
hardware padding produced `hardware_encoding_output_mismatch`, the immutable
plan selected software AV1, and actual output retained Main 8-bit, 320x180 and
48 decoded frames. Stop retired its sessions and cache readers. This result
does not broaden the measured hardware geometry to unsupported canvases.
