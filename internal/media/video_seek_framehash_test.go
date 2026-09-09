package media

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func videoSeekHashTestBase() VideoSeekIndex {
	base := videoSeekCandidateTestIndex()
	base.TimeBaseDenominator = 1000
	base.ParameterSetsSHA256 = ""
	base.PacketSideDataChecked = false
	base.NALScopeChecked = false
	base.DecodedFrameBytes = 0
	return base
}

func videoSeekHashTestHeaders(codedBase, decodedBase, parameterBase string) string {
	return videoSeekHashTestHeadersForStreams(codedBase, decodedBase, parameterBase, 5)
}

func videoSeekHashTestHeadersForStreams(codedBase, decodedBase, parameterBase string, streamCount int) string {
	var output strings.Builder
	output.WriteString("#format: frame checksums\n#version: 2\n#hash: SHA256\n#software: Lavf62.1.100\n")
	for number, timeBase := range []string{codedBase, decodedBase, parameterBase, codedBase, codedBase}[:streamCount] {
		codec := "h264"
		if number == 1 {
			codec = "rawvideo"
		}
		fmt.Fprintf(&output, "#tb %d: %s\n#media_type %d: video\n#codec_id %d: %s\n#dimensions %d: 64x64\n#sar %d: 1/1\n",
			number, timeBase, number, number, codec, number, number)
	}
	output.WriteString("#stream#, dts, pts, duration, size, hash\n")
	return output.String()
}

func videoSeekHashTestRecord(stream int, dts, pts, size int64, digest string) string {
	return fmt.Sprintf("%d, %d, %d, 1, %d, %s\n", stream, dts, pts, size, digest)
}

func videoSeekHashTestPoint(pts int64) string {
	return videoSeekHashTestRecord(0, pts-100, pts, 120, strings.Repeat("c", 64)) +
		videoSeekHashTestRecord(2, pts-100, pts, 40, strings.Repeat("e", 64)) +
		videoSeekHashTestRecord(3, pts-100, pts, 120, strings.Repeat("f", 64)) +
		videoSeekHashTestRecord(1, pts, pts, 6144, strings.Repeat("d", 64))
}

func videoSeekHashTestProofPoint(pts int64) string {
	return videoSeekHashTestRecord(0, pts-100, pts, 120, strings.Repeat("c", 64)) +
		videoSeekHashTestRecord(2, pts-100, pts, 40, strings.Repeat("e", 64)) +
		videoSeekHashTestRecord(1, pts, pts, 6144, strings.Repeat("d", 64))
}

func videoSeekHashTestParse(t *testing.T, data string, base VideoSeekIndex, maxEntries int) VideoSeekIndex {
	t.Helper()
	index, err := ParseVideoSeekFrameHash(strings.NewReader(data), base, maxEntries)
	if err != nil {
		t.Fatalf("parse framehash evidence: %v", err)
	}
	return index
}

func TestParseVideoSeekFrameHashAcceptsFiveAnalysisBranchesAndEarlyParameterSets(t *testing.T) {
	base := videoSeekHashTestBase()
	base.Entries = videoSeekCandidateTestIndex(999).Entries
	data := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000") +
		videoSeekHashTestPoint(0) + videoSeekHashTestPoint(1000) + videoSeekHashTestPoint(2000)
	index := videoSeekHashTestParse(t, data, base, MaxVideoSeekEntries)
	if len(index.Entries) != 3 || index.Entries[0].PTS != 0 || index.Entries[0].DTS != -100 ||
		index.Entries[2].PTS != 2000 || index.DecodedFrameBytes != 6144 ||
		index.ParameterSetsSHA256 != strings.Repeat("e", 64) || index.Width != 64 || index.Height != 64 ||
		index.SourceIdentity != base.SourceIdentity || index.ToolIdentity != base.ToolIdentity || !index.PacketSideDataChecked || !index.NALScopeChecked {
		t.Fatalf("five-branch evidence was not preserved: %+v", index)
	}
	for _, entry := range index.Entries {
		if entry.CodedSHA256 != strings.Repeat("c", 64) || entry.DecodedSHA256 != strings.Repeat("d", 64) {
			t.Fatalf("branch hashes were mixed: %+v", entry)
		}
	}
}

func TestParseVideoSeekFrameHashPairsExactNativeTimesAcrossDifferentTimeBases(t *testing.T) {
	data := videoSeekHashTestHeaders("1/1000", "1/2000", "1/1000")
	for _, pts := range []int64{1000, 2000} {
		data += videoSeekHashTestRecord(0, pts-100, pts, 120, strings.Repeat("c", 64)) +
			videoSeekHashTestRecord(2, pts-100, pts, 40, strings.Repeat("e", 64)) +
			videoSeekHashTestRecord(3, pts-100, pts, 120, strings.Repeat("f", 64)) +
			videoSeekHashTestRecord(1, pts*2, pts*2, 6144, strings.Repeat("d", 64))
	}
	index := videoSeekHashTestParse(t, data, videoSeekHashTestBase(), MaxVideoSeekEntries)
	if len(index.Entries) != 2 || index.Entries[0].PTS != 1000 || index.Entries[0].DTS != 900 ||
		index.Entries[1].PTS != 2000 || index.TimeBaseNumerator != 1 || index.TimeBaseDenominator != 1000 {
		t.Fatalf("native timestamps were rounded or replaced by decoded timestamps: %+v", index)
	}
	data = strings.Replace(data, "1, 2000, 2000,", "1, 2001, 2001,", 1)
	if _, err := ParseVideoSeekFrameHash(strings.NewReader(data), videoSeekHashTestBase(), MaxVideoSeekEntries); err == nil {
		t.Fatal("different native presentation times were paired")
	}
}

func TestParseVideoSeekFrameHashDoesNotAuthorizeOpenGOPKeyPictures(t *testing.T) {
	data := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000") + videoSeekHashTestPoint(0)
	for _, pts := range []int64{1000, 2000, 3000} {
		data += videoSeekHashTestRecord(3, pts-100, pts, 120, strings.Repeat("f", 64)) +
			videoSeekHashTestRecord(1, pts, pts, 6144, strings.Repeat("d", 64))
	}
	index := videoSeekHashTestParse(t, data, videoSeekHashTestBase(), MaxVideoSeekEntries)
	if len(index.Entries) != 1 || index.Entries[0].PTS != 0 {
		t.Fatalf("non-IDR key pictures became seek evidence: %+v", index.Entries)
	}
}

func TestParseVideoSeekFrameHashRequiresObservedStaticParameterSets(t *testing.T) {
	headers := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000")
	withoutParameters := headers + videoSeekHashTestRecord(0, -100, 0, 120, strings.Repeat("c", 64)) +
		videoSeekHashTestRecord(3, -100, 0, 120, strings.Repeat("f", 64)) +
		videoSeekHashTestRecord(1, 0, 0, 6144, strings.Repeat("d", 64))
	for _, prefilled := range []string{"", strings.Repeat("e", 64)} {
		base := videoSeekHashTestBase()
		base.ParameterSetsSHA256 = prefilled
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(withoutParameters), base, MaxVideoSeekEntries); err == nil {
			t.Errorf("missing parameter-set branch was accepted with prefilled hash %q", prefilled)
		}
	}
	changed := headers + videoSeekHashTestPoint(0) + videoSeekHashTestPoint(1000)
	changed = strings.Replace(changed, videoSeekHashTestRecord(2, 900, 1000, 40, strings.Repeat("e", 64)),
		videoSeekHashTestRecord(2, 900, 1000, 40, strings.Repeat("f", 64)), 1)
	for _, limit := range []int{1, MaxVideoSeekEntries} {
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(changed), videoSeekHashTestBase(), limit); err == nil {
			t.Errorf("parameter-set changes were ignored with entry budget %d", limit)
		}
	}
	base := videoSeekHashTestBase()
	base.ParameterSetsSHA256 = strings.Repeat("f", 64)
	if _, err := ParseVideoSeekFrameHash(strings.NewReader(headers+videoSeekHashTestPoint(0)), base, MaxVideoSeekEntries); err == nil {
		t.Fatal("observed parameter sets that disagree with the index were accepted")
	}
}

func TestParseVideoSeekFrameHashRequiresAnEmptyCompleteNALScopeBranch(t *testing.T) {
	headers := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000")
	for _, pts := range []int64{0, 1000} {
		data := headers + videoSeekHashTestPoint(0) + videoSeekHashTestRecord(4, pts, pts, 1, strings.Repeat("a", 64))
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(data), videoSeekHashTestBase(), 1); err == nil {
			t.Errorf("forbidden NAL evidence at PTS %d was accepted", pts)
		}
	}
	fourHeaders := videoSeekHashTestHeadersForStreams("1/1000", "1/1000", "1/1000", 4)
	if _, err := ParseVideoSeekFrameHash(strings.NewReader(fourHeaders+videoSeekHashTestPoint(0)), videoSeekHashTestBase(), MaxVideoSeekEntries); err == nil {
		t.Fatal("analysis without the complete fifth branch was accepted")
	}
	truncated := strings.TrimSuffix(headers+videoSeekHashTestPoint(0), "\n")
	if _, err := ParseVideoSeekFrameHash(strings.NewReader(truncated), videoSeekHashTestBase(), MaxVideoSeekEntries); err == nil {
		t.Fatal("an empty scope branch with a truncated scan was accepted")
	}
}

func TestParseVideoSeekFrameHashUsesThreeBranchesForAnAlreadyScopedPreflight(t *testing.T) {
	base := videoSeekHashTestBase()
	base.NALScopeChecked = true
	base.PacketSideDataChecked = true
	threeHeaders := videoSeekHashTestHeadersForStreams("1/1000", "1/1000", "1/1000", 3)
	index := videoSeekHashTestParse(t, threeHeaders+videoSeekHashTestProofPoint(0), base, 1)
	if !index.NALScopeChecked || !index.PacketSideDataChecked || len(index.Entries) != 1 {
		t.Fatalf("bounded preflight did not preserve established scope evidence: %+v", index)
	}
	for _, count := range []int{4, 5} {
		headers := videoSeekHashTestHeadersForStreams("1/1000", "1/1000", "1/1000", count)
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(headers+videoSeekHashTestProofPoint(0)), base, 1); err == nil {
			t.Errorf("preflight accepted %d stream headers", count)
		}
	}
	for _, stream := range []int{3, 4} {
		extraHeader := threeHeaders + fmt.Sprintf("#tb %d: 1/1000\n", stream) + videoSeekHashTestProofPoint(0)
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(extraHeader), base, 1); err == nil {
			t.Errorf("preflight accepted an unexpected stream %d header", stream)
		}
		data := threeHeaders + videoSeekHashTestProofPoint(0) + videoSeekHashTestRecord(stream, 1, 1, 1, strings.Repeat("a", 64))
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(data), base, 1); err == nil {
			t.Errorf("preflight accepted an unexpected stream %d record", stream)
		}
	}
	base.PacketSideDataChecked = false
	if _, err := ParseVideoSeekFrameHash(strings.NewReader(threeHeaders+videoSeekHashTestProofPoint(0)), base, 1); err == nil {
		t.Fatal("preflight invented a missing original packet side-data proof")
	}
}

func TestParseVideoSeekFrameHashRejectsIncompleteOrChangingHeaders(t *testing.T) {
	valid := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000") + videoSeekHashTestPoint(0)
	for _, fixture := range []struct {
		name, old, replacement string
	}{
		{"missing_format", "#format: frame checksums\n", ""},
		{"missing_version", "#version: 2\n", ""},
		{"missing_hash", "#hash: SHA256\n", ""},
		{"missing_parameter_time_base", "#tb 2: 1/1000\n", ""},
		{"missing_original_time_base", "#tb 3: 1/1000\n", ""},
		{"missing_scope_time_base", "#tb 4: 1/1000\n", ""},
		{"missing_decoded_dimensions", "#dimensions 1: 64x64\n", ""},
		{"duplicate_global", "#hash: SHA256\n", "#hash: SHA256\n#hash: SHA256\n"},
		{"duplicate_stream", "#tb 0: 1/1000\n", "#tb 0: 1/1000\n#tb 0: 1/1000\n"},
		{"unknown_stream", "#tb 2: 1/1000\n", "#tb 5: 1/1000\n"},
		{"wrong_coded_clock", "#tb 0: 1/1000\n", "#tb 0: 1/2000\n"},
		{"wrong_parameter_clock", "#tb 2: 1/1000\n", "#tb 2: 1/2000\n"},
		{"zero_time_base", "#tb 1: 1/1000\n", "#tb 1: 0/1000\n"},
		{"zero_denominator", "#tb 1: 1/1000\n", "#tb 1: 1/0\n"},
		{"overflow_time_base", "#tb 1: 1/1000\n", "#tb 1: 1/9223372036854775808\n"},
		{"wrong_dimensions", "#dimensions 1: 64x64\n", "#dimensions 1: 128x64\n"},
		{"wrong_codec", "#codec_id 1: rawvideo\n", "#codec_id 1: h264\n"},
		{"wrong_media", "#media_type 2: video\n", "#media_type 2: audio\n"},
		{"wrong_hash_algorithm", "#hash: SHA256\n", "#hash: MD5\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			invalid := strings.Replace(valid, fixture.old, fixture.replacement, 1)
			if _, err := ParseVideoSeekFrameHash(strings.NewReader(invalid), videoSeekHashTestBase(), MaxVideoSeekEntries); err == nil {
				t.Fatal("incomplete or incompatible headers were accepted")
			}
		})
	}
	for _, invalid := range []string{
		videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000"),
		valid + "#dimensions 1: 128x64\n",
		strings.TrimSuffix(valid, "\n"),
		"#software: " + strings.Repeat("x", maxVideoSeekLine) + "\n" + valid,
	} {
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(invalid), videoSeekHashTestBase(), MaxVideoSeekEntries); err == nil {
			t.Fatal("incomplete, changed, truncated, or oversized output was accepted")
		}
	}
}

func TestParseVideoSeekFrameHashRejectsMalformedOrAmbiguousRecords(t *testing.T) {
	headers := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000")
	first := videoSeekHashTestPoint(0)
	for _, fixture := range []struct {
		name, records string
	}{
		{"unknown_stream", strings.Replace(first, "0, -100, 0,", "5, -100, 0,", 1)},
		{"missing_pts", strings.Replace(first, "0, -100, 0,", "0, -100, N/A,", 1)},
		{"reserved_pts", strings.Replace(first, "0, -100, 0,", "0, -100, -9223372036854775808,", 1)},
		{"negative_duration", strings.Replace(first, "0, -100, 0, 1,", "0, -100, 0, -1,", 1)},
		{"empty_packet", strings.Replace(first, "1, 120,", "1, 0,", 1)},
		{"oversized_packet", strings.Replace(first, "1, 120,", "1, 536870913,", 1)},
		{"short_hash", strings.Replace(first, strings.Repeat("c", 64), strings.Repeat("c", 63), 1)},
		{"non_hex_hash", strings.Replace(first, strings.Repeat("c", 64), strings.Repeat("z", 64), 1)},
		{"uppercase_hash", strings.Replace(first, strings.Repeat("c", 64), strings.Repeat("C", 64), 1)},
		{"duplicate_coded", first + videoSeekHashTestRecord(0, -100, 0, 120, strings.Repeat("c", 64))},
		{"duplicate_decoded", first + videoSeekHashTestRecord(1, 0, 0, 6144, strings.Repeat("d", 64))},
		{"duplicate_parameters", first + videoSeekHashTestRecord(2, -100, 0, 40, strings.Repeat("e", 64))},
		{"decreasing_coded_pts", videoSeekHashTestPoint(1000) + videoSeekHashTestRecord(0, 1900, 500, 120, strings.Repeat("c", 64))},
		{"decreasing_decoded_pts", first + videoSeekHashTestRecord(1, 1000, -1, 6144, strings.Repeat("d", 64))},
		{"decreasing_dts", first + videoSeekHashTestRecord(0, -101, 1000, 120, strings.Repeat("c", 64))},
		{"duplicate_original_dts", first + videoSeekHashTestRecord(3, -100, 1000, 120, strings.Repeat("f", 64))},
		{"missing_decoded_picture", first + videoSeekHashTestRecord(0, 900, 1000, 120, strings.Repeat("c", 64))},
		{"decoded_picture_after_idr", strings.Replace(first, "1, 0, 0,", "1, 1000, 1000,", 1)},
		{"changed_frame_size", first + strings.Replace(videoSeekHashTestPoint(1000), "1, 6144,", "1, 6145,", 1)},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			if _, err := ParseVideoSeekFrameHash(strings.NewReader(headers+fixture.records), videoSeekHashTestBase(), MaxVideoSeekEntries); err == nil {
				t.Fatal("malformed or ambiguous records were accepted")
			}
		})
	}
}

func TestParseVideoSeekFrameHashAcceptsBoundedTransportPacketSideData(t *testing.T) {
	first := videoSeekHashTestPoint(0)
	coded := videoSeekHashTestRecord(0, -100, 0, 120, strings.Repeat("c", 64))
	streamIDHash := sha256.Sum256([]byte{0xe0})
	withSideData := strings.TrimSuffix(coded, "\n") + fmt.Sprintf(", S=1, 1, %x\n", streamIDHash)
	headers := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000")
	index := videoSeekHashTestParse(t, headers+strings.Replace(first, coded, withSideData, 1), videoSeekHashTestBase(), MaxVideoSeekEntries)
	if len(index.Entries) != 1 || index.Entries[0].CodedSHA256 != strings.Repeat("c", 64) {
		t.Fatalf("packet side data changed the coded evidence: %+v", index)
	}
	for _, sideData := range []string{
		", unexpected", ", S=1", ", S=1, 1", ", S=-1", ", S=17",
		", S=1, -1, " + strings.Repeat("a", 64), ", S=1, 1, invalid",
	} {
		invalid := strings.TrimSuffix(coded, "\n") + sideData + "\n"
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(headers+strings.Replace(first, coded, invalid, 1)), videoSeekHashTestBase(), MaxVideoSeekEntries); err == nil {
			t.Errorf("invalid side data %q was accepted", sideData)
		}
	}
}

func TestParseVideoSeekFrameHashChecksOriginalPacketsBeyondIDRMarkers(t *testing.T) {
	headers := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000")
	first := videoSeekHashTestPoint(0)
	original := videoSeekHashTestRecord(3, 900, 1000, 120, strings.Repeat("f", 64))
	for _, sideData := range []string{
		", S=1, 17, " + strings.Repeat("a", 64),
		", S=1, 1, " + strings.Repeat("a", 64),
		", S=2, 1, " + strings.Repeat("a", 64) + ", 17, " + strings.Repeat("b", 64),
	} {
		invalid := headers + first + strings.TrimSuffix(original, "\n") + sideData + "\n"
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(invalid), videoSeekHashTestBase(), 1); err == nil {
			t.Errorf("unproven side data on a non-IDR packet was accepted: %s", sideData)
		}
	}
	for streamID := byte(0xe0); streamID <= 0xef; streamID++ {
		digest := sha256.Sum256([]byte{streamID})
		data := headers + first + strings.TrimSuffix(original, "\n") + fmt.Sprintf(", S=1, 1, %x\n", digest)
		index := videoSeekHashTestParse(t, data, videoSeekHashTestBase(), MaxVideoSeekEntries)
		if !index.PacketSideDataChecked || len(index.Entries) != 1 {
			t.Fatalf("known transport stream ID did not retain bounded evidence: %+v", index)
		}
	}
	originalFirst := videoSeekHashTestRecord(3, -100, 0, 120, strings.Repeat("f", 64))
	missingOriginal := headers + strings.Replace(first, originalFirst, "", 1)
	for _, prefilled := range []bool{false, true} {
		base := videoSeekHashTestBase()
		base.PacketSideDataChecked = prefilled
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(missingOriginal), base, MaxVideoSeekEntries); err == nil {
			t.Errorf("missing original-packet branch was accepted with prefilled flag %t", prefilled)
		}
	}
}

func TestParseVideoSeekFrameHashAllowsOriginalBFramePresentationReordering(t *testing.T) {
	data := videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000") + videoSeekHashTestPoint(0)
	for position, pts := range []int64{3000, 1000, 2000} {
		data += videoSeekHashTestRecord(3, int64(position)*1000, pts, 120, strings.Repeat("f", 64))
	}
	index := videoSeekHashTestParse(t, data, videoSeekHashTestBase(), MaxVideoSeekEntries)
	if !index.PacketSideDataChecked || len(index.Entries) != 1 || index.Entries[0].PTS != 0 {
		t.Fatalf("original packet reordering changed the restart evidence: %+v", index)
	}
}

func TestParseVideoSeekFrameHashBoundsPendingRecordsAndParserConfiguration(t *testing.T) {
	base := videoSeekHashTestBase()
	for _, limit := range []int{-1, 0, MaxVideoSeekEntries + 1} {
		if _, err := ParseVideoSeekFrameHash(strings.NewReader(""), base, limit); err == nil {
			t.Errorf("entry budget %d was accepted", limit)
		}
	}
	if _, err := ParseVideoSeekFrameHash(nil, base, MaxVideoSeekEntries); err == nil {
		t.Fatal("nil input was accepted")
	}
	var data strings.Builder
	data.WriteString(videoSeekHashTestHeaders("1/1000", "1/1000", "1/1000"))
	data.WriteString(videoSeekHashTestRecord(2, -100, 0, 40, strings.Repeat("e", 64)))
	for position := 0; position <= maxVideoSeekPending; position++ {
		pts := int64(position) * 1000
		data.WriteString(videoSeekHashTestRecord(0, pts-100, pts, 120, strings.Repeat("c", 64)))
	}
	_, err := ParseVideoSeekFrameHash(strings.NewReader(data.String()), base, MaxVideoSeekEntries)
	if err == nil || !strings.Contains(err.Error(), "pending record budget") {
		t.Fatalf("pending records were not bounded before EOF: %v", err)
	}
}

func videoSeekHashTestArgValue(t *testing.T, args []string, option string) string {
	t.Helper()
	position := slices.Index(args, option)
	if position < 0 || position+1 >= len(args) {
		t.Fatalf("missing option %s in %v", option, args)
	}
	return args[position+1]
}

func TestBuildVideoSeekCommandArgsPreservesSourceIsolationAndBoundedProofBranches(t *testing.T) {
	for _, candidate := range []*string{nil, new(string)} {
		if candidate != nil {
			*candidate = "121.5666666"
		}
		args, err := BuildVideoSeekCommandArgs(7, candidate, 1)
		if err != nil {
			t.Fatal(err)
		}
		inputPosition := slices.Index(args, "-i")
		for _, option := range []string{"-protocol_whitelist", "-format_whitelist", "-threads", "-filter_threads", "-filter_complex_threads", "-max_pixels", "-max_alloc"} {
			if position := slices.Index(args, option); position < 0 || position >= inputPosition {
				t.Fatalf("input option %s was not applied before the source: %v", option, args)
			}
		}
		for option, want := range map[string]string{
			"-protocol_whitelist": "file,pipe", "-format_whitelist": probeFormats, "-i": "/proc/self/fd/3",
			"-threads": "1", "-filter_threads": "1", "-filter_complex_threads": "1",
			"-max_pixels": "8847360", "-max_alloc": "67108864",
			"-c:v:0": "copy", "-c:v:1": "rawvideo", "-c:v:2": "copy",
			"-bsf:v:0": "filter_units=pass_types=5", "-bsf:v:2": "h264_mp4toannexb,filter_units=pass_types=7|8",
			"-copypriorss:v:0": "1", "-copypriorss:v:2": "1", "-threads:v:1": "1",
			"-fps_mode:v:1": "passthrough", "-enc_time_base:v:1": "demux", "-f": "framehash", "-hash": "sha256",
		} {
			if got := videoSeekHashTestArgValue(t, args, option); got != want {
				t.Errorf("%s = %q; want %q", option, got, want)
			}
		}
		for _, option := range []string{"-copyts", "-nostdin", "-copyinkf:v:0", "-copyinkf:v:2"} {
			if !slices.Contains(args, option) {
				t.Errorf("required source-clock option %s is absent", option)
			}
		}
		maps := 0
		for position, arg := range args {
			if arg == "-map" {
				maps++
				if position+1 >= len(args) || args[position+1] != "0:7" {
					t.Fatalf("proof branch does not map the original selected stream: %v", args)
				}
			}
		}
		wantMaps := 5
		if candidate != nil {
			wantMaps = 3
		}
		if maps != wantMaps || args[len(args)-1] != "pipe:1" {
			t.Fatalf("expected %d hash branches and bounded pipe output: %v", wantMaps, args)
		}
		for _, option := range []string{"-skip_frame", "-c:v:3", "-copyinkf:v:3", "-copypriorss:v:3", "-c:v:4", "-copyinkf:v:4", "-copypriorss:v:4", "-bsf:v:4"} {
			if slices.Contains(args, option) != (candidate == nil) {
				t.Errorf("analysis scope option %s does not match candidate presence", option)
			}
		}
		for _, option := range []string{"-frames:v:3", "-frames:v:4"} {
			if slices.Contains(args, option) {
				t.Errorf("unexpected frame limit %s in analysis or bounded preflight: %v", option, args)
			}
		}
		if candidate == nil {
			for option, want := range map[string]string{
				"-skip_frame": "nokey", "-c:v:3": "copy", "-copypriorss:v:3": "1",
				"-c:v:4": "copy", "-copypriorss:v:4": "1",
				"-bsf:v:4": "filter_units=remove_types=1|5|6|7|8|9|10|11|12",
			} {
				if got := videoSeekHashTestArgValue(t, args, option); got != want {
					t.Errorf("%s = %q; want %q", option, got, want)
				}
			}
			if slices.Index(args, "-skip_frame") >= inputPosition || slices.Contains(args, "-filter:v:1") {
				t.Errorf("analysis did not keep key-picture selection on the input: %v", args)
			}
		}
		for _, option := range []string{"-ss", "-seek_timestamp", "-noaccurate_seek", "-filter:v:1", "-frames:v:0", "-frames:v:1", "-frames:v:2"} {
			if slices.Contains(args, option) != (candidate != nil) {
				t.Errorf("preflight option %s does not match candidate presence", option)
			}
		}
		if candidate != nil {
			if videoSeekHashTestArgValue(t, args, "-ss") != *candidate || slices.Index(args, "-ss") >= inputPosition ||
				videoSeekHashTestArgValue(t, args, "-seek_timestamp") != "1" {
				t.Fatalf("preflight changed the exact input seek argument: %v", args)
			}
			if videoSeekHashTestArgValue(t, args, "-filter:v:1") != "select=key" || slices.Index(args, "-filter:v:1") <= inputPosition {
				t.Errorf("preflight did not select key pictures after decoding: %v", args)
			}
			for _, option := range []string{"-frames:v:0", "-frames:v:1", "-frames:v:2"} {
				if videoSeekHashTestArgValue(t, args, option) != "1" {
					t.Errorf("preflight branch %s was not limited to one record", option)
				}
			}
		}
	}
}

func TestBuildVideoSeekCommandArgsRejectsInvalidCandidates(t *testing.T) {
	if _, err := BuildVideoSeekCommandArgs(-1, nil, 1); err == nil {
		t.Fatal("negative stream index was accepted")
	}
	for _, candidate := range []string{
		"", " ", " 1", "1 ", "1\n2", "1\x00", "NaN", "Inf", "1e2", "1E2", "1/2", "00:01",
		"0x1p0", "0b10", "1_0", "1.2.3", "1; touch output", strings.Repeat("1", 65),
	} {
		if _, err := BuildVideoSeekCommandArgs(0, &candidate, 1); err == nil {
			t.Errorf("invalid candidate %q was accepted", candidate)
		}
	}
	for _, candidate := range []string{"0", "-0.1250000", "121.5666666"} {
		if _, err := BuildVideoSeekCommandArgs(0, &candidate, 1); err != nil {
			t.Errorf("decimal source timestamp %q was rejected: %v", candidate, err)
		}
	}
}

func TestParseVideoSeekTimeBaseRejectsNonIntegerAndUnboundedNumericSyntax(t *testing.T) {
	for _, value := range []string{
		"", "1e100000000/1", "1/0", "1/1e100000000", "0x1/2", "1.5/2",
		"0/1", "-1/90000", "1/-90000", "1/9223372036854775808",
	} {
		if _, err := parseVideoSeekTimeBase(value); err == nil {
			t.Errorf("invalid or unbounded time base %q was accepted", value)
		}
	}
	base, err := parseVideoSeekTimeBase("1/90000")
	if err != nil || base == nil || base.RatString() != "1/90000" {
		t.Fatalf("native integer time base was not preserved: %v, %v", base, err)
	}
}

func TestParseVideoSeekIndexRejectsUnknownFieldsAndExcessBytes(t *testing.T) {
	index := videoSeekCandidateTestIndex(10, 20)
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseVideoSeekIndex(data)
	if err != nil || !reflect.DeepEqual(parsed, index) {
		t.Fatalf("index JSON did not preserve the native evidence: %+v, %v", parsed, err)
	}
	encoded := string(data)
	for _, invalid := range []string{
		"", "null", "[]", `"index"`, encoded + "{}", encoded[:len(encoded)-1],
		strings.TrimSuffix(encoded, "}") + `,"unknown":true}`,
		strings.Replace(encoded, `"pts":10`, `"pts":10,"unknown":true`, 1),
	} {
		if _, err := ParseVideoSeekIndex([]byte(invalid)); err == nil {
			t.Errorf("invalid index JSON was accepted: %q", invalid)
		}
	}
	padded := encoded + strings.Repeat(" ", MaxVideoSeekIndexBytes-len(encoded))
	if _, err := ParseVideoSeekIndex([]byte(padded)); err != nil {
		t.Fatalf("index exactly at its byte budget was rejected: %v", err)
	}
	if _, err := ParseVideoSeekIndex([]byte(padded + " ")); err == nil {
		t.Fatal("index above its byte budget was accepted")
	}
}

func TestValidateVideoSeekIndexRejectsInvalidEvidenceAndEntryCounts(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		mutate func(*VideoSeekIndex)
	}{
		{"version", func(i *VideoSeekIndex) { i.Version++ }},
		{"negative_stream", func(i *VideoSeekIndex) { i.StreamIndex = -1 }},
		{"zero_duration", func(i *VideoSeekIndex) { i.DurationTicks = 0 }},
		{"zero_denominator", func(i *VideoSeekIndex) { i.TimeBaseDenominator = 0 }},
		{"missing_source", func(i *VideoSeekIndex) { i.SourceIdentity = "" }},
		{"missing_tool", func(i *VideoSeekIndex) { i.ToolIdentity = "" }},
		{"missing_parameters", func(i *VideoSeekIndex) { i.ParameterSetsSHA256 = "" }},
		{"missing_side_data_scan", func(i *VideoSeekIndex) { i.PacketSideDataChecked = false }},
		{"missing_nal_scope_scan", func(i *VideoSeekIndex) { i.NALScopeChecked = false }},
		{"invalid_dimensions", func(i *VideoSeekIndex) { i.Width = 0 }},
		{"missing_frame_size", func(i *VideoSeekIndex) { i.DecodedFrameBytes = 0 }},
		{"bad_coded_hash", func(i *VideoSeekIndex) { i.Entries[0].CodedSHA256 = "invalid" }},
		{"bad_decoded_hash", func(i *VideoSeekIndex) { i.Entries[0].DecodedSHA256 = "invalid" }},
		{"duplicate_pts", func(i *VideoSeekIndex) { i.Entries[1].PTS = i.Entries[0].PTS }},
		{"decreasing_dts", func(i *VideoSeekIndex) { i.Entries[1].DTS = i.Entries[0].DTS - 1 }},
		{"no_entries", func(i *VideoSeekIndex) { i.Entries = nil }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			index := videoSeekCandidateTestIndex(10, 20)
			fixture.mutate(&index)
			data, err := json.Marshal(index)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseVideoSeekIndex(data); err == nil {
				t.Fatal("invalid index evidence was accepted")
			}
		})
	}
	points := make([]int64, MaxVideoSeekEntries)
	for position := range points {
		points[position] = int64(position)
	}
	index := videoSeekCandidateTestIndex(points...)
	if err := ValidateVideoSeekIndex(index); err != nil {
		t.Fatalf("index at its entry budget was rejected: %v", err)
	}
	index.Entries = append(index.Entries, videoSeekCandidateTestIndex(MaxVideoSeekEntries).Entries[0])
	if err := ValidateVideoSeekIndex(index); err == nil {
		t.Fatal("index above its entry budget was accepted")
	}
}
