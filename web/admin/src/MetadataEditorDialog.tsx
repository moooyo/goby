import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Paper, Skeleton, Stack, Tab, Tabs, TextField, Typography, useMediaQuery } from '@mui/material';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { MetadataDetail, MetadataFieldName } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { InactiveMetadataField, MetadataField, MetadataSection } from './MetadataField';
import { MetadataPeopleList, MetadataProviderList, MetadataStringList } from './MetadataCollections';
import { displayedDraft, draftOverrides, hasOverride, metadataDraftKey, metadataInput, metadataSummary } from './metadataDraft';
import type { MetadataDraftOverrides, MetadataDraftValues } from './metadataDraft';
import { colors, theme } from './theme';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

interface MetadataEditorDialogProps {
  itemId: string;
  onClose: () => void;
  onSaved: (detail: MetadataDetail) => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}

const labels: Record<MetadataFieldName, string> = {
  Name: 'Title',
  SortName: 'Sort title',
  Overview: 'Overview',
  OriginalTitle: 'Original title',
  OfficialRating: 'Content rating',
  ProductionYear: 'Production year',
  PremiereDate: 'Premiere date',
  CommunityRating: 'Community rating',
  Genres: 'Genres',
  Tags: 'Tags',
  Studios: 'Studios',
  People: 'People',
  ProviderIds: 'Provider identifiers',
  IndexNumber: 'Episode number',
  ParentIndexNumber: 'Season number',
};

function MetadataDiscardDialog({ reload, onKeep, onDiscard }: { reload: boolean; onKeep: () => void; onDiscard: () => void }) {
  return (
    <Dialog open onClose={onKeep} fullWidth maxWidth="xs" aria-labelledby="discard-metadata-title" aria-describedby="discard-metadata-description">
      <DialogTitle id="discard-metadata-title">Discard unsaved metadata changes?</DialogTitle>
      <DialogContent>
        <Typography id="discard-metadata-description" color="text.secondary">
          {reload ? 'Reloading replaces this draft with the latest saved metadata. Your unsaved changes will be lost.' : 'Your metadata changes have not been saved. Keep editing to finish them, or discard this draft.'}
        </Typography>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5, gap: 1, flexWrap: 'wrap' }}>
        <Button type="button" onClick={onKeep} autoFocus color="secondary">Keep editing</Button>
        <Button type="button" onClick={onDiscard} color="error" variant="contained">{reload ? 'Discard draft and reload' : 'Discard changes'}</Button>
      </DialogActions>
    </Dialog>
  );
}

function ReviewNotice({ reason, error, disabled, onReload }: { reason: 'conflict' | 'unknown'; error: unknown; disabled: boolean; onReload: () => void }) {
  return (
    <Alert severity="warning" sx={{ '& .MuiAlert-message': { minWidth: 0 } }}>
      <Typography variant="body2">
        {reason === 'conflict'
          ? 'This metadata changed after you opened it. Your draft is still here. Reload the latest metadata and review it before saving again.'
          : 'The response could not be confirmed. Your changes may already have been saved. Reload this item and check the saved metadata before trying again.'}
      </Typography>
      {error instanceof ApiError && error.requestId && <Typography variant="caption" component="div" sx={{ mt: 0.5 }}>Request ID: <span className="mono">{error.requestId}</span></Typography>}
      <Button type="button" size="small" color="inherit" disabled={disabled} startIcon={<RefreshRounded />} onClick={onReload} sx={{ mt: 1, ml: -1 }}>Reload latest metadata</Button>
    </Alert>
  );
}

function tabFor(field: string): number {
  if (field.startsWith('ProviderIds')) return 2;
  return ['People', 'Genres', 'Tags', 'Studios'].some((name) => field.startsWith(name)) ? 1 : 0;
}

export function MetadataEditorDialog({ itemId, onClose, onSaved, onNavigationGuardChange }: MetadataEditorDialogProps) {
  const fullScreen = useMediaQuery(theme.breakpoints.down('sm'));
  const [detail, setDetail] = useState<MetadataDetail>();
  const [overrides, setOverrides] = useState<MetadataDraftOverrides>({});
  const [lockedFields, setLockedFields] = useState<MetadataFieldName[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState<{ reason: 'conflict' | 'unknown'; error: unknown }>();
  const [reloadRevision, setReloadRevision] = useState(0);
  const [busy, setBusy] = useState(false);
  const [submitted, setSubmitted] = useState(false);
  const [pendingAction, setPendingAction] = useState<'close' | 'reload'>();
  const [notice, setNotice] = useState('');
  const [tab, setTab] = useState(0);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const validation = useMemo(() => metadataInput(detail?.Revision ?? '', overrides, lockedFields, detail), [detail, overrides, lockedFields]);
  const values = useMemo(() => detail ? displayedDraft(detail, overrides, lockedFields) : undefined, [detail, overrides, lockedFields]);
  const dirty = Boolean(detail && metadataDraftKey(overrides, lockedFields) !== metadataDraftKey(draftOverrides(detail), detail.LockedFields));
  const disabled = busy || loading;
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard unsaved metadata changes and leave this page?');

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoadError(null);
    void adminApi.getItemMetadata(itemId, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
        setDetail(result);
        setOverrides(draftOverrides(result));
        setLockedFields([...result.LockedFields]);
        setError(null);
        setReview(undefined);
        setSubmitted(false);
        setNotice('');
      })
      .catch((cause: unknown) => { if (!isAbortError(cause)) setLoadError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [itemId, reloadRevision]);

  function reload() {
    setPendingAction(undefined);
    setReloadRevision((current) => current + 1);
  }

  function requestAction(action: 'close' | 'reload') {
    if (inFlight.current) return;
    if (dirty) setPendingAction(action);
    else if (action === 'reload') reload();
    else onClose();
  }

  function clearFeedback() {
    setError(null);
    setNotice('');
  }

  function editable(field: MetadataFieldName): boolean {
    return Boolean(detail?.EditableFields.includes(field) && !detail.InactiveFields.includes(field));
  }

  function fieldTab(path: string): number {
    const field = path.replace(/^(?:Overrides|LockedFields)\./, '').split(/[.\[]/, 1)[0] as MetadataFieldName;
    return detail?.InactiveFields.includes(field) ? 0 : tabFor(field);
  }

  function change<K extends MetadataFieldName>(field: K, value: MetadataDraftValues[K]) {
    if (disabled || !editable(field)) return;
    setOverrides((current) => ({ ...current, [field]: value }));
    clearFeedback();
  }

  function changeLock(field: MetadataFieldName, locked: boolean) {
    if (disabled || !editable(field)) return;
    setLockedFields((current) => locked ? [...new Set([...current, field])] : current.filter((entry) => entry !== field));
    clearFeedback();
  }

  function useAutomatic(field: MetadataFieldName) {
    if (disabled || (!editable(field) && !detail?.InactiveFields.includes(field))) return;
    setOverrides((current) => {
      const next = { ...current };
      delete next[field];
      return next;
    });
    setLockedFields((current) => current.filter((entry) => entry !== field));
    clearFeedback();
  }

  function errorFor(path: string): string | undefined {
    const emptyEpisodeNumber = path === 'IndexNumber' && editable('IndexNumber') && hasOverride(overrides, 'IndexNumber') && !overrides.IndexNumber?.trim();
    return (submitted || emptyEpisodeNumber ? validation.errors[path] : undefined) ?? fieldError(error, path) ?? fieldError(error, `Overrides.${path}`);
  }

  function fieldSummaryError(field: MetadataFieldName): string | undefined {
    const direct = errorFor(field);
    if (direct) return direct;
    const localChild = submitted ? Object.keys(validation.errors).find((path) => path.startsWith(`${field}.`)) : undefined;
    if (localChild) return validation.errors[localChild];
    if (!(error instanceof ApiError) || !error.fields) return undefined;
    const serverChild = Object.keys(error.fields).find((path) => path.startsWith(`${field}.`) || path.startsWith(`Overrides.${field}.`) || path.startsWith(`${field}[`) || path.startsWith(`Overrides.${field}[`));
    return serverChild ? fieldError(error, serverChild) : undefined;
  }

  function frame(field: MetadataFieldName, children: ReactNode, label = labels[field]) {
    if (!detail || detail.InactiveFields.includes(field)) return null;
    const manual = hasOverride(overrides, field);
    const locked = lockedFields.includes(field);
    return (
      <MetadataField
        key={field}
        id={`metadata-${field}`}
        label={label}
        state={manual ? 'manual' : locked ? 'locked' : 'automatic'}
        locked={locked}
        canRestore={manual || locked}
        disabled={disabled}
        readOnlyReason={editable(field) ? undefined : field === 'IndexNumber' || field === 'ParentIndexNumber' ? 'This number follows the library structure and cannot be edited or locked.' : 'This field is read-only for this item.'}
        automaticSummary={metadataSummary(detail.Automatic[field])}
        lockedSummary={locked && hasOverride(detail.LockedValues, field) ? metadataSummary(detail.LockedValues[field]) : undefined}
        pendingLock={locked && !detail.LockedFields.includes(field)}
        error={fieldSummaryError(field)}
        onLockChange={(next) => changeLock(field, next)}
        onUseAutomatic={() => useAutomatic(field)}
      >
        {children}
      </MetadataField>
    );
  }

  function textField(field: 'Name' | 'SortName' | 'Overview' | 'OriginalTitle' | 'OfficialRating') {
    if (!values) return null;
    return frame(field, (
      <TextField
        id={`metadata-${field}-input`}
        name={field}
        fullWidth
        required={field === 'Name' || field === 'SortName'}
        label={labels[field]}
        value={values[field]}
        disabled={disabled || !editable(field)}
        onChange={(event) => change(field, event.target.value)}
        multiline={field === 'Overview'}
        minRows={field === 'Overview' ? 4 : undefined}
        maxRows={field === 'Overview' ? 12 : undefined}
        error={Boolean(errorFor(field))}
        helperText={field === 'SortName' ? 'Controls alphabetical ordering independently of the title.' : undefined}
        autoComplete="off"
        slotProps={{ htmlInput: { 'aria-describedby': `metadata-${field}-help` } }}
      />
    ));
  }

  function numberField(field: 'ProductionYear' | 'CommunityRating' | 'IndexNumber' | 'ParentIndexNumber', label = labels[field]) {
    if (!values) return null;
    return frame(field, (
      <TextField
        id={`metadata-${field}-input`}
        name={field}
        fullWidth
        label={label}
        required={field === 'IndexNumber' && editable(field)}
        type="text"
        value={values[field]}
        onChange={(event) => change(field, event.target.value)}
        disabled={disabled || !editable(field)}
        error={Boolean(errorFor(field))}
        helperText={!editable(field) ? undefined : field === 'IndexNumber' ? 'Enter an episode number from 0 to 2147483647.' : field === 'CommunityRating' ? 'Use a rating from 0 to 10. Leave empty to clear.' : 'Leave empty to clear.'}
        slotProps={{ htmlInput: { inputMode: field === 'CommunityRating' ? 'decimal' : 'numeric', 'aria-describedby': `metadata-${field}-help` } }}
      />
    ), label);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (inFlight.current || !detail || !dirty || disabled || review) return;
    setSubmitted(true);
    const errors = Object.keys(validation.errors);
    if (errors.length) {
      setTab(fieldTab(errors[0]));
      return;
    }
    inFlight.current = true;
    setBusy(true);
    clearFeedback();
    try {
      const result = await adminApi.updateItemMetadata(itemId, validation.input);
      if (!mounted.current) return;
      setDetail(result);
      setOverrides(draftOverrides(result));
      setLockedFields([...result.LockedFields]);
      setSubmitted(false);
      setReview(undefined);
      setNotice('Metadata changes saved.');
      onSaved(result);
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      if (cause instanceof ApiError && cause.status === 409) setReview({ reason: 'conflict', error: cause });
      else if (!(cause instanceof ApiError) || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview({ reason: 'unknown', error: cause });
      else {
        setError(cause);
        if (cause.fields) {
          const field = Object.keys(cause.fields)[0]?.replace(/^Overrides\./, '');
          if (field) setTab(fieldTab(field));
        }
      }
    } finally {
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  const sectionErrors = (index: number) => submitted && Object.keys(validation.errors).some((field) => fieldTab(field) === index);
  const showIndexNumber = Boolean(detail && values && !detail.InactiveFields.includes('IndexNumber') && (detail.Item.Type === 'Episode' || detail.Item.Type === 'Season' || editable('IndexNumber') || values.IndexNumber !== ''));
  const showParentIndexNumber = Boolean(detail && values && !detail.InactiveFields.includes('ParentIndexNumber') && detail.Item.Type !== 'Season' && (detail.Item.Type === 'Episode' || editable('ParentIndexNumber') || values.ParentIndexNumber !== ''));
  const savedInactiveFields = detail?.InactiveFields.filter((field) => hasOverride(detail.Overrides, field) || detail.LockedFields.includes(field)) ?? [];
  const numberingTitle = detail?.Item.Type === 'Episode' ? 'Episode numbering' : detail?.Item.Type === 'Season' ? 'Season numbering' : detail?.Item.Type === 'Audio' ? 'Track numbering' : 'Numbering';
  const indexLabel = detail?.Item.Type === 'Audio' ? 'Track number' : detail?.Item.Type === 'Season' ? 'Season number' : detail?.Item.Type === 'Episode' ? 'Episode number' : 'Item number';
  const parentIndexLabel = detail?.Item.Type === 'Audio' ? 'Disc number' : detail?.Item.Type === 'Episode' ? 'Season number' : 'Parent number';

  return (
    <>
      <Dialog open fullWidth maxWidth="lg" fullScreen={fullScreen} onClose={() => requestAction('close')} aria-labelledby="metadata-editor-title" slotProps={{ paper: { sx: { maxWidth: fullScreen ? undefined : 1000, ...(fullScreen ? { borderRadius: 0 } : {}) } } }}>
        <Box component="form" noValidate onSubmit={submit} aria-busy={disabled} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden', height: fullScreen ? '100%' : undefined }}>
          <DialogTitle id="metadata-editor-title" sx={{ px: { xs: 2.5, sm: 3 }, pt: 3, pb: 2 }}>
            <Stack direction="row" sx={{ alignItems: 'flex-start', justifyContent: 'space-between', gap: 2 }}>
              <Box sx={{ minWidth: 0 }}>
                <Typography component="span" variant="h3">Edit metadata</Typography>
                <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere', display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical', overflow: 'hidden' }}>{detail?.Effective.Name ?? 'Loading item details...'}</Typography>
              </Box>
              {detail && <Chip label={detail.Item.Type} size="small" variant="outlined" />}
            </Stack>
          </DialogTitle>
          {detail && <Tabs value={tab} onChange={(_, next: number) => setTab(next)} variant="scrollable" scrollButtons="auto" aria-label="Metadata sections" sx={{ px: { xs: 1, sm: 1.5 }, borderBottom: 1, borderColor: 'divider', flexShrink: 0 }}>
            {['Details', 'People & categories', 'Identifiers'].map((label, index) => <Tab key={label} id={`metadata-tab-${index}`} aria-controls={`metadata-panel-${index}`} label={sectionErrors(index) ? `${label} · Review` : label} sx={{ color: sectionErrors(index) ? 'error.main' : undefined }} />)}
          </Tabs>}
          <DialogContent sx={{ px: { xs: 2.5, sm: 3 }, py: 3, pt: '24px !important' }}>
            <Stack spacing={3}>
              {loadError != null && <ErrorNotice error={loadError} retry={() => requestAction('reload')} />}
              {!detail && loading && <Stack role="status" aria-label="Loading metadata" spacing={2}><Skeleton variant="rounded" height={90} /><Skeleton height={70} /><Skeleton height={100} /><Skeleton height={160} /></Stack>}
              {!detail && !loading && loadError != null && <Typography variant="body2" color="text.secondary">Reload this item to view and edit its metadata.</Typography>}
              {review && <ReviewNotice reason={review.reason} error={review.error} disabled={disabled} onReload={() => requestAction('reload')} />}
              {error != null && <ErrorNotice error={error} />}
              {submitted && Object.keys(validation.errors).length > 0 && <Alert severity="error">{validation.errors.Overrides ?? 'Review the highlighted fields before saving your changes.'}</Alert>}
              {detail && values && <>
                <Box id="metadata-panel-0" role="tabpanel" aria-labelledby="metadata-tab-0" hidden={tab !== 0}>
                  <Stack spacing={3}>
                    <MetadataSection title="Title and description" description="Edit a value to keep a manual override. Use automatic restores the value found during library scans.">
                      {textField('Name')}
                      {textField('SortName')}
                      {textField('OriginalTitle')}
                      {textField('Overview')}
                    </MetadataSection>
                    <Divider />
                    <MetadataSection title="Release and ratings" description="Add release details and the ratings shown in media clients.">
                      <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(2, minmax(0, 1fr))' }, gap: 3 }}>
                        {numberField('ProductionYear')}
                        {frame('PremiereDate', <TextField id="metadata-PremiereDate-input" name="PremiereDate" fullWidth type="date" label="Premiere date" value={values.PremiereDate.slice(0, 10)} disabled={disabled || !editable('PremiereDate')} onChange={(event) => change('PremiereDate', event.target.value ? `${event.target.value}T00:00:00Z` : '')} error={Boolean(errorFor('PremiereDate'))} helperText="Dates use UTC. Leave empty to clear." slotProps={{ inputLabel: { shrink: true }, htmlInput: { min: '0001-01-01', max: '9999-12-31', 'aria-describedby': 'metadata-PremiereDate-help' } }} />)}
                        {textField('OfficialRating')}
                        {numberField('CommunityRating')}
                      </Box>
                    </MetadataSection>
                    {(showIndexNumber || showParentIndexNumber) && <>
                      <Divider />
                      <MetadataSection title={numberingTitle} description="Numbering fields are editable only when supported by this item. Its place in the library hierarchy stays fixed.">
                        <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(2, minmax(0, 1fr))' }, gap: 3 }}>
                          {showIndexNumber && numberField('IndexNumber', indexLabel)}
                          {showParentIndexNumber && numberField('ParentIndexNumber', parentIndexLabel)}
                        </Box>
                      </MetadataSection>
                    </>}
                    {savedInactiveFields.length > 0 && <>
                      <Divider />
                      <MetadataSection title="Inactive saved fields" description="These values were saved for a different item type. They are kept for reference and do not apply to the current item.">
                        {savedInactiveFields.map((field) => <InactiveMetadataField
                          key={field}
                          id={`metadata-inactive-${field}`}
                          label={field === 'IndexNumber' ? 'Item number' : field === 'ParentIndexNumber' ? 'Parent number' : labels[field]}
                          overrideSummary={hasOverride(detail.Overrides, field) ? metadataSummary(detail.Overrides[field]) : undefined}
                          lockedSummary={hasOverride(detail.LockedValues, field) ? metadataSummary(detail.LockedValues[field]) : undefined}
                          removed={!hasOverride(overrides, field) && !lockedFields.includes(field)}
                          disabled={disabled}
                          error={fieldSummaryError(field) ?? errorFor(`LockedFields.${field}`)}
                          onUseAutomatic={() => useAutomatic(field)}
                        />)}
                      </MetadataSection>
                    </>}
                    <Paper component="section" aria-label="Item location" variant="outlined" sx={{ p: 2.5, bgcolor: colors.canvas, borderLeft: `3px solid ${colors.sea}` }}>
                      <Typography variant="overline" color="text.secondary">Item location</Typography>
                      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>The path and parent item are read-only.</Typography>
                      <Box component="dl" sx={{ m: 0, display: 'grid', gap: 1.5 }}>
                        {([['Path', detail.Item.Path || 'Not available'], ['Parent item', detail.Item.ParentName || detail.Item.ParentId || 'No parent item'], ['Library', detail.Item.LibraryId]] as const).map(([label, value]) => <Box key={label} sx={{ minWidth: 0 }}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere' }}>{value}</Typography></Box>)}
                      </Box>
                    </Paper>
                  </Stack>
                </Box>
                <Box id="metadata-panel-1" role="tabpanel" aria-labelledby="metadata-tab-1" hidden={tab !== 1}>
                  <Stack spacing={3}>
                    <MetadataSection title="People" description="Keep one row for each credit. The same person can appear in more than one role.">
                      {frame('People', <MetadataPeopleList value={values.People} disabled={disabled || !editable('People')} onChange={(next) => change('People', next)} errorFor={(path) => errorFor(`People.${path}`)} />)}
                    </MetadataSection>
                    <Divider />
                    <MetadataSection title="Categories" description="Enter each genre, tag, and studio on its own row. Commas and other punctuation stay part of the name.">
                      {(['Genres', 'Tags', 'Studios'] as const).map((field) => frame(field, <MetadataStringList id={`metadata-${field}-list`} label={labels[field]} singular={{ Genres: 'Genre', Tags: 'Tag', Studios: 'Studio' }[field]} value={values[field]} disabled={disabled || !editable(field)} onChange={(next) => change(field, next)} errorFor={(path) => errorFor(`${field}.${path}`)} />))}
                    </MetadataSection>
                  </Stack>
                </Box>
                <Box id="metadata-panel-2" role="tabpanel" aria-labelledby="metadata-tab-2" hidden={tab !== 2}>
                  <MetadataSection title="Provider identifiers" description="Store identifiers associated with this item. Each provider has one identifier.">
                    {frame('ProviderIds', <MetadataProviderList value={values.ProviderIds} disabled={disabled || !editable('ProviderIds')} onChange={(next) => change('ProviderIds', next)} errorFor={(path) => errorFor(`ProviderIds.${path}`)} />)}
                  </MetadataSection>
                </Box>
              </>}
            </Stack>
          </DialogContent>
          {notice && <Box sx={{ px: { xs: 2.5, sm: 3 }, pb: 2 }}><Alert severity="success" onClose={() => setNotice('')}>{notice}</Alert></Box>}
          <DialogActions sx={{ px: { xs: 2.5, sm: 3 }, py: 2, gap: 1, flexWrap: 'wrap', borderTop: 1, borderColor: 'divider' }}>
            <Typography variant="caption" color={review ? 'warning.main' : 'text.secondary'} sx={{ mr: 'auto', flexBasis: { xs: '100%', sm: 'auto' } }}>{loading ? 'Loading metadata...' : review ? 'Reload required before saving' : dirty ? 'Unsaved metadata changes' : detail ? 'All changes saved' : ''}</Typography>
            {detail && <Button type="button" color="secondary" onClick={() => requestAction('reload')} disabled={disabled} startIcon={<RefreshRounded />}>Reload</Button>}
            <Button type="button" color="secondary" onClick={() => requestAction('close')} disabled={busy}>Close</Button>
            <Button type="submit" variant="contained" disabled={disabled || !detail || !dirty || Boolean(review)} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving changes...' : 'Save changes'}</Button>
          </DialogActions>
        </Box>
      </Dialog>
      {pendingAction && <MetadataDiscardDialog reload={pendingAction === 'reload'} onKeep={() => setPendingAction(undefined)} onDiscard={() => { if (pendingAction === 'reload') reload(); else onClose(); }} />}
    </>
  );
}
