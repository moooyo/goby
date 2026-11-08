import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export type UpdateState = 'unconfigured' | 'not_checked' | 'failed' | 'current' | 'available' | 'version_unknown';
export type ReleaseClassification = 'Release' | 'Beta' | 'Dev';

export interface ServerRelease {
  Version: string;
  Classification: ReleaseClassification;
  PublishedAt: string;
  Description: string;
  InfoUrl: string;
  SourceUrl: string;
  SHA256: string;
  TargetFilename: string;
}

export interface ServerUpdates {
  CanSelfUpdate: false;
  Delivery: 'docker';
  ConfiguredSource: boolean;
  CurrentVersion: string;
  State: UpdateState;
  CheckedAt: string | null;
  Release: ServerRelease | null;
  ErrorCode: string | null;
}

function invalid(): never {
  throw new ApiError('The update response is incomplete. Reload the cached result before continuing.', { code: 'invalid_response' });
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function exact(value: Record<string, unknown>, fields: string[]): boolean {
  return fields.every((field) => Object.hasOwn(value, field)) && Object.keys(value).every((field) => fields.includes(field));
}

function text(value: unknown, maximum: number, allowEmpty = false): value is string {
  return typeof value === 'string' && (allowEmpty || value.length > 0) && value.length <= maximum && !/[\u0000-\u001f\u007f-\u009f]/.test(value);
}

function timestamp(value: unknown): value is string {
  return typeof value === 'string' && value.length <= 64 && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) && Number.isFinite(Date.parse(value));
}

function releaseUrl(value: unknown): value is string {
  if (!text(value, 2048) || value !== value.trim() || value.includes('?') || value.includes('#')) return false;
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && !url.username && !url.password && !url.search && !url.hash
      && (!url.port || url.port === '443') && url.hostname.length <= 253 && url.hostname.includes('.') && !/^\d+(?:\.\d+)*$/.test(url.hostname)
      && url.hostname.split('.').every((label) => label.length > 0 && label.length <= 63 && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/i.test(label));
  } catch { return false; }
}

function validRelease(value: unknown): value is ServerRelease {
  return record(value) && exact(value, ['Version', 'Classification', 'PublishedAt', 'Description', 'InfoUrl', 'SourceUrl', 'SHA256', 'TargetFilename'])
    && text(value.Version, 128) && ['Release', 'Beta', 'Dev'].includes(String(value.Classification)) && timestamp(value.PublishedAt)
    && typeof value.Description === 'string' && value.Description.length <= 4096 && !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/.test(value.Description)
    && releaseUrl(value.InfoUrl) && releaseUrl(value.SourceUrl)
    && typeof value.SHA256 === 'string' && /^[a-f0-9]{64}$/.test(value.SHA256)
    && typeof value.TargetFilename === 'string' && /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(value.TargetFilename);
}

function decodeUpdates(value: unknown): ServerUpdates {
  if (!record(value) || !exact(value, ['CanSelfUpdate', 'Delivery', 'ConfiguredSource', 'CurrentVersion', 'State', 'CheckedAt', 'Release', 'ErrorCode'])
    || value.CanSelfUpdate !== false || value.Delivery !== 'docker' || typeof value.ConfiguredSource !== 'boolean'
    || !text(value.CurrentVersion, 128, true)
    || !['unconfigured', 'not_checked', 'failed', 'current', 'available', 'version_unknown'].includes(String(value.State))) invalid();
  if (value.State === 'unconfigured' || value.State === 'not_checked') {
    if (value.ConfiguredSource !== (value.State === 'not_checked') || value.CheckedAt !== null || value.Release !== null || value.ErrorCode !== null) invalid();
  } else {
    if (!value.ConfiguredSource || !timestamp(value.CheckedAt)) invalid();
    if (value.State === 'failed') {
      if (value.Release !== null || value.ErrorCode !== 'update_check_failed') invalid();
    } else if (!validRelease(value.Release) || value.ErrorCode !== (value.State === 'version_unknown' ? 'current_version_unknown' : null)) invalid();
  }
  return value as unknown as ServerUpdates;
}

export const updatesApi = {
  async get(options: RequestOptions = {}): Promise<ServerUpdates> {
    return decodeUpdates(await authenticatedRequest<unknown>('/server-updates', options));
  },
  async check(options: RequestOptions = {}): Promise<ServerUpdates> {
    return decodeUpdates(await authenticatedRequest<unknown>('/server-updates/check', { ...options, method: 'POST', body: {} }));
  },
};
