package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

// Synthetic structure only: these samples are not independent decode evidence.
func generatedFMP4NativeClockFixture() ([]byte, []byte) {
	track := videoReadyH264Track(11)
	track.timescale = 12_288
	initialization := videoReadyInit(track)
	first := videoReadyFragment(videoReadyFragmentOptions{sequence: 1}, videoReadyRun{trackID: 11,
		samples: [][]byte{videoReadyIDR(), videoReadyIDR()}, decodeTime: 90 * 12_288, tfdtVersion: 1})
	second := videoReadyFragment(videoReadyFragmentOptions{sequence: 2}, videoReadyRun{trackID: 11,
		samples: [][]byte{videoReadyIDR()}, decodeTime: 90*12_288 + 2_000, tfdtVersion: 1})
	return initialization, append(first, second...)
}

func TestGeneratedFMP4NativeClockBindsActualTrackScaleEveryFragmentAndBytes(t *testing.T) {
	initialization, segment := generatedFMP4NativeClockFixture()
	clock, err := parseGeneratedFMP4NativeClock(context.Background(), bytes.NewReader(initialization), int64(len(initialization)), bytes.NewReader(segment), int64(len(segment)))
	if err != nil || clock.TrackID != 11 || clock.MediaTimeScale != 12_288 || clock.FragmentCount != 2 || clock.TotalSamples != 3 ||
		clock.HeaderDurationKnown || clock.HeaderDurationUnits != 0 || clock.EditPresent ||
		clock.InitializationSHA256 != sha256.Sum256(initialization) || clock.SegmentSHA256 != sha256.Sum256(segment) {
		t.Fatalf("native metadata association was lost: %+v, %v", clock, err)
	}
	if clock.Fragments[0] != (GeneratedFMP4FragmentClock{TrackID: 11, SequenceNumber: 1, DecodeUnits: 90 * 12_288, SampleCount: 2}) ||
		clock.Fragments[1] != (GeneratedFMP4FragmentClock{TrackID: 11, SequenceNumber: 2, DecodeUnits: 90*12_288 + 2_000, SampleCount: 1}) {
		t.Fatal("raw tfdt units were normalized, dropped or borrowed from another track")
	}
}

func TestGeneratedFMP4NativeClockRetainsEditsAndUnknownHeaderDuration(t *testing.T) {
	track := videoReadyH264Track(11)
	track.tkhdVersion, track.mdhdVersion, track.elstVersion = 1, 1, 1
	track.edits = []videoReadyEdit{{duration: 6_000, mediaTime: 0}}
	initialization := videoReadyInit(track)
	position := bytes.Index(initialization, []byte("mdhd"))
	if position < 0 {
		t.Fatal("synthetic clock header missing")
	}
	binary.BigEndian.PutUint64(initialization[position+28:position+36], ^uint64(0))
	segment := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}, decodeTime: 3_000})
	clock, err := parseGeneratedFMP4NativeClock(context.Background(), bytes.NewReader(initialization), int64(len(initialization)), bytes.NewReader(segment), int64(len(segment)))
	if err != nil || !clock.EditPresent || clock.HeaderDurationKnown || clock.HeaderDurationUnits != ^uint64(0) || clock.Fragments[0].DecodeUnits != 3_000 {
		t.Fatalf("movie edits or unknown duration became a synthetic native presentation: %+v, %v", clock, err)
	}
}

func TestGeneratedFMP4NativeClockHeaderDurationRemainsADeclaredValue(t *testing.T) {
	for _, version := range []byte{0, 1} {
		unknown := uint64(math.MaxUint32)
		positive := uint64(24_000)
		if version == 1 {
			unknown, positive = math.MaxUint64, uint64(math.MaxUint32)+24_000
		}
		for _, duration := range []uint64{0, unknown, positive} {
			track := videoReadyH264Track(11)
			track.mdhdVersion = version
			initialization := videoReadyInit(track)
			position := bytes.Index(initialization, []byte("mdhd")) + 20 + int(version)*8
			if version == 0 {
				binary.BigEndian.PutUint32(initialization[position:position+4], uint32(duration))
			} else {
				binary.BigEndian.PutUint64(initialization[position:position+8], duration)
			}
			segment := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}, decodeTime: 0})
			clock, err := parseGeneratedFMP4NativeClock(context.Background(), bytes.NewReader(initialization), int64(len(initialization)), bytes.NewReader(segment), int64(len(segment)))
			if err != nil || clock.HeaderDurationUnits != duration || clock.HeaderDurationKnown != (duration == positive) {
				t.Fatalf("version %d declared duration %d was synthesized or truncated: raw=%d known=%t error=%v", version, duration, clock.HeaderDurationUnits, clock.HeaderDurationKnown, err)
			}
		}
	}
}

func TestGeneratedFMP4NativeClockRejectsMissingForeignOrChangingNativeClocks(t *testing.T) {
	initialization, segment := generatedFMP4NativeClockFixture()
	for name, mutate := range map[string]func([]byte, []byte) ([]byte, []byte){
		"zero track scale": func(init, media []byte) ([]byte, []byte) {
			position := bytes.Index(init, []byte("mdhd"))
			binary.BigEndian.PutUint32(init[position+16:position+20], 0)
			return init, media
		},
		"foreign fragment track": func(init, media []byte) ([]byte, []byte) {
			position := bytes.Index(media, []byte("tfhd"))
			binary.BigEndian.PutUint32(media[position+8:position+12], 12)
			return init, media
		},
		"missing tfdt": func(init, media []byte) ([]byte, []byte) {
			position := bytes.Index(media, []byte("tfdt"))
			copy(media[position:position+4], "free")
			return init, media
		},
		"unknown tfdt flags": func(init, media []byte) ([]byte, []byte) {
			position := bytes.Index(media, []byte("tfdt"))
			media[position+7] = 1
			return init, media
		},
		"unknown tfdt version": func(init, media []byte) ([]byte, []byte) {
			position := bytes.Index(media, []byte("tfdt"))
			media[position+4] = 2
			return init, media
		},
		"later fragment repeats sequence": func(init, media []byte) ([]byte, []byte) {
			position := bytes.LastIndex(media, []byte("mfhd"))
			binary.BigEndian.PutUint32(media[position+8:position+12], 1)
			return init, media
		},
		"later fragment reverses clock": func(init, media []byte) ([]byte, []byte) {
			position := bytes.LastIndex(media, []byte("tfdt"))
			binary.BigEndian.PutUint64(media[position+8:position+16], 0)
			return init, media
		},
		"second initialized video": func(_ []byte, media []byte) ([]byte, []byte) {
			return videoReadyInit(videoReadyH264Track(11), videoReadyH264Track(12)), media
		},
		"initialized audio sibling": func(_ []byte, media []byte) ([]byte, []byte) {
			return videoReadyInit(videoReadyH264Track(11), videoReadyAACTrack(12)), media
		},
		"implicit top level extent": func(init, media []byte) ([]byte, []byte) {
			binary.BigEndian.PutUint32(media[:4], 0)
			return init, media
		},
	} {
		t.Run(name, func(t *testing.T) {
			init, media := mutate(bytes.Clone(initialization), bytes.Clone(segment))
			clock, err := parseGeneratedFMP4NativeClock(context.Background(), bytes.NewReader(init), int64(len(init)), bytes.NewReader(media), int64(len(media)))
			if err == nil || clock != (GeneratedFMP4NativeClock{}) {
				t.Fatal("incomplete or foreign metadata returned native clock facts")
			}
		})
	}
}

func TestGeneratedFMP4NativeClockRejectsBudgetAndCancelledOrShortReaders(t *testing.T) {
	initialization, segment := generatedFMP4NativeClockFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if clock, err := parseGeneratedFMP4NativeClock(ctx, bytes.NewReader(initialization), int64(len(initialization)), bytes.NewReader(segment), int64(len(segment))); !errors.Is(err, context.Canceled) || clock != (GeneratedFMP4NativeClock{}) {
		t.Fatal("cancellation did not preserve zero native evidence")
	}
	if clock, err := parseGeneratedFMP4NativeClock(context.Background(), bytes.NewReader(initialization), maxGeneratedBoundsInputBytes, bytes.NewReader(segment), 1); !errors.Is(err, ErrTimelineLimit) || clock != (GeneratedFMP4NativeClock{}) {
		t.Fatal("aggregate native clock byte budget was ignored")
	}
	short := bytes.NewReader(segment[:len(segment)-1])
	if clock, err := parseGeneratedFMP4NativeClock(context.Background(), bytes.NewReader(initialization), int64(len(initialization)), short, int64(len(segment))); err == nil || clock != (GeneratedFMP4NativeClock{}) {
		t.Fatal("a short full-byte hash reader returned partial clock evidence")
	}
}
