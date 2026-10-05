package server

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestAudioWaveformLevelWirePreservesStreamZeroAndValidity(t *testing.T) {
	const duration int64 = 0x01020304050607
	for _, count := range []int{512, 1024, 2048, 4096} {
		level := media.AudioWaveformLevel{BucketCount: count, Peaks: make([]uint16, count), RMS: make([]uint16, count), Validity: make([]byte, count/8)}
		// Bucket zero is valid digital silence. Bucket one is absent; the
		// validity byte must distinguish them despite their equal amplitudes.
		level.Validity[0] = 0x05
		level.Peaks[2], level.RMS[2] = 0x1234, 0x0123
		wire, err := marshalAudioWaveformLevel(duration, 0, level)
		if err != nil || len(wire) != 32+count*4+count/8 {
			t.Fatalf("level %d size: %d %v", count, len(wire), err)
		}
		if string(wire[:4]) != "GAWL" || binary.LittleEndian.Uint16(wire[4:]) != 1 || binary.LittleEndian.Uint16(wire[6:]) != 32 ||
			binary.LittleEndian.Uint32(wire[8:]) != 0 || binary.LittleEndian.Uint32(wire[12:]) != uint32(count) || binary.LittleEndian.Uint64(wire[16:]) != uint64(duration) ||
			binary.LittleEndian.Uint32(wire[24:]) != uint32(count/8) || !bytes.Equal(wire[28:32], make([]byte, 4)) ||
			!bytes.Equal(wire[40:44], []byte{0x34, 0x12, 0x23, 0x01}) || wire[32+count*4] != 0x05 {
			t.Fatalf("level %d changed its little-endian header, stream zero, RMS or validity layout", count)
		}
		level.RMS[2] = level.Peaks[2] + 1
		if _, err := marshalAudioWaveformLevel(duration, 0, level); !errors.Is(err, media.ErrAudioWaveformUnsupported) {
			t.Fatal("RMS above peak was accepted")
		}
		level.RMS[2], level.Peaks[1] = 0, 1
		if _, err := marshalAudioWaveformLevel(duration, 0, level); !errors.Is(err, media.ErrAudioWaveformUnsupported) {
			t.Fatal("an invalid bucket contained presentation energy")
		}
		level.Peaks[1] = 0
		level.Validity = level.Validity[:len(level.Validity)-1]
		if _, err := marshalAudioWaveformLevel(duration, 0, level); !errors.Is(err, media.ErrAudioWaveformUnsupported) {
			t.Fatal("a truncated validity mask was accepted")
		}
	}
}

func TestAudioWaveformPublicQueriesRejectAmbiguousAuthorityAndLevels(t *testing.T) {
	for _, test := range []struct {
		level bool
		query string
		valid bool
	}{
		{false, "", true}, {false, "?UserId=user&api_key=key", true}, {true, "?buckets=512&tag=current&UserId=user", true},
		{false, "?", false}, {false, "?Force=true", false}, {false, "?tag=current", false},
		{false, "?UserId=a&UserId=b", false}, {false, "?UserId=", false}, {false, "?api_key=a&api_key=b", false},
		{true, "?buckets=512&buckets=1024&tag=current", false}, {true, "?buckets=512&tag=a&tag=b", false},
		{true, "?buckets=512&tag=current&Unknown=x", false}, {true, "?Buckets=512&tag=current", false},
	} {
		w := httptest.NewRecorder()
		_, valid := audioWaveformQuery(w, httptest.NewRequest(http.MethodGet, "/emby/Items/item/AudioWaveforms"+test.query, nil), test.level)
		if valid != test.valid || !valid && w.Code != http.StatusBadRequest {
			t.Fatalf("query %q: accepted=%t status=%d", test.query, valid, w.Code)
		}
	}
}

func TestAudioWaveformRunInputRetainsForceAndTypedScope(t *testing.T) {
	const raw = `{"Kind":"waveform","RequestId":"waveform-retry","LibraryIds":["library-b","library-a"],"ItemIds":["item-b","item-a"],"Force":true}`
	input, ok := decodeAdminMediaAnalysisRun(httptest.NewRecorder(), analysisInputRequestForTest(raw))
	if !ok || input.TaskKey != library.TaskAudioWaveformGenerationKey || input.RequestID != "waveform-retry" || !input.Selection.Force ||
		!reflect.DeepEqual(input.Selection.ItemIDs, []string{"item-a", "item-b"}) || !reflect.DeepEqual(input.Selection.LibraryIDs, []string{"library-a", "library-b"}) {
		t.Fatalf("waveform scope changed: %+v", input)
	}
	for _, invalid := range []string{
		strings.Replace(raw, `"waveform"`, `"waveforms"`, 1), strings.Replace(raw, `"waveform-retry"`, `null`, 1),
		strings.Replace(raw, `"Force":true`, `"Force":null`, 1), strings.Replace(raw, `"Force":true`, `"Force":true,"Force":false`, 1),
		strings.Replace(raw, `["item-b","item-a"]`, `["item-a","item-a"]`, 1),
	} {
		w := httptest.NewRecorder()
		if _, ok := decodeAdminMediaAnalysisRun(w, analysisInputRequestForTest(invalid)); ok || w.Code != http.StatusBadRequest {
			t.Fatalf("invalid waveform admission accepted: %s", invalid)
		}
	}
}
