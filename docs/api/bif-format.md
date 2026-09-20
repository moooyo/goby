# Roku BIF version-zero codec

`internal/bif` implements the binary archive described by the
[pinned Roku specification](https://github.com/rokudev/dev-doc/blob/27e4a3cdfdeffabaf64b9959193e45e39bb12ad3/docs/DEVELOPER/media-playback/trick-mode/bif-file-creation.md#bif-file-specification).
The package contains no media extraction, database, HTTP, plugin inventory,
authentication, filesystem publication, or player implementation.

## Binary layout

Multibyte archive integers use little-endian encoding.

| Offset | Length | Meaning |
| --- | --- | --- |
| 0 | 8 | Magic: `89 42 49 46 0d 0a 1a 0a` |
| 8 | 4 | Version, exactly zero |
| 12 | 4 | Unsigned image count, `N` |
| 16 | 4 | Unsigned timestamp multiplier in milliseconds; zero means 1000 |
| 20 | 44 | Reserved zero bytes |
| 64 | `8 * (N + 1)` | Timestamp and absolute byte-offset pairs |

Within this package's admitted image profile, each ordinary index entry names
one JPEG. Its byte length is the next offset
minus its own offset. The final entry uses timestamp `0xffffffff` and the
complete archive length as its offset. JPEGs are adjacent. The reader permits
a gap between the index and the first JPEG, as the specification allows; the
writer emits no such gap. An encoded empty archive has a header and sentinel,
for a total of 72 bytes.

The admitted Goby profile requires complete JPEG images and strictly increasing
ordinary timestamps. Duplicate or decreasing timestamps are rejected. These
are codec admission rules, not additional requirements claimed for the Roku
specification. The codec permits nonuniform intervals; a consumer requiring
uniform samples needs a separately enforced production profile.

## Time units

`Frame.Timestamp` and `FrameSource.Timestamp` are raw unsigned BIF units.
They are not milliseconds, seconds, nanoseconds, or media ticks. For example,
timestamps `0, 1, 2` with multiplier `10000` describe `0, 10000, 20000`
milliseconds. Multiplier zero retains its wire representation while behaving
as 1000 milliseconds.

`Entry.TimestampMillis` is a `uint64`. It is calculated from the complete
unsigned product without converting through `time.Duration`, `int64`, or a
floating-point value. The largest admitted timestamp is `0xfffffffe` because
`0xffffffff` is reserved for the sentinel. The producer must check its own
media-time conversion and rounding policy before constructing timestamps.

## Encoding APIs

```go
type Frame struct {
    Timestamp uint32
    JPEG      []byte
}

func Encode(ctx context.Context, frames []Frame,
    multiplier uint32, limits Limits) ([]byte, error)

type FrameSource struct {
    Timestamp uint32
    Size      int64
    Open      func(context.Context) (io.ReadCloser, error)
}

func Write(ctx context.Context, writer io.Writer, frames []FrameSource,
    multiplier uint32, limits Limits) (written int64, err error)
```

`Write` checks every declared size, timestamp, source opener, and computed
offset before emitting output. It then processes frames in order, holding only
one bounded JPEG buffer and one decoded image at a time. It reads at most the
declared frame length plus one byte, detecting both truncation and oversized
input. A frame reader is closed before processing another frame. Reader-close
errors also fail the operation. The output writer remains caller-owned.

After reading and closing a source, the codec validates its complete JPEG
framing, decodes its dimensions, checks pixel budgets, and decodes the actual
image with Go's JPEG decoder. Unsupported JPEG encodings are rejected. A JPEG
header alone, a truncated image, concatenated JPEGs, or trailing bytes cannot
become a successful frame. Image bytes are preserved exactly; the codec does
not recompress them.

I/O, cancellation, source-close, or JPEG errors can leave partial output,
including an index whose promised payload has not been written. The returned
count records bytes accepted by the writer. A caller must use a private
temporary destination and publish it atomically only after `Write` succeeds.
The codec does not attempt to roll back an arbitrary writer.

`Encode` is the convenience wrapper for small, already resident frame lists.
It uses the same validation and format logic, returning no archive on error.
Large producers should use `Write` instead of collecting every JPEG in memory.

## Reading APIs

```go
func Open(reader io.ReaderAt, size int64, limits Limits) (*File, error)

func (file *File) Len() int
func (file *File) Multiplier() uint32
func (file *File) MultiplierMillis() uint32
func (file *File) Entry(index int) (Entry, error)
func (file *File) JPEG(ctx context.Context, index int) ([]byte, error)
func (file *File) AtMillis(milliseconds uint64) (index int, found bool)

type Entry struct {
    Timestamp       uint32
    TimestampMillis uint64
    Offset          uint32
    Size            uint32
}
```

`Open` reads only the header and bounded index. It validates magic, version,
reserved bytes, count, complete index extent, timeline ordering, every offset,
frame-size limits, and the final sentinel. The supplied `size` is the exact
length of the archive view; trailing data beyond a sentinel is not accepted as
part of that view. Index records are copied into private package state.

`JPEG` reads only the selected frame's byte range and repeats real JPEG
validation on those current bytes. Successful `Open` alone does not establish
that every payload is a valid JPEG. `AtMillis` selects the last frame whose
timestamp is at or before the requested time; an empty archive or a request
before the first frame returns `found == false`.

The caller owns the `ReaderAt`, its lifetime, current-source checks, and access
control. Bytes must remain stable during an operation. A changed source cannot
be detected from index metadata alone. Reusing a parsed index is not a substitute
for a media-source revision or content-integrity check at the integration layer.

## Resource and I/O contract

Each zero-valued `Limits` field selects that field's default. Nonzero values
may tighten or raise their individual budgets up to the hard ceilings below.

| Field | Default | Hard ceiling |
| --- | --- | --- |
| `MaxFrames` | 4096 | 65536 |
| `MaxFrameBytes` | 2 MiB | 8 MiB |
| `MaxTotalBytes` | 128 MiB | 512 MiB |
| `MaxDimension` | 2048 pixels per side | 4096 pixels per side |
| `MaxPixels` | 4 Mi pixels per JPEG | 16 Mi pixels per JPEG |

The total-byte budget includes header, index, input padding, and every image.
It must be at least 72 bytes. Dimensions and pixel counts are checked before
the full image decode. These are codec budgets, not claims about aggregate
application concurrency; the caller must also bound concurrent codec jobs.
All file-offset calculations use a wider accumulator and explicitly respect
the format's unsigned 32-bit offset ceiling before narrowing values.

Context is checked between bounded read/write chunks, between frames, and
before and after decoding. Standard JPEG decoding is bounded by the image
budgets and is not asynchronously preempted by cancellation. `FrameSource.Open`
and supplied reader/writer implementations must provide context-aware or
otherwise bounded I/O. The codec cannot forcibly interrupt an arbitrary
blocking `ReaderAt`, `Read`, `Write`, or `Close` call. It starts no background
goroutines or subprocesses and leaves no source reader open on a completed
success or error path.

## Errors

Errors support `errors.Is`. `ErrFormat` identifies malformed archive structure,
an invalid encoding layout, or an unusable source declaration.
`ErrUnsupportedVersion` identifies a nonzero format version. `ErrLimit`
identifies input beyond an admitted resource budget; `ErrInvalidLimits` identifies
invalid caller budgets. `ErrInvalidJPEG` identifies an invalid image or a source
whose bytes do not match its declared length. `ErrIndex` identifies an invalid
frame selector. Underlying I/O and context errors remain discoverable through
wrapping; callers must never treat an error as a successfully publishable BIF.
