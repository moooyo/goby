package media

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func backgroundClipDolbyVisionTestStream(profile int) Stream {
	stream := backgroundClipTestStream()
	stream.Codec, stream.PixelFormat, stream.BitDepth = "hevc", "yuv420p10le", 10
	stream.VideoRange, stream.VideoRangeKnown = "DOVI", true
	stream.DolbyVision = &DolbyVisionMetadata{Profile: profile, RPUPresent: true, BLPresent: true,
		RPUVerified: true, RPUProfile: profile, RPUFrameCount: 960, ResidualDisabled: profile != 7,
		ELPresent: profile == 7, CompatibilityID: 1, MetadataCompression: "none"}
	if profile == 5 {
		stream.DolbyVision.CompatibilityID = 0
	}
	return stream
}

func TestBackgroundClipDolbyVisionRequiresExplicitDeviceAndVerifiedSource(t *testing.T) {
	options := &BackgroundClipDolbyVisionOptions{Device: "/dev/dri/renderD128"}
	for _, profile := range []int{7, 8} {
		stream := backgroundClipDolbyVisionTestStream(profile)
		filter, err := backgroundClipDolbyVisionFilter(stream, options)
		if err != nil || !strings.Contains(filter, "strict_dolbyvision=1") || !strings.Contains(filter, "color_trc=bt709") {
			t.Fatalf("supported profile rejected: %s, %v", filter, err)
		}
		if _, err := backgroundClipDolbyVisionFilter(stream, nil); !errors.Is(err, ErrBackgroundClipDolbyVisionUnavailable) {
			t.Fatalf("missing runtime capability was accepted: %v", err)
		}
		for _, mutate := range []func(*Stream){
			func(s *Stream) { s.DolbyVision.RPUVerified = false },
			func(s *Stream) { s.DolbyVision.RPUPresent = false },
			func(s *Stream) { s.DolbyVision.BLPresent = false },
			func(s *Stream) { s.DolbyVision.RPUFrameCount = 0 },
			func(s *Stream) { s.DolbyVision.RPUProfile = 5 },
			func(s *Stream) { s.DolbyVision.RPUResidualMixed = true },
			func(s *Stream) { s.DolbyVision.ResidualDisabled = !s.DolbyVision.ResidualDisabled },
			func(s *Stream) { s.DolbyVision.CompatibilityID = 4 },
			func(s *Stream) { s.Codec = "h264" },
			func(s *Stream) { s.BitDepth = 8 },
		} {
			invalid := backgroundClipDolbyVisionTestStream(profile)
			mutate(&invalid)
			if _, err := backgroundClipDolbyVisionFilter(invalid, options); !errors.Is(err, ErrBackgroundClipUnsupported) {
				t.Fatalf("unproven source accepted: %+v, %v", invalid, err)
			}
		}
	}
	profile5 := backgroundClipDolbyVisionTestStream(5)
	if _, err := backgroundClipDolbyVisionFilter(profile5, options); !errors.Is(err, ErrBackgroundClipUnsupported) {
		t.Fatalf("profile 5 bypassed its separate acceptance gate: %v", err)
	}
	accepted5 := *options
	accepted5.AllowProfile5 = true
	if _, err := backgroundClipDolbyVisionFilter(profile5, &accepted5); err != nil {
		t.Fatalf("explicit profile 5 policy rejected: %v", err)
	}
	for _, device := range []string{"", "0", "/dev/dri/renderD127", "/dev/dri/renderD256", "/dev/dri/renderD0128", "/dev/dri/renderD128,debug=1", "/dev/dri/../renderD128"} {
		if _, err := backgroundClipDolbyVisionFilter(backgroundClipDolbyVisionTestStream(8), &BackgroundClipDolbyVisionOptions{Device: device}); !errors.Is(err, ErrBackgroundClipDolbyVisionUnavailable) {
			t.Fatalf("invalid render device accepted: %q, %v", device, err)
		}
	}
}

func TestBackgroundClipDolbyVisionCompatibilityGatesAreIndependent(t *testing.T) {
	for _, test := range []struct {
		name          string
		profile       int
		compatibility int
		flag          int
	}{
		{"profile5", 5, 0, 1},
		{"profile84", 8, 4, 2},
		{"profile82", 8, 2, 4},
		{"profile81", 8, 1, 0},
		{"profile7-compat1", 7, 1, 0},
		{"profile7-compat6", 7, 6, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := backgroundClipDolbyVisionTestStream(test.profile)
			stream.DolbyVision.CompatibilityID = test.compatibility
			for mask := 0; mask < 8; mask++ {
				options := &BackgroundClipDolbyVisionOptions{Device: "/dev/dri/renderD128",
					AllowProfile5: mask&1 != 0, AllowProfile84: mask&2 != 0, AllowProfile82: mask&4 != 0}
				filter, err := backgroundClipDolbyVisionFilter(stream, options)
				allowed := test.flag == 0 || mask&test.flag != 0
				if !allowed {
					if !errors.Is(err, ErrBackgroundClipUnsupported) || filter != "" {
						t.Fatalf("another profile's gate admitted this source: mask=%d, filter=%q, err=%v", mask, filter, err)
					}
					continue
				}
				if err != nil || !strings.Contains(filter, "strict_dolbyvision_profile="+strconv.Itoa(test.profile)) {
					t.Fatalf("independently enabled source was rejected: mask=%d, filter=%q, err=%v", mask, filter, err)
				}
			}
			for _, options := range []*BackgroundClipDolbyVisionOptions{nil, {AllowProfile5: true, AllowProfile84: true, AllowProfile82: true}} {
				if filter, err := backgroundClipDolbyVisionFilter(stream, options); filter != "" || !errors.Is(err, ErrBackgroundClipDolbyVisionUnavailable) {
					t.Fatalf("enabled profile bypassed device admission: filter=%q, err=%v", filter, err)
				}
			}
		})
	}
}

func TestBackgroundClipDolbyVisionEnabledProfilesRetainSourceAdmission(t *testing.T) {
	options := &BackgroundClipDolbyVisionOptions{Device: "/dev/dri/renderD128", AllowProfile5: true, AllowProfile84: true, AllowProfile82: true}
	for _, profile := range []struct{ profile, compatibility int }{{5, 0}, {8, 4}, {8, 2}} {
		for _, test := range []struct {
			name   string
			mutate func(*Stream)
		}{
			{"missing configuration", func(s *Stream) { s.DolbyVision = nil }},
			{"unverified RPU", func(s *Stream) { s.DolbyVision.RPUVerified = false }},
			{"missing RPU", func(s *Stream) { s.DolbyVision.RPUPresent = false }},
			{"missing base layer", func(s *Stream) { s.DolbyVision.BLPresent = false }},
			{"no scanned frames", func(s *Stream) { s.DolbyVision.RPUFrameCount = 0 }},
			{"mismatched RPU profile", func(s *Stream) { s.DolbyVision.RPUProfile = 7 }},
			{"mixed residuals", func(s *Stream) { s.DolbyVision.RPUResidualMixed = true }},
			{"residual enabled", func(s *Stream) { s.DolbyVision.ResidualDisabled = false }},
			{"unexpected enhancement layer", func(s *Stream) { s.DolbyVision.ELPresent = true }},
			{"invalid compatibility", func(s *Stream) { s.DolbyVision.CompatibilityID = 6 }},
			{"unsupported codec", func(s *Stream) { s.Codec = "h264" }},
			{"conflicting bit depth", func(s *Stream) { s.BitDepth = 8 }},
		} {
			stream := backgroundClipDolbyVisionTestStream(profile.profile)
			stream.DolbyVision.CompatibilityID = profile.compatibility
			test.mutate(&stream)
			if filter, err := backgroundClipDolbyVisionFilter(stream, options); filter != "" || !errors.Is(err, ErrBackgroundClipUnsupported) {
				t.Fatalf("profile %d.%d enabled an unproven source (%s): filter=%q, err=%v", profile.profile, profile.compatibility, test.name, filter, err)
			}
		}
	}
	missingMELLayer := backgroundClipDolbyVisionTestStream(7)
	missingMELLayer.DolbyVision.ELPresent = false
	if _, err := backgroundClipDolbyVisionFilter(missingMELLayer, options); !errors.Is(err, ErrBackgroundClipUnsupported) {
		t.Fatalf("new profile gates accepted an incomplete profile 7 source: %v", err)
	}
}

func TestBackgroundClipDolbyVisionNewProfilesConsumeNativePixelsBeforeTransforms(t *testing.T) {
	for _, test := range []struct {
		name, transfer, primaries, matrix, sourceRange string
		profile, compatibility                         int
	}{
		{"profile5-iptpqc2", "unknown", "unknown", "unknown", "pc", 5, 0},
		{"profile84-hlg", "arib-std-b67", "bt2020", "bt2020nc", "tv", 8, 4},
		{"profile82-sdr", "bt709", "bt709", "bt709", "tv", 8, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := backgroundClipDolbyVisionTestStream(test.profile)
			stream.DolbyVision.CompatibilityID = test.compatibility
			stream.ColorTransfer, stream.ColorPrimaries = test.transfer, test.primaries
			stream.ColorSpace, stream.ColorRange = test.matrix, test.sourceRange
			originalRPU := *stream.DolbyVision
			settings := &BackgroundClipDolbyVisionOptions{Device: "/dev/dri/renderD128", AllowProfile5: true, AllowProfile84: true, AllowProfile82: true}
			originalSettings := *settings
			geometry := analysisSquareGeometry(stream)
			geometry.clockwise = 180
			plan, err := planBackgroundClipWithOptions(Info{DurationTicks: 180 * TicksPerSecond}, stream, geometry,
				30*TicksPerSecond, 25*TicksPerSecond, BackgroundClipOptions{DolbyVision: settings}, DefaultAnalysisLimits())
			if err != nil {
				t.Fatal(err)
			}
			args := backgroundClipEncodeArgs(stream, plan, DefaultAnalysisLimits())
			filter := args[slices.Index(args, "-vf")+1]
			previous := -1
			for _, stage := range []string{"trim=duration=25.0000000", "setpts=PTS-STARTPTS", "libplacebo=", "strict_dolbyvision_profile=" + strconv.Itoa(test.profile), "hflip,vflip", "scale=w=1280:h=720", "sidedata=mode=delete", "fps=fps=24"} {
				index := strings.Index(filter, stage)
				if index <= previous {
					t.Fatalf("stage %q did not retain native RPU and pixels: %s", stage, filter)
				}
				previous = index
			}
			joined := strings.Join(args, " ")
			for _, forbidden := range []string{"setparams=", "zscale=", "smpte2084", "-hwaccel", "apply_dolbyvision=0"} {
				if strings.Contains(joined, forbidden) {
					t.Fatalf("source was relabeled or bypassed before strict DV processing: %s", joined)
				}
			}
			for _, required := range []string{"-ss 30.0000000", "-accurate_seek", "-frames:v 600", "-c:v libx264", "-color_trc bt709"} {
				if !strings.Contains(joined, required) {
					t.Fatalf("new compatibility profile lost output contract %q: %s", required, joined)
				}
			}
			if *stream.DolbyVision != originalRPU || stream.ColorTransfer != test.transfer || stream.ColorPrimaries != test.primaries || stream.ColorSpace != test.matrix || stream.ColorRange != test.sourceRange || *settings != originalSettings {
				t.Fatal("planning changed the caller's source or admission policy")
			}
			*settings = BackgroundClipDolbyVisionOptions{Device: "/dev/dri/renderD129"}
			if plan.options.DolbyVision == settings || *plan.options.DolbyVision != originalSettings {
				t.Fatal("a caller policy change altered an already captured profile plan")
			}
		})
	}
}

func TestBackgroundClipDolbyVisionTransformsBeforeGeometryAndKeepsSoftwareCodecs(t *testing.T) {
	stream := backgroundClipDolbyVisionTestStream(7)
	stream.IsInterlaced, stream.FieldOrder = true, "bt"
	geometry := analysisSquareGeometry(stream)
	geometry.clockwise = 180
	plan, err := planBackgroundClipWithOptions(Info{DurationTicks: 180 * TicksPerSecond}, stream, geometry, 30*TicksPerSecond, 25*TicksPerSecond,
		BackgroundClipOptions{DolbyVision: &BackgroundClipDolbyVisionOptions{Device: "/dev/dri/renderD129"}}, DefaultAnalysisLimits())
	if err != nil {
		t.Fatal(err)
	}
	args := backgroundClipEncodeArgs(stream, plan, DefaultAnalysisLimits())
	filter := args[slices.Index(args, "-vf")+1]
	previous := -1
	for _, stage := range []string{"setfield=mode=tff", "libplacebo=", "deinterlace=yadif:send_fields=0", "strict_dolbyvision_profile=7", "hflip,vflip", "scale=w=1280:h=720", "sidedata=mode=delete", "fps=fps=24"} {
		index := strings.Index(filter, stage)
		if index <= previous {
			t.Fatalf("stage %q did not preserve raw RPU before pixel transforms: %s", stage, filter)
		}
		previous = index
	}
	joined := strings.Join(args, " ")
	for _, required := range []string{"drm=gobydrm:/dev/dri/renderD129", "vulkan=gobyvk@gobydrm", "-c:v libx264", "-an -sn -dn", "-frames:v 600", "-ss 30.0000000", "-color_trc bt709"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing DV background contract %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{"-hwaccel", "h264_vaapi", "apply_dolbyvision=0", "bwdif", "zscale="} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("DV path lost its source or output contract: %s", joined)
		}
	}
}

func TestBackgroundClipDolbyVisionOptionsDoNotChangeSoftwarePlan(t *testing.T) {
	stream := backgroundClipTestStream()
	settings := &BackgroundClipDolbyVisionOptions{Device: "/dev/dri/renderD128"}
	plan, err := planBackgroundClipWithOptions(Info{DurationTicks: 180 * TicksPerSecond}, stream, analysisSquareGeometry(stream), 0, TicksPerSecond,
		BackgroundClipOptions{DolbyVision: settings}, DefaultAnalysisLimits())
	if err != nil || plan.dolbyVision || strings.Contains(strings.Join(backgroundClipEncodeArgs(stream, plan, DefaultAnalysisLimits()), " "), "vulkan") {
		t.Fatalf("configured DV changed an SDR plan: %+v, %v", plan, err)
	}
	settings.Device = "/dev/dri/renderD129"
	if plan.options.DolbyVision.Device != "/dev/dri/renderD128" {
		t.Fatal("the plan retained a mutable caller option pointer")
	}
}

func TestBackgroundClipDolbyVisionInBandParametersDecodeBeforeRequestedTrim(t *testing.T) {
	options := BackgroundClipOptions{DolbyVision: &BackgroundClipDolbyVisionOptions{
		Device: "/dev/dri/renderD128", AllowProfile5: true, AllowProfile84: true, AllowProfile82: true}}
	for _, profile := range []struct{ profile, compatibility int }{{5, 0}, {8, 4}, {8, 2}, {8, 1}, {7, 6}} {
		for _, inBand := range []bool{false, true} {
			for _, start := range []int64{0, 60 * TicksPerSecond} {
				stream := backgroundClipDolbyVisionTestStream(profile.profile)
				stream.DolbyVision.CompatibilityID = profile.compatibility
				stream.DolbyVision.InBandParameterSets = inBand
				plan, err := planBackgroundClipWithOptions(Info{DurationTicks: 180 * TicksPerSecond}, stream,
					analysisSquareGeometry(stream), start, 25*TicksPerSecond, options, DefaultAnalysisLimits())
				if err != nil {
					t.Fatal(err)
				}
				// Parameter-set evidence belongs to the captured plan, not to a
				// caller-owned metadata pointer that can change after planning.
				stream.DolbyVision.InBandParameterSets = !inBand
				args := backgroundClipEncodeArgs(stream, plan, DefaultAnalysisLimits())
				input, seek := slices.Index(args, "-i"), slices.Index(args, "-ss")
				if plan.decodeFromStart != inBand || input < 0 || args[input+1] != "/proc/self/fd/3" {
					t.Fatalf("parameter-set packaging changed the captured source admission: %+v, %v", plan, args)
				}
				trim := "trim=duration=25.0000000"
				if inBand {
					trim = "trim=start=" + backgroundClipSeconds(start) + ":duration=25.0000000"
					if seek >= 0 || slices.Contains(args, "-accurate_seek") {
						t.Fatalf("in-band VPS/SPS/PPS were skipped before decoder initialization: %v", args)
					}
				} else if seek < 0 || seek >= input || args[seek+1] != backgroundClipSeconds(start) || !slices.Contains(args[:input], "-accurate_seek") {
					t.Fatalf("complete hvcC source lost its ordinary input seek: %v", args)
				}
				filter := args[slices.Index(args, "-vf")+1]
				if !strings.HasPrefix(filter, trim+",setpts=PTS-STARTPTS,libplacebo=") {
					t.Fatalf("source-relative trim must precede the strict renderer without pixel conversion: %s", filter)
				}
				previous := -1
				for _, stage := range []string{"strict_dolbyvision=1", "scale=w=1280:h=720", "sidedata=mode=delete", "fps=fps=24"} {
					index := strings.Index(filter, stage)
					if index <= previous {
						t.Fatalf("in-band parameter handling changed strict render order: %s", filter)
					}
					previous = index
				}
				if plan.start != start || plan.duration != 25*TicksPerSecond || plan.frames != 600 ||
					args[slices.Index(args, "-t")+1] != "25.0000000" || args[slices.Index(args, "-frames:v")+1] != "600" ||
					args[slices.Index(args, "-err_detect")+1] != "crccheck+explode" || slices.Contains(args, "-hwaccel") {
					t.Fatalf("decode-from-start changed finite output or software/RPU decoding: %+v, %v", plan, args)
				}
			}
		}
	}
}

func TestBackgroundClipDolbyVisionEnablesStrictInputCRCDetection(t *testing.T) {
	options := BackgroundClipOptions{DolbyVision: &BackgroundClipDolbyVisionOptions{
		Device: "/dev/dri/renderD128", AllowProfile5: true, AllowProfile84: true, AllowProfile82: true}}
	for _, test := range []struct {
		name                   string
		profile, compatibility int
	}{
		{"profile5", 5, 0}, {"profile84", 8, 4}, {"profile82", 8, 2},
		{"profile81", 8, 1}, {"profile7", 7, 6},
		{"sdr", 0, 0}, {"hdr10", 0, 0}, {"hlg", 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := backgroundClipTestStream()
			if test.profile != 0 {
				stream = backgroundClipDolbyVisionTestStream(test.profile)
				stream.DolbyVision.CompatibilityID = test.compatibility
			} else if test.name != "sdr" {
				stream.PixelFormat, stream.BitDepth = "yuv420p10le", 10
				stream.ColorPrimaries, stream.ColorSpace, stream.ColorRange = "bt2020", "bt2020nc", "tv"
				stream.VideoRange, stream.ColorTransfer = "HDR10", "smpte2084"
				if test.name == "hlg" {
					stream.VideoRange, stream.ColorTransfer = "HLG", "arib-std-b67"
				}
			}
			plan, err := planBackgroundClipWithOptions(Info{DurationTicks: 180 * TicksPerSecond}, stream,
				analysisSquareGeometry(stream), 30*TicksPerSecond, 25*TicksPerSecond, options, DefaultAnalysisLimits())
			if err != nil {
				t.Fatal(err)
			}
			args := backgroundClipEncodeArgs(stream, plan, DefaultAnalysisLimits())
			input, detection, globalFailure := slices.Index(args, "-i"), slices.Index(args, "-err_detect"), slices.Index(args, "-xerror")
			if input < 0 || globalFailure < 0 || globalFailure >= input {
				t.Fatalf("global fail-on-error or source input changed: %v", args)
			}
			if test.profile == 0 {
				if detection >= 0 {
					t.Fatalf("DV-only decoder error policy altered ordinary generation: %v", args)
				}
				return
			}
			if detection < 0 || detection+1 >= input || args[detection+1] != "crccheck+explode" ||
				slices.Contains(args[detection+1:], "-err_detect") {
				t.Fatalf("DV CRC rejection must be enabled exactly once on the source decoder: %v", args)
			}
		})
	}
}

func TestBackgroundClipDolbyVisionProcessProfileAndCapabilityParsing(t *testing.T) {
	software := strings.Join(backgroundClipProcessLimits(false, "/proc/self/fd/4"), " ")
	gpu := strings.Join(backgroundClipProcessLimits(true, "/proc/self/fd/4"), " ")
	if !strings.Contains(software, "--as=2147483648:2147483648") || strings.Contains(gpu, "--as=") || !strings.Contains(gpu, "--data=2147483648:2147483648") {
		t.Fatalf("incorrect CPU/GPU allocation boundaries: %s, %s", software, gpu)
	}
	for _, limits := range []string{software, gpu} {
		if !strings.Contains(limits, "--nofile=64:64 --fsize=0:0 -- /proc/self/fd/4") {
			t.Fatalf("background write/descriptor boundaries changed: %s", limits)
		}
	}
	t.Setenv("GOBY_DATABASE_URL", "not-for-media")
	t.Setenv("XDG_CACHE_HOME", "/do-not-inherit")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/10001")
	environment := strings.Join(backgroundClipGPUEnvironment(), "\n")
	if strings.Contains(environment, "not-for-media") || strings.Contains(environment, "/do-not-inherit") ||
		!strings.Contains(environment, "MESA_SHADER_CACHE_DISABLE=true") || !strings.Contains(environment, "XDG_RUNTIME_DIR=/run/user/10001") {
		t.Fatalf("unsafe GPU environment: %s", environment)
	}
	help := " apply_dolbyvision <boolean>\n strict_dolbyvision <boolean>\n strict_dolbyvision_profile <int>\n"
	if !backgroundClipStrictDolbyVisionOptions(help) || backgroundClipStrictDolbyVisionOptions(strings.Replace(help, " strict_dolbyvision <boolean>\n", "", 1)) ||
		backgroundClipStrictDolbyVisionOptions("description mentions apply_dolbyvision strict_dolbyvision strict_dolbyvision_profile") {
		t.Fatal("capability accepted a missing or merely mentioned strict interface")
	}
}

func TestBackgroundClipRejectsRetainedHDRSideData(t *testing.T) {
	plan := backgroundClipPlan{frames: 24, width: 1280, height: 720}
	data := string(backgroundClipTestProbe(t, plan.frames))
	for _, kind := range []string{"DOVI configuration record", "Dolby Vision RPU Data", "Dolby Vision Metadata", "Mastering display metadata", "Content light level metadata", "HDR Dynamic Metadata SMPTE2094-40 (HDR10+)"} {
		invalid := strings.Replace(data, `"codec_type":"video"`, `"codec_type":"video","side_data_list":[{"side_data_type":"`+kind+`"}]`, 1)
		if err := parseBackgroundClipProbe(strings.NewReader(invalid), plan); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("retained HDR side data accepted: %s, %v", kind, err)
		}
	}
}
