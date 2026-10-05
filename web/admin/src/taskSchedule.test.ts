import assert from 'node:assert/strict';
import test from 'node:test';
import type { TaskTrigger } from './api.ts';
import { describeTrigger, scheduleFromTask, scheduleInput, systemEvents } from './taskSchedule.ts';

test('automatic intro requests remain readable and survive schedule editing', () => {
  const trigger: TaskTrigger = { Id: 'intro-request', Kind: 'system_event', SystemEvent: 'IntroAnalysisRequested', IntervalTicks: null, TimeOfDayTicks: null,
    DayOfWeek: null, MaxRuntimeTicks: null, NextFireAt: null, CalculationError: '' };
  assert.equal(systemEvents.find((event) => event.value === trigger.SystemEvent)?.label, 'Intro detection requested');
  assert.equal(describeTrigger(trigger), 'When intro detection requested');
  const draft = scheduleFromTask('UTC', [trigger]);
  assert.deepEqual(scheduleInput(draft), { ScheduleTimezone: 'UTC', Triggers: [{ Kind: 'system_event', SystemEvent: 'IntroAnalysisRequested', MaxRuntimeTicks: null }] });
  assert.equal(trigger.SystemEvent, 'IntroAnalysisRequested');
});

test('automatic preview requests retain their event identity through schedule editing', () => {
  const trigger: TaskTrigger = { Id: 'preview-request', Kind: 'system_event', SystemEvent: 'PreviewGenerationRequested', IntervalTicks: null, TimeOfDayTicks: null,
    DayOfWeek: null, MaxRuntimeTicks: null, NextFireAt: null, CalculationError: '' };
  assert.equal(systemEvents.find((event) => event.value === trigger.SystemEvent)?.label, 'Seek preview generation requested');
  assert.equal(describeTrigger(trigger), 'When seek preview generation requested');
  assert.deepEqual(scheduleInput(scheduleFromTask('UTC', [trigger])), {
    ScheduleTimezone: 'UTC', Triggers: [{ Kind: 'system_event', SystemEvent: 'PreviewGenerationRequested', MaxRuntimeTicks: null }],
  });
  assert.equal(trigger.SystemEvent, 'PreviewGenerationRequested');
});

test('background preview requests preserve event identity and runtime limits when edited with existing events', () => {
  const trigger: TaskTrigger = { Id: 'background-preview-request', Kind: 'system_event', SystemEvent: 'BackgroundPreviewGenerationRequested', IntervalTicks: null, TimeOfDayTicks: null,
    DayOfWeek: null, MaxRuntimeTicks: '12000000000', NextFireAt: null, CalculationError: '' };
  const previous: TaskTrigger = { ...trigger, Id: 'library-request', SystemEvent: 'LibraryChanged', MaxRuntimeTicks: null };
  assert.equal(systemEvents.find((event) => event.value === trigger.SystemEvent)?.label, '请求生成背景短片');
  assert.equal(describeTrigger(trigger), '请求生成背景短片时');
  assert.deepEqual(scheduleInput(scheduleFromTask('UTC', [previous, trigger])), {
    ScheduleTimezone: 'UTC', Triggers: [
      { Kind: 'system_event', SystemEvent: 'LibraryChanged', MaxRuntimeTicks: null },
      { Kind: 'system_event', SystemEvent: 'BackgroundPreviewGenerationRequested', MaxRuntimeTicks: '12000000000' },
    ],
  });
  assert.equal(trigger.SystemEvent, 'BackgroundPreviewGenerationRequested');
  assert.equal(previous.SystemEvent, 'LibraryChanged');
});

test('audio waveform requests preserve all existing events and runtime limits through schedule editing', () => {
  const trigger: TaskTrigger = { Id: 'audio-waveform-request', Kind: 'system_event', SystemEvent: 'AudioWaveformGenerationRequested', IntervalTicks: null, TimeOfDayTicks: null,
    DayOfWeek: null, MaxRuntimeTicks: '12000000000', NextFireAt: null, CalculationError: '' };
  const previous = (['ServerStarted', 'LibraryChanged', 'ConfigurationChanged', 'IntroAnalysisRequested', 'PreviewGenerationRequested', 'BackgroundPreviewGenerationRequested'] as const)
    .map((event, index): TaskTrigger => ({ ...trigger, Id: `existing-${index}`, SystemEvent: event, MaxRuntimeTicks: index % 2 === 0 ? null : '36000000000' }));
  assert.equal(systemEvents.find((event) => event.value === trigger.SystemEvent)?.label, '请求生成音轨波形');
  assert.equal(describeTrigger(trigger), '请求生成音轨波形时');
  assert.deepEqual(scheduleInput(scheduleFromTask('UTC', [...previous, trigger])), {
    ScheduleTimezone: 'UTC', Triggers: [
      ...previous.map((event) => ({ Kind: 'system_event', SystemEvent: event.SystemEvent, MaxRuntimeTicks: event.MaxRuntimeTicks })),
      { Kind: 'system_event', SystemEvent: 'AudioWaveformGenerationRequested', MaxRuntimeTicks: '12000000000' },
    ],
  });
  assert.equal(trigger.SystemEvent, 'AudioWaveformGenerationRequested');
  assert.deepEqual(previous.map((event) => event.SystemEvent), ['ServerStarted', 'LibraryChanged', 'ConfigurationChanged', 'IntroAnalysisRequested', 'PreviewGenerationRequested', 'BackgroundPreviewGenerationRequested']);
});

test('credits detection requests preserve existing events and runtime limits through schedule editing', () => {
  const trigger: TaskTrigger = { Id: 'credits-request', Kind: 'system_event', SystemEvent: 'CreditsAnalysisRequested', IntervalTicks: null, TimeOfDayTicks: null,
    DayOfWeek: null, MaxRuntimeTicks: '12000000000', NextFireAt: null, CalculationError: '' };
  const existingEvents = ['ServerStarted', 'LibraryChanged', 'ConfigurationChanged', 'IntroAnalysisRequested', 'PreviewGenerationRequested', 'BackgroundPreviewGenerationRequested', 'AudioWaveformGenerationRequested'] as const;
  const previous = existingEvents.map((event, index): TaskTrigger => ({ ...trigger, Id: `existing-${index}`, SystemEvent: event, MaxRuntimeTicks: index % 2 === 0 ? null : '36000000000' }));
  assert.equal(systemEvents.find((event) => event.value === trigger.SystemEvent)?.label, '请求识别片尾');
  assert.equal(describeTrigger(trigger), '请求识别片尾时');
  assert.deepEqual(scheduleInput(scheduleFromTask('UTC', [...previous, trigger])), {
    ScheduleTimezone: 'UTC', Triggers: [
      ...previous.map((event) => ({ Kind: 'system_event', SystemEvent: event.SystemEvent, MaxRuntimeTicks: event.MaxRuntimeTicks })),
      { Kind: 'system_event', SystemEvent: 'CreditsAnalysisRequested', MaxRuntimeTicks: '12000000000' },
    ],
  });
  assert.equal(trigger.SystemEvent, 'CreditsAnalysisRequested');
  assert.deepEqual(previous.map((event) => event.SystemEvent), existingEvents);
});

test('subtitle timeline requests retain every existing task event during schedule editing', () => {
  const trigger: TaskTrigger = { Id: 'subtitle-timeline-request', Kind: 'system_event', SystemEvent: 'SubtitleTimelineGenerationRequested', IntervalTicks: null, TimeOfDayTicks: null,
    DayOfWeek: null, MaxRuntimeTicks: '12000000000', NextFireAt: null, CalculationError: '' };
  const existingEvents = ['ServerStarted', 'LibraryChanged', 'ConfigurationChanged', 'IntroAnalysisRequested', 'PreviewGenerationRequested', 'BackgroundPreviewGenerationRequested', 'AudioWaveformGenerationRequested', 'CreditsAnalysisRequested'] as const;
  const previous = existingEvents.map((event, index): TaskTrigger => ({ ...trigger, Id: `existing-${index}`, SystemEvent: event, MaxRuntimeTicks: index % 2 === 0 ? null : '36000000000' }));
  assert.equal(systemEvents.find((event) => event.value === trigger.SystemEvent)?.label, '请求生成字幕时间轴');
  assert.equal(describeTrigger(trigger), '请求生成字幕时间轴时');
  assert.deepEqual(scheduleInput(scheduleFromTask('UTC', [...previous, trigger])), {
    ScheduleTimezone: 'UTC', Triggers: [
      ...previous.map((event) => ({ Kind: 'system_event', SystemEvent: event.SystemEvent, MaxRuntimeTicks: event.MaxRuntimeTicks })),
      { Kind: 'system_event', SystemEvent: 'SubtitleTimelineGenerationRequested', MaxRuntimeTicks: '12000000000' },
    ],
  });
  assert.equal(trigger.SystemEvent, 'SubtitleTimelineGenerationRequested');
  assert.deepEqual(previous.map((event) => event.SystemEvent), existingEvents);
});
