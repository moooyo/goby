package media

import (
	"errors"
	"math"
	"testing"
)

func mediaEditMP4PlanFixture() (mediaEditContainerProof, mediaEditDocument) {
	proof := mediaEditContainerProof{
		MP4Tracks: []mediaEditMP4TrackProof{{ID: 10, Codec: "avc1"}, {ID: 20, Codec: "tx3g"}, {ID: 30, Codec: "tx3g"}},
		MP4Layout: &mediaEditMP4Layout{SourceBytes: 1024, MovieDuration: 2000, Tracks: []mediaEditMP4TrackLayout{
			{ID: 10, Codec: "avc1", Duration: 2000, TypeOffset: 104, BodyOffset: 108, End: 200},
			{ID: 20, Codec: "tx3g", Duration: 1300, TypeOffset: 204, BodyOffset: 208, End: 300},
			{ID: 30, Codec: "tx3g", Duration: 1400, TypeOffset: 304, BodyOffset: 308, End: 400},
		}},
	}
	document := mediaEditDocument{Streams: []map[string]any{
		{"index": "0", "id": "0xa", "codec_type": "video", "codec_name": "h264"},
		{"index": "17", "id": "0x14", "codec_type": "subtitle", "codec_name": "mov_text"},
		{"index": "23", "id": "0x1e", "codec_type": "subtitle", "codec_name": "mov_text"},
	}}
	return proof, document
}

func TestMediaEditMP4StructuralPlanBindsAbsoluteSelectionAndPreservesDuration(t *testing.T) {
	proof, document := mediaEditMP4PlanFixture()
	plan, err := buildMediaEditMP4RemovalPlan(proof, document, 17)
	if err != nil || plan.SourceBytes != 1024 || plan.RemovedTrackID != 20 || plan.TrackTypeOffset != 204 || plan.TrackBodyOffset != 208 || plan.TrackEnd != 300 {
		t.Fatalf("wrong structural removal plan: %+v, %v", plan, err)
	}
	proof.MP4Layout.Tracks[1].BodyOffset += 8
	extended, err := buildMediaEditMP4RemovalPlan(proof, document, 17)
	if err != nil || extended.TrackTypeOffset != 204 || extended.TrackBodyOffset != 216 {
		t.Fatalf("valid extended-size track header rejected: %+v, %v", extended, err)
	}
	proof.MP4Layout.Tracks[1].Duration = 2000
	if _, err := buildMediaEditMP4RemovalPlan(proof, document, 17); err != nil {
		t.Fatalf("removing a co-longest track changed no movie timeline: %v", err)
	}
	proof.MP4Layout.Tracks[1].Duration = 2100
	proof.MP4Layout.MovieDuration = 2100
	if _, err := buildMediaEditMP4RemovalPlan(proof, document, 17); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("plan would require rewriting mvhd duration: %v", err)
	}
}

func TestMediaEditMP4StructuralPlanRejectsUnboundOrAmbiguousSource(t *testing.T) {
	for _, name := range []string{"no layout", "size budget", "no selected stream", "wrong selected codec", "wrong selected type", "missing physical ID", "duplicate layout ID", "duplicate proof ID", "duplicate probe ID", "duplicate probe index", "different codec proof", "overlapping tracks", "bad header size", "out of range", "overflowed header", "empty body", "missing physical track"} {
		t.Run(name, func(t *testing.T) {
			proof, document := mediaEditMP4PlanFixture()
			removed := 17
			switch name {
			case "no layout":
				proof.MP4Layout = nil
			case "size budget":
				proof.MP4Layout.SourceBytes = MaxSubtitleRemovalInputBytes + 1
			case "no selected stream":
				removed = 2
			case "wrong selected codec":
				document.Streams[1]["codec_name"] = "subrip"
			case "wrong selected type":
				document.Streams[1]["codec_type"] = "audio"
			case "missing physical ID":
				document.Streams[1]["id"] = "0x99"
			case "duplicate layout ID":
				proof.MP4Layout.Tracks[1].ID = 10
			case "duplicate proof ID":
				proof.MP4Tracks[2] = proof.MP4Tracks[1]
			case "duplicate probe ID":
				document.Streams[2]["id"] = "0x14"
			case "duplicate probe index":
				document.Streams[2]["index"] = "17"
			case "different codec proof":
				proof.MP4Tracks[1].Codec = "avc1"
			case "overlapping tracks":
				proof.MP4Layout.Tracks[1].TypeOffset, proof.MP4Layout.Tracks[1].BodyOffset = 180, 184
			case "bad header size":
				proof.MP4Layout.Tracks[1].BodyOffset = 209
			case "out of range":
				proof.MP4Layout.Tracks[1].End = 1025
			case "overflowed header":
				proof.MP4Layout.Tracks[1].TypeOffset, proof.MP4Layout.Tracks[1].BodyOffset = math.MaxInt64-2, math.MinInt64+1
			case "empty body":
				proof.MP4Layout.Tracks[1].End = proof.MP4Layout.Tracks[1].BodyOffset
			case "missing physical track":
				proof.MP4Layout.Tracks = proof.MP4Layout.Tracks[:2]
			}
			if _, err := buildMediaEditMP4RemovalPlan(proof, document, removed); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven structural plan accepted: %v", err)
			}
		})
	}
}
