//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPImageSlowTransfersReleaseProcessingSlotsAndBoundAdmission(t *testing.T) {
	fixture := newImageAPIFixture(t)
	path := "/emby/Items/" + fixture.itemID + "/Images/Primary"
	viewer, err := fixture.f.users.CreateUser(fixture.f.ctx, "Image Transfer Viewer", "image-transfer-viewer-password", false)
	if err != nil {
		t.Fatal("create the authorized image transfer viewer")
	}
	if _, err := fixture.f.pool.Exec(fixture.f.ctx, `UPDATE users SET policy = policy || jsonb_build_object(
		'EnableAllFolders', false, 'EnabledFolders', jsonb_build_array($2::text)) WHERE id = $1`, viewer.ID, fixture.libraryID); err != nil {
		t.Fatal("limit the image transfer viewer to its fixture library")
	}
	viewerToken := stringValue(t, fixture.f.embyLogin(t, viewer.Name, "image-transfer-viewer-password"), "AccessToken")
	headers := http.Header{"X-Emby-Token": {viewerToken}}
	type transfer struct {
		response *blockedDiagnosticResponse
		cancel   context.CancelFunc
		done     chan any
	}
	var pending []transfer
	defer func() {
		for _, item := range pending {
			item.cancel()
			item.response.release()
		}
		for _, item := range pending {
			select {
			case recovered := <-item.done:
				if recovered != http.ErrAbortHandler {
					t.Error("cancelled image did not abort its partial response")
				}
			case <-time.After(5 * time.Second):
				t.Error("cancelled image writer retained a handler")
			}
			item.response.assertDrainedDeadline(t)
		}
		fixture.f.app.images.mu.Lock()
		defer fixture.f.app.images.mu.Unlock()
		if fixture.f.app.images.activeTransfers != 0 || fixture.f.app.images.activeBytes != 0 || len(fixture.f.app.images.slots) != 0 {
			t.Error("cancelled image transfers retained processing or transmission capacity")
		}
	}()
	for index := 0; index < imageTransferCount; index++ {
		ctx, cancel := context.WithCancel(fixture.f.ctx)
		response := newBlockedDiagnosticResponse()
		done := make(chan any, 1)
		pending = append(pending, transfer{response: response, cancel: cancel, done: done})
		request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		request.Header.Set("X-Emby-Token", viewerToken)
		go func() {
			defer func() { done <- recover() }()
			fixture.f.handler.ServeHTTP(response, request)
		}()
		select {
		case <-response.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("authorized image failed to enter its bounded transmission phase")
		}
		if response.Code != http.StatusOK {
			t.Fatalf("authorized image entered transmission with status %d, want 200", response.Code)
		}
		if len(fixture.f.app.images.slots) != 0 {
			t.Fatal("a stalled image transmission retained a processing slot")
		}
		if index == 3 {
			// A new uncached variant must still render while four earlier bodies
			// are stalled, the situation that previously exhausted all work slots.
			response := fixture.f.request(t, http.MethodGet, path+"?Width=64&Format=png", nil, headers)
			assertAPIImage(t, response, 64, 96, "png")
		}
	}
	expectStatus(t, fixture.f.request(t, http.MethodGet, path, nil, headers), http.StatusTooManyRequests)
	if len(fixture.f.app.images.slots) != 0 {
		t.Fatal("rejected image transmission retained a processing slot")
	}
}

func TestHTTPOriginalOwnerLimitPreservesPeerRangesAndRecoversAfterClose(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	target := originalRevocationSparseSource(t, fixture)
	peerID := managedHTTPCreate(t, fixture.f, fixture.cookie, csrfToken(fixture.cookie.Value), "Original Quota Peer", false)
	originalRevocationUpdateUser(t, fixture, peerID, func(input map[string]any) {
		policy := input["Policy"].(map[string]any)
		policy["EnableAllFolders"] = false
		policy["EnabledFolders"] = []string{fixture.video.libraryID}
		policy["EnableMediaPlayback"] = true
	})
	peerToken := stringValue(t, fixture.f.embyLogin(t, "Original Quota Peer", "managed-user-password"), "AccessToken")
	var opened []*originalRevocationResponse
	defer func() {
		for _, response := range opened {
			response.close()
		}
		originalRevocationWaitSlots(t, fixture, 0)
	}()
	for index := 0; index < maxOriginalOwnerStreams; index++ {
		opened = append(opened, originalRevocationOpen(t, fixture, target, fixture.token))
	}
	expectStreamStatus(t, fixture.request(t, http.MethodHead, target, fixture.token, nil, nil), http.StatusTooManyRequests)
	peer := fixture.request(t, http.MethodGet, target, peerToken, http.Header{"Range": {"bytes=0-31"}}, nil)
	expectStreamStatus(t, peer, http.StatusPartialContent)
	if len(peer.body) != 32 {
		t.Fatal("a saturated owner prevented another user's bounded range")
	}
	opened[0].close()
	originalRevocationWaitSlots(t, fixture, maxOriginalOwnerStreams-1)
	expectStreamStatus(t, fixture.request(t, http.MethodHead, target, fixture.token, nil, nil), http.StatusOK)
}

func TestHTTPSubtitleCancellationInterruptsBodyAndFinalFlush(t *testing.T) {
	fixture := newSubtitleHTTPFixture(t)
	for _, onFlush := range []bool{false, true} {
		name := "body"
		if onFlush {
			name = "final flush"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(fixture.p.s.f.ctx)
			defer cancel()
			response := newBlockedDiagnosticResponse()
			response.blockOnFlush = onFlush
			defer response.release()
			request := httptest.NewRequest(http.MethodGet, subtitleHTTPRoute(fixture, 6, "", "srt"), nil).WithContext(ctx)
			request.Header.Set("X-Emby-Token", fixture.p.s.token)
			done := make(chan any, 1)
			go func() {
				defer func() { done <- recover() }()
				fixture.p.s.f.handler.ServeHTTP(response, request)
			}()
			select {
			case <-response.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("subtitle did not begin the expected blocked transfer")
			}
			cancel()
			select {
			case recovered := <-done:
				if recovered != http.ErrAbortHandler {
					t.Fatal("a cancelled subtitle reported normal transfer completion")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request cancellation did not interrupt the subtitle writer")
			}
			response.assertDrainedDeadline(t)
			if len(fixture.p.s.f.app.subtitleSlots) != 0 {
				t.Fatal("a cancelled subtitle retained its slot")
			}
		})
	}
}
