package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

type hlsProducerIdentityTestJobs struct {
	*hlsRuntimeTestJobs
	lookups []hlsRuntimeOpenCall
}

func (jobs *hlsProducerIdentityTestJobs) TryOpen(scope transcode.Scope, id, name string) (*transcode.ReadHandle, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.lookups = append(jobs.lookups, hlsRuntimeOpenCall{scope: scope, id: id, name: name})
	return nil, transcode.ErrOutputUnavailable
}

func TestGeneratedProducerArtifactURLsKeepExactIdentityAndAuthority(t *testing.T) {
	session := &hlsSession{id: "generated-revision", key: hlsKey{scope: transcode.Scope{
		ItemID: "item", SourceID: "source", PlaySessionID: "play", DeviceID: "device"}}}
	const producerID = "producer+/&"
	for _, name := range []string{"init.mp4", "segment-000000.m4s", "subtitle-0-segment-000000.vtt"} {
		value := hlsProducerArtifactURL(session, "Videos", name, "credential+/&", 10, producerID)
		parsed, err := url.Parse(value)
		if err != nil || parsed.Query().Get(hlsProducerQuery) != producerID || parsed.Query().Get("api_key") != "credential+/&" ||
			parsed.Query().Get("PlaySessionId") != "play" || parsed.Query().Get("GobyHlsId") != session.id {
			t.Fatalf("published artifact lost its producer or authority binding: %s/%v", value, err)
		}
	}
}

func TestGeneratedProducerBoundMissNeverAdmitsReplacement(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsProducerIdentityTestJobs{hlsRuntimeTestJobs: base}
	h.manager = jobs
	session := hlsRuntimeTestSession(t, h, "bound-miss", false)
	session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
	producer, err := h.ownProducer(session.key.scope, "published-producer", -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	session.producers = []hlsProducer{producer}
	input := hlsRuntimeInput(t)
	for _, name := range []string{"main.m3u8", "init.mp4", "segment-000000.m4s"} {
		if handle, err := h.generatedArtifact(context.Background(), session, input, name, producer.id); handle != nil || !errors.Is(err, transcode.ErrOutputUnavailable) {
			t.Fatalf("bound cache miss created or borrowed future output: %v", err)
		}
	}
	jobs.mu.Lock()
	lookups, ensures := append([]hlsRuntimeOpenCall(nil), jobs.lookups...), len(jobs.ensured)
	jobs.mu.Unlock()
	if len(lookups) != 3 || ensures != 0 {
		t.Fatal("bound resources switched to an unbound admission path")
	}
	for _, lookup := range lookups {
		if lookup.id != producer.id || lookup.scope != session.key.scope {
			t.Fatal("bound resources crossed a producer or ownership scope")
		}
	}
	if _, err := input.Stat(); err != nil {
		t.Fatal("a bound lookup consumed the caller's borrowed source")
	}
}

func TestGeneratedProducerUnknownAndReleasedIdentityCannotReachCache(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsProducerIdentityTestJobs{hlsRuntimeTestJobs: base}
	h.manager = jobs
	session := hlsRuntimeTestSession(t, h, "bound-owner", false)
	session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
	producer, err := h.ownProducer(session.key.scope, "known-producer", -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	session.producers = []hlsProducer{producer}
	input := hlsRuntimeInput(t)
	if _, err := h.generatedArtifact(context.Background(), session, input, "init.mp4", "another-producer"); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatalf("unregistered producer identity reached output: %v", err)
	}
	h.releaseProducer(session.key.scope, producer)
	if _, err := h.generatedArtifact(context.Background(), session, input, "init.mp4", producer.id); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatalf("released producer identity reached output: %v", err)
	}
	jobs.mu.Lock()
	lookups, ensures := len(jobs.lookups), len(jobs.ensured)
	jobs.mu.Unlock()
	if lookups != 0 || ensures != 0 {
		t.Fatal("unowned identity performed a cache lookup or admitted work")
	}
}

func TestGeneratedProducerPausedColdGraphDoesNotBootstrap(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, h, "paused-generated", false)
	session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 1, "Paused", 0))
	input := hlsRuntimeInput(t)
	if _, err := h.generatedArtifact(context.Background(), session, input, "main.m3u8"); !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatalf("committed pause admitted a cold generated graph: %v", err)
	}
	jobs.mu.Lock()
	ensures := len(jobs.ensured)
	jobs.mu.Unlock()
	if ensures != 0 || h.admissions != 0 || session.ctx.Err() != nil {
		t.Fatal("pause lost its registry presence or created new production")
	}
}

func TestGeneratedProducerPauseFencesLateAdmissionWithoutSealingAttachedOutput(t *testing.T) {
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "pause-pending-generated", false)
	session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
	input := hlsRuntimeInput(t)
	result := make(chan error, 1)
	go func() {
		_, err := h.generatedArtifact(context.Background(), session, input, "main.m3u8")
		result <- err
	}()
	call := hlsAdmissionCall(t, jobs)
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 1, "Paused", 0))
	hlsAdmissionResult(t, result, context.Canceled)
	call.finish()
	h.workers.Wait()
	if _, err := jobs.Snapshot(session.key.scope, call.id); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatalf("late paused admission became an attached generated producer: %v", err)
	}
	if len(session.producers) != 0 || session.ctx.Err() != nil {
		t.Fatal("paused late admission attached or retired the complete output revision")
	}
	if _, err := input.Stat(); err != nil {
		t.Fatal("paused generated admission consumed the borrowed HTTP source")
	}
}

func TestGeneratedProducerExplicitEmptySelectorCannotBootstrap(t *testing.T) {
	request := httptest.NewRequest("GET", "/emby/Videos/item/main.m3u8?GobyHlsProducerId=", nil)
	if _, err := hlsValues(request); !errors.Is(err, errHLSRequestInvalid) {
		t.Fatalf("empty bound selector was treated as an unbound graph: %v", err)
	}
}
