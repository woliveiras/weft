import {Activity} from 'react';
import {cleanup,render,waitFor} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {GraphCanvas} from './GraphCanvas';
import type {Graph} from '../../api/types';

beforeEach(()=>{
 vi.stubGlobal('DOMMatrixReadOnly',class {m22=1;});
 vi.spyOn(HTMLElement.prototype,'offsetWidth','get').mockReturnValue(300);
 vi.spyOn(HTMLElement.prototype,'offsetHeight','get').mockReturnValue(160);
 vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockReturnValue({x:0,y:0,width:300,height:160,top:0,left:0,right:300,bottom:160,toJSON:()=>({})});
 vi.stubGlobal('ResizeObserver',class {
  targets=new Set<Element>();
  constructor(private callback:ResizeObserverCallback){}
  observe(target:Element){this.targets.add(target);queueMicrotask(()=>{if(this.targets.has(target))this.callback([{target,contentRect:target.getBoundingClientRect()} as ResizeObserverEntry],this as unknown as ResizeObserver);});}
  unobserve(target:Element){this.targets.delete(target);} disconnect(){this.targets.clear();}
 });
});
afterEach(cleanup);
it('keeps channel paths after structure rebuild and cached workspace reveal',async()=>{
 const graph:Graph={apiVersion:'weft.io/v1',kind:'Graph',metadata:{id:'canvas',name:'Canvas',version:1},spec:{nodes:[{id:'a',runtime:{kind:'echo'},position:{x:0,y:0}},{id:'b',runtime:{kind:'echo'},position:{x:500,y:0}}],channels:[{id:'work',from:'a',to:['b']}]}};
 const show=(visible:boolean,value:Graph)=><Activity mode={visible?'visible':'hidden'}><GraphCanvas graph={value} selection={null} onSelect={()=>{}}/></Activity>;
 const view=render(show(true,graph));
 const check=async()=>waitFor(()=>{
  expect(view.container.querySelectorAll('.react-flow__edge')).toHaveLength(1);
  expect(view.container.querySelector('.react-flow__edge-path')?.getAttribute('d')).not.toContain('NaN');
 });
 await check();
 const revised={...graph,metadata:{...graph.metadata,version:2}};
 view.rerender(show(true,revised));await check();
 view.rerender(show(false,revised));view.rerender(show(true,revised));await check();
 expect(view.container.querySelectorAll('.react-flow__node')).toHaveLength(2);
 expect(revised.spec.channels).toEqual(graph.spec.channels);
 view.rerender(show(true,{...revised,spec:{...revised.spec,channels:[]}}));
 await waitFor(()=>expect(view.container.querySelectorAll('.react-flow__edge')).toHaveLength(0));
 view.rerender(show(true,revised));await check();
});
