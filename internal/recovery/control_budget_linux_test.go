//go:build linux

package recovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recoverycontrol"
	"github.com/moooyo/goby/internal/recoverydb"
)

func budgetFixture(t *testing.T, count int) controlData {
	t.Helper()
	deployment := strings.Repeat("1", 32)
	data := controlData{Version: 1, DeploymentID: deployment, Operations: []operationRecord{},
		Slots: []slotRecord{{Slot: lifecycle.DatabasePrimary, State: "active"}, {Slot: lifecycle.DatabaseRecovery, State: "unclaimed"}}}
	migrations, err := database.EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	facts := backupformat.SourceFacts{SchemaVersion: int64(len(migrations)), SchemaSHA256: strings.Repeat("a", 64),
		ProbeVersion: 6, PostgreSQLVersion: "17.11", PostgreSQLVersionNum: 170011, DatabaseSchema: "public", ServerID: "capacity-fixture"}
	for _, migration := range migrations {
		facts.MigrationChecksums = append(facts.MigrationChecksums, backupformat.MigrationFact{Version: migration.Version, Name: migration.Name, SHA256: migration.SHA256})
	}
	for i := 0; i < 35; i++ {
		facts.Tables = append(facts.Tables, backupformat.TableFact{Name: fmt.Sprintf("capacity_table_%02d", i), Rows: 1, SHA256: strings.Repeat("b", 64)})
	}
	when := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("%032x", i+1)
		marker := recoverydb.Marker{Version: 1, DeploymentID: deployment, GenerationID: id, Slot: lifecycle.DatabaseRecovery}
		raw, err := recoverydb.EncodeMarker(marker)
		if err != nil {
			t.Fatal(err)
		}
		manifest := &backupformat.Manifest{Format: backupformat.FormatVersion, ID: id, CreatedAt: when, GobyVersion: "capacity-fixture", Source: facts,
			Files: []backupformat.File{{Name: backupformat.DatabaseName, Size: 4096, SHA256: strings.Repeat("c", 64)},
				{Name: backupformat.ConfigurationName, Size: 256, SHA256: strings.Repeat("d", 64)}}}
		if err := backupformat.ValidateManifest(*manifest, backupformat.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
		summary := Summary(*manifest)
		data.Operations = append(data.Operations, operationRecord{
			ID: id, RequestID: id, Fingerprint: strings.Repeat("e", 64), Revision: 7, Kind: "restore", State: "cancelled", Phase: "finished",
			SourceState: lifecycle.State{DeploymentID: deployment, DatabaseSlot: lifecycle.DatabasePrimary, Master: lifecycle.MasterDefault, Digest: strings.Repeat("f", 64)},
			ActorID:     "capacity-administrator", CredentialID: "capacity-session", Authorized: true, CancelAuthorized: true,
			BackupID: id, Digest: strings.Repeat("c", 64), Size: 8192, CreatedAt: when, UpdatedAt: when,
			ErrorCode: "operation_cancelled", Source: summaryView(&summary), Manifest: manifest, GenerationID: id, TargetSlot: lifecycle.DatabaseRecovery,
			Target: &recoverydb.Retained{Marker: marker, RawMarker: recoverydb.RawMarker{Present: true, Value: raw}, Database: "capacity_target", Role: "capacity_owner", Facts: facts},
		})
	}
	return data
}

func budgetJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func budgetHistoryPrefix(history controlData, count int) controlData {
	history.Operations = append([]operationRecord(nil), history.Operations[:count]...)
	history.Slots = append([]slotRecord(nil), history.Slots...)
	return history
}

func budgetManager(t *testing.T, data controlData) *Manager {
	t.Helper()
	ctx := context.Background()
	store, err := recoverycontrol.Open(ctx, filepath.Join(t.TempDir(), "control"), data.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	snapshot, err := store.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.CompareAndSwap(ctx, snapshot.Digest, budgetJSON(t, data))
	if err != nil {
		t.Fatal(err)
	}
	current := lifecycle.State{DeploymentID: data.DeploymentID, DatabaseSlot: lifecycle.DatabasePrimary, Master: lifecycle.MasterDefault, Digest: strings.Repeat("f", 64)}
	return &Manager{runtime: &Runtime{control: store}, current: current, data: data, control: snapshot, ctx: ctx, jobs: map[string]*activeJob{}}
}

func TestControlCompactionPreservesReplayAndUnsettledProofs(t *testing.T) {
	data := budgetFixture(t, 56)
	before := budgetJSON(t, data)
	if len(before) <= recoverycontrol.MaxPayloadBytes {
		t.Fatal("fixture does not reproduce large restore history")
	}
	views := make([]OperationView, len(data.Operations))
	for i, op := range data.Operations {
		views[i] = operationView(op)
	}
	compactControl(&data)
	if len(budgetJSON(t, data)) >= recoverycontrol.MaxPayloadBytes/2 {
		t.Fatal("settled private proofs still consume the journal")
	}
	for i, op := range data.Operations {
		if op.Manifest != nil || op.Target != nil || !reflect.DeepEqual(views[i], operationView(op)) {
			t.Fatal("compaction changed replay or public history")
		}
	}
	m := budgetManager(t, data)
	op := data.Operations[0]
	if found, err := m.findRequestLocked(op.RequestID, op.Kind, op.Fingerprint, op.ActorID); err != nil || found == nil || found.ID != op.ID {
		t.Fatal("recent request identity was lost")
	}
	actor := identity.Principal{User: identity.User{ID: "capacity-administrator"}, SessionID: "capacity-session"}
	if _, err := m.newOperationLocked(actor, strings.Repeat("9", 32), "restore", strings.Repeat("a", 64)); err != nil {
		t.Fatalf("compacted history still blocks normal admission: %v", err)
	}

	unsettled := budgetFixture(t, 4)
	unsettled.Operations[0].State, unsettled.Operations[0].Phase = "interrupted", "publication"
	unsettled.Operations[1].State, unsettled.Operations[1].Phase = "interrupted", "cleanup"
	unsettled.Operations[2].State, unsettled.Operations[2].Phase = "ready", "ready"
	unsettled.Operations[3].State, unsettled.Operations[3].Phase = "applying", "activation"
	unsettled.Transition = &transitionRecord{OperationID: unsettled.Operations[3].ID, Before: unsettled.Operations[3].SourceState, Phase: "requested"}
	unsettled.Slots[1].Retained = unsettled.Operations[0].Target
	retained := budgetJSON(t, unsettled)
	compactControl(&unsettled)
	if !bytes.Equal(retained, budgetJSON(t, unsettled)) {
		t.Fatal("compaction changed unresolved operation or slot proof")
	}
}

func TestControlAdmissionUsesBytesWithoutConsumingRejectedRequest(t *testing.T) {
	actor := identity.Principal{User: identity.User{ID: "capacity-administrator"}, SessionID: "capacity-session"}
	history := budgetFixture(t, maxOperations-1)
	for count := 1; count < maxOperations; count++ {
		data := budgetHistoryPrefix(history, count)
		compactControl(&data)
		m := &Manager{data: data, current: data.Operations[0].SourceState}
		before := budgetJSON(t, m.data)
		request := strings.Repeat("9", 32)
		_, err := m.newOperationLocked(actor, request, "create", strings.Repeat("a", 64))
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrCapacity) || count >= maxOperations || len(before) >= recoverycontrol.MaxPayloadBytes || m.fault || !bytes.Equal(before, budgetJSON(t, m.data)) {
			t.Fatalf("byte refusal mutated the manager or used the count limit: count=%d err=%v", count, err)
		}
		if found, err := m.findRequestLocked(request, "create", strings.Repeat("a", 64), actor.User.ID); err != nil || found != nil {
			t.Fatal("rejected request ID was consumed")
		}
		return
	}
	t.Fatal("byte admission never protected the work reserve")
}

func TestControlPrunesOldHistoryWithoutDiscardingPendingReceiptsOrSlotOwners(t *testing.T) {
	data := budgetFixture(t, 5)
	for i := range data.Operations {
		data.Operations[i].CreatedAt = time.Now().UTC().Add(-9 * 24 * time.Hour)
		data.Operations[i].UpdatedAt = data.Operations[i].CreatedAt
	}
	data.Operations[0].Phase = "publication"
	data.Operations[1].Phase = "cleanup"
	data.Slots[1].Operation, data.Slots[1].Retained = data.Operations[2].ID, data.Operations[2].Target
	data.Operations[3].UpdatedAt = time.Now().UTC()
	removed := data.Operations[4].ID
	m := &Manager{data: data, current: data.Operations[0].SourceState}
	actor := identity.Principal{User: identity.User{ID: "capacity-administrator"}, SessionID: "capacity-session"}
	if _, err := m.newOperationLocked(actor, strings.Repeat("9", 32), "delete", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if len(m.data.Operations) != 5 || m.operationLocked(removed) != nil {
		t.Fatal("eligible history was not pruned below the count threshold")
	}
	for i := 0; i < 4; i++ {
		if m.operationLocked(data.Operations[i].ID) == nil {
			t.Fatal("pending receipt, slot owner, or recent request was pruned")
		}
	}
}

func TestControlUsesSerializedBytesAtTheExactBoundary(t *testing.T) {
	data := budgetFixture(t, 1)
	compactControl(&data)
	data.Operations[0].Source.ServerName = "Name <with> & escaped characters"
	encoded := budgetJSON(t, data)
	reserve := recoverycontrol.MaxPayloadBytes - len(encoded)
	if actual, err := encodeControl(data, reserve); err != nil || !bytes.Equal(actual, encoded) {
		t.Fatal("exact serialized byte boundary was rejected")
	}
	if _, err := encodeControl(data, reserve+1); !errors.Is(err, ErrCapacity) {
		t.Fatal("one-byte overflow was not rejected before CAS")
	}
}

func TestControlCapacityRestoresDurableMemoryAndAllowsTerminalCompletion(t *testing.T) {
	var data controlData
	history := budgetFixture(t, maxOperations-1)
	for count := 1; count < maxOperations; count++ {
		candidate := budgetHistoryPrefix(history, count)
		compactControl(&candidate)
		op := &candidate.Operations[len(candidate.Operations)-1]
		op.State, op.Phase, op.CancelAuthorized = "running", "staging", false
		size := len(budgetJSON(t, candidate))
		if size > recoverycontrol.MaxPayloadBytes-controlCompletionReserve {
			break
		}
		data = candidate
	}
	if len(data.Operations) == 0 {
		t.Fatal("missing near-capacity fixture")
	}
	m := budgetManager(t, data)
	before := m.control
	id := data.Operations[len(data.Operations)-1].ID
	proof := budgetFixture(t, 1).Operations[0]
	op := m.operationLocked(id)
	op.Manifest, op.Target = proof.Manifest, proof.Target
	if err := m.persistLocked(context.Background()); !errors.Is(err, ErrCapacity) || m.fault {
		t.Fatalf("known pre-CAS capacity rejection poisoned the manager: %v", err)
	}
	stored, err := m.runtime.control.Read(context.Background())
	if err != nil || stored.Digest != before.Digest || !bytes.Equal(stored.Payload, before.Payload) || !bytes.Equal(budgetJSON(t, m.data), before.Payload) {
		t.Fatal("capacity rejection changed durable state or retained uncommitted memory")
	}
	journalData, err := decodeControl(before.Payload, data.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	journalData.Operations[len(journalData.Operations)-1].Manifest = proof.Manifest
	journalData.Operations[len(journalData.Operations)-1].Target = proof.Target
	journal := &switchJournal{runtime: m.runtime, data: journalData, snapshot: before}
	if err := journal.save(context.Background()); !errors.Is(err, ErrCapacity) || journal.fault || !bytes.Equal(budgetJSON(t, journal.data), before.Payload) {
		t.Fatalf("switch journal did not restore a known pre-CAS refusal: %v", err)
	}
	if err := m.updateOperation(id, "failed", "finished", "capacity_exceeded", nil); err != nil || m.fault {
		t.Fatalf("admitted work cannot consume its terminal reserve: %v", err)
	}
	if op := m.operationLocked(id); op.State != "failed" || op.Target != nil || op.Manifest != nil || m.busyLocked("") {
		t.Fatal("capacity failure retained a running phantom operation")
	}
	// A genuine CAS conflict still makes the manager unavailable.
	if _, err := m.runtime.control.CompareAndSwap(context.Background(), m.control.Digest, budgetJSON(t, m.data)); err != nil {
		t.Fatal(err)
	}
	if err := m.persistLocked(context.Background()); !errors.Is(err, ErrUnavailable) || !m.fault {
		t.Fatal("a storage CAS conflict was misclassified as safe capacity pressure")
	}
}

func TestControlTransitionReservesAllSevenProofsWithoutChangingEvidence(t *testing.T) {
	var accepted controlData
	history := budgetFixture(t, maxOperations-1)
	for count := 1; count < maxOperations; count++ {
		data := budgetHistoryPrefix(history, count)
		proof := data.Operations[count-1].Target
		compactControl(&data)
		op := &data.Operations[count-1]
		op.State, op.Phase, op.CancelAuthorized, op.ApplyAuthorized, op.Target = "applying", "activation", false, true, proof
		data.Slots[0].Retained, data.Slots[1].Retained = proof, proof
		data.Transition = &transitionRecord{OperationID: op.ID, Phase: "rebinding", Before: op.SourceState, CanReturn: true,
			BeforeRetained: proof, TargetBefore: proof}
		before := budgetJSON(t, data)
		_, err := encodeControl(data, 0)
		if !bytes.Equal(before, budgetJSON(t, data)) {
			t.Fatal("capacity projection mutated real retained evidence")
		}
		if errors.Is(err, ErrCapacity) {
			if len(before) >= recoverycontrol.MaxPayloadBytes-controlCompletionReserve || len(accepted.Operations) == 0 {
				t.Fatal("transition budget did not account for future proof expansion")
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		accepted = data
	}
	if accepted.Transition == nil {
		t.Fatal("missing accepted transition fixture")
	}
	transition := accepted.Transition
	transition.TargetAfter = budgetRetained(transition.TargetBefore)
	transition.ReturnTarget = budgetRetained(transition.BeforeRetained)
	for _, phase := range []string{"prepared", "activated", "initializing", "returning", "return_planning", "return_prepared", "returned"} {
		transition.Phase = phase
		transition.After, transition.ReturnState = transition.Before, transition.Before
		transition.After.Revision, transition.ReturnState.Revision = math.MaxUint64, math.MaxUint64
		transition.After.GenerationID, transition.ReturnState.GenerationID = strings.Repeat("a", 32), strings.Repeat("b", 32)
		transition.PlanID, transition.ReturnPlanID = strings.Repeat("c", 32), strings.Repeat("d", 32)
		if _, err := encodeControl(accepted, 0); err != nil {
			t.Fatalf("accepted transition cannot retain future %s evidence: %v", phase, err)
		}
	}
}

func TestControlApplyRejectsExpansionBeforeChangingAuthority(t *testing.T) {
	history := budgetFixture(t, maxOperations-1)
	for count := 1; count < maxOperations; count++ {
		data := budgetHistoryPrefix(history, count)
		proof := data.Operations[count-1].Target
		compactControl(&data)
		op := &data.Operations[count-1]
		op.State, op.Phase, op.CancelAuthorized, op.Target = "ready", "ready", false, proof
		data.Slots[1].Retained, data.Slots[1].Operation = proof, op.ID
		if _, err := encodeControl(data, 0); err != nil {
			t.Fatal("ready fixture ran out of space before apply expansion")
		}
		m := &Manager{data: data, current: op.SourceState,
			control: recoverycontrol.Snapshot{Payload: budgetJSON(t, data)}}
		before := bytes.Clone(m.control.Payload)
		err := m.checkApplyCapacityLocked(op)
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrCapacity) || m.fault || !bytes.Equal(before, budgetJSON(t, m.data)) || m.data.Transition != nil || m.operationLocked(op.ID).ApplyAuthorized {
			t.Fatalf("apply expansion changed authority on a known capacity refusal: %v", err)
		}
		return
	}
	t.Fatal("apply expansion was never measured before authority")
}
