# Official Emby Reference Server

Status recorded at `2026-09-08T22:37:09Z` on the authorized Linux `test-env` host. This document records observations from an isolated official Emby Server instance. It does not establish that Goby implements or passes these contracts.

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
