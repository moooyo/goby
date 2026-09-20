import { useCallback, useEffect, useState } from 'react';
import { Box, Button, Chip, LinearProgress, MenuItem, Paper, Skeleton, Stack, TablePagination, TextField, Tooltip, Typography } from '@mui/material';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { ErrorNotice } from './components';
import { MediaOperationDialog } from './MediaOperationDialog';
import { MediaOperationProgress, MediaOperationStatusChip, mediaOperationKindLabel, mediaOperationStateLabel } from './MediaOperationStatus';
import { mediaOperationIsActive, mediaOperationKinds, mediaOperationsApi, mediaOperationStates } from './mediaOperationsApi';
import type { MediaOperation, MediaOperationKind, MediaOperationPage, MediaOperationState } from './mediaOperationsApi';
import { TaskTimestamp } from './TaskRunDialog';
import { useTaskResource } from './useTaskResource';
import type { UserNavigationGuardChange } from './userDraftNavigation';

interface OperationQuery { kind: MediaOperationKind | ''; state: MediaOperationState | ''; page: number; limit: number }
const pageSizes = [25, 50, 100];
const pollActiveOperations = (result: MediaOperationPage) => result.Items.some(mediaOperationIsActive);

function Identifier({ value }: { value: string }) {
  const abbreviated = value.length > 20 ? `${value.slice(0, 8)}\u2026${value.slice(-6)}` : value;
  return <Tooltip title={value} describeChild><Typography component="span" variant="caption" className="mono" tabIndex={0} sx={{ overflowWrap: 'anywhere' }}>{abbreviated}</Typography></Tooltip>;
}

function OperationRow({ operation, onOpen }: { operation: MediaOperation; onOpen: () => void }) {
  return <Paper component="li" variant="outlined" data-operation-id={operation.Id} sx={{ p: { xs: 2, sm: 2.5 } }}>
    <Stack spacing={2}>
      <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: 'flex-start', justifyContent: 'space-between', gap: 1.5 }}>
        <Box sx={{ minWidth: 0 }}>
          <Typography component="h3" variant="h4">{mediaOperationKindLabel(operation.Kind)}</Typography>
          <Stack direction="row" sx={{ gap: 2, flexWrap: 'wrap', mt: 0.75, color: 'text.secondary' }}>
            <Typography variant="caption">Item <Identifier value={operation.ItemId} /></Typography>
            <Typography variant="caption">Subtitle stream {operation.StreamIndex}</Typography>
            <Typography variant="caption">Operation <Identifier value={operation.Id} /></Typography>
          </Stack>
        </Box>
        <MediaOperationStatusChip operation={operation} />
      </Stack>
      <MediaOperationProgress operation={operation} />
      {operation.CancelRequestedAt && mediaOperationIsActive(operation) && <Typography variant="body2" color="text.secondary">Cancellation requested. Waiting for the operation to stop.</Typography>}
      {operation.ErrorMessage && <Typography variant="body2" color={operation.State === 'failed' ? 'error.main' : 'text.secondary'} sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{operation.ErrorMessage}</Typography>}
      <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { xs: 'flex-start', sm: 'center' }, justifyContent: 'space-between', gap: 1.5 }}>
        <Box component="dl" sx={{ display: 'flex', gap: 3, flexWrap: 'wrap', m: 0 }}>
          <Box><Typography component="dt" variant="caption" color="text.secondary">Created</Typography><Typography component="dd" variant="caption" sx={{ m: 0 }}><TaskTimestamp value={operation.CreatedAt} /></Typography></Box>
          <Box><Typography component="dt" variant="caption" color="text.secondary">Updated</Typography><Typography component="dd" variant="caption" sx={{ m: 0 }}><TaskTimestamp value={operation.UpdatedAt} /></Typography></Box>
        </Box>
        <Button variant="outlined" onClick={onOpen} aria-label={`View media operation ${operation.Id}`} sx={{ flexShrink: 0 }}>View operation</Button>
      </Stack>
    </Stack>
  </Paper>;
}

export function MediaOperationsPanel({ onNavigationGuardChange }: { onNavigationGuardChange: UserNavigationGuardChange }) {
  const [query, setQuery] = useState<OperationQuery>({ kind: '', state: '', page: 0, limit: 25 });
  const [selected, setSelected] = useState<string>();
  const load = useCallback((signal: AbortSignal) => mediaOperationsApi.list({
    StartIndex: query.page * query.limit, Limit: query.limit,
    ...(query.kind ? { Kind: query.kind } : {}), ...(query.state ? { State: query.state } : {}),
  }, { signal }), [query]);
  const resource = useTaskResource({ key: `${query.kind}:${query.state}:${query.page}:${query.limit}`, load, poll: pollActiveOperations });
  const hasFilters = Boolean(query.kind || query.state);
  const hasActive = resource.data?.Items.some(mediaOperationIsActive) ?? false;

  useEffect(() => {
    if (!resource.data) return;
    const lastPage = Math.max(0, Math.ceil(resource.data.TotalRecordCount / query.limit) - 1);
    if (query.page > lastPage) setQuery((current) => current === query ? { ...current, page: lastPage } : current);
  }, [resource.data, query]);

  function filterKind(kind: string) {
    if (kind === '' || mediaOperationKinds.includes(kind as MediaOperationKind)) setQuery((current) => ({ ...current, kind: kind as OperationQuery['kind'], page: 0 }));
  }
  function filterState(state: string) {
    if (state === '' || mediaOperationStates.includes(state as MediaOperationState)) setQuery((current) => ({ ...current, state: state as OperationQuery['state'], page: 0 }));
  }

  return <Stack spacing={2.5} aria-busy={resource.loading}>
    <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1.5 }}>
      <Box>
        <Stack direction="row" sx={{ alignItems: 'center', gap: 1 }}><Typography component="h2" variant="h4">Media processing</Typography>{resource.data && <Chip size="small" label={resource.data.TotalRecordCount.toLocaleString()} />}</Stack>
        <Typography variant="caption" color="text.secondary">{resource.error != null ? 'Automatic updates stopped after a request error.' : resource.paused ? 'Updates pause while this tab is hidden.' : hasActive ? 'Active operations update every 5 seconds.' : 'Review subtitle processing jobs and their results.'}</Typography>
      </Box>
      <Button size="small" startIcon={<RefreshRounded />} onClick={resource.reload} disabled={resource.loading}>Refresh operations</Button>
    </Stack>
    <Paper variant="outlined" sx={{ p: { xs: 2, sm: 2.5 } }}>
      <Stack direction={{ xs: 'column', md: 'row' }} spacing={2} sx={{ alignItems: { md: 'center' } }}>
        <TextField select label="Operation kind" value={query.kind} onChange={(event) => filterKind(event.target.value)} slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }} sx={{ flex: 1, minWidth: { md: 180 } }}>
          <MenuItem value="">All kinds</MenuItem>{mediaOperationKinds.map((kind) => <MenuItem key={kind} value={kind}>{mediaOperationKindLabel(kind)}</MenuItem>)}
        </TextField>
        <TextField select label="Operation state" value={query.state} onChange={(event) => filterState(event.target.value)} slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }} sx={{ flex: 1, minWidth: { md: 180 } }}>
          <MenuItem value="">All states</MenuItem>{mediaOperationStates.map((state) => <MenuItem key={state} value={state}>{mediaOperationStateLabel(state)}</MenuItem>)}
        </TextField>
        <Button onClick={() => setQuery((current) => ({ ...current, kind: '', state: '', page: 0 }))} disabled={!hasFilters} sx={{ flexShrink: 0 }}>Clear filters</Button>
      </Stack>
    </Paper>
    {resource.error != null && <ErrorNotice error={resource.error} retry={resource.reload} />}
    {!resource.data && resource.loading && <Stack role="status" aria-label="Loading media operations" spacing={2}><Skeleton variant="rounded" height={190} /><Skeleton variant="rounded" height={190} /></Stack>}
    {resource.data && <>
      {resource.loading && <LinearProgress aria-label="Refreshing media operations" />}
      {resource.data.Items.length === 0 ? <Paper variant="outlined" sx={{ p: 4 }}>
        <Typography component="h3" variant="h3">{hasFilters ? 'No matching operations' : 'No media processing operations'}</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>{hasFilters ? 'Choose another kind or state, or clear the filters.' : 'Open a movie or episode in a library to start subtitle processing.'}</Typography>
      </Paper> : <Stack component="ul" aria-label="Media processing operations" spacing={2} sx={{ p: 0, m: 0, listStyle: 'none' }}>
        {resource.data.Items.map((operation) => <OperationRow key={operation.Id} operation={operation} onOpen={() => setSelected(operation.Id)} />)}
      </Stack>}
      <TablePagination component="div" count={resource.data.TotalRecordCount} page={Math.min(query.page, Math.max(0, Math.ceil(resource.data.TotalRecordCount / query.limit) - 1))} rowsPerPage={query.limit} rowsPerPageOptions={pageSizes} labelRowsPerPage="Operations per page" disabled={resource.loading}
        onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))}
        onRowsPerPageChange={(event) => { const limit = Number(event.target.value); if (pageSizes.includes(limit)) setQuery((current) => ({ ...current, limit, page: 0 })); }}
        sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 0, sm: 1 }, gap: 0.5 }, '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } }, '& .MuiTablePagination-actions': { ml: 1 } }} />
    </>}
    {selected && <MediaOperationDialog key={selected} operationId={selected} onClose={() => { setSelected(undefined); resource.reload(); }} onChanged={resource.reload} onNavigationGuardChange={onNavigationGuardChange} />}
  </Stack>;
}
