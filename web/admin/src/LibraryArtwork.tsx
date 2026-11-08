import { useEffect, useRef, useState } from 'react';
import { Alert, Button, CircularProgress, Skeleton, Stack } from './ui';
import { DownloadRounded, ImageOutlined, RefreshRounded } from './icons';
import { ApiError, isAbortError } from './api';
import type { MetadataItemSummary } from './api';
import { artworkApi } from './artworkApi';
import type { ArtworkCollection, ArtworkTarget, ImageType } from './artworkApi';
import { providersApi } from './providersApi';
import type { MissingImageResult } from './providersApi';
import { ErrorNotice } from './components';
import { useTranslation } from './i18n';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

type DisplayType = 'Primary' | 'Backdrop' | 'Logo';
type ItemStatus = MissingImageResult['Status'] | 'queued' | 'downloading' | 'not_attempted' | 'failed' | 'unknown' | 'reviewed';
interface ItemResult { status: ItemStatus; retry: boolean; error?: unknown }
interface DownloadTarget { target: ArtworkTarget; libraryId: string; type: DisplayType }
interface BatchProgress { type: DisplayType; total: number; completed: number; downloaded: number }

const typeNames: Record<DisplayType, string> = { Primary: 'Posters', Backdrop: 'Backdrops', Logo: 'Logos' };
const statusNames: Record<ItemStatus, string> = {
  downloaded: 'Image downloaded',
  no_candidate: 'No online image found',
  already_present: 'Image already present',
  managed: 'Manual image choice retained',
  unmatched: 'Identify this item before downloading images',
  unsupported: 'Online images are unavailable for this item type',
  queued: 'Waiting to download',
  downloading: 'Downloading image...',
  not_attempted: 'Not attempted',
  failed: 'Image download was rejected',
  unknown: 'Image download needs review',
  reviewed: 'Saved images reloaded',
};

function resultKey(itemId: string, type: ImageType): string { return `${itemId}:${type}`; }
function unknownResult(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 0 || error.status === 409 || error.status >= 500
    || ['network_error', 'invalid_response', 'session_changed'].includes(error.code);
}
function retryable(error: unknown): boolean {
  return error instanceof ApiError && error.status === 429 && ['provider_quota', 'artwork_limit'].includes(error.code);
}
function targetFor(item: Pick<MetadataItemSummary, 'Id' | 'Name'>): ArtworkTarget {
  return { kind: 'items', id: item.Id, name: item.Name };
}
function lowResolution(type: DisplayType, width: number, height: number): boolean {
  const minimum = type === 'Primary' ? [1000, 1500] : type === 'Backdrop' ? [1920, 1080] : [400, 150];
  return width < minimum[0] || height < minimum[1];
}

export function LibraryArtwork({ libraryId, items, revision, onManage, onChanged, onNavigationGuardChange }: {
  libraryId: string;
  items: MetadataItemSummary[];
  revision: number;
  onManage: (target: ArtworkTarget, type: ImageType) => void;
  onChanged?: () => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const { t, locale } = useTranslation();
  const number = new Intl.NumberFormat(locale);
  const [images, setImages] = useState<Record<string, ArtworkCollection>>({});
  const [readErrors, setReadErrors] = useState<Record<string, unknown>>({});
  const [previewErrors, setPreviewErrors] = useState<Record<string, string>>({});
  const [type, setType] = useState<DisplayType>('Primary');
  const [filter, setFilter] = useState<'all' | 'missing' | 'small'>('all');
  const [results, setResults] = useState<Record<string, ItemResult>>({});
  const [reviewTargets, setReviewTargets] = useState<Record<string, DownloadTarget>>({});
  const [reading, setReading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<BatchProgress>();
  const readOperation = useRef<AbortController | undefined>(undefined);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const itemsRef = useRef(items);
  const reviewRef = useRef(reviewTargets);
  itemsRef.current = items;
  reviewRef.current = reviewTargets;
  const itemKey = JSON.stringify(items.map((item) => [item.Id, item.Name]));
  const reviewRequired = Object.keys(reviewTargets).length > 0;
  const disabled = busy || reading || reviewRequired;
  useUserDraftNavigation(reviewRequired, busy, onNavigationGuardChange, t('An image download needs review. Leave without reloading saved images?'));

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; readOperation.current?.abort(); };
  }, []);

  async function readCollections(targets: ArtworkTarget[], explicitReview: boolean) {
    readOperation.current?.abort();
    const controller = new AbortController(); readOperation.current = controller;
    const queue = [...new Map(targets.map((target) => [target.id, target])).values()];
    const requestedIds = new Set(queue.map((target) => target.id));
    const refreshed = new Set<string>();
    setReading(true);
    setReadErrors((current) => Object.fromEntries(Object.entries(current).filter(([id]) => !requestedIds.has(id))));
    async function readNext() {
      while (queue.length > 0 && !controller.signal.aborted) {
        const target = queue.shift(); if (!target) return;
        try {
          const value = await artworkApi.list(target, { signal: controller.signal });
          if (controller.signal.aborted || !mounted.current) return;
          refreshed.add(target.id);
          setImages((current) => ({ ...current, [target.id]: value }));
          setReadErrors((current) => Object.fromEntries(Object.entries(current).filter(([id]) => id !== target.id)));
        } catch (error) {
          if (!controller.signal.aborted && mounted.current && !isAbortError(error)) setReadErrors((current) => ({ ...current, [target.id]: error }));
        }
      }
    }
    try {
      await Promise.all(Array.from({ length: Math.min(4, queue.length) }, readNext));
      if (controller.signal.aborted || !mounted.current) return;
      // Ordinary parent refreshes never clear an uncertain mutation. Only an
      // explicit reload can reconcile every retained unknown target.
      const pendingReview = reviewRef.current;
      if (explicitReview && Object.values(pendingReview).every((entry) => refreshed.has(entry.target.id))) {
        setResults((current) => {
          const next = { ...current };
          for (const key of Object.keys(pendingReview)) next[key] = { status: 'reviewed', retry: false };
          return next;
        });
        reviewRef.current = {}; setReviewTargets({});
      }
    } finally {
      if (readOperation.current === controller) readOperation.current = undefined;
      if (!controller.signal.aborted && mounted.current) setReading(false);
    }
  }

  useEffect(() => {
    if (inFlight.current) return;
    void readCollections(itemsRef.current.map(targetFor), false);
    return () => readOperation.current?.abort();
  }, [libraryId, itemKey, revision, busy]);

  function reload() {
    if (inFlight.current) return;
    const targets = [...itemsRef.current.map(targetFor), ...Object.values(reviewRef.current).map((entry) => entry.target)];
    void readCollections(targets, true);
  }

  async function download(queue: DownloadTarget[]) {
    if (inFlight.current || reading || reviewRequired || queue.length === 0) return;
    inFlight.current = true;
    readOperation.current?.abort();
    setBusy(true);
    const batchType = queue[0].type;
    const all = [...queue];
    setProgress({ type: batchType, total: all.length, completed: 0, downloaded: 0 });
    setResults((current) => {
      const next = { ...current };
      for (const entry of all) next[resultKey(entry.target.id, entry.type)] = { status: 'queued', retry: false };
      return next;
    });
    let completed = 0;
    let downloaded = 0;
    let nextIndex = 0;
    try {
      for (; nextIndex < all.length && mounted.current; nextIndex += 1) {
        const entry = all[nextIndex];
        const key = resultKey(entry.target.id, entry.type);
        setResults((current) => ({ ...current, [key]: { status: 'downloading', retry: false } }));
        try {
          // Each entry retains its original page scope and image type even if
          // parent data refreshes while the sequential batch is in progress.
          const response = await providersApi.downloadMissingImage(entry.target.id, { LibraryId: entry.libraryId, ImageType: entry.type });
          if (!mounted.current) return;
          if (response.ItemId !== entry.target.id || response.LibraryId !== entry.libraryId || response.ImageType !== entry.type) {
            throw new ApiError('The image response is incomplete. Reload images before making changes.', { code: 'invalid_response' });
          }
          setImages((current) => ({ ...current, [entry.target.id]: response.Artwork }));
          setResults((current) => ({ ...current, [key]: { status: response.Status, retry: false } }));
          completed += 1;
          if (response.Status === 'downloaded') downloaded += 1;
          setProgress({ type: batchType, total: all.length, completed, downloaded });
        } catch (error) {
          if (!mounted.current) return;
          const unknown = unknownResult(error);
          const canRetry = retryable(error);
          setResults((current) => ({ ...current, [key]: { status: unknown ? 'unknown' : 'failed', retry: !unknown && canRetry, error } }));
          completed += 1;
          setProgress({ type: batchType, total: all.length, completed, downloaded });
          if (unknown) {
            const pending = { ...reviewRef.current, [key]: entry };
            reviewRef.current = pending; setReviewTargets(pending);
          }
          if (unknown || canRetry) { nextIndex += 1; break; }
        }
      }
    } finally {
      inFlight.current = false;
      if (mounted.current) {
        if (nextIndex < all.length) setResults((current) => {
          const next = { ...current };
          for (const entry of all.slice(nextIndex)) next[resultKey(entry.target.id, entry.type)] = { status: 'not_attempted', retry: false };
          return next;
        });
        setBusy(false);
        if (downloaded > 0) onChanged?.();
      }
    }
  }

  const imageFor = (itemId: string) => images[itemId]?.Items.find((image) => image.ImageType === type);
  const readable = (itemId: string) => Boolean(images[itemId] && !Object.hasOwn(readErrors, itemId));
  const missing = items.filter((item) => readable(item.Id) && !imageFor(item.Id));
  const small = items.filter((item) => { const image = imageFor(item.Id); return readable(item.Id) && Boolean(image && lowResolution(type, image.Width, image.Height)); });
  const unavailable = items.filter((item) => Object.hasOwn(readErrors, item.Id)).length;
  const visible = items.filter((item) => filter === 'all' || (filter === 'missing' ? missing : small).some((entry) => entry.Id === item.Id));

  return <section className="library-artwork" aria-label={t('Library images')}>
    <div className="media-artwork-toolbar">
      <div className="media-segmented" role="group" aria-label={t('Image type')}>{(['Primary', 'Backdrop', 'Logo'] as const).map((value) => <button key={value} type="button" aria-pressed={type === value} disabled={busy} onClick={() => setType(value)}>{t(typeNames[value])}</button>)}</div>
      <label className="media-filter-label">{t('Show')}<select value={filter} disabled={busy} onChange={(event) => setFilter(event.target.value as 'all' | 'missing' | 'small')}><option value="all">{t('All images')}</option><option value="missing">{t('Missing images')}</option><option value="small">{t('Low resolution')}</option></select></label>
      <Button type="button" size="small" variant="outlined" startIcon={busy ? <CircularProgress size={14} color="inherit" /> : <DownloadRounded />} disabled={disabled || missing.length === 0} onClick={() => void download(missing.map((item) => ({ target: targetFor(item), libraryId, type })))}>Download missing images on this page</Button>
      <Button type="button" size="small" startIcon={<RefreshRounded />} disabled={busy || reading} onClick={reload}>Reload images</Button>
    </div>
    <p className="media-muted">{t('This page and the selected image type only. Existing images and manual choices are kept.')}</p>
    <p className="media-muted">{t('On this page')} {number.format(visible.length)} / {number.format(items.length)} · {t('Missing images')} {number.format(missing.length)} · {t('Low resolution')} {number.format(small.length)}</p>
    {unavailable > 0 && <Alert severity="warning">{t('Some image collections could not be read. They are excluded from missing-image downloads.')} · {number.format(unavailable)}</Alert>}
    {reviewRequired && <Alert severity="warning">{t('An image download could not be confirmed. Reload images and review the saved results before starting another download.')}</Alert>}
    {progress && <p role="status" aria-live="polite" className="media-muted">{t(typeNames[progress.type])} · {busy ? t('Downloading images...') : t(progress.completed < progress.total ? 'Image download pass stopped' : 'Image download pass finished')} · {t('Processed')} {number.format(progress.completed)} / {number.format(progress.total)} · {t('Downloaded')} {number.format(progress.downloaded)}</p>}
    <div className={`media-artwork-grid library-artwork-grid ${type !== 'Primary' ? 'media-artwork-wide' : ''}`} data-image-type={type} aria-busy={reading}>{visible.map((item) => {
      const image = imageFor(item.Id);
      const result = results[resultKey(item.Id, type)];
      const previewFailed = image && previewErrors[item.Id] === image.PreviewUrl;
      return <Stack key={item.Id} spacing={0.75} className="library-artwork-entry" data-item-id={item.Id}>
        <button type="button" className="media-artwork-card" disabled={disabled} onClick={() => onManage(targetFor(item), type)} aria-label={`${t('Manage images')}: ${item.Name}`}>
          <span className="media-artwork-preview">{image && !previewFailed ? <img src={image.PreviewUrl} alt={item.Name} loading="lazy" onError={() => setPreviewErrors((current) => ({ ...current, [item.Id]: image.PreviewUrl }))} /> : result?.status === 'downloading' ? <CircularProgress size={24} /> : images[item.Id] || Object.hasOwn(readErrors, item.Id) ? <ImageOutlined sx={{ fontSize: 28 }} /> : <Skeleton variant="rounded" width="100%" height="100%" />}</span>
          <span className="media-strong media-ellipsis" translate="no">{item.Name}</span>
          <span className={`media-secondary-line ${image && lowResolution(type, image.Width, image.Height) ? 'media-warning' : ''}`}>{Object.hasOwn(readErrors, item.Id) ? t('Image status unavailable') : image ? `${number.format(image.Width)} × ${number.format(image.Height)} · ${t(image.Source)}` : images[item.Id] ? t('No image') : t('Loading…')}</span>
          {previewFailed && <span className="media-secondary-line">{t('Preview unavailable')}</span>}
        </button>
        {result && <span className={`library-artwork-result ${result.status === 'unknown' || result.status === 'failed' ? 'media-warning' : ''}`} data-download-status={result.status}>{t(statusNames[result.status])}</span>}
        {result?.retry && <Button type="button" size="small" disabled={disabled} onClick={() => void download([{ target: targetFor(item), libraryId, type }])} startIcon={<RefreshRounded />}>{t('Retry image download')}: <span translate="no">{item.Name}</span></Button>}
        {result?.error != null && <ErrorNotice error={result.error} />}
      </Stack>;
    })}</div>
    {visible.length === 0 && <div className="media-empty-state" role={reading ? 'status' : undefined}>{t(reading ? 'Loading…' : 'No matching images on this page.')}</div>}
  </section>;
}
