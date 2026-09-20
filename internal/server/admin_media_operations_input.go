package server

import (
	"bytes"
	"encoding/hex"
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
	"github.com/moooyo/goby/internal/media"
)

const (
	maxAdminMediaOperationBodyBytes     = 512 << 10
	maxAdminMediaOperationCueEdits      = 100
	maxAdminMediaOperationResponseBytes = 2 << 20
)

func adminMediaOperationInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the media operation request fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func adminMediaOperationNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		adminMediaOperationInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return false
	}
	return true
}

func adminMediaOperationID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != id {
		adminMediaOperationInputError(w, r, map[string]string{"Id": "Supply a lowercase 32-character hexadecimal identifier."})
		return "", false
	}
	return id, true
}

func adminMediaOperationBody(w http.ResponseWriter, r *http.Request, allowed, required []string) (map[string]json.RawMessage, bool) {
	if !adminMediaOperationNoQuery(w, r) {
		return nil, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return nil, false
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxAdminMediaOperationBodyBytes))
	if err != nil || !utf8.Valid(data) || !adminSettingsUnicode(data) {
		adminMediaOperationInputError(w, r, map[string]string{"Body": "Supply a lossless UTF-8 JSON object no larger than 512 KiB."})
		return nil, false
	}
	values, invalid := adminTaskObject(data, allowed, required, "")
	if len(invalid) != 0 {
		adminMediaOperationInputError(w, r, invalid)
		return nil, false
	}
	return values, true
}

func adminMediaOperationValue(raw json.RawMessage, name string, destination any, invalid map[string]string) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, destination) != nil {
		invalid[name] = "Supply the required value using its documented JSON type."
	}
}

func adminMediaOperationString(raw json.RawMessage, name string, maximum int, empty bool, invalid map[string]string) string {
	var value string
	adminMediaOperationValue(raw, name, &value, invalid)
	if (!empty && value == "") || len(value) > maximum || !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		invalid[name] = "Supply a bounded string without control characters or surrounding whitespace."
	}
	return value
}

func adminMediaOperationDecimal(raw json.RawMessage, name string, minimum int64, invalid map[string]string) int64 {
	var value string
	adminMediaOperationValue(raw, name, &value, invalid)
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < minimum || strconv.FormatInt(parsed, 10) != value {
		invalid[name] = "Supply a canonical decimal string within the signed 64-bit range."
	}
	return parsed
}

func decodeAdminMediaOperationStart(w http.ResponseWriter, r *http.Request, itemID string) (library.MediaOperationRequest, bool) {
	input := library.MediaOperationRequest{ItemID: itemID}
	fields := []string{"RequestId", "Kind", "MediaSourceId", "SourceRevision", "StreamIndex", "Parameters"}
	values, ok := adminMediaOperationBody(w, r, fields, fields)
	if !ok {
		return input, false
	}
	invalid := make(map[string]string)
	input.RequestID = adminMediaOperationString(values["RequestId"], "RequestId", 128, false, invalid)
	input.Kind = adminMediaOperationString(values["Kind"], "Kind", 64, false, invalid)
	input.MediaSourceID = adminMediaOperationString(values["MediaSourceId"], "MediaSourceId", 256, false, invalid)
	input.SourceRevision = adminMediaOperationString(values["SourceRevision"], "SourceRevision", 256, false, invalid)
	adminMediaOperationValue(values["StreamIndex"], "StreamIndex", &input.StreamIndex, invalid)
	if input.StreamIndex < 0 || input.StreamIndex > 4095 {
		invalid["StreamIndex"] = "Supply an indexed embedded subtitle stream between 0 and 4095."
	}
	if input.MediaSourceID != media.SourceID(itemID) {
		invalid["MediaSourceId"] = "Supply the current indexed media source identifier."
	}
	var parameterFields []string
	switch input.Kind {
	case library.MediaOperationRemoveSubtitle:
		parameterFields = []string{"Profile"}
	case library.MediaOperationOCR:
		parameterFields = []string{"ModelIds", "OutputFormat", "Language", "Title", "IsDefault", "IsForced", "IsHearingImpaired"}
	default:
		invalid["Kind"] = "Choose remove_embedded_subtitle or subtitle_ocr."
	}
	parameters, parameterErrors := adminTaskObject(values["Parameters"], parameterFields, parameterFields, "Parameters")
	for key, message := range parameterErrors {
		invalid[key] = message
	}
	if parameterErrors == nil {
		if input.Kind == library.MediaOperationRemoveSubtitle {
			input.Parameters.Profile = adminMediaOperationString(parameters["Profile"], "Parameters.Profile", 64, false, invalid)
			if input.Parameters.Profile != "matroska-v1" && input.Parameters.Profile != "mp4-movtext-v1" {
				invalid["Parameters.Profile"] = "Choose an admitted writable container profile."
			}
		} else if input.Kind == library.MediaOperationOCR {
			adminMediaOperationValue(parameters["ModelIds"], "Parameters.ModelIds", &input.Parameters.ModelIDs, invalid)
			seen := make(map[string]bool)
			if len(input.Parameters.ModelIDs) < 1 || len(input.Parameters.ModelIDs) > 3 {
				invalid["Parameters.ModelIds"] = "Choose one to three distinct admitted OCR model identifiers."
			}
			for _, id := range input.Parameters.ModelIDs {
				if !adminMediaOperationModelID(id) || seen[id] {
					invalid["Parameters.ModelIds"] = "Choose one to three distinct admitted OCR model identifiers."
				}
				seen[id] = true
			}
			input.Parameters.OutputFormat = adminMediaOperationString(parameters["OutputFormat"], "Parameters.OutputFormat", 3, false, invalid)
			if input.Parameters.OutputFormat != "srt" && input.Parameters.OutputFormat != "vtt" {
				invalid["Parameters.OutputFormat"] = "Choose srt or vtt."
			}
			input.Parameters.Language = adminMediaOperationString(parameters["Language"], "Parameters.Language", 32, true, invalid)
			if strings.Trim(input.Parameters.Language, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") != "" {
				invalid["Parameters.Language"] = "Supply at most 32 ASCII letters, digits or hyphens."
			}
			input.Parameters.Title = adminMediaOperationString(parameters["Title"], "Parameters.Title", 512, true, invalid)
			adminMediaOperationValue(parameters["IsDefault"], "Parameters.IsDefault", &input.Parameters.IsDefault, invalid)
			adminMediaOperationValue(parameters["IsForced"], "Parameters.IsForced", &input.Parameters.IsForced, invalid)
			adminMediaOperationValue(parameters["IsHearingImpaired"], "Parameters.IsHearingImpaired", &input.Parameters.IsHearingImpaired, invalid)
		}
	}
	if len(invalid) != 0 {
		adminMediaOperationInputError(w, r, invalid)
		return input, false
	}
	return input, true
}

func adminMediaOperationModelID(value string) bool {
	return value == "eng" || value == "chi_sim" || value == "chi_tra"
}

func decodeAdminMediaOperationApply(w http.ResponseWriter, r *http.Request) (library.MediaOperationApplyRequest, bool) {
	var input library.MediaOperationApplyRequest
	fields := []string{"Revision", "SourceRevision", "ResultHash", "RequestId"}
	values, ok := adminMediaOperationBody(w, r, fields, fields)
	if !ok {
		return input, false
	}
	invalid := make(map[string]string)
	input.Revision = adminMediaOperationDecimal(values["Revision"], "Revision", 1, invalid)
	input.SourceRevision = adminMediaOperationString(values["SourceRevision"], "SourceRevision", 256, false, invalid)
	input.RequestID = adminMediaOperationString(values["RequestId"], "RequestId", 128, false, invalid)
	input.ResultHash = adminMediaOperationString(values["ResultHash"], "ResultHash", 64, false, invalid)
	digest, err := hex.DecodeString(input.ResultHash)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != input.ResultHash {
		invalid["ResultHash"] = "Supply the current lowercase SHA-256 result hash."
	}
	if len(invalid) != 0 {
		adminMediaOperationInputError(w, r, invalid)
		return input, false
	}
	return input, true
}

func decodeAdminMediaOperationCancel(w http.ResponseWriter, r *http.Request) (int64, bool) {
	values, ok := adminMediaOperationBody(w, r, []string{"Revision"}, []string{"Revision"})
	if !ok {
		return 0, false
	}
	invalid := make(map[string]string)
	revision := adminMediaOperationDecimal(values["Revision"], "Revision", 1, invalid)
	if len(invalid) != 0 {
		adminMediaOperationInputError(w, r, invalid)
		return 0, false
	}
	return revision, true
}

func decodeAdminMediaOperationReview(w http.ResponseWriter, r *http.Request) (int64, []library.MediaOperationCueEdit, bool) {
	values, ok := adminMediaOperationBody(w, r, []string{"Revision", "Edits"}, []string{"Revision", "Edits"})
	if !ok {
		return 0, nil, false
	}
	invalid := make(map[string]string)
	revision := adminMediaOperationDecimal(values["Revision"], "Revision", 1, invalid)
	var rawEdits []json.RawMessage
	adminMediaOperationValue(values["Edits"], "Edits", &rawEdits, invalid)
	if len(rawEdits) < 1 || len(rawEdits) > maxAdminMediaOperationCueEdits {
		invalid["Edits"] = "Supply one to 100 cue edits."
	}
	edits := make([]library.MediaOperationCueEdit, 0, len(rawEdits))
	seen := make(map[int]bool)
	for index, raw := range rawEdits {
		if index >= maxAdminMediaOperationCueEdits {
			break
		}
		prefix := "Edits[" + strconv.Itoa(index) + "]"
		fields := []string{"Ordinal", "StartTicks", "EndTicks", "Text", "Included"}
		values, problems := adminTaskObject(raw, fields, fields, prefix)
		for key, message := range problems {
			invalid[key] = message
		}
		if problems != nil {
			continue
		}
		var edit library.MediaOperationCueEdit
		adminMediaOperationValue(values["Ordinal"], prefix+".Ordinal", &edit.Ordinal, invalid)
		if edit.Ordinal < 0 || edit.Ordinal >= library.MaxMediaOperationCues || seen[edit.Ordinal] {
			invalid[prefix+".Ordinal"] = "Supply distinct existing cue ordinals."
		}
		seen[edit.Ordinal] = true
		edit.StartTicks = adminMediaOperationDecimal(values["StartTicks"], prefix+".StartTicks", 0, invalid)
		edit.EndTicks = adminMediaOperationDecimal(values["EndTicks"], prefix+".EndTicks", 1, invalid)
		if edit.EndTicks <= edit.StartTicks {
			invalid[prefix+".EndTicks"] = "EndTicks must be later than StartTicks."
		}
		adminMediaOperationValue(values["Text"], prefix+".Text", &edit.Text, invalid)
		adminMediaOperationValue(values["Included"], prefix+".Included", &edit.Included, invalid)
		if !utf8.ValidString(edit.Text) || len(edit.Text) > media.MaxSubtitleOCRCueTextBytes || strings.IndexFunc(edit.Text, func(c rune) bool { return unicode.IsControl(c) && c != '\n' }) >= 0 || edit.Included && strings.TrimSpace(edit.Text) == "" {
			invalid[prefix+".Text"] = "Supply at most 4096 UTF-8 bytes of subtitle text; included cues must not be empty."
		}
		edits = append(edits, edit)
	}
	if len(invalid) != 0 {
		adminMediaOperationInputError(w, r, invalid)
		return 0, nil, false
	}
	return revision, edits, true
}

func adminMediaOperationPage(w http.ResponseWriter, r *http.Request, filters bool) (library.MediaOperationPageOptions, bool) {
	page := library.MediaOperationPageOptions{Limit: 50}
	if len(r.URL.RawQuery) > 4096 || r.URL.ForceQuery {
		adminMediaOperationInputError(w, r, map[string]string{"Query": "Supply a bounded pagination query."})
		return page, false
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		adminMediaOperationInputError(w, r, map[string]string{"Query": "Supply a valid pagination query."})
		return page, false
	}
	invalid := make(map[string]string)
	for name, entries := range values {
		if len(entries) != 1 {
			invalid[name] = "Supply each supported query parameter at most once."
			continue
		}
		value := entries[0]
		switch name {
		case "StartIndex", "Limit":
			parsed, err := strconv.ParseInt(value, 10, 32)
			if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value || name == "Limit" && (parsed < 1 || parsed > 100) {
				invalid[name] = "Use a nonnegative StartIndex or a Limit between 1 and 100."
				continue
			}
			if name == "StartIndex" {
				page.StartIndex = int(parsed)
			} else {
				page.Limit = int(parsed)
			}
		case "ItemId":
			decoded, err := hex.DecodeString(value)
			if !filters || err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != value {
				invalid[name] = "Supply a lowercase 32-character hexadecimal item identifier on the operation list."
			} else {
				page.ItemID = value
			}
		case "Kind":
			if !filters || value != library.MediaOperationOCR && value != library.MediaOperationRemoveSubtitle {
				invalid[name] = "Choose a supported operation kind on the operation list."
			} else {
				page.Kind = value
			}
		case "State":
			switch value {
			case "queued", "running", "ready", "applying", "completed", "failed", "cancelled", "interrupted", "stale", "recovery_required":
				if filters {
					page.State = value
					continue
				}
			}
			invalid[name] = "Choose a supported operation state on the operation list."
		default:
			invalid["Query"] = "The query contains an unsupported parameter."
		}
	}
	if len(invalid) != 0 {
		adminMediaOperationInputError(w, r, invalid)
		return page, false
	}
	return page, true
}
