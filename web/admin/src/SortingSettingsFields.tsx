import { Box, Paper, Stack, TextField, Typography } from '@mui/material';
import { sortingWordsHelp } from './sortingSettings';
import type { SortingSettings } from './sortingSettings';

export function SortingSettingsFields({ settings, draft, disabled, stale, error, onChange }: {
  settings?: SortingSettings; draft: string; disabled: boolean; stale: boolean;
  error?: string; onChange: (value: string) => void;
}) {
  const words = settings?.SortRemoveWords ?? [];
  return <Paper component="section" aria-labelledby="settings-sorting-heading" variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}>
    <Stack spacing={2}>
      <Box><Typography id="settings-sorting-heading" variant="h3" component="h2">Library sorting</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.75 }}>Saving updates generated sort names immediately. A matching leading whole word is removed when sorting. Custom sort names are kept.</Typography></Box>
      <TextField id="settings-SortRemoveWords-input" name="SortRemoveWords" label="Words removed from sort names" fullWidth multiline minRows={3} maxRows={8}
        value={draft} disabled={disabled} onChange={(event) => onChange(event.target.value)} error={Boolean(error)} helperText={error || sortingWordsHelp}
        slotProps={{ htmlInput: { autoComplete: 'off', autoCapitalize: 'none', spellCheck: false } }} />
      <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{stale ? 'Last confirmed words' : 'Saved words'}: {words.length ? words.join(', ') : 'None'}</Typography>
    </Stack>
  </Paper>;
}
