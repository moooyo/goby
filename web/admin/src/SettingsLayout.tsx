import type { ReactNode } from 'react';
import { translateNode, useTranslation } from './i18n';
import './settings-pages.css';

export const settingsFieldSx = {
  '& .MuiOutlinedInput-root': { borderRadius: '10px', backgroundColor: 'var(--s1)', minHeight: 34 },
  '& .MuiInputLabel-root': { fontSize: 12 },
  '& .MuiFormHelperText-root': { mx: 0, mt: 0.75, fontSize: 12, lineHeight: '18px' },
};

export function SettingsSource({ source }: { source: string }) {
  const { t } = useTranslation();
  return <span className={'settings-source' + (source === 'database' ? ' settings-source-custom' : '')}>
    {t(source === 'database' ? 'Database override' : 'Deployment default')}
  </span>;
}

export function SettingsGroup({ title, description, source, currentValue, defaultValue, stale = false, preserveValues = false, children }: {
  title: string;
  description: ReactNode;
  source?: string;
  currentValue?: ReactNode;
  defaultValue?: ReactNode;
  stale?: boolean;
  preserveValues?: boolean;
  children: ReactNode;
}) {
  const { t, language } = useTranslation();
  const sameValue = currentValue !== undefined && currentValue === defaultValue;
  return <section className="settings-group" aria-label={t(title)}>
    <header className="settings-group-heading">
      <h2>{t(title)}</h2>
      {source && <SettingsSource source={source} />}
      <details className="settings-group-info">
        <summary aria-label={t('Details') + ' · ' + t(title)}><svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7v.1" /></svg></summary>
        <div>{translateNode(description, language)}</div>
      </details>
    </header>
    <div className="settings-group-fields">
      {children}
      {(currentValue !== undefined || defaultValue !== undefined) && <dl className="settings-confirmed-values">
        {currentValue !== undefined && <div><dt>{t(stale ? 'Last confirmed' : 'Current value')}{sameValue && <> / {t('Deployment default')}</>}</dt><dd translate={preserveValues ? 'no' : undefined}>{preserveValues ? currentValue : translateNode(currentValue, language)}</dd></div>}
        {defaultValue !== undefined && !sameValue && <div><dt>{t('Deployment default')}</dt><dd translate={preserveValues ? 'no' : undefined}>{preserveValues ? defaultValue : translateNode(defaultValue, language)}</dd></div>}
      </dl>}
    </div>
  </section>;
}

export function OverrideMode({ name, override, disabled, onChange }: { name: string; override: boolean; disabled: boolean; onChange: (override: boolean) => void }) {
  const { t, language } = useTranslation();
  const accessibleName = language === 'en' ? name.toLowerCase() : t(name);
  return <div className="settings-override" role="group" aria-label={t(name) + ' · ' + t('Source')}>
    <button type="button" aria-pressed={!override} disabled={disabled} onClick={() => onChange(false)} aria-label={t('Use deployment default for') + ' ' + accessibleName}>
      {!override && <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true"><path d="m5 12 4.5 4.5L19 7" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>}
      {t('Deployment default')}
    </button>
    <button type="button" aria-pressed={override} disabled={disabled} onClick={() => onChange(true)} aria-label={t('Customize') + ' ' + accessibleName}>
      {override && <svg viewBox="0 0 24 24" width="13" height="13" aria-hidden="true"><path d="m5 12 4.5 4.5L19 7" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>}
      {t('Custom')}
    </button>
  </div>;
}
