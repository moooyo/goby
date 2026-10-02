//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func generatedFMP4NativeClockOpenFixture(t *testing.T) (*os.File, *os.File, Plan) {
	t.Helper()
	initialization, segment := generatedFMP4NativeClockFixture()
	directory := t.TempDir()
	paths := []string{filepath.Join(directory, "init.mp4"), filepath.Join(directory, "segment.m4s")}
	for index, data := range [][]byte{initialization, segment} {
		if err := os.WriteFile(paths[index], data, 0600); err != nil {
			t.Fatal("controlled native-clock fixture could not be saved")
		}
	}
	var files []*os.File
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal("controlled native-clock fixture could not be held")
		}
		files = append(files, file)
		t.Cleanup(func() { _ = file.Close() })
	}
	plan := generatedClosureMediaPlan("fmp4", 100, 90, 96, false)
	return files[0], files[1], plan
}

func TestGeneratedFMP4NativeClockHeldFilesKeepOffsetsAndRejectRoleAlias(t *testing.T) {
	initialization, segment, plan := generatedFMP4NativeClockOpenFixture(t)
	if _, err := initialization.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := segment.Seek(11, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	clock, err := MeasureGeneratedFMP4NativeClock(context.Background(), plan, initialization, segment)
	if err != nil || clock.TrackID != 11 || clock.MediaTimeScale != 12_288 || clock.FragmentCount != 2 {
		t.Fatalf("held native-clock metadata was not associated: %v", err)
	}
	for index, file := range []*os.File{initialization, segment} {
		wanted := int64(7 + index*4)
		if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != wanted {
			t.Fatal("native clock measurement moved or closed a borrowed descriptor")
		}
	}
	clock, err = MeasureGeneratedFMP4NativeClock(context.Background(), plan, initialization, initialization)
	if !errors.Is(err, ErrInvalidInput) || clock != (GeneratedFMP4NativeClock{}) {
		t.Fatal("one physical object supplied both initialization and media roles")
	}
	if _, err := initialization.Stat(); err != nil {
		t.Fatal("failed role selection closed its caller's source")
	}
}

func TestGeneratedFMP4NativeClockRejectsPartialFileAndInvalidMode(t *testing.T) {
	initialization, segment, plan := generatedFMP4NativeClockOpenFixture(t)
	plan.HLS.SegmentType, plan.Container = "mpegts", "ts"
	if clock, err := MeasureGeneratedFMP4NativeClock(context.Background(), plan, initialization, segment); !errors.Is(err, ErrInvalidPlan) || clock != (GeneratedFMP4NativeClock{}) {
		t.Fatal("transport stream plan acquired a raw MP4 track clock")
	}
	plan.HLS.SegmentType, plan.Container = "fmp4", "mp4"
	info, err := segment.Stat()
	if err != nil || os.Truncate(segment.Name(), info.Size()-1) != nil {
		t.Fatal("controlled truncated native-clock fixture could not be prepared")
	}
	if clock, err := MeasureGeneratedFMP4NativeClock(context.Background(), plan, initialization, segment); err == nil || clock != (GeneratedFMP4NativeClock{}) {
		t.Fatal("partial mdat acquired native clock facts before complete framing")
	}
}
