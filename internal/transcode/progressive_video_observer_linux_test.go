//go:build linux

package transcode

import "testing"

func TestProgressiveObserverDispatchesRealVideoPrefixes(t *testing.T) {
	data, mediaStart := progressiveVideoRealFixture(t)
	var events []Progress
	observer, err := newProgressiveObserver(t.TempDir(), nil, progressiveVideoRealPlan(), func(progress Progress) {
		events = append(events, progress)
	}, func() { t.Error("manual observer inspection unexpectedly cancelled a process") })
	if err != nil {
		t.Fatal(err)
	}
	defer observer.file.Close()
	if _, err := observer.file.Write(data[:mediaStart]); err != nil {
		t.Fatal(err)
	}
	observer.report(Progress{OutputTicks: ticksPerSecond, Bytes: int64(mediaStart), Ready: true})
	if err := observer.inspect(false); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Ready || observer.last.Ready {
		t.Fatal("progress metadata or container headers invented video readiness")
	}
	if _, err := observer.file.Write(data[mediaStart:]); err != nil {
		t.Fatal(err)
	}
	if err := observer.inspect(false); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || !events[1].Ready || events[1].Bytes != int64(len(data)) {
		t.Fatal("the observer did not publish verified video readiness")
	}
	observer.report(Progress{})
	if !events[len(events)-1].Ready || events[len(events)-1].Bytes != int64(len(data)) {
		t.Fatal("a later FFmpeg progress report cleared established video readiness")
	}
	if err := observer.inspect(true); err != nil {
		t.Fatal(err)
	}
}
