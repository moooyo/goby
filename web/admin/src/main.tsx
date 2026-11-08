import React from 'react';
import ReactDOM from 'react-dom/client';
import '@fontsource-variable/noto-serif-sc';
import '@fontsource-variable/league-gothic';
import '@fontsource-variable/noto-sans-sc';
import '@fontsource/ibm-plex-mono/400.css';
import '@fontsource/ibm-plex-mono/500.css';
import { App } from './App';
import { ErrorBoundary } from './ErrorBoundary';
import { AdminPreferencesProvider } from './i18n';
import './styles.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <AdminPreferencesProvider>
      <ErrorBoundary><App /></ErrorBoundary>
    </AdminPreferencesProvider>
  </React.StrictMode>,
);
