package diagnostics

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
)

func TestTaskRetryDiagnosticsRetainSafeFailureIdentity(t *testing.T) {
	for _, test := range []struct{ message, event string }{
		{"Task scheduling will retry; scheduler readiness is unavailable", "task.scheduling.retry"},
		{"Task coordination will retry", "task.coordination.retry"},
		{"created library initial scan unavailable", "library.initial_scan.failed"},
	} {
		t.Run(test.event, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(NewHandler(nil, slog.NewJSONHandler(&output, nil)))
			logger.Warn(test.message, "error", &fs.PathError{Op: "open", Path: "private-secret-path", Err: context.DeadlineExceeded},
				"token", "private-secret-token")
			record := diagnosticJSON(t, output.Bytes())
			if record["event"] != test.event || record["msg"] != test.message || record["level"] != "WARN" || record["error_class"] != "deadline_exceeded" {
				t.Fatalf("retry diagnosis lost its fixed identity or safe error class: %#v", record)
			}
			if strings.Contains(output.String(), "private-secret") || record["error"] != nil || record["token"] != nil {
				t.Fatal("retry diagnosis exposed an error payload or arbitrary attribute")
			}
		})
	}
}
