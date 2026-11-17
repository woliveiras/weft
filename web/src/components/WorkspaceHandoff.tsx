import {useState} from 'react';

// An editor link is a user action, not proof that VS Code is installed or that
// a loopback connection is not a tunnel to another computer.
export function workspaceEditorLink(path:string): string|undefined {
  if(!path.startsWith('/')||path.startsWith('//')||/[\u0000-\u001f\u007f]/.test(path))return undefined;
  return `vscode://file${path.split('/').map(encodeURIComponent).join('/')}`;
}
export function WorkspaceHandoff({path,nativeProfile}:{path:string;nativeProfile:boolean}) {
  const [confirmedPath,setConfirmedPath]=useState<string>();
  const loopback=['localhost','127.0.0.1','[::1]'].includes(window.location.hostname);
  const href=nativeProfile&&loopback?workspaceEditorLink(path):undefined;
  return <section aria-label="Review workspace">
    <h3>Review changes in your editor</h3>
    <p>Task workspace on {nativeProfile?'the native Weft host':'the Weft server'}: <code>{path}</code></p>
    {href?<>
      <label><input type="checkbox" checked={confirmedPath===path} onChange={e=>setConfirmedPath(e.target.checked?path:undefined)}/>Weft and this browser are on the same computer, with VS Code installed.</label>
      {confirmedPath===path&&<a className="btn" href={href}>Open in VS Code</a>}
      <p>If the browser is connected through a tunnel, open the path on the Weft host instead. Your browser may ask to open VS Code.</p>
    </>:<p>Open this path on the Weft host. Container and remote server paths may differ from paths on your computer.</p>}
    <p>Review the diff and test evidence before your own commit or push. Worktrees separate files; they do not isolate process or network access. <a href="https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/" target="_blank" rel="noreferrer">About worktrees</a></p>
  </section>;
}
