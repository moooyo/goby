import { useCallback, useEffect, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Paper, Skeleton, Stack, Typography } from '@mui/material';
import { ErrorNotice } from './components';
import { audioWaveformApi } from './audioWaveformApi';
import { activeAudioWaveform, audioWaveformStateLabels } from './audioWaveform';
import type { AudioWaveformActivity, AudioWaveformDetail } from './audioWaveform';
import { AudioWaveformRunProgress } from './AudioWaveformRunProgress';
import { useAudioWaveformRequest } from './useAudioWaveformRequest';
import { useTaskResource } from './useTaskResource';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';
import { ticksToSeconds } from './introApi';

const ignoreGuard: UserNavigationGuardChange = () => {};
export function AudioWaveformDialog({ itemId, itemName, currentUserId, onClose, onTasks, onNavigationGuardChange = ignoreGuard, onActivityChange }: {
  itemId: string; itemName: string; currentUserId: string; onClose: () => void; onTasks?: () => void;
  onNavigationGuardChange?: UserNavigationGuardChange; onActivityChange?: (value: AudioWaveformActivity) => void;
}) {
  const load = useCallback((signal: AbortSignal) => audioWaveformApi.get(itemId, { signal }), [itemId]);
  const resource = useTaskResource<AudioWaveformDetail>({ key: itemId, load, poll: activeAudioWaveform });
  const item = resource.data;
  const request = useAudioWaveformRequest(currentUserId, itemId);
  const [confirming, setConfirming] = useState(false);
  const [stopping, setStopping] = useState(false);
  const busy = request.busy || stopping;
  const pending = Boolean(request.pending);
  const hasArtifact = Boolean(item && (item.Artifact.Available || item.Artifact.Stale || item.Artifact.Tracks.length > 0));
  const canGenerate = Boolean(item && item.DurationTicks > 0 && item.AudioStreamCount > 0) && !resource.loading && !busy && !pending && !activeAudioWaveform(item);
  const runId = request.receipt?.RunId || item?.RunId;
  useUserDraftNavigation(pending, busy, onNavigationGuardChange, '离开音轨波形页面？尚未确认的生成请求会保留供下次核对。');
  useEffect(() => { onActivityChange?.({ pending, busy }); }, [pending, busy, onActivityChange]);

  async function generate(force: boolean) {
    if (!canGenerate) return;
    const accepted = await request.start({ LibraryIds: [], ItemIds: [itemId], Force: force });
    setConfirming(false); if (accepted) resource.reload();
  }
  async function retry() { if (await request.retry()) resource.reload(); }
  function close() { if (!busy) onClose(); }

  return <>
    <Dialog open fullWidth maxWidth="sm" onClose={close} aria-labelledby="waveform-editor-title">
      <DialogTitle id="waveform-editor-heading"><Typography component="span" variant="h3" id="waveform-editor-title">音轨波形</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{itemName}</Typography></DialogTitle>
      <DialogContent aria-busy={resource.loading || busy}><Stack spacing={2.5} sx={{ pt: 0.5 }}>
        {resource.error != null && <ErrorNotice error={resource.error} retry={resource.reload} />}
        {request.error != null && <ErrorNotice error={request.error} />}
        {request.pending && <Alert severity="warning">上一次{request.pending.Force ? '重新生成' : '生成'}请求尚未确认。核对将使用原请求编号，不会重复提交。<Button color="inherit" disabled={busy} onClick={() => void retry()}>核对波形生成请求</Button></Alert>}
        {request.notice && <Alert severity="success">{request.notice}</Alert>}
        {resource.loading && !item && <Skeleton variant="rounded" height={220} aria-label="正在加载音轨波形" />}
        {item && <>
          <Paper component="section" aria-label="已保存的音轨波形" variant="outlined" sx={{ p: 2 }}><Stack spacing={1.5}>
            <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">已保存的成品</Typography><Chip size="small" variant="outlined" color={item.Artifact.Stale ? 'warning' : item.Artifact.Available ? 'success' : 'default'} label={item.Artifact.Stale ? '已过期' : item.Artifact.Available ? '可以显示' : '尚无成品'} /></Stack>
            {item.Artifact.Stale && <Alert severity="warning">片源已经变化，已有波形仍保存在片源旁。播放器不会显示这些可能错位的波形；需要更新时请明确重新生成。</Alert>}
            {item.Artifact.Tracks.length > 0 ? <>
              <Typography variant="body2">已保存 {item.Artifact.Tracks.length} 条音轨的波形，覆盖影片时长 {ticksToSeconds(item.Artifact.DurationTicks)} 秒。</Typography>
              <Stack component="ul" aria-label="已生成波形的音轨" spacing={1} sx={{ listStyle: 'none', m: 0, p: 0 }}>{item.Artifact.Tracks.map((track) => <Box component="li" key={track.StreamIndex} sx={{ borderTop: 1, borderColor: 'divider', pt: 1 }}><Typography variant="body2" sx={{ fontWeight: 600 }}>音轨 {track.StreamIndex}</Typography><Typography variant="caption" color="text.secondary">{track.Channels} 声道 · {(track.SampleRate / 1000).toLocaleString()} kHz{track.ChannelLayout ? ` · ${track.ChannelLayout}` : ''}</Typography></Box>)}</Stack>
            </> : !item.Artifact.Stale && <Typography variant="body2" color="text.secondary">尚无已保存的波形，生成时会分别处理当前片源的每条音轨。</Typography>}
          </Stack></Paper>
          <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography variant="body2">最近任务：</Typography><Chip size="small" label={audioWaveformStateLabels[item.State]} color={item.State === 'failed' ? 'error' : activeAudioWaveform(item) ? 'primary' : 'default'} /></Stack>
          {item.State === 'ready' && item.Reused && <Typography variant="body2" color="text.secondary">本次复用了已有成品，没有重复生成。</Typography>}
          {item.ErrorCode && <Alert severity="warning">任务未能完成，请在任务中心查看详情。<Typography variant="caption" component="div" sx={{ overflowWrap: 'anywhere' }}>错误代码：{item.ErrorCode}</Typography></Alert>}
          {item.AudioStreamCount === 0 || item.DurationTicks === 0 ? <Alert severity="info">当前片源缺少可用的音轨或时长，暂时无法生成。请检查片源并重新扫描媒体库，已有成品会保留。</Alert>
            : <Typography variant="body2" color="text.secondary">当前片源有 {item.AudioStreamCount} 条音轨，时长 {ticksToSeconds(item.DurationTicks)} 秒。</Typography>}
          <Typography variant="body2" color="text.secondary">波形随片源持久保存，需要媒体目录可写。关闭自动生成、重新扫描或清理分析缓存都不会删除已有成品；只有明确重新生成才替换。</Typography>
          <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}><Button variant="outlined" disabled={!canGenerate || hasArtifact} onClick={() => void generate(false)} startIcon={request.busy ? <CircularProgress size={16} color="inherit" /> : undefined}>生成缺失波形</Button><Button variant="outlined" color="warning" disabled={!canGenerate || !hasArtifact} onClick={() => setConfirming(true)}>重新生成波形</Button></Stack>
        </>}
        {runId && <AudioWaveformRunProgress runId={runId} taskId={request.receipt?.TaskId} onFinished={resource.reload} onBusyChange={setStopping} onTasks={onTasks} />}
      </Stack></DialogContent>
      <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}>{onTasks && <Button disabled={busy} onClick={onTasks}>查看波形生成任务</Button>}<Button disabled={busy || resource.loading} onClick={resource.reload}>刷新</Button><Button disabled={busy} onClick={close}>关闭</Button></DialogActions>
    </Dialog>
    <Dialog open={confirming} onClose={() => { if (!busy) setConfirming(false); }} fullWidth maxWidth="xs" aria-labelledby="waveform-regenerate-title">
      <DialogTitle id="waveform-regenerate-title">重新生成音轨波形？</DialogTitle><DialogContent><Typography>将重新读取「{itemName}」的每条音轨，成功后替换已有波形。失败或取消会保留原成品。</Typography></DialogContent><DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus disabled={busy} onClick={() => setConfirming(false)}>保留现有波形</Button><Button variant="contained" color="warning" disabled={!canGenerate} onClick={() => void generate(true)} startIcon={request.busy ? <CircularProgress size={16} color="inherit" /> : undefined}>确认重新生成波形</Button></DialogActions>
    </Dialog>
  </>;
}
