package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/metadata"
)

// MetadataValues is the complete administrator-facing value projection.
// Nullable scalars retain absence; collections always encode as arrays or maps.
type MetadataValues struct {
	Name              string            `json:"Name"`
	SortName          string            `json:"SortName"`
	Overview          string            `json:"Overview"`
	OriginalTitle     string            `json:"OriginalTitle"`
	OfficialRating    string            `json:"OfficialRating"`
	ProductionYear    *int              `json:"ProductionYear"`
	PremiereDate      *time.Time        `json:"PremiereDate"`
	CommunityRating   *float64          `json:"CommunityRating"`
	ProviderIDs       map[string]string `json:"ProviderIds"`
	Genres            []string          `json:"Genres"`
	Tags              []string          `json:"Tags"`
	Studios           []string          `json:"Studios"`
	People            []metadata.Person `json:"People"`
	IndexNumber       *int              `json:"IndexNumber"`
	ParentIndexNumber *int              `json:"ParentIndexNumber"`
}

func (values MetadataValues) MarshalJSON() ([]byte, error) {
	type wireValues MetadataValues
	return json.Marshal(wireValues(completeMetadataCollections(values)))
}

type MetadataEdit struct {
	Revision     string                     `json:"Revision"`
	Overrides    map[string]json.RawMessage `json:"Overrides"`
	LockedFields []string                   `json:"LockedFields"`
}

type ItemMetadataDetail struct {
	ItemID         string                     `json:"ItemId"`
	LibraryID      string                     `json:"LibraryId"`
	ParentID       string                     `json:"ParentId"`
	ParentName     string                     `json:"ParentName"`
	Type           string                     `json:"Type"`
	Path           string                     `json:"Path"`
	Name           string                     `json:"Name"`
	IsFolder       bool                       `json:"IsFolder"`
	Revision       string                     `json:"Revision"`
	Automatic      MetadataValues             `json:"Automatic"`
	Effective      MetadataValues             `json:"Effective"`
	Overrides      map[string]json.RawMessage `json:"Overrides"`
	LockedValues   map[string]json.RawMessage `json:"LockedValues"`
	LockedFields   []string                   `json:"LockedFields"`
	EditableFields []string                   `json:"EditableFields"`
	InactiveFields []string                   `json:"InactiveFields"`
	LastEditedBy   string                     `json:"LastEditedBy"`
	LastEditedAt   *time.Time                 `json:"LastEditedAt"`
}

var ErrRevisionConflict = errors.New("metadata revision conflict")

type MetadataValidationError struct {
	Fields map[string]string
}

func (err *MetadataValidationError) Error() string {
	return "invalid metadata edit"
}

func (err *MetadataValidationError) Unwrap() error {
	return ErrInvalidInput
}

const (
	metadataEditMaxBytes    = 1024 * 1024
	metadataValueMaxBytes   = 64 * 1024
	metadataValueMaxName    = 1024
	metadataValueMaxEntries = 1024
)

var metadataValueFieldNames = []string{
	"Name", "SortName", "Overview", "OriginalTitle", "OfficialRating",
	"ProductionYear", "PremiereDate", "CommunityRating", "ProviderIds",
	"Genres", "Tags", "Studios", "People", "IndexNumber", "ParentIndexNumber",
}

func normalizeMetadataEdit(edit MetadataEdit, editable []string) (MetadataEdit, error) {
	fields := make(map[string]string)
	if revision, err := strconv.ParseInt(edit.Revision, 10, 64); err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != edit.Revision {
		fields["Revision"] = "Must be a canonical positive integer revision."
	}
	if edit.Overrides == nil {
		fields["Overrides"] = "Must be an object, including when empty."
	}
	if edit.LockedFields == nil {
		fields["LockedFields"] = "Must be an array, including when empty."
	}
	if encoded, err := json.Marshal(edit); err != nil {
		fields["Overrides"] = "Contains invalid JSON values."
	} else if len(encoded) > metadataEditMaxBytes {
		fields["Overrides"] = "The metadata edit must not exceed 1 MiB."
	}
	allowed := make(map[string]bool, len(editable))
	for _, field := range editable {
		if knownMetadataValueField(field) {
			allowed[field] = true
		}
	}
	result := MetadataEdit{
		Revision: edit.Revision, Overrides: make(map[string]json.RawMessage, len(edit.Overrides)),
		LockedFields: make([]string, 0, len(edit.LockedFields)),
	}
	keys := make([]string, 0, len(edit.Overrides))
	for field := range edit.Overrides {
		keys = append(keys, field)
	}
	sort.Strings(keys)
	for _, field := range keys {
		if !knownMetadataValueField(field) || !allowed[field] {
			fields[field] = "This metadata field is not editable."
			continue
		}
		value, err := normalizeMetadataValue(field, edit.Overrides[field])
		if err != nil {
			fields[field] = err.Error()
			continue
		}
		result.Overrides[field] = value
	}
	seenLocks := make(map[string]bool, len(edit.LockedFields))
	for _, field := range edit.LockedFields {
		if !knownMetadataValueField(field) || !allowed[field] {
			fields["LockedFields"] = "Contains an unknown or non-editable metadata field."
			continue
		}
		if seenLocks[field] {
			fields["LockedFields"] = "Must not contain duplicate fields."
			continue
		}
		seenLocks[field] = true
		result.LockedFields = append(result.LockedFields, field)
	}
	if len(fields) != 0 {
		return MetadataEdit{}, &MetadataValidationError{Fields: fields}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return MetadataEdit{}, fmt.Errorf("encode normalized metadata edit: %w", err)
	}
	if len(encoded) > metadataEditMaxBytes {
		return MetadataEdit{}, &MetadataValidationError{Fields: map[string]string{
			"Overrides": "The normalized metadata edit must not exceed 1 MiB.",
		}}
	}
	return result, nil
}

// decodeMetadataValues accepts the stored internal spelling ProviderIDs and the
// public ProviderIds spelling. The internal spelling wins if both are present.
func decodeMetadataValues(raw []byte) (MetadataValues, error) {
	source, err := metadataSourceObject(raw)
	if err != nil {
		return MetadataValues{}, err
	}
	public := make(map[string]json.RawMessage, len(metadataValueFieldNames))
	for _, field := range metadataValueFieldNames {
		value, exists := source[metadataInternalField(field)]
		if !exists {
			value, exists = source[field]
		}
		if exists {
			public[field] = value
		}
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		return MetadataValues{}, fmt.Errorf("encode metadata values: %w", err)
	}
	var values MetadataValues
	if err := json.Unmarshal(encoded, &values); err != nil {
		return MetadataValues{}, fmt.Errorf("decode metadata values: %w", err)
	}
	values = completeMetadataCollections(values)
	if values.PremiereDate != nil {
		utc := values.PremiereDate.UTC()
		if utc.Year() < 1 || utc.Year() > 9999 {
			return MetadataValues{}, fmt.Errorf("metadata premiere date is outside the UTC year range")
		}
		values.PremiereDate = &utc
	}
	return values, nil
}

// composeMetadataValues preserves source fields that this version does not
// understand. Manual values take precedence over retained lock snapshots.
func composeMetadataValues(automatic []byte, overrides, locked map[string]json.RawMessage) (MetadataValues, []byte, error) {
	source, err := metadataSourceObject(automatic)
	if err != nil {
		return MetadataValues{}, nil, err
	}
	for _, layer := range []map[string]json.RawMessage{locked, overrides} {
		for field, raw := range layer {
			if !knownMetadataValueField(field) {
				return MetadataValues{}, nil, &MetadataValidationError{Fields: map[string]string{
					field: "Unknown metadata field.",
				}}
			}
			value, err := normalizeMetadataValue(field, raw)
			if err != nil {
				return MetadataValues{}, nil, &MetadataValidationError{Fields: map[string]string{field: err.Error()}}
			}
			source[metadataInternalField(field)] = value
			if field == "ProviderIds" {
				delete(source, field)
			}
		}
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return MetadataValues{}, nil, fmt.Errorf("encode composed metadata source: %w", err)
	}
	values, err := decodeMetadataValues(encoded)
	if err != nil {
		return MetadataValues{}, nil, err
	}
	complete, err := metadataValueObject(values)
	if err != nil {
		return MetadataValues{}, nil, err
	}
	for field, value := range complete {
		source[field] = value
	}
	delete(source, "ProviderIds")
	encoded, err = json.Marshal(source)
	if err != nil {
		return MetadataValues{}, nil, fmt.Errorf("encode effective metadata: %w", err)
	}
	return values, encoded, nil
}

// buildMetadataProjection only replaces fields controlled by an override or a
// lock. Optional source scalars, Kind, and unknown source fields stay intact.
func buildMetadataProjection(localSource []byte, overrides, locked map[string]json.RawMessage, effective MetadataValues) ([]byte, error) {
	trimmed := bytes.TrimSpace(localSource)
	if (len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))) && len(overrides) == 0 && len(locked) == 0 {
		return nil, nil
	}
	source, err := metadataSourceObject(localSource)
	if err != nil {
		return nil, err
	}
	values, err := metadataValueObject(effective)
	if err != nil {
		return nil, err
	}
	for _, controls := range []map[string]json.RawMessage{locked, overrides} {
		for field := range controls {
			if !knownMetadataValueField(field) {
				return nil, &MetadataValidationError{Fields: map[string]string{field: "Unknown metadata field."}}
			}
			internal := metadataInternalField(field)
			source[internal] = append(json.RawMessage(nil), values[internal]...)
			if field == "ProviderIds" {
				delete(source, field)
			}
		}
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("encode metadata item projection: %w", err)
	}
	return encoded, nil
}

func knownMetadataValueField(field string) bool {
	for _, known := range metadataValueFieldNames {
		if field == known {
			return true
		}
	}
	return false
}

func metadataInternalField(field string) string {
	if field == "ProviderIds" {
		return "ProviderIDs"
	}
	return field
}

func completeMetadataCollections(values MetadataValues) MetadataValues {
	if values.ProviderIDs == nil {
		values.ProviderIDs = make(map[string]string)
	}
	if values.Genres == nil {
		values.Genres = []string{}
	}
	if values.Tags == nil {
		values.Tags = []string{}
	}
	if values.Studios == nil {
		values.Studios = []string{}
	}
	if values.People == nil {
		values.People = []metadata.Person{}
	}
	return values
}

func metadataValueObject(values MetadataValues) (map[string]json.RawMessage, error) {
	values = completeMetadataCollections(values)
	if values.PremiereDate != nil {
		utc := values.PremiereDate.UTC()
		values.PremiereDate = &utc
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode complete metadata values: %w", err)
	}
	object, err := metadataJSONObject(encoded)
	if err != nil {
		return nil, err
	}
	object["ProviderIDs"] = object["ProviderIds"]
	delete(object, "ProviderIds")
	return object, nil
}

func metadataSourceObject(raw []byte) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return make(map[string]json.RawMessage), nil
	}
	object, err := metadataJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("decode metadata source object: %w", err)
	}
	return object, nil
}

// Raw values avoid converting unknown numbers through float64 and own their
// byte storage. Duplicate object keys are rejected before a map can hide them.
func metadataJSONObject(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("must contain valid UTF-8 JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, fmt.Errorf("must be a JSON object")
	}
	object := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("must be a valid JSON object")
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("must have string object keys")
		}
		if _, exists := object[key]; exists {
			return nil, fmt.Errorf("must not contain duplicate object keys")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("must contain valid JSON values")
		}
		object[key] = append(json.RawMessage(nil), value...)
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, fmt.Errorf("must be a complete JSON object")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("must contain exactly one JSON object")
	}
	return object, nil
}

func normalizeMetadataValue(field string, raw json.RawMessage) (json.RawMessage, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, fmt.Errorf("Must contain valid UTF-8 JSON.")
	}
	var value any
	switch field {
	case "Name", "SortName":
		text, err := metadataStringValue(raw, true, metadataValueMaxName, true)
		if err != nil {
			return nil, err
		}
		value = text
	case "Overview", "OriginalTitle", "OfficialRating":
		text, err := metadataStringValue(raw, false, metadataValueMaxBytes, false)
		if err != nil {
			return nil, err
		}
		value = text
	case "ProductionYear":
		number, err := metadataIntegerValue(raw, 1, 9999)
		if err != nil {
			return nil, err
		}
		value = number
	case "IndexNumber":
		number, err := metadataIntegerValue(raw, 0, math.MaxInt32)
		if err != nil || number == nil {
			return nil, fmt.Errorf("Must be an integer from 0 to 2147483647.")
		}
		value = number
	case "ParentIndexNumber":
		number, err := metadataIntegerValue(raw, 0, math.MaxInt32)
		if err != nil {
			return nil, err
		}
		value = number
	case "PremiereDate":
		date, err := metadataDateValue(raw)
		if err != nil {
			return nil, err
		}
		value = date
	case "CommunityRating":
		var rating *float64
		if err := json.Unmarshal(raw, &rating); err != nil || (rating != nil && (math.IsNaN(*rating) || math.IsInf(*rating, 0) || *rating < 0 || *rating > 10)) {
			return nil, fmt.Errorf("Must be a finite number from 0 to 10 or null.")
		}
		value = rating
	case "ProviderIds":
		providers, err := metadataProviderValues(raw)
		if err != nil {
			return nil, err
		}
		value = providers
	case "Genres", "Tags", "Studios":
		names, err := metadataStringValues(raw)
		if err != nil {
			return nil, err
		}
		value = names
	case "People":
		people, err := metadataPeopleValues(raw)
		if err != nil {
			return nil, err
		}
		value = people
	default:
		return nil, fmt.Errorf("Unknown metadata field.")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("Could not encode the metadata value.")
	}
	return encoded, nil
}

func metadataStringValue(raw []byte, nonempty bool, maximum int, trim bool) (string, error) {
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "", fmt.Errorf("Must be a string.")
	}
	text := *value
	if trim {
		text = strings.TrimSpace(text)
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') || len(text) > maximum || (nonempty && strings.TrimSpace(text) == "") {
		if nonempty {
			return "", fmt.Errorf("Must be nonempty UTF-8 text without NUL, at most %d bytes.", maximum)
		}
		return "", fmt.Errorf("Must be UTF-8 text without NUL, at most %d bytes.", maximum)
	}
	return text, nil
}

func metadataIntegerValue(raw []byte, minimum, maximum int64) (*int, error) {
	var value *int64
	if err := json.Unmarshal(raw, &value); err != nil || (value != nil && (*value < minimum || *value > maximum)) {
		return nil, fmt.Errorf("Must be an integer from %d to %d or null.", minimum, maximum)
	}
	if value == nil {
		return nil, nil
	}
	number := int(*value)
	return &number, nil
}

func metadataProviderValues(raw []byte) (map[string]string, error) {
	object, err := metadataJSONObject(raw)
	if err != nil || len(object) > metadataValueMaxEntries {
		return nil, fmt.Errorf("Must be an object with at most %d provider IDs.", metadataValueMaxEntries)
	}
	providers := make(map[string]string, len(object))
	for name, rawValue := range object {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 64 || !metadataASCII(name, "") {
			return nil, fmt.Errorf("Provider keys must be ASCII alphanumeric text from 1 to 64 bytes.")
		}
		value, err := metadataStringValue(rawValue, true, 256, true)
		if err != nil || !metadataASCII(value, "._:-") {
			return nil, fmt.Errorf("Provider IDs must be bounded ASCII alphanumeric text with only ._:- separators.")
		}
		switch strings.ToLower(name) {
		case "imdb":
			name = "Imdb"
		case "tmdb":
			name = "Tmdb"
		case "tvdb":
			name = "Tvdb"
		}
		if previous, exists := providers[name]; exists && previous != value {
			return nil, fmt.Errorf("Canonical provider keys must not have conflicting IDs.")
		}
		providers[name] = value
	}
	return providers, nil
}

func metadataASCII(value, extra string) bool {
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune(extra, character) {
			continue
		}
		return false
	}
	return true
}

func metadataStringValues(raw []byte) ([]string, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil || len(entries) > metadataValueMaxEntries {
		return nil, fmt.Errorf("Must be an array with at most %d names; use [] to clear it.", metadataValueMaxEntries)
	}
	names := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name, err := metadataStringValue(entry, true, metadataValueMaxBytes, true)
		if err != nil {
			return nil, err
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	return names, nil
}

func metadataPeopleValues(raw []byte) ([]metadata.Person, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil || len(entries) > metadataValueMaxEntries {
		return nil, fmt.Errorf("Must be an array with at most %d credits; use [] to clear it.", metadataValueMaxEntries)
	}
	people := make([]metadata.Person, 0, len(entries))
	for _, entry := range entries {
		fields, err := metadataJSONObject(entry)
		if err != nil {
			return nil, fmt.Errorf("Every credit must be an object.")
		}
		for field := range fields {
			if field != "Name" && field != "Role" && field != "Type" && field != "SortOrder" {
				return nil, fmt.Errorf("Credit objects may only contain Name, Role, Type, and SortOrder.")
			}
		}
		var person metadata.Person
		person.Name, err = metadataStringValue(fields["Name"], true, metadataValueMaxBytes, true)
		if err != nil {
			return nil, fmt.Errorf("Every credit requires a bounded nonempty Name.")
		}
		person.Type, err = metadataStringValue(fields["Type"], true, metadataValueMaxBytes, true)
		if err != nil {
			return nil, fmt.Errorf("Every credit requires a bounded nonempty Type.")
		}
		switch person.Type {
		case "Actor", "Director", "Writer", "Producer", "GuestStar", "Composer", "Conductor", "Lyricist":
		default:
			return nil, fmt.Errorf("Credit Type must be Actor, Director, Writer, Producer, GuestStar, Composer, Conductor, or Lyricist.")
		}
		if role, exists := fields["Role"]; exists {
			person.Role, err = metadataStringValue(role, false, metadataValueMaxBytes, false)
			if err != nil {
				return nil, fmt.Errorf("Credit Role must be bounded UTF-8 text without NUL.")
			}
		}
		if order, exists := fields["SortOrder"]; exists {
			person.SortOrder, err = metadataIntegerValue(order, 0, math.MaxInt32)
			if err != nil {
				return nil, fmt.Errorf("Credit SortOrder must be an integer from 0 to 2147483647 or null.")
			}
		}
		people = append(people, person)
	}
	return people, nil
}

func metadataDateValue(raw []byte) (*time.Time, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	text, err := metadataStringValue(raw, true, 64, false)
	if err != nil || !metadataDateSyntax(text) {
		return nil, fmt.Errorf("Must be an ISO date, strict RFC3339 timestamp, or null.")
	}
	value, err := time.Parse("2006-01-02", text)
	if err != nil {
		value, err = time.Parse(time.RFC3339Nano, text)
	}
	if err != nil || value.Year() < 1 || value.Year() > 9999 {
		return nil, fmt.Errorf("Must be a valid date in years 1 through 9999.")
	}
	value = value.UTC()
	if value.Year() < 1 || value.Year() > 9999 {
		return nil, fmt.Errorf("The UTC date must be in years 1 through 9999.")
	}
	return &value, nil
}

func metadataDateSyntax(text string) bool {
	if len(text) < 10 || text[4] != '-' || text[7] != '-' || !metadataDecimalDigits(text[:4]) || !metadataDecimalDigits(text[5:7]) || !metadataDecimalDigits(text[8:10]) {
		return false
	}
	if len(text) == 10 {
		return true
	}
	if len(text) < 20 || text[10] != 'T' || text[13] != ':' || text[16] != ':' || !metadataDecimalDigits(text[11:13]) || !metadataDecimalDigits(text[14:16]) || !metadataDecimalDigits(text[17:19]) {
		return false
	}
	zone := len(text) - 1
	if text[zone] != 'Z' {
		zone = len(text) - 6
		if zone < 19 || (text[zone] != '+' && text[zone] != '-') || text[zone+3] != ':' || !metadataDecimalDigits(text[zone+1:zone+3]) || !metadataDecimalDigits(text[zone+4:]) {
			return false
		}
		hour := (text[zone+1]-'0')*10 + text[zone+2] - '0'
		minute := (text[zone+4]-'0')*10 + text[zone+5] - '0'
		if hour > 23 || minute > 59 {
			return false
		}
	}
	return zone == 19 || (zone >= 21 && zone <= 29 && text[19] == '.' && metadataDecimalDigits(text[20:zone]))
}

func metadataDecimalDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
