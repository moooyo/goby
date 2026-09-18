import { useEffect, useState } from 'react';
import { Box, Button, Checkbox, FormControlLabel, MenuItem, Skeleton, Stack, TablePagination, TextField, Typography } from '@mui/material';
import { adminApi, isAbortError } from './api';
import type { Library, MetadataItemsResponse } from './api';
import { ErrorNotice } from './components';

export function CollectionItemPicker({ disabled, selected, onSelectedChange }: { disabled: boolean; selected: string[]; onSelectedChange: (ids: string[]) => void }) {
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [librariesLoading, setLibrariesLoading] = useState(true);
  const [librariesError, setLibrariesError] = useState<unknown>();
  const [libraryId, setLibraryId] = useState('');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState({ term: '', page: 0 });
  const [data, setData] = useState<MetadataItemsResponse>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); setLibrariesLoading(true); setLibrariesError(undefined);
    void adminApi.getLibraries({ signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      setLibraries(result.Items);
      setLibraryId((current) => result.Items.some((library) => library.Id === current) ? current : result.Items[0]?.Id ?? '');
      if (!result.Items.length) { setData(undefined); setError(undefined); setLoading(false); }
    }).catch((cause) => {
      if (!controller.signal.aborted && !isAbortError(cause)) { setLibrariesError(cause); setLoading(false); }
    }).finally(() => { if (!controller.signal.aborted) setLibrariesLoading(false); });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => {
    if (!libraryId) return;
    const controller = new AbortController(); setLoading(true); setData(undefined); setError(undefined);
    void adminApi.getLibraryItems(libraryId, { SearchTerm: query.term || undefined, StartIndex: query.page * 25, Limit: 25 }, { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) { if (query.page > 0 && query.page * 25 >= result.TotalRecordCount) setQuery((current) => ({ ...current, page: Math.max(0, Math.ceil(result.TotalRecordCount / 25) - 1) })); else setData(result); } })
      .catch((cause) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [libraryId, query, revision]);
  return <Stack spacing={2}>
    {librariesError != null && <ErrorNotice error={librariesError} retry={() => setRevision((value) => value + 1)} />}
    {error != null && <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />}
    <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
      <TextField select fullWidth label="Source library" value={libraryId} disabled={disabled || !libraries.length} onChange={(event) => { setLibraryId(event.target.value); setQuery({ term: '', page: 0 }); setSearch(''); }}>{libraries.map((library) => <MenuItem key={library.Id} value={library.Id}>{library.Name}</MenuItem>)}</TextField>
      <TextField fullWidth label="Find media" value={search} disabled={disabled} onChange={(event) => setSearch(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') { event.preventDefault(); setQuery({ term: search.trim(), page: 0 }); } }} />
      <Button disabled={disabled || loading || !libraryId} onClick={() => setQuery({ term: search.trim(), page: 0 })}>Search</Button>
    </Stack>
    {(loading || librariesLoading) && <Skeleton variant="rounded" height={100} />}
    {!librariesLoading && librariesError == null && libraries.length === 0 && <Typography color="text.secondary">Create and scan a library before adding media.</Typography>}
    {data && <Box sx={{ maxHeight: 310, overflow: 'auto', border: 1, borderColor: 'divider', borderRadius: 1 }}>{data.Items.map((item) => <Box key={item.Id} sx={{ px: 1.5, py: 0.5, borderBottom: 1, borderColor: 'divider' }}><FormControlLabel control={<Checkbox disabled={disabled || (!selected.includes(item.Id) && selected.length >= 1000)} checked={selected.includes(item.Id)} onChange={(event) => onSelectedChange(event.target.checked ? [...selected, item.Id] : selected.filter((id) => id !== item.Id))} />} label={<Box><Typography variant="body2" sx={{ fontWeight: 600 }}>{item.Name}</Typography><Typography variant="caption" color="text.secondary">{item.Type}{item.ParentName ? ` · ${item.ParentName}` : ''}{item.ProductionYear ? ` · ${item.ProductionYear}` : ''}</Typography></Box>} /></Box>)}{data.Items.length === 0 && <Typography sx={{ p: 2 }} color="text.secondary">No matching media.</Typography>}</Box>}
    {data && <TablePagination component="div" count={data.TotalRecordCount} page={query.page} rowsPerPage={25} rowsPerPageOptions={[25]} disabled={disabled || loading} onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))} sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', px: 0 }, '& .MuiTablePagination-spacer': { display: 'none' } }} />}
    {selected.length > 0 && <Stack direction="row" sx={{ gap: 1, alignItems: 'center' }}><Typography variant="body2">{selected.length} selected across libraries</Typography><Button disabled={disabled} onClick={() => onSelectedChange([])}>Clear selection</Button></Stack>}
  </Stack>;
}
