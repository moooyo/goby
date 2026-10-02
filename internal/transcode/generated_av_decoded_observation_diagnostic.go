package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
)

// PTS/DTS/duration Known flags mean the original FFprobe-returned field exists.
// They do not assert a physical PES DTS field or repair an absent coded clock.
type GeneratedAVObservedFrame struct {
	Ordinal, StreamIndex                                      int
	Kind                                                      string
	PTSKnown, DTSKnown, DurationKnown, SamplesKnown, EndKnown bool
	PTS, DTS, Duration, Samples                               int64
	KeyKnown, Key                                             bool
	End                                                       GeneratedRational
	SideData                                                  []GeneratedAVEffectiveSideData
}

type GeneratedAVObservedTrack struct {
	StreamIndex                                                             int
	Kind, Codec, SampleFormat, ChannelLayout                                string
	TimeBase                                                                GeneratedRational
	SampleRate                                                              int64
	Channels                                                                int
	Frames, Samples, NativePTSFrames, NativeDTSFrames, NativeDurationFrames int64
	ClockComplete                                                           bool
	FirstNativePTSKnown, LastNativePTSKnown                                 bool
	FirstNativePTS, LastNativePTS                                           int64
	FirstNativePTSOrdinal, LastNativePTSOrdinal                             int
	AggregateEndKnown, GapFactsKnown                                        bool
	AggregateEnd                                                            GeneratedRational
	Gaps, Overlaps                                                          int64
}

// Complete/FramesParsed describe bounded JSON and joined decoder observation.
// NativeClockComplete is separate and cannot be restored by sample counts,
// nominal FPS, adjacent PTS or packet clocks. No result qualifies A/V playback.
type GeneratedAVDecodedObservation struct {
	Qualified, Complete, FramesParsed, NativeClockComplete bool
	InputBytes                                             int64
	InputSHA256                                            [][32]byte
	Video, Audio                                           GeneratedAVObservedTrack
	Frames                                                 []GeneratedAVObservedFrame
}

// ParseGeneratedAVDecodedObservation preserves absent original frame fields as
// unknown. It is separate from the unchanged strict effective/source parser.
func ParseGeneratedAVDecodedObservation(ctx context.Context, data []byte) (GeneratedAVDecodedObservation, error) {
	var empty GeneratedAVDecodedObservation
	invalid := func() (GeneratedAVDecodedObservation, error) { return empty, ErrTimelineProbe }
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if len(data) == 0 || len(data) > generatedAVEffectiveJSONBytes {
		return empty, ErrTimelineLimit
	}
	if err := generatedUniqueJSON(data); err != nil {
		return empty, err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil {
		return invalid()
	}
	for _, key := range []string{"frames", "streams", "programs", "stream_groups"} {
		raw, present := root[key]
		if !present || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '[' {
			return invalid()
		}
	}
	var doc struct {
		Frames   []json.RawMessage `json:"frames"`
		Streams  []json.RawMessage `json:"streams"`
		Programs []json.RawMessage `json:"programs"`
		Groups   []json.RawMessage `json:"stream_groups"`
	}
	if generatedBoundsDecodeRecord(data, &doc, "frames", "streams", "programs", "stream_groups") != nil || len(doc.Frames) == 0 || len(doc.Frames) > generatedAVEffectiveFrames || validateGeneratedAVObservedProgram(doc.Programs, doc.Groups, doc.Streams) != nil {
		return invalid()
	}
	result := GeneratedAVDecodedObservation{}
	for _, raw := range doc.Streams {
		if !generatedAVObservationNoNullFields(raw) {
			return invalid()
		}
		var stream generatedAVEffectiveStream
		if generatedBoundsDecodeRecord(raw, &stream, "index", "codec_type", "codec_name", "time_base", "sample_rate", "channels", "sample_fmt", "channel_layout") != nil || stream.Index == nil || *stream.Index < 0 || *stream.Index > 7 {
			return invalid()
		}
		base, err := generatedBoundsTimeBase(stream.TimeBase)
		if err != nil {
			return invalid()
		}
		track := GeneratedAVObservedTrack{StreamIndex: int(*stream.Index), Kind: stream.Kind, Codec: stream.Codec, TimeBase: base, SampleFormat: stream.Format, ChannelLayout: stream.Layout, ClockComplete: true, FirstNativePTSOrdinal: -1, LastNativePTSOrdinal: -1}
		switch stream.Kind {
		case "video":
			if stream.Codec != "h264" || result.Video.Kind != "" || stream.Rate != "" || stream.Channels != nil || stream.Format != "" || stream.Layout != "" {
				return invalid()
			}
			result.Video = track
		case "audio":
			if stream.Codec != "aac" || result.Audio.Kind != "" || stream.Rate != "48000" || stream.Channels == nil || *stream.Channels < 1 || *stream.Channels > 2 || stream.Format != "fltp" || (*stream.Channels == 1 && stream.Layout != "mono") || (*stream.Channels == 2 && stream.Layout != "stereo") {
				return invalid()
			}
			track.SampleRate, track.Channels = 48000, int(*stream.Channels)
			result.Audio = track
		default:
			return invalid()
		}
	}
	if result.Video.Kind == "" || result.Audio.Kind == "" || result.Video.StreamIndex == result.Audio.StreamIndex {
		return invalid()
	}
	for ordinal, raw := range doc.Frames {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		var frame generatedAVEffectiveFrameRecord
		if !generatedAVObservationNoNullFields(raw) {
			return invalid()
		}
		if generatedBoundsDecodeRecord(raw, &frame, "media_type", "stream_index", "pts", "pkt_dts", "duration", "nb_samples", "key_frame", "side_data_list") != nil || frame.Index == nil {
			return invalid()
		}
		track := &result.Video
		if frame.Kind == "audio" {
			track = &result.Audio
		} else if frame.Kind != "video" {
			return invalid()
		}
		if *frame.Index != int64(track.StreamIndex) {
			return invalid()
		}
		fact := GeneratedAVObservedFrame{Ordinal: ordinal, StreamIndex: track.StreamIndex, Kind: frame.Kind}
		if frame.PTS != nil {
			if !generatedInputClockBounded(*frame.PTS, track.TimeBase.Num, track.TimeBase.Den) {
				return invalid()
			}
			fact.PTSKnown, fact.PTS = true, *frame.PTS
			track.NativePTSFrames++
			if !track.FirstNativePTSKnown {
				track.FirstNativePTSKnown, track.FirstNativePTS, track.FirstNativePTSOrdinal = true, *frame.PTS, ordinal
			}
			track.LastNativePTSKnown, track.LastNativePTS, track.LastNativePTSOrdinal = true, *frame.PTS, ordinal
		}
		if frame.DTS != nil {
			if !generatedInputClockBounded(*frame.DTS, track.TimeBase.Num, track.TimeBase.Den) {
				return invalid()
			}
			fact.DTSKnown, fact.DTS = true, *frame.DTS
			track.NativeDTSFrames++
		}
		if frame.Duration != nil {
			if *frame.Duration <= 0 || !generatedInputClockBounded(*frame.Duration, track.TimeBase.Num, track.TimeBase.Den) {
				return invalid()
			}
			fact.DurationKnown, fact.Duration = true, *frame.Duration
			track.NativeDurationFrames++
		}
		if frame.Key != nil {
			if *frame.Key != 0 && *frame.Key != 1 {
				return invalid()
			}
			fact.KeyKnown, fact.Key = true, *frame.Key == 1
		}
		if frame.Kind == "audio" {
			if frame.Samples == nil || *frame.Samples < 1 || *frame.Samples > 1024 || *frame.Samples > generatedAudioInputMaxSamples-track.Samples {
				return invalid()
			}
			fact.SamplesKnown, fact.Samples = true, *frame.Samples
			track.Samples += *frame.Samples
		} else if frame.Samples != nil {
			return invalid()
		}
		if fact.PTSKnown && fact.DurationKnown {
			end := new(big.Rat).Add(generatedClockSeconds(fact.PTS, track.TimeBase.Num, track.TimeBase.Den), generatedClockSeconds(fact.Duration, track.TimeBase.Num, track.TimeBase.Den))
			var err error
			fact.End, err = generatedAVEffectiveRational(end)
			if err != nil {
				return empty, err
			}
			fact.EndKnown = true
		}
		track.ClockComplete = track.ClockComplete && fact.PTSKnown && fact.DurationKnown
		track.Frames++
		for _, side := range frame.Side {
			if !generatedAVObservationNoNullFields(side) {
				return invalid()
			}
			var value struct {
				Type          string `json:"side_data_type"`
				Skip          *int64 `json:"skip_samples"`
				Discard       *int64 `json:"discard_padding"`
				SkipReason    *int64 `json:"skip_reason"`
				DiscardReason *int64 `json:"discard_reason"`
			}
			if generatedBoundsDecodeRecord(side, &value, "side_data_type", "skip_samples", "discard_padding", "skip_reason", "discard_reason") != nil {
				return invalid()
			}
			switch value.Type {
			case "H.26[45] User Data Unregistered SEI message":
				if frame.Kind != "video" || value.Skip != nil || value.Discard != nil || value.SkipReason != nil || value.DiscardReason != nil {
					return invalid()
				}
			case "Skip Samples":
				if frame.Kind != "audio" || value.Skip == nil || value.Discard == nil || *value.Skip < 0 || *value.Discard < 0 || *value.Skip > 65536 || *value.Discard > 65536 {
					return invalid()
				}
			default:
				return invalid()
			}
			item := GeneratedAVEffectiveSideData{Type: value.Type}
			if value.Skip != nil {
				item.TrimKnown, item.SkipSamples = true, *value.Skip
			}
			if value.Discard != nil {
				item.DiscardPadding = *value.Discard
			}
			if value.SkipReason != nil {
				if *value.SkipReason < 0 || *value.SkipReason > 1 {
					return invalid()
				}
				item.SkipReasonKnown, item.SkipReason = true, *value.SkipReason
			}
			if value.DiscardReason != nil {
				if *value.DiscardReason < 0 || *value.DiscardReason > 1 {
					return invalid()
				}
				item.DiscardReasonKnown, item.DiscardReason = true, *value.DiscardReason
			}
			if len(fact.SideData) >= 4 {
				return empty, ErrTimelineLimit
			}
			fact.SideData = append(fact.SideData, item)
		}
		result.Frames = append(result.Frames, fact)
	}
	if result.Video.Frames == 0 || result.Audio.Frames == 0 {
		return invalid()
	}
	for _, track := range []*GeneratedAVObservedTrack{&result.Video, &result.Audio} {
		if !track.ClockComplete {
			continue
		}
		var previous *GeneratedAVObservedFrame
		for index := range result.Frames {
			frame := &result.Frames[index]
			if frame.StreamIndex != track.StreamIndex {
				continue
			}
			if previous != nil {
				current := generatedClockSeconds(frame.PTS, track.TimeBase.Num, track.TimeBase.Den)
				prior := new(big.Rat).SetFrac64(previous.End.Num, previous.End.Den)
				if current.Cmp(prior) > 0 {
					track.Gaps++
				} else if current.Cmp(prior) < 0 {
					track.Overlaps++
				}
			}
			previous = frame
		}
		track.AggregateEndKnown, track.GapFactsKnown = true, true
		track.AggregateEnd = previous.End
	}
	result.FramesParsed = true
	result.NativeClockComplete = result.Video.ClockComplete && result.Audio.ClockComplete
	return result, nil
}

func generatedAVObservationNoNullFields(raw []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}
