import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Box, Breadcrumbs, Button, Checkbox, Chip, FormControl, IconButton, InputLabel, LinearProgress, ListItemText, Menu, MenuItem, OutlinedInput, Paper, Select, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, Tooltip, Typography } from '@mui/material';
import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import EditOutlined from '@mui/icons-material/EditOutlined';
import FilterAltOffOutlined from '@mui/icons-material/FilterAltOffOutlined';
import LockOutlined from '@mui/icons-material/LockOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import MoreVertRounded from '@mui/icons-material/MoreVertRounded';
import NavigateNextRounded from '@mui/icons-material/NavigateNextRounded';
import VideoLibraryOutlined from '@mui/icons-material/VideoLibraryOutlined';
import { adminApi, isAbortError } from './api';
import type { MetadataItemSummary, MetadataItemsResponse } from './api';
import { ErrorNotice } from './components';
import { MetadataEditorDialog } from './MetadataEditorDialog';
import { EpisodeRosterDialog } from './EpisodeRosterDialog';
import { OnlineSourcesDialog } from './OnlineSourcesDialog';
import { CreditsEditorDialog } from './CreditsEditorDialog';
import { BackgroundPreviewEditorDialog } from './BackgroundPreviewEditorDialog';
import { AudioWaveformDialog } from './AudioWaveformDialog';
import { SubtitleTimelineDialog } from './SubtitleTimelineDialog';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { MediaSearchField, mediaPanelSx, mediaSelectSx } from './MediaPagePrimitives';

const itemTypes = [
  { value: 'Movie', label: 'Movie' },
  { value: 'Video', label: 'Video' },
  { value: 'Series', label: 'Series' },
  { value: 'Season', label: 'Season' },
  { value: 'Episode', label: 'Episode' },
  { value: 'Folder', label: 'Folder' },
  { value: 'Audio', label: 'Audio' },
  { value: 'MusicAlbum', label: 'Music album' },
  { value: 'MusicArtist', label: 'Music artist' },
] as const;

type ItemType = (typeof itemTypes)[number]['value'];

function itemTypeName(value: string): string {
  return itemTypes.find((type) => type.value === value)?.label ?? value;
}

function itemPosition(item: MetadataItemSummary): string {
  if (item.Type === 'Episode') return [item.ParentIndexNumber == null ? '' : `Season ${item.ParentIndexNumber}`, item.IndexNumber == null ? '' : `Episode ${item.IndexNumber}`].filter(Boolean).join(' · ');
  if (item.Type === 'Season' && item.IndexNumber != null) return `Season ${item.IndexNumber}`;
  if (item.Type === 'Audio') return [item.ParentIndexNumber == null ? '' : `Disc ${item.ParentIndexNumber}`, item.IndexNumber == null ? '' : `Track ${item.IndexNumber}`].filter(Boolean).join(' · ');
  return '';
}

function ItemIdentity({ name, path, parentId, parentName, position }: { name: string; path: string; parentId: string; parentName: string; position: string }) {
  const context = [parentName, position].filter(Boolean).join(' · ');
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{name || 'Untitled item'}</Typography>
      {context && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.25, overflowWrap: 'anywhere' }}>{context}</Typography>}
      {(path || (parentId && !parentName)) && <Typography variant="caption" color="text.secondary" title={path || `Parent: ${parentId}`} sx={{ display: 'block', fontFamily: '"JetBrains Mono Variable", Consolas, monospace', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 11, mt: 0.25 }}>{path || `Parent: ${parentId}`}</Typography>}
    </Box>
  );
}

function MetadataStatus({ hasOverrides, lockedFieldCount }: { hasOverrides: boolean; lockedFieldCount: number }) {
  return (
    <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 0.75 }}>
      {(hasOverrides || lockedFieldCount === 0) && <Chip size="small" color={hasOverrides ? 'info' : 'default'} label={hasOverrides ? 'Edited' : 'Automatic'} />}
      {lockedFieldCount > 0 && <Chip size="small" color="warning" icon={<LockOutlined />} label={`${lockedFieldCount} locked`} />}
    </Stack>
  );
}

function ItemsLoading() {
  return <Stack spacing={1} sx={{ px: { xs: 2.5, sm: 3 }, py: 2 }} role="status" aria-label="Loading library items"><Skeleton height={62} /><Skeleton height={62} /><Skeleton height={62} /><Skeleton height={62} /></Stack>;
}

function ItemsList({ items, onEdit, onSources, onRoster, onCredits, onBackgroundPreview, onAudioWaveform, onSubtitleTimeline }: { items: MetadataItemSummary[]; onEdit: (itemId: string) => void; onSources: (itemId: string) => void; onRoster: (item: MetadataItemSummary) => void; onCredits: (item: MetadataItemSummary) => void; onBackgroundPreview: (item: MetadataItemSummary) => void; onAudioWaveform: (item: MetadataItemSummary) => void; onSubtitleTimeline: (item: MetadataItemSummary) => void }) {
  const [menu, setMenu] = useState<{ anchor: HTMLElement; item: MetadataItemSummary }>();
  return (
    <>
      <Box component="ul" aria-label="Library items" sx={{ display: { xs: 'block', md: 'none' }, listStyle: 'none', p: 0, m: 0 }}>
        {items.map((item) => (
          <Box component="li" key={item.Id} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}>
            <Stack direction="row" sx={{ gap: 1, mb: 1, alignItems: 'center', flexWrap: 'wrap' }}>
              <Typography variant="caption" color="text.secondary">{itemTypeName(item.Type)}</Typography>
              <Typography variant="caption" color="text.secondary">{item.ProductionYear == null ? 'Year not set' : item.ProductionYear}</Typography>
            </Stack>
            <ItemIdentity name={item.Name} path={item.Path} parentId={item.ParentId} parentName={item.ParentName} position={itemPosition(item)} />
            <Box sx={{ mt: 2 }}><MetadataStatus hasOverrides={item.HasOverrides} lockedFieldCount={item.LockedFieldCount} /></Box>
            <Button size="small" variant="outlined" startIcon={<EditOutlined />} onClick={() => onEdit(item.Id)} aria-label={`Edit metadata for ${item.Name}`} sx={{ mt: 2 }}>Edit metadata</Button>
            <Button size="small" onClick={() => onSources(item.Id)} aria-label={`Online sources for ${item.Name}`} sx={{ mt: 2, ml: 1 }}>Online sources</Button>
            {item.Type === 'Series' && <Button size="small" onClick={() => onRoster(item)} aria-label={`Episode roster for ${item.Name}`} sx={{ mt: 2, ml: 1 }}>Episode roster</Button>}
            {!item.IsFolder && (item.Type === 'Movie' || item.Type === 'Episode') && <Button size="small" onClick={() => onCredits(item)} aria-label={`编辑「${item.Name}」的片尾标记`} sx={{ mt: 2, ml: 1 }}>片尾标记</Button>}
            {!item.IsFolder && (item.Type === 'Movie' || item.Type === 'Episode') && <Button size="small" onClick={() => onBackgroundPreview(item)} aria-label={`管理「${item.Name}」的背景短片`} sx={{ mt: 2, ml: 1 }}>背景短片</Button>}
            {!item.IsFolder && (item.Type === 'Movie' || item.Type === 'Episode') && <Button size="small" onClick={() => onAudioWaveform(item)} aria-label={`管理「${item.Name}」的音轨波形`} sx={{ mt: 2, ml: 1 }}>音轨波形</Button>}
            {!item.IsFolder && (item.Type === 'Movie' || item.Type === 'Episode') && <Button size="small" onClick={() => onSubtitleTimeline(item)} aria-label={`管理「${item.Name}」的字幕时间轴`} sx={{ mt: 2, ml: 1 }}>字幕时间轴</Button>}
          </Box>
        ))}
      </Box>
      <TableContainer sx={{ display: { xs: 'none', md: 'block' }, borderRadius: 0 }}>
        <Table aria-label="Library items" sx={{ tableLayout: 'fixed' }}>
          <TableHead><TableRow><TableCell sx={{ pl: 3 }}>Name</TableCell><TableCell sx={{ width: '13%' }}>Type</TableCell><TableCell sx={{ width: '10%' }}>Year</TableCell><TableCell sx={{ width: '23%' }}>Metadata</TableCell><TableCell align="right" sx={{ px: 2, width: 112 }}>Edit</TableCell></TableRow></TableHead>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.Id}>
                <TableCell component="th" scope="row" sx={{ pl: 3, maxWidth: 440 }}><ItemIdentity name={item.Name} path={item.Path} parentId={item.ParentId} parentName={item.ParentName} position={itemPosition(item)} /></TableCell>
                <TableCell><Typography variant="body2">{itemTypeName(item.Type)}</Typography></TableCell>
                <TableCell sx={{ whiteSpace: 'nowrap' }}><Typography variant="body2" color="text.secondary">{item.ProductionYear ?? '—'}</Typography></TableCell>
                <TableCell><MetadataStatus hasOverrides={item.HasOverrides} lockedFieldCount={item.LockedFieldCount} /></TableCell>
                <TableCell align="right" sx={{ px: 2, whiteSpace: 'nowrap' }}><Tooltip title="Edit metadata"><IconButton onClick={() => onEdit(item.Id)} aria-label={`Edit metadata for ${item.Name}`}><EditOutlined sx={{ fontSize: 19 }} /></IconButton></Tooltip><IconButton onClick={(event) => setMenu({ anchor: event.currentTarget, item })} aria-label={`More actions for ${item.Name}`} aria-haspopup="menu" aria-expanded={menu?.item.Id === item.Id}><MoreVertRounded sx={{ fontSize: 19 }} /></IconButton></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      <Menu anchorEl={menu?.anchor} open={Boolean(menu)} onClose={() => setMenu(undefined)}><MenuItem onClick={() => { if (menu) onSources(menu.item.Id); setMenu(undefined); }}>Online sources</MenuItem>{menu?.item.Type === 'Series' && <MenuItem onClick={() => { onRoster(menu.item); setMenu(undefined); }}>Episode roster</MenuItem>}{menu && !menu.item.IsFolder && (menu.item.Type === 'Movie' || menu.item.Type === 'Episode') && <MenuItem onClick={() => { onCredits(menu.item); setMenu(undefined); }}>片尾标记</MenuItem>}{menu && !menu.item.IsFolder && (menu.item.Type === 'Movie' || menu.item.Type === 'Episode') && <MenuItem onClick={() => { onBackgroundPreview(menu.item); setMenu(undefined); }}>背景短片</MenuItem>}{menu && !menu.item.IsFolder && (menu.item.Type === 'Movie' || menu.item.Type === 'Episode') && <MenuItem onClick={() => { onAudioWaveform(menu.item); setMenu(undefined); }}>音轨波形</MenuItem>}{menu && !menu.item.IsFolder && (menu.item.Type === 'Movie' || menu.item.Type === 'Episode') && <MenuItem onClick={() => { onSubtitleTimeline(menu.item); setMenu(undefined); }}>字幕时间轴</MenuItem>}</Menu>
    </>
  );
}

function ItemFilters({ searchTitle, types, loading, canClear, onSearchTitleChange, onTypesChange, onSearch, onClear }: { searchTitle: string; types: ItemType[]; loading: boolean; canClear: boolean; onSearchTitleChange: (value: string) => void; onTypesChange: (value: ItemType[]) => void; onSearch: (event: FormEvent<HTMLFormElement>) => void; onClear: () => void }) {
  return (
    <Box component="form" onSubmit={onSearch} sx={{ p: 2, display: 'flex', flexWrap: 'wrap', gap: 1.5, alignItems: 'center' }}>
      <Box sx={{ width: { xs: '100%', sm: 330 } }}><MediaSearchField label="Search title" value={searchTitle} onChange={onSearchTitleChange} disabled={loading} /></Box>
      <FormControl size="small" sx={mediaSelectSx}>
        <InputLabel id="items-types-label" shrink>Types</InputLabel>
        <Select<ItemType[]>
          id="items-types"
          labelId="items-types-label"
          multiple
          displayEmpty
          value={types}
          input={<OutlinedInput label="Types" />}
          onChange={(event) => {
            const next = typeof event.target.value === 'string' ? event.target.value.split(',') : event.target.value;
            onTypesChange(itemTypes.filter((type) => next.includes(type.value)).map((type) => type.value));
          }}
          renderValue={(selected) => selected.length === 0 ? 'All types' : selected.length <= 2 ? selected.map(itemTypeName).join(', ') : `${selected.length} types`}
          MenuProps={{ slotProps: { paper: { sx: { maxHeight: 360 } } } }}
        >
          {itemTypes.map((type) => <MenuItem key={type.value} value={type.value}><Checkbox size="small" checked={types.includes(type.value)} /><ListItemText primary={type.label} /></MenuItem>)}
        </Select>
      </FormControl>
      {canClear && <Button color="secondary" onClick={onClear} startIcon={<FilterAltOffOutlined />}>Clear filters</Button>}
    </Box>
  );
}

interface MetadataItemsPageProps {
  libraryId: string;
  currentUserId: string;
  onLibraries: () => void;
  onTasks?: () => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}

function LibraryItemsView({ libraryId, currentUserId, onLibraries, onTasks, onNavigationGuardChange }: MetadataItemsPageProps) {
  const [searchTitle, setSearchTitle] = useState('');
  const [query, setQuery] = useState({ searchTerm: '', types: [] as ItemType[], page: 0, pageSize: 50 });
  const [loaded, setLoaded] = useState<{ queryKey: string; result: MetadataItemsResponse }>();
  const [failure, setFailure] = useState<{ queryKey: string; cause: unknown }>();
  const [pending, setPending] = useState(true);
  const [revision, setRevision] = useState(0);
  const [editingItemId, setEditingItemId] = useState<string>();
  const [sourcesItemId, setSourcesItemId] = useState<string>();
  const [rosterItem, setRosterItem] = useState<MetadataItemSummary>();
  const [creditsItem, setCreditsItem] = useState<MetadataItemSummary>();
  const [backgroundPreviewItem, setBackgroundPreviewItem] = useState<MetadataItemSummary>();
  const [audioWaveformItem, setAudioWaveformItem] = useState<MetadataItemSummary>();
  const [subtitleTimelineItem, setSubtitleTimelineItem] = useState<MetadataItemSummary>();
  const queryKey = JSON.stringify([libraryId, query.searchTerm, query.types, query.page, query.pageSize]);
  const data = loaded?.queryKey === queryKey ? loaded.result : undefined;
  const failed = failure?.queryKey === queryKey;
  const loading = pending || (!data && !failed);
  const filtered = Boolean(query.searchTerm || query.types.length > 0);
  const libraryName = loaded?.result.Library.Name || 'Library items';

  useEffect(() => {
    const controller = new AbortController();
    setPending(true);
    setFailure(undefined);
    adminApi.getLibraryItems(libraryId, {
      SearchTerm: query.searchTerm || undefined,
      Types: query.types.length > 0 ? query.types : undefined,
      StartIndex: query.page * query.pageSize,
      Limit: query.pageSize,
    }, { signal: controller.signal })
      .then((result) => {
        if (controller.signal.aborted) return;
        if (query.page > 0 && query.page * query.pageSize >= result.TotalRecordCount) {
          setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(result.TotalRecordCount / query.pageSize) - 1) }));
          return;
        }
        setLoaded({ queryKey, result });
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted && !isAbortError(cause)) setFailure({ queryKey, cause });
      })
      .finally(() => { if (!controller.signal.aborted) setPending(false); });
    return () => controller.abort();
  }, [libraryId, query, queryKey, revision]);

  const refresh = () => setRevision((value) => value + 1);

  function search(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const next = searchTitle.trim();
    setSearchTitle(next);
    setQuery((current) => ({ ...current, searchTerm: next, page: 0 }));
  }

  function clearFilters() {
    setSearchTitle('');
    setQuery((current) => ({ ...current, searchTerm: '', types: [], page: 0 }));
  }

  return (
    <Box aria-busy={loading}>
      <Typography component="h2" className="visually-hidden">{libraryName}</Typography>
      <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 2.5 }}>
        <IconButton aria-label="Back to libraries" onClick={onLibraries} sx={{ ml: -1 }}><ArrowBackRounded sx={{ fontSize: 20 }} /></IconButton>
        <Breadcrumbs aria-label="Library navigation" separator={<NavigateNextRounded sx={{ fontSize: 16 }} />} sx={{ '& .MuiBreadcrumbs-li': { minWidth: 0 } }}>
          <Button onClick={onLibraries} sx={{ p: 0, minWidth: 0, minHeight: 32 }}>Libraries</Button>
          <Typography variant="body2" color="text.primary" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{libraryName}</Typography>
        </Breadcrumbs>
        <Typography color="text.secondary" sx={{ flex: '1 1 300px', ml: { sm: 1 }, fontSize: 14 }}>Review catalog entries and edit the metadata your clients display.</Typography>
        <Tooltip title="Refresh library items"><IconButton onClick={refresh} disabled={loading} aria-label="Refresh library items"><RefreshRounded sx={{ fontSize: 20 }} /></IconButton></Tooltip>
      </Stack>
      {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={failure?.cause} retry={refresh} /></Box>}
      <Paper variant="outlined" sx={mediaPanelSx}>
        <ItemFilters searchTitle={searchTitle} types={query.types} loading={loading} canClear={Boolean(searchTitle || filtered)} onSearchTitleChange={setSearchTitle} onTypesChange={(types) => setQuery((current) => ({ ...current, searchTerm: searchTitle.trim(), types, page: 0 }))} onSearch={search} onClear={clearFilters} />
        {data && loading && <LinearProgress aria-label="Refreshing library items" sx={{ height: 3 }} />}
        {!data && loading && <ItemsLoading />}
        {data && data.Items.length === 0 && (
          <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
            <VideoLibraryOutlined sx={{ fontSize: 40, color: 'primary.main' }} />
            <Typography variant="h3" component="h3">{filtered ? 'No matching items' : 'No items in this library'}</Typography>
            <Typography color="text.secondary" sx={{ maxWidth: 440 }}>{filtered ? 'Try another title or clear the filters to see more items.' : 'Scan this library from Libraries to add media to the catalog.'}</Typography>
            <Button onClick={filtered ? clearFilters : onLibraries} startIcon={filtered ? <FilterAltOffOutlined /> : <ArrowBackRounded />}>{filtered ? 'Clear filters' : 'Back to libraries'}</Button>
          </Stack>
        )}
        {data && data.Items.length > 0 && <ItemsList items={data.Items} onEdit={setEditingItemId} onSources={setSourcesItemId} onRoster={setRosterItem} onCredits={setCreditsItem} onBackgroundPreview={setBackgroundPreviewItem} onAudioWaveform={setAudioWaveformItem} onSubtitleTimeline={setSubtitleTimelineItem} />}
        {!data && !loading && failed && <Typography variant="body2" color="text.secondary" sx={{ px: { xs: 2.5, sm: 3 }, pb: 3 }}>The item list could not be loaded. Retry the request to see this library.</Typography>}
        {data && (
          <TablePagination
            component="div"
            count={data.TotalRecordCount}
            page={query.page}
            rowsPerPage={query.pageSize}
            rowsPerPageOptions={[25, 50, 100]}
            disabled={loading}
            labelRowsPerPage="Items per page:"
            onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))}
            onRowsPerPageChange={(event) => {
              const pageSize = Number(event.target.value);
              if ([25, 50, 100].includes(pageSize)) setQuery((current) => ({ ...current, pageSize, page: 0 }));
            }}
            sx={{ borderTop: 1, borderColor: 'divider', '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 2, sm: 3 }, py: 1, gap: 0.5 }, '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } }, '& .MuiTablePagination-actions': { ml: { xs: 1, sm: 2 } } }}
          />
        )}
      </Paper>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 2.5, px: 0.5 }}>Manual overrides stay in effect during library scans. Locked fields keep their saved values.</Typography>
      {editingItemId && <MetadataEditorDialog key={editingItemId} itemId={editingItemId} onClose={() => { setEditingItemId(undefined); refresh(); }} onSaved={refresh} onNavigationGuardChange={onNavigationGuardChange} />}
      {sourcesItemId && <OnlineSourcesDialog key={sourcesItemId} itemId={sourcesItemId} onClose={() => { setSourcesItemId(undefined); refresh(); }} onSaved={refresh} onNavigationGuardChange={onNavigationGuardChange} />}
      {rosterItem && <EpisodeRosterDialog key={rosterItem.Id} seriesId={rosterItem.Id} seriesName={rosterItem.Name} onClose={() => setRosterItem(undefined)} onNavigationGuardChange={onNavigationGuardChange} />}
      {creditsItem && <CreditsEditorDialog key={creditsItem.Id} itemId={creditsItem.Id} itemName={creditsItem.Name} currentUserId={currentUserId} onTasks={onTasks} onClose={() => setCreditsItem(undefined)} onNavigationGuardChange={onNavigationGuardChange} />}
      {backgroundPreviewItem && <BackgroundPreviewEditorDialog key={backgroundPreviewItem.Id} itemId={backgroundPreviewItem.Id} itemName={backgroundPreviewItem.Name} currentUserId={currentUserId} onClose={() => setBackgroundPreviewItem(undefined)} onTasks={onTasks} onNavigationGuardChange={onNavigationGuardChange} />}
      {audioWaveformItem && <AudioWaveformDialog key={audioWaveformItem.Id} itemId={audioWaveformItem.Id} itemName={audioWaveformItem.Name} currentUserId={currentUserId} onClose={() => setAudioWaveformItem(undefined)} onTasks={onTasks} onNavigationGuardChange={onNavigationGuardChange} />}
      {subtitleTimelineItem && <SubtitleTimelineDialog key={subtitleTimelineItem.Id} itemId={subtitleTimelineItem.Id} itemName={subtitleTimelineItem.Name} currentUserId={currentUserId} onClose={() => setSubtitleTimelineItem(undefined)} onTasks={onTasks} onNavigationGuardChange={onNavigationGuardChange} />}
    </Box>
  );
}

export function MetadataItemsPage(props: MetadataItemsPageProps) {
  return <LibraryItemsView key={props.libraryId} {...props} />;
}
