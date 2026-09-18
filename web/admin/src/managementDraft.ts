import type { ManagementSettings } from './api';

export interface ManagementDraft {
  Metadata: ManagementSettings['Metadata'];
  Subtitles: Omit<ManagementSettings['Subtitles'], 'DownloadLanguages'> & { DownloadLanguages: string };
  Tasks: { MaxConcurrent: string; CacheRetentionDays: string; CacheMaxEntries: string };
}

export function managementDraft(settings: ManagementSettings): ManagementDraft {
  return { Metadata: { ...settings.Metadata }, Subtitles: { ...settings.Subtitles, DownloadLanguages: settings.Subtitles.DownloadLanguages.join('\n') }, Tasks: { MaxConcurrent: String(settings.Tasks.MaxConcurrent), CacheRetentionDays: String(settings.Tasks.CacheRetentionDays), CacheMaxEntries: String(settings.Tasks.CacheMaxEntries) } };
}

function parseDraft(draft: ManagementDraft): { value: ManagementSettings; errors: Record<string, string> } {
  const errors: Record<string, string> = {};
  const language = /^[a-z]{2,3}(-[A-Z]{2})?$/;
  if (!language.test(draft.Metadata.PreferredMetadataLanguage)) errors['Management.Metadata.PreferredMetadataLanguage'] = 'Use a language code such as en or en-US.';
  if (!/^[A-Z]{2}$/.test(draft.Metadata.MetadataCountryCode)) errors['Management.Metadata.MetadataCountryCode'] = 'Use a two-letter uppercase country code such as US.';
  const languages = [...new Set(draft.Subtitles.DownloadLanguages.split('\n').map((entry) => entry.trim()).filter(Boolean))];
  if (languages.length > 8 || languages.some((entry) => !/^[a-z]{2}(-[A-Z]{2})?$/.test(entry))) errors['Management.Subtitles.DownloadLanguages'] = 'Enter up to 8 two-letter language codes, one per line, such as en or en-US.';
  const tasks = { MaxConcurrent: 0, CacheRetentionDays: 0, CacheMaxEntries: 0 };
  for (const [field, maximum] of [['MaxConcurrent', 16], ['CacheRetentionDays', 3650], ['CacheMaxEntries', 1000000]] as const) {
    const raw = draft.Tasks[field];
    const value = Number(raw);
    if (!/^\d+$/.test(raw) || !Number.isSafeInteger(value) || value < 1 || value > maximum) errors[`Management.Tasks.${field}`] = `Enter a whole number from 1 to ${maximum.toLocaleString()}.`;
    else tasks[field] = value;
  }
  return { errors, value: { Metadata: { ...draft.Metadata }, Subtitles: { ...draft.Subtitles, DownloadLanguages: languages }, Tasks: tasks } };
}

export function parseManagementDraft(draft: ManagementDraft): { value?: ManagementSettings; errors: Record<string, string> } {
  const { value, errors } = parseDraft(draft);
  return { errors, value: Object.keys(errors).length ? undefined : value };
}

export function managementDraftKey(draft: ManagementDraft): string {
  const { value, errors } = parseDraft(draft);
  return JSON.stringify([
    value.Metadata,
    { ...value.Subtitles, DownloadLanguages: errors['Management.Subtitles.DownloadLanguages'] ? ['invalid', draft.Subtitles.DownloadLanguages] : value.Subtitles.DownloadLanguages },
    ...(['MaxConcurrent', 'CacheRetentionDays', 'CacheMaxEntries'] as const).map((field) => [field, errors[`Management.Tasks.${field}`] ? ['invalid', draft.Tasks[field]] : value.Tasks[field]]),
  ]);
}
