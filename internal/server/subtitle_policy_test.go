package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/settings"
)

func subtitlePolicyTestItem() library.Item {
	return library.Item{
		ID: "movie", Name: "Movie", Type: "Movie", Path: "/media/movie.mkv",
		Media: &media.Info{Container: "matroska", Streams: []media.Stream{
			{Index: 0, CodecType: "video", Codec: "h264"},
			{Index: 5, CodecType: "subtitle", Codec: "ass", IsTextSubtitleStream: true, Language: "jpn", IsDefault: true, IsForced: true},
			{Index: 8, CodecType: "attachment", Codec: "ttf", Filename: "retained.ttf", MIMEType: "font/ttf"},
		}},
		Subtitles: []library.Subtitle{
			{Index: 65536, Codec: "srt", Language: "eng", Filename: "movie.eng.srt"},
			{Index: 65537, Codec: "vtt", Language: "fra", Owned: true, Tag: strings.Repeat("a", 64)},
		},
		BitmapSubtitles: []library.BitmapSubtitle{
			{Index: 65538, SourceStreamIndex: 0, Codec: "hdmv_pgs_subtitle", Format: "sup", Language: "zho", Filename: "movie.zho.sup", Tag: strings.Repeat("b", 64)},
		},
	}
}

func subtitlePolicyTestMetadata(stream map[string]any) map[string]any {
	metadata := make(map[string]any, len(stream))
	for key, value := range stream {
		if key != "DeliveryUrl" && key != "DeliveryMethod" && key != "SupportsExternalStream" {
			metadata[key] = value
		}
	}
	return metadata
}

func subtitlePolicyTestURL(t *testing.T, stream map[string]any, token, suffix string) *url.URL {
	t.Helper()
	raw, ok := stream["DeliveryUrl"].(string)
	if !ok {
		t.Fatalf("expected a delivery URL for stream %v", stream["Index"])
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Query().Get("api_key") != token || !strings.HasSuffix(parsed.Path, suffix) {
		t.Fatalf("stream %v has an incorrect delivery URL: %q", stream["Index"], raw)
	}
	return parsed
}

func assertSubtitlePolicyDTO(t *testing.T, dto map[string]any, item library.Item, policy settings.TranscodingPolicy, token, embeddedFormat string) {
	t.Helper()
	streams, ok := dto["MediaStreams"].([]map[string]any)
	if !ok {
		t.Fatal("the policy projection lost its media stream inventory")
	}
	original := itemMediaStreamsDTO(item)
	if len(streams) != len(original) {
		t.Fatalf("policy changed the inventory size: got %d, want %d", len(streams), len(original))
	}
	indexed := make(map[int]map[string]any, len(streams))
	for position, stream := range streams {
		if !reflect.DeepEqual(subtitlePolicyTestMetadata(stream), subtitlePolicyTestMetadata(original[position])) {
			t.Fatalf("policy changed indexed source metadata: got %+v, want %+v", stream, original[position])
		}
		index, valid := stream["Index"].(int)
		if !valid || indexed[index] != nil {
			t.Fatal("policy changed or duplicated a public stream index")
		}
		indexed[index] = stream
	}
	embedded := indexed[5]
	if embedded["IsExternal"] != false {
		t.Fatal("an embedded subtitle changed its source identity")
	}
	if policy.EnableSubtitleExtraction {
		if embedded["SupportsExternalStream"] != true || embedded["DeliveryMethod"] != "External" {
			t.Fatal("enabled subtitle extraction lost its external delivery claim")
		}
		subtitlePolicyTestURL(t, embedded, token, "/Subtitles/5/0/Stream."+embeddedFormat)
	} else {
		if embedded["SupportsExternalStream"] != false || embedded["DeliveryMethod"] == "External" {
			t.Fatal("disabled subtitle extraction retained an external delivery claim")
		}
		if _, exists := embedded["DeliveryUrl"]; exists {
			t.Fatal("disabled embedded extraction retained a delivery URL")
		}
	}
	attachment := indexed[8]
	if attachment["Filename"] != "retained.ttf" || attachment["MimeType"] != "font/ttf" {
		t.Fatal("attachment policy removed font metadata")
	}
	if policy.EnableOnTheFlyAttachmentExtraction {
		subtitlePolicyTestURL(t, attachment, token, "/Attachments/8/Stream")
	} else if _, exists := attachment["DeliveryUrl"]; exists {
		t.Fatal("disabled attachment extraction retained a delivery URL")
	}
	for _, index := range []int{65536, 65537} {
		stream := indexed[index]
		if stream["IsExternal"] != true || stream["SupportsExternalStream"] != true || stream["DeliveryMethod"] != "External" {
			t.Fatalf("embedded extraction policy changed external text stream %d", index)
		}
	}
	subtitlePolicyTestURL(t, indexed[65536], token, "/Subtitles/65536/0/Stream.srt")
	owned := subtitlePolicyTestURL(t, indexed[65537], token, "/Subtitles/65537/0/Stream.vtt")
	if owned.Query().Get("GobySubtitleTag") != item.Subtitles[1].Tag || indexed[65537]["Protocol"] != "Http" {
		t.Fatal("policy or credential projection changed an owned-caption URL identity")
	}
	if _, exists := indexed[65537]["Path"]; exists {
		t.Fatal("an owned caption gained a sidecar pathname")
	}
	bitmap := indexed[65538]
	if bitmap["IsExternal"] != true || bitmap["SupportsExternalStream"] != false || bitmap["DeliveryMethod"] != "Encode" {
		t.Fatal("text extraction policy changed bitmap encoded delivery")
	}
	if _, exists := bitmap["DeliveryUrl"]; exists {
		t.Fatal("text extraction policy fabricated a bitmap download URL")
	}
	if sources, ok := dto["MediaSources"].([]map[string]any); ok {
		for _, source := range sources {
			assertSubtitlePolicyDTO(t, source, item, policy, token, embeddedFormat)
		}
	}
}

func TestSubtitleDeliveryPolicyIndependentlyControlsNestedDownloadClaims(t *testing.T) {
	for _, test := range []struct {
		name                 string
		subtitle, attachment bool
	}{
		{"both enabled", true, true},
		{"subtitle extraction disabled", false, true},
		{"attachment extraction disabled", true, false},
		{"both disabled", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := subtitlePolicyTestItem()
			policy := settings.DefaultTranscodingPolicy()
			policy.EnableSubtitleExtraction = test.subtitle
			policy.EnableOnTheFlyAttachmentExtraction = test.attachment
			direct := map[string]any{"MediaStreams": itemMediaStreamsDTO(item, policy)}
			assertSubtitlePolicyDTO(t, direct, item, policy, "", "ass")

			dto := map[string]any{
				"MediaStreams": itemMediaStreamsDTO(item),
				"MediaSources": []map[string]any{{
					"MediaStreams": itemMediaStreamsDTO(item),
					"MediaSources": []map[string]any{{"MediaStreams": itemMediaStreamsDTO(item)}},
				}},
			}
			addSubtitleDeliveryCredentials(dto, item.ID, "captured-token", map[int]string{5: "vtt"}, policy)
			assertSubtitlePolicyDTO(t, dto, item, policy, "captured-token", "vtt")
		})
	}
}

func TestSubtitlePresentationUsesCapturedRequestPolicyForAllSourceDTOs(t *testing.T) {
	s := &Server{}
	item := subtitlePolicyTestItem()
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "captured enabled", false: "captured disabled"}[enabled], func(t *testing.T) {
			captured := settings.DefaultTranscodingPolicy()
			captured.EnableSubtitleExtraction = enabled
			captured.EnableOnTheFlyAttachmentExtraction = enabled
			captured.DownMixAudioBoost = 2
			snapshot := requestSettingsSnapshot{Revision: 7, Transcoding: captured}
			r := httptest.NewRequest(http.MethodGet, "/emby/Items/movie?Fields=MediaStreams,MediaSources&api_key=captured-token", nil)
			r = r.WithContext(context.WithValue(r.Context(), settingsSnapshotContextKey{}, snapshot))
			snapshot.Transcoding.EnableSubtitleExtraction = !enabled
			snapshot.Transcoding.EnableOnTheFlyAttachmentExtraction = !enabled
			presentation := s.readItemPresentation(r)
			if presentation.transcoding != captured {
				t.Fatal("item presentation lost the detached request policy")
			}
			dto := s.itemDTOWithToken(item, presentation.fields, true, presentation.deliveryToken, presentation.transcoding)
			assertSubtitlePolicyDTO(t, dto, item, captured, "captured-token", "ass")
			assertSubtitlePolicyDTO(t, s.itemDTOForRequest(r, item, nil, true), item, captured, "captured-token", "ass")
			assertSubtitlePolicyDTO(t, s.itemDTOWithToken(item, nil, true, "default-token"), item, settings.DefaultTranscodingPolicy(), "default-token", "ass")
		})
	}
}

func TestDisabledSubtitleAttachmentExtractionRejectsBeforeOpeningMedia(t *testing.T) {
	policy := settings.DefaultTranscodingPolicy()
	policy.EnableOnTheFlyAttachmentExtraction = false
	s := &Server{subtitleSlots: make(chan struct{}, 1)}
	r := httptest.NewRequest(http.MethodGet, "/emby/Videos/movie/source/Attachments/8/Stream", nil)
	r.SetPathValue("Id", "movie")
	r.SetPathValue("MediaSourceId", "source")
	r.SetPathValue("Index", "8")
	ctx := context.WithValue(r.Context(), settingsSnapshotContextKey{}, requestSettingsSnapshot{Transcoding: policy})
	ctx = context.WithValue(ctx, principalKey, identity.Principal{})
	r = r.WithContext(ctx)
	response := httptest.NewRecorder()
	s.subtitleAttachment(response, r)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled attachment extraction returned %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		ResponseStatus struct {
			ErrorCode string
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.ResponseStatus.ErrorCode != "subtitle_attachment_extraction_disabled" {
		t.Fatalf("disabled attachment extraction returned an unexpected error: %s", response.Body.String())
	}
	if len(s.subtitleSlots) != 0 {
		t.Fatal("disabled attachment extraction retained a request slot")
	}
}
