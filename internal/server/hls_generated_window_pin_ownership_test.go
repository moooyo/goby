//go:build linux

package server

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

func generatedWindowPinOwnerCount(h *hlsRuntime) int {
	h.generatedWindowPinMu.Lock()
	defer h.generatedWindowPinMu.Unlock()
	return len(h.generatedWindowPinOwners)
}

func TestGeneratedWindowPinOwnershipRejectsValidCrossRegistrationAlias(t *testing.T) {
	h, first, _, source, info := generatedWindowSlotActualFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binding, pin, err := h.generatedWindowSlot(ctx, first, source, info, first.id, 0, 0)
	if pin != nil {
		t.Cleanup(func() { _ = pin.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, first, first.id, binding, pin); err != nil {
		t.Fatal(err)
	}
	secondContext, stopSecond := context.WithCancel(h.ctx)
	t.Cleanup(stopSecond)
	secondKey := first.key
	secondKey.remote = !secondKey.remote
	second := &hlsSession{id: "second-valid-window-registration", key: secondKey, ctx: secondContext, cancel: stopSecond,
		principal: first.principal, accessed: time.Now()}
	producer, err := h.ownProducer(second.key.scope, binding.producer.id, hlsGeneratedWindowProducer, hlsGeneratedWindowProducer)
	if err != nil {
		t.Fatal(err)
	}
	secondBinding := binding
	secondBinding.producer = producer
	second.producers = []hlsProducer{producer}
	second.windowGraph = &hlsGeneratedWindowGraph{published: true, endpoint: first.windowGraph.endpoint,
		timeline: first.windowGraph.timeline, basePlan: first.windowGraph.basePlan,
		slots: map[int]hlsGeneratedWindowBinding{0: secondBinding}}
	h.mu.Lock()
	h.sessions[second.id], h.byKey[second.key] = second, second
	h.byScope[second.key.scope][second.id] = second
	h.mu.Unlock()
	t.Cleanup(func() { h.retire(second) })
	if err := h.commitGeneratedWindowRedirect(ctx, second, second.id, secondBinding, pin); !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatalf("a valid second graph borrowed the first graph's reader ownership: %v", err)
	}
	if h.generatedWindowPinOwner(pin) != first || generatedWindowPinOwnerCount(h) != 1 || second.windowGraph.slots[0].redirectPins[0] != nil {
		t.Fatal("cross-registration alias acquired a second owner or changed the original pin charge")
	}
	if _, err := pin.Stat(); err != nil {
		t.Fatal("rejecting a valid foreign alias closed its original graph's reader")
	}
	// Another registration may independently open the same immutable bytes.
	// The object, rather than only its encoding ID or artifact identity, owns
	// its one reader charge and may therefore be retained by that registration.
	secondPin, err := h.generatedWindowExactArtifact(ctx, second, binding.producer.id, binding.artifactName)
	if secondPin != nil {
		t.Cleanup(func() { _ = secondPin.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if secondPin == pin {
		t.Fatal("independent manager opens returned one reader ownership object")
	}
	if err := h.commitGeneratedWindowRedirect(ctx, second, second.id, secondBinding, secondPin); err != nil {
		t.Fatal(err)
	}
	if generatedWindowPinOwnerCount(h) != 2 {
		t.Fatal("independent retained readers did not receive separate bounded index entries")
	}
	h.retire(second)
	if generatedWindowPinOwnerCount(h) != 1 || h.generatedWindowPinOwner(pin) != first {
		t.Fatal("retiring the second registration removed another graph's ownership")
	}
	if _, err := secondPin.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("retiring the second registration retained its actual reader")
	}
	if _, err := pin.Stat(); err != nil {
		t.Fatal("retiring the second registration closed the first graph's reader")
	}
	first.mu.Lock()
	h.releaseGeneratedWindowPinsLocked(first)
	first.mu.Unlock()
	if generatedWindowPinOwnerCount(h) != 0 {
		t.Fatal("full retained-pin cleanup left an ownership index charge")
	}
}

func TestGeneratedWindowPinOwnershipLifecycleDrainsExactIndexEntries(t *testing.T) {
	for _, mode := range []string{"expiry", "discard", "removed_lease", "stop", "close"} {
		t.Run(mode, func(t *testing.T) {
			h, session, _, source, info := generatedWindowSlotActualFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			binding, pin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
			if pin != nil {
				t.Cleanup(func() { _ = pin.Close() })
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, binding, pin); err != nil {
				t.Fatal(err)
			}
			if generatedWindowPinOwnerCount(h) != 1 {
				t.Fatal("retaining an actual reader did not record its bounded owner")
			}
			switch mode {
			case "expiry":
				session.mu.Lock()
				h.releaseExpiredGeneratedWindowPinsLocked(session, time.Now().Add(2*hlsGeneratedWindowGrace))
				session.mu.Unlock()
			case "discard":
				session.mu.Lock()
				h.discardGeneratedWindowBindingLocked(session, 0, binding)
				session.mu.Unlock()
			case "removed_lease":
				h.releaseProducer(session.key.scope, binding.producer)
				session.mu.Lock()
				h.removeGeneratedWindowBindingsLocked(session)
				session.mu.Unlock()
			case "stop":
				h.retire(session)
			case "close":
				// This adapter fixture constructs the runtime directly, without
				// the production constructor's shutdown completion channel.
				h.done = make(chan struct{})
				if err := h.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if generatedWindowPinOwnerCount(h) != 0 || h.generatedWindowPinOwner(pin) != nil {
				t.Fatal("reader lifecycle cleanup retained an index owner after physical cleanup")
			}
			if _, err := pin.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("reader lifecycle cleanup left its descriptor open: %v", err)
			}
		})
	}
}
