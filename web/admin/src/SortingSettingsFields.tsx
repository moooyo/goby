import { useEffect, useState } from 'react';
import { Autocomplete, Chip, TextField } from '@mui/material';
import type { SortingSettings } from './sortingSettings';
import { SettingsGroup } from './SettingsLayout';

function wordsFromDraft(value: string): string[] {
  return value.split(/\r?\n/).filter((word) => word !== '');
}

export function SortingSettingsFields({ settings, draft, disabled, stale, error, onChange }: {
  settings?: SortingSettings; draft: string; disabled: boolean; stale: boolean;
  error?: string; onChange: (value: string) => void;
}) {
  const savedWords = settings?.SortRemoveWords ?? [];
  const [words, setWords] = useState(() => wordsFromDraft(draft));
  const [input, setInput] = useState('');
  const shownDraft = [...words, input].filter((word) => word !== '').join('\n');
  useEffect(() => {
    if (shownDraft !== draft) {
      setWords(wordsFromDraft(draft));
      setInput('');
    }
  }, [draft, shownDraft]);
  function update(nextWords: string[], nextInput = '') {
    setWords(nextWords);
    setInput(nextInput);
    onChange([...nextWords, nextInput].filter((word) => word !== '').join('\n'));
  }
  return <SettingsGroup title="Library sorting" description="Remove a matching leading whole word when sorting media. Saving updates generated sort names immediately and keeps custom sort names." currentValue={savedWords.length ? savedWords.join(', ') : 'No words removed'} stale={stale}>
    <Autocomplete multiple freeSolo options={[]} value={words} inputValue={input} disabled={disabled}
      onChange={(_, value) => update(value)}
      onInputChange={(_, value, reason) => { if (reason === 'input') update(words, value); }}
      onBlur={() => { if (input) update([...words, input]); }}
      renderValue={(values, getItemProps) => values.map((word, index) => {
        const { key, ...props } = getItemProps({ index });
        return <Chip key={key} {...props} label={word} size="small" />;
      })}
      renderInput={(params) => <TextField {...params} id="settings-SortRemoveWords-input" name="SortRemoveWords" label="Words removed from sort names" error={Boolean(error)} helperText={error || 'Type a word and press Enter. Up to 32 distinct words, without spaces; at most 128 UTF-8 bytes each. Clear all words to sort by complete names.'}
        onPaste={(event) => {
          const pasted = event.clipboardData.getData('text');
          if (!/[\r\n]/.test(pasted)) return;
          event.preventDefault();
          update([...words, ...wordsFromDraft(input + pasted)]);
        }}
        slotProps={{ ...params.slotProps, htmlInput: { ...params.slotProps.htmlInput, autoComplete: 'off', autoCapitalize: 'none', spellCheck: false } }} />}
    />
  </SettingsGroup>;
}
