package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

type subtitleTimelineActualFixture struct {
	name          string
	path          string
	codec         string
	origin        int64
	originKnown   bool
	wantIntervals []SubtitleTimelineInterval
}

func TestSubtitleTimelineAvailabilityActualFixtures(t *testing.T) {
	_, _, config, _ := subtitleTimelineLoadActualFixtures(t)
	wrongDigest := []byte(config.FFprobeSHA256)
	if wrongDigest[0] == '0' {
		wrongDigest[0] = '1'
	} else {
		wrongDigest[0] = '0'
	}
	for _, test := range []struct {
		name      string
		digest    string
		available bool
	}{
		{"pinned", config.FFprobeSHA256, true},
		{"inventory", "", true},
		{"mismatched", string(wrongDigest), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			candidate := config
			candidate.FFprobeSHA256 = test.digest
			capabilities, err := SubtitleTimelineAvailability(ctx, candidate)
			if err != nil {
				t.Fatalf("inventory actual FFprobe: %v", err)
			}
			if capabilities.Available != test.available {
				t.Fatalf("actual FFprobe availability=%+v, want available=%t; the real fixture tool must satisfy the FFprobe 9.0.1 profile", capabilities, test.available)
			}
			if test.available {
				if capabilities.Reason != "" || capabilities.FFprobeSHA256 != config.FFprobeSHA256 {
					t.Fatalf("available FFprobe lost its actual admitted identity: %+v", capabilities)
				}
			} else if capabilities.Reason == "" {
				t.Fatal("mismatched FFprobe digest did not explain its unavailable capability")
			}
		})
	}
}

// TestGenerateSubtitleTimelinesActualFixtures is opt-in real bitmap timeline
// acceptance. It reuses the pinned corpus and configuration selected through
// GOBY_BITMAP_OCR_FIXTURES and GOBY_BITMAP_OCR_CONFIG, but consumes only the
// FFprobe identity. No OCR engine or model is required or invoked.
func TestGenerateSubtitleTimelinesActualFixtures(t *testing.T) {
	root, manifest, config, fixtures := subtitleTimelineLoadActualFixtures(t)
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			file, info, before := subtitleTimelineOpenActualFixture(t, ctx, root, manifest, config, fixture)
			var output bytes.Buffer
			summary, err := GenerateSubtitleTimelines(ctx, config, file, info, &output, nil)
			if err != nil {
				t.Fatalf("generate real %s timeline: %v", fixture.codec, err)
			}
			data, err := ParseSubtitleTimelines(output.Bytes())
			if err != nil {
				t.Fatalf("parse generated real timeline: %v", err)
			}
			if data.Profile != SubtitleTimelineProfile || data.FFprobeSHA256 != config.FFprobeSHA256 ||
				data.DurationTicks != manifest.DurationTicks || len(data.Tracks) != 1 {
				t.Fatalf("timeline lost its profile, demuxer provenance, source duration, or track: %+v", data)
			}
			track := data.Tracks[0]
			if track.StreamIndex != info.Streams[0].Index || track.Codec != fixture.codec ||
				track.IntervalCount != len(fixture.wantIntervals) || !slices.Equal(track.Intervals, fixture.wantIntervals) {
				t.Fatalf("real display union changed: track=%+v, want intervals=%+v", track, fixture.wantIntervals)
			}
			if summary.Profile != data.Profile || summary.FFprobeSHA256 != data.FFprobeSHA256 ||
				summary.DurationTicks != data.DurationTicks || summary.Bytes != int64(output.Len()) || len(summary.Tracks) != 1 {
				t.Fatalf("timeline summary differs from its serialized data: %+v", summary)
			}
			trackSummary := summary.Tracks[0]
			if trackSummary.StreamIndex != track.StreamIndex || trackSummary.Codec != track.Codec ||
				trackSummary.IntervalCount != track.IntervalCount || !slices.Equal(trackSummary.Warnings, track.Warnings) {
				t.Fatalf("track summary differs from its serialized data: %+v", trackSummary)
			}
			position, err := file.Seek(0, io.SeekCurrent)
			if err != nil || position != 7 || !subtitleFileUnchanged(file, before) {
				t.Fatal("real timeline generation changed or closed its borrowed source descriptor")
			}
			t.Logf("actual timeline codec=%s ffprobe_sha256=%s intervals=%+v warnings=%v",
				fixture.codec, data.FFprobeSHA256, track.Intervals, track.Warnings)
		})
	}
}

func TestGenerateSubtitleTimelinesActualFixturesCancellation(t *testing.T) {
	root, manifest, config, fixtures := subtitleTimelineLoadActualFixtures(t)
	var selected subtitleTimelineActualFixture
	for _, fixture := range fixtures {
		if fixture.name == "overlap/PGS" {
			selected = fixture
			break
		}
	}
	if selected.path == "" {
		t.Fatal("real cancellation acceptance requires the overlapping PGS fixture")
	}
	for _, test := range []struct {
		name            string
		completedTracks int
	}{
		{"before-track", 0},
		{"after-track", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			file, info, before := subtitleTimelineOpenActualFixture(t, ctx, root, manifest, config, selected)
			var output bytes.Buffer
			canceledAtProgress := false
			_, err := GenerateSubtitleTimelines(ctx, config, file, info, &output, func(progress SubtitleTimelineProgress) {
				if progress.TotalTracks == 1 && progress.CompletedTracks == test.completedTracks {
					canceledAtProgress = true
					cancel()
				}
			})
			if !canceledAtProgress {
				t.Fatalf("generation did not report %d completed real bitmap tracks before publication", test.completedTracks)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled real timeline error=%v, want context.Canceled", err)
			}
			if output.Len() != 0 {
				t.Fatalf("canceled real timeline wrote %d output bytes", output.Len())
			}
			position, err := file.Seek(0, io.SeekCurrent)
			if err != nil || position != 7 || !subtitleFileUnchanged(file, before) {
				t.Fatal("canceled real timeline changed or closed its borrowed source descriptor")
			}
		})
	}
}

func TestGenerateSubtitleTimelinesActualFixturesAllTracksFailure(t *testing.T) {
	root, manifest, config, fixtures := subtitleTimelineLoadActualFixtures(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	file, info, before := subtitleTimelineOpenActualFixture(t, ctx, root, manifest, config, fixtures[0])
	info.Streams = append([]Stream(nil), info.Streams...)
	lastIndex := -1
	for _, stream := range info.Streams {
		lastIndex = max(lastIndex, stream.Index)
	}
	missing := info.Streams[0]
	missing.Index = lastIndex + 1
	// The real source remains unchanged: its admitted first track can decode,
	// but actual FFprobe must reject the additional declared, absent track.
	info.Streams = append(info.Streams, missing)
	var output bytes.Buffer
	completedTracks := 0
	startedMissingTrack := false
	_, err := GenerateSubtitleTimelines(ctx, config, file, info, &output, func(progress SubtitleTimelineProgress) {
		completedTracks = max(completedTracks, progress.CompletedTracks)
		if progress.TotalTracks == 2 && progress.StreamIndex == missing.Index && progress.CompletedTracks == 1 {
			startedMissingTrack = true
		}
	})
	if err == nil {
		t.Fatal("real timeline generation accepted a declared subtitle track absent from the source")
	}
	if ctx.Err() != nil {
		t.Fatalf("absent real subtitle track did not fail before the deadline: %v", err)
	}
	if completedTracks != 1 || !startedMissingTrack {
		t.Fatalf("generation did not finish its real first track before rejecting the absent second track: completed=%d started_missing=%t error=%v", completedTracks, startedMissingTrack, err)
	}
	if output.Len() != 0 {
		t.Fatalf("failed second subtitle track published %d partial output bytes", output.Len())
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 7 || !subtitleFileUnchanged(file, before) {
		t.Fatal("failed all-track generation changed or closed its borrowed source descriptor")
	}
}

func subtitleTimelineLoadActualFixtures(t *testing.T) (*os.Root, subtitleOCRFixtureManifest, BitmapSubtitleConfig, []subtitleTimelineActualFixture) {
	t.Helper()
	directory, configPath := os.Getenv("GOBY_BITMAP_OCR_FIXTURES"), os.Getenv("GOBY_BITMAP_OCR_CONFIG")
	if directory == "" && configPath == "" {
		t.Skip("set GOBY_BITMAP_OCR_FIXTURES and GOBY_BITMAP_OCR_CONFIG for real Linux bitmap timeline acceptance")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("actual bitmap timeline acceptance must run on the authorized Linux verification host")
	}
	if !filepath.IsAbs(directory) || !filepath.IsAbs(configPath) {
		t.Fatal("both fixture directory and bitmap configuration paths must be absolute")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	manifestFile, err := root.Open("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes := subtitleFixtureReadBounded(t, manifestFile, 1<<20)
	if err := manifestFile.Close(); err != nil {
		t.Fatal(err)
	}
	var manifest subtitleOCRFixtureManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != "goby-bitmap-subtitle-fixtures-v2" || manifest.DurationTicks != 97280000 ||
		manifest.Font.GitBlobSHA1 != "dc15562470b4f842321894787a0d066879ccff8b" ||
		manifest.Font.License != "SIL-OFL-1.1" || len(manifest.Font.SHA256) != 64 {
		t.Fatal("fixture manifest does not describe the pinned real bitmap corpus")
	}
	configurationFile, err := os.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configurationBytes := subtitleFixtureReadBounded(t, configurationFile, 64<<10)
	if err := configurationFile.Close(); err != nil {
		t.Fatal(err)
	}
	// Decode only FFprobe fields. The shared configuration may omit OCR engine
	// and model fields entirely, or contain identities unavailable on this host.
	var config BitmapSubtitleConfig
	configurationDecoder := json.NewDecoder(bytes.NewReader(configurationBytes))
	if err := configurationDecoder.Decode(&config); err != nil {
		t.Fatalf("invalid pinned bitmap configuration: %v", err)
	}
	var trailing any
	if err := configurationDecoder.Decode(&trailing); err != io.EOF {
		t.Fatal("bitmap configuration has trailing JSON data")
	}
	if !filepath.IsAbs(config.FFprobePath) || len(config.FFprobeSHA256) != 64 {
		t.Fatal("bitmap configuration requires an absolute FFprobe path and pinned digest")
	}
	// Keep the occupancy oracle independent of the production union function
	// and manifest: adjacent display states and overlapping objects occupy one
	// interval, while the later repeated caption retains its actual gap.
	wantUnions := map[string][]SubtitleTimelineInterval{
		"english":             {{StartTicks: 10240000, EndTicks: 40960000}},
		"chinese-forced":      {{StartTicks: 25600000, EndTicks: 56320000}},
		"chinese-traditional": {{StartTicks: 25600000, EndTicks: 56320000}},
		"overlap": {
			{StartTicks: 10240000, EndTicks: 56320000},
			{StartTicks: 66560000, EndTicks: 87040000},
		},
		"mixed-forced": {{StartTicks: 10240000, EndTicks: 56320000}},
	}
	if len(manifest.Cases) != len(wantUnions) {
		t.Fatal("real bitmap corpus must include every required timing scenario")
	}
	seen := make(map[string]bool)
	var fixtures []subtitleTimelineActualFixture
	for _, fixture := range manifest.Cases {
		want, exists := wantUnions[fixture.Name]
		if !exists || seen[fixture.Name] {
			t.Fatalf("unexpected or duplicate bitmap fixture case %q", fixture.Name)
		}
		seen[fixture.Name] = true
		for _, test := range []struct {
			name, path, codec string
			origin            int64
			originKnown       *bool
			firstPacketPTS    int64
			intervals         []subtitleOCRFixtureInterval
		}{
			{"PGS", fixture.PGSMatroskaFile, "hdmv_pgs_subtitle", fixture.PGSContainerOriginTicks, fixture.PGSFormatStartKnown, 0, fixture.PGSIntervals},
			{"DVD", fixture.DVDMatroskaFile, "dvd_subtitle", fixture.DVDContainerOriginTicks, fixture.DVDFormatStartKnown, fixture.DVDFirstPacketPTSTicks, fixture.DVDIntervals},
		} {
			if test.path == "" {
				if fixture.Name != "mixed-forced" || test.name != "DVD" || len(test.intervals) != 0 {
					t.Fatalf("missing real %s container for %s", test.name, fixture.Name)
				}
				continue // DVD cannot encode mixed forced objects in one display.
			}
			subtitleFixtureRequireCorpusTimeline(t, fixture.Name, test.name, test.origin, test.originKnown, test.firstPacketPTS, test.intervals)
			fixtures = append(fixtures, subtitleTimelineActualFixture{
				name: fixture.Name + "/" + test.name, path: test.path, codec: test.codec,
				origin: test.origin, originKnown: *test.originKnown,
				wantIntervals: append([]SubtitleTimelineInterval(nil), want...),
			})
		}
	}
	return root, manifest, config, fixtures
}

func subtitleTimelineOpenActualFixture(t *testing.T, ctx context.Context, root *os.Root, manifest subtitleOCRFixtureManifest, config BitmapSubtitleConfig, fixture subtitleTimelineActualFixture) (*os.File, Info, os.FileInfo) {
	t.Helper()
	if filepath.Base(fixture.path) != fixture.path {
		t.Fatal("fixture filename is invalid")
	}
	identity, exists := manifest.Files[fixture.path]
	if !exists || identity.Bytes <= 0 || identity.Bytes > 32<<20 || len(identity.SHA256) != 64 {
		t.Fatal("fixture file is not present in the generation manifest")
	}
	file, err := root.Open(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() != identity.Bytes {
		t.Fatal("fixture is not the generated regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.NewSectionReader(file, 0, before.Size())); err != nil ||
		hex.EncodeToString(hash.Sum(nil)) != identity.SHA256 {
		t.Fatal("fixture digest differs from its generation manifest")
	}
	if _, err := file.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	info, err := (Prober{FFprobePath: config.FFprobePath, Timeout: 30 * time.Second}).ProbeFile(ctx, file)
	if err != nil {
		t.Fatalf("probe actual authored container: %v", err)
	}
	if info.Container != "matroska,webm" || len(info.Streams) != 1 || info.Streams[0].CodecType != "subtitle" ||
		info.Streams[0].Codec != fixture.codec || info.DurationTicks != manifest.DurationTicks ||
		info.FormatStartKnown != fixture.originKnown || info.FormatStartTicks != fixture.origin {
		t.Fatalf("authored container facts changed: %+v", info)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 7 || !subtitleFileUnchanged(file, before) {
		t.Fatal("real probe changed or closed its borrowed source descriptor")
	}
	return file, info, before
}
