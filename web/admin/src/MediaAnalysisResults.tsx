import { useEffect, useState } from 'react';
import { Alert, Box, Button, Chip, Dialog, DialogActions, DialogContent, DialogTitle, MenuItem, Paper, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography } from '@mui/material';
import { isAbortError } from './api';
import type { Library } from './api';
import { ErrorNotice } from './components';
import { analysisBytes, analysisIntroStatus, analysisTime } from './mediaAnalysis';
import type { AnalysisItem, AnalysisItems } from './mediaAnalysis';
import { mediaAnalysisApi } from './mediaAnalysisApi';

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

export function MediaAnalysisResults({ libraries, disabled, refresh }: {
  libraries: Library[]; disabled: boolean; refresh: number;
}) {
  const [libraryId, setLibraryId] = useState(''); const [search, setSearch] = useState(''); const [query, setQuery] = useState(''); const [page, setPage] = useState(0);
  const [data, setData] = useState<AnalysisItems>(); const [error, setError] = useState<unknown>(); const [loading, setLoading] = useState(true); const [revision, setRevision] = useState(0);
  const [detail, setDetail] = useState<string>();
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(undefined); setData(undefined);
    void mediaAnalysisApi.items(libraryId, query, page * 25, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return;
      if (page > 0 && page * 25 >= value.TotalRecordCount) setPage(Math.max(0, Math.ceil(value.TotalRecordCount / 25) - 1)); else setData(value);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [libraryId, query, page, revision, refresh]);
  return <Paper component="section" aria-labelledby="analysis-results-title" variant="outlined" sx={{ p: { xs: 2, sm: 3 } }}>
    <Stack spacing={2.5}>
      <Typography component="h2" variant="h3" id="analysis-results-title">Analysis results</Typography>
      <Box component="form" onSubmit={(event) => { event.preventDefault(); setQuery(search.trim()); setPage(0); }}><Stack direction={{ xs: 'column', sm: 'row' }} spacing={2}>
        <TextField select label="Filter library" value={libraryId} disabled={disabled} onChange={(event) => { setLibraryId(event.target.value); setPage(0); }} sx={{ minWidth: 180 }}><MenuItem value="">All libraries</MenuItem>{libraries.map((library) => <MenuItem key={library.Id} value={library.Id}>{library.Name}</MenuItem>)}</TextField>
        <TextField label="Search media" value={search} onChange={(event) => setSearch(event.target.value)} fullWidth disabled={disabled} slotProps={{ htmlInput: { maxLength: 256 } }} /><Button type="submit" variant="outlined" disabled={disabled || loading}>Search</Button><Button disabled={disabled || loading} onClick={() => setRevision((value) => value + 1)}>Refresh results</Button>
      </Stack></Box>
      {error != null && <ErrorNotice error={error} retry={() => setRevision((value) => value + 1)} />}
      {loading && <Box role="status" aria-label="Loading media analysis items"><Skeleton height={54} /><Skeleton height={54} /></Box>}
      {data?.Items.length === 0 && <Typography color="text.secondary">No matching media. Scan a supported library or change the filters.</Typography>}
      {data && data.Items.length > 0 && <>
        <TableContainer><Table aria-label="Media analysis items"><TableHead><TableRow><TableCell>Item</TableCell><TableCell>Intro analysis</TableCell><TableCell>Previews</TableCell><TableCell>Details</TableCell></TableRow></TableHead><TableBody>{data.Items.map((item) => <TableRow key={item.Id}><TableCell component="th" scope="row" sx={{ overflowWrap: 'anywhere' }}>{item.Name}<Typography component="div" variant="caption" color="text.secondary">{item.Type}</Typography></TableCell><TableCell>{analysisIntroStatus(item)}{item.Detection.Effective && <Typography variant="caption" component="div"><IntervalText interval={item.Detection.Effective} /></Typography>}</TableCell><TableCell>{item.Previews.filter((preview) => preview.Status === 'ready').length} ready / {item.Previews.length} recorded</TableCell><TableCell><Button disabled={disabled} onClick={() => setDetail(item.Id)} aria-label={`View analysis for ${item.Name}`}>View</Button></TableCell></TableRow>)}</TableBody></Table></TableContainer>
      </>}
      {data && <TablePagination component="div" count={data.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} disabled={disabled || loading} onPageChange={(_, value) => setPage(value)} />}
    </Stack>
    {detail && <AnalysisItemDialog id={detail} onClose={() => setDetail(undefined)} />}
  </Paper>;
}
