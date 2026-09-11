package library

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestMergeAcceptedMusicSourceRetainsUnextractedLocalBytes(t *testing.T) {
	musicSources := []struct {
		name string
		raw  []byte
	}{
		{name: "Nil"},
		{name: "Empty", raw: []byte{}},
		{name: "Whitespace", raw: []byte(" \n\t ")},
		{name: "EmptyObject", raw: []byte(`{}`)},
		{name: "FormattedEmptyObject", raw: []byte(" { \n } ")},
		{name: "Null", raw: []byte(" \n null \t")},
	}
	localSources := []struct {
		name string
		raw  []byte
	}{
		{name: "NoNFO"},
		{name: "FormattedNFO", raw: []byte(" { \"Name\" : \"  Local title  \", \"Album\" : \"Old album\", \"FutureNumber\" : 90071992547409931234 } \n")},
	}
	for _, music := range musicSources {
		t.Run(music.name, func(t *testing.T) {
			for _, local := range localSources {
				t.Run(local.name, func(t *testing.T) {
					localBefore, musicBefore := bytes.Clone(local.raw), bytes.Clone(music.raw)
					merged, err := mergeAcceptedMusicSource(local.raw, music.raw)
					if err != nil {
						t.Fatalf("merge unextracted music source: %v", err)
					}
					if !bytes.Equal(merged, localBefore) || (local.raw == nil && merged != nil) {
						t.Errorf("unextracted music changed local bytes: got %q, want %q", merged, localBefore)
					}
					metadataMusicSourceAssertUnchanged(t, local.raw, localBefore, music.raw, musicBefore)
				})
			}
			hash, err := acceptedMusicSourceHash(music.raw)
			if err != nil || hash != "" {
				t.Errorf("unextracted source hash = %q, error = %v, want empty hash", hash, err)
			}
		})
	}
}

func TestMergeAcceptedMusicSourceWithoutNFOUsesAcceptedTextVerbatim(t *testing.T) {
	music := []byte(`{"Version":1,"Name":"  Embedded title  ","Album":"  Embedded album  ","Artists":[" A ","A","a"," A ",""," ",""],"AlbumArtists":[" Ensemble ","Ensemble"," Ensemble "]}`)
	before := bytes.Clone(music)
	merged, err := mergeAcceptedMusicSource(nil, music)
	if err != nil {
		t.Fatalf("merge embedded facts without NFO: %v", err)
	}
	object := metadataTestObject(t, merged)
	metadataMusicSourceAssertString(t, object, "Name", "  Embedded title  ")
	metadataMusicSourceAssertString(t, object, "Album", "  Embedded album  ")
	metadataMusicSourceAssertNames(t, object, "Artists", []string{" A ", "A", "a", "", " "})
	metadataMusicSourceAssertNames(t, object, "AlbumArtists", []string{" Ensemble ", "Ensemble"})
	if _, exists := object["Version"]; exists {
		t.Error("source version entered the metadata projection")
	}
	metadataMusicSourceAssertUnchanged(t, nil, nil, music, before)
}

func TestMergeAcceptedMusicSourcePreservesLocalNameAndUnknownNumbers(t *testing.T) {
	local := []byte(`{"Kind":"album","Name":"  Local title  ","Album":"Old album","Artists":["Old artist"],"AlbumArtists":["Old ensemble"],"FutureNumber":90071992547409931234,"Future":{"PreciseInteger":90071992547409931235,"Text":"  Preserve me  "},"People":[{"Name":"Credit","FutureCredit":{"PreciseInteger":90071992547409931236}}]}`)
	music := []byte(`{"Version":1,"Name":"Embedded title","Album":" ","Artists":["New artist"],"AlbumArtists":["New ensemble"]}`)
	localBefore, musicBefore := bytes.Clone(local), bytes.Clone(music)
	merged, err := mergeAcceptedMusicSource(local, music)
	if err != nil {
		t.Fatalf("merge accepted music with local metadata: %v", err)
	}
	object := metadataTestObject(t, merged)
	metadataMusicSourceAssertString(t, object, "Kind", "album")
	metadataMusicSourceAssertString(t, object, "Name", "  Local title  ")
	metadataMusicSourceAssertString(t, object, "Album", " ")
	metadataMusicSourceAssertNames(t, object, "Artists", []string{"New artist"})
	metadataMusicSourceAssertNames(t, object, "AlbumArtists", []string{"New ensemble"})
	if string(object["FutureNumber"]) != "90071992547409931234" {
		t.Errorf("unknown top-level number lost precision: %s", object["FutureNumber"])
	}
	future := metadataTestObject(t, object["Future"])
	if string(future["PreciseInteger"]) != "90071992547409931235" {
		t.Errorf("unknown nested number lost precision: %s", future["PreciseInteger"])
	}
	metadataMusicSourceAssertString(t, future, "Text", "  Preserve me  ")
	var people []map[string]json.RawMessage
	if err := json.Unmarshal(object["People"], &people); err != nil || len(people) != 1 {
		t.Fatalf("retained local credits = %s, error = %v", object["People"], err)
	}
	credit := metadataTestObject(t, people[0]["FutureCredit"])
	if string(credit["PreciseInteger"]) != "90071992547409931236" {
		t.Errorf("unknown credit number lost precision: %s", credit["PreciseInteger"])
	}
	metadataMusicSourceAssertUnchanged(t, local, localBefore, music, musicBefore)
}

func TestMergeAcceptedMusicSourceEmptyAcceptedFactsClearOnlyMusicFields(t *testing.T) {
	for _, music := range []string{
		`{"Version":1}`,
		`{"Version":1,"Name":"","Album":"","Artists":[],"AlbumArtists":[]}`,
	} {
		local := []byte(`{"Name":"Local title","Album":"Old album","Artists":["Old artist"],"AlbumArtists":["Old ensemble"],"FutureNumber":90071992547409931234}`)
		before := bytes.Clone(local)
		merged, err := mergeAcceptedMusicSource(local, []byte(music))
		if err != nil {
			t.Fatalf("merge accepted empty facts: %v", err)
		}
		object := metadataTestObject(t, merged)
		metadataMusicSourceAssertString(t, object, "Name", "Local title")
		metadataMusicSourceAssertNames(t, object, "Artists", []string{})
		metadataMusicSourceAssertNames(t, object, "AlbumArtists", []string{})
		if _, exists := object["Album"]; exists {
			t.Error("accepted empty album retained the old local album")
		}
		if string(object["FutureNumber"]) != "90071992547409931234" {
			t.Errorf("clearing music facts changed an unknown value: %s", object["FutureNumber"])
		}
		if !bytes.Equal(local, before) {
			t.Error("clearing accepted music facts changed the local input")
		}
	}
	for _, local := range [][]byte{nil, []byte(`{"Name":""}`)} {
		merged, err := mergeAcceptedMusicSource(local, []byte(`{"Version":1}`))
		if err != nil {
			t.Fatalf("merge empty accepted facts without a title: %v", err)
		}
		object := metadataTestObject(t, merged)
		metadataMusicSourceAssertNames(t, object, "Artists", []string{})
		metadataMusicSourceAssertNames(t, object, "AlbumArtists", []string{})
		if local == nil {
			if _, exists := object["Name"]; exists {
				t.Error("merge invented a name without local or embedded text")
			}
		} else {
			metadataMusicSourceAssertString(t, object, "Name", "")
		}
	}
}

func TestAcceptedMusicNameUsesVerbatimPriorityAndFallback(t *testing.T) {
	for _, scenario := range []struct {
		name, local, music, fallback, want string
	}{
		{name: "LocalName", local: `{"Name":"  Local  "}`, music: `{"Version":1,"Name":"Embedded"}`, fallback: "Fallback", want: "  Local  "},
		{name: "WhitespaceLocalName", local: `{"Name":" "}`, music: `{"Version":1,"Name":"Embedded"}`, fallback: "Fallback", want: " "},
		{name: "LocalNameKeepsExistingValidation", local: `{"Name":"Local\ttitle"}`, music: `{"Version":1,"Name":"Embedded"}`, fallback: "Fallback", want: "Local\ttitle"},
		{name: "EmbeddedName", music: `{"Version":1,"Name":"  Embedded  "}`, fallback: "Fallback", want: "  Embedded  "},
		{name: "EmptyLocalName", local: `{"Name":""}`, music: `{"Version":1,"Name":"Embedded"}`, fallback: "Fallback", want: "Embedded"},
		{name: "WhitespaceEmbeddedName", music: `{"Version":1,"Name":" "}`, fallback: "Fallback", want: " "},
		{name: "AcceptedEmptyFallback", local: `{}`, music: `{"Version":1}`, fallback: "  File name  ", want: "  File name  "},
		{name: "UnextractedFallback", fallback: "File name", want: "File name"},
		{name: "NullSourceFallback", music: `null`, fallback: "File name", want: "File name"},
		{name: "UnextractedLocalName", local: `{"Name":"Local"}`, music: `{}`, fallback: "Fallback", want: "Local"},
		{name: "AllNamesEmpty", local: `{"Name":""}`, music: `{"Version":1,"Name":""}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			local, music := []byte(scenario.local), []byte(scenario.music)
			localBefore, musicBefore := bytes.Clone(local), bytes.Clone(music)
			name, err := acceptedMusicName(local, music, scenario.fallback)
			if err != nil || name != scenario.want {
				t.Errorf("accepted name = %q, error = %v, want %q", name, err, scenario.want)
			}
			metadataMusicSourceAssertUnchanged(t, local, localBefore, music, musicBefore)
		})
	}
}

func TestAcceptedMusicSourceRejectsInvalidSnapshots(t *testing.T) {
	scenarios := []struct {
		name string
		raw  []byte
	}{
		{name: "OversizedUnextractedWhitespace", raw: bytes.Repeat([]byte(" "), musicSourceMaxBytes+1)},
		{name: "TopLevelArray", raw: []byte(`[]`)},
		{name: "TopLevelString", raw: []byte(`"music"`)},
		{name: "TopLevelNumber", raw: []byte(`1`)},
		{name: "TopLevelBoolean", raw: []byte(`true`)},
		{name: "MalformedJSON", raw: []byte(`{"Version":`)},
		{name: "TrailingJSON", raw: []byte(`{"Version":1} {}`)},
		{name: "MissingVersion", raw: []byte(`{"Artists":[]}`)},
		{name: "ZeroVersion", raw: []byte(`{"Version":0}`)},
		{name: "UnsupportedVersion", raw: []byte(`{"Version":2}`)},
		{name: "FractionalVersion", raw: []byte(`{"Version":1.5}`)},
		{name: "StringVersion", raw: []byte(`{"Version":"1"}`)},
		{name: "NullVersion", raw: []byte(`{"Version":null}`)},
		{name: "UnknownField", raw: []byte(`{"Version":1,"Future":true}`)},
		{name: "IncorrectFieldCase", raw: []byte(`{"version":1}`)},
		{name: "NullName", raw: []byte(`{"Version":1,"Name":null}`)},
		{name: "NumericName", raw: []byte(`{"Version":1,"Name":1}`)},
		{name: "NullAlbum", raw: []byte(`{"Version":1,"Album":null}`)},
		{name: "ArrayAlbum", raw: []byte(`{"Version":1,"Album":[]}`)},
		{name: "NullArtists", raw: []byte(`{"Version":1,"Artists":null}`)},
		{name: "NullAlbumArtists", raw: []byte(`{"Version":1,"AlbumArtists":null}`)},
		{name: "StringArtists", raw: []byte(`{"Version":1,"Artists":"Artist"}`)},
		{name: "ObjectAlbumArtists", raw: []byte(`{"Version":1,"AlbumArtists":{}}`)},
		{name: "NumericArtist", raw: []byte(`{"Version":1,"Artists":[1]}`)},
		{name: "NullArtist", raw: []byte(`{"Version":1,"Artists":[null]}`)},
		{name: "NestedAlbumArtist", raw: []byte(`{"Version":1,"AlbumArtists":[[]]}`)},
		{name: "NULName", raw: []byte(`{"Version":1,"Name":"bad\u0000name"}`)},
		{name: "NULArtist", raw: []byte(`{"Version":1,"Artists":["bad\u0000artist"]}`)},
		{name: "NewlineName", raw: []byte(`{"Version":1,"Name":"bad\nname"}`)},
		{name: "TabAlbum", raw: []byte(`{"Version":1,"Album":"bad\talbum"}`)},
		{name: "DELArtist", raw: []byte(`{"Version":1,"Artists":["bad\u007fartist"]}`)},
		{name: "C1AlbumArtist", raw: []byte(`{"Version":1,"AlbumArtists":["bad\u0085artist"]}`)},
	}
	for _, field := range []string{"Name", "Album"} {
		scenarios = append(scenarios, struct {
			name string
			raw  []byte
		}{name: "Oversized" + field, raw: metadataTestRaw(t, map[string]any{"Version": 1, field: strings.Repeat("x", metadataValueMaxName+1)})})
		scenarios = append(scenarios, struct {
			name string
			raw  []byte
		}{name: "OversizedUTF8" + field, raw: metadataTestRaw(t, map[string]any{"Version": 1, field: strings.Repeat("\u00e9", 513)})})
	}
	for _, field := range []string{"Artists", "AlbumArtists"} {
		scenarios = append(scenarios, struct {
			name string
			raw  []byte
		}{name: "Oversized" + field + "Entry", raw: metadataTestRaw(t, map[string]any{"Version": 1, field: []string{strings.Repeat("x", metadataValueMaxName+1)}})})
		scenarios = append(scenarios, struct {
			name string
			raw  []byte
		}{name: "OversizedUTF8" + field + "Entry", raw: metadataTestRaw(t, map[string]any{"Version": 1, field: []string{strings.Repeat("\u00e9", 513)}})})
		scenarios = append(scenarios, struct {
			name string
			raw  []byte
		}{name: "TooMany" + field, raw: metadataTestRaw(t, map[string]any{"Version": 1, field: make([]string, musicSourceMaxEntries+1)})})
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			local := []byte(`{"Name":"Local title"}`)
			localBefore, musicBefore := bytes.Clone(local), bytes.Clone(scenario.raw)
			if _, err := mergeAcceptedMusicSource(local, scenario.raw); err == nil {
				t.Error("merge accepted an invalid music source")
			}
			if _, err := acceptedMusicSourceHash(scenario.raw); err == nil {
				t.Error("hash accepted an invalid music source")
			}
			if _, err := acceptedMusicName(local, scenario.raw, "Fallback"); err == nil {
				t.Error("name selection ignored an invalid music source")
			}
			metadataMusicSourceAssertUnchanged(t, local, localBefore, scenario.raw, musicBefore)
		})
	}
}

func TestAcceptedMusicSourcePreservesExactUTF8ByteLimit(t *testing.T) {
	text := " " + strings.Repeat("\u00e9", 511) + " "
	music := metadataTestRaw(t, map[string]any{
		"Version": 1, "Name": text, "Album": text,
		"Artists": []string{text}, "AlbumArtists": []string{text},
	})
	before := bytes.Clone(music)
	merged, err := mergeAcceptedMusicSource(nil, music)
	if err != nil {
		t.Fatalf("merge accepted text at the 1024-byte limit: %v", err)
	}
	object := metadataTestObject(t, merged)
	metadataMusicSourceAssertString(t, object, "Name", text)
	metadataMusicSourceAssertString(t, object, "Album", text)
	metadataMusicSourceAssertNames(t, object, "Artists", []string{text})
	metadataMusicSourceAssertNames(t, object, "AlbumArtists", []string{text})
	name, err := acceptedMusicName(nil, music, "Fallback")
	if err != nil || name != text {
		t.Errorf("accepted name at the byte limit changed: got %q, error = %v", name, err)
	}
	hash, err := acceptedMusicSourceHash(music)
	if err != nil || len(hash) != 64 {
		t.Errorf("hash rejected text at the byte limit: hash = %q, error = %v", hash, err)
	}
	metadataMusicSourceAssertUnchanged(t, nil, nil, music, before)
}

func TestAcceptedMusicSourceHashNormalizesFormattingAndExactDuplicates(t *testing.T) {
	for _, group := range []struct {
		name    string
		sources []string
	}{
		{
			name: "AcceptedEmpty",
			sources: []string{
				`{"Version":1}`,
				`{"Artists":[],"AlbumArtists":[],"Version":1}`,
				`{"Version":1,"Name":"","Album":"","Artists":[],"AlbumArtists":[]}`,
			},
		},
		{
			name: "VerbatimNames",
			sources: []string{
				`{"Version":1,"Name":"  Title  ","Album":" Album ","Artists":[" A ","A",""," "],"AlbumArtists":[" Ensemble ","Ensemble"]}`,
				` { "AlbumArtists" : [" Ensemble ","Ensemble"," Ensemble "], "Artists" : [" A "," A ","A",""," ",""], "Album" : " Album ", "Name" : "  Title  ", "Version" : 1 } `,
				`{"Version":1,"Name":"  Title  ","Album":" Album ","Artists":[" A ","\u0041",""," "],"AlbumArtists":[" Ensemble ","Ensemble"]}`,
			},
		},
	} {
		t.Run(group.name, func(t *testing.T) {
			var expected string
			for _, source := range group.sources {
				raw := []byte(source)
				before := bytes.Clone(raw)
				hash, err := acceptedMusicSourceHash(raw)
				if err != nil {
					t.Fatalf("hash accepted music source: %v", err)
				}
				decoded, err := hex.DecodeString(hash)
				if err != nil || len(decoded) != 32 || len(hash) != 64 || hash != strings.ToLower(hash) {
					t.Fatalf("music source hash is not a lowercase SHA-256 digest: %q", hash)
				}
				if expected == "" {
					expected = hash
				} else if hash != expected {
					t.Errorf("equivalent source hash = %q, want %q", hash, expected)
				}
				if !bytes.Equal(raw, before) {
					t.Error("hashing changed the accepted source input")
				}
			}
		})
	}
}

func TestAcceptedMusicSourceHashPreservesTextAndArrayOrder(t *testing.T) {
	baseline := []byte(`{"Version":1,"Name":"Title","Album":"Album","Artists":["A","B"],"AlbumArtists":["C","D"]}`)
	baselineHash, err := acceptedMusicSourceHash(baseline)
	if err != nil || baselineHash == "" {
		t.Fatalf("hash baseline music source: hash = %q, error = %v", baselineHash, err)
	}
	for _, scenario := range []struct {
		name, source string
	}{
		{name: "NameWhitespace", source: `{"Version":1,"Name":" Title ","Album":"Album","Artists":["A","B"],"AlbumArtists":["C","D"]}`},
		{name: "AlbumWhitespace", source: `{"Version":1,"Name":"Title","Album":" Album ","Artists":["A","B"],"AlbumArtists":["C","D"]}`},
		{name: "ArtistWhitespace", source: `{"Version":1,"Name":"Title","Album":"Album","Artists":[" A ","B"],"AlbumArtists":["C","D"]}`},
		{name: "ArtistCase", source: `{"Version":1,"Name":"Title","Album":"Album","Artists":["a","B"],"AlbumArtists":["C","D"]}`},
		{name: "ArtistOrder", source: `{"Version":1,"Name":"Title","Album":"Album","Artists":["B","A"],"AlbumArtists":["C","D"]}`},
		{name: "AlbumArtistOrder", source: `{"Version":1,"Name":"Title","Album":"Album","Artists":["A","B"],"AlbumArtists":["D","C"]}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			hash, err := acceptedMusicSourceHash([]byte(scenario.source))
			if err != nil || hash == "" || hash == baselineHash {
				t.Errorf("distinct accepted text or order lost from hash: hash = %q, baseline = %q, error = %v", hash, baselineHash, err)
			}
		})
	}
}

func metadataMusicSourceAssertString(t *testing.T, object map[string]json.RawMessage, field, expected string) {
	t.Helper()
	var actual *string
	if err := json.Unmarshal(object[field], &actual); err != nil || actual == nil {
		t.Fatalf("metadata field %s is not a string: %s, error = %v", field, object[field], err)
	}
	if *actual != expected {
		t.Errorf("metadata field %s = %q, want %q", field, *actual, expected)
	}
}

func metadataMusicSourceAssertNames(t *testing.T, object map[string]json.RawMessage, field string, expected []string) {
	t.Helper()
	var actual []string
	if err := json.Unmarshal(object[field], &actual); err != nil {
		t.Fatalf("decode metadata field %s: %v", field, err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("metadata field %s = %#v, want %#v", field, actual, expected)
	}
}

func metadataMusicSourceAssertUnchanged(t *testing.T, local, localBefore, music, musicBefore []byte) {
	t.Helper()
	if !bytes.Equal(local, localBefore) {
		t.Error("merging or reading accepted music changed the local input")
	}
	if !bytes.Equal(music, musicBefore) {
		t.Error("merging or reading accepted music changed the music input")
	}
}
