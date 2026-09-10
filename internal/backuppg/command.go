package backuppg

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	defaultCommandTimeout = 5 * time.Minute
	maximumCommandTimeout = 30 * time.Minute
	defaultCommandBytes   = int64(16 << 30)
	maximumCommandBytes   = int64(64 << 30)
	commandStderrBytes    = int64(64 << 10)
	commandVersionBytes   = int64(4 << 10)
	commandWaitDelay      = 2 * time.Second
)

var backupVersion = regexp.MustCompile(`^(pg_dump|pg_restore) \(PostgreSQL\) 17\.[0-9]+(?:\.[0-9]+)?(?: \([^\r\n]*\))?\n?$`)
var backupSnapshot = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{8}-[1-9][0-9]*$`)

// sourceCommandConfig deliberately supports a single, explicit connection.
// libpq URI decoding preserves literal '+' bytes, unlike url.ParseQuery.
// Unknown parameters, services, multi-host fallback and implicit TLS policy are
// rejected rather than being lost when the connection is moved out of argv.
// The caller must independently compare this connection with its source pool.
func sourceCommandConfig(options Options) (map[string]string, error) {
	if len(options.SourceURL) == 0 || len(options.SourceURL) > 64<<10 || strings.IndexByte(options.SourceURL, 0) >= 0 {
		return nil, ErrConfiguration
	}
	uri, err := url.Parse(options.SourceURL)
	if err != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") || uri.Opaque != "" || uri.Fragment != "" || strings.Contains(options.SourceURL, "#") || uri.User == nil {
		return nil, ErrConfiguration
	}
	host := uri.Hostname()
	if host == "" || strings.ContainsAny(host, ",/\\% \t\r\n\x00") {
		return nil, ErrConfiguration
	}
	if strings.Contains(host, ":") {
		if net.ParseIP(host) == nil {
			return nil, ErrConfiguration
		}
	} else {
		for _, c := range host {
			if c != '.' && c != '-' && c != '_' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return nil, ErrConfiguration
			}
		}
	}
	port := uri.Port()
	if port == "" {
		port = "5432"
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 || strconv.FormatUint(portNumber, 10) != port {
		return nil, ErrConfiguration
	}
	user := uri.User.Username()
	password, _ := uri.User.Password()
	if !strings.HasPrefix(uri.Path, "/") {
		return nil, ErrConfiguration
	}
	database := strings.TrimPrefix(uri.Path, "/")
	for _, value := range []string{user, database} {
		if len(value) == 0 || len(value) > 8192 || strings.IndexByte(value, 0) >= 0 {
			return nil, ErrConfiguration
		}
	}
	if strings.IndexByte(password, 0) >= 0 {
		return nil, ErrConfiguration
	}
	values := map[string]string{
		"PGHOST": host, "PGPORT": port, "PGDATABASE": database,
		"PGUSER": user, "PGPASSWORD": password, "PGPASSFILE": "/dev/null",
		// pgx uses the configured TLS policy; libpq's default GSS preference
		// must not select a different transport before applying that policy.
		"PGGSSENCMODE": "disable",
	}
	parameters := make(map[string]string)
	if uri.RawQuery == "" {
		return nil, ErrConfiguration
	}
	for _, field := range strings.Split(uri.RawQuery, "&") {
		if strings.Count(field, "=") != 1 {
			return nil, ErrConfiguration
		}
		pair := strings.SplitN(field, "=", 2)
		name, nameErr := url.PathUnescape(pair[0])
		value, valueErr := url.PathUnescape(pair[1])
		if nameErr != nil || valueErr != nil || name == "" || value == "" || len(value) > 8192 || strings.IndexByte(value, 0) >= 0 {
			return nil, ErrConfiguration
		}
		if _, duplicate := parameters[name]; duplicate {
			return nil, ErrConfiguration
		}
		parameters[name] = value
	}
	sslmode := parameters["sslmode"]
	switch sslmode {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		values["PGSSLMODE"] = sslmode
	default:
		return nil, ErrConfiguration
	}
	for name, value := range parameters {
		switch name {
		case "sslmode":
		case "sslrootcert", "sslcert", "sslkey", "sslcrl", "sslcrldir":
			if name == "sslrootcert" && value == "system" {
				if sslmode != "verify-full" {
					return nil, ErrConfiguration
				}
			} else if !filepath.IsAbs(value) || filepath.Clean(value) != value || value == string(filepath.Separator) || strings.ContainsAny(value, "\r\n") {
				return nil, ErrConfiguration
			}
			if name == "sslkey" && strings.Contains(value, ":") {
				// libpq treats any colon in a Linux key path as engine:key.
				return nil, ErrConfiguration
			}
			values["PG"+strings.ToUpper(name)] = value
		case "connect_timeout":
			seconds, parseErr := strconv.ParseUint(value, 10, 32)
			if parseErr != nil || seconds == 0 || seconds > 1800 || strconv.FormatUint(seconds, 10) != value {
				return nil, ErrConfiguration
			}
			values["PGCONNECT_TIMEOUT"] = value
		case "channel_binding":
			if value != "require" && value != "prefer" && value != "disable" {
				return nil, ErrConfiguration
			}
			values["PGCHANNELBINDING"] = value
		case "target_session_attrs":
			switch value {
			case "any", "read-write", "read-only", "primary", "standby", "prefer-standby":
				values["PGTARGETSESSIONATTRS"] = value
			default:
				return nil, ErrConfiguration
			}
		default:
			// In particular, sslpassword has no libpq environment equivalent.
			// service changes precedence and options can change runtime settings.
			return nil, ErrConfiguration
		}
	}
	if (sslmode == "verify-ca" || sslmode == "verify-full") && values["PGSSLROOTCERT"] == "" {
		return nil, ErrConfiguration
	}
	if (values["PGSSLCERT"] == "") != (values["PGSSLKEY"] == "") {
		return nil, ErrConfiguration
	}
	return values, nil
}

func checkToolVersions(ctx context.Context, options Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout, _, err := commandBounds(options)
	if err != nil {
		return err
	}
	if timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	for _, tool := range []struct{ path, name string }{{options.PGDump, "pg_dump"}, {options.PGRestore, "pg_restore"}} {
		if err := checkBackupExecutable(tool.path); err != nil {
			return err
		}
		var output bytes.Buffer
		if err := runBackupOutput(ctx, timeout, commandVersionBytes, tool.path, commandEnvironment(nil), nil, &output, "--version"); err != nil {
			return err
		}
		if !backupVersion.Match(output.Bytes()) || !strings.HasPrefix(output.String(), tool.name+" ") {
			return ErrUnsupported
		}
	}
	return nil
}

// dumpCommand accepts an owned regular file or a bytes.Buffer. Arbitrary
// writers can block inside Write indefinitely and cannot be cancelled by Go.
// The caller owns output storage, must not concurrently close or write it, and
// must discard any partial output unless the entire backup operation succeeds.
func dumpCommand(ctx context.Context, options Options, exportedSnapshot string, out io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout, limit, err := commandBounds(options)
	if err != nil {
		return err
	}
	if !backupSnapshot.MatchString(exportedSnapshot) || !commandSchema(options.Schema) || !ownedCommandWriter(out) {
		return ErrConfiguration
	}
	values, err := sourceCommandConfig(options)
	if err != nil {
		return err
	}
	if options.sourceHostAddress != "" {
		if net.ParseIP(options.sourceHostAddress) == nil {
			return ErrConfiguration
		}
		// The snapshot owner pins this to its already connected backend.
		// Keep PGHOST for certificate host-name checks and authentication.
		values["PGHOSTADDR"] = options.sourceHostAddress
	}
	if err := checkBackupExecutable(options.PGDump); err != nil {
		return err
	}
	return runBackupOutput(ctx, timeout, limit, options.PGDump, commandEnvironment(values), nil, out,
		"--format=custom", "--quote-all-identifiers", "--no-owner", "--no-acl", "--no-password", "--strict-names",
		"--snapshot="+exportedSnapshot, "--schema="+pgx.Identifier{options.Schema}.Sanitize())
}

// decodeCommand never supplies a database connection to pg_restore. It only
// decodes archive data to a bounded pipe for the trusted COPY parser. The
// consumer must read through EOF, finish all work synchronously and honor its
// own operation context when accessing a database. It must not retain readers.
// Inputs are owned regular files or bounded, synchronous memory readers.
func decodeCommand(ctx context.Context, options Options, archive io.Reader, consume func(io.Reader) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout, limit, err := commandBounds(options)
	if err != nil {
		return err
	}
	if consume == nil {
		return ErrConfiguration
	}
	if err := ownedCommandReader(archive, limit); err != nil {
		return err
	}
	if runtime.GOOS != "linux" {
		return ErrUnsupported
	}
	if err := checkBackupExecutable(options.PGRestore); err != nil {
		return err
	}
	processContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(processContext, options.PGRestore, "--data-only", "--no-owner", "--no-acl", "--file=-")
	command.Env = commandEnvironment(nil)
	command.Stdin = archive
	command.WaitDelay = commandWaitDelay
	stderr := &backupOutput{limit: commandStderrBytes, destination: io.Discard, cancel: cancel}
	command.Stderr = stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return ErrCommand
	}
	defer stdout.Close()
	retired, err := startBackupProcess(command)
	if err != nil {
		return backupCommandError(processContext, err)
	}
	waited := false
	defer func() {
		// Even a consumer panic must not abandon an unreaped owned child.
		if !waited {
			cancel()
			_ = stdout.Close()
			<-retired
			_ = command.Wait()
		}
	}()
	stopClose := context.AfterFunc(processContext, func() { _ = stdout.Close() })
	defer stopClose()
	reader := &backupReader{source: stdout, remaining: limit, cancel: cancel}
	consumeErr := consume(reader)
	processErr := processContext.Err()
	if consumeErr != nil {
		cancel()
	}
	_ = stdout.Close()
	// A failed database COPY can return before its input-reading goroutine.
	// Closing the pipe releases such a reader before inspecting its state.
	eof, exceeded := reader.state()
	if consumeErr == nil && !eof {
		consumeErr = ErrArchive
		cancel()
	}
	waitErr := errors.Join(<-retired, command.Wait())
	waited = true
	if exceeded || stderr.exceeded {
		return ErrLimit
	}
	if processErr != nil {
		return processErr
	}
	if consumeErr != nil {
		// Preserve parser and sink sentinels, but not child stderr or exec errors.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return backupConsumerError(consumeErr)
	}
	return backupCommandError(processContext, waitErr)
}

func backupConsumerError(err error) error {
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded, ErrArchive, ErrLimit, ErrDatabase, ErrSchema, ErrTarget, ErrConfiguration, ErrUnsupported, ErrCommand} {
		if errors.Is(err, sentinel) {
			return sentinel
		}
	}
	return ErrArchive
}

func commandBounds(options Options) (time.Duration, int64, error) {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = defaultCommandTimeout
	}
	limit := options.MaxDumpBytes
	if limit == 0 {
		limit = defaultCommandBytes
	}
	if timeout < 0 || timeout > maximumCommandTimeout || limit < 1 || limit > maximumCommandBytes {
		return 0, 0, ErrConfiguration
	}
	return timeout, limit, nil
}

func commandSchema(value string) bool {
	return len(value) > 0 && len(value) <= 63 && strings.IndexByte(value, 0) < 0
}

func checkBackupExecutable(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
		return ErrConfiguration
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return ErrConfiguration
	}
	return nil
}

func commandEnvironment(values map[string]string) []string {
	// In particular, no inherited PG*, loader, shell, reporting, or credential
	// variables reach the decoder. Absolute executable paths are mandatory.
	env := []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "TZ=UTC", "HOME=/nonexistent"}
	keys := make([]string, 0, len(values))
	for name := range values {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		env = append(env, name+"="+values[name])
	}
	return env
}

func ownedCommandWriter(writer io.Writer) bool {
	switch value := writer.(type) {
	case *os.File:
		if value == nil {
			return false
		}
		info, err := value.Stat()
		return err == nil && info.Mode().IsRegular()
	case *bytes.Buffer:
		return value != nil
	default:
		return false
	}
}

func ownedCommandReader(reader io.Reader, limit int64) error {
	var size int64
	switch value := reader.(type) {
	case *os.File:
		if value == nil {
			return ErrConfiguration
		}
		info, err := value.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return ErrConfiguration
		}
		position, err := value.Seek(0, io.SeekCurrent)
		if err != nil || position < 0 || position > info.Size() {
			return ErrConfiguration
		}
		size = info.Size() - position
	case *bytes.Reader:
		if value == nil {
			return ErrConfiguration
		}
		size = int64(value.Len())
	case *strings.Reader:
		if value == nil {
			return ErrConfiguration
		}
		size = int64(value.Len())
	default:
		return ErrConfiguration
	}
	if size > limit {
		return ErrLimit
	}
	return nil
}

type backupOutput struct {
	destination io.Writer
	limit       int64
	written     int64
	exceeded    bool
	failed      bool
	cancel      context.CancelFunc
}

func (w *backupOutput) Write(p []byte) (int, error) {
	if int64(len(p)) > w.limit-w.written {
		w.exceeded = true
		w.cancel()
		return 0, ErrLimit
	}
	n, err := w.destination.Write(p)
	w.written += int64(n)
	if err != nil || n != len(p) {
		w.failed = true
		w.cancel()
		return n, ErrCommand
	}
	return n, nil
}

type backupReader struct {
	mu        sync.Mutex
	source    io.Reader
	remaining int64
	exceeded  bool
	eof       bool
	cancel    context.CancelFunc
}

func (r *backupReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if r.exceeded {
		return 0, ErrLimit
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.source.Read(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
		r.remaining = 0
		r.exceeded = true
		r.cancel()
		return n, ErrLimit
	}
	r.remaining -= int64(n)
	if errors.Is(err, io.EOF) {
		r.eof = true
	}
	return n, err
}

func (r *backupReader) state() (eof, exceeded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.eof, r.exceeded
}

func runBackupOutput(ctx context.Context, timeout time.Duration, limit int64, path string, env []string, input io.Reader, output io.Writer, args ...string) error {
	processContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout := &backupOutput{destination: output, limit: limit, cancel: cancel}
	stderr := &backupOutput{destination: io.Discard, limit: commandStderrBytes, cancel: cancel}
	command := exec.CommandContext(processContext, path, args...)
	command.Env, command.Stdin, command.Stdout, command.Stderr = env, input, stdout, stderr
	command.WaitDelay = commandWaitDelay
	retired, err := startBackupProcess(command)
	if err == nil {
		err = errors.Join(<-retired, command.Wait())
	}
	if stdout.exceeded || stderr.exceeded {
		return ErrLimit
	}
	if stdout.failed || stderr.failed {
		return ErrCommand
	}
	return backupCommandError(processContext, err)
}

func backupCommandError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, ErrUnsupported) {
		return ErrUnsupported
	}
	if err != nil {
		return ErrCommand
	}
	return nil
}

func startBackupProcess(command *exec.Cmd) (<-chan error, error) {
	retire, err := configureBackupProcess(command)
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	started := make(chan error, 1)
	go func() {
		// Linux Pdeathsig observes the creating thread. Keep that thread alive
		// until the child has exited and its process group has been retired.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := command.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		done <- retire()
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return done, nil
}
