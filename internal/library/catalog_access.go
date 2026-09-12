package library

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	catalogLibraryInputLimit = 4096
	catalogLibraryIDBytes    = 256
)

// AllowedCatalogLibraries applies the current subject policy to trusted library
// identities retained by committed catalog notifications. It deliberately does
// not require a library or item to exist: a removal notification retains its old
// library scope. This is not a resource lookup or a public authorization API.
// Callers must establish the publication's trusted ownership independently and
// revalidate the receiving session before delivery. Empty input still validates
// the current subject authority. Only authorized input IDs appear in the result.
func (s *Store) AllowedCatalogLibraries(ctx context.Context, subject Subject, libraryIDs []string) (map[string]bool, error) {
	if len(libraryIDs) > catalogLibraryInputLimit ||
		(subject.UserID != "" && !validCatalogLibraryIdentifier(subject.UserID)) ||
		(subject.ApplicationCredentialID != "" && !validCatalogLibraryIdentifier(subject.ApplicationCredentialID)) || !validSubject(subject) {
		return nil, ErrInvalidInput
	}
	for _, id := range libraryIDs {
		if !validCatalogLibraryIdentifier(id) {
			return nil, ErrInvalidInput
		}
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)

	allowed := make(map[string]bool, len(libraryIDs))
	for _, id := range libraryIDs {
		if _, exists := allowed[id]; !exists {
			allowed[strings.Clone(id)] = access.all
		}
	}
	if !access.all {
		// Bound the retained map by the request, not by the size of the stored
		// policy, and never return a policy entry the caller did not request.
		for _, id := range access.folders {
			if granted, requested := allowed[id]; requested && !granted {
				allowed[strings.Clone(id)] = true
			}
		}
		for id, granted := range allowed {
			if !granted {
				delete(allowed, id)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete catalog notification library authorization: %w", err)
	}
	return allowed, nil
}

func validCatalogLibraryIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= catalogLibraryIDBytes && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}
