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
