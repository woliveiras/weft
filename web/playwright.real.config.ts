import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e-real', workers: 1, retries: 0,
  timeout: 180000, expect: { timeout: 30000 },
  reporter: [['list'], ['html', { outputFolder: 'playwright-report-real', open: 'never' }]],
  outputDir: 'test-results-real',
  use: { baseURL: 'http://127.0.0.1:18090', actionTimeout: 15000, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [{ name: 'chromium-real-sandbox', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'pnpm run build && node ../scripts/e2e-real.mjs',
    url: 'http://127.0.0.1:18090/healthz', reuseExistingServer: false,
    timeout: 180000, gracefulShutdown: { signal: 'SIGTERM', timeout: 75000 },
  },
});
