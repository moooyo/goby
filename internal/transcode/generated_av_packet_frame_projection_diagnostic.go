package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// GeneratedAVProjectedPacket stores original probe-returned packet fields.
// DTSKnown does not assert a physical DTS field inside a PTS-only PES.
type GeneratedAVProjectedPacket struct {
	Ordinal, RecordOrdinal, StreamIndex              int
	Kind, Flags                                      string
	PTSKnown, DTSKnown, DurationKnown, PositionKnown bool
	PTS, DTS, Duration, Position, Size               int64
	DataSHA256                                       [32]byte
}

// GeneratedAVProjectedFrameOrigin preserves original reported packet-origin
// fields separately from decoded clock facts. A neighboring JSON packet is an
// emission-order fact only; it never supplies a missing returned origin.
type GeneratedAVProjectedFrameOrigin struct {
	FrameOrdinal, RecordOrdinal, StreamIndex int
	PacketPositionKnown                      bool
	PacketPosition, PacketSize               int64
	EmissionNeighborKnown                    bool
	EmissionNeighborPacketOrdinal            int
}

type generatedAVPacketFrameMeasurementSeal struct {
	jsonSHA, inputSHA, sourceSHA  [32]byte
	factsSHA, transportFactsSHA   [32]byte
	inputBytes                    int64
	inputIdentity, sourceIdentity string
}

// GeneratedAVPacketFrameProjection is a strict mixed-schema observation.
// Pure parsing fills no measured-input receipt or private seal. Only the NEW
// joined held-file wrapper can mint that seal after copier EOF and final fences.
// Public measured fields cannot promote an unmeasured parser result by themselves.
type GeneratedAVPacketFrameProjection struct {
	Qualified, Complete, SchemaParsed, MeasuredInputKnown bool
	JSONBytes                                             int
	JSONSHA256                                            [32]byte
	MeasuredInputBytes                                    int64
	MeasuredInputSHA256, MeasuredSourceSHA256             [32]byte
	MeasuredInputIdentity, MeasuredSourceIdentity         string
	Packets                                               []GeneratedAVProjectedPacket
	FrameOrigins                                          []GeneratedAVProjectedFrameOrigin
	Observation                                           GeneratedAVDecodedObservation
	measurement                                           *generatedAVPacketFrameMeasurementSeal
}

func generatedAVPacketFrameFactsFingerprint(value any) ([32]byte, error) {
	var empty [32]byte
	// Check public mutable collections/strings before JSON encoding can copy
	// an oversized caller mutation into a temporary canonical buffer.
	switch facts := value.(type) {
	case GeneratedAVPacketFrameProjection:
		if len(facts.Packets) > generatedAVTransportRecords || len(facts.FrameOrigins) > generatedAVEffectiveFrames || len(facts.Observation.Frames) > generatedAVEffectiveFrames ||
			len(facts.MeasuredInputIdentity) > 64 || len(facts.MeasuredSourceIdentity) > 64 || len(facts.Observation.InputSHA256) > 8 {
			return empty, ErrTimelineLimit
		}
		for _, packet := range facts.Packets {
			if len(packet.Kind) > 5 || len(packet.Flags) > 3 {
				return empty, ErrTimelineLimit
			}
		}
		for _, track := range []GeneratedAVObservedTrack{facts.Observation.Video, facts.Observation.Audio} {
			if len(track.Kind) > 5 || len(track.Codec) > 4 || len(track.SampleFormat) > 4 || len(track.ChannelLayout) > 6 {
				return empty, ErrTimelineLimit
			}
		}
		for _, frame := range facts.Observation.Frames {
			if len(frame.Kind) > 5 || len(frame.SideData) > 4 {
				return empty, ErrTimelineLimit
			}
			for _, side := range frame.SideData {
				if len(side.Type) > 80 {
					return empty, ErrTimelineLimit
				}
			}
		}
	case GeneratedAVTransportDiagnostic:
		if len(facts.PIDs) > generatedAVTransportPIDs || len(facts.Adaptations) > generatedAVTransportRecords || len(facts.PES) > generatedAVTransportRecords || len(facts.ADTS) > generatedAVTransportRecords || len(facts.PAT) > 1024 || len(facts.PMT) > 1024 || len(facts.SDT) > 1024 {
			return empty, ErrTimelineLimit
		}
		for _, pes := range facts.PES {
			if len(pes.Kind) > 5 || len(pes.Boundary) > 32 {
				return empty, ErrTimelineLimit
			}
		}
	default:
		return empty, ErrInvalidInput
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return empty, err
	}
	if len(encoded) > generatedAVEffectiveJSONBytes {
		return empty, ErrTimelineLimit
	}
	return sha256.Sum256(encoded), nil
}

func generatedAVPacketFrameDecimal(value string, allowZero bool) (int64, error) {
	if value == "" || len(value) > 20 || len(value) > 1 && value[0] == '0' {
		return 0, ErrTimelineProbe
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, ErrTimelineProbe
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || !allowZero && parsed == 0 {
		return 0, ErrTimelineProbe
	}
	return parsed, nil
}

func generatedAVPacketFrameHash(value string) ([32]byte, error) {
	var result [32]byte
	if len(value) != len("SHA256:")+64 || !strings.HasPrefix(value, "SHA256:") {
		return result, ErrTimelineProbe
	}
	digits := value[len("SHA256:"):]
	if digits != strings.ToLower(digits) {
		return result, ErrTimelineProbe
	}
	decoded, err := hex.DecodeString(digits)
	if err != nil || len(decoded) != len(result) {
		return result, ErrTimelineProbe
	}
	copy(result[:], decoded)
	return result, nil
}

// ParseGeneratedAVPacketFrameProjection supports the actually calibrated
// packets_and_frames/type projection only. It rejects unknown/null/duplicate
// fields and retains missing returned clocks/positions as unknown. The old
// strict source-effective parser is neither called nor relaxed by this API.
func ParseGeneratedAVPacketFrameProjection(ctx context.Context, data []byte) (GeneratedAVPacketFrameProjection, error) {
	var empty GeneratedAVPacketFrameProjection
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if len(data) < 1 || len(data) > generatedAVEffectiveJSONBytes {
		return empty, ErrTimelineLimit
	}
	// Enforce the existing raw envelope/entry budget before allocating the
	// typed record slice; this call grants no clock or measured-input facts.
	if _, err := ParseGeneratedAVAssociationProjectionEnvelope(ctx, data); err != nil {
		return empty, err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil || len(root) != 4 {
		return empty, ErrTimelineProbe
	}
	for _, key := range []string{"packets_and_frames", "programs", "stream_groups", "streams"} {
		raw, present := root[key]
		if !present || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '[' {
			return empty, ErrTimelineProbe
		}
	}
	var document struct {
		Records  []json.RawMessage `json:"packets_and_frames"`
		Programs []json.RawMessage `json:"programs"`
		Groups   []json.RawMessage `json:"stream_groups"`
		Streams  []json.RawMessage `json:"streams"`
	}
	if generatedBoundsDecodeRecord(data, &document, "packets_and_frames", "programs", "stream_groups", "streams") != nil || len(document.Records) == 0 || len(document.Records) > generatedAVEffectiveFrames || validateGeneratedAVObservedProgram(document.Programs, document.Groups, document.Streams) != nil {
		return empty, ErrTimelineProbe
	}
	result := GeneratedAVPacketFrameProjection{JSONBytes: len(data), JSONSHA256: sha256.Sum256(data)}
	var frames []json.RawMessage
	lastPacketRecord, lastPacketOrdinal, lastPacketStream := -2, -1, -1
	for recordOrdinal, raw := range document.Records {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if !generatedAVObservationNoNullFields(raw) {
			return empty, ErrTimelineProbe
		}
		var selected struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &selected) != nil {
			return empty, ErrTimelineProbe
		}
		switch selected.Type {
		case "packet":
			var packet struct {
				Type     string            `json:"type"`
				Kind     string            `json:"codec_type"`
				Index    *int64            `json:"stream_index"`
				PTS      *int64            `json:"pts"`
				DTS      *int64            `json:"dts"`
				Duration *int64            `json:"duration"`
				Size     string            `json:"size"`
				Position *string           `json:"pos"`
				Hash     string            `json:"data_hash"`
				Flags    string            `json:"flags"`
				Side     []json.RawMessage `json:"side_data_list"`
			}
			if generatedBoundsDecodeRecord(raw, &packet, "type", "codec_type", "stream_index", "pts", "dts", "duration", "size", "pos", "data_hash", "flags", "side_data_list") != nil {
				return empty, ErrTimelineProbe
			}
			if packet.Index == nil || (packet.Kind != "video" || *packet.Index != 0) && (packet.Kind != "audio" || *packet.Index != 1) || packet.Flags != "K__" && packet.Flags != "___" || len(packet.Side) != 1 || !generatedAVObservationNoNullFields(packet.Side[0]) {
				return empty, ErrTimelineProbe
			}
			var side struct {
				Type string `json:"side_data_type"`
			}
			if generatedBoundsDecodeRecord(packet.Side[0], &side, "side_data_type") != nil || side.Type != "MPEGTS Stream ID" {
				return empty, ErrTimelineProbe
			}
			fact := GeneratedAVProjectedPacket{Ordinal: len(result.Packets), RecordOrdinal: recordOrdinal, StreamIndex: int(*packet.Index), Kind: packet.Kind, Flags: packet.Flags}
			var err error
			fact.Size, err = generatedAVPacketFrameDecimal(packet.Size, false)
			if err != nil || fact.Size > generatedAVTransportPESBytes {
				return empty, ErrTimelineProbe
			}
			fact.DataSHA256, err = generatedAVPacketFrameHash(packet.Hash)
			if err != nil {
				return empty, err
			}
			if packet.Position != nil {
				fact.Position, err = generatedAVPacketFrameDecimal(*packet.Position, true)
				if err != nil || fact.Position >= generatedAVTransportBytes {
					return empty, ErrTimelineProbe
				}
				fact.PositionKnown = true
			}
			for _, clock := range []*int64{packet.PTS, packet.DTS, packet.Duration} {
				if clock != nil && !generatedInputClockBounded(*clock, 1, 90000) {
					return empty, ErrTimelineProbe
				}
			}
			if packet.PTS != nil {
				fact.PTSKnown, fact.PTS = true, *packet.PTS
			}
			if packet.DTS != nil {
				fact.DTSKnown, fact.DTS = true, *packet.DTS
			}
			if packet.Duration != nil {
				if *packet.Duration <= 0 {
					return empty, ErrTimelineProbe
				}
				fact.DurationKnown, fact.Duration = true, *packet.Duration
			}
			if len(result.Packets) >= generatedAVTransportRecords {
				return empty, ErrTimelineLimit
			}
			result.Packets = append(result.Packets, fact)
			lastPacketRecord, lastPacketOrdinal, lastPacketStream = recordOrdinal, fact.Ordinal, fact.StreamIndex
		case "frame":
			var frame struct {
				Type     string  `json:"type"`
				Kind     string  `json:"media_type"`
				Index    *int64  `json:"stream_index"`
				Size     string  `json:"pkt_size"`
				Position *string `json:"pkt_pos"`
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &frame) != nil || json.Unmarshal(raw, &fields) != nil || frame.Index == nil || (frame.Kind != "video" || *frame.Index != 0) && (frame.Kind != "audio" || *frame.Index != 1) {
				return empty, ErrTimelineProbe
			}
			allowed := map[string]bool{"type": true, "media_type": true, "stream_index": true, "pts": true, "pkt_dts": true, "duration": true, "nb_samples": true, "key_frame": true, "pkt_pos": true, "pkt_size": true, "side_data_list": true}
			for key := range fields {
				if !allowed[key] {
					return empty, ErrTimelineProbe
				}
			}
			fact := GeneratedAVProjectedFrameOrigin{FrameOrdinal: len(frames), RecordOrdinal: recordOrdinal, StreamIndex: int(*frame.Index), EmissionNeighborPacketOrdinal: -1}
			var err error
			fact.PacketSize, err = generatedAVPacketFrameDecimal(frame.Size, false)
			if err != nil || fact.PacketSize > generatedAVTransportPESBytes {
				return empty, ErrTimelineProbe
			}
			if frame.Position != nil {
				fact.PacketPosition, err = generatedAVPacketFrameDecimal(*frame.Position, true)
				if err != nil || fact.PacketPosition >= generatedAVTransportBytes {
					return empty, ErrTimelineProbe
				}
				fact.PacketPositionKnown = true
			}
			if lastPacketRecord == recordOrdinal-1 && lastPacketStream == fact.StreamIndex {
				fact.EmissionNeighborKnown, fact.EmissionNeighborPacketOrdinal = true, lastPacketOrdinal
			}
			delete(fields, "type")
			delete(fields, "pkt_pos")
			delete(fields, "pkt_size")
			mapped, err := json.Marshal(fields)
			if err != nil {
				return empty, err
			}
			frames = append(frames, mapped)
			result.FrameOrigins = append(result.FrameOrigins, fact)
		default:
			return empty, ErrTimelineProbe
		}
	}
	if len(result.Packets) == 0 || len(frames) == 0 {
		return empty, ErrTimelineProbe
	}
	observationJSON, err := json.Marshal(struct {
		Frames   []json.RawMessage `json:"frames"`
		Streams  []json.RawMessage `json:"streams"`
		Programs []json.RawMessage `json:"programs"`
		Groups   []json.RawMessage `json:"stream_groups"`
	}{frames, document.Streams, document.Programs, document.Groups})
	if err != nil {
		return empty, err
	}
	result.Observation, err = ParseGeneratedAVDecodedObservation(ctx, observationJSON)
	if err != nil {
		return empty, err
	}
	if result.Observation.Video.StreamIndex != 0 || result.Observation.Audio.StreamIndex != 1 || result.Observation.Video.TimeBase != (GeneratedRational{Num: 1, Den: 90000}) || result.Observation.Audio.TimeBase != (GeneratedRational{Num: 1, Den: 90000}) {
		return empty, ErrTimelineProbe
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	result.SchemaParsed = true
	return result, nil
}
