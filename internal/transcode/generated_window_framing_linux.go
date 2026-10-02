//go:build linux

package transcode

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
)

const (
	maxGeneratedFramingMetadata = 1 << 20
	maxGeneratedFramingBoxes    = 1024
	maxGeneratedFramingRanges   = 1024
)

// ValidateGeneratedWindowFraming checks the physical boundaries of one closed
// generated output. It borrows regular files through ReadAt and never closes
// or changes the offsets of the caller's descriptors. MPEG-TS checks complete
// transport packets and adaptation fields, not PES or codec completeness.
// fMP4 checks the initialized video tracks and every moof's sample addressing,
// requiring each complete mdat body to be covered exactly by declared samples.
// No result proves source coverage, timestamp continuity or restart safety.
func ValidateGeneratedWindowFraming(plan Plan, initialization, segment *os.File) error {
	if !GeneratedHLS(plan) {
		return ErrInvalidPlan
	}
	if err := ValidatePlan(plan); err != nil {
		return err
	}
	if segment == nil {
		return ErrInvalidInput
	}
	format := plan.HLS.SegmentType
	if format == "" {
		format = "mpegts"
	}
	if format != "mpegts" && format != "fmp4" || format == "mpegts" && initialization != nil {
		return ErrInvalidPlan
	}
	if format == "fmp4" && (initialization == nil || plan.VideoStreamIndex < 0 || !VideoEncodingSupported(VideoOutputCodec(plan)) ||
		plan.AudioStreamIndex < -1 || plan.AudioStreamIndex == plan.VideoStreamIndex ||
		plan.AudioStreamIndex >= 0 && plan.AudioCodec != "aac" && plan.AudioCodec != "copy") {
		return ErrInvalidPlan
	}
	inputs := []*os.File{segment}
	if initialization != nil {
		inputs = []*os.File{initialization, segment}
	}
	var owned []*os.File
	defer func() {
		for _, file := range owned {
			_ = file.Close()
		}
	}()
	var before []os.FileInfo
	var total int64
	for _, borrowed := range inputs {
		file, err := DuplicateInput(borrowed)
		if err != nil {
			return err
		}
		owned = append(owned, file)
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return ErrInvalidInput
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			return ErrInvalidInput
		}
		if info.Size() > maxGeneratedBoundsInputBytes-total {
			return ErrTimelineLimit
		}
		total += info.Size()
		before = append(before, info)
	}
	var err error
	if format == "mpegts" {
		err = generatedFramingTransport(owned[0], before[0].Size())
	} else {
		err = generatedFramingMP4(plan, owned[0], before[0].Size(), owned[1], before[1].Size())
	}
	if err != nil {
		return err
	}
	for index, file := range owned {
		if !transcodeSourceUnchanged(file, before[index]) {
			return ErrInvalidInput
		}
	}
	return nil
}

func generatedFramingInvalid(reason string) error {
	return fmt.Errorf("%w: generated window framing %s", ErrInvalidTimeline, reason)
}

func generatedFramingTransport(file *os.File, size int64) error {
	if err := validateLiveTransportStream(file, size); err != nil {
		return err
	}
	var buffer [188 * 256]byte
	for offset := int64(0); offset < size; {
		length := min(int64(len(buffer)), size-offset)
		if _, err := file.ReadAt(buffer[:length], offset); err != nil {
			return generatedFramingInvalid("transport read")
		}
		for position := 0; position < int(length); position += 188 {
			packet := buffer[position : position+188]
			control := packet[3] >> 4 & 3
			if control == 1 {
				continue
			}
			adaptation := int(packet[4])
			if control == 2 && adaptation != 183 || control == 3 && adaptation > 182 {
				return generatedFramingInvalid("transport adaptation size")
			}
			if adaptation == 0 {
				continue
			}
			end, cursor, flags := 5+adaptation, 6, packet[5]
			for _, field := range []struct {
				flag byte
				size int
			}{{0x10, 6}, {0x08, 6}, {0x04, 1}} {
				if flags&field.flag != 0 {
					if field.size > end-cursor {
						return generatedFramingInvalid("transport adaptation field")
					}
					cursor += field.size
				}
			}
			for _, flag := range []byte{0x02, 0x01} {
				if flags&flag == 0 {
					continue
				}
				if cursor == end {
					return generatedFramingInvalid("transport adaptation variable length")
				}
				count := int(packet[cursor])
				cursor++
				if count > end-cursor {
					return generatedFramingInvalid("transport adaptation variable field")
				}
				if flag == 0x01 {
					if err := generatedFramingAdaptationExtension(packet[cursor : cursor+count]); err != nil {
						return err
					}
				}
				cursor += count
			}
			for ; cursor < end; cursor++ {
				if packet[cursor] != 0xff {
					return generatedFramingInvalid("transport adaptation stuffing")
				}
			}
		}
		offset += length
	}
	return nil
}

func generatedFramingAdaptationExtension(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	cursor := 1
	for _, field := range []struct {
		flag byte
		size int
	}{{0x80, 2}, {0x40, 3}, {0x20, 5}} {
		if data[0]&field.flag != 0 {
			if field.size > len(data)-cursor {
				return generatedFramingInvalid("transport adaptation extension field")
			}
			cursor += field.size
		}
	}
	for ; cursor < len(data); cursor++ {
		if data[cursor] != 0xff {
			return generatedFramingInvalid("transport adaptation extension stuffing")
		}
	}
	return nil
}

type generatedFramingBox struct {
	kind        string
	start, body int64
	end         int64
}

func generatedFramingReadBox(file *os.File, size, offset int64) (generatedFramingBox, error) {
	var box generatedFramingBox
	if offset < 0 || offset > size || size-offset < 8 {
		return box, generatedFramingInvalid("partial box header")
	}
	var header [16]byte
	if _, err := file.ReadAt(header[:8], offset); err != nil {
		return box, generatedFramingInvalid("box header read")
	}
	length, headerSize := uint64(binary.BigEndian.Uint32(header[:4])), int64(8)
	if length == 1 {
		if size-offset < 16 {
			return box, generatedFramingInvalid("partial extended box header")
		}
		if _, err := file.ReadAt(header[8:], offset+8); err != nil {
			return box, generatedFramingInvalid("extended box header read")
		}
		length, headerSize = binary.BigEndian.Uint64(header[8:]), 16
	}
	// Zero-size boxes would consume an unspecified tail. No complete-window
	// proof accepts them, even where a general MP4 reader permits that form.
	if length < uint64(headerSize) || length > uint64(size-offset) {
		return box, generatedFramingInvalid("box length")
	}
	return generatedFramingBox{kind: string(header[4:8]), start: offset, body: offset + headerSize, end: offset + int64(length)}, nil
}

func generatedFramingMetadata(file *os.File, box generatedFramingBox) ([]byte, error) {
	length := box.end - box.body
	if length < 0 || length > maxGeneratedFramingMetadata {
		return nil, ErrTimelineLimit
	}
	data := make([]byte, int(length))
	if _, err := file.ReadAt(data, box.body); err != nil {
		return nil, generatedFramingInvalid("metadata read")
	}
	return data, nil
}

type generatedFramingRange struct{ start, end int64 }

type generatedFramingMP4State struct {
	parser    progressiveVideoParser
	boxes     int
	fragments [maxGeneratedFramingRanges]generatedFramingRange
	fragmentN int
	indexes   [maxGeneratedFramingRanges]generatedFramingRange
	indexN    int
}

func (s *generatedFramingMP4State) count() error {
	s.boxes++
	if s.boxes > maxGeneratedFramingBoxes {
		return ErrTimelineLimit
	}
	return nil
}

func generatedFramingMP4(plan Plan, initialization *os.File, initSize int64, segment *os.File, mediaSize int64) error {
	state := generatedFramingMP4State{parser: progressiveVideoParser{
		tracks: make(map[uint32]progressiveVideoTrack), wantAudio: plan.AudioStreamIndex >= 0, wantCodec: VideoOutputCodec(plan),
	}}
	haveType, haveMovie := false, false
	for offset := int64(0); offset < initSize; {
		box, err := generatedFramingReadBox(initialization, initSize, offset)
		if err != nil {
			return err
		}
		if err := state.count(); err != nil {
			return err
		}
		switch box.kind {
		case "ftyp":
			if offset != 0 || haveType || haveMovie {
				return generatedFramingInvalid("initialization type order")
			}
			data, err := generatedFramingMetadata(initialization, box)
			if err != nil {
				return err
			}
			if len(data) < 8 || len(data)%4 != 0 {
				return generatedFramingInvalid("initialization type size")
			}
			haveType = true
		case "moov":
			if !haveType || haveMovie {
				return generatedFramingInvalid("initialization movie order")
			}
			data, err := generatedFramingMetadata(initialization, box)
			if err != nil {
				return err
			}
			if err := state.parser.movie(data); err != nil {
				return generatedFramingInvalid("initialized track metadata")
			}
			haveMovie = true
		case "free":
			if !haveType {
				return generatedFramingInvalid("initialization padding")
			}
			if box.end-box.body > maxGeneratedFramingMetadata {
				return ErrTimelineLimit
			}
		default:
			return generatedFramingInvalid("unexpected initialization box")
		}
		offset = box.end
	}
	if !haveType || !haveMovie {
		return generatedFramingInvalid("missing initialization")
	}
	var pending *progressiveVideoFragment
	var pendingStart int64
	haveSegmentType := false
	for offset := int64(0); offset < mediaSize; {
		box, err := generatedFramingReadBox(segment, mediaSize, offset)
		if err != nil {
			return err
		}
		if err := state.count(); err != nil {
			return err
		}
		switch box.kind {
		case "styp":
			if offset != 0 || haveSegmentType || pending != nil || state.fragmentN != 0 {
				return generatedFramingInvalid("segment type order")
			}
			data, err := generatedFramingMetadata(segment, box)
			if err != nil {
				return err
			}
			if len(data) < 8 || len(data)%4 != 0 {
				return generatedFramingInvalid("segment type size")
			}
			haveSegmentType = true
		case "sidx":
			if pending != nil {
				return generatedFramingInvalid("index splits a fragment")
			}
			data, err := generatedFramingMetadata(segment, box)
			if err != nil {
				return err
			}
			if err := state.index(data, box, mediaSize); err != nil {
				return err
			}
		case "moof":
			if pending != nil {
				return generatedFramingInvalid("unmatched movie fragment")
			}
			data, err := generatedFramingMetadata(segment, box)
			if err != nil {
				return err
			}
			if err := generatedFramingFragmentChildren(data); err != nil {
				return err
			}
			pending, err = state.parser.fragment(progressiveVideoBox{kind: "moof", start: uint64(box.start), body: uint64(box.body), end: uint64(box.end), payload: data})
			if err != nil {
				return generatedFramingInvalid("movie fragment sample metadata")
			}
			pendingStart = box.start
		case "mdat":
			if pending == nil || box.end == box.body || len(pending.extents) == 0 {
				return generatedFramingInvalid("unmatched or empty media data")
			}
			cursor := uint64(box.body)
			for _, extent := range pending.extents {
				if extent.start != cursor || extent.end <= extent.start || extent.end > uint64(box.end) {
					return generatedFramingInvalid("media data sample coverage")
				}
				cursor = extent.end
			}
			if cursor != uint64(box.end) {
				return generatedFramingInvalid("unreferenced media bytes")
			}
			if state.fragmentN == len(state.fragments) {
				return ErrTimelineLimit
			}
			state.fragments[state.fragmentN] = generatedFramingRange{start: pendingStart, end: box.end}
			state.fragmentN++
			pending = nil
		default:
			return generatedFramingInvalid("unexpected media box")
		}
		offset = box.end
	}
	if pending != nil || state.fragmentN == 0 {
		return generatedFramingInvalid("incomplete media fragment")
	}
	for index := 0; index < state.indexN; index++ {
		reference := state.indexes[index]
		cursor := reference.start
		for fragment := 0; fragment < state.fragmentN; fragment++ {
			span := state.fragments[fragment]
			if span.start == cursor && span.end <= reference.end {
				cursor = span.end
			}
		}
		if cursor != reference.end {
			return generatedFramingInvalid("index does not describe complete fragments")
		}
	}
	return nil
}

// Keep the reused startup parser's sample-addressing rules, while rejecting
// fragment metadata that it would intentionally skip as a prefix detector.
func generatedFramingFragmentChildren(data []byte) error {
	for offset := 0; offset < len(data); {
		box, complete, err := progressiveVideoReadBox(data, offset, false)
		if err != nil || !complete || box.kind != "mfhd" && box.kind != "traf" {
			return generatedFramingInvalid("unknown fragment metadata")
		}
		if box.kind == "traf" {
			for childOffset := 0; childOffset < len(box.payload); {
				child, complete, err := progressiveVideoReadBox(box.payload, childOffset, false)
				if err != nil || !complete || child.kind != "tfhd" && child.kind != "tfdt" && child.kind != "trun" {
					return generatedFramingInvalid("unknown track fragment metadata")
				}
				childOffset = int(child.end)
			}
		}
		offset = int(box.end)
	}
	return nil
}

func (s *generatedFramingMP4State) index(data []byte, box generatedFramingBox, mediaSize int64) error {
	if len(data) < 24 || data[0] > 1 || binary.BigEndian.Uint32(data[:4])&0xffffff != 0 || binary.BigEndian.Uint32(data[8:12]) == 0 {
		return generatedFramingInvalid("index header")
	}
	if _, exists := s.parser.tracks[binary.BigEndian.Uint32(data[4:8])]; !exists {
		return generatedFramingInvalid("index track identity")
	}
	firstOffset, cursor := uint64(0), 20
	if data[0] == 0 {
		firstOffset = uint64(binary.BigEndian.Uint32(data[16:20]))
	} else {
		if len(data) < 32 {
			return generatedFramingInvalid("extended index header")
		}
		firstOffset, cursor = binary.BigEndian.Uint64(data[20:28]), 28
	}
	if binary.BigEndian.Uint16(data[cursor:cursor+2]) != 0 || firstOffset > uint64(mediaSize-box.end) {
		return generatedFramingInvalid("index offset or reserved field")
	}
	count := int(binary.BigEndian.Uint16(data[cursor+2 : cursor+4]))
	cursor += 4
	if count == 0 || count > maxGeneratedFramingRanges-s.indexN || count*12 != len(data)-cursor {
		return generatedFramingInvalid("index reference count")
	}
	start := box.end + int64(firstOffset)
	for entry := 0; entry < count; entry++ {
		reference := binary.BigEndian.Uint32(data[cursor : cursor+4])
		length := int64(reference & 0x7fffffff)
		if reference&0x80000000 != 0 || length == 0 || length > mediaSize-start || binary.BigEndian.Uint32(data[cursor+4:cursor+8]) == 0 {
			return generatedFramingInvalid("index reference extent")
		}
		s.indexes[s.indexN] = generatedFramingRange{start: start, end: start + length}
		s.indexN++
		start += length
		cursor += 12
	}
	return nil
}
