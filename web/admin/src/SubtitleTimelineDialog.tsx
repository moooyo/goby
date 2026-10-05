import { useCallback, useEffect, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Paper, Skeleton, Stack, Typography } from '@mui/material';
import { ErrorNotice } from './components';
import { subtitleTimelineApi } from './subtitleTimelineApi';
import { activeSubtitleTimeline, subtitleTimelineFailure, subtitleTimelineStateLabels } from './subtitleTimeline';
import type { SubtitleTimelineActivity, SubtitleTimelineDetail } from './subtitleTimeline';
import { AudioWaveformRunProgress } from './AudioWaveformRunProgress';
import { useSubtitleTimelineRequest } from './useSubtitleTimelineRequest';
import { useTaskResource } from './useTaskResource';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { ticksToSeconds } from './introApi';

const ignoreGuard: UserNavigationGuardChange = () => {};
export function SubtitleTimelineDialog({ itemId, itemName, currentUserId, onClose, onTasks, onNavigationGuardChange = ignoreGuard, onActivityChange }: {
  itemId: string; itemName: string; currentUserId: string; onClose: () => void; onTasks?: () => void;
  onNavigationGuardChange?: UserNavigationGuardChange; onActivityChange?: (value: SubtitleTimelineActivity) => void;
}) {
  const load = useCallback((signal: AbortSignal) => subtitleTimelineApi.get(itemId, { signal }), [itemId]);
  const resource = useTaskResource<SubtitleTimelineDetail>({ key: itemId, load, poll: activeSubtitleTimeline });
  const item = resource.data;
  const request = useSubtitleTimelineRequest(currentUserId, itemId);
  const [confirming, setConfirming] = useState(false);
  const [stopping, setStopping] = useState(false);
  const busy = request.busy || stopping;
  const pending = Boolean(request.pending);
  const hasArtifact = Boolean(item && (item.Artifact.Available || item.Artifact.Stale || item.Artifact.Tracks.length > 0));
  const canGenerate = Boolean(item && item.DurationTicks > 0 && item.SubtitleStreamCount > 0) && !resource.loading && !busy && !pending && !activeSubtitleTimeline(item);
  const runId = request.receipt?.RunId || item?.RunId;
  useUserDraftNavigation(pending, busy, onNavigationGuardChange, '离开字幕时间轴页面？尚未确认的生成请求会保留供下次核对。');
  useEffect(() => { onActivityChange?.({ pending, busy }); }, [pending, busy, onActivityChange]);

  async function generate(force: boolean) {
    if (!canGenerate) return;
    const accepted = await request.start({ LibraryIds: [], ItemIds: [itemId], Force: force });
    setConfirming(false); if (accepted) resource.reload();
  }
  async function retry() { if (await request.retry()) resource.reload(); }
  function close() { if (!busy) onClose(); }

  return <>
    <Dialog open fullWidth maxWidth="sm" onClose={close} aria-labelledby="subtitle-timeline-editor-title">
      <DialogTitle id="subtitle-timeline-editor-heading"><Typography component="span" variant="h3" id="subtitle-timeline-editor-title">字幕时间轴</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{itemName}</Typography></DialogTitle>
      <DialogContent aria-busy={resource.loading || busy}><Stack spacing={2.5} sx={{ pt: 0.5 }}>
        {resource.error != null && <ErrorNotice error={resource.error} retry={resource.reload} />}
        {request.error != null && <ErrorNotice error={request.error} />}
        {request.pending && <Alert severity="warning">上一次{request.pending.Force ? '重新生成' : '生成'}请求尚未确认。核对将使用原请求编号，不会重复提交。<Button color="inherit" disabled={busy} onClick={() => void retry()}>核对时间轴生成请求</Button></Alert>}
        {request.notice && <Alert severity="success">{request.notice}</Alert>}
        {resource.loading && !item && <Skeleton variant="rounded" height={220} aria-label="正在加载字幕时间轴" />}
        {item && <>
          <Paper component="section" aria-label="已保存的字幕时间轴" variant="outlined" sx={{ p: 2 }}><Stack spacing={1.5}>
            <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">已保存的成品</Typography><Chip size="small" variant="outlined" color={item.Artifact.Stale ? 'warning' : item.Artifact.Available ? 'success' : 'default'} label={item.Artifact.Stale ? '已过期' : item.Artifact.Available ? '可以显示' : '尚无成品'} /></Stack>
            {item.Artifact.Stale && <Alert severity="warning">片源已经变化，已有时间轴仍保存在片源旁。播放器不会显示这些可能错位的时间轴；需要更新时请明确重新生成。</Alert>}
            {item.Artifact.Tracks.length > 0 ? <>
              <Typography variant="body2">已保存 {item.Artifact.Tracks.length} 条字幕轨的时间轴，影片时长 {ticksToSeconds(item.Artifact.DurationTicks)} 秒。</Typography>
              <Stack component="ul" aria-label="已生成时间轴的字幕轨" spacing={1} sx={{ listStyle: 'none', m: 0, p: 0 }}>{item.Artifact.Tracks.map((track) => <Box component="li" key={track.StreamIndex} sx={{ borderTop: 1, borderColor: 'divider', pt: 1 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>字幕轨 {track.StreamIndex}</Typography><Typography variant="caption" color="text.secondary">{track.Codec.toUpperCase()} · {track.IntervalCount.toLocaleString()} 段字幕</Typography>{track.Warnings.length > 0 && <Typography variant="caption" component="div" color="warning.main" sx={{ overflowWrap: 'anywhere' }}>解析说明：{track.Warnings.join('；')}</Typography>}</Box>)}</Stack>
            </> : !item.Artifact.Stale && <Typography variant="body2" color="text.secondary">尚无已保存的时间轴，生成时会分别处理当前片源中支持的内封 PGS/DVD 字幕轨。</Typography>}
          </Stack></Paper>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography variant="body2">最近任务：</Typography><Chip size="small" label={subtitleTimelineStateLabels[item.State]} color={item.State === 'failed' ? 'error' : activeSubtitleTimeline(item) ? 'primary' : 'default'} /></Stack>
          {item.State === 'ready' && item.Reused && <Typography variant="body2" color="text.secondary">本次复用了已有成品，没有重复生成。</Typography>}
          {item.ErrorCode && <Alert severity="warning">{subtitleTimelineFailure(item.ErrorCode)}<Typography variant="caption" component="div" sx={{ overflowWrap: 'anywhere' }}>错误代码：{item.ErrorCode}</Typography></Alert>}
          {item.SubtitleStreamCount === 0 || item.DurationTicks === 0 ? <Alert severity="info">当前片源缺少可处理的内封 PGS/DVD 字幕轨或时长，暂时无法生成。请检查片源并重新扫描媒体库，已有成品会保留。</Alert>
            : <Typography variant="body2" color="text.secondary">当前片源有 {item.SubtitleStreamCount} 条可处理的字幕轨，时长 {ticksToSeconds(item.DurationTicks)} 秒。</Typography>}
          <Typography variant="body2" color="text.secondary">播放器只显示有效的时间轴；未生成、生成失败、不支持或片源已变化时隐藏对应时间轴行，字幕选择和播放照常保留。</Typography>
          <Typography variant="body2" color="text.secondary">时间轴随片源持久保存，需要媒体目录可写。关闭自动生成、重新扫描或清理分析缓存都不会删除已有成品；只有明确重新生成才替换。</Typography>
          <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}><Button variant="outlined" disabled={!canGenerate || hasArtifact} onClick={() => void generate(false)} startIcon={request.busy ? <CircularProgress size={16} color="inherit" /> : undefined}>生成缺失时间轴</Button><Button variant="outlined" color="warning" disabled={!canGenerate || !hasArtifact} onClick={() => setConfirming(true)}>重新生成时间轴</Button></Stack>
        </>}
        {runId && <AudioWaveformRunProgress subject="subtitle-timeline" runId={runId} taskId={request.receipt?.TaskId} onFinished={resource.reload} onBusyChange={setStopping} onTasks={onTasks} />}
      </Stack></DialogContent>
      <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}>{onTasks && <Button disabled={busy} onClick={onTasks}>查看时间轴生成任务</Button>}<Button disabled={busy || resource.loading} onClick={resource.reload}>刷新</Button><Button disabled={busy} onClick={close}>关闭</Button></DialogActions>
    </Dialog>
    <Dialog open={confirming} onClose={() => { if (!busy) setConfirming(false); }} fullWidth maxWidth="xs" aria-labelledby="subtitle-timeline-regenerate-title">
      <DialogTitle id="subtitle-timeline-regenerate-title">重新生成字幕时间轴？</DialogTitle><DialogContent><Typography>将重新读取「{itemName}」中支持的内封 PGS/DVD 字幕轨，成功后替换已有时间轴。失败或取消会保留原成品。</Typography></DialogContent><DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus disabled={busy} onClick={() => setConfirming(false)}>保留现有时间轴</Button><Button variant="contained" color="warning" disabled={!canGenerate} onClick={() => void generate(true)} startIcon={request.busy ? <CircularProgress size={16} color="inherit" /> : undefined}>确认重新生成时间轴</Button></DialogActions>
    </Dialog>
  </>;
}
