package dynamicsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestLeaseObservationsPreserveAuthorizationAndCloseSemantics(t *testing.T) {
	for _, method := range []string{"Info", "Validate"} {
		for _, change := range []string{"close", "deny", "cancelled_denial"} {
			t.Run(method+"/"+change, func(t *testing.T) {
				var authorize func(context.Context, Owner, string, string) error
				manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
					return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
				}), func(ctx context.Context, owner Owner, itemID, playID string) error {
					if authorize != nil {
						return authorize(ctx, owner, itemID, playID)
					}
					return nil
				}, Options{})
				owner := testOwner()
				lease := testOpen(t, manager, owner, "observation")
				owner.PeerIP = "192.0.2.48"
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				denied := errors.New("current lease authority denied")
				calls := 0
				authorize = func(work context.Context, current Owner, itemID, playID string) error {
					calls++
					if current != owner || itemID != lease.ItemID || playID != lease.PlaySessionID {
						t.Fatal("lease observation lost current owner or captured scope")
					}
					if !manager.mu.TryLock() {
						t.Fatal("lease observation held the manager mutex during authorization")
					}
					manager.mu.Unlock()
					if change == "close" {
						return manager.CloseLease(work, current, lease.ID)
					}
					if change == "cancelled_denial" {
						cancel()
					}
					return denied
				}
				var err error
				if method == "Info" {
					var result Lease
					result, err = manager.Info(ctx, owner, lease.ID)
					if result.ID != "" || result.Info.Streams != nil {
						t.Fatal("failed observation published its candidate metadata")
					}
				} else {
					err = manager.Validate(ctx, owner, lease.ID)
				}
				want := denied
				if change == "close" {
					want = ErrNotFound
				}
				if !errors.Is(err, want) || calls != 1 {
					t.Fatalf("observation error=%v authorizations=%d, want %v and 1", err, calls, want)
				}
				manager.mu.Lock()
				closed := manager.leases[lease.ID].closed
				manager.mu.Unlock()
				if closed != (change != "cancelled_denial") {
					t.Fatal("authorization failure changed best-effort close semantics")
				}
			})
		}
	}
}

func TestLeaseValidationRefreshesActivityBeforeConsumerFences(t *testing.T) {
	calls := 0
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
	}), func(context.Context, Owner, string, string) error {
		calls++
		return nil
	}, Options{})
	owner := testOwner()
	lease := testOpen(t, manager, owner, "consumer_fences")
	input, err := manager.Acquire(context.Background(), owner, lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	for _, method := range []string{"Acquire", "Subtitle"} {
		old := time.Now().Add(-time.Minute)
		manager.mu.Lock()
		manager.leases[lease.ID].accessed = old
		manager.mu.Unlock()
		before := calls
		if method == "Acquire" {
			if _, err := manager.Acquire(context.Background(), owner, lease.ID); !errors.Is(err, ErrBusy) {
				t.Fatalf("occupied input lost its fence: %v", err)
			}
		} else if _, err := manager.Subtitle(context.Background(), owner, lease.ID, lease.Generation+1, 0, "invalid"); !errors.Is(err, ErrStaleGeneration) {
			t.Fatalf("subtitle lookup lost its generation fence: %v", err)
		}
		manager.mu.Lock()
		accessed := manager.leases[lease.ID].accessed
		manager.mu.Unlock()
		if calls != before+1 || !accessed.After(old) {
			t.Fatalf("%s skipped fresh validation or its activity refresh", method)
		}
	}
}

func TestInfoAndInputRetainIndependentPreAuthorizationFacts(t *testing.T) {
	var duringAuthorization func()
	manager := testManager(t, connectorFunc(func(context.Context, Definition) (*Connection, error) {
		facts := testFacts()
		facts.Streams[0].DolbyVision = &media.DolbyVisionMetadata{Profile: 8}
		facts.Streams[1].AudioTiming = &media.AudioTiming{SampleCount: 48000}
		facts.Chapters = []media.Chapter{{Title: "Original chapter"}}
		return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: facts}, nil
	}), func(context.Context, Owner, string, string) error {
		if duringAuthorization != nil {
			duringAuthorization()
		}
		return nil
	}, Options{})
	owner := testOwner()
	lease := testOpen(t, manager, owner, "snapshot_facts")
	duringAuthorization = func() {
		manager.mu.Lock()
		manager.leases[lease.ID].lease.Info.Streams[0].Width = 640
		manager.mu.Unlock()
	}
	before, err := manager.Info(context.Background(), owner, lease.ID)
	if err != nil || before.Info.Streams[0].Width != 1280 {
		t.Fatalf("Info lost its pre-authorization snapshot: %v", err)
	}
	duringAuthorization = nil
	input, err := manager.Acquire(context.Background(), owner, lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	for _, facts := range []*media.Info{&before.Info, &input.Lease.Info} {
		facts.Streams[0].Width = 320
		facts.Streams[0].DolbyVision.Profile = 5
		facts.Streams[1].AudioTiming.SampleCount = 1
		facts.Chapters[0].Title = "Caller mutation"
	}
	current, err := manager.Info(context.Background(), owner, lease.ID)
	if err != nil || current.Info.Streams[0].Width != 640 || current.Info.Streams[0].DolbyVision.Profile != 8 ||
		current.Info.Streams[1].AudioTiming.SampleCount != 48000 || current.Info.Chapters[0].Title != "Original chapter" {
		t.Fatalf("returned Info or Input metadata aliased retained facts: %v", err)
	}
}

func BenchmarkLeaseObservation(b *testing.B) {
	for _, count := range []int{2, 256} {
		for _, method := range []string{"Info", "Validate"} {
			b.Run(fmt.Sprintf("%s/%d_streams", method, count), func(b *testing.B) {
				owner := testOwner()
				facts := testFacts()
				facts.Streams = make([]media.Stream, count)
				for index := range facts.Streams {
					facts.Streams[index].AudioTiming = &media.AudioTiming{SampleCount: 48000}
					facts.Streams[index].DolbyVision = &media.DolbyVisionMetadata{Profile: 8}
				}
				manager := &Manager{options: Options{Authorize: func(context.Context, Owner, string, string) error { return nil }},
					leases: map[string]*leaseState{"lease": {key: leaseKey{owner: owner.Identity()},
						lease: Lease{ID: "lease", Description: Description{ItemID: "42", Info: facts}, PlaySessionID: "play"}}}}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					var err error
					if method == "Info" {
						_, err = manager.Info(context.Background(), owner, "lease")
					} else {
						err = manager.Validate(context.Background(), owner, "lease")
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
