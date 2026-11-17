import type {MCPConnectionInput} from '../api/types';
export function MCPAtlassianPolicyFields({input,onChange}:{input:MCPConnectionInput;onChange:(value:MCPConnectionInput)=>void}) {
 const policy=input.atlassianPolicy;
 const applicable=['atlassian','jira','confluence','bitbucket'].includes(input.service)||/^https:\/\/mcp\.atlassian\.com\//i.test(input.endpoint);
 if(!applicable&&!policy)return null;
 return <fieldset><legend>Atlassian read boundaries</legend>
  <label><input type="checkbox" checked={!!policy} onChange={e=>onChange({...input,atlassianPolicy:e.target.checked?{products:['jira'],cloudIds:[]}:undefined})}/>Restrict Atlassian products and sites</label>
  {policy&&<>
   <p>Only six reviewed Jira and Confluence read operations are eligible. Unknown operations, generic search, dispatchers, writes and Bitbucket are excluded. Agent grants remain required. This does not restrict individual projects or spaces within an allowed site.</p>
   {(['jira','confluence'] as const).map(product=><label key={product}><input type="checkbox" checked={policy.products.includes(product)} onChange={e=>onChange({...input,atlassianPolicy:{...policy,products:e.target.checked?[...policy.products,product]:policy.products.filter(p=>p!==product)}})}/>{product==='jira'?'Allow Jira reads':'Allow Confluence reads'}</label>)}
   <label>Allowed Atlassian cloud IDs (one per line)<textarea required value={policy.cloudIds.join('\n')} onChange={e=>onChange({...input,atlassianPolicy:{...policy,cloudIds:e.target.value.split('\n')}})}/></label>
   <p>Use exact site UUIDs confirmed by your Atlassian administrator, not site URLs. Saving changes clears discovered tools; review discovery again.</p>
  </>}
 </fieldset>;
}
