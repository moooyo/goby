import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { frontendContributions } from './build/frontend-contributions';

export default defineConfig({
  plugins: [react(), frontendContributions()],
  base: '/admin/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    assetsInlineLimit: 0,
  },
  server: {
    proxy: {
      '/admin/v1': {
        target: process.env.GOBY_DEV_API ?? 'http://127.0.0.1:8096',
      },
    },
  },
});
