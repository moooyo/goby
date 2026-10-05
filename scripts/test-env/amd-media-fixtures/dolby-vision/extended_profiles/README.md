# Extended Dolby Vision input fixtures

These files prepare separate Profile 5, 8.4, and 8.2 input coverage. They do not
change the previously accepted Profile 8.1 or complete Profile 7 MEL fixtures.
FEL reconstruction is outside this fixture set. Generation and verification run
only on an authorized Linux worker. No downloaded media belongs in the repository.

## Two complementary sources of evidence

[`fetch_public_sources.py`](fetch_public_sources.py) downloads genuine P5 and
P8.4 SolLevante videos from Dolby's pinned public repository. The publisher's
CC BY 4.0 attribution and exact file hashes are retained; see
[`PUBLIC_SOURCES.md`](PUBLIC_SOURCES.md). The same-title official SDR master is
identified there but has not been downloaded or accepted as a pixel reference.
No real publicly licensed P8.2 sample was found in that research.

`generate.py` authors compact procedural color charts, real HEVC samples, and a
CRC-protected RPU on every frame. The scalar Python color oracle evaluates the
decoded source's swatch code values independently of Goby's renderer, FFmpeg's
DV decoder, and libplacebo's shader execution. This provides known pixel targets
that a video-only nonblack check or container profile tag cannot establish.
It is an analytic processing control, not a Dolby-certified or artistic grade.

| Profile | Original base signal | RPU and expected color evidence |
| --- | --- | --- |
| 5 | Full-range 10-bit IPTPQc2, obtained by inverting the default P5 matrices for authored BT.2020 linear RGB values | The pinned library's actual P5 header, matrices, and identity reshaping; the oracle recovers the authored colors, subject to quantization. Ordinary YCbCr with a changed P5 tag cannot do this. |
| 8.4 | Limited-range BT.2020 HLG, authored with the HLG OETF | The pinned library's iPhone 13 static polynomial and MMR mapping; scalar evaluation of that mapping produces a plain BT.2020 PQ reference. |
| 8.2 | Limited-range BT.709 SDR, authored with the BT.709 OETF | A deliberately authored analytic grade uses BT.709 YCbCr decoding, a BT.709-to-HPE linear matrix, and nonidentity component curves. The SDR samples, matrix, and mapping differ from the PQ P8.1 fixture. No real publisher P8.2 grade is implied. |

The P8.2 curve is `0.05 + 0.70*x` for luma and `0.10 + 0.80*x` for each chroma
component, with coefficients quantized to the RPU denominator `2^23`. It is not
claimed to emulate a standard SDR-to-HDR creative transform. Its purpose is to
exercise actual SDR-compatible input and a distinct, fully specified valid RPU
transform whose output is known independently.

The original 320 x 180 chart has sixteen color and neutral swatches. A bottom
12-pixel strip alternates between dark and bright once per second, leaving the
swatch centers intact and giving the product's motion gate meaningful input.
Only two raw frames are stored and looped; the default encoded movie lasts 28
seconds at 24 fps. This covers a nonzero start and the default 25-second output
without allocating a full-duration raw float video.

## Primary implementation references

- [Pinned P5 matrices](https://github.com/quietvoid/dovi_tool/blob/614c816b6446dcd1dbaf433403d499a6026fbb5a/dolby_vision/src/rpu/profiles/profile5.rs)
  and [actual P5/P8.4 RPU constructors](https://github.com/quietvoid/dovi_tool/blob/614c816b6446dcd1dbaf433403d499a6026fbb5a/dolby_vision/src/rpu/dovi_rpu.rs).
- [Pinned P8.4 static mapping](https://github.com/quietvoid/dovi_tool/blob/614c816b6446dcd1dbaf433403d499a6026fbb5a/dolby_vision/src/rpu/profiles/profile84.rs).
- [FFmpeg 9 profile and compatible-base selection](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavcodec/dovi_rpuenc.c),
  including IPTPQc2 P5, PQ compatibility 1, SDR compatibility 2, and HLG compatibility 4.
- [FFmpeg RPU coefficient and offset decoding](https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavcodec/dovi_rpudec.c).
- [libplacebo color normalization](https://github.com/haasn/libplacebo/blob/v7.349.0/src/colorspace.c)
  and [polynomial/MMR and HPE-LMS conversion](https://github.com/haasn/libplacebo/blob/v7.349.0/src/shaders/colorspace.c).

The oracle is an independently written scalar evaluation of those published
equations and stored RPU parameters. It does not invoke the product to obtain
expected colors. It uses the same published color model, so this bounded check
is not an independent proprietary Dolby reference implementation. Real-source
acceptance remains a complementary gate.

The small Rust generator depends on the same pinned `dolby_vision` library as
the older fixture set. Its MIT notice remains in
[`../THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md).

## Generation

On an authorized Linux worker with a suitable private FFmpeg, run:

```sh
python3 extended_profiles/generate.py \
  --output /path/to/new/analytic-fixtures \
  --ffmpeg /path/to/private/bin/ffmpeg \
  --ffprobe /path/to/private/bin/ffprobe
```

The destination must not exist. `--profiles 5 8.4 8.2` is the default; select a
subset when debugging. `--seconds` accepts 2 through 60 and defaults to 28.
Generation preserves raw command outputs, both actual probes, original and
decoded swatch values, complete per-file SHA-256 values, and RPU JSON. It checks
actual output profile/layer flags, P8.2/P8.4 transfer/primaries/matrix tags, the
number of actual RPU NALs, and source pixel fidelity before publishing a manifest.

By default Cargo builds the pinned small generator into the new destination.
On a constrained worker, `--rpu-generator /path/to/prebuilt/goby-extended-dovi-rpu`
uses an explicitly provided binary instead. Record its compiler, dependency,
source, and executable hashes. The 2026-10-05 run reused read-only Rust 1.88 and
the already pinned dependency rlibs, producing only the new small executable;
it did not create another Cargo target or modify prior fixtures/toolchains.

## Manifest and color comparison

`manifest.json` has a `profiles` map keyed by `5`, `8.4`, and `8.2`. Each entry
includes these fields:

- `movie`: full path to the original DV MP4.
- `reference`: two raw `gbrpf32le` BT.2020 PQ frames at 1 fps, intended to loop.
- `reference_movie`: a 28-second plain PQ HEVC reference with no RPU. Its color
  samples come from scalar evaluation of the decoded original chart centers.
- `reference_luminance`: mastering minimum 0 nits, maximum 1000 nits, MaxCLL 1000,
  MaxFALL 400. Those source bounds match the authored DV display-management data.
- `swatches`: a JSON array containing each index, center coordinates, original
  code values, decoded code values, and expected normalized BT.2020 PQ RGB.
- `configuration`: the actual FFprobe DV configuration record.
- `corruptions`: missing-RPU and bad-CRC copies, each damaged at frame 36 (1.5 s).
- `sha256`: mapping from every artifact basename to its full SHA-256.

Use an 8 x 8 center ROI per swatch, scaled to the actual output geometry. The
strip begins at source row 168; no center ROI should include it. Compare the
actual background output to the ordinary PQ reference rendered with the same
explicit SDR output range, BT.2390 settings, and disabled peak detection, with
DV processing disabled only for the reference. This isolates the correctness of
DV signal interpretation while sharing the final display mapping. Decoder,
subsampling, and output-codec quantization require a small documented tolerance.
Do not compare a plain PQ reference with a different nominal peak or automatic
peak detection and then attribute tone-map differences to the DV transform.

The reference is not an independently graded SDR movie, and the two official
P5/P8.4 editions are not independent SDR references for each other. Keep these
limits explicit when reporting product acceptance.
