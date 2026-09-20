package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMediaEditContainerAdmissionMatroskaProfile(t *testing.T) {
	chapter := containerTestEBML(0x1043A770, containerTestEBML(0x45B9,
		containerTestEBML(0xB6, containerTestUint(0x73C4, 99), containerTestUint(0x91, 0), containerTestUint(0x92, 1000),
			containerTestEBML(0x80, containerTestEBML(0x85, []byte("Introduction")), containerTestEBML(0x437C, []byte("eng"))))))
	attachment := containerTestEBML(0x1941A469, containerTestEBML(0x61A7,
		containerTestUint(0x46AE, 88), containerTestEBML(0x466E, []byte("font.ttf")),
		containerTestEBML(0x4660, []byte("application/x-truetype-font")), containerTestEBML(0x465C, []byte{1, 2, 3})))
	data := containerTestMKV(nil, nil, nil, chapter, attachment,
		containerTestTag("TITLE", "Example", 0, 0), containerTestTag("COMMENT", "Audio", 0x63C5, 11),
		containerTestTag("COMMENT", "Opening", 0x63C4, 99), containerTestTag("LICENSE", "Example", 0x63C6, 88))
	for _, extension := range []string{"mkv", "mka"} {
		t.Run(extension, func(t *testing.T) {
			writer, err := containerTestAdmit(t, data, extension)
			if err != nil {
				t.Fatal(err)
			}
			if len(writer) != 2 || writer["WritingApp"] != "Original writer" || writer["MuxingApp"] != "Original muxer" {
				t.Fatalf("unexpected writer evidence: %#v", writer)
			}
		})
	}
}

func TestMediaEditContainerAdmissionRejectsUnexposedMatroskaSemantics(t *testing.T) {
	chapter := func(elements ...[]byte) []byte {
		return containerTestEBML(0x1043A770, containerTestEBML(0x45B9, append([][]byte{
			containerTestEBML(0xB6, containerTestUint(0x73C4, 99), containerTestUint(0x91, 0), containerTestUint(0x92, 1000)),
		}, elements...)...))
	}
	cases := map[string][]byte{
		"linked segment":        containerTestMKV(containerTestEBML(0x3CB923, make([]byte, 16)), nil, nil),
		"segment family":        containerTestMKV(containerTestEBML(0x4444, make([]byte, 16)), nil, nil),
		"unknown info":          containerTestMKV(containerTestUint(0x7384, 1), nil, nil),
		"track compression":     containerTestMKV(nil, containerTestEBML(0x6D80, containerTestEBML(0x6240, containerTestEBML(0x5034))), nil),
		"track encryption":      containerTestMKV(nil, containerTestEBML(0x6D80, containerTestEBML(0x6240, containerTestEBML(0x5035))), nil),
		"disabled track":        containerTestMKV(nil, containerTestUint(0xB9, 0), nil),
		"unreported codec name": containerTestMKV(nil, containerTestEBML(0x258688, []byte("Hidden name")), nil),
		"unreported preroll":    containerTestMKV(nil, containerTestUint(0x56BB, 80000000), nil),
		"ordered edition":       containerTestMKV(nil, nil, nil, chapter(containerTestUint(0x45DD, 1))),
		"hidden edition":        containerTestMKV(nil, nil, nil, chapter(containerTestUint(0x45BD, 1))),
		"multiple editions": containerTestMKV(nil, nil, nil, containerTestEBML(0x1043A770,
			containerTestEBML(0x45B9, containerTestEBML(0xB6, containerTestUint(0x73C4, 1), containerTestUint(0x91, 0), containerTestUint(0x92, 1))),
			containerTestEBML(0x45B9))),
		"nested chapter": containerTestMKV(nil, nil, nil, containerTestEBML(0x1043A770, containerTestEBML(0x45B9,
			containerTestEBML(0xB6, containerTestUint(0x73C4, 1), containerTestUint(0x91, 0), containerTestUint(0x92, 1), containerTestEBML(0xB6))))),
		"invalid chapter language": containerTestMKV(nil, nil, nil, containerTestEBML(0x1043A770, containerTestEBML(0x45B9,
			containerTestEBML(0xB6, containerTestUint(0x73C4, 1), containerTestUint(0x91, 0), containerTestUint(0x92, 1),
				containerTestEBML(0x80, containerTestEBML(0x85, []byte("Chapter")), containerTestEBML(0x437C, []byte("fr-FR"))))))),
		"binary tag": containerTestMKV(nil, nil, nil, containerTestEBML(0x1254C367, containerTestEBML(0x7373,
			containerTestEBML(0x67C8, containerTestEBML(0x45A3, []byte("BINARY")), containerTestEBML(0x4485, []byte{1, 2}))))),
		"nested tag": containerTestMKV(nil, nil, nil, containerTestEBML(0x1254C367, containerTestEBML(0x7373,
			containerTestEBML(0x67C8, containerTestEBML(0x45A3, []byte("TITLE")), containerTestEBML(0x4487, []byte("Name")), containerTestEBML(0x67C8))))),
		"duplicate tag":       containerTestMKV(nil, nil, nil, containerTestTag("TITLE", "First", 0, 0), containerTestTag("title", "Second", 0, 0)),
		"dangling tag target": containerTestMKV(nil, nil, nil, containerTestTag("TITLE", "Missing", 0x63C5, 999)),
		"null tag":            containerTestMKV(nil, nil, nil, containerTestTag("TITLE", "First\x00hidden", 0, 0)),
		"unreported block addition": containerTestMKV(nil, nil, containerTestEBML(0xA0,
			containerTestEBML(0xA1, []byte{0x82, 0, 0, 0, 1}), containerTestEBML(0x75A1))),
		"codec state": containerTestMKV(nil, nil, containerTestEBML(0xA0,
			containerTestEBML(0xA1, []byte{0x82, 0, 0, 0, 1}), containerTestEBML(0xA4, []byte{1}))),
		"invisible block":     containerTestMKV(nil, nil, containerTestEBML(0xA3, []byte{0x81, 0, 0, 0x88, 1})),
		"discardable block":   containerTestMKV(nil, nil, containerTestEBML(0xA3, []byte{0x81, 0, 0, 0x81, 1})),
		"unknown block track": containerTestMKV(nil, nil, containerTestEBML(0xA3, []byte{0x83, 0, 0, 0x80, 1})),
		"encrypted block":     containerTestMKV(nil, nil, containerTestEBML(0xAF, []byte{1})),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := containerTestAdmit(t, data, "mkv"); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("expected an unsupported structure, got %v", err)
			}
		})
	}
}

func TestMediaEditContainerAdmissionMatroskaUnknownSizeAndTruncation(t *testing.T) {
	data := containerTestMKV(nil, nil, nil)
	header := containerTestEBML(0x1A45DFA3, containerTestEBML(0x4282, []byte("matroska")))
	segmentBody := bytes.Join([][]byte{containerTestInfo(nil), containerTestTracks(nil), containerTestCluster(nil)}, nil)
	unknownSegment := append(append(append([]byte{}, header...), []byte{0x18, 0x53, 0x80, 0x67, 0xff}...), segmentBody...)
	if _, err := containerTestAdmit(t, unknownSegment, "mkv"); err != nil {
		t.Fatalf("unknown Segment extent ending at EOF should be admitted: %v", err)
	}
	cases := map[string][]byte{
		"truncated header":  data[:3],
		"truncated payload": data[:len(data)-1],
		"trailing bytes":    append(append([]byte{}, data...), 0),
		"duplicate segment": append(append([]byte{}, data...), containerTestEBML(0x18538067)...),
		"unknown cluster extent": append(append(append([]byte{}, header...), []byte{0x18, 0x53, 0x80, 0x67, 0xff}...),
			bytes.Join([][]byte{containerTestInfo(nil), containerTestTracks(nil), {0x1f, 0x43, 0xb6, 0x75, 0xff}}, nil)...),
	}
	for name, malformed := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := containerTestAdmit(t, malformed, "mkv"); err == nil {
				t.Fatal("malformed container was admitted")
			}
		})
	}
}

func TestMediaEditContainerAdmissionRejectsMatroskaMetadataProjectionCollisions(t *testing.T) {
	chapter := containerTestEBML(0x1043A770, containerTestEBML(0x45B9,
		containerTestEBML(0xB6, containerTestUint(0x73C4, 99), containerTestUint(0x91, 0), containerTestUint(0x92, 1000),
			containerTestEBML(0x80, containerTestEBML(0x85, []byte("Original chapter"))))))
	attachment := containerTestEBML(0x1941A469, containerTestEBML(0x61A7,
		containerTestUint(0x46AE, 88), containerTestEBML(0x466E, []byte("font.ttf")),
		containerTestEBML(0x4660, []byte("application/x-truetype-font")), containerTestEBML(0x465C, []byte{1, 2, 3})))
	header := containerTestEBML(0x1A45DFA3, containerTestEBML(0x4282, []byte("matroska")))
	cases := map[string][]byte{
		"global title":           containerTestMKV(containerTestEBML(0x7BA9, []byte("Original")), nil, nil, containerTestTag("TITLE", "Shadow", 0, 0)),
		"global creation":        containerTestMKV(containerTestEBML(0x4461, make([]byte, 8)), nil, nil, containerTestTag("CREATION_TIME", "Shadow", 0, 0)),
		"track name":             containerTestMKV(nil, containerTestEBML(0x536E, []byte("Original track")), nil, containerTestTag("TITLE", "Shadow", 0x63C5, 11)),
		"default track language": containerTestMKV(nil, nil, nil, containerTestTag("LANGUAGE", "fra", 0x63C5, 11)),
		"chapter title":          containerTestMKV(nil, nil, nil, chapter, containerTestTag("TITLE", "Shadow", 0x63C4, 99)),
		"attachment filename":    containerTestMKV(nil, nil, nil, attachment, containerTestTag("FILENAME", "Shadow", 0x63C6, 88)),
		"attachment media type":  containerTestMKV(nil, nil, nil, attachment, containerTestTag("MIMETYPE", "Shadow", 0x63C6, 88)),
		"tag before original header": append(header, containerTestEBML(0x18538067,
			containerTestTag("TITLE", "Shadow", 0, 0), containerTestInfo(containerTestEBML(0x7BA9, []byte("Original"))),
			containerTestTracks(nil), containerTestCluster(nil))...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := containerTestAdmit(t, data, "mkv"); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("expected an ambiguous projection rejection, got %v", err)
			}
		})
	}
}

func TestMediaEditContainerAdmissionMatroskaLacingAndSeekExtents(t *testing.T) {
	for name, test := range map[string]struct {
		block []byte
		valid bool
	}{
		"fixed":         {[]byte{0x81, 0, 0, 0x84, 2, 1, 2, 3, 4, 5, 6}, true},
		"xiph":          {[]byte{0x81, 0, 0, 0x82, 2, 1, 2, 1, 2, 3, 4, 5, 6}, true},
		"ebml":          {[]byte{0x81, 0, 0, 0x86, 2, 0x81, 0xc0, 1, 2, 3, 4, 5, 6}, true},
		"invalid fixed": {[]byte{0x81, 0, 0, 0x84, 2, 1, 2, 3, 4, 5}, false},
		"invalid xiph":  {[]byte{0x81, 0, 0, 0x82, 2, 10, 2, 1}, false},
		"invalid ebml":  {[]byte{0x81, 0, 0, 0x86, 2, 0x81, 0xbd, 1, 2, 3}, false},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := containerTestAdmit(t, containerTestMKV(nil, nil, containerTestEBML(0xA3, test.block)), "mkv")
			if (err == nil) != test.valid {
				t.Fatalf("unexpected lace admission: %v", err)
			}
		})
	}
	for _, position := range []uint64{0, 1} {
		seek := containerTestEBML(0x114D9B74, containerTestEBML(0x4DBB,
			containerTestEBML(0x53AB, []byte{0x15, 0x49, 0xa9, 0x66}), containerTestUint(0x53AC, position)))
		_, err := containerTestAdmit(t, containerTestMKV(nil, nil, nil, seek), "mkv")
		if (err == nil) != (position == 0) {
			t.Fatalf("unexpected SeekHead extent admission for %d: %v", position, err)
		}
	}
}

func TestMediaEditContainerAdmissionRejectsMP4HiddenTimelineAndMetadata(t *testing.T) {
	metadata := func(key, value string) []byte {
		return containerTestMP4Metadata("mdta", containerTestMP4Box("keys", []byte{0, 0, 0, 0, 0, 0, 0, 1}, containerTestMP4Box("mdta", []byte(key))),
			containerTestMP4Box("\x00\x00\x00\x01", containerTestMP4Text(value)))
	}
	mutate := func(data []byte, kind string, change func([]byte)) []byte {
		offset := bytes.Index(data, []byte(kind))
		if offset < 0 {
			t.Fatalf("test fixture lacks %s", kind)
		}
		change(data[offset+4:])
		return data
	}
	const creation = uint32(2082844800 + 1609459200)
	setCreation := func(data []byte) {
		binary.BigEndian.PutUint32(data[4:8], creation)
		binary.BigEndian.PutUint32(data[8:12], creation)
	}
	cases := map[string][]byte{
		"movie duration":               mutate(containerTestMP4(nil, nil, nil), "mvhd", func(data []byte) { binary.BigEndian.PutUint32(data[16:20], 999) }),
		"track duration":               mutate(containerTestMP4(nil, nil, nil), "tkhd", func(data []byte) { binary.BigEndian.PutUint32(data[20:24], 999) }),
		"media duration":               mutate(containerTestMP4(nil, nil, nil), "mdhd", func(data []byte) { binary.BigEndian.PutUint32(data[16:20], 999) }),
		"contradictory track creation": mutate(containerTestMP4(nil, nil, nil), "tkhd", setCreation),
		"contradictory movie creation": mutate(containerTestMP4(nil, nil, metadata("creation_time", "2022-01-01T00:00:00Z")), "mvhd", setCreation),
		"contradictory brand":          containerTestMP4(nil, nil, metadata("major_brand", "mp42")),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := containerTestAdmit(t, data, "mp4"); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("expected a hidden semantic difference rejection, got %v", err)
			}
		})
	}
	equal := mutate(containerTestMP4(nil, nil, metadata("creation_time", "2021-01-01T00:00:00.000000Z")), "mvhd", setCreation)
	if _, err := containerTestAdmit(t, equal, "mp4"); err != nil {
		t.Fatalf("equivalent MP4 creation-time projections should be admitted: %v", err)
	}
}

func TestMediaEditContainerAdmissionMP4ProfileAndTags(t *testing.T) {
	for name, metadata := range map[string][]byte{
		"no metadata": nil,
		"itunes text": containerTestMP4Metadata("mdir", nil, containerTestMP4Box("\xa9nam", containerTestMP4Text("Example"))),
		"mdta text": containerTestMP4Metadata("mdta", containerTestMP4Box("keys", []byte{0, 0, 0, 0, 0, 0, 0, 1}, containerTestMP4Box("mdta", []byte("title"))),
			containerTestMP4Box("\x00\x00\x00\x01", containerTestMP4Text("Example"))),
	} {
		t.Run(name, func(t *testing.T) {
			writer, err := containerTestAdmit(t, containerTestMP4(nil, nil, metadata), "mp4")
			if err != nil || len(writer) != 0 {
				t.Fatalf("unexpected admission/evidence: %#v, %v", writer, err)
			}
		})
	}
	edit := containerTestMP4Edit(1000, 0, 1)
	if _, err := containerTestAdmit(t, containerTestMP4(edit, nil, nil), "mp4"); err != nil {
		t.Fatalf("trivial MP4 edit was rejected: %v", err)
	}
}

func TestMediaEditContainerAdmissionRejectsUnexposedMP4Semantics(t *testing.T) {
	cases := map[string][]byte{
		"fragmented":                  append(containerTestMP4(nil, nil, nil), containerTestMP4Box("moof")...),
		"private top level":           append(containerTestMP4(nil, nil, nil), containerTestMP4Box("uuid", make([]byte, 16))...),
		"chapter reference":           containerTestMP4(containerTestMP4Box("tref", containerTestMP4Box("chap", []byte{0, 0, 0, 2})), nil, nil),
		"trim edit":                   containerTestMP4(containerTestMP4Edit(1000, 1024, 1), nil, nil),
		"empty edit":                  containerTestMP4(containerTestMP4Edit(1000, ^uint32(0), 1), nil, nil),
		"rate edit":                   containerTestMP4(containerTestMP4Edit(1000, 0, 2), nil, nil),
		"edit duration":               containerTestMP4(containerTestMP4Edit(999, 0, 1), nil, nil),
		"sample encryption":           containerTestMP4(nil, containerTestMP4Box("sinf", containerTestMP4Box("schm", []byte("cenc"))), nil),
		"unreported sample extension": containerTestMP4(nil, containerTestMP4Box("uuid", make([]byte, 16)), nil),
		"unreported metadata":         containerTestMP4(nil, nil, containerTestMP4Box("udta", containerTestMP4Box("Xtra", []byte("hidden")))),
		"hidden chapters":             containerTestMP4(nil, nil, containerTestMP4Box("udta", containerTestMP4Box("chpl", []byte{0, 0, 0, 0}))),
		"itunes binary":               containerTestMP4(nil, nil, containerTestMP4Metadata("mdir", nil, containerTestMP4Box("covr", containerTestMP4Box("data", []byte{0, 0, 0, 13, 0, 0, 0, 0, 1})))),
		"duplicate metadata": containerTestMP4(nil, nil, containerTestMP4Metadata("mdir", nil,
			containerTestMP4Box("\xa9nam", containerTestMP4Text("First")), containerTestMP4Box("\xa9nam", containerTestMP4Text("Second")))),
		"metadata locale": containerTestMP4(nil, nil, containerTestMP4Metadata("mdir", nil,
			containerTestMP4Box("\xa9nam", containerTestMP4Box("data", []byte{0, 0, 0, 1, 0, 0, 0, 1}, []byte("Title"))))),
		"metadata hidden tail": containerTestMP4(nil, nil, containerTestMP4Metadata("mdir", nil,
			containerTestMP4Box("\xa9nam", containerTestMP4Text("Title"), containerTestMP4Box("uuid")))),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := containerTestAdmit(t, data, "mp4"); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("expected an unsupported structure, got %v", err)
			}
		})
	}
}

func TestMediaEditContainerAdmissionBudgetsAndDescriptorOwnership(t *testing.T) {
	t.Run("metadata budget", func(t *testing.T) {
		file, err := os.CreateTemp(t.TempDir(), "metadata-*.mp4")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[:4], mediaEditContainerMaxMetadata+9)
		copy(header[4:], "ftyp")
		if _, err := file.Write(header); err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(mediaEditContainerMaxMetadata + 9); err != nil {
			t.Fatal(err)
		}
		if _, err := mediaEditContainerAdmission(context.Background(), file, mediaEditContainerMaxMetadata+9, "mp4"); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("expected a metadata budget rejection, got %v", err)
		}
	})
	t.Run("header budget", func(t *testing.T) {
		file, err := os.CreateTemp(t.TempDir(), "headers-*.bin")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		s := mediaEditContainerScanner{ctx: context.Background(), file: file, headers: mediaEditContainerMaxHeaders}
		if err := s.header(1); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("expected header budget rejection, got %v", err)
		}
		if err := s.header(mediaEditContainerMaxDepth + 1); !errors.Is(err, ErrSubtitleRemovalBudget) {
			t.Fatalf("expected depth budget rejection, got %v", err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := mediaEditContainerAdmission(ctx, nil, 1, "mkv"); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	})
	t.Run("sparse payload is skipped", func(t *testing.T) {
		data := containerTestMP4(nil, nil, nil)
		file, err := os.CreateTemp(t.TempDir(), "payload-*.mp4")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
		const payloadSize = int64(128 << 20)
		var header [16]byte
		binary.BigEndian.PutUint32(header[:4], 1)
		copy(header[4:8], "mdat")
		binary.BigEndian.PutUint64(header[8:], uint64(payloadSize))
		if _, err := file.Write(header[:]); err != nil {
			t.Fatal(err)
		}
		length := int64(len(data)) + payloadSize
		if err := file.Truncate(length); err != nil {
			t.Fatal(err)
		}
		before, _ := file.Seek(7, io.SeekStart)
		if _, err := mediaEditContainerAdmission(context.Background(), file, length, "mp4"); err != nil {
			t.Fatal(err)
		}
		after, err := file.Seek(0, io.SeekCurrent)
		if err != nil || before != after {
			t.Fatalf("borrowed descriptor offset changed: %d -> %d (%v)", before, after, err)
		}
	})
}

func TestMediaEditContainerAdmissionTruncatedAndExtendedMP4(t *testing.T) {
	data := containerTestMP4(nil, nil, nil)
	oversize := []byte{0, 0, 0, 1, 'm', 'd', 'a', 't', 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	for name, malformed := range map[string][]byte{
		"short header": data[:7], "short body": data[:len(data)-1], "trailing bytes": append(append([]byte{}, data...), 0),
		"overflow extent": append(append([]byte{}, data...), oversize...),
		"undersize":       append(append([]byte{}, data...), []byte{0, 0, 0, 4, 'f', 'r', 'e', 'e'}...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := containerTestAdmit(t, malformed, "mp4"); err == nil {
				t.Fatal("malformed MP4 was admitted")
			}
		})
	}
}

func containerTestAdmit(t *testing.T, data []byte, container string) (map[string]string, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "container-*"+container)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(1, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	writer, admissionErr := mediaEditContainerAdmission(context.Background(), file, int64(len(data)), container)
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 1 {
		t.Fatalf("admission changed the borrowed descriptor position: %d (%v)", position, err)
	}
	return writer, admissionErr
}

func containerTestEBML(id uint64, elements ...[]byte) []byte {
	body := bytes.Join(elements, nil)
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], id)
	first := 0
	for first < 7 && encoded[first] == 0 {
		first++
	}
	result := append([]byte{}, encoded[first:]...)
	width := 1
	for uint64(len(body)) >= uint64(1)<<(7*width)-1 {
		width++
	}
	value := uint64(len(body)) | uint64(1)<<(7*width)
	binary.BigEndian.PutUint64(encoded[:], value)
	result = append(result, encoded[8-width:]...)
	return append(result, body...)
}

func containerTestUint(id, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	first := 0
	for first < 7 && encoded[first] == 0 {
		first++
	}
	return containerTestEBML(id, encoded[first:])
}

func containerTestInfo(extra []byte) []byte {
	return containerTestEBML(0x1549A966, containerTestEBML(0x4D80, []byte("Original muxer")), containerTestEBML(0x5741, []byte("Original writer")), extra)
}

func containerTestTracks(extra []byte) []byte {
	return containerTestEBML(0x1654AE6B,
		containerTestEBML(0xAE, containerTestUint(0xD7, 1), containerTestUint(0x73C5, 11), containerTestUint(0x83, 2), containerTestEBML(0x86, []byte("A_AAC")), extra),
		containerTestEBML(0xAE, containerTestUint(0xD7, 2), containerTestUint(0x73C5, 22), containerTestUint(0x83, 17), containerTestEBML(0x86, []byte("S_TEXT/UTF8"))))
}

func containerTestCluster(extra []byte) []byte {
	return containerTestEBML(0x1F43B675, containerTestUint(0xE7, 0), containerTestEBML(0xA3, []byte{0x81, 0, 0, 0x80, 1}), extra)
}

func containerTestMKV(infoExtra, trackExtra, clusterExtra []byte, extra ...[]byte) []byte {
	header := containerTestEBML(0x1A45DFA3, containerTestEBML(0x4282, []byte("matroska")))
	parts := append([][]byte{containerTestInfo(infoExtra), containerTestTracks(trackExtra), containerTestCluster(clusterExtra)}, extra...)
	return append(header, containerTestEBML(0x18538067, parts...)...)
}

func containerTestTag(name, value string, targetID, targetUID uint64) []byte {
	target := []byte{}
	if targetID != 0 {
		target = containerTestUint(targetID, targetUID)
	}
	return containerTestEBML(0x1254C367, containerTestEBML(0x7373, containerTestEBML(0x63C0, target),
		containerTestEBML(0x67C8, containerTestEBML(0x45A3, []byte(name)), containerTestEBML(0x4487, []byte(value)))))
}

func containerTestMP4Box(kind string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	result := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(result[:4], uint32(len(body)+8))
	copy(result[4:8], kind)
	return append(result, body...)
}

func containerTestMatrix(data []byte) {
	binary.BigEndian.PutUint32(data[0:4], 0x10000)
	binary.BigEndian.PutUint32(data[16:20], 0x10000)
	binary.BigEndian.PutUint32(data[32:36], 0x40000000)
}

func containerTestMP4(trackExtra, sampleExtra, movieExtra []byte) []byte {
	movie := make([]byte, 100)
	binary.BigEndian.PutUint32(movie[12:16], 1000)
	binary.BigEndian.PutUint32(movie[16:20], 1000)
	binary.BigEndian.PutUint32(movie[20:24], 0x10000)
	binary.BigEndian.PutUint16(movie[24:26], 0x100)
	containerTestMatrix(movie[36:72])
	binary.BigEndian.PutUint32(movie[96:100], 2)
	track := make([]byte, 84)
	track[3] = 3
	binary.BigEndian.PutUint32(track[12:16], 1)
	binary.BigEndian.PutUint32(track[20:24], 1000)
	containerTestMatrix(track[40:76])
	binary.BigEndian.PutUint32(track[76:80], 320<<16)
	binary.BigEndian.PutUint32(track[80:84], 180<<16)
	media := make([]byte, 24)
	binary.BigEndian.PutUint32(media[12:16], 1000)
	binary.BigEndian.PutUint32(media[16:20], 1000)
	binary.BigEndian.PutUint16(media[20:22], 0x55c4)
	handler := make([]byte, 24)
	copy(handler[8:12], "vide")
	visual := make([]byte, 78)
	binary.BigEndian.PutUint16(visual[6:8], 1)
	binary.BigEndian.PutUint16(visual[24:26], 320)
	binary.BigEndian.PutUint16(visual[26:28], 180)
	binary.BigEndian.PutUint32(visual[28:32], 0x480000)
	binary.BigEndian.PutUint32(visual[32:36], 0x480000)
	binary.BigEndian.PutUint16(visual[40:42], 1)
	binary.BigEndian.PutUint16(visual[74:76], 24)
	binary.BigEndian.PutUint16(visual[76:78], 0xffff)
	stsd := containerTestMP4Box("stsd", []byte{0, 0, 0, 0, 0, 0, 0, 1},
		containerTestMP4Box("avc1", visual, containerTestMP4Box("avcC", []byte{1, 0x64, 0, 0x1f, 0xff, 0xe0, 0}), sampleExtra))
	stts := containerTestMP4Box("stts", []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 3, 0xe8})
	stsc := containerTestMP4Box("stsc", []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1})
	stsz := containerTestMP4Box("stsz", []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1})
	stco := containerTestMP4Box("stco", []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 32})
	dref := containerTestMP4Box("dref", []byte{0, 0, 0, 0, 0, 0, 0, 1}, containerTestMP4Box("url ", []byte{0, 0, 0, 1}))
	minf := containerTestMP4Box("minf", containerTestMP4Box("vmhd", []byte{0, 0, 0, 1}, make([]byte, 8)),
		containerTestMP4Box("dinf", dref), containerTestMP4Box("stbl", stsd, stts, stsc, stsz, stco))
	mdia := containerTestMP4Box("mdia", containerTestMP4Box("mdhd", media), containerTestMP4Box("hdlr", handler), minf)
	return bytes.Join([][]byte{containerTestMP4Box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2avc1mp41")),
		containerTestMP4Box("moov", containerTestMP4Box("mvhd", movie), containerTestMP4Box("trak", containerTestMP4Box("tkhd", track), mdia, trackExtra), movieExtra),
		containerTestMP4Box("mdat", []byte{1})}, nil)
}

func containerTestMP4Metadata(handler string, keys []byte, values ...[]byte) []byte {
	data := make([]byte, 24)
	copy(data[8:12], handler)
	return containerTestMP4Box("udta", containerTestMP4Box("meta", make([]byte, 4), containerTestMP4Box("hdlr", data), keys, containerTestMP4Box("ilst", values...)))
}

func containerTestMP4Text(value string) []byte {
	return containerTestMP4Box("data", []byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte(value))
}

func containerTestMP4Edit(duration, mediaTime uint32, rate uint16) []byte {
	data := make([]byte, 20)
	binary.BigEndian.PutUint32(data[4:8], 1)
	binary.BigEndian.PutUint32(data[8:12], duration)
	binary.BigEndian.PutUint32(data[12:16], mediaTime)
	binary.BigEndian.PutUint16(data[16:18], rate)
	return containerTestMP4Box("edts", containerTestMP4Box("elst", data))
}

func TestMediaEditMP4ElementaryStreamDescriptorRejectsHiddenExtensions(t *testing.T) {
	descriptor := func(tag byte, value []byte) []byte { return append([]byte{tag, byte(len(value))}, value...) }
	decoder := append([]byte{0x40, 0x15}, make([]byte, 11)...)
	decoder = append(decoder, descriptor(5, []byte{0x12, 0x10})...)
	payload := append([]byte{0, 1, 0}, descriptor(4, decoder)...)
	payload = append(payload, descriptor(6, []byte{2})...)
	valid := append(make([]byte, 4), descriptor(3, payload)...)
	for name, data := range map[string][]byte{"valid": valid, "hidden descriptor": append(append([]byte{}, valid...), descriptor(7, []byte{1})...)} {
		t.Run(name, func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "esds-*")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := file.Write(data); err != nil {
				t.Fatal(err)
			}
			s := mediaEditContainerScanner{ctx: context.Background(), file: file, size: int64(len(data))}
			err = s.mp4ESDS(mediaEditMP4Box{kind: "esds", end: int64(len(data))})
			if name == "valid" && err != nil || name != "valid" && (err == nil || !strings.Contains(err.Error(), "descriptor")) {
				t.Fatalf("unexpected descriptor admission: %v", err)
			}
		})
	}
}
