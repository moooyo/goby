import type { ReactNode } from 'react';
import { Box, Chip, Stack, Typography } from './ui';
import type { ChipProps } from './ui';
import { useTranslation } from './i18n';
import './access-pages.css';

const tones = {
  success: { background: 'color-mix(in srgb, var(--ok) 10%, transparent)', foreground: 'var(--ok)' },
  warning: { background: 'color-mix(in srgb, var(--warn) 10%, transparent)', foreground: 'var(--warn)' },
  error: { background: 'color-mix(in srgb, var(--err) 10%, transparent)', foreground: 'var(--err)' },
  info: { background: 'var(--acc-soft)', foreground: 'var(--acc-ink)' },
  neutral: { background: 'var(--s3)', foreground: 'var(--ink2)' },
};

export function AccessBadge({ tone = 'neutral', ...props }: Omit<ChipProps, 'color'> & { tone?: keyof typeof tones }) {
  const color = tones[tone];
  return <Chip {...props} size="small" sx={{ height: 24, borderRadius: '999px', border: '1px solid var(--line)', bgcolor: color.background, color: color.foreground, fontSize: 11, fontWeight: 600, '& .MuiChip-label': { px: 0.9 }, '& .MuiChip-icon': { color: 'inherit', fontSize: 14 }, ...props.sx }} />;
}

export function AccessIconTile({ children }: { children: ReactNode }) {
  return <Box sx={{ width: 32, height: 32, borderRadius: '9px', display: 'grid', placeItems: 'center', flexShrink: 0, bgcolor: 'var(--s3)', color: 'text.secondary', '& .MuiSvgIcon-root': { fontSize: 21 } }}>{children}</Box>;
}

export function AccessTableHeader({ title, count, children }: { title: string; count?: number; children: ReactNode }) {
  return <Stack direction={{ xs: 'column', sm: 'row' }} sx={{ alignItems: { xs: 'stretch', sm: 'center' }, justifyContent: 'space-between', flexWrap: 'wrap', gap: 1.5, px: 2, py: 1.25, minHeight: 44, '& > :last-child': { maxWidth: '100%' } }}>
    <Stack direction="row" sx={{ alignItems: 'center', gap: 1, flexShrink: 0 }}><Typography variant="h4" component="h2">{title}</Typography>{count !== undefined && <AccessBadge label={new Intl.NumberFormat(typeof document === 'undefined' ? 'en-US' : document.documentElement.lang).format(count)} />}</Stack>
    {children}
  </Stack>;
}

export const accessSearchSx = {
  width: { xs: '100%', sm: 320 },
  maxWidth: '100%',
  minWidth: 0,
  '& .MuiOutlinedInput-root': { height: 34, borderRadius: '10px', bgcolor: 'var(--s1)', fontSize: 13, '& fieldset': { borderColor: 'transparent' }, '&:hover fieldset': { borderColor: 'var(--line2)' } },
};

export const accessTableSx = {
  '& .MuiTableCell-root': { px: 2, py: 1.25, borderColor: 'var(--line)', verticalAlign: 'middle' },
  '& .MuiTableCell-head': { py: 1, bgcolor: 'var(--s2)', fontSize: 11, fontWeight: 600, color: 'text.secondary', borderColor: 'var(--line)', whiteSpace: 'nowrap' },
  '& .MuiTableRow-root:last-child .MuiTableCell-body': { borderBottom: 0 },
  '& .MuiTableBody-root .MuiTableRow-root:hover': { bgcolor: 'var(--s3)' },
};

export function AccessDetails({ children, label = 'More details' }: { children: ReactNode; label?: string }) {
  const { t } = useTranslation();
  return <Box component="details" sx={{ mt: 0.5, color: 'text.secondary', fontSize: 11, '& summary': { cursor: 'pointer', width: 'fit-content', py: 0.25, borderRadius: 1, '&:hover': { color: 'primary.main' }, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 3 } } }}>
    <summary>{t(label)}</summary>
    <Box sx={{ mt: 0.75 }}>{children}</Box>
  </Box>;
}
