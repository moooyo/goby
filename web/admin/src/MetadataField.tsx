import { useState } from 'react';
import type { ReactNode } from 'react';
import { Box, Button, Checkbox, Chip, Collapse, FormControlLabel, Stack, Typography } from '@mui/material';
import CompareArrowsRounded from '@mui/icons-material/CompareArrowsRounded';
import KeyboardArrowDownRounded from '@mui/icons-material/KeyboardArrowDownRounded';
import RestartAltRounded from '@mui/icons-material/RestartAltRounded';

export type MetadataFieldState = 'automatic' | 'locked' | 'manual';

const stateLabels: Record<MetadataFieldState, string> = {
  automatic: 'Automatic',
  locked: 'Locked automatic',
  manual: 'Manual override',
};

interface MetadataFieldProps {
  id: string;
  label: string;
  state: MetadataFieldState;
  locked: boolean;
  canRestore: boolean;
  disabled?: boolean;
  readOnlyReason?: string;
  automaticSummary: string;
  lockedSummary?: string;
  pendingLock?: boolean;
  error?: string;
  children: ReactNode;
  onLockChange: (locked: boolean) => void;
  onUseAutomatic: () => void;
}

function SourceValue({ label, value }: { label: string; value: string }) {
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography component="dt" variant="caption" color="text.secondary">{label}</Typography>
      <Typography component="dd" variant="body2" sx={{ m: 0, mt: 0.5, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: 160, overflow: 'auto' }}>{value || 'No value'}</Typography>
    </Box>
  );
}

// The caller owns typed values, revision state, and persistence. This frame
// presents source ownership without interpreting an HTTP or storage contract.
export function MetadataField({ id, label, state, locked, canRestore, disabled = false, readOnlyReason, automaticSummary, lockedSummary, pendingLock = false, error, children, onLockChange, onUseAutomatic }: MetadataFieldProps) {
  const [comparing, setComparing] = useState(false);
  const readOnly = Boolean(readOnlyReason);
  return (
    <Box component="section" aria-labelledby={`${id}-heading`} sx={{ minWidth: 0 }}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', columnGap: 2, rowGap: 0.5, mb: 1 }}>
        <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1 }}>
          <Typography id={`${id}-heading`} variant="body2" sx={{ fontWeight: 650 }}>{label}</Typography>
          <Chip size="small" variant="outlined" color={state === 'automatic' ? 'default' : 'primary'} label={state === 'locked' && pendingLock ? 'Lock on save' : stateLabels[state]} />
        </Stack>
        <FormControlLabel sx={{ m: 0, gap: 0.25 }} control={<Checkbox size="small" checked={locked} disabled={disabled || readOnly} onChange={(event) => onLockChange(event.target.checked)} slotProps={{ input: { 'aria-label': `Lock ${label}`, 'aria-describedby': `${id}-help` } }} />} label={<Typography variant="caption">Lock</Typography>} />
      </Stack>
      {children}
      <Typography id={`${id}-help`} variant="caption" color={error ? 'error.main' : 'text.secondary'} sx={{ display: 'block', mt: 0.75 }}>
        {error ?? readOnlyReason ?? (state === 'manual' ? locked ? 'Your manual value stays in effect. Unlocking keeps the manual override.' : 'Your value stays in effect until you choose Use automatic.' : pendingLock ? 'This value will be locked when you save.' : locked ? 'The saved value stays fixed when the library is scanned.' : 'This value follows the information found during library scans.')}
      </Typography>
      <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', columnGap: 1, rowGap: 0.5, mt: 0.5 }}>
        <Button type="button" size="small" color="secondary" onClick={() => setComparing((value) => !value)} aria-expanded={comparing} aria-controls={`${id}-comparison`} startIcon={<CompareArrowsRounded />} endIcon={<KeyboardArrowDownRounded sx={{ transform: comparing ? 'rotate(180deg)' : undefined }} />} sx={{ minHeight: 32, px: 1 }}>
          Compare source
        </Button>
        <Button type="button" size="small" color="secondary" startIcon={<RestartAltRounded />} disabled={disabled || readOnly || !canRestore} onClick={onUseAutomatic} aria-label={`Use automatic ${label}`} sx={{ minHeight: 32, px: 1 }}>
          Use automatic
        </Button>
      </Stack>
      <Collapse in={comparing}>
        <Box id={`${id}-comparison`} component="dl" sx={{ m: 0, mt: 1, p: 2, border: 1, borderColor: 'divider', borderRadius: 2, bgcolor: 'background.default', display: 'grid', gridTemplateColumns: lockedSummary === undefined ? '1fr' : { xs: '1fr', sm: '1fr 1fr' }, gap: 2 }}>
          <SourceValue label="Automatic value" value={automaticSummary} />
          {lockedSummary !== undefined && <SourceValue label="Locked value" value={lockedSummary} />}
        </Box>
      </Collapse>
    </Box>
  );
}

export function MetadataSection({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <Box component="section" aria-label={title}>
      <Typography variant="h4" component="h3">{title}</Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, mb: 2.5 }}>{description}</Typography>
      <Stack spacing={3}>{children}</Stack>
    </Box>
  );
}

export function InactiveMetadataField({ id, label, overrideSummary, lockedSummary, removed, disabled, error, onUseAutomatic }: { id: string; label: string; overrideSummary?: string; lockedSummary?: string; removed: boolean; disabled: boolean; error?: string; onUseAutomatic: () => void }) {
  return (
    <Box component="section" aria-labelledby={`${id}-heading`} sx={{ minWidth: 0, p: 2, border: 1, borderColor: 'divider', borderRadius: 2, bgcolor: 'background.default' }}>
      <Stack direction="row" sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1 }}>
        <Typography id={`${id}-heading`} variant="body2" sx={{ fontWeight: 650 }}>{label}</Typography>
        <Chip size="small" variant="outlined" label="Not applied to this item type" />
      </Stack>
      <Box component="dl" sx={{ m: 0, display: 'grid', gridTemplateColumns: overrideSummary !== undefined && lockedSummary !== undefined ? { xs: '1fr', sm: '1fr 1fr' } : '1fr', gap: 2 }}>
        {overrideSummary !== undefined && <SourceValue label="Saved manual override" value={overrideSummary} />}
        {lockedSummary !== undefined && <SourceValue label="Saved locked value" value={lockedSummary} />}
      </Box>
      <Typography id={`${id}-help`} variant="caption" color={error ? 'error.main' : 'text.secondary'} sx={{ display: 'block', mt: 1.5 }}>
        {error ?? (removed ? 'This saved override and lock will be removed when you save.' : 'This saved field is retained but does not affect this item. It can be removed, but cannot be edited or locked.')}
      </Typography>
      <Button type="button" size="small" color="secondary" startIcon={<RestartAltRounded />} disabled={disabled || removed} onClick={onUseAutomatic} aria-label={`Use automatic ${label}`} aria-describedby={`${id}-help`} sx={{ mt: 0.5, px: 1 }}>
        Use automatic
      </Button>
    </Box>
  );
}
