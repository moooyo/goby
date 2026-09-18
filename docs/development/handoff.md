# Goby handoff — September 19, 2026

The source implementation for the [media, collections, and management
wave](feature-wave-20260919.md) is complete on
`codex/media-library-management`. Consolidated remote verification is still
running; the remaining checks, final build, resource closure, merge into main,
and push have not completed. Provider-specific acceptance and OCI work are
deferred by the user. Do not automatically resume older execution scopes or
pending rows. The original M2–M6 scope is still incomplete; M7 remains deferred.
License selection and external distribution remain undecided.

This is the single current handoff. It replaces the September 15 native-capacity
and September 17 wrap-up handoffs. The September 17
and September 18 resumption files are retained local historical
execution logs, not an instruction to repeat their old queues. Original results,
failed attempts and independent reviews remain preserved.

## Repository and closeout

- Workspace: `D:/Code/goby`; branch `codex/media-library-management`.
- HEAD: `a8e525bff77180d4db46fdeba489097697ec0ee2`.
- Remote: `git@github.com:moooyo/goby.git`.
- The historical accepted canonical full/build source is
  `1162808afafacdc9ab9a4d6c037263764b1afd7a`. Do not describe the current dirty
  feature checkout as that exact tested source without an explicit source comparison.
- The wave source and documentation are awaiting final integration. Select
  reviewed changes explicitly; unrelated OCI, packaging, client/helper and
  historical-draft changes remain outside this wave's commit selection.
- The pre-wave uncommitted product/tooling inventory included diagnostic
  plan/cgroup/process changes, user-configuration DTO/tests, a refresh browser
  test, release/notice packaging, the OCI recipe and client-execution helpers.
  Pre-existing untracked utility files were retained. Inspect `git status`
  before any integration; do not blanket-stage unrelated work.
- The pre-wave closeout made no commit, push, merge, PR or issue write.
- That historical closeout retained no live command/session handle. Its
  file-only publications, composition, binding and readbacks terminated.
  No new H1 live outer, PG-only baseline or playback request was started.
- Historical preparation agents stopped after saving their work. Incomplete
  cache-amendment/r07 drafts are explicitly non-executable.
- No local test, build, candidate execution or verification suite was run.
  A public index was downloaded on Windows as unverified transfer data only.

## Current wave verification checkpoint

- The 692-test server scope is covered by 676 original passes and 127 targeted
  rerun passes, including new, failed and previously unfinished cases. The
  reruns overlap the original scope; these counts must not be added together.
- Ten core packages recorded 5,136 passes and one existing opt-in helper skip.
  Identity recorded 181 passes, backuppg 497, and recoverydb 177.
- Seven real browser phases and five new mocked checks passed.
- The full recovery-manager package, four existing mocked checks, final build,
  owned-resource closure, merge and push remain pending. The wave is not yet
  accepted as a whole.

The [wave verification record](feature-wave-verification-20260919.md) owns the
source-bound results and final closeout. These checks do not upgrade old
Programs/W, H1, production-promotion or complete M2–M6 acceptance.

## Product functionality available

Implementation availability and acceptance scope are different. The following
capabilities exist, with per-increment evidence; this is not a claim of full
Emby compatibility or completion of every milestone.

| Area | Implemented functionality | Remaining boundary |
| --- | --- | --- |
| Service and authentication | Linux Go service, PostgreSQL migrations/persistence, first-admin setup, users, tokens, sessions, application keys and library policies | Full upstream policy/configuration and client-wire behavior are not complete |
| Media catalog | Movie/TV/music scanning, ffprobe, stable identities, local NFO metadata, artwork, search/filtering, hierarchy and library ACLs | Representative native scan/HTTP concurrency, broader storage faults and durability remain unaccepted |
| Basic client playback | Login/browse, PlaybackInfo, original-file HTTP/ranges, external SRT/WebVTT, progress/resume, watched/favorite state, sessions and initial events/control | Complete current-version real-client journeys and broader event/NextUp/refresh behavior remain open |
| Software media pipeline | Remux/progressive audio/video, embedded/external text and ASS subtitles, bounded fonts and HLS burn-in, TS/fMP4/packed-audio and adaptive HLS, generic dynamic-source conversion, software HDR/deinterlacing, and proof-gated nonzero copy seeks | Source implementation is complete for the selected contract; consolidated closeout, arbitrary format/timing combinations, actual hardware and full client profiles remain open |
| Playlists and collections | Persistent Playlist/BoxSet containers, membership, ordered duplicate playlist entries, sharing/ownership, catalog queries and user-state behavior | Current member authorization and the documented playlist/BoxSet distinctions apply; full upstream parity is not claimed |
| Administrator dashboard | Users and supported expanded policies, metadata/locks, sessions, keys, devices, tasks/schedules, typed management settings, activity/logs, media diagnostics, and integrated provider controls | Selected source is complete; provider-specific acceptance is user-deferred, the final wave gate is pending, and unsupported upstream fields remain outside the contract |
| Native backup/recovery | Encrypted backup, import/restore planning, activation/rollback, administrator UI and offline CLI | Acceptance belongs to recorded native scopes; OCI upgrade/restore and final delivery still need their own evidence |
| Packaging | Embedded assets, native systemd package work, OCI recipe/build/import and internal notices assembly | Final architecture/hardware/deployment matrix, licensing and external release are not complete |

See [implemented API surface](../api/implemented.md),
[scope](../api/implementation-scope.md), and the
[support matrix](../planning/support-and-delivery-matrix.md).

## Functionality still missing or intentionally limited

1. Advanced text extraction, font delivery, HLS subtitles and burn-in are now
   implemented within the [advanced-media contract](advanced-media.md).
   Standalone upstream subtitle-playlist aliases and rolling live subtitle
   windows are not registered; embedded subtitle deletion and bitmap OCR are
   not supported. Provider code is integrated, with its acceptance deferred.
2. Adaptive/fMP4/packed-audio HLS, generic dynamic-source conversion and
   software HDR/deinterlacing are implemented. Dynamic conversion is
   nonseekable TS/fMP4 without selected subtitles; copy seek requires exact
   proof-backed H.264 boundaries and absent or encoded AAC audio. Arbitrary
   timing/codec combinations and actual GPU profiles remain unaccepted.
3. Playlist/BoxSet membership, mutation, ordering and sharing are implemented;
   their bounded access, entry identity and user-state rules do not establish
   every upstream collection behavior.
4. Supported subfolder/parental policies, user mutations, management settings
   and task executors are implemented. Complete upstream policy/configuration
   coverage is not claimed; online provider-specific acceptance is deferred.
5. Broader Emby preference, event, query, alias and client-behavior coverage,
   plus the current wave's remaining verification and integration gates.

M7/P2 decisions remain separate: Live TV/EPG/DVR/tuners, DLNA, offline sync,
external channels/provider extensions beyond the integrated adapters, group
playback and optional extensions are deferred.
An Emby consumer web application, Emby Connect/cloud identity and proprietary
binary-plugin compatibility are outside the current scope. Goby's website is
an administrator dashboard, not a missing consumer-player implementation.

## Accepted verification that must be reused

The following results belong to earlier artifacts and scopes. They are not the
current wave's final acceptance or an instruction to replay historical work.

- Canonical `1162808` full/build: 25 packages, 2442 top-level passes, 28 required
  regressions, two builds, 59 assets and 5582 source files. The explicit mount
  profile gap and two inert helpers remain unexercised.
- Native r09 on the actual 116/e6 artifact: software diagnostic completion,
  cancel and owner deletion are scoped accepted, with resource closure. This
  does not establish which exact process exit was caused by each intervention.
- Administrator browser phases and native backup/recovery have their recorded
  scoped acceptances; they are not all claims about one identical current binary.
- M2: SQL/tiny-file baselines, bridge components and the finite permission-loss
  recovery profile are accepted. Native concurrent scan/HTTP capacity, other
  faults and host durability remain open.
- M6: the actual 116 internal notices package is scoped accepted: 59 files,
  64 directories and 54 notices; reusable packaging checks passed 32/32.
  This is not licensing clearance or external-distribution acceptance.
- OCI: r08 image build, r02 two-image import, r04 firstboot/normal stop and
  r09 catalog/same-image-restart business are accepted in their finite scopes.
  The original r09 outer remains failed; its separate business and physical
  reviews do not rewrite the missing original EOF/acceptance flags.

## Exact current H1 stop point

This is the retained, deferred H1 checkpoint. The current feature-wave checks
do not resume its live continuation or change any original failure flag.

The original H1 r02 failed at HEAD request 13 before any media GET. It had
already created the test media/library/viewer/policy/scan and a Prepared play.
Both retained 4-GiB volumes and owned processes closed. Do not recreate or
rescan that prefix, reuse revoked credentials, or rewrite the original failure.

The HEAD reader correction passed five real socketpair methods, original
`14d3cc`, with independent review. The original product HEAD remains incomplete.

The safe initialization projection `bd3971` is independently accepted only for
before-to-initialized facts: 35 tables, 365 columns, five sequences; 146 prior
row/xmin digests unchanged; 22 additions in twelve tables; no changed/removed
rows. Eight SQL wrappers support two full snapshot joins and three scoped
observations. All 253 explicit read/output descriptors closed. The projection
is 3760557 bytes / `6abc7a9fd17a5655b0b37d6267a64ec2d8935c252dac10592ffa53d0b7b25959`.
Its [root review](D:/Code/goby/.git/oci-playback-failure-projection-preparation-20260919-r01/actual-projection-01/root-initialization-review.md)
and two adjacent independent reviews preserve missing windows, credential
receipts and after-cleanup snapshots as unknown.

The new continuation preparation is:

`D:/Code/goby/.git/oci-software-playback-continuation-preparation-20260919-r01`

| Completed file-only operation | Actual result |
| --- | --- |
| Source publication `2d8482` | Manifest 9234 / `ce334a4445f2d72cc30d3b362124520930a81f346c888ac366fa5bafb565add5`; publication 4393 / `b6032d1c873894f4ca9ab075d8293f20831901d0ca32470275f10256f9d874c0` |
| Composition `1c1f09` | Request 7105 / `650af9df4645cb862182b1f92f90cd0d5ffd086d6fea589925d1d614611e8eb8` |
| Binding `528777` | Bound input 19220 / `38811aadcd068fd4e5f0fa66a9c104d31e57a21236fd407c134b759bd0cc2847`; derivation 4363 / `027884793d1dcdaa92d2c320b0f1d638cd1a00dc4edd5f33251071805e325c12` |
| Readbacks `dc9bcd`, `2f204a`, `8e0ead` | Eight exact files; all eight explicit file descriptors closed |
| Derived runtime/worker | 68923 / `ae9586b22defc1323c90f223ca02ff564859c8e0e947c5c655f8117b982b7514`; 14304 / `642443d3c76a55db9b799eaca3ed7fe88247abf7f50e7a5a6e8cd9de437c3df0` |

The [independent binding review](D:/Code/goby/.git/oci-software-playback-continuation-preparation-20260919-r01/actual-bound-read-01/independent-binding-review.md)
is 7312 bytes / `486b524110220a77d554480c4b7a6eac216d629b7996e9d011800c4a7eccc075`.
It verifies seven exact runtime literal replacements and one worker replacement;
installation root, PG alias, roles, execution owner and original private-input
Pin remain unchanged. Compose/bind each retain 724 bytes of three known public
SyntaxWarnings; their stderr is not empty.

The additional continuation checks have a split outcome:

- Check publication r01 `210a62` failed before tests because PowerShell changed
  UTC strings to an equivalent local-offset representation. Its original
  nonce `27c6d9a4f183` is consumed.
- The r02 publisher preserves the original strings; `53015e` succeeded for
  nonce `58b9e6c201df`. Checker/runner/subjects were unchanged.
- Original check `8908b2` retains tool/SSH exit 1 and supervisor status
  `failed_closed_or_inspection_required`. The child exited 0 and all ten
  methods/eighteen scenarios passed. The supervisor rejected exactly one
  248-byte public-source SyntaxWarning under its empty-stderr rule.
- The [independent scoped review](D:/Code/goby/.git/oci-software-playback-continuation-preparation-20260919-r01/targeted-check-preparation-r02/actual-check-58b9e6c201df/independent-scoped-check-review.md),
  11306 / `a246595ac92fdffbf1ab394f39b71d11ca1a555de6e49a79fddeb99137d2eca1`,
  accepts the actual ten/eighteen branches, privacy boundary and 36/36 FD/
  process/stream closure, separately from the original supervisor failure.
  Do not rerun passed scenarios just to manufacture a green original result.

No live outer has run. After an explicit user decision to continue, the
[prepared entry](D:/Code/goby/.git/oci-software-playback-continuation-preparation-20260919-r01/ENTRY.md)
must first check the current eight-file media layout and complete PG-only
post-cleanup state, then start Goby and check its exact startup delta before
fresh authentication and media delivery. Those future snapshots are still null.
The live admission may reject changed state; file binding is not live acceptance.

## Reference startup and trace-tool stop point

The reference instance's original r06 startup failed before its expected
application executable was observed; the child returned -25. SIGXFSZ is the
supported signal interpretation on that host, but the file/syscall cause is
unproved. Its processes/storage closed and its backing remains retained.
Do not increase the original 128-MiB application file-size limit without cause.

Trace-tool acquisition history must remain separate:

1. R04 signed Release and the 49-node host graph are accepted. Its index
   download timed out with a retained unauthenticated prefix.
2. R05 copied that prefix and completed two ranges. Range 03 timed out and
   closed; later ranges and assembly did not run.
3. Windows transport `870e00` received the complete public 9678380-byte index
   at `.git/artistless-index-transfer-preparation-20260919-r01/received-public-index.xz`.
   It was not parsed or authenticated locally.
4. R06 publication `4b0eed` succeeded, nonce `71f3b8d4a206`. Upload `8ea907`
   failed before stage creation; its original remote envelope has no result
   receipt or exception code. Local completed writes are not proof of remote
   received bytes. No index verification, package or extraction followed.
5. Current read `6d3dd1` matched 27 of 28 fixed files and both owner directories.
   `/etc/ld.so.cache` changed from 35207 / `b0089c2f2437d4f969e24e41bb76371ef58b9c756dd5775d3e6fa9ee774b2cb7`
   to 42483 / `da3e9ab42a4f25f5dba2804053e9311145466a4a3d4d29e5d5f2947834f483b0`.
   This currently fails the fixed-input gate before stage creation; it does not
   recover the unrecorded original exception or identify who changed the cache.

The [R06 failure/current-observation review](D:/Code/goby/.git/artistless-startup-trace-tool-preparation-20260918-r06/actual-upload-admission-read-01/independent-failure-observation-review.md)
is 9799 / `39a2ea9bdf4687b71eab32b37ae347fb93208c11d4b11ff1f0174a30383bef0e`.
No strace candidate has been accepted or executed by this acquisition.

These saved drafts are **not executable**:

| Directory under `D:/Code/goby/.git` | Stop condition |
| --- | --- |
| `artistless-loader-cache-amendment-preparation-20260919-r01` | Partial producer/PS sources only; missing core cache adapter, antecedent, manifest, entry and seal. Read `DRAFT-STATUS.md`. No actual amendment exists |
| `artistless-startup-trace-tool-preparation-20260918-r07` | Partial version-2 consumer hooks; implementation and PS interface unfinished. Inherited r06 manifest/ENTRY/seal do not match r07. Read `DRAFT-STATUS.md`; all actual amendment slots are null |

The contemplated amendment permits one pinned static `ldconfig -p`, current
cache/file/alias checks, and reuse of original ELF observations only if all
49 nodes, 42 resolution names, 125 edges and 96 version groups still match.
Changed/ambiguous dependencies must remain Pending. This plan has not executed.

Separate source-only trace preparations are saved at
`.git/m3-m4-a1-reference-startup-trace-preparation-20260919-r02`
(manifest 4116 / `084c165751f0d26f0278c6439d2494923c7f8e2736ceb683900292611e7f158d`)
and `.git/m3-m4-a1-reference-startup-trace-checks-preparation-20260919-r01`
(3077 / `003a87477c099cb987f15da911d29e51481d1877f31c9bdafc2347bf456a5e64`).
They preserve the original r01 source and correct a cleanup poll exception.
Eighteen current pure checks, one historical counterexample and three actual
tool cases are prepared but unexecuted; tool Pins remain unresolved. The final
whole package still requires root review. Do not mistake source preparation
for a completed startup trace or client acceptance.

## Capacity and retained resources

- Full capacity observation `8ef605`, `2026-09-18T16:21:59.553728Z`:
  MemAvailable 5510918144 bytes, root-free 9398964224 bytes, free inodes 5465229.
  The unchanged M2 floors are 6442450944 bytes available memory, 4294967296
  root-free bytes and 30000 inodes. The memory floor failed; this was not a reservation.
- Separate memory-only observation `5a166a`, `2026-09-18T16:47:15.008841Z`:
  total 12526919680, available 5490810880, swap zero. Do not combine these two
  timestamps into a fictitious single capacity sample.
- First M2 E75e capture/controller remain uninvoked. Do not lower the floor.
- The OCI backup/restore preparation's separate 35-GiB root-free requirement
  has not been met by the recorded root-space samples.
- Preserve intentional services A/B, the expected PostgreSQL instances and
  hosting/reference service. A's last recorded identity is PID 1907978/start
  34901535, invocation `2d8403319f3943dbb3d1da638f33093e`, 3d6/schema28; this
  closeout did not make a fresh live-service observation. Old-A-dependent work
  must finish before a new Programs transition is selected.
- Do not touch `/dev/shm/goby-m2-fullscan-20260914`. The paused mount profile
  and retained helpers require their own explicitly planned closure.
- Retained OCI, native and reference backings are evidence, not disposable
  temporary files. Fourteen earlier raw releases are already complete and
  cannot be reclaimed again. No new backing deletion was performed here.

## Work remaining before complete delivery

These are full-delivery obligations, not the immediate feature-wave queue.
The current wave's remaining checks and integration steps are listed above.

- Current Programs update/admission, then movie/resume, episode/browse,
  SRT/VTT/Off, audio bridge and G3 promotion after their prerequisites.
- Reference startup/W client acceptance and broader real-client/media profiles.
- H1 actual playback, OCI upgrade/backup-restore/public HTTPS and remaining
  packaging/deployment profiles.
- Native scan/HTTP overlap and throughput, broader storage faults and host
  durability, actual GPU and native arm64 acceptance.
- The bounded limitations listed above, final source integration, support profile,
  license decision and external-release selection.

No completion percentage is assigned: these obligations have different scopes,
and test counts cannot be converted into a reliable feature-completion ratio.

## Decisions for the user

The alternatives below record the earlier decision point. The user already
selected the feature wave; this table does not request another decision or
restart its deferred OCI/H1 alternatives.

| Direction | Next concrete outcome | Tradeoff |
| --- | --- | --- |
| Finish current acceptance first | Continue from the reviewed H1 binding, complete reference diagnosis and the chosen client/deployment gates | Produces a better-supported existing feature set; capacity/hardware prerequisites remain |
| Prioritize user-facing functionality | Select a bounded feature such as playlist membership or advanced subtitles, implement it and verify its exact scope | Expands capability while the existing delivery gaps remain open |
| Consolidate the release first | Review/integrate the dirty source, decide the first supported profiles and license/distribution boundary | Clarifies what can ship; it does not retroactively pass missing runtime evidence |

The user selected functionality first, with code development preceding combined
acceptance; see the active wave above. An internal release with limited supported
profiles must not be renamed completion of the full M2–M6 goal.

## Rules for the next session

Use Chinese for conversation and English for code/comments/documentation.
Use Windows PowerShell locally. All tests, builds and runtime/browser probes
remain serialized through `ssh test-env`; local verification is not authorized.
Read the actual original terminal and preserve every yielded chunk before
advancing. A consumed failed scope is not an automatic retry opportunity.

Keep private credentials, request bodies, raw database snapshots, environment
contents, keys and backing bodies out of local/public output. Use the scoped
remote safe projections. Preserve original false/null fields and keep component,
business, physical-closure and complete-delivery acceptance separate.

The mutable machine-readable checkpoint is
`D:/Code/goby/.git/resumption-runtime-checkpoint-20260917.json`. Original evidence
and preparation directories are under `.git`; they are not all committed or
portable with a plain source checkout. This handoff governs current disposition;
old queue statements in historical logs do not authorize new execution.
