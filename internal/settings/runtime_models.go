package settings

import (
	"github.com/moooyo/goby/internal/transcode"
)

// NetworkValues describes the requested socket binding, not an observed
// listener. Runtime owns the active address and pending-restart projection.
type NetworkValues struct {
	BindHost string
	HttpPort int
}

type NetworkOverrides struct {
	BindHost *string
	HttpPort *int
}

// HardwareSelection contains only an opaque deployment-authorized identity.
// Filesystem paths and loader environment never enter managed settings.
type HardwareSelection struct {
	Decode   string
	Encode   string
	DeviceID string
}

// TranscodingPolicy contains portable admission policy. Encoder identities and
// resource limits remain separate from these output and extraction choices.
type TranscodingPolicy struct {
	EnableHevcEncoding                 bool
	EnableHardwareEncoding             bool
	DownMixAudioBoost                  float64
	EnableSubtitleExtraction           bool
	EnableOnTheFlyAttachmentExtraction bool
}

// DefaultTranscodingPolicy preserves existing installations' behavior. Native
// reference defaults are documented separately rather than silently applied.
func DefaultTranscodingPolicy() TranscodingPolicy {
	return TranscodingPolicy{
		EnableHevcEncoding: true, EnableHardwareEncoding: true, DownMixAudioBoost: 1,
		EnableSubtitleExtraction: true, EnableOnTheFlyAttachmentExtraction: true,
	}
}

// RuntimeSubtitleOptions controls playback negotiation independently of the
// existing subtitle download policy. The default retains ASS text conversion.
type RuntimeSubtitleOptions struct {
	AllowAssToText bool `json:"AllowAssToText"`
}

func DefaultRuntimeSubtitleOptions() RuntimeSubtitleOptions {
	return RuntimeSubtitleOptions{AllowAssToText: true}
}

type RuntimeValues struct {
	Network               NetworkValues
	Hardware              HardwareSelection
	Execution             transcode.ExecutionOptions
	Transcoding           TranscodingPolicy
	TranscodingTempPath   string
	CodecConfigurations   transcode.CodecConfigurations
	EnableThrottling      bool
	ExtractionThreadCount int
	CodecProfileLimits    transcode.CodecProfileLimits
	SubtitleOptions       RuntimeSubtitleOptions
}

// RuntimeOptions is frozen at construction. The caller builds the authorized
// inventory; missing persisted devices remain visible but unavailable.
type RuntimeOptions struct {
	Network                       NetworkValues
	Hardware                      HardwareSelection
	Execution                     transcode.ExecutionOptions
	Transcoding                   TranscodingPolicy
	TranscodingTempPath           string
	TranscodingTempPathExclusions []string
	CodecConfigurations           transcode.CodecConfigurations
	EnableThrottling              bool
	ExtractionThreadCount         *int
	CodecProfileLimits            transcode.CodecProfileLimits
	SubtitleOptions               *RuntimeSubtitleOptions
	AuthorizedDeviceIDs           []string
	AvailableDeviceIDs            []string
	LegacyHardwareDefault         bool
}

type RuntimeOverrides struct {
	Network                *NetworkOverrides
	Hardware               *HardwareSelection
	Threads                *int
	H264                   *transcode.CPUQuality
	HEVC                   *transcode.CPUQuality
	SoftwareToneMapping    *bool
	VulkanToneMapping      *bool
	Transcoding            *TranscodingPolicy
	TranscodingTempPath    *string
	SoftwareToneMapOptions *transcode.SoftwareToneMapOptions
	CodecConfigurations    *transcode.CodecConfigurations
	EnableThrottling       *bool
	ExtractionThreadCount  *int
	CodecProfileLimits     *transcode.CodecProfileLimits
	SubtitleOptions        *RuntimeSubtitleOptions
}

type RuntimeSnapshot struct {
	Defaults                   RuntimeValues
	Overrides                  RuntimeOverrides
	DesiredNetwork             NetworkValues
	Hardware                   HardwareSelection
	HardwareAvailable          bool
	Execution                  transcode.ExecutionOptions
	Transcoding                TranscodingPolicy
	DesiredTranscodingTempPath string
	CodecConfigurations        transcode.CodecConfigurations
	EnableThrottling           bool
	ExtractionThreadCount      int
	CodecProfileLimits         transcode.CodecProfileLimits
	SubtitleOptions            RuntimeSubtitleOptions
}

// Change preserves absent versus explicit null. An absent change keeps the
// stored override; a present nil value clears it. Codec values are whole groups.
type Change[T any] struct {
	Present bool
	Value   *T
}

type RuntimeUpdate struct {
	Network                Change[NetworkOverrides]
	Hardware               Change[HardwareSelection]
	Threads                Change[int]
	H264                   Change[transcode.CPUQuality]
	HEVC                   Change[transcode.CPUQuality]
	SoftwareToneMapping    Change[bool]
	VulkanToneMapping      Change[bool]
	Transcoding            Change[TranscodingPolicy]
	TranscodingTempPath    Change[string]
	SoftwareToneMapOptions Change[transcode.SoftwareToneMapOptions]
	CodecConfigurations    Change[transcode.CodecConfigurations]
	EnableThrottling       Change[bool]
	ExtractionThreadCount  Change[int]
	CodecProfileLimits     Change[transcode.CodecProfileLimits]
	SubtitleOptions        Change[RuntimeSubtitleOptions]
}

const (
	FieldRuntime                Field = "Runtime"
	FieldNetwork                Field = "Runtime.Network"
	FieldHardware               Field = "Runtime.Hardware"
	FieldThreads                Field = "Runtime.Threads"
	FieldH264                   Field = "Runtime.H264"
	FieldHEVC                   Field = "Runtime.HEVC"
	FieldSoftwareToneMapping    Field = "Runtime.SoftwareToneMapping"
	FieldVulkanToneMapping      Field = "Runtime.VulkanToneMapping"
	FieldTranscoding            Field = "Runtime.Transcoding"
	FieldTranscodingTempPath    Field = "Runtime.TranscodingTempPath"
	FieldSoftwareToneMapOptions Field = "Runtime.SoftwareToneMapOptions"
	FieldCodecConfigurations    Field = "Runtime.CodecConfigurations"
	FieldEnableThrottling       Field = "Runtime.EnableThrottling"
	FieldExtractionThreadCount  Field = "Runtime.ExtractionThreadCount"
	FieldCodecProfileLimits     Field = "Runtime.CodecProfileLimits"
	FieldRuntimeSubtitleOptions Field = "Runtime.SubtitleOptions"
)

func DefaultRuntimeOptions() RuntimeOptions {
	extractionThreads := 1
	return RuntimeOptions{Network: NetworkValues{HttpPort: 8096},
		Hardware:  HardwareSelection{Decode: "software", Encode: "software"},
		Execution: transcode.DefaultExecutionOptions(2), Transcoding: DefaultTranscodingPolicy(),
		CodecConfigurations:   transcode.DefaultCodecConfigurations(),
		ExtractionThreadCount: &extractionThreads,
		CodecProfileLimits:    transcode.DefaultCodecProfileLimits(), SubtitleOptions: &RuntimeSubtitleOptions{AllowAssToText: true}}
}

// ResetRuntimeUpdate explicitly clears every managed runtime override.
func ResetRuntimeUpdate() RuntimeUpdate {
	return RuntimeUpdate{
		Network: Change[NetworkOverrides]{Present: true}, Hardware: Change[HardwareSelection]{Present: true},
		Threads: Change[int]{Present: true}, H264: Change[transcode.CPUQuality]{Present: true}, HEVC: Change[transcode.CPUQuality]{Present: true},
		SoftwareToneMapping: Change[bool]{Present: true}, VulkanToneMapping: Change[bool]{Present: true},
		Transcoding: Change[TranscodingPolicy]{Present: true}, TranscodingTempPath: Change[string]{Present: true},
		SoftwareToneMapOptions: Change[transcode.SoftwareToneMapOptions]{Present: true},
		CodecConfigurations:    Change[transcode.CodecConfigurations]{Present: true},
		EnableThrottling:       Change[bool]{Present: true},
		ExtractionThreadCount:  Change[int]{Present: true},
		CodecProfileLimits:     Change[transcode.CodecProfileLimits]{Present: true},
		SubtitleOptions:        Change[RuntimeSubtitleOptions]{Present: true},
	}
}

func cloneNetworkOverrides(value *NetworkOverrides) *NetworkOverrides {
	if value == nil {
		return nil
	}
	return &NetworkOverrides{BindHost: clonePointer(value.BindHost), HttpPort: clonePointer(value.HttpPort)}
}

func cloneRuntimeOverrides(value RuntimeOverrides) RuntimeOverrides {
	value.Network = cloneNetworkOverrides(value.Network)
	value.Hardware = clonePointer(value.Hardware)
	value.Threads = clonePointer(value.Threads)
	value.H264 = clonePointer(value.H264)
	value.HEVC = clonePointer(value.HEVC)
	value.SoftwareToneMapping = clonePointer(value.SoftwareToneMapping)
	value.VulkanToneMapping = clonePointer(value.VulkanToneMapping)
	value.Transcoding = clonePointer(value.Transcoding)
	value.TranscodingTempPath = clonePointer(value.TranscodingTempPath)
	value.SoftwareToneMapOptions = clonePointer(value.SoftwareToneMapOptions)
	value.CodecConfigurations = clonePointer(value.CodecConfigurations)
	value.EnableThrottling = clonePointer(value.EnableThrottling)
	value.ExtractionThreadCount = clonePointer(value.ExtractionThreadCount)
	value.CodecProfileLimits = clonePointer(value.CodecProfileLimits)
	value.SubtitleOptions = clonePointer(value.SubtitleOptions)
	return value
}

func cloneRuntimeSnapshot(value RuntimeSnapshot) RuntimeSnapshot {
	value.Overrides = cloneRuntimeOverrides(value.Overrides)
	return value
}

func cloneRuntimeUpdate(value RuntimeUpdate) RuntimeUpdate {
	value.Network.Value = cloneNetworkOverrides(value.Network.Value)
	value.Hardware.Value = clonePointer(value.Hardware.Value)
	value.Threads.Value = clonePointer(value.Threads.Value)
	value.H264.Value = clonePointer(value.H264.Value)
	value.HEVC.Value = clonePointer(value.HEVC.Value)
	value.SoftwareToneMapping.Value = clonePointer(value.SoftwareToneMapping.Value)
	value.VulkanToneMapping.Value = clonePointer(value.VulkanToneMapping.Value)
	value.Transcoding.Value = clonePointer(value.Transcoding.Value)
	value.TranscodingTempPath.Value = clonePointer(value.TranscodingTempPath.Value)
	value.SoftwareToneMapOptions.Value = clonePointer(value.SoftwareToneMapOptions.Value)
	value.CodecConfigurations.Value = clonePointer(value.CodecConfigurations.Value)
	value.EnableThrottling.Value = clonePointer(value.EnableThrottling.Value)
	value.ExtractionThreadCount.Value = clonePointer(value.ExtractionThreadCount.Value)
	value.CodecProfileLimits.Value = clonePointer(value.CodecProfileLimits.Value)
	value.SubtitleOptions.Value = clonePointer(value.SubtitleOptions.Value)
	return value
}

func equalRuntimeOverrides(first, second RuntimeOverrides) bool {
	networkEqual := first.Network == nil && second.Network == nil || first.Network != nil && second.Network != nil &&
		equalPointer(first.Network.BindHost, second.Network.BindHost) && equalPointer(first.Network.HttpPort, second.Network.HttpPort)
	return networkEqual && equalPointer(first.Hardware, second.Hardware) && equalPointer(first.Threads, second.Threads) &&
		equalPointer(first.H264, second.H264) && equalPointer(first.HEVC, second.HEVC) &&
		equalPointer(first.SoftwareToneMapping, second.SoftwareToneMapping) && equalPointer(first.VulkanToneMapping, second.VulkanToneMapping) &&
		equalPointer(first.Transcoding, second.Transcoding) && equalPointer(first.TranscodingTempPath, second.TranscodingTempPath) &&
		equalPointer(first.SoftwareToneMapOptions, second.SoftwareToneMapOptions) && equalPointer(first.CodecConfigurations, second.CodecConfigurations) &&
		equalPointer(first.EnableThrottling, second.EnableThrottling) && equalPointer(first.ExtractionThreadCount, second.ExtractionThreadCount) &&
		equalPointer(first.CodecProfileLimits, second.CodecProfileLimits) && equalPointer(first.SubtitleOptions, second.SubtitleOptions)
}

func applyChange[T any](target **T, change Change[T]) {
	if change.Present {
		*target = clonePointer(change.Value)
	}
}

func applyRuntimeUpdate(previous RuntimeOverrides, change RuntimeUpdate) RuntimeOverrides {
	next := cloneRuntimeOverrides(previous)
	if change.Network.Present {
		next.Network = cloneNetworkOverrides(change.Network.Value)
	}
	applyChange(&next.Hardware, change.Hardware)
	applyChange(&next.Threads, change.Threads)
	applyChange(&next.H264, change.H264)
	applyChange(&next.HEVC, change.HEVC)
	applyChange(&next.SoftwareToneMapping, change.SoftwareToneMapping)
	applyChange(&next.VulkanToneMapping, change.VulkanToneMapping)
	applyChange(&next.Transcoding, change.Transcoding)
	applyChange(&next.TranscodingTempPath, change.TranscodingTempPath)
	applyChange(&next.SoftwareToneMapOptions, change.SoftwareToneMapOptions)
	applyChange(&next.CodecConfigurations, change.CodecConfigurations)
	applyChange(&next.EnableThrottling, change.EnableThrottling)
	applyChange(&next.ExtractionThreadCount, change.ExtractionThreadCount)
	applyChange(&next.CodecProfileLimits, change.CodecProfileLimits)
	applyChange(&next.SubtitleOptions, change.SubtitleOptions)
	if change.CodecConfigurations.Present && next.CodecConfigurations != nil {
		if canonical, err := transcode.CanonicalCodecConfigurations(*next.CodecConfigurations); err == nil {
			next.CodecConfigurations = &canonical
		}
	}
	if change.H264.Present && next.H264 != nil {
		if canonical, err := transcode.CanonicalX264Options(next.H264.X264Options); err == nil {
			next.H264.X264Options = canonical
		}
	}
	return next
}

func clearRuntimeFields(value RuntimeOverrides, fields []Field) RuntimeOverrides {
	for _, field := range fields {
		switch field {
		case FieldRuntime:
			value = RuntimeOverrides{}
		case FieldNetwork:
			value.Network = nil
		case FieldHardware:
			value.Hardware = nil
		case FieldThreads:
			value.Threads = nil
		case FieldH264:
			value.H264 = nil
		case FieldHEVC:
			value.HEVC = nil
		case FieldSoftwareToneMapping:
			value.SoftwareToneMapping = nil
		case FieldVulkanToneMapping:
			value.VulkanToneMapping = nil
		case FieldTranscoding:
			value.Transcoding = nil
		case FieldTranscodingTempPath:
			value.TranscodingTempPath = nil
		case FieldSoftwareToneMapOptions:
			value.SoftwareToneMapOptions = nil
		case FieldCodecConfigurations:
			value.CodecConfigurations = nil
		case FieldEnableThrottling:
			value.EnableThrottling = nil
		case FieldExtractionThreadCount:
			value.ExtractionThreadCount = nil
		case FieldCodecProfileLimits:
			value.CodecProfileLimits = nil
		case FieldRuntimeSubtitleOptions:
			value.SubtitleOptions = nil
		}
	}
	return value
}
