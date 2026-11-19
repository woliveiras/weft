import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { GraphSummary } from '../api/types';
import { EventStreamProvider } from '../hooks/useEventStream';
import { apiError, fakeApi } from '../test/fakeApi';
import { NodePanel } from './NodePanel';

class FakeEventSource { addEventListener() {} close() {} }
const summary: GraphSummary = {
  graph: {apiVersion:'weft.io/v1',kind:'Graph',metadata:{id:'g',name:'G',version:1},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'}}]}},
  record:{id:'g',version:1,state:'stopped',desiredState:'stopped',updatedAt:'2026-11-19T08:00:00Z'},
};
beforeEach(()=>vi.stubGlobal('EventSource',FakeEventSource));
afterEach(cleanup);
function panel(props: {isSaved?:boolean;canControl?:boolean}={}) {
  return render(<EventStreamProvider token="test"><NodePanel summary={summary} nodeId="worker" onSaved={()=>undefined} onStatus={()=>undefined} {...props}/></EventStreamProvider>);
}
it('loads output only when requested and recovers from a failed history request',async()=>{
  const user=userEvent.setup(); let attempts=0;
  const routes=fakeApi({
    'GET /api/v1/tools':()=>[],
    'GET /api/v1/graphs/g/events':()=>++attempts===1 ? apiError(503,'History unavailable','unavailable') : [],
  });
  panel();
  expect(routes.find('GET','/api/v1/graphs/g/events')).toHaveLength(0);
  expect(screen.queryByLabelText('Operator message')).toBeNull();
  await user.click(screen.getByRole('button',{name:'Output'}));
  expect(await screen.findByText(/History unavailable/)).toBeTruthy();
  expect(screen.queryByText('No output yet.')).toBeNull();
  await user.click(screen.getByRole('button',{name:'Retry output'}));
  expect(await screen.findByText('No output yet.')).toBeTruthy();
  expect(attempts).toBe(2);
});
it('shows loading rather than an empty history while output is pending',async()=>{
  const user=userEvent.setup(); let resolve!: (value: unknown[])=>void;
  fakeApi({'GET /api/v1/tools':()=>[], 'GET /api/v1/graphs/g/events':()=>new Promise<unknown[]>(done=>{resolve=done;})});
  panel(); await user.click(screen.getByRole('button',{name:'Output'}));
  expect(screen.getByText('Loading output…')).toBeTruthy();
  expect(screen.queryByText('No output yet.')).toBeNull();
  resolve([]); await screen.findByText('No output yet.');
});
it('does not query unavailable runtime history for an unsaved node',async()=>{
  const user=userEvent.setup(); const routes=fakeApi({'GET /api/v1/tools':()=>[]}); panel({isSaved:false});
  expect(screen.getByRole('button',{name:'Start'})).toHaveProperty('disabled',true);
  for(const name of ['Output','Messages','Runs']) await user.click(screen.getByRole('button',{name}));
  expect(screen.getByText(/This agent is in the draft/)).toBeTruthy();
  expect(routes.calls.every(call=>call.path==='/api/v1/tools')).toBe(true);
});
it('keeps viewer activity readable without exposing editing or send controls',async()=>{
  const user=userEvent.setup(); fakeApi({'GET /api/v1/tools':()=>[],'GET /api/v1/graphs/g/nodes/worker/chat':()=>[]}); panel({canControl:false});
  expect(screen.getByRole('button',{name:'Start'})).toHaveProperty('disabled',true);
  await user.click(screen.getByRole('button',{name:'Configuration'}));
  expect(screen.queryByRole('button',{name:'Edit'})).toBeNull();
  await user.click(screen.getByRole('button',{name:'Messages'}));
  await waitFor(()=>expect(screen.getByText('No operator messages yet.')).toBeTruthy());
  expect(screen.queryByLabelText('Operator message')).toBeNull();
});
