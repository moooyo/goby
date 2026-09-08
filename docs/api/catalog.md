# Emby HTTP API reference catalog

This catalog inventories the pinned official API description for a Linux-only Go server
with a React/MUI administrator dashboard. Playback remains available to external Emby-compatible
clients; this project does not provide an end-user web playback application.

All entries are source-derived; `planned-unimplemented` records the original research baseline.
Current code and test evidence are tracked in the [implemented surface](implemented.md).
Listing a route does not establish implementation or real-client compatibility.

## Source and offline navigation

- [Official REST API guide](https://dev.emby.media/doc/restapi/index.html).
- [Pinned official SDK commit](https://github.com/MediaBrowser/Emby.SDK/tree/bdd0dd7c0801f6e069dff2795d80cddae6f91791), repository release label **4.9.5.0**.
- [Original source file](https://raw.githubusercontent.com/MediaBrowser/Emby.SDK/bdd0dd7c0801f6e069dff2795d80cddae6f91791/Documentation/Download/openapi_v2_noversion.json) and [source provenance notes](../sources/README.md).
- [Complete Swagger 2.0 snapshot](../sources/emby-sdk-openapi.snapshot.json).
- [Machine-readable operation inventory](inventory.json).
- [All DTOs and enums](models.md).
- [Operation-level implementation scope](implementation-scope.md).
- [Older static documentation snapshot](../sources/emby-openapi.snapshot.json), used only for historical comparison.

The pinned source contains **422 paths**, **535 HTTP operations**, **70 service groups with operations**,
and **333 definitions**. It declares 70 tags.
Declared tags without operations: none.
The repository release label is provenance, not an inferred `info.version`: the source omits
both `info.title` and `info.version`. The source host `emby.restapi` and its HTTP scheme are
documentation values, not deployment recommendations. The source does not declare `basePath`;
it records `/emby` in the vendor extension `x-original-basePath`. The compatibility transport layer
must handle client base-URL conventions explicitly. The raw file retains a trailing comma in `info`;
PowerShell 7 accepts that syntax for this documentation export. The inventory is reserialized as JSON,
while the original snapshot remains unchanged. No schema validator has been run.

### Reading source declarations safely

- The original `security`, `x-RequiredAuthentication`, and authorization header definitions are preserved.
  They are evidence to investigate, not the project permission policy. For example, the source labels
  `POST /Users/AuthenticateByName` as requiring user authentication.
- `securityDefinitions.embyauth` is empty. Do not feed this snapshot directly into an authorization
  generator and assume its result is safe or complete.
- Parameter tables distinguish explicit `required` from an omitted declaration. DTO fields without
  a `required` entry are not automatically optional in every real request or response.
- The source contains no object-level `required` arrays or schema `default` declarations. Defaults
  stated in prose are retained as source descriptions in the inventory, not promoted into schema defaults.
- Some parameter types are unspecified. Structural serialization fields can disagree with prose:
  `POST /Sessions/{Id}/Playing` describes `ItemIds` as comma delimited but declares `collectionFormat: multi`.
  The inventory preserves both statements; client fixtures must resolve that discrepancy.
- A missing response schema does not prove an empty body. Binary delivery, HTTP Range behavior,
  redirects, caching, subtitle formats, HLS semantics, and WebSocket events require separate contracts.
- Every operation has an exact JSON Pointer into the local snapshot. Use that snapshot for complete
  nested schemas, source prose, vendor extensions, and reusable response declarations.
- Per-operation official reference links are derived from the documented URL pattern. Their pages
  were not individually checked; the pinned local source is the parameter evidence.

## Service-level scope candidates

`CORE-CANDIDATE` means the service contains relevant initial workflows. It does **not** place every
operation in that service into the MVP. P0/P1 selections and exclusions live in
[implementation-scope.md](implementation-scope.md). `EXPANSION` follows the initial workflows;
`DEFERRED` covers separate subsystems; `OUT-OF-SCOPE` is excluded from this product scope.

| Service | Operations | Candidate scope | Purpose |
| --- | ---: | --- | --- |
| [ActivityLogService](services/ActivityLogService.md) | 1 | CORE-CANDIDATE | Administrative activity history. |
| [ArtistsService](services/ArtistsService.md) | 3 | CORE-CANDIDATE | Artist browsing and artist metadata. |
| [AudioService](services/AudioService.md) | 6 | CORE-CANDIDATE | Audio delivery routes. |
| [BackupApi](services/BackupApi.md) | 3 | EXPANSION | Backup metadata and restoration; plugin/version provenance remains unresolved. |
| [BifService](services/BifService.md) | 2 | EXPANSION | Video preview image index delivery. |
| [BrandingService](services/BrandingService.md) | 3 | EXPANSION | Server presentation settings. |
| [ChannelService](services/ChannelService.md) | 1 | DEFERRED | Available channel listing. |
| [CodecParameterService](services/CodecParameterService.md) | 2 | EXPANSION | Reading and updating encoding codec parameters. |
| [CollectionService](services/CollectionService.md) | 4 | CORE-CANDIDATE | Collection membership and creation. |
| [ConfigurationService](services/ConfigurationService.md) | 5 | CORE-CANDIDATE | Server configuration and named settings. |
| [ConnectService](services/ConnectService.md) | 5 | OUT-OF-SCOPE | Emby Connect account integration. |
| [ContentService](services/ContentService.md) | 2 | CORE-CANDIDATE | User home sections and section item queries. |
| [DeviceService](services/DeviceService.md) | 8 | CORE-CANDIDATE | Client device records, options, and camera uploads. |
| [DisplayPreferencesService](services/DisplayPreferencesService.md) | 5 | CORE-CANDIDATE | Per-client display preferences needed by external clients. |
| [DlnaServerService](services/DlnaServerService.md) | 16 | DEFERRED | DLNA descriptions, icons, service descriptors, and control routes. |
| [DlnaService](services/DlnaService.md) | 6 | DEFERRED | DLNA profiles and profile management. |
| [DynamicHlsService](services/DynamicHlsService.md) | 14 | CORE-CANDIDATE | Adaptive audio/video HLS playlists and segments. |
| [EncodingInfoService](services/EncodingInfoService.md) | 3 | CORE-CANDIDATE | Codec configuration defaults, video codec information, and tone mapping options. |
| [EnvironmentService](services/EnvironmentService.md) | 8 | CORE-CANDIDATE | Administrative filesystem discovery. |
| [FeatureService](services/FeatureService.md) | 1 | EXPANSION | Feature availability reports. |
| [FfmpegOptionsService](services/FfmpegOptionsService.md) | 2 | CORE-CANDIDATE | Reading and updating FFmpeg options. |
| [GameGenresService](services/GameGenresService.md) | 2 | DEFERRED | Game-specific genre browsing. |
| [GenericUIApiService](services/GenericUIApiService.md) | 2 | EXPANSION | Emby generic extension UI contracts. |
| [GenresService](services/GenresService.md) | 2 | CORE-CANDIDATE | Genre browsing and genre metadata. |
| [HlsSegmentService](services/HlsSegmentService.md) | 2 | CORE-CANDIDATE | Stopping active encodings through DELETE and POST aliases. |
| [ImageService](services/ImageService.md) | 49 | CORE-CANDIDATE | Item/user image retrieval and image administration. |
| [InstantMixService](services/InstantMixService.md) | 8 | EXPANSION | Generated music mixes and audiobook next-up queries. |
| [ItemLookupService](services/ItemLookupService.md) | 14 | CORE-CANDIDATE | External metadata search and identification. |
| [ItemRefreshService](services/ItemRefreshService.md) | 1 | CORE-CANDIDATE | Item metadata refresh requests. |
| [ItemsService](services/ItemsService.md) | 3 | CORE-CANDIDATE | Filtered item queries and resume listings. |
| [ItemUpdateService](services/ItemUpdateService.md) | 2 | CORE-CANDIDATE | Item metadata editing. |
| [LibraryService](services/LibraryService.md) | 31 | CORE-CANDIDATE | Library queries, file downloads, metadata, deletion, and refresh control. |
| [LibraryStructureService](services/LibraryStructureService.md) | 10 | CORE-CANDIDATE | Library roots, media paths, and library options. |
| [LiveStreamService](services/LiveStreamService.md) | 14 | DEFERRED | Live TV recording and live-stream file delivery over HTTP and HLS. |
| [LiveTvService](services/LiveTvService.md) | 61 | DEFERRED | Tuners, guide data, channels, recordings, and recording schedules. |
| [LocalizationService](services/LocalizationService.md) | 4 | CORE-CANDIDATE | Languages, countries, and localized option metadata. |
| [MediaInfoService](services/MediaInfoService.md) | 6 | CORE-CANDIDATE | Playback negotiation and media-source lifecycle. |
| [MoviesService](services/MoviesService.md) | 1 | CORE-CANDIDATE | Movie recommendations. |
| [MusicGenresService](services/MusicGenresService.md) | 2 | CORE-CANDIDATE | Music genre browsing. |
| [NotificationsService](services/NotificationsService.md) | 2 | EXPANSION | Administrative notifications and notification type discovery. |
| [OfficialRatingService](services/OfficialRatingService.md) | 1 | CORE-CANDIDATE | Content rating lookup. |
| [OpenApiService](services/OpenApiService.md) | 4 | CORE-CANDIDATE | API description delivery. |
| [PackageService](services/PackageService.md) | 6 | OUT-OF-SCOPE | Emby package catalog and installation. |
| [PartyService](services/PartyService.md) | 5 | DEFERRED | Synchronized group playback. |
| [PersonsService](services/PersonsService.md) | 2 | CORE-CANDIDATE | People browsing and person metadata. |
| [PlaylistService](services/PlaylistService.md) | 7 | CORE-CANDIDATE | Playlist contents and ordering. |
| [PlaystateService](services/PlaystateService.md) | 12 | CORE-CANDIDATE | Playback reporting and user play-state changes. |
| [PluginService](services/PluginService.md) | 6 | EXPANSION | Emby plugin lifecycle and configuration. |
| [RemoteImageService](services/RemoteImageService.md) | 4 | CORE-CANDIDATE | External image search and image selection. |
| [ScheduledTaskService](services/ScheduledTaskService.md) | 6 | CORE-CANDIDATE | Background task status, execution, and scheduling. |
| [SessionsService](services/SessionsService.md) | 20 | CORE-CANDIDATE | API keys, authentication providers, active sessions, capabilities, and remote commands. |
| [StudiosService](services/StudiosService.md) | 2 | CORE-CANDIDATE | Studio browsing and studio metadata. |
| [SubtitleOptionsService](services/SubtitleOptionsService.md) | 2 | CORE-CANDIDATE | Reading and updating subtitle options. |
| [SubtitleService](services/SubtitleService.md) | 16 | CORE-CANDIDATE | Subtitle search, provider downloads, delivery, deletion, and media attachments. |
| [SuggestionsService](services/SuggestionsService.md) | 1 | EXPANSION | Suggested item browsing. |
| [SyncService](services/SyncService.md) | 25 | DEFERRED | Offline synchronization jobs and targets. |
| [SystemService](services/SystemService.md) | 14 | CORE-CANDIDATE | Server discovery, health information, logs, and lifecycle control. |
| [TagService](services/TagService.md) | 14 | CORE-CANDIDATE | Browse facets for tags, codecs, containers, item types, languages, years, and prefixes; tag changes. |
| [ToneMapOptionsService](services/ToneMapOptionsService.md) | 4 | EXPANSION | Reading and updating full and public tone mapping options. |
| [TrailersService](services/TrailersService.md) | 1 | EXPANSION | Trailer browsing. |
| [TvShowsService](services/TvShowsService.md) | 5 | CORE-CANDIDATE | Season, episode, next-up, missing, and upcoming queries. |
| [UniversalAudioService](services/UniversalAudioService.md) | 4 | CORE-CANDIDATE | Client-directed audio delivery negotiation. |
| [UserLibraryService](services/UserLibraryService.md) | 19 | CORE-CANDIDATE | User item data, browsing, ratings, favorites, shared-item access, and additional video parts. |
| [UserNotificationsService](services/UserNotificationsService.md) | 2 | EXPANSION | Notification service defaults and delivery test requests. |
| [UserService](services/UserService.md) | 21 | CORE-CANDIDATE | User authentication, accounts, configuration, and policy. |
| [UserViewsService](services/UserViewsService.md) | 1 | CORE-CANDIDATE | User library views and grouping. |
| [VideoHlsService](services/VideoHlsService.md) | 2 | CORE-CANDIDATE | Audio and video segment retrieval through legacy HLS routes. |
| [VideoService](services/VideoService.md) | 6 | CORE-CANDIDATE | Progressive video delivery. |
| [VideosService](services/VideosService.md) | 3 | CORE-CANDIDATE | Merging video versions and removing alternate sources. |
| [WebAppService](services/WebAppService.md) | 4 | OUT-OF-SCOPE | Emby web-application configuration pages and localization strings. |

## Complete operation index

Every entry below links to parameters, request/response schemas, source authentication declarations,
and its source pointer. All entries have status `planned-unimplemented`. Scope is inherited from
the service as a candidate classification, not an operation delivery commitment.

### ActivityLogService

Candidate: `CORE-CANDIDATE`. Administrative activity history.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/System/ActivityLog/Entries` | [getSystemActivitylogEntries](services/ActivityLogService.md#operation-getsystemactivitylogentries) |

### ArtistsService

Candidate: `CORE-CANDIDATE`. Artist browsing and artist metadata.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Artists` | [getArtists](services/ArtistsService.md#operation-getartists) |
| `GET` | `/Artists/{Name}` | [getArtistsByName](services/ArtistsService.md#operation-getartistsbyname) |
| `GET` | `/Artists/AlbumArtists` | [getArtistsAlbumartists](services/ArtistsService.md#operation-getartistsalbumartists) |

### AudioService

Candidate: `CORE-CANDIDATE`. Audio delivery routes.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Audio/{Id}/{StreamFileName}` | [getAudioByIdByStreamfilename](services/AudioService.md#operation-getaudiobyidbystreamfilename) |
| `HEAD` | `/Audio/{Id}/{StreamFileName}` | [headAudioByIdByStreamfilename](services/AudioService.md#operation-headaudiobyidbystreamfilename) |
| `GET` | `/Audio/{Id}/stream` | [getAudioByIdStream](services/AudioService.md#operation-getaudiobyidstream) |
| `HEAD` | `/Audio/{Id}/stream` | [headAudioByIdStream](services/AudioService.md#operation-headaudiobyidstream) |
| `GET` | `/Audio/{Id}/stream.{Container}` | [getAudioByIdStreamByContainer](services/AudioService.md#operation-getaudiobyidstreambycontainer) |
| `HEAD` | `/Audio/{Id}/stream.{Container}` | [headAudioByIdStreamByContainer](services/AudioService.md#operation-headaudiobyidstreambycontainer) |

### BackupApi

Candidate: `EXPANSION`. Backup metadata and restoration; plugin/version provenance remains unresolved.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/BackupRestore/BackupInfo` | [getBackuprestoreBackupinfo](services/BackupApi.md#operation-getbackuprestorebackupinfo) |
| `POST` | `/BackupRestore/Restore` | [postBackuprestoreRestore](services/BackupApi.md#operation-postbackuprestorerestore) |
| `POST` | `/BackupRestore/RestoreData` | [postBackuprestoreRestoredata](services/BackupApi.md#operation-postbackuprestorerestoredata) |

### BifService

Candidate: `EXPANSION`. Video preview image index delivery.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Items/{Id}/ThumbnailSet` | [getItemsByIdThumbnailset](services/BifService.md#operation-getitemsbyidthumbnailset) |
| `GET` | `/Videos/{Id}/index.bif` | [getVideosByIdIndexBif](services/BifService.md#operation-getvideosbyidindexbif) |

### BrandingService

Candidate: `EXPANSION`. Server presentation settings.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Branding/Configuration` | [getBrandingConfiguration](services/BrandingService.md#operation-getbrandingconfiguration) |
| `GET` | `/Branding/Css` | [getBrandingCss](services/BrandingService.md#operation-getbrandingcss) |
| `GET` | `/Branding/Css.css` | [getBrandingCssCss](services/BrandingService.md#operation-getbrandingcsscss) |

### ChannelService

Candidate: `DEFERRED`. Available channel listing.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Channels` | [getChannels](services/ChannelService.md#operation-getchannels) |

### CodecParameterService

Candidate: `EXPANSION`. Reading and updating encoding codec parameters.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Encoding/CodecParameters` | [getEncodingCodecparameters](services/CodecParameterService.md#operation-getencodingcodecparameters) |
| `POST` | `/Encoding/CodecParameters` | [postEncodingCodecparameters](services/CodecParameterService.md#operation-postencodingcodecparameters) |

### CollectionService

Candidate: `CORE-CANDIDATE`. Collection membership and creation.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Collections` | [postCollections](services/CollectionService.md#operation-postcollections) |
| `POST` | `/Collections/{Id}/Items` | [postCollectionsByIdItems](services/CollectionService.md#operation-postcollectionsbyiditems) |
| `DELETE` | `/Collections/{Id}/Items` | [deleteCollectionsByIdItems](services/CollectionService.md#operation-deletecollectionsbyiditems) |
| `POST` | `/Collections/{Id}/Items/Delete` | [postCollectionsByIdItemsDelete](services/CollectionService.md#operation-postcollectionsbyiditemsdelete) |

### ConfigurationService

Candidate: `CORE-CANDIDATE`. Server configuration and named settings.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/System/Configuration` | [getSystemConfiguration](services/ConfigurationService.md#operation-getsystemconfiguration) |
| `POST` | `/System/Configuration` | [postSystemConfiguration](services/ConfigurationService.md#operation-postsystemconfiguration) |
| `GET` | `/System/Configuration/{Key}` | [getSystemConfigurationByKey](services/ConfigurationService.md#operation-getsystemconfigurationbykey) |
| `POST` | `/System/Configuration/{Key}` | [postSystemConfigurationByKey](services/ConfigurationService.md#operation-postsystemconfigurationbykey) |
| `POST` | `/System/Configuration/Partial` | [postSystemConfigurationPartial](services/ConfigurationService.md#operation-postsystemconfigurationpartial) |

### ConnectService

Candidate: `OUT-OF-SCOPE`. Emby Connect account integration.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Connect/Exchange` | [getConnectExchange](services/ConnectService.md#operation-getconnectexchange) |
| `GET` | `/Connect/Pending` | [getConnectPending](services/ConnectService.md#operation-getconnectpending) |
| `POST` | `/Users/{Id}/Connect/Link` | [postUsersByIdConnectLink](services/ConnectService.md#operation-postusersbyidconnectlink) |
| `DELETE` | `/Users/{Id}/Connect/Link` | [deleteUsersByIdConnectLink](services/ConnectService.md#operation-deleteusersbyidconnectlink) |
| `POST` | `/Users/{Id}/Connect/Link/Delete` | [postUsersByIdConnectLinkDelete](services/ConnectService.md#operation-postusersbyidconnectlinkdelete) |

### ContentService

Candidate: `CORE-CANDIDATE`. User home sections and section item queries.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Users/{UserId}/HomeSections` | [getUsersByUseridHomesections](services/ContentService.md#operation-getusersbyuseridhomesections) |
| `GET` | `/Users/{UserId}/Sections/{SectionId}/Items` | [getUsersByUseridSectionsBySectionidItems](services/ContentService.md#operation-getusersbyuseridsectionsbysectioniditems) |

### DeviceService

Candidate: `CORE-CANDIDATE`. Client device records, options, and camera uploads.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Devices` | [getDevices](services/DeviceService.md#operation-getdevices) |
| `DELETE` | `/Devices` | [deleteDevices](services/DeviceService.md#operation-deletedevices) |
| `GET` | `/Devices/CameraUploads` | [getDevicesCamerauploads](services/DeviceService.md#operation-getdevicescamerauploads) |
| `POST` | `/Devices/CameraUploads` | [postDevicesCamerauploads](services/DeviceService.md#operation-postdevicescamerauploads) |
| `POST` | `/Devices/Delete` | [postDevicesDelete](services/DeviceService.md#operation-postdevicesdelete) |
| `GET` | `/Devices/Info` | [getDevicesInfo](services/DeviceService.md#operation-getdevicesinfo) |
| `GET` | `/Devices/Options` | [getDevicesOptions](services/DeviceService.md#operation-getdevicesoptions) |
| `POST` | `/Devices/Options` | [postDevicesOptions](services/DeviceService.md#operation-postdevicesoptions) |

### DisplayPreferencesService

Candidate: `CORE-CANDIDATE`. Per-client display preferences needed by external clients.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/DisplayPreferences/{DisplayPreferencesId}` | [postDisplaypreferencesByDisplaypreferencesid](services/DisplayPreferencesService.md#operation-postdisplaypreferencesbydisplaypreferencesid) |
| `GET` | `/DisplayPreferences/{Id}` | [getDisplaypreferencesById](services/DisplayPreferencesService.md#operation-getdisplaypreferencesbyid) |
| `GET` | `/UserSettings/{UserId}` | [getUsersettingsByUserid](services/DisplayPreferencesService.md#operation-getusersettingsbyuserid) |
| `POST` | `/UserSettings/{UserId}` | [postUsersettingsByUserid](services/DisplayPreferencesService.md#operation-postusersettingsbyuserid) |
| `POST` | `/UserSettings/{UserId}/Partial` | [postUsersettingsByUseridPartial](services/DisplayPreferencesService.md#operation-postusersettingsbyuseridpartial) |

### DlnaServerService

Candidate: `DEFERRED`. DLNA descriptions, icons, service descriptors, and control routes.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Dlna/{UuId}/connectionmanager/connectionmanager` | [getDlnaByUuidConnectionmanagerConnectionmanager](services/DlnaServerService.md#operation-getdlnabyuuidconnectionmanagerconnectionmanager) |
| `HEAD` | `/Dlna/{UuId}/connectionmanager/connectionmanager` | [headDlnaByUuidConnectionmanagerConnectionmanager](services/DlnaServerService.md#operation-headdlnabyuuidconnectionmanagerconnectionmanager) |
| `GET` | `/Dlna/{UuId}/connectionmanager/connectionmanager.xml` | [getDlnaByUuidConnectionmanagerConnectionmanagerXml](services/DlnaServerService.md#operation-getdlnabyuuidconnectionmanagerconnectionmanagerxml) |
| `HEAD` | `/Dlna/{UuId}/connectionmanager/connectionmanager.xml` | [headDlnaByUuidConnectionmanagerConnectionmanagerXml](services/DlnaServerService.md#operation-headdlnabyuuidconnectionmanagerconnectionmanagerxml) |
| `POST` | `/Dlna/{UuId}/connectionmanager/control` | [postDlnaByUuidConnectionmanagerControl](services/DlnaServerService.md#operation-postdlnabyuuidconnectionmanagercontrol) |
| `GET` | `/Dlna/{UuId}/contentdirectory/contentdirectory` | [getDlnaByUuidContentdirectoryContentdirectory](services/DlnaServerService.md#operation-getdlnabyuuidcontentdirectorycontentdirectory) |
| `HEAD` | `/Dlna/{UuId}/contentdirectory/contentdirectory` | [headDlnaByUuidContentdirectoryContentdirectory](services/DlnaServerService.md#operation-headdlnabyuuidcontentdirectorycontentdirectory) |
| `GET` | `/Dlna/{UuId}/contentdirectory/contentdirectory.xml` | [getDlnaByUuidContentdirectoryContentdirectoryXml](services/DlnaServerService.md#operation-getdlnabyuuidcontentdirectorycontentdirectoryxml) |
| `HEAD` | `/Dlna/{UuId}/contentdirectory/contentdirectory.xml` | [headDlnaByUuidContentdirectoryContentdirectoryXml](services/DlnaServerService.md#operation-headdlnabyuuidcontentdirectorycontentdirectoryxml) |
| `POST` | `/Dlna/{UuId}/contentdirectory/control` | [postDlnaByUuidContentdirectoryControl](services/DlnaServerService.md#operation-postdlnabyuuidcontentdirectorycontrol) |
| `GET` | `/Dlna/{UuId}/description` | [getDlnaByUuidDescription](services/DlnaServerService.md#operation-getdlnabyuuiddescription) |
| `HEAD` | `/Dlna/{UuId}/description` | [headDlnaByUuidDescription](services/DlnaServerService.md#operation-headdlnabyuuiddescription) |
| `GET` | `/Dlna/{UuId}/description.xml` | [getDlnaByUuidDescriptionXml](services/DlnaServerService.md#operation-getdlnabyuuiddescriptionxml) |
| `HEAD` | `/Dlna/{UuId}/description.xml` | [headDlnaByUuidDescriptionXml](services/DlnaServerService.md#operation-headdlnabyuuiddescriptionxml) |
| `GET` | `/Dlna/{UuId}/icons/{Filename}` | [getDlnaByUuidIconsByFilename](services/DlnaServerService.md#operation-getdlnabyuuidiconsbyfilename) |
| `GET` | `/Dlna/icons/{Filename}` | [getDlnaIconsByFilename](services/DlnaServerService.md#operation-getdlnaiconsbyfilename) |

### DlnaService

Candidate: `DEFERRED`. DLNA profiles and profile management.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Dlna/ProfileInfos` | [getDlnaProfileinfos](services/DlnaService.md#operation-getdlnaprofileinfos) |
| `POST` | `/Dlna/Profiles` | [postDlnaProfiles](services/DlnaService.md#operation-postdlnaprofiles) |
| `GET` | `/Dlna/Profiles/{Id}` | [getDlnaProfilesById](services/DlnaService.md#operation-getdlnaprofilesbyid) |
| `POST` | `/Dlna/Profiles/{Id}` | [postDlnaProfilesById](services/DlnaService.md#operation-postdlnaprofilesbyid) |
| `DELETE` | `/Dlna/Profiles/{Id}` | [deleteDlnaProfilesById](services/DlnaService.md#operation-deletedlnaprofilesbyid) |
| `GET` | `/Dlna/Profiles/Default` | [getDlnaProfilesDefault](services/DlnaService.md#operation-getdlnaprofilesdefault) |

### DynamicHlsService

Candidate: `CORE-CANDIDATE`. Adaptive audio/video HLS playlists and segments.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | [getAudioByIdHls1ByPlaylistidBySegmentidBySegmentcontainer](services/DynamicHlsService.md#operation-getaudiobyidhls1byplaylistidbysegmentidbysegmentcontainer) |
| `HEAD` | `/Audio/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | [headAudioByIdHls1ByPlaylistidBySegmentidBySegmentcontainer](services/DynamicHlsService.md#operation-headaudiobyidhls1byplaylistidbysegmentidbysegmentcontainer) |
| `GET` | `/Audio/{Id}/live.m3u8` | [getAudioByIdLiveM3u8](services/DynamicHlsService.md#operation-getaudiobyidlivem3u8) |
| `GET` | `/Audio/{Id}/main.m3u8` | [getAudioByIdMainM3u8](services/DynamicHlsService.md#operation-getaudiobyidmainm3u8) |
| `GET` | `/Audio/{Id}/master.m3u8` | [getAudioByIdMasterM3u8](services/DynamicHlsService.md#operation-getaudiobyidmasterm3u8) |
| `HEAD` | `/Audio/{Id}/master.m3u8` | [headAudioByIdMasterM3u8](services/DynamicHlsService.md#operation-headaudiobyidmasterm3u8) |
| `GET` | `/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | [getVideosByIdHls1ByPlaylistidBySegmentidBySegmentcontainer](services/DynamicHlsService.md#operation-getvideosbyidhls1byplaylistidbysegmentidbysegmentcontainer) |
| `HEAD` | `/Videos/{Id}/hls1/{PlaylistId}/{SegmentId}.{SegmentContainer}` | [headVideosByIdHls1ByPlaylistidBySegmentidBySegmentcontainer](services/DynamicHlsService.md#operation-headvideosbyidhls1byplaylistidbysegmentidbysegmentcontainer) |
| `GET` | `/Videos/{Id}/live_subtitles.m3u8` | [getVideosByIdLiveSubtitlesM3u8](services/DynamicHlsService.md#operation-getvideosbyidlivesubtitlesm3u8) |
| `GET` | `/Videos/{Id}/live.m3u8` | [getVideosByIdLiveM3u8](services/DynamicHlsService.md#operation-getvideosbyidlivem3u8) |
| `GET` | `/Videos/{Id}/main.m3u8` | [getVideosByIdMainM3u8](services/DynamicHlsService.md#operation-getvideosbyidmainm3u8) |
| `GET` | `/Videos/{Id}/master.m3u8` | [getVideosByIdMasterM3u8](services/DynamicHlsService.md#operation-getvideosbyidmasterm3u8) |
| `HEAD` | `/Videos/{Id}/master.m3u8` | [headVideosByIdMasterM3u8](services/DynamicHlsService.md#operation-headvideosbyidmasterm3u8) |
| `GET` | `/Videos/{Id}/subtitles.m3u8` | [getVideosByIdSubtitlesM3u8](services/DynamicHlsService.md#operation-getvideosbyidsubtitlesm3u8) |

### EncodingInfoService

Candidate: `CORE-CANDIDATE`. Codec configuration defaults, video codec information, and tone mapping options.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Encoding/CodecConfiguration/Defaults` | [getEncodingCodecconfigurationDefaults](services/EncodingInfoService.md#operation-getencodingcodecconfigurationdefaults) |
| `GET` | `/Encoding/CodecInformation/Video` | [getEncodingCodecinformationVideo](services/EncodingInfoService.md#operation-getencodingcodecinformationvideo) |
| `GET` | `/Encoding/ToneMapOptions` | [getEncodingTonemapoptions](services/EncodingInfoService.md#operation-getencodingtonemapoptions) |

### EnvironmentService

Candidate: `CORE-CANDIDATE`. Administrative filesystem discovery.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Environment/DefaultDirectoryBrowser` | [getEnvironmentDefaultdirectorybrowser](services/EnvironmentService.md#operation-getenvironmentdefaultdirectorybrowser) |
| `GET` | `/Environment/DirectoryContents` | [getEnvironmentDirectorycontents](services/EnvironmentService.md#operation-getenvironmentdirectorycontents) |
| `POST` | `/Environment/DirectoryContents` | [postEnvironmentDirectorycontents](services/EnvironmentService.md#operation-postenvironmentdirectorycontents) |
| `GET` | `/Environment/Drives` | [getEnvironmentDrives](services/EnvironmentService.md#operation-getenvironmentdrives) |
| `GET` | `/Environment/NetworkDevices` | [getEnvironmentNetworkdevices](services/EnvironmentService.md#operation-getenvironmentnetworkdevices) |
| `GET` | `/Environment/NetworkShares` | [getEnvironmentNetworkshares](services/EnvironmentService.md#operation-getenvironmentnetworkshares) |
| `GET` | `/Environment/ParentPath` | [getEnvironmentParentpath](services/EnvironmentService.md#operation-getenvironmentparentpath) |
| `POST` | `/Environment/ValidatePath` | [postEnvironmentValidatepath](services/EnvironmentService.md#operation-postenvironmentvalidatepath) |

### FeatureService

Candidate: `EXPANSION`. Feature availability reports.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Features` | [getFeatures](services/FeatureService.md#operation-getfeatures) |

### FfmpegOptionsService

Candidate: `CORE-CANDIDATE`. Reading and updating FFmpeg options.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Encoding/FfmpegOptions` | [getEncodingFfmpegoptions](services/FfmpegOptionsService.md#operation-getencodingffmpegoptions) |
| `POST` | `/Encoding/FfmpegOptions` | [postEncodingFfmpegoptions](services/FfmpegOptionsService.md#operation-postencodingffmpegoptions) |

### GameGenresService

Candidate: `DEFERRED`. Game-specific genre browsing.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/GameGenres` | [getGamegenres](services/GameGenresService.md#operation-getgamegenres) |
| `GET` | `/GameGenres/{Name}` | [getGamegenresByName](services/GameGenresService.md#operation-getgamegenresbyname) |

### GenericUIApiService

Candidate: `EXPANSION`. Emby generic extension UI contracts.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/UI/Command` | [postUICommand](services/GenericUIApiService.md#operation-postuicommand) |
| `GET` | `/UI/View` | [getUIView](services/GenericUIApiService.md#operation-getuiview) |

### GenresService

Candidate: `CORE-CANDIDATE`. Genre browsing and genre metadata.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Genres` | [getGenres](services/GenresService.md#operation-getgenres) |
| `GET` | `/Genres/{Name}` | [getGenresByName](services/GenresService.md#operation-getgenresbyname) |

### HlsSegmentService

Candidate: `CORE-CANDIDATE`. Stopping active encodings through DELETE and POST aliases.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `DELETE` | `/Videos/ActiveEncodings` | [deleteVideosActiveencodings](services/HlsSegmentService.md#operation-deletevideosactiveencodings) |
| `POST` | `/Videos/ActiveEncodings/Delete` | [postVideosActiveencodingsDelete](services/HlsSegmentService.md#operation-postvideosactiveencodingsdelete) |

### ImageService

Candidate: `CORE-CANDIDATE`. Item/user image retrieval and image administration.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Artists/{Name}/Images/{Type}` | [getArtistsByNameImagesByType](services/ImageService.md#operation-getartistsbynameimagesbytype) |
| `HEAD` | `/Artists/{Name}/Images/{Type}` | [headArtistsByNameImagesByType](services/ImageService.md#operation-headartistsbynameimagesbytype) |
| `GET` | `/Artists/{Name}/Images/{Type}/{Index}` | [getArtistsByNameImagesByTypeByIndex](services/ImageService.md#operation-getartistsbynameimagesbytypebyindex) |
| `HEAD` | `/Artists/{Name}/Images/{Type}/{Index}` | [headArtistsByNameImagesByTypeByIndex](services/ImageService.md#operation-headartistsbynameimagesbytypebyindex) |
| `GET` | `/GameGenres/{Name}/Images/{Type}` | [getGamegenresByNameImagesByType](services/ImageService.md#operation-getgamegenresbynameimagesbytype) |
| `HEAD` | `/GameGenres/{Name}/Images/{Type}` | [headGamegenresByNameImagesByType](services/ImageService.md#operation-headgamegenresbynameimagesbytype) |
| `GET` | `/GameGenres/{Name}/Images/{Type}/{Index}` | [getGamegenresByNameImagesByTypeByIndex](services/ImageService.md#operation-getgamegenresbynameimagesbytypebyindex) |
| `HEAD` | `/GameGenres/{Name}/Images/{Type}/{Index}` | [headGamegenresByNameImagesByTypeByIndex](services/ImageService.md#operation-headgamegenresbynameimagesbytypebyindex) |
| `GET` | `/Genres/{Name}/Images/{Type}` | [getGenresByNameImagesByType](services/ImageService.md#operation-getgenresbynameimagesbytype) |
| `HEAD` | `/Genres/{Name}/Images/{Type}` | [headGenresByNameImagesByType](services/ImageService.md#operation-headgenresbynameimagesbytype) |
| `GET` | `/Genres/{Name}/Images/{Type}/{Index}` | [getGenresByNameImagesByTypeByIndex](services/ImageService.md#operation-getgenresbynameimagesbytypebyindex) |
| `HEAD` | `/Genres/{Name}/Images/{Type}/{Index}` | [headGenresByNameImagesByTypeByIndex](services/ImageService.md#operation-headgenresbynameimagesbytypebyindex) |
| `GET` | `/Items/{Id}/Images` | [getItemsByIdImages](services/ImageService.md#operation-getitemsbyidimages) |
| `GET` | `/Items/{Id}/Images/{Type}` | [getItemsByIdImagesByType](services/ImageService.md#operation-getitemsbyidimagesbytype) |
| `POST` | `/Items/{Id}/Images/{Type}` | [postItemsByIdImagesByType](services/ImageService.md#operation-postitemsbyidimagesbytype) |
| `DELETE` | `/Items/{Id}/Images/{Type}` | [deleteItemsByIdImagesByType](services/ImageService.md#operation-deleteitemsbyidimagesbytype) |
| `HEAD` | `/Items/{Id}/Images/{Type}` | [headItemsByIdImagesByType](services/ImageService.md#operation-headitemsbyidimagesbytype) |
| `GET` | `/Items/{Id}/Images/{Type}/{Index}` | [getItemsByIdImagesByTypeByIndex](services/ImageService.md#operation-getitemsbyidimagesbytypebyindex) |
| `POST` | `/Items/{Id}/Images/{Type}/{Index}` | [postItemsByIdImagesByTypeByIndex](services/ImageService.md#operation-postitemsbyidimagesbytypebyindex) |
| `DELETE` | `/Items/{Id}/Images/{Type}/{Index}` | [deleteItemsByIdImagesByTypeByIndex](services/ImageService.md#operation-deleteitemsbyidimagesbytypebyindex) |
| `HEAD` | `/Items/{Id}/Images/{Type}/{Index}` | [headItemsByIdImagesByTypeByIndex](services/ImageService.md#operation-headitemsbyidimagesbytypebyindex) |
| `GET` | `/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}` | [getItemsByIdImagesByTypeByIndexByTagByFormatByMaxwidthByMaxheightByPercentplayedByUnplayedcount](services/ImageService.md#operation-getitemsbyidimagesbytypebyindexbytagbyformatbymaxwidthbymaxheightbypercentplayedbyunplayedcount) |
| `HEAD` | `/Items/{Id}/Images/{Type}/{Index}/{Tag}/{Format}/{MaxWidth}/{MaxHeight}/{PercentPlayed}/{UnPlayedCount}` | [headItemsByIdImagesByTypeByIndexByTagByFormatByMaxwidthByMaxheightByPercentplayedByUnplayedcount](services/ImageService.md#operation-headitemsbyidimagesbytypebyindexbytagbyformatbymaxwidthbymaxheightbypercentplayedbyunplayedcount) |
| `POST` | `/Items/{Id}/Images/{Type}/{Index}/Delete` | [postItemsByIdImagesByTypeByIndexDelete](services/ImageService.md#operation-postitemsbyidimagesbytypebyindexdelete) |
| `POST` | `/Items/{Id}/Images/{Type}/{Index}/Index` | [postItemsByIdImagesByTypeByIndexIndex](services/ImageService.md#operation-postitemsbyidimagesbytypebyindexindex) |
| `POST` | `/Items/{Id}/Images/{Type}/{Index}/Url` | [postItemsByIdImagesByTypeByIndexUrl](services/ImageService.md#operation-postitemsbyidimagesbytypebyindexurl) |
| `POST` | `/Items/{Id}/Images/{Type}/Delete` | [postItemsByIdImagesByTypeDelete](services/ImageService.md#operation-postitemsbyidimagesbytypedelete) |
| `GET` | `/MusicGenres/{Name}/Images/{Type}` | [getMusicgenresByNameImagesByType](services/ImageService.md#operation-getmusicgenresbynameimagesbytype) |
| `HEAD` | `/MusicGenres/{Name}/Images/{Type}` | [headMusicgenresByNameImagesByType](services/ImageService.md#operation-headmusicgenresbynameimagesbytype) |
| `GET` | `/MusicGenres/{Name}/Images/{Type}/{Index}` | [getMusicgenresByNameImagesByTypeByIndex](services/ImageService.md#operation-getmusicgenresbynameimagesbytypebyindex) |
| `HEAD` | `/MusicGenres/{Name}/Images/{Type}/{Index}` | [headMusicgenresByNameImagesByTypeByIndex](services/ImageService.md#operation-headmusicgenresbynameimagesbytypebyindex) |
| `GET` | `/Persons/{Name}/Images/{Type}` | [getPersonsByNameImagesByType](services/ImageService.md#operation-getpersonsbynameimagesbytype) |
| `HEAD` | `/Persons/{Name}/Images/{Type}` | [headPersonsByNameImagesByType](services/ImageService.md#operation-headpersonsbynameimagesbytype) |
| `GET` | `/Persons/{Name}/Images/{Type}/{Index}` | [getPersonsByNameImagesByTypeByIndex](services/ImageService.md#operation-getpersonsbynameimagesbytypebyindex) |
| `HEAD` | `/Persons/{Name}/Images/{Type}/{Index}` | [headPersonsByNameImagesByTypeByIndex](services/ImageService.md#operation-headpersonsbynameimagesbytypebyindex) |
| `GET` | `/Studios/{Name}/Images/{Type}` | [getStudiosByNameImagesByType](services/ImageService.md#operation-getstudiosbynameimagesbytype) |
| `HEAD` | `/Studios/{Name}/Images/{Type}` | [headStudiosByNameImagesByType](services/ImageService.md#operation-headstudiosbynameimagesbytype) |
| `GET` | `/Studios/{Name}/Images/{Type}/{Index}` | [getStudiosByNameImagesByTypeByIndex](services/ImageService.md#operation-getstudiosbynameimagesbytypebyindex) |
| `HEAD` | `/Studios/{Name}/Images/{Type}/{Index}` | [headStudiosByNameImagesByTypeByIndex](services/ImageService.md#operation-headstudiosbynameimagesbytypebyindex) |
| `GET` | `/Users/{Id}/Images/{Type}` | [getUsersByIdImagesByType](services/ImageService.md#operation-getusersbyidimagesbytype) |
| `POST` | `/Users/{Id}/Images/{Type}` | [postUsersByIdImagesByType](services/ImageService.md#operation-postusersbyidimagesbytype) |
| `DELETE` | `/Users/{Id}/Images/{Type}` | [deleteUsersByIdImagesByType](services/ImageService.md#operation-deleteusersbyidimagesbytype) |
| `HEAD` | `/Users/{Id}/Images/{Type}` | [headUsersByIdImagesByType](services/ImageService.md#operation-headusersbyidimagesbytype) |
| `GET` | `/Users/{Id}/Images/{Type}/{Index}` | [getUsersByIdImagesByTypeByIndex](services/ImageService.md#operation-getusersbyidimagesbytypebyindex) |
| `POST` | `/Users/{Id}/Images/{Type}/{Index}` | [postUsersByIdImagesByTypeByIndex](services/ImageService.md#operation-postusersbyidimagesbytypebyindex) |
| `DELETE` | `/Users/{Id}/Images/{Type}/{Index}` | [deleteUsersByIdImagesByTypeByIndex](services/ImageService.md#operation-deleteusersbyidimagesbytypebyindex) |
| `HEAD` | `/Users/{Id}/Images/{Type}/{Index}` | [headUsersByIdImagesByTypeByIndex](services/ImageService.md#operation-headusersbyidimagesbytypebyindex) |
| `POST` | `/Users/{Id}/Images/{Type}/{Index}/Delete` | [postUsersByIdImagesByTypeByIndexDelete](services/ImageService.md#operation-postusersbyidimagesbytypebyindexdelete) |
| `POST` | `/Users/{Id}/Images/{Type}/Delete` | [postUsersByIdImagesByTypeDelete](services/ImageService.md#operation-postusersbyidimagesbytypedelete) |

### InstantMixService

Candidate: `EXPANSION`. Generated music mixes and audiobook next-up queries.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Albums/{Id}/InstantMix` | [getAlbumsByIdInstantmix](services/InstantMixService.md#operation-getalbumsbyidinstantmix) |
| `GET` | `/Artists/InstantMix` | [getArtistsInstantmix](services/InstantMixService.md#operation-getartistsinstantmix) |
| `GET` | `/AudioBooks/NextUp` | [getAudiobooksNextup](services/InstantMixService.md#operation-getaudiobooksnextup) |
| `GET` | `/Items/{Id}/InstantMix` | [getItemsByIdInstantmix](services/InstantMixService.md#operation-getitemsbyidinstantmix) |
| `GET` | `/MusicGenres/{Name}/InstantMix` | [getMusicgenresByNameInstantmix](services/InstantMixService.md#operation-getmusicgenresbynameinstantmix) |
| `GET` | `/MusicGenres/InstantMix` | [getMusicgenresInstantmix](services/InstantMixService.md#operation-getmusicgenresinstantmix) |
| `GET` | `/Playlists/{Id}/InstantMix` | [getPlaylistsByIdInstantmix](services/InstantMixService.md#operation-getplaylistsbyidinstantmix) |
| `GET` | `/Songs/{Id}/InstantMix` | [getSongsByIdInstantmix](services/InstantMixService.md#operation-getsongsbyidinstantmix) |

### ItemLookupService

Candidate: `CORE-CANDIDATE`. External metadata search and identification.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Items/{Id}/ExternalIdInfos` | [getItemsByIdExternalidinfos](services/ItemLookupService.md#operation-getitemsbyidexternalidinfos) |
| `POST` | `/Items/Metadata/Reset` | [postItemsMetadataReset](services/ItemLookupService.md#operation-postitemsmetadatareset) |
| `POST` | `/Items/RemoteSearch/Apply/{Id}` | [postItemsRemotesearchApplyById](services/ItemLookupService.md#operation-postitemsremotesearchapplybyid) |
| `POST` | `/Items/RemoteSearch/Book` | [postItemsRemotesearchBook](services/ItemLookupService.md#operation-postitemsremotesearchbook) |
| `POST` | `/Items/RemoteSearch/BoxSet` | [postItemsRemotesearchBoxset](services/ItemLookupService.md#operation-postitemsremotesearchboxset) |
| `POST` | `/Items/RemoteSearch/Game` | [postItemsRemotesearchGame](services/ItemLookupService.md#operation-postitemsremotesearchgame) |
| `GET` | `/Items/RemoteSearch/Image` | [getItemsRemotesearchImage](services/ItemLookupService.md#operation-getitemsremotesearchimage) |
| `POST` | `/Items/RemoteSearch/Movie` | [postItemsRemotesearchMovie](services/ItemLookupService.md#operation-postitemsremotesearchmovie) |
| `POST` | `/Items/RemoteSearch/MusicAlbum` | [postItemsRemotesearchMusicalbum](services/ItemLookupService.md#operation-postitemsremotesearchmusicalbum) |
| `POST` | `/Items/RemoteSearch/MusicArtist` | [postItemsRemotesearchMusicartist](services/ItemLookupService.md#operation-postitemsremotesearchmusicartist) |
| `POST` | `/Items/RemoteSearch/MusicVideo` | [postItemsRemotesearchMusicvideo](services/ItemLookupService.md#operation-postitemsremotesearchmusicvideo) |
| `POST` | `/Items/RemoteSearch/Person` | [postItemsRemotesearchPerson](services/ItemLookupService.md#operation-postitemsremotesearchperson) |
| `POST` | `/Items/RemoteSearch/Series` | [postItemsRemotesearchSeries](services/ItemLookupService.md#operation-postitemsremotesearchseries) |
| `POST` | `/Items/RemoteSearch/Trailer` | [postItemsRemotesearchTrailer](services/ItemLookupService.md#operation-postitemsremotesearchtrailer) |

### ItemRefreshService

Candidate: `CORE-CANDIDATE`. Item metadata refresh requests.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Items/{Id}/Refresh` | [postItemsByIdRefresh](services/ItemRefreshService.md#operation-postitemsbyidrefresh) |

### ItemsService

Candidate: `CORE-CANDIDATE`. Filtered item queries and resume listings.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Items` | [getItems](services/ItemsService.md#operation-getitems) |
| `GET` | `/Users/{UserId}/Items` | [getUsersByUseridItems](services/ItemsService.md#operation-getusersbyuseriditems) |
| `GET` | `/Users/{UserId}/Items/Resume` | [getUsersByUseridItemsResume](services/ItemsService.md#operation-getusersbyuseriditemsresume) |

### ItemUpdateService

Candidate: `CORE-CANDIDATE`. Item metadata editing.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Items/{ItemId}` | [postItemsByItemid](services/ItemUpdateService.md#operation-postitemsbyitemid) |
| `GET` | `/Items/{ItemId}/MetadataEditor` | [getItemsByItemidMetadataeditor](services/ItemUpdateService.md#operation-getitemsbyitemidmetadataeditor) |

### LibraryService

Candidate: `CORE-CANDIDATE`. Library queries, file downloads, metadata, deletion, and refresh control.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Albums/{Id}/Similar` | [getAlbumsByIdSimilar](services/LibraryService.md#operation-getalbumsbyidsimilar) |
| `GET` | `/Artists/{Id}/Similar` | [getArtistsByIdSimilar](services/LibraryService.md#operation-getartistsbyidsimilar) |
| `GET` | `/Games/{Id}/Similar` | [getGamesByIdSimilar](services/LibraryService.md#operation-getgamesbyidsimilar) |
| `DELETE` | `/Items` | [deleteItems](services/LibraryService.md#operation-deleteitems) |
| `DELETE` | `/Items/{Id}` | [deleteItemsById](services/LibraryService.md#operation-deleteitemsbyid) |
| `GET` | `/Items/{Id}/Ancestors` | [getItemsByIdAncestors](services/LibraryService.md#operation-getitemsbyidancestors) |
| `GET` | `/Items/{Id}/CriticReviews` | [getItemsByIdCriticreviews](services/LibraryService.md#operation-getitemsbyidcriticreviews) |
| `POST` | `/Items/{Id}/Delete` | [postItemsByIdDelete](services/LibraryService.md#operation-postitemsbyiddelete) |
| `GET` | `/Items/{Id}/DeleteInfo` | [getItemsByIdDeleteinfo](services/LibraryService.md#operation-getitemsbyiddeleteinfo) |
| `GET` | `/Items/{Id}/Download` | [getItemsByIdDownload](services/LibraryService.md#operation-getitemsbyiddownload) |
| `GET` | `/Items/{Id}/File` | [getItemsByIdFile](services/LibraryService.md#operation-getitemsbyidfile) |
| `GET` | `/Items/{Id}/Similar` | [getItemsByIdSimilar](services/LibraryService.md#operation-getitemsbyidsimilar) |
| `GET` | `/Items/{Id}/ThemeMedia` | [getItemsByIdThememedia](services/LibraryService.md#operation-getitemsbyidthememedia) |
| `GET` | `/Items/{Id}/ThemeSongs` | [getItemsByIdThemesongs](services/LibraryService.md#operation-getitemsbyidthemesongs) |
| `GET` | `/Items/{Id}/ThemeVideos` | [getItemsByIdThemevideos](services/LibraryService.md#operation-getitemsbyidthemevideos) |
| `GET` | `/Items/Counts` | [getItemsCounts](services/LibraryService.md#operation-getitemscounts) |
| `POST` | `/Items/Delete` | [postItemsDelete](services/LibraryService.md#operation-postitemsdelete) |
| `GET` | `/Items/Intros` | [getItemsIntros](services/LibraryService.md#operation-getitemsintros) |
| `GET` | `/Libraries/AvailableOptions` | [getLibrariesAvailableoptions](services/LibraryService.md#operation-getlibrariesavailableoptions) |
| `POST` | `/Library/Media/Updated` | [postLibraryMediaUpdated](services/LibraryService.md#operation-postlibrarymediaupdated) |
| `GET` | `/Library/MediaFolders` | [getLibraryMediafolders](services/LibraryService.md#operation-getlibrarymediafolders) |
| `POST` | `/Library/Movies/Added` | [postLibraryMoviesAdded](services/LibraryService.md#operation-postlibrarymoviesadded) |
| `POST` | `/Library/Movies/Updated` | [postLibraryMoviesUpdated](services/LibraryService.md#operation-postlibrarymoviesupdated) |
| `GET` | `/Library/PhysicalPaths` | [getLibraryPhysicalpaths](services/LibraryService.md#operation-getlibraryphysicalpaths) |
| `POST` | `/Library/Refresh` | [postLibraryRefresh](services/LibraryService.md#operation-postlibraryrefresh) |
| `GET` | `/Library/SelectableMediaFolders` | [getLibrarySelectablemediafolders](services/LibraryService.md#operation-getlibraryselectablemediafolders) |
| `POST` | `/Library/Series/Added` | [postLibrarySeriesAdded](services/LibraryService.md#operation-postlibraryseriesadded) |
| `POST` | `/Library/Series/Updated` | [postLibrarySeriesUpdated](services/LibraryService.md#operation-postlibraryseriesupdated) |
| `GET` | `/Movies/{Id}/Similar` | [getMoviesByIdSimilar](services/LibraryService.md#operation-getmoviesbyidsimilar) |
| `GET` | `/Shows/{Id}/Similar` | [getShowsByIdSimilar](services/LibraryService.md#operation-getshowsbyidsimilar) |
| `GET` | `/Trailers/{Id}/Similar` | [getTrailersByIdSimilar](services/LibraryService.md#operation-gettrailersbyidsimilar) |

### LibraryStructureService

Candidate: `CORE-CANDIDATE`. Library roots, media paths, and library options.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Library/VirtualFolders` | [postLibraryVirtualfolders](services/LibraryStructureService.md#operation-postlibraryvirtualfolders) |
| `DELETE` | `/Library/VirtualFolders` | [deleteLibraryVirtualfolders](services/LibraryStructureService.md#operation-deletelibraryvirtualfolders) |
| `POST` | `/Library/VirtualFolders/Delete` | [postLibraryVirtualfoldersDelete](services/LibraryStructureService.md#operation-postlibraryvirtualfoldersdelete) |
| `POST` | `/Library/VirtualFolders/LibraryOptions` | [postLibraryVirtualfoldersLibraryoptions](services/LibraryStructureService.md#operation-postlibraryvirtualfolderslibraryoptions) |
| `POST` | `/Library/VirtualFolders/Name` | [postLibraryVirtualfoldersName](services/LibraryStructureService.md#operation-postlibraryvirtualfoldersname) |
| `POST` | `/Library/VirtualFolders/Paths` | [postLibraryVirtualfoldersPaths](services/LibraryStructureService.md#operation-postlibraryvirtualfolderspaths) |
| `DELETE` | `/Library/VirtualFolders/Paths` | [deleteLibraryVirtualfoldersPaths](services/LibraryStructureService.md#operation-deletelibraryvirtualfolderspaths) |
| `POST` | `/Library/VirtualFolders/Paths/Delete` | [postLibraryVirtualfoldersPathsDelete](services/LibraryStructureService.md#operation-postlibraryvirtualfolderspathsdelete) |
| `POST` | `/Library/VirtualFolders/Paths/Update` | [postLibraryVirtualfoldersPathsUpdate](services/LibraryStructureService.md#operation-postlibraryvirtualfolderspathsupdate) |
| `GET` | `/Library/VirtualFolders/Query` | [getLibraryVirtualfoldersQuery](services/LibraryStructureService.md#operation-getlibraryvirtualfoldersquery) |

### LiveStreamService

Candidate: `DEFERRED`. Live TV recording and live-stream file delivery over HTTP and HLS.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/LiveTv/LiveRecordings/{Id}/hls/{Segment}` | [getLivetvLiverecordingsByIdHlsBySegment](services/LiveStreamService.md#operation-getlivetvliverecordingsbyidhlsbysegment) |
| `HEAD` | `/LiveTv/LiveRecordings/{Id}/hls/{Segment}` | [headLivetvLiverecordingsByIdHlsBySegment](services/LiveStreamService.md#operation-headlivetvliverecordingsbyidhlsbysegment) |
| `GET` | `/LiveTv/LiveRecordings/{Id}/hls/live.m3u8` | [getLivetvLiverecordingsByIdHlsLiveM3u8](services/LiveStreamService.md#operation-getlivetvliverecordingsbyidhlslivem3u8) |
| `HEAD` | `/LiveTv/LiveRecordings/{Id}/hls/live.m3u8` | [headLivetvLiverecordingsByIdHlsLiveM3u8](services/LiveStreamService.md#operation-headlivetvliverecordingsbyidhlslivem3u8) |
| `GET` | `/LiveTv/LiveRecordings/{Id}/hls/master.m3u8` | [getLivetvLiverecordingsByIdHlsMasterM3u8](services/LiveStreamService.md#operation-getlivetvliverecordingsbyidhlsmasterm3u8) |
| `HEAD` | `/LiveTv/LiveRecordings/{Id}/hls/master.m3u8` | [headLivetvLiverecordingsByIdHlsMasterM3u8](services/LiveStreamService.md#operation-headlivetvliverecordingsbyidhlsmasterm3u8) |
| `GET` | `/LiveTv/LiveRecordings/{Id}/stream` | [getLivetvLiverecordingsByIdStream](services/LiveStreamService.md#operation-getlivetvliverecordingsbyidstream) |
| `GET` | `/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}` | [getLivetvLivestreamfilesByIdHlsBySegment](services/LiveStreamService.md#operation-getlivetvlivestreamfilesbyidhlsbysegment) |
| `HEAD` | `/LiveTv/LiveStreamFiles/{Id}/hls/{Segment}` | [headLivetvLivestreamfilesByIdHlsBySegment](services/LiveStreamService.md#operation-headlivetvlivestreamfilesbyidhlsbysegment) |
| `GET` | `/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8` | [getLivetvLivestreamfilesByIdHlsLiveM3u8](services/LiveStreamService.md#operation-getlivetvlivestreamfilesbyidhlslivem3u8) |
| `HEAD` | `/LiveTv/LiveStreamFiles/{Id}/hls/live.m3u8` | [headLivetvLivestreamfilesByIdHlsLiveM3u8](services/LiveStreamService.md#operation-headlivetvlivestreamfilesbyidhlslivem3u8) |
| `GET` | `/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8` | [getLivetvLivestreamfilesByIdHlsMasterM3u8](services/LiveStreamService.md#operation-getlivetvlivestreamfilesbyidhlsmasterm3u8) |
| `HEAD` | `/LiveTv/LiveStreamFiles/{Id}/hls/master.m3u8` | [headLivetvLivestreamfilesByIdHlsMasterM3u8](services/LiveStreamService.md#operation-headlivetvlivestreamfilesbyidhlsmasterm3u8) |
| `GET` | `/LiveTv/LiveStreamFiles/{Id}/stream.{Container}` | [getLivetvLivestreamfilesByIdStreamByContainer](services/LiveStreamService.md#operation-getlivetvlivestreamfilesbyidstreambycontainer) |

### LiveTvService

Candidate: `DEFERRED`. Tuners, guide data, channels, recordings, and recording schedules.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/LiveTv/AvailableRecordingOptions` | [getLivetvAvailablerecordingoptions](services/LiveTvService.md#operation-getlivetvavailablerecordingoptions) |
| `GET` | `/LiveTv/ChannelMappingOptions` | [getLivetvChannelmappingoptions](services/LiveTvService.md#operation-getlivetvchannelmappingoptions) |
| `POST` | `/LiveTv/ChannelMappingOptions` | [postLivetvChannelmappingoptions](services/LiveTvService.md#operation-postlivetvchannelmappingoptions) |
| `PUT` | `/LiveTv/ChannelMappingOptions` | [putLivetvChannelmappingoptions](services/LiveTvService.md#operation-putlivetvchannelmappingoptions) |
| `DELETE` | `/LiveTv/ChannelMappingOptions` | [deleteLivetvChannelmappingoptions](services/LiveTvService.md#operation-deletelivetvchannelmappingoptions) |
| `HEAD` | `/LiveTv/ChannelMappingOptions` | [headLivetvChannelmappingoptions](services/LiveTvService.md#operation-headlivetvchannelmappingoptions) |
| `GET` | `/LiveTv/ChannelMappings` | [getLivetvChannelmappings](services/LiveTvService.md#operation-getlivetvchannelmappings) |
| `POST` | `/LiveTv/ChannelMappings` | [postLivetvChannelmappings](services/LiveTvService.md#operation-postlivetvchannelmappings) |
| `PUT` | `/LiveTv/ChannelMappings` | [putLivetvChannelmappings](services/LiveTvService.md#operation-putlivetvchannelmappings) |
| `DELETE` | `/LiveTv/ChannelMappings` | [deleteLivetvChannelmappings](services/LiveTvService.md#operation-deletelivetvchannelmappings) |
| `HEAD` | `/LiveTv/ChannelMappings` | [headLivetvChannelmappings](services/LiveTvService.md#operation-headlivetvchannelmappings) |
| `GET` | `/LiveTv/Channels` | [getLivetvChannels](services/LiveTvService.md#operation-getlivetvchannels) |
| `GET` | `/LiveTv/Channels/{Id}` | [getLivetvChannelsById](services/LiveTvService.md#operation-getlivetvchannelsbyid) |
| `GET` | `/LiveTv/ChannelTags` | [getLivetvChanneltags](services/LiveTvService.md#operation-getlivetvchanneltags) |
| `GET` | `/LiveTv/ChannelTags/Prefixes` | [getLivetvChanneltagsPrefixes](services/LiveTvService.md#operation-getlivetvchanneltagsprefixes) |
| `GET` | `/LiveTv/EPG` | [getLivetvEPG](services/LiveTvService.md#operation-getlivetvepg) |
| `GET` | `/LiveTv/Folder` | [getLivetvFolder](services/LiveTvService.md#operation-getlivetvfolder) |
| `GET` | `/LiveTv/GuideInfo` | [getLivetvGuideinfo](services/LiveTvService.md#operation-getlivetvguideinfo) |
| `GET` | `/LiveTv/Info` | [getLivetvInfo](services/LiveTvService.md#operation-getlivetvinfo) |
| `GET` | `/LiveTv/ListingProviders` | [getLivetvListingproviders](services/LiveTvService.md#operation-getlivetvlistingproviders) |
| `POST` | `/LiveTv/ListingProviders` | [postLivetvListingproviders](services/LiveTvService.md#operation-postlivetvlistingproviders) |
| `DELETE` | `/LiveTv/ListingProviders` | [deleteLivetvListingproviders](services/LiveTvService.md#operation-deletelivetvlistingproviders) |
| `GET` | `/LiveTv/ListingProviders/Available` | [getLivetvListingprovidersAvailable](services/LiveTvService.md#operation-getlivetvlistingprovidersavailable) |
| `GET` | `/LiveTv/ListingProviders/Default` | [getLivetvListingprovidersDefault](services/LiveTvService.md#operation-getlivetvlistingprovidersdefault) |
| `POST` | `/LiveTv/ListingProviders/Delete` | [postLivetvListingprovidersDelete](services/LiveTvService.md#operation-postlivetvlistingprovidersdelete) |
| `GET` | `/LiveTv/ListingProviders/Lineups` | [getLivetvListingprovidersLineups](services/LiveTvService.md#operation-getlivetvlistingproviderslineups) |
| `GET` | `/LiveTv/Manage/Channels` | [getLivetvManageChannels](services/LiveTvService.md#operation-getlivetvmanagechannels) |
| `POST` | `/LiveTv/Manage/Channels/{Id}/Disabled` | [postLivetvManageChannelsByIdDisabled](services/LiveTvService.md#operation-postlivetvmanagechannelsbyiddisabled) |
| `POST` | `/LiveTv/Manage/Channels/{Id}/SortIndex` | [postLivetvManageChannelsByIdSortindex](services/LiveTvService.md#operation-postlivetvmanagechannelsbyidsortindex) |
| `GET` | `/LiveTv/Programs` | [getLivetvPrograms](services/LiveTvService.md#operation-getlivetvprograms) |
| `POST` | `/LiveTv/Programs` | [postLivetvPrograms](services/LiveTvService.md#operation-postlivetvprograms) |
| `GET` | `/LiveTv/Programs/Recommended` | [getLivetvProgramsRecommended](services/LiveTvService.md#operation-getlivetvprogramsrecommended) |
| `GET` | `/LiveTv/Recordings` | [getLivetvRecordings](services/LiveTvService.md#operation-getlivetvrecordings) |
| `GET` | `/LiveTv/Recordings/{Id}` | [getLivetvRecordingsById](services/LiveTvService.md#operation-getlivetvrecordingsbyid) |
| `DELETE` | `/LiveTv/Recordings/{Id}` | [deleteLivetvRecordingsById](services/LiveTvService.md#operation-deletelivetvrecordingsbyid) |
| `POST` | `/LiveTv/Recordings/{Id}/Delete` | [postLivetvRecordingsByIdDelete](services/LiveTvService.md#operation-postlivetvrecordingsbyiddelete) |
| `GET` | `/LiveTv/Recordings/Folders` | [getLivetvRecordingsFolders](services/LiveTvService.md#operation-getlivetvrecordingsfolders) |
| `GET` | `/LiveTv/Recordings/Groups` | [getLivetvRecordingsGroups](services/LiveTvService.md#operation-getlivetvrecordingsgroups) |
| `GET` | `/LiveTv/Recordings/Series` | [getLivetvRecordingsSeries](services/LiveTvService.md#operation-getlivetvrecordingsseries) |
| `GET` | `/LiveTv/SeriesTimers` | [getLivetvSeriestimers](services/LiveTvService.md#operation-getlivetvseriestimers) |
| `POST` | `/LiveTv/SeriesTimers` | [postLivetvSeriestimers](services/LiveTvService.md#operation-postlivetvseriestimers) |
| `GET` | `/LiveTv/SeriesTimers/{Id}` | [getLivetvSeriestimersById](services/LiveTvService.md#operation-getlivetvseriestimersbyid) |
| `POST` | `/LiveTv/SeriesTimers/{Id}` | [postLivetvSeriestimersById](services/LiveTvService.md#operation-postlivetvseriestimersbyid) |
| `DELETE` | `/LiveTv/SeriesTimers/{Id}` | [deleteLivetvSeriestimersById](services/LiveTvService.md#operation-deletelivetvseriestimersbyid) |
| `POST` | `/LiveTv/SeriesTimers/{Id}/Delete` | [postLivetvSeriestimersByIdDelete](services/LiveTvService.md#operation-postlivetvseriestimersbyiddelete) |
| `GET` | `/LiveTv/Timers` | [getLivetvTimers](services/LiveTvService.md#operation-getlivetvtimers) |
| `POST` | `/LiveTv/Timers` | [postLivetvTimers](services/LiveTvService.md#operation-postlivetvtimers) |
| `GET` | `/LiveTv/Timers/{Id}` | [getLivetvTimersById](services/LiveTvService.md#operation-getlivetvtimersbyid) |
| `POST` | `/LiveTv/Timers/{Id}` | [postLivetvTimersById](services/LiveTvService.md#operation-postlivetvtimersbyid) |
| `DELETE` | `/LiveTv/Timers/{Id}` | [deleteLivetvTimersById](services/LiveTvService.md#operation-deletelivetvtimersbyid) |
| `POST` | `/LiveTv/Timers/{Id}/Delete` | [postLivetvTimersByIdDelete](services/LiveTvService.md#operation-postlivetvtimersbyiddelete) |
| `GET` | `/LiveTv/Timers/Defaults` | [getLivetvTimersDefaults](services/LiveTvService.md#operation-getlivetvtimersdefaults) |
| `GET` | `/LiveTv/TunerHosts` | [getLivetvTunerhosts](services/LiveTvService.md#operation-getlivetvtunerhosts) |
| `POST` | `/LiveTv/TunerHosts` | [postLivetvTunerhosts](services/LiveTvService.md#operation-postlivetvtunerhosts) |
| `DELETE` | `/LiveTv/TunerHosts` | [deleteLivetvTunerhosts](services/LiveTvService.md#operation-deletelivetvtunerhosts) |
| `GET` | `/LiveTv/TunerHosts/Default/{Type}` | [getLivetvTunerhostsDefaultByType](services/LiveTvService.md#operation-getlivetvtunerhostsdefaultbytype) |
| `POST` | `/LiveTv/TunerHosts/Delete` | [postLivetvTunerhostsDelete](services/LiveTvService.md#operation-postlivetvtunerhostsdelete) |
| `GET` | `/LiveTv/TunerHosts/Types` | [getLivetvTunerhostsTypes](services/LiveTvService.md#operation-getlivetvtunerhoststypes) |
| `POST` | `/LiveTv/Tuners/{Id}/Reset` | [postLivetvTunersByIdReset](services/LiveTvService.md#operation-postlivetvtunersbyidreset) |
| `GET` | `/LiveTv/Tuners/Discover` | [getLivetvTunersDiscover](services/LiveTvService.md#operation-getlivetvtunersdiscover) |
| `GET` | `/LiveTv/Tuners/Discvover` | [getLivetvTunersDiscvover](services/LiveTvService.md#operation-getlivetvtunersdiscvover) |

### LocalizationService

Candidate: `CORE-CANDIDATE`. Languages, countries, and localized option metadata.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Localization/Countries` | [getLocalizationCountries](services/LocalizationService.md#operation-getlocalizationcountries) |
| `GET` | `/Localization/Cultures` | [getLocalizationCultures](services/LocalizationService.md#operation-getlocalizationcultures) |
| `GET` | `/Localization/Options` | [getLocalizationOptions](services/LocalizationService.md#operation-getlocalizationoptions) |
| `GET` | `/Localization/ParentalRatings` | [getLocalizationParentalratings](services/LocalizationService.md#operation-getlocalizationparentalratings) |

### MediaInfoService

Candidate: `CORE-CANDIDATE`. Playback negotiation and media-source lifecycle.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Items/{Id}/PlaybackInfo` | [getItemsByIdPlaybackinfo](services/MediaInfoService.md#operation-getitemsbyidplaybackinfo) |
| `POST` | `/Items/{Id}/PlaybackInfo` | [postItemsByIdPlaybackinfo](services/MediaInfoService.md#operation-postitemsbyidplaybackinfo) |
| `POST` | `/LiveStreams/Close` | [postLivestreamsClose](services/MediaInfoService.md#operation-postlivestreamsclose) |
| `POST` | `/LiveStreams/MediaInfo` | [postLivestreamsMediainfo](services/MediaInfoService.md#operation-postlivestreamsmediainfo) |
| `POST` | `/LiveStreams/Open` | [postLivestreamsOpen](services/MediaInfoService.md#operation-postlivestreamsopen) |
| `GET` | `/Playback/BitrateTest` | [getPlaybackBitratetest](services/MediaInfoService.md#operation-getplaybackbitratetest) |

### MoviesService

Candidate: `CORE-CANDIDATE`. Movie recommendations.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Movies/Recommendations` | [getMoviesRecommendations](services/MoviesService.md#operation-getmoviesrecommendations) |

### MusicGenresService

Candidate: `CORE-CANDIDATE`. Music genre browsing.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/MusicGenres` | [getMusicgenres](services/MusicGenresService.md#operation-getmusicgenres) |
| `GET` | `/MusicGenres/{Name}` | [getMusicgenresByName](services/MusicGenresService.md#operation-getmusicgenresbyname) |

### NotificationsService

Candidate: `EXPANSION`. Administrative notifications and notification type discovery.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Notifications/Admin` | [postNotificationsAdmin](services/NotificationsService.md#operation-postnotificationsadmin) |
| `GET` | `/Notifications/Types` | [getNotificationsTypes](services/NotificationsService.md#operation-getnotificationstypes) |

### OfficialRatingService

Candidate: `CORE-CANDIDATE`. Content rating lookup.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/OfficialRatings` | [getOfficialratings](services/OfficialRatingService.md#operation-getofficialratings) |

### OpenApiService

Candidate: `CORE-CANDIDATE`. API description delivery.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/openapi` | [getOpenapi](services/OpenApiService.md#operation-getopenapi) |
| `GET` | `/openapi.json` | [getOpenapiJson](services/OpenApiService.md#operation-getopenapijson) |
| `GET` | `/swagger` | [getSwagger](services/OpenApiService.md#operation-getswagger) |
| `GET` | `/swagger.json` | [getSwaggerJson](services/OpenApiService.md#operation-getswaggerjson) |

### PackageService

Candidate: `OUT-OF-SCOPE`. Emby package catalog and installation.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Packages` | [getPackages](services/PackageService.md#operation-getpackages) |
| `GET` | `/Packages/{Name}` | [getPackagesByName](services/PackageService.md#operation-getpackagesbyname) |
| `POST` | `/Packages/Installed/{Name}` | [postPackagesInstalledByName](services/PackageService.md#operation-postpackagesinstalledbyname) |
| `DELETE` | `/Packages/Installing/{Id}` | [deletePackagesInstallingById](services/PackageService.md#operation-deletepackagesinstallingbyid) |
| `POST` | `/Packages/Installing/{Id}/Delete` | [postPackagesInstallingByIdDelete](services/PackageService.md#operation-postpackagesinstallingbyiddelete) |
| `GET` | `/Packages/Updates` | [getPackagesUpdates](services/PackageService.md#operation-getpackagesupdates) |

### PartyService

Candidate: `DEFERRED`. Synchronized group playback.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Parties` | [getParties](services/PartyService.md#operation-getparties) |
| `POST` | `/Parties` | [postParties](services/PartyService.md#operation-postparties) |
| `POST` | `/Parties/{Id}/Join` | [postPartiesByIdJoin](services/PartyService.md#operation-postpartiesbyidjoin) |
| `GET` | `/Parties/Info` | [getPartiesInfo](services/PartyService.md#operation-getpartiesinfo) |
| `POST` | `/Parties/Leave` | [postPartiesLeave](services/PartyService.md#operation-postpartiesleave) |

### PersonsService

Candidate: `CORE-CANDIDATE`. People browsing and person metadata.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Persons` | [getPersons](services/PersonsService.md#operation-getpersons) |
| `GET` | `/Persons/{Name}` | [getPersonsByName](services/PersonsService.md#operation-getpersonsbyname) |

### PlaylistService

Candidate: `CORE-CANDIDATE`. Playlist contents and ordering.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Playlists` | [postPlaylists](services/PlaylistService.md#operation-postplaylists) |
| `GET` | `/Playlists/{Id}/AddToPlaylistInfo` | [getPlaylistsByIdAddtoplaylistinfo](services/PlaylistService.md#operation-getplaylistsbyidaddtoplaylistinfo) |
| `GET` | `/Playlists/{Id}/Items` | [getPlaylistsByIdItems](services/PlaylistService.md#operation-getplaylistsbyiditems) |
| `POST` | `/Playlists/{Id}/Items` | [postPlaylistsByIdItems](services/PlaylistService.md#operation-postplaylistsbyiditems) |
| `DELETE` | `/Playlists/{Id}/Items` | [deletePlaylistsByIdItems](services/PlaylistService.md#operation-deleteplaylistsbyiditems) |
| `POST` | `/Playlists/{Id}/Items/{ItemId}/Move/{NewIndex}` | [postPlaylistsByIdItemsByItemidMoveByNewindex](services/PlaylistService.md#operation-postplaylistsbyiditemsbyitemidmovebynewindex) |
| `POST` | `/Playlists/{Id}/Items/Delete` | [postPlaylistsByIdItemsDelete](services/PlaylistService.md#operation-postplaylistsbyiditemsdelete) |

### PlaystateService

Candidate: `CORE-CANDIDATE`. Playback reporting and user play-state changes.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Sessions/Playing` | [postSessionsPlaying](services/PlaystateService.md#operation-postsessionsplaying) |
| `POST` | `/Sessions/Playing/Ping` | [postSessionsPlayingPing](services/PlaystateService.md#operation-postsessionsplayingping) |
| `POST` | `/Sessions/Playing/Progress` | [postSessionsPlayingProgress](services/PlaystateService.md#operation-postsessionsplayingprogress) |
| `POST` | `/Sessions/Playing/Stopped` | [postSessionsPlayingStopped](services/PlaystateService.md#operation-postsessionsplayingstopped) |
| `POST` | `/Users/{UserId}/Items/{ItemId}/UserData` | [postUsersByUseridItemsByItemidUserdata](services/PlaystateService.md#operation-postusersbyuseriditemsbyitemiduserdata) |
| `POST` | `/Users/{UserId}/PlayedItems/{Id}` | [postUsersByUseridPlayeditemsById](services/PlaystateService.md#operation-postusersbyuseridplayeditemsbyid) |
| `DELETE` | `/Users/{UserId}/PlayedItems/{Id}` | [deleteUsersByUseridPlayeditemsById](services/PlaystateService.md#operation-deleteusersbyuseridplayeditemsbyid) |
| `POST` | `/Users/{UserId}/PlayedItems/{Id}/Delete` | [postUsersByUseridPlayeditemsByIdDelete](services/PlaystateService.md#operation-postusersbyuseridplayeditemsbyiddelete) |
| `POST` | `/Users/{UserId}/PlayingItems/{Id}` | [postUsersByUseridPlayingitemsById](services/PlaystateService.md#operation-postusersbyuseridplayingitemsbyid) |
| `DELETE` | `/Users/{UserId}/PlayingItems/{Id}` | [deleteUsersByUseridPlayingitemsById](services/PlaystateService.md#operation-deleteusersbyuseridplayingitemsbyid) |
| `POST` | `/Users/{UserId}/PlayingItems/{Id}/Delete` | [postUsersByUseridPlayingitemsByIdDelete](services/PlaystateService.md#operation-postusersbyuseridplayingitemsbyiddelete) |
| `POST` | `/Users/{UserId}/PlayingItems/{Id}/Progress` | [postUsersByUseridPlayingitemsByIdProgress](services/PlaystateService.md#operation-postusersbyuseridplayingitemsbyidprogress) |

### PluginService

Candidate: `EXPANSION`. Emby plugin lifecycle and configuration.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Plugins` | [getPlugins](services/PluginService.md#operation-getplugins) |
| `DELETE` | `/Plugins/{Id}` | [deletePluginsById](services/PluginService.md#operation-deletepluginsbyid) |
| `GET` | `/Plugins/{Id}/Configuration` | [getPluginsByIdConfiguration](services/PluginService.md#operation-getpluginsbyidconfiguration) |
| `POST` | `/Plugins/{Id}/Configuration` | [postPluginsByIdConfiguration](services/PluginService.md#operation-postpluginsbyidconfiguration) |
| `POST` | `/Plugins/{Id}/Delete` | [postPluginsByIdDelete](services/PluginService.md#operation-postpluginsbyiddelete) |
| `GET` | `/Plugins/{Id}/Thumb` | [getPluginsByIdThumb](services/PluginService.md#operation-getpluginsbyidthumb) |

### RemoteImageService

Candidate: `CORE-CANDIDATE`. External image search and image selection.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Images/Remote` | [getImagesRemote](services/RemoteImageService.md#operation-getimagesremote) |
| `GET` | `/Items/{Id}/RemoteImages` | [getItemsByIdRemoteimages](services/RemoteImageService.md#operation-getitemsbyidremoteimages) |
| `POST` | `/Items/{Id}/RemoteImages/Download` | [postItemsByIdRemoteimagesDownload](services/RemoteImageService.md#operation-postitemsbyidremoteimagesdownload) |
| `GET` | `/Items/{Id}/RemoteImages/Providers` | [getItemsByIdRemoteimagesProviders](services/RemoteImageService.md#operation-getitemsbyidremoteimagesproviders) |

### ScheduledTaskService

Candidate: `CORE-CANDIDATE`. Background task status, execution, and scheduling.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/ScheduledTasks` | [getScheduledtasks](services/ScheduledTaskService.md#operation-getscheduledtasks) |
| `GET` | `/ScheduledTasks/{Id}` | [getScheduledtasksById](services/ScheduledTaskService.md#operation-getscheduledtasksbyid) |
| `POST` | `/ScheduledTasks/{Id}/Triggers` | [postScheduledtasksByIdTriggers](services/ScheduledTaskService.md#operation-postscheduledtasksbyidtriggers) |
| `POST` | `/ScheduledTasks/Running/{Id}` | [postScheduledtasksRunningById](services/ScheduledTaskService.md#operation-postscheduledtasksrunningbyid) |
| `DELETE` | `/ScheduledTasks/Running/{Id}` | [deleteScheduledtasksRunningById](services/ScheduledTaskService.md#operation-deletescheduledtasksrunningbyid) |
| `POST` | `/ScheduledTasks/Running/{Id}/Delete` | [postScheduledtasksRunningByIdDelete](services/ScheduledTaskService.md#operation-postscheduledtasksrunningbyiddelete) |

### SessionsService

Candidate: `CORE-CANDIDATE`. API keys, authentication providers, active sessions, capabilities, and remote commands.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Auth/Keys` | [getAuthKeys](services/SessionsService.md#operation-getauthkeys) |
| `POST` | `/Auth/Keys` | [postAuthKeys](services/SessionsService.md#operation-postauthkeys) |
| `DELETE` | `/Auth/Keys/{Key}` | [deleteAuthKeysByKey](services/SessionsService.md#operation-deleteauthkeysbykey) |
| `POST` | `/Auth/Keys/{Key}/Delete` | [postAuthKeysByKeyDelete](services/SessionsService.md#operation-postauthkeysbykeydelete) |
| `GET` | `/Auth/Providers` | [getAuthProviders](services/SessionsService.md#operation-getauthproviders) |
| `GET` | `/Sessions` | [getSessions](services/SessionsService.md#operation-getsessions) |
| `POST` | `/Sessions/{Id}/Command` | [postSessionsByIdCommand](services/SessionsService.md#operation-postsessionsbyidcommand) |
| `POST` | `/Sessions/{Id}/Command/{Command}` | [postSessionsByIdCommandByCommand](services/SessionsService.md#operation-postsessionsbyidcommandbycommand) |
| `POST` | `/Sessions/{Id}/Message` | [postSessionsByIdMessage](services/SessionsService.md#operation-postsessionsbyidmessage) |
| `POST` | `/Sessions/{Id}/Playing` | [postSessionsByIdPlaying](services/SessionsService.md#operation-postsessionsbyidplaying) |
| `POST` | `/Sessions/{Id}/Playing/{Command}` | [postSessionsByIdPlayingByCommand](services/SessionsService.md#operation-postsessionsbyidplayingbycommand) |
| `POST` | `/Sessions/{Id}/System/{Command}` | [postSessionsByIdSystemByCommand](services/SessionsService.md#operation-postsessionsbyidsystembycommand) |
| `POST` | `/Sessions/{Id}/Users/{UserId}` | [postSessionsByIdUsersByUserid](services/SessionsService.md#operation-postsessionsbyidusersbyuserid) |
| `DELETE` | `/Sessions/{Id}/Users/{UserId}` | [deleteSessionsByIdUsersByUserid](services/SessionsService.md#operation-deletesessionsbyidusersbyuserid) |
| `POST` | `/Sessions/{Id}/Users/{UserId}/Delete` | [postSessionsByIdUsersByUseridDelete](services/SessionsService.md#operation-postsessionsbyidusersbyuseriddelete) |
| `POST` | `/Sessions/{Id}/Viewing` | [postSessionsByIdViewing](services/SessionsService.md#operation-postsessionsbyidviewing) |
| `POST` | `/Sessions/Capabilities` | [postSessionsCapabilities](services/SessionsService.md#operation-postsessionscapabilities) |
| `POST` | `/Sessions/Capabilities/Full` | [postSessionsCapabilitiesFull](services/SessionsService.md#operation-postsessionscapabilitiesfull) |
| `POST` | `/Sessions/Logout` | [postSessionsLogout](services/SessionsService.md#operation-postsessionslogout) |
| `GET` | `/Sessions/PlayQueue` | [getSessionsPlayqueue](services/SessionsService.md#operation-getsessionsplayqueue) |

### StudiosService

Candidate: `CORE-CANDIDATE`. Studio browsing and studio metadata.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Studios` | [getStudios](services/StudiosService.md#operation-getstudios) |
| `GET` | `/Studios/{Name}` | [getStudiosByName](services/StudiosService.md#operation-getstudiosbyname) |

### SubtitleOptionsService

Candidate: `CORE-CANDIDATE`. Reading and updating subtitle options.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Encoding/SubtitleOptions` | [getEncodingSubtitleoptions](services/SubtitleOptionsService.md#operation-getencodingsubtitleoptions) |
| `POST` | `/Encoding/SubtitleOptions` | [postEncodingSubtitleoptions](services/SubtitleOptionsService.md#operation-postencodingsubtitleoptions) |

### SubtitleService

Candidate: `CORE-CANDIDATE`. Subtitle search, provider downloads, delivery, deletion, and media attachments.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}` | [getItemsByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat](services/SubtitleService.md#operation-getitemsbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat) |
| `HEAD` | `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}` | [headItemsByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat](services/SubtitleService.md#operation-headitemsbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat) |
| `GET` | `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | [getItemsByIdByMediasourceidSubtitlesByIndexStreamByFormat](services/SubtitleService.md#operation-getitemsbyidbymediasourceidsubtitlesbyindexstreambyformat) |
| `HEAD` | `/Items/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | [headItemsByIdByMediasourceidSubtitlesByIndexStreamByFormat](services/SubtitleService.md#operation-headitemsbyidbymediasourceidsubtitlesbyindexstreambyformat) |
| `GET` | `/Items/{Id}/RemoteSearch/Subtitles/{Language}` | [getItemsByIdRemotesearchSubtitlesByLanguage](services/SubtitleService.md#operation-getitemsbyidremotesearchsubtitlesbylanguage) |
| `POST` | `/Items/{Id}/RemoteSearch/Subtitles/{SubtitleId}` | [postItemsByIdRemotesearchSubtitlesBySubtitleid](services/SubtitleService.md#operation-postitemsbyidremotesearchsubtitlesbysubtitleid) |
| `DELETE` | `/Items/{Id}/Subtitles/{Index}` | [deleteItemsByIdSubtitlesByIndex](services/SubtitleService.md#operation-deleteitemsbyidsubtitlesbyindex) |
| `POST` | `/Items/{Id}/Subtitles/{Index}/Delete` | [postItemsByIdSubtitlesByIndexDelete](services/SubtitleService.md#operation-postitemsbyidsubtitlesbyindexdelete) |
| `GET` | `/Providers/Subtitles/Subtitles/{Id}` | [getProvidersSubtitlesSubtitlesById](services/SubtitleService.md#operation-getproviderssubtitlessubtitlesbyid) |
| `GET` | `/Videos/{Id}/{MediaSourceId}/Attachments/{Index}/Stream` | [getVideosByIdByMediasourceidAttachmentsByIndexStream](services/SubtitleService.md#operation-getvideosbyidbymediasourceidattachmentsbyindexstream) |
| `GET` | `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}` | [getVideosByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat](services/SubtitleService.md#operation-getvideosbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat) |
| `HEAD` | `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/Stream.{Format}` | [headVideosByIdByMediasourceidSubtitlesByIndexByStartpositionticksStreamByFormat](services/SubtitleService.md#operation-headvideosbyidbymediasourceidsubtitlesbyindexbystartpositionticksstreambyformat) |
| `GET` | `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | [getVideosByIdByMediasourceidSubtitlesByIndexStreamByFormat](services/SubtitleService.md#operation-getvideosbyidbymediasourceidsubtitlesbyindexstreambyformat) |
| `HEAD` | `/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/Stream.{Format}` | [headVideosByIdByMediasourceidSubtitlesByIndexStreamByFormat](services/SubtitleService.md#operation-headvideosbyidbymediasourceidsubtitlesbyindexstreambyformat) |
| `DELETE` | `/Videos/{Id}/Subtitles/{Index}` | [deleteVideosByIdSubtitlesByIndex](services/SubtitleService.md#operation-deletevideosbyidsubtitlesbyindex) |
| `POST` | `/Videos/{Id}/Subtitles/{Index}/Delete` | [postVideosByIdSubtitlesByIndexDelete](services/SubtitleService.md#operation-postvideosbyidsubtitlesbyindexdelete) |

### SuggestionsService

Candidate: `EXPANSION`. Suggested item browsing.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Users/{UserId}/Suggestions` | [getUsersByUseridSuggestions](services/SuggestionsService.md#operation-getusersbyuseridsuggestions) |

### SyncService

Candidate: `DEFERRED`. Offline synchronization jobs and targets.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Sync/{ItemId}/Status` | [postSyncByItemidStatus](services/SyncService.md#operation-postsyncbyitemidstatus) |
| `DELETE` | `/Sync/{TargetId}/Items` | [deleteSyncByTargetidItems](services/SyncService.md#operation-deletesyncbytargetiditems) |
| `POST` | `/Sync/{TargetId}/Items/Delete` | [postSyncByTargetidItemsDelete](services/SyncService.md#operation-postsyncbytargetiditemsdelete) |
| `POST` | `/Sync/Data` | [postSyncData](services/SyncService.md#operation-postsyncdata) |
| `POST` | `/Sync/Items/Cancel` | [postSyncItemsCancel](services/SyncService.md#operation-postsyncitemscancel) |
| `GET` | `/Sync/Items/Ready` | [getSyncItemsReady](services/SyncService.md#operation-getsyncitemsready) |
| `GET` | `/Sync/JobItems` | [getSyncJobitems](services/SyncService.md#operation-getsyncjobitems) |
| `DELETE` | `/Sync/JobItems/{Id}` | [deleteSyncJobitemsById](services/SyncService.md#operation-deletesyncjobitemsbyid) |
| `GET` | `/Sync/JobItems/{Id}/AdditionalFiles` | [getSyncJobitemsByIdAdditionalfiles](services/SyncService.md#operation-getsyncjobitemsbyidadditionalfiles) |
| `POST` | `/Sync/JobItems/{Id}/Delete` | [postSyncJobitemsByIdDelete](services/SyncService.md#operation-postsyncjobitemsbyiddelete) |
| `POST` | `/Sync/JobItems/{Id}/Enable` | [postSyncJobitemsByIdEnable](services/SyncService.md#operation-postsyncjobitemsbyidenable) |
| `GET` | `/Sync/JobItems/{Id}/File` | [getSyncJobitemsByIdFile](services/SyncService.md#operation-getsyncjobitemsbyidfile) |
| `HEAD` | `/Sync/JobItems/{Id}/File` | [headSyncJobitemsByIdFile](services/SyncService.md#operation-headsyncjobitemsbyidfile) |
| `POST` | `/Sync/JobItems/{Id}/MarkForRemoval` | [postSyncJobitemsByIdMarkforremoval](services/SyncService.md#operation-postsyncjobitemsbyidmarkforremoval) |
| `POST` | `/Sync/JobItems/{Id}/Transferred` | [postSyncJobitemsByIdTransferred](services/SyncService.md#operation-postsyncjobitemsbyidtransferred) |
| `POST` | `/Sync/JobItems/{Id}/UnmarkForRemoval` | [postSyncJobitemsByIdUnmarkforremoval](services/SyncService.md#operation-postsyncjobitemsbyidunmarkforremoval) |
| `GET` | `/Sync/Jobs` | [getSyncJobs](services/SyncService.md#operation-getsyncjobs) |
| `POST` | `/Sync/Jobs` | [postSyncJobs](services/SyncService.md#operation-postsyncjobs) |
| `GET` | `/Sync/Jobs/{Id}` | [getSyncJobsById](services/SyncService.md#operation-getsyncjobsbyid) |
| `POST` | `/Sync/Jobs/{Id}` | [postSyncJobsById](services/SyncService.md#operation-postsyncjobsbyid) |
| `DELETE` | `/Sync/Jobs/{Id}` | [deleteSyncJobsById](services/SyncService.md#operation-deletesyncjobsbyid) |
| `POST` | `/Sync/Jobs/{Id}/Delete` | [postSyncJobsByIdDelete](services/SyncService.md#operation-postsyncjobsbyiddelete) |
| `POST` | `/Sync/OfflineActions` | [postSyncOfflineactions](services/SyncService.md#operation-postsyncofflineactions) |
| `GET` | `/Sync/Options` | [getSyncOptions](services/SyncService.md#operation-getsyncoptions) |
| `GET` | `/Sync/Targets` | [getSyncTargets](services/SyncService.md#operation-getsynctargets) |

### SystemService

Candidate: `CORE-CANDIDATE`. Server discovery, health information, logs, and lifecycle control.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/System/Endpoint` | [getSystemEndpoint](services/SystemService.md#operation-getsystemendpoint) |
| `GET` | `/System/Info` | [getSystemInfo](services/SystemService.md#operation-getsysteminfo) |
| `GET` | `/System/Info/Public` | [getSystemInfoPublic](services/SystemService.md#operation-getsysteminfopublic) |
| `GET` | `/System/Logs/{Name}` | [getSystemLogsByName](services/SystemService.md#operation-getsystemlogsbyname) |
| `GET` | `/System/Logs/{Name}/Lines` | [getSystemLogsByNameLines](services/SystemService.md#operation-getsystemlogsbynamelines) |
| `GET` | `/System/Logs/Query` | [getSystemLogsQuery](services/SystemService.md#operation-getsystemlogsquery) |
| `GET` | `/System/Ping` | [getSystemPing](services/SystemService.md#operation-getsystemping) |
| `POST` | `/System/Ping` | [postSystemPing](services/SystemService.md#operation-postsystemping) |
| `HEAD` | `/System/Ping` | [headSystemPing](services/SystemService.md#operation-headsystemping) |
| `GET` | `/System/ReleaseNotes` | [getSystemReleasenotes](services/SystemService.md#operation-getsystemreleasenotes) |
| `GET` | `/System/ReleaseNotes/Versions` | [getSystemReleasenotesVersions](services/SystemService.md#operation-getsystemreleasenotesversions) |
| `POST` | `/System/Restart` | [postSystemRestart](services/SystemService.md#operation-postsystemrestart) |
| `POST` | `/System/Shutdown` | [postSystemShutdown](services/SystemService.md#operation-postsystemshutdown) |
| `GET` | `/System/WakeOnLanInfo` | [getSystemWakeonlaninfo](services/SystemService.md#operation-getsystemwakeonlaninfo) |

### TagService

Candidate: `CORE-CANDIDATE`. Browse facets for tags, codecs, containers, item types, languages, years, and prefixes; tag changes.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Artists/Prefixes` | [getArtistsPrefixes](services/TagService.md#operation-getartistsprefixes) |
| `GET` | `/AudioCodecs` | [getAudiocodecs](services/TagService.md#operation-getaudiocodecs) |
| `GET` | `/AudioLayouts` | [getAudiolayouts](services/TagService.md#operation-getaudiolayouts) |
| `GET` | `/Containers` | [getContainers](services/TagService.md#operation-getcontainers) |
| `GET` | `/ExtendedVideoTypes` | [getExtendedvideotypes](services/TagService.md#operation-getextendedvideotypes) |
| `POST` | `/Items/{Id}/Tags/Add` | [postItemsByIdTagsAdd](services/TagService.md#operation-postitemsbyidtagsadd) |
| `POST` | `/Items/{Id}/Tags/Delete` | [postItemsByIdTagsDelete](services/TagService.md#operation-postitemsbyidtagsdelete) |
| `GET` | `/Items/Prefixes` | [getItemsPrefixes](services/TagService.md#operation-getitemsprefixes) |
| `GET` | `/ItemTypes` | [getItemtypes](services/TagService.md#operation-getitemtypes) |
| `GET` | `/StreamLanguages` | [getStreamlanguages](services/TagService.md#operation-getstreamlanguages) |
| `GET` | `/SubtitleCodecs` | [getSubtitlecodecs](services/TagService.md#operation-getsubtitlecodecs) |
| `GET` | `/Tags` | [getTags](services/TagService.md#operation-gettags) |
| `GET` | `/VideoCodecs` | [getVideocodecs](services/TagService.md#operation-getvideocodecs) |
| `GET` | `/Years` | [getYears](services/TagService.md#operation-getyears) |

### ToneMapOptionsService

Candidate: `EXPANSION`. Reading and updating full and public tone mapping options.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Encoding/FullToneMapOptions` | [getEncodingFulltonemapoptions](services/ToneMapOptionsService.md#operation-getencodingfulltonemapoptions) |
| `POST` | `/Encoding/FullToneMapOptions` | [postEncodingFulltonemapoptions](services/ToneMapOptionsService.md#operation-postencodingfulltonemapoptions) |
| `GET` | `/Encoding/PublicToneMapOptions` | [getEncodingPublictonemapoptions](services/ToneMapOptionsService.md#operation-getencodingpublictonemapoptions) |
| `POST` | `/Encoding/PublicToneMapOptions` | [postEncodingPublictonemapoptions](services/ToneMapOptionsService.md#operation-postencodingpublictonemapoptions) |

### TrailersService

Candidate: `EXPANSION`. Trailer browsing.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Trailers` | [getTrailers](services/TrailersService.md#operation-gettrailers) |

### TvShowsService

Candidate: `CORE-CANDIDATE`. Season, episode, next-up, missing, and upcoming queries.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Shows/{Id}/Episodes` | [getShowsByIdEpisodes](services/TvShowsService.md#operation-getshowsbyidepisodes) |
| `GET` | `/Shows/{Id}/Seasons` | [getShowsByIdSeasons](services/TvShowsService.md#operation-getshowsbyidseasons) |
| `GET` | `/Shows/Missing` | [getShowsMissing](services/TvShowsService.md#operation-getshowsmissing) |
| `GET` | `/Shows/NextUp` | [getShowsNextup](services/TvShowsService.md#operation-getshowsnextup) |
| `GET` | `/Shows/Upcoming` | [getShowsUpcoming](services/TvShowsService.md#operation-getshowsupcoming) |

### UniversalAudioService

Candidate: `CORE-CANDIDATE`. Client-directed audio delivery negotiation.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Audio/{Id}/universal` | [getAudioByIdUniversal](services/UniversalAudioService.md#operation-getaudiobyiduniversal) |
| `HEAD` | `/Audio/{Id}/universal` | [headAudioByIdUniversal](services/UniversalAudioService.md#operation-headaudiobyiduniversal) |
| `GET` | `/Audio/{Id}/universal.{Container}` | [getAudioByIdUniversalByContainer](services/UniversalAudioService.md#operation-getaudiobyiduniversalbycontainer) |
| `HEAD` | `/Audio/{Id}/universal.{Container}` | [headAudioByIdUniversalByContainer](services/UniversalAudioService.md#operation-headaudiobyiduniversalbycontainer) |

### UserLibraryService

Candidate: `CORE-CANDIDATE`. User item data, browsing, ratings, favorites, shared-item access, and additional video parts.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `POST` | `/Items/{Id}/MakePrivate` | [postItemsByIdMakeprivate](services/UserLibraryService.md#operation-postitemsbyidmakeprivate) |
| `POST` | `/Items/{Id}/MakePublic` | [postItemsByIdMakepublic](services/UserLibraryService.md#operation-postitemsbyidmakepublic) |
| `POST` | `/Items/Access` | [postItemsAccess](services/UserLibraryService.md#operation-postitemsaccess) |
| `POST` | `/Items/Shared/Leave` | [postItemsSharedLeave](services/UserLibraryService.md#operation-postitemssharedleave) |
| `GET` | `/LiveTv/Programs/{Id}` | [getLivetvProgramsById](services/UserLibraryService.md#operation-getlivetvprogramsbyid) |
| `POST` | `/Users/{UserId}/FavoriteItems/{Id}` | [postUsersByUseridFavoriteitemsById](services/UserLibraryService.md#operation-postusersbyuseridfavoriteitemsbyid) |
| `DELETE` | `/Users/{UserId}/FavoriteItems/{Id}` | [deleteUsersByUseridFavoriteitemsById](services/UserLibraryService.md#operation-deleteusersbyuseridfavoriteitemsbyid) |
| `POST` | `/Users/{UserId}/FavoriteItems/{Id}/Delete` | [postUsersByUseridFavoriteitemsByIdDelete](services/UserLibraryService.md#operation-postusersbyuseridfavoriteitemsbyiddelete) |
| `GET` | `/Users/{UserId}/Items/{Id}` | [getUsersByUseridItemsById](services/UserLibraryService.md#operation-getusersbyuseriditemsbyid) |
| `POST` | `/Users/{UserId}/Items/{Id}/HideFromResume` | [postUsersByUseridItemsByIdHidefromresume](services/UserLibraryService.md#operation-postusersbyuseriditemsbyidhidefromresume) |
| `GET` | `/Users/{UserId}/Items/{Id}/Intros` | [getUsersByUseridItemsByIdIntros](services/UserLibraryService.md#operation-getusersbyuseriditemsbyidintros) |
| `GET` | `/Users/{UserId}/Items/{Id}/LocalTrailers` | [getUsersByUseridItemsByIdLocaltrailers](services/UserLibraryService.md#operation-getusersbyuseriditemsbyidlocaltrailers) |
| `POST` | `/Users/{UserId}/Items/{Id}/Rating` | [postUsersByUseridItemsByIdRating](services/UserLibraryService.md#operation-postusersbyuseriditemsbyidrating) |
| `DELETE` | `/Users/{UserId}/Items/{Id}/Rating` | [deleteUsersByUseridItemsByIdRating](services/UserLibraryService.md#operation-deleteusersbyuseriditemsbyidrating) |
| `POST` | `/Users/{UserId}/Items/{Id}/Rating/Delete` | [postUsersByUseridItemsByIdRatingDelete](services/UserLibraryService.md#operation-postusersbyuseriditemsbyidratingdelete) |
| `GET` | `/Users/{UserId}/Items/{Id}/SpecialFeatures` | [getUsersByUseridItemsByIdSpecialfeatures](services/UserLibraryService.md#operation-getusersbyuseriditemsbyidspecialfeatures) |
| `GET` | `/Users/{UserId}/Items/Latest` | [getUsersByUseridItemsLatest](services/UserLibraryService.md#operation-getusersbyuseriditemslatest) |
| `GET` | `/Users/{UserId}/Items/Root` | [getUsersByUseridItemsRoot](services/UserLibraryService.md#operation-getusersbyuseriditemsroot) |
| `GET` | `/Videos/{Id}/AdditionalParts` | [getVideosByIdAdditionalparts](services/UserLibraryService.md#operation-getvideosbyidadditionalparts) |

### UserNotificationsService

Candidate: `EXPANSION`. Notification service defaults and delivery test requests.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Notifications/Services/Defaults` | [getNotificationsServicesDefaults](services/UserNotificationsService.md#operation-getnotificationsservicesdefaults) |
| `POST` | `/Notifications/Services/Test` | [postNotificationsServicesTest](services/UserNotificationsService.md#operation-postnotificationsservicestest) |

### UserService

Candidate: `CORE-CANDIDATE`. User authentication, accounts, configuration, and policy.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Users/{Id}` | [getUsersById](services/UserService.md#operation-getusersbyid) |
| `POST` | `/Users/{Id}` | [postUsersById](services/UserService.md#operation-postusersbyid) |
| `DELETE` | `/Users/{Id}` | [deleteUsersById](services/UserService.md#operation-deleteusersbyid) |
| `POST` | `/Users/{Id}/Authenticate` | [postUsersByIdAuthenticate](services/UserService.md#operation-postusersbyidauthenticate) |
| `POST` | `/Users/{Id}/Configuration` | [postUsersByIdConfiguration](services/UserService.md#operation-postusersbyidconfiguration) |
| `POST` | `/Users/{Id}/Configuration/Partial` | [postUsersByIdConfigurationPartial](services/UserService.md#operation-postusersbyidconfigurationpartial) |
| `POST` | `/Users/{Id}/Delete` | [postUsersByIdDelete](services/UserService.md#operation-postusersbyiddelete) |
| `POST` | `/Users/{Id}/Password` | [postUsersByIdPassword](services/UserService.md#operation-postusersbyidpassword) |
| `POST` | `/Users/{Id}/Policy` | [postUsersByIdPolicy](services/UserService.md#operation-postusersbyidpolicy) |
| `DELETE` | `/Users/{Id}/TrackSelections/{TrackType}` | [deleteUsersByIdTrackselectionsByTracktype](services/UserService.md#operation-deleteusersbyidtrackselectionsbytracktype) |
| `POST` | `/Users/{Id}/TrackSelections/{TrackType}/Delete` | [postUsersByIdTrackselectionsByTracktypeDelete](services/UserService.md#operation-postusersbyidtrackselectionsbytracktypedelete) |
| `GET` | `/Users/{UserId}/TypedSettings/{Key}` | [getUsersByUseridTypedsettingsByKey](services/UserService.md#operation-getusersbyuseridtypedsettingsbykey) |
| `POST` | `/Users/{UserId}/TypedSettings/{Key}` | [postUsersByUseridTypedsettingsByKey](services/UserService.md#operation-postusersbyuseridtypedsettingsbykey) |
| `POST` | `/Users/AuthenticateByName` | [postUsersAuthenticatebyname](services/UserService.md#operation-postusersauthenticatebyname) |
| `POST` | `/Users/ForgotPassword` | [postUsersForgotpassword](services/UserService.md#operation-postusersforgotpassword) |
| `POST` | `/Users/ForgotPassword/Pin` | [postUsersForgotpasswordPin](services/UserService.md#operation-postusersforgotpasswordpin) |
| `GET` | `/Users/ItemAccess` | [getUsersItemaccess](services/UserService.md#operation-getusersitemaccess) |
| `POST` | `/Users/New` | [postUsersNew](services/UserService.md#operation-postusersnew) |
| `GET` | `/Users/Prefixes` | [getUsersPrefixes](services/UserService.md#operation-getusersprefixes) |
| `GET` | `/Users/Public` | [getUsersPublic](services/UserService.md#operation-getuserspublic) |
| `GET` | `/Users/Query` | [getUsersQuery](services/UserService.md#operation-getusersquery) |

### UserViewsService

Candidate: `CORE-CANDIDATE`. User library views and grouping.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Users/{UserId}/Views` | [getUsersByUseridViews](services/UserViewsService.md#operation-getusersbyuseridviews) |

### VideoHlsService

Candidate: `CORE-CANDIDATE`. Audio and video segment retrieval through legacy HLS routes.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Audio/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}` | [getAudioByIdHlsByPlaylistidBySegmentidBySegmentcontainer](services/VideoHlsService.md#operation-getaudiobyidhlsbyplaylistidbysegmentidbysegmentcontainer) |
| `GET` | `/Videos/{Id}/hls/{PlaylistId}/{SegmentId}.{SegmentContainer}` | [getVideosByIdHlsByPlaylistidBySegmentidBySegmentcontainer](services/VideoHlsService.md#operation-getvideosbyidhlsbyplaylistidbysegmentidbysegmentcontainer) |

### VideoService

Candidate: `CORE-CANDIDATE`. Progressive video delivery.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/Videos/{Id}/{StreamFileName}` | [getVideosByIdByStreamfilename](services/VideoService.md#operation-getvideosbyidbystreamfilename) |
| `HEAD` | `/Videos/{Id}/{StreamFileName}` | [headVideosByIdByStreamfilename](services/VideoService.md#operation-headvideosbyidbystreamfilename) |
| `GET` | `/Videos/{Id}/stream` | [getVideosByIdStream](services/VideoService.md#operation-getvideosbyidstream) |
| `HEAD` | `/Videos/{Id}/stream` | [headVideosByIdStream](services/VideoService.md#operation-headvideosbyidstream) |
| `GET` | `/Videos/{Id}/stream.{Container}` | [getVideosByIdStreamByContainer](services/VideoService.md#operation-getvideosbyidstreambycontainer) |
| `HEAD` | `/Videos/{Id}/stream.{Container}` | [headVideosByIdStreamByContainer](services/VideoService.md#operation-headvideosbyidstreambycontainer) |

### VideosService

Candidate: `CORE-CANDIDATE`. Merging video versions and removing alternate sources.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `DELETE` | `/Videos/{Id}/AlternateSources` | [deleteVideosByIdAlternatesources](services/VideosService.md#operation-deletevideosbyidalternatesources) |
| `POST` | `/Videos/{Id}/AlternateSources/Delete` | [postVideosByIdAlternatesourcesDelete](services/VideosService.md#operation-postvideosbyidalternatesourcesdelete) |
| `POST` | `/Videos/MergeVersions` | [postVideosMergeversions](services/VideosService.md#operation-postvideosmergeversions) |

### WebAppService

Candidate: `OUT-OF-SCOPE`. Emby web-application configuration pages and localization strings.

| Method | Path | Operation ID and local reference |
| --- | --- | --- |
| `GET` | `/web/ConfigurationPage` | [getWebConfigurationpage](services/WebAppService.md#operation-getwebconfigurationpage) |
| `GET` | `/web/ConfigurationPages` | [getWebConfigurationpages](services/WebAppService.md#operation-getwebconfigurationpages) |
| `GET` | `/web/strings` | [getWebStrings](services/WebAppService.md#operation-getwebstrings) |
| `GET` | `/web/stringset` | [getWebStringset](services/WebAppService.md#operation-getwebstringset) |

## Regeneration

The exporter requires PowerShell 7 and reads only the pinned local snapshot. It writes this catalog,
`inventory.json`, `models.md`, and one file per service. It does not fetch a newer release or perform
tests, schema validation, build verification, runtime probes, or compatibility verification.

```powershell
./scripts/docs/export-emby-reference.ps1
```

Generated files are a documentation projection. Hand-maintained scope decisions belong in
`implementation-scope.md` so regeneration cannot silently promote routes into the MVP.
