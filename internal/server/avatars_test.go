package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAvatarRevisionHeaderRejectsLossyAndWildcardPreconditions(t *testing.T) {
	for _, value := range []string{`*`, `W/"1"`, `1`, `"01"`, `"-1"`, `"1.5"`, `"1e3"`, `"1","2"`, `"\u0031"`, `"1\u0032"`, `"` + strings.Repeat("9", 79) + `"`} {
		request := httptest.NewRequest(http.MethodPut, "/admin/v1/users/test/image", nil)
		request.Header.Set("If-Match", value)
		response := httptest.NewRecorder()
		if _, ok := avatarMatchRevision(response, request, true); ok || response.Code != http.StatusBadRequest {
			t.Errorf("accepted avatar precondition %q: %d", value, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPut, "/admin/v1/users/test/image", nil)
	response := httptest.NewRecorder()
	if _, ok := avatarMatchRevision(response, request, true); ok || response.Code != http.StatusPreconditionRequired {
		t.Fatal("missing native avatar revision was accepted")
	}
	request.Header.Set("If-Match", `"9007199254740993"`)
	if revision, ok := avatarMatchRevision(httptest.NewRecorder(), request, true); !ok || *revision != "9007199254740993" {
		t.Fatal("exact revision was rounded through a JSON number")
	}
	request.Header.Set("If-Match", "\t \"9007199254740993\" \t")
	if revision, ok := avatarMatchRevision(httptest.NewRecorder(), request, true); !ok || *revision != "9007199254740993" {
		t.Fatal("HTTP optional whitespace changed the exact entity tag")
	}
}
