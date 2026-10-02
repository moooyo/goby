package transcode

import "context"

type GeneratedAVAssociationCandidateStatus string

const (
	GeneratedAVAssociationUnique        GeneratedAVAssociationCandidateStatus = "unique"
	GeneratedAVAssociationAmbiguous     GeneratedAVAssociationCandidateStatus = "ambiguous"
	GeneratedAVAssociationMissing       GeneratedAVAssociationCandidateStatus = "missing"
	GeneratedAVAssociationContradictory GeneratedAVAssociationCandidateStatus = "contradictory"
)

// GeneratedAVPacketUnitCandidate compares an actual packet hash/size with full
// ADTS units or AVC PES payloads. Order alone never selects a repeated candidate.
type GeneratedAVPacketUnitCandidate struct {
	PacketOrdinal, CandidateCount, PESIndex, ADTSIndex int
	Status                                             GeneratedAVAssociationCandidateStatus
	Representation                                     string
	UnitReused                                         bool
}

// GeneratedAVFramePacketCandidate is a returned-position candidate only. It
// asserts neither exclusive codec consumption nor selected-source PCM content.
type GeneratedAVFramePacketCandidate struct {
	FrameOrdinal, CandidateCount, PacketOrdinal int
	Status                                      GeneratedAVAssociationCandidateStatus
}

// GeneratedAVPacketFrameTransportAssociation keeps pure byte candidates separate
// from same-held input evidence. PacketUnitBindingComplete requires the private
// joined measurement seal and injective coverage of every physical encoded unit.
// DecodedFrameOriginComplete, ContentBound and Qualified stay false in this stage.
type GeneratedAVPacketFrameTransportAssociation struct {
	Qualified, SameHeldInput, CandidateByteSequenceEqual bool
	PacketUnitBindingComplete                            bool
	ReturnedPositionCandidatesComplete                   bool
	DecodedFrameOriginComplete, ContentBound             bool
	PacketUnits                                          []GeneratedAVPacketUnitCandidate
	FramePackets                                         []GeneratedAVFramePacketCandidate
}

type generatedAVPhysicalPacketUnit struct {
	kind                  string
	pes, adts, indexInPES int
	bytes, position       int64
	hash                  [32]byte
	ptsKnown, dtsKnown    bool
	pts, dts              uint64
}

func generatedAVPacketFrameHasJoinedSeal(projection GeneratedAVPacketFrameProjection) bool {
	seal := projection.measurement
	fingerprint, err := generatedAVPacketFrameFactsFingerprint(projection)
	return seal != nil && projection.Complete && projection.MeasuredInputKnown &&
		err == nil && seal.factsSHA == fingerprint &&
		projection.Observation.Complete && projection.Observation.InputBytes == seal.inputBytes && len(projection.Observation.InputSHA256) == 1 && projection.Observation.InputSHA256[0] == seal.inputSHA &&
		seal.jsonSHA == projection.JSONSHA256 && seal.inputSHA == projection.MeasuredInputSHA256 && seal.sourceSHA == projection.MeasuredSourceSHA256 &&
		seal.inputBytes == projection.MeasuredInputBytes && seal.inputIdentity == projection.MeasuredInputIdentity && seal.sourceIdentity == projection.MeasuredSourceIdentity &&
		seal.inputSHA != ([32]byte{}) && seal.sourceSHA != ([32]byte{}) && seal.inputBytes > 0 && seal.inputIdentity != "" && seal.sourceIdentity != ""
}

// AssociateGeneratedAVPacketFrameTransport reports candidates from actual
// projected hashes, sizes, kinds and positions. A pure parser has no same-input
// seal: even matching payloads and manually filled public receipt flags cannot
// establish same-held PacketUnitBindingComplete. Missing/ambiguous/contradictory
// origins are retained as diagnostic states, never repaired from emission order.
// The caller must keep all projection/transport slices read-only during this
// call. Sequential edits of sealed facts are rejected; concurrent mutation is
// unsupported. A measured wrapper keeps both borrowed inputs held and fences
// their actual identities before and after this association call.
func AssociateGeneratedAVPacketFrameTransport(ctx context.Context, projection GeneratedAVPacketFrameProjection, transport GeneratedAVTransportDiagnostic) (GeneratedAVPacketFrameTransportAssociation, error) {
	var empty GeneratedAVPacketFrameTransportAssociation
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !projection.SchemaParsed || projection.Qualified || !projection.Observation.FramesParsed || transport.Qualified || !transport.Complete ||
		transport.Bytes < 1 || transport.Bytes > generatedAVTransportBytes || transport.SHA256 == ([32]byte{}) || len(projection.Packets) < 1 || len(projection.Packets) > generatedAVTransportRecords ||
		len(projection.FrameOrigins) != len(projection.Observation.Frames) || len(transport.PES) > generatedAVTransportRecords || len(transport.ADTS) > generatedAVTransportRecords {
		return empty, ErrInvalidInput
	}
	result := GeneratedAVPacketFrameTransportAssociation{CandidateByteSequenceEqual: true, ReturnedPositionCandidatesComplete: true}
	if projection.measurement != nil {
		transportFingerprint, err := generatedAVPacketFrameFactsFingerprint(transport)
		if err != nil || !generatedAVPacketFrameHasJoinedSeal(projection) || projection.MeasuredInputSHA256 != transport.SHA256 || projection.MeasuredInputBytes != transport.Bytes || projection.measurement.transportFactsSHA != transportFingerprint {
			return empty, ErrInvalidInput
		}
		result.SameHeldInput = true
	}
	var units []generatedAVPhysicalPacketUnit
	var videoUnits, audioUnits []int
	for pesIndex, pes := range transport.PES {
		if pes.Kind != "video" {
			continue
		}
		if pes.PayloadBytes < 1 || pes.PayloadBytes > generatedAVTransportPESBytes || pes.FirstTransportOffset < 0 || pes.FirstTransportOffset >= transport.Bytes || pes.PayloadSHA256 == ([32]byte{}) {
			return empty, ErrInvalidInput
		}
		videoUnits = append(videoUnits, len(units))
		units = append(units, generatedAVPhysicalPacketUnit{kind: "video", pes: pesIndex, adts: -1, bytes: int64(pes.PayloadBytes), position: pes.FirstTransportOffset, hash: pes.PayloadSHA256, ptsKnown: pes.PTSKnown, dtsKnown: pes.DTSKnown, pts: pes.PTS33, dts: pes.DTS33})
	}
	for adtsIndex, unit := range transport.ADTS {
		if unit.PESIndex < 0 || unit.PESIndex >= len(transport.PES) || unit.IndexInPES < 0 || unit.Bytes < 8 || unit.Bytes > generatedAVTransportPESBytes || unit.SHA256 == ([32]byte{}) {
			return empty, ErrInvalidInput
		}
		pes := transport.PES[unit.PESIndex]
		if pes.Kind != "audio" || pes.FirstTransportOffset < 0 || pes.FirstTransportOffset >= transport.Bytes {
			return empty, ErrInvalidInput
		}
		audioUnits = append(audioUnits, len(units))
		units = append(units, generatedAVPhysicalPacketUnit{kind: "audio", pes: unit.PESIndex, adts: adtsIndex, indexInPES: unit.IndexInPES, bytes: int64(unit.Bytes), position: pes.FirstTransportOffset, hash: unit.SHA256, ptsKnown: pes.PTSKnown, dtsKnown: pes.DTSKnown, pts: pes.PTS33, dts: pes.DTS33})
	}
	if len(videoUnits) == 0 || len(audioUnits) == 0 || len(units) > generatedAVTransportRecords*2 {
		return empty, ErrInvalidInput
	}
	videoOrdinal, audioOrdinal := 0, 0
	usedUnits := make(map[int]int)
	allUnique := true
	for packetOrdinal, packet := range projection.Packets {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if packet.Ordinal != packetOrdinal || (packet.Kind != "video" || packet.StreamIndex != 0) && (packet.Kind != "audio" || packet.StreamIndex != 1) || packet.Size < 1 || packet.Size > generatedAVTransportPESBytes {
			return empty, ErrInvalidInput
		}
		ordered, ordinal := videoUnits, videoOrdinal
		if packet.Kind == "audio" {
			ordered, ordinal = audioUnits, audioOrdinal
			audioOrdinal++
		} else {
			videoOrdinal++
		}
		if ordinal >= len(ordered) || units[ordered[ordinal]].hash != packet.DataSHA256 || units[ordered[ordinal]].bytes != packet.Size {
			result.CandidateByteSequenceEqual = false
		}
		candidate := GeneratedAVPacketUnitCandidate{PacketOrdinal: packetOrdinal, PESIndex: -1, ADTSIndex: -1, Status: GeneratedAVAssociationMissing}
		if packet.Kind == "audio" {
			candidate.Representation = "whole_adts_unit_including_header"
		} else {
			candidate.Representation = "whole_avc_pes_payload"
		}
		baseMatches, selected := 0, -1
		for unitIndex, unit := range units {
			if unit.kind != packet.Kind || unit.bytes != packet.Size || unit.hash != packet.DataSHA256 {
				continue
			}
			baseMatches++
			if packet.PositionKnown && (unit.position != packet.Position || unit.kind == "audio" && unit.indexInPES != 0) {
				continue
			}
			candidate.CandidateCount++
			selected = unitIndex
		}
		switch {
		case candidate.CandidateCount > 1:
			candidate.Status = GeneratedAVAssociationAmbiguous
		case candidate.CandidateCount == 0 && baseMatches > 0:
			candidate.Status = GeneratedAVAssociationContradictory
		case candidate.CandidateCount == 1:
			unit := units[selected]
			candidate.Status, candidate.PESIndex, candidate.ADTSIndex = GeneratedAVAssociationUnique, unit.pes, unit.adts
			// Only physical PES clock fields are compared, at their actual
			// head unit. No derived later-AU clock is assigned or used to
			// disambiguate a repeated hash lacking a returned position.
			if packet.PTSKnown && (unit.kind == "audio" && unit.indexInPES != 0 || !unit.ptsKnown || packet.PTS < 0 || uint64(packet.PTS) != unit.pts) ||
				packet.DTSKnown && unit.dtsKnown && (unit.kind == "audio" && unit.indexInPES != 0 || packet.DTS < 0 || uint64(packet.DTS) != unit.dts) {
				candidate.Status = GeneratedAVAssociationContradictory
			}
			if candidate.Status == GeneratedAVAssociationUnique {
				if prior, present := usedUnits[selected]; present {
					candidate.Status, candidate.UnitReused = GeneratedAVAssociationAmbiguous, true
					result.PacketUnits[prior].Status, result.PacketUnits[prior].UnitReused = GeneratedAVAssociationAmbiguous, true
					allUnique = false
				} else {
					usedUnits[selected] = packetOrdinal
				}
			}
		}
		allUnique = allUnique && candidate.Status == GeneratedAVAssociationUnique
		result.PacketUnits = append(result.PacketUnits, candidate)
	}
	result.CandidateByteSequenceEqual = result.CandidateByteSequenceEqual && videoOrdinal == len(videoUnits) && audioOrdinal == len(audioUnits)
	result.PacketUnitBindingComplete = result.SameHeldInput && allUnique && len(usedUnits) == len(units) && len(projection.Packets) == len(units)
	for frameOrdinal, origin := range projection.FrameOrigins {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		frame := projection.Observation.Frames[frameOrdinal]
		if origin.FrameOrdinal != frameOrdinal || origin.StreamIndex != frame.StreamIndex {
			return empty, ErrInvalidInput
		}
		candidate := GeneratedAVFramePacketCandidate{FrameOrdinal: frameOrdinal, PacketOrdinal: -1, Status: GeneratedAVAssociationMissing}
		if origin.PacketPositionKnown {
			positionMatches := 0
			for packetOrdinal, packet := range projection.Packets {
				if packet.StreamIndex != origin.StreamIndex || !packet.PositionKnown || packet.Position != origin.PacketPosition {
					continue
				}
				positionMatches++
				if packet.Size != origin.PacketSize {
					continue
				}
				candidate.CandidateCount++
				candidate.PacketOrdinal = packetOrdinal
			}
			switch {
			case candidate.CandidateCount > 1:
				candidate.Status, candidate.PacketOrdinal = GeneratedAVAssociationAmbiguous, -1
			case candidate.CandidateCount == 0 && positionMatches > 0:
				candidate.Status = GeneratedAVAssociationContradictory
			case candidate.CandidateCount == 1:
				candidate.Status = GeneratedAVAssociationUnique
				packet := projection.Packets[candidate.PacketOrdinal]
				if frame.PTSKnown && packet.PTSKnown && frame.PTS != packet.PTS || frame.DTSKnown && packet.DTSKnown && frame.DTS != packet.DTS {
					candidate.Status = GeneratedAVAssociationContradictory
				}
			}
		}
		result.ReturnedPositionCandidatesComplete = result.ReturnedPositionCandidatesComplete && candidate.Status == GeneratedAVAssociationUnique
		result.FramePackets = append(result.FramePackets, candidate)
	}
	return result, nil
}
