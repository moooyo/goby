import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Paper, Skeleton, Stack, Typography } from '@mui/material';
import { isAbortError } from './api';
import { ErrorNotice } from './components';
import { mediaOperationsApi, mediaOperationIsActive, mediaOperationNeedsReload, mediaOperationRequestId } from './mediaOperationsApi';
import type { MediaOperation, MediaOperationApplyRequest } from './mediaOperationsApi';
import { MediaOperationNotice, MediaOperationProgress, MediaOperationStatusChip, MediaOperationSummary, mediaOperationKindLabel } from './MediaOperationStatus';
import { MediaOperationReview } from './MediaOperationReview';
import type { MediaReviewState } from './MediaOperationReview';
import { TaskTimestamp } from './TaskRunDialog';
import { useTaskResource } from './useTaskResource';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

type Mutation = 'cancel' | 'apply' | 'recover';
const pollOperation = (operation: MediaOperation) => mediaOperationIsActive(operation);
const emptyReview: MediaReviewState = { dirty: false, busy: false, blocked: false };

export function MediaOperationDialog({ operationId, onClose, onChanged, onNavigationGuardChange }: {
  operationId: string; onClose: () => void; onChanged: () => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const [operation, setOperation] = useState<MediaOperation>();
  const [busy, setBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [reviewState, setReviewState] = useState<MediaReviewState>(emptyReview);
  const [error, setError] = useState<unknown>(null);
  const [reviewRequired, setReviewRequired] = useState(false);
  const [confirming, setConfirming] = useState<Mutation>();
  const [confirmationSnapshot, setConfirmationSnapshot] = useState<Pick<MediaOperation, 'Revision' | 'SourceRevision' | 'ResultHash'>>();
  const [reloadReview, setReloadReview] = useState(0);
  const [notice, setNotice] = useState('');
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const refreshPending = useRef(false);
  const applyRequest = useRef<{ action: 'apply' | 'recover'; input: MediaOperationApplyRequest } | undefined>(undefined);
  const load = useCallback((signal: AbortSignal) => mediaOperationsApi.get(operationId, { signal }), [operationId]);
  const resource = useTaskResource({ key: operationId, load, poll: pollOperation, enabled: !busy && !reviewState.dirty && !reviewState.busy });
  const acceptOperation = useCallback((next: MediaOperation) => setOperation((current) => !current || BigInt(next.Revision) >= BigInt(current.Revision) ? next : current), []);
  useEffect(() => {
    if (!resource.data) return;
    acceptOperation(resource.data);
    if (refreshPending.current) { refreshPending.current = false; setRefreshing(false); setReviewRequired(false); }
  }, [resource.data, acceptOperation]);
  useEffect(() => {
    if (resource.error != null && refreshPending.current) { refreshPending.current = false; setRefreshing(false); setReviewRequired(true); }
  }, [resource.error]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useUserDraftNavigation(reviewState.dirty, busy || reviewState.busy, onNavigationGuardChange, 'Discard unsaved OCR cue changes and leave this operation?');
  const blocked = busy || refreshing || reviewRequired || reviewState.busy || reviewState.dirty || resource.error != null;
  const reviewCurrent = operation?.Kind !== 'subtitle_ocr' || (reviewState.revision === operation.Revision && reviewState.resultHash === operation.ResultHash);
  const confirmationChanged = Boolean(confirming && confirming !== 'cancel' && operation && confirmationSnapshot && (operation.Revision !== confirmationSnapshot.Revision || operation.SourceRevision !== confirmationSnapshot.SourceRevision || operation.ResultHash !== confirmationSnapshot.ResultHash));
  const confirmationAvailable = Boolean(operation && confirming && (confirming === 'cancel' ? operation.CanCancel : confirming === 'recover' ? operation.CanRecover : operation.CanApply));
  function close() { if (!inFlight.current && !reviewState.busy && (!reviewState.dirty || window.confirm('Discard unsaved OCR cue changes and close this operation?'))) onClose(); }
  function refresh() {
    if (inFlight.current || refreshPending.current || reviewState.busy || (reviewState.dirty && !window.confirm('Discard unsaved OCR cue changes and reload this operation?'))) return;
    refreshPending.current = true; setRefreshing(true);
    setError(null); setNotice(''); setReviewState(emptyReview); setReloadReview((value) => value + 1); resource.reload();
  }
  function request(action: Mutation) {
    if (!operation || blocked || (action === 'apply' && (reviewState.blocked || !reviewCurrent)) || (action === 'cancel' ? !operation.CanCancel : action === 'recover' ? !operation.CanRecover : !operation.CanApply)) return;
    setConfirmationSnapshot({ Revision: operation.Revision, SourceRevision: operation.SourceRevision, ResultHash: operation.ResultHash }); setConfirming(action);
  }
  async function mutate() {
    if (inFlight.current || !operation || !confirming || blocked || confirmationChanged) return;
    const action = confirming;
    if ((action === 'apply' && (reviewState.blocked || !reviewCurrent)) || (action === 'cancel' ? !operation.CanCancel : action === 'recover' ? !operation.CanRecover : !operation.CanApply)) return;
    inFlight.current = true; setBusy(true); setError(null); setNotice('');
    try {
      let result: MediaOperation;
      if (action === 'cancel') result = await mediaOperationsApi.cancel(operation.Id, operation.Revision);
      else {
        if (!applyRequest.current || applyRequest.current.action !== action || applyRequest.current.input.Revision !== operation.Revision) applyRequest.current = { action, input: { Revision: operation.Revision, SourceRevision: operation.SourceRevision, ResultHash: operation.ResultHash, RequestId: mediaOperationRequestId() } };
        result = (await mediaOperationsApi.publish(operation.Id, action, applyRequest.current.input)).Operation;
      }
      if (!mounted.current) return;
      acceptOperation(result); setNotice(action === 'cancel' ? 'Cancellation requested. Follow the final operation status.' : action === 'recover' ? 'Recovery requested. Follow the final publication status.' : 'Apply requested. Follow the final publication status.');
      onChanged(); resource.reload();
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause); if (mediaOperationNeedsReload(cause)) setReviewRequired(true);
    } finally { inFlight.current = false; if (mounted.current) { setBusy(false); setConfirming(undefined); } }
  }
  const confirmationTitle = confirming === 'cancel' ? 'Cancel media operation?' : confirming === 'recover' ? 'Recover publication?' : 'Apply reviewed result?';
  return <>
    <Dialog open fullWidth maxWidth="lg" onClose={close} aria-labelledby="media-operation-title" slotProps={{ paper: { sx: { maxWidth: 1060 } } }}>
      <DialogTitle id="media-operation-heading"><Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}><Typography component="span" variant="h3" id="media-operation-title">Media operation</Typography>{operation && <MediaOperationStatusChip operation={operation} />}</Stack><Typography variant="body2" color="text.secondary">{operation ? mediaOperationKindLabel(operation.Kind) : 'Loading operation...'}</Typography></DialogTitle>
      <DialogContent aria-busy={busy || resource.loading}><Stack spacing={3} sx={{ pt: 1 }}>
        {resource.error != null && <ErrorNotice error={resource.error} retry={refresh} />}
        {error != null && <ErrorNotice error={error} />}
        {reviewRequired && <Alert severity="warning" action={<Button color="inherit" onClick={refresh}>Reload operation</Button>}>The operation changed or the request could not be confirmed. Reload and review its current state before taking another action.</Alert>}
        {notice && <Alert severity="info">{notice}</Alert>}
        {!operation && resource.loading && <Skeleton variant="rounded" height={260} aria-label="Loading media operation" />}
        {operation && <>
          <MediaOperationNotice operation={operation} />
          <Paper variant="outlined" sx={{ p: 2.5 }}><MediaOperationProgress operation={operation} /></Paper>
          <Typography variant="caption" color="text.secondary">{resource.paused ? 'Updates pause while this tab is hidden.' : mediaOperationIsActive(operation) && resource.error == null ? 'Updates every 5 seconds while processing is active.' : 'This is the recorded operation state.'}</Typography>
          <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' }, m: 0, gap: 1.5 }}>
            {([['Item', operation.ItemId], ['Media source', operation.MediaSourceId], ['Subtitle stream', String(operation.StreamIndex)], ['Publication', operation.Applied ? 'Published to catalog' : 'Not published']] as const).map(([label, value]) => <Box key={label}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere' }}>{value}</Typography></Box>)}
            {([['Created', operation.CreatedAt], ['Started', operation.StartedAt], ['Finished', operation.FinishedAt]] as const).map(([label, value]) => <Box key={label}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0 }}><TaskTimestamp value={value} /></Typography></Box>)}
          </Box>
          <MediaOperationSummary operation={operation} />
          {operation.Kind === 'subtitle_ocr' && operation.ResultHash && <><Divider /><MediaOperationReview operation={operation} reloadKey={reloadReview} onOperation={acceptOperation} onChanged={onChanged} onStateChange={setReviewState} /></>}
          {reviewState.dirty && <Alert severity="info">Save or discard cue changes before applying, cancelling, or recovering this operation.</Alert>}
          <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Operation {operation.Id}</Typography>
        </>}
      </Stack></DialogContent>
      <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap', borderTop: 1, borderColor: 'divider' }}><Button disabled={busy || refreshing || reviewState.busy} onClick={refresh}>Refresh operation</Button><Button color="secondary" disabled={busy || reviewState.busy} onClick={close}>Close</Button>{operation?.CanCancel && <Button color="error" disabled={blocked} onClick={() => request('cancel')}>Cancel operation</Button>}{operation?.CanRecover && <Button variant="contained" color="warning" disabled={blocked} onClick={() => request('recover')}>Recover publication</Button>}{operation?.CanApply && <Button variant="contained" disabled={blocked || reviewState.blocked || !reviewCurrent} onClick={() => request('apply')}>Apply reviewed result</Button>}</DialogActions>
    </Dialog>
    {confirming && operation && <Dialog open fullWidth maxWidth="sm" onClose={busy ? undefined : () => setConfirming(undefined)} aria-labelledby="media-operation-confirm-title"><DialogTitle id="media-operation-confirm-title">{confirmationTitle}</DialogTitle><DialogContent><Stack spacing={2}>
      <Typography variant="body2">{confirming === 'cancel' ? 'Stop this operation and discard work that has not been published. Publication already committed to the catalog is preserved.' : confirming === 'recover' ? 'Resume server-managed recovery for this operation. Recovery may finish publishing the prepared result or restore the original media according to the recorded publication state.' : operation.Kind === 'subtitle_ocr' ? 'Publish the reviewed, included cues as a new subtitle for this exact media source. Check every review page before applying.' : `Permanently remove embedded subtitle stream ${operation.StreamIndex} by replacing this media file with the prepared copy. The server retains the original file for publication recovery.`}</Typography>
      {confirming !== 'cancel' && operation.Kind === 'remove_embedded_subtitle' && <Alert severity="warning">This changes the media file. A database backup does not include the original media. Only proceed after reviewing the prepared result.</Alert>}
      {confirmationChanged && <Alert severity="warning">The operation changed while this confirmation was open. Close this confirmation and review the current result before continuing.</Alert>}
      {!confirmationAvailable && <Alert severity="info">This action is no longer available in the current operation state. Close this confirmation to review its status.</Alert>}
      <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Item {operation.ItemId} · Source {operation.MediaSourceId} · Stream {operation.StreamIndex}</Typography>
    </Stack></DialogContent><DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus color="secondary" disabled={busy} onClick={() => setConfirming(undefined)}>Keep reviewing</Button><Button variant="contained" color={confirming === 'cancel' ? 'error' : confirming === 'recover' ? 'warning' : 'primary'} disabled={blocked || confirmationChanged || !confirmationAvailable || (confirming === 'apply' && (reviewState.blocked || !reviewCurrent))} onClick={() => void mutate()} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{busy ? 'Submitting...' : confirming === 'cancel' ? 'Confirm cancellation' : confirming === 'recover' ? 'Confirm recovery' : 'Confirm apply'}</Button></DialogActions></Dialog>}
  </>;
}
