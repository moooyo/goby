package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/media"
)

func TestLiveStreamOpenAcceptsExactNumericAndStringCatalogIDs(t *testing.T) {
	for _, data := range []string{
		`{"OpenToken":"opaque-open-token","ItemId":9223372036854775807,"PlaySessionId":"play_one","AudioStreamIndex":8}`,
		`{"OpenToken":"opaque-open-token","ItemId":"9223372036854775807","PlaySessionId":"play_one","AudioStreamIndex":8}`,
	} {
		request, err := parseLiveStreamRequest([]byte(data))
		if err != nil || request.ItemID != "9223372036854775807" || request.Playback.ID != request.ItemID || request.Playback.CurrentPlaySessionID != "play_one" || request.Playback.AudioStreamIndex == nil || *request.Playback.AudioStreamIndex != 8 {
			t.Fatal("pinned opening request lost identifier precision or stream selection")
		}
	}
}

func TestLiveStreamOpenRejectsAmbiguousIdentifiersAndSourceInjection(t *testing.T) {
	for _, body := range []string{
		`{"ItemId":"42"}`, `{"OpenToken":"a","opentoken":"b"}`, `{"OpenToken":"a","ItemId":1.5}`,
		`{"OpenToken":"a","ItemId":9223372036854775808}`, `{"OpenToken":"a","ItemId":null}`,
		`{"OpenToken":"a","ItemId":"42","Id":"43"}`, `{"OpenToken":"a","PlaySessionId":"one","CurrentPlaySessionId":"two"}`,
		`{"OpenToken":"a","Url":"https://untrusted.invalid/media"}`, `{"OpenToken":"a","Path":"/private/media"}`,
		`{"OpenToken":"a","RequiredHttpHeaders":{"Authorization":"secret"}}`, `{"OpenToken":"a","LiveStreamId":"foreign-live"}`,
	} {
		if _, err := parseLiveStreamRequest([]byte(body)); err == nil {
			t.Fatalf("invalid opening request was accepted: %s", body)
		}
	}
}

func TestDynamicSourceProjectionDoesNotExposeUpstreamOrSampleDuration(t *testing.T) {
	description := dynamicsource.Description{ItemID: "42", SourceID: media.SourceID("42"), OpenToken: "open_hint", Name: "Catalog source", Infinite: true}
	before := describeDynamicSourceDTO(description)
	if before["RequiresOpening"] != true || before["OpenToken"] != "open_hint" || before["SupportsDirectPlay"] != false {
		t.Fatal("unopened source advertised playable bytes")
	}
	after := dynamicSourceDTO(dynamicsource.Lease{Description: description, ID: "live_owned", PlaySessionID: "play_owned", Stamp: "private-generation"})
	for _, key := range []string{"Path", "Url", "EncoderPath", "ProbePath", "OpenToken", "RunTimeTicks", "Stamp"} {
		if _, exists := after[key]; exists {
			t.Fatalf("source projection exposed %s", key)
		}
	}
	if after["LiveStreamId"] != "live_owned" || after["Id"] != media.SourceID("42") || after["RequiresOpening"] != false || after["RequiresClosing"] != true {
		t.Fatal("opened source lost source or lease identity")
	}
	encoded, err := json.Marshal(after)
	if err != nil || strings.Contains(string(encoded), "private-generation") {
		t.Fatal("private lease stamp leaked in its public descriptor")
	}
}

func TestLiveStreamLeaseQueryRequiresOneConsistentIdentifier(t *testing.T) {
	for _, query := range []string{"", "?LiveStreamId=", "?LiveStreamId=one&livestreamid=two"} {
		if _, err := liveStreamID(httptest.NewRequest("POST", "/emby/LiveStreams/Close"+query, nil)); err == nil {
			t.Fatal("ambiguous or missing lease identifier was accepted")
		}
	}
	id, err := liveStreamID(httptest.NewRequest("POST", "/emby/LiveStreams/Close?LiveStreamId=live_owned", nil))
	if err != nil || id != "live_owned" {
		t.Fatal("owned source identifier changed")
	}
}
