package dynamicsource

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitPipeSet(t *testing.T, set *PipeSet) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return set.Wait(ctx)
}

func TestPipeSetReplaysIdenticalBytesFromOneIngressAcrossRingWraps(t *testing.T) {
	payload := bytes.Repeat([]byte("authorized-prefix-and-body\x00\xff\x17"), 24000)
	var opens atomic.Int32
	upstream := &countedReader{Reader: bytes.NewReader(payload), closed: make(chan struct{})}
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		opens.Add(1)
		return &Connection{Reader: upstream, Info: testFacts()}, nil
	}), nil, Options{FanoutBufferBytes: pipeChunkBytes, FanoutTotalBytes: pipeChunkBytes})
	lease := testOpen(t, manager, testOwner(), "same_bytes")
	input, err := manager.Acquire(context.Background(), testOwner(), lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	set, err := input.OpenPipeSet(2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	if set.LeaseID != lease.ID || set.Generation != lease.Generation || set.Stamp != lease.Stamp || len(set.Readers) != 2 {
		t.Fatal("pipe readers lost their generation binding")
	}
	if _, err := input.OpenPipeSet(1); !errors.Is(err, ErrBusy) {
		t.Fatal("a running input admitted a late reader")
	}
	if _, err := input.Read(make([]byte, 1)); !errors.Is(err, ErrBusy) {
		t.Fatal("direct reading bypassed pipe ownership")
	}
	type result struct {
		data []byte
		err  error
	}
	results := make(chan result, 2)
	firstRead := make(chan struct{})
	go func() {
		var data bytes.Buffer
		buffer := make([]byte, 137)
		n, err := set.Readers[0].Read(buffer)
		_, _ = data.Write(buffer[:n])
		close(firstRead)
		if err == nil {
			_, err = io.CopyBuffer(&data, set.Readers[0], buffer)
		}
		results <- result{data.Bytes(), err}
	}()
	go func() {
		<-firstRead
		data, err := io.ReadAll(set.Readers[1])
		results <- result{data, err}
	}()
	if err := waitPipeSet(t, set); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got := <-results
		if got.err != nil || !bytes.Equal(got.data, payload) {
			t.Fatal("uneven consumers received different or incomplete bytes")
		}
	}
	if opens.Load() != 1 {
		t.Fatal("fanout opened the upstream more than once")
	}
	select {
	case <-upstream.closed:
	default:
		t.Fatal("joined pipes retained their upstream")
	}
	manager.mu.Lock()
	reserved, active := manager.fanoutBytes, manager.leases[lease.ID].input
	manager.mu.Unlock()
	if reserved != 0 || active != nil {
		t.Fatal("completed fanout retained its budget or reader lease")
	}
}

func TestPipeSetStalledReaderFailsEveryLaneAndReleasesQuota(t *testing.T) {
	payload := bytes.Repeat([]byte("no-byte-may-be-skipped"), 100000)
	upstream := &countedReader{Reader: bytes.NewReader(payload), closed: make(chan struct{})}
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		return &Connection{Reader: upstream, Info: testFacts()}, nil
	}), nil, Options{FanoutBufferBytes: pipeChunkBytes, FanoutTotalBytes: pipeChunkBytes, FanoutStallTimeout: 300 * time.Millisecond})
	lease := testOpen(t, manager, testOwner(), "stalled")
	input, err := manager.Acquire(context.Background(), testOwner(), lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	set, err := input.OpenPipeSet(2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	consumed := make(chan int64, 1)
	go func() { n, _ := io.Copy(io.Discard, set.Readers[0]); consumed <- n }()
	if err := waitPipeSet(t, set); !errors.Is(err, ErrFanoutStalled) {
		t.Fatalf("stalled fanout error = %v", err)
	}
	if n := <-consumed; n >= int64(len(payload)) {
		t.Fatal("the fast reader bypassed the stalled reader's bounded journal")
	}
	manager.mu.Lock()
	reserved := manager.fanoutBytes
	manager.mu.Unlock()
	if reserved != 0 {
		t.Fatal("stalled fanout leaked its memory reservation")
	}
	select {
	case <-upstream.closed:
	default:
		t.Fatal("stalled fanout retained its ingress")
	}
}

func TestPipeSetQuotaAndByteZeroAdmissionAreAtomic(t *testing.T) {
	var writersMu sync.Mutex
	var writers []io.Closer
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		reader, writer := io.Pipe()
		writersMu.Lock()
		writers = append(writers, writer)
		writersMu.Unlock()
		return &Connection{Reader: reader, Info: testFacts()}, nil
	}), nil, Options{FanoutBufferBytes: pipeChunkBytes, FanoutTotalBytes: pipeChunkBytes})
	t.Cleanup(func() {
		for _, writer := range writers {
			_ = writer.Close()
		}
	})
	one := testOpen(t, manager, testOwner(), "quota_one")
	two := testOpen(t, manager, testOwner(), "quota_two")
	first, err := manager.Acquire(context.Background(), testOwner(), one.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Acquire(context.Background(), testOwner(), two.ID)
	if err != nil {
		t.Fatal(err)
	}
	set, err := first.OpenPipeSet(2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	if _, err := second.OpenPipeSet(2); !errors.Is(err, ErrBusy) {
		t.Fatal("concurrent fanout exceeded the manager's memory quota")
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitPipeSet(t, set); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	retry, err := second.OpenPipeSet(2)
	if err != nil {
		t.Fatal("failed reservation consumed the second connection", err)
	}
	if err := retry.Close(); err != nil {
		t.Fatal(err)
	}
	if err := waitPipeSet(t, retry); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}

	manager2 := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		return &Connection{Reader: io.NopCloser(bytes.NewReader([]byte("prefix"))), Info: testFacts()}, nil
	}), nil, Options{})
	lease := testOpen(t, manager2, testOwner(), "already_read")
	input, err := manager2.Acquire(context.Background(), testOwner(), lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err := input.OpenPipeSet(3); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded reader count accepted")
	}
	if _, err := input.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := input.OpenPipeSet(2); !errors.Is(err, ErrBusy) {
		t.Fatal("fanout started after byte zero")
	}
}

func TestPipeSetConsumerFailureAndLeaseCancellationJoinAllPumps(t *testing.T) {
	for _, mode := range []string{"consumer_failure", "lease_cancel", "manager_close"} {
		t.Run(mode, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer writer.Close()
			manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
				return &Connection{Reader: reader, Info: testFacts()}, nil
			}), nil, Options{})
			lease := testOpen(t, manager, testOwner(), "close_group")
			input, err := manager.Acquire(context.Background(), testOwner(), lease.ID)
			if err != nil {
				t.Fatal(err)
			}
			set, err := input.OpenPipeSet(2)
			if err != nil {
				t.Fatal(err)
			}
			defer set.Close()
			switch mode {
			case "consumer_failure":
				_ = set.Readers[1].Close()
				go func() { _, _ = writer.Write([]byte("wake closed reader")) }()
			case "lease_cancel":
				manager.CancelMatching(testOwner().SessionID, lease.PlaySessionID)
			case "manager_close":
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := manager.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err := waitPipeSet(t, set); err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("terminated group was not joined: %v", err)
			}
			if _, err := writer.Write([]byte("after close")); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatal("joined group retained a blocked upstream reader")
			}
		})
	}
}

func TestPipeSetReaderTimeoutAlsoAppliesAfterUpstreamEOF(t *testing.T) {
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		return &Connection{Reader: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte{1}, 2<<20))), Info: testFacts()}, nil
	}), nil, Options{FanoutBufferBytes: 4 << 20, FanoutTotalBytes: 4 << 20, FanoutStallTimeout: 300 * time.Millisecond})
	lease := testOpen(t, manager, testOwner(), "eof_before_consumed")
	input, err := manager.Acquire(context.Background(), testOwner(), lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	set, err := input.OpenPipeSet(2)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close()
	if err := waitPipeSet(t, set); !errors.Is(err, ErrFanoutStalled) {
		t.Fatal(err)
	}
	set.mu.Lock()
	eof := set.eof
	set.mu.Unlock()
	if !eof {
		t.Fatal("fixture did not cover pending egress after upstream EOF")
	}
}
