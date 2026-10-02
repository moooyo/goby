package media

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
)

func TestMediaNativeCapacityHandoffSerializesAwakenedSuccessor(t *testing.T) {
	// Only actual admission bookkeeping is exercised here. Synthetic registry
	// entries are not passing native Domains or operating-system retirement.
	governor := newMediaProcessAdmission(mediaProcessOwnerLimit, mediaProcessBackgroundLimit, 1)
	var counter atomic.Uint64
	owners := make([]*nativeMediaProcess, 0, mediaProcessOwnerLimit+1)
	releases := make([]func(), 0, mediaProcessOwnerLimit+1)
	creditReturned, allowReturn, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var allowOnce sync.Once
	var successorJoined <-chan struct{}
	type admissionResult struct {
		owner   *nativeMediaProcess
		release func()
		err     error
	}
	admitted := make(chan admissionResult, 1)
	handoffStarted, resultReceived := false, false
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	recordResult := func(result admissionResult) {
		if result.release != nil {
			releases = append(releases, result.release)
		}
		if result.owner != nil {
			owners = append(owners, result.owner)
		}
		resultReceived = true
	}
	t.Cleanup(func() {
		cancel()
		allowOnce.Do(func() { close(allowReturn) })
		if handoffStarted {
			select {
			case <-returned:
			case <-time.After(3 * time.Second):
				t.Error("original capacity handoff did not actually return")
				return
			}
		}
		if successorJoined != nil {
			select {
			case <-successorJoined:
			case <-time.After(3 * time.Second):
				t.Error("successor did not actually join")
				return
			}
			if !resultReceived {
				recordResult(<-admitted)
			}
		}
		// Discard only the isolated test's synthetic entries and charges.
		nativeMediaOwners.mu.Lock()
		for _, owner := range owners {
			if nativeMediaOwners.entries[owner.slot] == owner {
				nativeMediaOwners.entries[owner.slot] = nil
			}
		}
		nativeMediaOwners.mu.Unlock()
		for _, release := range releases {
			release()
		}
	})
	for index := 0; index < mediaProcessOwnerLimit; index++ {
		release, err := governor.acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
		ownedRelease := release
		if index == 0 {
			ownedRelease = func() { release(); close(creditReturned); <-allowReturn }
		}
		owner, err := newNativeMediaOwner(&commanddomain.CommandScope{}, governor, ownedRelease, &counter)
		if err != nil {
			t.Fatal(err)
		}
		owners = append(owners, owner)
	}
	attempted, joined := make(chan struct{}), make(chan struct{})
	successorJoined = joined
	go func() {
		defer close(joined)
		release, err := governor.acquire(ctx)
		if err != nil {
			admitted <- admissionResult{err: err}
			return
		}
		close(attempted)
		owner, err := newNativeMediaOwner(&commanddomain.CommandScope{}, governor, release, &counter)
		admitted <- admissionResult{owner: owner, release: release, err: err}
	}()
	// Observe a real queued successor rather than infer it from goroutine start.
	for {
		governor.mu.Lock()
		queued := len(governor.waiters)
		governor.mu.Unlock()
		if queued == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("successor never entered admission queue")
		default:
			runtime.Gosched()
		}
	}
	handoffStarted = true
	original := owners[0]
	go func() { defer close(returned); original.returnKnownCapacity() }()
	select {
	case <-creditReturned:
	case <-ctx.Done():
		t.Fatal("original credit was not returned")
	}
	select {
	case <-attempted:
	case <-ctx.Done():
		t.Fatal("returned credit did not wake successor")
	}
	select {
	case result := <-admitted:
		recordResult(result)
		t.Fatalf("successor inspected a registry before the original handoff returned: %v", result.err)
	case <-time.After(25 * time.Millisecond):
	}
	allowOnce.Do(func() { close(allowReturn) })
	select {
	case result := <-admitted:
		recordResult(result)
		if result.err != nil || result.owner == nil {
			t.Fatalf("awakened successor spuriously lost registry capacity: %v", result.err)
		}
	case <-ctx.Done():
		t.Fatal("successor did not receive the handed-off registry slot")
	}
	select {
	case <-returned:
	case <-ctx.Done():
		t.Fatal("original handoff did not actually join before its deadline")
	}
	select {
	case <-joined:
	case <-ctx.Done():
		t.Fatal("successor did not actually join before its deadline")
	}
	if stats := processCapacityStatsFor(governor, &counter); stats.Active != mediaProcessOwnerLimit || stats.Queued != 0 || stats.RetirementUnknown != 0 {
		t.Fatalf("handoff changed the existing budget: %+v", stats)
	}
}

func TestMediaNativeAbnormalCapacityReturnRetainsExactOwner(t *testing.T) {
	for _, test := range []struct {
		name    string
		release func()
	}{
		{"panic", func() { panic("synthetic release fault") }},
		{"goexit", runtime.Goexit},
	} {
		t.Run(test.name, func(t *testing.T) {
			governor := newMediaProcessAdmission(1, 1, 1)
			release, err := governor.acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var counter atomic.Uint64
			owner, err := newNativeMediaOwner(&commanddomain.CommandScope{}, governor, test.release, &counter)
			if err != nil {
				release()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				nativeMediaOwners.mu.Lock()
				if nativeMediaOwners.entries[owner.slot] == owner {
					nativeMediaOwners.entries[owner.slot] = nil
				}
				nativeMediaOwners.mu.Unlock()
				release()
			})
			joined := make(chan struct{})
			go func() { defer close(joined); defer func() { _ = recover() }(); owner.returnKnownCapacity() }()
			select {
			case <-joined:
			case <-time.After(3 * time.Second):
				t.Fatal("abnormal capacity return did not release its locks and join")
			}
			owner.returnKnownCapacity()
			owner.mu.Lock()
			unknown, released := owner.unknown, owner.released
			owner.mu.Unlock()
			nativeMediaOwners.mu.Lock()
			held := nativeMediaOwners.entries[owner.slot] == owner
			nativeMediaOwners.mu.Unlock()
			if !unknown || released || !held {
				t.Fatal("abnormal return discarded or reused its original exact owner")
			}
			if stats := processCapacityStatsFor(governor, &counter); stats.Active != 1 || stats.RetirementUnknown != 1 {
				t.Fatalf("abnormal return refunded its original charge: %+v", stats)
			}
		})
	}
}
