package dynamicsource

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type reconnectInfoResult struct {
	input *Input
	err   error
}

func TestInfoDuringReconnectKeepsCommittedGenerationAndReauthorizes(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var connections, authorizations atomic.Int32
	manager := testManager(t, connectorFunc(func(ctx context.Context, _ Definition) (*Connection, error) {
		facts := testFacts()
		if connections.Add(1) == 2 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			facts.Streams[0].Width, facts.Streams[0].Height = 640, 360
		}
		return &Connection{Reader: io.NopCloser(strings.NewReader("generation bytes")), Info: facts}, nil
	}), func(context.Context, Owner, string, string) error {
		authorizations.Add(1)
		return nil
	}, Options{})
	owner := testOwner()
	first := testOpen(t, manager, owner, "reconnect_info")
	input, err := manager.Acquire(context.Background(), owner, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	results := make(chan reconnectInfoResult, 1)
	go func() {
		input, err := manager.Acquire(context.Background(), owner, first.ID)
		results <- reconnectInfoResult{input, err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not enter its gated upstream opening")
	}
	before := authorizations.Load()
	for range 3 {
		current, err := manager.Info(context.Background(), owner, first.ID)
		if err != nil || current.ID != first.ID || current.Generation != first.Generation ||
			current.Stamp != first.Stamp || current.Info.Streams[0].Width != 1280 {
			t.Fatalf("pending reconnect did not preserve committed facts: generation=%d stamp=%q err=%v", current.Generation, current.Stamp, err)
		}
	}
	if authorizations.Load() != before+3 {
		t.Fatal("committed metadata reads bypassed current authorization while reconnecting")
	}
	foreign := owner
	foreign.SessionID = "foreign_credential"
	if _, err := manager.Info(context.Background(), foreign, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("a foreign credential read the committed reconnect metadata")
	}
	if authorizations.Load() != before+3 {
		t.Fatal("a foreign credential reached the owner's authorization path")
	}
	close(release)
	var next *Input
	select {
	case result := <-results:
		if result.err != nil || result.input == nil {
			t.Fatal("gated reconnect did not complete", result.err)
		}
		next = result.input
	case <-time.After(time.Second):
		t.Fatal("released reconnect did not commit")
	}
	defer next.Close()
	current, err := manager.Info(context.Background(), owner, first.ID)
	if err != nil || current.Generation != first.Generation+1 || current.Stamp == first.Stamp ||
		current.Generation != next.Generation || current.Stamp != next.Stamp || current.Info.Streams[0].Width != 640 ||
		connections.Load() != 2 {
		t.Fatal("successful reconnect did not atomically expose the new generation and facts", err)
	}
}

func TestInfoRevocationDuringReconnectCancelsPendingConnection(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	var connections, authorizations atomic.Int32
	var revoked atomic.Bool
	denied := errors.New("catalog access revoked during reconnect")
	manager := testManager(t, connectorFunc(func(ctx context.Context, _ Definition) (*Connection, error) {
		if connections.Add(1) == 1 {
			return &Connection{Reader: io.NopCloser(strings.NewReader("first generation")), Info: testFacts()}, nil
		}
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}), func(context.Context, Owner, string, string) error {
		authorizations.Add(1)
		if revoked.Load() {
			return denied
		}
		return nil
	}, Options{})
	owner := testOwner()
	first := testOpen(t, manager, owner, "revoked_reconnect_info")
	input, err := manager.Acquire(context.Background(), owner, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	results := make(chan reconnectInfoResult, 1)
	go func() {
		input, err := manager.Acquire(context.Background(), owner, first.ID)
		results <- reconnectInfoResult{input, err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not reach its pending connection")
	}
	before := authorizations.Load()
	revoked.Store(true)
	if _, err := manager.Info(context.Background(), owner, first.ID); !errors.Is(err, denied) {
		t.Fatal("pending reconnect hid a current authorization denial", err)
	}
	if authorizations.Load() != before+1 {
		t.Fatal("revoked committed metadata was not reauthorized exactly once")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("revocation retained the pending upstream connection")
	}
	select {
	case result := <-results:
		if result.input != nil {
			_ = result.input.Close()
			t.Fatal("revoked reconnect handed out new source bytes")
		}
		if result.err == nil {
			t.Fatal("revoked reconnect reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("revoked reconnect did not release its opening operation")
	}
	if _, err := manager.Info(context.Background(), owner, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("revocation left the old generation publicly readable")
	}
	manager.mu.Lock()
	generation, stamp := manager.leases[first.ID].lease.Generation, manager.leases[first.ID].lease.Stamp
	manager.mu.Unlock()
	if generation != first.Generation || stamp != first.Stamp {
		t.Fatal("cancelled reconnect committed a replacement generation")
	}
}

func TestInfoDuringInitialOpeningRemainsBusyWithoutCommittedFacts(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	manager := testManager(t, connectorFunc(func(ctx context.Context, _ Definition) (*Connection, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &Connection{Reader: io.NopCloser(strings.NewReader("initial bytes")), Info: testFacts()}, nil
	}), nil, Options{})
	owner := testOwner()
	description, err := manager.Describe(context.Background(), owner, "42", "")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 1)
	go func() {
		_, err := manager.Open(context.Background(), owner, OpenRequest{OpenToken: description.OpenToken, PlaySessionID: "initial_info"})
		results <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("initial connection did not reach its gate")
	}
	manager.mu.Lock()
	id := ""
	for candidate := range manager.leases {
		id = candidate
	}
	manager.mu.Unlock()
	if id == "" {
		t.Fatal("initial opening did not reserve its lease")
	}
	if _, err := manager.Info(context.Background(), owner, id); !errors.Is(err, ErrBusy) {
		t.Fatal("initial opening exposed uncommitted media facts", err)
	}
	close(release)
	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("initial opening did not finish after release")
	}
	if lease, err := manager.Info(context.Background(), owner, id); err != nil || lease.Generation != 1 || len(lease.Info.Streams) == 0 {
		t.Fatal("initial connection did not publish its first committed facts", err)
	}
}
