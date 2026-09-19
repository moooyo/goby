package transcode

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestVideoCodecOutputMatrixUsesCorrectEncoderAndPixels(t *testing.T) {
	for _, codec := range []string{"h264", "hevc", "av1"} {
		for _, backend := range []string{"software", "vaapi"} {
			for _, depth := range []int{8, 10} {
				if codec == "h264" && depth == 10 {
					continue
				}
				for _, output := range []string{"progressive", "fmp4", "mpegts"} {
					if codec == "av1" && output == "mpegts" {
						continue
					}
					t.Run(codec+"/"+backend+"/"+strconv.Itoa(depth)+"/"+output, func(t *testing.T) {
						p := commandPlan()
						if output == "progressive" {
							p = progressiveVideoPlan()
						} else if output == "fmp4" {
							p.Container, p.HLS.SegmentType = "mp4", "fmp4"
						}
						p.VideoCodec, p.VideoBitDepth = codec, depth
						p.Hardware.Encode = backend
						args, err := BuildArgs(p, 2)
						if err != nil {
							t.Fatal(err)
						}
						if !hasArgumentPair(args, "-c:v", VideoEncoder(codec, backend)) || !hasArgumentPair(args, "-profile:v", VideoOutputProfile(p)) {
							t.Fatalf("output codec/profile mismatch: %v", args)
						}
						if output != "mpegts" && !hasArgumentPair(args, "-tag:v", VideoMP4Tag(p)) {
							t.Fatalf("MP4 sample entry does not describe the coded output: %v", args)
						}
						joined := strings.Join(args, " ")
						if backend == "software" && !hasArgumentPair(args, "-pix_fmt", videoSoftwarePixelFormat(p)) {
							t.Fatalf("output pixel depth is missing: %v", args)
						}
						if backend == "vaapi" && depth == 10 && !strings.Contains(joined, "format=p010le") {
							t.Fatalf("10-bit frames were not uploaded to VAAPI: %v", args)
						}
						if codec == "hevc" && backend == "software" {
							repeatHeaders := "1"
							if output != "mpegts" {
								repeatHeaders = "0"
							}
							if !hasArgumentPair(args, "-forced-idr", "1") || !strings.Contains(joined, "pools=none:frame-threads=2:wpp=0:open-gop=0:scenecut=0:repeat-headers="+repeatHeaders) {
								t.Fatalf("HEVC thread/GOP/parameter-set contract missing: %v", args)
							}
						}
						if codec == "av1" && backend == "software" && (!hasArgumentPair(args, "-lag-in-frames", "0") || strings.Contains(joined, "-preset veryfast")) {
							t.Fatalf("AV1 received H.264 encoder options or delayed output: %v", args)
						}
					})
				}
			}
		}
	}
}

func TestVideoCodecRejectsContradictoryPixelAndContainerContracts(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"H264 10-bit":                   func(p *Plan) { p.VideoBitDepth = 10 },
		"H264 unknown profile":          func(p *Plan) { p.VideoProfile = "high444" },
		"HEVC profile mismatch":         func(p *Plan) { p.VideoCodec, p.VideoProfile, p.VideoBitDepth = "hevc", "main", 10 },
		"HEVC profile mismatch reverse": func(p *Plan) { p.VideoCodec, p.VideoProfile = "hevc", "main10" },
		"HEVC unsupported depth":        func(p *Plan) { p.VideoCodec, p.VideoBitDepth = "hevc", 12 },
		"AV1 unsupported profile":       func(p *Plan) { p.VideoCodec, p.VideoProfile = "av1", "professional" },
		"AV1 TS":                        func(p *Plan) { p.VideoCodec = "av1" },
		"HEVC deferred backend":         func(p *Plan) { p.VideoCodec, p.Hardware.Encode = "hevc", "nvenc" },
		"AV1 deferred backend":          func(p *Plan) { p.VideoCodec, p.Hardware.Encode = "av1", "qsv" },
		"encoding copy identity":        func(p *Plan) { p.VideoCopyCodec = "hevc" },
		"copy options": func(p *Plan) {
			p.VideoCodec, p.Width, p.Height, p.FrameRate, p.VideoBitrate = "copy", 0, 0, 0, 0
			p.VideoBitDepth = 8
		},
		"unknown copy codec": func(p *Plan) {
			p.VideoCodec, p.Width, p.Height, p.FrameRate, p.VideoBitrate = "copy", 0, 0, 0, 0
			p.VideoCopyCodec = "vp9"
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := commandPlan()
			mutate(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("incompatible codec plan accepted: %+v, %v", p, err)
			}
		})
	}
}

func TestVideoCodecAdaptiveLadderEncodesEveryRendition(t *testing.T) {
	for _, codec := range []string{"hevc", "av1"} {
		p := adaptiveHLSPlan()
		p.VideoCodec, p.VideoBitDepth = codec, 10
		args, err := BuildArgs(p, 1)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(args, " ")
		if strings.Count(joined, "-c:v "+VideoEncoder(codec, "software")) != 2 || strings.Count(joined, "expr:gte(t,n_forced*3)") != 2 ||
			strings.Count(joined, "-tag:v "+VideoMP4Tag(p)) != 2 {
			t.Fatalf("adaptive %s output does not encode aligned independent renditions: %v", codec, args)
		}
	}
}

func TestVideoCodecCopyPreservesCorrectMP4Entry(t *testing.T) {
	for _, codec := range []string{"h264", "hevc", "av1"} {
		p := progressiveVideoPlan()
		p.VideoCodec, p.VideoCopyCodec = "copy", codec
		p.Width, p.Height, p.VideoBitrate = 0, 0, 0
		args, err := BuildArgs(p, 1)
		if err != nil || !hasArgumentPair(args, "-c:v", "copy") || !hasArgumentPair(args, "-tag:v", VideoMP4Tag(p)) {
			t.Fatalf("copy sample entry mismatch for %s: %v, %v", codec, args, err)
		}
	}
}

func TestVideoCodecHEVCThreadLimitRespectsEncoderMaximum(t *testing.T) {
	p := commandPlan()
	p.VideoCodec = "hevc"
	args, err := BuildArgs(p, maxThreads)
	if err != nil || !hasArgumentPair(args, "-x265-params", "pools=none:frame-threads=16:wpp=0:open-gop=0:scenecut=0:repeat-headers=1") {
		t.Fatalf("x265 frame workers exceed its sixteen-worker limit: %v, %v", args, err)
	}
}

func TestVideoCodecHEVCParameterSetPlacementDeterminesMP4Entry(t *testing.T) {
	for _, tc := range []struct {
		name, codec, copyCodec, decode, encode, wantTag string
	}{
		{"software", "hevc", "", "", "", "hvc1"},
		{"explicit software", "hevc", "", "software", "software", "hvc1"},
		{"hardware decode only", "hevc", "", "vaapi", "software", "hvc1"},
		{"hardware encode only", "hevc", "", "software", "vaapi", "hev1"},
		{"hardware decode and encode", "hevc", "", "vaapi", "vaapi", "hev1"},
		{"copy", "copy", "hevc", "", "", "hev1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fragmentedHLS := range []bool{false, true} {
				p := progressiveVideoPlan()
				p.VideoCodec, p.VideoCopyCodec = tc.codec, tc.copyCodec
				p.Hardware = Hardware{Decode: tc.decode, Encode: tc.encode}
				if p.VideoCodec == "copy" {
					p.Width, p.Height, p.VideoBitrate = 0, 0, 0
				}
				if fragmentedHLS {
					p.OutputMode, p.SourceFormatStartKnown, p.SegmentSeconds, p.HLS.SegmentType = "", false, 1, "fmp4"
				}
				args, err := BuildArgs(p, 1)
				if err != nil || VideoMP4Tag(p) != tc.wantTag || !hasArgumentPair(args, "-tag:v", tc.wantTag) {
					t.Fatalf("MP4 entry must preserve the encoder's parameter-set placement: %+v, %v, %v", p, args, err)
				}
				if p.VideoCodec != "copy" && (!hasArgumentPair(args, "-b:v", "256000") || !hasArgumentPair(args, "-maxrate", "256000")) {
					t.Fatalf("parameter-set compatibility must not remove bitrate control: %v", args)
				}
			}
		})
	}
}
