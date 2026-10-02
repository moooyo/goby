package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// This fixture has actual TS envelopes and full ADTS bytes, but opaque codec
// payloads. It tests association mechanics and never claims decoder/content proof.
func generatedAVPacketFrameTestFixture(t *testing.T, repeatedAudio bool) ([]byte, GeneratedAVTransportDiagnostic) {
	t.Helper()
	return generatedAVPacketFrameTestFixtureProfile(t, repeatedAudio, false)
}

func generatedAVPacketFrameTestFixtureProfile(t *testing.T, repeatedAudio, repeatedVideo bool) ([]byte, GeneratedAVTransportDiagnostic) {
	t.Helper()
	pat := generatedAVTransportTestSection([]byte{0, 0xb0, 0, 0x12, 0x34, 0xc1, 0, 0, 0, 1, 0xf0, 0})
	pmt := generatedAVTransportTestSection([]byte{2, 0xb0, 0, 0, 1, 0xc1, 0, 0, 0xe1, 0, 0xf0, 0, 0x1b, 0xe1, 0, 0xf0, 0, 0x0f, 0xe1, 1, 0xf0, 0})
	ts := generatedAVTransportTestPacket(0, true, 0, append([]byte{0}, pat...))
	ts = append(ts, generatedAVTransportTestPacket(0x1000, true, 0, append([]byte{0}, pmt...))...)
	ts = append(ts, generatedAVTransportTestPacket(0x100, true, 0, generatedAVTransportTestPES(0xe0, 1920, nil, []byte{0, 0, 1, 0x65, 0x88}, true))...)
	if repeatedVideo {
		ts = append(ts, generatedAVTransportTestPacket(0x100, true, 1, generatedAVTransportTestPES(0xe0, 5670, nil, []byte{0, 0, 1, 0x65, 0x88}, true))...)
	}
	first, second := generatedAVTransportTestADTS(2), generatedAVTransportTestADTS(2)
	if !repeatedAudio {
		second[7] = 0x56
	}
	ts = append(ts, generatedAVTransportTestPacket(0x101, true, 0, generatedAVTransportTestPES(0xc0, 0, nil, append(first, second...), false))...)
	transport, err := ParseGeneratedAVTransportDiagnostic(context.Background(), bytes.NewReader(ts), int64(len(ts)))
	if err != nil {
		t.Fatal(err)
	}
	video, audio := transport.PES[0], transport.PES[len(transport.PES)-1]
	streams := `[ {"index":0,"codec_name":"h264","codec_type":"video","time_base":"1/90000"}, {"index":1,"codec_name":"aac","codec_type":"audio","sample_fmt":"fltp","sample_rate":"48000","channels":2,"channel_layout":"stereo","time_base":"1/90000"} ]`
	data := fmt.Sprintf(`{"packets_and_frames":[
{"type":"packet","codec_type":"video","stream_index":0,"pts":1920,"dts":1920,"size":"%d","pos":"%d","flags":"K__","data_hash":"SHA256:%x","side_data_list":[{"side_data_type":"MPEGTS Stream ID"}]},
{"type":"frame","media_type":"video","stream_index":0,"pts":1920,"pkt_dts":1920,"key_frame":1,"pkt_size":"%d","pkt_pos":"%d","side_data_list":[{"side_data_type":"H.26[45] User Data Unregistered SEI message"}]},
{"type":"packet","codec_type":"audio","stream_index":1,"pts":0,"dts":0,"size":"%d","pos":"%d","flags":"K__","data_hash":"SHA256:%x","side_data_list":[{"side_data_type":"MPEGTS Stream ID"}]},
{"type":"frame","media_type":"audio","stream_index":1,"pts":0,"pkt_dts":0,"key_frame":1,"nb_samples":1024,"pkt_size":"%d","pkt_pos":"%d"},
{"type":"packet","codec_type":"audio","stream_index":1,"size":"%d","flags":"K__","data_hash":"SHA256:%x","side_data_list":[{"side_data_type":"MPEGTS Stream ID"}]},
{"type":"frame","media_type":"audio","stream_index":1,"key_frame":1,"nb_samples":1024,"pkt_size":"%d"}
],"programs":[{"streams":%s}],"stream_groups":[],"streams":%s}`,
		video.PayloadBytes, video.FirstTransportOffset, video.PayloadSHA256, video.PayloadBytes, video.FirstTransportOffset,
		transport.ADTS[0].Bytes, audio.FirstTransportOffset, transport.ADTS[0].SHA256, transport.ADTS[0].Bytes, audio.FirstTransportOffset,
		transport.ADTS[1].Bytes, transport.ADTS[1].SHA256, transport.ADTS[1].Bytes, streams, streams)
	return []byte(data), transport
}

func generatedAVPacketFrameTestReplaceRoot(t *testing.T, data []byte, key string, value json.RawMessage) string {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	root[key] = value
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestGeneratedAVPacketFrameProjectionPreservesUnknownsAndPureInputBoundary(t *testing.T) {
	data, transport := generatedAVPacketFrameTestFixture(t, false)
	projection, err := ParseGeneratedAVPacketFrameProjection(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.SchemaParsed || projection.Complete || projection.MeasuredInputKnown || projection.MeasuredInputSHA256 != ([32]byte{}) || projection.measurement != nil || projection.Qualified || len(projection.Packets) != 3 || len(projection.FrameOrigins) != 3 {
		t.Fatal("pure mixed parsing acquired a measured-input or qualification claim")
	}
	unknown := projection.Observation.Frames[2]
	origin := projection.FrameOrigins[2]
	if unknown.PTSKnown || unknown.DTSKnown || unknown.DurationKnown || unknown.EndKnown || origin.PacketPositionKnown || !origin.EmissionNeighborKnown || origin.EmissionNeighborPacketOrdinal != 2 {
		t.Fatal("emission adjacency filled unknown returned clocks or decoded origin")
	}
	if projection.Observation.Audio.Samples != 2048 || projection.Observation.NativeClockComplete || projection.Observation.Audio.AggregateEndKnown || projection.Observation.Audio.GapFactsKnown || projection.Observation.Complete {
		t.Fatal("sample counts acquired a native timeline or decoder completion")
	}
	association, err := AssociateGeneratedAVPacketFrameTransport(context.Background(), projection, transport)
	if err != nil {
		t.Fatal(err)
	}
	if !association.CandidateByteSequenceEqual || association.SameHeldInput || association.PacketUnitBindingComplete || association.ReturnedPositionCandidatesComplete || association.DecodedFrameOriginComplete || association.ContentBound || association.Qualified {
		t.Fatal("pure byte matches became same-held binding, complete origin or content")
	}
	for _, candidate := range association.PacketUnits {
		if candidate.Status != GeneratedAVAssociationUnique {
			t.Fatal("different complete physical units lost their unique byte candidates")
		}
	}
	if association.FramePackets[2].Status != GeneratedAVAssociationMissing || association.FramePackets[2].PacketOrdinal != -1 {
		t.Fatal("missing returned position was selected by convenient packet ordinal")
	}
	projection.Complete, projection.MeasuredInputKnown = true, true
	projection.MeasuredInputSHA256, projection.MeasuredInputBytes = transport.SHA256, transport.Bytes
	projection.MeasuredInputIdentity = "public-flags-without-private-receipt"
	association, err = AssociateGeneratedAVPacketFrameTransport(context.Background(), projection, transport)
	if err != nil || association.SameHeldInput || association.PacketUnitBindingComplete {
		t.Fatal("hand-filled public flags replaced a joined measurement seal")
	}
}

func TestGeneratedAVPacketFrameProjectionRejectsUnknownNullDuplicateAndConflictingDescriptions(t *testing.T) {
	data, _ := generatedAVPacketFrameTestFixture(t, false)
	text := string(data)
	cases := map[string]string{
		"unknown_root":      strings.Replace(text, `"packets_and_frames":`, `"unexpected":0,"packets_and_frames":`, 1),
		"unknown_packet":    strings.Replace(text, `"type":"packet"`, `"type":"packet","unknown":0`, 1),
		"unknown_frame":     strings.Replace(text, `"type":"frame"`, `"type":"frame","unknown":0`, 1),
		"null_type":         strings.Replace(text, `"type":"packet"`, `"type":null`, 1),
		"duplicate_type":    strings.Replace(text, `"type":"packet"`, `"type":"packet","type":"frame"`, 1),
		"unknown_type":      strings.Replace(text, `"type":"packet"`, `"type":"other"`, 1),
		"null_programs":     generatedAVPacketFrameTestReplaceRoot(t, data, "programs", json.RawMessage(`null`)),
		"null_groups":       strings.Replace(text, `"stream_groups":[]`, `"stream_groups":null`, 1),
		"nonempty_groups":   strings.Replace(text, `"stream_groups":[]`, `"stream_groups":[{}]`, 1),
		"nested_null":       generatedAVPacketFrameTestReplaceRoot(t, data, "programs", json.RawMessage(`[{"streams":null}]`)),
		"nested_conflict":   strings.Replace(text, `"time_base":"1/90000"`, `"time_base":"1/45000"`, 1),
		"nested_duplicate":  strings.Replace(text, `"index":0,"codec_name"`, `"index":0,"index":0,"codec_name"`, 1),
		"size_wrong_type":   strings.Replace(text, `"size":"5"`, `"size":5`, 1),
		"noncanonical_pos":  strings.Replace(text, `"pos":"376"`, `"pos":"0376"`, 1),
		"negative_pos":      strings.Replace(text, `"pos":"376"`, `"pos":"-1"`, 1),
		"null_origin":       strings.Replace(text, `"pkt_pos":"376"`, `"pkt_pos":null`, 1),
		"bad_hash_prefix":   strings.Replace(text, `SHA256:`, `SHA512:`, 1),
		"packet_side_extra": strings.Replace(text, `"side_data_type":"MPEGTS Stream ID"`, `"side_data_type":"MPEGTS Stream ID","id":192`, 1),
	}
	for name, changed := range cases {
		t.Run(name, func(t *testing.T) {
			if changed == text {
				t.Fatal("negative fixture did not change actual evidence")
			}
			projection, err := ParseGeneratedAVPacketFrameProjection(context.Background(), []byte(changed))
			if err == nil || projection.SchemaParsed || projection.Complete || projection.Qualified {
				t.Fatal("unknown/null/duplicate/conflicting projection acquired evidence")
			}
		})
	}
	if _, err := ParseGeneratedAVPacketFrameProjection(nil, data); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal("nil context accepted")
	}
}

func TestGeneratedAVPacketFrameAssociationKeepsAmbiguityAndContradictions(t *testing.T) {
	data, transport := generatedAVPacketFrameTestFixture(t, true)
	projection, err := ParseGeneratedAVPacketFrameProjection(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	association, err := AssociateGeneratedAVPacketFrameTransport(context.Background(), projection, transport)
	if err != nil || !association.CandidateByteSequenceEqual || association.PacketUnits[1].Status != GeneratedAVAssociationUnique || association.PacketUnits[2].Status != GeneratedAVAssociationAmbiguous || association.PacketUnits[2].CandidateCount != 2 || association.PacketUnits[2].ADTSIndex != -1 || association.PacketUnitBindingComplete {
		t.Fatal("ordered repeated payloads selected a convenient physical unit")
	}
	projection.Packets[0].Position++
	association, err = AssociateGeneratedAVPacketFrameTransport(context.Background(), projection, transport)
	if err != nil || association.PacketUnits[0].Status != GeneratedAVAssociationContradictory || association.PacketUnitBindingComplete {
		t.Fatal("changed physical position was accepted through equal hash/count")
	}
	projection.Packets[0].Position--
	projection.FrameOrigins[0].PacketSize++
	association, err = AssociateGeneratedAVPacketFrameTransport(context.Background(), projection, transport)
	if err != nil || association.FramePackets[0].Status != GeneratedAVAssociationContradictory || association.ReturnedPositionCandidatesComplete || association.DecodedFrameOriginComplete {
		t.Fatal("returned frame size conflict acquired an origin claim")
	}
}

func TestGeneratedAVPacketFrameAssociationDoesNotSelectRepeatedVideoByClockOrOrdinal(t *testing.T) {
	data, transport := generatedAVPacketFrameTestFixtureProfile(t, false, true)
	projection, err := ParseGeneratedAVPacketFrameProjection(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	projection.Packets[0].PositionKnown = false
	association, err := AssociateGeneratedAVPacketFrameTransport(context.Background(), projection, transport)
	if err != nil || association.PacketUnits[0].CandidateCount != 2 || association.PacketUnits[0].Status != GeneratedAVAssociationAmbiguous || association.PacketUnits[0].PESIndex != -1 || association.PacketUnitBindingComplete {
		t.Fatal("a repeated video payload lacking position was selected by PTS or ordinal")
	}
}

func generatedAVPacketFrameTestSeal(t *testing.T, projection *GeneratedAVPacketFrameProjection, transport GeneratedAVTransportDiagnostic) {
	t.Helper()
	// This private synthetic seal tests tamper detection only; it does not
	// stand in for a decoder process or actual held-file measurement.
	projection.Complete, projection.MeasuredInputKnown = true, true
	projection.MeasuredInputBytes, projection.MeasuredInputSHA256 = transport.Bytes, transport.SHA256
	projection.Observation.Complete, projection.Observation.InputBytes, projection.Observation.InputSHA256 = true, transport.Bytes, [][32]byte{transport.SHA256}
	projection.MeasuredInputIdentity, projection.MeasuredSourceIdentity = "synthetic-input", "synthetic-source"
	projection.MeasuredSourceSHA256 = [32]byte{1}
	seal := &generatedAVPacketFrameMeasurementSeal{jsonSHA: projection.JSONSHA256, inputSHA: transport.SHA256, sourceSHA: projection.MeasuredSourceSHA256,
		inputBytes: transport.Bytes, inputIdentity: projection.MeasuredInputIdentity, sourceIdentity: projection.MeasuredSourceIdentity}
	var err error
	seal.factsSHA, err = generatedAVPacketFrameFactsFingerprint(*projection)
	if err != nil {
		t.Fatal(err)
	}
	seal.transportFactsSHA, err = generatedAVPacketFrameFactsFingerprint(transport)
	if err != nil {
		t.Fatal(err)
	}
	projection.measurement = seal
}

func TestGeneratedAVPacketFrameAssociationRejectsChangedSealedTypedFacts(t *testing.T) {
	data, originalTransport := generatedAVPacketFrameTestFixture(t, false)
	for _, mutation := range []string{"packet_hash", "frame_origin", "frame_clock", "physical_hash", "physical_offset", "whole_input", "oversized_side"} {
		t.Run(mutation, func(t *testing.T) {
			projection, err := ParseGeneratedAVPacketFrameProjection(context.Background(), data)
			if err != nil {
				t.Fatal(err)
			}
			encodedTransport, err := json.Marshal(originalTransport)
			if err != nil {
				t.Fatal(err)
			}
			var transport GeneratedAVTransportDiagnostic
			if err := json.Unmarshal(encodedTransport, &transport); err != nil {
				t.Fatal(err)
			}
			generatedAVPacketFrameTestSeal(t, &projection, transport)
			if _, err := AssociateGeneratedAVPacketFrameTransport(context.Background(), projection, transport); err != nil {
				t.Fatal("unchanged private typed-facts seal was rejected")
			}
			switch mutation {
			case "packet_hash":
				projection.Packets[0].DataSHA256[0] ^= 1
			case "frame_origin":
				projection.FrameOrigins[0].PacketPosition++
			case "frame_clock":
				projection.Observation.Frames[0].PTS++
			case "physical_hash":
				transport.ADTS[0].SHA256[0] ^= 1
			case "physical_offset":
				transport.PES[0].FirstTransportOffset++
			case "whole_input":
				projection.MeasuredInputSHA256[0] ^= 1
			case "oversized_side":
				projection.Observation.Frames[0].SideData[0].Type = strings.Repeat("x", (4<<20)+1)
			}
			if _, err := AssociateGeneratedAVPacketFrameTransport(context.Background(), projection, transport); !errors.Is(err, ErrInvalidInput) {
				t.Fatal("mutable public facts retained a stale joined same-input claim")
			}
		})
	}
}
