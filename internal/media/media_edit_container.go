package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	mediaEditContainerMaxHeaders  = 2_000_000
	mediaEditContainerMaxDepth    = 16
	mediaEditContainerMaxMetadata = 64 << 20
)

// mediaEditContainerAdmission rejects structures whose meaning is not fully
// represented by the separate stream, chapter, tag and packet proofs. It is
// deliberately a smaller profile than either container specification. It
// borrows the descriptor, uses only ReadAt, and never reads media payloads.
// Container instance IDs, indexes, padding and writer provenance may be
// regenerated. The latter is returned for explicit before/after evidence.
func mediaEditContainerAdmission(ctx context.Context, file *os.File, size int64, container string) (map[string]string, error) {
	proof, err := mediaEditReadContainerProof(ctx, file, size, container)
	if err != nil {
		return nil, err
	}
	return proof.Writer, nil
}

func mediaEditReadContainerProof(ctx context.Context, file *os.File, size int64, container string) (mediaEditContainerProof, error) {
	if err := ctx.Err(); err != nil {
		return mediaEditContainerProof{}, err
	}
	if file == nil || size <= 0 || size > MaxSubtitleRemovalInputBytes+mediaEditOutputAllowance {
		return mediaEditContainerProof{}, mediaEditContainerError("invalid descriptor extent")
	}
	s := mediaEditContainerScanner{ctx: ctx, file: file, size: size, writer: map[string]string{},
		trackNumbers: map[uint64]bool{}, uids: map[string]map[uint64]bool{"track": {}, "chapter": {}, "attachment": {}},
		tags: map[string]bool{}, projections: map[string]bool{}, mp4Projections: map[string]string{}, segmentElements: map[int64]uint64{}}
	var err error
	switch container {
	case "mkv", "mka":
		_, err = s.ebml("root", 0, size, 0)
		if err == nil {
			for _, target := range s.seekTargets {
				if target.position > uint64(size-s.segmentStart) || s.segmentElements[s.segmentStart+int64(target.position)] != target.id {
					err = mediaEditContainerError("Matroska SeekHead points outside its declared element")
					break
				}
			}
		}
		if err == nil {
			for _, target := range s.tagTargets {
				if target.kind != "" && !s.uids[target.kind][target.uid] {
					err = mediaEditContainerError("Matroska tag refers to an absent object")
					break
				}
			}
		}
	case "mp4":
		err = s.mp4("root", 0, size, 0, nil)
	default:
		err = mediaEditContainerError("unsupported container")
	}
	if err != nil {
		return mediaEditContainerProof{}, err
	}
	proof := mediaEditContainerProof{Writer: s.writer, Chapters: s.chapterDisplays}
	for _, track := range s.mp4Tracks {
		roll, err := s.mp4RollProof(track)
		if err != nil {
			return mediaEditContainerProof{}, err
		}
		idOffset := 12 + int(track.header[0])*8
		proof.MP4Tracks = append(proof.MP4Tracks, mediaEditMP4TrackProof{
			ID: uint64(binary.BigEndian.Uint32(track.header[idOffset : idOffset+4])), Codec: track.codec,
			Samples: track.tableSamples, Roll: roll,
		})
	}
	return proof, nil
}

type mediaEditContainerScanner struct {
	ctx             context.Context
	file            *os.File
	size            int64
	headers         int
	metadata        int64
	attachment      int64
	writer          map[string]string
	trackNumbers    map[uint64]bool
	uids            map[string]map[uint64]bool
	tags            map[string]bool
	projections     map[string]bool
	tagTargets      []mediaEditEBMLTarget
	globalMP4Tags   map[string]string
	mp4Projections  map[string]string
	mp4Tracks       []*mediaEditMP4Track
	movieScale      uint64
	movieDuration   uint64
	movieCreation   uint64
	segmentStart    int64
	segmentElements map[int64]uint64
	seekTargets     []mediaEditEBMLSeek
	chapterDisplays []mediaEditChapterDisplayProof
}

type mediaEditEBMLSeek struct {
	id, position uint64
}

func mediaEditContainerError(reason string) error {
	return fmt.Errorf("%w: container structure: %s", ErrSubtitleRemovalUnsupported, reason)
}

func (s *mediaEditContainerScanner) header(depth int) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	s.headers++
	if depth > mediaEditContainerMaxDepth || s.headers > mediaEditContainerMaxHeaders {
		return fmt.Errorf("%w: container structural complexity", ErrSubtitleRemovalBudget)
	}
	return nil
}

func (s *mediaEditContainerScanner) charge(size int64) error {
	if size < 0 || size > mediaEditContainerMaxMetadata-s.metadata {
		return fmt.Errorf("%w: container metadata extent", ErrSubtitleRemovalBudget)
	}
	s.metadata += size
	return nil
}

func (s *mediaEditContainerScanner) read(offset int64, data []byte) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if offset < 0 || offset > s.size || int64(len(data)) > s.size-offset {
		return mediaEditContainerError("truncated element")
	}
	if _, err := s.file.ReadAt(data, offset); err != nil {
		return fmt.Errorf("%w: container read: %v", ErrSubtitleRemovalUnsupported, err)
	}
	return nil
}

type mediaEditEBMLRule struct {
	kind string
	max  int // Zero permits repeated elements, subject to the global budget.
}

// Only leaves reported by ffprobe, neutral defaults, or container-local
// bookkeeping appear here. In particular ContentEncodings, linked segments,
// ordered editions, nested chapters/tags, BlockAdditions and CodecState do not.
var mediaEditEBMLProfile = map[string]map[uint64]mediaEditEBMLRule{
	"root": {0x1A45DFA3: {"header", 1}, 0x18538067: {"segment", 1}},
	"header": {0x4286: {"u", 1}, 0x42F7: {"u", 1}, 0x42F2: {"u", 1}, 0x42F3: {"u", 1},
		0x4282: {"text", 1}, 0x4287: {"u", 1}, 0x4285: {"u", 1}},
	"segment": {0x114D9B74: {"seekhead", 2}, 0x1549A966: {"info", 1}, 0x1654AE6B: {"tracks", 1},
		0x1F43B675: {"cluster", 0}, 0x1C53BB6B: {"cues", 1}, 0x1941A469: {"attachments", 1},
		0x1043A770: {"chapters", 1}, 0x1254C367: {"tags", 0}},
	"info": {0x73A4: {"uuid", 1}, 0x2AD7B1: {"u", 1}, 0x4489: {"float", 1},
		0x4461: {"date", 1}, 0x7BA9: {"text", 1}, 0x4D80: {"writer", 1}, 0x5741: {"writer", 1}},
	"seekhead": {0x4DBB: {"seek", 0}},
	"seek":     {0x53AB: {"seekid", 1}, 0x53AC: {"u", 1}},
	"tracks":   {0xAE: {"track", mediaEditMaxStreams}},
	"track": {0xD7: {"u", 1}, 0x73C5: {"u", 1}, 0x83: {"u", 1}, 0xB9: {"u", 1},
		0x88: {"u", 1}, 0x55AA: {"u", 1}, 0x55AB: {"u", 1}, 0x55AC: {"u", 1},
		0x55AD: {"u", 1}, 0x55AE: {"u", 1}, 0x55AF: {"u", 1}, 0x9C: {"u", 1},
		0x6DE7: {"u", 1}, 0x23E383: {"u", 1}, 0x23314F: {"float", 1}, 0x55EE: {"u", 1},
		0x536E: {"text", 1}, 0x22B59C: {"text", 1}, 0x86: {"text", 1}, 0x63A2: {"private", 1},
		0xAA: {"u", 1}, 0x56AA: {"u", 1}, 0x56BB: {"u", 1}, 0xE0: {"video", 1}, 0xE1: {"audio", 1}},
	"video": {0x9A: {"u", 1}, 0x9D: {"u", 1}, 0x53B8: {"u", 1}, 0x53C0: {"u", 1},
		0xB0: {"u", 1}, 0xBA: {"u", 1}, 0x54AA: {"u", 1}, 0x54BB: {"u", 1},
		0x54CC: {"u", 1}, 0x54DD: {"u", 1}, 0x54B0: {"u", 1}, 0x54BA: {"u", 1},
		0x54B2: {"u", 1}, 0x54B3: {"u", 1}, 0x55B0: {"colour", 1}},
	"colour": {0x55B1: {"u", 1}, 0x55B2: {"u", 1}, 0x55B7: {"u", 1}, 0x55B8: {"u", 1},
		0x55B9: {"u", 1}, 0x55BA: {"u", 1}, 0x55BB: {"u", 1}, 0x55BC: {"u", 1},
		0x55BD: {"u", 1}, 0x55D0: {"mastering", 1}},
	"mastering": {0x55D1: {"float", 1}, 0x55D2: {"float", 1}, 0x55D3: {"float", 1},
		0x55D4: {"float", 1}, 0x55D5: {"float", 1}, 0x55D6: {"float", 1}, 0x55D7: {"float", 1},
		0x55D8: {"float", 1}, 0x55D9: {"float", 1}, 0x55DA: {"float", 1}},
	"audio":      {0xB5: {"float", 1}, 0x78B5: {"float", 1}, 0x9F: {"u", 1}, 0x6264: {"u", 1}},
	"cluster":    {0xE7: {"u", 1}, 0xA7: {"u", 1}, 0xAB: {"u", 1}, 0xA3: {"simpleblock", 0}, 0xA0: {"blockgroup", 0}},
	"blockgroup": {0xA1: {"block", 1}, 0x9B: {"u", 1}, 0xFA: {"u", 1}, 0x75A2: {"signed", 1}},
	"cues":       {0xBB: {"cuepoint", 0}},
	"cuepoint":   {0xB3: {"u", 1}, 0xB7: {"cueposition", 0}},
	"cueposition": {0xF7: {"u", 1}, 0xF1: {"u", 1}, 0xF0: {"u", 1},
		0xB2: {"u", 1}, 0x5378: {"u", 1}},
	"attachments": {0x61A7: {"attachment", mediaEditMaxStreams}},
	"attachment": {0x467E: {"text", 1}, 0x466E: {"text", 1}, 0x4660: {"text", 1},
		0x465C: {"filedata", 1}, 0x46AE: {"u", 1}},
	"chapters": {0x45B9: {"edition", 1}},
	"edition": {0x45BC: {"u", 1}, 0x45BD: {"u", 1}, 0x45DB: {"u", 1},
		0x45DD: {"u", 1}, 0xB6: {"chapter", 10000}},
	"chapter": {0x73C4: {"u", 1}, 0x91: {"u", 1}, 0x92: {"u", 1},
		0x98: {"u", 1}, 0x4598: {"u", 1}, 0x80: {"display", 1}},
	"display": {0x85: {"text", 1}, 0x437C: {"text", 1}},
	"tags":    {0x7373: {"tag", 0}},
	"tag":     {0x63C0: {"targets", 1}, 0x67C8: {"simpletag", 4096}},
	"targets": {0x68CA: {"u", 1}, 0x63C5: {"u", 1}, 0x63C4: {"u", 1}, 0x63C6: {"u", 1}},
	"simpletag": {0x45A3: {"text", 1}, 0x447A: {"text", 1},
		0x4484: {"u", 1}, 0x4487: {"text", 1}},
}

type mediaEditEBMLTarget struct {
	kind string
	uid  uint64
}

type mediaEditEBMLScope struct {
	count   map[uint64]int
	uints   map[uint64]uint64
	texts   map[uint64]string
	floats  map[uint64]float64
	names   []string
	target  mediaEditEBMLTarget
	display *mediaEditChapterDisplayProof
}

func (s *mediaEditContainerScanner) ebml(parent string, start, end int64, depth int) (mediaEditEBMLScope, error) {
	r := mediaEditEBMLScope{count: map[uint64]int{}, uints: map[uint64]uint64{}, texts: map[uint64]string{}, floats: map[uint64]float64{}}
	for offset := start; offset < end; {
		if err := s.header(depth); err != nil {
			return r, err
		}
		id, idBytes, _, err := s.vint(offset, end, true)
		if err != nil {
			return r, err
		}
		length, lengthBytes, unknown, err := s.vint(offset+int64(idBytes), end, false)
		if err != nil {
			return r, err
		}
		body := offset + int64(idBytes+lengthBytes)
		if unknown {
			if parent != "root" || id != 0x18538067 {
				return r, mediaEditContainerError("unknown-size Matroska element outside Segment")
			}
			length = uint64(end - body)
		}
		if length > uint64(end-body) {
			return r, mediaEditContainerError("Matroska element exceeds its parent")
		}
		next := body + int64(length)
		if id == 0xEC { // Void has no playback or metadata meaning.
			offset = next
			continue
		}
		if id == 0xBF {
			if length != 4 || r.count[id] != 0 {
				return r, mediaEditContainerError("invalid Matroska CRC element")
			}
			r.count[id]++
			offset = next
			continue
		}
		rule, known := mediaEditEBMLProfile[parent][id]
		if !known {
			return r, mediaEditContainerError(fmt.Sprintf("unproven Matroska %s element 0x%X", parent, id))
		}
		r.count[id]++
		if rule.max != 0 && r.count[id] > rule.max {
			return r, mediaEditContainerError(fmt.Sprintf("duplicate or excessive Matroska %s element 0x%X", parent, id))
		}
		if parent == "root" && id == 0x18538067 && r.count[0x1A45DFA3] != 1 {
			return r, mediaEditContainerError("Matroska Segment precedes its EBML header")
		}
		if parent == "root" && id == 0x18538067 {
			s.segmentStart = body
		}
		if parent == "segment" {
			if len(s.segmentElements) >= 100000 {
				return r, fmt.Errorf("%w: Matroska top-level elements", ErrSubtitleRemovalBudget)
			}
			s.segmentElements[offset] = id
		}
		if _, master := mediaEditEBMLProfile[rule.kind]; master {
			child, err := s.ebml(rule.kind, body, next, depth+1)
			if err != nil {
				return r, err
			}
			if parent == "tag" && rule.kind == "simpletag" {
				r.names = append(r.names, child.texts[0x45A3])
			}
			if parent == "tag" && rule.kind == "targets" {
				r.target = child.target
			}
			if parent == "chapter" && rule.kind == "display" {
				language := "eng"
				if child.count[0x437C] != 0 {
					language = child.texts[0x437C]
				}
				r.display = &mediaEditChapterDisplayProof{HasDisplay: true, Title: child.texts[0x85], Language: language}
			}
		} else if err := s.ebmlLeaf(&r, rule.kind, id, body, int64(length)); err != nil {
			return r, err
		}
		offset = next
	}
	return r, s.ebmlFinish(parent, &r)
}

func (s *mediaEditContainerScanner) vint(offset, end int64, identifier bool) (uint64, int, bool, error) {
	var encoded [8]byte
	if offset >= end {
		return 0, 0, false, mediaEditContainerError("truncated Matroska variable integer")
	}
	if err := s.read(offset, encoded[:1]); err != nil {
		return 0, 0, false, err
	}
	width, marker := 1, byte(0x80)
	for marker != 0 && encoded[0]&marker == 0 {
		width++
		marker >>= 1
	}
	if marker == 0 || identifier && width > 4 || int64(width) > end-offset {
		return 0, 0, false, mediaEditContainerError("invalid Matroska variable integer")
	}
	if err := s.read(offset, encoded[:width]); err != nil {
		return 0, 0, false, err
	}
	value := uint64(encoded[0] & (marker - 1))
	for _, b := range encoded[1:width] {
		value = value<<8 | uint64(b)
	}
	unknown := value == uint64(1)<<(7*width)-1
	if identifier {
		if unknown {
			return 0, 0, false, mediaEditContainerError("reserved Matroska element identifier")
		}
		value |= uint64(marker) << (8 * (width - 1))
	}
	return value, width, unknown, nil
}

func (s *mediaEditContainerScanner) ebmlLeaf(r *mediaEditEBMLScope, kind string, id uint64, offset, size int64) error {
	if kind == "block" || kind == "simpleblock" {
		track, width, unknown, err := s.vint(offset, offset+size, false)
		if err != nil {
			return err
		}
		if unknown || !s.trackNumbers[track] || size < int64(width+3) {
			return mediaEditContainerError("Matroska block has no admitted track or header")
		}
		var flags [1]byte
		if err := s.read(offset+int64(width)+2, flags[:]); err != nil {
			return err
		}
		// Lacing and keyframe flags are represented by complete packet proofs.
		// Invisible/discardable semantics are not completely exposed by ffprobe.
		allowed := byte(0x06)
		if kind == "simpleblock" {
			allowed |= 0x80
		}
		if flags[0] & ^allowed != 0 {
			return mediaEditContainerError("unproven Matroska block flags")
		}
		return s.ebmlLacing(offset+int64(width)+3, offset+size, flags[0]&0x06)
	}
	if kind == "filedata" {
		if size <= 0 || size > (256<<20)-s.attachment {
			return fmt.Errorf("%w: Matroska attachment bytes", ErrSubtitleRemovalBudget)
		}
		s.attachment += size
		return nil // The independent attachment extradata digest proves all bytes.
	}
	if err := s.charge(size); err != nil {
		return err
	}
	if kind == "private" {
		if size > 16<<20 {
			return fmt.Errorf("%w: Matroska codec private bytes", ErrSubtitleRemovalBudget)
		}
		return nil // The independent codec extradata digest proves all bytes.
	}
	switch kind {
	case "u", "signed", "float", "date", "seekid":
		if size < 1 || size > 8 || kind == "float" && size != 4 && size != 8 || kind == "date" && size != 8 || kind == "seekid" && size > 4 {
			return mediaEditContainerError("invalid Matroska scalar width")
		}
		var raw [8]byte
		if err := s.read(offset, raw[8-size:]); err != nil {
			return err
		}
		value := binary.BigEndian.Uint64(raw[:])
		r.uints[id] = value
		if kind == "float" {
			v := math.Float64frombits(value)
			if size == 4 {
				v = float64(math.Float32frombits(uint32(value)))
			}
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return mediaEditContainerError("invalid Matroska floating-point value")
			}
			r.floats[id] = v
		}
	case "uuid":
		if size != 16 {
			return mediaEditContainerError("invalid Matroska Segment UUID")
		}
	case "text", "writer":
		limit := int64(1 << 20)
		if kind == "writer" || id == 0x45A3 {
			limit = 4096
		}
		if size > limit {
			return fmt.Errorf("%w: Matroska text length", ErrSubtitleRemovalBudget)
		}
		data := make([]byte, int(size))
		if err := s.read(offset, data); err != nil {
			return err
		}
		if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
			return mediaEditContainerError("Matroska text is not an unambiguous UTF-8 string")
		}
		r.texts[id] = string(data)
		if kind == "writer" {
			name := "MuxingApp"
			if id == 0x5741 {
				name = "WritingApp"
			}
			s.writer[name] = string(data)
		}
	default:
		return mediaEditContainerError("unknown Matroska profile rule")
	}
	return nil
}

// Inspect only lace length headers, never frame payloads. This prevents an
// invalid lace from making the demuxer silently ignore unaccounted block bytes.
func (s *mediaEditContainerScanner) ebmlLacing(start, end int64, kind byte) error {
	if kind == 0 {
		return nil
	}
	if start >= end {
		return mediaEditContainerError("truncated Matroska lace header")
	}
	var raw [1]byte
	if err := s.read(start, raw[:]); err != nil {
		return err
	}
	if err := s.charge(1); err != nil {
		return err
	}
	frames, cursor := int(raw[0])+1, start+1
	if frames < 2 {
		return mediaEditContainerError("invalid Matroska lace frame count")
	}
	if kind == 4 {
		if (end-cursor)%int64(frames) != 0 {
			return mediaEditContainerError("invalid fixed-size Matroska lace")
		}
		return nil
	}
	var sum, previous int64
	for index := 0; index < frames-1; index++ {
		if err := s.header(0); err != nil {
			return err
		}
		var length int64
		if kind == 2 {
			for {
				if cursor >= end {
					return mediaEditContainerError("truncated Xiph lace header")
				}
				if cursor-start >= 64<<10 {
					return fmt.Errorf("%w: Matroska lace header", ErrSubtitleRemovalBudget)
				}
				if err := s.read(cursor, raw[:]); err != nil {
					return err
				}
				if err := s.charge(1); err != nil {
					return err
				}
				cursor++
				length += int64(raw[0])
				if raw[0] != 255 {
					break
				}
			}
		} else {
			value, width, _, err := s.vint(cursor, end, false)
			if err != nil {
				return err
			}
			if err := s.charge(int64(width)); err != nil {
				return err
			}
			cursor += int64(width)
			length = int64(value)
			if index != 0 {
				length += previous - (int64(1)<<(7*width-1) - 1)
			}
		}
		if length < 0 || length > end-cursor || sum > end-cursor-length {
			return mediaEditContainerError("Matroska lace lengths exceed the block")
		}
		sum, previous = sum+length, length
	}
	if sum > end-cursor {
		return mediaEditContainerError("Matroska lace has no complete final frame")
	}
	return nil
}

func (s *mediaEditContainerScanner) ebmlFinish(parent string, r *mediaEditEBMLScope) error {
	required := map[string][]uint64{"root": {0x1A45DFA3, 0x18538067}, "header": {0x4282}, "segment": {0x1549A966, 0x1654AE6B, 0x1F43B675},
		"tracks": {0xAE}, "track": {0xD7, 0x73C5, 0x83, 0x86}, "cluster": {0xE7}, "blockgroup": {0xA1},
		"attachment": {0x466E, 0x4660, 0x465C, 0x46AE}, "chapters": {0x45B9}, "edition": {0xB6},
		"chapter": {0x73C4, 0x91, 0x92}, "display": {0x85}, "tag": {0x67C8}, "simpletag": {0x45A3, 0x4487}, "seek": {0x53AB, 0x53AC}}
	for _, id := range required[parent] {
		if r.count[id] == 0 {
			return mediaEditContainerError(fmt.Sprintf("Matroska %s lacks required element 0x%X", parent, id))
		}
	}
	defaults := map[string]map[uint64]uint64{"header": {0x4286: 1, 0x42F7: 1, 0x42F2: 4},
		"track":      {0xB9: 1, 0x6DE7: 0, 0x55EE: 0, 0xAA: 1, 0x56AA: 0, 0x56BB: 0},
		"video":      {0x54AA: 0, 0x54BB: 0, 0x54CC: 0, 0x54DD: 0, 0x54B2: 0, 0x54B3: 0},
		"colour":     {0x55B2: 0},
		"blockgroup": {0xFA: 0}, "edition": {0x45BD: 0, 0x45DD: 0},
		"chapter": {0x98: 0, 0x4598: 1}, "targets": {0x68CA: 50}, "simpletag": {0x4484: 1}}
	for id, expected := range defaults[parent] {
		if value, found := r.uints[id]; found && value != expected {
			return mediaEditContainerError(fmt.Sprintf("unproven nondefault Matroska %s element 0x%X", parent, id))
		}
	}
	switch parent {
	case "seek":
		if len(s.seekTargets) >= 1024 {
			return fmt.Errorf("%w: Matroska seek entries", ErrSubtitleRemovalBudget)
		}
		s.seekTargets = append(s.seekTargets, mediaEditEBMLSeek{id: r.uints[0x53AB], position: r.uints[0x53AC]})
	case "info":
		for id, key := range map[uint64]string{0x7BA9: "title", 0x4461: "creation_time"} {
			if r.count[id] > 0 {
				if err := s.ebmlProjection("", 0, key); err != nil {
					return err
				}
			}
		}
		if r.count[0x4461] > 0 && int64(r.uints[0x4461])%1000 != 0 {
			return mediaEditContainerError("Matroska creation timestamp exceeds ffprobe precision")
		}
		if r.count[0x2AD7B1] > 0 && r.uints[0x2AD7B1] == 0 {
			return mediaEditContainerError("zero Matroska timestamp scale")
		}
	case "header":
		if r.texts[0x4282] != "matroska" || r.count[0x4287] > 0 && (r.uints[0x4287] < 1 || r.uints[0x4287] > 4) ||
			r.count[0x4285] > 0 && (r.uints[0x4285] < 1 || r.uints[0x4285] > 4) ||
			r.count[0x42F3] > 0 && (r.uints[0x42F3] < 1 || r.uints[0x42F3] > 8) {
			return mediaEditContainerError("unsupported EBML document profile")
		}
	case "track":
		for _, id := range []uint64{0x88, 0x55AA, 0x55AB, 0x55AC, 0x55AD, 0x55AE, 0x55AF, 0x9C} {
			if r.uints[id] > 1 {
				return mediaEditContainerError("invalid Matroska track flag")
			}
		}
		if r.count[0x23314F] != 0 && r.floats[0x23314F] != 1 || r.texts[0x86] == "" {
			return mediaEditContainerError("unproven Matroska track scale or codec")
		}
		number := r.uints[0xD7]
		if number == 0 || s.trackNumbers[number] {
			return mediaEditContainerError("duplicate or zero Matroska track number")
		}
		s.trackNumbers[number] = true
		if err := s.ebmlUID("track", r.uints[0x73C5]); err != nil {
			return err
		}
		if err := s.ebmlProjection("track", r.uints[0x73C5], "language"); err != nil {
			return err
		}
		if r.count[0x536E] > 0 {
			return s.ebmlProjection("track", r.uints[0x73C5], "title")
		}
	case "attachment":
		if err := s.ebmlUID("attachment", r.uints[0x46AE]); err != nil {
			return err
		}
		for _, key := range []string{"filename", "mimetype"} {
			if err := s.ebmlProjection("attachment", r.uints[0x46AE], key); err != nil {
				return err
			}
		}
		if r.count[0x467E] > 0 {
			return s.ebmlProjection("attachment", r.uints[0x46AE], "title")
		}
	case "chapter":
		if r.uints[0x92] < r.uints[0x91] {
			return mediaEditContainerError("reversed Matroska chapter interval")
		}
		if err := s.ebmlUID("chapter", r.uints[0x73C4]); err != nil {
			return err
		}
		chapter := mediaEditChapterDisplayProof{StartNanoseconds: r.uints[0x91], EndNanoseconds: r.uints[0x92]}
		if r.display != nil {
			chapter.HasDisplay, chapter.Title, chapter.Language = r.display.HasDisplay, r.display.Title, r.display.Language
		}
		s.chapterDisplays = append(s.chapterDisplays, chapter)
		if r.count[0x80] > 0 {
			return s.ebmlProjection("chapter", r.uints[0x73C4], "title")
		}
	case "edition":
		if r.uints[0x45DB] > 1 {
			return mediaEditContainerError("invalid Matroska default edition flag")
		}
	case "display":
		if r.count[0x437C] > 0 && !mediaEditChapterLanguage(r.texts[0x437C]) {
			return mediaEditContainerError("chapter display language is outside the proven three-letter profile")
		}
	case "simpletag":
		if r.texts[0x45A3] == "" || r.count[0x447A] != 0 && r.texts[0x447A] != "und" {
			return mediaEditContainerError("unproven Matroska tag name or language")
		}
	case "targets":
		for id, kind := range map[uint64]string{0x63C5: "track", 0x63C4: "chapter", 0x63C6: "attachment"} {
			if r.count[id] == 0 {
				continue
			}
			if r.target.kind != "" || r.uints[id] == 0 {
				return mediaEditContainerError("ambiguous Matroska tag target")
			}
			r.target = mediaEditEBMLTarget{kind: kind, uid: r.uints[id]}
		}
	case "tag":
		for _, name := range r.names {
			key := fmt.Sprintf("%s/%d/%s", r.target.kind, r.target.uid, strings.ToLower(name))
			if s.tags[key] || s.projections[key] || len(s.tags) >= 16384 {
				return mediaEditContainerError("duplicate or excessive Matroska metadata tag")
			}
			s.tags[key] = true
		}
		s.tagTargets = append(s.tagTargets, r.target)
	}
	return nil
}

func (s *mediaEditContainerScanner) ebmlUID(kind string, uid uint64) error {
	if uid == 0 || s.uids[kind][uid] {
		return mediaEditContainerError("duplicate or zero Matroska " + kind + " UID")
	}
	s.uids[kind][uid] = true
	return nil
}

func (s *mediaEditContainerScanner) ebmlProjection(kind string, uid uint64, name string) error {
	key := fmt.Sprintf("%s/%d/%s", kind, uid, name)
	if s.tags[key] {
		return mediaEditContainerError("Matroska tag would hide a separate header metadata value")
	}
	s.projections[key] = true
	return nil
}

type mediaEditMP4Box struct {
	kind       string
	start, end int64
}

type mediaEditMP4Track struct {
	header           []byte
	handler          string
	codec            string
	mediaHeader      string
	duration         uint64
	mediaScale       uint64
	mediaDuration    uint64
	tableDuration    uint64
	tableSamples     uint64
	sizeSamples      uint64
	width            uint16
	height           uint16
	edit             []byte
	creation         uint64
	creationSet      bool
	rollDistance     *int16
	rollSamples      uint64
	rollDescriptions bool
	rollMapping      bool
}

func (s *mediaEditContainerScanner) mp4Box(offset, end int64, depth int) (mediaEditMP4Box, error) {
	if err := s.header(depth); err != nil {
		return mediaEditMP4Box{}, err
	}
	var raw [16]byte
	if end-offset < 8 {
		return mediaEditMP4Box{}, mediaEditContainerError("truncated MP4 box header")
	}
	if err := s.read(offset, raw[:8]); err != nil {
		return mediaEditMP4Box{}, err
	}
	size, header := uint64(binary.BigEndian.Uint32(raw[:4])), int64(8)
	if size == 1 {
		if end-offset < 16 {
			return mediaEditMP4Box{}, mediaEditContainerError("truncated extended MP4 box header")
		}
		if err := s.read(offset+8, raw[8:]); err != nil {
			return mediaEditMP4Box{}, err
		}
		size, header = binary.BigEndian.Uint64(raw[8:]), 16
	} else if size == 0 {
		size = uint64(end - offset)
	}
	if size < uint64(header) || size > uint64(end-offset) {
		return mediaEditMP4Box{}, mediaEditContainerError("MP4 box exceeds its parent")
	}
	return mediaEditMP4Box{kind: string(raw[4:8]), start: offset + header, end: offset + int64(size)}, nil
}

var mediaEditMP4Children = map[string]map[string]bool{
	"root": {"ftyp": true, "moov": true, "mdat": true},
	"moov": {"mvhd": true, "trak": true, "udta": true, "meta": true},
	"trak": {"tkhd": true, "mdia": true, "edts": true},
	"mdia": {"mdhd": true, "hdlr": true, "minf": true},
	"minf": {"vmhd": true, "smhd": true, "nmhd": true, "dinf": true, "stbl": true},
	"dinf": {"dref": true}, "edts": {"elst": true}, "udta": {"meta": true},
	"stbl": {"stsd": true, "stts": true, "ctts": true, "stsc": true, "stsz": true, "stco": true, "co64": true, "stss": true, "sgpd": true, "sbgp": true},
}

// MP4 is limited to ordinary self-contained avc1/mp4a/tx3g tracks. Fragmented,
// encrypted, chapter-reference, alternate-sample-description, private,
// external-data and nontrivial edit-list structures are rejected. AAC roll
// groups are admitted only with separately proven one-sample preroll semantics.
func (s *mediaEditContainerScanner) mp4(parent string, start, end int64, depth int, track *mediaEditMP4Track) error {
	counts := map[string]int{}
	for offset := start; offset < end; {
		box, err := s.mp4Box(offset, end, depth)
		if err != nil {
			return err
		}
		offset = box.end
		if box.kind == "free" || box.kind == "skip" {
			continue
		}
		if !mediaEditMP4Children[parent][box.kind] {
			return mediaEditContainerError(fmt.Sprintf("unproven MP4 %s box %q", parent, box.kind))
		}
		counts[box.kind]++
		if counts[box.kind] > 1 && box.kind != "mdat" && box.kind != "trak" || counts["trak"] > mediaEditMaxStreams {
			return mediaEditContainerError("duplicate or excessive MP4 " + box.kind + " box")
		}
		switch box.kind {
		case "mdat":
			if box.end == box.start {
				return mediaEditContainerError("empty MP4 media data")
			}
		case "trak":
			child := &mediaEditMP4Track{}
			if err := s.mp4("trak", box.start, box.end, depth+1, child); err != nil {
				return err
			}
			if err := s.mp4TrackFinish(child); err != nil {
				return err
			}
			s.mp4Tracks = append(s.mp4Tracks, child)
		case "meta":
			if err := s.mp4Metadata(box, depth+1); err != nil {
				return err
			}
		default:
			if _, master := mediaEditMP4Children[box.kind]; master {
				if err := s.mp4(box.kind, box.start, box.end, depth+1, track); err != nil {
					return err
				}
			} else if err := s.mp4Leaf(box, depth, track); err != nil {
				return err
			}
		}
	}
	required := map[string][]string{"root": {"ftyp", "moov", "mdat"}, "moov": {"mvhd", "trak"},
		"trak": {"tkhd", "mdia"}, "mdia": {"mdhd", "hdlr", "minf"}, "minf": {"dinf", "stbl"},
		"dinf": {"dref"}, "edts": {"elst"}, "stbl": {"stsd", "stts", "stsc", "stsz"}}
	for _, kind := range required[parent] {
		if counts[kind] == 0 {
			return mediaEditContainerError("MP4 " + parent + " lacks " + kind)
		}
	}
	if parent == "stbl" && counts["stco"]+counts["co64"] != 1 || parent == "minf" && counts["vmhd"]+counts["smhd"]+counts["nmhd"] != 1 {
		return mediaEditContainerError("ambiguous MP4 sample table or media header")
	}
	if parent == "root" {
		return s.mp4Timeline()
	}
	return nil
}

func (s *mediaEditContainerScanner) mp4Bytes(box mediaEditMP4Box, limit int64) ([]byte, error) {
	length := box.end - box.start
	if length > limit {
		return nil, fmt.Errorf("%w: MP4 %s metadata", ErrSubtitleRemovalBudget, box.kind)
	}
	data := make([]byte, int(length))
	if err := s.read(box.start, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *mediaEditContainerScanner) mp4Leaf(box mediaEditMP4Box, depth int, track *mediaEditMP4Track) error {
	if err := s.charge(box.end - box.start); err != nil {
		return err
	}
	if box.kind == "stsd" {
		return s.mp4SampleDescription(box, depth+1, track)
	}
	if box.kind == "sgpd" || box.kind == "sbgp" {
		return s.mp4SampleGroup(box, track)
	}
	if box.kind == "stts" || box.kind == "ctts" || box.kind == "stsc" || box.kind == "stsz" || box.kind == "stco" || box.kind == "co64" || box.kind == "stss" {
		return s.mp4Table(box, track)
	}
	data, err := s.mp4Bytes(box, 4096)
	if err != nil {
		return err
	}
	invalid := func() error { return mediaEditContainerError("unproven MP4 " + box.kind + " fields") }
	switch box.kind {
	case "ftyp":
		if len(data) < 8 || len(data)%4 != 0 {
			return invalid()
		}
		brands := map[string]bool{"isom": true, "iso2": true, "iso3": true, "iso4": true, "iso5": true, "iso6": true, "avc1": true, "mp41": true, "mp42": true, "M4A ": true}
		for offset := 0; offset < len(data); offset += 4 {
			if offset != 4 && !brands[string(data[offset:offset+4])] {
				return invalid()
			}
		}
		s.mp4Projections["major_brand"] = string(data[:4])
		s.mp4Projections["minor_version"] = strconv.FormatUint(uint64(binary.BigEndian.Uint32(data[4:8])), 10)
		s.mp4Projections["compatible_brands"] = string(data[8:])
	case "mvhd", "mdhd", "tkhd":
		if len(data) < 4 || data[0] > 1 {
			return invalid()
		}
		version := int(data[0])
		expected := map[string]int{"mvhd": 100 + 12*version, "mdhd": 24 + 12*version, "tkhd": 84 + 12*version}[box.kind]
		if len(data) != expected || box.kind != "tkhd" && !mediaEditZero(data[1:4]) || box.kind == "tkhd" && (data[1] != 0 || data[2] != 0 || data[3] != 2 && data[3] != 3) {
			return invalid()
		}
		width := 4 + 4*version
		if !bytes.Equal(data[4:4+width], data[4+width:4+2*width]) {
			return mediaEditContainerError("MP4 modification time differs from its represented creation time")
		}
		creation := mediaEditBigEndian(data[4 : 4+width])
		if creation != 0 && (creation < 2082844800 || creation > 253402300799+2082844800) {
			return mediaEditContainerError("ambiguous or unrepresentable MP4 creation time")
		}
		if box.kind == "mvhd" {
			s.movieCreation = creation
		} else {
			if track.creationSet && track.creation != creation {
				return mediaEditContainerError("MP4 track and media creation times differ")
			}
			track.creation, track.creationSet = creation, true
		}
		base := 4 + 2*width
		if box.kind == "tkhd" {
			if !mediaEditZero(data[base+4 : base+8]) {
				return invalid()
			}
			track.header = data
			track.duration = mediaEditBigEndian(data[base+8 : base+8+width])
			break
		}
		if binary.BigEndian.Uint32(data[base:base+4]) == 0 {
			return invalid()
		}
		if box.kind == "mdhd" {
			if !mediaEditZero(data[len(data)-2:]) {
				return invalid()
			}
			track.mediaScale = uint64(binary.BigEndian.Uint32(data[base : base+4]))
			track.mediaDuration = mediaEditBigEndian(data[base+4 : base+4+width])
			break
		}
		s.movieScale = uint64(binary.BigEndian.Uint32(data[base : base+4]))
		s.movieDuration = mediaEditBigEndian(data[base+4 : base+4+width])
		base += 4 + width
		if binary.BigEndian.Uint32(data[base:base+4]) != 0x10000 || binary.BigEndian.Uint16(data[base+4:base+6]) != 0x100 ||
			!mediaEditZero(data[base+6:base+16]) || !mediaEditIdentityMatrix(data[base+16:base+52]) || !mediaEditZero(data[base+52:base+76]) {
			return invalid()
		}
	case "hdlr":
		handler, err := mediaEditMP4Handler(data)
		if err != nil {
			return err
		}
		if handler != "vide" && handler != "soun" && handler != "sbtl" && handler != "text" {
			return invalid()
		}
		track.handler = handler
	case "vmhd":
		if len(data) != 12 || !bytes.Equal(data[:4], []byte{0, 0, 0, 1}) || !mediaEditZero(data[4:]) {
			return invalid()
		}
		track.mediaHeader = box.kind
	case "smhd":
		if len(data) != 8 || !mediaEditZero(data) {
			return invalid()
		}
		track.mediaHeader = box.kind
	case "nmhd":
		if len(data) != 4 || !mediaEditZero(data) {
			return invalid()
		}
		track.mediaHeader = box.kind
	case "dref":
		if len(data) != 20 || !mediaEditZero(data[:4]) || binary.BigEndian.Uint32(data[4:8]) != 1 || binary.BigEndian.Uint32(data[8:12]) != 12 || string(data[12:16]) != "url " || !bytes.Equal(data[16:], []byte{0, 0, 0, 1}) {
			return mediaEditContainerError("MP4 data references must be self-contained")
		}
	case "elst":
		if len(data) < 8 || data[0] > 1 || !mediaEditZero(data[1:4]) || binary.BigEndian.Uint32(data[4:8]) != 1 || len(data) != 20+int(data[0])*8 {
			return invalid()
		}
		width := 4 + int(data[0])*4
		if !mediaEditZero(data[8+width:8+2*width]) || !bytes.Equal(data[8+2*width:], []byte{0, 1, 0, 0}) {
			return mediaEditContainerError("MP4 edit list must be a single untrimmed unit-rate interval")
		}
		track.edit = data
	default:
		return invalid()
	}
	return nil
}

func mediaEditZero(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

func mediaEditBigEndian(data []byte) uint64 {
	var result uint64
	for _, b := range data {
		result = result<<8 | uint64(b)
	}
	return result
}

func mediaEditIdentityMatrix(data []byte) bool {
	if len(data) != 36 {
		return false
	}
	for index := 0; index < 9; index++ {
		expected := uint32(0)
		if index == 0 || index == 4 {
			expected = 0x10000
		} else if index == 8 {
			expected = 0x40000000
		}
		if binary.BigEndian.Uint32(data[index*4:]) != expected {
			return false
		}
	}
	return true
}

func mediaEditMP4Handler(data []byte) (string, error) {
	if len(data) < 24 || !mediaEditZero(data[:8]) || !mediaEditZero(data[12:24]) {
		return "", mediaEditContainerError("unproven MP4 handler fields")
	}
	name := data[24:]
	if len(name) > 0 && name[len(name)-1] == 0 {
		name = name[:len(name)-1]
	}
	if !utf8.Valid(name) || bytes.IndexByte(name, 0) >= 0 {
		return "", mediaEditContainerError("ambiguous MP4 handler name")
	}
	return string(data[8:12]), nil
}

func (s *mediaEditContainerScanner) mp4TrackFinish(track *mediaEditMP4Track) error {
	if len(track.header) < 84 || track.codec == "" || track.handler == "" {
		return mediaEditContainerError("incomplete MP4 track")
	}
	base := 24 + int(track.header[0])*12
	fields := track.header[base:]
	group, volume := uint16(0), uint16(0)
	switch track.codec {
	case "avc1":
		if track.handler != "vide" || track.mediaHeader != "vmhd" || binary.BigEndian.Uint32(fields[52:56]) != uint32(track.width)<<16 || binary.BigEndian.Uint32(fields[56:60]) != uint32(track.height)<<16 {
			return mediaEditContainerError("MP4 codec and handler differ")
		}
	case "mp4a":
		group, volume = 1, 0x100
		if track.handler != "soun" || track.mediaHeader != "smhd" || !mediaEditZero(fields[52:60]) {
			return mediaEditContainerError("MP4 codec and handler differ")
		}
	case "tx3g":
		group = 3
		if track.handler != "sbtl" && track.handler != "text" || track.mediaHeader != "nmhd" {
			return mediaEditContainerError("MP4 codec and handler differ")
		}
	}
	actualGroup := binary.BigEndian.Uint16(fields[10:12])
	if !mediaEditZero(fields[:10]) || actualGroup != group || binary.BigEndian.Uint16(fields[12:14]) != volume || !mediaEditZero(fields[14:16]) || !mediaEditIdentityMatrix(fields[16:52]) {
		return mediaEditContainerError("unproven MP4 track grouping, layer, volume or transform")
	}
	if track.edit != nil {
		width := 4 + int(track.edit[0])*4
		if mediaEditBigEndian(track.edit[8:8+width]) != track.duration {
			return mediaEditContainerError("MP4 edit duration differs from its track duration")
		}
	}
	if _, err := s.mp4RollProof(track); err != nil {
		return err
	}
	return nil
}

// FFmpeg reconstructs AAC preroll groups instead of copying opaque sample
// group boxes. The admitted profile is precisely one preceding access unit
// for every sample. Other recovery semantics would not survive that remux.
// See FFmpeg n9.0.1 mov_preroll_write_stbl_atoms (movenc.c:3305).
func (s *mediaEditContainerScanner) mp4SampleGroup(box mediaEditMP4Box, track *mediaEditMP4Track) error {
	if track == nil {
		return mediaEditContainerError("MP4 sample group has no track")
	}
	data, err := s.mp4Bytes(box, 12+65536*8)
	if err != nil {
		return err
	}
	if len(data) < 12 || !mediaEditZero(data[1:4]) || string(data[4:8]) != "roll" {
		return mediaEditContainerError("unsupported MP4 sample group type or flags")
	}
	switch box.kind {
	case "sgpd":
		if track.rollDescriptions || len(data) != 18 || data[0] != 1 || binary.BigEndian.Uint32(data[8:12]) != 2 || binary.BigEndian.Uint32(data[12:16]) != 1 {
			return mediaEditContainerError("unsupported MP4 roll description layout")
		}
		distance := int16(binary.BigEndian.Uint16(data[16:18]))
		if distance != -1 {
			return mediaEditContainerError("MP4 AAC preroll must be exactly one preceding sample")
		}
		track.rollDistance, track.rollDescriptions = &distance, true
	case "sbgp":
		count := uint64(binary.BigEndian.Uint32(data[8:12]))
		if track.rollMapping || data[0] != 0 || count == 0 || count > 65536 || uint64(len(data)-12) != count*8 {
			return mediaEditContainerError("unsupported MP4 roll mapping layout")
		}
		var total uint64
		for offset := 12; offset < len(data); offset += 8 {
			if err := s.ctx.Err(); err != nil {
				return err
			}
			samples := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
			if samples == 0 || samples > uint64(mediaEditMaxPackets)-total || binary.BigEndian.Uint32(data[offset+4:offset+8]) != 1 {
				return mediaEditContainerError("MP4 roll mapping has an unproven group or sample count")
			}
			total += samples
		}
		track.rollSamples, track.rollMapping = total, true
	default:
		return mediaEditContainerError("unknown MP4 sample group box")
	}
	return nil
}

func (s *mediaEditContainerScanner) mp4RollProof(track *mediaEditMP4Track) (mediaEditMP4RollProof, error) {
	if track == nil {
		return mediaEditMP4RollProof{}, mediaEditContainerError("MP4 preroll has no track")
	}
	if track.codec != "mp4a" || track.handler != "soun" {
		if track.rollDescriptions || track.rollMapping || track.rollDistance != nil || track.rollSamples != 0 {
			return mediaEditMP4RollProof{}, mediaEditContainerError("MP4 roll groups are supported only for AAC audio")
		}
		return mediaEditMP4RollProof{}, nil
	}
	if !track.rollDescriptions || !track.rollMapping || track.rollDistance == nil || *track.rollDistance != -1 || track.tableSamples == 0 || track.tableSamples != track.sizeSamples || track.rollSamples != track.tableSamples {
		return mediaEditMP4RollProof{}, mediaEditContainerError("MP4 AAC requires a complete one-sample preroll mapping")
	}
	return mediaEditMP4RollProof{Samples: track.rollSamples, Distance: *track.rollDistance}, nil
}

func (s *mediaEditContainerScanner) mp4Timeline() error {
	// Container duration fields can encode an otherwise invisible trim or gap.
	// Admit only the complete sample-table timeline, rounded upward to the movie
	// clock as the ordinary MP4 muxer does. Packet clocks are proven separately.
	var longest uint64
	ids := map[uint64]bool{}
	for _, track := range s.mp4Tracks {
		idOffset := 12 + int(track.header[0])*8
		id := uint64(binary.BigEndian.Uint32(track.header[idOffset : idOffset+4]))
		if id == 0 || ids[id] || track.mediaScale == 0 || track.tableSamples == 0 || track.tableSamples != track.sizeSamples || track.mediaDuration != track.tableDuration {
			return mediaEditContainerError("MP4 identity, sample count or media duration is ambiguous")
		}
		ids[id] = true
		duration := new(big.Int).Mul(new(big.Int).SetUint64(track.mediaDuration), new(big.Int).SetUint64(s.movieScale))
		duration.Add(duration, new(big.Int).SetUint64(track.mediaScale-1))
		duration.Quo(duration, new(big.Int).SetUint64(track.mediaScale))
		if !duration.IsUint64() || duration.Uint64() != track.duration {
			return mediaEditContainerError("MP4 track duration is not its complete sample timeline")
		}
		longest = max(longest, track.duration)
	}
	if s.movieScale == 0 || s.movieDuration != longest {
		return mediaEditContainerError("MP4 movie duration differs from its complete track timelines")
	}
	for key, value := range s.mp4Projections {
		if tag, present := s.globalMP4Tags[key]; present && tag != value {
			return mediaEditContainerError("MP4 metadata would hide a separate file-type value")
		}
	}
	if value, present := s.globalMP4Tags["creation_time"]; present && s.movieCreation != 0 {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || !parsed.Equal(time.Unix(int64(s.movieCreation)-2082844800, 0)) {
			return mediaEditContainerError("MP4 metadata would hide a separate movie creation time")
		}
	}
	return nil
}

func (s *mediaEditContainerScanner) mp4Table(box mediaEditMP4Box, track *mediaEditMP4Track) error {
	var data [12]byte
	width, prefix := int64(4), int64(8)
	if box.kind == "stsz" {
		prefix = 12
	}
	if box.end-box.start < prefix {
		return mediaEditContainerError("truncated MP4 sample table")
	}
	if err := s.read(box.start, data[:prefix]); err != nil {
		return err
	}
	if !mediaEditZero(data[:4]) && !(box.kind == "ctts" && data[0] == 1 && mediaEditZero(data[1:4])) {
		return mediaEditContainerError("unsupported MP4 sample table version or flags")
	}
	count := uint64(binary.BigEndian.Uint32(data[prefix-4 : prefix]))
	switch box.kind {
	case "stts", "ctts", "co64":
		width = 8
	case "stsc":
		width = 12
	case "stsz":
		track.sizeSamples = count
		if binary.BigEndian.Uint32(data[4:8]) != 0 {
			width = 0
		}
	}
	if count > uint64(mediaEditMaxPackets) || uint64(box.end-box.start-prefix) != count*uint64(width) {
		return mediaEditContainerError("invalid MP4 sample table extent")
	}
	if box.kind == "stsc" {
		previous := uint32(0)
		for offset := box.start + prefix; offset < box.end; offset += 12 {
			if err := s.read(offset, data[:]); err != nil {
				return err
			}
			first := binary.BigEndian.Uint32(data[:4])
			if first <= previous || previous == 0 && first != 1 || binary.BigEndian.Uint32(data[4:8]) == 0 || binary.BigEndian.Uint32(data[8:]) != 1 {
				return mediaEditContainerError("unproven MP4 sample description mapping")
			}
			previous = first
		}
	}
	if box.kind == "stts" {
		for offset := box.start + prefix; offset < box.end; offset += 8 {
			if err := s.read(offset, data[:8]); err != nil {
				return err
			}
			samples := uint64(binary.BigEndian.Uint32(data[:4]))
			delta := uint64(binary.BigEndian.Uint32(data[4:8]))
			if samples == 0 || samples > uint64(mediaEditMaxPackets)-track.tableSamples || delta*samples > math.MaxUint64-track.tableDuration {
				return mediaEditContainerError("invalid MP4 decoding timeline")
			}
			track.tableSamples += samples
			track.tableDuration += samples * delta
		}
	}
	return nil
}

func (s *mediaEditContainerScanner) mp4SampleDescription(box mediaEditMP4Box, depth int, track *mediaEditMP4Track) error {
	var prefix [8]byte
	if box.end-box.start < 8 {
		return mediaEditContainerError("truncated MP4 sample description")
	}
	if err := s.read(box.start, prefix[:]); err != nil {
		return err
	}
	if !mediaEditZero(prefix[:4]) || binary.BigEndian.Uint32(prefix[4:]) != 1 {
		return mediaEditContainerError("MP4 must have exactly one sample description per track")
	}
	entry, err := s.mp4Box(box.start+8, box.end, depth)
	if err != nil {
		return err
	}
	if entry.end != box.end || entry.kind != "avc1" && entry.kind != "mp4a" && entry.kind != "tx3g" {
		return mediaEditContainerError("MP4 sample entry is outside the avc1/mp4a/tx3g profile")
	}
	track.codec = entry.kind
	headerLength := map[string]int64{"avc1": 78, "mp4a": 28, "tx3g": 8}[entry.kind]
	if entry.end-entry.start < headerLength {
		return mediaEditContainerError("truncated MP4 sample entry")
	}
	data := make([]byte, int(headerLength))
	if err := s.read(entry.start, data); err != nil {
		return err
	}
	if !mediaEditZero(data[:6]) || binary.BigEndian.Uint16(data[6:8]) != 1 {
		return mediaEditContainerError("MP4 sample entry uses an external data reference")
	}
	if entry.kind == "tx3g" {
		// FFmpeg exposes the complete remaining tx3g record, including fonts and
		// styles, as codec extradata. Its exact digest is proven independently.
		if entry.end-entry.start < 38 || entry.end-entry.start > 1<<20 {
			return mediaEditContainerError("invalid MP4 timed-text sample entry extent")
		}
		return nil
	}
	if entry.kind == "avc1" {
		track.width = binary.BigEndian.Uint16(data[24:26])
		track.height = binary.BigEndian.Uint16(data[26:28])
		nameLength := int(data[42])
		if !mediaEditZero(data[8:24]) || binary.BigEndian.Uint16(data[24:26]) == 0 || binary.BigEndian.Uint16(data[26:28]) == 0 ||
			binary.BigEndian.Uint32(data[28:32]) != 0x480000 || binary.BigEndian.Uint32(data[32:36]) != 0x480000 ||
			!mediaEditZero(data[36:40]) || binary.BigEndian.Uint16(data[40:42]) != 1 || nameLength > 31 ||
			!mediaEditZero(data[43+min(nameLength, 31):74]) || binary.BigEndian.Uint16(data[74:76]) != 24 || binary.BigEndian.Uint16(data[76:78]) != 0xffff {
			return mediaEditContainerError("unproven MP4 visual sample entry fields")
		}
	} else if !mediaEditZero(data[8:16]) || binary.BigEndian.Uint16(data[16:18]) == 0 || binary.BigEndian.Uint16(data[18:20]) != 16 || !mediaEditZero(data[20:24]) || !mediaEditZero(data[26:28]) {
		return mediaEditContainerError("unproven MP4 audio sample entry fields")
	}
	counts := map[string]int{}
	for offset := entry.start + headerLength; offset < entry.end; {
		child, err := s.mp4Box(offset, entry.end, depth+1)
		if err != nil {
			return err
		}
		offset = child.end
		counts[child.kind]++
		if counts[child.kind] != 1 {
			return mediaEditContainerError("duplicate MP4 sample entry extension")
		}
		size := child.end - child.start
		switch child.kind {
		case "avcC":
			if entry.kind != "avc1" || size < 7 || size > 1<<20 {
				return mediaEditContainerError("invalid MP4 AVC configuration")
			}
		case "esds":
			if entry.kind != "mp4a" {
				return mediaEditContainerError("unexpected MP4 elementary stream descriptor")
			}
			if err := s.mp4ESDS(child); err != nil {
				return err
			}
		case "btrt":
			if size != 12 {
				return mediaEditContainerError("invalid MP4 bitrate descriptor")
			}
		case "pasp":
			if entry.kind != "avc1" || size != 8 {
				return mediaEditContainerError("invalid MP4 pixel aspect ratio")
			}
		case "colr":
			data, err := s.mp4Bytes(child, 11)
			if err != nil {
				return err
			}
			if entry.kind != "avc1" || len(data) != 11 || string(data[:4]) != "nclx" || data[10]&0x7f != 0 {
				return mediaEditContainerError("unproven MP4 colour profile")
			}
		default:
			return mediaEditContainerError(fmt.Sprintf("unproven MP4 sample extension %q", child.kind))
		}
	}
	if entry.kind == "avc1" && counts["avcC"] != 1 || entry.kind == "mp4a" && counts["esds"] != 1 {
		return mediaEditContainerError("MP4 sample entry lacks codec configuration")
	}
	return nil
}

func (s *mediaEditContainerScanner) mp4ESDS(box mediaEditMP4Box) error {
	data, err := s.mp4Bytes(box, 64<<10)
	if err != nil {
		return err
	}
	invalid := func() error { return mediaEditContainerError("unproven MP4 elementary stream descriptor") }
	if len(data) < 4 || !mediaEditZero(data[:4]) {
		return invalid()
	}
	tag, payload, tail, ok := mediaEditMP4Descriptor(data[4:])
	if !ok || tag != 3 || len(tail) != 0 || len(payload) < 3 || payload[2] != 0 {
		return invalid()
	}
	tag, decoder, tail, ok := mediaEditMP4Descriptor(payload[3:])
	if !ok || tag != 4 || len(decoder) < 13 || decoder[0] != 0x40 || decoder[1] != 0x15 {
		return invalid()
	}
	tag, specific, extra, ok := mediaEditMP4Descriptor(decoder[13:])
	if !ok || tag != 5 || len(specific) == 0 || len(extra) != 0 {
		return invalid()
	}
	tag, sl, tail, ok := mediaEditMP4Descriptor(tail)
	if !ok || tag != 6 || !bytes.Equal(sl, []byte{2}) || len(tail) != 0 {
		return invalid()
	}
	return nil
}

func mediaEditMP4Descriptor(data []byte) (byte, []byte, []byte, bool) {
	if len(data) < 2 {
		return 0, nil, nil, false
	}
	size := 0
	for index := 1; index <= 4 && index < len(data); index++ {
		size = size<<7 | int(data[index]&0x7f)
		if data[index]&0x80 == 0 {
			start := index + 1
			if size > len(data)-start {
				break
			}
			return data[0], data[start : start+size], data[start+size:], true
		}
	}
	return 0, nil, nil, false
}

func (s *mediaEditContainerScanner) mp4Metadata(box mediaEditMP4Box, depth int) error {
	var fullbox [4]byte
	if box.end-box.start < 4 {
		return mediaEditContainerError("truncated MP4 metadata box")
	}
	if err := s.read(box.start, fullbox[:]); err != nil {
		return err
	}
	if !mediaEditZero(fullbox[:]) {
		return mediaEditContainerError("unsupported MP4 metadata version")
	}
	handler := ""
	return s.mp4MetadataChildren(box.start+4, box.end, depth, &handler)
}

func (s *mediaEditContainerScanner) mp4MetadataChildren(start, end int64, depth int, handler *string) error {
	keys := []string{""}
	counts := map[string]int{}
	for offset := start; offset < end; {
		box, err := s.mp4Box(offset, end, depth)
		if err != nil {
			return err
		}
		offset = box.end
		counts[box.kind]++
		if counts[box.kind] != 1 {
			return mediaEditContainerError("duplicate MP4 metadata structure")
		}
		if err := s.charge(box.end - box.start); err != nil {
			return err
		}
		switch box.kind {
		case "hdlr":
			data, err := s.mp4Bytes(box, 4096)
			if err != nil {
				return err
			}
			// Apple's mdir handler convention uses appl in the first reserved word.
			if len(data) >= 24 && string(data[8:12]) == "mdir" && string(data[12:16]) == "appl" {
				clear(data[12:16])
			}
			value, err := mediaEditMP4Handler(data)
			if err != nil {
				return err
			}
			if value != "mdir" && value != "mdta" {
				return mediaEditContainerError("unsupported MP4 metadata handler")
			}
			*handler = value
		case "keys":
			if *handler != "mdta" || counts["ilst"] != 0 {
				return mediaEditContainerError("MP4 metadata keys have no preceding mdta handler")
			}
			keys, err = s.mp4MetadataKeys(box)
			if err != nil {
				return err
			}
		case "ilst":
			if *handler == "" || *handler == "mdta" && counts["keys"] != 1 {
				return mediaEditContainerError("MP4 metadata values precede their schema")
			}
			if err := s.mp4MetadataValues(box, depth+1, *handler, keys); err != nil {
				return err
			}
		default:
			return mediaEditContainerError(fmt.Sprintf("unproven MP4 metadata box %q", box.kind))
		}
	}
	if counts["hdlr"] != 1 || counts["ilst"] != 1 {
		return mediaEditContainerError("incomplete MP4 metadata structure")
	}
	return nil
}

func (s *mediaEditContainerScanner) mp4MetadataKeys(box mediaEditMP4Box) ([]string, error) {
	data, err := s.mp4Bytes(box, 1<<20)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 || !mediaEditZero(data[:4]) || binary.BigEndian.Uint32(data[4:8]) > 4096 {
		return nil, mediaEditContainerError("invalid MP4 metadata key table")
	}
	count := int(binary.BigEndian.Uint32(data[4:8]))
	keys, seen := []string{""}, map[string]bool{}
	data = data[8:]
	for index := 0; index < count; index++ {
		if len(data) < 8 {
			return nil, mediaEditContainerError("truncated MP4 metadata key")
		}
		size := int64(binary.BigEndian.Uint32(data[:4]))
		if size <= 8 || size > int64(len(data)) || size > 4104 || string(data[4:8]) != "mdta" {
			return nil, mediaEditContainerError("unproven MP4 metadata key namespace")
		}
		key := string(data[8:size])
		if !utf8.ValidString(key) || strings.ContainsRune(key, 0) || seen[strings.ToLower(key)] {
			return nil, mediaEditContainerError("ambiguous MP4 metadata key")
		}
		seen[strings.ToLower(key)] = true
		keys = append(keys, key)
		data = data[size:]
	}
	if len(data) != 0 {
		return nil, mediaEditContainerError("trailing MP4 metadata key bytes")
	}
	return keys, nil
}

func (s *mediaEditContainerScanner) mp4MetadataValues(box mediaEditMP4Box, depth int, handler string, keys []string) error {
	known := map[string]string{"\xa9nam": "title", "\xa9ART": "artist", "aART": "album_artist", "\xa9alb": "album", "\xa9cmt": "comment",
		"\xa9day": "date", "\xa9too": "encoder", "\xa9gen": "genre", "cprt": "copyright", "desc": "description", "ldes": "synopsis", "\xa9grp": "grouping", "\xa9wrt": "composer"}
	if s.globalMP4Tags == nil {
		s.globalMP4Tags = map[string]string{}
	}
	for offset := box.start; offset < box.end; {
		item, err := s.mp4Box(offset, box.end, depth)
		if err != nil {
			return err
		}
		offset = item.end
		key := known[item.kind]
		if handler == "mdta" {
			index := binary.BigEndian.Uint32([]byte(item.kind))
			if index == 0 || uint64(index) >= uint64(len(keys)) {
				return mediaEditContainerError("MP4 metadata references an absent key")
			}
			key = keys[index]
		}
		key = strings.ToLower(key)
		_, duplicate := s.globalMP4Tags[key]
		if key == "" || duplicate {
			return mediaEditContainerError("unknown or duplicate MP4 metadata value")
		}
		value, err := s.mp4Box(item.start, item.end, depth+1)
		if err != nil {
			return err
		}
		data, err := s.mp4Bytes(value, 1<<20)
		if err != nil {
			return err
		}
		if value.kind != "data" || value.end != item.end || len(data) < 8 || binary.BigEndian.Uint32(data[:4]) != 1 || !mediaEditZero(data[4:8]) || !utf8.Valid(data[8:]) || bytes.IndexByte(data[8:], 0) >= 0 {
			return mediaEditContainerError("MP4 metadata must be one unambiguous UTF-8 value per key")
		}
		s.globalMP4Tags[key] = string(data[8:])
	}
	return nil
}
