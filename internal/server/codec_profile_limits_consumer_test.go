package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func TestCodecProfileLimitsReachNewAdmissionsWithoutRewritingRetainedDynamicWorkers(t *testing.T) {
	server, principal, lease, request, jobs := dynamicServerFixture(t)
	old := server.currentSettingsSnapshot()
	old.CodecProfileLimits = transcode.DefaultCodecProfileLimits()
	copyDisabled := false
	request.AllowVideoStreamCopy = &copyDisabled
	initial := managedExecutionRequest(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), old)
	dto, err := server.dynamicPlaybackDTO(initial, principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(dto["TranscodingUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	server.dynamicStreams.mu.Lock()
	bound := server.dynamicStreams.sessions[target.Query().Get("GobyLiveId")]
	server.dynamicStreams.mu.Unlock()
	if bound == nil || bound.key.plan.VideoProfile != "high" {
		t.Fatal("the initial request did not admit the existing highest H.264 profile")
	}
	changed := old
	changed.Revision++
	changed.CodecProfileLimits.H264 = "baseline"
	get := managedExecutionRequest(dynamicTestRequest(principal, http.MethodGet, target.String()), changed)
	get.SetPathValue("LiveStreamId", lease.ID)
	values, err := hlsValues(get)
	if err != nil {
		t.Fatal(err)
	}
	retained, _, err := server.findDynamicSession(get, values)
	if err != nil || retained != bound {
		t.Fatalf("a current profile limit retired an already accepted dynamic tuple: %v", err)
	}
	if _, err := server.ensureDynamicJob(get.Context(), get, bound); err != nil {
		t.Fatal(err)
	}
	jobs.mu.Lock()
	stable := len(jobs.specs) == 1 && jobs.specs[0].Plan.VideoProfile == "high" && jobs.cancelled == 0
	jobs.mu.Unlock()
	if !stable {
		t.Fatal("a delayed worker lost its admitted profile after the settings changed")
	}
	limits, err := server.dynamicLimits(get.Context(), principal, request, get)
	if err != nil || limits.CodecProfileLimits.H264 != "baseline" {
		t.Fatal("the request settings snapshot did not reach new dynamic admission limits")
	}
	fresh, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), request, limits)
	if err != nil || fresh.Plan == nil || fresh.Plan.VideoProfile != "baseline" {
		t.Fatalf("a fresh dynamic plan did not consume the new encoding upper bound: %+v, %v", fresh, err)
	}
	server.dynamicStreams.revalidate = func(_ context.Context, prior identity.Principal) (identity.Principal, error) {
		prior.User.Policy = []byte(`{"EnablePlaybackRemuxing":false,"EnableVideoPlaybackTranscoding":false,"EnableAudioPlaybackTranscoding":false}`)
		return prior, nil
	}
	if _, _, err := server.findDynamicSession(get, values); err == nil {
		t.Fatal("the retained profile exemption also bypassed current playback permissions")
	}
}
