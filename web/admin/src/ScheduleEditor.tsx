import { useEffect, useRef, useState } from 'react';
import { Alert, Autocomplete, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, IconButton, MenuItem, Paper, Skeleton, Stack, Switch, TextField, Typography } from '@mui/material';
import AddRounded from '@mui/icons-material/AddRounded';
import DeleteOutlineRounded from '@mui/icons-material/DeleteOutlineRounded';
import EventAvailableOutlined from '@mui/icons-material/EventAvailableOutlined';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import SaveOutlined from '@mui/icons-material/SaveOutlined';
import { adminApi, ApiError, isAbortError } from './api';
import type { TaskDefinition, TaskScheduleInput, TaskSchedulePreview, TaskTriggerKind } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { durationUnits, newTrigger, scheduleDate, scheduleFromTask, scheduleInput, weekdays } from './taskSchedule';
import type { DurationDraft, DurationUnit, ScheduleDraft, TriggerDraft } from './taskSchedule';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const timezoneChoices = ['UTC', ...Intl.supportedValuesOf('timeZone')];
const triggerKinds: { value: TaskTriggerKind; label: string }[] = [
  { value: 'interval', label: 'At an interval' }, { value: 'daily', label: 'Every day' },
  { value: 'weekly', label: 'Every week' }, { value: 'startup', label: 'At server startup' },
];

function DurationField({ label, draft, disabled, onChange }: { label: string; draft: DurationDraft; disabled: boolean; onChange: (value: DurationDraft) => void }) {
  return <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'minmax(0, 1fr) 160px' }, gap: 1 }}>
    <TextField label={label} value={draft.value} disabled={disabled} onChange={(event) => onChange({ ...draft, value: event.target.value })} slotProps={{ htmlInput: { inputMode: 'decimal', autoComplete: 'off' } }} />
    <TextField select label={`${label} unit`} value={draft.unit} disabled={disabled} onChange={(event) => onChange({ ...draft, unit: event.target.value as DurationUnit })}>
      {Object.keys(durationUnits).map((unit) => <MenuItem key={unit} value={unit}>{unit[0].toUpperCase() + unit.slice(1)}</MenuItem>)}
    </TextField>
  </Box>;
}

interface ScheduleEditorProps {
  taskId: string;
  onClose: () => void;
  onSaved: (task: TaskDefinition) => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}

export function ScheduleEditor({ taskId, onClose, onSaved, onNavigationGuardChange }: ScheduleEditorProps) {
  const [task, setTask] = useState<TaskDefinition>();
  const [draft, setDraft] = useState<ScheduleDraft>();
  const [baseline, setBaseline] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [error, setError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  const [busy, setBusy] = useState<'preview' | 'save'>();
  const [preview, setPreview] = useState<{ key: string; result: TaskSchedulePreview }>();
  const [notice, setNotice] = useState('');
  const [pendingAction, setPendingAction] = useState<'close' | 'reload'>();
  const mutation = useRef<AbortController | undefined>(undefined);
  const dirty = Boolean(draft && JSON.stringify(draft) !== baseline);
  const blocked = error instanceof ApiError && (error.status === 409 || error.status >= 500 || ['network_error', 'invalid_response'].includes(error.code));
  const disabled = loading || Boolean(busy);
  let input: TaskScheduleInput | undefined;
  let validation = '';
  try { if (draft) input = scheduleInput(draft); } catch (cause) { validation = cause instanceof Error ? cause.message : 'Review the schedule fields.'; }
  const inputKey = input ? JSON.stringify(input) : '';
  const previewMatches = Boolean(inputKey && preview?.key === inputKey);
  useUserDraftNavigation(dirty, Boolean(busy), onNavigationGuardChange, 'Discard unsaved schedule changes and leave this page?');

  function acceptTask(value: TaskDefinition) {
    const next = scheduleFromTask(value.ScheduleTimezone, value.Triggers);
    setTask(value);
    setDraft(next);
    setBaseline(JSON.stringify(next));
    setPreview(undefined);
    setError(undefined);
  }

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setLoadError(undefined);
    void adminApi.getTask(taskId, { signal: controller.signal })
      .then((result) => { if (!controller.signal.aborted) { acceptTask(result.Task); setNotice(''); } })
      .catch((cause: unknown) => { if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [taskId, revision]);
  useEffect(() => () => mutation.current?.abort(), []);

  function finishAction(action: 'close' | 'reload') {
    setPendingAction(undefined);
    if (action === 'close') onClose();
    else setRevision((value) => value + 1);
  }

  function requestAction(action: 'close' | 'reload') {
    if (mutation.current || (action === 'reload' && loading)) return;
    if (dirty) setPendingAction(action);
    else finishAction(action);
  }

  function updateDraft(next: ScheduleDraft) {
    setDraft(next);
    setPreview(undefined);
    setNotice('');
  }

  function changeTrigger(index: number, next: TriggerDraft) {
    if (draft) updateDraft({ ...draft, triggers: draft.triggers.map((trigger, current) => current === index ? next : trigger) });
  }

  async function previewSchedule() {
    if (!input || mutation.current || blocked || loadError) return;
    const controller = new AbortController();
    mutation.current = controller;
    setBusy('preview');
    setError(undefined);
    setPreview(undefined);
    try {
      const result = await adminApi.previewTaskSchedule(taskId, input, { signal: controller.signal });
      if (!controller.signal.aborted) setPreview({ key: inputKey, result });
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) {
        // Preview does not change the schedule; its transport error can be retried.
        setError(cause instanceof ApiError && (cause.status >= 500 || ['network_error', 'invalid_response'].includes(cause.code))
          ? new Error(`The preview could not be loaded. ${cause.message}`) : cause);
      }
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) setBusy(undefined);
    }
  }

  async function save() {
    if (!task || !input || !previewMatches || !dirty || blocked || mutation.current || loadError) return;
    const controller = new AbortController();
    mutation.current = controller;
    setBusy('save');
    setError(undefined);
    try {
      const result = await adminApi.updateTaskSchedule(taskId, { ...input, Revision: task.Revision }, { signal: controller.signal });
      if (!controller.signal.aborted) { acceptTask(result.Task); setNotice('Schedule saved.'); onSaved(result.Task); }
    } catch (cause) {
      if (!controller.signal.aborted && !isAbortError(cause)) setError(cause);
    } finally {
      if (mutation.current === controller) mutation.current = undefined;
      if (!controller.signal.aborted) setBusy(undefined);
    }
  }

  return <>
    <Dialog open onClose={() => requestAction('close')} fullWidth maxWidth="md" aria-labelledby="schedule-editor-title">
      <DialogTitle id="schedule-editor-title" sx={{ pt: 3, overflowWrap: 'anywhere' }}>Edit schedule{task ? ` · ${task.Name}` : ''}</DialogTitle>
      <DialogContent aria-busy={disabled}>
        <Stack spacing={2.5} sx={{ pt: 0.5 }}>
          {loading && <Stack role="status" aria-label="Loading schedule"><Skeleton height={56} /><Skeleton height={150} /></Stack>}
          {loadError != null && <ErrorNotice error={loadError} retry={() => setRevision((value) => value + 1)} />}
          {error != null && <ErrorNotice error={error} />}
          {blocked && <Alert severity="warning">{error instanceof ApiError && error.status === 409
            ? 'This schedule changed after you opened it. Reload the latest schedule and review it before saving again.'
            : 'The save result could not be confirmed. The schedule may already have changed. Reload the latest schedule before saving again.'}
            <Button color="inherit" size="small" startIcon={<RefreshRounded />} onClick={() => requestAction('reload')} sx={{ display: 'flex', mt: 1, ml: -1 }}>Reload latest schedule</Button>
          </Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          {draft && !loading && <>
            <Typography variant="body2" color="text.secondary">Automatic triggers use the selected time zone. Preview the next occurrences before saving.</Typography>
            <Autocomplete freeSolo options={timezoneChoices} value={draft.timezone} inputValue={draft.timezone} disabled={disabled || Boolean(loadError)} onInputChange={(_event, timezone) => updateDraft({ ...draft, timezone })} renderInput={(params) => <TextField {...params} label="Schedule time zone" error={Boolean(fieldError(error, 'ScheduleTimezone'))} helperText={fieldError(error, 'ScheduleTimezone') ?? 'Use UTC or an IANA name such as Europe/London.'} />} />
            {draft.triggers.map((trigger, index) => <Paper key={trigger.key} component="section" aria-label={`Trigger ${index + 1}`} variant="outlined" sx={{ p: { xs: 2, sm: 2.5 } }}>
              <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'center', mb: 2 }}><Typography component="h3" variant="h4">Trigger {index + 1}</Typography><IconButton aria-label={`Remove trigger ${index + 1}`} size="small" disabled={disabled || Boolean(loadError)} onClick={() => updateDraft({ ...draft, triggers: draft.triggers.filter((_value, current) => current !== index) })}><DeleteOutlineRounded /></IconButton></Stack>
              <Stack spacing={2}>
                {task?.Triggers.find((saved) => saved.Id === trigger.key)?.CalculationError && <Alert severity="warning" sx={{ '& .MuiAlert-message': { minWidth: 0, overflowWrap: 'anywhere' } }}>This saved trigger is paused. Adjust its schedule, preview the result, and save to resume it.<Typography variant="caption" component="div" sx={{ mt: 0.5 }}>{task?.Triggers.find((saved) => saved.Id === trigger.key)?.CalculationError}</Typography></Alert>}
                <TextField select label="When to run" value={trigger.kind} disabled={disabled || Boolean(loadError)} onChange={(event) => changeTrigger(index, { ...trigger, kind: event.target.value as TaskTriggerKind })}>
                  {triggerKinds.map((kind) => <MenuItem key={kind.value} value={kind.value}>{kind.label}</MenuItem>)}
                </TextField>
                {trigger.kind === 'interval' && <DurationField label="Interval" draft={trigger.interval} disabled={disabled || Boolean(loadError)} onChange={(interval) => changeTrigger(index, { ...trigger, interval })} />}
                {trigger.kind === 'weekly' && <TextField select label="Day of week" value={trigger.day} disabled={disabled || Boolean(loadError)} onChange={(event) => changeTrigger(index, { ...trigger, day: Number(event.target.value) })}>{weekdays.map((day, value) => <MenuItem key={day} value={value}>{day}</MenuItem>)}</TextField>}
                {(trigger.kind === 'daily' || trigger.kind === 'weekly') && <TextField label="Time of day" value={trigger.time} disabled={disabled || Boolean(loadError)} onChange={(event) => changeTrigger(index, { ...trigger, time: event.target.value })} helperText={`24-hour time in ${draft.timezone || 'the selected time zone'}. Use HH:mm or HH:mm:ss with up to seven decimal places.`} slotProps={{ htmlInput: { autoComplete: 'off', spellCheck: false } }} />}
                {trigger.kind === 'startup' && <Typography variant="body2" color="text.secondary">Runs when the server starts. A startup trigger has no calendar time.</Typography>}
                <FormControlLabel control={<Switch checked={trigger.limitRuntime} disabled={disabled || Boolean(loadError)} onChange={(event) => changeTrigger(index, { ...trigger, limitRuntime: event.target.checked })} />} label="Limit run time" />
                {trigger.limitRuntime && <DurationField label="Maximum run time" draft={trigger.runtime} disabled={disabled || Boolean(loadError)} onChange={(runtime) => changeTrigger(index, { ...trigger, runtime })} />}
              </Stack>
            </Paper>)}
            {draft.triggers.length === 0 && <Alert severity="info">No automatic triggers. This task can be started manually when it is enabled.</Alert>}
            <Box><Button startIcon={<AddRounded />} onClick={() => updateDraft({ ...draft, triggers: [...draft.triggers, newTrigger()] })} disabled={disabled || Boolean(loadError) || draft.triggers.length >= 32}>Add trigger</Button></Box>
            {validation && <Alert severity="warning">{validation}</Alert>}
            <Box><Button variant="outlined" startIcon={busy === 'preview' ? <CircularProgress size={16} color="inherit" /> : <EventAvailableOutlined />} onClick={() => void previewSchedule()} disabled={disabled || !input || blocked || Boolean(loadError)}>{busy === 'preview' ? 'Loading preview...' : 'Preview schedule'}</Button></Box>
            {previewMatches && preview && <Paper variant="outlined" component="section" aria-label="Schedule preview" sx={{ p: 2.5, bgcolor: 'background.default' }}>
              <Typography variant="h4" component="h3">Upcoming occurrences</Typography>
              <Typography variant="caption" color="text.secondary">Server time: {scheduleDate(preview.result.ServerTime, draft.timezone.trim())}</Typography>
              {preview.result.Items.length === 0 && <Typography variant="body2" sx={{ mt: 1.5 }}>No automatic runs are scheduled.</Typography>}
              {preview.result.Items.map((item) => <Box key={item.Index} sx={{ mt: 2 }}>
                <Typography variant="body2" sx={{ fontWeight: 650 }}>Trigger {item.Index + 1}</Typography>
                {item.Event && <Typography variant="body2" color="text.secondary">{draft.triggers[item.Index]?.kind === 'startup' ? 'At the next server startup' : item.Event}</Typography>}
                <Box component="ul" sx={{ m: 0, pl: 2.5 }}>{item.Occurrences.map((time) => <Typography key={time} component="li" variant="body2" sx={{ overflowWrap: 'anywhere' }}><time dateTime={time}>{scheduleDate(time, draft.timezone.trim())}</time></Typography>)}</Box>
              </Box>)}
              <Typography variant="caption" color="text.secondary" component="p" sx={{ mb: 0, mt: 2 }}>Preview uses the server clock. Saved interval times may differ slightly.</Typography>
            </Paper>}
          </>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 3, flexWrap: 'wrap', gap: 1 }}><Button color="secondary" onClick={() => requestAction('close')} disabled={Boolean(busy)}>Close</Button><Button variant="contained" startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />} onClick={() => void save()} disabled={disabled || !dirty || !previewMatches || blocked || Boolean(loadError)}>{busy === 'save' ? 'Saving schedule...' : 'Save schedule'}</Button></DialogActions>
    </Dialog>
    {pendingAction && <Dialog open onClose={() => setPendingAction(undefined)} fullWidth maxWidth="xs" aria-labelledby="discard-schedule-title"><DialogTitle id="discard-schedule-title">Discard schedule changes?</DialogTitle><DialogContent><Typography variant="body2" color="text.secondary">{pendingAction === 'reload' ? 'Reloading replaces your draft with the latest saved schedule.' : 'Your schedule changes have not been saved.'}</Typography></DialogContent><DialogActions sx={{ px: 3, pb: 2.5, flexWrap: 'wrap', gap: 1 }}><Button color="secondary" onClick={() => setPendingAction(undefined)} autoFocus>Keep editing</Button><Button color="error" variant="contained" onClick={() => finishAction(pendingAction)}>{pendingAction === 'reload' ? 'Discard draft and reload' : 'Discard changes'}</Button></DialogActions></Dialog>}
  </>;
}
