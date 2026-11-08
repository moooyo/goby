package settings

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestServerPolicyValidationRejectsAmbiguousNetworks(t *testing.T) {
	for _, networks := range [][]string{
		nil, {"192.168.1.1/24"}, {"192.168.0.0/16", "192.168.0.0/16"},
		{" 10.0.0.0/8"}, {"::ffff:192.168.0.0/112"}, {"FC00::/7"},
		{"10.0.0.0"}, {"invalid"}, {"10.0.0.0/33"},
	} {
		value := DefaultServerPolicy()
		value.LocalNetworkSubnets = networks
		if ValidateServerPolicy(value) == nil {
			t.Errorf("accepted noncanonical or ambiguous network set: %q", networks)
		}
	}
	value := DefaultServerPolicy()
	value.LocalNetworkSubnets = []string{}
	if err := ValidateServerPolicy(value); err != nil {
		t.Fatal("an explicit empty subnet list must preserve loopback-only local access", err)
	}
	value.UICulture = "zh"
	if ValidateServerPolicy(value) == nil {
		t.Fatal("accepted an unsupported interface culture")
	}
}

func TestServerPolicyDefaultsAndExplicitLocalBoundary(t *testing.T) {
	values := DefaultServerPolicy()
	values.EnableRemoteAccess = false
	snapshot, err := makeServerPolicySnapshot(1, values, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, peer := range []string{"127.0.0.1", "127.100.10.1", "::1", "::ffff:127.0.0.1", "192.168.1.20", "10.20.30.40", "172.31.1.1", "fd00::7", "fe80::7%eth0"} {
		if !snapshot.IsLocal(peer) || !snapshot.Allows(peer) {
			t.Errorf("default local peer was denied: %s", peer)
		}
	}
	for _, peer := range []string{"", "invalid", "203.0.113.17", "2001:db8::17", "172.32.0.1"} {
		if snapshot.IsLocal(peer) || snapshot.Allows(peer) {
			t.Errorf("remote or unknown peer became local: %q", peer)
		}
	}
	values.LocalNetworkSubnets = []string{"203.0.113.0/24"}
	snapshot, err = makeServerPolicySnapshot(2, values, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.IsLocal("203.0.113.17") || snapshot.IsLocal("192.168.1.20") || !snapshot.IsLocal("::1") {
		t.Fatal("custom subnets did not replace default ranges while retaining loopback")
	}
	values.EnableRemoteAccess = true
	_, allowed, err := PreviewServerPolicy(values, "")
	if err != nil || !allowed {
		t.Fatal("default remote access must not silently reject unknown peers")
	}
}

func TestServerPolicyPublicationRevokesExistingRequestsAndDoesNotShareSlices(t *testing.T) {
	initial, err := makeServerPolicySnapshot(1, DefaultServerPolicy(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store := &ServerPolicyStore{observers: map[uint64]serverPolicyObserver{}}
	store.current.Store(&initial)
	remote, releaseRemote, accepted := store.Watch(context.Background(), "203.0.113.17")
	if !accepted {
		t.Fatal("remote request was not initially admitted")
	}
	defer releaseRemote()
	local, releaseLocal, accepted := store.Watch(context.Background(), "127.0.0.1")
	if !accepted {
		t.Fatal("local request was not admitted")
	}
	defer releaseLocal()
	values := DefaultServerPolicy()
	values.EnableRemoteAccess = false
	next, err := makeServerPolicySnapshot(2, values, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	store.publish(next)
	if !errors.Is(context.Cause(remote), ErrServerPolicyDenied) {
		t.Fatal("an already admitted remote request survived the committed denial")
	}
	if local.Err() != nil {
		t.Fatal("loopback request was revoked")
	}
	denied, releaseDenied, accepted := store.Watch(context.Background(), "203.0.113.18")
	defer releaseDenied()
	if accepted || !errors.Is(context.Cause(denied), ErrServerPolicyDenied) {
		t.Fatal("new remote request bypassed the committed denial")
	}
	copy := store.Snapshot()
	copy.Values.LocalNetworkSubnets[0] = "0.0.0.0/0"
	if reflect.DeepEqual(copy.Values.LocalNetworkSubnets, store.Snapshot().Values.LocalNetworkSubnets) {
		t.Fatal("caller mutated a published subnet slice")
	}
	if err := validateRevision(math.MaxInt64); err == nil {
		t.Fatal("revision overflow was admitted")
	}
}
