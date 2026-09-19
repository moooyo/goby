package transcode

import "encoding/binary"

// The startup detector checks the codec configuration and complete packet
// framing emitted by the controlled MP4 muxer. It is not a video decoder.
func (p *progressiveVideoParser) videoConfiguration(entry string, boxes []progressiveVideoBox) (string, int, error) {
	codec, configuration := "", ""
	switch entry {
	case "avc1":
		codec, configuration = "h264", "avcC"
	case "hvc1", "hev1":
		codec, configuration = "hevc", "hvcC"
	case "av01":
		codec, configuration = "av1", "av1C"
	default:
		return "", 0, invalidProgressive("video codec sample entry")
	}
	if codec != p.wantCodec {
		return "", 0, invalidProgressive("video codec differs from selected plan")
	}
	for _, box := range boxes {
		if (box.kind == "avcC" || box.kind == "hvcC" || box.kind == "av1C") && box.kind != configuration {
			return "", 0, invalidProgressive("conflicting video codec configuration")
		}
	}
	box, err := progressiveVideoOne(boxes, configuration, true)
	if err != nil {
		return "", 0, err
	}
	var length int
	switch codec {
	case "h264":
		length, err = progressiveVideoAVCC(box.payload)
	case "hevc":
		length, err = progressiveVideoHVCC(box.payload)
	case "av1":
		err = p.av1Configuration(box.payload)
	}
	return codec, length, err
}

// HEVCDecoderConfigurationRecord follows the hvcC structure used by FFmpeg's
// libavformat/hevc.c. Required parameter arrays must be bounded and complete.
func progressiveVideoHVCC(data []byte) (int, error) {
	invalid := func() (int, error) { return 0, invalidProgressive("video HEVC decoder configuration") }
	if len(data) < 23 || data[0] != 1 || data[13]&0xf0 != 0xf0 || data[15]&0xfc != 0xfc ||
		data[16]&0xfc != 0xfc || data[17]&0xf8 != 0xf8 || data[18]&0xf8 != 0xf8 || data[21]&3 == 2 {
		return invalid()
	}
	// Copy output retains the source's profile, chroma and bit depth. The
	// encoder matrix is narrower, but must not constrain packet-copy framing.
	if data[1]&31 == 0 && binary.BigEndian.Uint32(data[2:6]) == 0 {
		return invalid()
	}
	length, offset, elements := int(data[21]&3)+1, 23, 0
	seen := make(map[byte]bool)
	for array := 0; array < int(data[22]); array++ {
		if len(data)-offset < 3 || data[offset]&0x40 != 0 {
			return invalid()
		}
		kind := data[offset] & 63
		if seen[kind] || kind != 32 && kind != 33 && kind != 34 && kind != 39 && kind != 40 {
			return invalid()
		}
		seen[kind] = true
		count := int(binary.BigEndian.Uint16(data[offset+1 : offset+3]))
		offset += 3
		elements += count
		if count == 0 || elements > maxProgressiveHeaderElements {
			return invalid()
		}
		for index := 0; index < count; index++ {
			if len(data)-offset < 2 {
				return invalid()
			}
			size := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if size < 3 || size > len(data)-offset || data[offset]&0x80 != 0 ||
				(data[offset]>>1)&63 != kind || data[offset+1]&7 == 0 {
				return invalid()
			}
			offset += size
		}
	}
	if offset != len(data) || !seen[32] || !seen[33] || !seen[34] {
		return invalid()
	}
	return length, nil
}

// AV1CodecConfigurationRecord and low-overhead OBU framing are specified at
// https://aomediacodec.github.io/av1-isobmff/. Configuration OBUs cannot contain
// picture data; sample bytes must include a frame or a header and tile payload.
func (p *progressiveVideoParser) av1Configuration(data []byte) error {
	if len(data) < 4 || data[0] != 0x81 || data[1]>>5 > 2 || data[3]&0xe0 != 0 ||
		data[3]&0x10 == 0 && data[3]&15 != 0 || data[2]&0x20 != 0 && (data[1]>>5 != 2 || data[2]&0x40 == 0) {
		return invalidProgressive("video AV1 decoder configuration")
	}
	p.av1Profile = data[1] >> 5
	sequence, index := false, 0
	err := p.av1OBUs(data[4:], false, func(kind byte, payload []byte) error {
		switch kind {
		case 1:
			if sequence || index != 0 || len(payload) == 0 || payload[0]>>5 != p.av1Profile {
				return invalidProgressive("video AV1 sequence configuration")
			}
			sequence = true
		case 5, 15:
		default:
			return invalidProgressive("video AV1 picture in configuration")
		}
		index++
		return nil
	})
	p.av1Sequence = sequence
	return err
}

func (p *progressiveVideoParser) av1VideoSample(data []byte) (bool, error) {
	frame, header, tiles := false, false, false
	err := p.av1OBUs(data, true, func(kind byte, payload []byte) error {
		switch kind {
		case 1:
			if len(payload) == 0 || payload[0]>>5 != p.av1Profile {
				return invalidProgressive("video AV1 sample sequence")
			}
			p.av1Sequence = true
		case 3:
			if !p.av1Sequence {
				return invalidProgressive("video AV1 frame header before sequence")
			}
			header = len(payload) > 0
		case 4:
			tiles = tiles || header && len(payload) > 0
		case 6:
			if !p.av1Sequence {
				return invalidProgressive("video AV1 frame before sequence")
			}
			frame = frame || len(payload) > 0
		case 2, 5, 7, 15:
		default:
			return invalidProgressive("video AV1 unsupported OBU")
		}
		return nil
	})
	return err == nil && p.av1Sequence && (frame || header && tiles), err
}

func (p *progressiveVideoParser) av1OBUs(data []byte, allowFinalImplicitSize bool, visit func(byte, []byte) error) error {
	for offset := 0; offset < len(data); {
		if err := p.count(); err != nil {
			return err
		}
		header := data[offset]
		offset++
		if header&0x81 != 0 || header&2 == 0 && !allowFinalImplicitSize {
			return invalidProgressive("video AV1 OBU header")
		}
		if header&4 != 0 {
			if offset >= len(data) || data[offset]&7 != 0 {
				return invalidProgressive("video AV1 OBU extension")
			}
			offset++
		}
		if header&2 == 0 {
			// The final OBU of an ISOBMFF sample may omit its size. The trun
			// sample extent, rather than an untrusted declared length, bounds it.
			return visit((header>>3)&15, data[offset:])
		}
		var size uint64
		complete := false
		for index := 0; index < 8; index++ {
			if offset >= len(data) {
				return invalidProgressive("video AV1 OBU length")
			}
			value := data[offset]
			offset++
			size |= uint64(value&0x7f) << (index * 7)
			if value&0x80 == 0 {
				complete = true
				break
			}
		}
		if !complete || size > uint64(len(data)-offset) {
			return invalidProgressive("video AV1 OBU sample boundary")
		}
		end := offset + int(size)
		if err := visit((header>>3)&15, data[offset:end]); err != nil {
			return err
		}
		offset = end
	}
	return nil
}
