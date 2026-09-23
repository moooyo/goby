package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

const diagnosticTestID = "0123456789abcdef0123456789abcdef"

func diagnosticTestRecord(message string, attrs ...slog.Attr) slog.Record {
	record := slog.NewRecord(time.Date(2026, time.September, 10, 8, 0, 0, 0, time.UTC), slog.LevelInfo, message, 0)
	record.AddAttrs(attrs...)
	return record
}

func diagnosticJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	if len(data) > MaxRecordBytes || len(data) == 0 || data[len(data)-1] != '\n' || bytes.Count(data, []byte{'\n'}) != 1 {
		t.Fatal("diagnostic output is not one bounded JSONL record")
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode diagnostic record: %v", err)
	}
	return record
}

func TestHandlerUnknownEventDropsMessageAndAllAttributes(t *testing.T) {
	var output bytes.Buffer
	handler := NewHandler(nil, slog.NewJSONHandler(&output, nil)).WithAttrs([]slog.Attr{
		slog.String("request_id", diagnosticTestID),
	})
	record := diagnosticTestRecord("Authorization: Bearer test-secret\nsecond forged entry",
		slog.String("event", "server.listening"), slog.String("version", "1.2.3"),
		slog.Any("error", errors.New("database password=test-secret")))
	record.Level = slog.LevelError
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	actual := diagnosticJSON(t, output.Bytes())
	want := map[string]any{"time": "2026-09-10T08:00:00Z", "level": "ERROR", "msg": "unclassified event", "event": "unclassified"}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("unknown record retained unexpected data: %#v", actual)
	}
}

type diagnosticPanicValue struct{ calls *int }

func (value diagnosticPanicValue) LogValue() slog.Value {
	*value.calls++
	panic("secret LogValue")
}

type diagnosticPanicStringer struct{ calls *int }

func (value diagnosticPanicStringer) String() string {
	*value.calls++
	panic("secret String")
}

type diagnosticPanicMarshaler struct{ calls *int }

func (value diagnosticPanicMarshaler) MarshalJSON() ([]byte, error) {
	*value.calls++
	panic("secret MarshalJSON")
}

type diagnosticPanicError struct{ calls *int }

func (value diagnosticPanicError) Error() string {
	*value.calls++
	panic("secret Error")
}

func (value diagnosticPanicError) Unwrap() error {
	*value.calls++
	panic("secret Unwrap")
}

func (value diagnosticPanicError) Is(error) bool {
	*value.calls++
	panic("secret Is")
}

func (value diagnosticPanicError) As(any) bool {
	*value.calls++
	panic("secret As")
}

type diagnosticSliceError []string

func (diagnosticSliceError) Error() string { panic("uncomparable error formatted") }

func TestHandlerNeverEvaluatesArbitraryValues(t *testing.T) {
	calls := 0
	var output bytes.Buffer
	handler := NewHandler(nil, slog.NewJSONHandler(&output, nil)).WithAttrs([]slog.Attr{
		slog.Any("request_id", diagnosticPanicValue{&calls}),
		slog.Any("item_id", diagnosticPanicStringer{&calls}),
		slog.Any("request_id", diagnosticPanicMarshaler{&calls}),
		slog.Any("error", diagnosticPanicError{&calls}),
		slog.Any("job_id", map[string]any{"token": "test-secret"}),
	})
	if calls != 0 {
		t.Fatal("WithAttrs executed an arbitrary method")
	}
	record := diagnosticTestRecord("transcode failed",
		slog.Any("request_id", diagnosticPanicValue{&calls}),
		slog.Any("item_id", diagnosticPanicStringer{&calls}),
		slog.Any("error", diagnosticSliceError{"test-secret"}),
		slog.Any("stderr", diagnosticPanicStringer{&calls}),
		slog.String("job_id", diagnosticTestID))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	actual := diagnosticJSON(t, output.Bytes())
	if calls != 0 || bytes.Contains(output.Bytes(), []byte("test-secret")) || actual["error_class"] != "unclassified" || actual["job_id"] != diagnosticTestID {
		t.Fatal("handler evaluated or retained an arbitrary value")
	}
	for _, key := range []string{"request_id", "item_id", "error", "stderr"} {
		if _, exists := actual[key]; exists {
			t.Errorf("unsafe attribute %q survived", key)
		}
	}
}

func TestHandlerPreservesGroupBindingOrderAndCopiesAttributes(t *testing.T) {
	var output bytes.Buffer
	bound := []slog.Attr{slog.String("request_id", diagnosticTestID)}
	handler := NewHandler(nil, slog.NewJSONHandler(&output, nil)).WithAttrs(bound)
	bound[0] = slog.String("request_id", "test-secret")
	handler = handler.WithGroup("request").WithAttrs([]slog.Attr{slog.String("method", "GET")})
	handler = handler.WithGroup("http").WithAttrs([]slog.Attr{slog.String("route", "GET /admin/v1/users/{id}")})
	if err := handler.Handle(context.Background(), diagnosticTestRecord("request.completed", slog.Int("status", 204))); err != nil {
		t.Fatal(err)
	}
	actual := diagnosticJSON(t, output.Bytes())
	wantRequest := map[string]any{"method": "GET", "http": map[string]any{"route": "GET /admin/v1/users/{id}", "status": float64(204)}}
	if actual["request_id"] != diagnosticTestID || !reflect.DeepEqual(actual["request"], wantRequest) {
		t.Fatalf("WithAttrs and WithGroup ordering changed: %#v", actual)
	}
	if bytes.Count(output.Bytes(), []byte(`"request":`)) != 1 {
		t.Fatal("inherited groups produced duplicate JSON group keys")
	}
}

func TestHandlerFreezesNestedGroupsAndDropsUnsafeGroupNames(t *testing.T) {
	var output bytes.Buffer
	children := []slog.Attr{slog.String("method", "POST")}
	handler := NewHandler(nil, slog.NewJSONHandler(&output, nil)).WithAttrs([]slog.Attr{
		slog.GroupAttrs("http", children...),
		slog.Group("Authorization:test-secret", slog.String("method", "DELETE")),
	})
	children[0] = slog.String("method", "test-secret")
	if err := handler.Handle(context.Background(), diagnosticTestRecord("request completed", slog.Group("", slog.Int("status", 201)))); err != nil {
		t.Fatal(err)
	}
	actual := diagnosticJSON(t, output.Bytes())
	if !reflect.DeepEqual(actual["http"], map[string]any{"method": "POST"}) || actual["status"] != float64(201) || bytes.Contains(output.Bytes(), []byte("test-secret")) {
		t.Fatalf("nested attributes were not safely frozen: %#v", actual)
	}
	output.Reset()
	handler = NewHandler(nil, slog.NewJSONHandler(&output, nil)).WithAttrs([]slog.Attr{slog.String("request_id", diagnosticTestID)}).WithGroup("test-secret")
	if err := handler.Handle(context.Background(), diagnosticTestRecord("request completed", slog.Int("status", 200))); err != nil {
		t.Fatal(err)
	}
	actual = diagnosticJSON(t, output.Bytes())
	if actual["request_id"] != diagnosticTestID {
		t.Fatal("an unsafe inner group removed an earlier safe binding")
	}
	if _, exists := actual["status"]; exists || bytes.Contains(output.Bytes(), []byte("test-secret")) {
		t.Fatal("unsafe group contents escaped into the outer record")
	}
}

func TestHandlerRequestAttributeTypesAndRoutes(t *testing.T) {
	tests := []struct {
		name  string
		attr  slog.Attr
		key   string
		value any
	}{
		{"method", slog.String("method", "GET"), "method", "GET"},
		{"method secret", slog.String("method", "test-secret"), "method", nil},
		{"completed outcome", slog.String("outcome", "completed"), "outcome", "completed"},
		{"cancelled outcome", slog.String("outcome", "cancelled"), "outcome", "cancelled"},
		{"aborted outcome", slog.String("outcome", "aborted"), "outcome", "aborted"},
		{"outcome secret", slog.String("outcome", "test-secret"), "outcome", nil},
		{"outcome case", slog.String("outcome", "Completed"), "outcome", nil},
		{"integer outcome", slog.Int("outcome", 200), "outcome", nil},
		{"boolean outcome", slog.Bool("outcome", true), "outcome", nil},
		{"registered pattern", slog.String("route", "GET /admin/v1/users/{id}"), "route", "GET /admin/v1/users/{id}"},
		{"concrete path", slog.String("route", "GET /admin/v1/users/test-secret"), "route", "unmatched"},
		{"concrete log path", slog.String("route", "GET /admin/v1/logs/test-secret/lines"), "route", "unmatched"},
		{"query", slog.String("route", "/healthz?token=test-secret"), "route", "unmatched"},
		{"unregistered pattern", slog.String("route", "GET /private/{token}"), "route", "unmatched"},
		{"websocket", slog.String("route", "websocket"), "route", "websocket"},
		{"status", slog.Int("status", 599), "status", float64(599)},
		{"low status", slog.Int("status", 99), "status", nil},
		{"high status", slog.Int("status", 600), "status", nil},
		{"string status", slog.String("status", "200"), "status", nil},
		{"float status", slog.Float64("status", 200), "status", nil},
		{"duration", slog.Int64("duration_ms", 42), "duration_ms", float64(42)},
		{"negative duration", slog.Int64("duration_ms", -1), "duration_ms", nil},
		{"duration overflow", slog.Uint64("duration_ms", math.MaxUint64), "duration_ms", nil},
		{"typed duration", slog.Duration("duration_ms", time.Second), "duration_ms", nil},
		{"bytes", slog.Uint64("bytes", 25), "bytes", float64(25)},
		{"negative bytes", slog.Int64("bytes", -1), "bytes", nil},
		{"event mismatch", slog.String("version", "1.2.3"), "version", nil},
		{"headers", slog.Any("headers", map[string]string{"Authorization": "test-secret"}), "headers", nil},
		{"source", slog.String("source", "D:\\private\\test-secret"), "source", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(nil, slog.NewJSONHandler(&output, nil))
			if err := handler.Handle(context.Background(), diagnosticTestRecord("request completed", test.attr)); err != nil {
				t.Fatal(err)
			}
			actual := diagnosticJSON(t, output.Bytes())
			if !reflect.DeepEqual(actual[test.key], test.value) || bytes.Contains(output.Bytes(), []byte("test-secret")) {
				t.Fatalf("attribute was not classified as expected: %#v", actual)
			}
		})
	}
}

func TestHandlerPreservesActivityAndDiagnosticRoutePatterns(t *testing.T) {
	for _, route := range []string{
		"GET /admin/v1/activity",
		"GET /admin/v1/logs",
		"GET /admin/v1/logs/{name}/lines",
		"GET /admin/v1/logs/{name}/download",
		"GET /emby/System/ActivityLog/Entries",
		"GET /emby/System/Logs/Query",
		"GET /emby/System/Logs/{name}",
		"GET /emby/System/Logs/{name}/Lines",
	} {
		t.Run(route, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(nil, slog.NewJSONHandler(&output, nil))
			if err := handler.Handle(context.Background(), diagnosticTestRecord("request completed", slog.String("route", route))); err != nil {
				t.Fatal(err)
			}
			actual := diagnosticJSON(t, output.Bytes())
			if actual["route"] != route {
				t.Fatalf("registered route pattern was not preserved: %#v", actual)
			}
		})
	}
}

func TestHandlerClassifiesActivityRetentionRetryWithoutErrorText(t *testing.T) {
	tests := []struct {
		name  string
		attr  slog.Attr
		class any
	}{
		{"known error", slog.Any("error", &fs.PathError{Op: "test-secret", Path: "/test-secret", Err: fs.ErrPermission}), "permission_denied"},
		{"unknown error", slog.Any("error", errors.New("test-secret")), "unclassified"},
		{"raw error text", slog.String("error", "test-secret"), nil},
		{"known class", slog.String("error_class", "deadline_exceeded"), "deadline_exceeded"},
		{"unknown class", slog.String("error_class", "test-secret"), nil},
		{"unrelated outcome", slog.String("outcome", "completed"), nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(nil, slog.NewJSONHandler(&output, nil))
			if err := handler.Handle(context.Background(), diagnosticTestRecord("activity retention will retry", test.attr)); err != nil {
				t.Fatal(err)
			}
			actual := diagnosticJSON(t, output.Bytes())
			if actual["event"] != "activity.retention.retry" || actual["msg"] != "activity retention will retry" || actual["error_class"] != test.class {
				t.Fatalf("activity retention event was not safely classified: %#v", actual)
			}
			if _, exists := actual["error"]; exists || bytes.Contains(output.Bytes(), []byte("test-secret")) {
				t.Fatal("activity retention event retained raw error data")
			}
			if _, exists := actual["outcome"]; exists {
				t.Fatal("activity retention event retained an unrelated request outcome")
			}
		})
	}
}

func TestHandlerPreservesTranscodeProgressRejection(t *testing.T) {
	var output bytes.Buffer
	handler := NewHandler(nil, slog.NewJSONHandler(&output, nil))
	record := diagnosticTestRecord("transcode progress rejected",
		slog.String("job_id", diagnosticTestID), slog.String("error_class", "process_progress"),
		slog.String("phase", "Write"), slog.String("reason", "invalid_time"), slog.String("field", "out_time_us"),
		slog.Int("line_bytes", 14), slog.Int("captured_bytes", 14), slog.Bool("truncated", false),
		slog.String("line_sha256", strings.Repeat("a", 64)), slog.String("safe_value", "-1"),
		slog.Bool("previous_known", true), slog.Int64("output_ticks", 120), slog.Int64("bytes", 40), slog.Bool("ended", false),
		slog.Bool("wait_delay", true), slog.String("wait_error_class", "wait_delay"), slog.Int("exit_code", -1))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"time": "2026-09-10T08:00:00Z", "level": "INFO", "msg": "transcode progress rejected", "event": "transcode.progress.invalid",
		"job_id": diagnosticTestID, "error_class": "process_progress", "phase": "Write", "reason": "invalid_time", "field": "out_time_us",
		"line_bytes": float64(14), "captured_bytes": float64(14), "truncated": false, "line_sha256": strings.Repeat("a", 64), "safe_value": "-1",
		"previous_known": true, "output_ticks": float64(120), "bytes": float64(40), "ended": false,
		"wait_delay": true, "wait_error_class": "wait_delay", "exit_code": float64(-1),
	}
	if actual := diagnosticJSON(t, output.Bytes()); !reflect.DeepEqual(actual, want) {
		t.Fatalf("progress diagnostic lost its bounded fields: %#v", actual)
	}
}

func TestHandlerTranscodeProgressAttributeBoundaries(t *testing.T) {
	type attributeCase struct {
		name  string
		attr  slog.Attr
		value any
	}
	tests := []attributeCase{
		{"finish phase", slog.String("phase", "finish"), "finish"},
		{"phase case", slog.String("phase", "write"), nil},
		{"phase text", slog.String("phase", "test-secret"), nil},
		{"known reason", slog.String("reason", "missing_separator"), "missing_separator"},
		{"reason text", slog.String("reason", "test-secret"), nil},
		{"known field", slog.String("field", "stream_0_0_q"), "stream_0_0_q"},
		{"unknown field", slog.String("field", "unknown"), "unknown"},
		{"field text", slog.String("field", "test-secret"), nil},
		{"zero line bytes", slog.Int("line_bytes", 0), float64(0)},
		{"overflow lower bound", slog.Int("line_bytes", 4097), float64(4097)},
		{"line bytes excessive", slog.Int("line_bytes", 4098), nil},
		{"negative line bytes", slog.Int("line_bytes", -1), nil},
		{"line bytes wrong type", slog.String("line_bytes", "12"), nil},
		{"capture limit", slog.Int("captured_bytes", 4096), float64(4096)},
		{"capture excessive", slog.Int("captured_bytes", 4097), nil},
		{"negative capture", slog.Int("captured_bytes", -1), nil},
		{"capture wrong type", slog.Float64("captured_bytes", 12), nil},
		{"lowercase hash", slog.String("line_sha256", strings.Repeat("b", 64)), strings.Repeat("b", 64)},
		{"uppercase hash", slog.String("line_sha256", strings.Repeat("B", 64)), nil},
		{"short hash", slog.String("line_sha256", strings.Repeat("b", 63)), nil},
		{"hash text", slog.String("line_sha256", "test-secret"), nil},
		{"boolean truncation", slog.Bool("truncated", true), true},
		{"truncation wrong type", slog.String("truncated", "false"), nil},
		{"previous wrong type", slog.String("previous_known", "true"), nil},
		{"nonnegative ticks", slog.Int64("output_ticks", 0), float64(0)},
		{"negative ticks", slog.Int64("output_ticks", -1), nil},
		{"overflow ticks", slog.Uint64("output_ticks", math.MaxUint64), nil},
		{"negative bytes", slog.Int64("bytes", -1), nil},
		{"ended wrong type", slog.String("ended", "true"), nil},
		{"wait flag", slog.Bool("wait_delay", false), false},
		{"wait flag wrong type", slog.Int("wait_delay", 1), nil},
		{"wait class", slog.String("wait_error_class", "context_deadline"), "context_deadline"},
		{"wait class text", slog.String("wait_error_class", "test-secret"), nil},
		{"runner class", slog.String("error_class", "process_failed"), "process_failed"},
		{"unrelated error class", slog.String("error_class", "database_lease_busy"), nil},
		{"error class text", slog.String("error_class", "test-secret"), nil},
		{"exit limit", slog.Int("exit_code", 255), float64(255)},
		{"exit excessive", slog.Int("exit_code", 256), nil},
		{"raw progress", slog.String("line", "test-secret"), nil},
		{"raw stderr", slog.String("stderr", "test-secret"), nil},
	}
	for _, value := range []string{"continue", "end", "N/A", "+1", "-.5", "1.", "1e-5", "1E+5", strings.Repeat("1", 64)} {
		tests = append(tests, attributeCase{"safe value " + value, slog.String("safe_value", value), value})
	}
	for _, value := range []string{"", "+", ".", "1e", "NaN", "Inf", "-Inf", "1x", "00:00:01", "1 kbits/s", " 1", "1\n", "test-secret", strings.Repeat("1", 65)} {
		tests = append(tests, attributeCase{"unsafe value " + value, slog.String("safe_value", value), nil})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(nil, slog.NewJSONHandler(&output, nil))
			attrs := []slog.Attr{test.attr}
			if test.attr.Key == "safe_value" {
				attrs = append(attrs, slog.String("field", "progress"), slog.Bool("truncated", false))
			}
			if test.attr.Key == "output_ticks" || test.attr.Key == "bytes" || test.attr.Key == "ended" {
				attrs = append(attrs, slog.Bool("previous_known", true))
			}
			if err := handler.Handle(context.Background(), diagnosticTestRecord("transcode progress rejected", attrs...)); err != nil {
				t.Fatal(err)
			}
			actual := diagnosticJSON(t, output.Bytes())
			if !reflect.DeepEqual(actual[test.attr.Key], test.value) || bytes.Contains(output.Bytes(), []byte("test-secret")) {
				t.Fatalf("progress diagnostic retained or dropped an unexpected value: %#v", actual)
			}
		})
	}
}

func TestHandlerTranscodeProgressRequiresValueAndSnapshotContext(t *testing.T) {
	tests := []struct {
		name         string
		attrs        []slog.Attr
		valueAllowed bool
		priorAllowed bool
	}{
		{"known contexts", []slog.Attr{slog.String("field", "progress"), slog.Bool("truncated", false), slog.Bool("previous_known", true)}, true, true},
		{"unknown field", []slog.Attr{slog.String("field", "unknown"), slog.Bool("truncated", false), slog.Bool("previous_known", true)}, false, true},
		{"truncated line", []slog.Attr{slog.String("field", "progress"), slog.Bool("truncated", true), slog.Bool("previous_known", true)}, false, true},
		{"missing contexts", nil, false, false},
		{"missing truncation", []slog.Attr{slog.String("field", "progress")}, false, false},
		{"unknown previous", []slog.Attr{slog.String("field", "progress"), slog.Bool("truncated", false), slog.Bool("previous_known", false)}, true, false},
		{"duplicate field", []slog.Attr{slog.String("field", "unknown"), slog.String("field", "progress"), slog.Bool("truncated", false)}, false, false},
		{"duplicate truncation", []slog.Attr{slog.String("field", "progress"), slog.Bool("truncated", true), slog.Bool("truncated", false)}, false, false},
		{"duplicate previous", []slog.Attr{slog.Bool("previous_known", false), slog.Bool("previous_known", true)}, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(nil, slog.NewJSONHandler(&output, nil)).WithAttrs(test.attrs)
			record := diagnosticTestRecord("transcode progress rejected", slog.String("safe_value", "end"),
				slog.Int64("output_ticks", 0), slog.Int64("bytes", 0), slog.Bool("ended", false))
			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			actual := diagnosticJSON(t, output.Bytes())
			if _, exists := actual["safe_value"]; exists != test.valueAllowed {
				t.Fatalf("safe value ignored its capture context: %#v", actual)
			}
			for _, key := range []string{"output_ticks", "bytes", "ended"} {
				if _, exists := actual[key]; exists != test.priorAllowed {
					t.Fatalf("previous progress ignored its availability: %#v", actual)
				}
			}
		})
	}
}

func TestHandlerProgressFieldsDoNotExpandOtherEventAllowlists(t *testing.T) {
	for _, message := range []string{"server stopped", "transcode failed", "request completed"} {
		t.Run(message, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewHandler(nil, slog.NewJSONHandler(&output, nil)).WithAttrs([]slog.Attr{
				slog.String("error_class", "process_progress"), slog.String("field", "progress"), slog.Bool("truncated", false),
			})
			record := diagnosticTestRecord(message, slog.String("safe_value", "end"), slog.String("phase", "finish"),
				slog.String("reason", "invalid_state"), slog.Int("line_bytes", 12), slog.Int("captured_bytes", 12),
				slog.String("line_sha256", strings.Repeat("a", 64)), slog.Bool("previous_known", true),
				slog.Int64("output_ticks", 10), slog.Bool("ended", false), slog.Bool("wait_delay", false), slog.String("wait_error_class", "none"))
			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if actual := diagnosticJSON(t, output.Bytes()); len(actual) != 4 {
				t.Fatalf("progress diagnostic fields expanded an existing event: %#v", actual)
			}
		})
	}
}

func TestHandlerIdentifiersVersionsAndAdministrativeEnums(t *testing.T) {
	tests := []struct {
		message string
		attr    slog.Attr
		key     string
		value   any
	}{
		{"user created", slog.String("user_id", diagnosticTestID), "user_id", diagnosticTestID},
		{"user created", slog.String("user_id", "01234567-89AB-CDEF-0123-456789ABCDEF"), "user_id", "01234567-89ab-cdef-0123-456789abcdef"},
		{"user created", slog.String("user_id", "../../test-secret"), "user_id", nil},
		{"user created", slog.Int64("user_id", 7), "user_id", nil},
		{"administrator device removed", slog.Int64("device_id", 7), "device_id", float64(7)},
		{"administrator device removed", slog.String("device_id", "7"), "device_id", nil},
		{"application key revoked", slog.Int64("application_key_id", 0), "application_key_id", nil},
		{"user created", slog.Bool("administrator", true), "administrator", true},
		{"user created", slog.String("administrator", "true"), "administrator", nil},
		{"administrator user mutation", slog.String("action", "update_user"), "action", "update_user"},
		{"administrator user mutation", slog.String("action", "reset_user_password"), "action", "reset_user_password"},
		{"administrator user mutation", slog.String("action", "test-secret"), "action", nil},
		{"administrator session revocation", slog.String("kind", "emby"), "kind", "emby"},
		{"administrator session revocation", slog.String("kind", "application_key"), "kind", nil},
		{"server listening", slog.String("version", "0.1.0-dev"), "version", "0.1.0-dev"},
		{"server listening", slog.String("version", "v12.3.4-rc.2+abcdef0"), "version", "v12.3.4-rc.2+abcdef0"},
		{"server listening", slog.String("version", "1.2.3-test-secret"), "version", nil},
		{"server listening", slog.String("version", "01.2.3"), "version", nil},
		{"server listening", slog.String("address", "https://user:test-secret@host"), "address", nil},
		{"server listening", slog.String("database", "postgresql"), "database", "postgresql"},
		{"server listening", slog.String("database", "postgresql://test-secret"), "database", nil},
	}
	for index, test := range tests {
		var output bytes.Buffer
		handler := NewHandler(nil, slog.NewJSONHandler(&output, nil))
		if err := handler.Handle(context.Background(), diagnosticTestRecord(test.message, test.attr)); err != nil {
			t.Fatal(err)
		}
		actual := diagnosticJSON(t, output.Bytes())
		if !reflect.DeepEqual(actual[test.key], test.value) || bytes.Contains(output.Bytes(), []byte("test-secret")) {
			t.Errorf("case %d retained or lost unexpected data: %#v", index, actual)
		}
	}
}

func TestHandlerClassifiesOnlyKnownErrorTypesWithoutText(t *testing.T) {
	cycle := &fs.PathError{Op: "test-secret", Path: "/test-secret"}
	cycle.Err = cycle
	var nilPath *fs.PathError
	tests := []struct {
		err   error
		class string
	}{
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "deadline_exceeded"},
		{io.EOF, "eof"},
		{io.ErrUnexpectedEOF, "unexpected_eof"},
		{&fs.PathError{Op: "test-secret", Path: "/private/test-secret", Err: fs.ErrPermission}, "permission_denied"},
		{&url.Error{Op: "test-secret", URL: "https://test-secret", Err: &net.OpError{Op: "test-secret", Err: syscall.ECONNREFUSED}}, "connection_refused"},
		{&os.SyscallError{Syscall: "test-secret", Err: syscall.ENOSPC}, "no_space"},
		{&os.LinkError{Op: "test-secret", Old: "/test-secret", New: "/test-secret", Err: fs.ErrNotExist}, "not_found"},
		{&net.DNSError{Err: "test-secret", Name: "test-secret", IsTimeout: true}, "deadline_exceeded"},
		{&exec.ExitError{Stderr: []byte("test-secret")}, "process_exit"},
		{errors.New("test-secret"), "unclassified"},
		{cycle, "unclassified"},
		{nilPath, "unclassified"},
	}
	for index, test := range tests {
		var output bytes.Buffer
		handler := NewHandler(nil, slog.NewJSONHandler(&output, nil))
		if err := handler.Handle(context.Background(), diagnosticTestRecord("server stopped", slog.Any("error", test.err))); err != nil {
			t.Fatal(err)
		}
		actual := diagnosticJSON(t, output.Bytes())
		if actual["error_class"] != test.class || bytes.Contains(output.Bytes(), []byte("test-secret")) {
			t.Errorf("case %d leaked error text or lost its safe class: %#v", index, actual)
		}
	}
}

func TestHandlerBoundsRecordsAndGroupDepth(t *testing.T) {
	var output bytes.Buffer
	handler := NewHandler(nil, slog.NewJSONHandler(&output, nil))
	attrs := make([]slog.Attr, 1000)
	for index := range attrs {
		attrs[index] = slog.String("route", "GET /emby/Videos/{Id}/{MediaSourceId}/Subtitles/{Index}/{StartPositionTicks}/{SubtitleFileName}")
	}
	handler = handler.WithAttrs(attrs)
	for index := 0; index < 1000; index++ {
		handler = handler.WithGroup("context").WithAttrs(attrs)
	}
	record := diagnosticTestRecord("request completed", slog.String("route", strings.Repeat("test-secret", 100000)))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	actual := diagnosticJSON(t, output.Bytes())
	if actual["event"] != "request.completed" || bytes.Contains(output.Bytes(), []byte("test-secret")) {
		t.Fatal("bounded output lost its event classification or retained secret input")
	}
	output.Reset()
	record = diagnosticTestRecord(strings.Repeat("test-secret", 100000), attrs...)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	actual = diagnosticJSON(t, output.Bytes())
	if actual["event"] != "unclassified" || len(actual) != 4 {
		t.Fatal("oversized unknown record retained unsafe attributes")
	}
}

type diagnosticCaptureHandler struct {
	records         []slog.Record
	err             error
	panic           bool
	delegate        slog.Handler
	delegateEnabled bool
}

func (handler *diagnosticCaptureHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if handler.delegateEnabled && handler.delegate != nil {
		return handler.delegate.Enabled(ctx, level)
	}
	return true
}
func (handler *diagnosticCaptureHandler) Handle(ctx context.Context, record slog.Record) error {
	if handler.panic {
		panic("test-secret")
	}
	handler.records = append(handler.records, record.Clone())
	if handler.delegate != nil {
		return handler.delegate.Handle(ctx, record)
	}
	return handler.err
}
func (handler *diagnosticCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }
func (handler *diagnosticCaptureHandler) WithGroup(string) slog.Handler      { return handler }

func TestHandlerFallbackReceivesSanitizedRecordAndNoProgramCounter(t *testing.T) {
	fallback := &diagnosticCaptureHandler{}
	handler := NewHandler(nil, fallback)
	record := diagnosticTestRecord("request completed", slog.String("route", "/test-secret"), slog.Any("body", []byte("test-secret")))
	programCounters := make([]uintptr, 1)
	runtime.Callers(1, programCounters)
	record.PC = programCounters[0]
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if len(fallback.records) != 1 || fallback.records[0].PC != 0 || fallback.records[0].Message != "request completed" {
		t.Fatal("fallback did not receive one sanitized record")
	}
	fallback.records[0].Attrs(func(attr slog.Attr) bool {
		if attr.Value.Kind() != slog.KindString || attr.Key != "event" && attr.Key != "route" {
			t.Fatal("fallback received an unsafe attribute")
		}
		if strings.Contains(attr.Value.String(), "test-secret") {
			t.Fatal("fallback received secret text")
		}
		return true
	})
}

func TestHandlerContainsFallbackPanicsAndRecursion(t *testing.T) {
	fallback := &diagnosticCaptureHandler{panic: true}
	handler := NewHandler(nil, fallback)
	if err := handler.Handle(context.Background(), diagnosticTestRecord("server stopped")); !errors.Is(err, errDiagnosticFallback) {
		t.Fatal("fallback panic was not reduced to a fixed error")
	}
	fallback = &diagnosticCaptureHandler{}
	handler = NewHandler(nil, fallback)
	fallback.delegate = handler
	if err := handler.Handle(context.Background(), diagnosticTestRecord("server stopped")); !errors.Is(err, errDiagnosticRecursion) {
		t.Fatal("fallback recursion was not rejected")
	}
	if len(fallback.records) != 1 {
		t.Fatal("recursive fallback repeated a diagnostic record")
	}
}

func TestHandlerRejectsRecursiveFallbackEnabled(t *testing.T) {
	fallback := &diagnosticCaptureHandler{delegateEnabled: true}
	handler := NewHandler(nil, fallback)
	fallback.delegate = handler
	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("recursive Enabled delegation was accepted")
	}
	// Logger checks Enabled before reaching Handle, so a guard only in Handle
	// cannot protect against this delegation cycle.
	slog.New(handler).Info("server starting")
	if len(fallback.records) != 0 {
		t.Fatal("a disabled recursive fallback received a record")
	}
}

func TestHandlerPersistsTheSameSafeRecordAsFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("diagnostic persistence requires Linux descriptors")
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(Config{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var output bytes.Buffer
	handler := NewHandler(store, slog.NewJSONHandler(&output, nil))
	record := diagnosticTestRecord("transcode failed", slog.Any("error", &exec.ExitError{Stderr: []byte("test-secret")}),
		slog.String("job_id", diagnosticTestID), slog.String("path", "/private/test-secret"))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	page, err := store.List(context.Background(), ListOptions{Limit: 200})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list persisted diagnostics: count=%d error=%v", len(page.Items), err)
	}
	lines, err := store.Lines(context.Background(), page.Items[0].Name, LinesOptions{Limit: 500})
	if err != nil || len(lines.Items) != 1 {
		t.Fatalf("read persisted diagnostic: count=%d error=%v", len(lines.Items), err)
	}
	if lines.Items[0]+"\n" != output.String() || strings.Contains(lines.Items[0], "test-secret") {
		t.Fatal("persistence and fallback did not receive the same safe record")
	}
	diagnosticJSON(t, output.Bytes())
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := handler.Handle(context.Background(), record); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unavailable storage did not return its failure")
	}
	if output.Len() == 0 || bytes.Contains(output.Bytes(), []byte("test-secret")) {
		t.Fatal("safe fallback output stopped when persistence failed")
	}
}
