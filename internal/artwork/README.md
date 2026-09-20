# Artwork transformation contract

`TransformationVersion` is `artwork-v2`. HTTP callers must canonicalize options
with `CanonicalOptions` and include `OptionsKey` in derivative cache identities.
The source digest and current authorization remain separate obligations. The
renderer never opens a URL or a filesystem path and never closes its input
reader.

The pipeline is deterministic:

1. Apply JPEG EXIF orientation 1–8 when `AutoOrient` is true. Absent or malformed
   EXIF leaves the image in its encoded orientation. Source metadata continues
   to describe the original bytes and encoded dimensions.
2. Apply `CropRect` in the oriented coordinate space. An all-zero rectangle
   disables cropping; otherwise both dimensions must be positive, coordinates
   nonnegative, and the entire rectangle inside the oriented canvas.
3. Apply `CropWhitespace`. Only outer transparent or near-white margins are
   removed: alpha at most 8, or all unassociated RGB components at least 245.
   Interior whitespace remains. An entirely empty image becomes its top-left
   pixel. Animation uses the union of all displayed frame content, so its crop
   stays fixed across the timeline.
4. Downsize to the existing width, height, and maximum bounds without enlarging
   smaller inputs or changing the cropped aspect ratio.
5. Composite the source over `BackgroundColor` when supplied.
6. Draw `ForegroundLayer` in the center half of the shorter output dimension.
7. Draw the unplayed count at the top left and played indicator at the top right.
8. Draw the progress bar along the bottom edge.

Colors accept `black`, `white`, `transparent`, `#RRGGBB`, and `#RRGGBBAA`; their
canonical representation is lowercase `#rrggbbaa`. Foreground syntax is `play`,
`music`, or `folder`, optionally followed by `:` and one of these colors. Its
default color is `#ffffffcc`. These are fixed geometric icons, not arbitrary
scripts, image paths, embedded data, or remote assets. Unknown names are rejected.
The count is an integer from 0 through 9999; zero hides it. Progress accepts a
finite percentage from 0 through 100; zero hides the bar. Values are rejected
rather than silently clipped. Badges are clipped to tiny output canvases.

GIF output preserves all image frames, frame delays (including zero), and loop
count by default. Partial input frames are composited with their scoped disposal
method before transformation. Output frames cover the full output canvas and
use disposal-to-background with a transparent background, preserving displayed
frame content without leaving previous transparent pixels behind. GIF output
uses the existing deterministic color cube and binary transparency: alpha below
128 is transparent; other pixels are opaque. Exact source palette indexes and
delta rectangles are not retained. Unchanged images keep their original bytes.
`DisableAnimation` explicitly selects the first composited frame. PNG and JPEG
are static outputs and also use that first frame. Unsupported GIF plain-text
rendering extensions and reserved disposal modes are rejected rather than
silently dropping visible content.

The existing bounds continue to apply: 20 MiB encoded input, 25 Mi input pixels,
16384 input pixels per edge, 16 Mi output pixels, 4096 output pixels per edge,
and 80 MiB encoded output. GIF input permits at most 1000 frames and 32 Mi decoded
frame pixels. An animated transformation additionally limits full logical canvas
pixels multiplied by frame count, and accumulated output frame pixels, each to
32 Mi. This prevents sparse tiny delta frames from expanding into unbounded
working canvases or retained output frames. A whitespace pass is bounded by the
same full-canvas limit. Original animation bytes can still pass through when
no transformation is needed. The two shared decode slots remain occupied until
their worker exits even when a canceled caller returns earlier. Reading, pixel
traversal, row drawing, resampling, and output writes observe cancellation;
standard-library decoders run only after encoded size and pixel admission.

JPEG uses quality 85 when quality is zero and flattens remaining transparency
over white after the optional background. `Result.ETag` hashes actual output
bytes, while `Result.Source.Tag` hashes the validated original input.
