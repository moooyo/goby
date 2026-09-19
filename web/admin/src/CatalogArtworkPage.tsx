import { useEffect, useState } from 'react';
import { Box, Button, MenuItem, Paper, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography } from '@mui/material';
import ImageOutlined from '@mui/icons-material/ImageOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { isAbortError } from './api';
import { ArtworkManagerDialog } from './ArtworkManagerDialog';
import { catalogEntitiesApi } from './catalogEntitiesApi';
import type { CatalogEntity, CatalogEntityPage, EntityView } from './catalogEntitiesApi';
import { ErrorNotice, PageHeading } from './components';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const views: { value: EntityView; label: string }[] = [{ value: 'Person', label: 'People' }, { value: 'Genre', label: 'Genres' }, { value: 'Studio', label: 'Studios' }, { value: 'Tag', label: 'Tags' }, { value: 'MusicArtist', label: 'Music artists' }, { value: 'AlbumArtist', label: 'Album artists' }, { value: 'MusicGenre', label: 'Music genres' }];
export function CatalogArtworkPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [view, setView] = useState<EntityView>('Person');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(0);
  const [data, setData] = useState<CatalogEntityPage>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const [selected, setSelected] = useState<CatalogEntity>();
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setData(undefined); setError(null);
    void catalogEntitiesApi.list(view, query, page * 25, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      if (page > 0 && page * 25 >= result.TotalRecordCount) setPage(Math.max(0, Math.ceil(result.TotalRecordCount / 25) - 1));
      else setData(result);
    }).catch((cause: unknown) => { if (!isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [view, query, page, revision]);
  return <Box aria-busy={loading}>
    <PageHeading title="Catalog artwork" description="Manage images for people, music artists, genres, studios, and tags. Edit track and album credits in Libraries." action={<Button startIcon={<RefreshRounded />} disabled={loading} onClick={() => setRevision((value) => value + 1)}>Refresh</Button>} />
    <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
      <Box component="form" onSubmit={(event) => { event.preventDefault(); setQuery(search.trim()); setPage(0); }} sx={{ p: 3 }}><Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}><TextField select label="Catalog type" value={view} onChange={(event) => { setView(event.target.value as EntityView); setPage(0); }} sx={{ minWidth: 190 }}>{views.map((item) => <MenuItem key={item.value} value={item.value}>{item.label}</MenuItem>)}</TextField><TextField fullWidth label="Search names" value={search} onChange={(event) => setSearch(event.target.value)} /><Button type="submit" variant="outlined" disabled={loading}>Search</Button></Stack></Box>
      {error != null && <Box sx={{ px: 3, pb: 3 }}><ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} /></Box>}
      {loading && <Box sx={{ p: 3 }} role="status" aria-label="Loading catalog entries"><Skeleton height={54} /><Skeleton height={54} /><Skeleton height={54} /></Box>}
      {data && data.Items.length === 0 && <Box sx={{ px: 3, pb: 4, textAlign: 'center' }}><ImageOutlined sx={{ color: 'primary.main', fontSize: 36 }} /><Typography component="h2" variant="h4" sx={{ mt: 1 }}>No matching entries</Typography><Typography color="text.secondary" variant="body2" sx={{ mt: 1 }}>Scan a library to populate these entries, or try another search.</Typography></Box>}
      {data && data.Items.length > 0 && <TableContainer><Table aria-label="Catalog entries"><TableHead><TableRow><TableCell>Name</TableCell><TableCell>Type</TableCell><TableCell align="right">Images</TableCell></TableRow></TableHead><TableBody>{data.Items.map((item) => <TableRow key={item.Id}><TableCell component="th" scope="row" sx={{ overflowWrap: 'anywhere' }}>{item.Name}</TableCell><TableCell>{item.Type}</TableCell><TableCell align="right"><Button startIcon={<ImageOutlined />} aria-label={`Manage artwork for ${item.Name}`} onClick={() => setSelected(item)}>Manage artwork</Button></TableCell></TableRow>)}</TableBody></Table></TableContainer>}
      {data && <TablePagination component="div" count={data.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} onPageChange={(_, value) => setPage(value)} disabled={loading} sx={{ borderTop: 1, borderColor: 'divider' }} />}
    </Paper>
    {selected && <ArtworkManagerDialog target={{ kind: 'entities', id: selected.Id, name: selected.Name }} onClose={() => setSelected(undefined)} onNavigationGuardChange={onNavigationGuardChange} />}
  </Box>;
}
