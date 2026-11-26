import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { adminApi, ApiError, isAbortError } from './api';
import type { MetadataDetail, MetadataItemSummary } from './api';
import { ErrorNotice } from './components';
import { SearchRounded, VideoLibraryOutlined } from './icons';
import { useTranslation } from './i18n';
import { providersApi } from './providersApi';
import type { MetadataCandidate, OnlineProvider } from './providersApi';
import { Alert, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Skeleton, Stack, TextField } from './ui';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

function metadataProvider(type: string): string | undefined {
  if (['Movie', 'Video', 'Series', 'Season', 'Episode'].includes(type)) return 'tmdb';
  if (['MusicArtist', 'MusicAlbum', 'Audio'].includes(type)) return 'musicbrainz';
  return undefined;
}

function requiresReview(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 0 || error.status === 409 || error.status >= 500
    || ['network_error', 'invalid_response', 'session_changed'].includes(error.code);
}

export function IdentifyMetadataDialog({ item, onClose, onApplied, onNavigationGuardChange }: {
  item: MetadataItemSummary;
  onClose: (needsRefresh: boolean) => void;
  onApplied: (name: string) => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const { t } = useTranslation();
  const { Id: itemId, LibraryId: libraryId } = item;
  const [detail, setDetail] = useState<MetadataDetail>();
  const [provider, setProvider] = useState<OnlineProvider>();
  const [providersEnabled, setProvidersEnabled] = useState(false);
  const [title, setTitle] = useState('');
  const [year, setYear] = useState('');
  const [matches, setMatches] = useState<MetadataCandidate[]>();
  const [selectedId, setSelectedId] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState<'search' | 'apply'>();
  const [reviewRequired, setReviewRequired] = useState(false);
  const [revision, setRevision] = useState(0);
  const operation = useRef<AbortController | undefined>(undefined);
  const needsRefresh = useRef(false);

  const type = detail?.Item.Type ?? item.Type;
  const providerId = metadataProvider(type);
  const seriesScoped = type === 'Season' || type === 'Episode';
  const supportsYear = !seriesScoped && type !== 'MusicArtist';
  const name = title.trim();
  const yearValid = !year || /^\d{1,4}$/.test(year) && Number(year) > 0;
  const nameTooLong = new TextEncoder().encode(name).length > 1024;
  const selected = matches?.find((match) => match.Id === selectedId);
  const disabled = loading || loadError != null || Boolean(busy) || reviewRequired || !provider?.Configured || !providersEnabled;

  useUserDraftNavigation(false, Boolean(busy), onNavigationGuardChange);
  useEffect(() => () => operation.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setLoadError(undefined);
    void Promise.all([adminApi.getItemMetadata(itemId, { signal: controller.signal }), providersApi.getProviders({ signal: controller.signal })])
      .then(([value, providers]) => {
        if (controller.signal.aborted) return;
        if (value.Item.LibraryId !== libraryId) throw new ApiError('This item is no longer in the selected library.', { code: 'invalid_response' });
        const itemType = value.Item.Type;
        const providerId = metadataProvider(itemType);
        setDetail(value);
        setProvidersEnabled(providers.Enabled);
        setProvider(providers.Items.find((entry) => entry.Id === providerId && entry.Capabilities.includes('metadata')));
        // Season and episode searches use the server-resolved series title and
        // catalog positions by default, not the item's own episode/season title.
        setTitle(itemType === 'Season' || itemType === 'Episode' ? '' : value.Effective.Name);
        setYear(value.Effective.ProductionYear == null ? '' : String(value.Effective.ProductionYear));
        clearCandidates(); setError(undefined); setReviewRequired(false);
      }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [itemId, libraryId, revision]);

  function clearCandidates() { setMatches(undefined); setSelectedId(''); }
  function close() { if (!operation.current) onClose(needsRefresh.current); }
  function reload() { setRevision((value) => value + 1); }

  async function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!detail || !provider || disabled || operation.current || !yearValid || nameTooLong || !name && !seriesScoped) return;
    const controller = new AbortController(); operation.current = controller;
    setBusy('search'); setError(undefined); clearCandidates();
    try {
      const { Items } = await providersApi.search(itemId, {
        Provider: provider.Id,
        Name: name,
        ...(supportsYear && year ? { Year: Number(year) } : {}),
        // TMDB resolves an empty language to the server's metadata preference.
        Language: '',
      }, { signal: controller.signal });
      if (Items.some((match) => match.Provider !== provider.Id || match.Type !== detail.Item.Type || !match.Id || !match.Name.trim())
        || new Set(Items.map((match) => match.Id)).size !== Items.length) {
        throw new ApiError('The provider returned an invalid set of candidates.', { code: 'invalid_response' });
      }
      if (!controller.signal.aborted) setMatches(Items);
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    } finally {
      if (operation.current === controller) operation.current = undefined;
      if (!controller.signal.aborted) setBusy(undefined);
    }
  }

  async function apply() {
    if (!detail || !selected || disabled || operation.current) return;
    const controller = new AbortController(); operation.current = controller;
    setBusy('apply'); setError(undefined);
    try {
      const result = await providersApi.apply(itemId, {
        Provider: selected.Provider, Id: selected.Id, Language: selected.Language, Revision: detail.Revision,
      }, { signal: controller.signal });
      if (result?.Item?.Id !== itemId || result.Item.LibraryId !== libraryId || typeof result.Revision !== 'string' || !/^[1-9]\d*$/.test(result.Revision)) {
        throw new ApiError('The provider update response could not be confirmed.', { code: 'invalid_response' });
      }
      if (!controller.signal.aborted) onApplied(selected.Name);
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) {
        setError(cause);
        if (requiresReview(cause)) { setReviewRequired(true); needsRefresh.current = true; }
      }
    } finally {
      if (operation.current === controller) operation.current = undefined;
      if (!controller.signal.aborted) setBusy(undefined);
    }
  }

  return <Dialog open fullWidth maxWidth="sm" className="media-identify-sheet" onClose={busy ? undefined : close} aria-labelledby="media-identify-title">
    <DialogTitle id="media-identify-title">Re-identify</DialogTitle>
    <DialogContent aria-busy={loading || Boolean(busy)}>
      <Stack spacing={2}>
        <div className="media-identify-identity"><strong translate="no">{detail?.Effective.Name || item.Name || t('Untitled item')}</strong>{item.Path && <span className="media-muted media-mono" translate="no">{item.Path}</span>}</div>
        {loading && <Skeleton height={92} aria-label="Loading identification settings" />}
        {loadError != null && <ErrorNotice error={loadError} retry={reload} />}
        {error != null && <ErrorNotice error={error} />}
        {reviewRequired && loadError == null && <Alert severity="warning">The change conflicted with a newer version or its result could not be confirmed. Reload this item before making another change.<Button size="small" color="inherit" disabled={loading || Boolean(busy)} onClick={reload}>Reload latest item</Button></Alert>}
        {detail && !loading && loadError == null && <>
          {!providerId ? <Alert severity="info">Online identification is not available for this item type.</Alert>
            : !providersEnabled ? <Alert severity="info">Internet providers are disabled. Enable them in Settings to use online sources.</Alert>
              : !provider?.Configured && <Alert severity="info">No online metadata provider is configured for this item type.</Alert>}
          {provider && <form onSubmit={search} className="media-identify-form">
            <div className="media-identify-fields">
              <TextField autoFocus size="small" label={seriesScoped ? 'Series title (optional)' : 'Search title'} placeholder={seriesScoped ? 'Use the saved series title' : undefined} value={title} disabled={disabled} error={nameTooLong} helperText={nameTooLong ? 'Use no more than 1,024 UTF-8 bytes for the title.' : undefined} onChange={(event) => { setTitle(event.target.value); clearCandidates(); }} />
              {supportsYear && <TextField size="small" label="Year (optional)" value={year} disabled={disabled} error={!yearValid} helperText={!yearValid ? 'Enter a year from 1 to 9999.' : undefined} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 4 } }} onChange={(event) => { setYear(event.target.value); clearCandidates(); }} />}
            </div>
            <div className="media-identify-context">
              <span className="media-muted">{t('Online provider')}: <span translate="no">{provider.Name}</span></span>
              <Button type="submit" size="small" variant="outlined" startIcon={<SearchRounded />} disabled={disabled || nameTooLong || !yearValid || !name && !seriesScoped}>Search candidates</Button>
            </div>
            {seriesScoped && <span className="media-muted">{t('Results name the series. Applying a match uses this item’s saved season and episode numbers.')}</span>}
            {supportsYear && !year && <span className="media-muted">{t('A blank year uses the saved year, if any.')}</span>}
            {provider.Attribution && <span className="media-muted" translate="no">{provider.Attribution}</span>}
          </form>}
          {matches?.length === 0 && <p className="media-muted" role="status">{t('No matching metadata found. Try another title or year.')}</p>}
          {matches && matches.length > 0 && <fieldset className="media-identify-candidates" disabled={disabled}>
            <legend>{t('Candidate results')}</legend>
            {matches.map((match) => <label key={match.Id} className="media-identify-candidate" data-selected={match.Id === selectedId}>
              <input type="radio" name="identify-candidate" aria-label={match.Year ? `${match.Name} (${match.Year})` : match.Name} checked={match.Id === selectedId} onChange={() => setSelectedId(match.Id)} />
              <span className="media-item-tile" aria-hidden="true"><VideoLibraryOutlined sx={{ fontSize: 16 }} /></span>
              <span className="media-identify-match" translate="no">
                <strong>{match.Name}{match.Year ? <small className="media-mono media-muted"> · {match.Year}</small> : null}</strong>
                {match.OriginalTitle && match.OriginalTitle !== match.Name && <span className="media-muted">{match.OriginalTitle}</span>}
                <span className="media-mono media-muted">{match.Id}{match.Language && ` · ${match.Language}`}</span>
                {match.Overview && <span>{match.Overview}</span>}
              </span>
            </label>)}
          </fieldset>}
        </>}
        <p id="media-identify-consequence" className="media-muted media-identify-consequence">{t('Applying a match updates this item’s automatic metadata from the selected online entry. Manual overrides and locked fields stay unchanged.')}</p>
      </Stack>
    </DialogContent>
    <DialogActions sx={{ flexWrap: 'wrap' }}>
      {busy && <span role="status" className="media-identify-progress"><CircularProgress size={16} />{t(busy === 'search' ? 'Searching candidates…' : 'Applying identification…')}</span>}
      <Button color="secondary" disabled={Boolean(busy)} onClick={close}>Cancel</Button>
      <Button variant="contained" aria-describedby="media-identify-consequence" disabled={disabled || !selected} onClick={() => void apply()}>Apply identification</Button>
    </DialogActions>
  </Dialog>;
}
