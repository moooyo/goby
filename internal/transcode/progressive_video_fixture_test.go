package transcode

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"
)

func progressiveVideoRealFixture(t *testing.T) ([]byte, int) {
	t.Helper()
	data, err := os.ReadFile("testdata/progressive-video-first-fragment.mp4")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if len(data) != 26392 || hex.EncodeToString(digest[:]) != "59dead0c726e9b005234a9bc45363a43f4435e2b040520a574443b2af8d6a6ec" {
		t.Fatal("the generated FFmpeg video fixture changed")
	}
	for offset := 0; offset+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if size < 8 || size > len(data)-offset {
			t.Fatal("the recorded fixture has an incomplete top-level box")
		}
		if string(data[offset+4:offset+8]) == "mdat" {
			if offset+size != len(data) {
				t.Fatal("the recorded fixture must end after its first media box")
			}
			return data, offset + 8
		}
		offset += size
	}
	t.Fatal("the recorded fixture has no first media box")
	return nil, 0
}

func progressiveVideoRealPlan() Plan {
	return Plan{OutputMode: "progressive", Container: "mp4", VideoCodec: "copy", AudioCodec: "copy",
		VideoStreamIndex: 7, AudioStreamIndex: 9}
}

func TestProgressiveVideoReadyRealFFmpegFirstFragment(t *testing.T) {
	data, mediaStart := progressiveVideoRealFixture(t)
	plan := progressiveVideoRealPlan()
	if ready, err := ProgressiveMediaReady(plan, data[:mediaStart]); err != nil || ready {
		t.Fatalf("initialization and a media header became playable: %v, %v", ready, err)
	}
	if ready, err := ProgressiveMediaReady(plan, data); err != nil || !ready {
		t.Fatalf("the decoded FFmpeg first fragment was rejected: %v, %v", ready, err)
	}
	low, high := mediaStart, len(data)
	for low+1 < high {
		middle := low + (high-low)/2
		ready, err := ProgressiveVideoReady(plan, data[:middle])
		if err != nil {
			t.Fatalf("a valid growing FFmpeg prefix was rejected: %v", err)
		}
		if ready {
			high = middle
		} else {
			low = middle
		}
	}
	if high <= mediaStart || high >= len(data) {
		t.Fatal("startup did not wait for video or unnecessarily waited for the whole multiplexed fragment")
	}
	if ready, err := ProgressiveMediaReady(plan, data[:high-1]); err != nil || ready {
		t.Fatalf("an incomplete first usable video sample was advertised: %v, %v", ready, err)
	}
	if ready, err := ProgressiveMediaReady(plan, data[:high]); err != nil || !ready {
		t.Fatalf("a complete first usable video sample was not advertised: %v, %v", ready, err)
	}
	plan.AudioStreamIndex, plan.AudioCodec = -1, ""
	if ready, err := ProgressiveVideoReady(plan, data); err == nil || ready {
		t.Fatal("a real unselected AAC track bypassed the selected-track contract")
	}
}
