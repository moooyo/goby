package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	maxDiagnosticAttrs = 64
	maxDiagnosticDepth = 6
	maxInspectedAttrs  = 128
)

var (
	errDiagnosticEncoding  = errors.New("diagnostic record encoding failed")
	errDiagnosticFallback  = errors.New("diagnostic fallback handler failed")
	errDiagnosticRecursion = errors.New("recursive diagnostic logging rejected")
	buildVersionPattern    = regexp.MustCompile(`^v?(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})(-(dev|alpha|beta|rc)(\.(0|[1-9][0-9]{0,5}))?)?(\+[0-9a-f]{7,40})?$`)
)

type diagnosticContextKey struct{}

type diagnosticBinding struct {
	depth int
	attrs []slog.Attr
}

type diagnosticHandler struct {
	store    *Store
	fallback slog.Handler
	bindings []diagnosticBinding
	groups   []string
	bound    int
	blocked  bool
}

// NewHandler sanitizes records before both persistence and fallback output.
// A configured store enables Info and higher levels; other levels follow the
// fallback's policy. Enabled records go to both outputs, and a store failure
// does not prevent safe fallback output.
func NewHandler(store *Store, fallback slog.Handler) slog.Handler {
	return &diagnosticHandler{store: store, fallback: fallback}
}

func (h *diagnosticHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(diagnosticContextKey{}) != nil {
		return false
	}
	return h.store != nil && level >= slog.LevelInfo || fallbackEnabled(h.fallback, context.WithValue(ctx, diagnosticContextKey{}, true), level)
}

func (h *diagnosticHandler) Handle(ctx context.Context, record slog.Record) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Value(diagnosticContextKey{}) != nil {
		return errDiagnosticRecursion
	}
	spec := diagnosticEventFor(record.Message)
	levels := make([][]slog.Attr, len(h.groups)+1)
	budget := maxDiagnosticAttrs
	for _, binding := range h.bindings {
		levels[binding.depth] = append(levels[binding.depth], filterDiagnosticAttrs(binding.attrs, spec.fields, &budget)...)
	}
	if !h.blocked && budget > 0 {
		attrs := make([]slog.Attr, 0, min(record.NumAttrs(), maxInspectedAttrs))
		record.Attrs(func(attr slog.Attr) bool {
			attrs = append(attrs, attr)
			return len(attrs) < maxInspectedAttrs
		})
		freezeBudget := budget
		frozen := freezeDiagnosticAttrs(attrs, len(h.groups), &freezeBudget)
		levels[len(h.groups)] = append(levels[len(h.groups)], filterDiagnosticAttrs(frozen, spec.fields, &budget)...)
	}
	for depth := len(h.groups); depth > 0; depth-- {
		if len(levels[depth]) > 0 {
			levels[depth-1] = append(levels[depth-1], slog.GroupAttrs(h.groups[depth-1], levels[depth]...))
		}
	}
	stamp := record.Time.UTC()
	if stamp.Year() < 0 || stamp.Year() > 9999 {
		stamp = time.Time{}
	}
	attrs := levels[0]
	var safe slog.Record
	var line []byte
	for {
		// A zero program counter prevents a fallback with AddSource from
		// disclosing an arbitrary source path supplied by a caller.
		safe = slog.NewRecord(stamp, record.Level, spec.message, 0)
		safe.AddAttrs(slog.String("event", spec.name))
		safe.AddAttrs(attrs...)
		var buffer bytes.Buffer
		if err := slog.NewJSONHandler(&buffer, nil).Handle(ctx, safe); err != nil {
			return errDiagnosticEncoding
		}
		if buffer.Len() <= MaxRecordBytes {
			line = buffer.Bytes()
			break
		}
		if len(attrs) == 0 {
			return errDiagnosticEncoding
		}
		attrs = attrs[:len(attrs)-1]
	}
	var storeErr error
	if h.store != nil {
		storeErr = h.store.appendRecord(ctx, line)
	}
	var fallbackErr error
	if h.fallback != nil {
		fallbackErr = handleDiagnosticFallback(h.fallback, context.WithValue(ctx, diagnosticContextKey{}, true), safe)
	}
	return errors.Join(storeErr, fallbackErr)
}

func (h *diagnosticHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if h.blocked || h.bound >= maxDiagnosticAttrs {
		return h
	}
	budget := maxDiagnosticAttrs - h.bound
	frozen := freezeDiagnosticAttrs(attrs, len(h.groups), &budget)
	if len(frozen) == 0 {
		return h
	}
	copy := *h
	copy.bindings = append(append([]diagnosticBinding(nil), h.bindings...), diagnosticBinding{len(h.groups), frozen})
	copy.bound = maxDiagnosticAttrs - budget
	return &copy
}

func (h *diagnosticHandler) WithGroup(name string) slog.Handler {
	if name == "" || h.blocked {
		return h
	}
	copy := *h
	if !diagnosticGroupAllowed(name) || len(h.groups) >= maxDiagnosticDepth {
		copy.blocked = true
		return &copy
	}
	copy.groups = append(append([]string(nil), h.groups...), name)
	return &copy
}

func fallbackEnabled(handler slog.Handler, ctx context.Context, level slog.Level) (enabled bool) {
	if handler == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			enabled = false
		}
	}()
	return handler.Enabled(ctx, level)
}

func handleDiagnosticFallback(handler slog.Handler, ctx context.Context, record slog.Record) (err error) {
	defer func() {
		if recover() != nil {
			err = errDiagnosticFallback
		}
	}()
	if !handler.Enabled(ctx, record.Level) {
		return nil
	}
	return handler.Handle(ctx, record)
}

// Values are frozen at binding time. In particular, no LogValuer, Stringer,
// error formatting method, or arbitrary Any value is retained for later use.
func freezeDiagnosticAttrs(attrs []slog.Attr, depth int, budget *int) []slog.Attr {
	result := make([]slog.Attr, 0, min(len(attrs), *budget))
	for index, attr := range attrs {
		if *budget <= 0 || index >= maxInspectedAttrs {
			break
		}
		if len(attr.Key) > 32 {
			continue
		}
		if attr.Value.Kind() == slog.KindGroup {
			if depth >= maxDiagnosticDepth || attr.Key != "" && !diagnosticGroupAllowed(attr.Key) {
				continue
			}
			*budget--
			children := freezeDiagnosticAttrs(attr.Value.Group(), depth+1, budget)
			if len(children) > 0 {
				if attr.Key == "" {
					result = append(result, children...)
				} else {
					result = append(result, slog.GroupAttrs(attr.Key, children...))
				}
			}
			continue
		}
		if safe, ok := freezeDiagnosticValue(attr); ok {
			result = append(result, safe)
			*budget--
		}
	}
	return result
}

func filterDiagnosticAttrs(attrs []slog.Attr, fields map[string]bool, budget *int) []slog.Attr {
	var result []slog.Attr
	for _, attr := range attrs {
		if *budget <= 0 {
			break
		}
		if attr.Value.Kind() == slog.KindGroup {
			*budget--
			if children := filterDiagnosticAttrs(attr.Value.Group(), fields, budget); len(children) > 0 {
				result = append(result, slog.GroupAttrs(attr.Key, children...))
			}
		} else if fields[attr.Key] {
			result = append(result, attr)
			*budget--
		}
	}
	return result
}

func diagnosticGroupAllowed(name string) bool {
	switch name {
	case "request", "http", "server", "transcode", "task", "library", "identity", "diagnostics", "context", "operation":
		return true
	}
	return false
}

func freezeDiagnosticValue(attr slog.Attr) (slog.Attr, bool) {
	key, value := attr.Key, attr.Value
	switch key {
	case "actor_id", "user_id", "session_id", "actor_credential_id", "library_id", "job_id", "item_id", "task_id", "run_id", "trigger_id", "request_id":
		if value.Kind() == slog.KindString && diagnosticIDAllowed(value.String()) {
			return slog.String(key, strings.ToLower(value.String())), true
		}
	case "device_id", "application_key_id", "revision":
		return diagnosticInteger(attr, 1, 1<<63-1)
	case "revoked_login_count", "bytes", "scanned", "added", "updated":
		return diagnosticInteger(attr, 0, 1<<63-1)
	case "duration_ms":
		return diagnosticInteger(attr, 0, int64((7*24*time.Hour)/time.Millisecond))
	case "status":
		return diagnosticInteger(attr, 100, 599)
	case "exit_code":
		return diagnosticInteger(attr, -1, 255)
	case "administrator", "force_probe", "cancelled":
		if value.Kind() == slog.KindBool {
			return slog.Bool(key, value.Bool()), true
		}
	case "error":
		if value.Kind() == slog.KindAny {
			if err, ok := value.Any().(error); ok {
				return slog.String("error_class", diagnosticErrorClass(err, 0)), true
			}
		}
	case "error_class":
		if value.Kind() == slog.KindString && diagnosticErrorClassAllowed(value.String()) {
			return slog.String(key, value.String()), true
		}
	case "method":
		if value.Kind() == slog.KindString {
			switch value.String() {
			case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
				return slog.String(key, value.String()), true
			}
		}
	case "outcome":
		if value.Kind() == slog.KindString {
			switch value.String() {
			case "completed", "cancelled", "aborted":
				return slog.String(key, value.String()), true
			}
		}
	case "route":
		if value.Kind() == slog.KindString {
			if len(value.String()) <= 128 && diagnosticRoutes[value.String()] {
				return slog.String(key, value.String()), true
			}
			return slog.String(key, "unmatched"), true
		}
	case "version":
		if value.Kind() == slog.KindString && len(value.String()) <= 64 && buildVersionPattern.MatchString(value.String()) {
			return slog.String(key, value.String()), true
		}
	case "database":
		if value.Kind() == slog.KindString && value.String() == "postgresql" {
			return slog.String(key, "postgresql"), true
		}
	case "kind":
		if value.Kind() == slog.KindString && (value.String() == "admin" || value.String() == "emby") {
			return slog.String(key, value.String()), true
		}
	case "action":
		if value.Kind() == slog.KindString && (value.String() == "update_user" || value.String() == "reset_user_password") {
			return slog.String(key, value.String()), true
		}
	case "mode":
		if value.Kind() == slog.KindString {
			switch value.String() {
			case "audio", "video", "remux", "hls":
				return slog.String(key, value.String()), true
			}
		}
	}
	return slog.Attr{}, false
}

func diagnosticInteger(attr slog.Attr, minimum, maximum int64) (slog.Attr, bool) {
	var number int64
	switch attr.Value.Kind() {
	case slog.KindInt64:
		number = attr.Value.Int64()
	case slog.KindUint64:
		if attr.Value.Uint64() > uint64(maximum) {
			return slog.Attr{}, false
		}
		number = int64(attr.Value.Uint64())
	default:
		return slog.Attr{}, false
	}
	if number < minimum || number > maximum {
		return slog.Attr{}, false
	}
	return slog.Int64(attr.Key, number), true
}

func diagnosticIDAllowed(value string) bool {
	if len(value) != 32 && len(value) != 36 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if len(value) == 36 && (index == 8 || index == 13 || index == 18 || index == 23) {
			if value[index] != '-' {
				return false
			}
			continue
		}
		character := value[index]
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

// Only known standard wrappers are traversed, with a strict depth limit.
// errors.Is/As and Unwrap are intentionally avoided because they execute
// methods on arbitrary error implementations.
func diagnosticErrorClass(err error, depth int) string {
	if err == nil {
		return "none"
	}
	if depth >= maxDiagnosticDepth {
		return "unclassified"
	}
	switch err {
	case context.Canceled:
		return "cancelled"
	case context.DeadlineExceeded:
		return "deadline_exceeded"
	case io.EOF:
		return "eof"
	case io.ErrUnexpectedEOF:
		return "unexpected_eof"
	case fs.ErrNotExist:
		return "not_found"
	case fs.ErrPermission:
		return "permission_denied"
	case fs.ErrClosed:
		return "closed"
	}
	switch typed := err.(type) {
	case *fs.PathError:
		if typed != nil {
			return diagnosticErrorClass(typed.Err, depth+1)
		}
	case *os.LinkError:
		if typed != nil {
			return diagnosticErrorClass(typed.Err, depth+1)
		}
	case *os.SyscallError:
		if typed != nil {
			return diagnosticErrorClass(typed.Err, depth+1)
		}
	case *net.OpError:
		if typed != nil {
			return diagnosticErrorClass(typed.Err, depth+1)
		}
	case *url.Error:
		if typed != nil {
			return diagnosticErrorClass(typed.Err, depth+1)
		}
	case *net.DNSError:
		if typed != nil && typed.IsTimeout {
			return "deadline_exceeded"
		}
		return "network_error"
	case *exec.ExitError:
		return "process_exit"
	case syscall.Errno:
		switch typed {
		case syscall.ENOENT:
			return "not_found"
		case syscall.EACCES, syscall.EPERM:
			return "permission_denied"
		case syscall.ENOSPC:
			return "no_space"
		case syscall.ECONNREFUSED:
			return "connection_refused"
		case syscall.ECONNRESET, syscall.EPIPE:
			return "connection_closed"
		case syscall.ETIMEDOUT:
			return "deadline_exceeded"
		case syscall.EIO:
			return "io_error"
		}
		return "system_error"
	}
	return "unclassified"
}

func diagnosticErrorClassAllowed(value string) bool {
	switch value {
	case "none", "cancelled", "deadline_exceeded", "eof", "unexpected_eof", "not_found", "permission_denied", "closed", "network_error", "process_exit", "no_space", "connection_refused", "connection_closed", "io_error", "system_error", "unclassified":
		return true
	}
	return false
}

type diagnosticEvent struct {
	name    string
	message string
	fields  map[string]bool
}

func diagnosticEventFor(message string) diagnosticEvent {
	if len(message) <= 80 {
		if spec, ok := diagnosticEvents[message]; ok {
			return spec
		}
	}
	return diagnosticEvent{name: "unclassified", message: "unclassified event"}
}

var diagnosticEvents = func() map[string]diagnosticEvent {
	events := make(map[string]diagnosticEvent)
	add := func(name, message string, fields ...string) {
		spec := diagnosticEvent{name: name, message: message, fields: make(map[string]bool, len(fields))}
		for _, field := range fields {
			spec.fields[field] = true
		}
		events[name], events[message] = spec, spec
	}
	add("server.starting", "server starting", "version", "database")
	add("server.listening", "server listening", "version", "database")
	add("server.stopped", "server stopped", "error_class")
	add("server.shutdown.completed", "server shutdown completed", "duration_ms")
	add("server.shutdown.failed", "background work did not close cleanly", "error_class")
	add("request.completed", "request completed", "request_id", "method", "route", "status", "duration_ms", "bytes", "outcome")
	add("request.panic", "request panic", "request_id")
	add("activity.retention.retry", "activity retention will retry", "error_class")
	add("administrator.setup.completed", "administrator setup completed", "user_id")
	add("user.created", "user created", "actor_id", "user_id", "administrator")
	add("administrator.device.updated", "administrator device options updated", "actor_id", "device_id")
	add("administrator.device.removed", "administrator device removed", "actor_id", "device_id", "revoked_login_count")
	add("administrator.metadata.updated", "administrator metadata mutation", "actor_id", "item_id", "revision")
	add("administrator.session.revoked", "administrator session revocation", "actor_id", "user_id", "session_id", "kind")
	add("administrator.user.updated", "administrator user mutation", "actor_id", "user_id", "action")
	add("application_key.created", "application key created", "actor_credential_id", "application_key_id")
	add("application_key.revealed", "application key revealed", "actor_credential_id", "application_key_id")
	add("application_key.revoked", "application key revoked", "application_key_id")
	add("compatibility.device.updated", "compatibility device options updated", "actor_credential_id", "device_id")
	add("compatibility.device.removed", "compatibility device removed", "actor_credential_id", "device_id", "revoked_login_count")
	add("library.created", "library created", "actor_id", "library_id")
	add("library.removed", "library removed", "actor_id", "library_id")
	add("library.scan.requested", "library scan requested", "actor_id", "library_id", "job_id", "force_probe")
	add("library.scan.finalization.retry", "Retrying scan job finalization", "job_id", "error_class")
	for _, operation := range []string{"task", "settings", "identity", "library", "compatibility configuration", "compatibility task"} {
		add(strings.ReplaceAll(operation, " ", ".")+".operation.failed", operation+" operation failed", "request_id", "error_class")
	}
	add("transcode.completed", "transcode completed", "job_id", "request_id", "item_id", "duration_ms", "bytes", "mode", "exit_code", "cancelled")
	add("transcode.failed", "transcode failed", "job_id", "request_id", "item_id", "duration_ms", "mode", "exit_code", "cancelled", "error_class")
	return events
}()

// Every entry is a route registration literal or a fixed middleware category.
// Unknown patterns are reduced to "unmatched" instead of exposing URL data.
var diagnosticRoutes = func() map[string]bool {
	routes := make(map[string]bool)
	for _, pattern := range strings.Split(strings.TrimSpace(`
unmatched
websocket
cors
/
/admin/
/admin/v1/
/emby/
DELETE /admin/v1/libraries/{id}
DELETE /admin/v1/session
DELETE /emby/Auth/Keys/{Key}
DELETE /emby/Devices
DELETE /emby/ScheduledTasks/Running/{id}
DELETE /emby/Videos/ActiveEncodings
GET /{$}
GET /admin
GET /admin/v1/activity
GET /admin/v1/api-keys
GET /admin/v1/bootstrap
GET /admin/v1/capabilities
GET /admin/v1/devices
GET /admin/v1/items/{id}/metadata
GET /admin/v1/jobs
GET /admin/v1/libraries
GET /admin/v1/libraries/{id}/items
GET /admin/v1/logs
GET /admin/v1/logs/{name}/download
GET /admin/v1/logs/{name}/lines
GET /admin/v1/overview
GET /admin/v1/session
GET /admin/v1/sessions
GET /admin/v1/settings
GET /admin/v1/storage/roots
GET /admin/v1/task-runs/{id}
GET /admin/v1/tasks
GET /admin/v1/tasks/{id}
GET /admin/v1/tasks/{id}/runs
GET /admin/v1/users
GET /admin/v1/users/{id}
GET /emby/Auth/Keys
GET /emby/Devices
GET /emby/Devices/Info
GET /emby/Devices/Options
GET /emby/Items
GET /emby/Items/{Id}/Images
GET /emby/Items/{Id}/Images/{Type}
GET /emby/Items/{Id}/Images/{Type}/{Index}
GET /emby/Items/{Id}/PlaybackInfo
GET /emby/Library/VirtualFolders/Query
GET /emby/ScheduledTasks
GET /emby/ScheduledTasks/{id}
GET /emby/Sessions
GET /emby/Shows/{Id}/Episodes
GET /emby/Shows/{Id}/Seasons
GET /emby/Shows/NextUp
GET /emby/System/ActivityLog/Entries
GET /emby/System/Configuration
GET /emby/System/Configuration/{key}
GET /emby/System/Info
GET /emby/System/Info/Public
GET /emby/System/Logs/Query
GET /emby/System/Logs/{name}
GET /emby/System/Logs/{name}/Lines
GET /emby/System/Ping
GET /emby/Users
GET /emby/Users/{Id}
GET /emby/Users/{UserId}/Items
GET /emby/Users/{UserId}/Items/{Id}
GET /emby/Users/{UserId}/Items/Latest
GET /emby/Users/{UserId}/Items/Resume
GET /emby/Users/{UserId}/Items/Root
GET /emby/Users/{UserId}/Views
GET /emby/Users/Public
GET /emby/Users/Query
GET /healthz
GET /readyz
POST /admin/v1/api-keys
POST /admin/v1/api-keys/{id}/reveal
POST /admin/v1/api-keys/{id}/revoke
POST /admin/v1/bootstrap
POST /admin/v1/devices/{id}/delete
POST /admin/v1/devices/{id}/options
POST /admin/v1/jobs/{id}/cancel
POST /admin/v1/libraries
POST /admin/v1/libraries/{id}/scan
POST /admin/v1/session
POST /admin/v1/sessions/{id}/revoke
POST /admin/v1/settings/reset
POST /admin/v1/task-runs/{id}/cancel
POST /admin/v1/tasks/{id}/runs
POST /admin/v1/tasks/{id}/triggers/preview
POST /admin/v1/users
POST /admin/v1/users/{id}/password
POST /emby/Auth/Keys
POST /emby/Auth/Keys/{Key}/Delete
POST /emby/Devices/Delete
POST /emby/Devices/Options
POST /emby/Items/{Id}/PlaybackInfo
POST /emby/Library/Refresh
POST /emby/Library/VirtualFolders
POST /emby/Library/VirtualFolders/Delete
POST /emby/ScheduledTasks/{path...}
POST /emby/ScheduledTasks/Running/{id}
POST /emby/ScheduledTasks/Running/{id}/Delete
POST /emby/Sessions/{Id}/Command
POST /emby/Sessions/{Id}/Command/{Command}
POST /emby/Sessions/{Id}/Playing
POST /emby/Sessions/{Id}/Playing/{Command}
POST /emby/Sessions/Capabilities
POST /emby/Sessions/Capabilities/Full
POST /emby/Sessions/Logout
POST /emby/Sessions/Playing/Ping
POST /emby/System/Configuration
POST /emby/System/Configuration/{key}
POST /emby/System/Configuration/Partial
POST /emby/System/Ping
POST /emby/Users/{Id}/Authenticate
POST /emby/Users/AuthenticateByName
POST /emby/Videos/ActiveEncodings/Delete
PUT /admin/v1/items/{id}/metadata
PUT /admin/v1/settings
PUT /admin/v1/tasks/{id}/triggers
PUT /admin/v1/users/{id}
`), "\n") {
		routes[pattern] = true
	}
	for _, base := range []string{"/emby/Videos", "/emby/videos", "/Videos", "/videos", "/emby/Audio", "/emby/audio", "/Audio", "/audio"} {
		routes["GET "+base+"/{Id}/stream"] = true
		routes["GET "+base+"/{Id}/{StreamFileName}"] = true
	}
	for _, resource := range []string{"Videos", "Items"} {
		base := "GET /emby/" + resource + "/{Id}/{MediaSourceId}/Subtitles/{Index}/"
		routes[base+"{SubtitleFileName}"] = true
		routes[base+"{StartPositionTicks}/{SubtitleFileName}"] = true
	}
	for _, resource := range []string{"Genres", "Tags", "Studios", "Persons"} {
		routes["GET /emby/"+resource] = true
		if resource != "Tags" {
			routes["GET /emby/"+resource+"/{Name}"] = true
		}
	}
	for _, resource := range []string{"Videos", "Audio"} {
		base := "GET /emby/" + resource + "/{Id}/"
		for _, suffix := range []string{"master.m3u8", "main.m3u8", "hls1/{PlaylistId}/{SegmentFile}"} {
			routes[base+suffix] = true
		}
	}
	for _, suffix := range []string{"", "/Progress", "/Stopped"} {
		routes["POST /emby/Sessions/Playing"+suffix] = true
	}
	for _, action := range []string{"PlayedItems", "FavoriteItems"} {
		base := "/emby/Users/{UserId}/" + action + "/{Id}"
		routes["POST "+base], routes["DELETE "+base], routes["POST "+base+"/Delete"] = true, true, true
	}
	return routes
}()
