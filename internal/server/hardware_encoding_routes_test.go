//go:build linux

package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

type hardwareEncodingRouteProbe struct {
	usable              atomic.Bool
	hardwareIdentities  atomic.Int32
	hardwareProbes      atomic.Int32
	toolIdentities      atomic.Int32
	encoderEnumerations atomic.Int32
}

func installHardwareEncodingRouteProbe(t *testing.T, server *Server) *hardwareEncodingRouteProbe {
	t.Helper()
	probe := &hardwareEncodingRouteProbe{}
	runtime := newHardwareEncodingRuntime()
	runtime.identify = func(context.Context, string, string, string) (string, error) {
		probe.hardwareIdentities.Add(1)
		return "route-test-hardware-identity", nil
	}
	runtime.probe = func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error) {
		probe.hardwareProbes.Add(1)
		if probe.usable.Load() {
			return media.HardwareEncodingResult{Usable: true, Code: "verified"}, nil
		}
		return media.HardwareEncodingResult{Code: "route-test-hardware-rejected"}, nil
	}
	runtime.toolIdentity = func(context.Context, string) (string, error) {
		probe.toolIdentities.Add(1)
		return "route-test-tool-identity", nil
	}
	runtime.encoders = func(context.Context, string) ([]string, error) {
		probe.encoderEnumerations.Add(1)
		return []string{"libx264", "libx265", "libaom-av1"}, nil
	}
	server.hardwareEncoding = runtime
	t.Cleanup(runtime.cancel)
	return probe
}

func (probe *hardwareEncodingRouteProbe) calls() [4]int32 {
	return [4]int32{probe.hardwareIdentities.Load(), probe.hardwareProbes.Load(), probe.toolIdentities.Load(), probe.encoderEnumerations.Load()}
}

func hardwareEncodingDynamicRouteFixture(t *testing.T) (*Server, identity.Principal, dynamicsource.Lease, playback.Request, *dynamicTestJobs, *hardwareEncodingRouteProbe) {
	t.Helper()
	server, principal, lease, request, jobs := dynamicServerFixture(t)
	server.cfg.Transcoding.Hardware = transcode.Hardware{Decode: "software", Encode: "vaapi", Device: "/dev/dri/renderD128"}
	server.cfg.Transcoding.Execution = transcode.DefaultExecutionOptions(server.cfg.Transcoding.Threads)
	disabled := false
	request.AllowVideoStreamCopy = &disabled
	request.DeviceProfile.TranscodingProfiles[0].VideoCodec = "hevc"
	probe := installHardwareEncodingRouteProbe(t, server)
	return server, principal, lease, request, jobs, probe
}

func hardwareEncodingDynamicOutput(t *testing.T, server *Server, principal identity.Principal, lease dynamicsource.Lease, request playback.Request) (*dynamicStreamSession, *http.Request) {
	t.Helper()
	dto, err := server.dynamicPlaybackDTO(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), principal, lease, request)
	if err != nil {
		t.Fatalf("negotiate a controlled hardware output: %v", err)
	}
	target, ok := dto["TranscodingUrl"].(string)
	if !ok || target == "" {
		t.Fatal("hardware output did not advertise a transcoding URL")
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal("hardware output URL was malformed")
	}
	session := server.dynamicStreams.sessions[parsed.Query().Get("GobyLiveId")]
	if session == nil {
		t.Fatal("hardware output was not registered")
	}
	get := dynamicTestRequest(principal, http.MethodGet, target)
	get.SetPathValue("LiveStreamId", lease.ID)
	get.SetPathValue("Artifact", "master.m3u8")
	return session, get
}

func TestHardwareEncodingDynamicFallbackRetainsRevisionAfterHardwareRecovers(t *testing.T) {
	server, principal, lease, request, jobs, probe := hardwareEncodingDynamicRouteFixture(t)
	limits := hlsPrincipalLimits(server.cfg.Transcoding, principal)
	planned, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), request, limits)
	if err != nil || planned.Plan == nil || planned.Plan.Hardware.Encode != "vaapi" {
		t.Fatal("the controlled dynamic fixture did not require hardware encoding")
	}
	session, get := hardwareEncodingDynamicOutput(t, server, principal, lease, request)
	want := softwareEncodingPlan(*planned.Plan)
	if session.key.plan != want || session.key.plan.VideoCodec != "hevc" || probe.hardwareProbes.Load() != 1 || len(jobs.specs) != 0 {
		t.Fatal("dynamic negotiation did not register its complete software fallback before production")
	}
	if session.output.Info.Streams[0].CodecTag != "hvc1" || session.output.Info.Streams[0].CodecTagString != "hvc1" {
		t.Fatal("dynamic software fallback retained the hardware HEVC sample entry")
	}

	// A later successful admission belongs to a new revision. Reauthorization
	// must not revisit capabilities or promote an existing software revision.
	probe.usable.Store(true)
	server.hardwareEncoding.mu.Lock()
	server.hardwareEncoding.entries = make(map[hardwareEncodingKey]hardwareEncodingEntry)
	server.hardwareEncoding.mu.Unlock()
	before := probe.calls()
	values, err := hlsValues(get)
	if err != nil {
		t.Fatal(err)
	}
	found, _, err := server.findDynamicSession(get, values)
	if err != nil || found != session {
		t.Fatalf("software fallback failed dynamic reauthorization: %v", err)
	}
	if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil {
		t.Fatalf("software fallback failed producer reauthorization: %v", err)
	}
	if len(jobs.specs) != 1 || jobs.specs[0].Plan != want || jobs.bytes[0] != "authorized upstream bytes" || session.key.plan != want || probe.calls() != before {
		t.Fatal("dynamic delivery changed its admitted plan, repeated a probe, or consumed an unauthorized source")
	}
	if session.output.Info.Streams[0].CodecTag != "hvc1" || session.output.Info.Streams[0].CodecTagString != "hvc1" {
		t.Fatal("dynamic producer reauthorization restored stale hardware framing")
	}
	if _, _, err := server.findDynamicSession(get, values); err != nil {
		t.Fatalf("running software revision failed reauthorization: %v", err)
	}
	if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil || len(jobs.specs) != 1 || probe.calls() != before {
		t.Fatal("the existing dynamic producer was replaced after hardware recovery")
	}

	// A profile change seals the old producer but preserves its committed
	// timeshift presentation. The payload is opaque storage evidence, not a
	// claim that this synthetic route fixture encoded HEVC media.
	const retainedPayload = "software-fallback-history"
	published := (dynamicLifecycleFixture{server: server, principal: principal, lease: lease, request: request, session: session, jobs: jobs}).publish(t, retainedPayload)
	replacement, replacementGet := hardwareEncodingDynamicOutput(t, server, principal, lease, request)
	session.mu.Lock()
	oldEnded, oldClosed, oldInput := session.producerEnded, session.closed, session.input
	session.mu.Unlock()
	if replacement == session || replacement.id == session.id || replacement.key.plan.Hardware.Encode != "vaapi" ||
		!oldEnded || oldClosed || oldInput != nil || session.ctx.Err() != nil || jobs.cancelled != 1 || probe.hardwareProbes.Load() != 2 || session.key.plan != want {
		t.Fatal("explicit renegotiation did not isolate newly admitted hardware from the old revision")
	}
	server.dynamicStreams.mu.Lock()
	oldCurrent := server.dynamicStreams.byKey[session.key]
	newCurrent := server.dynamicStreams.byKey[replacement.key]
	retainedSession := server.dynamicStreams.sessions[session.id]
	server.dynamicStreams.mu.Unlock()
	if oldCurrent != nil || newCurrent != replacement || retainedSession != session || replacement.windowID == session.windowID {
		t.Fatal("renegotiation confused retained history with the current hardware presentation")
	}
	retained, err := server.dynamicStreams.store.Snapshot(get.Context(), timeshiftScope(session.scope), session.windowID)
	if err != nil || !retained.Ended || retained.LiveEdgeTicks != published.LiveEdgeTicks || len(retained.Segments) != 1 || len(retained.Segments[0].Artifacts) != 1 ||
		retained.Segments[0].Artifacts[0].ID != published.Segments[0].Artifacts[0].ID {
		t.Fatalf("hardware renegotiation destroyed or rewrote committed software history: %v", err)
	}
	handle, err := server.dynamicStreams.store.OpenArtifact(get.Context(), timeshiftScope(session.scope), session.windowID, retained.Segments[0].Artifacts[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	payload, readErr := io.ReadAll(handle)
	closeErr := handle.Close()
	if readErr != nil || closeErr != nil || string(payload) != retainedPayload {
		t.Fatal("retained software history became unreadable after hardware recovery")
	}
	if replacement.output.Info.Streams[0].CodecTag != "hev1" || replacement.output.Info.Streams[0].CodecTagString != "hev1" {
		t.Fatal("the new hardware revision lost its authoritative HEVC sample entry")
	}
	admittedCalls := probe.calls()
	if _, err := server.ensureDynamicJob(replacementGet.Context(), replacementGet, replacement); err != nil {
		t.Fatalf("the newly admitted hardware revision did not reach its own producer: %v", err)
	}
	if len(jobs.specs) != 2 || jobs.specs[0].Plan != want || jobs.specs[1].Plan != *planned.Plan ||
		jobs.bytes[1] != "authorized upstream bytes" || jobs.cancelled != 1 || probe.calls() != admittedCalls {
		t.Fatal("new hardware production reused the software plan or repeated capability admission")
	}
	if found, _, err := server.findDynamicSession(get, values); err != nil || found != session {
		t.Fatalf("retained software history failed reauthorization after hardware production began: %v", err)
	}
	if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil || len(jobs.specs) != 2 ||
		probe.calls() != admittedCalls || session.output.Info.Streams[0].CodecTag != "hvc1" {
		t.Fatal("reading retained history restarted or promoted its old software producer")
	}
}

func TestHardwareEncodingDynamicHeadDoesNotProbeOrStartProduction(t *testing.T) {
	server, principal, lease, request, jobs, probe := hardwareEncodingDynamicRouteFixture(t)
	head := dynamicTestRequest(principal, http.MethodHead, "/emby/LiveStreams/MediaInfo")
	if _, err := server.dynamicPlaybackDTO(head, principal, lease, request); err == nil {
		t.Fatal("cold HEAD advertised an output without a known available encoder")
	}
	if probe.hardwareProbes.Load() != 0 || probe.encoderEnumerations.Load() != 0 || len(jobs.specs) != 0 || len(server.dynamicStreams.sessions) != 0 {
		t.Fatal("cold HEAD probed an encoder or registered an unverified output")
	}
	session, get := hardwareEncodingDynamicOutput(t, server, principal, lease, request)
	before := probe.calls()
	for _, artifact := range []string{"master.m3u8", "main.m3u8"} {
		head := dynamicTestRequest(principal, http.MethodHead, get.URL.String())
		head.SetPathValue("LiveStreamId", lease.ID)
		head.SetPathValue("Artifact", artifact)
		response := httptest.NewRecorder()
		server.dynamicHLSArtifact(response, head)
		want := http.StatusOK
		if artifact == "main.m3u8" {
			want = http.StatusNotFound
		}
		// The direct recorder retains JSON error bodies that net/http suppresses
		// for HEAD on the wire. An uncached media artifact is an ordinary 404.
		if response.Code != want || want == http.StatusOK && response.Body.Len() != 0 || probe.calls() != before || len(jobs.specs) != 0 || session.closed {
			t.Fatalf("dynamic %s HEAD changed admission or production: status=%d", artifact, response.Code)
		}
	}
}

func TestHardwareEncodingDynamicFallbackStillRevokesCurrentPermissions(t *testing.T) {
	server, principal, lease, request, jobs, probe := hardwareEncodingDynamicRouteFixture(t)
	session, get := hardwareEncodingDynamicOutput(t, server, principal, lease, request)
	if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil {
		t.Fatal(err)
	}
	before := probe.calls()
	server.dynamicStreams.revalidate = func(_ context.Context, prior identity.Principal) (identity.Principal, error) {
		prior.User.Policy = []byte(`{"EnableVideoPlaybackTranscoding":false}`)
		return prior, nil
	}
	values, err := hlsValues(get)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := server.findDynamicSession(get, values); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("software fallback bypassed revoked video encoding permission: %v", err)
	}
	if !session.closed || jobs.cancelled != 1 || len(jobs.specs) != 1 || probe.calls() != before {
		t.Fatal("permission revocation did not retire the admitted producer without new capability work")
	}
}

func TestHardwareEncodingDynamicProducerRechecksPermissionAfterFallback(t *testing.T) {
	server, principal, lease, request, jobs, probe := hardwareEncodingDynamicRouteFixture(t)
	session, get := hardwareEncodingDynamicOutput(t, server, principal, lease, request)
	before := probe.calls()
	server.dynamicStreams.revalidate = func(_ context.Context, prior identity.Principal) (identity.Principal, error) {
		prior.User.Policy = []byte(`{"EnableVideoPlaybackTranscoding":false}`)
		return prior, nil
	}
	if _, err := server.ensureDynamicJob(get.Context(), get, session); !errors.Is(err, errHLSRequestUnsupported) {
		t.Fatalf("producer admission reused the negotiation-time permission: %v", err)
	}
	if len(jobs.specs) != 0 || session.jobID != "" || probe.calls() != before {
		t.Fatal("producer authorization failure reached hardware admission or media production")
	}
}

func TestHardwareEncodingDynamicAuthorizationPrecedesCapabilityWork(t *testing.T) {
	for _, denied := range []string{"credential", "video policy"} {
		t.Run(denied, func(t *testing.T) {
			server, principal, lease, request, jobs, probe := hardwareEncodingDynamicRouteFixture(t)
			server.dynamicStreams.revalidate = func(_ context.Context, prior identity.Principal) (identity.Principal, error) {
				if denied == "credential" {
					return identity.Principal{}, identity.ErrUnauthorized
				}
				prior.User.Policy = []byte(`{"EnableVideoPlaybackTranscoding":false}`)
				return prior, nil
			}
			if _, err := server.dynamicPlaybackDTO(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), principal, lease, request); err == nil {
				t.Fatal("unauthorized dynamic encoding was accepted")
			}
			if probe.calls() != [4]int32{} || len(jobs.specs) != 0 || len(server.dynamicStreams.sessions) != 0 {
				t.Fatal("unauthorized dynamic encoding reached capability inspection or production")
			}
		})
	}
}

func TestHTTPHardwareEncodingAuthorizationPrecedesCapabilityWork(t *testing.T) {
	fixture := newPlaybackHTTPFixture(t)
	server := fixture.s.f.app
	server.cfg.Transcoding = config.TranscodingConfig{Enabled: true, MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 8,
		Hardware: transcode.Hardware{Decode: "software", Encode: "vaapi", Device: "/dev/dri/renderD128"}}
	// This route fixture proves authorization ordering, not a physical device.
	// Its inventory and encoder evidence are both explicit controlled inputs.
	server.managedHardware = newManagedHardwareInventoryWithInspector(server.cfg.Transcoding,
		func(string) (managedHardwareIdentity, string) { return managedHardwareTestIdentity(20), "" })
	fixture.s.f.cfg = server.cfg
	initializeFixtureSettings(t, fixture.s.f)
	ctx, cancel := context.WithCancel(context.Background())
	jobs := &dynamicTestJobs{}
	server.hls = &hlsRuntime{server: server, manager: jobs, verify: server.authorizeHLS, ctx: ctx, cancel: cancel,
		sessions: make(map[string]*hlsSession), byKey: make(map[hlsKey]*hlsSession), done: make(chan struct{}), slots: make(chan struct{}, 8)}
	probe := installHardwareEncodingRouteProbe(t, server)
	if _, err := fixture.s.f.pool.Exec(fixture.s.f.ctx, `UPDATE items SET media = media || '{"FormatStartKnown":true,"FormatStartTicks":0}'::jsonb WHERE id=$1`, fixture.s.video.id); err != nil {
		t.Fatal("provide the progressive planner with a controlled source-clock origin")
	}
	profile := func() map[string]any {
		return map[string]any{"EnableDirectPlay": false, "EnableDirectStream": false, "EnableTranscoding": true, "AllowVideoStreamCopy": false,
			"DeviceProfile": map[string]any{"TranscodingProfiles": []map[string]any{{"Type": "Video", "Protocol": "hls", "Container": "mp4", "VideoCodec": "hevc", "AudioCodec": "aac"}}}}
	}
	object, source := fixture.prepare(t, profile())
	if source["SupportsTranscoding"] != true || probe.hardwareProbes.Load() != 1 || len(server.hls.sessions) != 1 || len(jobs.specs) != 0 {
		t.Fatal("the authorized control did not reach hardware admission before registering software output")
	}
	playID := stringValue(t, object, "PlaySessionId")
	playbackPath := "/emby/Items/" + fixture.s.video.id + "/PlaybackInfo"
	videoPath := "/emby/Videos/" + fixture.s.video.id + "/stream.mp4?VideoCodec=hevc&AllowVideoStreamCopy=false&PlaySessionId="
	hlsPath := "/emby/Videos/" + fixture.s.video.id + "/master.m3u8?VideoCodec=hevc&AllowVideoStreamCopy=false&SegmentContainer=mp4&PlaySessionId="

	// A successful HEAD can use the compiled software fallback without starting
	// a hardware probe for a progressive tuple not present in the HLS cache.
	response := fixture.s.f.request(t, http.MethodHead, videoPath+url.QueryEscape(playID), nil, fixture.headers)
	if response.Code != http.StatusOK || probe.hardwareProbes.Load() != 1 || len(jobs.specs) != 0 {
		t.Fatalf("authorized progressive HEAD did not preserve read-only admission: status=%d", response.Code)
	}
	progressiveFound := false
	for _, session := range server.hls.sessions {
		if session.key.plan.OutputMode == "progressive" {
			progressiveFound = session.key.plan.VideoCodec == "hevc" && session.key.plan.Hardware.Encode == "software"
		}
	}
	if !progressiveFound {
		t.Fatal("the authorized progressive HEAD did not retain its declared codec in a software revision")
	}
	response = fixture.s.f.request(t, http.MethodGet, hlsPath+url.QueryEscape(playID), nil, fixture.headers)
	if response.Code != http.StatusOK || probe.hardwareProbes.Load() != 1 || len(jobs.specs) != 0 {
		t.Fatalf("authorized manual HLS control did not reuse admission: status=%d", response.Code)
	}

	for _, test := range []struct {
		name, method, target string
		body                 map[string]any
		headers              http.Header
		status               int
	}{
		{"unauthenticated negotiation", http.MethodPost, playbackPath, profile(), nil, http.StatusUnauthorized},
		{"unauthenticated progressive", http.MethodGet, videoPath + url.QueryEscape(playID), nil, nil, http.StatusUnauthorized},
		{"unauthenticated HLS", http.MethodGet, hlsPath + url.QueryEscape(playID), nil, nil, http.StatusUnauthorized},
		{"foreign progressive reference", http.MethodGet, videoPath + "play_not_owned", nil, fixture.headers, http.StatusNotFound},
		{"foreign HLS reference", http.MethodGet, hlsPath + "play_not_owned", nil, fixture.headers, http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := probe.calls()
			response := fixture.s.f.request(t, test.method, test.target, test.body, test.headers)
			if response.Code != test.status || probe.calls() != before || len(jobs.specs) != 0 {
				t.Fatalf("rejected route reached hardware admission: status=%d, want=%d", response.Code, test.status)
			}
		})
	}
	before := probe.calls()
	body := profile()
	body["CurrentPlaySessionId"] = "play_not_owned"
	response = fixture.s.f.request(t, http.MethodPost, playbackPath, body, fixture.headers)
	if response.Code != http.StatusNotFound || probe.calls() != before || len(jobs.specs) != 0 {
		t.Fatal("foreign current playback reference reached hardware admission")
	}
	fixture.s.setPolicy(t, fixture.s.viewerID, false, []string{fixture.s.video.libraryID})
	for _, target := range []string{videoPath + url.QueryEscape(playID), hlsPath + url.QueryEscape(playID)} {
		response := fixture.s.f.request(t, http.MethodGet, target, nil, fixture.headers)
		if response.Code != http.StatusForbidden || probe.calls() != before || len(jobs.specs) != 0 {
			t.Fatal("revoked playback access reached hardware admission")
		}
	}
	response = fixture.s.f.request(t, http.MethodPost, playbackPath, profile(), fixture.headers)
	if response.Code != http.StatusForbidden || probe.calls() != before || len(jobs.specs) != 0 {
		t.Fatal("revoked playback negotiation reached hardware admission")
	}
}
