import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it } from 'vitest';
import { fakeApi, apiError } from '../test/fakeApi';
import { EnvironmentProposal } from './EnvironmentProposal';
afterEach(cleanup);
const path = '/api/v1/graphs/g/environment/proposal';
const pending = { id: 'apr', graphId: 'g', graphVersion: 1, state: 'awaiting_approval', resume: true, analysis: { environment: { image: 'node:22' }, rationale: 'Node matches package.json', tools: ['node'] } };

it('requests analysis with explicit resume intent and waits for separate approval', async () => {
 const client = fakeApi({ [`GET ${path}`]: () => null, [`POST ${path}`]: () => pending, [`POST ${path}/cancel`]: () => ({ ...pending, state: 'cancelled' }) });
 const user = userEvent.setup();
 render(<EnvironmentProposal graphId="g" disabled={false} />);
 await waitFor(() => expect((screen.getByRole('button', { name: 'Analyze repository' }) as HTMLButtonElement).disabled).toBe(false));
 await user.click(screen.getByRole('checkbox'));
 await user.click(screen.getByRole('button', { name: 'Analyze repository' }));
 expect(await screen.findByText('Node matches package.json')).toBeTruthy();
 expect(screen.getByText(/Open Approvals/)).toBeTruthy();
 expect(client.find('POST', path)[0].body).toEqual({ resume: true });
 expect(client.calls.some(c => c.path.endsWith('/build'))).toBe(false);
 await user.click(screen.getByRole('button', { name: 'Cancel proposal' }));
 expect(await screen.findByText('Environment proposal: cancelled')).toBeTruthy();
});

it('shows analysis failures without inventing a proposal or approval', async () => {
 fakeApi({ [`GET ${path}`]: () => null, [`POST ${path}`]: () => apiError(400, 'Model unavailable', 'invalid') });
 render(<EnvironmentProposal graphId="g" disabled={false} />);
 await waitFor(() => expect((screen.getByRole('button', { name: 'Analyze repository' }) as HTMLButtonElement).disabled).toBe(false));
 await userEvent.setup().click(screen.getByRole('button', { name: 'Analyze repository' }));
 expect(await screen.findByText('Model unavailable')).toBeTruthy();
 expect(screen.queryByText(/Open Approvals/)).toBeNull();
});


it('cancels an ongoing analysis and ignores its late response', async () => {
 let started = false;
 let finish!: (value: unknown) => void;
 const response = new Promise(resolve => { finish = resolve; });
 fakeApi({
  [`GET ${path}`]: () => started ? { ...pending, state: 'analyzing' } : null,
  [`POST ${path}`]: () => { started = true; return response; },
  [`POST ${path}/cancel`]: () => ({ ...pending, state: 'cancelled' }),
 });
 render(<EnvironmentProposal graphId="g" disabled={false} />);
 const user = userEvent.setup();
 await waitFor(() => expect((screen.getByRole('button', { name: 'Analyze repository' }) as HTMLButtonElement).disabled).toBe(false));
 await user.click(screen.getByRole('button', { name: 'Analyze repository' }));
 await user.click(await screen.findByRole('button', { name: 'Cancel proposal' }, { timeout: 3500 }));
 expect(await screen.findByText('Environment proposal: cancelled')).toBeTruthy();
 await act(async () => { finish(pending); await response; });
 expect(screen.getByText('Environment proposal: cancelled')).toBeTruthy();
 expect(screen.queryByText(/Open Approvals/)).toBeNull();
});
