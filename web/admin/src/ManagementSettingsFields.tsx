import { Box, Button, FormControlLabel, Paper, Stack, Switch, TextField, Typography } from '@mui/material';
import type { ManagementSettings } from './api';
import { fieldError } from './formFields';
import { managementDraft } from './managementDraft';
import type { ManagementDraft } from './managementDraft';

export function ManagementSettingsFields({ draft, defaults, disabled, errors, mutationError, onChange }: { draft: ManagementDraft; defaults?: ManagementSettings; disabled: boolean; errors: Record<string, string | undefined>; mutationError: unknown; onChange: (draft: ManagementDraft) => void }) {
  const issue = (field: string) => errors[`Management.${field}`] ?? fieldError(mutationError, `Management.${field}`);
  return <Paper component="section" aria-labelledby="management-settings-title" variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}>
    <Stack spacing={3}>
      <Box><Typography variant="h3" component="h2" id="management-settings-title">Metadata, subtitles, and tasks</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.75 }}>Configure online lookups, automatic subtitle downloads, and server task resources.</Typography></Box>
      <FormControlLabel label="Enable internet providers" control={<Switch checked={draft.Metadata.EnableInternetProviders} disabled={disabled} onChange={(event) => onChange({ ...draft, Metadata: { ...draft.Metadata, EnableInternetProviders: event.target.checked } })} />} />
      <Typography variant="body2" color="text.secondary">Provider credentials are loaded from deployment configuration at server startup. Restart the server after changing credentials. Review connection availability in Online providers.</Typography>
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
        <TextField fullWidth label="Preferred metadata language" value={draft.Metadata.PreferredMetadataLanguage} disabled={disabled} error={Boolean(issue('Metadata.PreferredMetadataLanguage'))} helperText={issue('Metadata.PreferredMetadataLanguage') ?? 'Language code, for example en or en-US.'} onChange={(event) => onChange({ ...draft, Metadata: { ...draft.Metadata, PreferredMetadataLanguage: event.target.value } })} />
        <TextField fullWidth label="Metadata country" value={draft.Metadata.MetadataCountryCode} disabled={disabled} error={Boolean(issue('Metadata.MetadataCountryCode'))} helperText={issue('Metadata.MetadataCountryCode') ?? 'Two-letter country code, for example US.'} onChange={(event) => onChange({ ...draft, Metadata: { ...draft.Metadata, MetadataCountryCode: event.target.value } })} />
      </Stack>
      <TextField multiline minRows={2} fullWidth label="Subtitle download languages" value={draft.Subtitles.DownloadLanguages} disabled={disabled} error={Boolean(issue('Subtitles.DownloadLanguages'))} helperText={issue('Subtitles.DownloadLanguages') ?? 'One language code per line, up to 8. Leave empty to disable automatic subtitle downloads.'} onChange={(event) => onChange({ ...draft, Subtitles: { ...draft.Subtitles, DownloadLanguages: event.target.value } })} />
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>{(['DownloadMovieSubtitles', 'DownloadEpisodeSubtitles'] as const).map((field) => <FormControlLabel key={field} label={field === 'DownloadMovieSubtitles' ? 'Download movie subtitles' : 'Download episode subtitles'} control={<Switch checked={draft.Subtitles[field]} disabled={disabled} onChange={(event) => onChange({ ...draft, Subtitles: { ...draft.Subtitles, [field]: event.target.checked } })} />} />)}</Stack>
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(3, 1fr)' }, gap: 2 }}>{([['MaxConcurrent', 'Concurrent provider and cache workers', '1–16 workers. Applies to new task admissions; library scanning uses its existing limits.'], ['CacheRetentionDays', 'Cache retention (days)', '1–3650 days.'], ['CacheMaxEntries', 'Maximum cache entries', '1–1,000,000 entries.']] as const).map(([field, label, help]) => <TextField key={field} label={label} value={draft.Tasks[field]} disabled={disabled} error={Boolean(issue(`Tasks.${field}`))} helperText={issue(`Tasks.${field}`) ?? help} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 16 } }} onChange={(event) => onChange({ ...draft, Tasks: { ...draft.Tasks, [field]: event.target.value } })} />)}</Box>
      {defaults && <Button sx={{ alignSelf: 'flex-start' }} color="secondary" disabled={disabled} onClick={() => onChange(managementDraft(defaults))}>Use default management settings</Button>}
    </Stack>
  </Paper>;
}
