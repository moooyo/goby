package media

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	MaxExternalBitmapSubtitleBytes int64 = 256 << 20
	maxExternalSubtitleIndexBytes  int64 = 4 << 20
	maxExternalSubtitleSegments          = 400000
)

// ExternalBitmapSubtitleTrack identifies one logical track within a held SUP
// file or IDX/SUB pair. SourceStreamIndex is the demux ordinal, not the IDX ID.
type ExternalBitmapSubtitleTrack struct {
	SourceStreamIndex int
	Codec             string
	Language          string
}

// ExternalSubtitleTimelineInput borrows descriptors already authorized by the
// library layer. For DVD, Input is IDX and Companion is its exact SUB partner.
type ExternalSubtitleTimelineInput struct {
	StreamIndex       int
	SourceStreamIndex int
	Codec             string
	Input             *os.File
	Companion         *os.File
}

type externalSubtitleDocument struct {
	tracks []externalSubtitleTrack
	extra  []byte
}

type externalSubtitleTrack struct {
	ExternalBitmapSubtitleTrack
	id      int
	entries []externalSubtitleIndexEntry
	packets []bitmapSubtitlePacket
}

type externalSubtitleIndexEntry struct {
	pts int64
	pos int64
}

// InspectExternalBitmapSubtitles reads only structure and track metadata. It
// never rasterizes subtitles, follows IDX path directives, or opens filenames.
// PTS and IDX delay retain the subtitle's absolute presentation clock.
func InspectExternalBitmapSubtitles(ctx context.Context, format string, input, companion *os.File) ([]ExternalBitmapSubtitleTrack, error) {
	document, err := readExternalBitmapSubtitles(ctx, format, input, companion, -1)
	if err != nil {
		return nil, err
	}
	tracks := make([]ExternalBitmapSubtitleTrack, len(document.tracks))
	for index, track := range document.tracks {
		tracks[index] = track.ExternalBitmapSubtitleTrack
	}
	return tracks, nil
}

// retainOrdinal=-1 performs structural inspection without keeping SPU/PGS
// payloads. A generation call retains packets for exactly one admitted track.
func readExternalBitmapSubtitles(ctx context.Context, format string, input, companion *os.File, retainOrdinal int) (document externalSubtitleDocument, resultErr error) {
	if ctx == nil || input == nil || (format != "sup" && format != "vobsub") ||
		(format == "sup" && companion != nil) || (format == "vobsub" && companion == nil) {
		return document, fmt.Errorf("%w: invalid external subtitle descriptors or format", ErrBitmapSubtitle)
	}
	if err := ctx.Err(); err != nil {
		return document, err
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxExternalBitmapSubtitleBytes {
		return document, fmt.Errorf("%w: external subtitle input limit or invalid file", ErrAnalysisBudget)
	}
	var paired os.FileInfo
	if companion != nil {
		paired, err = companion.Stat()
		if err != nil || !paired.Mode().IsRegular() || paired.Size() <= 0 || paired.Size() > MaxExternalBitmapSubtitleBytes-before.Size() || os.SameFile(before, paired) {
			return document, fmt.Errorf("%w: external subtitle companion limit or invalid pair", ErrAnalysisBudget)
		}
	}
	defer func() {
		if err := ctx.Err(); err != nil {
			document, resultErr = externalSubtitleDocument{}, errors.Join(resultErr, err)
		} else if !subtitleFileUnchanged(input, before) || (companion != nil && !subtitleFileUnchanged(companion, paired)) {
			document, resultErr = externalSubtitleDocument{}, errors.Join(resultErr, fmt.Errorf("%w: external subtitle changed", ErrBitmapSubtitle))
		}
	}()
	if format == "sup" {
		return readExternalSUP(ctx, io.NewSectionReader(input, 0, before.Size()), before.Size(), retainOrdinal)
	}
	if before.Size() > maxExternalSubtitleIndexBytes {
		return document, fmt.Errorf("%w: VobSub index byte limit", ErrAnalysisBudget)
	}
	document, err = readExternalVobSubIndex(ctx, io.NewSectionReader(input, 0, before.Size()), paired.Size())
	if err != nil {
		return externalSubtitleDocument{}, err
	}
	if retainOrdinal >= len(document.tracks) {
		return externalSubtitleDocument{}, fmt.Errorf("%w: external subtitle ordinal changed", ErrBitmapSubtitle)
	}
	var positions []int64
	for _, track := range document.tracks {
		for _, entry := range track.entries {
			positions = append(positions, entry.pos)
		}
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i] < positions[j] })
	ends := make(map[int64]int64, len(positions))
	for index, pos := range positions {
		end := paired.Size()
		if index+1 < len(positions) {
			end = positions[index+1]
		}
		if end <= pos {
			return externalSubtitleDocument{}, fmt.Errorf("%w: duplicate VobSub file position", ErrBitmapSubtitle)
		}
		ends[pos] = end
	}
	budget := 0
	for ordinal := range document.tracks {
		// Inventory validates every language. Generation reads just its chosen
		// language's disjoint indexed ranges, so a 32-language pair is not
		// scanned 32 times while the caller generates all admitted tracks.
		if retainOrdinal >= 0 && ordinal != retainOrdinal {
			continue
		}
		track := &document.tracks[ordinal]
		for _, entry := range track.entries {
			data, err := readExternalVobSubSPU(ctx, io.NewSectionReader(companion, entry.pos, ends[entry.pos]-entry.pos), track.id, &budget)
			if err != nil {
				return externalSubtitleDocument{}, fmt.Errorf("%w: VobSub track %d: %w", ErrBitmapSubtitle, ordinal, err)
			}
			if ordinal == retainOrdinal {
				track.packets = append(track.packets, bitmapSubtitlePacket{PTS: entry.pts, Data: data})
			}
		}
	}
	return document, nil
}

func readExternalSUP(ctx context.Context, reader io.Reader, size int64, retainOrdinal int) (externalSubtitleDocument, error) {
	document := externalSubtitleDocument{tracks: []externalSubtitleTrack{{ExternalBitmapSubtitleTrack: ExternalBitmapSubtitleTrack{SourceStreamIndex: 0, Codec: "hdmv_pgs_subtitle"}}}}
	if retainOrdinal > 0 {
		return externalSubtitleDocument{}, fmt.Errorf("%w: SUP has only one track", ErrBitmapSubtitle)
	}
	reader = bufio.NewReaderSize(reader, 64<<10)
	var packet bitmapSubtitlePacket
	var lastRaw uint32
	var epoch uint64
	var lastPTS int64 = -1
	var offset int64
	segments, displays := 0, 0
	inDisplay := false
	for offset < size {
		if err := ctx.Err(); err != nil {
			return externalSubtitleDocument{}, err
		}
		var header [13]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil || string(header[:2]) != "PG" {
			return externalSubtitleDocument{}, fmt.Errorf("%w: truncated or invalid SUP segment", ErrBitmapSubtitle)
		}
		length := int(binary.BigEndian.Uint16(header[11:13]))
		offset += 13 + int64(length)
		segments++
		if offset > size || segments > maxExternalSubtitleSegments {
			return externalSubtitleDocument{}, fmt.Errorf("%w: SUP segment limit or truncated payload", ErrBitmapSubtitle)
		}
		kind := header[10]
		if kind == pgsPresentationSegment {
			if inDisplay || length < 11 || displays >= maxBitmapSubtitlePackets {
				return externalSubtitleDocument{}, fmt.Errorf("%w: incomplete or excessive SUP display sets", ErrBitmapSubtitle)
			}
			raw := binary.BigEndian.Uint32(header[2:6])
			if displays > 0 && raw < lastRaw && lastRaw-raw > 1<<31 {
				epoch += 1 << 32
			}
			pts := int64(epoch+uint64(raw)) * TicksPerSecond / 90000
			if pts < lastPTS || pts >= subtitleTimelineDurationLimit {
				return externalSubtitleDocument{}, fmt.Errorf("%w: invalid SUP presentation clock", ErrBitmapSubtitle)
			}
			lastRaw, lastPTS = raw, pts
			packet = bitmapSubtitlePacket{PTS: pts}
			inDisplay, displays = true, displays+1
		} else if !inDisplay {
			return externalSubtitleDocument{}, fmt.Errorf("%w: SUP segment outside display set", ErrBitmapSubtitle)
		}
		switch kind {
		case pgsPresentationSegment:
		case pgsPaletteSegment:
			if length < 2 || (length-2)%5 != 0 {
				return externalSubtitleDocument{}, fmt.Errorf("%w: invalid SUP palette envelope", ErrBitmapSubtitle)
			}
		case pgsObjectSegment:
			if length < 4 {
				return externalSubtitleDocument{}, fmt.Errorf("%w: invalid SUP object envelope", ErrBitmapSubtitle)
			}
		case pgsWindowSegment:
			if length < 1 || (length-1)%9 != 0 {
				return externalSubtitleDocument{}, fmt.Errorf("%w: invalid SUP window envelope", ErrBitmapSubtitle)
			}
		case pgsEndSegment:
			if length != 0 {
				return externalSubtitleDocument{}, fmt.Errorf("%w: nonempty SUP end segment", ErrBitmapSubtitle)
			}
		default:
			return externalSubtitleDocument{}, fmt.Errorf("%w: unknown SUP segment", ErrSubtitleTimelineUnsupported)
		}
		if retainOrdinal == 0 {
			start := len(packet.Data)
			packet.Data = append(packet.Data, header[10:]...)
			packet.Data = append(packet.Data, make([]byte, length)...)
			if _, err := io.ReadFull(reader, packet.Data[start+3:]); err != nil {
				return externalSubtitleDocument{}, err
			}
		} else if _, err := io.CopyN(io.Discard, reader, int64(length)); err != nil {
			return externalSubtitleDocument{}, err
		}
		if kind == pgsEndSegment {
			inDisplay = false
			if retainOrdinal == 0 {
				document.tracks[0].packets = append(document.tracks[0].packets, packet)
			}
		}
	}
	if inDisplay || displays == 0 {
		return externalSubtitleDocument{}, fmt.Errorf("%w: empty or incomplete SUP display inventory", ErrBitmapSubtitle)
	}
	return document, nil
}

func externalSubtitleClock(value string, signed bool) (int64, error) {
	sign := int64(1)
	if signed && len(value) > 0 && (value[0] == '-' || value[0] == '+') {
		if value[0] == '-' {
			sign = -1
		}
		value = value[1:]
	}
	parts := strings.Split(value, ":")
	if len(parts) != 4 {
		return 0, fmt.Errorf("invalid subtitle timestamp")
	}
	var numbers [4]int64
	for index, part := range parts {
		if len(part) == 0 || len(part) > 3 {
			return 0, fmt.Errorf("invalid subtitle timestamp")
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return 0, fmt.Errorf("invalid subtitle timestamp")
			}
		}
		numbers[index], _ = strconv.ParseInt(part, 10, 32)
	}
	if numbers[1] > 59 || numbers[2] > 59 || numbers[3] > 999 {
		return 0, fmt.Errorf("invalid subtitle timestamp")
	}
	ticks := ((numbers[0]*3600+numbers[1]*60+numbers[2])*1000 + numbers[3]) * 10000
	if ticks >= subtitleTimelineDurationLimit {
		return 0, fmt.Errorf("subtitle timestamp out of range")
	}
	return ticks * sign, nil
}

func readExternalVobSubIndex(ctx context.Context, reader io.Reader, subSize int64) (externalSubtitleDocument, error) {
	var document externalSubtitleDocument
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	lineNumber, entryCount, current := 0, 0, -1
	delay := int64(0)
	ids := make(map[int]bool)
	var palette string
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return externalSubtitleDocument{}, err
		}
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if lineNumber == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
			if !strings.HasPrefix(line, "# VobSub index file, v7") && !strings.HasPrefix(line, "# VobSub index file, v6") {
				return externalSubtitleDocument{}, fmt.Errorf("%w: invalid VobSub index signature", ErrBitmapSubtitle)
			}
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return externalSubtitleDocument{}, fmt.Errorf("%w: malformed VobSub directive", ErrBitmapSubtitle)
		}
		value = strings.TrimSpace(value)
		switch key {
		case "id":
			language, indexValue, ok := strings.Cut(value, ", index:")
			language = strings.ToLower(strings.TrimSpace(language))
			id, err := strconv.Atoi(strings.TrimSpace(indexValue))
			if !ok || err != nil || id < 0 || id > 31 || ids[id] || len(language) < 2 || len(language) > 3 {
				return externalSubtitleDocument{}, fmt.Errorf("%w: invalid or duplicate VobSub language index", ErrBitmapSubtitle)
			}
			for _, char := range language {
				if char < 'a' || char > 'z' {
					return externalSubtitleDocument{}, fmt.Errorf("%w: invalid VobSub language", ErrBitmapSubtitle)
				}
			}
			ids[id] = true
			current = len(document.tracks)
			document.tracks = append(document.tracks, externalSubtitleTrack{ExternalBitmapSubtitleTrack: ExternalBitmapSubtitleTrack{SourceStreamIndex: current, Codec: "dvd_subtitle", Language: language}, id: id})
		case "timestamp":
			stamp, position, ok := strings.Cut(value, ", filepos:")
			pts, timeErr := externalSubtitleClock(strings.TrimSpace(stamp), false)
			position = strings.TrimSpace(position)
			pos, posErr := strconv.ParseUint(position, 16, 63)
			if !ok || current < 0 || timeErr != nil || posErr != nil || len(position) > 16 || pos >= uint64(subSize) || pts+delay <= -subtitleTimelineDurationLimit || pts+delay >= subtitleTimelineDurationLimit {
				return externalSubtitleDocument{}, fmt.Errorf("%w: invalid VobSub timestamp or file position", ErrBitmapSubtitle)
			}
			track := &document.tracks[current]
			if len(track.entries) > 0 {
				previous := track.entries[len(track.entries)-1]
				if int64(pos) <= previous.pos || pts+delay < previous.pts {
					return externalSubtitleDocument{}, fmt.Errorf("%w: VobSub entries move backwards", ErrBitmapSubtitle)
				}
			}
			entryCount++
			if entryCount > maxBitmapSubtitlePackets {
				return externalSubtitleDocument{}, fmt.Errorf("%w: VobSub entry count limit", ErrAnalysisBudget)
			}
			track.entries = append(track.entries, externalSubtitleIndexEntry{pts: pts + delay, pos: int64(pos)})
		case "delay":
			var err error
			delay, err = externalSubtitleClock(value, true)
			if err != nil {
				return externalSubtitleDocument{}, fmt.Errorf("%w: invalid VobSub delay", ErrBitmapSubtitle)
			}
		case "palette":
			if palette != "" {
				return externalSubtitleDocument{}, fmt.Errorf("%w: duplicate VobSub palette", ErrBitmapSubtitle)
			}
			palette = "palette: " + value + "\n"
			if _, _, err := dvdSubtitlePalette([]byte(palette)); err != nil {
				return externalSubtitleDocument{}, fmt.Errorf("%w: %w", ErrBitmapSubtitle, err)
			}
		case "alpha":
			if value != "100%" {
				return externalSubtitleDocument{}, fmt.Errorf("%w: VobSub global alpha", ErrSubtitleTimelineUnsupported)
			}
		case "forced subs":
			if !strings.EqualFold(value, "OFF") {
				return externalSubtitleDocument{}, fmt.Errorf("%w: VobSub forced-only selection", ErrSubtitleTimelineUnsupported)
			}
		case "custom colors":
			if !strings.HasPrefix(strings.ToUpper(value), "OFF,") {
				return externalSubtitleDocument{}, fmt.Errorf("%w: VobSub custom colors", ErrSubtitleTimelineUnsupported)
			}
		case "time offset":
			if value != "0" {
				return externalSubtitleDocument{}, fmt.Errorf("%w: VobSub time offset directive", ErrSubtitleTimelineUnsupported)
			}
		case "size", "org", "scale", "smooth", "fadein/out", "align", "langidx", "alt":
			// These presentation/default-selection hints do not change the
			// authored display clock. They never select files or commands.
		default:
			return externalSubtitleDocument{}, fmt.Errorf("%w: unknown VobSub directive %q", ErrSubtitleTimelineUnsupported, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return externalSubtitleDocument{}, fmt.Errorf("%w: VobSub index line limit or read failure: %w", ErrBitmapSubtitle, err)
	}
	if lineNumber == 0 || len(document.tracks) == 0 || entryCount == 0 {
		return externalSubtitleDocument{}, fmt.Errorf("%w: empty VobSub track inventory", ErrBitmapSubtitle)
	}
	for _, track := range document.tracks {
		if len(track.entries) == 0 {
			return externalSubtitleDocument{}, fmt.Errorf("%w: empty VobSub language track", ErrBitmapSubtitle)
		}
	}
	document.extra = []byte(palette)
	return document, nil
}

// VobSub wraps DVD SPU fragments in MPEG program-stream private_stream_1 PES
// packets. Only the authorized index range is visited; unknown payload types,
// mismatched substream IDs, and fragments crossing another IDX entry fail.
func readExternalVobSubSPU(ctx context.Context, input io.Reader, id int, totalPackets *int) ([]byte, error) {
	reader := bufio.NewReaderSize(input, 4096)
	var spu []byte
	expected := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var header [6]byte
		n, err := io.ReadFull(reader, header[:4])
		if err == io.EOF && n == 0 {
			break
		}
		if err != nil || header[0] != 0 || header[1] != 0 || header[2] != 1 {
			return nil, fmt.Errorf("invalid MPEG start code")
		}
		*totalPackets++
		if *totalPackets > maxExternalSubtitleSegments {
			return nil, fmt.Errorf("%w: MPEG packet count limit", ErrAnalysisBudget)
		}
		if header[3] == 0xb9 {
			continue
		}
		if header[3] == 0xba {
			first, err := reader.ReadByte()
			if err != nil {
				return nil, err
			}
			var rest [9]byte
			if first&0xc0 == 0x40 {
				if _, err := io.ReadFull(reader, rest[:]); err != nil {
					return nil, err
				}
				if first&4 == 0 || rest[1]&4 == 0 || rest[3]&4 == 0 || rest[4]&1 == 0 || rest[7]&3 != 3 {
					return nil, fmt.Errorf("invalid MPEG-2 pack markers")
				}
				if _, err := io.CopyN(io.Discard, reader, int64(rest[8]&7)); err != nil {
					return nil, err
				}
			} else if first&0xf0 == 0x20 {
				if _, err := io.ReadFull(reader, rest[:7]); err != nil {
					return nil, err
				}
				if first&1 == 0 || rest[1]&1 == 0 || rest[3]&1 == 0 || rest[4]&0x80 == 0 || rest[6]&1 == 0 {
					return nil, fmt.Errorf("invalid MPEG-1 pack markers")
				}
			} else {
				return nil, fmt.Errorf("unsupported MPEG pack header")
			}
			continue
		}
		if _, err := io.ReadFull(reader, header[4:6]); err != nil {
			return nil, err
		}
		length := int(binary.BigEndian.Uint16(header[4:6]))
		if length == 0 {
			return nil, fmt.Errorf("empty MPEG packet")
		}
		if header[3] == 0xbb || header[3] == 0xbe || header[3] == 0xbf {
			if _, err := io.CopyN(io.Discard, reader, int64(length)); err != nil {
				return nil, err
			}
			continue
		}
		if header[3] != 0xbd {
			return nil, fmt.Errorf("unexpected non-subtitle MPEG stream")
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		data, err := externalVobSubPESPayload(payload)
		if err != nil {
			return nil, err
		}
		if len(data) < 2 || data[0] != byte(0x20+id) {
			return nil, fmt.Errorf("IDX/SUB substream mismatch")
		}
		if expected != 0 && len(spu) == expected {
			return nil, fmt.Errorf("multiple DVD SPU packets for one IDX entry")
		}
		if len(spu)+len(data)-1 > 65535 {
			return nil, fmt.Errorf("DVD SPU byte limit")
		}
		spu = append(spu, data[1:]...)
		if len(spu) >= 4 && expected == 0 {
			expected = int(binary.BigEndian.Uint16(spu[:2]))
			control := int(binary.BigEndian.Uint16(spu[2:4]))
			if expected < 10 || control < 4 || control > expected-6 {
				return nil, fmt.Errorf("invalid DVD SPU envelope")
			}
		}
		if expected != 0 && len(spu) > expected {
			return nil, fmt.Errorf("DVD SPU exceeds declared length")
		}
	}
	if expected == 0 || len(spu) != expected {
		return nil, fmt.Errorf("truncated DVD SPU or mismatched IDX/SUB pair")
	}
	if err := inspectExternalDVDControls(ctx, spu); err != nil {
		return nil, err
	}
	return spu, nil
}

// Check command envelopes and pointers without reading RLE pixels. The display
// decoder repeats these checks when it needs geometry and actual visibility.
func inspectExternalDVDControls(ctx context.Context, data []byte) error {
	control := int(binary.BigEndian.Uint16(data[2:4]))
	position, lastDate := control, -1
	for sequences := 0; ; sequences++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if sequences >= 4096 || position < control || position > len(data)-5 {
			return fmt.Errorf("invalid DVD control chain")
		}
		date := int(binary.BigEndian.Uint16(data[position : position+2]))
		next := int(binary.BigEndian.Uint16(data[position+2 : position+4]))
		if date < lastDate || (next != position && (next <= position || next > len(data)-5)) {
			return fmt.Errorf("invalid DVD control clock or next pointer")
		}
		lastDate = date
		end := len(data)
		if next != position {
			end = next
		}
		cursor, terminated := position+4, false
		for cursor < end {
			command := data[cursor]
			cursor++
			length := 0
			switch command {
			case 0x00, 0x01, 0x02:
			case 0x03, 0x04:
				length = 2
			case 0x05:
				length = 6
			case 0x06:
				length = 4
			case 0x07:
				if end-cursor < 6 {
					return fmt.Errorf("truncated DVD color-change command")
				}
				length = int(binary.BigEndian.Uint16(data[cursor : cursor+2]))
				if length < 6 || length > end-cursor {
					return fmt.Errorf("invalid DVD color-change command length")
				}
				return fmt.Errorf("%w: DVD color-change command", ErrSubtitleTimelineUnsupported)
			case 0xff:
				terminated = true
			default:
				return fmt.Errorf("invalid DVD control command 0x%02x", command)
			}
			if length > end-cursor {
				return fmt.Errorf("truncated DVD control command")
			}
			if command == 6 {
				first, second := int(binary.BigEndian.Uint16(data[cursor:cursor+2])), int(binary.BigEndian.Uint16(data[cursor+2:cursor+4]))
				if first < 4 || second < 4 || first >= control || second >= control {
					return fmt.Errorf("invalid DVD image offset")
				}
			}
			cursor += length
			if terminated {
				break
			}
		}
		if !terminated {
			return fmt.Errorf("unterminated DVD control sequence")
		}
		if next == position {
			return nil
		}
		position = next
	}
}

func externalVobSubPESPayload(payload []byte) ([]byte, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("empty MPEG PES header")
	}
	if payload[0]&0xc0 == 0x80 {
		if len(payload) < 3 || payload[0]&0x30 != 0 || payload[1]&0xc0 == 0x40 {
			return nil, fmt.Errorf("invalid or scrambled MPEG-2 PES header")
		}
		length := int(payload[2])
		if length > len(payload)-3 {
			return nil, fmt.Errorf("truncated MPEG-2 PES header")
		}
		ptsMode := payload[1] >> 6
		if ptsMode == 2 && length < 5 || ptsMode == 3 && length < 10 {
			return nil, fmt.Errorf("truncated MPEG PES timestamp")
		}
		if ptsMode >= 2 && !externalPESTimestampValid(payload[3:8], ptsMode) {
			return nil, fmt.Errorf("invalid MPEG PES timestamp")
		}
		if ptsMode == 3 && !externalPESTimestampValid(payload[8:13], 1) {
			return nil, fmt.Errorf("invalid MPEG PES decode timestamp")
		}
		return payload[3+length:], nil
	}
	position := 0
	for position < len(payload) && payload[position] == 0xff {
		position++
	}
	if position < len(payload) && payload[position]&0xc0 == 0x40 {
		position += 2
	}
	if position >= len(payload) {
		return nil, fmt.Errorf("truncated MPEG-1 PES header")
	}
	mode := payload[position] >> 4
	if mode == 2 || mode == 3 {
		length := 5
		if mode == 3 {
			length = 10
		}
		if length > len(payload)-position || !externalPESTimestampValid(payload[position:position+5], mode) {
			return nil, fmt.Errorf("invalid MPEG-1 PES timestamp")
		}
		if mode == 3 && !externalPESTimestampValid(payload[position+5:position+10], 1) {
			return nil, fmt.Errorf("invalid MPEG-1 PES decode timestamp")
		}
		position += length
	} else if payload[position] == 0x0f {
		position++
	} else {
		return nil, fmt.Errorf("unsupported MPEG PES header")
	}
	return payload[position:], nil
}

func externalPESTimestampValid(value []byte, prefix byte) bool {
	return len(value) == 5 && value[0]>>4 == prefix && value[0]&1 == 1 && value[2]&1 == 1 && value[4]&1 == 1
}

func walkExternalBitmapSubtitles(ctx context.Context, input ExternalSubtitleTimelineInput, duration int64, emit func(BitmapSubtitleCue) error) ([]string, error) {
	format := "sup"
	if input.Codec == "dvd_subtitle" {
		format = "vobsub"
	} else if input.Codec != "hdmv_pgs_subtitle" {
		return nil, ErrSubtitleTimelineUnsupported
	}
	document, err := readExternalBitmapSubtitles(ctx, format, input.Input, input.Companion, input.SourceStreamIndex)
	if err != nil {
		return nil, err
	}
	if input.SourceStreamIndex < 0 || input.SourceStreamIndex >= len(document.tracks) {
		return nil, fmt.Errorf("%w: external stream ordinal", ErrBitmapSubtitle)
	}
	track := document.tracks[input.SourceStreamIndex]
	count, clipped := 0, false
	consume := func(cue BitmapSubtitleCue) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if cue.Image == nil || cue.Image.Bounds().Empty() || cue.StartTicks >= cue.EndTicks {
			return fmt.Errorf("%w: invalid external subtitle cue", ErrBitmapSubtitle)
		}
		if cue.EndTicks <= 0 || cue.StartTicks >= duration {
			clipped = true
			return nil
		}
		if cue.StartTicks < 0 {
			cue.StartTicks = 0
			clipped = true
		}
		if cue.EndTicks > duration {
			cue.EndTicks = duration
			clipped = true
		}
		count++
		return emit(cue)
	}
	limits := bitmapSubtitleLimits(duration)
	limits.MaxPacketBytes = int(MaxExternalBitmapSubtitleBytes)
	limits.MaxWorkPixels = 1_000_000_000
	// External PGS clear events may legitimately fall just outside the media
	// presentation. Decode them on their own clock, then clip actual coverage.
	for _, packet := range track.packets {
		limits.DurationTicks = max(limits.DurationTicks, packet.PTS+1)
	}
	var warnings []string
	if format == "sup" {
		warnings, err = walkPGSSubtitles(ctx, track.packets, limits, consume)
	} else {
		warnings, err = walkDVDSubtitles(ctx, track.packets, document.extra, limits, consume)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBitmapSubtitle, err)
	}
	if count == 0 {
		return nil, fmt.Errorf("%w: external subtitle has no visible coverage in the source presentation", ErrBitmapSubtitle)
	}
	if clipped {
		warnings = append(warnings, "cue_intervals_clipped_to_source_presentation")
	}
	return warnings, nil
}
