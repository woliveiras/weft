import {cleanup,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {ReactNode} from 'react';
import {App} from './App';
import {fakeApi,apiError} from './test/fakeApi';
vi.mock('./hooks/useEventStream',()=>({EventStreamProvider:({children}:{children:ReactNode})=>children,useEventStream:()=>({connected:false})}));
afterEach(()=>{cleanup();localStorage.clear();vi.restoreAllMocks();vi.unstubAllGlobals();});
it.each([null,'{',JSON.stringify({version:1,current:4,reviewed:['concepts']})])('opens graphs without reading or mutating legacy guide progress: %s',async(progress)=>{
 localStorage.setItem('weftToken','test-token');const key='weft.guide.v1:["operator","operator"]';if(progress!==null)localStorage.setItem(key,progress);
 location.hash='#/graphs';const reads=vi.spyOn(Storage.prototype,'getItem');
 const backend=fakeApi({'GET /api/v1/info':()=>({identity:{name:'operator',role:'operator'}}),'GET /api/v1/graphs':()=>[],'GET /api/v1/approvals':()=>[]});
 render(<App/>);await screen.findByRole('button',{name:'New graph'});
 expect(screen.queryByRole('region',{name:'Getting started'})).toBeNull();expect(screen.queryByRole('button',{name:'Getting started'})).toBeNull();
 expect(reads.mock.calls.some(([key])=>key.startsWith('weft.guide.'))).toBe(false);
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);expect(backend.calls.some(c=>c.path==='/api/v1/providers'||c.path==='/api/v1/mcp/connections')).toBe(false);
 expect(localStorage.getItem(key)).toBe(progress);
});
it('keeps dependency errors visible without a guide intercepting the screen',async()=>{
 localStorage.setItem('weftToken','test-token');location.hash='#/graphs';
 fakeApi({'GET /api/v1/info':()=>({identity:{name:'viewer',role:'viewer'}}),'GET /api/v1/graphs':()=>apiError(503,'Graphs unavailable','unavailable'),'GET /api/v1/approvals':()=>[]});
 render(<App/>);expect(await screen.findByText('Graphs unavailable')).toBeTruthy();expect(screen.queryByRole('region',{name:'Getting started'})).toBeNull();expect(screen.getByRole('navigation',{name:'Main navigation'})).toBeTruthy();
});
