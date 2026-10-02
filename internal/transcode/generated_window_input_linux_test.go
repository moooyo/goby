//go:build linux

package transcode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func generatedInputChildWrites(t *testing.T, observer *generatedInputObserver) []*os.File {
	t.Helper()
	var files []*os.File
	for _, pipe := range observer.pipes {
		fd, err := syscall.Dup(int(pipe.write.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		file := os.NewFile(uintptr(fd), "generated-input-test-child")
		files = append(files, file)
		t.Cleanup(func() { _ = file.Close() })
	}
	return files
}

func generatedInputObserverFinish(t *testing.T, observer *generatedInputObserver) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- observer.finish() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		observer.close()
		t.Fatal("generated input observer did not drain and release its readers")
		return nil
	}
}

func TestGeneratedInputObserverSeparatesConcurrentRenditionPipes(t *testing.T) {
	var mu sync.Mutex
	var reports [MaxHLSRenditions]int
	observer, err := newGeneratedInputObserver(context.Background(), generatedInputTestPlan(), func(evidence GeneratedInputEvidence) {
		mu.Lock()
		reports[evidence.Rendition]++
		mu.Unlock()
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	if len(observer.pipes) != 2 {
		t.Fatalf("renditions share %d private input pipes", len(observer.pipes))
	}
	children := generatedInputChildWrites(t, observer)
	observer.start()
	var writers sync.WaitGroup
	for rendition, file := range children {
		writers.Add(1)
		go func(rendition int, file *os.File) {
			defer writers.Done()
			defer file.Close()
			for frame := int64(0); frame < 64; frame++ {
				_, _ = fmt.Fprint(file, generatedInputRecord(rendition, frame, frame+4, frame, int64(rendition)*90000+frame*3000))
			}
		}(rendition, file)
	}
	// Reading progress concurrently exercises the observer's snapshot lock.
	snapshotsDone := make(chan struct{})
	go func() {
		defer close(snapshotsDone)
		for index := 0; index < 256; index++ {
			_ = observer.snapshot()
		}
	}()
	writers.Wait()
	if err := generatedInputObserverFinish(t, observer); err != nil {
		t.Fatalf("concurrent input evidence was corrupted: %v", err)
	}
	<-snapshotsDone
	snapshots := observer.snapshot()
	mu.Lock()
	defer mu.Unlock()
	for rendition := range children {
		if reports[rendition] != 64 || snapshots[rendition].Rendition != rendition || snapshots[rendition].Frames != 64 || snapshots[rendition].FirstInputPTS != int64(rendition)*90000 || snapshots[rendition].LastInputPTS != int64(rendition)*90000+63*3000 {
			t.Fatalf("rendition %d lost its own input evidence: %d reports, %+v", rendition, reports[rendition], snapshots[rendition])
		}
	}
}

func TestGeneratedInputObserverRejectsMissingRenditionAndReleasesPipes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer, err := newGeneratedInputObserver(ctx, generatedInputTestPlan(), nil, cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	children := generatedInputChildWrites(t, observer)
	observer.start()
	_, _ = fmt.Fprint(children[0], generatedInputRecord(0, 0, 0, 0, 0))
	for _, child := range children {
		_ = child.Close()
	}
	if err := generatedInputObserverFinish(t, observer); err == nil {
		t.Fatal("unobserved rendition inherited another output's input evidence")
	}
	observer.close()
	for _, pipe := range observer.pipes {
		if _, err := pipe.read.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("completed observer retained a read descriptor: %v", err)
		}
		if _, err := pipe.write.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("completed observer retained a write descriptor: %v", err)
		}
	}
}

func TestGeneratedInputObserverCancellationUnblocksAllReaders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observer, err := newGeneratedInputObserver(ctx, generatedInputTestPlan(), nil, cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	children := generatedInputChildWrites(t, observer)
	observer.start()
	// Simulated child descriptors remain open. Context cancellation must
	// unblock the reader rather than wait for those descriptors to reach EOF.
	cancel()
	if err := generatedInputObserverFinish(t, observer); err == nil {
		t.Fatal("cancelled observer accepted incomplete input evidence")
	}
	for _, child := range children {
		_ = child.Close()
	}
}

func TestGeneratedInputObserverRejectsForeignOutputAndCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer, err := newGeneratedInputObserver(ctx, generatedInputTestPlan(), nil, cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	children := generatedInputChildWrites(t, observer)
	observer.start()
	_, _ = fmt.Fprint(children[0], generatedInputRecord(1, 0, 0, 0, 0))
	if err := generatedInputObserverFinish(t, observer); err == nil || ctx.Err() == nil {
		t.Fatalf("foreign input evidence did not fail closed: %v, context=%v", err, ctx.Err())
	}
	for _, child := range children {
		_ = child.Close()
	}
}

func TestGeneratedInputObserverInvalidEvidenceUnblocksWithoutCancelCallback(t *testing.T) {
	observer, err := newGeneratedInputObserver(context.Background(), generatedInputTestPlan(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	children := generatedInputChildWrites(t, observer)
	observer.start()
	// The second simulated child keeps its writer open. The first invalid
	// pipe must invalidate the set and release all readers by itself.
	_, _ = fmt.Fprint(children[0], generatedInputRecord(1, 0, 0, 0, 0))
	if err := generatedInputObserverFinish(t, observer); err == nil {
		t.Fatal("invalid evidence left a valid-looking whole-output snapshot")
	}
	for _, child := range children {
		_ = child.Close()
	}
}

func TestGeneratedInputObserverReportPanicFailsAndJoinsEveryReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer, err := newGeneratedInputObserver(ctx, generatedInputTestPlan(), func(GeneratedInputEvidence) {
		panic("sensitive report callback detail")
	}, func() {
		cancel()
		panic("sensitive cancellation callback detail")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	children := generatedInputChildWrites(t, observer)
	defer func() {
		for _, child := range children {
			_ = child.Close()
		}
	}()
	observer.start()
	// The other rendition keeps its inherited writer open. A report panic
	// must invalidate and join the entire set without waiting for that EOF.
	_, _ = fmt.Fprint(children[0], generatedInputRecord(0, 0, 0, 0, 0))
	if err := generatedInputObserverFinish(t, observer); !errors.Is(err, ErrProgress) || ctx.Err() == nil {
		t.Fatalf("report panic escaped or left complete-looking evidence: %v, context=%v", err, ctx.Err())
	}
	for _, child := range children {
		if _, err := child.Write([]byte("still open")); err == nil {
			t.Fatal("failed observer retained an inherited writer's live reader")
		}
	}
}

func TestGeneratedInputObserverAbnormalReportExitFailsAndJoinsEveryReader(t *testing.T) {
	for name, report := range map[string]func(GeneratedInputEvidence){
		"nil panic":      func(GeneratedInputEvidence) { panic(nil) },
		"goroutine exit": func(GeneratedInputEvidence) { runtime.Goexit() },
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observer, err := newGeneratedInputObserver(ctx, generatedInputTestPlan(), report, func() {
				cancel()
				runtime.Goexit()
			})
			if err != nil {
				t.Fatal(err)
			}
			defer observer.close()
			children := generatedInputChildWrites(t, observer)
			defer func() {
				for _, child := range children {
					_ = child.Close()
				}
			}()
			observer.start()
			_, _ = fmt.Fprint(children[0], generatedInputRecord(0, 0, 0, 0, 0))
			if err := generatedInputObserverFinish(t, observer); !errors.Is(err, ErrProgress) || ctx.Err() == nil {
				t.Fatalf("abnormal report exit looked like complete observer EOF: %v, context=%v", err, ctx.Err())
			}
			for _, child := range children {
				if _, err := child.Write([]byte("still open")); err == nil {
					t.Fatal("abnormally exited reporter retained another reader")
				}
			}
		})
	}
}

func TestGeneratedInputObserverRejectsCancelledConstruction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newGeneratedInputObserver(ctx, generatedInputTestPlan(), nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observer construction = %v", err)
	}
	if _, err := newGeneratedInputObserver(nil, generatedInputTestPlan(), nil, nil); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil observer context = %v", err)
	}
}

func TestGeneratedInputObserverCalibratesActualFFmpegDuplicateAndDropFields(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for actual stats_enc_pre input-clock calibration")
	}
	for _, fixture := range []struct {
		name        string
		inputRate   int
		wantRepeat  bool
		wantSkipped bool
	}{
		{name: "CFR duplicate", inputRate: 24, wantRepeat: true},
		{name: "CFR drop", inputRate: 60, wantSkipped: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			p := generatedInputTestPlan()
			p.StartTicks, p.DurationTicks = 0, 4*ticksPerSecond
			p.HLS.Window.EndTicks, p.HLS.RenditionCount, p.HLS.Renditions = 3*ticksPerSecond, 0, [MaxHLSRenditions]HLSRendition{}
			var previous GeneratedInputEvidence
			var repeat, skipped bool
			observer, err := newGeneratedInputObserver(ctx, p, func(evidence GeneratedInputEvidence) {
				if previous.Frames > 0 {
					repeat = repeat || evidence.LastInputFrame == previous.LastInputFrame && evidence.LastInputPTS == previous.LastInputPTS
					skipped = skipped || evidence.LastInputFrame > previous.LastInputFrame+1
				}
				previous = evidence
			}, cancel)
			if err != nil {
				t.Fatal(err)
			}
			defer observer.close()
			command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-nostats", "-v", "error", "-f", "lavfi", "-i",
				fmt.Sprintf("testsrc2=size=160x90:rate=%d:duration=2", fixture.inputRate), "-map", "0:v:0", "-c:v", "libx264", "-threads:v", "1", "-bf", "0", "-r", "30",
				"-stats_enc_pre:v:0", "pipe:3", "-stats_enc_pre_fmt:v:0", "GOBY_INPUT {fidx} {sidx} {n} {ni} {tb} {pts} {tbi} {ptsi}", "-f", "null", "-")
			command.ExtraFiles = []*os.File{observer.pipes[0].write}
			var stderr bytes.Buffer
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatalf("calibration process start failed: %v", err)
			}
			observer.start()
			processErr := command.Wait()
			observerErr := generatedInputObserverFinish(t, observer)
			if processErr != nil || observerErr != nil || stderr.Len() != 0 {
				t.Fatalf("real input evidence failed: process=%v, observer=%v: %s", processErr, observerErr, strings.TrimSpace(stderr.String()))
			}
			evidence := observer.snapshot()[0]
			// A complete null output's CFR retirement may flush up to two
			// additional encoder frames. This calibration checks association
			// facts, not an exact finite-window duration. Closure independently
			// rejects any count or cadence that exceeds its published range.
			if err := ValidateGeneratedInputEvidence(p, evidence); err != nil || evidence.Frames < 59 || evidence.Frames > 62 ||
				repeat != fixture.wantRepeat || skipped != fixture.wantSkipped || evidence.SourceSequential {
				t.Fatalf("actual duplicate/drop fields did not match their documented input meaning: %+v, repeat=%v, skipped=%v, error=%v", evidence, repeat, skipped, err)
			}
			t.Logf("actual input clock calibration: %+v, repeated input=%v, skipped input=%v", evidence, repeat, skipped)
		})
	}
}
