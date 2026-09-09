package transcode

import (
	"bytes"
	"encoding/binary"
	"math"
	"sort"
)

const maxProgressiveVideoSamples = 65536

// ProgressiveMediaReady dispatches without changing the existing audio prefix
// contract. Video initialization and fragment addressing require stronger track
// association checks than the historical audio-only MP4 readiness detector.
func ProgressiveMediaReady(plan Plan, prefix []byte) (bool, error) {
	if plan.Container == "mp4" || plan.VideoCodec != "" {
		return ProgressiveVideoReady(plan, prefix)
	}
	return ProgressiveAudioReady(plan.Container, prefix)
}

// ProgressiveVideoReady recognizes the bounded FFmpeg fragmented-MP4 output:
// one avc1 H.264 track, optionally one initialized mp4a AAC track, self-contained
// data references, and movie-fragment-relative sample addressing. The structural
// rules follow https://www.w3.org/TR/mse-byte-stream-format-isobmff/ and FFmpeg's
// movenc.c. This is a startup detector, not a decoder or a complete file audit.
//
// A complete video sample with a complete length-prefixed VCL NAL must have
// arrived inside the declared mdat and its trun extent. A large mdat need not
// have arrived in full; its bytes are never allocated from its declared size.
// Initialization, moof metadata, and the first usable sample must fit the same
// four-MiB prefix budget. Later fragments are not certified by this result.
//
// Decode times, composition offsets, and edit lists are structurally checked
// but never rewritten or normalized. Selected audio needs valid initialization
// but is allowed to produce its first samples after video becomes readable.
func ProgressiveVideoReady(plan Plan, prefix []byte) (bool, error) {
	if plan.OutputMode != "progressive" || plan.Container != "mp4" ||
		plan.VideoStreamIndex < 0 || plan.VideoStreamIndex > maxStreamIndex ||
		(plan.VideoCodec != "h264" && plan.VideoCodec != "copy") ||
		plan.AudioStreamIndex < -1 || plan.AudioStreamIndex > maxStreamIndex ||
		plan.AudioStreamIndex == plan.VideoStreamIndex ||
		(plan.AudioStreamIndex == -1 && plan.AudioCodec != "") ||
		(plan.AudioStreamIndex >= 0 && plan.AudioCodec != "aac" && plan.AudioCodec != "copy") {
		return false, invalidProgressive("video output selection")
	}
	if len(prefix) > MaxProgressivePrefixBytes {
		return false, invalidProgressive("video prefix limit")
	}
	parser := progressiveVideoParser{tracks: make(map[uint32]progressiveVideoTrack), wantAudio: plan.AudioStreamIndex >= 0}
	ready, err := parser.read(prefix)
	if !ready && err == nil && len(prefix) == MaxProgressivePrefixBytes {
		err = invalidProgressive("video startup exhausts prefix limit")
	}
	return ready, err
}

type progressiveVideoBox struct {
	kind    string
	start   uint64
	body    uint64
	end     uint64
	payload []byte
}

type progressiveVideoTrack struct {
	id          uint32
	video       bool
	nalLength   int
	defaultSize uint32
}

type progressiveVideoSample struct {
	start, end uint64
	nalLength  int
}

type progressiveVideoExtent struct{ start, end uint64 }

type progressiveVideoFragment struct {
	extents []progressiveVideoExtent
	video   []progressiveVideoSample
}

type progressiveVideoParser struct {
	tracks    map[uint32]progressiveVideoTrack
	wantAudio bool
	elements  int
	samples   uint64
}

func (p *progressiveVideoParser) count() error {
	p.elements++
	if p.elements > maxProgressiveHeaderElements {
		return invalidProgressive("video header element limit")
	}
	return nil
}

func progressiveVideoReadBox(data []byte, offset int, partialMedia bool) (progressiveVideoBox, bool, error) {
	if offset < 0 || offset > len(data) {
		return progressiveVideoBox{}, false, invalidProgressive("video box offset")
	}
	if len(data)-offset < 8 {
		return progressiveVideoBox{}, false, nil
	}
	size := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
	header := 8
	if size == 1 {
		if len(data)-offset < 16 {
			return progressiveVideoBox{}, false, nil
		}
		header = 16
		size = binary.BigEndian.Uint64(data[offset+8 : offset+16])
	}
	if size < uint64(header) || size > math.MaxInt64-uint64(offset) {
		return progressiveVideoBox{}, false, invalidProgressive("video box size")
	}
	box := progressiveVideoBox{kind: string(data[offset+4 : offset+8]), start: uint64(offset),
		body: uint64(offset + header), end: uint64(offset) + size}
	if partialMedia && box.kind == "mdat" {
		return box, true, nil
	}
	if box.end > MaxProgressivePrefixBytes {
		return progressiveVideoBox{}, false, invalidProgressive("video metadata prefix limit")
	}
	if box.end > uint64(len(data)) {
		return box, false, nil
	}
	box.payload = data[box.body:box.end]
	return box, true, nil
}

func (p *progressiveVideoParser) children(data []byte) ([]progressiveVideoBox, error) {
	var boxes []progressiveVideoBox
	for offset := 0; offset < len(data); {
		box, complete, err := progressiveVideoReadBox(data, offset, false)
		if err != nil {
			return nil, err
		}
		if !complete {
			return nil, invalidProgressive("video child exceeds parent")
		}
		if err := p.count(); err != nil {
			return nil, err
		}
		boxes = append(boxes, box)
		offset = int(box.end)
	}
	return boxes, nil
}

func progressiveVideoOne(boxes []progressiveVideoBox, name string, required bool) (progressiveVideoBox, error) {
	var result progressiveVideoBox
	for _, box := range boxes {
		if box.kind == name {
			if result.kind != "" {
				return result, invalidProgressive("duplicate video initialization box")
			}
			result = box
		}
	}
	if required && result.kind == "" {
		return result, invalidProgressive("missing video initialization box")
	}
	return result, nil
}

func (p *progressiveVideoParser) read(data []byte) (bool, error) {
	haveType, haveMovie := false, false
	var fragment *progressiveVideoFragment
	for offset := 0; offset < len(data); {
		box, complete, err := progressiveVideoReadBox(data, offset, true)
		if err != nil || !complete {
			return false, err
		}
		if err := p.count(); err != nil {
			return false, err
		}
		switch box.kind {
		case "ftyp":
			if haveType || haveMovie || offset != 0 || len(box.payload) < 8 || len(box.payload)%4 != 0 {
				return false, invalidProgressive("video file type order or size")
			}
			haveType = true
		case "moov":
			if !haveType || haveMovie || fragment != nil {
				return false, invalidProgressive("video initialization order")
			}
			if err := p.movie(box.payload); err != nil {
				return false, err
			}
			haveMovie = true
		case "moof":
			if !haveMovie || fragment != nil {
				return false, invalidProgressive("video fragment order")
			}
			fragment, err = p.fragment(box)
			if err != nil {
				return false, err
			}
		case "mdat":
			if fragment == nil {
				return false, invalidProgressive("video media before fragment")
			}
			// The controlled FFmpeg muxer emits one mdat for each moof. All
			// referenced extents must belong to that declared media body, even
			// if a later audio/video sample has not reached the prefix yet.
			for _, extent := range fragment.extents {
				if extent.start < box.body || extent.end > box.end {
					return false, invalidProgressive("video sample outside declared media")
				}
			}
			for _, sample := range fragment.video {
				if sample.end > MaxProgressivePrefixBytes {
					return false, invalidProgressive("video sample exceeds startup prefix limit")
				}
				if sample.end > uint64(len(data)) {
					return false, nil
				}
				vcl, err := p.videoSample(data[sample.start:sample.end], sample.nalLength)
				if err != nil {
					return false, err
				}
				if vcl {
					return true, nil
				}
			}
			if box.end > uint64(len(data)) {
				return false, nil
			}
			fragment = nil
		case "free", "skip", "wide", "sidx", "styp", "prft", "pdin":
			// Bounded ancillary boxes do not establish playable samples.
		default:
			return false, invalidProgressive("unexpected video top-level box")
		}
		offset = int(box.end)
	}
	return false, nil
}

func progressiveVideoTimedHeader(data []byte, v0Size, v1Size int) (int, error) {
	if len(data) < 4 || data[0] > 1 {
		return 0, invalidProgressive("video timed header version")
	}
	position, size := 12, v0Size
	if data[0] == 1 {
		position, size = 20, v1Size
	}
	if len(data) != size {
		return 0, invalidProgressive("video timed header size")
	}
	return position, nil
}

func (p *progressiveVideoParser) movie(data []byte) error {
	boxes, err := p.children(data)
	if err != nil {
		return err
	}
	header, err := progressiveVideoOne(boxes, "mvhd", true)
	if err != nil {
		return err
	}
	position, err := progressiveVideoTimedHeader(header.payload, 100, 112)
	if err != nil || binary.BigEndian.Uint32(header.payload[:4])&0xffffff != 0 || binary.BigEndian.Uint32(header.payload[position:position+4]) == 0 {
		return invalidProgressive("video movie timescale")
	}
	videoCount, audioCount := 0, 0
	for _, box := range boxes {
		if box.kind != "trak" {
			continue
		}
		track, err := p.track(box.payload)
		if err != nil {
			return err
		}
		if _, exists := p.tracks[track.id]; exists {
			return invalidProgressive("duplicate video movie track ID")
		}
		p.tracks[track.id] = track
		if track.video {
			videoCount++
		} else {
			audioCount++
		}
		if len(p.tracks) > 2 {
			return invalidProgressive("video track count")
		}
	}
	if videoCount != 1 || audioCount != 0 && !p.wantAudio || p.wantAudio && audioCount != 1 {
		return invalidProgressive("video selected track initialization")
	}
	extends, err := progressiveVideoOne(boxes, "mvex", true)
	if err != nil {
		return err
	}
	children, err := p.children(extends.payload)
	if err != nil {
		return err
	}
	seen := make(map[uint32]bool)
	for _, box := range children {
		if box.kind != "trex" {
			continue
		}
		if len(box.payload) != 24 || binary.BigEndian.Uint32(box.payload[:4]) != 0 || binary.BigEndian.Uint32(box.payload[8:12]) != 1 {
			return invalidProgressive("video track extension defaults")
		}
		id := binary.BigEndian.Uint32(box.payload[4:8])
		track, exists := p.tracks[id]
		if !exists || seen[id] {
			return invalidProgressive("video track extension identity")
		}
		seen[id] = true
		track.defaultSize = binary.BigEndian.Uint32(box.payload[16:20])
		p.tracks[id] = track
	}
	if len(seen) != len(p.tracks) {
		return invalidProgressive("missing video track extension")
	}
	return nil
}

func (p *progressiveVideoParser) track(data []byte) (progressiveVideoTrack, error) {
	var track progressiveVideoTrack
	boxes, err := p.children(data)
	if err != nil {
		return track, err
	}
	tkhd, err := progressiveVideoOne(boxes, "tkhd", true)
	if err != nil {
		return track, err
	}
	position, err := progressiveVideoTimedHeader(tkhd.payload, 84, 96)
	if err != nil {
		return track, err
	}
	if binary.BigEndian.Uint32(tkhd.payload[:4])&0x00fffff0 != 0 {
		return track, invalidProgressive("video track header flags")
	}
	track.id = binary.BigEndian.Uint32(tkhd.payload[position : position+4])
	if track.id == 0 {
		return track, invalidProgressive("zero video movie track ID")
	}
	edits, err := progressiveVideoOne(boxes, "edts", false)
	if err != nil {
		return track, err
	}
	if edits.kind != "" {
		if err := p.edits(edits.payload); err != nil {
			return track, err
		}
	}
	mdia, err := progressiveVideoOne(boxes, "mdia", true)
	if err != nil {
		return track, err
	}
	media, err := p.children(mdia.payload)
	if err != nil {
		return track, err
	}
	mdhd, err := progressiveVideoOne(media, "mdhd", true)
	if err != nil {
		return track, err
	}
	position, err = progressiveVideoTimedHeader(mdhd.payload, 24, 36)
	if err != nil || binary.BigEndian.Uint32(mdhd.payload[:4])&0xffffff != 0 || binary.BigEndian.Uint32(mdhd.payload[position:position+4]) == 0 {
		return track, invalidProgressive("video track timescale")
	}
	handler, err := progressiveVideoOne(media, "hdlr", true)
	if err != nil {
		return track, err
	}
	if len(handler.payload) < 24 || binary.BigEndian.Uint32(handler.payload[:4]) != 0 {
		return track, invalidProgressive("video track handler")
	}
	switch string(handler.payload[8:12]) {
	case "vide":
		track.video = true
		if binary.BigEndian.Uint32(tkhd.payload[len(tkhd.payload)-8:len(tkhd.payload)-4]) == 0 ||
			binary.BigEndian.Uint32(tkhd.payload[len(tkhd.payload)-4:]) == 0 {
			return track, invalidProgressive("video display dimensions")
		}
	case "soun":
	default:
		return track, invalidProgressive("unexpected video movie handler")
	}
	minf, err := progressiveVideoOne(media, "minf", true)
	if err != nil {
		return track, err
	}
	information, err := p.children(minf.payload)
	if err != nil {
		return track, err
	}
	dinf, err := progressiveVideoOne(information, "dinf", true)
	if err != nil {
		return track, err
	}
	if err := p.references(dinf.payload); err != nil {
		return track, err
	}
	stbl, err := progressiveVideoOne(information, "stbl", true)
	if err != nil {
		return track, err
	}
	tables, err := p.children(stbl.payload)
	if err != nil {
		return track, err
	}
	for _, kind := range []string{"stts", "stsc"} {
		box, err := progressiveVideoOne(tables, kind, true)
		if err != nil {
			return track, err
		}
		if len(box.payload) != 8 || binary.BigEndian.Uint64(box.payload) != 0 {
			return track, invalidProgressive("nonempty video initialization table")
		}
	}
	stsz, err := progressiveVideoOne(tables, "stsz", true)
	if err != nil {
		return track, err
	}
	if len(stsz.payload) != 12 || binary.BigEndian.Uint32(stsz.payload[:4]) != 0 || binary.BigEndian.Uint32(stsz.payload[8:12]) != 0 {
		return track, invalidProgressive("nonempty video sample size table")
	}
	stco, err := progressiveVideoOne(tables, "stco", false)
	if err != nil {
		return track, err
	}
	co64, err := progressiveVideoOne(tables, "co64", false)
	if err != nil {
		return track, err
	}
	if (stco.kind == "") == (co64.kind == "") {
		return track, invalidProgressive("video chunk offset table")
	}
	if stco.kind == "" {
		stco = co64
	}
	if len(stco.payload) != 8 || binary.BigEndian.Uint64(stco.payload) != 0 {
		return track, invalidProgressive("nonempty video chunk offsets")
	}
	stsd, err := progressiveVideoOne(tables, "stsd", true)
	if err != nil {
		return track, err
	}
	if len(stsd.payload) < 8 || binary.BigEndian.Uint32(stsd.payload[:4]) != 0 || binary.BigEndian.Uint32(stsd.payload[4:8]) != 1 {
		return track, invalidProgressive("video sample description count")
	}
	entries, err := p.children(stsd.payload[8:])
	if err != nil {
		return track, err
	}
	if len(entries) != 1 {
		return track, invalidProgressive("video sample description entries")
	}
	entry := entries[0]
	if len(entry.payload) < 8 || binary.BigEndian.Uint16(entry.payload[6:8]) != 1 {
		return track, invalidProgressive("video external sample reference")
	}
	if track.video {
		if entry.kind != "avc1" || len(entry.payload) < 78 || binary.BigEndian.Uint16(entry.payload[24:26]) == 0 || binary.BigEndian.Uint16(entry.payload[26:28]) == 0 {
			return track, invalidProgressive("video AVC sample entry")
		}
		configuration, err := p.children(entry.payload[78:])
		if err != nil {
			return track, err
		}
		avcc, err := progressiveVideoOne(configuration, "avcC", true)
		if err != nil {
			return track, err
		}
		track.nalLength, err = progressiveVideoAVCC(avcc.payload)
		if err != nil {
			return track, err
		}
	} else {
		if entry.kind != "mp4a" || len(entry.payload) < 28 || binary.BigEndian.Uint16(entry.payload[8:10]) != 0 ||
			binary.BigEndian.Uint16(entry.payload[16:18]) == 0 || binary.BigEndian.Uint32(entry.payload[24:28]) == 0 {
			return track, invalidProgressive("video AAC sample entry")
		}
		configuration, err := p.children(entry.payload[28:])
		if err != nil {
			return track, err
		}
		esds, err := progressiveVideoOne(configuration, "esds", true)
		if err != nil {
			return track, err
		}
		if err := progressiveVideoESDS(esds.payload); err != nil {
			return track, err
		}
	}
	return track, nil
}

func (p *progressiveVideoParser) references(data []byte) error {
	boxes, err := p.children(data)
	if err != nil {
		return err
	}
	dref, err := progressiveVideoOne(boxes, "dref", true)
	if err != nil {
		return err
	}
	if len(dref.payload) < 8 || binary.BigEndian.Uint32(dref.payload[:4]) != 0 || binary.BigEndian.Uint32(dref.payload[4:8]) != 1 {
		return invalidProgressive("video data references")
	}
	entries, err := p.children(dref.payload[8:])
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].kind != "url " || len(entries[0].payload) != 4 || binary.BigEndian.Uint32(entries[0].payload) != 1 {
		return invalidProgressive("video external data reference")
	}
	return nil
}

func (p *progressiveVideoParser) edits(data []byte) error {
	boxes, err := p.children(data)
	if err != nil {
		return err
	}
	if len(boxes) != 1 || boxes[0].kind != "elst" {
		return invalidProgressive("video edit list structure")
	}
	body := boxes[0].payload
	if len(body) < 8 || body[0] > 1 || binary.BigEndian.Uint32(body[:4])&0xffffff != 0 {
		return invalidProgressive("video edit list version")
	}
	count := uint64(binary.BigEndian.Uint32(body[4:8]))
	width := uint64(12)
	if body[0] == 1 {
		width = 20
	}
	if count > maxProgressiveHeaderElements || 8+count*width != uint64(len(body)) {
		return invalidProgressive("video edit list size")
	}
	for offset := 8; offset < len(body); offset += int(width) {
		// media_time is signed (including -1 empty edits); duration zero and
		// negative timeline offsets are preserved without interpretation.
		rate := body[offset+int(width)-4 : offset+int(width)]
		if binary.BigEndian.Uint32(rate) != 0x00010000 {
			return invalidProgressive("video edit list playback rate")
		}
	}
	return nil
}

func progressiveVideoAVCC(data []byte) (int, error) {
	if len(data) < 7 || data[0] != 1 || data[4]&0xfc != 0xfc || data[5]&0xe0 != 0xe0 || data[4]&3 == 2 {
		return 0, invalidProgressive("video AVC decoder configuration")
	}
	nalLength := int(data[4]&3) + 1
	offset := 6
	readSets := func(count int, kind byte) bool {
		if count == 0 {
			return false
		}
		for index := 0; index < count; index++ {
			if len(data)-offset < 2 {
				return false
			}
			size := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if size < 2 || size > len(data)-offset || data[offset]&0x80 != 0 || data[offset]&31 != kind {
				return false
			}
			if kind == 7 && (size < 4 || data[offset+1] != data[1]) {
				return false
			}
			offset += size
		}
		return true
	}
	if !readSets(int(data[5]&31), 7) || offset >= len(data) {
		return 0, invalidProgressive("video AVC sequence parameter sets")
	}
	count := int(data[offset])
	offset++
	if !readSets(count, 8) {
		return 0, invalidProgressive("video AVC picture parameter sets")
	}
	if offset < len(data) {
		if data[1] == 66 || data[1] == 77 || data[1] == 88 || len(data)-offset < 4 ||
			data[offset]&0xfc != 0xfc || data[offset+1]&0xf8 != 0xf8 || data[offset+2]&0xf8 != 0xf8 {
			return 0, invalidProgressive("video AVC configuration extension")
		}
		count = int(data[offset+3])
		offset += 4
		if count > 0 && !readSets(count, 13) {
			return 0, invalidProgressive("video AVC sequence extension")
		}
	}
	if offset != len(data) {
		return 0, invalidProgressive("video AVC trailing configuration")
	}
	return nalLength, nil
}

func progressiveVideoDescriptor(data []byte, offset int) (byte, []byte, int, error) {
	if len(data)-offset < 2 {
		return 0, nil, offset, invalidProgressive("video AAC descriptor header")
	}
	tag := data[offset]
	offset++
	size := 0
	for index := 0; index < 4; index++ {
		if offset >= len(data) {
			break
		}
		value := data[offset]
		offset++
		size = size<<7 | int(value&0x7f)
		if value&0x80 == 0 {
			if size > len(data)-offset {
				return 0, nil, offset, invalidProgressive("video AAC descriptor length")
			}
			return tag, data[offset : offset+size], offset + size, nil
		}
	}
	return 0, nil, offset, invalidProgressive("video AAC descriptor length encoding")
}

func progressiveVideoESDS(data []byte) error {
	if len(data) < 4 || binary.BigEndian.Uint32(data[:4]) != 0 {
		return invalidProgressive("video AAC elementary stream descriptor")
	}
	tag, es, end, err := progressiveVideoDescriptor(data, 4)
	if err != nil {
		return err
	}
	if tag != 3 || end != len(data) || len(es) < 3 || es[2]&0xe0 != 0 {
		return invalidProgressive("video AAC elementary stream reference")
	}
	decoderSeen, slSeen := false, false
	for offset := 3; offset < len(es); {
		tag, body, next, err := progressiveVideoDescriptor(es, offset)
		if err != nil {
			return err
		}
		switch tag {
		case 4:
			if decoderSeen || len(body) < 13 || body[0] != 0x40 || body[1] != 0x15 {
				return invalidProgressive("video AAC decoder declaration")
			}
			decoderSeen = true
			kind, config, finish, err := progressiveVideoDescriptor(body, 13)
			if err != nil {
				return err
			}
			if kind != 5 || finish != len(body) || !progressiveVideoASC(config) {
				return invalidProgressive("video AAC decoder configuration")
			}
		case 6:
			if slSeen || !bytes.Equal(body, []byte{2}) {
				return invalidProgressive("video AAC synchronization configuration")
			}
			slSeen = true
		default:
			return invalidProgressive("video AAC unexpected descriptor")
		}
		offset = next
	}
	if !decoderSeen || !slSeen {
		return invalidProgressive("video AAC missing configuration")
	}
	return nil
}

func progressiveVideoASC(data []byte) bool {
	if len(data) < 2 || len(data) > 65536 {
		return false
	}
	position := 0
	bits := func(count int) (uint32, bool) {
		if count > len(data)*8-position {
			return 0, false
		}
		var value uint32
		for index := 0; index < count; index++ {
			value = value<<1 | uint32(data[position/8]>>uint(7-position%8)&1)
			position++
		}
		return value, true
	}
	object := func() (uint32, bool) {
		value, ok := bits(5)
		if ok && value == 31 {
			extension, complete := bits(6)
			return 32 + extension, complete
		}
		return value, ok
	}
	frequency := func() bool {
		index, ok := bits(4)
		if !ok {
			return false
		}
		if index == 15 {
			value, ok := bits(24)
			return ok && value > 0 && value <= 768000
		}
		return index <= 12
	}
	typeID, ok := object()
	if !ok || !frequency() {
		return false
	}
	channels, ok := bits(4)
	if !ok || channels > 7 {
		return false
	}
	if typeID == 5 || typeID == 29 {
		if !frequency() {
			return false
		}
		typeID, ok = object()
		if !ok {
			return false
		}
		if typeID == 22 {
			extendedChannels, ok := bits(4)
			if !ok || extendedChannels > 7 {
				return false
			}
		}
	}
	switch typeID {
	case 1, 2, 3, 4, 6, 17, 19, 20, 22, 23:
	default:
		return false
	}
	// GASpecificConfig has required flags even for ordinary AAC-LC. A PCE or
	// core-coder delay cannot be inferred from the enclosing descriptor length.
	if _, ok := bits(1); !ok {
		return false
	}
	depends, ok := bits(1)
	if !ok {
		return false
	}
	if depends != 0 {
		if _, ok := bits(14); !ok {
			return false
		}
	}
	extension, ok := bits(1)
	if !ok {
		return false
	}
	if typeID == 6 || typeID == 20 {
		if _, ok := bits(3); !ok {
			return false
		}
	}
	if channels == 0 && !progressiveVideoPCE(bits, &position, typeID) {
		return false
	}
	if extension != 0 {
		if typeID == 22 {
			if _, ok := bits(16); !ok {
				return false
			}
		}
		if typeID == 17 || typeID == 19 || typeID == 20 || typeID == 23 {
			if _, ok := bits(3); !ok {
				return false
			}
		}
		future, ok := bits(1)
		if !ok || future != 0 {
			return false
		}
	}
	if typeID == 17 || typeID == 19 || typeID == 20 || typeID == 23 {
		protection, ok := bits(2)
		if !ok || protection != 0 {
			return false
		}
	}
	// FFmpeg's native LC encoder writes a backward-compatible SBR-absent
	// extension. Copy input can also carry explicit SBR/PS configuration.
	if len(data)*8-position >= 11 {
		before := position
		sync, _ := bits(11)
		if sync == 0x2b7 {
			extensionType, ok := object()
			if !ok || extensionType != 5 && extensionType != 22 {
				return false
			}
			present, ok := bits(1)
			if !ok || present != 0 && !frequency() {
				return false
			}
			if extensionType == 22 {
				extendedChannels, ok := bits(4)
				if !ok || extendedChannels > 7 {
					return false
				}
			}
			if extensionType == 5 && len(data)*8-position >= 12 {
				beforePS := position
				psSync, _ := bits(11)
				if psSync == 0x548 {
					if _, ok := bits(1); !ok {
						return false
					}
				} else {
					position = beforePS
				}
			}
		} else {
			position = before
		}
	}
	for position < len(data)*8 {
		value, _ := bits(1)
		if value != 0 {
			return false
		}
	}
	return true
}

func progressiveVideoPCE(bits func(int) (uint32, bool), position *int, objectType uint32) bool {
	if _, ok := bits(4); !ok {
		return false
	}
	profile, ok := bits(2)
	if !ok || objectType <= 4 && profile+1 != objectType {
		return false
	}
	rate, ok := bits(4)
	if !ok || rate > 12 {
		return false
	}
	var counts [6]uint32
	for index, width := range []int{4, 4, 4, 2, 3, 4} {
		counts[index], ok = bits(width)
		if !ok {
			return false
		}
	}
	for _, width := range []int{4, 4, 3} {
		present, ok := bits(1)
		if !ok {
			return false
		}
		if present != 0 {
			if _, ok := bits(width); !ok {
				return false
			}
		}
	}
	var channels uint32
	for group := 0; group < 3; group++ {
		for index := uint32(0); index < counts[group]; index++ {
			pair, ok := bits(1)
			if !ok {
				return false
			}
			if _, ok := bits(4); !ok {
				return false
			}
			channels += 1 + pair
		}
	}
	for group := 3; group < len(counts); group++ {
		width := 4
		if group == 5 {
			width = 5
		}
		for index := uint32(0); index < counts[group]; index++ {
			if _, ok := bits(width); !ok {
				return false
			}
		}
		if group == 3 {
			channels += counts[group]
		}
	}
	if channels == 0 || channels > 64 {
		return false
	}
	if padding := (8 - *position%8) % 8; padding != 0 {
		if _, ok := bits(padding); !ok {
			return false
		}
	}
	comment, ok := bits(8)
	if !ok {
		return false
	}
	for index := uint32(0); index < comment; index++ {
		if _, ok := bits(8); !ok {
			return false
		}
	}
	return true
}

func (p *progressiveVideoParser) fragment(box progressiveVideoBox) (*progressiveVideoFragment, error) {
	children, err := p.children(box.payload)
	if err != nil {
		return nil, err
	}
	header, err := progressiveVideoOne(children, "mfhd", true)
	if err != nil {
		return nil, err
	}
	if len(header.payload) != 8 || binary.BigEndian.Uint32(header.payload[:4]) != 0 {
		return nil, invalidProgressive("video fragment header")
	}
	var trafs []progressiveVideoBox
	for _, child := range children {
		if child.kind == "traf" {
			trafs = append(trafs, child)
		}
	}
	if len(trafs) == 0 || len(trafs) > len(p.tracks) {
		return nil, invalidProgressive("video track fragment count")
	}
	result := &progressiveVideoFragment{}
	seen := make(map[uint32]bool)
	for _, traf := range trafs {
		if err := p.trackFragment(traf.payload, box, len(trafs) == 1, seen, result); err != nil {
			return nil, err
		}
	}
	if len(result.extents) == 0 {
		return nil, invalidProgressive("empty video movie fragment")
	}
	sort.Slice(result.extents, func(i, j int) bool { return result.extents[i].start < result.extents[j].start })
	for index := 1; index < len(result.extents); index++ {
		if result.extents[index].start < result.extents[index-1].end {
			return nil, invalidProgressive("overlapping video fragment sample extents")
		}
	}
	sort.Slice(result.video, func(i, j int) bool { return result.video[i].start < result.video[j].start })
	return result, nil
}

func (p *progressiveVideoParser) trackFragment(data []byte, moof progressiveVideoBox, single bool, seen map[uint32]bool, result *progressiveVideoFragment) error {
	boxes, err := p.children(data)
	if err != nil {
		return err
	}
	header, err := progressiveVideoOne(boxes, "tfhd", true)
	if err != nil {
		return err
	}
	if len(header.payload) < 8 || header.payload[0] != 0 {
		return invalidProgressive("video track fragment header")
	}
	flags := binary.BigEndian.Uint32(header.payload[:4])
	if flags & ^uint32(0x03003a) != 0 || flags&0x020000 == 0 && !single {
		return invalidProgressive("video fragment-relative addressing")
	}
	id := binary.BigEndian.Uint32(header.payload[4:8])
	track, exists := p.tracks[id]
	if !exists || seen[id] {
		return invalidProgressive("video fragment track identity")
	}
	seen[id] = true
	offset := 8
	for _, flag := range []uint32{2, 8, 16, 32} {
		if flags&flag == 0 {
			continue
		}
		if len(header.payload)-offset < 4 {
			return invalidProgressive("video track fragment default fields")
		}
		value := binary.BigEndian.Uint32(header.payload[offset : offset+4])
		offset += 4
		if flag == 2 && value != 1 {
			return invalidProgressive("video fragment sample description")
		}
		if flag == 16 {
			track.defaultSize = value
		}
	}
	if offset != len(header.payload) {
		return invalidProgressive("video track fragment default size")
	}
	decode, err := progressiveVideoOne(boxes, "tfdt", true)
	if err != nil {
		return err
	}
	if len(decode.payload) < 4 || decode.payload[0] > 1 || binary.BigEndian.Uint32(decode.payload[:4])&0xffffff != 0 ||
		(decode.payload[0] == 0 && len(decode.payload) != 8) || (decode.payload[0] == 1 && len(decode.payload) != 12) {
		return invalidProgressive("video decode time box")
	}
	var previousEnd uint64
	haveRun := false
	for _, run := range boxes {
		if run.kind != "trun" {
			continue
		}
		if run.start < header.start {
			return invalidProgressive("video sample run before track header")
		}
		body := run.payload
		if len(body) < 8 || body[0] > 1 {
			return invalidProgressive("video sample run header")
		}
		runFlags := binary.BigEndian.Uint32(body[:4]) & 0xffffff
		if runFlags & ^uint32(0x000f05) != 0 || runFlags&4 != 0 && runFlags&0x400 != 0 || !haveRun && runFlags&1 == 0 {
			return invalidProgressive("video sample run flags")
		}
		count := uint64(binary.BigEndian.Uint32(body[4:8]))
		if count > maxProgressiveVideoSamples-p.samples || flags&0x010000 != 0 && count != 0 {
			return invalidProgressive("video fragment sample count")
		}
		p.samples += count
		position := 8
		start := previousEnd
		if runFlags&1 != 0 {
			if len(body)-position < 4 {
				return invalidProgressive("video sample run offset")
			}
			delta := int64(int32(binary.BigEndian.Uint32(body[position : position+4])))
			position += 4
			if delta < 0 || uint64(delta) > math.MaxInt64-moof.start {
				return invalidProgressive("video sample run data offset")
			}
			start = moof.start + uint64(delta)
		}
		if runFlags&4 != 0 {
			position += 4
		}
		fields := 0
		for _, flag := range []uint32{0x100, 0x200, 0x400, 0x800} {
			if runFlags&flag != 0 {
				fields++
			}
		}
		if uint64(position)+count*uint64(fields)*4 != uint64(len(body)) {
			return invalidProgressive("video sample run fields")
		}
		end := start
		for index := uint64(0); index < count; index++ {
			size := track.defaultSize
			for _, flag := range []uint32{0x100, 0x200, 0x400, 0x800} {
				if runFlags&flag == 0 {
					continue
				}
				value := binary.BigEndian.Uint32(body[position : position+4])
				position += 4
				if flag == 0x200 {
					size = value
				}
				// Version 1 CTS is signed; version 0 is unsigned. Neither value
				// participates in byte addressing or gets normalized here.
			}
			if size == 0 || uint64(size) > math.MaxInt64-end {
				return invalidProgressive("video fragment sample size")
			}
			next := end + uint64(size)
			if track.video {
				result.video = append(result.video, progressiveVideoSample{start: end, end: next, nalLength: track.nalLength})
			}
			end = next
		}
		if count > 0 {
			if start < moof.end {
				return invalidProgressive("video sample overlaps fragment metadata")
			}
			result.extents = append(result.extents, progressiveVideoExtent{start: start, end: end})
		}
		previousEnd, haveRun = end, true
	}
	if !haveRun && flags&0x010000 == 0 {
		return invalidProgressive("video track has no sample runs")
	}
	return nil
}

func (p *progressiveVideoParser) videoSample(data []byte, length int) (bool, error) {
	vcl := false
	for offset := 0; offset < len(data); {
		if len(data)-offset < length {
			return false, invalidProgressive("video partial NAL length")
		}
		var size uint32
		for _, value := range data[offset : offset+length] {
			size = size<<8 | uint32(value)
		}
		offset += length
		if size < 2 || uint64(size) > uint64(len(data)-offset) {
			return false, invalidProgressive("video NAL sample boundary")
		}
		kind := data[offset] & 31
		if data[offset]&0x80 != 0 || kind == 0 || kind >= 24 {
			return false, invalidProgressive("video AVC NAL header")
		}
		if err := p.count(); err != nil {
			return false, err
		}
		vcl = vcl || kind == 1 || kind == 2 || kind == 5
		offset += int(size)
	}
	return vcl, nil
}
