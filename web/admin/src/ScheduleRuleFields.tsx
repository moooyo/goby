import { Box, FormControlLabel, MenuItem, Stack, Switch, TextField, Typography } from './ui';
import type { TaskTriggerKind } from './api';
import { useTranslation } from './i18n';
import { durationUnits, systemEvents, weekdays } from './taskSchedule';
import type { DurationDraft, DurationUnit, TriggerDraft } from './taskSchedule';
import { taskTriggerLabel } from './taskTriggerPresentation';

export const scheduleRuleKinds: { value: TaskTriggerKind; label: string; shortLabel: string }[] = [
  { value: 'daily', label: 'Every day', shortLabel: 'Daily' },
  { value: 'weekly', label: 'Every week', shortLabel: 'Weekly' },
  { value: 'interval', label: 'At an interval', shortLabel: 'Interval' },
  { value: 'startup', label: 'At server startup', shortLabel: 'Startup' },
  { value: 'system_event', label: 'On a system event', shortLabel: 'System event' },
];

export function ScheduleRuleKindPicker({ value, disabled, onChange }: { value: TaskTriggerKind; disabled: boolean; onChange: (kind: TaskTriggerKind) => void }) {
  const { t } = useTranslation();
  return <div className="system-schedule-kind-picker" role="group" aria-label={t('Rule type')}>
    {scheduleRuleKinds.map((kind) => <button key={kind.value} type="button" aria-pressed={kind.value === value} disabled={disabled} onClick={() => { if (kind.value !== value) onChange(kind.value); }}>{t(kind.shortLabel)}</button>)}
  </div>;
}

function DurationField({ label, draft, disabled, onChange }: { label: string; draft: DurationDraft; disabled: boolean; onChange: (value: DurationDraft) => void }) {
  return <div className="system-schedule-duration">
    <TextField size="small" label={label} value={draft.value} disabled={disabled} onChange={(event) => onChange({ ...draft, value: event.target.value })} slotProps={{ htmlInput: { inputMode: 'decimal', autoComplete: 'off' } }} />
    <TextField size="small" select label={label + ' unit'} value={draft.unit} disabled={disabled} onChange={(event) => onChange({ ...draft, unit: event.target.value as DurationUnit })}>
      {Object.keys(durationUnits).map((unit) => <MenuItem key={unit} value={unit}>{unit[0].toUpperCase() + unit.slice(1)}</MenuItem>)}
    </TextField>
  </div>;
}

export function ScheduleRuleFields({ value, timezone, disabled, showKind = true, onChange }: { value: TriggerDraft; timezone: string; disabled: boolean; showKind?: boolean; onChange: (trigger: TriggerDraft) => void }) {
  const { t } = useTranslation();
  return <Stack spacing={1.5} className="system-schedule-rule-fields">
    {showKind && <TextField size="small" select label="When to run" value={value.kind} disabled={disabled} onChange={(event) => onChange({ ...value, kind: event.target.value as TaskTriggerKind })}>
      {scheduleRuleKinds.map((kind) => <MenuItem key={kind.value} value={kind.value}>{kind.label}</MenuItem>)}
    </TextField>}
    {value.kind === 'interval' && <DurationField label="Interval" draft={value.interval} disabled={disabled} onChange={(interval) => onChange({ ...value, interval })} />}
    {(value.kind === 'daily' || value.kind === 'weekly') && <Box className="system-schedule-calendar-fields">
      {value.kind === 'weekly' && <TextField size="small" select label="Day of week" value={value.day} disabled={disabled} onChange={(event) => onChange({ ...value, day: Number(event.target.value) })}>{weekdays.map((day, index) => <MenuItem key={day} value={index}>{day}</MenuItem>)}</TextField>}
      <TextField size="small" label="Time of day" value={value.time} disabled={disabled} onChange={(event) => onChange({ ...value, time: event.target.value })} helperText={t('24-hour time in {timezone}. Seconds and up to seven decimal places are supported.', { timezone })} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} />
    </Box>}
    {value.kind === 'startup' && <Typography variant="body2" color="text.secondary">Runs once after each server startup.</Typography>}
    {value.kind === 'system_event' && <TextField size="small" select label="System event" value={value.systemEvent} disabled={disabled} onChange={(event) => onChange({ ...value, systemEvent: event.target.value as TriggerDraft['systemEvent'] })} helperText="Runs for new server events after this schedule is saved. Overlapping work is combined into the active run.">
      {systemEvents.map((event) => <MenuItem key={event.value} value={event.value}>{taskTriggerLabel({ Kind: 'system_event', SystemEvent: event.value }, t)}</MenuItem>)}
    </TextField>}
    <details className="system-schedule-runtime" open={value.limitRuntime || undefined}>
      <summary>{t('Run time limit')}</summary>
      <FormControlLabel control={<Switch checked={value.limitRuntime} disabled={disabled} onChange={(event) => onChange({ ...value, limitRuntime: event.target.checked })} />} label="Limit run time" />
      {value.limitRuntime && <DurationField label="Maximum run time" draft={value.runtime} disabled={disabled} onChange={(runtime) => onChange({ ...value, runtime })} />}
    </details>
  </Stack>;
}
