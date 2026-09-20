package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

const mediaEditMP4UDTATestScheme = "urn:mpeg:dash:role:2011"

func TestMediaEditMP4TrackUserDataCanonicalNameAndAllKinds(t *testing.T) {
	roles := []string{"caption", "commentary", "description", "dub", "forced-subtitle"}
	want := mediaEditMP4UserDataProof{NamePresent: true, Name: "Original audio name"}
	for _, role := range roles {
		want.Kinds = append(want.Kinds, mediaEditMP4KindProof{Scheme: mediaEditMP4UDTATestScheme, Value: role})
	}
	for _, order := range [][]int{{0, 1, 2, 3, 4}, {4, 3, 2, 1, 0}, {2, 0, 4, 1, 3}} {
		var children [][]byte
		for _, index := range order {
			children = append(children, mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, roles[index]))
		}
		children = append(children[:2], append([][]byte{containerTestMP4Box("name", []byte(want.Name))}, children[2:]...)...)
		s, box := mediaEditMP4UDTATestScanner(t, bytes.Join(children, nil))
		track := &mediaEditMP4Track{}
		if err := s.mp4TrackUserData(box, 1, track); err != nil {
			t.Fatalf("complete standard roles rejected in order %v: %v", order, err)
		}
		if !reflect.DeepEqual(track.userData, want) {
			t.Fatalf("raw name or canonical complete role set changed: got %#v, want %#v", track.userData, want)
		}
	}
}

func TestMediaEditMP4TrackUserDataNamesAreExactUTF8(t *testing.T) {
	for _, name := range []string{"A", "Audio", "\u4e2d", "\u4e2d\u6587 audio", "e\u0301", "\u00e9", strings.Repeat("a", 4096)} {
		s, box := mediaEditMP4UDTATestScanner(t, containerTestMP4Box("name", []byte(name)))
		track := &mediaEditMP4Track{}
		if err := s.mp4TrackUserData(box, 1, track); err != nil {
			t.Fatalf("valid raw UTF-8 name of %d bytes rejected: %v", len(name), err)
		}
		if !track.userData.NamePresent || track.userData.Name != name || len(track.userData.Kinds) != 0 {
			t.Fatalf("raw name was normalized, dropped or assigned a role: %#v", track.userData)
		}
	}
	// Raw UTF-8 admission does not prove FFmpeg's legacy name projection.
	// The independent stream binding must still reject a lossy projection.
}

func TestMediaEditMP4TrackUserDataDoesNotShareMovieMetadata(t *testing.T) {
	s, box := mediaEditMP4UDTATestScanner(t, bytes.Join([][]byte{
		containerTestMP4Box("name", []byte("Track title")),
		mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, "caption"),
	}, nil))
	s.globalMP4Tags = map[string]string{"title": "Movie title", "name": "Movie name"}
	s.mp4Projections = map[string]string{"major_brand": "isom"}
	s.writer = map[string]string{"test": "unchanged"}
	track := &mediaEditMP4Track{}
	if err := s.mp4TrackUserData(box, 1, track); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.globalMP4Tags, map[string]string{"title": "Movie title", "name": "Movie name"}) ||
		!reflect.DeepEqual(s.mp4Projections, map[string]string{"major_brand": "isom"}) ||
		!reflect.DeepEqual(s.writer, map[string]string{"test": "unchanged"}) {
		t.Fatalf("track user data contaminated a separate projection: %#v, %#v, %#v", s.globalMP4Tags, s.mp4Projections, s.writer)
	}
}

func TestMediaEditMP4TrackKindsRequireCompleteDispositionProjection(t *testing.T) {
	allFlags := []string{"hearing_impaired", "captions", "comment", "visual_impaired", "descriptions", "dub", "forced"}
	for role, enabled := range map[string][]string{
		"caption":         {"hearing_impaired", "captions"},
		"commentary":      {"comment"},
		"description":     {"visual_impaired", "descriptions"},
		"dub":             {"dub"},
		"forced-subtitle": {"forced"},
	} {
		t.Run(role, func(t *testing.T) {
			s, box := mediaEditMP4UDTATestScanner(t, mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, role))
			track := &mediaEditMP4Track{}
			if err := s.mp4TrackUserData(box, 1, track); err != nil {
				t.Fatal(err)
			}
			dispositions := make(map[string]any, len(allFlags))
			for _, flag := range allFlags {
				dispositions[flag] = "0"
			}
			for _, flag := range enabled {
				dispositions[flag] = "1"
			}
			proof := mediaEditContainerProof{MP4Tracks: []mediaEditMP4TrackProof{{ID: 1, UserData: track.userData}}}
			document := mediaEditDocument{Streams: []map[string]any{{"id": "0x1", "disposition": dispositions}}}
			if err := mediaEditValidateMP4UserDataProjection(proof, document); err != nil {
				t.Fatalf("complete standard disposition projection rejected: %v", err)
			}
			for _, flag := range enabled {
				dispositions[flag] = "0"
				if err := mediaEditValidateMP4UserDataProjection(proof, document); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
					t.Fatalf("missing %s from the complete %s role was accepted: %v", flag, role, err)
				}
				dispositions[flag] = "1"
			}
			for _, flag := range allFlags {
				if dispositions[flag] != "0" {
					continue
				}
				dispositions[flag] = "1"
				if err := mediaEditValidateMP4UserDataProjection(proof, document); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
					t.Fatalf("unrepresented %s disposition was accepted: %v", flag, err)
				}
				dispositions[flag] = "0"
			}
		})
	}
}

func TestMediaEditMP4TrackUserDataRejectsAmbiguousNamesAndChildren(t *testing.T) {
	validName := containerTestMP4Box("name", []byte("Track name"))
	cases := map[string][]byte{
		"empty user data":  nil,
		"empty name":       containerTestMP4Box("name"),
		"invalid UTF-8":    containerTestMP4Box("name", []byte{0xff, 0xfe}),
		"truncated UTF-8":  containerTestMP4Box("name", []byte{0xe4, 0xb8}),
		"NUL prefix":       containerTestMP4Box("name", []byte("\x00Hidden")),
		"NUL inside":       containerTestMP4Box("name", []byte("Name\x00Hidden")),
		"NUL suffix":       containerTestMP4Box("name", []byte("Name\x00")),
		"duplicate name":   append(append([]byte{}, validName...), validName...),
		"shadowing name":   append(append([]byte{}, validName...), containerTestMP4Box("name", []byte("Other"))...),
		"truncated header": {0, 0, 0, 8, 'n', 'a', 'm'},
		"truncated child":  validName[:len(validName)-1],
		"trailing byte":    append(append([]byte{}, validName...), 0),
	}
	for _, kind := range []string{"meta", "free", "skip", "uuid", "Xtra", "udta", "\xa9nam"} {
		cases["unproven child "+kind] = append(append([]byte{}, validName...), containerTestMP4Box(kind)...)
	}
	globalMetadata := containerTestMP4Metadata("mdir", nil, containerTestMP4Box("\xa9nam", containerTestMP4Text("Shadow movie")))
	cases["valid movie metadata in track"] = globalMetadata[8:]
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s, box := mediaEditMP4UDTATestScanner(t, body)
			s.globalMP4Tags = map[string]string{"title": "Original movie"}
			if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("ambiguous or unproven user data accepted: %v", err)
			}
			if !reflect.DeepEqual(s.globalMP4Tags, map[string]string{"title": "Original movie"}) {
				t.Fatalf("rejected track metadata altered movie metadata: %#v", s.globalMP4Tags)
			}
		})
	}
}

func TestMediaEditMP4TrackUserDataRejectsLossyKindProjections(t *testing.T) {
	validPayload := mediaEditMP4UDTATestKindPayload(mediaEditMP4UDTATestScheme, "caption")
	withByte := func(offset int, value byte) []byte {
		data := append([]byte{}, validPayload...)
		data[offset] = value
		return containerTestMP4Box("kind", data)
	}
	validKind := containerTestMP4Box("kind", validPayload)
	cases := map[string][]byte{
		"nonzero version":       withByte(0, 1),
		"nonzero high flag":     withByte(1, 1),
		"nonzero low flag":      withByte(3, 1),
		"truncated full box":    containerTestMP4Box("kind", []byte{0, 0, 0}),
		"no strings":            containerTestMP4Box("kind", make([]byte, 4)),
		"empty scheme":          mediaEditMP4UDTATestKind("", "caption"),
		"empty value":           mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, ""),
		"unknown scheme":        mediaEditMP4UDTATestKind("urn:example:role", "caption"),
		"scheme prefix suffix":  mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme+":private", "caption"),
		"scheme case change":    mediaEditMP4UDTATestKind("URN:mpeg:dash:role:2011", "caption"),
		"unknown role":          mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, "main"),
		"role prefix suffix":    mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, "caption-private"),
		"plural role":           mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, "captions"),
		"role case change":      mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, "Caption"),
		"missing value NUL":     containerTestMP4Box("kind", validPayload[:len(validPayload)-1]),
		"missing both NULs":     containerTestMP4Box("kind", append(make([]byte, 4), []byte(mediaEditMP4UDTATestScheme+"caption")...)),
		"extra terminal NUL":    containerTestMP4Box("kind", append(append([]byte{}, validPayload...), 0)),
		"hidden trailing bytes": containerTestMP4Box("kind", append(append([]byte{}, validPayload...), []byte("hidden")...)),
		"duplicate kind":        append(append([]byte{}, validKind...), validKind...),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			s, box := mediaEditMP4UDTATestScanner(t, body)
			if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("kind with an incomplete or colliding disposition projection accepted: %v", err)
			}
		})
	}
}

func TestMediaEditMP4TrackUserDataEnforcesBoundsAndCancellation(t *testing.T) {
	valid := containerTestMP4Box("name", []byte("Track name"))
	t.Run("name byte limit", func(t *testing.T) {
		s, box := mediaEditMP4UDTATestScanner(t, containerTestMP4Box("name", bytes.Repeat([]byte{'a'}, 4097)))
		if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("oversized track name did not exhaust its byte budget: %v", err)
		}
	})
	t.Run("kind byte limit", func(t *testing.T) {
		payload := mediaEditMP4UDTATestKindPayload(mediaEditMP4UDTATestScheme, "caption")
		payload = append(payload, make([]byte, 1025-len(payload))...)
		s, box := mediaEditMP4UDTATestScanner(t, containerTestMP4Box("kind", payload))
		if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("oversized kind did not exhaust its byte budget: %v", err)
		}
	})
	t.Run("six kind boxes", func(t *testing.T) {
		var body []byte
		for _, role := range []string{"caption", "commentary", "description", "dub", "forced-subtitle", "caption"} {
			body = append(body, mediaEditMP4UDTATestKind(mediaEditMP4UDTATestScheme, role)...)
		}
		s, box := mediaEditMP4UDTATestScanner(t, body)
		if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); err == nil {
			t.Fatal("more kinds than the complete admitted vocabulary were accepted")
		}
	})
	t.Run("shared metadata budget", func(t *testing.T) {
		s, box := mediaEditMP4UDTATestScanner(t, valid)
		s.metadata = mediaEditContainerMaxMetadata
		if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("track user data bypassed the shared metadata budget: %v", err)
		}
	})
	t.Run("header budget", func(t *testing.T) {
		s, box := mediaEditMP4UDTATestScanner(t, valid)
		s.headers = mediaEditContainerMaxHeaders
		if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("track user data bypassed the structural header budget: %v", err)
		}
	})
	t.Run("depth budget", func(t *testing.T) {
		s, box := mediaEditMP4UDTATestScanner(t, valid)
		if err := s.mp4TrackUserData(box, mediaEditContainerMaxDepth+1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("track user data bypassed the structural depth budget: %v", err)
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		s, box := mediaEditMP4UDTATestScanner(t, valid)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		s.ctx = ctx
		if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("track user data ignored cancellation: %v", err)
		}
	})
	t.Run("admitted descriptor extent", func(t *testing.T) {
		s, box := mediaEditMP4UDTATestScanner(t, valid)
		s.size = box.end - 1
		if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
			t.Fatalf("track user data read beyond the admitted descriptor: %v", err)
		}
	})
	t.Run("child crosses parent", func(t *testing.T) {
		body := append([]byte{}, valid...)
		binary.BigEndian.PutUint32(body[:4], uint32(len(body)+1))
		s, box := mediaEditMP4UDTATestScanner(t, body)
		if err := s.mp4TrackUserData(box, 1, &mediaEditMP4Track{}); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
			t.Fatalf("child extending into bytes outside its parent was accepted: %v", err)
		}
	})
}

func mediaEditMP4UDTATestKindPayload(scheme, value string) []byte {
	return append(make([]byte, 4), []byte(scheme+"\x00"+value+"\x00")...)
}

func mediaEditMP4UDTATestKind(scheme, value string) []byte {
	return containerTestMP4Box("kind", mediaEditMP4UDTATestKindPayload(scheme, value))
}

func mediaEditMP4UDTATestScanner(t *testing.T, body []byte) (*mediaEditContainerScanner, mediaEditMP4Box) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "track-user-data-*")
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "prefix!"
	data := append([]byte(prefix), body...)
	data = append(data, []byte("outside-the-user-data-extent")...)
	if _, err := file.Write(data); err != nil {
		file.Close()
		t.Fatal(err)
	}
	const borrowedOffset = 3
	if _, err := file.Seek(borrowedOffset, io.SeekStart); err != nil {
		file.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		position, err := file.Seek(0, io.SeekCurrent)
		if err != nil || position != borrowedOffset {
			t.Errorf("user-data parser changed the borrowed descriptor offset: %d, %v", position, err)
		}
		if err := file.Close(); err != nil {
			t.Errorf("close user-data fixture: %v", err)
		}
	})
	s := &mediaEditContainerScanner{ctx: context.Background(), file: file, size: int64(len(data))}
	return s, mediaEditMP4Box{kind: "udta", start: int64(len(prefix)), end: int64(len(prefix) + len(body))}
}
