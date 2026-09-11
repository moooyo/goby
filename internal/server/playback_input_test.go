package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

func originalWebPlaybackProfile(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/emby-web-4.9.5.0-playback-profile.json")
	if err != nil {
		t.Fatal("read the public original Web Client profile capture")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != "9974969118c36e5eb3643c59483378dc6380aed7223c70b70f5eb09eb233d484" {
		t.Fatal("the original client profile fixture differs from its captured bytes")
	}
	return data
}

func TestPlaybackInfoDecodesOriginalWebProfileWithoutChangingNativeJSON(t *testing.T) {
	body := append(append([]byte(`{"DeviceProfile":`), originalWebPlaybackProfile(t)...), '}')
	request, err := parsePlaybackInfoBody(body)
	if err != nil || request.DeviceProfile == nil {
		t.Fatal("the complete observed original Web Client profile did not decode")
	}
	profile := request.DeviceProfile
	if profile.MaxStaticBitrate == nil || *profile.MaxStaticBitrate != 200_000_000 ||
		profile.MaxStreamingBitrate == nil || *profile.MaxStreamingBitrate != 200_000_000 {
		t.Fatal("the captured static or streaming bitrate was dropped")
	}
	segments, required := 0, 0
	for _, transcode := range profile.TranscodingProfiles {
		if transcode.MinSegments != nil {
			segments++
			if *transcode.MinSegments != 1 {
				t.Fatal("the observed segment count changed during wire decoding")
			}
		}
	}
	for _, codec := range profile.CodecProfiles {
		for _, condition := range codec.Conditions {
			if condition.IsRequired != nil {
				required++
				if *condition.IsRequired {
					t.Fatal("the observed optional condition became required")
				}
			}
		}
	}
	if segments != 2 || required != 6 || len(profile.ResponseProfiles) != 1 || profile.ResponseProfiles[0].MimeType != "video/mp4" {
		t.Fatal("the complete profile lost observed segment, condition, or MIME declarations")
	}
	var rejected playback.Request
	if json.Unmarshal(body, &rejected) == nil {
		t.Fatal("the compatibility adapter changed the native typed JSON contract")
	}
	canonical, err := json.Marshal(request)
	var native playback.Request
	if err != nil || json.Unmarshal(canonical, &native) != nil {
		t.Fatal("the wire adapter failed to produce the ordinary typed representation")
	}
	// omitempty removes explicit empty slices. Decode into a fresh value so
	// the earlier rejected request cannot retain omitted profile collections.
	nativeJSON, err := json.Marshal(native)
	if err != nil || !bytes.Equal(nativeJSON, canonical) {
		t.Fatal("native decoding changed a represented profile field or value")
	}
	decoded, err := parsePlaybackInfoBody(canonical)
	if err != nil || !reflect.DeepEqual(decoded, native) {
		t.Fatal("already typed numeric and boolean JSON changed meaning")
	}
}

func TestOriginalWebProfileReachesPlannerAndEnforcesStaticBitrate(t *testing.T) {
	body := append(append([]byte(`{"DeviceProfile":`), originalWebPlaybackProfile(t)...), '}')
	request, err := parsePlaybackInfoBody(body)
	if err != nil {
		t.Fatal(err)
	}
	source := playback.Source{ItemID: "original-web-movie", Path: "/fixture/movie.mp4", ItemType: "Movie", Info: media.Info{
		Container: "mov,mp4,m4a,3gp,3g2,mj2", DurationTicks: 600 * media.TicksPerSecond, Bitrate: 4_000_000,
		Streams: []media.Stream{
			{Index: 0, CodecType: "video", Codec: "h264", Profile: "High", Level: 41, Width: 1920, Height: 1080, Bitrate: 3_500_000},
			{Index: 1, CodecType: "audio", Codec: "aac", Channels: 2, SampleRate: 48000, Bitrate: 192_000, IsDefault: true},
		},
	}}
	decision, err := playback.Evaluate(source, request)
	if err != nil || !decision.DirectPlay || !decision.DirectStream || !decision.ProfileMatched {
		t.Fatalf("the real client profile did not permit matching H.264/AAC facts: %+v, %v", decision, err)
	}
	limit := int64(3_999_999)
	request.DeviceProfile.MaxStaticBitrate = &limit
	decision, err = playback.Evaluate(source, request)
	if err != nil || decision.DirectPlay || decision.DirectStream || decision.ProfileMatched {
		t.Fatal("the parsed static bitrate did not constrain original-file delivery")
	}
	matched := false
	for _, reason := range decision.Reasons {
		matched = matched || reason.Code == "device_bitrate_limit" && reason.Property == "DeviceProfile.MaxStaticBitrate"
	}
	if !matched {
		t.Fatal("the decision did not identify the actual limiting profile field")
	}
}

func TestPlaybackInfoStringExceptionsRemainNarrowAndCanonical(t *testing.T) {
	for _, value := range []string{"0", "1", "2147483647"} {
		body := `{"DeviceProfile":{"TranscodingProfiles":[{"Type":"Audio","MinSegments":"` + value + `"}]}}`
		if _, err := parsePlaybackInfoBody([]byte(body)); err != nil {
			t.Fatal("a canonical segment-count representation was rejected")
		}
	}
	for _, value := range []string{"", "-1", "+1", "01", " 1", "1 ", "1.0", "1e0", "2147483648"} {
		body := `{"DeviceProfile":{"TranscodingProfiles":[{"Type":"Audio","MinSegments":"` + value + `"}]}}`
		if _, err := parsePlaybackInfoBody([]byte(body)); err == nil {
			t.Fatal("a noncanonical or out-of-range segment count was accepted")
		}
	}
	for _, location := range []string{"ContainerProfiles", "CodecProfiles", "ResponseProfiles"} {
		for _, value := range []string{"true", "false"} {
			body := `{"DeviceProfile":{"` + location + `":[{"Conditions":[{"IsRequired":"` + value + `"}]}]}}`
			if _, err := parsePlaybackInfoBody([]byte(body)); err != nil {
				t.Fatal("a canonical condition boolean was rejected")
			}
		}
	}
	for _, body := range []string{
		`{"DeviceProfile":{"CodecProfiles":[{"ApplyConditions":[{"IsRequired":"false"}]}]}}`,
		`{"DeviceProfile":{"TranscodingProfiles":[{"MinSegments":1}],"CodecProfiles":[{"Conditions":[{"IsRequired":false}]}]}}`,
	} {
		if _, err := parsePlaybackInfoBody([]byte(body)); err != nil {
			t.Fatal("a supported condition location or original typed value was rejected")
		}
	}
	for _, body := range []string{
		`{"EnableTranscoding":"false"}`, `{"MaxStreamingBitrate":"200000000"}`,
		`{"DeviceProfile":{"MaxStaticBitrate":"200000000"}}`,
		`{"DeviceProfile":{"TranscodingProfiles":[{"SegmentLength":"1"}]}}`,
		`{"DeviceProfile":{"TranscodingProfiles":[{"BreakOnNonKeyFrames":"false"}]}}`,
		`{"DeviceProfile":{"CodecProfiles":[{"Conditions":[{"IsRequired":"False"}]}]}}`,
		`{"DeviceProfile":{"CodecProfiles":[{"Conditions":[{"IsRequired":"0"}]}]}}`,
		`{"DeviceProfile":{"CodecProfiles":[{"Conditions":[{"IsRequired":" false"}]}]}}`,
	} {
		if _, err := parsePlaybackInfoBody([]byte(body)); err == nil {
			t.Fatal("a string-coercion exception escaped its exact field or lexical range")
		}
	}
}

func TestPlaybackInfoRejectsDuplicateMalformedAndUnboundedInput(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`,
		`{"DeviceProfile":{},"deviceprofile":{}}`,
		`{"DeviceProfile":{"MaxStaticBitrate":1,"MaxStaticBitrate":2}}`,
		`{"DeviceProfile":{"MaxStaticBitrate":1,"Max\u017ftaticBitrate":200000000}}`,
		`{"DeviceProfile":{"Max\u017ftaticBitrate":200000000}}`,
		`{"DeviceProfile":{"CodecProfiles":[{"Conditions":[{"IsRequired":false,"isrequired":"true"}]}]}}`,
		`{"DeviceProfile":{"Name":"\ud800"}}`, `{"DeviceProfile":{"Name":"bad\u0000text"}}`,
		string([]byte{'{', '"', 'I', 'd', '"', ':', '"', 0xff, '"', '}'}),
		`{} {}`, `{} ` + strings.Repeat("[", 32) + `0` + strings.Repeat("]", 32),
		`{"DeviceProfile":{"Name":"` + strings.Repeat("x", maxPlaybackInfoJSONText+1) + `"}}`,
		`{"DeviceProfile":` + strings.Repeat("[", maxPlaybackInfoJSONDepth+1) + `0` + strings.Repeat("]", maxPlaybackInfoJSONDepth+1) + `}`,
		`{"DeviceProfile":{"TranscodingProfiles":[` + strings.Repeat(`{},`, 1024) + `{}]}}`,
		`{}` + strings.Repeat(" ", maxPlaybackInfoBodyBytes),
	} {
		if _, err := parsePlaybackInfoBody([]byte(body)); err == nil {
			t.Fatal("unsupported, ambiguous, malformed, or oversized playback JSON was accepted")
		}
	}
}

func TestPlaybackInfoIgnoresBoundedExtensionsWithoutUsingTheirClaims(t *testing.T) {
	baseline, err := parsePlaybackInfoBody([]byte(`{"UserId":"viewer","DeviceProfile":{"MaxStaticBitrate":200000000,"DirectPlayProfiles":[{"Type":"Video","Container":"mp4"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	extended, err := parsePlaybackInfoBody([]byte(`{"UserId":"viewer","ClientExtensionMetadata":{"UserId":"other-user","AudioStreamIndex":99,"label":"\u96ea","values":[true,null,3]},
		"DeviceProfile":{"MaxStaticBitrate":200000000,"ClientExtensionMetadata":{"IsRequired":"not-a-boolean"},
		"DirectPlayProfiles":[{"Type":"Video","Container":"mp4","ClientExtensionMetadata":{"MinSegments":"not-an-integer"}}]}}`))
	if err != nil || !reflect.DeepEqual(extended, baseline) {
		t.Fatal("unmodeled extension metadata changed a modeled identity, selection, or profile field")
	}
	for _, body := range []string{
		`{"ClientExtensionMetadata":{"key":1,"KEY":2}}`,
		`{"ClientExtensionMetadata":{"label":"\ud800"}}`,
		`{"ClientExtensionMetadata":{"label":"` + strings.Repeat("x", maxPlaybackInfoJSONText+1) + `"}}`,
		`{"ClientExtensionMetadata":` + strings.Repeat("[", maxPlaybackInfoJSONDepth+1) + `0` + strings.Repeat("]", maxPlaybackInfoJSONDepth+1) + `}`,
		`{"ClientExtensionMetadata":[` + strings.Repeat(`[`+strings.Repeat(`0,`, 15)+`0],`, 1023) + `[` + strings.Repeat(`0,`, 15) + `0]]}`,
	} {
		if _, err := parsePlaybackInfoBody([]byte(body)); err == nil {
			t.Fatal("ignored extension data bypassed whole-document duplicate, encoding, or resource bounds")
		}
	}
}

func TestPlaybackInfoRetainsRecordedVideoIndexZeroExtensionCompatibility(t *testing.T) {
	data, err := os.ReadFile("testdata/emby-4.9.5.0-playback-video-index-zero.json")
	if err != nil {
		t.Fatal("read the retained M4e reference capture")
	}
	var capture struct {
		Request  struct{ Body json.RawMessage }
		Response struct {
			Status int
			Body   struct {
				MediaSources []struct {
					ID, Path, Container   string
					RunTimeTicks, Bitrate int64
					MediaStreams          []struct {
						Index, Channels, SampleRate, Width, Height, Level int
						Codec, Type, Profile                              string
						Bitrate                                           int64
						IsDefault                                         bool
					}
				}
			}
		}
	}
	if json.Unmarshal(data, &capture) != nil || capture.Response.Status != http.StatusOK || len(capture.Response.Body.MediaSources) != 1 {
		t.Fatal("the M4e fixture no longer contains its successful playback response")
	}
	var fields map[string]json.RawMessage
	var index int
	if json.Unmarshal(capture.Request.Body, &fields) != nil || fields["VideoStreamIndex"] == nil ||
		json.Unmarshal(fields["VideoStreamIndex"], &index) != nil || index != 0 {
		t.Fatal("the recorded VideoStreamIndex zero request is missing")
	}
	recorded, err := parsePlaybackInfoBody(capture.Request.Body)
	if err != nil {
		t.Fatal("the successful reference request was rejected for an unmodeled extension")
	}
	delete(fields, "VideoStreamIndex")
	baselineJSON, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := parsePlaybackInfoBody(baselineJSON)
	if err != nil || !reflect.DeepEqual(recorded, baseline) {
		t.Fatal("the legacy ignored extension changed the typed playback request")
	}
	dto := capture.Response.Body.MediaSources[0]
	source := playback.Source{ItemID: "43", MediaSourceID: dto.ID, Path: dto.Path, ItemType: "Movie",
		Info: media.Info{Container: dto.Container, DurationTicks: dto.RunTimeTicks, Bitrate: dto.Bitrate}}
	for _, stream := range dto.MediaStreams {
		source.Info.Streams = append(source.Info.Streams, media.Stream{Index: stream.Index, CodecType: strings.ToLower(stream.Type),
			Codec: stream.Codec, Profile: stream.Profile, Bitrate: stream.Bitrate, Level: stream.Level, Channels: stream.Channels,
			SampleRate: stream.SampleRate, Width: stream.Width, Height: stream.Height, IsDefault: stream.IsDefault})
	}
	withExtension, err := playback.Evaluate(source, recorded)
	if err != nil || !withExtension.DirectPlay || !withExtension.DirectStream {
		t.Fatal("the retained reference request lost its original-file playback decision")
	}
	withoutExtension, err := playback.Evaluate(source, baseline)
	if err != nil || !reflect.DeepEqual(withExtension, withoutExtension) {
		t.Fatal("an ignored VideoStreamIndex zero extension changed planning")
	}
	// This regression establishes only the historical ignored-zero behavior.
	// It does not implement video-track selection for nonzero index values.
}

func TestPlaybackInfoAdapterDoesNotBroadenOtherJSONDecoders(t *testing.T) {
	body := []byte(`{"MinSegments":"1","IsRequired":"false"}`)
	for _, decode := range []func(http.ResponseWriter, *http.Request, any) bool{decodeBody, decodeEmbyJSONBody} {
		request := httptest.NewRequest(http.MethodPost, "/native-fixture", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		var target struct {
			MinSegments int
			IsRequired  bool
		}
		if decode(response, request, &target) || response.Code != http.StatusBadRequest {
			t.Fatal("the PlaybackInfo exception broadened a general JSON decoder")
		}
	}
}

func TestPlaybackInfoWireRetainsExactInt64BitrateValues(t *testing.T) {
	for _, test := range []struct {
		text string
		want int64
	}{
		{"9007199254740993", 9_007_199_254_740_993},
		{"9223372036854775807", 9_223_372_036_854_775_807},
	} {
		request, err := parsePlaybackInfoBody([]byte(`{"DeviceProfile":{"MaxStaticBitrate":` + test.text + `}}`))
		if err != nil || request.DeviceProfile == nil || request.DeviceProfile.MaxStaticBitrate == nil || *request.DeviceProfile.MaxStaticBitrate != test.want {
			t.Fatal("the wire normalization passed an int64 bitrate through floating-point storage")
		}
	}
	if _, err := parsePlaybackInfoBody([]byte(`{"DeviceProfile":{"MaxStaticBitrate":9223372036854775808}}`)); err == nil {
		t.Fatal("an overflowing static bitrate was accepted")
	}
}
