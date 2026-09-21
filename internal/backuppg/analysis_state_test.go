package backuppg

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
)

func TestAnalysisArchiveGatePreservesHistoricalSchemaAndCancellation(t *testing.T) {
	for _, version := range []int64{44, 48, 49, 50, 51} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := validateAnalysisState(ctx, nil, version); !errors.Is(err, context.Canceled) {
			t.Fatal("analysis archive gate lost cancellation precedence")
		}
		want := error(nil)
		if version >= 50 {
			want = ErrDatabase
		}
		if err := validateAnalysisState(context.Background(), nil, version); !errors.Is(err, want) {
			t.Fatalf("schema %d gate = %v, want %v", version, err, want)
		}
	}
}

func TestAnalysisArchiveSelectionIsTypedBoundedCanonicalAndClosed(t *testing.T) {
	for _, raw := range []string{`{}`, `{"LibraryIds":["a","b"],"ItemIds":["item"],"Force":true}`, `{"LibraryIds":[],"Force":false}`} {
		value, ok := decodeAnalysisStateSelection([]byte(raw))
		if !ok || value.LibraryIDs == nil || value.ItemIDs == nil {
			t.Fatalf("valid analysis selection rejected: %s", raw)
		}
	}
	for _, raw := range []string{`null`, `[]`, `{"Path":"private"}`, `{"Force":null}`, `{"Force":"true"}`, `{"LibraryIds":null}`, `{"LibraryIds":[1]}`, `{"LibraryIds":["b","a"]}`, `{"ItemIds":["same","same"]}`, `{"ItemIds":[""]}`, `{"LibraryIds":[" a"]}`, `{"libraryids":["a"]}`, `{"LibraryIds":["a"],"libraryids":["b"]}`, `{"ItemIds":["` + strings.Repeat("x", 129) + `"]}`} {
		if _, ok := decodeAnalysisStateSelection([]byte(raw)); ok {
			t.Fatalf("invalid analysis selection accepted: %s", raw)
		}
	}
}

func TestAnalysisArchiveAuthorityRetainsHistoricalPeerWithoutGrantingLiveAccess(t *testing.T) {
	valid := []analysisStateActor{
		{source: "manual", kind: "admin", user: "retired-user", session: "revoked-session", peer: "127.0.0.1"},
		{source: "compatibility", kind: "emby", user: "retired-user", session: "expired-session", peer: "2001:db8::1"},
		{source: "compatibility", kind: "application_key", session: "revoked-key", application: 7, client: "retained-client", peer: "192.0.2.1"},
		{source: "schedule", kind: "system"}, {source: "startup", kind: "system"}, {source: "system_event", kind: "system"},
	}
	for _, actor := range valid {
		if !validAnalysisStateActor(actor, true) {
			t.Fatal("well-formed historical task authority was rejected for not being a live grant")
		}
	}
	for _, mutate := range []func(*analysisStateActor){
		func(value *analysisStateActor) { value.user = "" }, func(value *analysisStateActor) { value.session = "" },
		func(value *analysisStateActor) { value.application = 1 }, func(value *analysisStateActor) { value.client = "unrelated" },
		func(value *analysisStateActor) { value.peer = "::ffff:127.0.0.1" }, func(value *analysisStateActor) { value.peer = "fe80::1%eth0" },
		func(value *analysisStateActor) { value.peer = "host.invalid" }, func(value *analysisStateActor) { value.kind = "system" },
	} {
		actor := valid[0]
		mutate(&actor)
		if validAnalysisStateActor(actor, true) {
			t.Fatal("invalid analysis task authority accepted")
		}
	}
	if validAnalysisStateActor(analysisStateActor{source: "schedule", kind: "system", peer: "127.0.0.1"}, true) {
		t.Fatal("a scheduler source imported a caller peer")
	}
	if validAnalysisStateActor(analysisStateActor{source: "compatibility", kind: "application_key", application: 7, session: "credential"}, true) {
		t.Fatal("an application key lost its original client identity")
	}
}

func TestAnalysisArchiveFeatureKeyBindsItemSourceAndProfileIndependently(t *testing.T) {
	base := analysisStateFeatureKey("item", "source", strings.Repeat("a", 64))
	if !analysisStateDigest(base) {
		t.Fatal("feature identity is not a fixed digest")
	}
	for _, value := range []string{analysisStateFeatureKey("item2", "source", strings.Repeat("a", 64)), analysisStateFeatureKey("item", "source2", strings.Repeat("a", 64)), analysisStateFeatureKey("item", "source", strings.Repeat("b", 64))} {
		if value == base {
			t.Fatal("changed feature authority reused an old cache key")
		}
	}
	if reflect.DeepEqual(analysisStateFeatureKey("ab", "c", "d"), analysisStateFeatureKey("a", "bc", "d")) {
		t.Fatal("feature key lost its field length boundaries")
	}
}

func TestAnalysisArchiveDetectionBindsCompleteSupportAndSourceTimeline(t *testing.T) {
	interval := introdetect.Interval{StartTicks: 50000000, EndTicks: 400000000}
	support := []introdetect.Support{
		{EpisodeKey: "episode-one", SourceKey: "source-one", ContentIdentity: strings.Repeat("a", 64), Interval: interval},
		{EpisodeKey: "episode-two", SourceKey: "source-two", ContentIdentity: strings.Repeat("b", 64), Interval: interval},
		{EpisodeKey: "episode-three", SourceKey: "source-three", ContentIdentity: strings.Repeat("c", 64), Interval: interval},
	}
	metrics := introdetect.Metrics{AudioAgreementPermille: 1000, AudioInformativePermille: 1000, AudioSimilarityPermille: 1000, AudioSamples: 100, AudioDistinct: 100, VisualAgreementPermille: 1000, VisualSimilarityPermille: 1000, VisualCoveragePermille: 1000, VisualSamples: 30, VisualTransitions: 10, VisualChangeCoveragePermille: 1000, PairCount: 3}
	metrics.VisualAnchorCount, metrics.VisualMinBandMatchedPermille, metrics.VisualMatchedTimePermille = 35, 1000, 1000
	metrics.VisualDistinctStates, metrics.VisualDominantStatePermille = 8, 125
	value := library.AnalysisStoredResult{Version: introdetect.Version, Episode: introdetect.EpisodeResult{EpisodeKey: "episode-one", SourceKey: "source-one", ContentIdentity: strings.Repeat("a", 64), Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Candidates: []introdetect.Candidate{{Interval: interval, GroupID: "group", Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Metrics: metrics, Support: support}}}}
	raw, _ := json.Marshal(value)
	base := []analysisStateEvidence{
		{ItemID: "item-one", SourceRevision: "source-one", EpisodeKey: "episode-one", ContentSHA256: strings.Repeat("a", 64), DurationTicks: 600000000},
		{ItemID: "item-two", SourceRevision: "source-two", EpisodeKey: "episode-two", ContentSHA256: strings.Repeat("b", 64), DurationTicks: 600000000},
		{ItemID: "item-three", SourceRevision: "source-three", EpisodeKey: "episode-three", ContentSHA256: strings.Repeat("c", 64), DurationTicks: 600000000},
	}
	for _, test := range []struct {
		name   string
		mutate func([]analysisStateEvidence) []analysisStateEvidence
		valid  bool
	}{
		{"complete", func(value []analysisStateEvidence) []analysisStateEvidence { return value }, true},
		{"missing_support", func(value []analysisStateEvidence) []analysisStateEvidence { return value[:2] }, false},
		{"short_support_source", func(value []analysisStateEvidence) []analysisStateEvidence {
			value[1].DurationTicks = 100000000
			return value
		}, false},
		{"wrong_content", func(value []analysisStateEvidence) []analysisStateEvidence {
			value[1].ContentSHA256 = strings.Repeat("d", 64)
			return value
		}, false},
		{"wrong_episode", func(value []analysisStateEvidence) []analysisStateEvidence {
			value[1].EpisodeKey = "unrelated"
			return value
		}, false},
		{"duplicate_source", func(value []analysisStateEvidence) []analysisStateEvidence { value[1] = value[0]; return value }, false},
		{"extra_source", func(value []analysisStateEvidence) []analysisStateEvidence {
			return append(value, analysisStateEvidence{ItemID: "extra", SourceRevision: "extra", EpisodeKey: "extra", ContentSHA256: strings.Repeat("d", 64), DurationTicks: 600000000})
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			refs, _ := json.Marshal(test.mutate(append([]analysisStateEvidence(nil), base...)))
			if got := validAnalysisStateDetection("item-one", "source-one", "episode-one", 600000000, "qualified", raw, &interval.StartTicks, &interval.EndTicks, refs); got != test.valid {
				t.Fatalf("support validity=%v want=%v", got, test.valid)
			}
		})
	}
	refs, _ := json.Marshal(base)
	otherStart := interval.StartTicks + 1
	if validAnalysisStateDetection("item-one", "source-one", "episode-one", 600000000, "qualified", raw, &otherStart, &interval.EndTicks, refs) {
		t.Fatal("SQL interval diverged from qualified JSON evidence")
	}
}

func TestAnalysisArchiveAbstentionCannotAcquireFabricatedSupport(t *testing.T) {
	value := library.AnalysisStoredResult{Version: introdetect.Version, Reason: "source_unavailable", Episode: introdetect.EpisodeResult{SourceKey: "source", Status: introdetect.NoResult, Reasons: []introdetect.Reason{}, Candidates: []introdetect.Candidate{}}}
	raw, _ := json.Marshal(value)
	if !validAnalysisStateDetection("item", "source", "", 600000000, "no_result", raw, nil, nil, []byte(`[]`)) {
		t.Fatal("historical source abstention required invented media evidence")
	}
	refs, _ := json.Marshal([]analysisStateEvidence{{ItemID: "item", SourceRevision: "source", ContentSHA256: strings.Repeat("a", 64), DurationTicks: 600000000}})
	if validAnalysisStateDetection("item", "source", "", 600000000, "no_result", raw, nil, nil, refs) {
		t.Fatal("an abstention acquired an unobserved content hash")
	}
}
