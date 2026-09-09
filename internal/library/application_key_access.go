package library

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// Subject keeps an application credential independent from an optional target
// account. UserID selects catalog scope and user data; it never supplies an
// application key's authority.
type Subject struct {
	UserID                  string
	ApplicationCredentialID string
}

func validSubject(subject Subject) bool {
	if subject.ApplicationCredentialID == "" {
		return strings.TrimSpace(subject.UserID) != "" && !strings.ContainsRune(subject.UserID, '\x00')
	}
	return strings.TrimSpace(subject.ApplicationCredentialID) != "" &&
		!strings.ContainsRune(subject.ApplicationCredentialID, '\x00') &&
		(subject.UserID == "" || strings.TrimSpace(subject.UserID) != "") &&
		!strings.ContainsRune(subject.UserID, '\x00')
}

func checkSubjectApplicationKey(ctx context.Context, tx pgx.Tx, credentialID string, lock bool) error {
	if err := identity.CheckApplicationKey(ctx, tx, credentialID, lock); err != nil {
		if errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, identity.ErrInvalidCredentials) {
			return ErrForbidden
		}
		return fmt.Errorf("authorize application catalog credential: %w", err)
	}
	return nil
}

// A key and its optional target are checked in the same catalog snapshot as
// counts, pages, and projections. Target library policy scopes catalog reads;
// account disablement and playback policy do not remove the key's authority.
func (s *Store) beginSubjectRead(ctx context.Context, subject Subject) (pgx.Tx, libraryAccess, error) {
	if subject.ApplicationCredentialID == "" {
		return s.beginUserRead(ctx, subject.UserID)
	}
	if !validSubject(subject) {
		return nil, libraryAccess{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return nil, libraryAccess{}, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, libraryAccess{}, fmt.Errorf("begin application catalog read: %w", err)
	}
	if err := checkSubjectApplicationKey(ctx, tx, subject.ApplicationCredentialID, false); err != nil {
		rollback(tx)
		return nil, libraryAccess{}, err
	}
	if subject.UserID != "" {
		var administrator bool
		var policy []byte
		if err := tx.QueryRow(ctx, "SELECT is_administrator, policy FROM users WHERE id = $1", subject.UserID).Scan(&administrator, &policy); err != nil {
			rollback(tx)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, libraryAccess{}, ErrNotFound
			}
			return nil, libraryAccess{}, fmt.Errorf("read application catalog target: %w", err)
		}
		if !administrator {
			access, err := parseLibraryPolicy(policy)
			if err != nil {
				rollback(tx)
				return nil, libraryAccess{}, err
			}
			access.canPlay = true
			return tx, access, nil
		}
	}
	return tx, libraryAccess{all: true, folders: []string{}, canPlay: true}, nil
}

// Target account locks precede credential locks throughout state writes. An
// absent target is valid for playback lifecycle state, never for user_item_data.
func (s *Store) beginSubjectStateWrite(ctx context.Context, subject Subject, requirePlayback bool) (pgx.Tx, libraryAccess, error) {
	if subject.ApplicationCredentialID == "" {
		return s.beginStateWrite(ctx, subject.UserID, requirePlayback)
	}
	if !validSubject(subject) || s == nil || s.pool == nil {
		return nil, libraryAccess{}, ErrInvalidInput
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, libraryAccess{}, ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, libraryAccess{}, fmt.Errorf("begin application state update: %w", err)
	}
	if subject.UserID != "" {
		var id string
		if err := tx.QueryRow(ctx, "SELECT id FROM users WHERE id = $1 FOR SHARE", subject.UserID).Scan(&id); err != nil {
			rollback(tx)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, libraryAccess{}, ErrNotFound
			}
			return nil, libraryAccess{}, fmt.Errorf("lock application state target: %w", err)
		}
	}
	if err := checkSubjectApplicationKey(ctx, tx, subject.ApplicationCredentialID, true); err != nil {
		rollback(tx)
		return nil, libraryAccess{}, err
	}
	return tx, libraryAccess{all: true, folders: []string{}, canPlay: true}, nil
}
