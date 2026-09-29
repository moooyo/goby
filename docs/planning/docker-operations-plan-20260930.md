# Docker installation and operations plan

Status: **COMPLETE within the selected Docker operations scope**. The helper's
14 unit tests, actual software Docker journey and owned-resource closure passed.
See the [result record](../development/docker-operations-20260930.md).
Git integration is recorded separately; no merge or push is claimed here.
Decision date: September 30, 2026. Baseline: `553c9d4`.

## Selected outcome

Provide one short installation path and a small local operations helper for the
existing Linux amd64 software and AMD Docker deliveries. Reuse application source
`76d64bf0087f3cd2e40addde8067f4e6e65b2bac` and the accepted image/archive identities
from the [provider delivery](../development/online-providers-20260930.md).
This increment changes the operator toolkit and documentation, not Goby or its
media-tool images. The user explicitly deferred further online-provider work;
no provider credentials are needed for the current operations plan.

The [Docker-only policy](docker-delivery-policy.md) remains in force. External
PostgreSQL 17 and any reverse proxy stay operator-provisioned. This work does not
bundle a database, create a native installer, publish to a registry, or require
source-build tools on the installation host.

## Deliverables and completion evidence

| Deliverable | Acceptance | State |
| --- | --- | --- |
| `deploy/oci/goby-docker.py` | Python 3.10+ standard library; explicit prepare/check/start/status/stop/logs commands; 14 remote unit tests | PASS |
| `current-release.json` | Both accepted profiles bound to exact archive identities and per-profile companion hashes | PASS |
| [Quick start](../../deploy/oci/QUICKSTART.md) | CLI aligned to observed behavior; concise manual update procedure | Updated |
| Small operations toolkit | `goby-docker-operations.zip`: helper, catalog, quick start, Compose/seccomp companions and local guides; existing image archives reused | Identity and handoff in the result manifest |
| Runtime evidence | Actual preparation, readiness, bootstrap, scan/playback, database outage/recovery and stop | PASS |
| Final resource closure | Zero owned containers/networks; PostgreSQL stopped with zero clients; protected services unchanged; data/evidence retained | PASS |
| Git integration | Recorded separately from runtime results | No completion claim |

## Finite execution sequence

1. **Implement and align the CLI — complete.**
   `prepare` takes the selected release directory, new installation directory,
   media path, private database URL file, public URL, host port and profile.
   Optional inputs cover a database CA, recovery database URL file, writable
   subtitles and the AMD render node. Prefer `current-release.json` in the
   release directory, falling back to the helper's directory. Verify the archive
   hash and image identity, load only when needed, copy the helper/companions,
   and generate private application/Compose environment files and setup token.
   Preparation must not start the application.
2. **Verify focused helper contracts on `test-env` — complete.**
   Check valid software/AMD configuration, manifest/hash rejection, private
   file handling, new-directory ownership, secret-free diagnostics and command
   construction. Keep checks bounded to the helper and its installation surface;
   do not repeat the accepted Go or GPU campaigns.
3. **Run the actual Docker operator journey — complete.**
   Prepare an isolated installation with an external test database. Verify no
   application starts during preparation. Run check/start/status/logs/stop with
   the installed helper; verify readiness, a useful bounded failure diagnosis,
   normal shutdown and retained state. The actual journey uses the software
   profile. AMD/subtitle configuration retains its existing device/mount
   contracts and focused helper coverage; no new GPU or live provider acceptance
   is claimed. Check for secret exposure in outputs.
4. **Close owned resources and define the toolkit handoff — complete.**
   Closure preserves the test database, installations, configuration, media and
   failed evidence. Protected services are unchanged. The small toolkit is handed
   off at `D:/Code/goby/.artifacts/docker-operations-20260930/goby-docker-operations.zip`;
   its final size/hash are recorded in the
   [result manifest](../development/docker-operations-results-20260930.json).
   It serves the catalog's accepted images without repackaging their archives.
5. **Complete the result — complete; Git integration tracked separately.**
   The result records runtime and closure evidence. Merge/push status remains
   separate and does not expand the accepted operations scope.

## Operational boundaries

- Linux root runs the helper to use Docker and create/chown only new installation
  state/cache/log directories. It must not recursively chown or modify media.
  The application remains UID/GID `10001:10001`, with a read-only container root
  and read-only media unless the subtitle override is explicitly selected.
- Read the database URL from a private one-line file (`0600` or stricter), not a
  command-line URL. Print the setup-token path, not the token. Do not print
  environment dumps, database credentials or rendered secret configuration.
- `check` establishes Compose/storage/UID conditions, not database login.
  Actual `start` and readiness establish connectivity; `status` uses safe
  diagnostic codes. A successful status query is not necessarily a ready
  application. Status inspects the actual loopback `8096/tcp` mapping before
  probing and refuses a desired/actual port mismatch. Start has a bounded wait;
  logs have a bounded line count and configured-secret redaction.
- Keep `restart: "no"`, existing resource limits, external PostgreSQL and the
  existing AMD device/seccomp configuration. Preparation does not imply startup
  on host reboot or successful production HTTPS deployment.
- Helper `stop` retains its container, network and persistent data. The acceptance
  fixture's later retirement used a separate operator `compose down`; this is
  distinct from the helper's normal stop behavior.
- Updating remains manual: verify the archive, preserve the old image ID, stop
  and back up the database plus state/master key, load the new image, update
  `deployment.env`, then start and check readiness. There is no helper upgrade
  command or automatic database rollback promise.
- Completed image/GPU/provider evidence remains bound to its original receipts.
  This toolkit does not introduce a new image, browser E2E result, GPU campaign,
  scraper workflow or alternative supported delivery form.
