package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func TestApplicationKeyMediaTargetLimitsUseOnlyConversionPermissions(t *testing.T) {
	cfg := config.TranscodingConfig{Enabled: true, MaxBitrate: 4_000_000, MaxWidth: 1280, MaxHeight: 720, MaxAudioChannels: 2,
		Hardware: transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}}
	for _, test := range []struct {
		name                string
		policy              string
		disabled            bool
		remux, audio, video bool
	}{
		{"defaults", `{}`, false, true, true, true},
		{"only-account-disabled", `{}`, true, true, true, true},
		{"only-media-playback-disabled", `{"EnableMediaPlayback":false}`, false, true, true, true},
		{"all-conversion-disabled", `{"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":false,"EnableVideoPlaybackTranscoding":false}`, false, false, false, false},
		{"mixed-conversion-permissions", `{"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":true,"EnableVideoPlaybackTranscoding":false}`, false, false, true, false},
		{"malformed-permission", `{"EnablePlaybackRemuxing":null,"EnableAudioPlaybackTranscoding":"true","EnableVideoPlaybackTranscoding":0}`, false, false, false, false},
		{"malformed-policy", `null`, false, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := hlsApplicationTargetLimits(cfg, identity.User{IsDisabled: test.disabled, Policy: json.RawMessage(test.policy)})
			if limits.AllowRemux != test.remux || limits.AllowAudioTranscode != test.audio || limits.AllowVideoTranscode != test.video {
				t.Fatal("application target conversion permissions did not match the independently controlled flags")
			}
			if limits.MaxBitrate != cfg.MaxBitrate || limits.MaxWidth != cfg.MaxWidth || limits.MaxHeight != cfg.MaxHeight ||
				limits.MaxAudioChannels != cfg.MaxAudioChannels || limits.Hardware != cfg.Hardware {
				t.Fatal("target conversion policy replaced server execution limits")
			}
		})
	}
	cfg.Enabled = false
	limits := hlsApplicationTargetLimits(cfg, identity.User{Policy: json.RawMessage(`{}`)})
	if limits.AllowRemux || limits.AllowAudioTranscode || limits.AllowVideoTranscode {
		t.Fatal("target policy enabled conversion while the server disabled it")
	}
}

func applicationMediaRuntimePrincipal(id int64) identity.Principal {
	return identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: id,
		SessionID: fmt.Sprintf("%032x", id), ClientSessionID: fmt.Sprintf("%032x", id+1000), Client: identity.Client{DeviceID: "server-device"}}
}

func TestApplicationKeyMediaConversionLimitsPreserveGlobalConfiguration(t *testing.T) {
	cfg := config.TranscodingConfig{Enabled: true, MaxBitrate: 4_000_000, MaxWidth: 1280, MaxHeight: 720,
		MaxAudioChannels: 2, Hardware: transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD128"}}
	principal := applicationMediaRuntimePrincipal(1)
	limits := hlsPrincipalLimits(cfg, principal)
	if !limits.AllowRemux || !limits.AllowAudioTranscode || !limits.AllowVideoTranscode || limits.MaxBitrate != cfg.MaxBitrate ||
		limits.MaxWidth != cfg.MaxWidth || limits.MaxHeight != cfg.MaxHeight || limits.MaxAudioChannels != cfg.MaxAudioChannels || limits.Hardware != cfg.Hardware {
		t.Fatal("application conversion did not preserve the configured permissions and execution limits")
	}
	cfg.Enabled = false
	limits = hlsPrincipalLimits(cfg, principal)
	if limits.AllowRemux || limits.AllowAudioTranscode || limits.AllowVideoTranscode {
		t.Fatal("disabled server conversion remained available to an application key")
	}
	cfg.Enabled = true
	principal.ApplicationKeyID = 0
	limits = hlsPrincipalLimits(cfg, principal)
	if limits.AllowRemux || limits.AllowAudioTranscode || limits.AllowVideoTranscode {
		t.Fatal("an incomplete application principal acquired global playback permissions")
	}
}

func TestApplicationKeyMediaHLSCapacitySeparatesCredentialsAndRetainsGlobalLimit(t *testing.T) {
	runtime, _ := hlsRuntimeTestFixture(t)
	source := library.MediaFile{Item: library.Item{ID: "item", Type: "Movie"}, SourceID: "source", ETag: "stamp"}
	decision := playback.ConversionDecision{Plan: &transcode.Plan{Container: "ts", VideoCodec: "h264",
		VideoStreamIndex: 0, AudioStreamIndex: -1, DurationTicks: 120_000_000, SegmentSeconds: 3}}
	var first *hlsSession
	for credential := 1; credential <= maxHLSSessions/maxHLSAuthSessions; credential++ {
		principal := applicationMediaRuntimePrincipal(int64(credential))
		for play := 0; play < maxHLSAuthSessions; play++ {
			session, err := runtime.register(principal, source, fmt.Sprintf("play-%d", play), decision, 0)
			if err != nil {
				t.Fatalf("independent application credential hit a shared user quota at credential %d (%T)", credential, err)
			}
			if !session.key.scope.ApplicationKey || session.key.scope.UserID != "" {
				t.Fatal("application HLS registry lost its userless owner type")
			}
			if first == nil {
				first = session
			}
		}
		if _, err := runtime.register(principal, source, "one-too-many", decision, 0); !errors.Is(err, transcode.ErrBusy) {
			t.Fatal("application credential exceeded its authentication session quota")
		}
	}
	if _, err := runtime.register(applicationMediaRuntimePrincipal(100), source, "new-key-play", decision, 0); !errors.Is(err, transcode.ErrBusy) {
		t.Fatal("application credentials exceeded the shared HLS server capacity")
	}
	if _, err := runtime.find(first.id, applicationMediaRuntimePrincipal(2), source.Item.ID); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("another application credential found a foreign HLS session")
	}
	otherClient := applicationMediaRuntimePrincipal(1)
	otherClient.ClientSessionID = fmt.Sprintf("%032x", 2000)
	if _, err := runtime.find(first.id, otherClient, source.Item.ID); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("another client of the same key found a foreign HLS session")
	}
	ordinary := applicationMediaRuntimePrincipal(1)
	ordinary.Kind, ordinary.ApplicationKeyID, ordinary.ClientSessionID = "emby", 0, ""
	if _, err := runtime.find(first.id, ordinary, source.Item.ID); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("HLS ownership accepted a mismatched credential kind")
	}
}
