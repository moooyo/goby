//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPEmbyDiagnosticDownloadRechecksEveryApplicationIdentityDuringTransfer(t *testing.T) {
	for _, invalidation := range []string{"credential", "key sidecar", "client context"} {
		t.Run(invalidation, func(t *testing.T) {
			f := newEmbyObservabilityHTTPFixture(t)
			principal, err := f.users.ResolveEmbyForClient(f.ctx, f.keyToken, identity.Client{
				Name: "Observability fixture", DeviceID: "observability-client", Device: "Linux", Version: "1.0",
			})
			if err != nil || !principal.IsApplicationKey() || principal.ClientSessionID == "" || principal.User.ID != "" {
				t.Fatal("resolve the owned complete userless application identity")
			}
			request := httptest.NewRequest(http.MethodGet, "/emby/System/Logs/"+f.name+"?Sanitize=false", nil).WithContext(f.ctx)
			request.Header = f.keyHeaders.Clone()
			response := newBlockedDiagnosticResponse()
			response.blockOnFlush = invalidation == "client context"
			t.Cleanup(response.release)
			finished := make(chan any, 1)
			go func() {
				defer func() { finished <- recover() }()
				f.handler.ServeHTTP(response, request)
			}()
			select {
			case <-response.entered:
			case <-finished:
				t.Fatal("the application download did not begin its bounded transfer")
			case <-time.After(5 * time.Second):
				t.Fatal("the application download did not reach its write barrier")
			}
			statement, id := "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", principal.SessionID
			switch invalidation {
			case "key sidecar":
				statement = "DELETE FROM application_keys WHERE credential_id = $1"
			case "client context":
				statement, id = "DELETE FROM application_key_clients WHERE id = $1", principal.ClientSessionID
			}
			mutation, cancel := context.WithTimeout(f.ctx, 2*time.Second)
			tag, err := f.pool.Exec(mutation, statement, id)
			cancel()
			if err != nil || tag.RowsAffected() != 1 {
				t.Fatal("the transfer retained an application identity lock or did not invalidate the owned identity")
			}
			select {
			case recovered := <-finished:
				if recovered != http.ErrAbortHandler {
					t.Fatal("a download completed normally after its application identity became invalid")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("an invalid application identity did not interrupt its blocked download")
			}
			response.assertDrainedDeadline(t)
			if response.Header().Get("Content-Type") != "text/plain; charset=UTF-8" ||
				response.Header().Get("Content-Length") != strconv.Itoa(len(f.content)) || response.Body.Len() == 0 {
				t.Fatal("the bounded application download lost its original sanitized representation")
			}
			for range diagnostics.MaxReaders {
				reader, err := f.store.Snapshot(f.ctx, f.name)
				if err != nil {
					t.Fatal("an application download retained its reader slot after invalidation")
				}
				t.Cleanup(func() { _ = reader.Close() })
			}
		})
	}
}

func TestHTTPEmbyActivityKeepsRealApplicationAttributionWithoutInventingAUser(t *testing.T) {
	f := newEmbyObservabilityHTTPFixture(t)
	const privateValue = "Application-owned private settings value"
	response := f.request(t, http.MethodPost, "/emby/System/Configuration/Partial", map[string]any{"ServerName": privateValue}, f.keyHeaders)
	expectStatus(t, response, http.StatusNoContent)
	native := nativeActivityHTTPPage(t, f.serverFixture, f.cookie, "?Action=settings.updated")
	nativeItems := nativeObservabilityItems(t, native)
	if len(nativeItems) != 1 {
		t.Fatal("the application settings update did not create one committed activity fact")
	}
	nativeItem := nativeObservabilityItem(t, nativeItems[0])
	actor := objectValue(t, nativeItem, "Actor")
	if actor["Kind"] != "application_key" || actor["Name"] != nil || nativeItem["Source"] != "emby" || actor["Id"] == nil {
		t.Fatal("the native fact did not retain the real userless application actor")
	}
	result := embyObservabilityHTTPPage(t, f.request(t, http.MethodGet, "/emby/System/ActivityLog/Entries?Limit=200", nil, f.keyHeaders))
	found := false
	for _, raw := range nativeObservabilityItems(t, result) {
		item := nativeObservabilityItem(t, raw)
		if item["Type"] != "settings.updated" {
			continue
		}
		found = true
		nativeObservabilityFields(t, item, "Id", "Name", "Overview", "Type", "Date", "Severity")
		if item["Name"] != "Server settings updated" || item["Overview"] != "Supported server settings were updated." {
			t.Fatal("the compatibility activity copied a private settings value into display text")
		}
	}
	if !found {
		t.Fatal("the compatibility activity did not project its committed application event")
	}
}
