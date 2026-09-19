# Artwork management and image delivery

Goby combines scanned local artwork, configured provider images and explicitly
managed artwork. Phase 3 media/entity image mutation and current-authority
protection passed the selected [remote acceptance and closeout](amd-media-phase3-20260919.md). Historical
local-image and reference results do not validate the new mutation or access
contract. Generated genre/library collages and embedded audio-cover extraction
remain unsupported by this increment; provider-specific online acceptance remains
deferred.

## Current managed artwork contract

Native paths below start with `/admin/v1/{items|entities}/{id}/images`. They
require a current administrator cookie; writes also require same-origin and
CSRF protection. Entity images belong to the persistent entity itself rather
than borrowing an arbitrary associated item's image.

`GET /admin/v1/entities` supplies the management browser with Kind
Person/Genre/Studio/Tag/MusicArtist, SearchTerm and paging. It returns
`{Items, TotalRecordCount, StartIndex, Limit}`; the default limit is 25 and
maximum 100. Omitted Kind defaults to Person. Music-role lists have the separate
[music routes](music-metadata.md).

| Method and suffix | Input and response |
| --- | --- |
| `GET` collection | `{Revision, Items}` |
| `GET /{Type}/{Index}` | Protected image preview |
| `PUT /{Type}/{Index}` | Raw validated JPEG/PNG/GIF bytes, at most 20 MiB; quoted `If-Match` collection revision |
| `DELETE /{Type}/{Index}` | No body; quoted `If-Match` collection revision |
| `POST /{Type}/reorder` | `{Revision, Indexes}` containing the complete old-index permutation |
| `POST /{Type}/reset` | `{Revision}`; remove the managed type override and resume automatic sources |

Collection entries contain ImageType, ImageIndex, Tag, Width, Height, decimal
string Size, PreviewUrl and Source. Revision is an opaque decimal token of up
to 78 digits: never parse it as a JavaScript number or increment it. The token
includes effective automatic-image content so a concurrent scan/provider change
also conflicts. A stale mutation returns `409`; reload the collection before
editing again. Uploaded bytes are validated and retained without re-encoding.
Media/entity uploads accept their supported image Content-Type or
application/octet-stream, with exactly one Content-Type header; reorder/reset
require a JSON object of at most 4 KiB. Two request-admission slots cover a
media/entity mutation from body consumption through commit; a busy service
returns `429`. Body reads retain the shared 15-second request-body bound.

Managed types are Primary, Backdrop, Thumb, Banner, Logo, Art, Disc, Box,
BoxRear, Menu and Screenshot. Backdrop and Screenshot accept indices 0 through
31; the other types use index 0. Deletion/reordering compacts indices. A managed
type is a complete server-owned snapshot: an empty deletion mask prevents a
subsequent scan from restoring the automatic image. Only reset restores automatic
local/provider selection. No mutation deletes an original image in a media root.
Stored payload budgets are 128 MiB per type, 256 MiB per owner, and 512 MiB or
10,000 managed rows globally. Owner deletion cascades stored payload cleanup.

Compatibility mutations use POST/DELETE
`/emby/Items/{Id}/Images/{Type}[/{Index}]`, their POST `.../Delete` aliases,
and POST `.../{Index}/Index?NewIndex=...` for a supported move. They require a
current Emby administrator or application key and return an empty `200`.
If-Match is optional on this compatibility surface. Even without it, a mutation
captures the effective content snapshot and rechecks it before commit; a
concurrent change can return `409`. This does not make image mutation anonymous.
Delete and index-move requests require an empty body, including chunked input.
Native entity listing also checks current administrator credentials inside its
read transaction rather than relying only on the initial HTTP check.

The administrator dialog previews the actual saved collection, uses explicit
delete/reset confirmation and labeled reorder buttons, and revokes temporary
browser preview URLs. Busy mutations block closing/navigation. Conflicts and
unknown save outcomes preserve context and require reload; loading, error and
success states remain distinct. The selected twelve-stage browser journey and
desktop/390-pixel mobile visual review passed under the
[phase 3 administrator workflow](../../scripts/test-env/phase3-admin-browser.md).

### User avatars and public-login visibility

Native GET/PUT/DELETE `/admin/v1/users/{id}/image` uses the same
`{Revision, Items}` collection shape, with only Primary index 0. Native
GET `/admin/v1/users/{id}/image/content` is the protected preview. Writes require
the administrator cookie, CSRF and a quoted If-Match revision; missing native
preconditions return `428`, stale values `409`. Uploads require matching
image/jpeg, image/png or image/gif Content-Type and validated raw bytes up to
20 MiB. Native success returns the current collection and its revision ETag.
Deleting leaves a revisioned empty set and does not reuse the old tag.
An oversized upload body returns `413`; shared managed-storage or image-transfer
capacity returns `429 artwork_limit`. Avatars use the shared migration-0038
artwork state and quota, not a separate unbounded payload store.

Compatibility GET/HEAD `/emby/Users/{Id}/Images/Primary[/0]` serves an avatar;
POST uploads and DELETE or POST `.../Delete` removes it, returning `204`.
Compatibility mutation accepts an optional If-Match. Ordinary user logins can
read their own avatar under current account/session authority; self mutation
additionally requires preference access. Current administrators and permitted
application keys can manage targets.
An ordinary login cannot read another user's private avatar. All mutations
remain authenticated and repeat authority checks before commit.

An anonymous avatar read is allowed only for an account visible in the current
public-login picker: enabled, non-administrator, not hidden, and permitted by
the applicable remote/device visibility policy. Hidden-from-unused-device rules
use actual recorded user/device history. A missing/hidden target is not found.
A supplied invalid token never falls back to the anonymous branch. Public
visibility and private authority are rechecked before cached bytes or 304;
changing policy therefore changes access even when the caller retains an old
tag. All rendered image responses use `private, no-cache, no-transform`.

Authorized user DTOs and eligible public-login users expose PrimaryImageTag and
PrimaryImageAspectRatio only for a stored avatar. These projections are not
credentials and never substitute for payload authorization. The administrator
avatar manager previews through the native protected content route; it does
not construct a fake public URL.
Projection is best effort and does not turn a valid login into an authentication
failure when optional artwork metadata is unavailable. The login picker and
anonymous payload route share one PublicAvatarVisible predicate. No avatar
activity action is added in this increment; a changed artwork revision is not
misreported as a user.updated management revision.

Source: [managed artwork handlers](../../internal/server/artwork_management.go),
[image delivery](../../internal/server/artwork_images_http.go),
[avatar handlers](../../internal/server/avatars.go), and
[current avatar authority](../../internal/identity/avatars.go).
Migration [0038](../../internal/database/migrations/0038_artwork_entity_state.sql)
adds managed payloads and independent entity state. Old-row preservation and
native backup/recovery coverage passed the recorded phase 3 scopes. The exact
20 MiB valid PNG dump/validate/restore and canonical fingerprint witness passed
at a 1 GiB owned PostgreSQL limit, with peak 756,084,736 bytes. The original
512 MiB OOM remains a failed budget profile; the image fixture and 64 MiB
serialized-row ceiling were not reduced. These memory limits are distinct from
the managed-payload storage quotas above.

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

Images are decoded and validated before a short transaction replaces the stored records for each successful automatic image type. Missing candidates remove that type's old automatic records. An invalid, unreadable, or unstable candidate preserves the previous automatic images of that type and records a warning; other valid types can still update. Managed overrides remain independent. Images are not copied into the media directory or modified by scanning. Existing media counters describe media-item changes; image records are maintained separately. Per-library EnableLocalImages defaults to true; disabling it stops later directory-artwork import while preserving accepted images and manual overrides. Re-enabling requires a scan.

## API and access

| Route relative to `/emby` | Behavior |
| --- | --- |
| `GET /Items/{Id}/Images` | Requires an Emby user token and access to the item's library; returns an image metadata array |
| `GET`, `HEAD /Items/{Id}/Images/{Type}` | Current authenticated item/entity authorization; defaults to index 0 |
| `GET`, `HEAD /Items/{Id}/Images/{Type}/{Index}` | The same authorization at a supported index |

Current media/entity image contents and enumeration are protected by current
catalog access, including before cached or conditional responses. The
[earlier reference capture](../research/reference-server.md#m2b-local-artwork-and-nfo-extension)
observed public image contents; that historical observation does not describe
the phase 3 policy. Missing or invalid authentication cannot obtain protected
media artwork. These routes accept identifiers, never arbitrary filesystem
paths or network URLs. Public-login avatar visibility has its separate rule;
it does not make media artwork, metadata or mutations anonymous.

Compatibility image metadata includes ImageType, actual Width, Height and byte
Size, with authorized Path/Filename only for applicable indexed sources. Managed
storage paths are not exposed. Primary at index 0 omits ImageIndex; Backdrop
includes it. Goby reports real size instead of the reference sample's zero-valued
size. A missing valid image returns 404 rather than the reference's null-reference
error. The native collection shape above is a separate contract.

Authorized item lists/details expose `ImageTags` and ordered `BackdropImageTags`. `EnableImages=false`, `EnableImageTypes`, and `ImageTypeLimit` control these projections. Detail or `Fields=PrimaryImageAspectRatio` includes the known Primary aspect ratio. Only images actually indexed for that item are advertised; inherited parent art and generated entity images are not fabricated.

## Transformations and caching

Image query names are case-insensitive. Supported options are `Width`, `Height`, `MaxWidth`, `MaxHeight`, `Quality`, `Format`, `Index`, and `Tag`. Conflicting repeated values are rejected. Width/height values are aspect-preserving bounding limits from 0 through 4096; zero leaves that dimension unspecified. Images are never enlarged. `Format` accepts `original`, `jpg`/`jpeg`, `png`, and `gif`.

Unchanged images preserve the validated source bytes, including GIF animation. Resizing or converting a GIF emits its first composited frame. JPEG conversion places transparency over white; GIF conversion uses binary transparency. Quality is 0 through 100, with 0 selecting JPEG quality 85 for transformations. An explicit positive JPEG quality requests encoding; quality does not affect PNG or GIF output.

Cropping, custom background/foreground layers, played-state overlays, orientation transforms, and explicit animation-preserving transforms are not implemented. Requests requiring those operations receive 400 rather than an approximate result. No image enhancer plugins are configured. Even without a size option, output is bounded to 4096 pixels per edge and 16,777,216 pixels.

The indexed source tag is a SHA-256 digest of its original bytes. A quoted ETag identifies the response variant, including transformation parameters; Width and MaxWidth variants can have different validators even if their pixels match. If-None-Match supports GET/HEAD, weak validators, lists and `*`. An authorized matching request can return 304 without a body or Content-Length. Cache validators never bypass current authorization, including after revocation or loss of source visibility. The exact Emby tag/encoding algorithm is not reproduced; clients must treat validators as opaque values.

Indexed-file requests check root containment, inode identity, size, modification time and full content hash before using cached bytes or returning 304. A changed or unavailable indexed source returns 503 until a successful scan reindexes it. A removed catalog owner returns 404 even when an earlier representation is still in memory. Managed images use their committed payload identity. On a local cache miss, the renderer's input hash is checked again to catch an in-place change after opening.

The variant cache is an in-memory LRU with at most 256 entries and 64 MiB of retained byte-buffer capacity; individual allocations over 8 MiB bypass it. It needs no writable image-cache directory. HTTP image work has four admission slots and a 20-second request context. Transfers have separate limits of eight responses and 128 MiB of retained buffers, so a slow transfer does not retain a rendering slot. Four separate library I/O workers retain their slots until blocked operations actually finish, even if their callers have canceled. Decoding has two shared slots. This keeps abandoned work bounded on unavailable network storage.

Input limits are 20 MiB, 16,384 pixels per edge, and 26,214,400 pixels. GIFs are additionally limited to 1,000 frames and 33,554,432 total decoded pixels. SVG is never served through these bitmap routes. Failed jobs do not advertise transformed bytes or cache an invalid source.
