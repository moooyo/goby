//go:build linux

package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestHTTPAudioWaveformLevelErrorPrecedence(t *testing.T) {
	f := newAudioWaveformHTTPFixture(t)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	base := "/emby/Items/" + f.itemID + "/AudioWaveforms"
	wrongTag := base + "/0?buckets=512&tag=obsolete"

	t.Run("missing_resource_before_tag", func(t *testing.T) {
		expectAPIError(t, f.request(t, http.MethodGet, "/emby/Items/missing-waveform-item/AudioWaveforms/0?buckets=512&tag=obsolete", nil, headers),
			http.StatusNotFound, "not_found", true)
		expectAPIError(t, f.request(t, http.MethodGet, wrongTag, nil, headers), http.StatusNotFound, "not_found", true)
	})

	artifact := publishAudioWaveformHTTPArtifact(t, f)
	t.Run("tag_before_missing_track", func(t *testing.T) {
		expectAPIError(t, f.request(t, http.MethodGet, wrongTag, nil, headers), http.StatusNotFound, "audio_waveform_changed", true)
		expectAPIError(t, f.request(t, http.MethodGet, base+"/1?buckets=512&tag=obsolete", nil, headers),
			http.StatusNotFound, "audio_waveform_changed", true)
		expectAPIError(t, f.request(t, http.MethodGet, base+"/1?buckets=512&tag="+url.QueryEscape(artifact.ETag), nil, headers),
			http.StatusNotFound, "not_found", true)
	})

	t.Run("playback_permission_before_tag", func(t *testing.T) {
		setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":true,"EnableMediaPlayback":false}`)
		expectAPIError(t, f.request(t, http.MethodGet, wrongTag, nil, headers), http.StatusForbidden, "access_denied", true)
	})

	t.Run("library_visibility_before_tag", func(t *testing.T) {
		setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":false,"EnabledFolders":[],"EnableMediaPlayback":true}`)
		expectAPIError(t, f.request(t, http.MethodGet, wrongTag, nil, headers), http.StatusNotFound, "not_found", true)
	})
}

func TestHTTPAudioWaveformPayloadTamperingOverridesConditionalRequests(t *testing.T) {
	f := newAudioWaveformHTTPFixture(t)
	artifact := publishAudioWaveformHTTPArtifact(t, f)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	base := "/emby/Items/" + f.itemID + "/AudioWaveforms"
	target := base + "/0?buckets=512&tag=" + url.QueryEscape(artifact.ETag)
	response := f.request(t, http.MethodGet, target, nil, headers)
	expectStatus(t, response, http.StatusOK)
	etag := response.Header().Get("ETag")
	if etag == "" {
		t.Fatal("successful waveform response did not supply an ETag")
	}

	paths, err := filepath.Glob(filepath.Join(filepath.Dir(f.mediaPath), "backdrops", "goby-waveforms", "*", artifact.Generation))
	if err != nil || len(paths) != 1 {
		t.Fatalf("locate published waveform payload: %v %v", paths, err)
	}
	stored, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	data, err := media.ParseAudioWaveforms(stored)
	if err != nil {
		t.Fatal(err)
	}
	// Preserve valid encoding and size to isolate the stored digest check.
	data.Tracks[0].Levels[0].RMS[0]++
	tampered, err := media.MarshalAudioWaveforms(data)
	if err != nil || len(tampered) != len(stored) {
		t.Fatalf("encode same-size waveform tampering fixture: %v", err)
	}
	if err := os.WriteFile(paths[0], tampered, 0o600); err != nil {
		t.Fatal(err)
	}

	conditional := headers.Clone()
	conditional.Set("If-None-Match", etag)
	t.Run("matching_etag", func(t *testing.T) {
		expectAPIError(t, f.request(t, http.MethodGet, target, nil, conditional), http.StatusConflict, "audio_waveform_conflict", true)
	})
	t.Run("wrong_tag", func(t *testing.T) {
		expectAPIError(t, f.request(t, http.MethodGet, base+"/0?buckets=512&tag=obsolete", nil, headers),
			http.StatusConflict, "audio_waveform_conflict", true)
	})
	t.Run("descriptor", func(t *testing.T) {
		expectAPIError(t, f.request(t, http.MethodGet, base, nil, headers), http.StatusConflict, "audio_waveform_conflict", true)
	})
}

func TestHTTPAudioWaveformSourceReplacementRemainsStaleAfterRescan(t *testing.T) {
	f := newAudioWaveformHTTPFixture(t)
	artifact := publishAudioWaveformHTTPArtifact(t, f)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	base := "/emby/Items/" + f.itemID + "/AudioWaveforms"
	target := base + "/0?buckets=512&tag=" + url.QueryEscape(artifact.ETag)
	response := f.request(t, http.MethodGet, target, nil, headers)
	expectStatus(t, response, http.StatusOK)
	conditional := headers.Clone()
	conditional.Set("If-None-Match", response.Header().Get("ETag"))

	before, err := os.Stat(f.mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(f.mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := f.mediaPath + ".replacement"
	if err := os.WriteFile(replacement, source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, f.mediaPath); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(f.mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("source replacement fixture did not change identity while preserving size and modification time")
	}

	assertStale := func(t *testing.T) {
		t.Helper()
		if descriptor := audioWaveformHTTPDescriptor(t, f, "", headers); !reflect.DeepEqual(descriptor, map[string]any{"Available": false, "Stale": true}) {
			t.Fatalf("replacement source exposed a preserved waveform: %#v", descriptor)
		}
		expectAPIError(t, f.request(t, http.MethodGet, target, nil, conditional), http.StatusNotFound, "audio_waveform_stale", true)
		expectAPIError(t, f.request(t, http.MethodGet, base+"/1?buckets=512&tag=obsolete", nil, headers),
			http.StatusNotFound, "audio_waveform_stale", true)
	}
	t.Run("before_rescan", assertStale)
	f.rescan(t, adminMetadataAutomaticNFO)
	t.Run("after_rescan", assertStale)
}
