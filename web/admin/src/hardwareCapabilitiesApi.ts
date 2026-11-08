import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface CodecInterface {
  Managed: boolean;
  Compiled: boolean | null;
  Selected: boolean;
  Implementations: string[];
}

export interface CodecEncodingProof {
  DeviceId: string;
  Codec: string;
  Profile: string;
  BitDepth: number;
  Width: number;
  Height: number;
  FrameRate: number;
  Bitrate: number;
  State: 'passed' | 'failed';
  Code: string;
  ExpiresAt: string;
}

export interface CodecCapability {
  Codec: string;
  Name: string;
  SoftwareDecode: CodecInterface;
  SoftwareEncode: CodecInterface;
  HardwareDecode: CodecInterface;
  HardwareEncode: CodecInterface;
  EncodingProofs: CodecEncodingProof[];
}

export interface HardwareCapabilities {
  ObservedAt: string;
  CompiledAt: string | null;
  CompiledState: 'unknown' | 'current' | 'stale';
  FFmpegVersion: string;
  TranscodingAvailable: boolean;
  Selected: { Decode: string; Encode: string; DeviceId: string };
  Devices: { DeviceId: string; Label: string; Available: boolean; Code: string }[];
  Items: CodecCapability[];
}

const record = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);
const fields = (value: unknown, names: string[]): value is Record<string, unknown> => record(value) && Object.keys(value).length === names.length && Object.keys(value).every((key) => names.includes(key));
const text = (value: unknown): value is string => typeof value === 'string' && value.length <= 512 && !/[\p{Cc}\uD800-\uDFFF]/u.test(value);
const date = (value: unknown): value is string => typeof value === 'string' && value.length <= 40 && Number.isFinite(Date.parse(value));
const number = (value: unknown, maximum: number): value is number => typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= maximum;
const integer = (value: unknown, maximum: number) => number(value, maximum) && Number.isSafeInteger(value);

function validInterface(value: unknown): value is CodecInterface {
  return fields(value, ['Managed', 'Compiled', 'Selected', 'Implementations']) && typeof value.Managed === 'boolean'
    && (value.Compiled === null || typeof value.Compiled === 'boolean') && typeof value.Selected === 'boolean'
    && Array.isArray(value.Implementations) && value.Implementations.length <= 8 && value.Implementations.every(text)
    && (value.Managed || !value.Selected);
}

function validProof(value: unknown): value is CodecEncodingProof {
  return fields(value, ['DeviceId', 'Codec', 'Profile', 'BitDepth', 'Width', 'Height', 'FrameRate', 'Bitrate', 'State', 'Code', 'ExpiresAt'])
    && text(value.DeviceId) && text(value.Codec) && text(value.Profile) && integer(value.BitDepth, 16)
    && integer(value.Width, 8192) && integer(value.Height, 8192) && number(value.FrameRate, 240) && integer(value.Bitrate, 200_000_000)
    && (value.State === 'passed' || value.State === 'failed') && text(value.Code) && date(value.ExpiresAt);
}

function decode(value: unknown): HardwareCapabilities {
  if (!fields(value, ['ObservedAt', 'CompiledAt', 'CompiledState', 'FFmpegVersion', 'TranscodingAvailable', 'Selected', 'Devices', 'Items'])
    || !date(value.ObservedAt) || value.CompiledAt !== null && !date(value.CompiledAt)
    || !['unknown', 'current', 'stale'].includes(String(value.CompiledState)) || !text(value.FFmpegVersion) || typeof value.TranscodingAvailable !== 'boolean'
    || !fields(value.Selected, ['Decode', 'Encode', 'DeviceId']) || !text(value.Selected.Decode) || !text(value.Selected.Encode) || !text(value.Selected.DeviceId)
    || !Array.isArray(value.Devices) || value.Devices.length > 8 || !value.Devices.every((device) => fields(device, ['DeviceId', 'Label', 'Available', 'Code'])
      && text(device.DeviceId) && Boolean(device.DeviceId) && text(device.Label) && typeof device.Available === 'boolean' && text(device.Code))
    || !Array.isArray(value.Items) || value.Items.length > 32 || !value.Items.every((item) => fields(item, ['Codec', 'Name', 'SoftwareDecode', 'SoftwareEncode', 'HardwareDecode', 'HardwareEncode', 'EncodingProofs'])
      && text(item.Codec) && Boolean(item.Codec) && text(item.Name) && validInterface(item.SoftwareDecode) && validInterface(item.SoftwareEncode)
      && validInterface(item.HardwareDecode) && validInterface(item.HardwareEncode) && Array.isArray(item.EncodingProofs) && item.EncodingProofs.length <= 128
      && item.EncodingProofs.every((proof) => validProof(proof) && proof.Codec === item.Codec))
    || new Set(value.Items.map((item) => (item as CodecCapability).Codec)).size !== value.Items.length
    || new Set(value.Devices.map((device) => (device as HardwareCapabilities['Devices'][number]).DeviceId)).size !== value.Devices.length
    || value.CompiledState === 'current' && (value.CompiledAt === null || !value.FFmpegVersion)
    || value.CompiledState === 'unknown' && value.CompiledAt !== null
    || value.CompiledState !== 'current' && value.Items.some((item) => ['SoftwareDecode', 'SoftwareEncode', 'HardwareDecode', 'HardwareEncode'].some((field) => (item as unknown as Record<string, CodecInterface>)[field].Compiled !== null))) {
    throw new ApiError('The codec capability response is incomplete. Refresh before using this information.', { code: 'invalid_response' });
  }
  return value as unknown as HardwareCapabilities;
}

export const hardwareCapabilitiesApi = {
  async get(options: RequestOptions = {}) { return decode(await authenticatedRequest<unknown>('/hardware/codecs', options)); },
  async refresh(options: RequestOptions = {}) { return decode(await authenticatedRequest<unknown>('/hardware/codecs/refresh', { ...options, method: 'POST', body: {} })); },
};
