import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, Link, MenuItem, Paper, Skeleton, Stack, Switch, Tab, Tabs, TextField, Typography } from '@mui/material';
import DownloadRounded from '@mui/icons-material/DownloadRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SearchRounded from '@mui/icons-material/SearchRounded';
import { adminApi, ApiError, isAbortError } from './api';
import type { MetadataDetail } from './api';
import { ErrorNotice } from './components';
import { providersApi, providerWebsite, providerImagePreview } from './providersApi';
import type { ImageCandidate, MetadataCandidate, OnlineProviders, ProviderProvenance, SubtitleCandidate } from './providersApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

type SourceTab = 'metadata' | 'images' | 'subtitles';
function uncertain(error: unknown) { return !(error instanceof ApiError) || error.code === 'revision_conflict' || error.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(error.code); }
function savedProviderId(detail: MetadataDetail | undefined, provider: string) {
  return Object.entries(detail?.Effective.ProviderIds ?? {}).find(([key]) => key.toLowerCase() === provider.toLowerCase())?.[1] ?? '';
}

export function OnlineSourcesDialog({ itemId, onClose, onSaved, onNavigationGuardChange }: { itemId: string; onClose: () => void; onSaved: () => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [detail, setDetail] = useState<MetadataDetail>();
  const [providers, setProviders] = useState<OnlineProviders>();
  const [provenance, setProvenance] = useState<ProviderProvenance[]>([]);
  const [tab, setTab] = useState<SourceTab>('metadata');
  const [provider, setProvider] = useState('');
  const [name, setName] = useState('');
  const [year, setYear] = useState('');
  const [language, setLanguage] = useState('en');
  const [externalId, setExternalId] = useState('');
  const [imageIndex, setImageIndex] = useState('0');
  const [languages, setLanguages] = useState('en');
  const [hearingImpaired, setHearingImpaired] = useState(false);
  const [matches, setMatches] = useState<MetadataCandidate[]>();
  const [images, setImages] = useState<ImageCandidate[]>();
  const [subtitles, setSubtitles] = useState<SubtitleCandidate[]>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState('');
  const [notice, setNotice] = useState('');
  const [reviewRequired, setReviewRequired] = useState(false);
  const [revision, setRevision] = useState(0);
  const operation = useRef<AbortController | undefined>(undefined);
  const languageValid = /^[a-z]{2,3}(-[A-Z]{2})?$/.test(language);
  const subtitleLanguages = [...new Set(languages.split(/[,\n]/).map((value) => value.trim()).filter(Boolean))];
  const languagesValid = subtitleLanguages.length > 0 && subtitleLanguages.length <= 8 && subtitleLanguages.every((value) => /^[a-z]{2}(-[A-Z]{2})?$/.test(value));
  const providerInfo = providers?.Items.find((entry) => entry.Id === provider);
  const available = providers?.Enabled && providerInfo?.Configured;
  const disabled = loading || Boolean(busy) || !detail || reviewRequired || loadError != null;
  useUserDraftNavigation(false, Boolean(busy), onNavigationGuardChange);
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
        setExternalId(savedProviderId(item, initial?.Id ?? ''));
        setReviewRequired(false); setError(undefined); setMatches(undefined); setImages(undefined); setSubtitles(undefined);
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
        if (!controller.signal.aborted) { setDetail(item); setProvenance(history.Items); onSaved(); }
      }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); setNotice(''); if (mutation && (writeAcknowledged || uncertain(cause))) setReviewRequired(true); }
    } finally { if (operation.current === controller) operation.current = undefined; if (!controller.signal.aborted) setBusy(''); }
  }
  function selectProvider(value: string) {
    setProvider(value); setExternalId(savedProviderId(detail, value)); setMatches(undefined); setImages(undefined); setError(undefined);
  }
  function findImages(match?: MetadataCandidate) {
    const target = match ? { Provider: match.Provider, Id: match.Id, Language: match.Language || language } : { Provider: provider, Id: externalId.trim(), Language: language };
    setTab('images'); setExternalId(target.Id); setProvider(target.Provider);
    void run('Searching images', false, async (signal) => { const result = await providersApi.images(itemId, target, { signal }); if (!signal.aborted) setImages(result.Items); });
  }
  return <Dialog open fullWidth maxWidth="md" onClose={busy ? undefined : onClose} aria-labelledby="online-sources-title">
    <DialogTitle id="online-sources-title">Online sources{detail ? ` · ${detail.Effective.Name}` : ''}</DialogTitle>
    <DialogContent aria-busy={loading || Boolean(busy)}>
      <Stack spacing={2.5} sx={{ pt: 1 }}>
        {loadError != null && <ErrorNotice error={loadError} retry={() => setRevision((value) => value + 1)} />}
        {loading && !detail && <Skeleton variant="rounded" height={280} />}
        {providers && !providers.Enabled && <Alert severity="info">Internet providers are disabled. Enable them in Settings to use online sources.</Alert>}
        {error != null && <ErrorNotice error={error} />}
        {reviewRequired && <Alert severity="warning">The change conflicted with a newer version or its result could not be confirmed. Reload this item before making another change.<Button size="small" color="inherit" startIcon={<RefreshRounded />} onClick={() => setRevision((value) => value + 1)} sx={{ display: 'block', mt: 1 }}>Reload latest item</Button></Alert>}
        {notice && <Alert severity="success">{notice}</Alert>}
        {detail && providers && <>
          <Alert severity="info">Online metadata updates automatic values. Manual overrides and locked fields remain in effect. Review the selected match before applying it.</Alert>
          <Tabs value={tab} onChange={(_event, value: SourceTab) => { if (!busy) setTab(value); }} aria-label="Online source types" variant="scrollable" allowScrollButtonsMobile>
            <Tab value="metadata" label="Metadata" id="source-metadata-tab" aria-controls="source-metadata-panel" disabled={Boolean(busy)} />
            <Tab value="images" label="Images" id="source-images-tab" aria-controls="source-images-panel" disabled={Boolean(busy)} />
            <Tab value="subtitles" label="Subtitles" id="source-subtitles-tab" aria-controls="source-subtitles-panel" disabled={Boolean(busy)} />
          </Tabs>
          {tab !== 'subtitles' && <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
            <TextField select fullWidth label="Online provider" value={provider} disabled={disabled} onChange={(event) => selectProvider(event.target.value)}>{providers.Items.filter((entry) => entry.Capabilities.includes('metadata') || entry.Capabilities.includes('images')).map((entry) => <MenuItem key={entry.Id} value={entry.Id}>{entry.Name}{entry.Configured ? '' : ' (unavailable)'}</MenuItem>)}</TextField>
            <TextField label="Language" value={language} disabled={disabled} error={!languageValid} helperText={languageValid ? 'For example en or en-US.' : 'Enter a valid language code.'} onChange={(event) => setLanguage(event.target.value)} />
          </Stack>}
          {tab !== 'subtitles' && providerInfo && <Box><Typography variant="caption" color="text.secondary">{providerInfo.Attribution}</Typography>{!providerInfo.Configured && <Typography variant="body2" color="text.secondary">This provider is unavailable. Check Online providers and deployment credentials.</Typography>}</Box>}
          {tab === 'metadata' && <Stack spacing={2} role="tabpanel" id="source-metadata-panel" aria-labelledby="source-metadata-tab">
            <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}><TextField fullWidth label="Search title" value={name} disabled={disabled} onChange={(event) => setName(event.target.value)} /><TextField label="Year (optional)" value={year} disabled={disabled} onChange={(event) => setYear(event.target.value)} slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 4 } }} /></Stack>
            <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}>
              <Button variant="outlined" startIcon={<SearchRounded />} disabled={disabled || !available || !providerInfo?.Capabilities.includes('metadata') || !name.trim() || !languageValid || Boolean(year && !/^\d{1,4}$/.test(year))} onClick={() => void run('Searching metadata', false, async (signal) => { const result = await providersApi.search(itemId, { Provider: provider, Name: name.trim(), ...(year ? { Year: Number(year) } : {}), Language: language }, { signal }); if (!signal.aborted) setMatches(result.Items); })}>Search metadata</Button>
              <Button startIcon={<RefreshRounded />} disabled={disabled || !available || !providerInfo?.Capabilities.includes('metadata') || !languageValid} onClick={() => void run('Refreshing metadata', true, async (signal) => { await providersApi.refresh(itemId, { Provider: provider, Language: language, Revision: detail.Revision }, { signal }); if (!signal.aborted) setNotice('Metadata refreshed from the saved provider identifier.'); })}>Refresh saved match</Button>
            </Stack>
            {matches?.length === 0 && <Typography color="text.secondary">No matching metadata found. Try another title or year.</Typography>}
            {matches?.map((match) => <Paper key={`${match.Provider}:${match.Id}`} variant="outlined" sx={{ p: 2.5 }}>
              <Typography variant="h4" component="h3">{match.Name}{match.Year ? ` (${match.Year})` : ''}</Typography>
              <Typography variant="caption" color="text.secondary">{match.Provider} · {match.Id} · {match.Type}{match.Language ? ` · ${match.Language}` : ''}</Typography>
              {match.OriginalTitle && match.OriginalTitle !== match.Name && <Typography variant="body2" color="text.secondary">Original title: {match.OriginalTitle}</Typography>}
              {match.Overview && <Typography variant="body2" sx={{ mt: 1, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{match.Overview}</Typography>}
              <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap', mt: 2 }}><Button variant="outlined" disabled={disabled} onClick={() => void run('Applying metadata', true, async (signal) => { await providersApi.apply(itemId, { Provider: match.Provider, Id: match.Id, Language: match.Language || language, Revision: detail.Revision }, { signal }); if (!signal.aborted) { setExternalId(match.Id); setNotice(`Metadata from ${match.Name} applied.`); } })}>Apply metadata</Button>{providerInfo?.Capabilities.includes('images') && <Button disabled={disabled} onClick={() => findImages(match)}>Find images</Button>}</Stack>
            </Paper>)}
          </Stack>}
          {tab === 'images' && <Stack spacing={2} role="tabpanel" id="source-images-panel" aria-labelledby="source-images-tab">
            <TextField label="Provider item identifier" value={externalId} disabled={disabled} onChange={(event) => { setExternalId(event.target.value); setImages(undefined); }} helperText="Search for a metadata match first, or enter the identifier from the provider." />
            <Button variant="outlined" startIcon={<SearchRounded />} disabled={disabled || !available || !providerInfo?.Capabilities.includes('images') || !externalId.trim() || !languageValid} onClick={() => findImages()} sx={{ alignSelf: 'flex-start' }}>Search images</Button>
            <TextField type="number" label="Image index" value={imageIndex} disabled={disabled} onChange={(event) => setImageIndex(event.target.value)} helperText="Use 0 for the primary image or the first backdrop. Saving replaces the selected image type and index." slotProps={{ htmlInput: { min: 0, step: 1 } }} />
            {images?.length === 0 && <Typography color="text.secondary">No images found for this provider identifier.</Typography>}
            <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>{images?.map((candidate) => <Paper key={`${candidate.ImageType}:${candidate.ImageId}`} variant="outlined" sx={{ p: 2 }}>
              {providerImagePreview(candidate.PreviewUrl) && <Box component="img" src={providerImagePreview(candidate.PreviewUrl)} alt={`${candidate.ImageType} candidate`} loading="lazy" referrerPolicy="no-referrer" sx={{ width: '100%', height: 220, objectFit: 'contain', bgcolor: 'background.default', borderRadius: 1 }} />}
              {!providerImagePreview(candidate.PreviewUrl) && providerWebsite(candidate.PreviewUrl) && <Link href={providerWebsite(candidate.PreviewUrl)} target="_blank" rel="noopener noreferrer">Preview image on provider website</Link>}
              <Typography variant="body2" sx={{ mt: 1 }}>{candidate.ImageType} · {candidate.Width} × {candidate.Height}{candidate.Language ? ` · ${candidate.Language}` : ''}</Typography>
              <Button disabled={disabled || !/^\d+$/.test(imageIndex) || !Number.isSafeInteger(Number(imageIndex))} sx={{ mt: 1 }} onClick={() => void run('Saving image', true, async (signal) => { await providersApi.applyImage(itemId, { Provider: candidate.Provider, Id: candidate.Id, Language: candidate.Language || language, ImageId: candidate.ImageId, ImageType: candidate.ImageType, ImageIndex: Number(imageIndex), Revision: detail.Revision }, { signal }); if (!signal.aborted) setNotice(`${candidate.ImageType} image saved.`); })}>Use image</Button>
            </Paper>)}</Box>
          </Stack>}
          {tab === 'subtitles' && <Stack spacing={2} role="tabpanel" id="source-subtitles-panel" aria-labelledby="source-subtitles-tab">
            <TextField label="Subtitle languages" value={languages} disabled={disabled} onChange={(event) => setLanguages(event.target.value)} error={!languagesValid} helperText="Enter up to 8 language codes separated by commas, for example en, fr." />
            <FormControlLabel label="Hearing impaired subtitles" control={<Switch checked={hearingImpaired} disabled={disabled} onChange={(event) => setHearingImpaired(event.target.checked)} />} />
            <Button variant="outlined" startIcon={<SearchRounded />} disabled={disabled || !providers.Enabled || !providers.Items.some((entry) => entry.Configured && entry.Capabilities.includes('subtitles')) || !languagesValid} sx={{ alignSelf: 'flex-start' }} onClick={() => void run('Searching subtitles', false, async (signal) => { const result = await providersApi.searchSubtitles(itemId, { Languages: subtitleLanguages, HearingImpaired: hearingImpaired }, { signal }); if (!signal.aborted) setSubtitles(result.Items); })}>Search subtitles</Button>
            {!providers.Items.some((entry) => entry.Configured && entry.Capabilities.includes('subtitles')) && <Typography color="text.secondary">No subtitle provider is configured. Check Online providers and deployment credentials.</Typography>}
            {subtitles?.length === 0 && <Typography color="text.secondary">No matching subtitles found.</Typography>}
            {subtitles?.map((candidate) => <Paper key={`${candidate.Provider}:${candidate.Id}:${candidate.FileId}`} variant="outlined" sx={{ p: 2 }}><Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>{candidate.Name}</Typography><Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap', my: 1 }}><Chip label={candidate.Language} size="small" />{candidate.HearingImpaired && <Chip label="Hearing impaired" size="small" />}{candidate.IsForced && <Chip label="Forced" size="small" />}{candidate.MovieHashMatch && <Chip label="File hash match" size="small" color="success" variant="outlined" />}</Stack><Typography variant="caption" color="text.secondary">{candidate.Provider} · {candidate.DownloadCount.toLocaleString()} downloads</Typography><Box><Button startIcon={<DownloadRounded />} disabled={disabled} onClick={() => void run('Downloading subtitle', true, async (signal) => { await providersApi.downloadSubtitle(itemId, { Provider: candidate.Provider, Id: candidate.Id, FileId: candidate.FileId, Language: candidate.Language, Name: candidate.Name }, { signal }); if (!signal.aborted) setNotice(`Subtitle downloaded: ${candidate.Name}.`); })}>Download subtitle</Button></Box></Paper>)}
          </Stack>}
          {provenance.length > 0 && <Box component="section" aria-label="Metadata sources"><Typography variant="h4" component="h3" sx={{ mb: 1 }}>Saved metadata sources</Typography>{provenance.map((source, index) => <Box key={`${source.Provider}:${source.ProviderId}:${index}`} sx={{ py: 1 }}><Typography variant="body2">{source.Provider} · {source.ProviderId}</Typography><Typography variant="caption" color="text.secondary">{source.Fields.join(', ')} · {new Date(source.FetchedAt).toLocaleString()}</Typography>{providerWebsite(source.SourceUrl) && <Link href={providerWebsite(source.SourceUrl)} target="_blank" rel="noopener noreferrer" sx={{ display: 'block' }}>View source</Link>}</Box>)}</Box>}
        </>}
      </Stack>
    </DialogContent>
    <DialogActions sx={{ px: 3, py: 2, gap: 1 }}>{busy && <Stack role="status" direction="row" spacing={1} sx={{ mr: 'auto', alignItems: 'center' }}><CircularProgress size={16} /><Typography variant="caption">{busy}...</Typography></Stack>}<Button disabled={Boolean(busy)} onClick={onClose}>Close</Button></DialogActions>
  </Dialog>;
}
