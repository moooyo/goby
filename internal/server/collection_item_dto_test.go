package server

import (
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestCollectionItemProjectionKeepsAuthorizedCountsAndEntryIdentity(t *testing.T) {
	s := &Server{serverID: "server"}
	item := library.Item{ID: "collection", Type: library.PlaylistKind, IsFolder: true,
		Collection: &library.CollectionInfo{ItemCount: 2, IsLocked: true, MediaType: "Audio",
			Shares: []library.CollectionShare{{UserID: "private-share", CanEdit: true}}}}
	dto := s.itemDTO(item, nil, false)
	if dto["ChildCount"] != 2 || dto["IsLocked"] != true || dto["MediaType"] != "Audio" {
		t.Fatal("collection projection lost authorized metadata")
	}
	if _, exists := dto["Shares"]; exists {
		t.Fatal("catalog projection exposed collection management grants")
	}
	entry := s.itemDTO(library.Item{ID: "track", Type: "Audio", PlaylistItemID: "9007199254740993"}, nil, false)
	if entry["PlaylistItemId"] != "9007199254740993" {
		t.Fatal("playlist entry identity lost integer precision")
	}
}
