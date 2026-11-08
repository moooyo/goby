import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent, KeyboardEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, IconButton, LinearProgress, Skeleton, Stack, TablePagination, Tooltip } from './ui';
import { ArrowBackRounded } from './icons';
import { EditOutlined } from './icons';
import { FilterAltOffOutlined } from './icons';
import { LockOutlined } from './icons';
import { RefreshRounded } from './icons';
import { MoreVertRounded } from './icons';
import { VideoLibraryOutlined } from './icons';
import { adminApi, isAbortError } from './api';
import type { Job, JobsResponse, Library, MetadataItemSummary } from './api';
import { mediaDuration, mediaSummaryApi } from './mediaSummaryApi';
import type { MediaMetadataItem, MediaMetadataItemsResponse } from './mediaSummaryApi';
import { MediaCatalogAnalysis, MediaCatalogImages, MediaCatalogTechnical } from './MediaTechnicalDetails';
import { ErrorNotice } from './components';
import { MetadataEditorDialog } from './MetadataEditorDialog';
import { EpisodeRosterDialog } from './EpisodeRosterDialog';
import { OnlineSourcesDialog } from './OnlineSourcesDialog';
import { CreditsEditorDialog } from './CreditsEditorDialog';
import { BackgroundPreviewEditorDialog } from './BackgroundPreviewEditorDialog';
import { AudioWaveformDialog } from './AudioWaveformDialog';
import { SubtitleTimelineDialog } from './SubtitleTimelineDialog';
import { LibraryEditorDialog } from './LibraryEditorDialog';
import { RootBindingDialog } from './RootBindingDialog';
import { LibraryArtwork } from './LibraryArtwork';
import { LibraryDirectoriesPanel } from './LibraryDirectoriesPanel';
import type { ImageType } from './artworkApi';
import { useTaskResource } from './useTaskResource';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuard, UserNavigationGuardChange } from './userDraftNavigation';
import { mediaFeatures, MediaSearchField } from './MediaPagePrimitives';
import { useTranslation } from './i18n';
import './media-pages.css';

const itemTypes = [
  { value: 'Movie', label: 'Movie' }, { value: 'Video', label: 'Video' },
  { value: 'Series', label: 'Series' }, { value: 'Season', label: 'Season' },
  { value: 'Episode', label: 'Episode' }, { value: 'Folder', label: 'Folder' },
  { value: 'Audio', label: 'Audio' }, { value: 'MusicAlbum', label: 'Music album' },
  { value: 'MusicArtist', label: 'Music artist' },
] as const;
type ItemType = (typeof itemTypes)[number]['value'];
type LibraryTab = 'items' | 'images' | 'directories' | 'features' | 'history';
function navigateTypeFilter(event: KeyboardEvent<HTMLDetailsElement>) {
  const filter = event.currentTarget;
  if (event.key === 'Escape' && filter.open) {
    event.preventDefault(); event.stopPropagation(); filter.open = false;
    filter.querySelector('summary')?.focus(); return;
  }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
  const choices = Array.from(filter.querySelectorAll<HTMLInputElement>('input[type="checkbox"]:not(:disabled)'));
  if (choices.length === 0) return;
  event.preventDefault(); filter.open = true;
  const current = choices.indexOf(document.activeElement as HTMLInputElement);
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? choices.length - 1
    : event.key === 'ArrowDown' ? (current + 1) % choices.length
    : current < 0 ? choices.length - 1 : (current + choices.length - 1) % choices.length;
  choices[next]?.focus();
}

const libraryTabs: { id: LibraryTab; label: string }[] = [
  { id: 'items', label: 'Items' }, { id: 'images', label: 'Images' },
  { id: 'directories', label: 'Media directories' }, { id: 'features', label: 'Features and analysis' },
  { id: 'history', label: 'Scan history' },
];
const itemTypeName = (value: string) => itemTypes.find((type) => type.value === value)?.label ?? value;
function itemPosition(item: MetadataItemSummary): string {
  if (item.Type === 'Episode') return [item.ParentIndexNumber == null ? '' : `S${String(item.ParentIndexNumber).padStart(2, '0')}`, item.IndexNumber == null ? '' : `E${String(item.IndexNumber).padStart(2, '0')}`].join('');
  if (item.Type === 'Season' && item.IndexNumber != null) return `S${String(item.IndexNumber).padStart(2, '0')}`;
  if (item.Type === 'Audio') return [item.ParentIndexNumber == null ? '' : String(item.ParentIndexNumber), item.IndexNumber == null ? '' : String(item.IndexNumber)].filter(Boolean).join(' / ');
  return '';
}
function ItemIdentity({ item }: { item: MediaMetadataItem }) {
  const { t } = useTranslation();
  const context = [item.ParentName, itemPosition(item)].filter(Boolean).join(' · ');
  return <div className="media-item-identity"><span className="media-item-tile" aria-hidden="true"><VideoLibraryOutlined sx={{ fontSize: 15 }} /></span><div className="media-truncate"><span className="media-strong" translate="no">{item.Name || t('Untitled item')}</span><span className="media-secondary-line" title={item.Path}>{t(itemTypeName(item.Type))}{item.ProductionYear != null && <> · <span className="media-mono">{item.ProductionYear}</span></>}{(context || item.Path) && <span translate="no"> · {context || item.Path}</span>}</span><MediaCatalogTechnical summary={item.Media} compact /></div></div>;
}
function MetadataStatus({ item }: { item: MetadataItemSummary }) {
  const { t } = useTranslation();
  return <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 0.75 }}><Chip size="small" color={item.HasOverrides ? 'info' : 'default'} label={item.HasOverrides ? 'Edited' : 'Automatic'} />{item.LockedFieldCount > 0 && <Chip size="small" icon={<LockOutlined />} label={String(item.LockedFieldCount)} title={t('Locked fields')} />}</Stack>;
}

const activeScan = (job: Job) => ['pending', 'queued', 'running'].includes(job.Status.toLowerCase());
function LibraryScanHistory({ libraryId, onTasks }: { libraryId: string; onTasks?: () => void }) {
  const { t, locale } = useTranslation();
  const load = useCallback((signal: AbortSignal) => adminApi.getJobs({ signal }), []);
  const poll = useCallback((value: JobsResponse) => value.Items.some((job) => job.LibraryId === libraryId && activeScan(job)), [libraryId]);
  const resource = useTaskResource({ key: libraryId, load, poll });
  const jobs = resource.data?.Items.filter((job) => job.LibraryId === libraryId) ?? [];
  return <section className="media-panel" aria-labelledby="library-scan-history-title"><div className="media-panel-heading"><h2 id="library-scan-history-title">{t('Scan history')}</h2><Button size="small" onClick={resource.reload} disabled={resource.loading}>Refresh</Button>{onTasks && <Button size="small" onClick={onTasks}>View tasks</Button>}</div>
    {resource.error != null && <div className="media-panel-body"><ErrorNotice error={resource.error} retry={resource.reload} /></div>}
    {resource.loading && !resource.data && <Skeleton height={100} />}
    {resource.data && jobs.length === 0 && <p className="media-panel-body media-muted">{t('No scan records for this library.')}</p>}
    {jobs.map((job) => <div key={job.Id} className="media-history-row"><div><strong>{t(job.ForceProbe ? 'Media details refresh' : 'Library scan')}</strong><span className="media-secondary-line media-mono">{new Date(job.StartedAt ?? job.CreatedAt).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })}</span></div><Chip size="small" color={job.Status === 'failed' ? 'error' : activeScan(job) ? 'info' : 'default'} label={job.Status.charAt(0).toUpperCase() + job.Status.slice(1)} /><span className="media-mono media-muted">{t('Scanned')} {new Intl.NumberFormat(locale).format(job.Scanned)} · {t('Added')} {new Intl.NumberFormat(locale).format(job.Added)} · {t('Updated')} {new Intl.NumberFormat(locale).format(job.Updated)}</span>{job.Error && <span className="media-error">{job.Error}</span>}</div>)}
  </section>;
}

interface MetadataItemsPageProps { libraryId: string; currentUserId: string; onLibraries: () => void; onTasks?: () => void; onNavigationGuardChange: UserNavigationGuardChange }
function LibraryItemsView({ libraryId, currentUserId, onLibraries, onTasks, onNavigationGuardChange }: MetadataItemsPageProps) {
  const { t, locale, formatCount } = useTranslation();
  const [searchTitle, setSearchTitle] = useState('');
  const [query, setQuery] = useState({ searchTerm: '', types: [] as ItemType[], page: 0, pageSize: 50 });
  const [loaded, setLoaded] = useState<{ queryKey: string; result: MediaMetadataItemsResponse }>();
  const [failure, setFailure] = useState<{ queryKey: string; cause: unknown }>();
  const [pending, setPending] = useState(true); const [revision, setRevision] = useState(0);
  const [library, setLibrary] = useState<Library>(); const [libraryError, setLibraryError] = useState<unknown>();
  const [tab, setTab] = useState<LibraryTab>('items'); const [selected, setSelected] = useState<string[]>([]);
  const [editingItemId, setEditingItemId] = useState<string>(); const [sourcesItemId, setSourcesItemId] = useState<string>();
  const [editorTab, setEditorTab] = useState(0);
  const [editorImageType, setEditorImageType] = useState<ImageType>('Primary');
  const [rosterItem, setRosterItem] = useState<MetadataItemSummary>(); const [creditsItem, setCreditsItem] = useState<MetadataItemSummary>();
  const [backgroundPreviewItem, setBackgroundPreviewItem] = useState<MetadataItemSummary>(); const [audioWaveformItem, setAudioWaveformItem] = useState<MetadataItemSummary>();
  const [subtitleTimelineItem, setSubtitleTimelineItem] = useState<MetadataItemSummary>();
  const [editingLibrary, setEditingLibrary] = useState(false); const [bindingLibrary, setBindingLibrary] = useState(false);
  const [actionError, setActionError] = useState<unknown>(); const [notice, setNotice] = useState(''); const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const pageGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const artworkGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const modalGuard = useRef<UserNavigationGuard | undefined>(undefined);
  const artworkSummaryDirty = useRef(false);
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const canChangeScope = useCallback(() => !inFlight.current && [pageGuard.current, artworkGuard.current, modalGuard.current].every((guard) => !guard || guard()), []);
  const publishGuard = useCallback(() => {
    onNavigationGuardChange(pageGuard.current || artworkGuard.current || modalGuard.current ? canChangeScope : undefined);
  }, [canChangeScope, onNavigationGuardChange]);
  const registerPageGuard = useCallback((guard: UserNavigationGuard | undefined) => { pageGuard.current = guard; publishGuard(); }, [publishGuard]);
  const registerArtworkGuard = useCallback((guard: UserNavigationGuard | undefined) => { artworkGuard.current = guard; publishGuard(); }, [publishGuard]);
  const registerModalGuard = useCallback((guard: UserNavigationGuard | undefined) => { modalGuard.current = guard; publishGuard(); }, [publishGuard]);
  const markArtworkChanged = useCallback(() => { artworkSummaryDirty.current = true; }, []);
  useUserDraftNavigation(false, busy, registerPageGuard);
  useEffect(() => () => {
    pageGuard.current = undefined; artworkGuard.current = undefined; modalGuard.current = undefined;
    onNavigationGuardChange(undefined);
  }, [onNavigationGuardChange]);
  useEffect(() => {
    const closeMenus = (event: PointerEvent) => {
      document.querySelectorAll<HTMLDetailsElement>('.media-item-menu[open], .media-types-filter[open]').forEach((menu) => { if (!menu.contains(event.target as Node)) menu.open = false; });
    };
    document.addEventListener('pointerdown', closeMenus);
    return () => document.removeEventListener('pointerdown', closeMenus);
  }, []);
  const mutation = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => mutation.current?.abort(), []);
  const queryKey = JSON.stringify([libraryId, query.searchTerm, query.types, query.page, query.pageSize]);
  const data = loaded?.queryKey === queryKey ? loaded.result : undefined; const failed = failure?.queryKey === queryKey;
  const loading = pending || (!data && !failed); const filtered = Boolean(query.searchTerm || query.types.length > 0);
  const libraryName = loaded?.result.Library.Name || library?.Name || t('Library items');
  useEffect(() => {
    const controller = new AbortController(); setLibraryError(undefined);
    void adminApi.getLibraries({ signal: controller.signal }).then((result) => { if (!controller.signal.aborted) setLibrary(result.Items.find((entry) => entry.Id === libraryId)); }).catch((error: unknown) => { if (!controller.signal.aborted && !isAbortError(error)) setLibraryError(error); });
    return () => controller.abort();
  }, [libraryId, revision]);
  useEffect(() => {
    const controller = new AbortController(); setPending(true); setFailure(undefined); setSelected([]);
    mediaSummaryApi.items(libraryId, { SearchTerm: query.searchTerm || undefined, Types: query.types.length > 0 ? query.types : undefined, StartIndex: query.page * query.pageSize, Limit: query.pageSize }, { signal: controller.signal })
      .then((result) => { if (controller.signal.aborted) return; if (query.page > 0 && query.page * query.pageSize >= result.TotalRecordCount && !pageGuard.current && !artworkGuard.current && !modalGuard.current) { setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(result.TotalRecordCount / query.pageSize) - 1) })); return; } setLoaded({ queryKey, result }); })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ queryKey, cause }); })
      .finally(() => { if (!controller.signal.aborted) setPending(false); });
    return () => controller.abort();
  }, [libraryId, query, queryKey, revision]);
  function changeTab(next: LibraryTab): boolean {
    if (next === tab) return true;
    if (!canChangeScope()) return false;
    setTab(next);
    // The image grid already owns its saved results. Refresh catalog summaries
    // only after an explicitly permitted scope change, preserving unknown writes.
    if (artworkSummaryDirty.current) { artworkSummaryDirty.current = false; refresh(); }
    return true;
  }
  function changeQuery(update: (current: typeof query) => typeof query): boolean {
    if (!canChangeScope()) return false;
    artworkSummaryDirty.current = false;
    setQuery(update);
    return true;
  }
  function refreshItems() {
    if (!canChangeScope()) return;
    artworkSummaryDirty.current = false;
    refresh();
  }
  function openDialog(show: () => void) { if (canChangeScope()) show(); }
  function openEditor(itemId: string, initialTab = 0, imageType: ImageType = 'Primary') {
    openDialog(() => { setEditorTab(initialTab); setEditorImageType(imageType); setEditingItemId(itemId); });
  }
  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const next = searchTitle.trim();
    if (changeQuery((current) => ({ ...current, searchTerm: next, page: 0 }))) setSearchTitle(next);
  }
  function clearFilters() {
    if (!canChangeScope()) return;
    artworkSummaryDirty.current = false;
    setSearchTitle(''); setQuery((current) => ({ ...current, searchTerm: '', types: [], page: 0 }));
  }
  async function scan() {
    if (!canChangeScope()) return; inFlight.current = true; setBusy(true); setActionError(undefined); setNotice('');
    const controller = new AbortController(); mutation.current = controller;
    try { await adminApi.scanLibrary(libraryId, { signal: controller.signal }); if (!controller.signal.aborted) setNotice(t('Scan requested. View progress in Tasks.')); } catch (error) { if (!controller.signal.aborted && !isAbortError(error)) setActionError(error); } finally { inFlight.current = false; if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(false); }
  }
  async function lockSelected(lock: boolean) {
    if (selected.length === 0 || !canChangeScope()) return; inFlight.current = true; setBusy(true); setActionError(undefined); setNotice('');
    const controller = new AbortController(); mutation.current = controller;
    const errors: string[] = []; let completed = 0;
    for (const id of selected) {
      if (controller.signal.aborted) break;
      try {
        const detail = await adminApi.getItemMetadata(id, { signal: controller.signal });
        const editable = detail.EditableFields.filter((field) => !detail.InactiveFields.includes(field));
        const locked = lock ? [...new Set([...detail.LockedFields, ...editable])] : detail.LockedFields.filter((field) => !editable.includes(field));
        await adminApi.updateItemMetadata(id, { Revision: detail.Revision, Overrides: detail.Overrides, LockedFields: locked }, { signal: controller.signal }); completed++;
      } catch (error) { if (!controller.signal.aborted && !isAbortError(error)) errors.push(`${data?.Items.find((item) => item.Id === id)?.Name ?? id}: ${error instanceof Error ? error.message : t('The request could not be completed.')}`); }
    }
    inFlight.current = false; if (mutation.current === controller) mutation.current = undefined;
    if (controller.signal.aborted) return;
    setBusy(false); setSelected([]); refresh();
    if (completed) setNotice(`${t(lock ? 'Fields locked' : 'Fields unlocked')}: ${new Intl.NumberFormat(locale).format(completed)}`);
    if (errors.length) setActionError(new Error(errors.join('\n')));
  }
  const allSelected = Boolean(data?.Items.length && data.Items.every((item) => selected.includes(item.Id)));
  const toggleItem = (id: string) => setSelected((value) => value.includes(id) ? value.filter((entry) => entry !== id) : [...value, id]);
  const pageItems = data?.Items ?? [];
  return <Box aria-busy={loading || busy} className="media-page media-library-items-page">
    <div className="media-library-page-heading"><div className="media-library-heading-title"><IconButton aria-label="Back to libraries" onClick={onLibraries} disabled={busy}><ArrowBackRounded sx={{ fontSize: 17 }} /></IconButton><div><h1 translate="no">{libraryName}</h1><div className="media-library-summary"><span className="media-mono media-ellipsis" translate="no" title={library?.Paths.join(' · ')}>{library?.Paths.join(' · ') || t('Library items')}</span><span>{data ? formatCount(data.TotalRecordCount, 'item', 'items') : '— ' + t('items')}</span>{library?.LastScanAt && <span>{t('Last scan')} {new Date(library.LastScanAt).toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' })}</span>}</div></div></div><div className="media-heading-actions"><Button variant="outlined" onClick={() => openDialog(() => setEditingLibrary(true))} disabled={busy}>Library settings</Button><Button variant="contained" startIcon={busy ? <CircularProgress size={15} color="inherit" /> : <RefreshRounded />} onClick={() => void scan()} disabled={busy}>Scan library</Button></div></div>
    <div className="media-library-tabs" role="tablist" aria-label={t('Library pages')}>{libraryTabs.map((entry, index) => <button id={`library-tab-${entry.id}`} key={entry.id} type="button" role="tab" aria-selected={tab === entry.id} aria-controls={`library-panel-${entry.id}`} tabIndex={tab === entry.id ? 0 : -1} disabled={busy} onClick={() => changeTab(entry.id)} onKeyDown={(event) => { const next = event.key === 'ArrowRight' ? (index + 1) % libraryTabs.length : event.key === 'ArrowLeft' ? (index + libraryTabs.length - 1) % libraryTabs.length : event.key === 'Home' ? 0 : event.key === 'End' ? libraryTabs.length - 1 : -1; if (next >= 0) { event.preventDefault(); if (changeTab(libraryTabs[next].id)) document.getElementById(`library-tab-${libraryTabs[next].id}`)?.focus(); } }}>{t(entry.label)}</button>)}</div>
    {failed && <ErrorNotice error={failure?.cause} retry={refreshItems} />}{actionError != null && <ErrorNotice error={actionError} />}{notice && <Alert severity="success">{notice}{onTasks && <Button size="small" color="inherit" onClick={onTasks}>View tasks</Button>}</Alert>}
    <div role="tabpanel" id={`library-panel-${tab}`} aria-labelledby={`library-tab-${tab}`}>
      {(tab === 'items' || tab === 'images') && <>
        <form className="media-items-toolbar" onSubmit={search}><div className="media-items-search"><MediaSearchField label="Search title" value={searchTitle} onChange={setSearchTitle} disabled={loading || busy} /></div><details className="media-types-filter" onKeyDown={navigateTypeFilter}><summary>{t('Types')} <span>{query.types.length ? query.types.map((value) => t(itemTypeName(value))).join(', ') : t('All types')}</span></summary><div className="media-types-options">{itemTypes.map((option) => <label key={option.value}><input type="checkbox" checked={query.types.includes(option.value)} disabled={loading || busy} onChange={(event) => { const checked = event.target.checked; changeQuery((current) => ({ ...current, searchTerm: searchTitle.trim(), types: checked ? [...current.types, option.value] : current.types.filter((value) => value !== option.value), page: 0 })); }} />{t(option.label)}</label>)}</div></details>{(searchTitle || filtered) && <Button size="small" startIcon={<FilterAltOffOutlined />} onClick={clearFilters} disabled={busy}>Clear filters</Button>}<span className="media-mono media-muted media-toolbar-total">{data ? formatCount(data.TotalRecordCount, 'item', 'items') : '— ' + t('items')}</span><Tooltip title="Refresh library items"><IconButton onClick={refreshItems} disabled={loading || busy} aria-label="Refresh library items"><RefreshRounded sx={{ fontSize: 16 }} /></IconButton></Tooltip></form>
        {data && loading && <LinearProgress aria-label="Refreshing library items" />}
        {!data && loading && <Stack spacing={1} role="status" aria-label="Loading library items"><Skeleton height={52} /><Skeleton height={52} /><Skeleton height={52} /></Stack>}
        {data && pageItems.length === 0 && <section className="media-panel media-empty-state"><VideoLibraryOutlined sx={{ fontSize: 30 }} /><h2>{t(filtered ? 'No matching items' : 'No items in this library')}</h2><p>{t(filtered ? 'Try another title or clear the filters to see more items.' : 'Scan this library to add media to the catalog.')}</p><Button onClick={filtered ? clearFilters : () => void scan()} disabled={busy}>{filtered ? 'Clear filters' : 'Scan library'}</Button></section>}
        {data && tab === 'images' && <fieldset disabled={loading || busy} style={{ border: 0, margin: 0, padding: 0, minWidth: 0 }}>
          <LibraryArtwork key={queryKey} libraryId={libraryId} items={pageItems} revision={revision} onManage={(target, type) => openEditor(target.id, 1, type)} onChanged={markArtworkChanged} onNavigationGuardChange={registerArtworkGuard} />
        </fieldset>}
        {data && pageItems.length > 0 && tab === 'items' && <section className="media-panel media-item-table-panel">
          {selected.length > 0 && <div className="media-selection-toolbar"><span>{new Intl.NumberFormat(locale).format(selected.length)} {t('selected')}</span><Button size="small" disabled={busy || selected.length !== 1} onClick={() => openEditor(selected[0])}>Edit metadata</Button><Button size="small" disabled={busy} startIcon={<LockOutlined />} onClick={() => void lockSelected(true)}>Lock fields</Button><Button size="small" disabled={busy} onClick={() => void lockSelected(false)}>Unlock fields</Button><Button size="small" disabled={busy} onClick={() => setSelected([])}>Clear selection</Button></div>}
          <table className="media-items-table"><thead><tr><th scope="col" className="media-checkbox-cell"><input type="checkbox" aria-label={t('Select all items on this page')} checked={allSelected} ref={(input) => { if (input) input.indeterminate = selected.length > 0 && !allSelected; }} disabled={busy} onChange={() => setSelected(allSelected ? [] : pageItems.map((item) => item.Id))} /></th><th scope="col">{t('Title')}</th><th scope="col" className="media-item-technical">{t('Technical details')}</th><th scope="col" className="media-item-duration">{t('Duration')}</th><th scope="col" className="media-item-images">{t('Images')}</th><th scope="col" className="media-item-analysis">{t('Analysis')}</th><th scope="col" className="media-item-metadata">{t('Metadata')}</th><th scope="col"><span className="visually-hidden">{t('Actions')}</span></th></tr></thead><tbody>{pageItems.map((item) => <tr key={item.Id} data-selected={selected.includes(item.Id) || undefined}><td className="media-checkbox-cell"><input type="checkbox" aria-label={`${t('Select')}: ${item.Name}`} checked={selected.includes(item.Id)} disabled={busy} onChange={() => toggleItem(item.Id)} /></td><td><button type="button" className="media-item-name-button" onClick={() => openEditor(item.Id)} disabled={busy} aria-label={`Edit metadata for ${item.Name}`}><ItemIdentity item={item} /></button></td><td className="media-item-technical"><MediaCatalogTechnical summary={item.Media} disabled={busy} onOpen={() => openEditor(item.Id, 4)} /></td><td className="media-item-duration media-mono" translate="no">{mediaDuration(item.Media?.DurationTicks) ?? '—'}</td><td className="media-item-images"><MediaCatalogImages summary={item.Media} disabled={busy} onOpen={() => openEditor(item.Id, 1)} /></td><td className="media-item-analysis"><MediaCatalogAnalysis summary={item.Media} disabled={busy} onOpen={() => openEditor(item.Id, 3)} /></td><td className="media-item-metadata"><MetadataStatus item={item} /></td><td className="media-row-actions"><Tooltip title="Edit metadata"><IconButton disabled={busy} onClick={() => openEditor(item.Id)} aria-label={`Edit metadata for ${item.Name}`}><EditOutlined sx={{ fontSize: 15 }} /></IconButton></Tooltip><details className="media-item-menu" onClick={(event) => { if (busy) { event.preventDefault(); return; } if ((event.target as HTMLElement).closest('button')) event.currentTarget.open = false; }} onKeyDown={(event) => { if (event.key === 'Escape') { event.currentTarget.open = false; event.currentTarget.querySelector('summary')?.focus(); } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) { event.preventDefault(); event.currentTarget.open = true; const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button')); const current = buttons.indexOf(document.activeElement as HTMLButtonElement); const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 : event.key === 'ArrowDown' ? (current + 1) % buttons.length : current < 0 ? buttons.length - 1 : (current + buttons.length - 1) % buttons.length; buttons[next]?.focus(); } }}><summary aria-label={`${t('More actions')}: ${item.Name}`}><MoreVertRounded sx={{ fontSize: 16 }} /></summary><div role="group" aria-label={`${t('Actions')}: ${item.Name}`}><button type="button" onClick={() => openEditor(item.Id, 4)}>{t('View media details')}</button><button type="button" onClick={() => openEditor(item.Id, 1)}>{t('Manage images')}</button><button type="button" onClick={() => openDialog(() => setSourcesItemId(item.Id))}>{t('Online sources')}</button>{item.Type === 'Series' && <button type="button" onClick={() => openDialog(() => setRosterItem(item))}>{t('Episode roster')}</button>}{!item.IsFolder && ['Movie', 'Episode'].includes(item.Type) && <><button type="button" onClick={() => openDialog(() => setCreditsItem(item))}>{t('Credits markers')}</button><button type="button" onClick={() => openDialog(() => setBackgroundPreviewItem(item))}>{t('Background clips')}</button><button type="button" onClick={() => openDialog(() => setAudioWaveformItem(item))}>{t('Audio waveforms')}</button><button type="button" onClick={() => openDialog(() => setSubtitleTimelineItem(item))}>{t('Subtitle timelines')}</button></>}</div></details></td></tr>)}</tbody></table>
        </section>}
        {data && <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.pageSize} rowsPerPageOptions={[25, 50, 100]} disabled={loading || busy} labelRowsPerPage="Items per page:" onPageChange={(_event, page) => changeQuery((current) => ({ ...current, page }))} onRowsPerPageChange={(event) => { const pageSize = Number(event.target.value); if ([25, 50, 100].includes(pageSize)) changeQuery((current) => ({ ...current, pageSize, page: 0 })); }} />}
      </>}
      {tab === 'directories' && <>
        <div className="media-heading-actions"><Button size="small" disabled={busy} onClick={() => openDialog(() => setEditingLibrary(true))}>Edit directories</Button><Button size="small" disabled={busy || !library} onClick={() => openDialog(() => setBindingLibrary(true))}>Storage bindings</Button></div>
        {libraryError != null && <ErrorNotice error={libraryError} retry={refreshItems} />}
        {library ? <LibraryDirectoriesPanel paths={library.Paths} revision={revision} /> : !libraryError && <Skeleton height={70} />}
        <p className="media-panel-note">{t('Storage bindings protect catalog records when media is unavailable. Review the current storage before approving a replacement.')}</p>
      </>}
      {tab === 'features' && <section className="media-panel"><div className="media-panel-heading"><h2>{t('Features and analysis')}</h2><Button size="small" disabled={busy} onClick={() => openDialog(() => setEditingLibrary(true))}>Library settings</Button>{onTasks && <Button size="small" onClick={onTasks}>View tasks</Button>}</div>{libraryError != null && <div className="media-panel-body"><ErrorNotice error={libraryError} retry={refreshItems} /></div>}{library ? mediaFeatures.map((feature) => { const available = feature.episodes ? library.CollectionType === 'tvshows' : library.CollectionType !== 'music'; const enabled = available && Boolean(library.LibraryOptions?.[feature.key]); return <div className="media-feature-row" key={feature.key}><div><strong>{t(feature.label)}</strong><span className="media-secondary-line">{t(feature.description)}</span></div><Chip size="small" color={enabled ? 'info' : 'default'} label={!available ? 'Not applicable' : enabled ? 'Enabled' : 'Disabled'} /></div>; }) : !libraryError && <Skeleton height={120} />}<p className="media-panel-note">{t('Turning generation off keeps existing results. Replacing results requires an explicit regeneration request.')}</p></section>}
      {tab === 'history' && <LibraryScanHistory libraryId={libraryId} onTasks={onTasks} />}
    </div>
    {tab === 'items' && <p className="media-muted media-library-footnote">{t('Manual overrides stay in effect during library scans. Locked fields keep their saved values.')}</p>}
    {editingItemId && <MetadataEditorDialog key={editingItemId} itemId={editingItemId} currentUserId={currentUserId} initialTab={editorTab} initialImageType={editorImageType} onClose={() => { setEditingItemId(undefined); refresh(); }} onSaved={refresh} onNavigationGuardChange={registerModalGuard} />}
    {sourcesItemId && <OnlineSourcesDialog key={sourcesItemId} itemId={sourcesItemId} onClose={() => { setSourcesItemId(undefined); refresh(); }} onSaved={refresh} onNavigationGuardChange={registerModalGuard} />}
    {rosterItem && <EpisodeRosterDialog key={rosterItem.Id} seriesId={rosterItem.Id} seriesName={rosterItem.Name} onClose={() => setRosterItem(undefined)} onNavigationGuardChange={registerModalGuard} />}
    {creditsItem && <CreditsEditorDialog key={creditsItem.Id} itemId={creditsItem.Id} itemName={creditsItem.Name} currentUserId={currentUserId} onTasks={onTasks} onClose={() => setCreditsItem(undefined)} onNavigationGuardChange={registerModalGuard} />}
    {backgroundPreviewItem && <BackgroundPreviewEditorDialog key={backgroundPreviewItem.Id} itemId={backgroundPreviewItem.Id} itemName={backgroundPreviewItem.Name} currentUserId={currentUserId} onClose={() => setBackgroundPreviewItem(undefined)} onTasks={onTasks} onNavigationGuardChange={registerModalGuard} />}
    {audioWaveformItem && <AudioWaveformDialog key={audioWaveformItem.Id} itemId={audioWaveformItem.Id} itemName={audioWaveformItem.Name} currentUserId={currentUserId} onClose={() => setAudioWaveformItem(undefined)} onTasks={onTasks} onNavigationGuardChange={registerModalGuard} />}
    {subtitleTimelineItem && <SubtitleTimelineDialog key={subtitleTimelineItem.Id} itemId={subtitleTimelineItem.Id} itemName={subtitleTimelineItem.Name} currentUserId={currentUserId} onClose={() => setSubtitleTimelineItem(undefined)} onTasks={onTasks} onNavigationGuardChange={registerModalGuard} />}
    {editingLibrary && <LibraryEditorDialog libraryId={libraryId} onClose={() => setEditingLibrary(false)} onSaved={() => { setEditingLibrary(false); refresh(); }} onNavigationGuardChange={registerModalGuard} />}
    {bindingLibrary && library && <RootBindingDialog library={library} onClose={() => setBindingLibrary(false)} onNavigationGuardChange={registerModalGuard} />}
  </Box>;
}
export function MetadataItemsPage(props: MetadataItemsPageProps) { return <LibraryItemsView key={props.libraryId} {...props} />; }
