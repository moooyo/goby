import { useEffect, useRef, useState } from 'react';
import { ApiError, isAbortError } from './api';
import { mediaAnalysisApi } from './mediaAnalysisApi';
import type { AnalysisRunInput, AnalysisRunReceipt } from './mediaAnalysis';

const requests = new Map<string, AnalysisRunInput>();
function remember(key: string, value?: AnalysisRunInput) {
  if (value) requests.set(key, value); else requests.delete(key);
  try { if (value) sessionStorage.setItem(key, JSON.stringify(value)); else sessionStorage.removeItem(key); } catch { /* Keep the exact request in memory if browser storage is unavailable. */ }
}
function previous(key: string, itemId: string): AnalysisRunInput | undefined {
  try {
    const value = requests.get(key) ?? JSON.parse(sessionStorage.getItem(key) ?? 'null') as AnalysisRunInput | null;
    if (value?.Kind === 'credits' && typeof value.RequestId === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value.RequestId)
      && typeof value.Force === 'boolean' && Array.isArray(value.LibraryIds) && value.LibraryIds.length === 0
      && Array.isArray(value.ItemIds) && value.ItemIds.length === 1 && value.ItemIds[0] === itemId) return value;
  } catch { /* Incomplete local state is not a confirmed task receipt. */ }
  return undefined;
}
const unknownResult = (cause: unknown) => !(cause instanceof ApiError) || cause.status === 0 || cause.status === 409 || cause.status >= 500 || ['network_error', 'invalid_response', 'session_changed'].includes(cause.code);

export function useCreditsAnalysisRequest(currentUserId: string, itemId: string) {
  const key = `goby.credits-analysis.admission.${encodeURIComponent(currentUserId)}.${encodeURIComponent(itemId)}`;
  const [pending, setPending] = useState(() => previous(key, itemId));
  const [receipt, setReceipt] = useState<AnalysisRunReceipt>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState('');
  const mutation = useRef<AbortController | undefined>(undefined);
  useEffect(() => () => mutation.current?.abort(), []);
  async function submit(input: AnalysisRunInput): Promise<void> {
    if (mutation.current) return;
    const controller = new AbortController(); mutation.current = controller; remember(key, input); setPending(input); setBusy(true); setError(undefined); setNotice('');
    try {
      const value = await mediaAnalysisApi.start(input, { signal: controller.signal });
      if (!controller.signal.aborted) { remember(key); setPending(undefined); setReceipt(value); setNotice(value.Admitted ? '片尾识别任务已提交，人工标记不会被覆盖。' : '已找到本次片尾识别请求的任务记录。'); }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) { setError(cause); if (!unknownResult(cause)) { remember(key); setPending(undefined); } }
    } finally { if (mutation.current === controller) mutation.current = undefined; if (!controller.signal.aborted) setBusy(false); }
  }
  return { pending, receipt, busy, error, notice,
    start: (force: boolean) => pending ? Promise.resolve() : submit({ Kind: 'credits', RequestId: crypto.randomUUID(), LibraryIds: [], ItemIds: [itemId], Force: force }),
    retry: () => pending ? submit(pending) : Promise.resolve(),
  };
}
