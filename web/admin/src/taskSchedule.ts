import type { TaskScheduleInput, TaskTrigger, TaskTriggerInput, TaskTriggerKind } from './api';

export const ticksPerSecond = 10000000n;
const maxDurationTicks = 92233720368547758n;
const ticksPerDay = 86400n * ticksPerSecond;
export const durationUnits = { seconds: ticksPerSecond, minutes: 60n * ticksPerSecond, hours: 3600n * ticksPerSecond, days: ticksPerDay };
export type DurationUnit = keyof typeof durationUnits;
export interface DurationDraft { value: string; unit: DurationUnit }
export interface TriggerDraft { key: string; kind: TaskTriggerKind; interval: DurationDraft; time: string; day: number; runtime: DurationDraft; limitRuntime: boolean }
export interface ScheduleDraft { timezone: string; triggers: TriggerDraft[] }
export const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

function decimalSeconds(ticks: bigint): string {
  const fraction = (ticks % ticksPerSecond).toString().padStart(7, '0').replace(/0+$/, '');
  return `${ticks / ticksPerSecond}${fraction ? `.${fraction}` : ''}`;
}

export function durationFromTicks(value: string): DurationDraft {
  const ticks = BigInt(value);
  for (const unit of ['days', 'hours', 'minutes'] as const) {
    if (ticks > 0n && ticks % durationUnits[unit] === 0n) return { value: (ticks / durationUnits[unit]).toString(), unit };
  }
  return { value: decimalSeconds(ticks), unit: 'seconds' };
}

export function durationToTicks(draft: DurationDraft): string {
  if (!/^\d{1,20}(?:\.\d{1,7})?$/.test(draft.value)) throw new Error('Enter a positive duration with up to seven decimal places.');
  const [whole, fraction = ''] = draft.value.split('.');
  const denominator = 10n ** BigInt(fraction.length);
  const numerator = (BigInt(whole) * denominator + BigInt(fraction || '0')) * durationUnits[draft.unit];
  if (numerator % denominator !== 0n) throw new Error('This duration is more precise than the server supports. Use seconds with up to seven decimal places.');
  const ticks = numerator / denominator;
  if (ticks < ticksPerSecond) throw new Error('Use a duration of at least one second.');
  if (ticks > maxDurationTicks) throw new Error('This duration is too long. Use a shorter duration.');
  return ticks.toString();
}

export function timeFromTicks(value: string): string {
  const ticks = BigInt(value);
  const hours = ticks / (3600n * ticksPerSecond);
  const minutes = ticks / (60n * ticksPerSecond) % 60n;
  const seconds = ticks % (60n * ticksPerSecond);
  const base = `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}`;
  const wholeSeconds = (seconds / ticksPerSecond).toString().padStart(2, '0');
  const fraction = (seconds % ticksPerSecond).toString().padStart(7, '0').replace(/0+$/, '');
  return seconds === 0n ? base : `${base}:${wholeSeconds}${fraction ? `.${fraction}` : ''}`;
}

export function timeToTicks(value: string): string {
  const match = /^(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,7}))?)?$/.exec(value);
  if (!match || Number(match[1]) > 23 || Number(match[2]) > 59 || Number(match[3] ?? 0) > 59) {
    throw new Error('Use a time from 00:00 to 23:59:59.9999999.');
  }
  return ((BigInt(match[1]) * 3600n + BigInt(match[2]) * 60n + BigInt(match[3] ?? '0')) * ticksPerSecond
    + BigInt((match[4] ?? '').padEnd(7, '0'))).toString();
}

export function newTrigger(): TriggerDraft {
  return { key: globalThis.crypto.randomUUID?.() ?? `${Date.now()}-${Math.random()}`, kind: 'daily', interval: { value: '1', unit: 'days' }, time: '03:00', day: 0, runtime: { value: '1', unit: 'hours' }, limitRuntime: false };
}

export function scheduleFromTask(timezone: string, triggers: TaskTrigger[]): ScheduleDraft {
  return { timezone, triggers: triggers.map((trigger) => ({
    key: trigger.Id, kind: trigger.Kind, interval: durationFromTicks(trigger.IntervalTicks ?? ticksPerDay.toString()),
    time: timeFromTicks(trigger.TimeOfDayTicks ?? '108000000000'), day: trigger.DayOfWeek ?? 0,
    runtime: durationFromTicks(trigger.MaxRuntimeTicks && trigger.MaxRuntimeTicks !== '0' ? trigger.MaxRuntimeTicks : '36000000000'),
    limitRuntime: trigger.MaxRuntimeTicks !== null && trigger.MaxRuntimeTicks !== '0',
  })) };
}

export function scheduleInput(draft: ScheduleDraft): TaskScheduleInput {
  const timezone = draft.timezone.trim();
  if (!timezone || timezone === 'Local' || timezone.startsWith('/') || timezone.endsWith('/') || timezone.includes('//')
    || timezone.startsWith('posix/') || timezone.startsWith('right/') || !/^[A-Za-z0-9_+/-]+$/.test(timezone)
    || new TextEncoder().encode(timezone).length > 255) throw new Error('Enter UTC or an explicit IANA time zone, such as Europe/London.');
  if (draft.triggers.length > 32) throw new Error('Use at most 32 triggers.');
  const triggers = draft.triggers.map((trigger, index): TaskTriggerInput => {
    try {
      const result: TaskTriggerInput = { Kind: trigger.kind, MaxRuntimeTicks: trigger.limitRuntime ? durationToTicks(trigger.runtime) : null };
      if (trigger.kind === 'interval') result.IntervalTicks = durationToTicks(trigger.interval);
      if (trigger.kind === 'daily' || trigger.kind === 'weekly') result.TimeOfDayTicks = timeToTicks(trigger.time);
      if (trigger.kind === 'weekly') result.DayOfWeek = trigger.day;
      return result;
    } catch (error) {
      throw new Error(`Trigger ${index + 1}: ${error instanceof Error ? error.message : 'Review this trigger.'}`);
    }
  });
  return { ScheduleTimezone: timezone, Triggers: triggers };
}

export function describeTrigger(trigger: TaskTrigger): string {
  switch (trigger.Kind) {
    case 'interval': {
      const duration = durationFromTicks(trigger.IntervalTicks ?? '0');
      return `Every ${duration.value} ${duration.unit}`;
    }
    case 'daily': return `Daily at ${timeFromTicks(trigger.TimeOfDayTicks ?? '0')}`;
    case 'weekly': return `${weekdays[trigger.DayOfWeek ?? 0]} at ${timeFromTicks(trigger.TimeOfDayTicks ?? '0')}`;
    case 'startup': return 'At server startup';
  }
}

export function scheduleDate(value: string, timezone: string): string {
  try {
    const fraction = /\.(\d+)Z$/.exec(value)?.[1].replace(/0+$/, '');
    const formatter = new Intl.DateTimeFormat(undefined, { timeZone: timezone, year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
    const formatted = formatter.formatToParts(new Date(value)).map((part) => part.type === 'second' && fraction ? `${part.value}.${fraction}` : part.value).join('');
    return `${formatted} (${timezone})`;
  } catch {
    return `${value} (UTC)`;
  }
}
