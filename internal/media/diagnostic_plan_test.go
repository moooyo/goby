package media

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func diagnosticTestPair(args []string, option, value string) bool {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == option && args[index+1] == value {
			return true
		}
	}
	return false
}

func diagnosticTestParts(t *testing.T, plan DiagnosticPlan) ([]string, []string) {
	t.Helper()
	index := slices.Index(plan.Args, "-i")
	if index < 0 || index+1 >= len(plan.Args) || plan.Args[index+1] != "/proc/self/fd/3" || slices.Contains(plan.Args[index+2:], "-i") {
		t.Fatal("the plan did not retain exactly its fixed fd3 input")
	}
	return plan.Args[:index], plan.Args[index+2:]
}

func TestDiagnosticModesAcceptOnlyTheirFixedInputKinds(t *testing.T) {
	for _, mode := range []DiagnosticMode{DiagnosticDecode, DiagnosticEncode, DiagnosticCombined} {
		for _, sample := range []DiagnosticSampleKind{DiagnosticRawVideo, DiagnosticRawAudio, DiagnosticH264, DiagnosticAAC} {
			plan, err := BuildDiagnosticPlan(mode, sample, DiagnosticProfile{})
			prepared := sample == DiagnosticH264 || sample == DiagnosticAAC
			want := prepared != (mode == DiagnosticEncode)
			if (err == nil) != want {
				t.Fatalf("mode %s accepted the wrong raw/prepared sample %s", mode, sample)
			}
			if want && ValidateDiagnosticPlan(plan) != nil {
				t.Fatal("a generated fixed plan rejected itself")
			}
		}
	}
	for _, mode := range []DiagnosticMode{"", "Decode", "decode;encode", "probe"} {
		if _, err := BuildDiagnosticPlan(mode, DiagnosticH264, DiagnosticProfile{}); !errors.Is(err, ErrDiagnosticPlan) {
			t.Fatal("unknown diagnostic stage accepted")
		}
	}
}

func TestDiagnosticDecodeOnlyHasNoCompressedEncoderOrHardwareScaler(t *testing.T) {
	for _, backend := range []string{"software", "vaapi", "qsv", "cuda"} {
		plan, err := BuildDiagnosticPlan(DiagnosticDecode, DiagnosticH264, DiagnosticProfile{Decode: backend})
		if err != nil {
			t.Fatal(err)
		}
		input, output := diagnosticTestParts(t, plan)
		if !diagnosticTestPair(output, "-c:v", "rawvideo") || !diagnosticTestPair(output, "-f", "rawvideo") ||
			plan.ActiveEncodeBackend != "none" || plan.Verification.ActualEncoderMustBeRecorded || plan.Verification.OutputMustBeDecoded {
			t.Fatal("decode-only claimed a compressed encoder or output-decode stage")
		}
		for _, arg := range output {
			if strings.Contains(arg, "scale") || strings.Contains(arg, "vpp_qsv") || strings.Contains(arg, "hwupload") || strings.HasPrefix(arg, "h264_") || arg == "libx264" {
				t.Fatal("decode-only acquired an encoder, scaler, or upload dependency")
			}
		}
		if backend == "software" {
			if slices.Contains(input, "-hwaccel") || !diagnosticTestPair(input, "-c:v", "h264") {
				t.Fatal("software decoder selection is not explicit")
			}
		} else if !diagnosticTestPair(input, "-hwaccel", backend) || !diagnosticTestPair(input, "-hwaccel_output_format", backend) ||
			!diagnosticTestPair(output, "-vf", "hwdownload,format=nv12,format=yuv420p") || !plan.Verification.HardwareDecodeMustBeObserved {
			t.Fatal("hardware decode lacks a required hardware-frame download path")
		}
		if plan.Verification.RawOutputBytes != 737280 || plan.Verification.VideoFrames != 8 || plan.Limits.MaximumVideoFrames != 9 {
			t.Fatal("video decoding lost the exact expected frames or overflow sentinel")
		}
	}
}

func TestDiagnosticEncodeOnlyNeverEnablesTheConfiguredHardwareDecoder(t *testing.T) {
	for _, profile := range []DiagnosticProfile{{}, {Decode: "vaapi", Encode: "vaapi"}, {Decode: "qsv", Encode: "qsv"}, {Decode: "cuda", Encode: "nvenc"}} {
		plan, err := BuildDiagnosticPlan(DiagnosticEncode, DiagnosticRawVideo, profile)
		if err != nil {
			t.Fatal(err)
		}
		input, output := diagnosticTestParts(t, plan)
		if slices.Contains(input, "-hwaccel") || slices.Contains(input, "-hwaccel_output_format") || !diagnosticTestPair(input, "-c:v", "rawvideo") ||
			plan.ActiveDecodeBackend != "none" || plan.Verification.HardwareDecodeMustBeObserved || plan.Verification.ActualDecoderMustBeRecorded {
			t.Fatal("encoding-only exercised or claimed a hardware decoder")
		}
		codec := "libx264"
		if profile.Encode != "" {
			codec = "h264_" + profile.Encode
			if !diagnosticTestPair(output, "-vf", "format=nv12,hwupload=extra_hw_frames=8") || !plan.Verification.HardwareEncodeMustBeObserved {
				t.Fatal("hardware encode did not upload the fixed raw sample")
			}
		}
		if !diagnosticTestPair(output, "-c:v", codec) || !diagnosticTestPair(output, "-frames:v", "8") || plan.Profile != profile {
			t.Fatal("encoder selection, input frame count, or configured profile changed")
		}
	}
}

func TestDiagnosticCombinedUsesTheSelectedProductionHardwareFilterChain(t *testing.T) {
	for _, test := range []struct {
		decode, encode, filter, codec string
	}{
		{"software", "software", "scale=w=320:h=192,format=yuv420p", "libx264"},
		{"vaapi", "vaapi", "scale_vaapi=w=320:h=192:format=nv12", "h264_vaapi"},
		{"qsv", "qsv", "vpp_qsv=w=320:h=192:format=nv12", "h264_qsv"},
		{"cuda", "nvenc", "scale_cuda=w=320:h=192:format=nv12", "h264_nvenc"},
		{"vaapi", "software", "scale_vaapi=w=320:h=192:format=nv12,hwdownload,format=nv12,format=yuv420p", "libx264"},
		{"qsv", "software", "vpp_qsv=w=320:h=192:format=nv12,hwdownload,format=nv12,format=yuv420p", "libx264"},
		{"cuda", "software", "scale_cuda=w=320:h=192:format=nv12,hwdownload,format=nv12,format=yuv420p", "libx264"},
		{"software", "vaapi", "scale=w=320:h=192,format=nv12,hwupload=extra_hw_frames=8", "h264_vaapi"},
		{"software", "qsv", "scale=w=320:h=192,format=nv12,hwupload=extra_hw_frames=8", "h264_qsv"},
		{"software", "nvenc", "scale=w=320:h=192,format=nv12,hwupload=extra_hw_frames=8", "h264_nvenc"},
	} {
		plan, err := BuildDiagnosticPlan(DiagnosticCombined, DiagnosticH264, DiagnosticProfile{Decode: test.decode, Encode: test.encode})
		if err != nil {
			t.Fatal(err)
		}
		input, output := diagnosticTestParts(t, plan)
		if !diagnosticTestPair(output, "-vf", test.filter) || !diagnosticTestPair(output, "-c:v", test.codec) ||
			plan.ActiveDecodeBackend != test.decode || plan.ActiveEncodeBackend != test.encode {
			t.Fatalf("combined %s/%s changed its selected path", test.decode, test.encode)
		}
		if (test.decode != "software") != slices.Contains(input, "-hwaccel") || !plan.Verification.OutputMustBeDecoded || !plan.Verification.DecodedContentMustBeCompared {
			t.Fatal("combined output does not require both real path and content evidence")
		}
	}
}

func TestDiagnosticDeviceInitializationAndInjectionBoundaries(t *testing.T) {
	for _, test := range []struct {
		profile DiagnosticProfile
		device  string
		init    string
	}{
		{DiagnosticProfile{Decode: "vaapi"}, "/dev/dri/renderD128", "vaapi=diagnostic:/dev/dri/renderD128"},
		{DiagnosticProfile{Decode: "qsv", Device: "/dev/dri/renderD255"}, "/dev/dri/renderD255", "qsv=diagnostic@diagnosticva"},
		{DiagnosticProfile{Decode: "cuda"}, "0", "cuda=diagnostic:0"},
		{DiagnosticProfile{Decode: "cuda", Device: "31"}, "31", "cuda=diagnostic:31"},
	} {
		plan, err := BuildDiagnosticPlan(DiagnosticDecode, DiagnosticH264, test.profile)
		if err != nil || plan.PlannedDevice != test.device || !diagnosticTestPair(plan.Args, "-init_hw_device", test.init) || !plan.Execution.HardwareEnvironmentMustBeBound {
			t.Fatalf("planned device selection changed: %v", err)
		}
	}
	for _, profile := range []DiagnosticProfile{
		{Decode: "nvenc"}, {Encode: "cuda"}, {Decode: "vaapi", Encode: "qsv"}, {Decode: "cuda", Encode: "vaapi"},
		{Device: "0"}, {Decode: "vaapi", Device: "/dev/dri/renderD127"}, {Decode: "qsv", Device: "/dev/dri/renderD256"},
		{Decode: "vaapi", Device: "/dev/dri/../renderD128"}, {Decode: "qsv", Device: "/dev/dri/renderD128,driver=other"},
		{Decode: "cuda", Device: "32"}, {Decode: "cuda", Device: "00"}, {Decode: "cuda", Device: "+0"},
		{Decode: "cuda", Device: "0,primary_ctx=1"}, {Decode: "cuda", Device: "0\n-ignore_unknown"},
		{Decode: "vaapi", Device: "https://example.test/device"},
	} {
		if _, err := BuildDiagnosticPlan(DiagnosticCombined, DiagnosticH264, profile); !errors.Is(err, ErrDiagnosticPlan) {
			t.Fatal("unsupported or injected device/profile accepted")
		}
	}
}

func TestDiagnosticAACStagesRequireMeasuredPrimingReferences(t *testing.T) {
	for _, mode := range []DiagnosticMode{DiagnosticDecode, DiagnosticEncode, DiagnosticCombined} {
		sample := DiagnosticAAC
		if mode == DiagnosticEncode {
			sample = DiagnosticRawAudio
		}
		plan, err := BuildDiagnosticPlan(mode, sample, DiagnosticProfile{})
		if err != nil {
			t.Fatal(err)
		}
		input, output := diagnosticTestParts(t, plan)
		if !plan.Verification.AACStageReferenceRequired || plan.Verification.RawOutputBytes != 0 ||
			plan.Verification.SampleRate != 48000 || plan.Verification.Channels != 2 || slices.Contains(input, "-hwaccel") {
			t.Fatal("AAC used a guessed decoded length or a hardware video selection")
		}
		if mode == DiagnosticDecode {
			if !diagnosticTestPair(input, "-c:a", "aac") || !diagnosticTestPair(output, "-c:a", "pcm_s16le") || diagnosticTestPair(output, "-c:a", "aac") {
				t.Fatal("AAC decode-only included a compressed encoder")
			}
		} else if !diagnosticTestPair(output, "-c:a", "aac") || !diagnosticTestPair(output, "-profile:a", "aac_low") ||
			!diagnosticTestPair(output, "-b:a", "256000") || !diagnosticTestPair(output, "-f", "adts") || !plan.Verification.OutputMustBeDecoded {
			t.Fatal("AAC encoding lacks its independently decoded-output requirement")
		}
		if mode != DiagnosticDecode {
			for _, bitrate := range []string{"128000", "192000", "512000"} {
				changed := plan
				changed.Args = slices.Clone(plan.Args)
				changed.Args[slices.Index(changed.Args, "-b:a")+1] = bitrate
				if !errors.Is(ValidateDiagnosticPlan(changed), ErrDiagnosticPlan) {
					t.Fatal("AAC accepted a caller-selected bitrate outside its fixed preset")
				}
			}
		}
		if _, err := BuildDiagnosticPlan(mode, sample, DiagnosticProfile{Decode: "cuda"}); !errors.Is(err, ErrDiagnosticPlan) {
			t.Fatal("AAC silently ignored a hardware-only profile")
		}
	}
}

func TestDiagnosticPlansFixDescriptorsProtocolsAndExecutionBounds(t *testing.T) {
	for _, selected := range []struct {
		mode   DiagnosticMode
		sample DiagnosticSampleKind
	}{{DiagnosticEncode, DiagnosticRawVideo}, {DiagnosticDecode, DiagnosticH264}, {DiagnosticCombined, DiagnosticH264},
		{DiagnosticEncode, DiagnosticRawAudio}, {DiagnosticDecode, DiagnosticAAC}, {DiagnosticCombined, DiagnosticAAC}} {
		plan, err := BuildDiagnosticPlan(selected.mode, selected.sample, DiagnosticProfile{})
		if err != nil {
			t.Fatal(err)
		}
		input, output := diagnosticTestParts(t, plan)
		if plan.Args[len(plan.Args)-1] != "pipe:1" || !diagnosticTestPair(input, "-protocol_whitelist", "file,pipe") ||
			!diagnosticTestPair(input, "-format_whitelist", plan.Input.Format) || !diagnosticTestPair(output, "-protocol_whitelist", "pipe") ||
			slices.Contains(plan.Args, "-progress") || !slices.Contains(plan.Args, "-nostdin") {
			t.Fatal("fixed media endpoints or stdout ownership changed")
		}
		if plan.Execution.InputFD != 3 || !plan.Execution.InputMustBeRegular || !plan.Execution.InputMustBeReadOnly || !plan.Execution.InputIsBorrowed ||
			!plan.Execution.InputIdentityMustMatch || !plan.Execution.InputIdentityMustRemainStable || !plan.Execution.IsolatedEnvironmentRequired ||
			!plan.Execution.AggregateMemoryLimitRequired || !plan.Execution.ProcessGroupClosureRequired || plan.Execution.AllowSoftwareFallback {
			t.Fatal("the plan loosened future executor ownership or isolation requirements")
		}
		if plan.Limits.Deadline != 15*time.Second || plan.Limits.MaximumStdoutBytes <= 0 || plan.Limits.MaximumStdoutBytes > 1<<20 ||
			plan.Limits.MaximumStderrBytes != 64<<10 || plan.Limits.MaximumInputBytes != plan.Input.MaximumBytes ||
			!diagnosticTestPair(input, "-threads", "1") || !diagnosticTestPair(input, "-filter_threads", "1") ||
			!diagnosticTestPair(input, "-filter_complex_threads", "1") || !diagnosticTestPair(input, "-max_alloc", "16777216") {
			t.Fatal("a diagnostic stage lost an explicit process or allocation bound")
		}
	}
}

func TestDiagnosticPlanValidationRejectsMutatedArgumentsAndEvidenceRequirements(t *testing.T) {
	for _, change := range []func(*DiagnosticPlan){
		func(plan *DiagnosticPlan) { plan.Args = append(plan.Args, "-i", "https://example.test/source") },
		func(plan *DiagnosticPlan) { plan.Args[len(plan.Args)-1] = "/tmp/output" },
		func(plan *DiagnosticPlan) { plan.Execution.InputFD = 4 },
		func(plan *DiagnosticPlan) { plan.Execution.InputIsBorrowed = false },
		func(plan *DiagnosticPlan) { plan.Execution.InputMustBeReadOnly = false },
		func(plan *DiagnosticPlan) { plan.Execution.AllowSoftwareFallback = true },
		func(plan *DiagnosticPlan) { plan.Execution.IsolatedEnvironmentRequired = false },
		func(plan *DiagnosticPlan) { plan.Limits.Deadline = time.Minute },
		func(plan *DiagnosticPlan) { plan.Limits.MaximumStdoutBytes++ },
		func(plan *DiagnosticPlan) { plan.Limits.MaximumInputBytes++ },
		func(plan *DiagnosticPlan) { plan.Limits.CodecThreads = 2 },
		func(plan *DiagnosticPlan) { plan.Input.Width = 1920 },
		func(plan *DiagnosticPlan) { plan.Verification.PreparedInputReferenceRequired = false },
		func(plan *DiagnosticPlan) { plan.Verification.HardwareDecodeMustBeObserved = false },
	} {
		plan, err := BuildDiagnosticPlan(DiagnosticCombined, DiagnosticH264, DiagnosticProfile{Decode: "vaapi", Encode: "vaapi"})
		if err != nil {
			t.Fatal(err)
		}
		change(&plan)
		if !errors.Is(ValidateDiagnosticPlan(plan), ErrDiagnosticPlan) {
			t.Fatal("a changed command, bound, or required evidence was accepted")
		}
	}
	first, err := BuildDiagnosticPlan(DiagnosticCombined, DiagnosticH264, DiagnosticProfile{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildDiagnosticPlan(DiagnosticCombined, DiagnosticH264, DiagnosticProfile{})
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("the fixed command plan is not deterministic")
	}
	first.Args[0] = "changed"
	if ValidateDiagnosticPlan(second) != nil {
		t.Fatal("two plans shared mutable argument storage")
	}
}
