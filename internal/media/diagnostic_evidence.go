package media

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrDiagnosticEvidence = errors.New("diagnostic_evidence_invalid")

// Codes are fixed strings. Neither errors nor facts expose stderr, addresses,
// input paths, codec options, or names outside the fixed diagnostic allowlist.
type DiagnosticEvidenceError struct {
	Code string
}

func (err *DiagnosticEvidenceError) Error() string { return err.Code }
func (err *DiagnosticEvidenceError) Unwrap() error { return ErrDiagnosticEvidence }

func diagnosticEvidenceError(code string) error {
	return &DiagnosticEvidenceError{Code: code}
}

// These are log observations, not a successful stage or hardware attestation.
// Width through SampleFormat describe the actual output stream. Both input and
// output descriptors must match the fixed sample before any facts are returned.
// SampleFormat uses FFmpeg names: decoded pcm_s16le has sample format "s16".
// HardwarePixelFormat needs independent successful hardware-frame processing,
// output-content validation, and complete process/resource closure. In a decode
// plan that includes hwdownload, its validated raw output supplies the companion
// frame evidence; a compressed combined output needs its own software decode.
type DiagnosticFFmpegEvidence struct {
	Decoder                  string
	Encoder                  string
	Width                    int
	Height                   int
	PixelFormat              string
	SampleRate               int
	Channels                 int
	SampleFormat             string
	HardwarePixelFormat      string
	HardwareFormatSelections int
}

var (
	diagnosticMappingLine = regexp.MustCompile(`^Stream #0:0 -> #0:0 \(([a-z0-9_]{1,32}) \(([a-z0-9_]{1,32})\) -> ([a-z0-9_]{1,32}) \(([a-z0-9_]{1,32})\)\)$`)
	// At debug level dump_stream_format inserts probe frames and time base
	// between the stream index and the codec descriptor on this same line.
	diagnosticStreamLine      = regexp.MustCompile(`^Stream #0:0(?:, [0-9]{1,10}, [0-9]{1,10}/[1-9][0-9]{0,9})?: (Video|Audio): (.+)$`)
	diagnosticCodecHeading    = regexp.MustCompile(`^([a-z0-9_]{1,32})(?: \([^()]{1,128}\))*$`)
	diagnosticReferenceFrames = regexp.MustCompile(`^[0-9]{1,3} reference frames?(?: \([^()]{1,128}\))?$`)
	diagnosticPixelField      = regexp.MustCompile(`^([a-z0-9_]{1,16})(?:\([^()]{1,256}\))?$`)
	diagnosticVideoDimensions = regexp.MustCompile(`^([1-9][0-9]{0,5})x([1-9][0-9]{0,5})(?: \(([1-9][0-9]{0,5})x([1-9][0-9]{0,5})\))?(?: \[SAR [0-9]{1,10}:[0-9]{1,10} DAR [0-9]{1,10}:[0-9]{1,10}\])?$`)
	diagnosticRateField       = regexp.MustCompile(`^([1-9][0-9]{0,5}) Hz$`)
	diagnosticVideoTail       = regexp.MustCompile(`^(?:[0-9]{1,10}/[1-9][0-9]{0,9}|q=-?[0-9]{1,3}--?[0-9]{1,3}|(?:max\. )?[0-9]{1,10}(?:\.[0-9]{1,6})?[kM]? (?:fps|tbr|tbn|kb/s)|SAR [0-9]{1,10}:[0-9]{1,10} DAR [0-9]{1,10}:[0-9]{1,10}|start -?[0-9]{1,10}\.[0-9]{1,6})$`)
	diagnosticAudioTail       = regexp.MustCompile(`^(?:[0-9]{1,10}(?:\.[0-9]{1,6})? kb/s|(?:delay|padding) [0-9]{1,6})$`)
	diagnosticFormatChoice    = regexp.MustCompile(`^\[([a-z0-9_]{1,32}) @ (0x[0-9a-f]{1,16})\] \[debug\] Format ([a-z0-9_]{1,16}) chosen by get_format\(\)\.$`)
	diagnosticLogLevel        = regexp.MustCompile(`^(?:\[[^\]\r\n]{1,128} @ 0x[0-9a-f]{1,16}\] ){0,2}\[(error|fatal|panic)\] `)
)

// ParseDiagnosticFFmpegEvidence understands only the fixed single-input,
// single-output plans and FFmpeg's level-prefixed stderr. Its grammar follows
// FFmpeg n9.0.1, not arbitrary media metadata or a generic log protocol:
// https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/fftools/ffmpeg.c
// https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavformat/dump.c
// https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavcodec/avcodec.c
// https://github.com/FFmpeg/FFmpeg/blob/n9.0.1/libavcodec/decode.c
// Stream mapping is printed before sch_start. Earlier get_format calls may be
// input probing (or decoder initialization); they cannot prove decoded frames.
// A chosen format also precedes hardware setup, so retries and failures reject
// the evidence, and logs alone can never establish successful hardware use.
func ParseDiagnosticFFmpegEvidence(plan DiagnosticPlan, stderr []byte) (DiagnosticFFmpegEvidence, error) {
	if ValidateDiagnosticPlan(plan) != nil {
		return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_plan")
	}
	if len(stderr) == 0 || len(stderr) > plan.Limits.MaximumStderrBytes || !utf8.Valid(stderr) || stderr[len(stderr)-1] != '\n' {
		return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_log")
	}
	for index, value := range stderr {
		if value < 32 && value != '\n' && value != '\t' && !(value == '\r' && index+1 < len(stderr) && stderr[index+1] == '\n') || value == 127 {
			return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_log")
		}
	}
	inputCodec := diagnosticInputCodec(plan.Input.Kind)
	state, inputStreams, outputStreams, mappings := 0, 0, 0, 0
	var facts DiagnosticFFmpegEvidence
	var formatContext, selectedFormat string
	formatSelections := 0
	for _, raw := range strings.Split(string(stderr), "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if len(line) > 8192 {
			return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_log")
		}
		if diagnosticLogLevel.MatchString(line) {
			return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_tool_error")
		}
		if state >= 2 && strings.Contains(line, "get_format") {
			choice := diagnosticFormatChoice.FindStringSubmatch(line)
			if mappings != 1 || len(choice) == 0 || choice[1] != facts.Decoder ||
				formatContext != "" && choice[2] != formatContext ||
				selectedFormat != "" && choice[3] != selectedFormat || formatSelections >= 16 {
				return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_pixel_format")
			}
			if plan.Input.Kind != DiagnosticH264 || !diagnosticDecoderPixelFormat(plan.ActiveDecodeBackend, choice[3]) {
				return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_pixel_format")
			}
			formatContext, selectedFormat = choice[2], choice[3]
			formatSelections++
			continue
		}
		// Stream maps and dump_format records use av_log(NULL, AV_LOG_INFO).
		// A device/filter message quoting a stream line is not that record.
		if !strings.HasPrefix(line, "[info] ") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "[info] "))
		switch {
		case strings.HasPrefix(body, "Input #"):
			if state != 0 || body != "Input #0, "+plan.Input.Format+", from '/proc/self/fd/3':" {
				return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_streams")
			}
			state = 1
		case body == "Stream mapping:":
			if state != 1 || inputStreams != 1 {
				return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_mapping")
			}
			state = 2
		case strings.HasPrefix(body, "Output #"):
			if state != 2 || mappings != 1 || body != "Output #0, "+plan.OutputFormat+", to 'pipe:1':" {
				return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_streams")
			}
			state = 3
		case strings.HasPrefix(body, "Stream #"):
			if strings.Contains(body, " -> ") {
				mapping := diagnosticMappingLine.FindStringSubmatch(body)
				if state != 2 || mappings != 0 || len(mapping) == 0 || mapping[1] != inputCodec || mapping[3] != plan.OutputCodec {
					return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_mapping")
				}
				decoder, encoder := diagnosticCodecName(mapping[1], mapping[2]), diagnosticCodecName(mapping[3], mapping[4])
				if !diagnosticDecoderName(plan, decoder) || encoder != diagnosticEncoderName(plan) {
					return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_codec")
				}
				facts.Decoder, facts.Encoder = decoder, encoder
				mappings++
				continue
			}
			if state != 1 && state != 3 || state == 1 && inputStreams != 0 || state == 3 && outputStreams != 0 {
				return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_streams")
			}
			codec, encoder := inputCodec, ""
			if state == 3 {
				codec, encoder = plan.OutputCodec, facts.Encoder
			}
			shape, ok := diagnosticStreamShape(body, codec, encoder)
			if !ok {
				return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_shape")
			}
			if state == 1 {
				inputStreams++
			} else {
				facts.Width, facts.Height, facts.PixelFormat = shape.Width, shape.Height, shape.PixelFormat
				facts.SampleRate, facts.Channels, facts.SampleFormat = shape.SampleRate, shape.Channels, shape.SampleFormat
				outputStreams++
			}
		}
	}
	if state != 3 || inputStreams != 1 || outputStreams != 1 || mappings != 1 {
		return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_missing")
	}
	if plan.Verification.HardwareDecodeMustBeObserved {
		if formatSelections == 0 {
			return DiagnosticFFmpegEvidence{}, diagnosticEvidenceError("diagnostic_evidence_pixel_format")
		}
		facts.HardwarePixelFormat, facts.HardwareFormatSelections = selectedFormat, formatSelections
	}
	return facts, nil
}

func diagnosticInputCodec(kind DiagnosticSampleKind) string {
	switch kind {
	case DiagnosticRawVideo:
		return "rawvideo"
	case DiagnosticH264:
		return "h264"
	case DiagnosticRawAudio:
		return "pcm_s16le"
	case DiagnosticAAC:
		return "aac"
	}
	return ""
}

// print_stream_maps substitutes native precisely when implementation and codec
// descriptor names are equal. This is not an inference from requested options.
func diagnosticCodecName(codec, implementation string) string {
	if implementation == "native" {
		return codec
	}
	if implementation == codec {
		return ""
	}
	return implementation
}

func diagnosticDecoderName(plan DiagnosticPlan, name string) bool {
	if plan.Input.Kind != DiagnosticH264 {
		return name == diagnosticInputCodec(plan.Input.Kind)
	}
	switch plan.ActiveDecodeBackend {
	case "software", "vaapi":
		return name == "h264"
	case "qsv":
		return name == "h264_qsv"
	case "cuda":
		return name == "h264" || name == "h264_cuvid"
	}
	return false
}

func diagnosticEncoderName(plan DiagnosticPlan) string {
	if plan.OutputCodec != "h264" {
		return plan.OutputCodec
	}
	if plan.ActiveEncodeBackend == "software" {
		return "libx264"
	}
	return "h264_" + plan.ActiveEncodeBackend
}

func diagnosticDecoderPixelFormat(backend, format string) bool {
	if backend == "software" {
		return format == "yuv420p"
	}
	return (backend == "vaapi" || backend == "qsv" || backend == "cuda") && format == backend
}

func diagnosticOutputPixelFormat(encoder, format string) bool {
	switch encoder {
	case "", "rawvideo", "libx264":
		return format == "yuv420p"
	case "h264_vaapi":
		return format == "vaapi"
	case "h264_qsv":
		return format == "qsv" || format == "nv12"
	case "h264_nvenc":
		return format == "cuda" || format == "nv12" || format == "yuv420p"
	}
	return false
}

func diagnosticStreamShape(line, codec, encoder string) (DiagnosticFFmpegEvidence, bool) {
	var shape DiagnosticFFmpegEvidence
	stream := diagnosticStreamLine.FindStringSubmatch(line)
	if len(stream) == 0 {
		return shape, false
	}
	fields, ok := diagnosticDescriptorFields(stream[2])
	if !ok || len(fields) < 3 {
		return shape, false
	}
	heading := diagnosticCodecHeading.FindStringSubmatch(fields[0])
	if len(heading) == 0 || heading[1] != codec {
		return shape, false
	}
	if stream[1] == "Video" && (codec == "h264" || codec == "rawvideo") {
		position := 1
		if diagnosticReferenceFrames.MatchString(fields[position]) {
			position++
		}
		if position+1 >= len(fields) {
			return shape, false
		}
		pixel, dimensions := diagnosticPixelField.FindStringSubmatch(fields[position]), diagnosticVideoDimensions.FindStringSubmatch(fields[position+1])
		if len(pixel) == 0 || len(dimensions) == 0 || !diagnosticOutputPixelFormat(encoder, pixel[1]) {
			return shape, false
		}
		shape.Width, _ = strconv.Atoi(dimensions[1])
		shape.Height, _ = strconv.Atoi(dimensions[2])
		if shape.Width != DiagnosticWidth || shape.Height != DiagnosticHeight || dimensions[3] != "" && (dimensions[3] != dimensions[1] || dimensions[4] != dimensions[2]) {
			return shape, false
		}
		shape.PixelFormat = pixel[1]
		for _, field := range fields[position+2:] {
			if !diagnosticVideoTail.MatchString(field) {
				return shape, false
			}
		}
		return shape, true
	}
	if stream[1] != "Audio" || codec != "aac" && codec != "pcm_s16le" || len(fields) < 4 {
		return shape, false
	}
	rate := diagnosticRateField.FindStringSubmatch(fields[1])
	if len(rate) == 0 || fields[2] != "stereo" {
		return shape, false
	}
	shape.SampleRate, _ = strconv.Atoi(rate[1])
	if shape.SampleRate != DiagnosticSampleRate {
		return shape, false
	}
	if codec == "aac" && fields[3] != "fltp" || codec == "pcm_s16le" && fields[3] != "s16" {
		return shape, false
	}
	shape.Channels, shape.SampleFormat = DiagnosticAudioChannels, fields[3]
	for _, field := range fields[4:] {
		if !diagnosticAudioTail.MatchString(field) {
			return shape, false
		}
	}
	return shape, true
}

// Only top-level commas separate descriptor fields; pixel-color metadata may
// contain commas inside parentheses. The input is already byte/line bounded.
func diagnosticDescriptorFields(value string) ([]string, bool) {
	fields := make([]string, 0, 12)
	depth, start := 0, 0
	for index, character := range value {
		switch character {
		case '(':
			depth++
			if depth > 1 {
				return nil, false
			}
		case ')':
			depth--
			if depth < 0 {
				return nil, false
			}
		case ',':
			if depth == 0 {
				fields = append(fields, strings.TrimSpace(value[start:index]))
				start = index + 1
				if len(fields) > 20 {
					return nil, false
				}
			}
		}
	}
	if depth != 0 {
		return nil, false
	}
	fields = append(fields, strings.TrimSpace(value[start:]))
	return fields, true
}
