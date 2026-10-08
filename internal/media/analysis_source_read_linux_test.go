package media

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestAnalysisSourceReadProfilesRequireAdmissionBeforeProcess(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "analysis-source-")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	for _, gpu := range []bool{false, true} {
		admissionErr := errors.New("source phase rejected")
		phases, parsed := 0, false
		ctx := WithSourceReadPhase(context.Background(), func(context.Context, func(context.Context) error) error {
			phases++
			return admissionErr
		})
		sink := &analysisDiscardStderr{}
		err := runBackgroundClipProcess(ctx, "/must-never-spawn", input, nil, time.Second, 1024, sink,
			func(io.Reader) error { parsed = true; return nil }, gpu)
		if !errors.Is(err, admissionErr) || phases != 1 || parsed || !errors.Is(sink.failure(), admissionErr) {
			t.Fatalf("profile crossed a rejected source boundary: gpu=%v phases=%d parsed=%v error=%v", gpu, phases, parsed, err)
		}
	}
}

func TestAnalysisSourceReadIncludesParserAndActualProcessCleanup(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "analysis-source-")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	tool := analysisProcessTestTool(t, "printf 'complete source output'")
	before := GetProcessCapacityStats()
	active, parsed := false, false
	ctx := WithSourceReadPhase(context.Background(), func(work context.Context, run func(context.Context) error) error {
		active = true
		err := run(work)
		active = false
		if !parsed {
			return errors.Join(err, errors.New("source phase returned before its parser"))
		}
		if after := GetProcessCapacityStats(); after != before {
			return errors.Join(err, errors.New("source phase returned before actual process cleanup"))
		}
		return err
	})
	sink := &analysisDiscardStderr{}
	err = runAnalysisStream(ctx, tool, input, nil, time.Second, 1024, sink, func(reader io.Reader) error {
		if !active {
			return errors.New("parser ran outside the source phase")
		}
		data, err := io.ReadAll(reader)
		if err == nil && string(data) != "complete source output" {
			err = errors.New("source output was incomplete")
		}
		parsed = true
		return err
	})
	if err != nil || active || !parsed || sink.failure() != nil {
		t.Fatalf("source or metadata retirement failed: parsed=%v active=%v error=%v sink=%v", parsed, active, err, sink.failure())
	}
}
