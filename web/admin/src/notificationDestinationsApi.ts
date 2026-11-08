import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';
import { notificationReadTimeout, notificationRequest, notificationWriteTimeout, notificationWriteTimeoutMessage } from './notificationRequest';

export type NotificationTransport = 'webhook' | 'smtp';
export interface NotificationSMTP {
  Host: string;
  Port: number;
  Security: 'starttls' | 'tls';
  Username: string;
  From: string;
  To: string[];
}
export interface NotificationDestinationInput {
  Revision: string;
  Name: string;
  Transport: NotificationTransport;
  Enabled: boolean;
  EventIds: string[];
  AllowedNetworks: string[];
  Endpoint: string;
  SMTP: NotificationSMTP;
  Secret?: string;
}
export interface NotificationDestination extends Omit<NotificationDestinationInput, 'Secret'> {
  Id: string;
  HasSecret: boolean;
  LastOutcome: string;
  LastDeliveredAt?: string | null;
  PendingCount: number;
  DroppedCount: number;
}
export interface NotificationDestinationEvent { Id: string; Name: string; Actions: string[] }
export interface NotificationDestinations {
  Items: NotificationDestination[];
  SupportedEvents: NotificationDestinationEvent[];
  Limit: number;
}
export interface NotificationDestinationDraft {
  name: string;
  transport: NotificationTransport;
  enabled: boolean;
  eventIds: string[];
  networks: string;
  endpoint: string;
  smtpHost: string;
  smtpPort: string;
  smtpSecurity: 'starttls' | 'tls';
  smtpUsername: string;
  smtpFrom: string;
  smtpTo: string;
  secretAction: 'keep' | 'replace' | 'clear';
  secret: string;
}

const maximumRevision = 9223372036854775807n;
const destinationFields = new Set(['Id', 'Revision', 'Name', 'Transport', 'Enabled', 'EventIds', 'AllowedNetworks', 'Endpoint', 'SMTP', 'HasSecret', 'LastOutcome', 'LastDeliveredAt', 'PendingCount', 'DroppedCount']);
const smtpFields = new Set(['Host', 'Port', 'Security', 'Username', 'From', 'To']);
const knownEvents = new Set(['UserChanges', 'SignIn', 'LibraryChanges', 'ScanFinished', 'TaskFinished', 'BackupFinished', 'MetadataChanges']);
const byteLength = (value: string) => new TextEncoder().encode(value).length;
const safeText = (value: unknown, maximum: number): value is string => typeof value === 'string' && byteLength(value) <= maximum && !/[\p{Cc}\uD800-\uDFFF]/u.test(value);
const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const count = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
const revision = (value: unknown): value is string => typeof value === 'string' && /^[1-9]\d{0,18}$/.test(value) && BigInt(value) <= maximumRevision;
const strings = (value: unknown, maximum: number): value is string[] => Array.isArray(value) && value.length <= maximum && value.every((entry) => safeText(entry, 512)) && new Set(value).size === value.length;
const timestamp = (value: unknown) => value === undefined || value === null || typeof value === 'string' && /^\d{4}-\d\d-\d\dT/.test(value) && Number.isFinite(Date.parse(value));

function invalid(): never {
  throw new ApiError('The notification targets response is incomplete. Reload targets before editing.', { code: 'invalid_response' });
}
function validSMTP(value: unknown): value is NotificationSMTP {
  return record(value) && Object.keys(value).length === smtpFields.size && Object.keys(value).every((key) => smtpFields.has(key))
    && safeText(value.Host, 253) && count(value.Port) && value.Port >= 1 && value.Port <= 65535
    && ['starttls', 'tls'].includes(value.Security as string) && safeText(value.Username, 256)
    && safeText(value.From, 254) && strings(value.To, 16) && value.To.every((address) => safeText(address, 254));
}
function decodeDestination(value: unknown): NotificationDestination {
  if (!record(value) || !Object.keys(value).every((key) => destinationFields.has(key))
    || !safeText(value.Id, 64) || !/^[a-zA-Z0-9_-]+$/.test(value.Id) || !revision(value.Revision)
    || !safeText(value.Name, 120) || !value.Name.trim() || !['webhook', 'smtp'].includes(value.Transport as string)
    || typeof value.Enabled !== 'boolean' || !strings(value.EventIds, knownEvents.size) || value.EventIds.length === 0
    || !value.EventIds.every((event) => knownEvents.has(event)) || !strings(value.AllowedNetworks, 32)
    || !safeText(value.Endpoint, 2048) || !validSMTP(value.SMTP) || typeof value.HasSecret !== 'boolean'
    || !safeText(value.LastOutcome, 64) || !timestamp(value.LastDeliveredAt) || !count(value.PendingCount) || !count(value.DroppedCount)) invalid();
  return value as unknown as NotificationDestination;
}
function decodeList(value: unknown): NotificationDestinations {
  if (!record(value) || Object.keys(value).length !== 3 || !Object.keys(value).every((key) => ['Items', 'SupportedEvents', 'Limit'].includes(key))
    || !count(value.Limit) || value.Limit < 1 || value.Limit > 64 || !Array.isArray(value.Items) || value.Items.length > value.Limit
    || !Array.isArray(value.SupportedEvents) || value.SupportedEvents.length > knownEvents.size || !value.SupportedEvents.every((event) => record(event)
      && Object.keys(event).length === 3 && knownEvents.has(event.Id as string) && safeText(event.Name, 128) && strings(event.Actions, 16))) invalid();
  const items = value.Items.map(decodeDestination);
  if (new Set(items.map((item) => item.Id)).size !== items.length || new Set(value.SupportedEvents.map((event) => event.Id)).size !== value.SupportedEvents.length) invalid();
  return { Items: items, SupportedEvents: value.SupportedEvents as NotificationDestinationEvent[], Limit: value.Limit };
}

export function destinationDraft(value?: NotificationDestination): NotificationDestinationDraft {
  return {
    name: value?.Name ?? '', transport: value?.Transport ?? 'webhook', enabled: value?.Enabled ?? false,
    eventIds: [...(value?.EventIds ?? [])], networks: value?.AllowedNetworks.join('\n') ?? '', endpoint: value?.Endpoint ?? '',
    smtpHost: value?.SMTP.Host ?? '', smtpPort: String(value?.SMTP.Port ?? 587), smtpSecurity: value?.SMTP.Security ?? 'starttls',
    smtpUsername: value?.SMTP.Username ?? '', smtpFrom: value?.SMTP.From ?? '', smtpTo: value?.SMTP.To.join('\n') ?? '', secretAction: 'keep', secret: '',
  };
}
export function destinationSecretTargetChanged(draft: NotificationDestinationDraft, saved?: NotificationDestination): boolean {
  return Boolean(saved && (saved.Transport !== draft.transport || (draft.transport === 'webhook'
    ? saved.Endpoint !== draft.endpoint : saved.SMTP.Host !== draft.smtpHost || saved.SMTP.Username !== draft.smtpUsername)));
}
function validNetwork(value: string): boolean {
  const [address, prefix, extra] = value.split('/');
  if (extra !== undefined || !address || !/^\d{1,3}$/.test(prefix ?? '') || String(Number(prefix)) !== prefix) return false;
  if (address.includes(':')) {
    try { return Number(prefix) <= 128 && /^[a-f0-9:.]+$/.test(address) && new URL(`http://[${address}]/`).hostname.startsWith('['); } catch { return false; }
  }
  return Number(prefix) <= 32 && address.split('.').length === 4 && address.split('.').every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255 && String(Number(part)) === part);
}
function validEmail(value: string): boolean {
  return safeText(value, 254) && value === value.trim() && /^[\x21-\x7e]+$/.test(value) && /^[^\s<>@,;]+@[^\s<>@,;]+$/.test(value);
}
function validMailHost(value: string): boolean {
  if (!safeText(value, 253) || !value || value !== value.trim() || /[\s\/@?#]/.test(value)) return false;
  if (!value.includes(':')) return true;
  try { return new URL(`https://[${value}]/`).hostname === `[${value}]`; } catch { return false; }
}
export function parseDestinationDraft(draft: NotificationDestinationDraft, saved?: NotificationDestination): { input?: NotificationDestinationInput; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const name = draft.name.trim();
  if (!name || !safeText(name, 120)) errors.Name = 'Use a target name of at most 120 UTF-8 bytes.';
  const networks = draft.networks.split(/\r?\n/).map((value) => value.trim()).filter(Boolean);
  if (networks.length > 32 || new Set(networks).size !== networks.length || !networks.every(validNetwork)) errors.AllowedNetworks = 'Use at most 32 distinct network CIDRs, one per line. The server verifies canonical network addresses.';
  if (draft.eventIds.length === 0 || !draft.eventIds.every((event) => knownEvents.has(event)) || new Set(draft.eventIds).size !== draft.eventIds.length) errors.EventIds = 'Select at least one supported event.';
  const secretTargetChanged = destinationSecretTargetChanged(draft, saved);
  const hasSecret = draft.secretAction === 'replace' ? draft.secret.length > 0 : draft.secretAction === 'keep' && Boolean(saved?.HasSecret && !secretTargetChanged);
  if (saved?.HasSecret && secretTargetChanged && draft.secretAction === 'keep') errors.Secret = 'Changing the target address or account requires replacing or clearing the saved secret.';
  if (draft.transport === 'webhook') {
    try {
      const url = new URL(draft.endpoint);
      if (!safeText(draft.endpoint, 2048) || draft.endpoint !== draft.endpoint.trim() || url.protocol !== 'https:' || !url.hostname || url.username || url.password || url.search || url.hash) throw new Error();
    } catch { errors.Endpoint = 'Use an HTTPS receiver URL without credentials, a query, or a fragment.'; }
    if (draft.secretAction === 'replace' && !/^[\x21-\x7e]{16,1024}$/.test(draft.secret)) errors.Secret = 'Use 16 to 1,024 visible ASCII characters without spaces.';
    if (draft.enabled && !hasSecret) errors.Secret = 'An enabled webhook requires a saved or replacement signing secret.';
  } else {
    if (!validMailHost(draft.smtpHost)) errors.SMTPHost = 'Use a mail server hostname or IP address without a scheme, port, or path.';
    if (!/^\d{1,5}$/.test(draft.smtpPort) || Number(draft.smtpPort) < 1 || Number(draft.smtpPort) > 65535) errors.SMTPPort = 'Use a port between 1 and 65535.';
    if (!validEmail(draft.smtpFrom)) errors.SMTPFrom = 'Use one ASCII email address without a display name. International domains can use Punycode.';
    const recipients = draft.smtpTo.split(/\r?\n/).map((value) => value.trim()).filter(Boolean);
    if (!recipients.length || recipients.length > 16 || new Set(recipients.map((value) => value.toLowerCase())).size !== recipients.length || !recipients.every(validEmail)) errors.SMTPTo = 'Use 1 to 16 distinct ASCII email addresses, one per line. International domains can use Punycode.';
    if (!safeText(draft.smtpUsername, 256)) errors.SMTPUsername = 'Use a username of at most 256 UTF-8 bytes without control characters.';
    if (draft.secretAction === 'replace' && (!draft.secret || byteLength(draft.secret) > 1024 || /[\p{Cc}\uD800-\uDFFF]/u.test(draft.secret))) errors.Secret = 'Use a password of at most 1,024 UTF-8 bytes without control characters.';
    if (!draft.smtpUsername && hasSecret) errors.Secret = 'Enter a mail server username or clear the password.';
    if (draft.enabled && draft.smtpUsername && !hasSecret) errors.Secret = 'A mail server username requires a saved or replacement password.';
  }
  const input: NotificationDestinationInput = {
    Revision: saved?.Revision ?? '0', Name: name, Transport: draft.transport, Enabled: draft.enabled,
    EventIds: [...draft.eventIds].sort(), AllowedNetworks: networks,
    Endpoint: draft.transport === 'webhook' ? draft.endpoint : '',
    SMTP: draft.transport === 'smtp' ? { Host: draft.smtpHost, Port: Number(draft.smtpPort), Security: draft.smtpSecurity, Username: draft.smtpUsername, From: draft.smtpFrom, To: draft.smtpTo.split(/\r?\n/).map((value) => value.trim()).filter(Boolean) }
      : { Host: '', Port: 587, Security: 'starttls', Username: '', From: '', To: [] },
    ...(draft.secretAction === 'keep' ? {} : { Secret: draft.secretAction === 'clear' ? '' : draft.secret }),
  };
  return { errors, input: Object.keys(errors).length ? undefined : input };
}

export const notificationDestinationsApi = {
  async list(options: RequestOptions = {}): Promise<NotificationDestinations> {
    return decodeList(await notificationRequest(options, notificationReadTimeout, 'Notification targets did not respond. Refresh to try again.', (signal) => authenticatedRequest<unknown>('/notification-destinations', { signal })));
  },
  async save(id: string | undefined, input: NotificationDestinationInput, options: RequestOptions = {}): Promise<NotificationDestination> {
    return decodeDestination(await notificationRequest(options, notificationWriteTimeout, notificationWriteTimeoutMessage, (signal) => authenticatedRequest<unknown>(`/notification-destinations${id ? `/${encodeURIComponent(id)}` : ''}`, { signal, method: id ? 'PUT' : 'POST', body: input })));
  },
  async remove(id: string, value: string, options: RequestOptions = {}): Promise<void> {
    await notificationRequest(options, notificationWriteTimeout, notificationWriteTimeoutMessage, (signal) => authenticatedRequest<unknown>(`/notification-destinations/${encodeURIComponent(id)}`, { signal, method: 'DELETE', body: { Revision: value } }));
  },
  async test(id: string, value: string, options: RequestOptions = {}): Promise<{ Id: string; State: 'pending' }> {
    const result = await notificationRequest(options, notificationWriteTimeout, notificationWriteTimeoutMessage, (signal) => authenticatedRequest<unknown>(`/notification-destinations/${encodeURIComponent(id)}/test`, { signal, method: 'POST', body: { Revision: value } }));
    if (!record(result) || Object.keys(result).length !== 2 || !safeText(result.Id, 64) || !result.Id || result.State !== 'pending') invalid();
    return result as { Id: string; State: 'pending' };
  },
};
