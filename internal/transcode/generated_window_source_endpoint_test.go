package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func generatedEndpointTestBox(kind string, body []byte) []byte {
	box := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
	copy(box[4:8], kind)
	copy(box[8:], body)
	return box
}

func generatedEndpointTestJoin(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

func generatedEndpointTestU32(values ...uint32) []byte {
	body := make([]byte, 4*len(values))
	for index, value := range values {
		binary.BigEndian.PutUint32(body[index*4:index*4+4], value)
	}
	return body
}

// generatedEndpointTestFixture is a structural MP4 fixture, not decodable
// media. Its two chunks contain 48 declared samples at 24 samples per second.
// originTicks uses the movie clock, whose scale is ticksPerSecond.
func generatedEndpointTestFixture(originTicks int64) []byte {
	ftyp := generatedEndpointTestBox("ftyp", generatedEndpointTestJoin([]byte("isom"), generatedEndpointTestU32(512), []byte("isomiso2avc1mp41")))
	payload := bytes.Repeat([]byte{0x65}, 192)
	mdat := generatedEndpointTestBox("mdat", payload)
	firstChunk := uint32(len(ftyp) + 8)
	movieDuration := uint32(2*ticksPerSecond + originTicks)
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:16], uint32(ticksPerSecond))
	binary.BigEndian.PutUint32(mvhd[16:20], movieDuration)
	binary.BigEndian.PutUint32(mvhd[20:24], 0x00010000)
	binary.BigEndian.PutUint16(mvhd[24:26], 0x0100)
	binary.BigEndian.PutUint32(mvhd[36:40], 0x00010000)
	binary.BigEndian.PutUint32(mvhd[52:56], 0x00010000)
	binary.BigEndian.PutUint32(mvhd[68:72], 0x40000000)
	binary.BigEndian.PutUint32(mvhd[96:100], 2)
	tkhd := make([]byte, 84)
	tkhd[3] = 3
	binary.BigEndian.PutUint32(tkhd[12:16], 1)
	binary.BigEndian.PutUint32(tkhd[20:24], movieDuration)
	binary.BigEndian.PutUint32(tkhd[40:44], 0x00010000)
	binary.BigEndian.PutUint32(tkhd[56:60], 0x00010000)
	binary.BigEndian.PutUint32(tkhd[72:76], 0x40000000)
	binary.BigEndian.PutUint32(tkhd[76:80], 160<<16)
	binary.BigEndian.PutUint32(tkhd[80:84], 90<<16)
	mdhd := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhd[12:16], 24_000)
	binary.BigEndian.PutUint32(mdhd[16:20], 48_000)
	handler := func(kind string) []byte {
		body := make([]byte, 25)
		copy(body[8:12], kind)
		return generatedEndpointTestBox("hdlr", body)
	}
	avc1 := make([]byte, 78)
	binary.BigEndian.PutUint16(avc1[6:8], 1)
	binary.BigEndian.PutUint16(avc1[24:26], 160)
	binary.BigEndian.PutUint16(avc1[26:28], 90)
	binary.BigEndian.PutUint32(avc1[28:32], 0x00480000)
	binary.BigEndian.PutUint32(avc1[32:36], 0x00480000)
	binary.BigEndian.PutUint16(avc1[40:42], 1)
	binary.BigEndian.PutUint16(avc1[74:76], 24)
	binary.BigEndian.PutUint16(avc1[76:78], 0xffff)
	avcc := []byte{1, 244, 0, 10, 0xff, 0xe1, 0, 4, 0x67, 244, 0, 10, 1, 0, 2, 0x68, 0, 0xfd, 0xf8, 0xf8, 0}
	avc1 = append(avc1, generatedEndpointTestBox("avcC", avcc)...)
	avc1 = append(avc1, generatedEndpointTestBox("pasp", generatedEndpointTestU32(1, 1))...)
	stsd := generatedEndpointTestBox("stsd", generatedEndpointTestJoin(generatedEndpointTestU32(0, 1), generatedEndpointTestBox("avc1", avc1)))
	stts := generatedEndpointTestBox("stts", generatedEndpointTestU32(0, 2, 24, 1000, 24, 1000))
	sizes := generatedEndpointTestU32(0, 0, 48)
	for index := 0; index < 48; index++ {
		sizes = append(sizes, generatedEndpointTestU32(uint32(3+2*(index%2)))...)
	}
	stsz := generatedEndpointTestBox("stsz", sizes)
	stsc := generatedEndpointTestBox("stsc", generatedEndpointTestU32(0, 2, 1, 24, 1, 2, 24, 1))
	stco := generatedEndpointTestBox("stco", generatedEndpointTestU32(0, 2, firstChunk, firstChunk+96))
	ctts := generatedEndpointTestBox("ctts", generatedEndpointTestU32(0, 1, 48, 0))
	stss := generatedEndpointTestBox("stss", generatedEndpointTestU32(0, 2, 1, 25))
	stbl := generatedEndpointTestBox("stbl", generatedEndpointTestJoin(stsd, stts, stsz, stsc, stco, ctts, stss))
	dref := generatedEndpointTestBox("dref", generatedEndpointTestJoin(generatedEndpointTestU32(0, 1), generatedEndpointTestBox("url ", generatedEndpointTestU32(1))))
	dinf := generatedEndpointTestBox("dinf", dref)
	vmhd := generatedEndpointTestBox("vmhd", generatedEndpointTestJoin(generatedEndpointTestU32(1), make([]byte, 8)))
	minf := generatedEndpointTestBox("minf", generatedEndpointTestJoin(vmhd, dinf, stbl))
	mdia := generatedEndpointTestBox("mdia", generatedEndpointTestJoin(generatedEndpointTestBox("mdhd", mdhd), handler("vide"), minf))
	editBody := generatedEndpointTestU32(0, 1, uint32(2*ticksPerSecond), 0, 0x00010000)
	if originTicks > 0 {
		editBody = generatedEndpointTestU32(0, 2, uint32(originTicks), 0xffffffff, 0x00010000, uint32(2*ticksPerSecond), 0, 0x00010000)
	}
	edts := generatedEndpointTestBox("edts", generatedEndpointTestBox("elst", editBody))
	trak := generatedEndpointTestBox("trak", generatedEndpointTestJoin(generatedEndpointTestBox("tkhd", tkhd), edts, mdia))
	data := generatedEndpointTestBox("data", generatedEndpointTestJoin(generatedEndpointTestU32(1, 0), []byte("Lavf endpoint fixture")))
	ilst := generatedEndpointTestBox("ilst", generatedEndpointTestBox("\xa9too", data))
	meta := generatedEndpointTestBox("meta", generatedEndpointTestJoin(generatedEndpointTestU32(0), handler("mdir"), ilst))
	udta := generatedEndpointTestBox("udta", meta)
	moov := generatedEndpointTestBox("moov", generatedEndpointTestJoin(generatedEndpointTestBox("mvhd", mvhd), trak, udta))
	return generatedEndpointTestJoin(ftyp, mdat, moov)
}

func generatedEndpointTestPut(source []byte, kind string, bodyOffset int, value uint32) {
	pos := bytes.Index(source, []byte(kind)) + 4 + bodyOffset
	binary.BigEndian.PutUint32(source[pos:pos+4], value)
}

// Replacement is limited to the unique boxes in this fixture. The media-data
// body precedes the movie, so metadata size changes preserve its chunk offsets.
func generatedEndpointTestReplace(source []byte, kind string, replacement []byte) []byte {
	start := generatedEndpointTestBoxStart(source, kind)
	end := start + int(binary.BigEndian.Uint32(source[start:start+4]))
	delta := len(replacement) - (end - start)
	copySource := bytes.Clone(source)
	for _, ancestor := range []string{"moov", "trak", "mdia", "minf", "stbl", "stsd", "avc1", "dinf", "dref", "edts", "udta", "meta", "ilst", "\xa9too"} {
		pos := generatedEndpointTestBoxStart(source, ancestor)
		if pos < 0 || pos >= start {
			continue
		}
		size := int(binary.BigEndian.Uint32(source[pos : pos+4]))
		if pos+size >= end {
			binary.BigEndian.PutUint32(copySource[pos:pos+4], uint32(size+delta))
		}
	}
	return generatedEndpointTestJoin(copySource[:start], replacement, copySource[end:])
}

func generatedEndpointTestBoxStart(source []byte, kind string) int {
	for offset := 4; offset < len(source); {
		found := bytes.Index(source[offset:], []byte(kind))
		if found < 0 {
			return -1
		}
		pos := offset + found - 4
		size := uint64(binary.BigEndian.Uint32(source[pos : pos+4]))
		if size >= 8 && size <= uint64(len(source)-pos) {
			return pos
		}
		offset += found + 1
	}
	return -1
}

func TestGeneratedMP4SourceEndpointAdmittedVariants(t *testing.T) {
	source := generatedEndpointTestFixture(0)
	firstChunk := uint64(bytes.Index(source, []byte("mdat")) + 4)
	co64 := generatedEndpointTestJoin(generatedEndpointTestU32(0, 2), make([]byte, 16))
	binary.BigEndian.PutUint64(co64[8:16], firstChunk)
	binary.BigEndian.PutUint64(co64[16:24], firstChunk+96)
	source = generatedEndpointTestReplace(source, "stco", generatedEndpointTestBox("co64", co64))
	source = generatedEndpointTestReplace(source, "stsz", generatedEndpointTestBox("stsz", generatedEndpointTestU32(0, 4, 48)))
	source = generatedEndpointTestReplace(source, "ctts", generatedEndpointTestBox("ctts", generatedEndpointTestU32(0x01000000, 1, 48, 0)))
	for _, kind := range []string{"edts", "stss", "udta"} {
		source = generatedEndpointTestReplace(source, kind, nil)
	}
	for _, kind := range []string{"mvhd", "mdhd", "tkhd"} {
		start := bytes.Index(source, []byte(kind)) - 4
		old := source[start+8 : start+int(binary.BigEndian.Uint32(source[start:start+4]))]
		body := make([]byte, len(old)+12)
		body[0] = 1
		copy(body[1:4], old[1:4])
		copy(body[8:12], old[4:8])
		copy(body[16:20], old[8:12])
		if kind == "tkhd" {
			copy(body[20:28], old[12:20])
			binary.BigEndian.PutUint64(body[28:36], uint64(binary.BigEndian.Uint32(old[20:24])))
			copy(body[36:], old[24:])
		} else {
			copy(body[20:24], old[12:16])
			binary.BigEndian.PutUint64(body[24:32], uint64(binary.BigEndian.Uint32(old[16:20])))
			copy(body[32:], old[20:])
		}
		source = generatedEndpointTestReplace(source, kind, generatedEndpointTestBox(kind, body))
	}
	got, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0)
	if err != nil || got.SampleCount != 48 || got.End != (GeneratedRational{Num: 2, Den: 1}) || got.DurationTicks != 2*ticksPerSecond {
		t.Fatalf("admitted constant-size/co64/version-one profile failed: %+v, %v", got, err)
	}
	// A version-one edit list preserves the same leading-empty epoch.
	source = generatedEndpointTestFixture(2 * ticksPerSecond)
	edit := generatedEndpointTestJoin(generatedEndpointTestU32(0x01000000, 2), make([]byte, 40))
	binary.BigEndian.PutUint64(edit[8:16], uint64(2*ticksPerSecond))
	binary.BigEndian.PutUint64(edit[16:24], ^uint64(0))
	binary.BigEndian.PutUint32(edit[24:28], 0x00010000)
	binary.BigEndian.PutUint64(edit[28:36], uint64(2*ticksPerSecond))
	binary.BigEndian.PutUint32(edit[44:48], 0x00010000)
	source = generatedEndpointTestReplace(source, "elst", generatedEndpointTestBox("elst", edit))
	got, err = parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0)
	if err != nil || got.Origin != (GeneratedRational{Num: 2, Den: 1}) || got.End != (GeneratedRational{Num: 4, Den: 1}) {
		t.Fatalf("admitted version-one edit profile failed: %+v, %v", got, err)
	}
}

func TestGeneratedMP4SourceEndpointCertificate(t *testing.T) {
	for _, origin := range []int64{0, 2 * ticksPerSecond} {
		source := generatedEndpointTestFixture(origin)
		got, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0)
		if err != nil {
			t.Fatal(err)
		}
		wantOrigin := origin / ticksPerSecond
		if got.SourceIdentity != "" || got.StreamIndex != 0 || got.TrackID != 1 || got.SampleCount != 48 ||
			got.FrameDuration != (GeneratedRational{Num: 1, Den: 24}) || got.Origin != (GeneratedRational{Num: wantOrigin, Den: 1}) ||
			got.Last != (GeneratedRational{Num: wantOrigin*24 + 47, Den: 24}) || got.End != (GeneratedRational{Num: wantOrigin + 2, Den: 1}) ||
			!got.DurationTicksExact || got.DurationTicks != 2*ticksPerSecond {
			t.Fatalf("unexpected certificate: %+v", got)
		}
		moovStart := bytes.Index(source, []byte("moov")) - 4
		if got.MetadataSHA256 != sha256.Sum256(source[moovStart:]) {
			t.Fatal("movie hash does not cover the complete movie header and body")
		}
		digest := sha256.New()
		_, _ = digest.Write([]byte("goby-generated-mp4-sample-extents-v1\x00"))
		offset := uint64(bytes.Index(source, []byte("mdat")) + 4)
		var tuple [12]byte
		for sample := 0; sample < 48; sample++ {
			size := uint32(3 + 2*(sample%2))
			binary.BigEndian.PutUint64(tuple[:8], offset)
			binary.BigEndian.PutUint32(tuple[8:], size)
			_, _ = digest.Write(tuple[:])
			offset += uint64(size)
		}
		var wantExtents [32]byte
		copy(wantExtents[:], digest.Sum(nil))
		if got.SampleExtentsSHA256 != wantExtents {
			t.Fatal("extent hash does not follow logical sample order")
		}
	}
}

func TestGeneratedMP4SourceEndpointInexactTicks(t *testing.T) {
	source := generatedEndpointTestFixture(0)
	generatedEndpointTestPut(source, "mdhd", 12, 24_001)
	// The movie-clock ceiling is a consistency check, not the exact endpoint.
	generatedEndpointTestPut(source, "mvhd", 16, 19_999_167)
	generatedEndpointTestPut(source, "tkhd", 20, 19_999_167)
	generatedEndpointTestPut(source, "elst", 8, 19_999_167)
	got, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.DurationTicksExact || got.DurationTicks != 0 || got.End != (GeneratedRational{Num: 48_000, Den: 24_001}) {
		t.Fatalf("metadata ceiling replaced the exact sample endpoint: %+v", got)
	}
}

func TestGeneratedMP4SourceEndpointRejectsUnsupportedStructure(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"zero-sized top level", func(s []byte) []byte { binary.BigEndian.PutUint32(s[:4], 0); return s }},
		{"fragment", func(s []byte) []byte { return append(s, generatedEndpointTestBox("moof", nil)...) }},
		{"multiple movie", func(s []byte) []byte { return append(s, generatedEndpointTestBox("moov", nil)...) }},
		{"media duration truncation", func(s []byte) []byte { generatedEndpointTestPut(s, "mdhd", 16, 47_000); return s }},
		{"movie duration extension", func(s []byte) []byte { generatedEndpointTestPut(s, "mvhd", 16, 20_000_001); return s }},
		{"nonuniform period", func(s []byte) []byte { generatedEndpointTestPut(s, "stts", 20, 1001); return s }},
		{"invalid sample count", func(s []byte) []byte { generatedEndpointTestPut(s, "stsz", 8, 47); return s }},
		{"zero sample size", func(s []byte) []byte { generatedEndpointTestPut(s, "stsz", 12, 0); return s }},
		{"composition offset", func(s []byte) []byte { generatedEndpointTestPut(s, "ctts", 12, 1); return s }},
		{"composition hole", func(s []byte) []byte { generatedEndpointTestPut(s, "ctts", 8, 47); return s }},
		{"alternate description", func(s []byte) []byte { generatedEndpointTestPut(s, "stsc", 16, 2); return s }},
		{"mapping misses samples", func(s []byte) []byte { generatedEndpointTestPut(s, "stsc", 24, 23); return s }},
		{"mapping starts late", func(s []byte) []byte { generatedEndpointTestPut(s, "stsc", 8, 2); return s }},
		{"unordered sync sample", func(s []byte) []byte { generatedEndpointTestPut(s, "stss", 12, 1); return s }},
		{"chunk outside media", func(s []byte) []byte { generatedEndpointTestPut(s, "stco", 8, 1); return s }},
		{"overlapping chunks", func(s []byte) []byte {
			first := uint32(bytes.Index(s, []byte("mdat")) + 4)
			generatedEndpointTestPut(s, "stco", 12, first+1)
			return s
		}},
		{"chunk crosses media end", func(s []byte) []byte {
			first := uint32(bytes.Index(s, []byte("mdat")) + 4)
			generatedEndpointTestPut(s, "stco", 12, first+97)
			return s
		}},
		{"edit trims samples", func(s []byte) []byte { generatedEndpointTestPut(s, "elst", 8, 19_999_999); return s }},
		{"edit changes rate", func(s []byte) []byte { generatedEndpointTestPut(s, "elst", 16, 0x00010001); return s }},
		{"external data reference", func(s []byte) []byte { generatedEndpointTestPut(s, "url ", 0, 0); return s }},
		{"unknown duration", func(s []byte) []byte { generatedEndpointTestPut(s, "mdhd", 16, 0xffffffff); return s }},
		{"excessive scale", func(s []byte) []byte { generatedEndpointTestPut(s, "mdhd", 12, 0x80000000); return s }},
		{"excessive period", func(s []byte) []byte { generatedEndpointTestPut(s, "stts", 12, 0x80000000); return s }},
		{"hidden timing metadata", func(s []byte) []byte { copy(s[bytes.Index(s, []byte("\xa9too")):], "stts"); return s }},
		{"cover-art stream", func(s []byte) []byte { copy(s[bytes.Index(s, []byte("\xa9too")):], "covr"); return s }},
		{"hidden metadata track", func(s []byte) []byte { copy(s[bytes.Index(s, []byte("ilst")):], "trak"); return s }},
		{"metadata padding precedes handler", func(s []byte) []byte {
			start := generatedEndpointTestBoxStart(s, "meta")
			body := s[start+8 : start+int(binary.BigEndian.Uint32(s[start:start+4]))]
			padding := generatedEndpointTestBox("free", generatedEndpointTestJoin(generatedEndpointTestBox("hdlr", make([]byte, 25)), generatedEndpointTestBox("elst", nil)))
			replacement := generatedEndpointTestBox("meta", generatedEndpointTestJoin(body[:4], padding, body[4:]))
			return generatedEndpointTestReplace(s, "meta", replacement)
		}},
		{"metadata item list precedes handler", func(s []byte) []byte {
			start := generatedEndpointTestBoxStart(s, "meta")
			body := s[start+8 : start+int(binary.BigEndian.Uint32(s[start:start+4]))]
			handlerEnd := 4 + int(binary.BigEndian.Uint32(body[4:8]))
			replacement := generatedEndpointTestBox("meta", generatedEndpointTestJoin(body[:4], body[handlerEnd:], body[4:handlerEnd]))
			return generatedEndpointTestReplace(s, "meta", replacement)
		}},
		{"ambiguous offsets", func(s []byte) []byte { copy(s[bytes.Index(s, []byte("stss")):], "co64"); return s }},
		{"truncated AVC extension", func(s []byte) []byte {
			pos := bytes.Index(s, []byte("avcC")) + 4 + 20
			s[pos] = 1
			return s
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.mutate(generatedEndpointTestFixture(0))
			if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0); !errors.Is(err, ErrTimelineProbe) {
				t.Fatalf("unsupported structure was not rejected: %v", err)
			}
		})
	}
	source := generatedEndpointTestFixture(1)
	if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0); !errors.Is(err, ErrTimelineProbe) {
		t.Fatalf("inexact leading-empty media clock was not rejected: %v", err)
	}
}

type generatedEndpointTestMetadataReader struct {
	source               []byte
	mediaStart, mediaEnd int64
	payloadRead          bool
}

type generatedEndpointTestCountingContext struct {
	context.Context
	calls int
}

func (c *generatedEndpointTestCountingContext) Err() error {
	c.calls++
	if c.calls >= 60 {
		return context.Canceled
	}
	return nil
}

func (r *generatedEndpointTestMetadataReader) ReadAt(data []byte, offset int64) (int, error) {
	if offset < r.mediaEnd && offset+int64(len(data)) > r.mediaStart {
		r.payloadRead = true
		return 0, errors.New("media payload must not be read")
	}
	return bytes.NewReader(r.source).ReadAt(data, offset)
}

func TestGeneratedMP4SourceEndpointSkipsPayload(t *testing.T) {
	source := generatedEndpointTestFixture(0)
	start := int64(bytes.Index(source, []byte("mdat")) + 4)
	reader := &generatedEndpointTestMetadataReader{source: source, mediaStart: start, mediaEnd: start + 192}
	before, err := parseGeneratedMP4SourceEndpoint(context.Background(), reader, int64(len(source)), 0)
	if err != nil || reader.payloadRead {
		t.Fatalf("parser read media payload: %v", err)
	}
	source[start] ^= 0xff
	after, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0)
	if err != nil || before != after {
		t.Fatalf("payload bytes changed a structural certificate: %v", err)
	}
	generatedEndpointTestPut(source, "stsz", 12, 4)
	generatedEndpointTestPut(source, "stsz", 16, 4)
	changed, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0)
	if err != nil || before.SampleExtentsSHA256 == changed.SampleExtentsSHA256 {
		t.Fatalf("changed logical sample extents did not change their hash: %v", err)
	}
}

func TestGeneratedMP4SourceEndpointBudgetsAndCancellation(t *testing.T) {
	source := generatedEndpointTestFixture(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseGeneratedMP4SourceEndpoint(ctx, bytes.NewReader(source), int64(len(source)), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context was not preserved: %v", err)
	}
	counting := &generatedEndpointTestCountingContext{Context: context.Background()}
	if _, err := parseGeneratedMP4SourceEndpoint(counting, bytes.NewReader(source), int64(len(source)), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation during table traversal was not preserved: %v", err)
	}
	if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 1); !errors.Is(err, ErrTimelineProbe) {
		t.Fatalf("unsupported stream selection was not rejected: %v", err)
	}
	generatedEndpointTestPut(source, "stts", 4, generatedMP4MaxSamples+1)
	if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("excessive sample table count was not bounded: %v", err)
	}
	source = generatedEndpointTestFixture(0)
	for index := 0; index < generatedMP4MaxTopBoxes; index++ {
		source = append(source, generatedEndpointTestBox("free", nil)...)
	}
	if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("excessive top-level count was not bounded: %v", err)
	}
	source = generatedEndpointTestFixture(0)
	udtaStart := generatedEndpointTestBoxStart(source, "udta")
	udtaSize := int(binary.BigEndian.Uint32(source[udtaStart : udtaStart+4]))
	metadata := bytes.Clone(source[udtaStart : udtaStart+udtaSize])
	for index := 0; index < generatedMP4MaxBoxes; index++ {
		metadata = append(metadata, generatedEndpointTestBox("free", nil)...)
	}
	source = generatedEndpointTestReplace(source, "udta", metadata)
	if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("excessive metadata box count was not bounded: %v", err)
	}
	source = generatedEndpointTestReplace(generatedEndpointTestFixture(0), "moov", generatedEndpointTestBox("moov", make([]byte, generatedMP4MaxMoov)))
	if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0); !errors.Is(err, ErrTimelineLimit) {
		t.Fatalf("excessive movie metadata bytes were not bounded: %v", err)
	}
	// An explicit 64-bit size is admitted without a consume-to-EOF shortcut.
	source = generatedEndpointTestFixture(0)
	extended := generatedEndpointTestJoin(generatedEndpointTestU32(1), []byte("free"), make([]byte, 8))
	binary.BigEndian.PutUint64(extended[8:16], 16)
	source = append(source, extended...)
	if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), bytes.NewReader(source), int64(len(source)), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := parseGeneratedMP4SourceEndpoint(context.Background(), io.NewSectionReader(bytes.NewReader(source), 0, int64(len(source)-1)), int64(len(source)), 0); err == nil {
		t.Fatal("truncated declared descriptor was admitted")
	}
}
