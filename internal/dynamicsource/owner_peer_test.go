package dynamicsource

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLeasePeerChangesPreserveIdentityAndUseCurrentPolicyContext(t *testing.T) {
	owner := testOwner()
	owner.PeerIP = "192.168.1.20"
	expectedPeer := owner.PeerIP
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
	}), func(_ context.Context, current Owner, _, _ string) error {
		if current.PeerIP != expectedPeer {
			t.Fatalf("source authorization received peer %q; want current peer %q", current.PeerIP, expectedPeer)
		}
		return nil
	}, Options{MaxOwnerLeases: 1})
	first := testOpen(t, manager, owner, "play_one")
	moved := owner
	moved.PeerIP = "8.8.8.8"
	expectedPeer = moved.PeerIP
	duplicate := testOpen(t, manager, moved, "play_one")
	if duplicate.ID != first.ID || owner.Identity() != moved.Identity() {
		t.Fatal("an allowed peer change split one credential's lease identity")
	}
	if _, err := manager.Info(context.Background(), moved, first.ID); err != nil {
		t.Fatalf("current peer could not read its existing lease: %v", err)
	}
	description, err := manager.Describe(context.Background(), moved, "42", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), moved, OpenRequest{
		ItemID: "42", OpenToken: description.OpenToken, PlaySessionID: "play_two",
	}); !errors.Is(err, ErrBusy) {
		t.Fatalf("peer change bypassed the credential's lease quota: %v", err)
	}
	input, err := manager.Acquire(context.Background(), moved, first.ID)
	if err != nil {
		t.Fatalf("current peer could not acquire its existing lease: %v", err)
	}
	defer input.Close()
	if err := manager.CloseLease(context.Background(), moved, first.ID); err != nil {
		t.Fatalf("peer change stranded an owned lease: %v", err)
	}
	if _, err := manager.Info(context.Background(), owner, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("closed lease remained accessible through its original peer: %v", err)
	}
}
