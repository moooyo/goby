package media

import (
	"context"
	"encoding/binary"
	"io"
)

// This is the existing whole-scan bound, rather than a smaller format limit.
// Even a single NAL at this bound uses only the fixed envelope state below.
const maxDolbyVisionProbeNAL = maxDolbyVisionProbeOutput

// dolbyVisionRPUStream consumes every byte before publishing evidence. Syntax
// failure is sticky, but does not stop draining or hide a later output limit.
// Write and finish belong to the command's stdout copier and its joined caller.
type dolbyVisionRPUStream struct {
	ctx              context.Context
	compressionMode  string
	compressionValid bool
	total            int
	started          bool
	finished         bool
	zeroes           int
	reason           string
	err              error
	nalBytes         int
	nalHeader        [2]byte
	nalType          byte
	rpu              dolbyVisionRPUStreamEnvelope
	frames           int64
	hasRPU           bool
	residualEnabled  bool
	residualDisabled bool
	profile          int
	profileKnown     bool
	profileMixed     bool
}

func newDolbyVisionRPUStream(ctx context.Context, metadataCompression ...string) *dolbyVisionRPUStream {
	mode := "none"
	if len(metadataCompression) > 0 {
		mode = metadataCompression[0]
	}
	return &dolbyVisionRPUStream{
		ctx: ctx, compressionMode: mode,
		compressionValid: len(metadataCompression) <= 1 && (mode == "none" || mode == "limited" || mode == "extended"),
	}
}

func (stream *dolbyVisionRPUStream) Write(data []byte) (int, error) {
	if stream.finished {
		return 0, io.ErrClosedPipe
	}
	if err := stream.ctx.Err(); err != nil {
		stream.err = err
		return 0, err
	}
	if len(data) > maxDolbyVisionProbeOutput-stream.total {
		stream.total = maxDolbyVisionProbeOutput
		stream.reason = dolbyVisionRPUOutputLimit
		return len(data), nil
	}
	stream.total += len(data)
	if stream.reason != "" {
		return len(data), nil
	}
	for index, value := range data {
		if index&4095 == 0 {
			if err := stream.ctx.Err(); err != nil {
				stream.err = err
				return index, err
			}
		}
		if value == 0 {
			stream.zeroes++
			continue
		}
		if value == 1 && stream.zeroes >= 2 {
			if stream.started {
				stream.finishNAL()
			}
			stream.started, stream.zeroes = true, 0
			stream.nalBytes, stream.nalHeader, stream.nalType = 0, [2]byte{}, 0
			stream.rpu = dolbyVisionRPUStreamEnvelope{checksum: 0xffffffff}
		} else {
			if !stream.started {
				stream.reason = dolbyVisionRPUInvalidScan
			} else {
				// Zero bytes are held until the next nonzero byte proves they
				// are payload, rather than Annex-B padding or a start code.
				for stream.zeroes > 0 && stream.reason == "" {
					stream.appendNALByte(0)
					stream.zeroes--
				}
				stream.zeroes = 0
				if stream.reason == "" {
					stream.appendNALByte(value)
				}
			}
		}
		if stream.reason != "" {
			break
		}
	}
	return len(data), nil
}

func (stream *dolbyVisionRPUStream) appendNALByte(value byte) {
	stream.nalBytes++
	if stream.nalBytes > maxDolbyVisionProbeNAL {
		stream.reason = dolbyVisionRPUOutputLimit
		return
	}
	if stream.nalBytes <= 2 {
		stream.nalHeader[stream.nalBytes-1] = value
		if stream.nalBytes == 2 {
			header := stream.nalHeader
			if header[0]&0x80 != 0 || header[1]&7 == 0 || header[0]&1 != 0 || header[1]>>3 != 0 {
				stream.reason = dolbyVisionRPUInvalidScan
				return
			}
			stream.nalType = (header[0] >> 1) & 0x3f
			if stream.nalType != 35 && stream.nalType != 62 ||
				stream.nalType == 62 && (stream.frames == 0 || stream.hasRPU || header[1]&7 != 1) {
				stream.reason = dolbyVisionRPUInvalidScan
			}
		}
		return
	}
	if stream.nalType == 35 {
		if stream.nalBytes != 3 || value>>5 > 2 || value&0x1f != 0x10 {
			stream.reason = dolbyVisionRPUInvalidScan
		}
		return
	}
	if stream.nalBytes == 3 && !stream.compressionValid {
		stream.reason = dolbyVisionRPUUnsupported
		return
	}
	if !stream.rpu.append(value) {
		stream.reason = dolbyVisionRPUInvalidScan
	}
}

func (stream *dolbyVisionRPUStream) finishNAL() {
	if stream.reason != "" {
		return
	}
	if stream.nalBytes < 3 {
		stream.reason = dolbyVisionRPUInvalidScan
		return
	}
	if stream.nalType == 35 {
		if stream.frames > 0 && !stream.hasRPU {
			stream.reason = dolbyVisionRPUMetadataMissing
			return
		}
		stream.frames++
		stream.hasRPU = false
		return
	}
	if !stream.compressionValid {
		stream.reason = dolbyVisionRPUUnsupported
		return
	}
	disabled, profile, reason := stream.rpu.finish(stream.compressionMode)
	if reason != "" {
		stream.reason = reason
		return
	}
	stream.hasRPU = true
	stream.residualEnabled = stream.residualEnabled || !disabled
	stream.residualDisabled = stream.residualDisabled || disabled
	if !stream.profileKnown {
		stream.profile, stream.profileKnown = profile, true
	} else if stream.profile != profile {
		stream.profileMixed = true
	}
}

func (stream *dolbyVisionRPUStream) finish() (dolbyVisionRPUEvidence, error) {
	if err := stream.ctx.Err(); err != nil {
		return dolbyVisionRPUEvidence{}, err
	}
	if stream.err != nil {
		return dolbyVisionRPUEvidence{}, stream.err
	}
	if !stream.finished {
		stream.finished = true
		if stream.reason == "" {
			switch {
			case stream.total == 0:
				stream.reason = dolbyVisionRPUNoFrames
			case !stream.started:
				stream.reason = dolbyVisionRPUInvalidScan
			default:
				// Pending zeroes are Annex-B trailing_zero_8bits at EOF.
				stream.finishNAL()
				if stream.reason == "" && !stream.hasRPU {
					stream.reason = dolbyVisionRPUMetadataMissing
				}
			}
		}
	}
	if stream.reason != "" {
		return dolbyVisionRPUEvidence{reason: stream.reason}, nil
	}
	evidence := dolbyVisionRPUEvidence{
		verified: true, profile: stream.profile, frameCount: stream.frames,
		residualDisabled: !stream.residualEnabled,
		residualMixed:    stream.residualEnabled && stream.residualDisabled,
	}
	if stream.profileMixed {
		evidence.profile, evidence.reason = 0, dolbyVisionRPUProfileMixed
	} else if evidence.residualMixed {
		evidence.reason = dolbyVisionRPUResidualMixed
	} else if stream.residualEnabled {
		evidence.reason = dolbyVisionRPUResidualEnabled
	}
	return evidence, nil
}

// The first byte is the RPU prefix. A five-byte circular tail excludes the CRC
// and terminator from both the checksum and the header prefix. Mapping bytes
// outside the prefix are checksummed and discarded, never silently truncated.
type dolbyVisionRPUStreamEnvelope struct {
	escapedZeroes     int
	pendingPrevention bool
	rbspBytes         int64
	bodyBytes         int64
	bodyPrefix        [64]byte
	tail              [5]byte
	tailBytes         int
	tailNext          int
	checksum          uint32
}

func (rpu *dolbyVisionRPUStreamEnvelope) append(value byte) bool {
	if rpu.pendingPrevention {
		if value > 3 {
			return false
		}
		rpu.pendingPrevention = false
	}
	if rpu.escapedZeroes == 2 {
		if value == 3 {
			rpu.pendingPrevention, rpu.escapedZeroes = true, 0
			return true
		}
		if value < 3 {
			return false
		}
	}
	if value == 0 {
		rpu.escapedZeroes++
	} else {
		rpu.escapedZeroes = 0
	}
	rpu.rbspBytes++
	if rpu.rbspBytes == 1 {
		return value == 25
	}
	if rpu.tailBytes == len(rpu.tail) {
		body := rpu.tail[rpu.tailNext]
		if rpu.bodyBytes < int64(len(rpu.bodyPrefix)) {
			rpu.bodyPrefix[rpu.bodyBytes] = body
		}
		rpu.bodyBytes++
		rpu.checksum = rpu.checksum<<8 ^ dolbyVisionRPUCRCTable[byte(rpu.checksum>>24)^body]
	} else {
		rpu.tailBytes++
	}
	rpu.tail[rpu.tailNext] = value
	rpu.tailNext = (rpu.tailNext + 1) % len(rpu.tail)
	return true
}

func (rpu *dolbyVisionRPUStreamEnvelope) finish(compressionMode string) (bool, int, string) {
	if rpu.pendingPrevention || rpu.rbspBytes < 6 || rpu.tailBytes != len(rpu.tail) {
		return false, 0, dolbyVisionRPUInvalidScan
	}
	var tail [5]byte
	for index := range tail {
		tail[index] = rpu.tail[(rpu.tailNext+index)%len(rpu.tail)]
	}
	if tail[4] != 0x80 || binary.BigEndian.Uint32(tail[:4]) != rpu.checksum {
		return false, 0, dolbyVisionRPUInvalidScan
	}
	// 64 bytes exceed every path through the bounded header fields, including
	// rejected Exp-Golomb values. The complete body length still fences EOF.
	prefix := rpu.bodyPrefix[:min(rpu.bodyBytes, int64(len(rpu.bodyPrefix)))]
	disabled, profile, reason, _ := dolbyVisionRPUHeaderFields(prefix, rpu.bodyBytes, compressionMode)
	return disabled, profile, reason
}
