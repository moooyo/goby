import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Checkbox, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, FormControlLabel, IconButton, Paper, Skeleton, Stack, TextField, Typography } from '@mui/material';
import AddRounded from '@mui/icons-material/AddRounded';
import DeleteOutlineRounded from '@mui/icons-material/DeleteOutlineRounded';
import FolderOpenOutlined from '@mui/icons-material/FolderOpenOutlined';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { ApiError, isAbortError } from './api';
import type { LibraryResponse } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { DirectoryPickerDialog } from './DirectoryPickerDialog';
import { libraryManagementApi } from './libraryManagementApi';
import type { EditableLibrary, LibraryOptions } from './libraryManagementApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

interface PathDraft { key: string; original: string; path: string; preserve: boolean; count: number }
export function LibraryEditorDialog({ libraryId, onClose, onSaved, onNavigationGuardChange }: { libraryId: string; onClose: () => void; onSaved: (result: LibraryResponse) => void; onNavigationGuardChange: UserNavigationGuardChange }) {
  const [library, setLibrary] = useState<EditableLibrary>();
  const [name, setName] = useState('');
  const [paths, setPaths] = useState<PathDraft[]>([]);
  const [options, setOptions] = useState<LibraryOptions>();
  const [scan, setScan] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false);
  const [revision, setRevision] = useState(0);
  const [picker, setPicker] = useState<string>();
  const inFlight = useRef(false);
  const changedPaths = Boolean(library && JSON.stringify(paths.map((row) => row.path.trim())) !== JSON.stringify(library.Paths));
  const dirty = Boolean(library && (name !== library.Name || changedPaths || JSON.stringify(options) !== JSON.stringify(library.LibraryOptions) || scan));
  const removed = library?.RegisteredPaths.filter((root) => !paths.some((row) => row.path.trim() === root.Path || (row.original === root.Path && row.preserve))) ?? [];
  const invalidPaths = paths.length === 0 || paths.length > 32 || paths.some((row) => !row.path.trim().startsWith('/')) || new Set(paths.map((row) => row.path.trim())).size !== paths.length;
  useUserDraftNavigation(dirty, busy, onNavigationGuardChange, 'Discard unsaved library changes and leave this page?');
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setLibrary(undefined); setOptions(undefined);
    void libraryManagementApi.get(libraryId, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      setLibrary(result); setName(result.Name); setOptions(result.LibraryOptions);
      setPaths(result.Paths.map((path) => ({ key: path, original: path, path, preserve: true, count: result.RegisteredPaths.find((root) => root.Path === path)?.ItemCount ?? 0 })));
      setScan(false); setAcknowledged(false); setReview(false);
    }).catch((cause: unknown) => { if (!isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [libraryId, revision]);
  function close() { if (!inFlight.current && (!dirty || window.confirm('Discard unsaved library changes?'))) onClose(); }
  function reload() { if (!inFlight.current && (!dirty || window.confirm('Discard your draft and reload the latest library?'))) setRevision((value) => value + 1); }
  function updatePath(key: string, update: Partial<PathDraft>) { setPaths((rows) => rows.map((row) => row.key === key ? { ...row, ...update } : row)); setAcknowledged(false); }
  async function save() {
    if (inFlight.current || !library || !options || !dirty || review || invalidPaths || !name.trim() || (removed.length > 0 && !acknowledged)) return;
    inFlight.current = true; setBusy(true); setError(null);
    try {
      onSaved(await libraryManagementApi.update(libraryId, { Revision: library.Revision, Name: name.trim(), Paths: paths.map((row) => row.path.trim()), PathReplacements: paths.filter((row) => row.original && row.preserve && row.original !== row.path.trim()).map((row) => ({ From: row.original, To: row.path.trim() })), LibraryOptions: options, Scan: scan, AcknowledgePathRemoval: removed.length > 0 && acknowledged }));
    } catch (cause) {
      if (!isAbortError(cause)) setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
    } finally { inFlight.current = false; setBusy(false); }
  }
  const disabled = loading || busy || review;
  return <>
    <Dialog open fullWidth maxWidth="md" onClose={close} aria-labelledby="library-editor-title" slotProps={{ paper: { sx: { maxWidth: 760 } } }}>
      <DialogTitle id="library-editor-title">Edit library</DialogTitle>
      <DialogContent aria-busy={loading || busy}><Stack spacing={3} sx={{ pt: 1 }}>
        <Typography variant="body1" color="text.secondary">Update this library's name, media directories, and import preferences.</Typography>
        {error != null && <ErrorNotice error={error} />}
        {review && <Alert severity="warning" action={<Button color="inherit" onClick={reload}>Reload</Button>}>The library may have changed or the save response could not be confirmed. Reload and review the saved library before trying again.</Alert>}
        {loading && <Skeleton variant="rounded" height={220} aria-label="Loading library settings" />}
        {library && options && <>
          <TextField autoFocus fullWidth required label="Library name" value={name} disabled={disabled} onChange={(event) => setName(event.target.value)} error={Boolean(fieldError(error, 'Name'))} helperText={fieldError(error, 'Name')} />
          <Box component="section" aria-label="Library directories"><Stack spacing={2}>
            <Typography component="h3" variant="h4">Media directories</Typography>
            <Typography variant="body2" color="text.secondary">A moved directory can keep its media records and users' playback history. Verify the new server mount before scanning.</Typography>
            {paths.map((row, index) => <Paper key={row.key} sx={{ p: 2, borderRadius: '16px' }}><Stack spacing={1.5}>
              <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}><TextField fullWidth label={`Directory ${index + 1}`} value={row.path} disabled={disabled} onChange={(event) => updatePath(row.key, { path: event.target.value })} error={Boolean(row.path && !row.path.trim().startsWith('/'))} helperText={row.original ? `${row.count.toLocaleString()} catalog items` : 'New directory'} slotProps={{ htmlInput: { spellCheck: false, autoCapitalize: 'none' } }} sx={{ '& input': { fontFamily: '"JetBrains Mono Variable", Consolas, monospace', fontSize: 13 } }} /><Stack direction="row" sx={{ alignItems: 'flex-start', justifyContent: 'flex-end' }}><IconButton aria-label={`Browse directory ${index + 1}`} disabled={disabled} onClick={() => setPicker(row.key)}><FolderOpenOutlined /></IconButton><IconButton aria-label={`Remove directory ${index + 1}`} disabled={disabled} onClick={() => { setPaths((rows) => rows.filter((entry) => entry.key !== row.key)); setAcknowledged(false); }}><DeleteOutlineRounded /></IconButton></Stack></Stack>
              {row.original && row.original !== row.path.trim() && <><Typography variant="caption" sx={{ overflowWrap: 'anywhere' }}>Original: {row.original}</Typography><FormControlLabel control={<Checkbox checked={row.preserve} disabled={disabled} onChange={(event) => updatePath(row.key, { preserve: event.target.checked })} />} label="Move this path and preserve media records" /></>}
            </Stack></Paper>)}
            <Button sx={{ alignSelf: 'flex-start' }} startIcon={<AddRounded />} disabled={disabled || paths.length >= 32} onClick={() => { setPaths((rows) => [...rows, { key: crypto.randomUUID(), original: '', path: '', preserve: false, count: 0 }]); setAcknowledged(false); }}>Add directory</Button>
            {invalidPaths && <Alert severity="info">Use 1–32 unique absolute Linux directory paths.</Alert>}
            {fieldError(error, 'Paths') && <Typography variant="body2" color="error.main">{fieldError(error, 'Paths')}</Typography>}
            {removed.length > 0 && <Alert severity="warning"><Typography variant="body2">Removing these paths removes {removed.reduce((sum, root) => sum + root.ItemCount, 0).toLocaleString()} catalog items and their associated state. Media files on disk are kept.</Typography><FormControlLabel control={<Checkbox checked={acknowledged} disabled={disabled} onChange={(event) => setAcknowledged(event.target.checked)} />} label="I understand that these catalog records will be removed" /></Alert>}
          </Stack></Box>
          <Divider />
          <Box component="section" aria-label="Library options"><Typography component="h3" variant="h4" sx={{ mb: 2 }}>Library options</Typography><Stack spacing={2}>
            {library.CollectionType === 'tvshows' && <Box><FormControlLabel control={<Checkbox checked={options.EnableIntroDetection ?? false} disabled={disabled} onChange={(event) => setOptions({ ...options, EnableIntroDetection: event.target.checked })} />} label="Automatic intro detection" /><Typography variant="body2" color="text.secondary">Analyze episode intros in the background and use reliable matches for playback. If no intro is found, playback stays unchanged. Progress and errors are available in Tasks.</Typography></Box>}
            {['movies', 'tvshows', 'mixed'].includes(library.CollectionType) && <Box><FormControlLabel control={<Checkbox checked={options.EnableCreditsDetection ?? false} disabled={disabled} onChange={(event) => setOptions({ ...options, EnableCreditsDetection: event.target.checked })} />} label="自动识别片尾" /><Typography variant="body2" color="text.secondary">在影片尾部分析音频重复片段和画面；人工标记优先，未识别到结果时不改变播放。进度与错误在任务中心查看。</Typography></Box>}
            {['movies', 'tvshows', 'mixed'].includes(library.CollectionType) && <Box><FormControlLabel control={<Checkbox checked={options.EnablePreviewGeneration ?? false} disabled={disabled} onChange={(event) => setOptions({ ...options, EnablePreviewGeneration: event.target.checked })} />} label="Automatic seek previews" /><Typography variant="body2" color="text.secondary">Generate preview images in the background for seeking in compatible players. Progress and errors are available in Tasks. Turning this off keeps existing valid previews.</Typography></Box>}
            {['movies', 'tvshows', 'mixed'].includes(library.CollectionType) && <Box><FormControlLabel control={<Checkbox checked={options.EnableBackgroundPreviewGeneration ?? false} disabled={disabled} onChange={(event) => setOptions({ ...options, EnableBackgroundPreviewGeneration: event.target.checked })} />} label="自动生成背景短片" /><Typography variant="body2" color="text.secondary">默认关闭。生成的短片保存在片源旁，需要媒体目录可写。关闭生成不会删除已有短片，已有成品只有明确重新生成时才替换。可在任务中心查看进度和错误。</Typography></Box>}
            {['movies', 'tvshows', 'mixed'].includes(library.CollectionType) && <Box><FormControlLabel control={<Checkbox checked={options.EnableAudioWaveformGeneration ?? false} disabled={disabled} onChange={(event) => setOptions({ ...options, EnableAudioWaveformGeneration: event.target.checked })} />} label="自动生成音轨波形" /><Typography variant="body2" color="text.secondary">默认关闭。为每条音轨分别生成波形，并随片源持久保存，需要媒体目录可写。普通任务复用已有成品，只有明确重新生成时才替换。过期成品仍会保留，但播放时不显示；关闭生成不会删除已有波形。</Typography></Box>}
            {['movies', 'tvshows', 'mixed'].includes(library.CollectionType) && <Box><FormControlLabel control={<Checkbox checked={options.EnableSubtitleTimelineGeneration ?? false} disabled={disabled} onChange={(event) => setOptions({ ...options, EnableSubtitleTimelineGeneration: event.target.checked })} />} label="自动生成字幕时间轴" /><Typography variant="body2" color="text.secondary">默认关闭。为内封 PGS/DVD 字幕生成显示区间，并随片源持久保存。普通任务复用已有成品，只有明确重新生成时才替换；需要媒体目录可写。播放器只显示有效时间轴，其余状态仅在任务中心查看，字幕播放不受影响。</Typography></Box>}
            <FormControlLabel control={<Checkbox checked={options.EnableLocalMetadata} disabled={disabled} onChange={(event) => setOptions({ ...options, EnableLocalMetadata: event.target.checked })} />} label="Import local metadata files" />
            <FormControlLabel control={<Checkbox checked={options.EnableLocalImages} disabled={disabled} onChange={(event) => setOptions({ ...options, EnableLocalImages: event.target.checked })} />} label="Import images from media directories" />
            <FormControlLabel control={<Checkbox checked={options.EnableEmbeddedArtwork ?? true} disabled={disabled || !options.EnableLocalImages} onChange={(event) => setOptions({ ...options, EnableEmbeddedArtwork: event.target.checked })} />} label="Extract embedded audio artwork" />
            <Typography variant="body2" color="text.secondary">Embedded audio covers require both image import and extraction to be enabled. These options control future scans. Turning one off keeps previously imported information and manual changes. Scan after re-enabling it to refresh local sources.</Typography>
          </Stack></Box>
          <Paper sx={{ px: 2, py: 1, borderRadius: '16px' }}><FormControlLabel control={<Checkbox checked={scan} disabled={disabled} onChange={(event) => setScan(event.target.checked)} />} label={<Box><Typography variant="body2" sx={{ fontWeight: 600 }}>Scan after saving</Typography><Typography variant="caption" color="text.secondary">Find new media using the updated library settings.</Typography></Box>} /></Paper>
        </>}
      </Stack></DialogContent>
      <DialogActions sx={{ p: 3, flexWrap: 'wrap', gap: 1 }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{dirty ? 'Unsaved library changes' : 'Changes apply after saving'}</Typography><Button onClick={close} disabled={busy} color="secondary">Cancel</Button><Button onClick={reload} disabled={busy || loading}>Reload</Button><Button variant="contained" onClick={() => void save()} disabled={disabled || !library || !dirty || !name.trim() || invalidPaths || (removed.length > 0 && !acknowledged)} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? 'Saving library...' : 'Save library'}</Button></DialogActions>
    </Dialog>
    {picker && <DirectoryPickerDialog onClose={() => setPicker(undefined)} onChoose={(path) => { updatePath(picker, { path }); setPicker(undefined); }} />}
  </>;
}
