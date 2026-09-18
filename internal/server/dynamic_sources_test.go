package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

type dynamicTestConnector struct{}

func (dynamicTestConnector) Open(context.Context, dynamicsource.Definition) (*dynamicsource.Connection, error) {
	return &dynamicsource.Connection{Reader: io.NopCloser(strings.NewReader("authorized upstream bytes")), Info: dynamicTestInfo()}, nil
}

func dynamicTestInfo() media.Info {
	return media.Info{Container: "mpegts", Bitrate: 4_000_000, Streams: []media.Stream{
		{Index: 2, CodecType: "video", Codec: "h264", Bitrate: 3_500_000, Width: 1280, Height: 720, AverageFrameRate: "25/1", InterlaceKnown: true, BitDepth: 8, PixelFormat: "yuv420p"},
		{Index: 5, CodecType: "audio", Codec: "aac", Bitrate: 192_000, Channels: 2, SampleRate: 48000},
	}}
}

type dynamicTestJobs struct {
	mu        sync.Mutex
	specs     []transcode.Spec
	bytes     []string
	cancelled int
	state     string
}

func (*dynamicTestJobs) Health() transcode.Health {
	return transcode.Health{Available: true, Code: "ready"}
}
func (jobs *dynamicTestJobs) Ensure(_ context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	defer input.Close()
	data, err := io.ReadAll(input)
	if err != nil {
		return transcode.Record{}, err
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.specs, jobs.bytes = append(jobs.specs, spec), append(jobs.bytes, string(data))
	return transcode.Record{ID: "dynamic_test_job", Spec: spec, State: "running"}, nil
}
func (jobs *dynamicTestJobs) Snapshot(scope transcode.Scope, id string) (transcode.Record, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	state := jobs.state
	if state == "" {
		state = "running"
	}
	return transcode.Record{ID: id, Spec: transcode.Spec{Scope: scope}, State: state}, nil
}
func (*dynamicTestJobs) TryOpen(transcode.Scope, string, string) (*transcode.ReadHandle, error) {
	return nil, transcode.ErrOutputUnavailable
}
func (*dynamicTestJobs) Open(context.Context, transcode.Scope, string, string) (*transcode.ReadHandle, error) {
	return nil, transcode.ErrOutputUnavailable
}
func (jobs *dynamicTestJobs) CancelJob(string, transcode.Scope) error {
	jobs.mu.Lock()
	jobs.cancelled++
	jobs.mu.Unlock()
	return nil
}
func (*dynamicTestJobs) Close(context.Context) error { return nil }

func dynamicServerFixture(t *testing.T) (*Server, identity.Principal, dynamicsource.Lease, playback.Request, *dynamicTestJobs) {
	t.Helper()
	principal := identity.Principal{User: identity.User{ID: "viewer", Policy: []byte(`{}`)}, SessionID: "auth", Client: identity.Client{DeviceID: "device"}, Kind: "emby", PeerIP: "127.0.0.1"}
	manager, err := dynamicsource.New(context.Background(), []dynamicsource.Definition{{ItemID: "42", URL: "https://configured.invalid/secret-url", Headers: map[string]string{"Authorization": "secret"}, Infinite: true, MaxReconnects: 2}}, dynamicsource.Options{Connector: dynamicTestConnector{}, Authorize: func(context.Context, dynamicsource.Owner, string, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	description, err := manager.Describe(context.Background(), dynamicSourceOwner(principal), "42", "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Open(context.Background(), dynamicSourceOwner(principal), dynamicsource.OpenRequest{ItemID: "42", OpenToken: description.OpenToken, PlaySessionID: "play_one"})
	if err != nil {
		t.Fatal(err)
	}
	jobs := &dynamicTestJobs{}
	lifetime, cancel := context.WithCancel(context.Background())
	server := &Server{cfg: config.Config{Transcoding: config.TranscodingConfig{Enabled: true, MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 8}},
		dynamicSources: manager, dynamicStreams: &dynamicStreamRuntime{sessions: make(map[string]*dynamicStreamSession), byKey: make(map[dynamicStreamKey]*dynamicStreamSession), revalidate: func(_ context.Context, principal identity.Principal) (identity.Principal, error) {
			return principal, nil
		}},
		hls: &hlsRuntime{ctx: lifetime, cancel: cancel, manager: jobs, slots: make(chan struct{}, 8)}}
	t.Cleanup(func() {
		cancel()
		ctx, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		if err := server.closeDynamicSources(ctx); err != nil {
			t.Error(err)
		}
	})
	request := playback.Request{ID: "42", MediaSourceID: media.SourceID("42"), CurrentPlaySessionID: "play_one", DeviceProfile: &playback.DeviceProfile{TranscodingProfiles: []playback.TranscodingProfile{{Type: playback.DlnaProfileTypeVideo, Protocol: "hls", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"}}}}
	return server, principal, lease, request, jobs
}

func dynamicTestRequest(principal identity.Principal, method, address string) *http.Request {
	request := httptest.NewRequest(method, address, nil)
	return request.WithContext(context.WithValue(request.Context(), principalKey, principal))
}

func TestDynamicNegotiationAndHeadDoNotStartAnEncoder(t *testing.T) {
	server, principal, lease, request, jobs := dynamicServerFixture(t)
	httpRequest := dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open?api_key=owned-token")
	dto, err := server.dynamicPlaybackDTO(httpRequest, principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(dto["TranscodingUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if target.Host != "" || target.Query().Get("MediaSourceId") != lease.SourceID || !strings.Contains(target.Path, lease.ID) || strings.Contains(target.String(), "secret") || len(jobs.specs) != 0 {
		t.Fatal("negotiation opened an encoder or exposed upstream configuration")
	}
	head := dynamicTestRequest(principal, http.MethodHead, target.String())
	head.SetPathValue("LiveStreamId", lease.ID)
	head.SetPathValue("Artifact", "master.m3u8")
	response := httptest.NewRecorder()
	server.dynamicHLSArtifact(response, head)
	if response.Code != http.StatusOK || response.Body.Len() != 0 || len(jobs.specs) != 0 {
		t.Fatal("master HEAD emitted bytes or started an encoder")
	}
	head.SetPathValue("Artifact", "main.m3u8")
	response = httptest.NewRecorder()
	server.dynamicHLSArtifact(response, head)
	if response.Code != http.StatusNotFound || len(jobs.specs) != 0 {
		t.Fatal("uncached media HEAD started an encoder")
	}
}

func TestDynamicJobConsumesOnlyAuthorizedPipeAndRetiresFailedRevision(t *testing.T) {
	server, principal, lease, request, jobs := dynamicServerFixture(t)
	httpRequest := dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open?api_key=owned-token")
	dto, err := server.dynamicPlaybackDTO(httpRequest, principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(dto["TranscodingUrl"].(string))
	get := dynamicTestRequest(principal, http.MethodGet, target.String())
	get.SetPathValue("LiveStreamId", lease.ID)
	values, _ := hlsValues(get)
	session, _, err := server.findDynamicSession(get, values)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil {
		t.Fatal(err)
	}
	if len(jobs.specs) != 1 || jobs.bytes[0] != "authorized upstream bytes" || jobs.specs[0].Plan.SourceMode != "stream" || jobs.specs[0].Plan.DurationTicks != 0 || jobs.specs[0].SourceStamp != lease.Stamp || jobs.specs[0].Scope.AuthSessionID != principal.SessionID {
		t.Fatal("dynamic source was not converted from its owned pipe with source-generation scope")
	}
	jobs.mu.Lock()
	jobs.state = "completed"
	jobs.mu.Unlock()
	if _, err := server.ensureDynamicJob(get.Context(), get, session); !errors.Is(err, dynamicsource.ErrUnavailable) {
		t.Fatal("ended ongoing input reused a cached presentation")
	}
	if !session.closed || jobs.cancelled == 0 {
		t.Fatal("ended ongoing output revision was not retired")
	}
	next, err := server.dynamicMediaInfoDTO(httpRequest, principal, lease)
	if err != nil {
		t.Fatal(err)
	}
	if next["TranscodingUrl"] == dto["TranscodingUrl"] {
		t.Fatal("reconnection reused old segment URLs")
	}
}

func TestDynamicOutputOwnershipCannotBeChangedByURL(t *testing.T) {
	server, principal, lease, request, _ := dynamicServerFixture(t)
	dto, err := server.dynamicPlaybackDTO(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	target := dto["TranscodingUrl"].(string)
	foreign := principal
	foreign.SessionID = "foreign-auth"
	get := dynamicTestRequest(foreign, http.MethodGet, target)
	get.SetPathValue("LiveStreamId", lease.ID)
	values, _ := hlsValues(get)
	if _, _, err := server.findDynamicSession(get, values); !errors.Is(err, dynamicsource.ErrNotFound) {
		t.Fatal("foreign credential obtained an output revision")
	}
	get = dynamicTestRequest(principal, http.MethodGet, target)
	get.SetPathValue("LiveStreamId", lease.ID)
	values["playsessionid"] = "foreign-play"
	if _, _, err := server.findDynamicSession(get, values); !errors.Is(err, dynamicsource.ErrNotFound) {
		t.Fatal("query changed the prepared playback owner")
	}
}

func TestDynamicMediaInfoReplacesEndedOutputBeforeAnotherArtifactRequest(t *testing.T) {
	server, principal, lease, request, jobs := dynamicServerFixture(t)
	httpRequest := dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/MediaInfo")
	dto, err := server.dynamicPlaybackDTO(httpRequest, principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(dto["TranscodingUrl"].(string))
	session := server.dynamicStreams.sessions[target.Query().Get("GobyLiveId")]
	get := dynamicTestRequest(principal, http.MethodGet, target.String())
	if _, err := server.ensureDynamicJob(get.Context(), get, session); err != nil {
		t.Fatal(err)
	}
	jobs.mu.Lock()
	jobs.state = "completed"
	jobs.mu.Unlock()
	next, err := server.dynamicMediaInfoDTO(httpRequest, principal, lease)
	if err != nil {
		t.Fatal(err)
	}
	if next["TranscodingUrl"] == dto["TranscodingUrl"] || !session.closed {
		t.Fatal("explicit media refresh retained the completed ongoing output revision")
	}
}

func TestDynamicArtifactRevalidatesPolicyAndRejectsUnnegotiatedQueryEdits(t *testing.T) {
	server, principal, lease, request, _ := dynamicServerFixture(t)
	dto, err := server.dynamicPlaybackDTO(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	get := dynamicTestRequest(principal, http.MethodGet, dto["TranscodingUrl"].(string))
	get.SetPathValue("LiveStreamId", lease.ID)
	values, _ := hlsValues(get)
	values["videobitrate"] = "800000"
	if _, _, err := server.findDynamicSession(get, values); !errors.Is(err, dynamicsource.ErrInvalid) {
		t.Fatal("immutable output URL accepted a new encoding setting")
	}
	delete(values, "videobitrate")
	server.dynamicStreams.revalidate = func(_ context.Context, prior identity.Principal) (identity.Principal, error) {
		prior.User.Policy = []byte(`{"EnablePlaybackRemuxing":false,"EnableVideoPlaybackTranscoding":false,"EnableAudioPlaybackTranscoding":false}`)
		return prior, nil
	}
	if _, _, err := server.findDynamicSession(get, values); err == nil {
		t.Fatal("output delivery used the original request's obsolete policy")
	}
}

func TestDynamicOpenWithoutOptionalProfileReturnsObservedMediaInfo(t *testing.T) {
	server, principal, lease, request, jobs := dynamicServerFixture(t)
	request.DeviceProfile = nil
	dto, err := server.dynamicPlaybackDTO(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	if dto["LiveStreamId"] != lease.ID || dto["RequiresClosing"] != true || dto["RequiresOpening"] != false || len(jobs.specs) != 0 {
		t.Fatal("metadata-only opening did not retain its real source lease")
	}
	if _, exists := dto["TranscodingUrl"]; exists {
		t.Fatal("opening without a client profile invented an output capability")
	}
}
