import {cleanup, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, expect, it, vi} from 'vitest';
import type {ReactNode} from 'react';
import {App} from './App';
import {fakeApi} from './test/fakeApi';
import {THEME_STORAGE_KEY} from './theme';
vi.mock('./hooks/useEventStream',()=>({EventStreamProvider:({children}:{children:ReactNode})=>children,useEventStream:()=>({connected:false})}));
afterEach(()=>{cleanup();localStorage.clear();vi.restoreAllMocks();vi.unstubAllGlobals();delete document.documentElement.dataset.theme;});

it('shows the Weft brand and switches theme without calling the API',async()=>{
 localStorage.setItem('weftToken','test-token');location.hash='#/approvals';
 const backend=fakeApi({
  'GET /api/v1/info':()=>({identity:{name:'viewer',role:'viewer'}}),
  'GET /api/v1/graphs':()=>[],
  'GET /api/v1/approvals':()=>[],
 });
 render(<App/>);
 const brand=screen.getByRole('button',{name:'Weft'});
 expect(brand.querySelector('svg.brand-mark')).not.toBeNull();
 expect(brand.textContent).toBe('weft');
 await waitFor(()=>expect(backend.calls.length).toBeGreaterThan(0));
 const before=backend.calls.length;
 await userEvent.selectOptions(screen.getByRole('combobox',{name:'Theme'}),'dark');
 expect(document.documentElement.dataset.theme).toBe('dark');
 expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
 expect(backend.calls.slice(before).every(c=>c.method==='GET')).toBe(true);
 await userEvent.click(brand);
 await waitFor(()=>expect(location.hash).toBe('#/graphs'));
 expect(document.documentElement.dataset.theme).toBe('dark');
});
