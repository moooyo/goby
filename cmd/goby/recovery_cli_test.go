package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/recovery"
)

const cliTestID = "0123456789abcdef0123456789abcdef"
const cliTestDigest = cliTestID + cliTestID

func cliTestConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	paths := map[string]string{
		"GOBY_RECOVERY_STATE_DIR":      filepath.Join(root, "state"),
		"GOBY_BACKUP_DIR":              filepath.Join(root, "backups"),
		"GOBY_RECOVERY_OPERATIONS_DIR": filepath.Join(root, "operations"),
	}
	for name, path := range paths {
		t.Setenv(name, path)
	}
	cfg := config.Config{StartupTimeout: time.Second}
	cfg.Recovery.Directory, cfg.Recovery.Backups.Directory, cfg.Recovery.OperationsDirectory =
		paths["GOBY_RECOVERY_STATE_DIR"], paths["GOBY_BACKUP_DIR"], paths["GOBY_RECOVERY_OPERATIONS_DIR"]
	return cfg
}

func captureCLIInput(t *testing.T, cfg config.Config, args []string) (bool, error, string, string) {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "stdout-")
	if err != nil {
		t.Fatal("create owned CLI output")
	}
	defer output.Close()
	errorsFile, err := os.CreateTemp(t.TempDir(), "stderr-")
	if err != nil {
		t.Fatal("create owned CLI error output")
	}
	defer errorsFile.Close()
	previousOutput, previousErrors := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = output, errorsFile
	defer func() { os.Stdout, os.Stderr = previousOutput, previousErrors }()
	handled, callErr := runCLI(context.Background(), cfg, args)
	read := func(file *os.File) string {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			t.Fatal("rewind owned CLI output")
		}
		data, err := io.ReadAll(io.LimitReader(file, 16385))
		if err != nil || len(data) > 16384 {
			t.Fatal("CLI input output exceeded its bound")
		}
		return string(data)
	}
	return handled, callErr, read(output), read(errorsFile)
}

func assertCLIStoresUnopened(t *testing.T, cfg config.Config) {
	t.Helper()
	for _, path := range []string{cfg.Recovery.Directory, cfg.Recovery.Backups.Directory, cfg.Recovery.OperationsDirectory} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid CLI input opened recovery storage")
		}
	}
}

func TestCLIRejectsInvalidInputBeforeSecretsOrRecoveryStorage(t *testing.T) {
	plan := []string{"restore", "plan", "--backup-id", cliTestID, "--sha256", cliTestDigest, "--request-id", cliTestID, "--generation-revision", "0"}
	for _, test := range []struct {
		name string
		args []string
	}{
		{"unknown command", []string{"private-secret-command"}},
		{"secret flag", []string{"restore", "plan", "--passphrase", "private-secret"}},
		{"deployment override", []string{"backup", "list", "--database-url", "postgres://private-secret@host/database"}},
		{"positional secret", []string{"backup", "list", "private-secret"}},
		{"negative start", []string{"backup", "list", "--start=-1"}},
		{"overflow start", []string{"recovery", "jobs", "--start=2147483648"}},
		{"zero page", []string{"backup", "list", "--limit=0"}},
		{"large page", []string{"backup", "list", "--limit=101"}},
		{"missing file", []string{"backup", "import", "--request-id", cliTestID}},
		{"missing request", []string{"backup", "import", "--file", "/private/never-opened"}},
		{"uppercase request", []string{"backup", "import", "--file", "/private/never-opened", "--request-id", strings.ToUpper(cliTestID)}},
		{"missing cancel revision", []string{"recovery", "cancel", "--id", cliTestID}},
		{"noncanonical revision", []string{"recovery", "cancel", "--id", cliTestID, "--revision", "01"}},
		{"overflow revision", []string{"restore", "apply", "--id", cliTestID, "--revision", "18446744073709551616", "--generation-revision", "0", "--accept-no-rollback"}},
		{"missing apply id", []string{"restore", "apply", "--revision", "1", "--generation-revision", "0", "--accept-no-rollback"}},
		{"missing rollback generation", []string{"restore", "rollback", "--request-id", cliTestID, "--accept-no-rollback"}},
		{"no secret source", append(append([]string(nil), plan...), []string{}...)},
		{"both secret sources", append(append([]string(nil), plan...), "--passphrase-file", "/private/never-opened", "--passphrase-stdin")},
		{"missing plan selectors before stdin", []string{"restore", "plan", "--passphrase-stdin"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := cliTestConfig(t)
			handled, err, out, diagnostic := captureCLIInput(t, cfg, test.args)
			if !handled || !errors.Is(err, errCLIInput) || out != "" || !strings.Contains(diagnostic, errCLIInput.Error()) {
				t.Fatal("invalid CLI arguments were not rejected before execution")
			}
			for _, secret := range []string{"private-secret", "/private/", "postgres://"} {
				if strings.Contains(diagnostic, secret) {
					t.Fatal("CLI argument rejection exposed private input")
				}
			}
			assertCLIStoresUnopened(t, cfg)
		})
	}
}

func TestCLIRequiresExplicitOfflineActivationConsentBeforeOpeningStorage(t *testing.T) {
	for _, args := range [][]string{
		{"restore", "apply", "--id", cliTestID, "--revision", "1", "--generation-revision", "0"},
		{"restore", "apply", "--id", cliTestID, "--revision", "1", "--generation-revision", "0", "--accept-no-rollback=false"},
		{"restore", "rollback", "--request-id", cliTestID, "--generation-revision", "0"},
	} {
		cfg := cliTestConfig(t)
		handled, err, out, _ := captureCLIInput(t, cfg, args)
		if !handled || !errors.Is(err, errCLINoRollback) || out != "" {
			t.Fatal("offline activation did not require explicit no-rollback consent")
		}
		assertCLIStoresUnopened(t, cfg)
	}
}

func TestCLIHelpAndServeDispatchHaveNoRecoverySideEffects(t *testing.T) {
	for _, test := range []struct {
		args    []string
		handled bool
	}{
		{nil, false}, {[]string{"serve"}, false}, {[]string{"-test.v=true"}, false}, {[]string{"recovery", "help"}, true},
	} {
		cfg := cliTestConfig(t)
		handled, err, out, diagnostic := captureCLIInput(t, cfg, test.args)
		if err != nil || handled != test.handled || diagnostic != "" {
			t.Fatal("CLI command dispatch changed ordinary serving or help")
		}
		if handled && !strings.Contains(out, "Goby offline recovery") || !handled && out != "" {
			t.Fatal("CLI dispatch emitted unrelated output")
		}
		assertCLIStoresUnopened(t, cfg)
	}
}

func TestCLIErrorProjectionNeverReturnsPrivateWrappedDetails(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, recovery.ErrConflict, recovery.ErrArchive, errors.New("private-secret")} {
		err := safeCLIError(fmt.Errorf("postgres://private-secret@host/private-path: %w", cause))
		if strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "postgres://") {
			t.Fatal("CLI error projection exposed a private wrapped error")
		}
	}
}

type cliReplaySwitchManager struct {
	pendingID    string
	activated    bool
	operation    recovery.OperationView
	pendingCalls int
	operationIDs []string
	prepareIDs   []string
}

func (m *cliReplaySwitchManager) PendingSwitch(context.Context) (string, bool, error) {
	m.pendingCalls++
	return m.pendingID, m.activated, nil
}

func (m *cliReplaySwitchManager) OperatorOperation(_ context.Context, id string) (recovery.OperationView, error) {
	m.operationIDs = append(m.operationIDs, id)
	return m.operation, nil
}

func (m *cliReplaySwitchManager) PrepareSwitch(_ context.Context, id string) (*recovery.SwitchCandidate, error) {
	m.prepareIDs = append(m.prepareIDs, id)
	return nil, errors.New("completed or unrelated CLI replay must not prepare a switch")
}

func TestCLICompletedActivationReplayReturnsCurrentStateWithoutPreparingAgain(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the private recovery runtime requires Linux")
	}
	for _, kind := range []string{"restore", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			cfg := cliTestConfig(t)
			cfg.Recovery.Backups.MinFreeBytes = 1
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			store, err := recovery.Open(ctx, cfg)
			if err != nil {
				t.Fatal("open the isolated replay runtime")
			}
			defer func() {
				if err := store.Close(); err != nil {
					t.Error("close the isolated replay runtime")
				}
			}()
			_, before, err := store.ActiveConfig(ctx)
			if err != nil {
				t.Fatal("read the existing replay generation")
			}
			manager := &cliReplaySwitchManager{operation: recovery.OperationView{Id: cliTestID, Kind: kind, State: "completed"}}
			closeRuntime := true
			result, err := activateCLI(ctx, store, manager, cliTestID, &closeRuntime)
			if err != nil || manager.pendingCalls != 1 || len(manager.prepareIDs) != 0 ||
				len(manager.operationIDs) != 1 || manager.operationIDs[0] != cliTestID || !closeRuntime {
				t.Fatal("completed activation replay changed admission or runtime ownership")
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal("encode the safe replay result")
			}
			var view struct {
				Status             string
				OperationId        string
				GenerationRevision string
				StartService       bool
			}
			if json.Unmarshal(encoded, &view) != nil || view.Status != "completed" || view.OperationId != cliTestID ||
				view.GenerationRevision != "0" || !view.StartService {
				t.Fatal("completed replay did not report its requested operation and current generation")
			}
			_, after, err := store.ActiveConfig(ctx)
			if err != nil || after != before {
				t.Fatal("completed replay changed the active generation")
			}
		})
	}
}

func TestCLIActivationReplayRejectsAnotherPendingOperationBeforePreparation(t *testing.T) {
	for _, activated := range []bool{false, true} {
		manager := &cliReplaySwitchManager{pendingID: strings.Repeat("a", 32), activated: activated,
			operation: recovery.OperationView{Id: cliTestID, Kind: "restore", State: "completed"}}
		closeRuntime := true
		// A foreign pending operation must be rejected before runtime access,
		// including when that operation has already published its target.
		result, err := activateCLI(context.Background(), nil, manager, cliTestID, &closeRuntime)
		if !errors.Is(err, recovery.ErrConflict) || result != nil || manager.pendingCalls != 1 ||
			len(manager.operationIDs) != 0 || len(manager.prepareIDs) != 0 || !closeRuntime {
			t.Fatal("a replay entered another operation's preparation or acceptance path")
		}
	}
}

func TestCLIActivationWithoutPendingRejectsIncompleteOrUnrelatedOperations(t *testing.T) {
	for _, operation := range []recovery.OperationView{
		{Kind: "restore", State: "ready"}, {Kind: "restore", State: "running"},
		{Kind: "restore", State: "failed"}, {Kind: "rollback", State: "interrupted"},
		{Kind: "create", State: "completed"}, {Kind: "import", State: "completed"}, {Kind: "delete", State: "completed"},
	} {
		operation.Id = cliTestID
		manager := &cliReplaySwitchManager{operation: operation}
		closeRuntime := true
		result, err := activateCLI(context.Background(), nil, manager, cliTestID, &closeRuntime)
		if !errors.Is(err, recovery.ErrConflict) || result != nil || manager.pendingCalls != 1 ||
			len(manager.operationIDs) != 1 || manager.operationIDs[0] != cliTestID || len(manager.prepareIDs) != 0 || !closeRuntime {
			t.Fatal("an operation without a valid completed activation was prepared or accepted")
		}
	}
}
