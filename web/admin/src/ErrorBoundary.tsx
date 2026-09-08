import { Component } from 'react';
import type { ReactNode } from 'react';
import { Button, Paper, Stack, Typography } from '@mui/material';
import { Brand } from './components';

export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <Stack component="main" sx={{ minHeight: '100dvh', p: 3, alignItems: 'center', justifyContent: 'center' }}>
        <Paper variant="outlined" sx={{ width: '100%', maxWidth: 500, p: 4 }}>
          <Brand />
          <Typography component="h1" variant="h3" sx={{ mt: 4 }}>The dashboard could not be loaded</Typography>
          <Typography color="text.secondary" sx={{ mt: 1.5 }}>Reload the page to get the latest dashboard files and reconnect to your server.</Typography>
          <Button variant="contained" onClick={() => window.location.reload()} sx={{ mt: 3 }}>Reload dashboard</Button>
        </Paper>
      </Stack>
    );
  }
}
