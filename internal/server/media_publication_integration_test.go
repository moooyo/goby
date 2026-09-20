//go:build linux

package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// This exercises actual TCP cancellation and descriptor closure. The separate
// media-edit fixtures prove container preservation and the publication commit.
func TestHTTPMediaPublicationJoinsBlockedOriginalTransport(t *testing.T) {
	mediaPublicationBlockedTransport(t, false)
}

func TestHTTPMediaPublicationJoinsBlockedDownloadTransport(t *testing.T) {
	mediaPublicationBlockedTransport(t, true)
}

func mediaPublicationBlockedTransport(t *testing.T, download bool) {
	t.Helper()
	fixture := newStreamHTTPFixture(t)
	target := originalRevocationSparseSource(t, fixture)
	if download {
		target = "/emby/Items/" + fixture.video.id + "/Download"
	}
	opened := originalRevocationOpen(t, fixture, target, fixture.token)
	originalRevocationWaitSlots(t, fixture, 1)
	ctx, cancel := context.WithTimeout(fixture.f.ctx, 10*time.Second)
	defer cancel()
	if err := fixture.f.app.retireReplacedMediaSource(ctx, fixture.video.id, media.SourceID(fixture.video.id)); err != nil {
		t.Fatalf("retire published source transport: %v", err)
	}
	originalRevocationWaitSlots(t, fixture, 0)
	originalRevocationAssertAborted(t, opened)
	// Retirement does not revoke account authority or blacklist the item. Once
	// a publication barrier is absent, a fresh authorized request still works.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.server.URL+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Emby-Token", fixture.token)
	request.Header.Set("Range", "bytes=0-3")
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 5))
	if err != nil || response.StatusCode != http.StatusPartialContent || !bytes.Equal(body, fixture.video.data[:4]) {
		t.Fatal("source retirement revoked a fresh valid request")
	}
}
