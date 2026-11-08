import { cloneElement, isValidElement, useState } from 'react';
import type { ReactNode } from 'react';
import { Box, Button, Chip, Stack, TextField, Typography } from './ui';
import { CompareArrowsRounded } from './icons';
import { InfoOutlined, LockOutlined, LockResetRounded } from './icons';
import { useTranslation } from './i18n';
import './media-editor.css';
import { RestartAltRounded } from './icons';

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
      <Typography component="dd" variant="body2" sx={{ m: 0, mt: 0.5, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', maxHeight: 160, overflow: 'auto' }}>{value ? <span translate="no">{value}</span> : 'No value'}</Typography>
    </Box>
  );
}

// The caller owns typed values, revision state, and persistence. This frame
// presents source ownership without interpreting an HTTP or storage contract.
export function MetadataField({ id, label, state, locked, canRestore, disabled = false, readOnlyReason, automaticSummary, lockedSummary, pendingLock = false, error, children, onLockChange, onUseAutomatic }: MetadataFieldProps) {
  const { t } = useTranslation();
  const [comparing, setComparing] = useState(false);
  const readOnly = Boolean(readOnlyReason);
  const stateLabel = state === 'locked' && pendingLock ? 'Lock on save' : stateLabels[state];
  const sourceHelp = readOnlyReason ?? (state === 'manual'
    ? locked ? 'Your manual value stays in effect. Unlocking keeps the manual override.' : 'Your value stays in effect until you choose Use automatic.'
    : pendingLock ? 'This value will be locked when you save.'
      : locked ? 'The saved value stays fixed when the library is scanned.' : 'This value follows the information found during library scans.');
  const directField = isValidElement<{ id?: string; label?: ReactNode; helperText?: ReactNode; required?: boolean; slotProps?: { htmlInput?: Record<string, unknown>; [key: string]: unknown } }>(children) && children.type === TextField ? children : undefined;
  const controlHelp = directField?.props.helperText;
  const required = directField?.props.required;
  const inputId = directField?.props.id;
  const control = directField ? cloneElement(directField, {
    label: undefined,
    // General guidance stays in the source disclosure. The field error below
    // remains visible and is linked from the input even when guidance is closed.
    helperText: undefined,
    slotProps: {
      ...directField.props.slotProps,
      htmlInput: {
        ...directField.props.slotProps?.htmlInput,
        'aria-describedby': [directField.props.slotProps?.htmlInput?.['aria-describedby'], id + '-help', id + '-state'].filter(Boolean).join(' '),
      },
    },
  }) : children;

  return <section className={'metadata-field metadata-field-' + state} aria-labelledby={id + '-heading'}>
    <div className="metadata-field-heading">
      {inputId ? <label id={id + '-heading'} className="metadata-field-label" htmlFor={inputId}>{t(label)}{required && <span aria-hidden="true"> *</span>}</label>
        : <span id={id + '-heading'} className="metadata-field-label">{t(label)}</span>}
      <span id={id + '-state'} className={'metadata-field-state metadata-field-state-' + state} title={t(stateLabel)}><span className="visually-hidden">{t(stateLabel)}</span></span>
      <label className="metadata-lock-control" title={t('Lock ' + label)}>
        <input id={id + '-lock'} type="checkbox" checked={locked} disabled={disabled || readOnly} onChange={(event) => onLockChange(event.target.checked)} aria-label={t('Lock ' + label)} aria-describedby={id + '-help ' + id + '-state'} />
        {locked ? <LockOutlined aria-hidden="true" /> : <LockResetRounded aria-hidden="true" />}
      </label>
      <button type="button" className="metadata-source-toggle" onClick={() => setComparing((value) => !value)} aria-label={t('Compare source')} aria-expanded={comparing} aria-controls={id + '-comparison'} title={t('Compare source')}><CompareArrowsRounded aria-hidden="true" /></button>
    </div>
    <div className="metadata-field-control">{control}</div>
    <p id={id + '-help'} className={error ? 'metadata-field-error' : 'visually-hidden'} aria-live={error ? 'polite' : undefined}>{error ? <span translate="no">{error}</span> : t(sourceHelp)}</p>
    <div id={id + '-comparison'} className="metadata-field-comparison" hidden={!comparing}>
      <div className="metadata-field-source-heading"><strong>{t(stateLabel)}</strong><span>{t(locked ? 'Locked' : 'Unlocked')}</span></div>
      <p className="metadata-field-source-note">{t(sourceHelp)}</p>
      {controlHelp && <p className="metadata-field-source-note">{typeof controlHelp === 'string' ? controlHelp === error ? <span translate="no">{controlHelp}</span> : t(controlHelp) : controlHelp}</p>}
      <dl className={'metadata-field-source-values' + (lockedSummary !== undefined ? ' metadata-field-source-pair' : '')}>
        <SourceValue label="Automatic value" value={automaticSummary} />
        {lockedSummary !== undefined && <SourceValue label="Locked value" value={lockedSummary} />}
      </dl>
      <Button type="button" size="small" color="secondary" startIcon={<RestartAltRounded />} disabled={disabled || readOnly || !canRestore} onClick={onUseAutomatic} aria-label={'Use automatic ' + label}>Use automatic</Button>
    </div>
  </section>;
}

export function MetadataSection({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  const { t } = useTranslation();
  return <section className="metadata-section" aria-label={t(title)}>
    <div className="metadata-section-heading">
      <h3>{t(title)}</h3>
      {description && <details className="metadata-section-guidance"><summary aria-label={t('Details')} title={t('Details')}><InfoOutlined aria-hidden="true" /></summary><p>{t(description)}</p></details>}
    </div>
    <div className="metadata-section-fields">{children}</div>
  </section>;
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
        {error ? <span translate="no">{error}</span> : (removed ? 'This saved override and lock will be removed when you save.' : 'This saved field is retained but does not affect this item. It can be removed, but cannot be edited or locked.')}
      </Typography>
      <Button type="button" size="small" color="secondary" startIcon={<RestartAltRounded />} disabled={disabled || removed} onClick={onUseAutomatic} aria-label={`Use automatic ${label}`} aria-describedby={`${id}-help`} sx={{ mt: 0.5, px: 1 }}>
        Use automatic
      </Button>
    </Box>
  );
}
