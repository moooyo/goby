//go:build linux

package transcode

import (
	"fmt"
	"os"
	"sync"
	"syscall"
	"testing"
)

func TestHLSClockObserverSeparatesConcurrentRenditionPipes(t *testing.T) {
	plan := adaptiveHLSPlan()
	plan.Subtitle = SubtitlePlan{Mode: "hls", Codec: "subrip", StreamIndex: 2}
	clocks := make(chan HLSMuxClock, 2)
	observer, err := newHLSClockObserver(plan, func(progress Progress) { clocks <- *progress.HLSClock }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.close()
	if len(observer.pipes) != 2 {
		t.Fatalf("muxers share %d pipes", len(observer.pipes))
	}
	var childWrites []*os.File
	for _, pipe := range observer.pipes {
		fd, err := syscall.Dup(int(pipe.write.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		file := os.NewFile(uintptr(fd), "clock-child-write")
		childWrites = append(childWrites, file)
		defer file.Close()
	}
	observer.start()
	var writers sync.WaitGroup
	for index, file := range childWrites {
		writers.Add(1)
		go func(index int, file *os.File) {
			defer writers.Done()
			defer file.Close()
			for packet := 0; packet < 64; packet++ {
				_, _ = fmt.Fprintf(file, "GOBY %d %d 1/90000 %d\n", index, packet, index*90000+packet*3000)
			}
		}(index, file)
	}
	writers.Wait()
	if err := observer.finish(); err != nil {
		t.Fatalf("concurrent clocks were corrupted: %v", err)
	}
	close(clocks)
	seen := map[int]int64{}
	for clock := range clocks {
		seen[clock.Rendition] = clock.PTS
	}
	if len(seen) != 2 || seen[0] != 0 || seen[1] != 90000 {
		t.Fatalf("renditions lost their own clocks: %+v", seen)
	}
}
