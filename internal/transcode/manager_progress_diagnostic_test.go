//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
)

type managerProgressLogObservation struct {
	data          []byte
	contextErr    error
	locksReleased bool
	finished      bool
	running       int
}

type managerProgressLogSink struct {
	slog.Handler
	manager *Manager
	records chan managerProgressLogObservation
}

func (sink *managerProgressLogSink) Handle(ctx context.Context, record slog.Record) error {
	observed := managerProgressLogObservation{contextErr: ctx.Err()}
	locks := make(chan managerProgressLogObservation, 1)
	go func() {
		value := managerProgressLogObservation{}
		sink.manager.mu.Lock()
		value.running = sink.manager.running
		for _, job := range sink.manager.jobs {
			value.finished = job.finished
		}
		sink.manager.mu.Unlock()
		sink.manager.filesMu.Lock()
		sink.manager.filesMu.Unlock()
		value.locksReleased = true
		locks <- value
	}()
	select {
	case value := <-locks:
		observed.locksReleased, observed.finished, observed.running = value.locksReleased, value.finished, value.running
	case <-time.After(2 * time.Second):
	}
	var output bytes.Buffer
	err := slog.NewJSONHandler(&output, nil).Handle(ctx, record)
	observed.data = append([]byte(nil), output.Bytes()...)
	sink.records <- observed
	return err
}

func TestManagerProgressFailureDiagnostic(t *testing.T) {
	for _, test := range []struct {
		name, stopCode, wantCode, wantState, wantClass string
		runErr                                         error
		diagnostic, previous, wantLog                  bool
	}{
		{name: "progress failure", runErr: fmt.Errorf("%w: secret-argument", ErrProgress), diagnostic: true,
			previous: true, wantLog: true, wantCode: "process_progress", wantState: "failed", wantClass: "process_progress"},
		{name: "higher priority context", runErr: context.Canceled, diagnostic: true, wantLog: true,
			wantCode: "cancelled", wantState: "cancelled", wantClass: "cancelled"},
		{name: "existing stop reason", runErr: ErrProgress, diagnostic: true, wantLog: true,
			stopCode: "progress_timeout", wantCode: "progress_timeout", wantState: "failed", wantClass: "process_progress"},
		{name: "no diagnostic", runErr: ErrProgress, wantCode: "process_progress", wantState: "failed"},
		{name: "successful result", diagnostic: true, wantState: "completed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			previousLogger := slog.Default()
			t.Cleanup(func() { slog.SetDefault(previousLogger) })
			var failure *ProgressFailure
			if test.diagnostic {
				failure = &ProgressFailure{Phase: "Write", Reason: "invalid_time", Field: "out_time_us",
					LineBytes: 14, CapturedBytes: 14, LineSHA256: strings.Repeat("a", 64),
					WaitDelay: true, WaitErrorClass: "wait_delay", ExitCode: -1}
				if test.previous {
					failure.SafeValue = "-1"
					failure.Previous = &ProgressSnapshot{OutputTicks: 120, Bytes: 40, Ended: false}
				}
			}
			started, release := make(chan struct{}), make(chan struct{})
			run := func(ctx context.Context, _, directory string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
				if err := publishManagerTestOutput(directory, 188); err != nil {
					return RunResult{}, err
				}
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return RunResult{StderrTail: "secret-stderr /secret/path --secret-argument", ProgressFailure: failure}, test.runErr
			}
			manager := newTestManager(t, managerTestOptions(t, run))
			sink := &managerProgressLogSink{Handler: slog.NewJSONHandler(io.Discard, nil), manager: manager,
				records: make(chan managerProgressLogObservation, 8)}
			slog.SetDefault(slog.New(diagnostics.NewHandler(nil, sink)))
			record, err := manager.Ensure(context.Background(), managerTestSpec(1), managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("fake runner did not start")
			}
			if test.stopCode != "" {
				manager.mu.Lock()
				manager.stopLocked(manager.jobs[record.ID], test.stopCode)
				manager.mu.Unlock()
			}
			close(release)
			finished := managerTestWaitFinished(t, manager, record.ID)
			if finished.State != test.wantState || finished.ErrorCode != test.wantCode {
				t.Fatalf("diagnostic changed the durable outcome: %+v", finished)
			}
			if test.wantLog {
				var observed managerProgressLogObservation
				select {
				case observed = <-sink.records:
				case <-time.After(5 * time.Second):
					t.Fatal("progress diagnostic was not emitted")
				}
				if observed.contextErr != nil || !observed.locksReleased || !observed.finished || observed.running != 0 {
					t.Fatalf("diagnostic ran before finalization or under a manager lock: %+v", observed)
				}
				var value map[string]any
				if err := json.Unmarshal(observed.data, &value); err != nil {
					t.Fatal(err)
				}
				if value["msg"] != "transcode progress rejected" || value["event"] != "transcode.progress.invalid" ||
					value["level"] != "WARN" || value["job_id"] != record.ID || value["error_class"] != test.wantClass ||
					value["phase"] != "Write" || value["reason"] != "invalid_time" || value["field"] != "out_time_us" ||
					value["line_bytes"] != float64(14) || value["captured_bytes"] != float64(14) || value["truncated"] != false ||
					value["line_sha256"] != strings.Repeat("a", 64) || value["previous_known"] != test.previous ||
					value["wait_delay"] != true || value["wait_error_class"] != "wait_delay" || value["exit_code"] != float64(-1) {
					t.Fatalf("diagnostic lost its source and job binding: %s", observed.data)
				}
				if test.previous {
					if value["safe_value"] != "-1" || value["output_ticks"] != float64(120) || value["bytes"] != float64(40) || value["ended"] != false {
						t.Fatalf("previous progress was not retained: %s", observed.data)
					}
				} else {
					for _, key := range []string{"safe_value", "output_ticks", "bytes", "ended"} {
						if _, exists := value[key]; exists {
							t.Fatalf("unknown progress invented %q: %s", key, observed.data)
						}
					}
				}
				for _, secret := range []string{"secret-stderr", "/secret/path", "secret-argument", "/not-executed/ffmpeg"} {
					if bytes.Contains(observed.data, []byte(secret)) {
						t.Fatalf("diagnostic exposed %q: %s", secret, observed.data)
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := manager.Close(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case observed := <-sink.records:
				t.Fatalf("unexpected or duplicate diagnostic: %s", observed.data)
			default:
			}
		})
	}
}
