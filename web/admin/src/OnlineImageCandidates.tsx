import { useEffect, useRef, useState } from 'react';
import { Alert, Button, CircularProgress, Skeleton } from './ui';
import { CheckRounded, ImageOutlined, RefreshRounded, SearchRounded } from './icons';
import { adminApi, isAbortError } from './api';
import { providersApi, providerImagePreview } from './providersApi';
import type { ImageCandidate, OnlineProvider } from './providersApi';
import type { ImageType } from './artworkApi';
import { ErrorNotice } from './components';
import { useTranslation } from './i18n';

export const artworkTypeNames: Record<ImageType, string> = {
  Primary: 'Poster', Backdrop: 'Backdrop', Thumb: 'Thumbnail', Banner: 'Banner', Logo: 'Logo',
  Art: 'Art', Disc: 'Disc', Box: 'Box', BoxRear: 'Box rear', Menu: 'Menu', Screenshot: 'Screenshot',
};

export function imageCandidateKey(candidate: ImageCandidate): string {
  return JSON.stringify([candidate.Provider, candidate.Id, candidate.ImageId, candidate.ImageType, candidate.Language]);
}

export type CandidateFailure = 'retry' | 'blocked' | 'review';
export interface OnlineImageSelection { Provider: string; Id: string; Language: string }

export function lowResolution(type: string, width: number, height: number): boolean {
  const minimum = type === 'Primary' ? [1000, 1500] : type === 'Backdrop' ? [1920, 1080] : type === 'Logo' ? [400, 150] : undefined;
  return Boolean(minimum && (width < minimum[0] || height < minimum[1]));
}

export function OnlineImageCandidates({ itemId, imageType, disabled, busyCandidate, currentCandidate, failures, reloadKey, sourceRevision, initialSelection, onChoose }: {
  itemId: string;
  imageType: ImageType;
  disabled: boolean;
  busyCandidate?: string;
  currentCandidate?: string;
  failures: Record<string, CandidateFailure>;
  reloadKey: number;
  sourceRevision?: string;
  initialSelection?: OnlineImageSelection;
  onChoose: (candidate: ImageCandidate) => void;
}) {
  const { t, locale } = useTranslation();
  const number = new Intl.NumberFormat(locale);
  const [providers, setProviders] = useState<OnlineProvider[]>([]);
  const [enabled, setEnabled] = useState(false);
  const [supported, setSupported] = useState(true);
  const [provider, setProvider] = useState('');
  const [identifier, setIdentifier] = useState('');
  const [language, setLanguage] = useState('en');
  const [candidates, setCandidates] = useState<ImageCandidate[]>();
  const [loading, setLoading] = useState(true);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState<unknown>();
  const [localReload, setLocalReload] = useState(0);
  const [previewFailures, setPreviewFailures] = useState<Record<string, boolean>>({});
  const [previewAttempts, setPreviewAttempts] = useState<Record<string, number>>({});
  const operation = useRef<AbortController | undefined>(undefined);
  const selectedProvider = providers.find((entry) => entry.Id === provider);
  const languageValid = /^[a-z]{2,3}(-[A-Z]{2})?$/.test(language);
  const shown = candidates?.filter((candidate) => candidate.ImageType === imageType);

  useEffect(() => {
    const controller = new AbortController();
    operation.current?.abort();
    operation.current = controller;
    setLoading(true); setError(undefined); setCandidates(undefined); setPreviewFailures({}); setPreviewAttempts({});
    void Promise.all([
      adminApi.getItemMetadata(itemId, { signal: controller.signal }),
      providersApi.getProviders({ signal: controller.signal }),
    ]).then(async ([detail, available]) => {
      if (controller.signal.aborted) return;
      const supportsImages = ['Movie', 'Video', 'Series', 'Season', 'Episode'].includes(detail.Item.Type);
      const imageProviders = available.Items.filter((entry) => entry.Capabilities.includes('images'));
      const initial = imageProviders.find((entry) => entry.Id === initialSelection?.Provider) ?? imageProviders.find((entry) => entry.Id === 'tmdb') ?? imageProviders[0];
      const savedId = initialSelection && initialSelection.Provider === initial?.Id ? initialSelection.Id : Object.entries(detail.Effective.ProviderIds).find(([key]) => key.toLowerCase() === initial?.Id.toLowerCase())?.[1] ?? '';
      const initialLanguage = initialSelection?.Language || 'en';
      setProviders(imageProviders); setEnabled(available.Enabled); setSupported(supportsImages);
      setProvider(initial?.Id ?? ''); setIdentifier(savedId); setLanguage(initialLanguage);
      if (!supportsImages || !available.Enabled || !initial?.Configured || !savedId) return;
      setSearching(true);
      const result = await providersApi.images(itemId, { Provider: initial.Id, Id: savedId, Language: initialLanguage }, { signal: controller.signal });
      if (!controller.signal.aborted) setCandidates(result.Items);
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    }).finally(() => {
      if (operation.current === controller) operation.current = undefined;
      if (!controller.signal.aborted) { setLoading(false); setSearching(false); }
    });
    return () => { controller.abort(); operation.current?.abort(); };
  }, [itemId, reloadKey, sourceRevision, localReload, initialSelection?.Provider, initialSelection?.Id, initialSelection?.Language]);

  async function search() {
    if (operation.current || disabled || !enabled || !supported || !selectedProvider?.Configured || !identifier.trim() || !languageValid) return;
    const controller = new AbortController();
    operation.current = controller; setSearching(true); setError(undefined);
    try {
      const result = await providersApi.images(itemId, { Provider: provider, Id: identifier.trim(), Language: language }, { signal: controller.signal });
      if (!controller.signal.aborted) { setCandidates(result.Items); setPreviewFailures({}); setPreviewAttempts({}); }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    } finally {
      if (operation.current === controller) operation.current = undefined;
      if (!controller.signal.aborted) setSearching(false);
    }
  }

  return <section className="artwork-online-section" aria-label={t('Online image candidates')} aria-busy={loading || searching}>
    <div className="artwork-section-heading"><h3>{t('Online image candidates')}</h3><span>{t('Each image is applied immediately')}</span></div>
    {error != null && <ErrorNotice error={error} retry={disabled ? undefined : () => setLocalReload((value) => value + 1)} />}
    {loading && <div className="artwork-candidate-grid" aria-label={t('Loading image candidates')}>{[0, 1, 2].map((value) => <Skeleton key={value} variant="rounded" height={160} />)}</div>}
    {!loading && !supported && <p className="editor-note">{t('Online images are unavailable for this item type. Upload an image from a file.')}</p>}
    {!loading && supported && !enabled && <Alert severity="info">Internet providers are disabled. Enable them in Settings to use online sources.</Alert>}
    {!loading && supported && enabled && providers.length === 0 && <p className="editor-note">{t('No image provider is configured. Upload an image or check Online providers in Settings.')}</p>}
    {!loading && supported && providers.length > 0 && <>
      <details className="artwork-search-options" open={!candidates}>
        <summary>{t('Image search options')}</summary>
        <div className="artwork-search-fields">
          <label className="editor-field"><span>{t('Online provider')}</span><select value={provider} disabled={disabled || searching || !enabled} onChange={(event) => { setProvider(event.target.value); setIdentifier(''); setCandidates(undefined); }}>
            {providers.map((entry) => <option key={entry.Id} value={entry.Id}>{entry.Name}{entry.Configured ? '' : ' · ' + t('Unavailable')}</option>)}
          </select></label>
          <label className="editor-field"><span>{t('Language')}</span><input value={language} disabled={disabled || searching || !enabled} aria-invalid={!languageValid} onChange={(event) => { setLanguage(event.target.value); setCandidates(undefined); }} /></label>
          <label className="editor-field artwork-provider-identifier"><span>{t('Provider item identifier')}</span><input value={identifier} disabled={disabled || searching || !enabled} onChange={(event) => { setIdentifier(event.target.value); setCandidates(undefined); }} /></label>
        </div>
        <p className="editor-note">{t('Search for a metadata match first, or enter the identifier from the provider.')}</p>
        <Button type="button" size="small" variant="outlined" startIcon={searching ? <CircularProgress size={14} color="inherit" /> : <SearchRounded />} disabled={disabled || searching || !enabled || !selectedProvider?.Configured || !identifier.trim() || !languageValid} onClick={() => void search()}>{searching ? 'Searching images' : 'Search images'}</Button>
      </details>
      {selectedProvider?.Attribution && <p className="artwork-provider-attribution" translate="no">{selectedProvider.Attribution}</p>}
      {shown?.length === 0 && <div className="artwork-candidates-empty"><ImageOutlined aria-hidden="true" /><p>{t('No images of this type were returned. Choose another image type.')}</p></div>}
      <div className="artwork-candidate-grid" data-image-type={imageType}>
        {shown?.map((candidate) => {
          const key = imageCandidateKey(candidate);
          const current = currentCandidate === key;
          const active = busyCandidate === key;
          const failure = failures[key];
          const preview = providerImagePreview(candidate.PreviewUrl);
          const previewFailed = previewFailures[key] || !preview;
          const providerName = providers.find((entry) => entry.Id === candidate.Provider)?.Name ?? candidate.Provider;
          const typeName = t(artworkTypeNames[imageType]);
          const label = (failure === 'retry' ? t('Retry: ') : '') + t('Use ') + providerName + ' ' + candidate.Width + ' × ' + candidate.Height + ' ' + typeName;
          return <article key={key} className={'artwork-candidate' + (current ? ' is-current' : '') + (failure ? ' has-failure' : '')} data-candidate-id={candidate.ImageId}>
            <button type="button" className="artwork-candidate-choice" aria-label={label} aria-pressed={current} aria-busy={active} disabled={disabled || current || failure === 'blocked' || failure === 'review'} onClick={() => onChoose(candidate)}>
              <span className="artwork-candidate-picture" data-image-type={imageType}>
                {previewFailed ? <span className="artwork-candidate-placeholder"><ImageOutlined aria-hidden="true" /><span>{t('Preview unavailable')}</span></span>
                  : <img key={previewAttempts[key] ?? 0} src={preview} alt={t('Image candidate')} loading="lazy" referrerPolicy="no-referrer" onError={() => setPreviewFailures((value) => ({ ...value, [key]: true }))} />}
                {current && <span className="artwork-candidate-current"><CheckRounded aria-hidden="true" />{t('Current')}</span>}
                {active && <span className="artwork-candidate-overlay"><CircularProgress size={16} color="inherit" />{t('Applying image...')}</span>}
                {!active && failure && <span className="artwork-candidate-overlay" data-state={failure}>{t(failure === 'retry' ? 'Failed · click to retry' : failure === 'review' ? 'Review saved images' : 'Image could not be applied')}</span>}
              </span>
              <span className="artwork-candidate-source" translate="no">{providerName}</span>
              <span className="artwork-candidate-meta">{number.format(candidate.Width)} × {number.format(candidate.Height)} · {candidate.Language ? <>{t('Search language')} <span translate="no">{candidate.Language}</span></> : t('Language not provided')}</span>
              {lowResolution(candidate.ImageType, candidate.Width, candidate.Height) && <span className="artwork-resolution-warning">{t('Low resolution')}</span>}
            </button>
            {preview && previewFailures[key] && <button type="button" className="artwork-preview-retry" disabled={disabled} onClick={() => { setPreviewFailures((value) => ({ ...value, [key]: false })); setPreviewAttempts((value) => ({ ...value, [key]: (value[key] ?? 0) + 1 })); }}><RefreshRounded aria-hidden="true" />{t('Retry preview')}</button>}
          </article>;
        })}
      </div>
    </>}
  </section>;
}
