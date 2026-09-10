import { ApiError, authenticatedRequest } from "./api";
import type { RequestOptions } from "./api";

export type BackupKind = "generated" | "imported";
export type BackupState = "writing" | "ready" | "failed" | "cancelled" | "interrupted" | "deleting";
export type OperationKind = "create" | "import" | "delete" | "restore" | "rollback";
export type OperationState = "pending" | "running" | "ready" | "applying" | "completed" | "failed" | "cancelled" | "interrupted";
export type OperationPhase = "admission" | "upload" | "snapshot" | "encryption" | "publication" | "validation" | "staging" | "ready" | "activation" | "rollback" | "cleanup" | "finished";
export type BackupUnavailableReason = "" | "storage_unavailable" | "tools_unavailable" | "database_unavailable" | "recovery_database_not_configured" | "recovery_required";
export type BackupErrorCode = BackupUnavailableReason | "invalid_archive" | "capacity_exceeded" | "target_not_ready" | "source_changed" | "authority_changed" | "audit_unavailable" | "operation_cancelled" | "operation_interrupted" | "activation_failed";

export interface TableView {
  Name: string;
  Rows: string;
}

export interface SourceView {
  ServerId: string;
  ServerName: string;
  GobyVersion: string;
  SchemaVersion: string;
  CreatedAt: string;
  Tables: TableView[];
}

export interface BackupView {
  Id: string;
  Kind: BackupKind;
  State: BackupState;
  CreatedAt: string;
  UpdatedAt: string;
  SizeBytes: string;
  SHA256: string;
  Verified: boolean;
  ErrorCode: BackupErrorCode;
  Source: SourceView | null;
}

export interface OperationView {
  Id: string;
  RequestId: string;
  Revision: string;
  Kind: OperationKind;
  State: OperationState;
  Phase: OperationPhase;
  BackupId: string;
  CreatedAt: string;
  UpdatedAt: string;
  ErrorCode: BackupErrorCode;
  Source: SourceView | null;
  RestoreDefaults: boolean;
  ReplaceRollback: boolean;
  CanCancel: boolean;
  CanApply: boolean;
  GenerationRevision: string;
}

export interface BackupPageQuery {
  StartIndex?: number;
  Limit?: number;
}

export interface BackupPage {
  Items: BackupView[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
}

export interface OperationPage {
  Items: OperationView[];
  TotalRecordCount: number;
  StartIndex: number;
  Limit: number;
}

export interface LimitsView {
  MaxBackupBytes: string;
  MaxStoredBytes: string;
  MaxBackups: number;
  MinPassphraseBytes: number;
  MaxPassphraseBytes: number;
}

export interface StorageView {
  Bytes: string;
  Objects: number;
}

export interface RollbackView {
  Available: boolean;
  MustReplace: boolean;
  CreatedAt: string | null;
  ServerName: string;
  Generation: string;
  UnavailableReason: BackupUnavailableReason;
}

export interface StatusView {
  Available: boolean;
  UnavailableReason: BackupUnavailableReason;
  RestoreAvailable: boolean;
  RestoreUnavailableReason: BackupUnavailableReason;
  Busy: boolean;
  ActiveOperationId: string;
  GenerationRevision: string;
  Limits: LimitsView;
  Storage: StorageView;
  Rollback: RollbackView;
}

export interface CreateBackupInput {
  RequestId: string;
  Passphrase: string;
}

export interface DeleteBackupInput {
  RequestId: string;
  SHA256: string;
}

export interface CancelOperationInput {
  Revision: string;
}

export interface PlanRestoreInput {
  RequestId: string;
  BackupId: string;
  SHA256: string;
  Passphrase: string;
  RestoreDefaults: boolean;
  ReplaceRollback: boolean;
  GenerationRevision: string;
}

export interface ApplyRestoreInput {
  Revision: string;
  GenerationRevision: string;
}

export interface RollbackInput {
  RequestId: string;
  GenerationRevision: string;
}

const backupKinds = ["generated", "imported"] as const;
const backupStates = ["writing", "ready", "failed", "cancelled", "interrupted", "deleting"] as const;
const operationKinds = ["create", "import", "delete", "restore", "rollback"] as const;
const operationStates = ["pending", "running", "ready", "applying", "completed", "failed", "cancelled", "interrupted"] as const;
const operationPhases = ["admission", "upload", "snapshot", "encryption", "publication", "validation", "staging", "ready", "activation", "rollback", "cleanup", "finished"] as const;
const unavailableReasons = ["", "storage_unavailable", "tools_unavailable", "database_unavailable", "recovery_database_not_configured", "recovery_required"] as const;
const errorCodes = [...unavailableReasons, "invalid_archive", "capacity_exceeded", "target_not_ready", "source_changed", "authority_changed", "audit_unavailable", "operation_cancelled", "operation_interrupted", "activation_failed"] as const;
const sourceFields = ["ServerId", "ServerName", "GobyVersion", "SchemaVersion", "CreatedAt", "Tables"];
const backupFields = ["Id", "Kind", "State", "CreatedAt", "UpdatedAt", "SizeBytes", "SHA256", "Verified", "ErrorCode", "Source"];
const operationFields = ["Id", "RequestId", "Revision", "Kind", "State", "Phase", "BackupId", "CreatedAt", "UpdatedAt", "ErrorCode", "Source", "RestoreDefaults", "ReplaceRollback", "CanCancel", "CanApply", "GenerationRevision"];
const pageFields = ["Items", "TotalRecordCount", "StartIndex", "Limit"];
const statusFields = ["Available", "UnavailableReason", "RestoreAvailable", "RestoreUnavailableReason", "Busy", "ActiveOperationId", "GenerationRevision", "Limits", "Storage", "Rollback"];
const maxUint64 = "18446744073709551615";

function exactFields(value: unknown, fields: readonly string[]): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    && Object.keys(value).length === fields.length
    && Object.keys(value).every((field) => fields.includes(field));
}

function oneOf<T extends string>(value: unknown, values: readonly T[]): value is T {
  return typeof value === "string" && values.includes(value as T);
}

function validId(value: unknown): value is string {
  return typeof value === "string" && /^[a-f0-9]{32}$/.test(value);
}

function validDigest(value: unknown): value is string {
  return typeof value === "string" && /^[a-f0-9]{64}$/.test(value);
}

function validDecimal(value: unknown): value is string {
  return typeof value === "string" && /^(0|[1-9]\d*)$/.test(value)
    && (value.length < maxUint64.length || (value.length === maxUint64.length && value <= maxUint64));
}

function validCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= 2147483647;
}

function validTimestamp(value: unknown): value is string {
  if (typeof value !== "string") return false;
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|\+00:00)$/.exec(value);
  if (!match) return false;
  const [, yearText, monthText, dayText, hourText, minuteText, secondText] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1]
    && Number(hourText) <= 23 && Number(minuteText) <= 59 && Number(secondText) <= 59
    && Number.isFinite(Date.parse(value));
}

function validSource(value: unknown): value is SourceView {
  if (!exactFields(value, sourceFields)
    || typeof value.ServerId !== "string" || typeof value.ServerName !== "string"
    || typeof value.GobyVersion !== "string" || !validDecimal(value.SchemaVersion)
    || !validTimestamp(value.CreatedAt) || !Array.isArray(value.Tables)) return false;
  const names = new Set<string>();
  for (const table of value.Tables) {
    if (!exactFields(table, ["Name", "Rows"]) || typeof table.Name !== "string" || !table.Name
      || !validDecimal(table.Rows) || names.has(table.Name)) return false;
    names.add(table.Name);
  }
  return true;
}

function validBackup(value: unknown): value is BackupView {
  return exactFields(value, backupFields) && validId(value.Id)
    && oneOf(value.Kind, backupKinds) && oneOf(value.State, backupStates)
    && validTimestamp(value.CreatedAt) && validTimestamp(value.UpdatedAt)
    && validDecimal(value.SizeBytes) && (value.SHA256 === "" || validDigest(value.SHA256))
    && (value.State !== "ready" || validDigest(value.SHA256))
    && typeof value.Verified === "boolean" && oneOf(value.ErrorCode, errorCodes)
    && (value.Source === null || validSource(value.Source))
    && (value.Kind !== "imported" || value.Verified || value.Source === null);
}

function validOperation(value: unknown): value is OperationView {
  return exactFields(value, operationFields) && validId(value.Id) && validId(value.RequestId)
    && validDecimal(value.Revision) && oneOf(value.Kind, operationKinds)
    && oneOf(value.State, operationStates) && oneOf(value.Phase, operationPhases)
    && (value.BackupId === "" || validId(value.BackupId))
    && validTimestamp(value.CreatedAt) && validTimestamp(value.UpdatedAt)
    && oneOf(value.ErrorCode, errorCodes) && (value.Source === null || validSource(value.Source))
    && [value.RestoreDefaults, value.ReplaceRollback, value.CanCancel, value.CanApply].every((field) => typeof field === "boolean")
    && validDecimal(value.GenerationRevision);
}

function validStatus(value: unknown): value is StatusView {
  if (!exactFields(value, statusFields)
    || ![value.Available, value.RestoreAvailable, value.Busy].every((field) => typeof field === "boolean")
    || !oneOf(value.UnavailableReason, unavailableReasons) || !oneOf(value.RestoreUnavailableReason, unavailableReasons)
    || (value.ActiveOperationId !== "" && !validId(value.ActiveOperationId))
    || !validDecimal(value.GenerationRevision)
    || !exactFields(value.Limits, ["MaxBackupBytes", "MaxStoredBytes", "MaxBackups", "MinPassphraseBytes", "MaxPassphraseBytes"])
    || !exactFields(value.Storage, ["Bytes", "Objects"])
    || !exactFields(value.Rollback, ["Available", "MustReplace", "CreatedAt", "ServerName", "Generation", "UnavailableReason"])) return false;
  const { Limits, Storage, Rollback } = value;
  return validDecimal(Limits.MaxBackupBytes) && validDecimal(Limits.MaxStoredBytes)
    && validCount(Limits.MaxBackups) && Limits.MinPassphraseBytes === 12 && Limits.MaxPassphraseBytes === 1024
    && validDecimal(Storage.Bytes) && validCount(Storage.Objects)
    && typeof Rollback.Available === "boolean" && typeof Rollback.MustReplace === "boolean"
    && (Rollback.CreatedAt === null || validTimestamp(Rollback.CreatedAt))
    && typeof Rollback.ServerName === "string" && (Rollback.Generation === "" || validId(Rollback.Generation))
    && oneOf(Rollback.UnavailableReason, unavailableReasons);
}

function invalidResponse(): ApiError {
  return new ApiError("The server returned an invalid response. Please try again.", { status: 200, code: "invalid_response" });
}

function invalidInput(message: string): ApiError {
  return new ApiError(message, { code: "invalid_backup_request" });
}

function requireId(id: string): void {
  if (!validId(id)) throw invalidInput("The backup or operation ID is invalid. Refresh the page.");
}

function pageParameters(query: BackupPageQuery): { startIndex: number; limit: number; parameters: URLSearchParams } {
  const startIndex = query.StartIndex ?? 0;
  const limit = query.Limit ?? 25;
  if (!validCount(startIndex) || !validCount(limit) || limit < 1 || limit > 100
    || Object.keys(query).some((field) => field !== "StartIndex" && field !== "Limit")) {
    throw invalidInput("The backup page must use a nonnegative start index and a limit between 1 and 100.");
  }
  return { startIndex, limit, parameters: new URLSearchParams({ StartIndex: String(startIndex), Limit: String(limit) }) };
}

function validatePage<T extends { Id: string }>(
  value: unknown,
  startIndex: number,
  limit: number,
  validateItem: (item: unknown) => item is T,
): asserts value is { Items: T[]; TotalRecordCount: number; StartIndex: number; Limit: number } {
  if (!exactFields(value, pageFields) || !Array.isArray(value.Items) || !validCount(value.TotalRecordCount)
    || value.StartIndex !== startIndex || value.Limit !== limit
    || value.Items.length > limit || value.Items.length > Math.max(0, value.TotalRecordCount - startIndex)) throw invalidResponse();
  const ids = new Set<string>();
  for (const item of value.Items) {
    if (!validateItem(item) || ids.has(item.Id)) throw invalidResponse();
    ids.add(item.Id);
  }
}

function operationResult(value: unknown, expected: {
  id?: string;
  requestId?: string;
  kind?: OperationKind;
  backupId?: string;
  restoreDefaults?: boolean;
  replaceRollback?: boolean;
} = {}): OperationView {
  if (!exactFields(value, ["Operation"]) || !validOperation(value.Operation)) throw invalidResponse();
  const operation = value.Operation;
  if ((expected.id !== undefined && operation.Id !== expected.id)
    || (expected.requestId !== undefined && operation.RequestId !== expected.requestId)
    || (expected.kind !== undefined && operation.Kind !== expected.kind)
    || (expected.backupId !== undefined && operation.BackupId !== expected.backupId)
    || (expected.restoreDefaults !== undefined && operation.RestoreDefaults !== expected.restoreDefaults)
    || (expected.replaceRollback !== undefined && operation.ReplaceRollback !== expected.replaceRollback)) throw invalidResponse();
  return operation;
}

export function validateBackupPassphrase(passphrase: string): number {
  if (typeof passphrase !== "string") throw invalidInput("Enter a backup passphrase.");
  for (let index = 0; index < passphrase.length; index += 1) {
    const code = passphrase.charCodeAt(index);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = passphrase.charCodeAt(index + 1);
      if (!(next >= 0xdc00 && next <= 0xdfff)) throw invalidInput("The passphrase contains an invalid Unicode character.");
      index += 1;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      throw invalidInput("The passphrase contains an invalid Unicode character.");
    }
  }
  const bytes = new TextEncoder().encode(passphrase).byteLength;
  if (bytes < 12 || bytes > 1024) throw invalidInput("The passphrase must contain between 12 and 1024 UTF-8 bytes.");
  return bytes;
}

export function newBackupRequestId(): string {
  if (!globalThis.crypto?.randomUUID) {
    throw new ApiError("Secure request IDs are unavailable. Open the administrator page over HTTPS.", { code: "secure_random_unavailable" });
  }
  return globalThis.crypto.randomUUID().replaceAll("-", "");
}

export function isTerminalOperation(operation: OperationView): boolean {
  return ["completed", "failed", "cancelled", "interrupted"].includes(operation.State);
}

export const backupsApi = {
  async getStatus(options: RequestOptions = {}): Promise<StatusView> {
    const result = await authenticatedRequest<unknown>("/backups/status", options);
    if (!validStatus(result)) throw invalidResponse();
    return result;
  },

  async getBackups(query: BackupPageQuery = {}, options: RequestOptions = {}): Promise<BackupPage> {
    const { startIndex, limit, parameters } = pageParameters(query);
    const result = await authenticatedRequest<unknown>(`/backups?${parameters}`, options);
    validatePage(result, startIndex, limit, validBackup);
    return result;
  },

  async getBackup(id: string, options: RequestOptions = {}): Promise<BackupView> {
    requireId(id);
    const result = await authenticatedRequest<unknown>(`/backups/${encodeURIComponent(id)}`, options);
    if (!exactFields(result, ["Backup"]) || !validBackup(result.Backup) || result.Backup.Id !== id) throw invalidResponse();
    return result.Backup;
  },

  async getOperations(query: BackupPageQuery = {}, options: RequestOptions = {}): Promise<OperationPage> {
    const { startIndex, limit, parameters } = pageParameters(query);
    const result = await authenticatedRequest<unknown>(`/backup-operations?${parameters}`, options);
    validatePage(result, startIndex, limit, validOperation);
    return result;
  },

  async getOperation(id: string, options: RequestOptions = {}): Promise<OperationView> {
    requireId(id);
    const result = await authenticatedRequest<unknown>(`/backup-operations/${encodeURIComponent(id)}`, options);
    return operationResult(result, { id });
  },

  async createBackup(input: CreateBackupInput, options: RequestOptions = {}): Promise<OperationView> {
    if (!exactFields(input, ["RequestId", "Passphrase"]) || !validId(input.RequestId)) throw invalidInput("The backup request ID is invalid.");
    validateBackupPassphrase(input.Passphrase);
    const result = await authenticatedRequest<unknown>("/backups", { ...options, method: "POST", body: input });
    return operationResult(result, { requestId: input.RequestId, kind: "create" });
  },

  async importBackup(file: File, requestId: string, options: RequestOptions = {}): Promise<OperationView> {
    if (!(file instanceof File) || !Number.isSafeInteger(file.size) || file.size < 1) throw invalidInput("Select a nonempty encrypted backup file.");
    if (!validId(requestId)) throw invalidInput("The import request ID is invalid.");
    // An uncertain upload failure is never replayed; a new attempt needs a new request ID.
    const result = await authenticatedRequest<unknown>("/backups/import", {
      ...options, method: "POST", rawBody: file, headers: { "X-Backup-Request-Id": requestId },
    });
    return operationResult(result, { requestId, kind: "import" });
  },

  async deleteBackup(id: string, input: DeleteBackupInput, options: RequestOptions = {}): Promise<OperationView> {
    requireId(id);
    if (!exactFields(input, ["RequestId", "SHA256"]) || !validId(input.RequestId)
      || (input.SHA256 !== "" && !validDigest(input.SHA256))) throw invalidInput("Refresh the selected backup before deleting it.");
    const result = await authenticatedRequest<unknown>(`/backups/${encodeURIComponent(id)}`, { ...options, method: "DELETE", body: input });
    return operationResult(result, { requestId: input.RequestId, kind: "delete", backupId: id });
  },

  async cancelOperation(id: string, input: CancelOperationInput, options: RequestOptions = {}): Promise<OperationView> {
    requireId(id);
    if (!exactFields(input, ["Revision"]) || !validDecimal(input.Revision)) throw invalidInput("Refresh the operation before cancelling it.");
    const result = await authenticatedRequest<unknown>(`/backup-operations/${encodeURIComponent(id)}/cancel`, { ...options, method: "POST", body: input });
    return operationResult(result, { id });
  },

  async planRestore(input: PlanRestoreInput, options: RequestOptions = {}): Promise<OperationView> {
    if (!exactFields(input, ["RequestId", "BackupId", "SHA256", "Passphrase", "RestoreDefaults", "ReplaceRollback", "GenerationRevision"])
      || !validId(input.RequestId) || !validId(input.BackupId) || !validDigest(input.SHA256)
      || !validDecimal(input.GenerationRevision) || typeof input.RestoreDefaults !== "boolean" || typeof input.ReplaceRollback !== "boolean") {
      throw invalidInput("Refresh the selected backup and recovery status before planning a restore.");
    }
    validateBackupPassphrase(input.Passphrase);
    const result = await authenticatedRequest<unknown>("/restores/plans", { ...options, method: "POST", body: input });
    return operationResult(result, {
      requestId: input.RequestId, kind: "restore", backupId: input.BackupId,
      restoreDefaults: input.RestoreDefaults, replaceRollback: input.ReplaceRollback,
    });
  },

  async applyRestore(id: string, input: ApplyRestoreInput, options: RequestOptions = {}): Promise<OperationView> {
    requireId(id);
    if (!exactFields(input, ["Revision", "GenerationRevision"]) || !validDecimal(input.Revision) || !validDecimal(input.GenerationRevision)) {
      throw invalidInput("Refresh the restore plan and recovery status before applying it.");
    }
    const result = await authenticatedRequest<unknown>(`/restores/${encodeURIComponent(id)}/apply`, { ...options, method: "POST", body: input });
    return operationResult(result, { id, kind: "restore" });
  },

  async rollback(input: RollbackInput, options: RequestOptions = {}): Promise<OperationView> {
    if (!exactFields(input, ["RequestId", "GenerationRevision"]) || !validId(input.RequestId) || !validDecimal(input.GenerationRevision)) {
      throw invalidInput("Refresh recovery status before rolling back.");
    }
    const result = await authenticatedRequest<unknown>("/restores/rollback", { ...options, method: "POST", body: input });
    return operationResult(result, { requestId: input.RequestId, kind: "rollback" });
  },

  async prepareDownload(id: string, options: RequestOptions = {}): Promise<string> {
    requireId(id);
    const path = `/backups/${encodeURIComponent(id)}/file`;
    const response = await authenticatedRequest<Response>(path, { ...options, method: "HEAD" });
    const length = response.headers.get("Content-Length");
    const disposition = response.headers.get("Content-Disposition");
    const etag = response.headers.get("ETag");
    if (response.status !== 200 || !validDecimal(length) || length === "0"
      || !disposition || !/^attachment(?:\s*;|$)/i.test(disposition)
      || !etag || !/^"[a-f0-9]{64}"$/.test(etag)) throw invalidResponse();
    return `/admin/v1${path}`;
  },
};
