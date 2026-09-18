package server

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestHLSSegmentHEADDoesNotStartProducerOrReserveMediaDelivery(t *testing.T) {
	runtime, jobs := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, runtime, "head", false)
	input := hlsRuntimeInput(t)
	handle, err := runtime.segmentHeader(context.Background(), session, input, 0)
	if err != nil || handle != nil || len(jobs.ensured) != 0 || len(session.producers) != 0 {
		t.Fatalf("uncached HEAD started media production: handle=%v err=%v jobs=%d", handle, err, len(jobs.ensured))
	}
	if _, err := input.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("HEAD retained its source descriptor")
	}
	if _, err := runtime.segmentHeader(context.Background(), session, hlsRuntimeInput(t), len(session.timeline.Segments)); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("HEAD accepted an out-of-range segment")
	}
}

func TestHLSRegistrySeparatesTrustedLocalAndRemotePolicyContexts(t *testing.T) {
	fixture := newAudioRuntimeFixture(t)
	local := fixture.principal
	local.PeerIP = "127.0.0.1"
	localSession, err := fixture.h.register(local, fixture.source, fixture.session.key.scope.PlaySessionID, fixture.decision, 0)
	if err != nil {
		t.Fatal(err)
	}
	if localSession.id == fixture.session.id || localSession.key.remote {
		t.Fatal("local admission inherited a remote policy snapshot")
	}
	if _, err := fixture.h.find(localSession.id, fixture.principal, fixture.source.Item.ID); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("a remote request reused a session authorized for the local network")
	}
	if _, err := fixture.h.find(localSession.id, local, fixture.source.Item.ID); err != nil {
		t.Fatal(err)
	}
}
