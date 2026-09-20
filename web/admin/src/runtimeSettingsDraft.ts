import { cpuPresets, runtimeFields } from './runtimeSettings';
import type { RuntimeField, RuntimeHardware, RuntimeOverrides, RuntimeQuality, RuntimeSettings, RuntimeUpdate } from './runtimeSettings';

interface OverrideDraft<T> { override: boolean; value: T }
export interface RuntimeDraft {
  baseline: RuntimeOverrides;
  Network: { BindHost: OverrideDraft<string>; HttpPort: OverrideDraft<string> };
  Hardware: OverrideDraft<RuntimeHardware>; Threads: OverrideDraft<string>;
  H264: OverrideDraft<Omit<RuntimeQuality, 'CRF'> & { CRF: string }>;
  HEVC: OverrideDraft<Omit<RuntimeQuality, 'CRF'> & { CRF: string }>;
  SoftwareToneMapping: OverrideDraft<boolean>; VulkanToneMapping: OverrideDraft<boolean>;
}

export function runtimeDraft(settings: RuntimeSettings): RuntimeDraft {
  const chosen = <K extends Exclude<RuntimeField, 'Network'>>(field: K) => settings.Overrides[field] ?? settings.Defaults[field];
  const baseline = structuredClone(settings.Overrides);
  if (baseline.Network?.BindHost === null && baseline.Network.HttpPort === null) baseline.Network = null;
  return {
    baseline,
    Network: {
      BindHost: { override: settings.Overrides.Network?.BindHost != null, value: settings.Network.Desired.BindHost },
      HttpPort: { override: settings.Overrides.Network?.HttpPort != null, value: String(settings.Network.Desired.HttpPort) },
    },
    Hardware: { override: settings.Overrides.Hardware !== null, value: { ...chosen('Hardware') } },
    Threads: { override: settings.Overrides.Threads !== null, value: String(chosen('Threads')) },
    H264: { override: settings.Overrides.H264 !== null, value: { ...chosen('H264'), CRF: String(chosen('H264').CRF) } },
    HEVC: { override: settings.Overrides.HEVC !== null, value: { ...chosen('HEVC'), CRF: String(chosen('HEVC').CRF) } },
    SoftwareToneMapping: { override: settings.Overrides.SoftwareToneMapping !== null, value: chosen('SoftwareToneMapping') },
    VulkanToneMapping: { override: settings.Overrides.VulkanToneMapping !== null, value: chosen('VulkanToneMapping') },
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
  };
  if (draft.Threads.override && overrides.Threads === null) errors['Runtime.Threads'] = 'Enter a whole number from 1 to 64.';
  if (overrides.Hardware) {
    const hardware = overrides.Hardware;
    for (const mode of ['Decode', 'Encode'] as const) if (!['software', 'vaapi'].includes(hardware[mode])) errors[`Runtime.Hardware.${mode}`] = 'Select CPU or AMD VA-API.';
    if ((hardware.Decode === 'vaapi' || hardware.Encode === 'vaapi') && !hardware.DeviceId) errors['Runtime.Hardware.DeviceId'] = 'Select an available AMD device.';
  }
  for (const codec of ['H264', 'HEVC'] as const) {
    const quality = draft[codec];
    overrides[codec] = null;
    if (!quality.override) continue;
    const crf = whole(quality.value.CRF, 18, 35);
    if (crf === undefined) errors[`Runtime.${codec}.CRF`] = 'Enter a whole number from 18 to 35.';
    if (!cpuPresets.includes(quality.value.Preset)) errors[`Runtime.${codec}.Preset`] = 'Select a supported CPU preset.';
    if (!['bitrate', 'capped_crf'].includes(quality.value.RateControl)) errors[`Runtime.${codec}.RateControl`] = 'Select bitrate or capped CRF.';
    overrides[codec] = { ...quality.value, CRF: crf ?? 18 };
  }
  const value: RuntimeUpdate = {};
  for (const field of runtimeFields) {
    if (JSON.stringify(overrides[field]) === JSON.stringify(draft.baseline[field])) continue;
    Object.assign(value, { [field]: overrides[field] });
  }
  return { value: Object.keys(errors).length ? undefined : value, errors };
}

export function runtimeDraftKey(draft: RuntimeDraft): string {
  const parsed = parseRuntimeDraft(draft);
  return JSON.stringify(Object.keys(parsed.errors).length ? { errors: parsed.errors, draft } : parsed.value);
}
