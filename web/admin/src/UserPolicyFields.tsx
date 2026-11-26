import type { ReactNode } from 'react';
import { Box, Button, Checkbox, Divider, FormControlLabel, MenuItem, Stack, Switch, TextField, Typography } from './ui';
import { fieldError } from './formFields';
import { accessDays, editableUnratedCategories } from './userPolicy';
import { useTranslation } from './i18n';
import { DeletionFoldersField } from './DeletionFoldersField';
import type { UserPolicyDraft } from './userPolicy';

type BooleanField = { [K in keyof UserPolicyDraft]: UserPolicyDraft[K] extends boolean ? K : never }[keyof UserPolicyDraft];
type ListField = { [K in keyof UserPolicyDraft]: UserPolicyDraft[K] extends string[] ? K : never }[keyof UserPolicyDraft];
interface Props { policy: UserPolicyDraft; disabled: boolean; error: unknown; errors: Record<string, string>; onChange: <K extends keyof UserPolicyDraft>(field: K, value: UserPolicyDraft[K]) => void }

export function UserPolicyLimits({ policy, disabled, error, errors, onChange }: Props) {
  const { t } = useTranslation();
  const [first] = policy.AccessSchedules;
  const preset = policy.AccessSchedules.length === 0 ? 'any' : policy.AccessSchedules.length === 1 && first.DayOfWeek === 'Everyday' && Number(first.StartHour) === 7 && Number(first.EndHour) === 22 ? 'daytime' : 'custom';
  const scheduleError = errors['Policy.AccessSchedules'] ?? fieldError(error, 'Policy.AccessSchedules');
  const numberFields = [
    ['SimultaneousStreamLimit', 'Simultaneous stream limit', '0 means no account-specific stream limit.'],
    ['RemoteClientBitrateLimit', 'Remote bitrate limit (Mbps)', '0 means no account-specific bitrate limit.'],
    ['MaxParentalRating', 'Maximum parental rating', 'Leave blank for no rating limit. Use the numeric rating level supported by your library.'],
  ] as const;
  return <div className="users-setting-list">
    {numberFields.map(([key, label, help]) => {
      const issue = errors[`Policy.${key}`] ?? fieldError(error, `Policy.${key}`);
      return <div key={key} className="users-limit-row">
        <div><label htmlFor={`user-limit-${key}`}>{t(label)}</label><small className="users-help" id={`user-limit-${key}-help`}>{t(help)}</small></div>
        <TextField id={`user-limit-${key}`} value={policy[key]} disabled={disabled} onChange={(event) => onChange(key, event.target.value)} error={Boolean(issue)} helperText={issue} slotProps={{ htmlInput: { inputMode: key === 'RemoteClientBitrateLimit' ? 'decimal' : 'numeric', maxLength: 64, 'aria-describedby': `user-limit-${key}-help` } }} />
      </div>;
    })}
    <div className="users-limit-row">
      <div><label htmlFor="user-schedule-choice">{t('Allowed access times')}</label><small className="users-help" id="user-schedule-help">{t('Client access outside these hours is blocked. Hours use the server time zone.')}</small></div>
      <TextField id="user-schedule-choice" select value={preset} disabled={disabled} slotProps={{ select: { 'aria-describedby': 'user-schedule-help' } }} onChange={(event) => onChange('AccessSchedules', event.target.value === 'any' ? [] : event.target.value === 'daytime' ? [{ DayOfWeek: 'Everyday', StartHour: '7', EndHour: '22' }] : [{ DayOfWeek: 'Everyday', StartHour: '9', EndHour: '17' }])}>
        <MenuItem value="any">No time limit</MenuItem><MenuItem value="daytime">Every day, 07:00–22:00</MenuItem><MenuItem value="custom">Custom hours…</MenuItem>
      </TextField>
    </div>
    {preset === 'custom' && <div className="users-custom-schedule"><Stack spacing={1.5}>
      <p className="users-help">{t('Split overnight access into two intervals.')}</p>
      {policy.AccessSchedules.map((schedule, index) => <div key={index} className="users-schedule-interval">
        <TextField select label={`Access day ${index + 1}`} value={schedule.DayOfWeek} disabled={disabled} onChange={(event) => onChange('AccessSchedules', policy.AccessSchedules.map((value, position) => position === index ? { ...value, DayOfWeek: event.target.value } : value))}>{accessDays.map((day) => <MenuItem key={day} value={day}>{day}</MenuItem>)}</TextField>
        {(['StartHour', 'EndHour'] as const).map((field) => <TextField key={field} label={`${field === 'StartHour' ? 'Start' : 'End'} hour ${index + 1}`} value={schedule[field]} disabled={disabled} onChange={(event) => onChange('AccessSchedules', policy.AccessSchedules.map((value, position) => position === index ? { ...value, [field]: event.target.value } : value))} error={Boolean(scheduleError)} slotProps={{ htmlInput: { inputMode: 'decimal', maxLength: 64 } }} />)}
        <Button color="error" disabled={disabled} onClick={() => onChange('AccessSchedules', policy.AccessSchedules.filter((_, position) => position !== index))} aria-label={`Remove access interval ${index + 1}`}>Remove</Button>
      </div>)}
      <Button variant="outlined" disabled={disabled || policy.AccessSchedules.length >= 256} onClick={() => onChange('AccessSchedules', [...policy.AccessSchedules, { DayOfWeek: 'Everyday', StartHour: '9', EndHour: '17' }])} sx={{ alignSelf: 'flex-start' }}>Add access interval</Button>
    </Stack></div>}
    {scheduleError && <Typography color="error" variant="body2">{scheduleError}</Typography>}
    <FormControlLabel className="users-setting-row" labelPlacement="start" label="Allow remote access" control={<Switch checked={policy.EnableRemoteAccess} disabled={disabled} onChange={(event) => onChange('EnableRemoteAccess', event.target.checked)} />} />
  </div>;
}

export function UserPolicyFields({ policy, disabled, error, errors, onChange }: Props) {
  const issue = (field: keyof UserPolicyDraft) => errors[`Policy.${field}`] ?? fieldError(error, `Policy.${field}`);
  const boolean = (field: BooleanField, label: string, help?: string) => <Box key={field}><FormControlLabel sx={{ m: 0, width: '100%', justifyContent: 'space-between', gap: 2 }} labelPlacement="start" label={label} control={<Switch checked={policy[field]} disabled={disabled} onChange={(event) => onChange(field, event.target.checked)} slotProps={{ input: { 'aria-describedby': help ? `policy-${field}-help` : undefined } }} />} />{help && <Typography id={`policy-${field}-help`} variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>{help}</Typography>}</Box>;
  const list = (field: ListField, label: string, help = 'Enter one value per line. Empty lines are ignored.') => <TextField key={field} fullWidth multiline minRows={2} label={label} value={policy[field].join('\n')} disabled={disabled} onChange={(event) => onChange(field, event.target.value.split('\n'))} error={Boolean(issue(field))} helperText={issue(field) ?? help} />;
  const numeric = (field: 'AutoRemoteQuality' | 'RemoteClientBitrateLimit' | 'SimultaneousStreamLimit', label: string, help: string) => <TextField key={field} fullWidth label={label} value={policy[field]} disabled={disabled} onChange={(event) => onChange(field, event.target.value)} error={Boolean(issue(field))} helperText={issue(field) ?? help} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 64 } }} />;
  const section = (title: string, description: string, fields: ReactNode) => <Box component="section" aria-label={title}><Typography variant="h4" component="h3">{title}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, mb: 2 }}>{description}</Typography><Stack spacing={2}>{fields}</Stack></Box>;
  return <Stack spacing={3} divider={<Divider />}>
    {section('Visibility and remote access', 'Choose where this account appears and how it may connect.', <>
      {boolean('IsHidden', 'Hide from sign-in screens')}{boolean('IsHiddenRemotely', 'Hide from remote sign-in screens')}{boolean('IsHiddenFromUnusedDevices', 'Hide on devices not previously used')}
      {boolean('EnableUserPreferenceAccess', 'Allow preference changes', 'Manage personal preferences must also be checked in Feature access.')}
      {boolean('EnableRemoteControlOfOtherUsers', 'Control other users\' sessions')}{boolean('EnableSharedDeviceControl', 'Control shared devices')}
      {boolean('EnableAllDevices', 'Allow all devices')}
      {!policy.EnableAllDevices && list('EnabledDevices', 'Allowed device identifiers', 'Enter one reported device identifier per line. An empty list allows no devices.')}
    </>)}
    {section('Content restrictions', 'Limit content by rating, tags, and folders.', <>
      {list('BlockedTags', 'Blocked tags')}{list('IncludeTags', 'Included tags')}
      {boolean('IsTagBlockingModeInclusive', 'Use blocked tags as an allow list (legacy)')}{boolean('AllowTagOrRating', 'Allow a matching tag or rating')}
      <Box><Typography variant="body2" sx={{ fontWeight: 650, mb: 1 }}>Block unrated content</Typography><Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' } }}>{editableUnratedCategories.map((category) => <FormControlLabel key={category} label={category} control={<Checkbox disabled={disabled} checked={policy.BlockUnratedItems.includes(category)} onChange={(event) => onChange('BlockUnratedItems', event.target.checked ? [...policy.BlockUnratedItems, category] : policy.BlockUnratedItems.filter((value) => value !== category))} />} />)}</Box>{policy.BlockUnratedItems.filter((category) => !editableUnratedCategories.includes(category as typeof editableUnratedCategories[number])).map((category) => <Stack key={category} direction="row" sx={{ alignItems: 'center', gap: 1 }}><Typography variant="body2" color="text.secondary">{category} · Inactive saved category</Typography><Button disabled={disabled} color="error" aria-label={`Remove inactive unrated category ${category}`} onClick={() => onChange('BlockUnratedItems', policy.BlockUnratedItems.filter((value) => value !== category))}>Remove</Button></Stack>)}{issue('BlockUnratedItems') && <Typography color="error" variant="body2">{issue('BlockUnratedItems')}</Typography>}</Box>
      {list('ExcludedSubFolders', 'Excluded folder identifiers')}
    </>)}
    {section('Subtitles and media changes', 'Grant subtitle and deletion permissions separately.', <>
      {boolean('EnableSubtitleDownloading', 'Download subtitles', 'Download subtitles from providers must also be checked in Feature access.')}{boolean('EnableSubtitleManagement', 'Manage subtitles', 'Manage external subtitles must also be checked in Feature access.')}
      {boolean('EnableContentDeletion', 'Delete media')}<DeletionFoldersField value={policy.EnableContentDeletionFromFolders} disabled={disabled} error={issue('EnableContentDeletionFromFolders')} onChange={(value) => onChange('EnableContentDeletionFromFolders', value)} />
    </>)}
    {numeric('AutoRemoteQuality', 'Automatic remote quality (bits per second)', 'Compatible clients use this preference for automatic remote playback quality. Set 0 for automatic/default quality. The separate remote bitrate limit remains the server limit.')}
  </Stack>;
}
