import { useEffect, useState } from 'react';
import { Alert, Box, Button, Checkbox, Chip, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, MenuItem, Paper, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Tooltip, Typography } from '@mui/material';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { isAbortError } from './api';
import type { Library } from './api';
import { ErrorNotice } from './components';
import { analysisBytes, analysisIntroStatus, analysisTime } from './mediaAnalysis';
import type { AnalysisItem, AnalysisItems } from './mediaAnalysis';
import { mediaAnalysisApi } from './mediaAnalysisApi';
import { mediaPanelSx, MediaSearchField, mediaSelected, mediaSelectSx } from './MediaPagePrimitives';

function statusLabel(status: string) { const value = status.replaceAll('_', ' '); return value.charAt(0).toUpperCase() + value.slice(1); }
function statusTone(status: string): 'success' | 'warning' | 'error' | 'info' | 'default' {
  if (['ready', 'qualified', 'published'].includes(status)) return 'success';
  if (['review', 'stale', 'suppressed'].includes(status)) return 'warning';
  if (['failed', 'error'].includes(status)) return 'error';
  if (['building', 'running', 'pending', 'queued'].includes(status)) return 'info';
  return 'default';
}

function IntroStatus({ item }: { item: AnalysisItem }) {
  const detection = item.Detection;
  const tone = detection.Effective ? 'success' : ['failed', 'error', 'unavailable'].includes(detection.Status) ? 'error' : ['queued', 'running'].includes(detection.Status) ? 'info' : 'default';
  return <Stack spacing={0.5} sx={{ alignItems: 'flex-start' }}>
    <Chip size="small" color={tone} label={analysisIntroStatus(item)} />
    {detection.Effective && <Typography variant="caption" color="text.secondary"><IntervalText interval={detection.Effective} /></Typography>}
  </Stack>;
}

function PreviewStatus({ previews }: { previews: AnalysisItem['Previews'] }) {
  if (previews.length === 0) return <Chip size="small" label="Not generated" />;
  const counts = new Map<string, number>();
  for (const preview of previews) counts.set(preview.Status, (counts.get(preview.Status) ?? 0) + 1);
  return <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5 }}>{Array.from(counts, ([status, count]) => <Chip key={status} size="small" color={statusTone(status)} label={status === 'ready' && count === previews.length ? 'Generated' : `${statusLabel(status)}${previews.length > 1 ? ` · ${count}` : ''}`} />)}</Stack>;
}

function IntervalText({ interval }: { interval: { StartTicks: number; EndTicks: number } }) {
  return <>{analysisTime(interval.StartTicks)}–{analysisTime(interval.EndTicks)}</>;
}
function RecordedTime({ value }: { value: string }) { return value.startsWith('0001-') ? <>Not recorded</> : <time dateTime={value}>{new Date(value).toLocaleString()}</time>; }

function DetectionSummary({ item }: { item: AnalysisItem }) {
  const detection = item.Detection;
  const failed = ['failed', 'error', 'unavailable'].includes(detection.Status);
  return <Stack spacing={1.5}>
    <Chip label={analysisIntroStatus(item)} size="small" variant="outlined" sx={{ alignSelf: 'flex-start' }} />
    {detection.Effective ? <Box><Typography component="h3" variant="h4">Playback intro</Typography><Typography variant="body2" sx={{ mt: 1 }}><IntervalText interval={detection.Effective} /></Typography><Typography variant="body2" color="text.secondary">This interval is available to compatible players.</Typography></Box>
      : item.Type !== 'Episode' ? <Typography variant="body2" color="text.secondary">Automatic intro detection is available for TV episodes.</Typography>
        : <Typography variant="body2" color="text.secondary">No intro is applied. Playback stays unchanged.</Typography>}
    {detection.Status === 'stale' && <Alert severity="info">The previous analysis is no longer current.</Alert>}
    {failed && <Alert severity="warning">{detection.Reasons.length ? detection.Reasons.map((reason) => reason.replaceAll('_', ' ')).join('; ') : 'Analysis could not complete.'} Open Tasks for progress and error details.</Alert>}
    <Typography variant="caption" color="text.secondary">Analysis updated: <RecordedTime value={detection.UpdatedAt} /></Typography>
  </Stack>;
}

function AnalysisItemDialog({ id, onClose }: { id: string; onClose: () => void }) {
  const [item, setItem] = useState<AnalysisItem>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setLoadError(undefined);
    void mediaAnalysisApi.item(id, { signal: controller.signal }).then((value) => { if (!controller.signal.aborted) setItem(value); })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [id, revision]);
  return <Dialog open fullWidth maxWidth="md" onClose={onClose} aria-labelledby="analysis-item-title">
    <DialogTitle id="analysis-item-title">{item?.Name || 'Media analysis result'}</DialogTitle>
    <DialogContent><Stack spacing={2.5}>
      {loading && <Box role="status" aria-label="Loading analysis result"><Skeleton height={50} /><Skeleton height={140} /></Box>}
      {loadError != null && <ErrorNotice error={loadError} retry={() => setRevision((value) => value + 1)} />}
      {item && !loading && <>
        <DetectionSummary item={item} />
        <Box><Typography variant="h4" component="h3">Preview outputs</Typography>{item.Previews.length === 0 ? <Typography color="text.secondary" variant="body2" sx={{ mt: 1 }}>No preview outputs recorded.</Typography> : <TableContainer><Table size="small" aria-label="Preview outputs"><TableHead><TableRow><TableCell>Dimensions</TableCell><TableCell>Frames</TableCell><TableCell>Size</TableCell><TableCell>Status</TableCell></TableRow></TableHead><TableBody>{item.Previews.map((preview, index) => <TableRow key={`${preview.Width}-${index}`}><TableCell>{preview.Width} × {preview.Height}</TableCell><TableCell>{preview.FrameCount}</TableCell><TableCell>{analysisBytes(preview.Size)}</TableCell><TableCell>{preview.Status}{preview.FailureCode && <Typography variant="caption" component="div">{preview.FailureCode.replaceAll('_', ' ')}</Typography>}<Typography variant="caption" component="div"><RecordedTime value={preview.UpdatedAt} /></Typography></TableCell></TableRow>)}</TableBody></Table></TableContainer>}</Box>
      </>}
    </Stack></DialogContent>
    <DialogActions><Button disabled={loading} onClick={() => setRevision((value) => value + 1)}>Refresh result</Button><Button onClick={onClose}>Close</Button></DialogActions>
  </Dialog>;
}

export function MediaAnalysisResults({ libraries, disabled, previewAvailable, refresh, onRun }: {
  libraries: Library[]; disabled: boolean; previewAvailable: boolean; refresh: number;
  onRun: (kind: 'previews', libraryIds: string[], itemIds: string[]) => void;
}) {
  const [libraryId, setLibraryId] = useState(''); const [search, setSearch] = useState(''); const [query, setQuery] = useState(''); const [page, setPage] = useState(0);
  const [data, setData] = useState<AnalysisItems>(); const [error, setError] = useState<unknown>(); const [loading, setLoading] = useState(true); const [revision, setRevision] = useState(0);
  const [selected, setSelected] = useState<string[]>([]); const [detail, setDetail] = useState<string>();
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(undefined); setData(undefined); setSelected([]);
    void mediaAnalysisApi.items(libraryId, query, page * 25, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return;
      if (page > 0 && page * 25 >= value.TotalRecordCount) setPage(Math.max(0, Math.ceil(value.TotalRecordCount / 25) - 1)); else setData(value);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [libraryId, query, page, revision, refresh]);
  const chosen = data?.Items.filter((item) => selected.includes(item.Id)) ?? [];
  const toggleItem = (id: string, checked: boolean) => setSelected((values) => checked ? [...values, id] : values.filter((value) => value !== id));
  const selectAll = <Checkbox slotProps={{ input: { 'aria-label': 'Select all visible media' } }} disabled={disabled} checked={Boolean(data?.Items.length) && selected.length === data?.Items.length} indeterminate={selected.length > 0 && selected.length < (data?.Items.length ?? 0)} onChange={(event) => setSelected(event.target.checked ? data?.Items.map((item) => item.Id) ?? [] : [])} />;
  return <Paper component="section" aria-labelledby="analysis-results-title" variant="outlined" sx={mediaPanelSx}>
    <Stack direction={{ xs: 'column', md: 'row' }} sx={{ alignItems: { xs: 'stretch', md: 'center' }, gap: 2, p: 2.5 }}>
      <Typography component="h2" variant="h3" id="analysis-results-title" sx={{ flex: 1 }}>Analysis results</Typography>
      <Box component="form" onSubmit={(event) => { event.preventDefault(); setQuery(search.trim()); setPage(0); }} sx={{ width: { xs: '100%', md: 'auto' } }}>
        <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ gap: 1.5, alignItems: { xs: 'stretch', sm: 'center' } }}>
          <TextField select label="Filter library" size="small" value={libraryId} disabled={disabled} onChange={(event) => { setLibraryId(event.target.value); setPage(0); }} slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }} sx={mediaSelectSx}><MenuItem value="">All libraries</MenuItem>{libraries.map((library) => <MenuItem key={library.Id} value={library.Id}>{library.Name}</MenuItem>)}</TextField>
          <Box sx={{ width: { xs: '100%', sm: 260 } }}><MediaSearchField label="Search media" value={search} onChange={setSearch} disabled={disabled} maxLength={256} /></Box>
          <Tooltip title="Refresh results"><span><IconButton aria-label="Refresh results" disabled={disabled || loading} onClick={() => setRevision((value) => value + 1)}><RefreshRounded sx={{ fontSize: 20 }} /></IconButton></span></Tooltip>
        </Stack>
      </Box>
    </Stack>
    {error != null && <Box sx={{ px: 2.5, pb: 2.5 }}><ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} /></Box>}
    {loading && <Box role="status" aria-label="Loading media analysis items" sx={{ px: 2.5, pb: 2.5 }}><Skeleton height={54} /><Skeleton height={54} /></Box>}
    {data?.Items.length === 0 && <Box sx={{ mx: 2.5, mb: 2.5, p: 4, textAlign: 'center', border: '1px dashed', borderColor: 'divider', borderRadius: '12px' }}><Typography variant="body2" color="text.secondary">No matching media. Scan a supported library or change the filters.</Typography></Box>}
    {data && data.Items.length > 0 && <>
      {selected.length > 0 && <Box sx={{ px: 2.5, py: 1, bgcolor: mediaSelected }}>
        <Stack direction={{ xs: 'column', md: 'row' }} sx={{ alignItems: { xs: 'stretch', md: 'center' }, gap: 1 }}>
          <Typography role="status" aria-atomic="true" variant="body2" sx={{ flex: 1, fontWeight: 600, color: '#0F2A57' }}>{selected.length} {selected.length === 1 ? 'item' : 'items'} selected</Typography>
          <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 0.5 }}>
            <Button size="small" disabled={disabled || !previewAvailable || chosen.some((item) => !['Movie', 'Episode', 'Video'].includes(item.Type))} onClick={() => onRun('previews', [], selected)}>Build selected previews</Button>
          </Stack>
        </Stack>
      </Box>}
      <TableContainer sx={{ borderRadius: 0, display: { xs: 'none', md: 'block' } }}>
        <Table aria-label="Media analysis items" sx={{ '& td, & th': { px: 2.5 }, '& .MuiTableCell-paddingCheckbox': { width: 44, pr: 0, pl: 1.5 } }}>
          <TableHead><TableRow><TableCell padding="checkbox">{selectAll}</TableCell><TableCell sx={{ width: '46%' }}>Item</TableCell><TableCell sx={{ width: '30%' }}>Intro analysis</TableCell><TableCell>Seek previews</TableCell></TableRow></TableHead>
          <TableBody>{data.Items.map((item) => <TableRow key={item.Id} selected={selected.includes(item.Id)} hover sx={{ '&.Mui-selected': { bgcolor: 'transparent' }, '&.Mui-selected:hover': { bgcolor: '#F3F6FB' } }}>
            <TableCell padding="checkbox"><Checkbox slotProps={{ input: { 'aria-label': `Select ${item.Name}` } }} disabled={disabled} checked={selected.includes(item.Id)} onChange={(event) => toggleItem(item.Id, event.target.checked)} /></TableCell>
            <TableCell component="th" scope="row"><Button disabled={disabled} onClick={() => setDetail(item.Id)} aria-label={`View analysis for ${item.Name}`} sx={{ color: 'text.primary', display: 'block', minHeight: 0, minWidth: 0, p: 0, borderRadius: 0.5, textAlign: 'left', overflowWrap: 'anywhere', fontSize: 13, fontWeight: 600 }}>{item.Name || 'Untitled media'}</Button><Typography component="div" variant="caption" color="text.secondary" sx={{ mt: 0.25 }}>{libraries.find((library) => library.Id === item.LibraryId)?.Name ?? 'Unknown library'} · {item.Type}</Typography></TableCell>
            <TableCell><IntroStatus item={item} /></TableCell><TableCell><PreviewStatus previews={item.Previews} /></TableCell>
          </TableRow>)}</TableBody>
        </Table>
      </TableContainer>
      <Box sx={{ display: { xs: 'block', md: 'none' } }}>
        <Stack direction="row" sx={{ alignItems: 'center', px: 1.5, py: 0.5, bgcolor: '#F6F8FC' }}>{selectAll}<Typography variant="caption" color="text.secondary">Select all visible media</Typography></Stack>
        {data.Items.map((item) => <Box key={item.Id} sx={{ p: 2, borderTop: '1px solid', borderColor: 'divider' }}>
          <Stack direction="row" sx={{ alignItems: 'flex-start', gap: 0.5 }}>
            <Checkbox slotProps={{ input: { 'aria-label': `Select ${item.Name}` } }} disabled={disabled} checked={selected.includes(item.Id)} onChange={(event) => toggleItem(item.Id, event.target.checked)} sx={{ ml: -1, mt: -0.75 }} />
            <Box sx={{ minWidth: 0, flex: 1 }}><Button disabled={disabled} onClick={() => setDetail(item.Id)} aria-label={`View analysis for ${item.Name}`} sx={{ minHeight: 0, p: 0, color: 'text.primary', textAlign: 'left', overflowWrap: 'anywhere', fontWeight: 600 }}>{item.Name || 'Untitled media'}</Button><Typography variant="caption" component="div" color="text.secondary" sx={{ mt: 0.25 }}>{libraries.find((library) => library.Id === item.LibraryId)?.Name ?? 'Unknown library'} · {item.Type}</Typography></Box>
          </Stack>
          <Box sx={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 2, mt: 1.5, pl: 3.5 }}>
            <Stack spacing={0.75}><Typography variant="caption" color="text.secondary">Intro analysis</Typography><IntroStatus item={item} /></Stack>
            <Stack spacing={0.75} sx={{ alignItems: 'flex-start' }}><Typography variant="caption" color="text.secondary">Seek previews</Typography><PreviewStatus previews={item.Previews} /></Stack>
          </Box>
        </Box>)}
      </Box>
      {selected.length > 0 && <Typography variant="caption" color="text.secondary" component="p" sx={{ px: 2.5, py: 1.5 }}>Selections apply to this page. Previews support movies, episodes, and videos. The rebuild choice in Run options applies to selected previews.</Typography>}
    </>}
    {data && <TablePagination component="div" count={data.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} disabled={disabled || loading} onPageChange={(_, value) => setPage(value)} sx={{ borderTop: '1px solid', borderColor: 'divider' }} />}
    {detail && <AnalysisItemDialog id={detail} onClose={() => setDetail(undefined)} />}
  </Paper>;
}
