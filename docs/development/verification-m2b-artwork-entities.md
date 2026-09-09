# M2b artwork and catalog entities verification

Date: 2026-09-09, Asia/Shanghai. This increment delivers indexed local artwork and persistent genre/tag/studio/person identities. It does not complete the playback pipeline, the entire Emby query surface, generated artwork, or release compatibility testing.

## Implementation and reference evidence

Migration `0004` backfills stored NFO metadata into stable entities and item associations. Live metadata changes update both within one catalog ownership transaction. Numeric item facet IDs and string list/person IDs follow the recorded wire contracts. Every entity query joins visible source items before counting and pagination; orphan identities remain stored but cannot be browsed without a visible association.

Migration `0005` stores validated local artwork independently from media probe data. Scans detect image changes even when media probing is cached, preserve prior images of a failing type, and index directory names once per held directory snapshot. The public binary API and authenticated image enumeration follow the observed reference distinction. Hash/identity checks precede cache hits and 304 responses. The renderer, I/O worker gate, HTTP admission slots, and byte-capacity LRU bound concurrent and retained work.

The fixture set now contains 107 exchanges: the original 65, 24 NFO/artwork captures, and 18 new entity navigation/filtering captures. The new remote audit preserved the preceding 89 raw/exported files by SHA-256, maintained original response headers and JSON types/numbers, and checked credential redaction. Reference evidence establishes list/detail envelopes, numeric versus string ID usage, name/ID navigation, same-dimension OR, cross-dimension AND, negative filters, and totals before pagination. See the [reference report](../research/reference-server.md#entity-navigation-and-filtering-extension).

## Builds and Linux tests

The authorized Windows command `go build ./...` passed on the integrated source. No functional tests or runtime/media probes ran locally.

The complete source snapshot was transferred to `/opt/goby-test/verify-m2b` on `test-env`. With the existing protected environment and RAM-backed Go caches, this command passed on its first run:

```sh
go test -race -count=1 -timeout=180s ./...
```

| Package | Result |
| --- | --- |
| `internal/artwork` | Passed, 2.561 s |
| `internal/database` | Passed, 1.288 s |
| `internal/identity` | Passed, 15.523 s |
| `internal/library` | Passed, 8.216 s |
| `internal/media` | Passed, 2.628 s |
| `internal/metadata` | Passed, 1.759 s |
| `internal/server` | Passed, 56.281 s |
| `cmd/goby`, `internal/config` | Compiled; no test files |

Coverage includes legacy metadata backfill without scanning, 4 KiB entity names, transaction rollback when entity synchronization fails, stable IDs across restart/update, multiple credits for one person, ACL-filtered counts/Latest/grouping, strict ID parsing, and same-credit person/type filters. Display/sort names now have a tested 1,024-byte UTF-8 parser limit to keep accepted metadata within the PostgreSQL sort-index limit.

Image tests cover JPEG/PNG/GIF validation, animation boundaries, transforms, alpha handling, input/output/concurrency limits, directory indexing among 10,000 unrelated names, cancellation and late file-descriptor cleanup, source/root replacement, symlinks/FIFOs, cross-library root corruption, public versus protected routes, ETags/HEAD/304, and stale-cache rejection even when inode, size, and modification time are unchanged. The worker tests simulate blocked work under controlled gates; they do not claim testing a real stalled NFS mount.

## Deployed verification

The same source was installed through the owned test deployment script. Schema migrations completed and only `goby-foundation-test.service` restarted. The service became ready at `127.0.0.1:18096` with non-root effective UID 995. Existing Go 1.27.1, PostgreSQL 17.11, and FFmpeg/ffprobe 9.0.1 were reused; no test software or reference data was changed.

Both scripts below passed their first and only run against that deployment:

| Script | Observations |
| --- | --- |
| [nfo-smoke.py](../../scripts/test-env/nfo-smoke.py), approximately 1.75 s | Valid/update/malformed/removal flows still pass; embedded numeric genre IDs match string list IDs and retained genres preserve identity |
| [artwork-entities-smoke.py](../../scripts/test-env/artwork-entities-smoke.py), approximately 0.98 s | Four entity lists, three name details, four generic ID details, and four ID filters lead back to the movie; image delivery, transforms, conditional caching, replacement, rescan, and catalog removal behave correctly |

The artwork script generated a 96x64 PNG beside a separate copy of the owned synthetic H.264/AAC movie. The returned PNG and 48x32 JPEG transformation were passed to the installed ffprobe through stdin with `-show_frames`; complete frames with the expected codec and dimensions were returned. Public GET/HEAD accepted absent or invalid tokens while image enumeration returned 401. Matching validators produced empty 304 responses; a matching source Tag enabled the one-year cache policy.

Replacing a source image before scanning produced 503 even for already cached representations. A rescan changed the source/variant bytes and tags while preserving item/entity IDs and media probe fields. Removing the library made cached image URLs return 404 and preserved the fixture files before the script's own cleanup. Readiness was 200 afterward.

Both scripts removed their temporary libraries and marked directories, revoked their sessions, and reported no cleanup errors. Shared/orphan entity identities remain in the catalog by design. Source movie SHA-256 before and after both checks was `7763ca57b5f9285d3e0036250f7aab418832faa149f9dca7f0ef5545fbb0948a`. No credentials were written into the repository or summaries.

## Remaining scope

The React/MUI administrator UI was unchanged, so its prior browser evidence was not repeated for these backend-only changes. There is still no consumer web player. Real external client playback, direct media transport, subtitles, session state, conversion, and hardware decode/encode remain subsequent stages. Real GPU execution is unverified because this host has no GPU device.

Generated entity/library artwork, inherited parent art, embedded audio covers, additional image formats/transforms, image mutations, broader query filters, and filesystem reconciliation remain separate work. High-volume upgrade timing also needs attention before release: the current application startup deadline is 30 seconds and ordinary PostgreSQL statements default to 15 seconds, including migration backfill unless that migration execution policy is revised. This test establishes correct backfill behavior for its fixtures, not unrestricted catalog scale.

Subsequent update: the [startup timeout increment](verification-startup-timeouts.md) replaces those startup/migration time budgets with configurable, transaction-scoped limits. Representative large-catalog timing remains release work.
