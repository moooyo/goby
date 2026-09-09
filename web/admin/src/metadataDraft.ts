import type { MetadataDetail, MetadataFieldName, MetadataUpdateInput, MetadataValues } from './api';
import type { PersonEntry, ProviderEntry, StringEntry } from './MetadataCollections';
import { metadataCreditTypes } from './metadataCreditTypes';

export const metadataFields: MetadataFieldName[] = [
  'Name', 'SortName', 'Overview', 'OriginalTitle', 'OfficialRating', 'ProductionYear',
  'PremiereDate', 'CommunityRating', 'Genres', 'Tags', 'Studios', 'People',
  'ProviderIds', 'IndexNumber', 'ParentIndexNumber',
];

export interface MetadataDraftValues {
  Name: string;
  SortName: string;
  Overview: string;
  OriginalTitle: string;
  OfficialRating: string;
  ProductionYear: string;
  PremiereDate: string;
  CommunityRating: string;
  Genres: StringEntry[];
  Tags: StringEntry[];
  Studios: StringEntry[];
  People: PersonEntry[];
  ProviderIds: ProviderEntry[];
  IndexNumber: string;
  ParentIndexNumber: string;
}

export type MetadataDraftOverrides = Partial<MetadataDraftValues>;
export type MetadataDraftErrors = Record<string, string>;

const maxEntries = 1024;
const maxTextBytes = 64 * 1024;
const maxRequestBytes = 1024 * 1024;

function textBytes(value: string): number {
  return new TextEncoder().encode(value).length;
}

function checkText(value: string, path: string, errors: MetadataDraftErrors, options: { maximum: number; nonempty?: boolean; trim?: boolean }): void {
  const text = options.trim ? value.trim() : value;
  if (options.nonempty && !text.trim()) errors[path] = 'Enter a value or remove this row.';
  else if (text.includes('\0')) errors[path] = 'Remove the unsupported null character.';
  else if (textBytes(text) > options.maximum) errors[path] = `Use at most ${options.maximum.toLocaleString('en-US')} UTF-8 bytes.`;
}

function canonicalProviderKey(key: string): string {
  const value = key.trim();
  switch (value.toLowerCase()) {
    case 'imdb': return 'Imdb';
    case 'tmdb': return 'Tmdb';
    case 'tvdb': return 'Tvdb';
    default: return value;
  }
}

export function hasOverride(overrides: object, field: string): boolean {
  return Object.hasOwn(overrides, field);
}

export function draftValues(values: MetadataValues): MetadataDraftValues {
  const entries = (field: 'Genres' | 'Tags' | 'Studios'): StringEntry[] => values[field].map((value, index) => ({ id: `${field}-${index}`, value }));
  return {
    Name: values.Name,
    SortName: values.SortName,
    Overview: values.Overview,
    OriginalTitle: values.OriginalTitle,
    OfficialRating: values.OfficialRating,
    ProductionYear: values.ProductionYear === null ? '' : String(values.ProductionYear),
    PremiereDate: values.PremiereDate ?? '',
    CommunityRating: values.CommunityRating === null ? '' : String(values.CommunityRating),
    Genres: entries('Genres'),
    Tags: entries('Tags'),
    Studios: entries('Studios'),
    People: values.People.map((person, index) => ({ ...person, id: `People-${index}`, SortOrder: person.SortOrder === null ? '' : String(person.SortOrder) })),
    ProviderIds: Object.entries(values.ProviderIds).map(([Key, Value], index) => ({ id: `ProviderIds-${index}`, Key, Value })),
    IndexNumber: values.IndexNumber === null ? '' : String(values.IndexNumber),
    ParentIndexNumber: values.ParentIndexNumber === null ? '' : String(values.ParentIndexNumber),
  };
}

export function draftOverrides(detail: MetadataDetail): MetadataDraftOverrides {
  const values = draftValues({ ...detail.Automatic, ...detail.Overrides });
  return Object.fromEntries(metadataFields.filter((field) => hasOverride(detail.Overrides, field)).map((field) => [field, values[field]])) as MetadataDraftOverrides;
}

export function displayedDraft(detail: MetadataDetail, overrides: MetadataDraftOverrides, lockedFields: MetadataFieldName[]): MetadataDraftValues {
  const inactive = new Set(detail.InactiveFields);
  const lockedValues = Object.fromEntries(lockedFields.filter((field) => !inactive.has(field) && hasOverride(detail.LockedValues, field)).map((field) => [field, detail.LockedValues[field]]));
  const activeOverrides = Object.fromEntries(metadataFields.filter((field) => !inactive.has(field) && hasOverride(overrides, field)).map((field) => [field, overrides[field]]));
  const inactiveEffective = Object.fromEntries(detail.InactiveFields.map((field) => [field, detail.Effective[field]]));
  return { ...draftValues({ ...detail.Automatic, ...lockedValues, ...inactiveEffective }), ...activeOverrides };
}

export function metadataDraftKey(overrides: MetadataDraftOverrides, lockedFields: MetadataFieldName[]): string {
  const ordered = Object.fromEntries(metadataFields.filter((field) => hasOverride(overrides, field)).map((field) => [field, overrides[field]]));
  return JSON.stringify({ Overrides: ordered, LockedFields: [...lockedFields].sort() }, (key, value: unknown) => key === 'id' ? undefined : value);
}

function nullableNumber(raw: string, path: string, errors: MetadataDraftErrors, options: { integer?: boolean; min?: number; max?: number } = {}): number | null {
  const text = raw.trim();
  if (!text) return null;
  const syntax = options.integer ? /^-?\d+$/ : /^-?(?:\d+\.?\d*|\.\d+)$/;
  const value = Number(text);
  if (!syntax.test(text) || !Number.isFinite(value) || (options.integer && !Number.isSafeInteger(value))) {
    errors[path] = options.integer ? 'Enter a whole number or leave this empty.' : 'Enter a number or leave this empty.';
  } else if ((options.min !== undefined && value < options.min) || (options.max !== undefined && value > options.max)) {
    errors[path] = `Enter a value from ${options.min} to ${options.max}.`;
  }
  return value;
}

function validUTCDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z$/.test(value) || value.startsWith('0000-')) return false;
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return false;
  return date.toISOString().slice(0, 19) === value.slice(0, 19);
}

export function metadataInput(revision: string, overrides: MetadataDraftOverrides, lockedFields: MetadataFieldName[], source?: MetadataDetail): { input: MetadataUpdateInput; errors: MetadataDraftErrors } {
  const values: Partial<MetadataValues> = {};
  const errors: MetadataDraftErrors = {};
  const inactive = new Set(source?.InactiveFields ?? []);
  const hasActiveOverride = (field: MetadataFieldName) => !inactive.has(field) && hasOverride(overrides, field);
  if (source && inactive.size) {
    const original = draftOverrides(source);
    for (const field of inactive) {
      if (lockedFields.includes(field) && !source.LockedFields.includes(field)) errors[`LockedFields.${field}`] = 'Inactive fields cannot be locked. Remove this saved field or keep its original state.';
      if (!hasOverride(overrides, field)) continue;
      const unchanged = hasOverride(source.Overrides, field)
        && metadataDraftKey({ [field]: overrides[field] }, []) === metadataDraftKey({ [field]: original[field] }, []);
      if (!unchanged) {
        errors[field] = 'Inactive saved values cannot be changed. Use automatic to remove this saved field.';
        continue;
      }
      Object.assign(values, { [field]: structuredClone(source.Overrides[field]) });
    }
  }
  const textFields = ['Name', 'SortName', 'Overview', 'OriginalTitle', 'OfficialRating'] as const;
  for (const field of textFields) {
    if (!hasActiveOverride(field)) continue;
    const value = overrides[field] ?? '';
    values[field] = value;
    checkText(value, field, errors, { maximum: field === 'Name' || field === 'SortName' ? 1024 : maxTextBytes, trim: field === 'Name' || field === 'SortName' });
    if ((field === 'Name' || field === 'SortName') && !value.trim()) errors[field] = field === 'Name' ? 'Enter a title.' : 'Enter a sort title.';
  }
  if (hasActiveOverride('ProductionYear')) values.ProductionYear = nullableNumber(overrides.ProductionYear ?? '', 'ProductionYear', errors, { integer: true, min: 1, max: 9999 });
  if (hasActiveOverride('CommunityRating')) values.CommunityRating = nullableNumber(overrides.CommunityRating ?? '', 'CommunityRating', errors, { min: 0, max: 10 });
  for (const field of ['IndexNumber', 'ParentIndexNumber'] as const) {
    if (!hasActiveOverride(field)) continue;
    const raw = overrides[field] ?? '';
    if (field === 'IndexNumber' && source?.EditableFields.includes(field) && !raw.trim()) {
      errors[field] = 'Enter an episode number from 0 to 2147483647.';
      continue;
    }
    values[field] = nullableNumber(raw, field, errors, { integer: true, min: 0, max: 2147483647 });
  }
  if (hasActiveOverride('PremiereDate')) {
    const value = overrides.PremiereDate?.trim() ?? '';
    values.PremiereDate = value || null;
    if (value && !validUTCDate(value)) errors.PremiereDate = 'Enter a valid premiere date or leave this empty.';
  }
  for (const field of ['Genres', 'Tags', 'Studios'] as const) {
    if (!hasActiveOverride(field)) continue;
    const rows = overrides[field] ?? [];
    if (rows.length > maxEntries) errors[field] = `Use at most ${maxEntries.toLocaleString('en-US')} entries.`;
    values[field] = rows.map((entry, index) => {
      checkText(entry.value, `${field}.${index}`, errors, { maximum: maxTextBytes, nonempty: true, trim: true });
      return entry.value;
    });
  }
  if (hasActiveOverride('People')) {
    const rows = overrides.People ?? [];
    if (rows.length > maxEntries) errors.People = `Use at most ${maxEntries.toLocaleString('en-US')} credits.`;
    values.People = rows.map((person, index) => {
      checkText(person.Name, `People.${index}.Name`, errors, { maximum: maxTextBytes, nonempty: true, trim: true });
      checkText(person.Role, `People.${index}.Role`, errors, { maximum: maxTextBytes });
      checkText(person.Type, `People.${index}.Type`, errors, { maximum: maxTextBytes, nonempty: true, trim: true });
      if (!person.Name.trim()) errors[`People.${index}.Name`] = 'Enter a name or remove this credit.';
      if (!metadataCreditTypes.includes(person.Type)) errors[`People.${index}.Type`] = 'Choose Actor, Director, Writer, Producer, GuestStar, Composer, Conductor, or Lyricist.';
      return {
        Name: person.Name,
        Role: person.Role,
        Type: person.Type,
        SortOrder: nullableNumber(person.SortOrder, `People.${index}.SortOrder`, errors, { integer: true, min: 0, max: 2147483647 }),
      };
    });
  }
  if (hasActiveOverride('ProviderIds')) {
    const rows = overrides.ProviderIds ?? [];
    if (rows.length > maxEntries) errors.ProviderIds = `Use at most ${maxEntries.toLocaleString('en-US')} provider identifiers.`;
    const keys = new Set<string>();
    for (const [index, row] of rows.entries()) {
      const key = canonicalProviderKey(row.Key);
      const value = row.Value.trim();
      if (!/^[A-Za-z0-9]{1,64}$/.test(key)) errors[`ProviderIds.${index}.Key`] = 'Use 1 to 64 letters or digits for the provider name.';
      if (!/^[A-Za-z0-9._:-]{1,256}$/.test(value)) errors[`ProviderIds.${index}.Value`] = 'Use up to 256 letters, digits, or ._:- characters.';
      if (keys.has(key)) errors[`ProviderIds.${index}.Key`] = 'Each provider can have only one identifier.';
      keys.add(key);
    }
    values.ProviderIds = Object.fromEntries(rows.map((row) => [row.Key, row.Value]));
  }
  const input = { Revision: revision, Overrides: values, LockedFields: [...lockedFields] };
  if (textBytes(JSON.stringify(input)) > maxRequestBytes) errors.Overrides = 'These metadata changes exceed the 1 MB limit. Shorten long values or remove entries before saving.';
  return { input, errors };
}

export function metadataSummary(value: MetadataValues[MetadataFieldName] | undefined): string {
  if (value === null || value === undefined || value === '') return 'No value';
  if (Array.isArray(value)) {
    if (!value.length) return 'No entries';
    return value.map((entry) => typeof entry === 'string' ? entry : `${entry.Name}${entry.Role ? ` — ${entry.Role}` : ''}${entry.Type ? ` (${entry.Type})` : ''}${entry.SortOrder === null ? '' : ` · Order ${entry.SortOrder}`}`).join('\n');
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value);
    return entries.length ? entries.map(([key, item]) => `${key}: ${item}`).join('\n') : 'No identifiers';
  }
  return String(value);
}
