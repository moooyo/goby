package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type generatedWindowHeadJobs struct {
	*hlsProducerIdentityTestJobs
	record transcode.Record
}

func TestGeneratedWindowLogicalHEADAndGETRejectMetadataOnlyStart(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, h, "logical-head-window", false)
	plan, _, certificate := generatedWindowGraphPlanFixture()
	plan.DurationTicks = 102 * media.TicksPerSecond
	session.key.plan = plan
	session.windowGraph = &hlsGeneratedWindowGraph{published: true, endpoint: certificate,
		basePlan: hlsGeneratedWindowProductionBase(plan, certificate),
		timeline: transcode.Timeline{Segments: make([]transcode.TimelineSegment, 17)}}
	server := &Server{hls: h}
	for _, method := range []string{http.MethodHead, http.MethodGet} {
		for _, start := range []int64{100 * media.TicksPerSecond, 101 * media.TicksPerSecond} {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/controlled-logical-window", nil)
			server.serveGeneratedWindowAdmission(response, request, session, nil, media.Info{}, session.id, 15, 0, "Videos", "", start)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("logical %s admitted a metadata-only source position %d: %d", method, start, response.Code)
			}
		}
	}
	response := httptest.NewRecorder()
	server.serveGeneratedWindowAdmission(response, httptest.NewRequest(http.MethodHead, "/controlled-logical-window", nil),
		session, nil, media.Info{}, session.id, 15, 0, "Videos", "", 90*media.TicksPerSecond)
	if response.Code != http.StatusOK || len(jobs.ensured) != 0 || len(jobs.opens) != 0 || session.admission != nil {
		t.Fatal("valid logical HEAD changed production or acquired a reader")
	}
}

func (jobs *generatedWindowHeadJobs) Snapshot(_ transcode.Scope, id string) (transcode.Record, error) {
	if id != jobs.record.ID {
		return transcode.Record{}, transcode.ErrJobNotFound
	}
	return jobs.record, nil
}

func TestGeneratedWindowHeadChecksResourceRolesWithoutOpeningOrProducing(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &generatedWindowHeadJobs{hlsProducerIdentityTestJobs: &hlsProducerIdentityTestJobs{hlsRuntimeTestJobs: base}}
	h.manager = jobs
	session := hlsRuntimeTestSession(t, h, "head-window", false)
	binding := generatedWindowRuntimeBindingFixture(2)
	session.key.plan = binding.plan
	session.key.plan.DurationTicks = 102 * media.TicksPerSecond
	session.windowGraph = &hlsGeneratedWindowGraph{published: true,
		endpoint: transcode.GeneratedSourceEndpointCertificate{DurationTicks: 100 * media.TicksPerSecond},
		slots:    map[int]hlsGeneratedWindowBinding{15: binding}}
	jobs.record = transcode.Record{ID: binding.producer.id, State: "completed", Spec: transcode.Spec{Plan: binding.plan}}
	for _, fixture := range []struct {
		name, artifact, producer, graph string
		start                           int64
		want                            error
	}{
		{"logical playlist", "v1.m3u8", "", session.id, 90 * media.TicksPerSecond, nil},
		{"untagged current playlist", "v1.m3u8", "", "", 90 * media.TicksPerSecond, nil},
		{"untagged native endpoint", "v1.m3u8", "", "", 100 * media.TicksPerSecond, errHLSRequestInvalid},
		{"untagged metadata-only tail", "v1.m3u8", "", "", 101 * media.TicksPerSecond, errHLSRequestInvalid},
		{"native endpoint", "v1.m3u8", "", session.id, 100 * media.TicksPerSecond, errHLSRequestInvalid},
		{"metadata-only tail", "v1.m3u8", "", session.id, 101 * media.TicksPerSecond, errHLSRequestInvalid},
		{"foreign graph", "v1.m3u8", "", "another-graph", 0, transcode.ErrJobNotFound},
		{"graph raw artifact", binding.artifactNames[1], "", session.id, 0, transcode.ErrJobNotFound},
		{"mixed roles", "v1.m3u8", binding.producer.id, session.id, 0, errHLSRequestInvalid},
		{"exact private artifact", binding.artifactNames[1], binding.producer.id, "", 0, nil},
		{"private playlist", "v1.m3u8", binding.producer.id, "", 0, errHLSRequestInvalid},
		{"unproved private artifact", "v1-segment-000016.ts", binding.producer.id, "", 0, transcode.ErrJobNotFound},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			err := h.checkGeneratedWindowHead(context.Background(), session, fixture.artifact, fixture.producer, fixture.graph, fixture.start)
			if !errors.Is(err, fixture.want) {
				t.Fatalf("HEAD changed the admitted resource role: got=%v want=%v", err, fixture.want)
			}
		})
	}
	if len(jobs.lookups) != 0 || len(jobs.ensured) != 0 || len(jobs.cancels) != 0 || len(jobs.opens) != 0 {
		t.Fatal("HEAD acquired a cache reader, admitted production or changed ownership")
	}
}
