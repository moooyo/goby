import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, IconButton, MenuItem, Paper, Skeleton, Stack, TextField, Typography } from '@mui/material';
import ArrowUpwardRounded from '@mui/icons-material/ArrowUpwardRounded';
import ArrowDownwardRounded from '@mui/icons-material/ArrowDownwardRounded';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { introSkipModes, subtitleModes, userPreferencesApi } from './userPreferencesApi';
import type { UserConfiguration, UserPreferences, WritableUserConfiguration } from './userPreferencesApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const audioPreferenceBooleans = ['PlayDefaultAudioTrack', 'RememberAudioSelections', 'RememberSubtitleSelections'] as const;
const discoveryPreferenceBooleans = ['HidePlayedInLatest', 'HidePlayedInMoreLikeThis'] as const;
const booleanLabels: Record<typeof audioPreferenceBooleans[number] | typeof discoveryPreferenceBooleans[number], string> = {
  PlayDefaultAudioTrack: 'Use the default audio track', RememberAudioSelections: 'Remember audio track selections', RememberSubtitleSelections: 'Remember subtitle selections', HidePlayedInLatest: 'Hide played items from Latest', HidePlayedInMoreLikeThis: 'Hide played items from More Like This',
};
const modeLabels = { Default: 'Default', Always: 'Always', OnlyForced: 'Forced subtitles only', None: 'Off', Smart: 'Smart', HearingImpaired: 'Hearing impaired' };
const introModeLabels = { None: 'Off', ShowButton: 'Show skip button', AutoSkip: 'Skip automatically' };
export function UserPreferencesDialog({ userId, userName, onClose, onNavigationGuardChange }: { userId: string; userName: string; onClose: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [saved, setSaved] = useState<UserPreferences>();
  const [draft, setDraft] = useState<UserConfiguration>();
  const [rewind, setRewind] = useState('0');
  const [libraries, setLibraries] = useState<Library[]>();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [libraryError, setLibraryError] = useState<unknown>(null);
  const [review, setReview] = useState(false);
  const [revision, setRevision] = useState(0);
  const [notice, setNotice] = useState('');
  const inFlight = useRef(false);
  const dirty = Boolean(saved && draft && (JSON.stringify(saved.Configuration) !== JSON.stringify(draft) || rewind !== String(saved.Configuration.ResumeRewindSeconds)));
  const invalidRewind = !/^\d{1,3}$/.test(rewind) || Number(rewind) > 300;
  const invalidLanguage = (value: string) => value !== '' && !/^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$/.test(value);
  const invalid = invalidRewind || Boolean(draft && (invalidLanguage(draft.AudioLanguagePreference) || invalidLanguage(draft.SubtitleLanguagePreference)));
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard unsaved preferences and leave this page?');
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setLibraryError(null); setSaved(undefined); setDraft(undefined);
    void Promise.allSettled([
      userPreferencesApi.get(userId, { signal: controller.signal }).then((result) => { if (!controller.signal.aborted) { setSaved(result); setDraft(result.Configuration); setRewind(String(result.Configuration.ResumeRewindSeconds)); setReview(false); setNotice(''); } }).catch((cause: unknown) => { if (!isAbortError(cause)) setError(cause); }),
      adminApi.getLibraries({ signal: controller.signal }).then((result) => { if (!controller.signal.aborted) setLibraries(result.Items); }).catch((cause: unknown) => { if (!isAbortError(cause)) setLibraryError(cause); }),
    ]).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [userId, revision]);
  function change<K extends keyof WritableUserConfiguration>(key: K, value: WritableUserConfiguration[K]) { setDraft((current) => current ? { ...current, [key]: value } : current); setNotice(''); }
  function close() { if (!inFlight.current && (!dirty || window.confirm('Discard unsaved user preferences?'))) onClose(); }
  function reload() { if (!inFlight.current && (!dirty || window.confirm('Discard your draft and reload saved preferences?'))) setRevision((value) => value + 1); }
  async function save() {
    if (inFlight.current || !saved || !draft || !dirty || invalid || review) return;
    inFlight.current = true; setBusy(true); setError(null); setNotice('');
    try {
      const configuration: Partial<WritableUserConfiguration> = {
        AudioLanguagePreference: draft.AudioLanguagePreference, SubtitleLanguagePreference: draft.SubtitleLanguagePreference,
        PlayDefaultAudioTrack: draft.PlayDefaultAudioTrack, RememberAudioSelections: draft.RememberAudioSelections, RememberSubtitleSelections: draft.RememberSubtitleSelections,
        SubtitleMode: draft.SubtitleMode, HidePlayedInLatest: draft.HidePlayedInLatest, HidePlayedInMoreLikeThis: draft.HidePlayedInMoreLikeThis,
        IntroSkipMode: draft.IntroSkipMode, EnableNextEpisodeAutoPlay: draft.EnableNextEpisodeAutoPlay,
        OrderedViews: draft.OrderedViews, LatestItemsExcludes: draft.LatestItemsExcludes, MyMediaExcludes: draft.MyMediaExcludes,
      };
      const result = await userPreferencesApi.update(userId, saved.Revision, { ...configuration, ResumeRewindSeconds: Number(rewind) });
      setSaved(result); setDraft(result.Configuration); setRewind(String(result.Configuration.ResumeRewindSeconds)); setNotice('Preferences saved. New client requests use these settings.');
    } catch (cause) {
      if (!isAbortError(cause)) setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
    } finally { inFlight.current = false; setBusy(false); }
  }
  function reorder(index: number, delta: number) { if (!draft) return; const next = [...draft.OrderedViews]; [next[index], next[index + delta]] = [next[index + delta], next[index]]; change('OrderedViews', next); }
  const disabled = loading || busy || review;
  const nameFor = (id: string) => libraries?.find((library) => library.Id === id)?.Name ?? `Unavailable library (${id})`;
  const listIDs = [...new Set([...(libraries?.map((library) => library.Id) ?? []), ...(draft?.LatestItemsExcludes ?? []), ...(draft?.MyMediaExcludes ?? [])])];
  return <Dialog open fullWidth maxWidth="md" onClose={close} aria-labelledby="user-preferences-title">
    <DialogTitle id="user-preferences-heading"><Typography component="span" variant="h3" id="user-preferences-title">Playback and display preferences</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>{userName}</Typography></DialogTitle>
    <DialogContent aria-busy={loading || busy}><Stack spacing={3} sx={{ pt: 1 }}>
      {error != null && <ErrorNotice error={error} />}
      {review && <Alert severity="warning" action={<Button color="inherit" onClick={reload}>Reload</Button>}>Preferences changed or the save response could not be confirmed. Your draft is kept. Reload and review the saved preferences before saving again.</Alert>}
      {notice && <Alert severity="success">{notice}</Alert>}
      {loading && <Skeleton variant="rounded" height={220} aria-label="Loading user preferences" />}
      {draft && <>
        <Box component="section" aria-label="Audio and subtitles"><Typography component="h3" variant="h4" sx={{ mb: 2 }}>Audio and subtitles</Typography><Stack spacing={2}>
          {(['AudioLanguagePreference', 'SubtitleLanguagePreference'] as const).map((field) => <TextField key={field} label={field === 'AudioLanguagePreference' ? 'Preferred audio language' : 'Preferred subtitle language'} value={draft[field]} disabled={disabled} onChange={(event) => change(field, event.target.value)} error={invalidLanguage(draft[field]) || Boolean(fieldError(error, `Configuration.${field}`))} helperText={fieldError(error, `Configuration.${field}`) ?? 'Language code, such as en, eng, or pt-BR. Leave empty for no preference.'} />)}
          <TextField select label="Subtitle mode" value={draft.SubtitleMode} disabled={disabled} onChange={(event) => change('SubtitleMode', event.target.value as UserConfiguration['SubtitleMode'])}>{subtitleModes.map((mode) => <MenuItem key={mode} value={mode}>{modeLabels[mode]}</MenuItem>)}</TextField>
          {audioPreferenceBooleans.map((field) => <FormControlLabel key={field} control={<Checkbox checked={draft[field]} disabled={disabled} onChange={(event) => change(field, event.target.checked)} />} label={booleanLabels[field]} />)}
        </Stack></Box>
        <Divider /><Box component="section" aria-label="Playback and discovery"><Typography component="h3" variant="h4" sx={{ mb: 2 }}>Playback and discovery</Typography><Stack spacing={2}>
          <TextField label="Rewind on resume (seconds)" value={rewind} disabled={disabled} onChange={(event) => { setRewind(event.target.value); setNotice(''); }} error={invalidRewind || Boolean(fieldError(error, 'Configuration.ResumeRewindSeconds'))} helperText={fieldError(error, 'Configuration.ResumeRewindSeconds') ?? 'Use a whole number from 0 to 300.'} slotProps={{ htmlInput: { inputMode: 'numeric' } }} />
          <TextField select label="Intro skipping" value={draft.IntroSkipMode} disabled={disabled} onChange={(event) => change('IntroSkipMode', event.target.value as UserConfiguration['IntroSkipMode'])} error={Boolean(fieldError(error, 'Configuration.IntroSkipMode'))} helperText={fieldError(error, 'Configuration.IntroSkipMode') ?? 'Uses an intro interval for the current media source. Skip controls and automatic skipping require a compatible client.'}>{introSkipModes.map((mode) => <MenuItem key={mode} value={mode}>{introModeLabels[mode]}</MenuItem>)}</TextField>
          <Box><FormControlLabel control={<Checkbox checked={draft.EnableNextEpisodeAutoPlay} disabled={disabled} onChange={(event) => change('EnableNextEpisodeAutoPlay', event.target.checked)} slotProps={{ input: { 'aria-describedby': 'next-episode-help' } }} />} label="Automatically play the next episode" /><Typography id="next-episode-help" variant="body2" color={fieldError(error, 'Configuration.EnableNextEpisodeAutoPlay') ? 'error.main' : 'text.secondary'}>{fieldError(error, 'Configuration.EnableNextEpisodeAutoPlay') ?? 'Compatible clients can continue to the next available episode that this user may play.'}</Typography></Box>
          {discoveryPreferenceBooleans.map((field) => <FormControlLabel key={field} control={<Checkbox checked={draft[field]} disabled={disabled} onChange={(event) => change(field, event.target.checked)} />} label={booleanLabels[field]} />)}
        </Stack></Box>
        <Divider /><Box component="section" aria-label="Library display"><Typography component="h3" variant="h4" sx={{ mb: 1 }}>Library display</Typography><Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>These choices affect display only. They do not grant access to a library.</Typography>
          {libraryError != null && <ErrorNotice error={libraryError} retry={reload} />}
          <Stack spacing={1}>{draft.OrderedViews.map((id, index) => <Paper key={id} variant="outlined" sx={{ p: 1.5 }}><Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}><Typography variant="body2" sx={{ flex: 1, overflowWrap: 'anywhere' }}>{nameFor(id)}</Typography><IconButton aria-label={`Move ${nameFor(id)} up`} disabled={disabled || index === 0} onClick={() => reorder(index, -1)}><ArrowUpwardRounded /></IconButton><IconButton aria-label={`Move ${nameFor(id)} down`} disabled={disabled || index === draft.OrderedViews.length - 1} onClick={() => reorder(index, 1)}><ArrowDownwardRounded /></IconButton><Button disabled={disabled} onClick={() => change('OrderedViews', draft.OrderedViews.filter((entry) => entry !== id))}>Remove</Button></Stack></Paper>)}</Stack>
          <TextField select fullWidth label="Add to display order" value="" disabled={disabled || !libraries} sx={{ mt: 2 }} onChange={(event) => change('OrderedViews', [...draft.OrderedViews, event.target.value])}>{(libraries ?? []).filter((library) => !draft.OrderedViews.includes(library.Id)).map((library) => <MenuItem key={library.Id} value={library.Id}>{library.Name}</MenuItem>)}</TextField>
          {draft.OrderedViews.length === 0 && <Typography variant="caption" color="text.secondary">Using the default library order.</Typography>}
          {(['LatestItemsExcludes', 'MyMediaExcludes'] as const).map((field) => <Box key={field} sx={{ mt: 2 }}><Typography variant="body2" sx={{ fontWeight: 650 }}>{field === 'LatestItemsExcludes' ? 'Hide from Latest' : 'Hide from My Media'}</Typography>{listIDs.map((id) => <FormControlLabel key={id} sx={{ display: 'flex' }} control={<Checkbox checked={draft[field].includes(id)} disabled={disabled || !libraries} onChange={(event) => change(field, event.target.checked ? [...draft[field], id] : draft[field].filter((value) => value !== id))} />} label={<Typography variant="body2">{nameFor(id)}</Typography>} />)}</Box>)}
        </Box>
      </>}
    </Stack></DialogContent>
    <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{dirty ? 'Unsaved preferences' : 'All preferences saved'}</Typography><Button onClick={reload} disabled={busy || loading}>Reload</Button><Button color="secondary" onClick={close} disabled={busy}>Close</Button><Button variant="contained" onClick={() => void save()} disabled={disabled || !saved || !dirty || invalid} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving preferences...' : 'Save preferences'}</Button></DialogActions>
  </Dialog>;
}
