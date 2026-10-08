package notifications

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func notificationOwnershipRuntime(t *testing.T) *Runtime {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{ctx: ctx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}, 1), active: make(map[string]*activeAttempt)}
	t.Cleanup(r.BeginClose)
	return r
}

func TestNotificationAttemptCapacityIncludesCanceledUnretiredWorkers(t *testing.T) {
	r := notificationOwnershipRuntime(t)
	for index := 0; index < notificationSenderLimit; index++ {
		id := fmt.Sprintf("registration-%d", index)
		_, attempt, ok := r.beginAttempt(delivery{id: id, registration: id, session: id})
		if !ok {
			t.Fatal("sender capacity was exhausted early")
		}
		attempt.cancel()
		if _, _, ok := r.beginAttempt(delivery{id: "replacement", registration: id, session: id}); ok {
			t.Fatal("canceled worker's registration was replaced before retirement")
		}
	}
	if _, _, ok := r.beginAttempt(delivery{id: "overflow", registration: "overflow"}); ok {
		t.Fatal("cancellation released sender capacity before worker retirement")
	}
	first := r.active["registration-0"]
	r.endAttempt("registration-0", first)
	if _, _, ok := r.beginAttempt(delivery{id: "replacement", registration: "registration-0"}); !ok {
		t.Fatal("retired worker did not release its registration")
	}
	if len(r.active) != notificationSenderLimit {
		t.Fatalf("active sender count = %d", len(r.active))
	}
}

func TestNotificationFenceFindsOwnershipBeforeTheFirstTargetRead(t *testing.T) {
	for _, kind := range []string{"session", "user", "registration", "configuration"} {
		t.Run(kind, func(t *testing.T) {
			r := notificationOwnershipRuntime(t)
			ctx, attempt, ok := r.beginAttempt(delivery{registration: "registration", session: "session", user: "user", regRevision: 2, configRevision: 3})
			if !ok {
				t.Fatal("attempt was not admitted")
			}
			fenced := make(chan error, 1)
			go func() {
				wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				switch kind {
				case "session":
					fenced <- r.FenceSession(wait, "session")
				case "user":
					fenced <- r.FenceUser(wait, "user")
				case "registration":
					fenced <- r.FenceRegistration(wait, "registration", 3)
				case "configuration":
					fenced <- r.FenceConfig(wait, 4)
				}
			}()
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("fence did not find the worker before its target read")
			}
			select {
			case err := <-fenced:
				t.Fatalf("fence returned before retirement: %v", err)
			default:
			}
			r.endAttempt("registration", attempt)
			if err := <-fenced; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNotificationFenceWaitsForInFlightClaimOwnership(t *testing.T) {
	r := notificationOwnershipRuntime(t)
	r.claiming = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := r.FenceSession(ctx, "session"); err != context.DeadlineExceeded {
		t.Fatalf("fence ignored a claim whose owner was not known: %v", err)
	}
	_, attempt, ok := r.beginAttempt(delivery{registration: "registration", session: "session"})
	if !ok {
		t.Fatal("claim owner was not admitted")
	}
	r.endClaim()
	r.endAttempt("registration", attempt)
	if err := r.FenceSession(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationFenceRetainsRegistrationWhileTLSAuthorizationRetires(t *testing.T) {
	r := notificationOwnershipRuntime(t)
	ctx, attempt, ok := r.beginAttempt(delivery{registration: "registration", session: "session"})
	if !ok {
		t.Fatal("attempt was not admitted")
	}
	request := make(chan struct{}, 1)
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		request <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	entered, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	defer released.Do(func() { close(release) })
	retired := make(chan struct{})
	go func() {
		defer close(retired)
		defer r.endAttempt("registration", attempt)
		postWebhook(ctx, target{endpoint: receiver.URL, networks: []string{"127.0.0.1/32"}}, "receiver", "target", "event", []byte(`{}`), RuntimeOptions{RootCAs: roots}, func(ctx context.Context) error {
			close(entered)
			<-release
			return ctx.Err()
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("TLS pre-body authorization did not begin")
	}
	wait, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.FenceSession(wait, "session"); err != context.DeadlineExceeded {
		t.Fatalf("fence did not wait for the transport worker: %v", err)
	}
	if r.ActiveForSession("session") != 1 {
		t.Fatal("cancellation discarded unretired transport ownership")
	}
	if _, _, ok := r.beginAttempt(delivery{id: "new-lease", registration: "registration", session: "session"}); ok {
		t.Fatal("a replacement lease overlapped the unretired transport")
	}
	released.Do(func() { close(release) })
	select {
	case <-retired:
	case <-time.After(5 * time.Second):
		t.Fatal("transport did not retire after authorization returned")
	}
	if err := r.FenceSession(context.Background(), "session"); err != nil || r.ActiveForSession("session") != 0 {
		t.Fatalf("retired transport retained ownership: %v", err)
	}
	select {
	case <-request:
		t.Fatal("canceled pre-body authorization allowed an HTTP request")
	default:
	}
}
