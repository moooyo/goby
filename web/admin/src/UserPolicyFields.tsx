import type { ReactNode } from 'react';
import { Box, Button, Checkbox, Divider, FormControlLabel, MenuItem, Stack, Switch, TextField, Typography } from '@mui/material';
import { fieldError } from './formFields';
import { FeatureAccessFields } from './FeatureAccessFields';
import { accessDays, editableUnratedCategories } from './userPolicy';
import { DeletionFoldersField } from './DeletionFoldersField';
import type { UserPolicyDraft } from './userPolicy';

type BooleanField = { [K in keyof UserPolicyDraft]: UserPolicyDraft[K] extends boolean ? K : never }[keyof UserPolicyDraft];
type ListField = { [K in keyof UserPolicyDraft]: UserPolicyDraft[K] extends string[] ? K : never }[keyof UserPolicyDraft];
interface Props { policy: UserPolicyDraft; disabled: boolean; error: unknown; errors: Record<string, string>; onChange: <K extends keyof UserPolicyDraft>(field: K, value: UserPolicyDraft[K]) => void }

export function UserPolicyFields({ policy, disabled, error, errors, onChange }: Props) {
  const issue = (field: keyof UserPolicyDraft) => errors[`Policy.${field}`] ?? fieldError(error, `Policy.${field}`);
  const boolean = (field: BooleanField, label: string) => <FormControlLabel key={field} sx={{ m: 0, justifyContent: 'space-between', gap: 2 }} labelPlacement="start" label={label} control={<Switch checked={policy[field]} disabled={disabled} onChange={(event) => onChange(field, event.target.checked)} />} />;
  const list = (field: ListField, label: string, help = 'Enter one value per line. Empty lines are ignored.') => <TextField key={field} fullWidth multiline minRows={2} label={label} value={policy[field].join('\n')} disabled={disabled} onChange={(event) => onChange(field, event.target.value.split('\n'))} error={Boolean(issue(field))} helperText={issue(field) ?? help} />;
  const numeric = (field: 'AutoRemoteQuality' | 'RemoteClientBitrateLimit' | 'SimultaneousStreamLimit', label: string, help: string) => <TextField key={field} fullWidth label={label} value={policy[field]} disabled={disabled} onChange={(event) => onChange(field, event.target.value)} error={Boolean(issue(field))} helperText={issue(field) ?? help} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 64 } }} />;
  const section = (title: string, description: string, fields: ReactNode) => <Box component="section" aria-label={title}><Typography variant="h4" component="h3">{title}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, mb: 2 }}>{description}</Typography><Stack spacing={2}>{fields}</Stack></Box>;
  return <Stack spacing={3} divider={<Divider />}>
    {section('Visibility and remote access', 'Choose where this account appears and how it may connect.', <>
      {boolean('IsHidden', 'Hide from sign-in screens')}{boolean('IsHiddenRemotely', 'Hide from remote sign-in screens')}{boolean('IsHiddenFromUnusedDevices', 'Hide on devices not previously used')}
      {boolean('EnableRemoteAccess', 'Allow remote access')}{boolean('EnableUserPreferenceAccess', 'Allow preference changes')}
      {boolean('EnableRemoteControlOfOtherUsers', 'Control other users\' sessions')}{boolean('EnableSharedDeviceControl', 'Control shared devices')}
      {boolean('EnableAllDevices', 'Allow all devices')}
      {!policy.EnableAllDevices && list('EnabledDevices', 'Allowed device identifiers', 'Enter one reported device identifier per line. An empty list allows no devices.')}
    </>)}
    {section('Content restrictions', 'Limit content by rating, tags, and folders.', <>
      <TextField label="Maximum parental rating" value={policy.MaxParentalRating} disabled={disabled} onChange={(event) => onChange('MaxParentalRating', event.target.value)} helperText={issue('MaxParentalRating') ?? 'Leave blank for no rating limit. Use the numeric rating level supported by your library.'} error={Boolean(issue('MaxParentalRating'))} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 64 } }} />
      {list('BlockedTags', 'Blocked tags')}{list('IncludeTags', 'Included tags')}
      {boolean('IsTagBlockingModeInclusive', 'Use blocked tags as an allow list (legacy)')}{boolean('AllowTagOrRating', 'Allow a matching tag or rating')}
      <Box><Typography variant="body2" sx={{ fontWeight: 650, mb: 1 }}>Block unrated content</Typography><Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' } }}>{editableUnratedCategories.map((category) => <FormControlLabel key={category} label={category} control={<Checkbox disabled={disabled} checked={policy.BlockUnratedItems.includes(category)} onChange={(event) => onChange('BlockUnratedItems', event.target.checked ? [...policy.BlockUnratedItems, category] : policy.BlockUnratedItems.filter((value) => value !== category))} />} />)}</Box>{policy.BlockUnratedItems.filter((category) => !editableUnratedCategories.includes(category as typeof editableUnratedCategories[number])).map((category) => <Stack key={category} direction="row" sx={{ alignItems: 'center', gap: 1 }}><Typography variant="body2" color="text.secondary">{category} · Inactive saved category</Typography><Button disabled={disabled} color="error" aria-label={`Remove inactive unrated category ${category}`} onClick={() => onChange('BlockUnratedItems', policy.BlockUnratedItems.filter((value) => value !== category))}>Remove</Button></Stack>)}{issue('BlockUnratedItems') && <Typography color="error" variant="body2">{issue('BlockUnratedItems')}</Typography>}</Box>
      {list('ExcludedSubFolders', 'Excluded folder identifiers')}
    </>)}
    <FeatureAccessFields restricted={policy.RestrictedFeatures} disabled={disabled} error={issue('RestrictedFeatures')} onChange={(value) => onChange('RestrictedFeatures', value)} />
    {section('Downloads and media changes', 'Grant download, subtitle, and deletion permissions separately.', <>
      {boolean('EnableContentDownloading', 'Download media')}{boolean('EnableSubtitleDownloading', 'Download subtitles')}{boolean('EnableSubtitleManagement', 'Manage subtitles')}
      {boolean('EnableContentDeletion', 'Delete media')}<DeletionFoldersField value={policy.EnableContentDeletionFromFolders} disabled={disabled} error={issue('EnableContentDeletionFromFolders')} onChange={(value) => onChange('EnableContentDeletionFromFolders', value)} />
    </>)}
    {section('Playback limits and quality', 'Configure account playback limits and the compatible client quality preference.', <>
      {numeric('RemoteClientBitrateLimit', 'Remote bitrate limit (bits per second)', 'Set 0 for no account-specific bitrate limit.')}
      {numeric('SimultaneousStreamLimit', 'Simultaneous stream limit', 'Set 0 for no account-specific stream limit.')}
      {numeric('AutoRemoteQuality', 'Automatic remote quality (bits per second)', 'Compatible clients use this preference for automatic remote playback quality. Set 0 for automatic/default quality. The separate remote bitrate limit remains the server limit.')}
    </>)}
    {section('Access schedule', 'An empty schedule allows access at any time. Hours use the server time zone; split overnight access into two intervals.', <>
      {policy.AccessSchedules.map((schedule, index) => <Stack key={index} direction={{ xs: 'column', sm: 'row' }} spacing={1.5} sx={{ alignItems: 'flex-start' }}>
        <TextField select label={`Access day ${index + 1}`} value={schedule.DayOfWeek} disabled={disabled} sx={{ minWidth: 145 }} onChange={(event) => onChange('AccessSchedules', policy.AccessSchedules.map((value, position) => position === index ? { ...value, DayOfWeek: event.target.value } : value))}>{accessDays.map((day) => <MenuItem key={day} value={day}>{day}</MenuItem>)}</TextField>
        {(['StartHour', 'EndHour'] as const).map((field) => <TextField key={field} label={`${field === 'StartHour' ? 'Start' : 'End'} hour ${index + 1}`} value={schedule[field]} disabled={disabled} onChange={(event) => onChange('AccessSchedules', policy.AccessSchedules.map((value, position) => position === index ? { ...value, [field]: event.target.value } : value))} error={Boolean(issue('AccessSchedules'))} slotProps={{ htmlInput: { inputMode: 'decimal', maxLength: 64 } }} />)}
        <Button color="error" disabled={disabled} onClick={() => onChange('AccessSchedules', policy.AccessSchedules.filter((_, position) => position !== index))} aria-label={`Remove access interval ${index + 1}`}>Remove</Button>
      </Stack>)}
      {issue('AccessSchedules') && <Typography color="error" variant="body2">{issue('AccessSchedules')}</Typography>}
      <Button variant="outlined" disabled={disabled || policy.AccessSchedules.length >= 256} onClick={() => onChange('AccessSchedules', [...policy.AccessSchedules, { DayOfWeek: 'Everyday', StartHour: '9', EndHour: '17' }])} sx={{ alignSelf: 'flex-start' }}>Add access interval</Button>
    </>)}
  </Stack>;
}
