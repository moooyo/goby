# M2b local NFO increment verification

Date: 2026-09-09, Asia/Shanghai. This is an ingestion increment within M2b, not completion of artwork, entity indexing, playback, or full Emby compatibility.

## Delivered behavior

- Bounded UTF-8/XML NFO parsing for movie, television, season, album, and artist document types; scanner discovery currently covers movies, episodes, real series/season directories, and albums.
- PostgreSQL migration `0003` persists the local metadata, source SHA-256, and root-relative source path separately from media probe data.
- Scans reread sidecars when media probing is cached. Valid changes preserve item IDs and probe metadata; removal restores scanner-derived values; malformed, unsafe, or inaccessible documents preserve the prior valid override with a warning.
- Folder descriptions and metadata commit atomically through the existing catalog ownership connection. File/directory identity checks prevent a moved parent from being mistaken for deliberate sidecar removal.
- Existing ACL-filtered item, detail, and Latest queries carry metadata from the selected catalog item. Episode season conflicts cannot reparent an item through descriptive metadata.
- HTTP projections follow four new real reference captures for default/selected fields and `GenreItems`/`TagItems` shapes. Persisted facet/person identities remain pending and are explicitly omitted.

Implementation details and example files are in [local metadata](local-metadata.md). The [implemented API surface](../api/implemented.md) identifies current compatibility limits.

## Build and test environment

The user authorized local compilation only. `go build ./...` passed on Windows after the integrated source changes. No local functional tests, application execution, or media probes were run.

Tests ran through `ssh test-env` using Go 1.27.1, PostgreSQL 17.11, FFmpeg/ffprobe 9.0.1, the protected test environment, and dedicated RAM-backed Go caches. The source snapshot was unpacked into `/opt/goby-test/verify-nfo`. PostgreSQL tests created and removed only their own randomly named schemas.

| Remote command scope, all with `go test -race -count=1` | Result |
| --- | --- |
| `./internal/database` | Passed, 1.110 s |
| `./internal/identity` | Passed, 14.620 s |
| `./internal/library` | Passed, 4.400 s |
| `./internal/media` | Passed, 2.308 s |
| `./internal/metadata` | Passed, 1.716 s |
| `./internal/server` | Passed, 36.878 s |
| `./internal/config`, `./cmd/goby` | Compiled; no test files |

The library package includes eight new scanner cases plus query/Latest metadata coverage. Cases include preferred/fallback selection, cached-probe refresh, deletion, wrong kind, malformed/oversized/DOCTYPE documents, symlink and FIFO rejection, folder kinds, season conflicts, root series metadata, and preservation when a media directory moves during probing. Existing ownership, cancellation, stable-ID, root-containment, and ACL tests also passed.

The HTTP DTO tests cover numeric JSON types, selected/omitted fields, UTC timestamps, explicit zero values, empty objects/arrays, credit ordering without source mutation, omitted unpersisted entity IDs, ignored sidecar paths, and existing Path/image-switch behavior.

A final reference-driven change sorts only `TagItems` by name while retaining the input metadata. The targeted `go test -race -count=1 ./internal/server -run TestMetadataDTO` passed in 1.022 s after that change. The final Windows build also passed, and the final source was redeployed with readiness confirmed. Unrelated packages were not rerun for this isolated projection change.

The independent artwork renderer was excluded from this NFO source snapshot and is not delivered as an HTTP feature by this increment.

## Reference evidence

The isolated official Emby 4.9.5.0 instance supplied 24 additional NFO/image exchanges, bringing the fixture set to 89. The original 65 JSON fixtures were unchanged. The remote export audit preserved original headers and JSON types/numbers and checked that credentials were absent from text and decoded image bytes. See the [reference report](../research/reference-server.md#m2b-local-artwork-and-nfo-extension).

The new samples corrected assumptions that populated metadata scalars belonged in the default list and that `Fields=Tags` returned a string array. They also establish the need for numeric facet IDs and string person IDs in a later entity-indexing increment. Date-only NFO input has a recorded timezone-policy difference: Goby currently uses UTC, while the reference interpreted the captured date in its Asia/Shanghai host timezone.

## Deployed workflow

The source was installed through the existing owned deployment script, which migrated the test database and restarted only `goby-foundation-test.service`. The service became ready on `127.0.0.1:18096` as the unprivileged `goby` user.

[nfo-smoke.py](../../scripts/test-env/nfo-smoke.py) passed its first and only remote run in 1.65 s. It used the existing protected synthetic credentials, real administrator/Emby HTTP endpoints, and a separate marked fixture directory. The running service had effective UID 995. Real ffprobe output matched the synthetic H.264 160x90/AAC file, its approximately two-second duration, and its actual byte size.

| Stage | Scanned / added / updated | Result |
| --- | --- | --- |
| Valid NFO | 1 / 1 / 0 | Title, year, overview, genres, and provider IDs visible through list Fields and item detail |
| NFO-only update | 1 / 0 / 1 | New metadata applied; item ID and technical probe fields stable |
| Malformed NFO | 1 / 0 / 0 | Previous metadata retained; completed job contains a warning |
| Sidecar removal | 1 / 0 / 1 | Filename-derived values restored; local metadata cleared |
| Native catalog deletion | Not a scan | Media file preserved; `/readyz` returns 200 |

The script removed only its own marked directory, removed the temporary library, revoked its sessions, and reported no cleanup errors. Original source SHA-256 before and after was `7763ca57b5f9285d3e0036250f7aab418832faa149f9dca7f0ef5545fbb0948a`. HTTP field stability does not prove ffprobe invocation counts; the Go scanner tests provide that separate evidence. The later tag-order-only change was covered by its targeted DTO test and final readiness check rather than repeating this unchanged scan workflow.

No real third-party client playback was tested in this increment. The absence of a GPU on `test-env` still prevents actual hardware decode/encode verification; that does not block local metadata ingestion.
