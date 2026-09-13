# TV browse01 evidence and contract review

The [one TV browse execution](audited-tv-browse01-execution-decision.md) completed
the visible library, series, two season selections, Episode 2-1 detail, Home
return and logout workflow. The observed layout is a cross-season series list;
strict season filtering is not claimed. The original browser exit remains 1,
with `candidate_item_detail_binding_changed` and one page error. Full TV
acceptance remains open.

The [owned-state closeout](audited-core-tv-browse01-owned-state-closeout.json)
passed from saved evidence only. It compares all 35 tables and sequences: 30
tables are exact, all fifteen sessions are revoked, all six prior movie play
rows and the movie userdata row are exact. The new TV session/device, two audit
entries, unstarted Prepared row and zero-history userdata are fully explained.
The preparation is bound to physical exchange 277. Both workers are closed;
the candidate, PostgreSQL, lease and stdout prefix remain exact as recorded.
No Playing or media request occurred. Neither failure nor retained state is
reset to manufacture a fresh actor.

## Three distinct findings

1. **The adapter confused a derived catalog relationship with an observed DTO
   field.** Browser request 272 and physical exchange 274 contain the same
   complete Episode 2-1 response. Its decoded body is 3,017 bytes with SHA-256
   `3de8340f54d6cc0ec6f8e37def8755ec96a10fc2ab81568653a70f0e95d6c043`.
   Id, Type, Name, Path, ParentId, IndexNumber, ParentIndexNumber and RunTimeTicks
   all match the frozen catalog; SeriesId is absent. The seed mapper explicitly
   derives `seriesId` from the episode-to-season-to-series parent edges. The
   adapter incorrectly required the response to contain that derived field.
   The pre-execution static review missed this representation mismatch.
2. **A concrete product DTO difference also remains.** An already retained
   reference Episode detail returns SeriesId, SeasonId, SeriesName and SeasonName;
   Goby's current item mapper does not provide those fields. The existing schema
   description does not establish a field-level required promise or prove that
   every client tolerates omission. Correcting the adapter's provenance error
   does not close this M2/M3 compatibility difference. Resolve the product
   mapping from authorized parent relationships and verify its access/data
   boundaries separately; do not add fields solely to silence a test helper.
3. **Diagnostic completeness is classified too broadly.** The page error was
   recorded at 15,428 ms. At finalization, the unrelated non-credential detail
   assertion made `authorityComplete=false`, so the collector discarded its
   name/message/location. That error text cannot now be reconstructed. A narrow
   exception may preserve redacted diagnostics only for that exact assertion
   after completed body parsing/private capture, bound to a known-item GET/read
   200 for this actor. Credential collection must still be complete and all
   observation work drained. Login, header, body, save, unknown failure and
   timeout cases continue to suppress diagnostic text. Unknown page errors
   remain failures requiring review.

The nearby `/LiveTv/Programs` request returned a complete physical 404. Its
proximity is a lead, not causal proof. The error predates the Episode 2-1 request
at 18,040 ms, so this later response cannot be its direct trigger. Do not add
Live TV functionality or reopen global NextUp research on that timing alone.

## Saved evidence

Current paths below are beneath
`/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/candidate-core-client-tv-browse-01/`.

| Artifact | SHA-256 |
| --- | --- |
| `browser/observation.json` | `8861b40b9751c756a0dcf312a935747220fc2ae99f7eb447ce8add8927d5b87a` |
| `browser/summary.json` | `05b96bd0b3282a919bcd4341efc34a89d9978cc9e1eea6dafb132bcfab754572` |
| `browser/response-272.json` | `7f6faac8b0c13dd23d1f59eb363bd492b7bf881b06ecc6da50ccc11aefdf4f79` |
| `private/adapter-input.json` | `d42b7c854179a2847ff57fbbacc84110d5f399afbaa947efab296b2dc2d783b0` |
| `gateway/index.json` | `bc1db0ae906babfad15da1a612b53917d55b6201cdd2d6ab4da00039f6e56b0e` |
| `private/failure.json` | `be26f4958725b524d88c8e790775cce09556e8d157b11f427583655b5c24561d` |

The reference wire artifact is
`/opt/goby-test/exec-work-m3e/nextup-global-reference-runs-05/matrix-05/private/0019-R0-P-detail-A1-wire.json`,
SHA-256 `8d1a6a3a3168430f520e8fcb157678c6c770ac15d816d920e12d845d4409e837`.
It contains a complete, untruncated 200 Episode detail; decoded body SHA-256
`93f904ab0abaa23610bc1b8ccdefd60738aed152fa33a0b394616277b8b91892`.
It is retained historical evidence, not a new reference request or current
reference-service identity check.

## Next boundary

The shared adapter correction now [passes 62 remote checks](audited-tv-browse01-adapter-verification.json):
55 adapter tests and seven checks using the actual saved response/failure
metadata. The requested item and observed parent fields remain bound;
contradictory SeriesId is rejected when present. The diagnostic exception is
restricted to the exact known-item assertion after body capture, with unique
request/failure binding and otherwise complete credential/observer state. It
does not change formal failure, unknown-error review or credential redaction.
The saved error text remains unavailable. The synthetic future-error regression
is not a reconstructed cause of that historical error.

Keep client execution paused while addressing the concrete product DTO
difference. Use the existing authorized library read transaction and a bounded
parent projection shared by detail/list/batch reads; do not perform a separate
query per DTO. Only actual same-library ordinary Season/Series relationships
may supply IDs and stored names. Wrong-type, hidden, auxiliary, reserved or
cross-library parents must not expose metadata. Legal virtual parents can have
empty filesystem paths. Define detail and requested-list-field behavior from
the existing DTO contract, and run targeted authorization/mapping regressions
plus the required final product verification before selecting a new artifact.
No schema migration or Live TV expansion is implied.

The live controller still pins the earlier adapter. Integrate the final selected
product and verified tools under a fresh reviewed input; do not substitute
hashes in the consumed TV or movie input. No TV retry or audio run is authorized
by this review.

The TV actor now has retained preparation and userdata, so its consumed null
baseline cannot be reused for another TV run. Its future admission needs an
explicitly reviewed closed-state contract, as does movie. Other actors remain
independent once their shared tool prerequisites are resolved. Product DTO work,
the unknown page-error cause, remaining core journeys and main recovery/upgrade
all stay open; M2-M6 scope is unchanged.
