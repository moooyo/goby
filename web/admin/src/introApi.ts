import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface IntroInterval {
  StartTicks: number;
  EndTicks: number;
  Provenance: 'Chapter' | 'Manual' | 'Import';
}

export interface ItemIntro {
  ItemId: string;
  MediaSourceId: string;
  SourceRevision: string;
  Revision: string;
  DurationTicks: number;
  Automatic: IntroInterval | null;
  Effective: IntroInterval | null;
  Override: IntroInterval | null;
  OverrideSource: 'Manual' | 'Import' | '';
  OverrideStale: boolean;
  LastEditedBy: string;
  LastEditedAt: string | null;
}

export interface IntroInput {
  Revision: string;
  SourceRevision: string;
  StartTicks: number;
  EndTicks: number;
  Provenance: 'Manual' | 'Import';
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function validInterval(value: unknown, duration?: number): value is IntroInterval {
  if (!record(value)) return false;
  return typeof value.StartTicks === 'number' && Number.isSafeInteger(value.StartTicks) && value.StartTicks >= 0
    && typeof value.EndTicks === 'number' && Number.isSafeInteger(value.EndTicks) && value.EndTicks > value.StartTicks
    && (duration === undefined || value.EndTicks <= duration)
    && ['Chapter', 'Manual', 'Import'].includes(String(value.Provenance));
}

function decode(value: ItemIntro, itemId: string): ItemIntro {
  if (!record(value) || value.ItemId !== itemId || typeof value.MediaSourceId !== 'string' || !value.MediaSourceId
    || typeof value.SourceRevision !== 'string' || !value.SourceRevision || typeof value.Revision !== 'string' || !value.Revision
    || !Number.isSafeInteger(value.DurationTicks) || value.DurationTicks <= 0
    || typeof value.OverrideStale !== 'boolean' || !['Manual', 'Import', ''].includes(value.OverrideSource)
    || typeof value.LastEditedBy !== 'string' || (value.LastEditedAt !== null && typeof value.LastEditedAt !== 'string')
    || (value.Automatic !== null && (!validInterval(value.Automatic, value.DurationTicks) || value.Automatic.Provenance !== 'Chapter'))
    || (value.Effective !== null && !validInterval(value.Effective, value.DurationTicks))
    || (value.Override !== null && (!validInterval(value.Override, value.OverrideStale ? undefined : value.DurationTicks) || value.Override.Provenance === 'Chapter'))
    || (value.Override === null && (value.OverrideSource !== '' || value.OverrideStale))
    || (value.Override !== null && value.Override.Provenance !== value.OverrideSource)) {
    throw new ApiError('The intro response is incomplete. Reload the item before editing.', { code: 'invalid_response' });
  }
  return value;
}

export function ticksToSeconds(ticks: number): string {
  const value = BigInt(ticks);
  const whole = value / 10000000n;
  const fraction = (value % 10000000n).toString().padStart(7, '0').replace(/0+$/, '');
  return fraction ? `${whole}.${fraction}` : whole.toString();
}

export function secondsToTicks(seconds: string): number | undefined {
  if (!/^\d{1,9}(?:\.\d{1,7})?$/.test(seconds)) return undefined;
  const [whole, fraction = ''] = seconds.split('.');
  const ticks = BigInt(whole) * 10000000n + BigInt(fraction.padEnd(7, '0'));
  if (ticks > BigInt(Number.MAX_SAFE_INTEGER)) return undefined;
  return Number(ticks);
}

export function parseIntroImport(text: string, duration: number): IntroInterval {
  let value: unknown;
  try { value = JSON.parse(text); } catch { throw new Error('Choose a JSON file containing one object with StartTicks and EndTicks.'); }
  if (!record(value) || Object.keys(value).some((key) => !['StartTicks', 'EndTicks', 'Provenance'].includes(key))
    || (value.Provenance !== undefined && value.Provenance !== 'Import')
    || !validInterval({ ...value, Provenance: 'Import' }, duration)) {
    throw new Error('The import must contain one interval with integer StartTicks and EndTicks within this media duration. An optional Provenance must be Import.');
  }
  return { StartTicks: value.StartTicks as number, EndTicks: value.EndTicks as number, Provenance: 'Import' };
}

export const introApi = {
  async get(itemId: string, options: RequestOptions = {}): Promise<ItemIntro> {
    return decode(await authenticatedRequest<ItemIntro>(`/items/${encodeURIComponent(itemId)}/intro`, options), itemId);
  },
  async update(itemId: string, input: IntroInput): Promise<ItemIntro> {
    return decode(await authenticatedRequest<ItemIntro>(`/items/${encodeURIComponent(itemId)}/intro`, { method: 'PUT', body: input }), itemId);
  },
  async reset(itemId: string, input: Pick<IntroInput, 'Revision' | 'SourceRevision'>): Promise<ItemIntro> {
    return decode(await authenticatedRequest<ItemIntro>(`/items/${encodeURIComponent(itemId)}/intro`, { method: 'DELETE', body: input }), itemId);
  },
};
