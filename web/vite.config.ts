import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const api = process.env.WEFT_API ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: api, changeOrigin: true },
      '/healthz': api,
      '/readyz': api,
      '/metrics': api,
    },
  },
  build: { outDir: 'dist', sourcemap: true, chunkSizeWarningLimit: 1200 },
});
