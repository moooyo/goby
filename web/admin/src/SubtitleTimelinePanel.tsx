import { useCallback, useEffect, useState } from 'react';
import { Box, Alert, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, MenuItem, Paper, Skeleton, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TablePagination, TableRow, TextField, Typography } from '@mui/material';
import type { Library } from './api';
import { ErrorNotice } from './components';
import { subtitleTimelineApi } from './subtitleTimelineApi';
import { activeSubtitleTimeline, subtitleTimelineStates, subtitleTimelineStateLabels } from './subtitleTimeline';
import type { SubtitleTimelineActivity, SubtitleTimelineItem, SubtitleTimelinePage, SubtitleTimelineState } from './subtitleTimeline';
import { SubtitleTimelineDialog } from './SubtitleTimelineDialog';
import { AudioWaveformRunProgress } from './AudioWaveformRunProgress';
import { useSubtitleTimelineRequest } from './useSubtitleTimelineRequest';
import { useTaskResource } from './useTaskResource';
import { mediaPanelSx, MediaSearchField, mediaSelectSx } from './MediaPagePrimitives';

const poll = (value: SubtitleTimelinePage) => value.Items.some(activeSubtitleTimeline);
function Status({ item }: { item: SubtitleTimelineItem }) { return <Chip size="small" label={subtitleTimelineStateLabels[item.State]} color={item.State === 'failed' ? 'error' : activeSubtitleTimeline(item) ? 'primary' : 'default'} />; }

export function SubtitleTimelinePanel({ currentUserId, libraries, onLibraries, onTasks, onActivityChange, visible, available, reasons = [] }: {
  currentUserId: string; libraries: Library[]; onLibraries: () => void; onTasks: () => void; onActivityChange: (value: SubtitleTimelineActivity) => void; visible: boolean;
  available?: boolean; reasons?: string[];
}) {
  const [libraryId, setLibraryId] = useState('');
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');
  const [state, setState] = useState<SubtitleTimelineState | ''>('');
  const [page, setPage] = useState(0);
  const [detail, setDetail] = useState<SubtitleTimelineItem>();
  const [dialogActivity, setDialogActivity] = useState<SubtitleTimelineActivity>({ pending: false, busy: false });
  const [stopping, setStopping] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const request = useSubtitleTimelineRequest(currentUserId);
  const load = useCallback((signal: AbortSignal) => subtitleTimelineApi.items({ libraryId, search: query, state, start: page * 25 }, { signal }), [libraryId, query, state, page]);
  const resource = useTaskResource({ key: JSON.stringify([libraryId, query, state, page]), load, poll, enabled: visible });
  const supportedLibraries = libraries.filter((library) => ['movies', 'tvshows', 'mixed'].includes(library.CollectionType));
  const selectedLibrary = supportedLibraries.find((library) => library.Id === libraryId);
  const pendingLibrary = supportedLibraries.find((library) => library.Id === request.pending?.LibraryIds[0]);
  const busy = request.busy || stopping;
  useEffect(() => { onActivityChange({ pending: Boolean(request.pending) || dialogActivity.pending, busy: busy || dialogActivity.busy }); }, [request.pending, dialogActivity, busy, onActivityChange]);
  useEffect(() => {
    if (resource.data && page > 0 && page * 25 >= resource.data.TotalRecordCount) setPage(Math.max(0, Math.ceil(resource.data.TotalRecordCount / 25) - 1));
  }, [resource.data, page]);

  async function generateLibrary(force: boolean) {
    if (busy || request.pending || !selectedLibrary || available === false) return;
    if (await request.start({ LibraryIds: [selectedLibrary.Id], ItemIds: [], Force: force })) resource.reload();
    setConfirming(false);
  }
  async function retry() { if (await request.retry()) resource.reload(); }
  function closeDetail() { setDetail(undefined); setDialogActivity({ pending: false, busy: false }); resource.reload(); }

  return <Stack spacing={2.5}>
    <Paper component="section" aria-labelledby="subtitle-timeline-automation-title" variant="outlined" sx={{ ...mediaPanelSx, p: 2.5 }}><Stack spacing={1.5}>
      <Typography component="h2" variant="h3" id="subtitle-timeline-automation-title">位图字幕时间轴</Typography>
      <Typography variant="body2" color="text.secondary">为内封 PGS/DVD 字幕生成显示时间区间，在播放器媒体信息页展示每条字幕轨的分布。自动生成默认关闭，可在电影、剧集或混合媒体库设置中启用。</Typography>
      <Typography variant="body2" color="text.secondary">成品随片源持久保存，需要媒体目录可写。普通生成任务复用已有成品；只有明确重新生成才替换。片源变化后的旧成品仍保留，播放器会隐藏可能错位的时间轴。</Typography>
      <Typography variant="body2" color="text.secondary">支持内封 PGS/DVD、外挂 SUP 及多语言 IDX/SUB 时间轴；未生成或没有有效数据时，播放器隐藏对应行。生成文件持久保存在片源旁，仅明确重生成时替换。外挂位图时间轴不代表已支持其字幕播放。</Typography>
      {available === false && <Alert severity="info">字幕时间轴生成能力当前不可用。{reasons.length > 0 ? ` ${reasons.join('；')}` : '请检查媒体分析配置和任务中心。'}</Alert>}
      <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button onClick={onLibraries} disabled={busy}>打开媒体库设置</Button><Button onClick={onTasks} disabled={busy}>查看时间轴生成任务</Button></Stack>
    </Stack></Paper>
    {request.error != null && <ErrorNotice error={request.error} />}
    {request.pending && <Alert severity="warning">为「{pendingLibrary?.Name || '此前选择的媒体库'}」提交的时间轴{request.pending.Force ? '重新生成' : '生成'}请求尚未确认。核对会使用原请求编号，当前筛选条件不会改变该请求。<Button color="inherit" disabled={busy} onClick={() => void retry()}>核对时间轴生成请求</Button></Alert>}
    {request.notice && <Alert severity="success">{request.notice}</Alert>}
    {request.receipt && <Paper variant="outlined" sx={{ ...mediaPanelSx, p: 2.5 }}><AudioWaveformRunProgress subject="subtitle-timeline" runId={request.receipt.RunId} taskId={request.receipt.TaskId} onFinished={resource.reload} onBusyChange={setStopping} onTasks={onTasks} /></Paper>}
    <Paper component="section" aria-labelledby="subtitle-timeline-results-title" variant="outlined" sx={mediaPanelSx}>
      <Stack spacing={2} sx={{ p: 2.5 }}>
        <Typography component="h2" variant="h3" id="subtitle-timeline-results-title">字幕时间轴任务记录</Typography>
        <Box component="form" onSubmit={(event) => { event.preventDefault(); setQuery(search.trim()); setPage(0); }}><Stack direction={{ xs: 'column', md: 'row' }} sx={{ gap: 1.5, alignItems: { xs: 'stretch', md: 'center' }, flexWrap: 'wrap' }}>
          <TextField select size="small" label="媒体库" value={libraryId} disabled={busy} onChange={(event) => { setLibraryId(event.target.value); setPage(0); }} slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }} sx={mediaSelectSx}><MenuItem value="">全部媒体库</MenuItem>{supportedLibraries.map((library) => <MenuItem key={library.Id} value={library.Id}>{library.Name}</MenuItem>)}</TextField>
          <TextField select size="small" label="任务状态" value={state} disabled={busy} onChange={(event) => { setState(event.target.value as SubtitleTimelineState | ''); setPage(0); }} slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }} sx={mediaSelectSx}><MenuItem value="">全部状态</MenuItem>{subtitleTimelineStates.map((value) => <MenuItem key={value} value={value}>{subtitleTimelineStateLabels[value]}</MenuItem>)}</TextField>
          <Box sx={{ width: { xs: '100%', md: 250 } }}><MediaSearchField label="搜索影片名称" value={search} onChange={setSearch} maxLength={256} disabled={busy} /></Box><Button disabled={busy || resource.loading} onClick={resource.reload}>刷新记录</Button>
        </Stack></Box>
        <Stack spacing={1}><Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}><Button variant="outlined" disabled={busy || Boolean(request.pending) || !selectedLibrary || available === false} onClick={() => void generateLibrary(false)} startIcon={request.busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{selectedLibrary ? `为「${selectedLibrary.Name}」生成缺失时间轴` : '为媒体库生成缺失时间轴'}</Button><Button color="warning" variant="outlined" disabled={busy || Boolean(request.pending) || !selectedLibrary || available === false} onClick={() => setConfirming(true)}>重新生成此库时间轴</Button></Stack><Typography variant="caption" color="text.secondary">选择媒体库后可批量生成。批量任务处理该库全部支持的影片，不受名称或状态筛选影响；普通生成复用已有成品。</Typography></Stack>
      </Stack>
      {resource.error != null && <Box sx={{ px: 2.5, pb: 2.5 }}><ErrorNotice error={resource.error} retry={resource.reload} /></Box>}
      {resource.loading && !resource.data && <Box role="status" aria-label="正在加载字幕时间轴记录" sx={{ px: 2.5, pb: 2.5 }}><Skeleton height={60} /><Skeleton height={60} /></Box>}
      {resource.data?.Items.length === 0 && <Typography variant="body2" color="text.secondary" sx={{ p: 3 }}>没有匹配的影片，请调整筛选条件或扫描支持的媒体库。</Typography>}
      {resource.data && resource.data.Items.length > 0 && <>
        <TableContainer sx={{ display: { xs: 'none', md: 'block' }, borderRadius: 0 }}><Table aria-label="字幕时间轴记录"><TableHead><TableRow><TableCell>影片</TableCell><TableCell>当前字幕轨数</TableCell><TableCell>最近任务</TableCell><TableCell align="right">操作</TableCell></TableRow></TableHead><TableBody>{resource.data.Items.map((item) => <TableRow key={item.ItemId}>
          <TableCell component="th" scope="row"><Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{item.Name || '未命名影片'}</Typography><Typography variant="caption" color="text.secondary">{libraries.find((library) => library.Id === item.LibraryId)?.Name || '媒体库'}</Typography></TableCell><TableCell>{item.SubtitleStreamCount}</TableCell><TableCell><Status item={item} /></TableCell><TableCell align="right"><Button disabled={busy} onClick={() => setDetail(item)} aria-label={`管理「${item.Name}」的字幕时间轴`}>管理时间轴</Button></TableCell>
        </TableRow>)}</TableBody></Table></TableContainer>
        <Box component="ul" aria-label="字幕时间轴记录" sx={{ display: { xs: 'block', md: 'none' }, listStyle: 'none', p: 0, m: 0 }}>{resource.data.Items.map((item) => <Box component="li" key={item.ItemId} sx={{ p: 2.5, borderTop: 1, borderColor: 'divider' }}><Stack spacing={1.5}><Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>{item.Name || '未命名影片'}</Typography><Typography variant="caption" color="text.secondary">{libraries.find((library) => library.Id === item.LibraryId)?.Name || '媒体库'} · {item.SubtitleStreamCount} 条字幕轨</Typography><Stack direction="row" sx={{ gap: 1, alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap' }}><Status item={item} /><Button disabled={busy} onClick={() => setDetail(item)} aria-label={`管理「${item.Name}」的字幕时间轴`}>管理时间轴</Button></Stack></Stack></Box>)}</Box>
      </>}
      {resource.data && <TablePagination component="div" count={resource.data.TotalRecordCount} page={page} rowsPerPage={25} rowsPerPageOptions={[25]} labelRowsPerPage="每页条目" labelDisplayedRows={({ from, to, count }) => `${from}–${to} / ${count}`} disabled={busy || resource.loading} onPageChange={(_, value) => setPage(value)} sx={{ borderTop: 1, borderColor: 'divider' }} />}
    </Paper>
    {detail && <SubtitleTimelineDialog key={detail.ItemId} itemId={detail.ItemId} itemName={detail.Name} currentUserId={currentUserId} onClose={closeDetail} onTasks={onTasks} onActivityChange={setDialogActivity} />}
    <Dialog open={confirming} onClose={() => { if (!busy) setConfirming(false); }} fullWidth maxWidth="xs" aria-labelledby="subtitle-timeline-library-regenerate-title"><DialogTitle id="subtitle-timeline-library-regenerate-title">重新生成此库字幕时间轴？</DialogTitle><DialogContent><Typography>将重新处理「{selectedLibrary?.Name}」中所有支持的影片，不受名称或状态筛选影响。每项成功后替换其已有时间轴；失败或取消会保留原成品。</Typography></DialogContent><DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus disabled={busy} onClick={() => setConfirming(false)}>保留现有时间轴</Button><Button color="warning" variant="contained" disabled={busy || Boolean(request.pending) || !selectedLibrary || available === false} onClick={() => void generateLibrary(true)} startIcon={request.busy ? <CircularProgress size={16} color="inherit" /> : undefined}>确认重新生成时间轴</Button></DialogActions></Dialog>
  </Stack>;
}
