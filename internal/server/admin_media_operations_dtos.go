package server

import (
	"encoding/json"
	"time"

	"github.com/moooyo/goby/internal/library"
)

// Runtime admission supplies this inventory. Configuration alone never proves
// that an executable, model, publication directory, or worker is usable.
type adminMediaOperationCapabilities struct {
	Enabled           bool                      `json:"Enabled"`
	Available         bool                      `json:"Available"`
	UnavailableReason string                    `json:"UnavailableReason"`
	MaxConcurrent     int                       `json:"MaxConcurrent"`
	MaxQueued         int                       `json:"MaxQueued"`
	WritableProfiles  []string                  `json:"WritableProfiles"`
	OCR               adminMediaOCRCapabilities `json:"OCR"`
}

type adminMediaOCRCapabilities struct {
	Available         bool                 `json:"Available"`
	UnavailableReason string               `json:"UnavailableReason"`
	Models            []adminMediaOCRModel `json:"Models"`
	OutputFormats     []string             `json:"OutputFormats"`
}

type adminMediaOCRModel struct {
	ID       string `json:"Id"`
	Language string `json:"Language"`
}

type adminMediaProcessingTarget struct {
	ItemID         string                          `json:"ItemId"`
	MediaSourceID  string                          `json:"MediaSourceId"`
	SourceRevision string                          `json:"SourceRevision"`
	Container      string                          `json:"Container"`
	Streams        []adminMediaProcessingStream    `json:"Streams"`
	Capabilities   adminMediaOperationCapabilities `json:"Capabilities"`
}

// Deliberately omit Stream.Filename, subtitle paths, decoder timing witnesses,
// root identities and source snapshots from this administrator projection.
type adminMediaProcessingStream struct {
	Index                int    `json:"Index"`
	Codec                string `json:"Codec"`
	CodecType            string `json:"CodecType"`
	Language             string `json:"Language"`
	Title                string `json:"Title"`
	IsDefault            bool   `json:"IsDefault"`
	IsForced             bool   `json:"IsForced"`
	IsHearingImpaired    bool   `json:"IsHearingImpaired"`
	IsExternal           bool   `json:"IsExternal"`
	IsTextSubtitleStream bool   `json:"IsTextSubtitleStream"`
}

// This is the only result document serialized by the native HTTP adapter.
// Private executor journals and arbitrary result keys never cross the wire.
type adminMediaOperationSummary struct {
	ContainerProfile     string                   `json:"ContainerProfile,omitempty"`
	RemovedStreamIndex   *int                     `json:"RemovedStreamIndex,omitempty"`
	PreservedStreamCount *int                     `json:"PreservedStreamCount,omitempty"`
	OriginalBytes        *int64                   `json:"OriginalBytes,string,omitempty"`
	CandidateBytes       *int64                   `json:"CandidateBytes,string,omitempty"`
	BackupRetained       bool                     `json:"BackupRetained"`
	EngineSHA256         string                   `json:"EngineSHA256,omitempty"`
	ModelID              string                   `json:"ModelID,omitempty"`
	ModelSHA256          string                   `json:"ModelSHA256,omitempty"`
	Models               []adminMediaOCRModelHash `json:"Models"`
	CueCount             *int                     `json:"CueCount,omitempty"`
	WarningCount         *int                     `json:"WarningCount,omitempty"`
	AppliedStreamIndex   *int                     `json:"AppliedStreamIndex,omitempty"`
	AppliedContentSHA256 string                   `json:"AppliedContentSHA256,omitempty"`
	AppliedFormat        string                   `json:"AppliedFormat,omitempty"`
	Warnings             []string                 `json:"Warnings"`
}

type adminMediaOCRModelHash struct {
	ID     string `json:"ID"`
	SHA256 string `json:"SHA256"`
}

type adminMediaOperationDTO struct {
	ID                string                           `json:"Id"`
	Kind              string                           `json:"Kind"`
	State             string                           `json:"State"`
	Revision          int64                            `json:"Revision,string"`
	ItemID            string                           `json:"ItemId"`
	LibraryID         string                           `json:"LibraryId"`
	MediaSourceID     string                           `json:"MediaSourceId"`
	SourceRevision    string                           `json:"SourceRevision"`
	StreamIndex       int                              `json:"StreamIndex"`
	Parameters        library.MediaOperationParameters `json:"Parameters"`
	Progress          library.MediaOperationProgress   `json:"Progress"`
	ResultSummary     adminMediaOperationSummary       `json:"ResultSummary"`
	ResultHash        string                           `json:"ResultHash"`
	PublicationPhase  string                           `json:"PublicationPhase"`
	CancelRequestedAt *time.Time                       `json:"CancelRequestedAt"`
	CreatedAt         time.Time                        `json:"CreatedAt"`
	UpdatedAt         time.Time                        `json:"UpdatedAt"`
	StartedAt         *time.Time                       `json:"StartedAt"`
	FinishedAt        *time.Time                       `json:"FinishedAt"`
	ErrorCode         string                           `json:"ErrorCode"`
	ErrorMessage      string                           `json:"ErrorMessage"`
	CanCancel         bool                             `json:"CanCancel"`
	CanReview         bool                             `json:"CanReview"`
	CanApply          bool                             `json:"CanApply"`
	CanRecover        bool                             `json:"CanRecover"`
	Applied           bool                             `json:"Applied"`
	TargetPresent     bool                             `json:"TargetPresent"`
}

func mediaOperationDTO(operation library.MediaOperation) adminMediaOperationDTO {
	summary := adminMediaOperationSummary{Warnings: []string{}, Models: []adminMediaOCRModelHash{}}
	// Malformed stored evidence is not echoed. The result hash remains the
	// authority for an explicit apply request; this projection is explanatory.
	if len(operation.ResultSummary) <= library.MaxMediaOperationDocumentBytes {
		switch operation.Kind {
		case library.MediaOperationRemoveSubtitle:
			var candidate struct {
				ContainerProfile                         string
				RemovedStreamIndex, PreservedStreamCount *int
				OriginalBytes, CandidateBytes            *int64
				BackupRetained                           bool
			}
			if json.Unmarshal(operation.ResultSummary, &candidate) == nil {
				summary.ContainerProfile, summary.RemovedStreamIndex = candidate.ContainerProfile, candidate.RemovedStreamIndex
				summary.PreservedStreamCount, summary.OriginalBytes, summary.CandidateBytes = candidate.PreservedStreamCount, candidate.OriginalBytes, candidate.CandidateBytes
				summary.BackupRetained = candidate.BackupRetained
			}
		case library.MediaOperationOCR:
			var candidate struct {
				EngineSHA256, ModelID, ModelSHA256         string
				Models                                     []adminMediaOCRModelHash
				CueCount, WarningCount, AppliedStreamIndex *int
				AppliedContentSHA256, AppliedFormat        string
				Warnings                                   []string
			}
			if json.Unmarshal(operation.ResultSummary, &candidate) == nil {
				summary.EngineSHA256, summary.ModelID, summary.ModelSHA256 = candidate.EngineSHA256, candidate.ModelID, candidate.ModelSHA256
				summary.Models, summary.CueCount, summary.Warnings = candidate.Models, candidate.CueCount, candidate.Warnings
				summary.WarningCount = candidate.WarningCount
				summary.AppliedStreamIndex, summary.AppliedContentSHA256, summary.AppliedFormat = candidate.AppliedStreamIndex, candidate.AppliedContentSHA256, candidate.AppliedFormat
			}
		}
	}
	if summary.Warnings == nil {
		summary.Warnings = []string{}
	}
	if summary.Models == nil {
		summary.Models = []adminMediaOCRModelHash{}
	}
	return adminMediaOperationDTO{
		ID: operation.ID, Kind: operation.Kind, State: operation.State, Revision: operation.Revision,
		ItemID: operation.ItemID, LibraryID: operation.LibraryID, MediaSourceID: operation.MediaSourceID,
		SourceRevision: operation.SourceRevision, StreamIndex: operation.StreamIndex, Parameters: operation.Parameters,
		Progress: operation.Progress, ResultSummary: summary, ResultHash: operation.ResultHash,
		PublicationPhase: operation.PublicationPhase, CancelRequestedAt: operation.CancelRequestedAt,
		CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt, StartedAt: operation.StartedAt,
		FinishedAt: operation.FinishedAt, ErrorCode: operation.ErrorCode, ErrorMessage: operation.ErrorMessage,
		CanCancel: operation.CanCancel, CanReview: operation.CanReview, CanApply: operation.CanApply,
		CanRecover: operation.CanRecover, Applied: operation.Applied, TargetPresent: operation.TargetPresent,
	}
}

func mediaProcessingTargetDTO(target library.MediaOperationTarget, capabilities adminMediaOperationCapabilities) adminMediaProcessingTarget {
	result := adminMediaProcessingTarget{ItemID: target.ItemID, MediaSourceID: target.MediaSourceID,
		SourceRevision: target.SourceRevision, Container: target.Container,
		Streams: []adminMediaProcessingStream{}, Capabilities: capabilities}
	for _, stream := range target.Streams {
		result.Streams = append(result.Streams, adminMediaProcessingStream{
			Index: stream.Index, Codec: stream.Codec, CodecType: stream.CodecType,
			Language: stream.Language, Title: stream.Title, IsDefault: stream.IsDefault,
			IsForced: stream.IsForced, IsHearingImpaired: stream.IsHearingImpaired,
			IsExternal: stream.IsExternal, IsTextSubtitleStream: stream.IsTextSubtitleStream,
		})
	}
	return result
}
