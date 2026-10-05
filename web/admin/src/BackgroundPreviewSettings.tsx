import { useEffect, useRef, useState } from 'react';
import { Alert, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, MenuItem, Paper, Skeleton, Stack, TextField, Typography } from '@mui/material';
import { ApiError, isAbortError } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { backgroundPreviewApi, validBackgroundPreviewProfile } from './backgroundPreviewApi';
import type { BackgroundPreviewConfiguration, BackgroundPreviewProfile } from './backgroundPreviewApi';
import { mediaPanelSx } from './MediaPagePrimitives';

type Draft = Record<keyof BackgroundPreviewProfile, string>;
export interface BackgroundPreviewSettingsState { dirty: boolean; busy: boolean }
const draftFor = (profile: BackgroundPreviewProfile): Draft => ({ DurationSeconds: String(profile.DurationSeconds), MaxWidth: String(profile.MaxWidth), VideoBitrate: String(profile.VideoBitrate), MaxItemRuntimeSeconds: String(profile.MaxItemRuntimeSeconds) });
const fields = [
  { key: 'DurationSeconds', label: '短片时长（秒）', help: '5–60 秒，默认 25 秒。临近片尾时可能缩短。', min: 5, max: 60 },
  { key: 'VideoBitrate', label: '视频目标码率（bps）', help: '250,000–8,000,000 bps，默认 1,500,000 bps（1.5 Mbps）。', min: 250000, max: 8000000 },
  { key: 'MaxItemRuntimeSeconds', label: '单个任务最长处理时间（秒）', help: '30–7,200 秒，默认 1,200 秒。超过此时间将停止该任务。', min: 30, max: 7200 },
] as const;

function parse(draft: Draft) {
  const profile: BackgroundPreviewProfile = { DurationSeconds: Number(draft.DurationSeconds), MaxWidth: Number(draft.MaxWidth), VideoBitrate: Number(draft.VideoBitrate), MaxItemRuntimeSeconds: Number(draft.MaxItemRuntimeSeconds) };
  const errors: Partial<Record<keyof BackgroundPreviewProfile, string>> = {};
  for (const { key, min, max } of fields) if (!/^\d+$/.test(draft[key]) || !Number.isSafeInteger(profile[key]) || profile[key] < min || profile[key] > max) errors[key] = `请输入 ${min.toLocaleString()}–${max.toLocaleString()} 之间的整数。`;
  if (![640, 960, 1280, 1920].includes(profile.MaxWidth)) errors.MaxWidth = '请选择支持的画面宽度。';
  return { profile: Object.keys(errors).length === 0 && validBackgroundPreviewProfile(profile) ? profile : undefined, errors };
}

export function BackgroundPreviewSettings({ onStateChange, onLibraries, onTasks }: {
  onStateChange: (state: BackgroundPreviewSettingsState) => void; onLibraries: () => void; onTasks: () => void;
}) {
  const [saved, setSaved] = useState<BackgroundPreviewConfiguration>();
  const [draft, setDraft] = useState<Draft>();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [review, setReview] = useState(false);
  const [notice, setNotice] = useState('');
  const [revision, setRevision] = useState(0);
  const preserveDraft = useRef(false);
  const mutation = useRef<AbortController | undefined>(undefined);
  const dirty = Boolean(saved && draft && JSON.stringify(draft) !== JSON.stringify(draftFor(saved.Profile)));
  const parsed = draft ? parse(draft) : undefined;
  const disabled = busy || loading || review || !saved;

  useEffect(() => { onStateChange({ dirty, busy }); }, [dirty, busy, onStateChange]);
  useEffect(() => () => mutation.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(undefined);
    void backgroundPreviewApi.configuration({ signal: controller.signal }).then((value) => {
      if (controller.signal.aborted) return;
      setSaved(value); if (!preserveDraft.current) setDraft(draftFor(value.Profile)); setReview(false);
    }).catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);

  function reload() { if (mutation.current) return; preserveDraft.current = Boolean(draft); setRevision((value) => value + 1); }
  async function save() {
    if (!saved || !parsed?.profile || !dirty || disabled || mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; setBusy(true); setError(undefined); setNotice('');
    try {
      const value = await backgroundPreviewApi.configure(saved.Revision, parsed.profile, { signal: controller.signal });
      if (!controller.signal.aborted) { setSaved(value); setDraft(draftFor(value.Profile)); setNotice('生成规格已保存，只影响后续首次生成和明确执行的重新生成。已有短片保持不变。'); }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) {
        setError(cause);
        if (!(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || cause.code === 'invalid_response') setReview(true);
      }
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(false); }
  }

  return <>
    <Paper component="section" aria-labelledby="background-settings-title" variant="outlined" sx={{ ...mediaPanelSx, p: 2.5 }}>
      <Stack spacing={1.5}>
        <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1 }}>
          <Typography component="h2" variant="h3" id="background-settings-title">背景短片</Typography>
          <Button variant="outlined" onClick={() => setOpen(true)} disabled={busy}>生成设置{dirty ? ' •' : ''}</Button>
        </Stack>
        <Typography variant="body2" color="text.secondary">从本地片源生成无声背景短片，自动生成默认关闭。请在电影、剧集或混合媒体库设置中启用；也可在影片的“背景短片”操作中手动生成。</Typography>
        {loading && !saved ? <Skeleton width="70%" /> : saved && <Typography variant="body2">当前规格：{saved.Profile.DurationSeconds} 秒 · 最宽 {saved.Profile.MaxWidth} 像素 · {(saved.Profile.VideoBitrate / 1_000_000).toLocaleString()} Mbps · 无音轨</Typography>}
        <Typography variant="body2" color="text.secondary">成品保存在片源旁，需要媒体目录可写。已有短片将持续保留，关闭自动生成、修改规格或清理分析缓存都不会删除或替换；只有明确重新生成才更新。</Typography>
        {!open && error != null && <ErrorNotice error={error} retry={reload} />}
        {!open && notice && <Alert severity="success">{notice}</Alert>}
        <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}><Button onClick={onLibraries}>打开媒体库设置</Button><Button onClick={onTasks}>查看生成任务</Button></Stack>
      </Stack>
    </Paper>
    <Dialog open={open} onClose={() => { if (!busy) setOpen(false); }} fullWidth maxWidth="sm" aria-labelledby="background-configuration-title">
      <DialogTitle id="background-configuration-title">背景短片生成设置</DialogTitle>
      <DialogContent><Box component="form" id="background-configuration-form" noValidate onSubmit={(event) => { event.preventDefault(); void save(); }}><Stack spacing={2.5} sx={{ pt: 0.5 }}>
        <Typography variant="body2" color="text.secondary">保存只改变后续任务的生成规格，不会自动重新生成已有短片。</Typography>
        {error != null && <ErrorNotice error={error} />}
        {review && <Alert severity="warning">设置已变化或无法确认保存结果，草稿已保留。重新加载最新设置后，请核对草稿再保存。<Button color="inherit" disabled={busy || loading} onClick={reload}>重新加载并保留草稿</Button></Alert>}
        {notice && <Alert severity="success">{notice}</Alert>}
        {loading && !draft && <Skeleton variant="rounded" height={280} />}
        {draft && <>
          <TextField select fullWidth label="画面最大宽度" value={draft.MaxWidth} disabled={disabled} onChange={(event) => { setDraft({ ...draft, MaxWidth: event.target.value }); setNotice(''); }} helperText="保持画面比例，默认 1280 像素；16:9 片源约为 720p。">
            {[640, 960, 1280, 1920].map((width) => <MenuItem key={width} value={String(width)}>{width} 像素</MenuItem>)}
          </TextField>
          {fields.map(({ key, label, help }) => {
            const problem = parsed?.errors[key] ?? fieldError(error, `Profile.${key}`) ?? fieldError(error, key);
            return <TextField key={key} fullWidth label={label} value={draft[key]} disabled={disabled} error={Boolean(problem)} helperText={problem ?? help} slotProps={{ htmlInput: { inputMode: 'numeric' } }} onChange={(event) => { setDraft({ ...draft, [key]: event.target.value }); if (!review) setError(undefined); setNotice(''); }} />;
          })}
          <Typography variant="body2" color="text.secondary">截取起点优先使用人工设置，其次使用片头结束后 3 秒；没有片头标记时使用片长的 5%，限制在 30 秒至 5 分钟。短片不会越过已知片尾或影片结束时间。</Typography>
          <Stack direction="row" sx={{ gap: 1, flexWrap: 'wrap' }}><Button disabled={disabled} onClick={() => { if (saved) { setDraft(draftFor(saved.Defaults)); setNotice(''); } }}>使用默认规格</Button><Button disabled={busy || loading || !saved} onClick={() => { if (saved) { setDraft(draftFor(saved.Profile)); setNotice('已恢复上次确认的设置。'); } }}>放弃草稿</Button></Stack>
          {dirty && <Typography role="status" variant="caption" color="text.secondary">生成设置尚未保存，关闭此窗口将保留草稿。</Typography>}
        </>}
      </Stack></Box></DialogContent>
      <DialogActions sx={{ p: 3, gap: 1, flexWrap: 'wrap' }}><Button disabled={busy || loading} onClick={reload}>重新加载</Button><Button disabled={busy} onClick={() => setOpen(false)}>关闭</Button><Button type="submit" form="background-configuration-form" variant="contained" disabled={disabled || !dirty || !parsed?.profile} startIcon={busy ? <CircularProgress size={16} color="inherit" /> : undefined}>保存生成设置</Button></DialogActions>
    </Dialog>
  </>;
}
