package transcode

import (
	"errors"
	"testing"
)

func TestX264OptionsCanonicalNativeFactoryAndBoundaries(t *testing.T) {
	for _, pair := range [][2]string{
		{"", ""},
		{"subme=0:me_range=4:rc_lookahead=10:partitions=none", "me-range=4:partitions=none:rc-lookahead=10:subme=0"},
		{"subme=9:merange=16:rc-lookahead=250:partitions=all:aq-mode=3:aq-strength=3.0:deblock=-6,6", "aq-mode=3:aq-strength=3:deblock=-6,6:me-range=16:partitions=all:rc-lookahead=250:subme=9"},
	} {
		got, err := CanonicalX264Options(pair[0])
		if err != nil || got != pair[1] {
			t.Fatalf("valid options were changed or rejected: %q -> %q: %v", pair[0], got, err)
		}
	}
}

func TestX264OptionsRejectFilesPlanOverridesAndMalformedValues(t *testing.T) {
	for _, value := range []string{
		"stats=/tmp/secret", "cqmfile=/tmp/secret", "dump-yuv=/tmp/secret", "pass=2", "opencl-clbin=/tmp/cache",
		"threads=0", "keyint=1", "open-gop=1", "bframes=3", "crf=20", "vbv-maxrate=200", "weightp=1", "cabac=1", "8x8dct=1",
		"unknown=1", "subme=0:subme=1", "merange=4:me_range=5", "subme=10", "me-range=17", "rc-lookahead=251",
		"partitions=i4x4", "aq-mode=4", "aq-strength=NaN", "aq-strength=Inf", "aq-strength=-.1", "aq-strength=3.1", "deblock=7,0", "deblock=0",
		"subme", "subme=", "=0", "subme=0:", "subme=0=1", "subme='0'", "subme=0 -f null", "subme=0\n", "subme=0\\:stats=/tmp/file",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := CanonicalX264Options(value); !errors.Is(err, ErrInvalidExecution) {
				t.Fatalf("unsafe or malformed options accepted: %q: %v", value, err)
			}
		})
	}
}
