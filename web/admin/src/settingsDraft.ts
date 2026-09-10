import type { ServerNameMode, ServerSettings, SettingsField, SettingsOverrides, SettingsResetField, SettingsUpdateInput, SettingsValues } from './api';

export const settingsFields: readonly SettingsField[] = [
  'ServerName', 'MaxBitrate', 'MaxWidth', 'MaxHeight', 'MaxAudioChannels',
];

export type OutputSettingField = Exclude<SettingsField, 'ServerName'>;
export const outputSettingsFields: readonly OutputSettingField[] = ['MaxBitrate', 'MaxWidth', 'MaxHeight', 'MaxAudioChannels'];
export const settingsResetFields: readonly SettingsResetField[] = [...settingsFields, 'TranscodingMaxWidth'];

export const settingLabels: Record<SettingsResetField, string> = {
  ServerName: 'Server name',
  MaxBitrate: 'Maximum bitrate',
  MaxWidth: 'Maximum width',
  MaxHeight: 'Maximum height',
  MaxAudioChannels: 'Maximum audio channels',
  TranscodingMaxWidth: 'Additional video width limit',
};

export interface SettingDraft {
  override: boolean;
  value: string;
}

export interface ServerNameDraft {
  mode: ServerNameMode;
  value: string;
}

export type SettingsDraft = Record<OutputSettingField, SettingDraft> & {
  ServerName: ServerNameDraft;
  TranscodingMaxWidth: string;
};
type SettingsDraftErrors = Partial<Record<SettingsResetField, string>>;
type SettingsDraftInput = Omit<SettingsUpdateInput, 'Revision'>;

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
    // Preserve the saved host-name representation until the administrator
    // explicitly changes the name choice. Editing another field must not
    // silently replace an unset name with an empty name or a deployment name.
    ServerName: { mode: value.ServerNameMode, value: value.Effective.ServerName },
    MaxBitrate: create('MaxBitrate'),
    MaxWidth: create('MaxWidth'),
    MaxHeight: create('MaxHeight'),
    MaxAudioChannels: create('MaxAudioChannels'),
    TranscodingMaxWidth: String(value.Encoding.TranscodingMaxWidth),
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

function parseWholeNumber(raw: string, maximum: number, minimum = 1): number | undefined {
  if (raw.length > maxNumericInputLength) return undefined;
  const text = raw.trim();
  if (!/^\d+$/.test(text)) return undefined;
  const value = Number(text);
  return Number.isSafeInteger(value) && value >= minimum && value <= maximum ? value : undefined;
}

function parseDraft(draft: SettingsDraft): { input: SettingsDraftInput; errors: SettingsDraftErrors } {
  const overrides: SettingsOverrides = {
    ServerName: null,
    MaxBitrate: null,
    MaxWidth: null,
    MaxHeight: null,
    MaxAudioChannels: null,
  };
  const errors: SettingsDraftErrors = {};
  const name = draft.ServerName;
  if (name.mode === 'custom') {
    // Go strings.TrimSpace follows Unicode White_Space. Preserve the input
    // itself, including allowed surrounding whitespace, in the saved value.
    if (!/[^\p{White_Space}]/u.test(name.value)) errors.ServerName = 'Enter a server name.';
    else if (name.value.includes('\0')) errors.ServerName = 'Server name cannot contain null characters.';
    else if (/[\uD800-\uDFFF]/u.test(name.value)) errors.ServerName = 'Use valid Unicode text.';
    else if (new TextEncoder().encode(name.value).length > 128) errors.ServerName = 'Use at most 128 UTF-8 bytes.';
    else overrides.ServerName = name.value;
  } else if (name.mode === 'empty') overrides.ServerName = '';
  for (const field of outputSettingsFields) {
    const entry = draft[field];
    if (!entry.override) continue;
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
  const additionalWidth = parseWholeNumber(draft.TranscodingMaxWidth, 8192, 0);
  if (additionalWidth === undefined) errors.TranscodingMaxWidth = 'Enter a whole number from 0 to 8192.';
  return { input: { Overrides: overrides, ServerNameMode: name.mode, Encoding: { TranscodingMaxWidth: additionalWidth ?? 0 } }, errors };
}

export function parseSettingsDraft(draft: SettingsDraft): {
  input: SettingsDraftInput | undefined;
  errors: SettingsDraftErrors;
} {
  const { input, errors } = parseDraft(draft);
  return { input: Object.keys(errors).length ? undefined : input, errors };
}

export function settingsDraftKey(draft: SettingsDraft): string {
  const { input, errors } = parseDraft(draft);
  const output = outputSettingsFields.map((field) => {
    if (!draft[field].override) return [field, false];
    // Invalid edits remain distinguishable while valid numeric spelling and
    // inactive cached values do not make an otherwise unchanged draft dirty.
    return errors[field]
      ? [field, true, 'invalid', draft[field].value]
      : [field, true, 'valid', input.Overrides[field]];
  });
  return JSON.stringify([
    ['ServerName', draft.ServerName.mode, errors.ServerName ? ['invalid', draft.ServerName.value] : input.Overrides.ServerName],
    ...output,
    ['TranscodingMaxWidth', errors.TranscodingMaxWidth ? ['invalid', draft.TranscodingMaxWidth] : input.Encoding.TranscodingMaxWidth],
  ]);
}
