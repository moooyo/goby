# Seek previews

These authenticated read endpoints expose already published, source-bound video
previews. A read never schedules analysis or invokes FFmpeg. Missing results do
not become a request-time generation job.

| Endpoint | Required query | Response |
| --- | --- | --- |
| `GET /Items/{Id}/ThumbnailSet` | `Width=240`, `320` or `400` | `ThumbnailSetInfo` JSON |
| `GET /Videos/{Id}/index.bif` | `Width=240`, `320` or `400` | Roku version-zero BIF, `application/octet-stream` |
| `GET /Items/{Id}/Images/Thumbnail` | Nonnegative integer `PositionTicks` | JPEG at or immediately before the requested time |

The `/emby` prefix is supported through the shared compatibility namespace.
`HEAD` has the corresponding GET response headers and no body. Route literals
are normalized by that namespace; item and media-source identifiers remain
opaque. The static `Thumbnail` and `index.bif` routes take precedence over the
existing artwork-type and media-filename wildcards.

All three endpoints accept an optional `MediaSourceId` selecting the current
indexed source for the item. A wrong or inaccessible source is not a missing
derivative. Ordinary authenticated credentials and current library/media access
are required even when the image URL carries an ImageTag, a cache validator or
a byte range. Anonymous tokenless Web image requests are not authorized by a tag.

## Discovery and image selection

The exact JSON shape follows the repository's pinned Emby SDK 4.9.5.0 export:

```json
{
  "AspectRatio": 1.7857142857142858,
  "Thumbnails": [
    {"PositionTicks": 0, "ImageTag": "goby-preview-400-<64 lowercase hex digits>"},
    {"PositionTicks": 100000000, "ImageTag": "goby-preview-400-<64 lowercase hex digits>"}
  ]
}
```

The placeholder tags above illustrate their shape, not valid identifiers.
Positions come from the validated BIF index: milliseconds convert to 100 ns
ticks exactly. Overflow is rejected. Frames before the first indexed timestamp
are unavailable; later requests select the last frame at or before that time.
ImageTag binds the item, source identity/revision, full BIF digest, stored width
and indexed frame time. It is a cache identity, never permission.

The JPEG endpoint accepts `tag`, `maxWidth` from 0 through 4096, and `quality`
from 0 through 100. Zero uses the existing artwork default: no extra width bound
and JPEG quality 85. A valid supplied tag selects its stored width and must match
the current selected frame. A stale tag returns 404 instead of reusing it for a
replacement source. Without a tag, the provider selects the largest ready
variant, in the order 400, 320, 240.

`maxWidth` limits display output; it is not a generation variant. A 400-width
thumbnail requested with `maxWidth=800` remains 400 pixels wide, while a smaller
bound downsizes it with its aspect ratio preserved. Rendering uses bounded Go
JPEG/image processing and does not invoke a media executable.

Query names are case-insensitive. Repeated identical values are accepted;
conflicting case aliases or values, malformed query encoding, unknown fields,
unsupported widths, noncanonical/negative/overflowing integer positions and
unsupported image options return 400. `UserId`, filesystem paths and image-index
selectors are not supported on these routes. Authentication carrier conflicts
remain the responsibility of the shared credential parser, before route access.

## Missing results and delivery

For an authorized current video without a ready derivative, BIF returns 200 with
a valid 72-byte, zero-image archive. This behavior is supported by the historical
first-party BifService source. It does not claim the source has zero duration or
that generation succeeded. ThumbnailSet and JPEG return 404 for missing results;
404 is declared by the pinned SDK, and no current reference capture establishes
a different missing-result DTO. These explicit Goby choices are distinct from
claims of complete current Emby runtime parity.

Every read opens an authorized source-bound lease, then revalidates the current
session, media ACL, source and same immutable derivative immediately before
conditional processing and response headers. This includes cache hits, 304,
HEAD, empty BIFs and range errors. Retirement, request cancellation and runtime
shutdown cancel the lease. Cleanup closes the reader and joins its owned work;
no SQL transaction is held across HTTP transmission by this layer.

Successful representations use a strong content/source-bound ETag and
`Cache-Control: private, no-cache, no-transform`. `If-Match` and `If-None-Match`
are applied before Range. A matching weak `If-None-Match` may return 304 after
authorization. BIF supports one closed, open-ended or suffix byte range with
206; malformed, multiple or unsatisfiable ranges return 416 and
`Content-Range: bytes */<size>`. A matching strong `If-Range` permits a range;
other `If-Range` values select the full representation. Dates are not advertised
as validators because source and derivative identity are more precise than
second-resolution timestamps. Metadata and JPEG responses ignore Range.

The BIF reader is bounded to 128 MiB, 4096 frames and the BIF codec's image
limits. HTTP transmission uses the existing idle-write limit and a 30-second
request budget. A read error after headers aborts the response rather than
claiming a complete archive. Authorization and source-open errors do not publish
a preview ETag; authorized range/precondition responses retain their validator.

## Primary evidence and limits

- The pinned SDK at commit
  [`bdd0dd7c0801f6e069dff2795d80cddae6f91791`](https://github.com/MediaBrowser/Emby.SDK/blob/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json)
  defines required Width and the exact `RokuMetadata.Api.ThumbnailSetInfo` and
  `ThumbnailInfo` properties. Its retained local copy is
  [emby-sdk-openapi.snapshot.json](../sources/emby-sdk-openapi.snapshot.json).
- The first-party Roku client at
  [`0908e13f68433284c1b411f094a1be62cf781ec4`](https://github.com/MediaBrowser/Emby.Roku/blob/0908e13f68433284c1b411f094a1be62cf781ec4/source/VideoPlayer.brs#L242)
  requests 320/240 widths and MediaSourceId. Its separate `Roku Thumbnails`
  plugin check is not implemented by inventing Goby plugin inventory.
- Public Web source
  [`osdcontroller.js?v=26.0.26`](https://app.emby.media/modules/playback/osdcontroller.js?v=26.0.26)
  requests width 400, walks thumbnail positions to the last preceding frame,
  and builds a `Thumbnail` image URL with PositionTicks, MediaSourceId, tag and
  maxWidth. This source is newer than the pinned SDK and is workflow evidence,
  not a current server acceptance capture. The historical first-party
  [ApiClient](https://github.com/MediaBrowser/Emby.ApiClient.Javascript/blob/fdd0939ac596406ab92274f30fd83d226a677424/apiclient.js#L3138)
  corroborates the image URL shape. The public
  [current ApiClient](https://app.emby.media/modules/emby-apiclient/apiclient.js?v=26.0.26)
  rounds the pixel-ratio-adjusted maxWidth and supplies quality 90 for thumbnails.
- Historical first-party
  [BifService](https://github.com/MediaBrowser/roku-bif/blob/3ac428a13da366f522515e9aa6af1b19706be9f4/RokuMetadata/Api/BifService.cs#L59)
  returns an empty BIF when no derivative exists and serves octet-stream through
  the static-file layer. Its
  [HTTP result factory](https://github.com/MediaBrowser/Emby/blob/ed925c3367a47ea32d43e664a7fb3f5ddafbc4b7/Emby.Server.Implementations/HttpServer/HttpResultFactory.cs#L447)
  supplies historical conditional/static-file behavior.
- Roku's pinned
  [BIF format specification](https://github.com/rokudev/dev-doc/blob/27e4a3cdfdeffabaf64b9959193e45e39bb12ad3/docs/DEVELOPER/media-playback/trick-mode/bif-file-creation.md#L155)
  defines the binary representation. The codec implementation remains separate
  from authorization, source identity, generation and cache ownership.

HTTP contract test sources cover DTO keys, timestamps and tags, JPEG display
bounds, full and partial BIF bytes, conditional/HEAD/range authority ordering,
missing results, cancellation, lease closure and route precedence. They are not
evidence of playback compatibility or completed Phase 2 verification; those
results belong to the consolidated remote acceptance record.
