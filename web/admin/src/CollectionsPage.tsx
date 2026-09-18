import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Box, Button, Chip, Paper, Skeleton, Stack, Tab, TablePagination, Tabs, TextField, Typography } from '@mui/material';
import AddRounded from '@mui/icons-material/AddRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { isAbortError } from './api';
import { collectionsApi } from './collectionsApi';
import type { CollectionKind, CollectionPage, ManagedCollection } from './collectionsApi';
import { CollectionDialog } from './CollectionDialog';
import { ErrorNotice, PageHeading } from './components';
import type { UserNavigationGuardChange } from './userDraftNavigation';

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
    <Tabs value={kind} onChange={(_event, value: CollectionKind) => { setKind(value); setSearch(''); setQuery((current) => ({ ...current, term: '', page: 0 })); }} aria-label="Media group types" sx={{ mb: 3, borderBottom: 1, borderColor: 'divider' }}><Tab value="playlists" label="Playlists" id="playlists-tab" aria-controls="collections-list-panel" /><Tab value="collections" label="Collections" id="collections-tab" aria-controls="collections-list-panel" /></Tabs>
    <Paper variant="outlined" sx={{ p: { xs: 2, sm: 3 } }} role="tabpanel" id="collections-list-panel" aria-labelledby={kind === 'playlists' ? 'playlists-tab' : 'collections-tab'}>
      <Stack spacing={2.5}>
        <Stack component="form" onSubmit={submit} direction={{ xs: 'column', sm: 'row' }} spacing={2}><TextField fullWidth label={`Search ${kind}`} value={search} onChange={(event) => setSearch(event.target.value)} /><Button type="submit" disabled={loading}>Search</Button><Button disabled={loading} startIcon={<RefreshRounded />} onClick={refresh}>Refresh</Button></Stack>
        {error != null && <ErrorNotice error={error} retry={refresh} />}
        {loading && <Skeleton variant="rounded" height={180} />}
        {data?.Items.length === 0 && <Typography color="text.secondary">{query.term ? `No matching ${kind}. Try another name.` : `No ${kind} yet. Create one to group media from your libraries.`}</Typography>}
        {data?.Items.map((entry) => <Box key={entry.Id} sx={{ py: 2, borderTop: 1, borderColor: 'divider' }}><Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', gap: 2, alignItems: { xs: 'flex-start', sm: 'center' } }}><Box sx={{ minWidth: 0 }}><Typography variant="h4" component="h2" sx={{ overflowWrap: 'anywhere' }}>{entry.Name}</Typography><Stack direction="row" sx={{ gap: 1, mt: 1, flexWrap: 'wrap' }}><Chip size="small" variant="outlined" label={`${entry.ChildCount} items`} /><Chip size="small" variant="outlined" label={entry.IsPublic ? 'Public' : 'Private'} />{entry.IsLocked && <Chip size="small" variant="outlined" color="warning" label="Locked" />}{entry.MediaType && <Chip size="small" variant="outlined" label={entry.MediaType} />}</Stack></Box><Button variant="outlined" aria-label={`Manage ${entry.Name}`} onClick={() => setEditing({ id: entry.Id })}>Manage</Button></Stack></Box>)}
        {data && <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={query.limit} rowsPerPageOptions={[25, 50, 100]} disabled={loading} onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))} onRowsPerPageChange={(event) => setQuery((current) => ({ ...current, limit: Number(event.target.value), page: 0 }))} sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', px: 0 }, '& .MuiTablePagination-spacer': { display: 'none' } }} />}
      </Stack>
    </Paper>
    {editing && <CollectionDialog key={`${kind}:${editing.id ?? 'new'}`} kind={kind} collectionId={editing.id} onClose={() => { setEditing(undefined); refresh(); }} onSaved={refresh} onNavigationGuardChange={onNavigationGuardChange} />}
  </Box>;
}
