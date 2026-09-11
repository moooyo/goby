package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func musicMetadataProbeDocument(tags, streams string) []byte {
	if streams == "" {
		streams = `[{"index":3,"codec_type":"audio","codec_name":"mp3","sample_rate":"48000",` +
			`"channels":2,"bit_rate":"192000","bits_per_sample":16,"time_base":"1/48000",` +
			`"tags":{"language":"eng","title":"Audio stream label"},"disposition":{"default":1}}]`
	}
	if tags != "" {
		tags = `,"tags":` + tags
	}
	return []byte(`{"format":{"format_name":"mp3","duration":"180","start_time":"0",` +
		`"bit_rate":"192000","size":"4321"` + tags + `},"streams":` + streams +
		`,"chapters":[{"start_time":"0","end_time":"180","tags":{"title":"Opening chapter"}}]}`)
}

func TestMusicMetadataFormatTagsPreserveTechnicalFacts(t *testing.T) {
	baseline, err := parseProbe(musicMetadataProbeDocument("", ""))
	if err != nil {
		t.Fatal("parse technical music baseline")
	}
	info, err := parseProbe(musicMetadataProbeDocument(`{
		"title":"  Title \u00e9 \ud83c\udfb5  ","album":"Tagged Album",
		"artist":"Earth, Wind & Fire; Guest / Duo",
		"album_artist":"Unobserved Album Artist","composer":"Unobserved Composer","track":"3/12","disc":"1/2"
	}`, ""))
	if err != nil {
		t.Fatalf("parse supported format music tags: %v", err)
	}
	want := &MusicMetadata{Version: 1, Title: "  Title \u00e9 \U0001f3b5  ", Album: "Tagged Album", Artist: "Earth, Wind & Fire; Guest / Duo"}
	if !reflect.DeepEqual(info.EmbeddedMusic, want) {
		t.Fatal("format music facts changed observed tag text or inferred extra metadata")
	}
	if info.Streams[0].Title != "Audio stream label" || info.Streams[0].Language != "eng" ||
		info.DurationTicks != 180*TicksPerSecond || !info.FormatStartKnown || info.Size != 4321 {
		t.Fatal("music extraction changed independent stream or format facts")
	}
	baseline.EmbeddedMusic, info.EmbeddedMusic = nil, nil
	if !reflect.DeepEqual(info, baseline) {
		t.Fatal("format music extraction changed the technical media snapshot")
	}
}

func TestMusicMetadataInspectedEmptyFactsDifferFromLegacyCache(t *testing.T) {
	for _, tags := range []string{"", `{}`, `{"title":"","album":"","artist":""}`,
		`{"album_artist":"Unobserved","composer":["Ignored"],"track":3,"disc":null,"private":{"nested":true}}`,
		`{"unknown":"\ud800","another":[null,false,1],"private":"` + strings.Repeat("x", 2048) + `"}`} {
		info, err := parseProbe(musicMetadataProbeDocument(tags, ""))
		if err != nil || !reflect.DeepEqual(info.EmbeddedMusic, &MusicMetadata{Version: 1}) {
			t.Fatal("an inspected source without supported tags lost its explicit empty version")
		}
		encoded, err := json.Marshal(info)
		if err != nil {
			t.Fatal("encode inspected music cache")
		}
		var restored Info
		if err := json.Unmarshal(encoded, &restored); err != nil || !reflect.DeepEqual(restored.EmbeddedMusic, info.EmbeddedMusic) {
			t.Fatal("music extraction presence did not survive cache serialization")
		}
	}
	var legacy Info
	if err := json.Unmarshal([]byte(`{"ProbeVersion":6,"Container":"mp3","Streams":[{"Index":0,"CodecType":"audio"}]}`), &legacy); err != nil || legacy.EmbeddedMusic != nil {
		t.Fatal("a legacy cache was mislabeled as inspected music metadata")
	}
	if CurrentProbeVersion != 6 || (Prober{}).CacheVersion() != 6 || CurrentMusicMetadataVersion != 1 || (Prober{}).MusicMetadataVersion() != 1 {
		t.Fatal("music metadata refresh changed the accepted technical probe version")
	}
}

func TestMusicMetadataCaseAliasesPreserveMatchingScalarValues(t *testing.T) {
	for _, tags := range []string{
		`{"TITLE":"Title","ALBUM":"Album","ARTIST":"Artist"}`,
		`{"tItLe":"Title","aLbUm":"Album","aRtIsT":"Artist"}`,
		`{"title":"Title","TITLE":"Title","title":"Title","album":"Album","artist":"Artist"}`,
		`{"\u0074itle":"Title","TITLE":"\u0054itle","album":"Album","artist":"Artist"}`,
	} {
		info, err := parseProbe(musicMetadataProbeDocument(tags, ""))
		if err != nil || !reflect.DeepEqual(info.EmbeddedMusic, &MusicMetadata{Version: 1, Title: "Title", Album: "Album", Artist: "Artist"}) {
			t.Fatal("matching ASCII-insensitive music aliases did not preserve one exact scalar")
		}
	}
	info, err := parseProbe(musicMetadataProbeDocument(`{"arti\u017ft":"Not an ASCII alias"," title":"Not an exact tag key"}`, ""))
	if err != nil || !reflect.DeepEqual(info.EmbeddedMusic, &MusicMetadata{Version: 1}) {
		t.Fatal("non-ASCII or whitespace key variants became supported music tags")
	}
}

func TestMusicMetadataRejectsConflictsAndWrongKnownTypes(t *testing.T) {
	tests := []struct{ name, tags string }{
		{"same_key_conflict", `{"title":"private-first-value","title":"private-second-value"}`},
		{"case_key_conflict", `{"artist":"private-first-value","ARTIST":"private-second-value"}`},
		{"encoded_key_conflict", `{"album":"private-first-value","\u0041LBUM":"private-second-value"}`},
		{"empty_value_conflict", `{"title":"","TITLE":"private-second-value"}`},
		{"null", `{"title":null}`},
		{"number", `{"album":12}`},
		{"boolean", `{"artist":false}`},
		{"array", `{"artist":["private-first-value"]}`},
		{"object", `{"title":{"private-first-value":true}}`},
		{"later_invalid_alias", `{"title":"private-first-value","TITLE":null}`},
		{"invalid_after_valid_field", `{"title":"private-first-value","album":false}`},
		{"null_tag_container", `null`},
		{"array_tag_container", `[]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info, err := parseProbe(musicMetadataProbeDocument(test.tags, ""))
			if !errors.Is(err, ErrInvalidMusicMetadata) || !reflect.DeepEqual(info, Info{}) {
				t.Fatal("invalid music tags produced usable or partially accepted facts")
			}
			if err.Error() != "invalid embedded music metadata" || strings.Contains(err.Error(), "private-") {
				t.Fatal("music tag errors exposed untrusted metadata values")
			}
		})
	}
}

func TestMusicMetadataRejectsMalformedUnicodeAndControlCharacters(t *testing.T) {
	for _, value := range []string{`"\ud800"`, `"\udc00"`, `"\ud83c\u0041"`, `"\ud800x"`,
		`"line\nbreak"`, `"tab\tvalue"`, `"nul\u0000value"`, `"delete\u007fvalue"`, `"next\u0085line"`,
		"\"invalid-" + string([]byte{0xff}) + "\""} {
		if _, err := parseProbe(musicMetadataProbeDocument(`{"title":`+value+`}`, "")); !errors.Is(err, ErrInvalidMusicMetadata) {
			t.Fatal("invalid Unicode or a control character entered music metadata")
		}
	}
	for _, test := range []struct{ raw, want string }{
		{`"\ud83c\udfb5"`, "\U0001f3b5"},
		{`"\ufffd"`, "\ufffd"},
		{`"\\ud800"`, `\ud800`},
		{`"Quoted \"title\""`, `Quoted "title"`},
	} {
		info, err := parseProbe(musicMetadataProbeDocument(`{"title":`+test.raw+`}`, ""))
		if err != nil || info.EmbeddedMusic == nil || info.EmbeddedMusic.Title != test.want {
			t.Fatal("valid Unicode or escaped literal text was altered by tag parsing")
		}
	}
}

func TestMusicMetadataBoundsDecodedNamesAndTotalExtractedBytes(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", 1024), strings.Repeat("\u00e9", 512)} {
		tags, err := json.Marshal(map[string]string{"title": text, "album": text, "artist": text})
		if err != nil {
			t.Fatal("encode boundary music tags")
		}
		info, err := parseProbe(musicMetadataProbeDocument(string(tags), ""))
		if err != nil || info.EmbeddedMusic == nil || info.EmbeddedMusic.Title != text || info.EmbeddedMusic.Album != text || info.EmbeddedMusic.Artist != text {
			t.Fatal("exactly bounded decoded music metadata was rejected or truncated")
		}
	}
	for _, text := range []string{strings.Repeat("x", 1025), strings.Repeat("\u00e9", 513)} {
		tags, err := json.Marshal(map[string]string{"title": text})
		if err != nil {
			t.Fatal("encode excessive music tag")
		}
		if _, err := parseProbe(musicMetadataProbeDocument(string(tags), "")); !errors.Is(err, ErrInvalidMusicMetadata) {
			t.Fatal("an oversized decoded music name was accepted")
		}
	}
	if _, err := parseMusicMetadata(json.RawMessage(`{"unknown":"` + strings.Repeat("x", maxProbeOutput) + `"}`)); !errors.Is(err, ErrInvalidMusicMetadata) {
		t.Fatal("standalone music parsing exceeded the containing probe output budget")
	}
}

func TestMusicMetadataIsLimitedToAudioWithoutRealVideo(t *testing.T) {
	const audio = `{"index":3,"codec_type":"audio","codec_name":"mp3","sample_rate":"48000","channels":2}`
	const picture = `{"index":7,"codec_type":"video","codec_name":"mjpeg","disposition":{"attached_pic":1}}`
	const video = `{"index":7,"codec_type":"video","codec_name":"h264","disposition":{"attached_pic":0}}`
	info, err := parseProbe(musicMetadataProbeDocument(`{"title":"Album track"}`, "["+audio+","+picture+"]"))
	if err != nil || info.EmbeddedMusic == nil || info.EmbeddedMusic.Title != "Album track" || !info.Streams[1].IsAttachedPicture {
		t.Fatal("an attached cover image suppressed valid music metadata")
	}
	for _, streams := range []string{"[" + audio + "," + video + "]", "[" + video + "]", "[" + picture + "]"} {
		baseline, err := parseProbe(musicMetadataProbeDocument("", streams))
		if err != nil {
			t.Fatal("parse independent video baseline")
		}
		info, err := parseProbe(musicMetadataProbeDocument(`{"title":false,"artist":["Ignored"]}`, streams))
		if err != nil || info.EmbeddedMusic != nil || !reflect.DeepEqual(info, baseline) {
			t.Fatal("music-only metadata validation changed unrelated video facts")
		}
		encoded, err := json.Marshal(info)
		if err != nil || bytes.Contains(encoded, []byte("EmbeddedMusic")) {
			t.Fatal("unrelated media cache acquired a music extraction marker")
		}
	}
}

func TestMusicMetadataSurvivesExactAudioTimingRefinement(t *testing.T) {
	info := audioTimingTestInfo("flac", "flac", 48000, "1/48000")
	info.EmbeddedMusic = &MusicMetadata{Version: 1, Title: "Persisted title", Album: "Persisted album", Artist: "Persisted artist"}
	result := audioTimingTestParse(t, info,
		audioTimingTestPacket(0, 100, 0, 48000), audioTimingTestFrame(0, 100, 0, 48000))
	if !result.AudioDurationExact || !reflect.DeepEqual(result.EmbeddedMusic, info.EmbeddedMusic) || info.AudioDurationExact {
		t.Fatal("audio timing refinement dropped music facts or changed the caller's timing snapshot")
	}
}
