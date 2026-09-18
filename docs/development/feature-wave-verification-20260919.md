# Feature wave verification — September 19, 2026

This record concerns the new media, collections, and management wave. It does
not resume OCI or replace any earlier accepted or failed execution record.
All execution below is on `ssh test-env`; there were no local tests, builds,
validators, browser checks, or media probes. Some delegated source formatting
initially ran locally before the stricter toolchain document was read; local
formatting then stopped, and consolidated formatting runs on the remote host.

The user explicitly deferred testing of online TMDB, MusicBrainz, and
OpenSubtitles integration. Their source is still compiled. The `internal/providers`
package and named online-provider tests are excluded from this wave's
acceptance. Shared offline selection/authorization tests can run with their
remaining packages; they do not exercise the external services or establish
actual service support.

## Isolation and source

The verification scope is a newly owned directory, independent PostgreSQL
instance, and disposable databases. Existing Goby and PostgreSQL services are
retained. Worker units have 3 GiB memory, zero swap, a 512-task ceiling, and a
two-CPU quota; commands run serially. Source copies, original output, terminal
receipts, and private environment files remain under the local Git-private
work directory and the owned remote scope. Public projections omit credentials
and raw database/request data.

Initial candidate source tree: `747799530a980e0ed7d46b64d07e00532e4143e2`.
The subsequent source tree including remote formatting, the published migration
append, schema catalog, and browser harness is
`a7e341172eea4f1b79c12edbc3cb55da58ea1766`.
Later fixes need their own source comparison and verification evidence.

The intermediate integrated source snapshot before its remote formatting patch is
`cf31deab6b5835c6478eb9914a44a385866f7435`. Overlay 06 contains 57 changed
Go files, including the final authority, collection, HLS clock, recovery
fixture, and browser-harness changes. Remote formatting completed and its
71020-byte patch was applied back to the development checkout. Overlay 05
was prepared but never executed; it is not a verification source.

## Observed results

| Operation | Result | Scope |
| --- | --- | --- |
| Tool inventory | Go 1.27.1; FFmpeg/ffprobe 9.0.1; PostgreSQL 17.11; Node 20.19.2; npm 9.2.0 | Actual remote version commands |
| Remote formatting | Completed | Initial 993 Go files; original stderr retained |
| compile01 | Failed before compilation | Isolated worker lacked explicit GOPATH/GOMODCACHE; original failure retained |
| compile02 | Passed and closed | `go test -run=^$ ./...` compiled packages/test binaries; no test cases selected |
| catalog01 | Passed and closed | Fresh schema 35, 42 tables, six sequences, actual PostgreSQL catalog export |
| full01 | Failed and closed | 8093 pass, 100 fail, one skip events including subtests; original package results and output retained |
| npm-ci01 | Passed and closed | Locked administrator dependencies installed in the owned verification directory |
| frontend-build01 | Failed and closed | TypeScript rejected a Material UI Link property; original output retained |
| frontend-build02 | Passed and closed | TypeScript and Vite production build after the property correction |
| focused01 | Failed and closed | 492 pass, 11 fail, zero skip events including subtests; remaining fixture defects and missing zscale identified |
| hdr-configure01/02 | Failed and closed | Initial worker command resolution and private hardware-header search path prerequisites corrected |
| hdr-configure03, hdr-build01, hdr-install01 | Passed and closed | Private FFmpeg 9.0.1 build with libzimg; existing service toolchains unchanged |
| hdr-accept01 | Passed and closed | Actual PQ/HLG HDR pixels and color metadata, cadence, and deinterlacing; four pass events, no failure or skip |
| backup-accept01 | Failed and closed | 671 pass, 15 fail, zero skip events; historical timestamp serialization and public recovery fixture cleanup issues identified |
| focused02 | Failed and closed | 244 pass, six fail, zero skip events; encoded subtitle clocks passed, copied-stream clock and contaminated backup-pair prerequisites remained |
| recovery-accept02 | Failed and closed | 46 pass, 12 fail, zero skip events; a fresh pair confirmed the schema-29 fixture cleanup omitted schema-35 objects |
| store-accept02 | Passed and closed | Complete recoverydb package using its own empty pair; 177 pass events, zero failure or skip |
| server-full02 | Failed and closed | 676 completed top-level passes and ten completed top-level failures; 2602 pass/14 fail events including subtests, then the unchanged 20-minute package timeout interrupted the WebSocket tail |
| browser-mock01 | Passed and closed | Five management/playlist/policy browser scenarios; the two provider-specific scenarios excluded, no production API proxy |
| server-repair03 | Passed and closed | All 127 selected top-level tests, 455 pass events, zero failure or skip; includes the interrupted WebSocket tail and every server failure from full02 |
| core-full02 | Passed and closed | Ten complete packages: activity, database, events, dynamicsource, library, playback, settings, subtitle, tasks and transcode; 5136 pass events, zero failure, one existing mount-namespace helper skip |
| identity-policy02 | Passed and closed | Current managed-user, policy, runtime fallback, feature and copying scopes; 181 pass events, zero failure or skip |
| browser-live01 | Failed and closed | No business stages completed: its legacy probe fixture lacked current probe-version/ctime facts, so the required original-download baseline correctly returned 503 |
| backup-accept03 | Passed and closed | Complete backuppg package on its new owned pair; 497 pass events, zero failure or skip, including exact historical data and nonempty schema-35 archive restoration |
| browser-live02 | Failed and closed | Three stages completed; duplicate accessible Close buttons made a test locator ambiguous after saving user policy |
| browser-live03 | Passed and closed | All seven real HTTP/UI/database stages, unchanged original media, actual completed global cache execution, no online provider requests, all owned sessions logged out without fallback revocations |
| browser-regression02 | Passed and closed | Four existing mock scenarios: three media-diagnostic settings workflows and pending root-approval navigation protection |
| recovery-accept03 | Failed and closed | 69 pass/four fail events; exact business-history JSON comparisons used different time-zone representations after connection reopening |
| recovery-accept04 | Passed and closed | Complete recovery manager package on a fresh pair; 73 pass events, zero failure or skip, including historical/current apply, restart, acceptance and rollback |
| source10 reconciliation | Passed | All 1178 selected product, test, frontend, module and toolchain-input files exactly match their Git blobs in the intended source tree |
| build-final01 | Passed and closed | Ordinary and embedded-dashboard Linux binaries built with Go 1.27.1, CGO disabled, trimpath and explicit source binding |
| Resource closure | Passed | All 33 recorded worker invocations terminated, owned PostgreSQL stopped, five protected process start/executable identities unchanged |

The schema-35 catalog is 564138 bytes, SHA-256
`3e37870add52dd752188089dbdd8661967a556140904e4329667952800f5d498`.
Publication readback matched the original generated bytes. This establishes
the catalog artifact, not backup/restore acceptance by itself.

The first regression identified outdated historical migration assertions and
backup fixture prerequisites: independently owned, unprivileged roles and
`goby_backup_` database names. It also found a ServeMux attachment/image route
conflict, playlist move parameter typing, application-key state-write authority,
and fixture/projection issues. Fixes preserve historical row/object invariants
and the original failures. The single skip is recorded in the original output;
it is not silently treated as a pass.

The owned PostgreSQL instance now has a password-authenticated loopback endpoint
and separate unprivileged backup roles/databases. No existing PostgreSQL service
was restarted or reconfigured. Recovery tests retain their original endpoint by
default and require an explicit test-only port override for this owned instance.
The administrator production build passed. The initial focused regression found
two application-key/download fixture prerequisites, subsequently corrected and
reverified. HLS subtitle review identified the need to measure the actual mux
timestamp shift rather than assume zero; its final real subtitle/media clock
comparisons passed in server-repair03.

The private FFmpeg build uses the official 9.0.1 source archive (SHA-256
`cf38e0e28c7e5605942c4a77755349b0145804a397af37eb1fb4c77cb237f635`),
verified by the release signature from
`FCF986EA15E6E293A5644F10B4322F04D67658D8`. The required zscale, tonemap,
bwdif, subtitles, and overlay filters are present. The installed ffmpeg binary
has SHA-256 `55f896cc1551f6cad52b618c5f3a0a74c6f8a27afd3290a8ca1076b1ca7372fe`;
ffprobe has SHA-256
`f55bc45d863e9f9edddcb0a5c377870c8f13bc064ce056938cc99263459aacc8`.
These facts and the actual HDR test receipt are retained separately from the
original toolchain failure.

Historical archive witnesses now use the restoration transaction's canonical
output settings: the worker database defaults to Asia/Shanghai, while restore
uses UTC. The exact timestamp values and complete historical columns remain
part of the comparison. Recovery manager fixture cleanup also needs the current
authenticated catalog rather than the old schema-29 table/function list; the
fresh pair demonstrated that the seven new tables and three functions remained
after that old cleanup. Previous pairs and their failure evidence are retained.

Source review additionally found and repaired visible-index playlist ordering,
hidden-member removal, ancestor collection invalidation, and deletion-worker
shutdown ownership. Queued remote commands retain and revalidate their original
sender session. Original media responses acquire an independent known-expiry
deadline, and user-state/playback transactions recheck current credentials and
time-dependent policy after business-row waits. These repairs passed the later
integrated core and server scopes; earlier passing results alone did not accept
the changes.

The updated HLS subtitle code measures an actual first-packet clock before and
after muxing. FFmpeg supplies pre-mux statistics for encoded streams, but omits
them for copied streams; a bounded single-packet framehash side output covers
the latter. The original real decoded-frame alignment assertions remain in
place, and copied-stream/adaptive-rendition acceptance passed in server-repair03.

The server run confirmed copied TS/fMP4 subtitle alignment, then found a
multi-rendition FFmpeg statistics-pipe conflict. Each rendition now has an
independent bounded pipe. It also exposed normal completion incorrectly
cancelling reusable progressive output, unconditional catalog resync during
unrelated user deletion, and original-file correlation incorrectly rejecting
an already stopped play session. Source repairs preserve the existing output,
state, quota, and peer-isolation assertions. Legacy positive image fixtures now
authenticate, and the queued-command authorization fixture creates real
identity-layer sessions without consuming its unrelated shared HTTP login
budget. Neither production authentication nor rate limits were relaxed.

Overlay 07 starts from source tree
`887746f06c78b8b5d33e47d74e03a1cedfb07137`, followed by its remote formatting
patch. It contains 27 Go files. The server repair selection includes all
previously failing, new or incomplete cases and the affected media lifecycle
families: 127 top-level tests. Earlier completed passes remain scoped to the
unchanged behavior they exercised; the timed-out WebSocket test is not counted
as a completed pass. The repair run completed all 127 selected tests, including
all actual copied/encoded/adaptive subtitle clock comparisons. Together with
the unchanged completed server cases, its selection covers all 692 currently
in-scope top-level server tests. The provider-specific server test remains
outside that count. The first selection summary counted the package-level
failure as an empty-name failure; this did not change selection, and the actual
top-level failure count was ten.

The browser-accepted source tree is `ecc3a0c15e748d9376285653bc91c5f0a84cc2e8`.
After the accepted product-code regression, the only semantic changes were to
the browser harness: it now uses the existing current HTTP probe fixture and
checks indexed probe version, file ctime/size and download admission before
launch; its two Close locators explicitly select the footer text button.
No product authorization, source validation, browser state assertions or
media-preservation checks were relaxed. The real browser artifact inventory
matches the complete frozen administrator build. Collection, settings and
completed-task screenshots were visually inspected.

The current source tree is `e5a74ccbea84988c318acefb61fae2102d9b3fe7`.
Its only subsequent changes are the recovery test witnesses. Reopened
production pools retain their ordinary configuration; test-only read-only
transactions now apply the same canonical timestamp, date, interval, bytea
and numeric output settings on both sides of every existing complete-history
comparison. Original fields, ordering, exact numbers, rollback and acceptance
checks remain intact. The fresh-pair recovery replay passed in full.

## Final artifacts and acceptance

The ordinary binary is 34109327 bytes with SHA-256
`40a93c113020a69a8ee0deef6661d37c568a28b871c8da597b233a3d5c6135c5`.
The embedded-dashboard binary is 35333451 bytes with SHA-256
`08897e758c1d9f44d42d86e253b48d4a19646a0f7a8ab6830e6697dba6e257cf`.
They remain in the owned remote evidence scope. Temporary FFmpeg object files
and duplicate build binaries were retired only after checking that the verified
installed tools stayed byte-identical; signed source, receipts and final
artifacts remain available.

The selected wave's functional, integration, browser, build and resource gates
are complete within the documented software profile and user-authorized scope.
The original failures and timeout remain failed records. Their passing
successors establish the repaired behavior without rewriting those attempts.
Provider-service acceptance remains user-deferred, and the existing opt-in
mount helper skip does not become a passed native-capacity profile. This wave
does not accept OCI deployment, GPUs, every external client, or the original
complete M2-M6 release objective.

## Integration

Feature commit `39893aa` was fast-forward merged into `main` and successfully
pushed to `origin/main`. Its selected source files match the accepted source
tree; the following closeout edit changes documentation only. Unrelated OCI,
packaging, client-helper and historical-draft work remains outside the wave's
commits. The current repository handoff records the delivered scope and the
remaining project decisions.
