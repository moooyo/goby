# Internal amd64 embedded systemd package

Status: remote package builds, focused checks, independent review and build
resource closure passed. The first installation prepared successfully but its
runtime controller failed before Goby started. Owned services stopped, and the
failed installation now has independently reviewed evidence preservation,
archive readback and resource closure. The first preservation-checker rejection
is also retained. Installation acceptance remains
open. See the [first attempt](systemd-installation-first-attempt.json). Reviewed
on 2026-09-15. The M5 increment completed focused/browser
acceptance, its 25-package ordinary regression (2,295 passes, zero failures and
one declared opt-in skip), independent reviews and resource closure. Its source
checkpoint is committed and pushed as `5faf854`. The subsequent build-script,
environment-template, installation-document and scoped evidence checkpoint is
committed and pushed as `beaea34`; the shipped unit remains unchanged. Its staged
tree matched all 863 backend/module/package inputs in the E11 source manifest;
the 58 generated asset/provenance files retain their separate build bindings.
The [source checkpoint](systemd-package-source-checkpoint.json) records that
separately retained bridge. The
[package build receipt](systemd-package-build-verification.json) records three
actual builds, seven focused top-level tests (26 including subtests), and 26
package guards, with no failures or skips. All artifacts and private evidence
were preserved and reread, and the owned build tmpfs was ordinarily unmounted.
During the E11 build phase, no application was started or installed. The completed build inputs and earlier
candidate/catalog inputs must not be replayed.

## Applied implementation

1. `scripts/build-release.mjs` now has an explicit optional `--package systemd`
   mode, initially restricted to `--arch amd64`. Existing invocations, `goby`,
   and the version-1 build `manifest.json` structure remain. It continues to
   build with `goby_embed_admin`, without dependency installation or runtime
   execution. No installer framework was added.
2. `deploy/linux/.env.example` replaces the unconditional
   `GOBY_WEB_DIR=/usr/share/goby/admin` assignment with a commented explanation
   of the optional external override. `cmd/goby/dashboard_assets.go:10` makes
   any nonempty value select disk assets, including in an embedded binary.
   That production behavior is unchanged; ordinary builds and existing
   external-asset installations retain their explicit setting.
3. `deploy/linux/INSTALL.md` now documents explicit manual installation paths,
   PostgreSQL 17 and FFmpeg/FFprobe prerequisites, service-account reuse,
   root-only environment creation, first-admin setup, normal stop/start,
   data/key retention, and removal instructions. The build runs no install
   script. `deploy/linux/goby.service` is unchanged.

Produce `goby-linux-amd64-systemd.tar.gz` with one fixed top directory containing
`goby`, `manifest.json`, `goby.service`, `goby.env.example`, and `INSTALL.md`.
Use fixed regular-file names, mode 0755 for the executable and 0644 for the
other payloads; reject links, duplicate names, path traversal and special
files. A package manifest outside the archive binds its complete member list,
bytes/modes/SHA256, archive bytes/SHA256, build manifest and the exact deployment
source inputs. This avoids a self-referential archive digest. Use pinned
existing tar/gzip tools with normalized ownership, ordering and timestamps;
do not add an npm archive dependency. Read every archived member back.

The new package record is separate from the existing build manifest. Bind the
new embedded build to the completed M5 production Go/embed/module source and
the actual administrator bundle; the ordinary full-suite binary is a distinct
artifact. Deployment/build-script changes need focused package guards, not
invented reuse of tagged full-suite evidence. If production source changes,
reassess the affected final-source regression before installation.

This is an internal candidate artifact only. Project licensing and distribution
notices remain open; neither this archive nor INSTALL grants external
distribution permission. Do not bundle PostgreSQL, FFmpeg or an external admin
directory in this increment.

## Completed remote build acceptance

This phase completed once in the scope below and is consumed. Its retained
package is `artifacts/linux-amd64-systemd/goby-linux-amd64-systemd.tar.gz`,
14,072,899 bytes, SHA256
`2dc2090441a255ec739f924f6f3453442edc4bc7f9ab9a8e6c4e715b59865a1a`.
The package and legacy amd64 binaries are byte-identical. The arm64 result is
a cross-build only. The following requirements describe the completed phase.

1. Freeze the current build/deployment files with the production Go/module
   sources and actual embedded assets bound to the accepted M5 source. Preserve
   the existing frontend bundle provenance; this packaging increment does not
   claim a new TypeScript build. Record the exact Go, Node, GNU tar and gzip
   executable identities and versions on `test-env` before building.
2. Use one fresh private scope at
   `/opt/goby-test/m6-systemd-package-20260915`, with an independent 3 GiB tmpfs
   at `build-ram`. Apply the current resource and protected-runtime checks below.
   The source is `build-ram/source`; the first package output is
   `build-ram/artifacts/linux-amd64-systemd`. Preserve outputs on persistent
   storage only after review. Do not borrow the paused M2 mount or its budget.
3. Run the actual amd64 `--package systemd` build, then the existing amd64 and
   arm64 no-package modes to check compatibility. Arm64 remains a cross-build.
   Check actual archive members and hashes, fixed CLI/input rejection cases,
   and malformed archive rejection using the production readback function.
   Synthetic Go fixtures prove guards only and are excluded from real builds.
   Retain focused embedded-asset checks separately from the ordinary full suite.
4. Give every child and the complete build phase finite recorded deadlines,
   capture failure evidence and close owned process groups. Independently
   review source/tool bindings, archives, outputs and guard results; preserve
   evidence and close the build tmpfs before admitting the installation phase.

This scope starts no Goby, PostgreSQL, FFmpeg or browser process. It does not
rerun the completed M5 suite merely because packaging or documentation changed.
Any later production change requires reassessing the affected verification.

## Fresh installation acceptance

This first scope is consumed and closed. The following profile records its
intended acceptance and retained boundaries, not another execution instruction.
The active queue keeps new installation attempts paused.

The selected execution scope is
`/opt/goby-test/m6-systemd-install-20260915`, with private evidence under its
root-owned 0700 directory. The separate synthetic fixture root is
`/opt/goby-m6-install-fixture-20260915`. PostgreSQL will use a new 768 MiB tmpfs,
database `goby_m6_systemd`, ordinary role `goby_m6_app` and private loopback
port 25499. The application will use port 18099 in the same new network namespace.
The tester stays in the host namespace for existing-runtime protection checks
and enters the pinned private namespace only for each owned HTTP or SQL call.
Shared service accounts remain unchanged. No credential or business input from
an earlier fixture is reused.

The bounded read-only precheck found all selected standard paths, service names,
aliases and enablement links available, with the expected account identities.
It created no fixture, mount, database or service. Preparation must check those
facts again immediately before its mutations. The 16 selected tool binaries
have also been pinned and their versions observed. These preliminary results
do not establish installation or runtime acceptance.

The recovery-plan review corrected a combined timing issue: the former
1,800-second anchor lifetime could expire before all phase budgets completed.
The owned anchor now has `RuntimeMaxSec=3600`; PostgreSQL remains bound to it.
Preparation handoff, runtime entry and seal entry require at least 2,400, 2,340
and 1,260 seconds respectively, derived from the original anchor start and its
actual systemd limit. These thresholds cover unchanged phase budgets, 60-second
handoffs and a final 60-second stop margin. Changed boot/process/invocation,
restart or insufficient time rejects further business and preserves the owned
cleanup path. The clock is never reset.

Independent static review, 28 remote synthetic boundary checks and remote
syntax checks of all seven helpers passed. The boundary receipt is
`private/lifetime-boundary-checks.json`, SHA256
`396bf902507d9adb026472965a138de602d38e4c9a1025e6bfaf4978e531b065`;
the preserved revision/syntax receipt is `private/lifetime-revision.json`, SHA256
`0ec8bc2da29e65be22c74b6c720abd7108222adeb3699613e37ef0175644ef34`.
These checks ran zero actor mains, service commands, SQL or HTTP calls. No
installation fixture, PostgreSQL or service has started at this checkpoint.

Freeze all three operators and the preparation input before the first service
start, then run
preparation, runtime and closure consecutively. Bind later phase inputs to the
actual preceding receipts and the frozen operators. The preparation ceiling is
420 seconds plus a fixed 240-second cleanup reserve; runtime has 900 plus 120
seconds; closure has 900 plus 300 seconds. These are phase limits, not predicted durations or
permission to replay a failed phase. Mandatory owned-service stopping retains
its reserved time and does not depend on successful evidence-file writes.

After successful installation and resource closure, publish the scoped receipt,
bind the final staged source to the retained package, and commit/push a partial
M6 checkpoint. Do not extend this increment into another general runner or
repeat accepted builds and ordinary regression without a relevant source change
or concrete failure.

- Admit one new private evidence scope after M5 closure. Recheck disk, memory,
  loopback ports, and exact absence of `/usr/local/bin/goby`, `/etc/goby`,
  `/var/lib/goby`, `/var/cache/goby`, `/var/log/goby`, `/run/goby`, and the
  `goby.service` unit/load state, aliases, drop-ins and enablement links.
  The earlier absence observation is not execution authority. Reuse the
  existing `goby` UID 995 / GID 986 unchanged, including account membership;
  never delete or modify that shared account.
- Use a fresh private PostgreSQL 17 cluster, ordinary role and database, a
  private network namespace, new secrets and the existing seven-file synthetic
  media recipe. The database has no inherited users or catalog. Do not use the
  three protected PostgreSQL instances. Reuse the E9 prepare module's pinned
  `protected(base, runtimeGuardValue)` and deployment-lock primitive for the
  current seven-unit / three-PG / two-app baseline, not an old runtime epoch.
- Install the archive's exact binary at `/usr/local/bin/goby`, unit at
  `/etc/systemd/system/goby.service`, and a rendered environment at
  `/etc/goby/goby.env`. Preserve each installed source hash, destination
  inode/owner/mode and creation intent. Root owns the binary and unit; the
  environment is root 0600 in root 0700 `/etc/goby`. Let systemd create its
  declared State/Cache/Logs/RuntimeDirectory paths. Do not redirect them to
  an easier writable test directory or run the application as root.
- Render only declared fixture substitutions: fresh DB/setup secrets,
  loopback HTTP listen/public origin, `GOBY_COOKIE_SECURE=false` for that
  direct local HTTP profile, no trusted proxy, fixture name/media root, and
  the verified FFmpeg/FFprobe absolute paths. The template's `/opt/ffmpeg/...`
  paths must not be assumed to exist: the known test tools are under
  `/opt/goby-toolchains/ffmpeg-9.0.1/bin`. Omit `GOBY_WEB_DIR`; retain the
  template's key, cache and log paths and the application's default
  `/var/lib/goby/recovery`, `/var/lib/goby/recovery-operations` and
  `/var/lib/goby/backups` paths. The template does not explicitly set those
  three recovery paths; bind their effective defaults during installation.
  No shell sourcing or plaintext environment in public evidence.
- Use only an explicitly recorded test isolation/resource drop-in: join the
  owned network namespace, make the new media read-only, and hide protected
  installations and their Unix-socket paths. Retain every packaged hardening
  directive, `User/Group=goby`, executable/CWD/environment paths and directory
  declarations. Do not loosen `ProtectSystem`, capabilities or other sandbox
  settings. Preserve `Restart=on-failure`; any unexpected automatic restart
  fails acceptance and triggers closure of this owned unit. Do not enable it
  at boot. Loaded unit properties and actual process credentials/namespaces
  must confirm the claimed profile.
- Bootstrap once, log in, create two libraries and fresh ACL users, scan the
  seven real media files, write the existing small UserData fixture, then
  close every obtained credential with logout 204 / same-credential 401.
  Verify the embedded index and every manifest asset/reference by HTTP hash,
  with no external admin directory. Perform a normal `systemctl stop`, then
  start the same installed unit a second time over the same PG/data/media.
  Before any new scan or UserData mutation, verify initialized setup,
  catalog/ACL/IDs/UserData, core-table rows and xmin; log in and close the new
  credentials. Stop normally again. Require two distinct process identities,
  the package binary, UID/GID 995/986, zero automatic restarts, successful
  exits, shutdown completion, no ERROR-level application log events and no
  surviving application processes or cgroups. Preserve expected auth-denial
  HTTP responses rather than calling all non-2xx responses failures.

Reuse the verified `CatalogJourney` HTTP recorder, `first_start`,
`second_start`, and `cleanup` in `.git/native-catalog-journey.py`; its seven-file
recipe and snapshot comparisons are useful. Its existing root-process launcher
in `.git/native-catalog-runtime.py` is not formal systemd acceptance and must
not be replayed. A small scenario-specific owner should provide actual unit
identity checks before HTTP, normal systemd start/stop and independent SQL
closure. Reuse existing command, listener and process primitives. Do not add a
new general runner or repeat the full recovery/client journeys.

## Budgets, closure and stop conditions

The E10 regression and E11 package build have closed their owned resources.
Installation admits only a fresh PG/data allocation within an owned 768 MiB
tmpfs; the completed 3 GiB build tmpfs is not provisioned again. Serialize new
heavy work on the shared environment. Require
fresh MemAvailable of at least 6 GiB and root free bytes of at least 512 MiB
reserve plus the complete package/install/evidence/closed-PG allowance. Set
that allowance from actual output sizes before mutation, not compressed backup
size or historical free space. The tiny media corpus needs only its measured
bounded ext4 allocation. This is neither capacity nor host-durability evidence.

The completed build's normal preservation kept the 512 MiB root reserve. Its
sealer permits only emergency failure receipts to use that reserve: at most
three attempted files of 8 MiB each, including a partial failed primary receipt.
That exception was not used in the successful run; it is not a general write
allowance. The build retained 329,647,450 bytes and closure observed
6,428,753,920 root bytes free. Installation still requires fresh capacity checks.

Bound setup/scan readiness and HTTP using the existing journey limits, with a
separate credential-cleanup reserve and an outer deadline covering normal
`TimeoutStopSec=20s` plus terminal observation. Keep unit resource ceilings
explicit in the isolated profile. Unexpected exits, ownership/protection
drift, space pressure, incomplete auth cleanup, unexpected DB writes, missing
assets or any forced kill stop the scenario and preserve failure evidence;
they cannot be converted to success by a second attempt. Do not automatically
retry, rename a scope to evade its failure, or replay consumed inputs.

The first runtime entry failed before application or business dispatch because
of the demonstrated missing import. After independent preservation and complete
disposal of that failed installation, one fresh attempt may be reviewed against
the import correction and newly frozen source/input identities. Use fresh
secrets, namespace, PostgreSQL and catalog state; retain the original failure
and its count across scopes. Reuse the unchanged E11 package and M5 product
evidence. Recheck affected helper interfaces, current tool/resource/protected
identities and installation-path absence. A second entry or controller failure
pauses installation acceptance; it does not automatically admit a third attempt.
No new attempt is admitted before these prerequisites are complete.

The first preservation checker subsequently rejected the real stopped-unit
receipt: it assumed retained process-exit codes, but both observed codes were
zero with an empty invocation. Its ten metadata commands closed, and no archive,
move, removal or unmount occurred. New installation attempts are now paused.
Correct only the required failure-preservation path against the actual receipt,
successful stop commands and private PostgreSQL shutdown log; retain both
failures and reassess the execution approach before further installation work.
The corrected preservation has now passed a real read-only precheck, independent
evidence review and full archive readback. Both private retained trees and all
five installation copies matched their inventories; the old installation paths,
three unit registrations, namespace and PG tmpfs are absent. Only empty fixture
directories remain. No application, SQL or HTTP journey was executed by closure.

On success, independently bind package inputs, installed/loaded unit, both
processes, full HTTP/raw response receipts, database observations and unchanged
media. Seal private logs, environment and closed PG evidence before removing
only this newly installed binary, unit, isolation drop-in and exact created
configuration/data/cache/log directories. Stop only the recorded owned units;
never kill a backend or stop a foreign service. Verify PG/backend closure,
ordinary unmount, namespace disappearance and daemon-reload removal of the new
unit. RuntimeDirectory may have been removed by systemd already: record that
fact. Refuse cleanup of a changed path or unexpected descendant; retain it
with explicit responsibility. Do not delete the shared account, protected
candidate state, old archives, or private failure/evidence records. A failed
scenario retains its installation/data after owned-process closure until a
separate reviewed disposal decision.

For this fixture, preserve the entire newly created state, configuration,
cache, log and media directories with exclusive same-filesystem renames into
the scope's root-private `retained` directory after the application stops.
Verify their identities before and after the move. Verification tools observe
master-key metadata only; they do not read those bytes or place them in an archive.
The standard installation paths become absent while this private state remains
retained. Closed PostgreSQL and HTTP/SQL/journal evidence may be archived only
within the private scope, with complete readback before the owned PG tmpfs is
ordinarily unmounted. The shared accounts and all earlier installations remain
outside this disposal scope.

## Result boundary and remaining inputs

The post-closure execution review found that normal runtime and seal checks
also assume retained exit fields in `systemctl show`. The failed infrastructure
scope demonstrates that those fields can be unavailable; it does not establish
what the unstarted application would report. Before another installation,
retain the invocation's exit result with a bounded observer established before
stopping. Reuse the existing invocation journal, `shutdown.completed` event,
process/cgroup and database-backend checks, and bind the manager's stop outcome.
An unavailable exit result stays unknown. Runtime and sealer must consume the
same saved evidence and distinguish physical closure from proved normal exit.
The future normal sealer also needs the exact, metadata-checked
`PG/data/postmaster.opts` exception already used in failed-scope preservation;
other master/key files remain metadata-only. These are prerequisites for a
reviewed execution decision, not permission to replay or start another scope.

Passing would prove one actual Linux amd64 embedded package installed and run
through the shipped nonroot systemd template, with normal application
stop/start and persistent catalog/auth state in the stated isolated profile.
It does not prove upgrade, boot enablement, PG restart, host reboot/power loss,
native arm64, GPU, OCI, browser/core-video acceptance or full M6 completion.

The completed M5 full-suite source/review/closure bindings are recorded in
[the final regression receipt](m5-final-regression-verification.json). Before
each new remote phase, bind the final package source and asset pins, fresh
resource facts and finite execution budgets. Installation additionally needs
fresh absence/UID facts, fixed namespace/port/DB and cleanup inventories, and
one reviewed scenario input. These are measured execution inputs, not
open product-design questions. The selected design remains manual internal
tar installation, the existing account/template, a fresh isolated database and
two normal starts; no new product feature or public release is proposed.
