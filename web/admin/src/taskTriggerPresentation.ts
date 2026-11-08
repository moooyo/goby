import type { TaskDefinition, TaskSystemEvent, TaskTriggerInput } from './api';
import type { TranslationValues } from './i18n';
import { durationFromTicks, timeFromTicks, weekdays } from './taskSchedule';

export type TaskTranslator = (key: string, values?: TranslationValues) => string;

const eventLabels: Record<TaskSystemEvent, string> = {
  ServerStarted: 'When the server starts',
  LibraryChanged: 'When a library changes',
  ConfigurationChanged: 'When the server configuration changes',
  IntroAnalysisRequested: 'When intro detection is requested',
  PreviewGenerationRequested: 'When seek preview generation is requested',
  BackgroundPreviewGenerationRequested: 'When background clip generation is requested',
  AudioWaveformGenerationRequested: 'When audio waveform generation is requested',
  CreditsAnalysisRequested: 'When credits detection is requested',
  SubtitleTimelineGenerationRequested: 'When subtitle timeline generation is requested',
  OnlineImagesRequested: 'When online image download is requested',
};

export function taskTriggerLabel(trigger: TaskTriggerInput, t: TaskTranslator): string {
  switch (trigger.Kind) {
    case 'daily': return t('Daily at {time}', { time: timeFromTicks(trigger.TimeOfDayTicks ?? '0') });
    case 'weekly': return t('Every ' + weekdays[trigger.DayOfWeek ?? 0] + ' at {time}', { time: timeFromTicks(trigger.TimeOfDayTicks ?? '0') });
    case 'interval': {
      const duration = durationFromTicks(trigger.IntervalTicks ?? '0');
      return t('Every {value} {unit}', { value: duration.value, unit: t(Number(duration.value) === 1 ? duration.unit.replace(/s$/, '') : duration.unit) });
    }
    case 'startup': return t('At server startup');
    case 'system_event': return t(trigger.SystemEvent ? eventLabels[trigger.SystemEvent] : 'When a matching server event occurs');
  }
}

export function taskScheduleSummary(task: Pick<TaskDefinition, 'Triggers'>, t: TaskTranslator): string {
  if (!task.Triggers.length) return t('No triggers · manual only');
  const first = taskTriggerLabel(task.Triggers[0], t);
  return task.Triggers.length === 1 ? first : t('{first} + {count} more rules', { first, count: task.Triggers.length - 1 });
}
