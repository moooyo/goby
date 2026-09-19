import { useEffect, useRef, useState } from 'react';
import { Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, List, ListItemButton, ListItemIcon, ListItemText, Skeleton, Stack, TextField, Typography } from '@mui/material';
import FolderOutlined from '@mui/icons-material/FolderOutlined';
import ArrowUpwardRounded from '@mui/icons-material/ArrowUpwardRounded';
import { isAbortError } from './api';
import { ErrorNotice } from './components';
import { libraryManagementApi } from './libraryManagementApi';
import type { DirectoryPage } from './libraryManagementApi';

export function DirectoryPickerDialog({ onClose, onChoose }: { onClose: () => void; onChoose: (path: string) => void }) {
  const [path, setPath] = useState('');
  const [manualPath, setManualPath] = useState('');
  const [start, setStart] = useState(0);
  const [page, setPage] = useState<DirectoryPage>();
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [revision, setRevision] = useState(0);
  const inFlight = useRef(false);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setPage(undefined); setError(null);
    void libraryManagementApi.directories(path, start, { signal: controller.signal }).then((result) => { if (!controller.signal.aborted) setPage(result); })
      .catch((cause: unknown) => { if (!isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [path, start, revision]);
  function open(next: string) { setPath(next); setManualPath(next); setStart(0); }
  async function choose(value: string) {
    if (inFlight.current || !value.trim()) return;
    inFlight.current = true; setBusy(true); setError(null);
    try { onChoose(await libraryManagementApi.validate(value.trim())); }
    catch (cause) { if (!isAbortError(cause)) setError(cause); }
    finally { inFlight.current = false; setBusy(false); }
  }
  return <Dialog open fullWidth maxWidth="sm" onClose={busy ? undefined : onClose} aria-labelledby="directory-picker-title">
    <DialogTitle id="directory-picker-title">Choose a media directory</DialogTitle>
    <DialogContent aria-busy={loading || busy}><Stack spacing={2} sx={{ pt: 1 }}>
      <Typography variant="body2" color="text.secondary">Browse directories allowed by this server. The selected directory is checked before it is added to your draft.</Typography>
      <Stack direction="row" spacing={1}><TextField autoFocus fullWidth label="Directory path" value={manualPath} onChange={(event) => setManualPath(event.target.value)} disabled={busy} slotProps={{ htmlInput: { spellCheck: false, autoCapitalize: 'none' } }} /><Button onClick={() => open(manualPath.trim())} disabled={busy || loading || !manualPath.startsWith('/')}>Browse</Button></Stack>
      {error != null && <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />}
      <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}><Button size="small" onClick={() => open('')} disabled={busy || loading}>Allowed roots</Button>{page?.ParentPath && <Button size="small" startIcon={<ArrowUpwardRounded />} disabled={busy || loading} onClick={() => open(page.ParentPath!)}>Parent</Button>}</Stack>
      <Typography variant="body2" sx={{ overflowWrap: 'anywhere', fontFamily: 'monospace' }}>{page?.Path || 'Allowed media directories'}</Typography>
      {loading && <Skeleton variant="rounded" height={180} aria-label="Loading directories" />}
      {page && <Box sx={{ border: 1, borderColor: 'divider', borderRadius: 2, maxHeight: 300, overflow: 'auto' }}><List aria-label="Subdirectories" disablePadding>{page.Items.map((item) => <ListItemButton key={item.Path} disabled={busy} onClick={() => open(item.Path)}><ListItemIcon sx={{ minWidth: 36 }}><FolderOutlined /></ListItemIcon><ListItemText primary={item.Name} secondary={item.Path} sx={{ overflowWrap: 'anywhere' }} /></ListItemButton>)}</List>{page.Items.length === 0 && <Typography color="text.secondary" variant="body2" sx={{ p: 2 }}>No subdirectories are available here.</Typography>}</Box>}
      {page && page.TotalRecordCount > 100 && <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center' }}><Button disabled={busy || loading || start === 0} onClick={() => setStart(Math.max(0, start - 100))}>Previous</Button><Typography variant="caption">{start + 1}–{start + page.Items.length} of {page.TotalRecordCount}</Typography><Button disabled={busy || loading || start + page.Items.length >= page.TotalRecordCount} onClick={() => setStart(start + 100)}>Next</Button></Stack>}
    </Stack></DialogContent>
    <DialogActions sx={{ p: 3 }}><Button color="secondary" disabled={busy} onClick={onClose}>Cancel</Button><Button variant="contained" disabled={busy || !manualPath.startsWith('/')} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <FolderOutlined />} onClick={() => void choose(manualPath)}>{busy ? 'Checking directory...' : 'Use directory'}</Button></DialogActions>
  </Dialog>;
}
