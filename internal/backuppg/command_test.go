//go:build linux

package backuppg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const commandFixtureURI = "postgresql://backup_user:backup_password@db.example:5433/goby?sslmode=disable"
const commandFixtureSnapshot = "00000003-0000001B-1"

func TestBackupCommandConnectionPreservesLibpqEncodingAndTLSPolicy(t *testing.T) {
	options := Options{SourceURL: "postgresql://user%2Bname:p%2Bass+%26%3D%25%20word@[::1]:5433/db%2Bname?sslmode=verify-full&sslrootcert=%2Fowned%2Froot+bundle.pem&sslcert=%2Fowned%2Fclient.pem&sslkey=%2Fowned%2Fkey.pem&sslcrl=%2Fowned%2Fcrl.pem&sslcrldir=%2Fowned%2Fcrls&connect_timeout=12&channel_binding=require&target_session_attrs=read-write"}
	got, err := sourceCommandConfig(options)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"PGHOST": "::1", "PGPORT": "5433", "PGDATABASE": "db+name", "PGUSER": "user+name",
		"PGPASSWORD": "p+ass+&=% word", "PGPASSFILE": "/dev/null", "PGSSLMODE": "verify-full", "PGGSSENCMODE": "disable",
		"PGSSLROOTCERT": "/owned/root+bundle.pem", "PGSSLCERT": "/owned/client.pem", "PGSSLKEY": "/owned/key.pem",
		"PGSSLCRL": "/owned/crl.pem", "PGSSLCRLDIR": "/owned/crls", "PGCONNECT_TIMEOUT": "12",
		"PGCHANNELBINDING": "require", "PGTARGETSESSIONATTRS": "read-write",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("connection parameters did not preserve the explicit URI values")
	}
	for _, mode := range []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"} {
		t.Run(mode, func(t *testing.T) {
			value := "postgres://backup_user:password@localhost/goby?sslmode=" + mode
			if strings.HasPrefix(mode, "verify-") {
				value += "&sslrootcert=/owned/roots.pem"
			}
			parsed, err := sourceCommandConfig(Options{SourceURL: value})
			if err != nil || parsed["PGSSLMODE"] != mode || parsed["PGPORT"] != "5432" {
				t.Fatal("explicit SSL policy or default port changed")
			}
		})
	}
	got, err = sourceCommandConfig(Options{SourceURL: "postgres://backup_user@localhost/goby?sslmode=verify-full&sslrootcert=system"})
	if err != nil || got["PGSSLROOTCERT"] != "system" || got["PGSSLMODE"] != "verify-full" {
		t.Fatal("explicit system root trust was not preserved")
	}
}

func TestBackupCommandConnectionRejectsAmbiguousOrUnsupportedParameters(t *testing.T) {
	for name, value := range map[string]string{
		"empty": "", "keyword": "host=localhost user=backup dbname=goby", "scheme": "https://backup_user@localhost/goby?sslmode=disable",
		"implicit_user": "postgres://localhost/goby?sslmode=disable", "empty_user": "postgres://@localhost/goby?sslmode=disable",
		"implicit_host": "postgres://backup_user@/goby?sslmode=disable", "implicit_database": "postgres://backup_user@localhost/?sslmode=disable",
		"implicit_ssl": "postgres://backup_user@localhost/goby", "socket": "postgres://backup_user@%2Fvar%2Frun%2Fpostgresql/goby?sslmode=disable",
		"multihost": "postgres://backup_user@localhost,other/goby?sslmode=disable", "ipv6_zone": "postgres://backup_user@[fe80::1%25eth0]/goby?sslmode=disable",
		"empty_fragment": commandFixtureURI + "#", "fragment": commandFixtureURI + "#secret", "bad_port": "postgres://backup_user@localhost:65536/goby?sslmode=disable",
		"zero_port": "postgres://backup_user@localhost:0/goby?sslmode=disable", "noncanonical_port": "postgres://backup_user@localhost:05432/goby?sslmode=disable",
		"service": commandFixtureURI + "&service=alternate", "host_override": commandFixtureURI + "&host=other", "hostaddr_override": commandFixtureURI + "&hostaddr=127.0.0.2",
		"database_override": commandFixtureURI + "&dbname=other", "options": commandFixtureURI + "&options=-c%20search_path=other", "pgx_pool_option": commandFixtureURI + "&pool_max_conns=5",
		"unknown": commandFixtureURI + "&unsafe=true", "duplicate": commandFixtureURI + "&sslmode=prefer", "encoded_duplicate": commandFixtureURI + "&ssl%6dode=prefer",
		"password_without_environment_mapping": commandFixtureURI + "&sslpassword=secret", "no_equals": commandFixtureURI + "&channel_binding", "unencoded_equals": commandFixtureURI + "&channel_binding=a=b",
		"empty_value": commandFixtureURI + "&channel_binding=", "empty_field": commandFixtureURI + "&", "nul_value": commandFixtureURI + "&sslcert=/owned/%00key",
		"bad_escape": commandFixtureURI + "&sslcert=%GG", "nul_password": "postgres://backup_user:%00@localhost/goby?sslmode=disable", "nul_database": "postgres://backup_user@localhost/db%00?sslmode=disable",
		"unknown_sslmode": "postgres://backup_user@localhost/goby?sslmode=mandatory", "implicit_root_trust": "postgres://backup_user@localhost/goby?sslmode=verify-full",
		"weaker_system_policy": "postgres://backup_user@localhost/goby?sslmode=require&sslrootcert=system", "root_without_sslmode": "postgres://backup_user@localhost/goby?sslrootcert=system",
		"lone_certificate": commandFixtureURI + "&sslcert=/owned/client.pem", "lone_key": commandFixtureURI + "&sslkey=/owned/client.key",
		"relative_root": commandFixtureURI + "&sslrootcert=roots.pem", "unclean_root": commandFixtureURI + "&sslrootcert=/owned/../roots.pem",
		"ssl_engine": commandFixtureURI + "&sslcert=/owned/client.pem&sslkey=/owned/engine:key", "zero_connection_timeout": commandFixtureURI + "&connect_timeout=0",
		"excessive_connection_timeout": commandFixtureURI + "&connect_timeout=1801", "unknown_binding": commandFixtureURI + "&channel_binding=true",
		"unknown_session_target": commandFixtureURI + "&target_session_attrs=leader",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := sourceCommandConfig(Options{SourceURL: value})
			if err != ErrConfiguration {
				t.Fatalf("expected a configuration sentinel, got %v", err)
			}
		})
	}
}

func TestBackupCommandChecksActualPostgreSQL17ToolVersions(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		version string
		want    error
	}{
		{"upstream", "17.11", nil}, {"distribution", "17.11 (Debian 17.11-1.pgdg120+1)", nil},
		{"older", "16.11", ErrUnsupported}, {"newer", "18.1", ErrUnsupported},
		{"development", "17devel", ErrUnsupported}, {"beta", "17beta1", ErrUnsupported},
		{"prefix_only", "17", ErrUnsupported}, {"extra_line", "17.11\nsecret", ErrUnsupported},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			dump := commandFixture(t, "print("+pythonString("pg_dump (PostgreSQL) "+fixture.version)+")")
			restore := commandFixture(t, "print("+pythonString("pg_restore (PostgreSQL) "+fixture.version)+")")
			err := checkToolVersions(context.Background(), Options{PGDump: dump, PGRestore: restore})
			if !errors.Is(err, fixture.want) {
				t.Fatalf("version check = %v, want %v", err, fixture.want)
			}
		})
	}
	t.Run("wrong_program", func(t *testing.T) {
		tool := commandFixture(t, "print('pg_restore (PostgreSQL) 17.11')")
		if err := checkToolVersions(context.Background(), Options{PGDump: tool, PGRestore: tool}); err != ErrUnsupported {
			t.Fatalf("wrong tool identity was accepted: %v", err)
		}
	})
	t.Run("bounded_version_output", func(t *testing.T) {
		tool := commandFixture(t, "import os\nos.write(1, b'x' * 8192)")
		if err := checkToolVersions(context.Background(), Options{PGDump: tool, PGRestore: tool}); err != ErrLimit {
			t.Fatalf("unbounded version output was accepted: %v", err)
		}
	})
}

func TestBackupCommandDumpUsesExactArgumentsAndIsolatedCredentials(t *testing.T) {
	for key, value := range map[string]string{"PGHOST": "poisoned", "PGPASSWORD": "inherited-secret", "PGSERVICE": "external", "PGOPTIONS": "-c search_path=external", "PGSSLROOTCERT": "/untrusted/roots.pem", "LD_PRELOAD": "inherited-library", "SSL_CERT_FILE": "/untrusted/system.pem", "PYTHONPATH": "/untrusted/python", "HOME": "/untrusted/home"} {
		t.Setenv(key, value)
	}
	tool := commandFixture(t, "import json, os, sys\njson.dump({'Arguments': sys.argv[1:], 'Environment': dict(os.environ)}, sys.stdout)")
	options := Options{PGDump: tool, SourceURL: commandFixtureURI, Schema: `owned."schema`, sourceHostAddress: "127.0.0.1"}
	var output bytes.Buffer
	if err := dumpCommand(context.Background(), options, commandFixtureSnapshot, &output); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Arguments   []string
		Environment map[string]string
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal("fixture output was not complete JSON")
	}
	wantArgs := []string{"--format=custom", "--quote-all-identifiers", "--no-owner", "--no-acl", "--no-password", "--strict-names", "--snapshot=" + commandFixtureSnapshot, `--schema="owned.""schema"`}
	if !reflect.DeepEqual(got.Arguments, wantArgs) {
		t.Fatalf("dump arguments changed: %q", got.Arguments)
	}
	wantEnv := map[string]string{
		"PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C", "TZ": "UTC", "HOME": "/nonexistent",
		"PGHOST": "db.example", "PGHOSTADDR": "127.0.0.1", "PGPORT": "5433", "PGDATABASE": "goby", "PGUSER": "backup_user",
		"PGPASSWORD": "backup_password", "PGPASSFILE": "/dev/null", "PGSSLMODE": "disable", "PGGSSENCMODE": "disable",
	}
	if !reflect.DeepEqual(got.Environment, wantEnv) {
		t.Fatal("the child inherited a variable or lost an explicit connection parameter")
	}
	for _, argument := range got.Arguments {
		if strings.Contains(argument, "backup_password") || strings.Contains(argument, "postgresql:") {
			t.Fatal("a credential appeared in the child argument list")
		}
	}
}

func TestBackupCommandDumpOutputAndStderrAreIndependentlyBounded(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		body   string
		limit  int64
		want   error
		length int
	}{
		{"exact_stdout", "import os\nos.write(1, b'abcd')", 4, nil, 4},
		{"over_stdout", "import os\nos.write(1, b'abcde')", 4, ErrLimit, 0},
		{"binary_stdout", "import os\nos.write(1, bytes(range(256)))", 256, nil, 256},
		{"exact_stderr", "import os\nos.write(2, b'x' * 65536)\nos.write(1, b'abcd')", 4, nil, 4},
		{"over_stderr", "import os\nos.write(2, b'x' * 65537)", 4, ErrLimit, 0},
		{"nonzero_exit", "import sys\nsys.stderr.write('private-source-password:secret\\n')\nsys.exit(23)", 128, ErrCommand, 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tool := commandFixture(t, fixture.body)
			var output bytes.Buffer
			err := dumpCommand(context.Background(), Options{SourceURL: commandFixtureURI, PGDump: tool, Schema: "public", MaxDumpBytes: fixture.limit}, commandFixtureSnapshot, &output)
			if !errors.Is(err, fixture.want) {
				t.Fatalf("dump = %v, want %v", err, fixture.want)
			}
			if fixture.want == nil && output.Len() != fixture.length {
				t.Fatalf("successful output length = %d, want %d", output.Len(), fixture.length)
			}
			if int64(output.Len()) > fixture.limit {
				t.Fatal("partial output exceeded its independent byte limit")
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("a child error disclosed stderr")
			}
		})
	}
}

func TestBackupCommandRejectsUnownedBlockingStreams(t *testing.T) {
	tool := commandFixture(t, "raise AssertionError('this tool must not run')")
	options := Options{SourceURL: commandFixtureURI, PGDump: tool, PGRestore: tool, Schema: "public"}
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipeReader.Close()
	defer pipeWriter.Close()
	var nilFile *os.File
	var nilBuffer *bytes.Buffer
	for _, writer := range []io.Writer{nil, nilFile, nilBuffer, pipeWriter, io.Discard, &commandNeverWriter{}} {
		if err := dumpCommand(context.Background(), options, commandFixtureSnapshot, writer); err != ErrConfiguration {
			t.Fatalf("unsafe output was accepted: %v", err)
		}
	}
	var nilReader *bytes.Reader
	var nilStringReader *strings.Reader
	for _, reader := range []io.Reader{nil, nilFile, nilReader, nilStringReader, pipeReader, &commandNeverReader{}} {
		if err := decodeCommand(context.Background(), options, reader, func(io.Reader) error { return nil }); err != ErrConfiguration {
			t.Fatalf("unsafe input was accepted: %v", err)
		}
	}
	options.MaxDumpBytes = 3
	if err := decodeCommand(context.Background(), options, strings.NewReader("four"), func(io.Reader) error { return nil }); err != ErrLimit {
		t.Fatalf("oversized archive input was accepted: %v", err)
	}
}

func TestBackupCommandOwnedFilesPreserveBytesAndCurrentInputPosition(t *testing.T) {
	tool := commandFixture(t, "import os\nos.write(1, bytes(range(256)))")
	output, err := os.CreateTemp(t.TempDir(), "owned-dump-")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	options := Options{SourceURL: commandFixtureURI, PGDump: tool, Schema: "public", MaxDumpBytes: 256}
	if err := dumpCommand(context.Background(), options, commandFixtureSnapshot, output); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Seek(128, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	options.PGRestore = commandFixture(t, "import sys\nsys.stdout.buffer.write(sys.stdin.buffer.read())")
	options.MaxDumpBytes = 128
	var decoded []byte
	if err := decodeCommand(context.Background(), options, output, func(reader io.Reader) error {
		var err error
		decoded, err = io.ReadAll(reader)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 128 || decoded[0] != 128 || decoded[127] != 255 {
		t.Fatal("the owned file's current input position or binary bytes changed")
	}
}

func TestBackupCommandOutputFileFailureCancelsWithoutDisclosingItsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-output-filename")
	if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	tool := commandFixture(t, "import os, time\nos.write(1, b'new output')\ntime.sleep(20)")
	started := time.Now()
	err = dumpCommand(context.Background(), Options{PGDump: tool, SourceURL: commandFixtureURI, Schema: "public", Timeout: time.Second}, commandFixtureSnapshot, output)
	if err != ErrCommand {
		t.Fatalf("output file failure = %v", err)
	}
	if strings.Contains(err.Error(), "private-output-filename") || time.Since(started) > 3*time.Second {
		t.Fatal("output failure disclosed a private path or failed to stop the child")
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != "existing" {
		t.Fatal("a failed output stream changed its existing content")
	}
}

func TestBackupCommandDecoderHasNoDatabaseConnectionOrInheritedCredentials(t *testing.T) {
	for key, value := range map[string]string{"PGDATABASE": "postgres://poisoned", "PGUSER": "inherited", "PGPASSWORD": "inherited-secret", "PGSERVICE": "external", "PGOPTIONS": "external", "LD_PRELOAD": "inherited-library", "HOME": "/untrusted/home"} {
		t.Setenv(key, value)
	}
	tool := commandFixture(t, "import json, os, sys\nvalue = sys.stdin.buffer.read()\njson.dump({'Arguments': sys.argv[1:], 'Environment': dict(os.environ), 'InputLength': len(value)}, sys.stdout)")
	var got struct {
		Arguments   []string
		Environment map[string]string
		InputLength int
	}
	err := decodeCommand(context.Background(), Options{PGRestore: tool, SourceURL: "intentionally-invalid-and-unused"}, bytes.NewReader([]byte("archive fixture")), func(reader io.Reader) error {
		encoded, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, &got)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Arguments, []string{"--data-only", "--no-owner", "--no-acl", "--file=-"}) || got.InputLength != len("archive fixture") {
		t.Fatal("the decoder did not use only stdin and data-only stdout")
	}
	wantEnv := map[string]string{"PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C", "TZ": "UTC", "HOME": "/nonexistent"}
	if !reflect.DeepEqual(got.Environment, wantEnv) {
		t.Fatal("the offline decoder inherited connection or credential configuration")
	}
}

func TestBackupCommandDecoderRejectsOverrunPartialConsumptionAndUnsafeErrors(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		body    string
		limit   int64
		consume func(io.Reader) error
		want    error
	}{
		{"stdout_overrun", "import os\nos.write(1, b'x' * 17)", 16, commandConsumeAll, ErrLimit},
		{"stderr_overrun", "import os\nos.write(2, b'x' * 65537)", 16, commandConsumeAll, ErrLimit},
		{"partially_consumed", "import os\nos.write(1, b'abcd')", 16, func(io.Reader) error { return nil }, ErrArchive},
		{"untrusted_consumer_error", "import os\nos.write(1, b'abcd')", 16, func(io.Reader) error { return errors.New("secret connection detail") }, ErrArchive},
		{"trusted_consumer_sentinel", "import os\nos.write(1, b'abcd')", 16, func(io.Reader) error { return fmt.Errorf("secret connection detail: %w", ErrDatabase) }, ErrDatabase},
		{"nonzero_after_eof", "import os, sys\nos.write(1, b'abcd')\nsys.exit(7)", 16, commandConsumeAll, ErrCommand},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			tool := commandFixture(t, fixture.body)
			err := decodeCommand(context.Background(), Options{PGRestore: tool, MaxDumpBytes: fixture.limit}, strings.NewReader("input"), fixture.consume)
			if err != fixture.want {
				t.Fatalf("decoder = %v, want %v", err, fixture.want)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("the decoder disclosed an unsafe error detail")
			}
		})
	}
}

func TestBackupCommandCancellationKillsOwnedDescendants(t *testing.T) {
	for _, decode := range []bool{false, true} {
		name := "dump"
		if decode {
			name = "decode"
		}
		t.Run(name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "child.json")
			body := "import json, os, subprocess, time\nchild = subprocess.Popen(['/usr/bin/python3', '-c', 'import time; time.sleep(20)'])\nwith open(" + pythonString(marker) + ", 'w') as handle:\n    json.dump({'Leader': os.getpid(), 'Child': child.pid}, handle)\n    handle.flush()\nwhile True: time.sleep(1)"
			tool := commandFixture(t, body)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				options := Options{PGDump: tool, PGRestore: tool, SourceURL: commandFixtureURI, Schema: "public", Timeout: 5 * time.Second}
				if decode {
					done <- decodeCommand(ctx, options, strings.NewReader("input"), commandConsumeAll)
				} else {
					var output bytes.Buffer
					done <- dumpCommand(ctx, options, commandFixtureSnapshot, &output)
				}
			}()
			owned := commandWaitMarker(t, marker)
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("owned command did not finish after cancellation")
			}
			commandAssertExited(t, owned.Leader)
			commandAssertExited(t, owned.Child)
		})
	}
}

func TestBackupCommandSuccessfulLeaderRetiresDescendantHoldingPipes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child.json")
	tool := commandFixture(t, "import json, os, subprocess\nchild = subprocess.Popen(['/usr/bin/python3', '-c', 'import time; time.sleep(20)'])\nwith open("+pythonString(marker)+", 'w') as handle:\n    json.dump({'Leader': os.getpid(), 'Child': child.pid}, handle)")
	var output bytes.Buffer
	started := time.Now()
	err := dumpCommand(context.Background(), Options{PGDump: tool, SourceURL: commandFixtureURI, Schema: "public", Timeout: 5 * time.Second}, commandFixtureSnapshot, &output)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("a successful leader left inherited pipes waiting on a descendant")
	}
	owned := commandWaitMarker(t, marker)
	commandAssertExited(t, owned.Leader)
	commandAssertExited(t, owned.Child)
}

func TestBackupCommandDecoderTimeoutClosesBlockedPipeAndWaitsAfterEOF(t *testing.T) {
	for name, body := range map[string]string{
		"blocked_stdout":  "import time\ntime.sleep(20)",
		"eof_before_exit": "import os, time\nos.close(1)\ntime.sleep(20)",
	} {
		t.Run(name, func(t *testing.T) {
			tool := commandFixture(t, body)
			started := time.Now()
			err := decodeCommand(context.Background(), Options{PGRestore: tool, Timeout: 150 * time.Millisecond}, strings.NewReader("input"), commandConsumeAll)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline = %v", err)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("decoder timeout failed to release its process and stdout pipe")
			}
		})
	}
}

func TestBackupCommandConsumerPanicStillReapsOwnedChild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child.json")
	tool := commandFixture(t, "import json, os, time\nwith open("+pythonString(marker)+", 'w') as handle:\n    json.dump({'Leader': os.getpid(), 'Child': os.getpid()}, handle)\nos.write(1, b'ready')\ntime.sleep(20)")
	defer func() {
		if recover() != "consumer fixture panic" {
			t.Error("the consumer panic was not preserved")
		}
		owned := commandWaitMarker(t, marker)
		commandAssertExited(t, owned.Leader)
	}()
	_ = decodeCommand(context.Background(), Options{PGRestore: tool, Timeout: time.Second}, strings.NewReader("input"), func(reader io.Reader) error {
		var one [1]byte
		if _, err := reader.Read(one[:]); err != nil {
			return err
		}
		panic("consumer fixture panic")
	})
	t.Fatal("consumer panic did not propagate")
}

func TestBackupCommandConsumerFailureClosesAnOutstandingCopyReader(t *testing.T) {
	tool := commandFixture(t, "import time\ntime.sleep(20)")
	reading := make(chan struct{})
	done := make(chan struct{})
	err := decodeCommand(context.Background(), Options{PGRestore: tool, Timeout: time.Second}, strings.NewReader("input"), func(reader io.Reader) error {
		go func() {
			defer close(done)
			close(reading)
			_, _ = io.Copy(io.Discard, reader)
		}()
		<-reading
		return ErrDatabase
	})
	if err != ErrDatabase {
		t.Fatalf("early COPY failure = %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a COPY input read remained blocked after its consumer failed")
	}
}

func TestBackupCommandConfigurationAndPrecancelledContextNeverLaunch(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "unexpected-launch")
	tool := commandFixture(t, "open("+pythonString(marker)+", 'w').close()")
	base := Options{SourceURL: commandFixtureURI, PGDump: tool, PGRestore: tool, Schema: "public"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := dumpCommand(ctx, base, commandFixtureSnapshot, &output); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := decodeCommand(ctx, base, strings.NewReader("input"), commandConsumeAll); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := checkToolVersions(ctx, base); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Options){
		"relative_executable":  func(o *Options) { o.PGDump = "pg_dump" },
		"missing_executable":   func(o *Options) { o.PGDump = filepath.Join(t.TempDir(), "not-present") },
		"directory_executable": func(o *Options) { o.PGDump = t.TempDir() },
		"negative_timeout":     func(o *Options) { o.Timeout = -time.Second },
		"excessive_timeout":    func(o *Options) { o.Timeout = 31 * time.Minute },
		"negative_limit":       func(o *Options) { o.MaxDumpBytes = -1 },
		"excessive_limit":      func(o *Options) { o.MaxDumpBytes = maximumCommandBytes + 1 },
		"invalid_pin":          func(o *Options) { o.sourceHostAddress = "other.example" },
		"empty_schema":         func(o *Options) { o.Schema = "" },
		"nul_schema":           func(o *Options) { o.Schema = "public\x00other" },
	} {
		t.Run(name, func(t *testing.T) {
			options := base
			mutate(&options)
			if err := dumpCommand(context.Background(), options, commandFixtureSnapshot, &output); err != ErrConfiguration {
				t.Fatalf("invalid configuration launched or escaped a detail: %v", err)
			}
		})
	}
	for _, snapshot := range []string{"", "0", commandFixtureSnapshot + " --dbname=other", "00000003-0000001B-0", "00000003-0000001b-1"} {
		if err := dumpCommand(context.Background(), base, snapshot, &output); err != ErrConfiguration {
			t.Fatalf("invalid snapshot was accepted: %v", err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a rejected request launched its executable")
	}
}

func commandFixture(t *testing.T, body string) string {
	t.Helper()
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Fatal("remote command verification requires /usr/bin/python3")
	}
	path := filepath.Join(t.TempDir(), "owned-pg-tool")
	if err := os.WriteFile(path, []byte("#!/usr/bin/python3\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func pythonString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func commandConsumeAll(reader io.Reader) error {
	_, err := io.Copy(io.Discard, reader)
	return err
}

type commandNeverWriter struct{}

func (*commandNeverWriter) Write([]byte) (int, error) { panic("an unsupported writer was invoked") }

type commandNeverReader struct{}

func (*commandNeverReader) Read([]byte) (int, error) { panic("an unsupported reader was invoked") }

type commandProcessMarker struct{ Leader, Child int }

func commandWaitMarker(t *testing.T, path string) commandProcessMarker {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		encoded, err := os.ReadFile(path)
		if err == nil {
			var value commandProcessMarker
			if json.Unmarshal(encoded, &value) == nil && value.Leader > 1 && value.Child > 1 {
				return value
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("owned fixture did not acknowledge its child process")
	return commandProcessMarker{}
}

func commandAssertExited(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		encoded, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		if err == nil {
			endName := bytes.LastIndexByte(encoded, ')')
			if endName >= 0 && endName+2 < len(encoded) && (encoded[endName+2] == 'Z' || encoded[endName+2] == 'X') {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the owned process or a descendant remained runnable")
}
