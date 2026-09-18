package dynamicsource

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestHTTPConnectorProbesPrivatePrefixAndReplaysExactBytes(t *testing.T) {
	body := bytes.Repeat([]byte("transport bytes\n"), 15000)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer configured-secret" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("configured upstream request changed")
		}
		_, _ = w.Write(body)
	}))
	defer upstream.Close()
	connector := NewHTTPConnector(media.Prober{}, t.TempDir())
	connector.probe = func(_ context.Context, file *os.File) (media.Info, error) {
		prefix := make([]byte, probeSampleBytes)
		if _, err := file.ReadAt(prefix, 0); err != nil || !bytes.Equal(prefix, body[:probeSampleBytes]) {
			t.Fatal("probe did not receive the exact bounded private prefix")
		}
		facts := testFacts()
		facts.DurationTicks, facts.Size, facts.FileChangeTimeNs = 99, 77, 66
		return facts, nil
	}
	connection, err := connector.Open(context.Background(), Definition{ItemID: "42", URL: upstream.URL, Headers: map[string]string{"Authorization": "Bearer configured-secret"}, Infinite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Reader.Close()
	actual, err := io.ReadAll(connection.Reader)
	if err != nil || !bytes.Equal(actual, body) {
		t.Fatal("probing consumed or duplicated media bytes")
	}
	if connection.Info.DurationTicks != 0 || connection.Info.Size != 0 || connection.Info.FileChangeTimeNs != 0 {
		t.Fatal("prefix facts were advertised as complete source facts")
	}
	files, err := os.ReadDir(connector.tempDir)
	if err != nil || len(files) != 0 {
		t.Fatal("private probe sample was retained after opening")
	}
}

func TestHTTPConnectorNeverFollowsRedirectWithConfiguredCredentials(t *testing.T) {
	var destinationRequests atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { destinationRequests.Add(1) }))
	defer destination.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	connector := NewHTTPConnector(media.Prober{}, t.TempDir())
	if _, err := connector.Open(context.Background(), Definition{ItemID: "42", URL: upstream.URL, Headers: map[string]string{"Authorization": "secret"}}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("redirect was accepted")
	}
	if destinationRequests.Load() != 0 {
		t.Fatal("configured credentials followed an upstream redirect")
	}
}

func TestConfiguredSourceValidationRejectsProtocolAndHeaderInjection(t *testing.T) {
	for _, target := range []string{"file:///private/video", "concat:https://media", "https://user:secret@example.com/media", "https://example.com/media#fragment", "https://example.com/\r\nHeader: value"} {
		if err := validateDefinition(Definition{ItemID: "42", URL: target}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid configured target was accepted: %q", target)
		}
	}
	for _, header := range []map[string]string{{"Authorization": "token\r\nInjected: yes"}, {"Host": "another.invalid"}, {"Range": "bytes=100-"}, {"Bad Header": "value"}, {"X-Long": strings.Repeat("x", 4097)}} {
		if err := validateDefinition(Definition{ItemID: "42", URL: "https://example.invalid/media", Headers: header}); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid upstream header was accepted")
		}
	}
}
