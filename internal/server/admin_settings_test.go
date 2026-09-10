package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

const adminSettingsNullUpdateForTest = `{"Revision":"1","Overrides":{"ServerName":null,"MaxBitrate":null,"MaxWidth":null,"MaxHeight":null,"MaxAudioChannels":null}}`
const adminSettingsResetForTest = `{"Revision":"1","Fields":["ServerName"]}`

func adminSettingsRequestForTest(body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPut, "/admin/v1/settings", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r.WithContext(context.WithValue(r.Context(), requestIDKey, "settings-request-id"))
}

func adminSettingsUpdateBodyForTest(field, raw string) string {
	return strings.Replace(adminSettingsNullUpdateForTest, `"`+field+`":null`, `"`+field+`":`+raw, 1)
}

func assertAdminSettingsInputError(t *testing.T, response *httptest.ResponseRecorder, status int, field string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("settings validation status = %d, want %d", response.Code, status)
	}
	var body struct {
		Error struct {
			Code   string
			Fields map[string]string
		}
		RequestID string `json:"RequestId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode native settings error: %v", err)
	}
	wantCode := "invalid_input"
	if status == http.StatusUnsupportedMediaType {
		wantCode = "unsupported_media_type"
	}
	if body.Error.Code != wantCode || body.RequestID != "settings-request-id" {
		t.Fatal("native settings error lost its stable code or request ID")
	}
	if field != "" && body.Error.Fields[field] == "" {
		t.Fatalf("native settings error omitted the %s field", field)
	}
	for key := range body.Error.Fields {
		switch key {
		case "Body", "Query", "Revision", "Overrides", "Fields", "Overrides.ServerName", "Overrides.MaxBitrate", "Overrides.MaxWidth", "Overrides.MaxHeight", "Overrides.MaxAudioChannels":
		default:
			t.Fatal("native settings error used a request-controlled field name")
		}
	}
	if strings.Contains(response.Body.String(), "private-marker") {
		t.Fatal("native settings error reflected rejected request data")
	}
}

func adminSettingsDecodersForTest() []struct {
	name   string
	valid  string
	decode func(http.ResponseWriter, *http.Request) bool
} {
	return []struct {
		name   string
		valid  string
		decode func(http.ResponseWriter, *http.Request) bool
	}{
		{"update", adminSettingsNullUpdateForTest, func(w http.ResponseWriter, r *http.Request) bool {
			_, ok := decodeAdminSettingsUpdate(w, r)
			return ok
		}},
		{"reset", adminSettingsResetForTest, func(w http.ResponseWriter, r *http.Request) bool {
			_, ok := decodeAdminSettingsReset(w, r)
			return ok
		}},
	}
}

func TestSettingsDTOExactContractPreservesRevisionSourcesAndUTC(t *testing.T) {
	stamp := time.Date(2026, 9, 10, 12, 13, 14, 123456789, time.FixedZone("fixture", 8*60*60))
	defaults := settings.Values{ServerName: "Deployment Server", MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 8}
	deployment := config.TranscodingConfig{
		Enabled: true, CacheDirectory: "/private-marker/cache", Threads: 3, MaxJobs: 4, MaxUserJobs: 2, MaxSessionJobs: 1,
		MaxQueueJobs: 101, MaxRetainedJobs: 102, MaxCacheBytes: 103, MaxJobBytes: 104, MinFreeBytes: 105,
		MaxBitrate: 106, MaxWidth: 107, MaxHeight: 108, MaxAudioChannels: 109,
		Hardware: transcode.Hardware{Decode: "vaapi", Encode: "qsv", Device: "/private-marker/hardware"},
	}
	for _, revision := range []int64{1, 9007199254740993, math.MaxInt64} {
		for _, explicit := range []bool{false, true} {
			t.Run(strconv.FormatInt(revision, 10)+"/explicit="+strconv.FormatBool(explicit), func(t *testing.T) {
				snapshot := settings.Snapshot{Revision: revision, Defaults: defaults, Effective: defaults, UpdatedAt: stamp}
				overrides := map[string]any{"ServerName": nil, "MaxBitrate": nil, "MaxWidth": nil, "MaxHeight": nil, "MaxAudioChannels": nil}
				source := "deployment"
				if explicit {
					snapshot.Overrides = settings.Overrides{
						ServerName: &defaults.ServerName, MaxBitrate: &defaults.MaxBitrate,
						MaxWidth: &defaults.MaxWidth, MaxHeight: &defaults.MaxHeight, MaxAudioChannels: &defaults.MaxAudioChannels,
					}
					overrides = map[string]any{"ServerName": "Deployment Server", "MaxBitrate": float64(20_000_000), "MaxWidth": float64(1920), "MaxHeight": float64(1080), "MaxAudioChannels": float64(8)}
					source = "database"
				}
				encoded, err := json.Marshal(settingsDTO(snapshot, deployment))
				if err != nil {
					t.Fatal(err)
				}
				var got map[string]any
				if err := json.Unmarshal(encoded, &got); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{
					"Revision":   strconv.FormatInt(revision, 10),
					"Defaults":   map[string]any{"ServerName": "Deployment Server", "MaxBitrate": float64(20_000_000), "MaxWidth": float64(1920), "MaxHeight": float64(1080), "MaxAudioChannels": float64(8)},
					"Overrides":  overrides,
					"Effective":  map[string]any{"ServerName": "Deployment Server", "MaxBitrate": float64(20_000_000), "MaxWidth": float64(1920), "MaxHeight": float64(1080), "MaxAudioChannels": float64(8)},
					"Sources":    map[string]any{"ServerName": source, "MaxBitrate": source, "MaxWidth": source, "MaxHeight": source, "MaxAudioChannels": source},
					"UpdatedAt":  "2026-09-10T04:13:14.123456789Z",
					"Deployment": map[string]any{"TranscodingEnabled": true, "HardwareDecoder": "vaapi", "HardwareEncoder": "qsv", "Threads": float64(3), "MaxJobs": float64(4), "MaxUserJobs": float64(2), "MaxSessionJobs": float64(1)},
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("settings DTO changed its exact safe fields, value types, null overrides, explicit sources, revision precision, or UTC timestamp")
				}
				if strings.Contains(string(encoded), "private-marker") {
					t.Fatal("settings DTO exposed a deployment path or hardware device")
				}
				if snapshot.UpdatedAt.Location().String() != "fixture" {
					t.Fatal("settings DTO modified the snapshot timestamp")
				}
			})
		}
	}
}

func TestSettingsDTOCopiesAllOverrideValuesAndKeepsMixedSources(t *testing.T) {
	name, bitrate, width, height, channels := "Database Server", int64(12_000_000), 1280, 720, 2
	snapshot := settings.Snapshot{
		Defaults:  settings.Values{ServerName: "Deployment Server", MaxBitrate: 20_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 8},
		Overrides: settings.Overrides{ServerName: &name, MaxBitrate: &bitrate, MaxWidth: &width, MaxHeight: &height, MaxAudioChannels: &channels},
		Effective: settings.Values{ServerName: name, MaxBitrate: bitrate, MaxWidth: width, MaxHeight: height, MaxAudioChannels: channels},
	}
	got := settingsDTO(snapshot, config.TranscodingConfig{})
	want := map[string]any{"ServerName": name, "MaxBitrate": bitrate, "MaxWidth": width, "MaxHeight": height, "MaxAudioChannels": channels}
	if !reflect.DeepEqual(got["Overrides"], want) || !reflect.DeepEqual(got["Effective"], want) {
		t.Fatal("settings DTO did not dereference every override into an independent scalar")
	}
	name, bitrate, width, height, channels = "Changed Server", 1, 2, 3, 4
	if !reflect.DeepEqual(got["Overrides"], want) || !reflect.DeepEqual(got["Effective"], want) {
		t.Fatal("settings DTO retained mutable override pointers")
	}
	got["Defaults"].(map[string]any)["ServerName"] = "Response Mutation"
	if snapshot.Defaults.ServerName != "Deployment Server" {
		t.Fatal("settings DTO retained mutable default values")
	}
	snapshot.Overrides.MaxBitrate, snapshot.Overrides.MaxHeight = nil, nil
	mixed := settingsDTO(snapshot, config.TranscodingConfig{})
	wantSources := map[string]any{"ServerName": "database", "MaxBitrate": "deployment", "MaxWidth": "database", "MaxHeight": "deployment", "MaxAudioChannels": "database"}
	if !reflect.DeepEqual(mixed["Sources"], wantSources) {
		t.Fatal("settings DTO inferred a source from effective equality instead of each override pointer")
	}
}

func TestDecodeAdminSettingsPreservesCanonicalRevisionPrecision(t *testing.T) {
	for _, decoder := range adminSettingsDecodersForTest() {
		t.Run(decoder.name, func(t *testing.T) {
			for _, revision := range []string{"1", "9007199254740993", "9223372036854775806"} {
				body := strings.Replace(decoder.valid, `"Revision":"1"`, `"Revision":"`+revision+`"`, 1)
				response := httptest.NewRecorder()
				var got int64
				var ok bool
				if decoder.name == "update" {
					var request settings.UpdateRequest
					request, ok = decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(body, "application/json"))
					got = request.Revision
					if !reflect.DeepEqual(request.Overrides, settings.Overrides{}) {
						t.Fatal("null overrides did not remain nil")
					}
				} else {
					var request settings.ResetRequest
					request, ok = decodeAdminSettingsReset(response, adminSettingsRequestForTest(body, "application/json"))
					got = request.Revision
				}
				if !ok || strconv.FormatInt(got, 10) != revision || response.Body.Len() != 0 {
					t.Fatal("settings parser changed a valid decimal revision")
				}
			}
			for _, revision := range []string{`null`, `true`, `false`, `1`, `1.0`, `[]`, `{}`, `""`, `"0"`, `"-1"`, `"+1"`, `"01"`, `"1.0"`, `"1e3"`, `" 1"`, `"1 "`, `"1\n"`, `"\u0000"`, `"\uff11"`, `"9223372036854775807"`, `"9223372036854775808"`, `"999999999999999999999999999999999999"`, `"private-marker"`} {
				body := strings.Replace(decoder.valid, `"Revision":"1"`, `"Revision":`+revision, 1)
				response := httptest.NewRecorder()
				if decoder.decode(response, adminSettingsRequestForTest(body, "application/json")) {
					t.Fatal("settings parser accepted a noncanonical, nonpositive, or overflowing revision")
				}
				assertAdminSettingsInputError(t, response, http.StatusBadRequest, "Revision")
			}
		})
	}
}

func TestDecodeAdminSettingsUpdateCopiesNullableValuesAndIntegerSyntax(t *testing.T) {
	for _, field := range []string{"MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels"} {
		t.Run(field, func(t *testing.T) {
			for _, raw := range []string{"1", "8193", "1000000000", " \t9\r\n "} {
				response := httptest.NewRecorder()
				request, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(adminSettingsUpdateBodyForTest(field, raw), "application/json"))
				if !ok || response.Body.Len() != 0 {
					t.Fatal("settings parser rejected a safe integer before domain validation")
				}
				var got int64
				switch field {
				case "MaxBitrate":
					if request.Overrides.MaxBitrate == nil {
						t.Fatal("settings parser discarded a bitrate override")
					}
					got = *request.Overrides.MaxBitrate
				case "MaxWidth":
					if request.Overrides.MaxWidth == nil {
						t.Fatal("settings parser discarded a width override")
					}
					got = int64(*request.Overrides.MaxWidth)
				case "MaxHeight":
					if request.Overrides.MaxHeight == nil {
						t.Fatal("settings parser discarded a height override")
					}
					got = int64(*request.Overrides.MaxHeight)
				case "MaxAudioChannels":
					if request.Overrides.MaxAudioChannels == nil {
						t.Fatal("settings parser discarded an audio channel override")
					}
					got = int64(*request.Overrides.MaxAudioChannels)
				}
				if strconv.FormatInt(got, 10) != strings.TrimSpace(raw) {
					t.Fatal("settings parser changed an integer override")
				}
			}
			for _, raw := range []string{"0", "-0", "-1", "+1", "01", "1.0", "1e0", "1E0", "1e+3", "1000000001", "9223372036854775807", "999999999999999999999999999999", `"1"`, `"private-marker"`, "true", "false", "[]", "{}"} {
				response := httptest.NewRecorder()
				if _, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(adminSettingsUpdateBodyForTest(field, raw), "application/json")); ok {
					t.Fatal("settings parser accepted a noninteger or out-of-range numeric override")
				}
				assertAdminSettingsInputError(t, response, http.StatusBadRequest, "")
			}
		})
	}
	for _, raw := range []string{"true", "false", "1", "[]", "{}"} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(adminSettingsUpdateBodyForTest("ServerName", raw), "application/json")); ok {
			t.Fatal("settings parser accepted a nonstring server name")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, "Overrides.ServerName")
	}
}

func TestDecodeAdminSettingsPreservesServerNameForSharedDomainValidation(t *testing.T) {
	for _, name := range []string{
		"", " \t\r\n ", "  Living Room  ", "Cafe\u0301", "Name\x00With\nControls",
		strings.Repeat("\u00e9", 64), strings.Repeat("\u00e9", 64) + "x",
	} {
		raw, err := json.Marshal(name)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		request, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(adminSettingsUpdateBodyForTest("ServerName", string(raw)), "application/json"))
		if !ok || request.Overrides.ServerName == nil || *request.Overrides.ServerName != name || response.Body.Len() != 0 {
			t.Fatal("settings parser changed the server name or duplicated the shared domain policy")
		}
	}
}

func TestDecodeAdminSettingsUpdateRequiresExactCompleteObjects(t *testing.T) {
	for _, field := range []string{"ServerName", "MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels"} {
		missing := strings.Replace(adminSettingsNullUpdateForTest, `"`+field+`":null,`, "", 1)
		if missing == adminSettingsNullUpdateForTest {
			missing = strings.Replace(missing, `,"`+field+`":null`, "", 1)
		}
		for _, body := range []string{
			missing,
			strings.Replace(adminSettingsNullUpdateForTest, `"`+field+`":null`, `"`+strings.ToLower(field)+`":null`, 1),
			strings.Replace(adminSettingsNullUpdateForTest, `"`+field+`":null`, `"`+field+`":null,"`+field+`":null`, 1),
		} {
			response := httptest.NewRecorder()
			if _, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(body, "application/json")); ok {
				t.Fatal("settings update accepted an incomplete, mis-cased, or duplicate override field")
			}
			assertAdminSettingsInputError(t, response, http.StatusBadRequest, "")
		}
	}
	for _, body := range []string{
		`{"Revision":"1"}`, `{"Overrides":{}}`, `{"Revision":"1","Overrides":null}`, `{"Revision":"1","Overrides":[]}`,
		`{"Revision":"1","Overrides":"private-marker"}`, `{"Revision":"1","Overrides":1}`, `{"Revision":"1","Overrides":false}`,
		strings.Replace(adminSettingsNullUpdateForTest, `"Overrides":`, `"overrides":`, 1),
		strings.Replace(adminSettingsNullUpdateForTest, `"Overrides":`, `"Overrides":{},"Overrides":`, 1),
		strings.Replace(adminSettingsNullUpdateForTest, `"ServerName":null`, `"ServerName":null,"\u0053erverName":"private-marker"`, 1),
		strings.Replace(adminSettingsNullUpdateForTest, `"MaxWidth":null`, `"MaxWidth":null,"\u004daxWidth":1`, 1),
		strings.Replace(adminSettingsNullUpdateForTest, `"ServerName":null`, `"ServerName":null,"private-marker":null`, 1),
		strings.Replace(adminSettingsNullUpdateForTest, `"ServerName":null`, `"ServerName":null,"CacheDirectory":"private-marker"`, 1),
	} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(body, "application/json")); ok {
			t.Fatal("settings update accepted an unsupported object shape or key")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, "")
	}
	valid := `{"Overrides":{"MaxAudioChannels":null,"MaxHeight":null,"MaxWidth":null,"MaxBitrate":null,"\u0053erverName":"Goby"},"\u0052evision":"1"}`
	response := httptest.NewRecorder()
	request, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(valid, "application/json"))
	if !ok || request.Revision != 1 || request.Overrides.ServerName == nil || *request.Overrides.ServerName != "Goby" || response.Body.Len() != 0 {
		t.Fatal("settings update rejected reordered keys or a single escaped canonical key")
	}
}

func TestDecodeAdminSettingsResetRequiresUniqueSupportedFields(t *testing.T) {
	valid := `{"Fields":["MaxAudioChannels","MaxHeight","\u004daxWidth","MaxBitrate","ServerName"],"Revision":"1"}`
	response := httptest.NewRecorder()
	request, ok := decodeAdminSettingsReset(response, adminSettingsRequestForTest(valid, "application/json"))
	want := []settings.Field{settings.FieldMaxAudioChannels, settings.FieldMaxHeight, settings.FieldMaxWidth, settings.FieldMaxBitrate, settings.FieldServerName}
	if !ok || request.Revision != 1 || !reflect.DeepEqual(request.Fields, want) || response.Body.Len() != 0 {
		t.Fatal("settings reset changed a valid selection or its order")
	}
	for _, fields := range []string{
		`null`, `[]`, `{}`, `"ServerName"`, `true`, `1`, `[null]`, `[true]`, `[1]`, `[{}]`, `[[]]`,
		`[""]`, `["servername"]`, `["private-marker"]`, `["Deployment"]`, `["ServerName "]`,
		`["ServerName","ServerName"]`, `["ServerName","\u0053erverName"]`,
		`["ServerName","MaxBitrate","MaxWidth","MaxHeight","MaxAudioChannels","ServerName"]`,
	} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminSettingsReset(response, adminSettingsRequestForTest(`{"Revision":"1","Fields":`+fields+`}`, "application/json")); ok {
			t.Fatal("settings reset accepted an invalid or duplicate field selection")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, "Fields")
	}
	for _, body := range []string{
		`{"Revision":"1"}`, `{"Fields":["ServerName"]}`, `{"Revision":"1","fields":["ServerName"]}`,
		`{"Revision":"1","Fields":["ServerName"],"Fields":["MaxWidth"]}`,
		`{"Revision":"1","Fields":["ServerName"],"\u0046ields":["MaxWidth"]}`,
	} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminSettingsReset(response, adminSettingsRequestForTest(body, "application/json")); ok {
			t.Fatal("settings reset accepted an incomplete, mis-cased, or duplicate object")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, "")
	}
}

func TestDecodeAdminSettingsRejectsMalformedAmbiguousAndOversizedBodies(t *testing.T) {
	if maxAdminSettingsBodyBytes != 16*1024 {
		t.Fatal("settings body limit changed from 16 KiB")
	}
	for _, decoder := range adminSettingsDecodersForTest() {
		t.Run(decoder.name, func(t *testing.T) {
			for _, body := range []string{
				"", "null", "[]", "{}", "true", "1", `"private-marker"`, "{", "\xef\xbb\xbf" + decoder.valid,
				decoder.valid + "{}", decoder.valid + "null", decoder.valid + "private-marker", decoder.valid + "\x00", decoder.valid + "// comment",
				strings.Replace(decoder.valid, `"Revision":"1"`, `"Revision":"1","Revision":"2"`, 1),
				strings.Replace(decoder.valid, `"Revision":"1"`, `"Revision":"1","\u0052evision":"2"`, 1),
				strings.Replace(decoder.valid, `"Revision":"1"`, `"revision":"1"`, 1),
				strings.Replace(decoder.valid, `"Revision":"1"`, `"Revision":"1","private-marker":"private-marker"`, 1),
				strings.Replace(decoder.valid, `"Revision":"1"`, `"Revision":"1","Deployment":{"CacheDirectory":"private-marker"}`, 1),
				strings.Replace(decoder.valid, `"Revision":"1"`, "\"Revision\":\"1\",\"\xff\":null", 1),
				strings.Replace(decoder.valid, `"Revision":"1"`, `"Revision":"1","\ud800":null`, 1),
				decoder.valid + strings.Repeat(" ", maxAdminSettingsBodyBytes+1-len(decoder.valid)),
			} {
				response := httptest.NewRecorder()
				r := adminSettingsRequestForTest(body, "application/json")
				r.ContentLength = -1
				if decoder.decode(response, r) {
					t.Fatal("settings parser accepted malformed, ambiguous, private, or oversized input")
				}
				assertAdminSettingsInputError(t, response, http.StatusBadRequest, "")
			}
			for _, body := range []string{decoder.valid + " \t\r\n", decoder.valid + strings.Repeat(" ", maxAdminSettingsBodyBytes-len(decoder.valid))} {
				response := httptest.NewRecorder()
				if !decoder.decode(response, adminSettingsRequestForTest(body, "application/json")) || response.Body.Len() != 0 {
					t.Fatal("settings parser rejected allowed trailing whitespace or a body at exactly 16 KiB")
				}
			}
			response := httptest.NewRecorder()
			r := adminSettingsRequestForTest(decoder.valid, "application/json")
			r.Body = io.NopCloser(adminSettingsErrorReaderForTest{})
			if decoder.decode(response, r) {
				t.Fatal("settings parser accepted a failed body read")
			}
			assertAdminSettingsInputError(t, response, http.StatusBadRequest, "")
		})
	}
}

type adminSettingsErrorReaderForTest struct{}

func (adminSettingsErrorReaderForTest) Read([]byte) (int, error) {
	return 0, errors.New("private-marker body read failed")
}

func TestDecodeAdminSettingsRejectsInvalidUTF8AndUnpairedSurrogates(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{`"Goby \ud83d\udc1f"`, "Goby \U0001f41f"},
		{`"Goby \uFFFD"`, "Goby \ufffd"},
		{`"Goby \\uD800"`, `Goby \uD800`},
		{`"Caf\u00e9"`, "Caf\u00e9"},
	} {
		response := httptest.NewRecorder()
		request, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(adminSettingsUpdateBodyForTest("ServerName", test.raw), "application/json"))
		if !ok || request.Overrides.ServerName == nil || *request.Overrides.ServerName != test.want || response.Body.Len() != 0 {
			t.Fatal("settings parser rejected valid Unicode or confused literal backslash text with a surrogate escape")
		}
	}
	for _, raw := range []string{
		`"\ud800"`, `"\udbff"`, `"\udc00"`, `"\udfff"`, `"\ud800x"`, `"\ud800\u0041"`, `"\ud800\ud800"`, `"\udc00\ud800"`, `"\ud83d\ud800\udc00"`,
		"\"Goby \xff\"", "\"Goby \xc0\xaf\"", "\"Goby \xed\xa0\x80\"", "\"Goby \xf4\x90\x80\x80\"",
	} {
		response := httptest.NewRecorder()
		if _, ok := decodeAdminSettingsUpdate(response, adminSettingsRequestForTest(adminSettingsUpdateBodyForTest("ServerName", raw), "application/json")); ok {
			t.Fatal("settings parser repaired invalid UTF-8 or an unpaired surrogate instead of rejecting it")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, "")
	}
}

func TestDecodeAdminSettingsRequiresOneJSONContentTypeWithOptionalUTF8(t *testing.T) {
	for _, decoder := range adminSettingsDecodersForTest() {
		t.Run(decoder.name, func(t *testing.T) {
			for _, contentType := range []string{"application/json", "application/json; charset=utf-8", `application/json; charset="UTF-8"`} {
				response := httptest.NewRecorder()
				if !decoder.decode(response, adminSettingsRequestForTest(decoder.valid, contentType)) || response.Body.Len() != 0 {
					t.Fatal("settings parser rejected its declared JSON media type")
				}
			}
			for _, contentType := range []string{
				"", "text/plain", "application/x-www-form-urlencoded", "application/problem+json", "application/json, application/json",
				"application/json; invalid", "application/json; charset=iso-8859-1", "application/json; charset=utf8",
				"application/json; boundary=private-marker", "application/json; charset=utf-8; private-marker=value",
				"application/json; charset=utf-8; charset=utf-8", "application/json; charset=utf-8; Charset=utf-8",
				"application/json; private-marker*=invalid", "application/json; private-marker*1=x", "application/json; charset*=utf-8''utf-8",
			} {
				response := httptest.NewRecorder()
				if decoder.decode(response, adminSettingsRequestForTest(decoder.valid, contentType)) {
					t.Fatal("settings parser accepted an undeclared or ambiguous media type")
				}
				assertAdminSettingsInputError(t, response, http.StatusUnsupportedMediaType, "")
			}
			for _, second := range []string{"application/json", "text/plain", ""} {
				response := httptest.NewRecorder()
				r := adminSettingsRequestForTest(decoder.valid, "application/json")
				r.Header.Add("Content-Type", second)
				if decoder.decode(response, r) {
					t.Fatal("settings parser accepted multiple Content-Type header values")
				}
				assertAdminSettingsInputError(t, response, http.StatusUnsupportedMediaType, "")
			}
		})
	}
}

func TestAdminSettingsRejectsEveryQueryIncludingAnEmptyQuestionMark(t *testing.T) {
	response := httptest.NewRecorder()
	if !adminSettingsNoQuery(response, adminSettingsRequestForTest("", "")) || response.Body.Len() != 0 {
		t.Fatal("settings query validation rejected a query-free URL")
	}
	for _, query := range []string{"Revision=1", "Fields=ServerName", "api_key=private-marker", "private-marker=private-marker", "%zz", ";", "="} {
		response := httptest.NewRecorder()
		r := adminSettingsRequestForTest("", "")
		r.URL.RawQuery = query
		if adminSettingsNoQuery(response, r) {
			t.Fatal("settings query validation accepted an undeclared query")
		}
		assertAdminSettingsInputError(t, response, http.StatusBadRequest, "Query")
	}
	response = httptest.NewRecorder()
	r := adminSettingsRequestForTest("", "")
	r.URL.ForceQuery = true
	if adminSettingsNoQuery(response, r) {
		t.Fatal("settings query validation accepted a trailing question mark")
	}
	assertAdminSettingsInputError(t, response, http.StatusBadRequest, "Query")
	for _, decoder := range adminSettingsDecodersForTest() {
		for _, forceQuery := range []bool{false, true} {
			response := httptest.NewRecorder()
			r := adminSettingsRequestForTest(decoder.valid, "application/json")
			if forceQuery {
				r.URL.ForceQuery = true
			} else {
				r.URL.RawQuery = "api_key=private-marker"
			}
			if decoder.decode(response, r) {
				t.Fatal("settings mutation decoding bypassed strict query validation")
			}
			assertAdminSettingsInputError(t, response, http.StatusBadRequest, "Query")
		}
	}
}
