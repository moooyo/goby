# Real bitmap subtitle fixtures

`bitmap-subtitle-fixtures.py` authors actual PGS display sets and DVD SPU control
packets from a pinned font. It never samples a video at a fixed frame rate. Use
it on the authorized `test-env` host after the phase's implementation is ready
for consolidated verification. Generation alone is not an OCR acceptance result.

## Inputs and font provenance

The script requires Python 3.10 or later, Pillow, an absolute existing font
filename, the operator's independently recorded lowercase SHA-256 for that file,
and an absolute output directory that does not yet exist. Its parent must exist.
The script does not download dependencies, invoke FFmpeg, overwrite prior
attempts, or copy the font into the fixture directory.

Use upstream Noto Sans CJK SC Regular from the `Sans2.004` release commit
`523d033d6cb47f4a80c58a35753646f5c3608a78`:

- [Pinned font download](https://raw.githubusercontent.com/notofonts/noto-cjk/523d033d6cb47f4a80c58a35753646f5c3608a78/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Regular.otf).
- [Pinned upstream font identity](https://github.com/notofonts/noto-cjk/blob/523d033d6cb47f4a80c58a35753646f5c3608a78/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Regular.otf):
  Git blob `dc15562470b4f842321894787a0d066879ccff8b`, 16,437,364 bytes.
- [Pinned SIL Open Font License 1.1](https://github.com/notofonts/noto-cjk/blob/523d033d6cb47f4a80c58a35753646f5c3608a78/LICENSE).

The script checks both the caller's SHA-256 and the upstream Git blob identity.
The caller must retain the font's source, digest, copyright information, and
license with the separately provisioned font. No downloaded font is committed
to this repository. The output consists of rendered text documents; it does not
redistribute or modify the font software.

Run in a fresh owned verification directory:

```sh
python3 scripts/test-env/bitmap-subtitle-fixtures.py \
  --font-file /owned/tools/NotoSansCJKsc-Regular.otf \
  --font-sha256 VERIFIED_LOWERCASE_SHA256 \
  --output-dir /owned/bitmap-fixtures-attempt-01
```

The output directory is private. The script writes `manifest.json` last. A
failed attempt is left intact without a completeness manifest. Preserve the
manifest, tool/model digests, and subsequent verification output as evidence.

## Corpus

| Case | Text | Models | Formats |
| --- | --- | --- | --- |
| `english` | `Hello world` | `eng` | SUP, PGS Matroska, DVD Matroska/SPU |
| `chinese-forced` | `中文测试` | `chi_sim` | SUP, PGS Matroska, DVD Matroska/SPU |
| `chinese-traditional` | `中文測試` | `chi_tra` | SUP, PGS Matroska, DVD Matroska/SPU |
| `overlap` | Simultaneous English and Simplified Chinese, then a repeated English cue | `eng` + `chi_sim` | SUP, PGS Matroska, DVD Matroska/SPU |
| `mixed-forced` | Ordinary English and forced Simplified Chinese in one display set | `eng` + `chi_sim` | SUP and PGS Matroska |

All containers are self-contained, single-subtitle-track Matroska files. PGS
also has a raw `.sup` file with real `PG`/PTS/DTS headers. DVD also has every raw
`.spu` packet and `dvd-codec-private.txt`, containing the actual palette.
These files can be copy-muxed with a separately generated tiny media source for
the stage's application E2E; the standalone OCR acceptance needs no mux step.

The canvas is 720 by 576. Display boundaries are at 0, 1.024, 2.560, 4.096, 5.632,
6.656, and 8.704 seconds where applicable. The declared container duration is
9.728 seconds. Every DVD duration is exact in the 1024/90000-second control
clock; no video-frame sampling or timing tolerance is used. Initial and final
PGS clears are real presentation events. DVD stop commands end each visible
interval. Overlap is a real two-object PGS composition and a single composited
DVD bitmap, not two unrelated SPU packets incorrectly assumed to coexist.

DVD has one forced bit per subpicture. The `mixed-forced` case deliberately has
no DVD equivalent because that format cannot assign distinct forced bits to
simultaneous objects. The manifest records this limitation explicitly. Its
single Chinese forced case verifies DVD's actual forced control command.

The admitted demuxer reports an unknown format origin for these DVD-only
containers. Their first SPU packets still have exact nonzero PTS in the Matroska
segment clock. The version-two manifest records those facts separately:
`dvd_format_start_known` is false, `dvd_container_origin_ticks` is zero, and
`dvd_first_packet_pts_ticks` retains the authored first packet timestamp.
Acceptance requires the unknown origin explicitly and checks the exact original
cue times, including the initial caption-free interval. It never subtracts the
first subtitle PTS as an invented format origin. PGS containers include a clear
at zero; their explicit format origin remains known and zero.

Production OCR follows the indexed playback clock: it subtracts only an
explicit known `FormatStartTicks` and otherwise retains container packet time.
A later exhaustive packet scan cannot silently introduce a different origin.
The original version-one corpus and its failed precondition receipt remain
historical evidence; regenerate into a fresh directory for version-two checks.

The manifest contains source text, exact intervals, forced flags, coordinates,
dimensions, binary and RGBA pixel digests, file digests, and rasterizer versions.
PGS pixel expectations include the object borders. DVD expectations trim only
the transparent outer border and retain its corresponding absolute coordinate
offset; `encoded_rectangle` also records the uncropped wire rectangle. PNG
compression bytes may differ between Pillow and Go, so acceptance compares
decoded RGBA pixels and separately checks the returned PNG's own SHA-256.

## Actual OCR acceptance

Production recognition consumes real display events synchronously through a
decoder callback, recognizes one image, and releases that raster before advancing.
It retains compressed review evidence rather than every uncompressed subtitle
image in a film. The PGS working set includes its bounded object cache and at
most two pending composed images; DVD retains one active display and its crop.
The public batch decode helper has a separate cumulative retention ceiling and
is not the production recognition path.

The fixed media limits are 10,000 cues, 40,000 demuxed packets, 32 MiB of encoded
packet data, 32 megapixels in the decoder working set, and one billion pixels of
cumulative decode/render work. An individual image cannot exceed 4,096 by 2,160
or 1 MiB of review PNG. A review retains at most 64 MiB of PNG evidence and 8 MiB
of text, with 4 KiB per cue. Demuxing has a two-minute deadline, recognition has
a fifteen-minute deadline, and each engine invocation has twenty seconds.
All limits reject the candidate; none silently truncate an accepted track.

PGS supports fragmented objects, palettes, cropped/windowed composition,
overlapping objects, forced membership, and explicit clear events. Standard
two-bit DVD SPU supports interlaced RLE, coordinates, color/alpha changes, forced
display, and control-clock start/stop events. HD-DVD eight-bit subpictures and
unknown DVD commands (including region color-control extensions) fail explicitly.
A missing DVD palette uses a declared monochrome review warning; malformed
palettes fail. A missing DVD stop requires a real packet duration. A final PGS
display may close at an explicit packet or indexed source duration, with a
warning. No missing interval is replaced by a guessed fixed display length.

Empty recognition for one valid bitmap is retained with zero confidence and a
review warning, allowing a human to transcribe it. An entirely empty recognized
track fails instead of becoming an apparently complete empty subtitle.

Prepare a private JSON file matching `media.SubtitleOCRConfig`:

```json
{
  "FFprobePath": "/owned/tools/ffprobe",
  "FFprobeSHA256": "VERIFIED_LOWERCASE_SHA256",
  "TesseractPath": "/owned/tools/tesseract",
  "TesseractSHA256": "VERIFIED_LOWERCASE_SHA256",
  "ScratchDirectory": "/owned/private/ocr-scratch",
  "MaxScratchBytes": 536870912,
  "Models": [
    {"ID": "eng", "Path": "/owned/models/eng.traineddata", "SHA256": "VERIFIED_LOWERCASE_SHA256"},
    {"ID": "chi_sim", "Path": "/owned/models/chi_sim.traineddata", "SHA256": "VERIFIED_LOWERCASE_SHA256"},
    {"ID": "chi_tra", "Path": "/owned/models/chi_tra.traineddata", "SHA256": "VERIFIED_LOWERCASE_SHA256"}
  ]
}
```

Provision real supported Tesseract and language files, not shell helpers. Set
`ScratchDirectory` to an existing directory owned by the service identity with
mode `0700`. These two scratch settings can be omitted for the media-layer test's
safe defaults; the production server supplies its private job directory.
Set
both opt-in environment variables only on the authorized verification host:

```sh
GOBY_BITMAP_OCR_FIXTURES=/owned/bitmap-fixtures-attempt-01 \
GOBY_BITMAP_OCR_CONFIG=/owned/private/bitmap-ocr-config.json \
go test ./internal/media -run '^TestRecognizeBitmapSubtitlesActualFixtures$' -count=1 -v
```

The test checks nine actual format/case combinations. It opens the generated
file under a contained root, verifies its manifest digest, probes the held
descriptor, and calls the production `RecognizeBitmapSubtitles` path with real
FFprobe, Tesseract, and pinned selected model files. It verifies exact timeline
and forced membership, original pixels, transcript, positive finite confidence,
engine/model provenance, retained PNG digest, and source descriptor stability.
The required transcripts, display boundaries, and forced/overlap structure are
also asserted independently of the generated manifest, so changing generator
and manifest together cannot silently remove a required scenario.
Text comparison ignores whitespace only; it preserves case, punctuation, and
the distinction between Simplified and Traditional Chinese. A missing model,
empty transcript, changed input, or skipped required format fails acceptance.

The ordinary unit tests in `bitmap_subtitle_fixture_test.go` instead use a small
independently authored Latin pixel alphabet. They exercise SUP envelopes,
overlap, clear events, DVD field order, and control timing without installing a
font or executing media/OCR processes. They do not establish real OCR success.

This suite establishes the decoder/recognition path. The phase's broader E2E
must separately verify job ownership, review/correction, publication, playback,
cancel/restart, and backup/restore behavior through the application APIs/UI.
