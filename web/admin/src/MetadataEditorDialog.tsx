import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, MenuItem, Skeleton, Stack, Tab, Tabs, TextField, Typography } from './ui';
import { RefreshRounded } from './icons';
import { SaveOutlined } from './icons';
import { ContentCopyRounded } from './icons';
import { CreditsEditorDialog } from './CreditsEditorDialog';
import { IntroEditorDialog } from './IntroEditorDialog';
import { creditsApi } from './creditsApi';
import type { ItemCredits } from './credits';
import { introApi } from './introApi';
import type { ItemIntro } from './introApi';
import { MediaChaptersDetails, MediaTechnicalDetails } from './MediaTechnicalDetails';
import { SeriesDisplayOrderFields } from './SeriesDisplayOrderFields';
import { adminLocale, useTranslation } from './i18n';
import './media-editor.css';
import { adminApi, ApiError, isAbortError } from './api';
import type { MetadataDetail, MetadataFieldName } from './api';
import { ErrorNotice } from './components';
import { ArtworkManagerDialog } from './ArtworkManagerDialog';
import type { ArtworkEditorState } from './ArtworkManagerDialog';
import type { ImageType } from './artworkApi';
import { EpisodeRosterDialog } from './EpisodeRosterDialog';
import { MediaProcessingDialog } from './MediaProcessingDialog';
import { fieldError } from './formFields';
import { InactiveMetadataField, MetadataField, MetadataSection } from './MetadataField';
import { MetadataPeopleList, MetadataProviderList, MetadataStringList } from './MetadataCollections';
import { displayedDraft, draftOverrides, hasOverride, metadataDraftKey, metadataInput, metadataSummary } from './metadataDraft';
import type { MetadataDraftOverrides, MetadataDraftValues } from './metadataDraft';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';

interface MetadataEditorDialogProps {
  itemId: string;
  currentUserId?: string;
  initialTab?: number;
  initialImageType?: ImageType;
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
  EndDate: 'End date',
  Status: 'Series status',
  CommunityRating: 'Community rating',
  Genres: 'Genres',
  Tags: 'Tags',
  Studios: 'Studios',
  People: 'People',
  ProviderIds: 'Provider identifiers',
  IndexNumber: 'Episode number',
  ParentIndexNumber: 'Season number',
  AirsBeforeSeasonNumber: 'Airs before season',
  AirsAfterSeasonNumber: 'Airs after season',
  AirsBeforeEpisodeNumber: 'Airs before episode',
  Album: 'Album tag',
  Artists: 'Artists',
  AlbumArtists: 'Album artists',
};

function MetadataDiscardDialog({ reload, onKeep, onDiscard }: { reload: boolean; onKeep: () => void; onDiscard: () => void }) {
  return (
    <Dialog open role="alertdialog" onClose={onKeep} fullWidth maxWidth="xs" aria-labelledby="discard-metadata-title" aria-describedby="discard-metadata-description">
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

function MetadataImagesDraftDialog({ saving, disabled, onKeep, onDiscard, onSave }: {
  saving: boolean; disabled: boolean; onKeep: () => void; onDiscard: () => void; onSave: () => void;
}) {
  return <Dialog open role="alertdialog" onClose={saving ? undefined : onKeep} fullWidth maxWidth="xs" aria-labelledby="metadata-images-draft-title" aria-describedby="metadata-images-draft-description">
    <DialogTitle id="metadata-images-draft-title">Edit images with unsaved metadata?</DialogTitle>
    <DialogContent><Typography id="metadata-images-draft-description" color="text.secondary">Save your metadata to use its latest provider match for images, or discard the draft and use the saved metadata. Keep editing preserves your draft.</Typography></DialogContent>
    <DialogActions sx={{ px: 3, pb: 2.5, gap: 1, flexWrap: 'wrap' }}>
      <Button type="button" color="secondary" onClick={onKeep} disabled={saving} autoFocus>Keep editing</Button>
      <Button type="button" color="error" onClick={onDiscard} disabled={saving}>Discard metadata and edit images</Button>
      <Button type="button" variant="contained" onClick={onSave} disabled={disabled} startIcon={saving ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{saving ? 'Saving changes...' : 'Save metadata and edit images'}</Button>
    </DialogActions>
  </Dialog>;
}

function ReviewNotice({ reason, error, disabled, onReload }: { reason: 'conflict' | 'unknown'; error: unknown; disabled: boolean; onReload: () => void }) {
  return (
    <Alert severity="warning" sx={{ '& .MuiAlert-message': { minWidth: 0 } }}>
      <Typography variant="body2">
        {reason === 'conflict'
          ? 'This metadata changed after you opened it. Your draft is still here. Reload the latest metadata and review it before saving again.'
          : 'The response could not be confirmed. Your changes may already have been saved. Reload this item and check the saved metadata before trying again.'}
      </Typography>
      {error instanceof ApiError && error.requestId && <Typography variant="caption" component="div" sx={{ mt: 0.5 }}>Request ID: <span className="mono" translate="no">{error.requestId}</span></Typography>}
      <Button type="button" size="small" color="inherit" disabled={disabled} startIcon={<RefreshRounded />} onClick={onReload} sx={{ mt: 1, ml: -1 }}>Reload latest metadata</Button>
    </Alert>
  );
}

function tabFor(field: string): number {
  return field.startsWith('People') ? 2 : 0;
}


function mediaClock(ticks: number): string {
  const total = Math.floor(ticks / 10_000_000);
  return Math.floor(total / 3600) + ':' + String(Math.floor(total % 3600 / 60)).padStart(2, '0') + ':' + String(total % 60).padStart(2, '0');
}

function MetadataChaptersPanel({ detail, disabled, canEditCredits, onIntro, onCredits, onRoster }: {
  detail: MetadataDetail; disabled: boolean; canEditCredits: boolean; onIntro: () => void; onCredits: () => void; onRoster: () => void;
}) {
  const { t } = useTranslation();
  const [credits, setCredits] = useState<ItemCredits>();
  const [intro, setIntro] = useState<ItemIntro>();
  const [creditsError, setCreditsError] = useState<unknown>();
  const [introError, setIntroError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const supported = ['Movie', 'Episode'].includes(detail.Item.Type);
  const isEpisode = detail.Item.Type === 'Episode';
  useEffect(() => {
    const controller = new AbortController();
    if (!supported) { setLoading(false); return () => controller.abort(); }
    setLoading(true); setCreditsError(undefined); setIntroError(undefined); setCredits(undefined); setIntro(undefined);
    const reads: Promise<unknown>[] = [
      creditsApi.get(detail.Item.Id, { signal: controller.signal }).then((value) => { if (!controller.signal.aborted) setCredits(value); })
        .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setCreditsError(cause); }),
    ];
    if (isEpisode) reads.push(introApi.get(detail.Item.Id, { signal: controller.signal }).then((value) => { if (!controller.signal.aborted) setIntro(value); })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setIntroError(cause); }));
    void Promise.all(reads).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [detail.Item.Id, supported, isEpisode, revision]);
  const reload = () => setRevision((value) => value + 1);

  return <div className="metadata-analysis-panel">
    {disabled && <Alert severity="info">Save or discard metadata changes before editing playback markers.</Alert>}
    <section className="editor-detail-section"><h3>{t('Chapters')}</h3><MediaChaptersDetails itemId={detail.Item.Id} />
      {detail.Item.Type === 'Series' && <Button type="button" onClick={onRoster} disabled={disabled}>Episode roster</Button>}
    </section>
    {loading && supported && <Skeleton height={120} aria-label="Loading playback markers" />}
    {supported && <section className="editor-detail-section"><h3>{t('Intro & credits')}</h3>
      {creditsError != null && <ErrorNotice error={creditsError} retry={reload} />}
      {introError != null && <ErrorNotice error={introError} retry={reload} />}
      <dl className="editor-detail-list">
        <div><dt>{t('Duration')}</dt><dd className="mono">{credits ? mediaClock(credits.DurationTicks) : intro ? mediaClock(intro.DurationTicks) : '—'}</dd></div>
        <div><dt>{t('Intro')}</dt><dd>{isEpisode ? intro ? intro.Effective ? <span className="mono">{mediaClock(intro.Effective.StartTicks)}–{mediaClock(intro.Effective.EndTicks)}</span> : t('No interval applied') : '—' : t('Movies do not use automatic intro detection')}</dd></div>
        <div><dt>{t('Credits')}</dt><dd>{credits ? credits.Effective ? <><span className="mono">{mediaClock(credits.Effective.StartTicks)}</span><span className="editor-inline-note"> · {t(credits.Effective.Provenance)}</span></> : t('No marker applied') : '—'}</dd></div>
        {credits && <div><dt>{t('Detection')}</dt><dd>{t(credits.DetectedStatus.replaceAll('_', ' '))}{credits.DetectedStale && <> · {t('Stale')}</>}</dd></div>}
      </dl>
      {credits?.OverrideStale && <Alert severity="warning">The saved marker is stale because the media source changed. Review it before applying a new marker.</Alert>}
      <div className="editor-inline-actions">{isEpisode && <Button type="button" variant="outlined" disabled={disabled} onClick={onIntro}>Edit intro</Button>}<Button type="button" variant="outlined" disabled={disabled || !canEditCredits} onClick={onCredits}>Edit credits</Button><Button type="button" disabled={loading} onClick={reload}>Refresh</Button></div>
    </section>}
    {!supported && detail.Item.Type !== 'Series' && <p className="editor-note">{t('Playback markers are available for movies and episodes.')}</p>}
  </div>;
}

function MetadataFilePanel({ detail, disabled, onProcessing }: { detail: MetadataDetail; disabled: boolean; onProcessing: () => void }) {
  const { t } = useTranslation();
  const [copyError, setCopyError] = useState<unknown>();
  const [copied, setCopied] = useState(false);
  const supported = ['Movie', 'Episode'].includes(detail.Item.Type);
  async function copyPath() {
    try { await navigator.clipboard.writeText(detail.Item.Path); setCopied(true); setCopyError(undefined); }
    catch (cause) { setCopyError(cause); setCopied(false); }
  }
  return <div className="metadata-file-panel">
    <section className="editor-detail-section"><h3>{t('Item location')}</h3>
      <dl className="editor-detail-list">
        <div><dt>{t('Path')}</dt><dd className="mono" translate="no">{detail.Item.Path || t('Not available')}</dd></div>
        <div><dt>{t('Parent item')}</dt><dd translate="no">{detail.Item.ParentName || detail.Item.ParentId || t('No parent item')}</dd></div>
        <div><dt>{t('Library')}</dt><dd className="mono" translate="no">{detail.Item.LibraryId}</dd></div>
      </dl>
      {detail.Item.Path && <Button type="button" size="small" startIcon={<ContentCopyRounded />} onClick={() => void copyPath()}>{copied ? 'Copied' : 'Copy path'}</Button>}
      {copyError != null && <ErrorNotice error={copyError} />}
    </section>
    <MediaTechnicalDetails itemId={detail.Item.Id} processingDisabled={disabled} onProcessing={supported ? onProcessing : undefined} />
  </div>;
}

export function MetadataEditorDialog({ itemId, currentUserId = '', initialTab = 0, initialImageType = 'Primary', onClose, onSaved, onNavigationGuardChange }: MetadataEditorDialogProps) {
  const { t, formatCount } = useTranslation();
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
  const [pendingImages, setPendingImages] = useState(false);
  const [notice, setNotice] = useState('');
  const [tab, setTab] = useState(initialTab >= 0 && initialTab <= 4 ? initialTab : 0);
  const [imagesVisited, setImagesVisited] = useState(initialTab === 1);
  const [artworkBusy, setArtworkBusy] = useState(false);
  const [editingArtwork, setEditingArtwork] = useState(false);
  const [editingRoster, setEditingRoster] = useState(false);
  const [processingMedia, setProcessingMedia] = useState(false);
  const [editingIntro, setEditingIntro] = useState(false);
  const [editingCredits, setEditingCredits] = useState(false);
  const [analysisRevision, setAnalysisRevision] = useState(0);
  const [orderState, setOrderState] = useState({ dirty: false, busy: false, reviewRequired: false });
  const formRef = useRef<HTMLFormElement>(null);
  const ownGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const artworkGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const childGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const orderGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const publishGuard = useCallback(() => {
    onNavigationGuardChange(ownGuard.current || artworkGuard.current || childGuard.current || orderGuard.current
      ? () => (!orderGuard.current || orderGuard.current()) && (!artworkGuard.current || artworkGuard.current()) && (!childGuard.current || childGuard.current()) && (!ownGuard.current || ownGuard.current())
      : undefined);
  }, [onNavigationGuardChange]);
  const registerOwnGuard = useCallback((guard: UserNavigationGuard | undefined) => { ownGuard.current = guard; publishGuard(); }, [publishGuard]);
  const registerArtworkGuard = useCallback((guard: UserNavigationGuard | undefined) => { artworkGuard.current = guard; publishGuard(); }, [publishGuard]);
  const registerChildGuard = useCallback((guard: UserNavigationGuard | undefined) => { childGuard.current = guard; publishGuard(); }, [publishGuard]);
  const registerOrderGuard = useCallback((guard: UserNavigationGuard | undefined) => { orderGuard.current = guard; publishGuard(); }, [publishGuard]);
  const registerArtworkState = useCallback((state: ArtworkEditorState) => { setArtworkBusy(state.busy); }, []);
  const inFlight = useRef(false);
  const imagesAfterSave = useRef(false);
  const imagesAfterReload = useRef(false);
  const mounted = useRef(true);
  const validation = useMemo(() => metadataInput(detail?.Revision ?? '', overrides, lockedFields, detail), [detail, overrides, lockedFields]);
  const values = useMemo(() => detail ? displayedDraft(detail, overrides, lockedFields) : undefined, [detail, overrides, lockedFields]);
  const dirty = Boolean(detail && metadataDraftKey(overrides, lockedFields) !== metadataDraftKey(draftOverrides(detail), detail.LockedFields));
  const disabled = busy || artworkBusy || loading || orderState.dirty || orderState.busy || orderState.reviewRequired;
  useUserDraftNavigation(dirty, busy, registerOwnGuard, t('Discard unsaved metadata changes and leave this page?'));

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
        if (imagesAfterReload.current) {
          imagesAfterReload.current = false; setImagesVisited(true); setTab(1);
        }
      })
      .catch((cause: unknown) => { imagesAfterReload.current = false; if (!isAbortError(cause)) setLoadError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [itemId, reloadRevision]);

  function reload() {
    setPendingAction(undefined);
    setPendingImages(false);
    setReloadRevision((current) => current + 1);
  }

  function requestAction(action: 'close' | 'reload') {
    if (inFlight.current || artworkBusy || editingArtwork || editingRoster || processingMedia || editingIntro || editingCredits) return;
    if (orderGuard.current && !orderGuard.current()) return;
    if (artworkGuard.current && !artworkGuard.current()) return;
    if (childGuard.current && !childGuard.current()) return;
    if (dirty) setPendingAction(action);
    else if (action === 'reload') reload();
    else onClose();
  }

  function changeTab(next: number) {
    if (busy || artworkBusy || (tab !== next && orderGuard.current && !orderGuard.current()) || (tab !== next && artworkGuard.current && !artworkGuard.current()) || (tab !== next && childGuard.current && !childGuard.current())) return;
    if (next === 1 && (dirty || review)) { setPendingImages(true); return; }
    if (next === 1) setImagesVisited(true);
    setTab(next);
  }

  useEffect(() => {
    function saveShortcut(event: KeyboardEvent) {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== 's' || editingArtwork || editingRoster || processingMedia || editingIntro || editingCredits) return;
      event.preventDefault();
      if (tab !== 1 && !pendingImages) formRef.current?.requestSubmit();
    }
    window.addEventListener('keydown', saveShortcut);
    return () => window.removeEventListener('keydown', saveShortcut);
  }, [tab, pendingImages, editingArtwork, editingRoster, processingMedia, editingIntro, editingCredits]);

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
    // Lock only the field edited by this interaction. Explicit unlocks remain
    // effective because saving never reconstructs locks from the override set.
    setLockedFields((current) => current.includes(field) ? current : [...current, field]);
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

  function textField(field: 'Name' | 'SortName' | 'Overview' | 'OriginalTitle' | 'OfficialRating' | 'Album') {
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
        helperText={field === 'SortName' ? 'Controls alphabetical ordering independently of the title.' : field === 'Album' ? 'Stored album tag, up to 1,024 UTF-8 bytes. This does not move files or change their album identity or directory. Edit the album item title to rename the album.' : undefined}
        autoComplete="off"
        slotProps={{ htmlInput: { 'aria-describedby': `metadata-${field}-help` } }}
      />
    ));
  }

  function numberField(field: 'ProductionYear' | 'CommunityRating' | 'IndexNumber' | 'ParentIndexNumber' | 'AirsBeforeSeasonNumber' | 'AirsAfterSeasonNumber' | 'AirsBeforeEpisodeNumber', label = labels[field]) {
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
        helperText={!editable(field) ? undefined : field === 'IndexNumber' ? `Enter ${detail?.Item.Type === 'Audio' ? 'a track' : 'an episode'} number from 0 to 2147483647.` : field === 'CommunityRating' ? 'Use a rating from 0 to 10. Leave empty to clear.' : 'Leave empty to clear.'}
        slotProps={{ htmlInput: { inputMode: field === 'CommunityRating' ? 'decimal' : 'numeric', 'aria-describedby': `metadata-${field}-help` } }}
      />
    ), label);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (tab === 1 || inFlight.current || !detail || !dirty || disabled || review) return;
    setSubmitted(true);
    const errors = Object.keys(validation.errors);
    if (errors.length) {
      imagesAfterSave.current = false;
      setPendingImages(false);
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
      if (imagesAfterSave.current) {
        setPendingImages(false); setImagesVisited(true); setTab(1);
      }
    } catch (cause) {
      setPendingImages(false);
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
      imagesAfterSave.current = false;
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
      <Dialog open fullWidth maxWidth="md" className="media-editor-sheet metadata-editor-sheet" onClose={() => requestAction('close')} aria-labelledby="metadata-editor-title" slotProps={{ paper: { sx: { width: '100%', maxWidth: 540 } } }}>
        <Box ref={formRef} component="form" className="metadata-editor-form" noValidate onSubmit={submit} aria-busy={busy || loading || orderState.busy} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden', height: '100%' }}>
          <DialogTitle id="metadata-editor-title">
            <span className="editor-eyebrow">{t('Edit metadata')}</span>
            <span className="editor-item-name" translate="no">{detail?.Effective.Name ?? t('Loading item details...')}</span>
            {detail && <span className="editor-item-subtitle" translate="no">{[detail.Effective.OriginalTitle, detail.Effective.ProductionYear].filter(Boolean).join(' · ') || t(detail.Item.Type)}</span>}
            {detail && <span className="editor-item-flags"><Chip label={detail.Item.Type} size="small" variant="outlined" /><span>{formatCount(detail.LockedFields.length, 'locked field', 'locked fields')}</span>{detail.LastEditedAt && <span>{t('Saved')} {new Date(detail.LastEditedAt).toLocaleDateString(adminLocale())}</span>}</span>}
          </DialogTitle>
          {detail && <Tabs value={tab} onChange={(_, next: number) => changeTab(next)} variant="scrollable" scrollButtons="auto" aria-label="Metadata sections" className="metadata-editor-tabs" sx={{ px: 2, borderBottom: 1, borderColor: 'divider', flexShrink: 0 }}>
            {['Basic information', 'Images', 'Cast & crew', 'Chapters & credits', 'File'].map((label, index) => <Tab key={label} value={index} id={'metadata-tab-' + index} aria-controls={'metadata-panel-' + index} label={sectionErrors(index) ? t(label) + ' · ' + t('Review') : label} sx={{ color: sectionErrors(index) ? 'error.main' : undefined }} />)}
          </Tabs>}
          <DialogContent className="metadata-editor-content" sx={{ px: 3, py: 2.5 }}>
            <Stack spacing={3}>
              {loadError != null && <ErrorNotice error={loadError} retry={() => requestAction('reload')} />}
              {!detail && loading && <Stack role="status" aria-label="Loading metadata" spacing={2}><Skeleton variant="rounded" height={90} /><Skeleton height={70} /><Skeleton height={100} /><Skeleton height={160} /></Stack>}
              {!detail && !loading && loadError != null && <Typography variant="body2" color="text.secondary">Reload this item to view and edit its metadata.</Typography>}
              {review && <ReviewNotice reason={review.reason} error={review.error} disabled={disabled} onReload={() => requestAction('reload')} />}
              {error != null && <ErrorNotice error={error} />}
              {submitted && Object.keys(validation.errors).length > 0 && <Alert severity="error">{validation.errors.Overrides ?? 'Review the highlighted fields before saving your changes.'}</Alert>}
              {detail && values && <>
                <Box id="metadata-panel-0" role="tabpanel" aria-labelledby="metadata-tab-0" hidden={tab !== 0}>
                  <Stack spacing={2} className="metadata-basic-sections">
                    <MetadataSection title="Title and description" description="Edit a value to keep a manual override. Use automatic restores the value found during library scans.">
                      {textField('Name')}
                      {textField('OriginalTitle')}
                      {textField('SortName')}
                    </MetadataSection>
                    <Divider />
                    {['Audio', 'MusicAlbum', 'MusicArtist'].includes(detail.Item.Type) && <><MetadataSection title="Music credits" description="Keep each artist on a separate row, with up to 1,024 UTF-8 bytes (1 KiB) per name and 1,024 entries per list. Punctuation remains part of the name. Add composers in People & categories.">
                      {textField('Album')}
                      {(['Artists', 'AlbumArtists'] as const).map((field) => frame(field, <MetadataStringList id={`metadata-${field}-list`} label={labels[field]} singular={field === 'Artists' ? 'Artist' : 'Album artist'} value={values[field]} disabled={disabled || !editable(field)} onChange={(next) => change(field, next)} errorFor={(path) => errorFor(`${field}.${path}`)} />))}
                    </MetadataSection><Divider /></>}
                    <MetadataSection title="Release and ratings" description="Add release details and the ratings shown in media clients.">
                      <Box className="metadata-field-grid">
                        {numberField('ProductionYear')}
                        {frame('PremiereDate', <TextField id="metadata-PremiereDate-input" name="PremiereDate" fullWidth type="date" label="Premiere date" value={values.PremiereDate.slice(0, 10)} disabled={disabled || !editable('PremiereDate')} onChange={(event) => change('PremiereDate', event.target.value ? `${event.target.value}T00:00:00Z` : '')} error={Boolean(errorFor('PremiereDate'))} helperText="Dates use UTC. Leave empty to clear." slotProps={{ inputLabel: { shrink: true }, htmlInput: { min: '0001-01-01', max: '9999-12-31', 'aria-describedby': 'metadata-PremiereDate-help' } }} />)}
                        {frame('EndDate', <TextField id="metadata-EndDate-input" name="EndDate" fullWidth type="date" label="End date" value={values.EndDate.slice(0, 10)} disabled={disabled || !editable('EndDate')} onChange={(event) => change('EndDate', event.target.value ? `${event.target.value}T00:00:00Z` : '')} error={Boolean(errorFor('EndDate'))} helperText="Dates use UTC. Leave empty for unknown." slotProps={{ inputLabel: { shrink: true }, htmlInput: { min: '0001-01-01', max: '9999-12-31', 'aria-describedby': 'metadata-EndDate-help' } }} />)}
                        {detail.Item.Type === 'Series' && frame('Status', <TextField id="metadata-Status-input" name="Status" select fullWidth label="Series status" value={values.Status} disabled={disabled || !editable('Status')} onChange={(event) => change('Status', event.target.value)} error={Boolean(errorFor('Status'))} helperText="Unknown keeps the status unspecified."><MenuItem value="">Unknown</MenuItem><MenuItem value="Continuing">Continuing</MenuItem><MenuItem value="Ended">Ended</MenuItem></TextField>)}
                        {textField('OfficialRating')}
                        {numberField('CommunityRating')}
                      </Box>
                    </MetadataSection>
                    {detail.Item.Type === 'Series' && <SeriesDisplayOrderFields key={itemId + ':' + detail.Revision} seriesId={itemId} disabled={busy || loading || dirty || Boolean(review)} onNavigationGuardChange={registerOrderGuard} onStateChange={setOrderState} />}
                    <MetadataSection title="Description" description="">
                      {textField('Overview')}
                    </MetadataSection>
                    {(showIndexNumber || showParentIndexNumber) && <>
                      <Divider />
                      <MetadataSection title={numberingTitle} description="Numbering fields are editable only when supported by this item. Its place in the library hierarchy stays fixed.">
                        <Box className="metadata-field-grid">
                          {showIndexNumber && numberField('IndexNumber', indexLabel)}
                          {showParentIndexNumber && numberField('ParentIndexNumber', parentIndexLabel)}
                        </Box>
                      </MetadataSection>
                    </>}
                    {detail.Item.Type === 'Episode' && <>
                      <Divider />
                      <MetadataSection title="Special episode placement" description="Set the regular season associated with a season-zero special. Leave both seasons empty or zero to keep it standalone. Its physical season and episode numbers stay unchanged.">
                        <Box className="metadata-field-grid">
                          {numberField('AirsBeforeSeasonNumber')}
                          {numberField('AirsAfterSeasonNumber')}
                          {numberField('AirsBeforeEpisodeNumber')}
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
                    <Divider />
                    <MetadataSection title="Categories" description="Enter each genre, tag, and studio on its own row. Commas and other punctuation stay part of the name.">
                      {(['Genres', 'Tags', 'Studios'] as const).map((field) => frame(field, <MetadataStringList id={`metadata-${field}-list`} label={labels[field]} singular={{ Genres: 'Genre', Tags: 'Tag', Studios: 'Studio' }[field]} value={values[field]} disabled={disabled || !editable(field)} onChange={(next) => change(field, next)} errorFor={(path) => errorFor(`${field}.${path}`)} />))}
                    </MetadataSection>
                    <Box component="details" className="editor-advanced-fields">
                      <Typography component="summary" variant="body2">Provider identifiers</Typography>
                      <Box sx={{ pt: 2 }}><MetadataSection title="Provider identifiers" description="Store identifiers associated with this item. Each provider has one identifier.">
                    {frame('ProviderIds', <MetadataProviderList value={values.ProviderIds} disabled={disabled || !editable('ProviderIds')} onChange={(next) => change('ProviderIds', next)} errorFor={(path) => errorFor(`ProviderIds.${path}`)} />)}
                  </MetadataSection></Box>
                    </Box>
                  </Stack>
                </Box>
                <Box id="metadata-panel-1" role="tabpanel" aria-labelledby="metadata-tab-1" hidden={tab !== 1}>
                  {imagesVisited && <ArtworkManagerDialog embedded target={{ kind: 'items', id: itemId, name: detail.Effective.Name }} initialType={initialImageType} sourceRevision={detail.Revision} onClose={() => changeTab(0)} onNavigationGuardChange={registerArtworkGuard} onStateChange={registerArtworkState} />}
                </Box>
                <Box id="metadata-panel-2" role="tabpanel" aria-labelledby="metadata-tab-2" hidden={tab !== 2}>
                  <MetadataSection title="People" description="Keep one row for each credit. The same person can appear in more than one role.">
                    {frame('People', <MetadataPeopleList value={values.People} disabled={disabled || !editable('People')} onChange={(next) => change('People', next)} errorFor={(path) => errorFor('People.' + path)} />)}
                  </MetadataSection>
                </Box>
                <Box id="metadata-panel-3" role="tabpanel" aria-labelledby="metadata-tab-3" hidden={tab !== 3}>
                  {tab === 3 && <MetadataChaptersPanel key={analysisRevision} detail={detail} disabled={disabled || dirty || Boolean(review)} canEditCredits={Boolean(currentUserId)} onIntro={() => setEditingIntro(true)} onCredits={() => setEditingCredits(true)} onRoster={() => setEditingRoster(true)} />}
                </Box>
                <Box id="metadata-panel-4" role="tabpanel" aria-labelledby="metadata-tab-4" hidden={tab !== 4}>
                  {tab === 4 && <MetadataFilePanel detail={detail} disabled={disabled || dirty || Boolean(review)} onProcessing={() => setProcessingMedia(true)} />}
                </Box>
              </>}
            </Stack>
          </DialogContent>
          {notice && <Box sx={{ px: { xs: 2.5, sm: 3 }, pb: 2 }}><Alert severity="success" onClose={() => setNotice('')}>{notice}</Alert></Box>}
          <DialogActions sx={{ px: { xs: 2.5, sm: 3 }, py: 2, gap: 1, flexWrap: 'wrap', borderTop: 1, borderColor: 'divider' }}>
            <Typography variant="caption" color={review || orderState.reviewRequired ? 'warning.main' : 'text.secondary'} sx={{ mr: 'auto', flexBasis: { xs: '100%', sm: 'auto' } }}>{tab === 1 ? dirty ? 'Metadata draft retained. Image changes are saved immediately.' : 'Image changes are saved immediately' : loading ? 'Loading metadata...' : orderState.busy ? 'Saving episode order...' : orderState.reviewRequired ? 'Reload episode order before saving metadata' : orderState.dirty ? 'Save or discard episode order changes first' : review ? 'Reload required before saving' : dirty ? 'Unsaved metadata changes' : detail ? 'All changes saved' : ''}</Typography>
            {tab !== 1 && detail && <Button type="button" color="secondary" onClick={() => requestAction('reload')} disabled={disabled} startIcon={<RefreshRounded />}>{dirty ? 'Discard' : 'Reload'}</Button>}
            <Button type="button" color="secondary" onClick={() => requestAction('close')} disabled={busy || artworkBusy || orderState.busy}>Close</Button>
            {tab !== 1 && <Button type="submit" variant="contained" disabled={disabled || !detail || !dirty || Boolean(review)} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving changes...' : 'Save changes'}</Button>}
          </DialogActions>
        </Box>
      </Dialog>
      {pendingAction && <MetadataDiscardDialog reload={pendingAction === 'reload'} onKeep={() => setPendingAction(undefined)} onDiscard={() => { if (pendingAction === 'reload') reload(); else onClose(); }} />}
      {pendingImages && <MetadataImagesDraftDialog saving={busy} disabled={disabled || Boolean(review)} onKeep={() => setPendingImages(false)} onDiscard={() => { imagesAfterReload.current = true; reload(); }} onSave={() => { imagesAfterSave.current = true; formRef.current?.requestSubmit(); }} />}
      {editingArtwork && detail && <ArtworkManagerDialog target={{ kind: 'items', id: itemId, name: detail.Effective.Name }} onClose={() => setEditingArtwork(false)} onNavigationGuardChange={registerChildGuard} />}
      {editingRoster && detail && <EpisodeRosterDialog seriesId={itemId} seriesName={detail.Effective.Name} onClose={() => setEditingRoster(false)} onNavigationGuardChange={registerChildGuard} />}
      {editingIntro && detail && <IntroEditorDialog itemId={itemId} itemName={detail.Effective.Name} onClose={() => { setEditingIntro(false); setAnalysisRevision((value) => value + 1); }} onNavigationGuardChange={registerChildGuard} />}
      {editingCredits && detail && <CreditsEditorDialog itemId={itemId} itemName={detail.Effective.Name} currentUserId={currentUserId} onClose={() => { setEditingCredits(false); setAnalysisRevision((value) => value + 1); }} onNavigationGuardChange={registerChildGuard} />}
      {processingMedia && detail && <MediaProcessingDialog itemId={itemId} itemName={detail.Effective.Name} onClose={() => { setProcessingMedia(false); reload(); }} onNavigationGuardChange={registerChildGuard} />}
    </>
  );
}
