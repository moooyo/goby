package transcode

import (
	"strings"
	"testing"
)

func TestGeneratedAVPreMuxDiagnosticPreservesRawSignedClock(t *testing.T) {
	writer, err := newGeneratedAVPreMuxWriter(1, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("GOBY_AV_MUX 1 1 0 1/48000 -1024 -1024 321 K\nGOBY_AV_MUX 1 1 1 1/48000 0 0 305 K\n")); err != nil {
		t.Fatal(err)
	}
	got, err := writer.finish()
	if err != nil {
		t.Fatal(err)
	}
	if got.Qualified || got.ReaderEOF || !got.RecordsParsed || len(got.Packets) != 2 || got.Packets[0].PTS != -1024 || got.Packets[0].DTS != -1024 || got.Packets[1].Number != 1 {
		t.Fatal("pre-mux packet clock was rebased or qualified")
	}
}

func TestGeneratedAVPreMuxDiagnosticCannotRepairMissingOrPartialRecords(t *testing.T) {
	for _, data := range []string{"GOBY_AV_MUX 0 1 0 1/48000 0 0 10 K", "GOBY_AV_MUX 0 1 1 1/48000 0 0 10 K\n", "GOBY_AV_MUX 0 1 0 1/48000 0 9223372036854775807 10 K\n", "GOBY_AV_MUX 0 0 0 1/48000 0 0 10 K\n", strings.Repeat("x", 513)} {
		writer, _ := newGeneratedAVPreMuxWriter(0, 1, nil)
		_, writeErr := writer.Write([]byte(data))
		got, finishErr := writer.finish()
		if writeErr == nil && finishErr == nil || got.RecordsParsed || got.ReaderEOF || len(got.Packets) != 0 {
			t.Fatal("bad raw record became complete packet evidence")
		}
	}
}
