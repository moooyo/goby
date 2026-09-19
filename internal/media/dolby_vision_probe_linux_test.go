package media

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestDolbyVisionRPUProbeRequiresCleanExtractionAndSyntaxValidation(t *testing.T) {
	for _, test := range []struct {
		name             string
		extractionStderr string
		validationStderr string
		validationStdout string
		validationExit   int
		requireResidual  bool
		mixedResidual    bool
		wantReason       string
		wantVerified     bool
	}{
		{name: "complete clean scans", wantVerified: true},
		{name: "verified profile 7 residual path", requireResidual: true, wantReason: dolbyVisionRPUResidualEnabled, wantVerified: true},
		{name: "mixed residual profiles remain explicit", mixedResidual: true, wantReason: dolbyVisionRPUProfileMixed, wantVerified: true},
		{name: "extraction error with successful exit", extractionStderr: "invalid packet", wantReason: dolbyVisionRPUDecoderError},
		{name: "RPU warning with successful exit", validationStderr: "Error parsing DOVI NAL unit", wantReason: dolbyVisionRPUDecoderError},
		{name: "diagnostic bytes are not ignored", validationStderr: "\n", wantReason: dolbyVisionRPUDecoderError},
		{name: "nonzero syntax validation", validationExit: 1, wantReason: dolbyVisionRPUScanFailed},
		{name: "unexpected syntax output", validationStdout: `{"frames":[]}`, wantReason: dolbyVisionRPUInvalidScan},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			fixture := append(dolbyVisionAccessUnitFixture(!test.requireResidual), dolbyVisionAccessUnitFixture(!test.requireResidual && !test.mixedResidual)...)
			profile := 8
			if test.requireResidual {
				profile = 7
			}
			dolbyVisionProcessFixture(t, directory, "process-extract", fixture, test.extractionStderr, 0)
			dolbyVisionProcessFixture(t, directory, "process-validate", []byte(test.validationStdout), test.validationStderr, test.validationExit)
			executable := filepath.Join(directory, "process")
			program := "#!/bin/sh\nset -eu\ncase \"$*\" in\n*dovi_rpu=compression=none*) exec \"$0-validate\" \"$@\" ;;\n*) exec \"$0-extract\" \"$@\" ;;\nesac\n"
			if err := os.WriteFile(executable, []byte(program), 0700); err != nil {
				t.Fatal(err)
			}
			inputPath := filepath.Join(directory, "media.hevc")
			if err := os.WriteFile(inputPath, fixture, 0600); err != nil {
				t.Fatal(err)
			}
			input, err := os.Open(inputPath)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			info := Info{Container: "hevc", Streams: []Stream{{
				Index: 7, Codec: "hevc", CodecType: "video", VideoRange: "DOVI", VideoRangeKnown: true,
				DolbyVision: &DolbyVisionMetadata{Profile: profile, Level: 6, RPUPresent: true, ELPresent: test.requireResidual, BLPresent: true, CompatibilityID: 6, MetadataCompression: "none"},
			}}}
			probed, err := runDolbyVisionRPUProbe(context.Background(), executable, input, int64(len(fixture)), info)
			if err != nil {
				t.Fatal(err)
			}
			got := probed.Streams[0].DolbyVision
			wantDisabled := test.wantVerified && !test.requireResidual && !test.mixedResidual
			wantProfile := profile
			if !test.wantVerified || test.mixedResidual {
				wantProfile = 0
			}
			if got.RPUVerified != test.wantVerified || got.ResidualDisabled != wantDisabled || got.RPUValidationReason != test.wantReason ||
				got.RPUProfile != wantProfile || got.RPUResidualMixed != test.mixedResidual {
				t.Fatalf("incorrect combined RPU evidence: %+v", got)
			}
			if test.wantVerified && got.RPUFrameCount != 2 || !test.wantVerified && got.RPUFrameCount != 0 {
				t.Fatalf("partial scan retained a frame count: %+v", got)
			}
		})
	}
}

func TestMediaProcessOutputRetainsDiagnosticsWithoutChangingLegacyCallers(t *testing.T) {
	executable := dolbyVisionProcessFixture(t, t.TempDir(), "process", []byte("fixture-output"), "fixture-warning", 0)
	output, err := runLimitedFilesOutput(context.Background(), 5*time.Second, 1024, executable, nil)
	if err != nil || string(output.stdout) != "fixture-output" || string(output.stderr) != "fixture-warning" {
		t.Fatalf("process diagnostics were not retained: %+v, %v", output, err)
	}
	legacy, err := runLimitedFiles(context.Background(), 5*time.Second, 1024, executable, nil)
	if err != nil || string(legacy) != "fixture-output" {
		t.Fatalf("legacy process behavior changed: %q, %v", legacy, err)
	}
}

func dolbyVisionProcessFixture(t *testing.T, directory, name string, stdout []byte, stderr string, exitCode int) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name+".out"), stdout, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name+".err"), []byte(stderr), 0600); err != nil {
		t.Fatal(err)
	}
	program := "#!/bin/sh\nset -eu\n" +
		"cat \"$0.out\"\ncat \"$0.err\" >&2\nexit " + strconv.Itoa(exitCode) + "\n"
	executable := filepath.Join(directory, name)
	if err := os.WriteFile(executable, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	return executable
}

func TestDolbyVisionActualRPUCoverageCRCAndNativeSyntax(t *testing.T) {
	positive := os.Getenv("GOBY_TEST_DOLBY_VISION_FILE")
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if positive == "" || ffmpeg == "" || ffprobe == "" {
		t.Skip("the generated complete Dolby Vision fixtures and pinned tools are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	prober := Prober{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 10 * time.Second}
	for _, test := range []struct {
		name, path string
		valid      bool
	}{
		{"complete", positive, true},
		{"missing middle RPU", filepath.Join(filepath.Dir(positive), "profile81-missing-middle-rpu.mp4"), false},
		{"configuration without RPU", filepath.Join(filepath.Dir(positive), "profile81-config-without-rpu.mp4"), false},
		{"middle RPU CRC damage", filepath.Join(filepath.Dir(positive), "profile81-corrupt-middle-crc.mp4"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			info, err := prober.Probe(ctx, test.path)
			if err != nil || len(info.Streams) != 1 || info.Streams[0].DolbyVision == nil {
				t.Fatalf("fixture is not an inspectable configured video: %+v, %v", info, err)
			}
			dv := info.Streams[0].DolbyVision
			if dv.RPUVerified != test.valid || test.valid && (dv.RPUFrameCount != 96 || dv.RPUProfile != 8 || dv.MetadataCompression != "none") {
				t.Fatalf("incorrect complete-source RPU evidence: %+v", dv)
			}
		})
	}
	t.Run("CRC-valid malformed mapping", func(t *testing.T) {
		path := os.Getenv("GOBY_TEST_DOLBY_VISION_BAD_SYNTAX_FILE")
		if path == "" {
			t.Skip("a CRC-valid mapping syntax mutation is required")
		}
		input, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		output, err := runLimitedFilesOutput(ctx, 20*time.Second, maxDolbyVisionProbeOutput, ffmpeg, []*os.File{input}, dolbyVisionRPUProbeArgs(0)...)
		if err != nil || len(output.stderr) != 0 {
			t.Fatalf("syntax fixture cannot be extracted independently: %v, %s", err, output.stderr)
		}
		envelope, err := parseDolbyVisionRPUAccessUnits(ctx, output.stdout, "none")
		if err != nil || !envelope.verified || envelope.frameCount != 96 {
			t.Fatalf("syntax negative does not retain valid headers, CRCs, and RPU coverage: %+v, %v", envelope, err)
		}
		info, err := prober.Probe(ctx, path)
		if err != nil || len(info.Streams) != 1 || info.Streams[0].DolbyVision == nil {
			t.Fatalf("syntax fixture was not importable: %+v, %v", info, err)
		}
		dv := info.Streams[0].DolbyVision
		if dv.RPUVerified || dv.RPUValidationReason != dolbyVisionRPUScanFailed && dv.RPUValidationReason != dolbyVisionRPUDecoderError {
			t.Fatalf("native mapping validation silently accepted the damaged middle RPU: %+v", dv)
		}
	})
}
