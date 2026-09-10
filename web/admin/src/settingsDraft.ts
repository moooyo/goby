import type { ServerSettings, SettingsField, SettingsOverrides, SettingsValues } from './api';

export const settingsFields: readonly SettingsField[] = [
  'ServerName', 'MaxBitrate', 'MaxWidth', 'MaxHeight', 'MaxAudioChannels',
];

export const settingLabels: Record<SettingsField, string> = {
  ServerName: 'Server name',
  MaxBitrate: 'Maximum bitrate',
  MaxWidth: 'Maximum width',
  MaxHeight: 'Maximum height',
  MaxAudioChannels: 'Maximum audio channels',
};

export interface SettingDraft {
  override: boolean;
  value: string;
}

export type SettingsDraft = Record<SettingsField, SettingDraft>;
type SettingsDraftErrors = Partial<Record<SettingsField, string>>;

const maxNumericInputLength = 64;
const bitsPerMegabit = 1_000_000n;
const maximumBitrate = 1_000_000_000n;

export function formatMbps(bps: number): string {
  if (!Number.isSafeInteger(bps) || bps < 0) {
    throw new RangeError('Bitrate must be a nonnegative safe integer.');
  }
  const bits = BigInt(bps);
  const whole = bits / bitsPerMegabit;
  const fraction = (bits % bitsPerMegabit).toString().padStart(6, '0').replace(/0+$/, '');
  return fraction ? `${whole}.${fraction}` : whole.toString();
}

export function formatSettingValue(field: SettingsField, value: string | number): string {
  switch (field) {
    case 'MaxBitrate': return `${formatMbps(Number(value))} Mbps`;
    case 'MaxWidth':
    case 'MaxHeight': return `${value} px`;
    case 'MaxAudioChannels': return `${value} ${Number(value) === 1 ? 'channel' : 'channels'}`;
    case 'ServerName': return String(value);
  }
}

function fieldDraft(field: SettingsField, defaults: SettingsValues, overrides: SettingsOverrides): SettingDraft {
  const saved = overrides[field];
  const value = saved ?? defaults[field];
  return { override: saved !== null, value: field === 'MaxBitrate' ? formatMbps(Number(value)) : String(value) };
}

export function draftFromSettings(value: ServerSettings): SettingsDraft {
  const create = (field: SettingsField): SettingDraft => fieldDraft(field, value.Defaults, value.Overrides);
  return {
    ServerName: create('ServerName'),
    MaxBitrate: create('MaxBitrate'),
    MaxWidth: create('MaxWidth'),
    MaxHeight: create('MaxHeight'),
    MaxAudioChannels: create('MaxAudioChannels'),
  };
}

function parseMbps(raw: string): number | undefined {
  if (raw.length > maxNumericInputLength) return undefined;
  const text = raw.trim();
  if (!/^(?:\d+(?:\.\d{0,6})?|\.\d{1,6})$/.test(text)) return undefined;
  const [whole, fraction = ''] = text.split('.');
  // Convert decimal digits directly to integer bits per second. Floating-point
  // multiplication or rounding could silently accept a different saved limit.
  const bits = BigInt(whole || '0') * bitsPerMegabit + BigInt(fraction.padEnd(6, '0'));
  return bits >= 1n && bits <= maximumBitrate ? Number(bits) : undefined;
}

function parseWholeNumber(raw: string, maximum: number): number | undefined {
  if (raw.length > maxNumericInputLength) return undefined;
  const text = raw.trim();
  if (!/^\d+$/.test(text)) return undefined;
  const value = Number(text);
  return Number.isSafeInteger(value) && value >= 1 && value <= maximum ? value : undefined;
}

function parseDraft(draft: SettingsDraft): { overrides: SettingsOverrides; errors: SettingsDraftErrors } {
  const overrides: SettingsOverrides = {
    ServerName: null,
    MaxBitrate: null,
    MaxWidth: null,
    MaxHeight: null,
    MaxAudioChannels: null,
  };
  const errors: SettingsDraftErrors = {};
  for (const field of settingsFields) {
    const entry = draft[field];
    if (!entry.override) continue;
    if (field === 'ServerName') {
      // Go strings.TrimSpace follows Unicode White_Space. Preserve the input
      // itself, including allowed surrounding whitespace, in the saved value.
      if (!/[^\p{White_Space}]/u.test(entry.value)) errors[field] = 'Enter a server name.';
      else if (entry.value.includes('\0')) errors[field] = 'Server name cannot contain null characters.';
      else if (/[\uD800-\uDFFF]/u.test(entry.value)) errors[field] = 'Use valid Unicode text.';
      else if (new TextEncoder().encode(entry.value).length > 128) errors[field] = 'Use at most 128 UTF-8 bytes.';
      else overrides.ServerName = entry.value;
      continue;
    }
    const maximum = field === 'MaxAudioChannels' ? 8 : 8192;
    const value = field === 'MaxBitrate' ? parseMbps(entry.value) : parseWholeNumber(entry.value, maximum);
    if (value === undefined) {
      errors[field] = field === 'MaxBitrate'
        ? 'Enter 0.000001 to 1000 Mbps using at most 6 decimal places.'
        : `Enter a whole number from 1 to ${maximum}.`;
    } else {
      overrides[field] = value;
    }
  }
  return { overrides, errors };
}

export function parseSettingsDraft(draft: SettingsDraft): {
  overrides: SettingsOverrides | undefined;
  errors: Partial<Record<SettingsField, string>>;
} {
  const { overrides, errors } = parseDraft(draft);
  return { overrides: Object.keys(errors).length ? undefined : overrides, errors };
}

export function settingsDraftKey(draft: SettingsDraft): string {
  const { overrides, errors } = parseDraft(draft);
  return JSON.stringify(settingsFields.map((field) => {
    if (!draft[field].override) return [field, false];
    // Invalid edits remain distinguishable while valid numeric spelling and
    // inactive cached values do not make an otherwise unchanged draft dirty.
    return errors[field]
      ? [field, true, 'invalid', draft[field].value]
      : [field, true, 'valid', overrides[field]];
  }));
}
