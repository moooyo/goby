import { useEffect, useState } from 'react';
import { Box, Checkbox, FormControlLabel, Skeleton, Stack, Typography } from '@mui/material';
import { adminApi, isAbortError } from './api';
import type { FeatureInfo } from './api';
import { ErrorNotice } from './components';

export function FeatureAccessFields({ restricted, disabled, error, onChange }: { restricted: string[]; disabled: boolean; error?: string; onChange: (value: string[]) => void }) {
  const [features, setFeatures] = useState<FeatureInfo[]>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<unknown>();
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setLoadError(undefined);
    void adminApi.getFeatures({ signal: controller.signal }).then((result) => {
      if (!controller.signal.aborted) setFeatures(result.Items);
    }).catch((cause) => {
      if (!controller.signal.aborted && !isAbortError(cause)) setLoadError(cause);
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);
  const unavailable = restricted.filter((id) => !features?.some((feature) => feature.Id === id));
  const editingDisabled = disabled || loading || loadError != null || !features?.length;
  return <Box component="section" aria-labelledby="feature-access-heading">
    <Typography variant="h4" component="h3" id="feature-access-heading">Feature access</Typography>
    <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, mb: 2 }}>Choose which features this server allows the user to access. Unchecked features are restricted; separate playback, download, and library permissions still apply.</Typography>
    <Stack spacing={1.5}>
      {loadError != null && <><ErrorNotice error={loadError} retry={() => setRevision((value) => value + 1)} /><Typography variant="body2" color="text.secondary">Feature choices are unavailable. Existing restrictions are retained, and other account settings can still be edited.</Typography></>}
      {loading && !features && <Stack role="status" aria-label="Loading feature choices"><Skeleton height={36} /><Skeleton height={36} /></Stack>}
      {!loading && loadError == null && features?.length === 0 && <Typography variant="body2" color="text.secondary">This server has no configurable user features. Existing restrictions are retained.</Typography>}
      <Box role="group" aria-label="Feature access" sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: '1fr 1fr' }, gap: 0.5 }}>
        {features?.map((feature) => <FormControlLabel key={feature.Id} label={feature.Name} control={<Checkbox checked={!restricted.includes(feature.Id)} disabled={editingDisabled} onChange={(event) => onChange(event.target.checked ? restricted.filter((id) => id !== feature.Id) : [...new Set([...restricted, feature.Id])])} />} />)}
        {unavailable.map((id) => <FormControlLabel key={id} label={`Unlisted feature (${id})`} control={<Checkbox checked={false} disabled />} />)}
      </Box>
      {unavailable.length > 0 && <Typography variant="caption" color="text.secondary">Restrictions for unlisted features are retained when you save.</Typography>}
      {error && <Typography variant="body2" color="error.main">{error}</Typography>}
    </Stack>
  </Box>;
}
