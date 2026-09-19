package activity

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestMusicFieldsAreNamesAllowedOnlyForMetadataActivity(t *testing.T) {
	event := Event{Action: ActionMetadataUpdated, Source: SourceNative,
		Actor: Actor{Kind: ActorUser, ID: "music-editor"}, Resource: Resource{Kind: ResourceItem, ID: "music-track"},
		ChangedFields: []Field{FieldArtists, FieldAlbum, FieldAlbumArtists}}
	executor := &activityCaptureExecutor{}
	if err := Record(context.Background(), executor, event); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 1 || !reflect.DeepEqual(executor.args[len(executor.args)-1], []string{"Album", "AlbumArtists", "Artists"}) {
		t.Fatal("music metadata activity did not retain canonical sorted field names")
	}
	for _, fields := range [][]Field{{"Album=private-value"}, {"Private artist name"}, {FieldAlbum, FieldAlbum}} {
		event.ChangedFields = fields
		executor := &activityCaptureExecutor{}
		if err := Record(context.Background(), executor, event); !errors.Is(err, ErrInvalidInput) || executor.calls != 0 {
			t.Fatal("invalid music activity reached storage", err)
		}
	}
	for _, field := range []Field{FieldAlbum, FieldArtists, FieldAlbumArtists} {
		unrelated := validationEvent()
		unrelated.ChangedFields = []Field{field}
		if _, err := eventArguments(unrelated); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("a music field escaped metadata action scope", field)
		}
	}
}
