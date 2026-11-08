# Automatic missing-image downloads

Automatic downloads are a native library policy. They do not modify media files,
provider identities, metadata overrides, playback records, or source paths.

## Opt-in and supported content

`LibraryOptions.EnableOnlineImages` defaults to `false`, including libraries
created before this option existed. It is editable through the existing library
create/update contracts and uses the library's exact revision CAS.

Only movie, television, and mixed video libraries support this policy. The
worker uses an existing TMDB identity, including a resolved parent-series
identity for seasons and episodes. It never searches by title or guesses a
match. Supported missing types are:

| Item | Image types | Maximum added per type |
| --- | --- | --- |
| Movie or series | Primary, Backdrop, Logo | 1 |
| Season | Primary | 1 |
| Episode | Thumb | 1 |

The provider's first matching candidate is used. A successful response with no
candidate leaves that type missing. Provider errors, invalid image data, quota
failures, and cache capacity limits remain visible task failures/unavailability.

Existing images of the same type are retained, even when their index is not
zero. An explicitly managed image type is always retained. This includes an
empty managed type left by an administrator's deliberate deletion; only an
explicit reset restores automatic ownership.

## Scheduling and cancellation

The registered task key is `images.download`. Its initial schedule has a daily
interval and an `OnlineImagesRequested` system-event trigger, each with a
30-minute maximum runtime. Registration never replaces administrator-edited,
cleared, or disabled schedules. It never opts libraries into network access.

A committed opt-in and a completed scan emit the dedicated event. Downloaded
images do not emit that event, and actual task execution retains the existing
derived-work suppression of `LibraryChanged`. An event received during an
active image run remains unconsumed until a fresh run can include new work.

Only opted-in libraries with an actual missing type and a known provider
identity receive execution children. At most 10,000 missing items are captured
per library execution. Normal Tasks controls expose durable progress, errors,
and stopping. A timeout of an individual provider request is a provider failure;
it is not reported as an administrator cancelling the task.

## Publication boundary

Provider HTTP uses the existing fixed-host transport, request deadlines,
connection limits, and 20 MiB response limit. Downloads and image validation
take place outside catalog transactions. Shared image-mutation slots bound
retained source bytes and decoding work.

Every download captures a consistent library revision, item metadata revision,
resolved provider identity, managed-type set, and complete artwork revision.
Publication uses the existing catalog owner and follows this sequence:

1. Validate the live process-local task fence before business locks.
2. Lock managed artwork, then the library and item metadata.
3. Recheck opt-in, all captured revisions, the resolved provider identity, and
   the missing-type condition. Parent-series rematches are re-resolved here.
4. Enforce the existing 512 MiB/10,000-entry provider-image capacity and insert
   at index zero with `ON CONFLICT DO NOTHING`.
5. Record and flush catalog/notification changes, then validate the live task
   fence as the final database operation before commit.

A concurrent administrator edit, scan, or policy change wins over the obsolete
download. The downloaded bytes are discarded and the next scheduled snapshot
can reconsider remaining gaps. Failed final fences roll back both bytes and
catalog notifications.

This contract intentionally exposes a bounded missing-only policy. It does not
claim full compatibility with every upstream `ImageFetchers`/`ImageOptions`
combination, an image-lock API, atomic multi-image drafts, or an overwrite mode.

## Manual scoped downloads

`POST /admin/v1/items/{id}/providers/images/missing` accepts a native
administrator cookie, the normal CSRF header, and exactly this JSON body:

```json
{"LibraryId":"library-id","ImageType":"Primary"}
```

`ImageType` must be exactly `Primary`, `Backdrop`, or `Logo`. Each request handles
one item and one type, within the supplied library. It does not require or change
`EnableOnlineImages`; the global provider setting and server-side TMDB credentials
still apply to provider work. Only an existing TMDB match is used. The endpoint
never searches by title, changes a match, or writes metadata or media files.

The response contains `Status`, `ItemId`, `LibraryId`, `ImageType`, and an
`Artwork` object with the current `Revision` and `Items` from the native artwork
contract. Status is one of:

| Status | Meaning |
| --- | --- |
| `downloaded` | One missing image of the requested type was committed. |
| `no_candidate` | TMDB returned no candidate for this type and match. |
| `already_present` | An effective image of this type already exists. |
| `managed` | An explicit managed type, including an empty deletion mask, was retained. |
| `unmatched` | The item has no resolved TMDB identity. |
| `unsupported` | The item or library does not support this requested image type. |

All successful responses recheck native authority and the item's library scope
while reading the returned artwork. Publication retains the same library,
metadata, complete artwork revision, and resolved-identity checks as automatic
downloads. A concurrent edit returns `409 revision_conflict`; callers should
read the current state before retrying. Provider failures remain errors rather
than successful empty results.

Requests share the existing two-slot artwork admission and image decoding
budgets. Busy request admission returns `429 artwork_limit` with `Retry-After: 2`
before provider work. A library UI may page through its items and make bounded
single-item requests, retaining each result and failure. This endpoint does not
start or claim completion of the global `images.download` task.
