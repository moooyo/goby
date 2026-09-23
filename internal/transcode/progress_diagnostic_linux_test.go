//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestProgressFailureKeepsFirstRejectionAndPreviousCompleteUpdate(t *testing.T) {
	cancellations := 0
	writer := &progressWriter{cancel: func() { cancellations++ }}
	if _, err := writer.Write([]byte("out_time_us=123\ntotal_size=456\nprogress=continue\nout_time_us=900\n")); err != nil {
		t.Fatal(err)
	}
	if writer.failure != nil {
		t.Fatal("accepted progress allocated a failure record")
	}
	const rejected = "total_size=-1"
	if _, err := writer.Write([]byte(rejected + "\nprogress=end\n")); err != ErrProgress {
		t.Fatalf("parser error identity changed: %v", err)
	}
	failure := writer.failure
	sum := sha256.Sum256([]byte(rejected))
	if failure == nil || failure.Phase != "Write" || failure.Reason != "negative_size" || failure.Field != "total_size" ||
		failure.SafeValue != "-1" || failure.LineBytes != len(rejected) || failure.CapturedBytes != len(rejected) ||
		failure.Truncated || failure.LineSHA256 != hex.EncodeToString(sum[:]) || failure.Previous == nil ||
		*failure.Previous != (ProgressSnapshot{OutputTicks: 1230, Bytes: 456}) {
		t.Fatalf("incorrect first rejection: %+v", failure)
	}
	if n, err := writer.Write([]byte("private-second-error")); n != 0 || err != ErrProgress {
		t.Fatalf("failed writer resumed: n=%d err=%v", n, err)
	}
	writer.finish()
	if writer.failure != failure || cancellations != 1 {
		t.Fatalf("first failure was replaced or recancelled: same=%t cancellations=%d", writer.failure == failure, cancellations)
	}
}

func TestProgressFailureClassifiesRejectedLinesWithoutRawText(t *testing.T) {
	for _, test := range []struct {
		name, line, reason, field, value string
		finish                           bool
	}{
		{"separator", "private/path-and-token", "missing_separator", "unknown", "", false},
		{"time syntax", "out_time_us=1e6", "invalid_time", "out_time_us", "1e6", false},
		{"time limit", "out_time_us=9223372036854775807", "time_limit", "out_time_us", "9223372036854775807", false},
		{"size syntax", "total_size=secret-token", "invalid_size", "total_size", "", false},
		{"state", "progress=N/A", "invalid_state", "progress", "N/A", false},
		{"partial final line", "progress=en", "invalid_state", "progress", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &progressWriter{}
			input := test.line
			if !test.finish {
				input += "\n"
			}
			_, err := writer.Write([]byte(input))
			if test.finish {
				if err != nil || writer.failure != nil {
					t.Fatal("incomplete line was rejected before finish")
				}
				writer.finish()
			}
			phase := "Write"
			if test.finish {
				phase = "finish"
			}
			failure := writer.failure
			if writer.err != ErrProgress || failure == nil || failure.Phase != phase || failure.Reason != test.reason ||
				failure.Field != test.field || failure.SafeValue != test.value || failure.Previous != nil ||
				failure.LineBytes != len(test.line) || failure.CapturedBytes != len(test.line) || failure.Truncated {
				t.Fatalf("incorrect rejection: err=%v failure=%+v", writer.err, failure)
			}
		})
	}
	writer := &progressWriter{}
	line := "out_time_us=" + strings.Repeat("9", maxProgressLine)
	_, err := writer.Write([]byte(line))
	failure := writer.failure
	sum := sha256.Sum256([]byte(line[:maxProgressLine]))
	if err != ErrProgress || failure == nil || failure.Reason != "line_too_long" || !failure.Truncated ||
		failure.LineBytes != maxProgressLine+1 || failure.CapturedBytes != maxProgressLine || failure.SafeValue != "" ||
		failure.LineSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("overflow did not retain a bounded prefix digest: err=%v failure=%+v", err, failure)
	}
}

func TestProgressFailureSafeValuesAndWaitClasses(t *testing.T) {
	for _, test := range []struct {
		line, value string
	}{
		{"out_time_us=-1.5e+3", "-1.5e+3"},
		{"progress=continue", "continue"},
		{"progress=end", "end"},
		{"out_time_us=N/A", "N/A"},
		{"out_time_us=123 ", ""},
		{"out_time_us=NaN", ""},
		{"out_time_us=1:02:03", ""},
		{"out_time_us=https://private.invalid/?api_key=secret", ""},
		{"out_time_us=" + strings.Repeat("1", 65), ""},
		{"unrecognized_private_field=123", ""},
	} {
		failure := newProgressFailure("finish", "invalid_time", []byte(test.line), len(test.line), nil)
		if failure.SafeValue != test.value {
			t.Fatalf("unsafe or missing diagnostic value: got=%q want=%q", failure.SafeValue, test.value)
		}
		if strings.HasPrefix(test.line, "unrecognized") && failure.Field != "unknown" {
			t.Fatal("unrecognized field name was retained")
		}
	}
	for _, test := range []struct {
		err   error
		class string
	}{
		{nil, "none"}, {exec.ErrWaitDelay, "wait_delay"}, {context.Canceled, "context_canceled"},
		{context.DeadlineExceeded, "context_deadline"}, {&exec.ExitError{}, "exit"}, {errors.New("private-error-text"), "other"},
	} {
		if class := progressWaitErrorClass(test.err); class != test.class {
			t.Fatalf("unexpected wait error classification: got=%s want=%s", class, test.class)
		}
	}
}
