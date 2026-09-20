import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface NotificationSettings { Revision: string; Enabled: boolean; Endpoint: string; AllowedNetworks: string[]; HasReceiverCredential: boolean; SupportedEvents: string[]; PendingCount: number }
export interface NotificationSettingsInput { Revision: string; Enabled: boolean; Endpoint: string; AllowedNetworks: string[]; ReceiverCredential?: string }
export interface NotificationDraft { enabled: boolean; endpoint: string; networks: string; credentialMode: 'keep' | 'replace' | 'clear'; credential: string }
const keys = ['Revision', 'Enabled', 'Endpoint', 'AllowedNetworks', 'HasReceiverCredential', 'SupportedEvents', 'PendingCount'];

export function notificationDraft(value: NotificationSettings): NotificationDraft { return { enabled: value.Enabled, endpoint: value.Endpoint, networks: value.AllowedNetworks.join('\n'), credentialMode: 'keep', credential: '' }; }

export function parseNotificationDraft(draft: NotificationDraft, saved: NotificationSettings): { input?: NotificationSettingsInput; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const endpoint = draft.endpoint;
  if (endpoint || draft.enabled) {
    try {
      const url = new URL(endpoint);
      if (endpoint !== endpoint.trim() || new TextEncoder().encode(endpoint).length > 2048 || /[\p{Cc}\uD800-\uDFFF]/u.test(endpoint) || url.protocol !== 'https:' || !url.hostname || url.username || url.password || url.search || url.hash) throw new Error();
    } catch { errors.Endpoint = 'Use an HTTPS receiver URL of at most 2,048 UTF-8 bytes, without credentials, a query, or a fragment.'; }
  }
  const networks = draft.networks.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  if (networks.length > 32 || new Set(networks).size !== networks.length || networks.some((value) => {
    const [address, prefix, extra] = value.split('/');
    if (extra !== undefined || !address || !/^\d{1,3}$/.test(prefix ?? '') || String(Number(prefix)) !== prefix) return true;
    if (address.includes(':')) { try { return Number(prefix) > 128 || !/^[a-f0-9:.]+$/.test(address) || !new URL(`http://[${address}]/`).hostname.startsWith('['); } catch { return true; } }
    return Number(prefix) > 32 || address.split('.').length !== 4 || address.split('.').some((part) => !/^\d{1,3}$/.test(part) || Number(part) > 255 || String(Number(part)) !== part);
  })) errors.AllowedNetworks = 'Use at most 32 distinct network CIDRs, one per line. The server verifies canonical network addresses.';
  if (draft.credentialMode === 'replace' && !/^[\x21-\x7e]{16,2048}$/.test(draft.credential)) errors.ReceiverCredential = 'Use 16 to 2,048 visible ASCII characters without spaces.';
  const hasCredential = draft.credentialMode === 'replace' ? !errors.ReceiverCredential : draft.credentialMode === 'keep' && saved.HasReceiverCredential;
  if (draft.enabled && !hasCredential) errors.ReceiverCredential = 'An enabled receiver requires a saved or replacement credential. Disable notifications before clearing it.';
  const input: NotificationSettingsInput = { Revision: saved.Revision, Enabled: draft.enabled, Endpoint: endpoint, AllowedNetworks: networks,
    ...(draft.credentialMode === 'keep' ? {} : { ReceiverCredential: draft.credentialMode === 'clear' ? '' : draft.credential }),
  };
  return { errors, input: Object.keys(errors).length ? undefined : input };
}

function decode(value: NotificationSettings): NotificationSettings {
  if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).length !== keys.length || Object.keys(value).some((key) => !keys.includes(key))
    || typeof value.Revision !== 'string' || !/^[1-9]\d*$/.test(value.Revision) || value.Revision.length > 19 || BigInt(value.Revision) > 9223372036854775807n
    || typeof value.Enabled !== 'boolean' || typeof value.Endpoint !== 'string' || new TextEncoder().encode(value.Endpoint).length > 2048
    || typeof value.HasReceiverCredential !== 'boolean' || !Array.isArray(value.AllowedNetworks) || value.AllowedNetworks.length > 32 || !value.AllowedNetworks.every((entry) => typeof entry === 'string')
    || !Array.isArray(value.SupportedEvents) || !value.SupportedEvents.every((entry) => typeof entry === 'string' && entry.length > 0) || !Number.isSafeInteger(value.PendingCount) || value.PendingCount < 0) {
    throw new ApiError('The notification settings response is incomplete. Reload before editing.', { code: 'invalid_response' });
  }
  return value;
}

export const notificationsApi = {
  async get(options: RequestOptions = {}): Promise<NotificationSettings> { return decode(await authenticatedRequest<NotificationSettings>('/notifications', options)); },
  async update(input: NotificationSettingsInput): Promise<NotificationSettings> { return decode(await authenticatedRequest<NotificationSettings>('/notifications', { method: 'PUT', body: input })); },
};
