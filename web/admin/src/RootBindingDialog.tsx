import { useEffect, useRef, useState } from 'react';
import { Accordion, AccordionDetails, AccordionSummary, Alert, Box, Button, Checkbox, Chip, CircularProgress, Dialog, DialogActions, DialogContent, DialogTitle, FormControlLabel, MenuItem, Paper, Skeleton, Stack, TextField, Typography } from '@mui/material';
import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import RefreshRounded from '@mui/icons-material/RefreshRounded';
import { ApiError, isAbortError } from './api';
import type { Library } from './api';
import { ErrorNotice } from './components';
import { rootBindingsApi } from './rootBindingsApi';
import type { RegisteredRoot, RootBinding, StorageIdentity } from './rootBindingsApi';

const mono = { fontFamily: 'ui-monospace, Consolas, monospace', overflowWrap: 'anywhere', whiteSpace: 'pre-wrap', minWidth: 0 } as const;
const acknowledgement = 'I understand that a later complete scan which verifies this storage may remove catalog records for missing files.';
const statusLabels: Record<RootBinding['Status'], string> = {
  verified: 'Verified', unbound: 'Unbound', mismatch: 'Storage changed', unavailable: 'Unavailable',
};
const statusMessages: Record<RootBinding['Status'], string> = {
  verified: 'The current storage matches the approved binding. Missing catalog records may be removed only after a later complete scan verifies this storage.',
  unbound: 'This root has no approved storage binding. Review the observed storage before binding it.',
  mismatch: 'The current storage differs from the approved binding. Review the changes before accepting this replacement.',
  unavailable: 'The server could not fully observe this root and its nested storage. Check mounts and access, then refresh the observation.',
};

function sameIdentity(before: StorageIdentity, after: StorageIdentity): boolean {
  return before.Profile === after.Profile && before.FilesystemUUID === after.FilesystemUUID && before.Digest === after.Digest;
}

function difference(before: StorageIdentity | undefined, after: StorageIdentity | undefined, observed: boolean): string {
  if (!observed) return 'Not observed';
  if (!before) return 'Added';
  if (!after) return 'Removed';
  return sameIdentity(before, after) ? 'Unchanged' : 'Changed';
}

function IdentityDetails({ identity, empty }: { identity?: StorageIdentity; empty: string }) {
  if (!identity) return <Typography variant="body2" color="text.secondary">{empty}</Typography>;
  return (
    <Box component="dl" sx={{ m: 0 }}>
      <Typography component="dt" variant="caption" color="text.secondary">Filesystem ID</Typography>
      <Typography component="dd" variant="body2" sx={{ ...mono, m: 0, mb: 1 }}>{identity.FilesystemUUID}</Typography>
      <Typography component="dt" variant="caption" color="text.secondary">Directory ID</Typography>
      <Typography component="dd" variant="body2" sx={{ ...mono, m: 0 }}>{identity.Digest}</Typography>
    </Box>
  );
}

function IdentityComparison({ before, after, observed }: { before?: StorageIdentity; after?: StorageIdentity; observed: boolean }) {
  return (
    <Box sx={{ display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
      <Box sx={{ minWidth: 0 }}>
        <Typography variant="body2" sx={{ fontWeight: 650, mb: 1 }}>Previously approved</Typography>
        <IdentityDetails identity={before} empty="No previous approval" />
      </Box>
      <Box sx={{ minWidth: 0 }}>
        <Typography variant="body2" sx={{ fontWeight: 650, mb: 1 }}>Current observation</Typography>
        <IdentityDetails identity={after} empty={observed ? 'Not present in this observation' : 'Unavailable'} />
      </Box>
    </Box>
  );
}

function TopologyComparison({ binding }: { binding: RootBinding }) {
  const observed = binding.Observed !== undefined;
  const approvedBoundaries = new Map(binding.Approved?.Boundaries.map((boundary) => [boundary.RelativePath, boundary.Identity]) ?? []);
  const observedBoundaries = new Map(binding.Observed?.Boundaries.map((boundary) => [boundary.RelativePath, boundary.Identity]) ?? []);
  const paths = [...new Set([...approvedBoundaries.keys(), ...observedBoundaries.keys()])].sort();
  return (
    <Stack spacing={2}>
      {(['Anchor', 'RegisteredRoot'] as const).map((key) => {
        const before = binding.Approved?.[key];
        const after = binding.Observed?.[key];
        const change = difference(before, after, observed);
        return (
          <Paper key={key} variant="outlined" sx={{ p: 2, minWidth: 0 }}>
            <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 1, mb: 2 }}>
              <Typography component="h3" variant="h4">{key === 'Anchor' ? 'Storage anchor identity' : 'Registered root identity'}</Typography>
              <Chip size="small" variant="outlined" color={change === 'Changed' ? 'warning' : 'default'} label={change === 'Added' ? 'Not previously bound' : change} />
            </Stack>
            <IdentityComparison before={before} after={after} observed={observed} />
          </Paper>
        );
      })}
      <Box>
        <Typography component="h3" variant="h4" sx={{ mb: 0.5 }}>Nested storage boundaries</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
          {binding.Approved?.Boundaries.length ?? 0} previously approved; {observed ? `${binding.Observed!.Boundaries.length} currently observed` : 'current storage unavailable'}.
          {' '}Paths below are relative to the registered root. Expand a boundary to compare its identities.
        </Typography>
        {paths.length === 0 && <Typography variant="body2" color="text.secondary">{observed ? 'No nested storage boundaries in this observation or previous approval.' : 'No previously approved nested storage boundaries are available to display.'}</Typography>}
        {paths.map((path) => {
          const before = approvedBoundaries.get(path);
          const after = observedBoundaries.get(path);
          const change = difference(before, after, observed);
          return (
            <Accordion key={path} disableGutters defaultExpanded={change !== 'Unchanged'} sx={{ '&:before': { display: 'none' }, border: 1, borderColor: 'divider', boxShadow: 'none', mb: 1, minWidth: 0 }}>
              <AccordionSummary expandIcon={<ExpandMoreRounded />} sx={{ '& .MuiAccordionSummary-content': { minWidth: 0, alignItems: 'center', gap: 1.5 }, '& .MuiAccordionSummary-expandIconWrapper': { flexShrink: 0 } }}>
                <Typography variant="body2" sx={{ ...mono, flex: 1 }}>{path}</Typography>
                <Chip size="small" variant="outlined" label={change} color={['Added', 'Removed', 'Changed'].includes(change) ? 'warning' : 'default'} sx={{ flexShrink: 0 }} />
              </AccordionSummary>
              <AccordionDetails><IdentityComparison before={before} after={after} observed={observed} /></AccordionDetails>
            </Accordion>
          );
        })}
      </Box>
    </Stack>
  );
}

function SelectedRootValue({ root }: { root: RegisteredRoot }) {
  return (
    <Box id="root-binding-selected-value" sx={{ minWidth: 0, maxWidth: '100%', overflow: 'hidden' }}>
      <Typography component="span" variant="body2" title={root.Path} sx={{ ...mono, display: '-webkit-box', WebkitBoxOrient: 'vertical', WebkitLineClamp: 2, overflow: 'hidden', whiteSpace: 'normal' }}>{root.Path}</Typography>
      <Typography component="span" variant="caption" color="text.secondary" title={root.Id} sx={{ ...mono, display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>Root ID: {root.Id}</Typography>
    </Box>
  );
}

function BindingFacts({ root, binding }: { root: RegisteredRoot; binding?: RootBinding }) {
  return (
    <Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'max-content minmax(0, 1fr)' }, columnGap: 2, rowGap: 0.75 }}>
      {([['Registered root path', root.Path], ['Storage anchor path', root.AllowedPath], ['Root ID', root.Id], ['Binding revision', root.Revision]] as const).map(([label, value]) => (
        <Box key={label} sx={{ display: 'contents' }}>
          <Typography component="dt" variant="body2" color="text.secondary">{label}</Typography>
          <Typography component="dd" variant="body2" sx={{ ...mono, m: 0, mb: { xs: 1, sm: 0 } }}>{value}</Typography>
        </Box>
      ))}
      {binding?.BoundAt && <>
        <Typography component="dt" variant="body2" color="text.secondary">Last approved</Typography>
        <Typography component="dd" variant="body2" sx={{ m: 0, overflowWrap: 'anywhere' }}><time dateTime={binding.BoundAt} title={binding.BoundAt}>{new Date(binding.BoundAt).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'long' })}</time></Typography>
        <Typography component="dt" variant="body2" color="text.secondary">Last approved by</Typography>
        <Typography component="dd" variant="body2" sx={{ ...mono, m: 0 }}>{binding.BoundBy}</Typography>
      </>}
    </Box>
  );
}

function mutationRecovery(error: unknown): string {
  if (error instanceof ApiError && error.code === 'scan_busy') return 'A scan is active for this library. Wait for it to finish, then refresh the observation before approving storage.';
  if (error instanceof ApiError && error.status === 409) return 'The registered root or storage changed after this observation. Refresh the observation and review the differences before approving again.';
  if (!(error instanceof ApiError) || ['network_error', 'invalid_response'].includes(error.code) || error.status >= 500) return 'The result could not be confirmed. The binding may have been saved. Refresh the observation before trying again.';
  return 'Refresh the observation and review the current storage before approving again.';
}

export function RootBindingDialog({ library, onClose }: { library: Library; onClose: () => void }) {
  const [roots, setRoots] = useState<RegisteredRoot[]>();
  const [rootId, setRootId] = useState('');
  const [rootsVersion, setRootsVersion] = useState(0);
  const [observationVersion, setObservationVersion] = useState(0);
  const [rootsError, setRootsError] = useState<unknown>(null);
  const [error, setError] = useState<unknown>(null);
  const [binding, setBinding] = useState<RootBinding>();
  const [observing, setObserving] = useState(false);
  const [saving, setSaving] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);
  const [refreshRequired, setRefreshRequired] = useState(false);
  const [saved, setSaved] = useState(false);
  const mounted = useRef(true);
  const mutation = useRef<AbortController | undefined>(undefined);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; mutation.current?.abort(); };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setRoots(undefined);
    setRootId('');
    setRootsError(null);
    void rootBindingsApi.listRoots(library.Id, { signal: controller.signal }).then((result) => {
      if (!active) return;
      setRoots(result.Items);
      setRootId(result.Items[0]?.Id ?? '');
    }).catch((cause: unknown) => { if (active && !isAbortError(cause)) setRootsError(cause); });
    return () => { active = false; controller.abort(); };
  }, [library.Id, rootsVersion]);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setBinding(undefined);
    setAcknowledged(false);
    setRefreshRequired(false);
    setError(null);
    setSaved(false);
    setObserving(Boolean(rootId));
    if (rootId) {
      void rootBindingsApi.getBinding(library.Id, rootId, { signal: controller.signal }).then((result) => {
        if (active) setBinding(result);
      }).catch((cause: unknown) => { if (active && !isAbortError(cause)) setError(cause); })
        .finally(() => { if (active) setObserving(false); });
    }
    return () => { active = false; controller.abort(); };
  }, [library.Id, rootId, observationVersion]);

  const selectedRoot = roots?.find((root) => root.Id === rootId);
  const currentBinding = binding?.Id === rootId ? binding : undefined;
  const canApprove = Boolean(currentBinding && ['unbound', 'mismatch'].includes(currentBinding.Status)
    && currentBinding.Observed && currentBinding.ObservedFingerprint && acknowledged && !observing && !saving && !refreshRequired);

  function selectRoot(id: string) {
    if (saving || mutation.current) return;
    setBinding(undefined);
    setAcknowledged(false);
    setRootId(id);
  }

  function refreshObservation() {
    if (!rootId || saving || observing || mutation.current) return;
    setBinding(undefined);
    setAcknowledged(false);
    setObserving(true);
    setObservationVersion((value) => value + 1);
  }

  async function approve() {
    if (!canApprove || !currentBinding?.ObservedFingerprint || mutation.current) return;
    const controller = new AbortController();
    mutation.current = controller;
    setSaving(true);
    setAcknowledged(false);
    setError(null);
    setSaved(false);
    try {
      const result = await rootBindingsApi.updateBinding(library.Id, currentBinding.Id, {
        Revision: currentBinding.Revision, ObservedFingerprint: currentBinding.ObservedFingerprint, AcknowledgeMissingRemoval: true,
      }, { signal: controller.signal });
      if (mounted.current) { setBinding(result); setSaved(true); }
    } catch (cause) {
      if (mounted.current && !isAbortError(cause)) { setError(cause); setRefreshRequired(true); }
    } finally {
      mutation.current = undefined;
      if (mounted.current) setSaving(false);
    }
  }

  return (
    <Dialog open fullWidth maxWidth="md" onClose={saving ? undefined : onClose} aria-labelledby="root-binding-title" aria-describedby="root-binding-description">
      <DialogTitle id="root-binding-title">Storage bindings</DialogTitle>
      <DialogContent aria-busy={observing || saving} sx={{ minWidth: 0 }}>
        <Typography sx={{ fontWeight: 650, overflowWrap: 'anywhere', mb: 1 }}>{library.Name}</Typography>
        <Typography id="root-binding-description" variant="body2" color="text.secondary" sx={{ mb: 3 }}>Review which physical storage belongs to each registered directory before approving a binding.</Typography>
        <Stack spacing={2.5}>
          {rootsError != null && <ErrorNotice error={rootsError} retry={() => setRootsVersion((value) => value + 1)} />}
          {!roots && rootsError == null && <Skeleton variant="rounded" height={60} aria-label="Loading registered roots" />}
          {roots?.length === 0 && <Alert severity="info">This library has no registered roots.</Alert>}
          {roots && roots.length > 0 && <TextField id="root-binding-root" select fullWidth label="Registered root" value={rootId} onChange={(event) => selectRoot(event.target.value)} disabled={saving} helperText="Each registration is identified by its root ID." slotProps={{ select: { 'aria-describedby': `root-binding-root-helper-text${selectedRoot ? ' root-binding-selected-value' : ''}`, renderValue: () => selectedRoot ? <SelectedRootValue root={selectedRoot} /> : null, MenuProps: { slotProps: { paper: { sx: { maxWidth: 'calc(100vw - 32px)' } } } } } }} sx={{ '& .MuiSelect-select': { minWidth: 0, overflow: 'hidden', whiteSpace: 'normal' } }}>
            {roots.map((root) => <MenuItem key={root.Id} value={root.Id} sx={{ whiteSpace: 'normal', overflowWrap: 'anywhere', minWidth: 0 }}><Box sx={{ minWidth: 0 }}><Typography variant="body2" sx={mono}>{root.Path}</Typography><Typography variant="caption" color="text.secondary" sx={mono}>Root ID: {root.Id}</Typography></Box></MenuItem>)}
          </TextField>}
          {currentBinding && <>
            {saved && <Alert severity="success" role="status">Storage binding saved.</Alert>}
            <Alert severity={currentBinding.Status === 'verified' ? 'success' : currentBinding.Status === 'unbound' ? 'info' : 'warning'}>
              <Typography component="span" sx={{ display: 'block', fontWeight: 650 }}>{statusLabels[currentBinding.Status]}</Typography>
              {statusMessages[currentBinding.Status]}
            </Alert>
          </>}
          {selectedRoot && <Box><Button startIcon={observing ? <CircularProgress size={16} color="inherit" /> : <RefreshRounded />} onClick={refreshObservation} disabled={observing || saving}>{observing ? 'Observing storage...' : 'Refresh observation'}</Button></Box>}
          {error != null && <ErrorNotice error={error} />}
          {refreshRequired && <Alert severity="warning">{mutationRecovery(error)}</Alert>}
          {observing && <Skeleton variant="rounded" height={120} aria-label="Observing storage binding" />}
          {selectedRoot && <Paper variant="outlined" sx={{ p: 2, bgcolor: 'background.default' }}><BindingFacts root={currentBinding ?? selectedRoot} binding={currentBinding} /></Paper>}
          {currentBinding && <>
            <TopologyComparison key={`${currentBinding.Id}:${currentBinding.Revision}:${currentBinding.ObservedFingerprint ?? 'unavailable'}`} binding={currentBinding} />
            {['unbound', 'mismatch'].includes(currentBinding.Status) && <Paper variant="outlined" sx={{ p: 2, bgcolor: 'background.default' }}>
              <FormControlLabel control={<Checkbox checked={acknowledged} onChange={(event) => setAcknowledged(event.target.checked)} disabled={saving || observing || refreshRequired} />} label={acknowledgement} sx={{ alignItems: 'flex-start', m: 0, '& .MuiCheckbox-root': { pt: 0, pl: 0 }, '& .MuiFormControlLabel-label': { fontSize: 14 } }} />
            </Paper>}
          </>}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5, flexWrap: 'wrap', gap: 1 }}>
        <Button color="secondary" onClick={onClose} disabled={saving}>Close</Button>
        <Button variant="contained" onClick={() => void approve()} disabled={!canApprove} startIcon={saving ? <CircularProgress size={16} color="inherit" /> : undefined}>{saving ? 'Saving...' : currentBinding?.Approved ? 'Accept replacement' : 'Bind storage'}</Button>
      </DialogActions>
    </Dialog>
  );
}
