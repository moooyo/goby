package media

import (
	"errors"
	"testing"
)

func mediaEditMP4BindingFixture() (mediaEditContainerProof, mediaEditContainerProof, mediaEditDocument, mediaEditDocument, []MediaEditStreamEvidence, map[int]mediaEditPacketDigest, map[int]mediaEditPacketDigest) {
	source := mediaEditContainerProof{MP4Tracks: []mediaEditMP4TrackProof{
		{ID: 16, Codec: "avc1", Samples: 2},
		{ID: 32, Codec: "mp4a", Samples: 4, Roll: mediaEditMP4RollProof{Samples: 4, Distance: -1}},
		{ID: 48, Codec: "tx3g", Samples: 1},
	}}
	candidate := mediaEditContainerProof{MP4Tracks: []mediaEditMP4TrackProof{
		{ID: 1, Codec: "avc1", Samples: 2},
		{ID: 2, Codec: "mp4a", Samples: 4, Roll: mediaEditMP4RollProof{Samples: 4, Distance: -1}},
	}}
	sourceDocument := mediaEditDocument{Streams: []map[string]any{{"index": "0", "id": "0x10"}, {"index": "1", "id": "0x20"}, {"index": "2", "id": "0x30"}}}
	candidateDocument := mediaEditDocument{Streams: []map[string]any{{"index": "0", "id": "0x1"}, {"index": "1", "id": "0x2"}}}
	pairs := []MediaEditStreamEvidence{{SourceIndex: 0, CandidateIndex: 0}, {SourceIndex: 1, CandidateIndex: 1}}
	before := map[int]mediaEditPacketDigest{0: {Packets: 2}, 1: {Packets: 4}, 2: {Packets: 1}}
	after := map[int]mediaEditPacketDigest{0: {Packets: 2}, 1: {Packets: 4}}
	return source, candidate, sourceDocument, candidateDocument, pairs, before, after
}

func TestMediaEditContainerProofBindsMP4RollToProbeAndPacketInventory(t *testing.T) {
	source, candidate, sourceDocument, candidateDocument, pairs, before, after := mediaEditMP4BindingFixture()
	bindings, err := mediaEditMP4Bindings(sourceDocument, candidateDocument, 2, pairs, before, after)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := compareMediaEditContainerProofs(source, candidate, bindings)
	if err != nil || len(digest) != 64 {
		t.Fatalf("complete MP4 structural proof = %q, %v", digest, err)
	}
	candidate.MP4Tracks[1].ID = 99
	candidateDocument.Streams[1]["id"] = "0x63"
	bindings, err = mediaEditMP4Bindings(sourceDocument, candidateDocument, 2, pairs, before, after)
	if err != nil {
		t.Fatal(err)
	}
	renumbered, err := compareMediaEditContainerProofs(source, candidate, bindings)
	if err != nil || renumbered != digest {
		t.Fatalf("container-local track renumbering changed semantic proof: %q, %v", renumbered, err)
	}
}

func TestMediaEditContainerProofRejectsUnboundOrChangedMP4Semantics(t *testing.T) {
	for _, name := range []string{"distance", "sample count", "packet count", "unbound track", "duplicate track", "missing track", "duplicate binding", "missing binding", "codec", "absent AAC roll", "unbound removal", "non-subtitle removal", "conflicting removal"} {
		t.Run(name, func(t *testing.T) {
			source, candidate, sourceDocument, candidateDocument, pairs, before, after := mediaEditMP4BindingFixture()
			bindings, err := mediaEditMP4Bindings(sourceDocument, candidateDocument, 2, pairs, before, after)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "distance":
				candidate.MP4Tracks[1].Roll.Distance = -2
			case "sample count":
				candidate.MP4Tracks[1].Samples = 3
				candidate.MP4Tracks[1].Roll.Samples = 3
			case "packet count":
				bindings[1].CandidatePackets = 3
			case "unbound track":
				bindings[1].CandidateID = 99
			case "duplicate track":
				candidate.MP4Tracks[1].ID = 1
			case "missing track":
				candidate.MP4Tracks = candidate.MP4Tracks[:1]
			case "duplicate binding":
				bindings[1] = bindings[0]
			case "missing binding":
				bindings = bindings[:1]
			case "codec":
				candidate.MP4Tracks[1].Codec = "Opus"
			case "absent AAC roll":
				candidate.MP4Tracks[1].Roll = mediaEditMP4RollProof{}
			case "unbound removal":
				source.MP4Tracks[2].ID = 999
			case "non-subtitle removal":
				source.MP4Tracks[2].Codec = "avc1"
			case "conflicting removal":
				bindings[1].RemovedSourceID = 999
			}
			if _, err := compareMediaEditContainerProofs(source, candidate, bindings); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven MP4 semantics accepted: %v", err)
			}
		})
	}
}

func TestMediaEditMP4BindingsRejectAmbiguousProbeIDsAndMissingPackets(t *testing.T) {
	for _, name := range []string{"missing ID", "duplicate ID", "zero ID", "oversized ID", "missing packets", "negative count", "duplicate output inventory"} {
		t.Run(name, func(t *testing.T) {
			_, _, source, candidate, pairs, before, after := mediaEditMP4BindingFixture()
			switch name {
			case "missing ID":
				delete(candidate.Streams[1], "id")
			case "duplicate ID":
				candidate.Streams[1]["id"] = "0x1"
			case "zero ID":
				candidate.Streams[1]["id"] = "0x0"
			case "oversized ID":
				candidate.Streams[1]["id"] = "0x100000000"
			case "missing packets":
				delete(after, 1)
			case "negative count":
				after[1] = mediaEditPacketDigest{Packets: -1}
			case "duplicate output inventory":
				candidate.Streams = append(candidate.Streams, map[string]any{"index": "2", "id": "0x3"})
			}
			if _, err := mediaEditMP4Bindings(source, candidate, 2, pairs, before, after); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("ambiguous binding accepted: %v", err)
			}
		})
	}
}
