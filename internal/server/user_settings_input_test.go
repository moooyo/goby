package server

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestDecodeUserSettingsPatchPreservesOrderedCaseInsensitiveScalars(t *testing.T) {
	patch, err := decodeUserSettingsPatch([]byte(`{"genreLimitOnDetails":"1","genreLimitOnDetails":"2",
		"Theme":"dark","theme":"light","count":123,"enabled":true,"disabled":false,"removed":null}`))
	if err != nil {
		t.Fatal(err)
	}
	text := func(value string) *string { return &value }
	want := identity.UserSettingsPatch{
		"genreLimitOnDetails": text("2"), "Theme": text("light"), "count": text("123"),
		"enabled": text("true"), "disabled": text("false"), "removed": nil,
	}
	if !reflect.DeepEqual(patch, want) {
		t.Fatal("ordered aliases, first spelling, scalar coercion, or deletion semantics changed")
	}
	patch, err = decodeUserSettingsPatch([]byte(`{"escaped":"first","esc\u0061ped":"last","unicode":"日本語 😀"}`))
	if err != nil || len(patch) != 2 || patch["escaped"] == nil || *patch["escaped"] != "last" || *patch["unicode"] != "日本語 😀" {
		t.Fatal("decoded key identity or exact Unicode changed")
	}
}

func TestDecodeUserSettingsStructuredValuesRetainObservedOrderAndText(t *testing.T) {
	patch, err := decodeUserSettingsPatch([]byte(`{"complex":{"object":{"text":"space value, \"quoted\" \\ backslash"},
		"array":["two words","with,comma","with\"quote","with\\backslash"]},"simple":{"nested":"value"},"list":["value"]}`))
	if err != nil || patch["complex"] == nil || patch["simple"] == nil || patch["list"] == nil {
		t.Fatal("the observed structured preference values did not decode")
	}
	if *patch["complex"] != `{object:{text:space value, "quoted" \ backslash},array:[two words,with,comma,with"quote,with\backslash]}` ||
		*patch["simple"] != `{nested:value}` || *patch["list"] != `[value]` {
		t.Fatal("structured preference conversion changed wire order, delimiters, or literal nested strings")
	}
}

func TestDecodeUserSettingsStructuredValuesHaveIndependentDepthAndNodeLimits(t *testing.T) {
	deep := `{"value":` + strings.Repeat("[", maxUserSettingsDepth+2) + `"text"` + strings.Repeat("]", maxUserSettingsDepth+2) + `}`
	if _, err := decodeUserSettingsPatch([]byte(deep)); !errors.Is(err, identity.ErrUserSettingsLimit) {
		t.Fatal("deep preference input did not stop at the nesting bound")
	}
	wide := `{"value":[` + strings.Repeat(`0,`, maxUserSettingsNodes) + `0]}`
	if _, err := decodeUserSettingsPatch([]byte(wide)); !errors.Is(err, identity.ErrUserSettingsLimit) {
		t.Fatal("many small preference nodes bypassed the independent node bound")
	}
}

func TestDecodeUserSettingsNestedMatrixPreservesNullsEmptyValuesAndDuplicateKeys(t *testing.T) {
	patch, err := decodeUserSettingsPatch([]byte(`{"m3e_nested":{"nullValue":null,"emptyString":"","emptyObject":{},"emptyArray":[],
		"object":{"dup":"first","dup":"second","Case":"upper","case":"lower","":"empty-key",
		"key with space":"space","key,comma":"comma","key:colon":"colon","key\"quote":"quote","key\\slash":"slash","\u96ea":"unicode-key"},
		"array":[null,"",{},[],{"innerNull":null,"innerEmpty":""},["",null,{},[]]],"tail":"end"}}`))
	if err != nil || patch["m3e_nested"] == nil {
		t.Fatal("the reference nested-value matrix did not decode")
	}
	want := `{nullValue:null,emptyString:,emptyObject:{},emptyArray:[],object:{dup:first,dup:second,Case:upper,case:lower,:empty-key,key with space:space,key,comma:comma,key:colon:colon,key"quote:quote,key\slash:slash,雪:unicode-key},array:[null,,{},[],{innerNull:null,innerEmpty:},[,null,{},[]]],tail:end}`
	if *patch["m3e_nested"] != want {
		t.Fatal("nested conversion applied top-level deletion, duplicate, casing, or quoting rules")
	}
}

func TestDecodeUserSettingsPatchRejectsLossyEncodingAndMalformedDocuments(t *testing.T) {
	for _, body := range []string{
		``, `null`, `[]`, `"value"`, `{"key":}`, `{"key":"value"`,
		`{"key":"value"} {}`, `{"key":"\ud800"}`, `{"\udc00":"value"}`,
		string([]byte{'{', '"', 'k', '"', ':', '"', 0xff, '"', '}'}),
	} {
		if _, err := decodeUserSettingsPatch([]byte(body)); err == nil {
			t.Fatal("malformed or lossy user settings input was accepted")
		}
	}
}
