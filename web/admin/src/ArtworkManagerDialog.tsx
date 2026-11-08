import { useEffect, useId, useRef, useState } from 'react';
import { Alert, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Skeleton } from './ui';
import { ArrowBackRounded, ArrowForwardRounded, DeleteOutlineRounded, UploadRounded, RefreshRounded, ImageOutlined } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import { artworkApi, imageTypes, multipleImages } from './artworkApi';
import type { ArtworkCollection, ArtworkImage, ArtworkTarget, ImageType } from './artworkApi';
import { providersApi } from './providersApi';
import type { ImageCandidate } from './providersApi';
import { ErrorNotice } from './components';
import { OnlineImageCandidates, artworkTypeNames, imageCandidateKey, lowResolution } from './OnlineImageCandidates';
import type { CandidateFailure, OnlineImageSelection } from './OnlineImageCandidates';
import { confirmAction, useTranslation } from './i18n';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import './media-editor.css';

const primaryTypes: ImageType[] = ['Primary', 'Backdrop', 'Logo'];
type ImageAction = { type: ImageType; index: number } & (
  { kind: 'candidate'; candidate: ImageCandidate }
  | { kind: 'upload'; file: File }
  | { kind: 'remove'; image: ArtworkImage }
  | { kind: 'reorder'; indexes: number[] }
  | { kind: 'reset' }
);
interface ImageResult { state: 'success' | 'failed' | 'unknown'; message: string; error?: unknown; retry?: ImageAction }
interface CurrentCandidate { key: string; tag: string }
export interface ArtworkEditorState { busy: boolean; review: boolean }

function uncertain(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 0 || error.status === 409 || error.status >= 500
    || ['invalid_response', 'network_error', 'session_changed'].includes(error.code);
}

function retryable(error: unknown): boolean {
  return error instanceof ApiError && error.status === 429 && ['provider_quota', 'artwork_limit', 'rate_limited'].includes(error.code);
}


function readImage(file: File): Promise<void> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    const image = new Image();
    const failed = () => reject(new ApiError('Unable to read this image. Choose a different file.', { code: 'image_read_failed' }));
    image.onload = () => {
      if (image.naturalWidth > 0 && image.naturalHeight > 0) resolve();
      else failed();
    };
    image.onerror = failed;
    reader.onerror = failed;
    reader.onabort = failed;
    reader.onload = () => {
      if (typeof reader.result !== 'string' || !reader.result.startsWith('data:image/')) { failed(); return; }
      // Local decoding uses the data images already permitted by the dashboard CSP.
      image.src = reader.result;
    };
    reader.readAsDataURL(file);
  });
}

function SavedImagePreview({ image, name }: { image: ArtworkImage; name: string }) {
  const { t } = useTranslation();
  const [failed, setFailed] = useState(false);
  return failed
    ? <div className="artwork-image-unavailable" role="status"><ImageOutlined aria-hidden="true" /><span>{t('This image is unavailable. Reload images to check the current source and access.')}</span></div>
    : <img src={image.PreviewUrl} alt={t(artworkTypeNames[image.ImageType]) + ' ' + (image.ImageIndex + 1) + ' · ' + name} onError={() => setFailed(true)} className="artwork-preview" />;
}

export function ArtworkManagerDialog({ target, onClose, onNavigationGuardChange, embedded = false, onStateChange, initialSource, sourceRevision, initialType = 'Primary', readOnly = false }: {
  target: ArtworkTarget;
  onClose: () => void;
  onNavigationGuardChange: UserNavigationGuardChange;
  embedded?: boolean;
  onStateChange?: (state: ArtworkEditorState) => void;
  initialSource?: OnlineImageSelection;
  sourceRevision?: string;
  initialType?: ImageType;
  readOnly?: boolean;
}) {
  const { t, locale } = useTranslation();
  const number = new Intl.NumberFormat(locale);
  const fileId = useId();
  const [collection, setCollection] = useState<ArtworkCollection>();
  const [type, setType] = useState<ImageType>(target.kind === 'users' ? 'Primary' : initialType);
  const [indexes, setIndexes] = useState<Partial<Record<ImageType, number>>>({});
  const [loadError, setLoadError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<{ type: ImageType; candidate?: string; label: string }>();
  const [review, setReview] = useState(false);
  const [reloadRevision, setReloadRevision] = useState(0);
  const [results, setResults] = useState<Partial<Record<ImageType, ImageResult>>>({});
  const [failures, setFailures] = useState<Record<string, CandidateFailure>>({});
  const [currentCandidates, setCurrentCandidates] = useState<Record<string, CurrentCandidate>>({});
  const [removing, setRemoving] = useState<ArtworkImage>();
  const [resetting, setResetting] = useState(false);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const images = collection?.Items.filter((item) => item.ImageType === type).sort((left, right) => left.ImageIndex - right.ImageIndex) ?? [];
  const appendIndex = images.reduce((next, image) => Math.max(next, image.ImageIndex + 1), 0);
  const index = indexes[type] ?? images[0]?.ImageIndex ?? 0;
  const selected = images.find((image) => image.ImageIndex === index);
  const disabled = readOnly || loading || Boolean(busy) || review || loadError != null || !collection;
  const result = results[type];
  useUserDraftNavigation(review, Boolean(busy), onNavigationGuardChange, t('The last image change needs review. Leave without reloading saved images?'));

  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => { onStateChange?.({ busy: Boolean(busy), review }); }, [busy, review, onStateChange]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setLoadError(undefined);
    void artworkApi.list(target, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return;
      setCollection(value); setReview(false); setResults({}); setFailures({}); setCurrentCandidates({});
      setIndexes((current) => Object.fromEntries(imageTypes.map((imageType) => {
        const saved = value.Items.filter((image) => image.ImageType === imageType).sort((left, right) => left.ImageIndex - right.ImageIndex);
        const requested = current[imageType];
        return [imageType, saved.some((image) => image.ImageIndex === requested) ? requested : saved[0]?.ImageIndex ?? 0];
      })));
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause);
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [target.id, target.kind, reloadRevision]);

  function commit(value: ArtworkCollection) {
    setCollection(value);
    setCurrentCandidates((current) => Object.fromEntries(Object.entries(current).filter(([destination, entry]) => {
      const [imageType, imageIndex] = destination.split(':');
      return value.Items.some((image) => image.ImageType === imageType && image.ImageIndex === Number(imageIndex) && image.Tag === entry.tag);
    })));
  }

  function close() {
    if (inFlight.current) return;
    if (review && !confirmAction(t('The last image change needs review. Leave without reloading saved images?'))) return;
    onClose();
  }

  function reload() {
    if (!inFlight.current) setReloadRevision((value) => value + 1);
  }

  async function perform(action: ImageAction) {
    if (inFlight.current || disabled || !collection) return;
    const candidateKey = action.kind === 'candidate' ? imageCandidateKey(action.candidate) : undefined;
    if (action.kind === 'upload') {
      const message = !['image/jpeg', 'image/png', 'image/gif'].includes(action.file.type) ? 'Choose a JPEG, PNG, or GIF image.'
        : action.file.size === 0 || action.file.size > 20 * 1024 * 1024 ? 'Choose an image between 1 byte and 20 MiB.' : '';
      if (message) { setResults((current) => ({ ...current, [action.type]: { state: 'failed', message } })); return; }
    }
    inFlight.current = true;
    setBusy({ type: action.type, candidate: candidateKey, label: action.kind === 'upload' ? 'Reading image...' : 'Applying image...' });
    setResults((current) => ({ ...current, [action.type]: undefined }));
    if (candidateKey) setFailures((current) => { const next = { ...current }; delete next[candidateKey]; return next; });
    let writeStarted = false;
    let acknowledged = false;
    try {
      let saved: ArtworkCollection;
      let message: string;
      if (action.kind === 'candidate') {
        // Provider selection uses the metadata revision. Artwork mutations keep
        // their separate If-Match revision; the two tokens are not interchangeable.
        const metadata = await adminApi.getItemMetadata(target.id);
        if (!mounted.current) return;
        writeStarted = true;
        const response = await providersApi.applyImage(target.id, {
          Provider: action.candidate.Provider, Id: action.candidate.Id, Language: action.candidate.Language,
          ImageId: action.candidate.ImageId, ImageType: action.type, ImageIndex: action.index, Revision: metadata.Revision,
          ArtworkRevision: collection.Revision,
        });
        acknowledged = true;
        saved = response.Artwork;
        if (!mounted.current) return;
        commit(saved);
        setCurrentCandidates((current) => ({ ...current, [action.type + ':' + action.index]: { key: candidateKey!, tag: response.Tag } }));
        message = 'Image applied.';
      } else {
        if (action.kind === 'upload') {
          await readImage(action.file);
          if (!mounted.current) return;
          setBusy({ type: action.type, label: 'Saving image...' });
          writeStarted = true;
          saved = await artworkApi.upload(target, action.type, action.index, action.file, collection.Revision);
          message = 'Image saved.';
        } else if (action.kind === 'remove') {
          writeStarted = true;
          saved = await artworkApi.remove(target, action.image, collection.Revision);
          message = 'Image deleted.';
        } else if (action.kind === 'reorder') {
          writeStarted = true;
          saved = await artworkApi.reorder(target, action.type, action.indexes, collection.Revision);
          message = 'Image order saved.';
        } else {
          writeStarted = true;
          saved = await artworkApi.reset(target, action.type, collection.Revision);
          message = 'Automatic images restored.';
        }
        acknowledged = true;
        if (!mounted.current) return;
        commit(saved);
      }
      if (!mounted.current) return;
      setIndexes((current) => ({ ...current, [action.type]: saved.Items.some((image) => image.ImageType === action.type && image.ImageIndex === action.index) ? action.index : saved.Items.find((image) => image.ImageType === action.type)?.ImageIndex ?? 0 }));
      setResults((current) => ({ ...current, [action.type]: { state: 'success', message } }));
    } catch (cause) {
      if (!mounted.current) return;
      const unknown = writeStarted && (acknowledged || uncertain(cause));
      const canRetry = !unknown && (retryable(cause) || !writeStarted && !(cause instanceof ApiError && (cause.code === 'image_read_failed' || cause.status === 403 || cause.status === 401)));
      if (unknown) setReview(true);
      if (candidateKey) setFailures((current) => ({ ...current, [candidateKey]: unknown ? 'review' : canRetry ? 'retry' : 'blocked' }));
      setResults((current) => ({ ...current, [action.type]: {
        state: unknown ? 'unknown' : 'failed',
        message: unknown ? 'The image change could not be confirmed. Reload images and review the saved result before making another change.' : 'The image could not be applied. The current image is unchanged.',
        error: cause, ...(canRetry ? { retry: action } : {}),
      } }));
    } finally {
      inFlight.current = false;
      if (mounted.current) { setBusy(undefined); setRemoving(undefined); setResetting(false); }
    }
  }

  function move(position: number, delta: number) {
    const order = images.map((image) => image.ImageIndex);
    [order[position], order[position + delta]] = [order[position + delta], order[position]];
    void perform({ kind: 'reorder', type, index, indexes: order });
  }

  const body = <div className="artwork-editor-body" aria-busy={loading || Boolean(busy)}>
    {loadError != null && <ErrorNotice error={loadError} retry={reload} />}
    {review && <Alert severity="warning">The image change could not be confirmed. Reload images and review the saved result before making another change.</Alert>}
    {target.kind !== 'users' && <div className="artwork-type-toolbar">
      <div className="editor-segmented" role="group" aria-label={t('Image type')}>
        {primaryTypes.map((value) => <button key={value} type="button" aria-pressed={type === value} onClick={() => setType(value)}>{t(artworkTypeNames[value])}{collection && !collection.Items.some((image) => image.ImageType === value) && <span className="artwork-type-missing">{t('Missing')}</span>}</button>)}
      </div>
      <label className="editor-compact-select"><span className="visually-hidden">{t('Other image types')}</span><select aria-label={t('Other image types')} value={primaryTypes.includes(type) ? '' : type} onChange={(event) => setType(event.target.value as ImageType)}>
        <option value="" disabled>{t('Other image types')}</option>{imageTypes.filter((value) => !primaryTypes.includes(value)).map((value) => <option key={value} value={value}>{t(artworkTypeNames[value])}</option>)}
      </select></label>
    </div>}
    {loading && !collection && <Skeleton variant="rounded" height={228} aria-label="Loading images" />}
    {collection && <section className="artwork-current" aria-label={t('Current image')}>
      <div className="artwork-current-stage" data-image-type={type}>
        {selected ? <SavedImagePreview key={selected.PreviewUrl + ':' + selected.Tag} image={selected} name={target.name} />
          : <div className="artwork-empty"><ImageOutlined aria-hidden="true" /><strong>{t('No image saved')}</strong><span>{t('Choose an online candidate or upload an image from a file.')}</span></div>}
      </div>
      <div className="artwork-current-details">
        <div><h3>{t(target.kind === 'users' ? 'Current avatar' : 'Current image')} <span>{target.kind !== 'users' && t(artworkTypeNames[type])}</span></h3>
          <strong>{selected ? t(selected.Source) : t('No image saved')}</strong>
          <p>{selected ? <>{number.format(selected.Width)} × {number.format(selected.Height)} · {number.format(Math.round(Number(selected.Size) / 1024))} KiB{lowResolution(type, selected.Width, selected.Height) && <span className="artwork-resolution-warning"> · {t('Low resolution')}</span>}</> : t('Select a candidate below to add this image.')}</p>
        </div>
        <label className="artwork-upload-button" htmlFor={fileId}><UploadRounded aria-hidden="true" /><span>{t('Upload from file')}</span><input id={fileId} type="file" accept="image/jpeg,image/png,image/gif" disabled={disabled} onChange={(event) => {
          const file = event.target.files?.[0]; event.target.value = '';
          if (file) void perform({ kind: 'upload', type, index, file });
        }} aria-label={t('Choose image file')} /></label>
      </div>
      {multipleImages(type) && <label className="editor-field"><span>{t('Upload destination')}</span><select value={index} disabled={disabled} onChange={(event) => setIndexes((current) => ({ ...current, [type]: Number(event.target.value) }))}>{images.map((image) => <option key={image.ImageIndex} value={image.ImageIndex}>{t('Replace image')} {image.ImageIndex + 1}</option>)}{appendIndex < 32 && <option value={appendIndex}>{t(images.length ? 'Add a new image' : 'First image')}</option>}</select></label>}
      <p className="artwork-upload-guidance">{t('JPEG, PNG, or GIF, up to 20 MiB. The file is uploaded as soon as it can be read.')}</p>
    </section>}
    {images.length > 0 && <ul className="artwork-saved-images" aria-label={t('Saved images')}>
      {images.map((image, position) => <li key={image.ImageType + ':' + image.ImageIndex + ':' + image.Tag} className={index === image.ImageIndex ? 'is-selected' : ''}>
        {multipleImages(type) && <button type="button" className="artwork-saved-choice" disabled={disabled} aria-pressed={index === image.ImageIndex} aria-label={t('Select image') + ' ' + (image.ImageIndex + 1)} onClick={() => setIndexes((current) => ({ ...current, [type]: image.ImageIndex }))}><SavedImagePreview image={image} name={target.name} /></button>}
        <div className="artwork-image-details"><div><strong>{t(artworkTypeNames[image.ImageType])} {image.ImageIndex + 1}</strong><span className="editor-badge">{t(image.Source)}</span></div>
          <div className="artwork-image-actions"><span>{multipleImages(type) && <><button className="editor-icon-button" type="button" aria-label={t('Move image earlier') + ' ' + (position + 1)} disabled={disabled || position === 0} onClick={() => move(position, -1)}><ArrowBackRounded aria-hidden="true" /></button><button className="editor-icon-button" type="button" aria-label={t('Move image later') + ' ' + (position + 1)} disabled={disabled || position === images.length - 1} onClick={() => move(position, 1)}><ArrowForwardRounded aria-hidden="true" /></button></>}</span><button className="editor-icon-button editor-danger" type="button" aria-label={t('Delete image') + ' ' + (image.ImageIndex + 1)} disabled={disabled} onClick={() => setRemoving(image)}><DeleteOutlineRounded aria-hidden="true" /></button></div>
        </div>
      </li>)}
    </ul>}
    {target.kind === 'items' && <OnlineImageCandidates itemId={target.id} imageType={type} disabled={disabled} busyCandidate={busy?.candidate} currentCandidate={currentCandidates[type + ':' + index]?.key} failures={failures} reloadKey={reloadRevision} sourceRevision={sourceRevision} initialSelection={initialSource} onChoose={(candidate) => void perform({ kind: 'candidate', type, index, candidate })} />}
    {target.kind !== 'users' && collection && <Button type="button" color="secondary" sx={{ alignSelf: 'flex-start' }} disabled={disabled} onClick={() => setResetting(true)}>Restore automatic images</Button>}
    {result?.error != null && <ErrorNotice error={result.error} />}
  </div>;

  const footer = <div className="artwork-editor-footer" role="status" aria-live="polite" data-state={review ? 'unknown' : loadError != null ? 'failed' : result?.state ?? 'idle'}>
    {busy && <CircularProgress size={16} color="inherit" />}
    <span>{busy ? t(artworkTypeNames[busy.type]) + ' · ' + t(busy.label) : review ? t('The image change could not be confirmed. Reload images and review the saved result before making another change.') : loadError != null ? t('Saved images could not be read. Reload images before making changes.') : result ? t(result.message) : t('Choose a candidate or upload a file. Each image is saved immediately.')}</span>
    {!review && result?.retry && <Button type="button" onClick={() => void perform(result.retry!)} disabled={disabled} startIcon={<RefreshRounded />}>Retry</Button>}
    <Button type="button" startIcon={<RefreshRounded />} onClick={reload} disabled={Boolean(busy) || loading}>Reload images</Button>
    {!embedded && <Button type="button" color="secondary" onClick={close} disabled={Boolean(busy)}>Close</Button>}
  </div>;

  return <>
    {embedded ? <section className="artwork-embedded" onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') { event.preventDefault(); event.stopPropagation(); } }}>{body}{footer}</section>
      : <Dialog open fullWidth maxWidth="md" className="media-editor-sheet artwork-editor-sheet" slotProps={{ paper: { sx: { width: '100%', maxWidth: 540 } } }} onClose={close} aria-labelledby="artwork-title">
        <DialogTitle id="artwork-dialog-heading"><span id="artwork-title">{t(target.kind === 'users' ? 'Manage avatar' : 'Manage artwork')}</span><span className="editor-heading-subtitle" translate="no">{target.name}</span></DialogTitle>
        <DialogContent>{body}</DialogContent><DialogActions>{footer}</DialogActions>
      </Dialog>}
    {(removing || resetting) && <Dialog open role="alertdialog" maxWidth="xs" fullWidth onClose={busy ? undefined : () => { setRemoving(undefined); setResetting(false); }} aria-labelledby="confirm-image-title">
      <DialogTitle id="confirm-image-title">{resetting ? 'Restore automatic images?' : 'Delete this image?'}</DialogTitle>
      <DialogContent><p className="editor-note">{t(resetting ? 'This removes your saved overrides for this image type and uses images found in the media source.' : 'This image will no longer be displayed. Original media files are kept.')}</p></DialogContent>
      <DialogActions><Button autoFocus type="button" onClick={() => { setRemoving(undefined); setResetting(false); }} disabled={Boolean(busy)}>Cancel</Button><Button type="button" variant="contained" color={resetting ? 'primary' : 'error'} disabled={disabled} onClick={() => void perform(removing ? { kind: 'remove', type: removing.ImageType, index: removing.ImageIndex, image: removing } : { kind: 'reset', type, index })}>{resetting ? 'Restore automatic' : 'Delete image'}</Button></DialogActions>
    </Dialog>}
  </>;
}
