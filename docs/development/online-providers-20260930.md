# Online provider verification - September 30, 2026

Status: **COMPLETE within the selected credential-free scope**. The selected
offline checks, real MusicBrainz Docker HTTP, both application image updates,
archive save/load and owned-resource closure passed. Git integration is handled
separately; this record does not claim a merge or push.

The user selected credential-free acceptance first. TMDB and OpenSubtitles
remain **PENDING CREDENTIALS** for online acceptance. Their offline/configuration
results below do not establish successful upstream authentication, artwork
downloads, subtitle quotas or real subtitle downloads.

See the [execution plan](../planning/online-providers-plan-20260930.md),
[Docker operator guide](../../deploy/oci/README.providers.md), and
[Docker-only delivery policy](../planning/docker-delivery-policy.md). Exact
artifact and evidence references are collected in the
[result manifest](online-providers-results-20260930.json).

## Recorded identities

| Input or artifact | Recorded identity |
| --- | --- |
| Application source | `76d64bf0087f3cd2e40addde8067f4e6e65b2bac` |
| Goby binary SHA-256 | `65f447ef9f74c35134d25e8179f44699b9c62a77bc7ca2145f440a1919bbbfdc` |
| Goby binary size | 50,367,426 bytes |
| Updated software image | `sha256:fa13611dbb940a2a876d9f52606d3bc05ccc6364b4f5fc8de19bec86204c3cb4` |
| Updated AMD image | `sha256:a35b7653d262afcf006b95f31f450b58d89ce11b886279e2685a8a2ea0d3178e` |
| Software archive size | 388,846,080 bytes |
| Software archive SHA-256 | `f35b29ffb4257f04d607d6d67d61b1dfda290a84f7b83e5db4b0509de0ba92e1` |
| AMD archive size | 941,316,608 bytes |
| AMD archive SHA-256 | `383e372d1ca1aa7caccbf7e81673b5e6c17ac741a902da0e473a27614f5ec767` |
| Actual archive save/load | PASS for both profiles |

The local delivery destinations are
`D:/Code/goby/.artifacts/oci-providers-20260930/software` and
`D:/Code/goby/.artifacts/oci-providers-20260930/amd`.

The two images contain the same new application binary. Media tools are inherited
from the accepted [software](oci-delivery-20260929.md) and
[AMD](oci-amd-delivery-20260929.md) deliveries. This is an application update;
the earlier GPU/media-tool results are reused through unchanged tool identities,
not presented as newly rerun GPU acceptance. Both complete base layer sequences
remain prefixes of the updated images, followed by two application/probe layers.
FFmpeg, ffprobe and PostgreSQL tool hashes are individually unchanged. The
[application recipe](../../deploy/oci/Dockerfile.application) records this update.

## Repairs and offline results

| Area | Observed result |
| --- | --- |
| Provider/config/server selection | All selected offline checks passed |
| MusicBrainz ID fallback | Item-type-specific MusicBrainz IDs resolve before the legacy generic ID when no saved provenance exists |
| Subtitle write preflight | Five added cases passed, including non-root `EACCES` and an actual read-only mount |
| Sidecar deletion and restart | Selected checks passed after the optional Compose deployment supplied a stable machine identity |
| Preflight temporary-file cleanup | Created file descriptor remains held until unlink completes; the corrected check passed on rerun |

The composed regression result is **34 parent passes, 20 subtest passes, zero
failures and zero skips**. It reuses only the four existing metadata checks from
`library01`; the five preflight cases use their replacement `library03` results.
Superseded preflight executions are not added again to the totals.

The subtitle preflight checks the target directory before requesting a
quota-bearing download. It does not replace the later held-root/source identity
checks or exclusive publication. A successful preflight cannot guarantee that
the filesystem remains writable through publication.

The first Docker sidecar-deletion run failed because the container did not have
the required machine identity. The narrowly scoped deployment correction adds a
read-only host `/etc/machine-id` mount to `compose.subtitles.yaml`, alongside its
explicit writable-media bind. `GOBY_MACHINE_ID_FILE` can name an alternate stable
host file. Default Compose retains read-only media and no new machine-ID mount.
The failed attempt remains distinct from the later passing checks.

The optional override also passed actual UID 10001 media temporary-file creation
and removal, with `/etc/machine-id` read-only. Its first configuration checker
incorrectly required an explicit `read_only: false`; Compose omits that default.
The checker was corrected and only the overlay checks were repeated. The failed
attempt is retained. TMDB and OpenSubtitles credentials were not used.

## Real MusicBrainz Docker HTTP result

The local fixture was a synthetic FLAC carrying the typed MusicBrainz release
group tag for the real album **Kind of Blue**,
`8e8a594f-2175-38c7-a871-abb68ec363e7`. It started without a generic MusicBrainz
ID or saved provider provenance. This made the first refresh exercise the typed
ID fallback instead of an already accepted provider match.

The previous software image
`sha256:ff9beeb782aea48dbb19630712b67824c6ef775c5886b7bac9ef856fb5c5dcab`
reproduced the original `409` refresh defect. The updated software image passed
this sequence through the actual Docker application HTTP handlers:

1. Enable both the startup and managed provider switches; refresh the typed
   album ID before any search or apply, and retain the returned provenance.
2. Perform a real MusicBrainz search, explicitly apply the selected match and
   refresh its metadata.
3. Retain a manual `Name` override and an `Overview` lock across provider work.
4. Reject a stale revision with `409` and reject provider work with `409` while
   the managed switch is disabled; re-enable and refresh successfully.
5. Re-scan the library and recreate the container with the same image; retain
   both the Album and Audio catalog IDs and the accepted metadata.
6. Stop the application cleanly.

This proves the selected album workflow against MusicBrainz, including the
typed-ID defect and persistence boundary. It does not claim a new browser E2E
run, every music entity or every upstream response. MusicBrainz remains keyless
with identifying User-Agent and the existing process-wide request gate.

The updated AMD image then started against the retained Album and Audio state,
preserved the metadata locks, performed a real MusicBrainz refresh, and stopped
cleanly. This verifies the new application with the inherited AMD tools; it is
not a repeat of the previous GPU campaign.

## Retained build failure

Image build 01 compiled the application successfully, but its `--help` probe
failed because Goby does not support that flag. Build 02 reused that exact binary,
checked that it was executable and not writable, and passed. The actual software
and AMD application startups provide the runtime evidence. The failed probe and
its logs remain retained; no application compile failure is inferred from it.

## Delivery and resource closure

| Item | Observed result |
| --- | --- |
| Software and AMD archives | Both saved and loaded successfully with the identities above |
| AMD application check | Startup, inherited catalog/locks, real MusicBrainz refresh and clean stop passed |
| Owned containers and network | Zero remaining |
| Isolated PostgreSQL on port 55994 | Zero clients, normal stop; database data retained |
| Application port 38961 | Closed |
| Three protected containers | Original IDs, PIDs and start times unchanged |
| Unsuccessful attempts | Logs and failed states retained |
| Git merge/push | Handled separately; no completion claim |

The result remains bounded to credential-free
MusicBrainz, the selected offline repairs, and both Docker application updates.
Default UID/GID `10001:10001`, read-only container root, external PostgreSQL 17
and explicitly optional subtitle write access remain the deployment boundary.
