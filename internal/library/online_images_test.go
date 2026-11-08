package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/providers"
)

func TestMissingOnlineImageTypesPreservesExistingAndDeletedArtwork(t *testing.T) {
	collection := ArtworkCollection{Items: []Image{{ImageType: "Backdrop", ImageIndex: 2}}}
	managed := artwork.ManagedSet{Types: []string{"Primary"}}
	want := map[string]bool{"Logo": true}
	if got := missingOnlineImageTypes("Movie", collection, managed); !reflect.DeepEqual(got, want) {
		t.Fatalf("existing backdrop or deleted poster was considered missing: got %v, want %v", got, want)
	}
	if got := missingOnlineImageTypes("Episode", ArtworkCollection{Items: []Image{{ImageType: "Primary"}}}, artwork.ManagedSet{}); !reflect.DeepEqual(got, map[string]bool{"Thumb": true}) {
		t.Fatalf("episode thumbnail policy changed: %v", got)
	}
	if got := missingOnlineImageTypes("Audio", ArtworkCollection{}, artwork.ManagedSet{}); len(got) != 0 {
		t.Fatalf("unsupported audio item received an online-image policy: %v", got)
	}
}

func TestOnlineImageProviderTimeoutIsNotTaskCancellation(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled} {
		err := onlineImageProviderError(context.Background(), cause)
		if !errors.Is(err, providers.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			t.Fatalf("provider failure was mislabeled as administrator cancellation: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := onlineImageProviderError(ctx, providers.ErrUnavailable); !errors.Is(err, context.Canceled) {
		t.Fatalf("real task cancellation was hidden: %v", err)
	}
}

func TestLibraryOnlineImagesRequiresExplicitVideoOptIn(t *testing.T) {
	if DefaultLibraryOptions().EnableOnlineImages || EffectiveLibraryOptions(Library{}).EnableOnlineImages {
		t.Fatal("legacy/default libraries opted into provider downloads")
	}
	var update LibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"EnableOnlineImages":true}`), &update); err != nil {
		t.Fatal(err)
	}
	options := applyLibraryOptions(DefaultLibraryOptions(), &update)
	if !options.EnableOnlineImages || validateLibraryOptions("movies", options) != nil || validateLibraryOptions("music", options) == nil {
		t.Fatal("online images did not remain an explicit video-library policy")
	}
	for _, raw := range []string{`{"EnableOnlineImages":null}`, `{"EnableOnlineImages":true,"enableonlineimages":false}`, `{"EnableOnlineImages":1}`} {
		if err := json.Unmarshal([]byte(raw), &update); err == nil {
			t.Fatalf("accepted an ambiguous online-image switch: %s", raw)
		}
	}
}
