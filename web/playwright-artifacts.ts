import { randomUUID } from 'node:crypto';
import { join, resolve } from 'node:path';
import type { PlaywrightTestConfig } from '@playwright/test';

export function runArtifacts(results: string, report?: string): Pick<PlaywrightTestConfig, 'outputDir' | 'reporter'> {
  // Workers reload the config. All workers of one runner must use its directory.
  const id = process.env.WEFT_PLAYWRIGHT_RUN_ID ??= randomUUID();
  if (!/^[\da-f]{8}(?:-[\da-f]{4}){3}-[\da-f]{12}$/i.test(id)) {
    throw new Error('Invalid Playwright run ID');
  }
  // Keep --last-failed usable across invocations with different output directories.
  process.env.PLAYWRIGHT_LAST_RUN_OUTPUT_FILE ??= resolve(import.meta.dirname, results, '.last-run.json');
  return {
    outputDir: join(results, id),
    reporter: report ? [['list'], ['html', { outputFolder: join(report, id), open: 'never' }]] : [['list']],
  };
}
