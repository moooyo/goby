import { sortingWordKey } from './sortingSettingsFold.ts';

export interface SortingSettings { SortRemoveWords: string[] }

export const sortingWordsHelp = 'Enter up to 32 words, one per line, with no spaces. Each word can use up to 128 UTF-8 bytes. Leave empty to keep complete names when sorting.';
const sortingWordsError = 'Use at most 32 distinct case-insensitive words, each 1–128 UTF-8 bytes without whitespace or control characters.';

function validWords(value: unknown): value is string[] {
  if (!Array.isArray(value) || value.length > 32) return false;
  const seen = new Set<string>();
  for (const word of value) {
    if (typeof word !== 'string' || word.length === 0 || /[\p{White_Space}\u0000-\u001f\u007f-\u009f\uD800-\uDFFF]/u.test(word)
      || new TextEncoder().encode(word).length > 128) return false;
    const key = sortingWordKey(word);
    if (seen.has(key)) return false;
    seen.add(key);
  }
  return true;
}

export function validSortingSettings(value: unknown): value is SortingSettings {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    && Object.keys(value).length === 1 && 'SortRemoveWords' in value && validWords(value.SortRemoveWords);
}

export function sortingDraft(value?: SortingSettings): string {
  return value?.SortRemoveWords.join('\n') ?? '';
}

export function parseSortingDraft(draft: string): { value?: SortingSettings; error?: string } {
  // Empty lines are separators, but token whitespace is never trimmed or saved differently.
  const words = draft.split(/\r?\n/).filter((word) => word !== '');
  return validWords(words) ? { value: { SortRemoveWords: words } } : { error: sortingWordsError };
}

export function sortingDraftKey(draft: string): string {
  const parsed = parseSortingDraft(draft);
  return JSON.stringify(parsed.value ? ['valid', parsed.value.SortRemoveWords] : ['invalid', draft]);
}
