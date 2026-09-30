import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Box, Button, Chip, IconButton, Paper, Skeleton, Stack, Tab, TablePagination, Tabs, Tooltip, Typography } from '@mui/material';
import AddRounded from '@mui/icons-material/AddRounded';
import CheckRounded from '@mui/icons-material/CheckRounded';
import CollectionsBookmarkRounded from '@mui/icons-material/CollectionsBookmarkRounded';
import LockOutlined from '@mui/icons-material/LockOutlined';
import PublicRounded from '@mui/icons-material/PublicRounded';
import QueueMusicRounded from '@mui/icons-material/QueueMusicRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { isAbortError } from './api';
import { collectionsApi } from './collectionsApi';
import type { CollectionKind, CollectionPage, ManagedCollection } from './collectionsApi';
import { CollectionDialog } from './CollectionDialog';
import { ErrorNotice, PageHeading } from './components';
import { MediaIconTile, MediaSearchField, mediaPanelSx, mediaSelected } from './MediaPagePrimitives';
import { colors } from './theme';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const metadataChipSx = {
  height: 24,
  borderRadius: '8px',
  borderColor: colors.fog,
  bgcolor: 'transparent',
  color: 'text.secondary',
  fontSize: 12,
  fontWeight: 400,
  '& .MuiChip-label': { px: 1 },
  '& .MuiChip-icon': { color: 'text.secondary', fontSize: 14, ml: 0.75, mr: -0.25 },
} as const;

export function CollectionsPage({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [kind, setKind] = useState<CollectionKind>('playlists');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState({ term: '', page: 0, limit: 25 });
  const [data, setData] = useState<CollectionPage<ManagedCollection>>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const [editing, setEditing] = useState<{ id?: string }>();
  const label = kind === 'playlists' ? 'playlist' : 'collection';
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(undefined); setData(undefined);
    void collectionsApi.list(kind, query.page * query.limit, query.limit, query.term, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return;
      if (query.page > 0 && query.page * query.limit >= value.TotalRecordCount) setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(value.TotalRecordCount / query.limit) - 1) }));
      else setData(value);
    }).catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [kind, query, revision]);
  const refresh = () => setRevision((value) => value + 1);
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setQuery((current) => ({ ...current, term: search.trim(), page: 0 })); }
  return <Box aria-busy={loading}>
    <PageHeading title="Playlists and collections" description="Manage curated media groups, ordered playlists, and their sharing permissions." action={<Button variant="contained" startIcon={<AddRounded />} onClick={() => setEditing({})}>Create {label}</Button>} />
    <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { xs: 'stretch', sm: 'center' }, justifyContent: 'space-between', gap: 1.5, mb: 2 }}>
      <Tabs
        value={kind}
        onChange={(_event, value: CollectionKind) => { setKind(value); setSearch(''); setQuery((current) => ({ ...current, term: '', page: 0 })); }}
        aria-label="Media group types"
        sx={{
          alignSelf: 'flex-start', minHeight: 40, border: `1px solid ${colors.outline}`, borderRadius: '20px',
          '& .MuiTabs-indicator': { display: 'none' },
          '& .MuiTab-root': { minHeight: 38, py: 0.75, px: 2.25, fontSize: 13, lineHeight: '20px', '& + .MuiTab-root': { borderLeft: `1px solid ${colors.outline}` }, '&.Mui-selected': { bgcolor: mediaSelected, color: colors.deep } },
          '& .MuiTab-icon': { fontSize: 18, mr: 0.75 },
          '& .MuiTab-root.Mui-focusVisible': { outline: `2px solid ${colors.primary}`, outlineOffset: -3 },
        }}
      >
        <Tab value="playlists" label="Playlists" icon={kind === 'playlists' ? <CheckRounded /> : undefined} iconPosition="start" id="playlists-tab" aria-controls="collections-list-panel" />
        <Tab value="collections" label="Collections" icon={kind === 'collections' ? <CheckRounded /> : undefined} iconPosition="start" id="collections-tab" aria-controls="collections-list-panel" />
      </Tabs>
      <Box component="form" onSubmit={submit} sx={{ width: { xs: '100%', sm: 300 }, maxWidth: '100%' }}>
        <MediaSearchField label={`Search ${kind}`} value={search} onChange={setSearch} />
      </Box>
    </Stack>
    <Paper variant="outlined" sx={mediaPanelSx} role="tabpanel" id="collections-list-panel" aria-labelledby={kind === 'playlists' ? 'playlists-tab' : 'collections-tab'}>
      {error != null && <Box sx={{ p: 2 }}><ErrorNotice error={error} retry={refresh} /></Box>}
      {loading && <Stack spacing={0} aria-label={`Loading ${kind}`}>
        {[0, 1, 2, 3].map((row) => <Stack key={row} direction="row" sx={{ gap: 2, alignItems: 'center', px: 3, py: 1.75, borderBottom: '1px solid #EEF1F7' }}>
          <Skeleton variant="rounded" width={44} height={44} sx={{ flexShrink: 0, borderRadius: '12px' }} />
          <Box sx={{ flex: 1 }}><Skeleton width="35%" height={20} /><Skeleton width="50%" height={24} sx={{ mt: 0.75 }} /></Box>
          <Skeleton variant="rounded" width={82} height={36} sx={{ borderRadius: '18px' }} />
        </Stack>)}
      </Stack>}
      {data?.Items.length === 0 && <Stack sx={{ alignItems: 'center', textAlign: 'center', gap: 1.5, px: 3, py: 5 }}>
        <MediaIconTile>{kind === 'playlists' ? <QueueMusicRounded /> : <CollectionsBookmarkRounded />}</MediaIconTile>
        <Typography color="text.secondary">{query.term ? `No matching ${kind}. Try another name.` : `No ${kind} yet. Create one to group media from your libraries.`}</Typography>
      </Stack>}
      {data && data.Items.length > 0 && <Box component="ul" aria-label={kind === 'playlists' ? 'Playlists' : 'Collections'} sx={{ m: 0, p: 0, listStyle: 'none' }}>
        {data.Items.map((entry) => <Box component="li" key={entry.Id} sx={{ display: 'grid', gridTemplateColumns: { xs: '44px minmax(0, 1fr)', sm: '44px minmax(0, 1fr) auto' }, gap: 2, alignItems: 'center', pl: { xs: 2, sm: 3 }, pr: 2, py: 1.75, borderBottom: '1px solid #EEF1F7', '&:hover': { bgcolor: '#F7F9FD' } }}>
          <MediaIconTile>{kind === 'playlists' ? <QueueMusicRounded /> : <CollectionsBookmarkRounded />}</MediaIconTile>
          <Box sx={{ minWidth: 0 }}>
            <Typography component="h2" sx={{ fontSize: 14, lineHeight: '20px', fontWeight: 600, overflowWrap: 'anywhere' }}>{entry.Name}</Typography>
            <Stack direction="row" sx={{ gap: 0.75, mt: 0.75, flexWrap: 'wrap' }}>
              <Chip size="small" variant="outlined" label={`${entry.ChildCount} items`} sx={metadataChipSx} />
              <Chip size="small" variant="outlined" icon={entry.IsPublic ? <PublicRounded /> : <LockOutlined />} label={entry.IsPublic ? 'Public' : 'Private'} sx={metadataChipSx} />
              {entry.MediaType && <Chip size="small" variant="outlined" label={entry.MediaType} sx={metadataChipSx} />}
              {entry.IsLocked && <Chip size="small" color="warning" label="Locked" sx={{ height: 24, borderRadius: '8px', fontSize: 12, fontWeight: 600 }} />}
            </Stack>
          </Box>
          <Button variant="outlined" size="small" aria-label={`Manage ${entry.Name}`} onClick={() => setEditing({ id: entry.Id })} sx={{ gridColumn: { xs: 2, sm: 'auto' }, justifySelf: 'end', minHeight: { xs: 44, sm: 36 }, borderColor: colors.outline, fontSize: 13 }}>Manage</Button>
        </Box>)}
      </Box>}
      {data && <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, px: 1.5, py: 0.5 }}>
        <Tooltip title={`Refresh ${kind}`}><span><IconButton aria-label="Refresh" disabled={loading} onClick={refresh}><RefreshRounded sx={{ fontSize: 20 }} /></IconButton></span></Tooltip>
        <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.limit} rowsPerPageOptions={[25, 50, 100]} disabled={loading} onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))} onRowsPerPageChange={(event) => setQuery((current) => ({ ...current, limit: Number(event.target.value), page: 0 }))} sx={{ maxWidth: '100%', ml: 'auto', '& .MuiTablePagination-toolbar': { minHeight: 40, flexWrap: 'wrap', justifyContent: 'flex-end', px: 0, columnGap: 1.5 }, '& .MuiTablePagination-spacer': { display: 'none' }, '& .MuiTablePagination-selectLabel, & .MuiTablePagination-displayedRows': { fontSize: 12, my: 1 }, '& .MuiTablePagination-input': { mx: 0 }, '& .MuiTablePagination-actions': { ml: 0 } }} />
      </Stack>}
    </Paper>
    {editing && <CollectionDialog key={`${kind}:${editing.id ?? 'new'}`} kind={kind} collectionId={editing.id} onClose={() => { setEditing(undefined); refresh(); }} onSaved={refresh} onNavigationGuardChange={onNavigationGuardChange} />}
  </Box>;
}
