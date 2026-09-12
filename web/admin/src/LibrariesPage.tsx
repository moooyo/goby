import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, MenuItem, Paper, Skeleton, Snackbar, Stack, TextField, Typography } from '@mui/material';
import AddRounded from '@mui/icons-material/AddRounded';
import DeleteOutlineRounded from '@mui/icons-material/DeleteOutlineRounded';
import FolderOpenOutlined from '@mui/icons-material/FolderOpenOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import RestartAltRounded from '@mui/icons-material/RestartAltRounded';
import VideoLibraryOutlined from '@mui/icons-material/VideoLibraryOutlined';
import PlaylistAddCheckRounded from '@mui/icons-material/PlaylistAddCheckRounded';
import ListAltRounded from '@mui/icons-material/ListAltRounded';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library, LibraryInput, LibraryResponse, LibrariesResponse, StorageRootsResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError } from './formFields';
import { RootBindingDialog } from './RootBindingDialog';

const collectionTypes: { value: LibraryInput['CollectionType']; label: string }[] = [
  { value: 'movies', label: 'Movies' },
  { value: 'tvshows', label: 'TV shows' },
  { value: 'music', label: 'Music' },
  { value: 'mixed', label: 'Mixed media' },
];

function collectionName(value: string) {
  return collectionTypes.find((type) => type.value === value)?.label ?? value;
}

function scanDate(value: string | null) {
  if (!value) return 'Not scanned yet';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

function StorageRoots({ roots }: { roots: StorageRootsResponse }) {
  if (!roots.Configured || roots.Items.length === 0) {
    return <Alert severity="warning">No media directories are configured. Configure the allowed media paths in your server deployment, then refresh this page.</Alert>;
  }
  return (
    <Box>
      <Typography variant="body2" sx={{ fontWeight: 650, mb: 0.5 }}>Configured media directories</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>Library paths must be inside these directories on the server.</Typography>
      <Stack spacing={1}>
        {roots.Items.map((root) => (
          <Stack key={root.Path} direction="row" sx={{ gap: 1, justifyContent: 'space-between', alignItems: 'flex-start' }}>
            <Typography variant="body2" sx={{ fontFamily: 'ui-monospace, Consolas, monospace', overflowWrap: 'anywhere', minWidth: 0 }}>{root.Path}</Typography>
            <Chip size="small" variant="outlined" color={root.Available ? 'success' : 'warning'} label={root.Available ? 'Available' : 'Unavailable'} />
          </Stack>
        ))}
      </Stack>
      {roots.Items.every((root) => !root.Available) && <Alert severity="warning" sx={{ mt: 2 }}>The configured directories are unavailable. Check the server mounts and read permissions, then refresh.</Alert>}
    </Box>
  );
}

function CreateLibraryDialog({ roots, onClose, onCreated }: { roots: StorageRootsResponse; onClose: () => void; onCreated: (result: LibraryResponse) => void }) {
  const [name, setName] = useState('');
  const [type, setType] = useState<LibraryInput['CollectionType']>('movies');
  const [pathsText, setPathsText] = useState('');
  const [scan, setScan] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [outcomeUnknown, setOutcomeUnknown] = useState(false);
  const paths = [...new Set(pathsText.split(/\r?\n/).map((path) => path.trim()).filter(Boolean))];
  const invalidPaths = paths.some((path) => !path.startsWith('/'));
  const pathsMessage = fieldError(error, 'Paths') ?? (invalidPaths ? 'Use an absolute Linux path beginning with / for each directory.' : 'Enter one absolute Linux directory path per line.');

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || outcomeUnknown || invalidPaths || paths.length === 0) return;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.createLibrary({ Name: name.trim(), CollectionType: type, Paths: paths, Scan: scan });
      onCreated(result);
    } catch (cause) {
      if (cause instanceof ApiError && ['network_error', 'invalid_response'].includes(cause.code)) setOutcomeUnknown(true);
      else if (!isAbortError(cause)) setError(cause);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open onClose={busy ? undefined : onClose} fullWidth maxWidth="sm" aria-labelledby="create-library-title">
      <Box component="form" onSubmit={submit} aria-busy={busy}>
        <DialogTitle id="create-library-title" sx={{ px: 3, pt: 3, pb: 0.5 }}><Typography component="span" variant="h3">Create library</Typography></DialogTitle>
        <DialogContent sx={{ px: 3, pt: '12px !important' }}>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>Group media directories into a library for your connected clients.</Typography>
          <Stack spacing={2.5}>
            {error != null && <ErrorNotice error={error} />}
            {outcomeUnknown && <Alert severity="warning">The server response could not be confirmed. This library may already have been created. Check the library list before trying again.</Alert>}
            <TextField id="library-name" name="Name" autoFocus required fullWidth label="Library name" value={name} onChange={(event) => setName(event.target.value)} disabled={busy} error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name')} />
            <TextField id="library-type" name="CollectionType" select required fullWidth label="Content type" value={type} onChange={(event) => setType(event.target.value as LibraryInput['CollectionType'])} disabled={busy} error={Boolean(fieldError(error, 'CollectionType'))} helperText={fieldError(error, 'CollectionType')}>
              {collectionTypes.map((option) => <MenuItem key={option.value} value={option.value}>{option.label}</MenuItem>)}
            </TextField>
            <TextField id="library-paths" name="Paths" required fullWidth multiline minRows={3} maxRows={6} label="Media directories" value={pathsText} onChange={(event) => setPathsText(event.target.value)} disabled={busy} error={invalidPaths || Boolean(fieldError(error, 'Paths'))} helperText={pathsMessage} slotProps={{ htmlInput: { spellCheck: false, autoCapitalize: 'none' } }} sx={{ '& textarea': { fontFamily: 'ui-monospace, Consolas, monospace', fontSize: 13 } }} />
            <Paper variant="outlined" sx={{ p: 2, bgcolor: 'background.default' }}><StorageRoots roots={roots} /></Paper>
            <FormControlLabel control={<Checkbox checked={scan} onChange={(event) => setScan(event.target.checked)} disabled={busy} />} label={<Box><Typography variant="body2" sx={{ fontWeight: 600 }}>Scan after creating</Typography><Typography variant="caption" color="text.secondary">Find media files and add them to the catalog.</Typography></Box>} />
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, pt: 1 }}>
          <Button color="secondary" onClick={onClose} disabled={busy}>{outcomeUnknown ? 'Close' : 'Cancel'}</Button>
          {outcomeUnknown ? <Button variant="contained" onClick={onClose} startIcon={<RefreshRounded />}>Check libraries</Button> : <Button type="submit" variant="contained" disabled={busy || !name.trim() || paths.length === 0 || invalidPaths} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <AddRounded />}>{busy ? 'Creating library...' : 'Create library'}</Button>}
        </DialogActions>
      </Box>
    </Dialog>
  );
}

function DeleteLibraryDialog({ library, onClose, onDeleted }: { library: Library; onClose: () => void; onDeleted: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  async function remove() {
    if (busy) return;
    setBusy(true);
    setError(null);
    try {
      await adminApi.deleteLibrary(library.Id);
      onDeleted();
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 404) onDeleted();
      else if (!isAbortError(cause)) setError(cause);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open onClose={busy ? undefined : onClose} fullWidth maxWidth="xs" aria-labelledby="delete-library-title" aria-describedby="delete-library-description">
      <DialogTitle id="delete-library-title">Delete library?</DialogTitle>
      <DialogContent>
        <Typography sx={{ fontWeight: 650, overflowWrap: 'anywhere', mb: 2 }}>{library.Name}</Typography>
        <Typography id="delete-library-description" color="text.secondary">This removes the library and its catalog records from Goby. Media files on disk will not be deleted.</Typography>
        {error != null && <Box sx={{ mt: 2 }}><ErrorNotice error={error} /></Box>}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5 }}><Button onClick={onClose} disabled={busy} color="secondary">Keep library</Button><Button color="error" variant="contained" onClick={remove} disabled={busy} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <DeleteOutlineRounded />}>{busy ? 'Deleting...' : 'Delete library'}</Button></DialogActions>
    </Dialog>
  );
}

function RefreshMediaDialog({ library, outcomeUnknown, onUnknown, onClose, onStarted, onTasks }: { library: Library; outcomeUnknown: boolean; onUnknown: () => void; onClose: () => void; onStarted: () => void; onTasks: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);

  async function start() {
    if (busy || outcomeUnknown) return;
    setBusy(true);
    setError(null);
    try {
      await adminApi.refreshLibraryMedia(library.Id);
      onStarted();
    } catch (cause) {
      if (cause instanceof ApiError && ['network_error', 'invalid_response'].includes(cause.code)) onUnknown();
      else if (!isAbortError(cause)) setError(cause);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open onClose={busy ? undefined : onClose} fullWidth maxWidth="xs" aria-labelledby="refresh-media-title" aria-describedby="refresh-media-description">
      <DialogTitle id="refresh-media-title">Refresh media details?</DialogTitle>
      <DialogContent aria-busy={busy}>
        <Typography sx={{ fontWeight: 650, overflowWrap: 'anywhere', mb: 2 }}>{library.Name}</Typography>
        <Stack id="refresh-media-description" spacing={2}>
          <Typography color="text.secondary">This re-reads every media file in this library and rebuilds playback indexes for supported formats. It can take longer than a normal scan.</Typography>
          <Typography color="text.secondary">Your edited metadata and play history are kept.</Typography>
          <Typography variant="body2" color="text.secondary">Some formats do not support playback indexes, even when the refresh completes.</Typography>
        </Stack>
        {error != null && <Box sx={{ mt: 2 }}><ErrorNotice error={error} /></Box>}
        {outcomeUnknown && <Alert severity="warning" sx={{ mt: 2 }}>The server response could not be confirmed. This refresh may already be running. View Tasks and check this library before requesting another refresh.</Alert>}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5, flexWrap: 'wrap', gap: 1 }}>
        <Button autoFocus color="secondary" onClick={onClose} disabled={busy}>{outcomeUnknown ? 'Close' : 'Cancel'}</Button>
        {outcomeUnknown
          ? <Button variant="contained" onClick={onTasks} startIcon={<PlaylistAddCheckRounded />}>View tasks</Button>
          : <Button variant="contained" onClick={() => void start()} disabled={busy} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <RestartAltRounded />}>{busy ? 'Requesting...' : 'Refresh media details'}</Button>}
      </DialogActions>
    </Dialog>
  );
}

export function LibrariesPage({ onTasks, onManageItems }: { onTasks: () => void; onManageItems: (library: Library) => void }) {
  const [libraries, setLibraries] = useState<LibrariesResponse>();
  const [roots, setRoots] = useState<StorageRootsResponse>();
  const [error, setError] = useState<unknown>(null);
  const [rootsError, setRootsError] = useState<unknown>(null);
  const [actionError, setActionError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<Library>();
  const [refreshingMedia, setRefreshingMedia] = useState<Library>();
  const [bindingLibrary, setBindingLibrary] = useState<Library>();
  const [unconfirmedRefreshes, setUnconfirmedRefreshes] = useState<Set<string>>(new Set());
  const [scanning, setScanning] = useState<Set<string>>(new Set());
  const [notice, setNotice] = useState<{ message: string; taskLink: boolean }>();

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    setRootsError(null);
    void Promise.allSettled([
      adminApi.getLibraries({ signal: controller.signal }).then(setLibraries).catch((cause: unknown) => { if (!isAbortError(cause)) setError(cause); }),
      adminApi.getStorageRoots({ signal: controller.signal }).then(setRoots).catch((cause: unknown) => { if (!isAbortError(cause)) setRootsError(cause); }),
    ]).then(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);

  const refresh = () => setRevision((value) => value + 1);
  const canCreate = !loading && rootsError == null && roots?.Configured && roots.Items.some((root) => root.Available);

  async function scanLibrary(library: Library) {
    if (scanning.has(library.Id) || unconfirmedRefreshes.has(library.Id)) return;
    setScanning((ids) => new Set(ids).add(library.Id));
    setActionError(null);
    try {
      await adminApi.scanLibrary(library.Id);
      setNotice({ message: `Scan requested for ${library.Name}.`, taskLink: true });
    } catch (cause) {
      if (!isAbortError(cause)) setActionError(cause);
    } finally {
      setScanning((ids) => { const next = new Set(ids); next.delete(library.Id); return next; });
    }
  }

  function libraryCreated(result: LibraryResponse) {
    setCreating(false);
    if (result.ScanError) {
      setNotice(undefined);
      setActionError(new Error(`Library ${result.Library.Name} was created, but its first scan could not start. ${result.ScanError.Message} Use Scan library to try again.`));
    } else {
      setActionError(null);
      setNotice({ message: `Library ${result.Library.Name} created.${result.Job ? ' A scan has been requested.' : ''}`, taskLink: Boolean(result.Job) });
    }
    refresh();
  }

  return (
    <Box aria-busy={loading}>
      <PageHeading title="Libraries" description="Choose the media directories your server keeps in its catalog." action={<Button variant="contained" startIcon={<AddRounded />} onClick={() => setCreating(true)} disabled={!canCreate}>Create library</Button>} />
      <Stack spacing={3}>
        {error != null && <ErrorNotice error={error} retry={refresh} />}
        {actionError != null && <ErrorNotice error={actionError} />}
        <Paper variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}>
          <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 2, mb: 2 }}><Typography component="h2" variant="h4">Storage access</Typography><Button size="small" startIcon={<RefreshRounded />} disabled={loading} onClick={refresh}>Refresh</Button></Stack>
          {rootsError != null && <ErrorNotice error={rootsError} retry={refresh} />}
          {!roots && loading && <Skeleton variant="rounded" height={62} aria-label="Loading configured media directories" />}
          {roots && <StorageRoots roots={roots} />}
        </Paper>
        {!libraries && loading && <Stack spacing={2} role="status" aria-label="Loading libraries"><Skeleton variant="rounded" height={180} /><Skeleton variant="rounded" height={180} /></Stack>}
        {libraries && libraries.Items.length === 0 && (
          <Paper variant="outlined" sx={{ p: { xs: 4, sm: 6 }, textAlign: 'center' }}>
            <VideoLibraryOutlined sx={{ fontSize: 42, color: 'primary.main', mb: 2 }} />
            <Typography component="h2" variant="h3">Your catalog starts with a library</Typography>
            <Typography color="text.secondary" sx={{ maxWidth: 450, mx: 'auto', mt: 1, mb: 2.5 }}>{canCreate ? 'Create a library, choose its media directories, and scan to discover your collection.' : 'Once the server has access to a media directory, you can create your first library here.'}</Typography>
            <Button onClick={() => setCreating(true)} variant="outlined" startIcon={<AddRounded />} disabled={!canCreate}>Create library</Button>
          </Paper>
        )}
        {libraries && libraries.Items.length > 0 && (
          <Box>
            <Stack direction="row" sx={{ gap: 1, alignItems: 'center', mb: 2 }}><Typography component="h2" variant="h4">Your libraries</Typography><Chip size="small" label={libraries.TotalRecordCount.toLocaleString()} /></Stack>
            <Stack component="ul" aria-label="Media libraries" spacing={2} sx={{ listStyle: 'none', p: 0, m: 0 }}>
              {libraries.Items.map((library) => (
                <Paper component="li" key={library.Id} variant="outlined" sx={{ p: { xs: 2.5, sm: 3 } }}>
                  <Stack direction={{ xs: 'column', lg: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: 'flex-start', lg: 'center' }, gap: 2 }}>
                    <Stack direction="row" sx={{ gap: 1.5, alignItems: 'center', minWidth: 0 }}>
                      <Box sx={{ bgcolor: '#E8F3F3', color: 'primary.main', borderRadius: 2, p: 1.3, display: 'flex' }}><VideoLibraryOutlined /></Box>
                      <Box sx={{ minWidth: 0 }}><Typography component="h3" variant="h3" sx={{ overflowWrap: 'anywhere' }}>{library.Name}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.3 }}>{collectionName(library.CollectionType)}</Typography></Box>
                    </Stack>
                    <Stack direction="row" sx={{ gap: 1, flexShrink: 0, flexWrap: 'wrap', maxWidth: '100%' }}>
                      <Button variant="contained" size="small" startIcon={<ListAltRounded />} onClick={() => onManageItems(library)} aria-label={`Manage items in ${library.Name}`}>Manage items</Button>
                      <Button size="small" color="secondary" startIcon={<FolderOpenOutlined />} onClick={() => setBindingLibrary(library)} aria-label={`Storage bindings for ${library.Name}`}>Storage bindings</Button>
                      <Button variant="outlined" size="small" startIcon={scanning.has(library.Id) ? <CircularProgress size={16} color="inherit" /> : <RefreshRounded />} onClick={() => void scanLibrary(library)} disabled={scanning.has(library.Id) || unconfirmedRefreshes.has(library.Id)}>{scanning.has(library.Id) ? 'Requesting...' : 'Scan library'}</Button>
                      <Button size="small" color="secondary" startIcon={<RestartAltRounded />} onClick={() => setRefreshingMedia(library)} disabled={scanning.has(library.Id)} aria-label={`Refresh media details for ${library.Name}`}>Refresh media details</Button>
                      <Button size="small" color="secondary" startIcon={<DeleteOutlineRounded />} onClick={() => setDeleting(library)} disabled={scanning.has(library.Id)}>Delete</Button>
                    </Stack>
                  </Stack>
                  {unconfirmedRefreshes.has(library.Id) && <Alert severity="warning" sx={{ mt: 2 }} action={<Button color="inherit" size="small" onClick={onTasks}>View tasks</Button>}>A media details refresh request could not be confirmed. Check Tasks before starting another scan or refresh.</Alert>}
                  <Stack spacing={1} sx={{ mt: 2.5, p: 2, bgcolor: 'background.default', borderRadius: 2 }}>
                    {library.Paths.map((path) => <Stack key={path} direction="row" sx={{ alignItems: 'flex-start', gap: 1 }}><FolderOpenOutlined sx={{ fontSize: 18, color: 'text.secondary', mt: 0.2, flexShrink: 0 }} /><Typography variant="body2" sx={{ fontFamily: 'ui-monospace, Consolas, monospace', overflowWrap: 'anywhere', minWidth: 0 }}>{path}</Typography></Stack>)}
                  </Stack>
                  <Divider sx={{ my: 2 }} />
                  <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ gap: 0.5, justifyContent: 'space-between' }}><Typography variant="body2" color="text.secondary">Last scan: {scanDate(library.LastScanAt)}</Typography><Typography variant="caption" color="text.secondary">Created {scanDate(library.CreatedAt)}</Typography></Stack>
                </Paper>
              ))}
            </Stack>
          </Box>
        )}
        <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'center' }, gap: 1, borderTop: 1, borderColor: 'divider', pt: 2 }}><Typography variant="body2" color="text.secondary">Scans and media details refreshes run in the background. Open Tasks to follow their progress.</Typography><Button onClick={onTasks} startIcon={<PlaylistAddCheckRounded />} sx={{ flexShrink: 0 }}>View tasks</Button></Stack>
      </Stack>
      {creating && roots && <CreateLibraryDialog roots={roots} onClose={() => { setCreating(false); refresh(); }} onCreated={libraryCreated} />}
      {deleting && <DeleteLibraryDialog library={deleting} onClose={() => setDeleting(undefined)} onDeleted={() => { setNotice({ message: `Library ${deleting.Name} deleted. Media files were kept.`, taskLink: false }); setDeleting(undefined); refresh(); }} />}
      {refreshingMedia && <RefreshMediaDialog library={refreshingMedia} outcomeUnknown={unconfirmedRefreshes.has(refreshingMedia.Id)} onUnknown={() => setUnconfirmedRefreshes((ids) => new Set(ids).add(refreshingMedia.Id))} onClose={() => setRefreshingMedia(undefined)} onStarted={() => { setNotice({ message: `Media details refresh requested for ${refreshingMedia.Name}.`, taskLink: true }); setRefreshingMedia(undefined); }} onTasks={onTasks} />}
      {bindingLibrary && <RootBindingDialog key={bindingLibrary.Id} library={bindingLibrary} onClose={() => setBindingLibrary(undefined)} />}
      <Snackbar open={Boolean(notice)} autoHideDuration={notice?.taskLink ? 10000 : 6000} onClose={() => setNotice(undefined)} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity="success" variant="filled" action={notice?.taskLink ? <Button color="inherit" size="small" onClick={onTasks}>View tasks</Button> : undefined} onClose={() => setNotice(undefined)}>{notice?.message}</Alert></Snackbar>
    </Box>
  );
}
