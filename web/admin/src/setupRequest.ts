import { ApiError } from './api';

export function setupNeedsReview(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 0 || error.status >= 500
    || ['network_error', 'invalid_response', 'session_changed'].includes(error.code);
}

export async function setupRequest<T>(action: (signal: AbortSignal) => Promise<T>, lifetime: AbortSignal): Promise<T> {
  const controller = new AbortController();
  const cancel = () => controller.abort(lifetime.reason);
  let rejectAbort: (() => void) | undefined;
  const aborted = new Promise<never>((_resolve, reject) => {
    rejectAbort = () => reject(controller.signal.reason);
    controller.signal.addEventListener('abort', rejectAbort, { once: true });
  });
  lifetime.addEventListener('abort', cancel, { once: true });
  const deadline = setTimeout(() => controller.abort(new ApiError('The request timed out. Its result could not be confirmed.', { code: 'network_error' })), 15_000);
  if (lifetime.aborted) cancel();
  try { return await Promise.race([action(controller.signal), aborted]); }
  finally {
    clearTimeout(deadline);
    lifetime.removeEventListener('abort', cancel);
    if (rejectAbort) controller.signal.removeEventListener('abort', rejectAbort);
  }
}
