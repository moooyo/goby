export const runtimeFields = ['Network', 'Hardware', 'Threads', 'H264', 'HEVC', 'SoftwareToneMapping', 'VulkanToneMapping'] as const;
export type RuntimeField = typeof runtimeFields[number];
export const cpuPresets = ['veryfast', 'fast', 'medium', 'slow'] as const;
export interface RuntimeNetwork { BindHost: string; HttpPort: number }
export interface RuntimeHardware { Decode: string; Encode: string; DeviceId: string }
export interface RuntimeQuality { Preset: typeof cpuPresets[number]; RateControl: 'bitrate' | 'capped_crf'; CRF: number }
export interface RuntimeValues { Network: RuntimeNetwork; Hardware: RuntimeHardware; Threads: number; H264: RuntimeQuality; HEVC: RuntimeQuality; SoftwareToneMapping: boolean; VulkanToneMapping: boolean }
export interface RuntimeOverrides {
  Network: { BindHost: string | null; HttpPort: number | null } | null;
  Hardware: RuntimeHardware | null; Threads: number | null; H264: RuntimeQuality | null; HEVC: RuntimeQuality | null;
  SoftwareToneMapping: boolean | null; VulkanToneMapping: boolean | null;
}
export type RuntimeUpdate = Partial<RuntimeOverrides>;
type SettingSource = 'database' | 'deployment';
export interface RuntimeSettings {
  Defaults: RuntimeValues; Overrides: RuntimeOverrides; Effective: Omit<RuntimeValues, 'Network'>;
  Sources: Record<Exclude<RuntimeField, 'Network'>, SettingSource> & { Network: { BindHost: SettingSource; HttpPort: SettingSource } };
  Network: { Desired: RuntimeNetwork; Active: null | { Configured: RuntimeNetwork; BoundHost: string; HttpPort: number; Revision: string }; RestartRequired: boolean; ReconnectURL: string };
  Hardware: { Available: boolean; Code: string; Devices: { DeviceId: string; Label: string; Available: boolean; Code: string }[] };
  Effects: Record<Exclude<RuntimeField, 'Network'>, 'next_admission'> & { Network: 'restart' };
  Applicability: { H264: 'software_h264_output'; HEVC: 'software_hevc_output'; SoftwareToneMapping: 'software_filter'; VulkanToneMapping: 'vulkan_filter' };
}

const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const fields = (value: unknown, keys: readonly string[]): value is Record<string, unknown> => record(value) && Object.keys(value).length === keys.length && Object.keys(value).every((key) => keys.includes(key));
const integer = (value: unknown, minimum: number, maximum: number): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= minimum && value <= maximum;
const source = (value: unknown) => value === 'database' || value === 'deployment';
const text = (value: unknown): value is string => typeof value === 'string' && !/[\p{Cc}\uD800-\uDFFF]/u.test(value);
const network = (value: unknown): value is RuntimeNetwork => fields(value, ['BindHost', 'HttpPort']) && text(value.BindHost) && integer(value.HttpPort, 0, 65535);
const hardware = (value: unknown): value is RuntimeHardware => fields(value, ['Decode', 'Encode', 'DeviceId']) && text(value.Decode) && text(value.Encode) && text(value.DeviceId);
const quality = (value: unknown): value is RuntimeQuality => fields(value, ['Preset', 'RateControl', 'CRF']) && cpuPresets.includes(value.Preset as RuntimeQuality['Preset']) && ['bitrate', 'capped_crf'].includes(String(value.RateControl)) && integer(value.CRF, 18, 35);

export function validRuntimeSettings(value: unknown): value is RuntimeSettings {
  if (!fields(value, ['Defaults', 'Overrides', 'Effective', 'Sources', 'Network', 'Hardware', 'Effects', 'Applicability'])
    || !fields(value.Defaults, runtimeFields) || !fields(value.Overrides, runtimeFields) || !fields(value.Effective, runtimeFields.filter((field) => field !== 'Network'))
    || !fields(value.Sources, runtimeFields) || !fields(value.Sources.Network, ['BindHost', 'HttpPort'])
    || !fields(value.Network, ['Desired', 'Active', 'RestartRequired', 'ReconnectURL']) || !network(value.Network.Desired)
    || typeof value.Network.RestartRequired !== 'boolean' || typeof value.Network.ReconnectURL !== 'string'
    || !fields(value.Hardware, ['Available', 'Code', 'Devices']) || typeof value.Hardware.Available !== 'boolean' || !text(value.Hardware.Code) || !Array.isArray(value.Hardware.Devices) || value.Hardware.Devices.length > 8
    || !fields(value.Effects, runtimeFields) || value.Effects.Network !== 'restart'
    || !fields(value.Applicability, ['H264', 'HEVC', 'SoftwareToneMapping', 'VulkanToneMapping'])
    || value.Applicability.H264 !== 'software_h264_output' || value.Applicability.HEVC !== 'software_hevc_output'
    || value.Applicability.SoftwareToneMapping !== 'software_filter' || value.Applicability.VulkanToneMapping !== 'vulkan_filter') return false;
  for (const values of [value.Defaults, value.Effective]) {
    if (!hardware(values.Hardware) || !integer(values.Threads, 0, 2147483647) || !quality(values.H264) || !quality(values.HEVC)
      || typeof values.SoftwareToneMapping !== 'boolean' || typeof values.VulkanToneMapping !== 'boolean') return false;
  }
  if (!network(value.Defaults.Network)) return false;
  const override = value.Overrides;
  if (override.Network !== null && (!fields(override.Network, ['BindHost', 'HttpPort']) || (override.Network.BindHost !== null && !text(override.Network.BindHost)) || (override.Network.HttpPort !== null && !integer(override.Network.HttpPort, 1, 65535)))) return false;
  if (override.Hardware !== null && (!hardware(override.Hardware) || !['software', 'vaapi'].includes(override.Hardware.Decode) || !['software', 'vaapi'].includes(override.Hardware.Encode))) return false;
  if (override.Threads !== null && !integer(override.Threads, 1, 64)) return false;
  for (const field of ['H264', 'HEVC'] as const) if (override[field] !== null && !quality(override[field])) return false;
  for (const field of ['SoftwareToneMapping', 'VulkanToneMapping'] as const) if (override[field] !== null && typeof override[field] !== 'boolean') return false;
  if (!source(value.Sources.Network.BindHost) || !source(value.Sources.Network.HttpPort)) return false;
  for (const field of runtimeFields.filter((field) => field !== 'Network')) if (!source(value.Sources[field]) || value.Effects[field] !== 'next_admission') return false;
  const active = value.Network.Active;
  if (active !== null && (!fields(active, ['Configured', 'BoundHost', 'HttpPort', 'Revision']) || !network(active.Configured) || !text(active.BoundHost) || !integer(active.HttpPort, 1, 65535)
    || typeof active.Revision !== 'string' || !/^(?:0|[1-9]\d*)$/.test(active.Revision) || active.Revision.length > 19 || BigInt(active.Revision) > 9223372036854775807n)) return false;
  const ids = new Set<string>();
  for (const device of value.Hardware.Devices) {
    if (!fields(device, ['DeviceId', 'Label', 'Available', 'Code']) || !text(device.DeviceId) || !device.DeviceId || ids.has(device.DeviceId) || !text(device.Label) || typeof device.Available !== 'boolean' || !text(device.Code)) return false;
    ids.add(device.DeviceId);
  }
  return true;
}
