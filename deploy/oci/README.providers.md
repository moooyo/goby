# Online providers in Docker

Status: **COMPLETE within the selected credential-free scope**. Real MusicBrainz
Docker HTTP, override/lock preservation, scan/recreation, both application image
updates, archive save/load and owned-resource closure passed. TMDB and
OpenSubtitles remain **PENDING CREDENTIALS**; configuration or offline tests are
not online acceptance. See the
[result record](../../docs/development/online-providers-20260930.md) and
[execution plan](../../docs/planning/online-providers-plan-20260930.md).

Use this guide with the [software deployment guide](README.md), or the
[AMD guide](README.amd.md) when that profile is selected. Goby is delivered only
as a Docker image under the [Docker delivery policy](../../docs/planning/docker-delivery-policy.md).
Both profiles retain external PostgreSQL 17, UID/GID `10001:10001`, a read-only
container root, and read-only media by default.

For this application update, use the new immutable image identities in the
[provider result](../../docs/development/online-providers-20260930.md); the older
base-image receipts remain historical acceptance records. The
[application-update recipe](Dockerfile.application) retains the accepted tools.
This increment does not claim a new browser E2E run or GPU verification campaign.

## Enable credential-free MusicBrainz

1. Edit the private application environment file named by `GOBY_CONFIG_FILE`.
   Enable the startup switch and use an identifying User-Agent. No MusicBrainz
   key or account is needed. These are the supported defaults and switch:

   ```dotenv
   GOBY_ONLINE_PROVIDERS_ENABLED=true
   GOBY_MUSICBRAINZ_USER_AGENT=Goby/0.1 (https://github.com/moooyo/goby)
   ```

   An operator may provide an application/version and a real contact URL or
   email. Leave TMDB and OpenSubtitles credentials unset for this workflow.
2. Recreate the application using the same deployment files as its existing
   installation. An environment-file change is a startup configuration change:

   ```sh
   docker compose --env-file deployment.env -f compose.yaml config --quiet
   docker compose --env-file deployment.env -f compose.yaml up -d --force-recreate goby
   ```

   Include the existing AMD override when applicable. Do not omit an override
   that supplies the installation's selected hardware profile.
3. Sign in as an administrator and enable internet providers in Settings.
   The persisted setting is `Management.Metadata.EnableInternetProviders`.
   Both this setting and the startup switch must be true. Native settings API
   callers must use the current revision and preserve the complete supplied
   Management object; see [settings](../../docs/api/settings.md).
4. Open **Online providers**. MusicBrainz should be configured; TMDB and
   OpenSubtitles should remain unavailable without their credentials.
   `GET /admin/v1/providers` exposes readiness and attribution, not secrets.
   Configured status establishes configuration readiness, not network success.
5. Open a music item's **Online sources**, choose MusicBrainz, and search for
   the intended artist, album or audio recording. Inspect the returned match
   before **Apply metadata**. Use **Refresh saved match** for subsequent
   refreshes. The adapter maps albums to release groups, artists to artists,
   and audio to recordings; typed IDs retain that distinction.
6. Inspect **Saved metadata sources**, re-scan the library, and restart the
   application. Check that the selected ID, provenance and effective fields
   persist, with the same catalog identity and user data. Local facts,
   administrator overrides and locked fields keep their existing precedence.

MusicBrainz requests share a process-wide gate with at least one second between
completed requests. Run this workflow serially. Multiple Goby containers do not
coordinate that gate. A bounded provider error or quota response should be
reported and retried later, not turned into an unbounded retry loop.

The administrator HTTP workflow uses these item-relative routes:

| Route below `/admin/v1/items/{id}/providers` | Purpose |
| --- | --- |
| `POST /search` | Search using `Provider`, optional name/year, and language |
| `POST /apply` | Apply a returned `Provider`/`Id` with the current metadata `Revision` |
| `POST /refresh` | Refresh the saved match with the current metadata `Revision` |
| `GET /provenance` | Read retained provider ID, source, fetched time and fields |

Use the native administrator session and normal same-origin/CSRF handling for
mutations. Keep authentication material out of shell history and captured logs.

## Prepare credentialed providers

The application accepts the following startup settings. Obtain and store real
credentials separately before selecting a live workflow; this guide includes no
claim of TMDB or OpenSubtitles online success.

| Provider | Environment settings | Purpose |
| --- | --- | --- |
| TMDB | `GOBY_TMDB_TOKEN` or `GOBY_TMDB_TOKEN_FILE` | API Read Access Token for metadata and images |
| OpenSubtitles | `GOBY_OPENSUBTITLES_API_KEY` or its `_FILE` form | Application API key |
| OpenSubtitles | `GOBY_OPENSUBTITLES_USERNAME` and `GOBY_OPENSUBTITLES_PASSWORD`, each optionally `_FILE` | Account credentials; configure both together |
| OpenSubtitles | `GOBY_OPENSUBTITLES_USER_AGENT` | Application identification; default `Goby v0.1` |

Choose exactly one nonempty form for each secret. `_FILE` paths are container
paths to readable regular files, not host paths or symlinks. Values must be
bounded single-line text; a single trailing line ending is accepted in a file.
The operator must supply read-only mounts and permissions readable by UID 10001.
The base Compose file does not automatically mount credential files.

For example, a private operator-owned `compose.providers.yaml` can add this
mount; add equivalent mounts for each selected OpenSubtitles credential:

```yaml
services:
  goby:
    volumes:
      - type: bind
        source: /etc/goby/provider-secrets/tmdb-token
        target: /run/secrets/tmdb-token
        read_only: true
        bind:
          create_host_path: false
```

Set `GOBY_TMDB_TOKEN_FILE=/run/secrets/tmdb-token` in the private application
environment file and include `-f compose.providers.yaml` when recreating the
container. Keep secret files outside the checkout and build context. Do not
print their contents, environment dumps, or rendered Compose configuration;
use `config --quiet` when checking Compose configuration. Provider status DTOs
are the appropriate readiness surface. Read the
[adapter contract](../../internal/providers/README.md) for attribution and
provider-specific behavior before selecting credentialed acceptance.

## Optional subtitle write access

Metadata and selected images use catalog/database storage and need no writable
media mount. Downloaded subtitles are different: Goby publishes a new sidecar
in the source media directory. The default read-only `/media` mount cannot
support a subtitle download.

The optional `compose.subtitles.yaml` is provided for explicit selection.
It makes the `/media` bind for `GOBY_MEDIA_DIR` writable and mounts the Docker
host's `/etc/machine-id` read-only, as required by recoverable sidecar deletion.
`GOBY_MACHINE_ID_FILE` may select another existing host path to that stable
machine identity; it is a Compose variable, not an application credential.
Do not generate a new identity on each container recreation. Use an
operator-approved media directory, preferably a bounded dedicated selection,
and give UID/GID 10001 write/traverse permissions on the intended directories.
Do not recursively change ownership of an existing collection merely to enable
this option. Retain the read-only container root, non-root user, capability
restrictions, and external database configuration.

When writable media is explicitly selected,
include `-f compose.subtitles.yaml` with the base Compose file and any existing
AMD/private-secret overrides. Its inclusion permits media sidecar writes; it
does not enable providers or grant an OpenSubtitles account quota.

Goby checks target-directory writability before a quota-bearing download;
selected offline checks passed with non-root permission and real read-only
filesystem failures. The optional override also passed actual UID 10001 media
create/remove and read-only machine-ID checks. Publication still rechecks source
identity and must not overwrite an existing file. Actual OpenSubtitles login,
quota handling, download, sidecar
publication and playback remain unaccepted until credentials and that live
workflow are supplied. Configuration preparation alone is not a pass.

## Disable and retain local state

Turn off the managed internet-provider setting to stop enabling new provider
work, or set the startup switch to false and recreate the container. Previously
accepted metadata is persisted catalog state; disabling providers does not
itself remove it. Follow the base guide for normal stop, backup and rollback.
