package backuppg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func bitmapSubtitleArchiveFixture(t *testing.T, paired bool) (library.BitmapSubtitle, string, []byte) {
	t.Helper()
	track := library.BitmapSubtitle{Index: 4, Codec: "hdmv_pgs_subtitle", Format: "sup", Language: "eng", Title: "English",
		Filename: "Film.en.sup", IsDefault: true, Components: []library.BitmapSubtitleComponent{{
			Name: "Film.en.sup", Identity: "retained-sup", SHA256: strings.Repeat("a", 64), Size: 1024,
			ModifiedNS: 9007199254740993, ChangeTimeNS: 9007199254740995,
		}}}
	if paired {
		track.Format, track.Codec, track.Filename = "vobsub", "dvd_subtitle", "Film.multi.idx"
		track.Components[0].Name, track.Components[0].Identity = "Film.multi.idx", "retained-idx"
		track.Components = append(track.Components, library.BitmapSubtitleComponent{
			Name: "Film.multi.sub", Identity: "retained-sub", SHA256: strings.Repeat("b", 64), Size: 65536,
			ModifiedNS: 9007199254740997, ChangeTimeNS: 9007199254740999,
		})
	}
	track.Tag = library.BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components)
	raw, err := json.Marshal(track.Components)
	if err != nil {
		t.Fatal(err)
	}
	return track, "nested/" + track.Filename, raw
}

func TestBitmapSubtitleArchiveRejectsMalformedAndReinterpretedEvidence(t *testing.T) {
	for _, paired := range []bool{false, true} {
		track, relative, raw := bitmapSubtitleArchiveFixture(t, paired)
		if !validBitmapSubtitleArchiveTrack(track, relative, raw) {
			t.Fatalf("valid retained %s evidence was rejected", track.Format)
		}
		components, valid := decodeBitmapSubtitleArchiveComponents(raw)
		if !valid || components[0].ModifiedNS != 9007199254740993 || components[0].ChangeTimeNS != 9007199254740995 {
			t.Fatal("bitmap component snapshot lost exact bigint timestamps")
		}
	}
	for _, test := range []struct {
		name string
		edit func(*library.BitmapSubtitle, *string, *[]byte)
	}{
		{"unknown_field", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) {
			*raw = []byte(strings.Replace(string(*raw), `"Name":`, `"Unknown":true,"Name":`, 1))
		}},
		{"mis_cased_field", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) {
			*raw = []byte(strings.Replace(string(*raw), `"Name":`, `"name":`, 1))
		}},
		{"missing_field", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) {
			*raw = []byte(strings.Replace(string(*raw), `"Size":1024,`, ``, 1))
		}},
		{"null_field", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) {
			*raw = []byte(strings.Replace(string(*raw), `"Size":1024`, `"Size":null`, 1))
		}},
		{"fractional_stamp", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) {
			*raw = []byte(strings.Replace(string(*raw), `9007199254740993`, `1.5`, 1))
		}},
		{"quoted_stamp", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) {
			*raw = []byte(strings.Replace(string(*raw), `9007199254740993`, `"9007199254740993"`, 1))
		}},
		{"component_path", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) {
			*raw = []byte(strings.Replace(string(*raw), `Film.en.sup`, `../Film.en.sup`, 1))
		}},
		{"component_mismatch", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) {
			*raw = []byte(strings.Replace(string(*raw), `Film.en.sup`, `Another.sup`, 1))
		}},
		{"absolute_path", func(_ *library.BitmapSubtitle, relative *string, _ *[]byte) { *relative = "/nested/Film.en.sup" }},
		{"parent_path", func(_ *library.BitmapSubtitle, relative *string, _ *[]byte) { *relative = "../Film.en.sup" }},
		{"internal_parent_path", func(_ *library.BitmapSubtitle, relative *string, _ *[]byte) { *relative = "nested/../Film.en.sup" }},
		{"noncanonical_path", func(_ *library.BitmapSubtitle, relative *string, _ *[]byte) { *relative = "nested//Film.en.sup" }},
		{"dot_path", func(_ *library.BitmapSubtitle, relative *string, _ *[]byte) { *relative = "./Film.en.sup" }},
		{"backslash_path", func(_ *library.BitmapSubtitle, relative *string, _ *[]byte) { *relative = `nested\Film.en.sup` }},
		{"nul_path", func(_ *library.BitmapSubtitle, relative *string, _ *[]byte) { *relative = "nested/\x00Film.en.sup" }},
		{"oversized_path", func(_ *library.BitmapSubtitle, relative *string, _ *[]byte) {
			*relative = strings.Repeat("a", 4096) + "/Film.en.sup"
		}},
		{"invalid_index", func(track *library.BitmapSubtitle, _ *string, _ *[]byte) { track.Index = -1 }},
		{"invalid_sup_stream", func(track *library.BitmapSubtitle, _ *string, _ *[]byte) { track.SourceStreamIndex = 1 }},
		{"codec_mismatch", func(track *library.BitmapSubtitle, _ *string, _ *[]byte) { track.Codec = "dvd_subtitle" }},
		{"wrong_source_hash", func(track *library.BitmapSubtitle, _ *string, _ *[]byte) { track.Tag = strings.Repeat("0", 64) }},
		{"long_language", func(track *library.BitmapSubtitle, _ *string, _ *[]byte) { track.Language = strings.Repeat("a", 33) }},
		{"long_title", func(track *library.BitmapSubtitle, _ *string, _ *[]byte) { track.Title = strings.Repeat("a", 513) }},
		{"empty_components", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) { *raw = []byte(`[]`) }},
		{"null_components", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) { *raw = []byte(`null`) }},
		{"non_array", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) { *raw = []byte(`{}`) }},
		{"trailing_value", func(_ *library.BitmapSubtitle, _ *string, raw *[]byte) { *raw = append(*raw, []byte(` {}`)...) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			track, relative, raw := bitmapSubtitleArchiveFixture(t, false)
			test.edit(&track, &relative, &raw)
			if validBitmapSubtitleArchiveTrack(track, relative, raw) {
				t.Fatal("malformed or reinterpreted bitmap subtitle evidence was accepted")
			}
		})
	}
	track, relative, _ := bitmapSubtitleArchiveFixture(t, true)
	track.Components[0], track.Components[1] = track.Components[1], track.Components[0]
	track.Tag = library.BitmapSubtitleSourceHash(track.Format, track.SourceStreamIndex, track.Components)
	raw, _ := json.Marshal(track.Components)
	if validBitmapSubtitleArchiveTrack(track, relative, raw) {
		t.Fatal("a correctly hashed but reordered IDX/SUB pair was accepted")
	}
}

func TestValidateBitmapSubtitleStatePreservesVersionAndContext(t *testing.T) {
	for _, version := range []int64{23, 59, 60, 61} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := validateBitmapSubtitleState(ctx, nil, version); !errors.Is(err, context.Canceled) {
			t.Fatalf("schema %d lost cancellation: %v", version, err)
		}
		want := error(nil)
		if version >= 61 {
			want = ErrDatabase
		}
		if err := validateBitmapSubtitleState(context.Background(), nil, version); !errors.Is(err, want) {
			t.Fatalf("schema %d crossed its migration boundary: %v", version, err)
		}
	}
	for _, test := range []struct {
		name   string
		err    error
		cancel bool
		want   error
	}{
		{name: "invalid_relations", want: ErrSchema},
		{name: "read_failure", err: errors.New("read failed"), want: ErrDatabase},
		{name: "driver_deadline", err: fmt.Errorf("driver: %w", context.DeadlineExceeded), want: context.DeadlineExceeded},
		{name: "cancelled_invalid_read", cancel: true, want: context.Canceled},
		{name: "cancelled_failed_read", cancel: true, err: errors.New("read interrupted"), want: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			row := themeStateTestRow{err: test.err}
			if test.cancel {
				row.onScan = cancel
			}
			tx := &themeStateTestTx{row: row}
			if err := validateBitmapSubtitleState(ctx, tx, 61); !errors.Is(err, test.want) {
				t.Fatalf("bitmap subtitle recovery result = %v, want %v", err, test.want)
			}
			if tx.queries != 1 {
				t.Fatal("invalid relationships should stop further inspection")
			}
		})
	}
}
