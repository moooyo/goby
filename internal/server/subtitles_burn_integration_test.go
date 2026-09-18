//go:build linux

package server

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestExternalBurnAssetsReauthorizeSessionAndSubtitleFingerprint(t *testing.T) {
	fixture := newSubtitleHTTPFixture(t)
	streamFixture := fixture.p.s
	f := streamFixture.f
	principal, err := f.users.Resolve(f.ctx, streamFixture.token, "emby")
	if err != nil {
		t.Fatal(err)
	}
	file, source, err := f.app.library.OpenMediaFor(f.ctx, librarySubject(principal, principal.User.ID), streamFixture.video.id, media.SourceID(streamFixture.video.id))
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	content, err := f.app.library.ReadSubtitleFor(f.ctx, librarySubject(principal, principal.User.ID), source.Item.ID, source.SourceID, 6)
	if err != nil {
		t.Fatal(err)
	}
	play, err := f.app.library.PreparePlayback(f.ctx, playbackOwner(principal), source.Item.ID, source.SourceID, "")
	if err != nil {
		t.Fatal(err)
	}
	plan := transcode.Plan{Container: "ts", VideoCodec: "h264", AudioCodec: "aac", VideoStreamIndex: 2, AudioStreamIndex: 5,
		DurationTicks: source.Item.Media.DurationTicks, SegmentSeconds: 6,
		Subtitle: transcode.SubtitlePlan{Mode: "burn", Codec: "srt", StreamIndex: 6, ExternalTag: content.Info.Tag}}
	scope := transcode.Scope{UserID: principal.User.ID, AuthSessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
		PlaySessionID: play.ID, ItemID: source.Item.ID, SourceID: source.SourceID}
	session := &hlsSession{key: hlsKey{scope: scope, stamp: source.ETag, plan: plan}, principal: principal}
	priorRuntime, priorEnabled := f.app.hls, f.app.cfg.Transcoding.Enabled
	f.app.hls = &hlsRuntime{sessions: map[string]*hlsSession{"test-session": session}}
	f.app.cfg.Transcoding.Enabled = true
	defer func() { f.app.hls, f.app.cfg.Transcoding.Enabled = priorRuntime, priorEnabled }()
	spec := transcode.Spec{Scope: scope, SourceStamp: source.ETag, Plan: plan}
	asset, err := f.app.readBurnSubtitleAsset(f.ctx, spec)
	if err != nil || !strings.Contains(string(asset), "[Script Info]") || !strings.Contains(string(asset), "Before start") {
		t.Fatalf("authorized sidecar did not produce an ASS burn asset: %v", err)
	}
	wrongScope := spec
	wrongScope.Scope.AuthSessionID = "other-session"
	if _, err := f.app.readBurnSubtitleAsset(f.ctx, wrongScope); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("another authentication scope borrowed the subtitle asset: %v", err)
	}
	if err := os.WriteFile(fixture.srtPath, []byte(strings.ReplaceAll(subtitleHTTPSRT, "Before start", "Changed caption")), 0600); err != nil {
		t.Fatal(err)
	}
	streamFixture.rescan(t, streamFixture.video.libraryID)
	if _, err := f.app.readBurnSubtitleAsset(f.ctx, spec); !errors.Is(err, library.ErrSourceChanged) {
		t.Fatalf("rescanned subtitle bytes reused the old burn fingerprint: %v", err)
	}
	streamFixture.setPolicy(t, streamFixture.viewerID, false, []string{streamFixture.video.libraryID})
	if _, err := f.app.readBurnSubtitleAsset(f.ctx, spec); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("revoked playback permission retained subtitle preparation: %v", err)
	}
	streamFixture.setPolicy(t, streamFixture.viewerID, true, []string{streamFixture.video.libraryID})
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at = now() WHERE id = $1", principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.readBurnSubtitleAsset(f.ctx, spec); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked authentication retained subtitle preparation: %v", err)
	}
}
