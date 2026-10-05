# Merged-main Docker image refresh — October 6, 2026

## Completed scope

Release `2026-10-06-main-images` refreshes the software and AMD backend images
to merged `main` source `2ba10e3613cac9da0a2e2c8ae7317bba229fdd56` at database
schema **61**. It packages the scan and task authorization integration recorded
in [the main integration report](player-main-integration-20261006.md).
The standalone player image and archive are reused unchanged from the
[October 5 release](player-release-20261005.md), retaining their own source
revision. This is an image refresh, not a new player build or schema migration.

The eleven-phase software container verification, nine-phase AMD runtime
verification, published-clip color readback and owned-resource closure passed.
Release `2026-10-06-main-images` is complete within this selected scope.
The build identities below describe construction; the separate runtime results
in this record supply acceptance evidence. Immutable build receipts retain
their original construction-only status.
The October 5 schema-upgrade, backup-based rollback, feature and GPU results
remain evidence for their original images; they are not relabeled as new-image
results. Original dated reports and immutable build receipts are preserved.
The 270 source regressions recorded for main integration belong to that earlier
verification run at the selected source; they were not rerun or added to this
image refresh's phase counts.

## Frozen identities

Both backend profiles contain the same new executable, including its embedded
administrator assets:

- Backend source: `2ba10e3613cac9da0a2e2c8ae7317bba229fdd56`.
- Backend executable SHA-256: `268624c978509ef6297abc75100f9afa578e3d1ec60348bdd328a812775cd337`.
- Database schema: `61`.
- Player source: `7aaaeed44526848737270089ab0227d2d410f864`.

| Component | Immutable image ID | Disposition |
| --- | --- | --- |
| Software backend | `sha256:318a7d1294d6cdc036c0f2ee41e5165e6a643048144885980299a8c9c76646d0` | New application layer from merged main |
| AMD backend | `sha256:f09d0b42a418b0d839faac3998a5e393abaeb30d6b056e6f5b67fa6c110fff28` | New application layer from merged main |
| Standalone player | `sha256:7140ce5c531a302b0aca595ee47ce98c52cd08ed73d5a00e5a27972d1ad94192` | Reused unchanged |

The delivery root is
`C:/Users/moooyo/.codex/worktrees/165c/goby/.artifacts/main-image-refresh-20261006/delivery`.
It contains separate `software`, `amd` and `player` archive directories, plus
`goby-docker-operations.zip`. The operations toolkit contains configuration,
the helper, catalog and operator guides; it does not duplicate the images.

| Archive | Bytes | SHA-256 |
| --- | ---: | --- |
| `software/goby-linux-amd64-image.tar` | 976,653,312 | `eeeaf9aa3294ac5d604c14c98b005a5ac4d1f379bc355075f99ba6d495367ce5` |
| `amd/goby-linux-amd64-amd-image.tar` | 1,402,021,888 | `368fa102ee7b3e29de194c03989866aa7c6bd7f775ccaf105e2b6843a885b24b` |
| `player/goby-player-linux-amd64-image.tar` | 68,103,168 | `1952fa675beaf9a44e34dc410a2a3df7f43c7d87a83da05434faffaa543dfae3` |

The frozen source archive is 221,276,160 bytes, SHA-256
`add9242e8eb7cc3d8794f315910ef4f29c2bd9d3cfe166941820a0e0b20e498d`.
The application-layer builder verifies the selected `git archive` and source
receipt, pins the accepted media-image base, and reads the executable and
media-tool configuration back from each resulting image. Existing primary
FFmpeg/ffprobe, AMD libraries/drivers and the separate native analysis toolchain
retain their pins. The reused player retains its original build receipt,
digest-pinned Node/nginx inputs, served-asset inventory and license readback.
Its complete original delivery directory is copied unchanged; the original
`build-receipt.json` SHA-256 is
`ff16e86486fedc97256a2ec9b154fa471bb2078d934279605d0d281a774682c1`.
A separate reuse receipt records that provenance without rewriting the build
receipt or claiming a new player build from the backend revision.
The [release catalog](../../deploy/oci/current-release.json) supplies the
installation image IDs, archives and companion hashes.

## Targeted acceptance

The selected verification covers the newly packaged backend and its integration
boundaries rather than repeating the October 5 complete upgrade/rollback run:

- Software container startup at schema 61 with the immutable new executable,
  merged scan behavior and task authorization checks.
- Persistent source-side media generation and authenticated delivery through
  the unchanged standalone player.
- AMD container task submission, actual GPU generation and output delivery
  using the existing accepted device/toolchain profile.
- Restart/reuse and owned-resource closure, with input and published-sidecar
  identity preserved.

The software journey passed eleven phases on `test-env`: one baseline phase,
six refreshed-image verification phases and four restart phases. It preserved
schema 61, existing media/account identities, favorites, playback progress,
fenced state and the previously published background MP4. A new 35-second
fixture with audio and real external SUP plus multilingual IDX/SUB files
produced three scanned subtitle tracks. The new backend published a 25-second,
600-frame background, a four-level waveform with more than 95 percent active
coverage, and three subtitle timelines matching an independent oracle.
Restart preserved the sidecar hashes, modification times and inodes; the
unchanged player retained its container and image identities.

The initial software baseline incorrectly required the optional
`application-key-master.key` file, which the existing installation had never
created. The corrected check binds both persistent control files and the
actual optional-key presence before and after the refresh. No production code
or state was changed to satisfy that check. The original attempt remains in
the evidence. Both software verification containers subsequently stopped with
exit code 0 and `OOMKilled=false`. The private Chinese software report is
retained at `.artifacts/main-image-refresh-20261006/software/REVIEW.md`.

The AMD journey passed nine phases on the previously authorized AMD worker.
P5, P8.4 and P8.2 each used the strict GPU path with observed device handles
and published a 25-second, 600-frame silent H.264/BT.709 clip. The independent
player naturally played each published clip with 600 total frames, 599
displayed frames and one dropped frame. Existing deployment identity, the
three-item catalog, user state and source descriptors were preserved; the
media-process count returned to zero. This flow adds new-image evidence for
the selected task, GPU and player path; it does not expand the accepted
profile/source or hardware boundaries.

The raw AMD run is `live-backgrounds-1791221494737-93667878`. The private
Chinese report is retained at
`.artifacts/main-image-refresh-20261006/amd/REVIEW.md`.

Independent readback of the three actual exported clips passed the selected
color checks. Results are retained at
`.artifacts/main-image-refresh-20261006/delivered-colors/results.json`:

| Check | Result |
| --- | --- |
| P5/P8.4 corresponding-scene RGB mean absolute error | 4.744138 |
| P5/P8.4 corresponding-scene luma correlation | 0.996525 |
| P8.2 independent scalar oracle | Mean channel error 0.336263; maximum 1.953125, within mean 3 / maximum 8 limits |

The P5/P8.4 comparison checks corresponding scene content across profiles;
it is not an independent absolute-color oracle. P8.2 retains its original
analytic SDR-plus-RPU fixture and independent scalar oracle. These results
describe the clips generated by the new container image, without claiming
additional natural P8.2-source coverage or arbitrary-source color accuracy.

The user's local preview and unrelated retained service data were outside this
verification scope.

## Resource closure and retention

The twelve retained AMD assets preserved their SHA-256 hashes,
modification times and inodes. The three new source/manifest/MP4 sets remain
retained as evidence. The reused player kept the same container ID through the
refresh and runtime checks until the final owned shutdown.

- Both AMD application containers stopped with exit code 0,
  `OOMKilled=false` and PID 0 before removal. Their owned network was removed.
- The private Docker engine, SSH daemon and tunnel stopped; CT104 IP forwarding
  returned to its original value of 0. No media worker remained.
- The owned PostgreSQL container stopped with exit code 0 and
  `OOMKilled=false`. Both software verification containers had already stopped
  successfully, as recorded above.
- Task-owned Go compiler scratch and the 1.2 MB Node compile cache were reclaimed
  after confirming that their workers had exited. The ordinary shared Go build
  and module caches were retained; source, data and raw evidence are separate
  from reclaimed compiler output.

This refresh installed no new APT packages and made no worker disk expansion.
The previously recorded 20 GiB CT104 disk and iptables dependencies remain from
the October 5 infrastructure preparation. Retained fixtures, private backups,
source and results were not removed as a side effect of resource closure.

## Installation and boundaries

Use the [quick start](../../deploy/oci/QUICKSTART.md), matching
[software guide](../../deploy/oci/README.md) or
[AMD guide](../../deploy/oci/README.amd.md), and the current catalog. Verify the
archive and companion hashes before loading or preparing the deployment.
`prepare --with-player` selects the unchanged independent nginx player;
`--writable-media` grants the media mount writes needed for source-side
generation, subject to host permissions. Generation remains opt-in.

The October 5 release and this refresh both use schema 61. Preserve the existing
state mount and its deployment identity, database, media and generated sidecars.
Take matching database/state backups before replacing the immutable backend
image. The [original upgrade guidance](player-release-20261005.md#deployment-and-persistence)
still applies when coming from an older schema, including backup-based recovery
and the prohibition on relocating fenced state by a plain directory copy.

No registry publication, public deployment, P7 FEL reconstruction, external
SUP/IDX+SUB playback or burn-in, additional GPU/driver platform, or new
recognition-quality claim is included. The project-license and public
distribution decisions are unchanged.
