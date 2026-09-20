import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export const episodeRosterMaximumBytes = 512 * 1024;
export const episodeRosterMaximumEntries = 2000;

export interface EpisodeRosterSourceInput { Key: string; Label: string; Revision: string }
export interface EpisodeRosterSource extends EpisodeRosterSourceInput { Kind: 'admin_import'; ParserVersion: 1; SHA256: string }
export interface EpisodeRosterEntryInput { Key: string; SeasonNumber: number; EpisodeNumber: number; Name: string; PremiereDate?: string }
export interface EpisodeRosterEntry extends EpisodeRosterEntryInput {
  Id: string; AvailableItemIds: string[]; AvailableItemCount: number; Availability: 'available' | 'missing'; Airing: 'unknown' | 'aired' | 'unaired';
}
export interface EpisodeRoster {
  SeriesId: string; SeriesName: string; Revision: string; State: 'absent' | 'active' | 'withdrawn';
  Source: EpisodeRosterSource | null; Entries: EpisodeRosterEntry[]; RetiredCount: number;
}
export interface EpisodeRosterImport { Source: EpisodeRosterSourceInput; Entries: EpisodeRosterEntryInput[] }
export interface EpisodeRosterInput extends EpisodeRosterImport { Revision: string }
export interface EpisodeRosterDraft { key: string; label: string; sourceRevision: string; entries: string }

const sourceKeys = ['Key', 'Label', 'Revision'];
const entryKeys = ['Key', 'SeasonNumber', 'EpisodeNumber', 'Name', 'PremiereDate'];
const bytes = (value: string) => new TextEncoder().encode(value).length;
const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const knownKeys = (value: Record<string, unknown>, allowed: string[]) => Object.keys(value).every((key) => allowed.includes(key));

function plainText(value: unknown, maximum: number, required = false): value is string {
  if (typeof value !== 'string' || (required && value.length === 0) || value.trim() !== value || /\p{Cc}/u.test(value) || bytes(value) > maximum) return false;
  for (const character of value) {
    const code = character.codePointAt(0)!;
    if (code >= 0xd800 && code <= 0xdfff) return false;
  }
  return true;
}

function validDate(value: unknown): value is string {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const [year, month, day] = value.split('-').map(Number);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return year >= 1 && month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1];
}

function validSource(value: unknown): value is EpisodeRosterSourceInput {
  return record(value) && plainText(value.Key, 128, true) && plainText(value.Label, 256) && plainText(value.Revision, 128, true);
}

function validEntry(value: unknown): value is EpisodeRosterEntryInput {
  return record(value) && plainText(value.Key, 128, true) && plainText(value.Name, 512)
    && Number.isInteger(value.SeasonNumber) && Number(value.SeasonNumber) >= 0 && Number(value.SeasonNumber) <= 9999
    && Number.isInteger(value.EpisodeNumber) && Number(value.EpisodeNumber) >= 0 && Number(value.EpisodeNumber) <= 9999
    && (!Object.hasOwn(value, 'PremiereDate') || validDate(value.PremiereDate));
}

function validRevision(value: unknown): value is string {
  return typeof value === 'string' && /^(?:0|[1-9]\d*)$/.test(value) && value.length <= 19 && BigInt(value) <= 9223372036854775807n;
}

// Check property names before JSON.parse can silently replace a duplicate.
// The bounded input is already valid JSON, so only structural tokens are needed.
function parseStrictJSON(text: string): unknown {
  if (bytes(text) > episodeRosterMaximumBytes) throw new Error('The roster JSON must be at most 512 KiB.');
  let value: unknown;
  try { value = JSON.parse(text); } catch { throw new Error('Enter valid JSON before reviewing the roster.'); }
  const containers: (Set<string> | null)[] = [];
  const tokens = /"(?:\\.|[^"\\])*"|[{}\[\],:]/g;
  for (const match of text.matchAll(tokens)) {
    const token = match[0];
    if (token === '{' || token === '[') {
      containers.push(token === '{' ? new Set<string>() : null);
      if (containers.length > 8) throw new Error('The roster JSON contains unsupported nested data.');
    } else if (token === '}' || token === ']') containers.pop();
    else if (token.startsWith('"')) {
      let after = match.index! + token.length;
      while (/\s/.test(text[after] ?? '') && after < text.length) after += 1;
      if (text[after] !== ':') continue;
      const keys = containers.at(-1);
      const key = JSON.parse(token) as string;
      if (keys?.has(key)) throw new Error(`The roster JSON repeats the property ${key}.`);
      keys?.add(key);
    }
  }
  return value;
}

function checkedEntries(value: unknown): EpisodeRosterEntryInput[] {
  if (!Array.isArray(value) || value.length > episodeRosterMaximumEntries) throw new Error('Entries must be an array containing at most 2,000 episodes.');
  const keys = new Set<string>();
  const positions = new Set<string>();
  for (const [index, entry] of value.entries()) {
    if (!validEntry(entry) || !knownKeys(entry as unknown as Record<string, unknown>, entryKeys)) {
      throw new Error(`Entry ${index + 1} must contain Key, SeasonNumber, EpisodeNumber, and Name. Use numbers from 0 to 9999 and an optional real YYYY-MM-DD PremiereDate. Text must have no surrounding whitespace or control characters; Key is limited to 128 UTF-8 bytes and Name to 512.`);
    }
    if (keys.has(entry.Key)) throw new Error(`Entry ${index + 1} repeats an episode Key.`);
    const position = `${entry.SeasonNumber}:${entry.EpisodeNumber}`;
    if (positions.has(position)) throw new Error(`Entry ${index + 1} repeats a season and episode number.`);
    keys.add(entry.Key); positions.add(position);
  }
  return value as EpisodeRosterEntryInput[];
}

export function episodeRosterDraft(roster: EpisodeRoster): EpisodeRosterDraft {
  return {
    key: roster.Source?.Key ?? '', label: roster.Source?.Label ?? '', sourceRevision: roster.Source?.Revision ?? '',
    entries: entriesText(roster.Entries.map(({ Key, SeasonNumber, EpisodeNumber, Name, PremiereDate }) => ({ Key, SeasonNumber, EpisodeNumber, Name, ...(PremiereDate === undefined ? {} : { PremiereDate }) }))),
  };
}

function entriesText(entries: EpisodeRosterEntryInput[]): string {
  const pretty = JSON.stringify(entries, null, 2);
  return bytes(pretty) <= episodeRosterMaximumBytes ? pretty : JSON.stringify(entries);
}

export function parseEpisodeRosterImport(text: string): EpisodeRosterDraft {
  const value = parseStrictJSON(text);
  if (!record(value) || !knownKeys(value, ['Source', 'Entries']) || !validSource(value.Source)
    || !knownKeys(value.Source as unknown as Record<string, unknown>, sourceKeys)) {
    throw new Error('Import an object containing Source {Key, Label, Revision} and Entries. Source Key and Revision are required and limited to 128 UTF-8 bytes; Label is limited to 256. Text must have no surrounding whitespace or control characters.');
  }
  const entries = checkedEntries(value.Entries);
  return { key: value.Source.Key, label: value.Source.Label, sourceRevision: value.Source.Revision, entries: entriesText(entries) };
}

export function validateEpisodeRosterDraft(draft: EpisodeRosterDraft, revision: string): { errors: Record<string, string>; input?: EpisodeRosterInput } {
  const errors: Record<string, string> = {};
  if (!plainText(draft.key, 128, true)) errors['Source.Key'] = 'Enter a source key of 1 to 128 UTF-8 bytes without surrounding whitespace or control characters.';
  if (!plainText(draft.label, 256)) errors['Source.Label'] = 'Use at most 256 UTF-8 bytes without surrounding whitespace or control characters.';
  if (!plainText(draft.sourceRevision, 128, true)) errors['Source.Revision'] = 'Enter a source version of 1 to 128 UTF-8 bytes without surrounding whitespace or control characters.';
  let entries: EpisodeRosterEntryInput[] | undefined;
  try { entries = checkedEntries(parseStrictJSON(draft.entries)); } catch (cause) { errors.Entries = cause instanceof Error ? cause.message : 'The entries could not be read.'; }
  if (Object.keys(errors).length > 0 || !entries) return { errors };
  const input = { Revision: revision, Source: { Key: draft.key, Label: draft.label, Revision: draft.sourceRevision }, Entries: entries };
  if (bytes(JSON.stringify(input)) > episodeRosterMaximumBytes) return { errors: { Entries: 'The complete roster, including its source, must be at most 512 KiB.' } };
  return { errors, input };
}

function decode(value: EpisodeRoster, seriesId: string): EpisodeRoster {
  let valid = record(value) && value.SeriesId === seriesId && typeof value.SeriesName === 'string' && validRevision(value.Revision)
    && ['absent', 'active', 'withdrawn'].includes(value.State) && Number.isSafeInteger(value.RetiredCount) && value.RetiredCount >= 0
    && Array.isArray(value.Entries) && value.Entries.length <= episodeRosterMaximumEntries;
  if (valid) {
    valid = value.State === 'absent' ? value.Revision === '0' && value.Source === null && value.Entries.length === 0 && value.RetiredCount === 0
      : value.Revision !== '0' && validSource(value.Source) && value.Source.Kind === 'admin_import' && value.Source.ParserVersion === 1
        && /^[a-f0-9]{64}$/.test(value.Source.SHA256) && (value.State !== 'withdrawn' || value.Entries.length === 0);
  }
  const keys = new Set<string>(); const positions = new Set<string>(); const ids = new Set<string>();
  if (valid) for (const entry of value.Entries) {
    if (!validEntry(entry)) { valid = false; break; }
    const position = `${entry.SeasonNumber}:${entry.EpisodeNumber}`;
    if (typeof entry.Id !== 'string' || !entry.Id || ids.has(entry.Id) || keys.has(entry.Key) || positions.has(position)
      || !Array.isArray(entry.AvailableItemIds) || entry.AvailableItemIds.length > 8 || !entry.AvailableItemIds.every((id) => typeof id === 'string' && id.length > 0)
      || !Number.isSafeInteger(entry.AvailableItemCount) || entry.AvailableItemCount < entry.AvailableItemIds.length
      || new Set(entry.AvailableItemIds).size !== entry.AvailableItemIds.length
      || !['available', 'missing'].includes(entry.Availability) || !['unknown', 'aired', 'unaired'].includes(entry.Airing)
      || (entry.Availability === 'available') !== (entry.AvailableItemIds.length > 0)
      || (entry.Availability === 'available') !== (entry.AvailableItemCount > 0)
      || (entry.Airing === 'unknown') !== (entry.PremiereDate === undefined)) { valid = false; break; }
    ids.add(entry.Id); keys.add(entry.Key); positions.add(position);
  }
  if (!valid) throw new ApiError('The episode roster response is incomplete. Reload before editing.', { code: 'invalid_response' });
  return value;
}

const pathFor = (seriesId: string) => `/series/${encodeURIComponent(seriesId)}/episode-roster`;
export const episodeRosterApi = {
  async get(seriesId: string, options: RequestOptions = {}): Promise<EpisodeRoster> { return decode(await authenticatedRequest<EpisodeRoster>(pathFor(seriesId), options), seriesId); },
  async update(seriesId: string, input: EpisodeRosterInput): Promise<EpisodeRoster> { return decode(await authenticatedRequest<EpisodeRoster>(pathFor(seriesId), { method: 'PUT', body: input }), seriesId); },
  async withdraw(seriesId: string, revision: string): Promise<EpisodeRoster> { return decode(await authenticatedRequest<EpisodeRoster>(pathFor(seriesId), { method: 'DELETE', body: { Revision: revision } }), seriesId); },
};
