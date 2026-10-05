import { useCallback, useEffect, useState } from 'react';
import { Box, Alert, Button, Chip, CircularProgress, MenuItem, Paper, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography } from '@mui/material';
import type { Library } from './api';
import { ErrorNotice } from './components';
import { audioWaveformApi } from './audioWaveformApi';
import { activeAudioWaveform, audioWaveformStates, audioWaveformStateLabels } from './audioWaveform';
import type { AudioWaveformActivity, AudioWaveformItem, AudioWaveformPage, AudioWaveformState } from './audioWaveform';
import { AudioWaveformDialog } from './AudioWaveformDialog';
import { AudioWaveformRunProgress } from './AudioWaveformRunProgress';
import { useAudioWaveformRequest } from './useAudioWaveformRequest';
import { useTaskResource } from './useTaskResource';
import { mediaPanelSx, MediaSearchField, mediaSelectSx } from './MediaPagePrimitives';

const poll = (value: AudioWaveformPage) => value.Items.some(activeAudioWaveform);
function Status({ item }: { item: AudioWaveformItem }) { return <Chip size="small" label={audioWaveformStateLabels[item.State]} color={item.State === 'failed' ? 'error' : activeAudioWaveform(item) ? 'primary' : 'default'} />; }

export function AudioWaveformPanel({ currentUserId, libraries, onLibraries, onTasks, onActivityChange, visible }: {
  currentUserId: string; libraries: Library[]; onLibraries: () => void; onTasks: () => void; onActivityChange: (value: AudioWaveformActivity) => void; visible: boolean;
}) {
  const [libraryId, setLibraryId] = useState('');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');
  const [state, setState] = useState<AudioWaveformState | ''>('');
  const [page, setPage] = useState(0);
  const [detail, setDetail] = useState<AudioWaveformItem>();
  const [dialogActivity, setDialogActivity] = useState<AudioWaveformActivity>({ pending: false, busy: false });
  const [stopping, setStopping] = useState(false);
  const request = useAudioWaveformRequest(currentUserId);
  const load = useCallback((signal: AbortSignal) => audioWaveformApi.items({ libraryId, search: query, state, start: page * 25 }, { signal }), [libraryId, query, state, page]);
  const resource = useTaskResource({ key: JSON.stringify([libraryId, query, state, page]), load, poll, enabled: visible });
  const supportedLibraries = libraries.filter((library) => ['movies', 'tvshows', 'mixed'].includes(library.CollectionType));
  const selectedLibrary = supportedLibraries.find((library) => library.Id === libraryId);
  const pendingLibrary = supportedLibraries.find((library) => library.Id === request.pending?.LibraryIds[0]);
  const busy = request.busy || stopping;
  useEffect(() => { onActivityChange({ pending: Boolean(request.pending) || dialogActivity.pending, busy: busy || dialogActivity.busy }); }, [request.pending, dialogActivity, busy, onActivityChange]);
  useEffect(() => {
    if (resource.data && page > 0 && page * 25 >= resource.data.TotalRecordCount) setPage(Math.max(0, Math.ceil(resource.data.TotalRecordCount / 25) - 1));
  }, [resource.data, page]);

  async function generateLibrary() {
    if (busy || request.pending || !selectedLibrary) return;
    if (await request.start({ LibraryIds: [selectedLibrary.Id], ItemIds: [], Force: false })) resource.reload();
  }
  async function retry() { if (await request.retry()) resource.reload(); }
  function closeDetail() { setDetail(undefined); setDialogActivity({ pending: false, busy: false }); resource.reload(); }

  return <Stack spacing={2.5}>
    <Paper component="section" aria-labelledby="waveform-automation-title" variant="outlined" sx={{ ...mediaPanelSx, p: 2.5 }}><Stack spacing={1.5}>
      <Typography component="h2" variant="h3" id="waveform-automation-title">逐音轨波形</Typography>
      <Typography variant="body2" color="text.secondary">分别为片源中的每条音轨生成波形，播放器切换音轨时使用对应的结果。自动生成默认关闭，可在电影、剧集或混合媒体库设置中启用。</Typography>
      <Typography variant="body2" color="text.secondary">成品随片源持久保存，需要媒体目录可写。普通生成任务复用已有成品；只有明确重新生成才替换。片源变化后的旧成品仍保留，播放器会隐藏可能错位的波形。</Typography>
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button onClick={onLibraries} disabled={busy}>打开媒体库设置</Button><Button onClick={onTasks} disabled={busy}>查看波形生成任务</Button></Stack>
    </Stack></Paper>
    {request.error != null && <ErrorNotice error={request.error} />}
    {request.pending && <Alert severity="warning">为「{pendingLibrary?.Name || '此前选择的媒体库'}」提交的波形生成请求尚未确认。核对会使用原请求编号，当前筛选条件不会改变该请求。<Button color="inherit" disabled={busy} onClick={() => void retry()}>核对波形生成请求</Button></Alert>}
    {request.notice && <Alert severity="success">{request.notice}</Alert>}
    {request.receipt && <Paper variant="outlined" sx={{ ...mediaPanelSx, p: 2.5 }}><AudioWaveformRunProgress runId={request.receipt.RunId} taskId={request.receipt.TaskId} onFinished={resource.reload} onBusyChange={setStopping} onTasks={onTasks} /></Paper>}
    <Paper component="section" aria-labelledby="waveform-results-title" variant="outlined" sx={mediaPanelSx}>
      <Stack spacing={2} sx={{ p: 2.5 }}>
        <Typography component="h2" variant="h3" id="waveform-results-title">音轨波形任务记录</Typography>
        <Box component="form" onSubmit={(event) => { event.preventDefault(); setQuery(search.trim()); setPage(0); }}><Stack direction={{ xs: 'column', md: 'row' }} sx={{ gap: 1.5, alignItems: { xs: 'stretch', md: 'center' }, flexWrap: 'wrap' }}>
          <TextField select size="small" label="媒体库" value={libraryId} disabled={busy} onChange={(event) => { setLibraryId(event.target.value); setPage(0); }} slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }} sx={mediaSelectSx}><MenuItem value="">全部媒体库</MenuItem>{supportedLibraries.map((library) => <MenuItem key={library.Id} value={library.Id}>{library.Name}</MenuItem>)}</TextField>
          <TextField select size="small" label="任务状态" value={state} disabled={busy} onChange={(event) => { setState(event.target.value as AudioWaveformState | ''); setPage(0); }} slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }} sx={mediaSelectSx}><MenuItem value="">全部状态</MenuItem>{audioWaveformStates.map((value) => <MenuItem key={value} value={value}>{audioWaveformStateLabels[value]}</MenuItem>)}</TextField>
          <Box sx={{ width: { xs: '100%', md: 250 } }}><MediaSearchField label="搜索影片名称" value={search} onChange={setSearch} maxLength={256} disabled={busy} /></Box><Button disabled={busy || resource.loading} onClick={resource.reload}>刷新记录</Button>
        </Stack></Box>
        <Stack spacing={1}><Box><Button variant="outlined" disabled={busy || Boolean(request.pending) || !selectedLibrary} onClick={() => void generateLibrary()} startIcon={request.busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{selectedLibrary ? `为「${selectedLibrary.Name}」生成缺失波形` : '为媒体库生成缺失波形'}</Button></Box><Typography variant="caption" color="text.secondary">选择媒体库后可批量生成。批量任务处理该库全部支持的影片，不受名称或状态筛选影响；已有成品会复用。</Typography></Stack>
      </Stack>
      {resource.error != null && <Box sx={{ px: 2.5, pb: 2.5 }}><ErrorNotice error={resource.error} retry={resource.reload} /></Box>}
      {resource.loading && !resource.data && <Box role="status" aria-label="正在加载音轨波形记录" sx={{ px: 2.5, pb: 2.5 }}><Skeleton height={60} /><Skeleton height={60} /></Box>}
      {resource.data?.Items.length === 0 && <Typography variant="body2" color="text.secondary" sx={{ p: 3 }}>没有匹配的影片，请调整筛选条件或扫描支持的媒体库。</Typography>}
      {resource.data && resource.data.Items.length > 0 && <>
        <TableContainer sx={{ display: { xs: 'none', md: 'block' }, borderRadius: 0 }}><Table aria-label="音轨波形记录"><TableHead><TableRow><TableCell>影片</TableCell><TableCell>当前音轨数</TableCell><TableCell>最近任务</TableCell><TableCell align="right">操作</TableCell></TableRow></TableHead><TableBody>{resource.data.Items.map((item) => <TableRow key={item.ItemId}>
          <TableCell component="th" scope="row"><Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{item.Name || '未命名影片'}</Typography><Typography variant="caption" color="text.secondary">{libraries.find((library) => library.Id === item.LibraryId)?.Name || '媒体库'}</Typography></TableCell><TableCell>{item.AudioStreamCount}</TableCell><TableCell><Status item={item} /></TableCell><TableCell align="right"><Button disabled={busy} onClick={() => setDetail(item)} aria-label={`管理「${item.Name}」的音轨波形`}>管理波形</Button></TableCell>
        </TableRow>)}</TableBody></Table></TableContainer>
        <Box component="ul" aria-label="音轨波形记录" sx={{ display: { xs: 'block', md: 'none' }, listStyle: 'none', p: 0, m: 0 }}>{resource.data.Items.map((item) => <Box component="li" key={item.ItemId} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}><Stack spacing={1.5}><Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{item.Name || '未命名影片'}</Typography><Typography variant="caption" color="text.secondary">{libraries.find((library) => library.Id === item.LibraryId)?.Name || '媒体库'} · {item.AudioStreamCount} 条音轨</Typography><Stack direction="row" sx={{ gap: 1, alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap' }}><Status item={item} /><Button disabled={busy} onClick={() => setDetail(item)} aria-label={`管理「${item.Name}」的音轨波形`}>管理波形</Button></Stack></Stack></Box>)}</Box>
      </>}
      {resource.data && <TablePagination component="div" count={resource.data.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} labelRowsPerPage="每页条目" labelDisplayedRows={({ from, to, count }) => `${from}–${to} / ${count}`} disabled={busy || resource.loading} onPageChange={(_, value) => setPage(value)} sx={{ borderTop: 1, borderColor: 'divider' }} />}
    </Paper>
    {detail && <AudioWaveformDialog key={detail.ItemId} itemId={detail.ItemId} itemName={detail.Name} currentUserId={currentUserId} onClose={closeDetail} onTasks={onTasks} onActivityChange={setDialogActivity} />}
  </Stack>;
}
