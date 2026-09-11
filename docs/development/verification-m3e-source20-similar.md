# Source20 Similar and album-artist checkpoint

This partial M3e checkpoint implements reference-constrained Similar queries
and explicit album-artist metadata. It is accepted on the isolated candidate;
the primary service remains source18. ThemeMedia and complete M3/M4/M5/M6
acceptance remain unfinished. All verification ran through `ssh test-env`.

## Product behavior

`GET /emby/Items/{Id}/Similar` authorizes the seed and complete same-type candidate
population before scoring and pagination. Current metadata controls, artist
roles and UserData share one read snapshot. Returned counts describe the page;
Movie zero-limit and music zero-limit behavior follow the captured differences.
Explicit sorting overrides score order and equal-score order is randomized.
The [working scorer](client-auxiliary-reads-plan.md) fits the controlled reference
fixtures and does not uniquely reconstruct Emby's private ranking algorithm.
Entity seeds and unindexed membership filters remain explicitly unsupported.

Music probe version 2 accepts the bounded scalar `album_artist`. Technical probe
version 6 and stored music-source version 1 remain compatible. Own album-artist
credits precede physical-album fallback, and incomplete/conflicting album
members cannot publish a guessed uniform artist. See [music metadata](music-metadata.md).

## Verification and candidate

[Targeted tests](m3e-source20-targeted.json) passed 43 cases. The unchanged
1,050-candidate regression took 377.86 ms to construct and 85.96 ms to query in
that run; these observations are not a general latency guarantee.
[Complete verification](m3e-source20-full.json) passed 1,763 race tests across
24 packages, with zero failures/skips and complete owned database cleanup.

The frozen source manifest is
`456f90464a0c18d8f1a31e268a67a5c65dcf1264a7ee69fc45be4d085d94e945`.
The executable SHA-256 is
`a5028f865d3638f020c758a77bf1d431ca755699b7767a25b711509a8f8c12a9`.
The [candidate replacement](m3e-source20-candidate-upgrade.json) preserved all
existing business rows and sequences across 30 tables, plus runtime, recovery
and credentials. Candidate PID 581075/start ticks 3900536 is separate from
the unchanged main source18 PID 539535/start ticks 3115871. Music validation
uses the explicit source15-to16-to18-to20 chain, with six lineage guards passed.

[Staged-source reconciliation](m3e-source20-staged-source-check.json) found
3,186 byte-identical product/test files and one reviewed JSON test-fixture
normalization: CRLF became LF, with exactly equal parsed JSON. Its only consumer
decodes JSON in a test; it is not production-embedded. All production source,
17 browser files and eight executed reference/operator files matched exactly.
The initial byte-comparison failure is retained, and no 3,187-file byte-equality
claim is made. Later changes added only this evidence and documentation.

## Original-client scope and remaining failure

The [fourth browser observation](m3e-source20-auxiliary-album.json) established
the requested album URL, both exact track rows, and an owned Similar HTTP 200
whose transfer completed. Four item UserData projections, user configuration,
policy and saved preferences remained equal. There was no playback. UI logout
returned 204 and the same token was rejected with 401.

The original run remains failed at album-wait: ThemeMedia returned 404, its
transfer completion was not observed, and one page error remained. Home was
not reached. The Similar response is narrower passing evidence; the workflow
was not relabeled as complete. The error timing follows ThemeMedia headers and
precedes Similar headers without an explicit error-to-request correlation.

[Three earlier browser attempts](m3e-source20-auxiliary-album-failed-01-03.json)
stopped before auxiliary reads. The synthetic root album omits Path, and the
original client's card omits data-id/type. The corrected harness requires the
post-navigation owner, track and Request identities, retaining absence as
unknown. All four original browser sessions logged out and closed. No history
reset or playback replay occurred. [Final harness evidence](m3e-source20-browser-guards-final.json)
records 11 pure mock tests and the complete current dependency closure; it is
separate from real UI acceptance.

The next work is real theme-resource ingestion, persistent hidden classification,
independent song/video inheritance and guarded delivery. Uncommitted Theme
owner/path prototypes and the main-upgrade preflight draft are outside this
schema25 checkpoint and have not been verified or deployed.
