import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {WorkspaceHandoff,workspaceEditorLink} from './WorkspaceHandoff';
afterEach(cleanup);
it('encodes paths without allowing another URI authority, query or fragment',()=>{
 expect(workspaceEditorLink('/tmp/my work/#file?x')).toBe('vscode://file/tmp/my%20work/%23file%3Fx');
 for(const path of ['relative','//remote/path','/tmp/\nfile'])expect(workspaceEditorLink(path)).toBeUndefined();
});
it('requires an explicit same-computer confirmation and resets it for another workspace',async()=>{
 const {rerender}=render(<WorkspaceHandoff path="/tmp/one" nativeProfile/>);
 expect(screen.queryByRole('link',{name:'Open in VS Code'})).toBeNull();
 await userEvent.click(screen.getByRole('checkbox'));
 expect(screen.getByRole('link',{name:'Open in VS Code'}).getAttribute('href')).toBe('vscode://file/tmp/one');
 rerender(<WorkspaceHandoff path="/tmp/two" nativeProfile/>);
 expect(screen.queryByRole('link',{name:'Open in VS Code'})).toBeNull();
});
it('does not offer host editor links for a server/container profile',()=>{
 render(<WorkspaceHandoff path="/data/workspaces/task" nativeProfile={false}/>);
 expect(screen.queryByRole('checkbox')).toBeNull();
 expect(screen.queryByRole('link',{name:'Open in VS Code'})).toBeNull();
 expect(screen.getByText(/Container and remote server paths/)).toBeTruthy();
});
