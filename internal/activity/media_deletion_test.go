package activity

import (
	"context"
	"errors"
	"testing"
)

func TestFileDeletionActivitiesHaveTypedItemResourcesWithoutPaths(t *testing.T) {
	for _, action := range []Action{ActionItemDeleted, ActionSubtitleDeleted} {
		event := Event{Action: action, Source: SourceEmby, Actor: Actor{Kind: ActorUser, ID: "actor", CredentialID: "session"}, Resource: Resource{Kind: ResourceItem, ID: "item"}, Count: 1}
		executor := &activityCaptureExecutor{}
		if err := Record(context.Background(), executor, event); err != nil || executor.calls != 1 {
			t.Fatalf("file deletion audit rejected %s: %v", action, err)
		}
		name, overview := description(action, "")
		if name == "" || overview == "" {
			t.Fatal("file deletion audit requires fixed public text")
		}
		event.ChangedFields = []Field{FieldName}
		if err := Record(context.Background(), executor, event); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("file deletion accepted arbitrary changed values: %v", err)
		}
	}
}
