package activity

import (
	"context"
	"errors"
	"testing"
)

func TestUserDeletedActivityHasItsOwnActionAndNoUpdateFields(t *testing.T) {
	event := Event{Action: ActionUserDeleted, Source: SourceNative,
		Actor:    Actor{Kind: ActorUser, ID: "deleted-actor", CredentialID: "deleted-session"},
		Resource: Resource{Kind: ResourceUser, ID: "deleted-user"}, Revision: 7, Count: 1}
	executor := &activityCaptureExecutor{}
	if err := Record(context.Background(), executor, event); err != nil || executor.calls != 1 || executor.args[0] != "user.deleted" {
		t.Fatalf("deletion did not record its distinct typed action: %v", err)
	}
	name, overview := description(ActionUserDeleted, "")
	if name != "User deleted" || overview != "A user account and its associated user state were deleted." {
		t.Fatal("user deletion lacks fixed public activity metadata")
	}
	event.ChangedFields = []Field{FieldName}
	if err := Record(context.Background(), executor, event); !errors.Is(err, ErrInvalidInput) || executor.calls != 1 {
		t.Fatal("a deletion event accepted an update field or reached storage")
	}
}
