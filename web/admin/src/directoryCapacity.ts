import type { LibraryStorageDirectory } from './api';

const uint64Maximum = 18446744073709551615n;
const byteUnits = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];

export interface DirectoryCapacity {
  total: bigint | null;
  used: bigint | null;
  available: bigint | null;
  percent: number | null;
  warning: boolean;
  complete: boolean;
}

export function capacityBytes(value: string | null | undefined): bigint | null {
  if (typeof value !== 'string' || !/^(0|[1-9]\d{0,19})$/.test(value)) return null;
  const parsed = BigInt(value);
  return parsed <= uint64Maximum ? parsed : null;
}

export function directoryCapacity(directory?: Pick<LibraryStorageDirectory, 'TotalBytes' | 'UsedBytes' | 'AvailableBytes'>): DirectoryCapacity {
  const total = capacityBytes(directory?.TotalBytes);
  const used = capacityBytes(directory?.UsedBytes);
  const available = capacityBytes(directory?.AvailableBytes);
  const hasRatio = total != null && used != null && total > 0n && used <= total;
  return {
    total, used, available,
    // Keep comparisons exact and truncate the displayed ratio so 89.99% never reads as 90%.
    percent: hasRatio ? Number(used * 1000n / total) / 10 : null,
    warning: hasRatio && used * 10n >= total * 9n,
    complete: total != null && used != null && available != null,
  };
}

function capacityUnit(value: bigint): number {
  let unit = 0;
  while (unit < byteUnits.length - 1 && value >= 1024n ** BigInt(unit + 1)) unit += 1;
  return unit;
}

export function directoryByteParts(value: bigint | null, locale: string, unit = value == null ? 0 : capacityUnit(value)): { value: string; unit: string } {
  if (value == null) return { value: '—', unit: '' };
  const divisor = 1024n ** BigInt(unit);
  const tenths = (value * 10n + divisor / 2n) / divisor;
  return { value: new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(Number(tenths) / 10), unit: byteUnits[unit] };
}

export function formatDirectoryBytes(value: bigint | null, locale: string): string {
  const parts = directoryByteParts(value, locale);
  return value == null ? parts.value : `${parts.value} ${parts.unit}`;
}

export function formatDirectoryCapacity(capacity: DirectoryCapacity, locale: string): string {
  const { used, total } = capacity;
  if (used == null || total == null) return `${formatDirectoryBytes(used, locale)} / ${formatDirectoryBytes(total, locale)}`;
  const unit = capacityUnit(total);
  const usedParts = directoryByteParts(used, locale, unit);
  const totalParts = directoryByteParts(total, locale, unit);
  return `${usedParts.value} / ${totalParts.value} ${totalParts.unit}`;
}

export function lowestAvailableDirectory(directories: readonly LibraryStorageDirectory[]): LibraryStorageDirectory | undefined {
  let result: LibraryStorageDirectory | undefined;
  let minimum: bigint | null = null;
  for (const directory of directories) {
    const available = capacityBytes(directory.AvailableBytes);
    // AvailableBytes includes reserved space semantics; TotalBytes - UsedBytes does not.
    if (available != null && (minimum == null || available < minimum)) {
      result = directory;
      minimum = available;
    }
  }
  return result;
}

export function directoryReadAccess(directory?: LibraryStorageDirectory): boolean | null {
  if (!directory) return null;
  if (!directory.Available || directory.Readable === false) return false;
  return directory.Readable;
}

export function uniqueDirectoryPaths(paths: readonly string[]): string[] {
  // Only identical registered paths are duplicates. Do not infer mount or volume identity.
  return [...new Set(paths)];
}
