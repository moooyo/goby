import { Box, Button, FormControlLabel, InputAdornment, Stack, Switch, TextField } from '@mui/material';
import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import RestartAltRounded from '@mui/icons-material/RestartAltRounded';
import type { ManagementSettings } from './api';
import { fieldError } from './formFields';
import { managementDraft } from './managementDraft';
import type { ManagementDraft } from './managementDraft';
import { SettingsGroup } from './SettingsLayout';

export function ManagementSettingsFields({ draft, defaults, disabled, errors, mutationError, onChange, onProviders }: { draft: ManagementDraft; defaults?: ManagementSettings; disabled: boolean; errors: Record<string, string | undefined>; mutationError: unknown; onChange: (draft: ManagementDraft) => void; onProviders?: () => void }) {
  const issue = (field: string) => errors['Management.' + field] ?? fieldError(mutationError, 'Management.' + field);
  return <>
    <SettingsGroup title="Online lookups" description="Allow online metadata, images, and subtitles. Provider credentials are loaded at server startup; restart after changing credentials.">
      <FormControlLabel sx={{ m: 0 }} label="Enable internet providers" control={<Switch checked={draft.Metadata.EnableInternetProviders} disabled={disabled} onChange={(event) => onChange({ ...draft, Metadata: { ...draft.Metadata, EnableInternetProviders: event.target.checked } })} />} />
      {onProviders && <Button sx={{ alignSelf: 'flex-start', px: 0, minHeight: 32 }} endIcon={<ArrowForwardRounded />} disabled={disabled} onClick={onProviders}>View provider connection status</Button>}
    </SettingsGroup>
    <SettingsGroup title="Metadata language and region" description="Preferred language and country for online metadata lookups. Changes apply to the next work item.">
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
        <TextField fullWidth label="Preferred metadata language" value={draft.Metadata.PreferredMetadataLanguage} disabled={disabled} error={Boolean(issue('Metadata.PreferredMetadataLanguage'))} helperText={issue('Metadata.PreferredMetadataLanguage') ?? 'Language code, for example en or en-US.'} onChange={(event) => onChange({ ...draft, Metadata: { ...draft.Metadata, PreferredMetadataLanguage: event.target.value } })} />
        <TextField fullWidth label="Metadata country" value={draft.Metadata.MetadataCountryCode} disabled={disabled} error={Boolean(issue('Metadata.MetadataCountryCode'))} helperText={issue('Metadata.MetadataCountryCode') ?? 'Two-letter country code, for example US.'} onChange={(event) => onChange({ ...draft, Metadata: { ...draft.Metadata, MetadataCountryCode: event.target.value } })} />
      </Box>
    </SettingsGroup>
    <SettingsGroup title="Subtitle downloads" description="Automatically download subtitles in the selected languages. Changes apply to the next work item.">
      <TextField multiline minRows={2} fullWidth label="Subtitle download languages" value={draft.Subtitles.DownloadLanguages} disabled={disabled} error={Boolean(issue('Subtitles.DownloadLanguages'))} helperText={issue('Subtitles.DownloadLanguages') ?? 'One language code per line, up to 8. Leave empty to disable automatic downloads.'} onChange={(event) => onChange({ ...draft, Subtitles: { ...draft.Subtitles, DownloadLanguages: event.target.value } })} />
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 2 }}>{(['DownloadMovieSubtitles', 'DownloadEpisodeSubtitles'] as const).map((field) => <FormControlLabel sx={{ m: 0 }} key={field} label={field === 'DownloadMovieSubtitles' ? 'Download movie subtitles' : 'Download episode subtitles'} control={<Switch checked={draft.Subtitles[field]} disabled={disabled} onChange={(event) => onChange({ ...draft, Subtitles: { ...draft.Subtitles, [field]: event.target.checked } })} />} />)}</Stack>
    </SettingsGroup>
    <SettingsGroup title="Task resources and cache" description="Set resource limits for newly admitted provider and cache tasks. Library scanning keeps its existing limits.">
      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(3, minmax(0, 1fr))' }, gap: 2 }}>{([['MaxConcurrent', 'Concurrent workers', '1–16 workers.', ''], ['CacheRetentionDays', 'Cache retention', '1–3650 days.', 'days'], ['CacheMaxEntries', 'Maximum cache entries', '1–1,000,000 entries.', '']] as const).map(([field, label, help, unit]) => <TextField key={field} label={label} value={draft.Tasks[field]} disabled={disabled} error={Boolean(issue('Tasks.' + field))} helperText={issue('Tasks.' + field) ?? help} slotProps={{ input: unit ? { endAdornment: <InputAdornment position="end">{unit}</InputAdornment> } : undefined, htmlInput: { inputMode: 'numeric', maxLength: 16 } }} onChange={(event) => onChange({ ...draft, Tasks: { ...draft.Tasks, [field]: event.target.value } })} />)}</Box>
      {defaults && <Button sx={{ alignSelf: 'flex-start', px: 0 }} startIcon={<RestartAltRounded />} disabled={disabled} onClick={() => onChange(managementDraft(defaults))}>Restore default management settings</Button>}
    </SettingsGroup>
  </>;
}
