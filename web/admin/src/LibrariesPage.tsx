import { useEffect, useState } from 'react';
import type { FocusEvent, FormEvent } from 'react';
import { Alert, Box, Button, Checkbox, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, IconButton, ListItemIcon, Menu, MenuItem, Paper, Skeleton, Snackbar, Stack, Switch, TextField, Tooltip, Typography } from './ui';
import { AddRounded } from './icons';
import { DeleteOutlineRounded } from './icons';
import { FolderOpenOutlined } from './icons';
import { RefreshRounded } from './icons';
import { RestartAltRounded } from './icons';
import { VideoLibraryOutlined } from './icons';
import { PlaylistAddCheckRounded } from './icons';
import { ListAltRounded } from './icons';
import { EditOutlined } from './icons';
import { MoreVertRounded } from './icons';
import { MovieOutlined } from './icons';
import { LiveTvOutlined } from './icons';
import { LibraryMusicOutlined } from './icons';
import { PermMediaOutlined } from './icons';
import { StorageOutlined } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { Library, LibraryInput, LibraryResponse, LibrariesResponse, StorageRootsResponse } from './api';
import { ErrorNotice, PageHeading } from './components';
import { fieldError } from './formFields';
import { RootBindingDialog } from './RootBindingDialog';
import { LibraryEditorDialog } from './LibraryEditorDialog';
import { OnlineImagesPolicyField } from './OnlineImagesPolicyField';
import type { LibraryOptions } from './libraryOptions';
import { DirectoryPickerDialog } from './DirectoryPickerDialog';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { MediaIconTile, mediaFeatures } from './MediaPagePrimitives';
import { LibraryDirectoriesPanel } from './LibraryDirectoriesPanel';
import { confirmAction, useTranslation } from './i18n';
import './media-pages.css';

const collectionTypes: { value: LibraryInput['CollectionType']; label: string }[] = [
  { value: 'movies', label: 'Movies' },
  { value: 'tvshows', label: 'TV shows' },
  { value: 'music', label: 'Music' },
  { value: 'mixed', label: 'Mixed media' },
];

function collectionName(value: string) {
  return collectionTypes.find((type) => type.value === value)?.label ?? value;
}

function scanDate(value: string | null, locale: string) {
  if (!value) return 'Not scanned yet';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
}

function StorageRoots({ roots }: { roots: StorageRootsResponse }) {
  const { t } = useTranslation();
  if (!roots.Configured || roots.Items.length === 0) {
    return <Alert severity="warning">No media directories are configured. Configure the allowed media paths in your server deployment, then refresh this page.</Alert>;
  }
  return (
    <Box>
      <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>Configured media directories</Typography><Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1.5 }}>Library paths must be inside these directories on the server.</Typography>
      <div className="media-storage-roots" role="list" aria-label={t('Configured media directories')}>
        {roots.Items.map((root) => {
          const readable = root.Available && root.Readable === true;
          const unknown = root.Available && root.Readable == null;
          const writable = readable ? root.Writable : null;
          const accessLabel = unknown ? 'Read access unknown' : !readable ? 'Unavailable' : writable === true ? 'Read and write' : writable === false ? 'Read only' : 'Write access unknown';
          return <div key={root.Path} role="listitem" aria-label={root.Path} className="media-storage-root media-storage-root-compact">
            <StorageOutlined sx={{ color: 'text.secondary', fontSize: 18, flexShrink: 0 }} />
            <Typography translate="no" variant="body2" title={root.Path} sx={{ fontFamily: 'var(--mono)', fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: 0, flex: 1 }}>{root.Path}</Typography>
            <Tooltip title={t(unknown ? 'Read access has not been established.' : readable ? writable === null ? 'The directory is readable. Write access could not be established.' : 'Effective service permissions are checked without modifying files.' : 'The directory could not be read by the service.')}><span><Chip size="small" color={unknown ? 'default' : readable ? writable === null ? 'default' : 'success' : 'warning'} label={t(accessLabel)} /></span></Tooltip>
          </div>;
        })}
      </div>
      {roots.Items.every((root) => !root.Available) && <Alert severity="warning" sx={{ mt: 2 }}>The configured directories are unavailable. Check the server mounts and read permissions, then refresh.</Alert>}
    </Box>
  );
}

export function CreateLibraryDialog({ roots, onClose, onCreated, onNavigationGuardChange, createRequest = adminApi.createLibrary }: { roots: StorageRootsResponse; onClose: (needsRefresh?: boolean) => void; onCreated: (result: LibraryResponse) => void; onNavigationGuardChange: UserNavigationGuardChange; createRequest?: (input: LibraryInput) => Promise<LibraryResponse> }) {
  const { t } = useTranslation();
  const [name, setName] = useState('');
  const [type, setType] = useState<LibraryInput['CollectionType']>('movies');
  const [pathsText, setPathsText] = useState('');
  const [scan, setScan] = useState(true);
  const [options, setOptions] = useState<LibraryOptions>({ EnableLocalMetadata: true, EnableLocalImages: true, EnableEmbeddedArtwork: true, EnableOnlineImages: false, EnableIntroDetection: false, EnableCreditsDetection: false, EnablePreviewGeneration: false, EnableBackgroundPreviewGeneration: false, EnableAudioWaveformGeneration: false, EnableSubtitleTimelineGeneration: false });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [outcomeUnknown, setOutcomeUnknown] = useState(false);
  const [browsing, setBrowsing] = useState(false);
  const dirty = Boolean(name || pathsText || type !== 'movies' || !scan || !options.EnableLocalMetadata || !options.EnableLocalImages || !options.EnableEmbeddedArtwork || options.EnableOnlineImages || options.EnableIntroDetection || options.EnableCreditsDetection || options.EnablePreviewGeneration || options.EnableBackgroundPreviewGeneration || options.EnableAudioWaveformGeneration || options.EnableSubtitleTimelineGeneration);
  useUserDraftNavigation(dirty && !outcomeUnknown, busy, onNavigationGuardChange, 'Discard the new library draft and leave this page?');
  function close() { if (!busy && (outcomeUnknown || !dirty || confirmAction('Discard the new library draft?'))) onClose(outcomeUnknown); }
  const paths = [...new Set(pathsText.split(/\r?\n/).map((path) => path.trim()).filter(Boolean))];
  const invalidPaths = paths.some((path) => !path.startsWith('/'));
  const pathsMessage = fieldError(error, 'Paths') ?? (invalidPaths ? 'Use an absolute Linux path beginning with / for each directory.' : 'Enter one absolute Linux directory path per line.');

  function revealNameContext(event: FocusEvent<HTMLInputElement>) {
    const input = event.currentTarget;
    requestAnimationFrame(() => {
      const dialog = input.closest<HTMLDialogElement>('dialog');
      const content = input.closest<HTMLDivElement>('.goby-dialog-content');
      if (document.activeElement !== input || !dialog?.open || !content) return;
      content.scrollTop = 0;
      dialog.scrollTop = 0;
    });
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || outcomeUnknown || invalidPaths || paths.length === 0) return;
    setBusy(true);
    setError(null);
    try {
      const result = await createRequest({ Name: name.trim(), CollectionType: type, Paths: paths, Scan: scan, LibraryOptions: options });
      onCreated(result);
    } catch (cause) {
      if (cause instanceof ApiError && (cause.status === 0 || cause.status >= 500 || ['network_error', 'invalid_response'].includes(cause.code))) setOutcomeUnknown(true);
      else if (!isAbortError(cause)) setError(cause);
    } finally {
      setBusy(false);
    }
  }

  return (
    <><Dialog open onClose={close} fullWidth maxWidth="sm" aria-labelledby="create-library-title" slotProps={{ paper: { className: 'media-create-library', sx: { maxWidth: 480 } } }}>
      <Box component="form" onSubmit={submit} aria-busy={busy}>
        <DialogTitle id="create-library-title" sx={{ px: 3, pt: 3, pb: 0.5, fontFamily: 'var(--serif)', fontSize: 22, fontWeight: 900, lineHeight: 1.25 }}>Create library</DialogTitle>
        <DialogContent sx={{ px: 3, pt: '12px !important' }}>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>Group media directories into a library for your connected clients.</Typography>
          <Stack spacing={2.5}>
            {error != null && <ErrorNotice error={error} />}
            {outcomeUnknown && <Alert severity="warning">The server response could not be confirmed. This library may already have been created. Check the library list before trying again.</Alert>}
            <TextField id="library-name" name="Name" autoFocus required fullWidth label="Library name" value={name} onFocus={revealNameContext} onChange={(event) => setName(event.target.value)} disabled={busy} error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name')} />
            <TextField id="library-type" name="CollectionType" select required fullWidth label="Content type" value={type} onChange={(event) => { const next = event.target.value as LibraryInput['CollectionType']; setType(next); setOptions((current) => ({ ...current, EnableOnlineImages: next !== 'music' && current.EnableOnlineImages, EnableIntroDetection: next === 'tvshows' && current.EnableIntroDetection, EnableCreditsDetection: next !== 'music' && current.EnableCreditsDetection, EnablePreviewGeneration: next !== 'music' && current.EnablePreviewGeneration, EnableBackgroundPreviewGeneration: next !== 'music' && current.EnableBackgroundPreviewGeneration, EnableAudioWaveformGeneration: next !== 'music' && current.EnableAudioWaveformGeneration, EnableSubtitleTimelineGeneration: next !== 'music' && current.EnableSubtitleTimelineGeneration })); }} disabled={busy} error={Boolean(fieldError(error, 'CollectionType'))} helperText={fieldError(error, 'CollectionType')}>
              {collectionTypes.map((option) => <MenuItem key={option.value} value={option.value}>{option.label}</MenuItem>)}
            </TextField>
            <Box><TextField id="library-paths" name="Paths" required fullWidth multiline minRows={2} maxRows={6} label="Media directories" value={pathsText} onChange={(event) => setPathsText(event.target.value)} disabled={busy} error={invalidPaths || Boolean(fieldError(error, 'Paths'))} helperText={pathsMessage} slotProps={{ htmlInput: { spellCheck: false, autoCapitalize: 'none' } }} sx={{ '& textarea': { fontFamily: 'var(--mono)', fontSize: 13 } }} />
            <Box sx={{ display: 'flex', justifyContent: 'flex-end', mt: 0.5 }}><Button type="button" size="small" startIcon={<FolderOpenOutlined />} onClick={() => setBrowsing(true)} disabled={busy || outcomeUnknown}>Browse directories</Button></Box></Box>
            <Paper sx={{ p: 2, bgcolor: 'background.paper', borderRadius: '16px' }}><StorageRoots roots={roots} /></Paper>
            <Box component="section" aria-label="Library options"><Stack spacing={1}>
              <FormControlLabel control={<Checkbox checked={options.EnableLocalMetadata} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableLocalMetadata: event.target.checked })} />} label="Import local metadata files" />
              <FormControlLabel control={<Checkbox checked={options.EnableLocalImages} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, EnableLocalImages: event.target.checked })} />} label="Import local artwork" />
              <FormControlLabel control={<Checkbox checked={options.EnableEmbeddedArtwork} disabled={busy || outcomeUnknown || !options.EnableLocalImages} onChange={(event) => setOptions({ ...options, EnableEmbeddedArtwork: event.target.checked })} />} label="Extract embedded audio artwork" />
              <Typography variant="caption" color="text.secondary">Embedded audio covers require both local artwork import and extraction to be enabled. These options control future scans. Disabling an option keeps information already imported.</Typography>
              <OnlineImagesPolicyField collectionType={type} options={options} disabled={busy || outcomeUnknown} onChange={setOptions} />
              {fieldError(error, 'LibraryOptions') && <Typography variant="body2" color="error.main">{fieldError(error, 'LibraryOptions')}</Typography>}
            </Stack></Box>
            {type !== 'music' && <section className="media-create-feature-section" aria-labelledby="create-library-features-title"><h3 id="create-library-features-title">{t('Features and analysis')}</h3>{mediaFeatures.filter((feature) => !feature.episodes || type === 'tvshows').map((feature) => <div className="media-create-feature-row" key={feature.key}><div><strong>{t(feature.label)}</strong><span>{t(feature.description)}</span></div><Switch aria-label={t(feature.label)} checked={options[feature.key]} disabled={busy || outcomeUnknown} onChange={(event) => setOptions({ ...options, [feature.key]: event.target.checked })} /></div>)}<p>{t('Turning generation off keeps existing results. Replacing results requires an explicit regeneration request.')}</p></section>}
            <FormControlLabel control={<Checkbox checked={scan} onChange={(event) => setScan(event.target.checked)} disabled={busy} />} label={<Box><Typography variant="body2" sx={{ fontWeight: 600 }}>Scan after creating</Typography><Typography variant="caption" color="text.secondary">Find media files and add them to the catalog.</Typography></Box>} />
          </Stack>
        </DialogContent>
        <DialogActions sx={{ px: 3, pb: 3, pt: 1 }}>
          <Button color="secondary" onClick={close} disabled={busy}>{outcomeUnknown ? 'Close' : 'Cancel'}</Button>
          {outcomeUnknown ? <Button variant="contained" onClick={() => onClose(true)} startIcon={<RefreshRounded />}>Check libraries</Button> : <Button type="submit" variant="contained" disabled={busy || !name.trim() || paths.length === 0 || invalidPaths} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <AddRounded />}>{busy ? 'Creating library...' : 'Create library'}</Button>}
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
    <Dialog open role="alertdialog" onClose={busy ? undefined : onClose} fullWidth maxWidth="xs" aria-labelledby="delete-library-title" aria-describedby="delete-library-description">
      <DialogTitle id="delete-library-title">Delete library?</DialogTitle>
      <DialogContent>
        <Typography translate="no" sx={{ fontWeight: 650, overflowWrap: 'anywhere', mb: 2 }}>{library.Name}</Typography>
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
        <Typography translate="no" sx={{ fontWeight: 650, overflowWrap: 'anywhere', mb: 2 }}>{library.Name}</Typography>
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
  const { t, locale, countLabel, formatCount } = useTranslation();
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
  const [counts, setCounts] = useState<Record<string, number | undefined>>({});

  useEffect(() => {
    if (!libraries) return;
    const controller = new AbortController();
    const queue = [...libraries.Items];
    setCounts({});
    async function readCounts() {
      while (queue.length > 0 && !controller.signal.aborted) {
        const library = queue.shift();
        if (!library) return;
        try {
          const result = await adminApi.getLibraryItems(library.Id, { StartIndex: 0, Limit: 1 }, { signal: controller.signal });
          if (!controller.signal.aborted) setCounts((current) => ({ ...current, [library.Id]: result.TotalRecordCount }));
        } catch { /* The library remains usable when its independent count cannot load. */ }
      }
    }
    void Promise.all(Array.from({ length: Math.min(4, queue.length) }, readCounts));
    return () => controller.abort();
  }, [libraries]);

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

  async function scanAll() {
    if (!libraries || libraries.Items.length === 0 || scanning.size > 0 || unconfirmedRefreshes.size > 0) return;
    setScanning(new Set(libraries.Items.map((library) => library.Id)));
    setActionError(null);
    const results = await Promise.allSettled(libraries.Items.map((library) => adminApi.scanLibrary(library.Id)));
    setScanning(new Set());
    const failures = results.flatMap((result, index) => result.status === 'rejected' ? [`${libraries.Items[index].Name}: ${result.reason instanceof Error ? result.reason.message : t('The request could not be completed.')}`] : []);
    if (failures.length > 0) setActionError(new Error(failures.join('\n')));
    const started = results.filter((result) => result.status === 'fulfilled').length;
    if (started > 0) setNotice({ message: `${t('Scan requested')}: ${formatCount(started, 'library', 'libraries')}.`, taskLink: true });
  }

  function libraryCreated(result: LibraryResponse) {
    setCreating(false);
    const imagesEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableOnlineImages === true;
    const imageNotice = imagesEnabled ? ` ${t('Automatic image downloads are enabled.')} ${t('View image download progress in Tasks.')}` : '';
    if (result.ScanError) {
      setNotice(imagesEnabled ? { message: imageNotice.trim(), taskLink: true, background: true } : undefined);
      setActionError(new Error(`Library ${result.Library.Name} was created, but its first scan could not start. ${result.ScanError.Message} Use Scan library to try again.`));
    } else {
      setActionError(null);
      const introEnabled = result.Library.CollectionType === 'tvshows' && result.Library.LibraryOptions?.EnableIntroDetection === true;
      const previewsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnablePreviewGeneration === true;
      const backgroundPreviewsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableBackgroundPreviewGeneration === true;
      const audioWaveformsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableAudioWaveformGeneration === true; const subtitleTimelinesEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableSubtitleTimelineGeneration === true;
      const creditsEnabled = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType) && result.Library.LibraryOptions?.EnableCreditsDetection === true;
      const backgroundEnabled = introEnabled || creditsEnabled || previewsEnabled || backgroundPreviewsEnabled || audioWaveformsEnabled || subtitleTimelinesEnabled;
      setNotice({ message: `Library ${result.Library.Name} created.${result.Job ? ' A scan has been requested.' : ''}${introEnabled ? ' Automatic intro detection is enabled.' : ''}${creditsEnabled ? ' Automatic credits detection is enabled.' : ''}${previewsEnabled ? ' Automatic seek previews are enabled.' : ''}${backgroundPreviewsEnabled ? ' Automatic background clips are enabled.' : ''}${audioWaveformsEnabled ? ' Automatic audio waveforms are enabled.' : ''}${subtitleTimelinesEnabled ? ' Automatic subtitle timelines are enabled.' : ''}${backgroundEnabled ? ' View background processing progress in Tasks.' : ''}${imageNotice}`, taskLink: Boolean(result.Job) || backgroundEnabled || imagesEnabled, background: backgroundEnabled || imagesEnabled });
    }
    refresh();
  }

  function librarySaved(result: LibraryResponse) {
    setEditingLibrary(undefined);
    refresh();
    const videoLibrary = ['movies', 'tvshows', 'mixed'].includes(result.Library.CollectionType);
    const imagesEnabled = videoLibrary && result.Library.LibraryOptions?.EnableOnlineImages === true;
    const imageNotice = imagesEnabled ? ` ${t('Automatic image downloads are enabled.')} ${t('View image download progress in Tasks.')}` : '';
    const introEnabled = result.Library.CollectionType === 'tvshows' && result.Library.LibraryOptions?.EnableIntroDetection === true;
    const creditsEnabled = videoLibrary && result.Library.LibraryOptions?.EnableCreditsDetection === true;
    const previewsEnabled = videoLibrary && result.Library.LibraryOptions?.EnablePreviewGeneration === true;
    const backgroundPreviewsEnabled = videoLibrary && result.Library.LibraryOptions?.EnableBackgroundPreviewGeneration === true;
    const audioWaveformsEnabled = videoLibrary && result.Library.LibraryOptions?.EnableAudioWaveformGeneration === true;
    const subtitleTimelinesEnabled = videoLibrary && result.Library.LibraryOptions?.EnableSubtitleTimelineGeneration === true;
    const backgroundEnabled = introEnabled || creditsEnabled || previewsEnabled || backgroundPreviewsEnabled || audioWaveformsEnabled || subtitleTimelinesEnabled;
    if (result.ScanError) {
      setActionError(new Error(`Library saved, but the scan could not start. ${result.ScanError.Message}`));
      setNotice(imagesEnabled ? { message: imageNotice.trim(), taskLink: true, background: true } : undefined);
    } else {
      setNotice({ message: `Library ${result.Library.Name} saved.${introEnabled ? ' Automatic intro detection is enabled.' : ''}${creditsEnabled ? ' Automatic credits detection is enabled.' : ''}${previewsEnabled ? ' Automatic seek previews are enabled.' : ''}${backgroundPreviewsEnabled ? ' Automatic background clips are enabled.' : ''}${audioWaveformsEnabled ? ' Automatic audio waveforms are enabled.' : ''}${subtitleTimelinesEnabled ? ' Automatic subtitle timelines are enabled.' : ''}${backgroundEnabled ? ' View background processing progress in Tasks.' : ''}${imageNotice}`, taskLink: Boolean(result.Job) || backgroundEnabled || imagesEnabled, background: backgroundEnabled || imagesEnabled });
    }
  }

  return (
    <Box aria-busy={loading} className="media-page media-libraries-page">
      <PageHeading title="Libraries" description={`${libraries ? formatCount(libraries.TotalRecordCount, 'library', 'libraries') : '— ' + t('libraries')} · ${t('Media directories and automatic processing')}`} action={<Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}><Button variant="outlined" startIcon={<RefreshRounded />} onClick={() => void scanAll()} disabled={!libraries?.Items.length || scanning.size > 0 || unconfirmedRefreshes.size > 0}>Scan all libraries</Button><Button variant="contained" startIcon={<AddRounded />} onClick={() => setCreating(true)} disabled={!canCreate}>Create library</Button></Stack>} />
      <Stack spacing={2.25}>
        {error != null && <ErrorNotice error={error} retry={refresh} />}
        {rootsError != null && <ErrorNotice error={rootsError} retry={refresh} />}
        {actionError != null && <ErrorNotice error={actionError} />}
        {!libraries && loading && <Stack spacing={1} role="status" aria-label="Loading libraries"><Skeleton variant="rounded" height={72} /><Skeleton variant="rounded" height={72} /><Skeleton variant="rounded" height={72} /></Stack>}
        {libraries && libraries.Items.length === 0 && <Paper variant="outlined" className="media-empty-state">
          <VideoLibraryOutlined sx={{ fontSize: 32, color: 'var(--ink3)' }} />
          <Typography component="h2" variant="h3">Your catalog starts with a library</Typography>
          <Typography color="text.secondary">{canCreate ? 'Create a library, choose its media directories, and scan to discover your collection.' : 'Once the server has access to a media directory, you can create your first library here.'}</Typography>
          <Button onClick={() => setCreating(true)} variant="outlined" startIcon={<AddRounded />} disabled={!canCreate}>Create library</Button>
        </Paper>}
        {libraries && libraries.Items.length > 0 && <section className="media-panel media-library-table-panel" aria-label={t('Media libraries')}>
          <table className="media-library-table">
            <thead><tr><th scope="col">{t('Name')}</th><th scope="col" className="media-library-count">{t('Content')}</th><th scope="col" className="media-library-paths">{t('Media directories')}</th><th scope="col" className="media-library-features">{t('Features')}</th><th scope="col" className="media-library-last">{t('Last scan')}</th><th scope="col"><span className="visually-hidden">{t('Actions')}</span></th></tr></thead>
            <tbody>{libraries.Items.map((library) => {
              const enabled = mediaFeatures.filter((feature) => library.LibraryOptions?.[feature.key]);
              const imagesEnabled = ['movies', 'tvshows', 'mixed'].includes(library.CollectionType) && library.LibraryOptions?.EnableOnlineImages === true;
              const count = counts[library.Id];
              return <tr key={library.Id}>
                <td><div className="media-library-identity"><MediaIconTile size={36}>{library.CollectionType === 'movies' ? <MovieOutlined /> : library.CollectionType === 'tvshows' ? <LiveTvOutlined /> : library.CollectionType === 'music' ? <LibraryMusicOutlined /> : <PermMediaOutlined />}</MediaIconTile><div className="media-truncate"><button type="button" className="media-name-button" translate="no" onClick={() => onManageItems(library)}>{library.Name}</button><span className="media-secondary-line">{t(collectionName(library.CollectionType))}</span></div></div></td>
                <td className="media-library-count"><span className="media-mono" title={count === undefined ? t('Count unavailable') : undefined}>{count === undefined ? '—' : new Intl.NumberFormat(locale).format(count)}</span><span className="media-secondary-line">{count === undefined ? t('items') : countLabel(count, 'item', 'items')}</span></td>
                <td className="media-library-paths"><div className="media-path-list">{library.Paths.map((path) => <span key={path} title={path} translate="no">{path}</span>)}</div></td>
                <td className="media-library-features"><span className="media-ellipsis" title={enabled.map((feature) => t(feature.label)).join(' · ')}>{enabled.length > 0 ? enabled.map((feature) => t(feature.label)).join(' · ') : t('No analysis enabled')}</span><span className="media-secondary-line media-mono">{enabled.length} / 6 {t('enabled')}</span>{imagesEnabled && <span className="media-secondary-line"><Tooltip title={t('Automatic image downloads are enabled.')}><span><Chip size="small" variant="outlined" label={t('Automatic images')} /></span></Tooltip></span>}</td>
                <td className="media-library-last"><span className="media-scan-status"><i className={`media-status-dot ${scanning.has(library.Id) ? 'media-status-active' : ''}`} />{scanning.has(library.Id) ? t('Starting…') : t(scanDate(library.LastScanAt, locale))}</span>{unconfirmedRefreshes.has(library.Id) && <span className="media-secondary-line media-warning">{t('Check the last request in Tasks')}</span>}</td>
                <td className="media-row-actions"><Tooltip title="Scan library"><IconButton size="small" onClick={() => void scanLibrary(library)} aria-label={`Scan library ${library.Name}`} disabled={scanning.has(library.Id) || unconfirmedRefreshes.has(library.Id)}>{scanning.has(library.Id) ? <CircularProgress size={15} color="inherit" /> : <RefreshRounded sx={{ fontSize: 15 }} />}</IconButton></Tooltip><Tooltip title="Manage items"><IconButton size="small" onClick={() => onManageItems(library)} aria-label={`Manage items in ${library.Name}`}><ListAltRounded sx={{ fontSize: 15 }} /></IconButton></Tooltip><IconButton size="small" aria-label={`More actions for ${library.Name}`} aria-haspopup="menu" aria-controls={menu?.library.Id === library.Id ? 'library-actions-menu' : undefined} aria-expanded={menu?.library.Id === library.Id ? true : undefined} onClick={(event) => setMenu({ anchor: event.currentTarget, library })}><MoreVertRounded sx={{ fontSize: 15 }} /></IconButton></td>
              </tr>;
            })}</tbody>
          </table>
        </section>}
        <div className="media-libraries-bottom">
          <LibraryDirectoriesPanel paths={libraries?.Items.flatMap((library) => library.Paths)} revision={revision} />
          <section className="media-panel" aria-labelledby="media-background-title"><div className="media-panel-heading"><h2 id="media-background-title">{t('Scans and processing')}</h2><Button size="small" onClick={onBackgroundTasks}>View tasks</Button></div><div className="media-panel-body"><p>{t('Scans discover files and update the catalog. Background analysis uses the features enabled for each library.')}</p><p className="media-muted">{t('Progress and errors are recorded in Tasks. Stopping a scan keeps catalog entries already processed.')}</p><Button variant="outlined" onClick={onTasks} startIcon={<PlaylistAddCheckRounded />}>Scan history</Button></div></section>
        </div>
      </Stack>
      <Menu id="library-actions-menu" anchorEl={menu?.anchor} open={Boolean(menu)} onClose={() => setMenu(undefined)} slotProps={{ paper: { sx: { minWidth: 228, borderRadius: '12px', bgcolor: 'var(--s2)', '& .MuiMenuItem-root': { minHeight: 44 } } } }}>
        <MenuItem onClick={() => { setEditingLibrary(menu?.library); setMenu(undefined); }}><ListItemIcon><EditOutlined fontSize="small" /></ListItemIcon>Edit library</MenuItem>
        <MenuItem onClick={() => { setBindingLibrary(menu?.library); setMenu(undefined); }}><ListItemIcon><FolderOpenOutlined fontSize="small" /></ListItemIcon>Storage bindings</MenuItem>
        <MenuItem disabled={Boolean(menu && scanning.has(menu.library.Id))} onClick={() => { setRefreshingMedia(menu?.library); setMenu(undefined); }}><ListItemIcon><RestartAltRounded fontSize="small" /></ListItemIcon>Refresh media details</MenuItem>
        <Divider />
        <MenuItem disabled={Boolean(menu && scanning.has(menu.library.Id))} onClick={() => { setDeleting(menu?.library); setMenu(undefined); }} sx={{ color: 'error.main' }}><ListItemIcon sx={{ color: 'inherit' }}><DeleteOutlineRounded fontSize="small" /></ListItemIcon>Delete library</MenuItem>
      </Menu>
      {creating && roots && <CreateLibraryDialog roots={roots} onClose={(needsRefresh) => { setCreating(false); if (needsRefresh) refresh(); }} onCreated={libraryCreated} onNavigationGuardChange={onNavigationGuardChange} />}
      {deleting && <DeleteLibraryDialog library={deleting} onClose={() => setDeleting(undefined)} onDeleted={() => { setNotice({ message: `Library ${deleting.Name} deleted. Media files were kept.`, taskLink: false }); setDeleting(undefined); refresh(); }} />}
      {refreshingMedia && <RefreshMediaDialog library={refreshingMedia} outcomeUnknown={unconfirmedRefreshes.has(refreshingMedia.Id)} onUnknown={() => setUnconfirmedRefreshes((ids) => new Set(ids).add(refreshingMedia.Id))} onClose={() => setRefreshingMedia(undefined)} onStarted={() => { setNotice({ message: `Media details refresh requested for ${refreshingMedia.Name}.`, taskLink: true }); setRefreshingMedia(undefined); }} onTasks={onTasks} />}
      {bindingLibrary && <RootBindingDialog key={bindingLibrary.Id} library={bindingLibrary} onClose={() => setBindingLibrary(undefined)} onNavigationGuardChange={onNavigationGuardChange} />}
      {editingLibrary && <LibraryEditorDialog key={editingLibrary.Id} libraryId={editingLibrary.Id} onClose={() => setEditingLibrary(undefined)} onSaved={librarySaved} onNavigationGuardChange={onNavigationGuardChange} />}
      <Snackbar open={Boolean(notice)} autoHideDuration={notice?.taskLink ? 10000 : 6000} onClose={() => setNotice(undefined)} anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}><Alert severity="success" variant="filled" action={notice?.taskLink ? <Button color="inherit" size="small" onClick={notice.background ? onBackgroundTasks : onTasks}>View tasks</Button> : undefined} onClose={() => setNotice(undefined)}>{notice?.message}</Alert></Snackbar>
    </Box>
  );
}
