package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestNotificationManagementFailuresRemainUnavailableAndPrivate(t *testing.T) {
	for _, path := range []string{"/admin/v1/notifications", "/emby/Notifications"} {
		for _, test := range []struct {
			err    error
			status int
			code   string
		}{
			{fmt.Errorf("lock notification management: %w", errors.New("private database failure")), http.StatusServiceUnavailable, "notification_unavailable"},
			{fmt.Errorf("lock notification management: %w", context.DeadlineExceeded), http.StatusServiceUnavailable, "notification_unavailable"},
			{fmt.Errorf("lock notification management: %w", context.Canceled), http.StatusServiceUnavailable, "notification_unavailable"},
			{fmt.Errorf("authorize notification: %w", identity.ErrUnauthorized), http.StatusUnauthorized, "invalid_credentials"},
		} {
			r := httptest.NewRequest(http.MethodPut, path, nil)
			w := httptest.NewRecorder()
			(&Server{}).notificationError(w, r, test.err)
			wantText := test.code
			if test.status == http.StatusUnauthorized && strings.HasPrefix(path, "/emby/") {
				wantText = embyInvalidTokenMessage
			}
			if w.Code != test.status || !strings.Contains(w.Body.String(), wantText) || strings.Contains(w.Body.String(), "private database failure") || strings.Contains(w.Body.String(), "lock notification management") {
				t.Fatalf("notification error mapping changed or exposed an internal failure: status=%d body=%s", w.Code, w.Body.String())
			}
		}
	}
}
