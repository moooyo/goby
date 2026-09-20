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

type RuntimeValues struct {
	Network   NetworkValues
	Hardware  HardwareSelection
	Execution transcode.ExecutionOptions
}

// RuntimeOptions is frozen at construction. The caller builds the authorized
// inventory; missing persisted devices remain visible but unavailable.
type RuntimeOptions struct {
	Network               NetworkValues
	Hardware              HardwareSelection
	Execution             transcode.ExecutionOptions
	AuthorizedDeviceIDs   []string
	AvailableDeviceIDs    []string
	LegacyHardwareDefault bool
}

type RuntimeOverrides struct {
	Network             *NetworkOverrides
	Hardware            *HardwareSelection
	Threads             *int
	H264                *transcode.CPUQuality
	HEVC                *transcode.CPUQuality
	SoftwareToneMapping *bool
	VulkanToneMapping   *bool
}

type RuntimeSnapshot struct {
	Defaults          RuntimeValues
	Overrides         RuntimeOverrides
	DesiredNetwork    NetworkValues
	Hardware          HardwareSelection
	HardwareAvailable bool
	Execution         transcode.ExecutionOptions
}

// Change preserves absent versus explicit null. An absent change keeps the
// stored override; a present nil value clears it. Codec values are whole groups.
type Change[T any] struct {
	Present bool
	Value   *T
}

type RuntimeUpdate struct {
	Network             Change[NetworkOverrides]
	Hardware            Change[HardwareSelection]
	Threads             Change[int]
	H264                Change[transcode.CPUQuality]
	HEVC                Change[transcode.CPUQuality]
	SoftwareToneMapping Change[bool]
	VulkanToneMapping   Change[bool]
}

const (
	FieldRuntime             Field = "Runtime"
	FieldNetwork             Field = "Runtime.Network"
	FieldHardware            Field = "Runtime.Hardware"
	FieldThreads             Field = "Runtime.Threads"
	FieldH264                Field = "Runtime.H264"
	FieldHEVC                Field = "Runtime.HEVC"
	FieldSoftwareToneMapping Field = "Runtime.SoftwareToneMapping"
	FieldVulkanToneMapping   Field = "Runtime.VulkanToneMapping"
)

func DefaultRuntimeOptions() RuntimeOptions {
	return RuntimeOptions{Network: NetworkValues{HttpPort: 8096},
		Hardware:  HardwareSelection{Decode: "software", Encode: "software"},
		Execution: transcode.DefaultExecutionOptions(2)}
}

// ResetRuntimeUpdate explicitly clears every managed runtime override.
func ResetRuntimeUpdate() RuntimeUpdate {
	return RuntimeUpdate{
		Network: Change[NetworkOverrides]{Present: true}, Hardware: Change[HardwareSelection]{Present: true},
		Threads: Change[int]{Present: true}, H264: Change[transcode.CPUQuality]{Present: true}, HEVC: Change[transcode.CPUQuality]{Present: true},
		SoftwareToneMapping: Change[bool]{Present: true}, VulkanToneMapping: Change[bool]{Present: true},
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
	return value
}

func equalRuntimeOverrides(first, second RuntimeOverrides) bool {
	networkEqual := first.Network == nil && second.Network == nil || first.Network != nil && second.Network != nil &&
		equalPointer(first.Network.BindHost, second.Network.BindHost) && equalPointer(first.Network.HttpPort, second.Network.HttpPort)
	return networkEqual && equalPointer(first.Hardware, second.Hardware) && equalPointer(first.Threads, second.Threads) &&
		equalPointer(first.H264, second.H264) && equalPointer(first.HEVC, second.HEVC) &&
		equalPointer(first.SoftwareToneMapping, second.SoftwareToneMapping) && equalPointer(first.VulkanToneMapping, second.VulkanToneMapping)
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
		}
	}
	return value
}
