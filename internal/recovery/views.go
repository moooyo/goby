package recovery

import (
	"context"
	"slices"
	"strconv"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
)

func (m *Manager) Status(ctx context.Context, actor identity.Principal) (StatusView, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return StatusView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	storage := m.runtime.backups.Status()
	cfg := m.cfg.Recovery.WithDefaults()
	view := StatusView{
		Available:          !m.closed && !m.fault && storage.Healthy && m.engine != nil,
		RestoreAvailable:   cfg.DatabaseURL != "" && m.engine != nil && !m.fault && !m.closed,
		GenerationRevision: decimal(m.current.Revision),
		Limits: LimitsView{MaxBackupBytes: strconv.FormatInt(cfg.Backups.MaxObjectBytes, 10),
			MaxStoredBytes: strconv.FormatInt(cfg.Backups.MaxTotalBytes, 10), MaxBackups: cfg.Backups.MaxObjects,
			MinPassphraseBytes: MinPassphraseBytes, MaxPassphraseBytes: 1024},
		Storage: StorageView{Bytes: strconv.FormatInt(storage.Bytes, 10), Objects: storage.Objects},
	}
	switch {
	case m.fault || m.closed:
		view.UnavailableReason = "recovery_required"
	case !storage.Healthy:
		view.UnavailableReason = "storage_unavailable"
	case m.engine == nil:
		view.UnavailableReason = "tools_unavailable"
	}
	if cfg.DatabaseURL == "" {
		view.RestoreUnavailableReason = "recovery_database_not_configured"
	} else if !view.Available {
		view.RestoreUnavailableReason = view.UnavailableReason
	}
	view.RestoreAvailable = view.Available && cfg.DatabaseURL != ""
	for _, op := range m.data.Operations {
		if !terminalOperation(op.State) {
			view.Busy, view.ActiveOperationId = true, op.ID
			break
		}
	}
	for _, slot := range m.data.Slots {
		if slot.Slot == m.current.DatabaseSlot {
			continue
		}
		view.Rollback.MustReplace = slot.State == "retained" || slot.State == "failed" || slot.State == "staged"
		if slot.State == "retained" && slot.Retained != nil && slot.ImageID != "" {
			when := slot.Captured
			view.Rollback.Available, view.Rollback.CreatedAt = true, &when
			view.Rollback.ServerName, view.Rollback.Generation = slot.Name, slot.ImageID
		}
	}
	return view, nil
}

func (m *Manager) ListBackups(ctx context.Context, actor identity.Principal, start, limit int) (BackupPage, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return BackupPage{}, err
	}
	if start < 0 || limit < 1 || limit > 100 {
		return BackupPage{}, ErrInvalid
	}
	page, err := m.runtime.backups.List(ctx, start, limit)
	if err != nil {
		return BackupPage{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := BackupPage{Items: make([]BackupView, len(page.Items)), TotalRecordCount: page.TotalRecordCount, StartIndex: start, Limit: limit}
	for index, item := range page.Items {
		result.Items[index] = m.backupViewLocked(item)
	}
	return result, nil
}

func (m *Manager) Backup(ctx context.Context, actor identity.Principal, id string) (BackupView, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return BackupView{}, err
	}
	metadata, err := m.runtime.backups.Get(ctx, id)
	if err != nil {
		return BackupView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backupViewLocked(metadata), nil
}

func (m *Manager) backupViewLocked(metadata backupstore.Metadata) BackupView {
	view := BackupView{Id: metadata.ID, Kind: string(metadata.Kind), State: string(metadata.State),
		CreatedAt: metadata.CreatedAt, UpdatedAt: metadata.UpdatedAt, SizeBytes: strconv.FormatInt(metadata.Size, 10),
		SHA256: metadata.Digest, Verified: metadata.Verified, Source: summaryView(metadata.Summary)}
	switch metadata.ErrorCode {
	case backupstore.CodeNone:
	case backupstore.CodeCancelled:
		view.ErrorCode = "operation_cancelled"
	case backupstore.CodeInterrupted:
		view.ErrorCode = "operation_interrupted"
	case backupstore.CodeQuota:
		view.ErrorCode = "capacity_exceeded"
	case backupstore.CodeIntegrity, backupstore.CodeImport, backupstore.CodeVerification:
		view.ErrorCode = "invalid_archive"
	case backupstore.CodeStorage:
		view.ErrorCode = "storage_unavailable"
	default:
		view.ErrorCode = "database_unavailable"
	}
	for _, op := range m.data.Operations {
		if op.BackupID == metadata.ID {
			if op.Kind == "delete" && !terminalOperation(op.State) {
				view.State = "deleting"
			}
			if view.Source != nil && op.Source != nil && op.Source.ServerName != "" {
				view.Source.ServerName = op.Source.ServerName
			}
		}
	}
	return view
}

func summaryView(summary *backupstore.SourceSummary) *SourceView {
	if summary == nil {
		return nil
	}
	view := &SourceView{ServerId: summary.ServerID, GobyVersion: summary.ApplicationVersion,
		SchemaVersion: strconv.Itoa(summary.SchemaVersion), CreatedAt: summary.CreatedAt,
		Tables: make([]TableView, len(summary.Tables))}
	for index, table := range summary.Tables {
		view.Tables[index] = TableView{Name: table.Name, Rows: strconv.FormatInt(table.Rows, 10)}
	}
	return view
}

func (m *Manager) ListOperations(ctx context.Context, actor identity.Principal, start, limit int) (OperationPage, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return OperationPage{}, err
	}
	if start < 0 || limit < 1 || limit > 100 {
		return OperationPage{}, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	items := slices.Clone(m.data.Operations)
	slices.SortFunc(items, func(first, second operationRecord) int {
		if compare := second.CreatedAt.Compare(first.CreatedAt); compare != 0 {
			return compare
		}
		if first.ID < second.ID {
			return -1
		}
		if first.ID > second.ID {
			return 1
		}
		return 0
	})
	result := OperationPage{Items: []OperationView{}, TotalRecordCount: len(items), StartIndex: start, Limit: limit}
	if start >= len(items) {
		return result, nil
	}
	for _, item := range items[start:min(start+limit, len(items))] {
		result.Items = append(result.Items, operationView(item))
	}
	return result, nil
}

func (m *Manager) Operation(ctx context.Context, actor identity.Principal, id string) (OperationView, error) {
	if err := m.authorize(ctx, actor); err != nil {
		return OperationView{}, err
	}
	op, err := m.operationCopy(id)
	if err != nil {
		return OperationView{}, err
	}
	return operationView(op), nil
}

func operationView(op operationRecord) OperationView {
	view := OperationView{Id: op.ID, RequestId: op.RequestID, Revision: decimal(op.Revision), Kind: op.Kind,
		State: op.State, Phase: op.Phase, BackupId: op.BackupID, CreatedAt: op.CreatedAt,
		UpdatedAt: op.UpdatedAt, ErrorCode: op.ErrorCode, RestoreDefaults: op.RestoreDefaults,
		ReplaceRollback: op.ReplaceRollback, GenerationRevision: decimal(op.SourceState.Revision)}
	if op.Source != nil {
		copy := *op.Source
		copy.Tables = append([]TableView{}, copy.Tables...)
		view.Source = &copy
	}
	view.CanCancel = op.Authorized && !op.CancelAuthorized && !op.ApplyAuthorized && op.Phase != "publication" &&
		(op.Kind == "create" || op.Kind == "import" || op.Kind == "restore") &&
		(op.State == "pending" || op.State == "running" || op.State == "ready")
	view.CanApply = op.Kind == "restore" && op.State == "ready" && !op.ApplyAuthorized && !op.CancelAuthorized
	return view
}
