package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

const maxAdminMediaAnalysisBodyBytes = 64 << 10

func adminMediaAnalysisInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the media analysis request fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func adminMediaAnalysisNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		adminMediaAnalysisInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return false
	}
	return true
}

func adminMediaAnalysisBody(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, bool) {
	if !adminMediaAnalysisNoQuery(w, r) {
		return nil, false
	}
	rawType := r.Header.Get("Content-Type")
	_, parameter, hasParameter := strings.Cut(rawType, ";")
	parameterName, _, hasParameterValue := strings.Cut(parameter, "=")
	validParameter := !hasParameter || hasParameterValue && strings.EqualFold(strings.TrimSpace(parameterName), "charset")
	mediaType, parameters, err := mime.ParseMediaType(rawType)
	if len(r.Header.Values("Content-Type")) != 1 || err != nil || mediaType != "application/json" || len(parameters) > 1 ||
		!validParameter || len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8") || strings.Count(rawType, ";") > 1 {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json with UTF-8 for this request.")
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAdminMediaAnalysisBodyBytes))
	if err != nil || !utf8.Valid(data) || !adminSettingsUnicode(data) {
		adminMediaAnalysisInputError(w, r, map[string]string{"Body": "Supply a lossless UTF-8 JSON object no larger than 64 KiB."})
		return nil, false
	}
	values, invalid := adminTaskObject(data, fields, fields, "")
	if len(invalid) != 0 {
		adminMediaAnalysisInputError(w, r, invalid)
		return nil, false
	}
	return values, true
}

func adminMediaAnalysisValue(raw json.RawMessage, name string, destination any, invalid map[string]string) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, destination) != nil {
		invalid[name] = "Supply the required value using its documented JSON type."
	}
}

func adminMediaAnalysisText(raw json.RawMessage, name string, maximum int, empty bool, invalid map[string]string) string {
	var value string
	adminMediaAnalysisValue(raw, name, &value, invalid)
	if !empty && value == "" || len(value) > maximum || !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		invalid[name] = "Supply a bounded UTF-8 string without controls or surrounding whitespace."
	}
	return value
}

func adminMediaAnalysisRevision(raw json.RawMessage, name string, allowZero bool, invalid map[string]string) string {
	var text string
	adminMediaAnalysisValue(raw, name, &text, invalid)
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 || !allowZero && value == 0 || strconv.FormatInt(value, 10) != text {
		invalid[name] = "Supply the current canonical decimal revision string within the signed 64-bit range."
	}
	return text
}

var adminMediaAnalysisProfileFields = []string{"AutoPublishIntros", "PreviewIntervalSeconds", "PreviewQuality", "MaxSourceBytes", "MaxItemRuntimeSeconds", "FeatureCacheMaxBytes"}

func decodeAdminMediaAnalysisConfiguration(w http.ResponseWriter, r *http.Request) (library.AnalysisConfigurationUpdate, bool) {
	var result library.AnalysisConfigurationUpdate
	values, ok := adminMediaAnalysisBody(w, r, []string{"Revision", "Profile"})
	if !ok {
		return result, false
	}
	invalid := make(map[string]string)
	result.Revision = adminMediaAnalysisRevision(values["Revision"], "Revision", false, invalid)
	profile, profileErrors := adminTaskObject(values["Profile"], adminMediaAnalysisProfileFields, adminMediaAnalysisProfileFields, "Profile")
	for field, message := range profileErrors {
		invalid[field] = message
	}
	if len(profileErrors) == 0 {
		adminMediaAnalysisValue(profile["AutoPublishIntros"], "Profile.AutoPublishIntros", &result.Profile.AutoPublishIntros, invalid)
		for _, field := range []struct {
			name             string
			minimum, maximum int64
			target           any
		}{
			{"PreviewIntervalSeconds", 2, 120, &result.Profile.PreviewIntervalSeconds},
			{"PreviewQuality", 40, 95, &result.Profile.PreviewQuality},
			{"MaxSourceBytes", 1, 1 << 40, &result.Profile.MaxSourceBytes},
			{"MaxItemRuntimeSeconds", 1, 7200, &result.Profile.MaxItemRuntimeSeconds},
			{"FeatureCacheMaxBytes", 1 << 20, 512 << 20, &result.Profile.FeatureCacheMaxBytes},
		} {
			var number int64
			name := "Profile." + field.name
			adminMediaAnalysisValue(profile[field.name], name, &number, invalid)
			if number < field.minimum || number > field.maximum {
				invalid[name] = "Supply an integer JSON number within the documented profile range."
			}
			if invalid[name] == "" {
				switch target := field.target.(type) {
				case *int:
					*target = int(number)
				case *int64:
					*target = number
				}
			}
		}
	}
	if len(invalid) != 0 {
		adminMediaAnalysisInputError(w, r, invalid)
		return library.AnalysisConfigurationUpdate{}, false
	}
	return result, true
}

type adminMediaAnalysisRunInput struct {
	TaskKey   string
	RequestID string
	Selection library.AnalysisSelection
}

func decodeAdminMediaAnalysisRun(w http.ResponseWriter, r *http.Request) (adminMediaAnalysisRunInput, bool) {
	var result adminMediaAnalysisRunInput
	values, ok := adminMediaAnalysisBody(w, r, []string{"Kind", "RequestId", "LibraryIds", "ItemIds", "Force"})
	if !ok {
		return result, false
	}
	invalid := make(map[string]string)
	kind := adminMediaAnalysisText(values["Kind"], "Kind", 16, false, invalid)
	switch kind {
	case "intro":
		result.TaskKey = library.TaskIntroAnalysisKey
	case "previews":
		result.TaskKey = library.TaskPreviewGenerationKey
	default:
		invalid["Kind"] = "Choose intro or previews."
	}
	result.RequestID = adminMediaAnalysisText(values["RequestId"], "RequestId", tasks.MaxRequestIDBytes, false, invalid)
	adminMediaAnalysisValue(values["LibraryIds"], "LibraryIds", &result.Selection.LibraryIDs, invalid)
	adminMediaAnalysisValue(values["ItemIds"], "ItemIds", &result.Selection.ItemIDs, invalid)
	adminMediaAnalysisValue(values["Force"], "Force", &result.Selection.Force, invalid)
	selection, err := library.NormalizeAnalysisSelection(result.Selection)
	if err != nil {
		invalid["Selection"] = "Supply at most 64 distinct library and 256 distinct item identifiers, each at most 128 UTF-8 bytes without spaces or controls."
	}
	if len(invalid) != 0 {
		adminMediaAnalysisInputError(w, r, invalid)
		return adminMediaAnalysisRunInput{}, false
	}
	result.Selection = selection
	return result, true
}

func decodeAdminMediaAnalysisDecision(w http.ResponseWriter, r *http.Request) (library.AnalysisDecision, bool) {
	var result library.AnalysisDecision
	values, ok := adminMediaAnalysisBody(w, r, []string{"Revision", "SourceRevision", "ManualRevision", "Action"})
	if !ok {
		return result, false
	}
	invalid := make(map[string]string)
	result.Revision = adminMediaAnalysisRevision(values["Revision"], "Revision", true, invalid)
	result.ManualRevision = adminMediaAnalysisRevision(values["ManualRevision"], "ManualRevision", true, invalid)
	result.SourceRevision = adminMediaAnalysisText(values["SourceRevision"], "SourceRevision", 256, false, invalid)
	result.Action = adminMediaAnalysisText(values["Action"], "Action", 16, false, invalid)
	if result.Action != "accept" && result.Action != "reject" && result.Action != "reset" {
		invalid["Action"] = "Choose accept, reject or reset."
	}
	if len(invalid) != 0 {
		adminMediaAnalysisInputError(w, r, invalid)
		return library.AnalysisDecision{}, false
	}
	return result, true
}

func decodeAdminMediaAnalysisPrune(w http.ResponseWriter, r *http.Request) (string, bool) {
	values, ok := adminMediaAnalysisBody(w, r, []string{"Revision"})
	if !ok {
		return "", false
	}
	invalid := make(map[string]string)
	revision := adminMediaAnalysisRevision(values["Revision"], "Revision", false, invalid)
	if len(invalid) != 0 {
		adminMediaAnalysisInputError(w, r, invalid)
		return "", false
	}
	return revision, true
}

func adminMediaAnalysisID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := library.NormalizeAnalysisSelection(library.AnalysisSelection{ItemIDs: []string{id}}); err != nil {
		adminMediaAnalysisInputError(w, r, map[string]string{"Id": "Supply one bounded media item identifier."})
		return "", false
	}
	return id, true
}

func adminMediaAnalysisQuery(w http.ResponseWriter, r *http.Request) (library.AnalysisItemQuery, bool) {
	result := library.AnalysisItemQuery{Limit: 50}
	invalid := func() (library.AnalysisItemQuery, bool) {
		adminMediaAnalysisInputError(w, r, map[string]string{"Query": "Supply supported, unique, bounded media analysis filters and canonical pagination values."})
		return result, false
	}
	if len(r.URL.RawQuery) > 4096 || !utf8.ValidString(r.URL.RawQuery) {
		return invalid()
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return invalid()
	}
	for name, entries := range values {
		if len(entries) != 1 || !utf8.ValidString(entries[0]) || strings.IndexFunc(entries[0], unicode.IsControl) >= 0 {
			return invalid()
		}
		value := entries[0]
		switch name {
		case "LibraryId":
			if _, err := library.NormalizeAnalysisSelection(library.AnalysisSelection{LibraryIDs: []string{value}}); err != nil {
				return invalid()
			}
			result.LibraryID = value
		case "SearchTerm":
			if len(value) > 256 {
				return invalid()
			}
			result.SearchTerm = value
		case "StartIndex", "Limit":
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number < 0 || strconv.FormatInt(number, 10) != value || name == "Limit" && (number < 1 || number > 200) {
				return invalid()
			}
			if name == "StartIndex" {
				result.StartIndex = int(number)
			} else {
				result.Limit = int(number)
			}
		default:
			return invalid()
		}
	}
	return result, true
}
