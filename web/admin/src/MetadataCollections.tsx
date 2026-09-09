import { Box, Button, IconButton, MenuItem, Stack, TextField, Typography } from '@mui/material';
import AddRounded from '@mui/icons-material/AddRounded';
import DeleteOutlineRounded from '@mui/icons-material/DeleteOutlineRounded';
import { metadataCreditTypes } from './metadataCreditTypes';

let nextEntryId = 0;

function nextId(): string {
  nextEntryId += 1;
  return `draft-${nextEntryId}`;
}

export interface StringEntry {
  id: string;
  value: string;
}

export interface PersonEntry {
  id: string;
  Name: string;
  Role: string;
  Type: string;
  SortOrder: string;
}

export interface ProviderEntry {
  id: string;
  Key: string;
  Value: string;
}

interface CollectionProps<T> {
  value: T[];
  disabled: boolean;
  onChange: (value: T[]) => void;
  errorFor?: (path: string) => string | undefined;
}

interface StringListProps extends CollectionProps<StringEntry> {
  id: string;
  label: string;
  singular: string;
}

function CollectionActions({ label, singular, empty, disabled, onAdd, onClear }: { label: string; singular: string; empty: boolean; disabled: boolean; onAdd: () => void; onClear: () => void }) {
  return (
    <Stack direction="row" sx={{ flexWrap: 'wrap', gap: 1 }}>
      <Button type="button" size="small" color="secondary" startIcon={<AddRounded />} onClick={onAdd} disabled={disabled}>Add {singular.toLowerCase()}</Button>
      <Button type="button" size="small" color="secondary" onClick={onClear} disabled={disabled || empty} aria-label={`Clear all ${label.toLowerCase()}`}>Clear all</Button>
    </Stack>
  );
}

export function MetadataStringList({ id, label, singular, value, disabled, onChange, errorFor }: StringListProps) {
  return (
    <Stack spacing={1.5} role="group" aria-label={label}>
      {value.length === 0 && <Typography variant="body2" color="text.secondary">No {label.toLowerCase()} added.</Typography>}
      {value.map((entry, index) => {
        const error = errorFor?.(`${index}`);
        return (
          <Stack key={entry.id} direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
            <TextField
              id={`${id}-${entry.id}`}
              fullWidth
              label={`${singular} ${index + 1}`}
              value={entry.value}
              disabled={disabled}
              onChange={(event) => onChange(value.map((current) => current.id === entry.id ? { ...current, value: event.target.value } : current))}
              error={Boolean(error)}
              helperText={error}
            />
            <IconButton type="button" color="secondary" disabled={disabled} aria-label={`Remove ${singular.toLowerCase()} ${index + 1}`} onClick={() => onChange(value.filter((current) => current.id !== entry.id))} sx={{ mt: 1, flexShrink: 0 }}><DeleteOutlineRounded /></IconButton>
          </Stack>
        );
      })}
      <CollectionActions label={label} singular={singular} empty={value.length === 0} disabled={disabled} onAdd={() => onChange([...value, { id: nextId(), value: '' }])} onClear={() => onChange([])} />
    </Stack>
  );
}

export function MetadataPeopleList({ value, disabled, onChange, errorFor }: CollectionProps<PersonEntry>) {
  function change(id: string, key: keyof Omit<PersonEntry, 'id'>, next: string) {
    onChange(value.map((entry) => entry.id === id ? { ...entry, [key]: next } : entry));
  }

  return (
    <Stack spacing={1.5} role="group" aria-label="People">
      {value.length === 0 && <Typography variant="body2" color="text.secondary">No people added.</Typography>}
      {value.map((entry, index) => {
        const nameError = errorFor?.(`${index}.Name`);
        const roleError = errorFor?.(`${index}.Role`);
        const typeError = errorFor?.(`${index}.Type`);
        const orderError = errorFor?.(`${index}.SortOrder`);
        const unknownType = Boolean(entry.Type) && !metadataCreditTypes.includes(entry.Type);
        return (
          <Box key={entry.id} component="section" aria-labelledby={`metadata-person-${entry.id}-heading`} sx={{ minWidth: 0, p: 2, border: 1, borderColor: 'divider', borderRadius: 2 }}>
            <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, mb: 1.5 }}>
              <Typography id={`metadata-person-${entry.id}-heading`} variant="body2" sx={{ fontWeight: 650 }}>Person {index + 1}</Typography>
              <IconButton type="button" size="small" color="secondary" disabled={disabled} aria-label={`Remove person ${index + 1}`} onClick={() => onChange(value.filter((current) => current.id !== entry.id))}><DeleteOutlineRounded fontSize="small" /></IconButton>
            </Stack>
            <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
              <TextField id={`metadata-person-${entry.id}-name`} required fullWidth label="Name" value={entry.Name} disabled={disabled} onChange={(event) => change(entry.id, 'Name', event.target.value)} error={Boolean(nameError)} helperText={nameError} />
              <TextField id={`metadata-person-${entry.id}-role`} fullWidth label="Role" value={entry.Role} disabled={disabled} onChange={(event) => change(entry.id, 'Role', event.target.value)} error={Boolean(roleError)} helperText={roleError} />
              <TextField id={`metadata-person-${entry.id}-type`} select required fullWidth label="Type" value={entry.Type} disabled={disabled} onChange={(event) => change(entry.id, 'Type', event.target.value)} error={Boolean(typeError)} helperText={typeError ?? (unknownType ? 'Current type. Choose a listed type when editing this credit.' : 'Choose the credit type.')}>
                <MenuItem value="" disabled>Select a type</MenuItem>
                {unknownType && <MenuItem value={entry.Type} disabled>{entry.Type}</MenuItem>}
                {metadataCreditTypes.map((type) => <MenuItem key={type} value={type}>{type}</MenuItem>)}
              </TextField>
              <TextField id={`metadata-person-${entry.id}-sort-order`} fullWidth label="Sort order" type="text" value={entry.SortOrder} disabled={disabled} onChange={(event) => change(entry.id, 'SortOrder', event.target.value)} error={Boolean(orderError)} helperText={orderError ?? 'Leave blank for no explicit sort order.'} slotProps={{ htmlInput: { inputMode: 'numeric' } }} />
            </Box>
          </Box>
        );
      })}
      <CollectionActions label="people" singular="person" empty={value.length === 0} disabled={disabled} onAdd={() => onChange([...value, { id: nextId(), Name: '', Role: '', Type: '', SortOrder: '' }])} onClear={() => onChange([])} />
    </Stack>
  );
}

export function MetadataProviderList({ value, disabled, onChange, errorFor }: CollectionProps<ProviderEntry>) {
  function change(id: string, key: keyof Omit<ProviderEntry, 'id'>, next: string) {
    onChange(value.map((entry) => entry.id === id ? { ...entry, [key]: next } : entry));
  }

  return (
    <Stack spacing={1.5} role="group" aria-label="Provider identifiers">
      {value.length === 0 && <Typography variant="body2" color="text.secondary">No provider identifiers added.</Typography>}
      {value.map((entry, index) => {
        const keyError = errorFor?.(`${index}.Key`);
        const valueError = errorFor?.(`${index}.Value`);
        return (
          <Box key={entry.id} component="section" aria-labelledby={`metadata-provider-${entry.id}-heading`} sx={{ minWidth: 0, p: 2, border: 1, borderColor: 'divider', borderRadius: 2 }}>
            <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, mb: 1.5 }}>
              <Typography id={`metadata-provider-${entry.id}-heading`} variant="body2" sx={{ fontWeight: 650 }}>Provider identifier {index + 1}</Typography>
              <IconButton type="button" size="small" color="secondary" disabled={disabled} aria-label={`Remove provider identifier ${index + 1}`} onClick={() => onChange(value.filter((current) => current.id !== entry.id))}><DeleteOutlineRounded fontSize="small" /></IconButton>
            </Stack>
            <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2 }}>
              <TextField id={`metadata-provider-${entry.id}-key`} fullWidth label="Provider" value={entry.Key} disabled={disabled} onChange={(event) => change(entry.id, 'Key', event.target.value)} error={Boolean(keyError)} helperText={keyError} />
              <TextField id={`metadata-provider-${entry.id}-value`} fullWidth label="Identifier" value={entry.Value} disabled={disabled} onChange={(event) => change(entry.id, 'Value', event.target.value)} error={Boolean(valueError)} helperText={valueError} />
            </Box>
          </Box>
        );
      })}
      <CollectionActions label="provider identifiers" singular="provider identifier" empty={value.length === 0} disabled={disabled} onAdd={() => onChange([...value, { id: nextId(), Key: '', Value: '' }])} onClear={() => onChange([])} />
    </Stack>
  );
}
