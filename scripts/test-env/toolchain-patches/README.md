# Private FFmpeg toolchain patches

## Strict Dolby Vision and zero-residual MEL

`ffmpeg-libplacebo-strict-dovi-mel.patch` is a private, opt-in patch for
FFmpeg `n9.0.1`, commit `bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa`, built with
libplacebo `7.351.0`, commit `3188549fba13bbdf3a5a98de2a38c2e71f04e21e`.
It changes only `libavfilter/vf_libplacebo.c`; the upstream
`LGPL-2.1-or-later` license notice remains intact. It is not an upstream patch
or a claim of general Dolby Vision enhancement-layer support.

Enable it only for a plan that has established the HEVC Dolby Vision
configuration profile. For example, the profile 7 portion of a filter is:

```text
libplacebo=apply_dolbyvision=1:strict_dolbyvision=1:strict_dolbyvision_profile=7
```

Both new options are static. `strict_dolbyvision` defaults to `0`, preserving
upstream behavior. `strict_dolbyvision_profile` defaults to `0`; strict mode
requires an explicitly declared profile of `5`, `7`, or `8` and requires
`apply_dolbyvision=1`. Every input frame, on every input pad, must satisfy the
strict contract. Mixed DV/non-DV input is rejected.

### Admission and normalization

Before a frame enters the libplacebo queue, the patch requires both
`AV_FRAME_DATA_DOVI_RPU_BUFFER` and `AV_FRAME_DATA_DOVI_METADATA`. It checks the
raw HEVC RPU prefix, type, format, sequence-header presence, and agreement of
its profile/level with the parsed header. Native buffer ownership, complete
metadata regions, non-overlapping offsets, alignment, extension-block bounds,
reshaping curve bounds, mapping methods/orders, rational denominators, and PQ
ranges are checked before libplacebo can access the metadata.
The native `av_dovi_metadata_alloc()` layout reserves the extension-block
array and a nonzero stride even when `num_ext_blocks` is zero; the bounds
checks accept that empty native layout without reading an extension block.

The strict subset is ordinary HEVC RPU type 2, format 18, RPU level 0,
10-bit BL/EL, normalized-idc 1, fixed coefficients with denominator 13 through
30, and no extended mapping or explicit chroma resampling filters. The fixed
coefficient restriction deliberately excludes floating-point RPUs, which
FFmpeg converts to denominator 32: libplacebo 7.351 uses a signed 32-bit
`1 << coef_log2_denom` while mapping coefficients. Denominators 31 and 32 are
therefore rejected instead of entering undefined arithmetic. This is a
deliberately bounded private implementation, not a new definition of valid DV.

Profile classification follows FFmpeg's `ff_dovi_guess_profile_hevc()` header
rules and must equal the declared configuration profile. Profile 5 and 8
frames pass only with `disable_residual_flag=1` and `AV_DOVI_NLQ_NONE`.
The filter does not reinterpret a profile 8 RPU as profile 7 merely because
the container was labeled profile 7.

For profile 7, the original parsed header must still report an enhancement
layer: `disable_residual_flag=0`,
`el_spatial_resampling_filter_flag=1`, and 12-bit VDR depth. The NLQ method
must be `AV_DOVI_NLQ_LINEAR_DZ`. Each of all three components must satisfy:

```text
nlq_offset == 0
vdr_in_max == (UINT64_C(1) << coef_log2_denom)
linear_deadzone_slope == 0
linear_deadzone_threshold == 0
```

FFmpeg stores NLQ values as fixed-point integers, including the integer part;
`vdr_in_max == 1` would be incorrect. The predicate corresponds to
`dovi_tool`'s MEL test and libplacebo's later trivial-NLQ test. FEL and unknown
residuals return `AVERROR_INVALIDDATA`.

Only after that predicate succeeds does the filter allocate a new buffer and
copy every byte of the original metadata side data. The consumed `AVFrame`
belongs to this filter, but the old side-data buffer may still be referenced
elsewhere. Replacing this frame's `sd->buf` and `sd->data` isolates the changes
without copying video planes or modifying other references.

The private copy changes only residual-related fields: it enables
`disable_residual_flag`, clears `el_spatial_resampling_filter_flag`, sets NLQ
to `NONE`, sets both partition counts to one, and clears the unused NLQ
parameters/pivots. The original reshaping curves, mapping colorspace, color
matrices, offsets, display-management extensions, and raw RPU bytes are
preserved. No hardcoded profile 8 matrix is substituted.

libplacebo clones the normalized frame during `pl_map_avframe_ex()` and owns
that reference until `pl_unmap_avframe()`. The existing queue mapping and
discard callbacks retain their ownership roles. After a successful mapping,
strict mode requires `PL_COLOR_SYSTEM_DOLBYVISION` and a non-null DV metadata
pointer. A silent DV skip is unmapped and returned as a queue/filter error.

### Why raw RPU presence is required

FFmpeg's HEVC decoder attaches parsed DV metadata from persistent decoder
state even when the current access unit has no RPU. Checking parsed metadata
alone could therefore accept a missing-RPU frame. It also returns success
without updating that state for an unknown RPU type. Requiring the raw RPU
and its supported type closes both cases for the supported software HEVC
decoder path. That decoder exports both side-data types by default; no extra
`export_side_data` flag is required. The patch does not perform a second full
bitstream parse or replace the decoder's parsing responsibility.
Raw side data retains NAL emulation-prevention bytes. For the supported
type-2, format-18 layout, its first three bytes are `19 08 09` in hexadecimal,
so no emulation-prevention byte can occur before any of the first five bytes
inspected here. Later bytes are left to the decoder's normal unescaping and
RPU parsing path.

### Provenance

- [FFmpeg n9.0.1 libplacebo filter](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavfilter/vf_libplacebo.c)
- [FFmpeg metadata layout and accessors](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavutil/dovi_meta.h)
- [FFmpeg native metadata allocation](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavutil/dovi_meta.c)
- [FFmpeg HEVC profile classification](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavcodec/dovi_rpu.c)
- [FFmpeg RPU coefficient parsing and persistent metadata attachment](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavcodec/dovi_rpudec.c)
- [FFmpeg per-access-unit raw RPU attachment](https://github.com/FFmpeg/FFmpeg/blob/bf1b838f2ab88b4f8fd83443325c782ea0e0f7fa/libavcodec/hevc/hevcdec.c)
- [Pinned libplacebo mapping and frame lifetime](https://github.com/haasn/libplacebo/blob/3188549fba13bbdf3a5a98de2a38c2e71f04e21e/src/include/libplacebo/utils/libav_internal.h)
- [dovi_tool 2.3.4 MEL predicate](https://github.com/quietvoid/dovi_tool/blob/614c816b6446dcd1dbaf433403d499a6026fbb5a/dolby_vision/src/rpu/rpu_data_nlq.rs)
- [dovi_tool profile header validation](https://github.com/quietvoid/dovi_tool/blob/614c816b6446dcd1dbaf433403d499a6026fbb5a/dolby_vision/src/rpu/rpu_data_header.rs)
- [Later libplacebo trivial-NLQ predicate](https://github.com/haasn/libplacebo/blob/e2972fdd09adacd383656738d7d280f0cd84a761/src/include/libplacebo/utils/libav_internal.h#L923)

### Integration and verification status

Historical authoring note: no patch-application check, compiler, formatter, test,
or runtime probe was run locally when this patch was first written. At that time,
the current toolchain installation and active remote build were unchanged.

The patch was subsequently integrated into the isolated `amd-media-v2` recipe.
Phase 1 recorded actual chart checks for profile 8.1 and the complete single-track
profile 7 MEL fixture, including SDR and HDR10 outputs. See the
[accepted profile material and color checks](../../../docs/development/amd-media-phase1-20260919.md#complete-profile-7-mel-material-and-color-checks)
and [retained scope](../../../docs/development/amd-media-phase1-20260919.md#scope-retained-after-closeout).
Those results do not expand actual chart acceptance to profile 5, arbitrary
profile 8 input, FEL reconstruction, or Dolby color certification. Profile/layer
admission and malformed, missing, mismatched, or unsupported RPU rejection remain
part of the strict contract. Successful process exit alone is not proof of correct
reconstruction or tone mapping.

The successor recipe retains this patch unchanged, records its digest, and uses a
new immutable prefix. Its independent scheduler patch below does not alter Dolby
Vision admission or rendering semantics.

## Decoder queue receive wakeup

`ffmpeg-decoder-queue-wakeup.patch` is a separate patch for the same pinned FFmpeg
source. It changes only `fftools/thread_queue.h`, `fftools/thread_queue.c`, and
`fftools/ffmpeg_sched.c`; upstream license notices remain intact.

A choked decoder can buffer packets in its overflow FIFO and then wait on an
empty incoming thread queue. When the graph selects that decoder again,
`waiter_set()` originally signaled a different condition variable. If the common
demuxer remained unchoked and no new packet arrived, the decoder never returned
to its already buffered packets. A bare condition broadcast would also be
insufficient: the queue's internal empty-queue loop would wait again.

The repair stores a coalesced receive-interruption bit under the queue mutex and
wakes the receiver. `tq_receive()` returns an internal `EINTR` before consuming
packet or EOF state; `sch_dec_receive()` retries its existing overflow check.
Unchoke and scheduler stop notify both wait domains. No synthetic media packet,
timestamp adjustment, EOF, periodic polling, or extra decoder is introduced.
The existing scheduler-to-queue lock order and decoder-owned FIFO are preserved.

The [native regression gate](decoder-queue-wakeup/README.md) checks exact packet
order, the notification-before-wait race, normal EOF, cancellation, and buffer
release. Its original-source negative control must reproduce four bounded
deadlocks, while the patched source must pass all eight cases. The private
candidate additionally passed the original dual-input AMD live path with saved
identical prefixes, both TS/fMP4 outputs, precise subtitle pixels and clocks, and
empty-stream cancellation. See the
[phase 2 candidate acceptance](../../../docs/development/amd-media-phase2-20260919.md#decoder-queue-wakeup-candidate-acceptance).
These selected results do not by themselves accept a later recipe build or the
entire phase.

`amd-media-v3` hashes both patches and the native harness build inputs into the
recipe identity. It applies strict Dolby Vision first, retains a complete
baseline source, applies the queue patch, and builds out of tree. Publication
requires both native contracts. Metadata retains both patches, hashes of all
four changed source files before and after patching, the strict-only baseline
hashes, native logs/receipts, and the actual dependency versions. A changed recipe
gets a successor prefix; existing prefixes are never overwritten.

`GOBY_TOOLCHAIN_SKIP_PACKAGE_INSTALL=1` checks that every required build package
is already installed and records its actual version, without running apt update
or installation. Missing packages fail explicitly. The default `0` retains the
existing package setup behavior. Both modes rebuild private libplacebo and FFmpeg
from verified sources; neither imports earlier private objects or changes GPU
drivers as part of the patch.
