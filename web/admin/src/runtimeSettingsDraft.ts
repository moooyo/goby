import { h264ProfileLimits, hevcProfileLimits, presetsForCodec, runtimeFields, softwareEncoderIds, softwareToneMapAlgorithms, softwareToneMapSizeLimits } from './runtimeSettings';
import type { RuntimeCodecConfiguration, RuntimeField, RuntimeHardware, RuntimeOverrides, RuntimeQuality, RuntimeSettings, RuntimeUpdate } from './runtimeSettings';
import { normalizeX264Options } from './x264Options';

interface OverrideDraft<T> { override: boolean; value: T }
export interface RuntimeQualityDraft extends Omit<RuntimeQuality, 'CRF' | 'ThreadCount'> {
  CRF: string;
  ThreadCount: string;
  ThreadMode: 'inherit' | 'automatic' | 'limited';
}
export interface RuntimeDraft {
  baseline: RuntimeOverrides;
  Network: { BindHost: OverrideDraft<string>; HttpPort: OverrideDraft<string> };
  Hardware: OverrideDraft<RuntimeHardware>; Threads: OverrideDraft<string>;
  H264: OverrideDraft<RuntimeQualityDraft>;
  HEVC: OverrideDraft<RuntimeQualityDraft>;
  SoftwareToneMapping: OverrideDraft<boolean>; VulkanToneMapping: OverrideDraft<boolean>;
  Transcoding: OverrideDraft<Omit<NonNullable<RuntimeOverrides['Transcoding']>, 'DownMixAudioBoost'> & { DownMixAudioBoost: string }>;
  TranscodingTempPath: OverrideDraft<string>;
  SoftwareToneMapOptions: OverrideDraft<NonNullable<RuntimeOverrides['SoftwareToneMapOptions']>>;
  CodecConfigurations: OverrideDraft<(Omit<RuntimeCodecConfiguration, 'Priority'> & { Priority: string })[]>;
  EnableThrottling: OverrideDraft<boolean>;
  ExtractionThreadCount: OverrideDraft<string>;
  CodecProfileLimits: OverrideDraft<NonNullable<RuntimeOverrides['CodecProfileLimits']>>;
  SubtitleOptions: OverrideDraft<NonNullable<RuntimeOverrides['SubtitleOptions']>>;
}

export function runtimeDraft(settings: RuntimeSettings): RuntimeDraft {
  const chosen = <K extends Exclude<RuntimeField, 'Network'>>(field: K) => settings.Overrides[field] ?? settings.Defaults[field];
  const baseline = structuredClone(settings.Overrides);
  if (baseline.Network?.BindHost === null && baseline.Network.HttpPort === null) baseline.Network = null;
  const quality = (codec: 'H264' | 'HEVC'): RuntimeQualityDraft => {
    const value = chosen(codec);
    return { ...value, CRF: String(value.CRF), ThreadCount: String(value.ThreadCount || Math.min(16, Math.max(1, settings.Effective.Threads || 2))),
      ThreadMode: value.ThreadCount === null ? 'inherit' : value.ThreadCount === 0 ? 'automatic' : 'limited' };
  };
  return {
    baseline,
    Network: {
      BindHost: { override: settings.Overrides.Network?.BindHost != null, value: settings.Network.Desired.BindHost },
      HttpPort: { override: settings.Overrides.Network?.HttpPort != null, value: String(settings.Network.Desired.HttpPort) },
    },
    Hardware: { override: settings.Overrides.Hardware !== null, value: { ...chosen('Hardware') } },
    Threads: { override: settings.Overrides.Threads !== null, value: String(chosen('Threads')) },
    H264: { override: settings.Overrides.H264 !== null, value: quality('H264') },
    HEVC: { override: settings.Overrides.HEVC !== null, value: quality('HEVC') },
    SoftwareToneMapping: { override: settings.Overrides.SoftwareToneMapping !== null, value: chosen('SoftwareToneMapping') },
    VulkanToneMapping: { override: settings.Overrides.VulkanToneMapping !== null, value: chosen('VulkanToneMapping') },
    Transcoding: { override: settings.Overrides.Transcoding !== null, value: { ...chosen('Transcoding'), DownMixAudioBoost: String(chosen('Transcoding').DownMixAudioBoost) } },
    TranscodingTempPath: { override: settings.Overrides.TranscodingTempPath !== null, value: chosen('TranscodingTempPath') },
    SoftwareToneMapOptions: { override: settings.Overrides.SoftwareToneMapOptions !== null, value: { ...chosen('SoftwareToneMapOptions') } },
    CodecConfigurations: { override: settings.Overrides.CodecConfigurations !== null, value: chosen('CodecConfigurations').map((value) => ({ ...value, Priority: String(value.Priority) })) },
    EnableThrottling: { override: settings.Overrides.EnableThrottling !== null, value: chosen('EnableThrottling') },
    ExtractionThreadCount: { override: settings.Overrides.ExtractionThreadCount !== null, value: String(chosen('ExtractionThreadCount')) },
    CodecProfileLimits: { override: settings.Overrides.CodecProfileLimits !== null, value: { ...chosen('CodecProfileLimits') } },
    SubtitleOptions: { override: settings.Overrides.SubtitleOptions !== null, value: { ...chosen('SubtitleOptions') } },
  };
}

function whole(raw: string, minimum: number, maximum: number): number | undefined {
  if (!/^\d{1,10}$/.test(raw)) return undefined;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value >= minimum && value <= maximum ? value : undefined;
}

function literalHost(value: string): boolean {
  if (value === '') return true;
  if (value !== value.trim() || !/^[0-9a-f.:]+$/i.test(value)) return false;
  if (!value.includes(':')) return value.split('.').length === 4 && value.split('.').every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255 && String(Number(part)) === part);
  try { return new URL(`http://[${value}]/`).hostname.startsWith('['); } catch { return false; }
}

function audioBoost(raw: string): number | undefined {
  if (raw.length > 32 || !/^(?:\d+(?:\.\d*)?|\.\d+)$/.test(raw)) return undefined;
  const value = Number(raw);
  return Number.isFinite(value) && value >= 0.1 && value <= 10 ? value : undefined;
}

function encoderPriority(raw: string): number | undefined {
  if (!/^-?\d{1,4}$/.test(raw)) return undefined;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value >= -1000 && value <= 1000 ? value : undefined;
}

function temporaryPath(raw: string): boolean {
  return raw === raw.trim() && new TextEncoder().encode(raw).length <= 4096 && !/[\p{Cc}\uD800-\uDFFF]/u.test(raw)
    && (raw === '' || /^(?:\/|[a-z]:[\\/]|\\\\)/i.test(raw));
}

function equalRuntimeValue(first: unknown, second: unknown): boolean {
  if (first === second) return true;
  if (first === null || second === null || typeof first !== 'object' || typeof second !== 'object') return false;
  if (Array.isArray(first) || Array.isArray(second)) return Array.isArray(first) && Array.isArray(second)
    && first.length === second.length && first.every((value, index) => equalRuntimeValue(value, second[index]));
  const keys = Object.keys(first);
  return keys.length === Object.keys(second).length && keys.every((key) => Object.prototype.hasOwnProperty.call(second, key)
    && equalRuntimeValue((first as Record<string, unknown>)[key], (second as Record<string, unknown>)[key]));
}

export function parseRuntimeDraft(draft: RuntimeDraft): { value?: RuntimeUpdate; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const network = draft.Network;
  const port = network.HttpPort.override ? whole(network.HttpPort.value, 1, 65535) : null;
  if (network.BindHost.override && !literalHost(network.BindHost.value)) errors['Runtime.Network.BindHost'] = 'Use an empty value for all interfaces or a literal IPv4 or IPv6 address, without a port or zone.';
  if (port === undefined) errors['Runtime.Network.HttpPort'] = 'Enter a whole number from 1 to 65535.';
  const overrides: RuntimeOverrides = { ...draft.baseline,
    Network: network.BindHost.override || network.HttpPort.override ? { BindHost: network.BindHost.override ? network.BindHost.value : null, HttpPort: port ?? null } : null,
    Hardware: draft.Hardware.override ? { ...draft.Hardware.value } : null,
    Threads: draft.Threads.override ? whole(draft.Threads.value, 1, 64) ?? null : null,
    SoftwareToneMapping: draft.SoftwareToneMapping.override ? draft.SoftwareToneMapping.value : null,
    VulkanToneMapping: draft.VulkanToneMapping.override ? draft.VulkanToneMapping.value : null,
    Transcoding: draft.Transcoding.override ? { ...draft.Transcoding.value, DownMixAudioBoost: audioBoost(draft.Transcoding.value.DownMixAudioBoost) ?? 1 } : null,
    TranscodingTempPath: draft.TranscodingTempPath.override ? draft.TranscodingTempPath.value : null,
    SoftwareToneMapOptions: draft.SoftwareToneMapOptions.override ? { ...draft.SoftwareToneMapOptions.value } : null,
    CodecConfigurations: draft.CodecConfigurations.override ? draft.CodecConfigurations.value.map((value) => ({ ...value, Priority: encoderPriority(value.Priority) ?? 0 })) : null,
    EnableThrottling: draft.EnableThrottling.override ? draft.EnableThrottling.value : null,
    ExtractionThreadCount: draft.ExtractionThreadCount.override ? whole(draft.ExtractionThreadCount.value, 0, 16) ?? null : null,
    CodecProfileLimits: draft.CodecProfileLimits.override ? { ...draft.CodecProfileLimits.value } : null,
    SubtitleOptions: draft.SubtitleOptions.override ? { ...draft.SubtitleOptions.value } : null,
  };
  if (draft.Threads.override && overrides.Threads === null) errors['Runtime.Threads'] = 'Enter a whole number from 1 to 64.';
  if (draft.Transcoding.override && audioBoost(draft.Transcoding.value.DownMixAudioBoost) === undefined) errors['Runtime.Transcoding.DownMixAudioBoost'] = 'Enter a number from 0.1 to 10.';
  if (draft.TranscodingTempPath.override && !temporaryPath(draft.TranscodingTempPath.value)) errors['Runtime.TranscodingTempPath'] = 'Use an absolute directory path without surrounding spaces, or leave empty for the deployment default.';
  if (draft.EnableThrottling.override && typeof draft.EnableThrottling.value !== 'boolean') errors['Runtime.EnableThrottling'] = 'Supply a boolean value.';
  if (draft.ExtractionThreadCount.override && overrides.ExtractionThreadCount === null) errors['Runtime.ExtractionThreadCount'] = 'Enter a whole number from 0 to 16.';
  if (overrides.SoftwareToneMapOptions) {
    if (!softwareToneMapAlgorithms.includes(overrides.SoftwareToneMapOptions.SoftwareToneMapAlgorithm)) errors['Runtime.SoftwareToneMapOptions.SoftwareToneMapAlgorithm'] = 'Select a supported software tone mapping algorithm.';
    if (!softwareToneMapSizeLimits.includes(overrides.SoftwareToneMapOptions.ToneMapSizeLimit)) errors['Runtime.SoftwareToneMapOptions.ToneMapSizeLimit'] = 'Select a supported software tone mapping height limit.';
  }
  if (draft.CodecConfigurations.override) {
    if (draft.CodecConfigurations.value.length !== 2 || softwareEncoderIds.some((id) => draft.CodecConfigurations.value.filter((value) => value.CodecId === id).length !== 1)) errors['Runtime.CodecConfigurations'] = 'Select the supported H.264 and HEVC software encoders.';
    for (const value of draft.CodecConfigurations.value) if (encoderPriority(value.Priority) === undefined) errors[`Runtime.CodecConfigurations.${value.CodecId}.Priority`] = 'Enter a whole number from -1000 to 1000.';
  }
  if (overrides.CodecProfileLimits) {
    if (!h264ProfileLimits.includes(overrides.CodecProfileLimits.H264)) errors['Runtime.CodecProfileLimits.H264'] = 'Select a supported H.264 profile limit.';
    if (!hevcProfileLimits.includes(overrides.CodecProfileLimits.HEVC)) errors['Runtime.CodecProfileLimits.HEVC'] = 'Select a supported HEVC profile limit.';
  }
  if (overrides.SubtitleOptions && typeof overrides.SubtitleOptions.AllowAssToText !== 'boolean') errors['Runtime.SubtitleOptions.AllowAssToText'] = 'Select whether to allow ASS to plain text conversion.';
  if (overrides.Hardware) {
    const hardware = overrides.Hardware;
    for (const mode of ['Decode', 'Encode'] as const) if (!['software', 'vaapi'].includes(hardware[mode])) errors[`Runtime.Hardware.${mode}`] = 'Select CPU or AMD VA-API.';
    if ((hardware.Decode === 'vaapi' || hardware.Encode === 'vaapi') && !hardware.DeviceId) errors['Runtime.Hardware.DeviceId'] = 'Select an available AMD device.';
  }
  for (const codec of ['H264', 'HEVC'] as const) {
    const quality = draft[codec];
    overrides[codec] = null;
    if (!quality.override) continue;
    const crf = whole(quality.value.CRF, 1, 51);
    if (crf === undefined) errors[`Runtime.${codec}.CRF`] = 'Enter a whole number from 1 to 51.';
    if (!presetsForCodec(codec).includes(quality.value.Preset)) errors[`Runtime.${codec}.Preset`] = 'Select a supported CPU preset.';
    if (!['bitrate', 'capped_crf'].includes(quality.value.RateControl)) errors[`Runtime.${codec}.RateControl`] = 'Select bitrate or capped CRF.';
    let threadCount: number | null | undefined = null;
    if (quality.value.ThreadMode === 'automatic') threadCount = 0;
    else if (quality.value.ThreadMode === 'limited') threadCount = quality.value.ThreadCount === '' ? 0 : whole(quality.value.ThreadCount, 0, 16);
    else if (quality.value.ThreadMode !== 'inherit') threadCount = undefined;
    if (threadCount === undefined) errors[`Runtime.${codec}.ThreadCount`] = 'Enter a whole number from 0 to 16. Empty or 0 selects automatic threads.';
    const options = normalizeX264Options(quality.value.X264Options);
    if (options === undefined || codec === 'HEVC' && options !== '') errors[`Runtime.${codec}.X264Options`] = 'Use supported H.264 encoder options only. Check the parameter names and ranges below.';
    // Preserve stored spelling when the group was not edited. Server storage
    // canonicalizes explicit updates without creating a draft on initial load.
    overrides[codec] = { Preset: quality.value.Preset, RateControl: quality.value.RateControl, CRF: crf ?? 1, ThreadCount: threadCount ?? null, X264Options: options === undefined ? '' : quality.value.X264Options };
  }
  const value: RuntimeUpdate = {};
  for (const field of runtimeFields) {
    if (equalRuntimeValue(overrides[field], draft.baseline[field])) continue;
    Object.assign(value, { [field]: overrides[field] });
  }
  return { value: Object.keys(errors).length ? undefined : value, errors };
}

export function runtimeDraftKey(draft: RuntimeDraft): string {
  const parsed = parseRuntimeDraft(draft);
  return JSON.stringify(Object.keys(parsed.errors).length ? { errors: parsed.errors, draft } : parsed.value);
}
