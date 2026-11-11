import {cleanup, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, expect, it, vi} from 'vitest';
import type {ReactNode} from 'react';
import {App} from './App';
import {fakeApi, apiError} from './test/fakeApi';
vi.mock('./hooks/useEventStream',()=>({EventStreamProvider:({children}:{children:ReactNode})=>children,useEventStream:()=>({connected:false})}));
afterEach(()=>{cleanup();localStorage.clear();vi.restoreAllMocks();vi.unstubAllGlobals();});

it('keeps graph load failures recoverable through grouped navigation without mutations',async()=>{
 localStorage.setItem('weftToken','test-token');location.hash='#/graphs';
 const backend=fakeApi({
  'GET /api/v1/info':()=>({identity:{name:'viewer',role:'viewer'}}),
  'GET /api/v1/graphs':()=>apiError(503,'Graphs unavailable','unavailable'),
  'GET /api/v1/approvals':()=>[],
 });
 render(<App/>);
 await screen.findByText('Graphs unavailable');
 await userEvent.click(screen.getByRole('button',{name:'Approvals'}));
 await waitFor(()=>expect(location.hash).toBe('#/approvals'));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Approvals'}).getAttribute('aria-current')).toBe('page'));
 await userEvent.click(screen.getByRole('button',{name:'Workflows'}));
 await screen.findByText('Graphs unavailable');
 expect(screen.getByRole('button',{name:'Workflows'}).getAttribute('aria-current')).toBe('page');
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
 expect(backend.calls.some(c=>c.path.includes('/tasks')||c.path.includes('/projects'))).toBe(false);
});
