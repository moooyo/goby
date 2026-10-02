//go:build linux

package transcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

func generatedAudioInputChildWrites(t *testing.T, observer *generatedAudioInputObserver) []*os.File {
	t.Helper()
	var files []*os.File
	for _, pipe := range observer.pipes {
		fd, err := syscall.Dup(int(pipe.write.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		file := os.NewFile(uintptr(fd), "generated-audio-input-test-child")
		files = append(files, file)
		t.Cleanup(func() { _ = file.Close() })
	}
	return files
}

func generatedAudioInputObserverFinish(t *testing.T, observer *generatedAudioInputObserver) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- observer.finish() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		observer.close()
		t.Fatal("generated audio input observer did not join its owned readers")
		return nil
	}
}

func TestGeneratedAudioInputObserverSeparatesConcurrentRenditions(t *testing.T) {
	options := []RawGeneratedAudioInputOptions{generatedAudioInputTestOptions(0), generatedAudioInputTestOptions(1)}
	observer, err := newGeneratedAudioInputObserver(context.Background(), options, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	children := generatedAudioInputChildWrites(t, observer)
	observer.start()
	var writers sync.WaitGroup
	for rendition, child := range children {
		writers.Add(1)
		go func(rendition int, child *os.File) {
			defer writers.Done()
			defer child.Close()
			for number := int64(0); number < 64; number++ {
				_, _ = fmt.Fprint(child, generatedAudioInputRecord(rendition, number, number*1024, 1024, number*1024, number, number*1024+int64(rendition)*48000))
			}
		}(rendition, child)
	}
	writers.Wait()
	if err := generatedAudioInputObserverFinish(t, observer); err != nil {
		t.Fatal(err)
	}
	got := observer.snapshot()
	for rendition := range children {
		if got[rendition].Rendition != rendition || got[rendition].Frames != 64 || got[rendition].TotalSamples != 65536 ||
			!got[rendition].EncoderSampleClockExact || !got[rendition].InputCadenceExact || got[rendition].FirstInput.Clock.PTS != int64(rendition)*48000 {
			t.Fatalf("private rendition evidence interleaved: %+v", got)
		}
	}
	for _, pipe := range observer.pipes {
		if _, err := pipe.read.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("completed observer kept its reader open: %v", err)
		}
	}
}

func TestGeneratedAudioInputObserverFaultsCloseAllPipes(t *testing.T) {
	for name, data := range map[string]string{
		"foreign rendition": generatedAudioInputRecord(1, 0, 0, 1024, 0, 0, 0),
		"truncated record":  "GOBY_AUDIO 0 1 0 0 1024 1/48000 0 0 1/48000 0",
		"missing evidence":  "",
	} {
		t.Run(name, func(t *testing.T) {
			observer, err := newGeneratedAudioInputObserver(context.Background(), []RawGeneratedAudioInputOptions{generatedAudioInputTestOptions(0), generatedAudioInputTestOptions(1)}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer observer.close()
			children := generatedAudioInputChildWrites(t, observer)
			observer.start()
			_, _ = fmt.Fprint(children[0], data)
			_ = children[0].Close()
			// The other inherited writer deliberately stays open.
			if err := generatedAudioInputObserverFinish(t, observer); err == nil {
				t.Fatal("invalid evidence was accepted or another reader was not joined")
			}
			if _, err := children[1].Write([]byte("still open")); err == nil {
				t.Fatal("faulted evidence retained the sibling reader")
			}
		})
	}
}

func TestGeneratedAudioInputObserverAbnormalCallbacksJoinAllReaders(t *testing.T) {
	for name, callback := range map[string]func(RawGeneratedAudioInputEvidence){
		"panic":    func(RawGeneratedAudioInputEvidence) { panic("private callback detail") },
		"nilpanic": func(RawGeneratedAudioInputEvidence) { panic(nil) },
		"Goexit":   func(RawGeneratedAudioInputEvidence) { runtime.Goexit() },
	} {
		t.Run(name, func(t *testing.T) {
			for _, faultRendition := range []int{0, 1} {
				t.Run(fmt.Sprintf("fault_rendition_%d", faultRendition), func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					observer, err := newGeneratedAudioInputObserver(ctx, []RawGeneratedAudioInputOptions{generatedAudioInputTestOptions(0), generatedAudioInputTestOptions(1)}, callback, func() {
						cancel()
						runtime.Goexit()
					})
					if err != nil {
						t.Fatal(err)
					}
					defer observer.close()
					children := generatedAudioInputChildWrites(t, observer)
					observer.start()
					// Only this rendition reports. The sibling's inherited writer
					// stays open until the originating fault closes its reader.
					_, _ = fmt.Fprint(children[faultRendition], generatedAudioInputRecord(faultRendition, 0, 0, 1024, 0, 0, 0))
					if err := generatedAudioInputObserverFinish(t, observer); !errors.Is(err, ErrProgress) || errors.Is(err, os.ErrClosed) || ctx.Err() == nil {
						t.Fatalf("originating callback fault was hidden by sibling close: rendition=%d, error=%v, context=%v", faultRendition, err, ctx.Err())
					}
					for _, child := range children {
						if _, err := child.Write([]byte("still open")); err == nil {
							t.Fatal("abnormal callback retained an inherited writer's reader")
						}
					}
				})
			}
		})
	}
}

func TestGeneratedAudioInputObserverCancellationCannotUpgradeCompleteRows(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	observer, err := newGeneratedAudioInputObserver(ctx, []RawGeneratedAudioInputOptions{generatedAudioInputTestOptions(0)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	children := generatedAudioInputChildWrites(t, observer)
	observer.start()
	_, _ = fmt.Fprint(children[0], generatedAudioInputRecord(0, 0, 0, 1024, 0, 0, 0))
	cancel()
	if err := generatedAudioInputObserverFinish(t, observer); err == nil {
		t.Fatal("cancelled input was accepted as completed proof")
	}
}

func TestGeneratedAudioInputObserverRejectsInvalidConstruction(t *testing.T) {
	option := generatedAudioInputTestOptions(0)
	if _, err := newGeneratedAudioInputObserver(nil, []RawGeneratedAudioInputOptions{option}, nil, nil); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil context accepted: %v", err)
	}
	if _, err := newGeneratedAudioInputObserver(context.Background(), []RawGeneratedAudioInputOptions{option, option}, nil, nil); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("shared rendition path accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newGeneratedAudioInputObserver(ctx, []RawGeneratedAudioInputOptions{option}, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled observer construction accepted: %v", err)
	}
}
