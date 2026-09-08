# Local artwork and image delivery

Goby indexes local artwork during a library scan and serves it through the Emby ImageService routes. This increment supports indexed item images; user avatars, generated genre/library collages, remote downloads, image uploads, and embedded audio-cover extraction remain separate work.

## Discovery

The supported file naming subset follows the official [movie](https://emby.media/support/articles/Movie-Naming.html), [television](https://emby.media/support/articles/TV-Naming.html), and [music](https://emby.media/support/articles/Music-Naming.html) naming tables. Discovery uses case-insensitive matching with deterministic spelling selection on Linux. More specific media-basename candidates take precedence over shared directory artwork.

| Image type | Recognized candidates |
| --- | --- |
| Movie Primary | `<basename>-poster`, `<basename>-cover`, `<basename>`, `<basename>-default`, `<basename>-movie`, then directory `poster`, `folder`, `cover`, `default`, `movie` |
| Episode Primary | `<basename>-thumb`, then `<basename>` |
| Folder Primary | `poster`, `folder`, `cover`, `default`; a Series also recognizes `show` |
| Backdrop | Basename-prefixed or applicable directory `backdrop`, `fanart`, `background`, `art`, followed by optional numeric suffixes |
| Thumb | `thumb`, `landscape`, with basename-prefixed candidates preferred |
| Banner | `banner`, with a basename-prefixed candidate preferred |
| Logo | `clearlogo`, `logo`, with basename-prefixed candidates preferred |
| Art | `clearart`, with a basename-prefixed candidate preferred |

Candidates use `.jpg`, `.jpeg`, `.png`, or `.gif`, in that preference order. A recognizable but unsupported replacement such as an SVG or WebP produces a warning and preserves the previously valid image for its type. Only the preferred single image is selected for Primary/Thumb/Banner/Logo/Art. Backdrops are deterministically ordered and limited to 32 entries. Episode and audio backdrop/secondary-image selection does not borrow unrelated shared directory artwork.

The scanner does not traverse `extrafanart` directories or resolve series-directory `seasonXX` artwork in this increment. Real series/album wrappers at a media root can use that root's artwork. A CollectionFolder can contain several roots and does not yet select or generate a library-level image.

Every successful media visit, including a cached media probe, checks selected image contents. File-name indexing is performed once per held directory identity and modification time during a scan; individual media lookups use bounded candidate sets. This avoids repeatedly enumerating an entire large directory for each movie. A directory change invalidates the current discovery snapshot rather than making missing images appear deliberately deleted.

Images are decoded and validated before a short transaction replaces the stored records for each successful image type. Missing candidates remove that type's old records. An invalid, unreadable, or unstable candidate preserves all previous images of that type and records a warning; other valid types can still update. Images are not copied into the media directory or modified by scanning. Existing media counters describe media-item changes; image records are maintained separately.

## API and access

| Route relative to `/emby` | Behavior |
| --- | --- |
| `GET /Items/{Id}/Images` | Requires an Emby user token and access to the item's library; returns an image metadata array |
| `GET`, `HEAD /Items/{Id}/Images/{Type}` | Public retrieval of an already indexed image; defaults to index 0 |
| `GET`, `HEAD /Items/{Id}/Images/{Type}/{Index}` | Public retrieval at an index from 0 through 31 |

The difference between authenticated enumeration and public image contents is intentional and follows the [captured Emby reference behavior](../research/reference-server.md#m2b-local-artwork-and-nfo-extension). A missing or invalid token does not prevent public image retrieval. Library access policies protect image enumeration and catalog metadata; they do not make these indexed image bytes private. These routes accept item identifiers, never arbitrary filesystem paths or network URLs. Image mutations and user-profile images are not made public by this policy.

Image metadata includes `ImageType`, authorized `Path`, `Filename`, actual `Width`, `Height`, and byte `Size`. Primary at index 0 omits `ImageIndex`; Backdrop includes it. Goby reports the real size instead of reproducing the reference sample's zero-valued size. Valid types/indices with no indexed image return 404; the reference's null-reference error for a missing Primary index is not reproduced.

Authorized item lists/details expose `ImageTags` and ordered `BackdropImageTags`. `EnableImages=false`, `EnableImageTypes`, and `ImageTypeLimit` control these projections. Detail or `Fields=PrimaryImageAspectRatio` includes the known Primary aspect ratio. Only images actually indexed for that item are advertised; inherited parent art and generated entity images are not fabricated.

## Transformations and caching

Image query names are case-insensitive. Supported options are `Width`, `Height`, `MaxWidth`, `MaxHeight`, `Quality`, `Format`, `Index`, and `Tag`. Conflicting repeated values are rejected. Width/height values are aspect-preserving bounding limits from 0 through 4096; zero leaves that dimension unspecified. Images are never enlarged. `Format` accepts `original`, `jpg`/`jpeg`, `png`, and `gif`.

Unchanged images preserve the validated source bytes, including GIF animation. Resizing or converting a GIF emits its first composited frame. JPEG conversion places transparency over white; GIF conversion uses binary transparency. Quality is 0 through 100, with 0 selecting JPEG quality 85 for transformations. An explicit positive JPEG quality requests encoding; quality does not affect PNG or GIF output.

Cropping, custom background/foreground layers, played-state overlays, orientation transforms, and explicit animation-preserving transforms are not implemented. Requests requiring those operations receive 400 rather than an approximate result. No image enhancer plugins are configured. Even without a size option, output is bounded to 4096 pixels per edge and 16,777,216 pixels.

The indexed source tag is a SHA-256 digest of its original bytes. A quoted ETag identifies the response variant, including its transformation parameters; Width and MaxWidth variants can have different validators even if their output pixels match. `If-None-Match` supports GET/HEAD, weak validators, lists, and `*`. Matching requests return 304 without a body or Content-Length. Correct `Tag` requests get `Cache-Control: public, max-age=31536000`; other successful image responses use `public`. The exact Emby tag/encoding algorithm is not reproduced, and clients must treat validators as opaque values.

Every request checks the indexed file's root containment, inode identity, size, modification time, and full content hash before using cached bytes or returning 304. A changed or unavailable source returns 503 until a successful scan reindexes it. A removed catalog item returns 404 even when a previous representation is still in memory. On a cache miss, the renderer's input hash is checked again to catch an in-place change after opening.

The variant cache is an in-memory LRU with at most 256 entries and 64 MiB of retained byte-buffer capacity; individual allocations over 8 MiB bypass it. It needs no writable image-cache directory. HTTP image work has four admission slots and a 20-second request context. Four separate library I/O workers retain their slots until blocked operations actually finish, even if their callers have canceled. Decoding has two shared slots. This keeps abandoned work bounded on unavailable network storage.

Input limits are 20 MiB, 16,384 pixels per edge, and 26,214,400 pixels. GIFs are additionally limited to 1,000 frames and 33,554,432 total decoded pixels. SVG is never served through these bitmap routes. Failed jobs do not advertise transformed bytes or cache an invalid source.
