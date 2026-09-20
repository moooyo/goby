import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, MenuItem, Paper, Skeleton, Stack, TablePagination, TextField, Typography } from '@mui/material';
import { adminApi, isAbortError } from './api';
import type { Library } from './api';
import { ErrorNotice } from './components';
import { getDeletionFolders } from './deletionFoldersApi';
import type { DeletionFolder, DeletionFolders } from './deletionFoldersApi';

function FolderPicker({ selected, disabled, onSelect, onClose }: { selected: string[]; disabled: boolean; onSelect: (item: DeletionFolder, checked: boolean) => void; onClose: () => void }) {
  const [search, setSearch] = useState(''); const [query, setQuery] = useState({ SearchTerm: '', LibraryId: '', StartIndex: 0, Limit: 50 });
  const [result, setResult] = useState<DeletionFolders>(); const [libraries, setLibraries] = useState<Library[]>();
  const [loading, setLoading] = useState(true); const [error, setError] = useState<unknown>(null); const [libraryError, setLibraryError] = useState<unknown>(null); const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void adminApi.getLibraries({ signal: controller.signal }).then((value) => { if (!controller.signal.aborted) setLibraries(value.Items); }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setLibraryError(cause); });
    return () => controller.abort();
  }, [revision]);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setResult(undefined); setError(null);
    void getDeletionFolders(query, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return;
      if (query.StartIndex > 0 && query.StartIndex >= value.TotalRecordCount) { setQuery((current) => ({ ...current, StartIndex: 0 })); return; }
      setResult(value);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [query, revision]);
  function searchFolders(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setQuery((value) => ({ ...value, SearchTerm: search.trim(), StartIndex: 0 })); }
  return <Dialog open fullWidth maxWidth="md" onClose={onClose} aria-labelledby="deletion-folder-picker-title"><DialogTitle id="deletion-folder-picker-title">Choose deletion folders</DialogTitle><DialogContent><Stack spacing={2} sx={{ pt: 1 }}>
    <Typography variant="body2" color="text.secondary">Select current libraries or indexed folders. Changes remain in the account draft until you save the user.</Typography>
    <Box component="form" onSubmit={searchFolders}><Stack direction={{ xs: 'column', sm: 'row' }} spacing={1.5}>
      <TextField fullWidth label="Search deletion folders" value={search} onChange={(event) => setSearch(event.target.value)} disabled={disabled} slotProps={{ htmlInput: { maxLength: 256 } }} />
      <TextField fullWidth select label="Deletion folder library" value={query.LibraryId} disabled={disabled || !libraries} onChange={(event) => setQuery((value) => ({ ...value, LibraryId: event.target.value, StartIndex: 0 }))}><MenuItem value="">All libraries</MenuItem>{libraries?.map((library) => <MenuItem key={library.Id} value={library.Id}>{library.Name}</MenuItem>)}</TextField>
      <Button type="submit" variant="outlined" disabled={disabled || loading}>Search folders</Button>
    </Stack></Box>
    {error != null && <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />}
    {libraryError != null && <Alert severity="warning">Library filters are unavailable. You can still search the complete folder list.</Alert>}
    {loading && <Skeleton variant="rounded" height={180} aria-label="Loading deletion folders" />}
    {result && <><Box component="ul" aria-label="Deletion folder choices" sx={{ listStyle: 'none', p: 0, m: 0 }}>{result.Items.map((item) => <Paper component="li" key={item.Id} variant="outlined" sx={{ p: 1.5, mb: 1 }}><FormControlLabel sx={{ m: 0, alignItems: 'flex-start', width: '100%' }} control={<Checkbox checked={selected.includes(item.Id)} disabled={disabled || (!selected.includes(item.Id) && selected.length >= 256)} onChange={(event) => onSelect(item, event.target.checked)} slotProps={{ input: { 'aria-label': `Allow deletion in ${item.Name || item.Id}` } }} />} label={<Box sx={{ pt: 0.75, minWidth: 0 }}><Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>{item.Name || item.Id}</Typography><Typography variant="caption" color="text.secondary" sx={{ display: 'block', overflowWrap: 'anywhere' }}>{item.LibraryName} · {item.Type === 'CollectionFolder' ? 'Library' : item.Type}</Typography><Typography variant="caption" color="text.secondary" sx={{ display: 'block', overflowWrap: 'anywhere' }}>{item.Path || item.Id}</Typography></Box>} /></Paper>)}</Box>
      {result.Items.length === 0 && <Typography variant="body2" color="text.secondary">No matching current libraries or folders.</Typography>}
      <TablePagination component="div" count={result.TotalRecordCount} page={query.StartIndex / query.Limit} rowsPerPage={query.Limit} rowsPerPageOptions={[50]} onPageChange={(_event, page) => setQuery((value) => ({ ...value, StartIndex: page * value.Limit }))} disabled={loading || disabled} />
    </>}
  </Stack></DialogContent><DialogActions sx={{ p: 3 }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{selected.length} selected grants</Typography><Button onClick={onClose}>Done choosing folders</Button></DialogActions></Dialog>;
}

export function DeletionFoldersField({ value, disabled, error, onChange }: { value: string[]; disabled: boolean; error?: string; onChange: (value: string[]) => void }) {
  const [open, setOpen] = useState(false); const [names, setNames] = useState<Record<string, string>>({});
  return <Box component="section" aria-label="Folders allowing media deletion"><Typography variant="body2" sx={{ fontWeight: 650, mb: 1 }}>Folders allowing media deletion</Typography><Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>Choose current library or folder entries. Existing identifiers are retained until you remove them. These grants are retained when media deletion is disabled.</Typography>
    <Stack spacing={1}>{value.map((id) => <Paper key={id} variant="outlined" sx={{ p: 1.5 }}><Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}><Box sx={{ minWidth: 0 }}><Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>{names[id] ?? 'Saved folder grant'}</Typography><Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>{id}</Typography></Box><Button color="error" disabled={disabled} aria-label={`Remove deletion grant ${id}`} onClick={() => onChange(value.filter((entry) => entry !== id))}>Remove</Button></Stack></Paper>)}</Stack>
    {value.length === 0 && <Typography variant="caption" color="text.secondary">No folder-specific grants selected.</Typography>}
    {error && <Typography color="error" variant="body2" sx={{ mt: 1 }}>{error}</Typography>}
    <Button variant="outlined" disabled={disabled} onClick={() => setOpen(true)} sx={{ mt: 1.5 }}>Choose deletion folders</Button>
    {open && <FolderPicker selected={value} disabled={disabled} onClose={() => setOpen(false)} onSelect={(item, checked) => { setNames((current) => ({ ...current, [item.Id]: item.Name })); onChange(checked ? [...value, item.Id] : value.filter((id) => id !== item.Id)); }} />}
  </Box>;
}
