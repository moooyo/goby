package library

import (
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestStorePlaybackDemandRevisionOrdersCommittedReportsAndExcludesPing(t *testing.T) {
	ctx, pool, store, owner, ids := playSessionFixture(t, 1)
	prepared := playSessionPrepare(t, ctx, store, owner, ids[0], "")
	if prepared.PlaybackRevision != 0 {
		t.Fatal("preparation invented a playback report revision")
	}
	started, data := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	if started.PlaybackRevision != 1 || data.PlayCount != 1 {
		t.Fatalf("Started did not establish one committed revision and play count: %+v/%+v", started, data)
	}
	duplicate, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Started"})
	if duplicate.PlaybackRevision != started.PlaybackRevision {
		t.Fatal("idempotent Started advanced demand")
	}
	paused, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress",
		EventName: "Pause", PositionTicks: playSessionPosition(120 * media.TicksPerSecond)})
	if paused.State != "Paused" || paused.PlaybackRevision != 2 {
		t.Fatalf("Pause hint did not persist before returning demand: %+v", paused)
	}
	beforePlay, beforeData := playSessionSnapshot(t, ctx, pool, paused)
	ping, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Ping", EventName: "Unpause"})
	_, afterData := playSessionSnapshot(t, ctx, pool, paused)
	if ping.State != "Paused" || ping.PositionTicks != paused.PositionTicks || ping.PlaybackRevision != paused.PlaybackRevision || beforeData != afterData {
		t.Fatal("presence Ping changed demand or watched state")
	}
	if afterPlay, _ := playSessionSnapshot(t, ctx, pool, paused); afterPlay == beforePlay {
		t.Fatal("presence Ping did not refresh the existing lease")
	}
	results := playSessionParallelReports(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID,
		Event: "Progress", EventName: "Unpause", IsPaused: true, PositionTicks: playSessionPosition(360 * media.TicksPerSecond)}, 8)
	seen := make(map[int64]bool)
	for _, result := range results {
		if result.err != nil || result.session.State != "Playing" || result.session.PlaybackRevision < 3 || result.session.PlaybackRevision > 10 || seen[result.session.PlaybackRevision] {
			t.Fatalf("committed reports did not have distinct row-lock ordered revisions: %+v", result)
		}
		seen[result.session.PlaybackRevision] = true
	}
	current, err := store.GetPlaybackSession(ctx, owner, prepared.ID)
	if err != nil || current.PlaybackRevision != 10 {
		t.Fatalf("fresh validation lost the committed revision: %+v/%v", current, err)
	}
	stopped, data := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Stopped"})
	late, _ := playSessionReport(t, ctx, store, owner, PlaybackReport{PlaySessionID: prepared.ID, Event: "Progress", EventName: "Unpause"})
	if stopped.PlaybackRevision != 11 || late.PlaybackRevision != 11 || late.State != "Stopped" || data.PlayCount != 1 {
		t.Fatal("terminal idempotency or watched count changed with demand revisions")
	}
}
