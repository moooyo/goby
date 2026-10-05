import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Paper, Skeleton, Stack, TextField, Typography } from '@mui/material';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { backgroundPreviewApi } from './backgroundPreviewApi';
import type { BackgroundPreviewDetail, BackgroundPreviewRunInput } from './backgroundPreviewApi';
import { secondsToTicks, ticksToSeconds } from './introApi';
import { analysisBytes } from './mediaAnalysis';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const active = (value?: BackgroundPreviewDetail) => value?.State === 'pending' || value?.State === 'running';
const draftFor = (value: BackgroundPreviewDetail) => value.StartTicks === null ? '' : ticksToSeconds(value.StartTicks);
const labels: Record<BackgroundPreviewDetail['State'], string> = { missing: '尚未生成', pending: '等待生成', running: '正在生成', ready: '任务完成', failed: '生成失败', cancelled: '已取消' };
const receipts = new Map<string, BackgroundPreviewRunInput>();
const receiptKey = (userId: string, itemId: string) => `goby.background-preview.admission.${encodeURIComponent(userId)}.${encodeURIComponent(itemId)}`;
function remember(key: string, value?: BackgroundPreviewRunInput) {
  if (value) receipts.set(key, value); else receipts.delete(key);
  try { if (value) sessionStorage.setItem(key, JSON.stringify(value)); else sessionStorage.removeItem(key); } catch { /* Keep the in-memory receipt when session storage is unavailable. */ }
}
function previous(key: string, itemId: string): BackgroundPreviewRunInput | undefined {
  const memory = receipts.get(key); if (memory) return memory;
  try {
    const value = JSON.parse(sessionStorage.getItem(key) ?? 'null') as BackgroundPreviewRunInput | null;
    if (value?.Kind === 'background' && typeof value.Force === 'boolean' && Array.isArray(value.LibraryIds) && value.LibraryIds.length === 0
      && Array.isArray(value.ItemIds) && value.ItemIds.length === 1 && value.ItemIds[0] === itemId
      && typeof value.RequestId === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value.RequestId)) return value;
  } catch { /* Unreadable local state is not a confirmed server result. */ }
  return undefined;
}
const unknownResult = (cause: unknown) => !(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(cause.code);

export function BackgroundPreviewEditorDialog({ itemId, itemName, currentUserId, onClose, onTasks, onNavigationGuardChange }: {
  itemId: string; itemName: string; currentUserId: string; onClose: () => void; onTasks?: () => void; onNavigationGuardChange: UserNavigationGuardChange;
}) {
  const key = receiptKey(currentUserId, itemId);
  const [saved, setSaved] = useState<BackgroundPreviewDetail>();
  const [status, setStatus] = useState<BackgroundPreviewDetail>();
  const [draft, setDraft] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<'save' | 'generate'>();
  const [error, setError] = useState<unknown>();
  const [pollError, setPollError] = useState<unknown>();
  const [review, setReview] = useState(false);
  const [notice, setNotice] = useState('');
  const [revision, setRevision] = useState(0);
  const [confirmRegenerate, setConfirmRegenerate] = useState(false);
  const [pending, setPending] = useState(() => previous(key, itemId));
  const [pageHidden, setPageHidden] = useState(document.hidden);
  const mutation = useRef<AbortController | undefined>(undefined);
  const current = status ?? saved;
  const dirty = Boolean(saved && draft !== draftFor(saved));
  const startTicks = draft === '' ? null : secondsToTicks(draft);
  const startError = startTicks === undefined ? '请输入非负秒数，最多保留 7 位小数；留空使用自动规则。'
    : saved && startTicks !== null && startTicks >= saved.DurationTicks ? '起点必须早于影片结束时间。' : fieldError(error, 'StartTicks');
  const invalid = startTicks === undefined || Boolean(saved && startTicks !== null && startTicks !== undefined && startTicks >= saved.DurationTicks);
  const disabled = Boolean(busy) || loading || review || Boolean(pending) || active(current);
  const canGenerate = !disabled && !dirty && Boolean(saved && saved.DurationTicks > 0) && !invalid;
  useUserDraftNavigation(dirty || Boolean(pending), Boolean(busy), onNavigationGuardChange, '离开背景短片编辑？未保存的起点将被丢弃，尚未确认的生成请求会保留供下次核对。');
  useEffect(() => () => mutation.current?.abort(), []);
  useEffect(() => {
    const changed = () => setPageHidden(document.hidden);
    document.addEventListener('visibilitychange', changed);
    return () => document.removeEventListener('visibilitychange', changed);
  }, []);

  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(undefined); setPollError(undefined);
    void backgroundPreviewApi.get(itemId, { signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return;
      setSaved(value); setStatus(value); setDraft(draftFor(value)); setReview(false);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [itemId, revision]);

  useEffect(() => {
    if (!active(current) || busy || loading || pollError || pageHidden) return;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void backgroundPreviewApi.get(itemId, { signal: controller.signal }).then((value) => {
        if (controller.signal.aborted) return;
        setStatus(value);
        if (!dirty) { setSaved(value); setDraft(draftFor(value)); }
        else if (saved && (saved.Revision !== value.Revision || saved.SourceRevision !== value.SourceRevision)) setReview(true);
      }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setPollError(cause); });
    }, 5000);
    return () => { window.clearTimeout(timer); controller.abort(); };
  }, [itemId, current, busy, loading, pollError, dirty, saved, pageHidden]);

  function close() { if (!mutation.current && (!dirty || window.confirm('放弃尚未保存的背景短片起点？'))) onClose(); }
  function reload() {
    if (mutation.current || loading || dirty && !window.confirm('放弃当前草稿并加载最新起点和生成状态？')) return;
    setRevision((value) => value + 1);
  }
  async function save() {
    if (!saved || disabled || !dirty || invalid || startTicks === undefined || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy('save'); setError(undefined); setNotice('');
    try {
      const value = await backgroundPreviewApi.update(itemId, { Revision: saved.Revision, SourceRevision: saved.SourceRevision, StartTicks: startTicks }, { signal: controller.signal });
      if (!controller.signal.aborted) { setSaved(value); setStatus(value); setDraft(draftFor(value)); setNotice('截取起点已保存。已有短片保持不变，只有明确重新生成时才应用新起点。'); }
    } catch (cause) { if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); if (unknownResult(cause)) setReview(true); } }
    finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  async function generate(input: BackgroundPreviewRunInput) {
    if (mutation.current || loading || dirty || !saved) return;
    const controller = new AbortController(); mutation.current = controller; remember(key, input); setPending(input); setBusy('generate'); setError(undefined); setNotice('');
    try {
      const value = await backgroundPreviewApi.start(input, { signal: controller.signal });
      if (!controller.signal.aborted) {
        remember(key); setPending(undefined); setConfirmRegenerate(false);
        setNotice(value.Admitted ? `生成请求已提交，新增 ${value.Queued} 个待处理条目。可在任务中心查看进度或取消。` : '已找到这次请求的处理记录，可在任务中心查看进度。');
        setRevision((count) => count + 1);
      }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) {
        setError(cause); setConfirmRegenerate(false);
        if (!unknownResult(cause)) { remember(key); setPending(undefined); }
      }
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(undefined); }
  }
  function start(force: boolean) { if (canGenerate) void generate({ Kind: 'background', RequestId: crypto.randomUUID(), LibraryIds: [], ItemIds: [itemId], Force: force }); }

  return <>
    <Dialog open fullWidth maxWidth="sm" onClose={close} aria-labelledby="background-editor-title">
      <Box component="form" noValidate onSubmit={(event) => { event.preventDefault(); void save(); }} sx={{ display: 'flex', flexDirection: 'column', minHeight: 0, overflow: 'hidden' }}>
        <DialogTitle id="background-editor-heading"><Typography component="span" variant="h3" id="background-editor-title">背景短片</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>{itemName}</Typography></DialogTitle>
        <DialogContent aria-busy={loading || Boolean(busy)}><Stack spacing={2.5} sx={{ pt: 0.5 }}>
          {error != null && <ErrorNotice error={error} />}
          {review && <Alert severity="warning">起点或片源已变化，或无法确认保存结果。草稿已保留，请重新加载并核对后再操作。</Alert>}
          {pending && <Alert severity="warning">上一次{pending.Force ? '重新生成' : '生成'}请求尚未确认。核对时会使用同一个请求编号，避免重复提交。<Button color="inherit" disabled={Boolean(busy) || loading || dirty || !saved} onClick={() => void generate(pending)}>核对生成请求</Button></Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          {loading && !saved && <Skeleton variant="rounded" height={260} aria-label="正在加载背景短片" />}
          {current && saved && <>
            <Paper component="section" aria-label="已保存的背景短片" variant="outlined" sx={{ p: 2 }}><Stack spacing={1}>
              <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography component="h3" variant="h4">已保存的成品</Typography><Chip size="small" variant="outlined" color={current.Artifact.Available ? 'success' : 'default'} label={current.Artifact.Available ? '可以播放' : '尚无成品'} /></Stack>
              {current.Artifact.Available ? <>
                <Typography variant="body2">截取区间：{ticksToSeconds(current.Artifact.StartPositionTicks)}–{ticksToSeconds(current.Artifact.StartPositionTicks + current.Artifact.RunTimeTicks)} 秒</Typography>
                <Typography variant="body2" color="text.secondary">{current.Artifact.Width} × {current.Artifact.Height} · {analysisBytes(current.Artifact.Size)} · 无音轨</Typography>
                {current.Artifact.SourceChanged && <Alert severity="info">片源已经变化，已有短片仍保留。需要更新内容时，请明确重新生成。</Alert>}
              </> : <Typography variant="body2" color="text.secondary">尚无可播放的背景短片，可手动生成，或在媒体库设置中开启自动生成。</Typography>}
            </Stack></Paper>
            <Box component="section" aria-label="背景短片任务状态"><Stack spacing={1}>
              <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexWrap: 'wrap' }}><Typography variant="body2">最近任务：</Typography><Chip size="small" label={labels[current.State]} color={current.State === 'failed' ? 'error' : active(current) ? 'primary' : 'default'} /></Stack>
              {active(current) && <Typography variant="body2" color="text.secondary">正在后台排队或生成，可以关闭此窗口；任务中心提供进度与取消操作。</Typography>}
              {current.State === 'ready' && current.Reused && <Typography variant="body2" color="text.secondary">已有成品已复用，没有重复生成。</Typography>}
              {current.ErrorCode && <Typography variant="body2" color="error.main" sx={{ overflowWrap: 'anywhere' }}>任务错误：{current.ErrorCode}</Typography>}
              {pollError != null && <ErrorNotice error={pollError} retry={reload} />}
              {onTasks && <Box><Button onClick={onTasks} disabled={Boolean(busy)}>查看生成任务</Button></Box>}
            </Stack></Box>
            <Divider />
            {saved.DurationTicks > 0 ? <Typography variant="body2" color="text.secondary">影片时长：{ticksToSeconds(saved.DurationTicks)} 秒。生成时使用已保存的时长与画质设置，默认 25 秒、最宽 1280 像素、无音轨。</Typography> : <Alert severity="warning">当前片源尚无有效时长，暂时无法生成。请检查片源并重新扫描媒体库；已有成品仍会保留。</Alert>}
            <TextField fullWidth label="手动截取起点（秒）" value={draft} disabled={disabled} onChange={(event) => { setDraft(event.target.value); setError(undefined); setNotice(''); }} error={Boolean(startError)} helperText={startError ?? '留空使用自动规则；从 0 开始，最多保留 7 位小数。'} slotProps={{ htmlInput: { inputMode: 'decimal' } }} />
            <Typography variant="body2" color="text.secondary">自动规则：优先从片头结束后 3 秒开始；没有片头标记时取片长的 5%，限制在 30 秒至 5 分钟。片段不会越过已知片尾或影片结束。</Typography>
            <Typography variant="body2" color="text.secondary">保存起点不会启动生成。成品保存在片源旁并长期保留，修改起点或关闭自动生成都不会删除；媒体目录必须可写。</Typography>
            <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}>
              <Button variant="outlined" disabled={!canGenerate || current.Artifact.Available} onClick={() => start(false)}>生成缺失短片</Button>
              <Button variant="outlined" color="warning" disabled={!canGenerate || !current.Artifact.Available} onClick={() => setConfirmRegenerate(true)}>重新生成</Button>
            </Stack>
          </>}
        </Stack></DialogContent>
        <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Typography variant="caption" color="text.secondary" sx={{ mr: 'auto' }}>{dirty ? '截取起点尚未保存' : saved ? '起点已保存' : ''}</Typography><Button onClick={reload} disabled={Boolean(busy) || loading}>刷新</Button><Button onClick={close} disabled={Boolean(busy)}>关闭</Button><Button type="submit" variant="contained" disabled={disabled || !dirty || invalid || !saved} startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />}>保存起点</Button></DialogActions>
      </Box>
    </Dialog>
    {confirmRegenerate && <Dialog open fullWidth maxWidth="xs" onClose={() => { if (!mutation.current) setConfirmRegenerate(false); }} aria-labelledby="background-regenerate-title" aria-describedby="background-regenerate-description">
      <DialogTitle id="background-regenerate-title">重新生成背景短片？</DialogTitle><DialogContent><Typography id="background-regenerate-description">将按当前保存的起点和生成规格，为「{itemName}」重新生成短片。新文件生成成功后才替换已有成品；失败或取消会保留原成品。</Typography></DialogContent><DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button autoFocus disabled={Boolean(busy)} onClick={() => setConfirmRegenerate(false)}>保留现有短片</Button><Button variant="contained" color="warning" disabled={!canGenerate} onClick={() => start(true)} startIcon={busy === 'generate' ? <CircularProgress size={16} color="inherit" /> : undefined}>确认重新生成</Button></DialogActions>
    </Dialog>}
  </>;
}
