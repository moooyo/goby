package media

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type DiagnosticMode string

const (
	DiagnosticDecode            DiagnosticMode = "decode"
	DiagnosticEncode            DiagnosticMode = "encode"
	DiagnosticCombined          DiagnosticMode = "combined"
	DiagnosticDeadline                         = 15 * time.Second
	DiagnosticAllocationLimit                  = 16 << 20
	DiagnosticAudioFramesLimit                 = 16384
	DiagnosticAudioPacketsLimit                = 32
)

var ErrDiagnosticPlan = errors.New("invalid fixed media diagnostic plan")

// DiagnosticProfile must come from server-owned configuration. This package
// deliberately does not import config or transcode, which depend on media.
// Empty selections have the same meaning as an explicit software selection.
type DiagnosticProfile struct {
	Decode string
	Encode string
	Device string
}

type DiagnosticLimits struct {
	Deadline                 time.Duration
	MaximumInputBytes        int
	MaximumStdoutBytes       int
	MaximumStderrBytes       int
	MaximumAllocationBytes   int
	MaximumVideoFrames       int
	MaximumAudioSampleFrames int
	MaximumAudioPackets      int
	CodecThreads             int
	FilterThreads            int
}

// These are requirements for the executor, not observations or success
// claims. A process exit code or codec/device enumeration cannot satisfy them.
// The executor must supply an isolated, recorded environment and an explicit
// hardware-variable policy. Neither ambient inheritance by runLimited nor the
// smaller probe environment alone attests to the configured GPU environment.
type DiagnosticExecutionRequirements struct {
	InputFD                        int
	InputMustBeRegular             bool
	InputMustBeReadOnly            bool
	InputIsBorrowed                bool
	InputIdentityMustMatch         bool
	InputIdentityMustRemainStable  bool
	ToolIdentityMustMatch          bool
	IsolatedEnvironmentRequired    bool
	HardwareEnvironmentMustBeBound bool
	AggregateMemoryLimitRequired   bool
	ProcessGroupClosureRequired    bool
	RejectOutputOrTimeLimit        bool
	AllowSoftwareFallback          bool
}

// References must be independently retained from actual software preparation.
// Compressed bytes may differ between encoders. Content comparison operates on
// decoded samples and the fixed sample's frame pattern, not bitstream equality.
// AAC references must describe the exact stage, including observed delay and
// padding; combined AAC encoding cannot reuse a guessed one-generation length.
// These requirements gate a diagnostic success result. A software preparation
// invocation may collect the first reference but cannot certify a stage by
// comparing its output with that same newly collected output.
type DiagnosticVerificationRequirements struct {
	Reference                      string
	PreparedInputReferenceRequired bool
	OutputMustBeDecoded            bool
	DecodedContentMustBeCompared   bool
	ActualDecoderMustBeRecorded    bool
	ActualEncoderMustBeRecorded    bool
	HardwareDecodeMustBeObserved   bool
	HardwareEncodeMustBeObserved   bool
	Width                          int
	Height                         int
	VideoFrames                    int
	RawOutputBytes                 int
	SampleRate                     int
	Channels                       int
	AACStageReferenceRequired      bool
}

// DiagnosticPlan has fixed endpoints and bounds. Args are FFmpeg arguments,
// never a shell command. ValidateDiagnosticPlan must be called after decoding
// or copying a plan and immediately before the executor dispatches it.
// Runtime evidence belongs in a separate result and must not mutate this plan.
type DiagnosticPlan struct {
	Version             int
	Mode                DiagnosticMode
	Profile             DiagnosticProfile
	Input               DiagnosticSampleSpec
	OutputFormat        string
	OutputCodec         string
	ActiveDecodeBackend string
	ActiveEncodeBackend string
	PlannedDevice       string
	Args                []string
	Limits              DiagnosticLimits
	Execution           DiagnosticExecutionRequirements
	Verification        DiagnosticVerificationRequirements
}

// BuildDiagnosticPlan accepts only built-in sample kinds, stages and a closed
// server profile. It accepts no executable, media path, URL, filter, or argv.
// Software encode plans over the raw fixtures also serve as the sample
// preparation commands. Record their actual compressed input identity before
// dispatching a decode command. Software decode may then collect the reference;
// independent reference validation remains required for diagnostic acceptance.
func BuildDiagnosticPlan(mode DiagnosticMode, sample DiagnosticSampleKind, profile DiagnosticProfile) (DiagnosticPlan, error) {
	if mode != DiagnosticDecode && mode != DiagnosticEncode && mode != DiagnosticCombined {
		return DiagnosticPlan{}, ErrDiagnosticPlan
	}
	input, err := DiagnosticSampleSpecification(sample)
	if err != nil || input.RequiresPreparation == (mode == DiagnosticEncode) {
		return DiagnosticPlan{}, ErrDiagnosticPlan
	}
	decode, encode, err := diagnosticProfile(profile)
	if err != nil {
		return DiagnosticPlan{}, err
	}
	video := input.Width != 0
	if !video && (decode != "software" || encode != "software" || profile.Device != "") {
		return DiagnosticPlan{}, ErrDiagnosticPlan
	}
	if mode == DiagnosticEncode {
		decode = "none"
	}
	if mode == DiagnosticDecode {
		encode = "none"
	}
	backend := encode
	if backend == "nvenc" {
		backend = "cuda"
	}
	if backend == "none" || backend == "software" {
		backend = decode
	}
	hardware := backend != "none" && backend != "software"
	device := ""
	if hardware {
		device = profile.Device
		if device == "" {
			device = "/dev/dri/renderD128"
			if backend == "cuda" {
				device = "0"
			}
		}
	}
	plan := DiagnosticPlan{Version: 1, Mode: mode, Profile: profile, Input: input,
		ActiveDecodeBackend: decode, ActiveEncodeBackend: encode, PlannedDevice: device,
		Limits: DiagnosticLimits{Deadline: DiagnosticDeadline, MaximumInputBytes: input.MaximumBytes,
			MaximumStdoutBytes: DiagnosticCompressedBytesLimit, MaximumStderrBytes: maxProcessStderr,
			MaximumAllocationBytes: DiagnosticAllocationLimit, CodecThreads: 1, FilterThreads: 1},
		Execution: DiagnosticExecutionRequirements{InputFD: 3, InputMustBeRegular: true, InputMustBeReadOnly: true, InputIsBorrowed: true,
			InputIdentityMustMatch: true, InputIdentityMustRemainStable: true, ToolIdentityMustMatch: true,
			IsolatedEnvironmentRequired: true, HardwareEnvironmentMustBeBound: hardware,
			AggregateMemoryLimitRequired: true, ProcessGroupClosureRequired: true, RejectOutputOrTimeLimit: true},
		Verification: DiagnosticVerificationRequirements{PreparedInputReferenceRequired: input.RequiresPreparation,
			OutputMustBeDecoded: mode != DiagnosticDecode, DecodedContentMustBeCompared: true,
			ActualDecoderMustBeRecorded: mode != DiagnosticEncode, ActualEncoderMustBeRecorded: mode != DiagnosticDecode,
			HardwareDecodeMustBeObserved: decode != "none" && decode != "software",
			HardwareEncodeMustBeObserved: encode != "none" && encode != "software"}}
	// Thread counts and max_alloc constrain codec work and individual allocator
	// requests. They are not a total RSS, driver-memory, or process/thread limit.
	// Actual get_format decisions are debug-level evidence. The bounded stderr
	// consumer still rejects truncation; argument selection alone is not proof.
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "level+debug", "-xerror",
		"-max_alloc", strconv.Itoa(DiagnosticAllocationLimit), "-filter_threads", "1", "-filter_complex_threads", "1"}
	if hardware {
		args = diagnosticHardwareDevice(args, backend, device)
	}
	if decode != "none" && decode != "software" {
		// Match the production hardware-acceleration selection. The actual
		// decoder name remains runtime evidence, not a capability-list claim.
		args = append(args, "-hwaccel", decode, "-hwaccel_device", "diagnostic", "-hwaccel_output_format", decode)
	}
	args = append(args, "-threads", "1", "-probesize", strconv.Itoa(DiagnosticCompressedBytesLimit), "-analyzeduration", "1000000",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", input.Format, "-f", input.Format)
	if video {
		args = append(args, "-max_pixels", strconv.Itoa(DiagnosticWidth*DiagnosticHeight))
		if !input.RequiresPreparation {
			args = append(args, "-pixel_format", "yuv420p", "-video_size", "320x192", "-framerate", "8", "-c:v", "rawvideo")
		} else {
			args = append(args, "-r", "8")
			if decode == "software" {
				args = append(args, "-c:v", "h264")
			}
		}
	} else if input.RequiresPreparation {
		args = append(args, "-c:a", "aac")
	} else {
		args = append(args, "-ar", "48000", "-ac", "2", "-c:a", "pcm_s16le")
	}
	args = append(args, "-i", "/proc/self/fd/3", "-map_metadata", "-1", "-map_chapters", "-1", "-sn", "-dn")
	if video {
		args = diagnosticVideoOutput(args, &plan)
	} else {
		args = diagnosticAudioOutput(args, &plan)
	}
	// -fs is only a secondary muxer stop. A bounded stdout consumer must reject
	// excess bytes and reap the complete group; successful truncation is failure.
	plan.Args = append(args, "-fs", strconv.Itoa(plan.Limits.MaximumStdoutBytes), "-protocol_whitelist", "pipe", "pipe:1")
	return plan, nil
}

func ValidateDiagnosticPlan(plan DiagnosticPlan) error {
	expected, err := BuildDiagnosticPlan(plan.Mode, plan.Input.Kind, plan.Profile)
	if err != nil || !reflect.DeepEqual(expected, plan) {
		return ErrDiagnosticPlan
	}
	return nil
}

func diagnosticProfile(profile DiagnosticProfile) (string, string, error) {
	decode, encode := profile.Decode, profile.Encode
	if decode == "" {
		decode = "software"
	}
	if encode == "" {
		encode = "software"
	}
	if decode != "software" && decode != "vaapi" && decode != "qsv" && decode != "cuda" ||
		encode != "software" && encode != "vaapi" && encode != "qsv" && encode != "nvenc" {
		return "", "", ErrDiagnosticPlan
	}
	backend := encode
	if backend == "nvenc" {
		backend = "cuda"
	}
	if decode != "software" && backend != "software" && decode != backend {
		return "", "", ErrDiagnosticPlan
	}
	if backend == "software" {
		backend = decode
	}
	if backend == "software" {
		if profile.Device != "" {
			return "", "", ErrDiagnosticPlan
		}
	} else if profile.Device != "" {
		if backend == "cuda" {
			number, err := strconv.Atoi(profile.Device)
			if err != nil || number < 0 || number > 31 || strconv.Itoa(number) != profile.Device {
				return "", "", ErrDiagnosticPlan
			}
		} else {
			const prefix = "/dev/dri/renderD"
			number, err := strconv.Atoi(strings.TrimPrefix(profile.Device, prefix))
			if err != nil || number < 128 || number > 255 || profile.Device != prefix+strconv.Itoa(number) {
				return "", "", ErrDiagnosticPlan
			}
		}
	}
	return decode, encode, nil
}

func diagnosticHardwareDevice(args []string, backend, device string) []string {
	switch backend {
	case "qsv":
		args = append(args, "-init_hw_device", "vaapi=diagnosticva:"+device, "-init_hw_device", "qsv=diagnostic@diagnosticva")
	case "vaapi", "cuda":
		args = append(args, "-init_hw_device", backend+"=diagnostic:"+device)
	}
	return append(args, "-filter_hw_device", "diagnostic")
}

func diagnosticVideoOutput(args []string, plan *DiagnosticPlan) []string {
	plan.Verification.Width, plan.Verification.Height = DiagnosticWidth, DiagnosticHeight
	plan.Verification.VideoFrames = DiagnosticVideoFrames
	plan.Verification.Reference = "original-raw-video"
	if plan.Input.RequiresPreparation {
		plan.Verification.Reference = "prepared-software-decoded-video"
	}
	// One extra frame is a sentinel: decode/combined validation requires exactly
	// eight frames and rejects hitting this command bound or any other truncation.
	frames := DiagnosticVideoFrames + 1
	if plan.Mode == DiagnosticEncode {
		frames = DiagnosticVideoFrames
	}
	plan.Limits.MaximumVideoFrames = frames
	args = append(args, "-map", "0:v:0", "-an", "-threads:v", "1", "-fps_mode", "passthrough", "-frames:v", strconv.Itoa(frames), "-t", "2")
	decode, encode := plan.ActiveDecodeBackend, plan.ActiveEncodeBackend
	if plan.Mode == DiagnosticDecode {
		filter := "format=yuv420p"
		if decode != "software" {
			filter = "hwdownload,format=nv12,format=yuv420p"
		}
		plan.OutputFormat, plan.OutputCodec = "rawvideo", "rawvideo"
		plan.Limits.MaximumStdoutBytes = frames * DiagnosticVideoFrameBytes
		plan.Verification.RawOutputBytes = DiagnosticVideoBytes
		return append(args, "-vf", filter, "-c:v", "rawvideo", "-pix_fmt", "yuv420p", "-f", "rawvideo")
	}
	filter := "format=yuv420p"
	if plan.Mode == DiagnosticCombined {
		filter = "scale=w=320:h=192,format=yuv420p"
		if decode != "software" {
			name := "scale_" + decode
			if decode == "qsv" {
				name = "vpp_qsv"
			}
			filter = name + "=w=320:h=192:format=nv12"
			if encode == "software" {
				filter += ",hwdownload,format=nv12,format=yuv420p"
			}
		}
	}
	if encode != "software" && (decode == "none" || decode == "software") {
		filter = "format=nv12,hwupload=extra_hw_frames=8"
		if plan.Mode == DiagnosticCombined {
			filter = "scale=w=320:h=192," + filter
		}
	}
	codec := "libx264"
	if encode != "software" {
		codec = "h264_" + encode
	}
	plan.OutputFormat, plan.OutputCodec = "h264", "h264"
	args = append(args, "-vf", filter, "-c:v", codec, "-g", "8", "-bf", "0", "-refs", "1", "-b:v", "500000", "-maxrate", "500000", "-bufsize", "1000000")
	if encode == "software" {
		args = append(args, "-preset", "veryfast", "-pix_fmt", "yuv420p", "-sc_threshold", "0")
	}
	return append(args, "-f", "h264")
}

func diagnosticAudioOutput(args []string, plan *DiagnosticPlan) []string {
	plan.Verification.SampleRate, plan.Verification.Channels = DiagnosticSampleRate, DiagnosticAudioChannels
	plan.Verification.Reference = "prepared-software-aac-reference-for-this-stage"
	plan.Verification.AACStageReferenceRequired = true
	plan.Limits.MaximumAudioSampleFrames = DiagnosticAudioFramesLimit
	plan.Limits.MaximumAudioPackets = DiagnosticAudioPacketsLimit
	samples := DiagnosticAudioFramesLimit
	if plan.Mode == DiagnosticEncode {
		samples = DiagnosticAudioSampleFrames
	}
	args = append(args, "-map", "0:a:0", "-vn", "-threads:a", "1", "-ar", "48000", "-ac", "2",
		"-af", "atrim=end_sample="+strconv.Itoa(samples)+",asetpts=N/SR/TB", "-frames:a", strconv.Itoa(DiagnosticAudioPacketsLimit), "-t", "1")
	if plan.Mode == DiagnosticDecode {
		plan.OutputFormat, plan.OutputCodec = "s16le", "pcm_s16le"
		plan.Limits.MaximumStdoutBytes = DiagnosticAudioFramesLimit * DiagnosticAudioChannels * 2
		return append(args, "-c:a", "pcm_s16le", "-f", "s16le")
	}
	plan.OutputFormat, plan.OutputCodec = "adts", "aac"
	// Retain the fixed PCM through two AAC generations under the content policy.
	return append(args, "-c:a", "aac", "-profile:a", "aac_low", "-b:a", "256000", "-f", "adts")
}
