import { useEffect, useMemo, useRef, useState } from 'react';
import type { ChangeEvent, FormEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Paper, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography } from './ui';
import { SaveOutlined } from './icons';
import { confirmAction, useTranslation } from './i18n';
import './media-editor.css';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { episodeRosterApi, episodeRosterDraft, episodeRosterMaximumBytes, parseEpisodeRosterImport, validateEpisodeRosterDraft } from './episodeRosterApi';
import type { EpisodeRoster, EpisodeRosterDraft } from './episodeRosterApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const emptyDraft: EpisodeRosterDraft = { key: '', label: '', sourceRevision: '', entries: '[]' };
const stateLabels = { absent: 'No roster', active: 'Active', withdrawn: 'Withdrawn' };
const example = '{"Source":{"Key":"series-guide","Label":"Series guide","Revision":"2026-09-20"},"Entries":[{"Key":"s1e1","SeasonNumber":1,"EpisodeNumber":1,"Name":"Pilot","PremiereDate":"2026-09-01"}]}';

export function EpisodeRosterDialog({ seriesId, seriesName, onClose, onNavigationGuardChange }: {
  seriesId: string; seriesName: string; onClose: () => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const { t, locale, formatCount } = useTranslation();
  const formRef = useRef<HTMLFormElement>(null);
  const [season, setSeason] = useState<number | 'all'>('all');
  const [saved, setSaved] = useState<EpisodeRoster>();
  const [draft, setDraft] = useState<EpisodeRosterDraft>(emptyDraft);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [importError, setImportError] = useState('');
  const [review, setReview] = useState(false);
  const [reloadRevision, setReloadRevision] = useState(0);
  const [confirmWithdraw, setConfirmWithdraw] = useState(false);
  const [notice, setNotice] = useState('');
  const [page, setPage] = useState(0);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const importSequence = useRef(0);
  const dirty = Boolean(saved && JSON.stringify(draft) !== JSON.stringify(episodeRosterDraft(saved)));
  const validation = useMemo(() => validateEpisodeRosterDraft(draft, saved?.Revision ?? '0'), [draft, saved?.Revision]);
  const preview = validation.input?.Entries;
  const seasons = [...new Set(preview?.map((entry) => entry.SeasonNumber) ?? [])].sort((left, right) => left - right);
  const visibleEntries = preview?.filter((entry) => season === 'all' || entry.SeasonNumber === season);
  const disabled = loading || busy || importing || review;
  const availableCount = saved?.Entries.filter((entry) => entry.Availability === 'available').length ?? 0;
  const savedEntries = new Map(saved?.Entries.map((entry) => [entry.Key, entry] as const));
  const showSavedStatus = !dirty && saved?.State === 'active';
  useUserDraftNavigation(dirty, busy || importing, onNavigationGuardChange, t('Discard the unsaved episode roster and leave this page?'));

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; importSequence.current += 1; };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setSaved(undefined); setError(null); setImportError('');
    void episodeRosterApi.get(seriesId, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      setSaved(result); setDraft(episodeRosterDraft(result)); setReview(false); setNotice(''); setPage(0); setSeason('all');
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [seriesId, reloadRevision]);

  function close() { if (!inFlight.current && !importing && (!dirty || confirmAction(t('Discard the unsaved episode roster?')))) onClose(); }
  function reload() {
    if (inFlight.current || importing || (dirty && !confirmAction(t('Discard your draft and reload the saved episode roster?')))) return;
    setConfirmWithdraw(false); setReloadRevision((value) => value + 1);
  }
  function change(field: keyof EpisodeRosterDraft, value: string) {
    if (disabled) return;
    setDraft((current) => ({ ...current, [field]: value })); setPage(0); setSeason('all'); setError(null); setImportError(''); setNotice('');
  }
  function errorFor(field: string): string | undefined { return (dirty ? validation.errors[field] : undefined) ?? fieldError(error, field); }

  async function importFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]; event.target.value = '';
    if (!file || !saved || disabled) return;
    const sequence = ++importSequence.current;
    setImportError(''); setNotice('');
    if (file.size > episodeRosterMaximumBytes) { setImportError('Choose a roster JSON file of at most 512 KiB.'); return; }
    setImporting(true);
    try {
      const buffer = await file.arrayBuffer();
      let text: string;
      try { text = new TextDecoder('utf-8', { fatal: true }).decode(buffer); } catch { throw new Error('Choose a roster JSON file encoded as valid UTF-8.'); }
      const imported = parseEpisodeRosterImport(text);
      if (!mounted.current || sequence !== importSequence.current) return;
      setDraft(imported); setError(null); setPage(0); setNotice('Imported roster ready for review. Save roster to replace the current expected episode list.');
    } catch (cause) {
      if (mounted.current && sequence === importSequence.current) setImportError(cause instanceof Error ? cause.message : 'The roster file could not be read.');
    } finally { if (mounted.current && sequence === importSequence.current) setImporting(false); }
  }

  async function mutate(withdraw: boolean) {
    if (inFlight.current || !saved || disabled || (withdraw ? saved.State !== 'active' : !dirty || !validation.input)) return;
    inFlight.current = true; setBusy(true); setError(null); setImportError(''); setNotice('');
    try {
      const result = withdraw ? await episodeRosterApi.withdraw(seriesId, saved.Revision) : await episodeRosterApi.update(seriesId, validation.input!);
      if (!mounted.current) return;
      setSaved(result); setDraft(episodeRosterDraft(result)); setReview(false); setPage(0);
      setNotice(withdraw ? 'Episode roster withdrawn. Its provenance is retained.' : 'Episode roster saved. Availability reflects the current library catalog.');
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
    } finally {
      inFlight.current = false;
      if (mounted.current) { setBusy(false); setConfirmWithdraw(false); }
    }
  }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); void mutate(false); }
  const entryErrors = error instanceof ApiError && error.fields ? Object.keys(error.fields).filter((field) => field.startsWith('Entries.') || field.startsWith('Entries[')) : [];

  return <>
    <Dialog open fullWidth maxWidth="md" className="media-editor-sheet episode-roster-sheet" slotProps={{ paper: { sx: { width: '100%', maxWidth: 600 } } }} onClose={close} aria-labelledby="episode-roster-title">
      <Box ref={formRef} component="form" noValidate onSubmit={submit} onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') { event.preventDefault(); formRef.current?.requestSubmit(); } }} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' }}>
        <DialogTitle id="episode-roster-heading"><Typography component="span" variant="h3" id="episode-roster-title">Episode roster</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{saved?.SeriesName || seriesName}</Typography></DialogTitle>
        <DialogContent aria-busy={loading || busy || importing}><Stack spacing={2.5} sx={{ pt: 0.5 }}>
          {error != null && <ErrorNotice error={error} retry={!saved ? reload : undefined} />}
          {review && <Alert severity="warning" action={<Button color="inherit" onClick={reload}>Reload</Button>}>The roster changed, its source version conflicts, or the save response could not be confirmed. Your draft is kept. Reload and review the saved roster before saving again.</Alert>}
          {notice && <Alert severity={dirty ? 'info' : 'success'}>{notice}</Alert>}
          {loading && <Skeleton variant="rounded" height={220} aria-label="Loading episode roster" />}
          {saved && <>
            <Paper component="section" aria-label="Saved episode roster" variant="outlined" className="episode-roster-summary" sx={{ p: 2 }}><Stack spacing={1}>
              <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">Saved roster</Typography><Chip size="small" variant="outlined" label={stateLabels[saved.State]} /></Stack>
              <Typography variant="body2">{formatCount(saved.Entries.length, 'expected episode', 'expected episodes')} · {availableCount.toLocaleString(locale)} available · {(saved.Entries.length - availableCount).toLocaleString(locale)} missing</Typography>
              <Typography variant="body2" color="text.secondary">{saved.Entries.filter((entry) => entry.Airing === 'aired').length.toLocaleString(locale)} aired · {saved.Entries.filter((entry) => entry.Airing === 'unaired').length.toLocaleString(locale)} unaired · {formatCount(saved.Entries.filter((entry) => entry.Airing === 'unknown').length, 'unknown air date', 'unknown air dates')}</Typography>
              <Typography variant="caption" color="text.secondary">{formatCount(saved.RetiredCount, 'retired entry', 'retired entries')} · Saved revision {saved.Revision}</Typography>
              {saved.Source && <><Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>Source: {saved.Source.Label || saved.Source.Key} · {saved.Source.Key} · Version {saved.Source.Revision}</Typography><Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Imported by an administrator · Parser {saved.Source.ParserVersion} · SHA-256 {saved.Source.SHA256}</Typography></>}
              {saved.State === 'absent' && <Typography variant="body2" color="text.secondary">Import an explicit episode list to establish which episodes are expected.</Typography>}
            </Stack></Paper>
            <Box component="section" aria-label="Episode roster preview"><Typography component="h3" variant="h4" sx={{ mb: 1 }}>Roster preview</Typography>
              {!preview && <Typography variant="body2" color="text.secondary">Complete the source fields and fix the JSON to review the full replacement.</Typography>}
              {preview && <><Typography variant="body2" sx={{ mb: 1.5 }}>{formatCount(preview.length, 'entry', 'entries')} in this draft · {preview.filter((entry) => entry.PremiereDate === undefined).length.toLocaleString(locale)} without a known air date</Typography><Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1.5 }}>Availability is evaluated after saving against the current catalog. Dates are interpreted in UTC.</Typography>
                <div className="episode-roster-seasons" role="group" aria-label={t('Season')}>
                  <button type="button" className="editor-season-button" aria-pressed={season === 'all'} onClick={() => { setSeason('all'); setPage(0); }}>{t('All seasons')}</button>
                  {seasons.map((value) => <button key={value} type="button" className="editor-season-button" aria-pressed={season === value} onClick={() => { setSeason(value); setPage(0); }}>{value === 0 ? t('Specials') : t('Season') + ' ' + value.toLocaleString(locale)}</button>)}
                </div>
                <TableContainer component={Paper} variant="outlined"><Table size="small" aria-label="Episode roster preview" sx={{ minWidth: 0 }}><TableHead><TableRow><TableCell>Season</TableCell><TableCell>Episode</TableCell><TableCell>Name and key</TableCell><TableCell>Premiere date</TableCell>{showSavedStatus && <TableCell>Saved status</TableCell>}</TableRow></TableHead><TableBody>{visibleEntries!.slice(page * 25, (page + 1) * 25).map((entry) => <TableRow key={entry.Key}><TableCell>{entry.SeasonNumber}</TableCell><TableCell>{entry.EpisodeNumber}</TableCell><TableCell sx={{ maxWidth: 340, overflowWrap: 'anywhere' }}><Typography variant="body2" translate="no">{entry.Name || `Episode ${entry.EpisodeNumber}`}</Typography><Typography variant="caption" color="text.secondary" translate="no">{entry.Key}</Typography></TableCell><TableCell sx={{ whiteSpace: 'nowrap' }}>{entry.PremiereDate ?? 'Unknown'}</TableCell>{showSavedStatus && <TableCell><Typography variant="body2">{savedEntries.get(entry.Key)?.Availability === 'available' ? 'Available' : 'Missing'}</Typography><Typography variant="caption" color="text.secondary">{savedEntries.get(entry.Key)?.Airing === 'aired' ? 'Aired' : savedEntries.get(entry.Key)?.Airing === 'unaired' ? 'Unaired' : 'Unknown air date'}{(savedEntries.get(entry.Key)?.AvailableItemCount ?? 0) > 1 ? ` · ${savedEntries.get(entry.Key)!.AvailableItemCount} files` : ''}</Typography></TableCell>}</TableRow>)}</TableBody></Table></TableContainer>
                {preview.length === 0 && <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>This replacement contains no expected episodes.</Typography>}
                <TablePagination component="div" count={visibleEntries!.length} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} onPageChange={(_event, next) => setPage(next)} sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap' } }} />
              </>}
            </Box>
            <Box component="details" className="episode-roster-source" open={saved.State !== 'active'}>
              <Typography component="summary" variant="body2">Import & source</Typography>
              <Stack spacing={2} sx={{ pt: 2 }}>
            <Typography variant="body2" color="text.secondary">Each save replaces the complete expected episode list. Keep entry keys stable across source versions. Episodes omitted from a replacement are retired. Numbering gaps alone do not establish missing episodes.</Typography>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
              <TextField fullWidth required label="Source key" value={draft.key} disabled={disabled} onChange={(event) => change('key', event.target.value)} error={Boolean(errorFor('Source.Key'))} helperText={errorFor('Source.Key') ?? 'Stable source identifier, up to 128 UTF-8 bytes.'} />
              <TextField fullWidth required label="Source version" value={draft.sourceRevision} disabled={disabled} onChange={(event) => change('sourceRevision', event.target.value)} error={Boolean(errorFor('Source.Revision'))} helperText={errorFor('Source.Revision') ?? 'Use a new version when changing this source content.'} />
            </Stack>
            <TextField fullWidth label="Source label" value={draft.label} disabled={disabled} onChange={(event) => change('label', event.target.value)} error={Boolean(errorFor('Source.Label'))} helperText={errorFor('Source.Label') ?? 'Optional display label, up to 256 UTF-8 bytes.'} />
            <TextField fullWidth multiline minRows={6} maxRows={14} label="Episode entries JSON" value={draft.entries} disabled={disabled} onChange={(event) => change('entries', event.target.value)} error={Boolean(errorFor('Entries'))} helperText={errorFor('Entries') ?? 'An array of up to 2,000 entries. Each entry needs Key, SeasonNumber, EpisodeNumber, and Name. PremiereDate is optional; omit it when unknown.'} slotProps={{ htmlInput: { spellCheck: false, maxLength: episodeRosterMaximumBytes, style: { fontFamily: 'ui-monospace, Consolas, monospace' } } }} />
            {entryErrors.length > 0 && <Alert severity="error"><Stack spacing={0.5}>{entryErrors.slice(0, 5).map((field) => <Typography key={field} variant="body2">{field}: {fieldError(error, field)}</Typography>)}{entryErrors.length > 5 && <Typography variant="body2">Review the remaining entry errors before saving.</Typography>}</Stack></Alert>}
            <Box><Button component="label" variant="outlined" disabled={disabled}>Import roster JSON<input type="file" accept="application/json,.json" aria-label="Import roster JSON" hidden disabled={disabled} onChange={(event) => void importFile(event)} /></Button><Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>Maximum 512 KiB including source and entries. Import prepares a draft for review.</Typography>{importError && <Alert severity="error" sx={{ mt: 1 }}>{importError}</Alert>}</Box>
            <Box component="details"><Typography component="summary" variant="body2" sx={{ cursor: 'pointer' }}>JSON import format</Typography><Typography component="pre" variant="caption" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', p: 1.5, bgcolor: 'background.default' }}>{example}</Typography></Box>

              </Stack>
            </Box>
          </>}
        </Stack></DialogContent>
        <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{dirty ? 'Unsaved episode roster' : saved ? 'All roster changes saved' : ''}</Typography><Button color="warning" onClick={() => setConfirmWithdraw(true)} disabled={disabled || saved?.State !== 'active'}>Withdraw roster</Button><Button color="secondary" onClick={reload} disabled={loading || busy || importing}>Reload</Button><Button color="secondary" onClick={close} disabled={busy || importing}>Close</Button><Button type="submit" variant="contained" disabled={disabled || !saved || !dirty || !validation.input} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving roster...' : 'Save roster'}</Button></DialogActions>
      </Box>
    </Dialog>
    {confirmWithdraw && <Dialog open role="alertdialog" maxWidth="xs" fullWidth onClose={() => { if (!busy) setConfirmWithdraw(false); }} aria-labelledby="withdraw-episode-roster-title" aria-describedby="withdraw-episode-roster-description"><DialogTitle id="withdraw-episode-roster-title">Withdraw episode roster?</DialogTitle><DialogContent><Stack spacing={2}><Typography id="withdraw-episode-roster-description">Withdraw the expected episode list for {saved?.SeriesName || seriesName}? Its entries will be retired and its source history retained. Media files and their metadata stay in the library.</Typography>{dirty && <Alert severity="warning">Your unsaved roster draft will also be discarded.</Alert>}</Stack></DialogContent><DialogActions sx={{ p: 3, gap: 1 }}><Button autoFocus color="secondary" disabled={busy} onClick={() => setConfirmWithdraw(false)}>Keep roster</Button><Button variant="contained" color="warning" disabled={busy} onClick={() => void mutate(true)}>Withdraw expected episodes</Button></DialogActions></Dialog>}
  </>;
}
