# Online providers: selected execution plan

Status: **COMPLETE within the selected credential-free scope**. Offline contracts,
real MusicBrainz Docker HTTP, both application image updates, archive save/load
and owned-resource closure passed. Git integration is handled separately; this
plan does not claim a merge or push. See the
[result record](../development/online-providers-20260930.md).
Decision date: September 30, 2026. Baseline: `6288457`.

## Selected scope

The user selected credential-free provider work first. MusicBrainz is the only
provider selected for live acceptance in this increment. TMDB and OpenSubtitles
have no supplied credentials. Their configuration and selected offline contracts
are prepared and verified, but their online acceptance remains **PENDING
CREDENTIALS**.

The [Docker-only delivery policy](docker-delivery-policy.md) applies. The software
and AMD Docker archives contain the accepted application changes. Previously
accepted media-tool identities are retained as an explicit source/build bridge;
this application increment did not repeat the AMD GPU toolchain acceptance.
There is no standalone installer or registry publication in this work.

| Work | Completion evidence | Current state |
| --- | --- | --- |
| Provider configuration and offline contracts | Selected provider/config/server checks and subtitle preflight/deletion/restart checks passed | PASS |
| MusicBrainz real Docker workflow | Typed-ID first refresh, search/apply, overrides/locks, stale/disabled rejection, scan and container recreation | PASS |
| TMDB metadata and images | Configuration and offline checks only; live credentials required | Pending credentials for online acceptance |
| OpenSubtitles | Configuration, offline checks and optional writable-media/machine-ID mounts prepared | Pending credentials for online acceptance |
| Updated software and AMD image archives | Both archives saved and loaded; software HTTP and AMD startup/state/real refresh/clean stop passed; inherited tool hashes unchanged | PASS |
| Owned-resource closure | Owned containers/network removed; isolated PostgreSQL stopped without clients; application port closed; protected services unchanged | PASS |
| Git integration | Final docs, merge and push after delivery closure | Handled separately; no completion claim |

## Execution order

1. **Close the two local defects and verify the offline contract — complete.**
   Resolve typed MusicBrainz IDs by item type before falling back to the legacy
   generic ID. Check the target subtitle directory's actual writability before
   consuming a quota-bearing download. Preserve held-root/source checks and
   exclusive sidecar publication; preflight success cannot guarantee a later
   write. Cover negative cases without live TMDB/OpenSubtitles requests.
2. **Run one bounded real MusicBrainz workflow through Docker HTTP — complete.**
   Use an isolated external PostgreSQL database and a small music fixture with
   readable media. Enable both the startup switch and the persisted management
   switch. Search for a known recording/artist/album appropriate to the item,
   explicitly select its returned ID, apply it, and refresh the saved match.
   Record the effective fields, typed provider ID and provenance. Re-scan and
   restart the container, then verify those facts, item identity and user data
   persist. Check the disabled-switch behavior without claiming an upstream
   network failure is a product failure. Keep requests serial and bounded.
3. **Close the runtime resources and retain evidence — complete.**
   Stop the owned application normally; record exit, released ports/processes,
   and the disposition of its database and files. Preserve unsuccessful
   attempts with their causes. Never disturb unrelated containers or databases.
4. **Update both Docker archives — complete.**
   Package the accepted application into the software and AMD profiles. Record
   source and binary hashes, image IDs, archive sizes/digests, and unchanged
   tool hashes. Load the resulting archives and check the application/config
   bridge in the actual containers. Reopen media-tool checks only if their
   inputs or executable identities change.
5. **Publish the bounded result — complete; Git integration tracked separately.**
   The guide and result record contain observed outcomes and retain absent
   credential cases as pending. Merge and push are separate integration actions,
   not evidence that additional providers have passed.

## Runtime and acceptance boundaries

- `GOBY_ONLINE_PROVIDERS_ENABLED=true` and
  `Management.Metadata.EnableInternetProviders=true` are both required.
  Credentials remain startup-only private settings, outside managed DTOs.
- MusicBrainz requires no key. Its default identification is
  `Goby/0.1 (https://github.com/moooyo/goby)`. Its shared in-process gate spaces
  completed requests by at least one second; independent containers do not
  share that gate. This acceptance uses one requesting application process.
- TMDB's token and OpenSubtitles' API key, username and password support either
  direct environment values or `_FILE`, exclusively per setting. Operators
  supply private regular files through read-only container mounts. Evidence
  must not contain credentials, login tokens or temporary download URLs.
- Default Compose keeps media and the container root read-only, runs as
  UID/GID `10001:10001`, and uses external PostgreSQL 17. Metadata and selected
  artwork do not require writable media.
- Subtitle downloads publish sidecars beside media. The optional
  `compose.subtitles.yaml` prepares write access only to the explicitly selected
  `GOBY_MEDIA_DIR` and mounts the stable host `/etc/machine-id` read-only for
  recoverable sidecar deletion. `GOBY_MACHINE_ID_FILE` selects another existing
  host path when needed. Preparing that override is not a live download
  acceptance or authorization to use credentials that have not been supplied.
- Completion means the selected credential-free workflow and updated Docker
  delivery pass. It does not mean all providers, public licensing/distribution,
  unrelated hardware, or historical performance matrices have passed.

Operator instructions: [Docker provider guide](../../deploy/oci/README.providers.md).
Implementation contract: [provider adapter notes](../../internal/providers/README.md).
