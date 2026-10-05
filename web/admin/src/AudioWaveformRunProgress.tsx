import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Stack, Typography } from '@mui/material';
import { adminApi, ApiError, isAbortError } from './api';
import type { TaskRunDetail } from './api';
import { ErrorNotice } from './components';
import { isActiveTaskRun, RunProgress, RunStatusChip } from './TaskRunDialog';
import { useTaskResource } from './useTaskResource';

const poll = (value: TaskRunDetail) => isActiveTaskRun(value.Run);
export function AudioWaveformRunProgress({ runId, taskId, onFinished, onBusyChange, onTasks, onActiveChange, subject = 'waveform' }: {
  runId: string; taskId?: string; onFinished: () => void; onBusyChange: (busy: boolean) => void; onTasks?: () => void; onActiveChange?: (active: boolean) => void; subject?: 'waveform' | 'credits' | 'subtitle-timeline';
}) {
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<unknown>();
  const mutation = useRef<AbortController | undefined>(undefined);
  const finished = useRef('');
  const load = useCallback(async (signal: AbortSignal) => {
    const value = await adminApi.getTaskRun(runId, { Limit: 25 }, { signal });
    if (taskId && value.Run.TaskId !== taskId) throw new ApiError('任务记录与本次处理请求不一致，请重新加载。', { code: 'invalid_response' });
    return value;
  }, [runId, taskId]);
  const resource = useTaskResource({ key: runId, load, poll, enabled: !busy });
  const run = resource.data?.Run;
  useEffect(() => { onBusyChange(busy); }, [busy, onBusyChange]);
  useEffect(() => { if (run) onActiveChange?.(isActiveTaskRun(run)); }, [run, onActiveChange]);
  useEffect(() => () => mutation.current?.abort(), []);
  useEffect(() => {
    if (run && !isActiveTaskRun(run) && finished.current !== run.Id) { finished.current = run.Id; onFinished(); }
  }, [run, onFinished]);

  async function stop() {
    if (mutation.current || !run || !isActiveTaskRun(run) || run.State === 'stopping') return;
    const controller = new AbortController(); mutation.current = controller; setBusy(true); setError(undefined);
    try { await adminApi.cancelTaskRun(run.Id, { signal: controller.signal }); if (!controller.signal.aborted) { setConfirming(false); resource.reload(); } }
    catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); }
    finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(false); }
  }

  return <Box component="section" aria-label={subject === 'credits' ? '片尾识别任务进度' : subject === 'subtitle-timeline' ? '字幕时间轴生成任务进度' : '波形生成任务进度'}><Stack spacing={1.5}>
    <Typography component="h3" variant="h4">{subject === 'credits' ? '识别任务' : '生成任务'}</Typography>
    {resource.loading && !run && <Typography role="status" variant="body2">正在读取任务进度…</Typography>}
    {resource.error != null && <ErrorNotice error={resource.error} retry={resource.reload} />}
    {error != null && !confirming && <ErrorNotice error={error} />}
    {run && <><RunStatusChip state={run.State} /><RunProgress run={run} compact />
      {run.State === 'stopping' && <Alert severity="info">已请求停止，等待正在处理的条目结束。</Alert>}
      {run.ErrorMessage && <Alert severity="warning">{run.ErrorMessage}</Alert>}
      {resource.data?.Children.Items.filter((child) => child.ErrorCode || child.ErrorMessage).map((child) => <Alert severity="warning" key={child.Id}>{child.LibraryName || '处理条目'}：{child.ErrorMessage || child.ErrorCode}</Alert>)}
    </>}
    <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button disabled={busy || resource.loading || !run || !isActiveTaskRun(run) || run.State === 'stopping'} onClick={() => setConfirming(true)}>停止本批任务</Button><Button disabled={busy || resource.loading} onClick={resource.reload}>刷新任务进度</Button>{onTasks && <Button disabled={busy} onClick={onTasks}>打开任务中心</Button>}</Stack>
  </Stack>
    <Dialog open={confirming} onClose={() => { if (!busy) setConfirming(false); }} fullWidth maxWidth="xs" aria-labelledby={`${subject}-stop-title`}>
      <DialogTitle id={`${subject}-stop-title`}>{subject === 'credits' ? '停止这批片尾识别任务？' : subject === 'subtitle-timeline' ? '停止这批字幕时间轴生成任务？' : '停止这批波形生成任务？'}</DialogTitle><DialogContent><Stack spacing={2}><Typography>{subject === 'credits' ? '此操作会停止该批次尚未完成的条目，已经发布的检测结果和人工标记会保留。' : '此操作会停止该批次尚未完成的所有条目，已经生成的成品会保留。若正在重新生成，原成品也会保留。'}</Typography>{error != null && <ErrorNotice error={error} />}</Stack></DialogContent><DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus disabled={busy} onClick={() => setConfirming(false)}>继续处理</Button><Button color="warning" variant="contained" disabled={busy || !run || !isActiveTaskRun(run)} onClick={() => void stop()} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>确认停止</Button></DialogActions>
    </Dialog>
  </Box>;
}
