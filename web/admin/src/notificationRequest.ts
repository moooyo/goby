import { ApiError } from './api';
import type { RequestOptions } from './api';

export const notificationReadTimeout = 15000;
export const notificationWriteTimeout = 20000;
export const notificationWriteTimeoutMessage = 'The notification request timed out. Its result could not be confirmed.';

// Settling the deadline does not prove that a mutation was rejected. Callers
// retain their review gate until an explicitly requested readback succeeds.
export async function notificationRequest<T>(options: RequestOptions, milliseconds: number, message: string, operation: (signal: AbortSignal) => Promise<T>): Promise<T> {
  if (options.signal?.aborted) throw new DOMException('The request was cancelled.', 'AbortError');
  const controller = new AbortController();
  const timeoutError = new ApiError(message, { code: 'network_error' });
  let timedOut = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let abort: (() => void) | undefined;
  const interrupted = new Promise<never>((_resolve, reject) => {
    abort = () => {
      controller.abort();
      reject(new DOMException('The request was cancelled.', 'AbortError'));
    };
    options.signal?.addEventListener('abort', abort, { once: true });
    timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
      reject(timeoutError);
    }, milliseconds);
  });
  // Promise.race observes both promises, including a transport that rejects
  // after its deadline or ignores cancellation, so it cannot reject unhandled.
  const request = Promise.resolve().then(() => operation(controller.signal));
  try {
    return await Promise.race([request, interrupted]);
  } catch (error) {
    if (timedOut) throw timeoutError;
    throw error;
  } finally {
    clearTimeout(timer);
    if (abort) options.signal?.removeEventListener('abort', abort);
    controller.abort();
  }
}
