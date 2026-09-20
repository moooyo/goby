# Analysis display geometry profile

`orthogonal-display-sar-v2` reads private geometry facts from an authorized open
descriptor. It does not alter the general media probe version or persist new
fields in `Info`. The selected stream index, coded dimensions, and time base
must agree with the caller's indexed stream facts.

The descriptor-based FFprobe process is pinned to version 9.0.1 and, when
provided, the admitted executable SHA-256. The held executable is executed
through its inherited descriptor. The source descriptor remains open through
probing and extraction; both source and executable identities are rechecked.
The geometry JSON has a 64 KiB bound, at most one selected stream, at most 16
side-data entries, and a 512-byte display matrix. Stream analysis is configured
with `probesize=8 MiB`, `max_probe_packets=256`, and `analyzeduration=10 s`.
Duplicate JSON keys, invalid Unicode, and trailing documents are rejected.
These do not claim to cap all container-header I/O. The admitted source-file
size, one decoder thread, process memory limit, and at most 30 seconds of wall
time within the extraction deadline bound the probe as a whole. The common
descriptor, stderr, cancellation, and process-group retirement bounds apply.

The profile accepts a missing display matrix as identity and accepts only the
four exact fixed-point orthogonal rotation matrices. It rejects reflections,
scaling, shear, translation, perspective, damaged matrices, duplicate matrices,
inconsistent derived rotation values, and unproven legacy rotation tags.
Positive, finite sample aspect ratios are retained as exact rationals, including
common 16:15 and 64:45 sources. A missing ratio, `N/A`, or `0:1` remains an
explicitly unknown source ratio. The display policy uses square pixels for these
cases, following FFplay's [unknown-ratio display fallback](https://github.com/FFmpeg/FFmpeg/blob/n8.0/fftools/ffplay.c#L860).
This is a rendering default, not a claim that the source declared `1:1`.
Null, empty, malformed, negative, and zero-denominator values remain invalid.

Automatic rotation is disabled. Proven rotation is applied explicitly using
`transpose=clock`, `hflip,vflip`, or `transpose=cclock`. The source-frame audit
checks the resulting raster dimensions and SAR on every observed frame. An
unknown source ratio must remain `0/1` throughout that audit; a transition to
a declared ratio is rejected. A
quarter turn swaps both coded dimensions and the SAR numerator/denominator.
Decoded and rotated intermediate rasters use `MaxSourcePixels`; final visual
and preview rasters use `MaxFramePixels`.

Selected preview frames are scaled to the admitted width and the nearest
integer height computed from the exact rotated display aspect ratio. The final
`setsar=1` produces square pixels. No rounded intermediate normalization raster
is used, and discarded source frames are not rescaled. The fixed 32 by 32 visual
hash raster uses the same rotation policy and final square-pixel normalization.
An extreme aspect ratio that cannot produce a positive bounded output height
fails the pixel budget.

The matrix direction and signed angle agreement follow the pinned FFmpeg
[display matrix implementation](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavutil/display.c),
[rotation helper](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/cmdutils.c),
and [explicit filter mapping](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg_filter.c).

## Full-duration diagnostic budget

The streaming stderr sink retains only bounded line and timestamp state; the
cumulative byte count is still a hard resource budget. Streaming does not make
that cumulative cost disappear.

For an ordinary fully decoded two-hour, 24 fps source, 172,800 source frames
must be audited. The pinned `demuxer`, `demuxer+tsfixup`, `decoder`, `showinfo`,
and color-property records contain at least 449 fixed field characters per
frame. This excludes every field value, address, log-level prefix, and other
debug record. Those fixed characters alone require at least 73.99 MiB, so a
64 MiB diagnostic limit cannot complete that full-duration case. This is a
static lower bound for one pass, not a runtime measurement or a throughput
claim. Each child remains capped at 256 MiB. The two-pass preview operation has
an explicit shared 512 MiB default and hard maximum, counting both passes
without retaining their complete logs. The representative two-pass estimate and
hold-frame contract are in [EXTRACTION.md](EXTRACTION.md). Both caps remain
enforced; higher-rate or longer media can still end with an explicit
resource-budget failure.

Source references for that bound are the pinned
[demuxer](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg_demux.c),
[decoder](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg_dec.c), and
[showinfo filter](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavfilter/vf_showinfo.c).
