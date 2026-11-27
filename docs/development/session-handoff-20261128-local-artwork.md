# Session handoff: local metadata repairs and the remaining artwork queue

**November 28, 2026. Next task: design and prove the exact-target execution
fence before separately requesting approval for a Joy of Life pilot.** The
single-item backdrop reread is implemented in source. No artwork pilot,
deployment, or production mutation was performed in this follow-up. The last
verified deployed backend in the preceding session was `04308156` (PostgreSQL
17.12, schema 81). Do not treat this record as fresh production state or as
authorization to connect, deploy, or write.

## Completed work: do not replay

- `bd9e8812` fixed analysis admission exhausting the scan publication budget and
  blocking deferred local imports. Local posters recovered. The pinned
  [Emby NFO differential](../../tests/compatibility/fixtures/reference/emby-4.10.2.0/nfo-differential.md)
  and [two-episode replay](../../tests/compatibility/fixtures/reference/emby-4.10.2.0/nfo-multi-episode.md)
  cover the follow-up compatibility work. Three real double-episode sidecars
  passed production acceptance without splitting the underlying items or sources.
- `3000f4b8` added the separate damaged-NFO regeneration task and schema 81.
  The seven damaged production sidecars were reversibly isolated and retired from
  the queue because the formal candidates were absent. This was **not** a
  production regeneration test; automatic sidecar writing was not enabled.
- `0df9d4ee` added an advisory series identity audit. The production pilot
  observed 142 accepted series sidecars and returned three warnings. The audit
  does not enforce identity before import. Stock Emby 4.10.2.0 with Nfo Metadata
  1.0.88.0 also accepted wrong-work NFO declarations in offline fixtures. With
  automatic grouping enabled, lists combined two physical series with matching
  declared IDs while their direct item IDs and parents remained separate. The
  same-number watched flag was shared even with grouping off in that fixture.
  This is not online-provider, playback, or resume evidence, and is not behavior
  to copy into Goby's physical identity or viewing-state model.
- `04308156` added the [single-item local NFO reread](../api/admin-metadata.md#reread-local-nfo).
  Its deployment probe only proved route registration (unauthenticated `401`);
  subsequent operations proved real positive imports.

| Production repair | Accepted scope and outcome | Final readback (UTC) |
| --- | --- | --- |
| Modern Family | One wrong-work series NFO; its physical episode tree and the separate Succession series were not moved or merged. One ordinary scan of the selected library was approved specifically for this repair. | 2026-11-27 12:50:56 |
| What If...? | 11 originals privately backed up, then one series, one season, and nine episodes individually accepted (`200`, `Changed: true`, matching ID). No scan or provider refresh. | 2026-11-27 15:50:00 |
| Ghost / Mouse | 23 originals privately backed up and 23 single-item rereads accepted: Ghost series, Ghost season 1 and E01–E20, and Mouse season 1. Mouse series, regular episodes, and specials were untouched. Ghost's existing `12+` and both season overviews were deliberately retained, not validated as correct. No scan or provider refresh. | 2026-11-27 17:22:41 |

For the latter two batches, accepted bytes, hierarchy/item identity, SMB access,
viewing state, and the scoped image records were checked. No playback test was
performed. Host-private originals remain available; they are not in Git. The
Ghost/Mouse offline drafts are historical and must not be replayed. A future
identity gate and any rating or season-overview revision are separate work.

## Outstanding: 18 local-artwork items

The last read-only production observation ended **2026-11-27 17:34:18 UTC**
(2026-11-28 01:34:18 in Asia/Shanghai). The `video` queue had 18 image items and
zero NFO or online-enrichment items before and after the observation. All 18
were attached to the latest completed scan, not orphaned work. The last timed
local import inspected 18 items, reported zero updates, and failed. That counter
alone does not prove that no image type could commit independently. Local and
online images were both enabled. No images were changed, exported, or taken
from the videos; the 15 indexed videos were only checked by file attributes.

Of 34 distinct files selected by the deployed rules, 16 decoded and 18 failed:
15 empty files and three non-image files containing zero padding. They affect
22 image-type slots, because four Night Agent episode files each supply both
`Primary` and `Thumb`. Fresh paired reads reproduced the failures; the old
receipt does not establish uninterrupted file identity or byte continuity.

| Scope | Last observation | Suggested follow-up, not authorized here |
| --- | --- | --- |
| Joy of Life (Qing Yu Nian), series | The empty `backdrop.jpg` and valid `fanart.jpg` are both selected as `Backdrop`. The invalid member blocks replacement of the entire group. The original `fanart.jpg` is a fully decoded 1920 × 1080 JPEG; excluding the empty file in memory left it as the only candidate. The existing poster, banner, and logo were valid. | First pilot: isolate the empty file and index the existing `fanart.jpg` in place. Do not copy it to a second recognized name; both files would be candidates. |
| 15 episodes | No indexed own primary or usable same-type replacement. The three non-image files are The Night Agent S03E02, E03, and E05; S02E02 has the fourth affected `Primary`/`Thumb` pair. The two Modern Family episodes were incorrectly labeled Succession in the old receipt; their physical binding was always Modern Family. | Prefer a verified original still. Obtaining frames from each exact episode requires separate approval. A client can still display inherited series artwork; that is not an own-episode repair. |
| Yes, Minister and House of Cards, series | Empty `banner.jpg` (`Banner`) and empty `clearart.png` (`Art`), respectively. No same-type replacement was found; both posters were valid. | If separately approved, isolate the failed optional candidate and leave the type absent rather than use a placeholder. |

The earlier scan's generic warning has not been decomposed; do not assume these
18 files account for the whole warning.

## Single-item path and the remaining production gate

The endpoint described in steps 1–3 is implemented; see the
[local backdrop reread contract](local-artwork.md#reread-one-local-series-backdrop).
The owned writer's active-work and generation checks protect the endpoint's
transaction, not a preceding filesystem operation. The existing scanner and
importer do not share an exact-target hold that an operator can acquire before
quarantine. Step 4 remains a prerequisite to even proposing a live window;
step 5 has not been run against the production item.

1. **Keep the first contract narrow.** The native admin route is
   `POST /admin/v1/items/{id}/images/Backdrop/reread-local`. It requires the
   native same-origin cookie and CSRF contract and accepts no body or query.
   It resolves the ordinary physical `Series` and its registered root from the ID.
   Admit an existing image-only pending row (`import_images` true, `import_nfo`
   and `enrich_online` false) with a still-current source key, directory identity,
   root binding, and latest completed scan generation. Do not create or renew
   scan authority or accept a path, URL, or user-supplied payload. A no-longer
   eligible row is not permission to process a different item or other work.
2. **Publish only Backdrop; prove the whole image phase before completing it.**
   Use normal selection, bounded decoding, and descriptor-based source checks.
   Require a nonempty, fully valid Backdrop set. All five other types must be
   observed and match their complete raw accepted local rows, including confirmed
   absence where no rows exist. A bad, changed, or unproven other type leaves all
   rows and the queue untouched in this first contract. Do not overwrite or reset
   managed/provider artwork. Recheck the item, roots, library options and active
   work under the owned writer; independently reprove the directory and selected
   names/files just before commit. No long filesystem decode inside the write
   transaction. Roll back on any changed source, authority, or generation.
3. **Commit indexing and queue completion together, without downstream work.**
   Update only `item_images` for the target Backdrop and retire only the exact
   matched image-only queue row in one transaction. Reuse normal client catalog
   notifications for an effective change. Use derived context to suppress the
   generic `LibraryChanged` task signal, and do not call the ordinary queue
   completion path that explicitly emits `OnlineImagesRequested` or
   `OnlineMetadataIngestRequested`. Check that clearing the bit does not unlock
   a missing online-image type on this target: for a Series the worker checks
   Primary, Backdrop, and Logo. The operation must not launch work or change
   scheduler settings; independently scheduled work is a separate concern.
4. **Solve the handoff between the filesystem and the endpoint.** The production
   gate is still open design work: prove a short exact-target fence, honored by
   ordinary scanning and deferred import, from before quarantine through commit.
   An SMB exclusive window addresses external writers, not the existing hourly,
   event, or startup importer. An instant check that no run is active is not a
   fence. Do not move the bad file until both the execution fence and a newly
   approved SMB window are established. Re-prove two candidates, back up only
   the empty original with its permissions, then atomically move it out of the
   recognized names without touching `fanart.jpg` or the directory identity.
5. **Accept what the client can actually read.** Verify `Backdrop[0]` is the
   local `fanart.jpg` and can be retrieved, other image types are unchanged,
   only the target queue row is retired (expected 18 → 17), and no scan or
   online-task signal was created. With a timeout or unknown outcome, check
   disk and catalog before retrying or restoring. Putting the bad image back
   does not reverse a successful catalog commit. Do not hand-edit SQL or rerun
   the broader importer to reconcile the outcome.

The current [managed artwork upload](local-artwork.md#current-managed-artwork-contract)
can mask a visible gap, but it does not repair the local sidecar or queue and
may emit the ordinary task signal. Reset restores already indexed automatic
sources; it does not reread disk. The global local-metadata importer and a
library scan are not equivalent to the single-item operation.

### Code and verification starting points

- [Selection](../../internal/library/images_candidates.go) and [indexed directory lookup](../../internal/library/images_index.go): preserve current name order and whole-group Backdrop behavior.
- [Observation and publication](../../internal/library/images_scan.go) and [raw row comparison](../../internal/library/image_catalog_changes.go): reuse the image inspection and differential writer, not a second scanner.
- [Existing deferred import and completion](../../internal/library/metadata_ingest_task.go), [source-key construction](../../internal/library/scan_metadata_ingest.go), and [online admission](../../internal/library/online_images.go): verify generation and suppression separately; a completed scan alone is not a live grant.
- [Single-item NFO authority pattern](../../internal/library/local_nfo_reread.go) and [catalog versus task notifications](../../internal/library/ownership.go): reuse bounded read/commit and authentication patterns, not the NFO route itself.
- [Store regression](../../internal/library/local_backdrop_reread_integration_test.go)
  and [HTTP regression](../../internal/server/admin_local_backdrop_reread_integration_test.go)
  use synthetic media. A valid `fanart.jpg` plus an empty `backdrop.jpg` first
  rejects the whole set. After external isolation in the fixture, the original
  JPEG is retrievable, the exact row is retired, and other images, pending work,
  scans and task-event counters are unchanged. Rejection cases include changed
  non-Backdrop rows, stale generation and root, a real concurrent automatic
  importer, revoked authority, and file or candidate changes before commit.
- `test-env` (Linux amd64, Go 1.27.1, private PostgreSQL 17.12, XFS fixtures)
  passed `go build -p 2 ./...` and the selected race-enabled library, server,
  artwork and tasks tests (233 tests and subtests; zero failures or skips).
  The full diagnostics package also passed with `-race`.
  Replay from the committed source with `GOBY_TEST_DATABASE_URL` pointed at an
  owned test database and both temporary-directory variables set to the chosen
  fixture filesystem. The test selector was
  `LocalBackdropReread|RereadLocalBackdrop|LocalNFOReread|RereadLocalNFO|ScanImages|ImageCatalogChanges|LocalMetadataIngest|HTTPFastIngest|Artwork|OnlineImage|RequestLog`.
  The raw run and closeout logs are retained on `test-env` under
  `/var/tmp/goby-local-backdrop.W00b4p/logs`; earlier failed attempts are kept
  separately from the accepted run. Test acceptance is not permission to deploy
  or alter production files.

## Local evidence and delivery boundary

These workspace-relative receipts are in an ignored directory. They are local
handoff pointers, not files included in a fresh clone:

```text
.artifacts/priority-library-repairs-20261127/pending-artwork-readonly-recheck-20261128.json
.artifacts/priority-library-repairs-20261127/ghost-mouse-production-application-20261128.json
.artifacts/priority-library-repairs-20261127/what-if-production-application.json
.artifacts/priority-library-repairs-20261127/emby-nfo-identity-lab-20261128.json
.artifacts/modern-family-nfo-replacement-20261127/production-application.json
.artifacts/local-nfo-reread-deploy-20261127/result.json
```

Leave unrelated modified, deleted, and untracked workspace files untouched.
Build and verify subsequent work from committed `main` in an isolated task
worktree, not from an overlay of that dirty workspace. No earlier production
approval, scan exception, or exclusive file window carries forward.
