package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func diagnosticEvidencePlan(t *testing.T, mode DiagnosticMode, sample DiagnosticSampleKind, profile DiagnosticProfile) DiagnosticPlan {
	t.Helper()
	plan, err := BuildDiagnosticPlan(mode, sample, profile)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func diagnosticEvidenceCode(t *testing.T, facts DiagnosticFFmpegEvidence, err error, code string) {
	t.Helper()
	var detail *DiagnosticEvidenceError
	if !errors.Is(err, ErrDiagnosticEvidence) || !errors.As(err, &detail) || detail.Code != code || err.Error() != code {
		t.Fatalf("evidence error = %v, want fixed code %s", err, code)
	}
	if facts != (DiagnosticFFmpegEvidence{}) {
		t.Fatal("invalid evidence returned partial facts")
	}
}

// These are synthetic source-derived grammar fixtures, not saved executions or
// evidence of an installed codec/GPU. The expected implementation names below
// are explicit test inputs rather than values copied from the parser's policy.
func diagnosticEvidenceVideoLog(mode DiagnosticMode, decoder, encoder, outputPixel, decodePixel string) string {
	inputCodec, inputFormat := "h264", "h264"
	inputDescriptor := "h264 (High), 1 reference frame, yuv420p(tv, bt709, progressive, left), 320x192 [SAR 1:1 DAR 5:3], 0/1, 8 fps, 8 tbr, 1200k tbn"
	if mode == DiagnosticEncode {
		inputCodec, inputFormat = "rawvideo", "rawvideo"
		inputDescriptor = "rawvideo, 1 reference frame (I420 / 0x30323449), yuv420p, 320x192, 0/1, 5898 kb/s, 8 tbr, 8 tbn"
	}
	outputCodec, outputFormat := "h264", "h264"
	outputDescriptor := "h264 (High), " + outputPixel + "(tv, progressive), 320x192 [SAR 1:1 DAR 5:3], 0/1, q=2-31, 500 kb/s, 8 fps, 8 tbn"
	if mode == DiagnosticDecode {
		outputCodec, outputFormat = "rawvideo", "rawvideo"
		outputDescriptor = "rawvideo (I420 / 0x30323449), " + outputPixel + ", 320x192, 0/1, q=2-31, 5898 kb/s, 8 fps, 8 tbn"
	}
	decoderLabel, encoderLabel := decoder, encoder
	if decoder == inputCodec {
		decoderLabel = "native"
	}
	if encoder == outputCodec {
		encoderLabel = "native"
	}
	probe := ""
	if mode != DiagnosticEncode {
		probe = "[h264 @ 0x1111] [debug] Format yuv420p chosen by get_format().\n"
	}
	choice := ""
	if decodePixel != "" {
		choice = "[" + decoder + " @ 0x2222] [debug] Format " + decodePixel + " chosen by get_format().\n"
	}
	return probe + "[info] Input #0, " + inputFormat + ", from '/proc/self/fd/3':\n" +
		"[info]   Duration: N/A, bitrate: N/A\n" +
		"[info]   Stream #0:0, 8, 1/1200000: Video: " + inputDescriptor + "\n" +
		"[info] Stream mapping:\n" +
		"[info]   Stream #0:0 -> #0:0 (" + inputCodec + " (" + decoderLabel + ") -> " + outputCodec + " (" + encoderLabel + "))\n" + choice +
		"[info] Output #0, " + outputFormat + ", to 'pipe:1':\n" +
		"[info]   Stream #0:0, 0, 1/8: Video: " + outputDescriptor + "\n"
}

func diagnosticEvidenceAudioLog(mode DiagnosticMode) string {
	inputCodec, inputFormat, inputDescriptor := "aac", "aac", "aac (LC), 48000 Hz, stereo, fltp, 128 kb/s"
	outputCodec, outputFormat, outputDescriptor := "aac", "adts", "aac (LC), 48000 Hz, stereo, fltp, delay 1024, 128 kb/s"
	if mode == DiagnosticEncode {
		inputCodec, inputFormat, inputDescriptor = "pcm_s16le", "s16le", "pcm_s16le, 48000 Hz, stereo, s16, 1536 kb/s"
	}
	if mode == DiagnosticDecode {
		outputCodec, outputFormat, outputDescriptor = "pcm_s16le", "s16le", "pcm_s16le, 48000 Hz, stereo, s16, 1536 kb/s"
	}
	return "[info] Input #0, " + inputFormat + ", from '/proc/self/fd/3':\n" +
		"[info]   Stream #0:0, 8, 1/48000: Audio: " + inputDescriptor + "\n" +
		"[info] Stream mapping:\n" +
		"[info]   Stream #0:0 -> #0:0 (" + inputCodec + " (native) -> " + outputCodec + " (native))\n" +
		"[info] Output #0, " + outputFormat + ", to 'pipe:1':\n" +
		"[info]   Stream #0:0, 0, 1/48000: Audio: " + outputDescriptor + "\n"
}

func TestDiagnosticEvidenceSoftwareMappingAndActualShape(t *testing.T) {
	cases := []struct {
		mode                                  DiagnosticMode
		sample                                DiagnosticSampleKind
		decoder, encoder, pixel, sampleFormat string
	}{
		{DiagnosticDecode, DiagnosticH264, "h264", "rawvideo", "yuv420p", ""},
		{DiagnosticEncode, DiagnosticRawVideo, "rawvideo", "libx264", "yuv420p", ""},
		{DiagnosticCombined, DiagnosticH264, "h264", "libx264", "yuv420p", ""},
		{DiagnosticDecode, DiagnosticAAC, "aac", "pcm_s16le", "", "s16"},
		{DiagnosticEncode, DiagnosticRawAudio, "pcm_s16le", "aac", "", "fltp"},
		{DiagnosticCombined, DiagnosticAAC, "aac", "aac", "", "fltp"},
	}
	for _, test := range cases {
		t.Run(string(test.sample)+"/"+string(test.mode), func(t *testing.T) {
			plan := diagnosticEvidencePlan(t, test.mode, test.sample, DiagnosticProfile{})
			log := diagnosticEvidenceAudioLog(test.mode)
			if test.pixel != "" {
				decodePixel := "yuv420p"
				if test.mode == DiagnosticEncode {
					decodePixel = ""
				}
				log = diagnosticEvidenceVideoLog(test.mode, test.decoder, test.encoder, test.pixel, decodePixel)
			}
			input := []byte(log)
			before := bytes.Clone(input)
			facts, err := ParseDiagnosticFFmpegEvidence(plan, input)
			if err != nil || facts.Decoder != test.decoder || facts.Encoder != test.encoder || facts.PixelFormat != test.pixel || facts.SampleFormat != test.sampleFormat {
				t.Fatalf("software mapping or shape rejected: %v", err)
			}
			if test.pixel != "" {
				if facts.Width != 320 || facts.Height != 192 || facts.SampleRate != 0 || facts.Channels != 0 {
					t.Fatal("video facts did not contain the actual output shape")
				}
			} else if facts.SampleRate != 48000 || facts.Channels != 2 || facts.Width != 0 || facts.Height != 0 {
				t.Fatal("audio facts did not contain the actual output shape")
			}
			if facts.HardwarePixelFormat != "" || facts.HardwareFormatSelections != 0 || !bytes.Equal(input, before) {
				t.Fatal("software evidence claimed hardware or mutated stderr")
			}
		})
	}
}

func TestDiagnosticEvidenceHardwareStagesRecordActualNamesAndFormats(t *testing.T) {
	backends := []struct{ decode, encode, decoder, encoder, pixel string }{
		{"vaapi", "vaapi", "h264", "h264_vaapi", "vaapi"},
		{"qsv", "qsv", "h264_qsv", "h264_qsv", "qsv"},
		{"cuda", "nvenc", "h264", "h264_nvenc", "cuda"},
		{"cuda", "nvenc", "h264_cuvid", "h264_nvenc", "cuda"},
	}
	for _, backend := range backends {
		for _, mode := range []DiagnosticMode{DiagnosticDecode, DiagnosticEncode, DiagnosticCombined} {
			t.Run(backend.decoder+"/"+backend.encode+"/"+string(mode), func(t *testing.T) {
				sample := DiagnosticH264
				decoder, encoder, outputPixel, decodePixel := backend.decoder, backend.encoder, backend.pixel, backend.pixel
				if mode == DiagnosticEncode {
					sample, decoder, decodePixel = DiagnosticRawVideo, "rawvideo", ""
				}
				if mode == DiagnosticDecode {
					encoder, outputPixel = "rawvideo", "yuv420p"
				}
				plan := diagnosticEvidencePlan(t, mode, sample, DiagnosticProfile{Decode: backend.decode, Encode: backend.encode})
				facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(diagnosticEvidenceVideoLog(mode, decoder, encoder, outputPixel, decodePixel)))
				if err != nil || facts.Decoder != decoder || facts.Encoder != encoder || facts.PixelFormat != outputPixel || facts.HardwarePixelFormat != decodePixel {
					t.Fatalf("hardware log evidence rejected: %v", err)
				}
				wantSelections := 1
				if mode == DiagnosticEncode {
					wantSelections = 0
				}
				if facts.HardwareFormatSelections != wantSelections {
					t.Fatal("probe was counted as actual hardware decoding")
				}
			})
		}
	}
}

func TestDiagnosticEvidenceCombinedSoftwareHardwareDirectionsRemainDistinct(t *testing.T) {
	cases := []struct {
		profile                                    DiagnosticProfile
		decoder, encoder, outputPixel, decodePixel string
	}{
		{DiagnosticProfile{Decode: "vaapi"}, "h264", "libx264", "yuv420p", "vaapi"},
		{DiagnosticProfile{Decode: "qsv"}, "h264_qsv", "libx264", "yuv420p", "qsv"},
		{DiagnosticProfile{Decode: "cuda"}, "h264", "libx264", "yuv420p", "cuda"},
		{DiagnosticProfile{Encode: "vaapi"}, "h264", "h264_vaapi", "vaapi", "yuv420p"},
		{DiagnosticProfile{Encode: "qsv"}, "h264", "h264_qsv", "qsv", "yuv420p"},
		{DiagnosticProfile{Encode: "nvenc"}, "h264", "h264_nvenc", "cuda", "yuv420p"},
	}
	for _, test := range cases {
		plan := diagnosticEvidencePlan(t, DiagnosticCombined, DiagnosticH264, test.profile)
		facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(diagnosticEvidenceVideoLog(DiagnosticCombined, test.decoder, test.encoder, test.outputPixel, test.decodePixel)))
		if err != nil || facts.Decoder != test.decoder || facts.Encoder != test.encoder || facts.PixelFormat != test.outputPixel {
			t.Fatalf("combined path rejected: %v", err)
		}
		if test.profile.Decode == "" && facts.HardwarePixelFormat != "" || test.profile.Decode != "" && facts.HardwarePixelFormat != test.decodePixel {
			t.Fatal("hardware encoding was substituted for hardware decoding")
		}
	}
}

func TestDiagnosticEvidenceRejectsMissingDuplicateAndForeignMappings(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticDecode, DiagnosticH264, DiagnosticProfile{})
	log := diagnosticEvidenceVideoLog(DiagnosticDecode, "h264", "rawvideo", "yuv420p", "yuv420p")
	mapping := "[info]   Stream #0:0 -> #0:0 (h264 (native) -> rawvideo (native))\n"
	cases := []struct{ name, log, code string }{
		{"missing", strings.Replace(log, mapping, "", 1), "diagnostic_evidence_pixel_format"},
		{"duplicate", strings.Replace(log, mapping, mapping+mapping, 1), "diagnostic_evidence_mapping"},
		{"foreign-input", strings.Replace(log, "Stream #0:0 ->", "Stream #1:0 ->", 1), "diagnostic_evidence_mapping"},
		{"foreign-output", strings.Replace(log, "-> #0:0", "-> #0:1", 1), "diagnostic_evidence_mapping"},
		{"stream-copy", strings.Replace(log, mapping, "[info]   Stream #0:0 -> #0:0 (copy)\n", 1), "diagnostic_evidence_mapping"},
		{"unknown-decoder", strings.Replace(log, "(h264 (native) ->", "(h264 (uncontrolled_decoder) ->", 1), "diagnostic_evidence_codec"},
		{"wrong-encoder", strings.Replace(log, "-> rawvideo (native)", "-> rawvideo (libx264)", 1), "diagnostic_evidence_codec"},
		{"noncanonical-native", strings.Replace(log, "(h264 (native) ->", "(h264 (h264) ->", 1), "diagnostic_evidence_codec"},
		{"extra-section", log + "[info] Stream mapping:\n", "diagnostic_evidence_mapping"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(test.log))
			diagnosticEvidenceCode(t, facts, err, test.code)
		})
	}
}

func TestDiagnosticEvidenceRejectsMissingConflictingAndUnknownVideoShape(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticDecode, DiagnosticH264, DiagnosticProfile{})
	log := diagnosticEvidenceVideoLog(DiagnosticDecode, "h264", "rawvideo", "yuv420p", "yuv420p")
	output := strings.LastIndex(log, "[info]   Stream #")
	cases := []struct{ name, log, code string }{
		{"input-size", strings.Replace(log, "320x192", "640x96", 1), "diagnostic_evidence_shape"},
		{"output-size", log[:output] + strings.Replace(log[output:], "320x192", "640x96", 1), "diagnostic_evidence_shape"},
		{"missing-output", log[:output], "diagnostic_evidence_missing"},
		{"duplicate-output", log + log[output:], "diagnostic_evidence_streams"},
		{"unknown-pixel", log[:output] + strings.Replace(log[output:], "yuv420p", "unknown_fmt", 1), "diagnostic_evidence_shape"},
		{"wrong-layout", log[:output] + strings.Replace(log[output:], "yuv420p", "nv12", 1), "diagnostic_evidence_shape"},
		{"conflicting-size", log[:output] + strings.Replace(log[output:], "320x192", "320x192, 640x480", 1), "diagnostic_evidence_shape"},
		{"coded-size", log[:output] + strings.Replace(log[output:], "320x192", "320x192 (640x480)", 1), "diagnostic_evidence_shape"},
		{"another-stream", strings.Replace(log, "Stream #0:0, 8", "Stream #0:1, 8", 1), "diagnostic_evidence_shape"},
		{"context-quote", log[:output] + strings.Replace(log[output:], "[info]", "[filter @ 0x4444] [info]", 1), "diagnostic_evidence_missing"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(test.log))
			diagnosticEvidenceCode(t, facts, err, test.code)
		})
	}
}

func TestDiagnosticEvidenceAudioShapeIsObservedAndDoesNotGuessPadding(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticDecode, DiagnosticAAC, DiagnosticProfile{})
	log := diagnosticEvidenceAudioLog(DiagnosticDecode)
	output := strings.LastIndex(log, "[info]   Stream #")
	for _, oldAndNew := range [][2]string{{"48000 Hz", "44100 Hz"}, {"stereo", "mono"}, {"stereo", "2 channels"}, {"s16,", "s32,"}, {"48000 Hz", "48000 Hz, 44100 Hz"}} {
		changed := log[:output] + strings.Replace(log[output:], oldAndNew[0], oldAndNew[1], 1)
		facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(changed))
		diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_shape")
	}
	facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(strings.Replace(log, "48000 Hz", "24000 Hz", 1)))
	diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_shape")
	combined := diagnosticEvidencePlan(t, DiagnosticCombined, DiagnosticAAC, DiagnosticProfile{})
	for _, padding := range []string{"delay 1024", "delay 2112, padding 960", "padding 0"} {
		facts, err = ParseDiagnosticFFmpegEvidence(combined, []byte(strings.Replace(diagnosticEvidenceAudioLog(DiagnosticCombined), "delay 1024", padding, 1)))
		if err != nil || facts.SampleFormat != "fltp" {
			t.Fatalf("audio padding was incorrectly treated as a decoded-length claim: %v", err)
		}
	}
}

func TestDiagnosticEvidenceHardwareRequiresPostMappingChoiceFromMappedDecoder(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticDecode, DiagnosticH264, DiagnosticProfile{Decode: "vaapi"})
	log := diagnosticEvidenceVideoLog(DiagnosticDecode, "h264", "rawvideo", "yuv420p", "vaapi")
	choice := "[h264 @ 0x2222] [debug] Format vaapi chosen by get_format().\n"
	cases := []struct{ name, log string }{
		{"probe-only", strings.Replace(strings.Replace(log, choice, "", 1), "Format yuv420p", "Format vaapi", 1)},
		{"wrong-context", strings.Replace(log, choice, strings.Replace(choice, "[h264 @", "[h264_qsv @", 1), 1)},
		{"missing-context", strings.Replace(log, choice, strings.TrimPrefix(choice, "[h264 @ 0x2222] "), 1)},
		{"wrong-level", strings.Replace(log, choice, strings.Replace(choice, "[debug]", "[verbose]", 1), 1)},
		{"software-fallback", strings.Replace(log, choice, strings.Replace(choice, "Format vaapi", "Format yuv420p", 1), 1)},
		{"unknown-format", strings.Replace(log, choice, strings.Replace(choice, "Format vaapi", "Format driver_unknown", 1), 1)},
		{"conflicting-context", log + strings.Replace(choice, "0x2222", "0x3333", 1)},
		{"conflicting-format", log + strings.Replace(choice, "Format vaapi", "Format qsv", 1)},
		{"retry", log + "[h264 @ 0x2222] [debug] Format vaapi not usable, retrying get_format() without it.\n"},
		{"selection-limit", log + strings.Repeat(choice, 16)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(test.log))
			diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_pixel_format")
		})
	}
	facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(log+choice))
	if err != nil || facts.HardwareFormatSelections != 2 || facts.HardwarePixelFormat != "vaapi" {
		t.Fatalf("consistent repeated format choice was rejected: %v", err)
	}
}

func TestDiagnosticEvidenceHardwareCannotBeInferredFromRequestedOptionsOrEncoder(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticCombined, DiagnosticH264, DiagnosticProfile{Decode: "qsv", Encode: "qsv"})
	log := diagnosticEvidenceVideoLog(DiagnosticCombined, "h264_qsv", "h264_qsv", "qsv", "")
	facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(log))
	diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_pixel_format")
	log = diagnosticEvidenceVideoLog(DiagnosticCombined, "h264", "h264_qsv", "qsv", "qsv")
	facts, err = ParseDiagnosticFFmpegEvidence(plan, []byte(log))
	diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_codec")
	encode := diagnosticEvidencePlan(t, DiagnosticEncode, DiagnosticRawVideo, DiagnosticProfile{Encode: "nvenc"})
	log = diagnosticEvidenceVideoLog(DiagnosticEncode, "rawvideo", "libx264", "yuv420p", "")
	facts, err = ParseDiagnosticFFmpegEvidence(encode, []byte(log))
	diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_codec")
}

func TestDiagnosticEvidenceRejectsSetupFailureAndSuccessfulExitCannotOverrideIt(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticDecode, DiagnosticH264, DiagnosticProfile{Decode: "vaapi"})
	log := diagnosticEvidenceVideoLog(DiagnosticDecode, "h264", "rawvideo", "yuv420p", "vaapi")
	for _, failure := range []string{
		"[h264 @ 0x2222] [error] Failed setup for format vaapi: hwaccel initialisation returned error.\n",
		"[vist#0:0/h264 @ 0x3333] [dec:h264 @ 0x4444] [fatal] Error while opening decoder: unavailable\n",
		"[error] Conversion failed\n",
	} {
		facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(log+failure+"[info] Exiting normally, received signal 0.\n"))
		diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_tool_error")
	}
}

func TestDiagnosticEvidenceRejectsUnboundedMalformedAndIncompleteLogs(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticDecode, DiagnosticAAC, DiagnosticProfile{})
	log := diagnosticEvidenceAudioLog(DiagnosticDecode)
	cases := [][]byte{
		nil,
		[]byte(strings.TrimSuffix(log, "\n")),
		[]byte(log + "\x00\n"),
		[]byte(log + "\x1b[31m\n"),
		[]byte(log + "\rnot-a-newline\n"),
		append([]byte(log), 0xff, '\n'),
		[]byte(log + strings.Repeat("x", 8193) + "\n"),
		[]byte(strings.Repeat("x\n", plan.Limits.MaximumStderrBytes/2+1)),
	}
	for _, input := range cases {
		facts, err := ParseDiagnosticFFmpegEvidence(plan, input)
		diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_log")
	}
	facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(strings.ReplaceAll(log, "\n", "\r\n")))
	if err != nil || facts.SampleFormat != "s16" {
		t.Fatalf("complete CRLF log was rejected: %v", err)
	}
}

func TestDiagnosticEvidenceRejectsChangedPlanEndpointsAndLogEndpoints(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticDecode, DiagnosticAAC, DiagnosticProfile{})
	log := diagnosticEvidenceAudioLog(DiagnosticDecode)
	changedPlan := plan
	changedPlan.Args = append([]string(nil), plan.Args...)
	changedPlan.Args[len(changedPlan.Args)-1] = "/arbitrary/output"
	facts, err := ParseDiagnosticFFmpegEvidence(changedPlan, []byte(log))
	diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_plan")
	for _, replacement := range [][2]string{{"/proc/self/fd/3", "/tmp/other-input"}, {"pipe:1", "pipe:2"}, {"Input #0, aac", "Input #0, matroska"}, {"Output #0, s16le", "Output #0, wav"}} {
		facts, err = ParseDiagnosticFFmpegEvidence(plan, []byte(strings.Replace(log, replacement[0], replacement[1], 1)))
		diagnosticEvidenceCode(t, facts, err, "diagnostic_evidence_streams")
	}
}

func TestDiagnosticEvidenceFactsHaveNoRawLogOrStageAcceptanceClaim(t *testing.T) {
	plan := diagnosticEvidencePlan(t, DiagnosticDecode, DiagnosticH264, DiagnosticProfile{Decode: "cuda"})
	log := diagnosticEvidenceVideoLog(DiagnosticDecode, "h264", "rawvideo", "yuv420p", "cuda")
	facts, err := ParseDiagnosticFFmpegEvidence(plan, []byte(log))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"0x2222", "/proc/", "Stream mapping", "Passed", "Accepted", "Verified", "stderr"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("evidence exposed raw data or a stage success claim")
		}
	}
	expected := []string{"Decoder", "Encoder", "Width", "Height", "PixelFormat", "SampleRate", "Channels", "SampleFormat", "HardwarePixelFormat", "HardwareFormatSelections"}
	typeOf := reflect.TypeOf(facts)
	if typeOf.NumField() != len(expected) {
		t.Fatal("evidence shape changed without a fixed-field review")
	}
	for index, name := range expected {
		if typeOf.Field(index).Name != name {
			t.Fatal("evidence field changed without a fixed-field review")
		}
	}
}
