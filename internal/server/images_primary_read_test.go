package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestImagePrimaryReadAdmissionReturnsRetryableImageCapacityError(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/Items/item/Images/Primary", nil)
	response := httptest.NewRecorder()
	server.imageError(response, request, library.ErrBusy)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "2" ||
		!strings.Contains(response.Body.String(), "image_read_limit") {
		t.Fatalf("artwork root admission error=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestImagePrimaryReadCancellationDoesNotWriteCapacityResponse(t *testing.T) {
	server := &Server{}
	request := httptest.NewRequest(http.MethodGet, "/Items/item/Images/Primary", nil)
	response := httptest.NewRecorder()
	server.imageError(response, request, errors.Join(context.Canceled, library.ErrBusy))
	if response.Body.Len() != 0 || response.Header().Get("Retry-After") != "" {
		t.Fatalf("canceled image request wrote a capacity response: headers=%v body=%s", response.Header(), response.Body.String())
	}
}
