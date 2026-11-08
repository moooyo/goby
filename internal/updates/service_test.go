package updates

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (operation roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return operation(request)
}

// fixtureTLS injects an explicit transport that rewrites one configured public
// URL to this test's TLS listener. Production URL validation and DNS policy are
// not relaxed, and the fixture client retains certificate verification.
func fixtureTLS(t *testing.T, handler http.HandlerFunc) http.RoundTripper {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("parse TLS fixture URL")
	}
	transport := server.Client().Transport
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		copy := request.Clone(request.Context())
		address := *request.URL
		address.Scheme, address.Host = target.Scheme, target.Host
		copy.URL, copy.Host = &address, target.Host
		return transport.RoundTrip(copy)
	})
}

func allowCheck(context.Context) error { return nil }

func TestUpdateCheckTLSKeepsUnconfiguredFailedAndCurrentDistinct(t *testing.T) {
	var requests atomic.Int32
	var status atomic.Int32
	status.Store(http.StatusOK)
	feed := fixtureFeed(t, "0.2.0")
	transport := fixtureTLS(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/release.json" || r.URL.RawQuery != "" ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Accept") != "application/json" {
			t.Error("update check sent an unexpected request or credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write(feed)
	})
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	service, err := New(Options{CurrentVersion: "0.1.0", FeedURL: "https://updates.example.com/release.json", Transport: transport, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	if view := service.Status(); view.State != NotChecked || view.CheckedAt != nil || view.Release != nil {
		t.Fatal("a configured source was presented as already checked")
	}
	view, err := service.Check(context.Background(), allowCheck)
	if err != nil || view.State != Available || view.Release == nil || view.Release.Version != "0.2.0" || view.CheckedAt == nil || !view.CheckedAt.Equal(now) {
		t.Fatal("new Goby release was not observed through the TLS fixture")
	}
	if available, known := service.HasUpdateAvailable(); !available || !known {
		t.Fatal("successful comparison was not available to the compatibility projection")
	}
	view.Release.Version = "tampered"
	*view.CheckedAt = time.Time{}
	if service.Status().Release.Version != "0.2.0" || service.Status().CheckedAt.IsZero() {
		t.Fatal("a caller modified the service's stored observation")
	}
	status.Store(http.StatusBadGateway)
	view, err = service.Check(context.Background(), allowCheck)
	if !errors.Is(err, ErrCheckFailed) || view.State != Failed || view.Release != nil || view.CheckedAt == nil || view.ErrorCode == nil || *view.ErrorCode != "update_check_failed" {
		t.Fatal("a failed check retained or fabricated a successful result")
	}
	if _, known := service.HasUpdateAvailable(); known {
		t.Fatal("failed observation retained an old compatibility assertion")
	}
	status.Store(http.StatusOK)
	current, err := New(Options{CurrentVersion: "v0.2.0+own-build", FeedURL: "https://updates.example.com/release.json", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(current.Close)
	view, err = current.Check(context.Background(), allowCheck)
	if err != nil || view.State != Current {
		t.Fatal("equal product versions were not compared independently of build metadata")
	}
	unknown, err := New(Options{CurrentVersion: "integration-version", FeedURL: "https://updates.example.com/release.json", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	view, err = unknown.Check(context.Background(), allowCheck)
	if !errors.Is(err, ErrVersionUnknown) || view.State != VersionUnknown || view.Release == nil || view.CheckedAt == nil {
		t.Fatal("an incomparable product build was presented as current or available")
	}
	unconfigured, err := New(Options{CurrentVersion: "0.1.0", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	view, err = unconfigured.Check(context.Background(), allowCheck)
	if !errors.Is(err, ErrUnconfigured) || view.State != Unconfigured || view.CheckedAt != nil || view.ConfiguredSource || requests.Load() != before {
		t.Fatal("unconfigured source performed I/O or claimed a successful check")
	}
}

func TestUpdateCheckResponseBudgetsAndRedirectsNeverBecomeCurrent(t *testing.T) {
	feed := fixtureFeed(t, "0.1.0")
	for _, test := range []struct {
		name, mediaType, encoding, payload string
		status                             int
	}{
		{"redirect", "application/json", "", string(feed), http.StatusFound},
		{"wrong-type", "text/html", "", string(feed), http.StatusOK},
		{"wrong-charset", "application/json; charset=utf-16", "", string(feed), http.StatusOK},
		{"compressed", "application/json", "gzip", string(feed), http.StatusOK},
		{"oversized", "application/json", "", strings.Repeat(" ", MaxFeedBytes+1), http.StatusOK},
		{"malformed", "application/json", "", `{}`, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			transport := fixtureTLS(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", test.mediaType)
				w.Header().Set("Content-Encoding", test.encoding)
				w.Header().Set("Location", "https://other.example.com/redirected")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.payload)
			})
			service, err := New(Options{CurrentVersion: "0.1.0", FeedURL: "https://updates.example.com/release.json", Transport: transport})
			if err != nil {
				t.Fatal(err)
			}
			view, err := service.Check(context.Background(), allowCheck)
			if !errors.Is(err, ErrCheckFailed) || view.State != Failed || view.Release != nil || calls.Load() != 1 {
				t.Fatal("untrusted response was accepted or its redirect was followed")
			}
		})
	}
}

func TestUpdateCheckIsSingleFlightAndDiscardsRevokedAuthority(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	feed := fixtureFeed(t, "0.2.0")
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > 10*time.Second {
			t.Error("network request did not retain the total observation budget")
		}
		close(entered)
		<-release
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(feed)))}, nil
	})
	service, err := New(Options{CurrentVersion: "0.1.0", FeedURL: "https://updates.example.com/release.json", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	var authorized atomic.Bool
	authorized.Store(true)
	denied := errors.New("fixture administrator was revoked")
	checkAuthority := func(context.Context) error {
		if !authorized.Load() {
			return denied
		}
		return nil
	}
	done := make(chan error, 1)
	go func() { _, err := service.Check(context.Background(), checkAuthority); done <- err }()
	<-entered
	if _, err := service.Check(context.Background(), allowCheck); !errors.Is(err, ErrBusy) {
		t.Error("simultaneous check was not rejected")
	}
	authorized.Store(false)
	close(release)
	if err := <-done; !errors.Is(err, denied) {
		t.Fatal("network result bypassed current administrator authority")
	}
	if view := service.Status(); view.State != NotChecked || view.CheckedAt != nil || view.Release != nil {
		t.Fatal("revoked actor published the pending release result")
	}
}

func TestCanceledUpdateCheckDoesNotPublishAResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	service, err := New(Options{CurrentVersion: "0.1.0", FeedURL: "https://updates.example.com/release.json", Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(entered)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := service.Check(ctx, allowCheck); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("canceled check did not stop")
	}
	if view := service.Status(); view.State != NotChecked || view.CheckedAt != nil {
		t.Fatal("canceled request published a success or global failure")
	}
}
