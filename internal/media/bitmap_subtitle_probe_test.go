package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBitmapSubtitleHexDumpDoesNotReadASCIIColumn(t *testing.T) {
	data, err := decodeSubtitleHexDump("\n00000000: 0001 abff 2020                           ....  abcd1234\n", 6, 6)
	if err != nil || hex.EncodeToString(data) != "0001abff2020" {
		t.Fatalf("%x %v", data, err)
	}
	data, err = decodeSubtitleHexDump("\n00000000: 2020                                      \n", 2, 2)
	if err != nil || string(data) != "  " {
		t.Fatalf("whitespace ASCII column: %x %v", data, err)
	}
	for _, text := range []string{"00000001: 01  .", "00000000: 01", "00000000: zz  .", "00000000: 000102  ..."} {
		if _, err := decodeSubtitleHexDump(text, 2, 2); err == nil {
			t.Fatalf("invalid dump accepted: %q", text)
		}
	}
}

func bitmapProbeDocument(t *testing.T, pts any, streamIndex int) []byte {
	t.Helper()
	document := map[string]any{
		"packets": []map[string]any{{"stream_index": streamIndex, "pts": pts, "duration": 90000, "size": "3", "data": "\n00000000: 8000 00                                  ...\n"}},
		"streams": []map[string]any{{"index": 2, "codec_name": "hdmv_pgs_subtitle", "codec_type": "subtitle", "time_base": "1/90000"}},
		"format":  map[string]any{"start_time": "1.500000"},
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBitmapSubtitleProbePreservesAbsoluteIndexAndSourceClock(t *testing.T) {
	stream := Stream{Index: 2, Codec: "hdmv_pgs_subtitle", CodecType: "subtitle"}
	source := Info{FormatStartKnown: true, FormatStartTicks: 15_000_000}
	packets, _, err := parseBitmapSubtitleProbe(bitmapProbeDocument(t, 180000, 2), stream, source)
	if err != nil || len(packets) != 1 || packets[0].PTS != 5_000_000 || packets[0].Duration != TicksPerSecond {
		t.Fatalf("%+v %v", packets, err)
	}
	if _, _, err := parseBitmapSubtitleProbe(bitmapProbeDocument(t, 180000, 0), stream, source); err == nil {
		t.Fatal("filtered-position index accepted")
	}
	if _, _, err := parseBitmapSubtitleProbe(bitmapProbeDocument(t, "N/A", 2), stream, source); err == nil {
		t.Fatal("missing PTS accepted")
	}
	source.FormatStartTicks++
	if _, _, err := parseBitmapSubtitleProbe(bitmapProbeDocument(t, 180000, 2), stream, source); err == nil {
		t.Fatal("changed source origin accepted")
	}
}

func TestBitmapSubtitleProbeUnknownOriginRetainsContainerPacketTime(t *testing.T) {
	stream := Stream{Index: 2, Codec: "hdmv_pgs_subtitle", CodecType: "subtitle"}
	for _, reported := range []any{nil, "N/A", "0.000000", "1.500000"} {
		var document map[string]any
		if err := json.Unmarshal(bitmapProbeDocument(t, 180000, 2), &document); err != nil {
			t.Fatal(err)
		}
		document["format"] = map[string]any{"start_time": reported}
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		packets, _, err := parseBitmapSubtitleProbe(data, stream, Info{})
		if err != nil || len(packets) != 1 || packets[0].PTS != 20_000_000 {
			t.Fatalf("unknown indexed origin shifted raw PTS for fresh start %v: %+v %v", reported, packets, err)
		}
	}
	if _, _, err := parseBitmapSubtitleProbe(bitmapProbeDocument(t, 180000, 2), stream, Info{FormatStartTicks: 1}); err == nil {
		t.Fatal("unknown origin with a contradictory numeric value was accepted")
	}
}

func TestBitmapSubtitleRationalTimestampsNeverUseFloatingPoint(t *testing.T) {
	got, err := bitmapScalarTicks(scalar("-1"), big.NewRat(1, 90000))
	if err != nil || got != -112 {
		t.Fatalf("negative floor %d %v", got, err)
	}
	if _, err := bitmapScalarTicks(scalar("1e100"), big.NewRat(1, 1)); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestBitmapSubtitlePacketArrayRejectsExcessCountDuringDecoding(t *testing.T) {
	const packet = `{"size":1,"data":"00000000: 01  ."}`
	data := "[" + strings.Repeat(packet+",", maxBitmapSubtitlePackets) + packet + "]"
	var packets bitmapProbePackets
	if err := json.Unmarshal([]byte(data), &packets); err == nil || !strings.Contains(err.Error(), "count limit") || len(packets) != 0 {
		t.Fatalf("oversized packet list was partially retained: %d %v", len(packets), err)
	}
	if err := json.Unmarshal([]byte(`[{"size":33554433,"data":"00000000: 01  ."}]`), &packets); err == nil {
		t.Fatal("declared packet budget was deferred until raw byte parsing")
	}
}

func TestPinnedSubtitleToolUsesHeldDescriptorAndRejectsChangedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool")
	content := []byte("owned fixture executable bytes")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	file, before, err := openPinnedSubtitleFile(context.Background(), path, hex.EncodeToString(digest[:]), 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if !subtitleFileUnchanged(file, before) {
		t.Fatal("new held descriptor is stale")
	}
	if _, _, err := openPinnedSubtitleFile(context.Background(), path, strings.Repeat("0", 64), 1024); err == nil {
		t.Fatal("wrong digest accepted")
	}
	if _, _, err := openPinnedSubtitleFile(context.Background(), path, hex.EncodeToString(digest[:]), 2); err == nil {
		t.Fatal("oversized tool accepted")
	}
	if _, _, err := openPinnedSubtitleFile(context.Background(), "tool", hex.EncodeToString(digest[:]), 1024); err == nil {
		t.Fatal("relative tool accepted")
	}
}
