//go:build linux

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const (
	originalRevocationSourceBytes = int64(256 << 20)
	originalRevocationDrainBytes  = int64(8 << 20)
)

type originalRevocationResponse struct {
	connection *net.TCPConn
	response   *http.Response
}

// The sparse extension keeps an original response larger than any socket
// buffer without allocating or reading hundreds of MiB of fixture media.
func originalRevocationSparseSource(t *testing.T, fixture *streamHTTPFixture) string {
	t.Helper()
	if err := os.Truncate(fixture.video.path, originalRevocationSourceBytes); err != nil {
		t.Fatal("extend owned sparse original source")
	}
	info, err := os.Stat(fixture.video.path)
	if err != nil || info.Size() != originalRevocationSourceBytes {
		t.Fatal("inspect owned sparse original source")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Blocks*512 > 1<<20 {
		t.Fatal("original revocation fixture allocated excessive physical storage")
	}
	fixture.rescan(t, fixture.video.libraryID)
	return "/emby/Videos/" + fixture.video.id + "/original.mp4"
}

func originalRevocationOpen(t *testing.T, fixture *streamHTTPFixture, target, token string) *originalRevocationResponse {
	t.Helper()
	base, err := url.Parse(fixture.server.URL)
	if err != nil {
		t.Fatal("parse owned original HTTP server address")
	}
	address, err := net.ResolveTCPAddr("tcp", base.Host)
	if err != nil {
		t.Fatal("resolve owned original HTTP server address")
	}
	connection, err := net.DialTCP("tcp", nil, address)
	if err != nil {
		t.Fatalf("connect original response (%T)", err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetReadBuffer(32 * 1024); err != nil {
		t.Fatal("bound original HTTP receive buffer")
	}
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal("bound original HTTP header exchange")
	}
	request, err := http.NewRequest(http.MethodGet, fixture.server.URL+target, nil)
	if err != nil {
		t.Fatal("construct original revocation request")
	}
	request.Close = true
	request.Header.Set("X-Emby-Token", token)
	if err := request.Write(connection); err != nil {
		t.Fatalf("send original revocation request (%T)", err)
	}
	response, err := http.ReadResponse(bufio.NewReaderSize(connection, 4096), request)
	if err != nil {
		t.Fatalf("read original response headers (%T)", err)
	}
	opened := &originalRevocationResponse{connection: connection, response: response}
	t.Cleanup(opened.close)
	if response.StatusCode != http.StatusOK || response.ContentLength != originalRevocationSourceBytes ||
		response.Header.Get("Content-Type") != "video/mp4" || response.Header.Get("ETag") == "" {
		t.Fatal("long original GET did not start the complete authorized representation")
	}
	prefix := make([]byte, 32)
	if _, err := io.ReadFull(response.Body, prefix); err != nil || !bytes.Equal(prefix, fixture.video.data[:len(prefix)]) {
		t.Fatal("long original GET did not publish source bytes before revocation")
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		t.Fatal("clear initial original HTTP deadline")
	}
	return opened
}

func (opened *originalRevocationResponse) close() {
	// Closing the socket first prevents Body.Close from draining a large body.
	_ = opened.connection.Close()
	_ = opened.response.Body.Close()
}

func originalRevocationWaitSlots(t *testing.T, fixture *streamHTTPFixture, want int) {
	t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if len(fixture.f.app.streamSlots) == want {
			return
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatalf("active original response count = %d, want %d within the revocation bound", len(fixture.f.app.streamSlots), want)
		}
	}
}

func originalRevocationAssertAborted(t *testing.T, opened *originalRevocationResponse) {
	t.Helper()
	// The handler has already exited. Drain only bounded socket leftovers,
	// allowing a larger receive window so a small initial window cannot make
	// buffered TCP delivery look like a slow authorization watcher.
	if err := opened.connection.SetReadBuffer(1 << 20); err != nil {
		t.Fatal("expand bounded original response drain window")
	}
	if err := opened.connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal("bound revoked original response drain")
	}
	read, err := io.CopyBuffer(io.Discard, io.LimitReader(opened.response.Body, originalRevocationDrainBytes), make([]byte, 32*1024))
	var timeout net.Error
	if err == nil || errors.As(err, &timeout) && timeout.Timeout() || read >= originalRevocationDrainBytes {
		t.Fatalf("revoked original GET did not end with a bounded transport error (%T, %d drained bytes)", err, read)
	}
	opened.close()
}

func originalRevocationAssertPeerActive(t *testing.T, fixture *streamHTTPFixture, opened *originalRevocationResponse) {
	t.Helper()
	if err := opened.connection.SetReadBuffer(1 << 20); err != nil {
		t.Fatal("expand authorized original response window")
	}
	if err := opened.connection.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal("bound authorized original response read")
	}
	// Read well beyond the deliberately small receive buffer. A closed peer
	// cannot pass merely because its original headers and prefix were buffered.
	read, err := io.CopyN(io.Discard, opened.response.Body, originalRevocationDrainBytes)
	if err != nil || read != originalRevocationDrainBytes || len(fixture.f.app.streamSlots) != 1 {
		t.Fatalf("another user's revocation interrupted the still-authorized original response (%T)", err)
	}
	if err := opened.connection.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal("clear authorized original response deadline")
	}
}

// Reuse the native managed-user schema helpers, but submit mutations over the
// real HTTP listener while original response handlers are blocked on writes.
func originalRevocationUpdateUser(t *testing.T, fixture *streamHTTPFixture, id string, change func(map[string]any)) {
	t.Helper()
	input := managedHTTPUpdateBody(managedHTTPDetail(t, fixture.f, fixture.cookie, id))
	change(input)
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal("encode native original-access mutation")
	}
	request, err := http.NewRequestWithContext(fixture.f.ctx, http.MethodPut, fixture.server.URL+"/admin/v1/users/"+id, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal("construct native original-access mutation")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrfToken(fixture.cookie.Value))
	request.AddCookie(fixture.cookie)
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatalf("perform native original-access mutation (%T)", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("native original-access mutation status = %d", response.StatusCode)
	}
	var result struct {
		User                  map[string]any
		CurrentSessionRevoked bool
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result); err != nil || result.User["Id"] != id || result.CurrentSessionRevoked {
		t.Fatal("native original-access mutation did not preserve the administrator's session")
	}
}

func originalRevocationAssertRestored(t *testing.T, fixture *streamHTTPFixture, target, token string) {
	t.Helper()
	head := fixture.request(t, http.MethodHead, target, token, nil, nil)
	expectStreamStatus(t, head, http.StatusOK)
	if len(head.body) != 0 || head.header.Get("Content-Length") != strconv.FormatInt(originalRevocationSourceBytes, 10) {
		t.Fatal("restored original HEAD lost its representation metadata")
	}
	ranged := fixture.request(t, http.MethodGet, target, token, http.Header{"Range": {"bytes=0-31"}}, nil)
	expectStreamStatus(t, ranged, http.StatusPartialContent)
	if !bytes.Equal(ranged.body, fixture.video.data[:32]) || ranged.header.Get("Content-Range") != "bytes 0-31/"+strconv.FormatInt(originalRevocationSourceBytes, 10) {
		t.Fatal("restored original access did not retain exact byte ranges")
	}
}

func TestHTTPOriginalNativeRevocationInterruptsActiveStreamAndIsolatesOtherUsers(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	target := originalRevocationSparseSource(t, fixture)
	peerID := managedHTTPCreate(t, fixture.f, fixture.cookie, csrfToken(fixture.cookie.Value), "Original Stream Peer", false)
	originalRevocationUpdateUser(t, fixture, peerID, func(input map[string]any) {
		policy := input["Policy"].(map[string]any)
		policy["EnableAllFolders"] = false
		policy["EnabledFolders"] = []string{fixture.video.libraryID, fixture.audio.libraryID}
		policy["EnableMediaPlayback"] = true
	})
	peerToken := stringValue(t, fixture.f.embyLogin(t, "Original Stream Peer", "managed-user-password"), "AccessToken")
	for _, disable := range []bool{true, false} {
		name := "library ACL"
		if disable {
			name = "account disablement"
		}
		if !t.Run(name, func(t *testing.T) {
			oldToken := fixture.token
			affected := originalRevocationOpen(t, fixture, target, oldToken)
			peer := originalRevocationOpen(t, fixture, target, peerToken)
			if count := len(fixture.f.app.streamSlots); count != 2 {
				t.Fatalf("responses did not remain active before revocation: %d", count)
			}
			originalRevocationUpdateUser(t, fixture, fixture.viewerID, func(input map[string]any) {
				if disable {
					input["IsDisabled"] = true
				} else {
					policy := input["Policy"].(map[string]any)
					policy["EnableAllFolders"] = false
					policy["EnabledFolders"] = []string{fixture.audio.libraryID}
				}
			})
			// Neither large body is consumed while the watcher must detect the
			// committed change and interrupt the affected blocked write.
			originalRevocationWaitSlots(t, fixture, 1)
			originalRevocationAssertAborted(t, affected)
			originalRevocationAssertPeerActive(t, fixture, peer)
			denied := http.StatusNotFound
			if disable {
				denied = http.StatusUnauthorized
			}
			expectStreamStatus(t, fixture.request(t, http.MethodHead, target, oldToken, nil, nil), denied)
			originalRevocationUpdateUser(t, fixture, fixture.viewerID, func(input map[string]any) {
				input["IsDisabled"] = false
				policy := input["Policy"].(map[string]any)
				policy["EnableAllFolders"] = false
				policy["EnabledFolders"] = []string{fixture.video.libraryID, fixture.audio.libraryID}
			})
			if disable {
				// Re-enabling an account does not resurrect revoked credentials.
				expectStreamStatus(t, fixture.request(t, http.MethodHead, target, oldToken, nil, nil), http.StatusUnauthorized)
				fixture.token = stringValue(t, fixture.f.embyLogin(t, "Stream Viewer", "stream-viewer-password"), "AccessToken")
			} else if fixture.token != oldToken {
				t.Fatal("ACL-only recovery unnecessarily replaced the authentication token")
			}
			originalRevocationAssertRestored(t, fixture, target, fixture.token)
			peer.close()
			originalRevocationWaitSlots(t, fixture, 0)
		}) {
			return
		}
	}
}

func TestHTTPOriginalServerCloseInterruptsBlockedResponseAndJoinsWatchers(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	target := originalRevocationSparseSource(t, fixture)
	opened := originalRevocationOpen(t, fixture, target, fixture.token)
	if count := len(fixture.f.app.streamSlots); count != 1 {
		t.Fatalf("original response was not active at shutdown: %d", count)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fixture.f.app.Close(ctx); err != nil {
		t.Fatalf("server shutdown did not stop its blocked original response and watcher (%T)", err)
	}
	originalRevocationWaitSlots(t, fixture, 0)
	originalRevocationAssertAborted(t, opened)
}
