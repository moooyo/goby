# PCM fingerprint protocol, version 1

The only requests are:

```text
goby-intro-fingerprint --describe
goby-intro-fingerprint --sample-rate 11025 --channels 1
```

The two PCM option pairs may appear in either order. Both are mandatory and
their values must match exactly. Duplicate options, extra arguments, filenames,
URLs, alternate algorithms, and implicit input formats are rejected. The helper
never opens a media path or starts another process.

PCM input is signed 16-bit little-endian, one channel, 11,025 samples per second.
Samples are decoded explicitly into the native byte order expected by the
Chromaprint API. The helper performs no resampling, downmixing, silence trimming,
seeking, padding, or input gain changes. Its caller owns decoding and must retain
the actual resampled PCM presentation-time mapping. A partial final two-byte
sample and empty input are errors. An aligned nonempty input shorter than the
first complete fingerprint window succeeds with an empty `raw` array.

At most 6,615,000 samples (13,230,000 bytes, 600 seconds) are accepted. One
additional sentinel byte may be read solely to detect excess input. No valid
prefix result is emitted for oversized or malformed input. PCM is read through
fixed-size buffers; the complete input is never retained. The complete raw
fingerprint and one bounded JSON response fit in memory. The parent must impose
its operation deadline and process resource limits: a stalled stdin is not an
audio-duration limit, and a byte limit cannot terminate a writer that never
sends EOF.

## Success response

Stdout is exactly one UTF-8 JSON object followed by a single LF. It is at most
65,536 bytes. All numbers are nonnegative JSON integers; raw fingerprints are
unsigned 32-bit values in their original order, not compressed strings or
signed decimal values. The schema has these common fields in both modes:

| Field | Meaning |
| --- | --- |
| `protocol_version` | Exactly `1` |
| `mode` | `describe` or `fingerprint` |
| `chromaprint_version` | Actual API version, required to equal `1.6.1` |
| `chromaprint_revision` | `aed8eba2202dd9d7b3b0a56c77904cc805490d72` |
| `algorithm` | Actual API algorithm, explicitly requested as `CHROMAPRINT_ALGORITHM_TEST2` (`1`) |
| `sample_rate` | Actual native API sample rate, required to equal `11025` |
| `channels` | Actual native API channel count, required to equal `1` |
| `item_duration_samples` | `chromaprint_get_item_duration`, in native-rate samples |
| `delay_samples` | `chromaprint_get_delay`, in native-rate samples |
| `first_item_end_sample` | `delay_samples + item_duration_samples`, exclusive end of the first complete support window |
| `max_input_samples` | `6615000` |
| `max_input_bytes` | `13230000` |
| `max_output_bytes` | `65536`, including the terminating LF |

`--describe` creates the configured API context and queries the API getters.
It returns only the common fields and does not read stdin or start processing
audio. The pinned revision identifies the vendored build, not a runtime library
API claim. The build recipe checks every vendored file against its manifest and
links that code statically; it never loads a system Chromaprint library.

Fingerprint mode includes all common fields and these additional fields:

| Field | Meaning |
| --- | --- |
| `input_samples` | Exact number of complete input samples |
| `input_bytes` | Exact number of input bytes, twice `input_samples` |
| `raw_count` | Number of raw 32-bit items |
| `raw` | Array containing exactly `raw_count` unsigned integers |

No fields are optional within their mode. No diagnostic or progress text is
written to stdout. Callers must accept a result only after exit status zero,
complete JSON parsing, and validation of the version, pinned configuration,
field inventory, integer bounds, item count, and PCM input count. In particular,
an empty fingerprint provides no match evidence.

## Exact sample coordinates and boundary limits

The pinned [configuration](https://github.com/acoustid/chromaprint/blob/aed8eba2202dd9d7b3b0a56c77904cc805490d72/src/fingerprinter_configuration.cpp)
uses a 4,096-sample FFT frame, overlap `4096 - floor(4096 / 3) = 2731`, five
chroma filter coefficients, and classifiers with a maximum width of 16 rows.
The [configuration API](https://github.com/acoustid/chromaprint/blob/aed8eba2202dd9d7b3b0a56c77904cc805490d72/src/fingerprinter_configuration.h)
therefore reports:

```text
R = sample_rate = 11025
S = item_duration_samples = 4096 - 2731 = 1365
D = delay_samples = ((5 - 1) + (16 - 1)) * S + 2731 = 28666
W = first_item_end_sample = D + S = 30031
```

These equations explain the pinned source; the JSON values are read from the
sample-precision API, not guessed millisecond constants. The millisecond getters
truncate their results and must not be used for index conversion.

The [audio slicer](https://github.com/acoustid/chromaprint/blob/aed8eba2202dd9d7b3b0a56c77904cc805490d72/src/audio/audio_slicer.h)
starts its first FFT window at input sample zero and emits only complete
windows. The [chroma filter](https://github.com/acoustid/chromaprint/blob/aed8eba2202dd9d7b3b0a56c77904cc805490d72/src/chroma_filter.cpp)
first emits after five FFT rows; the
[fingerprint calculator](https://github.com/acoustid/chromaprint/blob/aed8eba2202dd9d7b3b0a56c77904cc805490d72/src/fingerprint_calculator.cpp)
first emits after 16 filtered rows. Finishing only flushes already supplied
audio; it adds no zero-padded final FFT window. Consequently, for raw index `i`:

```text
left-edge bin:       [i*S, (i+1)*S)
audio support:      [i*S, i*S + W)
earliest complete input extent: i*S + W
raw_count(N):       max(0, floor((N - D) / S))
```

The support interval is the union needed by all classifier bits; individual
bits may use narrower regions. The left-edge bin is an indexing convention,
not an assertion that a scene or intro begins there. In particular, do not add
`D` to every index as if it were a trimmed leading section: TEST2 preserves
leading silence, and `D` describes analysis buffering/lookahead. The first raw
item requires 30,031 input samples even though its left-edge bin starts at zero.

Map a sample position through the caller's actual PCM timestamps. On a verified
continuous PCM segment with origin `T0`, the rational time is `T0 + sample/R`;
an index displacement of `k` corresponds to `k*S/R`. Retain rational arithmetic
until the final timeline conversion. An input discontinuity or unknown resample
origin invalidates a single-origin mapping; the helper cannot recover media
timestamps from raw PCM.

A fingerprint match does not locate an exact audible boundary. A conservative
audio boundary guard is at least `ceil(W * timeline_ticks_per_second / R)`
ticks, recorded separately from the index position. Boundary refinement and
cross-modal confirmation belong to the caller. A rounded stride multiplied by
many indexes accumulates avoidable drift and must not replace the exact sample
formula above.

## Errors

| Exit status | Meaning |
| --- | --- |
| `0` | Complete successful JSON result |
| `2` | Invalid command-line contract |
| `3` | Empty, unaligned, or oversized PCM input |
| `4` | Incompatible API metadata, fingerprint/count failure, allocation failure, or output budget failure |
| `5` | Stdin/stdout I/O or binary-mode setup failure |

Errors produce a short fixed stderr identifier without input bytes or paths.
Before an output I/O failure, stdout is empty on failure; an output failure can
leave partial JSON, which is unusable. External termination, platform signals,
and process-level allocation faults can produce other nonzero exit statuses;
callers must reject every nonzero result. The helper writes no files.
