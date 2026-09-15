# Core artifact source comparison

Status: **source comparison complete; no candidate or client admission**.
Recorded on 2026-09-15 through bounded saved-file reads on `ssh test-env`.

The retained audited client binary and E11 have identical audio, playback,
authentication, media and transcode implementation files in the compared
inventory. E11 nevertheless changes application asset selection and the shared
task/scan lifecycle. These findings support a focused artifact-transition review;
they do not transfer the old client's acceptance to E11.

## Artifacts and evidence pins

| Label | Binary SHA256 | Existing binding |
| --- | --- | --- |
| A: audited client candidate | `b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42` | [TV-parent full verification](tv-parent-metadata-full-verification.json), [MP3 closeout](audited-mp3-client01-closeout.json), [FLAC closeout](audited-flac-client01-closeout.json) |
| B: earlier embedded candidate | `59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312` | [Embedded build](embedded-administrator-verification.json), [fresh candidate admission](fresh-embedded-candidate-checkpoint.json) |
| E11: selected next delivery artifact | `7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1` | [Package build](systemd-package-build-verification.json), [864-input source bridge](systemd-package-source-checkpoint.json) |

The new private comparison receipt is
`/opt/goby-test/review-resume-20260915-artifact-comparison/comparison.json`,
164,151 bytes, SHA256
`0ec51ae5e0f4216b6dcd2600e5fd9789eb91c59bd64672c15802f72a7f4c5bbf`.
It contains full per-file hashes, inventory differences and actual readback
counts. The following immutable inputs were reread against their pins:

| Input | Remote path | Bytes | SHA256 |
| --- | --- | ---: | --- |
| A source manifest | `/opt/goby-test/audit-fixes-20260913-20260913T141732Z-a393812c3356/source-manifest.json` | 909,696 | `bce4d22a4c51dacca4660a6c8e8e3fac816141cd612a7b32b87367799e495cff` |
| B source manifest | `/opt/goby-test/m6-embedded-20260914/source-manifest.json` | 135,201 | `4e14cdcedd57a2e0d318e23d37570e85d24c03a8e4da4c0c49eb4827e0994e07` |
| E11 source manifest | `/opt/goby-test/m6-systemd-package-20260915/private/source-manifest.json` | 158,020 | `acdda037db9fedbaa388a09b512050e898727b19506b03595d0e53a65705bea8` |
| B source archive | `/opt/goby-test/m6-embedded-20260914/artifacts/source.tar.gz` | 2,710,656 | `10d88202cc2a06071b3a216330f70e92512f292f51c46764a4230aa136639dff` |
| E11 source archive | `/opt/goby-test/m6-systemd-package-20260915/private/source.tar.gz` | 2,708,124 | `e9143f909ac2383aefe4aa53209d7806d31ab74388881d0dce124b0fcfaed510` |

## Comparison method and inventory boundaries

A is a 4,870-entry repository snapshot; E11 is a 921-entry package-build source
inventory. Both manifests are keyed by repository-relative path. B is a
911-entry array containing the same kind of relative paths. B was normalized
by its explicit `path` field with a uniqueness check; only the harmless leading
`./` archive prefix was removed. No basename matching, source-root substitution,
or filename-based hash inference was used.

Equality means identical byte length and SHA256. A/B do not record E11's `mode`
field, so permission equality is outside this comparison. Production Go below
means non-`_test.go` files under `cmd/`, `internal/`, and the explicit
`web/admin/embedded.go` wrapper. This is a file inventory, not the resolved
Go build dependency graph; it includes alternative build-tag/platform files.
Backend embedded resources are exactly the 28 migration SQL files and six
backup catalog JSON files named by the production embed directives.

For A, 434 actual retained source files were reread beneath its `source/`
directory: 334 production Go files, 34 backend embedded resources, two module
files and 64 administrator source/tooling files, totaling 6,877,740 bytes.
Every selected file matched A's manifest. A's complete 4,870-file archive was
not reread. For B and E11, every regular source-archive member was reread and
matched its respective manifest: 911 files / 13,057,837 uncompressed bytes and
921 files / 13,222,740 bytes. Neither archive was extracted onto the filesystem.
The existing build results supply the binary-to-source binding; this review
did not rebuild or inspect running binaries.

| Selected category | A to E11: identical | Changed | Only in A inventory | Only in E11 inventory |
| --- | ---: | ---: | ---: | ---: |
| Production Go | 323 | 11 | 0 | 4 |
| Backend embedded resources | 34 | 0 | 0 | 0 |
| `go.mod` and `go.sum` | 2 | 0 | 0 | 0 |
| Package inputs | 1 | 1 | 0 | 2 |
| Administrator source/tooling | 0 | 0 | 64 | 0 |
| Generated administrator assets | 0 | 0 | 0 | 57 |

The last two rows are different inventories, not 64 deleted source files or
57 newly implemented UI features. A contains TypeScript/source/tooling and no
`dist` entries; E11 contains the generated bundle and Go wrapper, not those
TypeScript sources. Their equality cannot be decided from these two manifests.
B additionally records one administrator README; it differs from A's README
and is absent from E11's inventory. That documentation difference says nothing
about the generated UI.

The useful frontend comparison is B to E11: all 57 generated administrator
assets, totaling 1,106,130 bytes, have identical paths, lengths and hashes. The
embed wrapper is also identical. This is bundle-byte continuity between B and
E11; it does not establish A's separately served bundle identity or the complete
historical JavaScript contribution graph.

## Actual production changes

Hash prefixes below are the first 12 hexadecimal characters of full SHA256
values retained in the pinned comparison receipt. All 11 changed files were
read from A's retained tree and E11's verified archive before the textual diff
was inspected.

| Path | A SHA256 prefix | E11 SHA256 prefix | Change found in the actual source |
| --- | --- | --- | --- |
| `cmd/goby/generation.go` | `bb877c1ad3ab` | `56d669ab188bd` | Selects dashboard assets before `server.New` and passes `WithDashboardAssets`; adds an asset-selection failure path. |
| `internal/server/dashboard.go` | `e49d90c5c143` | `a7f358089061` | Adds the filesystem option and serves the selected filesystem, retaining the configured external-directory fallback. |
| `internal/server/server.go` | `dd64cb1edb38` | `0539b366a191` | Adds `io/fs` and the `dashboardFiles` field. Its existing task initialization still invokes the now-changed task repository. |
| `internal/library/scan.go` | `d9e8390e1923` | `3e1fb045aafa` | Successful explicit `ForceProbe` refresh can repair derived entity associations, preserving the accepted projection/overrides. |
| `internal/library/themes_scan.go` | `ba0b7a6f2a33` | `ca8034fe536b` | Applies the same successful-refresh association repair to theme/extra resources. |
| `internal/library/task_scans.go` | `519a9222490f` | `64c72415dc2e` | Resolves the immutable parent task key into scan/refresh mode, persists `force_probe`, and checks that child/job/retained state agrees. |
| `internal/tasks/models.go` | `2cda860d06cc` | `eafc81e63739` | Shares the two fixed executor keys with the library package. |
| `internal/tasks/runs.go` | `e8b9a11c44eb` | `a0bd02810c16` | Binds admission fingerprints and cancellation/recovery ownership to the actual supported executor and force-probe mode. |
| `internal/tasks/scheduler.go` | `f87b3f7e84b1` | `fd95a28b0a7c` | Initializes and selects due rules for both fixed definitions with a shared lock order and bounded rule counts. |
| `internal/tasks/store.go` | `ece00ac05b80` | `63737203aaa7` | Reconciles both `library.scan` and `library.refresh_media`; checks the executor and preserves the narrower Emby administrator route policy. |
| `internal/tasks/triggers.go` | `090a6624ff81` | `1392c8594179` | Uses the shared supported-executor/actor check for trigger mutation. |

| Production file added after A | E11 SHA256 | Role |
| --- | --- | --- |
| `cmd/goby/dashboard_assets.go` | `371b77b812dbf3eee1512166dd6959ef0ece7e83594c15e1fbab77c37df47391` | Explicit `GOBY_WEB_DIR` override and default asset selection. |
| `cmd/goby/dashboard_assets_embedded.go` | `92ad8e29c3197fab07d2dc510626c17f28d00a0d18a3b8b9a819d2c53aecdec0` | `goby_embed_admin` build selects the bundled filesystem. |
| `cmd/goby/dashboard_assets_external.go` | `8de5834cc5b591a1cd76c34d9aeb797499b9c2bb2fdc0b29242912ae674df684` | Ordinary builds retain the external-directory filesystem. |
| `web/admin/embedded.go` | `a9e37c8bd6d0e1a12d4548a19abfb494ff46174eee7965eeb4c612cc95d14354` | Embeds the complete `dist` tree and requires `dist/index.html`. |

There is no production Go removal in this comparison. A to B accounts for
the three asset-selection/assembly changes and four additions. B to E11 accounts
for exactly the eight library/task changes; the other 330 production Go files
are identical between B and E11.

For packaging, A and E11 record identical `deploy/linux/goby.service` bytes.
The repository `.env.example` differs; `deploy/linux/INSTALL.md` and
`scripts/build-release.mjs` are E11-only inventory entries. B already contains
`scripts/build-release.mjs`, whose bytes change by E11, but B does not inventory
the three deployment files. These are package-source observations, not a read
of any installed environment or service configuration.

## Unchanged implementation and its practical limit

The full unchanged-path list is in the private receipt. The following families
are entirely byte-identical in their production Go inventory between A and E11:

| Family | Identical production Go files | Relevant boundary |
| --- | ---: | --- |
| `internal/identity` | 18 | Authentication/identity implementation unchanged. |
| `internal/playback` | 9 | Playback state implementation unchanged. |
| `internal/media` | 23 | Media/probe/seek-analysis implementation unchanged. |
| `internal/transcode` | 22 | Conversion implementation unchanged; no new GPU/runtime acceptance follows. |
| `internal/subtitle` | 4 | Subtitle implementation unchanged. |
| `internal/config` | 5 | Configuration-loader source unchanged; actual configuration is not compared. |
| `internal/database` plus embedded SQL | 9 Go files and 28 SQL files | Database/migration source unchanged; runtime data and startup effects remain separate. |

`cmd/goby/main.go` is unchanged. Within `internal/server`, 75 production Go
files are identical and only `dashboard.go`/`server.go` differ. The unchanged
files include `auth.go`, `client_sessions.go`, `admin_sessions.go`,
`admin_users.go`, `audio_http.go`, `audio_playback_info.go`, `audio_request.go`,
`audio_runtime.go`, `playback_info.go`, `playback_input.go`, `streams.go`,
`subtitles.go`, `subtitles_dto.go` and `userdata_notifier.go`.
Both module files are exact:

- `go.mod`: 502 bytes, SHA256
  `cd5137649ee15d846b70dc394b730efda87c325d31b7b947743dd8421c89e314`.
- `go.sum`: 3,446 bytes, SHA256
  `0e8b530d6f1ad5f697aee4bfdba09c2feeea84add0a1e70b13db47f97fa6cdb2`.

All six embedded PostgreSQL backup catalog JSON files are also exact. The
unchanged modules/resources do not establish identical runtime libraries,
configuration, database contents, mounts, media files or client process state.

Three source-grounded consequences matter for the final candidate:

1. **Startup is not an unchanged-state observation.** The existing
   `Server.initializeTasks` calls `Store.Reconcile`. E11's implementation can
   register the new refresh definition when it is absent. It does not itself
   create a run or schedule, but current task definitions and any existing
   triggers must be included in the candidate transition's expected data delta.
   Unchanged migration files do not make an A-to-E11 start a zero-write action.
2. **Shared catalog work has changed.** Existing scans, task-child recovery and
   scheduling now enforce executor/force-probe consistency; successful explicit
   refresh can repair metadata associations. The accepted
   [M5 focused/browser result](task-media-refresh-verification.json) and
   [final ordinary regression](m5-final-regression-verification.json) address
   their recorded changed-source scopes. They do not describe the retained
   client's current task/catalog state on E11.
3. **The deployment profile has changed even though core route bodies have
   not.** The embedded asset selection, build tag and selected runtime must be
   admitted together. B's native admission and E11's r04 runtime are separately
   useful evidence; neither is an E11 original-client playback closeout.

## Use in the next decision

Keep A's accepted MP3/FLAC journeys as historical evidence. This comparison
supports limiting new source review to the actual changed assembly/task/scan
paths instead of assuming that every audio implementation changed. Decide
explicitly which unaffected evidence can be reused after the selected artifact
and retained-state transition are reviewed. Do not mark E11's audio row accepted
solely from file equality, and do not replay consumed audio inputs to obtain a
new label.

Movie, episode and subtitle acceptance was already open on its historical
artifacts. Unchanged core code neither explains the old page errors nor supplies
physical293's missing cancellation evidence. Those issues retain the separate
bounded cause/contract decision in the
[support and delivery matrix](../planning/support-and-delivery-matrix.md).

The [core resolution](core-client-acceptance-resolution.md) has selected E11 as
the next delivery artifact. The candidate transition and current-state admission
remain unexecuted. No build, test suite, client run,
HTTP/SQL request, service operation, vendor-source read or live-configuration
read occurred in this comparison. The only new remote write was the exclusive
private comparison receipt; original manifests, archives and source trees were
left unchanged.
