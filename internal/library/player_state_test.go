package library

import (
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func playerStatePtr[T any](value T) *T { return &value }

func playerStateFullFixture() PlayerState {
	return PlayerState{
		CanSeek: playerStatePtr(true), IsMuted: playerStatePtr(true), VolumeLevel: playerStatePtr(80),
		AudioStreamIndex: playerStatePtr(5), SubtitleStreamIndex: playerStatePtr(2),
		PlayMethod: playerStatePtr("DirectStream"), RepeatMode: playerStatePtr("RepeatAll"),
		PlaybackRate: playerStatePtr(1.5), Shuffle: playerStatePtr(true), SubtitleOffset: playerStatePtr(25),
	}
}

func TestPlayerStateEncodingPreservesUnknownAndExplicitZeroValues(t *testing.T) {
	encoded, err := encodePlayerStateUpdate(nil)
	if err != nil || string(encoded) != "{}" {
		t.Fatalf("omitted player state encoded as %q, error=%v", encoded, err)
	}
	empty, err := decodePlayerState(encoded)
	if err != nil || !reflect.DeepEqual(empty, PlayerState{}) {
		t.Fatalf("omitted state invented known values: %+v, error=%v", empty, err)
	}
	state := PlayerState{
		CanSeek: playerStatePtr(false), IsMuted: playerStatePtr(false), VolumeLevel: playerStatePtr(0),
		AudioStreamIndex: playerStatePtr(-1), SubtitleStreamIndex: playerStatePtr(0),
		PlayMethod: playerStatePtr("DirectPlay"), RepeatMode: playerStatePtr("RepeatNone"),
		PlaybackRate: playerStatePtr(1.0), Shuffle: playerStatePtr(false), SubtitleOffset: playerStatePtr(0),
	}
	encoded, err = encodePlayerStateUpdate(&state)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodePlayerState(encoded)
	if err != nil || !reflect.DeepEqual(decoded, state) {
		t.Fatalf("explicit zero/false state did not round trip: %+v, error=%v", decoded, err)
	}
	for _, field := range []string{`"CanSeek":false`, `"VolumeLevel":0`, `"AudioStreamIndex":-1`, `"SubtitleOffset":0`} {
		if !strings.Contains(string(encoded), field) {
			t.Errorf("player state omitted explicit field %s", field)
		}
	}
}

func playerStateInvalidUpdates() map[string]PlayerStateUpdate {
	values := map[string]PlayerStateUpdate{
		"negative volume":     {VolumeLevel: playerStatePtr(-1)},
		"excess volume":       {VolumeLevel: playerStatePtr(101)},
		"negative audio":      {AudioStreamIndex: playerStatePtr(-2)},
		"negative subtitle":   {SubtitleStreamIndex: playerStatePtr(-2)},
		"empty play method":   {PlayMethod: playerStatePtr("")},
		"invalid play method": {PlayMethod: playerStatePtr("copy")},
		"invalid repeat mode": {RepeatMode: playerStatePtr("repeatall")},
		"zero rate":           {PlaybackRate: playerStatePtr(0.0)},
		"negative rate":       {PlaybackRate: playerStatePtr(-1.0)},
		"excess rate":         {PlaybackRate: playerStatePtr(10.0001)},
		"NaN rate":            {PlaybackRate: playerStatePtr(math.NaN())},
		"infinite rate":       {PlaybackRate: playerStatePtr(math.Inf(1))},
		"negative infinity":   {PlaybackRate: playerStatePtr(math.Inf(-1))},
	}
	if strconv.IntSize > 32 {
		overflow := int64(math.MaxInt32) + 1
		underflow := int64(math.MinInt32) - 1
		values["audio int32 overflow"] = PlayerStateUpdate{AudioStreamIndex: playerStatePtr(int(overflow))}
		values["subtitle int32 overflow"] = PlayerStateUpdate{SubtitleStreamIndex: playerStatePtr(int(overflow))}
		values["offset int32 overflow"] = PlayerStateUpdate{SubtitleOffset: playerStatePtr(int(overflow))}
		values["offset int32 underflow"] = PlayerStateUpdate{SubtitleOffset: playerStatePtr(int(underflow))}
	}
	return values
}

func TestPlayerStateValidatesSupportedValueRanges(t *testing.T) {
	for name, update := range playerStateInvalidUpdates() {
		t.Run(name, func(t *testing.T) {
			if encoded, err := encodePlayerStateUpdate(&update); !errors.Is(err, ErrInvalidInput) || len(encoded) != 0 {
				t.Fatalf("invalid player state returned bytes=%q error=%v", encoded, err)
			}
		})
	}
	for _, method := range []string{"DirectPlay", "DirectStream", "Transcode"} {
		for _, repeat := range []string{"RepeatNone", "RepeatAll", "RepeatOne"} {
			for _, rate := range []float64{math.SmallestNonzeroFloat64, 1, 10} {
				state := PlayerState{PlayMethod: &method, RepeatMode: &repeat, PlaybackRate: &rate,
					VolumeLevel: playerStatePtr(100), AudioStreamIndex: playerStatePtr(math.MaxInt32), SubtitleOffset: playerStatePtr(math.MinInt32)}
				encoded, err := encodePlayerStateUpdate(&state)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := decodePlayerState(encoded)
				if err != nil || !reflect.DeepEqual(decoded, state) {
					t.Fatalf("valid boundary state did not round trip: %+v, %v", decoded, err)
				}
			}
		}
	}
}

func TestPlayerStateRejectsCorruptStoredProjection(t *testing.T) {
	for _, input := range []string{
		"", "null", "[]", `{"CanSeek":null}`, `{"CanSeek":"true"}`, `{"canseek":true}`,
		`{"CanSeek":true,"VolumeLevel":101}`, `{"PlaybackRate":0}`, `{"VolumeLevel":1.5}`,
		`{"AudioStreamIndex":2147483648}`, `{"SubtitleOffset":-2147483649}`,
		`{"PlayMethod":"unknown"}`, `{"PositionTicks":123}`, `{"IsPaused":true}`, `{"ItemId":"other"}`,
		`{"CanSeek":true} {}`, `{"PlayMethod":"` + strings.Repeat("x", 2048) + `"}`,
	} {
		state, err := decodePlayerState([]byte(input))
		if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(state, PlayerState{}) {
			t.Errorf("corrupt stored state produced a projection: %+v, error=%v", state, err)
		}
	}
}
