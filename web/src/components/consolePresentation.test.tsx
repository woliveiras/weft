import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {Btn,ContextHelp,PageHeading} from './common';
afterEach(cleanup);
it('keeps optional help closed and lets native disclosure reveal its content',()=>{
 const {container}=render(<ContextHelp title="Usage guidance"><p>Additional explanation</p></ContextHelp>);
 const details=container.querySelector('details')!;
 expect(details.open).toBe(false);
 fireEvent.click(screen.getByText('Usage guidance'));
 expect(details.open).toBe(true);
 fireEvent.click(screen.getByText('Usage guidance'));
 expect(details.open).toBe(false);
});
it('keeps heading actions accessible without changing disabled behavior',()=>{
 const action=vi.fn();
 render(<PageHeading title="Models" description="Choose a provider"><Btn tone="primary" disabled onClick={action}>Add provider</Btn></PageHeading>);
 expect(screen.getByRole('heading',{level:1,name:'Models'})).toBeTruthy();
 fireEvent.click(screen.getByRole('button',{name:'Add provider'}));
 expect(action).not.toHaveBeenCalled();
 expect(screen.getByText('Choose a provider')).toBeTruthy();
});
