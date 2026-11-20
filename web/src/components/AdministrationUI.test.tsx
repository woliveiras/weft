import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {AdminDraftContext,AdminReview,useAdminDraft} from './AdministrationUI';
afterEach(cleanup);
function Draft({dirty}:{dirty:boolean}){useAdminDraft('policy',dirty);return <p>Policy draft</p>;}
it('reports unsaved state, warns on reload and clears the parent guard on unmount',()=>{
 const report=vi.fn();const view=render(<AdminDraftContext.Provider value={report}><Draft dirty/></AdminDraftContext.Provider>);
 expect(report).toHaveBeenLastCalledWith('policy',{dirty:true,busy:false});
 const event=new Event('beforeunload',{cancelable:true});fireEvent(window,event);expect(event.defaultPrevented).toBe(true);
 view.unmount();expect(report).toHaveBeenLastCalledWith('policy',{dirty:false,busy:false});
});
it('moves keyboard focus into the exact review without submitting anything',()=>{
 render(<AdminReview label="Confirm policy"><h3>Review changes</h3></AdminReview>);
 expect(document.activeElement).toBe(screen.getByRole('region',{name:'Confirm policy'}));
});
