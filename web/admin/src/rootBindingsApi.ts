import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface RegisteredRoot {
  Id: string;
  LibraryId: string;
  Path: string;
  AllowedPath: string;
  RelativePath: string;
  Revision: string;
}

export interface RegisteredRootsResponse {
  Items: RegisteredRoot[];
  TotalRecordCount: number;
}

export interface StorageIdentity {
  Profile: string;
  FilesystemUUID: string;
  Digest: string;
}

export interface StorageBoundary {
  RelativePath: string;
  Identity: StorageIdentity;
}

export interface StorageTopology {
  Anchor: StorageIdentity;
  RegisteredRoot: StorageIdentity;
  Boundaries: StorageBoundary[];
}

export interface RootBinding extends RegisteredRoot {
  Status: 'verified' | 'unbound' | 'mismatch' | 'unavailable';
  ApprovedFingerprint?: string;
  ObservedFingerprint?: string;
  Approved?: StorageTopology;
  Observed?: StorageTopology;
  BoundAt?: string;
  BoundBy?: string;
}

export interface RootBindingInput {
  Revision: string;
  ObservedFingerprint: string;
  AcknowledgeMissingRemoval: true;
}

const rootFields = ['Id', 'LibraryId', 'Path', 'AllowedPath', 'RelativePath', 'Revision'];
const bindingFields = [...rootFields, 'Status', 'ApprovedFingerprint', 'ObservedFingerprint', 'Approved', 'Observed', 'BoundAt', 'BoundBy'];
const maximumRevision = '9223372036854775807';
const fingerprintPattern = /^[0-9a-f]{64}$/;

function invalidResponse(): ApiError {
  return new ApiError('The storage binding response is incomplete or invalid. Refresh the observation before making changes.', { code: 'invalid_response' });
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function fields(value: Record<string, unknown>, allowed: string[]): boolean {
  return Object.keys(value).every((key) => allowed.includes(key));
}

function boundedText(value: unknown, maximum: number): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= maximum
    && !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(^|[^\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(value)
    && new TextEncoder().encode(value).length <= maximum;
}

function identifier(value: unknown): value is string {
  return boundedText(value, 256) && value.trim() === value && !/[\u0000-\u001f\u007f-\u009f]/u.test(value);
}

function revision(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]*$/.test(value) && value.length <= maximumRevision.length
    && (value.length < maximumRevision.length || value <= maximumRevision);
}

function fingerprint(value: unknown): value is string {
  return typeof value === 'string' && fingerprintPattern.test(value);
}

function path(value: unknown, absolute: boolean): value is string {
  if (!boundedText(value, 4096) || value.includes('\0') || value.startsWith('/') !== absolute) return false;
  if (value === (absolute ? '/' : '.')) return true;
  return (absolute ? value.slice(1) : value).split('/').every((part) => part !== '' && part !== '.' && part !== '..');
}

function registeredRoot(value: unknown): value is RegisteredRoot {
  if (!record(value) || !identifier(value.Id) || !identifier(value.LibraryId) || !revision(value.Revision)
    || !path(value.Path, true) || !path(value.AllowedPath, true) || !path(value.RelativePath, false)) return false;
  return value.Path === (value.RelativePath === '.' ? value.AllowedPath : `${value.AllowedPath === '/' ? '' : value.AllowedPath}/${value.RelativePath}`);
}

function identity(value: unknown): value is StorageIdentity {
  return record(value) && fields(value, ['Profile', 'FilesystemUUID', 'Digest'])
    && value.Profile === 'linux-fsuuid-filehandle-v1' && typeof value.FilesystemUUID === 'string'
    && /^[0-9a-f]{32}$/.test(value.FilesystemUUID) && value.FilesystemUUID !== '0'.repeat(32) && fingerprint(value.Digest);
}

function topology(value: unknown): value is StorageTopology {
  return record(value) && fields(value, ['Anchor', 'RegisteredRoot', 'Boundaries'])
    && identity(value.Anchor) && identity(value.RegisteredRoot) && Array.isArray(value.Boundaries)
    && value.Boundaries.length <= 256
    && value.Boundaries.every((boundary: unknown) => record(boundary) && fields(boundary, ['RelativePath', 'Identity'])
      && path(boundary.RelativePath, false) && boundary.RelativePath !== '.' && identity(boundary.Identity))
    && new Set(value.Boundaries.map((boundary: StorageBoundary) => boundary.RelativePath)).size === value.Boundaries.length;
}

function timestamp(value: unknown): value is string {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)
    && Number.isFinite(Date.parse(value));
}

function decodeBinding(result: unknown, libraryId: string, rootId: string): RootBinding {
  if (!record(result) || !fields(result, ['Binding']) || !record(result.Binding)) throw invalidResponse();
  const value = result.Binding;
  if (!fields(value, bindingFields) || !registeredRoot(value) || value.LibraryId !== libraryId || value.Id !== rootId
    || !['verified', 'unbound', 'mismatch', 'unavailable'].includes(value.Status as string)) throw invalidResponse();
  const approved = value.Approved !== undefined;
  const observed = value.Observed !== undefined;
  if (approved !== (value.ApprovedFingerprint !== undefined) || approved !== (value.BoundAt !== undefined)
    || approved !== (value.BoundBy !== undefined) || observed !== (value.ObservedFingerprint !== undefined)
    || (approved && (!topology(value.Approved) || !fingerprint(value.ApprovedFingerprint) || !timestamp(value.BoundAt)
      || !boundedText(value.BoundBy, 256) || !/^[a-zA-Z0-9][a-zA-Z0-9._:-]*$/.test(value.BoundBy)))
    || (observed && (!topology(value.Observed) || !fingerprint(value.ObservedFingerprint)))) throw invalidResponse();
  if ((value.Status === 'unavailable' && observed) || (value.Status !== 'unavailable' && !observed)
    || (value.Status === 'unbound' && approved) || (['verified', 'mismatch'].includes(value.Status as string) && !approved)
    || (value.Status === 'verified' && value.ApprovedFingerprint !== value.ObservedFingerprint)
    || (value.Status === 'mismatch' && value.ApprovedFingerprint === value.ObservedFingerprint)) throw invalidResponse();
  return value as unknown as RootBinding;
}

function bindingPath(libraryId: string, rootId: string): string {
  if (!identifier(libraryId) || !identifier(rootId)) throw new ApiError('The registered root is invalid. Refresh the root list.', { code: 'invalid_root_binding' });
  return `/libraries/${encodeURIComponent(libraryId)}/roots/${encodeURIComponent(rootId)}/binding`;
}

export const rootBindingsApi = {
  async listRoots(libraryId: string, options: RequestOptions = {}): Promise<RegisteredRootsResponse> {
    if (!identifier(libraryId)) throw new ApiError('The library is invalid. Refresh the library list.', { code: 'invalid_root_binding' });
    const result = await authenticatedRequest<unknown>(`/libraries/${encodeURIComponent(libraryId)}/roots`, options);
    if (!record(result) || !fields(result, ['Items', 'TotalRecordCount']) || !Array.isArray(result.Items)
      || !Number.isSafeInteger(result.TotalRecordCount) || result.TotalRecordCount !== result.Items.length || result.Items.length > 4096
      || !result.Items.every((item: unknown) => record(item) && fields(item, rootFields) && registeredRoot(item) && item.LibraryId === libraryId)
      || new Set(result.Items.map((item: RegisteredRoot) => item.Id)).size !== result.Items.length) throw invalidResponse();
    return result as unknown as RegisteredRootsResponse;
  },

  async getBinding(libraryId: string, rootId: string, options: RequestOptions = {}): Promise<RootBinding> {
    return decodeBinding(await authenticatedRequest<unknown>(bindingPath(libraryId, rootId), options), libraryId, rootId);
  },

  async updateBinding(libraryId: string, rootId: string, input: RootBindingInput, options: RequestOptions = {}): Promise<RootBinding> {
    if (!record(input) || !fields(input, ['Revision', 'ObservedFingerprint', 'AcknowledgeMissingRemoval'])
      || !revision(input.Revision) || !fingerprint(input.ObservedFingerprint) || input.AcknowledgeMissingRemoval !== true) {
      throw new ApiError('Refresh the storage observation and acknowledge missing-record removal before approving it.', { code: 'invalid_root_binding' });
    }
    const result = decodeBinding(await authenticatedRequest<unknown>(bindingPath(libraryId, rootId), { ...options, method: 'PUT', body: input }), libraryId, rootId);
    if (result.Status !== 'verified' || result.ObservedFingerprint !== input.ObservedFingerprint
      || result.Revision !== (BigInt(input.Revision) + 1n).toString()) throw invalidResponse();
    return result;
  },
};
