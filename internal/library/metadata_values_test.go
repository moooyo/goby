package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func metadataTestRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal metadata test value: %v", err)
	}
	return raw
}

func metadataTestEdit() MetadataEdit {
	return MetadataEdit{Revision: "1", Overrides: map[string]json.RawMessage{}, LockedFields: []string{}}
}

func metadataTestObject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode metadata test object: %v", err)
	}
	return object
}

func metadataTestEqualJSON(t *testing.T, actual, expected []byte) {
	t.Helper()
	decode := func(raw []byte) any {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("decode JSON comparison: %v", err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(actual), decode(expected)) {
		t.Errorf("JSON = %s, want %s", actual, expected)
	}
}

func TestMetadataValuesAlwaysEncodeCompleteCollections(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("null"), []byte(`{"Genres":null,"Tags":null,"Studios":null,"People":null,"ProviderIDs":null}`)} {
		values, err := decodeMetadataValues(raw)
		if err != nil {
			t.Fatalf("decode empty metadata: %v", err)
		}
		if values.ProviderIDs == nil || values.Genres == nil || values.Tags == nil || values.Studios == nil || values.People == nil {
			t.Fatalf("decoded collections must be nonnil: %+v", values)
		}
	}
	encoded := metadataTestRaw(t, MetadataValues{})
	object := metadataTestObject(t, encoded)
	if len(object) != 15 {
		t.Errorf("complete value projection has %d fields, want 15: %s", len(object), encoded)
	}
	for _, field := range []string{"Genres", "Tags", "Studios", "People"} {
		if string(object[field]) != "[]" {
			t.Errorf("empty %s = %s, want []", field, object[field])
		}
	}
	if string(object["ProviderIds"]) != "{}" {
		t.Errorf("empty ProviderIds = %s, want {}", object["ProviderIds"])
	}
	for _, field := range []string{"ProductionYear", "PremiereDate", "CommunityRating", "IndexNumber", "ParentIndexNumber"} {
		if string(object[field]) != "null" {
			t.Errorf("absent %s = %s, want null", field, object[field])
		}
	}
	if _, exists := object["ProviderIDs"]; exists {
		t.Error("administrator projection used the internal ProviderIDs spelling")
	}
}

func TestNormalizeMetadataEditPreservesIndependentNamesAndCredits(t *testing.T) {
	edit := metadataTestEdit()
	edit.Revision = "9223372036854775807"
	edit.Overrides = map[string]json.RawMessage{
		"Name":              json.RawMessage(`"  Display title  "`),
		"SortName":          json.RawMessage(`"  Independent sort  "`),
		"Overview":          json.RawMessage(`"  Keep description spacing\n"`),
		"ProductionYear":    json.RawMessage(`null`),
		"PremiereDate":      json.RawMessage(`"2025-04-05T01:02:03+08:00"`),
		"CommunityRating":   json.RawMessage(`0`),
		"IndexNumber":       json.RawMessage(`0`),
		"ParentIndexNumber": json.RawMessage(`2147483647`),
		"ProviderIds":       json.RawMessage(`{"imdb":"tt123","TMDB":"456","tvdb":"789","Vendor":"part_1:abc.def-2"}`),
		"Genres":            json.RawMessage(`[" Drama ","Drama","drama"]`),
		"Tags":              json.RawMessage(`[]`),
		"Studios":           json.RawMessage(`[]`),
		"People": json.RawMessage(`[
			{"Name":" Same Person ","Type":"Actor","Role":"First role","SortOrder":0},
			{"Name":"Same Person","Type":"Director","SortOrder":null},
			{"Name":"Same Person","Type":"Writer","Role":"Second role","SortOrder":1}
		]`),
	}
	edit.LockedFields = []string{"Name", "Overview"}
	normalized, err := normalizeMetadataEdit(edit, metadataValueFieldNames)
	if err != nil {
		t.Fatalf("normalize valid metadata edit: %v", err)
	}
	if string(normalized.Overrides["Name"]) != `"Display title"` || string(normalized.Overrides["SortName"]) != `"Independent sort"` {
		t.Fatalf("name fields were not normalized independently: %+v", normalized.Overrides)
	}
	if !bytes.Equal(normalized.Overrides["Overview"], edit.Overrides["Overview"]) {
		t.Error("description spacing changed during normalization")
	}
	metadataTestEqualJSON(t, normalized.Overrides["ProviderIds"], []byte(`{"Imdb":"tt123","Tmdb":"456","Tvdb":"789","Vendor":"part_1:abc.def-2"}`))
	metadataTestEqualJSON(t, normalized.Overrides["Genres"], []byte(`["Drama","drama"]`))
	if string(normalized.Overrides["PremiereDate"]) != `"2025-04-04T17:02:03Z"` {
		t.Errorf("date was not normalized to UTC: %s", normalized.Overrides["PremiereDate"])
	}
	values, _, err := composeMetadataValues(nil, normalized.Overrides, nil)
	if err != nil {
		t.Fatalf("compose normalized values: %v", err)
	}
	if len(values.People) != 3 || values.People[0].Name != "Same Person" || values.People[0].SortOrder == nil || *values.People[0].SortOrder != 0 || values.People[1].SortOrder != nil || values.People[2].Role != "Second role" {
		t.Errorf("credit identity, order, or nullable sort was lost: %+v", values.People)
	}
	edit.Overrides["Name"][1] = 'X'
	edit.LockedFields[0] = "Tags"
	if string(normalized.Overrides["Name"]) != `"Display title"` || normalized.LockedFields[0] != "Name" {
		t.Error("normalized edit aliases caller-owned storage")
	}
}

func TestComposeMetadataValuesPrecedenceClearAndUnknownSource(t *testing.T) {
	source := []byte(`{"Kind":"movie","Name":"Automatic name","SortName":"automatic sort","Overview":"Automatic overview","ProductionYear":2020,"ProviderIDs":{"Vendor":"original"},"Genres":["Source genre"],"Future":{"Number":90071992547409931234,"Text":"<retain>"},"FutureList":[null,true,{"Name":"unknown"}]}`)
	original := append([]byte(nil), source...)
	locked := map[string]json.RawMessage{
		"Name": json.RawMessage(`"Locked name"`), "Overview": json.RawMessage(`"Locked overview"`),
		"ProductionYear": json.RawMessage(`2021`),
	}
	overrides := map[string]json.RawMessage{
		"Name": json.RawMessage(`"Manual name"`), "Overview": json.RawMessage(`""`),
		"ProductionYear": json.RawMessage(`null`), "Genres": json.RawMessage(`[]`), "ProviderIds": json.RawMessage(`{}`),
	}
	values, encoded, err := composeMetadataValues(source, overrides, locked)
	if err != nil {
		t.Fatalf("compose layered metadata: %v", err)
	}
	if values.Name != "Manual name" || values.SortName != "automatic sort" || values.Overview != "" || values.ProductionYear != nil || len(values.Genres) != 0 || len(values.ProviderIDs) != 0 {
		t.Fatalf("manual, locked, and automatic precedence is incorrect: %+v", values)
	}
	object := metadataTestObject(t, encoded)
	before := metadataTestObject(t, source)
	for _, field := range []string{"Kind", "Future", "FutureList"} {
		metadataTestEqualJSON(t, object[field], before[field])
	}
	if _, exists := object["ProviderIds"]; exists || string(object["ProviderIDs"]) != "{}" {
		t.Errorf("effective source provider spelling or clear is incorrect: %s", encoded)
	}
	delete(overrides, "Name")
	values, _, err = composeMetadataValues(source, overrides, locked)
	if err != nil || values.Name != "Locked name" {
		t.Fatalf("clearing the manual value did not reveal the lock: values = %+v, error = %v", values, err)
	}
	delete(locked, "Name")
	values, _, err = composeMetadataValues(source, overrides, locked)
	if err != nil || values.Name != "Automatic name" {
		t.Fatalf("clearing both controls did not reveal the source: values = %+v, error = %v", values, err)
	}
	values.ProviderIDs["new"] = "caller-change"
	values.Genres = append(values.Genres, "caller-change")
	if !bytes.Equal(source, original) {
		t.Error("composition changed the caller's source bytes")
	}
	if string(locked["Overview"]) != `"Locked overview"` || string(overrides["Overview"]) != `""` {
		t.Error("composition changed caller-owned layer values")
	}
}

func TestBuildMetadataProjectionOnlyChangesControlledFields(t *testing.T) {
	source := []byte(`{"Kind":"tvshow","Name":"Source name","Overview":"Source overview","ProviderIDs":{"Vendor":"keep"},"People":[{"Name":"Person","Type":"Actor","FutureCredit":"keep"}],"Future":{"Value":12345678901234567890}}`)
	original := append([]byte(nil), source...)
	controls := map[string]json.RawMessage{"Overview": json.RawMessage(`"Changed"`)}
	effective := MetadataValues{Name: "Uncontrolled effective name", Overview: "Changed"}
	projection, err := buildMetadataProjection(source, controls, nil, effective)
	if err != nil {
		t.Fatalf("build selective metadata projection: %v", err)
	}
	actual, expected := metadataTestObject(t, projection), metadataTestObject(t, source)
	if string(actual["Overview"]) != `"Changed"` {
		t.Errorf("controlled overview = %s", actual["Overview"])
	}
	delete(actual, "Overview")
	delete(expected, "Overview")
	metadataTestEqualJSON(t, metadataTestRaw(t, actual), metadataTestRaw(t, expected))
	if !bytes.Equal(source, original) {
		t.Error("projection changed the source bytes")
	}
	for _, empty := range [][]byte{nil, []byte("null")} {
		projection, err := buildMetadataProjection(empty, nil, nil, effective)
		if err != nil || projection != nil {
			t.Errorf("source-free uncontrolled item acquired metadata: projection = %s, error = %v", projection, err)
		}
	}
	projection, err = buildMetadataProjection(nil, map[string]json.RawMessage{"ProductionYear": json.RawMessage(`null`)}, nil, MetadataValues{})
	if err != nil {
		t.Fatalf("build explicit scalar-clear projection: %v", err)
	}
	metadataTestEqualJSON(t, projection, []byte(`{"ProductionYear":null}`))
	projection, err = buildMetadataProjection(source, nil, map[string]json.RawMessage{"ProviderIds": json.RawMessage(`{}`)}, MetadataValues{})
	if err != nil {
		t.Fatalf("project a controlled empty provider map: %v", err)
	}
	if object := metadataTestObject(t, projection); string(object["ProviderIDs"]) != "{}" || object["ProviderIds"] != nil {
		t.Errorf("controlled provider map did not replace the internal source field: %s", projection)
	}
}

func TestDecodeMetadataValuesProviderNamesDatesAndOwnership(t *testing.T) {
	raw := []byte(`{"ProviderIDs":{"Vendor":"internal"},"ProviderIds":{"Vendor":"public"},"PremiereDate":"2025-04-05T01:02:03+08:00","People":[{"Name":"Person","Type":"Actor","SortOrder":0}],"Future":true}`)
	values, err := decodeMetadataValues(raw)
	if err != nil {
		t.Fatalf("decode metadata source: %v", err)
	}
	if values.ProviderIDs["Vendor"] != "internal" || values.PremiereDate == nil || values.PremiereDate.Location() != time.UTC || values.PremiereDate.Format(time.RFC3339) != "2025-04-04T17:02:03Z" {
		t.Fatalf("source aliases or date normalization failed: %+v", values)
	}
	values.ProviderIDs["Vendor"] = "changed"
	*values.People[0].SortOrder = 99
	second, err := decodeMetadataValues(raw)
	if err != nil || second.ProviderIDs["Vendor"] != "internal" || *second.People[0].SortOrder != 0 {
		t.Fatalf("decoded values share mutable storage: values = %+v, error = %v", second, err)
	}
	public, err := decodeMetadataValues([]byte(`{"ProviderIds":{"Imdb":"tt1"}}`))
	if err != nil || public.ProviderIDs["Imdb"] != "tt1" {
		t.Fatalf("public values did not decode: %+v, %v", public, err)
	}
}

func TestNormalizeMetadataEditRejectsMalformedAndOutOfRangeValues(t *testing.T) {
	tests := []struct {
		name, field string
		raw         json.RawMessage
	}{
		{"blank_name", "Name", json.RawMessage(`"  "`)},
		{"null_name", "Name", json.RawMessage(`null`)},
		{"long_name", "Name", metadataTestRaw(t, strings.Repeat("n", 1025))},
		{"multibyte_name", "Name", metadataTestRaw(t, strings.Repeat("é", 513))},
		{"blank_sort", "SortName", json.RawMessage(`""`)},
		{"null_description", "Overview", json.RawMessage(`null`)},
		{"nul_description", "Overview", json.RawMessage(`"bad\u0000text"`)},
		{"invalid_utf8", "Overview", json.RawMessage{34, 0xff, 34}},
		{"long_description", "Overview", metadataTestRaw(t, strings.Repeat("d", 65537))},
		{"zero_year", "ProductionYear", json.RawMessage(`0`)},
		{"large_year", "ProductionYear", json.RawMessage(`10000`)},
		{"fraction_year", "ProductionYear", json.RawMessage(`2025.0`)},
		{"short_hour", "PremiereDate", json.RawMessage(`"2025-01-02T3:04:05Z"`)},
		{"bad_zone", "PremiereDate", json.RawMessage(`"2025-01-02T03:04:05+24:00"`)},
		{"long_fraction", "PremiereDate", json.RawMessage(`"2025-01-02T03:04:05.1234567890Z"`)},
		{"invalid_day", "PremiereDate", json.RawMessage(`"2025-02-29"`)},
		{"utc_year_overflow", "PremiereDate", json.RawMessage(`"9999-12-31T23:59:59-01:00"`)},
		{"utc_year_underflow", "PremiereDate", json.RawMessage(`"0001-01-01T00:00:00+01:00"`)},
		{"negative_rating", "CommunityRating", json.RawMessage(`-0.1`)},
		{"large_rating", "CommunityRating", json.RawMessage(`10.1`)},
		{"nonfinite_rating", "CommunityRating", json.RawMessage(`1e999`)},
		{"quoted_rating", "CommunityRating", json.RawMessage(`"NaN"`)},
		{"negative_index", "IndexNumber", json.RawMessage(`-1`)},
		{"null_index", "IndexNumber", json.RawMessage(`null`)},
		{"large_parent_index", "ParentIndexNumber", json.RawMessage(`2147483648`)},
		{"fraction_index", "IndexNumber", json.RawMessage(`1.5`)},
		{"null_providers", "ProviderIds", json.RawMessage(`null`)},
		{"empty_provider_key", "ProviderIds", json.RawMessage(`{"":"x"}`)},
		{"bad_provider_key", "ProviderIds", json.RawMessage(`{"bad-key":"x"}`)},
		{"bad_provider_value", "ProviderIds", json.RawMessage(`{"Imdb":"bad/value"}`)},
		{"empty_provider_value", "ProviderIds", json.RawMessage(`{"Imdb":""}`)},
		{"provider_alias_conflict", "ProviderIds", json.RawMessage(`{"imdb":"tt1","Imdb":"tt2"}`)},
		{"duplicate_provider", "ProviderIds", json.RawMessage(`{"Imdb":"tt1","Imdb":"tt2"}`)},
		{"null_genres", "Genres", json.RawMessage(`null`)},
		{"nonstring_genre", "Genres", json.RawMessage(`[1]`)},
		{"blank_tag", "Tags", json.RawMessage(`[" "]`)},
		{"null_studio", "Studios", json.RawMessage(`[null]`)},
		{"null_people", "People", json.RawMessage(`null`)},
		{"missing_person_name", "People", json.RawMessage(`[{"Type":"Actor"}]`)},
		{"missing_person_type", "People", json.RawMessage(`[{"Name":"Person"}]`)},
		{"unknown_person_type", "People", json.RawMessage(`[{"Name":"Person","Type":"Other Credit"}]`)},
		{"noncanonical_person_type", "People", json.RawMessage(`[{"Name":"Person","Type":"actor"}]`)},
		{"nul_person_role", "People", json.RawMessage(`[{"Name":"Person","Type":"Actor","Role":"\u0000"}]`)},
		{"negative_credit_order", "People", json.RawMessage(`[{"Name":"Person","Type":"Actor","SortOrder":-1}]`)},
		{"large_credit_order", "People", json.RawMessage(`[{"Name":"Person","Type":"Actor","SortOrder":2147483648}]`)},
		{"person_id_authority", "People", json.RawMessage(`[{"Name":"Person","Type":"Actor","Id":"123"}]`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			edit := metadataTestEdit()
			edit.Overrides[test.field] = test.raw
			_, err := normalizeMetadataEdit(edit, metadataValueFieldNames)
			var validation *MetadataValidationError
			if !errors.Is(err, ErrInvalidInput) || !errors.As(err, &validation) || validation.Fields[test.field] == "" {
				t.Fatalf("invalid %s value was not rejected with field diagnostics: %v", test.field, err)
			}
		})
	}
}

func TestNormalizeMetadataEditValidatesRevisionFieldScopeAndSize(t *testing.T) {
	for _, revision := range []string{"", "0", "-1", "+1", "01", "1 ", " 1", "9223372036854775808", "1.0"} {
		edit := metadataTestEdit()
		edit.Revision = revision
		_, err := normalizeMetadataEdit(edit, metadataValueFieldNames)
		var validation *MetadataValidationError
		if !errors.As(err, &validation) || validation.Fields["Revision"] == "" {
			t.Errorf("noncanonical revision %q was accepted: %v", revision, err)
		}
	}
	tests := []struct {
		name, field string
		edit        MetadataEdit
		editable    []string
	}{
		{"nil_overrides", "Overrides", MetadataEdit{Revision: "1", LockedFields: []string{}}, metadataValueFieldNames},
		{"nil_locks", "LockedFields", MetadataEdit{Revision: "1", Overrides: map[string]json.RawMessage{}}, metadataValueFieldNames},
		{"unknown_field", "Kind", MetadataEdit{Revision: "1", Overrides: map[string]json.RawMessage{"Kind": json.RawMessage(`"movie"`)}, LockedFields: []string{}}, metadataValueFieldNames},
		{"internal_provider_name", "ProviderIDs", MetadataEdit{Revision: "1", Overrides: map[string]json.RawMessage{"ProviderIDs": json.RawMessage(`{}`)}, LockedFields: []string{}}, metadataValueFieldNames},
		{"noneditable_field", "Name", MetadataEdit{Revision: "1", Overrides: map[string]json.RawMessage{"Name": json.RawMessage(`"Title"`)}, LockedFields: []string{}}, []string{"Overview"}},
		{"unknown_lock", "LockedFields", MetadataEdit{Revision: "1", Overrides: map[string]json.RawMessage{}, LockedFields: []string{"Kind"}}, metadataValueFieldNames},
		{"noneditable_lock", "LockedFields", MetadataEdit{Revision: "1", Overrides: map[string]json.RawMessage{}, LockedFields: []string{"Name"}}, []string{"Overview"}},
		{"duplicate_lock", "LockedFields", MetadataEdit{Revision: "1", Overrides: map[string]json.RawMessage{}, LockedFields: []string{"Name", "Name"}}, metadataValueFieldNames},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeMetadataEdit(test.edit, test.editable)
			var validation *MetadataValidationError
			if !errors.As(err, &validation) || validation.Fields[test.field] == "" {
				t.Fatalf("invalid edit shape or field scope was accepted: %v", err)
			}
		})
	}
	tooMany := make([]string, 1025)
	for index := range tooMany {
		tooMany[index] = "name"
	}
	edit := metadataTestEdit()
	edit.Overrides["Genres"] = metadataTestRaw(t, tooMany)
	if _, err := normalizeMetadataEdit(edit, metadataValueFieldNames); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("array entry limit was not enforced: %v", err)
	}
	large := make([]string, 20)
	for index := range large {
		large[index] = strings.Repeat(string(rune('a'+index)), 60000)
	}
	edit.Overrides["Genres"] = metadataTestRaw(t, large)
	_, err := normalizeMetadataEdit(edit, metadataValueFieldNames)
	var validation *MetadataValidationError
	if !errors.As(err, &validation) || validation.Fields["Overrides"] == "" {
		t.Fatalf("total metadata payload limit was not enforced: %v", err)
	}
}

func TestMetadataValueNullableScalarsAndExplicitEmptyCollections(t *testing.T) {
	edit := metadataTestEdit()
	for _, field := range []string{"ProductionYear", "PremiereDate", "CommunityRating", "ParentIndexNumber"} {
		edit.Overrides[field] = json.RawMessage(`null`)
	}
	for _, field := range []string{"Genres", "Tags", "Studios", "People"} {
		edit.Overrides[field] = json.RawMessage(`[]`)
	}
	edit.Overrides["ProviderIds"] = json.RawMessage(`{}`)
	for _, field := range []string{"Overview", "OriginalTitle", "OfficialRating"} {
		edit.Overrides[field] = json.RawMessage(`""`)
	}
	normalized, err := normalizeMetadataEdit(edit, metadataValueFieldNames)
	if err != nil {
		t.Fatalf("explicit empty values were rejected: %v", err)
	}
	if len(normalized.Overrides) != len(edit.Overrides) {
		t.Fatal("empty values were converted into inherited fields")
	}
	dateOnly := metadataTestEdit()
	dateOnly.Overrides["PremiereDate"] = json.RawMessage(`"2024-02-29"`)
	normalized, err = normalizeMetadataEdit(dateOnly, metadataValueFieldNames)
	if err != nil || string(normalized.Overrides["PremiereDate"]) != `"2024-02-29T00:00:00Z"` {
		t.Fatalf("ISO date was not accepted and normalized: %s, %v", normalized.Overrides["PremiereDate"], err)
	}
}
