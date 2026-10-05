import { useEffect, useRef, useState } from 'react';
import { ApiError, isAbortError } from './api';
import { subtitleTimelineApi } from './subtitleTimelineApi';
import { validSubtitleTimelineRunInput } from './subtitleTimeline';
import type { SubtitleTimelineRunInput, SubtitleTimelineRunReceipt } from './subtitleTimeline';

const receipts = new Map<string, SubtitleTimelineRunInput>();
function remember(key: string, value?: SubtitleTimelineRunInput) {
  if (value) receipts.set(key, value); else receipts.delete(key);
  try { if (value) sessionStorage.setItem(key, JSON.stringify(value)); else sessionStorage.removeItem(key); } catch { /* Keep the receipt in memory when session storage is unavailable. */ }
}
function previous(key: string, itemId?: string): SubtitleTimelineRunInput | undefined {
  try {
    const value: unknown = receipts.get(key) ?? JSON.parse(sessionStorage.getItem(key) ?? 'null');
    if (validSubtitleTimelineRunInput(value) && (itemId ? value.ItemIds.length === 1 && value.ItemIds[0] === itemId && value.LibraryIds.length === 0
      : value.ItemIds.length === 0 && value.LibraryIds.length === 1)) return value;
  } catch { /* Invalid browser state is not a server result. */ }
  return undefined;
}
const unknownResult = (cause: unknown) => !(cause instanceof ApiError) || cause.status === 409 || cause.status === 0 || cause.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(cause.code);

export function useSubtitleTimelineRequest(currentUserId: string, itemId?: string) {
  const key = `goby.subtitle-timeline.admission.${encodeURIComponent(currentUserId)}.${itemId ? `item.${encodeURIComponent(itemId)}` : 'libraries'}`;
  const [pending, setPending] = useState(() => previous(key, itemId));
  const [receipt, setReceipt] = useState<SubtitleTimelineRunReceipt>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState('');
  const mutation = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => mutation.current?.abort(), []);

  async function submit(input: SubtitleTimelineRunInput): Promise<boolean> {
    if (mutation.current) return false;
    const controller = new AbortController(); mutation.current = controller; remember(key, input); setPending(input); setBusy(true); setError(undefined); setNotice('');
    try {
      const value = await subtitleTimelineApi.start(input, { signal: controller.signal });
      if (controller.signal.aborted) return false;
      remember(key); setPending(undefined); setReceipt(value);
      setNotice(value.Admitted ? `时间轴生成请求已提交，新增 ${value.Queued} 个待处理条目。` : '已找到这次时间轴生成请求的处理记录。');
      return true;
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) {
        setError(cause);
        if (!unknownResult(cause)) { remember(key); setPending(undefined); }
      }
      return false;
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(false); }
  }
  return {
    pending, receipt, busy, error, notice,
    start: (selection: Pick<SubtitleTimelineRunInput, 'LibraryIds' | 'ItemIds' | 'Force'>) => pending ? Promise.resolve(false) : submit({ ...selection, Kind: 'subtitle-timeline', RequestId: crypto.randomUUID() }),
    retry: () => pending ? submit(pending) : Promise.resolve(false),
  };
}
