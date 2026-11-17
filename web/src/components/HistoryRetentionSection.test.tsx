import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it } from 'vitest';
import { fakeApi, apiError } from '../test/fakeApi';
import { HistoryRetentionSection } from './HistoryRetentionSection';
const current = { policy: { days: 7, maxEvents: 1000 }, revision: 2 };
afterEach(cleanup);
it('shows current limits read-only to non-administrators', async () => {
 fakeApi({ 'GET /api/v1/blocked-history-retention': () => current });
 render(<HistoryRetentionSection canManage={false} />);
 const input = await screen.findByLabelText('Retention days (1–90)');
 expect((input as HTMLInputElement).disabled).toBe(true);
 expect(screen.queryByRole('button', { name: 'Review retention changes' })).toBeNull();
});
it('reviews exact limits and revision before saving and permits cancellation', async () => {
 let saved = current;
 const backend = fakeApi({ 'GET /api/v1/blocked-history-retention': () => saved,
 'PUT /api/v1/blocked-history-retention': call => { expect(call.body).toEqual({ policy: { days: 2, maxEvents: 1000 }, revision: 2, confirm: true }); saved = { policy: { days: 2, maxEvents: 1000 }, revision: 3 }; return saved; } });
 render(<HistoryRetentionSection canManage />);
 const input = await screen.findByLabelText('Retention days (1–90)');
 await userEvent.clear(input); await userEvent.type(input, '2');
 await userEvent.click(screen.getByRole('button', { name: 'Review retention changes' }));
 expect(screen.getByText(/permanently discards/)).toBeTruthy();
 expect(backend.calls.filter(call => call.method === 'PUT')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button', { name: 'Cancel retention changes' }));
 await userEvent.click(screen.getByRole('button', { name: 'Review retention changes' }));
 await userEvent.click(screen.getByRole('button', { name: 'Confirm retention changes' }));
 expect(await screen.findByRole('status')).toBeTruthy();
 expect(backend.calls.filter(call => call.method === 'PUT')).toHaveLength(1);
});
it('rejects invalid limits and handles conflicts without silent retry', async () => {
 fakeApi({ 'GET /api/v1/blocked-history-retention': () => current,
 'PUT /api/v1/blocked-history-retention': () => apiError(409, 'revision conflict', 'conflict') });
 render(<HistoryRetentionSection canManage />);
 const input = await screen.findByLabelText('Retention days (1–90)');
 await userEvent.clear(input); await userEvent.type(input, '91');
 expect((screen.getByRole('button', { name: 'Review retention changes' }) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.clear(input); await userEvent.type(input, '3');
 await userEvent.click(screen.getByRole('button', { name: 'Review retention changes' }));
 await userEvent.click(screen.getByRole('button', { name: 'Confirm retention changes' }));
 expect(await screen.findByText(/Save could not be confirmed/)).toBeTruthy();
 expect(screen.queryByRole('button', { name: 'Confirm retention changes' })).toBeNull();
});
it('handles failed reads and clears a pending confirmation after role changes', async () => {
 let fail = true;
 fakeApi({ 'GET /api/v1/blocked-history-retention': () => fail ? apiError(500, 'storage unavailable', 'internal') : current });
 const view = render(<HistoryRetentionSection canManage />);
 expect(await screen.findByText('storage unavailable')).toBeTruthy();
 expect(screen.queryByLabelText('Retention days (1–90)')).toBeNull();
 fail = false;
 await userEvent.click(screen.getByRole('button', { name: 'Reload retention limits' }));
 await screen.findByLabelText('Retention days (1–90)');
 await userEvent.click(screen.getByRole('button', { name: 'Review retention changes' }));
 view.rerender(<HistoryRetentionSection canManage={false} />);
 expect(screen.queryByRole('button', { name: 'Confirm retention changes' })).toBeNull();
});
