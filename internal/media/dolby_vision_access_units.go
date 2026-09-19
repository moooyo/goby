package media

import (
	"bytes"
	"context"
	"encoding/binary"
)

// parseDolbyVisionRPUAccessUnits checks the filtered Annex-B access units and
// each RPU's header and CRC envelope. The caller must additionally require
// FFmpeg's strict, complete RPU syntax validation before trusting this evidence.
func parseDolbyVisionRPUAccessUnits(ctx context.Context, data []byte, metadataCompression ...string) (evidence dolbyVisionRPUEvidence, resultErr error) {
	defer func() {
		if err := ctx.Err(); err != nil {
			evidence, resultErr = dolbyVisionRPUEvidence{}, err
		}
	}()
	if err := ctx.Err(); err != nil {
		return dolbyVisionRPUEvidence{}, err
	}
	invalid := dolbyVisionRPUEvidence{reason: dolbyVisionRPUInvalidScan}
	if len(data) > maxDolbyVisionProbeOutput {
		return dolbyVisionRPUEvidence{reason: dolbyVisionRPUOutputLimit}, nil
	}
	if len(data) == 0 {
		return dolbyVisionRPUEvidence{reason: dolbyVisionRPUNoFrames}, nil
	}
	startCode := []byte{0, 0, 1}
	start := bytes.Index(data, startCode)
	if start < 0 {
		return invalid, nil
	}
	for index, value := range data[:start] {
		if index&4095 == 0 {
			if err := ctx.Err(); err != nil {
				return dolbyVisionRPUEvidence{}, err
			}
		}
		if value != 0 {
			return invalid, nil
		}
	}
	var frames int64
	hasRPU, residualEnabled, residualDisabled := false, false, false
	profile, profileKnown, profileMixed := 0, false, false
	for {
		if err := ctx.Err(); err != nil {
			return dolbyVisionRPUEvidence{}, err
		}
		payloadStart := start + len(startCode)
		next := bytes.Index(data[payloadStart:], startCode)
		end := len(data)
		if next >= 0 {
			next += payloadStart
			end = next
		}
		// Annex B permits zero bytes before the next start code or at EOF.
		for end > payloadStart && data[end-1] == 0 {
			if (end-payloadStart)&4095 == 0 {
				if err := ctx.Err(); err != nil {
					return dolbyVisionRPUEvidence{}, err
				}
			}
			end--
		}
		nal := data[payloadStart:end]
		if len(nal) < 3 || nal[0]&0x80 != 0 || nal[1]&7 == 0 ||
			nal[0]&1 != 0 || nal[1]>>3 != 0 {
			return invalid, nil
		}
		switch (nal[0] >> 1) & 0x3f {
		case 35:
			// AUD has a three-bit picture type and the RBSP stop bit.
			if len(nal) != 3 || nal[2]>>5 > 2 || nal[2]&0x1f != 0x10 {
				return invalid, nil
			}
			if frames > 0 && !hasRPU {
				return dolbyVisionRPUEvidence{reason: dolbyVisionRPUMetadataMissing}, nil
			}
			frames++
			hasRPU = false
		case 62:
			// FFmpeg only parses layer-zero RPUs with temporal_id equal to zero.
			if frames == 0 || hasRPU || nal[1]&7 != 1 {
				return invalid, nil
			}
			disabled, rpuProfile, reason, err := dolbyVisionRPUHeaderResidual(ctx, nal[2:], metadataCompression...)
			if err != nil {
				return dolbyVisionRPUEvidence{}, err
			}
			if reason != "" {
				return dolbyVisionRPUEvidence{reason: reason}, nil
			}
			hasRPU = true
			residualEnabled = residualEnabled || !disabled
			residualDisabled = residualDisabled || disabled
			if !profileKnown {
				profile, profileKnown = rpuProfile, true
			} else if profile != rpuProfile {
				profileMixed = true
			}
		default:
			return invalid, nil
		}
		if next < 0 {
			break
		}
		start = next
	}
	if err := ctx.Err(); err != nil {
		return dolbyVisionRPUEvidence{}, err
	}
	if !hasRPU {
		return dolbyVisionRPUEvidence{reason: dolbyVisionRPUMetadataMissing}, nil
	}
	evidence = dolbyVisionRPUEvidence{
		verified: true, profile: profile, frameCount: frames,
		residualDisabled: !residualEnabled, residualMixed: residualEnabled && residualDisabled,
	}
	if profileMixed {
		evidence.profile, evidence.reason = 0, dolbyVisionRPUProfileMixed
	} else if evidence.residualMixed {
		evidence.reason = dolbyVisionRPUResidualMixed
	} else if residualEnabled {
		evidence.reason = dolbyVisionRPUResidualEnabled
	}
	return evidence, nil
}

// The field order and CRC envelope follow FFmpeg n9.0.1 dovi_rpudec.c.
// The bit reader excludes the CRC and terminator, so truncated headers cannot
// consume either as purported configuration fields.
func dolbyVisionRPUHeaderResidual(ctx context.Context, escaped []byte, metadataCompression ...string) (bool, int, string, error) {
	compressionMode := "none"
	if len(metadataCompression) != 0 {
		compressionMode = metadataCompression[0]
	}
	if len(metadataCompression) > 1 || compressionMode != "none" && compressionMode != "limited" && compressionMode != "extended" {
		return false, 0, dolbyVisionRPUUnsupported, nil
	}
	rbsp, valid, err := dolbyVisionRPUUnescape(ctx, escaped)
	if err != nil {
		return false, 0, "", err
	}
	if !valid || len(rbsp) < 6 || rbsp[0] != 25 || rbsp[len(rbsp)-1] != 0x80 {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	crcStart := len(rbsp) - 5
	body := rbsp[1:crcStart]
	checksum, err := dolbyVisionRPUChecksum(ctx, body)
	if err != nil {
		return false, 0, "", err
	}
	if checksum != binary.BigEndian.Uint32(rbsp[crcStart:crcStart+4]) {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	bits := dolbyVisionRPUHeaderBits{data: body}
	rpuType := bits.read(6)
	format := bits.read(11)
	profile := bits.read(4) // vdr_rpu_profile
	bits.read(4)            // vdr_rpu_level
	sequenceInfo := bits.read(1)
	if bits.invalid {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	if rpuType != 2 || format&0x700 != 0 || sequenceInfo != 1 {
		return false, 0, dolbyVisionRPUResidualUnknown, nil
	}
	bits.read(1) // chroma_resampling_explicit_filter_flag
	coefficientType := bits.read(2)
	if coefficientType > 1 {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	if coefficientType == 0 && bits.ue(32) < 13 {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	bits.read(2)                   // vdr_rpu_normalized_idc
	fullRange := bits.read(1) == 1 // bl_video_full_range_flag
	bits.ue(8)                     // bl_bit_depth_minus8
	enhancementDepth := bits.ue(0xffff)
	if enhancementDepth&0xff > 8 {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	vdrDepth := bits.ue(8) + 8
	bits.read(1) // spatial_resampling_filter_flag
	compression := bits.read(3)
	elSpatial := bits.read(1) == 1 // el_spatial_resampling_filter_flag
	disabled := bits.read(1) == 1
	metadataPresent := bits.read(1)
	usePrevious := bits.read(1)
	if bits.invalid || bits.position >= int64(len(body))*8 || compression == 1 && metadataPresent == 0 {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	if compression > 1 {
		return false, 0, dolbyVisionRPUResidualUnknown, nil
	}
	if compressionMode == "none" && (usePrevious != 0 || compression != 0) {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	// Match ff_dovi_guess_profile_hevc instead of treating an enabled residual
	// flag as proof of FEL; profile 7 requires separate NLQ validation.
	inferredProfile := 0
	switch profile {
	case 0:
		if fullRange {
			inferredProfile = 5
		}
	case 1:
		if elSpatial && !disabled {
			inferredProfile = 4
			if vdrDepth == 12 {
				inferredProfile = 7
			}
		} else {
			inferredProfile = 8
		}
	}
	if inferredProfile < 8 && compressionMode != "none" {
		return false, 0, dolbyVisionRPUInvalidScan, nil
	}
	return disabled, inferredProfile, "", nil
}

func dolbyVisionRPUUnescape(ctx context.Context, escaped []byte) ([]byte, bool, error) {
	rbsp := make([]byte, 0, len(escaped))
	zeroes := 0
	for index, value := range escaped {
		if index&4095 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, false, err
			}
		}
		if zeroes == 2 {
			if value == 3 {
				if index+1 == len(escaped) || escaped[index+1] > 3 {
					return nil, false, nil
				}
				zeroes = 0
				continue
			}
			if value < 3 {
				return nil, false, nil
			}
		}
		rbsp = append(rbsp, value)
		if value == 0 {
			zeroes++
		} else {
			zeroes = 0
		}
	}
	return rbsp, true, nil
}

type dolbyVisionRPUHeaderBits struct {
	data     []byte
	position int64
	invalid  bool
}

func (bits *dolbyVisionRPUHeaderBits) read(width int) uint32 {
	if bits.invalid || width < 0 || width > 32 || int64(width) > int64(len(bits.data))*8-bits.position {
		bits.invalid = true
		return 0
	}
	var value uint32
	for width > 0 {
		available := 8 - int(bits.position&7)
		take := min(width, available)
		value = value<<take | uint32(bits.data[bits.position>>3]>>(available-take))&((1<<take)-1)
		bits.position += int64(take)
		width -= take
	}
	return value
}

func (bits *dolbyVisionRPUHeaderBits) ue(maximum uint32) uint32 {
	zeroes := 0
	for bits.read(1) == 0 {
		if bits.invalid || zeroes == 32 {
			bits.invalid = true
			return 0
		}
		zeroes++
	}
	value := (uint64(1)<<zeroes - 1) + uint64(bits.read(zeroes))
	if value > uint64(maximum) {
		bits.invalid = true
		return 0
	}
	return uint32(value)
}

var dolbyVisionRPUCRCTable = func() [256]uint32 {
	var table [256]uint32
	for index := range table {
		value := uint32(index) << 24
		for bit := 0; bit < 8; bit++ {
			if value&0x80000000 != 0 {
				value = value<<1 ^ 0x04c11db7
			} else {
				value <<= 1
			}
		}
		table[index] = value
	}
	return table
}()

// Dolby Vision uses the non-reflected MPEG-2 CRC32 with no final XOR.
func dolbyVisionRPUChecksum(ctx context.Context, data []byte) (uint32, error) {
	checksum := uint32(0xffffffff)
	for index, value := range data {
		if index&4095 == 0 {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
		}
		checksum = checksum<<8 ^ dolbyVisionRPUCRCTable[byte(checksum>>24)^value]
	}
	return checksum, nil
}
