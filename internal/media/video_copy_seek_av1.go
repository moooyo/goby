package media

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	maxVideoCopySeekPacketBytes = 1024 * 1024
	maxVideoCopySeekAV1OBUs     = 4096
)

// videoCopySeekPacketData decodes ffprobe's fixed-width 16-byte hex/ASCII rows.
// The ASCII column is never interpreted as hexadecimal packet contents.
func videoCopySeekPacketData(encoded string) ([]byte, error) {
	invalid := func() ([]byte, error) {
		return nil, fmt.Errorf("invalid or oversized ffprobe packet hexdump")
	}
	if len(encoded) == 0 || len(encoded) > 5*maxVideoCopySeekPacketBytes {
		return invalid()
	}
	if strings.HasPrefix(encoded, "\r\n") {
		encoded = encoded[2:]
	} else {
		encoded = strings.TrimPrefix(encoded, "\n")
	}
	var data []byte
	partial := false
	for len(encoded) != 0 {
		line, rest, complete := strings.Cut(encoded, "\n")
		if !complete || partial {
			return invalid()
		}
		encoded = rest
		line = strings.TrimSuffix(line, "\r")
		if len(line) < 52 || len(line) > 67 || line[8:10] != ": " || line[50] != ' ' {
			return invalid()
		}
		address, err := strconv.ParseUint(line[:8], 16, 32)
		if err != nil || address != uint64(len(data)) {
			return invalid()
		}
		count := len(line) - 51
		if count > maxVideoCopySeekPacketBytes-len(data) {
			return invalid()
		}
		for i := 0; i < 16; i++ {
			position := 10 + 2*i + i/2
			if i >= count {
				if line[position] != ' ' || line[position+1] != ' ' {
					return invalid()
				}
			} else {
				high, highOK := videoCopySeekHexNibble(line[position])
				low, lowOK := videoCopySeekHexNibble(line[position+1])
				if !highOK || !lowOK {
					return invalid()
				}
				value := high<<4 | low
				printable := byte('.')
				if value >= 32 && value <= 126 {
					printable = value
				}
				if line[51+i] != printable {
					return invalid()
				}
				data = append(data, value)
			}
			if i&1 != 0 && line[position+2] != ' ' {
				return invalid()
			}
		}
		partial = count != 16
	}
	if len(data) == 0 {
		return invalid()
	}
	return data, nil
}

func videoCopySeekHexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

// videoCopySeekAV1RestartPacket classifies a bounded AV1 packet or ISOBMFF sample.
// A complete in-band sequence header must precede exactly one displayed key
// frame. External av1C/extradata is intentionally insufficient for this API.
// This checks framing and restart syntax, not entropy-coded tile validity;
// the caller must also prove the packet with a fresh decoder and frame hash.
func videoCopySeekAV1RestartPacket(data []byte) bool {
	if len(data) == 0 || len(data) > maxVideoCopySeekPacketBytes {
		return false
	}
	sequence, reduced, frame, separateHeader, tiles := false, false, false, false, false
	for count := 0; len(data) != 0; count++ {
		if count >= maxVideoCopySeekAV1OBUs {
			return false
		}
		header := data[0]
		data = data[1:]
		// A single operating point with idc zero cannot contain extensions.
		// The same mask rejects forbidden and reserved header bits.
		if header&0x85 != 0 {
			return false
		}
		kind := (header >> 3) & 15
		payload := data
		if header&2 != 0 {
			size, consumed, ok := videoCopySeekAV1LEB128(data)
			if !ok || size > uint64(len(data)-consumed) {
				return false
			}
			payload = data[consumed : consumed+int(size)]
			data = data[consumed+int(size):]
		} else {
			// ISOBMFF permits the final OBU to consume the rest of the sample.
			// Only a frame or tile group can finish this restart proof; all
			// sequence/configuration OBUs still require explicit payload sizes.
			if kind != 4 && kind != 6 {
				return false
			}
			data = nil
		}
		switch kind {
		case 1: // OBU_SEQUENCE_HEADER
			if sequence || frame {
				return false
			}
			parsedReduced, ok := videoCopySeekAV1SequenceHeader(payload)
			if !ok {
				return false
			}
			reduced = parsedReduced
			sequence = true
		case 2: // OBU_TEMPORAL_DELIMITER
			if count != 0 || len(payload) != 0 {
				return false
			}
		case 3, 6: // OBU_FRAME_HEADER, OBU_FRAME
			if !sequence || frame || len(payload) == 0 {
				return false
			}
			// Reduced still-picture headers imply these three values. Other
			// headers must code show_existing_frame=0, KEY_FRAME, show_frame=1.
			if !reduced && payload[0]&0xf0 != 0x10 {
				return false
			}
			separateHeader = kind == 3
			frame = true
			if !separateHeader && len(payload) < 2 {
				return false
			}
		case 4: // OBU_TILE_GROUP
			if !frame || !separateHeader || len(payload) == 0 {
				return false
			}
			tiles = true
		case 5: // OBU_METADATA
			// Only fixed-size HDR metadata is accepted. Scalability, timecode,
			// private metadata, and unknown syntax remain conservative misses.
			if !sequence || frame || !videoCopySeekAV1HDRMetadata(payload) {
				return false
			}
		case 15: // OBU_PADDING
			if len(payload) != 0 && !videoCopySeekAV1Nonzero(payload) {
				return false
			}
		default:
			// Reserved OBUs, redundant headers, and tile lists are not evidence.
			return false
		}
	}
	return sequence && frame && (!separateHeader || tiles)
}

func videoCopySeekAV1LEB128(data []byte) (uint64, int, bool) {
	var value uint64
	for i := 0; i < len(data) && i < 8; i++ {
		value |= uint64(data[i]&0x7f) << (7 * i)
		if data[i]&0x80 == 0 {
			return value, i + 1, value <= 0xffffffff
		}
	}
	return 0, 0, false
}

func videoCopySeekAV1Nonzero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return true
		}
	}
	return false
}

func videoCopySeekAV1HDRMetadata(data []byte) bool {
	kind, consumed, ok := videoCopySeekAV1LEB128(data)
	if !ok {
		return false
	}
	length := 0
	switch kind {
	case 1:
		length = 4
	case 2:
		length = 24
	default:
		return false
	}
	if len(data)-consumed < length+1 {
		return false
	}
	reader := videoCopySeekAV1Bits{data: data, position: (consumed + length) * 8}
	return reader.trailing()
}

type videoCopySeekAV1Bits struct {
	data     []byte
	position int
	failed   bool
}

func (reader *videoCopySeekAV1Bits) read(count int) uint64 {
	if reader.failed || count < 0 || count > 64 || count > len(reader.data)*8-reader.position {
		reader.failed = true
		return 0
	}
	var value uint64
	for range count {
		value = value<<1 | uint64((reader.data[reader.position/8]>>(7-reader.position%8))&1)
		reader.position++
	}
	return value
}

func (reader *videoCopySeekAV1Bits) uvlc() {
	for zeros := 0; zeros < 32; zeros++ {
		if reader.read(1) != 0 {
			reader.read(zeros)
			return
		}
	}
	// UINT32_MAX is not a conforming num_ticks_per_picture_minus_1.
	reader.failed = true
}

func (reader *videoCopySeekAV1Bits) trailing() bool {
	if reader.read(1) != 1 {
		return false
	}
	for reader.position < len(reader.data)*8 && !reader.failed {
		if reader.read(1) != 0 {
			return false
		}
	}
	return !reader.failed
}

// videoCopySeekAV1SequenceHeader consumes all sequence syntax and trailing bits.
// Its single operating point deliberately excludes multilayer dependencies.
func videoCopySeekAV1SequenceHeader(data []byte) (bool, bool) {
	if len(data) == 0 || len(data) > 1024 {
		return false, false
	}
	reader := videoCopySeekAV1Bits{data: data}
	profile := reader.read(3)
	still := reader.read(1) != 0
	reduced := reader.read(1) != 0
	if profile > 2 || reduced && !still {
		return false, false
	}
	validLevel := func(level uint64) bool { return level < 24 || level == 31 }
	if reduced {
		if !validLevel(reader.read(5)) {
			return false, false
		}
	} else {
		decoderModel := false
		delayBits := 0
		if reader.read(1) != 0 {
			if reader.read(32) == 0 || reader.read(32) == 0 {
				return false, false
			}
			if reader.read(1) != 0 {
				reader.uvlc()
			}
			decoderModel = reader.read(1) != 0
			if decoderModel {
				delayBits = int(reader.read(5)) + 1
				if reader.read(32) == 0 {
					return false, false
				}
				reader.read(5)
				reader.read(5)
			}
		}
		initialDelay := reader.read(1) != 0
		if reader.read(5) != 0 || reader.read(12) != 0 {
			return false, false
		}
		level := reader.read(5)
		if !validLevel(level) {
			return false, false
		}
		if level > 7 {
			reader.read(1)
		}
		if decoderModel && reader.read(1) != 0 {
			reader.read(delayBits)
			reader.read(delayBits)
			reader.read(1)
		}
		if initialDelay && reader.read(1) != 0 {
			reader.read(4)
		}
	}
	widthBits, heightBits := int(reader.read(4))+1, int(reader.read(4))+1
	reader.read(widthBits)
	reader.read(heightBits)
	if !reduced && reader.read(1) != 0 {
		delta, additional := reader.read(4), reader.read(3)
		if delta+additional+3 > 16 {
			return false, false
		}
	}
	reader.read(3)
	if !reduced {
		reader.read(4)
		orderHint := reader.read(1) != 0
		if orderHint {
			reader.read(2)
		}
		forceScreen := uint64(2)
		if reader.read(1) == 0 {
			forceScreen = reader.read(1)
		}
		if forceScreen > 0 && reader.read(1) == 0 {
			reader.read(1)
		}
		if orderHint {
			reader.read(3)
		}
	}
	reader.read(3)
	bitDepth := 8
	if reader.read(1) != 0 {
		bitDepth = 10
		if profile == 2 && reader.read(1) != 0 {
			bitDepth = 12
		}
	}
	monochrome := profile != 1 && reader.read(1) != 0
	primaries, transfer, matrix := uint64(2), uint64(2), uint64(2)
	if reader.read(1) != 0 {
		primaries, transfer, matrix = reader.read(8), reader.read(8), reader.read(8)
	}
	if monochrome {
		reader.read(1)
	} else {
		subsamplingX, subsamplingY := false, false
		if primaries != 1 || transfer != 13 || matrix != 0 {
			reader.read(1)
			switch profile {
			case 0:
				subsamplingX, subsamplingY = true, true
			case 2:
				subsamplingX = true
				if bitDepth == 12 {
					subsamplingX = reader.read(1) != 0
					subsamplingY = subsamplingX && reader.read(1) != 0
				}
			}
			if subsamplingX && subsamplingY && reader.read(2) == 3 {
				return false, false
			}
		}
		if matrix == 0 && (subsamplingX || subsamplingY) {
			return false, false
		}
		if primaries == 1 && transfer == 13 && matrix == 0 && profile != 1 && bitDepth != 12 {
			return false, false
		}
		reader.read(1)
	}
	reader.read(1)
	return reduced, reader.trailing()
}
