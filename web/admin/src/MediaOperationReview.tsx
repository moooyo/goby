import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, FormControlLabel, Paper, Skeleton, Stack, TablePagination, TextField, Typography } from '@mui/material';
import { isAbortError } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { mediaCueImageUrl, mediaOperationsApi, mediaOperationNeedsReload, mediaSecondsToTicks, mediaTicksToSeconds } from './mediaOperationsApi';
import type { MediaOperation, MediaOperationCue, MediaOperationCueEdit, MediaOperationCuePage } from './mediaOperationsApi';

interface CueDraft { ordinal: number; start: string; end: string; text: string; included: boolean }
export interface MediaReviewState { dirty: boolean; busy: boolean; blocked: boolean; revision?: string; resultHash?: string }
const draftFor = (cue: MediaOperationCue): CueDraft => ({ ordinal: cue.Ordinal, start: mediaTicksToSeconds(cue.StartTicks), end: mediaTicksToSeconds(cue.EndTicks), text: cue.Text, included: cue.Included });
function cueErrors(draft: CueDraft) {
  const start = mediaSecondsToTicks(draft.start); const end = mediaSecondsToTicks(draft.end);
  return {
    start: start === undefined ? 'Use nonnegative seconds with up to 7 decimal places.' : undefined,
    end: end === undefined ? 'Use nonnegative seconds with up to 7 decimal places.' : start !== undefined && BigInt(end) <= BigInt(start) ? 'The end must be after the start.' : undefined,
    text: new TextEncoder().encode(draft.text).length > 4096 ? 'Use at most 4,096 UTF-8 bytes per cue.' : /[\u0000-\u0009\u000b-\u001f\u007f-\u009f]/.test(draft.text) ? 'Line breaks are allowed; remove other control characters.' : draft.included && !draft.text.trim() ? 'Enter text or exclude this cue.' : undefined,
  };
}
function CueImage({ operationId, cue }: { operationId: string; cue: MediaOperationCue }) {
  const [failed, setFailed] = useState(false);
  if (!cue.ImageSHA256) return <Typography variant="body2" color="text.secondary">No source image was retained for this cue.</Typography>;
  if (failed) return <Alert severity="warning">The original cue image could not be loaded. Reload the review to try again.</Alert>;
  return <Box component="img" loading="lazy" alt={`Original bitmap subtitle for cue ${cue.Ordinal + 1}`} src={mediaCueImageUrl(operationId, cue.Ordinal)} referrerPolicy="same-origin" onError={() => setFailed(true)} sx={{ display: 'block', maxWidth: '100%', maxHeight: 240, width: 'auto', objectFit: 'contain', bgcolor: 'grey.900', borderRadius: 1 }} />;
}

export function MediaOperationReview({ operation, reloadKey, onOperation, onChanged, onStateChange }: {
  operation: MediaOperation; reloadKey: number; onOperation: (operation: MediaOperation) => void; onChanged: () => void; onStateChange: (state: MediaReviewState) => void;
}) {
  const [page, setPage] = useState(0);
  const [limit, setLimit] = useState(10);
  const [reloadRevision, setReloadRevision] = useState(0);
  const [saved, setSaved] = useState<MediaOperationCuePage>();
  const [drafts, setDrafts] = useState<CueDraft[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false);
  const [notice, setNotice] = useState('');
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const onOperationRef = useRef(onOperation);
  onOperationRef.current = onOperation;
  const dirty = Boolean(saved && JSON.stringify(drafts) !== JSON.stringify(saved.Items.map(draftFor)));
  const invalid = drafts.some((draft) => Object.values(cueErrors(draft)).some(Boolean));
  const stale = Boolean(saved && (saved.Operation.Revision !== operation.Revision || saved.Operation.ResultHash !== operation.ResultHash));
  const disabled = loading || busy || review || stale || !operation.CanReview;
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => { onStateChange({ dirty, busy: loading || busy, blocked: review || error != null || invalid || stale, revision: saved?.Operation.Revision, resultHash: saved?.Operation.ResultHash }); }, [dirty, loading, busy, review, error, invalid, stale, saved?.Operation.Revision, saved?.Operation.ResultHash, onStateChange]);
  useEffect(() => () => onStateChange({ dirty: false, busy: false, blocked: false }), [onStateChange]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setSaved(undefined); setDrafts([]); setNotice('');
    void mediaOperationsApi.review(operation.Id, page * limit, limit, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      if (page > 0 && page * limit >= result.TotalRecordCount) { setPage(Math.max(0, Math.ceil(result.TotalRecordCount / limit) - 1)); return; }
      setSaved(result); setDrafts(result.Items.map(draftFor)); setReview(false); onOperationRef.current(result.Operation);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [operation.Id, page, limit, reloadRevision, reloadKey]);
  function discardAllowed(): boolean { return !inFlight.current && (!dirty || window.confirm('Discard unsaved cue changes on this page?')); }
  function reload() { if (discardAllowed()) setReloadRevision((value) => value + 1); }
  function change(ordinal: number, patch: Partial<CueDraft>) { setDrafts((current) => current.map((draft) => draft.ordinal === ordinal ? { ...draft, ...patch } : draft)); setError(null); setNotice(''); }
  async function save() {
    if (inFlight.current || !saved || disabled || !dirty || invalid) return;
    const edits: MediaOperationCueEdit[] = drafts.filter((draft, index) => JSON.stringify(draft) !== JSON.stringify(draftFor(saved.Items[index]))).map((draft) => ({ Ordinal: draft.ordinal, StartTicks: mediaSecondsToTicks(draft.start)!, EndTicks: mediaSecondsToTicks(draft.end)!, Text: draft.text, Included: draft.included }));
    inFlight.current = true; setBusy(true); setError(null); setNotice('');
    try {
      const result = await mediaOperationsApi.saveReview(operation.Id, saved.Operation.Revision, edits);
      if (!mounted.current) return;
      const items = saved.Items.map((cue) => { const edit = edits.find((entry) => entry.Ordinal === cue.Ordinal); return edit ? { ...cue, ...edit } : cue; });
      setSaved({ ...saved, Operation: result, Items: items }); setDrafts(items.map(draftFor));
      setNotice('Cue changes saved. Review the remaining pages before applying.'); onOperationRef.current(result); onChanged();
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause); if (mediaOperationNeedsReload(cause)) setReview(true);
    } finally { inFlight.current = false; if (mounted.current) setBusy(false); }
  }
  return <Stack component="section" aria-label="OCR cue review" spacing={2}>
    <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', gap: 1 }}><Box><Typography component="h3" variant="h4">OCR cue review</Typography><Typography variant="body2" color="text.secondary">Compare the bitmap and original recognition with the text to publish. Save each edited page before changing pages.</Typography></Box><Button onClick={reload} disabled={loading || busy}>Reload review</Button></Stack>
    {error != null && <ErrorNotice error={error} />}
    {review && <Alert severity="warning">The review changed or its save could not be confirmed. Your draft is kept. Reload the review and check the saved cues before editing or applying again.</Alert>}
    {stale && !review && <Alert severity="warning">This operation changed after these cues were loaded. Your draft is kept. Reload the review before editing or applying the newer result.</Alert>}
    {notice && <Alert severity="success">{notice}</Alert>}
    {loading && <Skeleton variant="rounded" height={250} aria-label="Loading OCR cues" />}
    {saved && saved.Items.length === 0 && <Alert severity="info">No recognized cues are available for this operation.</Alert>}
    {saved?.Items.map((cue, index) => {
      const draft = drafts[index]; if (!draft) return null;
      const errors = cueErrors(draft);
      const editIndex = drafts.filter((entry, draftIndex) => JSON.stringify(entry) !== JSON.stringify(draftFor(saved.Items[draftIndex]))).findIndex((entry) => entry.ordinal === cue.Ordinal);
      const serverTextError = editIndex >= 0 ? fieldError(error, `Edits[${editIndex}].Text`) : undefined;
      return <Paper key={`${operation.Id}:${cue.Ordinal}:${reloadRevision}:${reloadKey}`} variant="outlined" component="section" aria-label={`Cue ${cue.Ordinal + 1}`} sx={{ p: { xs: 2, sm: 2.5 } }}><Stack spacing={2}>
        <Stack direction="row" sx={{ justifyContent: 'space-between', gap: 1, flexWrap: 'wrap' }}><Typography component="h4" variant="h4">Cue {cue.Ordinal + 1}</Typography><Typography variant="body2" color="text.secondary">{cue.Confidence === null ? 'Confidence unavailable' : `Confidence: ${cue.Confidence.toFixed(1)}%`}{cue.IsForced ? ' · Forced' : ''}{cue.IsHearingImpaired ? ' · Hearing impaired' : ''}</Typography></Stack>
        <CueImage operationId={operation.Id} cue={cue} />
        <Box><Typography variant="caption" color="text.secondary">Original recognition · {mediaTicksToSeconds(cue.OriginalStartTicks)}–{mediaTicksToSeconds(cue.OriginalEndTicks)} seconds</Typography><Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{cue.OriginalText || 'No text recognized'}</Typography></Box>
        {cue.Warnings.map((warning, warningIndex) => <Alert key={`${warningIndex}:${warning}`} severity="warning">{warning}</Alert>)}
        <FormControlLabel control={<Checkbox disabled={disabled} checked={draft.included} onChange={(event) => change(cue.Ordinal, { included: event.target.checked })} />} label={`Include cue ${cue.Ordinal + 1}`} />
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}><TextField fullWidth label={`Cue ${cue.Ordinal + 1} start (seconds)`} value={draft.start} disabled={disabled} onChange={(event) => change(cue.Ordinal, { start: event.target.value })} error={Boolean(errors.start)} helperText={errors.start ?? 'Up to 7 decimal places.'} slotProps={{ htmlInput: { inputMode: 'decimal' } }} /><TextField fullWidth label={`Cue ${cue.Ordinal + 1} end (seconds)`} value={draft.end} disabled={disabled} onChange={(event) => change(cue.Ordinal, { end: event.target.value })} error={Boolean(errors.end)} helperText={errors.end ?? 'Must be after the start.'} slotProps={{ htmlInput: { inputMode: 'decimal' } }} /></Stack>
        <TextField multiline minRows={2} maxRows={10} fullWidth label={`Cue ${cue.Ordinal + 1} text`} value={draft.text} disabled={disabled} onChange={(event) => change(cue.Ordinal, { text: event.target.value })} error={Boolean(errors.text || serverTextError)} helperText={errors.text ?? serverTextError ?? 'Plain subtitle text, up to 4,096 UTF-8 bytes. Excluded cues are not published.'} />
      </Stack></Paper>;
    })}
    {saved && <TablePagination component="div" count={saved.TotalRecordCount} page={page} rowsPerPage={limit} rowsPerPageOptions={[10, 20, 50]} labelRowsPerPage="Cues per page:" onPageChange={(_, next) => { if (discardAllowed()) setPage(next); }} onRowsPerPageChange={(event) => { if (discardAllowed()) { setLimit(Number(event.target.value)); setPage(0); } }} disabled={loading || busy} sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', px: 0 } }} />}
    <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}><Typography variant="caption" color="text.secondary">{dirty ? 'Unsaved cue changes on this page' : 'All displayed cues saved'}</Typography><Button variant="contained" disabled={disabled || !dirty || invalid} onClick={() => void save()} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{busy ? 'Saving cues...' : 'Save cue changes'}</Button></Stack>
  </Stack>;
}
