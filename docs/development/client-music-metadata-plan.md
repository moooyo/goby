# Real music metadata and artist relationships

Status: runtime acceptance in progress. The running isolated candidate is
source15/schema25; 46 targeted tests, build and the protected upgrade passed.
The controlled [music rescan](m3e-music-scan.json) and preservation proof passed.
Original-client audio acceptance and full-source regression remain open.
All verification and media probing run on `test-env`.

## Observed client gap

The original-client source12 album response contains the four music arrays and
the actual `ChildCount=2`, but those relationships are empty. The earlier caught
exception reading `0` has changed to one reading `Name`, and a screenshot still
shows an empty album content area. This is evidence of an incomplete client
contract, not proof of the exact JavaScript member being read. The implementation
will index the fixture's real embedded tags and expose real artist identities;
it will not invent names or identifiers to satisfy a renderer.

The synthetic media already contains distinct title tags, a common album tag and
a common artist tag. It has no observed `album_artist` tag. The original media
files, physical item IDs and directory relationships remain the source of truth.

## Accepted implementation scope

- Retain technical `ProbeVersion=6`. Independent music metadata version 1 records
  accepted `format.tags` title, album and artist facts, with bounded UTF-8 values
  and deterministic treatment of case-colliding keys. Do not guess multi-artist
  separators. Existing video snapshots remain usable, and an ordinary music
  scan refreshes old audio tag caches without first blocking audio playback.
- Feed accepted tags through the automatic/effective metadata pipeline. Existing
  name overrides and locks retain precedence. Audio titles use accepted title
  tags; an album uses a tag-derived label only when its accepted members agree.
  Mixed or missing labels use the physical directory fallback. Item IDs, paths
  and parent IDs do not change merely because a label changes.
- Store accepted music source data separately from NFO source data. Aggregate
  album metadata after successful root processing, retaining known state on
  failed or cancelled scans. Folder processing must not erase album source
  information by clearing its technical media descriptor.
- Represent artists through the existing catalog entity system. `MusicArtist`
  is a distinct kind, with stable numeric identity; `Artist` and `AlbumArtist`
  are relationship roles. A uniform real artist among album members may supply
  a derived album-artist relationship as an explicit Goby policy. Missing or
  mixed artist data never becomes a fabricated first or unknown artist.
- Project music arrays from those indexed relationships. Audio album references
  use an actual same-library album, including its effective display label.
  Related-item filters and numeric entity details retain current library ACLs,
  counts and pagination. An ID must resolve to its real authorized identity.

Native music metadata editing, provider lookup, arbitrary tag formats and the
complete upstream artist endpoint surface are outside this increment. Composer
extraction is not claimed. A stored session capability profile does not replace
an omitted PlaybackInfo request profile.

## Schema25 contract

Append migration25 without rewriting any earlier migration or catalog23/24.
The accepted schema changes are:

- Extend the catalog entity kind constraint with `MusicArtist`.
- Add a bounded `credit_group` smallint: legacy relationships use 0, Artist
  uses 1 and AlbumArtist uses 2. Include this group in the item/entity/position
  primary key. Keep the existing unbounded `credit_type` text unchanged; putting
  that text directly into the index could reject valid old long values.
- Extend entity synchronization for the accepted `Artists` and `AlbumArtists`
  string arrays while preserving the four existing entity kinds.
- Add `item_metadata_state.music_source` as non-null JSONB with exact default
  `{}`. The database still has 30 tables. Existing metadata rows gain only the
  empty source value, and existing relationships gain only group 0; all their
  preexisting columns and IDs remain intact.

The candidate upgrade operator must explicitly verify both new-column defaults
and compare all old row columns. It must also bind catalog25 to a reviewed source
manifest and caller-supplied digest. An unknown result retains its evidence and
is not automatically retried.

## Verification sequence

The [73 runner guards](m3e-schema25-runner-guards.json) and [catalog25 bootstrap](m3e-schema25-catalog.json)
have passed remotely. The latter used source13 solely to generate the actual
PostgreSQL definitions; it is not a product-source or old-data restore result.
The trusted catalog SHA-256 is
`e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b`,
bound to migration25 SHA-256
`3b21cac79173b4b9184d73b76066aff4199a4d6d4ad6fc64d2d0d826796b6eaf`.
The independent [source14 schema/recovery selection](m3e-schema25-targeted.json)
then passed 12 tests, and [all 37 candidate upgrade guards](m3e-schema25-fixture-guards.json)
passed against that real catalog. Source14 is a schema/backup verification source,
not the complete music product source. The complete [source15 product candidate](m3e-source15-targeted-upgrade.json)
then passed its targeted tests and protected replacement. The [42 scan guards](m3e-music-scan-guards.json)
and actual normal scan subsequently passed, preserving original media, item IDs
and user state. Remaining client acceptance and full-source regression are pending.

1. Freeze migration25 and an independent catalog-generation source. Run the
   bounded catalog operator against newly created, verified empty databases.
   Export the actual PostgreSQL 17 definitions after only the trusted migrations;
   verify version, migration hashes, objects and target emptiness before cleanup.
   Keep the generated artifact outside the immutable bootstrap source.
2. Include that trusted catalog in a complete later source snapshot. Verify
   extraction, cache refresh, source precedence, album aggregation, stable
   identities, current ACLs and filtered pagination on the remote test cluster.
3. Verify raw and encrypted old-backup transitions, including schema24 to 25,
   schema23 to the current schema, and current-version music source/relationship
   preservation through apply, reopening and rollback.
4. Verify the upgraded fixture and runner guards, then replace the isolated
   candidate while preserving the three existing accounts and their state.
5. Run the owned music library scan. Compare original media hashes, item IDs,
   parents, movie state and existing audio user data before and after the scan.
   Record legitimate tag-derived names rather than rewriting the media or
   retaining obsolete test-driver labels.
6. Run fresh original-client MP3 and FLAC album/track/playback journeys. Client
   playback and logout evidence remain separate from metadata and API tests.

The source12 full regression and its UI evidence remain historical scoped
results for that exact source; they do not validate the pending schema25 code.
