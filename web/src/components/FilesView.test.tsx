import {act,cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {FilesView} from './FilesView';
import {fakeApi} from '../test/fakeApi';
import type {GraphSummary} from '../api/types';
afterEach(cleanup);
const summary=(id:string):GraphSummary=>({graph:{apiVersion:'weft.io/v1',kind:'Graph',metadata:{id,name:id},spec:{nodes:[]}},record:{id,version:1,desiredState:'stopped',state:'stopped',updatedAt:'2026-01-01T00:00:00Z'}});
const entry=(name:string)=>({name,path:name,dir:false,size:10,modTime:'2026-01-01T00:00:00Z'});
function routes(id:string){return {
 ['GET /api/v1/graphs/'+id+'/files']:()=>[entry('first.txt'),entry('second.txt')],
 ['GET /api/v1/graphs/'+id+'/git/status']:()=>({repository:false}),
 ['GET /api/v1/graphs/'+id+'/events']:()=>[],
 ['GET /api/v1/graphs/'+id+'/executions']:()=>[],
};}
it('keeps the latest selected file when earlier content arrives late and only reads',async()=>{
 let resolve!:(response:Response)=>void;
 let calls=0;
 const backend=fakeApi({...routes('a'),'GET /api/v1/graphs/a/files/content':()=>++calls===1?new Promise<Response>(r=>resolve=r):new Response('second content')});
 render(<FilesView summary={summary('a')}/>);
 await userEvent.click(await screen.findByRole('button',{name:/first.txt/}));
 await userEvent.click(screen.getByRole('button',{name:/second.txt/}));
 expect(await screen.findByText('second content')).toBeTruthy();
 await act(async()=>resolve(new Response('stale first content')));
 expect(screen.queryByText('stale first content')).toBeNull();
 expect(screen.getByText('second content')).toBeTruthy();
 expect(backend.calls.every(call=>call.method==='GET')).toBe(true);
 expect(screen.queryByRole('textbox')).toBeNull();
});
it('discards pending failures and contents from the previous graph',async()=>{
 let resolve!:(response:Response)=>void;
 fakeApi({...routes('a'),...routes('b'),'GET /api/v1/graphs/a/files/content':()=>new Promise<Response>(r=>resolve=r),'GET /api/v1/graphs/b/files/content':()=>new Response('graph B content')});
 const view=render(<FilesView summary={summary('a')}/>);
 await userEvent.click(await screen.findByRole('button',{name:/first.txt/}));
 view.rerender(<FilesView summary={summary('b')}/>);
 await userEvent.click(await screen.findByRole('button',{name:/second.txt/}));
 expect(await screen.findByText('graph B content')).toBeTruthy();
 await act(async()=>resolve(new Response(JSON.stringify({error:'graph A failure'}),{status:500,headers:{'Content-Type':'application/json'}})));
 expect(screen.queryByText('graph A failure')).toBeNull();
 expect(screen.getByText('graph B content')).toBeTruthy();
});
