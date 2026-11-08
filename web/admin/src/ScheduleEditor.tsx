import { useEffect, useRef, useState } from 'react';
import { Alert, Autocomplete, Box, Button, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Paper, Skeleton, Stack, TextField, Typography } from './ui';
import { AddRounded } from './icons';
import { DeleteOutlineRounded } from './icons';
import { EventAvailableOutlined } from './icons';
import { RefreshRounded } from './icons';
import { SaveOutlined } from './icons';
import { ScheduleOutlined } from './icons';
import { adminApi, ApiError, isAbortError } from './api';
import type { TaskDefinition, TaskScheduleInput, TaskSchedulePreview } from './api';
import { ErrorNotice } from './components';
import { fieldError } from './formFields';
import { newTrigger, scheduleDate, scheduleFromTask, scheduleInput } from './taskSchedule';
import type { ScheduleDraft, TriggerDraft } from './taskSchedule';
import { ScheduleRuleFields, ScheduleRuleKindPicker } from './ScheduleRuleFields';
import { taskTriggerLabel } from './taskTriggerPresentation';
import { useTranslation } from './i18n';
import type { TaskTranslator } from './taskTriggerPresentation';
import { useUserDraftNavigation } from './userDraftNavigation';
import type { UserNavigationGuardChange } from './userDraftNavigation';

const timezoneChoices = ['UTC', ...Intl.supportedValuesOf('timeZone')];
function inputForTrigger(trigger: TriggerDraft) { return scheduleInput({ timezone: 'UTC', triggers: [trigger] }).Triggers[0]; }

function draftTriggerLabel(trigger: TriggerDraft, t: TaskTranslator): string {
  try { return taskTriggerLabel(inputForTrigger(trigger), t); }
  catch { return t('Review this rule'); }
}

function scheduleFieldLabel(field: string, t: TaskTranslator): string {
  const labels: Record<string, string> = {
    scheduletimezone: 'Schedule time zone', triggers: 'Trigger rules', passphrase: 'Backup passphrase',
    backuppassphrase: 'Backup passphrase', credential: 'Backup credentials', credentials: 'Backup credentials',
    encryptionpassphrase: 'Backup passphrase', revision: 'Saved schedule version',
  };
  if (labels[field.toLowerCase()]) return t(labels[field.toLowerCase()]);
  const trigger = /^Triggers\[(\d+)\](?:\.(.+))?$/i.exec(field);
  if (trigger) return t('Trigger {count}', { count: Number(trigger[1]) + 1 });
  return t(field.replace(/([a-z])([A-Z])/g, '$1 $2').replaceAll('_', ' '));
}

interface ScheduleEditorProps {
  taskId: string;
  onClose: () => void;
  onSaved: (task: TaskDefinition) => void;
  onNavigationGuardChange: UserNavigationGuardChange;
}

export function ScheduleEditor({ taskId, onClose, onSaved, onNavigationGuardChange }: ScheduleEditorProps) {
  const { t, locale } = useTranslation();
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
  const [expandedRule, setExpandedRule] = useState<string>();
  const [candidate, setCandidate] = useState(newTrigger);
  const [candidateTouched, setCandidateTouched] = useState(false);
  const [candidateError, setCandidateError] = useState('');
  const mutation = useRef<AbortController | undefined>(undefined);
  const fieldErrorSummary = useRef<HTMLDivElement>(null);
  const scheduleDirty = Boolean(draft && JSON.stringify(draft) !== baseline);
  const dirty = scheduleDirty || candidateTouched;
  const blocked = error instanceof ApiError && (error.status === 409 || error.status >= 500 || ['network_error', 'invalid_response'].includes(error.code));
  const disabled = loading || Boolean(busy);
  let input: TaskScheduleInput | undefined;
  let validation = '';
  try { if (draft) input = scheduleInput(draft); } catch (cause) { validation = cause instanceof Error ? cause.message : 'Review the schedule fields.'; }
  const inputKey = input ? JSON.stringify(input) : '';
  const previewMatches = Boolean(inputKey && preview?.key === inputKey);
  const fieldErrors = error instanceof ApiError && error.fields ? Object.entries(error.fields).flatMap(([field, messages]) => (Array.isArray(messages) ? messages : [messages]).map((message) => ({ field, message }))) : [];
  useUserDraftNavigation(dirty, Boolean(busy), onNavigationGuardChange, 'Discard unsaved schedule changes and leave this page?');

  function acceptTask(value: TaskDefinition) {
    const next = scheduleFromTask(value.ScheduleTimezone, value.Triggers);
    setTask(value);
    setDraft(next);
    setBaseline(JSON.stringify(next));
    setPreview(undefined);
    setError(undefined);
    setExpandedRule(undefined);
    setCandidate(newTrigger());
    setCandidateTouched(false);
    setCandidateError('');
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
  useEffect(() => { if (error instanceof ApiError && error.fields) fieldErrorSummary.current?.focus(); }, [error]);

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

  function changeCandidate(next: TriggerDraft) {
    setCandidate(next);
    setCandidateTouched(true);
    setCandidateError('');
    setPreview(undefined);
    setNotice('');
  }

  function addTrigger() {
    if (!draft || disabled || loadError || draft.triggers.length >= 32) return;
    try {
      const key = JSON.stringify(inputForTrigger(candidate));
      const duplicate = draft.triggers.some((trigger) => { try { return JSON.stringify(inputForTrigger(trigger)) === key; } catch { return false; } });
      if (duplicate) { setCandidateError('This rule already exists. Edit the existing rule or choose another time.'); return; }
      updateDraft({ ...draft, triggers: [...draft.triggers, candidate] });
      setCandidate(newTrigger());
      setCandidateTouched(false);
      setCandidateError('');
    } catch (cause) { setCandidateError(cause instanceof Error ? cause.message : 'Review this rule'); }
  }

  function ruleOccurrence(trigger: TriggerDraft, index: number): { label: string; time?: string } {
    const item = previewMatches ? preview?.result.Items[index] : undefined;
    if (item?.Occurrences[0]) return { label: t('Next {time}', { time: scheduleDate(item.Occurrences[0], draft!.timezone.trim()) }), time: item.Occurrences[0] };
    const saved = task?.Triggers.find((value) => value.Id === trigger.key);
    let unchanged = false;
    try { unchanged = Boolean(saved && task && draft?.timezone.trim() === task.ScheduleTimezone && JSON.stringify(inputForTrigger(trigger)) === JSON.stringify(inputForTrigger(scheduleFromTask(task.ScheduleTimezone, [saved]).triggers[0]))); } catch { /* Invalid drafts need a new preview. */ }
    if (!previewMatches && unchanged && saved?.NextFireAt) return { label: t('Next {time}', { time: scheduleDate(saved.NextFireAt, task!.ScheduleTimezone) }), time: saved.NextFireAt };
    if (trigger.kind === 'startup') return { label: t('Runs once after each server startup.') };
    if (trigger.kind === 'system_event') return { label: t('Runs for new matching server events.') };
    return { label: t(previewMatches ? 'No timed occurrence' : 'Preview to calculate the next run.') };
  }

  async function previewSchedule() {
    if (!input || candidateTouched || mutation.current || blocked || loadError) return;
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
    if (!task || !input || !previewMatches || !scheduleDirty || candidateTouched || blocked || mutation.current || loadError) return;
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
    <Dialog open onClose={() => requestAction('close')} fullWidth maxWidth="sm" data-layout="modal" className="system-schedule-editor" aria-labelledby="schedule-editor-title">
      <DialogTitle id="schedule-editor-title">Trigger rules{task && <Typography component="span" variant="body2" className="system-schedule-task-name">{task.Name}</Typography>}</DialogTitle>
      <DialogContent aria-busy={disabled}>
        <Stack spacing={2.5} sx={{ pt: 0.5 }}>
          {loading && <Stack role="status" aria-label="Loading schedule"><Skeleton height={56} /><Skeleton height={150} /></Stack>}
          {loadError != null && <ErrorNotice error={loadError} retry={() => setRevision((value) => value + 1)} />}
          {error != null && <ErrorNotice error={error} />}
          {fieldErrors.length > 0 && <div ref={fieldErrorSummary} tabIndex={-1} role="group" aria-label={t('Schedule field errors')} className="system-schedule-field-errors"><ul>{fieldErrors.map(({ field, message }, index) => <li key={field + ':' + index}><strong>{scheduleFieldLabel(field, t)}</strong><span>{t(message)}</span></li>)}</ul>{task?.Key.includes('backup') && <Button component="a" href="/admin/system/backups" target="_blank" rel="noreferrer" size="small">Open backup settings</Button>}</div>}
          {blocked && <Alert severity="warning">{error instanceof ApiError && error.status === 409
            ? 'This schedule changed after you opened it. Reload the latest schedule and review it before saving again.'
            : 'The save result could not be confirmed. The schedule may already have changed. Reload the latest schedule before saving again.'}
            <Button color="inherit" size="small" startIcon={<RefreshRounded />} onClick={() => requestAction('reload')} sx={{ display: 'flex', mt: 1, ml: -1 }}>Reload latest schedule</Button>
          </Alert>}
          {notice && <Alert severity="success">{notice}</Alert>}
          {draft && !loading && <>
            <p className="system-schedule-rule-count">{t(draft.triggers.length ? '{count} rules · runs when any rule matches' : 'No triggers yet', { count: draft.triggers.length.toLocaleString(locale) })}</p>
            <div className="system-schedule-rules" aria-label={t('Configured trigger rules')}>
              {draft.triggers.map((trigger, index) => {
                const occurrence = ruleOccurrence(trigger, index);
                const calculationError = task?.Triggers.find((saved) => saved.Id === trigger.key)?.CalculationError;
                return <section key={trigger.key} aria-label={t('Trigger {count}', { count: index + 1 })} className="system-schedule-rule" data-expanded={expandedRule === trigger.key || undefined}>
                  <div className="system-schedule-rule-row"><ScheduleOutlined aria-hidden="true" /><button type="button" className="system-schedule-rule-open" aria-label={t('Edit trigger {count}', { count: index + 1 })} aria-expanded={expandedRule === trigger.key} aria-controls={expandedRule === trigger.key ? 'schedule-rule-fields-' + index : undefined} disabled={disabled || Boolean(loadError)} onClick={() => setExpandedRule(expandedRule === trigger.key ? undefined : trigger.key)}><strong>{draftTriggerLabel(trigger, t)}</strong>{occurrence.time ? <time className="system-mono" dateTime={occurrence.time} title={occurrence.label}>{occurrence.label}</time> : <span className="system-mono">{occurrence.label}</span>}</button><IconButton aria-label={t('Remove trigger {count}', { count: index + 1 })} size="small" disabled={disabled || Boolean(loadError)} onClick={() => updateDraft({ ...draft, triggers: draft.triggers.filter((_value, current) => current !== index) })}><DeleteOutlineRounded /></IconButton></div>
                  {calculationError && <Alert severity="warning" className="system-schedule-rule-warning">This saved trigger is paused. Adjust its schedule, preview the result, and save to resume it.<Typography variant="caption" component="div">{calculationError}</Typography></Alert>}
                  {expandedRule === trigger.key && <div id={'schedule-rule-fields-' + index} className="system-schedule-rule-detail"><ScheduleRuleFields value={trigger} timezone={draft.timezone || t('the selected time zone')} disabled={disabled || Boolean(loadError)} onChange={(next) => changeTrigger(index, next)} /><Button size="small" onClick={() => setExpandedRule(undefined)}>Done editing rule</Button></div>}
                </section>;
              })}
              {draft.triggers.length === 0 && <p className="system-schedule-no-rules"><ScheduleOutlined aria-hidden="true" />{t('No triggers · manual only')}</p>}
            </div>
            <section className="system-schedule-composer" aria-label={t('Add a rule')}>
              <h3>{t('Add a rule')}</h3>
              <ScheduleRuleKindPicker value={candidate.kind} disabled={disabled || Boolean(loadError) || draft.triggers.length >= 32} onChange={(kind) => changeCandidate({ ...candidate, kind })} />
              <ScheduleRuleFields value={candidate} timezone={draft.timezone || t('the selected time zone')} showKind={false} disabled={disabled || Boolean(loadError) || draft.triggers.length >= 32} onChange={changeCandidate} />
              {candidateError && <p className="system-schedule-rule-error" role="alert">{t(candidateError)}</p>}
              <div className="system-schedule-composer-actions">{candidateTouched && <Button size="small" onClick={() => { setCandidate(newTrigger()); setCandidateTouched(false); setCandidateError(''); }} disabled={disabled}>Reset new rule</Button>}<Button variant="outlined" size="small" startIcon={<AddRounded />} onClick={addTrigger} disabled={disabled || Boolean(loadError) || draft.triggers.length >= 32}>Add trigger</Button></div>
            </section>
            <details className="system-schedule-timezone" open={Boolean(fieldError(error, 'ScheduleTimezone')) || undefined}><summary>{t('Schedule time zone')}<span className="system-mono">{draft.timezone}</span></summary><Autocomplete freeSolo options={timezoneChoices} value={draft.timezone} inputValue={draft.timezone} disabled={disabled || Boolean(loadError)} onInputChange={(_event, timezone) => updateDraft({ ...draft, timezone })} renderInput={(params) => <TextField {...params} size="small" label="Schedule time zone" error={Boolean(fieldError(error, 'ScheduleTimezone'))} helperText={fieldError(error, 'ScheduleTimezone') ?? 'Use UTC or an IANA name such as Europe/London.'} />} /></details>
            {validation && <Alert severity="warning">{validation}</Alert>}
            {candidateTouched && <Typography variant="caption" color="text.secondary">Add the new rule or reset it before previewing and saving.</Typography>}
            <Box><Button variant="outlined" size="small" startIcon={busy === 'preview' ? <CircularProgress size={16} color="inherit" /> : <EventAvailableOutlined />} onClick={() => void previewSchedule()} disabled={disabled || !input || candidateTouched || blocked || Boolean(loadError)}>{busy === 'preview' ? 'Loading preview...' : 'Preview schedule'}</Button></Box>
            {previewMatches && preview && <Paper variant="outlined" component="section" aria-label="Schedule preview" className="system-schedule-preview">
              <Typography variant="body2" sx={{ fontWeight: 600 }}>Schedule preview is current.</Typography>
              <Typography variant="caption" color="text.secondary">Server time: {scheduleDate(preview.result.ServerTime, draft.timezone.trim())}</Typography>
              {preview.result.Items.length === 0 && <Typography variant="body2" sx={{ mt: 1.5 }}>No automatic runs are scheduled.</Typography>}
              <details><summary>{t('Upcoming occurrences')}</summary>{preview.result.Items.map((item) => <Box key={item.Index} sx={{ mt: 1.5 }}>
                <Typography variant="body2" sx={{ fontWeight: 650 }}>{t('Trigger {count}', { count: item.Index + 1 })}</Typography>
                {item.Event && <Typography variant="body2" color="text.secondary">{draft.triggers[item.Index]?.kind === 'startup' ? 'At the next server startup' : taskTriggerLabel(input!.Triggers[item.Index], t)}</Typography>}
                <Box component="ul" sx={{ m: 0, pl: 2.5 }}>{item.Occurrences.map((time) => <Typography key={time} component="li" variant="body2" sx={{ overflowWrap: 'anywhere' }}><time dateTime={time}>{scheduleDate(time, draft.timezone.trim())}</time></Typography>)}</Box>
              </Box>)}</details>
              <Typography variant="caption" color="text.secondary" component="p" sx={{ mb: 0, mt: 2 }}>Preview uses the server clock. Saved interval times may differ slightly.</Typography>
            </Paper>}
          </>}
        </Stack>
      </DialogContent>
      <DialogActions className="system-schedule-editor-actions"><Typography variant="caption">Preview and save to apply rule changes.</Typography><Button color="secondary" onClick={() => requestAction('close')} disabled={Boolean(busy)}>Close</Button><Button variant="contained" startIcon={busy === 'save' ? <CircularProgress size={16} color="inherit" /> : <SaveOutlined />} onClick={() => void save()} disabled={disabled || !scheduleDirty || candidateTouched || !previewMatches || blocked || Boolean(loadError)}>{busy === 'save' ? 'Saving schedule...' : 'Save schedule'}</Button></DialogActions>
    </Dialog>
    {pendingAction && <Dialog open onClose={() => setPendingAction(undefined)} fullWidth maxWidth="xs" aria-labelledby="discard-schedule-title"><DialogTitle id="discard-schedule-title">Discard schedule changes?</DialogTitle><DialogContent><Typography variant="body2" color="text.secondary">{pendingAction === 'reload' ? 'Reloading replaces your draft with the latest saved schedule.' : 'Your schedule changes have not been saved.'}</Typography></DialogContent><DialogActions sx={{ px: 3, pb: 2.5, flexWrap: 'wrap', gap: 1 }}><Button color="secondary" onClick={() => setPendingAction(undefined)} autoFocus>Keep editing</Button><Button color="error" variant="contained" onClick={() => finishAction(pendingAction)}>{pendingAction === 'reload' ? 'Discard draft and reload' : 'Discard changes'}</Button></DialogActions></Dialog>}
  </>;
}
