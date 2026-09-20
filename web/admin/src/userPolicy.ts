import type { UserPolicy } from './api';

export const accessDays = ['Everyday', 'Weekday', 'Weekend', 'Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'] as const;
export const unratedCategories = ['Movie', 'Trailer', 'Series', 'Music', 'Game', 'Book', 'LiveTvChannel', 'LiveTvProgram', 'ChannelContent', 'Other'] as const;
export const editableUnratedCategories = ['Movie', 'Trailer', 'Series', 'Music', 'Other'] as const;
export const policyBooleanDefaults = {
  IsHidden: false, IsHiddenRemotely: false, IsHiddenFromUnusedDevices: false,
  AllowTagOrRating: false, IsTagBlockingModeInclusive: false, EnableUserPreferenceAccess: true,
  EnableRemoteControlOfOtherUsers: false, EnableSharedDeviceControl: false, EnableRemoteAccess: true,
  EnableContentDeletion: false, EnableContentDownloading: true, EnableSubtitleDownloading: true,
  EnableSubtitleManagement: false, EnableAllDevices: true,
} as const;
export const policyListFields = ['BlockedTags', 'IncludeTags', 'BlockUnratedItems', 'RestrictedFeatures', 'EnableContentDeletionFromFolders', 'ExcludedSubFolders', 'EnabledDevices'] as const;
export const policyNumberFields = ['AutoRemoteQuality', 'RemoteClientBitrateLimit', 'SimultaneousStreamLimit'] as const;
type PolicyNumberField = typeof policyNumberFields[number];
type LegacyPolicyField = 'EnableAllFolders' | 'EnabledFolders' | 'EnableMediaPlayback' | 'EnablePlaybackRemuxing' | 'EnableAudioPlaybackTranscoding' | 'EnableVideoPlaybackTranscoding';
type LegacyUserPolicy = Pick<UserPolicy, LegacyPolicyField> & Partial<Omit<UserPolicy, LegacyPolicyField>>;
export type UserPolicyDraft = Omit<UserPolicy, PolicyNumberField | 'MaxParentalRating' | 'AccessSchedules'> & Record<PolicyNumberField, string> & {
  MaxParentalRating: string;
  AccessSchedules: { DayOfWeek: string; StartHour: string; EndHour: string }[];
};

export function normalizeUserPolicy(policy: LegacyUserPolicy): UserPolicy {
  const normalized: UserPolicy = {
    ...policyBooleanDefaults, MaxParentalRating: null, AutoRemoteQuality: 0, RemoteClientBitrateLimit: 0, SimultaneousStreamLimit: 0,
    BlockedTags: [], IncludeTags: [], BlockUnratedItems: [], RestrictedFeatures: [], EnableContentDeletionFromFolders: [],
    ExcludedSubFolders: [], EnabledDevices: [], AccessSchedules: [],
    EnableAllFolders: policy.EnableAllFolders, EnabledFolders: [...policy.EnabledFolders],
    EnableMediaPlayback: policy.EnableMediaPlayback, EnablePlaybackRemuxing: policy.EnablePlaybackRemuxing,
    EnableAudioPlaybackTranscoding: policy.EnableAudioPlaybackTranscoding, EnableVideoPlaybackTranscoding: policy.EnableVideoPlaybackTranscoding,
  };
  for (const field of Object.keys(policyBooleanDefaults) as (keyof typeof policyBooleanDefaults)[]) normalized[field] = policy[field] ?? policyBooleanDefaults[field];
  for (const field of policyNumberFields) normalized[field] = policy[field] ?? 0;
  normalized.MaxParentalRating = policy.MaxParentalRating ?? null;
  for (const field of policyListFields) normalized[field] = [...(policy[field] ?? [])];
  normalized.AccessSchedules = (policy.AccessSchedules ?? []).map(({ DayOfWeek, StartHour, EndHour }) => ({ DayOfWeek, StartHour, EndHour }));
  return normalized;
}

export function draftFromUserPolicy(policy: UserPolicy): UserPolicyDraft {
  const normalized = normalizeUserPolicy(policy);
  return {
    ...normalized, MaxParentalRating: normalized.MaxParentalRating === null ? '' : String(normalized.MaxParentalRating),
    AutoRemoteQuality: String(normalized.AutoRemoteQuality), RemoteClientBitrateLimit: String(normalized.RemoteClientBitrateLimit),
    SimultaneousStreamLimit: String(normalized.SimultaneousStreamLimit),
    AccessSchedules: normalized.AccessSchedules.map((schedule) => ({ ...schedule, StartHour: String(schedule.StartHour), EndHour: String(schedule.EndHour) })),
  };
}

function policyInteger(raw: string): number | undefined {
  const text = raw.trim();
  const value = Number(text);
  return text.length <= 64 && /^\d+$/.test(text) && Number.isSafeInteger(value) && value <= 2147483647 ? value : undefined;
}

function policyHour(raw: string): number | undefined {
  const text = raw.trim();
  const value = Number(text);
  return text.length <= 64 && /^(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(text) && Number.isFinite(value) && value <= 24 ? value : undefined;
}

function trimPolicyIdentifier(value: string): string {
  return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '');
}

function validPolicyIdentifier(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value === trimPolicyIdentifier(value)
    && !/[\p{Cc}\uD800-\uDFFF]/u.test(value) && new TextEncoder().encode(value).length <= 256;
}

export function parseUserPolicyDraft(draft: UserPolicyDraft): { policy?: UserPolicy; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const numbers = { AutoRemoteQuality: 0, RemoteClientBitrateLimit: 0, SimultaneousStreamLimit: 0 };
  for (const field of policyNumberFields) {
    const value = policyInteger(draft[field]);
    if (value === undefined) errors[`Policy.${field}`] = 'Enter a whole number from 0 to 2147483647.';
    else numbers[field] = value;
  }
  const parentalRating = draft.MaxParentalRating.trim() === '' ? null : policyInteger(draft.MaxParentalRating);
  if (parentalRating === undefined) errors['Policy.MaxParentalRating'] = 'Leave blank or enter a whole number from 0 to 2147483647.';
  const schedules = draft.AccessSchedules.map((schedule) => {
    const start = policyHour(schedule.StartHour);
    const end = policyHour(schedule.EndHour);
    if (!accessDays.includes(schedule.DayOfWeek as typeof accessDays[number]) || start === undefined || end === undefined || start >= end) {
      errors['Policy.AccessSchedules'] = 'Each interval needs a supported day and hours satisfying 0 <= start < end <= 24.';
    }
    return { DayOfWeek: schedule.DayOfWeek, StartHour: start ?? 0, EndHour: end ?? 0 };
  });
  if (schedules.length > 256) errors['Policy.AccessSchedules'] = 'Use at most 256 access intervals.';
  const policy: UserPolicy = { ...draft, ...numbers, MaxParentalRating: parentalRating ?? null, AccessSchedules: schedules };
  for (const field of ['EnabledFolders', ...policyListFields] as const) {
    const entries = [...new Set(draft[field].map(trimPolicyIdentifier).filter(Boolean))].sort();
    if (entries.length > 256 || !entries.every(validPolicyIdentifier)) errors[`Policy.${field}`] = 'Use at most 256 values, each at most 256 UTF-8 bytes without control characters.';
    policy[field] = entries;
  }
  if (policy.BlockUnratedItems.some((value) => !unratedCategories.includes(value as typeof unratedCategories[number]))) errors['Policy.BlockUnratedItems'] = 'Choose supported unrated content categories.';
  return { policy: Object.keys(errors).length ? undefined : policy, errors };
}

export function validExpandedPolicy(policy: Record<string, unknown>): boolean {
  const number = (value: unknown) => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 && value <= 2147483647;
  if (Object.keys(policyBooleanDefaults).some((field) => policy[field] !== undefined && typeof policy[field] !== 'boolean')) return false;
  if (policyNumberFields.some((field) => policy[field] !== undefined && !number(policy[field]))) return false;
  if (policy.MaxParentalRating !== undefined && policy.MaxParentalRating !== null && !number(policy.MaxParentalRating)) return false;
  if (['EnabledFolders', ...policyListFields].some((field) => policy[field] !== undefined && (!Array.isArray(policy[field]) || (policy[field] as unknown[]).length > 256 || !(policy[field] as unknown[]).every(validPolicyIdentifier)))) return false;
  if (Array.isArray(policy.BlockUnratedItems) && policy.BlockUnratedItems.some((value) => !unratedCategories.includes(value as typeof unratedCategories[number]))) return false;
  if (policy.AccessSchedules !== undefined && (!Array.isArray(policy.AccessSchedules) || policy.AccessSchedules.length > 256 || !policy.AccessSchedules.every((value: unknown) => {
    if (!value || typeof value !== 'object' || Array.isArray(value) || Object.keys(value).length !== 3) return false;
    const schedule = value as Record<string, unknown>;
    return accessDays.includes(schedule.DayOfWeek as typeof accessDays[number]) && typeof schedule.StartHour === 'number' && Number.isFinite(schedule.StartHour)
      && typeof schedule.EndHour === 'number' && Number.isFinite(schedule.EndHour) && schedule.StartHour >= 0 && schedule.EndHour <= 24 && schedule.StartHour < schedule.EndHour;
  }))) return false;
  return true;
}
