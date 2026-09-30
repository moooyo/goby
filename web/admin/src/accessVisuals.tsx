import type { ReactNode } from 'react';
import { Box, Chip, Stack, Typography } from '@mui/material';
import type { ChipProps } from '@mui/material';

const tones = {
  success: { background: '#CDEBD6', foreground: '#0D4F2B' },
  warning: { background: '#FDE3B8', foreground: '#5C3C00' },
  error: { background: '#FFDAD6', foreground: '#8C1D18' },
  info: { background: '#D8E4FA', foreground: '#0F2A57' },
  neutral: { background: '#E4E8F0', foreground: '#434753' },
};

export function AccessBadge({ tone = 'neutral', ...props }: Omit<ChipProps, 'color'> & { tone?: keyof typeof tones }) {
  const color = tones[tone];
  return <Chip {...props} size="small" sx={{ height: 23, borderRadius: '6px', bgcolor: color.background, color: color.foreground, fontSize: 11, fontWeight: 600, '& .MuiChip-label': { px: 0.9 }, '& .MuiChip-icon': { color: 'inherit', fontSize: 14 }, ...props.sx }} />;
}

export function AccessIconTile({ children }: { children: ReactNode }) {
  return <Box sx={{ width: 40, height: 40, borderRadius: '12px', display: 'grid', placeItems: 'center', flexShrink: 0, bgcolor: '#F3F6FB', color: 'text.secondary', '& .MuiSvgIcon-root': { fontSize: 21 } }}>{children}</Box>;
}

export function AccessTableHeader({ title, count, children }: { title: string; count?: number; children: ReactNode }) {
  return <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { xs: 'stretch', sm: 'center' }, justifyContent: 'space-between', flexWrap: 'wrap', gap: 2, px: { xs: 2, sm: 3 }, py: 2, '& > :last-child': { maxWidth: '100%' } }}>
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexShrink: 0 }}><Typography variant="h4" component="h2">{title}</Typography>{count !== undefined && <AccessBadge label={count.toLocaleString()} />}</Stack>
    {children}
  </Stack>;
}

export const accessSearchSx = {
  width: { xs: '100%', sm: 320 },
  maxWidth: '100%',
  minWidth: 0,
  '& .MuiOutlinedInput-root': { height: 40, borderRadius: '999px', bgcolor: '#EEF2F9', fontSize: 13, '& fieldset': { borderColor: 'transparent' }, '&:hover fieldset': { borderColor: '#C1C7D2' } },
};

export const accessTableSx = {
  '& .MuiTableCell-root': { px: 3, py: 1.7, borderColor: '#EEF1F7', verticalAlign: 'middle' },
  '& .MuiTableCell-head': { py: 1.25, bgcolor: '#F6F8FC', fontSize: 12, fontWeight: 600, color: 'text.secondary', borderColor: '#E6EBF3', whiteSpace: 'nowrap' },
  '& .MuiTableRow-root:last-child .MuiTableCell-body': { borderBottom: 0 },
  '& .MuiTableBody-root .MuiTableRow-root:hover': { bgcolor: '#F7F9FD' },
};

export function AccessDetails({ children, label = 'More details' }: { children: ReactNode; label?: string }) {
  return <Box component="details" sx={{ mt: 0.5, color: 'text.secondary', fontSize: 11, '& summary': { cursor: 'pointer', width: 'fit-content', py: 0.25, borderRadius: 1, '&:hover': { color: 'primary.main' }, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 3 } } }}>
    <summary>{label}</summary>
    <Box sx={{ mt: 0.75 }}>{children}</Box>
  </Box>;
}
