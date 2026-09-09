package media

import (
	"encoding/json"
	"io"
	"math"
	"math/big"
	"strings"
)

const (
	maxAudioTimingPackets = 1_000_000
	maxAudioTimingFrames  = 1_000_000
	maxAudioTimingPending = 4096
	maxAudioTimingStreams = 32
	maxAudioFrameSamples  = 1 << 20
)

// audioTimingUnproven preserves ordinary probe facts when an exhaustive scan
// cannot prove both the decoded sample timeline and its packet boundaries.
type audioTimingUnproven struct {
	Reason string
}

func (e *audioTimingUnproven) Error() string {
	return "audio timing is unproven: " + e.Reason
}

func unprovenAudioTiming(reason string) error {
	return &audioTimingUnproven{Reason: reason}
}

// audioTimingSupport distinguishes audio-only input from the deliberately
// narrow set of self-contained formats with verified packet/frame semantics.
func audioTimingSupport(info Info) (audioOnly bool, reason string) {
	audioStreams := 0
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture {
			return false, ""
		}
		if stream.CodecType == "audio" {
			audioOnly = true
			audioStreams++
		}
	}
	if !audioOnly {
		return false, ""
	}
	if audioStreams > maxAudioTimingStreams {
		return true, "stream_limit"
	}
	knownFormat := false
	for _, format := range strings.Split(strings.ToLower(info.Container), ",") {
		switch strings.TrimSpace(format) {
		case "mp3", "aac", "flac", "wav", "mov", "mp4", "m4a", "ogg":
			knownFormat = true
		case "3gp", "3g2", "mj2":
			// These aliases accompany the mov/mp4/m4a demuxer name.
		default:
			return true, "unsupported_format"
		}
	}
	if !knownFormat {
		return true, "unsupported_format"
	}
	for _, stream := range info.Streams {
		if stream.CodecType != "audio" {
			continue
		}
		if stream.SampleRate <= 0 || stream.SampleRate > 768000 {
			return true, "invalid_scan"
		}
		codec := strings.ToLower(stream.Codec)
		if strings.EqualFold(info.Container, "ogg") {
			if codec != "opus" && codec != "vorbis" && codec != "flac" {
				return true, "unsupported_codec"
			}
			continue
		}
		switch codec {
		case "mp3", "aac", "flac", "alac":
		default:
			if !strings.HasPrefix(codec, "pcm_") || len(codec) == len("pcm_") {
				return true, "unsupported_codec"
			}
		}
	}
	return true, ""
}

type audioTimingRecord struct {
	Type       string `json:"type"`
	CodecType  string `json:"codec_type"`
	MediaType  string `json:"media_type"`
	Stream     scalar `json:"stream_index"`
	PTS        scalar `json:"pts"`
	Duration   scalar `json:"duration"`
	Position   scalar `json:"pos"`
	PacketPos  scalar `json:"pkt_pos"`
	Samples    scalar `json:"nb_samples"`
	SampleRate scalar `json:"sample_rate"`
	Flags      string `json:"flags"`
	SideData   []struct {
		Type           string `json:"side_data_type"`
		SkipSamples    scalar `json:"skip_samples"`
		DiscardPadding scalar `json:"discard_padding"`
	} `json:"side_data_list"`
}

type audioTimingPacket struct {
	ordinal         int64
	position        int64
	start           *big.Rat
	end             *big.Rat
	duration        *big.Rat
	padding         int64
	hasSamples      bool
	samples         int64
	firstFrame      *big.Rat
	lastFrame       *big.Rat
	expectedSamples int64
	expectedStart   *big.Rat
}

type audioTimingStream struct {
	position          int
	codec             string
	rate              int64
	base              *big.Rat
	packets           int64
	samples           int64
	lastPacketPos     int64
	nextPacketPTS     int64
	lastFrameOrdinal  int64
	firstFrame        *big.Rat
	nextFrame         *big.Rat
	firstPacket       *big.Rat
	lastPacket        *big.Rat
	maxPacketDuration *big.Rat
	queue             []*audioTimingPacket
	head              int
	ogg               bool
	expectedPackets   int64
	opusSamples       []uint16
	opusPreSkip       int64
	skipRemaining     int64
	vorbisMaxSamples  int64
}

type audioTimingScan struct {
	streams map[int]*audioTimingStream
	known   map[int]bool
	packets int64
	frames  int64
	pending int
}

// parseAudioTiming keeps only one JSON record and a bounded packet association
// window. It never mutates info unless every audio stream is proven exact.
func parseAudioTiming(reader io.Reader, info Info) (Info, error) {
	return parseAudioTimingWithOgg(reader, info, nil)
}

func parseAudioTimingWithOgg(reader io.Reader, info Info, ogg *oggAudioEvidence) (Info, error) {
	audioOnly, reason := audioTimingSupport(info)
	if !audioOnly {
		return info, unprovenAudioTiming("not_audio_only")
	}
	if reason != "" {
		return info, unprovenAudioTiming(reason)
	}
	if strings.EqualFold(info.Container, "ogg") && ogg == nil {
		return info, unprovenAudioTiming("ogg_structure_unverified")
	}
	scan := &audioTimingScan{
		streams: make(map[int]*audioTimingStream),
		known:   make(map[int]bool),
	}
	for position, stream := range info.Streams {
		if stream.Index < 0 || scan.known[stream.Index] {
			return info, unprovenAudioTiming("invalid_scan")
		}
		scan.known[stream.Index] = true
		if stream.CodecType != "audio" {
			continue
		}
		base, err := scalar(stream.TimeBase).rational()
		if err != nil || base.Sign() <= 0 || stream.SampleRate <= 0 || stream.SampleRate > 768000 {
			return info, unprovenAudioTiming("invalid_scan")
		}
		state := &audioTimingStream{
			position: position,
			codec:    strings.ToLower(stream.Codec),
			rate:     int64(stream.SampleRate),
			base:     base,
		}
		if ogg != nil {
			if base.Cmp(new(big.Rat).SetFrac64(1, state.rate)) != 0 || ogg.PacketCounts[stream.Index] <= 0 {
				return info, unprovenAudioTiming("ogg_timing_unverified")
			}
			state.ogg, state.expectedPackets = true, ogg.PacketCounts[stream.Index]
			state.opusSamples, state.opusPreSkip = ogg.OpusSamples[stream.Index], ogg.OpusPreSkip[stream.Index]
			state.skipRemaining = state.opusPreSkip
			state.vorbisMaxSamples = int64(ogg.VorbisMaxPacketSamples[stream.Index])
			if state.codec == "opus" && int64(len(state.opusSamples)) != state.expectedPackets ||
				state.codec == "vorbis" && state.vorbisMaxSamples <= 0 {
				return info, unprovenAudioTiming("ogg_timing_unverified")
			}
		}
		scan.streams[stream.Index] = state
	}
	decoder := json.NewDecoder(reader)
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return info, unprovenAudioTiming("invalid_scan")
	}
	found := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil || key != "packets_and_frames" || found {
			return info, unprovenAudioTiming("invalid_scan")
		}
		found = true
		if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
			return info, unprovenAudioTiming("invalid_scan")
		}
		for decoder.More() {
			var record audioTimingRecord
			if err := decoder.Decode(&record); err != nil {
				return info, unprovenAudioTiming("invalid_scan")
			}
			if err := scan.record(record); err != nil {
				return info, err
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return info, unprovenAudioTiming("invalid_scan")
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || !found {
		return info, unprovenAudioTiming("invalid_scan")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return info, unprovenAudioTiming("invalid_scan")
	}
	return scan.finish(info)
}

func (scan *audioTimingScan) record(record audioTimingRecord) error {
	index, err := audioTimingInteger(record.Stream, true)
	if err != nil || index < 0 || int64(int(index)) != index || !scan.known[int(index)] {
		return unprovenAudioTiming("invalid_scan")
	}
	stream := scan.streams[int(index)]
	switch record.Type {
	case "packet":
		scan.packets++
		if scan.packets > maxAudioTimingPackets {
			return unprovenAudioTiming("scan_limit")
		}
		if stream == nil {
			return nil
		}
		if record.CodecType != "" && record.CodecType != "audio" {
			return unprovenAudioTiming("invalid_scan")
		}
		return scan.packet(stream, record)
	case "frame":
		scan.frames++
		if scan.frames > maxAudioTimingFrames {
			return unprovenAudioTiming("scan_limit")
		}
		if stream == nil {
			return nil
		}
		if record.MediaType != "" && record.MediaType != "audio" {
			return unprovenAudioTiming("invalid_scan")
		}
		return scan.frame(stream, record)
	default:
		return unprovenAudioTiming("invalid_scan")
	}
}

func (scan *audioTimingScan) packet(stream *audioTimingStream, record audioTimingRecord) error {
	position, posErr := audioTimingInteger(record.Position, true)
	pts, ptsErr := audioTimingInteger(record.PTS, true)
	duration, durationErr := audioTimingInteger(record.Duration, true)
	skip, discard, paddingErr := audioTimingTrim(record)
	padding := skip + discard
	if paddingErr != nil {
		return paddingErr
	}
	if posErr != nil || ptsErr != nil || durationErr != nil ||
		position < 0 || duration <= 0 || (stream.packets > 0 &&
		(position < stream.lastPacketPos || !stream.ogg && position == stream.lastPacketPos)) || len(record.Flags) > 64 {
		return unprovenAudioTiming("invalid_scan")
	}
	if strings.Contains(record.Flags, "C") {
		return unprovenAudioTiming("corrupt_packet")
	}
	for _, side := range record.SideData {
		if strings.EqualFold(side.Type, "New Extradata") || strings.EqualFold(side.Type, "Param Change") ||
			stream.ogg && stream.packets > 0 && strings.EqualFold(side.Type, "Strings Metadata") {
			return unprovenAudioTiming("audio_parameters_changed")
		}
	}
	if pts > math.MaxInt64-duration {
		return unprovenAudioTiming("timing_overflow")
	}
	if stream.packets > 0 && pts != stream.nextPacketPTS {
		return unprovenAudioTiming("non_contiguous_packets")
	}
	if scan.pending >= maxAudioTimingPending {
		return unprovenAudioTiming("scan_limit")
	}
	packet := &audioTimingPacket{
		ordinal:  stream.packets + 1,
		position: position,
		start:    new(big.Rat).Mul(new(big.Rat).SetInt64(pts), stream.base),
		end:      new(big.Rat).Mul(new(big.Rat).SetInt64(pts+duration), stream.base),
		duration: new(big.Rat).Mul(new(big.Rat).SetInt64(duration), stream.base),
		padding:  padding,
	}
	if stream.ogg {
		if packet.ordinal > stream.expectedPackets {
			return unprovenAudioTiming("ogg_packet_count_mismatch")
		}
		if stream.codec == "vorbis" && duration > stream.vorbisMaxSamples {
			return unprovenAudioTiming("ogg_packet_duration")
		}
		if stream.codec == "opus" {
			raw := int64(stream.opusSamples[stream.packets])
			last := packet.ordinal == stream.expectedPackets
			if raw <= 0 || raw > 5760 || skip != 0 && (stream.packets != 0 || skip != stream.opusPreSkip) ||
				stream.packets == 0 && skip != stream.opusPreSkip || discard > 0 && !last || discard > raw ||
				duration != max(int64(1), raw-discard) {
				return unprovenAudioTiming("ogg_opus_trim")
			}
			used := min(raw, stream.skipRemaining)
			stream.skipRemaining -= used
			packet.expectedSamples = max(int64(0), raw-used-discard)
			packet.expectedStart = new(big.Rat).Add(packet.start, new(big.Rat).SetFrac64(used, stream.rate))
			packet.duration = new(big.Rat).SetFrac64(raw, stream.rate)
		}
	}
	stream.packets++
	stream.lastPacketPos = position
	stream.nextPacketPTS = pts + duration
	if stream.maxPacketDuration == nil || packet.duration.Cmp(stream.maxPacketDuration) > 0 {
		stream.maxPacketDuration = packet.duration
	}
	stream.queue = append(stream.queue, packet)
	scan.pending++
	return nil
}

func (scan *audioTimingScan) frame(stream *audioTimingStream, record audioTimingRecord) error {
	position, posErr := audioTimingInteger(record.PacketPos, true)
	pts, ptsErr := audioTimingInteger(record.PTS, true)
	samples, samplesErr := audioTimingInteger(record.Samples, true)
	rate, rateErr := audioTimingInteger(record.SampleRate, false)
	_, paddingErr := audioTimingPadding(record)
	if paddingErr != nil {
		return paddingErr
	}
	if posErr != nil || ptsErr != nil || samplesErr != nil || rateErr != nil ||
		position < 0 || samples < 0 || (!record.SampleRate.missing() && rate != stream.rate) {
		return unprovenAudioTiming("invalid_scan")
	}
	if samples == 0 {
		return nil
	}
	if samples > maxAudioFrameSamples {
		return unprovenAudioTiming("scan_limit")
	}
	start := new(big.Rat).Mul(new(big.Rat).SetInt64(pts), stream.base)
	end := new(big.Rat).Add(start, new(big.Rat).SetFrac64(samples, stream.rate))
	var packet *audioTimingPacket
	positionKnown := false
	for _, candidate := range stream.queue[stream.head:] {
		if candidate.position != position {
			continue
		}
		positionKnown = true
		if start.Cmp(candidate.start) >= 0 && end.Cmp(candidate.end) <= 0 {
			if packet != nil {
				return unprovenAudioTiming("unproven_packet_mapping")
			}
			packet = candidate
		}
	}
	if packet == nil {
		if positionKnown {
			return unprovenAudioTiming("packet_frame_mismatch")
		}
		return unprovenAudioTiming("unproven_packet_mapping")
	}
	if packet.ordinal < stream.lastFrameOrdinal {
		return unprovenAudioTiming("unproven_packet_mapping")
	}
	if stream.nextFrame != nil && start.Cmp(stream.nextFrame) != 0 {
		return unprovenAudioTiming("non_contiguous_frames")
	}
	// nb_samples already reflects decoder trimming. Subtracting packet or frame
	// skip/discard values again would shorten MP3 and other padded codecs twice.
	// A frame extending beyond its packet can reflect an AAC edit; the scan then
	// cannot prove packet-copy boundaries even when the decoded samples are known.
	if start.Cmp(packet.start) < 0 || end.Cmp(packet.end) > 0 {
		return unprovenAudioTiming("packet_frame_mismatch")
	}
	if stream.samples > math.MaxInt64-samples {
		return unprovenAudioTiming("timing_overflow")
	}
	for stream.head < len(stream.queue) && stream.queue[stream.head].ordinal < packet.ordinal {
		if err := scan.retire(stream, stream.queue[stream.head]); err != nil {
			return err
		}
		stream.queue[stream.head] = nil
		stream.head++
	}
	if stream.head > 0 && stream.head >= len(stream.queue)/2 {
		stream.queue = append([]*audioTimingPacket(nil), stream.queue[stream.head:]...)
		stream.head = 0
	}
	if stream.firstFrame == nil {
		stream.firstFrame = start
		stream.firstPacket = packet.start
	}
	stream.nextFrame = end
	stream.lastPacket = packet.start
	stream.lastFrameOrdinal = packet.ordinal
	stream.samples += samples
	packet.hasSamples = true
	packet.samples += samples
	if packet.firstFrame == nil {
		packet.firstFrame = start
	}
	packet.lastFrame = end
	return nil
}

func (scan *audioTimingScan) retire(stream *audioTimingStream, packet *audioTimingPacket) error {
	if stream.ogg {
		switch {
		case stream.codec == "opus":
			if packet.samples != packet.expectedSamples || packet.hasSamples &&
				(packet.firstFrame.Cmp(packet.expectedStart) != 0 || packet.lastFrame.Cmp(packet.end) != 0) {
				return unprovenAudioTiming("ogg_frame_coverage")
			}
		case !packet.hasSamples && stream.codec == "vorbis" && packet.ordinal == 1:
			// FFmpeg's Vorbis decoder internally discards its first decoded block
			// to initialize overlap-add. The validated header bounds its duration.
		case !packet.hasSamples || packet.firstFrame.Cmp(packet.start) != 0 || packet.lastFrame.Cmp(packet.end) != 0:
			return unprovenAudioTiming("ogg_frame_coverage")
		}
		scan.pending--
		return nil
	}
	if !packet.hasSamples {
		rawSamples := new(big.Rat).Mul(packet.duration, new(big.Rat).SetInt64(stream.rate))
		if !rawSamples.IsInt() || !rawSamples.Num().IsInt64() {
			return unprovenAudioTiming("unproven_packet_drop")
		}
		count := rawSamples.Num().Int64()
		// AAC edit lists can shorten duration while retaining discard padding for
		// the original frame. Require a complete known codec frame before padding
		// can prove that a packet without decoded samples was entirely discarded.
		wholeFrame := stream.codec == "mp3" && (count == 1152 || count == 576) ||
			stream.codec == "aac" && (count == 1024 || count == 2048)
		if !wholeFrame || count > packet.padding {
			return unprovenAudioTiming("unproven_packet_drop")
		}
	}
	scan.pending--
	return nil
}

func (scan *audioTimingScan) finish(info Info) (Info, error) {
	var origin *big.Rat
	for _, stream := range scan.streams {
		if stream.ogg && stream.packets != stream.expectedPackets {
			return info, unprovenAudioTiming("ogg_packet_count_mismatch")
		}
		if stream.firstFrame == nil || stream.samples <= 0 {
			return info, unprovenAudioTiming("no_audio_frames")
		}
		for _, packet := range stream.queue[stream.head:] {
			if err := scan.retire(stream, packet); err != nil {
				return info, err
			}
		}
		if origin == nil || stream.firstFrame.Cmp(origin) < 0 {
			origin = stream.firstFrame
		}
	}
	if origin == nil {
		return info, unprovenAudioTiming("no_audio_frames")
	}
	originTicks, err := rationalTicks(origin)
	if err != nil {
		return info, unprovenAudioTiming("timing_overflow")
	}
	result := info
	result.Streams = append([]Stream(nil), info.Streams...)
	result.DurationTicks = 0
	for _, stream := range scan.streams {
		timing := &AudioTiming{Exact: true, SampleCount: stream.samples, PacketCount: stream.packets}
		fields := []struct {
			value  *big.Rat
			target *int64
			ceil   bool
		}{
			{new(big.Rat).Sub(stream.firstFrame, origin), &timing.StartTicks, false},
			{new(big.Rat).Sub(stream.nextFrame, origin), &timing.EndTicks, true},
			{new(big.Rat).Sub(stream.firstPacket, origin), &timing.FirstPacketStartTicks, false},
			{new(big.Rat).Sub(stream.lastPacket, origin), &timing.LastPacketStartTicks, false},
			{stream.maxPacketDuration, &timing.MaxPacketDurationTicks, true},
		}
		for _, field := range fields {
			*field.target, err = audioTimingTicks(field.value, field.ceil)
			if err != nil {
				return info, err
			}
		}
		result.Streams[stream.position].AudioTiming = timing
		if timing.EndTicks > result.DurationTicks {
			result.DurationTicks = timing.EndTicks
		}
	}
	result.AudioDurationExact = true
	result.AudioDurationReason = ""
	result.PresentationOriginTicks = originTicks
	return result, nil
}

func audioTimingInteger(value scalar, required bool) (int64, error) {
	if len(value) > 256 || required && value.missing() {
		return 0, unprovenAudioTiming("invalid_scan")
	}
	return value.integer()
}

func audioTimingPadding(record audioTimingRecord) (int64, error) {
	skip, discard, err := audioTimingTrim(record)
	return skip + discard, err
}

func audioTimingTrim(record audioTimingRecord) (int64, int64, error) {
	if len(record.SideData) > 64 {
		return 0, 0, unprovenAudioTiming("scan_limit")
	}
	var skip, discard int64
	found := false
	for _, side := range record.SideData {
		if len(side.Type) > 128 {
			return 0, 0, unprovenAudioTiming("invalid_scan")
		}
		if !side.SkipSamples.missing() || !side.DiscardPadding.missing() {
			if found {
				return 0, 0, unprovenAudioTiming("invalid_scan")
			}
			found = true
		}
		sideSkip, err := audioTimingInteger(side.SkipSamples, false)
		if err != nil || sideSkip < 0 {
			return 0, 0, unprovenAudioTiming("invalid_scan")
		}
		sideDiscard, err := audioTimingInteger(side.DiscardPadding, false)
		if err != nil || sideDiscard < 0 {
			return 0, 0, unprovenAudioTiming("invalid_scan")
		}
		if !side.SkipSamples.missing() {
			skip = sideSkip
		}
		if !side.DiscardPadding.missing() {
			discard = sideDiscard
		}
		if skip > math.MaxInt64-discard {
			return 0, 0, unprovenAudioTiming("invalid_scan")
		}
	}
	return skip, discard, nil
}

// audioTimingTicks rounds outward so a finite tick grid never removes samples.
func audioTimingTicks(seconds *big.Rat, ceil bool) (int64, error) {
	ticks := new(big.Rat).Mul(seconds, new(big.Rat).SetInt64(TicksPerSecond))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(ticks.Num(), ticks.Denom(), remainder)
	if remainder.Sign() != 0 {
		if ceil && ticks.Sign() > 0 {
			quotient.Add(quotient, big.NewInt(1))
		} else if !ceil && ticks.Sign() < 0 {
			quotient.Sub(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0, unprovenAudioTiming("timing_overflow")
	}
	return quotient.Int64(), nil
}
