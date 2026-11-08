import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, MenuItem, Stack, TextField, Typography } from './ui';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { confirmAction, useTranslation } from './i18n';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { seriesOrderApi } from './seriesOrderApi';
import type { SeriesOrder, SeriesOrderDetail, SeriesOrderEntry, SeriesOrderGroup, SeriesOrderPreview } from './seriesOrderApi';
import './series-display-order.css';

export interface SeriesOrderDraftState { dirty: boolean; busy: boolean; reviewRequired: boolean }
const noGuard: UserNavigationGuardChange = () => undefined;
const noState = (_state: SeriesOrderDraftState) => undefined;
const orderLabels: Record<SeriesOrder, string> = { Aired: 'Aired order', Dvd: 'DVD order', Absolute: 'Absolute order' };

function OrderPreview({ entries }: { entries: SeriesOrderEntry[] }) {
  const { t, locale } = useTranslation();
  const [visible, setVisible] = useState(25);
  useEffect(() => setVisible(25), [entries]);
  return <div className="series-order-preview">
    <table><caption>{t('Episode order preview')}</caption><thead><tr><th scope="col">{t('Position')}</th><th scope="col">{t('Provider group')}</th><th scope="col">{t('Aired episode')}</th></tr></thead><tbody>
      {entries.slice(0, visible).map((entry) => <tr key={entry.ProviderEpisodeId}><td>{(entry.Ordinal + 1).toLocaleString(locale)}</td><td translate="no">{entry.GroupName || '—'}</td><td translate="no">S{String(entry.SeasonNumber).padStart(2, '0')}E{String(entry.EpisodeNumber).padStart(2, '0')}</td></tr>)}
    </tbody></table>
    {visible < entries.length && <Button type="button" size="small" onClick={() => setVisible((value) => value + 25)}>{t('Show more episodes')}</Button>}
  </div>;
}

export function SeriesDisplayOrderFields({ seriesId, disabled = false, onNavigationGuardChange = noGuard, onStateChange = noState }: {
  seriesId: string; disabled?: boolean; onNavigationGuardChange?: UserNavigationGuardChange; onStateChange?: (state: SeriesOrderDraftState) => void;
}) {
  const { t, locale } = useTranslation();
  const [saved, setSaved] = useState<SeriesOrderDetail>();
  const [selected, setSelected] = useState<SeriesOrder>('Aired');
  const [groupId, setGroupId] = useState('');
  const [groups, setGroups] = useState<SeriesOrderGroup[]>();
  const [preview, setPreview] = useState<SeriesOrderPreview>();
  const [loading, setLoading] = useState(true);
  const [loadingGroups, setLoadingGroups] = useState(false);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [reviewRequired, setReviewRequired] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState('');
  const [reloadRevision, setReloadRevision] = useState(0);
  const inFlight = useRef<AbortController | undefined>(undefined);
  const groupRequest = useRef<AbortController | undefined>(undefined);
  const previewRequest = useRef<AbortController | undefined>(undefined);
  const mounted = useRef(true);
  const busy = saving || previewing || loadingGroups;
  const dirty = Boolean(saved && (selected !== saved.DisplayOrder || groupId !== saved.GroupId
    || preview && preview.MappingSHA256 !== saved.MappingSHA256));
  const locked = disabled || loading || busy || reviewRequired;
  const restore = useCallback(() => {
    if (!saved) return;
    setSelected(saved.DisplayOrder); setGroupId(saved.GroupId); setPreview(undefined); setNotice('');
  }, [saved]);

  useEffect(() => {
    onStateChange({ dirty, busy, reviewRequired });
    return () => onStateChange({ dirty: false, busy: false, reviewRequired: false });
  }, [dirty, busy, reviewRequired, onStateChange]);
  useEffect(() => {
    if (!dirty && !busy) { onNavigationGuardChange(undefined); return; }
    const guard = () => {
      if (busy) return false;
      if (dirty && !confirmAction(t('Discard unsaved episode order changes?'))) return false;
      restore(); return true;
    };
    const beforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ''; };
    onNavigationGuardChange(guard); window.addEventListener('beforeunload', beforeUnload);
    return () => { onNavigationGuardChange(undefined); window.removeEventListener('beforeunload', beforeUnload); };
  }, [dirty, busy, restore, onNavigationGuardChange, t]);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; inFlight.current?.abort(); groupRequest.current?.abort(); previewRequest.current?.abort(); };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setGroups(undefined); setPreview(undefined); setNotice('');
    void seriesOrderApi.get(seriesId, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      setSaved(result); setSelected(result.DisplayOrder); setGroupId(result.GroupId); setReviewRequired(false);
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [seriesId, reloadRevision]);

  function reload() {
    if (busy || dirty && !confirmAction(t('Discard unsaved episode order changes?'))) return;
    setReloadRevision((value) => value + 1);
  }
  async function loadGroups() {
    if (locked || !saved?.ProviderSeriesId) return;
    const controller = new AbortController(); groupRequest.current = controller;
    setLoadingGroups(true); setError(null); setNotice('');
    try { const result = await seriesOrderApi.groups(seriesId, { signal: controller.signal }); if (!controller.signal.aborted) setGroups(result); }
    catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }
    finally { if (groupRequest.current === controller) groupRequest.current = undefined; if (mounted.current && !controller.signal.aborted) setLoadingGroups(false); }
  }
  async function selectGroup(value: string) {
    if (locked || !saved) return;
    setGroupId(value); setPreview(undefined); setError(null); setNotice('');
    if (!value) return;
    const controller = new AbortController(); previewRequest.current = controller;
    setPreviewing(true);
    try {
      const result = await seriesOrderApi.preview(seriesId, value, { signal: controller.signal });
      if (!controller.signal.aborted) {
        if (result.Revision !== saved.Revision || result.DisplayOrder !== selected) { setReviewRequired(true); setError(new ApiError('The series order changed. Reload before editing.', { code: 'revision_conflict', status: 409 })); }
        else setPreview(result);
      }
    } catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }
    finally { if (previewRequest.current === controller) previewRequest.current = undefined; if (mounted.current && !controller.signal.aborted) setPreviewing(false); }
  }
  async function save() {
    if (locked || !saved || !dirty || selected !== 'Aired' && (!preview || preview.Coverage.UnmappedCount !== 0)) return;
    const controller = new AbortController(); inFlight.current = controller;
    setSaving(true); setError(null); setNotice('');
    try {
      const result = await seriesOrderApi.update(seriesId, { Revision: saved.Revision, DisplayOrder: selected,
        GroupId: selected === 'Aired' ? '' : groupId, MappingSHA256: selected === 'Aired' ? '' : preview!.MappingSHA256 }, { signal: controller.signal });
      if (controller.signal.aborted) return;
      setSaved(result); setSelected(result.DisplayOrder); setGroupId(result.GroupId); setPreview(undefined); setReviewRequired(false);
      setNotice('Episode order saved. Playback queues now use this order.');
    } catch (cause) {
      if (controller.signal.aborted) return;
      setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response' || cause.code === 'network_error') setReviewRequired(true);
    } finally { if (inFlight.current === controller) inFlight.current = undefined; if (mounted.current && !controller.signal.aborted) setSaving(false); }
  }
  const availableGroups = groups?.filter((group) => group.DisplayOrder === selected);
  const coverage = preview?.Coverage ?? saved?.Coverage;
  const shownEntries = preview?.Entries ?? (selected === saved?.DisplayOrder && groupId === saved.GroupId ? saved.Entries : undefined);
  return <section className="series-display-order" aria-labelledby={'series-order-heading-' + seriesId} aria-busy={loading || busy}>
    <div className="series-order-heading"><h3 id={'series-order-heading-' + seriesId}>{t('Episode display order')}</h3><span>{saved ? t(orderLabels[saved.DisplayOrder]) : t('Loading…')}</span></div>
    <p>{t('Choose a provider-defined order for episode browsing, Next up, and playback queues. Media files and watched progress keep their identities.')}</p>
    {loading && <Typography role="status">{t('Loading episode order…')}</Typography>}
    {error != null && <ErrorNotice error={error} retry={!saved ? reload : undefined} />}
    {reviewRequired && <Alert severity="warning">{t('The order changed or the save result could not be confirmed. Reload and review the saved order before saving again.')}</Alert>}
    {notice && <Alert severity="success">{t(notice)}</Alert>}
    {saved && <Stack spacing={1.5}>
      {disabled && <Typography variant="caption" color="text.secondary">{t('Save or discard the metadata draft before changing episode order.')}</Typography>}
      {!saved.SourceMatches && <Alert severity="warning">{t('The series provider identifier changed. Preview a group for the current series before using an alternate order.')}</Alert>}
      {saved.DisplayOrder !== 'Aired' && saved.Coverage.UnmappedCount > 0 && <Alert severity="warning">{t('Some newly scanned or expected episodes are not mapped. They appear after the mapped episodes until a complete group is selected.')}</Alert>}
      <TextField select fullWidth label="Episode display order" value={selected} disabled={locked} onChange={(event) => {
        const next = event.target.value as SeriesOrder; setSelected(next); setGroupId(next === saved.DisplayOrder ? saved.GroupId : ''); setPreview(undefined); setNotice(''); setError(null);
      }}>{(['Aired', 'Dvd', 'Absolute'] as const).map((value) => <MenuItem key={value} value={value}>{t(orderLabels[value])}</MenuItem>)}</TextField>
      {selected !== 'Aired' && <>
        {!saved.ProviderSeriesId && <Alert severity="info">{t('Set the series TMDB identifier and enable the provider before loading alternate orders.')}</Alert>}
        <div className="series-order-group-action"><Button type="button" size="small" onClick={() => void loadGroups()} disabled={locked || !saved.ProviderSeriesId}>{t(loadingGroups ? 'Loading groups…' : groups ? 'Refresh provider groups' : 'Load provider groups')}</Button>{saved.ProviderSeriesId && <span translate="no">TMDB {saved.ProviderSeriesId}</span>}</div>
        {groups && <TextField select fullWidth label="Provider group" value={groupId} disabled={locked} onChange={(event) => void selectGroup(event.target.value)}>
          <MenuItem value="">{t('Choose a provider group')}</MenuItem>
          {groupId && !availableGroups?.some((group) => group.Id === groupId) && <MenuItem value={groupId} disabled><span translate="no">{saved.GroupName || groupId}</span></MenuItem>}
          {availableGroups?.map((group) => <MenuItem key={group.Id} value={group.Id}><span translate="no">{group.Name}</span> · {group.EpisodeCount.toLocaleString(locale)}</MenuItem>)}
        </TextField>}
        {groupId && <Box><Button type="button" size="small" onClick={() => void selectGroup(groupId)} disabled={locked}>{t(preview ? 'Refresh group preview' : 'Preview selected group')}</Button></Box>}
        {groups && availableGroups?.length === 0 && <Alert severity="info">{t('This provider has no group for the selected order. Keep aired order or choose another available order.')}</Alert>}
        {previewing && <Typography role="status">{t('Checking the selected group…')}</Typography>}
        {preview && <Typography variant="caption" color="text.secondary">{t('Mapped episodes')}: {preview.Entries.length.toLocaleString(locale)} · {t('Unmapped episodes')}: {preview.Coverage.UnmappedCount.toLocaleString(locale)}</Typography>}
        {preview && coverage && coverage.UnmappedCount > 0 && <Alert severity="warning"><Stack spacing={0.5}><span>{t('This group is incomplete for the catalog or expected episode list. Choose another group before saving.')}</span>{coverage.Unmapped.slice(0, 5).map((entry) => <Typography key={entry.Kind + entry.Id} variant="caption" translate="no">{entry.Name || entry.Id} · S{entry.SeasonNumber ?? '?'}E{entry.EpisodeNumber ?? '?'}</Typography>)}</Stack></Alert>}
        {shownEntries && shownEntries.length > 0 && <OrderPreview entries={shownEntries} />}
      </>}
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, justifyContent: 'flex-end' }}>
        <Button type="button" size="small" color="secondary" onClick={reload} disabled={loading || busy}>{t('Reload order')}</Button>
        {dirty && !reviewRequired && <Button type="button" size="small" color="secondary" onClick={restore} disabled={busy}>{t('Discard order changes')}</Button>}
        <Button type="button" size="small" variant="contained" onClick={() => void save()} disabled={locked || !dirty || selected !== 'Aired' && (!preview || preview.Coverage.UnmappedCount !== 0)}>{t(saving ? 'Saving order…' : 'Save episode order')}</Button>
      </Box>
    </Stack>}
  </section>;
}
