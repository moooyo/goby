import { useCallback, useEffect, useState } from 'react';
import { Box, Button, IconButton, LinearProgress, MenuItem, Paper, Skeleton, Stack, TablePagination, TextField, Tooltip, Typography } from '@mui/material';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import VideoSettingsRounded from '@mui/icons-material/VideoSettingsRounded';
import { ErrorNotice } from './components';
import { MediaOperationDialog } from './MediaOperationDialog';
import { MediaOperationProgress, MediaOperationStatusChip, mediaOperationKindLabel, mediaOperationStateLabel } from './MediaOperationStatus';
import { mediaOperationIsActive, mediaOperationKinds, mediaOperationsApi, mediaOperationStates } from './mediaOperationsApi';
import type { MediaOperation, MediaOperationKind, MediaOperationPage, MediaOperationState } from './mediaOperationsApi';
import { TaskTimestamp } from './TaskRunDialog';
import { colors } from './theme';
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
  return <Box component="li" data-operation-id={operation.Id} sx={{ display: 'grid', gridTemplateColumns: { xs: '48px minmax(0, 1fr)', lg: '48px minmax(0, 1.4fr) minmax(0, 1.2fr) minmax(190px, 1fr) auto' }, alignItems: 'center', gap: { xs: 1.5, lg: 2.5 }, px: { xs: 2.5, sm: 3 }, py: 2.5, borderBottom: '1px solid', borderColor: 'divider', '&:last-child': { borderBottom: 0 }, '&:hover': { bgcolor: colors.surface } }}>
    <Box sx={{ width: 48, height: 48, display: 'grid', placeItems: 'center', borderRadius: '14px', bgcolor: colors.iconContainer, color: 'primary.main', alignSelf: 'start' }}><VideoSettingsRounded aria-hidden="true" /></Box>
    <Box sx={{ minWidth: 0 }}>
      <Typography component="h3" variant="h4">{mediaOperationKindLabel(operation.Kind)}</Typography>
      <Stack spacing={0.5} sx={{ mt: 0.75, color: 'text.secondary' }}>
        <Typography variant="caption">Item <Identifier value={operation.ItemId} /></Typography>
        <Typography variant="caption">Subtitle stream {operation.StreamIndex}</Typography>
        <Typography variant="caption">Operation <Identifier value={operation.Id} /></Typography>
      </Stack>
    </Box>
    <Stack spacing={1.25} sx={{ gridColumn: { xs: '1 / -1', lg: 'auto' }, minWidth: 0, overflowWrap: 'anywhere', '& > .MuiChip-root': { alignSelf: 'flex-start' } }}>
      <MediaOperationStatusChip operation={operation} />
      <MediaOperationProgress operation={operation} />
    </Stack>
    <Box component="dl" sx={{ display: 'flex', flexDirection: { xs: 'row', lg: 'column' }, gap: { xs: 2.5, lg: 1 }, flexWrap: 'wrap', gridColumn: { xs: '1 / -1', lg: 'auto' }, m: 0 }}>
      <Box><Typography component="dt" variant="caption" color="text.secondary">Created</Typography><Typography component="dd" variant="caption" sx={{ m: 0, whiteSpace: 'nowrap' }}><TaskTimestamp value={operation.CreatedAt} /></Typography></Box>
      <Box><Typography component="dt" variant="caption" color="text.secondary">Updated</Typography><Typography component="dd" variant="caption" sx={{ m: 0, whiteSpace: 'nowrap' }}><TaskTimestamp value={operation.UpdatedAt} /></Typography></Box>
    </Box>
    <Button size="small" onClick={onOpen} aria-label={`View media operation ${operation.Id}`} sx={{ gridColumn: { xs: '1 / -1', lg: 'auto' }, justifySelf: 'end' }}>View operation</Button>
    {operation.CancelRequestedAt && mediaOperationIsActive(operation) && <Typography variant="body2" color="text.secondary" sx={{ gridColumn: '1 / -1' }}>Cancellation requested. Waiting for the operation to stop.</Typography>}
    {operation.ErrorMessage && <Typography variant="body2" color={operation.State === 'failed' ? 'error.main' : 'text.secondary'} sx={{ gridColumn: '1 / -1', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{operation.ErrorMessage}</Typography>}
  </Box>;
}

export function MediaOperationsPanel({ onNavigationGuardChange, onStatusChange }: { onNavigationGuardChange: UserNavigationGuardChange; onStatusChange?: (status: { message: string; active: boolean }) => void }) {
  const [query, setQuery] = useState<OperationQuery>({ kind: '', state: '', page: 0, limit: 25 });
  const [selected, setSelected] = useState<string>();
  const load = useCallback((signal: AbortSignal) => mediaOperationsApi.list({
    StartIndex: query.page * query.limit, Limit: query.limit,
    ...(query.kind ? { Kind: query.kind } : {}), ...(query.state ? { State: query.state } : {}),
  }, { signal }), [query]);
  const resource = useTaskResource({ key: `${query.kind}:${query.state}:${query.page}:${query.limit}`, load, poll: pollActiveOperations });
  const hasFilters = Boolean(query.kind || query.state);
  const hasActive = resource.data?.Items.some(mediaOperationIsActive) ?? false;
  const showToolbar = !resource.data || resource.data.TotalRecordCount > 0 || hasFilters;
  const statusMessage = resource.error != null ? 'Automatic updates stopped after a request error.' : !resource.data ? 'Loading media processing tasks...' : resource.paused ? 'Updates pause while this tab is hidden.' : hasActive ? 'Active operations update every 5 seconds.' : 'No active media processing tasks.';
  const statusActive = hasActive && !resource.paused && resource.error == null;

  useEffect(() => {
    onStatusChange?.({ message: statusMessage, active: statusActive });
  }, [onStatusChange, statusMessage, statusActive]);

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
    {showToolbar && <Stack direction={{ xs: 'column', md: 'row' }} sx={{ alignItems: { md: 'center' }, gap: 1.5, flexWrap: 'wrap' }}>
      <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ gap: 1.5, flex: 1, flexWrap: 'wrap', alignItems: { sm: 'center' } }}>
        <TextField select size="small" label="Operation kind" value={query.kind} onChange={(event) => filterKind(event.target.value)} slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }} sx={{ minWidth: { sm: 190 }, '& .MuiOutlinedInput-root': { borderRadius: '8px' } }}>
          <MenuItem value="">All kinds</MenuItem>{mediaOperationKinds.map((kind) => <MenuItem key={kind} value={kind}>{mediaOperationKindLabel(kind)}</MenuItem>)}
        </TextField>
        <TextField select size="small" label="Operation state" value={query.state} onChange={(event) => filterState(event.target.value)} slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }} sx={{ minWidth: { sm: 180 }, '& .MuiOutlinedInput-root': { borderRadius: '8px' } }}>
          <MenuItem value="">All states</MenuItem>{mediaOperationStates.map((state) => <MenuItem key={state} value={state}>{mediaOperationStateLabel(state)}</MenuItem>)}
        </TextField>
        <Button size="small" onClick={() => setQuery((current) => ({ ...current, kind: '', state: '', page: 0 }))} disabled={!hasFilters} sx={{ alignSelf: 'flex-start' }}>Clear filters</Button>
      </Stack>
      <Button size="small" startIcon={<RefreshRounded />} onClick={resource.reload} disabled={resource.loading} sx={{ alignSelf: { xs: 'flex-end', md: 'center' } }}>Refresh operations</Button>
    </Stack>}
    {resource.error != null && <ErrorNotice error={resource.error} retry={resource.reload} />}
    {!resource.data && resource.loading && <Stack role="status" aria-label="Loading media operations" spacing={2}><Skeleton variant="rounded" height={190} /><Skeleton variant="rounded" height={190} /></Stack>}
    {resource.data && <>
      {resource.loading && <LinearProgress aria-label="Refreshing media operations" />}
      {resource.data.Items.length === 0 ? <Paper component="section" variant="outlined" sx={{ position: 'relative', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 1.5, py: 8, px: 3, borderStyle: 'dashed', borderColor: colors.outlineVariant, borderRadius: '20px', textAlign: 'center' }}>
        {!showToolbar && <Tooltip title="Refresh operations"><span style={{ position: 'absolute', top: 12, right: 12 }}><IconButton aria-label="Refresh operations" onClick={resource.reload} disabled={resource.loading}><RefreshRounded fontSize="small" /></IconButton></span></Tooltip>}
        <Box sx={{ width: 56, height: 56, borderRadius: '50%', bgcolor: colors.surface, color: 'text.secondary', display: 'grid', placeItems: 'center' }}><VideoSettingsRounded aria-hidden="true" sx={{ fontSize: 28 }} /></Box>
        <Typography component="h3" variant="h3">{hasFilters ? 'No matching operations' : 'No media processing tasks'}</Typography>
        <Typography color="text.secondary" sx={{ maxWidth: 420, textWrap: 'pretty' }}>{hasFilters ? 'Choose another kind or state, or clear the filters.' : 'Open a movie or episode in a library to start subtitle processing. Task progress and results will appear here.'}</Typography>
      </Paper> : <Paper component="ul" variant="outlined" aria-label="Media processing operations" sx={{ p: 0, m: 0, listStyle: 'none', overflow: 'hidden' }}>
        {resource.data.Items.map((operation) => <OperationRow key={operation.Id} operation={operation} onOpen={() => setSelected(operation.Id)} />)}
      </Paper>}
      {resource.data.TotalRecordCount > 0 && <TablePagination component="div" count={resource.data.TotalRecordCount} page={Math.min(query.page, Math.max(0, Math.ceil(resource.data.TotalRecordCount / query.limit) - 1))} rowsPerPage={query.limit} rowsPerPageOptions={pageSizes} labelRowsPerPage="Operations per page" disabled={resource.loading}
        onPageChange={(_event, page) => setQuery((current) => ({ ...current, page }))}
        onRowsPerPageChange={(event) => { const limit = Number(event.target.value); if (pageSizes.includes(limit)) setQuery((current) => ({ ...current, limit, page: 0 })); }}
        sx={{ '& .MuiTablePagination-toolbar': { flexWrap: 'wrap', justifyContent: 'flex-end', px: { xs: 0, sm: 1 }, gap: 0.5 }, '& .MuiTablePagination-spacer': { display: { xs: 'none', sm: 'block' } }, '& .MuiTablePagination-actions': { ml: 1 } }} />}
    </>}
    {selected && <MediaOperationDialog key={selected} operationId={selected} onClose={() => { setSelected(undefined); resource.reload(); }} onChanged={resource.reload} onNavigationGuardChange={onNavigationGuardChange} />}
  </Stack>;
}
