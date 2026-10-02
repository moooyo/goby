package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"hash"
	"math/big"
)

const (
	generatedAVEffectiveJSONBytes = 4 << 20
	generatedAVEffectiveFrames    = 8192
	generatedAVPCMBytes           = 24 << 20
	generatedAVPCMMaxBins         = 1024
)

type GeneratedAVEffectiveSideData struct {
	Type                                           string
	SkipSamples, DiscardPadding                    int64
	SkipReason, DiscardReason                      int64
	TrimKnown, SkipReasonKnown, DiscardReasonKnown bool
}

// GeneratedAVEffectiveFrame retains native frame PTS. End is derived from the
// actual returned nb_samples for audio, or native duration for video. A decoder
// may already have applied trim; these samples are never trimmed a second time.
type GeneratedAVEffectiveFrame struct {
	StreamIndex           int
	Kind                  string
	PTS                   int64
	DTSKnown              bool
	DTS                   int64
	DurationKnown         bool
	Duration              int64
	Samples               int64
	KeyKnown              bool
	Key                   bool
	End                   GeneratedRational
	EndDerivedFromSamples bool
	SideData              []GeneratedAVEffectiveSideData
}

type GeneratedAVEffectiveTrack struct {
	StreamIndex                              int
	Kind, Codec, SampleFormat, ChannelLayout string
	TimeBase                                 GeneratedRational
	SampleRate                               int64
	Channels                                 int
	Frames, Samples                          int64
	First, Last, End                         GeneratedRational
	Gaps, Overlaps                           int64
}

// GeneratedAVEffectiveDecodeDiagnostic is actual decoded frame evidence, not
// source EOF, applied-edit provenance, native playback or A/V qualification.
// Complete requires full JSON, a retired decoder and input copier EOF in the
// held-file wrapper; the pure parser only sets FramesParsed.
type GeneratedAVEffectiveDecodeDiagnostic struct {
	Qualified, Complete, FramesParsed bool
	InputBytes                        int64
	InputSHA256                       [][32]byte
	Video, Audio                      GeneratedAVEffectiveTrack
	Frames                            []GeneratedAVEffectiveFrame
}

type GeneratedAVPCMBin struct {
	FirstSample, Samples, NonzeroValues int64
	SumSquares                          [2]uint64
}

// GeneratedAVPCMDiagnostic counts actual streamed s16le values. PCM carries no
// native timestamps; its sample count cannot repair missing decoded-frame PTS.
type GeneratedAVPCMDiagnostic struct {
	Qualified, Complete bool
	Bytes, Samples      int64
	Channels            int
	SampleRate          int64
	SHA256              [32]byte
	Bins                []GeneratedAVPCMBin
}

type generatedAVEffectiveStream struct {
	Index    *int64 `json:"index"`
	Kind     string `json:"codec_type"`
	Codec    string `json:"codec_name"`
	TimeBase string `json:"time_base"`
	Rate     string `json:"sample_rate"`
	Channels *int64 `json:"channels"`
	Format   string `json:"sample_fmt"`
	Layout   string `json:"channel_layout"`
}

type generatedAVEffectiveFrameRecord struct {
	Kind     string            `json:"media_type"`
	Index    *int64            `json:"stream_index"`
	PTS      *int64            `json:"pts"`
	DTS      *int64            `json:"pkt_dts"`
	Duration *int64            `json:"duration"`
	Samples  *int64            `json:"nb_samples"`
	Key      *int64            `json:"key_frame"`
	Side     []json.RawMessage `json:"side_data_list"`
}

func generatedAVEffectiveRational(value *big.Rat) (GeneratedRational, error) {
	if !value.Num().IsInt64() || !value.Denom().IsInt64() {
		return GeneratedRational{}, ErrTimelineLimit
	}
	return GeneratedRational{Num: value.Num().Int64(), Den: value.Denom().Int64()}, nil
}

// ParseGeneratedAVEffectiveDecodeDiagnostic rejects missing original PTS and
// every unknown projected field. Gaps/overlaps remain diagnostic findings rather
// than being hidden by an endpoint-only acceptance or nominal cropping.
func ParseGeneratedAVEffectiveDecodeDiagnostic(ctx context.Context, data []byte) (GeneratedAVEffectiveDecodeDiagnostic, error) {
	var empty GeneratedAVEffectiveDecodeDiagnostic
	invalid := func() (GeneratedAVEffectiveDecodeDiagnostic, error) {
		return empty, generatedAVTransportInvalid("effective frame projection")
	}
	if ctx == nil {
		return invalid()
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
	var doc struct {
		Frames   []json.RawMessage `json:"frames"`
		Streams  []json.RawMessage `json:"streams"`
		Programs []json.RawMessage `json:"programs"`
		Groups   []json.RawMessage `json:"stream_groups"`
	}
	if generatedBoundsDecodeRecord(data, &doc, "frames", "streams", "programs", "stream_groups") != nil || len(doc.Streams) != 2 || len(doc.Frames) == 0 || len(doc.Frames) > generatedAVEffectiveFrames || len(doc.Programs) != 0 || len(doc.Groups) != 0 {
		return invalid()
	}
	result := GeneratedAVEffectiveDecodeDiagnostic{}
	for _, raw := range doc.Streams {
		var stream generatedAVEffectiveStream
		if generatedBoundsDecodeRecord(raw, &stream, "index", "codec_type", "codec_name", "time_base", "sample_rate", "channels", "sample_fmt", "channel_layout") != nil || stream.Index == nil || *stream.Index < 0 || *stream.Index > 7 {
			return invalid()
		}
		base, err := generatedBoundsTimeBase(stream.TimeBase)
		if err != nil {
			return invalid()
		}
		track := GeneratedAVEffectiveTrack{StreamIndex: int(*stream.Index), Kind: stream.Kind, Codec: stream.Codec, TimeBase: base, SampleFormat: stream.Format, ChannelLayout: stream.Layout}
		switch stream.Kind {
		case "video":
			if stream.Codec != "h264" || result.Video.Kind != "" || stream.Rate != "" || stream.Channels != nil || stream.Format != "" || stream.Layout != "" {
				return invalid()
			}
			result.Video = track
		case "audio":
			if stream.Codec != "aac" || result.Audio.Kind != "" || stream.Rate != "48000" || stream.Channels == nil || *stream.Channels < 1 || *stream.Channels > 2 || stream.Format != "fltp" ||
				(*stream.Channels == 1 && stream.Layout != "mono") || (*stream.Channels == 2 && stream.Layout != "stereo") {
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
	for _, raw := range doc.Frames {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		var frame generatedAVEffectiveFrameRecord
		if generatedBoundsDecodeRecord(raw, &frame, "media_type", "stream_index", "pts", "pkt_dts", "duration", "nb_samples", "key_frame", "side_data_list") != nil || frame.Index == nil || frame.PTS == nil {
			return invalid()
		}
		track := &result.Video
		if frame.Kind == "audio" {
			track = &result.Audio
		} else if frame.Kind != "video" {
			return invalid()
		}
		if *frame.Index != int64(track.StreamIndex) || !generatedInputClockBounded(*frame.PTS, track.TimeBase.Num, track.TimeBase.Den) {
			return invalid()
		}
		fact := GeneratedAVEffectiveFrame{StreamIndex: track.StreamIndex, Kind: frame.Kind, PTS: *frame.PTS}
		if frame.DTS != nil {
			if !generatedInputClockBounded(*frame.DTS, track.TimeBase.Num, track.TimeBase.Den) {
				return invalid()
			}
			fact.DTSKnown, fact.DTS = true, *frame.DTS
		}
		if frame.Key != nil {
			if *frame.Key != 0 && *frame.Key != 1 {
				return invalid()
			}
			fact.KeyKnown, fact.Key = true, *frame.Key == 1
		}
		pts := generatedClockSeconds(*frame.PTS, track.TimeBase.Num, track.TimeBase.Den)
		var duration *big.Rat
		if frame.Kind == "video" {
			if frame.Duration == nil || *frame.Duration <= 0 || frame.Samples != nil {
				return invalid()
			}
			duration = generatedClockSeconds(*frame.Duration, track.TimeBase.Num, track.TimeBase.Den)
		} else {
			if frame.Samples == nil || *frame.Samples < 1 || *frame.Samples > 1024 || *frame.Samples > generatedAudioInputMaxSamples-track.Samples {
				return invalid()
			}
			fact.Samples = *frame.Samples
			fact.EndDerivedFromSamples = true
			track.Samples += *frame.Samples
			duration = new(big.Rat).SetFrac64(*frame.Samples, 48000)
		}
		if frame.Duration != nil {
			if *frame.Duration <= 0 {
				return invalid()
			}
			fact.DurationKnown, fact.Duration = true, *frame.Duration
		}
		end := new(big.Rat).Add(pts, duration)
		var err error
		fact.End, err = generatedAVEffectiveRational(end)
		if err != nil {
			return empty, err
		}
		if track.Frames == 0 {
			track.First, err = generatedAVEffectiveRational(pts)
			if err != nil {
				return empty, err
			}
		} else {
			prior := new(big.Rat).SetFrac64(track.End.Num, track.End.Den)
			if pts.Cmp(prior) > 0 {
				track.Gaps++
			} else if pts.Cmp(prior) < 0 {
				track.Overlaps++
			}
		}
		track.Last, err = generatedAVEffectiveRational(pts)
		if err != nil {
			return empty, err
		}
		track.End = fact.End
		track.Frames++
		for _, side := range frame.Side {
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
			// Only the known projected AVC SEI wrapper and actual audio trim
			// fields are admitted. Omitted payload fields supply no evidence.
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
			factSide := GeneratedAVEffectiveSideData{Type: value.Type}
			if value.Skip != nil {
				factSide.SkipSamples = *value.Skip
				factSide.TrimKnown = true
			}
			if value.Discard != nil {
				factSide.DiscardPadding = *value.Discard
			}
			if value.SkipReason != nil {
				if *value.SkipReason < 0 || *value.SkipReason > 1 {
					return invalid()
				}
				factSide.SkipReason = *value.SkipReason
				factSide.SkipReasonKnown = true
			}
			if value.DiscardReason != nil {
				if *value.DiscardReason < 0 || *value.DiscardReason > 1 {
					return invalid()
				}
				factSide.DiscardReason = *value.DiscardReason
				factSide.DiscardReasonKnown = true
			}
			if len(fact.SideData) >= 4 {
				return empty, ErrTimelineLimit
			}
			fact.SideData = append(fact.SideData, factSide)
		}
		result.Frames = append(result.Frames, fact)
	}
	if result.Video.Frames == 0 || result.Audio.Frames == 0 {
		return invalid()
	}
	result.FramesParsed = true
	return result, nil
}

type generatedAVPCMWriter struct {
	channels int
	limit    int64
	digest   hash.Hash
	pending  [4]byte
	pendingN int
	result   GeneratedAVPCMDiagnostic
	err      error
	cancel   context.CancelFunc
}

func newGeneratedAVPCMWriter(channels int, limit int64) (*generatedAVPCMWriter, error) {
	if channels < 1 || channels > 2 || limit < 1 || limit > generatedAVPCMBytes {
		return nil, ErrInvalidOptions
	}
	return &generatedAVPCMWriter{channels: channels, limit: limit, digest: sha256.New(), result: GeneratedAVPCMDiagnostic{Channels: channels, SampleRate: 48000}}, nil
}

func (writer *generatedAVPCMWriter) Write(data []byte) (int, error) {
	if writer.err != nil {
		return 0, writer.err
	}
	if int64(len(data)) > writer.limit-writer.result.Bytes {
		writer.err = ErrTimelineLimit
		if writer.cancel != nil {
			writer.cancel()
		}
		return 0, writer.err
	}
	_, _ = writer.digest.Write(data)
	writer.result.Bytes += int64(len(data))
	for _, value := range data {
		writer.pending[writer.pendingN] = value
		writer.pendingN++
		if writer.pendingN != writer.channels*2 {
			continue
		}
		if writer.result.Samples%4800 == 0 {
			if len(writer.result.Bins) >= generatedAVPCMMaxBins {
				writer.err = ErrTimelineLimit
				if writer.cancel != nil {
					writer.cancel()
				}
				return 0, writer.err
			}
			writer.result.Bins = append(writer.result.Bins, GeneratedAVPCMBin{FirstSample: writer.result.Samples})
		}
		bin := &writer.result.Bins[len(writer.result.Bins)-1]
		for channel := 0; channel < writer.channels; channel++ {
			sample := int64(int16(binary.LittleEndian.Uint16(writer.pending[channel*2 : channel*2+2])))
			if sample != 0 {
				bin.NonzeroValues++
			}
			bin.SumSquares[channel] += uint64(sample * sample)
		}
		bin.Samples++
		writer.result.Samples++
		writer.pendingN = 0
	}
	return len(data), nil
}

func (writer *generatedAVPCMWriter) finish() (GeneratedAVPCMDiagnostic, error) {
	if writer.err != nil {
		return GeneratedAVPCMDiagnostic{}, writer.err
	}
	if writer.pendingN != 0 || writer.result.Samples == 0 {
		return GeneratedAVPCMDiagnostic{}, generatedAVTransportInvalid("partial or absent PCM samples")
	}
	copy(writer.result.SHA256[:], writer.digest.Sum(nil))
	return writer.result, nil
}
