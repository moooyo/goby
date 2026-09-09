package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	maxHLSSessions     = 128
	maxHLSUserSessions = 32
	maxHLSAuthSessions = 16
	maxHLSProducers    = 8
	hlsProducerSpan    = 256
	hlsIdleTTL         = 5 * time.Minute
)

type hlsKey struct {
	scope transcode.Scope
	stamp string
	plan  transcode.Plan
}

type hlsProducer struct {
	id          string
	first, last int
}

type hlsJobs interface {
	Ensure(context.Context, transcode.Spec, *os.File) (transcode.Record, error)
	TryOpen(transcode.Scope, string, string) (*transcode.ReadHandle, error)
	Snapshot(transcode.Scope, string) (transcode.Record, error)
	Open(context.Context, transcode.Scope, string, string) (*transcode.ReadHandle, error)
	CancelJob(string, transcode.Scope) error
	Close(context.Context) error
}

type hlsSession struct {
	mu        sync.Mutex
	id        string
	key       hlsKey
	principal identity.Principal
	output    playback.Source
	startHint int64
	accessed  time.Time
	closed    bool
	timeline  *transcode.Timeline
	lead      int64
	building  chan struct{}
	producers []hlsProducer
	lastAsked int
	ctx       context.Context
	cancel    context.CancelFunc
}

// HLS session IDs identify immutable output revisions, not credentials. Their
// timelines and segment numbers span the complete source, independently of a
// producer that starts later after a seek. Every HTTP use is authorized again.
type hlsRuntime struct {
	server   *Server
	manager  hlsJobs
	verify   func(context.Context, identity.Principal, transcode.Scope, string, transcode.Plan) (*os.File, library.MediaFile, error)
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	sessions map[string]*hlsSession
	byKey    map[hlsKey]*hlsSession
	closing  bool
	requests sync.WaitGroup
	workers  sync.WaitGroup
	once     sync.Once
	done     chan struct{}
	closeErr error
	probes   chan struct{}
	slots    chan struct{}
}

func newHLSRuntime(ctx context.Context, server *Server) (*hlsRuntime, error) {
	if !server.cfg.Transcoding.Enabled {
		return nil, nil
	}
	if _, err := exec.LookPath(server.cfg.FFmpegPath); err != nil {
		return nil, errors.New("the configured FFmpeg executable is unavailable")
	}
	if _, err := exec.LookPath(server.cfg.FFprobePath); err != nil {
		return nil, errors.New("the configured FFprobe executable is unavailable")
	}
	manager, err := transcode.NewManager(ctx, server.cfg.Transcoding.ManagerOptions(server.cfg.FFmpegPath, transcode.NewRepository(server.db)))
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	runtime := &hlsRuntime{server: server, manager: manager, ctx: lifetime, cancel: cancel, sessions: make(map[string]*hlsSession),
		byKey: make(map[hlsKey]*hlsSession), done: make(chan struct{}), probes: make(chan struct{}, 2), slots: make(chan struct{}, 32)}
	runtime.verify = server.authorizeHLS
	runtime.workers.Add(1)
	go runtime.maintain()
	return runtime, nil
}

func (h *hlsRuntime) enter() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.requests.Add(1)
	return true
}

func (h *hlsRuntime) register(principal identity.Principal, source library.MediaFile, playID string, decision playback.ConversionDecision, start int64) (*hlsSession, error) {
	if decision.Plan == nil {
		return nil, transcode.ErrInvalidPlan
	}
	plan := *decision.Plan
	plan.StartTicks = 0
	key := hlsKey{scope: transcode.Scope{UserID: principal.User.ID, AuthSessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
		PlaySessionID: playID, ItemID: source.Item.ID, SourceID: source.SourceID}, stamp: source.ETag, plan: plan}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return nil, transcode.ErrManagerClosed
	}
	if prior := h.byKey[key]; prior != nil {
		prior.mu.Lock()
		closed := prior.closed
		if !closed {
			prior.accessed = time.Now()
		}
		prior.mu.Unlock()
		if !closed {
			return prior, nil
		}
	}
	userCount, authCount := 0, 0
	for _, prior := range h.sessions {
		if prior.key.scope.UserID == key.scope.UserID {
			userCount++
		}
		if prior.key.scope.AuthSessionID == key.scope.AuthSessionID {
			authCount++
		}
	}
	if len(h.sessions) >= maxHLSSessions || userCount >= maxHLSUserSessions || authCount >= maxHLSAuthSessions {
		return nil, transcode.ErrBusy
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(h.ctx)
	session := &hlsSession{id: hex.EncodeToString(random[:]), key: key, principal: principal, output: decision.OutputSource,
		startHint: start, accessed: time.Now(), lastAsked: -1, ctx: ctx, cancel: cancel}
	h.sessions[session.id], h.byKey[key] = session, session
	return session, nil
}

func (h *hlsRuntime) find(id string, principal identity.Principal, itemID string) (*hlsSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessions[id]
	if h.closing || session == nil || session.key.scope.UserID != principal.User.ID ||
		session.key.scope.AuthSessionID != principal.SessionID || session.key.scope.DeviceID != principal.Client.DeviceID ||
		session.key.scope.ItemID != itemID {
		return nil, transcode.ErrJobNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil, transcode.ErrJobNotFound
	}
	session.accessed = time.Now()
	return session, nil
}

func (h *hlsRuntime) retire(session *hlsSession) {
	h.mu.Lock()
	if h.sessions[session.id] == session {
		delete(h.sessions, session.id)
		if h.byKey[session.key] == session {
			delete(h.byKey, session.key)
		}
	}
	session.mu.Lock()
	session.closed = true
	session.cancel()
	producers := append([]hlsProducer(nil), session.producers...)
	session.mu.Unlock()
	h.mu.Unlock()
	for _, producer := range producers {
		_ = h.manager.CancelJob(producer.id, session.key.scope)
	}
}

func (h *hlsRuntime) cancelMatching(authID, playID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	var matching []*hlsSession
	for _, session := range h.sessions {
		if session.key.scope.AuthSessionID == authID && (playID == "" || session.key.scope.PlaySessionID == playID) {
			matching = append(matching, session)
		}
	}
	h.mu.Unlock()
	for _, session := range matching {
		h.retire(session)
	}
}

func (h *hlsRuntime) touchMatching(authID, playID string) {
	if h == nil || playID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, session := range h.sessions {
		if session.key.scope.AuthSessionID == authID && session.key.scope.PlaySessionID == playID {
			session.mu.Lock()
			if !session.closed {
				session.accessed = time.Now()
			}
			session.mu.Unlock()
		}
	}
}

func permanentHLSError(err error) bool {
	return errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, library.ErrForbidden) || errors.Is(err, library.ErrNotFound) || errors.Is(err, library.ErrSourceChanged)
}

func (s *Server) authorizeHLS(ctx context.Context, principal identity.Principal, scope transcode.Scope, stamp string, plan transcode.Plan) (*os.File, library.MediaFile, error) {
	fresh, err := s.identity.RevalidateSession(ctx, principal)
	if err != nil {
		return nil, library.MediaFile{}, err
	}
	if fresh.User.ID != scope.UserID || fresh.SessionID != scope.AuthSessionID || fresh.Client.DeviceID != scope.DeviceID {
		return nil, library.MediaFile{}, library.ErrNotFound
	}
	limits := hlsUserLimits(s.cfg.Transcoding, fresh.User)
	if !hlsPlanAllowed(plan, limits) {
		return nil, library.MediaFile{}, library.ErrForbidden
	}
	play, err := s.library.GetPlaybackSession(ctx, playbackOwner(fresh), scope.PlaySessionID)
	if err != nil {
		return nil, library.MediaFile{}, err
	}
	if play.ItemID != scope.ItemID || play.MediaSourceID != scope.SourceID || !time.Now().Before(play.ExpiresAt) ||
		(play.State != "Prepared" && play.State != "Playing" && play.State != "Paused") {
		return nil, library.MediaFile{}, library.ErrNotFound
	}
	file, source, err := s.library.OpenMedia(ctx, scope.UserID, scope.ItemID, scope.SourceID)
	if err != nil {
		return nil, library.MediaFile{}, err
	}
	if stamp != "" && stamp != source.ETag {
		_ = file.Close()
		return nil, library.MediaFile{}, library.ErrNotFound
	}
	return file, source, nil
}

func hlsPlanAllowed(plan transcode.Plan, limits playback.ConversionLimits) bool {
	videoEncoded := plan.VideoCodec != "" && plan.VideoCodec != "copy"
	audioEncoded := plan.AudioCodec != "" && plan.AudioCodec != "copy"
	return (!videoEncoded || limits.AllowVideoTranscode) && (!audioEncoded || limits.AllowAudioTranscode) &&
		(videoEncoded || audioEncoded || limits.AllowRemux)
}

func (h *hlsRuntime) timeline(ctx context.Context, session *hlsSession, input *os.File) (transcode.Timeline, error) {
	for {
		session.mu.Lock()
		if session.closed {
			session.mu.Unlock()
			return transcode.Timeline{}, transcode.ErrJobNotFound
		}
		if session.timeline != nil {
			timeline := *session.timeline
			session.mu.Unlock()
			return timeline, nil
		}
		if pending := session.building; pending != nil {
			session.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return transcode.Timeline{}, ctx.Err()
			case <-session.ctx.Done():
				return transcode.Timeline{}, transcode.ErrJobNotFound
			}
		}
		pending := make(chan struct{})
		session.building = pending
		session.mu.Unlock()
		workCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		stop := context.AfterFunc(session.ctx, cancel)
		var keys []int64
		var err error
		plan := session.key.plan
		copiedVideo := plan.VideoCodec == "copy"
		if copiedVideo {
			select {
			case h.probes <- struct{}{}:
				keys, err = transcode.Keyframes(workCtx, h.server.cfg.FFprobePath, input, plan.VideoStreamIndex, plan.DurationTicks)
				<-h.probes
			case <-workCtx.Done():
				err = workCtx.Err()
			}
		}
		var timeline transcode.Timeline
		if err == nil {
			timeline, err = transcode.BuildTimeline(plan.DurationTicks, plan.SegmentSeconds, keys, copiedVideo)
		}
		stop()
		cancel()
		session.mu.Lock()
		if session.closed {
			err = transcode.ErrJobNotFound
		}
		if err == nil {
			session.timeline = &timeline
			if copiedVideo {
				session.lead = keys[0]
			}
		}
		session.building = nil
		close(pending)
		session.mu.Unlock()
		return timeline, err
	}
}

// segment consumes input in every case. Nearby sequential requests wait for an
// existing producer; distant seeks start an immutable revision at the requested
// source boundary. Earlier published files may remain reusable until eviction.
func (h *hlsRuntime) segment(ctx context.Context, session *hlsSession, input *os.File, number int) (*transcode.ReadHandle, error) {
	owned := false
	defer func() {
		if !owned {
			_ = input.Close()
		}
	}()
	timeline, err := h.timeline(ctx, session, input)
	if err != nil {
		return nil, err
	}
	if number < 0 || number >= len(timeline.Segments) {
		return nil, transcode.ErrJobNotFound
	}
	name := "segment-" + paddedSegmentNumber(number) + ".ts"
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil, transcode.ErrJobNotFound
	}
	for index := len(session.producers) - 1; index >= 0; index-- {
		producer := session.producers[index]
		if number < producer.first || number > producer.last {
			continue
		}
		if handle, openErr := h.manager.TryOpen(session.key.scope, producer.id, name); openErr == nil {
			session.lastAsked, session.accessed = number, time.Now()
			session.mu.Unlock()
			return handle, nil
		}
		state, stateErr := h.manager.Snapshot(session.key.scope, producer.id)
		if stateErr == nil && (state.State == "queued" || state.State == "running") &&
			number >= session.lastAsked-3 && number <= session.lastAsked+3 {
			session.lastAsked, session.accessed = number, time.Now()
			session.mu.Unlock()
			return h.manager.Open(ctx, session.key.scope, producer.id, name)
		}
	}
	for _, producer := range session.producers {
		state, stateErr := h.manager.Snapshot(session.key.scope, producer.id)
		if stateErr == nil && (state.State == "queued" || state.State == "running") {
			_ = h.manager.CancelJob(producer.id, session.key.scope)
		}
	}
	plan := session.key.plan
	last := min(len(timeline.Segments)-1, number+hlsProducerSpan-1)
	cuts, err := timeline.BoundaryTicks(number, last)
	if err != nil {
		session.mu.Unlock()
		return nil, err
	}
	encodedCuts := make([]string, len(cuts))
	for index, cut := range cuts {
		encodedCuts[index] = strconv.FormatInt(cut, 10)
	}
	plan.SegmentMode, plan.SegmentStartNumber, plan.StartTicks = "vod", number, timeline.Segments[number].StartTicks
	plan.EndTicks = timeline.Segments[last].StartTicks + timeline.Segments[last].DurationTicks
	plan.SegmentTimes = strings.Join(encodedCuts, ",")
	if number == 0 {
		plan.ReferenceStartTicks = session.lead
	}
	record, err := h.manager.Ensure(ctx, transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: plan}, input)
	owned = true
	if err != nil {
		session.mu.Unlock()
		return nil, err
	}
	if len(session.producers) >= maxHLSProducers {
		oldest := session.producers[0]
		_ = h.manager.CancelJob(oldest.id, session.key.scope)
		session.producers = append(session.producers[:0], session.producers[1:]...)
	}
	session.producers = append(session.producers, hlsProducer{id: record.ID, first: number, last: last})
	session.lastAsked, session.accessed = number, time.Now()
	session.mu.Unlock()
	return h.manager.Open(ctx, session.key.scope, record.ID, name)
}

func paddedSegmentNumber(number int) string {
	value := strconv.Itoa(number)
	if len(value) < 6 {
		value = strings.Repeat("0", 6-len(value)) + value
	}
	return value
}

func (h *hlsRuntime) maintain() {
	defer h.workers.Done()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-tick.C:
		}
		h.mu.Lock()
		sessions := make([]*hlsSession, 0, len(h.sessions))
		for _, session := range h.sessions {
			sessions = append(sessions, session)
		}
		h.mu.Unlock()
		cycle, cancel := context.WithTimeout(h.ctx, 4*time.Second)
		h.maintainSessions(cycle, sessions)
		cancel()
	}
}

func (h *hlsRuntime) maintainSessions(cycle context.Context, sessions []*hlsSession) {
	for _, session := range sessions {
		if cycle.Err() != nil {
			break
		}
		session.mu.Lock()
		idle, active := time.Since(session.accessed) > hlsIdleTTL, len(session.producers) > 0
		session.mu.Unlock()
		if idle {
			h.retire(session)
			continue
		}
		if !active {
			continue
		}
		check, stop := context.WithTimeout(cycle, 750*time.Millisecond)
		file, _, err := h.verify(check, session.principal, session.key.scope, session.key.stamp, session.key.plan)
		stop()
		if file != nil {
			_ = file.Close()
		}
		if permanentHLSError(err) {
			h.retire(session)
		}
	}
}

func (h *hlsRuntime) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.once.Do(func() {
		h.mu.Lock()
		h.closing = true
		h.cancel()
		h.mu.Unlock()
		go func() {
			h.closeErr = h.manager.Close(context.Background())
			h.requests.Wait()
			h.workers.Wait()
			close(h.done)
		}()
	})
	select {
	case <-h.done:
		return h.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
