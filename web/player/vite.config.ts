import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  base: '/',
  build: { outDir: 'dist', assetsInlineLimit: 0 },
  server: {
    port: 5174,
    strictPort: true,
    proxy: {
      '/emby': { target: process.env.GOBY_DEV_API ?? 'http://127.0.0.1:8096' },
      '/admin': { target: process.env.GOBY_DEV_API ?? 'http://127.0.0.1:8096' },
    },
  },
});
