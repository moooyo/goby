package server

import (
	"net/http/httptest"
	"testing"
)

func TestDecodeLocalCredentialsPreservesAbsentClearAndReplacement(t *testing.T) {
	for _, test := range []struct {
		body               string
		local, pin         bool
		wantLocal, wantPin string
	}{
		{body: `{"Revision":"1","EnableLocalPassword":false}`},
		{body: `{"Revision":"2","EnableLocalPassword":false,"LocalPassword":"","ProfilePin":""}`, local: true, pin: true},
		{body: `{"Revision":"3","EnableLocalPassword":true,"LocalPassword":" separate password ","ProfilePin":"0123"}`, local: true, pin: true, wantLocal: " separate password ", wantPin: "0123"},
	} {
		response := httptest.NewRecorder()
		input, ok := decodeLocalCredentials(response, managedUserRequestForTest(test.body, "application/json"))
		if !ok || (input.LocalPassword != nil) != test.local || (input.ProfilePin != nil) != test.pin {
			t.Fatal("decoder lost credential omission semantics")
		}
		if input.LocalPassword != nil && *input.LocalPassword != test.wantLocal || input.ProfilePin != nil && *input.ProfilePin != test.wantPin {
			t.Fatal("decoder changed a supplied credential")
		}
	}
	for _, body := range []string{
		`{"Revision":"1","EnableLocalPassword":false,"LocalPassword":null}`,
		`{"Revision":"1","EnableLocalPassword":false,"ProfilePin":1234}`,
		`{"Revision":1,"EnableLocalPassword":false}`,
		`{"Revision":"1"}`,
		`{"Revision":"1","EnableLocalPassword":false,"ProfilePin":"1234","ProfilePin":"5678"}`,
		`{"Revision":"1","EnableLocalPassword":false,"Unknown":true}`,
	} {
		if _, ok := decodeLocalCredentials(httptest.NewRecorder(), managedUserRequestForTest(body, "application/json")); ok {
			t.Fatal("ambiguous credential request was accepted")
		}
	}
}
