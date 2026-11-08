# Provider image selection

Native administrator image selection uses
`POST /admin/v1/items/{id}/providers/image`. It requires the current
administrator cookie, same-origin protection, and CSRF token. The request names
one server-supported provider candidate; arbitrary download URLs are not accepted.

The existing fields are `Provider`, `Id`, `Language`, `ImageId`, `ImageType`,
`ImageIndex`, and `Revision`. `Revision` is the item's metadata revision.

## Explicit replacement

Supply `ArtworkRevision` from `GET /admin/v1/items/{id}/images` to replace the
selected effective image through managed artwork. It is an opaque decimal
string of at most 78 digits. A supplied null, number, empty string, leading zero,
or nondecimal value is invalid. The metadata revision remains required.

The server downloads and validates the candidate before a short publication
transaction. This transaction locks the current target, verifies its metadata
revision and item type, and checks the complete effective artwork revision.
Only the selected slot is replaced. Other images of that type retain their
bytes, including additional backdrops and manual uploads. Other image types
remain unchanged. This operation does not reset a type, write media files, or
change the underlying provider-image cache or metadata.

A successful response retains the original acknowledgement fields:
`Provider`, `ProviderId`, `ImageId`, `ImageType`, `ImageIndex`, `Width`, `Height`,
and `Tag`. It adds `Artwork`, the committed native `{Revision, Items}`
collection. Each entry uses the normal protected preview URL, actual dimensions,
source, byte size, and content tag. The acknowledgement's `Tag` identifies the
selected slot in that collection.

Two request-admission slots bound downloads and retained replacement bytes.
A busy service returns `429 artwork_limit` with `Retry-After: 2` before
downloading. A stale metadata or artwork snapshot returns `409 artwork_conflict`.
Read the current metadata and image collection before retrying. For an unknown
network or server result, read back the current image collection before
offering another replacement.

## Legacy selection

Omitting `ArtworkRevision` retains the existing provider-cache selection
behavior and original acknowledgement shape. An existing managed image type
continues to take precedence over those cached provider bytes. The field must
be omitted to select this behavior; a supplied invalid value never falls back
to it.

Scoped missing-only downloads use the separate
[manual missing-image endpoint](automatic-images.md#manual-scoped-downloads).
