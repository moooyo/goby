// Package dynamicsource owns authenticated leases for configured streaming inputs.
package dynamicsource

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
)

var (
	ErrInvalid     = errors.New("invalid dynamic source request")
	ErrNotFound    = errors.New("dynamic source not found")
	ErrBusy        = errors.New("dynamic source capacity exhausted")
	ErrClosed      = errors.New("dynamic source manager closed")
	ErrUnavailable = errors.New("dynamic source unavailable")
)

// Definition is trusted startup configuration, never a playback request DTO.
// URL and Headers must never be included in a public source description.
type Definition struct {
	ItemID        string            `json:"itemId"`
	Name          string            `json:"name,omitempty"`
	URL           string            `json:"url"`
	Headers       map[string]string `json:"headers,omitempty"`
	Infinite      bool              `json:"infinite,omitempty"`
	MaxReconnects int               `json:"maxReconnects,omitempty"`
}

func (Definition) String() string   { return "<dynamic-source configuration>" }
func (Definition) GoString() string { return "<dynamic-source configuration>" }

type Owner struct {
	UserID, SessionID, DeviceID, ApplicationClientID string
	// PeerIP retains the authenticated transport address for policy rechecks.
	PeerIP         string
	ApplicationKey bool
}

// Identity excludes request transport metadata from credential ownership.
// A permitted address change must not strand an existing lease or bypass its
// owner quota; each authorization still receives the current request's PeerIP.
func (owner Owner) Identity() Owner {
	owner.PeerIP = ""
	return owner
}

type Description struct {
	ItemID, SourceID, OpenToken, Name, ItemType, Protocol string
	Info                                                  media.Info
	Infinite                                              bool
}

type Lease struct {
	Description
	ID, PlaySessionID, Stamp string
	Generation               uint64
}

type OpenRequest struct {
	ItemID, OpenToken, PlaySessionID string
}

// Connection owns a newly opened upstream stream and its observed media facts.
type Connection struct {
	Reader io.ReadCloser
	Info   media.Info
}

// Connector implementations must honor context cancellation before and after
// Open returns. Manager closes every connection it does not hand to a caller.
type Connector interface {
	Open(context.Context, Definition) (*Connection, error)
}

type Options struct {
	Authorize                             func(context.Context, Owner, string, string) error
	Connector                             Connector
	Prober                                media.Prober
	TempDir                               string
	OpenTimeout, IdleTimeout              time.Duration
	MaxLeases, MaxOwnerLeases, MaxOpening int
}

type leaseKey struct {
	owner          Owner
	itemID, playID string
}

type leaseState struct {
	lease           Lease
	key             leaseKey
	definition      Definition
	ctx             context.Context
	cancel          context.CancelFunc
	ready           chan struct{}
	err             error
	connection      *Connection
	input           *Input
	opening, closed bool
	reconnects      int
	accessed        time.Time
}

type Manager struct {
	mu             sync.Mutex
	options        Options
	definitions    map[string]Definition
	descriptions   map[string]Description
	byToken        map[string]string
	leases         map[string]*leaseState
	byKey          map[leaseKey]*leaseState
	ctx            context.Context
	cancel         context.CancelFunc
	closing        bool
	slots          chan struct{}
	workers        sync.WaitGroup
	done           chan struct{}
	ownedConnector *HTTPConnector
}

func validID(value string, optional bool) bool {
	return (optional || value != "") && len(value) <= 256 && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validOwner(owner Owner) bool {
	return validID(owner.SessionID, false) && validID(owner.DeviceID, true) &&
		((owner.ApplicationKey && owner.UserID == "" && validID(owner.ApplicationClientID, false)) ||
			(!owner.ApplicationKey && validID(owner.UserID, false) && owner.ApplicationClientID == ""))
}

func newID(prefix string) (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value[:]), nil
}

func New(ctx context.Context, definitions []Definition, options Options) (*Manager, error) {
	if options.Authorize == nil || len(definitions) > 256 {
		return nil, ErrInvalid
	}
	if options.OpenTimeout <= 0 {
		options.OpenTimeout = 30 * time.Second
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = 5 * time.Minute
	}
	if options.MaxLeases <= 0 {
		options.MaxLeases = 128
	}
	if options.MaxOwnerLeases <= 0 {
		options.MaxOwnerLeases = 16
	}
	if options.MaxOpening <= 0 {
		options.MaxOpening = 4
	}
	if options.OpenTimeout > time.Minute || options.IdleTimeout > time.Hour || options.MaxLeases > 1024 || options.MaxOpening > 32 {
		return nil, ErrInvalid
	}
	var ownedConnector *HTTPConnector
	if options.Connector == nil {
		ownedConnector = NewHTTPConnector(options.Prober, options.TempDir)
		options.Connector = ownedConnector
	}
	lifetime, cancel := context.WithCancel(ctx)
	m := &Manager{options: options, definitions: make(map[string]Definition), descriptions: make(map[string]Description),
		byToken: make(map[string]string), leases: make(map[string]*leaseState), byKey: make(map[leaseKey]*leaseState),
		ctx: lifetime, cancel: cancel, slots: make(chan struct{}, options.MaxOpening), done: make(chan struct{}), ownedConnector: ownedConnector}
	for _, definition := range definitions {
		if err := validateDefinition(definition); err != nil {
			cancel()
			return nil, err
		}
		if _, exists := m.definitions[definition.ItemID]; exists {
			cancel()
			return nil, ErrInvalid
		}
		definition.Headers = cloneHeaders(definition.Headers)
		token, err := newID("open_")
		if err != nil {
			cancel()
			return nil, err
		}
		m.definitions[definition.ItemID] = definition
		m.descriptions[definition.ItemID] = Description{ItemID: definition.ItemID, SourceID: media.SourceID(definition.ItemID),
			OpenToken: token, Name: definition.Name, Protocol: "Http", Infinite: definition.Infinite}
		m.byToken[token] = definition.ItemID
	}
	m.workers.Add(1)
	go m.maintain()
	return m, nil
}

func (m *Manager) Describe(ctx context.Context, owner Owner, itemID, sourceID string) (Description, error) {
	if m == nil {
		return Description{}, ErrNotFound
	}
	if !validOwner(owner) || !validID(itemID, false) {
		return Description{}, ErrInvalid
	}
	m.mu.Lock()
	description, exists := m.descriptions[itemID]
	closing := m.closing
	m.mu.Unlock()
	if closing {
		return Description{}, ErrClosed
	}
	if !exists || sourceID != "" && sourceID != description.SourceID {
		return Description{}, ErrNotFound
	}
	if err := m.options.Authorize(ctx, owner, itemID, ""); err != nil {
		return Description{}, err
	}
	return description, nil
}

// DescribeToken resolves an opaque opening hint only after current catalog
// authorization. It is not a URL, a credential, or a replacement for access.
func (m *Manager) DescribeToken(ctx context.Context, owner Owner, token, itemID string) (Description, error) {
	if m == nil || !validID(token, false) {
		return Description{}, ErrNotFound
	}
	m.mu.Lock()
	resolved := m.byToken[token]
	m.mu.Unlock()
	if resolved == "" || itemID != "" && itemID != resolved {
		return Description{}, ErrNotFound
	}
	return m.Describe(ctx, owner, resolved, "")
}

// Open reserves a lease before connecting, so concurrent retries share one
// bounded opening operation. A canceled opener cancels that operation; a retry
// receives a new lease instead of reviving the canceled generation.
func (m *Manager) Open(ctx context.Context, owner Owner, request OpenRequest) (Lease, error) {
	if m == nil {
		return Lease{}, ErrNotFound
	}
	if !validOwner(owner) || !validID(request.OpenToken, false) || !validID(request.PlaySessionID, false) {
		return Lease{}, ErrInvalid
	}
	m.mu.Lock()
	itemID := m.byToken[request.OpenToken]
	description := m.descriptions[itemID]
	m.mu.Unlock()
	if itemID == "" || request.ItemID != "" && request.ItemID != itemID || subtle.ConstantTimeCompare([]byte(description.OpenToken), []byte(request.OpenToken)) != 1 {
		return Lease{}, ErrNotFound
	}
	if err := m.options.Authorize(ctx, owner, itemID, request.PlaySessionID); err != nil {
		return Lease{}, err
	}
	key := leaseKey{owner: owner.Identity(), itemID: itemID, playID: request.PlaySessionID}
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return Lease{}, ErrClosed
	}
	if previous := m.byKey[key]; previous != nil && !previous.closed {
		if previous.definition.Infinite && !previous.opening && previous.connection == nil && previous.input == nil {
			// An exhausted ongoing input starts a new presentation on explicit
			// Open. Reusing its old lease could repeat segment numbers silently.
			m.closeLocked(previous)
		} else {
			ready := previous.ready
			m.mu.Unlock()
			select {
			case <-ctx.Done():
				return Lease{}, ctx.Err()
			case <-ready:
			}
			return m.Info(ctx, owner, previous.lease.ID)
		}
	}
	active, owned := 0, 0
	for _, state := range m.leases {
		if !state.closed {
			active++
			if state.key.owner == owner.Identity() {
				owned++
			}
		}
	}
	if active >= m.options.MaxLeases || owned >= m.options.MaxOwnerLeases || len(m.leases) >= 2*m.options.MaxLeases {
		m.mu.Unlock()
		return Lease{}, ErrBusy
	}
	id, err := newID("live_")
	if err != nil {
		m.mu.Unlock()
		return Lease{}, ErrUnavailable
	}
	lifetime, cancel := context.WithCancel(m.ctx)
	state := &leaseState{lease: Lease{Description: description, ID: id, PlaySessionID: request.PlaySessionID, Generation: 1, Stamp: id + "-1"},
		key: key, definition: m.definitions[itemID], ctx: lifetime, cancel: cancel, ready: make(chan struct{}), opening: true, accessed: time.Now()}
	m.leases[id], m.byKey[key] = state, state
	m.workers.Add(1)
	m.mu.Unlock()
	defer m.workers.Done()
	connection, err := m.connect(ctx, state)
	if err == nil {
		err = m.options.Authorize(ctx, owner, itemID, request.PlaySessionID)
	}
	m.mu.Lock()
	state.opening = false
	if state.closed || m.closing {
		if err == nil {
			err = ErrClosed
		}
	}
	if err != nil {
		state.err = err
		m.closeLocked(state)
	} else {
		state.connection = connection
		state.lease.Info = cloneInfo(connection.Info)
		state.lease.ItemType = streamItemType(connection.Info)
	}
	close(state.ready)
	result := cloneLease(state.lease)
	m.mu.Unlock()
	if err != nil && connection != nil {
		_ = connection.Reader.Close()
	}
	return result, err
}

func (m *Manager) connect(ctx context.Context, state *leaseState) (*Connection, error) {
	opening, cancel := context.WithTimeout(ctx, m.options.OpenTimeout)
	defer cancel()
	stop := context.AfterFunc(state.ctx, cancel)
	defer stop()
	select {
	case m.slots <- struct{}{}:
	case <-opening.Done():
		return nil, opening.Err()
	}
	defer func() { <-m.slots }()
	for {
		// A connection outlives this opening request. Cancellation is attached
		// only while opening and permanently to the lease itself.
		connectionCtx, connectionCancel := context.WithCancel(state.ctx)
		stopOpening := context.AfterFunc(opening, connectionCancel)
		connection, err := m.options.Connector.Open(connectionCtx, state.definition)
		stopped := stopOpening()
		if opening.Err() != nil || !stopped {
			connectionCancel()
			if connection != nil && connection.Reader != nil {
				_ = connection.Reader.Close()
			}
			return nil, opening.Err()
		}
		if err == nil && (connection == nil || connection.Reader == nil || len(connection.Info.Streams) == 0) {
			err = ErrUnavailable
		}
		if err == nil {
			connection.Reader = &cancelReader{ReadCloser: connection.Reader, cancel: connectionCancel}
			return connection, nil
		}
		connectionCancel()
		if connection != nil && connection.Reader != nil {
			_ = connection.Reader.Close()
		}
		m.mu.Lock()
		if state.reconnects >= state.definition.MaxReconnects {
			m.mu.Unlock()
			return nil, ErrUnavailable
		}
		state.reconnects++
		delay := time.Duration(state.reconnects) * 200 * time.Millisecond
		m.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-opening.Done():
			timer.Stop()
			return nil, opening.Err()
		case <-timer.C:
		}
	}
}

func (m *Manager) Info(ctx context.Context, owner Owner, id string) (Lease, error) {
	if m == nil || !validOwner(owner) || !validID(id, false) {
		return Lease{}, ErrNotFound
	}
	m.mu.Lock()
	state := m.leases[id]
	if m.closing || state == nil || state.closed || state.key.owner != owner.Identity() {
		m.mu.Unlock()
		return Lease{}, ErrNotFound
	}
	if state.opening {
		m.mu.Unlock()
		return Lease{}, ErrBusy
	}
	lease := cloneLease(state.lease)
	m.mu.Unlock()
	if err := m.options.Authorize(ctx, owner, lease.ItemID, lease.PlaySessionID); err != nil {
		_ = m.CloseLease(ctx, owner, id)
		return Lease{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if state.closed {
		return Lease{}, ErrNotFound
	}
	state.accessed = time.Now()
	return lease, nil
}

// Acquire gives one reader exclusive ownership of the current connection.
// Reopening never silently joins unrelated byte timelines: it returns a new
// generation and stamp, which the caller must use for a fresh encoder job.
func (m *Manager) Acquire(ctx context.Context, owner Owner, id string) (*Input, error) {
	if _, err := m.Info(ctx, owner, id); err != nil {
		return nil, err
	}
	m.mu.Lock()
	state := m.leases[id]
	if state == nil || state.closed {
		m.mu.Unlock()
		return nil, ErrNotFound
	}
	if state.opening || state.input != nil {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	connection := state.connection
	if connection == nil {
		if state.reconnects >= state.definition.MaxReconnects {
			m.mu.Unlock()
			return nil, ErrUnavailable
		}
		state.reconnects++
		state.opening = true
		m.workers.Add(1)
		defer m.workers.Done()
		m.mu.Unlock()
		var err error
		connection, err = m.connect(ctx, state)
		if err == nil {
			err = m.options.Authorize(ctx, owner, state.lease.ItemID, state.lease.PlaySessionID)
		}
		m.mu.Lock()
		state.opening = false
		if err != nil || state.closed {
			m.mu.Unlock()
			if connection != nil {
				_ = connection.Reader.Close()
			}
			if err == nil {
				err = ErrNotFound
			}
			return nil, err
		}
		state.lease.Generation++
		state.lease.Stamp = fmt.Sprintf("%s-%d", state.lease.ID, state.lease.Generation)
		state.lease.Info = cloneInfo(connection.Info)
		state.lease.ItemType = streamItemType(connection.Info)
	}
	state.connection = nil
	input := &Input{Lease: cloneLease(state.lease), reader: connection.Reader, ctx: state.ctx}
	input.release = func() {
		m.mu.Lock()
		if state.input == input {
			state.input = nil
			state.accessed = time.Now()
		}
		m.mu.Unlock()
	}
	state.input = input
	m.mu.Unlock()
	return input, nil
}

func (m *Manager) closeLocked(state *leaseState) {
	if state.closed {
		return
	}
	state.closed = true
	state.accessed = time.Now()
	state.cancel()
	delete(m.byKey, state.key)
	// Readers close asynchronously to avoid calling transport code under mu.
	if state.connection != nil {
		reader := state.connection.Reader
		state.connection = nil
		m.workers.Add(1)
		go func() { defer m.workers.Done(); _ = reader.Close() }()
	}
	if state.input != nil {
		input := state.input
		m.workers.Add(1)
		go func() { defer m.workers.Done(); _ = input.Close() }()
	}
}

func (m *Manager) CloseLease(ctx context.Context, owner Owner, id string) error {
	if m == nil || !validOwner(owner) {
		return ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.leases[id]
	if state == nil || state.key.owner != owner.Identity() {
		return ErrNotFound
	}
	m.closeLocked(state)
	return nil
}

// CancelMatching releases sources after logout, revocation, or stopped playback.
func (m *Manager) CancelMatching(sessionID, playID string) {
	if m == nil || sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, state := range m.leases {
		if state.key.owner.SessionID == sessionID && (playID == "" || state.key.playID == playID) {
			m.closeLocked(state)
		}
	}
}

// PlaybackReference restores an application's already authenticated client
// context from its own lease. The identifier alone grants no source access;
// the caller must authenticate the credential and resolve this playback in the
// identity store before using the complete Owner with Info or CloseLease.
func (m *Manager) PlaybackReference(owner Owner, id string) (string, error) {
	if m == nil || !owner.ApplicationKey || !validID(owner.SessionID, false) || !validID(id, false) {
		return "", ErrNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.leases[id]
	if state == nil || state.key.owner.SessionID != owner.SessionID || !state.key.owner.ApplicationKey {
		return "", ErrNotFound
	}
	return state.lease.PlaySessionID, nil
}

func (m *Manager) maintain() {
	defer m.workers.Done()
	ticker := time.NewTicker(max(time.Millisecond, min(m.options.IdleTimeout/2, time.Minute)))
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case now := <-ticker.C:
			m.mu.Lock()
			for id, state := range m.leases {
				if now.Sub(state.accessed) < m.options.IdleTimeout {
					continue
				}
				if state.closed && !state.opening {
					delete(m.leases, id)
				} else {
					m.closeLocked(state)
				}
			}
			m.mu.Unlock()
		}
	}
}

func (m *Manager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if !m.closing {
		m.closing = true
		for _, state := range m.leases {
			m.closeLocked(state)
		}
		m.cancel()
		go func() {
			m.workers.Wait()
			if m.ownedConnector != nil {
				_ = m.ownedConnector.Close()
			}
			close(m.done)
		}()
	}
	m.mu.Unlock()
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cloneInfo(info media.Info) media.Info {
	info.Streams = append([]media.Stream(nil), info.Streams...)
	for index := range info.Streams {
		if info.Streams[index].AudioTiming != nil {
			timing := *info.Streams[index].AudioTiming
			info.Streams[index].AudioTiming = &timing
		}
	}
	info.Chapters = append([]media.Chapter(nil), info.Chapters...)
	info.VideoSeekIndexes = nil
	info.EmbeddedMusic = nil
	return info
}

func cloneLease(lease Lease) Lease { lease.Info = cloneInfo(lease.Info); return lease }

func streamItemType(info media.Info) string {
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture {
			return "Video"
		}
	}
	return "Audio"
}

type cancelReader struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *cancelReader) Close() error { r.cancel(); return r.ReadCloser.Close() }

// Input closes its upstream transport and releases the lease reader together.
type Input struct {
	Lease
	reader  io.ReadCloser
	ctx     context.Context
	release func()
	once    sync.Once
	pipeMu  sync.Mutex
	pipe    *os.File
	piped   bool
	closed  bool
}

func (input *Input) Read(data []byte) (int, error) {
	input.pipeMu.Lock()
	closed, piped := input.closed, input.piped
	input.pipeMu.Unlock()
	if closed {
		return 0, ErrClosed
	}
	if piped {
		return 0, ErrBusy
	}
	return input.reader.Read(data)
}

func (input *Input) Close() error {
	var err error
	input.once.Do(func() {
		input.pipeMu.Lock()
		input.closed = true
		if input.pipe != nil {
			_ = input.pipe.Close()
		}
		input.pipeMu.Unlock()
		err = input.reader.Close()
		input.release()
	})
	return err
}

// OpenPipe hands a real anonymous descriptor to the conversion manager. It
// feeds only already authorized bytes; FFmpeg never resolves a remote URL.
// The recipient consumes the read descriptor. Closing it releases this input.
func (input *Input) OpenPipe() (*os.File, error) {
	input.pipeMu.Lock()
	if input.closed {
		input.pipeMu.Unlock()
		return nil, ErrClosed
	}
	if input.piped {
		input.pipeMu.Unlock()
		return nil, ErrBusy
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		input.pipeMu.Unlock()
		return nil, err
	}
	input.piped, input.pipe = true, writer
	input.pipeMu.Unlock()
	go func() {
		stop := context.AfterFunc(input.ctx, func() { _ = writer.Close(); _ = input.reader.Close() })
		_, _ = io.Copy(writer, input.reader)
		stop()
		_ = writer.Close()
		_ = input.Close()
	}()
	return reader, nil
}
