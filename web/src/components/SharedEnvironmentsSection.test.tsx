import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it } from 'vitest';
import { apiError, fakeApi } from '../test/fakeApi';
import { SharedEnvironmentsSection } from './SharedEnvironmentsSection';

const path = 'POST /api/v1/environments/cleanup';
const unused = { id: 'shared_a', image: 'weft-shared/a:ready-user', caches: [{ volume: 'seed_a', path: '/root/.npm' }], eligible: true, removed: false };
const retained = { ...unused, id: 'shared_b', eligible: false, usedBy: ['graph:other', 'container:stopped'] };
afterEach(cleanup);

async function selectAndReview() {
  const user = userEvent.setup();
  await user.click(screen.getByRole('button', { name: 'Refresh preview' }));
  await user.click(await screen.findByRole('checkbox', { name: 'Select shared_a' }));
  await user.click(screen.getByRole('button', { name: 'Review cleanup (1)' }));
  return user;
}

describe('shared resource cleanup API integration', () => {
  it('requires admin, invalidates confirmation on role change and never fetches for viewers', async () => {
    const backend = fakeApi({ [path]: () => ({ dryRun: true, generations: [unused] }) });
    const view = render(<SharedEnvironmentsSection canManage={false} />);
    expect(screen.queryByRole('button')).toBeNull();
    expect(backend.calls).toHaveLength(0);
    view.rerender(<SharedEnvironmentsSection canManage />);
    await selectAndReview();
    view.rerender(<SharedEnvironmentsSection canManage={false} />);
    view.rerender(<SharedEnvironmentsSection canManage />);
    expect(screen.queryByRole('button', { name: 'Confirm cleanup' })).toBeNull();
  });

  it('previews without deletion, excludes protected resources and requires explicit confirmation', async () => {
    const backend = fakeApi({ [path]: ({ body }) => {
      if ((body as { dryRun: boolean }).dryRun) return { dryRun: true, generations: [unused, retained] };
      return { dryRun: false, generations: [{ ...unused, removed: true }] };
    } });
    render(<SharedEnvironmentsSection canManage />);
    const user = await selectAndReview();
    expect((screen.getByRole('checkbox', { name: 'Select shared_b' }) as HTMLInputElement).disabled).toBe(true);
    const confirmation = screen.getByRole('region', { name: 'Confirm shared resource cleanup' });
    expect(within(confirmation).getByText('seed_a')).toBeTruthy();
    expect(backend.calls).toHaveLength(1);
    await user.click(screen.getByRole('button', { name: 'Cancel cleanup' }));
    expect(backend.calls).toHaveLength(1);
    await user.click(screen.getByRole('button', { name: 'Review cleanup (1)' }));
    await user.click(screen.getByRole('button', { name: 'Confirm cleanup' }));
    expect(await screen.findByText(/: Removed/)).toBeTruthy();
    expect(backend.calls.map(call => call.body)).toEqual([{ dryRun: true }, { dryRun: false, generations: ['shared_a'] }, { dryRun: true }]);
  });

  it('reports a newly retained resource and partial failure rather than claiming success', async () => {
    fakeApi({ [path]: ({ body }) => ({ dryRun: (body as { dryRun: boolean }).dryRun,
      generations: (body as { dryRun: boolean }).dryRun ? [unused] : [
        { ...unused, eligible: false, usedBy: ['graph:new-task'] },
        { ...unused, id: 'shared_error', error: 'volume busy' },
        { ...unused, id: 'shared_missing', missing: true },
      ] }) });
    render(<SharedEnvironmentsSection canManage />);
    const user = await selectAndReview();
    await user.click(screen.getByRole('button', { name: 'Confirm cleanup' }));
    const report = await screen.findByRole('status');
    expect(report.textContent).toContain('Retained — graph:new-task');
    expect(report.textContent).toContain('Failed: volume busy');
    expect(report.textContent).toContain('Already absent');
  });

  it('clears stale selection after an uncertain mutation and failed refresh', async () => {
    let calls = 0;
    fakeApi({ [path]: () => ++calls === 1 ? { dryRun: true, generations: [unused] } : apiError(503, 'daemon unavailable', 'unavailable') });
    render(<SharedEnvironmentsSection canManage />);
    const user = await selectAndReview();
    await user.click(screen.getByRole('button', { name: 'Confirm cleanup' }));
    expect((await screen.findByRole('alert')).textContent).toContain('Some resources may have been removed');
    expect(screen.getByRole('alert').textContent).toContain('Preview failed');
    expect(screen.queryByRole('checkbox')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Confirm cleanup' })).toBeNull();
  });

  it('handles empty previews and inspection errors without offering deletion', async () => {
    let calls = 0;
    fakeApi({ [path]: () => ({ dryRun: true, generations: ++calls === 1 ? [] : [{ ...unused, error: 'inspect failed' }] }) });
    render(<SharedEnvironmentsSection canManage />);
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Refresh preview' }));
    expect(await screen.findByText('No registered shared resources found.')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Refresh preview' }));
    expect((await screen.findByRole('checkbox')) as HTMLInputElement).toHaveProperty('disabled', true);
  });

  it('caps selection at 100 and allows deselection to free a slot', async () => {
    fakeApi({ [path]: () => ({ dryRun: true, generations: Array.from({ length: 101 }, (_, index) => ({ ...unused, id: `shared_${index}` })) }) });
    render(<SharedEnvironmentsSection canManage />);
    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Refresh preview' }));
    const boxes = await screen.findAllByRole('checkbox');
    for (const box of boxes.slice(0, 100)) await user.click(box);
    expect(boxes[100]).toHaveProperty('disabled', true);
    await user.click(boxes[0]);
    expect(boxes[100]).toHaveProperty('disabled', false);
    await user.click(boxes[100]);
    expect(screen.getByRole('button', { name: 'Review cleanup (100)' })).toBeTruthy();
  });

  it('prevents duplicate deletion while a request is pending', async () => {
    let resolve!: (value: unknown) => void;
    const backend = fakeApi({ [path]: ({ body }) => (body as { dryRun: boolean }).dryRun
      ? { dryRun: true, generations: [unused] } : new Promise(done => { resolve = done; }) });
    render(<SharedEnvironmentsSection canManage />);
    const user = await selectAndReview();
    await user.dblClick(screen.getByRole('button', { name: 'Confirm cleanup' }));
    expect(backend.calls.filter(call => !(call.body as { dryRun: boolean }).dryRun)).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'Cancel cleanup' })).toHaveProperty('disabled', true);
    resolve({ dryRun: false, generations: [{ ...unused, removed: true }] });
    await screen.findByText(/: Removed/);
  });
});
