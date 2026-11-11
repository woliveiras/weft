import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it } from 'vitest';
import { fakeApi, apiError } from '../test/fakeApi';
import { GitView } from './GitView';
afterEach(cleanup);
const result = { repository: true, directory: '/fixture/project', branch: 'main', content: ' M result.txt', attribution: 'inconclusive' };
it('inspects status, diff and history read-only with execution context', async () => {
 const backend = fakeApi({
  'GET /api/v1/graphs/g/events': () => [],
 'GET /api/v1/graphs/g/git/status': () => result,
  'GET /api/v1/graphs/g/git/diff': () => ({ ...result, content: '+updated result' }),
  'GET /api/v1/graphs/g/git/history': () => ({ ...result, content: 'abc123 baseline' }),
  'GET /api/v1/graphs/g/executions': () => [{ id: 'exec-fixture', nodeId: 'worker', replica: 0, status: 'succeeded', summary: 'Task done' }],
 });
 render(<GitView graphId="g" node="worker" replica={0} path="result.txt" />);
 expect(await screen.findByText('M result.txt')).toBeTruthy();
 expect(screen.getByText(/Change attribution is inconclusive/)).toBeTruthy();
 expect(screen.getByText(/Task done/)).toBeTruthy();
 await userEvent.selectOptions(screen.getByLabelText('Git view'), 'diff');
 expect(await screen.findByText('+updated result')).toBeTruthy();
 await userEvent.selectOptions(screen.getByLabelText('Git view'), 'history');
 expect(await screen.findByText('abc123 baseline')).toBeTruthy();
 expect(backend.calls.every(call => call.method === 'GET')).toBe(true);
 expect(String(backend.fetch.mock.calls[0][0])).toContain('replica=0');
});
it('shows Git failures and allows refresh; distinguishes non-repositories', async () => {
 let failure = true;
 fakeApi({ 'GET /api/v1/graphs/g/events': () => [],
 'GET /api/v1/graphs/g/git/status': () => failure ? apiError(500,'Git unavailable','internal') : { ...result, repository: false }, 'GET /api/v1/graphs/g/executions': () => [] });
 render(<GitView graphId="g" node="" replica={0} path="" />);
 expect(await screen.findByText('Git unavailable')).toBeTruthy();
 failure = false;
 await userEvent.click(screen.getByRole('button', {name:'Refresh Git'}));
 expect(await screen.findByText(/Workspace has no Git repository/)).toBeTruthy();
});
it('renders shell observations and incomplete scans with execution identity', async () => {
 fakeApi({
  'GET /api/v1/graphs/g/events': () => [{id:'observation',type:'workspace.observed',executionId:'exec-shell',nodeId:'worker',replica:0,data:{tool:'shell.exec',status:'partial',changes:[{path:'result.txt',operation:'modified'}]}}],
  'GET /api/v1/graphs/g/git/status': () => result,
  'GET /api/v1/graphs/g/executions': () => [],
 });
 render(<GitView graphId="g" node="worker" replica={0} path="result.txt" />);
 expect(await screen.findByText(/result.txt \(modified\)/)).toBeTruthy();
 expect(screen.getByText(/observed · partial/)).toBeTruthy();
 expect(screen.getByText('exec-shell')).toBeTruthy();
});
it('includes shared monitor events in a selected agent view without assigning authorship', async () => {
 fakeApi({
  'GET /api/v1/graphs/g/events': () => [{id:'external',type:'workspace.observed',data:{source:'monitor',status:'complete',changes:[{path:'result.txt',operation:'modified'}]}}],
  'GET /api/v1/graphs/g/git/status': () => result,
  'GET /api/v1/graphs/g/executions': () => [],
 });
 render(<GitView graphId="g" node="worker" replica={0} path="result.txt" />);
 expect(await screen.findByText(/workspace monitor/)).toBeTruthy();
 expect(screen.getByText(/author unknown/)).toBeTruthy();
 expect(screen.getByText(/execution unknown/)).toBeTruthy();
});
it('renders kernel process evidence and transient deletion from the API', async () => {
 fakeApi({
  'GET /api/v1/graphs/g/events': () => [{id:'kernel',type:'workspace.observed',data:{source:'kernel-journal',status:'complete',process:{pid:23,start:'100'},changes:[{path:'transient',operation:'deleted'}]}}],
  'GET /api/v1/graphs/g/git/status': () => result,
  'GET /api/v1/graphs/g/executions': () => [],
 });
 render(<GitView graphId="g" node="" replica={0} path="" />);
 expect(await screen.findByText(/kernel PID 23/)).toBeTruthy();
 expect(screen.getByText(/transient \(deleted\)/)).toBeTruthy();
 expect(screen.getByText(/execution unknown/)).toBeTruthy();
});
