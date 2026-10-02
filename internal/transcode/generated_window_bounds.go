package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	maxGeneratedBoundsInputBytes  = 64 << 20
	maxGeneratedBoundsOutputBytes = 128 << 20
	maxGeneratedBoundsRecordBytes = 16 << 10
	maxGeneratedBoundsPackets     = 1_000_000
	maxGeneratedBoundsIntervals   = 64
)

// GeneratedRational represents seconds per integral media clock unit.
type GeneratedRational struct {
	Num int64
	Den int64
}

// GeneratedTrackBounds contains actual packet clocks, without converting them
// to floating point or guessing missing durations. FirstPTS is the earliest
// presentation timestamp; LastPTS is the greatest presentation timestamp.
// Decode timestamps describe demux order. SkipSamples and DiscardPadding are
// sample counts; they have not been subtracted from the raw packet envelope.
// Gap fields retain the largest permitted one-clock-unit rounding difference.
type GeneratedTrackBounds struct {
	Present             bool
	Kind                string
	Codec               string
	TimeBase            GeneratedRational
	SampleRate          int64
	FirstPTS            int64
	LastPTS             int64
	EndPTS              int64
	FirstPacketPTS      int64
	LastPacketPTS       int64
	FirstPacketDuration int64
	LastPacketDuration  int64
	// Durations use this track's native integral TimeBase units. Alignment
	// means every observed packet has PTS == DTS, including middle packets.
	MinPacketDuration         int64
	MaxPacketDuration         int64
	PresentationDecodeAligned bool
	FirstFlags                string
	LastFlags                 string
	FirstDTS                  int64
	LastDTS                   int64
	EndDTS                    int64
	PacketCount               int64
	FirstKey                  bool
	SkipSamples               int64
	DiscardPadding            int64
	HasDiscard                bool
	HasCorrupt                bool
	MaxPresentationGapTicks   int64
	MaxDecodeGapTicks         int64
	TotalPresentationGapTicks int64
	TotalDecodeGapTicks       int64
}

// GeneratedSegmentBounds binds the observed packet envelope to the complete
// bytes supplied to the probe. A missing initialization has the zero digest.
// This is packet evidence, not a proof that a demuxer recognized every byte,
// that a source interval was fully represented, or that a fragment can restart.
// Callers separately prove container framing, source coverage and restart safety.
type GeneratedSegmentBounds struct {
	Video                GeneratedTrackBounds
	Audio                GeneratedTrackBounds
	InitializationSHA256 [32]byte
	SegmentSHA256        [32]byte
}

type generatedBoundsBudget struct {
	mu     sync.Mutex
	bytes  int
	err    error
	cancel context.CancelFunc
}

func (b *generatedBoundsBudget) add(length int) error {
	b.mu.Lock()
	if b.err != nil {
		err := b.err
		b.mu.Unlock()
		return err
	}
	if length > maxGeneratedBoundsOutputBytes-b.bytes {
		b.err = ErrTimelineLimit
		b.mu.Unlock()
		b.cancel()
		return ErrTimelineLimit
	}
	b.bytes += length
	b.mu.Unlock()
	return nil
}

func (b *generatedBoundsBudget) fail(err error) error {
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	err = b.err
	b.mu.Unlock()
	b.cancel()
	return err
}

func (b *generatedBoundsBudget) failure() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

type generatedBoundsInterval struct{ start, end int64 }

type generatedBoundsTrack struct {
	index       int64
	bounds      GeneratedTrackBounds
	streamSeen  bool
	minDuration int64
	intervals   [maxGeneratedBoundsIntervals]generatedBoundsInterval
	intervalsN  int
}

// generatedBoundsOutput parses an envelope incrementally. Only one bounded
// object and at most two tracks of bounded reordered intervals are retained.
// All JSON is consumed, including after the final packet and stream records.
type generatedBoundsOutput struct {
	budget   *generatedBoundsBudget
	state    uint8
	section  string
	seen     uint8
	key      [32]byte
	keyN     int
	record   [maxGeneratedBoundsRecordBytes]byte
	recordN  int
	depth    int
	inString bool
	escaped  bool
	tracks   [2]generatedBoundsTrack
	tracksN  int
	packets  int64
	err      error
}

const (
	generatedJSONRoot uint8 = iota
	generatedJSONKeyOrEnd
	generatedJSONKeyRequired
	generatedJSONKey
	generatedJSONColon
	generatedJSONArray
	generatedJSONRecordOrEnd
	generatedJSONRecordRequired
	generatedJSONRecord
	generatedJSONRecordSeparator
	generatedJSONSectionSeparator
	generatedJSONComplete
)

func generatedJSONSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\r' || char == '\n'
}

func (w *generatedBoundsOutput) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if err := w.budget.add(len(data)); err != nil {
		w.err = err
		return 0, err
	}
	for index, char := range data {
		if err := w.consumeByte(char); err != nil {
			w.err = w.budget.fail(err)
			return index + 1, w.err
		}
	}
	return len(data), nil
}

func (w *generatedBoundsOutput) consumeByte(char byte) error {
	if w.state != generatedJSONKey && w.state != generatedJSONRecord && generatedJSONSpace(char) {
		return nil
	}
	switch w.state {
	case generatedJSONRoot:
		if char != '{' {
			return ErrTimelineProbe
		}
		w.state = generatedJSONKeyOrEnd
	case generatedJSONKeyOrEnd, generatedJSONKeyRequired:
		if char == '}' && w.state == generatedJSONKeyOrEnd {
			w.state = generatedJSONComplete
			return nil
		}
		if char != '"' {
			return ErrTimelineProbe
		}
		w.keyN, w.state = 0, generatedJSONKey
	case generatedJSONKey:
		if char == '"' {
			w.section = string(w.key[:w.keyN])
			var bit uint8
			switch w.section {
			case "packets":
				bit = 1
			case "streams":
				bit = 2
			case "programs":
				bit = 4
			case "stream_groups":
				bit = 8
			default:
				return ErrTimelineProbe
			}
			if w.seen&bit != 0 {
				return ErrTimelineProbe
			}
			w.seen |= bit
			w.state = generatedJSONColon
			return nil
		}
		if (char < 'a' || char > 'z') && char != '_' || w.keyN == len(w.key) {
			return ErrTimelineProbe
		}
		w.key[w.keyN] = char
		w.keyN++
	case generatedJSONColon:
		if char != ':' {
			return ErrTimelineProbe
		}
		w.state = generatedJSONArray
	case generatedJSONArray:
		if char != '[' {
			return ErrTimelineProbe
		}
		w.state = generatedJSONRecordOrEnd
	case generatedJSONRecordOrEnd, generatedJSONRecordRequired:
		if char == ']' && w.state == generatedJSONRecordOrEnd {
			w.state = generatedJSONSectionSeparator
			return nil
		}
		if char != '{' {
			return ErrTimelineProbe
		}
		w.record[0], w.recordN, w.depth = char, 1, 1
		w.inString, w.escaped, w.state = false, false, generatedJSONRecord
	case generatedJSONRecord:
		if w.recordN == len(w.record) {
			return ErrTimelineLimit
		}
		w.record[w.recordN] = char
		w.recordN++
		if w.inString {
			if w.escaped {
				w.escaped = false
			} else if char == '\\' {
				w.escaped = true
			} else if char == '"' {
				w.inString = false
			}
			return nil
		}
		switch char {
		case '"':
			w.inString = true
		case '{', '[':
			w.depth++
			if w.depth > 8 {
				return ErrTimelineLimit
			}
		case '}', ']':
			w.depth--
			if w.depth == 0 {
				if char != '}' {
					return ErrTimelineProbe
				}
				if err := w.consumeRecord(w.record[:w.recordN]); err != nil {
					return err
				}
				w.recordN, w.state = 0, generatedJSONRecordSeparator
			}
		}
	case generatedJSONRecordSeparator:
		if char == ',' {
			w.state = generatedJSONRecordRequired
		} else if char == ']' {
			w.state = generatedJSONSectionSeparator
		} else {
			return ErrTimelineProbe
		}
	case generatedJSONSectionSeparator:
		if char == ',' {
			w.state = generatedJSONKeyRequired
		} else if char == '}' {
			w.state = generatedJSONComplete
		} else {
			return ErrTimelineProbe
		}
	case generatedJSONComplete:
		return ErrTimelineProbe
	default:
		return ErrTimelineProbe
	}
	return nil
}

// generatedUniqueJSON rejects duplicate keys at every depth before typed
// decoding can silently select the last occurrence of a clock or stream ID.
func generatedUniqueJSON(data []byte) error {
	if !utf8.Valid(data) || !generatedBoundsJSONUnicode(data) {
		return ErrTimelineProbe
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 8 {
			return ErrTimelineLimit
		}
		token, err := decoder.Token()
		if err != nil {
			return ErrTimelineProbe
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delimiter {
		case '{':
			keys := make(map[string]struct{})
			for decoder.More() {
				token, err := decoder.Token()
				key, ok := token.(string)
				if err != nil || !ok {
					return ErrTimelineProbe
				}
				if _, found := keys[key]; found {
					return ErrTimelineProbe
				}
				keys[key] = struct{}{}
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return ErrTimelineProbe
		}
		closing, err := decoder.Token()
		if err != nil || delimiter == '{' && closing != json.Delim('}') || delimiter == '[' && closing != json.Delim(']') {
			return ErrTimelineProbe
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrTimelineProbe
	}
	return nil
}

// Reject surrogate replacement before encoding/json normalizes invalid
// Unicode into the same metadata as a literal replacement character.
func generatedBoundsJSONUnicode(data []byte) bool {
	quoted := false
	for position := 0; position < len(data); position++ {
		if data[position] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[position] != '\\' {
			continue
		}
		position++
		if position >= len(data) {
			return false
		}
		if data[position] != 'u' {
			continue
		}
		if position+4 >= len(data) {
			return false
		}
		code, err := strconv.ParseUint(string(data[position+1:position+5]), 16, 16)
		if err != nil {
			return false
		}
		position += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code < 0xd800 || code > 0xdbff {
			continue
		}
		if position+6 >= len(data) || data[position+1] != '\\' || data[position+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(data[position+3:position+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		position += 6
	}
	return true
}

type generatedBoundsPacket struct {
	StreamIndex *int64                    `json:"stream_index"`
	PTS         *int64                    `json:"pts"`
	DTS         *int64                    `json:"dts"`
	Duration    *int64                    `json:"duration"`
	Flags       *string                   `json:"flags"`
	SideData    []generatedBoundsSideData `json:"side_data_list"`
}

type generatedBoundsSideData struct {
	Type           string `json:"side_data_type"`
	SkipSamples    *int64 `json:"skip_samples"`
	DiscardPadding *int64 `json:"discard_padding"`
}

type generatedBoundsStream struct {
	Index      *int64 `json:"index"`
	Kind       string `json:"codec_type"`
	Codec      string `json:"codec_name"`
	TimeBase   string `json:"time_base"`
	SampleRate string `json:"sample_rate"`
}

func (w *generatedBoundsOutput) track(index int64) (*generatedBoundsTrack, error) {
	if index < 0 || index > maxStreamIndex {
		return nil, ErrTimelineProbe
	}
	for slot := 0; slot < w.tracksN; slot++ {
		if w.tracks[slot].index == index {
			return &w.tracks[slot], nil
		}
	}
	if w.tracksN == len(w.tracks) {
		return nil, ErrTimelineProbe
	}
	track := &w.tracks[w.tracksN]
	track.index = index
	w.tracksN++
	return track, nil
}

func (w *generatedBoundsOutput) consumeRecord(data []byte) error {
	if err := generatedUniqueJSON(data); err != nil {
		return err
	}
	switch w.section {
	case "packets":
		var packet generatedBoundsPacket
		if err := generatedBoundsDecodeRecord(data, &packet, "stream_index", "pts", "dts", "duration", "flags", "side_data_list", "pts_time", "dts_time", "duration_time"); err != nil || packet.StreamIndex == nil || packet.PTS == nil || packet.DTS == nil ||
			packet.Duration == nil || *packet.Duration <= 0 || packet.Flags == nil || len(*packet.Flags) > 8 ||
			*packet.PTS > math.MaxInt64-*packet.Duration || *packet.DTS > math.MaxInt64-*packet.Duration {
			return ErrTimelineProbe
		}
		if w.packets == maxGeneratedBoundsPackets {
			return ErrTimelineLimit
		}
		w.packets++
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return ErrTimelineProbe
		}
		if raw, exists := fields["side_data_list"]; exists {
			var sides []json.RawMessage
			if err := json.Unmarshal(raw, &sides); err != nil || len(sides) > 8 {
				return ErrTimelineProbe
			}
			for _, side := range sides {
				var decoded generatedBoundsSideData
				if err := generatedBoundsDecodeRecord(side, &decoded, "side_data_type", "skip_samples", "discard_padding", "skip_reason", "discard_reason", "id"); err != nil {
					return err
				}
			}
		}
		track, err := w.track(*packet.StreamIndex)
		if err != nil {
			return err
		}
		return track.addPacket(packet)
	case "streams":
		var stream generatedBoundsStream
		if err := generatedBoundsDecodeRecord(data, &stream, "index", "codec_type", "codec_name", "time_base", "sample_rate", "avg_frame_rate", "r_frame_rate", "start_time", "duration"); err != nil || stream.Index == nil || stream.Kind != "video" && stream.Kind != "audio" ||
			stream.Codec == "" || len(stream.Codec) > 32 {
			return ErrTimelineProbe
		}
		for _, char := range stream.Codec {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
				return ErrTimelineProbe
			}
		}
		base, err := generatedBoundsTimeBase(stream.TimeBase)
		if err != nil {
			return err
		}
		track, err := w.track(*stream.Index)
		if err != nil || track.streamSeen {
			return ErrTimelineProbe
		}
		track.streamSeen = true
		track.bounds.Kind, track.bounds.Codec, track.bounds.TimeBase = stream.Kind, stream.Codec, base
		if stream.Kind == "audio" {
			track.bounds.SampleRate, err = strconv.ParseInt(stream.SampleRate, 10, 64)
			if err != nil || track.bounds.SampleRate <= 0 || track.bounds.SampleRate > 768000 {
				return ErrTimelineProbe
			}
		}
	case "programs", "stream_groups":
		// FFprobe can emit duplicate stream descriptions under these arrays.
		// Their bounded, validated JSON does not supply authoritative clocks.
	default:
		return ErrTimelineProbe
	}
	return nil
}

// Typed JSON decoding matches field names case-insensitively. Exact key checks
// keep differently cased names from overriding otherwise unique evidence.
func generatedBoundsDecodeRecord(data []byte, result any, allowed ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return ErrTimelineProbe
	}
	for field := range fields {
		found := false
		for _, key := range allowed {
			found = found || field == key
		}
		if !found {
			return ErrTimelineProbe
		}
	}
	if err := json.Unmarshal(data, result); err != nil {
		return ErrTimelineProbe
	}
	return nil
}

func generatedBoundsTimeBase(value string) (GeneratedRational, error) {
	numerator, denominator, ok := strings.Cut(value, "/")
	num, numErr := strconv.ParseInt(numerator, 10, 64)
	den, denErr := strconv.ParseInt(denominator, 10, 64)
	if !ok || numErr != nil || denErr != nil || num <= 0 || den <= 0 || num > math.MaxInt32 || den > math.MaxInt32 ||
		strconv.FormatInt(num, 10) != numerator || strconv.FormatInt(den, 10) != denominator {
		return GeneratedRational{}, ErrTimelineProbe
	}
	return GeneratedRational{Num: num, Den: den}, nil
}

func (t *generatedBoundsTrack) addPacket(packet generatedBoundsPacket) error {
	b := &t.bounds
	pts, dts, duration := *packet.PTS, *packet.DTS, *packet.Duration
	key, discard, corrupt := false, false, false
	for _, flag := range *packet.Flags {
		switch flag {
		case 'K':
			if key {
				return ErrTimelineProbe
			}
			key = true
		case 'D':
			if discard {
				return ErrTimelineProbe
			}
			discard = true
		case 'C':
			corrupt = true
		case '_':
		default:
			return ErrTimelineProbe
		}
	}
	if corrupt || b.DiscardPadding != 0 {
		return ErrTimelineProbe
	}
	var skip, padding int64
	seenSkip := false
	seenStreamID := false
	for _, side := range packet.SideData {
		switch side.Type {
		case "MPEGTS Stream ID":
			if seenStreamID || side.SkipSamples != nil || side.DiscardPadding != nil {
				return ErrTimelineProbe
			}
			seenStreamID = true
		case "Skip Samples":
			if side.SkipSamples == nil || side.DiscardPadding == nil || *side.SkipSamples < 0 || *side.DiscardPadding < 0 ||
				*side.SkipSamples > math.MaxUint32 || *side.DiscardPadding > math.MaxUint32 {
				return ErrTimelineProbe
			}
			if seenSkip {
				return ErrTimelineProbe
			}
			skip, padding, seenSkip = *side.SkipSamples, *side.DiscardPadding, true
		default:
			return ErrTimelineProbe
		}
	}
	if b.PacketCount == 0 {
		b.Present, b.FirstKey = true, key
		b.FirstPTS, b.LastPTS, b.EndPTS = pts, pts, pts+duration
		b.FirstDTS, b.LastDTS, b.EndDTS = dts, dts, dts+duration
		b.SkipSamples = skip
		b.FirstPacketPTS, b.FirstPacketDuration, b.FirstFlags = pts, duration, *packet.Flags
		b.MinPacketDuration, b.MaxPacketDuration = duration, duration
		b.PresentationDecodeAligned = pts == dts
		t.minDuration = duration
	} else {
		if skip != 0 || dts <= b.LastDTS || dts < b.EndDTS || dts > b.EndDTS && (b.EndDTS == math.MaxInt64 || dts != b.EndDTS+1) {
			return ErrTimelineProbe
		}
		if dts != b.EndDTS {
			b.MaxDecodeGapTicks = 1
			b.TotalDecodeGapTicks++
		}
		b.FirstPTS = min(b.FirstPTS, pts)
		b.LastPTS = max(b.LastPTS, pts)
		b.EndPTS = max(b.EndPTS, pts+duration)
		b.LastDTS, b.EndDTS = dts, dts+duration
		t.minDuration = min(t.minDuration, duration)
		b.MinPacketDuration = min(b.MinPacketDuration, duration)
		b.MaxPacketDuration = max(b.MaxPacketDuration, duration)
		b.PresentationDecodeAligned = b.PresentationDecodeAligned && pts == dts
	}
	b.PacketCount++
	b.LastPacketPTS, b.LastPacketDuration, b.LastFlags = pts, duration, *packet.Flags
	b.DiscardPadding, b.HasDiscard, b.HasCorrupt = padding, b.HasDiscard || discard, b.HasCorrupt || corrupt
	return t.addPresentationInterval(pts, pts+duration)
}

func (t *generatedBoundsTrack) addPresentationInterval(start, end int64) error {
	position := 0
	for position < t.intervalsN && t.intervals[position].start < start {
		position++
	}
	if position > 0 && t.intervals[position-1].end > start || position < t.intervalsN && end > t.intervals[position].start {
		return ErrTimelineProbe
	}
	// Merge adjacent presentation intervals, allowing only a one-clock-unit
	// rounding difference. The final minimum-duration check prevents this
	// allowance from hiding a complete one-unit frame or sample packet.
	allowRounding := t.minDuration > 1
	left := position > 0 && (t.intervals[position-1].end == start || allowRounding && t.intervals[position-1].end < math.MaxInt64 && t.intervals[position-1].end+1 == start)
	right := position < t.intervalsN && (end == t.intervals[position].start || allowRounding && end < math.MaxInt64 && end+1 == t.intervals[position].start)
	if left && t.intervals[position-1].end != start || right && end != t.intervals[position].start {
		t.bounds.MaxPresentationGapTicks = 1
	}
	if left && t.intervals[position-1].end != start {
		t.bounds.TotalPresentationGapTicks++
	}
	if right && end != t.intervals[position].start {
		t.bounds.TotalPresentationGapTicks++
	}
	switch {
	case left && right:
		t.intervals[position-1].end = t.intervals[position].end
		copy(t.intervals[position:], t.intervals[position+1:t.intervalsN])
		t.intervalsN--
	case left:
		t.intervals[position-1].end = end
	case right:
		t.intervals[position].start = start
	default:
		if t.intervalsN == len(t.intervals) {
			return ErrTimelineLimit
		}
		copy(t.intervals[position+1:], t.intervals[position:t.intervalsN])
		t.intervals[position] = generatedBoundsInterval{start: start, end: end}
		t.intervalsN++
	}
	return nil
}

func (w *generatedBoundsOutput) finish(video bool) (GeneratedSegmentBounds, error) {
	var result GeneratedSegmentBounds
	if w.err != nil {
		return result, w.err
	}
	if w.state != generatedJSONComplete || w.seen&3 != 3 || w.packets == 0 {
		return result, ErrTimelineProbe
	}
	for index := 0; index < w.tracksN; index++ {
		track := &w.tracks[index]
		bounds := track.bounds
		if !track.streamSeen || !bounds.Present || track.intervalsN != 1 ||
			track.minDuration <= 1 && (bounds.MaxPresentationGapTicks != 0 || bounds.MaxDecodeGapTicks != 0) {
			return GeneratedSegmentBounds{}, ErrTimelineProbe
		}
		switch bounds.Kind {
		case "video":
			if result.Video.Present || !bounds.FirstKey || bounds.HasDiscard || bounds.SkipSamples != 0 || bounds.DiscardPadding != 0 {
				return GeneratedSegmentBounds{}, ErrTimelineProbe
			}
			result.Video = bounds
		case "audio":
			if result.Audio.Present || !generatedBoundsTrimFits(bounds.SkipSamples, bounds.FirstPacketDuration, bounds) ||
				!generatedBoundsTrimFits(bounds.DiscardPadding, bounds.LastPacketDuration, bounds) ||
				bounds.PacketCount == 1 && !generatedBoundsTrimFits(bounds.SkipSamples+bounds.DiscardPadding, bounds.FirstPacketDuration, bounds) {
				return GeneratedSegmentBounds{}, ErrTimelineProbe
			}
			result.Audio = bounds
		default:
			return GeneratedSegmentBounds{}, ErrTimelineProbe
		}
	}
	if result.Video.Present != video || !video && !result.Audio.Present {
		return GeneratedSegmentBounds{}, ErrTimelineProbe
	}
	return result, nil
}

// The largest product is bounded to 115 bits by the integer field limits.
// Permit only the ceiling of a clock-quantized sample count, without a float
// conversion or an overflowing multiplication of duration and sample rate.
func generatedBoundsTrimFits(samples, duration int64, bounds GeneratedTrackBounds) bool {
	if samples == 0 {
		return true
	}
	var sampleTicks, packetTicks big.Int
	sampleTicks.Mul(big.NewInt(samples), big.NewInt(bounds.TimeBase.Den))
	packetTicks.Mul(big.NewInt(duration), big.NewInt(bounds.TimeBase.Num))
	packetTicks.Mul(&packetTicks, big.NewInt(bounds.SampleRate))
	packetTicks.Add(&packetTicks, big.NewInt(bounds.TimeBase.Den-1))
	return sampleTicks.Cmp(&packetTicks) <= 0
}
