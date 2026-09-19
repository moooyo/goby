//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Future clusters are withheld until output advances. This distinguishes live
// progress from a finite demuxer reaching its next cue or flushing at EOF; -re
// remuxing is not a reliable boundary for sparse subtitle inputs.
func TestLiveBitmapActualSilentPeriodsAdvanceBeforeFutureCuesAndEOF(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	for _, cue := range []bool{false, true} {
		t.Run(fmt.Sprintf("cue_%t", cue), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			directory := t.TempDir()
			sup, source := filepath.Join(directory, "events.sup"), filepath.Join(directory, "source.mkv")
			if err := os.WriteFile(sup, liveBitmapPGSFixture(cue), 0600); err != nil {
				t.Fatal(err)
			}
			// The first nonempty SUP event is at six seconds. Preserve that
			// shared source clock instead of subtracting the SUP's own start
			// time independently of the video and audio inputs.
			args := []string{"-hide_banner", "-v", "error", "-nostdin", "-copyts", "-filter_threads", "1",
				"-f", "lavfi", "-i", "color=c=black:s=320x192:r=16:d=12", "-f", "sup", "-i", sup}
			if cue {
				args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=12")
			}
			args = append(args, "-map", "0:v:0")
			if cue {
				args = append(args, "-map", "2:a:0", "-c:a", "pcm_s16le")
			}
			args = append(args, "-map", "1:s:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-bf", "0", "-g", "16",
				"-pix_fmt", "yuv420p", "-c:s", "copy", "-cluster_time_limit", "250", "-t", "12", source)
			progressiveVideoCommand(t, ctx, ffmpeg, args...)
			assertLiveBitmapSourceEventClock(t, ctx, ffprobe, source, cue)
			data, err := os.ReadFile(source)
			if err != nil || len(data) > 4<<20 {
				t.Fatal("unbounded live bitmap fixture", err)
			}
			chunks := liveBitmapClusters(t, data)
			p := Plan{SourceMode: "stream", Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1,
				Width: 320, Height: 192, FrameRate: 16, SegmentSeconds: 1,
				Subtitle: SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: 1}}
			if cue {
				p.AudioStreamIndex, p.Subtitle.StreamIndex = 1, 2
			}
			graph, output, err := BitmapSubtitleGraph(p, "format=yuv420p")
			if err != nil {
				t.Fatal(err)
			}
			graph += ";" + output + "format=gray,showinfo[measured]"
			var reads, writes []*os.File
			for range 2 {
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				reads, writes = append(reads, reader), append(writes, writer)
			}
			for _, file := range append(reads, writes...) {
				defer file.Close()
			}
			unused, err := os.Open(os.DevNull)
			if err != nil {
				t.Fatal(err)
			}
			defer unused.Close()
			args = []string{"-hide_banner", "-v", "info", "-nostdin", "-nostats", "-copyts", "-filter_threads", "1", "-filter_complex_threads", "1",
				"-threads", "1", "-probesize", "32768", "-analyzeduration", "1000000", "-itsoffset", signedTickSeconds(LiveSourceClockBiasTicks(p)), "-i", "pipe:3",
				"-probesize", "32768", "-analyzeduration", "1000000"}
			args = appendLiveBitmapInputArgs(args, p, 1)
			args = append(args, "-filter_complex", graph, "-map", "[measured]", "-an", "-sn", "-dn", "-c:v", "rawvideo", "-threads:v", "1",
				"-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
			args = appendLiveBitmapHeartbeatOutputArgs(args, p)
			var stderr bytes.Buffer
			pixels := &liveBitmapPixels{}
			command := exec.CommandContext(ctx, ffmpeg, args...)
			// FD 4/5 are reserved for the product's media clock and journal.
			command.ExtraFiles = []*os.File{reads[0], unused, unused, reads[1]}
			command.Stdout, command.Stderr = pixels, &stderr
			command.WaitDelay = 2 * time.Second
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					cancel()
					_ = command.Wait()
				}
			}()
			for _, reader := range reads {
				_ = reader.Close()
			}
			feedErrors := make(chan error, 2)
			// Broadcast each bounded stage to distinct pipes; consumers must
			// never compete for bytes from the same read descriptor.
			feed := func(payload []byte) {
				var group sync.WaitGroup
				for _, writer := range writes {
					group.Add(1)
					go func() {
						defer group.Done()
						_, err := writer.Write(payload)
						if err != nil {
							feedErrors <- err
						}
					}()
				}
				group.Wait()
				select {
				case err := <-feedErrors:
					t.Fatal(err)
				default:
				}
			}
			position := 0
			feedUntil := func(end float64) {
				var payload []byte
				for position < len(chunks) && chunks[position].time <= end {
					payload = append(payload, chunks[position].data...)
					position++
				}
				feed(payload)
			}
			feedUntil(2)
			if !pixels.wait(ctx, 16) {
				t.Fatal("initial transparent bitmap state stalled before future cues were supplied")
			}
			feedUntil(9)
			if !pixels.wait(ctx, 128) {
				t.Fatal("bitmap clear or quiet interval stalled before source EOF")
			}
			feedUntil(math.Inf(1))
			for _, writer := range writes {
				_ = writer.Close()
			}
			err = command.Wait()
			waited = true
			if err != nil {
				t.Fatalf("live bitmap command failed: %v: %s", err, stderr.String())
			}
			pixels.mu.Lock()
			frames, pending := append([]liveBitmapFrame(nil), pixels.frames...), len(pixels.pending)
			pixels.mu.Unlock()
			pts := regexp.MustCompile(`\bn:\s*\d+\s+pts:.*?pts_time:([-0-9.]+)`).FindAllStringSubmatch(stderr.String(), -1)
			if len(frames) != 192 || len(pts) != 192 || pending != 0 {
				t.Fatalf("live heartbeat changed frame count: pixels=%d pts=%d pending=%d", len(frames), len(pts), pending)
			}
			for index, frame := range frames {
				clock, err := strconv.ParseFloat(pts[index][1], 64)
				if err != nil || math.Abs(clock-float64(LiveSourceClockBiasTicks(p))/float64(ticksPerSecond)-float64(index)/16) > .00051 {
					t.Fatalf("heartbeat moved source frame %d: %s", index, pts[index][1])
				}
				visible := cue && index >= 96 && index < 112
				if visible && frame.inside < 2000 || !visible && frame.inside > 4 || frame.outside > 4 {
					t.Fatalf("bitmap visibility/clear mismatch at %d: %+v", index, frame)
				}
			}
		})
	}
}

func assertLiveBitmapSourceEventClock(t *testing.T, ctx context.Context, ffprobe, source string, cue bool) {
	t.Helper()
	data := progressiveVideoCommand(t, ctx, ffprobe, "-v", "error", "-select_streams", "s:0", "-show_packets",
		"-show_entries", "packet=pts_time", "-of", "json", source)
	var document struct {
		Packets []struct {
			PTS string "json:\"pts_time\""
		} "json:\"packets\""
	}
	if err := json.Unmarshal(data, &document); err != nil || len(document.Packets) == 0 {
		t.Fatal("live bitmap fixture has no independently observed subtitle packet clock", err)
	}
	wantFirst, wantLast := 0.0, 0.0
	if cue {
		wantFirst, wantLast = 6, 7
	}
	first, firstErr := strconv.ParseFloat(document.Packets[0].PTS, 64)
	last, lastErr := strconv.ParseFloat(document.Packets[len(document.Packets)-1].PTS, 64)
	if firstErr != nil || lastErr != nil || math.Abs(first-wantFirst) > .000001 || math.Abs(last-wantLast) > .000001 {
		t.Fatalf("fixture muxing changed its authored PGS event origin: first=%q last=%q", document.Packets[0].PTS, document.Packets[len(document.Packets)-1].PTS)
	}
}

func liveBitmapPGSFixture(cue bool) []byte {
	data := bitmapSubtitlePGSFixture()
	var result []byte
	for len(data) >= 13 {
		size := 13 + int(binary.BigEndian.Uint16(data[11:13]))
		packet := append([]byte(nil), data[:size]...)
		pts := binary.BigEndian.Uint32(packet[2:6])
		if cue && pts != 0 || !cue && pts == 0 {
			if cue {
				if pts == 225000 {
					pts = 6 * 90000
				} else {
					pts = 7 * 90000
				}
				binary.BigEndian.PutUint32(packet[2:6], pts)
				binary.BigEndian.PutUint32(packet[6:10], pts)
			}
			result = append(result, packet...)
		}
		data = data[size:]
	}
	return result
}

type liveBitmapChunk struct {
	time float64
	data []byte
}
type liveBitmapElement struct {
	tag                 uint64
	start, payload, end int
}

func liveBitmapVInt(t *testing.T, data []byte, position int, identifier bool) (uint64, int) {
	t.Helper()
	if position >= len(data) || data[position] == 0 {
		t.Fatal("invalid fixture EBML integer")
	}
	width, marker := 1, byte(128)
	for data[position]&marker == 0 {
		width++
		marker >>= 1
	}
	if width > 8 || position+width > len(data) {
		t.Fatal("truncated fixture EBML integer")
	}
	value := uint64(data[position])
	if !identifier {
		value &= uint64(marker - 1)
	}
	for index := 1; index < width; index++ {
		value = value<<8 | uint64(data[position+index])
	}
	return value, width
}

func liveBitmapElements(t *testing.T, data []byte, start, end int) []liveBitmapElement {
	t.Helper()
	var result []liveBitmapElement
	for start < end {
		tag, a := liveBitmapVInt(t, data, start, true)
		size, b := liveBitmapVInt(t, data, start+a, false)
		payload := start + a + b
		if size > uint64(end-payload) {
			t.Fatal("unbounded fixture EBML element")
		}
		result = append(result, liveBitmapElement{tag, start, payload, payload + int(size)})
		start = payload + int(size)
	}
	return result
}

func liveBitmapClusters(t *testing.T, data []byte) []liveBitmapChunk {
	t.Helper()
	var clusters []liveBitmapElement
	for _, root := range liveBitmapElements(t, data, 0, len(data)) {
		if root.tag != 0x18538067 {
			continue
		}
		for _, element := range liveBitmapElements(t, data, root.payload, root.end) {
			if element.tag == 0x1f43b675 {
				clusters = append(clusters, element)
			}
		}
	}
	if len(clusters) < 20 {
		t.Fatal("fixture lacks independently bounded media clusters")
	}
	result := []liveBitmapChunk{{0, data[:clusters[0].start]}}
	for index, cluster := range clusters {
		children := liveBitmapElements(t, data, cluster.payload, cluster.end)
		clock := int64(-1)
		for _, child := range children {
			if child.tag == 0xe7 {
				clock = 0
				for _, octet := range data[child.payload:child.end] {
					clock = clock<<8 | int64(octet)
				}
			}
		}
		if clock < 0 {
			t.Fatal("fixture cluster lacks its media clock")
		}
		last := clock
		for _, child := range children {
			blocks := []liveBitmapElement{child}
			if child.tag == 0xa0 {
				blocks = liveBitmapElements(t, data, child.payload, child.end)
			}
			for _, block := range blocks {
				if block.tag != 0xa3 && block.tag != 0xa1 {
					continue
				}
				_, width := liveBitmapVInt(t, data, block.payload, false)
				if block.payload+width+2 > block.end {
					t.Fatal("fixture block lacks its timestamp")
				}
				last = max(last, clock+int64(int16(binary.BigEndian.Uint16(data[block.payload+width:]))))
			}
		}
		end := len(data)
		if index+1 < len(clusters) {
			end = clusters[index+1].start
		}
		result = append(result, liveBitmapChunk{float64(last) / 1000, data[cluster.start:end]})
	}
	return result
}

type liveBitmapFrame struct{ inside, outside int }
type liveBitmapPixels struct {
	mu      sync.Mutex
	pending []byte
	frames  []liveBitmapFrame
}

func (pixels *liveBitmapPixels) Write(data []byte) (int, error) {
	pixels.mu.Lock()
	defer pixels.mu.Unlock()
	pixels.pending = append(pixels.pending, data...)
	for len(pixels.pending) >= 320*192 {
		var measured liveBitmapFrame
		for index, value := range pixels.pending[:320*192] {
			if value <= 180 {
				continue
			}
			x, y := index%320, index/320
			if x >= 112 && x < 208 && y >= 144 && y < 168 {
				measured.inside++
			} else if x < 108 || x >= 212 || y < 140 || y >= 172 {
				measured.outside++
			}
		}
		pixels.frames = append(pixels.frames, measured)
		pixels.pending = pixels.pending[320*192:]
	}
	return len(data), nil
}

func (pixels *liveBitmapPixels) wait(ctx context.Context, count int) bool {
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		pixels.mu.Lock()
		ready := len(pixels.frames) >= count
		pixels.mu.Unlock()
		if ready {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}

var _ io.Writer = (*liveBitmapPixels)(nil)
