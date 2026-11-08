package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/settings"
)

func TestConfigurationPolicyProjectionOwnsItsSubnetArray(t *testing.T) {
	for _, subnets := range [][]string{{}, {"192.0.2.0/24", "2001:db8::/32"}} {
		policy := settings.ServerPolicySnapshot{Values: settings.ServerPolicyValues{
			EnableRemoteAccess: false, LocalNetworkSubnets: subnets, UICulture: "en-US"}}
		value := serverConfigurationDTO(settings.Configuration{CanManage: true, ServerPolicy: &policy})
		projected, ok := value["LocalNetworkSubnets"].([]string)
		if !ok || projected == nil || !reflect.DeepEqual(projected, subnets) ||
			value["EnableRemoteAccess"] != false || value["UICulture"] != "en-US" {
			t.Fatal("configuration projection omitted backed policy values or emitted a null subnet list")
		}
		if len(projected) != 0 {
			projected[0] = "198.51.100.0/24"
			if policy.Values.LocalNetworkSubnets[0] != "192.0.2.0/24" {
				t.Fatal("configuration projection shared the stored subnet array")
			}
		}
	}
}

func TestConfigurationPolicyDecoderPreservesPresenceAndExactTypes(t *testing.T) {
	for _, section := range []settings.ConfigurationSection{settings.ConfigurationFull, settings.ConfigurationPartial} {
		for _, body := range []string{`{}`, `{"EnableRemoteAccess":false,"LocalNetworkSubnets":[],"UICulture":"en-US"}`,
			`{"ENABLEREMOTEACCESS":true,"localNetworkSubnets":["192.0.2.0/24"],"uiCulture":"zh-CN"}`} {
			request := httptest.NewRequest(http.MethodPost, "/emby/System/Configuration", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			mutation, ok := decodeConfiguration(response, request, section)
			if !ok {
				t.Fatalf("supported server policy object was rejected: %s", body)
			}
			if body == `{}` {
				if mutation.EnableRemoteAccess != nil || mutation.LocalNetworkSubnets != nil || mutation.UICulture != nil {
					t.Fatal("omitted policy fields were converted into writes")
				}
			} else if mutation.EnableRemoteAccess == nil || mutation.LocalNetworkSubnets == nil ||
				*mutation.LocalNetworkSubnets == nil || mutation.UICulture == nil {
				t.Fatal("explicit false or empty-array policy fields lost their presence")
			}
		}
	}
	for _, test := range []struct {
		section settings.ConfigurationSection
		body    string
	}{
		{settings.ConfigurationPartial, `{"EnableRemoteAccess":null}`},
		{settings.ConfigurationPartial, `{"EnableRemoteAccess":0}`},
		{settings.ConfigurationPartial, `{"EnableRemoteAccess":"false"}`},
		{settings.ConfigurationPartial, `{"LocalNetworkSubnets":null}`},
		{settings.ConfigurationPartial, `{"LocalNetworkSubnets":"192.0.2.0/24"}`},
		{settings.ConfigurationPartial, `{"LocalNetworkSubnets":[1]}`},
		{settings.ConfigurationPartial, `{"UICulture":null}`},
		{settings.ConfigurationPartial, `{"UICulture":true}`},
		{settings.ConfigurationFull, `{"EnableRemoteAccess":true,"enableremoteaccess":false}`},
		{settings.ConfigurationFull, `{"UICulture":"en-US","\u0055ICulture":"zh-CN"}`},
		{settings.ConfigurationFull, `{"LocalNetworkSubnets":[],"LOCALNETWORKSUBNETS":[]}`},
		{settings.ConfigurationFull, `{"EnableRemoteAccess":true,"ConfirmDisconnect":true}`},
		{settings.ConfigurationEncoding, `{"EnableRemoteAccess":true}`},
		{settings.ConfigurationSubtitles, `{"UICulture":"en-US"}`},
		{settings.ConfigurationTasks, `{"LocalNetworkSubnets":[]}`},
	} {
		request := httptest.NewRequest(http.MethodPost, "/emby/System/Configuration", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		if _, ok := decodeConfiguration(response, request, test.section); ok || response.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous, null, wrong-type or mixed-section policy field was accepted: %s", test.body)
		}
	}
}
