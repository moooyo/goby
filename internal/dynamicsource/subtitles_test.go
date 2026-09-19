package dynamicsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func testSubtitleDefinition() SubtitleDefinition {
	return SubtitleDefinition{ID: "english", Name: "English", Language: "eng", Format: "webvtt", Mode: "webvtt-hls",
		URL: "https://captions.example/private-token/index.m3u8", Headers: map[string]string{"Authorization": "Bearer private-value"},
		Clock: "mpegts", SegmentClock: "timestamp-map", OffsetTicks: media.TicksPerSecond / 2, Default: true}
}

func TestSubtitleDeclarationProjectionStaysPrivateAndStableAcrossReconnect(t *testing.T) {
	definition := Definition{ItemID: "42", URL: "https://example.invalid/source", Infinite: true, MaxReconnects: 2,
		Subtitles: []SubtitleDefinition{testSubtitleDefinition()}}
	manager, err := New(context.Background(), []Definition{definition}, Options{
		Authorize: func(context.Context, Owner, string, string) error { return nil },
		Connector: connectorFunc(func(_ context.Context, declaration Definition) (*Connection, error) {
			// Connector arguments cannot mutate the retained startup declaration.
			declaration.Subtitles[0].Headers["Authorization"] = "connector mutation"
			return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	definition.Subtitles[0].URL = "https://caller-mutation.invalid/"
	definition.Subtitles[0].Headers["Authorization"] = "caller mutation"
	first := testOpen(t, manager, testOwner(), "subtitles")
	stream := first.Info.Streams[len(first.Info.Streams)-1]
	if stream.Index != ExternalSubtitleIndexBase || stream.CodecType != "subtitle" || stream.Codec != "webvtt" ||
		!stream.IsExternal || !stream.IsTextSubtitleStream || !stream.IsDefault || stream.Language != "eng" || stream.Title != "English" || len(stream.SubtitleTag) != 64 {
		t.Fatal("configured subtitle did not become a bound text track")
	}
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-token", "private-value", "captions.example", "Authorization", "caller-mutation"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("public lease disclosed a private subtitle request")
		}
	}
	resolved, err := manager.Subtitle(context.Background(), testOwner(), first.ID, first.Generation, stream.Index, stream.SubtitleTag)
	if err != nil || resolved.URL != testSubtitleDefinition().URL || resolved.Headers["Authorization"] != "Bearer private-value" {
		t.Fatal("authorized subtitle lookup lost its private declaration", err)
	}
	for _, output := range []string{fmt.Sprint(resolved), fmt.Sprintf("%+v", resolved), fmt.Sprintf("%#v", resolved)} {
		if strings.Contains(output, "private-value") || strings.Contains(output, "captions.example") {
			t.Fatal("formatting disclosed subtitle secrets")
		}
	}
	resolved.Headers["Authorization"] = "lookup mutation"
	input, err := manager.Acquire(context.Background(), testOwner(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	reconnected, err := manager.Acquire(context.Background(), testOwner(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	current := reconnected.Info.Streams[len(reconnected.Info.Streams)-1]
	if reconnected.Generation != first.Generation+1 || reconnected.Stamp == first.Stamp || current.SubtitleTag != stream.SubtitleTag || current.Index != stream.Index {
		t.Fatal("reconnect changed the fixed track identity or failed to fence source bytes")
	}
	if _, err := manager.Subtitle(context.Background(), testOwner(), first.ID, first.Generation, stream.Index, stream.SubtitleTag); !errors.Is(err, ErrStaleGeneration) {
		t.Fatal("old generation retrieved a current subtitle declaration")
	}
	resolved, err = manager.Subtitle(context.Background(), testOwner(), first.ID, reconnected.Generation, current.Index, current.SubtitleTag)
	if err != nil || resolved.Headers["Authorization"] != "Bearer private-value" {
		t.Fatal("lookup caller mutated the retained headers", err)
	}
	other := testOpen(t, manager, testOwner(), "another_presentation")
	otherStream := other.Info.Streams[len(other.Info.Streams)-1]
	if otherStream.SubtitleTag == stream.SubtitleTag {
		t.Fatal("different leases shared subtitle capabilities")
	}
}

func TestSubtitleLookupRechecksOwnerPolicyGenerationAndBinding(t *testing.T) {
	var revoked atomic.Bool
	denied := errors.New("access revoked")
	manager, err := New(context.Background(), []Definition{{ItemID: "42", URL: "https://example.invalid/source", Subtitles: []SubtitleDefinition{testSubtitleDefinition()}}}, Options{
		Authorize: func(context.Context, Owner, string, string) error {
			if revoked.Load() {
				return denied
			}
			return nil
		},
		Connector: connectorFunc(func(context.Context, Definition) (*Connection, error) {
			return &Connection{Reader: io.NopCloser(strings.NewReader("stream")), Info: testFacts()}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	lease := testOpen(t, manager, testOwner(), "authorize_subtitles")
	stream := lease.Info.Streams[len(lease.Info.Streams)-1]
	foreign := testOwner()
	foreign.SessionID = "another_credential"
	for _, request := range []struct {
		owner      Owner
		generation uint64
		index      int
		tag        string
		want       error
	}{
		{foreign, lease.Generation, stream.Index, stream.SubtitleTag, ErrNotFound},
		{testOwner(), 0, stream.Index, stream.SubtitleTag, ErrStaleGeneration},
		{testOwner(), lease.Generation + 1, stream.Index, stream.SubtitleTag, ErrStaleGeneration},
		{testOwner(), lease.Generation, stream.Index + 1, stream.SubtitleTag, ErrNotFound},
		{testOwner(), lease.Generation, 3, stream.SubtitleTag, ErrNotFound},
		{testOwner(), lease.Generation, stream.Index, strings.Repeat("0", 64), ErrNotFound},
	} {
		if _, err := manager.Subtitle(context.Background(), request.owner, lease.ID, request.generation, request.index, request.tag); !errors.Is(err, request.want) {
			t.Fatalf("subtitle binding error = %v; want %v", err, request.want)
		}
	}
	revoked.Store(true)
	if _, err := manager.Subtitle(context.Background(), testOwner(), lease.ID, lease.Generation, stream.Index, stream.SubtitleTag); !errors.Is(err, denied) {
		t.Fatal("subtitle lookup ignored current catalog authorization")
	}
	if _, err := manager.Info(context.Background(), testOwner(), lease.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("revocation left the subtitle lease active")
	}
}

func TestSubtitleDeclarationValidationAndDescriptorIdentity(t *testing.T) {
	valid := testSubtitleDefinition()
	if err := ValidateSubtitleDefinitions([]SubtitleDefinition{valid}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*SubtitleDefinition){
		func(s *SubtitleDefinition) { s.ID = "" },
		func(s *SubtitleDefinition) { s.Name = "bad\nname" },
		func(s *SubtitleDefinition) { s.Language = strings.Repeat("x", 65) },
		func(s *SubtitleDefinition) { s.Format = "pgs" },
		func(s *SubtitleDefinition) { s.Mode = "automatic" },
		func(s *SubtitleDefinition) { s.Clock = "" },
		func(s *SubtitleDefinition) { s.Clock = "wallclock" },
		func(s *SubtitleDefinition) { s.SegmentClock = "" },
		func(s *SubtitleDefinition) { s.SegmentClock = "automatic" },
		func(s *SubtitleDefinition) { s.StreamWatermarks = "goby-note-v1" },
		func(s *SubtitleDefinition) { s.Format = "ass"; s.Mode = "document" },
		func(s *SubtitleDefinition) { s.Format = "subrip"; s.Clock = "media" },
		func(s *SubtitleDefinition) { s.URL = "file:///tmp/captions.vtt" },
		func(s *SubtitleDefinition) { s.URL = "https://user:secret@example.invalid/captions.vtt" },
		func(s *SubtitleDefinition) { s.Headers = map[string]string{"Range": "bytes=0-"} },
		func(s *SubtitleDefinition) { s.Headers = map[string]string{"X-Token": "injected\r\nHeader: secret"} },
		func(s *SubtitleDefinition) { s.OffsetTicks = 24*60*60*media.TicksPerSecond + 1 },
	} {
		invalid := valid
		change(&invalid)
		if err := ValidateSubtitleDefinitions([]SubtitleDefinition{invalid}); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid subtitle declaration accepted")
		}
	}
	if err := ValidateSubtitleDefinitions([]SubtitleDefinition{valid, valid}); !errors.Is(err, ErrInvalid) {
		t.Fatal("duplicate subtitle identity accepted")
	}
	second := valid
	second.ID = "second"
	if err := ValidateSubtitleDefinitions([]SubtitleDefinition{valid, second}); !errors.Is(err, ErrInvalid) {
		t.Fatal("multiple default subtitles accepted")
	}
	tooMany := make([]SubtitleDefinition, 9)
	if err := ValidateSubtitleDefinitions(tooMany); !errors.Is(err, ErrInvalid) {
		t.Fatal("unbounded subtitle track list accepted")
	}
	for _, format := range []string{"webvtt", "subrip", "ass"} {
		document := valid
		document.Format, document.Mode, document.Clock = format, "document", "media"
		document.SegmentClock = ""
		if err := ValidateSubtitleDefinitions([]SubtitleDefinition{document}); err != nil {
			t.Fatal("valid finite text declaration rejected", err)
		}
	}
	base := subtitleTag("lease", valid)
	for _, change := range []func(*SubtitleDefinition){
		func(s *SubtitleDefinition) { s.ID = "changed" },
		func(s *SubtitleDefinition) { s.URL += "?generation=changed" },
		func(s *SubtitleDefinition) { s.Headers = map[string]string{"Authorization": "rotated"} },
		func(s *SubtitleDefinition) { s.OffsetTicks++ },
		func(s *SubtitleDefinition) { s.Clock = "media" },
		func(s *SubtitleDefinition) { s.SegmentClock = "changed" },
	} {
		changed := valid
		change(&changed)
		if subtitleTag("lease", changed) == base {
			t.Fatal("subtitle identity did not bind its request and timing declaration")
		}
	}
}

func TestSubtitleDefinitionsRequireDeclaredCompletenessContracts(t *testing.T) {
	stream := testSubtitleDefinition()
	stream.Mode, stream.Clock, stream.SegmentClock, stream.StreamWatermarks = "webvtt-stream", "media", "", "goby-note-v1"
	if err := ValidateSubtitleDefinitions([]SubtitleDefinition{stream}); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*SubtitleDefinition){
		func(s *SubtitleDefinition) { s.StreamWatermarks = "" },
		func(s *SubtitleDefinition) { s.StreamWatermarks = "guess" },
		func(s *SubtitleDefinition) { s.SegmentClock = "timestamp-map" },
		func(s *SubtitleDefinition) { s.Mode = "document" },
	} {
		invalid := stream
		mutate(&invalid)
		if err := ValidateSubtitleDefinitions([]SubtitleDefinition{invalid}); !errors.Is(err, ErrInvalid) {
			t.Fatal("an incomplete or irrelevant watermark contract was accepted")
		}
	}
	plain := testSubtitleDefinition()
	plain.Mode, plain.Clock = "document", "media"
	if err := ValidateSubtitleDefinitions([]SubtitleDefinition{plain}); !errors.Is(err, ErrInvalid) {
		t.Fatal("a finite document retained an irrelevant segment clock declaration")
	}
	plain.SegmentClock = ""
	if err := ValidateSubtitleDefinitions([]SubtitleDefinition{plain}); err != nil {
		t.Fatal(err)
	}
	base := subtitleTag("lease", stream)
	stream.StreamWatermarks = "changed"
	if subtitleTag("lease", stream) == base {
		t.Fatal("declaration identity did not bind its completeness contract")
	}
}

func TestConnectorCannotSupplyReservedExternalSubtitleFacts(t *testing.T) {
	for _, mode := range []string{"reserved_index", "external_flag", "tag"} {
		t.Run(mode, func(t *testing.T) {
			manager, err := New(context.Background(), []Definition{{ItemID: "42", URL: "https://example.invalid/source"}}, Options{
				Authorize: func(context.Context, Owner, string, string) error { return nil },
				Connector: connectorFunc(func(context.Context, Definition) (*Connection, error) {
					info := testFacts()
					switch mode {
					case "reserved_index":
						info.Streams[0].Index = ExternalSubtitleIndexBase
					case "external_flag":
						info.Streams[0].IsExternal = true
					case "tag":
						info.Streams[0].SubtitleTag = strings.Repeat("1", 64)
					}
					return &Connection{Reader: io.NopCloser(strings.NewReader("bytes")), Info: info}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Close(context.Background())
			description, err := manager.Describe(context.Background(), testOwner(), "42", "")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := manager.Open(ctx, testOwner(), OpenRequest{OpenToken: description.OpenToken, PlaySessionID: "reserved"}); !errors.Is(err, ErrUnavailable) {
				t.Fatal("connector facts impersonated configured sidecar ownership")
			}
		})
	}
}
