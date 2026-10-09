//go:build linux

package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func subtitleExtractionCacheFixture(t *testing.T, runtime *hlsRuntime) (*hlsSession, library.MediaFile, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	session := &hlsSession{ctx: ctx, cancel: cancel, key: hlsKey{stamp: "source-version", scope: transcode.Scope{ItemID: "item", SourceID: "source"}}}
	source := library.MediaFile{Item: library.Item{ID: "item"}, SourceID: "source", ETag: "source-version"}
	tool := filepath.Join(t.TempDir(), "extractor")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.retire(session) })
	return session, source, tool
}

func subtitleExtractionCacheRead(t *testing.T, runtime *hlsRuntime, session *hlsSession, source library.MediaFile, index int, format, tool string) *hlsSubtitleExtractionRead {
	t.Helper()
	read := &hlsSubtitleExtractionRead{runtime: runtime, session: session}
	if err := read.prepare(context.Background(), source, media.Stream{Index: index, Codec: "subrip", CodecType: "subtitle"}, format, tool); err != nil {
		t.Fatal(err)
	}
	return read
}

func TestHLSSubtitleExtractionCacheBindsActualSourceTrackFormatAndTool(t *testing.T) {
	runtime := &hlsRuntime{}
	session, source, tool := subtitleExtractionCacheFixture(t, runtime)
	read := subtitleExtractionCacheRead(t, runtime, session, source, 2, "vtt", tool)
	read.candidate = library.SubtitleContent{Data: []byte("verified subtitle"), Info: library.Subtitle{Codec: "vtt"}}
	read.publish(context.Background())
	read.candidate.Data[0] = 'x'
	current := subtitleExtractionCacheRead(t, runtime, session, source, 2, "vtt", tool)
	if content, found := current.get(); !found || string(content.Data) != "verified subtitle" {
		t.Fatal("a matching source did not reuse independently owned extraction bytes")
	}
	for _, changed := range []struct {
		index  int
		format string
	}{{3, "vtt"}, {2, "srt"}} {
		if _, found := subtitleExtractionCacheRead(t, runtime, session, source, changed.index, changed.format, tool).get(); found {
			t.Fatal("a different track or extraction format reused cached bytes")
		}
	}
	changed := source
	changed.ETag = "replaced-source"
	if err := current.prepare(context.Background(), changed, media.Stream{Index: 2}, "vtt", tool); !errors.Is(err, library.ErrSourceChanged) {
		t.Fatal("the actual extraction source was relabeled with the session's earlier witness")
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 1\n# changed tool\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, found := subtitleExtractionCacheRead(t, runtime, session, source, 2, "vtt", tool).get(); found {
		t.Fatal("changed executable bytes retained an extraction from the previous file version")
	}
	if runtime.subtitleExtractionBytes != len("verified subtitle") {
		t.Fatal("cache accounting charged the extractor's backing capacity or a cache miss")
	}
}

func TestHLSSubtitleExtractionCacheCancellationRetirementAndLatePublication(t *testing.T) {
	runtime := &hlsRuntime{}
	session, source, tool := subtitleExtractionCacheFixture(t, runtime)
	read := subtitleExtractionCacheRead(t, runtime, session, source, 2, "vtt", tool)
	read.candidate.Data = []byte("complete extraction")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	read.publish(cancelled)
	if runtime.subtitleExtractionBytes != 0 {
		t.Fatal("a cancelled request published a reusable extraction")
	}
	read.publish(context.Background())
	if runtime.subtitleExtractionBytes == 0 {
		t.Fatal("the successful extraction was not retained")
	}
	runtime.retire(session)
	read.publish(context.Background())
	if runtime.subtitleExtractionBytes != 0 || len(session.subtitleExtractions) != 0 {
		t.Fatal("retirement did not release cached bytes or accepted a late result")
	}
	if _, found := read.get(); found {
		t.Fatal("a retired session returned cached extraction bytes")
	}
	if err := read.prepare(context.Background(), source, media.Stream{Index: 2}, "vtt", tool); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("a retired session admitted another extraction")
	}
}

func TestHLSSubtitleExtractionCacheBoundsSessionsAndAggregateRetention(t *testing.T) {
	runtime := &hlsRuntime{}
	data := make([]byte, media.MaxSubtitleExtractionBytes)
	var sessions []*hlsSession
	for number := 0; number < 5; number++ {
		session, source, tool := subtitleExtractionCacheFixture(t, runtime)
		sessions = append(sessions, session)
		for index := 0; index < 3; index++ {
			read := subtitleExtractionCacheRead(t, runtime, session, source, index, "vtt", tool)
			read.candidate.Data = data
			read.publish(context.Background())
		}
		if session.subtitleExtractionBytes > hlsSubtitleSessionCacheBytes || runtime.subtitleExtractionBytes > hlsSubtitleServiceCacheBytes {
			t.Fatal("subtitle retention exceeded a session or service byte budget")
		}
	}
	if runtime.subtitleExtractionBytes != hlsSubtitleServiceCacheBytes || sessions[4].subtitleExtractionBytes != 0 {
		t.Fatal("the aggregate cache budget did not bypass otherwise successful extraction results")
	}
	for _, session := range sessions {
		runtime.retire(session)
	}
	if runtime.subtitleExtractionBytes != 0 {
		t.Fatal("retirement leaked the aggregate cache reservation")
	}
}

func TestHLSSubtitleExtractionCacheBypassesAliasInvocations(t *testing.T) {
	runtime := &hlsRuntime{}
	session, source, tool := subtitleExtractionCacheFixture(t, runtime)
	alias := filepath.Join(t.TempDir(), "tool-alias")
	if err := os.Symlink(tool, alias); err != nil {
		t.Fatal(err)
	}
	for _, configured := range []string{alias, filepath.Base(tool), filepath.Dir(tool) + "/./" + filepath.Base(tool)} {
		read := subtitleExtractionCacheRead(t, runtime, session, source, 2, "vtt", configured)
		read.candidate.Data = []byte("successful alias extraction")
		read.publish(context.Background())
		if _, found := read.get(); found || read.tool != nil || runtime.subtitleExtractionBytes != 0 {
			t.Fatal("an alias invocation entered the direct-tool extraction cache")
		}
	}
}
