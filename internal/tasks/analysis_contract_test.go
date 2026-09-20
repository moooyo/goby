package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestAnalysisRequestKeepsLegacyFingerprintAndCanonicalSelection(t *testing.T) {
	request := StartRequest{TaskID: "task"}
	legacy := sha256.Sum256([]byte(`{"TaskID":"task","Executor":"library.scan","Source":"manual"}`))
	if got := taskRequestFingerprint(request, LibraryScanKey, "manual", nil); got != legacy {
		t.Fatal("legacy nil-input fingerprint changed")
	}
	if _, err := normalizedTaskAnalysis(LibraryScanKey, &library.AnalysisSelection{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("legacy task accepted typed analysis input")
	}
	input := &library.AnalysisSelection{LibraryIDs: []string{"b", "a"}, ItemIDs: []string{"y", "x"}, Force: true}
	normalized, err := normalizedTaskAnalysis(library.TaskIntroAnalysisKey, input)
	if err != nil || !reflect.DeepEqual(normalized.LibraryIDs, []string{"a", "b"}) {
		t.Fatalf("normalize: %+v %v", normalized, err)
	}
	input.LibraryIDs[0] = "mutated"
	if normalized.LibraryIDs[1] != "b" {
		t.Fatal("selection retained caller storage")
	}
	equivalent := &library.AnalysisSelection{LibraryIDs: []string{"a", "b"}, ItemIDs: []string{"x", "y"}, Force: true}
	if taskRequestFingerprint(request, library.TaskIntroAnalysisKey, "manual", normalized) != taskRequestFingerprint(request, library.TaskIntroAnalysisKey, "manual", equivalent) {
		t.Fatal("equivalent selections acquired different receipts")
	}
	empty, err := normalizedTaskAnalysis(library.TaskPreviewGenerationKey, nil)
	if err != nil || empty == nil || empty.LibraryIDs == nil || empty.ItemIDs == nil {
		t.Fatal("analysis default did not produce a detached canonical selection")
	}
}

func TestAnalysisAuthorityAndFenceCannotBeReconstructedFromJSON(t *testing.T) {
	var run Run
	if err := json.Unmarshal([]byte(`{"id":"run","source":"compatibility","actor_kind":"application_key","actor_session_id":"credential","actor_application_key_id":7,"actor_client_session_id":"original-client","actor_peer_ip":"198.51.100.3","analysis_input":{"LibraryIds":["library"]}}`), &run); err != nil {
		t.Fatal(err)
	}
	actor, err := executionActor(run)
	if err != nil || actor == nil || !actor.Principal.IsApplicationKey() || actor.Principal.ClientSessionID != "original-client" || actor.Principal.PeerIP != "198.51.100.3" {
		t.Fatalf("application authority lost its original binding: %+v %v", actor, err)
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"actor_application_key_id", "actor_client_session_id", "actor_peer_ip", "original-client", "198.51.100.3"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("run JSON exposed private execution authority")
		}
	}
	work := executionWork(context.Background(), run, Child{ID: "child", RunID: "run", LibraryID: "library", AnalysisScopeKey: "chunk-1"}, "private-executor-token")
	encoded, err = json.Marshal(work)
	if err != nil || strings.Contains(string(encoded), "private-executor-token") || strings.Contains(string(encoded), "original-client") {
		t.Fatal("work serialized its sealed capability")
	}
	var copied Work
	if err := json.Unmarshal(encoded, &copied); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(copied.Fence(nil), ErrUnavailable) {
		t.Fatal("JSON reconstructed a publication capability")
	}
	for _, invalid := range []Run{
		{Source: "schedule"}, {Source: "manual", ActorKind: "system"}, {Source: "schedule", ActorKind: "system", ActorSessionID: "borrowed"},
	} {
		actor, err := executionActor(invalid)
		if err == nil && actor == nil {
			t.Fatal("incomplete manual/history authority became system")
		}
	}
	if actor, err := executionActor(Run{Source: "system_event", ActorKind: "system"}); err != nil || actor != nil {
		t.Fatal("explicit system source was not retained")
	}
	if actor.Audience != identity.AdministratorEmby {
		t.Fatal("application audience changed")
	}
}
