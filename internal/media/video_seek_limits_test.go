package media

import (
	"strconv"
	"testing"
)

func TestVideoSeekResourceLimitsRejectUnsupportedRepresentation(t *testing.T) {
	for _, test := range []struct {
		width, height int
		format        string
		bytes         int64
	}{
		{4096, 2160, "yuv420p", 13271040},
		{4096, 2160, "yuv420p10le", 26542080},
		{65, 65, "yuv420p", 6403},
	} {
		got, supported := videoSeekFrameBytes(test.width, test.height, test.format)
		if !supported || got != test.bytes {
			t.Fatalf("supported representation returned %d, %t; want %d", got, supported, test.bytes)
		}
	}
	for _, test := range []struct {
		width, height int
		format        string
	}{
		{4097, 2160, "yuv420p"}, {32768, 32768, "yuv420p"},
		{64, 64, "yuv444p"}, {64, 64, "yuv420p12le"},
		{0, 64, "yuv420p"}, {64, -1, "yuv420p"},
	} {
		if _, supported := videoSeekFrameBytes(test.width, test.height, test.format); supported {
			t.Fatal("unsupported or excessive decoded representation was accepted")
		}
	}
	index := videoSeekCandidateTestIndex(10_000_000)
	index.DurationTicks = MaxVideoSeekDurationTicks
	if err := ValidateVideoSeekIndex(index); err != nil {
		t.Fatalf("duration boundary was rejected: %v", err)
	}
	index.DurationTicks++
	if err := ValidateVideoSeekIndex(index); err == nil {
		t.Fatal("excessive source duration was accepted")
	}
	index = videoSeekCandidateTestIndex(10_000_000)
	index.DecodedFrameBytes++
	if err := ValidateVideoSeekIndex(index); err == nil {
		t.Fatal("inconsistent decoded frame size was accepted")
	}
}

func TestVideoSeekProofSharesTheActualDecoderThreadLimit(t *testing.T) {
	candidate := "2.0000000"
	for _, threads := range []int{1, 2, MaxVideoSeekDecoderThreads} {
		args, err := BuildVideoSeekCommandArgs(0, &candidate, threads)
		if err != nil || videoSeekHashTestArgValue(t, args, "-threads") != strconv.Itoa(threads) {
			t.Fatalf("proof did not preserve decoder thread limit %d: %v", threads, err)
		}
	}
	for _, threads := range []int{0, -1, MaxVideoSeekDecoderThreads + 1} {
		if _, err := BuildVideoSeekCommandArgs(0, &candidate, threads); err == nil {
			t.Fatal("unbounded decoder thread limit was accepted")
		}
	}
	if _, err := BuildVideoSeekCommandArgs(0, nil, 2); err == nil {
		t.Fatal("library analysis must retain its single decoder thread")
	}
}
