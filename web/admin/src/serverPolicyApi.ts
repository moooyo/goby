import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export type ServerUICulture = 'zh-CN' | 'en-US';

export interface ServerPolicyClient {
  Address: string | null;
  IsLocal: boolean;
}

export interface ServerPolicy {
  Revision: string;
  EnableRemoteAccess: boolean;
  LocalNetworkSubnets: string[];
  UICulture: ServerUICulture;
  UpdatedAt: string;
  Client: ServerPolicyClient;
  Restart: { Available: boolean; InProgress: boolean };
  Updates: { CanSelfUpdate: false; Delivery: 'docker'; ConfiguredSource: boolean; CurrentVersion: string };
  DisconnectExpected?: boolean;
}

export interface ServerPolicyInput {
  Revision: string;
  EnableRemoteAccess: boolean;
  LocalNetworkSubnets: string[];
  UICulture: ServerUICulture;
}

export interface ServerPolicyPreview {
  Revision: string;
  WouldDisconnect: boolean;
  Client: ServerPolicyClient;
}

export interface ServerPolicyDraft {
  enableRemoteAccess: boolean;
  localNetworkSubnets: string;
  uiCulture: ServerUICulture;
}

function invalid(): never {
  throw new ApiError('The server policy response is incomplete. Reload the saved settings before continuing.', { code: 'invalid_response' });
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function exact(value: Record<string, unknown>, required: string[], optional: string[] = []): boolean {
  return required.every((key) => Object.hasOwn(value, key)) && Object.keys(value).every((key) => required.includes(key) || optional.includes(key));
}

function validRevision(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9]\d*$/.test(value) && value.length <= 19 && BigInt(value) <= 9223372036854775807n;
}

function validClient(value: unknown): value is ServerPolicyClient {
  return record(value) && exact(value, ['Address', 'IsLocal']) && typeof value.IsLocal === 'boolean'
    && (value.Address === null || typeof value.Address === 'string' && value.Address.length <= 64 && /^[0-9a-f:.]+(?:%[0-9a-z_.~-]{1,32})?$/i.test(value.Address));
}

function validCIDRSyntax(value: string): boolean {
  const [address, prefix, extra] = value.split('/');
  if (extra !== undefined || !address || !/^(?:0|[1-9]\d{0,2})$/.test(prefix ?? '')) return false;
  const bits = Number(prefix);
  if (address.includes(':')) {
    if (bits > 128 || !/^[0-9a-f:.]+$/i.test(address)) return false;
    try { return new URL(`http://[${address}]/`).hostname.startsWith('['); } catch { return false; }
  }
  const octets = address.split('.');
  if (bits > 32 || octets.length !== 4 || octets.some((part) => !/^(?:0|[1-9]\d{0,2})$/.test(part) || Number(part) > 255)) return false;
  const numeric = octets.reduce((result, part) => (result << 8) | Number(part), 0) >>> 0;
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  return ((numeric & mask) >>> 0) === numeric;
}

export function serverPolicyDraft(value: ServerPolicy): ServerPolicyDraft {
  return { enableRemoteAccess: value.EnableRemoteAccess, localNetworkSubnets: value.LocalNetworkSubnets.join('\n'), uiCulture: value.UICulture };
}

export function parseServerPolicyDraft(draft: ServerPolicyDraft, saved: ServerPolicy): { input?: ServerPolicyInput; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const networks = draft.localNetworkSubnets.split(/\r?\n/).map((value) => value.trim()).filter(Boolean);
  if (networks.length > 32 || new Set(networks).size !== networks.length || networks.some((value) => value.length > 64 || !validCIDRSyntax(value))) {
    errors.LocalNetworkSubnets = 'Use up to 32 distinct network CIDRs, one per line. Use network addresses without host bits.';
  }
  if (!['zh-CN', 'en-US'].includes(draft.uiCulture)) errors.UICulture = 'Choose a supported default language.';
  return {
    input: Object.keys(errors).length ? undefined : {
      Revision: saved.Revision, EnableRemoteAccess: draft.enableRemoteAccess, LocalNetworkSubnets: networks, UICulture: draft.uiCulture,
    },
    errors,
  };
}

function decodePolicy(value: unknown): ServerPolicy {
  if (!record(value) || !exact(value, ['Revision', 'EnableRemoteAccess', 'LocalNetworkSubnets', 'UICulture', 'UpdatedAt', 'Client', 'Restart', 'Updates'], ['DisconnectExpected'])
    || !validRevision(value.Revision) || typeof value.EnableRemoteAccess !== 'boolean'
    || !Array.isArray(value.LocalNetworkSubnets) || value.LocalNetworkSubnets.length > 32
    || !value.LocalNetworkSubnets.every((entry) => typeof entry === 'string' && entry.length <= 64 && validCIDRSyntax(entry))
    || new Set(value.LocalNetworkSubnets).size !== value.LocalNetworkSubnets.length
    || !['zh-CN', 'en-US'].includes(String(value.UICulture)) || typeof value.UpdatedAt !== 'string' || !Number.isFinite(Date.parse(value.UpdatedAt))
    || !validClient(value.Client) || !record(value.Restart) || !exact(value.Restart, ['Available', 'InProgress'])
    || typeof value.Restart.Available !== 'boolean' || typeof value.Restart.InProgress !== 'boolean'
    || !record(value.Updates) || !exact(value.Updates, ['CanSelfUpdate', 'Delivery', 'ConfiguredSource', 'CurrentVersion'])
    || value.Updates.CanSelfUpdate !== false || value.Updates.Delivery !== 'docker' || typeof value.Updates.ConfiguredSource !== 'boolean'
    || typeof value.Updates.CurrentVersion !== 'string' || value.Updates.CurrentVersion.length > 128 || /[\u0000-\u001f\u007f]/.test(value.Updates.CurrentVersion)
    || value.DisconnectExpected !== undefined && typeof value.DisconnectExpected !== 'boolean') invalid();
  return value as unknown as ServerPolicy;
}

export const serverPolicyApi = {
  async get(options: RequestOptions = {}): Promise<ServerPolicy> {
    return decodePolicy(await authenticatedRequest<unknown>('/server-policy', options));
  },
  async preview(input: ServerPolicyInput, options: RequestOptions = {}): Promise<ServerPolicyPreview> {
    const value = await authenticatedRequest<unknown>('/server-policy/preview', { ...options, method: 'POST', body: input });
    if (!record(value) || !exact(value, ['Revision', 'WouldDisconnect', 'Client']) || !validRevision(value.Revision)
      || value.Revision !== input.Revision || typeof value.WouldDisconnect !== 'boolean' || !validClient(value.Client)) invalid();
    return value as unknown as ServerPolicyPreview;
  },
  async update(input: ServerPolicyInput, confirmDisconnect: boolean, options: RequestOptions = {}): Promise<ServerPolicy> {
    const value = decodePolicy(await authenticatedRequest<unknown>('/server-policy', { ...options, method: 'PUT', body: { ...input, ConfirmDisconnect: confirmDisconnect } }));
    if (BigInt(value.Revision) < BigInt(input.Revision) || typeof value.DisconnectExpected !== 'boolean') invalid();
    return value;
  },
  async restart(revision: string, options: RequestOptions = {}): Promise<void> {
    const value = await authenticatedRequest<unknown>('/server/restart', { ...options, method: 'POST', body: { Revision: revision, Confirm: true } });
    if (!record(value) || !exact(value, ['Accepted', 'Restarting']) || value.Accepted !== true || value.Restarting !== true) invalid();
  },
};
