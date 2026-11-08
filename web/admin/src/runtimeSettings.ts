import { normalizeX264Options } from './x264Options';

export const runtimeFields = ['Network', 'Hardware', 'Threads', 'H264', 'HEVC', 'SoftwareToneMapping', 'VulkanToneMapping', 'Transcoding', 'TranscodingTempPath', 'SoftwareToneMapOptions', 'CodecConfigurations', 'EnableThrottling', 'ExtractionThreadCount', 'CodecProfileLimits', 'SubtitleOptions'] as const;
export type RuntimeField = typeof runtimeFields[number];
export const h264CpuPresets = ['superfast', 'veryfast', 'faster', 'fast', 'medium', 'slow', 'slower', 'veryslow'] as const;
export const hevcCpuPresets = ['ultrafast', ...h264CpuPresets, 'placebo'] as const;
export const cpuPresets = hevcCpuPresets;
export const presetsForCodec = (codec: 'H264' | 'HEVC'): readonly string[] => codec === 'H264' ? h264CpuPresets : hevcCpuPresets;
export interface RuntimeNetwork { BindHost: string; HttpPort: number }
export interface RuntimeHardware { Decode: string; Encode: string; DeviceId: string }
export interface RuntimeQuality { Preset: typeof cpuPresets[number]; RateControl: 'bitrate' | 'capped_crf'; CRF: number; ThreadCount: number | null; X264Options: string }
export const transcodingPolicyFields = ['EnableHevcEncoding', 'EnableHardwareEncoding', 'DownMixAudioBoost', 'EnableSubtitleExtraction', 'EnableOnTheFlyAttachmentExtraction'] as const;
export interface RuntimeTranscodingPolicy { EnableHevcEncoding: boolean; EnableHardwareEncoding: boolean; DownMixAudioBoost: number; EnableSubtitleExtraction: boolean; EnableOnTheFlyAttachmentExtraction: boolean }
export const softwareToneMapAlgorithms = ['Hable', 'Reinhard', 'Mobius'] as const;
export const softwareToneMapSizeLimits = ['None', 'Limit420', 'Limit720', 'Limit1080'] as const;
export interface RuntimeSoftwareToneMapOptions { ToneMapSizeLimit: typeof softwareToneMapSizeLimits[number]; SoftwareToneMapAlgorithm: typeof softwareToneMapAlgorithms[number] }
export const softwareEncoderIds = ['V-E-libx264', 'V-E-libx265'] as const;
export interface RuntimeCodecConfiguration { CodecId: typeof softwareEncoderIds[number]; IsEnabled: boolean; Priority: number }
export const h264ProfileLimits = ['baseline', 'main', 'high'] as const;
export const hevcProfileLimits = ['main', 'main10'] as const;
export interface RuntimeCodecProfileLimits { H264: typeof h264ProfileLimits[number]; HEVC: typeof hevcProfileLimits[number] }
export interface RuntimeSubtitleOptions { AllowAssToText: boolean }
export interface RuntimeValues { Network: RuntimeNetwork; Hardware: RuntimeHardware; Threads: number; H264: RuntimeQuality; HEVC: RuntimeQuality; SoftwareToneMapping: boolean; VulkanToneMapping: boolean; Transcoding: RuntimeTranscodingPolicy; TranscodingTempPath: string; SoftwareToneMapOptions: RuntimeSoftwareToneMapOptions; CodecConfigurations: RuntimeCodecConfiguration[]; EnableThrottling: boolean; ExtractionThreadCount: number; CodecProfileLimits: RuntimeCodecProfileLimits; SubtitleOptions: RuntimeSubtitleOptions }
export interface RuntimeOverrides {
  Network: { BindHost: string | null; HttpPort: number | null } | null;
  Hardware: RuntimeHardware | null; Threads: number | null; H264: RuntimeQuality | null; HEVC: RuntimeQuality | null;
  SoftwareToneMapping: boolean | null; VulkanToneMapping: boolean | null;
  Transcoding: RuntimeTranscodingPolicy | null; TranscodingTempPath: string | null;
  SoftwareToneMapOptions: RuntimeSoftwareToneMapOptions | null;
  CodecConfigurations: RuntimeCodecConfiguration[] | null;
  EnableThrottling: boolean | null;
  ExtractionThreadCount: number | null;
  CodecProfileLimits: RuntimeCodecProfileLimits | null;
  SubtitleOptions: RuntimeSubtitleOptions | null;
}
export type RuntimeUpdate = Partial<RuntimeOverrides>;
type SettingSource = 'database' | 'deployment';
export interface RuntimeSettings {
  Defaults: RuntimeValues; Overrides: RuntimeOverrides; Effective: Omit<RuntimeValues, 'Network' | 'TranscodingTempPath'>;
  Sources: Record<Exclude<RuntimeField, 'Network'>, SettingSource> & { Network: { BindHost: SettingSource; HttpPort: SettingSource } };
  Network: { Desired: RuntimeNetwork; Active: null | { Configured: RuntimeNetwork; BoundHost: string; HttpPort: number; Revision: string }; RestartRequired: boolean; ReconnectURL: string };
  Hardware: { Available: boolean; Code: string; Devices: { DeviceId: string; Label: string; Available: boolean; Code: string }[] };
  TranscodingTempPath: { Desired: string; Active: string | null; RestartRequired: boolean };
  Effects: Record<Exclude<RuntimeField, 'Network' | 'TranscodingTempPath' | 'EnableThrottling' | 'ExtractionThreadCount'>, 'next_admission'> & { Network: 'restart'; TranscodingTempPath: 'restart'; EnableThrottling: 'next_hls_playback'; ExtractionThreadCount: 'next_image_extraction' };
  Applicability: { H264: 'software_h264_output'; HEVC: 'software_hevc_output'; SoftwareToneMapping: 'software_filter'; VulkanToneMapping: 'vulkan_filter'; Transcoding: 'encoding_and_extraction'; TranscodingTempPath: 'cache_root_after_restart'; SoftwareToneMapOptions: 'software_tone_mapping'; CodecConfigurations: 'software_video_encoding'; EnableThrottling: 'file_hls_video_encoding'; ExtractionThreadCount: 'image_extraction'; CodecProfileLimits: 'new_video_encoding'; SubtitleOptions: 'ass_text_negotiation' };
}

const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const fields = (value: unknown, keys: readonly string[]): value is Record<string, unknown> => record(value) && Object.keys(value).length === keys.length && Object.keys(value).every((key) => keys.includes(key));
const integer = (value: unknown, minimum: number, maximum: number): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= minimum && value <= maximum;
const source = (value: unknown) => value === 'database' || value === 'deployment';
const text = (value: unknown): value is string => typeof value === 'string' && !/[\p{Cc}\uD800-\uDFFF]/u.test(value);
const network = (value: unknown): value is RuntimeNetwork => fields(value, ['BindHost', 'HttpPort']) && text(value.BindHost) && integer(value.HttpPort, 0, 65535);
const hardware = (value: unknown): value is RuntimeHardware => fields(value, ['Decode', 'Encode', 'DeviceId']) && text(value.Decode) && text(value.Encode) && text(value.DeviceId);
const quality = (value: unknown, codec: 'H264' | 'HEVC'): value is RuntimeQuality => fields(value, ['Preset', 'RateControl', 'CRF', 'ThreadCount', 'X264Options'])
  && typeof value.Preset === 'string' && presetsForCodec(codec).includes(value.Preset) && typeof value.RateControl === 'string'
  && ['bitrate', 'capped_crf'].includes(value.RateControl) && integer(value.CRF, 1, 51)
  && (value.ThreadCount === null || integer(value.ThreadCount, 0, 16)) && text(value.X264Options) && normalizeX264Options(value.X264Options) !== undefined && (codec === 'H264' || value.X264Options === '');
const transcoding = (value: unknown): value is RuntimeTranscodingPolicy => fields(value, transcodingPolicyFields)
  && transcodingPolicyFields.filter((field) => field !== 'DownMixAudioBoost').every((field) => typeof value[field] === 'boolean')
  && typeof value.DownMixAudioBoost === 'number' && Number.isFinite(value.DownMixAudioBoost) && value.DownMixAudioBoost >= 0.1 && value.DownMixAudioBoost <= 10;
const softwareToneMapOptions = (value: unknown): value is RuntimeSoftwareToneMapOptions => fields(value, ['ToneMapSizeLimit', 'SoftwareToneMapAlgorithm'])
  && softwareToneMapSizeLimits.includes(value.ToneMapSizeLimit as RuntimeSoftwareToneMapOptions['ToneMapSizeLimit'])
  && softwareToneMapAlgorithms.includes(value.SoftwareToneMapAlgorithm as RuntimeSoftwareToneMapOptions['SoftwareToneMapAlgorithm']);
const codecConfigurations = (value: unknown): value is RuntimeCodecConfiguration[] => Array.isArray(value) && value.length === softwareEncoderIds.length
  && softwareEncoderIds.every((id) => value.filter((item: unknown) => record(item) && item.CodecId === id).length === 1)
  && value.every((item: unknown) => fields(item, ['CodecId', 'IsEnabled', 'Priority']) && softwareEncoderIds.includes(item.CodecId as RuntimeCodecConfiguration['CodecId'])
    && typeof item.IsEnabled === 'boolean' && integer(item.Priority, -1000, 1000));
const codecProfileLimits = (value: unknown): value is RuntimeCodecProfileLimits => fields(value, ['H264', 'HEVC'])
  && h264ProfileLimits.includes(value.H264 as RuntimeCodecProfileLimits['H264']) && hevcProfileLimits.includes(value.HEVC as RuntimeCodecProfileLimits['HEVC']);
const subtitleOptions = (value: unknown): value is RuntimeSubtitleOptions => fields(value, ['AllowAssToText']) && typeof value.AllowAssToText === 'boolean';

export function validRuntimeSettings(value: unknown): value is RuntimeSettings {
  if (!fields(value, ['Defaults', 'Overrides', 'Effective', 'Sources', 'Network', 'Hardware', 'Effects', 'Applicability', 'TranscodingTempPath'])
    || !fields(value.Defaults, runtimeFields) || !fields(value.Overrides, runtimeFields) || !fields(value.Effective, runtimeFields.filter((field) => field !== 'Network' && field !== 'TranscodingTempPath'))
    || !fields(value.Sources, runtimeFields) || !fields(value.Sources.Network, ['BindHost', 'HttpPort'])
    || !fields(value.Network, ['Desired', 'Active', 'RestartRequired', 'ReconnectURL']) || !network(value.Network.Desired)
    || typeof value.Network.RestartRequired !== 'boolean' || typeof value.Network.ReconnectURL !== 'string'
    || !fields(value.Hardware, ['Available', 'Code', 'Devices']) || typeof value.Hardware.Available !== 'boolean' || !text(value.Hardware.Code) || !Array.isArray(value.Hardware.Devices) || value.Hardware.Devices.length > 8
    || !fields(value.TranscodingTempPath, ['Desired', 'Active', 'RestartRequired']) || !text(value.TranscodingTempPath.Desired)
    || (value.TranscodingTempPath.Active !== null && !text(value.TranscodingTempPath.Active)) || typeof value.TranscodingTempPath.RestartRequired !== 'boolean'
    || !fields(value.Effects, runtimeFields) || value.Effects.Network !== 'restart' || value.Effects.TranscodingTempPath !== 'restart'
    || value.Effects.EnableThrottling !== 'next_hls_playback' || value.Effects.ExtractionThreadCount !== 'next_image_extraction'
    || !fields(value.Applicability, ['H264', 'HEVC', 'SoftwareToneMapping', 'VulkanToneMapping', 'Transcoding', 'TranscodingTempPath', 'SoftwareToneMapOptions', 'CodecConfigurations', 'EnableThrottling', 'ExtractionThreadCount', 'CodecProfileLimits', 'SubtitleOptions'])
    || value.Applicability.H264 !== 'software_h264_output' || value.Applicability.HEVC !== 'software_hevc_output'
    || value.Applicability.SoftwareToneMapping !== 'software_filter' || value.Applicability.VulkanToneMapping !== 'vulkan_filter'
    || value.Applicability.Transcoding !== 'encoding_and_extraction' || value.Applicability.TranscodingTempPath !== 'cache_root_after_restart'
    || value.Applicability.SoftwareToneMapOptions !== 'software_tone_mapping' || value.Applicability.CodecConfigurations !== 'software_video_encoding'
    || value.Applicability.EnableThrottling !== 'file_hls_video_encoding' || value.Applicability.ExtractionThreadCount !== 'image_extraction'
    || value.Applicability.CodecProfileLimits !== 'new_video_encoding' || value.Applicability.SubtitleOptions !== 'ass_text_negotiation') return false;
  for (const values of [value.Defaults, value.Effective]) {
    if (!hardware(values.Hardware) || !integer(values.Threads, 0, 2147483647) || !quality(values.H264, 'H264') || !quality(values.HEVC, 'HEVC')
      || typeof values.SoftwareToneMapping !== 'boolean' || typeof values.VulkanToneMapping !== 'boolean' || !transcoding(values.Transcoding) || !softwareToneMapOptions(values.SoftwareToneMapOptions)
      || typeof values.EnableThrottling !== 'boolean' || !integer(values.ExtractionThreadCount, 0, 16)
      || !codecConfigurations(values.CodecConfigurations) || !codecProfileLimits(values.CodecProfileLimits) || !subtitleOptions(values.SubtitleOptions)) return false;
  }
  if (!network(value.Defaults.Network) || !text(value.Defaults.TranscodingTempPath)) return false;
  const override = value.Overrides;
  if (override.Network !== null && (!fields(override.Network, ['BindHost', 'HttpPort']) || (override.Network.BindHost !== null && !text(override.Network.BindHost)) || (override.Network.HttpPort !== null && !integer(override.Network.HttpPort, 1, 65535)))) return false;
  if (override.Hardware !== null && (!hardware(override.Hardware) || !['software', 'vaapi'].includes(override.Hardware.Decode) || !['software', 'vaapi'].includes(override.Hardware.Encode))) return false;
  if (override.Threads !== null && !integer(override.Threads, 1, 64)) return false;
  for (const field of ['H264', 'HEVC'] as const) if (override[field] !== null && !quality(override[field], field)) return false;
  for (const field of ['SoftwareToneMapping', 'VulkanToneMapping'] as const) if (override[field] !== null && typeof override[field] !== 'boolean') return false;
  if (override.Transcoding !== null && !transcoding(override.Transcoding) || override.TranscodingTempPath !== null && !text(override.TranscodingTempPath)) return false;
  if (override.SoftwareToneMapOptions !== null && !softwareToneMapOptions(override.SoftwareToneMapOptions)) return false;
  if (override.CodecConfigurations !== null && !codecConfigurations(override.CodecConfigurations)) return false;
  if (override.EnableThrottling !== null && typeof override.EnableThrottling !== 'boolean') return false;
  if (override.ExtractionThreadCount !== null && !integer(override.ExtractionThreadCount, 0, 16)) return false;
  if (override.CodecProfileLimits !== null && !codecProfileLimits(override.CodecProfileLimits)) return false;
  if (override.SubtitleOptions !== null && !subtitleOptions(override.SubtitleOptions)) return false;
  if (!source(value.Sources.Network.BindHost) || !source(value.Sources.Network.HttpPort)) return false;
  if (!source(value.Sources.TranscodingTempPath)) return false;
  for (const field of runtimeFields.filter((field) => field !== 'Network' && field !== 'TranscodingTempPath')) if (!source(value.Sources[field])) return false;
  for (const field of runtimeFields.filter((field) => field !== 'Network' && field !== 'TranscodingTempPath' && field !== 'EnableThrottling' && field !== 'ExtractionThreadCount')) if (value.Effects[field] !== 'next_admission') return false;
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
