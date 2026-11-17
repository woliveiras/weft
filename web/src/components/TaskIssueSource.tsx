import type {TaskInput} from '../api/tasks';
import type {ToolGrant} from '../api/types';

export function issueConnections(grants:(ToolGrant|string)[],kind:string) {
 const suffix=kind==='jira'?'getJiraIssue':'issue_read';
 return [...new Set(grants.flatMap(g=>{
  const match=/^mcp\.([a-z][a-z0-9_-]{0,63})\.([A-Za-z0-9_-]+)$/.exec(typeof g==='string'?g:g.name);
  return match&&match[2]===suffix?[match[1]]:[];
 }))];
}
export function TaskIssueSource({source,grants,onChange}:{source:TaskInput['source'];grants:(ToolGrant|string)[];onChange:(source:TaskInput['source'])=>void}) {
 const connections=issueConnections(grants,source?.kind??'github');
 return <fieldset><legend>Issue source (optional)</legend>
  <label>Issue service<select value={source?.kind??''} onChange={e=>onChange(e.target.value?{kind:e.target.value as 'github'|'jira',connectionId:'',reference:''}:undefined)}><option value="">No issue source</option><option value="github">GitHub</option><option value="jira">Jira</option></select></label>
  {source&&<>
   <label>Issue connection<select required value={source.connectionId} onChange={e=>onChange({...source,connectionId:e.target.value})}><option value="">Select granted connection</option>{source.connectionId&&!connections.includes(source.connectionId)&&<option value={source.connectionId}>{source.connectionId} (grant unavailable)</option>}{connections.map(id=><option key={id} value={id}>{id}</option>)}</select></label>
   <label>Issue reference<input required maxLength={512} placeholder={source.kind==='github'?'owner/repository#123 or GitHub issue URL':'PROJECT-123 or Jira issue URL'} value={source.reference} onChange={e=>onChange({...source,reference:e.target.value})}/></label>
   {source.kind==='jira'&&<><label>Jira cloud ID<input required value={source.cloudId??''} onChange={e=>onChange({...source,cloudId:e.target.value})}/></label><p>Use the cloud UUID of the site allowed by this connection. The URL is a reference; its hostname does not verify the selected cloud ID.</p></>}
   <p>Only exact read-tool grants on the entry agent appear here. Saving does not fetch the issue or grant access. The agent reads it during execution through the gateway; approvals and connection policies still apply. Issue content is untrusted data.</p>
   {!connections.length&&<p>Grant {source.kind==='github'?'issue_read':'getJiraIssue'} to the entry agent, then return to this draft. <a href="#/connections" target="_blank" rel="noopener noreferrer">Review connections in another tab</a>.</p>}
  </>}
 </fieldset>;
}
