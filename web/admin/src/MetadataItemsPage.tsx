import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Box, Button, Checkbox, Chip, FormControl, InputLabel, LinearProgress, ListItemText, MenuItem, OutlinedInput, Paper, Select, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography } from '@mui/material';
import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import EditOutlined from '@mui/icons-material/EditOutlined';
import FilterAltOffOutlined from '@mui/icons-material/FilterAltOffOutlined';
import LockOutlined from '@mui/icons-material/LockOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SearchRounded from '@mui/icons-material/SearchRounded';
import VideoLibraryOutlined from '@mui/icons-material/VideoLibraryOutlined';
import { adminApi, isAbortError } from './api';
import type { MetadataItemSummary, MetadataItemsResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { MetadataEditorDialog } from './MetadataEditorDialog';
import type { UserNavigationGuardChange } from './userDraftNavigation';

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
      <Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>{name || 'Untitled item'}</Typography>
      {context && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.25, overflowWrap: 'anywhere' }}>{context}</Typography>}
      {(path || (parentId && !parentName)) && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontFamily: 'ui-monospace, Consolas, monospace', overflowWrap: 'anywhere', mt: 0.5 }}>{path || `Parent: ${parentId}`}</Typography>}
    </Box>
  );
}

function MetadataStatus({ hasOverrides, lockedFieldCount }: { hasOverrides: boolean; lockedFieldCount: number }) {
  return (
    <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 0.75 }}>
      <Chip size="small" variant="outlined" color={hasOverrides ? 'primary' : 'default'} label={hasOverrides ? 'Manual overrides' : 'No manual overrides'} />
      {lockedFieldCount > 0 && <Chip size="small" variant="outlined" icon={<LockOutlined />} label={`${lockedFieldCount} locked`} />}
    </Stack>
  );
}

function ItemsLoading() {
  return <Stack spacing={1} sx={{ px: { xs: 2.5, sm: 3 }, py: 2 }} role="status" aria-label="Loading library items"><Skeleton height={62} /><Skeleton height={62} /><Skeleton height={62} /><Skeleton height={62} /></Stack>;
}

function ItemsList({ items, onEdit }: { items: MetadataItemSummary[]; onEdit: (itemId: string) => void }) {
  return (
    <>
      <Box component="ul" aria-label="Library items" sx={{ display: { xs: 'block', lg: 'none' }, listStyle: 'none', p: 0, m: 0 }}>
        {items.map((item) => (
          <Box component="li" key={item.Id} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}>
            <Stack direction="row" sx={{ gap: 1, mb: 1, alignItems: 'center', flexWrap: 'wrap' }}>
              <Typography variant="caption" color="text.secondary">{itemTypeName(item.Type)}</Typography>
              <Typography variant="caption" color="text.secondary">{item.ProductionYear == null ? 'Year not set' : item.ProductionYear}</Typography>
            </Stack>
            <ItemIdentity name={item.Name} path={item.Path} parentId={item.ParentId} parentName={item.ParentName} position={itemPosition(item)} />
            <Box sx={{ mt: 2 }}><MetadataStatus hasOverrides={item.HasOverrides} lockedFieldCount={item.LockedFieldCount} /></Box>
            <Button size="small" variant="outlined" startIcon={<EditOutlined />} onClick={() => onEdit(item.Id)} aria-label={`Edit metadata for ${item.Name}`} sx={{ mt: 2 }}>Edit metadata</Button>
          </Box>
        ))}
      </Box>
      <TableContainer sx={{ display: { xs: 'none', lg: 'block' } }}>
        <Table aria-label="Library items" sx={{ minWidth: 760 }}>
          <TableHead><TableRow><TableCell sx={{ pl: 3, width: '42%' }}>Name</TableCell><TableCell>Type</TableCell><TableCell>Year</TableCell><TableCell>Metadata</TableCell><TableCell align="right" sx={{ pr: 3 }}>Edit</TableCell></TableRow></TableHead>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.Id}>
                <TableCell component="th" scope="row" sx={{ pl: 3, maxWidth: 440 }}><ItemIdentity name={item.Name} path={item.Path} parentId={item.ParentId} parentName={item.ParentName} position={itemPosition(item)} /></TableCell>
                <TableCell sx={{ whiteSpace: 'nowrap' }}><Typography variant="body2">{itemTypeName(item.Type)}</Typography></TableCell>
                <TableCell sx={{ whiteSpace: 'nowrap' }}><Typography variant="body2" color="text.secondary">{item.ProductionYear ?? '—'}</Typography></TableCell>
                <TableCell><MetadataStatus hasOverrides={item.HasOverrides} lockedFieldCount={item.LockedFieldCount} /></TableCell>
                <TableCell align="right" sx={{ pr: 3, whiteSpace: 'nowrap' }}><Button size="small" startIcon={<EditOutlined />} onClick={() => onEdit(item.Id)} aria-label={`Edit metadata for ${item.Name}`}>Edit metadata</Button></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
    </>
  );
}

function ItemFilters({ searchTitle, types, loading, canClear, onSearchTitleChange, onTypesChange, onSearch, onClear }: { searchTitle: string; types: ItemType[]; loading: boolean; canClear: boolean; onSearchTitleChange: (value: string) => void; onTypesChange: (value: ItemType[]) => void; onSearch: (event: FormEvent<HTMLFormElement>) => void; onClear: () => void }) {
  return (
    <Box component="form" onSubmit={onSearch} sx={{ px: { xs: 2.5, sm: 3 }, pb: 2.5, display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'minmax(0, 1fr) 230px', lg: 'minmax(0, 1fr) 230px auto' }, gap: 2, alignItems: 'start' }}>
      <TextField id="items-search-title" name="SearchTerm" fullWidth label="Search title" value={searchTitle} onChange={(event) => onSearchTitleChange(event.target.value)} helperText="Searches item names in this library." slotProps={{ htmlInput: { autoComplete: 'off' } }} />
      <FormControl fullWidth>
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
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1, pt: { xs: 0, lg: 1 }, gridColumn: { xs: 'auto', md: '1 / -1', lg: 'auto' } }}>
        <Button type="submit" variant="outlined" startIcon={<SearchRounded />} disabled={loading}>Search</Button>
        <Button color="secondary" onClick={onClear} disabled={!canClear} startIcon={<FilterAltOffOutlined />}>Clear filters</Button>
      </Stack>
    </Box>
  );
}

interface MetadataItemsPageProps {
  libraryId: string;
  onLibraries: () => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}

function LibraryItemsView({ libraryId, onLibraries, onNavigationGuardChange }: MetadataItemsPageProps) {
  const [searchTitle, setSearchTitle] = useState('');
  const [query, setQuery] = useState({ searchTerm: '', types: [] as ItemType[], page: 0, pageSize: 50 });
  const [loaded, setLoaded] = useState<{ queryKey: string; result: MetadataItemsResponse }>();
  const [failure, setFailure] = useState<{ queryKey: string; cause: unknown }>();
  const [pending, setPending] = useState(true);
  const [revision, setRevision] = useState(0);
  const [editingItemId, setEditingItemId] = useState<string>();
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
    <Box aria-busy={loading} sx={{ '& h1': { overflowWrap: 'anywhere' } }}>
      <Button color="secondary" startIcon={<ArrowBackRounded />} onClick={onLibraries} sx={{ mb: 2, ml: -1, px: 1 }}>Back to libraries</Button>
      <PageHeading title={libraryName} description="Review catalog entries and edit the metadata your clients display." />
      {failed && <Box sx={{ mb: 3 }}><ErrorNotice error={failure?.cause} retry={refresh} /></Box>}
      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, px: { xs: 2.5, sm: 3 }, py: 2.2 }}>
          <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1.2 }}>
            <Typography variant="h4" component="h2">{filtered ? 'Matching items' : 'Library items'}</Typography>
            {data && <Chip label={data.TotalRecordCount.toLocaleString()} size="small" sx={{ bgcolor: 'background.default' }} />}
            {data && loading && <Typography variant="caption" color="text.secondary" role="status" aria-live="polite">Refreshing items...</Typography>}
          </Stack>
          <Button size="small" onClick={refresh} disabled={loading} startIcon={<RefreshRounded />}>Refresh</Button>
        </Stack>
        <ItemFilters searchTitle={searchTitle} types={query.types} loading={loading} canClear={Boolean(searchTitle || filtered)} onSearchTitleChange={setSearchTitle} onTypesChange={(types) => setQuery((current) => ({ ...current, searchTerm: searchTitle.trim(), types, page: 0 }))} onSearch={search} onClear={clearFilters} />
        <Box sx={{ height: 3 }}>{data && loading && <LinearProgress aria-label="Refreshing library items" sx={{ height: 3 }} />}</Box>
        {!data && loading && <ItemsLoading />}
        {data && data.Items.length === 0 && (
          <Stack spacing={1.5} sx={{ alignItems: 'center', p: { xs: 3, sm: 5 }, borderTop: 1, borderColor: 'divider', textAlign: 'center' }}>
            <VideoLibraryOutlined sx={{ fontSize: 40, color: 'primary.main' }} />
            <Typography variant="h3" component="h3">{filtered ? 'No matching items' : 'No items in this library'}</Typography>
            <Typography color="text.secondary" sx={{ maxWidth: 440 }}>{filtered ? 'Try another title or clear the filters to see more items.' : 'Scan this library from Libraries to add media to the catalog.'}</Typography>
            <Button onClick={filtered ? clearFilters : onLibraries} startIcon={filtered ? <FilterAltOffOutlined /> : <ArrowBackRounded />}>{filtered ? 'Clear filters' : 'Back to libraries'}</Button>
          </Stack>
        )}
        {data && data.Items.length > 0 && <ItemsList items={data.Items} onEdit={setEditingItemId} />}
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
    </Box>
  );
}

export function MetadataItemsPage(props: MetadataItemsPageProps) {
  return <LibraryItemsView key={props.libraryId} {...props} />;
}
