// @vitest-environment node
import { basename, join, resolve } from 'node:path';
import { afterEach, expect, it, vi } from 'vitest';
import { runArtifacts } from '../playwright-artifacts';

afterEach(() => vi.unstubAllEnvs());

it('uses the same generated ID for the worker outputs and the HTML report and keeps last-failed discoverable', () => {
  vi.stubEnv('WEFT_PLAYWRIGHT_RUN_ID', undefined);
  vi.stubEnv('PLAYWRIGHT_LAST_RUN_OUTPUT_FILE', undefined);
  const config = runArtifacts('test-results', 'playwright-report');
  const id = basename(config.outputDir!);
  expect(config.outputDir).toBe(join('test-results', id));
  expect(config.reporter).toEqual([['list'], ['html', { outputFolder: join('playwright-report', id), open: 'never' }]]);
  expect(process.env.WEFT_PLAYWRIGHT_RUN_ID).toBe(id);
  expect(runArtifacts('test-results').outputDir).toBe(config.outputDir);
  expect(process.env.PLAYWRIGHT_LAST_RUN_OUTPUT_FILE).toBe(resolve(import.meta.dirname, '../test-results/.last-run.json'));
});

it('preserves an explicit rerun history and rejects a run ID that escapes the output directory', () => {
  vi.stubEnv('PLAYWRIGHT_LAST_RUN_OUTPUT_FILE', 'my-rerun.json');
  vi.stubEnv('WEFT_PLAYWRIGHT_RUN_ID', '00000000-0000-4000-8000-000000000001');
  expect(runArtifacts('test-results-native').reporter).toEqual([['list']]);
  expect(process.env.PLAYWRIGHT_LAST_RUN_OUTPUT_FILE).toBe('my-rerun.json');
  vi.stubEnv('WEFT_PLAYWRIGHT_RUN_ID', '..');
  expect(() => runArtifacts('test-results-native')).toThrow('Invalid Playwright run ID');
});
