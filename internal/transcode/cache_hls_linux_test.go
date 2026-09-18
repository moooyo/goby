//go:build linux

package transcode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheHLSArtifactsAccountTemporaryFilesWithoutPublishingThem(t *testing.T) {
	for _, extension := range []string{"ts", "m4s", "aac", "mp3"} {
		t.Run(extension, func(t *testing.T) {
			cache := newTestCache(t)
			if err := cache.CreateJob(cacheTestJobA); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cache.RootPath(), cacheTestJobA)
			writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "playlist")
			segment := "segment-000000." + extension
			writeCacheTestFile(t, filepath.Join(path, segment+".tmp"), "segment")
			if bytes, ready, err := cache.ScanJob(cacheTestJobA); err != nil || ready || bytes != 15 {
				t.Fatalf("temporary segment accounting = %d, %v, %v", bytes, ready, err)
			}
			if file, err := cache.OpenJobFile(cacheTestJobA, segment+".tmp"); file != nil || !errors.Is(err, ErrCacheInvalid) {
				t.Fatalf("temporary segment became public: %v", err)
			}
			if err := os.Rename(filepath.Join(path, segment+".tmp"), filepath.Join(path, segment)); err != nil {
				t.Fatal(err)
			}
			if extension == "m4s" {
				writeCacheTestFile(t, filepath.Join(path, "init.mp4.tmp"), "init")
				if bytes, ready, err := cache.ScanJob(cacheTestJobA); err != nil || ready || bytes != 19 {
					t.Fatalf("unpublished initialization accounting = %d, %v, %v", bytes, ready, err)
				}
				if err := os.Rename(filepath.Join(path, "init.mp4.tmp"), filepath.Join(path, "init.mp4")); err != nil {
					t.Fatal(err)
				}
			}
			if _, ready, err := cache.ScanJob(cacheTestJobA); err != nil || !ready {
				t.Fatalf("published %s output not ready: %v, %v", extension, ready, err)
			}
			file, err := cache.OpenJobFile(cacheTestJobA, segment)
			if err != nil {
				t.Fatal(err)
			}
			_ = file.Close()
			if err := cache.Recover(); err != nil {
				t.Fatalf("HLS artifact prevented recovery: %v", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("recovered HLS directory remains: %v", err)
			}
		})
	}
}

func TestCacheHLSReadinessRequiresEveryPlannedRendition(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA)
	plan := Plan{HLS: HLSPlan{SegmentType: "fmp4", RenditionCount: 2}}
	for _, name := range []string{"v0.m3u8", "v0-init.mp4", "v0-segment-000000.m4s", "v1.m3u8", "init.mp4", "segment-000000.m4s"} {
		writeCacheTestFile(t, filepath.Join(path, name), "data")
	}
	if bytes, ready, err := cache.ScanPlanJob(cacheTestJobA, plan); err != nil || ready || bytes != 24 {
		t.Fatalf("missing second rendition = %d, %v, %v", bytes, ready, err)
	}
	writeCacheTestFile(t, filepath.Join(path, "v1-segment-000000.m4s"), "data")
	writeCacheTestFile(t, filepath.Join(path, "v1-init.mp4.tmp"), "data")
	if bytes, ready, err := cache.ScanPlanJob(cacheTestJobA, plan); err != nil || ready || bytes != 32 {
		t.Fatalf("unpublished second initialization = %d, %v, %v", bytes, ready, err)
	}
	if err := os.Rename(filepath.Join(path, "v1-init.mp4.tmp"), filepath.Join(path, "v1-init.mp4")); err != nil {
		t.Fatal(err)
	}
	if bytes, ready, err := cache.ScanPlanJob(cacheTestJobA, plan); err != nil || !ready || bytes != 32 {
		t.Fatalf("complete rendition set = %d, %v, %v", bytes, ready, err)
	}
	if _, ready, err := cache.ScanJob(cacheTestJobA); err != nil || ready {
		t.Fatalf("variant output substituted for a missing primary playlist: %v, %v", ready, err)
	}
	if err := cache.RemoveJob(cacheTestJobA); err != nil {
		t.Fatalf("renditions prevented bounded cleanup: %v", err)
	}
}

func TestCacheHLSReadinessRequiresPlannedSegmentType(t *testing.T) {
	cache := newTestCache(t)
	if err := cache.CreateJob(cacheTestJobA); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cache.RootPath(), cacheTestJobA)
	writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "playlist")
	writeCacheTestFile(t, filepath.Join(path, "segment-000000.vtt"), "WEBVTT")
	for _, segmentType := range []string{"", "mpegts", "fmp4", "packed"} {
		if _, ready, err := cache.ScanPlanJob(cacheTestJobA, Plan{HLS: HLSPlan{SegmentType: segmentType}}); err != nil || ready {
			t.Fatalf("subtitle made %q media ready: %v, %v", segmentType, ready, err)
		}
	}
	writeCacheTestFile(t, filepath.Join(path, "segment-000000.aac"), "audio")
	for _, segmentType := range []string{"mpegts", "fmp4"} {
		if _, ready, err := cache.ScanPlanJob(cacheTestJobA, Plan{HLS: HLSPlan{SegmentType: segmentType}}); err != nil || ready {
			t.Fatalf("packed audio made %q media ready: %v, %v", segmentType, ready, err)
		}
	}
	if _, ready, err := cache.ScanPlanJob(cacheTestJobA, Plan{HLS: HLSPlan{SegmentType: "packed"}}); err != nil || !ready {
		t.Fatalf("packed audio was not ready: %v, %v", ready, err)
	}
}

func TestCacheHLSArtifactNamesKeepPrivateAndExternalPathsClosed(t *testing.T) {
	for _, name := range []string{"main.m3u8", "v0.m3u8", "v3-init.mp4", "v2-segment-000001.m4s", "segment-000002.aac", "segment-000003.mp3", "segment-000004.vtt", "segment-0.ts"} {
		if !validOutputName(name) || !validCacheFileName(name) || !validCacheFileName(name+".tmp") {
			t.Errorf("generated artifact was not recognized: %s", name)
		}
	}
	for _, name := range []string{"../init.mp4", "/init.mp4", "v0/init.mp4", `v0\init.mp4`, "https://example.test/init.mp4", "init.mp4?token=x", "v4.m3u8", "v4-init.mp4", "v4-segment-000000.m4s", "v0-segment-0.m4s", "segment-000000.mp4", "segment-list.m3u8", "main.m3u8.publish.tmp", "init.mp4.tmp", "stream.bin"} {
		if validOutputName(name) {
			t.Errorf("private or external artifact became public: %s", name)
		}
	}
	for _, name := range []string{"../init.mp4.tmp", "v0/init.mp4", "v4.m3u8", "v0-segment-000000.m4s.tmp.tmp", "unrelated.bin"} {
		if validCacheFileName(name) {
			t.Errorf("unknown content became cache-owned: %s", name)
		}
	}
}

func TestCachePrivateSubtitleAssetsAreAccountedWithoutMakingOutputReady(t *testing.T) {
	for _, mode := range []string{"", "progressive"} {
		t.Run("mode="+mode, func(t *testing.T) {
			cache := newTestCache(t)
			if err := cache.CreateJob(cacheTestJobA); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(cache.RootPath(), cacheTestJobA)
			writeCacheTestFile(t, filepath.Join(path, "subtitle.ass"), "style")
			writeCacheTestFile(t, filepath.Join(path, "font-0.ttf"), "font")
			plan := Plan{OutputMode: mode}
			if bytes, ready, err := cache.ScanPlanJob(cacheTestJobA, plan); err != nil || ready || bytes != 9 {
				t.Fatalf("private assets established readiness or escaped accounting: %d, %v, %v", bytes, ready, err)
			}
			for _, name := range []string{"subtitle.ass", "font-0.ttf"} {
				if file, err := cache.OpenJobFile(cacheTestJobA, name); file != nil || !errors.Is(err, ErrCacheInvalid) {
					if file != nil {
						_ = file.Close()
					}
					t.Fatalf("private rendering asset became public: %s, %v", name, err)
				}
			}
			if mode == "progressive" {
				writeCacheTestFile(t, filepath.Join(path, "stream.bin"), "stream")
			} else {
				writeCacheTestFile(t, filepath.Join(path, "main.m3u8"), "playlist")
				writeCacheTestFile(t, filepath.Join(path, "segment-0.ts"), "segment")
			}
			if _, ready, err := cache.ScanPlanJob(cacheTestJobA, plan); err != nil || !ready {
				t.Fatalf("private assets prevented output readiness: %v, %v", ready, err)
			}
			if err := cache.Recover(); err != nil {
				t.Fatalf("private assets prevented recovery: %v", err)
			}
		})
	}
	for _, name := range []string{"font-16.ttf", "font-00.ttf", "font--1.ttf", "font-0.otf", "fonts/font-0.ttf", "subtitle.ass.tmp"} {
		if validCacheFileName(name) {
			t.Errorf("unbounded private asset name was accepted: %s", name)
		}
	}
}
