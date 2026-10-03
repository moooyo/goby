package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/metadata"
)

func TestLocalNFOObservationOwnsBytesAndDefersStateChanges(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "Film.nfo")
	document := `<movie><title>Observed title</title></movie>`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	state := &scanState{opened: root, warnings: 7}
	observation, err := state.observeLocalNFO(context.Background(), []string{"Film.nfo", "movie.nfo"}, "movie")
	if err != nil || state.warnings != 7 {
		t.Fatalf("observation changed scanner state: warnings=%d error=%v", state.warnings, err)
	}
	if err := os.WriteFile(path, []byte(`<movie><title>Changed after observation</title></movie>`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	previous := localMetadata{raw: []byte(`{"FutureField":42}`)}
	local := state.applyLocalNFO(observation, "movie", previous)
	if local.value == nil || local.value.Name != "Observed title" || local.path != "Film.nfo" ||
		local.hash == "" || string(local.raw) != string(previous.raw) || state.warnings != 7 {
		t.Fatalf("application did not use the completed immutable observation: local=%+v warnings=%d", local, state.warnings)
	}
}

func TestLocalNFOObservationRetainsInvalidPreferredAndClearsOnlyAbsence(t *testing.T) {
	for _, scenario := range []string{"malformed", "wrong_kind", "oversized", "directory", "absent", "disabled", "previous_kind_changed"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			root, err := os.OpenRoot(directory)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = root.Close() })
			state := &scanState{opened: root, warnings: 3}
			previous := localMetadata{value: &metadata.Metadata{Kind: "movie", Name: "Accepted title"}, hash: "accepted", path: "Film.nfo"}
			fallback := `<movie><title>Unwanted fallback</title></movie>`
			if scenario != "absent" {
				if err := os.WriteFile(filepath.Join(directory, "movie.nfo"), []byte(fallback), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			document := ""
			switch scenario {
			case "malformed":
				document = `<movie><title>Broken</movie>`
			case "wrong_kind":
				document = `<tvshow><title>Wrong kind</title></tvshow>`
			case "oversized":
				document = strings.Repeat("x", maxLocalNFOBytes+1)
			case "directory":
				if err := os.Mkdir(filepath.Join(directory, "Film.nfo"), 0o700); err != nil {
					t.Fatal(err)
				}
			case "disabled", "previous_kind_changed":
				state.library.Options = &LibraryOptions{}
				if err := root.Close(); err != nil {
					t.Fatal(err)
				}
				if scenario == "previous_kind_changed" {
					previous.value.Kind = "tvshow"
				}
			}
			if document != "" {
				if err := os.WriteFile(filepath.Join(directory, "Film.nfo"), []byte(document), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			observation, err := state.observeLocalNFO(context.Background(), []string{"Film.nfo", "movie.nfo"}, "movie")
			if err != nil || state.warnings != 3 {
				t.Fatalf("observation parsed or warned before its phase retired: warnings=%d error=%v", state.warnings, err)
			}
			local := state.applyLocalNFO(observation, "movie", previous)
			switch scenario {
			case "absent", "previous_kind_changed":
				if !reflect.DeepEqual(local, localMetadata{}) || state.warnings != 3 {
					t.Fatalf("obsolete source was retained: local=%+v warnings=%d", local, state.warnings)
				}
			case "disabled":
				if !reflect.DeepEqual(local, previous) || state.warnings != 3 {
					t.Fatalf("disabled import changed accepted facts: local=%+v warnings=%d", local, state.warnings)
				}
			default:
				if !reflect.DeepEqual(local, previous) || state.warnings != 4 {
					t.Fatalf("invalid preferred NFO used a fallback or replaced accepted facts: local=%+v warnings=%d", local, state.warnings)
				}
			}
		})
	}
}

type localNFOFaultFile struct {
	*os.File
	readErr   error
	closeErr  error
	afterRead func()
	closes    int
}

func (file *localNFOFaultFile) Read(buffer []byte) (int, error) {
	if file.readErr != nil {
		return 0, file.readErr
	}
	count, err := file.File.Read(buffer)
	if file.afterRead != nil {
		file.afterRead()
	}
	return count, err
}

func (file *localNFOFaultFile) Close() error {
	file.closes++
	return errors.Join(file.File.Close(), file.closeErr)
}

func TestLocalNFOObservationJoinsActualCloseAndPreservesLifecycleFailures(t *testing.T) {
	readFailure := errors.New("injected NFO read failure")
	closeFailure := errors.New("injected NFO close failure")
	for _, scenario := range []string{"success", "read", "close", "read_and_close", "cancel_after_read"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Film.nfo")
			if err := os.WriteFile(path, []byte(`<movie><title>Bounded read</title></movie>`), 0o600); err != nil {
				t.Fatal(err)
			}
			opened, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = opened.Close() })
			before, err := opened.Stat()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			file := &localNFOFaultFile{File: opened}
			if scenario == "read" || scenario == "read_and_close" {
				file.readErr = readFailure
			}
			if scenario == "close" || scenario == "read_and_close" {
				file.closeErr = closeFailure
			}
			if scenario == "cancel_after_read" {
				file.afterRead = cancel
			}
			data, invalid, err := readOpenedLocalNFO(ctx, file, before, func() (os.FileInfo, error) { return os.Stat(path) })
			if file.closes != 1 {
				t.Fatalf("descriptor close calls=%d, want one", file.closes)
			}
			if _, statErr := opened.Stat(); !errors.Is(statErr, os.ErrClosed) {
				t.Fatalf("observation returned before actual descriptor close: %v", statErr)
			}
			if invalid || scenario == "success" && (err != nil || len(data) == 0) || scenario != "success" && err == nil {
				t.Fatalf("unexpected observation result: bytes=%d invalid=%v error=%v", len(data), invalid, err)
			}
			if file.readErr != nil && !errors.Is(err, readFailure) || file.closeErr != nil && !errors.Is(err, closeFailure) {
				t.Fatalf("read or close failure was lost: %v", err)
			}
			fatal := localNFOFatalReadError(ctx, err)
			switch scenario {
			case "close", "read_and_close":
				if !errors.Is(fatal, errSidecarRetirementUnknown) || !errors.Is(fatal, closeFailure) {
					t.Fatalf("uncertain close became an ordinary warning: %v", fatal)
				}
			case "cancel_after_read":
				if !errors.Is(fatal, context.Canceled) {
					t.Fatalf("canceled observation became an ordinary warning: %v", fatal)
				}
			default:
				if fatal != nil {
					t.Fatalf("ordinary source failure changed the scan lifecycle: %v", fatal)
				}
			}
		})
	}
	if err := localNFOFatalReadError(context.Background(), errors.Join(io.ErrUnexpectedEOF, context.DeadlineExceeded)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("joined read error concealed a deadline: %v", err)
	}
}
