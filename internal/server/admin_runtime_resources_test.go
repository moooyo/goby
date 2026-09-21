package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func originalResourceSnapshotForTest(t *testing.T, runtime *originalStreamRuntime) originalResourcesDTO {
	t.Helper()
	value, err := runtime.resourceSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestOriginalResourceLeaseCompletionRequiresActualOnceLeave(t *testing.T) {
	runtime := newOriginalStreamRuntime()
	t.Cleanup(func() { runtime.stop(); runtime.wait() })
	actor := identity.Principal{User: identity.User{ID: "private-owner"}, SessionID: "private-session", Kind: "emby"}
	lifetime, leave, err := runtime.enterSource(actor, "item-1", "mediasource_item-1")
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	initial := originalResourceSnapshotForTest(t, runtime)
	if len(initial.InstanceID) != 32 || len(initial.Current) != 1 || len(initial.Completed) != 0 {
		t.Fatal("actual registration was not projected")
	}
	active := initial.Current[0]
	if active.LeaseID != initial.InstanceID+"-1" || len(active.LeaseID) > 96 || active.Sequence != "1" ||
		active.CompletionSequence != "0" || active.CompletedUnixNano != "0" || !active.Active || active.StartedUnixNano == "0" {
		t.Fatal("registered lease identity or active timestamps differ")
	}
	pending := runtime.retireSource("item-1", "mediasource_item-1")
	if len(pending) != 1 || lifetime.Err() != context.Canceled {
		t.Fatal("source retirement did not request actual cancellation")
	}
	if status := originalResourceSnapshotForTest(t, runtime); status.ActiveCount != 1 || len(status.Completed) != 0 {
		t.Fatal("cancellation was mistaken for completed cleanup")
	}
	leave()
	leave()
	completed := originalResourceSnapshotForTest(t, runtime)
	if completed.ActiveCount != 0 || len(completed.Current) != 0 || len(completed.Completed) != 1 ||
		completed.NextLeaseSequence != "2" || completed.NextCompletionSequence != "2" {
		t.Fatal("once leave lost or duplicated its real event")
	}
	final := completed.Completed[0]
	if final.LeaseID != active.LeaseID || final.Sequence != active.Sequence || final.StartedUnixNano != active.StartedUnixNano ||
		final.Active || final.CompletionSequence != "1" || final.CompletedUnixNano == "0" {
		t.Fatal("completed event did not bind the same lease")
	}
	if _, err := strconv.ParseInt(final.CompletedUnixNano, 10, 64); err != nil {
		t.Fatal("completion time was not an exact int64 string")
	}
	other := newOriginalStreamRuntime()
	defer other.stop()
	if originalResourceSnapshotForTest(t, other).InstanceID == initial.InstanceID {
		t.Fatal("independent runtimes reused an observation identity")
	}
}

func TestOriginalResourceCompletionFollowsExistingPreviewWatcherJoin(t *testing.T) {
	runtime := newOriginalStreamRuntime()
	actor := identity.Principal{User: identity.User{ID: "viewer"}, Kind: "emby"}
	_, leave, err := runtime.enterSource(actor, "item", "source")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	preview := &publishedAnalysisPreview{watched: make(chan struct{}), cancel: func() { close(entered) },
		stopSource: func() bool { return true }, leaveSource: leave, leave: func() {}}
	closed := make(chan error, 1)
	var once sync.Once
	releaseWatcher := func() { once.Do(func() { close(preview.watched) }) }
	t.Cleanup(func() { releaseWatcher(); _ = preview.Close(); runtime.stop(); runtime.wait() })
	go func() { closed <- preview.Close() }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual preview cleanup did not begin")
	}
	if status := originalResourceSnapshotForTest(t, runtime); status.ActiveCount != 1 || len(status.Completed) != 0 {
		t.Fatal("source completion was published before the existing watcher join")
	}
	releaseWatcher()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("preview cleanup did not join")
	}
	if status := originalResourceSnapshotForTest(t, runtime); status.ActiveCount != 0 || len(status.Completed) != 1 {
		t.Fatal("joined preview did not execute its actual source leave")
	}
}

func TestOriginalResourceSnapshotBoundsProjectionWithoutChangingAdmission(t *testing.T) {
	runtime := newOriginalStreamRuntime()
	var leaves []func()
	t.Cleanup(func() {
		for _, leave := range leaves {
			leave()
		}
		runtime.stop()
		runtime.wait()
	})
	for index := 0; index < 65; index++ {
		actor := identity.Principal{User: identity.User{ID: fmt.Sprintf("owner-%d", index)}, Kind: "emby"}
		_, leave, err := runtime.enterSource(actor, fmt.Sprintf("item-%d", index), "source")
		if err != nil {
			t.Fatal("observation projection changed independent owners' admission")
		}
		leaves = append(leaves, leave)
	}
	status := originalResourceSnapshotForTest(t, runtime)
	if status.ActiveCount != 65 || len(status.Current) != 64 || status.CurrentCapacityDropped != 1 ||
		status.Current[0].Sequence != "1" || status.Current[63].Sequence != "64" {
		t.Fatal("bounded current projection concealed omission or lost ordering")
	}
	for _, leave := range leaves {
		leave()
	}
	actor := identity.Principal{User: identity.User{ID: "owner"}, Kind: "emby"}
	for index := 65; index < 131; index++ {
		_, leave, err := runtime.enterSource(actor, "item", "source")
		if err != nil {
			t.Fatal(err)
		}
		leave()
	}
	status = originalResourceSnapshotForTest(t, runtime)
	if status.CurrentCapacityDropped != 0 || len(status.Completed) != 128 || status.CompletionCapacityDropped != "3" ||
		status.OldestCompletionSequence != "4" || status.NextCompletionSequence != "132" {
		t.Fatal("history rollover did not expose its exact loss boundary")
	}
	for index, record := range status.Completed {
		if record.Active || record.CompletionSequence != strconv.Itoa(index+4) {
			t.Fatal("completion ring did not retain real leave order")
		}
	}
	raw, err := json.Marshal(status)
	if err != nil || len(raw) > 128<<10 {
		t.Fatal("bounded runtime projection exceeded its field budget")
	}
}

func TestOriginalResourceLeaveOrderAndSequenceExhaustion(t *testing.T) {
	runtime := newOriginalStreamRuntime()
	actor := identity.Principal{User: identity.User{ID: "owner"}, Kind: "emby"}
	var leaves []func()
	t.Cleanup(func() {
		for _, leave := range leaves {
			leave()
		}
		runtime.stop()
		runtime.wait()
	})
	for index := 0; index < 3; index++ {
		_, leave, err := runtime.enterSource(actor, "item", "source")
		if err != nil {
			t.Fatal(err)
		}
		leaves = append(leaves, leave)
	}
	leaves[2]()
	leaves[0]()
	leaves[1]()
	status := originalResourceSnapshotForTest(t, runtime)
	for index, want := range []string{"3", "1", "2"} {
		if status.Completed[index].Sequence != want || status.Completed[index].CompletionSequence != strconv.Itoa(index+1) {
			t.Fatal("leave order was confused with admission order")
		}
	}
	runtime.mu.Lock()
	runtime.leaseSequence = originalResourceMaxSequence
	runtime.mu.Unlock()
	if _, leave, err := runtime.enterSource(actor, "item", "source"); err == nil || leave != nil {
		t.Fatal("exhausted sequence wrapped to an existing lease")
	}
	if status := originalResourceSnapshotForTest(t, runtime); status.ActiveCount != 0 || status.NextLeaseSequence != strconv.FormatUint(^uint64(0), 10) {
		t.Fatal("sequence exhaustion fabricated a resource or rounded its counter")
	}
}

func TestOriginalResourceMetadataBoundDoesNotChangeSourceLifetime(t *testing.T) {
	runtime := newOriginalStreamRuntime()
	t.Cleanup(func() { runtime.stop(); runtime.wait() })
	actor := identity.Principal{User: identity.User{ID: "owner"}, Kind: "emby"}
	itemID := strings.Repeat("x", 129)
	work, leave, err := runtime.enterSource(actor, itemID, "source")
	if err != nil {
		t.Fatal("diagnostic metadata changed source admission")
	}
	defer leave()
	if _, err := runtime.resourceSnapshot(); err == nil {
		t.Fatal("snapshot exposed an unbounded or silently truncated identifier")
	}
	if pending := runtime.retireSource(itemID, "source"); len(pending) != 1 || work.Err() != context.Canceled {
		t.Fatal("diagnostic bounds changed exact source retirement")
	}
	leave()
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.completed[0].itemID != "" || runtime.completed[0].mediaSourceID != "" {
		t.Fatal("completion history retained rejected metadata")
	}
}
