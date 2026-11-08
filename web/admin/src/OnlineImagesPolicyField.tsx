import { Checkbox, FormControlLabel, Typography } from './ui';
import { useTranslation } from './i18n';
import type { LibraryOptions } from './libraryOptions';

export function OnlineImagesPolicyField({ collectionType, options, disabled, onChange }: {
  collectionType: string; options: LibraryOptions; disabled: boolean; onChange: (value: LibraryOptions) => void;
}) {
  const { t } = useTranslation();
  if (!['movies', 'tvshows', 'mixed'].includes(collectionType)) return null;
  return <div>
    <FormControlLabel control={<Checkbox checked={options.EnableOnlineImages ?? false} disabled={disabled}
      onChange={(event) => onChange({ ...options, EnableOnlineImages: event.target.checked })} />}
      label={t('Automatically download missing images')} />
    <Typography variant="body2" color="text.secondary">{t('Uses known TMDB matches for missing posters, backdrops, logos, and episode thumbnails. Existing images and manual deletions are kept.')}</Typography>
    <Typography variant="body2" color="text.secondary">{t('Runs after enabling and completed scans, and checks daily. Internet providers and TMDB credentials must be enabled. View progress or stop downloads in Tasks.')}</Typography>
  </div>;
}
