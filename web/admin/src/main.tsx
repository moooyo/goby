import React from 'react';
import ReactDOM from 'react-dom/client';
import { CssBaseline, ThemeProvider } from '@mui/material';
import '@fontsource-variable/manrope';
import '@fontsource-variable/public-sans';
import { App } from './App';
import { ErrorBoundary } from './ErrorBoundary';
import { theme } from './theme';
import './styles.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <ErrorBoundary><App /></ErrorBoundary>
    </ThemeProvider>
  </React.StrictMode>,
);
