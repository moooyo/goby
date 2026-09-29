import { useEffect, useState } from 'react';
import { Box, Button, MenuItem, Paper, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography } from '@mui/material';
import ImageOutlined from '@mui/icons-material/ImageOutlined';
import PersonOutlineRounded from '@mui/icons-material/PersonOutlineRounded';
import CategoryOutlined from '@mui/icons-material/CategoryOutlined';
import BusinessOutlined from '@mui/icons-material/BusinessOutlined';
import LocalOfferOutlined from '@mui/icons-material/LocalOfferOutlined';
import MusicNoteOutlined from '@mui/icons-material/MusicNoteOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { isAbortError } from './api';
import { ArtworkManagerDialog } from './ArtworkManagerDialog';
import { catalogEntitiesApi } from './catalogEntitiesApi';
import type { CatalogEntity, CatalogEntityPage, EntityView } from './catalogEntitiesApi';
import { ErrorNotice, PageHeading } from './components';
import { MediaIconTile, MediaSearchField, mediaPanelSx, mediaSelectSx } from './MediaPagePrimitives';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const views: { value: EntityView; label: string }[] = [{ value: 'Person', label: 'People' }, { value: 'Genre', label: 'Genres' }, { value: 'Studio', label: 'Studios' }, { value: 'Tag', label: 'Tags' }, { value: 'MusicArtist', label: 'Music artists' }, { value: 'AlbumArtist', label: 'Album artists' }, { value: 'MusicGenre', label: 'Music genres' }];

function entityTypeName(type: string) {
  return ({ Person: 'Person', Genre: 'Genre', Studio: 'Studio', Tag: 'Tag', MusicArtist: 'Music artist', AlbumArtist: 'Album artist', MusicGenre: 'Music genre' } as Record<string, string>)[type] ?? type;
}

function EntityIdentity({ item }: { item: CatalogEntity }) {
  const icon = item.Type === 'Person' ? <PersonOutlineRounded /> : item.Type === 'Studio' ? <BusinessOutlined /> : item.Type === 'Tag' ? <LocalOfferOutlined /> : ['MusicArtist', 'AlbumArtist'].includes(item.Type) ? <MusicNoteOutlined /> : <CategoryOutlined />;
  return <Stack direction="row" sx={{ alignItems: 'center', gap: 1.5, minWidth: 0 }}><MediaIconTile size={40} neutral>{icon}</MediaIconTile><Typography variant="body2" sx={{ fontWeight: 500, overflowWrap: 'anywhere' }}>{item.Name}</Typography></Stack>;
}
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
    <Paper variant="outlined" sx={mediaPanelSx}>
      <Box component="form" onSubmit={(event) => { event.preventDefault(); setQuery(search.trim()); setPage(0); }} sx={{ p: 2, display: 'flex', flexWrap: 'wrap', gap: 1.5 }}>
        <TextField select size="small" label="Catalog type" value={view} onChange={(event) => { setView(event.target.value as EntityView); setPage(0); }} sx={{ ...mediaSelectSx, width: { xs: '100%', sm: 190 } }}>{views.map((item) => <MenuItem key={item.value} value={item.value}>{item.label}</MenuItem>)}</TextField>
        <Box sx={{ width: { xs: '100%', sm: 320 } }}><MediaSearchField label="Search names" value={search} onChange={setSearch} disabled={loading} /></Box>
      </Box>
      {error != null && <Box sx={{ px: 3, pb: 3 }}><ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} /></Box>}
      {loading && <Box sx={{ p: 3 }} role="status" aria-label="Loading catalog entries"><Skeleton height={54} /><Skeleton height={54} /><Skeleton height={54} /></Box>}
      {data && data.Items.length === 0 && <Box sx={{ px: 3, pb: 4, textAlign: 'center' }}><ImageOutlined sx={{ color: 'primary.main', fontSize: 36 }} /><Typography component="h2" variant="h4" sx={{ mt: 1 }}>No matching entries</Typography><Typography color="text.secondary" variant="body2" sx={{ mt: 1 }}>Scan a library to populate these entries, or try another search.</Typography></Box>}
      {data && data.Items.length > 0 && <>
        <Box component="ul" aria-label="Catalog entries" sx={{ display: { xs: 'block', md: 'none' }, m: 0, p: 0, listStyle: 'none' }}>
          {data.Items.map((item) => <Box component="li" key={item.Id} sx={{ p: 2, borderTop: 1, borderColor: 'divider' }}><EntityIdentity item={item} /><Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, mt: 1.5 }}><Typography variant="body2" color="text.secondary">{entityTypeName(item.Type)}</Typography><Button size="small" startIcon={<ImageOutlined />} aria-label={`Manage artwork for ${item.Name}`} onClick={() => setSelected(item)}>Manage artwork</Button></Stack></Box>)}
        </Box>
        <TableContainer sx={{ display: { xs: 'none', md: 'block' }, borderRadius: 0 }}><Table aria-label="Catalog entries"><TableHead><TableRow><TableCell sx={{ width: '56%' }}>Name</TableCell><TableCell>Type</TableCell><TableCell align="right">Images</TableCell></TableRow></TableHead><TableBody>{data.Items.map((item) => <TableRow key={item.Id}><TableCell component="th" scope="row" sx={{ py: 1.5 }}><EntityIdentity item={item} /></TableCell><TableCell sx={{ whiteSpace: 'nowrap' }}>{entityTypeName(item.Type)}</TableCell><TableCell align="right" sx={{ whiteSpace: 'nowrap' }}><Button size="small" startIcon={<ImageOutlined />} aria-label={`Manage artwork for ${item.Name}`} onClick={() => setSelected(item)}>Manage artwork</Button></TableCell></TableRow>)}</TableBody></Table></TableContainer>
      </>}
      {data && <TablePagination component="div" count={data.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} onPageChange={(_, value) => setPage(value)} disabled={loading} sx={{ borderTop: 1, borderColor: 'divider' }} />}
    </Paper>
    {selected && <ArtworkManagerDialog target={{ kind: 'entities', id: selected.Id, name: selected.Name }} onClose={() => setSelected(undefined)} onNavigationGuardChange={onNavigationGuardChange} />}
  </Box>;
}
