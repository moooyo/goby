# Selected compatibility phase 2 execution record

Status: **implementation and integration in progress; verification not started**.

Phase 1 closed at `f02b81f` under the user's explicit third-party-client adapter
boundary. Original Web commercial licensing remains a recorded client limitation
and is not a gate for this increment. Work continues on
`codex/selected-client-compatibility` in the isolated checkout. No merge, push or
deployment of this four-phase increment has occurred.

The user requires all phase delivery code before consolidated verification.
Local compilation and unit tests are authorized, while actual media, database,
browser and integration verification uses `test-env`. No phase 2 test, build or
actual media/browser run has been performed during this implementation pass.
The ui-ux-pro-max skill remains disabled for this task.

## Delivery inventory

| Requirement | Implementation boundary | Verification requirement |
| --- | --- | --- |
| B1 Embedded subtitle removal | Prepare a remuxed candidate for explicit review/apply; preserve admitted stream, packet, metadata, chapter and attachment facts; atomic same-filesystem exchange retains the original file | Actual MKV/MKA and selected MP4, source/worker/authority races, cancellation, existing readers, stable item/user state and explicit interrupted-publication recovery |
| B2 Bitmap subtitle OCR | Stream actual PGS/DVD display events to pinned Tesseract models; retain original/reviewed cues and images; atomically publish independent owned SRT/VTT tracks | English, Simplified and Traditional Chinese, combinations, exact event times/forced state, review/correction, real subtitle delivery and source replacement |
| B3 Embedded audio covers | Automatic MP3/FLAC/M4A cover provenance and bounded bytes; ready/none/failed state; managed/provider/sidecar/embedded precedence | Real tagged containers, deterministic picture selection, cache/rescan/source changes, authority, tombstones and restore |
| B4 Generated collages | Deterministic authorized library/genre members and source manifest; current authority/source checks precede cache and conditional responses | Empty/partial/multiple/deduplicated members, rendered pixels, concurrent reads, source changes and revoked access |
| B5 Image transformations | Crop, EXIF orientation, fixed foreground shapes, backgrounds and user-state overlays; complete GIF frame composition and timing | Pixel/orientation fixtures, GIF disposal/loop/delay/transparency, budgets, cancellation and actual HTTP/cache semantics |
| B6 Jobs and management | Independent parameterized operations, idempotency/CAS, review/apply/cancel/recover and native Tasks/item UI; durable derivative state | Actual native journeys, safe restart/restore, old-grant invalidation, runtime joining, API authority and historical migration |

Schema 44 adds `media_operations`, `media_operation_cues` and
`item_owned_subtitles`. Schema 45 adds `item_embedded_artwork`. The final SQL
manifest append and PostgreSQL 17 catalog export belong to integrated closeout;
historical catalog files and existing migration digests remain immutable.

## Important integration contracts

- The operation coordinator uses the existing catalog owner and drains all
  admitted worker/process/storage activity before releasing that owner.
  Configuration comes from a strict operator-owned file, not request paths or
  command arguments. Tool/model identities are captured and rechecked.
- Ready results do not publish changes. Apply binds the reviewed revision,
  source revision and result hash. Only an unresolved B1 `prepared` or
  `catalog_committed` journal reserves its source against conflicting operations.
- Public media/subtitle/download/image source opens recheck publication and the
  complete source identity after opening. Internal publication reads retain a
  separate path for the operation's own source checks.
- Source replacement expires old playback identities in the catalog commit.
  The server registers cancellation handles before its final source check,
  retires the exact source, and joins its original readers and conversion jobs.
  Unrelated sources remain active.
- The original media file retained by B1 is outside database backup coverage.
  Restart and database restore never automatically rename, purge or replay a
  media publication. A current administrator must explicitly recover it.
- OCR recognition is a stream of display events, not sampled video frames or
  all uncompressed subtitle images retained at once. Initial model IDs are
  `eng`, `chi_sim` and `chi_tra`, with one to three explicit selections. Review
  preserves source evidence separately from editable text/times.
- Owned subtitle indexes are not reused. Issued owned-track delivery URLs bind
  their content tag so a later embedded-stream index collision cannot select
  different bytes through an old owned URL.
- Recovery verifies the raw archive fingerprints before trusted normalization.
  It preserves clean ready review state and derivative bytes, clears old
  execution/apply grants, and marks unfinished work interrupted or requiring
  explicit recovery without touching original media.

## Pending consolidated verification

The implementation includes unit, PostgreSQL, actual media and native-browser
verification code. Prepared tests are not results. The coordinator will freeze
the integrated source, build ordinary/embedded artifacts with the current
administrator assets, generate the schema 45 catalog on fresh PostgreSQL 17, and
run affected packages and real OCR/native journeys in owned remote scopes.

Tool/model/font inputs, actual source and binary identities, result counts,
retained failures, targeted repairs and owned-resource closure must be recorded
before this phase can close. Phases 3 and 4 have not started implementation.

## Related contracts

- [Native media processing API](../api/media-processing.md)
- [Media publication and recovery](media-edit-publication.md)
- [Admitted media preservation profiles](media-edit-preservation-profile.md)
- [Bitmap fixture and actual OCR profile](../../scripts/test-env/bitmap-subtitle-fixtures.md)
- [Artwork rendering contract](../../internal/artwork/README.md)
- [Four-phase plan](../planning/selected-compatibility-plan-20260920.md)
