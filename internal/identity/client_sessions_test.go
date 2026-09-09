package identity_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestParseClientCapabilitiesPreservesSupportedShapeWithoutSecrets(t *testing.T) {
	input := []byte(`{
		"PlayableMediaTypes":["Video","Audio"],"SupportedCommands":["Play","Pause"],
		"SupportsMediaControl":true,"SupportsSync":true,"AppId":"example.player",
		"IconUrl":"https://example.test/icon.png","PushToken":"push-secret-sentinel",
		"PushTokenType":"PushProvider","UserId":"foreign-user","SessionId":"foreign-session",
		"FutureExtension":{"Private":"discarded-extension-sentinel"},
		"DeviceProfile":{
			"Name":"Example Player","Id":"profile-id","SupportedMediaTypes":"Video,Audio",
			"MaxStreamingBitrate":9223372036854775807,"MusicStreamingTranscodingBitrate":192000,
			"MaxStaticMusicBitrate":320000,"DeclaredFeatures":["example-feature"],
			"DirectPlayProfiles":[{"Container":"mp4","AudioCodec":"aac","VideoCodec":"h264","Type":"Video","Future":true}],
			"TranscodingProfiles":[{"Container":"ts","Type":"Video","VideoCodec":"h264","AudioCodec":"aac",
				"Protocol":"hls","EstimateContentLength":false,"EnableMpegtsM2TsMode":true,
				"TranscodeSeekInfo":"Auto","CopyTimestamps":true,"Context":"Streaming",
				"MaxAudioChannels":"6","MinSegments":1,"SegmentLength":6,"BreakOnNonKeyFrames":false,
				"AllowInterlacedVideoStreamCopy":true,"ManifestSubtitles":"vtt","MaxManifestSubtitles":5,
				"MaxWidth":1920,"MaxHeight":1080,"FillEmptySubtitleSegments":true}],
			"ContainerProfiles":[{"Type":"Video","Container":"mp4","Conditions":[{"Condition":"Equals","Property":"VideoCodecTag","Value":"avc1","IsRequired":true}]}],
			"CodecProfiles":[{"Type":"Video","Codec":"h264","Container":"mp4","Conditions":[{"Condition":"LessThanEqual","Property":"Width","Value":"1920"}],"ApplyConditions":[]}],
			"ResponseProfiles":[{"Container":"mp4","AudioCodec":"aac","VideoCodec":"h264","Type":"Video","OrgPn":"AVC_MP4","MimeType":"video/mp4","Conditions":[]}],
			"SubtitleProfiles":[{"Format":"vtt","Method":"External","DidlMode":"CaptionInfoEx","Language":"eng","Container":"mp4","AllowChunkedResponse":true,"Protocol":"http"}],
			"NewProfileField":"discarded-profile-sentinel"
		}
	}`)
	capabilities, err := identity.ParseClientCapabilities(input)
	if err != nil {
		t.Fatalf("parse supported capability shape: %v", err)
	}
	if !reflect.DeepEqual(capabilities.PlayableMediaTypes, []string{"Video", "Audio"}) ||
		!reflect.DeepEqual(capabilities.SupportedCommands, []string{"Play", "Pause"}) ||
		!capabilities.SupportsMediaControl || !capabilities.SupportsSync || capabilities.AppID != "example.player" ||
		capabilities.IconURL != "https://example.test/icon.png" {
		t.Fatalf("supported capability fields were lost: %+v", capabilities)
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(capabilities.DeviceProfile, &profile); err != nil {
		t.Fatalf("decode canonical profile: %v", err)
	}
	if len(profile) != 13 || string(profile["MaxStreamingBitrate"]) != "9223372036854775807" {
		t.Errorf("profile fields or int64 precision changed: %s", capabilities.DeviceProfile)
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"push-secret-sentinel", "PushToken", "PushProvider", "foreign-user", "foreign-session", "discarded-", "Future"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Errorf("capabilities retained unsupported or secret data %q", forbidden)
		}
	}
	if _, err := identity.ParseClientCapabilities(encoded); err != nil {
		t.Errorf("canonical capabilities are not valid input: %v", err)
	}
}

func TestParseClientCapabilitiesRejectsInvalidAndUnboundedInput(t *testing.T) {
	tooMany := strings.Repeat(`"Video",`, identity.MaxClientCapabilityEntries) + `"Video"`
	tooManyFields := make([]string, 65)
	for i := range tooManyFields {
		tooManyFields[i] = fmt.Sprintf(`"Extension%d":true`, i)
	}
	wideArray := `[` + strings.Repeat(`true,`, 127) + `true]`
	tooManyNodes := `{"Future":[` + strings.Repeat(wideArray+`,`, 32) + wideArray + `]}`
	tests := map[string][]byte{
		"empty":                   nil,
		"null document":           []byte(`null`),
		"array document":          []byte(`[]`),
		"trailing document":       []byte(`{} {}`),
		"trailing junk":           []byte(`{} !`),
		"malformed":               []byte(`{"DeviceProfile":`),
		"wrong string array":      []byte(`{"PlayableMediaTypes":"Video"}`),
		"wrong array entry":       []byte(`{"SupportedCommands":[1]}`),
		"null array entry":        []byte(`{"SupportedCommands":[null]}`),
		"wrong boolean":           []byte(`{"SupportsMediaControl":"true"}`),
		"wrong push token":        []byte(`{"PushToken":123}`),
		"wrong profile":           []byte(`{"DeviceProfile":[]}`),
		"wrong nested profile":    []byte(`{"DeviceProfile":{"DirectPlayProfiles":["Video"]}}`),
		"wrong nested boolean":    []byte(`{"DeviceProfile":{"SubtitleProfiles":[{"AllowChunkedResponse":"true"}]}}`),
		"unknown enum":            []byte(`{"DeviceProfile":{"DirectPlayProfiles":[{"Type":"video"}]}}`),
		"invalid condition":       []byte(`{"DeviceProfile":{"CodecProfiles":[{"Conditions":[{"Condition":"FutureComparison"}]}]}}`),
		"fractional integer":      []byte(`{"DeviceProfile":{"MaxStreamingBitrate":1.5}}`),
		"int64 overflow":          []byte(`{"DeviceProfile":{"MaxStreamingBitrate":9223372036854775808}}`),
		"int32 overflow":          []byte(`{"DeviceProfile":{"MusicStreamingTranscodingBitrate":2147483648}}`),
		"negative numeric limit":  []byte(`{"DeviceProfile":{"MaxStaticMusicBitrate":-1}}`),
		"duplicate known key":     []byte(`{"SupportsSync":false,"SupportsSync":true}`),
		"duplicate unknown key":   []byte(`{"Future":1,"Future":2}`),
		"control text":            []byte(`{"AppId":"app\u0000id"}`),
		"control extension":       []byte(`{"Future":"line\nbreak"}`),
		"long text":               []byte(`{"AppId":"` + strings.Repeat("x", identity.MaxClientCapabilityText+1) + `"}`),
		"long object key":         []byte(`{"` + strings.Repeat("x", 257) + `":true}`),
		"too many entries":        []byte(`{"PlayableMediaTypes":[` + tooMany + `]}`),
		"too many unknown fields": []byte(`{` + strings.Join(tooManyFields, ",") + `}`),
		"too deep unknown data":   []byte(`{"Future":` + strings.Repeat(`[`, 9) + `true` + strings.Repeat(`]`, 9) + `}`),
		"too many JSON values":    []byte(tooManyNodes),
		"too many bytes":          []byte(`{` + strings.Repeat(" ", identity.MaxClientCapabilitiesBytes) + `}`),
		"invalid UTF-8":           append([]byte(`{"AppId":"`), 0xff, '"', '}'),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := identity.ParseClientCapabilities(input); !errors.Is(err, identity.ErrInvalidInput) {
				t.Errorf("parse malformed capability input: got %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestParseClientCapabilitiesBoundariesAndOptionalNulls(t *testing.T) {
	entries := make([]string, identity.MaxClientCapabilityEntries)
	for i := range entries {
		entries[i] = "Video"
	}
	input, err := json.Marshal(map[string]any{
		"PlayableMediaTypes": entries,
		"AppId":              strings.Repeat("x", identity.MaxClientCapabilityText),
		"SupportedCommands":  nil, "DeviceProfile": nil, "SupportsSync": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := identity.ParseClientCapabilities(input)
	if err != nil {
		t.Fatalf("parse exact entry and text boundaries: %v", err)
	}
	if len(capabilities.PlayableMediaTypes) != identity.MaxClientCapabilityEntries ||
		len(capabilities.AppID) != identity.MaxClientCapabilityText || capabilities.DeviceProfile != nil ||
		capabilities.SupportedCommands != nil || capabilities.SupportsSync {
		t.Error("boundary values or optional null normalization changed")
	}
	padding := []byte("{" + strings.Repeat(" ", identity.MaxClientCapabilitiesBytes-2) + "}")
	if _, err := identity.ParseClientCapabilities(padding); err != nil {
		t.Errorf("parse exact document byte boundary: %v", err)
	}
	if _, err := identity.ParseClientCapabilities([]byte(`{"DeviceProfile":{"MaxStreamingBitrate":0,"MusicStreamingTranscodingBitrate":2147483647}}`)); err != nil {
		t.Errorf("parse numeric schema boundaries: %v", err)
	}
}
