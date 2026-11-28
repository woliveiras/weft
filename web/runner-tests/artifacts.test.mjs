import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, readdir, rm, symlink, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';

const exec = promisify(execFile);
const cli = createRequire(import.meta.url).resolve('@playwright/test/cli');
const web = path.resolve(import.meta.dirname, '..');

test('a second Playwright run preserves the first worker’s live trace and both reports', async t => {
  const dir = await mkdtemp(path.join(tmpdir(), 'weft-playwright-'));
  t.after(() => rm(dir, { recursive: true, force: true }));
  await writeFile(path.join(dir, 'package.json'), JSON.stringify({ type: 'module' }));
  await symlink(path.join(web, 'node_modules'), path.join(dir, 'node_modules'), 'dir');
  await writeFile(path.join(dir, 'playwright.config.ts'), `
    import base from ${JSON.stringify(path.join(web, 'playwright.config.ts'))};
    export default { ...base, testDir: '.', webServer: undefined, use: { ...base.use, trace: 'on' } };
  `);
  await writeFile(path.join(dir, 'overlap.spec.ts'), `
    import { expect, test } from '@playwright/test';
    test('capture a trace', async ({ page }) => {
      await page.setContent('<h1>Ready</h1>');
      if (process.env.HOLD_TRACE_URL) await page.request.get(process.env.HOLD_TRACE_URL);
      await expect(page.getByRole('heading')).toHaveText('Ready');
    });
  `);

  // Hold the first browser context open until the other runner and its worker have exited.
  let signalReady;
  const ready = new Promise(resolve => { signalReady = resolve; });
  const server = createServer((_, response) => signalReady(response));
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => server.close());
  const env = { ...process.env, PLAYWRIGHT_HTML_OPEN: 'never',
    PLAYWRIGHT_LAST_RUN_OUTPUT_FILE: path.join(dir, 'last-run.json') };
  for (const key of ['WEFT_PLAYWRIGHT_RUN_ID', 'PLAYWRIGHT_HTML_OUTPUT_DIR', 'PLAYWRIGHT_HTML_REPORT', 'HOLD_TRACE_URL']) {
    delete env[key];
  }
  const run = (overrides = {}) => exec(process.execPath, [cli, 'test'], {
    cwd: dir, timeout: 45000, env: { ...env, ...overrides },
  });
  const first = run({ HOLD_TRACE_URL: 'http://127.0.0.1:' + server.address().port });
  const response = await Promise.race([ready, first.then(() => assert.fail('First worker never reached the barrier'))]);
  try {
    await run();
  } finally {
    response.end('continue');
    await first;
  }

  for (const [root, filename] of [['test-results', 'trace.zip'], ['playwright-report', 'index.html']]) {
    const files = await readdir(path.join(dir, root), { recursive: true });
    const runs = files.filter(file => path.basename(file) === filename).map(file => file.split(path.sep)[0]);
    assert.equal(new Set(runs).size, 2, 'Expected two independent copies of ' + filename);
  }
});
