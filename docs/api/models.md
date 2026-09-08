# Emby data model reference

This offline index covers all **333 definitions** in the [pinned Swagger snapshot](../sources/emby-sdk-openapi.snapshot.json).
It lists source field names, types, required declarations, defaults, enums, and constraints without
duplicating repetitive source descriptions. The complete nested schema and vendor extensions remain
available in that local snapshot at each JSON Pointer. [Return to the API catalog](catalog.md).

## Interpretation and Go implementation notes

- Preserve the exact PascalCase JSON field names and case-sensitive enum strings at the compatibility boundary.
- A missing object-level `required` array means the source does not require a field; it does not prove that
  real clients tolerate its omission. Presence, `null`, empty values, and endpoint-specific projections need fixtures.
- Use Go `int64` for tick values and source `int64` fields. Preserve IDs as source-compatible strings when
  declared as strings. Do not expose database keys merely because an identifier looks numeric.
- Date-time format labels and numeric formats are source metadata, not guarantees of exact serialization.
- Separate wire DTOs from storage entities. A large response model such as `BaseItemDto` is not a database schema.
- Linked references show the immediate schema graph. Self-references and nested references are intentionally
  not expanded recursively; use the model links and source pointer to navigate them offline.
- A model listed here may belong to a deferred or excluded service. Inclusion does not commit the project
  to implementing that feature, its full model, or every source field.

## Definition index

| Definition | Source type | Declared properties |
| --- | --- | ---: |
| [AccessSchedule](#model-accessschedule) | object | 3 |
| [Actions.PostbackAction](#model-actions-postbackaction) | object | 3 |
| [ActivityLogEntry](#model-activitylogentry) | object | 10 |
| [AlbumInfo](#model-albuminfo) | object | 14 |
| [AllThemeMediaResult](#model-allthememediaresult) | object | 3 |
| [Api.AddAdminNotification](#model-api-addadminnotification) | object | 1 |
| [Api.AvailableRecordingOptions](#model-api-availablerecordingoptions) | object | 3 |
| [Api.BaseItemsRequest](#model-api-baseitemsrequest) | object | 39 |
| [Api.ConfigurationPageInfo](#model-api-configurationpageinfo) | object | 13 |
| [Api.EpgRow](#model-api-epgrow) | object | 2 |
| [Api.ListingProviderTypeInfo](#model-api-listingprovidertypeinfo) | object | 3 |
| [Api.NameIdDescriptionPair](#model-api-nameiddescriptionpair) | object | 3 |
| [Api.OnPlaybackProgress](#model-api-onplaybackprogress) | object | 6 |
| [Api.SetChannelDisabled](#model-api-setchanneldisabled) | object | 3 |
| [Api.SetChannelMapping](#model-api-setchannelmapping) | object | 2 |
| [Api.SetChannelSortIndex](#model-api-setchannelsortindex) | object | 3 |
| [Api.TagItem](#model-api-tagitem) | object | 2 |
| [ArtistInfo](#model-artistinfo) | object | 12 |
| [Attributes.SimpleCondition](#model-attributes-simplecondition) | string | 0 |
| [Attributes.ValueCondition](#model-attributes-valuecondition) | string | 0 |
| [AuthenticateUser](#model-authenticateuser) | object | 1 |
| [AuthenticateUserByName](#model-authenticateuserbyname) | object | 2 |
| [Authentication.AuthenticationResult](#model-authentication-authenticationresult) | object | 4 |
| [BaseItemDto](#model-baseitemdto) | object | 161 |
| [BaseItemPerson](#model-baseitemperson) | object | 5 |
| [BaseRefreshRequest](#model-baserefreshrequest) | object | 1 |
| [BitRate](#model-bitrate) | object | 3 |
| [BookInfo](#model-bookinfo) | object | 13 |
| [Branding.BrandingOptions](#model-branding-brandingoptions) | object | 2 |
| [ChannelManagementInfo](#model-channelmanagementinfo) | object | 2 |
| [ChapterInfo](#model-chapterinfo) | object | 5 |
| [ClientCapabilities](#model-clientcapabilities) | object | 9 |
| [CodecConfiguration](#model-codecconfiguration) | object | 3 |
| [CodecDirections](#model-codecdirections) | string | 0 |
| [CodecKinds](#model-codeckinds) | string | 0 |
| [CodecProfile](#model-codecprofile) | object | 5 |
| [CodecType](#model-codectype) | string | 0 |
| [Collections.CollectionCreationResult](#model-collections-collectioncreationresult) | object | 2 |
| [ColorFormats](#model-colorformats) | string | 0 |
| [Common.EditorTypes](#model-common-editortypes) | string | 0 |
| [Common.Interfaces.ICodecDeviceCapabilities](#model-common-interfaces-icodecdevicecapabilities) | object | 5 |
| [Common.Interfaces.ICodecDeviceInfo](#model-common-interfaces-icodecdeviceinfo) | object | 15 |
| [Common.Plugins.IPlugin](#model-common-plugins-iplugin) | object | 6 |
| [Conditions.PropertyCondition](#model-conditions-propertycondition) | object | 6 |
| [Conditions.PropertyConditionType](#model-conditions-propertyconditiontype) | string | 0 |
| [Configuration.ToneMapping.ToneMapOptionsVisibility](#model-configuration-tonemapping-tonemapoptionsvisibility) | object | 11 |
| [Connect.ConnectAuthenticationExchangeResult](#model-connect-connectauthenticationexchangeresult) | object | 2 |
| [Connect.UserLinkResult](#model-connect-userlinkresult) | object | 3 |
| [Connect.UserLinkType](#model-connect-userlinktype) | string | 0 |
| [ContainerProfile](#model-containerprofile) | object | 3 |
| [ContentSection](#model-contentsection) | object | 14 |
| [CreateUserByName](#model-createuserbyname) | object | 3 |
| [DayOfWeek](#model-dayofweek) | string | 0 |
| [DefaultDirectoryBrowserInfo](#model-defaultdirectorybrowserinfo) | object | 1 |
| [DeviceProfile](#model-deviceprofile) | object | 13 |
| [Devices.ContentUploadHistory](#model-devices-contentuploadhistory) | object | 2 |
| [Devices.DeviceInfo](#model-devices-deviceinfo) | object | 11 |
| [Devices.DeviceOptions](#model-devices-deviceoptions) | object | 1 |
| [Devices.LocalFileInfo](#model-devices-localfileinfo) | object | 5 |
| [DirectPlayProfile](#model-directplayprofile) | object | 4 |
| [DisplayPreferences](#model-displaypreferences) | object | 5 |
| [Dlna.Profiles.DeviceIdentification](#model-dlna-profiles-deviceidentification) | object | 10 |
| [Dlna.Profiles.DeviceProfileType](#model-dlna-profiles-deviceprofiletype) | string | 0 |
| [Dlna.Profiles.DlnaProfile](#model-dlna-profiles-dlnaprofile) | object | 40 |
| [Dlna.Profiles.HeaderMatchType](#model-dlna-profiles-headermatchtype) | string | 0 |
| [Dlna.Profiles.HttpHeaderInfo](#model-dlna-profiles-httpheaderinfo) | object | 3 |
| [Dlna.Profiles.ProtocolInfoDetection](#model-dlna-profiles-protocolinfodetection) | object | 3 |
| [DlnaProfileType](#model-dlnaprofiletype) | string | 0 |
| [Drawing.ImageOrientation](#model-drawing-imageorientation) | string | 0 |
| [DynamicDayOfWeek](#model-dynamicdayofweek) | string | 0 |
| [EditObjectContainer](#model-editobjectcontainer) | object | 4 |
| [Editors.EditorBase](#model-editors-editorbase) | object | 10 |
| [Editors.EditorButtonItem](#model-editors-editorbuttonitem) | object | 10 |
| [Editors.EditorRoot](#model-editors-editorroot) | object | 14 |
| [EncodingContext](#model-encodingcontext) | string | 0 |
| [Entities.ItemImageInfo](#model-entities-itemimageinfo) | object | 6 |
| [Entities.User](#model-entities-user) | object | 20 |
| [Enums.UICommandType](#model-enums-uicommandtype) | string | 0 |
| [Enums.UIViewType](#model-enums-uiviewtype) | string | 0 |
| [ExtendedVideoSubTypes](#model-extendedvideosubtypes) | string | 0 |
| [ExtendedVideoTypes](#model-extendedvideotypes) | string | 0 |
| [ExternalIdInfo](#model-externalidinfo) | object | 5 |
| [ExternalUrl](#model-externalurl) | object | 2 |
| [FeatureInfo](#model-featureinfo) | object | 3 |
| [FeatureType](#model-featuretype) | string | 0 |
| [ForgotPassword](#model-forgotpassword) | object | 1 |
| [ForgotPasswordAction](#model-forgotpasswordaction) | string | 0 |
| [ForgotPasswordPin](#model-forgotpasswordpin) | object | 1 |
| [ForgotPasswordResult](#model-forgotpasswordresult) | object | 3 |
| [GameInfo](#model-gameinfo) | object | 12 |
| [GeneralCommand](#model-generalcommand) | object | 3 |
| [GenericEdit.IEditObjectContainer](#model-genericedit-ieditobjectcontainer) | object | 3 |
| [GetDirectoryContents](#model-getdirectorycontents) | object | 2 |
| [Globalization.CountryInfo](#model-globalization-countryinfo) | object | 5 |
| [Globalization.CultureDto](#model-globalization-culturedto) | object | 6 |
| [Globalization.LocalizatonOption](#model-globalization-localizatonoption) | object | 2 |
| [ImageInfo](#model-imageinfo) | object | 7 |
| [ImageOption](#model-imageoption) | object | 3 |
| [ImageProviderInfo](#model-imageproviderinfo) | object | 2 |
| [Images.BaseDownloadRemoteImage](#model-images-basedownloadremoteimage) | object | 1 |
| [ImageSavingConvention](#model-imagesavingconvention) | string | 0 |
| [ImageType](#model-imagetype) | string | 0 |
| [InstallationInfo](#model-installationinfo) | object | 6 |
| [IO.FileSystemEntryInfo](#model-io-filesystementryinfo) | object | 3 |
| [IO.FileSystemEntryType](#model-io-filesystementrytype) | string | 0 |
| [ItemCounts](#model-itemcounts) | object | 14 |
| [ItemFileInfo](#model-itemfileinfo) | object | 5 |
| [ItemFileType](#model-itemfiletype) | string | 0 |
| [ItemLookupInfo](#model-itemlookupinfo) | object | 12 |
| [LevelInformation](#model-levelinformation) | object | 9 |
| [Library.AddMediaPath](#model-library-addmediapath) | object | 4 |
| [Library.AddVirtualFolder](#model-library-addvirtualfolder) | object | 5 |
| [Library.DeleteInfo](#model-library-deleteinfo) | object | 1 |
| [Library.ItemLinkType](#model-library-itemlinktype) | string | 0 |
| [Library.MediaFolder](#model-library-mediafolder) | object | 5 |
| [Library.MediaUpdateInfo](#model-library-mediaupdateinfo) | object | 2 |
| [Library.PostUpdatedMedia](#model-library-postupdatedmedia) | object | 1 |
| [Library.RemoveMediaPath](#model-library-removemediapath) | object | 3 |
| [Library.RemoveVirtualFolder](#model-library-removevirtualfolder) | object | 2 |
| [Library.RenameVirtualFolder](#model-library-renamevirtualfolder) | object | 2 |
| [Library.SubFolder](#model-library-subfolder) | object | 4 |
| [Library.UpdateLibraryOptions](#model-library-updatelibraryoptions) | object | 2 |
| [Library.UpdateMediaPath](#model-library-updatemediapath) | object | 2 |
| [Library.UserCopyOptions](#model-library-usercopyoptions) | string | 0 |
| [LibraryOptionInfo](#model-libraryoptioninfo) | object | 4 |
| [LibraryOptions](#model-libraryoptions) | object | 65 |
| [LibraryOptionsResult](#model-libraryoptionsresult) | object | 6 |
| [LibraryTypeOptions](#model-librarytypeoptions) | object | 5 |
| [LinkedItemInfo](#model-linkediteminfo) | object | 3 |
| [LiveStreamRequest](#model-livestreamrequest) | object | 16 |
| [LiveStreamResponse](#model-livestreamresponse) | object | 1 |
| [LiveTv.ChannelType](#model-livetv-channeltype) | string | 0 |
| [LiveTv.GuideInfo](#model-livetv-guideinfo) | object | 2 |
| [LiveTv.KeepUntil](#model-livetv-keepuntil) | string | 0 |
| [LiveTv.KeywordInfo](#model-livetv-keywordinfo) | object | 2 |
| [LiveTv.KeywordType](#model-livetv-keywordtype) | string | 0 |
| [LiveTv.ListingsProviderInfo](#model-livetv-listingsproviderinfo) | object | 22 |
| [LiveTv.LiveTvInfo](#model-livetv-livetvinfo) | object | 2 |
| [LiveTv.RecordingStatus](#model-livetv-recordingstatus) | string | 0 |
| [LiveTv.SeriesTimerInfo](#model-livetv-seriestimerinfo) | object | 27 |
| [LiveTv.SeriesTimerInfoDto](#model-livetv-seriestimerinfodto) | object | 38 |
| [LiveTv.TimerInfoDto](#model-livetv-timerinfodto) | object | 26 |
| [LiveTv.TimerType](#model-livetv-timertype) | string | 0 |
| [LiveTv.TunerHostInfo](#model-livetv-tunerhostinfo) | object | 18 |
| [LocationType](#model-locationtype) | string | 0 |
| [LogFile](#model-logfile) | object | 4 |
| [Logging.LogSeverity](#model-logging-logseverity) | string | 0 |
| [MarkerType](#model-markertype) | string | 0 |
| [MBBackup.Api.AllBackupsInfo](#model-mbbackup-api-allbackupsinfo) | object | 2 |
| [MBBackup.Api.DataRestoreOptions](#model-mbbackup-api-datarestoreoptions) | object | 1 |
| [MBBackup.Api.RestoreOptions](#model-mbbackup-api-restoreoptions) | object | 2 |
| [MBBackup.Api.UserRestoreInfo](#model-mbbackup-api-userrestoreinfo) | object | 2 |
| [MBBackup.BackupInfo](#model-mbbackup-backupinfo) | object | 7 |
| [MediaEncoding.CodecParameterContext](#model-mediaencoding-codecparametercontext) | string | 0 |
| [MediaPathInfo](#model-mediapathinfo) | object | 4 |
| [MediaProtocol](#model-mediaprotocol) | string | 0 |
| [MediaSourceInfo](#model-mediasourceinfo) | object | 47 |
| [MediaSourceType](#model-mediasourcetype) | string | 0 |
| [MediaStream](#model-mediastream) | object | 55 |
| [MediaStreamType](#model-mediastreamtype) | string | 0 |
| [MediaUrl](#model-mediaurl) | object | 2 |
| [MetadataEditorInfo](#model-metadataeditorinfo) | object | 5 |
| [MetadataFeatures](#model-metadatafeatures) | string | 0 |
| [MetadataFields](#model-metadatafields) | string | 0 |
| [MetadataRefreshMode](#model-metadatarefreshmode) | string | 0 |
| [MovieInfo](#model-movieinfo) | object | 12 |
| [MusicVideoInfo](#model-musicvideoinfo) | object | 13 |
| [NameIdPair](#model-nameidpair) | object | 2 |
| [NameLongIdPair](#model-namelongidpair) | object | 2 |
| [NameValuePair](#model-namevaluepair) | object | 2 |
| [Net.EndPointInfo](#model-net-endpointinfo) | object | 2 |
| [Net.Sockets.AddressFamily](#model-net-sockets-addressfamily) | string | 0 |
| [NotificationCategoryInfo](#model-notificationcategoryinfo) | object | 3 |
| [Notifications.NotificationLevel](#model-notifications-notificationlevel) | string | 0 |
| [NotificationTypeInfo](#model-notificationtypeinfo) | object | 4 |
| [OperatingSystem](#model-operatingsystem) | string | 0 |
| [PackageInfo](#model-packageinfo) | object | 23 |
| [PackageTargetSystem](#model-packagetargetsystem) | string | 0 |
| [PackageVersionClass](#model-packageversionclass) | string | 0 |
| [PackageVersionInfo](#model-packageversioninfo) | object | 12 |
| [ParentalRating](#model-parentalrating) | object | 2 |
| [PathSubstitution](#model-pathsubstitution) | object | 2 |
| [Persistence.IntroDebugInfo](#model-persistence-introdebuginfo) | object | 4 |
| [PersonLookupInfo](#model-personlookupinfo) | object | 12 |
| [PersonType](#model-persontype) | string | 0 |
| [PinRedeemResult](#model-pinredeemresult) | object | 2 |
| [PlaybackErrorCode](#model-playbackerrorcode) | string | 0 |
| [PlaybackInfoRequest](#model-playbackinforequest) | object | 19 |
| [PlaybackInfoResponse](#model-playbackinforesponse) | object | 3 |
| [PlaybackProgressInfo](#model-playbackprogressinfo) | object | 30 |
| [PlaybackStartInfo](#model-playbackstartinfo) | object | 30 |
| [PlaybackStopInfo](#model-playbackstopinfo) | object | 14 |
| [PlayCommand](#model-playcommand) | string | 0 |
| [PlayerStateInfo](#model-playerstateinfo) | object | 16 |
| [Playlists.AddToPlaylistInfo](#model-playlists-addtoplaylistinfo) | object | 2 |
| [Playlists.AddToPlaylistResult](#model-playlists-addtoplaylistresult) | object | 2 |
| [Playlists.PlaylistCreationResult](#model-playlists-playlistcreationresult) | object | 3 |
| [PlayMethod](#model-playmethod) | string | 0 |
| [PlayRequest](#model-playrequest) | object | 5 |
| [PlaystateCommand](#model-playstatecommand) | string | 0 |
| [PlaystateRequest](#model-playstaterequest) | object | 3 |
| [Plugins.ConfigurationPageType](#model-plugins-configurationpagetype) | string | 0 |
| [Plugins.PluginInfo](#model-plugins-plugininfo) | object | 6 |
| [ProcessRun.Metrics.ProcessMetricPoint](#model-processrun-metrics-processmetricpoint) | object | 4 |
| [ProcessRun.Metrics.ProcessStatistics](#model-processrun-metrics-processstatistics) | object | 5 |
| [ProfileCondition](#model-profilecondition) | object | 4 |
| [ProfileConditionType](#model-profileconditiontype) | string | 0 |
| [ProfileConditionValue](#model-profileconditionvalue) | string | 0 |
| [ProfileInformation](#model-profileinformation) | object | 5 |
| [ProfileLevelInformation](#model-profilelevelinformation) | object | 2 |
| [ProgressEvent](#model-progressevent) | string | 0 |
| [ProviderIdDictionary](#model-provideriddictionary) | object | 0 |
| [ProxyHeaderMode](#model-proxyheadermode) | string | 0 |
| [PublicSystemInfo](#model-publicsysteminfo) | object | 7 |
| [QueryResult_ActivityLogEntry](#model-queryresult_activitylogentry) | object | 2 |
| [QueryResult_Api.EpgRow](#model-queryresult_api-epgrow) | object | 2 |
| [QueryResult_BaseItemDto](#model-queryresult_baseitemdto) | object | 2 |
| [QueryResult_ChannelManagementInfo](#model-queryresult_channelmanagementinfo) | object | 2 |
| [QueryResult_Devices.DeviceInfo](#model-queryresult_devices-deviceinfo) | object | 2 |
| [QueryResult_LiveTv.SeriesTimerInfoDto](#model-queryresult_livetv-seriestimerinfodto) | object | 2 |
| [QueryResult_LiveTv.TimerInfoDto](#model-queryresult_livetv-timerinfodto) | object | 2 |
| [QueryResult_LogFile](#model-queryresult_logfile) | object | 2 |
| [QueryResult_String](#model-queryresult_string) | object | 2 |
| [QueryResult_SyncJob](#model-queryresult_syncjob) | object | 2 |
| [QueryResult_SyncJobItem](#model-queryresult_syncjobitem) | object | 2 |
| [QueryResult_UserDto](#model-queryresult_userdto) | object | 2 |
| [QueryResult_UserLibrary.OfficialRatingItem](#model-queryresult_userlibrary-officialratingitem) | object | 2 |
| [QueryResult_UserLibrary.TagItem](#model-queryresult_userlibrary-tagitem) | object | 2 |
| [QueryResult_VirtualFolderInfo](#model-queryresult_virtualfolderinfo) | object | 2 |
| [QueueItem](#model-queueitem) | object | 2 |
| [RatingType](#model-ratingtype) | string | 0 |
| [RecommendationDto](#model-recommendationdto) | object | 4 |
| [RecommendationType](#model-recommendationtype) | string | 0 |
| [RemoteImageInfo](#model-remoteimageinfo) | object | 11 |
| [RemoteImageResult](#model-remoteimageresult) | object | 3 |
| [RemoteSearchQuery_AlbumInfo](#model-remotesearchquery_albuminfo) | object | 5 |
| [RemoteSearchQuery_ArtistInfo](#model-remotesearchquery_artistinfo) | object | 5 |
| [RemoteSearchQuery_BookInfo](#model-remotesearchquery_bookinfo) | object | 5 |
| [RemoteSearchQuery_GameInfo](#model-remotesearchquery_gameinfo) | object | 5 |
| [RemoteSearchQuery_ItemLookupInfo](#model-remotesearchquery_itemlookupinfo) | object | 5 |
| [RemoteSearchQuery_MovieInfo](#model-remotesearchquery_movieinfo) | object | 5 |
| [RemoteSearchQuery_MusicVideoInfo](#model-remotesearchquery_musicvideoinfo) | object | 5 |
| [RemoteSearchQuery_PersonLookupInfo](#model-remotesearchquery_personlookupinfo) | object | 5 |
| [RemoteSearchQuery_SeriesInfo](#model-remotesearchquery_seriesinfo) | object | 5 |
| [RemoteSearchQuery_TrailerInfo](#model-remotesearchquery_trailerinfo) | object | 5 |
| [RemoteSearchResult](#model-remotesearchresult) | object | 19 |
| [RemoteSubtitleInfo](#model-remotesubtitleinfo) | object | 14 |
| [RepeatMode](#model-repeatmode) | string | 0 |
| [Resolution](#model-resolution) | object | 2 |
| [ResolutionWithRate](#model-resolutionwithrate) | object | 4 |
| [ResponseProfile](#model-responseprofile) | object | 7 |
| [RokuMetadata.Api.ThumbnailInfo](#model-rokumetadata-api-thumbnailinfo) | object | 2 |
| [RokuMetadata.Api.ThumbnailSetInfo](#model-rokumetadata-api-thumbnailsetinfo) | object | 2 |
| [RunUICommand](#model-runuicommand) | object | 5 |
| [ScrollDirection](#model-scrolldirection) | string | 0 |
| [SecondaryFrameworks](#model-secondaryframeworks) | string | 0 |
| [SegmentSkipMode](#model-segmentskipmode) | string | 0 |
| [SeriesDisplayOrder](#model-seriesdisplayorder) | string | 0 |
| [SeriesInfo](#model-seriesinfo) | object | 14 |
| [ServerConfiguration](#model-serverconfiguration) | object | 69 |
| [Session.PartyInfo](#model-session-partyinfo) | object | 4 |
| [Session.PartyInfoResult](#model-session-partyinforesult) | object | 1 |
| [Session.SessionInfo](#model-session-sessioninfo) | object | 26 |
| [SessionUserInfo](#model-sessionuserinfo) | object | 3 |
| [SleepTimerMode](#model-sleeptimermode) | string | 0 |
| [SongInfo](#model-songinfo) | object | 16 |
| [SortOrder](#model-sortorder) | string | 0 |
| [SubtitleDeliveryMethod](#model-subtitledeliverymethod) | string | 0 |
| [SubtitleLocationType](#model-subtitlelocationtype) | string | 0 |
| [SubtitlePlaybackMode](#model-subtitleplaybackmode) | string | 0 |
| [SubtitleProfile](#model-subtitleprofile) | object | 7 |
| [Subtitles.SubtitleDownloadResult](#model-subtitles-subtitledownloadresult) | object | 1 |
| [SyncCategory](#model-synccategory) | string | 0 |
| [SyncDataRequest](#model-syncdatarequest) | object | 2 |
| [SyncDataResponse](#model-syncdataresponse) | object | 1 |
| [SyncDialogOptions](#model-syncdialogoptions) | object | 4 |
| [SyncedItem](#model-synceditem) | object | 9 |
| [SyncedItemProgress](#model-synceditemprogress) | object | 2 |
| [SyncJob](#model-syncjob) | object | 27 |
| [SyncJobCreationResult](#model-syncjobcreationresult) | object | 2 |
| [SyncJobItem](#model-syncjobitem) | object | 16 |
| [SyncJobItemStatus](#model-syncjobitemstatus) | string | 0 |
| [SyncJobOption](#model-syncjoboption) | string | 0 |
| [SyncJobRequest](#model-syncjobrequest) | object | 16 |
| [SyncJobStatus](#model-syncjobstatus) | string | 0 |
| [SyncProfileOption](#model-syncprofileoption) | object | 5 |
| [SyncQualityOption](#model-syncqualityoption) | object | 5 |
| [SyncTarget](#model-synctarget) | object | 2 |
| [SystemEvent](#model-systemevent) | string | 0 |
| [SystemInfo](#model-systeminfo) | object | 36 |
| [TaskCompletionStatus](#model-taskcompletionstatus) | string | 0 |
| [TaskInfo](#model-taskinfo) | object | 10 |
| [TaskResult](#model-taskresult) | object | 8 |
| [TaskState](#model-taskstate) | string | 0 |
| [TaskTriggerInfo](#model-tasktriggerinfo) | object | 6 |
| [TextSectionInfo](#model-textsectioninfo) | object | 4 |
| [ThemeMediaResult](#model-thememediaresult) | object | 3 |
| [TrailerInfo](#model-trailerinfo) | object | 12 |
| [TranscodeReason](#model-transcodereason) | string | 0 |
| [TranscodeSeekInfo](#model-transcodeseekinfo) | string | 0 |
| [Transcoding.VpStepInfo](#model-transcoding-vpstepinfo) | object | 11 |
| [Transcoding.VpStepTypes](#model-transcoding-vpsteptypes) | string | 0 |
| [TranscodingInfo](#model-transcodinginfo) | object | 32 |
| [TranscodingProfile](#model-transcodingprofile) | object | 20 |
| [TransportStreamTimestamp](#model-transportstreamtimestamp) | string | 0 |
| [Tuple_Double-Double](#model-tuple_double-double) | object | 2 |
| [TypeOptions](#model-typeoptions) | object | 6 |
| [UICommand](#model-uicommand) | object | 7 |
| [UITabPageInfo](#model-uitabpageinfo) | object | 6 |
| [UIViewInfo](#model-uiviewinfo) | object | 13 |
| [UnratedItem](#model-unrateditem) | string | 0 |
| [UpdateUserPassword](#model-updateuserpassword) | object | 3 |
| [UserAction](#model-useraction) | object | 9 |
| [UserActionType](#model-useractiontype) | string | 0 |
| [UserConfiguration](#model-userconfiguration) | object | 18 |
| [UserDto](#model-userdto) | object | 18 |
| [UserItemDataDto](#model-useritemdatadto) | object | 11 |
| [UserItemShareLevel](#model-useritemsharelevel) | string | 0 |
| [UserLibrary.AddTags](#model-userlibrary-addtags) | object | 1 |
| [UserLibrary.LeaveSharedItems](#model-userlibrary-leaveshareditems) | object | 2 |
| [UserLibrary.OfficialRatingItem](#model-userlibrary-officialratingitem) | object | 1 |
| [UserLibrary.RemoveTags](#model-userlibrary-removetags) | object | 1 |
| [UserLibrary.TagItem](#model-userlibrary-tagitem) | object | 2 |
| [UserLibrary.UpdateUserItemAccess](#model-userlibrary-updateuseritemaccess) | object | 3 |
| [UserNotificationInfo](#model-usernotificationinfo) | object | 15 |
| [UserPolicy](#model-userpolicy) | object | 46 |
| [ValidatePath](#model-validatepath) | object | 4 |
| [Version](#model-version) | object | 6 |
| [Video3DFormat](#model-video3dformat) | string | 0 |
| [VideoCodecBase](#model-videocodecbase) | object | 25 |
| [VideoMediaTypes](#model-videomediatypes) | string | 0 |
| [VirtualFolderInfo](#model-virtualfolderinfo) | object | 11 |
| [WakeOnLanInfo](#model-wakeonlaninfo) | object | 3 |

<a id="model-accessschedule"></a>

## AccessSchedule

- Source pointer: `#/definitions/AccessSchedule`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.AccessSchedule`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `DayOfWeek` | [DynamicDayOfWeek](models.md#model-dynamicdayofweek) | not listed | not declared | not declared |
| `StartHour` | number (double) | not listed | not declared | not declared |
| `EndHour` | number (double) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DynamicDayOfWeek](models.md#model-dynamicdayofweek).

<a id="model-actions-postbackaction"></a>

## Actions.PostbackAction

- Source pointer: `#/definitions/Actions.PostbackAction`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericEdit.Actions.PostbackAction`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `TargetEditorId` | string | not listed | not declared | not declared |
| `PostbackCommandId` | string | not listed | not declared | not declared |
| `CommandParameterPropertyId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-activitylogentry"></a>

## ActivityLogEntry

- Source pointer: `#/definitions/ActivityLogEntry`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Activity.ActivityLogEntry`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | integer (int64) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Overview` | string | not listed | not declared | not declared |
| `ShortOverview` | string | not listed | not declared | not declared |
| `Type` | string | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `Date` | string (date-time) | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `UserPrimaryImageTag` | string | not listed | not declared | not declared |
| `Severity` | [Logging.LogSeverity](models.md#model-logging-logseverity) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Logging.LogSeverity](models.md#model-logging-logseverity).

<a id="model-albuminfo"></a>

## AlbumInfo

- Source pointer: `#/definitions/AlbumInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.AlbumInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `AlbumArtists` | array&lt;string&gt; | not listed | not declared | not declared |
| `SongInfos` | array&lt;[SongInfo](models.md#model-songinfo)&gt; | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).
- [SongInfo](models.md#model-songinfo).

<a id="model-allthememediaresult"></a>

## AllThemeMediaResult

- Source pointer: `#/definitions/AllThemeMediaResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Querying.AllThemeMediaResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ThemeVideosResult` | [ThemeMediaResult](models.md#model-thememediaresult) | not listed | not declared | not declared |
| `ThemeSongsResult` | [ThemeMediaResult](models.md#model-thememediaresult) | not listed | not declared | not declared |
| `SoundtrackSongsResult` | [ThemeMediaResult](models.md#model-thememediaresult) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ThemeMediaResult](models.md#model-thememediaresult).

<a id="model-api-addadminnotification"></a>

## Api.AddAdminNotification

- Source pointer: `#/definitions/Api.AddAdminNotification`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Notifications.Api.AddAdminNotification`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `DisplayDateTime` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-api-availablerecordingoptions"></a>

## Api.AvailableRecordingOptions

- Source pointer: `#/definitions/Api.AvailableRecordingOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.Api.AvailableRecordingOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `RecordingFolders` | array&lt;[Api.NameIdDescriptionPair](models.md#model-api-nameiddescriptionpair)&gt; | not listed | not declared | not declared |
| `MovieRecordingFolders` | array&lt;[Api.NameIdDescriptionPair](models.md#model-api-nameiddescriptionpair)&gt; | not listed | not declared | not declared |
| `SeriesRecordingFolders` | array&lt;[Api.NameIdDescriptionPair](models.md#model-api-nameiddescriptionpair)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Api.NameIdDescriptionPair](models.md#model-api-nameiddescriptionpair).

<a id="model-api-baseitemsrequest"></a>

## Api.BaseItemsRequest

- Source pointer: `#/definitions/Api.BaseItemsRequest`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Api.BaseItemsRequest`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `IsSpecialEpisode` | boolean | not listed | not declared | not declared |
| `Is4K` | boolean | not listed | not declared | not declared |
| `MinDateCreated` | string (date-time) | not listed | not declared | not declared |
| `MaxDateCreated` | string (date-time) | not listed | not declared | not declared |
| `EnableTotalRecordCount` | boolean | not listed | not declared | not declared |
| `MatchAnyWord` | boolean | not listed | not declared | not declared |
| `IsDuplicate` | boolean | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `RecordingKeyword` | string | not listed | not declared | not declared |
| `RecordingKeywordType` | [LiveTv.KeywordType](models.md#model-livetv-keywordtype) | not listed | not declared | not declared |
| `RandomSeed` | integer (int32) | not listed | not declared | not declared |
| `GenreIds` | string | not listed | not declared | not declared |
| `CollectionIds` | string | not listed | not declared | not declared |
| `TagIds` | string | not listed | not declared | not declared |
| `ExcludeTagIds` | string | not listed | not declared | not declared |
| `ItemPersonTypes` | array&lt;[PersonType](models.md#model-persontype)&gt; | not listed | not declared | not declared |
| `ExcludeArtistIds` | string | not listed | not declared | not declared |
| `AlbumArtistIds` | string | not listed | not declared | not declared |
| `ComposerArtistIds` | string | not listed | not declared | not declared |
| `ContributingArtistIds` | string | not listed | not declared | not declared |
| `AlbumIds` | string | not listed | not declared | not declared |
| `OuterIds` | string | not listed | not declared | not declared |
| `ListItemIds` | string | not listed | not declared | not declared |
| `AudioLanguages` | string | not listed | not declared | not declared |
| `SubtitleLanguages` | string | not listed | not declared | not declared |
| `CanEditItems` | boolean | not listed | not declared | not declared |
| `GroupItemsInto` | [Library.ItemLinkType](models.md#model-library-itemlinktype) | not listed | not declared | not declared |
| `IsStandaloneSpecial` | boolean | not listed | not declared | not declared |
| `MinWidth` | integer (int32) | not listed | not declared | not declared |
| `MinHeight` | integer (int32) | not listed | not declared | not declared |
| `MaxWidth` | integer (int32) | not listed | not declared | not declared |
| `MaxHeight` | integer (int32) | not listed | not declared | not declared |
| `GroupProgramsBySeries` | boolean | not listed | not declared | not declared |
| `GroupByPresentationUniqueKey` | boolean | not listed | not declared | not declared |
| `AirDays` | array&lt;[DayOfWeek](models.md#model-dayofweek)&gt; | not listed | not declared | not declared |
| `IsAiring` | boolean | not listed | not declared | not declared |
| `HasAired` | boolean | not listed | not declared | not declared |
| `CollectionTypes` | string | not listed | not declared | not declared |
| `ExcludeSources` | array&lt;string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DayOfWeek](models.md#model-dayofweek).
- [Library.ItemLinkType](models.md#model-library-itemlinktype).
- [LiveTv.KeywordType](models.md#model-livetv-keywordtype).
- [PersonType](models.md#model-persontype).

<a id="model-api-configurationpageinfo"></a>

## Api.ConfigurationPageInfo

- Source pointer: `#/definitions/Api.ConfigurationPageInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.Api.ConfigurationPageInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `EnableInMainMenu` | boolean | not listed | not declared | not declared |
| `EnableInUserMenu` | boolean | not listed | not declared | not declared |
| `FeatureId` | string | not listed | not declared | not declared |
| `MenuSection` | string | not listed | not declared | not declared |
| `MenuIcon` | string | not listed | not declared | not declared |
| `DisplayName` | string | not listed | not declared | not declared |
| `ConfigurationPageType` | [Plugins.ConfigurationPageType](models.md#model-plugins-configurationpagetype) | not listed | not declared | not declared |
| `PluginId` | string | not listed | not declared | not declared |
| `Href` | string | not listed | not declared | not declared |
| `NavMenuId` | string | not listed | not declared | not declared |
| `Plugin` | [Common.Plugins.IPlugin](models.md#model-common-plugins-iplugin) | not listed | not declared | not declared |
| `Translations` | array&lt;string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Common.Plugins.IPlugin](models.md#model-common-plugins-iplugin).
- [Plugins.ConfigurationPageType](models.md#model-plugins-configurationpagetype).

<a id="model-api-epgrow"></a>

## Api.EpgRow

- Source pointer: `#/definitions/Api.EpgRow`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.Api.EpgRow`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Channel` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `Programs` | array&lt;[BaseItemDto](models.md#model-baseitemdto)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).

<a id="model-api-listingprovidertypeinfo"></a>

## Api.ListingProviderTypeInfo

- Source pointer: `#/definitions/Api.ListingProviderTypeInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.Api.ListingProviderTypeInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `SetupUrl` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-api-nameiddescriptionpair"></a>

## Api.NameIdDescriptionPair

- Source pointer: `#/definitions/Api.NameIdDescriptionPair`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.Api.NameIdDescriptionPair`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ShortOverview` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-api-onplaybackprogress"></a>

## Api.OnPlaybackProgress

- Source pointer: `#/definitions/Api.OnPlaybackProgress`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.MediaEncoding.Api.OnPlaybackProgress`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PlaylistIndex` | integer (int32) | not listed | not declared | not declared |
| `PlaylistLength` | integer (int32) | not listed | not declared | not declared |
| `Shuffle` | boolean | not listed | not declared | not declared |
| `SleepTimerMode` | [SleepTimerMode](models.md#model-sleeptimermode) | not listed | not declared | not declared |
| `SleepTimerEndTime` | string (date-time) | not listed | not declared | not declared |
| `EventName` | [ProgressEvent](models.md#model-progressevent) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ProgressEvent](models.md#model-progressevent).
- [SleepTimerMode](models.md#model-sleeptimermode).

<a id="model-api-setchanneldisabled"></a>

## Api.SetChannelDisabled

- Source pointer: `#/definitions/Api.SetChannelDisabled`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.Api.SetChannelDisabled`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `ManagementId` | string | not listed | not declared | not declared |
| `Disabled` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-api-setchannelmapping"></a>

## Api.SetChannelMapping

- Source pointer: `#/definitions/Api.SetChannelMapping`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.Api.SetChannelMapping`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `TunerChannelId` | string | not listed | not declared | not declared |
| `ProviderChannelId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-api-setchannelsortindex"></a>

## Api.SetChannelSortIndex

- Source pointer: `#/definitions/Api.SetChannelSortIndex`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.Api.SetChannelSortIndex`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `ManagementId` | string | not listed | not declared | not declared |
| `NewIndex` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-api-tagitem"></a>

## Api.TagItem

- Source pointer: `#/definitions/Api.TagItem`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.Api.TagItem`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-artistinfo"></a>

## ArtistInfo

- Source pointer: `#/definitions/ArtistInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.ArtistInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-attributes-simplecondition"></a>

## Attributes.SimpleCondition

- Source pointer: `#/definitions/Attributes.SimpleCondition`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Attributes.SimpleCondition`.
- Object-level required declaration: omitted.
- enum: `"IsTrue"`, `"IsFalse"`, `"IsNull"`, `"IsNotNullOrEmpty"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-attributes-valuecondition"></a>

## Attributes.ValueCondition

- Source pointer: `#/definitions/Attributes.ValueCondition`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Attributes.ValueCondition`.
- Object-level required declaration: omitted.
- enum: `"IsEqual"`, `"IsNotEqual"`, `"IsGreater"`, `"IsGreaterOrEqual"`, `"IsLess"`, `"IsLessOrEqual"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-authenticateuser"></a>

## AuthenticateUser

- Source pointer: `#/definitions/AuthenticateUser`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.AuthenticateUser`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Pw` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-authenticateuserbyname"></a>

## AuthenticateUserByName

- Source pointer: `#/definitions/AuthenticateUserByName`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.AuthenticateUserByName`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Username` | string | not listed | not declared | not declared |
| `Pw` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-authentication-authenticationresult"></a>

## Authentication.AuthenticationResult

- Source pointer: `#/definitions/Authentication.AuthenticationResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Authentication.AuthenticationResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `User` | [UserDto](models.md#model-userdto) | not listed | not declared | not declared |
| `SessionInfo` | [Session.SessionInfo](models.md#model-session-sessioninfo) | not listed | not declared | not declared |
| `AccessToken` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Session.SessionInfo](models.md#model-session-sessioninfo).
- [UserDto](models.md#model-userdto).

<a id="model-baseitemdto"></a>

## BaseItemDto

- Source pointer: `#/definitions/BaseItemDto`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.BaseItemDto`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `OriginalTitle` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Guid` | string | not listed | not declared | not declared |
| `Etag` | string | not listed | not declared | not declared |
| `Prefix` | string | not listed | not declared | not declared |
| `TunerName` | string | not listed | not declared | not declared |
| `PlaylistItemId` | string | not listed | not declared | not declared |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |
| `DateModified` | string (date-time) | not listed | not declared | not declared |
| `VideoCodec` | string | not listed | not declared | not declared |
| `AudioCodec` | string | not listed | not declared | not declared |
| `AverageFrameRate` | number (float) | not listed | not declared | not declared |
| `RealFrameRate` | number (float) | not listed | not declared | not declared |
| `ExtraType` | string | not listed | not declared | not declared |
| `SortIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `SortParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `CanDelete` | boolean | not listed | not declared | not declared |
| `CanDownload` | boolean | not listed | not declared | not declared |
| `CanEditItems` | boolean | not listed | not declared | not declared |
| `SupportsResume` | boolean | not listed | not declared | not declared |
| `PresentationUniqueKey` | string | not listed | not declared | not declared |
| `PreferredMetadataLanguage` | string | not listed | not declared | not declared |
| `PreferredMetadataCountryCode` | string | not listed | not declared | not declared |
| `SupportsSync` | boolean | not listed | not declared | not declared |
| `SyncStatus` | [SyncJobItemStatus](models.md#model-syncjobitemstatus) | not listed | not declared | not declared |
| `CanManageAccess` | boolean | not listed | not declared | not declared |
| `CanLeaveContent` | boolean | not listed | not declared | not declared |
| `CanMakePublic` | boolean | not listed | not declared | not declared |
| `Container` | string | not listed | not declared | not declared |
| `SortName` | string | not listed | not declared | not declared |
| `ForcedSortName` | string | not listed | not declared | not declared |
| `Video3DFormat` | [Video3DFormat](models.md#model-video3dformat) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `ExternalUrls` | array&lt;[ExternalUrl](models.md#model-externalurl)&gt; | not listed | not declared | not declared |
| `MediaSources` | array&lt;[MediaSourceInfo](models.md#model-mediasourceinfo)&gt; | not listed | not declared | not declared |
| `CriticRating` | number (float) | not listed | not declared | not declared |
| `GameSystemId` | integer (int64) | not listed | not declared | not declared |
| `AsSeries` | boolean | not listed | not declared | not declared |
| `GameSystem` | string | not listed | not declared | not declared |
| `ProductionLocations` | array&lt;string&gt; | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `OfficialRating` | string | not listed | not declared | not declared |
| `CustomRating` | string | not listed | not declared | not declared |
| `ChannelId` | string | not listed | not declared | not declared |
| `ChannelName` | string | not listed | not declared | not declared |
| `Overview` | string | not listed | not declared | not declared |
| `Taglines` | array&lt;string&gt; | not listed | not declared | not declared |
| `Genres` | array&lt;string&gt; | not listed | not declared | not declared |
| `CommunityRating` | number (float) | not listed | not declared | not declared |
| `RunTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `Size` | integer (int64) | not listed | not declared | not declared |
| `FileName` | string | not listed | not declared | not declared |
| `Bitrate` | integer (int32) | not listed | not declared | not declared |
| `ProductionYear` | integer (int32) | not listed | not declared | not declared |
| `Number` | string | not listed | not declared | not declared |
| `ChannelNumber` | string | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `IndexNumberEnd` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `RemoteTrailers` | array&lt;[MediaUrl](models.md#model-mediaurl)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `IsFolder` | boolean | not listed | not declared | not declared |
| `ParentId` | string | not listed | not declared | not declared |
| `Type` | string | not listed | not declared | not declared |
| `People` | array&lt;[BaseItemPerson](models.md#model-baseitemperson)&gt; | not listed | not declared | not declared |
| `Studios` | array&lt;[NameLongIdPair](models.md#model-namelongidpair)&gt; | not listed | not declared | not declared |
| `GenreItems` | array&lt;[NameLongIdPair](models.md#model-namelongidpair)&gt; | not listed | not declared | not declared |
| `TagItems` | array&lt;[NameLongIdPair](models.md#model-namelongidpair)&gt; | not listed | not declared | not declared |
| `ParentLogoItemId` | string | not listed | not declared | not declared |
| `ParentBackdropItemId` | string | not listed | not declared | not declared |
| `ParentBackdropImageTags` | array&lt;string&gt; | not listed | not declared | not declared |
| `LocalTrailerCount` | integer (int32) | not listed | not declared | not declared |
| `UserData` | [UserItemDataDto](models.md#model-useritemdatadto) | not listed | not declared | not declared |
| `RecursiveItemCount` | integer (int32) | not listed | not declared | not declared |
| `ChildCount` | integer (int32) | not listed | not declared | not declared |
| `SeasonCount` | integer (int32) | not listed | not declared | not declared |
| `SeriesName` | string | not listed | not declared | not declared |
| `SeriesId` | string | not listed | not declared | not declared |
| `SeasonId` | string | not listed | not declared | not declared |
| `SpecialFeatureCount` | integer (int32) | not listed | not declared | not declared |
| `DisplayPreferencesId` | string | not listed | not declared | not declared |
| `Status` | string | not listed | not declared | not declared |
| `AirDays` | array&lt;[DayOfWeek](models.md#model-dayofweek)&gt; | not listed | not declared | not declared |
| `Tags` | array&lt;string&gt; | not listed | not declared | not declared |
| `PrimaryImageAspectRatio` | number (double) | not listed | not declared | not declared |
| `Artists` | array&lt;string&gt; | not listed | not declared | not declared |
| `ArtistItems` | array&lt;[NameIdPair](models.md#model-nameidpair)&gt; | not listed | not declared | not declared |
| `Composers` | array&lt;[NameIdPair](models.md#model-nameidpair)&gt; | not listed | not declared | not declared |
| `Album` | string | not listed | not declared | not declared |
| `CollectionType` | string | not listed | not declared | not declared |
| `DisplayOrder` | string | not listed | not declared | not declared |
| `AlbumId` | string | not listed | not declared | not declared |
| `AlbumPrimaryImageTag` | string | not listed | not declared | not declared |
| `SeriesPrimaryImageTag` | string | not listed | not declared | not declared |
| `AlbumArtist` | string | not listed | not declared | not declared |
| `AlbumArtists` | array&lt;[NameIdPair](models.md#model-nameidpair)&gt; | not listed | not declared | not declared |
| `SeasonName` | string | not listed | not declared | not declared |
| `MediaStreams` | array&lt;[MediaStream](models.md#model-mediastream)&gt; | not listed | not declared | not declared |
| `PartCount` | integer (int32) | not listed | not declared | not declared |
| `ImageTags` | map&lt;string, string&gt; | not listed | not declared | not declared |
| `BackdropImageTags` | array&lt;string&gt; | not listed | not declared | not declared |
| `ParentLogoImageTag` | string | not listed | not declared | not declared |
| `SeriesStudio` | string | not listed | not declared | not declared |
| `PrimaryImageItemId` | string | not listed | not declared | not declared |
| `PrimaryImageTag` | string | not listed | not declared | not declared |
| `ParentThumbItemId` | string | not listed | not declared | not declared |
| `ParentThumbImageTag` | string | not listed | not declared | not declared |
| `Chapters` | array&lt;[ChapterInfo](models.md#model-chapterinfo)&gt; | not listed | not declared | not declared |
| `LocationType` | [LocationType](models.md#model-locationtype) | not listed | not declared | not declared |
| `MediaType` | string | not listed | not declared | not declared |
| `EndDate` | string (date-time) | not listed | not declared | not declared |
| `LockedFields` | array&lt;[MetadataFields](models.md#model-metadatafields)&gt; | not listed | not declared | not declared |
| `LockData` | boolean | not listed | not declared | not declared |
| `Width` | integer (int32) | not listed | not declared | not declared |
| `Height` | integer (int32) | not listed | not declared | not declared |
| `CameraMake` | string | not listed | not declared | not declared |
| `CameraModel` | string | not listed | not declared | not declared |
| `Software` | string | not listed | not declared | not declared |
| `ExposureTime` | number (double) | not listed | not declared | not declared |
| `FocalLength` | number (double) | not listed | not declared | not declared |
| `ImageOrientation` | [Drawing.ImageOrientation](models.md#model-drawing-imageorientation) | not listed | not declared | not declared |
| `Aperture` | number (double) | not listed | not declared | not declared |
| `ShutterSpeed` | number (double) | not listed | not declared | not declared |
| `Latitude` | number (double) | not listed | not declared | not declared |
| `Longitude` | number (double) | not listed | not declared | not declared |
| `Altitude` | number (double) | not listed | not declared | not declared |
| `IsoSpeedRating` | integer (int32) | not listed | not declared | not declared |
| `SeriesTimerId` | string | not listed | not declared | not declared |
| `ChannelPrimaryImageTag` | string | not listed | not declared | not declared |
| `StartDate` | string (date-time) | not listed | not declared | not declared |
| `CompletionPercentage` | number (double) | not listed | not declared | not declared |
| `IsRepeat` | boolean | not listed | not declared | not declared |
| `IsNew` | boolean | not listed | not declared | not declared |
| `EpisodeTitle` | string | not listed | not declared | not declared |
| `IsMovie` | boolean | not listed | not declared | not declared |
| `IsSports` | boolean | not listed | not declared | not declared |
| `IsSeries` | boolean | not listed | not declared | not declared |
| `IsLive` | boolean | not listed | not declared | not declared |
| `IsNews` | boolean | not listed | not declared | not declared |
| `IsKids` | boolean | not listed | not declared | not declared |
| `IsPremiere` | boolean | not listed | not declared | not declared |
| `TimerType` | [LiveTv.TimerType](models.md#model-livetv-timertype) | not listed | not declared | not declared |
| `Disabled` | boolean | not listed | not declared | not declared |
| `ManagementId` | string | not listed | not declared | not declared |
| `TimerId` | string | not listed | not declared | not declared |
| `CurrentProgram` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `MovieCount` | integer (int32) | not listed | not declared | not declared |
| `SeriesCount` | integer (int32) | not listed | not declared | not declared |
| `AlbumCount` | integer (int32) | not listed | not declared | not declared |
| `SongCount` | integer (int32) | not listed | not declared | not declared |
| `MusicVideoCount` | integer (int32) | not listed | not declared | not declared |
| `Subviews` | array&lt;string&gt; | not listed | not declared | not declared |
| `ListingsProviderId` | string | not listed | not declared | not declared |
| `ListingsChannelId` | string | not listed | not declared | not declared |
| `ListingsPath` | string | not listed | not declared | not declared |
| `ListingsId` | string | not listed | not declared | not declared |
| `ListingsChannelName` | string | not listed | not declared | not declared |
| `ListingsChannelNumber` | string | not listed | not declared | not declared |
| `AffiliateCallSign` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [BaseItemPerson](models.md#model-baseitemperson).
- [ChapterInfo](models.md#model-chapterinfo).
- [DayOfWeek](models.md#model-dayofweek).
- [Drawing.ImageOrientation](models.md#model-drawing-imageorientation).
- [ExternalUrl](models.md#model-externalurl).
- [LiveTv.TimerType](models.md#model-livetv-timertype).
- [LocationType](models.md#model-locationtype).
- [MediaSourceInfo](models.md#model-mediasourceinfo).
- [MediaStream](models.md#model-mediastream).
- [MediaUrl](models.md#model-mediaurl).
- [MetadataFields](models.md#model-metadatafields).
- [NameIdPair](models.md#model-nameidpair).
- [NameLongIdPair](models.md#model-namelongidpair).
- [ProviderIdDictionary](models.md#model-provideriddictionary).
- [SyncJobItemStatus](models.md#model-syncjobitemstatus).
- [UserItemDataDto](models.md#model-useritemdatadto).
- [Video3DFormat](models.md#model-video3dformat).

<a id="model-baseitemperson"></a>

## BaseItemPerson

- Source pointer: `#/definitions/BaseItemPerson`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.BaseItemPerson`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Role` | string | not listed | not declared | not declared |
| `Type` | [PersonType](models.md#model-persontype) | not listed | not declared | not declared |
| `PrimaryImageTag` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [PersonType](models.md#model-persontype).

<a id="model-baserefreshrequest"></a>

## BaseRefreshRequest

- Source pointer: `#/definitions/BaseRefreshRequest`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.BaseRefreshRequest`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ReplaceThumbnailImages` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-bitrate"></a>

## BitRate

- Source pointer: `#/definitions/BitRate`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Media.Model.Types.BitRate`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `bps` | integer (int64) | not listed | not declared | not declared |
| `kbps` | number (double) | not listed | not declared | not declared |
| `Mbps` | number (double) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-bookinfo"></a>

## BookInfo

- Source pointer: `#/definitions/BookInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.BookInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SeriesName` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-branding-brandingoptions"></a>

## Branding.BrandingOptions

- Source pointer: `#/definitions/Branding.BrandingOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Branding.BrandingOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `LoginDisclaimer` | string | not listed | not declared | not declared |
| `CustomCss` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-channelmanagementinfo"></a>

## ChannelManagementInfo

- Source pointer: `#/definitions/ChannelManagementInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.LiveTV.ChannelManagementInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-chapterinfo"></a>

## ChapterInfo

- Source pointer: `#/definitions/ChapterInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Entities.ChapterInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `StartPositionTicks` | integer (int64) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `ImageTag` | string | not listed | not declared | not declared |
| `MarkerType` | [MarkerType](models.md#model-markertype) | not listed | not declared | not declared |
| `ChapterIndex` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MarkerType](models.md#model-markertype).

<a id="model-clientcapabilities"></a>

## ClientCapabilities

- Source pointer: `#/definitions/ClientCapabilities`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.ClientCapabilities`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PlayableMediaTypes` | array&lt;string&gt; | not listed | not declared | not declared |
| `SupportedCommands` | array&lt;string&gt; | not listed | not declared | not declared |
| `SupportsMediaControl` | boolean | not listed | not declared | not declared |
| `PushToken` | string | not listed | not declared | not declared |
| `PushTokenType` | string | not listed | not declared | not declared |
| `SupportsSync` | boolean | not listed | not declared | not declared |
| `DeviceProfile` | [DeviceProfile](models.md#model-deviceprofile) | not listed | not declared | not declared |
| `IconUrl` | string | not listed | not declared | not declared |
| `AppId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DeviceProfile](models.md#model-deviceprofile).

<a id="model-codecconfiguration"></a>

## CodecConfiguration

- Source pointer: `#/definitions/CodecConfiguration`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.CodecConfiguration`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `IsEnabled` | boolean | not listed | not declared | not declared |
| `Priority` | integer (int32) | not listed | not declared | not declared |
| `CodecId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-codecdirections"></a>

## CodecDirections

- Source pointer: `#/definitions/CodecDirections`.
- Type: string.
- Source internal type name: `Emby.Media.Model.Enums.CodecDirections`.
- Object-level required declaration: omitted.
- enum: `"Encoder"`, `"Decoder"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-codeckinds"></a>

## CodecKinds

- Source pointer: `#/definitions/CodecKinds`.
- Type: string.
- Source internal type name: `Emby.Media.Model.Enums.CodecKinds`.
- Object-level required declaration: omitted.
- enum: `"Audio"`, `"Video"`, `"SubTitles"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-codecprofile"></a>

## CodecProfile

- Source pointer: `#/definitions/CodecProfile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dlna.CodecProfile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Type` | [CodecType](models.md#model-codectype) | not listed | not declared | not declared |
| `Conditions` | array&lt;[ProfileCondition](models.md#model-profilecondition)&gt; | not listed | not declared | not declared |
| `ApplyConditions` | array&lt;[ProfileCondition](models.md#model-profilecondition)&gt; | not listed | not declared | not declared |
| `Codec` | string | not listed | not declared | not declared |
| `Container` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [CodecType](models.md#model-codectype).
- [ProfileCondition](models.md#model-profilecondition).

<a id="model-codectype"></a>

## CodecType

- Source pointer: `#/definitions/CodecType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dlna.CodecType`.
- Object-level required declaration: omitted.
- enum: `"Video"`, `"VideoAudio"`, `"Audio"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-collections-collectioncreationresult"></a>

## Collections.CollectionCreationResult

- Source pointer: `#/definitions/Collections.CollectionCreationResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Collections.CollectionCreationResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-colorformats"></a>

## ColorFormats

- Source pointer: `#/definitions/ColorFormats`.
- Type: string.
- Source internal type name: `Emby.Media.Model.Enums.ColorFormats`.
- Object-level required declaration: omitted.
- enum: see values below.

**`ColorFormats` enum values**

- `"Unknown"`
- `"yuv420p"`
- `"yuyv422"`
- `"rgb24"`
- `"bgr24"`
- `"yuv422p"`
- `"yuv444p"`
- `"yuv410p"`
- `"yuv411p"`
- `"gray"`
- `"monow"`
- `"monob"`
- `"pal8"`
- `"yuvj420p"`
- `"yuvj422p"`
- `"yuvj444p"`
- `"uyvy422"`
- `"uyyvyy411"`
- `"bgr8"`
- `"bgr4"`
- `"bgr4_byte"`
- `"rgb8"`
- `"rgb4"`
- `"rgb4_byte"`
- `"nv12"`
- `"nv21"`
- `"argb"`
- `"rgba"`
- `"abgr"`
- `"bgra"`
- `"gray16"`
- `"yuv440p"`
- `"yuvj440p"`
- `"yuva420p"`
- `"rgb48"`
- `"rgb565"`
- `"rgb555"`
- `"bgr565"`
- `"bgr555"`
- `"vaapi_moco"`
- `"vaapi_idct"`
- `"vaapi_vld"`
- `"yuv420p16"`
- `"yuv422p16"`
- `"yuv444p16"`
- `"dxva2_vld"`
- `"rgb444"`
- `"bgr444"`
- `"ya8"`
- `"bgr48"`
- `"yuv420p9"`
- `"yuv420p10"`
- `"yuv422p10"`
- `"yuv444p9"`
- `"yuv444p10"`
- `"yuv422p9"`
- `"gbrp"`
- `"gbrp9"`
- `"gbrp10"`
- `"gbrp16"`
- `"yuva422p"`
- `"yuva444p"`
- `"yuva420p9"`
- `"yuva422p9"`
- `"yuva444p9"`
- `"yuva420p10"`
- `"yuva422p10"`
- `"yuva444p10"`
- `"yuva420p16"`
- `"yuva422p16"`
- `"yuva444p16"`
- `"vdpau"`
- `"xyz12"`
- `"nv16"`
- `"nv20"`
- `"rgba64"`
- `"bgra64"`
- `"yvyu422"`
- `"ya16"`
- `"gbrap"`
- `"gbrap16"`
- `"qsv"`
- `"mmal"`
- `"d3d11va_vld"`
- `"cuda"`
- `"_0rgb"`
- `"rgb0"`
- `"_0bgr"`
- `"bgr0"`
- `"yuv420p12"`
- `"yuv420p14"`
- `"yuv422p12"`
- `"yuv422p14"`
- `"yuv444p12"`
- `"yuv444p14"`
- `"gbrp12"`
- `"gbrp14"`
- `"yuvj411p"`
- `"bayer_bggr8"`
- `"bayer_rggb8"`
- `"bayer_gbrg8"`
- `"bayer_grbg8"`
- `"bayer_bggr16"`
- `"bayer_rggb16"`
- `"bayer_gbrg16"`
- `"bayer_grbg16"`
- `"xvmc"`
- `"yuv440p10"`
- `"yuv440p12"`
- `"ayuv64"`
- `"videotoolbox_vld"`
- `"p010"`
- `"gbrap12"`
- `"gbrap10"`
- `"mediacodec"`
- `"gray12"`
- `"gray10"`
- `"gray14"`
- `"p016"`
- `"d3d11"`
- `"gray9"`
- `"gbrpf32"`
- `"gbrapf32"`
- `"drm_prime"`
- `"opencl"`
- `"grayf32"`
- `"yuva422p12"`
- `"yuva444p12"`
- `"nv24"`
- `"nv42"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-common-editortypes"></a>

## Common.EditorTypes

- Source pointer: `#/definitions/Common.EditorTypes`.
- Type: string.
- Source internal type name: `Emby.Web.GenericEdit.Common.EditorTypes`.
- Object-level required declaration: omitted.
- enum: see values below.

**`Common.EditorTypes` enum values**

- `"Group"`
- `"Text"`
- `"Numeric"`
- `"Boolean"`
- `"SelectSingle"`
- `"SelectMultiple"`
- `"Date"`
- `"FilePath"`
- `"FolderPath"`
- `"StatusItem"`
- `"ProgressItem"`
- `"ButtonItem"`
- `"ButtonGroup"`
- `"CaptionItem"`
- `"LabelItem"`
- `"ItemList"`
- `"RadioGroup"`
- `"DxDataGrid"`
- `"DxPivotGrid"`
- `"SpacerItem"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-common-interfaces-icodecdevicecapabilities"></a>

## Common.Interfaces.ICodecDeviceCapabilities

- Source pointer: `#/definitions/Common.Interfaces.ICodecDeviceCapabilities`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.MediaEncoding.Codecs.Common.Interfaces.ICodecDeviceCapabilities`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SupportsHwUpload` | boolean | not listed | not declared | not declared |
| `SupportsHwDownload` | boolean | not listed | not declared | not declared |
| `SupportsStandaloneDeviceInit` | boolean | not listed | not declared | not declared |
| `Supports10BitProcessing` | boolean | not listed | not declared | not declared |
| `SupportsNativeToneMapping` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-common-interfaces-icodecdeviceinfo"></a>

## Common.Interfaces.ICodecDeviceInfo

- Source pointer: `#/definitions/Common.Interfaces.ICodecDeviceInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.MediaEncoding.Codecs.Common.Interfaces.ICodecDeviceInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Capabilities` | [Common.Interfaces.ICodecDeviceCapabilities](models.md#model-common-interfaces-icodecdevicecapabilities) | not listed | not declared | not declared |
| `Adapter` | integer (int32) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Desription` | string | not listed | not declared | not declared |
| `Driver` | string | not listed | not declared | not declared |
| `DriverVersion` | [Version](models.md#model-version) | not listed | not declared | not declared |
| `ApiVersion` | [Version](models.md#model-version) | not listed | not declared | not declared |
| `VendorId` | integer (int32) | not listed | not declared | not declared |
| `DeviceId` | integer (int32) | not listed | not declared | not declared |
| `DeviceIdentifier` | string | not listed | not declared | not declared |
| `HardwareContextFramework` | [SecondaryFrameworks](models.md#model-secondaryframeworks) | not listed | not declared | not declared |
| `DevPath` | string | not listed | not declared | not declared |
| `DrmNode` | string | not listed | not declared | not declared |
| `VendorName` | string | not listed | not declared | not declared |
| `DeviceName` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Common.Interfaces.ICodecDeviceCapabilities](models.md#model-common-interfaces-icodecdevicecapabilities).
- [SecondaryFrameworks](models.md#model-secondaryframeworks).
- [Version](models.md#model-version).

<a id="model-common-plugins-iplugin"></a>

## Common.Plugins.IPlugin

- Source pointer: `#/definitions/Common.Plugins.IPlugin`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Common.Plugins.IPlugin`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `Id` | string (guid) | not listed | not declared | not declared |
| `Version` | [Version](models.md#model-version) | not listed | not declared | not declared |
| `AssemblyFilePath` | string | not listed | not declared | not declared |
| `DataFolderPath` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Version](models.md#model-version).

<a id="model-conditions-propertycondition"></a>

## Conditions.PropertyCondition

- Source pointer: `#/definitions/Conditions.PropertyCondition`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericEdit.Conditions.PropertyCondition`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `AffectedPropertyId` | string | not listed | not declared | not declared |
| `ConditionType` | [Conditions.PropertyConditionType](models.md#model-conditions-propertyconditiontype) | not listed | not declared | not declared |
| `TargetPropertyId` | string | not listed | not declared | not declared |
| `SimpleCondition` | [Attributes.SimpleCondition](models.md#model-attributes-simplecondition) | not listed | not declared | not declared |
| `ValueCondition` | [Attributes.ValueCondition](models.md#model-attributes-valuecondition) | not listed | not declared | not declared |
| `Value` | object | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Attributes.SimpleCondition](models.md#model-attributes-simplecondition).
- [Attributes.ValueCondition](models.md#model-attributes-valuecondition).
- [Conditions.PropertyConditionType](models.md#model-conditions-propertyconditiontype).

<a id="model-conditions-propertyconditiontype"></a>

## Conditions.PropertyConditionType

- Source pointer: `#/definitions/Conditions.PropertyConditionType`.
- Type: string.
- Source internal type name: `Emby.Web.GenericEdit.Conditions.PropertyConditionType`.
- Object-level required declaration: omitted.
- enum: `"Visible"`, `"Enabled"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-configuration-tonemapping-tonemapoptionsvisibility"></a>

## Configuration.ToneMapping.ToneMapOptionsVisibility

- Source pointer: `#/definitions/Configuration.ToneMapping.ToneMapOptionsVisibility`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.MediaEncoding.Configuration.ToneMapping.ToneMapOptionsVisibility`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ShowAdvanced` | boolean | not listed | not declared | not declared |
| `IsSoftwareToneMappingAvailable` | boolean | not listed | not declared | not declared |
| `IsAnyHardwareToneMappingAvailable` | boolean | not listed | not declared | not declared |
| `ShowNvidiaOptions` | boolean | not listed | not declared | not declared |
| `ShowQuickSyncOptions` | boolean | not listed | not declared | not declared |
| `ShowVaapiOptions` | boolean | not listed | not declared | not declared |
| `IsOpenClAvailable` | boolean | not listed | not declared | not declared |
| `IsOpenClSuperTAvailable` | boolean | not listed | not declared | not declared |
| `IsVaapiNativeAvailable` | boolean | not listed | not declared | not declared |
| `IsQuickSyncNativeAvailable` | boolean | not listed | not declared | not declared |
| `OperatingSystem` | [OperatingSystem](models.md#model-operatingsystem) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [OperatingSystem](models.md#model-operatingsystem).

<a id="model-connect-connectauthenticationexchangeresult"></a>

## Connect.ConnectAuthenticationExchangeResult

- Source pointer: `#/definitions/Connect.ConnectAuthenticationExchangeResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Connect.ConnectAuthenticationExchangeResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `LocalUserId` | string | not listed | not declared | not declared |
| `AccessToken` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-connect-userlinkresult"></a>

## Connect.UserLinkResult

- Source pointer: `#/definitions/Connect.UserLinkResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Connect.UserLinkResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `IsPending` | boolean | not listed | not declared | not declared |
| `IsNewUserInvitation` | boolean | not listed | not declared | not declared |
| `GuestDisplayName` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-connect-userlinktype"></a>

## Connect.UserLinkType

- Source pointer: `#/definitions/Connect.UserLinkType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Connect.UserLinkType`.
- Object-level required declaration: omitted.
- enum: `"LinkedUser"`, `"Guest"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-containerprofile"></a>

## ContainerProfile

- Source pointer: `#/definitions/ContainerProfile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dlna.ContainerProfile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Type` | [DlnaProfileType](models.md#model-dlnaprofiletype) | not listed | not declared | not declared |
| `Conditions` | array&lt;[ProfileCondition](models.md#model-profilecondition)&gt; | not listed | not declared | not declared |
| `Container` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DlnaProfileType](models.md#model-dlnaprofiletype).
- [ProfileCondition](models.md#model-profilecondition).

<a id="model-contentsection"></a>

## ContentSection

- Source pointer: `#/definitions/ContentSection`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Entities.ContentSection`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Subtitle` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `SectionType` | string | not listed | not declared | not declared |
| `CollectionType` | string | not listed | not declared | not declared |
| `ViewType` | string | not listed | not declared | not declared |
| `Monitor` | array&lt;string&gt; | not listed | not declared | not declared |
| `CardSizeOffset` | integer (int32) | not listed | not declared | not declared |
| `ScrollDirection` | [ScrollDirection](models.md#model-scrolldirection) | not listed | not declared | not declared |
| `ParentItem` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `TextInfo` | [TextSectionInfo](models.md#model-textsectioninfo) | not listed | not declared | not declared |
| `PremiumFeature` | string | not listed | not declared | not declared |
| `PremiumMessage` | string | not listed | not declared | not declared |
| `RefreshInterval` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [ScrollDirection](models.md#model-scrolldirection).
- [TextSectionInfo](models.md#model-textsectioninfo).

<a id="model-createuserbyname"></a>

## CreateUserByName

- Source pointer: `#/definitions/CreateUserByName`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.CreateUserByName`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `CopyFromUserId` | string | not listed | not declared | not declared |
| `UserCopyOptions` | array&lt;[Library.UserCopyOptions](models.md#model-library-usercopyoptions)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Library.UserCopyOptions](models.md#model-library-usercopyoptions).

<a id="model-dayofweek"></a>

## DayOfWeek

- Source pointer: `#/definitions/DayOfWeek`.
- Type: string.
- Source internal type name: `System.DayOfWeek`.
- Object-level required declaration: omitted.
- enum: `"Sunday"`, `"Monday"`, `"Tuesday"`, `"Wednesday"`, `"Thursday"`, `"Friday"`, `"Saturday"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-defaultdirectorybrowserinfo"></a>

## DefaultDirectoryBrowserInfo

- Source pointer: `#/definitions/DefaultDirectoryBrowserInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.DefaultDirectoryBrowserInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Path` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-deviceprofile"></a>

## DeviceProfile

- Source pointer: `#/definitions/DeviceProfile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dlna.DeviceProfile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `SupportedMediaTypes` | string | not listed | not declared | not declared |
| `MaxStreamingBitrate` | integer (int64) | not listed | not declared | not declared |
| `MusicStreamingTranscodingBitrate` | integer (int32) | not listed | not declared | not declared |
| `MaxStaticMusicBitrate` | integer (int32) | not listed | not declared | not declared |
| `DeclaredFeatures` | array&lt;string&gt; | not listed | not declared | not declared |
| `DirectPlayProfiles` | array&lt;[DirectPlayProfile](models.md#model-directplayprofile)&gt; | not listed | not declared | not declared |
| `TranscodingProfiles` | array&lt;[TranscodingProfile](models.md#model-transcodingprofile)&gt; | not listed | not declared | not declared |
| `ContainerProfiles` | array&lt;[ContainerProfile](models.md#model-containerprofile)&gt; | not listed | not declared | not declared |
| `CodecProfiles` | array&lt;[CodecProfile](models.md#model-codecprofile)&gt; | not listed | not declared | not declared |
| `ResponseProfiles` | array&lt;[ResponseProfile](models.md#model-responseprofile)&gt; | not listed | not declared | not declared |
| `SubtitleProfiles` | array&lt;[SubtitleProfile](models.md#model-subtitleprofile)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [CodecProfile](models.md#model-codecprofile).
- [ContainerProfile](models.md#model-containerprofile).
- [DirectPlayProfile](models.md#model-directplayprofile).
- [ResponseProfile](models.md#model-responseprofile).
- [SubtitleProfile](models.md#model-subtitleprofile).
- [TranscodingProfile](models.md#model-transcodingprofile).

<a id="model-devices-contentuploadhistory"></a>

## Devices.ContentUploadHistory

- Source pointer: `#/definitions/Devices.ContentUploadHistory`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Devices.ContentUploadHistory`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `DeviceId` | string | not listed | not declared | not declared |
| `FilesUploaded` | array&lt;[Devices.LocalFileInfo](models.md#model-devices-localfileinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Devices.LocalFileInfo](models.md#model-devices-localfileinfo).

<a id="model-devices-deviceinfo"></a>

## Devices.DeviceInfo

- Source pointer: `#/definitions/Devices.DeviceInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Devices.DeviceInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `InternalId` | integer (int64) | not listed | not declared | not declared |
| `ReportedDeviceId` | string | not listed | not declared | not declared |
| `LastUserName` | string | not listed | not declared | not declared |
| `AppName` | string | not listed | not declared | not declared |
| `AppVersion` | string | not listed | not declared | not declared |
| `LastUserId` | string | not listed | not declared | not declared |
| `DateLastActivity` | string (date-time) | not listed | not declared | not declared |
| `IconUrl` | string | not listed | not declared | not declared |
| `IpAddress` | string (ipv4) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-devices-deviceoptions"></a>

## Devices.DeviceOptions

- Source pointer: `#/definitions/Devices.DeviceOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Devices.DeviceOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `CustomName` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-devices-localfileinfo"></a>

## Devices.LocalFileInfo

- Source pointer: `#/definitions/Devices.LocalFileInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Devices.LocalFileInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Album` | string | not listed | not declared | not declared |
| `MimeType` | string | not listed | not declared | not declared |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-directplayprofile"></a>

## DirectPlayProfile

- Source pointer: `#/definitions/DirectPlayProfile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dlna.DirectPlayProfile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Container` | string | not listed | not declared | not declared |
| `AudioCodec` | string | not listed | not declared | not declared |
| `VideoCodec` | string | not listed | not declared | not declared |
| `Type` | [DlnaProfileType](models.md#model-dlnaprofiletype) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DlnaProfileType](models.md#model-dlnaprofiletype).

<a id="model-displaypreferences"></a>

## DisplayPreferences

- Source pointer: `#/definitions/DisplayPreferences`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.DisplayPreferences`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `SortBy` | string | not listed | not declared | not declared |
| `CustomPrefs` | map&lt;string, string&gt; | not listed | not declared | not declared |
| `SortOrder` | [SortOrder](models.md#model-sortorder) | not listed | not declared | not declared |
| `Client` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SortOrder](models.md#model-sortorder).

<a id="model-dlna-profiles-deviceidentification"></a>

## Dlna.Profiles.DeviceIdentification

- Source pointer: `#/definitions/Dlna.Profiles.DeviceIdentification`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Dlna.Profiles.DeviceIdentification`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `FriendlyName` | string | not listed | not declared | not declared |
| `ModelNumber` | string | not listed | not declared | not declared |
| `SerialNumber` | string | not listed | not declared | not declared |
| `ModelName` | string | not listed | not declared | not declared |
| `ModelDescription` | string | not listed | not declared | not declared |
| `DeviceDescription` | string | not listed | not declared | not declared |
| `ModelUrl` | string | not listed | not declared | not declared |
| `Manufacturer` | string | not listed | not declared | not declared |
| `ManufacturerUrl` | string | not listed | not declared | not declared |
| `Headers` | array&lt;[Dlna.Profiles.HttpHeaderInfo](models.md#model-dlna-profiles-httpheaderinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Dlna.Profiles.HttpHeaderInfo](models.md#model-dlna-profiles-httpheaderinfo).

<a id="model-dlna-profiles-deviceprofiletype"></a>

## Dlna.Profiles.DeviceProfileType

- Source pointer: `#/definitions/Dlna.Profiles.DeviceProfileType`.
- Type: string.
- Source internal type name: `Emby.Dlna.Profiles.DeviceProfileType`.
- Object-level required declaration: omitted.
- enum: `"System"`, `"User"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-dlna-profiles-dlnaprofile"></a>

## Dlna.Profiles.DlnaProfile

- Source pointer: `#/definitions/Dlna.Profiles.DlnaProfile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Dlna.Profiles.DlnaProfile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Type` | [Dlna.Profiles.DeviceProfileType](models.md#model-dlna-profiles-deviceprofiletype) | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `AlbumArtPn` | string | not listed | not declared | not declared |
| `MaxAlbumArtWidth` | integer (int32) | not listed | not declared | not declared |
| `MaxAlbumArtHeight` | integer (int32) | not listed | not declared | not declared |
| `MaxIconWidth` | integer (int32) | not listed | not declared | not declared |
| `MaxIconHeight` | integer (int32) | not listed | not declared | not declared |
| `FriendlyName` | string | not listed | not declared | not declared |
| `Manufacturer` | string | not listed | not declared | not declared |
| `ManufacturerUrl` | string | not listed | not declared | not declared |
| `ModelName` | string | not listed | not declared | not declared |
| `ModelDescription` | string | not listed | not declared | not declared |
| `ModelNumber` | string | not listed | not declared | not declared |
| `ModelUrl` | string | not listed | not declared | not declared |
| `SerialNumber` | string | not listed | not declared | not declared |
| `EnableAlbumArtInDidl` | boolean | not listed | not declared | not declared |
| `EnableSingleAlbumArtLimit` | boolean | not listed | not declared | not declared |
| `EnableSingleSubtitleLimit` | boolean | not listed | not declared | not declared |
| `ProtocolInfo` | string | not listed | not declared | not declared |
| `TimelineOffsetSeconds` | integer (int32) | not listed | not declared | not declared |
| `RequiresPlainVideoItems` | boolean | not listed | not declared | not declared |
| `RequiresPlainFolders` | boolean | not listed | not declared | not declared |
| `IgnoreTranscodeByteRangeRequests` | boolean | not listed | not declared | not declared |
| `SupportsSamsungBookmark` | boolean | not listed | not declared | not declared |
| `Identification` | [Dlna.Profiles.DeviceIdentification](models.md#model-dlna-profiles-deviceidentification) | not listed | not declared | not declared |
| `ProtocolInfoDetection` | [Dlna.Profiles.ProtocolInfoDetection](models.md#model-dlna-profiles-protocolinfodetection) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `SupportedMediaTypes` | string | not listed | not declared | not declared |
| `MaxStreamingBitrate` | integer (int64) | not listed | not declared | not declared |
| `MusicStreamingTranscodingBitrate` | integer (int32) | not listed | not declared | not declared |
| `MaxStaticMusicBitrate` | integer (int32) | not listed | not declared | not declared |
| `DeclaredFeatures` | array&lt;string&gt; | not listed | not declared | not declared |
| `DirectPlayProfiles` | array&lt;[DirectPlayProfile](models.md#model-directplayprofile)&gt; | not listed | not declared | not declared |
| `TranscodingProfiles` | array&lt;[TranscodingProfile](models.md#model-transcodingprofile)&gt; | not listed | not declared | not declared |
| `ContainerProfiles` | array&lt;[ContainerProfile](models.md#model-containerprofile)&gt; | not listed | not declared | not declared |
| `CodecProfiles` | array&lt;[CodecProfile](models.md#model-codecprofile)&gt; | not listed | not declared | not declared |
| `ResponseProfiles` | array&lt;[ResponseProfile](models.md#model-responseprofile)&gt; | not listed | not declared | not declared |
| `SubtitleProfiles` | array&lt;[SubtitleProfile](models.md#model-subtitleprofile)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [CodecProfile](models.md#model-codecprofile).
- [ContainerProfile](models.md#model-containerprofile).
- [DirectPlayProfile](models.md#model-directplayprofile).
- [Dlna.Profiles.DeviceIdentification](models.md#model-dlna-profiles-deviceidentification).
- [Dlna.Profiles.DeviceProfileType](models.md#model-dlna-profiles-deviceprofiletype).
- [Dlna.Profiles.ProtocolInfoDetection](models.md#model-dlna-profiles-protocolinfodetection).
- [ResponseProfile](models.md#model-responseprofile).
- [SubtitleProfile](models.md#model-subtitleprofile).
- [TranscodingProfile](models.md#model-transcodingprofile).

<a id="model-dlna-profiles-headermatchtype"></a>

## Dlna.Profiles.HeaderMatchType

- Source pointer: `#/definitions/Dlna.Profiles.HeaderMatchType`.
- Type: string.
- Source internal type name: `Emby.Dlna.Profiles.HeaderMatchType`.
- Object-level required declaration: omitted.
- enum: `"Equals"`, `"Regex"`, `"Substring"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-dlna-profiles-httpheaderinfo"></a>

## Dlna.Profiles.HttpHeaderInfo

- Source pointer: `#/definitions/Dlna.Profiles.HttpHeaderInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Dlna.Profiles.HttpHeaderInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Value` | string | not listed | not declared | not declared |
| `Match` | [Dlna.Profiles.HeaderMatchType](models.md#model-dlna-profiles-headermatchtype) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Dlna.Profiles.HeaderMatchType](models.md#model-dlna-profiles-headermatchtype).

<a id="model-dlna-profiles-protocolinfodetection"></a>

## Dlna.Profiles.ProtocolInfoDetection

- Source pointer: `#/definitions/Dlna.Profiles.ProtocolInfoDetection`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Dlna.Profiles.ProtocolInfoDetection`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `EnabledForVideo` | boolean | not listed | not declared | not declared |
| `EnabledForAudio` | boolean | not listed | not declared | not declared |
| `EnabledForPhotos` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-dlnaprofiletype"></a>

## DlnaProfileType

- Source pointer: `#/definitions/DlnaProfileType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dlna.DlnaProfileType`.
- Object-level required declaration: omitted.
- enum: `"Audio"`, `"Video"`, `"Photo"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-drawing-imageorientation"></a>

## Drawing.ImageOrientation

- Source pointer: `#/definitions/Drawing.ImageOrientation`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Drawing.ImageOrientation`.
- Object-level required declaration: omitted.
- enum: `"TopLeft"`, `"TopRight"`, `"BottomRight"`, `"BottomLeft"`, `"LeftTop"`, `"RightTop"`, `"RightBottom"`, `"LeftBottom"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-dynamicdayofweek"></a>

## DynamicDayOfWeek

- Source pointer: `#/definitions/DynamicDayOfWeek`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Configuration.DynamicDayOfWeek`.
- Object-level required declaration: omitted.
- enum: see values below.

**`DynamicDayOfWeek` enum values**

- `"Sunday"`
- `"Monday"`
- `"Tuesday"`
- `"Wednesday"`
- `"Thursday"`
- `"Friday"`
- `"Saturday"`
- `"Everyday"`
- `"Weekday"`
- `"Weekend"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-editobjectcontainer"></a>

## EditObjectContainer

- Source pointer: `#/definitions/EditObjectContainer`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericEdit.EditObjectContainer`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Object` | object | not listed | not declared | not declared |
| `DefaultObject` | object | not listed | not declared | not declared |
| `TypeName` | string | not listed | not declared | not declared |
| `EditorRoot` | [Editors.EditorRoot](models.md#model-editors-editorroot) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Editors.EditorRoot](models.md#model-editors-editorroot).

<a id="model-editors-editorbase"></a>

## Editors.EditorBase

- Source pointer: `#/definitions/Editors.EditorBase`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericEdit.Editors.EditorBase`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `EditorType` | [Common.EditorTypes](models.md#model-common-editortypes) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `AllowEmpty` | boolean | not listed | not declared | not declared |
| `IsReadOnly` | boolean | not listed | not declared | not declared |
| `IsAdvanced` | boolean | not listed | not declared | not declared |
| `DisplayName` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `FeatureRequiresPremiere` | boolean | not listed | not declared | not declared |
| `ParentId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Common.EditorTypes](models.md#model-common-editortypes).

<a id="model-editors-editorbuttonitem"></a>

## Editors.EditorButtonItem

- Source pointer: `#/definitions/Editors.EditorButtonItem`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericEdit.Editors.EditorButtonItem`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `EditorType` | [Common.EditorTypes](models.md#model-common-editortypes) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `AllowEmpty` | boolean | not listed | not declared | not declared |
| `IsReadOnly` | boolean | not listed | not declared | not declared |
| `IsAdvanced` | boolean | not listed | not declared | not declared |
| `DisplayName` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `FeatureRequiresPremiere` | boolean | not listed | not declared | not declared |
| `ParentId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Common.EditorTypes](models.md#model-common-editortypes).

<a id="model-editors-editorroot"></a>

## Editors.EditorRoot

- Source pointer: `#/definitions/Editors.EditorRoot`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericEdit.Editors.EditorRoot`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PropertyConditions` | array&lt;[Conditions.PropertyCondition](models.md#model-conditions-propertycondition)&gt; | not listed | not declared | not declared |
| `PostbackActions` | array&lt;[Actions.PostbackAction](models.md#model-actions-postbackaction)&gt; | not listed | not declared | not declared |
| `TitleButton` | [Editors.EditorButtonItem](models.md#model-editors-editorbuttonitem) | not listed | not declared | not declared |
| `EditorItems` | array&lt;[Editors.EditorBase](models.md#model-editors-editorbase)&gt; | not listed | not declared | not declared |
| `EditorType` | [Common.EditorTypes](models.md#model-common-editortypes) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `AllowEmpty` | boolean | not listed | not declared | not declared |
| `IsReadOnly` | boolean | not listed | not declared | not declared |
| `IsAdvanced` | boolean | not listed | not declared | not declared |
| `DisplayName` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `FeatureRequiresPremiere` | boolean | not listed | not declared | not declared |
| `ParentId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Actions.PostbackAction](models.md#model-actions-postbackaction).
- [Common.EditorTypes](models.md#model-common-editortypes).
- [Conditions.PropertyCondition](models.md#model-conditions-propertycondition).
- [Editors.EditorBase](models.md#model-editors-editorbase).
- [Editors.EditorButtonItem](models.md#model-editors-editorbuttonitem).

<a id="model-encodingcontext"></a>

## EncodingContext

- Source pointer: `#/definitions/EncodingContext`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dlna.EncodingContext`.
- Object-level required declaration: omitted.
- enum: `"Streaming"`, `"Static"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-entities-itemimageinfo"></a>

## Entities.ItemImageInfo

- Source pointer: `#/definitions/Entities.ItemImageInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Entities.ItemImageInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Path` | string | not listed | not declared | not declared |
| `Type` | [ImageType](models.md#model-imagetype) | not listed | not declared | not declared |
| `Orientation` | [Drawing.ImageOrientation](models.md#model-drawing-imageorientation) | not listed | not declared | not declared |
| `DateModified` | string (date-time) | not listed | not declared | not declared |
| `Width` | integer (int32) | not listed | not declared | not declared |
| `Height` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Drawing.ImageOrientation](models.md#model-drawing-imageorientation).
- [ImageType](models.md#model-imagetype).

<a id="model-entities-user"></a>

## Entities.User

- Source pointer: `#/definitions/Entities.User`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Entities.User`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `UsesIdForConfigurationPath` | boolean | not listed | not declared | not declared |
| `Password` | string | not listed | not declared | not declared |
| `EasyPassword` | string | not listed | not declared | not declared |
| `Salt` | string | not listed | not declared | not declared |
| `ConnectUserName` | string | not listed | not declared | not declared |
| `ConnectUserId` | string | not listed | not declared | not declared |
| `ConnectLinkType` | [Connect.UserLinkType](models.md#model-connect-userlinktype) | not listed | not declared | not declared |
| `ConnectAccessKey` | string | not listed | not declared | not declared |
| `ImageInfos` | array&lt;[Entities.ItemImageInfo](models.md#model-entities-itemimageinfo)&gt; | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `LastLoginDate` | string (date-time) | not listed | not declared | not declared |
| `LastActivityDate` | string (date-time) | not listed | not declared | not declared |
| `PlayedPercentage` | number (double) | not listed | not declared | not declared |
| `RecursiveChildCountEqualsChildCount` | boolean | not listed | not declared | not declared |
| `OriginalParsedName` | string | not listed | not declared | not declared |
| `IsNameParsedFromFolder` | boolean | not listed | not declared | not declared |
| `IdString` | string | not listed | not declared | not declared |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |
| `ImportedCollections` | array&lt;[LinkedItemInfo](models.md#model-linkediteminfo)&gt; | not listed | not declared | not declared |
| `ResolvedPresentationUniqueKey` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Connect.UserLinkType](models.md#model-connect-userlinktype).
- [Entities.ItemImageInfo](models.md#model-entities-itemimageinfo).
- [LinkedItemInfo](models.md#model-linkediteminfo).

<a id="model-enums-uicommandtype"></a>

## Enums.UICommandType

- Source pointer: `#/definitions/Enums.UICommandType`.
- Type: string.
- Source internal type name: `Emby.Web.GenericUI.Model.Enums.UICommandType`.
- Object-level required declaration: omitted.
- enum: see values below.

**`Enums.UICommandType` enum values**

- `"Custom"`
- `"WizardCancel"`
- `"WizardBack"`
- `"WizardNext"`
- `"WizardFinish"`
- `"DialogCancel"`
- `"DialogOk"`
- `"PageSave"`
- `"PageBack"`
- `"WizardButton1"`
- `"WizardButton2"`
- `"WizardButton3"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-enums-uiviewtype"></a>

## Enums.UIViewType

- Source pointer: `#/definitions/Enums.UIViewType`.
- Type: string.
- Source internal type name: `Emby.Web.GenericUI.Model.Enums.UIViewType`.
- Object-level required declaration: omitted.
- enum: `"RegularPage"`, `"Dialog"`, `"Wizard"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-extendedvideosubtypes"></a>

## ExtendedVideoSubTypes

- Source pointer: `#/definitions/ExtendedVideoSubTypes`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.ExtendedVideoSubTypes`.
- Object-level required declaration: omitted.
- enum: see values below.

**`ExtendedVideoSubTypes` enum values**

- `"None"`
- `"Hdr10"`
- `"HyperLogGamma"`
- `"Hdr10Plus0"`
- `"DoviProfile02"`
- `"DoviProfile10"`
- `"DoviProfile22"`
- `"DoviProfile30"`
- `"DoviProfile42"`
- `"DoviProfile50"`
- `"DoviProfile61"`
- `"DoviProfile76"`
- `"DoviProfile81"`
- `"DoviProfile82"`
- `"DoviProfile83"`
- `"DoviProfile84"`
- `"DoviProfile85"`
- `"DoviProfile92"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-extendedvideotypes"></a>

## ExtendedVideoTypes

- Source pointer: `#/definitions/ExtendedVideoTypes`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.ExtendedVideoTypes`.
- Object-level required declaration: omitted.
- enum: `"None"`, `"Hdr10"`, `"Hdr10Plus"`, `"HyperLogGamma"`, `"DolbyVision"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-externalidinfo"></a>

## ExternalIdInfo

- Source pointer: `#/definitions/ExternalIdInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Providers.ExternalIdInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Key` | string | not listed | not declared | not declared |
| `Website` | string | not listed | not declared | not declared |
| `UrlFormatString` | string | not listed | not declared | not declared |
| `IsSupportedAsIdentifier` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-externalurl"></a>

## ExternalUrl

- Source pointer: `#/definitions/ExternalUrl`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Providers.ExternalUrl`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Url` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-featureinfo"></a>

## FeatureInfo

- Source pointer: `#/definitions/FeatureInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Features.FeatureInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `FeatureType` | [FeatureType](models.md#model-featuretype) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [FeatureType](models.md#model-featuretype).

<a id="model-featuretype"></a>

## FeatureType

- Source pointer: `#/definitions/FeatureType`.
- Type: string.
- Source internal type name: `Emby.Features.FeatureType`.
- Object-level required declaration: omitted.
- enum: `"System"`, `"User"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-forgotpassword"></a>

## ForgotPassword

- Source pointer: `#/definitions/ForgotPassword`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.ForgotPassword`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `EnteredUsername` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-forgotpasswordaction"></a>

## ForgotPasswordAction

- Source pointer: `#/definitions/ForgotPasswordAction`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Users.ForgotPasswordAction`.
- Object-level required declaration: omitted.
- enum: `"ContactAdmin"`, `"PinCode"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-forgotpasswordpin"></a>

## ForgotPasswordPin

- Source pointer: `#/definitions/ForgotPasswordPin`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.ForgotPasswordPin`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Pin` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-forgotpasswordresult"></a>

## ForgotPasswordResult

- Source pointer: `#/definitions/ForgotPasswordResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Users.ForgotPasswordResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Action` | [ForgotPasswordAction](models.md#model-forgotpasswordaction) | not listed | not declared | not declared |
| `PinFile` | string | not listed | not declared | not declared |
| `PinExpirationDate` | string (date-time) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ForgotPasswordAction](models.md#model-forgotpasswordaction).

<a id="model-gameinfo"></a>

## GameInfo

- Source pointer: `#/definitions/GameInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.GameInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-generalcommand"></a>

## GeneralCommand

- Source pointer: `#/definitions/GeneralCommand`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.GeneralCommand`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `ControllingUserId` | string | not listed | not declared | not declared |
| `Arguments` | map&lt;string, string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-genericedit-ieditobjectcontainer"></a>

## GenericEdit.IEditObjectContainer

- Source pointer: `#/definitions/GenericEdit.IEditObjectContainer`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.GenericEdit.IEditObjectContainer`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Object` | object | not listed | not declared | not declared |
| `DefaultObject` | object | not listed | not declared | not declared |
| `TypeName` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-getdirectorycontents"></a>

## GetDirectoryContents

- Source pointer: `#/definitions/GetDirectoryContents`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.GetDirectoryContents`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Username` | string | not listed | not declared | not declared |
| `Password` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-globalization-countryinfo"></a>

## Globalization.CountryInfo

- Source pointer: `#/definitions/Globalization.CountryInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Globalization.CountryInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `DisplayName` | string | not listed | not declared | not declared |
| `EnglishName` | string | not listed | not declared | not declared |
| `TwoLetterISORegionName` | string | not listed | not declared | not declared |
| `ThreeLetterISORegionName` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-globalization-culturedto"></a>

## Globalization.CultureDto

- Source pointer: `#/definitions/Globalization.CultureDto`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Globalization.CultureDto`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `DisplayName` | string | not listed | not declared | not declared |
| `TwoLetterISOLanguageName` | string | not listed | not declared | not declared |
| `ThreeLetterISOLanguageName` | string | not listed | not declared | not declared |
| `ThreeLetterISOLanguageNames` | array&lt;string&gt; | not listed | not declared | not declared |
| `TwoLetterISOLanguageNames` | array&lt;string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-globalization-localizatonoption"></a>

## Globalization.LocalizatonOption

- Source pointer: `#/definitions/Globalization.LocalizatonOption`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Globalization.LocalizatonOption`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Value` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-imageinfo"></a>

## ImageInfo

- Source pointer: `#/definitions/ImageInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.ImageInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ImageType` | [ImageType](models.md#model-imagetype) | not listed | not declared | not declared |
| `ImageIndex` | integer (int32) | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `Filename` | string | not listed | not declared | not declared |
| `Height` | integer (int32) | not listed | not declared | not declared |
| `Width` | integer (int32) | not listed | not declared | not declared |
| `Size` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ImageType](models.md#model-imagetype).

<a id="model-imageoption"></a>

## ImageOption

- Source pointer: `#/definitions/ImageOption`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.ImageOption`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Type` | [ImageType](models.md#model-imagetype) | not listed | not declared | not declared |
| `Limit` | integer (int32) | not listed | not declared | not declared |
| `MinWidth` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ImageType](models.md#model-imagetype).

<a id="model-imageproviderinfo"></a>

## ImageProviderInfo

- Source pointer: `#/definitions/ImageProviderInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Providers.ImageProviderInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `SupportedImages` | array&lt;[ImageType](models.md#model-imagetype)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ImageType](models.md#model-imagetype).

<a id="model-images-basedownloadremoteimage"></a>

## Images.BaseDownloadRemoteImage

- Source pointer: `#/definitions/Images.BaseDownloadRemoteImage`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Images.BaseDownloadRemoteImage`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ImageIndex` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-imagesavingconvention"></a>

## ImageSavingConvention

- Source pointer: `#/definitions/ImageSavingConvention`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Configuration.ImageSavingConvention`.
- Object-level required declaration: omitted.
- enum: `"Legacy"`, `"Compatible"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-imagetype"></a>

## ImageType

- Source pointer: `#/definitions/ImageType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.ImageType`.
- Object-level required declaration: omitted.
- enum: see values below.

**`ImageType` enum values**

- `"Primary"`
- `"Art"`
- `"Backdrop"`
- `"Banner"`
- `"Logo"`
- `"Thumb"`
- `"Disc"`
- `"Box"`
- `"Screenshot"`
- `"Menu"`
- `"Chapter"`
- `"BoxRear"`
- `"Thumbnail"`
- `"LogoLight"`
- `"LogoLightColor"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-installationinfo"></a>

## InstallationInfo

- Source pointer: `#/definitions/InstallationInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Updates.InstallationInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string (guid) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `AssemblyGuid` | string | not listed | not declared | not declared |
| `Version` | string | not listed | not declared | not declared |
| `UpdateClass` | [PackageVersionClass](models.md#model-packageversionclass) | not listed | not declared | not declared |
| `PercentComplete` | number (double) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [PackageVersionClass](models.md#model-packageversionclass).

<a id="model-io-filesystementryinfo"></a>

## IO.FileSystemEntryInfo

- Source pointer: `#/definitions/IO.FileSystemEntryInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.IO.FileSystemEntryInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `Type` | [IO.FileSystemEntryType](models.md#model-io-filesystementrytype) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [IO.FileSystemEntryType](models.md#model-io-filesystementrytype).

<a id="model-io-filesystementrytype"></a>

## IO.FileSystemEntryType

- Source pointer: `#/definitions/IO.FileSystemEntryType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.IO.FileSystemEntryType`.
- Object-level required declaration: omitted.
- enum: `"File"`, `"Directory"`, `"NetworkComputer"`, `"NetworkShare"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-itemcounts"></a>

## ItemCounts

- Source pointer: `#/definitions/ItemCounts`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.ItemCounts`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `MovieCount` | integer (int32) | not listed | not declared | not declared |
| `SeriesCount` | integer (int32) | not listed | not declared | not declared |
| `EpisodeCount` | integer (int32) | not listed | not declared | not declared |
| `GameCount` | integer (int32) | not listed | not declared | not declared |
| `ArtistCount` | integer (int32) | not listed | not declared | not declared |
| `ProgramCount` | integer (int32) | not listed | not declared | not declared |
| `GameSystemCount` | integer (int32) | not listed | not declared | not declared |
| `TrailerCount` | integer (int32) | not listed | not declared | not declared |
| `SongCount` | integer (int32) | not listed | not declared | not declared |
| `AlbumCount` | integer (int32) | not listed | not declared | not declared |
| `MusicVideoCount` | integer (int32) | not listed | not declared | not declared |
| `BoxSetCount` | integer (int32) | not listed | not declared | not declared |
| `BookCount` | integer (int32) | not listed | not declared | not declared |
| `ItemCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-itemfileinfo"></a>

## ItemFileInfo

- Source pointer: `#/definitions/ItemFileInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.ItemFileInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Type` | [ItemFileType](models.md#model-itemfiletype) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `ImageType` | [ImageType](models.md#model-imagetype) | not listed | not declared | not declared |
| `Index` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ImageType](models.md#model-imagetype).
- [ItemFileType](models.md#model-itemfiletype).

<a id="model-itemfiletype"></a>

## ItemFileType

- Source pointer: `#/definitions/ItemFileType`.
- Type: string.
- Source internal type name: `Emby.Server.Sync.Model.ItemFileType`.
- Object-level required declaration: omitted.
- enum: `"Media"`, `"Image"`, `"Subtitles"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-itemlookupinfo"></a>

## ItemLookupInfo

- Source pointer: `#/definitions/ItemLookupInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.ItemLookupInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-levelinformation"></a>

## LevelInformation

- Source pointer: `#/definitions/LevelInformation`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Media.Model.Types.LevelInformation`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ShortName` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `Ordinal` | integer (int32) | not listed | not declared | not declared |
| `MaxBitRate` | [BitRate](models.md#model-bitrate) | not listed | not declared | not declared |
| `MaxBitRateDisplay` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `ResolutionRates` | array&lt;[ResolutionWithRate](models.md#model-resolutionwithrate)&gt; | not listed | not declared | not declared |
| `ResolutionRateStrings` | array&lt;string&gt; | not listed | not declared | not declared |
| `ResolutionRatesDisplay` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BitRate](models.md#model-bitrate).
- [ResolutionWithRate](models.md#model-resolutionwithrate).

<a id="model-library-addmediapath"></a>

## Library.AddMediaPath

- Source pointer: `#/definitions/Library.AddMediaPath`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.AddMediaPath`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `PathInfo` | [MediaPathInfo](models.md#model-mediapathinfo) | not listed | not declared | not declared |
| `RefreshLibrary` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MediaPathInfo](models.md#model-mediapathinfo).

<a id="model-library-addvirtualfolder"></a>

## Library.AddVirtualFolder

- Source pointer: `#/definitions/Library.AddVirtualFolder`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.AddVirtualFolder`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `CollectionType` | string | not listed | not declared | not declared |
| `RefreshLibrary` | boolean | not listed | not declared | not declared |
| `Paths` | array&lt;string&gt; | not listed | not declared | not declared |
| `LibraryOptions` | [LibraryOptions](models.md#model-libraryoptions) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LibraryOptions](models.md#model-libraryoptions).

<a id="model-library-deleteinfo"></a>

## Library.DeleteInfo

- Source pointer: `#/definitions/Library.DeleteInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.DeleteInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Paths` | array&lt;string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-library-itemlinktype"></a>

## Library.ItemLinkType

- Source pointer: `#/definitions/Library.ItemLinkType`.
- Type: string.
- Source internal type name: `MediaBrowser.Controller.Library.ItemLinkType`.
- Object-level required declaration: omitted.
- enum: see values below.

**`Library.ItemLinkType` enum values**

- `"Artists"`
- `"AlbumArtists"`
- `"Genres"`
- `"Studios"`
- `"Tags"`
- `"Composers"`
- `"Collections"`
- `"Albums"`
- `"CollectionFolders"`
- `"LiveTVSeries"`
- `"GameSystems"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-library-mediafolder"></a>

## Library.MediaFolder

- Source pointer: `#/definitions/Library.MediaFolder`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.MediaFolder`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Guid` | string | not listed | not declared | not declared |
| `SubFolders` | array&lt;[Library.SubFolder](models.md#model-library-subfolder)&gt; | not listed | not declared | not declared |
| `IsUserAccessConfigurable` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Library.SubFolder](models.md#model-library-subfolder).

<a id="model-library-mediaupdateinfo"></a>

## Library.MediaUpdateInfo

- Source pointer: `#/definitions/Library.MediaUpdateInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.MediaUpdateInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Path` | string | not listed | not declared | not declared |
| `UpdateType` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-library-postupdatedmedia"></a>

## Library.PostUpdatedMedia

- Source pointer: `#/definitions/Library.PostUpdatedMedia`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.PostUpdatedMedia`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Updates` | array&lt;[Library.MediaUpdateInfo](models.md#model-library-mediaupdateinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Library.MediaUpdateInfo](models.md#model-library-mediaupdateinfo).

<a id="model-library-removemediapath"></a>

## Library.RemoveMediaPath

- Source pointer: `#/definitions/Library.RemoveMediaPath`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.RemoveMediaPath`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `RefreshLibrary` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-library-removevirtualfolder"></a>

## Library.RemoveVirtualFolder

- Source pointer: `#/definitions/Library.RemoveVirtualFolder`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.RemoveVirtualFolder`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `RefreshLibrary` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-library-renamevirtualfolder"></a>

## Library.RenameVirtualFolder

- Source pointer: `#/definitions/Library.RenameVirtualFolder`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.RenameVirtualFolder`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `NewName` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-library-subfolder"></a>

## Library.SubFolder

- Source pointer: `#/definitions/Library.SubFolder`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.SubFolder`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `IsUserAccessConfigurable` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-library-updatelibraryoptions"></a>

## Library.UpdateLibraryOptions

- Source pointer: `#/definitions/Library.UpdateLibraryOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.UpdateLibraryOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `LibraryOptions` | [LibraryOptions](models.md#model-libraryoptions) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LibraryOptions](models.md#model-libraryoptions).

<a id="model-library-updatemediapath"></a>

## Library.UpdateMediaPath

- Source pointer: `#/definitions/Library.UpdateMediaPath`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Library.UpdateMediaPath`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `PathInfo` | [MediaPathInfo](models.md#model-mediapathinfo) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MediaPathInfo](models.md#model-mediapathinfo).

<a id="model-library-usercopyoptions"></a>

## Library.UserCopyOptions

- Source pointer: `#/definitions/Library.UserCopyOptions`.
- Type: string.
- Source internal type name: `MediaBrowser.Controller.Library.UserCopyOptions`.
- Object-level required declaration: omitted.
- enum: `"UserPolicy"`, `"UserConfiguration"`, `"UserData"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-libraryoptioninfo"></a>

## LibraryOptionInfo

- Source pointer: `#/definitions/LibraryOptionInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.LibraryOptionInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `SetupUrl` | string | not listed | not declared | not declared |
| `DefaultEnabled` | boolean | not listed | not declared | not declared |
| `Features` | array&lt;[MetadataFeatures](models.md#model-metadatafeatures)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MetadataFeatures](models.md#model-metadatafeatures).

<a id="model-libraryoptions"></a>

## LibraryOptions

- Source pointer: `#/definitions/LibraryOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.LibraryOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `EnableArchiveMediaFiles` | boolean | not listed | not declared | not declared |
| `EnablePhotos` | boolean | not listed | not declared | not declared |
| `EnableRealtimeMonitor` | boolean | not listed | not declared | not declared |
| `EnableMarkerDetection` | boolean | not listed | not declared | not declared |
| `EnableMarkerDetectionDuringLibraryScan` | boolean | not listed | not declared | not declared |
| `IntroDetectionFingerprintLength` | integer (int32) | not listed | not declared | not declared |
| `EnableChapterImageExtraction` | boolean | not listed | not declared | not declared |
| `ExtractChapterImagesDuringLibraryScan` | boolean | not listed | not declared | not declared |
| `DownloadImagesInAdvance` | boolean | not listed | not declared | not declared |
| `CacheImages` | boolean | not listed | not declared | not declared |
| `ExcludeFromSearch` | boolean | not listed | not declared | not declared |
| `EnablePlexIgnore` | boolean | not listed | not declared | not declared |
| `PathInfos` | array&lt;[MediaPathInfo](models.md#model-mediapathinfo)&gt; | not listed | not declared | not declared |
| `IgnoreHiddenFiles` | boolean | not listed | not declared | not declared |
| `IgnoreFileExtensions` | array&lt;string&gt; | not listed | not declared | not declared |
| `SaveLocalMetadata` | boolean | not listed | not declared | not declared |
| `SaveMetadataHidden` | boolean | not listed | not declared | not declared |
| `SaveLocalThumbnailSets` | boolean | not listed | not declared | not declared |
| `ImportPlaylists` | boolean | not listed | not declared | not declared |
| `EnableAutomaticSeriesGrouping` | boolean | not listed | not declared | not declared |
| `ShareEmbeddedMusicAlbumImages` | boolean | not listed | not declared | not declared |
| `EnableEmbeddedTitles` | boolean | not listed | not declared | not declared |
| `EnableAudioResume` | boolean | not listed | not declared | not declared |
| `AutoGenerateChapters` | boolean | not listed | not declared | not declared |
| `MergeTopLevelFolders` | boolean | not listed | not declared | not declared |
| `AutoGenerateChapterIntervalMinutes` | integer (int32) | not listed | not declared | not declared |
| `AutomaticRefreshIntervalDays` | integer (int32) | not listed | not declared | not declared |
| `PlaceholderMetadataRefreshIntervalDays` | integer (int32) | not listed | not declared | not declared |
| `PreferredMetadataLanguage` | string | not listed | not declared | not declared |
| `PreferredImageLanguage` | string | not listed | not declared | not declared |
| `ContentType` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataSavers` | array&lt;string&gt; | not listed | not declared | not declared |
| `DisabledLocalMetadataReaders` | array&lt;string&gt; | not listed | not declared | not declared |
| `LocalMetadataReaderOrder` | array&lt;string&gt; | not listed | not declared | not declared |
| `DisabledLyricsFetchers` | array&lt;string&gt; | not listed | not declared | not declared |
| `SaveLyricsWithMedia` | boolean | not listed | not declared | not declared |
| `LyricsDownloadMaxAgeDays` | integer (int32) | not listed | not declared | not declared |
| `LyricsFetcherOrder` | array&lt;string&gt; | not listed | not declared | not declared |
| `LyricsDownloadLanguages` | array&lt;string&gt; | not listed | not declared | not declared |
| `DisabledSubtitleFetchers` | array&lt;string&gt; | not listed | not declared | not declared |
| `SubtitleFetcherOrder` | array&lt;string&gt; | not listed | not declared | not declared |
| `SkipSubtitlesIfEmbeddedSubtitlesPresent` | boolean | not listed | not declared | not declared |
| `SkipSubtitlesIfAudioTrackMatches` | boolean | not listed | not declared | not declared |
| `SubtitleDownloadLanguages` | array&lt;string&gt; | not listed | not declared | not declared |
| `SubtitleDownloadMaxAgeDays` | integer (int32) | not listed | not declared | not declared |
| `RequirePerfectSubtitleMatch` | boolean | not listed | not declared | not declared |
| `SaveSubtitlesWithMedia` | boolean | not listed | not declared | not declared |
| `ForcedSubtitlesOnly` | boolean | not listed | not declared | not declared |
| `HearingImpairedSubtitlesOnly` | boolean | not listed | not declared | not declared |
| `TypeOptions` | array&lt;[TypeOptions](models.md#model-typeoptions)&gt; | not listed | not declared | not declared |
| `CollapseSingleItemFolders` | boolean | not listed | not declared | not declared |
| `ForceCollapseSingleItemFolders` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |
| `ImportCollections` | boolean | not listed | not declared | not declared |
| `EnableMultiVersionByFiles` | boolean | not listed | not declared | not declared |
| `EnableMultiVersionByMetadata` | boolean | not listed | not declared | not declared |
| `EnableMultiPartItems` | boolean | not listed | not declared | not declared |
| `MinCollectionItems` | integer (int32) | not listed | not declared | not declared |
| `MusicFolderStructure` | string | not listed | not declared | not declared |
| `MinResumePct` | integer (int32) | not listed | not declared | not declared |
| `MaxResumePct` | integer (int32) | not listed | not declared | not declared |
| `MinResumeDurationSeconds` | integer (int32) | not listed | not declared | not declared |
| `ThumbnailImagesIntervalSeconds` | integer (int32) | not listed | not declared | not declared |
| `SampleIgnoreSize` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MediaPathInfo](models.md#model-mediapathinfo).
- [TypeOptions](models.md#model-typeoptions).

<a id="model-libraryoptionsresult"></a>

## LibraryOptionsResult

- Source pointer: `#/definitions/LibraryOptionsResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.LibraryOptionsResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `MetadataSavers` | array&lt;[LibraryOptionInfo](models.md#model-libraryoptioninfo)&gt; | not listed | not declared | not declared |
| `MetadataReaders` | array&lt;[LibraryOptionInfo](models.md#model-libraryoptioninfo)&gt; | not listed | not declared | not declared |
| `SubtitleFetchers` | array&lt;[LibraryOptionInfo](models.md#model-libraryoptioninfo)&gt; | not listed | not declared | not declared |
| `LyricsFetchers` | array&lt;[LibraryOptionInfo](models.md#model-libraryoptioninfo)&gt; | not listed | not declared | not declared |
| `TypeOptions` | array&lt;[LibraryTypeOptions](models.md#model-librarytypeoptions)&gt; | not listed | not declared | not declared |
| `DefaultLibraryOptions` | [LibraryOptions](models.md#model-libraryoptions) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LibraryOptionInfo](models.md#model-libraryoptioninfo).
- [LibraryOptions](models.md#model-libraryoptions).
- [LibraryTypeOptions](models.md#model-librarytypeoptions).

<a id="model-librarytypeoptions"></a>

## LibraryTypeOptions

- Source pointer: `#/definitions/LibraryTypeOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.LibraryTypeOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Type` | string | not listed | not declared | not declared |
| `MetadataFetchers` | array&lt;[LibraryOptionInfo](models.md#model-libraryoptioninfo)&gt; | not listed | not declared | not declared |
| `ImageFetchers` | array&lt;[LibraryOptionInfo](models.md#model-libraryoptioninfo)&gt; | not listed | not declared | not declared |
| `SupportedImageTypes` | array&lt;[ImageType](models.md#model-imagetype)&gt; | not listed | not declared | not declared |
| `DefaultImageOptions` | array&lt;[ImageOption](models.md#model-imageoption)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ImageOption](models.md#model-imageoption).
- [ImageType](models.md#model-imagetype).
- [LibraryOptionInfo](models.md#model-libraryoptioninfo).

<a id="model-linkediteminfo"></a>

## LinkedItemInfo

- Source pointer: `#/definitions/LinkedItemInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.LinkedItemInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Id` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-livestreamrequest"></a>

## LiveStreamRequest

- Source pointer: `#/definitions/LiveStreamRequest`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.MediaInfo.LiveStreamRequest`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `OpenToken` | string | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `PlaySessionId` | string | not listed | not declared | not declared |
| `MaxStreamingBitrate` | integer (int64) | not listed | not declared | not declared |
| `StartTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `AudioStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `SubtitleStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `MaxAudioChannels` | integer (int32) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `DeviceProfile` | [DeviceProfile](models.md#model-deviceprofile) | not listed | not declared | not declared |
| `EnableDirectPlay` | boolean | not listed | not declared | not declared |
| `EnableDirectStream` | boolean | not listed | not declared | not declared |
| `EnableTranscoding` | boolean | not listed | not declared | not declared |
| `AllowVideoStreamCopy` | boolean | not listed | not declared | not declared |
| `AllowInterlacedVideoStreamCopy` | boolean | not listed | not declared | not declared |
| `AllowAudioStreamCopy` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DeviceProfile](models.md#model-deviceprofile).

<a id="model-livestreamresponse"></a>

## LiveStreamResponse

- Source pointer: `#/definitions/LiveStreamResponse`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.MediaInfo.LiveStreamResponse`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `MediaSource` | [MediaSourceInfo](models.md#model-mediasourceinfo) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MediaSourceInfo](models.md#model-mediasourceinfo).

<a id="model-livetv-channeltype"></a>

## LiveTv.ChannelType

- Source pointer: `#/definitions/LiveTv.ChannelType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.LiveTv.ChannelType`.
- Object-level required declaration: omitted.
- enum: `"TV"`, `"Radio"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-livetv-guideinfo"></a>

## LiveTv.GuideInfo

- Source pointer: `#/definitions/LiveTv.GuideInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.LiveTv.GuideInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `StartDate` | string (date-time) | not listed | not declared | not declared |
| `EndDate` | string (date-time) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-livetv-keepuntil"></a>

## LiveTv.KeepUntil

- Source pointer: `#/definitions/LiveTv.KeepUntil`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.LiveTv.KeepUntil`.
- Object-level required declaration: omitted.
- enum: `"UntilDeleted"`, `"UntilSpaceNeeded"`, `"UntilWatched"`, `"UntilDate"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-livetv-keywordinfo"></a>

## LiveTv.KeywordInfo

- Source pointer: `#/definitions/LiveTv.KeywordInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.LiveTv.KeywordInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `KeywordType` | [LiveTv.KeywordType](models.md#model-livetv-keywordtype) | not listed | not declared | not declared |
| `Keyword` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LiveTv.KeywordType](models.md#model-livetv-keywordtype).

<a id="model-livetv-keywordtype"></a>

## LiveTv.KeywordType

- Source pointer: `#/definitions/LiveTv.KeywordType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.LiveTv.KeywordType`.
- Object-level required declaration: omitted.
- enum: `"Name"`, `"EpisodeTitle"`, `"Overview"`, `"Actor"`, `"Director"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-livetv-listingsproviderinfo"></a>

## LiveTv.ListingsProviderInfo

- Source pointer: `#/definitions/LiveTv.ListingsProviderInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.LiveTv.ListingsProviderInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `SetupUrl` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Type` | string | not listed | not declared | not declared |
| `Username` | string | not listed | not declared | not declared |
| `Password` | string | not listed | not declared | not declared |
| `ListingsId` | string | not listed | not declared | not declared |
| `ZipCode` | string | not listed | not declared | not declared |
| `Country` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `EnabledTuners` | array&lt;string&gt; | not listed | not declared | not declared |
| `EnableAllTuners` | boolean | not listed | not declared | not declared |
| `NewsCategories` | array&lt;string&gt; | not listed | not declared | not declared |
| `SportsCategories` | array&lt;string&gt; | not listed | not declared | not declared |
| `KidsCategories` | array&lt;string&gt; | not listed | not declared | not declared |
| `MovieCategories` | array&lt;string&gt; | not listed | not declared | not declared |
| `ChannelMappings` | array&lt;[NameValuePair](models.md#model-namevaluepair)&gt; | not listed | not declared | not declared |
| `TvgShiftTicks` | integer (int64) | not listed | not declared | not declared |
| `MoviePrefix` | string | not listed | not declared | not declared |
| `PreferredLanguage` | string | not listed | not declared | not declared |
| `UserAgent` | string | not listed | not declared | not declared |
| `DataVersion` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [NameValuePair](models.md#model-namevaluepair).

<a id="model-livetv-livetvinfo"></a>

## LiveTv.LiveTvInfo

- Source pointer: `#/definitions/LiveTv.LiveTvInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.LiveTv.LiveTvInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `IsEnabled` | boolean | not listed | not declared | not declared |
| `EnabledUsers` | array&lt;string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-livetv-recordingstatus"></a>

## LiveTv.RecordingStatus

- Source pointer: `#/definitions/LiveTv.RecordingStatus`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.LiveTv.RecordingStatus`.
- Object-level required declaration: omitted.
- enum: `"New"`, `"InProgress"`, `"Completed"`, `"Cancelled"`, `"ConflictedOk"`, `"ConflictedNotOk"`, `"Error"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-livetv-seriestimerinfo"></a>

## LiveTv.SeriesTimerInfo

- Source pointer: `#/definitions/LiveTv.SeriesTimerInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.LiveTv.SeriesTimerInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `ChannelId` | string | not listed | not declared | not declared |
| `ChannelIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `ParentFolderId` | integer (int64) | not listed | not declared | not declared |
| `ProgramId` | string | not listed | not declared | not declared |
| `ServiceName` | string | not listed | not declared | not declared |
| `Overview` | string | not listed | not declared | not declared |
| `StartDate` | string (date-time) | not listed | not declared | not declared |
| `EndDate` | string (date-time) | not listed | not declared | not declared |
| `RecordAnyTime` | boolean | not listed | not declared | not declared |
| `KeepUpTo` | integer (int32) | not listed | not declared | not declared |
| `KeepUntil` | [LiveTv.KeepUntil](models.md#model-livetv-keepuntil) | not listed | not declared | not declared |
| `SkipEpisodesInLibrary` | boolean | not listed | not declared | not declared |
| `MatchExistingItemsWithAnyLibrary` | boolean | not listed | not declared | not declared |
| `RecordNewOnly` | boolean | not listed | not declared | not declared |
| `Days` | array&lt;[DayOfWeek](models.md#model-dayofweek)&gt; | not listed | not declared | not declared |
| `Priority` | integer (int32) | not listed | not declared | not declared |
| `PrePaddingSeconds` | integer (int32) | not listed | not declared | not declared |
| `PostPaddingSeconds` | integer (int32) | not listed | not declared | not declared |
| `IsPrePaddingRequired` | boolean | not listed | not declared | not declared |
| `IsPostPaddingRequired` | boolean | not listed | not declared | not declared |
| `SeriesId` | string | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `MaxRecordingSeconds` | integer (int32) | not listed | not declared | not declared |
| `Keywords` | array&lt;[LiveTv.KeywordInfo](models.md#model-livetv-keywordinfo)&gt; | not listed | not declared | not declared |
| `TimerType` | [LiveTv.TimerType](models.md#model-livetv-timertype) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DayOfWeek](models.md#model-dayofweek).
- [LiveTv.KeepUntil](models.md#model-livetv-keepuntil).
- [LiveTv.KeywordInfo](models.md#model-livetv-keywordinfo).
- [LiveTv.TimerType](models.md#model-livetv-timertype).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-livetv-seriestimerinfodto"></a>

## LiveTv.SeriesTimerInfoDto

- Source pointer: `#/definitions/LiveTv.SeriesTimerInfoDto`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.LiveTv.SeriesTimerInfoDto`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `RecordAnyTime` | boolean | not listed | not declared | not declared |
| `SkipEpisodesInLibrary` | boolean | not listed | not declared | not declared |
| `MatchExistingItemsWithAnyLibrary` | boolean | not listed | not declared | not declared |
| `RecordAnyChannel` | boolean | not listed | not declared | not declared |
| `KeepUpTo` | integer (int32) | not listed | not declared | not declared |
| `MaxRecordingSeconds` | integer (int32) | not listed | not declared | not declared |
| `RecordNewOnly` | boolean | not listed | not declared | not declared |
| `ChannelIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `Days` | array&lt;[DayOfWeek](models.md#model-dayofweek)&gt; | not listed | not declared | not declared |
| `ImageTags` | map&lt;string, string&gt; | not listed | not declared | not declared |
| `ParentThumbItemId` | string | not listed | not declared | not declared |
| `ParentThumbImageTag` | string | not listed | not declared | not declared |
| `ParentPrimaryImageItemId` | string | not listed | not declared | not declared |
| `ParentPrimaryImageTag` | string | not listed | not declared | not declared |
| `SeriesId` | string | not listed | not declared | not declared |
| `Keywords` | array&lt;[LiveTv.KeywordInfo](models.md#model-livetv-keywordinfo)&gt; | not listed | not declared | not declared |
| `TimerType` | [LiveTv.TimerType](models.md#model-livetv-timertype) | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Type` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |
| `ChannelId` | string | not listed | not declared | not declared |
| `ChannelName` | string | not listed | not declared | not declared |
| `ChannelNumber` | string | not listed | not declared | not declared |
| `ChannelPrimaryImageTag` | string | not listed | not declared | not declared |
| `ProgramId` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Overview` | string | not listed | not declared | not declared |
| `ParentFolderId` | string | not listed | not declared | not declared |
| `StartDate` | string (date-time) | not listed | not declared | not declared |
| `EndDate` | string (date-time) | not listed | not declared | not declared |
| `Priority` | integer (int32) | not listed | not declared | not declared |
| `PrePaddingSeconds` | integer (int32) | not listed | not declared | not declared |
| `PostPaddingSeconds` | integer (int32) | not listed | not declared | not declared |
| `IsPrePaddingRequired` | boolean | not listed | not declared | not declared |
| `ParentBackdropItemId` | string | not listed | not declared | not declared |
| `ParentBackdropImageTags` | array&lt;string&gt; | not listed | not declared | not declared |
| `IsPostPaddingRequired` | boolean | not listed | not declared | not declared |
| `KeepUntil` | [LiveTv.KeepUntil](models.md#model-livetv-keepuntil) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DayOfWeek](models.md#model-dayofweek).
- [LiveTv.KeepUntil](models.md#model-livetv-keepuntil).
- [LiveTv.KeywordInfo](models.md#model-livetv-keywordinfo).
- [LiveTv.TimerType](models.md#model-livetv-timertype).

<a id="model-livetv-timerinfodto"></a>

## LiveTv.TimerInfoDto

- Source pointer: `#/definitions/LiveTv.TimerInfoDto`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.LiveTv.TimerInfoDto`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Status` | [LiveTv.RecordingStatus](models.md#model-livetv-recordingstatus) | not listed | not declared | not declared |
| `SeriesTimerId` | string | not listed | not declared | not declared |
| `RunTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `ProgramInfo` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `TimerType` | [LiveTv.TimerType](models.md#model-livetv-timertype) | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Type` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |
| `ChannelId` | string | not listed | not declared | not declared |
| `ChannelName` | string | not listed | not declared | not declared |
| `ChannelNumber` | string | not listed | not declared | not declared |
| `ChannelPrimaryImageTag` | string | not listed | not declared | not declared |
| `ProgramId` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Overview` | string | not listed | not declared | not declared |
| `ParentFolderId` | string | not listed | not declared | not declared |
| `StartDate` | string (date-time) | not listed | not declared | not declared |
| `EndDate` | string (date-time) | not listed | not declared | not declared |
| `Priority` | integer (int32) | not listed | not declared | not declared |
| `PrePaddingSeconds` | integer (int32) | not listed | not declared | not declared |
| `PostPaddingSeconds` | integer (int32) | not listed | not declared | not declared |
| `IsPrePaddingRequired` | boolean | not listed | not declared | not declared |
| `ParentBackdropItemId` | string | not listed | not declared | not declared |
| `ParentBackdropImageTags` | array&lt;string&gt; | not listed | not declared | not declared |
| `IsPostPaddingRequired` | boolean | not listed | not declared | not declared |
| `KeepUntil` | [LiveTv.KeepUntil](models.md#model-livetv-keepuntil) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [LiveTv.KeepUntil](models.md#model-livetv-keepuntil).
- [LiveTv.RecordingStatus](models.md#model-livetv-recordingstatus).
- [LiveTv.TimerType](models.md#model-livetv-timertype).

<a id="model-livetv-timertype"></a>

## LiveTv.TimerType

- Source pointer: `#/definitions/LiveTv.TimerType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.LiveTv.TimerType`.
- Object-level required declaration: omitted.
- enum: `"Program"`, `"DateTime"`, `"Keyword"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-livetv-tunerhostinfo"></a>

## LiveTv.TunerHostInfo

- Source pointer: `#/definitions/LiveTv.TunerHostInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.LiveTv.TunerHostInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `Url` | string | not listed | not declared | not declared |
| `Type` | string | not listed | not declared | not declared |
| `DeviceId` | string | not listed | not declared | not declared |
| `FriendlyName` | string | not listed | not declared | not declared |
| `SetupUrl` | string | not listed | not declared | not declared |
| `ImportFavoritesOnly` | boolean | not listed | not declared | not declared |
| `PreferEpgChannelImages` | boolean | not listed | not declared | not declared |
| `PreferEpgChannelNumbers` | boolean | not listed | not declared | not declared |
| `AllowHWTranscoding` | boolean | not listed | not declared | not declared |
| `AllowMappingByNumber` | boolean | not listed | not declared | not declared |
| `ImportGuideData` | boolean | not listed | not declared | not declared |
| `Source` | string | not listed | not declared | not declared |
| `TunerCount` | integer (int32) | not listed | not declared | not declared |
| `UserAgent` | string | not listed | not declared | not declared |
| `Referrer` | string | not listed | not declared | not declared |
| `ProviderOptions` | string | not listed | not declared | not declared |
| `DataVersion` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-locationtype"></a>

## LocationType

- Source pointer: `#/definitions/LocationType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.LocationType`.
- Object-level required declaration: omitted.
- enum: `"FileSystem"`, `"Virtual"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-logfile"></a>

## LogFile

- Source pointer: `#/definitions/LogFile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.System.LogFile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |
| `DateModified` | string (date-time) | not listed | not declared | not declared |
| `Size` | integer (int64) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-logging-logseverity"></a>

## Logging.LogSeverity

- Source pointer: `#/definitions/Logging.LogSeverity`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Logging.LogSeverity`.
- Object-level required declaration: omitted.
- enum: `"Info"`, `"Debug"`, `"Warn"`, `"Error"`, `"Fatal"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-markertype"></a>

## MarkerType

- Source pointer: `#/definitions/MarkerType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.MarkerType`.
- Object-level required declaration: omitted.
- enum: `"Chapter"`, `"IntroStart"`, `"IntroEnd"`, `"CreditsStart"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-mbbackup-api-allbackupsinfo"></a>

## MBBackup.Api.AllBackupsInfo

- Source pointer: `#/definitions/MBBackup.Api.AllBackupsInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MBBackup.Api.AllBackupsInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `FullBackupInfo` | [MBBackup.BackupInfo](models.md#model-mbbackup-backupinfo) | not listed | not declared | not declared |
| `LightBackups` | array&lt;[MBBackup.BackupInfo](models.md#model-mbbackup-backupinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MBBackup.BackupInfo](models.md#model-mbbackup-backupinfo).

<a id="model-mbbackup-api-datarestoreoptions"></a>

## MBBackup.Api.DataRestoreOptions

- Source pointer: `#/definitions/MBBackup.Api.DataRestoreOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MBBackup.Api.DataRestoreOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Users` | array&lt;[MBBackup.Api.UserRestoreInfo](models.md#model-mbbackup-api-userrestoreinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MBBackup.Api.UserRestoreInfo](models.md#model-mbbackup-api-userrestoreinfo).

<a id="model-mbbackup-api-restoreoptions"></a>

## MBBackup.Api.RestoreOptions

- Source pointer: `#/definitions/MBBackup.Api.RestoreOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MBBackup.Api.RestoreOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `RestoreServerId` | boolean | not listed | not declared | not declared |
| `UseFiles` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-mbbackup-api-userrestoreinfo"></a>

## MBBackup.Api.UserRestoreInfo

- Source pointer: `#/definitions/MBBackup.Api.UserRestoreInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MBBackup.Api.UserRestoreInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SourceUserId` | string | not listed | not declared | not declared |
| `TargetUserId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-mbbackup-backupinfo"></a>

## MBBackup.BackupInfo

- Source pointer: `#/definitions/MBBackup.BackupInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MBBackup.BackupInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ServerVersion` | string | not listed | not declared | not declared |
| `PluginVersion` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `CanRestore` | boolean | not listed | not declared | not declared |
| `IsFullBackup` | boolean | not listed | not declared | not declared |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |
| `Users` | array&lt;[NameIdPair](models.md#model-nameidpair)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [NameIdPair](models.md#model-nameidpair).

<a id="model-mediaencoding-codecparametercontext"></a>

## MediaEncoding.CodecParameterContext

- Source pointer: `#/definitions/MediaEncoding.CodecParameterContext`.
- Type: string.
- Source internal type name: `MediaBrowser.Controller.MediaEncoding.CodecParameterContext`.
- Object-level required declaration: omitted.
- enum: `"Playback"`, `"Conversion"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-mediapathinfo"></a>

## MediaPathInfo

- Source pointer: `#/definitions/MediaPathInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.MediaPathInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Path` | string | not listed | not declared | not declared |
| `NetworkPath` | string | not listed | not declared | not declared |
| `Username` | string | not listed | not declared | not declared |
| `Password` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-mediaprotocol"></a>

## MediaProtocol

- Source pointer: `#/definitions/MediaProtocol`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.MediaInfo.MediaProtocol`.
- Object-level required declaration: omitted.
- enum: `"File"`, `"Http"`, `"Rtmp"`, `"Rtsp"`, `"Udp"`, `"Rtp"`, `"Ftp"`, `"Mms"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-mediasourceinfo"></a>

## MediaSourceInfo

- Source pointer: `#/definitions/MediaSourceInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.MediaSourceInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Chapters` | array&lt;[ChapterInfo](models.md#model-chapterinfo)&gt; | not listed | not declared | not declared |
| `Protocol` | [MediaProtocol](models.md#model-mediaprotocol) | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `EncoderPath` | string | not listed | not declared | not declared |
| `EncoderProtocol` | [MediaProtocol](models.md#model-mediaprotocol) | not listed | not declared | not declared |
| `Type` | [MediaSourceType](models.md#model-mediasourcetype) | not listed | not declared | not declared |
| `ProbePath` | string | not listed | not declared | not declared |
| `ProbeProtocol` | [MediaProtocol](models.md#model-mediaprotocol) | not listed | not declared | not declared |
| `Container` | string | not listed | not declared | not declared |
| `Size` | integer (int64) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `SortName` | string | not listed | not declared | not declared |
| `IsRemote` | boolean | not listed | not declared | not declared |
| `HasMixedProtocols` | boolean | not listed | not declared | not declared |
| `RunTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `ContainerStartTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `SupportsTranscoding` | boolean | not listed | not declared | not declared |
| `TrancodeLiveStartIndex` | integer (int32) | not listed | not declared | not declared |
| `WallClockStart` | string (date-time) | not listed | not declared | not declared |
| `SupportsDirectStream` | boolean | not listed | not declared | not declared |
| `SupportsDirectPlay` | boolean | not listed | not declared | not declared |
| `IsInfiniteStream` | boolean | not listed | not declared | not declared |
| `RequiresOpening` | boolean | not listed | not declared | not declared |
| `OpenToken` | string | not listed | not declared | not declared |
| `RequiresClosing` | boolean | not listed | not declared | not declared |
| `LiveStreamId` | string | not listed | not declared | not declared |
| `BufferMs` | integer (int32) | not listed | not declared | not declared |
| `RequiresLooping` | boolean | not listed | not declared | not declared |
| `SupportsProbing` | boolean | not listed | not declared | not declared |
| `Video3DFormat` | [Video3DFormat](models.md#model-video3dformat) | not listed | not declared | not declared |
| `MediaStreams` | array&lt;[MediaStream](models.md#model-mediastream)&gt; | not listed | not declared | not declared |
| `Formats` | array&lt;string&gt; | not listed | not declared | not declared |
| `Bitrate` | integer (int32) | not listed | not declared | not declared |
| `Timestamp` | [TransportStreamTimestamp](models.md#model-transportstreamtimestamp) | not listed | not declared | not declared |
| `RequiredHttpHeaders` | map&lt;string, string&gt; | not listed | not declared | not declared |
| `DirectStreamUrl` | string | not listed | not declared | not declared |
| `AddApiKeyToDirectStreamUrl` | boolean | not listed | not declared | not declared |
| `TranscodingUrl` | string | not listed | not declared | not declared |
| `TranscodingSubProtocol` | string | not listed | not declared | not declared |
| `TranscodingContainer` | string | not listed | not declared | not declared |
| `AnalyzeDurationMs` | integer (int32) | not listed | not declared | not declared |
| `ReadAtNativeFramerate` | boolean | not listed | not declared | not declared |
| `DefaultAudioStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `DefaultSubtitleStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ChapterInfo](models.md#model-chapterinfo).
- [MediaProtocol](models.md#model-mediaprotocol).
- [MediaSourceType](models.md#model-mediasourcetype).
- [MediaStream](models.md#model-mediastream).
- [TransportStreamTimestamp](models.md#model-transportstreamtimestamp).
- [Video3DFormat](models.md#model-video3dformat).

<a id="model-mediasourcetype"></a>

## MediaSourceType

- Source pointer: `#/definitions/MediaSourceType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dto.MediaSourceType`.
- Object-level required declaration: omitted.
- enum: `"Default"`, `"Grouping"`, `"Placeholder"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-mediastream"></a>

## MediaStream

- Source pointer: `#/definitions/MediaStream`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Entities.MediaStream`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Codec` | string | not listed | not declared | not declared |
| `CodecTag` | string | not listed | not declared | not declared |
| `Language` | string | not listed | not declared | not declared |
| `ColorTransfer` | string | not listed | not declared | not declared |
| `ColorPrimaries` | string | not listed | not declared | not declared |
| `ColorSpace` | string | not listed | not declared | not declared |
| `Comment` | string | not listed | not declared | not declared |
| `StreamStartTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `TimeBase` | string | not listed | not declared | not declared |
| `Title` | string | not listed | not declared | not declared |
| `Extradata` | string | not listed | not declared | not declared |
| `VideoRange` | string | not listed | not declared | not declared |
| `DisplayTitle` | string | not listed | not declared | not declared |
| `DisplayLanguage` | string | not listed | not declared | not declared |
| `NalLengthSize` | string | not listed | not declared | not declared |
| `IsInterlaced` | boolean | not listed | not declared | not declared |
| `IsAVC` | boolean | not listed | not declared | not declared |
| `ChannelLayout` | string | not listed | not declared | not declared |
| `BitRate` | integer (int32) | not listed | not declared | not declared |
| `BitDepth` | integer (int32) | not listed | not declared | not declared |
| `RefFrames` | integer (int32) | not listed | not declared | not declared |
| `Rotation` | integer (int32) | not listed | not declared | not declared |
| `Channels` | integer (int32) | not listed | not declared | not declared |
| `SampleRate` | integer (int32) | not listed | not declared | not declared |
| `IsDefault` | boolean | not listed | not declared | not declared |
| `IsForced` | boolean | not listed | not declared | not declared |
| `IsHearingImpaired` | boolean | not listed | not declared | not declared |
| `Height` | integer (int32) | not listed | not declared | not declared |
| `Width` | integer (int32) | not listed | not declared | not declared |
| `AverageFrameRate` | number (float) | not listed | not declared | not declared |
| `RealFrameRate` | number (float) | not listed | not declared | not declared |
| `Profile` | string | not listed | not declared | not declared |
| `Type` | [MediaStreamType](models.md#model-mediastreamtype) | not listed | not declared | not declared |
| `AspectRatio` | string | not listed | not declared | not declared |
| `Index` | integer (int32) | not listed | not declared | not declared |
| `IsExternal` | boolean | not listed | not declared | not declared |
| `DeliveryMethod` | [SubtitleDeliveryMethod](models.md#model-subtitledeliverymethod) | not listed | not declared | not declared |
| `DeliveryUrl` | string | not listed | not declared | not declared |
| `IsExternalUrl` | boolean | not listed | not declared | not declared |
| `IsChunkedResponse` | boolean | not listed | not declared | not declared |
| `IsTextSubtitleStream` | boolean | not listed | not declared | not declared |
| `SupportsExternalStream` | boolean | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `Protocol` | [MediaProtocol](models.md#model-mediaprotocol) | not listed | not declared | not declared |
| `PixelFormat` | string | not listed | not declared | not declared |
| `Level` | number (double) | not listed | not declared | not declared |
| `IsAnamorphic` | boolean | not listed | not declared | not declared |
| `ExtendedVideoType` | [ExtendedVideoTypes](models.md#model-extendedvideotypes) | not listed | not declared | not declared |
| `ExtendedVideoSubType` | [ExtendedVideoSubTypes](models.md#model-extendedvideosubtypes) | not listed | not declared | not declared |
| `ExtendedVideoSubTypeDescription` | string | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |
| `AttachmentSize` | integer (int32) | not listed | not declared | not declared |
| `MimeType` | string | not listed | not declared | not declared |
| `SubtitleLocationType` | [SubtitleLocationType](models.md#model-subtitlelocationtype) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ExtendedVideoSubTypes](models.md#model-extendedvideosubtypes).
- [ExtendedVideoTypes](models.md#model-extendedvideotypes).
- [MediaProtocol](models.md#model-mediaprotocol).
- [MediaStreamType](models.md#model-mediastreamtype).
- [SubtitleDeliveryMethod](models.md#model-subtitledeliverymethod).
- [SubtitleLocationType](models.md#model-subtitlelocationtype).

<a id="model-mediastreamtype"></a>

## MediaStreamType

- Source pointer: `#/definitions/MediaStreamType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.MediaStreamType`.
- Object-level required declaration: omitted.
- enum: `"Unknown"`, `"Audio"`, `"Video"`, `"Subtitle"`, `"EmbeddedImage"`, `"Attachment"`, `"Data"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-mediaurl"></a>

## MediaUrl

- Source pointer: `#/definitions/MediaUrl`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Entities.MediaUrl`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Url` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-metadataeditorinfo"></a>

## MetadataEditorInfo

- Source pointer: `#/definitions/MetadataEditorInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.MetadataEditorInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ParentalRatingOptions` | array&lt;[ParentalRating](models.md#model-parentalrating)&gt; | not listed | not declared | not declared |
| `Countries` | array&lt;[Globalization.CountryInfo](models.md#model-globalization-countryinfo)&gt; | not listed | not declared | not declared |
| `Cultures` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ExternalIdInfos` | array&lt;[ExternalIdInfo](models.md#model-externalidinfo)&gt; | not listed | not declared | not declared |
| `PersonExternalIdInfos` | array&lt;[ExternalIdInfo](models.md#model-externalidinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ExternalIdInfo](models.md#model-externalidinfo).
- [Globalization.CountryInfo](models.md#model-globalization-countryinfo).
- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ParentalRating](models.md#model-parentalrating).

<a id="model-metadatafeatures"></a>

## MetadataFeatures

- Source pointer: `#/definitions/MetadataFeatures`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Configuration.MetadataFeatures`.
- Object-level required declaration: omitted.
- enum: `"Collections"`, `"Adult"`, `"RequiredSetup"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-metadatafields"></a>

## MetadataFields

- Source pointer: `#/definitions/MetadataFields`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.MetadataFields`.
- Object-level required declaration: omitted.
- enum: see values below.

**`MetadataFields` enum values**

- `"Cast"`
- `"Genres"`
- `"ProductionLocations"`
- `"Studios"`
- `"Tags"`
- `"Name"`
- `"Overview"`
- `"Runtime"`
- `"OfficialRating"`
- `"Collections"`
- `"ChannelNumber"`
- `"SortName"`
- `"OriginalTitle"`
- `"SortIndexNumber"`
- `"SortParentIndexNumber"`
- `"CommunityRating"`
- `"CriticRating"`
- `"Tagline"`
- `"Composers"`
- `"Artists"`
- `"AlbumArtists"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-metadatarefreshmode"></a>

## MetadataRefreshMode

- Source pointer: `#/definitions/MetadataRefreshMode`.
- Type: string.
- Source internal type name: `MediaBrowser.Controller.Providers.MetadataRefreshMode`.
- Object-level required declaration: omitted.
- enum: `"ValidationOnly"`, `"Default"`, `"FullRefresh"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-movieinfo"></a>

## MovieInfo

- Source pointer: `#/definitions/MovieInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.MovieInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-musicvideoinfo"></a>

## MusicVideoInfo

- Source pointer: `#/definitions/MusicVideoInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.MusicVideoInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Artists` | array&lt;string&gt; | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-nameidpair"></a>

## NameIdPair

- Source pointer: `#/definitions/NameIdPair`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.NameIdPair`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-namelongidpair"></a>

## NameLongIdPair

- Source pointer: `#/definitions/NameLongIdPair`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.NameLongIdPair`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-namevaluepair"></a>

## NameValuePair

- Source pointer: `#/definitions/NameValuePair`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.NameValuePair`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Value` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-net-endpointinfo"></a>

## Net.EndPointInfo

- Source pointer: `#/definitions/Net.EndPointInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Net.EndPointInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `IsLocal` | boolean | not listed | not declared | not declared |
| `IsInNetwork` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-net-sockets-addressfamily"></a>

## Net.Sockets.AddressFamily

- Source pointer: `#/definitions/Net.Sockets.AddressFamily`.
- Type: string.
- Source internal type name: `System.Net.Sockets.AddressFamily`.
- Object-level required declaration: omitted.
- enum: see values below.

**`Net.Sockets.AddressFamily` enum values**

- `"Unspecified"`
- `"Unix"`
- `"InterNetwork"`
- `"ImpLink"`
- `"Pup"`
- `"Chaos"`
- `"NS"`
- `"Ipx"`
- `"Iso"`
- `"Osi"`
- `"Ecma"`
- `"DataKit"`
- `"Ccitt"`
- `"Sna"`
- `"DecNet"`
- `"DataLink"`
- `"Lat"`
- `"HyperChannel"`
- `"AppleTalk"`
- `"NetBios"`
- `"VoiceView"`
- `"FireFox"`
- `"Banyan"`
- `"Atm"`
- `"InterNetworkV6"`
- `"Cluster"`
- `"Ieee12844"`
- `"Irda"`
- `"NetworkDesigners"`
- `"Max"`
- `"Packet"`
- `"ControllerAreaNetwork"`
- `"Unknown"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-notificationcategoryinfo"></a>

## NotificationCategoryInfo

- Source pointer: `#/definitions/NotificationCategoryInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Notifications.NotificationCategoryInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Events` | array&lt;[NotificationTypeInfo](models.md#model-notificationtypeinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [NotificationTypeInfo](models.md#model-notificationtypeinfo).

<a id="model-notifications-notificationlevel"></a>

## Notifications.NotificationLevel

- Source pointer: `#/definitions/Notifications.NotificationLevel`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Notifications.NotificationLevel`.
- Object-level required declaration: omitted.
- enum: `"Normal"`, `"Warning"`, `"Error"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-notificationtypeinfo"></a>

## NotificationTypeInfo

- Source pointer: `#/definitions/NotificationTypeInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Notifications.NotificationTypeInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `CategoryName` | string | not listed | not declared | not declared |
| `CategoryId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-operatingsystem"></a>

## OperatingSystem

- Source pointer: `#/definitions/OperatingSystem`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.System.OperatingSystem`.
- Object-level required declaration: omitted.
- enum: `"Windows"`, `"Linux"`, `"OSX"`, `"BSD"`, `"Android"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-packageinfo"></a>

## PackageInfo

- Source pointer: `#/definitions/PackageInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Updates.PackageInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `id` | string | not listed | not declared | not declared |
| `name` | string | not listed | not declared | not declared |
| `shortDescription` | string | not listed | not declared | not declared |
| `overview` | string | not listed | not declared | not declared |
| `isPremium` | boolean | not listed | not declared | not declared |
| `adult` | boolean | not listed | not declared | not declared |
| `richDescUrl` | string | not listed | not declared | not declared |
| `thumbImage` | string | not listed | not declared | not declared |
| `previewImage` | string | not listed | not declared | not declared |
| `type` | string | not listed | not declared | not declared |
| `targetFilename` | string | not listed | not declared | not declared |
| `owner` | string | not listed | not declared | not declared |
| `category` | string | not listed | not declared | not declared |
| `tileColor` | string | not listed | not declared | not declared |
| `featureId` | string | not listed | not declared | not declared |
| `price` | number (float) | not listed | not declared | not declared |
| `targetSystem` | [PackageTargetSystem](models.md#model-packagetargetsystem) | not listed | not declared | not declared |
| `guid` | string | not listed | not declared | not declared |
| `isRegistered` | boolean | not listed | not declared | not declared |
| `expDate` | string (date-time) | not listed | not declared | not declared |
| `versions` | array&lt;[PackageVersionInfo](models.md#model-packageversioninfo)&gt; | not listed | not declared | not declared |
| `enableInAppStore` | boolean | not listed | not declared | not declared |
| `installs` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [PackageTargetSystem](models.md#model-packagetargetsystem).
- [PackageVersionInfo](models.md#model-packageversioninfo).

<a id="model-packagetargetsystem"></a>

## PackageTargetSystem

- Source pointer: `#/definitions/PackageTargetSystem`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Updates.PackageTargetSystem`.
- Object-level required declaration: omitted.
- enum: `"Server"`, `"MBTheater"`, `"MBClassic"`, `"Other"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-packageversionclass"></a>

## PackageVersionClass

- Source pointer: `#/definitions/PackageVersionClass`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Updates.PackageVersionClass`.
- Object-level required declaration: omitted.
- enum: `"Release"`, `"Beta"`, `"Dev"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-packageversioninfo"></a>

## PackageVersionInfo

- Source pointer: `#/definitions/PackageVersionInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Updates.PackageVersionInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `name` | string | not listed | not declared | not declared |
| `guid` | string | not listed | not declared | not declared |
| `versionStr` | string | not listed | not declared | not declared |
| `classification` | [PackageVersionClass](models.md#model-packageversionclass) | not listed | not declared | not declared |
| `description` | string | not listed | not declared | not declared |
| `requiredVersionStr` | string | not listed | not declared | not declared |
| `sourceUrl` | string | not listed | not declared | not declared |
| `checksum` | string | not listed | not declared | not declared |
| `targetFilename` | string | not listed | not declared | not declared |
| `infoUrl` | string | not listed | not declared | not declared |
| `runtimes` | string | not listed | not declared | not declared |
| `timestamp` | string (date-time) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [PackageVersionClass](models.md#model-packageversionclass).

<a id="model-parentalrating"></a>

## ParentalRating

- Source pointer: `#/definitions/ParentalRating`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Entities.ParentalRating`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Value` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-pathsubstitution"></a>

## PathSubstitution

- Source pointer: `#/definitions/PathSubstitution`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.PathSubstitution`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `From` | string | not listed | not declared | not declared |
| `To` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-persistence-introdebuginfo"></a>

## Persistence.IntroDebugInfo

- Source pointer: `#/definitions/Persistence.IntroDebugInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Persistence.IntroDebugInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | integer (int64) | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `Start` | integer (int64) | not listed | not declared | not declared |
| `End` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-personlookupinfo"></a>

## PersonLookupInfo

- Source pointer: `#/definitions/PersonLookupInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.PersonLookupInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-persontype"></a>

## PersonType

- Source pointer: `#/definitions/PersonType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.PersonType`.
- Object-level required declaration: omitted.
- enum: `"Actor"`, `"Director"`, `"Writer"`, `"Producer"`, `"GuestStar"`, `"Composer"`, `"Conductor"`, `"Lyricist"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-pinredeemresult"></a>

## PinRedeemResult

- Source pointer: `#/definitions/PinRedeemResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Users.PinRedeemResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Success` | boolean | not listed | not declared | not declared |
| `UsersReset` | array&lt;string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-playbackerrorcode"></a>

## PlaybackErrorCode

- Source pointer: `#/definitions/PlaybackErrorCode`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dlna.PlaybackErrorCode`.
- Object-level required declaration: omitted.
- enum: `"NotAllowed"`, `"NoCompatibleStream"`, `"RateLimitExceeded"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-playbackinforequest"></a>

## PlaybackInfoRequest

- Source pointer: `#/definitions/PlaybackInfoRequest`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.MediaInfo.PlaybackInfoRequest`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `MaxStreamingBitrate` | integer (int64) | not listed | not declared | not declared |
| `StartTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `AudioStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `SubtitleStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `MaxAudioChannels` | integer (int32) | not listed | not declared | not declared |
| `MediaSourceId` | string | not listed | not declared | not declared |
| `LiveStreamId` | string | not listed | not declared | not declared |
| `DeviceProfile` | [DeviceProfile](models.md#model-deviceprofile) | not listed | not declared | not declared |
| `EnableDirectPlay` | boolean | not listed | not declared | not declared |
| `EnableDirectStream` | boolean | not listed | not declared | not declared |
| `EnableTranscoding` | boolean | not listed | not declared | not declared |
| `AllowInterlacedVideoStreamCopy` | boolean | not listed | not declared | not declared |
| `AllowVideoStreamCopy` | boolean | not listed | not declared | not declared |
| `AllowAudioStreamCopy` | boolean | not listed | not declared | not declared |
| `IsPlayback` | boolean | not listed | not declared | not declared |
| `AutoOpenLiveStream` | boolean | not listed | not declared | not declared |
| `CurrentPlaySessionId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DeviceProfile](models.md#model-deviceprofile).

<a id="model-playbackinforesponse"></a>

## PlaybackInfoResponse

- Source pointer: `#/definitions/PlaybackInfoResponse`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.MediaInfo.PlaybackInfoResponse`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `MediaSources` | array&lt;[MediaSourceInfo](models.md#model-mediasourceinfo)&gt; | not listed | not declared | not declared |
| `PlaySessionId` | string | not listed | not declared | not declared |
| `ErrorCode` | [PlaybackErrorCode](models.md#model-playbackerrorcode) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MediaSourceInfo](models.md#model-mediasourceinfo).
- [PlaybackErrorCode](models.md#model-playbackerrorcode).

<a id="model-playbackprogressinfo"></a>

## PlaybackProgressInfo

- Source pointer: `#/definitions/PlaybackProgressInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.PlaybackProgressInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `CanSeek` | boolean | not listed | not declared | not declared |
| `NowPlayingQueue` | array&lt;[QueueItem](models.md#model-queueitem)&gt; | not listed | not declared | not declared |
| `PlaylistItemId` | string | not listed | not declared | not declared |
| `SessionId` | string | not listed | not declared | not declared |
| `AudioStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `SubtitleStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `IsPaused` | boolean | not listed | not declared | not declared |
| `PlaylistIndex` | integer (int32) | not listed | not declared | not declared |
| `PlaylistLength` | integer (int32) | not listed | not declared | not declared |
| `IsMuted` | boolean | not listed | not declared | not declared |
| `RunTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `PlaybackStartTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `VolumeLevel` | integer (int32) | not listed | not declared | not declared |
| `Brightness` | integer (int32) | not listed | not declared | not declared |
| `AspectRatio` | string | not listed | not declared | not declared |
| `EventName` | [ProgressEvent](models.md#model-progressevent) | not listed | not declared | not declared |
| `PlayMethod` | [PlayMethod](models.md#model-playmethod) | not listed | not declared | not declared |
| `RepeatMode` | [RepeatMode](models.md#model-repeatmode) | not listed | not declared | not declared |
| `SleepTimerMode` | [SleepTimerMode](models.md#model-sleeptimermode) | not listed | not declared | not declared |
| `SleepTimerEndTime` | string (date-time) | not listed | not declared | not declared |
| `Shuffle` | boolean | not listed | not declared | not declared |
| `SubtitleOffset` | integer (int32) | not listed | not declared | not declared |
| `PlaybackRate` | number (double) | not listed | not declared | not declared |
| `PlaylistItemIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `PlaySessionId` | string | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `LiveStreamId` | string | not listed | not declared | not declared |
| `MediaSourceId` | string | not listed | not declared | not declared |
| `Item` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `PositionTicks` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [PlayMethod](models.md#model-playmethod).
- [ProgressEvent](models.md#model-progressevent).
- [QueueItem](models.md#model-queueitem).
- [RepeatMode](models.md#model-repeatmode).
- [SleepTimerMode](models.md#model-sleeptimermode).

<a id="model-playbackstartinfo"></a>

## PlaybackStartInfo

- Source pointer: `#/definitions/PlaybackStartInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.PlaybackStartInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `CanSeek` | boolean | not listed | not declared | not declared |
| `NowPlayingQueue` | array&lt;[QueueItem](models.md#model-queueitem)&gt; | not listed | not declared | not declared |
| `PlaylistItemId` | string | not listed | not declared | not declared |
| `SessionId` | string | not listed | not declared | not declared |
| `AudioStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `SubtitleStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `IsPaused` | boolean | not listed | not declared | not declared |
| `PlaylistIndex` | integer (int32) | not listed | not declared | not declared |
| `PlaylistLength` | integer (int32) | not listed | not declared | not declared |
| `IsMuted` | boolean | not listed | not declared | not declared |
| `RunTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `PlaybackStartTimeTicks` | integer (int64) | not listed | not declared | not declared |
| `VolumeLevel` | integer (int32) | not listed | not declared | not declared |
| `Brightness` | integer (int32) | not listed | not declared | not declared |
| `AspectRatio` | string | not listed | not declared | not declared |
| `EventName` | [ProgressEvent](models.md#model-progressevent) | not listed | not declared | not declared |
| `PlayMethod` | [PlayMethod](models.md#model-playmethod) | not listed | not declared | not declared |
| `RepeatMode` | [RepeatMode](models.md#model-repeatmode) | not listed | not declared | not declared |
| `SleepTimerMode` | [SleepTimerMode](models.md#model-sleeptimermode) | not listed | not declared | not declared |
| `SleepTimerEndTime` | string (date-time) | not listed | not declared | not declared |
| `Shuffle` | boolean | not listed | not declared | not declared |
| `SubtitleOffset` | integer (int32) | not listed | not declared | not declared |
| `PlaybackRate` | number (double) | not listed | not declared | not declared |
| `PlaylistItemIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `PlaySessionId` | string | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `LiveStreamId` | string | not listed | not declared | not declared |
| `MediaSourceId` | string | not listed | not declared | not declared |
| `Item` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `PositionTicks` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [PlayMethod](models.md#model-playmethod).
- [ProgressEvent](models.md#model-progressevent).
- [QueueItem](models.md#model-queueitem).
- [RepeatMode](models.md#model-repeatmode).
- [SleepTimerMode](models.md#model-sleeptimermode).

<a id="model-playbackstopinfo"></a>

## PlaybackStopInfo

- Source pointer: `#/definitions/PlaybackStopInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.PlaybackStopInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `NowPlayingQueue` | array&lt;[QueueItem](models.md#model-queueitem)&gt; | not listed | not declared | not declared |
| `PlaylistItemId` | string | not listed | not declared | not declared |
| `PlaylistIndex` | integer (int32) | not listed | not declared | not declared |
| `PlaylistLength` | integer (int32) | not listed | not declared | not declared |
| `SessionId` | string | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `Failed` | boolean | not listed | not declared | not declared |
| `NextMediaType` | string | not listed | not declared | not declared |
| `PlaySessionId` | string | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `LiveStreamId` | string | not listed | not declared | not declared |
| `MediaSourceId` | string | not listed | not declared | not declared |
| `Item` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `PositionTicks` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [QueueItem](models.md#model-queueitem).

<a id="model-playcommand"></a>

## PlayCommand

- Source pointer: `#/definitions/PlayCommand`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Session.PlayCommand`.
- Object-level required declaration: omitted.
- enum: `"PlayNow"`, `"PlayNext"`, `"PlayLast"`, `"PlayInstantMix"`, `"PlayShuffle"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-playerstateinfo"></a>

## PlayerStateInfo

- Source pointer: `#/definitions/PlayerStateInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.PlayerStateInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PositionTicks` | integer (int64) | not listed | not declared | not declared |
| `CanSeek` | boolean | not listed | not declared | not declared |
| `IsPaused` | boolean | not listed | not declared | not declared |
| `IsMuted` | boolean | not listed | not declared | not declared |
| `VolumeLevel` | integer (int32) | not listed | not declared | not declared |
| `AudioStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `SubtitleStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `MediaSourceId` | string | not listed | not declared | not declared |
| `MediaSource` | [MediaSourceInfo](models.md#model-mediasourceinfo) | not listed | not declared | not declared |
| `PlayMethod` | [PlayMethod](models.md#model-playmethod) | not listed | not declared | not declared |
| `RepeatMode` | [RepeatMode](models.md#model-repeatmode) | not listed | not declared | not declared |
| `SleepTimerMode` | [SleepTimerMode](models.md#model-sleeptimermode) | not listed | not declared | not declared |
| `SleepTimerEndTime` | string (date-time) | not listed | not declared | not declared |
| `SubtitleOffset` | integer (int32) | not listed | not declared | not declared |
| `Shuffle` | boolean | not listed | not declared | not declared |
| `PlaybackRate` | number (double) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MediaSourceInfo](models.md#model-mediasourceinfo).
- [PlayMethod](models.md#model-playmethod).
- [RepeatMode](models.md#model-repeatmode).
- [SleepTimerMode](models.md#model-sleeptimermode).

<a id="model-playlists-addtoplaylistinfo"></a>

## Playlists.AddToPlaylistInfo

- Source pointer: `#/definitions/Playlists.AddToPlaylistInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Playlists.AddToPlaylistInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ItemCount` | integer (int32) | not listed | not declared | not declared |
| `ContainsDuplicates` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-playlists-addtoplaylistresult"></a>

## Playlists.AddToPlaylistResult

- Source pointer: `#/definitions/Playlists.AddToPlaylistResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Playlists.AddToPlaylistResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `ItemAddedCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-playlists-playlistcreationresult"></a>

## Playlists.PlaylistCreationResult

- Source pointer: `#/definitions/Playlists.PlaylistCreationResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Playlists.PlaylistCreationResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `ItemAddedCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-playmethod"></a>

## PlayMethod

- Source pointer: `#/definitions/PlayMethod`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Session.PlayMethod`.
- Object-level required declaration: omitted.
- enum: `"Transcode"`, `"DirectStream"`, `"DirectPlay"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-playrequest"></a>

## PlayRequest

- Source pointer: `#/definitions/PlayRequest`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.PlayRequest`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ControllingUserId` | string | not listed | not declared | not declared |
| `SubtitleStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `AudioStreamIndex` | integer (int32) | not listed | not declared | not declared |
| `MediaSourceId` | string | not listed | not declared | not declared |
| `StartIndex` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-playstatecommand"></a>

## PlaystateCommand

- Source pointer: `#/definitions/PlaystateCommand`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Session.PlaystateCommand`.
- Object-level required declaration: omitted.
- enum: see values below.

**`PlaystateCommand` enum values**

- `"Stop"`
- `"Pause"`
- `"Unpause"`
- `"NextTrack"`
- `"PreviousTrack"`
- `"Seek"`
- `"Rewind"`
- `"FastForward"`
- `"PlayPause"`
- `"SeekRelative"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-playstaterequest"></a>

## PlaystateRequest

- Source pointer: `#/definitions/PlaystateRequest`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.PlaystateRequest`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Command` | [PlaystateCommand](models.md#model-playstatecommand) | not listed | not declared | not declared |
| `SeekPositionTicks` | integer (int64) | not listed | not declared | not declared |
| `ControllingUserId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [PlaystateCommand](models.md#model-playstatecommand).

<a id="model-plugins-configurationpagetype"></a>

## Plugins.ConfigurationPageType

- Source pointer: `#/definitions/Plugins.ConfigurationPageType`.
- Type: string.
- Source internal type name: `MediaBrowser.Controller.Plugins.ConfigurationPageType`.
- Object-level required declaration: omitted.
- enum: `"PluginConfiguration"`, `"None"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-plugins-plugininfo"></a>

## Plugins.PluginInfo

- Source pointer: `#/definitions/Plugins.PluginInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Plugins.PluginInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Version` | string | not listed | not declared | not declared |
| `ConfigurationFileName` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `ImageTag` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-processrun-metrics-processmetricpoint"></a>

## ProcessRun.Metrics.ProcessMetricPoint

- Source pointer: `#/definitions/ProcessRun.Metrics.ProcessMetricPoint`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.ProcessRun.Metrics.ProcessMetricPoint`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Time` | string (time) | not listed | not declared | not declared |
| `CpuPercent` | number (double) | not listed | not declared | not declared |
| `VirtualMemory` | number (double) | not listed | not declared | not declared |
| `WorkingSet` | number (double) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-processrun-metrics-processstatistics"></a>

## ProcessRun.Metrics.ProcessStatistics

- Source pointer: `#/definitions/ProcessRun.Metrics.ProcessStatistics`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.ProcessRun.Metrics.ProcessStatistics`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `CurrentCpu` | number (double) | not listed | not declared | not declared |
| `AverageCpu` | number (double) | not listed | not declared | not declared |
| `CurrentVirtualMemory` | number (double) | not listed | not declared | not declared |
| `CurrentWorkingSet` | number (double) | not listed | not declared | not declared |
| `Metrics` | array&lt;[ProcessRun.Metrics.ProcessMetricPoint](models.md#model-processrun-metrics-processmetricpoint)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ProcessRun.Metrics.ProcessMetricPoint](models.md#model-processrun-metrics-processmetricpoint).

<a id="model-profilecondition"></a>

## ProfileCondition

- Source pointer: `#/definitions/ProfileCondition`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dlna.ProfileCondition`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Condition` | [ProfileConditionType](models.md#model-profileconditiontype) | not listed | not declared | not declared |
| `Property` | [ProfileConditionValue](models.md#model-profileconditionvalue) | not listed | not declared | not declared |
| `Value` | string | not listed | not declared | not declared |
| `IsRequired` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ProfileConditionType](models.md#model-profileconditiontype).
- [ProfileConditionValue](models.md#model-profileconditionvalue).

<a id="model-profileconditiontype"></a>

## ProfileConditionType

- Source pointer: `#/definitions/ProfileConditionType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dlna.ProfileConditionType`.
- Object-level required declaration: omitted.
- enum: `"Equals"`, `"NotEquals"`, `"LessThanEqual"`, `"GreaterThanEqual"`, `"EqualsAny"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-profileconditionvalue"></a>

## ProfileConditionValue

- Source pointer: `#/definitions/ProfileConditionValue`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dlna.ProfileConditionValue`.
- Object-level required declaration: omitted.
- enum: see values below.

**`ProfileConditionValue` enum values**

- `"AudioChannels"`
- `"AudioBitrate"`
- `"AudioProfile"`
- `"Width"`
- `"Height"`
- `"Has64BitOffsets"`
- `"PacketLength"`
- `"VideoBitDepth"`
- `"VideoBitrate"`
- `"VideoFramerate"`
- `"VideoLevel"`
- `"VideoProfile"`
- `"VideoTimestamp"`
- `"IsAnamorphic"`
- `"RefFrames"`
- `"NumAudioStreams"`
- `"NumVideoStreams"`
- `"IsSecondaryAudio"`
- `"VideoCodecTag"`
- `"IsAvc"`
- `"IsInterlaced"`
- `"AudioSampleRate"`
- `"AudioBitDepth"`
- `"VideoRange"`
- `"VideoRotation"`
- `"IsExternalAudio"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-profileinformation"></a>

## ProfileInformation

- Source pointer: `#/definitions/ProfileInformation`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Media.Model.Types.ProfileInformation`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ShortName` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `Details` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `BitDepths` | array&lt;integer (int32)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-profilelevelinformation"></a>

## ProfileLevelInformation

- Source pointer: `#/definitions/ProfileLevelInformation`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Media.Model.Types.ProfileLevelInformation`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Profile` | [ProfileInformation](models.md#model-profileinformation) | not listed | not declared | not declared |
| `Level` | [LevelInformation](models.md#model-levelinformation) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LevelInformation](models.md#model-levelinformation).
- [ProfileInformation](models.md#model-profileinformation).

<a id="model-progressevent"></a>

## ProgressEvent

- Source pointer: `#/definitions/ProgressEvent`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Session.ProgressEvent`.
- Object-level required declaration: omitted.
- enum: see values below.

**`ProgressEvent` enum values**

- `"TimeUpdate"`
- `"Pause"`
- `"Unpause"`
- `"VolumeChange"`
- `"RepeatModeChange"`
- `"AudioTrackChange"`
- `"SubtitleTrackChange"`
- `"PlaylistItemMove"`
- `"PlaylistItemRemove"`
- `"PlaylistItemAdd"`
- `"QualityChange"`
- `"StateChange"`
- `"SubtitleOffsetChange"`
- `"PlaybackRateChange"`
- `"ShuffleChange"`
- `"SleepTimerChange"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-provideriddictionary"></a>

## ProviderIdDictionary

- Source pointer: `#/definitions/ProviderIdDictionary`.
- Type: map&lt;string, string&gt;.
- Source internal type name: `MediaBrowser.Model.Entities.ProviderIdDictionary`.
- Object-level required declaration: omitted.
- Additional property values: string.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-proxyheadermode"></a>

## ProxyHeaderMode

- Source pointer: `#/definitions/ProxyHeaderMode`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Configuration.ProxyHeaderMode`.
- Object-level required declaration: omitted.
- enum: `"None"`, `"LanAddressesOnly"`, `"RemoteAddressesOnly"`, `"AllAddresses"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-publicsysteminfo"></a>

## PublicSystemInfo

- Source pointer: `#/definitions/PublicSystemInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.System.PublicSystemInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `LocalAddress` | string | not listed | not declared | not declared |
| `LocalAddresses` | array&lt;string&gt; | not listed | not declared | not declared |
| `WanAddress` | string | not listed | not declared | not declared |
| `RemoteAddresses` | array&lt;string&gt; | not listed | not declared | not declared |
| `ServerName` | string | not listed | not declared | not declared |
| `Version` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-queryresult_activitylogentry"></a>

## QueryResult_ActivityLogEntry

- Source pointer: `#/definitions/QueryResult_ActivityLogEntry`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[ActivityLogEntry](models.md#model-activitylogentry)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ActivityLogEntry](models.md#model-activitylogentry).

<a id="model-queryresult_api-epgrow"></a>

## QueryResult_Api.EpgRow

- Source pointer: `#/definitions/QueryResult_Api.EpgRow`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[Api.EpgRow](models.md#model-api-epgrow)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Api.EpgRow](models.md#model-api-epgrow).

<a id="model-queryresult_baseitemdto"></a>

## QueryResult_BaseItemDto

- Source pointer: `#/definitions/QueryResult_BaseItemDto`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[BaseItemDto](models.md#model-baseitemdto)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).

<a id="model-queryresult_channelmanagementinfo"></a>

## QueryResult_ChannelManagementInfo

- Source pointer: `#/definitions/QueryResult_ChannelManagementInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[ChannelManagementInfo](models.md#model-channelmanagementinfo)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ChannelManagementInfo](models.md#model-channelmanagementinfo).

<a id="model-queryresult_devices-deviceinfo"></a>

## QueryResult_Devices.DeviceInfo

- Source pointer: `#/definitions/QueryResult_Devices.DeviceInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[Devices.DeviceInfo](models.md#model-devices-deviceinfo)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Devices.DeviceInfo](models.md#model-devices-deviceinfo).

<a id="model-queryresult_livetv-seriestimerinfodto"></a>

## QueryResult_LiveTv.SeriesTimerInfoDto

- Source pointer: `#/definitions/QueryResult_LiveTv.SeriesTimerInfoDto`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[LiveTv.SeriesTimerInfoDto](models.md#model-livetv-seriestimerinfodto)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LiveTv.SeriesTimerInfoDto](models.md#model-livetv-seriestimerinfodto).

<a id="model-queryresult_livetv-timerinfodto"></a>

## QueryResult_LiveTv.TimerInfoDto

- Source pointer: `#/definitions/QueryResult_LiveTv.TimerInfoDto`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[LiveTv.TimerInfoDto](models.md#model-livetv-timerinfodto)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LiveTv.TimerInfoDto](models.md#model-livetv-timerinfodto).

<a id="model-queryresult_logfile"></a>

## QueryResult_LogFile

- Source pointer: `#/definitions/QueryResult_LogFile`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[LogFile](models.md#model-logfile)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LogFile](models.md#model-logfile).

<a id="model-queryresult_string"></a>

## QueryResult_String

- Source pointer: `#/definitions/QueryResult_String`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;string&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-queryresult_syncjob"></a>

## QueryResult_SyncJob

- Source pointer: `#/definitions/QueryResult_SyncJob`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[SyncJob](models.md#model-syncjob)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SyncJob](models.md#model-syncjob).

<a id="model-queryresult_syncjobitem"></a>

## QueryResult_SyncJobItem

- Source pointer: `#/definitions/QueryResult_SyncJobItem`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[SyncJobItem](models.md#model-syncjobitem)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SyncJobItem](models.md#model-syncjobitem).

<a id="model-queryresult_userdto"></a>

## QueryResult_UserDto

- Source pointer: `#/definitions/QueryResult_UserDto`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[UserDto](models.md#model-userdto)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [UserDto](models.md#model-userdto).

<a id="model-queryresult_userlibrary-officialratingitem"></a>

## QueryResult_UserLibrary.OfficialRatingItem

- Source pointer: `#/definitions/QueryResult_UserLibrary.OfficialRatingItem`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[UserLibrary.OfficialRatingItem](models.md#model-userlibrary-officialratingitem)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [UserLibrary.OfficialRatingItem](models.md#model-userlibrary-officialratingitem).

<a id="model-queryresult_userlibrary-tagitem"></a>

## QueryResult_UserLibrary.TagItem

- Source pointer: `#/definitions/QueryResult_UserLibrary.TagItem`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[UserLibrary.TagItem](models.md#model-userlibrary-tagitem)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [UserLibrary.TagItem](models.md#model-userlibrary-tagitem).

<a id="model-queryresult_virtualfolderinfo"></a>

## QueryResult_VirtualFolderInfo

- Source pointer: `#/definitions/QueryResult_VirtualFolderInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[VirtualFolderInfo](models.md#model-virtualfolderinfo)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [VirtualFolderInfo](models.md#model-virtualfolderinfo).

<a id="model-queueitem"></a>

## QueueItem

- Source pointer: `#/definitions/QueueItem`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.QueueItem`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | integer (int64) | not listed | not declared | not declared |
| `PlaylistItemId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-ratingtype"></a>

## RatingType

- Source pointer: `#/definitions/RatingType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dto.RatingType`.
- Object-level required declaration: omitted.
- enum: `"Score"`, `"Likes"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-recommendationdto"></a>

## RecommendationDto

- Source pointer: `#/definitions/RecommendationDto`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.RecommendationDto`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Items` | array&lt;[BaseItemDto](models.md#model-baseitemdto)&gt; | not listed | not declared | not declared |
| `RecommendationType` | [RecommendationType](models.md#model-recommendationtype) | not listed | not declared | not declared |
| `BaselineItemName` | string | not listed | not declared | not declared |
| `CategoryId` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [RecommendationType](models.md#model-recommendationtype).

<a id="model-recommendationtype"></a>

## RecommendationType

- Source pointer: `#/definitions/RecommendationType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dto.RecommendationType`.
- Object-level required declaration: omitted.
- enum: `"SimilarToRecentlyPlayed"`, `"SimilarToLikedItem"`, `"HasDirectorFromRecentlyPlayed"`, `"HasActorFromRecentlyPlayed"`, `"HasLikedDirector"`, `"HasLikedActor"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-remoteimageinfo"></a>

## RemoteImageInfo

- Source pointer: `#/definitions/RemoteImageInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Providers.RemoteImageInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ProviderName` | string | not listed | not declared | not declared |
| `Url` | string | not listed | not declared | not declared |
| `ThumbnailUrl` | string | not listed | not declared | not declared |
| `Height` | integer (int32) | not listed | not declared | not declared |
| `Width` | integer (int32) | not listed | not declared | not declared |
| `CommunityRating` | number (double) | not listed | not declared | not declared |
| `VoteCount` | integer (int32) | not listed | not declared | not declared |
| `Language` | string | not listed | not declared | not declared |
| `DisplayLanguage` | string | not listed | not declared | not declared |
| `Type` | [ImageType](models.md#model-imagetype) | not listed | not declared | not declared |
| `RatingType` | [RatingType](models.md#model-ratingtype) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ImageType](models.md#model-imagetype).
- [RatingType](models.md#model-ratingtype).

<a id="model-remoteimageresult"></a>

## RemoteImageResult

- Source pointer: `#/definitions/RemoteImageResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Providers.RemoteImageResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Images` | array&lt;[RemoteImageInfo](models.md#model-remoteimageinfo)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [RemoteImageInfo](models.md#model-remoteimageinfo).

<a id="model-remotesearchquery_albuminfo"></a>

## RemoteSearchQuery_AlbumInfo

- Source pointer: `#/definitions/RemoteSearchQuery_AlbumInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [AlbumInfo](models.md#model-albuminfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [AlbumInfo](models.md#model-albuminfo).

<a id="model-remotesearchquery_artistinfo"></a>

## RemoteSearchQuery_ArtistInfo

- Source pointer: `#/definitions/RemoteSearchQuery_ArtistInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [ArtistInfo](models.md#model-artistinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ArtistInfo](models.md#model-artistinfo).

<a id="model-remotesearchquery_bookinfo"></a>

## RemoteSearchQuery_BookInfo

- Source pointer: `#/definitions/RemoteSearchQuery_BookInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [BookInfo](models.md#model-bookinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BookInfo](models.md#model-bookinfo).

<a id="model-remotesearchquery_gameinfo"></a>

## RemoteSearchQuery_GameInfo

- Source pointer: `#/definitions/RemoteSearchQuery_GameInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [GameInfo](models.md#model-gameinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [GameInfo](models.md#model-gameinfo).

<a id="model-remotesearchquery_itemlookupinfo"></a>

## RemoteSearchQuery_ItemLookupInfo

- Source pointer: `#/definitions/RemoteSearchQuery_ItemLookupInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [ItemLookupInfo](models.md#model-itemlookupinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ItemLookupInfo](models.md#model-itemlookupinfo).

<a id="model-remotesearchquery_movieinfo"></a>

## RemoteSearchQuery_MovieInfo

- Source pointer: `#/definitions/RemoteSearchQuery_MovieInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [MovieInfo](models.md#model-movieinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MovieInfo](models.md#model-movieinfo).

<a id="model-remotesearchquery_musicvideoinfo"></a>

## RemoteSearchQuery_MusicVideoInfo

- Source pointer: `#/definitions/RemoteSearchQuery_MusicVideoInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [MusicVideoInfo](models.md#model-musicvideoinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [MusicVideoInfo](models.md#model-musicvideoinfo).

<a id="model-remotesearchquery_personlookupinfo"></a>

## RemoteSearchQuery_PersonLookupInfo

- Source pointer: `#/definitions/RemoteSearchQuery_PersonLookupInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [PersonLookupInfo](models.md#model-personlookupinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [PersonLookupInfo](models.md#model-personlookupinfo).

<a id="model-remotesearchquery_seriesinfo"></a>

## RemoteSearchQuery_SeriesInfo

- Source pointer: `#/definitions/RemoteSearchQuery_SeriesInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [SeriesInfo](models.md#model-seriesinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SeriesInfo](models.md#model-seriesinfo).

<a id="model-remotesearchquery_trailerinfo"></a>

## RemoteSearchQuery_TrailerInfo

- Source pointer: `#/definitions/RemoteSearchQuery_TrailerInfo`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SearchInfo` | [TrailerInfo](models.md#model-trailerinfo) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `Providers` | array&lt;string&gt; | not listed | not declared | not declared |
| `IncludeDisabledProviders` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [TrailerInfo](models.md#model-trailerinfo).

<a id="model-remotesearchresult"></a>

## RemoteSearchResult

- Source pointer: `#/definitions/RemoteSearchResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Providers.RemoteSearchResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `OriginalTitle` | string | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `ProductionYear` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `IndexNumberEnd` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `SortIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `SortParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `StartDate` | string (date-time) | not listed | not declared | not declared |
| `EndDate` | string (date-time) | not listed | not declared | not declared |
| `ImageUrl` | string | not listed | not declared | not declared |
| `SearchProviderName` | string | not listed | not declared | not declared |
| `GameSystem` | string | not listed | not declared | not declared |
| `Overview` | string | not listed | not declared | not declared |
| `DisambiguationComment` | string | not listed | not declared | not declared |
| `AlbumArtist` | [RemoteSearchResult](models.md#model-remotesearchresult) | not listed | not declared | not declared |
| `Artists` | array&lt;[RemoteSearchResult](models.md#model-remotesearchresult)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ProviderIdDictionary](models.md#model-provideriddictionary).
- [RemoteSearchResult](models.md#model-remotesearchresult).

<a id="model-remotesubtitleinfo"></a>

## RemoteSubtitleInfo

- Source pointer: `#/definitions/RemoteSubtitleInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Providers.RemoteSubtitleInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ThreeLetterISOLanguageName` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `ProviderName` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Format` | string | not listed | not declared | not declared |
| `Author` | string | not listed | not declared | not declared |
| `Comment` | string | not listed | not declared | not declared |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |
| `CommunityRating` | number (float) | not listed | not declared | not declared |
| `DownloadCount` | integer (int32) | not listed | not declared | not declared |
| `IsHashMatch` | boolean | not listed | not declared | not declared |
| `IsForced` | boolean | not listed | not declared | not declared |
| `IsHearingImpaired` | boolean | not listed | not declared | not declared |
| `Language` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-repeatmode"></a>

## RepeatMode

- Source pointer: `#/definitions/RepeatMode`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Session.RepeatMode`.
- Object-level required declaration: omitted.
- enum: `"RepeatNone"`, `"RepeatAll"`, `"RepeatOne"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-resolution"></a>

## Resolution

- Source pointer: `#/definitions/Resolution`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Media.Model.Types.Resolution`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Width` | integer (int32) | not listed | not declared | not declared |
| `Height` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-resolutionwithrate"></a>

## ResolutionWithRate

- Source pointer: `#/definitions/ResolutionWithRate`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Media.Model.Types.ResolutionWithRate`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Width` | integer (int32) | not listed | not declared | not declared |
| `Height` | integer (int32) | not listed | not declared | not declared |
| `FrameRate` | number (double) | not listed | not declared | not declared |
| `Resolution` | [Resolution](models.md#model-resolution) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Resolution](models.md#model-resolution).

<a id="model-responseprofile"></a>

## ResponseProfile

- Source pointer: `#/definitions/ResponseProfile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dlna.ResponseProfile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Container` | string | not listed | not declared | not declared |
| `AudioCodec` | string | not listed | not declared | not declared |
| `VideoCodec` | string | not listed | not declared | not declared |
| `Type` | [DlnaProfileType](models.md#model-dlnaprofiletype) | not listed | not declared | not declared |
| `OrgPn` | string | not listed | not declared | not declared |
| `MimeType` | string | not listed | not declared | not declared |
| `Conditions` | array&lt;[ProfileCondition](models.md#model-profilecondition)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DlnaProfileType](models.md#model-dlnaprofiletype).
- [ProfileCondition](models.md#model-profilecondition).

<a id="model-rokumetadata-api-thumbnailinfo"></a>

## RokuMetadata.Api.ThumbnailInfo

- Source pointer: `#/definitions/RokuMetadata.Api.ThumbnailInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `RokuMetadata.Api.ThumbnailInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PositionTicks` | integer (int64) | not listed | not declared | not declared |
| `ImageTag` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-rokumetadata-api-thumbnailsetinfo"></a>

## RokuMetadata.Api.ThumbnailSetInfo

- Source pointer: `#/definitions/RokuMetadata.Api.ThumbnailSetInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `RokuMetadata.Api.ThumbnailSetInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `AspectRatio` | number (double) | not listed | not declared | not declared |
| `Thumbnails` | array&lt;[RokuMetadata.Api.ThumbnailInfo](models.md#model-rokumetadata-api-thumbnailinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [RokuMetadata.Api.ThumbnailInfo](models.md#model-rokumetadata-api-thumbnailinfo).

<a id="model-runuicommand"></a>

## RunUICommand

- Source pointer: `#/definitions/RunUICommand`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericUI.Api.Endpoints.RunUICommand`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PageId` | string | not listed | not declared | not declared |
| `CommandId` | string | not listed | not declared | not declared |
| `Data` | string | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `ClientLocale` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-scrolldirection"></a>

## ScrollDirection

- Source pointer: `#/definitions/ScrollDirection`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.ScrollDirection`.
- Object-level required declaration: omitted.
- enum: `"Horizontal"`, `"Vertical"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-secondaryframeworks"></a>

## SecondaryFrameworks

- Source pointer: `#/definitions/SecondaryFrameworks`.
- Type: string.
- Source internal type name: `Emby.Media.Model.Enums.SecondaryFrameworks`.
- Object-level required declaration: omitted.
- enum: see values below.

**`SecondaryFrameworks` enum values**

- `"Unknown"`
- `"None"`
- `"AmdAmf"`
- `"MediaCodec"`
- `"NvEncDec"`
- `"OpenMax"`
- `"QuickSync"`
- `"VaApi"`
- `"V4L2"`
- `"DxVa"`
- `"D3d11va"`
- `"VideoToolbox"`
- `"Mmal"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-segmentskipmode"></a>

## SegmentSkipMode

- Source pointer: `#/definitions/SegmentSkipMode`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Configuration.SegmentSkipMode`.
- Object-level required declaration: omitted.
- enum: `"ShowButton"`, `"AutoSkip"`, `"None"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-seriesdisplayorder"></a>

## SeriesDisplayOrder

- Source pointer: `#/definitions/SeriesDisplayOrder`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.SeriesDisplayOrder`.
- Object-level required declaration: omitted.
- enum: `"Aired"`, `"Dvd"`, `"Absolute"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-seriesinfo"></a>

## SeriesInfo

- Source pointer: `#/definitions/SeriesInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.SeriesInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `EpisodeAirDate` | string (date-time) | not listed | not declared | not declared |
| `DisplayOrder` | [SeriesDisplayOrder](models.md#model-seriesdisplayorder) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).
- [SeriesDisplayOrder](models.md#model-seriesdisplayorder).

<a id="model-serverconfiguration"></a>

## ServerConfiguration

- Source pointer: `#/definitions/ServerConfiguration`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.ServerConfiguration`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `EnableUPnP` | boolean | not listed | not declared | not declared |
| `PublicPort` | integer (int32) | not listed | not declared | not declared |
| `PublicHttpsPort` | integer (int32) | not listed | not declared | not declared |
| `HttpServerPortNumber` | integer (int32) | not listed | not declared | not declared |
| `HttpsPortNumber` | integer (int32) | not listed | not declared | not declared |
| `EnableHttps` | boolean | not listed | not declared | not declared |
| `CertificatePath` | string | not listed | not declared | not declared |
| `CertificatePassword` | string | not listed | not declared | not declared |
| `IsPortAuthorized` | boolean | not listed | not declared | not declared |
| `AutoRunWebApp` | boolean | not listed | not declared | not declared |
| `EnableRemoteAccess` | boolean | not listed | not declared | not declared |
| `LogAllQueryTimes` | boolean | not listed | not declared | not declared |
| `DisableOutgoingIPv6` | boolean | not listed | not declared | not declared |
| `EnableCaseSensitiveItemIds` | boolean | not listed | not declared | not declared |
| `MetadataPath` | string | not listed | not declared | not declared |
| `MetadataNetworkPath` | string | not listed | not declared | not declared |
| `PreferredMetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `SortRemoveWords` | array&lt;string&gt; | not listed | not declared | not declared |
| `LibraryMonitorDelaySeconds` | integer (int32) | not listed | not declared | not declared |
| `EnableDashboardResponseCaching` | boolean | not listed | not declared | not declared |
| `DashboardSourcePath` | string | not listed | not declared | not declared |
| `ImageSavingConvention` | [ImageSavingConvention](models.md#model-imagesavingconvention) | not listed | not declared | not declared |
| `EnableAutomaticRestart` | boolean | not listed | not declared | not declared |
| `ServerName` | string | not listed | not declared | not declared |
| `PreferredDetectedRemoteAddressFamily` | [Net.Sockets.AddressFamily](models.md#model-net-sockets-addressfamily) | not listed | not declared | not declared |
| `WanDdns` | string | not listed | not declared | not declared |
| `UICulture` | string | not listed | not declared | not declared |
| `RemoteClientBitrateLimit` | integer (int32) | not listed | not declared | not declared |
| `LocalNetworkSubnets` | array&lt;string&gt; | not listed | not declared | not declared |
| `LocalNetworkAddresses` | array&lt;string&gt; | not listed | not declared | not declared |
| `EnableExternalContentInSuggestions` | boolean | not listed | not declared | not declared |
| `RequireHttps` | boolean | not listed | not declared | not declared |
| `IsBehindProxy` | boolean | not listed | not declared | not declared |
| `RemoteIPFilter` | array&lt;string&gt; | not listed | not declared | not declared |
| `IsRemoteIPFilterBlacklist` | boolean | not listed | not declared | not declared |
| `ImageExtractionTimeoutMs` | integer (int32) | not listed | not declared | not declared |
| `PathSubstitutions` | array&lt;[PathSubstitution](models.md#model-pathsubstitution)&gt; | not listed | not declared | not declared |
| `UninstalledPlugins` | array&lt;string&gt; | not listed | not declared | not declared |
| `CollapseVideoFolders` | boolean | not listed | not declared | not declared |
| `EnableOriginalTrackTitles` | boolean | not listed | not declared | not declared |
| `VacuumDatabaseOnStartup` | boolean | not listed | not declared | not declared |
| `SimultaneousStreamLimit` | integer (int32) | not listed | not declared | not declared |
| `DatabaseCacheSizeMB` | integer (int32) | not listed | not declared | not declared |
| `EnableSqLiteMmio` | boolean | not listed | not declared | not declared |
| `PlaylistsUpgradedToM3U` | boolean | not listed | not declared | not declared |
| `ImageExtractorUpgraded1` | boolean | not listed | not declared | not declared |
| `EnablePeopleLetterSubFolders` | boolean | not listed | not declared | not declared |
| `OptimizeDatabaseOnShutdown` | boolean | not listed | not declared | not declared |
| `DatabaseAnalysisLimit` | integer (int32) | not listed | not declared | not declared |
| `MaxLibraryDatabaseConnections` | integer (int32) | not listed | not declared | not declared |
| `MaxAuthDbConnections` | integer (int32) | not listed | not declared | not declared |
| `MaxOtherDbConnections` | integer (int32) | not listed | not declared | not declared |
| `DisableAsyncIO` | boolean | not listed | not declared | not declared |
| `MigratedToUserItemShares8` | boolean | not listed | not declared | not declared |
| `MigratedLibraryOptionsToDb` | boolean | not listed | not declared | not declared |
| `AllowLegacyLocalNetworkPassword` | boolean | not listed | not declared | not declared |
| `EnableSavedMetadataForPeople` | boolean | not listed | not declared | not declared |
| `TvChannelsRefreshed` | boolean | not listed | not declared | not declared |
| `ProxyHeaderMode` | [ProxyHeaderMode](models.md#model-proxyheadermode) | not listed | not declared | not declared |
| `IsInMaintenanceMode` | boolean | not listed | not declared | not declared |
| `MaintenanceModeMessage` | string | not listed | not declared | not declared |
| `EnableDebugLevelLogging` | boolean | not listed | not declared | not declared |
| `RevertDebugLogging` | string | not listed | not declared | not declared |
| `EnableAutoUpdate` | boolean | not listed | not declared | not declared |
| `LogFileRetentionDays` | integer (int32) | not listed | not declared | not declared |
| `RunAtStartup` | boolean | not listed | not declared | not declared |
| `IsStartupWizardCompleted` | boolean | not listed | not declared | not declared |
| `CachePath` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ImageSavingConvention](models.md#model-imagesavingconvention).
- [Net.Sockets.AddressFamily](models.md#model-net-sockets-addressfamily).
- [PathSubstitution](models.md#model-pathsubstitution).
- [ProxyHeaderMode](models.md#model-proxyheadermode).

<a id="model-session-partyinfo"></a>

## Session.PartyInfo

- Source pointer: `#/definitions/Session.PartyInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Session.PartyInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Sessions` | array&lt;[Session.SessionInfo](models.md#model-session-sessioninfo)&gt; | not listed | not declared | not declared |
| `Users` | array&lt;[Entities.User](models.md#model-entities-user)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Entities.User](models.md#model-entities-user).
- [Session.SessionInfo](models.md#model-session-sessioninfo).

<a id="model-session-partyinforesult"></a>

## Session.PartyInfoResult

- Source pointer: `#/definitions/Session.PartyInfoResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Session.PartyInfoResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PartyInfo` | [Session.PartyInfo](models.md#model-session-partyinfo) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Session.PartyInfo](models.md#model-session-partyinfo).

<a id="model-session-sessioninfo"></a>

## Session.SessionInfo

- Source pointer: `#/definitions/Session.SessionInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Session.SessionInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PlayState` | [PlayerStateInfo](models.md#model-playerstateinfo) | not listed | not declared | not declared |
| `AdditionalUsers` | array&lt;[SessionUserInfo](models.md#model-sessionuserinfo)&gt; | not listed | not declared | not declared |
| `RemoteEndPoint` | string (ipv4) | not listed | not declared | not declared |
| `Protocol` | string | not listed | not declared | not declared |
| `PlayableMediaTypes` | array&lt;string&gt; | not listed | not declared | not declared |
| `PlaylistItemId` | string | not listed | not declared | not declared |
| `PlaylistIndex` | integer (int32) | not listed | not declared | not declared |
| `PlaylistLength` | integer (int32) | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `PartyId` | string | not listed | not declared | not declared |
| `UserName` | string | not listed | not declared | not declared |
| `UserPrimaryImageTag` | string | not listed | not declared | not declared |
| `Client` | string | not listed | not declared | not declared |
| `LastActivityDate` | string (date-time) | not listed | not declared | not declared |
| `DeviceName` | string | not listed | not declared | not declared |
| `DeviceType` | string | not listed | not declared | not declared |
| `NowPlayingItem` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `InternalDeviceId` | integer (int64) | not listed | not declared | not declared |
| `DeviceId` | string | not listed | not declared | not declared |
| `ApplicationVersion` | string | not listed | not declared | not declared |
| `AppIconUrl` | string | not listed | not declared | not declared |
| `SupportedCommands` | array&lt;string&gt; | not listed | not declared | not declared |
| `TranscodingInfo` | [TranscodingInfo](models.md#model-transcodinginfo) | not listed | not declared | not declared |
| `SupportsRemoteControl` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [PlayerStateInfo](models.md#model-playerstateinfo).
- [SessionUserInfo](models.md#model-sessionuserinfo).
- [TranscodingInfo](models.md#model-transcodinginfo).

<a id="model-sessionuserinfo"></a>

## SessionUserInfo

- Source pointer: `#/definitions/SessionUserInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.SessionUserInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `UserId` | string | not listed | not declared | not declared |
| `UserName` | string | not listed | not declared | not declared |
| `UserInternalId` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-sleeptimermode"></a>

## SleepTimerMode

- Source pointer: `#/definitions/SleepTimerMode`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Session.SleepTimerMode`.
- Object-level required declaration: omitted.
- enum: `"None"`, `"AfterItem"`, `"AtTime"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-songinfo"></a>

## SongInfo

- Source pointer: `#/definitions/SongInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.SongInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `AlbumArtists` | array&lt;string&gt; | not listed | not declared | not declared |
| `Album` | string | not listed | not declared | not declared |
| `Artists` | array&lt;string&gt; | not listed | not declared | not declared |
| `Composers` | array&lt;string&gt; | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-sortorder"></a>

## SortOrder

- Source pointer: `#/definitions/SortOrder`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.SortOrder`.
- Object-level required declaration: omitted.
- enum: `"Ascending"`, `"Descending"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-subtitledeliverymethod"></a>

## SubtitleDeliveryMethod

- Source pointer: `#/definitions/SubtitleDeliveryMethod`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dlna.SubtitleDeliveryMethod`.
- Object-level required declaration: omitted.
- enum: `"Encode"`, `"Embed"`, `"External"`, `"Hls"`, `"VideoSideData"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-subtitlelocationtype"></a>

## SubtitleLocationType

- Source pointer: `#/definitions/SubtitleLocationType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.SubtitleLocationType`.
- Object-level required declaration: omitted.
- enum: `"InternalStream"`, `"VideoSideData"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-subtitleplaybackmode"></a>

## SubtitlePlaybackMode

- Source pointer: `#/definitions/SubtitlePlaybackMode`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Configuration.SubtitlePlaybackMode`.
- Object-level required declaration: omitted.
- enum: `"Default"`, `"Always"`, `"OnlyForced"`, `"None"`, `"Smart"`, `"HearingImpaired"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-subtitleprofile"></a>

## SubtitleProfile

- Source pointer: `#/definitions/SubtitleProfile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dlna.SubtitleProfile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Format` | string | not listed | not declared | not declared |
| `Method` | [SubtitleDeliveryMethod](models.md#model-subtitledeliverymethod) | not listed | not declared | not declared |
| `DidlMode` | string | not listed | not declared | not declared |
| `Language` | string | not listed | not declared | not declared |
| `Container` | string | not listed | not declared | not declared |
| `AllowChunkedResponse` | boolean | not listed | not declared | not declared |
| `Protocol` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SubtitleDeliveryMethod](models.md#model-subtitledeliverymethod).

<a id="model-subtitles-subtitledownloadresult"></a>

## Subtitles.SubtitleDownloadResult

- Source pointer: `#/definitions/Subtitles.SubtitleDownloadResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.Subtitles.SubtitleDownloadResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `NewIndex` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-synccategory"></a>

## SyncCategory

- Source pointer: `#/definitions/SyncCategory`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Sync.SyncCategory`.
- Object-level required declaration: omitted.
- enum: `"Latest"`, `"NextUp"`, `"Resume"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-syncdatarequest"></a>

## SyncDataRequest

- Source pointer: `#/definitions/SyncDataRequest`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncDataRequest`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `LocalItemIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `InternalTargetIds` | array&lt;integer (int64)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-syncdataresponse"></a>

## SyncDataResponse

- Source pointer: `#/definitions/SyncDataResponse`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncDataResponse`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ItemIdsToRemove` | array&lt;string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-syncdialogoptions"></a>

## SyncDialogOptions

- Source pointer: `#/definitions/SyncDialogOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncDialogOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Targets` | array&lt;[SyncTarget](models.md#model-synctarget)&gt; | not listed | not declared | not declared |
| `Options` | array&lt;[SyncJobOption](models.md#model-syncjoboption)&gt; | not listed | not declared | not declared |
| `QualityOptions` | array&lt;[SyncQualityOption](models.md#model-syncqualityoption)&gt; | not listed | not declared | not declared |
| `ProfileOptions` | array&lt;[SyncProfileOption](models.md#model-syncprofileoption)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SyncJobOption](models.md#model-syncjoboption).
- [SyncProfileOption](models.md#model-syncprofileoption).
- [SyncQualityOption](models.md#model-syncqualityoption).
- [SyncTarget](models.md#model-synctarget).

<a id="model-synceditem"></a>

## SyncedItem

- Source pointer: `#/definitions/SyncedItem`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncedItem`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ServerId` | string | not listed | not declared | not declared |
| `SyncJobId` | integer (int64) | not listed | not declared | not declared |
| `SyncJobName` | string | not listed | not declared | not declared |
| `SyncJobDateCreated` | string (date-time) | not listed | not declared | not declared |
| `SyncJobItemId` | integer (int64) | not listed | not declared | not declared |
| `OriginalFileName` | string | not listed | not declared | not declared |
| `Item` | [BaseItemDto](models.md#model-baseitemdto) | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `AdditionalFiles` | array&lt;[ItemFileInfo](models.md#model-itemfileinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).
- [ItemFileInfo](models.md#model-itemfileinfo).

<a id="model-synceditemprogress"></a>

## SyncedItemProgress

- Source pointer: `#/definitions/SyncedItemProgress`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncedItemProgress`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Progress` | number (double) | not listed | not declared | not declared |
| `Status` | [SyncJobItemStatus](models.md#model-syncjobitemstatus) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SyncJobItemStatus](models.md#model-syncjobitemstatus).

<a id="model-syncjob"></a>

## SyncJob

- Source pointer: `#/definitions/SyncJob`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Sync.SyncJob`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | integer (int64) | not listed | not declared | not declared |
| `TargetId` | string | not listed | not declared | not declared |
| `InternalTargetId` | integer (int64) | not listed | not declared | not declared |
| `TargetName` | string | not listed | not declared | not declared |
| `Quality` | string | not listed | not declared | not declared |
| `Bitrate` | integer (int32) | not listed | not declared | not declared |
| `Container` | string | not listed | not declared | not declared |
| `VideoCodec` | string | not listed | not declared | not declared |
| `AudioCodec` | string | not listed | not declared | not declared |
| `Profile` | string | not listed | not declared | not declared |
| `Category` | [SyncCategory](models.md#model-synccategory) | not listed | not declared | not declared |
| `ParentId` | integer (int64) | not listed | not declared | not declared |
| `Progress` | number (double) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Status` | [SyncJobStatus](models.md#model-syncjobstatus) | not listed | not declared | not declared |
| `UserId` | integer (int64) | not listed | not declared | not declared |
| `UnwatchedOnly` | boolean | not listed | not declared | not declared |
| `SyncNewContent` | boolean | not listed | not declared | not declared |
| `ItemLimit` | integer (int32) | not listed | not declared | not declared |
| `RequestedItemIds` | array&lt;integer (int64)&gt; | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |
| `DateLastModified` | string (date-time) | not listed | not declared | not declared |
| `ItemCount` | integer (int32) | not listed | not declared | not declared |
| `ParentName` | string | not listed | not declared | not declared |
| `PrimaryImageItemId` | string | not listed | not declared | not declared |
| `PrimaryImageTag` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SyncCategory](models.md#model-synccategory).
- [SyncJobStatus](models.md#model-syncjobstatus).

<a id="model-syncjobcreationresult"></a>

## SyncJobCreationResult

- Source pointer: `#/definitions/SyncJobCreationResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncJobCreationResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Job` | [SyncJob](models.md#model-syncjob) | not listed | not declared | not declared |
| `JobItems` | array&lt;[SyncJobItem](models.md#model-syncjobitem)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SyncJob](models.md#model-syncjob).
- [SyncJobItem](models.md#model-syncjobitem).

<a id="model-syncjobitem"></a>

## SyncJobItem

- Source pointer: `#/definitions/SyncJobItem`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncJobItem`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | integer (int64) | not listed | not declared | not declared |
| `JobId` | integer (int64) | not listed | not declared | not declared |
| `ItemId` | integer (int64) | not listed | not declared | not declared |
| `ItemName` | string | not listed | not declared | not declared |
| `MediaSourceId` | string | not listed | not declared | not declared |
| `MediaSource` | [MediaSourceInfo](models.md#model-mediasourceinfo) | not listed | not declared | not declared |
| `TargetId` | string | not listed | not declared | not declared |
| `InternalTargetId` | integer (int64) | not listed | not declared | not declared |
| `OutputPath` | string | not listed | not declared | not declared |
| `Status` | [SyncJobItemStatus](models.md#model-syncjobitemstatus) | not listed | not declared | not declared |
| `Progress` | number (double) | not listed | not declared | not declared |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |
| `PrimaryImageItemId` | string | not listed | not declared | not declared |
| `PrimaryImageTag` | string | not listed | not declared | not declared |
| `TemporaryPath` | string | not listed | not declared | not declared |
| `AdditionalFiles` | array&lt;[ItemFileInfo](models.md#model-itemfileinfo)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ItemFileInfo](models.md#model-itemfileinfo).
- [MediaSourceInfo](models.md#model-mediasourceinfo).
- [SyncJobItemStatus](models.md#model-syncjobitemstatus).

<a id="model-syncjobitemstatus"></a>

## SyncJobItemStatus

- Source pointer: `#/definitions/SyncJobItemStatus`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Sync.SyncJobItemStatus`.
- Object-level required declaration: omitted.
- enum: `"Queued"`, `"Converting"`, `"ReadyToTransfer"`, `"Transferring"`, `"Synced"`, `"Failed"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-syncjoboption"></a>

## SyncJobOption

- Source pointer: `#/definitions/SyncJobOption`.
- Type: string.
- Source internal type name: `Emby.Server.Sync.Model.SyncJobOption`.
- Object-level required declaration: omitted.
- enum: `"Name"`, `"Quality"`, `"UnwatchedOnly"`, `"SyncNewContent"`, `"ItemLimit"`, `"Profile"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-syncjobrequest"></a>

## SyncJobRequest

- Source pointer: `#/definitions/SyncJobRequest`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncJobRequest`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `TargetId` | string | not listed | not declared | not declared |
| `ItemIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `Category` | [SyncCategory](models.md#model-synccategory) | not listed | not declared | not declared |
| `ParentId` | string | not listed | not declared | not declared |
| `Quality` | string | not listed | not declared | not declared |
| `Profile` | string | not listed | not declared | not declared |
| `Container` | string | not listed | not declared | not declared |
| `VideoCodec` | string | not listed | not declared | not declared |
| `AudioCodec` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `UnwatchedOnly` | boolean | not listed | not declared | not declared |
| `SyncNewContent` | boolean | not listed | not declared | not declared |
| `ItemLimit` | integer (int32) | not listed | not declared | not declared |
| `Bitrate` | integer (int32) | not listed | not declared | not declared |
| `Downloaded` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SyncCategory](models.md#model-synccategory).

<a id="model-syncjobstatus"></a>

## SyncJobStatus

- Source pointer: `#/definitions/SyncJobStatus`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Sync.SyncJobStatus`.
- Object-level required declaration: omitted.
- enum: `"Queued"`, `"Converting"`, `"ReadyToTransfer"`, `"Transferring"`, `"Completed"`, `"CompletedWithError"`, `"Failed"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-syncprofileoption"></a>

## SyncProfileOption

- Source pointer: `#/definitions/SyncProfileOption`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncProfileOption`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `IsDefault` | boolean | not listed | not declared | not declared |
| `EnableQualityOptions` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-syncqualityoption"></a>

## SyncQualityOption

- Source pointer: `#/definitions/SyncQualityOption`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.Sync.Model.SyncQualityOption`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `IsDefault` | boolean | not listed | not declared | not declared |
| `IsOriginalQuality` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-synctarget"></a>

## SyncTarget

- Source pointer: `#/definitions/SyncTarget`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Sync.SyncTarget`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-systemevent"></a>

## SystemEvent

- Source pointer: `#/definitions/SystemEvent`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Tasks.SystemEvent`.
- Object-level required declaration: omitted.
- enum: `"WakeFromSleep"`, `"DisplayConfigurationChange"`, `"NetworkChange"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-systeminfo"></a>

## SystemInfo

- Source pointer: `#/definitions/SystemInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.System.SystemInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `SystemUpdateLevel` | [PackageVersionClass](models.md#model-packageversionclass) | not listed | not declared | not declared |
| `OperatingSystemDisplayName` | string | not listed | not declared | not declared |
| `PackageName` | string | not listed | not declared | not declared |
| `HasPendingRestart` | boolean | not listed | not declared | not declared |
| `IsShuttingDown` | boolean | not listed | not declared | not declared |
| `HasImageEnhancers` | boolean | not listed | not declared | not declared |
| `OperatingSystem` | string | not listed | not declared | not declared |
| `SupportsLibraryMonitor` | boolean | not listed | not declared | not declared |
| `SupportsLocalPortConfiguration` | boolean | not listed | not declared | not declared |
| `SupportsWakeServer` | boolean | not listed | not declared | not declared |
| `WebSocketPortNumber` | integer (int32) | not listed | not declared | not declared |
| `CompletedInstallations` | array&lt;[InstallationInfo](models.md#model-installationinfo)&gt; | not listed | not declared | not declared |
| `CanSelfRestart` | boolean | not listed | not declared | not declared |
| `CanSelfUpdate` | boolean | not listed | not declared | not declared |
| `CanLaunchWebBrowser` | boolean | not listed | not declared | not declared |
| `ProgramDataPath` | string | not listed | not declared | not declared |
| `ItemsByNamePath` | string | not listed | not declared | not declared |
| `CachePath` | string | not listed | not declared | not declared |
| `LogPath` | string | not listed | not declared | not declared |
| `InternalMetadataPath` | string | not listed | not declared | not declared |
| `TranscodingTempPath` | string | not listed | not declared | not declared |
| `HttpServerPortNumber` | integer (int32) | not listed | not declared | not declared |
| `SupportsHttps` | boolean | not listed | not declared | not declared |
| `HttpsPortNumber` | integer (int32) | not listed | not declared | not declared |
| `HasUpdateAvailable` | boolean | not listed | not declared | not declared |
| `SupportsAutoRunAtStartup` | boolean | not listed | not declared | not declared |
| `HardwareAccelerationRequiresPremiere` | boolean | not listed | not declared | not declared |
| `WakeOnLanInfo` | array&lt;[WakeOnLanInfo](models.md#model-wakeonlaninfo)&gt; | not listed | not declared | not declared |
| `IsInMaintenanceMode` | boolean | not listed | not declared | not declared |
| `LocalAddress` | string | not listed | not declared | not declared |
| `LocalAddresses` | array&lt;string&gt; | not listed | not declared | not declared |
| `WanAddress` | string | not listed | not declared | not declared |
| `RemoteAddresses` | array&lt;string&gt; | not listed | not declared | not declared |
| `ServerName` | string | not listed | not declared | not declared |
| `Version` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [InstallationInfo](models.md#model-installationinfo).
- [PackageVersionClass](models.md#model-packageversionclass).
- [WakeOnLanInfo](models.md#model-wakeonlaninfo).

<a id="model-taskcompletionstatus"></a>

## TaskCompletionStatus

- Source pointer: `#/definitions/TaskCompletionStatus`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Tasks.TaskCompletionStatus`.
- Object-level required declaration: omitted.
- enum: `"Completed"`, `"Failed"`, `"Cancelled"`, `"Aborted"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-taskinfo"></a>

## TaskInfo

- Source pointer: `#/definitions/TaskInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Tasks.TaskInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `State` | [TaskState](models.md#model-taskstate) | not listed | not declared | not declared |
| `CurrentProgressPercentage` | number (double) | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `LastExecutionResult` | [TaskResult](models.md#model-taskresult) | not listed | not declared | not declared |
| `Triggers` | array&lt;[TaskTriggerInfo](models.md#model-tasktriggerinfo)&gt; | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `Category` | string | not listed | not declared | not declared |
| `IsHidden` | boolean | not listed | not declared | not declared |
| `Key` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [TaskResult](models.md#model-taskresult).
- [TaskState](models.md#model-taskstate).
- [TaskTriggerInfo](models.md#model-tasktriggerinfo).

<a id="model-taskresult"></a>

## TaskResult

- Source pointer: `#/definitions/TaskResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Tasks.TaskResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `StartTimeUtc` | string (date-time) | not listed | not declared | not declared |
| `EndTimeUtc` | string (date-time) | not listed | not declared | not declared |
| `Status` | [TaskCompletionStatus](models.md#model-taskcompletionstatus) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Key` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `ErrorMessage` | string | not listed | not declared | not declared |
| `LongErrorMessage` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [TaskCompletionStatus](models.md#model-taskcompletionstatus).

<a id="model-taskstate"></a>

## TaskState

- Source pointer: `#/definitions/TaskState`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Tasks.TaskState`.
- Object-level required declaration: omitted.
- enum: `"Idle"`, `"Cancelling"`, `"Running"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-tasktriggerinfo"></a>

## TaskTriggerInfo

- Source pointer: `#/definitions/TaskTriggerInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Tasks.TaskTriggerInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Type` | string | not listed | not declared | not declared |
| `TimeOfDayTicks` | integer (int64) | not listed | not declared | not declared |
| `IntervalTicks` | integer (int64) | not listed | not declared | not declared |
| `SystemEvent` | [SystemEvent](models.md#model-systemevent) | not listed | not declared | not declared |
| `DayOfWeek` | [DayOfWeek](models.md#model-dayofweek) | not listed | not declared | not declared |
| `MaxRuntimeTicks` | integer (int64) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DayOfWeek](models.md#model-dayofweek).
- [SystemEvent](models.md#model-systemevent).

<a id="model-textsectioninfo"></a>

## TextSectionInfo

- Source pointer: `#/definitions/TextSectionInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Entities.TextSectionInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Text` | string | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Level` | [Notifications.NotificationLevel](models.md#model-notifications-notificationlevel) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Notifications.NotificationLevel](models.md#model-notifications-notificationlevel).

<a id="model-thememediaresult"></a>

## ThemeMediaResult

- Source pointer: `#/definitions/ThemeMediaResult`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Querying.ThemeMediaResult`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `OwnerId` | integer (int64) | not listed | not declared | not declared |
| `Items` | array&lt;[BaseItemDto](models.md#model-baseitemdto)&gt; | not listed | not declared | not declared |
| `TotalRecordCount` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BaseItemDto](models.md#model-baseitemdto).

<a id="model-trailerinfo"></a>

## TrailerInfo

- Source pointer: `#/definitions/TrailerInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Controller.Providers.TrailerInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Path` | string | not listed | not declared | not declared |
| `MetadataLanguage` | string | not listed | not declared | not declared |
| `MetadataCountryCode` | string | not listed | not declared | not declared |
| `MetadataLanguages` | array&lt;[Globalization.CultureDto](models.md#model-globalization-culturedto)&gt; | not listed | not declared | not declared |
| `ProviderIds` | [ProviderIdDictionary](models.md#model-provideriddictionary) | not listed | not declared | not declared |
| `Year` | integer (int32) | not listed | not declared | not declared |
| `IndexNumber` | integer (int32) | not listed | not declared | not declared |
| `ParentIndexNumber` | integer (int32) | not listed | not declared | not declared |
| `PremiereDate` | string (date-time) | not listed | not declared | not declared |
| `IsAutomated` | boolean | not listed | not declared | not declared |
| `EnableAdultMetadata` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Globalization.CultureDto](models.md#model-globalization-culturedto).
- [ProviderIdDictionary](models.md#model-provideriddictionary).

<a id="model-transcodereason"></a>

## TranscodeReason

- Source pointer: `#/definitions/TranscodeReason`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Session.TranscodeReason`.
- Object-level required declaration: omitted.
- enum: see values below.

**`TranscodeReason` enum values**

- `"ContainerNotSupported"`
- `"VideoCodecNotSupported"`
- `"AudioCodecNotSupported"`
- `"ContainerBitrateExceedsLimit"`
- `"AudioBitrateNotSupported"`
- `"AudioChannelsNotSupported"`
- `"VideoResolutionNotSupported"`
- `"UnknownVideoStreamInfo"`
- `"UnknownAudioStreamInfo"`
- `"AudioProfileNotSupported"`
- `"AudioSampleRateNotSupported"`
- `"AnamorphicVideoNotSupported"`
- `"InterlacedVideoNotSupported"`
- `"SecondaryAudioNotSupported"`
- `"RefFramesNotSupported"`
- `"VideoBitDepthNotSupported"`
- `"VideoBitrateNotSupported"`
- `"VideoFramerateNotSupported"`
- `"VideoLevelNotSupported"`
- `"VideoProfileNotSupported"`
- `"AudioBitDepthNotSupported"`
- `"SubtitleCodecNotSupported"`
- `"DirectPlayError"`
- `"VideoRangeNotSupported"`
- `"SubtitleContentOptionsEnabled"`
- `"ExternalAudioNotSupported"`
- `"AudioDelayNotSupported"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-transcodeseekinfo"></a>

## TranscodeSeekInfo

- Source pointer: `#/definitions/TranscodeSeekInfo`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dlna.TranscodeSeekInfo`.
- Object-level required declaration: omitted.
- enum: `"Auto"`, `"Bytes"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-transcoding-vpstepinfo"></a>

## Transcoding.VpStepInfo

- Source pointer: `#/definitions/Transcoding.VpStepInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.Transcoding.VpStepInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `StepType` | [Transcoding.VpStepTypes](models.md#model-transcoding-vpsteptypes) | not listed | not declared | not declared |
| `StepTypeName` | string | not listed | not declared | not declared |
| `HardwareContextName` | string | not listed | not declared | not declared |
| `IsHardwareContext` | boolean | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Short` | string | not listed | not declared | not declared |
| `FfmpegName` | string | not listed | not declared | not declared |
| `FfmpegDescription` | string | not listed | not declared | not declared |
| `FfmpegOptions` | string | not listed | not declared | not declared |
| `Param` | string | not listed | not declared | not declared |
| `ParamShort` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Transcoding.VpStepTypes](models.md#model-transcoding-vpsteptypes).

<a id="model-transcoding-vpsteptypes"></a>

## Transcoding.VpStepTypes

- Source pointer: `#/definitions/Transcoding.VpStepTypes`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Session.Transcoding.VpStepTypes`.
- Object-level required declaration: omitted.
- enum: see values below.

**`Transcoding.VpStepTypes` enum values**

- `"Decoder"`
- `"Encoder"`
- `"Scaling"`
- `"Deinterlace"`
- `"SubtitleOverlay"`
- `"ToneMapping"`
- `"ColorConversion"`
- `"SplitCaptions"`
- `"TextSub2Video"`
- `"GraphicSub2Video"`
- `"GraphicSub2Text"`
- `"BurnInTextSubs"`
- `"BurnInGraphicSubs"`
- `"ScaleSubs"`
- `"TextMod"`
- `"Censor"`
- `"ShowSpeaker"`
- `"StripStyles"`
- `"ConnectTo"`
- `"Rotate"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-transcodinginfo"></a>

## TranscodingInfo

- Source pointer: `#/definitions/TranscodingInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Session.TranscodingInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `AudioCodec` | string | not listed | not declared | not declared |
| `VideoCodec` | string | not listed | not declared | not declared |
| `SubProtocol` | string | not listed | not declared | not declared |
| `Container` | string | not listed | not declared | not declared |
| `IsVideoDirect` | boolean | not listed | not declared | not declared |
| `IsAudioDirect` | boolean | not listed | not declared | not declared |
| `Bitrate` | integer (int32) | not listed | not declared | not declared |
| `AudioBitrate` | integer (int32) | not listed | not declared | not declared |
| `VideoBitrate` | integer (int32) | not listed | not declared | not declared |
| `Framerate` | number (float) | not listed | not declared | not declared |
| `CompletionPercentage` | number (double) | not listed | not declared | not declared |
| `TranscodingPositionTicks` | number (double) | not listed | not declared | not declared |
| `TranscodingStartPositionTicks` | number (double) | not listed | not declared | not declared |
| `Width` | integer (int32) | not listed | not declared | not declared |
| `Height` | integer (int32) | not listed | not declared | not declared |
| `AudioChannels` | integer (int32) | not listed | not declared | not declared |
| `TranscodeReasons` | array&lt;[TranscodeReason](models.md#model-transcodereason)&gt; | not listed | not declared | not declared |
| `CurrentCpuUsage` | number (double) | not listed | not declared | not declared |
| `AverageCpuUsage` | number (double) | not listed | not declared | not declared |
| `CpuHistory` | array&lt;[Tuple_Double-Double](models.md#model-tuple_double-double)&gt; | not listed | not declared | not declared |
| `ProcessStatistics` | [ProcessRun.Metrics.ProcessStatistics](models.md#model-processrun-metrics-processstatistics) | not listed | not declared | not declared |
| `CurrentThrottle` | integer (int32) | not listed | not declared | not declared |
| `VideoDecoder` | string | not listed | not declared | not declared |
| `VideoDecoderIsHardware` | boolean | not listed | not declared | not declared |
| `VideoDecoderMediaType` | string | not listed | not declared | not declared |
| `VideoDecoderHwAccel` | string | not listed | not declared | not declared |
| `VideoEncoder` | string | not listed | not declared | not declared |
| `VideoEncoderIsHardware` | boolean | not listed | not declared | not declared |
| `VideoEncoderMediaType` | string | not listed | not declared | not declared |
| `VideoEncoderHwAccel` | string | not listed | not declared | not declared |
| `VideoPipelineInfo` | array&lt;[Transcoding.VpStepInfo](models.md#model-transcoding-vpstepinfo)&gt; | not listed | not declared | not declared |
| `SubtitlePipelineInfos` | array&lt;array&lt;[Transcoding.VpStepInfo](models.md#model-transcoding-vpstepinfo)&gt;&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ProcessRun.Metrics.ProcessStatistics](models.md#model-processrun-metrics-processstatistics).
- [TranscodeReason](models.md#model-transcodereason).
- [Transcoding.VpStepInfo](models.md#model-transcoding-vpstepinfo).
- [Tuple_Double-Double](models.md#model-tuple_double-double).

<a id="model-transcodingprofile"></a>

## TranscodingProfile

- Source pointer: `#/definitions/TranscodingProfile`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dlna.TranscodingProfile`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Container` | string | not listed | not declared | not declared |
| `Type` | [DlnaProfileType](models.md#model-dlnaprofiletype) | not listed | not declared | not declared |
| `VideoCodec` | string | not listed | not declared | not declared |
| `AudioCodec` | string | not listed | not declared | not declared |
| `Protocol` | string | not listed | not declared | not declared |
| `EstimateContentLength` | boolean | not listed | not declared | not declared |
| `EnableMpegtsM2TsMode` | boolean | not listed | not declared | not declared |
| `TranscodeSeekInfo` | [TranscodeSeekInfo](models.md#model-transcodeseekinfo) | not listed | not declared | not declared |
| `CopyTimestamps` | boolean | not listed | not declared | not declared |
| `Context` | [EncodingContext](models.md#model-encodingcontext) | not listed | not declared | not declared |
| `MaxAudioChannels` | string | not listed | not declared | not declared |
| `MinSegments` | integer (int32) | not listed | not declared | not declared |
| `SegmentLength` | integer (int32) | not listed | not declared | not declared |
| `BreakOnNonKeyFrames` | boolean | not listed | not declared | not declared |
| `AllowInterlacedVideoStreamCopy` | boolean | not listed | not declared | not declared |
| `ManifestSubtitles` | string | not listed | not declared | not declared |
| `MaxManifestSubtitles` | integer (int32) | not listed | not declared | not declared |
| `MaxWidth` | integer (int32) | not listed | not declared | not declared |
| `MaxHeight` | integer (int32) | not listed | not declared | not declared |
| `FillEmptySubtitleSegments` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [DlnaProfileType](models.md#model-dlnaprofiletype).
- [EncodingContext](models.md#model-encodingcontext).
- [TranscodeSeekInfo](models.md#model-transcodeseekinfo).

<a id="model-transportstreamtimestamp"></a>

## TransportStreamTimestamp

- Source pointer: `#/definitions/TransportStreamTimestamp`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.MediaInfo.TransportStreamTimestamp`.
- Object-level required declaration: omitted.
- enum: `"None"`, `"Zero"`, `"Valid"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-tuple_double-double"></a>

## Tuple_Double-Double

- Source pointer: `#/definitions/Tuple_Double-Double`.
- Type: object (inline fields; see source pointer).
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Item1` | number (double) | not listed | not declared | not declared |
| `Item2` | number (double) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-typeoptions"></a>

## TypeOptions

- Source pointer: `#/definitions/TypeOptions`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.TypeOptions`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Type` | string | not listed | not declared | not declared |
| `MetadataFetchers` | array&lt;string&gt; | not listed | not declared | not declared |
| `MetadataFetcherOrder` | array&lt;string&gt; | not listed | not declared | not declared |
| `ImageFetchers` | array&lt;string&gt; | not listed | not declared | not declared |
| `ImageFetcherOrder` | array&lt;string&gt; | not listed | not declared | not declared |
| `ImageOptions` | array&lt;[ImageOption](models.md#model-imageoption)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [ImageOption](models.md#model-imageoption).

<a id="model-uicommand"></a>

## UICommand

- Source pointer: `#/definitions/UICommand`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericUI.Model.UICommand`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `CommandType` | [Enums.UICommandType](models.md#model-enums-uicommandtype) | not listed | not declared | not declared |
| `CommandId` | string | not listed | not declared | not declared |
| `IsVisible` | boolean | not listed | not declared | not declared |
| `IsEnabled` | boolean | not listed | not declared | not declared |
| `Caption` | string | not listed | not declared | not declared |
| `SetFocus` | boolean | not listed | not declared | not declared |
| `ConfirmationPrompt` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Enums.UICommandType](models.md#model-enums-uicommandtype).

<a id="model-uitabpageinfo"></a>

## UITabPageInfo

- Source pointer: `#/definitions/UITabPageInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericUI.Model.UITabPageInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `PageId` | string | not listed | not declared | not declared |
| `DisplayName` | string | not listed | not declared | not declared |
| `PluginId` | string | not listed | not declared | not declared |
| `Href` | string | not listed | not declared | not declared |
| `NavKey` | string | not listed | not declared | not declared |
| `Index` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-uiviewinfo"></a>

## UIViewInfo

- Source pointer: `#/definitions/UIViewInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Web.GenericUI.Model.UIViewInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ViewId` | string | not listed | not declared | not declared |
| `PageId` | string | not listed | not declared | not declared |
| `Caption` | string | not listed | not declared | not declared |
| `SubCaption` | string | not listed | not declared | not declared |
| `PluginId` | string | not listed | not declared | not declared |
| `ViewType` | [Enums.UIViewType](models.md#model-enums-uiviewtype) | not listed | not declared | not declared |
| `ShowDialogFullScreen` | boolean | not listed | not declared | not declared |
| `IsInSequence` | boolean | not listed | not declared | not declared |
| `RedirectViewUrl` | string | not listed | not declared | not declared |
| `EditObjectContainer` | [GenericEdit.IEditObjectContainer](models.md#model-genericedit-ieditobjectcontainer) | not listed | not declared | not declared |
| `Commands` | array&lt;[UICommand](models.md#model-uicommand)&gt; | not listed | not declared | not declared |
| `TabPageInfos` | array&lt;[UITabPageInfo](models.md#model-uitabpageinfo)&gt; | not listed | not declared | not declared |
| `IsPageChangeInfo` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Enums.UIViewType](models.md#model-enums-uiviewtype).
- [GenericEdit.IEditObjectContainer](models.md#model-genericedit-ieditobjectcontainer).
- [UICommand](models.md#model-uicommand).
- [UITabPageInfo](models.md#model-uitabpageinfo).

<a id="model-unrateditem"></a>

## UnratedItem

- Source pointer: `#/definitions/UnratedItem`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Configuration.UnratedItem`.
- Object-level required declaration: omitted.
- enum: see values below.

**`UnratedItem` enum values**

- `"Movie"`
- `"Trailer"`
- `"Series"`
- `"Music"`
- `"Game"`
- `"Book"`
- `"LiveTvChannel"`
- `"LiveTvProgram"`
- `"ChannelContent"`
- `"Other"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-updateuserpassword"></a>

## UpdateUserPassword

- Source pointer: `#/definitions/UpdateUserPassword`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.UpdateUserPassword`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `NewPw` | string | not listed | not declared | not declared |
| `ResetPassword` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-useraction"></a>

## UserAction

- Source pointer: `#/definitions/UserAction`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Users.UserAction`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Id` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `Type` | [UserActionType](models.md#model-useractiontype) | not listed | not declared | not declared |
| `Date` | string (date-time) | not listed | not declared | not declared |
| `PositionTicks` | integer (int64) | not listed | not declared | not declared |
| `Played` | boolean | not listed | not declared | not declared |
| `IsFavorite` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [UserActionType](models.md#model-useractiontype).

<a id="model-useractiontype"></a>

## UserActionType

- Source pointer: `#/definitions/UserActionType`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Users.UserActionType`.
- Object-level required declaration: omitted.
- enum: `"PlayedItem"`, `"MarkPlayed"`, `"MarkFavorite"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-userconfiguration"></a>

## UserConfiguration

- Source pointer: `#/definitions/UserConfiguration`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Configuration.UserConfiguration`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `AudioLanguagePreference` | string | not listed | not declared | not declared |
| `PlayDefaultAudioTrack` | boolean | not listed | not declared | not declared |
| `SubtitleLanguagePreference` | string | not listed | not declared | not declared |
| `ProfilePin` | string | not listed | not declared | not declared |
| `DisplayMissingEpisodes` | boolean | not listed | not declared | not declared |
| `SubtitleMode` | [SubtitlePlaybackMode](models.md#model-subtitleplaybackmode) | not listed | not declared | not declared |
| `OrderedViews` | array&lt;string&gt; | not listed | not declared | not declared |
| `LatestItemsExcludes` | array&lt;string&gt; | not listed | not declared | not declared |
| `MyMediaExcludes` | array&lt;string&gt; | not listed | not declared | not declared |
| `HidePlayedInLatest` | boolean | not listed | not declared | not declared |
| `HidePlayedInMoreLikeThis` | boolean | not listed | not declared | not declared |
| `HidePlayedInSuggestions` | boolean | not listed | not declared | not declared |
| `RememberAudioSelections` | boolean | not listed | not declared | not declared |
| `RememberSubtitleSelections` | boolean | not listed | not declared | not declared |
| `EnableNextEpisodeAutoPlay` | boolean | not listed | not declared | not declared |
| `ResumeRewindSeconds` | integer (int32) | not listed | not declared | not declared |
| `IntroSkipMode` | [SegmentSkipMode](models.md#model-segmentskipmode) | not listed | not declared | not declared |
| `EnableLocalPassword` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [SegmentSkipMode](models.md#model-segmentskipmode).
- [SubtitlePlaybackMode](models.md#model-subtitleplaybackmode).

<a id="model-userdto"></a>

## UserDto

- Source pointer: `#/definitions/UserDto`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.UserDto`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |
| `ServerName` | string | not listed | not declared | not declared |
| `Prefix` | string | not listed | not declared | not declared |
| `ConnectUserName` | string | not listed | not declared | not declared |
| `DateCreated` | string (date-time) | not listed | not declared | not declared |
| `ConnectLinkType` | [Connect.UserLinkType](models.md#model-connect-userlinktype) | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `PrimaryImageTag` | string | not listed | not declared | not declared |
| `HasPassword` | boolean | not listed | not declared | not declared |
| `HasConfiguredPassword` | boolean | not listed | not declared | not declared |
| `EnableAutoLogin` | boolean | not listed | not declared | not declared |
| `LastLoginDate` | string (date-time) | not listed | not declared | not declared |
| `LastActivityDate` | string (date-time) | not listed | not declared | not declared |
| `Configuration` | [UserConfiguration](models.md#model-userconfiguration) | not listed | not declared | not declared |
| `Policy` | [UserPolicy](models.md#model-userpolicy) | not listed | not declared | not declared |
| `PrimaryImageAspectRatio` | number (double) | not listed | not declared | not declared |
| `UserItemShareLevel` | [UserItemShareLevel](models.md#model-useritemsharelevel) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [Connect.UserLinkType](models.md#model-connect-userlinktype).
- [UserConfiguration](models.md#model-userconfiguration).
- [UserItemShareLevel](models.md#model-useritemsharelevel).
- [UserPolicy](models.md#model-userpolicy).

<a id="model-useritemdatadto"></a>

## UserItemDataDto

- Source pointer: `#/definitions/UserItemDataDto`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Dto.UserItemDataDto`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Rating` | number (double) | not listed | not declared | not declared |
| `PlayedPercentage` | number (double) | not listed | not declared | not declared |
| `UnplayedItemCount` | integer (int32) | not listed | not declared | not declared |
| `PlaybackPositionTicks` | integer (int64) | not listed | not declared | not declared |
| `PlayCount` | integer (int32) | not listed | not declared | not declared |
| `IsFavorite` | boolean | not listed | not declared | not declared |
| `LastPlayedDate` | string (date-time) | not listed | not declared | not declared |
| `Played` | boolean | not listed | not declared | not declared |
| `Key` | string | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `ServerId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-useritemsharelevel"></a>

## UserItemShareLevel

- Source pointer: `#/definitions/UserItemShareLevel`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Dto.UserItemShareLevel`.
- Object-level required declaration: omitted.
- enum: `"None"`, `"Read"`, `"Write"`, `"Manage"`, `"ManageDelete"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-userlibrary-addtags"></a>

## UserLibrary.AddTags

- Source pointer: `#/definitions/UserLibrary.AddTags`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.UserLibrary.AddTags`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Tags` | array&lt;[NameIdPair](models.md#model-nameidpair)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [NameIdPair](models.md#model-nameidpair).

<a id="model-userlibrary-leaveshareditems"></a>

## UserLibrary.LeaveSharedItems

- Source pointer: `#/definitions/UserLibrary.LeaveSharedItems`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.UserLibrary.LeaveSharedItems`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ItemIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-userlibrary-officialratingitem"></a>

## UserLibrary.OfficialRatingItem

- Source pointer: `#/definitions/UserLibrary.OfficialRatingItem`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.UserLibrary.OfficialRatingItem`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-userlibrary-removetags"></a>

## UserLibrary.RemoveTags

- Source pointer: `#/definitions/UserLibrary.RemoveTags`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.UserLibrary.RemoveTags`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Tags` | array&lt;[NameIdPair](models.md#model-nameidpair)&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [NameIdPair](models.md#model-nameidpair).

<a id="model-userlibrary-tagitem"></a>

## UserLibrary.TagItem

- Source pointer: `#/definitions/UserLibrary.TagItem`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.UserLibrary.TagItem`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-userlibrary-updateuseritemaccess"></a>

## UserLibrary.UpdateUserItemAccess

- Source pointer: `#/definitions/UserLibrary.UpdateUserItemAccess`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.UserLibrary.UpdateUserItemAccess`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ItemIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `UserIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `ItemAccess` | [UserItemShareLevel](models.md#model-useritemsharelevel) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [UserItemShareLevel](models.md#model-useritemsharelevel).

<a id="model-usernotificationinfo"></a>

## UserNotificationInfo

- Source pointer: `#/definitions/UserNotificationInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Notifications.UserNotificationInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `NotifierKey` | string | not listed | not declared | not declared |
| `SetupModuleUrl` | string | not listed | not declared | not declared |
| `ServiceName` | string | not listed | not declared | not declared |
| `PluginId` | string | not listed | not declared | not declared |
| `FriendlyName` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Enabled` | boolean | not listed | not declared | not declared |
| `UserIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `DeviceIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `LibraryIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `EventIds` | array&lt;string&gt; | not listed | not declared | not declared |
| `UserId` | string | not listed | not declared | not declared |
| `IsSelfNotification` | boolean | not listed | not declared | not declared |
| `GroupItems` | boolean | not listed | not declared | not declared |
| `Options` | map&lt;string, string&gt; | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-userpolicy"></a>

## UserPolicy

- Source pointer: `#/definitions/UserPolicy`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Users.UserPolicy`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `IsAdministrator` | boolean | not listed | not declared | not declared |
| `IsHidden` | boolean | not listed | not declared | not declared |
| `IsHiddenRemotely` | boolean | not listed | not declared | not declared |
| `IsHiddenFromUnusedDevices` | boolean | not listed | not declared | not declared |
| `IsDisabled` | boolean | not listed | not declared | not declared |
| `LockedOutDate` | integer (int64) | not listed | not declared | not declared |
| `MaxParentalRating` | integer (int32) | not listed | not declared | not declared |
| `AllowTagOrRating` | boolean | not listed | not declared | not declared |
| `BlockedTags` | array&lt;string&gt; | not listed | not declared | not declared |
| `IsTagBlockingModeInclusive` | boolean | not listed | not declared | not declared |
| `IncludeTags` | array&lt;string&gt; | not listed | not declared | not declared |
| `EnableUserPreferenceAccess` | boolean | not listed | not declared | not declared |
| `AccessSchedules` | array&lt;[AccessSchedule](models.md#model-accessschedule)&gt; | not listed | not declared | not declared |
| `BlockUnratedItems` | array&lt;[UnratedItem](models.md#model-unrateditem)&gt; | not listed | not declared | not declared |
| `EnableRemoteControlOfOtherUsers` | boolean | not listed | not declared | not declared |
| `EnableSharedDeviceControl` | boolean | not listed | not declared | not declared |
| `EnableRemoteAccess` | boolean | not listed | not declared | not declared |
| `EnableLiveTvManagement` | boolean | not listed | not declared | not declared |
| `EnableLiveTvAccess` | boolean | not listed | not declared | not declared |
| `EnableMediaPlayback` | boolean | not listed | not declared | not declared |
| `EnableAudioPlaybackTranscoding` | boolean | not listed | not declared | not declared |
| `EnableVideoPlaybackTranscoding` | boolean | not listed | not declared | not declared |
| `AutoRemoteQuality` | integer (int32) | not listed | not declared | not declared |
| `EnablePlaybackRemuxing` | boolean | not listed | not declared | not declared |
| `EnableContentDeletion` | boolean | not listed | not declared | not declared |
| `RestrictedFeatures` | array&lt;string&gt; | not listed | not declared | not declared |
| `EnableContentDeletionFromFolders` | array&lt;string&gt; | not listed | not declared | not declared |
| `EnableContentDownloading` | boolean | not listed | not declared | not declared |
| `EnableSubtitleDownloading` | boolean | not listed | not declared | not declared |
| `EnableSubtitleManagement` | boolean | not listed | not declared | not declared |
| `EnableSyncTranscoding` | boolean | not listed | not declared | not declared |
| `EnableMediaConversion` | boolean | not listed | not declared | not declared |
| `EnabledChannels` | array&lt;string&gt; | not listed | not declared | not declared |
| `EnableAllChannels` | boolean | not listed | not declared | not declared |
| `EnabledFolders` | array&lt;string&gt; | not listed | not declared | not declared |
| `EnableAllFolders` | boolean | not listed | not declared | not declared |
| `InvalidLoginAttemptCount` | integer (int32) | not listed | not declared | not declared |
| `EnablePublicSharing` | boolean | not listed | not declared | not declared |
| `RemoteClientBitrateLimit` | integer (int32) | not listed | not declared | not declared |
| `AuthenticationProviderId` | string | not listed | not declared | not declared |
| `ExcludedSubFolders` | array&lt;string&gt; | not listed | not declared | not declared |
| `SimultaneousStreamLimit` | integer (int32) | not listed | not declared | not declared |
| `EnabledDevices` | array&lt;string&gt; | not listed | not declared | not declared |
| `EnableAllDevices` | boolean | not listed | not declared | not declared |
| `AllowCameraUpload` | boolean | not listed | not declared | not declared |
| `AllowSharingPersonalItems` | boolean | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [AccessSchedule](models.md#model-accessschedule).
- [UnratedItem](models.md#model-unrateditem).

<a id="model-validatepath"></a>

## ValidatePath

- Source pointer: `#/definitions/ValidatePath`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Api.ValidatePath`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `ValidateWriteable` | boolean | not listed | not declared | not declared |
| `IsFile` | boolean | not listed | not declared | not declared |
| `Username` | string | not listed | not declared | not declared |
| `Password` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-version"></a>

## Version

- Source pointer: `#/definitions/Version`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `System.Version`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Major` | integer (int32) | not listed | not declared | not declared |
| `Minor` | integer (int32) | not listed | not declared | not declared |
| `Build` | integer (int32) | not listed | not declared | not declared |
| `Revision` | integer (int32) | not listed | not declared | not declared |
| `MajorRevision` | integer (int32) | not listed | not declared | not declared |
| `MinorRevision` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

<a id="model-video3dformat"></a>

## Video3DFormat

- Source pointer: `#/definitions/Video3DFormat`.
- Type: string.
- Source internal type name: `MediaBrowser.Model.Entities.Video3DFormat`.
- Object-level required declaration: omitted.
- enum: `"HalfSideBySide"`, `"FullSideBySide"`, `"FullTopAndBottom"`, `"HalfTopAndBottom"`, `"MVC"`.

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-videocodecbase"></a>

## VideoCodecBase

- Source pointer: `#/definitions/VideoCodecBase`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `Emby.Server.MediaEncoding.Codecs.VideoCodecs.VideoCodecBase`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `CodecDeviceInfo` | [Common.Interfaces.ICodecDeviceInfo](models.md#model-common-interfaces-icodecdeviceinfo) | not listed | not declared | not declared |
| `CodecKind` | [CodecKinds](models.md#model-codeckinds) | not listed | not declared | not declared |
| `MediaTypeName` | string | not listed | not declared | not declared |
| `VideoMediaType` | [VideoMediaTypes](models.md#model-videomediatypes) | not listed | not declared | not declared |
| `MinWidth` | integer (int32) | not listed | not declared | not declared |
| `MaxWidth` | integer (int32) | not listed | not declared | not declared |
| `MinHeight` | integer (int32) | not listed | not declared | not declared |
| `MaxHeight` | integer (int32) | not listed | not declared | not declared |
| `WidthAlignment` | integer (int32) | not listed | not declared | not declared |
| `HeightAlignment` | integer (int32) | not listed | not declared | not declared |
| `MaxBitRate` | [BitRate](models.md#model-bitrate) | not listed | not declared | not declared |
| `SupportedColorFormats` | array&lt;[ColorFormats](models.md#model-colorformats)&gt; | not listed | not declared | not declared |
| `SupportedColorFormatStrings` | array&lt;string&gt; | not listed | not declared | not declared |
| `ProfileAndLevelInformation` | array&lt;[ProfileLevelInformation](models.md#model-profilelevelinformation)&gt; | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Direction` | [CodecDirections](models.md#model-codecdirections) | not listed | not declared | not declared |
| `Name` | string | not listed | not declared | not declared |
| `Description` | string | not listed | not declared | not declared |
| `FrameworkCodec` | string | not listed | not declared | not declared |
| `IsHardwareCodec` | boolean | not listed | not declared | not declared |
| `SecondaryFramework` | [SecondaryFrameworks](models.md#model-secondaryframeworks) | not listed | not declared | not declared |
| `SecondaryFrameworkCodec` | string | not listed | not declared | not declared |
| `MaxInstanceCount` | integer (int32) | not listed | not declared | not declared |
| `IsEnabledByDefault` | boolean | not listed | not declared | not declared |
| `DefaultPriority` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [BitRate](models.md#model-bitrate).
- [CodecDirections](models.md#model-codecdirections).
- [CodecKinds](models.md#model-codeckinds).
- [ColorFormats](models.md#model-colorformats).
- [Common.Interfaces.ICodecDeviceInfo](models.md#model-common-interfaces-icodecdeviceinfo).
- [ProfileLevelInformation](models.md#model-profilelevelinformation).
- [SecondaryFrameworks](models.md#model-secondaryframeworks).
- [VideoMediaTypes](models.md#model-videomediatypes).

<a id="model-videomediatypes"></a>

## VideoMediaTypes

- Source pointer: `#/definitions/VideoMediaTypes`.
- Type: string.
- Source internal type name: `Emby.Media.Model.Enums.VideoMediaTypes`.
- Object-level required declaration: omitted.
- enum: see values below.

**`VideoMediaTypes` enum values**

- `"Unknown"`
- `"copy"`
- `"flv1"`
- `"h263"`
- `"h263p"`
- `"h264"`
- `"hevc"`
- `"mjpeg"`
- `"mpeg1video"`
- `"mpeg2video"`
- `"mpeg4"`
- `"msvideo1"`
- `"theora"`
- `"vc1image"`
- `"vc1"`
- `"vp8"`
- `"vp9"`
- `"wmv1"`
- `"wmv2"`
- `"wmv3"`
- `"_012v"`
- `"_4xm"`
- `"_8bps"`
- `"a64_multi"`
- `"a64_multi5"`
- `"aasc"`
- `"aic"`
- `"alias_pix"`
- `"amv"`
- `"anm"`
- `"ansi"`
- `"apng"`
- `"asv1"`
- `"asv2"`
- `"aura"`
- `"aura2"`
- `"av1"`
- `"avrn"`
- `"avrp"`
- `"avs"`
- `"avui"`
- `"ayuv"`
- `"bethsoftvid"`
- `"bfi"`
- `"binkvideo"`
- `"bintext"`
- `"bitpacked"`
- `"bmp"`
- `"bmv_video"`
- `"brender_pix"`
- `"c93"`
- `"cavs"`
- `"cdgraphics"`
- `"cdxl"`
- `"cfhd"`
- `"cinepak"`
- `"clearvideo"`
- `"cljr"`
- `"cllc"`
- `"cmv"`
- `"cpia"`
- `"cscd"`
- `"cyuv"`
- `"daala"`
- `"dds"`
- `"dfa"`
- `"dirac"`
- `"dnxhd"`
- `"dpx"`
- `"dsicinvideo"`
- `"dvvideo"`
- `"dxa"`
- `"dxtory"`
- `"dxv"`
- `"escape124"`
- `"escape130"`
- `"exr"`
- `"ffv1"`
- `"ffvhuff"`
- `"fic"`
- `"fits"`
- `"flashsv"`
- `"flashsv2"`
- `"flic"`
- `"fmvc"`
- `"fraps"`
- `"frwu"`
- `"g2m"`
- `"gdv"`
- `"gif"`
- `"h261"`
- `"h263i"`
- `"hap"`
- `"hnm4video"`
- `"hq_hqa"`
- `"hqx"`
- `"huffyuv"`
- `"idcin"`
- `"idf"`
- `"iff_ilbm"`
- `"indeo2"`
- `"indeo3"`
- `"indeo4"`
- `"indeo5"`
- `"interplayvideo"`
- `"jpeg2000"`
- `"jpegls"`
- `"jv"`
- `"kgv1"`
- `"kmvc"`
- `"lagarith"`
- `"ljpeg"`
- `"loco"`
- `"m101"`
- `"mad"`
- `"magicyuv"`
- `"mdec"`
- `"mimic"`
- `"mjpegb"`
- `"mmvideo"`
- `"motionpixels"`
- `"msa1"`
- `"mscc"`
- `"msmpeg4v1"`
- `"msmpeg4v2"`
- `"msmpeg4v3"`
- `"msrle"`
- `"mss1"`
- `"mss2"`
- `"mszh"`
- `"mts2"`
- `"mvc1"`
- `"mvc2"`
- `"mxpeg"`
- `"nuv"`
- `"paf_video"`
- `"pam"`
- `"pbm"`
- `"pcx"`
- `"pgm"`
- `"pgmyuv"`
- `"pictor"`
- `"pixlet"`
- `"png"`
- `"ppm"`
- `"prores"`
- `"psd"`
- `"ptx"`
- `"qdraw"`
- `"qpeg"`
- `"qtrle"`
- `"r10k"`
- `"r210"`
- `"rawvideo"`
- `"rl2"`
- `"roq"`
- `"rpza"`
- `"rscc"`
- `"rv10"`
- `"rv20"`
- `"rv30"`
- `"rv40"`
- `"sanm"`
- `"scpr"`
- `"screenpresso"`
- `"sgi"`
- `"sgirle"`
- `"sheervideo"`
- `"smackvideo"`
- `"smc"`
- `"smvjpeg"`
- `"snow"`
- `"sp5x"`
- `"speedhq"`
- `"srgc"`
- `"sunrast"`
- `"svg"`
- `"svq1"`
- `"svq3"`
- `"targa"`
- `"targa_y216"`
- `"tdsc"`
- `"tgq"`
- `"tgv"`
- `"thp"`
- `"tiertexseqvideo"`
- `"tiff"`
- `"tmv"`
- `"tqi"`
- `"truemotion1"`
- `"truemotion2"`
- `"truemotion2rt"`
- `"tscc"`
- `"tscc2"`
- `"txd"`
- `"ulti"`
- `"utvideo"`
- `"v210"`
- `"v210x"`
- `"v308"`
- `"v408"`
- `"v410"`
- `"vb"`
- `"vble"`
- `"vcr1"`
- `"vixl"`
- `"vmdvideo"`
- `"vmnc"`
- `"vp3"`
- `"vp5"`
- `"vp6"`
- `"vp6a"`
- `"vp6f"`
- `"vp7"`
- `"webp"`
- `"wmv3image"`
- `"wnv1"`
- `"wrapped_avframe"`
- `"ws_vqa"`
- `"xan_wc3"`
- `"xan_wc4"`
- `"xbin"`
- `"xbm"`
- `"xface"`
- `"xpm"`
- `"xwd"`
- `"y41p"`
- `"ylc"`
- `"yop"`
- `"yuv4"`
- `"zerocodec"`
- `"zlib"`
- `"zmbv"`

No named properties are declared on this definition. Its scalar, enum, array, map, or composition
declarations above and the full source schema define its shape.

<a id="model-virtualfolderinfo"></a>

## VirtualFolderInfo

- Source pointer: `#/definitions/VirtualFolderInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.Entities.VirtualFolderInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `Name` | string | not listed | not declared | not declared |
| `Locations` | array&lt;string&gt; | not listed | not declared | not declared |
| `CollectionType` | string | not listed | not declared | not declared |
| `LibraryOptions` | [LibraryOptions](models.md#model-libraryoptions) | not listed | not declared | not declared |
| `ItemId` | string | not listed | not declared | not declared |
| `Id` | string | not listed | not declared | not declared |
| `Guid` | string | not listed | not declared | not declared |
| `PrimaryImageItemId` | string | not listed | not declared | not declared |
| `PrimaryImageTag` | string | not listed | not declared | not declared |
| `RefreshProgress` | number (double) | not listed | not declared | not declared |
| `RefreshStatus` | string | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.

Immediate referenced definitions:

- [LibraryOptions](models.md#model-libraryoptions).

<a id="model-wakeonlaninfo"></a>

## WakeOnLanInfo

- Source pointer: `#/definitions/WakeOnLanInfo`.
- Type: object (inline fields; see source pointer).
- Source internal type name: `MediaBrowser.Model.System.WakeOnLanInfo`.
- Object-level required declaration: omitted.

| Field | Type or reference | In required array | Default / enum | Other constraints |
| --- | --- | --- | --- | --- |
| `MacAddress` | string | not listed | not declared | not declared |
| `BroadcastAddress` | string | not listed | not declared | not declared |
| `Port` | integer (int32) | not listed | not declared | not declared |

Field source pointers append `/properties/{field}` to the definition pointer, using JSON Pointer escaping.
