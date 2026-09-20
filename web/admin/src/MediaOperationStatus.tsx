import { Alert, Box, Chip, LinearProgress, Stack, Typography } from '@mui/material';
import type { ChipProps } from '@mui/material';
import type { MediaOperation, MediaOperationKind, MediaOperationState } from './mediaOperationsApi';

const stateLabels: Record<MediaOperationState, string> = {
  queued: 'Queued', running: 'Preparing', ready: 'Ready for review', applying: 'Applying', completed: 'Published',
  failed: 'Failed', cancelled: 'Cancelled', interrupted: 'Interrupted', stale: 'Source changed', recovery_required: 'Recovery required',
};
export function mediaOperationKindLabel(kind: MediaOperationKind): string { return kind === 'subtitle_ocr' ? 'Subtitle OCR' : 'Remove embedded subtitle'; }
export function mediaOperationStateLabel(state: MediaOperationState): string { return stateLabels[state]; }
export function MediaOperationStatusChip({ operation }: { operation: MediaOperation }) {
  const color: ChipProps['color'] = operation.State === 'completed' ? 'success' : operation.State === 'failed' ? 'error'
    : ['recovery_required', 'interrupted', 'stale'].includes(operation.State) ? 'warning' : ['running', 'applying'].includes(operation.State) ? 'primary' : 'default';
  const cancellationPending = operation.CancelRequestedAt && !operation.Applied && ['queued', 'running', 'ready', 'applying'].includes(operation.State);
  return <Chip size="small" color={color} variant="outlined" label={cancellationPending ? 'Cancellation requested' : stateLabels[operation.State]} />;
}
export function MediaOperationProgress({ operation }: { operation: MediaOperation }) {
  const progress = operation.Progress;
  const active = ['queued', 'running', 'applying'].includes(operation.State);
  return <Stack spacing={1}>
    <Typography variant="body2">{progress.Stage ? progress.Stage.replaceAll('_', ' ') : operation.State === 'queued' ? 'Waiting for a worker' : stateLabels[operation.State]}</Typography>
    {(active || progress.Total > 0) && <LinearProgress aria-label="Media processing progress" variant={progress.Total > 0 ? 'determinate' : 'indeterminate'} value={progress.Total > 0 ? Math.min(100, progress.Processed / progress.Total * 100) : undefined} />}
    {progress.Total > 0 && <Typography variant="caption" color="text.secondary">{progress.Processed.toLocaleString()} of {progress.Total.toLocaleString()} processed</Typography>}
  </Stack>;
}
export function MediaOperationNotice({ operation }: { operation: MediaOperation }) {
  const messages: Partial<Record<MediaOperationState, string>> = {
    queued: 'Preparation is queued. Review the result before applying it.',
    running: 'Preparation is running. The original media has not been replaced.',
    ready: operation.Kind === 'subtitle_ocr' ? 'Review the recognized text and timings before publishing this subtitle.' : 'The replacement is prepared. Review the result, then explicitly apply the removal.',
    applying: 'The reviewed result is being published. Keep this operation available until its final status is recorded.',
    completed: operation.Kind === 'subtitle_ocr' ? 'The reviewed subtitle has been published to the catalog.' : 'The selected embedded subtitle was removed and the replacement media was published.',
    cancelled: 'The operation was cancelled. Check the publication status below for any work already committed.',
    interrupted: 'Preparation stopped before completion. Start a new operation from the item after reviewing this record.',
    stale: 'The media source changed after preparation. This result cannot be applied. Reload the item and prepare a new operation.',
    recovery_required: 'Publication requires recovery. Review the recorded state and use Recover publication to resume the server-managed recovery.',
    failed: 'The operation failed. Review the error before preparing a new operation.',
  };
  return <Stack spacing={1}>
    <Alert severity={['failed'].includes(operation.State) ? 'error' : ['stale', 'interrupted', 'recovery_required'].includes(operation.State) ? 'warning' : operation.State === 'completed' ? 'success' : 'info'}>{messages[operation.State]}</Alert>
    {!operation.TargetPresent && <Alert severity="warning">The original catalog item is no longer available. This retained record cannot be applied to another item.</Alert>}
    {operation.ErrorMessage && <Alert severity="error"><Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{operation.ErrorMessage}</Typography>{operation.ErrorCode && <Typography variant="caption">Error code: {operation.ErrorCode}</Typography>}</Alert>}
    {operation.Applied && operation.State !== 'completed' && <Alert severity="warning">The catalog already records publication. Recovery may still be required to finish cleanup.</Alert>}
  </Stack>;
}
export function MediaOperationSummary({ operation }: { operation: MediaOperation }) {
  const summary = operation.ResultSummary;
  const fields = [
    ['ContainerProfile', 'Container profile'], ['RemovedStreamIndex', 'Removed stream'], ['PreservedStreamCount', 'Preserved streams'],
    ['OriginalBytes', 'Original bytes'], ['CandidateBytes', 'Prepared bytes'], ['CueCount', 'Recognized cues'], ['WarningCount', 'Warnings'],
    ['ModelID', 'OCR model'], ['AppliedFormat', 'Published format'], ['AppliedStreamIndex', 'Published subtitle stream'],
  ] as const;
  const rows = fields.filter(([field]) => typeof summary[field] === 'string' || typeof summary[field] === 'number');
  const warnings = Array.isArray(summary.Warnings) ? summary.Warnings.filter((entry): entry is string => typeof entry === 'string') : [];
  return <Stack spacing={2}>
    {rows.length > 0 && <Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 1.5 }}>{rows.map(([field, label]) => <Box key={field}><Typography component="dt" variant="caption" color="text.secondary">{label}</Typography><Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere' }}>{String(summary[field])}</Typography></Box>)}</Box>}
    {summary.BackupRetained === true && <Alert severity="info">The server retained the original media for publication recovery.</Alert>}
    {warnings.map((warning, index) => <Alert severity="warning" key={`${index}:${warning}`}>{warning}</Alert>)}
  </Stack>;
}
