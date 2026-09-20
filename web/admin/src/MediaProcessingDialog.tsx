import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, MenuItem, Skeleton, Stack, TextField, Typography } from '@mui/material';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { mediaOperationsApi, mediaOperationNeedsReload, mediaOperationRequestId } from './mediaOperationsApi';
import type { MediaOperationKind, MediaOperationRequest, MediaProcessingStream, MediaProcessingTarget } from './mediaOperationsApi';
import { MediaOperationDialog } from './MediaOperationDialog';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

function streamLabel(stream: MediaProcessingStream): string {
  return `Stream ${stream.Index} · ${stream.Codec}${stream.Language ? ` · ${stream.Language}` : ''}${stream.Title ? ` · ${stream.Title}` : ''}`;
}
const modelLabels: Record<string, string> = { eng: 'English', chi_sim: 'Simplified Chinese', chi_tra: 'Traditional Chinese' };

export function MediaProcessingDialog({ itemId, itemName, onClose, onNavigationGuardChange }: {
  itemId: string; itemName: string; onClose: () => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const [target, setTarget] = useState<MediaProcessingTarget>();
  const [kind, setKind] = useState<MediaOperationKind>('remove_embedded_subtitle');
  const [streamIndex, setStreamIndex] = useState('');
  const [profile, setProfile] = useState('');
  const [models, setModels] = useState<string[]>([]);
  const [format, setFormat] = useState('srt');
  const [language, setLanguage] = useState('');
  const [title, setTitle] = useState('');
  const [isDefault, setIsDefault] = useState(false);
  const [isForced, setIsForced] = useState(false);
  const [hearingImpaired, setHearingImpaired] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [touched, setTouched] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [reloadRevision, setReloadRevision] = useState(0);
  const [operationId, setOperationId] = useState<string>();
  const pendingRequest = useRef<MediaOperationRequest | undefined>(undefined);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const streams = target?.Streams.filter((stream) => stream.CodecType.toLowerCase() === 'subtitle' && !stream.IsExternal && (kind !== 'subtitle_ocr' || ['hdmv_pgs_subtitle', 'pgssub', 'dvd_subtitle', 'dvdsub'].includes(stream.Codec.toLowerCase()))) ?? [];
  const stream = streams.find((entry) => String(entry.Index) === streamIndex);
  const canPrepare = target?.Capabilities.Enabled && target.Capabilities.Available && Boolean(stream)
    && (kind === 'subtitle_ocr' ? target.Capabilities.OCR.Available && models.length > 0 && target.Capabilities.OCR.OutputFormats.includes(format) : target.Capabilities.WritableProfiles.includes(profile));
  const disabled = busy || loading || review || uncertain;
  const invalidLanguage = kind === 'subtitle_ocr' && language !== '' && (language.length > 32 || !/^[A-Za-z0-9-]+$/.test(language));
  const invalidTitle = kind === 'subtitle_ocr' && (new TextEncoder().encode(title.trim()).length > 512 || /\p{Cc}/u.test(title));
  useUserDraftNavigation(!operationId && touched, !operationId && busy, onNavigationGuardChange, 'Discard the media processing setup and leave this page?');
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setTarget(undefined);
    void mediaOperationsApi.target(itemId, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      const preferredProfile = result.Container.split(',').some((container) => ['mov', 'mp4', 'm4a', '3gp', '3g2', 'mj2'].includes(container)) ? 'mp4-movtext-v1' : 'matroska-v1';
      setTarget(result); setStreamIndex(''); setModels([]); setProfile(result.Capabilities.WritableProfiles.includes(preferredProfile) ? preferredProfile : result.Capabilities.WritableProfiles[0] ?? '');
      setFormat(result.Capabilities.OCR.OutputFormats[0] ?? ''); setLanguage(''); setTitle(''); setIsDefault(false); setIsForced(false); setHearingImpaired(false);
      setReview(false); setUncertain(false); setTouched(false); pendingRequest.current = undefined;
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [itemId, reloadRevision]);
  function change() { setTouched(true); setError(null); }
  function close() { if (!inFlight.current && (!touched || window.confirm(uncertain ? 'The preparation may already be queued. Close and check Media processing in Tasks?' : 'Discard the media processing setup?'))) onClose(); }
  function reload() {
    if (!inFlight.current && !uncertain && (!touched || window.confirm('Discard this setup and reload the current source?'))) setReloadRevision((value) => value + 1);
  }
  async function prepare(retry = false) {
    if (inFlight.current || (!retry && (!target || !canPrepare || invalidLanguage || invalidTitle || disabled))) return;
    const input = retry ? pendingRequest.current : {
      RequestId: mediaOperationRequestId(), Kind: kind, MediaSourceId: target!.MediaSourceId, SourceRevision: target!.SourceRevision,
      StreamIndex: Number(streamIndex), Parameters: kind === 'remove_embedded_subtitle' ? { Profile: profile }
        : { ModelIds: models, OutputFormat: format, Language: language, Title: title.trim(), IsDefault: isDefault, IsForced: isForced, IsHearingImpaired: hearingImpaired },
    } satisfies MediaOperationRequest;
    if (!input) return;
    pendingRequest.current = input; inFlight.current = true; setBusy(true); setError(null);
    try {
      const result = await mediaOperationsApi.prepare(itemId, input);
      if (mounted.current) { setTouched(false); setUncertain(false); setOperationId(result.Operation.Id); }
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setUncertain(true);
      else if (mediaOperationNeedsReload(cause)) { setReview(true); setUncertain(false); }
      else { pendingRequest.current = undefined; setUncertain(false); }
    } finally { inFlight.current = false; if (mounted.current) setBusy(false); }
  }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); void prepare(); }
  if (operationId) return <MediaOperationDialog operationId={operationId} onClose={onClose} onChanged={() => undefined} onNavigationGuardChange={onNavigationGuardChange} />;
  return <Dialog open fullWidth maxWidth="md" onClose={close} aria-labelledby="media-processing-title">
    <Box component="form" noValidate onSubmit={submit} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' }}>
      <DialogTitle id="media-processing-heading"><Typography component="span" variant="h3" id="media-processing-title">Prepare media processing</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>{itemName}</Typography></DialogTitle>
      <DialogContent aria-busy={loading || busy}><Stack spacing={2.5} sx={{ pt: 1 }}>
        {error != null && <ErrorNotice error={error} />}
        {review && <Alert severity="warning" action={<Button color="inherit" onClick={reload}>Reload source</Button>}>The source or operation state changed. Reload before preparing another result.</Alert>}
        {uncertain && <Alert severity="warning" action={<Button color="inherit" disabled={busy} onClick={() => void prepare(true)}>Retry same request</Button>}>The preparation response could not be confirmed. Retry the same request to find its existing operation without queuing a duplicate.</Alert>}
        {loading && <Skeleton variant="rounded" height={200} aria-label="Loading media processing options" />}
        {target && <>
          <Alert severity="info">Preparation creates a result for review. The source media changes only after you explicitly apply a ready result.</Alert>
          {(!target.Capabilities.Enabled || !target.Capabilities.Available) && <Alert severity="warning">{target.Capabilities.UnavailableReason || 'Media processing is unavailable on this server.'}</Alert>}
          <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>Current source: {target.MediaSourceId} · Container: {target.Container || 'Unknown'}</Typography>
          <TextField select fullWidth label="Processing operation" value={kind} disabled={disabled} onChange={(event) => { setKind(event.target.value as MediaOperationKind); setStreamIndex(''); change(); }}><MenuItem value="remove_embedded_subtitle">Remove embedded subtitle</MenuItem><MenuItem value="subtitle_ocr">Recognize bitmap subtitle (OCR)</MenuItem></TextField>
          <TextField select fullWidth label="Embedded subtitle stream" value={streamIndex} disabled={disabled || streams.length === 0} onChange={(event) => { setStreamIndex(event.target.value); const next = streams.find((entry) => String(entry.Index) === event.target.value); setLanguage(next?.Language ?? ''); setIsForced(next?.IsForced ?? false); setHearingImpaired(next?.IsHearingImpaired ?? false); change(); }} error={Boolean(fieldError(error, 'StreamIndex'))} helperText={fieldError(error, 'StreamIndex') ?? (streams.length === 0 ? 'No eligible embedded subtitle streams are available for this operation.' : 'Select the indexed stream to process.')}>{streams.map((entry) => <MenuItem key={entry.Index} value={String(entry.Index)}>{streamLabel(entry)}</MenuItem>)}</TextField>
          {kind === 'remove_embedded_subtitle' ? <>
            <TextField select fullWidth label="Writable container profile" value={profile} disabled={disabled || target.Capabilities.WritableProfiles.length === 0} onChange={(event) => { setProfile(event.target.value); change(); }} error={Boolean(fieldError(error, 'Parameters.Profile'))} helperText={fieldError(error, 'Parameters.Profile') ?? 'The server checks that the source can be safely rewritten using this profile.'}>{target.Capabilities.WritableProfiles.map((entry) => <MenuItem key={entry} value={entry}>{entry === 'matroska-v1' ? 'Matroska' : entry === 'mp4-movtext-v1' ? 'MP4 with mov_text subtitles' : entry}</MenuItem>)}</TextField>
            <Typography variant="body2" color="text.secondary">The prepared replacement removes only the selected embedded subtitle stream. Applying it replaces the original media file. A database backup does not back up media files.</Typography>
          </> : <>
            {!target.Capabilities.OCR.Available && <Alert severity="warning">{target.Capabilities.OCR.UnavailableReason || 'OCR tools and models are unavailable.'}</Alert>}
            <Box component="fieldset" sx={{ m: 0, p: 2, border: 1, borderColor: fieldError(error, 'Parameters.ModelIds') ? 'error.main' : 'divider', borderRadius: 1 }}><Typography component="legend" variant="body2">OCR language models</Typography>{target.Capabilities.OCR.Models.map((model) => <FormControlLabel key={model.Id} control={<Checkbox disabled={disabled} checked={models.includes(model.Id)} onChange={(event) => { setModels((current) => event.target.checked ? [...current, model.Id] : current.filter((id) => id !== model.Id)); change(); }} />} label={`${modelLabels[model.Language] ?? model.Language} (${model.Id})`} />)}{(models.length === 0 || fieldError(error, 'Parameters.ModelIds')) && <Typography variant="body2" color={fieldError(error, 'Parameters.ModelIds') ? 'error.main' : 'text.secondary'}>{fieldError(error, 'Parameters.ModelIds') ?? 'Select at least one installed model.'}</Typography>}</Box>
            <TextField select label="Subtitle output format" value={format} disabled={disabled} onChange={(event) => { setFormat(event.target.value); change(); }} error={Boolean(fieldError(error, 'Parameters.OutputFormat'))} helperText={fieldError(error, 'Parameters.OutputFormat')}>{target.Capabilities.OCR.OutputFormats.map((entry) => <MenuItem key={entry} value={entry}>{entry.toUpperCase()}</MenuItem>)}</TextField>
            <TextField label="Subtitle language" value={language} disabled={disabled} onChange={(event) => { setLanguage(event.target.value); change(); }} error={invalidLanguage || Boolean(fieldError(error, 'Parameters.Language'))} helperText={fieldError(error, 'Parameters.Language') ?? 'Use up to 32 ASCII letters, numbers, or hyphens, such as en, eng, or zh. Leave empty if unknown.'} />
            <TextField label="Subtitle title" value={title} disabled={disabled} onChange={(event) => { setTitle(event.target.value); change(); }} error={invalidTitle || Boolean(fieldError(error, 'Parameters.Title'))} helperText={invalidTitle ? 'Use at most 512 UTF-8 bytes without control characters.' : fieldError(error, 'Parameters.Title') ?? 'Optional name shown in the subtitle list.'} />
            <Stack>{[{ label: 'Default subtitle', value: isDefault, set: setIsDefault }, { label: 'Forced subtitle', value: isForced, set: setIsForced }, { label: 'Hearing impaired subtitle', value: hearingImpaired, set: setHearingImpaired }].map((flag) => <FormControlLabel key={flag.label} control={<Checkbox checked={flag.value} disabled={disabled} onChange={(event) => { flag.set(event.target.checked); change(); }} />} label={flag.label} />)}</Stack>
          </>}
        </>}
      </Stack></DialogContent>
      <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button color="secondary" disabled={busy || loading || uncertain} onClick={reload}>Reload source</Button><Button color="secondary" disabled={busy} onClick={close}>Close</Button><Button type="submit" variant="contained" disabled={disabled || !canPrepare || invalidLanguage || invalidTitle} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{busy ? 'Preparing request...' : 'Prepare for review'}</Button></DialogActions>
    </Box>
  </Dialog>;
}
