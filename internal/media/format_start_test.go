package media

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func formatStartDocument(value string) []byte {
	field := ""
	if value != "" {
		field = `,"start_time":` + value
	}
	return []byte(`{"format":{"format_name":"matroska,webm","duration":"2"` + field + `},"streams":[{"index":0,"codec_type":"video","codec_name":"h264"}]}`)
}

func TestParseProbeFormatStartPreservesExplicitZeroAndSignedExactNumbers(t *testing.T) {
	for _, fixture := range []struct {
		name, value string
		known       bool
		ticks       int64
	}{
		{"missing", "", false, 0},
		{"null", "null", false, 0},
		{"not_available", `"N/A"`, false, 0},
		{"empty", `""`, false, 0},
		{"zero_string", `"0.000000"`, true, 0},
		{"zero_number", `0`, true, 0},
		{"negative_zero", `-0`, true, 0},
		{"positive", `"1.23456789"`, true, 12_345_679},
		{"negative", `"-1.23456789"`, true, -12_345_679},
		{"numeric_precision", `900719925.4740993`, true, 9_007_199_254_740_993},
		{"scientific", `1e-7`, true, 1},
		{"positive_half_tick", `"0.00000005"`, true, 1},
		{"negative_half_tick", `"-0.00000005"`, true, -1},
		{"positive_limit", `"922337203685.4775807"`, true, math.MaxInt64},
		{"negative_limit", `"-922337203685.4775808"`, true, math.MinInt64},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			info, err := parseProbe(formatStartDocument(fixture.value))
			if err != nil || info.FormatStartKnown != fixture.known || info.FormatStartTicks != fixture.ticks {
				t.Fatalf("format start = %d (known %t), error %v; want %d (known %t)", info.FormatStartTicks, info.FormatStartKnown, err, fixture.ticks, fixture.known)
			}
			if info.DurationTicks != 20_000_000 || info.PresentationOriginTicks != 0 || info.AudioDurationExact {
				t.Fatalf("format start changed independent media facts: %+v", info)
			}
		})
	}
}

func TestParseProbeFormatStartRejectsMalformedAndOutOfRangeValues(t *testing.T) {
	for _, value := range []string{
		`"NaN"`, `"Infinity"`, `"not-a-timestamp"`, `"0/0"`,
		`"922337203685.4775808"`, `"-922337203685.4775809"`, `"1e1001"`,
		fmt.Sprintf("%q", strings.Repeat("1", 257)), fmt.Sprintf("%q", strings.Repeat(" ", 257)),
	} {
		t.Run(value, func(t *testing.T) {
			_, err := parseProbe(formatStartDocument(value))
			if err == nil || !strings.Contains(err.Error(), "invalid ffprobe format.start_time") {
				t.Fatalf("invalid format start returned %v", err)
			}
		})
	}
	for _, value := range []string{`true`, `[]`, `{}`} {
		if _, err := parseProbe(formatStartDocument(value)); err == nil {
			t.Errorf("non-numeric JSON format start %s was accepted", value)
		}
	}
}

func TestAudioTimingPreservesTheIndependentDemuxerStartFact(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprintf("known_%t", known), func(t *testing.T) {
			info := audioTimingTestInfo("wav", "pcm_s16le", 48000, "1/48000")
			info.FormatStartKnown = known
			if known {
				info.FormatStartTicks = -5_000_000
			}
			result := audioTimingTestParse(t, info, audioTimingTestPacket(0, 100, 48000, 48000), audioTimingTestFrame(0, 100, 48000, 48000))
			if result.FormatStartKnown != known || result.FormatStartTicks != info.FormatStartTicks ||
				result.PresentationOriginTicks != 10_000_000 || !result.AudioDurationExact {
				t.Fatalf("audio presentation replaced the independent format start: %+v", result)
			}
		})
	}
}

func TestParseProbeMissingFormatStartDoesNotUseTheStreamOrigin(t *testing.T) {
	info, err := parseProbe([]byte(`{"format":{"format_name":"matroska,webm","duration":"2"},"streams":[{"index":0,"codec_type":"video","codec_name":"h264","start_time":"1.250000"}]}`))
	if err != nil || info.FormatStartKnown || info.FormatStartTicks != 0 || info.DurationTicks != 20_000_000 {
		t.Fatalf("the stream origin was used to invent a format origin: %+v, %v", info, err)
	}
}
