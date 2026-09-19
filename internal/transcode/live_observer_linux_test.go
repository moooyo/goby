//go:build linux

package transcode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func liveTestTransportPackets() []byte {
	data := make([]byte, 188*3)
	for offset := 0; offset < len(data); offset += 188 {
		data[offset], data[offset+3] = 0x47, 0x10
	}
	return data
}

func liveTestObserver(t *testing.T, p Plan, publish func(context.Context, Spec, string, LiveSegment) error) (*liveObserver, []*os.File, string, context.CancelFunc) {
	t.Helper()
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	o, err := newLiveObserver(ctx, directory, p, liveRuntime{maxBytes: 1 << 20, timeout: time.Second, publish: publish}, cancel, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); o.close() })
	var writers []*os.File
	for _, pipe := range o.journals {
		fd, err := syscall.Dup(int(pipe.write.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		file := os.NewFile(uintptr(fd), "live-test-child-journal")
		writers = append(writers, file)
		t.Cleanup(func() { _ = file.Close() })
	}
	return o, writers, directory, cancel
}

func liveTestFinish(t *testing.T, o *liveObserver) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- o.finish(true) }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("live publication did not retire")
		return nil
	}
}

func TestLiveObserverPublishesEveryClosedRecordAndReclaimsBorrowedFiles(t *testing.T) {
	p := liveTestPlan()
	var bundles []LiveSegment
	var borrowed []*os.File
	o, writers, directory, _ := liveTestObserver(t, p, func(_ context.Context, _ Spec, _ string, bundle LiveSegment) error {
		if bundle.RenditionCount != 1 || bundle.Renditions[0].Media == nil {
			t.Error("callback lacks a complete rendition")
		}
		data := make([]byte, 188*3)
		if _, err := bundle.Renditions[0].Media.ReadAt(data, 0); err != nil || !bytes.Equal(data, liveTestTransportPackets()) {
			t.Errorf("borrowed media differs: %v", err)
		}
		bundles = append(bundles, bundle)
		borrowed = append(borrowed, bundle.Renditions[0].Media)
		return nil
	})
	o.setClock(HLSMuxClock{Rendition: 0, PTS: 125, TimeBaseNumerator: 1, TimeBaseDenominator: 100})
	for sequence := 0; sequence < 3; sequence++ {
		if err := os.WriteFile(filepath.Join(directory, liveSegmentName(p, 0, int64(sequence), true)), liveTestTransportPackets(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	o.start()
	for sequence := 0; sequence < 3; sequence++ {
		start := 1.25 + float64(sequence)
		if sequence == 0 {
			start = 0
		}
		fmt.Fprintf(writers[0], "%s,%.6f,%.6f\n", liveSegmentName(p, 0, int64(sequence), true), start, 2.25+float64(sequence))
	}
	_ = writers[0].Close()
	if err := liveTestFinish(t, o); err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 3 || bundles[0].StartTicks != 12_500_000 || bundles[0].DurationTicks != ticksPerSecond || !bundles[0].Discontinuity || bundles[1].Discontinuity {
		t.Fatalf("journal lost records or inferred the wrong first clock: %+v", bundles)
	}
	for _, file := range borrowed {
		if _, err := file.Stat(); err == nil {
			t.Fatal("borrowed media survived callback lifetime")
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("published scratch was retained: %v %v", entries, err)
	}
}

func TestLiveObserverRejectsUnalignedRenditionsBeforePublishing(t *testing.T) {
	p := liveTestPlan()
	p.HLS.RenditionCount = 2
	called := false
	o, writers, directory, _ := liveTestObserver(t, p, func(context.Context, Spec, string, LiveSegment) error { called = true; return nil })
	for rendition := 0; rendition < 2; rendition++ {
		o.setClock(HLSMuxClock{Rendition: rendition, PTS: int64(1000 + 10*rendition), TimeBaseNumerator: 1, TimeBaseDenominator: 1000})
		if err := os.WriteFile(filepath.Join(directory, liveSegmentName(p, rendition, 0, true)), liveTestTransportPackets(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	o.start()
	for rendition := 0; rendition < 2; rendition++ {
		fmt.Fprintf(writers[rendition], "%s,0.000000,2.000000\n", liveSegmentName(p, rendition, 0, true))
		_ = writers[rendition].Close()
	}
	if err := liveTestFinish(t, o); err == nil || called {
		t.Fatalf("unaligned ladder was published: called=%t err=%v", called, err)
	}
}

func TestLiveFragmentsSplitOnlyCompleteValidatedMedia(t *testing.T) {
	p := liveTestPlan()
	p.Container, p.HLS.SegmentType = "mp4", "fmp4"
	p.AudioStreamIndex, p.AudioCodec = -1, ""
	initialization := videoReadyInit(videoReadyH264Track(11))
	fragment := videoReadyFragment(videoReadyFragmentOptions{}, videoReadyRun{trackID: 11, samples: [][]byte{videoReadyIDR()}})
	data := videoReadyJoin(initialization, fragment)
	directory := t.TempDir()
	dir, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	var previous [32]byte
	for sequence := int64(0); sequence < 2; sequence++ {
		name := liveSegmentName(p, 0, sequence, true)
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		opened, err := openLiveSegment(dir, p, 0, liveJournalRecord{name: name, sequence: sequence}, MaxLiveSegmentBytes, sequence == 0, previous)
		if err != nil {
			t.Fatal(err)
		}
		if opened.render.Init == nil {
			t.Fatal("fMP4 verification did not receive its complete initialization")
		}
		actual := make([]byte, len(fragment))
		if _, err := opened.render.Media.ReadAt(actual, 0); err != nil || !bytes.Equal(actual, fragment) {
			t.Fatalf("split media changed fragment-relative offsets: %v", err)
		}
		previous = opened.initHash
		if err := opened.closeAndRemove(dir); err != nil {
			t.Fatal(err)
		}
	}
	name := liveSegmentName(p, 0, 2, true)
	if err := os.WriteFile(filepath.Join(directory, name), data[:len(data)-1], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openLiveSegment(dir, p, 0, liveJournalRecord{name: name, sequence: 2}, MaxLiveSegmentBytes, false, previous); err == nil {
		t.Fatal("a truncated mdat became a published fragment")
	}
}
