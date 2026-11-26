import { adminApi } from './api';
import type { Job, Library } from './api';

// The jobs endpoint returns at most this many recent admissions, with no
// per-library filter or pagination. An absent row need not mean no scan ran.
export const recentScanJobLimit = 1000;

export function indexLatestLibraryScans(libraries: readonly Library[], jobs: readonly Job[], finishedOnly = false): Record<string, Job> {
  const libraryIds = new Set(libraries.map(({ Id }) => Id));
  const latest: Record<string, Job> = Object.create(null);
  for (const job of jobs) {
    if (!libraryIds.has(job.LibraryId) || finishedOnly && !job.FinishedAt) continue;
    const previous = latest[job.LibraryId];
    if (!previous || Date.parse(job.CreatedAt) > Date.parse(previous.CreatedAt)
      || Date.parse(job.CreatedAt) === Date.parse(previous.CreatedAt) && job.Id < previous.Id) {
      latest[job.LibraryId] = job;
    }
  }
  return latest;
}

export async function latestLibraryScans(libraries: readonly Library[], signal?: AbortSignal) {
  if (!libraries.length) return { byLibrary: indexLatestLibraryScans(libraries, []), atLimit: false };
  const response = await adminApi.getJobs({ signal });
  return { byLibrary: indexLatestLibraryScans(libraries, response.Items), atLimit: response.Items.length >= recentScanJobLimit };
}

export function scanFailed(job: Job): boolean {
  return ['failed', 'error', 'interrupted'].includes(job.Status.toLowerCase());
}

export function scanDirectoryFailure(message: string): boolean {
  // Match the native scan result, not a generic substring such as "directory".
  return /^\d+ media directories could not be scanned; existing catalog records were retained(?:\b|\.)/i.test(message)
    || /^(media directory (?:cannot be read|is unavailable)|media directories? (?:could not be read|unreadable))\.?$/i.test(message.trim());
}
