import { ApiError, api } from './api';
import type { AuthSession } from '../types';

export interface ViewingStatistics {
  EstimatedContentHours: number;
  EstimatedContentTicks: string;
  IsEstimate: true;
}

function sameSession(expected: AuthSession): boolean {
  const current = api.getSession();
  return current !== null && current.AccessToken === expected.AccessToken && current.User.Id === expected.User.Id && current.serverUrl === expected.serverUrl;
}

export async function viewingStatistics(session: AuthSession, signal?: AbortSignal): Promise<ViewingStatistics> {
  if (!sameSession(session)) throw new ApiError(401, 'session_changed', 'The active account changed before its viewing statistics were loaded.');
  const url = new URL(`${session.serverUrl}/emby/Users/${encodeURIComponent(session.User.Id)}/ViewingStatistics`, window.location.origin);
  const fields = { Client: 'Goby Player', Device: 'Web Browser', DeviceId: api.deviceId, Version: '1.0.0', Token: session.AccessToken };
  const authorization = `Emby ${Object.entries(fields).map(([key, value]) => `${key}="${value.replace(/["\\\r\n]/g, '')}"`).join(', ')}`;
  const response = await fetch(url, { signal, credentials: 'omit', cache: 'no-store', headers: { Accept: 'application/json', 'X-Emby-Authorization': authorization } });
  if (!sameSession(session)) throw new ApiError(401, 'session_changed', 'The active account changed before its viewing statistics were loaded.');
  if (!response.ok) throw new ApiError(response.status, 'viewing_statistics_unavailable', 'Viewing statistics are unavailable.');
  const value: unknown = await response.json();
  if (!sameSession(session)) throw new ApiError(401, 'session_changed', 'The active account changed before its viewing statistics were loaded.');
  if (!value || typeof value !== 'object') throw new ApiError(200, 'invalid_response', 'Viewing statistics are invalid.');
  const result = value as Partial<ViewingStatistics>;
  if (result.IsEstimate !== true || !Number.isSafeInteger(result.EstimatedContentHours) || result.EstimatedContentHours! < 0 ||
    typeof result.EstimatedContentTicks !== 'string' || !/^(0|[1-9][0-9]*)$/.test(result.EstimatedContentTicks) || result.EstimatedContentTicks.length > 64 ||
    (BigInt(result.EstimatedContentTicks) + 18_000_000_000n) / 36_000_000_000n !== BigInt(result.EstimatedContentHours!)) {
    throw new ApiError(200, 'invalid_response', 'Viewing statistics are invalid.');
  }
  return result as ViewingStatistics;
}
