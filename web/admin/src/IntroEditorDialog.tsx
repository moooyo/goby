import { useEffect, useRef, useState } from 'react';
import type { ChangeEvent, FormEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Paper, Skeleton, Stack, TextField, Typography } from '@mui/material';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { introApi, parseIntroImport, secondsToTicks, ticksToSeconds } from './introApi';
import type { IntroInterval, ItemIntro } from './introApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

interface IntroDraft { start: string; end: string; provenance: 'Manual' | 'Import' }

function draftFor(intro: ItemIntro): IntroDraft {
  return {
    start: intro.Effective ? ticksToSeconds(intro.Effective.StartTicks) : '',
    end: intro.Effective ? ticksToSeconds(intro.Effective.EndTicks) : '',
    provenance: intro.Effective?.Provenance === 'Import' ? 'Import' : 'Manual',
  };
}

function intervalLabel(interval: IntroInterval | null): string {
  return interval ? `${ticksToSeconds(interval.StartTicks)}–${ticksToSeconds(interval.EndTicks)} seconds` : 'No interval';
}

export function IntroEditorDialog({ itemId, itemName, onClose, onNavigationGuardChange }: {
  itemId: string; itemName: string; onClose: () => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const [saved, setSaved] = useState<ItemIntro>();
  const [draft, setDraft] = useState<IntroDraft>({ start: '', end: '', provenance: 'Manual' });
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [importError, setImportError] = useState('');
  const [review, setReview] = useState(false);
  const [revision, setRevision] = useState(0);
  const [confirmReset, setConfirmReset] = useState(false);
  const [notice, setNotice] = useState('');
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const importSequence = useRef(0);
  const dirty = Boolean(saved && JSON.stringify(draft) !== JSON.stringify(draftFor(saved)));
  const startTicks = secondsToTicks(draft.start);
  const endTicks = secondsToTicks(draft.end);
  const startError = draft.start && startTicks === undefined ? 'Use nonnegative seconds with up to 7 decimal places.' : fieldError(error, 'StartTicks');
  const endError = draft.end && endTicks === undefined ? 'Use nonnegative seconds with up to 7 decimal places.'
    : startTicks !== undefined && endTicks !== undefined && endTicks <= startTicks ? 'The end must be after the start.'
      : saved && endTicks !== undefined && endTicks > saved.DurationTicks ? 'The end must be within this media duration.' : fieldError(error, 'EndTicks');
  const invalid = startTicks === undefined || endTicks === undefined || endTicks <= startTicks || Boolean(saved && endTicks > saved.DurationTicks);
  const disabled = loading || busy || importing || review;
  useUserDraftNavigation(dirty, busy || importing, onNavigationGuardChange, 'Discard the unsaved intro interval and leave this page?');

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; importSequence.current += 1; };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setSaved(undefined); setImportError('');
    void introApi.get(itemId, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      setSaved(result); setDraft(draftFor(result)); setReview(false); setNotice('');
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [itemId, revision]);

  function close() { if (!inFlight.current && !importing && (!dirty || window.confirm('Discard the unsaved intro interval?'))) onClose(); }
  function reload() {
    if (inFlight.current || importing || (dirty && !window.confirm('Discard your draft and reload the current media source and intro?'))) return;
    setConfirmReset(false); setRevision((value) => value + 1);
  }
  function change(field: 'start' | 'end', value: string) {
    setDraft((current) => ({ ...current, [field]: value, provenance: 'Manual' })); setError(null); setImportError(''); setNotice('');
  }
  async function importFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]; event.target.value = '';
    if (!file || !saved || disabled) return;
    const sequence = ++importSequence.current;
    setImportError(''); setNotice('');
    if (file.size > 16 * 1024) { setImportError('Choose an intro JSON file of at most 16 KiB.'); return; }
    setImporting(true);
    try {
      const interval = parseIntroImport(await file.text(), saved.DurationTicks);
      if (!mounted.current || sequence !== importSequence.current) return;
      setDraft({ start: ticksToSeconds(interval.StartTicks), end: ticksToSeconds(interval.EndTicks), provenance: 'Import' }); setError(null);
      setNotice('Imported interval ready for review. Save intro to apply it to this media source.');
    } catch (cause) {
      if (mounted.current && sequence === importSequence.current) setImportError(cause instanceof Error ? cause.message : 'The intro file could not be read.');
    } finally { if (mounted.current && sequence === importSequence.current) setImporting(false); }
  }
  async function mutate(reset: boolean) {
    if (inFlight.current || !saved || disabled || (!reset && (!dirty || invalid || startTicks === undefined || endTicks === undefined))) return;
    inFlight.current = true; setBusy(true); setError(null); setImportError(''); setNotice('');
    try {
      const identity = { Revision: saved.Revision, SourceRevision: saved.SourceRevision };
      const result = reset ? await introApi.reset(itemId, identity) : await introApi.update(itemId, { ...identity, StartTicks: startTicks!, EndTicks: endTicks!, Provenance: draft.provenance });
      if (!mounted.current) return;
      setSaved(result); setDraft(draftFor(result)); setReview(false);
      setNotice(reset ? 'Intro override removed. Current explicit chapter markers apply when available.' : 'Intro interval saved for this media source.');
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
    } finally {
      inFlight.current = false;
      if (mounted.current) { setBusy(false); setConfirmReset(false); }
    }
  }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); void mutate(false); }

  return <>
    <Dialog open fullWidth maxWidth="sm" onClose={close} aria-labelledby="intro-editor-title">
      <Box component="form" noValidate onSubmit={submit}>
        <DialogTitle><Typography component="span" variant="h3" id="intro-editor-title">Intro interval</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{itemName}</Typography></DialogTitle>
        <DialogContent aria-busy={loading || busy || importing}><Stack spacing={2.5} sx={{ pt: 0.5 }}>
          {error != null && <ErrorNotice error={error} />}
          {review && <Alert severity="warning" action={<Button color="inherit" onClick={reload}>Reload</Button>}>The intro or media source changed, or the save response could not be confirmed. Your draft is kept. Reload the current source and review its interval before saving again.</Alert>}
          {notice && <Alert severity={dirty ? 'info' : 'success'}>{notice}</Alert>}
          {loading && <Skeleton variant="rounded" height={260} aria-label="Loading intro interval" />}
          {saved && <>
            <Paper component="section" aria-label="Current intro source" variant="outlined" sx={{ p: 2 }}><Stack spacing={1}>
              <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">Current media source</Typography><Chip size="small" variant="outlined" label={saved.Effective?.Provenance ?? 'No intro'} /></Stack>
              <Typography variant="body2">Duration: {ticksToSeconds(saved.DurationTicks)} seconds</Typography>
              <Typography variant="body2">Active intro: {intervalLabel(saved.Effective)}</Typography>
              <Typography variant="body2" color="text.secondary">Explicit chapter markers: {intervalLabel(saved.Automatic)}</Typography>
              <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Media source: {saved.MediaSourceId}</Typography>
              {saved.Override && <Typography variant="body2" color="text.secondary">{saved.OverrideSource} override: {intervalLabel(saved.Override)}</Typography>}
              {saved.LastEditedAt && <Typography variant="caption" color="text.secondary">Last edited: {saved.LastEditedAt}{saved.LastEditedBy ? ` by ${saved.LastEditedBy}` : ''}</Typography>}
            </Stack></Paper>
            {saved.OverrideStale && <Alert severity="warning">The saved override belongs to an older media source and is inactive. Review the current source before saving a replacement, or reset to remove the old override.</Alert>}
            <Typography variant="body2" color="text.secondary">Set one intro interval for this movie or episode. Ordinary chapters do not identify an intro. The interval is tied to the current media source.</Typography>
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
              <TextField fullWidth label="Intro start (seconds)" value={draft.start} disabled={disabled} onChange={(event) => change('start', event.target.value)} error={Boolean(startError)} helperText={startError ?? 'Zero or later; up to 7 decimal places.'} slotProps={{ htmlInput: { inputMode: 'decimal' } }} />
              <TextField fullWidth label="Intro end (seconds)" value={draft.end} disabled={disabled} onChange={(event) => change('end', event.target.value)} error={Boolean(endError)} helperText={endError ?? 'After the start, within the media duration.'} slotProps={{ htmlInput: { inputMode: 'decimal' } }} />
            </Stack>
            <Typography variant="caption" color="text.secondary">Draft source: {draft.provenance === 'Import' ? 'Local JSON import' : 'Administrator edit'}. Compatible clients use the saved interval with their intro-skipping preference.</Typography>
            <Divider />
            <Box component="section" aria-label="Import intro"><Typography component="h3" variant="h4" sx={{ mb: 1 }}>Import an interval</Typography><Typography id="intro-import-help" variant="body2" color="text.secondary" sx={{ mb: 2 }}>Choose a local JSON file with one object containing StartTicks and EndTicks. There are 10,000,000 ticks per second. Import fills this form for review before saving.</Typography><Box component="input" type="file" accept=".json,application/json" aria-label="Import intro JSON" aria-describedby="intro-import-help" disabled={disabled} onChange={importFile} sx={{ display: 'block', width: '100%', minWidth: 0, font: 'inherit', fontSize: '0.875rem' }} />{importing && <Typography role="status" variant="body2" sx={{ mt: 1 }}>Reading intro file...</Typography>}{importError && <Alert severity="error" sx={{ mt: 2 }}>{importError}</Alert>}</Box>
            <Divider />
            <Box><Button variant="outlined" color="warning" disabled={disabled || !saved.Override} onClick={() => setConfirmReset(true)}>Reset to chapter markers</Button><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>Removes the saved manual or imported override. Only explicit intro chapter markers remain; if none exist, intro skipping is unavailable.</Typography></Box>
          </>}
        </Stack></DialogContent>
        <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{review ? 'Reload required before saving' : dirty ? 'Unsaved intro interval' : saved ? 'All changes saved' : ''}</Typography><Button onClick={reload} disabled={busy || loading || importing}>Reload</Button><Button color="secondary" onClick={close} disabled={busy || importing}>Close</Button><Button type="submit" variant="contained" disabled={disabled || !saved || !dirty || invalid} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving intro...' : 'Save intro'}</Button></DialogActions>
      </Box>
    </Dialog>
    {confirmReset && <Dialog open fullWidth maxWidth="xs" onClose={() => { if (!inFlight.current) setConfirmReset(false); }} aria-labelledby="reset-intro-title" aria-describedby="reset-intro-description">
      <DialogTitle id="reset-intro-title">Reset intro override?</DialogTitle><DialogContent><Stack spacing={2}><Typography id="reset-intro-description">Remove the saved override for {itemName}? The current explicit chapter interval will apply if available.</Typography>{dirty && <Alert severity="warning">Your unsaved interval will also be discarded.</Alert>}</Stack></DialogContent><DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus color="secondary" disabled={busy} onClick={() => setConfirmReset(false)}>Keep editing</Button><Button variant="contained" color="warning" disabled={busy} onClick={() => void mutate(true)} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>Reset intro</Button></DialogActions>
    </Dialog>}
  </>;
}
