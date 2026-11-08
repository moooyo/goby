package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/tasks"
)

const taskBackupResultTestRun = "00000000000000000000000000000001"
const taskBackupResultTestID = "00000000000000000000000000000002"
const taskBackupResultTestSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestTaskBackupResultsRetainExactMetadataAndOmitUnconfirmedValues(t *testing.T) {
	association := taskBackupAssociation{runID: taskBackupResultTestRun, backupID: taskBackupResultTestID, deploymentID: "current-deployment", state: "completed", digest: taskBackupResultTestSHA}
	metadata := backupstore.Metadata{ID: taskBackupResultTestID, Kind: backupstore.KindGenerated, State: backupstore.StateReady,
		Size: 9007199254740993, Digest: taskBackupResultTestSHA, Verified: true}
	for _, test := range []struct {
		name   string
		change func(*taskBackupAssociation, *backupstore.Metadata)
		err    error
		status string
	}{
		{name: "published", status: "available"},
		{name: "unverified", status: "available", change: func(_ *taskBackupAssociation, m *backupstore.Metadata) { m.Verified = false }},
		{name: "writing", status: "available", change: func(a *taskBackupAssociation, m *backupstore.Metadata) {
			a.state, a.digest, m.State, m.Digest, m.Size, m.Verified = "running", "", backupstore.StateWriting, "", 123, false
		}},
		{name: "prepared", status: "available", change: func(a *taskBackupAssociation, m *backupstore.Metadata) {
			a.state, m.State, m.Verified = "publishing", backupstore.StateWriting, false
		}},
		{name: "no association", status: "not_recorded", change: func(a *taskBackupAssociation, _ *backupstore.Metadata) { *a = taskBackupAssociation{runID: a.runID} }},
		{name: "deleted", status: "deleted", change: func(a *taskBackupAssociation, _ *backupstore.Metadata) { a.state = "deleted" }},
		{name: "deleting is not deleted", status: "deleting", err: backupstore.ErrNotFound, change: func(a *taskBackupAssociation, _ *backupstore.Metadata) { a.state = "deleting" }},
		{name: "missing is not deleted", status: "missing", err: backupstore.ErrNotFound},
		{name: "storage failure", status: "unavailable", err: errors.New("private filesystem path /private/backup")},
		{name: "foreign deployment", status: "mismatch", change: func(a *taskBackupAssociation, _ *backupstore.Metadata) { a.deploymentID = "foreign-deployment-secret" }},
		{name: "restored evidence", status: "mismatch", change: func(a *taskBackupAssociation, _ *backupstore.Metadata) { a.state = "restored" }},
		{name: "imported object", status: "mismatch", change: func(_ *taskBackupAssociation, m *backupstore.Metadata) { m.Kind = backupstore.KindImported }},
		{name: "manual generated object", status: "mismatch", change: func(_ *taskBackupAssociation, m *backupstore.Metadata) {
			m.CreatorID, m.SessionID = "private-user", "private-credential"
		}},
		{name: "different object", status: "mismatch", change: func(_ *taskBackupAssociation, m *backupstore.Metadata) { m.ID = strings.Repeat("f", 32) }},
		{name: "different digest", status: "mismatch", change: func(_ *taskBackupAssociation, m *backupstore.Metadata) { m.Digest = strings.Repeat("f", 64) }},
		{name: "missing publication digest", status: "mismatch", change: func(a *taskBackupAssociation, _ *backupstore.Metadata) { a.digest = "" }},
		{name: "unbound verified metadata", status: "mismatch", change: func(a *taskBackupAssociation, _ *backupstore.Metadata) { a.state, a.digest = "running", "" }},
		{name: "verified writing metadata", status: "mismatch", change: func(_ *taskBackupAssociation, m *backupstore.Metadata) { m.State = backupstore.StateWriting }},
		{name: "negative size", status: "mismatch", change: func(_ *taskBackupAssociation, m *backupstore.Metadata) { m.Size = -1 }},
		{name: "unknown archive state", status: "mismatch", change: func(_ *taskBackupAssociation, m *backupstore.Metadata) { m.State = "private-state" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, m := association, metadata
			if test.change != nil {
				test.change(&a, &m)
			}
			result := taskBackupMetadataResult(a, "current-deployment", m, test.err)
			if result.Status != test.status {
				t.Fatalf("status=%s want=%s", result.Status, test.status)
			}
			if test.status == "available" {
				if result.BackupId != a.backupID || result.State != string(m.State) || result.SHA256 != m.Digest || result.Verified == nil || *result.Verified != m.Verified {
					t.Fatalf("projection did not retain exact metadata: %#v", result)
				}
				wantSize := "9007199254740993"
				if test.name == "writing" {
					wantSize = "123"
				}
				if result.SizeBytes != wantSize {
					t.Fatalf("size lost decimal precision: %q", result.SizeBytes)
				}
			} else if result.SizeBytes != "" || result.SHA256 != "" || result.Verified != nil {
				t.Fatalf("unconfirmed result exposed archive metadata: %#v", result)
			}
			if (test.status == "mismatch" || test.status == "not_recorded") && result.BackupId != "" {
				t.Fatal("unconfirmed association exposed an object identifier")
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"private-", "foreign-deployment", "Source", "Actor", "Deployment", "Session", "Creator"} {
				if strings.Contains(string(encoded), private) {
					t.Fatalf("projection exposed private metadata: %s", encoded)
				}
			}
			if test.status != "available" && (strings.Contains(string(encoded), "SizeBytes") || strings.Contains(string(encoded), "SHA256") || strings.Contains(string(encoded), "Verified")) {
				t.Fatalf("unknown values were serialized as measurements: %s", encoded)
			}
		})
	}
}

func TestTaskBackupResultsRejectUnboundedOrInexactIdentifiersBeforeRead(t *testing.T) {
	var manager *Manager
	for _, ids := range [][]string{{"1"}, {strings.Repeat("a", 31)}, {strings.Repeat("A", 32)}, {taskBackupResultTestRun + "\n"}, make([]string, tasks.MaxPageLimit+1)} {
		if result, err := manager.TaskBackupResults(t.Context(), identity.Principal{}, ids); !errors.Is(err, ErrInvalid) || result != nil {
			t.Fatalf("invalid identifiers reached the manager: result=%v err=%v", result, err)
		}
	}
	if result, err := manager.TaskBackupResults(t.Context(), identity.Principal{}, []string{taskBackupResultTestRun}); !errors.Is(err, ErrUnavailable) || result != nil {
		t.Fatalf("unavailable manager invented results: result=%v err=%v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := manager.TaskBackupResults(ctx, identity.Principal{}, []string{taskBackupResultTestRun}); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("request cancellation was hidden: result=%v err=%v", result, err)
	}
}
