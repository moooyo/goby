package media

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseDolbyVisionRPUFramesRequiresEveryFrame(t *testing.T) {
	const rpu = `{"side_data_type":"Dolby Vision Metadata","disable_residual_flag":1}`
	const frame = `{"stream_index":3,"side_data_list":[` + rpu + `]}`
	for _, test := range []struct {
		name     string
		frames   string
		count    int64
		reason   string
		verified bool
		disabled bool
		mixed    bool
	}{
		{"complete multi-frame scan", frame + `,` + frame, 2, "", true, true, false},
		{"later frame has no RPU", frame + `,{"stream_index":3}`, 2, dolbyVisionRPUMetadataMissing, false, false, false},
		{"first frame has no RPU", `{"stream_index":3},` + frame, 2, dolbyVisionRPUMetadataMissing, false, false, false},
		{"residual-enabled RPU", strings.Replace(frame, `"disable_residual_flag":1`, `"disable_residual_flag":0`, 1), 1, dolbyVisionRPUResidualEnabled, true, false, false},
		{"mixed residual flags", frame + `,` + strings.Replace(frame, `"disable_residual_flag":1`, `"disable_residual_flag":0`, 1), 2, dolbyVisionRPUResidualMixed, true, false, true},
		{"unknown residual flag", strings.Replace(frame, `"disable_residual_flag":1`, `"disable_residual_flag":"N/A"`, 1), 1, dolbyVisionRPUResidualUnknown, false, false, false},
		{"missing residual flag", `{"stream_index":3,"side_data_list":[{"side_data_type":"Dolby Vision Metadata"}]}`, 1, dolbyVisionRPUResidualUnknown, false, false, false},
		{"raw RPU without decoded metadata", strings.Replace(frame, "Dolby Vision Metadata", "Dolby Vision RPU Data", 1), 1, dolbyVisionRPUMetadataMissing, false, false, false},
		{"empty frames", "", 0, dolbyVisionRPUNoFrames, false, false, false},
		{"mixed frame metadata", `{"stream_index":3,"side_data_list":[` + rpu + `,{"side_data_type":"Dolby Vision Metadata","disable_residual_flag":0}]}`, 1, dolbyVisionRPUResidualMixed, true, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := parseDolbyVisionRPUFrames(context.Background(), []byte(`{"frames":[`+test.frames+`]}`), 3)
			if err != nil || evidence.verified != test.verified || evidence.residualDisabled != test.disabled || evidence.residualMixed != test.mixed ||
				evidence.frameCount != test.count || evidence.reason != test.reason {
				t.Fatalf("incorrect RPU frame evidence: %+v, %v", evidence, err)
			}
		})
	}
}

func TestParseDolbyVisionRPUFramesRejectsInvalidOrPartialDocuments(t *testing.T) {
	for name, document := range map[string]string{
		"empty input":          "",
		"missing frames":       `{}`,
		"null frames":          `{"frames":null}`,
		"truncated array":      `{"frames":[{"stream_index":3}`,
		"truncated document":   `{"frames":[]`,
		"trailing document":    `{"frames":[]}{}`,
		"duplicate frame list": `{"frames":[],"frames":[]}`,
		"missing stream index": `{"frames":[{"side_data_list":[{"side_data_type":"Dolby Vision Metadata","disable_residual_flag":1}]}]}`,
		"wrong stream index":   `{"frames":[{"stream_index":0}]}`,
		"boolean stream index": `{"frames":[{"stream_index":true}]}`,
		"invalid residual":     `{"frames":[{"stream_index":3,"side_data_list":[{"side_data_type":"Dolby Vision Metadata","disable_residual_flag":2}]}]}`,
		"boolean residual":     `{"frames":[{"stream_index":3,"side_data_list":[{"side_data_type":"Dolby Vision Metadata","disable_residual_flag":true}]}]}`,
		"fractional residual":  `{"frames":[{"stream_index":3,"side_data_list":[{"side_data_type":"Dolby Vision Metadata","disable_residual_flag":"1/2"}]}]}`,
		"later malformed RPU":  `{"frames":[{"stream_index":3},{"stream_index":3,"side_data_list":[{"side_data_type":"Dolby Vision Metadata","disable_residual_flag":{}}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			evidence, err := parseDolbyVisionRPUFrames(context.Background(), []byte(document), 3)
			if err != nil || evidence.verified || evidence.residualDisabled || evidence.frameCount != 0 || evidence.reason != dolbyVisionRPUInvalidScan {
				t.Fatalf("invalid frame scan became evidence: %+v, %v", evidence, err)
			}
		})
	}
}

func TestDolbyVisionRPUProbePreservesCancellation(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		var ctx context.Context
		var cancel context.CancelFunc
		want := context.Canceled
		if deadline {
			ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
			want = context.DeadlineExceeded
		} else {
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		}
		defer cancel()
		if evidence, err := parseDolbyVisionRPUFrames(ctx, []byte(`{"frames":[]}`), 0); !errors.Is(err, want) || evidence.verified {
			t.Fatalf("frame parser hid cancellation: %+v, %v", evidence, err)
		}
		if _, err := runDolbyVisionRPUProbe(ctx, "/missing/ffmpeg", nil, 0, Info{}); !errors.Is(err, want) {
			t.Fatalf("RPU probe hid cancellation: %v", err)
		}
	}
}

func TestDolbyVisionRPUProbeUnsupportedConfigurationRemainsUnverified(t *testing.T) {
	metadata := &DolbyVisionMetadata{Profile: 4, RPUPresent: true, BLPresent: true, RPUVerified: true, RPUProfile: 7, RPUResidualMixed: true, ResidualDisabled: true, RPUFrameCount: 10}
	info := Info{Container: "matroska,webm", Streams: []Stream{{Index: 0, CodecType: "video", Codec: "hevc", DolbyVision: metadata}}}
	probed, err := runDolbyVisionRPUProbe(context.Background(), "/missing/ffmpeg", nil, 0, info)
	if err != nil {
		t.Fatal(err)
	}
	got := probed.Streams[0].DolbyVision
	if got == nil || got.RPUVerified || got.ResidualDisabled || got.RPUFrameCount != 0 || got.RPUProfile != 0 || got.RPUResidualMixed || got.RPUValidationReason != dolbyVisionRPUUnsupported {
		t.Fatalf("unsupported configuration retained verified evidence: %+v", got)
	}
	if !metadata.RPUVerified || !metadata.ResidualDisabled || metadata.RPUFrameCount != 10 || metadata.RPUProfile != 7 || !metadata.RPUResidualMixed {
		t.Fatal("RPU scan mutated the caller's existing evidence")
	}
}

func TestDolbyVisionRPUScanHasBoundedSourceSizedBudget(t *testing.T) {
	minimum := dolbyVisionRPUScanTimeout(0)
	large := dolbyVisionRPUScanTimeout(16 * 1024 * 1024 * 1024)
	maximum := dolbyVisionRPUScanTimeout(math.MaxInt64)
	if minimum < 2*time.Minute || large <= minimum || maximum < large || maximum > 15*time.Minute || dolbyVisionRPUScanTimeout(-1) != minimum {
		t.Fatalf("invalid RPU scan budgets: minimum=%v large=%v maximum=%v", minimum, large, maximum)
	}
}

func TestDolbyVisionRPUProbeRequiresKnownCompatibleCompression(t *testing.T) {
	for _, test := range []struct {
		profile     int
		compression string
	}{{8, ""}, {8, "reserved"}, {7, "limited"}, {5, "extended"}} {
		info := Info{Streams: []Stream{{Index: 0, CodecType: "video", Codec: "hevc", DolbyVision: &DolbyVisionMetadata{
			Profile: test.profile, RPUPresent: true, BLPresent: true, MetadataCompression: test.compression,
		}}}}
		probed, err := runDolbyVisionRPUProbe(context.Background(), "/missing/ffmpeg", nil, 0, info)
		if err != nil || probed.Streams[0].DolbyVision.RPUVerified || probed.Streams[0].DolbyVision.RPUValidationReason != dolbyVisionRPUUnsupported {
			t.Fatalf("unproven compression configuration reached process execution: %+v, %v", probed, err)
		}
	}
}

func TestDolbyVisionRPUProbeUsesCompleteAuthorizedStream(t *testing.T) {
	args := dolbyVisionRPUProbeArgs(7)
	for name, want := range map[string]string{
		"-i":                  "/proc/self/fd/3",
		"-map":                "0:7",
		"-c:v":                "copy",
		"-protocol_whitelist": "file,pipe",
		"-format_whitelist":   probeFormats,
		"-bsf:v":              "hevc_mp4toannexb,hevc_metadata=aud=insert,filter_units=pass_types=35|62",
	} {
		found := false
		for index := 0; index+1 < len(args); index++ {
			if args[index] == name {
				found = true
				if args[index+1] != want {
					t.Fatalf("incorrect %s argument: %q", name, args[index+1])
				}
			}
		}
		if !found {
			t.Fatalf("required %s argument was omitted", name)
		}
	}
	for _, arg := range args {
		if arg == "-t" || arg == "-to" || arg == "-ss" || strings.HasPrefix(arg, "-frames") {
			t.Fatalf("RPU scan was restricted to a source fragment: %v", args)
		}
	}
	if !reflect.DeepEqual(args[len(args)-2:], []string{"hevc", "pipe:1"}) {
		t.Fatalf("RPU scan does not return the filtered byte stream: %v", args)
	}
}

func TestDolbyVisionRPUSyntaxValidationUsesNativeParserWithoutPixelDecoding(t *testing.T) {
	args := dolbyVisionRPUValidationArgs(7)
	for name, want := range map[string]string{
		"-v":     "warning",
		"-map":   "0:7",
		"-c:v":   "copy",
		"-bsf:v": "hevc_mp4toannexb,hevc_metadata=aud=insert,filter_units=pass_types=35|62,dovi_rpu=compression=none",
		"-f":     "null",
		"-i":     "/proc/self/fd/3",
	} {
		found := false
		for index := 0; index+1 < len(args); index++ {
			if args[index] == name && args[index+1] == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("native syntax validation omitted %s=%s: %v", name, want, args)
		}
	}
	for _, arg := range args {
		if arg == "-skip_frame" || arg == "-show_frames" || arg == "-t" || arg == "-ss" || strings.HasPrefix(arg, "-frames") {
			t.Fatalf("syntax verification decoded or restricted the complete source: %v", args)
		}
	}
}
