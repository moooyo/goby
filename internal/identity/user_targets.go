package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// GetUserPolicy returns the raw stored target policy without authenticating or
// authorizing the target. Disabled accounts remain observable to callers whose
// existing target-selection rules permit them. Runtime policy interpretation
// belongs to the caller; this projection does not impose stricter validation.
func (s *Store) GetUserPolicy(ctx context.Context, id string) (json.RawMessage, error) {
	var policy json.RawMessage
	err := s.pool.QueryRow(ctx, "SELECT policy FROM users WHERE id = $1", id).Scan(&policy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user policy: %w", err)
	}
	return policy, nil
}

// GetUserName selects a login target's current name. The caller must still run
// normal credential and policy authentication before issuing a login.
func (s *Store) GetUserName(ctx context.Context, id string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, "SELECT name FROM users WHERE id = $1", id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get user name: %w", err)
	}
	return name, nil
}

// GetUserConfiguration returns an independent complete configuration document
// for the existing preference projection. It is not an authority observation.
func (s *Store) GetUserConfiguration(ctx context.Context, id string) (json.RawMessage, error) {
	var configuration json.RawMessage
	err := s.pool.QueryRow(ctx, "SELECT configuration FROM users WHERE id = $1", id).Scan(&configuration)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get user configuration: %w", err)
	}
	return configuration, nil
}

// ClientSessionController contains only the account facts needed to filter a
// session list. It is not an authenticated principal and does not replace the
// independent authorization required when a command is accepted or delivered.
type ClientSessionController struct {
	ID              string
	IsAdministrator bool
	IsDisabled      bool
	Policy          json.RawMessage `json:"-"`
}

// GetClientSessionController retains disabled accounts so the response-local
// policy decision can apply the same denial as a complete GetUser observation.
func (s *Store) GetClientSessionController(ctx context.Context, id string) (ClientSessionController, error) {
	var controller ClientSessionController
	err := s.pool.QueryRow(ctx, `SELECT id, is_administrator, is_disabled, policy
		FROM users WHERE id = $1`, id).
		Scan(&controller.ID, &controller.IsAdministrator, &controller.IsDisabled, &controller.Policy)
	if errors.Is(err, pgx.ErrNoRows) {
		return ClientSessionController{}, ErrNotFound
	}
	if err != nil {
		return ClientSessionController{}, fmt.Errorf("get client session controller: %w", err)
	}
	return controller, nil
}
