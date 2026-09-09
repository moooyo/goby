package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func remoteCommandTestRequest(command, query, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/emby/Sessions/target/Command"+query, strings.NewReader(body))
	request.SetPathValue("Id", "target")
	request.SetPathValue("Command", command)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestRemoteGeneralCommandPreservesFullArgumentsAndDiscardsNamedBodies(t *testing.T) {
	body := "{\"Id\":\"forged-target\",\"Name\":\"SetVolume\",\"ControllingUserId\":\"forged-controller\",\"Arguments\":{\"Volume\":\"37\"}}"
	full, err := parseRemoteGeneralCommand(remoteCommandTestRequest("", "", body), false)
	if err != nil || !reflect.DeepEqual(full, map[string]any{"Name": "SetVolume", "Arguments": map[string]string{"Volume": "37"}}) {
		t.Fatalf("full GeneralCommand lost its typed arguments: %v", err)
	}
	actor := identity.Principal{User: identity.User{ID: "controller"}}
	target := identity.ClientSession{SessionID: "authorized-target", UserID: "receiver"}
	envelope, err := remoteCommandEnvelope("GeneralCommand", full, actor, target, true)
	if err != nil || envelope.MessageType != "GeneralCommand" || envelope.MessageID != "" {
		t.Fatal("GeneralCommand envelope must leave unique message IDs to the hub")
	}
	var decoded map[string]any
	if json.Unmarshal(envelope.Data, &decoded) != nil || decoded["Id"] != target.SessionID ||
		decoded["ControllingUserId"] != actor.User.ID {
		t.Error("GeneralCommand trusted client-supplied target or controller identities")
	}
	for _, ignored := range []string{body, "ignored non-JSON body", ""} {
		named, err := parseRemoteGeneralCommand(remoteCommandTestRequest("VolumeUp", "", ignored), true)
		if err != nil || !reflect.DeepEqual(named, map[string]any{"Name": "VolumeUp", "Arguments": map[string]string{}}) {
			t.Fatalf("named GeneralCommand interpreted its ignored body: %v", err)
		}
		envelope, err := remoteCommandEnvelope("GeneralCommand", named, actor, target, false)
		if err != nil || json.Unmarshal(envelope.Data, &decoded) != nil {
			t.Fatal("named GeneralCommand could not be encoded")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(envelope.Data, &fields) != nil || fields["Id"] != nil {
			t.Error("named GeneralCommand must omit Data.Id")
		}
	}
}

func TestRemoteGeneralCommandRejectsAmbiguousTypesAndBoundViolations(t *testing.T) {
	for _, body := range []string{
		"null", "[]", "{}", "{\"Name\":null}", "{\"Name\":12}",
		"{\"Name\":\"SetVolume\",\"Arguments\":[]}",
		"{\"Name\":\"SetVolume\",\"Arguments\":{\"Volume\":37}}",
		"{\"Name\":\"SetVolume\",\"Arguments\":{\"Volume\":null}}",
		"{\"Name\":\"SetVolume\",\"Name\":\"Mute\"}",
		"{\"Name\":\"SetVolume\",\"name\":\"Mute\"}",
		"{\"Name\":\"SetVolume\",\"Arguments\":{\"Volume\":\"37\",\"Volume\":\"38\"}}",
		"{\"Name\":\"SetVolume\"} {}",
		"{\"Name\":\"" + strings.Repeat("a", maxRemoteCommandNameBytes+1) + "\"}",
		"{\"Name\":\"SetVolume\",\"Arguments\":{\"Value\":\"" + strings.Repeat("a", maxRemoteArgumentBytes+1) + "\"}}",
	} {
		if _, err := parseRemoteGeneralCommand(remoteCommandTestRequest("", "", body), false); !errors.Is(err, errRemoteCommandInput) {
			t.Errorf("malformed GeneralCommand error = %v, want invalid input", err)
		}
	}
	arguments := map[string]string{}
	for index := 0; index <= maxRemoteCommandArguments; index++ {
		arguments["argument-"+strconv.Itoa(index)] = "value"
	}
	encoded, err := json.Marshal(map[string]any{"Name": "Bounded", "Arguments": arguments})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseRemoteGeneralCommand(remoteCommandTestRequest("", "", string(encoded)), false); err == nil {
		t.Error("GeneralCommand accepted too many argument keys")
	}
	request := remoteCommandTestRequest("", "", "{\"Name\":\"SetVolume\"}")
	request.Header.Set("Content-Type", "text/plain")
	if _, err := parseRemoteGeneralCommand(request, false); !errors.Is(err, errRemoteCommandMedia) {
		t.Error("full GeneralCommand accepted a non-JSON content type")
	}
	for _, named := range []bool{false, true} {
		if _, err := parseRemoteGeneralCommand(remoteCommandTestRequest("VolumeUp", "", strings.Repeat("x", maxRemoteCommandBytes+1)), named); !errors.Is(err, errRemoteCommandLimit) {
			t.Error("remote command accepted an oversized body")
		}
	}
	large := map[string]any{"Name": "Large", "Arguments": map[string]string{"Value": strings.Repeat("<", maxRemoteCommandBytes/2)}}
	if _, err := remoteCommandEnvelope("GeneralCommand", large, identity.Principal{}, identity.ClientSession{}, false); !errors.Is(err, errRemoteCommandLimit) {
		t.Error("JSON escaping allowed the encoded command to exceed its byte limit")
	}
}

func TestRemotePlaystateValidatesThePinnedEnumsAndMatchingSeekInputs(t *testing.T) {
	for _, command := range []string{"Stop", "Pause", "Unpause", "NextTrack", "PreviousTrack", "Seek", "Rewind", "FastForward", "PlayPause", "SeekRelative"} {
		body := "{}"
		if command == "Seek" || command == "SeekRelative" {
			body = "{\"SeekPositionTicks\":0}"
		}
		data, err := parseRemotePlaystate(remoteCommandTestRequest(command, "", body), nil)
		if err != nil || data["Command"] != command {
			t.Errorf("pinned Playstate command %s was rejected: %v", command, err)
		}
	}
	request := remoteCommandTestRequest("Seek", "", "{\"Command\":\"Seek\",\"SeekPositionTicks\":9223372036854775807,\"ControllingUserId\":\"forged\"}")
	data, err := parseRemotePlaystate(request, map[string]string{"command": "Seek", "seekpositionticks": "9223372036854775807"})
	if err != nil || data["SeekPositionTicks"] != int64(9223372036854775807) || data["ControllingUserId"] != nil {
		t.Error("Playstate failed to preserve an exact int64 seek or ignored controller hint")
	}
	for _, test := range []struct {
		command, body string
		query         map[string]string
	}{
		{"Unknown", "{}", nil},
		{"Pause", "{\"Command\":\"Unpause\"}", nil},
		{"Pause", "{}", map[string]string{"command": "Stop"}},
		{"Seek", "{}", nil},
		{"SeekRelative", "{}", nil},
		{"Seek", "{\"SeekPositionTicks\":-1}", nil},
		{"Seek", "{\"SeekPositionTicks\":1.5}", nil},
		{"Seek", "{\"SeekPositionTicks\":\"1\"}", nil},
		{"Seek", "{\"SeekPositionTicks\":null}", nil},
		{"Seek", "{\"SeekPositionTicks\":9223372036854775808}", nil},
		{"Seek", "{\"SeekPositionTicks\":1}", map[string]string{"seekpositionticks": "2"}},
	} {
		if _, err := parseRemotePlaystate(remoteCommandTestRequest(test.command, "", test.body), test.query); !errors.Is(err, errRemoteCommandInput) {
			t.Errorf("invalid Playstate request error = %v, want invalid input", err)
		}
	}
}

func TestRemotePlayRequestSupportsOpaqueMultipleItemsAndExactStarts(t *testing.T) {
	request := remoteCommandTestRequest("", "?ItemIds=item-one&ItemIds=item-two&PlayCommand=PlayNow&StartPositionTicks=9223372036854775807",
		"{\"AudioStreamIndex\":5,\"SubtitleStreamIndex\":-1,\"StartIndex\":1,\"ControllingUserId\":\"forged\"}")
	data, err := parseRemotePlay(request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data["ItemIds"], []string{"item-one", "item-two"}) || data["PlayCommand"] != "PlayNow" ||
		data["StartPositionTicks"] != int64(9223372036854775807) || data["SubtitleStreamIndex"] != int64(-1) ||
		data["StartIndex"] != int64(1) || data["ControllingUserId"] != nil {
		t.Error("PlayRequest lost opaque item IDs, exact positions, or ignored controller hints")
	}
	for _, test := range []struct{ query, body string }{
		{"?PlayCommand=PlayNow", "{}"},
		{"?ItemIds=one&PlayCommand=Unknown", "{}"},
		{"?ItemIds=one&PlayCommand=PlayNow", "{\"ItemIds\":[\"two\"]}"},
		{"?ItemIds=one&PlayCommand=PlayNow", "{\"PlayCommand\":\"PlayNext\"}"},
		{"?ItemIds=one&itemids=two&PlayCommand=PlayNow", "{}"},
		{"?ItemIds=one&PlayCommand=PlayNow&StartPositionTicks=1", "{\"StartPositionTicks\":2}"},
		{"?ItemIds=one&PlayCommand=PlayNow&StartPositionTicks=-1", "{}"},
		{"?ItemIds=one&PlayCommand=PlayNow", "{\"StartPositionTicks\":-1}"},
		{"?ItemIds=one&PlayCommand=PlayNow", "{\"AudioStreamIndex\":2147483648}"},
		{"?ItemIds=one&PlayCommand=PlayNow", "{\"SubtitleStreamIndex\":-2}"},
		{"?ItemIds=one&PlayCommand=PlayNow", "{\"StartIndex\":1}"},
		{"?ItemIds=one&PlayCommand=PlayNow", "{\"MediaSourceId\":\"unrelated-source\"}"},
	} {
		if _, err := parseRemotePlay(remoteCommandTestRequest("", test.query, test.body)); !errors.Is(err, errRemoteCommandInput) {
			t.Errorf("invalid PlayRequest error = %v, want invalid input", err)
		}
	}
	ids := make([]string, maxRemotePlayItems+1)
	for index := range ids {
		ids[index] = "item"
	}
	encoded, err := json.Marshal(map[string]any{"ItemIds": ids, "PlayCommand": "PlayNow"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseRemotePlay(remoteCommandTestRequest("", "", string(encoded))); !errors.Is(err, errRemoteCommandInput) {
		t.Error("PlayRequest accepted an unbounded item list")
	}
}

func TestRemoteSeekRelativeForwardsSignedInt64WithoutChangingAbsolutePositions(t *testing.T) {
	for _, position := range []int64{-9223372036854775808, -10000000, -1, 0, 9223372036854775807} {
		raw := strconv.FormatInt(position, 10)
		for _, source := range []string{"body", "query", "both"} {
			t.Run(source+"/"+raw, func(t *testing.T) {
				body := "{}"
				query := map[string]string{}
				if source != "query" {
					body = "{\"Command\":\"SeekRelative\",\"SeekPositionTicks\":" + raw + "}"
				}
				if source != "body" {
					query["seekpositionticks"] = raw
				}
				data, err := parseRemotePlaystate(remoteCommandTestRequest("SeekRelative", "", body), query)
				if err != nil || data["Command"] != "SeekRelative" || data["SeekPositionTicks"] != position {
					t.Errorf("signed relative seek was not preserved exactly: %v", err)
				}
			})
		}
	}
	for _, test := range []struct {
		name, command, body string
		query               map[string]string
	}{
		{"body-underflow", "SeekRelative", "{\"SeekPositionTicks\":-9223372036854775809}", nil},
		{"body-overflow", "SeekRelative", "{\"SeekPositionTicks\":9223372036854775808}", nil},
		{"query-underflow", "SeekRelative", "{}", map[string]string{"seekpositionticks": "-9223372036854775809"}},
		{"query-overflow", "SeekRelative", "{}", map[string]string{"seekpositionticks": "9223372036854775808"}},
		{"negative-conflict", "SeekRelative", "{\"SeekPositionTicks\":-1}", map[string]string{"seekpositionticks": "-2"}},
		{"opposite-sign-conflict", "SeekRelative", "{\"SeekPositionTicks\":-1}", map[string]string{"seekpositionticks": "1"}},
		{"zero-conflict", "SeekRelative", "{\"SeekPositionTicks\":0}", map[string]string{"seekpositionticks": "-1"}},
		{"absolute-query-negative", "Seek", "{}", map[string]string{"seekpositionticks": "-1"}},
		{"absolute-body-negative", "Seek", "{\"SeekPositionTicks\":-1}", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseRemotePlaystate(remoteCommandTestRequest(test.command, "", test.body), test.query); !errors.Is(err, errRemoteCommandInput) {
				t.Errorf("invalid or conflicting seek error = %v, want invalid input", err)
			}
		})
	}
}
