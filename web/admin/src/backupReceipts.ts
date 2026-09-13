import { ApiError } from './api';
import type { OperationView } from './backupsApi';

export interface BackupAttempt {
  RequestId: string;
  Kind: OperationView['Kind'];
  OperationId?: string;
  ApplyingRevision?: string;
}

export interface BackupReceiptScope {
  ActorId: string;
  ServerId: string;
}

const fields = ['RequestId', 'Kind', 'OperationId', 'ApplyingRevision'];
const idPattern = /^[a-f0-9]{32}$/;
const maximumRevision = '18446744073709551615';

function unavailable(): ApiError {
  return new ApiError('The pending recovery receipt could not be stored or read. Allow session storage and reload before starting another recovery request.', { code: 'backup_receipt_unavailable' });
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function identifier(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= 256
    && value.trim() === value && !/[\u0000-\u001f\u007f-\u009f]/u.test(value);
}

function validAttempt(value: unknown): value is BackupAttempt {
  return record(value) && Object.keys(value).every((field) => fields.includes(field))
    && typeof value.RequestId === 'string' && idPattern.test(value.RequestId)
    && typeof value.Kind === 'string' && ['create', 'import', 'delete', 'restore', 'rollback'].includes(value.Kind)
    && (value.OperationId === undefined || (typeof value.OperationId === 'string' && idPattern.test(value.OperationId)))
    && (value.ApplyingRevision === undefined || (value.Kind === 'restore' && typeof value.OperationId === 'string'
      && typeof value.ApplyingRevision === 'string' && /^(0|[1-9]\d*)$/.test(value.ApplyingRevision)
      && (value.ApplyingRevision.length < maximumRevision.length
        || (value.ApplyingRevision.length === maximumRevision.length && value.ApplyingRevision <= maximumRevision))));
}

function key(scope: BackupReceiptScope): string {
  if (!identifier(scope.ActorId) || !identifier(scope.ServerId)) throw unavailable();
  return `goby.backup-attempt.v1.${encodeURIComponent(window.location.origin)}.${encodeURIComponent(scope.ServerId)}.${encodeURIComponent(scope.ActorId)}`;
}

export function readBackupAttempt(scope: BackupReceiptScope): BackupAttempt | undefined {
  try {
    const raw = sessionStorage.getItem(key(scope));
    if (raw === null) return undefined;
    if (raw.length > 2048) throw unavailable();
    const saved: unknown = JSON.parse(raw);
    if (!record(saved) || Object.keys(saved).length !== 5
      || !Object.keys(saved).every((field) => ['Version', 'Origin', 'ActorId', 'ServerId', 'Attempt'].includes(field))
      || saved.Version !== 1 || saved.Origin !== window.location.origin
      || saved.ActorId !== scope.ActorId || saved.ServerId !== scope.ServerId || !validAttempt(saved.Attempt)) throw unavailable();
    return saved.Attempt;
  } catch {
    throw unavailable();
  }
}

export function sameBackupAttempt(first: BackupAttempt, second: BackupAttempt): boolean {
  return first.RequestId === second.RequestId && first.Kind === second.Kind
    && first.OperationId === second.OperationId && first.ApplyingRevision === second.ApplyingRevision;
}

export function storeBackupAttempt(scope: BackupReceiptScope, attempt: BackupAttempt): void {
  if (!validAttempt(attempt)) throw unavailable();
  // Only non-secret correlation fields are serialized. No request body, password,
  // passphrase, cookie, CSRF token, or access token can enter this receipt.
  const saved: BackupAttempt = { RequestId: attempt.RequestId, Kind: attempt.Kind,
    ...(attempt.OperationId === undefined ? {} : { OperationId: attempt.OperationId }),
    ...(attempt.ApplyingRevision === undefined ? {} : { ApplyingRevision: attempt.ApplyingRevision }) };
  try {
    const previous = readBackupAttempt(scope);
    if (previous && !sameBackupAttempt(previous, saved)) throw unavailable();
    sessionStorage.setItem(key(scope), JSON.stringify({ Version: 1, Origin: window.location.origin,
      ActorId: scope.ActorId, ServerId: scope.ServerId, Attempt: saved }));
  } catch {
    throw unavailable();
  }
}

export function removeBackupAttempt(scope: BackupReceiptScope, attempt: BackupAttempt): void {
  try {
    const saved = readBackupAttempt(scope);
    // A late result cannot remove a different operation's pending receipt.
    if (saved && sameBackupAttempt(saved, attempt)) sessionStorage.removeItem(key(scope));
  } catch {
    throw unavailable();
  }
}

export function backupAttemptMatchesOperation(attempt: BackupAttempt, operation: OperationView): boolean {
  return attempt.RequestId === operation.RequestId && attempt.Kind === operation.Kind
    && (attempt.OperationId === undefined || attempt.OperationId === operation.Id);
}

export function backupAttemptResolvedByOperation(attempt: BackupAttempt, operation: OperationView): boolean {
  if (!backupAttemptMatchesOperation(attempt, operation)) return false;
  if (attempt.ApplyingRevision === undefined) return true;
  // A stale planning response cannot settle a later activation request. Only a
  // newer activation or terminal transition resolves that request's receipt.
  return BigInt(operation.Revision) > BigInt(attempt.ApplyingRevision)
    && ['applying', 'completed', 'failed', 'cancelled', 'interrupted'].includes(operation.State);
}
