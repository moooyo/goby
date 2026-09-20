package library

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

const (
	MediaOperationRemoveSubtitle   = "remove_embedded_subtitle"
	MediaOperationOCR              = "subtitle_ocr"
	MaxMediaOperationCues          = 50000
	MaxMediaOperationTextBytes     = 8 << 20
	MaxMediaOperationImageBytes    = 64 << 20
	MaxMediaOperationDocumentBytes = 256 << 10
)

var (
	ErrMediaOperationConflict = errors.New("media operation revision or request conflicts")
	ErrMediaOperationState    = errors.New("media operation state does not allow this action")
	ErrMediaOperationRecovery = errors.New("media operation requires explicit recovery")
)

// MediaOperationSourceRevisionSQL is an invalidation stamp, never authority.
// All owned subtitle readers and operation admissions must use this expression.
// The surrounding item alias is i, matching the catalog projection helpers.
const MediaOperationSourceRevisionSQL = `('media-operation-source-v1-' || md5(jsonb_build_array(i.root_id,
	i.relative_path, i.file_identity, i.file_size, extract(epoch FROM i.modified_at), i.media,
	(SELECT ir.binding_revision FROM library_roots ir WHERE ir.id=i.root_id))::text))`

type MediaOperationParameters struct {
	ModelIDs          []string `json:"ModelIds,omitempty"`
	OutputFormat      string   `json:"OutputFormat,omitempty"`
	Profile           string   `json:"Profile,omitempty"`
	Language          string   `json:"Language,omitempty"`
	Title             string   `json:"Title,omitempty"`
	IsDefault         bool     `json:"IsDefault"`
	IsForced          bool     `json:"IsForced"`
	IsHearingImpaired bool     `json:"IsHearingImpaired"`
}

// MediaOperation retains safe public status separately from private execution
// witnesses. Paths, credential identifiers and executable configuration never
// enter a native operation response through ordinary JSON serialization.
type MediaOperation struct {
	ID                 string                   `json:"Id"`
	Kind               string                   `json:"Kind"`
	State              string                   `json:"State"`
	Revision           int64                    `json:"Revision,string"`
	ItemID             string                   `json:"ItemId"`
	LibraryID          string                   `json:"LibraryId"`
	RootID             string                   `json:"-"`
	MediaSourceID      string                   `json:"MediaSourceId"`
	SourceRevision     string                   `json:"SourceRevision"`
	StreamIndex        int                      `json:"StreamIndex"`
	Parameters         MediaOperationParameters `json:"Parameters"`
	Progress           MediaOperationProgress   `json:"Progress"`
	ResultSummary      json.RawMessage          `json:"ResultSummary"`
	ResultHash         string                   `json:"ResultHash"`
	PublicationPhase   string                   `json:"PublicationPhase"`
	CancelRequestedAt  *time.Time               `json:"CancelRequestedAt"`
	CreatedAt          time.Time                `json:"CreatedAt"`
	UpdatedAt          time.Time                `json:"UpdatedAt"`
	StartedAt          *time.Time               `json:"StartedAt"`
	FinishedAt         *time.Time               `json:"FinishedAt"`
	ErrorCode          string                   `json:"ErrorCode"`
	ErrorMessage       string                   `json:"ErrorMessage"`
	CanCancel          bool                     `json:"CanCancel"`
	CanReview          bool                     `json:"CanReview"`
	CanApply           bool                     `json:"CanApply"`
	CanRecover         bool                     `json:"CanRecover"`
	Applied            bool                     `json:"Applied"`
	TargetPresent      bool                     `json:"TargetPresent"`
	SourceSnapshot     json.RawMessage          `json:"-"`
	ExecutionSnapshot  json.RawMessage          `json:"-"`
	Journal            json.RawMessage          `json:"-"`
	WorkerToken        string                   `json:"-"`
	RequestActor       identity.Principal       `json:"-"`
	ApplyActor         identity.Principal       `json:"-"`
	RequestID          string                   `json:"-"`
	RequestFingerprint []byte                   `json:"-"`
	ApplyRequestID     string                   `json:"-"`
	ApplyFingerprint   []byte                   `json:"-"`
	ApplyRevision      *int64                   `json:"-"`
}

type MediaOperationProgress struct {
	Stage     string `json:"Stage"`
	Processed int64  `json:"Processed"`
	Total     int64  `json:"Total"`
}

type MediaOperationRequest struct {
	RequestID         string                   `json:"RequestId"`
	Kind              string                   `json:"Kind"`
	ItemID            string                   `json:"-"`
	MediaSourceID     string                   `json:"MediaSourceId"`
	SourceRevision    string                   `json:"SourceRevision"`
	StreamIndex       int                      `json:"StreamIndex"`
	Parameters        MediaOperationParameters `json:"Parameters"`
	ExecutionSnapshot json.RawMessage          `json:"-"`
	MaxQueued         int                      `json:"-"`
}

type MediaOperationAdmission struct {
	Operation MediaOperation `json:"Operation"`
	Admitted  bool           `json:"Admitted"`
}

type MediaOperationApplyRequest struct {
	Revision       int64  `json:"Revision,string"`
	SourceRevision string `json:"SourceRevision"`
	ResultHash     string `json:"ResultHash"`
	RequestID      string `json:"RequestId"`
}

// A claim is process-local work backed by a durable random fencing token.
// Executors must join every process and storage worker before returning.
type MediaOperationWork struct {
	Operation MediaOperation
	Token     string
	Apply     bool
	Discard   bool
}

type MediaOperationResult struct {
	Summary    json.RawMessage
	ResultHash string
	Cues       []MediaOperationCue
	Journal    json.RawMessage
}

type MediaOperationCue struct {
	Ordinal            int      `json:"Ordinal"`
	OriginalStartTicks int64    `json:"OriginalStartTicks,string"`
	OriginalEndTicks   int64    `json:"OriginalEndTicks,string"`
	OriginalText       string   `json:"OriginalText"`
	StartTicks         int64    `json:"StartTicks,string"`
	EndTicks           int64    `json:"EndTicks,string"`
	Text               string   `json:"Text"`
	Included           bool     `json:"Included"`
	Confidence         *float64 `json:"Confidence"`
	Warnings           []string `json:"Warnings"`
	ImageSHA256        string   `json:"ImageSHA256"`
	ImagePNG           []byte   `json:"-"`
	IsForced           bool     `json:"IsForced"`
	IsHearingImpaired  bool     `json:"IsHearingImpaired"`
}

type MediaOperationCueEdit struct {
	Ordinal    int    `json:"Ordinal"`
	StartTicks int64  `json:"StartTicks,string"`
	EndTicks   int64  `json:"EndTicks,string"`
	Text       string `json:"Text"`
	Included   bool   `json:"Included"`
}

type MediaOperationPageOptions struct {
	StartIndex int
	Limit      int
	ItemID     string
	Kind       string
	State      string
}

type MediaOperationPage struct {
	Items            []MediaOperation `json:"Items"`
	TotalRecordCount int64            `json:"TotalRecordCount"`
	StartIndex       int              `json:"StartIndex"`
	Limit            int              `json:"Limit"`
}

type MediaOperationCuePage struct {
	Operation        MediaOperation      `json:"Operation"`
	Items            []MediaOperationCue `json:"Items"`
	TotalRecordCount int64               `json:"TotalRecordCount"`
	StartIndex       int                 `json:"StartIndex"`
	Limit            int                 `json:"Limit"`
}

type MediaOperationTarget struct {
	ItemID         string         `json:"ItemId"`
	MediaSourceID  string         `json:"MediaSourceId"`
	SourceRevision string         `json:"SourceRevision"`
	Container      string         `json:"Container"`
	Streams        []media.Stream `json:"Streams"`
}

// MediaOperationApplyFunc runs bounded SQL and pure in-memory computation through
// the owned transaction.
// It must not start processes, wait for workers, perform filesystem operations,
// retain the transaction, or commit it. Catalog changes belong in this callback.
type MediaOperationApplyFunc func(context.Context, pgx.Tx, MediaOperation) error

func mediaOperationStateValid(state string) bool {
	switch state {
	case "queued", "running", "ready", "applying", "completed", "failed", "cancelled", "interrupted", "stale", "recovery_required":
		return true
	default:
		return false
	}
}

func projectMediaOperation(operation *MediaOperation) {
	operation.Applied = operation.PublicationPhase == "catalog_committed" || operation.PublicationPhase == "done" || operation.Kind == MediaOperationOCR && operation.State == "completed"
	operation.CanReview = operation.Kind == MediaOperationOCR && operation.State == "ready" && operation.CancelRequestedAt == nil
	operation.CanApply = operation.State == "ready" && operation.TargetPresent && operation.CancelRequestedAt == nil
	operation.CanCancel = (operation.State == "queued" || operation.State == "running" || operation.State == "ready" || operation.State == "applying") && operation.CancelRequestedAt == nil && !operation.Applied
	operation.CanRecover = operation.State == "recovery_required"
	if operation.Kind == MediaOperationOCR && operation.State == "interrupted" && operation.PublicationPhase == "none" && operation.WorkerToken == "" {
		operation.CanCancel = true
	}
	if operation.Kind == MediaOperationRemoveSubtitle && operation.PublicationPhase == "none" && operation.WorkerToken == "" &&
		(operation.State == "interrupted" || operation.State == "failed" || operation.State == "stale" || operation.State == "recovery_required") {
		operation.CanCancel = true
	}
}
