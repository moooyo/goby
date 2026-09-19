package activity

import (
	"strings"
	"testing"
)

func TestLibraryUpdatedActivityUsesBoundedFacts(t *testing.T) {
	event := Event{Action: ActionLibraryUpdated, Source: SourceNative,
		Actor:    Actor{Kind: ActorUser, ID: "administrator", CredentialID: "credential"},
		Resource: Resource{Kind: ResourceLibrary, ID: "library"}, Revision: 8, Count: 2}
	args, err := eventArguments(event)
	if err != nil || len(args) == 0 || args[0] != "library.updated" {
		t.Fatalf("library edit event was rejected: %+v, %v", args, err)
	}
	title, overview := description(ActionLibraryUpdated, "")
	if !strings.Contains(title, "Library") || overview == "" {
		t.Fatalf("library edit lacks a public description: %q, %q", title, overview)
	}
}
