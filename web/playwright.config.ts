import { defineConfig, devices } from '@playwright/test';

const port = process.env.WEFT_E2E_PORT || '18089';
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: './e2e',
  testIgnore: ['mcp-cimd.spec.ts', 'visual.spec.ts', 'work-activity.spec.ts'],
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60000,
  expect: { timeout: 15000 },
  reporter: [['list'], ['html', { open: 'never' }]],
  use: { actionTimeout: 15000, baseURL, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'pnpm run build && cd ../server && go build -tags e2e -o bin/weft-e2e ./cmd/weft-e2e && exec bin/weft-e2e',
    url: `${baseURL}/healthz`,
    reuseExistingServer: false,
    timeout: 120000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 15000 },
  },
});
