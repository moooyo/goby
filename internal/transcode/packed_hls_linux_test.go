//go:build linux

package transcode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackedHLSPublicationWaitsForClosedSegments(t *testing.T) {
	for _, extension := range []string{"aac", "mp3"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			plan := packedTestPlan(extension)
			plan.StartTicks, plan.SegmentStartNumber = 12_500_000, 7
			publisher := &packedHLSPublisher{directory: directory, plan: plan}
			payload := packedTestAudio(extension)
			first, second := "segment-000007."+extension, "segment-000008."+extension
			private := packedTestPlaylist("#EXTINF:1.024000,\n" + first + ".tmp\n")
			packedTestWrite(t, directory, first+".tmp", payload)
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(private))
			if err := publisher.publish(false); err != nil {
				t.Fatal(err)
			}
			packedTestAbsent(t, directory, "main.m3u8")
			packedTestAbsent(t, directory, first)

			packedTestWrite(t, directory, second+".tmp", payload)
			if err := publisher.publish(false); err != nil {
				t.Fatal(err)
			}
			partial := packedTestRead(t, directory, "main.m3u8")
			list, err := ParseMediaPlaylist(partial)
			if err != nil || list.Sequence != 7 || list.Type != "EVENT" || list.Ended || len(list.Segments) != 1 || list.Segments[0].DurationTicks != 10_240_000 {
				t.Fatalf("partial publication: %+v, %v", list, err)
			}
			packedTestSegment(t, directory, first, payload, 112_500)
			oldPlaylist, err := os.Open(filepath.Join(directory, "main.m3u8"))
			if err != nil {
				t.Fatal(err)
			}
			defer oldPlaylist.Close()

			private += "#EXTINF:0.576000,\n" + second + ".tmp\n#EXT-X-ENDLIST\n"
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(private))
			if err := publisher.publish(false); err != nil {
				t.Fatal(err)
			}
			packedTestAbsent(t, directory, second)
			if data := packedTestRead(t, directory, "main.m3u8"); !bytes.Equal(data, partial) {
				t.Fatalf("ENDLIST published before process exit: %s", data)
			}
			if err := publisher.publish(true); err != nil {
				t.Fatal(err)
			}
			complete := packedTestRead(t, directory, "main.m3u8")
			list, err = ParseMediaPlaylist(complete)
			if err != nil || list.Sequence != 7 || list.Type != "EVENT" || !list.Ended || len(list.Segments) != 2 {
				t.Fatalf("complete publication: %+v, %v", list, err)
			}
			if list.Segments[0].DurationTicks != 10_240_000 || list.Segments[1].DurationTicks != 5_760_000 {
				t.Fatalf("measured durations changed: %+v", list.Segments)
			}
			for _, segment := range list.Segments {
				if segment.Discontinuity {
					t.Fatalf("continuous packed audio gained a discontinuity: %+v", segment)
				}
			}
			packedTestSegment(t, directory, second, payload, 204_660)
			oldData, err := io.ReadAll(oldPlaylist)
			if err != nil || !bytes.Equal(oldData, partial) {
				t.Fatalf("playlist was modified in place: %v: %s", err, oldData)
			}
			if err := publisher.publish(true); err != nil {
				t.Fatalf("repeat completion: %v", err)
			}
			if data := packedTestRead(t, directory, "main.m3u8"); !bytes.Equal(data, complete) {
				t.Fatalf("repeat publication changed the playlist: %s", data)
			}
		})
	}
}

func TestPackedHLSTimestampsAccumulateBeforeConversionAndWrap(t *testing.T) {
	directory := t.TempDir()
	plan := packedTestPlan("aac")
	plan.StartTicks = 100_000*ticksPerSecond + 17
	plan.DurationTicks = plan.StartTicks + 60*ticksPerSecond
	publisher := &packedHLSPublisher{directory: directory, plan: plan}
	var entries strings.Builder
	for number := range 3 {
		name := fmt.Sprintf("segment-%06d.aac", number)
		packedTestWrite(t, directory, name+".tmp", packedTestAudio("aac"))
		fmt.Fprintf(&entries, "#EXTINF:1.000010,\n%s.tmp\n", name)
	}
	packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist(entries.String())+"#EXT-X-ENDLIST\n"))
	if err := publisher.publish(true); err != nil {
		t.Fatal(err)
	}
	for number := range 3 {
		start := plan.StartTicks + int64(number)*10_000_100
		want := uint64(start*9/1000) & ((1 << 33) - 1)
		packedTestSegment(t, directory, fmt.Sprintf("segment-%06d.aac", number), packedTestAudio("aac"), want)
	}
}

func TestPackedHLSCompletionRequiresEndList(t *testing.T) {
	for _, haveList := range []bool{false, true} {
		t.Run(fmt.Sprintf("have-list-%t", haveList), func(t *testing.T) {
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan("aac")}
			if haveList {
				packedTestWrite(t, directory, "segment-000000.aac.tmp", packedTestAudio("aac"))
				packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist("#EXTINF:1.000000,\nsegment-000000.aac.tmp\n")))
			}
			if err := publisher.publish(false); err != nil {
				t.Fatalf("unfinished input: %v", err)
			}
			if err := publisher.publish(true); err == nil {
				t.Fatal("completion without ENDLIST was accepted")
			}
			packedTestAbsent(t, directory, "main.m3u8")
			packedTestAbsent(t, directory, "segment-000000.aac")
		})
	}
}

func TestPackedHLSRejectsUnsafePrivateSegmentNames(t *testing.T) {
	for _, name := range []string{
		"segment-000000.aac", "segment-000000.aac.tmp.tmp", "segment-0.aac.tmp",
		"segment-000000.mp3.tmp", "segment-000000.ts.tmp", "segment-000001.aac.tmp",
		"v0-segment-000000.aac.tmp", "../segment-000000.aac.tmp", "./segment-000000.aac.tmp",
		"/segment-000000.aac.tmp", "https://example.invalid/segment-000000.aac.tmp",
		"segment-000000.aac.tmp?query=1", "segment-000000.aac.tmp#fragment",
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan("aac")}
			packedTestWrite(t, directory, "segment-000000.aac.tmp", packedTestAudio("aac"))
			if strings.HasSuffix(name, ".tmp") && !strings.ContainsAny(name, "/\\") {
				payload := packedTestAudio("aac")
				if strings.HasSuffix(name, ".mp3.tmp") {
					payload = packedTestAudio("mp3")
				}
				packedTestWrite(t, directory, name, payload)
			}
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist("#EXTINF:1.000000,\n"+name+"\n")+"#EXT-X-ENDLIST\n"))
			if err := publisher.publish(true); err == nil {
				t.Fatalf("unsafe private name was accepted: %q", name)
			}
			packedTestAbsent(t, directory, "main.m3u8")
			packedTestAbsent(t, directory, "segment-000000.aac")
		})
	}
}

func TestPackedHLSRejectsMissingOrWrongAudioHeaders(t *testing.T) {
	for _, extension := range []string{"aac", "mp3"} {
		other := "aac"
		if extension == "aac" {
			other = "mp3"
		}
		for name, payload := range map[string][]byte{
			"empty": nil, "text": []byte("not an audio segment"), "short sync": {0xff, 0xf1},
			"wrong codec": packedTestAudio(other),
		} {
			t.Run(extension+"/"+name, func(t *testing.T) {
				directory := t.TempDir()
				publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan(extension)}
				segment := "segment-000000." + extension
				packedTestWrite(t, directory, segment+".tmp", payload)
				packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist("#EXTINF:1.000000,\n"+segment+".tmp\n")+"#EXT-X-ENDLIST\n"))
				if err := publisher.publish(true); err == nil {
					t.Fatal("invalid audio payload was published")
				}
				packedTestAbsent(t, directory, "main.m3u8")
				packedTestAbsent(t, directory, segment)
			})
		}
	}
}

func TestPackedHLSRejectsPrivateSymlinks(t *testing.T) {
	for _, linkedName := range []string{"segment-list.m3u8", "segment-000000.aac.tmp", "segment-000001.aac.tmp"} {
		t.Run(linkedName, func(t *testing.T) {
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan("aac")}
			private := []byte(packedTestPlaylist("#EXTINF:1.000000,\nsegment-000000.aac.tmp\n"))
			for name, data := range map[string][]byte{
				"segment-list.m3u8": private, "segment-000000.aac.tmp": packedTestAudio("aac"),
				"segment-000001.aac.tmp": packedTestAudio("aac"),
			} {
				if name != linkedName {
					packedTestWrite(t, directory, name, data)
					continue
				}
				target := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(target, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(directory, name)); err != nil {
					t.Fatal(err)
				}
			}
			if err := publisher.publish(false); err == nil {
				t.Fatalf("private symlink was accepted: %s", linkedName)
			}
			packedTestAbsent(t, directory, "main.m3u8")
			packedTestAbsent(t, directory, "segment-000000.aac")
		})
	}
}

func TestPackedHLSRejectsPublishedPlaylistChanges(t *testing.T) {
	first := "#EXTINF:1.024000,\nsegment-000000.aac.tmp\n"
	second := "#EXTINF:0.976000,\nsegment-000001.aac.tmp\n"
	for name, entries := range map[string]string{
		"duration mutation": strings.Replace(first, "1.024000", "1.025000", 1) + second,
		"segment rollback":  first,
		"empty rollback":    "",
		"name mutation":     strings.Replace(first, "000000", "000001", 1) + second,
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan("aac")}
			for number := range 3 {
				packedTestWrite(t, directory, fmt.Sprintf("segment-%06d.aac.tmp", number), packedTestAudio("aac"))
			}
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist(first+second)))
			if err := publisher.publish(false); err != nil {
				t.Fatal(err)
			}
			original := packedTestRead(t, directory, "main.m3u8")
			list, err := ParseMediaPlaylist(original)
			if err != nil || len(list.Segments) != 2 {
				t.Fatalf("initial publication: %+v, %v", list, err)
			}
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist(entries)))
			if err := publisher.publish(false); err == nil {
				t.Fatal("published playlist history was changed")
			}
			if data := packedTestRead(t, directory, "main.m3u8"); !bytes.Equal(data, original) {
				t.Fatalf("rejected private list changed the public playlist: %s", data)
			}
			packedTestSegment(t, directory, "segment-000000.aac", packedTestAudio("aac"), 0)
			packedTestSegment(t, directory, "segment-000001.aac", packedTestAudio("aac"), 92_160)
		})
	}
}

func TestPackedHLSPreservesLeadingID3Metadata(t *testing.T) {
	for _, extension := range []string{"aac", "mp3"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan(extension)}
			segment := "segment-000000." + extension
			payload := append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}, packedTestAudio(extension)...)
			packedTestWrite(t, directory, segment+".tmp", payload)
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist("#EXTINF:1.000000,\n"+segment+".tmp\n")+"#EXT-X-ENDLIST\n"))
			if err := publisher.publish(true); err != nil {
				t.Fatalf("valid leading metadata: %v", err)
			}
			packedTestSegment(t, directory, segment, payload, 0)
			list, err := ParseMediaPlaylist(packedTestRead(t, directory, "main.m3u8"))
			if err != nil || list.Type != "EVENT" || !list.Ended || len(list.Segments) != 1 {
				t.Fatalf("metadata publication: %+v, %v", list, err)
			}
		})
	}
}

func TestPackedHLSRejectsTruncatedSecondFrame(t *testing.T) {
	for _, extension := range []string{"aac", "mp3"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan(extension)}
			segment := "segment-000000." + extension
			frame := packedTestAudio(extension)
			payload := append(append([]byte(nil), frame...), frame[:len(frame)-1]...)
			packedTestWrite(t, directory, segment+".tmp", payload)
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist("#EXTINF:1.000000,\n"+segment+".tmp\n")+"#EXT-X-ENDLIST\n"))
			if err := publisher.publish(true); err == nil {
				t.Fatal("complete first frame hid a truncated second frame")
			}
			packedTestAbsent(t, directory, "main.m3u8")
			packedTestAbsent(t, directory, segment)
		})
	}
}

func TestPackedHLSRejectsOversizedSparseSource(t *testing.T) {
	for _, extension := range []string{"aac", "mp3"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan(extension)}
			segment := "segment-000000." + extension
			packedTestWrite(t, directory, segment+".tmp", packedTestAudio(extension))
			if err := os.Truncate(filepath.Join(directory, segment+".tmp"), (64<<20)+1); err != nil {
				t.Fatal(err)
			}
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist("#EXTINF:1.000000,\n"+segment+".tmp\n")+"#EXT-X-ENDLIST\n"))
			if err := publisher.publish(true); err == nil {
				t.Fatal("packed source larger than 64 MiB was accepted")
			}
			packedTestAbsent(t, directory, "main.m3u8")
			packedTestAbsent(t, directory, segment)
		})
	}
}

func TestPackedHLSRejectsPublicationStagingSymlinks(t *testing.T) {
	for _, extension := range []string{"aac", "mp3"} {
		segment := "segment-000000." + extension
		for _, stageName := range []string{segment + ".publish.tmp", "main.m3u8.publish.tmp"} {
			t.Run(extension+"/"+stageName, func(t *testing.T) {
				directory := t.TempDir()
				publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan(extension)}
				packedTestWrite(t, directory, segment+".tmp", packedTestAudio(extension))
				packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist("#EXTINF:1.000000,\n"+segment+".tmp\n")+"#EXT-X-ENDLIST\n"))
				outside := t.TempDir()
				original := []byte("external publication target must remain unchanged")
				packedTestWrite(t, outside, "target", original)
				target := filepath.Join(outside, "target")
				stage := filepath.Join(directory, stageName)
				if err := os.Symlink(target, stage); err != nil {
					t.Fatal(err)
				}
				if err := publisher.publish(true); err == nil {
					t.Fatalf("publication staging symlink was accepted: %s", stageName)
				}
				if data := packedTestRead(t, outside, "target"); !bytes.Equal(data, original) {
					t.Fatalf("publication overwrote an external target: %q", data)
				}
				if destination, err := os.Readlink(stage); err != nil || destination != target {
					t.Fatalf("publication changed the staging symlink: %q, %v", destination, err)
				}
				packedTestAbsent(t, directory, "main.m3u8")
				if stageName == segment+".publish.tmp" {
					packedTestAbsent(t, directory, segment)
				}
			})
		}
	}
}

func packedTestPlan(extension string) Plan {
	return Plan{
		Container: extension, AudioCodec: extension, VideoStreamIndex: -1, AudioStreamIndex: 0,
		DurationTicks: 60 * ticksPerSecond, SegmentSeconds: 3, HLS: HLSPlan{SegmentType: "packed"},
	}
}

func packedTestPlaylist(entries string) string {
	return "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-ALLOW-CACHE:YES\n#EXT-X-TARGETDURATION:3\n" + entries
}

func packedTestAudio(extension string) []byte {
	if extension == "aac" {
		return []byte{0xff, 0xf1, 0x50, 0x80, 0x01, 0x3f, 0xfc, 0x00, 0x00}
	}
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	return frame
}

func packedTestWrite(t *testing.T, directory, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func packedTestRead(t *testing.T, directory, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func packedTestAbsent(t *testing.T, directory, name string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected public file %s: %v", name, err)
	}
}

func packedTestSegment(t *testing.T, directory, name string, payload []byte, want uint64) {
	t.Helper()
	data := packedTestRead(t, directory, name)
	owner := []byte("com.apple.streaming.transportStreamTimestamp\x00")
	tagSize := 10 + len(owner) + 8
	if len(data) != 10+tagSize+len(payload) || !bytes.Equal(data[:6], []byte{'I', 'D', '3', 4, 0, 0}) {
		t.Fatalf("segment %s lacks a bounded ID3v2.4 timestamp tag: %x", name, data)
	}
	if packedTestSynchsafe(t, data[6:10]) != tagSize || string(data[10:14]) != "PRIV" ||
		packedTestSynchsafe(t, data[14:18]) != len(owner)+8 || data[18] != 0 || data[19] != 0 ||
		!bytes.Equal(data[20:20+len(owner)], owner) {
		t.Fatalf("segment %s has an invalid PRIV frame: %x", name, data[:10+tagSize])
	}
	timestamp := binary.BigEndian.Uint64(data[20+len(owner) : 28+len(owner)])
	if timestamp != want || timestamp>>33 != 0 {
		t.Fatalf("segment %s timestamp = %d, want %d", name, timestamp, want)
	}
	if !bytes.Equal(data[10+tagSize:], payload) {
		t.Fatalf("segment %s changed its audio payload", name)
	}
}

func packedTestSynchsafe(t *testing.T, data []byte) int {
	t.Helper()
	value := 0
	for _, part := range data {
		if part&0x80 != 0 {
			t.Fatalf("invalid ID3 synchsafe integer: %x", data)
		}
		value = value<<7 | int(part)
	}
	return value
}
