package media

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func mediaEditTestDocument(t *testing.T) mediaEditDocument {
	t.Helper()
	data := `{"programs":[],"streams":[
	{"index":0,"codec_type":"video","codec_name":"h264","width":160,"height":90,"time_base":"1/1000","disposition":{"default":0,"forced":0},"extradata_size":4,"extradata_hash":"SHA256:` + strings.Repeat("a", 64) + `","tags":{"title":"Main video","ENCODER":"Original codec"}},
	{"index":7,"codec_type":"subtitle","codec_name":"subrip","time_base":"1/1000","disposition":{"default":1,"forced":0},"tags":{"language":"eng"}},
	{"index":12,"codec_type":"audio","codec_name":"aac","time_base":"1/1000","disposition":{"default":0,"forced":0},"extradata_size":2,"extradata_hash":"SHA256:` + strings.Repeat("b", 64) + `","tags":{"language":"chi","title":"Retained audio"}},
	{"index":13,"codec_type":"attachment","codec_name":"ttf","time_base":"1/90000","disposition":{"default":0,"forced":0},"extradata_size":12,"extradata_hash":"SHA256:` + strings.Repeat("c", 64) + `","tags":{"filename":"Example.ttf","mimetype":"font/ttf"}}
	],"chapters":[{"id":42,"time_base":"1/1000","start":0,"end":1000,"start_time":"0.0","end_time":"1.0","tags":{"title":"Intro"}}],
	"format":{"filename":"/proc/self/fd/3","nb_streams":4,"nb_programs":0,"format_name":"matroska,webm","format_long_name":"Matroska / WebM","start_time":"0","duration":"1","size":"4096","bit_rate":"32768","probe_score":100,"tags":{"title":"Example","creation_time":"2026-01-01T00:00:00Z","ENCODER":"Old muxer","comment":"Keep this"}}}`
	document, err := parseMediaEditDocument([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func mediaEditTestCandidate(t *testing.T, source mediaEditDocument) mediaEditDocument {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := parseMediaEditDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Streams = append(candidate.Streams[:1], candidate.Streams[2:]...)
	for index, stream := range candidate.Streams {
		stream["index"] = json.Number(strconv.Itoa(index))
	}
	candidate.Format["nb_streams"] = json.Number("3")
	candidate.Format["size"] = "3072"
	candidate.Format["tags"].(map[string]any)["ENCODER"] = "New muxer"
	candidate.Streams[1]["time_base"] = "1/48000"
	candidate.Chapters[0]["time_base"] = "1/48000"
	candidate.Chapters[0]["end"] = json.Number("48000")
	return candidate
}

func TestSubtitleRemovalMetadataProofPreservesRetainedSemantics(t *testing.T) {
	source := mediaEditTestDocument(t)
	candidate := mediaEditTestCandidate(t, source)
	digest, streams, err := compareMediaEditDocuments(source, candidate, "mkv", 7)
	if err != nil || len(digest) != 64 || len(streams) != 3 {
		t.Fatalf("metadata proof = %q, %+v, %v", digest, streams, err)
	}
	if streams[1].SourceIndex != 12 || streams[1].CandidateIndex != 1 || streams[2].ExtradataSHA256 != strings.Repeat("c", 64) {
		t.Fatalf("wrong retained stream or attachment proof: %+v", streams)
	}
	changes, err := mediaEditWriterChanges(source, candidate, map[string]string{"MuxingApp": "Old muxer", "WritingApp": "Original application"}, map[string]string{"MuxingApp": "New muxer", "WritingApp": "New application"})
	if err != nil || len(changes) != 3 || changes[0].Field != "GlobalEncoder" || changes[0].Before != "Old muxer" || changes[0].After != "New muxer" || !changes[0].BeforePresent || !changes[0].AfterPresent {
		t.Fatalf("writer provenance not explicitly recorded: %+v, %v", changes, err)
	}
}

func TestSubtitleRemovalMetadataProofRejectsLostFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*mediaEditDocument)
	}{
		{"global title", func(d *mediaEditDocument) { d.Format["tags"].(map[string]any)["title"] = "Changed" }},
		{"creation time", func(d *mediaEditDocument) { delete(d.Format["tags"].(map[string]any), "creation_time") }},
		{"comment", func(d *mediaEditDocument) { delete(d.Format["tags"].(map[string]any), "comment") }},
		{"stream encoder", func(d *mediaEditDocument) { d.Streams[0]["tags"].(map[string]any)["ENCODER"] = "Changed" }},
		{"language", func(d *mediaEditDocument) { d.Streams[1]["tags"].(map[string]any)["language"] = "eng" }},
		{"disposition", func(d *mediaEditDocument) { d.Streams[1]["disposition"].(map[string]any)["default"] = json.Number("1") }},
		{"attachment bytes", func(d *mediaEditDocument) { d.Streams[2]["extradata_hash"] = "SHA256:" + strings.Repeat("d", 64) }},
		{"missing attachment hash", func(d *mediaEditDocument) { delete(d.Streams[2], "extradata_hash") }},
		{"codec header", func(d *mediaEditDocument) { d.Streams[0]["extradata_size"] = json.Number("9") }},
		{"chapter time", func(d *mediaEditDocument) { d.Chapters[0]["end"] = json.Number("48001") }},
		{"chapter title", func(d *mediaEditDocument) { d.Chapters[0]["tags"].(map[string]any)["title"] = "Changed" }},
		{"additional track", func(d *mediaEditDocument) { d.Streams = append(d.Streams, d.Streams[0]) }},
		{"unknown stream fact", func(d *mediaEditDocument) { d.Streams[0]["new_codec_fact"] = "Changed" }},
		{"unknown side data", func(d *mediaEditDocument) {
			d.Streams[0]["side_data_list"] = []any{map[string]any{"side_data_type": "New Extradata"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := mediaEditTestDocument(t)
			candidate := mediaEditTestCandidate(t, source)
			test.mutate(&candidate)
			if _, _, err := compareMediaEditDocuments(source, candidate, "mkv", 7); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("lost fact was accepted: %v", err)
			}
		})
	}
}

func TestSubtitleRemovalAdmissionAndAbsoluteMapping(t *testing.T) {
	source := mediaEditTestDocument(t)
	for _, profile := range []struct {
		container string
		index     int
	}{
		{"mkv", 12}, {"mkv", 1}, {"mka", 7}, {"mp4", 7},
	} {
		if err := source.admit(profile.container, profile.index); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
			t.Fatalf("unsupported selection admitted: %+v, %v", profile, err)
		}
	}
	args, err := buildMediaEditRemuxArgs(source, SubtitleRemovalOptions{Container: "mkv", StreamIndex: 7})
	if err != nil {
		t.Fatal(err)
	}
	var maps []string
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "-map" {
			maps = append(maps, args[index+1])
		}
	}
	if !reflect.DeepEqual(maps, []string{"0", "-0:7"}) || args[len(args)-1] != "/proc/self/fd/4" {
		t.Fatalf("remux did not remove exactly one absolute stream: %v", args)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{"-map_metadata 0", "-map_chapters 0", "-c copy", "-copyts", "-default_mode passthrough", "-disposition:0 0", "-disposition:1 0", "-disposition:2 0", "-map_metadata:s:1 0:s:12", "-i /proc/self/fd/3"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing preservation argument %q: %v", expected, args)
		}
	}
}

func TestSubtitleRemovalMetadataRejectsAmbiguousJSONAndOpaqueSideData(t *testing.T) {
	data, err := json.Marshal(mediaEditTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := strings.Replace(string(data), `"index":0`, `"index":0,"index":1`, 1)
	if _, err := parseMediaEditDocument([]byte(duplicate)); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("duplicate metadata key accepted: %v", err)
	}
	if _, err := mediaEditTags(map[string]any{"TITLE": "First", "title": "Second"}); err == nil {
		t.Fatal("case-ambiguous metadata tags accepted")
	}
	if err := mediaEditValidateStreamSideData([]any{map[string]any{"side_data_type": "Display Matrix", "rotation": json.Number("90")}}); err == nil {
		t.Fatal("incomplete display matrix accepted")
	}
	if err := mediaEditValidateStreamSideData([]any{map[string]any{"side_data_type": "Content light level metadata", "max_content": json.Number("1000"), "max_average": json.Number("400")}}); err != nil {
		t.Fatalf("complete rendered HDR metadata rejected: %v", err)
	}
	dovi := map[string]any{"side_data_type": "DOVI configuration record", "dv_version_major": json.Number("1"), "dv_version_minor": json.Number("0"),
		"dv_profile": json.Number("8"), "dv_level": json.Number("6"), "rpu_present_flag": json.Number("1"), "el_present_flag": json.Number("0"), "bl_present_flag": json.Number("1"),
		"dv_bl_signal_compatibility_id": json.Number("1"), "dv_md_compression": "none"}
	if err := mediaEditValidateStreamSideData([]any{dovi}); err != nil {
		t.Fatalf("complete Dolby Vision compression metadata rejected: %v", err)
	}
	dovi["dv_md_compression"] = "unproven"
	if err := mediaEditValidateStreamSideData([]any{dovi}); err == nil {
		t.Fatal("unknown Dolby Vision compression accepted")
	}
}

func TestSubtitleRemovalDigestBorrowsDescriptorAndChecksLength(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "candidate-*.mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("complete retained bytes"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := mediaEditFileDigest(context.Background(), file, info.Size())
	if err != nil || len(digest) != 64 {
		t.Fatalf("digest = %q, %v", digest, err)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 3 {
		t.Fatalf("borrowed offset moved: %d, %v", position, err)
	}
	if _, err := mediaEditFileDigest(context.Background(), file, info.Size()+1); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("short source accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mediaEditFileDigest(ctx, file, info.Size()); !errors.Is(err, context.Canceled) {
		t.Fatalf("hash did not honor cancellation: %v", err)
	}
	if _, err := file.WriteAt([]byte("changed"), info.Size()); err != nil {
		t.Fatal(err)
	}
	if err := mediaEditCheckUnchanged(file, info); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("changed source accepted: %v", err)
	}
}
