package media

import (
	"context"
	"errors"
	"os"
	"testing"
)

func mediaEditMP4UserDataProjectionFixture() (mediaEditContainerProof, mediaEditDocument) {
	kinds := []mediaEditMP4KindProof{}
	for _, value := range []string{"caption", "commentary", "description", "dub", "forced-subtitle"} {
		kinds = append(kinds, mediaEditMP4KindProof{Scheme: mediaEditMP4KindScheme, Value: value})
	}
	proof := mediaEditContainerProof{MP4Tracks: []mediaEditMP4TrackProof{{ID: 1, UserData: mediaEditMP4UserDataProof{NamePresent: true, Name: "Complete track name", Kinds: kinds}}}}
	document := mediaEditDocument{Streams: []map[string]any{{"id": "0x1", "tags": map[string]any{"name": "Complete track name"},
		"disposition": map[string]any{"hearing_impaired": "1", "captions": "1", "comment": "1", "visual_impaired": "1", "descriptions": "1", "dub": "1", "forced": "1"}}}}
	return proof, document
}

func TestMediaEditMP4TrackUserDataBindsCompleteProbeProjection(t *testing.T) {
	proof, document := mediaEditMP4UserDataProjectionFixture()
	if err := mediaEditValidateMP4UserDataProjection(proof, document); err != nil {
		t.Fatalf("complete track user-data projection rejected: %v", err)
	}
	for _, name := range []string{"missing name", "truncated name", "ambiguous title", "partial caption flags", "partial description flags", "missing role flag", "extra role flag", "wrong track", "duplicate track", "absent track"} {
		t.Run(name, func(t *testing.T) {
			proof, document := mediaEditMP4UserDataProjectionFixture()
			tags := document.Streams[0]["tags"].(map[string]any)
			flags := document.Streams[0]["disposition"].(map[string]any)
			switch name {
			case "missing name":
				delete(tags, "name")
			case "truncated name":
				tags["name"] = "Complete"
			case "ambiguous title":
				tags["title"] = "Another title projection"
			case "partial caption flags":
				flags["hearing_impaired"] = "0"
			case "partial description flags":
				flags["visual_impaired"] = "0"
			case "missing role flag":
				delete(flags, "comment")
			case "extra role flag":
				proof.MP4Tracks[0].UserData.Kinds = nil
			case "wrong track":
				document.Streams[0]["id"] = "0x2"
			case "duplicate track":
				proof.MP4Tracks = append(proof.MP4Tracks, proof.MP4Tracks[0])
				document.Streams = append(document.Streams, document.Streams[0])
			case "absent track":
				document.Streams = nil
			}
			if err := mediaEditValidateMP4UserDataProjection(proof, document); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven track projection accepted: %v", err)
			}
		})
	}
}

func TestMediaEditMP4CannotSelectTheLossyRemuxCommand(t *testing.T) {
	args, err := buildMediaEditRemuxArgs(mediaEditDocument{}, SubtitleRemovalOptions{Container: "mp4", StreamIndex: 7})
	if !errors.Is(err, ErrSubtitleRemovalUnsupported) || len(args) != 0 {
		t.Fatalf("MP4 incorrectly selected the generic FFmpeg writer: %#v %v", args, err)
	}
}

func TestMediaEditMP4TrackUserDataParticipatesInStructuralProof(t *testing.T) {
	source, candidate, sourceDocument, candidateDocument, pairs, before, after := mediaEditMP4BindingFixture()
	bindings, err := mediaEditMP4Bindings(sourceDocument, candidateDocument, 2, pairs, before, after)
	if err != nil {
		t.Fatal(err)
	}
	without, err := compareMediaEditContainerProofs(source, candidate, bindings)
	if err != nil {
		t.Fatal(err)
	}
	data := mediaEditMP4UserDataProof{NamePresent: true, Name: "Retained audio", Kinds: []mediaEditMP4KindProof{{Scheme: mediaEditMP4KindScheme, Value: "commentary"}}}
	source.MP4Tracks[1].UserData, candidate.MP4Tracks[1].UserData = data, data
	with, err := compareMediaEditContainerProofs(source, candidate, bindings)
	if err != nil || with == without || len(with) != 64 {
		t.Fatalf("userdata was omitted from structural evidence: %q %q %v", without, with, err)
	}
	for _, mutate := range []func(*mediaEditMP4UserDataProof){
		func(value *mediaEditMP4UserDataProof) { value.Name = "Changed" },
		func(value *mediaEditMP4UserDataProof) { value.NamePresent, value.Name = false, "" },
		func(value *mediaEditMP4UserDataProof) { value.Kinds = nil },
		func(value *mediaEditMP4UserDataProof) {
			value.Kinds = []mediaEditMP4KindProof{{Scheme: mediaEditMP4KindScheme, Value: "dub"}}
		},
	} {
		candidate.MP4Tracks[1].UserData = data
		mutate(&candidate.MP4Tracks[1].UserData)
		if _, err := compareMediaEditContainerProofs(source, candidate, bindings); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
			t.Fatalf("changed track user data was accepted: %v", err)
		}
	}
}

func TestMediaEditMP4TrackUDTARoutesToTrackScopedEvidence(t *testing.T) {
	kind := append([]byte{0, 0, 0, 0}, []byte(mediaEditMP4KindScheme+"\x00commentary\x00")...)
	data := containerTestMP4(containerTestMP4Box("udta", containerTestMP4Box("name", []byte("Track title")), containerTestMP4Box("kind", kind)), nil, nil)
	file, err := os.CreateTemp(t.TempDir(), "track-udta-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	proof, err := mediaEditReadContainerProof(context.Background(), file, int64(len(data)), "mp4")
	if err != nil || len(proof.MP4Tracks) != 1 {
		t.Fatalf("track user-data structural admission failed: %+v %v", proof, err)
	}
	if !proof.MP4Tracks[0].UserData.NamePresent || proof.MP4Tracks[0].UserData.Name != "Track title" || len(proof.MP4Tracks[0].UserData.Kinds) != 1 || proof.MP4Tracks[0].UserData.Kinds[0].Value != "commentary" {
		t.Fatalf("track user data was routed to global metadata: %+v", proof.MP4Tracks[0])
	}
}
