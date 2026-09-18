package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func remoteBitratePrincipal(limit int64) identity.Principal {
	return identity.Principal{User: identity.User{ID: "remote-user", Policy: json.RawMessage(fmt.Sprintf(`{"RemoteClientBitrateLimit":%d}`, limit))},
		PeerIP: "203.0.113.10", SessionID: "remote-auth", Client: identity.Client{DeviceID: "remote-device"}}
}

func remoteBitrateSource(bitrate, size, duration int64) library.MediaFile {
	return library.MediaFile{Item: library.Item{ID: "remote-item", Type: "Movie", Media: &media.Info{
		Bitrate: bitrate, Size: size, DurationTicks: duration, Streams: []media.Stream{{Index: 0, CodecType: "video", Codec: "h264", Bitrate: bitrate}}}},
		SourceID: "remote-source", ETag: "remote-stamp", Size: size}
}

func TestRemoteBitrateLimitsUseTrustedPeerAndStoredPolicy(t *testing.T) {
	for _, test := range []struct {
		peer string
		want int64
	}{{"203.0.113.10", 800_000}, {"", 800_000}, {"untrusted-text", 800_000}, {"127.0.0.1", 0},
		{"10.1.2.3", 0}, {"::1", 0}, {"::ffff:192.168.1.2", 0}} {
		principal := remoteBitratePrincipal(800_000)
		principal.PeerIP = test.peer
		limit, err := principalRemoteBitrateLimit(principal)
		if err != nil || limit != test.want {
			t.Fatalf("peer %q: limit=%d error=%v", test.peer, limit, err)
		}
	}
	principal := remoteBitratePrincipal(800_000)
	principal.User.Policy = json.RawMessage(`{"RemoteClientBitrateLimit":"800000"}`)
	principal.PeerIP = "127.0.0.1"
	if _, err := principalRemoteBitrateLimit(principal); err == nil {
		t.Fatal("malformed stored policy acquired a local exemption")
	}
	key := applicationMediaRuntimePrincipal(99)
	if limit, err := principalRemoteBitrateLimit(key); err != nil || limit != 0 {
		t.Fatal("application authority inherited a user bitrate policy")
	}
}

func TestRemoteBitratePlanningPreservesServerCeilingsAndPermissions(t *testing.T) {
	cfg := config.TranscodingConfig{Enabled: true, MaxBitrate: 2_000_000}
	principal := remoteBitratePrincipal(800_000)
	limits := hlsPrincipalLimits(cfg, principal)
	if limits.MaxBitrate != 800_000 || !limits.AllowRemux || !limits.AllowAudioTranscode || !limits.AllowVideoTranscode {
		t.Fatal("remote user ceiling did not reach conversion planning")
	}
	cfg.MaxBitrate = 400_000
	if limited := hlsPrincipalLimits(cfg, principal); limited.MaxBitrate != 400_000 {
		t.Fatal("user ceiling increased a stricter server limit")
	}
	cfg.MaxBitrate = 0
	if limited := hlsPrincipalLimits(cfg, remoteBitratePrincipal(40_000_000)); limited.MaxBitrate != 20_000_000 {
		t.Fatal("user ceiling increased the default conversion budget")
	}
	principal.PeerIP = "10.0.0.1"
	if limited := hlsPrincipalLimits(cfg, principal); limited.MaxBitrate != 0 {
		t.Fatal("local playback inherited the remote ceiling")
	}
	principal.User.Policy = json.RawMessage(`{"RemoteClientBitrateLimit":null}`)
	if limited := hlsPrincipalLimits(cfg, principal); limited.AllowRemux || limited.AllowAudioTranscode || limited.AllowVideoTranscode {
		t.Fatal("malformed policy left conversion permissions enabled")
	}
}

func TestRemoteOriginalAdmissionUsesPhysicalAverageAndDeclinesUnknown(t *testing.T) {
	principal := remoteBitratePrincipal(900_000)
	for _, test := range []struct {
		name   string
		source library.MediaFile
		want   bool
	}{
		{"known-below", remoteBitrateSource(800_000, 0, 0), true},
		{"probed-above", remoteBitrateSource(1_000_000, 0, 0), false},
		{"physical-average-above", remoteBitrateSource(100_000, 125_000, media.TicksPerSecond), false},
		{"physical-average-known", remoteBitrateSource(0, 100_000, media.TicksPerSecond), true},
		{"unknown", remoteBitrateSource(0, 0, 0), false},
		{"missing-info", library.MediaFile{}, false},
		{"overflow", remoteBitrateSource(100_000, math.MaxInt64, 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if actual := principalOriginalBitrateAllowed(principal, test.source); actual != test.want {
				t.Fatalf("admission=%t want=%t", actual, test.want)
			}
		})
	}
	source := remoteBitrateSource(100_000, 0, 0)
	source.Item.Media.Streams = append(source.Item.Media.Streams, media.Stream{Index: 1, CodecType: "audio", Bitrate: 850_000})
	if principalOriginalBitrateAllowed(principal, source) {
		t.Fatal("low container declaration hid a higher stream sum")
	}
	if !principalOriginalBitrateAllowed(remoteBitratePrincipal(0), library.MediaFile{}) {
		t.Fatal("zero user ceiling changed uncapped original admission")
	}
}

func TestRemotePlanAdmissionUsesExecutableTargetsAndCurrentSource(t *testing.T) {
	principal := remoteBitratePrincipal(1_000_000)
	plan := transcode.Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1, VideoBitrate: 800_000}
	source := remoteBitrateSource(5_000_000, 0, 0)
	if !principalPlanBitrateAllowed(principal, source, plan) {
		t.Fatal("permitted encoding did not reduce a high source bitrate")
	}
	plan.VideoBitrate = 950_000
	if principalPlanBitrateAllowed(principal, source, plan) {
		t.Fatal("HLS output omitted its transport budget")
	}
	plan.OutputMode = "progressive"
	if !principalPlanBitrateAllowed(principal, source, plan) {
		t.Fatal("progressive media ceiling was confused with HLS transport budgeting")
	}
	plan.VideoCodec, plan.VideoBitrate = "copy", 1
	if principalPlanBitrateAllowed(principal, source, plan) {
		t.Fatal("a fabricated copy target replaced current source evidence")
	}
	source = remoteBitrateSource(0, 0, 0)
	if principalPlanBitrateAllowed(principal, source, plan) {
		t.Fatal("unknown copied bitrate was admitted")
	}
	source = remoteBitrateSource(0, 100_000, media.TicksPerSecond)
	if !principalPlanBitrateAllowed(principal, source, plan) {
		t.Fatal("physical source evidence was ignored for a copied stream")
	}
	principal = remoteBitratePrincipal(700_000)
	if principalPlanBitrateAllowed(principal, source, plan) {
		t.Fatal("a lower current policy retained a previously admissible output")
	}
}

func TestRemoteHLSRegistrationRejectsMisleadingOutputProjection(t *testing.T) {
	runtime, _ := hlsRuntimeTestFixture(t)
	principal := remoteBitratePrincipal(1_000_000)
	source := remoteBitrateSource(2_000_000, 0, 0)
	decision := playback.ConversionDecision{Plan: &transcode.Plan{Container: "ts", VideoCodec: "copy", VideoStreamIndex: 0, AudioStreamIndex: -1},
		OutputSource: playback.Source{Info: media.Info{Bitrate: 100_000}}}
	if _, err := runtime.register(principal, source, "remote-play", decision, 0); !errors.Is(err, library.ErrForbidden) {
		t.Fatal("low output projection authorized a high-bitrate copied source")
	}
	if len(runtime.sessions) != 0 {
		t.Fatal("declined registration consumed a session slot")
	}
}

func TestRemoteLosslessAdmissionUsesPayloadAndFrameBounds(t *testing.T) {
	plan := transcode.Plan{OutputMode: "progressive", Container: "flac", VideoStreamIndex: -1, AudioStreamIndex: 0,
		AudioCodec: "flac", AudioChannels: 2, AudioSampleRate: 48_000, AudioBitDepth: 16,
		AudioSourceSampleCount: 48_000, AudioSourceSampleRate: 48_000, DurationTicks: media.TicksPerSecond}
	if !principalPlanBitrateAllowed(remoteBitratePrincipal(1_600_000), library.MediaFile{}, plan) {
		t.Fatal("bounded FLAC output was rejected below the user ceiling")
	}
	if principalPlanBitrateAllowed(remoteBitratePrincipal(1_580_000), library.MediaFile{}, plan) {
		t.Fatal("FLAC frame overhead was omitted from its output bound")
	}
	plan.AudioBitrate = 1
	if principalPlanBitrateAllowed(remoteBitratePrincipal(1_600_000), library.MediaFile{}, plan) {
		t.Fatal("a fabricated lossy bitrate target authorized FLAC")
	}
	plan.AudioCodec, plan.Container, plan.AudioBitrate = "pcm_s16le", "wav", 0
	if !principalPlanBitrateAllowed(remoteBitratePrincipal(1_536_000), library.MediaFile{}, plan) ||
		principalPlanBitrateAllowed(remoteBitratePrincipal(1_535_999), library.MediaFile{}, plan) {
		t.Fatal("PCM admission did not use its actual channel and sample payload")
	}
}

func TestRemotePlanningRetriesAuthorizedEncodingAfterConservativeCopyDecline(t *testing.T) {
	input := hlsRequestTestSource()
	source := library.MediaFile{Item: library.Item{ID: input.ItemID, Type: input.ItemType, Media: &input.Info}, SourceID: input.MediaSourceID}
	request := playback.Request{DeviceProfile: &playback.DeviceProfile{Name: "remote HLS client",
		TranscodingProfiles: []playback.TranscodingProfile{{Type: playback.DlnaProfileTypeVideo, Container: "ts", Protocol: "hls", VideoCodec: "h264", AudioCodec: "aac"}}}}
	principal := remoteBitratePrincipal(1_000_000)
	limits := applyPrincipalRemoteBitrateLimit(hlsRequestTestLimits(), principal)
	decision, err := principalConversionDecision(principal, source, input, request, limits)
	if err != nil || decision.Plan == nil || decision.Plan.VideoCodec != "h264" || decision.Plan.AudioCodec != "aac" ||
		!principalPlanBitrateAllowed(principal, source, *decision.Plan) {
		t.Fatalf("usable full encoding did not replace the declined mixed-copy candidate: error=%v plan=%+v", err, decision.Plan)
	}
	if request.AllowVideoStreamCopy != nil || request.AllowAudioStreamCopy != nil {
		t.Fatal("fallback mutated caller-owned copy preferences")
	}
	limits.AllowAudioTranscode = false
	decision, err = principalConversionDecision(principal, source, input, request, limits)
	if err != nil || decision.Plan != nil {
		t.Fatal("copy fallback acquired a denied audio encoding permission")
	}
}

func TestRemoteEncodingRetryPreservesExplicitCodecRequirements(t *testing.T) {
	values := map[string]string{"VideoCodec": "copy", "AudioCodec": "copy", "AllowVideoStreamCopy": "true", "allowaudiostreamcopy": "true"}
	retry := remoteBitrateEncodingValues(values)
	if retry["VideoCodec"] != "copy" || retry["AudioCodec"] != "copy" || retry["allowvideostreamcopy"] != "false" ||
		retry["allowaudiostreamcopy"] != "false" || values["AllowVideoStreamCopy"] != "true" || values["allowaudiostreamcopy"] != "true" {
		t.Fatal("fallback changed an explicit codec requirement or mutated the original query")
	}
	if _, exists := retry["AllowVideoStreamCopy"]; exists {
		t.Fatal("retry left conflicting case-insensitive copy preferences")
	}
}
