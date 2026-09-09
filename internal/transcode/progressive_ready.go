package transcode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// MaxProgressivePrefixBytes bounds both the caller's prefix read and the amount
// of container metadata accepted before the first audio payload.
const MaxProgressivePrefixBytes = 4 * 1024 * 1024

const maxProgressiveHeaderElements = 4096

var ErrInvalidProgressiveStream = errors.New("invalid progressive audio stream")

// ProgressiveAudioReady inspects an append-only prefix produced by the bounded
// FFmpeg runner. False with no error means more bytes are needed. True means a
// complete first MPEG/ADTS frame, Ogg page, fragmented MP4 media box, or a native
// FLAC/WAV audio payload is present; it does not certify decoding or the rest of
// the stream. The function neither scans past junk nor accepts arbitrary media
// input as an alternative to source authorization and format negotiation.
func ProgressiveAudioReady(container string, prefix []byte) (bool, error) {
	if len(prefix) > MaxProgressivePrefixBytes {
		return false, invalidProgressive("prefix limit")
	}
	var ready bool
	var err error
	switch container {
	case "mp3":
		ready, err = progressiveMP3Ready(prefix)
	case "aac":
		ready, err = progressiveADTSReady(prefix)
	case "flac":
		ready, err = progressiveFLACReady(prefix)
	case "ogg":
		ready, err = progressiveOggReady(prefix)
	case "wav":
		ready, err = progressiveWAVReady(prefix)
	case "m4a":
		ready, err = progressiveM4AReady(prefix)
	default:
		err = invalidProgressive("container")
	}
	if !ready && err == nil && len(prefix) == MaxProgressivePrefixBytes {
		err = invalidProgressive("metadata exhausts prefix limit")
	}
	return ready, err
}

func invalidProgressive(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidProgressiveStream, reason)
}

// progressiveMagic rejects a mismatching signature even when the prefix has
// not yet grown to the signature's full length.
func progressiveMagic(data []byte, magic string) (bool, error) {
	n := min(len(data), len(magic))
	if !bytes.Equal(data[:n], []byte(magic)[:n]) {
		return false, invalidProgressive("signature")
	}
	return len(data) >= len(magic), nil
}

func progressiveMP3Ready(data []byte) (bool, error) {
	offset := 0
	if len(data) > 0 && data[0] == 'I' {
		complete, err := progressiveMagic(data, "ID3")
		if err != nil || !complete {
			return false, err
		}
		if len(data) < 10 {
			return false, nil
		}
		version, flags := data[3], data[5]
		if version < 2 || version > 4 || data[4] == 255 ||
			(version == 2 && flags&0x3f != 0) || (version == 3 && flags&0x1f != 0) ||
			(version == 4 && flags&0x0f != 0) {
			return false, invalidProgressive("ID3 version or flags")
		}
		size := 0
		for _, value := range data[6:10] {
			if value&0x80 != 0 {
				return false, invalidProgressive("ID3 size")
			}
			size = size<<7 | int(value)
		}
		offset = 10 + size
		footer := version == 4 && flags&0x10 != 0
		if footer {
			offset += 10
		}
		if offset > MaxProgressivePrefixBytes-4 {
			return false, invalidProgressive("ID3 limit")
		}
		if len(data) < offset {
			return false, nil
		}
		if footer && (!bytes.Equal(data[offset-10:offset-7], []byte("3DI")) ||
			!bytes.Equal(data[offset-7:offset], data[3:10])) {
			return false, invalidProgressive("ID3 footer")
		}
	}
	frame := data[offset:]
	if len(frame) < 4 {
		if len(frame) > 0 && frame[0] != 0xff {
			return false, invalidProgressive("MPEG audio sync")
		}
		return false, nil
	}
	version, layer := int(frame[1]>>3&3), int(frame[1]>>1&3)
	bitrateIndex, rateIndex := int(frame[2]>>4), int(frame[2]>>2&3)
	if frame[0] != 0xff || frame[1]&0xe0 != 0xe0 || version == 1 || layer == 0 ||
		bitrateIndex == 0 || bitrateIndex == 15 || rateIndex == 3 || frame[3]&3 == 2 {
		return false, invalidProgressive("MPEG audio header")
	}
	// The MPEG header encodes Layer III as 1 and Layer I as 3.
	bitrates := [5][14]int{
		{32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
		{32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
		{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
		{32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
		{8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	}
	table := 3 - layer
	if version != 3 {
		table = 4
		if layer == 3 {
			table = 3
		}
	}
	rate := [3]int{44100, 48000, 32000}[rateIndex]
	if version == 2 {
		rate /= 2
	} else if version == 0 {
		rate /= 4
	}
	bitrate, padding := bitrates[table][bitrateIndex-1]*1000, int(frame[2]>>1&1)
	size := 144*bitrate/rate + padding
	if layer == 3 {
		size = (12*bitrate/rate + padding) * 4
	} else if layer == 1 && version != 3 {
		size = 72*bitrate/rate + padding
	}
	headerSize := 4
	if frame[1]&1 == 0 {
		headerSize += 2
	}
	if size <= headerSize || size > MaxProgressivePrefixBytes-offset {
		return false, invalidProgressive("MPEG audio frame size")
	}
	return len(frame) >= size, nil
}

func progressiveADTSReady(data []byte) (bool, error) {
	if len(data) > 0 && data[0] != 0xff {
		return false, invalidProgressive("ADTS sync")
	}
	if len(data) > 1 && data[1]&0xf6 != 0xf0 {
		return false, invalidProgressive("ADTS header")
	}
	if len(data) < 7 {
		return false, nil
	}
	if data[2]>>2&15 > 12 {
		return false, invalidProgressive("ADTS sample rate")
	}
	headerSize := 7
	if data[1]&1 == 0 {
		headerSize += 2
		// Multiple protected raw blocks also carry a position for each block
		// after the first. Their trailing checksums belong to the frame body.
		headerSize += int(data[6]&3) * 2
	}
	size := int(data[3]&3)<<11 | int(data[4])<<3 | int(data[5]>>5)
	if size <= headerSize {
		return false, invalidProgressive("ADTS frame size")
	}
	return len(data) >= size, nil
}

func progressiveFLACReady(data []byte) (bool, error) {
	complete, err := progressiveMagic(data, "fLaC")
	if err != nil || !complete {
		return false, err
	}
	offset := 4
	for block := 0; block < maxProgressiveHeaderElements; block++ {
		if len(data)-offset < 4 {
			return false, nil
		}
		kind := data[offset] & 0x7f
		last := data[offset]&0x80 != 0
		size := int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
		if kind > 6 || (block == 0 && (kind != 0 || size != 34)) || (block > 0 && kind == 0) ||
			(kind == 2 && size < 4) || (kind == 3 && size%18 != 0) ||
			(kind == 4 && size < 8) || (kind == 5 && size < 396) || (kind == 6 && size < 32) {
			return false, invalidProgressive("FLAC metadata block")
		}
		offset += 4
		if size > MaxProgressivePrefixBytes-offset {
			return false, invalidProgressive("FLAC metadata limit")
		}
		if len(data)-offset < size {
			return false, nil
		}
		if block == 0 {
			info := data[offset : offset+size]
			minBlock, maxBlock := binary.BigEndian.Uint16(info[:2]), binary.BigEndian.Uint16(info[2:4])
			rate := uint32(info[10])<<12 | uint32(info[11])<<4 | uint32(info[12]>>4)
			bits := (info[12]&1)<<4 | info[13]>>4
			if minBlock < 16 || maxBlock < minBlock || rate == 0 || bits+1 < 4 {
				return false, invalidProgressive("FLAC STREAMINFO")
			}
		}
		offset += size
		if last {
			return progressiveFLACFrameReady(data[offset:])
		}
	}
	return false, invalidProgressive("FLAC metadata count")
}

func progressiveFLACFrameReady(data []byte) (bool, error) {
	if len(data) > 0 && data[0] != 0xff {
		return false, invalidProgressive("FLAC frame sync")
	}
	if len(data) > 1 && data[1]&0xfe != 0xf8 {
		return false, invalidProgressive("FLAC frame sync")
	}
	if len(data) < 5 {
		return false, nil
	}
	block, rate := data[2]>>4, data[2]&15
	if block == 0 || rate == 15 || data[3]>>4 > 10 || data[3]&1 != 0 || data[3]>>1&7 == 3 {
		return false, invalidProgressive("FLAC frame header")
	}
	first := data[4]
	numberBytes := 1
	number := uint64(first)
	if first&0x80 != 0 {
		numberBytes = 0
		for mask := byte(0x80); first&mask != 0 && mask != 0; mask >>= 1 {
			numberBytes++
		}
		if numberBytes < 2 || numberBytes > 7 {
			return false, invalidProgressive("FLAC frame number")
		}
		number = uint64(first & byte(0x7f>>numberBytes))
	}
	if len(data) < 4+numberBytes {
		return false, nil
	}
	for _, value := range data[5 : 4+numberBytes] {
		if value&0xc0 != 0x80 {
			return false, invalidProgressive("FLAC frame number continuation")
		}
		number = number<<6 | uint64(value&0x3f)
	}
	minimum := [8]uint64{0, 0, 1 << 7, 1 << 11, 1 << 16, 1 << 21, 1 << 26, 1 << 31}
	if number < minimum[numberBytes] || (data[1]&1 == 0 && number >= 1<<31) {
		return false, invalidProgressive("FLAC frame number range")
	}
	offset := 4 + numberBytes
	if block == 6 {
		offset++
	} else if block == 7 {
		offset += 2
	}
	rateOffset := offset
	if rate == 12 {
		offset++
	} else if rate == 13 || rate == 14 {
		offset += 2
	}
	if len(data) <= offset {
		return false, nil
	}
	if (rate == 12 && data[rateOffset] == 0) ||
		((rate == 13 || rate == 14) && binary.BigEndian.Uint16(data[rateOffset:offset]) == 0) {
		return false, invalidProgressive("FLAC frame sample rate")
	}
	if progressiveFLACHeaderCRC(data[:offset]) != data[offset] {
		return false, invalidProgressive("FLAC frame header checksum")
	}
	offset++
	if len(data) <= offset {
		return false, nil
	}
	subframe := data[offset] >> 1 & 0x3f
	if data[offset]&0x80 != 0 || (subframe > 1 && subframe < 8) || (subframe > 12 && subframe < 32) {
		return false, invalidProgressive("FLAC subframe header")
	}
	// Native FLAC has no byte length in its frame header. Requiring subframe
	// bytes is deliberately weaker than proving a full frame or decoding it.
	return len(data)-offset >= 3, nil
}

func progressiveFLACHeaderCRC(data []byte) byte {
	var crc byte
	for _, value := range data {
		crc ^= value
		for bit := 0; bit < 8; bit++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func progressiveOggReady(data []byte) (bool, error) {
	offset := 0
	var serial uint32
	var continued, ended bool
	for page := 0; page < maxProgressiveHeaderElements; page++ {
		complete, err := progressiveMagic(data[offset:], "OggS")
		if err != nil || !complete {
			return false, err
		}
		if ended {
			return false, invalidProgressive("Ogg bytes after end of stream")
		}
		if len(data)-offset < 27 {
			return false, nil
		}
		header := data[offset : offset+27]
		flags := header[5]
		currentSerial := binary.LittleEndian.Uint32(header[14:18])
		sequence := binary.LittleEndian.Uint32(header[18:22])
		if header[4] != 0 || flags&^byte(7) != 0 || sequence != uint32(page) ||
			(flags&1 != 0) != continued || (page == 0 && flags&2 == 0) || (page > 0 && flags&2 != 0) ||
			(page > 0 && currentSerial != serial) {
			return false, invalidProgressive("Ogg page header")
		}
		serial = currentSerial
		segments := int(header[26])
		if len(data)-offset-27 < segments {
			return false, nil
		}
		size, hasPacket := 0, false
		for _, lace := range data[offset+27 : offset+27+segments] {
			size += int(lace)
			hasPacket = hasPacket || lace < 255
		}
		end := offset + 27 + segments + size
		if end > MaxProgressivePrefixBytes {
			return false, invalidProgressive("Ogg page limit")
		}
		if len(data) < end {
			return false, nil
		}
		granule := int64(binary.LittleEndian.Uint64(header[6:14]))
		if granule < -1 || (granule >= 0 && !hasPacket && segments > 0) {
			return false, invalidProgressive("Ogg granule position")
		}
		if granule > 0 && size > 0 && hasPacket {
			return true, nil
		}
		if segments > 0 {
			continued = data[offset+27+segments-1] == 255
		}
		ended = flags&4 != 0
		offset = end
	}
	return false, invalidProgressive("Ogg page count")
}

func progressiveWAVReady(data []byte) (bool, error) {
	complete, err := progressiveMagic(data, "RIFF")
	if err != nil || !complete {
		return false, err
	}
	if len(data) < 12 {
		return false, nil
	}
	if !bytes.Equal(data[8:12], []byte("WAVE")) {
		return false, invalidProgressive("WAVE signature")
	}
	riffSize := uint64(binary.LittleEndian.Uint32(data[4:8]))
	unknownRIFF := riffSize == 0xffffffff
	if !unknownRIFF && riffSize < 4 {
		return false, invalidProgressive("RIFF size")
	}
	riffEnd := riffSize + 8
	offset, alignment := 12, 0
	for chunk := 0; chunk < maxProgressiveHeaderElements; chunk++ {
		if !unknownRIFF && uint64(offset) == riffEnd {
			return false, nil
		}
		if !unknownRIFF && uint64(offset)+8 > riffEnd {
			return false, invalidProgressive("RIFF chunk header size")
		}
		if len(data)-offset < 8 {
			return false, nil
		}
		kind := string(data[offset : offset+4])
		size := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if size == 0xffffffff && kind != "data" {
			return false, invalidProgressive("WAVE unknown metadata size")
		}
		if size != 0xffffffff && !unknownRIFF && uint64(offset)+size+(size&1) > riffEnd {
			return false, invalidProgressive("WAVE chunk exceeds RIFF size")
		}
		if kind == "data" {
			if alignment == 0 {
				return false, invalidProgressive("WAVE data before format")
			}
			if size == 0xffffffff || size > 0 {
				if size != 0xffffffff && size%uint64(alignment) != 0 {
					return false, invalidProgressive("WAVE partial declared sample")
				}
				if !unknownRIFF && uint64(offset+alignment) > riffEnd {
					return false, invalidProgressive("WAVE sample exceeds RIFF size")
				}
				return len(data)-offset >= alignment, nil
			}
		}
		paddedSize := size + (size & 1)
		if paddedSize > uint64(MaxProgressivePrefixBytes-offset) {
			return false, invalidProgressive("WAVE metadata limit")
		}
		if uint64(len(data)-offset) < paddedSize {
			return false, nil
		}
		if kind == "fmt " {
			if alignment != 0 {
				return false, invalidProgressive("duplicate WAVE format")
			}
			alignment, err = progressiveWAVFormat(data[offset : offset+int(size)])
			if err != nil {
				return false, err
			}
		}
		offset += int(paddedSize)
	}
	return false, invalidProgressive("WAVE chunk count")
}

func progressiveWAVFormat(data []byte) (int, error) {
	if len(data) < 16 || len(data) == 17 {
		return 0, invalidProgressive("WAVE format size")
	}
	tag := binary.LittleEndian.Uint16(data[:2])
	channels := uint64(binary.LittleEndian.Uint16(data[2:4]))
	rate := uint64(binary.LittleEndian.Uint32(data[4:8]))
	byteRate := uint64(binary.LittleEndian.Uint32(data[8:12]))
	alignment := uint64(binary.LittleEndian.Uint16(data[12:14]))
	bits := binary.LittleEndian.Uint16(data[14:16])
	if channels == 0 || channels > 64 || rate == 0 || bits != 16 || alignment != channels*2 || byteRate != rate*alignment {
		return 0, invalidProgressive("WAVE PCM16 format")
	}
	extra := 0
	if len(data) >= 18 {
		extra = int(binary.LittleEndian.Uint16(data[16:18]))
		if extra > len(data)-18 {
			return 0, invalidProgressive("WAVE format extension size")
		}
	}
	pcmGUID := []byte{1, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71}
	if (tag == 1 && extra != 0) || (tag != 1 && tag != 0xfffe) ||
		(tag == 0xfffe && (extra < 22 || len(data) < 40 || binary.LittleEndian.Uint16(data[18:20]) != 16 || !bytes.Equal(data[24:40], pcmGUID))) {
		return 0, invalidProgressive("WAVE PCM16 encoding")
	}
	return int(alignment), nil
}

type progressiveBox struct {
	kind    string
	payload []byte
	next    int
}

func progressiveReadBox(data []byte, offset int) (progressiveBox, bool, error) {
	if len(data)-offset < 8 {
		return progressiveBox{}, false, nil
	}
	size := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
	header := 8
	if size == 1 {
		if len(data)-offset < 16 {
			return progressiveBox{}, false, nil
		}
		size = binary.BigEndian.Uint64(data[offset+8 : offset+16])
		header = 16
	}
	// A zero size extends to EOF, which cannot establish completion in an
	// append-only prefix. The fragmented runner always emits explicit sizes.
	if size < uint64(header) || size > uint64(MaxProgressivePrefixBytes-offset) {
		return progressiveBox{}, false, invalidProgressive("MP4 box size")
	}
	if size > uint64(len(data)-offset) {
		return progressiveBox{}, false, nil
	}
	end := offset + int(size)
	return progressiveBox{kind: string(data[offset+4 : offset+8]), payload: data[offset+header : end], next: end}, true, nil
}

type progressiveMP4Facts struct {
	boxes         int
	audio         bool
	track         bool
	extends       bool
	fragment      bool
	trackFragment bool
	samples       uint64
}

func progressiveM4AReady(data []byte) (bool, error) {
	offset := 0
	var haveType, haveMovie, haveFragment bool
	facts := progressiveMP4Facts{}
	for offset < len(data) {
		box, complete, err := progressiveReadBox(data, offset)
		if err != nil || !complete {
			return false, err
		}
		facts.boxes++
		if facts.boxes > maxProgressiveHeaderElements {
			return false, invalidProgressive("MP4 box count")
		}
		switch box.kind {
		case "ftyp":
			if haveType || haveMovie || len(box.payload) < 8 || len(box.payload)%4 != 0 {
				return false, invalidProgressive("MP4 file type box")
			}
			haveType = true
		case "moov":
			if !haveType || haveMovie || haveFragment {
				return false, invalidProgressive("MP4 movie box order")
			}
			if err := progressiveMP4Children(box.payload, "moov", &facts, 0); err != nil {
				return false, err
			}
			if !facts.track || !facts.audio || !facts.extends {
				return false, invalidProgressive("MP4 fragmented audio initialization")
			}
			haveMovie = true
		case "moof":
			if !haveMovie || haveFragment {
				return false, invalidProgressive("MP4 fragment box order")
			}
			if err := progressiveMP4Children(box.payload, "moof", &facts, 0); err != nil {
				return false, err
			}
			if !facts.fragment || !facts.trackFragment || facts.samples == 0 {
				return false, invalidProgressive("MP4 empty fragment")
			}
			haveFragment = true
		case "mdat":
			if !haveFragment || len(box.payload) == 0 || facts.samples > uint64(len(box.payload)) {
				return false, invalidProgressive("MP4 media before nonempty fragment")
			}
			return true, nil
		case "free", "skip", "wide", "sidx", "styp", "prft":
			// These boxes do not establish that any audio has been written.
		default:
			return false, invalidProgressive("MP4 unexpected top-level box")
		}
		offset = box.next
	}
	return false, nil
}

func progressiveMP4Children(data []byte, parent string, facts *progressiveMP4Facts, depth int) error {
	if depth > 8 || len(data) == 0 {
		return invalidProgressive("MP4 container depth or size")
	}
	haveTrackHeader, firstSample := false, facts.samples
	for offset := 0; offset < len(data); {
		box, complete, err := progressiveReadBox(data, offset)
		if err != nil {
			return err
		}
		if !complete {
			return invalidProgressive("MP4 child exceeds parent box")
		}
		facts.boxes++
		if facts.boxes > maxProgressiveHeaderElements {
			return invalidProgressive("MP4 box count")
		}
		switch box.kind {
		case "trak", "mdia", "minf", "stbl", "mvex", "traf":
			expectedParent := ""
			switch box.kind {
			case "trak", "mvex":
				expectedParent = "moov"
			case "mdia":
				expectedParent = "trak"
			case "minf":
				expectedParent = "mdia"
			case "stbl":
				expectedParent = "minf"
			case "traf":
				expectedParent = "moof"
			}
			if parent != expectedParent {
				return invalidProgressive("MP4 child box placement")
			}
			if box.kind == "trak" {
				if facts.track {
					return invalidProgressive("MP4 requires one audio track")
				}
				facts.track = true
			}
			if box.kind == "mvex" {
				if facts.extends {
					return invalidProgressive("duplicate MP4 movie extension")
				}
				facts.extends = true
			}
			if err := progressiveMP4Children(box.payload, box.kind, facts, depth+1); err != nil {
				return err
			}
		case "hdlr":
			if parent == "mdia" {
				if len(box.payload) < 24 || box.payload[0] != 0 {
					return invalidProgressive("MP4 handler box")
				}
				facts.audio = facts.audio || bytes.Equal(box.payload[8:12], []byte("soun"))
			}
		case "mfhd":
			if parent != "moof" || facts.fragment || len(box.payload) != 8 || binary.BigEndian.Uint32(box.payload[:4]) != 0 {
				return invalidProgressive("MP4 fragment header")
			}
			facts.fragment = true
		case "tfhd":
			if parent != "traf" || haveTrackHeader || len(box.payload) < 8 || box.payload[0] != 0 || binary.BigEndian.Uint32(box.payload[4:8]) == 0 {
				return invalidProgressive("MP4 track fragment header")
			}
			flags := binary.BigEndian.Uint32(box.payload[:4])
			if flags & ^uint32(0x03003b) != 0 {
				return invalidProgressive("MP4 track fragment flags")
			}
			expected := 8
			if flags&1 != 0 {
				expected += 8
			}
			for _, flag := range []uint32{2, 8, 16, 32} {
				if flags&flag != 0 {
					expected += 4
				}
			}
			if len(box.payload) != expected {
				return invalidProgressive("MP4 track fragment fields")
			}
			haveTrackHeader = true
		case "trun":
			if parent != "traf" || !haveTrackHeader || len(box.payload) < 8 || box.payload[0] > 1 {
				return invalidProgressive("MP4 sample run")
			}
			flags := binary.BigEndian.Uint32(box.payload[:4]) & 0xffffff
			if flags & ^uint32(0x000f05) != 0 || flags&4 != 0 && flags&0x400 != 0 {
				return invalidProgressive("MP4 sample run flags")
			}
			count := uint64(binary.BigEndian.Uint32(box.payload[4:8]))
			fields, expected := uint64(0), uint64(8)
			for _, flag := range []uint32{1, 4} {
				if flags&flag != 0 {
					expected += 4
				}
			}
			for _, flag := range []uint32{0x100, 0x200, 0x400, 0x800} {
				if flags&flag != 0 {
					fields++
				}
			}
			expected += count * fields * 4
			if expected != uint64(len(box.payload)) || count > MaxProgressivePrefixBytes || facts.samples+count > MaxProgressivePrefixBytes {
				return invalidProgressive("MP4 sample run size")
			}
			facts.samples += count
		}
		offset = box.next
	}
	if parent == "traf" {
		if !haveTrackHeader || facts.samples == firstSample {
			return invalidProgressive("MP4 empty track fragment")
		}
		facts.trackFragment = true
	}
	return nil
}
