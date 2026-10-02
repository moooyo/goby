package transcode

import (
	"context"
	"fmt"
	"testing"
)

// These memory-state tests exercise real cancellation/dedup/context methods.
// They make no filesystem, runner, packet, AUTH or producer-acceptance claim.
func TestManagerCancelFileHLSPlaybackFiltersSharedCanonicalScope(t *testing.T) {
	for _, state := range []string{"creating", "queued", "running", "completed"} {
		t.Run(state, func(t *testing.T) {
			manager := &Manager{jobs: make(map[string]*managedJob), bySpec: make(map[Spec]*managedJob), wake: make(chan struct{}, 1)}
			manager.options.PlaybackAdmission = func(context.Context, Spec) (context.Context, func(), error) {
				t.Fatal("cancellation entered admission")
				return nil, nil, nil
			}
			manager.options.PlaybackStopped = func(Spec) bool { t.Fatal("cancellation entered the optional stopped hook"); return false }
			type candidate struct {
				label, source, output, auth, play string
				stopped                           bool
			}
			for index, candidate := range []candidate{
				{label: "implicit_file_hls", auth: "auth", play: "play", stopped: true},
				{label: "explicit_file_hls", output: "hls", auth: "auth", play: "play", stopped: true},
				{label: "file_progressive", output: "progressive", auth: "auth", play: "play"},
				{label: "dynamic_hls", source: "stream", output: "hls", auth: "auth", play: "play"},
				{label: "dynamic_implicit", source: "stream", auth: "auth", play: "play"},
				{label: "foreign_auth", output: "hls", auth: "other-auth", play: "play"},
				{label: "foreign_play", output: "hls", auth: "auth", play: "other-play"},
			} {
				spec := Spec{Scope: Scope{UserID: "owner", AuthSessionID: candidate.auth, PlaySessionID: candidate.play,
					DeviceID: "device", ItemID: "item", SourceID: "source"}, SourceStamp: "stamp",
					Plan: Plan{SourceMode: candidate.source, OutputMode: candidate.output, StartTicks: int64(index)}}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				job := &managedJob{record: Record{ID: fmt.Sprint(index), Spec: spec, State: state}, ctx: ctx, cancel: cancel,
					changed: make(chan struct{}), finished: state == "completed", running: state == "running", durable: state != "creating"}
				manager.jobs[job.record.ID], manager.bySpec[spec] = job, job
				if state == "queued" {
					manager.queue = append(manager.queue, job)
				}
				previous := job.changed
				manager.CancelFileHLSPlayback("auth", "play")
				if candidate.stopped {
					if job.stopCode != "cancelled" || manager.bySpec[spec] != nil {
						t.Fatalf("%s retained cancellation/dedup admission", candidate.label)
					}
					if state != "completed" && ctx.Err() != context.Canceled {
						t.Fatalf("%s did not cancel its real owned context", candidate.label)
					}
					select {
					case <-previous:
					default:
						t.Fatalf("%s did not notify existing observers", candidate.label)
					}
				} else {
					if job.stopCode != "" || manager.bySpec[spec] != job || ctx.Err() != nil {
						t.Fatalf("%s was cancelled by file-HLS early Stop", candidate.label)
					}
					select {
					case <-previous:
						t.Fatalf("%s received a spurious cancellation notification", candidate.label)
					default:
					}
				}
			}
			// The established post-commit API still cancels every transport of
			// the same canonical play, while retaining foreign authority/plays.
			manager.CancelPlayback("auth", "play")
			for _, job := range manager.jobs {
				scope := job.record.Spec.Scope
				want := scope.AuthSessionID == "auth" && scope.PlaySessionID == "play"
				if (job.stopCode == "cancelled") != want {
					t.Fatal("generic post-commit playback cancellation changed")
				}
			}
		})
	}
}

func TestManagerCancelFileHLSPlaybackEmptyIdentityDoesNotCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	spec := Spec{Scope: Scope{AuthSessionID: "auth", PlaySessionID: "play"}}
	job := &managedJob{record: Record{ID: "job", Spec: spec}, ctx: ctx, cancel: cancel, changed: make(chan struct{})}
	manager := &Manager{jobs: map[string]*managedJob{"job": job}, bySpec: map[Spec]*managedJob{spec: job}, wake: make(chan struct{}, 1)}
	manager.CancelFileHLSPlayback("", "play")
	manager.CancelFileHLSPlayback("auth", "")
	if job.stopCode != "" || ctx.Err() != nil || manager.bySpec[spec] != job {
		t.Fatal("an incomplete cancellation identity retired a job")
	}
}
