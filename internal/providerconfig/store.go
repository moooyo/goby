// Package providerconfig publishes encrypted administrator provider overrides.
package providerconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
)

var (
	ErrInvalid     = errors.New("invalid provider configuration")
	ErrConflict    = errors.New("provider configuration revision changed")
	ErrUnavailable = errors.New("provider configuration unavailable")
)

type Field struct {
	ID       string `json:"Id"`
	Label    string `json:"Label"`
	Secret   bool   `json:"Secret"`
	HasValue bool   `json:"HasValue"`
	Source   string `json:"Source"`
}

type Item struct {
	ID         string  `json:"Id"`
	Name       string  `json:"Name"`
	Configured bool    `json:"Configured"`
	Fields     []Field `json:"Fields"`
}

type View struct {
	Revision          string `json:"Revision"`
	DeploymentEnabled bool   `json:"DeploymentEnabled"`
	Enabled           bool   `json:"Enabled"`
	Items             []Item `json:"Items"`
}

type Change struct {
	Mode  string  `json:"Mode"`
	Value *string `json:"Value,omitempty"`
}

type Update struct {
	Revision string            `json:"Revision"`
	Fields   map[string]Change `json:"Fields"`
}

// Credential documents and runtime snapshots are private and are never JSON
// projections. Only View may leave the configuration boundary.
type state struct {
	revision  string
	overrides map[string]string
	effective config.OnlineProvidersConfig
	ready     bool
}

type Store struct {
	pool     *pgxpool.Pool
	users    *identity.Store
	defaults config.OnlineProvidersConfig
	mu       sync.Mutex
	current  atomic.Pointer[state]
}

func New(ctx context.Context, pool *pgxpool.Pool, users *identity.Store, defaults config.OnlineProvidersConfig) (*Store, error) {
	if pool == nil || users == nil || defaults.Validate() != nil {
		return nil, ErrInvalid
	}
	store := &Store{pool: pool, users: users, defaults: defaults}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rollback(tx)
	initial, err := store.read(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, ErrUnavailable
	}
	store.current.Store(initial)
	return store, nil
}

// Effective reads one coherent, immutable snapshot. An unconfirmed commit
// fails closed until an authorized configuration refresh resolves the state.
func (s *Store) Effective() config.OnlineProvidersConfig {
	current := s.current.Load()
	if current == nil || !current.ready {
		return config.OnlineProvidersConfig{}
	}
	return current.effective
}

func (s *Store) Get(ctx context.Context, actor identity.Principal) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.observe(ctx, actor, "")
	if err != nil {
		return View{}, err
	}
	return view(value), nil
}

// Capture revalidates the native administrator and saved revision before a
// test request. It returns private credentials only to the server's adapter.
func (s *Store) Capture(ctx context.Context, actor identity.Principal, expected string) (config.OnlineProvidersConfig, error) {
	if _, err := revision(expected); err != nil {
		return config.OnlineProvidersConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, err := s.observe(ctx, actor, expected)
	if err != nil {
		return config.OnlineProvidersConfig{}, err
	}
	return value.effective, nil
}

func (s *Store) observe(ctx context.Context, actor identity.Principal, expected string) (*state, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rollback(tx)
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, true); err != nil {
		return nil, err
	}
	value, err := s.read(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	if expected != "" && expected != value.revision {
		return nil, ErrConflict
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, ErrUnavailable
	}
	s.current.Store(value)
	return value, nil
}

func (s *Store) Update(ctx context.Context, actor identity.Principal, providerID string, update Update) (View, error) {
	expected, err := revision(update.Revision)
	if err != nil || expected == math.MaxInt64 || validateChanges(providerID, update.Fields) != nil {
		return View{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return View{}, ErrUnavailable
	}
	defer rollback(tx)
	if err := identity.LockProviderConfigurationMutation(ctx, tx, actor); err != nil {
		return View{}, err
	}
	previous, err := s.read(ctx, tx, true)
	if err != nil {
		return View{}, err
	}
	if previous.revision != update.Revision {
		return View{}, ErrConflict
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return View{}, err
	}
	overrides := clone(previous.overrides)
	for field, change := range update.Fields {
		key := providerID + "." + field
		switch change.Mode {
		case "default":
			delete(overrides, key)
		case "clear":
			overrides[key] = ""
		case "replace":
			overrides[key] = *change.Value
		}
	}
	effective, err := apply(s.defaults, overrides)
	if err != nil {
		return View{}, err
	}
	if equal(previous.overrides, overrides) {
		if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
			return View{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return View{}, ErrUnavailable
		}
		s.current.Store(previous)
		return view(previous), nil
	}
	next := &state{revision: strconv.FormatInt(expected+1, 10), overrides: overrides, effective: effective, ready: true}
	var sealed []byte
	if len(overrides) != 0 {
		document, err := json.Marshal(overrides)
		if err != nil {
			return View{}, ErrInvalid
		}
		sealed, err = s.users.SealProviderConfiguration(ctx, tx, next.revision, document)
		clear(document)
		if err != nil {
			return View{}, err
		}
	}
	result, err := tx.Exec(ctx, `UPDATE provider_configuration SET revision=revision+1,
		configuration_ciphertext=$2, updated_at=clock_timestamp() WHERE id=1 AND revision=$1`, expected, sealed)
	if err != nil {
		return View{}, ErrUnavailable
	}
	if result.RowsAffected() != 1 {
		return View{}, ErrConflict
	}
	if err := activity.Record(ctx, tx, activity.Event{
		Action: activity.ActionSettingsUpdated, Source: activity.SourceNative,
		Actor:    activity.Actor{Kind: activity.ActorUser, ID: actor.User.ID, CredentialID: actor.SessionID},
		Resource: activity.Resource{Kind: activity.ResourceSettings, ID: "providers"}, Revision: expected + 1, Count: 1,
	}); err != nil {
		return View{}, err
	}
	if err := identity.CheckAdministrator(ctx, tx, actor, identity.AdministratorNative, false); err != nil {
		return View{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		s.current.Store(&state{})
		return View{}, ErrUnavailable
	}
	s.current.Store(next)
	return view(next), nil
}

func (s *Store) read(ctx context.Context, tx pgx.Tx, lock bool) (*state, error) {
	query := `SELECT revision::text, configuration_ciphertext FROM provider_configuration WHERE id=1`
	if lock {
		query += " FOR UPDATE"
	}
	var current state
	var sealed []byte
	if tx.QueryRow(ctx, query).Scan(&current.revision, &sealed) != nil {
		return nil, ErrUnavailable
	}
	if _, err := revision(current.revision); err != nil {
		return nil, ErrUnavailable
	}
	current.overrides = map[string]string{}
	if sealed != nil {
		document, err := s.users.OpenProviderConfiguration(ctx, current.revision, sealed)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(document))
		err = decoder.Decode(&current.overrides)
		clear(document)
		if err != nil || current.overrides == nil || len(current.overrides) > 6 {
			return nil, ErrUnavailable
		}
	}
	var err error
	current.effective, err = apply(s.defaults, current.overrides)
	if err != nil {
		return nil, ErrUnavailable
	}
	current.ready = true
	return &current, nil
}

type fieldDefinition struct {
	id, label string
	secret    bool
}
type providerDefinition struct {
	id, name string
	fields   []fieldDefinition
}

var definitions = []providerDefinition{
	{"tmdb", "The Movie Database", []fieldDefinition{{"Token", "Access token", true}}},
	{"musicbrainz", "MusicBrainz", []fieldDefinition{{"UserAgent", "Application identification", false}}},
	{"opensubtitles", "OpenSubtitles", []fieldDefinition{{"APIKey", "API key", true}, {"Username", "Username", false}, {"Password", "Password", true}, {"UserAgent", "Application identification", false}}},
}

func ValidProvider(providerID string) bool {
	return slices.ContainsFunc(definitions, func(value providerDefinition) bool { return value.id == providerID })
}

func validateChanges(providerID string, changes map[string]Change) error {
	var fields []fieldDefinition
	for _, definition := range definitions {
		if definition.id == providerID {
			fields = definition.fields
		}
	}
	if len(fields) == 0 || len(changes) == 0 || len(changes) > len(fields) {
		return ErrInvalid
	}
	for field, change := range changes {
		if !slices.ContainsFunc(fields, func(value fieldDefinition) bool { return value.id == field }) {
			return ErrInvalid
		}
		switch change.Mode {
		case "replace":
			if change.Value == nil || *change.Value == "" {
				return ErrInvalid
			}
		case "clear", "default":
			if change.Value != nil {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}

func apply(defaults config.OnlineProvidersConfig, overrides map[string]string) (config.OnlineProvidersConfig, error) {
	effective := defaults
	for key, value := range overrides {
		switch key {
		case "tmdb.Token":
			effective.TMDBToken = value
		case "musicbrainz.UserAgent":
			effective.MusicBrainzUserAgent = value
		case "opensubtitles.APIKey":
			effective.OpenSubtitlesAPIKey = value
		case "opensubtitles.Username":
			effective.OpenSubtitlesUsername = value
		case "opensubtitles.Password":
			effective.OpenSubtitlesPassword = value
		case "opensubtitles.UserAgent":
			effective.OpenSubtitlesUserAgent = value
		default:
			return config.OnlineProvidersConfig{}, ErrInvalid
		}
	}
	if effective.Validate() != nil {
		return config.OnlineProvidersConfig{}, ErrInvalid
	}
	return effective, nil
}

func fieldValue(value config.OnlineProvidersConfig, key string) string {
	switch key {
	case "tmdb.Token":
		return value.TMDBToken
	case "musicbrainz.UserAgent":
		return value.MusicBrainzUserAgent
	case "opensubtitles.APIKey":
		return value.OpenSubtitlesAPIKey
	case "opensubtitles.Username":
		return value.OpenSubtitlesUsername
	case "opensubtitles.Password":
		return value.OpenSubtitlesPassword
	case "opensubtitles.UserAgent":
		return value.OpenSubtitlesUserAgent
	}
	return ""
}

func view(value *state) View {
	result := View{Revision: value.revision, DeploymentEnabled: value.effective.Enabled, Enabled: value.effective.Enabled, Items: []Item{}}
	for _, definition := range definitions {
		item := Item{ID: definition.id, Name: definition.name, Configured: true, Fields: []Field{}}
		for _, field := range definition.fields {
			key := definition.id + "." + field.id
			_, override := value.overrides[key]
			source := "deployment"
			if override {
				source = "override"
			}
			has := strings.TrimSpace(fieldValue(value.effective, key)) != ""
			item.Configured = item.Configured && has
			item.Fields = append(item.Fields, Field{ID: field.id, Label: field.label, Secret: field.secret, HasValue: has, Source: source})
		}
		result.Items = append(result.Items, item)
	}
	return result
}

func revision(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 || strconv.FormatInt(value, 10) != raw {
		return 0, ErrInvalid
	}
	return value, nil
}
func clone(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
func equal(first, second map[string]string) bool {
	if len(first) != len(second) {
		return false
	}
	for key, value := range first {
		if other, ok := second[key]; !ok || other != value {
			return false
		}
	}
	return true
}
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
