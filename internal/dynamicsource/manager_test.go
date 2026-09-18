package dynamicsource

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type connectorFunc func(context.Context, Definition) (*Connection, error)

func (function connectorFunc) Open(ctx context.Context, definition Definition) (*Connection, error) {
	return function(ctx, definition)
}

type countedReader struct {
	io.Reader
	once   sync.Once
	closed chan struct{}
}

func (reader *countedReader) Close() error {
	reader.once.Do(func() { close(reader.closed) })
	return nil
}

func testFacts() media.Info {
	return media.Info{ProbeVersion: media.CurrentProbeVersion, Container: "mpegts", Streams: []media.Stream{
		{Index: 3, Codec: "h264", CodecType: "video", Width: 1280, Height: 720},
		{Index: 8, Codec: "aac", CodecType: "audio", Channels: 2, SampleRate: 48000},
	}}
}

func testOwner() Owner { return Owner{UserID: "viewer", SessionID: "auth", DeviceID: "device"} }

func testManager(t *testing.T, connector Connector, authorize func(context.Context, Owner, string, string) error, options Options) *Manager {
	t.Helper()
	options.Connector = connector
	if authorize == nil {
		authorize = func(context.Context, Owner, string, string) error { return nil }
	}
	options.Authorize = authorize
	manager, err := New(context.Background(), []Definition{{ItemID: "42", Name: "Catalog stream", URL: "https://example.invalid/source", Infinite: true, MaxReconnects: 2}}, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return manager
}

func testOpen(t *testing.T, manager *Manager, owner Owner, playID string) Lease {
	t.Helper()
	description, err := manager.Describe(context.Background(), owner, "42", "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Open(context.Background(), owner, OpenRequest{ItemID: "42", OpenToken: description.OpenToken, PlaySessionID: playID})
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestLeaseOwnershipStableIdentityReconnectAndIdempotentClose(t *testing.T) {
	var opened atomic.Int32
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		opened.Add(1)
		return &Connection{Reader: io.NopCloser(strings.NewReader("authorized stream bytes")), Info: testFacts()}, nil
	}), nil, Options{})
	owner := testOwner()
	first := testOpen(t, manager, owner, "play_one")
	duplicate := testOpen(t, manager, owner, "play_one")
	if first.ID != duplicate.ID || first.SourceID != media.SourceID("42") || opened.Load() != 1 {
		t.Fatal("idempotent opening did not reuse the owned source")
	}
	foreign := owner
	foreign.SessionID = "another-auth"
	if _, err := manager.Info(context.Background(), foreign, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign credential read a lease")
	}
	if err := manager.CloseLease(context.Background(), foreign, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign credential closed a lease")
	}
	input, err := manager.Acquire(context.Background(), owner, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Acquire(context.Background(), owner, first.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("one connection was given to two readers")
	}
	pipe, err := input.OpenPipe()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(pipe)
	_ = pipe.Close()
	_ = input.Close()
	if err != nil || string(data) != "authorized stream bytes" {
		t.Fatal("source pipe changed the authorized bytes")
	}
	second, err := manager.Acquire(context.Background(), owner, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.SourceID != first.SourceID || second.Generation != 2 || second.Stamp == first.Stamp || opened.Load() != 2 {
		t.Fatal("reconnect did not preserve identity and fence the old encoder generation")
	}
	_ = second.Close()
	if err := manager.CloseLease(context.Background(), owner, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.CloseLease(context.Background(), owner, first.ID); err != nil {
		t.Fatal("owned close was not idempotent")
	}
	if _, err := manager.Info(context.Background(), owner, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("closed lease remained readable")
	}
}

func TestRevokedCatalogAccessClosesHeldSource(t *testing.T) {
	var revoked atomic.Bool
	denied := errors.New("catalog permission revoked")
	reader := &countedReader{Reader: strings.NewReader("stream"), closed: make(chan struct{})}
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		return &Connection{Reader: reader, Info: testFacts()}, nil
	}), func(_ context.Context, _ Owner, item, play string) error {
		if item != "42" {
			t.Error("authorization used a different catalog item")
		}
		if revoked.Load() {
			return denied
		}
		return nil
	}, Options{})
	lease := testOpen(t, manager, testOwner(), "play_one")
	revoked.Store(true)
	if _, err := manager.Info(context.Background(), testOwner(), lease.ID); !errors.Is(err, denied) {
		t.Fatal("lease ignored the current catalog policy")
	}
	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Fatal("revocation retained the upstream connection")
	}
}

func TestCanceledOpenCannotRetainAnUndeliveredConnection(t *testing.T) {
	entered := make(chan struct{})
	reader := &countedReader{Reader: strings.NewReader("stream"), closed: make(chan struct{})}
	manager := testManager(t, connectorFunc(func(ctx context.Context, _ Definition) (*Connection, error) {
		close(entered)
		<-ctx.Done()
		return &Connection{Reader: reader, Info: testFacts()}, nil
	}), nil, Options{})
	description, err := manager.Describe(context.Background(), testOwner(), "42", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := manager.Open(ctx, testOwner(), OpenRequest{OpenToken: description.OpenToken, PlaySessionID: "play_one"})
		result <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("open error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("opening did not observe cancellation")
	}
	select {
	case <-reader.closed:
	case <-time.After(time.Second):
		t.Fatal("canceled opening stranded an upstream reader")
	}
}

func TestConcurrentOpeningAndLeaseCapacityStayBounded(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	manager := testManager(t, connectorFunc(func(ctx context.Context, _ Definition) (*Connection, error) {
		if count.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
	}), nil, Options{MaxLeases: 1, MaxOwnerLeases: 1})
	description, _ := manager.Describe(context.Background(), testOwner(), "42", "")
	request := OpenRequest{OpenToken: description.OpenToken, PlaySessionID: "play_one"}
	results := make(chan Lease, 2)
	errorsFound := make(chan error, 2)
	open := func() {
		lease, err := manager.Open(context.Background(), testOwner(), request)
		results <- lease
		errorsFound <- err
	}
	go open()
	<-entered
	go open()
	if _, err := manager.Open(context.Background(), testOwner(), OpenRequest{OpenToken: description.OpenToken, PlaySessionID: "play_two"}); !errors.Is(err, ErrBusy) {
		t.Fatal("pending opens did not reserve capacity")
	}
	close(release)
	first, second := <-results, <-results
	if err := <-errorsFound; err != nil {
		t.Fatal(err)
	}
	if err := <-errorsFound; err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || count.Load() != 1 {
		t.Fatal("concurrent duplicate opens created separate connections")
	}
}

func TestUnknownSourceAndUntrustedOpeningTokenNeverConnect(t *testing.T) {
	var opened atomic.Int32
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) { opened.Add(1); return nil, ErrUnavailable }), nil, Options{})
	if _, err := manager.Describe(context.Background(), testOwner(), "unknown", ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown source did not fail closed")
	}
	if _, err := manager.Open(context.Background(), testOwner(), OpenRequest{ItemID: "42", OpenToken: "https://untrusted.invalid/media", PlaySessionID: "play_one"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("request URL was accepted as an opening token")
	}
	if opened.Load() != 0 {
		t.Fatal("untrusted input reached the upstream connector")
	}
}

func TestSourceFactsAreCopiedAndCredentialRevocationCancelsEveryPlay(t *testing.T) {
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
	}), nil, Options{})
	one := testOpen(t, manager, testOwner(), "play_one")
	two := testOpen(t, manager, testOwner(), "play_two")
	one.Info.Streams[0].Codec = "untrusted"
	current, err := manager.Info(context.Background(), testOwner(), one.ID)
	if err != nil || current.Info.Streams[0].Codec != "h264" {
		t.Fatal("caller mutated retained source facts")
	}
	manager.CancelMatching(testOwner().SessionID, "")
	for _, lease := range []Lease{one, two} {
		if _, err := manager.Info(context.Background(), testOwner(), lease.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("revoked credential retained an active source")
		}
	}
}

func TestClosedInputCannotCreateASecondPipeAndApplicationHintsStayCredentialScoped(t *testing.T) {
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
	}), nil, Options{})
	owner := Owner{ApplicationKey: true, SessionID: "key_credential", ApplicationClientID: "client_alpha", DeviceID: "alpha_device"}
	lease := testOpen(t, manager, owner, "play_alpha")
	hintOwner := Owner{ApplicationKey: true, SessionID: "key_credential"}
	if play, err := manager.PlaybackReference(hintOwner, lease.ID); err != nil || play != "play_alpha" {
		t.Fatal("owned application lease could not restore its playback context")
	}
	hintOwner.SessionID = "another_key"
	if _, err := manager.PlaybackReference(hintOwner, lease.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("lease disclosed playback context to another credential")
	}
	input, err := manager.Acquire(context.Background(), owner, lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := input.OpenPipe(); !errors.Is(err, ErrClosed) {
		t.Fatal("closed source reader created another pipe")
	}
}

func TestReconnectBudgetIncludesFailedConnectionAttempts(t *testing.T) {
	var attempts atomic.Int32
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		if attempts.Add(1) == 1 {
			return nil, errors.New("first upstream attempt failed")
		}
		return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
	}), nil, Options{})
	lease := testOpen(t, manager, testOwner(), "play_one")
	input, err := manager.Acquire(context.Background(), testOwner(), lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	input, err = manager.Acquire(context.Background(), testOwner(), lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	if _, err := manager.Acquire(context.Background(), testOwner(), lease.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("reopening exceeded the lease's shared retry budget")
	}
	if attempts.Load() != 3 {
		t.Fatal("source opening retry and reopening did not share their bound")
	}
}
