package settings

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

const MaxLocalNetworkSubnets = 32

var ErrServerPolicyDenied = errors.New("server remote access is disabled")

// ServerPolicyValues controls the application-wide access boundary. Individual
// account policies remain additional restrictions, independently of this list.
type ServerPolicyValues struct {
	EnableRemoteAccess  bool
	LocalNetworkSubnets []string
	UICulture           string
}

type ServerPolicySnapshot struct {
	Revision  int64
	Values    ServerPolicyValues
	UpdatedAt time.Time
	prefixes  []netip.Prefix
}

type ServerPolicyUpdate struct {
	Revision          int64
	Values            ServerPolicyValues
	ClientAddress     string
	ConfirmDisconnect bool
}

// DefaultServerPolicy preserves existing access until an administrator changes
// it. Loopback is always local and cannot be removed by editing subnet entries.
func DefaultServerPolicy() ServerPolicyValues {
	return ServerPolicyValues{
		EnableRemoteAccess: true,
		LocalNetworkSubnets: []string{
			"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
			"169.254.0.0/16", "fc00::/7", "fe80::/10",
		},
		UICulture: "zh-CN",
	}
}

func ValidateServerPolicy(value ServerPolicyValues) error {
	fields := map[string]string{}
	if value.UICulture != "zh-CN" && value.UICulture != "en-US" {
		fields["UICulture"] = "Select zh-CN or en-US."
	}
	if value.LocalNetworkSubnets == nil || len(value.LocalNetworkSubnets) > MaxLocalNetworkSubnets {
		fields["LocalNetworkSubnets"] = "Supply an array of at most 32 distinct canonical network CIDRs."
	}
	seen := make(map[string]bool, len(value.LocalNetworkSubnets))
	for _, entry := range value.LocalNetworkSubnets {
		prefix, err := netip.ParsePrefix(entry)
		if len(entry) > 64 || err != nil || prefix.Addr().Is4In6() || prefix != prefix.Masked() ||
			prefix.String() != entry || seen[entry] {
			fields["LocalNetworkSubnets"] = "Use distinct canonical IPv4 or IPv6 network CIDRs, with no host bits."
			break
		}
		seen[entry] = true
	}
	if len(fields) != 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func makeServerPolicySnapshot(revision int64, values ServerPolicyValues, updated time.Time) (ServerPolicySnapshot, error) {
	if revision < 1 {
		return ServerPolicySnapshot{}, ErrStoredSettings
	}
	if err := ValidateServerPolicy(values); err != nil {
		return ServerPolicySnapshot{}, fmt.Errorf("%w: invalid server policy", ErrStoredSettings)
	}
	values.LocalNetworkSubnets = slices.Clone(values.LocalNetworkSubnets)
	result := ServerPolicySnapshot{Revision: revision, Values: values, UpdatedAt: updated.UTC()}
	result.prefixes = make([]netip.Prefix, len(values.LocalNetworkSubnets))
	for index, entry := range values.LocalNetworkSubnets {
		result.prefixes[index] = netip.MustParsePrefix(entry)
	}
	return result, nil
}

func (snapshot ServerPolicySnapshot) IsLocal(address string) bool {
	peer, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	peer = peer.Unmap()
	if peer.Zone() != "" {
		peer = peer.WithZone("")
	}
	if peer.IsUnspecified() || peer.IsMulticast() {
		return false
	}
	if peer.IsLoopback() {
		return true
	}
	for _, prefix := range snapshot.prefixes {
		if prefix.Contains(peer) {
			return true
		}
	}
	return false
}

func (snapshot ServerPolicySnapshot) Allows(address string) bool {
	return snapshot.Values.EnableRemoteAccess || snapshot.IsLocal(address)
}

func cloneServerPolicy(snapshot ServerPolicySnapshot) ServerPolicySnapshot {
	snapshot.Values.LocalNetworkSubnets = slices.Clone(snapshot.Values.LocalNetworkSubnets)
	snapshot.prefixes = slices.Clone(snapshot.prefixes)
	return snapshot
}

type serverPolicyObserver struct {
	address string
	cancel  context.CancelCauseFunc
}

// ServerPolicyStore publishes only committed policy. The observer lock fences
// request admission against publication so a request cannot miss revocation.
type ServerPolicyStore struct {
	owner        library.OwnedTransactions
	writeMu      sync.Mutex
	current      atomic.Pointer[ServerPolicySnapshot]
	observerMu   sync.Mutex
	observers    map[uint64]serverPolicyObserver
	nextObserver uint64
}

func NewServerPolicy(ctx context.Context, owner library.OwnedTransactions) (*ServerPolicyStore, error) {
	if owner == nil {
		return nil, ErrInvalidInput
	}
	store := &ServerPolicyStore{owner: owner, observers: make(map[uint64]serverPolicyObserver)}
	var initial ServerPolicySnapshot
	err := owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		var err error
		initial, err = readServerPolicy(tx.QueryRow("SELECT " + serverPolicyColumns + " FROM server_policy_settings WHERE id=1"))
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("load server policy: %w", err)
	}
	store.current.Store(&initial)
	return store, nil
}

func (store *ServerPolicyStore) Snapshot() ServerPolicySnapshot {
	return cloneServerPolicy(*store.current.Load())
}

func (store *ServerPolicyStore) Get(ctx context.Context, actor Actor) (ServerPolicySnapshot, error) {
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	var result ServerPolicySnapshot
	err := store.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if err := checkActor(tx, actor, true); err != nil {
			return err
		}
		result = store.Snapshot()
		return checkActor(tx, actor, false)
	})
	return result, err
}

func (store *ServerPolicyStore) Update(ctx context.Context, actor Actor, request ServerPolicyUpdate) (ServerPolicySnapshot, error) {
	if err := validateRevision(request.Revision); err != nil {
		return ServerPolicySnapshot{}, err
	}
	if err := ValidateServerPolicy(request.Values); err != nil {
		return ServerPolicySnapshot{}, err
	}
	proposed, err := makeServerPolicySnapshot(request.Revision, request.Values, time.Time{})
	if err != nil {
		return ServerPolicySnapshot{}, err
	}
	if !proposed.Allows(request.ClientAddress) && !request.ConfirmDisconnect {
		return ServerPolicySnapshot{}, &ValidationError{Fields: map[string]string{
			"ConfirmDisconnect": "Confirm that this policy will disconnect the current client.",
		}}
	}
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	var committed ServerPolicySnapshot
	err = store.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		if err := checkActor(tx, actor, true); err != nil {
			return err
		}
		previous, err := readServerPolicy(tx.QueryRow("SELECT " + serverPolicyColumns + " FROM server_policy_settings WHERE id=1 FOR UPDATE"))
		if err != nil {
			return err
		}
		if err := checkActor(tx, actor, false); err != nil {
			return err
		}
		// A previously admitted policy request may have waited while another
		// administrator changed the boundary. It cannot reopen remote access
		// from an address that the latest committed policy already rejects.
		if !previous.Allows(request.ClientAddress) {
			return ErrServerPolicyDenied
		}
		if previous.Revision != request.Revision {
			return ErrRevisionConflict
		}
		published := store.current.Load()
		if published == nil || published.Revision != previous.Revision || !equalServerPolicy(published.Values, previous.Values) {
			return ErrStoredSettings
		}
		if equalServerPolicy(previous.Values, proposed.Values) {
			committed = previous
		} else {
			committed, err = readServerPolicy(tx.QueryRow(`UPDATE server_policy_settings SET revision=revision+1,
				enable_remote_access=$2, local_network_subnets=$3, ui_culture=$4, updated_at=clock_timestamp()
				WHERE id=1 AND revision=$1 RETURNING `+serverPolicyColumns,
				previous.Revision, proposed.Values.EnableRemoteAccess, proposed.Values.LocalNetworkSubnets, proposed.Values.UICulture))
			if err != nil {
				return err
			}
			if err := recordServerPolicyActivity(tx, actor, committed.Revision); err != nil {
				return err
			}
			if err := systemevents.Record(tx.Exec, systemevents.ConfigurationChanged); err != nil {
				return err
			}
		}
		return checkActor(tx, actor, false)
	})
	if err != nil {
		return ServerPolicySnapshot{}, err
	}
	store.publish(committed)
	return cloneServerPolicy(committed), nil
}

func equalServerPolicy(first, second ServerPolicyValues) bool {
	return first.EnableRemoteAccess == second.EnableRemoteAccess && first.UICulture == second.UICulture &&
		slices.Equal(first.LocalNetworkSubnets, second.LocalNetworkSubnets)
}

func recordServerPolicyActivity(tx library.OwnedTx, actor Actor, revision int64) error {
	source := activity.SourceNative
	if actor.Audience == identity.AdministratorEmby {
		source = activity.SourceEmby
	}
	actorValue := activity.Actor{Kind: activity.ActorUser, ID: actor.Principal.User.ID, CredentialID: actor.Principal.SessionID}
	if actor.Principal.IsApplicationKey() {
		actorValue.Kind = activity.ActorApplicationKey
		actorValue.ID = strconv.FormatInt(actor.Principal.ApplicationKeyID, 10)
	}
	// The distinct resource identifies this policy without recording private
	// address ranges or confusing its revision with managed_settings.
	return activity.RecordOwned(tx, activity.Event{
		Action: activity.ActionSettingsUpdated, Severity: activity.SeverityInfo, Source: source,
		Actor: actorValue, Resource: activity.Resource{Kind: activity.ResourceSettings, ID: "server_policy"},
		Revision: revision, Count: 1,
	})
}

// Watch binds an admitted operation to its current client address. Publication
// cancels operations that lose access, including requests admitted under an
// earlier revision. Callers must release the registration when work completes.
func (store *ServerPolicyStore) Watch(parent context.Context, address string) (context.Context, func(), bool) {
	ctx, cancel := context.WithCancelCause(parent)
	store.observerMu.Lock()
	if !store.current.Load().Allows(address) {
		store.observerMu.Unlock()
		cancel(ErrServerPolicyDenied)
		return ctx, func() {}, false
	}
	store.nextObserver++
	id := store.nextObserver
	store.observers[id] = serverPolicyObserver{address: address, cancel: cancel}
	store.observerMu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			store.observerMu.Lock()
			delete(store.observers, id)
			store.observerMu.Unlock()
			cancel(context.Canceled)
		})
	}
	stop := context.AfterFunc(parent, release)
	return ctx, func() { stop(); release() }, true
}

func (store *ServerPolicyStore) publish(snapshot ServerPolicySnapshot) {
	copy := cloneServerPolicy(snapshot)
	store.observerMu.Lock()
	store.current.Store(&copy)
	var revoked []context.CancelCauseFunc
	for id, observer := range store.observers {
		if !copy.Allows(observer.address) {
			revoked = append(revoked, observer.cancel)
			delete(store.observers, id)
		}
	}
	store.observerMu.Unlock()
	for _, cancel := range revoked {
		cancel(ErrServerPolicyDenied)
	}
}

const serverPolicyColumns = "revision, enable_remote_access, local_network_subnets, ui_culture, updated_at"

func readServerPolicy(row library.OwnedRow) (ServerPolicySnapshot, error) {
	var revision int64
	var values ServerPolicyValues
	var updated time.Time
	err := row.Scan(&revision, &values.EnableRemoteAccess, &values.LocalNetworkSubnets, &values.UICulture, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServerPolicySnapshot{}, fmt.Errorf("%w: server policy singleton is missing", ErrStoredSettings)
	}
	if err != nil {
		return ServerPolicySnapshot{}, err
	}
	return makeServerPolicySnapshot(revision, values, updated)
}

// PreviewServerPolicy validates proposed values and computes the exact access
// result used by the runtime boundary without writing or publishing anything.
func PreviewServerPolicy(values ServerPolicyValues, address string) (local, allowed bool, err error) {
	if err = ValidateServerPolicy(values); err != nil {
		return false, false, err
	}
	snapshot, err := makeServerPolicySnapshot(1, values, time.Time{})
	if err != nil {
		return false, false, err
	}
	return snapshot.IsLocal(address), snapshot.Allows(address), nil
}
