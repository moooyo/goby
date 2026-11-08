package transcode

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCodecConfigurationsDecodeRejectsAmbiguousPolicies(t *testing.T) {
	for name, data := range map[string]string{
		"empty":        `[]`,
		"missing item": `[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0}]`,
		"extra item":   `[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0},{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0}]`,
		"duplicate id": `[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0},{"CodecId":"V-E-libx264","IsEnabled":false,"Priority":1}]`,
		"unknown id":   `[{"CodecId":"h264","IsEnabled":true,"Priority":0},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]`,
		"missing key":  `[{"CodecId":"V-E-libx264","IsEnabled":true},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]`,
		"unknown key":  `[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0,"Encoder":"libx264"},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]`,
		"duplicate key": `[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0,"Priority":1},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]`,
		"null key":      `[{"CodecId":"V-E-libx264","IsEnabled":null,"Priority":0},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]`,
		"quoted flag":   `[{"CodecId":"V-E-libx264","IsEnabled":"true","Priority":0},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]`,
		"fraction":      `[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0.5},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]`,
		"excess priority": `[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":1001},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}]`,
		"trailing input":  `[{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":0},{"CodecId":"V-E-libx265","IsEnabled":true,"Priority":0}] {}`,
	} {
		t.Run(name, func(t *testing.T) {
			value := DefaultCodecConfigurations()
			before := value
			if err := json.Unmarshal([]byte(data), &value); err == nil {
				t.Fatal("ambiguous codec policy was accepted")
			}
			if value != before {
				t.Fatal("rejected codec policy changed the previous value")
			}
		})
	}
}

func TestCodecConfigurationsCanonicalizeIdentityWithoutChangingPriority(t *testing.T) {
	var value CodecConfigurations
	if err := json.Unmarshal([]byte(`[{"CodecId":"V-E-libx265","IsEnabled":false,"Priority":50},{"CodecId":"V-E-libx264","IsEnabled":true,"Priority":-10}]`), &value); err != nil {
		t.Fatal(err)
	}
	want := CodecConfigurations{{CodecId: H264SoftwareCodecID, IsEnabled: true, Priority: -10}, {CodecId: HEVCSoftwareCodecID, Priority: 50}}
	if value != want {
		t.Fatalf("encoder identity, enablement, or priority changed: got %+v, want %+v", value, want)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip CodecConfigurations
	if err := json.Unmarshal(data, &roundTrip); err != nil || roundTrip != value {
		t.Fatalf("codec policy did not round trip: %+v, %v", roundTrip, err)
	}
	if _, err := ResolveCodecConfigurations(CodecConfigurations{}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCodecConfigurations(CodecConfigurations{}); !errors.Is(err, ErrInvalidCodecConfigurations) {
		t.Fatalf("an absent managed group became a complete stored policy: %v", err)
	}
}

func TestCodecConfigurationsGateActualEncoderAndFallback(t *testing.T) {
	configurations := DefaultCodecConfigurations()
	configurations[0].IsEnabled = false
	for _, test := range []struct {
		name, codec, backend string
		allowed              bool
	}{
		{"disabled x264", "h264", "software", false},
		{"default x264 backend", "h264", "", false},
		{"different hardware encoder", "h264", "vaapi", true},
		{"unconfigured AV1 encoder", "av1", "software", true},
		{"enabled x265", "hevc", "software", true},
		{"copied video", "copy", "software", true},
		{"audio only", "", "software", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan := Plan{VideoCodec: test.codec, Hardware: Hardware{Encode: test.backend}}
			if allowed := ConfiguredVideoEncodingAllowed(configurations, plan); allowed != test.allowed {
				t.Fatalf("wrong admission for actual encoder: got %v, want %v", allowed, test.allowed)
			}
		})
	}
	plan := Plan{VideoCodec: "h264", Hardware: Hardware{Encode: "vaapi"}}
	if !ConfiguredVideoEncodingAllowed(configurations, plan) {
		t.Fatal("software configuration disabled hardware encoding")
	}
	plan.Hardware.Encode = "software"
	if ConfiguredVideoEncodingAllowed(configurations, plan) {
		t.Fatal("hardware fallback bypassed disabled software encoding")
	}
}
