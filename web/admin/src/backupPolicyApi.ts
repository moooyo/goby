import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';
import { validateBackupPassphrase } from './backupsApi';

export interface BackupPolicySchedule {
  Kind: 'daily' | 'weekly' | 'custom';
  DayOfWeek: number | null;
  TimeOfDay: string;
  Timezone: string;
}

export interface BackupPolicy {
  Revision: string;
  Enabled: boolean;
  Schedule: BackupPolicySchedule;
  KeepCount: number;
  IncludeImages: boolean;
  HasPassphrase: boolean;
  NextRunAt: string | null;
  Available: boolean;
  UnavailableReason: string;
  TaskId: string;
  LastRunAt: string | null;
  LastRunState: string;
  LastBackupId: string;
}

export interface BackupPolicyInput {
  Revision: string;
  KeepCount: number;
  IncludeImages: boolean;
  Passphrase?: string;
}

export interface BackupPolicyDraft {
  keepCount: string;
  includeImages: boolean;
  passphraseMode: 'keep' | 'replace' | 'clear';
  passphrase: string;
  confirmation: string;
}

const policyFields = ['Revision', 'Enabled', 'Schedule', 'KeepCount', 'IncludeImages', 'HasPassphrase', 'NextRunAt', 'Available', 'UnavailableReason', 'TaskId', 'LastRunAt', 'LastRunState', 'LastBackupId'];
const scheduleFields = ['Kind', 'DayOfWeek', 'TimeOfDay', 'Timezone'];
const timePattern = /^(?:[01]\d|2[0-3]):[0-5]\d$/;
const timezonePattern = /^[A-Za-z0-9_+-]+(?:\/[A-Za-z0-9_+-]+)*$/;
const idPattern = /^[a-f0-9]{32}$/;
const revisionPattern = /^[1-9]\d{0,18}$/;

function exactRecord(value: unknown, fields: string[]): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    && Object.keys(value).length === fields.length && Object.keys(value).every((key) => fields.includes(key));
}
function timestamp(value: unknown): value is string | null {
  return value === null || typeof value === 'string' && value.length <= 40 && Number.isFinite(Date.parse(value));
}
function timezone(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= 128
    && !['Local', 'localtime', 'posixrules'].includes(value) && timezonePattern.test(value);
}

function decodePolicy(value: unknown): BackupPolicy {
  const invalid = () => new ApiError('The backup settings response is incomplete. Reload the saved settings before editing.', { code: 'invalid_response' });
  if (!exactRecord(value, policyFields) || !exactRecord(value.Schedule, scheduleFields)) throw invalid();
  const schedule = value.Schedule;
  if (typeof value.Revision !== 'string' || !revisionPattern.test(value.Revision) || BigInt(value.Revision) > 9223372036854775807n
    || typeof value.Enabled !== 'boolean' || typeof value.IncludeImages !== 'boolean' || typeof value.HasPassphrase !== 'boolean'
    || typeof value.Available !== 'boolean' || typeof value.KeepCount !== 'number' || !Number.isInteger(value.KeepCount) || value.KeepCount < 1 || value.KeepCount > 30
    || !['daily', 'weekly', 'custom'].includes(schedule.Kind as string) || !timezone(schedule.Timezone)
    || typeof schedule.TimeOfDay !== 'string' || (schedule.Kind === 'custom' ? schedule.TimeOfDay !== '' && !timePattern.test(schedule.TimeOfDay) : !timePattern.test(schedule.TimeOfDay))
    || (schedule.DayOfWeek !== null && (typeof schedule.DayOfWeek !== 'number' || !Number.isInteger(schedule.DayOfWeek) || schedule.DayOfWeek < 0 || schedule.DayOfWeek > 6))
    || schedule.Kind === 'weekly' && schedule.DayOfWeek === null || schedule.Kind === 'daily' && schedule.DayOfWeek !== null
    || !timestamp(value.NextRunAt) || !timestamp(value.LastRunAt)
    || typeof value.TaskId !== 'string' || value.TaskId !== '' && !idPattern.test(value.TaskId)
    || typeof value.LastBackupId !== 'string' || value.LastBackupId !== '' && !idPattern.test(value.LastBackupId)
    || typeof value.UnavailableReason !== 'string' || value.UnavailableReason.length > 128
    || typeof value.LastRunState !== 'string' || value.LastRunState.length > 128) throw invalid();
  return value as unknown as BackupPolicy;
}

export function backupPolicyDraft(policy: BackupPolicy): BackupPolicyDraft {
  return {
    keepCount: String(policy.KeepCount),
    includeImages: policy.IncludeImages, passphraseMode: 'keep', passphrase: '', confirmation: '',
  };
}

export function parseBackupPolicyDraft(draft: BackupPolicyDraft, saved: BackupPolicy): { input?: BackupPolicyInput; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  if (!/^(?:[1-9]|[12]\d|30)$/.test(draft.keepCount)) errors.KeepCount = 'Keep between 1 and 30 completed automatic backups.';
  if (draft.passphraseMode === 'replace') {
    try { validateBackupPassphrase(draft.passphrase); }
    catch { errors.Passphrase = 'Use 12–1024 UTF-8 bytes. Spaces and letter case are preserved exactly.'; }
    if (draft.confirmation !== draft.passphrase) errors.Confirmation = 'The passphrases do not match.';
  }
  if (Object.keys(errors).length) return { errors };
  return { errors, input: {
    Revision: saved.Revision,
    KeepCount: Number(draft.keepCount), IncludeImages: draft.includeImages,
    ...(draft.passphraseMode === 'keep' ? {} : { Passphrase: draft.passphraseMode === 'clear' ? '' : draft.passphrase }),
  } };
}

async function policyRequest(method: 'GET' | 'PATCH', body: BackupPolicyInput | undefined, options: RequestOptions): Promise<BackupPolicy> {
  const controller = new AbortController();
  let timedOut = false;
  const cancel = () => controller.abort(options.signal?.reason);
  if (options.signal?.aborted) cancel();
  else options.signal?.addEventListener('abort', cancel, { once: true });
  let stopAbort: (() => void) | undefined;
  const aborted = new Promise<never>((_resolve, reject) => {
    stopAbort = () => reject(controller.signal.reason);
    if (controller.signal.aborted) stopAbort();
    else controller.signal.addEventListener('abort', stopAbort, { once: true });
  });
  const timer = setTimeout(() => { timedOut = true; controller.abort(new DOMException('Backup settings request timed out.', 'TimeoutError')); }, 15_000);
  try {
    return decodePolicy(await Promise.race([authenticatedRequest<unknown>('/backups/policy', { ...options, signal: controller.signal, method, ...(body ? { body } : {}) }), aborted]));
  } catch (error) {
    if (timedOut) throw new ApiError('The server did not respond in time. Reload the saved backup settings before trying again.', { code: 'network_error' });
    throw error;
  } finally {
    clearTimeout(timer);
    options.signal?.removeEventListener('abort', cancel);
    if (stopAbort) controller.signal.removeEventListener('abort', stopAbort);
  }
}

export const backupPolicyApi = {
  get(options: RequestOptions = {}) { return policyRequest('GET', undefined, options); },
  update(input: BackupPolicyInput, options: RequestOptions = {}) { return policyRequest('PATCH', input, options); },
};
