const memoryReceipts = new Map<string, string>();
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

function receiptKey(userId: string, taskId: string) { return `goby.task-start.${encodeURIComponent(userId)}.${encodeURIComponent(taskId)}`; }

export function readTaskAdmission(userId: string, taskId: string): string | undefined {
  const key = receiptKey(userId, taskId);
  const memory = memoryReceipts.get(key);
  if (memory) return memory;
  try { const value = sessionStorage.getItem(key); return value && uuidPattern.test(value) ? value : undefined; } catch { return undefined; }
}

export function keepTaskAdmission(userId: string, taskId: string, requestId: string) {
  const key = receiptKey(userId, taskId);
  memoryReceipts.set(key, requestId);
  try { sessionStorage.setItem(key, requestId); } catch { /* The in-memory receipt still survives in-app navigation. */ }
}

export function forgetTaskAdmission(userId: string, taskId: string) {
  const key = receiptKey(userId, taskId);
  memoryReceipts.delete(key);
  try { sessionStorage.removeItem(key); } catch { /* A blocked store must not hide a confirmed run. */ }
}

export function taskAdmissionId(): string {
  // getRandomValues is also available on HTTP deployment origins.
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 15) | 64;
  bytes[8] = (bytes[8] & 63) | 128;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
