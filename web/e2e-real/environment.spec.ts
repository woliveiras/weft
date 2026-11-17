import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { expect, test, type Page } from '@playwright/test';

const exec = promisify(execFile);
const daemon = process.env.WEFT_TEST_SANDBOX_CONTAINER || 'weft-sandbox-daemon-1';
const headers = { Authorization: 'Bearer e2e-admin-token' };
async function read(page: Page, id: string, suffix: string) {
  const response = await page.request.get(`/api/v1/graphs/${id}/${suffix}`, { headers });
  expect(response.ok()).toBeTruthy();
  return response.json();
}
async function file(page: Page, id: string) {
  await page.goto(`/#/g/${id}/files`);
  await page.getByRole('button', { name: /^result\.txt/ }).click();
  await expect(page.getByRole('region', { name: 'File contents' })).toContainText(id);
}

test('approved proposal builds a real sandbox; shell.exec writes isolated workspaces with shared image reuse', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  const views = [];
  const workdirs: string[] = [];
  for (const id of ['fixture-real-a', 'fixture-real-b']) {
    await page.goto(`/#/g/${id}/canvas`);
    await page.getByRole('button', { name: 'Graph settings', exact: true }).first().click();
    await page.getByRole('button', { name: 'Discover environment', exact: true }).click();
    await expect(page.getByRole('region', { name: 'Environment analysis' })).toContainText('No environment definition found');
    await page.getByLabel('Start graph when the approved environment is ready').check();
    await page.getByRole('button', { name: 'Analyze repository', exact: true }).click();
    await expect(page.getByText('Environment proposal: awaiting_approval')).toBeVisible();
    expect((await read(page, id, 'environment')).state).toBe('absent');
    expect(await read(page, id, 'executions')).toHaveLength(0);
    const blocked = page.waitForResponse(r => r.url().endsWith(`/graphs/${id}/start`) && r.request().method() === 'POST');
    await page.getByRole('button', { name: 'Run all', exact: true }).click();
    expect((await blocked).status()).toBe(422);
    await page.goto('/#/approvals');
    const approval = page.locator('article.approval').filter({ hasText: id });
    await expect(approval).toContainText('alpine:3.22');
    await approval.getByRole('button', { name: 'Approve', exact: true }).click();
    await expect.poll(async () => (await read(page, id, 'environment')).state, { timeout: 90000 }).toBe('ready');
    await expect.poll(async () => (await read(page, id, 'executions')).map((e: { status: string }) => e.status)).toEqual(['succeeded']);
    views.push(await read(page, id, 'environment'));
    const events = await read(page, id, 'events');
    expect(events.some((e: { type: string; data?: { tool: string; outcome: string } }) => e.type === 'tool.completed' && e.data?.tool === 'shell.exec' && e.data.outcome === 'ok')).toBe(true);
    const status = await read(page, id, 'status');
    workdirs.push(status.nodes[0].replicas[0].workdir);
    await page.goto(`/#/g/${id}/canvas`);
    await page.locator('.react-flow__node').filter({ hasText: 'worker' }).click();
    await expect(page.getByText(/Real shell verified:.*exit code: 0/).first()).toBeVisible();
    if (id === 'fixture-real-b') await expect(page.getByText('Reused compatible prepared image and cache snapshots.')).toBeVisible();
    await file(page, id);
  }
  expect(views[0].reused ?? false).toBe(false);
  expect(views[1].reused).toBe(true);
  expect(views[0].sharedImage).toBeTruthy();
  expect(views[1].sharedImage).toBe(views[0].sharedImage);
  const image = views[0].sharedImage as string;
  const inspected = await exec('docker', ['exec', daemon, 'docker', 'image', 'inspect', '--format', '{{.Id}}', image]);
  expect(inspected.stdout.trim()).toMatch(/^sha256:/);
  // The host daemon must not contain this uniquely generated prepared image.
  await expect(exec('docker', ['image', 'inspect', image])).rejects.toMatchObject({ code: 1 });
  expect(workdirs[0]).toBeTruthy();
  expect(workdirs[1]).toBeTruthy();
  expect(workdirs[0]).not.toBe(workdirs[1]);
  // The second shell command required result.txt to be absent, then wrote its
  // own marker. Re-read the first workspace to prove it was not overwritten.
  await file(page, 'fixture-real-a');
  await expect(page.getByRole('region', { name: 'File contents' })).not.toContainText('fixture-real-b');
  await page.goto('/#/g/fixture-real-a/canvas');
  await page.getByRole('button', { name: 'Stop all', exact: true }).click();
  await expect.poll(async () => (await read(page, 'fixture-real-a', 'status')).state).toBe('stopped');
  await page.getByRole('button', { name: 'Graph settings', exact: true }).first().click();
  page.once('dialog', dialog => dialog.accept());
  await page.getByRole('button', { name: 'Reset', exact: true }).click();
  await expect.poll(async () => (await read(page, 'fixture-real-a', 'environment')).state).toBe('absent');
  expect((await read(page, 'fixture-real-b', 'environment')).upToDate).toBe(true);
  await exec('docker', ['exec', daemon, 'docker', 'image', 'inspect', image]);
  await file(page, 'fixture-real-b');
  await file(page, 'fixture-real-a');

});

for (const [source, path] of [
  ['weft', '.weft/environment.yaml'], ['devcontainer', '.devcontainer/devcontainer.json'],
  ['dockerfile', 'Dockerfile'], ['compose', 'compose.yaml'],
]) {
  test(`real ${source} discovery, acceptance, build and native shell execution`, async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
    const id = `fixture-source-${source}`;
    await page.goto(`/#/g/${id}/canvas`);
    await page.getByRole('button', { name: 'Graph settings', exact: true }).first().click();
    await page.getByRole('button', { name: 'Discover environment', exact: true }).click();
    await page.getByRole('combobox', { name: 'Repository definition', exact: true }).selectOption(path);
    if (source === 'compose') await page.getByRole('combobox', { name: 'Development service', exact: true }).selectOption('dev');
    await page.getByRole('button', { name: 'Review environment', exact: true }).click();
    await expect(page.getByLabel('Environment proposal', { exact: true })).toContainText('alpine:3.22');
    expect((await read(page, id, 'environment')).state).toBe('absent');
    await page.getByRole('button', { name: 'Accept into draft', exact: true }).click();
    await page.getByRole('button', { name: 'Save graph', exact: true }).click();
    await page.getByRole('button', { name: 'Build', exact: true }).click();
    await expect.poll(async () => (await read(page, id, 'environment')).state, { timeout: 90000 }).toBe('ready');
    const before = await read(page, id, 'environment');
    await page.getByRole('button', { name: 'Run all', exact: true }).click();
    await expect.poll(async () => (await read(page, id, 'executions')).map((e: { status: string }) => e.status)).toEqual(['succeeded']);
    await file(page, id);
    if (source === 'weft') {
      // The real agent wrote a new lockfile; the manager must invalidate reuse.
      await page.goto(`/#/g/${id}/canvas`);
      await page.getByRole('button', { name: 'Stop all', exact: true }).click();
      await expect.poll(async () => (await read(page, id, 'status')).state).toBe('stopped');
      await page.getByRole('button', { name: 'Graph settings', exact: true }).first().click();
      await expect(page.getByText('Outdated', { exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'Build', exact: true }).click();
      await expect.poll(async () => (await read(page, id, 'environment')).upToDate).toBe(true);
      expect((await read(page, id, 'environment')).sharedImage).not.toBe(before.sharedImage);
    }
  });
}

test('mise proposal installs locked tools and dependencies, builds and tests offline in two isolated tasks', async ({ page }) => {
  test.setTimeout(480000);
  await page.addInitScript(() => localStorage.setItem('weftToken', 'e2e-admin-token'));
  const views = [];
  for (const id of ['fixture-mise-a', 'fixture-mise-b']) {
    await page.goto(`/#/g/${id}/canvas`);
    await page.getByRole('button', { name: 'Graph settings', exact: true }).first().click();
    await page.getByRole('button', { name: 'Discover environment', exact: true }).click();
    await expect(page.getByRole('region', { name: 'Environment analysis' })).toContainText('No environment definition found');
    await page.getByLabel('Start graph when the approved environment is ready').check();
    await page.getByRole('button', { name: 'Analyze repository', exact: true }).click();
    await expect(page.getByText('Environment proposal: awaiting_approval')).toBeVisible();
    expect((await read(page, id, 'environment')).state).toBe('absent');
    expect(await read(page, id, 'executions')).toHaveLength(0);
    await page.goto('/#/approvals');
    const approval = page.locator('article.approval').filter({ hasText: id });
    await expect(approval).toContainText('mise install');
    await expect(approval).toContainText('pnpm install --frozen-lockfile --ignore-scripts');
    await approval.getByRole('button', { name: 'Approve', exact: true }).click();
    await expect.poll(async () => {
      const view = await read(page, id, 'environment');
      if (view.state === 'failed') throw new Error(JSON.stringify(view));
      return view.state;
    }, { timeout: 180000 }).toBe('ready');
    await expect.poll(async () => (await read(page, id, 'executions')).map((e: { status: string }) => e.status), { timeout: 60000 }).toEqual(['succeeded']);
    views.push(await read(page, id, 'environment'));
    await page.goto(`/#/g/${id}/canvas`);
    await page.locator('.react-flow__node').filter({ hasText: 'worker' }).click();
    await expect(page.getByText(/Real shell verified:.*exit code: 0/).first()).toContainText('tests 2');
    await file(page, id);
  }
  expect(views[0].reused ?? false).toBe(false);
  expect(views[1].reused).toBe(true);
  expect(views[1].sharedImage).toBe(views[0].sharedImage);
  expect(views[1].image).not.toBe(views[0].image);
  expect(views[0].caches).toHaveLength(1);
  expect(views[0].caches[0].path).toBe('/home/sandbox/.local/share/pnpm/store');
  expect(views[1].caches[0].volume).not.toBe(views[0].caches[0].volume);
  await file(page, 'fixture-mise-a');
  await expect(page.getByRole('region', { name: 'File contents' })).not.toContainText('fixture-mise-b');
});
