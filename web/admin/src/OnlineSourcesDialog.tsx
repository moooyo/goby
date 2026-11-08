import { adminLocale, useTranslation } from './i18n';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Link, MenuItem, Paper, Skeleton, Stack, Switch, Tab, Tabs, TextField, Typography } from './ui';
import { DownloadRounded } from './icons';
import { RefreshRounded } from './icons';
import { SearchRounded } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { MetadataDetail } from './api';
import { ErrorNotice } from './components';
import { providersApi, providerWebsite } from './providersApi';
import type { MetadataCandidate, OnlineProviders, ProviderProvenance, SubtitleCandidate } from './providersApi';
import { ArtworkManagerDialog } from './ArtworkManagerDialog';
import type { ArtworkEditorState } from './ArtworkManagerDialog';
import type { OnlineImageSelection } from './OnlineImageCandidates';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';
import './online-sources.css';

type SourceTab = 'metadata' | 'images' | 'subtitles';
function uncertain(error: unknown) { return !(error instanceof ApiError) || error.status === 0 || error.status === 409 || error.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(error.code); }
function savedProviderId(detail: MetadataDetail | undefined, provider: string) {
  return Object.entries(detail?.Effective.ProviderIds ?? {}).find(([key]) => key.toLowerCase() === provider.toLowerCase())?.[1] ?? '';
}

export function OnlineSourcesDialog({ itemId, onClose, onSaved, onNavigationGuardChange, initialTab = 'metadata' }: { itemId: string; onClose: () => void; onSaved: () => void; onNavigationGuardChange: UserNavigationGuardChange; initialTab?: SourceTab }) {
  const { formatCount } = useTranslation();
  const [detail, setDetail] = useState<MetadataDetail>();
  const [providers, setProviders] = useState<OnlineProviders>();
  const [provenance, setProvenance] = useState<ProviderProvenance[]>([]);
  const [tab, setTab] = useState<SourceTab>(initialTab);
  const [provider, setProvider] = useState('');
  const [name, setName] = useState('');
  const [year, setYear] = useState('');
  const [language, setLanguage] = useState('en');
  const [languages, setLanguages] = useState('en');
  const [hearingImpaired, setHearingImpaired] = useState(false);
  const [matches, setMatches] = useState<MetadataCandidate[]>();
  const [imageSelection, setImageSelection] = useState<OnlineImageSelection>();
  const [imagesVisited, setImagesVisited] = useState(initialTab === 'images');
  const [artworkBusy, setArtworkBusy] = useState(false);
  const [subtitles, setSubtitles] = useState<SubtitleCandidate[]>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [reviewRequired, setReviewRequired] = useState(false);
  const [revision, setRevision] = useState(0);
  const operation = useRef<AbortController | undefined>(undefined);
  const ownGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const childGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const publishGuard = useCallback(() => {
    onNavigationGuardChange(ownGuard.current || childGuard.current
      ? () => (!childGuard.current || childGuard.current()) && (!ownGuard.current || ownGuard.current())
      : undefined);
  }, [onNavigationGuardChange]);
  const registerOwnGuard = useCallback((guard: UserNavigationGuard | undefined) => { ownGuard.current = guard; publishGuard(); }, [publishGuard]);
  const registerChildGuard = useCallback((guard: UserNavigationGuard | undefined) => { childGuard.current = guard; publishGuard(); }, [publishGuard]);
  const registerArtworkState = useCallback((state: ArtworkEditorState) => { setArtworkBusy(state.busy); }, []);
  const languageValid = /^[a-z]{2,3}(-[A-Z]{2})?$/.test(language);
  const subtitleLanguages = [...new Set(languages.split(/[,\n]/).map((value) => value.trim()).filter(Boolean))];
  const languagesValid = subtitleLanguages.length > 0 && subtitleLanguages.length <= 8 && subtitleLanguages.every((value) => /^[a-z]{2}(-[A-Z]{2})?$/.test(value));
  const providerInfo = providers?.Items.find((entry) => entry.Id === provider);
  const available = providers?.Enabled && providerInfo?.Configured;
  const disabled = loading || Boolean(busy) || !detail || reviewRequired || loadError != null;
  useUserDraftNavigation(false, Boolean(busy), registerOwnGuard);
  useEffect(() => () => operation.current?.abort(), []);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setLoadError(undefined);
    void Promise.all([adminApi.getItemMetadata(itemId, { signal: controller.signal }), providersApi.getProviders({ signal: controller.signal }), providersApi.provenance(itemId, { signal: controller.signal })])
      .then(([item, availableProviders, history]) => {
        if (controller.signal.aborted) return;
        setDetail(item); setProviders(availableProviders); setProvenance(history.Items);
        const preferred = ['Audio', 'MusicAlbum', 'MusicArtist'].includes(item.Item.Type) ? 'musicbrainz' : 'tmdb';
        const initial = availableProviders.Items.find((entry) => entry.Id === preferred) ?? availableProviders.Items.find((entry) => entry.Capabilities.includes('metadata'));
        setProvider(initial?.Id ?? ''); setName(item.Effective.Name); setYear(item.Effective.ProductionYear == null ? '' : String(item.Effective.ProductionYear));
        setReviewRequired(false); setError(undefined); setMatches(undefined); setSubtitles(undefined);
      }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [itemId, revision]);

  async function run(label: string, mutation: boolean, action: (signal: AbortSignal) => Promise<void>) {
    if (operation.current || loading || (mutation && reviewRequired)) return;
    const controller = new AbortController(); operation.current = controller; setBusy(label); setError(undefined); setNotice('');
    let writeAcknowledged = false;
    try {
      await action(controller.signal);
      if (mutation && !controller.signal.aborted) {
        writeAcknowledged = true;
        const item = await adminApi.getItemMetadata(itemId, { signal: controller.signal });
        const history = await providersApi.provenance(itemId, { signal: controller.signal });
        if (!controller.signal.aborted) {
          setDetail(item); setProvenance(history.Items);
          if (tab === 'metadata') setImageSelection(undefined);
          onSaved();
        }
      }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); setNotice(''); if (mutation && (writeAcknowledged || uncertain(cause))) setReviewRequired(true); }
    } finally { if (operation.current === controller) operation.current = undefined; if (!controller.signal.aborted) setBusy(''); }
  }
  function selectProvider(value: string) {
    setProvider(value); setMatches(undefined); setError(undefined);
  }
  function findImages(match?: MetadataCandidate) {
    if (busy || artworkBusy) return;
    setImageSelection(match ? { Provider: match.Provider, Id: match.Id, Language: match.Language || language } : { Provider: provider, Id: savedProviderId(detail, provider), Language: language });
    setImagesVisited(true); setTab('images');
  }
  function close() {
    if (operation.current || artworkBusy || childGuard.current && !childGuard.current()) return;
    onClose();
  }
  function changeTab(value: SourceTab) {
    if (busy || artworkBusy || value !== tab && childGuard.current && !childGuard.current()) return;
    if (value === 'images') setImagesVisited(true);
    setTab(value);
  }
  return <Dialog open fullWidth maxWidth="md" className="online-sources-sheet" slotProps={{ paper: { sx: { width: '100%', maxWidth: 540 } } }} onClose={close} aria-labelledby="online-sources-title">
    <DialogTitle id="online-sources-title">Online sources{detail && <> · <span translate="no">{detail.Effective.Name}</span></>}</DialogTitle>
    <DialogContent aria-busy={loading || Boolean(busy)}>
      <Stack spacing={2.5} sx={{ pt: 1 }}>
        {loadError != null && <ErrorNotice error={loadError} retry={() => setRevision((value) => value + 1)} />}
        {loading && !detail && <Skeleton variant="rounded" height={280} />}
        {providers && !providers.Enabled && <Alert severity="info">Internet providers are disabled. Enable them in Settings to use online sources.</Alert>}
        {error != null && <ErrorNotice error={error} />}
        {reviewRequired && <Alert severity="warning">The change conflicted with a newer version or its result could not be confirmed. Reload this item before making another change.<Button size="small" color="inherit" startIcon={<RefreshRounded />} onClick={() => setRevision((value) => value + 1)} sx={{ display: 'block', mt: 1 }}>Reload latest item</Button></Alert>}
        {notice && <Alert severity="success">{notice}</Alert>}
        {detail && providers && <>
          {tab === 'metadata' && <Alert severity="info">Online metadata updates automatic values. Manual overrides and locked fields remain in effect. Review the selected match before applying it.</Alert>}
          <Tabs value={tab} onChange={(_event, value: SourceTab) => changeTab(value)} aria-label="Online source types" variant="scrollable" allowScrollButtonsMobile>
            <Tab value="metadata" label="Metadata" id="source-metadata-tab" aria-controls="source-metadata-panel" disabled={Boolean(busy) || artworkBusy} />
            <Tab value="images" label="Images" id="source-images-tab" aria-controls="source-images-panel" disabled={Boolean(busy) || artworkBusy} />
            <Tab value="subtitles" label="Subtitles" id="source-subtitles-tab" aria-controls="source-subtitles-panel" disabled={Boolean(busy) || artworkBusy} />
          </Tabs>
          {tab === 'metadata' && <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
            <TextField select fullWidth label="Online provider" value={provider} disabled={disabled} onChange={(event) => selectProvider(event.target.value)}>{providers.Items.filter((entry) => entry.Capabilities.includes('metadata') || entry.Capabilities.includes('images')).map((entry) => <MenuItem key={entry.Id} value={entry.Id}><span translate="no">{entry.Name}</span>{entry.Configured ? '' : ' (unavailable)'}</MenuItem>)}</TextField>
            <TextField label="Language" value={language} disabled={disabled} error={!languageValid} helperText={languageValid ? 'For example en or en-US.' : 'Enter a valid language code.'} onChange={(event) => setLanguage(event.target.value)} />
          </Stack>}
          {tab === 'metadata' && providerInfo && <Box><Typography variant="caption" color="text.secondary" translate="no">{providerInfo.Attribution}</Typography>{!providerInfo.Configured && <Typography variant="body2" color="text.secondary">This provider is unavailable. Check Online providers and deployment credentials.</Typography>}</Box>}
          {tab === 'metadata' && <Stack spacing={2} role="tabpanel" id="source-metadata-panel" aria-labelledby="source-metadata-tab">
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}><TextField fullWidth label="Search title" value={name} disabled={disabled} onChange={(event) => setName(event.target.value)} /><TextField label="Year (optional)" value={year} disabled={disabled} onChange={(event) => setYear(event.target.value)} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 4 } }} /></Stack>
            <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}>
              <Button variant="outlined" startIcon={<SearchRounded />} disabled={disabled || !available || !providerInfo?.Capabilities.includes('metadata') || !name.trim() || !languageValid || Boolean(year && !/^\d{1,4}$/.test(year))} onClick={() => void run('Searching metadata', false, async (signal) => { const result = await providersApi.search(itemId, { Provider: provider, Name: name.trim(), ...(year ? { Year: Number(year) } : {}), Language: language }, { signal }); if (!signal.aborted) setMatches(result.Items); })}>Search metadata</Button>
              <Button startIcon={<RefreshRounded />} disabled={disabled || !available || !providerInfo?.Capabilities.includes('metadata') || !languageValid} onClick={() => void run('Refreshing metadata', true, async (signal) => { await providersApi.refresh(itemId, { Provider: provider, Language: language, Revision: detail.Revision }, { signal }); if (!signal.aborted) setNotice('Metadata refreshed from the saved provider identifier.'); })}>Refresh saved match</Button>
            </Stack>
            {matches?.length === 0 && <Typography color="text.secondary">No matching metadata found. Try another title or year.</Typography>}
            {matches?.map((match) => <Paper key={`${match.Provider}:${match.Id}`} variant="outlined" sx={{ p: 2.5 }}>
              <Typography variant="h4" component="h3" translate="no">{match.Name}{match.Year ? ` (${match.Year})` : ''}</Typography>
              <Typography variant="caption" color="text.secondary">{match.Provider} · {match.Id} · {match.Type}{match.Language ? ` · ${match.Language}` : ''}</Typography>
              {match.OriginalTitle && match.OriginalTitle !== match.Name && <Typography variant="body2" color="text.secondary">Original title: <span translate="no">{match.OriginalTitle}</span></Typography>}
              {match.Overview && <Typography variant="body2" translate="no" sx={{ mt: 1, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{match.Overview}</Typography>}
              <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap', mt: 2 }}><Button variant="outlined" disabled={disabled} onClick={() => void run('Applying metadata', true, async (signal) => { await providersApi.apply(itemId, { Provider: match.Provider, Id: match.Id, Language: match.Language || language, Revision: detail.Revision }, { signal }); if (!signal.aborted) setNotice(`Metadata from ${match.Name} applied.`); })}>Apply metadata</Button>{providerInfo?.Capabilities.includes('images') && <Button disabled={disabled} onClick={() => findImages(match)}>Find images</Button>}</Stack>
            </Paper>)}
          </Stack>}
          {imagesVisited && <Box role="tabpanel" id="source-images-panel" aria-labelledby="source-images-tab" hidden={tab !== 'images'}>
            <ArtworkManagerDialog embedded readOnly={disabled} target={{ kind: 'items', id: itemId, name: detail.Effective.Name }} sourceRevision={detail.Revision} initialSource={imageSelection} onClose={() => changeTab('metadata')} onNavigationGuardChange={registerChildGuard} onStateChange={registerArtworkState} />
          </Box>}
          {tab === 'subtitles' && <Stack spacing={2} role="tabpanel" id="source-subtitles-panel" aria-labelledby="source-subtitles-tab">
            <TextField label="Subtitle languages" value={languages} disabled={disabled} onChange={(event) => setLanguages(event.target.value)} error={!languagesValid} helperText="Enter up to 8 language codes separated by commas, for example en, fr." />
            <FormControlLabel label="Hearing impaired subtitles" control={<Switch checked={hearingImpaired} disabled={disabled} onChange={(event) => setHearingImpaired(event.target.checked)} />} />
            <Button variant="outlined" startIcon={<SearchRounded />} disabled={disabled || !providers.Enabled || !providers.Items.some((entry) => entry.Configured && entry.Capabilities.includes('subtitles')) || !languagesValid} sx={{ alignSelf: 'flex-start' }} onClick={() => void run('Searching subtitles', false, async (signal) => { const result = await providersApi.searchSubtitles(itemId, { Languages: subtitleLanguages, HearingImpaired: hearingImpaired }, { signal }); if (!signal.aborted) setSubtitles(result.Items); })}>Search subtitles</Button>
            {!providers.Items.some((entry) => entry.Configured && entry.Capabilities.includes('subtitles')) && <Typography color="text.secondary">No subtitle provider is configured. Check Online providers and deployment credentials.</Typography>}
            {subtitles?.length === 0 && <Typography color="text.secondary">No matching subtitles found.</Typography>}
            {subtitles?.map((candidate) => <Paper key={`${candidate.Provider}:${candidate.Id}:${candidate.FileId}`} variant="outlined" sx={{ p: 2 }}><Typography variant="body2" translate="no" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>{candidate.Name}</Typography><Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap', my: 1 }}><Chip label={candidate.Language} size="small" />{candidate.HearingImpaired && <Chip label="Hearing impaired" size="small" />}{candidate.IsForced && <Chip label="Forced" size="small" />}{candidate.MovieHashMatch && <Chip label="File hash match" size="small" color="success" variant="outlined" />}</Stack><Typography variant="caption" color="text.secondary">{candidate.Provider} · {formatCount(candidate.DownloadCount, 'download', 'downloads')}</Typography><Box><Button startIcon={<DownloadRounded />} disabled={disabled} onClick={() => void run('Downloading subtitle', true, async (signal) => { await providersApi.downloadSubtitle(itemId, { Provider: candidate.Provider, Id: candidate.Id, FileId: candidate.FileId, Language: candidate.Language, Name: candidate.Name }, { signal }); if (!signal.aborted) setNotice(`Subtitle downloaded: ${candidate.Name}.`); })}>Download subtitle</Button></Box></Paper>)}
          </Stack>}
          {tab === 'metadata' && provenance.length > 0 && <Box component="section" aria-label="Metadata sources"><Typography variant="h4" component="h3" sx={{ mb: 1 }}>Saved metadata sources</Typography>{provenance.map((source, index) => <Box key={`${source.Provider}:${source.ProviderId}:${index}`} sx={{ py: 1 }}><Typography variant="body2">{source.Provider} · {source.ProviderId}</Typography><Typography variant="caption" color="text.secondary">{source.Fields.join(', ')} · {new Date(source.FetchedAt).toLocaleString(adminLocale())}</Typography>{providerWebsite(source.SourceUrl) && <Link href={providerWebsite(source.SourceUrl)} target="_blank" rel="noopener noreferrer" sx={{ display: 'block' }}>View source</Link>}</Box>)}</Box>}
        </>}
      </Stack>
    </DialogContent>
    <DialogActions sx={{ px: 3, py: 2, gap: 1 }}>{busy && <Stack role="status" direction="row" spacing={1} sx={{ mr: 'auto', alignItems: 'center' }}><CircularProgress size={16} /><Typography variant="caption">{busy}...</Typography></Stack>}<Button type="button" disabled={Boolean(busy) || artworkBusy} onClick={close}>Close</Button></DialogActions>
  </Dialog>;
}
