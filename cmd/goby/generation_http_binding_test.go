package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/server"
	"github.com/moooyo/goby/internal/settings"
)

func TestGenerationHTTPBindingReservesExactDesiredBeforePublishing(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", "", "deployment-host.internal"} {
		t.Run(host, func(t *testing.T) {
			binding := server.HTTPBindingStartup{Binding: settings.NetworkValues{BindHost: host, HttpPort: 12345}, Revision: 9007199254740993}
			owned := newGenerationTestListener(false)
			calls, published := 0, false
			listener, err := reserveGenerationHTTPBinding(context.Background(), binding,
				func(_ context.Context, network, address string) (net.Listener, error) {
					calls++
					if network != "tcp" || address != net.JoinHostPort(host, "12345") || published {
						t.Fatal("reservation did not use the exact desired address before publication")
					}
					return owned, nil
				}, func(address net.Addr, receipt server.HTTPBindingStartup) error {
					if calls != 1 || receipt != binding || address.String() != owned.Addr().String() || owned.accepts.Load() != 0 {
						t.Fatal("publication did not identify its successful unserved reservation")
					}
					published = true
					return nil
				})
			if err != nil || listener != owned || calls != 1 || !published {
				t.Fatal("reservation lost ownership or attempted a fallback address")
			}
			g := generationTestReserved(listener)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := g.Close(ctx); err != nil {
				t.Fatal("close the owned unserved binding")
			}
		})
	}
}

func TestGenerationHTTPBindingFailureNeverFallsBackOrPublishes(t *testing.T) {
	binding := server.HTTPBindingStartup{Binding: settings.NetworkValues{BindHost: "127.0.0.1", HttpPort: 12345}, Revision: 1}
	calls, published := 0, false
	listener, err := reserveGenerationHTTPBinding(context.Background(), binding,
		func(context.Context, string, string) (net.Listener, error) {
			calls++
			return nil, errors.New("private occupied endpoint")
		}, func(net.Addr, server.HTTPBindingStartup) error { published = true; return nil })
	if listener != nil || err == nil || diagnostics.ErrorClass(err) != "listener_start_failed" || calls != 1 || published {
		t.Fatal("failed reservation was hidden, retried or published as active")
	}
	if err.Error() != "listener_start_failed" {
		t.Fatal("listener failure disclosed an unbounded endpoint error")
	}
}

func TestGenerationHTTPBindingPublicationFailureRetainsCleanupOwnership(t *testing.T) {
	binding := server.HTTPBindingStartup{Binding: settings.NetworkValues{BindHost: "127.0.0.1", HttpPort: 12345}, Revision: 1}
	owned := newGenerationTestListener(false)
	listener, err := reserveGenerationHTTPBinding(context.Background(), binding,
		func(context.Context, string, string) (net.Listener, error) { return owned, nil },
		func(net.Addr, server.HTTPBindingStartup) error { return errors.New("private publication failure") })
	if err == nil || listener != owned || owned.accepts.Load() != 0 {
		t.Fatal("failed publication leaked or served its reservation")
	}
	g := generationTestReserved(listener)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.Close(ctx); err != nil {
		t.Fatal("generation cleanup did not join the failed publication reservation")
	}
	select {
	case <-owned.closed:
	default:
		t.Fatal("failed publication kept the socket reserved")
	}
}
