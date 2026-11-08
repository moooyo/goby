package updates

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrUnconfigured   = errors.New("Goby update source is not configured")
	ErrCheckFailed    = errors.New("Goby update check failed")
	ErrBusy           = errors.New("Goby update check is already running")
	ErrVersionUnknown = errors.New("running Goby product version cannot be compared")
)

type State string

const (
	Unconfigured   State = "unconfigured"
	NotChecked     State = "not_checked"
	Failed         State = "failed"
	Current        State = "current"
	Available      State = "available"
	VersionUnknown State = "version_unknown"
)

// View is a process-local observation. Restart discards it and never implies
// that the running Docker image has been replaced or verified by Goby.
type View struct {
	CanSelfUpdate    bool
	Delivery         string
	ConfiguredSource bool
	CurrentVersion   string
	State            State
	CheckedAt        *time.Time
	Release          *Release
	ErrorCode        *string
}

type Options struct {
	CurrentVersion string
	FeedURL        string
	// Transport is an explicit embedding/fixture dependency. Production callers
	// leave it nil; no request or environment setting can replace the transport.
	Transport http.RoundTripper
	Now       func() time.Time
}

type Service struct {
	feedURL string
	client  *http.Client
	now     func() time.Time
	gate    chan struct{}
	mu      sync.RWMutex
	view    View
}

func New(options Options) (*Service, error) {
	if options.FeedURL != "" {
		if err := ValidateFeedURL(options.FeedURL); err != nil {
			return nil, err
		}
	}
	version := options.CurrentVersion
	if len(version) > 128 || !utf8.ValidString(version) || containsControl(version) {
		version = ""
	}
	service := &Service{
		feedURL: options.FeedURL, gate: make(chan struct{}, 1), now: options.Now,
		view: View{Delivery: "docker", ConfiguredSource: options.FeedURL != "", CurrentVersion: version, State: Unconfigured},
	}
	if service.now == nil {
		service.now = time.Now
	}
	if options.FeedURL != "" {
		service.view.State = NotChecked
		transport := options.Transport
		if transport == nil {
			parsed, _ := parsePublicHTTPSURL(options.FeedURL)
			transport = newFeedTransport(parsed.Hostname())
		}
		service.client = &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return service, nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func cloneView(value View) View {
	if value.CheckedAt != nil {
		copied := *value.CheckedAt
		value.CheckedAt = &copied
	}
	if value.Release != nil {
		copied := *value.Release
		value.Release = &copied
	}
	if value.ErrorCode != nil {
		copied := *value.ErrorCode
		value.ErrorCode = &copied
	}
	return value
}

func (service *Service) Status() View {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return cloneView(service.view)
}

// Check performs at most one network observation at a time. Authorization is
// checked before I/O and again immediately before publishing its result. No SQL
// transaction or authority lock is held while a remote server responds.
func (service *Service) Check(ctx context.Context, authorize func(context.Context) error) (View, error) {
	if authorize == nil {
		return service.Status(), errors.New("an update check requires current administrator authority")
	}
	if err := authorize(ctx); err != nil {
		return service.Status(), err
	}
	if service.feedURL == "" {
		return service.Status(), ErrUnconfigured
	}
	select {
	case service.gate <- struct{}{}:
	default:
		return service.Status(), ErrBusy
	}
	defer func() { <-service.gate }()
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	release, err := service.fetch(checkContext)
	cancel()
	if ctx.Err() != nil {
		return service.Status(), ctx.Err()
	}
	if authorityErr := authorize(ctx); authorityErr != nil {
		return service.Status(), authorityErr
	}
	result := service.Status()
	checkedAt := service.now().UTC()
	result.CheckedAt, result.Release, result.ErrorCode = &checkedAt, nil, nil
	if err != nil {
		code := "update_check_failed"
		result.State, result.ErrorCode = Failed, &code
		err = ErrCheckFailed
	} else {
		result.Release = &release
		currentVersion, ok := parseVersion(result.CurrentVersion)
		if !ok {
			code := "current_version_unknown"
			result.State, result.ErrorCode, err = VersionUnknown, &code, ErrVersionUnknown
		} else {
			publishedVersion, _ := parseVersion(release.Version)
			result.State = Current
			if compareVersions(publishedVersion, currentVersion) > 0 {
				result.State = Available
			}
		}
	}
	service.mu.Lock()
	service.view = cloneView(result)
	service.mu.Unlock()
	return cloneView(result), err
}

// HasUpdateAvailable is deliberately unknown until a successful comparison.
// A later failed observation removes the previous compatibility assertion.
func (service *Service) HasUpdateAvailable() (bool, bool) {
	view := service.Status()
	return view.State == Available, view.State == Current || view.State == Available
}

func (service *Service) Close() {
	if service.client != nil {
		service.client.CloseIdleConnections()
	}
}
