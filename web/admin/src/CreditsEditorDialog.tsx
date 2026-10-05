import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Paper, Skeleton, Stack, TextField, Typography } from '@mui/material';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { creditsApi } from './creditsApi';
import type { CreditsPoint, ItemCredits } from './creditsApi';
import { creditsDetectionLabel, creditsSourceLabels } from './credits';
import type { CreditsEditorActivity } from './credits';
import { useCreditsAnalysisRequest } from './useCreditsAnalysisRequest';
import { AudioWaveformRunProgress } from './AudioWaveformRunProgress';
import { secondsToTicks, ticksToSeconds } from './introApi';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

function draftFor(credits: ItemCredits): string {
  return credits.Effective ? ticksToSeconds(credits.Effective.StartTicks) : '';
}

function pointLabel(point: CreditsPoint | null): string {
  return point ? `${ticksToSeconds(point.StartTicks)} 秒` : '未设置';
}

function sourceLabel(point: CreditsPoint | null): string {
  return point?.Provenance === 'Chapter' ? '章节标记' : point?.Provenance === 'Manual' ? '人工设置' : point?.Provenance === 'Import' ? '导入标记' : point?.Provenance === 'Detected' ? '自动识别' : '无片尾标记';
}

const ignoreGuard: UserNavigationGuardChange = () => {};
export function CreditsEditorDialog({ itemId, itemName, currentUserId, onClose, onTasks, onNavigationGuardChange = ignoreGuard, onActivityChange }: {
  itemId: string; itemName: string; currentUserId: string; onClose: () => void; onTasks?: () => void;
  onNavigationGuardChange?: UserNavigationGuardChange; onActivityChange?: (value: CreditsEditorActivity) => void;
}) {
  const [saved, setSaved] = useState<ItemCredits>();
  const [draft, setDraft] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [review, setReview] = useState(false);
  const [revision, setRevision] = useState(0);
  const [confirmReset, setConfirmReset] = useState(false);
  const [notice, setNotice] = useState('');
  const [stopping, setStopping] = useState(false);
  const [analysisRunning, setAnalysisRunning] = useState(false);
  const analysis = useCreditsAnalysisRequest(currentUserId, itemId);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const dirty = Boolean(saved && draft !== draftFor(saved));
  const startTicks = secondsToTicks(draft);
  const startError = draft && startTicks === undefined ? '请输入非负秒数，最多保留 7 位小数。'
    : saved && startTicks !== undefined && startTicks >= saved.DurationTicks ? '片尾开始时间必须早于影片结束时间。'
      : fieldError(error, 'StartTicks');
  const invalid = startTicks === undefined || Boolean(saved && startTicks >= saved.DurationTicks);
  const locked = busy || analysis.busy || stopping;
  const disabled = loading || locked || review;
  const pendingAnalysis = Boolean(analysis.pending);
  const canAnalyze = !disabled && !dirty && !pendingAnalysis && !analysisRunning && Boolean(saved);
  const draftBusy = useRef(false);
  draftBusy.current = dirty || busy;
  const detectionFinished = useCallback(() => {
    if (!mounted.current) return;
    if (draftBusy.current) setNotice('片尾识别任务已结束，当前草稿已保留。保存或放弃草稿后，重新加载可查看最新结果。');
    else setRevision((value) => value + 1);
  }, []);
  useUserDraftNavigation(dirty || pendingAnalysis, locked, onNavigationGuardChange, '离开片尾标记编辑？未保存的标记将被丢弃，未确认的识别请求会保留供下次核对。');
  useEffect(() => { onActivityChange?.({ dirty, pending: pendingAnalysis, busy: locked }); }, [dirty, pendingAnalysis, locked, onActivityChange]);
  useEffect(() => { if (analysis.receipt) setAnalysisRunning(true); }, [analysis.receipt]);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setSaved(undefined); setNotice('');
    void creditsApi.get(itemId, { signal: controller.signal }).then((result) => {
      if (controller.signal.aborted) return;
      setSaved(result); setDraft(draftFor(result)); setReview(false);
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [itemId, revision]);

  function close() {
    if (!locked && !inFlight.current && (!dirty || window.confirm('放弃尚未保存的片尾标记？'))) onClose();
  }

  function reload() {
    if (locked || inFlight.current || loading || (dirty && !window.confirm('放弃当前草稿，重新加载最新片源和片尾标记？'))) return;
    setConfirmReset(false); setRevision((value) => value + 1);
  }

  async function mutate(reset: boolean) {
    if (inFlight.current || !saved || disabled || (!reset && (!dirty || invalid || startTicks === undefined))) return;
    inFlight.current = true; setBusy(true); setError(null); setNotice('');
    try {
      const identity = { Revision: saved.Revision, SourceRevision: saved.SourceRevision };
      const result = reset ? await creditsApi.reset(itemId, identity)
        : await creditsApi.update(itemId, { ...identity, StartTicks: startTicks!, Provenance: 'Manual' });
      if (!mounted.current) return;
      setSaved(result); setDraft(draftFor(result)); setReview(false);
      setNotice(reset ? '人工片尾标记已清除。将优先使用片源章节标记，其次使用有效的自动识别结果。' : '片尾开始时间已保存到当前片源。');
    } catch (cause) {
      if (!mounted.current || isAbortError(cause)) return;
      setError(cause);
      if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
    } finally {
      inFlight.current = false;
      if (mounted.current) { setBusy(false); setConfirmReset(false); }
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); void mutate(false); }

  return <>
    <Dialog open fullWidth maxWidth="sm" onClose={close} aria-labelledby="credits-editor-title">
      <Box component="form" noValidate onSubmit={submit} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' }}>
        <DialogTitle id="credits-editor-heading"><Typography component="span" variant="h3" id="credits-editor-title">片尾标记</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{itemName}</Typography></DialogTitle>
        <DialogContent aria-busy={loading || locked}><Stack spacing={2.5} sx={{ pt: 0.5 }}>
          {error != null && <ErrorNotice error={error} />}
          {review && <Alert severity="warning" action={<Button color="inherit" onClick={reload}>重新加载</Button>}>片尾标记或片源已发生变化，或无法确认保存结果。草稿已保留，请重新加载并核对后再保存。</Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          {analysis.error != null && <ErrorNotice error={analysis.error} />}
          {analysis.pending && <Alert severity="warning">上一次片尾识别请求尚未确认。核对会使用原请求编号，避免重复提交。<Button color="inherit" disabled={locked} onClick={() => void analysis.retry()}>核对片尾识别请求</Button></Alert>}
          {analysis.notice && <Alert severity="success">{analysis.notice}</Alert>}
          {loading && <Skeleton variant="rounded" height={240} aria-label="正在加载片尾标记" />}
          {saved && <>
            <Paper component="section" aria-label="当前片源的片尾标记" variant="outlined" sx={{ p: 2 }}><Stack spacing={1}>
              <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">当前片源</Typography><Chip size="small" variant="outlined" label={sourceLabel(saved.Effective)} /></Stack>
              <Typography variant="body2">影片时长：{ticksToSeconds(saved.DurationTicks)} 秒</Typography>
              <Typography variant="body2">生效的片尾开始时间：{pointLabel(saved.Effective)}</Typography>
              <Typography variant="body2" color="text.secondary">可用的自动依据：{pointLabel(saved.Automatic)}{saved.Automatic ? `（${sourceLabel(saved.Automatic)}）` : ''}</Typography>
              {saved.Override && <Typography variant="body2" color="text.secondary">已保存的{saved.Override.Provenance === 'Import' ? '导入' : '人工'}标记：{pointLabel(saved.Override)}</Typography>}
              {saved.LastEditedAt && <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>上次编辑：{saved.LastEditedAt}</Typography>}
            </Stack></Paper>
            {saved.OverrideStale && <Alert severity="warning">已保存的标记属于旧片源，目前不生效。请核对当前影片后重新保存，或清除旧标记。</Alert>}
            <Paper component="section" aria-label="自动片尾识别结果" variant="outlined" sx={{ p: 2 }}><Stack spacing={1.5}>
              <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">自动识别</Typography><Chip size="small" variant="outlined" color={saved.DetectedStale ? 'warning' : saved.Detected.length > 0 ? 'success' : 'default'} label={creditsDetectionLabel(saved)} /></Stack>
              {saved.DetectedStale && <Alert severity="warning">片源、匹配依据或分析设置已变化，这些检测结果目前不参与播放。可重新识别，人工标记不会被覆盖。</Alert>}
              {saved.Detected.length > 0 && <Stack component="ul" aria-label="识别到的片尾区间" spacing={1} sx={{ listStyle: 'none', p: 0, m: 0 }}>{saved.Detected.map((segment, index) => <Box component="li" key={`${segment.StartTicks}-${segment.EndTicks}-${index}`}><Typography variant="body2">{ticksToSeconds(segment.StartTicks)}–{ticksToSeconds(segment.EndTicks)} 秒 · {creditsSourceLabels[segment.Source]}</Typography></Box>)}</Stack>}
              {saved.DetectedStatus === 'no_result' && !saved.DetectedStale && <Typography variant="body2" color="text.secondary">本次没有识别到可靠的片尾区间，原有人工或章节标记仍按优先级生效。</Typography>}
              {saved.DetectedReason && <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>检测说明：{saved.DetectedReason === 'no_credits_detected' ? '没有识别到片尾' : saved.DetectedReason}</Typography>}
              {saved.DetectedUpdatedAt && <Typography variant="caption" color="text.secondary">上次识别：{new Date(saved.DetectedUpdatedAt).toLocaleString()}</Typography>}
              <Typography variant="body2" color="text.secondary">播放优先使用人工或导入标记，其次使用片源章节，最后使用有效的自动结果。自动结果参与播放需要在媒体库设置中启用片尾识别。</Typography>
              <Typography variant="caption" color="text.secondary">分析剧集最后 450 秒、电影最后 900 秒；音频匹配沿用现有 Intro Skipper 匹配设置。</Typography>
              <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}><Button variant="outlined" disabled={!canAnalyze} onClick={() => void analysis.start(saved.DetectedRevision !== '0')} startIcon={analysis.busy ? <CircularProgress size={16} color="inherit" /> : undefined}>{saved.DetectedRevision === '0' ? '识别此影片片尾' : '重新识别此影片片尾'}</Button>{onTasks && <Button disabled={locked} onClick={onTasks}>查看识别任务</Button>}</Stack>
            </Stack></Paper>
            <Typography variant="body2" color="text.secondary">填写片尾开始的秒数。播放器会据此显示下一集提示；电影和没有下一集的剧集仍可继续播放至结束。</Typography>
            <TextField fullWidth label="片尾开始时间（秒）" value={draft} disabled={disabled} onChange={(event) => { setDraft(event.target.value); setError(null); setNotice(''); }} error={Boolean(startError)} helperText={startError ?? '从 0 开始，必须早于影片结束时间，最多保留 7 位小数。'} slotProps={{ htmlInput: { inputMode: 'decimal' } }} />
            <Typography variant="caption" color="text.secondary">标记只对当前片源生效。替换影片文件后，请重新核对片尾时间。</Typography>
            <Divider />
            <Box><Button variant="outlined" color="warning" disabled={disabled || !saved.Override} onClick={() => setConfirmReset(true)}>清除人工标记</Button><Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>清除后优先使用片源中的片尾章节标记，其次使用有效的自动识别结果；两者都没有时恢复默认片尾提示。</Typography></Box>
          </>}
          {analysis.receipt && <AudioWaveformRunProgress subject="credits" runId={analysis.receipt.RunId} taskId={analysis.receipt.TaskId} onFinished={detectionFinished} onBusyChange={setStopping} onActiveChange={setAnalysisRunning} onTasks={onTasks} />}
        </Stack></DialogContent>
        <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}>
          <Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{review ? '重新加载后才可保存' : dirty ? '片尾标记尚未保存' : saved ? '所有更改已保存' : ''}</Typography>
          <Button onClick={reload} disabled={locked || loading}>重新加载</Button><Button color="secondary" onClick={close} disabled={locked}>关闭</Button>
          <Button type="submit" variant="contained" disabled={disabled || !saved || !dirty || invalid} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>{busy ? '正在保存…' : '保存片尾标记'}</Button>
        </DialogActions>
      </Box>
    </Dialog>
    {confirmReset && <Dialog open fullWidth maxWidth="xs" onClose={() => { if (!inFlight.current) setConfirmReset(false); }} aria-labelledby="reset-credits-title" aria-describedby="reset-credits-description">
      <DialogTitle id="reset-credits-title">清除人工片尾标记？</DialogTitle><DialogContent><Stack spacing={2}><Typography id="reset-credits-description">清除「{itemName}」已保存的人工标记？将优先使用片源章节标记，其次使用有效的自动识别结果。</Typography>{dirty && <Alert severity="warning">尚未保存的草稿也将被丢弃。</Alert>}</Stack></DialogContent>
      <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus color="secondary" disabled={busy} onClick={() => setConfirmReset(false)}>继续编辑</Button><Button variant="contained" color="warning" disabled={busy} onClick={() => void mutate(true)} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>清除标记</Button></DialogActions>
    </Dialog>}
  </>;
}
