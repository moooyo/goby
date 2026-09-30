import type { ReactNode } from 'react';
import { Box, IconButton, InputAdornment, TextField } from '@mui/material';
import SearchRounded from '@mui/icons-material/SearchRounded';
import { colors } from './theme';

export const mediaSurface = colors.surface;
export const mediaSelected = colors.secondaryContainer;
export const mediaPanelSx = { borderRadius: '20px', overflow: 'hidden' } as const;
export const mediaSelectSx = { minWidth: 170, '& .MuiOutlinedInput-root': { height: 40, borderRadius: '8px', '& fieldset': { borderColor: colors.outlineVariant } }, '& .MuiSelect-select': { py: 1, fontSize: 13 } } as const;

export function MediaIconTile({ children, size = 44, neutral = false }: { children: ReactNode; size?: number; neutral?: boolean }) {
  return <Box aria-hidden="true" sx={{ display: 'grid', placeItems: 'center', width: size, height: size, flexShrink: 0, borderRadius: neutral ? '50%' : '14px', bgcolor: neutral ? mediaSurface : colors.iconContainer, color: neutral ? 'text.secondary' : colors.onSecondaryContainer, '& .MuiSvgIcon-root': { fontSize: size > 40 ? 24 : 20 } }}>{children}</Box>;
}

export function MediaSearchField({ label, value, onChange, disabled = false, maxLength }: { label: string; value: string; onChange: (value: string) => void; disabled?: boolean; maxLength?: number }) {
  return <TextField fullWidth size="small" placeholder={label} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} slotProps={{ htmlInput: { 'aria-label': label, autoComplete: 'off', maxLength }, input: { startAdornment: <InputAdornment position="start"><IconButton type="submit" size="small" aria-label={label} disabled={disabled} sx={{ ml: -0.75 }}><SearchRounded sx={{ fontSize: 19 }} /></IconButton></InputAdornment> } }} sx={{ '& .MuiOutlinedInput-root': { height: 40, bgcolor: colors.container, borderRadius: '24px', fontSize: 13, '& fieldset': { borderColor: 'transparent' }, '&:hover fieldset': { borderColor: 'transparent' }, '&.Mui-focused fieldset': { borderColor: 'primary.main' } } }} />;
}
