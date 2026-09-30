import { useState } from 'react';
import { Accordion, AccordionDetails, AccordionSummary, Box, Stack, TextField, Typography } from '@mui/material';
import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import { fieldError } from './formFields';
import { analysisNumberFields, introSkipperFields } from './mediaAnalysis';
import type { AnalysisDraft, AnalysisErrors, AnalysisField } from './mediaAnalysis';

const fieldGrid = { display: 'grid', gridTemplateColumns: { xs: 'minmax(0, 1fr)', sm: 'repeat(2, minmax(0, 1fr))' }, gap: 2.5 };

interface Props {
  draft: AnalysisDraft;
  errors: AnalysisErrors;
  mutationError: unknown;
  disabled: boolean;
  onChange: (value: AnalysisDraft) => void;
}

export function AnalysisConfigurationFields({ draft, errors, mutationError, disabled, onChange }: Props) {
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const issue = (field: AnalysisField) => errors[field] ?? fieldError(mutationError, `Profile.${field}`);
  const advancedErrors = introSkipperFields.some((field) => field.advanced && issue(`IntroSkipper.${field.key}`));
  const introFields = (advanced: boolean) => introSkipperFields.filter((field) => Boolean(field.advanced) === advanced).map((field) => {
    const error = issue(`IntroSkipper.${field.key}`);
    return <TextField key={field.key} id={`analysis-intro-${field.key}`} fullWidth label={field.label} value={draft.IntroSkipper[field.key]} disabled={disabled}
      onChange={(event) => { if (field.advanced) setAdvancedOpen(true); onChange({ ...draft, IntroSkipper: { ...draft.IntroSkipper, [field.key]: event.target.value } }); }}
      error={Boolean(error)} helperText={error ?? field.help}
      slotProps={{ htmlInput: { inputMode: field.decimal ? 'decimal' : 'numeric', maxLength: 16, autoComplete: 'off' }, formHelperText: { 'aria-live': 'polite' } }} />;
  });

  return <Stack spacing={3}>
    <Stack component="section" aria-labelledby="analysis-intro-settings-title" spacing={2.5}>
      <Box><Typography component="h3" variant="h4" id="analysis-intro-settings-title">Intro detection</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.75 }}>Uses Intro Skipper audio matching. The default values follow upstream. Library settings control which TV libraries are analyzed.</Typography></Box>
      <Box sx={fieldGrid}>{introFields(false)}</Box>
      <Accordion expanded={advancedOpen || advancedErrors} onChange={(_, expanded) => setAdvancedOpen(expanded)} disableGutters elevation={0}
        sx={{ bgcolor: 'transparent', border: 1, borderColor: 'divider', borderRadius: '12px', '&::before': { display: 'none' } }}>
        <AccordionSummary expandIcon={<ExpandMoreRounded />} id="analysis-intro-advanced-heading" aria-controls="analysis-intro-advanced-content">
          <Typography variant="body2" sx={{ fontWeight: 600 }}>Advanced audio matching{advancedErrors ? ' · Check values' : ''}</Typography>
        </AccordionSummary>
        <AccordionDetails><Stack spacing={2.5}>
          <Typography variant="body2" color="text.secondary">These upstream controls change how fingerprints match. The ranges shown are Goby's supported processing limits.</Typography>
          <Box sx={fieldGrid}>{introFields(true)}</Box>
        </Stack></AccordionDetails>
      </Accordion>
    </Stack>
    <Stack component="section" aria-labelledby="analysis-processing-settings-title" spacing={2.5}>
      <Box><Typography component="h3" variant="h4" id="analysis-processing-settings-title">Seek previews and processing limits</Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.75 }}>Preview settings and resource budgets apply to newly admitted work.</Typography></Box>
      <Box sx={fieldGrid}>{analysisNumberFields.map((field) => {
        const error = issue(field.key);
        return <TextField key={field.key} id={`analysis-${field.key}`} fullWidth label={field.label} value={draft[field.key]} disabled={disabled}
          onChange={(event) => onChange({ ...draft, [field.key]: event.target.value })} error={Boolean(error)} helperText={error ?? field.help}
          slotProps={{ htmlInput: { inputMode: 'numeric', maxLength: 16, autoComplete: 'off' }, formHelperText: { 'aria-live': 'polite' } }} />;
      })}</Box>
    </Stack>
  </Stack>;
}
