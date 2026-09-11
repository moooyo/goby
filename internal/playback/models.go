package playback

// Request describes the official Emby playback information request.
// Pointer fields distinguish omitted values from explicitly supplied zero values.
type Request struct {
	ID                             string         `json:"Id,omitempty"`
	UserID                         string         `json:"UserId,omitempty"`
	MaxStreamingBitrate            *int64         `json:"MaxStreamingBitrate,omitempty"`
	StartTimeTicks                 *int64         `json:"StartTimeTicks,omitempty"`
	AudioStreamIndex               *int           `json:"AudioStreamIndex,omitempty"`
	SubtitleStreamIndex            *int           `json:"SubtitleStreamIndex,omitempty"`
	MaxAudioChannels               *int           `json:"MaxAudioChannels,omitempty"`
	MediaSourceID                  string         `json:"MediaSourceId,omitempty"`
	LiveStreamID                   string         `json:"LiveStreamId,omitempty"`
	DeviceProfile                  *DeviceProfile `json:"DeviceProfile,omitempty"`
	EnableDirectPlay               *bool          `json:"EnableDirectPlay,omitempty"`
	EnableDirectStream             *bool          `json:"EnableDirectStream,omitempty"`
	EnableTranscoding              *bool          `json:"EnableTranscoding,omitempty"`
	AllowInterlacedVideoStreamCopy *bool          `json:"AllowInterlacedVideoStreamCopy,omitempty"`
	AllowVideoStreamCopy           *bool          `json:"AllowVideoStreamCopy,omitempty"`
	AllowAudioStreamCopy           *bool          `json:"AllowAudioStreamCopy,omitempty"`
	IsPlayback                     *bool          `json:"IsPlayback,omitempty"`
	AutoOpenLiveStream             *bool          `json:"AutoOpenLiveStream,omitempty"`
	CurrentPlaySessionID           string         `json:"CurrentPlaySessionId,omitempty"`
}

// PlaybackInfoRequest retains the official schema name for Request.
type PlaybackInfoRequest = Request

// DeviceProfile describes the media capabilities declared by a client.
type DeviceProfile struct {
	Name                             string               `json:"Name,omitempty"`
	ID                               string               `json:"Id,omitempty"`
	SupportedMediaTypes              string               `json:"SupportedMediaTypes,omitempty"`
	MaxStreamingBitrate              *int64               `json:"MaxStreamingBitrate,omitempty"`
	MaxStaticBitrate                 *int64               `json:"MaxStaticBitrate,omitempty"`
	MusicStreamingTranscodingBitrate *int                 `json:"MusicStreamingTranscodingBitrate,omitempty"`
	MaxStaticMusicBitrate            *int                 `json:"MaxStaticMusicBitrate,omitempty"`
	DeclaredFeatures                 []string             `json:"DeclaredFeatures,omitempty"`
	DirectPlayProfiles               []DirectPlayProfile  `json:"DirectPlayProfiles,omitempty"`
	TranscodingProfiles              []TranscodingProfile `json:"TranscodingProfiles,omitempty"`
	ContainerProfiles                []ContainerProfile   `json:"ContainerProfiles,omitempty"`
	CodecProfiles                    []CodecProfile       `json:"CodecProfiles,omitempty"`
	ResponseProfiles                 []ResponseProfile    `json:"ResponseProfiles,omitempty"`
	SubtitleProfiles                 []SubtitleProfile    `json:"SubtitleProfiles,omitempty"`
}

// DirectPlayProfile declares a container and codecs that a client can play.
type DirectPlayProfile struct {
	Container  string          `json:"Container,omitempty"`
	AudioCodec string          `json:"AudioCodec,omitempty"`
	VideoCodec string          `json:"VideoCodec,omitempty"`
	Type       DlnaProfileType `json:"Type,omitempty"`
}

// ContainerProfile applies conditions to a media container.
type ContainerProfile struct {
	Type       DlnaProfileType    `json:"Type,omitempty"`
	Conditions []ProfileCondition `json:"Conditions,omitempty"`
	Container  string             `json:"Container,omitempty"`
}

// CodecProfile applies codec conditions when its applicability conditions match.
type CodecProfile struct {
	Type            CodecType          `json:"Type,omitempty"`
	Conditions      []ProfileCondition `json:"Conditions,omitempty"`
	ApplyConditions []ProfileCondition `json:"ApplyConditions,omitempty"`
	Codec           string             `json:"Codec,omitempty"`
	Container       string             `json:"Container,omitempty"`
}

// ProfileCondition compares a media property with a declared value.
type ProfileCondition struct {
	Condition  ProfileConditionType  `json:"Condition,omitempty"`
	Property   ProfileConditionValue `json:"Property,omitempty"`
	Value      string                `json:"Value,omitempty"`
	IsRequired *bool                 `json:"IsRequired,omitempty"`
}

// ResponseProfile describes response metadata for matching media.
type ResponseProfile struct {
	Container  string             `json:"Container,omitempty"`
	AudioCodec string             `json:"AudioCodec,omitempty"`
	VideoCodec string             `json:"VideoCodec,omitempty"`
	Type       DlnaProfileType    `json:"Type,omitempty"`
	OrgPN      string             `json:"OrgPn,omitempty"`
	MimeType   string             `json:"MimeType,omitempty"`
	Conditions []ProfileCondition `json:"Conditions,omitempty"`
}

// TranscodingProfile declares the output formats and settings a client accepts.
type TranscodingProfile struct {
	Container                      string            `json:"Container,omitempty"`
	Type                           DlnaProfileType   `json:"Type,omitempty"`
	VideoCodec                     string            `json:"VideoCodec,omitempty"`
	AudioCodec                     string            `json:"AudioCodec,omitempty"`
	Protocol                       string            `json:"Protocol,omitempty"`
	EstimateContentLength          *bool             `json:"EstimateContentLength,omitempty"`
	EnableMpegtsM2TsMode           *bool             `json:"EnableMpegtsM2TsMode,omitempty"`
	TranscodeSeekInfo              TranscodeSeekInfo `json:"TranscodeSeekInfo,omitempty"`
	CopyTimestamps                 *bool             `json:"CopyTimestamps,omitempty"`
	Context                        EncodingContext   `json:"Context,omitempty"`
	MaxAudioChannels               string            `json:"MaxAudioChannels,omitempty"`
	MinSegments                    *int              `json:"MinSegments,omitempty"`
	SegmentLength                  *int              `json:"SegmentLength,omitempty"`
	BreakOnNonKeyFrames            *bool             `json:"BreakOnNonKeyFrames,omitempty"`
	AllowInterlacedVideoStreamCopy *bool             `json:"AllowInterlacedVideoStreamCopy,omitempty"`
	ManifestSubtitles              string            `json:"ManifestSubtitles,omitempty"`
	MaxManifestSubtitles           *int              `json:"MaxManifestSubtitles,omitempty"`
	MaxWidth                       *int              `json:"MaxWidth,omitempty"`
	MaxHeight                      *int              `json:"MaxHeight,omitempty"`
	FillEmptySubtitleSegments      *bool             `json:"FillEmptySubtitleSegments,omitempty"`
}

// SubtitleProfile declares a supported subtitle delivery format.
type SubtitleProfile struct {
	Format               string                 `json:"Format,omitempty"`
	Method               SubtitleDeliveryMethod `json:"Method,omitempty"`
	DIDLMode             string                 `json:"DidlMode,omitempty"`
	Language             string                 `json:"Language,omitempty"`
	Container            string                 `json:"Container,omitempty"`
	AllowChunkedResponse *bool                  `json:"AllowChunkedResponse,omitempty"`
	Protocol             string                 `json:"Protocol,omitempty"`
}

// DlnaProfileType identifies the media kind covered by a profile.
type DlnaProfileType string

const (
	DlnaProfileTypeAudio DlnaProfileType = "Audio"
	DlnaProfileTypeVideo DlnaProfileType = "Video"
	DlnaProfileTypePhoto DlnaProfileType = "Photo"
)

// CodecType identifies the media stream kind covered by a codec profile.
type CodecType string

const (
	CodecTypeVideo      CodecType = "Video"
	CodecTypeVideoAudio CodecType = "VideoAudio"
	CodecTypeAudio      CodecType = "Audio"
)

// ProfileConditionType identifies a comparison operation.
type ProfileConditionType string

const (
	ProfileConditionTypeEquals           ProfileConditionType = "Equals"
	ProfileConditionTypeNotEquals        ProfileConditionType = "NotEquals"
	ProfileConditionTypeLessThanEqual    ProfileConditionType = "LessThanEqual"
	ProfileConditionTypeGreaterThanEqual ProfileConditionType = "GreaterThanEqual"
	ProfileConditionTypeEqualsAny        ProfileConditionType = "EqualsAny"
)

// ProfileConditionValue identifies a media property used in a condition.
type ProfileConditionValue string

const (
	ProfileConditionValueAudioChannels    ProfileConditionValue = "AudioChannels"
	ProfileConditionValueAudioBitrate     ProfileConditionValue = "AudioBitrate"
	ProfileConditionValueAudioProfile     ProfileConditionValue = "AudioProfile"
	ProfileConditionValueWidth            ProfileConditionValue = "Width"
	ProfileConditionValueHeight           ProfileConditionValue = "Height"
	ProfileConditionValueHas64BitOffsets  ProfileConditionValue = "Has64BitOffsets"
	ProfileConditionValuePacketLength     ProfileConditionValue = "PacketLength"
	ProfileConditionValueVideoBitDepth    ProfileConditionValue = "VideoBitDepth"
	ProfileConditionValueVideoBitrate     ProfileConditionValue = "VideoBitrate"
	ProfileConditionValueVideoFramerate   ProfileConditionValue = "VideoFramerate"
	ProfileConditionValueVideoLevel       ProfileConditionValue = "VideoLevel"
	ProfileConditionValueVideoProfile     ProfileConditionValue = "VideoProfile"
	ProfileConditionValueVideoTimestamp   ProfileConditionValue = "VideoTimestamp"
	ProfileConditionValueIsAnamorphic     ProfileConditionValue = "IsAnamorphic"
	ProfileConditionValueRefFrames        ProfileConditionValue = "RefFrames"
	ProfileConditionValueNumAudioStreams  ProfileConditionValue = "NumAudioStreams"
	ProfileConditionValueNumVideoStreams  ProfileConditionValue = "NumVideoStreams"
	ProfileConditionValueIsSecondaryAudio ProfileConditionValue = "IsSecondaryAudio"
	ProfileConditionValueVideoCodecTag    ProfileConditionValue = "VideoCodecTag"
	ProfileConditionValueIsAvc            ProfileConditionValue = "IsAvc"
	ProfileConditionValueIsInterlaced     ProfileConditionValue = "IsInterlaced"
	ProfileConditionValueAudioSampleRate  ProfileConditionValue = "AudioSampleRate"
	ProfileConditionValueAudioBitDepth    ProfileConditionValue = "AudioBitDepth"
	ProfileConditionValueVideoRange       ProfileConditionValue = "VideoRange"
	ProfileConditionValueVideoRotation    ProfileConditionValue = "VideoRotation"
	ProfileConditionValueIsExternalAudio  ProfileConditionValue = "IsExternalAudio"
)

// TranscodeSeekInfo identifies the seek information a transcoded response uses.
type TranscodeSeekInfo string

const (
	TranscodeSeekInfoAuto  TranscodeSeekInfo = "Auto"
	TranscodeSeekInfoBytes TranscodeSeekInfo = "Bytes"
)

// EncodingContext identifies how the encoded media is delivered.
type EncodingContext string

const (
	EncodingContextStreaming EncodingContext = "Streaming"
	EncodingContextStatic    EncodingContext = "Static"
)

// SubtitleDeliveryMethod identifies how subtitle data reaches the client.
type SubtitleDeliveryMethod string

const (
	SubtitleDeliveryMethodEncode        SubtitleDeliveryMethod = "Encode"
	SubtitleDeliveryMethodEmbed         SubtitleDeliveryMethod = "Embed"
	SubtitleDeliveryMethodExternal      SubtitleDeliveryMethod = "External"
	SubtitleDeliveryMethodHls           SubtitleDeliveryMethod = "Hls"
	SubtitleDeliveryMethodVideoSideData SubtitleDeliveryMethod = "VideoSideData"
)
