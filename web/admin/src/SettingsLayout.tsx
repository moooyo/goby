import type { ReactNode } from 'react';
import { Box, Chip, Stack, ToggleButton, ToggleButtonGroup, Typography } from '@mui/material';
import CheckRounded from '@mui/icons-material/CheckRounded';

export const settingsFieldSx = {
  '& .MuiOutlinedInput-root': { borderRadius: '4px', backgroundColor: 'transparent', minHeight: 56 },
  '& .MuiInputLabel-root': { fontSize: 14 },
  '& .MuiFormHelperText-root': { mx: 0, mt: 0.75, fontSize: 12, lineHeight: '18px' },
  // The group supplies the disabled opacity; do not multiply MUI's default alpha.
  '& .MuiInputBase-input.Mui-disabled': { WebkitTextFillColor: '#181C23' },
  '& .MuiFormControlLabel-label.Mui-disabled': { color: '#181C23' },
  '& .MuiSwitch-switchBase.Mui-disabled + .MuiSwitch-track': { opacity: 1 },
  '& .MuiInputLabel-root.Mui-disabled:not(.Mui-error), & .MuiFormHelperText-root.Mui-disabled:not(.Mui-error), & .MuiSelect-icon.Mui-disabled': { color: '#434753' },
  '& .MuiOutlinedInput-root.Mui-disabled .MuiOutlinedInput-notchedOutline': { borderColor: '#717785' },
};

export function SettingsSource({ source }: { source: string }) {
  return <Chip size="small" label={source === 'database' ? 'Database override' : 'Deployment default'} sx={{ height: 22, borderRadius: '6px', fontSize: 11, fontWeight: 600, backgroundColor: source === 'database' ? '#D8E4FA' : '#E4E8F0', color: source === 'database' ? '#0F2A57' : '#434753', '& .MuiChip-label': { px: 0.9 } }} />;
}

export function SettingsGroup({ title, description, source, currentValue, defaultValue, stale = false, children }: {
  title: string;
  description: ReactNode;
  source?: string;
  currentValue?: ReactNode;
  defaultValue?: ReactNode;
  stale?: boolean;
  children: ReactNode;
}) {
  return <Box component="section" aria-label={title} sx={{ display: 'flex', flexWrap: 'wrap', gap: '20px 48px', py: 3.5, borderTop: '1px solid #E3E8F1', '&:first-of-type': { borderTop: 0, pt: 1.5 } }}>
    <Box sx={{ flex: '1 1 240px', maxWidth: { xs: 'none', md: 340 }, minWidth: 0 }}>
      <Stack direction="row" sx={{ flexWrap: 'wrap', alignItems: 'center', gap: 1 }}>
        <Typography component="h2" sx={{ fontSize: 16, lineHeight: '24px', fontWeight: 600 }}>{title}</Typography>
        {source && <SettingsSource source={source} />}
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.75, fontSize: 13, lineHeight: '20px' }}>{description}</Typography>
      {(currentValue !== undefined || defaultValue !== undefined) && <Box component="dl" sx={{ display: 'grid', gridTemplateColumns: 'auto minmax(0, 1fr)', gap: '6px 12px', m: 0, mt: 1.75, fontSize: 12, lineHeight: '18px' }}>
        {currentValue !== undefined && <><Box component="dt" sx={{ color: 'text.secondary' }}>{stale ? 'Last confirmed' : 'Current value'}</Box><Box component="dd" sx={{ m: 0, overflowWrap: 'anywhere', fontWeight: 500 }}>{currentValue}</Box></>}
        {defaultValue !== undefined && <><Box component="dt" sx={{ color: 'text.secondary' }}>Deployment default</Box><Box component="dd" sx={{ m: 0, color: 'text.secondary', overflowWrap: 'anywhere' }}>{defaultValue}</Box></>}
      </Box>}
    </Box>
    <Stack spacing={2} sx={{ flex: '999 1 420px', minWidth: 0, ...settingsFieldSx }}>{children}</Stack>
  </Box>;
}

export function OverrideMode({ name, override, disabled, onChange }: { name: string; override: boolean; disabled: boolean; onChange: (override: boolean) => void }) {
  return <ToggleButtonGroup exclusive size="small" value={override ? 'custom' : 'deployment'} disabled={disabled} aria-label={`${name} source`} onChange={(_, value: string | null) => { if (value) onChange(value === 'custom'); }} sx={{ alignSelf: 'flex-start', borderRadius: '20px', minHeight: 40, '& .MuiToggleButton-root': { px: 2, py: 0.8, color: '#434753', fontSize: 12, fontWeight: 500, textTransform: 'none', borderColor: '#717785', lineHeight: '20px', '&.Mui-selected': { backgroundColor: '#D8E4FA', color: '#0F2A57', '&:hover': { backgroundColor: '#C7D8F5' } } } }}>
    <ToggleButton value="deployment" aria-label={`Use deployment default for ${name.toLowerCase()}`}>{!override && <CheckRounded sx={{ fontSize: 16, mr: 0.75 }} />}Deployment default</ToggleButton>
    <ToggleButton value="custom" aria-label={`Customize ${name.toLowerCase()}`}>{override && <CheckRounded sx={{ fontSize: 16, mr: 0.75 }} />}Custom</ToggleButton>
  </ToggleButtonGroup>;
}
