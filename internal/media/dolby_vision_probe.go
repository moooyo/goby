package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxDolbyVisionProbeOutput = 256 * 1024 * 1024

// Validation reasons are closed codes; decoder output is never cached here.
const (
	dolbyVisionRPUUnsupported     = "unsupported_configuration"
	dolbyVisionRPUOutputLimit     = "scan_output_limit"
	dolbyVisionRPUTimeout         = "scan_timeout"
	dolbyVisionRPUScanFailed      = "scan_failed"
	dolbyVisionRPUDecoderError    = "decoder_error"
	dolbyVisionRPUInvalidScan     = "invalid_scan"
	dolbyVisionRPUNoFrames        = "no_frames"
	dolbyVisionRPUMetadataMissing = "rpu_metadata_missing"
	dolbyVisionRPUResidualUnknown = "residual_flag_unknown"
	dolbyVisionRPUResidualEnabled = "residual_enabled"
	dolbyVisionRPUResidualMixed   = "residual_mixed"
	dolbyVisionRPUProfileMixed    = "rpu_profile_mixed"
)

type dolbyVisionRPUEvidence struct {
	verified         bool
	profile          int
	residualMixed    bool
	residualDisabled bool
	frameCount       int64
	reason           string
}

// The scan uses source-sized time and output budgets without decoding pixels.
// Exhausting its shared budget leaves ordinary media facts available for import.
func runDolbyVisionRPUProbe(ctx context.Context, executable string, file *os.File, size int64, info Info) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if executable == "" {
		executable = "ffmpeg"
	}
	timeout := dolbyVisionRPUScanTimeout(size)
	scanContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	info.Streams = append([]Stream(nil), info.Streams...)
	for index := range info.Streams {
		stream := &info.Streams[index]
		if stream.CodecType != "video" || stream.DolbyVision == nil {
			continue
		}
		metadata := *stream.DolbyVision
		metadata.RPUVerified, metadata.ResidualDisabled, metadata.RPUFrameCount = false, false, 0
		metadata.RPUProfile, metadata.RPUResidualMixed = 0, false
		metadata.RPUValidationReason = dolbyVisionRPUUnsupported
		stream.DolbyVision = &metadata
		if stream.Codec != "hevc" || !metadata.RPUPresent || !metadata.BLPresent ||
			(metadata.Profile != 5 && metadata.Profile != 7 && metadata.Profile != 8) ||
			(metadata.MetadataCompression != "none" && metadata.MetadataCompression != "limited" && metadata.MetadataCompression != "extended") ||
			metadata.Profile < 8 && metadata.MetadataCompression != "none" {
			continue
		}
		output, runErr := runLimitedFilesOutput(scanContext, timeout, maxDolbyVisionProbeOutput, executable, []*os.File{file},
			dolbyVisionRPUProbeArgs(stream.Index)...)
		if err := ctx.Err(); err != nil {
			return Info{}, err
		}
		evidence := dolbyVisionRPUEvidence{}
		switch {
		case errors.Is(runErr, ErrOutputLimit):
			evidence.reason = dolbyVisionRPUOutputLimit
		case errors.Is(runErr, context.DeadlineExceeded) || errors.Is(scanContext.Err(), context.DeadlineExceeded):
			evidence.reason = dolbyVisionRPUTimeout
		case runErr != nil:
			evidence.reason = dolbyVisionRPUScanFailed
		case len(output.stderr) != 0:
			evidence.reason = dolbyVisionRPUDecoderError
		default:
			var parseErr error
			evidence, parseErr = parseDolbyVisionRPUAccessUnits(scanContext, output.stdout, metadata.MetadataCompression)
			if err := ctx.Err(); err != nil {
				return Info{}, err
			}
			if errors.Is(parseErr, context.DeadlineExceeded) {
				evidence = dolbyVisionRPUEvidence{reason: dolbyVisionRPUTimeout}
			} else if parseErr != nil {
				evidence = dolbyVisionRPUEvidence{reason: dolbyVisionRPUScanFailed}
			}
		}
		if evidence.verified {
			// The envelope parser identifies every access unit's residual flag.
			// Force each packet down to AUD+RPU before the native dovi_rpu BSF,
			// which otherwise only parses a packet's last NAL. The BSF parses
			// every full mapping without pixel decoding or stale decoder frame
			// attachments. CRC and configuration consistency were checked above.
			validation, validationErr := runLimitedFilesOutput(scanContext, timeout, maxProbeOutput, executable, []*os.File{file},
				dolbyVisionRPUValidationArgs(stream.Index)...)
			if err := ctx.Err(); err != nil {
				return Info{}, err
			}
			switch {
			case errors.Is(validationErr, ErrOutputLimit):
				evidence = dolbyVisionRPUEvidence{reason: dolbyVisionRPUOutputLimit}
			case errors.Is(validationErr, context.DeadlineExceeded) || errors.Is(scanContext.Err(), context.DeadlineExceeded):
				evidence = dolbyVisionRPUEvidence{reason: dolbyVisionRPUTimeout}
			case validationErr != nil:
				evidence = dolbyVisionRPUEvidence{reason: dolbyVisionRPUScanFailed}
			case len(validation.stderr) != 0:
				evidence = dolbyVisionRPUEvidence{reason: dolbyVisionRPUDecoderError}
			case len(validation.stdout) != 0:
				evidence = dolbyVisionRPUEvidence{reason: dolbyVisionRPUInvalidScan}
			}
		}
		metadata.RPUVerified, metadata.ResidualDisabled = evidence.verified, evidence.residualDisabled
		metadata.RPUProfile, metadata.RPUResidualMixed = evidence.profile, evidence.residualMixed
		metadata.RPUFrameCount, metadata.RPUValidationReason = evidence.frameCount, evidence.reason
	}
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	return info, nil
}

func dolbyVisionRPUScanTimeout(size int64) time.Duration {
	const (
		base            = 2 * time.Minute
		maximum         = 15 * time.Minute
		bytesPerSecond  = 32 * 1024 * 1024
		maximumExtraSec = int64((maximum - base) / time.Second)
	)
	if size <= 0 {
		return base
	}
	seconds := size / bytesPerSecond
	if size%bytesPerSecond != 0 {
		seconds++
	}
	if seconds >= maximumExtraSec {
		return maximum
	}
	return base + time.Duration(seconds)*time.Second
}

func dolbyVisionRPUProbeArgs(streamIndex int) []string {
	return []string{
		"-v", "error", "-nostats", "-nostdin", "-threads", "1",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3", "-map", "0:" + strconv.Itoa(streamIndex), "-c:v", "copy",
		"-bsf:v", "hevc_mp4toannexb,hevc_metadata=aud=insert,filter_units=pass_types=35|62",
		"-f", "hevc", "pipe:1",
	}
}

func dolbyVisionRPUValidationArgs(streamIndex int) []string {
	return []string{
		"-v", "warning", "-xerror", "-nostats", "-nostdin", "-threads", "1",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3", "-map", "0:" + strconv.Itoa(streamIndex), "-c:v", "copy",
		"-bsf:v", "hevc_mp4toannexb,hevc_metadata=aud=insert,filter_units=pass_types=35|62,dovi_rpu=compression=none",
		"-f", "null", "pipe:1",
	}
}

// Every decoded frame must carry an RPU with an explicit residual flag. Parsing
// continues after missing evidence so a later frame cannot be silently skipped.
// This parser permits independent comparison with a complete ffprobe frame scan;
// the production scan reads access units without the cost of pixel decoding.
func parseDolbyVisionRPUFrames(ctx context.Context, data []byte, streamIndex int) (dolbyVisionRPUEvidence, error) {
	if err := ctx.Err(); err != nil {
		return dolbyVisionRPUEvidence{}, err
	}
	invalid := dolbyVisionRPUEvidence{reason: dolbyVisionRPUInvalidScan}
	if len(data) > maxDolbyVisionProbeOutput {
		return dolbyVisionRPUEvidence{reason: dolbyVisionRPUOutputLimit}, nil
	}
	if streamIndex < 0 {
		return invalid, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return invalid, nil
	}
	evidence := dolbyVisionRPUEvidence{}
	seenFrames := false
	allVerified, hasDisabled, hasEnabled := true, false, false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil || key != "frames" || seenFrames {
			return invalid, nil
		}
		seenFrames = true
		opening, err := decoder.Token()
		if err != nil || opening != json.Delim('[') {
			return invalid, nil
		}
		for decoder.More() {
			if err := ctx.Err(); err != nil {
				return dolbyVisionRPUEvidence{}, err
			}
			var frame struct {
				StreamIndex scalar            `json:"stream_index"`
				SideData    []json.RawMessage `json:"side_data_list"`
			}
			if err := decoder.Decode(&frame); err != nil || frame.StreamIndex.missing() {
				return invalid, nil
			}
			index, err := frame.StreamIndex.integer()
			if err != nil || index != int64(streamIndex) {
				return invalid, nil
			}
			evidence.frameCount++
			current := dolbyVisionRPUFrameEvidence(frame.SideData)
			if current.reason == dolbyVisionRPUInvalidScan {
				return invalid, nil
			}
			allVerified = allVerified && current.verified
			if !current.verified {
				if evidence.reason == "" {
					evidence.reason = current.reason
				}
			} else {
				hasDisabled = hasDisabled || current.residualDisabled || current.residualMixed
				hasEnabled = hasEnabled || !current.residualDisabled
			}
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
			return invalid, nil
		}
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return invalid, nil
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalid, nil
	}
	if err := ctx.Err(); err != nil {
		return dolbyVisionRPUEvidence{}, err
	}
	if !seenFrames {
		return invalid, nil
	}
	if evidence.frameCount == 0 {
		evidence.reason = dolbyVisionRPUNoFrames
	} else if allVerified {
		evidence.verified, evidence.residualDisabled = true, !hasEnabled
		evidence.residualMixed = hasDisabled && hasEnabled
		if evidence.residualMixed {
			evidence.reason = dolbyVisionRPUResidualMixed
		} else if hasEnabled {
			evidence.reason = dolbyVisionRPUResidualEnabled
		}
	}
	return evidence, nil
}

func dolbyVisionRPUFrameEvidence(records []json.RawMessage) dolbyVisionRPUEvidence {
	present, hasDisabled, hasEnabled := false, false, false
	reason := ""
	for _, raw := range records {
		var header struct {
			Type string `json:"side_data_type"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return dolbyVisionRPUEvidence{reason: dolbyVisionRPUInvalidScan}
		}
		if header.Type != "Dolby Vision Metadata" {
			continue
		}
		present = true
		var metadata struct {
			ResidualDisabled scalar `json:"disable_residual_flag"`
		}
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return dolbyVisionRPUEvidence{reason: dolbyVisionRPUInvalidScan}
		}
		if metadata.ResidualDisabled.missing() || strings.EqualFold(string(metadata.ResidualDisabled), "unknown") {
			if reason == "" {
				reason = dolbyVisionRPUResidualUnknown
			}
			continue
		}
		value, err := metadata.ResidualDisabled.integer()
		if err != nil || value < 0 || value > 1 {
			return dolbyVisionRPUEvidence{reason: dolbyVisionRPUInvalidScan}
		}
		if value == 0 {
			hasEnabled = true
		} else {
			hasDisabled = true
		}
	}
	if !present {
		return dolbyVisionRPUEvidence{reason: dolbyVisionRPUMetadataMissing}
	}
	if reason != "" {
		return dolbyVisionRPUEvidence{reason: reason}
	}
	return dolbyVisionRPUEvidence{verified: true, residualDisabled: !hasEnabled, residualMixed: hasEnabled && hasDisabled}
}
