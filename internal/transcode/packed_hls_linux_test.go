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
			if publisher.manifestPublished || publisher.lastRender != (packedHLSRenderState{}) {
				t.Fatal("an open first segment acquired a successful manifest marker")
			}

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

func TestPackedHLSUnchangedPollsSkipManifestRendering(t *testing.T) {
	for _, count := range []int{10, 100, 1000} {
		t.Run(fmt.Sprintf("segments-%d", count), func(t *testing.T) {
			publisher, _ := packedTestPendingPublisher(t, "aac", count)
			original := packedTestRead(t, publisher.directory, "main.m3u8")
			before, err := os.Stat(filepath.Join(publisher.directory, "main.m3u8"))
			if err != nil {
				t.Fatal(err)
			}
			state := publisher.lastRender
			packedTestWrite(t, publisher.directory, "main.m3u8.publish.tmp", []byte("unchanged polls must not publish"))
			for range 3 {
				if err := publisher.publish(false); err != nil {
					t.Fatalf("unchanged poll: %v", err)
				}
				if output, err := publisher.renderManifest(state); err != nil || output != "" {
					t.Fatalf("unchanged inputs still rendered a manifest: bytes=%d error=%v", len(output), err)
				}
			}
			after, err := os.Stat(filepath.Join(publisher.directory, "main.m3u8"))
			if err != nil || !os.SameFile(before, after) || publisher.lastRender != state || len(publisher.published) != count {
				t.Fatalf("unchanged polls replaced the manifest or advanced publication: state=%+v error=%v", publisher.lastRender, err)
			}
			if data := packedTestRead(t, publisher.directory, "main.m3u8"); !bytes.Equal(data, original) {
				t.Fatal("unchanged polls changed the manifest contents")
			}
			packedTestAbsent(t, publisher.directory, fmt.Sprintf("segment-%06d.aac", count))
		})
	}
}

func TestPackedHLSUnchangedListObservesNewClosureEvidence(t *testing.T) {
	for _, extension := range []string{"aac", "mp3"} {
		for _, symlink := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/symlink-%t", extension, symlink), func(t *testing.T) {
				publisher, private := packedTestPendingPublisher(t, extension, 1)
				original := packedTestRead(t, publisher.directory, "main.m3u8")
				state := publisher.lastRender
				next := "segment-000002." + extension + ".tmp"
				if symlink {
					if err := os.Symlink(filepath.Join(publisher.directory, "segment-000001."+extension+".tmp"), filepath.Join(publisher.directory, next)); err != nil {
						t.Fatal(err)
					}
				} else {
					packedTestWrite(t, publisher.directory, next, packedTestAudio(extension))
				}
				err := publisher.publish(false)
				if symlink {
					if err == nil || publisher.lastRender != state || len(publisher.published) != 1 {
						t.Fatalf("unchanged private bytes bypassed unsafe closure evidence: state=%+v error=%v", publisher.lastRender, err)
					}
					if data := packedTestRead(t, publisher.directory, "main.m3u8"); !bytes.Equal(data, original) {
						t.Fatal("unsafe closure evidence changed the manifest")
					}
					packedTestAbsent(t, publisher.directory, "segment-000001."+extension)
				} else {
					list, parseErr := ParseMediaPlaylist(packedTestRead(t, publisher.directory, "main.m3u8"))
					if err != nil || parseErr != nil || len(list.Segments) != 2 || list.Ended || publisher.lastRender.segments != 2 {
						t.Fatalf("new closure did not publish the next segment: list=%+v error=%v parse=%v", list, err, parseErr)
					}
					packedTestSegment(t, publisher.directory, "segment-000001."+extension, packedTestAudio(extension), 90_000)
				}
				if data := packedTestRead(t, publisher.directory, "segment-list.m3u8"); !bytes.Equal(data, private) {
					t.Fatal("closure fixture unexpectedly changed the private playlist")
				}
			})
		}
	}
}

func TestPackedHLSRenderStateTracksTargetDurationAndSuccessfulEnd(t *testing.T) {
	for _, extension := range []string{"aac", "mp3"} {
		t.Run(extension, func(t *testing.T) {
			publisher, private := packedTestPendingPublisher(t, extension, 1)
			packedTestWrite(t, publisher.directory, "segment-000002."+extension+".tmp", packedTestAudio(extension))
			if err := publisher.publish(false); err != nil {
				t.Fatal(err)
			}
			private = bytes.Replace(private, []byte("#EXT-X-TARGETDURATION:3"), []byte("#EXT-X-TARGETDURATION:4"), 1)
			packedTestWrite(t, publisher.directory, "segment-list.m3u8", private)
			if err := publisher.publish(false); err != nil {
				t.Fatal(err)
			}
			partial := packedTestRead(t, publisher.directory, "main.m3u8")
			list, err := ParseMediaPlaylist(partial)
			if err != nil || list.TargetDuration != 4 || list.Ended || len(list.Segments) != 2 || publisher.lastRender.targetDuration != 4 {
				t.Fatalf("unchanged segment count lost the new target duration: list=%+v error=%v", list, err)
			}
			before, err := os.Stat(filepath.Join(publisher.directory, "main.m3u8"))
			if err != nil {
				t.Fatal(err)
			}
			packedTestWrite(t, publisher.directory, "segment-list.m3u8", append(private, []byte("#EXT-X-ENDLIST\n")...))
			if err := publisher.publish(false); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(filepath.Join(publisher.directory, "main.m3u8"))
			if err != nil || !os.SameFile(before, after) || publisher.lastRender.ended {
				t.Fatalf("private ENDLIST changed the manifest before successful completion: %v", err)
			}
			if data := packedTestRead(t, publisher.directory, "main.m3u8"); !bytes.Equal(data, partial) {
				t.Fatal("private ENDLIST escaped before successful completion")
			}
			if err := publisher.publish(true); err != nil {
				t.Fatal(err)
			}
			list, err = ParseMediaPlaylist(packedTestRead(t, publisher.directory, "main.m3u8"))
			if err != nil || !list.Ended || list.TargetDuration != 4 || len(list.Segments) != 2 || !publisher.lastRender.ended {
				t.Fatalf("successful completion did not publish ENDLIST at the same segment count: list=%+v error=%v", list, err)
			}
			packedTestWrite(t, publisher.directory, "main.m3u8.publish.tmp", []byte("repeat completion must not publish"))
			if err := publisher.publish(true); err != nil {
				t.Fatalf("repeat completion rendered or published again: %v", err)
			}
		})
	}
}

func TestPackedHLSFailedCompletionRetainsPublishedState(t *testing.T) {
	publisher, private := packedTestPendingPublisher(t, "aac", 1)
	original := packedTestRead(t, publisher.directory, "main.m3u8")
	state := publisher.lastRender
	failure := publisher.publish(true)
	if !errors.Is(failure, ErrInvalidPlaylist) || publisher.lastRender != state || !publisher.manifestPublished || len(publisher.published) != 1 {
		t.Fatalf("completion without ENDLIST changed publication: state=%+v error=%v", publisher.lastRender, failure)
	}
	packedTestWrite(t, publisher.directory, "segment-list.m3u8", append(private, []byte("#EXT-X-ENDLIST\n")...))
	if err := publisher.publish(true); err != failure {
		t.Fatalf("a later ENDLIST cleared the completion failure: %v", err)
	}
	if data := packedTestRead(t, publisher.directory, "main.m3u8"); !bytes.Equal(data, original) {
		t.Fatal("failed completion changed the last successful manifest")
	}
	packedTestAbsent(t, publisher.directory, "segment-000001.aac")
}

func TestPackedHLSManifestFailureDoesNotAdvanceRenderState(t *testing.T) {
	for _, change := range []string{"first publication", "target duration", "completed end"} {
		t.Run(change, func(t *testing.T) {
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: packedTestPlan("aac")}
			private := []byte(packedTestPlaylist("#EXTINF:1.000000,\nsegment-000000.aac.tmp\n"))
			packedTestWrite(t, directory, "segment-000000.aac.tmp", packedTestAudio("aac"))
			packedTestWrite(t, directory, "segment-000001.aac.tmp", packedTestAudio("aac"))
			packedTestWrite(t, directory, "segment-list.m3u8", private)
			var original []byte
			if change != "first publication" {
				if err := publisher.publish(false); err != nil {
					t.Fatal(err)
				}
				original = packedTestRead(t, directory, "main.m3u8")
			}
			state, published := publisher.lastRender, publisher.manifestPublished
			finished := change == "completed end"
			if finished {
				private = append(private, []byte("#EXT-X-ENDLIST\n")...)
			} else if change == "target duration" {
				private = bytes.Replace(private, []byte("#EXT-X-TARGETDURATION:3"), []byte("#EXT-X-TARGETDURATION:4"), 1)
			}
			packedTestWrite(t, directory, "segment-list.m3u8", private)
			packedTestWrite(t, directory, "main.m3u8.publish.tmp", []byte("publication obstruction"))
			failure := publisher.publish(finished)
			if failure == nil || publisher.err != failure || publisher.lastRender != state || publisher.manifestPublished != published {
				t.Fatalf("failed publication acquired a successful render marker: state=%+v published=%t error=%v", publisher.lastRender, publisher.manifestPublished, failure)
			}
			if len(publisher.published) != 1 {
				t.Fatal("fixture did not reach manifest publication after publishing its segment")
			}
			if err := os.Remove(filepath.Join(directory, "main.m3u8.publish.tmp")); err != nil {
				t.Fatal(err)
			}
			for _, complete := range []bool{false, true} {
				if err := publisher.publish(complete); err != failure {
					t.Fatalf("publication failure was not sticky: complete=%t error=%v", complete, err)
				}
			}
			if published {
				if data := packedTestRead(t, directory, "main.m3u8"); !bytes.Equal(data, original) {
					t.Fatal("failed publication changed the last successful manifest")
				}
			} else {
				packedTestAbsent(t, directory, "main.m3u8")
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
		"duration mutation":       strings.Replace(first, "1.024000", "1.025000", 1) + second,
		"later duration mutation": first + strings.Replace(second, "0.976000", "0.977000", 1),
		"discontinuity mutation":  "#EXT-X-DISCONTINUITY\n" + first + second,
		"segment rollback":        first,
		"empty rollback":          "",
		"name mutation":           strings.Replace(first, "000000", "000001", 1) + second,
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
			state := publisher.lastRender
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist(entries)))
			failure := publisher.publish(false)
			if !errors.Is(failure, ErrInvalidPlaylist) || publisher.lastRender != state || !publisher.manifestPublished {
				t.Fatalf("published playlist history changed or acquired a render marker: state=%+v error=%v", publisher.lastRender, failure)
			}
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist(first+second)+"#EXT-X-ENDLIST\n"))
			if err := publisher.publish(true); err != failure {
				t.Fatalf("restored history cleared the sticky publication failure: %v", err)
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

// This benchmark isolates formatting and its unchanged-state guard. Private-list
// parsing, source validation, and filesystem publication are intentionally absent.
func BenchmarkPackedHLSManifestRendering(b *testing.B) {
	for _, count := range []int{10, 100, 1000} {
		segments := make([]MediaSegment, count)
		for number := range segments {
			segments[number] = MediaSegment{Number: int64(number), Name: fmt.Sprintf("segment-%06d.aac", number), DurationTicks: 10_240_000}
		}
		state := packedHLSRenderState{segments: count, targetDuration: 3}
		for _, unchanged := range []bool{false, true} {
			b.Run(fmt.Sprintf("segments-%d/unchanged-%t", count, unchanged), func(b *testing.B) {
				publisher := &packedHLSPublisher{plan: packedTestPlan("aac"), published: segments, lastRender: state, manifestPublished: unchanged}
				b.ReportAllocs()
				b.ResetTimer()
				for index := 0; index < b.N; index++ {
					output, err := publisher.renderManifest(state)
					if err != nil || (output == "") != unchanged {
						b.Fatalf("manifest rendering: bytes=%d error=%v", len(output), err)
					}
					packedHLSBenchmarkManifest = output
				}
			})
		}
	}
}

var packedHLSBenchmarkManifest string

func packedTestPendingPublisher(t *testing.T, extension string, closed int) (*packedHLSPublisher, []byte) {
	t.Helper()
	directory := t.TempDir()
	plan := packedTestPlan(extension)
	plan.DurationTicks = int64(closed+2) * ticksPerSecond
	publisher := &packedHLSPublisher{directory: directory, plan: plan}
	var entries strings.Builder
	for number := 0; number <= closed; number++ {
		name := fmt.Sprintf("segment-%06d.%s", number, extension)
		packedTestWrite(t, directory, name+".tmp", packedTestAudio(extension))
		fmt.Fprintf(&entries, "#EXTINF:1.000000,\n%s.tmp\n", name)
	}
	private := []byte(packedTestPlaylist(entries.String()))
	packedTestWrite(t, directory, "segment-list.m3u8", private)
	if err := publisher.publish(false); err != nil {
		t.Fatal(err)
	}
	if len(publisher.published) != closed || !publisher.manifestPublished || publisher.lastRender != (packedHLSRenderState{segments: closed, targetDuration: 3}) {
		t.Fatalf("pending segment fixture did not publish its closed prefix: segments=%d state=%+v", len(publisher.published), publisher.lastRender)
	}
	return publisher, private
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
