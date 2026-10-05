import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, IconButton, ListItemIcon, Menu, MenuItem, Paper, Skeleton, Snackbar, Stack, TextField, Tooltip, Typography } from '@mui/material';
import AddRounded from '@mui/icons-material/AddRounded';
import DeleteOutlineRounded from '@mui/icons-material/DeleteOutlineRounded';
import FolderOpenOutlined from '@mui/icons-material/FolderOpenOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import RestartAltRounded from '@mui/icons-material/RestartAltRounded';
import VideoLibraryOutlined from '@mui/icons-material/VideoLibraryOutlined';
import PlaylistAddCheckRounded from '@mui/icons-material/PlaylistAddCheckRounded';
import ListAltRounded from '@mui/icons-material/ListAltRounded';
import EditOutlined from '@mui/icons-material/EditOutlined';
import MoreVertRounded from '@mui/icons-material/MoreVertRounded';
import MovieOutlined from '@mui/icons-material/MovieOutlined';
import LiveTvOutlined from '@mui/icons-material/LiveTvOutlined';
import LibraryMusicOutlined from '@mui/icons-material/LibraryMusicOutlined';
import PermMediaOutlined from '@mui/icons-material/PermMediaOutlined';
import StorageOutlined from '@mui/icons-material/StorageOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library, LibraryInput, LibraryResponse, LibrariesResponse, StorageRootsResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError } from './formFields';
import { RootBindingDialog } from './RootBindingDialog';
import { LibraryEditorDialog } from './LibraryEditorDialog';
import { DirectoryPickerDialog } from './DirectoryPickerDialog';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { MediaIconTile, mediaPanelSx, mediaSelected, mediaSurface } from './MediaPagePrimitives';

const collectionTypes: { value: LibraryInput['CollectionType']; label: string }[] = [
  { value: 'movies', label: 'Movies' },
  { value: 'tvshows', label: 'TV shows' },
  { value: 'music', label: 'Music' },
  { value: 'mixed', label: 'Mixed media' },
];

function collectionName(value: string) {
  return collectionTypes.find((type) => type.value === value)?.label ?? value;
}

function scanDate(value: string | null, dateOnly = false) {
  if (!value) return 'Not scanned yet';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleString(undefined, { dateStyle: 'medium', ...(dateOnly ? {} : { timeStyle: 'short' as const }) });
}

function StorageRoots({ roots, compact = false }: { roots: StorageRootsResponse; compact?: boolean }) {
  if (!roots.Configured || roots.Items.length === 0) {
    return <Alert severity="warning">No media directories are configured. Configure the allowed media paths in your server deployment, then refresh this page.</Alert>;
  }
  return (
    <Box>
      {compact && <><Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>Configured media directories</Typography><Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1.5 }}>Library paths must be inside these directories on the server.</Typography></>}
      <Stack spacing={1}>
        {roots.Items.map((root) => (
          <Stack key={root.Path} direction="row" sx={{ gap: 1.5, alignItems: 'center', bgcolor: 'background.paper', px: 2, py: 1.2, borderRadius: '12px' }}>
            <StorageOutlined sx={{ color: 'text.secondary', fontSize: 18, flexShrink: 0 }} />
            <Typography variant="body2" title={root.Path} sx={{ fontFamily: '"JetBrains Mono Variable", Consolas, monospace', fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0, flex: 1 }}>{root.Path}</Typography>
            <Chip size="small" color={root.Available ? 'success' : 'warning'} label={root.Available ? 'Available' : 'Unavailable'} />
          </Stack>
        ))}
      </Stack>
      {roots.Items.every((root) => !root.Available) && <Alert severity="warning" sx={{ mt: 2 }}>The configured directories are unavailable. Check the server mounts and read permissions, then refresh.</Alert>}
    </Box>
  );
}

function CreateLibraryDialog({ roots, onClose, onCreated, onNavigationGuardChange }: { roots: StorageRootsResponse; onClose: () => void; onCreated: (result: LibraryResponse) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [name, setName] = useState('');
  const [type, setType] = useState<LibraryInput['CollectionType']>('movies');
  const [pathsText, setPathsText] = useState('');
  const [scan, setScan] = useState(true);
  const [options, setOptions] = useState({ EnableLocalMetadata: true, EnableLocalImages: true, EnableEmbeddedArtwork: true, EnableIntroDetection: false, EnableCreditsDetection: false, EnablePreviewGeneration: false, EnableBackgroundPreviewGeneration: false, EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [outcomeUnknown, setOutcomeUnknown] = useState(false);
  const [browsing, setBrowsing] = useState(false);
  const dirty = Boolean(name || pathsText || type !== 'movies' || !scan || !options.EnableLocalMetadata || !options.EnableLocalImages || !options.EnableEmbeddedArtwork || options.EnableIntroDetection || options.EnableCreditsDetection || options.EnablePreviewGeneration || options.EnableBackgroundPreviewGeneration || options.EnableAudioWaveformGeneration || options.EnableSubtitleTimelineGeneration);
  useUserDraftNavigation(dirty && !outcomeUnknown, busy, onNavigationGuardChange, 'Discard the new library draft and leave this page?');
  function close() { if (!busy && (outcomeUnknown || !dirty || window.confirm('Discard the new library draft?'))) onClose(); }
  const paths = [...new Set(pathsText.split(/\r?\n/).map((path) => path.trim()).filter(Boolean))];
  const invalidPaths = paths.some((path) => !path.startsWith('/'));
  const pathsMessage = fieldError(error, 'Paths') ?? (invalidPaths ? 'Use an absolute Linux path beginning with / for each directory.' : 'Enter one absolute Linux directory path per line.');

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || outcomeUnknown || invalidPaths || paths.length === 0) return;
    setBusy(true);
    setError(null);
    try {
      const result = await adminApi.createLibrary({ Name: name.trim(), CollectionType: type, Paths: paths, Scan: scan, LibraryOptions: options });
      onCreated(result);
    } catch (cause) {
      if (cause instanceof ApiError && ['network_error', 'invalid_response'].includes(cause.code)) setOutcomeUnknown(true);
      else if (!isAbortError(cause)) setError(cause);
    } finally {
      setBusy(false);
    }
  }

  return (
    <><Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="create-library-title" slotProps={{ paper: { sx: { maxWidth: 560 } } }}>
      <Box component="form" onSubmit={submit} aria-busy={busy}>
        <DialogTitle id="create-library-title" sx={{ px: 3, pt: 3, pb: 0.5 }}>Create library</DialogTitle>
        <DialogContent sx={{ px: 3, pt: '12px !important' }}>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>Group media directories into a library for your connected clients.</Typography>
          <Stack spacing={2.5}>
            {error != null && <ErrorNotice error={error} />}
            {outcomeUnknown && <Alert severity="warning">The server response could not be confirmed. This library may already have been created. Check the library list before trying again.</Alert>}
            <TextField id="library-name" name="Name" autoFocus required fullWidth label="Library name" value={name} onChange={(event) => setName(event.target.value)} disabled={busy} error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name')} />
            <TextField id="library-type" name="CollectionType" select required fullWidth label="Content type" value={type} onChange={(event) => { const next = event.target.value as LibraryInput['CollectionType']; setType(next); setOptions((current) => ({ ...current, EnableIntroDetection: next === 'tvshows' && current.EnableIntroDetection, EnableCreditsDetection: next !== 'music' && current.EnableCreditsDetection, EnablePreviewGeneration: next !== 'music' && current.EnablePreviewGeneration, EnableBackgroundPreviewGeneration: next !== 'music' && current.EnableBackgroundPreviewGeneration, EnableAudioWaveformGeneration: next !== 'music' && current.EnableAudioWaveformGeneration, EnableSubtitleTimelineGeneration: next !== 'music' && current.EnableSubtitleTimelineGeneration })); }} disabled={busy} error={Boolean(fieldError(error, 'CollectionType'))} helperText={fieldError(error, 'CollectionType')}>
              {collectionTypes.map((option) => <MenuItem key={option.value} value={option.value}>{option.label}</MenuItem>)}
            </TextField>
            {type === 'tvshows' && <Paper sx={{ p: 2, borderRadius: '16px' }}><FormControlLabel control={<Checkbox checked={options.EnableIntroDetection} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableIntroDetection: event.target.checked })} />} label="Automatic intro detection" /><Typography variant="body2" color="text.secondary">Analyze episode intros in the background and use reliable matches for playback. If no intro is found, playback stays unchanged. Progress and errors are available in Tasks.</Typography></Paper>}
            {type !== 'music' && <Paper sx={{ p: 2, borderRadius: '16px' }}><FormControlLabel control={<Checkbox checked={options.EnableCreditsDetection} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableCreditsDetection: event.target.checked })} />} label="自动识别片尾" /><Typography variant="body2" color="text.secondary">在影片尾部分析音频重复片段和画面；人工标记优先，未识别到结果时不改变播放。进度与错误在任务中心查看。</Typography></Paper>}
            {type !== 'music' && <Paper sx={{ p: 2, borderRadius: '16px' }}><FormControlLabel control={<Checkbox checked={options.EnablePreviewGeneration} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnablePreviewGeneration: event.target.checked })} />} label="Automatic seek previews" /><Typography variant="body2" color="text.secondary">Generate preview images in the background for seeking in compatible players. Progress and errors are available in Tasks. Turning this off keeps existing valid previews.</Typography></Paper>}
            {type !== 'music' && <Paper sx={{ p: 2, borderRadius: '16px' }}><FormControlLabel control={<Checkbox checked={options.EnableBackgroundPreviewGeneration} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableBackgroundPreviewGeneration: event.target.checked })} />} label="自动生成背景短片" /><Typography variant="body2" color="text.secondary">默认关闭。生成的短片保存在片源旁，需要媒体目录可写。关闭生成不会删除已有短片，已有成品只有明确重新生成时才替换。可在任务中心查看进度和错误。</Typography></Paper>}
            {type !== 'music' && <Paper sx={{ p: 2, borderRadius: '16px' }}><FormControlLabel control={<Checkbox checked={options.EnableAudioWaveformGeneration} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableAudioWaveformGeneration: event.target.checked })} />} label="自动生成音轨波形" /><Typography variant="body2" color="text.secondary">默认关闭。为每条音轨分别生成波形，并随片源持久保存，需要媒体目录可写。普通任务复用已有成品，只有明确重新生成时才替换。过期成品仍会保留，但播放时不显示；关闭生成不会删除已有波形。</Typography></Paper>}
            {type !== 'music' && <Paper sx={{ p: 2, borderRadius: '16px' }}><FormControlLabel control={<Checkbox checked={options.EnableSubtitleTimelineGeneration} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableSubtitleTimelineGeneration: event.target.checked })} />} label="自动生成字幕时间轴" /><Typography variant="body2" color="text.secondary">默认关闭。为内封 PGS/DVD 字幕生成显示区间，并随片源持久保存。普通任务复用已有成品，只有明确重新生成时才替换；需要媒体目录可写。播放器只显示有效时间轴，其余状态仅在任务中心查看，字幕播放不受影响。</Typography></Paper>}
            <Box><TextField id="library-paths" name="Paths" required fullWidth multiline minRows={2} maxRows={6} label="Media directories" value={pathsText} onChange={(event) => setPathsText(event.target.value)} disabled={busy} error={invalidPaths || Boolean(fieldError(error, 'Paths'))} helperText={pathsMessage} slotProps={{ htmlInput: { spellCheck: false, autoCapitalize: 'none' } }} sx={{ '& textarea': { fontFamily: '"JetBrains Mono Variable", Consolas, monospace', fontSize: 13 } }} />
            <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 0.5 }}><Button type="button" size="small" startIcon={<FolderOpenOutlined />} onClick={() => setBrowsing(true)} disabled={busy || outcomeUnknown}>Browse directories</Button></Box></Box>
            <Paper sx={{ p: 2, bgcolor: 'background.paper', borderRadius: '16px' }}><StorageRoots roots={roots} compact /></Paper>
            <Box component="section" aria-label="Library options"><Stack spacing={1}>
              <FormControlLabel control={<Checkbox checked={options.EnableLocalMetadata} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableLocalMetadata: event.target.checked })} />} label="Import local metadata files" />
              <FormControlLabel control={<Checkbox checked={options.EnableLocalImages} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableLocalImages: event.target.checked })} />} label="Import local artwork" />
              <FormControlLabel control={<Checkbox checked={options.EnableEmbeddedArtwork} disabled={busy || outcomeUnknown || !options.EnableLocalImages} onChange={(event) => setOptions({ ...options, EnableEmbeddedArtwork: event.target.checked })} />} label="Extract embedded audio artwork" />
              <Typography variant="caption" color="text.secondary">Embedded audio covers require both local artwork import and extraction to be enabled. These options control future scans. Disabling an option keeps information already imported.</Typography>
              {fieldError(error, 'LibraryOptions') && <Typography variant="body2" color="error.main">{fieldError(error, 'LibraryOptions')}</Typography>}
            </Stack></Box>
            <FormControlLabel control={<Checkbox checked={scan} onChange={(event) => setScan(event.target.checked)} disabled={busy} />} label={<Box><Typography variant="body2" sx={{ fontWeight: 600 }}>Scan after creating</Typography><Typography variant="caption" color="text.secondary">Find media files and add them to the catalog.</Typography></Box>} />
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, pt: 1 }}>
          <Button color="secondary" onClick={close} disabled={busy}>{outcomeUnknown ? 'Close' : 'Cancel'}</Button>
          {outcomeUnknown ? <Button variant="contained" onClick={onClose} startIcon={<RefreshRounded />}>Check libraries</Button> : <Button type="submit" variant="contained" disabled={busy || !name.trim() || paths.length === 0 || invalidPaths} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <AddRounded />}>{busy ? 'Creating library...' : 'Create library'}</Button>}
        </DialogActions>
      </Box>
    </Dialog>{browsing && <DirectoryPickerDialog onClose={() => setBrowsing(false)} onChoose={(path) => { setPathsText([...new Set([...paths, path])].join('\n')); setBrowsing(false); }} />}</>
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

export function LibrariesPage({ onTasks, onBackgroundTasks = onTasks, onManageItems, onNavigationGuardChange }: { onTasks: () => void; onBackgroundTasks?: () => void; onManageItems: (library: Library) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
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
  const [editingLibrary, setEditingLibrary] = useState<Library>();
  const [unconfirmedRefreshes, setUnconfirmedRefreshes] = useState<Set<string>>(new Set());
  const [scanning, setScanning] = useState<Set<string>>(new Set());
  const [notice, setNotice] = useState<{ message: string; taskLink: boolean; background?: boolean }>();
  const [menu, setMenu] = useState<{ anchor: HTMLElement; library: Library }>();

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
      const introEnabled = result.Library.CollectionType === 'tvshows' && result.Library.LibraryOptions?.EnableIntroDetection === true;
      const previewsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnablePreviewGeneration === true;
      const backgroundPreviewsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableBackgroundPreviewGeneration === true;
      const audioWaveformsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableAudioWaveformGeneration === true; const subtitleTimelinesEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableSubtitleTimelineGeneration === true;
      const creditsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableCreditsDetection === true;
      const backgroundEnabled = introEnabled || creditsEnabled || previewsEnabled || backgroundPreviewsEnabled || audioWaveformsEnabled || subtitleTimelinesEnabled;
      setNotice({ message: `Library ${result.Library.Name} created.${result.Job ? ' A scan has been requested.' : ''}${introEnabled ? ' Automatic intro detection is enabled.' : ''}${creditsEnabled ? ' 已启用自动识别片尾。' : ''}${previewsEnabled ? ' Automatic seek previews are enabled.' : ''}${backgroundPreviewsEnabled ? ' 已启用自动生成背景短片。' : ''}${audioWaveformsEnabled ? ' 已启用自动生成音轨波形。' : ''}${subtitleTimelinesEnabled ? ' 已启用自动生成字幕时间轴。' : ''}${backgroundEnabled ? ' 可在任务中心查看后台处理进度。' : ''}`, taskLink: Boolean(result.Job) || backgroundEnabled, background: backgroundEnabled });
    }
    refresh();
  }

  return (
    <Box aria-busy={loading}>
      <PageHeading title="Libraries" description="Choose the media directories your server keeps in its catalog. Scans and media details refreshes run in the background." action={<Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}><Button variant="outlined" startIcon={<PlaylistAddCheckRounded />} onClick={onTasks}>View tasks</Button><Button variant="contained" startIcon={<AddRounded />} onClick={() => setCreating(true)} disabled={!canCreate}>Create library</Button></Stack>} />
      <Stack spacing={2.5}>
        {error != null && <ErrorNotice error={error} retry={refresh} />}
        {actionError != null && <ErrorNotice error={actionError} />}
        <Paper component="section" sx={{ p: 2.5, bgcolor: mediaSurface, borderRadius: '20px', display: 'grid', gridTemplateColumns: { xs: '1fr', md: '250px minmax(0, 1fr)' }, gap: 3 }}>
          <Box>
            <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', mb: 0.5 }}><Typography component="h2" variant="h4">Storage access</Typography><Tooltip title="Refresh storage access"><IconButton size="small" aria-label="Refresh storage access" disabled={loading} onClick={refresh}><RefreshRounded sx={{ fontSize: 18 }} /></IconButton></Tooltip></Stack>
            <Typography variant="body2" color="text.secondary">Library paths must be inside the configured media directories on this server. Deployment settings control this access.</Typography>
          </Box>
          <Box sx={{ minWidth: 0 }}>
            {rootsError != null && <ErrorNotice error={rootsError} retry={refresh} />}
            {!roots && loading && <Skeleton variant="rounded" height={62} aria-label="Loading configured media directories" />}
            {roots && <StorageRoots roots={roots} />}
          </Box>
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
            <Box component="ul" aria-label="Media libraries" sx={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 400px), 1fr))', gap: 2, listStyle: 'none', p: 0, m: 0 }}>
              {libraries.Items.map((library) => (
                <Paper component="li" key={library.Id} variant="outlined" sx={{ ...mediaPanelSx, p: 2.5, display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <Stack direction="row" sx={{ alignItems: 'center', gap: 1.5, minWidth: 0 }}>
                    <MediaIconTile size={48}>{library.CollectionType === 'movies' ? <MovieOutlined /> : library.CollectionType === 'tvshows' ? <LiveTvOutlined /> : library.CollectionType === 'music' ? <LibraryMusicOutlined /> : <PermMediaOutlined />}</MediaIconTile>
                    <Box sx={{ minWidth: 0, flex: 1 }}><Typography component="h3" sx={{ fontSize: 16, fontWeight: 600, overflowWrap: 'anywhere' }}>{library.Name}</Typography><Typography variant="body2" color="text.secondary">{collectionName(library.CollectionType)}</Typography></Box>
                    <Button variant="outlined" size="small" startIcon={scanning.has(library.Id) ? <CircularProgress size={16} color="inherit" /> : <RefreshRounded />} onClick={() => void scanLibrary(library)} aria-label={`Scan library ${library.Name}`} disabled={scanning.has(library.Id) || unconfirmedRefreshes.has(library.Id)} sx={{ minHeight: 36, px: 1.5 }}>{scanning.has(library.Id) ? 'Starting...' : 'Scan'}</Button>
                    <IconButton aria-label={`More actions for ${library.Name}`} aria-haspopup="menu" aria-controls={menu?.library.Id === library.Id ? 'library-actions-menu' : undefined} aria-expanded={menu?.library.Id === library.Id ? true : undefined} onClick={(event) => setMenu({ anchor: event.currentTarget, library })} sx={{ ml: -0.5, mr: -1 }}><MoreVertRounded /></IconButton>
                  </Stack>
                  {unconfirmedRefreshes.has(library.Id) && <Alert severity="warning" action={<Button color="inherit" size="small" onClick={onTasks}>View tasks</Button>}>A media details refresh request could not be confirmed. Check Tasks before starting another scan or refresh.</Alert>}
                  <Stack spacing={0.75} sx={{ p: 1.5, bgcolor: mediaSurface, borderRadius: '12px' }}>
                    {library.Paths.map((path) => <Stack key={path} direction="row" sx={{ alignItems: 'center', gap: 1 }}><FolderOpenOutlined sx={{ fontSize: 18, color: 'text.secondary', flexShrink: 0 }} /><Typography variant="body2" title={path} sx={{ fontFamily: '"JetBrains Mono Variable", Consolas, monospace', fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0 }}>{path}</Typography></Stack>)}
                  </Stack>
                  <Stack direction="row" sx={{ gap: 1.5, alignItems: 'center', justifyContent: 'space-between', mt: 'auto' }}>
                    <Typography variant="caption" color="text.secondary" sx={{ minWidth: 0 }}>Last scan <Box component="span" sx={{ color: 'text.primary' }}>{scanDate(library.LastScanAt)}</Box> · Created {scanDate(library.CreatedAt, true)}</Typography>
                    <Button size="small" startIcon={<ListAltRounded />} onClick={() => onManageItems(library)} aria-label={`Manage items in ${library.Name}`} sx={{ flexShrink: 0, minHeight: 36, px: 1.5, bgcolor: mediaSelected, color: '#0F2A57', '&:hover': { bgcolor: '#C8D8F2' } }}>Manage items</Button>
                  </Stack>
                </Paper>
              ))}
            </Box>
          </Box>
        )}
        <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ justifyContent: 'space-between', alignItems: { xs: 'flex-start', sm: 'center' }, gap: 1, borderTop: 1, borderColor: 'divider', pt: 2 }}><Typography variant="body2" color="text.secondary">扫描、媒体信息刷新、片头检测、片尾识别、进度预览、背景短片和音轨波形生成均在后台运行。可在任务中心查看进度和错误，或停止正在运行的任务。</Typography><Button onClick={onBackgroundTasks} startIcon={<PlaylistAddCheckRounded />} sx={{ flexShrink: 0 }}>View tasks</Button></Stack>
      </Stack>
      <Menu id="library-actions-menu" anchorEl={menu?.anchor} open={Boolean(menu)} onClose={() => setMenu(undefined)} slotProps={{ paper: { sx: { minWidth: 228, borderRadius: '12px', bgcolor: '#EEF2F9', '& .MuiMenuItem-root': { minHeight: 44 } } } }}>
        <MenuItem onClick={() => { setEditingLibrary(menu?.library); setMenu(undefined); }}><ListItemIcon><EditOutlined fontSize="small" /></ListItemIcon>Edit library</MenuItem>
        <MenuItem onClick={() => { setBindingLibrary(menu?.library); setMenu(undefined); }}><ListItemIcon><FolderOpenOutlined fontSize="small" /></ListItemIcon>Storage bindings</MenuItem>
        <MenuItem disabled={Boolean(menu && scanning.has(menu.library.Id))} onClick={() => { setRefreshingMedia(menu?.library); setMenu(undefined); }}><ListItemIcon><RestartAltRounded fontSize="small" /></ListItemIcon>Refresh media details</MenuItem>
        <Divider />
        <MenuItem disabled={Boolean(menu && scanning.has(menu.library.Id))} onClick={() => { setDeleting(menu?.library); setMenu(undefined); }} sx={{ color: 'error.main' }}><ListItemIcon sx={{ color: 'inherit' }}><DeleteOutlineRounded fontSize="small" /></ListItemIcon>Delete library</MenuItem>
      </Menu>
      {creating && roots && <CreateLibraryDialog roots={roots} onClose={() => { setCreating(false); refresh(); }} onCreated={libraryCreated} onNavigationGuardChange={onNavigationGuardChange} />}
      {deleting && <DeleteLibraryDialog library={deleting} onClose={() => setDeleting(undefined)} onDeleted={() => { setNotice({ message: `Library ${deleting.Name} deleted. Media files were kept.`, taskLink: false }); setDeleting(undefined); refresh(); }} />}
      {refreshingMedia && <RefreshMediaDialog library={refreshingMedia} outcomeUnknown={unconfirmedRefreshes.has(refreshingMedia.Id)} onUnknown={() => setUnconfirmedRefreshes((ids) => new Set(ids).add(refreshingMedia.Id))} onClose={() => setRefreshingMedia(undefined)} onStarted={() => { setNotice({ message: `Media details refresh requested for ${refreshingMedia.Name}.`, taskLink: true }); setRefreshingMedia(undefined); }} onTasks={onTasks} />}
      {bindingLibrary && <RootBindingDialog key={bindingLibrary.Id} library={bindingLibrary} onClose={() => setBindingLibrary(undefined)} onNavigationGuardChange={onNavigationGuardChange} />}
      {editingLibrary && <LibraryEditorDialog key={editingLibrary.Id} libraryId={editingLibrary.Id} onClose={() => setEditingLibrary(undefined)} onSaved={(result) => { setEditingLibrary(undefined); refresh(); const introEnabled = result.Library.CollectionType === 'tvshows' && result.Library.LibraryOptions?.EnableIntroDetection === true; const creditsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableCreditsDetection === true; const previewsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnablePreviewGeneration === true; const backgroundPreviewsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableBackgroundPreviewGeneration === true; const audioWaveformsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableAudioWaveformGeneration === true; const subtitleTimelinesEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableSubtitleTimelineGeneration === true; const backgroundEnabled = introEnabled || creditsEnabled || previewsEnabled || backgroundPreviewsEnabled || audioWaveformsEnabled || subtitleTimelinesEnabled; if (result.ScanError) setActionError(new Error(`Library saved, but the scan could not start. ${result.ScanError.Message}`)); else setNotice({ message: `Library ${result.Library.Name} saved.${introEnabled ? ' Automatic intro detection is enabled.' : ''}${creditsEnabled ? ' 已启用自动识别片尾。' : ''}${previewsEnabled ? ' Automatic seek previews are enabled.' : ''}${backgroundPreviewsEnabled ? ' 已启用自动生成背景短片。' : ''}${audioWaveformsEnabled ? ' 已启用自动生成音轨波形。' : ''}${subtitleTimelinesEnabled ? ' 已启用自动生成字幕时间轴。' : ''}${backgroundEnabled ? ' 可在任务中心查看后台处理进度。' : ''}`, taskLink: Boolean(result.Job) || backgroundEnabled, background: backgroundEnabled }); }} onNavigationGuardChange={onNavigationGuardChange} />}
      <Snackbar open={Boolean(notice)} autoHideDuration={notice?.taskLink ? 10000 : 6000} onClose={() => setNotice(undefined)} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity="success" variant="filled" action={notice?.taskLink ? <Button color="inherit" size="small" onClick={notice.background ? onBackgroundTasks : onTasks}>View tasks</Button> : undefined} onClose={() => setNotice(undefined)}>{notice?.message}</Alert></Snackbar>
    </Box>
  );
}
