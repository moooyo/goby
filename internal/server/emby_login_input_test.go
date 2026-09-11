package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDecodeEmbyLoginFormAndJSONTransports(t *testing.T) {
	for _, test := range []struct {
		name, mediaType, body string
		byName                bool
		want                  embyLoginInput
	}{
		{"browser_form", "application/x-www-form-urlencoded; charset=UTF-8", "Username=Form+Viewer&Pw=secret", true, embyLoginInput{"Form Viewer", "secret"}},
		{"escaped_form", "application/x-www-form-urlencoded", url.Values{"Username": {"Viewer + name"}, "Pw": {"password +&=% \u5bc6\u7801"}}.Encode(), true, embyLoginInput{"Viewer + name", "password +&=% \u5bc6\u7801"}},
		{"case_variants", "application/x-www-form-urlencoded; charset=\"utf-8\"", "username=Viewer&pW=secret", true, embyLoginInput{"Viewer", "secret"}},
		{"selected_user", "application/x-www-form-urlencoded", "Pw=secret", false, embyLoginInput{Pw: "secret"}},
		{"empty_selected_password", "application/x-www-form-urlencoded", "Pw=", false, embyLoginInput{}},
		{"passwordless_name", "application/x-www-form-urlencoded", "Username=Viewer", true, embyLoginInput{Username: "Viewer"}},
		{"json_name_regression", "application/json; charset=utf-8", `{"Username":"Viewer","Pw":"secret"}`, true, embyLoginInput{"Viewer", "secret"}},
		{"json_selected_regression", "application/json", `{"Pw":"secret"}`, false, embyLoginInput{Pw: "secret"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName?Username=Query&Pw=query-secret", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.mediaType)
			response := httptest.NewRecorder()
			got, ok := decodeEmbyLogin(response, request, test.byName)
			if !ok || got != test.want {
				t.Fatalf("login transport did not preserve body credentials: accepted=%t status=%d", ok, response.Code)
			}
			if request.Form != nil || request.PostForm != nil {
				t.Fatal("login decoding populated merged query/form credentials")
			}
		})
	}
}

func TestDecodeEmbyLoginFormRejectsAmbiguousAndMalformedBodies(t *testing.T) {
	for _, test := range []struct {
		name, body string
		byName     bool
	}{
		{"empty", "", true},
		{"duplicate_equal_name", "Username=Viewer&Username=Viewer&Pw=secret", true},
		{"duplicate_password", "Username=Viewer&Pw=secret&Pw=other", true},
		{"case_aliased_name", "Username=Viewer&username=Other&Pw=secret", true},
		{"encoded_name_alias", "Username=Viewer&%55sername=Viewer", true},
		{"unknown_identity", "Username=Viewer&Pw=secret&UserId=administrator", true},
		{"unknown_token", "Username=Viewer&Token=token", true},
		{"unsupported_password_field", "Username=Viewer&Password=secret", true},
		{"selected_username", "Username=Other&Pw=secret", false},
		{"selected_user_id", "Id=Other&Pw=secret", false},
		{"bad_key_escape", "%GG=Viewer&Pw=secret", true},
		{"bad_password_escape", "Username=Viewer&Pw=secret%2", true},
		{"invalid_decoded_utf8", "Username=Viewer&Pw=%ff", true},
		{"invalid_raw_utf8", "Username=Viewer&Pw=\xff", true},
		{"nul_password", "Username=Viewer&Pw=secret%00", true},
		{"bare_semicolon", "Username=Viewer;Pw=secret", true},
		{"empty_field_name", "=Viewer&Pw=secret", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			input, ok := decodeEmbyLogin(response, request, test.byName)
			if ok || input != (embyLoginInput{}) {
				t.Fatal("malformed or ambiguous form exposed credentials to authentication")
			}
			expectAPIError(t, response, http.StatusBadRequest, "invalid_form", true)
		})
	}
}

func TestDecodeEmbyLoginFormRejectsUnsupportedContentTypes(t *testing.T) {
	for _, mediaType := range []string{"", "text/plain", "multipart/form-data; boundary=test", "application/x-www-form-urlencoded; charset=latin1",
		"application/x-www-form-urlencoded; other=value", "application/x-www-form-urlencoded; charset=utf-8; charset=utf-8",
		"application/x-www-form-urlencoded; charset", "application/x-www-form-urlencoded, application/json"} {
		request := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", strings.NewReader("Username=Viewer&Pw=secret"))
		request.Header.Set("Content-Type", mediaType)
		response := httptest.NewRecorder()
		if _, ok := decodeEmbyLogin(response, request, true); ok {
			t.Fatal("unsupported login media type was accepted")
		}
		expectAPIError(t, response, http.StatusUnsupportedMediaType, "unsupported_media_type", true)
	}
	for _, header := range []string{"Content-Type", "Content-Encoding"} {
		request := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", strings.NewReader("Username=Viewer&Pw=secret"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Add(header, "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		if _, ok := decodeEmbyLogin(response, request, true); ok {
			t.Fatal("ambiguous media type or unsupported body encoding was accepted")
		}
		expectAPIError(t, response, http.StatusUnsupportedMediaType, "unsupported_media_type", true)
	}
}

type embyLoginCountingBody struct {
	reader io.Reader
	read   int
}

func (b *embyLoginCountingBody) Read(target []byte) (int, error) {
	n, err := b.reader.Read(target)
	b.read += n
	return n, err
}

func (*embyLoginCountingBody) Close() error { return nil }

func TestDecodeEmbyLoginFormBoundsActualBytesRegardlessOfLengthHint(t *testing.T) {
	for _, length := range []int64{-1, 0, 2, maxEmbyLoginFormBytes + 1} {
		body := &embyLoginCountingBody{reader: strings.NewReader("Username=" + strings.Repeat("x", maxEmbyLoginFormBytes+128))}
		request := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", nil)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Body, request.ContentLength = body, length
		response := httptest.NewRecorder()
		if input, ok := decodeEmbyLogin(response, request, true); ok || input != (embyLoginInput{}) {
			t.Fatal("oversized body reached the authentication layer")
		}
		expectAPIError(t, response, http.StatusRequestEntityTooLarge, "payload_too_large", true)
		if body.read != maxEmbyLoginFormBytes+1 {
			t.Fatalf("body reader consumed %d bytes, want limit plus one", body.read)
		}
	}
	body := "Username=" + strings.Repeat("x", maxEmbyLoginFormBytes-len("Username="))
	request := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if input, ok := decodeEmbyLogin(httptest.NewRecorder(), request, true); !ok || len(input.Username) != maxEmbyLoginFormBytes-len("Username=") {
		t.Fatal("exact-limit form did not reach its ordinary credential validation")
	}
}

type embyLoginFailedBody struct{}

func (embyLoginFailedBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (embyLoginFailedBody) Close() error             { return nil }

func TestDecodeEmbyLoginFormRejectsReadFailuresWithoutCredentials(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/emby/Users/AuthenticateByName", nil)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Body = embyLoginFailedBody{}
	response := httptest.NewRecorder()
	if input, ok := decodeEmbyLogin(response, request, true); ok || input != (embyLoginInput{}) {
		t.Fatal("a failed request body produced credentials")
	}
	if _, err := request.Body.Read(make([]byte, 1)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("body failure was replaced with a successful EOF")
	}
	expectAPIError(t, response, http.StatusBadRequest, "invalid_form", true)
}
