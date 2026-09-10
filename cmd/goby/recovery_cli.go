package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/recovery"
)

var errCLIInput = errors.New("invalid recovery command; use goby recovery help")
var errCLINoRollback = errors.New("offline activation requires --accept-no-rollback; the original database is preserved but cannot be certified as a rollback copy")

// runCLI executes locally authorized maintenance while holding the same
// deployment fence as serve. It never logs command arguments or passphrases.
func runCLI(ctx context.Context, cfg config.Config, args []string) (handled bool, runErr error) {
	defer func() {
		if handled && runErr != nil {
			_, _ = fmt.Fprintln(os.Stderr, safeCLIError(runErr))
		}
	}()
	if len(args) == 0 || args[0] == "serve" {
		if len(args) > 1 {
			return true, errCLIInput
		}
		return false, nil
	}
	if flag.Lookup("test.v") != nil && strings.HasPrefix(args[0], "-test.") {
		return false, nil
	}
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") ||
		len(args) == 2 && args[0] == "recovery" && args[1] == "help" {
		_, err := io.WriteString(os.Stdout, recoveryCLIHelp)
		return true, err
	}
	if len(args) < 2 {
		return true, errCLIInput
	}
	command := args[0] + " " + args[1]
	allowed := map[string]bool{"recovery status": true, "recovery jobs": true, "recovery cancel": true,
		"recovery resume": true, "backup list": true, "backup import": true,
		"restore plan": true, "restore apply": true, "restore rollback": true}
	if !allowed[command] {
		return true, errCLIInput
	}
	flags := flag.NewFlagSet("recovery", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var id, requestID, backupID, digest, revision, generation, file, passFile string
	var passStdin, restoreDefaults, replaceRollback, acceptNoRollback bool
	start, limit := 0, 25
	switch command {
	case "recovery jobs", "backup list":
		flags.IntVar(&start, "start", 0, "First item")
		flags.IntVar(&limit, "limit", 25, "Page size")
	case "recovery cancel":
		flags.StringVar(&id, "id", "", "Operation ID")
		flags.StringVar(&revision, "revision", "", "Operation revision")
	case "backup import":
		flags.StringVar(&file, "file", "", "Encrypted archive file")
		flags.StringVar(&requestID, "request-id", "", "Unique 32-character lowercase hexadecimal request ID")
	case "restore plan":
		flags.StringVar(&backupID, "backup-id", "", "Backup ID")
		flags.StringVar(&digest, "sha256", "", "Exact encrypted archive SHA-256")
		flags.StringVar(&requestID, "request-id", "", "Unique request ID")
		flags.StringVar(&generation, "generation-revision", "", "Current generation revision")
		flags.StringVar(&passFile, "passphrase-file", "", "Private passphrase file")
		flags.BoolVar(&passStdin, "passphrase-stdin", false, "Read exact passphrase bytes from standard input")
		flags.BoolVar(&restoreDefaults, "restore-defaults", false, "Restore allowlisted logical defaults")
		flags.BoolVar(&replaceRollback, "replace-rollback", false, "Replace the explicitly owned inactive copy")
	case "restore apply":
		flags.StringVar(&id, "id", "", "Ready plan ID")
		flags.StringVar(&revision, "revision", "", "Ready plan revision")
		flags.StringVar(&generation, "generation-revision", "", "Current generation revision")
		flags.BoolVar(&acceptNoRollback, "accept-no-rollback", false, "Proceed without capturing an unavailable original database")
	case "restore rollback":
		flags.StringVar(&requestID, "request-id", "", "Unique request ID")
		flags.StringVar(&generation, "generation-revision", "", "Current generation revision")
		flags.BoolVar(&acceptNoRollback, "accept-no-rollback", false, "Proceed without capturing the current database")
	}
	if flags.Parse(args[2:]) != nil || flags.NArg() != 0 {
		return true, errCLIInput
	}
	validID := func(value string, length int) bool {
		if len(value) != length {
			return false
		}
		for _, c := range value {
			if c < '0' || c > '9' {
				if c < 'a' || c > 'f' {
					return false
				}
			}
		}
		return true
	}
	validRevision := func(value string) bool {
		n, err := strconv.ParseUint(value, 10, 64)
		return err == nil && strconv.FormatUint(n, 10) == value
	}
	valid := true
	switch command {
	case "recovery jobs", "backup list":
		valid = start >= 0 && int64(start) <= 2147483647 && limit >= 1 && limit <= 100
	case "recovery cancel":
		valid = validID(id, 32) && validRevision(revision)
	case "backup import":
		valid = file != "" && validID(requestID, 32)
	case "restore plan":
		valid = validID(backupID, 32) && validID(digest, 64) && validID(requestID, 32) &&
			validRevision(generation) && ((passFile != "") != passStdin)
	case "restore apply":
		valid = validID(id, 32) && validRevision(revision) && validRevision(generation)
	case "restore rollback":
		valid = validID(requestID, 32) && validRevision(generation)
	}
	if !valid {
		return true, errCLIInput
	}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(diagnostics.NewHandler(nil, slog.NewJSONHandler(os.Stderr, nil))))
	defer slog.SetDefault(previousLogger)
	if (command == "restore apply" || command == "restore rollback") && !acceptNoRollback {
		return true, errCLINoRollback
	}
	var passphrase []byte
	if command == "restore plan" {
		var err error
		passphrase, err = readCLIPassphrase(ctx, passFile, passStdin)
		if err != nil {
			return true, err
		}
		defer clear(passphrase)
	}
	work, cancel := context.WithTimeout(ctx, cfg.Recovery.WithDefaults().OperationTimeout)
	defer cancel()
	runtime, err := recovery.Open(work, cfg)
	if err != nil {
		return true, errors.New("exclusive recovery storage unavailable; stop the service and use its deployment owner and configuration")
	}
	// The shared fence outlives every generation and operator worker, including
	// cleanup after cancellation. A failed cleanup deliberately retains it.
	closeRuntime := true
	defer func() {
		if closeRuntime {
			if err := runtime.Close(); err != nil && runErr == nil {
				runErr = recovery.ErrUnavailable
			}
		}
	}()
	cfg, _, err = runtime.ActiveConfig(work)
	if err != nil {
		return true, recovery.ErrUnavailable
	}
	manager, err := recovery.NewOfflineManager(work, runtime, cfg, version)
	if err != nil {
		return true, recovery.ErrUnavailable
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if manager.Close(cleanup) != nil {
			closeRuntime = false
			if runErr == nil {
				runErr = recovery.ErrUnavailable
			}
		}
	}()
	if command != "recovery status" && command != "recovery jobs" && command != "backup list" {
		if err := manager.Reconcile(work); err != nil {
			return true, recovery.ErrUnavailable
		}
	}
	var result any
	var operation recovery.OperationView
	switch command {
	case "recovery status":
		result, err = manager.OperatorStatus(work)
	case "recovery jobs":
		result, err = manager.OperatorOperations(work, start, limit)
	case "backup list":
		result, err = manager.OperatorBackups(work, start, limit)
	case "recovery cancel":
		result, err = manager.OperatorCancel(work, id, revision)
	case "backup import":
		var input *os.File
		input, err = openCLIArchive(file)
		if err == nil {
			defer input.Close()
			result, err = manager.OperatorImport(work, requestID, input)
		}
	case "restore plan":
		operation, err = manager.OperatorPlan(work, recovery.PlanRequest{RequestId: requestID, BackupId: backupID,
			SHA256: digest, Passphrase: passphrase, RestoreDefaults: restoreDefaults, ReplaceRollback: replaceRollback,
			GenerationRevision: generation})
		if err == nil {
			result, err = waitCLIOperation(work, manager, operation.Id)
		}
	case "restore apply":
		operation, err = manager.OperatorApply(work, id, recovery.ApplyRequest{Revision: revision, GenerationRevision: generation})
	case "restore rollback":
		operation, err = manager.OperatorRollback(work, recovery.RollbackRequest{RequestId: requestID, GenerationRevision: generation})
	case "recovery resume":
		operation.Id, _, err = manager.PendingSwitch(work)
		if err == nil && operation.Id == "" {
			return true, recovery.ErrNotFound
		}
	}
	if err != nil {
		return true, safeCLIError(err)
	}
	if command == "restore apply" || command == "restore rollback" || command == "recovery resume" {
		result, err = activateCLI(work, runtime, manager, operation.Id, &closeRuntime)
		if err != nil {
			return true, safeCLIError(err)
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return true, errors.New("recovery output unavailable; inspect recovery jobs before retrying")
	}
	return true, nil
}

func waitCLIOperation(ctx context.Context, manager *recovery.Manager, id string) (recovery.OperationView, error) {
	timer := time.NewTicker(200 * time.Millisecond)
	defer timer.Stop()
	for {
		op, err := manager.OperatorOperation(ctx, id)
		if err != nil {
			return op, err
		}
		switch op.State {
		case "ready", "completed":
			return op, nil
		case "failed", "cancelled", "interrupted":
			// Fixed coordinator classifications are safe for operator output.
			return op, fmt.Errorf("recovery operation %s: %s", op.Id, op.ErrorCode)
		}
		select {
		case <-ctx.Done():
			return op, ctx.Err()
		case <-timer.C:
		}
	}
}

type cliSwitchManager interface {
	PendingSwitch(context.Context) (string, bool, error)
	OperatorOperation(context.Context, string) (recovery.OperationView, error)
	PrepareSwitch(context.Context, string) (*recovery.SwitchCandidate, error)
}

func activateCLI(ctx context.Context, runtime *recovery.Runtime, manager cliSwitchManager, id string, closeRuntime *bool) (result any, resultErr error) {
	pendingID, activated, err := manager.PendingSwitch(ctx)
	if err != nil {
		return nil, err
	}
	if pendingID == "" {
		op, err := manager.OperatorOperation(ctx, id)
		if err != nil {
			return nil, err
		}
		if op.State != "completed" || op.Kind != "restore" && op.Kind != "rollback" {
			return nil, recovery.ErrConflict
		}
		return cliActivationResult(ctx, runtime, id)
	}
	if pendingID != id {
		return nil, recovery.ErrConflict
	}
	var candidate *recovery.SwitchCandidate
	if !activated {
		candidate, err = manager.PrepareSwitch(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	logger := slog.New(diagnostics.NewHandler(nil, slog.NewJSONHandler(os.Stderr, nil)))
	var next *generation
	if candidate == nil {
		next, err = prepareGeneration(ctx, runtime, logger, nil, version, nil, nil)
	} else {
		next, err = prepareGeneration(ctx, runtime, logger, nil, version, candidate.Pool, candidate.Lease)
	}
	if next != nil {
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if next.Close(cleanup) != nil {
				*closeRuntime = false
				if resultErr == nil {
					resultErr = recovery.ErrUnavailable
				}
			}
		}()
	}
	if err != nil {
		return nil, err
	}
	if err := next.manager.AcceptSwitch(ctx); err != nil {
		return nil, err
	}
	return cliActivationResult(ctx, runtime, id)
}

func cliActivationResult(ctx context.Context, runtime *recovery.Runtime, id string) (any, error) {
	_, state, err := runtime.ActiveConfig(ctx)
	if err != nil {
		return nil, err
	}
	return struct {
		Status             string
		OperationId        string
		GenerationRevision string
		StartService       bool
	}{"completed", id, fmt.Sprint(state.Revision), true}, nil
}

func safeCLIError(err error) error {
	for _, safe := range []error{errCLIInput, errCLINoRollback, context.Canceled, context.DeadlineExceeded, recovery.ErrInvalid,
		recovery.ErrBusy, recovery.ErrConflict, recovery.ErrNotFound, recovery.ErrArchive, recovery.ErrCapacity} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	return errors.New("recovery command did not complete; inspect recovery status and jobs before retrying")
}

const recoveryCLIHelp = `Goby offline recovery

Stop the service. Run as its deployment owner with the same environment.
All output is JSON; request IDs are caller-selected 32-character lowercase hex.
No command accepts a database URL, master key, or passphrase as an argument.

  goby recovery status
  goby recovery jobs [--start 0 --limit 25]
  goby backup list [--start 0 --limit 25]
  goby backup import --file ARCHIVE --request-id REQUEST
  goby restore plan --backup-id BACKUP --sha256 DIGEST --request-id REQUEST
      --generation-revision REV --passphrase-file PRIVATE_FILE
      [--restore-defaults] [--replace-rollback]
  goby restore apply --id PLAN --revision REV --generation-revision GEN
      --accept-no-rollback
  goby restore rollback --request-id REQUEST --generation-revision GEN
      --accept-no-rollback
  goby recovery cancel --id OPERATION --revision REV
  goby recovery resume

Use --passphrase-stdin instead of --passphrase-file to read standard input.
Passphrases are exact UTF-8 bytes (12..1024); newlines are not stripped.
Passphrase files must be regular, owned by the current UID, mode 0600, with
one link and no final symlink. Original media files are never in the archive.
Plans are staged without changing the active database. Offline activation
does not connect to or certify a rollback copy of an unavailable original
database. After successful activation, start the service and sign in again.
`
