# Official Emby Reference Server

Status recorded at `2026-09-08T22:37:09Z` on the authorized Linux `test-env` host. This document records observations from an isolated official Emby Server instance. It does not establish that Goby implements or passes these contracts.

Later bounded studies are documented separately: [HLS](hls-reference.md),
[Universal/progressive audio](audio-reference.md), [audio profiles](audio-profile-reference.md),
and [progressive-video profiles](video-progressive-reference.md). The retained
corpus now contains 828 JSON records, including supporting observations and
preserved incomplete responses; historical counts below describe their original
capture boundaries. The M4e video extension adds 92 records with 61 complete HTTP
captures and does not establish general copied-video seek correctness.

## Provenance

| Property | Recorded value |
| --- | --- |
| Product and version | Emby Server `4.9.5.0` |
| Official release | [MediaBrowser/Emby.Releases, tag 4.9.5.0](https://github.com/MediaBrowser/Emby.Releases/releases/tag/4.9.5.0) |
| Release publication | `2026-05-18T19:24:03Z`; GitHub metadata has `prerelease=false`, `draft=false` |
| Package | `emby-server-deb_4.9.5.0_amd64.deb` |
| Package URL | [Official Linux amd64 DEB](https://github.com/MediaBrowser/Emby.Releases/releases/download/4.9.5.0/emby-server-deb_4.9.5.0_amd64.deb) |
| Download size | `190375356` bytes |
| SHA-256 | `1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843` |
| Hash evidence | Official release API asset `digest`, independently checked against the downloaded package on `test-env` |
| Package metadata | `Package: emby-server`, `Version: 4.9.5.0`, `Architecture: amd64`, `Installed-Size: 727522` KiB |
| Actual server version | `GET /emby/System/Info/Public` returned `Version: "4.9.5.0"` |
| Related SDK baseline | [Pinned SDK specification](../sources/emby-sdk-openapi.snapshot.json), associated with SDK release 4.9.5.0 |

The package was extracted with `dpkg-deb`; its installation scripts were not run. The vendor server binaries were not modified, copied into this repository, decompiled, or used as implementation source. The official package's shell launcher was inspected only to identify supported program-data and bundled-tool arguments. Ordinary publicly served wizard assets were inspected to reproduce the initial setup requests.

## Deployment and isolation

The host is Debian 13 amd64. The existing `goby-foundation-test` service remained active with its original PID throughout capture, and its PostgreSQL database was not used or changed.

| Resource | Reference configuration |
| --- | --- |
| Transient service | `goby-emby-reference.service` |
| Service state at the recorded checkpoint | `active/running`, PID `3131777` |
| Package and runtime directory | `/dev/shm/goby-emby-reference` |
| Persistent data directory | `/opt/goby-test/emby-reference-data` |
| Ownership marker | `.goby-managed`, containing `goby-emby-reference-owned-v1`, in both dedicated directories |
| Private credentials and raw captures | `/opt/goby-test/emby-reference-data/private`; files use mode `0600` and directories use `0700` |
| Credentials file | `private/credentials.env`, `0600 root:root`; never copied into the repository |
| HTTP access for capture | `http://127.0.0.1:18097/emby`, accessible from inside the reference network namespace |
| Host-network reference listener | None; no reference listener on host port 18097 or 8096 |
| Namespace listeners actually observed | TCP `*:18097` and UDP `*:7359`, confined to the private network namespace |
| Network isolation | systemd `PrivateNetwork=yes`; reference and host network namespace identifiers were different |
| Service sandbox | `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `NoNewPrivileges=yes`; fixture tree mounted read-only |
| Resource limits | `CPUQuota=150%`, `MemoryMax=1G`, `TasksMax=256`, `LimitNOFILE=65536` |
| Checkpoint footprint | Approximately 895 MiB for package/extraction in tmpfs; 7.8 MiB persistent reference data; approximately 163 MiB service memory |

The official [network setup documentation](https://emby.media/support/articles/Hosting-Settings.html) describes the configured local IP as an address presented to clients; it does not establish a loopback-only socket bind. Therefore `LocalNetworkAddresses=["127.0.0.1"]` was not relied upon as a network boundary. The explicit HTTP port was configured before startup, and systemd network isolation prevented host listeners and all external network access, including discovery traffic and online metadata requests. No proxy or host-facing port forward was created.

The private service runs as root with the listed filesystem and network restrictions. Its source-media mounts are read-only. This is a limited reference-test setup, not a proposed production deployment for Goby.

The extracted package lives in volatile tmpfs, and the unit is transient. A host reboot removes that runtime and requires preparation/startup again. Persistent reference data, including the configured account and libraries, is separate. Do not rerun the initial setup stage on an initialized data directory.

## Ordinary setup and source media

The reference followed the regular first-run process described in the official [installation guide](https://emby.media/support/articles/Installation.html#running-the-startup-wizard): create a local administrator, configure local libraries and remote-access settings, and complete the wizard. No Emby Connect account was linked and no entitlement, paid feature, or activation mechanism was bypassed.

The publicly served 4.9.5.0 wizard uses:

- `GET /Startup/User` to read the initial username.
- `POST /Startup/User` with URL-encoded `Name` and `Password`; the observed response was HTTP 200 with `{}`.
- `POST /Startup/RemoteAccess` with URL-encoded `EnableAutomaticPortMapping=false`; the observed response was HTTP 204.
- `POST /Startup/Complete`; the observed response was HTTP 204.

These are observed startup requests from the bundled public client, not additions inferred from the SDK. The account is a synthetic `reference-admin` account with a random password kept only in restricted remote files. The completed server was then accessed through normal `Users/AuthenticateByName` authentication.

Three independent reference libraries read the existing owned synthetic fixtures:

| Library | CollectionType | Read-only source |
| --- | --- | --- |
| Reference Movies | `movies` | `/opt/goby-fixtures/movies` |
| Reference Shows | `tvshows` | `/opt/goby-fixtures/tv` |
| Reference Music | `music` | `/opt/goby-fixtures/music` |

Library options disabled realtime monitoring, marker/chapter-image work, local metadata writes, subtitle/lyrics downloads, and automatic refresh. `TypeOptions` explicitly contained empty metadata/image fetcher arrays for the relevant media types; the reference returned these arrays as empty. `PrivateNetwork` independently guaranteed no online access regardless of provider-default interpretation.

`EnableEmbeddedTitles=true` was explicitly selected. Thus the observed episode display names come from the synthetic container title, not necessarily the episode filename. The current audio fixtures have insufficient album/artist metadata to establish general music-album or artist behavior: this run returned audio items and a folder, with no `MusicAlbum` or `MusicArtist` results.

The movie fixture contains `Sample` in its filename and is small. With the server's returned default `SampleIgnoreSize=314572800`, the first scan omitted it. Changing only this option to zero on the dedicated reference movie library and refreshing made one `Movie` visible. Existing source files were neither renamed nor modified. The initial query/Latest fixtures remain from the first scan; the later movie/playback fixtures record the configuration change separately.

## Captured contracts

All 65 current fixtures are under [tests/compatibility/fixtures/reference/emby-4.9.5.0](../../tests/compatibility/fixtures/reference/emby-4.9.5.0). The final capture occurred at `2026-09-08T22:39:30Z`. Their request paths are API calls to this isolated instance, not calls to hosts embedded in downloaded Swagger files.

### Public information and Ping

| Request | Observed response |
| --- | --- |
| `GET /emby/System/Info/Public` | HTTP 200 JSON object containing `LocalAddresses`, `RemoteAddresses`, `ServerName`, `Version`, and `Id`; both address arrays were empty inside the isolated namespace. No `StartupWizardCompleted` field was present. |
| `GET /emby/Users/Public` | HTTP 200 JSON array; after setup it contained the synthetic account with password flags and activity/login timestamps. |
| `GET /emby/System/Ping` | HTTP 200, `Content-Type: text/plain`, `Content-Length: 11`, exact body `Emby Server` without a newline. |
| `POST /emby/System/Ping` | Same status, content type, content length, and body as GET. |
| `HEAD /emby/System/Ping` | HTTP 200, `Content-Type: text/plain`, `Content-Length: 11`, empty response body. |

Evidence: `after-setup-system-info.json`, `after-setup-users.json`, and `after-setup-ping-{get,post,head}.json`. Matching before-setup public captures are also retained.

### Authentication

Successful JSON responses use `Content-Type: application/json; charset=utf-8` and contain `User`, `SessionInfo`, `AccessToken`, and `ServerId`. The tested body uses `Username` and `Pw`.

| Client/device metadata carrier or error case | Status and observed body |
| --- | --- |
| `Authorization: Emby Client=..., Device=..., DeviceId=..., Version=...` | HTTP 200 authentication result. |
| `X-Emby-Authorization` with the same `Emby` value | HTTP 200 authentication result. |
| `Authorization: MediaBrowser Client=..., Device=..., DeviceId=..., Version=...` | HTTP 200 authentication result. |
| Only `X-Emby-Client`, `X-Emby-Client-Version`, `X-Emby-Device-Id`, `X-Emby-Device-Name` | HTTP 200 authentication result. |
| Unknown username | HTTP 401, `text/plain`, `Invalid username or password. Please try again.` |
| Wrong password for the synthetic account | HTTP 401 with the same text as unknown username. |
| Correct credentials, no client/device authorization metadata | HTTP 400, `text/plain`, `Value cannot be null. (Parameter 'appName')` |
| `Emby` client metadata present but `DeviceId` absent | HTTP 400, `text/plain`, `Value cannot be null. (Parameter 'reportedDeviceId')` |
| Body is the single character `{`, with JSON content type | HTTP 400, `text/plain`, `Value cannot be null. (Parameter 'name')` |
| Protected `GET /Users` with no token | HTTP 401, `text/plain`, `Access token is invalid or expired.` |

Evidence: `auth-*.json`. These are exact observations for the recorded requests; they do not establish precedence when multiple metadata carriers conflict, malformed authorization grammar, password-lockout policy, or all possible parsing errors.

A valid token supplied only through `X-MediaBrowser-Token` also authenticated `GET /Users/{userId}`: HTTP 200 with an 11-property user object. That request had no `Authorization`, `X-Emby-Authorization`, or `X-Emby-Token` header. Evidence: `auth-x-mediabrowser-token.json`.

### CORS preflight

The following unauthenticated request was captured:

```http
OPTIONS /emby/Items
Origin: https://example.invalid
Access-Control-Request-Method: GET
Access-Control-Request-Headers: X-Emby-Token,Content-Type
```

It returned HTTP 200, an empty body, `Content-Length: 0`, and `Content-Type: text/plain`. `Access-Control-Allow-Origin` echoed `https://example.invalid`, and `Access-Control-Allow-Credentials` was `true`. Allowed methods were `GET, POST, PUT, DELETE, PATCH, OPTIONS`. The returned fixed header list included both requested headers, `X-MediaBrowser-Token`, `X-Emby-Authorization`, and the four independent client/device headers. The full unmodified list is in `cors-options-items.json`. This is an HTTP preflight observation; no browser behavior or conflicting-origin policy was tested.

### Libraries, shows, and projection

| Request or behavior | Observed result |
| --- | --- |
| `POST /Library/VirtualFolders` with JSON `Name`, `CollectionType`, `Paths`, `LibraryOptions`, `RefreshLibrary=false` | HTTP 204 with empty body, for all three source libraries. |
| `GET /Library/VirtualFolders/Query` | HTTP 200 object with `Items` and `TotalRecordCount`; returned options include server defaults. |
| `POST /Library/Refresh` | HTTP 204 empty; scan completion was observed through subsequent queries, not implied by this response. |
| `POST /Library/VirtualFolders/LibraryOptions` | HTTP 204 empty when changing the dedicated movie library sample filter. |
| `GET /Shows/{seriesId}/Seasons?UserId=...` | HTTP 200 `Items`/`TotalRecordCount`; two `Season` items with `IndexNumber` 1 and 2. |
| `GET /Shows/{seriesId}/Episodes?UserId=...` | HTTP 200 `Items`/`TotalRecordCount`; three episodes ordered S01E01, S01E02, S02E01 in this fixture. |
| Episodes with `Season=1` | Two episodes, `TotalRecordCount=2`. |
| `GET /Shows/NextUp?UserId=...&Limit=10` | Empty `Items`, `TotalRecordCount=0` for the new account with no watch history. |
| `GET /Users/{userId}/Items?Recursive=true` | Includes source-root folders, series, seasons, and media. Default episode projection omits `Path`, `MediaSources`, and `MediaStreams`. |
| List query with `Fields=Path` | Adds `Path` while retaining the ordinary default fields; it is not an exclusive one-field projection. |
| List query with `EnableImages=false&EnableUserData=false` | Omits `ImageTags`, `BackdropImageTags`, and `UserData`. |
| List query requesting `MediaSources,MediaStreams,...` | Adds those requested structures and related media facts. An absent overview does not become an invented nonempty value. |
| `GET /Users/{userId}/Items/{itemId}` | The recorded movie detail has 46 properties, including `Path`, `MediaSources`, and `MediaStreams`. |
| The same detail endpoint with `Fields=Path` | Returned the same 46 property names in this case. Do not apply the list endpoint's projection behavior to item detail automatically. |

Evidence: `library-*.json`, `items-*.json`, `item-detail-*.json`, `shows-*.json`, and `users-views.json`.

### Latest grouping

`GET /Users/{userId}/Items/Latest` returns a top-level JSON array. During the initial scan, three episodes in one series and two ungrouped audio tracks were available:

- With `GroupItems` omitted, the response represented the three episodes as one `Series` item, plus the two audio items.
- With `GroupItems=false`, the response contained the three `Episode` items and two audio items.
- With `IncludeItemTypes=Episode` and grouping omitted, the response was an array containing one `Series` item.
- With `IncludeItemTypes=Episode&GroupItems=false`, the response was an array containing three `Episode` items.

Evidence: `latest-default-group.json`, `latest-group-false.json`, `latest-episodes-default-group.json`, and `latest-episodes-group-false.json`. A one-element response remains an array; consumers must not collapse it to an object. These cases do not establish album grouping or every limit/order rule.

### PlaybackInfo and HLS URLs

After the sample-filter adjustment, the movie's GET and minimal POST `PlaybackInfo` requests returned HTTP 200 with `MediaSources` and `PlaySessionId`; `ErrorCode` was omitted. Both returned a local MP4 source, `DefaultAudioStreamIndex=1`, and false `RequiresOpening`/`RequiresClosing`. All three support flags were true in the minimal cases, and no transcoding URL was returned there.

A bounded HLS device profile with direct delivery disabled returned a `TranscodingUrl` beginning with lowercase `/videos/{itemId}/master.m3u8`. Its observed query names were:

```text
DeviceId, MediaSourceId, PlaySessionId, api_key,
VideoCodec, AudioCodec, VideoBitrate, AudioBitrate, AudioStreamIndex,
TranscodingMaxAudioChannels, SegmentContainer, SegmentLength, MinSegments,
BreakOnNonKeyFrames, TranscodeReasons
```

For this source, the URL contained `BreakOnNonKeyFrames=False` and `TranscodeReasons=ContainerNotSupported,DirectPlayError`. The exact URL is in `playback-info-post-hls-profile.json`, with its token redacted.

The exact returned URL was then requested through `/emby` inside the namespace. Both the master manifest and its relative `main.m3u8` child returned HTTP 200 with `Content-Type: application/vnd.apple.mpegurl`. The child response contained:

```text
#EXTM3U
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:3.0000, nodesc
hls1/main/0.ts?PlaySessionId=<play-session-id>
#EXT-X-ENDLIST
```

This establishes a returned relative segment URL shape for this specific 4.9.5.0 request. No segment was downloaded or decoded, no third-party player was exercised, and seeking, timeline accuracy, segment authorization, and output codec correctness remain unverified. Cleanup through `DELETE /Videos/ActiveEncodings?DeviceId=...&PlaySessionId=...` returned HTTP 204 with an empty body.

Evidence: `playback-info-*.json`, `hls-master.json`, `hls-media.json`, and `hls-cleanup.json`.

## Fixture handling and integrity

The recorder keeps originals only in restricted remote `private/raw` files. Repository exports redact the reference password, all observed access tokens, password/token fields, and token query values. Private deployment paths are replaced with placeholders. IDs belong only to this synthetic isolated instance and are intentionally retained; they are not secrets, and retaining them preserves relationships and URL/header semantics.

An initial generic ID-alias experiment was discarded because a numeric item ID could incorrectly replace a `Content-Length` value. Every exported fixture was regenerated from the remote original after that correction. The final remote export audit checked all 65 fixtures for:

1. Exact equality of every response-header name and value with the raw capture.
2. Preservation of object keys, array lengths, JSON types, numbers, booleans, and nulls.
3. String differences restricted to credential redaction and the documented deployment-path replacement.
4. Absence of every recorded raw access token and the actual reference password.

`Content-Length` is the observed length of the original wire body. A redacted JSON or manifest body may have a different serialized length, so fixture consumers must not recompute or rewrite this header from the exported body. `Date`, IDs, and capture timestamps are real test-instance observations; differential tests should explicitly normalize only the nondeterminism appropriate to the comparison.

No raw credentials were copied to the local workspace. All server execution, HTTP capture, package-hash verification, and export-integrity checks ran through `ssh test-env`; no local tests or runtime probes were run.

## Reproduction and remaining work

The supporting scripts are [reference-prepare.sh](../../scripts/test-env/reference-prepare.sh), [reference-start.sh](../../scripts/test-env/reference-start.sh), and [reference-capture.py](../../scripts/test-env/reference-capture.py). Preparation verifies the official package and extracts it without installing or starting the vendor's global service. Startup requires the ownership markers and creates the isolated transient unit. Capture stages are explicit; `setup` refuses to run when a token already exists, and `library` refuses to duplicate nonempty libraries.

Example from PowerShell for an already prepared and running reference:

```powershell
ssh test-env 'pid=$(systemctl show goby-emby-reference -p MainPID --value); nsenter -t "$pid" -n python3 /dev/shm/goby-emby-reference/reference-capture.py public'
ssh test-env 'pid=$(systemctl show goby-emby-reference -p MainPID --value); nsenter -t "$pid" -n python3 /dev/shm/goby-emby-reference/reference-capture.py export'
```

Do not point this recorder at a production Emby instance or run destructive setup/library stages against unrelated data. The reference remains available for further authorized capture, with its process and private data independent of Goby.

Outstanding evidence includes real third-party-client flows, static media range/conditional requests, actual HLS segments and seeking, subtitles, playback reports/resume policy, WebSocket handshake/events, conflicting authentication carriers, music grouping with tagged fixtures, and additional error/permission cases. The current capture is an M0 reference baseline, not a compatibility test pass for Goby.

## M2b local artwork and NFO extension

An additional 24 `artwork-*.json` fixtures were captured on the same isolated Emby 4.9.5.0 instance on 2026-09-08 UTC. They extend the original 65-fixture baseline; all 130 original raw/export files were checked by SHA-256 and remained byte-for-byte unchanged. This section records reference behavior for Goby's metadata and image work, not a Goby compatibility result.

### Source provenance and refresh

The existing synthetic movie was retained without changes. Only three new adjacent files were created:

| File under `/opt/goby-fixtures/movies` | Origin and size | SHA-256 |
| --- | --- | --- |
| `Goby Sample (2026)-poster.jpg` | Self-generated color pattern, 160x240 JPEG, 22361 bytes; encoded with the existing test-env FFmpeg 9.0.1 | `c27e2c603548899acd6de7961b9f4b61e0b0cfce7f9059755bf10aa1221ac561` |
| `Goby Sample (2026)-fanart.png` | Self-generated color pattern, 320x180 PNG, 9358 bytes; generated with Python's standard library | `9109c25b91e8bdf6a4e2f527c86e5f5b3ba4958490b5dfda405d3a08b280afe7` |
| `Goby Sample (2026).nfo` | Synthetic metadata, 624 bytes | `cefe9cca37b0acae912f6b44e466695a8b5649caa8b29a9a54d2066ac997a9eb` |

The original MP4 SHA-256 before and after preparation was `7763ca57b5f9285d3e0036250f7aab418832faa149f9dca7f0ef5545fbb0948a`. No existing media file was replaced or renamed. The reference service continued to see the fixture tree read-only, and its private network namespace prevented online metadata retrieval. No Goby scan was triggered by the recorder.

The filenames follow the official [movie image naming rules](https://emby.media/support/articles/Movie-Naming.html#video-images). The extension also consulted the explicitly versioned [Emby SDK 4.9.5.0 OpenAPI](https://github.com/MediaBrowser/Emby.SDK/blob/4.9.5.0/Resources/OpenApi/openapi_v3.json) and [same-version image guide](https://github.com/MediaBrowser/Emby.SDK/blob/4.9.5.0/Documentation/doc/restapi/Images.html). Only public documentation, API requests, and self-generated inputs were used; no vendor-private implementation was inspected.

After file creation, `POST /Items/18/Refresh` with `Recursive=false`, both refresh modes `FullRefresh`, both replacement flags true, and JSON `{}` returned HTTP 204. This was a targeted reference-item refresh, not a full-library scan. The corresponding evidence is `artwork-refresh.json`.

The NFO contains the following controlled values:

```xml
<movie>
  <title>Reference Artwork Film</title>
  <originaltitle>Reference Original Title</originaltitle>
  <year>2024</year>
  <premiered>2024-01-02</premiered>
  <rating>7.5</rating>
  <mpaa>PG-13</mpaa>
  <genre>Drama</genre>
  <genre>Science Fiction</genre>
  <tag>reference</tag>
  <tag>local-artwork</tag>
  <studio>Reference Studio</studio>
  <uniqueid type="imdb" default="true">tt999999999</uniqueid>
  <uniqueid type="tmdb">999999999</uniqueid>
  <actor><name>Reference Actor</name><role>Lead</role><order>0</order></actor>
  <director>Reference Director</director>
</movie>
```

These provider identifiers are synthetic test values; no provider was contacted. The NFO has no overview.

### NFO projection observations

All four NFO requests were authenticated. List responses remained `{ "Items": [...], "TotalRecordCount": 1 }`; item detail remained a single JSON object.

| Fixture | Request | Observed shape |
| --- | --- | --- |
| `artwork-nfo-default-items.json` | `GET /Users/{userId}/Items?Ids=18` | Only `Name`, `ServerId`, `Id`, `RunTimeTicks`, `IsFolder`, `Type`, `UserData`, `ImageTags`, `BackdropImageTags`, `MediaType`. The new NFO title is visible; optional NFO scalars and collections are absent, not null. |
| `artwork-nfo-detail.json` | `GET /Users/{userId}/Items/18` | 51 properties, including `OriginalTitle`, `ProductionYear`, `PremiereDate`, `CommunityRating`, `OfficialRating`, `Genres`, `ProviderIds`, `People`, `Studios`, `GenreItems`, and `TagItems`. |
| `artwork-nfo-projected-items.json` | List with `Fields=ProviderIds,Genres,Tags,Studios,People` | Adds the requested collections and related `GenreItems`/`TagItems`; the optional NFO scalars remain absent. |
| `artwork-nfo-scalar-fields.json` | List with `Fields=ProductionYear,PremiereDate,OriginalTitle,CommunityRating,OfficialRating,Overview,SortName,DateCreated` | Enables the named nonempty scalars. `Overview` stays absent because this fixture contains no overview. |

The returned NFO values included `OriginalTitle="Reference Original Title"`, `ProductionYear=2024`, `CommunityRating=7.5`, `OfficialRating="PG-13"`, and `ProviderIds={"Imdb":"tt999999999","Tmdb":"999999999"}`. `Genres` was the string array `["Drama","Science Fiction"]`.

There was no `Tags` property. Requesting `Fields=Tags` produced `TagItems`, and the fixture's original tag order was sorted by name:

```json
{
  "TagItems": [
    {"Name": "local-artwork", "Id": 24},
    {"Name": "reference", "Id": 25}
  ],
  "Studios": [
    {"Name": "Reference Studio", "Id": 23}
  ],
  "People": [
    {"Name": "Reference Actor", "Id": "19", "Role": "Lead", "Type": "Actor"},
    {"Name": "Reference Director", "Id": "20", "Type": "Director"}
  ]
}
```

The distinction is material: `TagItems[].Id` and `Studios[].Id` are JSON numbers in these responses, while `People[].Id` is a JSON string. The actor precedes the director. The actor's NFO `<order>0</order>` did not produce an explicit `SortOrder` property. These observations do not establish ordering among multiple actors with different order values.

The reference host's timezone was confirmed read-only as `Asia/Shanghai`, `CST+0800`. For NFO `<premiered>2024-01-02</premiered>`, Emby returned `PremiereDate="2024-01-01T16:00:00Z"`, consistent with a local-midnight interpretation on that host. The timezone was not changed, and this single case does not establish a universal fixed offset for other deployments.

### Artwork discovery and image-list authentication

The refreshed item contained `ImageTags.Primary="66dcea91e5ed256521d919b37c1e6c4d"` and one `BackdropImageTags` entry, `ea3300eb80fb72fba2c9eff80d1a45b4`. These tags appeared in the default list, explicit projections, and detail.

Authenticated `GET /Items/18/Images` returned a top-level array with Primary and Backdrop image records. Both included `ImageType`, `Path`, `Filename`, `Height`, `Width`, and `Size`. Primary omitted `ImageIndex`; Backdrop explicitly had `ImageIndex: 0`. `Size` was `0` for both despite their nonempty files. No image-list `Tag` field was present. Evidence: `artwork-images-list.json`.

The image metadata and binary endpoints had different authentication behavior in this configured instance:

| Request | Valid `X-Emby-Token` | No token | Invalid `X-Emby-Token` |
| --- | --- | --- | --- |
| `GET /Items/18/Images` | 200 JSON array | 401 `text/plain` | 401 `text/plain` |
| `GET /Items/18/Images/Primary` | 200 JPEG | 200 JPEG | 200 JPEG |

The two unauthorized metadata-list responses had the exact body `Access token is invalid or expired.`. All three Primary body responses had the same bytes, dimensions, ETag, and content length. This observation covers image retrieval for the configured synthetic item; it does not cover image mutation, user-profile images, or per-user restricted libraries.

### Image transforms and indexed paths

The following image-content requests used a valid token unless explicitly named otherwise. The recorder decoded image headers on `test-env` to record format and dimensions; it did not run image processing locally.

| Request suffix after `/Items/18/Images/Primary` | Status | Content type | Dimensions | Wire bytes |
| --- | --- | --- | --- | --- |
| No suffix, GET | 200 | `image/jpeg` | 160x240 | 22361 |
| No suffix, HEAD | 200 | `image/jpeg` | No response body | `Content-Length: 22361` |
| `/0` | 200 | `image/jpeg` | 160x240 | 22361 |
| `/1` | 500 | `text/plain` | Not an image | 53 |
| `?Width=64` | 200 | `image/jpeg` | 64x96 | 5897 |
| `?MaxWidth=64` | 200 | `image/jpeg` | 64x96 | 5897 |
| `?MaxWidth=320` | 200 | `image/jpeg` | 160x240 | 22361 |
| `?Format=png` | 200 | `image/png` | 160x240 | 60770 |
| `?Format=jpg&Quality=30` | 200 | `image/jpeg` | 160x240 | 3266 |
| `?Format=jpg&Quality=90` | 200 | `image/jpeg` | 160x240 | 22361 |

`Width=64` and `MaxWidth=64` preserved the portrait aspect ratio and produced identical image bytes. `MaxWidth=320` did not upscale the 160-pixel source. No separate `Width=320` upscaling case was requested. The default and quality-90 responses were byte-identical to the generated source JPEG.

The unavailable Primary index `1` returned `Object reference not set to an instance of an object.`. This is an observed reference error, not evidence that an implementation needs a failing internal dereference.

`GET /Items/18/Images/Backdrop/0?Format=original` returned the original 320x180 PNG, 9358 bytes, with a body hash identical to the generated PNG. Evidence for every transform is in `artwork-primary-*.json` and `artwork-backdrop-original.json`. Captured byte hashes establish provenance; cross-server image checks should separately evaluate dimensions, format, decodability, and cache behavior.

### ETag, conditional requests, and Tag caching

The unmodified Primary GET and HEAD returned the quoted ETag `"66dcea91e5ed256521d919b37c1e6c4d"` with `Cache-Control: public`. Supplying that exact value in `If-None-Match` yielded HTTP 304 for both GET and HEAD, with empty bodies. The observed 304 headers retained `Content-Type: image/jpeg` and `ETag`, and omitted `Content-Length` and `Cache-Control`.

Adding `?Tag=66dcea91e5ed256521d919b37c1e6c4d` kept the same body and ETag but returned `Cache-Control: public, max-age=31536000`, plus `Expires` and `Last-Modified`. Untagged responses did not contain those last two headers. Evidence: `artwork-primary-tagged.json` and `artwork-primary-if-none-match-{get,head}.json`.

ETags varied with transformation requests. In particular, `Width=64` and `MaxWidth=64` had different ETags even though their binary bodies matched. PNG conversion and quality 30 also produced distinct ETags. The capture does not identify or reproduce Emby's ETag algorithm. Conditional requests with weak validators, multiple validators, an incorrect validator, or a validator from another transformation were not tested.

### Extension fixture format and audit

Only the new artwork fixtures use `response.bodyType="binary-base64"` when an actual image body is present; `response.body` then contains the base64 encoding of the exact wire bytes. Their existing optional `observation` property records byte length, SHA-256, format, and dimensions. Empty HEAD/304 bodies remain `bodyType="text"` and `body=""`; JSON and error responses keep their original representations. The original 65 fixtures and their schema were not modified.

The `export_artwork` stage exported only `artwork-*` records. Its remote audit checked exact response-header equality, JSON structure and primitive values, absence of all recorded credentials in exported text and decoded image bytes, and SHA-256 preservation of every original baseline file. Private raw records and credentials remain in the existing restricted reference data directory; only sanitized `artwork-*.json` exports were copied into the repository. Synthetic reference IDs and source-artwork paths are intentionally retained.

The corresponding additional recorder stages are `artwork_prepare`, `artwork_refresh`, `artwork`, `artwork_scalars`, and `export_artwork`. They refuse to overwrite existing artwork evidence. All image generation, HTTP execution, and audit work ran through `ssh test-env`. The reference remained in its original private network namespace, and the existing Goby service was not restarted or scanned by this task.

## Entity navigation and filtering extension

The next extension contains 18 `entity-*.json` fixtures from the same isolated Emby 4.9.5.0 reference. Every request was an authenticated GET using the existing synthetic administrator. No user, media, NFO, library option, or server configuration was created or changed. The original 89 raw captures and 89 exports were hashed before capture and remained byte-for-byte unchanged after the final remote audit.

### Entity lists and identifier types

`GET /Genres`, `/Tags`, `/Studios`, and `/Persons`, each with the existing `UserId`, returned HTTP 200 with `Content-Type: application/json; charset=utf-8`. Each response was an object containing `Items` and numeric `TotalRecordCount`; none had a top-level `StartIndex` field.

| Endpoint | Observed list items | List-item `Id` type | `TotalRecordCount` |
| --- | --- | --- | --- |
| `/Genres` | `BaseItemDto`-shaped objects with `Type: "Genre"`; Drama and Science Fiction | JSON string, `"21"` and `"22"` | 2 |
| `/Tags` | Only `Name` and `Id`; local-artwork and reference | JSON string, `"24"` and `"25"` | 2 |
| `/Studios` | `BaseItemDto`-shaped object with `Type: "Studio"`; Reference Studio | JSON string, `"23"` | 1 |
| `/Persons` | `BaseItemDto`-shaped objects with `Type: "Person"`; Reference Actor and Reference Director | JSON string, `"19"` and `"20"` | 2 |

The actual tag response was:

```json
{
  "Items": [
    {"Name": "local-artwork", "Id": "24"},
    {"Name": "reference", "Id": "25"}
  ],
  "TotalRecordCount": 2
}
```

There was no `Type`, `Count`, `UserData`, or image field on these tag entries. This agrees with the SDK's `UserLibrary.TagItem` shape. It differs from the embedded movie `TagItems` discussed above, whose IDs were JSON numbers. Similarly, `/Genres` and `/Studios` use string IDs while embedded `GenreItems` and `Studios` use numeric IDs. The underlying identifiers remained the same across those representations.

Genre, Studio, and Person list items contained `Name`, `ServerId`, `Id`, `Type`, `UserData`, `ImageTags`, and `BackdropImageTags`. They did not contain `IsFolder`, `MediaType`, `ChildCount`, or per-entity media-count fields in the recorded default projection. The two Genre items had Primary image tags, while the Studio and Person entries had empty `ImageTags` objects. The capture did not request count-enabling fields.

Evidence: `entity-list-{genres,tags,studios,persons}.json`. The SDK snapshot declares `QueryResult<BaseItemDto>` for Genres, Studios, and Persons, and `QueryResult<UserLibrary.TagItem>` for Tags; the response shape above was also observed directly.

### Navigation by name and ID

The following requests all returned HTTP 200 with a single JSON object, not an `Items` envelope:

| Request, with the existing `UserId` where applicable | Observed object |
| --- | --- |
| `/Genres/Drama?UserId=...` | Genre detail, `Id: "21"`, `Type: "Genre"`, 24 properties |
| `/Studios/Reference%20Studio?UserId=...` | Studio detail, `Id: "23"`, `Type: "Studio"`, 22 properties |
| `/Persons/Reference%20Actor?UserId=...` | Person detail, `Id: "19"`, `Type: "Person"`, 22 properties |
| `/Users/{userId}/Items/21` | Genre detail with the same property set as the name route |
| `/Users/{userId}/Items/19` | Person detail with 23 properties; it additionally included `TagItems: []` compared with the name route |

The details included `Etag`, dates, `SortName`, `ForcedSortName`, `ExternalUrls`, `ProviderIds`, `UserData`, display preferences, image fields, and lock fields where shown in the fixtures. `CanDelete` and `CanDownload` were explicitly false. They did not include `IsFolder`, `MediaType`, or count fields. A standalone Person has `Type: "Person"`; the actor/director relationship types belong to a media item's `People` entries.

Evidence: `entity-name-{genres,studios,persons}.json` and `entity-item-detail-{genre,person}.json`. This confirms that the sampled embedded numeric Genre ID can be rendered as a path string and navigated through the ordinary user-item detail route. Direct Tag-ID and Studio-ID detail requests were not part of this bounded capture.

### Item filters

All item-filter requests used `/Items?UserId=...&Recursive=true`. Their HTTP 200 responses used the ordinary `Items`/`TotalRecordCount` envelope. Every positive result below contained only the existing movie `Id: "18"`, `Type: "Movie"`, `Name: "Reference Artwork Film"`.

| Additional query | Result | Fixture |
| --- | --- | --- |
| `GenreIds=21` | One matching movie | `entity-filter-genreids.json` |
| `TagIds=25` | One matching movie | `entity-filter-tagids.json` |
| `StudioIds=23` | One matching movie | `entity-filter-studioids.json` |
| `PersonIds=19` | One matching movie | `entity-filter-personids.json` |
| `Genres=Drama|Science Fiction` | One matching movie | `entity-filter-genres-pipe.json` |
| `Tags=reference|local-artwork` | One matching movie | `entity-filter-tags-pipe.json` |
| `Genres=Drama|Missing Reference Genre` and `Tags=reference|Missing Reference Tag` | One matching movie despite a nonexistent alternative in each field | `entity-filter-mixed-positive-missing.json` |
| `Genres=Drama` and `Tags=Missing Reference Tag` | `Items: []`, `TotalRecordCount: 0` | `entity-filter-negative-tag.json` |

Pipe characters and spaces were URL-encoded by the recorder; the table shows decoded query values. The mixed positive/missing case is consistent with OR across alternatives inside each of the sampled name filters. The matching-genre/missing-tag case supports AND between those active filters and demonstrates that the negative tag was not simply ignored. Both observations apply to these named cases; they do not establish every combined-filter rule or multi-ID separator.

The SDK explicitly documents pipe-separated `Genres`, `Tags`, and `StudioIds`. It does not explicitly describe OR/AND behavior. `GenreIds` and `TagIds` are incompletely exposed in the snapshot's GET parameter lists, so their successful GET behavior here is direct reference evidence rather than a deduction from those lists. Only one ID per ID-filter request was tested.

### Paging and media-type parameter

`GET /Genres?UserId=...&IncludeItemTypes=Movie&StartIndex=1&Limit=1` returned one Genre item, Science Fiction (`Id: "22"`), while `TotalRecordCount` remained 2. This confirms a zero-based starting offset and a count before paging for that request. It also records a successful Movie-filtered request, but this dataset does not independently establish exclusion behavior for another `IncludeItemTypes` value. Evidence: `entity-genres-movie-page.json`.

### Integrity and scope

The added stages are `entities_prepare`, `entity_lists`, `entity_navigation`, and `export_entities`. The recorder refuses to overwrite existing `entity-*` evidence. The final export selected only that prefix and remotely checked original response headers, JSON structure and number/string distinctions, and absence of the recorded password and tokens. SHA-256 checks preserved all 178 previous raw/export files. All 18 new fixtures retain the established JSON fixture structure and original synthetic IDs.

No local runtime verification was performed. The reference stayed in its existing private network namespace, and no media scan or metadata refresh was requested. No Goby service or database operation was performed by this capture. Unknown entity names/IDs, Tag detail navigation, multiple numeric IDs, entity deletion, count fields, favorites, permission-restricted users, and further paging combinations remain outside this evidence set.

## M3 playback negotiation, transfer, and reporting extension

This extension adds 42 `playback-m3-*.json` fixtures, including the two explicitly authorized controlled negotiation follow-ups. All server execution and audit work ran on `test-env` inside the existing private network namespace. The original 107 raw captures and 107 exports retained their SHA-256 hashes. Existing media, NFO files, libraries, and users were not used for playback-state mutations; reports and manual user-data changes used a newly created dedicated account and media item.

### Dedicated media and account

A new owned directory, `/opt/goby-fixtures/playback-reference`, contains a synthetic 600-second black H.264/AAC MP4 and a same-basename external SRT. The video was generated without real-time pacing using the existing FFmpeg 9.0.1 installation, at 160x90 and one frame per second, with mono 8 kHz audio. No package or codec dependency was installed.

| File | Size | SHA-256 |
| --- | --- | --- |
| `Reference Playback M3.mp4` | 56379 bytes | `997af268405a91e01685afa52d70a892c767e0e7135d25f6a33087cfb72de1c3` |
| `Reference Playback M3.srt` | 121 bytes | `d801087b005e02b1fac35140024ebfb0e2e9ce4ab0578dd242721e15f8676797` |

The directory has its own `.goby-managed` marker. An independent `Reference Playback M3` movies library was added with online fetchers, realtime monitoring, and local metadata writes disabled. Only that new library received a targeted recursive refresh. The existing network sandbox independently prevented external access. The actual returned library defaults were `MinResumePct=2`, `MaxResumePct=90`, and `MinResumeDurationSeconds=120`; the recorder did not override them.

The administrator created the dedicated `reference-playback-m3` account using `POST /Users/New`, then set its password with `POST /Users/{Id}/Password` and JSON `Id`, `NewPw`, `ResetPassword=false`. User creation returned 200; the password update returned 204; normal username/password authentication returned 200. The new account uses device ID `goby-playback-m3-recorder`, separate from the administrator's recorder device.

Its credentials are kept in `private/playback-m3-credentials.env`, mode `0600 root:root`; the original administrator credential file remains separate. The sanitizer loads both credential sets and redacts `NewPw` as well as other password/token fields. No credentials were printed or copied to the workspace.

The new library item was `Id: "28"`, its media source was `Id: "mediasource_28"`, and its returned `RunTimeTicks` was exactly `6000000000`. At the final checkpoint the root filesystem still had approximately 373 MiB free and tmpfs approximately 1.6 GiB free; the reference service remained active with `PrivateNetwork=yes`.

### PlaybackInfo defaults and controlled capability cases

The short-file negotiation cases used the existing two-second movie `Id: "18"` and its current source ID `mediasource_18`. These calls did not send playback reports for the existing account. The matching profile declared MP4, H.264, and AAC direct playback, an H.264/AAC HLS transcoding profile, external SRT/WebVTT subtitles, and `MaxStreamingBitrate=200000000`. The mismatch profile differed only by declaring HEVC instead of H.264 in its direct-play video codec field.

Every sampled request returned HTTP 200 with `MediaSources` and `PlaySessionId`. No sampled response contained `ErrorCode`, including the all-disabled and mismatch-with-transcoding-disabled cases.

| Request/body variation | SupportsDirectPlay | SupportsDirectStream | SupportsTranscoding | Returned URLs |
| --- | --- | --- | --- | --- |
| GET with required `UserId`, no profile | true | true | true | Neither URL present |
| Minimal POST with `UserId` | true | true | true | Neither URL present |
| Matching profile, enable flags omitted | true | true | true | `DirectStreamUrl` points to `original.mp4` |
| HEVC mismatch profile, flags and `IsPlayback` omitted | false | false | true | Both `DirectStreamUrl` and `TranscodingUrl` point to the same HLS master |
| Matching profile; direct play false, direct stream true, transcoding false | false | true | false | `DirectStreamUrl` points to `original.mp4` |
| Matching profile; all three enable flags false | false | false | false | Neither URL present; source still returned |
| HEVC mismatch; `EnableTranscoding=false`, `IsPlayback=true` | true | true | false | `DirectStreamUrl` points to `original.mp4` |
| Same mismatch; `EnableTranscoding=false`, `IsPlayback=false` | true | true | false | Same original-file URL shape |
| Same mismatch; `EnableTranscoding=true`, `IsPlayback=true` | false | false | true | Both URLs point to the same HLS master |

The last two requests isolate the two changed booleans against the earlier cases. In this tested profile, the original-file fallback follows `EnableTranscoding=false`, rather than the `IsPlayback` switch. This is an observation of the server's returned flags, not evidence that a HEVC-only decoder can play H.264. Complex profile conditions, wildcards, device policies, HDR, and unsupported source types were not exercised.

The original-file URL has this exact shape:

```text
/videos/{itemId}/original.mp4?DeviceId=...&MediaSourceId=...&PlaySessionId=...&api_key=...
```

Its query consists only of `DeviceId`, `MediaSourceId`, `PlaySessionId`, and `api_key`; it has no `Static`, `Container`, or `UserId` parameter. The mismatch HLS URLs additionally contained the encoding/segment parameters recorded in the fixtures and `TranscodeReasons=VideoCodecNotSupported`. In that case `DirectStreamUrl` was populated even though `SupportsDirectStream=false`.

Evidence: all `playback-m3-short-*.json` files. The observed source-ID strings are retained as identifiers; this sample does not establish a universal source-ID construction rule.

### Long-source streams and external subtitle descriptor

The dedicated account's matching-profile POST returned the original MP4 URL and all three support flags true. Its video stream had `Codec: "h264"`, `Profile: "Constrained Baseline"`, `Level: 10`, and `VideoRange: "SDR"`. Audio was stream index 1, codec AAC, profile LC, one channel, 8000 Hz. `DefaultAudioStreamIndex` was 1; `DefaultSubtitleStreamIndex` was absent.

The external subtitle was returned as stream index 2 with:

```json
{
  "Codec": "srt",
  "Type": "Subtitle",
  "Index": 2,
  "IsExternal": true,
  "IsTextSubtitleStream": true,
  "SupportsExternalStream": true,
  "DeliveryMethod": "External",
  "DeliveryUrl": "/Videos/28/mediasource_28/Subtitles/2/0/Stream.srt?api_key=[REDACTED_TOKEN]",
  "Protocol": "File"
}
```

The full descriptor also includes its synthetic path and false/default stream flags. This extension captured the descriptor, not a subtitle download or timing test. Evidence: `playback-m3-long-item.json` and `playback-m3-long-info.json`.

### Static media delivery, authentication, and aliases

The main stream URL was `/emby/Videos/28/stream?Static=true&MediaSourceId=mediasource_28&PlaySessionId=...`. No `Container` query was required for this tested static request. All authenticated requests used the dedicated user; unauthenticated/invalid-token cases retained the same source and play-session identifiers.

| Request | Status | Recorded result |
| --- | --- | --- |
| Authenticated full GET | 200 | `video/mp4`, 56379 bytes; body SHA-256 matches the synthetic source |
| Authenticated HEAD | 200 | Same `Content-Type` and `Content-Length`, empty body |
| `Range: bytes=0-31` | 206 | 32 bytes; `Content-Range: bytes 0-31/56379` |
| `Range: bytes=-32` | 206 | **Reference deviation:** 33 leading bytes; `Content-Range: bytes 0-32/56379`, not the final 32 bytes |
| `Range: bytes=9999999-` | 416 | `text/plain`, 94-byte exception message; no `Content-Range` header |
| No authentication token, with first-byte range | 401 | `Access token is invalid or expired.` |
| Invalid `X-Emby-Token`, with first-byte range | 401 | Same error, despite the retained negotiated `PlaySessionId` |
| Root `/Videos/28/stream`, valid token and range | 206 | Same first 32 bytes |
| Lowercase `/emby/videos/28/stream`, valid token and range | 206 | Same first 32 bytes |
| Exact generated `/emby/videos/28/original.mp4?...&api_key=...`, no added auth header or Static parameter | 206 | Same first 32 bytes, using the generated query token |
| `Range: bytes=0-31` plus matching `If-Range` ETag from HEAD | 206 | Same first 32 bytes |

Successful static responses included `Accept-Ranges: bytes`, `Cache-Control: private, no-transform`, and a quoted ETag. Matching `If-Range` used that exact ETag. Mismatched validators, conditional 304, multipart ranges, and empty files were not tested. The suffix-range result is preserved as an upstream deviation; it must not be described as successful standard suffix-range handling.

The out-of-range body was:

```text
Exception of type 'MediaBrowser.Common.Extensions.RangeRequestOutOfRangeException' was thrown.
```

Only one full file body was fetched. Successful additional media GETs used small ranges. A remote audit decoded all seven media-body fixtures and confirmed that each byte sequence matched the source slice declared by its original response `Content-Range`, including the anomalous leading-byte suffix result. It also checked each recorded `Content-Length`. Evidence: `playback-m3-media-*.json`.

### Dedicated-user playback and user-data observations

The report chain used the dedicated account's session ID, the negotiated play-session ID, item/source IDs, `PlayMethod: "DirectStream"`, and a source duration of 600 seconds. Started reported position zero. Progress reported `EventName: "TimeUpdate"` and position `1200000000` ticks. No real-time 120-second wait or media decoding was performed; this tests client-report handling.

| Observation point | Position ticks | PlayCount | Played | Other observed fields |
| --- | --- | --- | --- | --- |
| Detail immediately after Started | 0 | 1 | false | `LastPlayedDate` present; `PlayedPercentage` absent |
| Detail after Progress at 120 seconds | 1200000000 | 1 | false | `PlayedPercentage: 20`, `LastPlayedDate` present |
| Resume list immediately after that Progress | 1200000000 | **0** | false | One returned item; `PlayedPercentage: 20`; `LastPlayedDate` absent |
| Detail after Stopped at 120 seconds | 1200000000 | 1 | false | Percentage 20 and date present |
| Detail after an exact duplicate Stopped report | 1200000000 | 1 | false | Same captured values as after the first stop |

Started, Progress, Stopped, and duplicate Stopped all returned HTTP 204 with empty bodies. The Resume result used the normal `Items`/`TotalRecordCount` envelope and `TotalRecordCount: 1`.

The Resume list's zero count and omitted date differed from the adjacent item-detail response. This trace does not determine whether that difference comes from projection, caching, or persistence timing. It is not evidence that the stored play count was reset: later details still showed count 1. The requests occurred within the same second, so the repeated `LastPlayedDate` value does not establish its update cadence. The duplicate-stop observation covers one exact duplicate for the same owned session, not every idempotence or out-of-order case.

Manual changes were then applied only to this account and item:

| Request | Status | Returned `UserItemDataDto` state |
| --- | --- | --- |
| `POST /Users/{userId}/PlayedItems/28` | 200 | Position 0, count 1, played true, favorite false, date present |
| `DELETE /Users/{userId}/PlayedItems/28` | 200 | Position 0, count 0, played false, favorite false; date omitted |
| `POST /Users/{userId}/FavoriteItems/28` | 200 | Position 0, count 0, played false, favorite true |
| `DELETE /Users/{userId}/FavoriteItems/28` | 200 | Position 0, count 0, played false, favorite false |

The final detail agreed with the final zero-count, unplayed, nonfavorite state and omitted `LastPlayedDate`. These mutation responses were standalone user-data objects, not item details or collection envelopes. The first manual played operation preserved the existing count of 1; repeat manual operations were not tested.

Evidence: `playback-m3-started.json`, `playback-m3-progress-120.json`, `playback-m3-stopped-*.json`, `playback-m3-detail-*.json`, `playback-m3-resume-after-progress.json`, `playback-m3-mark-*.json`, and `playback-m3-favorite-*.json`.

### M3 integrity and remaining boundaries

The added stages are `playback_m3_begin`, `playback_m3_flags`, `playback_m3_rejection`, `playback_m3_factorial`, `playback_m3_prepare`, `playback_m3_setup`, `playback_m3_long_info`, `playback_m3_transport`, `playback_m3_reports`, and `export_playback_m3`. Existing named evidence cannot be overwritten. Export selects only the `playback-m3-` prefix, enforces the authorized 42-fixture bound, and preserves all prior raw/export hashes.

Media bodies use the established `binary-base64` representation; original headers and JSON types remain intact. The final remote audit checked credentials from both accounts, decoded media bodies, source/range correspondence, header values, and the 214 original baseline hashes. Both credential files remained mode 0600. No local build, test, or runtime probe was performed for this capture, and the Goby service/database was not operated by it.

Real player interoperability, unknown or omitted play-session IDs, duplicate Started, late Progress after Stop, completion-threshold boundaries, pause/rate extrapolation, multiple concurrent playback sessions, denied user policies, nondefault audio selection, actual subtitle delivery, and transcoded media correctness remain unverified. These fixtures establish reference responses for the documented inputs; they do not claim Goby playback compatibility.

## Watched-state propagation from a series

Four additional `folder-state-*.json` fixtures use only the dedicated M3 account against the existing synthetic `Example Series`. Its current library access already permitted the requests, so no policy change was needed. The sequence marked the series played, read its episodes, marked the series unplayed again, and read its episodes to confirm restoration. The original administrator and other users were not targeted.

Both `POST /Users/{userId}/PlayedItems/{seriesId}` and the corresponding DELETE returned HTTP 200 with a standalone user-data object. After POST, that object had `Played: true`, `UnplayedItemCount: 0`, and `PlayCount: 0`. `GET /Shows/{seriesId}/Episodes?UserId=...` then returned all three episodes across two seasons with `UserData.Played: true`. After DELETE, the series response had `Played: false` and `UnplayedItemCount: 3`; all three episodes subsequently had `Played: false`.

This confirms that the sampled series operation affected descendant episodes rather than only the parent series. The default episode-list projection showed zero `PlayCount` and omitted dates in both reads; no episode-detail count or date inference should be made from those fields. Season-node details and partially restricted library policies were not sampled.

The four requests are `folder-state-series-played.json`, `folder-state-episodes-after-played.json`, `folder-state-series-unplayed.json`, and `folder-state-episodes-after-unplayed.json`. The dedicated user's series and episodes were restored to unplayed through the ordinary API. No media, NFO, vendor code, or server configuration changed. The remote export audit preserved all 298 previous raw/export files by SHA-256, kept original headers and JSON types, and removed credentials. The supporting stages are `folder_state_begin`, `folder_state`, and `export_folder_state`; the extension is limited to these four fixtures.

## M3b session listing, capabilities, and Ping

This extension adds 26 `session-m3b-*.json` fixtures. A new ordinary account, `reference-session-m3b`, and device ID `goby-session-m3b-recorder` isolate capability updates and later series-state tests from previous users. The account was created and assigned a random password through ordinary administrator APIs. Its separate `private/session-m3b-credentials.env` file uses mode 0600; the sanitizer includes this credential set alongside the earlier accounts. No existing user's capability state was targeted.

### Session response shape and visibility

Login returned HTTP 200 with a `SessionInfo` object. `GET /Sessions` returned a top-level JSON array: the administrator saw three existing user/device sessions, while the new ordinary account saw only its own session in this configuration. A matching `DeviceId` or `Id` returned a one-element array; a nonexistent `Id` returned `[]`, all with HTTP 200.

The sampled default session properties were `PlayState`, `AdditionalUsers`, `RemoteEndPoint`, `Protocol`, `PlayableMediaTypes`, `PlaylistIndex`, `PlaylistLength`, `Id`, `ServerId`, `UserId`, `UserName`, `Client`, `LastActivityDate`, `DeviceName`, `InternalDeviceId`, `DeviceId`, `ApplicationVersion`, `SupportedCommands`, and `SupportsRemoteControl`. `InternalDeviceId` was a JSON number; the public session/user/server IDs were strings. `IsActive`, `Capabilities`, `DeviceProfile`, `SupportsSync`, and `NowPlayingItem` were absent in this idle HTTP-only session.

Default `PlayableMediaTypes` and `SupportedCommands` were empty arrays, and `SupportsRemoteControl` was false. The default `PlayState` included explicit false `CanSeek`, `IsPaused`, and `IsMuted`, `RepeatMode: "RepeatNone"`, `SleepTimerMode: "None"`, `SubtitleOffset: 0`, `Shuffle: false`, and `PlaybackRate: 1`; it did not invent a current item or playback position.

`ActiveWithinSeconds=0` and `ActiveWithinSeconds=3600` both returned the same single recently active ordinary session. These requests establish acceptance but do not determine whether that query is filtered, ignored, or treats zero specially. The pinned specification lists `DeviceId`, `Id`, and `ControllableByUserId`, but does not expose `ActiveWithinSeconds` in this operation's parameter list. No stale-session timing experiment was performed.

Evidence: `session-m3b-user-login.json` and `session-m3b-list-*.json`. These visibility results cover the sampled accounts and their default permissions; they do not establish every remote-control policy combination.

### Capability updates and current-session binding

All recorded Simple and Full capability POSTs returned HTTP 204 with empty bodies. The following GETs used the dedicated session's known ID:

| Capability request | Fields visible in the subsequent session |
| --- | --- |
| Simple form with only query `Id` | Empty `PlayableMediaTypes` and `SupportedCommands` |
| Simple form with comma-separated `Audio,Video`, commands `PlayMediaSource,SetVolume`, `SupportsMediaControl=true`, `SupportsSync=false` | Arrays reflected those media types and commands in supplied order |
| Full form with query `Id` and body `{}` | Both arrays cleared again |
| Full populated JSON with `Video,Audio`, four commands, media control enabled, and a `DeviceProfile` | Media-type and command arrays reflected the JSON input |
| Simple populated form with query `Id` omitted | The current dedicated session changed to `Audio` and `VolumeUp` |
| Full form with a nonexistent query `Id` | The current dedicated session changed to `Audio` and `VolumeDown` |

The missing/unknown ID observations show that these requests updated the authenticated device's current session rather than rejecting the supplied target. They do not prove how a valid ID belonging to a different existing session would be treated: no such session was targeted. The `Id` query must not be mistaken for proof of authorization to mutate another device.

`SupportsRemoteControl` remained false even when `SupportsMediaControl=true` was sent. The test session used HTTP requests without a WebSocket connection, and no remote command delivery was attempted. `DeviceProfile`, `Capabilities`, and `SupportsSync` were not exposed by the sampled session GETs, so their absence is not evidence that the uploaded values were rejected or not stored.

For a bounded profile control, Full capabilities uploaded a HEVC-only direct-play profile against the known H.264 source. A subsequent minimal `POST /Items/28/PlaybackInfo` supplied only `UserId`, with no request `DeviceProfile`. It returned the generic three support flags true and no direct/transcoding URL, matching the earlier profile-less behavior. Thus this request did not automatically apply the uploaded mismatch profile. The capture does not establish other uses of a stored profile.

Evidence: `session-m3b-caps-*.json`, `session-m3b-after-*.json`, and `session-m3b-playbackinfo-after-profile.json`.

### Playback Ping

All Ping requests were authenticated as the new ordinary account:

| `POST /Sessions/Playing/Ping` query | Response |
| --- | --- |
| `PlaySessionId` omitted | HTTP 400, `text/plain`, `Value cannot be null. (Parameter 'key')` |
| Syntactically valid but unknown ID | HTTP 204, empty body |
| ID issued by this account's preceding PlaybackInfo request | HTTP 204, empty body |

The known ID was negotiated but had not received a Started report. These observations establish response behavior, not renewal duration, worker liveness, active-playback validation, or cross-user ownership rules. Evidence: `session-m3b-ping-*.json`.

## M3b NextUp controls with the existing short episodes

The 33 `nextup-m3b-*.json` fixtures use the fresh ordinary account and the existing three two-second episodes in `Example Series` (`SeriesId: "12"`). State changes targeted only that account. Every control sequence ended by marking the whole series unplayed again; the restoration responses reported `UnplayedItemCount: 3` and `Played: false`.

The directional result is established: after a played episode, specifying `SeriesId` can return all later unplayed episodes and skip earlier gaps. The global default result remains unresolved in this synthetic setup; it must not be generalized into a rule that global NextUp is always empty.

### Manual-state and parameter controls

All NextUp reads returned HTTP 200 with `{ "Items": [...], "TotalRecordCount": ... }`. Returned nonempty entries were `Type: "Episode"` with the same series ID, not Series DTOs.

| State and request | Observed result |
| --- | --- |
| Fresh account, all three episodes unplayed, default query | Empty |
| Same state, `SeriesId=12` | Empty |
| All unplayed, only TV-library `ParentId=5` | Empty |
| All unplayed, `SeriesId=12&ParentId=5` | Empty |
| Only S01E02 played, followed by a two-second wait, default query | Empty |
| Same state, only `ParentId=5` | Empty |
| Same state, only `SeriesId=12` | S02E01 only, skipping the still-unplayed S01E01 gap |
| Same state, `SeriesId=12&ParentId=5` | S02E01 only |
| S01E02 restored, only S01E01 played, `SeriesId=12&ParentId=5` without additional flags | S01E02 and S02E01, both from Series 12 |
| Only S01E01 played, default query after a 30-second wait | Empty |
| All three episodes played, default query | Empty |
| All played, `SeriesId=12&EnableRewatching=true` | Empty |

The earlier combined request with `SeriesId`, `ParentId`, `EnableResumable=true`, and `EnableRewatching=false` also returned S01E02 and S02E01. The later no-extra-flags control shows that those extra booleans were not necessary to obtain the two-item result in this state. It does not establish their independent effects in resumable or rewatch scenarios.

The two-second clips are unsuitable for a 120-second partial-playback experiment, so none was fabricated. The first twelve NextUp fixtures were captured before capability updates; the additive controls used the same dedicated HTTP session after the capability sequence, whose recorded final playable-media array was `Audio`. The tests did not independently determine whether session capabilities or other setup factors affect global NextUp selection.

Evidence: the initial `nextup-m3b-*.json` sequence and `nextup-m3b-split-*.json`. The additive controls preserved the first twelve fixtures unchanged and did not reuse their filenames.

### Playback-event control

A final nine-request control replaced manual first-episode marking with `PlaybackInfo` followed by Started at zero and Stopped at the source's actual two-second duration. The subsequent episode detail confirmed `Played: true`, `PlayCount: 1`, and a present `LastPlayedDate`.

After a two-second wait, default NextUp and ParentId-only NextUp still returned empty results. `SeriesId=12` returned both S01E02 and S02E01. Series detail showed `UnplayedItemCount: 2`, `PlayCount: 0`, and `Played: false`, with no `LastPlayedDate` in that DTO. Restoration returned the series and descendants to unplayed.

This control did not make the global query positive, so the observed difference cannot be attributed solely to using manual watched marking rather than playback events. No internal database or vendor implementation was inspected to explain it. Evidence: `nextup-m3b-playback-*.json`.

### NextUp evidence boundaries and integrity

The history-cursor observation applies to the specified-series requests in the tested ordered series. It is evidence against always selecting the earliest unplayed gap and against limiting this specified-series response to one item. The global positive-selection rule, rewatch behavior, partially resumed episodes, multiple active series, missing episode numbering, and permission-restricted series remain unverified.

The session and short-NextUp export audited all 26 session fixtures and 33 NextUp fixtures. It preserved the original 306 raw/export files, the 382 files preceding the split-parameter controls, and the 406 files preceding the playback-event controls. Headers, JSON primitive types, and credentials were checked remotely. No prior account, media file, NFO, or library configuration was altered by these controls.

## Independent long-duration NextUp control

The final duration control adds 12 `nextup-long-*.json` fixtures. It uses a new owned `/opt/goby-fixtures/nextup-long-reference` directory and a new `Reference NextUp Long` TV library, leaving the older TV library and files unchanged. Three copies of the existing 600-second synthetic MP4 total 169137 bytes; each copy retains SHA-256 `997af268405a91e01685afa52d70a892c767e0e7135d25f6a33087cfb72de1c3`. No new encoding or dependency installation was needed.

The new library reused the older test TV library's returned options, changing its source paths only. Provider access remained disabled, and only this new library received a targeted refresh. The `reference-session-m3b` account was reused without a policy change.

The indexed series was `Id: "31"`, with S01E01 `Id: "35"`, S01E02 `Id: "34"`, and S02E01 `Id: "36"`. All three episodes returned `RunTimeTicks: 6000000000`. IDs are not episode-order keys; the selection used season and episode numbers.

| Long-series state and query | Observed result |
| --- | --- |
| Unstarted, global default | Empty |
| Unstarted, `SeriesId=31` | Empty |
| Only S01E01 marked played, followed by a two-second wait, global default | Empty |
| Same state, only the new library's ParentId | Empty |
| Same state, `SeriesId=31` | S01E02 and S02E01, both 600-second episodes |

Series detail showed two unplayed items. The final DELETE restored all three episodes to unplayed, and the series response reported `UnplayedItemCount: 3`. Because the global query was still empty, the optional middle-episode branch was not executed and no further request expansion was performed.

Increasing duration did not make the global query positive in this new-library control. The global selection behavior therefore remains uncertain; duration alone is not an established explanation for the earlier empty results. The new library, episode files, and existing capability context are recorded so that this limited comparison is not presented as a complete causal model or general client compatibility proof.

The long-duration export checked all 12 new fixtures and preserved every one of the 424 pre-existing raw/export files by SHA-256, including all 33 prior NextUp and 26 session records. Only sanitized exports were copied to the repository. This completes the bounded control; it did not modify Goby, vendor code, older media, or older library settings.

## External-subtitle follow-up requirements from existing evidence

The session work did not add subtitle requests or implement subtitle delivery. Existing evidence already returns a `/Videos/{item}/{source}/Subtitles/{index}/0/Stream.srt?api_key=...` URL, so a backend emitting that shape must support the path-offset form, including zero. The SDK also declares GET/HEAD and both `/Videos` and `/Items` aliases. A subtitle index is the index in all `MediaStreams` (2 in this source), not its position in a subtitle-only array.

The remaining concrete delivery gaps are authentication/error responses, GET/HEAD content type and length, alias behavior, SRT-to-WebVTT conversion and encoding, Start/End filtering versus clipping or rebasing, boundary-spanning cues, `CopyTimestamps` omitted/true/false behavior, and path/query precedence. Start and End use int64 ticks; the general unit is 10000000 ticks per second. The separate playback-report `SubtitleOffset` field still lacks a confirmed unit and must not be equated with a subtitle download's start position. No unsupported timing behavior is established by the descriptor alone.

## NextUp capability declaration control

Eight `nextup-capability-*.json` fixtures isolate the explicitly observed Audio-only session declaration from the preceding long-series comparison. A Simple capabilities POST declared `PlayableMediaTypes=Video,Audio`, an empty `SupportedCommands` value, `SupportsMediaControl=true`, and `SupportsSync=false`. No `DeviceProfile` property was supplied. The following session GET confirmed `PlayableMediaTypes: ["Video","Audio"]` and `SupportedCommands: []`; `SupportsRemoteControl` remained false.

Only the long series' first episode was then marked played for the same dedicated account. After a two-second wait, global NextUp and ParentId-only NextUp still returned empty query-result objects. Specifying `SeriesId=31` returned the same two later episodes, S01E02 (`Id: "34"`) and S02E01 (`Id: "36"`). The Video declaration therefore did not make the global query positive in this controlled case, and an Audio-only declaration is not an established explanation for the prior empty result.

The final series DELETE returned `UnplayedItemCount: 3` and `Played: false`. A final Simple capabilities POST restored `Audio` and `VolumeDown` and returned HTTP 204, without supplying a profile. No additional case was attempted after this result.

The export audited these eight records remotely and preserved all 448 preceding raw/export files, covering the original 224 HTTP fixtures. The supporting stages are `nextup_capability_begin`, `nextup_capability`, and `export_nextup_capability`. Separately, the long-fixture generator now explicitly applies mode 0755 to its newly created root and season directories after creation, so the recorder's restrictive umask does not prevent the unprivileged Goby service from reading those synthetic fixtures. Image/media contents and their hashes are unchanged; files retain mode 0644.

## M3c external SRT delivery and WebVTT conversion

This extension adds 36 `subtitle-m3c-*.json` HTTP fixtures against the same official 4.9.5.0 server in its private network namespace. It resolves the principal delivery and timing questions listed above for one external UTF-8 SRT source. It does not establish support for every subtitle format, character encoding, embedded stream, or client.

### Owned source and scope

A new owned directory, `/opt/goby-fixtures/subtitle-reference`, contains one copy of the existing 600-second synthetic MP4 and a newly generated ten-cue English SRT. The root and movie directory explicitly use mode 0755; the ownership marker and two fixture files use 0644. No old media bytes, NFO, user watch state, library options, or Goby process changed. The separate `Reference Subtitle M3c` library reused the provider-disabled movie options and received only a targeted refresh. Requests after library setup used the existing dedicated `reference-session-m3b` account, with no policy change.

| Input | Size and provenance |
| --- | --- |
| Copied H.264/AAC MP4 | 56379 bytes; SHA-256 `997af268405a91e01685afa52d70a892c767e0e7135d25f6a33087cfb72de1c3`; runtime 6000000000 ticks |
| `Reference Subtitle M3c (2026).en.srt` | 617 bytes; SHA-256 `f559a4e972dc1332690a34d6afbb5687bb6aa14d9244460c9e7fc82bb9546b76`; UTF-8 without BOM; 41 CRLF sequences |

The source cue intervals, in seconds, are `0-5`, `8-10`, `9-12`, `10-11`, `12.5-15.25`, `18-22`, `19.5-20`, `20-21`, `22-25`, and `30-33`. The `9-12` cue has two lines, non-ASCII text, an `<i>` span, and literal `&amp;`; the `12.5-15.25` cue has a `<b>` span. These inputs make equality at both window boundaries and cues crossing either boundary observable without modifying previous sources.

### Descriptor and profile negotiation

The new movie is item `"40"`, source `"mediasource_40"`. Its external subtitle has numeric `Index: 2` in the complete MediaStreams array, `Codec: "srt"`, `Language: "en"`, `Type: "Subtitle"`, `IsExternal: true`, `IsTextSubtitleStream: true`, and `SupportsExternalStream: true`. The item query included the source path but did not include a delivery URL. PlaybackInfo added `DeliveryMethod: "External"`, `DeliveryUrl`, `IsExternalUrl: false`, and `IsChunkedResponse: false`.

Both PlaybackInfo POSTs supplied the matching MP4/H.264/AAC direct-play profile and `IsPlayback: true`. With SRT and VTT external subtitle support, the returned URL was `/Videos/40/mediasource_40/Subtitles/2/0/Stream.srt?api_key=...`. With only `{ "Format": "vtt", "Method": "External" }` and explicit `SubtitleStreamIndex: 2`, the URL changed to `/Videos/40/mediasource_40/Subtitles/2/0/Stream.vtt?api_key=...`, while the stream's `Codec` remained `srt`. The response describes the stored stream codec separately from the requested delivery format.

Both responses kept `SupportsDirectPlay`, `SupportsDirectStream`, and `SupportsTranscoding` true and supplied the original MP4 direct-stream URL. Neither response included `DefaultSubtitleStreamIndex`, including the explicit-index request. Absence must not be converted into an observed `-1` or `2` value. Evidence: `subtitle-m3c-item.json`, `subtitle-m3c-playbackinfo.json`, and `subtitle-m3c-playbackinfo-vtt-only.json`.

### Routes, authentication, and response bytes

The generated URL was resolved under the `/emby` API base, retaining its query token. It returned HTTP 200 for GET and HEAD. The following additional GET forms also returned 200 with the same original SRT bytes:

- `/emby/Videos/40/mediasource_40/Subtitles/2/Stream.srt`
- `/emby/Items/40/mediasource_40/Subtitles/2/Stream.srt`
- `/emby/Items/40/mediasource_40/Subtitles/2/0/Stream.srt`

| Response | Content-Type | Content-Length | Body |
| --- | --- | --- | --- |
| SRT GET without a time window | `text/plain` | `617` | Exactly the input bytes, including CRLF, numbering, markup, and non-ASCII text |
| Corresponding SRT HEAD | `text/plain` | `617` | Zero bytes |
| Converted VTT GET without a time window | `text/vtt` | `502` | UTF-8 without BOM, 32 LF characters and no CRLF |
| Corresponding VTT HEAD | `text/vtt` | `502` | Zero bytes |
| Nonexistent source or subtitle index `99` | `text/plain` | `0` | HTTP 200 with an empty body |

Successful content responses advertised `Accept-Ranges: bytes`, `Cache-Control: public, no-transform`, and an ETag. The nonexistent source/index responses used no-cache headers and omitted ETag and Accept-Ranges. Conditional and Range requests were not exercised in this extension; advertised headers alone do not prove their full behavior.

The anonymous and invalid-token controls deliberately constructed `/emby/Videos/40/mediasource_40/Subtitles/2/0/Stream.srt` with no query string. The anonymous request had no recorded headers; the invalid-token request had only `X-Emby-Token: invalid-reference-token`. Neither carried an `api_key`, valid authorization header, or cookie. Both returned HTTP 200 with the complete original SRT. Each request used a separate HTTP connection.

To exclude prior authenticated access to the exact URL as the explanation, a new window URL was first requested anonymously: `/emby/Videos/40/mediasource_40/Subtitles/2/Stream.srt?EndPositionTicks=200000000&StartPositionTicks=100000000`. It returned 200 and the same 328 bytes as the subsequent authenticated request. This establishes the observed public delivery behavior in this isolated configuration. It does not establish all library-policy combinations or authorize weakening Goby's token, library, and playback access checks. Goby's stricter delivery authorization is a deliberate policy difference; generated delivery URLs should carry the caller's API token. Evidence: `subtitle-m3c-no-token.json`, `subtitle-m3c-invalid-token.json`, and `subtitle-m3c-window-unprimed-no-token.json`.

### Time-window behavior

The matrix independently varied SRT/VTT, path/query start, and `CopyTimestamps` omitted/true/false. Every matrix request used `StartPositionTicks=100000000` and `EndPositionTicks=200000000`, or the equivalent `100000000` path segment. All returned 200. Path and query start forms produced identical bytes for each matching format and CopyTimestamps value. Omitted CopyTimestamps was byte-identical to false.

| Source interval | `CopyTimestamps=true` | Omitted or false |
| --- | --- | --- |
| `0-5`, `8-10`, `9-12` | Excluded | Excluded |
| `10-11` | `10-11` | `0-1` |
| `12.5-15.25` | `12.5-15.25` | `2.5-5.25` |
| `18-22` | `18-22` | `8-12` |
| `19.5-20` | `19.5-20` | `9.5-10` |
| `20-21` | Excluded | `10-11` |
| `22-25` | Excluded | `12-15` |
| `30-33` | Excluded | Excluded |

The observed rule is consistent with filtering out cues whose original start is less than Start, subtracting Start from both timestamps when CopyTimestamps is false, then excluding cues whose resulting start is greater than or equal to End. End is therefore applied in the output timestamp coordinate system in these samples. This ordering is an inference from the controlled results, not a claim about inspected vendor implementation.

The cue crossing Start (`9-12`) is discarded entirely. A retained cue crossing End (`18-22` with CopyTimestamps true) is not clipped. Start equality is included; End equality is excluded. A nominally reversed request, Start=20 seconds and End=10 seconds with CopyTimestamps omitted, also returned 200: the original `20-21` and `22-25` cues became `0-1` and `2-5`; original `30-33` became start 10 and was excluded. Treating every End less than Start as an invalid absolute interval would not match this response.

A path start of 10 seconds combined with query `StartPositionTicks=0`, End=20 seconds, and CopyTimestamps=true returned the first seven source cues, beginning at zero. The query value took precedence in this conflict. Omitting End retained every cue beginning at or after Start, including the final `30-33` cue. An empty SRT window from 40 to 50 seconds returned 200, `text/plain`, and zero bytes.

The four-cue CopyTimestamps=true bodies were 221 bytes in SRT and 196 bytes in VTT. The six-cue omitted/false bodies were 328 and 287 bytes, respectively. Evidence: `subtitle-m3c-{srt,vtt}-{query,path}-copy-*.json`, `subtitle-m3c-*-path-query-conflict.json`, the two `*-start-only.json` files, `subtitle-m3c-reversed-window.json`, and `subtitle-m3c-empty-window.json`.

### Text serialization and integrity

Converted VTT omitted the SRT numeric identifiers, began with `WEBVTT\n\n`, and used `mm:ss.mmm` timestamps for these sub-hour cues. Cue blocks were separated by a blank line, and the last payload ended with one LF, without a final blank line. Markup and non-ASCII text survived, and multiline cue text used LF. The complete VTT body has SHA-256 `7e5ffe9d66f192edd002c6cee076df43e8c0d07b2feed56a8c167aad8c32e0bb`.

Windowed SRT was reserialized with consecutive numbering beginning at 1, `hh:mm:ss,mmm` timestamps, LF structure separators, and two LF characters after the last cue. The path/query-conflict sample retained the original CRLF inside the multiline cue payload while its surrounding structure used LF. By contrast, same-format SRT without time transformation retained the complete original byte stream. The study did not include a BOM-bearing or UTF-16 source, so their exact reference behavior remains unverified. All cue times were less than one minute; minute rollover, hour formatting, and an empty VTT document were not sampled.

The recorder saved 30 private wire-body captures before text decoding. The remote audit reconstructed each text body as UTF-8 and required byte equality with the captured body; binary bodies and JSON structures had corresponding preservation checks. All non-HEAD Content-Length values matched the captured byte length, and both HEAD comparisons retained the corresponding GET length and content type with zero body bytes. Original response headers, JSON types and numbers, and credential removal were audited before copying sanitized exports into the repository. Known synthetic paths and random reference IDs remain intact; private host details and credentials use the established sanitizer.

All 464 files belonging to the preceding 232 raw/export fixture pairs retained their SHA-256 values. New `websocket-m3c-*` files were concurrently owned by a separate capture task and were neither rewritten nor included as immutable members of this earlier 232-fixture baseline. The exporter now accepts either mapping or pair-array request headers when reading other captures for known credential values, without changing the selected fixture schema. The six new stages are `subtitle_m3c_prepare`, `subtitle_m3c_setup`, `subtitle_m3c_descriptor`, `subtitle_m3c_delivery`, `subtitle_m3c_windows`, and `export_subtitle_m3c`. No local runtime verification, vendor implementation inspection, WebSocket probe, or Git publication was performed by this extension.

## Native VTT descriptor control

Three additional `subtitle-vtt-source-*.json` fixtures resolve whether a native WebVTT sidecar uses `vtt` or `webvtt` in the response DTO. The previously captured 502-byte VTT body was saved without changes as `Reference Subtitle M3c (2026).en.forced.vtt` in the same owned movie directory. Its SHA-256 remains `7e5ffe9d66f192edd002c6cee076df43e8c0d07b2feed56a8c167aad8c32e0bb`; the file uses mode 0644 and both enclosing fixture directories use 0755. Original SRT and MP4 bytes were unchanged. Only this new subtitle library received a targeted refresh.

The item query and subsequent matching-profile PlaybackInfo both reported the new sidecar with `Codec: "vtt"`, `Language: "en"`, `IsForced: true`, `IsDefault: false`, `DisplayTitle: "English (Forced VTT)"`, and numeric `Index: 2`. PlaybackInfo returned its `/Videos/40/mediasource_40/Subtitles/2/0/Stream.vtt?api_key=...` delivery URL. The existing SRT stream moved to index 3 and received the corresponding SRT URL containing `/Subtitles/3/`.

This confirms native VTT's wire codec spelling in the sampled version and demonstrates that external stream indices can change after a library refresh adds a sidecar. The earlier 36 fixtures describe the prior one-sidecar state and remain immutable; their index-2 SRT descriptor is not a promise about the current two-sidecar state. No new delivery or timing matrix was run.

The remote export audit preserved all 536 preceding HTTP raw/export files, including the original 232 pairs and all 36 M3c pairs, and verified both original source hashes. Concurrent WebSocket records were not modified. All three new records passed the existing header, JSON type/number, and credential checks. Supporting stages are `subtitle_vtt_source_prepare`, `subtitle_vtt_source_capture`, and `export_subtitle_vtt_source`.
