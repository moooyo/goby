package server

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
)

const (
	maxLibraryChangedIDs     = 4096
	maxLibraryChangedIDBytes = 256
)

// These are the observed public fields. Re-encoding this projection also strips
// unknown data, which cannot acquire authority from a trusted item's scope.
type libraryChangedData struct {
	FoldersAddedTo     []string `json:"FoldersAddedTo"`
	FoldersRemovedFrom []string `json:"FoldersRemovedFrom"`
	ItemsAdded         []string `json:"ItemsAdded"`
	ItemsRemoved       []string `json:"ItemsRemoved"`
	ItemsUpdated       []string `json:"ItemsUpdated"`
	CollectionFolders  []string `json:"CollectionFolders"`
	IsEmpty            bool     `json:"IsEmpty"`
}

func validLibraryChangedID(id string) bool {
	return len(id) > 0 && len(id) <= maxLibraryChangedIDBytes && utf8.ValidString(id) &&
		strings.TrimSpace(id) == id && strings.IndexFunc(id, unicode.IsControl) < 0
}

func parseLibraryChangedData(raw json.RawMessage) (libraryChangedData, error) {
	var fields map[string]json.RawMessage
	var result libraryChangedData
	if len(raw) > events.DefaultMaxMessageBytes || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return result, events.ErrInvalidEvent
	}
	count := 0
	for _, field := range []struct {
		name string
		ids  *[]string
	}{
		{"FoldersAddedTo", &result.FoldersAddedTo}, {"FoldersRemovedFrom", &result.FoldersRemovedFrom},
		{"ItemsAdded", &result.ItemsAdded}, {"ItemsRemoved", &result.ItemsRemoved},
		{"ItemsUpdated", &result.ItemsUpdated}, {"CollectionFolders", &result.CollectionFolders},
	} {
		*field.ids = []string{}
		if value, present := fields[field.name]; present {
			value = bytes.TrimSpace(value)
			if len(value) == 0 || value[0] != '[' || json.Unmarshal(value, field.ids) != nil {
				return libraryChangedData{}, events.ErrInvalidEvent
			}
		}
		if len(*field.ids) > maxLibraryChangedIDs-count {
			return libraryChangedData{}, events.ErrInvalidEvent
		}
		count += len(*field.ids)
		for _, id := range *field.ids {
			if !validLibraryChangedID(id) {
				return libraryChangedData{}, events.ErrInvalidEvent
			}
		}
	}
	if value, present := fields["IsEmpty"]; present {
		value = bytes.TrimSpace(value)
		if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
			return libraryChangedData{}, events.ErrInvalidEvent
		}
	}
	// IsEmpty describes the recipient's final projection, not the original event.
	result.IsEmpty = count == 0
	return result, nil
}

// The socket loop revalidates the session immediately before calling this method.
// Resource authorization uses a fresh catalog snapshot, including application
// credentials independently of any user. Deleted IDs use the committed private
// scope and do not require a surviving item or library row.
func (s *Server) libraryChangedSocketPayload(ctx context.Context, principal identity.Principal, event events.Event) ([]byte, error) {
	payload := event.Bytes()
	if len(payload) > events.DefaultMaxMessageBytes {
		return nil, events.ErrInvalidEvent
	}
	var envelope events.Envelope
	if json.Unmarshal(payload, &envelope) != nil || envelope.MessageType != "LibraryChanged" {
		return nil, events.ErrInvalidEvent
	}
	data, err := parseLibraryChangedData(envelope.Data)
	if err != nil {
		return nil, err
	}
	scopes := event.CatalogScopes()
	if len(scopes) > maxLibraryChangedIDs {
		return nil, events.ErrInvalidEvent
	}
	libraryIDs := make([]string, 0, len(scopes))
	seenLibraries := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		if !validLibraryChangedID(scope.ItemID) || !validLibraryChangedID(scope.LibraryID) {
			return nil, events.ErrInvalidEvent
		}
		if !seenLibraries[scope.LibraryID] {
			seenLibraries[scope.LibraryID] = true
			libraryIDs = append(libraryIDs, scope.LibraryID)
		}
	}
	allowedLibraries, err := s.library.AllowedCatalogLibraries(ctx, librarySubject(principal, principal.User.ID), libraryIDs)
	if err != nil {
		return nil, err
	}
	allowedItems := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		// A shared or removed identifier can have several trusted visibility
		// contexts. A permitted context allows this identifier, without exposing
		// the other libraries; subsequent resource reads keep their own checks.
		if allowedLibraries[scope.LibraryID] {
			allowedItems[scope.ItemID] = true
		}
	}
	count := 0
	for _, ids := range []*[]string{&data.FoldersAddedTo, &data.FoldersRemovedFrom, &data.ItemsAdded,
		&data.ItemsRemoved, &data.ItemsUpdated, &data.CollectionFolders} {
		filtered := make([]string, 0, len(*ids))
		for _, id := range *ids {
			if allowedItems[id] {
				filtered = append(filtered, id)
			}
		}
		*ids = filtered
		count += len(filtered)
	}
	if count == 0 {
		return nil, nil
	}
	data.IsEmpty = false
	envelope.Data, err = json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}
