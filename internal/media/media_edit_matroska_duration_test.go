package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"sort"
	"testing"
)

type mediaEditDurationFixture struct {
	source, candidate           *os.File
	sourceBytes, candidateBytes []byte
	sourceProof, candidateProof mediaEditContainerProof
	sourceDoc, candidateDoc     mediaEditDocument
}

func mediaEditDurationTestMaster(id uint64, elements ...[]byte) []byte {
	body := bytes.Join(elements, nil)
	var checksum [4]byte
	binary.LittleEndian.PutUint32(checksum[:], crc32.ChecksumIEEE(body))
	return containerTestEBML(id, containerTestEBML(0xBF, checksum[:]), body)
}

func mediaEditDurationTestFile(t *testing.T, data []byte, position int64) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "duration-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(position, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return file
}

func mediaEditDurationTestFixture(t *testing.T, candidateDuration string) mediaEditDurationFixture {
	t.Helper()
	private := []byte{1, 2, 3, 4}
	privateHash := sha256.Sum256(private)
	makeContainer := func(candidate bool, duration string) []byte {
		videoUID := uint64(11)
		keepUID, keepNumber := uint64(33), uint64(3)
		if candidate {
			videoUID, keepUID, keepNumber = 101, 103, 2
		}
		track := containerTestEBML(0xAE, containerTestUint(0xD7, 1), containerTestUint(0x73C5, videoUID),
			containerTestUint(0x83, 1), containerTestEBML(0x86, []byte("V_MPEG4/ISO/AVC")),
			containerTestUint(0x23E383, 41666666), containerTestEBML(0x63A2, private),
			containerTestEBML(0xE0, containerTestUint(0xB0, 160), containerTestUint(0xBA, 90)))
		tracks := [][]byte{track}
		if !candidate {
			tracks = append(tracks, containerTestEBML(0xAE, containerTestUint(0xD7, 2), containerTestUint(0x73C5, 22),
				containerTestUint(0x83, 17), containerTestEBML(0x86, []byte("S_TEXT/UTF8")), containerTestEBML(0x22B59C, []byte("eng"))))
		}
		tracks = append(tracks, containerTestEBML(0xAE, containerTestUint(0xD7, keepNumber), containerTestUint(0x73C5, keepUID),
			containerTestUint(0x83, 17), containerTestEBML(0x86, []byte("S_TEXT/UTF8")), containerTestEBML(0x22B59C, []byte("fra"))))
		tags := mediaEditDurationTestMaster(0x1254C367,
			mediaEditDurationTestMaster(0x7373,
				mediaEditDurationTestMaster(0x63C0, containerTestUint(0x63C5, videoUID)),
				mediaEditDurationTestMaster(0x67C8, containerTestEBML(0x45A3, []byte("DURATION")), containerTestEBML(0x4487, append([]byte(duration), 0)))))
		header := containerTestEBML(0x1A45DFA3, containerTestEBML(0x4282, []byte("matroska")))
		segment := mediaEditDurationTestMaster(0x18538067, containerTestInfo(nil), containerTestEBML(0x1654AE6B, tracks...),
			tags, containerTestEBML(0xEC, bytes.Repeat([]byte{0xA5}, 32)), containerTestCluster(nil))
		return append(header, segment...)
	}
	makeDocument := func(candidate bool, duration string) mediaEditDocument {
		streams := []map[string]any{{"index": 0, "codec_type": "video", "codec_name": "h264", "time_base": "1/1000",
			"disposition": map[string]any{"default": 0}, "extradata_size": len(private), "extradata_hash": "SHA256:" + hex.EncodeToString(privateHash[:]),
			"tags": map[string]any{"DURATION": duration}}}
		if !candidate {
			streams = append(streams, map[string]any{"index": 1, "codec_type": "subtitle", "codec_name": "subrip", "time_base": "1/1000",
				"disposition": map[string]any{"default": 1}, "tags": map[string]any{"language": "eng"}})
		}
		streams = append(streams, map[string]any{"index": len(streams), "codec_type": "subtitle", "codec_name": "subrip", "time_base": "1/1000",
			"disposition": map[string]any{"default": 0}, "tags": map[string]any{"language": "fra"}})
		data, err := json.Marshal(mediaEditDocument{Streams: streams, Format: map[string]any{"format_name": "matroska,webm"}})
		if err != nil {
			t.Fatal(err)
		}
		document, err := parseMediaEditDocument(data)
		if err != nil {
			t.Fatal(err)
		}
		return document
	}
	fixture := mediaEditDurationFixture{sourceBytes: makeContainer(false, "00:00:12.000000000"), candidateBytes: makeContainer(true, candidateDuration),
		sourceDoc: makeDocument(false, "00:00:12.000000000"), candidateDoc: makeDocument(true, candidateDuration)}
	fixture.source = mediaEditDurationTestFile(t, fixture.sourceBytes, 13)
	fixture.candidate = mediaEditDurationTestFile(t, fixture.candidateBytes, 7)
	var err error
	fixture.sourceProof, err = mediaEditReadContainerProof(context.Background(), fixture.source, int64(len(fixture.sourceBytes)), "mkv")
	if err != nil {
		t.Fatal(err)
	}
	fixture.candidateProof, err = mediaEditReadContainerProof(context.Background(), fixture.candidate, int64(len(fixture.candidateBytes)), "mkv")
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture mediaEditDurationFixture) restore(ctx context.Context) (*mediaEditDurationRestoration, error) {
	baseline, err := fixture.candidate.Stat()
	if err != nil {
		return nil, err
	}
	return restoreMediaEditMatroskaDurations(ctx, fixture.source, fixture.candidate, fixture.sourceProof, fixture.candidateProof,
		fixture.sourceDoc, fixture.candidateDoc, 1, baseline)
}

func mediaEditDurationTestRead(t *testing.T, file *os.File) []byte {
	t.Helper()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, info.Size())
	if _, err := file.ReadAt(data, 0); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMediaEditMatroskaDurationRestoresOnlyBoundTagAndAncestorCRCs(t *testing.T) {
	fixture := mediaEditDurationTestFixture(t, "00:00:11.999000000")
	result, err := fixture.restore(context.Background())
	if err != nil || result == nil || result.Tags != 1 || len(result.SHA256) != 64 || len(result.PreservedSHA256) != 64 {
		t.Fatalf("duration restoration: %+v, %v", result, err)
	}
	if !bytes.Equal(mediaEditDurationTestRead(t, fixture.source), fixture.sourceBytes) {
		t.Fatal("source bytes changed")
	}
	expected := append([]byte(nil), fixture.candidateBytes...)
	target := fixture.candidateProof.MatroskaDurationTags[0]
	copy(expected[target.Offset:target.Offset+target.Bytes], append([]byte("00:00:12.000000000"), 0))
	scopes := mediaEditDurationCRCScopes(fixture.candidateProof, []mediaEditMatroskaDurationTag{target})
	if len(scopes) != 4 {
		t.Fatalf("fixture lost its four affected CRC ancestors: %d", len(scopes))
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].Depth > scopes[j].Depth })
	for _, scope := range scopes {
		checksum := crc32.ChecksumIEEE(expected[scope.ElementEnd:scope.ParentEnd])
		binary.LittleEndian.PutUint32(expected[scope.ValueOffset:scope.ValueOffset+4], checksum)
	}
	actual := mediaEditDurationTestRead(t, fixture.candidate)
	if !bytes.Equal(actual, expected) {
		t.Fatal("restoration changed a byte outside the exact source tag and covering CRC values")
	}
	digest := sha256.Sum256(actual)
	if result.SHA256 != hex.EncodeToString(digest[:]) || result.Snapshot.Size() != int64(len(actual)) {
		t.Fatal("restoration witness does not bind complete candidate bytes")
	}
	for file, wanted := range map[*os.File]int64{fixture.source: 13, fixture.candidate: 7} {
		if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != wanted {
			t.Fatalf("borrowed descriptor moved: %d, %v", position, err)
		}
	}
}

func TestMediaEditMatroskaDurationRejectsUnprovenRepairBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *mediaEditDurationFixture)
	}{
		{"unrelated metadata change", func(_ *testing.T, f *mediaEditDurationFixture) {
			f.candidateDoc.Streams[0]["tags"].(map[string]any)["title"] = "Changed"
		}},
		{"raw source tag differs from probe", func(_ *testing.T, f *mediaEditDurationFixture) {
			f.sourceProof.MatroskaDurationTags[0].Value = "00:00:12.001000000"
		}},
		{"tag targets another retained track", func(_ *testing.T, f *mediaEditDurationFixture) {
			f.candidateProof.MatroskaDurationTags[0].TrackUID = 103
		}},
		{"codec private is unbound", func(_ *testing.T, f *mediaEditDurationFixture) {
			f.candidateProof.MatroskaTracks[0].CodecPrivateBytes++
		}},
		{"raw default duration changed", func(_ *testing.T, f *mediaEditDurationFixture) {
			f.candidateProof.MatroskaTracks[0].DefaultDurationNS++
		}},
		{"integer millisecond default duration", func(_ *testing.T, f *mediaEditDurationFixture) {
			f.sourceProof.MatroskaTracks[0].DefaultDurationNS = 40_000_000
			f.candidateProof.MatroskaTracks[0].DefaultDurationNS = 40_000_000
		}},
		{"candidate slot extent changed", func(_ *testing.T, f *mediaEditDurationFixture) { f.candidateProof.MatroskaDurationTags[0].Bytes-- }},
		{"source CRC is corrupt", func(t *testing.T, f *mediaEditDurationFixture) {
			mediaEditDurationTestCorruptCRC(t, f.source, f.sourceProof)
		}},
		{"candidate CRC is corrupt", func(t *testing.T, f *mediaEditDurationFixture) {
			mediaEditDurationTestCorruptCRC(t, f.candidate, f.candidateProof)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := mediaEditDurationTestFixture(t, "00:00:11.999000000")
			test.mutate(t, &fixture)
			before := mediaEditDurationTestRead(t, fixture.candidate)
			if result, err := fixture.restore(context.Background()); result != nil || !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven repair succeeded: %+v, %v", result, err)
			}
			if !bytes.Equal(before, mediaEditDurationTestRead(t, fixture.candidate)) {
				t.Fatal("failed admission modified candidate bytes")
			}
		})
	}
}

func TestMediaEditMatroskaDurationRejectsOtherRoundingAmounts(t *testing.T) {
	for _, candidateDuration := range []string{"00:00:11.998000000", "00:00:12.001000000", "00:00:11.999999999"} {
		t.Run(candidateDuration, func(t *testing.T) {
			fixture := mediaEditDurationTestFixture(t, candidateDuration)
			if result, err := fixture.restore(context.Background()); result != nil || !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("a different rounding amount was restored: %+v, %v", result, err)
			}
			if !bytes.Equal(fixture.candidateBytes, mediaEditDurationTestRead(t, fixture.candidate)) {
				t.Fatal("unproven rounding changed candidate bytes")
			}
		})
	}
}

func mediaEditDurationTestCorruptCRC(t *testing.T, file *os.File, proof mediaEditContainerProof) {
	t.Helper()
	data := mediaEditDurationTestRead(t, file)
	offset := proof.matroskaCRCs[0].ValueOffset
	if _, err := file.WriteAt([]byte{data[offset] ^ 1}, offset); err != nil {
		t.Fatal(err)
	}
}

func TestMediaEditMatroskaDurationNoDifferenceKeepsExistingProfile(t *testing.T) {
	fixture := mediaEditDurationTestFixture(t, "00:00:12.000000000")
	// A no-op must not newly require the narrow raw CodecPrivate binding used
	// by this repair. Existing non-repair container/packet proofs remain owners.
	fixture.sourceProof.MatroskaTracks[0].CodecID = "V_NOT_IN_REPAIR_PROFILE"
	result, err := fixture.restore(context.Background())
	if result != nil || err != nil || !bytes.Equal(fixture.candidateBytes, mediaEditDurationTestRead(t, fixture.candidate)) {
		t.Fatalf("no-op restoration changed the existing admission path: %+v, %v", result, err)
	}
}

func TestMediaEditMatroskaDurationCancellationDoesNotWrite(t *testing.T) {
	fixture := mediaEditDurationTestFixture(t, "00:00:11.999000000")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := fixture.restore(ctx); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled restoration: %+v, %v", result, err)
	}
	if !bytes.Equal(fixture.candidateBytes, mediaEditDurationTestRead(t, fixture.candidate)) {
		t.Fatal("cancelled restoration wrote candidate bytes")
	}
}

func TestMediaEditMatroskaDurationWitnessRejectsChangesBeforeCallerBaseline(t *testing.T) {
	for _, mutation := range []string{"unchanged padding", "appended Void"} {
		t.Run(mutation, func(t *testing.T) {
			fixture := mediaEditDurationTestFixture(t, "00:00:11.999000000")
			result, err := fixture.restore(context.Background())
			if err != nil || result == nil {
				t.Fatalf("initial restoration: %+v, %v", result, err)
			}
			data := mediaEditDurationTestRead(t, fixture.candidate)
			if mutation == "unchanged padding" {
				offset := bytes.Index(data, bytes.Repeat([]byte{0xA5}, 32))
				if offset < 0 {
					t.Fatal("fixture has no padding extent")
				}
				_, err = fixture.candidate.WriteAt([]byte{0xA4}, int64(offset))
			} else {
				_, err = fixture.candidate.WriteAt(containerTestEBML(0xEC, []byte{0}), int64(len(data)))
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := mediaEditCheckUnchanged(fixture.candidate, result.Snapshot); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("post-repair mutation became a caller baseline: %v", err)
			}
			if mutation == "unchanged padding" {
				digest, err := mediaEditFileDigest(context.Background(), fixture.candidate, result.Snapshot.Size())
				if err != nil || digest == result.SHA256 {
					t.Fatalf("same-size mutation retained repaired full-file evidence: %q, %v", digest, err)
				}
			}
		})
	}
}

func TestMediaEditMatroskaDurationFinalProofRejectsCRCChangedBeforeSnapshot(t *testing.T) {
	fixture := mediaEditDurationTestFixture(t, "00:00:11.999000000")
	result, err := fixture.restore(context.Background())
	if err != nil || result == nil {
		t.Fatalf("initial restoration: %+v, %v", result, err)
	}
	target := fixture.candidateProof.MatroskaDurationTags[0]
	scopes := mediaEditDurationCRCScopes(fixture.candidateProof, []mediaEditMatroskaDurationTag{target})
	ranges := []mediaEditDurationRange{{Start: target.Offset, End: target.Offset + target.Bytes}}
	for _, scope := range scopes {
		ranges = append(ranges, mediaEditDurationRange{Start: scope.ValueOffset, End: scope.ValueOffset + 4})
	}
	patches := []mediaEditDurationPatch{{Offset: target.Offset, After: append([]byte("00:00:12.000000000"), 0)}}
	// This models the excluded CRC bytes changing after rewrite verification
	// but before the final proof captures its new candidate snapshot.
	mediaEditDurationTestCorruptCRC(t, fixture.candidate, fixture.candidateProof)
	after, err := fixture.candidate.Stat()
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := mediaEditDurationUnchangedDigest(context.Background(), fixture.candidate, after.Size(), ranges)
	if err != nil || unchanged != result.PreservedSHA256 {
		t.Fatalf("mutation was not confined to an excluded CRC value: %q, %v", unchanged, err)
	}
	if result, err := proveMediaEditMatroskaDurationRestoration(context.Background(), fixture.candidate, after, patches, scopes, ranges, unchanged); result != nil || !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("a stale CRC was adopted by the new full-file proof baseline: %+v, %v", result, err)
	}
}
