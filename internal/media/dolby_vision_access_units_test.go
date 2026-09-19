package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

func TestDolbyVisionRPUAccessUnitsRequireCompleteRPUCoverage(t *testing.T) {
	disabled, enabled := dolbyVisionAccessUnitFixture(true), dolbyVisionAccessUnitFixture(false)
	aud := []byte{0, 0, 0, 1, 70, 1, 16}
	for _, test := range []struct {
		name     string
		data     []byte
		verified bool
		disabled bool
		mixed    bool
		profile  int
		frames   int64
		reason   string
	}{
		{"single profile 8 access unit", disabled, true, true, false, 8, 1, ""},
		{"all profile 8 access units", append(append([]byte{}, disabled...), disabled...), true, true, false, 8, 2, ""},
		{"profile 7 residual path", enabled, true, false, false, 7, 1, dolbyVisionRPUResidualEnabled},
		{"mixed residuals and profiles", append(append([]byte{}, disabled...), enabled...), true, false, true, 0, 2, dolbyVisionRPUProfileMixed},
		{"no access units", nil, false, false, false, 0, 0, dolbyVisionRPUNoFrames},
		{"AUD without RPU", aud, false, false, false, 0, 0, dolbyVisionRPUMetadataMissing},
		{"missing last RPU", append(append([]byte{}, disabled...), aud...), false, false, false, 0, 0, dolbyVisionRPUMetadataMissing},
		{"missing first RPU", append(append([]byte{}, aud...), disabled...), false, false, false, 0, 0, dolbyVisionRPUMetadataMissing},
		{"RPU without AUD", disabled[len(aud):], false, false, false, 0, 0, dolbyVisionRPUInvalidScan},
		{"duplicate RPU", append(append([]byte{}, disabled...), disabled[len(aud):]...), false, false, false, 0, 0, dolbyVisionRPUInvalidScan},
		{"leading nonzero data", append([]byte{9}, disabled...), false, false, false, 0, 0, dolbyVisionRPUInvalidScan},
		{"empty final NAL", append(append([]byte{}, disabled...), 0, 0, 1), false, false, false, 0, 0, dolbyVisionRPUInvalidScan},
		{"Annex B trailing zeroes", append(append([]byte{}, disabled...), 0, 0, 0), true, true, false, 8, 1, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), test.data)
			if err != nil || evidence.verified != test.verified || evidence.residualDisabled != test.disabled ||
				evidence.residualMixed != test.mixed || evidence.profile != test.profile ||
				evidence.frameCount != test.frames || evidence.reason != test.reason {
				t.Fatalf("incorrect access-unit evidence: %+v, %v", evidence, err)
			}
		})
	}
}

func TestDolbyVisionRPUAccessUnitsRejectTruncationAndCorruption(t *testing.T) {
	complete := dolbyVisionAccessUnitFixture(true)
	for length := 1; length < len(complete); length++ {
		evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), complete[:length])
		if err != nil || evidence.verified || evidence.residualDisabled || evidence.frameCount != 0 {
			t.Fatalf("truncated access unit of %d bytes became verified: %+v, %v", length, evidence, err)
		}
	}
	for _, test := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"forbidden NAL bit", func(data []byte) { data[4] |= 0x80 }},
		{"invalid temporal identifier", func(data []byte) { data[5] = 0 }},
		{"unexpected layer", func(data []byte) { data[5] |= 8 }},
		{"invalid AUD picture type", func(data []byte) { data[6] = 0x70 }},
		{"invalid AUD stop bit", func(data []byte) { data[6] = 0 }},
		{"unexpected NAL type", func(data []byte) { data[11] = 64 }},
		{"RPU skipped by FFmpeg temporal filter", func(data []byte) { data[12] = 2 }},
		{"invalid RPU prefix", func(data []byte) { data[13] = 24 }},
		{"corrupt CRC", func(data []byte) { data[len(data)-2] ^= 0x40 }},
		{"invalid terminator", func(data []byte) { data[len(data)-1] = 0x81 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := append([]byte{}, complete...)
			test.mutate(data)
			evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), data)
			if err != nil || evidence.verified || evidence.residualDisabled || evidence.frameCount != 0 {
				t.Fatalf("corrupt access unit became verified: %+v, %v", evidence, err)
			}
		})
	}
}

func TestDolbyVisionRPUAccessUnitsPreserveProfileInference(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*dolbyVisionAccessUnitHeaderFixture)
		profile int
	}{
		{"profile 5", func(h *dolbyVisionAccessUnitHeaderFixture) { h.rpuProfile, h.fullRange = 0, 1 }, 5},
		{"profile 7 residual path", func(h *dolbyVisionAccessUnitHeaderFixture) { h.residualDisabled = false }, 7},
		{"profile 4 residual path", func(h *dolbyVisionAccessUnitHeaderFixture) { h.residualDisabled, h.vdrDepth = false, 2 }, 4},
		{"profile 8 residual disabled", func(h *dolbyVisionAccessUnitHeaderFixture) {}, 8},
		{"profile 8 without EL resampling", func(h *dolbyVisionAccessUnitHeaderFixture) { h.residualDisabled, h.elSpatial = false, 0 }, 8},
		{"unknown profile", func(h *dolbyVisionAccessUnitHeaderFixture) { h.rpuProfile = 2 }, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := dolbyVisionDefaultAccessUnitHeader()
			test.change(&header)
			evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), dolbyVisionAccessUnitWithHeader(header))
			if err != nil || !evidence.verified || evidence.profile != test.profile ||
				evidence.residualDisabled != header.residualDisabled || evidence.residualMixed || evidence.frameCount != 1 {
				t.Fatalf("incorrect inferred RPU profile: %+v, %v", evidence, err)
			}
		})
	}
}

func TestDolbyVisionRPUAccessUnitsKeepMixedEvidenceSticky(t *testing.T) {
	profile8Enabled := dolbyVisionDefaultAccessUnitHeader()
	profile8Enabled.residualDisabled, profile8Enabled.elSpatial = false, 0
	profile5 := dolbyVisionDefaultAccessUnitHeader()
	profile5.rpuProfile, profile5.fullRange = 0, 1
	for _, test := range []struct {
		name    string
		middle  []byte
		profile int
		mixed   bool
		reason  string
	}{
		{"residual change with stable profile", dolbyVisionAccessUnitWithHeader(profile8Enabled), 8, true, dolbyVisionRPUResidualMixed},
		{"profile change with stable residual", dolbyVisionAccessUnitWithHeader(profile5), 0, false, dolbyVisionRPUProfileMixed},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := append(dolbyVisionAccessUnitFixture(true), test.middle...)
			data = append(data, dolbyVisionAccessUnitFixture(true)...)
			evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), data)
			if err != nil || !evidence.verified || evidence.profile != test.profile || evidence.residualMixed != test.mixed ||
				evidence.residualDisabled != !test.mixed || evidence.frameCount != 3 || evidence.reason != test.reason {
				t.Fatalf("later access unit erased inconsistent evidence: %+v, %v", evidence, err)
			}
		})
	}
}

func TestDolbyVisionRPUHeaderRejectsUnknownAndOutOfRangeFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*dolbyVisionAccessUnitHeaderFixture)
		reason string
	}{
		{"unknown RPU type", func(h *dolbyVisionAccessUnitHeaderFixture) { h.rpuType = 3 }, dolbyVisionRPUResidualUnknown},
		{"unsupported format", func(h *dolbyVisionAccessUnitHeaderFixture) { h.format = 0x100 }, dolbyVisionRPUResidualUnknown},
		{"missing sequence information", func(h *dolbyVisionAccessUnitHeaderFixture) { h.sequenceInfo = 0 }, dolbyVisionRPUResidualUnknown},
		{"unknown coefficient type", func(h *dolbyVisionAccessUnitHeaderFixture) { h.coefficientType = 2 }, dolbyVisionRPUInvalidScan},
		{"small denominator", func(h *dolbyVisionAccessUnitHeaderFixture) { h.denominator = 12 }, dolbyVisionRPUInvalidScan},
		{"large denominator", func(h *dolbyVisionAccessUnitHeaderFixture) { h.denominator = 33 }, dolbyVisionRPUInvalidScan},
		{"invalid base depth", func(h *dolbyVisionAccessUnitHeaderFixture) { h.baseDepth = 9 }, dolbyVisionRPUInvalidScan},
		{"invalid enhancement depth", func(h *dolbyVisionAccessUnitHeaderFixture) { h.enhancementDepth = 9 }, dolbyVisionRPUInvalidScan},
		{"large mapping identifier", func(h *dolbyVisionAccessUnitHeaderFixture) { h.enhancementDepth = 0x10002 }, dolbyVisionRPUInvalidScan},
		{"invalid VDR depth", func(h *dolbyVisionAccessUnitHeaderFixture) { h.vdrDepth = 9 }, dolbyVisionRPUInvalidScan},
		{"unknown DM compression", func(h *dolbyVisionAccessUnitHeaderFixture) { h.compression = 2 }, dolbyVisionRPUResidualUnknown},
		{"compressed missing metadata", func(h *dolbyVisionAccessUnitHeaderFixture) { h.compression = 1 }, dolbyVisionRPUInvalidScan},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := dolbyVisionDefaultAccessUnitHeader()
			test.change(&header)
			evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), dolbyVisionAccessUnitWithHeader(header))
			if err != nil || evidence.verified || evidence.residualDisabled || evidence.frameCount != 0 || evidence.reason != test.reason {
				t.Fatalf("invalid header became authoritative: %+v, %v", evidence, err)
			}
		})
	}
}

func TestDolbyVisionRPUHeaderPreservesSupportedCoefficientAndMappingFields(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*dolbyVisionAccessUnitHeaderFixture)
	}{
		{"floating coefficients", func(h *dolbyVisionAccessUnitHeaderFixture) { h.coefficientType = 1 }},
		{"maximum fixed denominator", func(h *dolbyVisionAccessUnitHeaderFixture) { h.denominator = 32 }},
		{"extended mapping identifier", func(h *dolbyVisionAccessUnitHeaderFixture) { h.enhancementDepth = 0xff02 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := dolbyVisionDefaultAccessUnitHeader()
			test.change(&header)
			evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), dolbyVisionAccessUnitWithHeader(header))
			if err != nil || !evidence.verified || !evidence.residualDisabled || evidence.frameCount != 1 || evidence.reason != "" {
				t.Fatalf("supported header lost its evidence: %+v, %v", evidence, err)
			}
		})
	}
}

func TestDolbyVisionRPUEnvelopeNeverBorrowsCRCBytesForHeader(t *testing.T) {
	for _, body := range [][]byte{{}, {8}, {8, 0}, {8, 0, 8}, {8, 0, 8, 0xff}} {
		data := dolbyVisionAccessUnitEnvelope(body)
		evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), data)
		if err != nil || evidence.verified || evidence.residualDisabled {
			t.Fatalf("short header consumed its CRC envelope: %+v, %v", evidence, err)
		}
	}
}

func TestDolbyVisionRPUUnescapeRequiresValidPreventionBytes(t *testing.T) {
	for _, escaped := range [][]byte{{0, 0, 3}, {0, 0, 3, 4}, {0, 0, 0}, {0, 0, 1}, {0, 0, 2}} {
		if _, valid, err := dolbyVisionRPUUnescape(context.Background(), escaped); valid || err != nil {
			t.Fatalf("invalid emulation prevention accepted: %x, %v", escaped, err)
		}
	}
	got, valid, err := dolbyVisionRPUUnescape(context.Background(), []byte{0, 0, 3, 0, 0, 3, 3})
	if err != nil || !valid || !bytes.Equal(got, []byte{0, 0, 0, 0, 3}) {
		t.Fatalf("emulation prevention bytes were not removed: %x, %v, %v", got, valid, err)
	}
}

func TestDolbyVisionRPUChecksumUsesMPEG2Convention(t *testing.T) {
	checksum, err := dolbyVisionRPUChecksum(context.Background(), []byte("123456789"))
	if err != nil || checksum != 0x0376e6e7 {
		t.Fatalf("incorrect CRC-32/MPEG-2 check value: %08x, %v", checksum, err)
	}
}

func TestDolbyVisionRPUCompressionMatchesConfigurationBeforeNativeParsing(t *testing.T) {
	for _, test := range []struct {
		name   string
		mode   string
		change func(*dolbyVisionAccessUnitHeaderFixture)
	}{
		{"uncompressed previous mapping", "none", func(h *dolbyVisionAccessUnitHeaderFixture) { h.usePrevious = 1 }},
		{"uncompressed DM compression", "none", func(h *dolbyVisionAccessUnitHeaderFixture) { h.compression, h.metadataPresent = 1, 1 }},
		{"profile 7 compression", "limited", func(h *dolbyVisionAccessUnitHeaderFixture) { h.residualDisabled = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := dolbyVisionDefaultAccessUnitHeader()
			test.change(&header)
			evidence, err := parseDolbyVisionRPUAccessUnits(context.Background(), dolbyVisionAccessUnitWithHeader(header), test.mode)
			if err != nil || evidence.verified || evidence.reason != dolbyVisionRPUInvalidScan {
				t.Fatalf("configuration/header contradiction became verified: %+v, %v", evidence, err)
			}
		})
	}
}

func TestDolbyVisionRPUAccessUnitsPropagateCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	evidence, err := parseDolbyVisionRPUAccessUnits(ctx, dolbyVisionAccessUnitFixture(true))
	if !errors.Is(err, context.Canceled) || evidence != (dolbyVisionRPUEvidence{}) {
		t.Fatalf("cancelled scan returned evidence: %+v, %v", evidence, err)
	}
}

type dolbyVisionAccessUnitHeaderFixture struct {
	rpuType, format, sequenceInfo, coefficientType, denominator uint32
	baseDepth, enhancementDepth, vdrDepth, compression          uint32
	rpuProfile, fullRange, elSpatial                            uint32
	metadataPresent, usePrevious                                uint32
	residualDisabled                                            bool
}

func dolbyVisionDefaultAccessUnitHeader() dolbyVisionAccessUnitHeaderFixture {
	return dolbyVisionAccessUnitHeaderFixture{
		rpuType: 2, sequenceInfo: 1, denominator: 13,
		baseDepth: 2, enhancementDepth: 2, vdrDepth: 4, rpuProfile: 1, elSpatial: 1, residualDisabled: true,
	}
}

// The fixture contains a complete minimal mapping, including NLQ when enabled.
// The flag alone does not distinguish profile 7 MEL from FEL.
func dolbyVisionAccessUnitFixture(residualDisabled bool) []byte {
	header := dolbyVisionDefaultAccessUnitHeader()
	header.residualDisabled = residualDisabled
	return dolbyVisionAccessUnitWithHeader(header)
}

func dolbyVisionAccessUnitWithHeader(header dolbyVisionAccessUnitHeaderFixture) []byte {
	var bits dolbyVisionAccessUnitBitWriter
	bits.write(header.rpuType, 6)
	bits.write(header.format, 11)
	bits.write(header.rpuProfile, 4)
	bits.write(0, 4)
	bits.write(header.sequenceInfo, 1)
	bits.write(0, 1)
	bits.write(header.coefficientType, 2)
	if header.coefficientType == 0 {
		bits.ue(header.denominator)
	}
	bits.write(1, 2)
	bits.write(header.fullRange, 1)
	bits.ue(header.baseDepth)
	bits.ue(header.enhancementDepth)
	bits.ue(header.vdrDepth)
	bits.write(0, 1)
	bits.write(header.compression, 3)
	bits.write(header.elSpatial, 1)
	if header.residualDisabled {
		bits.write(1, 1)
	} else {
		bits.write(0, 1)
	}
	bits.write(header.metadataPresent, 1)
	bits.write(header.usePrevious, 1)
	bits.ue(0) // vdr_rpu_id
	bits.ue(0) // mapping_color_space
	bits.ue(0) // mapping_chroma_format_idc
	for component := 0; component < 3; component++ {
		bits.ue(0)
		bits.write(0, 10)
		bits.write(1023, 10)
	}
	if !header.residualDisabled {
		bits.write(0, 3)
		bits.write(0, 10)
		bits.write(1023, 10)
	}
	bits.ue(0) // num_x_partitions_minus1
	bits.ue(0) // num_y_partitions_minus1
	for component := 0; component < 3; component++ {
		bits.ue(0)       // polynomial mapping
		bits.ue(0)       // first order
		bits.write(0, 1) // no linear interpolation
		for _, coefficient := range []uint32{0, 1} {
			if header.coefficientType == 1 {
				bits.write(coefficient*0x3f800000, 32)
			} else {
				bits.ue(coefficient) // signed integer codes for zero and one
				bits.write(0, int(header.denominator))
			}
		}
	}
	if !header.residualDisabled {
		for component := 0; component < 3; component++ {
			bits.write(0, 10)
			for _, coefficient := range []uint32{1, 1, 0} {
				if header.coefficientType == 1 {
					bits.write(coefficient*0x3f800000, 32)
				} else {
					bits.ue(coefficient)
					bits.write(0, int(header.denominator))
				}
			}
		}
	}
	return dolbyVisionAccessUnitEnvelope(bits.data)
}

func dolbyVisionAccessUnitEnvelope(body []byte) []byte {
	rbsp := append([]byte{25}, body...)
	// This independent bitwise implementation also checks the production table.
	checksum := uint32(0xffffffff)
	for _, value := range body {
		checksum ^= uint32(value) << 24
		for bit := 0; bit < 8; bit++ {
			if checksum&0x80000000 != 0 {
				checksum = checksum<<1 ^ 0x04c11db7
			} else {
				checksum <<= 1
			}
		}
	}
	rbsp = binary.BigEndian.AppendUint32(rbsp, checksum)
	rbsp = append(rbsp, 0x80)
	data := []byte{0, 0, 0, 1, 70, 1, 16, 0, 0, 0, 1, 124, 1}
	zeroes := 0
	for _, value := range rbsp {
		if zeroes == 2 && value <= 3 {
			data = append(data, 3)
			zeroes = 0
		}
		data = append(data, value)
		if value == 0 {
			zeroes++
		} else {
			zeroes = 0
		}
	}
	return data
}

type dolbyVisionAccessUnitBitWriter struct {
	data     []byte
	position int
}

func (bits *dolbyVisionAccessUnitBitWriter) write(value uint32, width int) {
	for bit := width - 1; bit >= 0; bit-- {
		if bits.position&7 == 0 {
			bits.data = append(bits.data, 0)
		}
		bits.data[bits.position>>3] |= byte((value>>bit)&1) << (7 - (bits.position & 7))
		bits.position++
	}
}

func (bits *dolbyVisionAccessUnitBitWriter) ue(value uint32) {
	code := value + 1
	width := 0
	for remaining := code; remaining != 0; remaining >>= 1 {
		width++
	}
	bits.write(0, width-1)
	bits.write(code, width)
}
