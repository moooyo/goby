package recovery

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"time"

	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recoverycontrol"
	"github.com/moooyo/goby/internal/recoverydb"
)

const maxOperations = 512

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
	var data controlData
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&data) != nil || decoder.Decode(new(any)) != io.EOF || validateControl(data, current.DeploymentID) != nil {
		return controlData{}, snapshot, ErrUnavailable
	}
	return data, snapshot, nil
}

func writeControl(ctx context.Context, runtime *Runtime, expected recoverycontrol.Snapshot, data controlData) (recoverycontrol.Snapshot, error) {
	if validateControl(data, data.DeploymentID) != nil {
		return recoverycontrol.Snapshot{}, ErrInvalid
	}
	payload, err := json.Marshal(data)
	if err != nil || len(payload) > recoverycontrol.MaxPayloadBytes {
		return recoverycontrol.Snapshot{}, ErrUnavailable
	}
	return runtime.control.CompareAndSwap(ctx, expected.Digest, payload)
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
