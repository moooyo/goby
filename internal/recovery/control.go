package recovery

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recoverycontrol"
	"github.com/moooyo/goby/internal/recoverydb"
)

const (
	maxOperations = 512
	// Nonterminal writes reserve fixed-size receipt, cancellation, and final
	// state changes. Settled writes may consume this space at the hard limit.
	controlCompletionReserve = 16 << 10
)

type controlData struct {
	Version      int               `json:"version"`
	DeploymentID string            `json:"deploymentId"`
	Operations   []operationRecord `json:"operations"`
	Slots        []slotRecord      `json:"slots"`
	Transition   *transitionRecord `json:"transition,omitempty"`
}

type operationRecord struct {
	ID                 string                 `json:"id"`
	RequestID          string                 `json:"requestId"`
	Fingerprint        string                 `json:"fingerprint"`
	Revision           uint64                 `json:"revision"`
	Kind               string                 `json:"kind"`
	State              string                 `json:"state"`
	Phase              string                 `json:"phase"`
	SourceState        lifecycle.State        `json:"sourceState"`
	ActorID            string                 `json:"actorId"`
	CredentialID       string                 `json:"credentialId"`
	Operator           bool                   `json:"operator"`
	Authorized         bool                   `json:"authorized"`
	BackupID           string                 `json:"backupId"`
	Digest             string                 `json:"digest"`
	Size               int64                  `json:"size"`
	CreatedAt          time.Time              `json:"createdAt"`
	UpdatedAt          time.Time              `json:"updatedAt"`
	ErrorCode          string                 `json:"errorCode"`
	Source             *SourceView            `json:"source,omitempty"`
	Manifest           *backupformat.Manifest `json:"manifest,omitempty"`
	RestoreDefaults    bool                   `json:"restoreDefaults"`
	ReplaceRollback    bool                   `json:"replaceRollback"`
	GenerationID       string                 `json:"generationId"`
	TargetSlot         lifecycle.DatabaseSlot `json:"targetSlot"`
	Target             *recoverydb.Retained   `json:"target,omitempty"`
	TargetHost         *hostSettingsCapture   `json:"targetHost,omitempty"`
	ApplyAuthorized    bool                   `json:"applyAuthorized"`
	CancelAuthorized   bool                   `json:"cancelAuthorized"`
	ActivationAccepted bool                   `json:"activationAccepted"`
	FailureGeneration  string                 `json:"failureGeneration,omitempty"`
}

// ImageID names immutable local configuration/key files. It can differ from
// Retained.Marker.GenerationID for the original, pre-generation deployment.
// The marker is rebound only when that inactive image becomes a new candidate.
type slotRecord struct {
	Slot      lifecycle.DatabaseSlot `json:"slot"`
	State     string                 `json:"state"`
	ImageID   string                 `json:"imageId"`
	Name      string                 `json:"name"`
	Captured  time.Time              `json:"captured"`
	Retained  *recoverydb.Retained   `json:"retained,omitempty"`
	Operation string                 `json:"operation"`
}

type transitionRecord struct {
	OperationID    string                 `json:"operationId"`
	Phase          string                 `json:"phase"`
	Before         lifecycle.State        `json:"before"`
	After          lifecycle.State        `json:"after"`
	PlanID         string                 `json:"planId"`
	BeforeImage    string                 `json:"beforeImage"`
	BeforeMaster   lifecycle.MasterSource `json:"beforeMaster,omitempty"`
	BeforeRetained *recoverydb.Retained   `json:"beforeRetained,omitempty"`
	CanReturn      bool                   `json:"canReturn"`
	TargetMaster   lifecycle.MasterSource `json:"targetMaster,omitempty"`
	TargetBefore   *recoverydb.Retained   `json:"targetBefore,omitempty"`
	TargetAfter    *recoverydb.Retained   `json:"targetAfter,omitempty"`
	ReturnTarget   *recoverydb.Retained   `json:"returnTarget,omitempty"`
	ReturnPlanID   string                 `json:"returnPlanId,omitempty"`
	ReturnState    lifecycle.State        `json:"returnState"`
}

func readControl(ctx context.Context, runtime *Runtime) (controlData, recoverycontrol.Snapshot, error) {
	snapshot, err := runtime.control.Read(ctx)
	if err != nil {
		return controlData{}, snapshot, err
	}
	current, err := runtime.lifecycle.Current()
	if err != nil {
		return controlData{}, snapshot, err
	}
	if len(snapshot.Payload) == 0 {
		return controlData{Version: 1, DeploymentID: current.DeploymentID,
			Operations: []operationRecord{}, Slots: []slotRecord{
				{Slot: lifecycle.DatabasePrimary, State: "active"},
				{Slot: lifecycle.DatabaseRecovery, State: "unclaimed"},
			}}, snapshot, nil
	}
	data, err := decodeControl(snapshot.Payload, current.DeploymentID)
	if err != nil {
		return controlData{}, snapshot, ErrUnavailable
	}
	return data, snapshot, nil
}

func decodeControl(payload []byte, deployment string) (controlData, error) {
	var data controlData
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&data) != nil || decoder.Decode(new(any)) != io.EOF || validateControl(data, deployment) != nil {
		return controlData{}, ErrUnavailable
	}
	return data, nil
}

func writeControl(ctx context.Context, runtime *Runtime, expected recoverycontrol.Snapshot, data controlData) (recoverycontrol.Snapshot, error) {
	payload, err := encodeControl(data, 0)
	if err != nil {
		return recoverycontrol.Snapshot{}, err
	}
	return runtime.control.CompareAndSwap(ctx, expected.Digest, payload)
}

// Capacity errors occur before CAS and have a certain, unchanged outcome.
// Unlike filesystem publication failures, they do not poison the store.
func encodeControl(data controlData, extraReserve int) ([]byte, error) {
	if validateControl(data, data.DeploymentID) != nil {
		return nil, ErrInvalid
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, ErrInvalid
	}
	reserve := extraReserve
	for _, op := range data.Operations {
		if !terminalOperation(op.State) {
			reserve += controlCompletionReserve
			break
		}
	}
	if len(payload) > recoverycontrol.MaxPayloadBytes-reserve {
		return nil, ErrCapacity
	}
	projected := data
	projected.Operations = slices.Clone(data.Operations)
	for index := range projected.Operations {
		if !terminalOperation(projected.Operations[index].State) {
			budgetOperationScalars(&projected.Operations[index])
		}
	}
	maximum, err := json.Marshal(projected)
	if err != nil {
		return nil, ErrInvalid
	}
	if len(maximum) > recoverycontrol.MaxPayloadBytes-reserve {
		return nil, ErrCapacity
	}
	if err := checkTransitionCapacity(data, reserve); err != nil {
		return nil, err
	}
	return payload, nil
}

func budgetOperationScalars(op *operationRecord) {
	op.Revision, op.Size = math.MaxUint64, math.MaxInt64
	op.State, op.Phase, op.ErrorCode = "interrupted", "publication", "recovery_database_not_configured"
	op.BackupID, op.GenerationID = strings.Repeat("f", 32), strings.Repeat("f", 32)
	op.Digest, op.FailureGeneration = strings.Repeat("f", 64), strings.Repeat("f", 64)
	op.TargetSlot = lifecycle.DatabaseRecovery
	op.Authorized, op.CancelAuthorized, op.ApplyAuthorized, op.ActivationAccepted = false, false, false, false
	op.RestoreDefaults, op.ReplaceRollback = false, false
	op.UpdatedAt = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
}

func settledOperation(data controlData, op operationRecord) bool {
	return terminalOperation(op.State) && op.Phase != "publication" && op.Phase != "cleanup" &&
		(data.Transition == nil || data.Transition.OperationID != op.ID)
}

// Public history and recent request IDs remain intact. Only proofs that no
// longer participate in recovery are removed; slot proofs remain independent.
func compactControl(data *controlData) {
	for index := range data.Operations {
		op := &data.Operations[index]
		if settledOperation(*data, *op) {
			op.Manifest, op.Target = nil, nil
			op.TargetHost = nil
		}
	}
}

func restoreControl(expected recoverycontrol.Snapshot, deployment string) (controlData, error) {
	if len(expected.Payload) == 0 {
		return controlData{Version: 1, DeploymentID: deployment, Operations: []operationRecord{},
			Slots: []slotRecord{{Slot: lifecycle.DatabasePrimary, State: "active"}, {Slot: lifecycle.DatabaseRecovery, State: "unclaimed"}}}, nil
	}
	return decodeControl(expected.Payload, deployment)
}

// Reserve the full return path before publishing the first captured source
// proof. Retained values can occur seven times in JSON even when pointers are
// shared. Actual later proofs retain this same schema/field shape; the fixed
// completion reserve covers canonical marker, revision, and receipt growth.
func checkTransitionCapacity(data controlData, reserve int) error {
	if data.Transition == nil {
		return nil
	}
	transition := *data.Transition
	data.Transition = &transition
	data.Operations = slices.Clone(data.Operations)
	data.Slots = slices.Clone(data.Slots)
	var target *recoverydb.Retained
	var targetSlot lifecycle.DatabaseSlot
	for index := range data.Operations {
		op := &data.Operations[index]
		if op.ID == transition.OperationID {
			target, targetSlot = op.Target, op.TargetSlot
			if transition.TargetAfter != nil {
				target = transition.TargetAfter
			}
			target = budgetRetained(target)
			op.Target = target
			budgetOperationScalars(op)
			break
		}
	}
	if transition.TargetBefore == nil {
		transition.TargetBefore = target
	}
	transition.TargetAfter = target
	source := budgetRetained(transition.BeforeRetained)
	if transition.CanReturn {
		transition.BeforeRetained, transition.ReturnTarget = source, source
	}
	for index := range data.Slots {
		slot := &data.Slots[index]
		if slot.Slot == targetSlot {
			slot.Retained = target
		} else if transition.CanReturn && source != nil {
			slot.Retained = source
			if len(slot.Name) < len("Retired generation") {
				slot.Name = "Retired generation"
			}
		}
		slot.State, slot.ImageID, slot.Operation = "unclaimed", strings.Repeat("f", 32), strings.Repeat("f", 32)
		slot.Captured = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	}
	transition.PlanID, transition.BeforeImage, transition.ReturnPlanID = strings.Repeat("f", 32), strings.Repeat("f", 32), strings.Repeat("f", 32)
	transition.Phase = "return_prepared"
	transition.BeforeMaster, transition.TargetMaster = lifecycle.MasterGeneration, lifecycle.MasterGeneration
	future := transition.Before
	future.Revision, future.GenerationID, future.Digest = math.MaxUint64, strings.Repeat("f", 32), strings.Repeat("f", 64)
	future.Master, future.DatabaseSlot = lifecycle.MasterGeneration, lifecycle.DatabaseRecovery
	transition.After, transition.ReturnState = future, future
	projected, err := json.Marshal(data)
	if err != nil {
		return ErrInvalid
	}
	if len(projected) > recoverycontrol.MaxPayloadBytes-reserve {
		return ErrCapacity
	}
	return nil
}

// This synthetic upper bound is used only for byte accounting. It is never
// validated as data, saved in control, or supplied to a database operation.
func budgetRetained(actual *recoverydb.Retained) *recoverydb.Retained {
	if actual == nil {
		return nil
	}
	result := *actual
	result.Facts.Tables = slices.Clone(actual.Facts.Tables)
	for index := range result.Facts.Tables {
		result.Facts.Tables[index].Rows = math.MaxInt64
	}
	result.Marker.GenerationID = strings.Repeat("f", 32)
	if encoded, err := recoverydb.EncodeMarker(result.Marker); err == nil {
		canonicalJSON, _ := json.Marshal(encoded)
		originalJSON, _ := json.Marshal(result.RawMarker.Value)
		if len(canonicalJSON) > len(originalJSON) {
			result.RawMarker = recoverydb.RawMarker{Present: true, Value: encoded}
		}
	}
	return &result
}

func recoverCapacity(data *controlData, expected recoverycontrol.Snapshot, err error) error {
	if !errors.Is(err, ErrCapacity) {
		return err
	}
	previous, restoreErr := restoreControl(expected, data.DeploymentID)
	if restoreErr != nil {
		return restoreErr
	}
	*data = previous
	return ErrCapacity
}

func validateControl(data controlData, deployment string) error {
	if data.Version != 1 || data.DeploymentID != deployment || !hexID(deployment, 32) ||
		len(data.Operations) > maxOperations || len(data.Slots) != 2 {
		return ErrInvalid
	}
	seen, requests := map[string]bool{}, map[string]bool{}
	for _, op := range data.Operations {
		if !hexID(op.ID, 32) || !hexID(op.RequestID, 32) || !hexID(op.Fingerprint, 64) ||
			seen[op.ID] || requests[op.RequestID] || op.Revision == 0 || op.Size < 0 ||
			op.SourceState.DeploymentID != deployment || !validOperationKind(op.Kind) ||
			!validOperationState(op.State) || !validOperationPhase(op.Phase) ||
			op.CreatedAt.IsZero() || op.UpdatedAt.Before(op.CreatedAt) ||
			op.BackupID != "" && !hexID(op.BackupID, 32) || op.Digest != "" && !hexID(op.Digest, 64) ||
			op.GenerationID != "" && !hexID(op.GenerationID, 32) ||
			op.FailureGeneration != "" && !hexID(op.FailureGeneration, 64) ||
			!validOperationError(op.ErrorCode) || op.Operator && (op.ActorID != "" || op.CredentialID != "") ||
			!op.Operator && (op.ActorID == "" || op.CredentialID == "") {
			return ErrInvalid
		}
		if op.TargetHost != nil && ((op.Kind != "restore" && op.Kind != "rollback") || !op.TargetHost.valid(op.Operator)) {
			return ErrInvalid
		}
		seen[op.ID], requests[op.RequestID] = true, true
	}
	if data.Slots[0].Slot != lifecycle.DatabasePrimary || data.Slots[1].Slot != lifecycle.DatabaseRecovery {
		return ErrInvalid
	}
	for _, slot := range data.Slots {
		if !slices.Contains([]string{"unclaimed", "empty", "active", "staging", "staged", "retained", "failed"}, slot.State) ||
			slot.ImageID != "" && !hexID(slot.ImageID, 32) || slot.Operation != "" && !hexID(slot.Operation, 32) {
			return ErrInvalid
		}
	}
	if transition := data.Transition; transition != nil {
		if !seen[transition.OperationID] || transition.Before.DeploymentID != deployment ||
			!slices.Contains([]string{"requested", "retiring", "rebinding", "planning", "prepared", "aborting", "activated", "initializing", "accepting", "accepted", "returning", "return_planning", "return_prepared", "returned"}, transition.Phase) ||
			transition.PlanID != "" && !hexID(transition.PlanID, 32) ||
			transition.BeforeImage != "" && !hexID(transition.BeforeImage, 32) ||
			transition.ReturnPlanID != "" && !hexID(transition.ReturnPlanID, 32) ||
			transition.BeforeMaster != "" && transition.BeforeMaster != lifecycle.MasterDefault && transition.BeforeMaster != lifecycle.MasterGeneration ||
			transition.TargetMaster != "" && transition.TargetMaster != lifecycle.MasterDefault && transition.TargetMaster != lifecycle.MasterGeneration {
			return ErrInvalid
		}
	}
	return nil
}

func validOperationKind(value string) bool {
	return slices.Contains([]string{"create", "import", "delete", "restore", "rollback"}, value)
}

func validOperationState(value string) bool {
	return slices.Contains([]string{"pending", "running", "ready", "applying", "completed", "failed", "cancelled", "interrupted"}, value)
}

func validOperationPhase(value string) bool {
	return slices.Contains([]string{"admission", "upload", "snapshot", "encryption", "publication", "validation", "staging", "ready", "activation", "rollback", "cleanup", "finished"}, value)
}

func validOperationError(value string) bool {
	return slices.Contains([]string{"", "storage_unavailable", "tools_unavailable", "database_unavailable", "recovery_database_not_configured",
		"recovery_required", "invalid_archive", "capacity_exceeded", "target_not_ready", "source_changed", "authority_changed",
		"audit_unavailable", "operation_cancelled", "operation_interrupted", "activation_failed"}, value)
}

func terminalOperation(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled" || state == "interrupted"
}

func hexID(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func randomOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", ErrUnavailable
	}
	return hex.EncodeToString(value[:]), nil
}

func requestFingerprint(value any) string {
	// Callers supply only non-secret typed arguments, never a passphrase.
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
