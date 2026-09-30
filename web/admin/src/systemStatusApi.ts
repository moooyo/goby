import { ApiError, authenticatedRequest } from './api';
import type { RequestOptions } from './api';

export interface SystemStatus {
  Timestamp: string;
  UptimeSeconds: number;
  Host: { OS: string; Architecture: string; CPUCount: number };
  CPU: { UsagePercent: number | null; Load1: number | null; Load5: number | null; Load15: number | null };
  Memory: { TotalBytes: number | null; UsedBytes: number | null; CachedBytes: number | null; SwapUsedBytes: number | null };
  Storage: {
    TotalBytes: number | null;
    UsedBytes: number | null;
    Complete: boolean;
    Volumes: { Path: string; TotalBytes: number | null; UsedBytes: number | null; Available: boolean }[];
  };
  Transcoding: { Available: boolean; Active: number | null; Limit: number | null; HardwareActive: number | null; SoftwareActive: number | null };
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function nonnegative(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0;
}
function count(value: unknown): value is number { return nonnegative(value) && Number.isSafeInteger(value); }
function nullable(value: unknown, valid: (value: unknown) => boolean): boolean { return value === null || valid(value); }
function validCapacity(value: Record<string, unknown>): boolean {
  return nullable(value.TotalBytes, count) && nullable(value.UsedBytes, count)
    && !(typeof value.UsedBytes === 'number' && typeof value.TotalBytes === 'number' && value.UsedBytes > value.TotalBytes);
}
function validSystemStatus(value: unknown): value is SystemStatus {
  if (!record(value) || typeof value.Timestamp !== 'string' || !Number.isFinite(Date.parse(value.Timestamp))
    || !nonnegative(value.UptimeSeconds) || !record(value.Host) || !record(value.CPU)
    || !record(value.Memory) || !record(value.Storage) || !record(value.Transcoding)) return false;
  const { Host: host, CPU: cpu, Memory: memory, Storage: storage, Transcoding: transcoding } = value;
  return typeof host.OS === 'string' && host.OS.length > 0
    && typeof host.Architecture === 'string' && host.Architecture.length > 0 && count(host.CPUCount) && host.CPUCount > 0
    && nullable(cpu.UsagePercent, (percent) => nonnegative(percent) && percent <= 100)
    && [cpu.Load1, cpu.Load5, cpu.Load15].every((load) => nullable(load, nonnegative))
    && validCapacity(memory) && nullable(memory.CachedBytes, count) && nullable(memory.SwapUsedBytes, count)
    && validCapacity(storage) && typeof storage.Complete === 'boolean' && Array.isArray(storage.Volumes)
    && storage.Volumes.every((volume) => record(volume) && typeof volume.Path === 'string' && volume.Path.length > 0
      && typeof volume.Available === 'boolean' && validCapacity(volume))
    && typeof transcoding.Available === 'boolean'
    && [transcoding.Active, transcoding.Limit, transcoding.HardwareActive, transcoding.SoftwareActive].every((item) => nullable(item, count));
}

export const systemStatusApi = {
  async getStatus(options: RequestOptions = {}): Promise<SystemStatus> {
    const result = await authenticatedRequest<unknown>('/system/status', options);
    if (!validSystemStatus(result)) throw new ApiError('The server returned an invalid system status response.', { code: 'invalid_response' });
    return result;
  },
};
