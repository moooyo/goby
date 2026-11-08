package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validAdminServerPolicyBody = `{"Revision":"9007199254740993","EnableRemoteAccess":false,"LocalNetworkSubnets":["192.168.0.0/16"],"UICulture":"en-US","ConfirmDisconnect":true}`

func TestAdminServerPolicyInputPreservesExactRevisionAndRejectsAmbiguity(t *testing.T) {
	request := adminSettingsRequestForTest(validAdminServerPolicyBody, "application/json")
	value, ok := decodeAdminServerPolicy(httptest.NewRecorder(), request, true)
	if !ok || value.Revision != 9007199254740993 || value.Values.EnableRemoteAccess || value.Values.UICulture != "en-US" || !value.ConfirmDisconnect {
		t.Fatal("server policy decoding lost an exact revision or configured value")
	}
	for _, test := range []struct {
		old, replacement string
		field            string
	}{
		{`"Revision":"9007199254740993"`, `"Revision":9007199254740993`, "Revision"},
		{`"Revision":"9007199254740993"`, `"Revision":"9223372036854775807"`, "Revision"},
		{`"EnableRemoteAccess":false`, `"EnableRemoteAccess":null`, "EnableRemoteAccess"},
		{`"EnableRemoteAccess":false`, `"EnableRemoteAccess":"false"`, "EnableRemoteAccess"},
		{`["192.168.0.0/16"]`, `null`, "LocalNetworkSubnets"},
		{`["192.168.0.0/16"]`, `["192.168.1.1/16"]`, "LocalNetworkSubnets"},
		{`"UICulture":"en-US"`, `"UICulture":"en"`, "UICulture"},
		{`"ConfirmDisconnect":true`, `"ConfirmDisconnect":null`, "ConfirmDisconnect"},
	} {
		body := strings.Replace(validAdminServerPolicyBody, test.old, test.replacement, 1)
		response := httptest.NewRecorder()
		if _, ok := decodeAdminServerPolicy(response, adminSettingsRequestForTest(body, "application/json"), true); ok || response.Code != http.StatusBadRequest {
			t.Errorf("invalid %s was accepted", test.field)
			continue
		}
		var result struct {
			Error struct{ Fields map[string]string }
		}
		if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Error.Fields[test.field] == "" {
			t.Errorf("invalid %s omitted its field error", test.field)
		}
	}
	for _, body := range []string{
		strings.TrimSuffix(validAdminServerPolicyBody, "}") + `,"Unknown":true}`,
		strings.TrimSuffix(validAdminServerPolicyBody, "}") + `,"UICulture":"zh-CN"}`,
		validAdminServerPolicyBody + `{}`,
	} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminServerPolicy(response, adminSettingsRequestForTest(body, "application/json"), true); ok || response.Code != http.StatusBadRequest {
			t.Fatal("ambiguous policy request was accepted")
		}
	}
}
