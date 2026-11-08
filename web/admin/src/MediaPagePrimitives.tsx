import type { ReactNode } from 'react';
import { Box, IconButton, InputAdornment, TextField } from './ui';
import { SearchRounded } from './icons';
import './media-pages.css';

export const mediaSurface = 'var(--s2)';
export const mediaSelected = 'var(--acc-soft)';
export const mediaPanelSx = { borderRadius: '14px', overflow: 'hidden', backgroundColor: 'var(--s2)', borderColor: 'var(--line)' } as const;
export const mediaSelectSx = { minWidth: 150, '& .MuiOutlinedInput-root': { height: 34, borderRadius: '10px', '& fieldset': { borderColor: 'var(--line2)' } }, '& .MuiSelect-select': { py: 0.75, fontSize: 13 } } as const;

export const mediaFeatures = [
  { key: 'EnablePreviewGeneration', label: 'Seek previews', description: 'Preview images while seeking', video: true, episodes: false },
  { key: 'EnableIntroDetection', label: 'Intro detection', description: 'Matched episode intros', video: true, episodes: true },
  { key: 'EnableCreditsDetection', label: 'Credits detection', description: 'Automatic credits markers', video: true, episodes: false },
  { key: 'EnableBackgroundPreviewGeneration', label: 'Background clips', description: 'Short clips for media details', video: true, episodes: false },
  { key: 'EnableAudioWaveformGeneration', label: 'Audio waveforms', description: 'Waveforms for each audio track', video: true, episodes: false },
  { key: 'EnableSubtitleTimelineGeneration', label: 'Subtitle timelines', description: 'Image subtitle display intervals', video: true, episodes: false },
] as const;

export function MediaIconTile({ children, size = 44, neutral = false }: { children: ReactNode; size?: number; neutral?: boolean }) {
  return <Box aria-hidden="true" className="media-icon-tile" sx={{ display: 'grid', placeItems: 'center', width: size, height: size, flexShrink: 0, borderRadius: neutral ? '50%' : '10px', bgcolor: 'var(--s3)', color: 'var(--ink2)', '& .MuiSvgIcon-root': { fontSize: size > 40 ? 22 : 17 } }}>{children}</Box>;
}

export function MediaSearchField({ label, value, onChange, disabled = false, maxLength }: { label: string; value: string; onChange: (value: string) => void; disabled?: boolean; maxLength?: number }) {
  return <TextField className="media-search-field" fullWidth size="small" placeholder={label} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} slotProps={{ htmlInput: { 'aria-label': label, autoComplete: 'off', maxLength }, input: { startAdornment: <InputAdornment position="start"><IconButton type="submit" size="small" aria-label={label} disabled={disabled} sx={{ ml: -0.5 }}><SearchRounded sx={{ fontSize: 15 }} /></IconButton></InputAdornment> } }} sx={{ '& .MuiOutlinedInput-root': { height: 34, bgcolor: 'var(--s2)', borderRadius: '10px', fontSize: 13, '& fieldset': { borderColor: 'var(--line2)' }, '&.Mui-focused fieldset': { borderColor: 'var(--acc)' } } }} />;
}
