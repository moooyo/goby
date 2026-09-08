# Emby 4.9.5.0 Reference Fixtures

These 107 JSON files were captured from an official, isolated Emby Server 4.9.5.0 instance on the authorized Linux `test-env` host: 65 initial baseline files, 24 `artwork-*` files covering local NFO metadata/images, and 18 `entity-*` files covering entity navigation and filtering. They record reference behavior; they are not evidence that Goby passes all these contracts. The earlier fixtures are unchanged by each extension.

See [reference-server.md](../../../../../docs/research/reference-server.md) for the official package URL/hash, setup, network isolation, source fixtures, observations, and limitations. The recorder is [reference-capture.py](../../../../../scripts/test-env/reference-capture.py).

Each fixture contains:

- `reference`: product, actual server version, and UTC capture timestamp.
- `request`: exact method, API-relative path, explicit recorder headers, and body.
- `response`: status, ordered header pairs, and body representation. `json` stores parsed JSON and `text` stores exact text. New image responses use `binary-base64` to store the exact wire bytes as base64; empty HEAD/304 responses remain empty text.
- Optional `observation`: context that affects interpretation.

Passwords and access tokens are redacted. Private deployment paths use placeholders. Non-sensitive synthetic instance/item/user IDs are preserved to retain type, relationship, and URL semantics. Response headers, including `Content-Length`, are unchanged from the original wire capture; redacted bodies can have different lengths from those headers.

The final export was audited remotely against the private originals: all response headers were exact, JSON structure/types/numeric values were preserved, and all recorded credentials were absent. An earlier generic ID-alias export was replaced entirely because it could alter numeric header values.

The initial library/list/Latest captures preceded the movie library's `SampleIgnoreSize=0` adjustment. Files named `items-movies-after-sample-filter`, `item-detail-*`, and `playback-info-*` follow that adjustment. No source media file was changed. One-element arrays must remain arrays, and omitted fields must not become explicit nulls during fixture consumption.

The later artwork captures follow a single-item refresh after adding synthetic NFO, JPEG, and PNG sidecars. They have a different title and richer metadata for the same movie ID. See the report's M2b extension for source hashes, timezone effects, field-selection behavior, image authentication, transforms, and conditional requests. The extension audit verified its 24 exports independently and retained SHA-256 equality of all original raw/exported baseline files.

The 18 entity captures are read-only requests after that refresh. They cover genre/tag/studio/person lists, name and generic-ID details, ID/name filtering, a negative filter, and pagination. Their audit preserved all preceding 89 raw/exported files. Embedded numeric facet IDs and string entity/list/person IDs must retain their original JSON types during comparisons.

Do not replay setup/library mutations against arbitrary servers. Differential checks should normalize explicitly chosen nondeterministic fields without changing casing, array/object shape, status, content type, null/omission distinctions, or numeric values.
