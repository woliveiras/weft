import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { MCPConnectionInput, MCPTemplate } from '../api/types';
import { useAsync } from '../hooks/useData';
import { Btn, ErrorBox } from './common';
import { templateDraft, templateEndpoint } from './mcpTemplateDefaults';

export function MCPTemplateSettings({input,onChange}:{input:MCPConnectionInput;onChange:(input:MCPConnectionInput)=>void}) {
  const catalogue=useAsync(api.mcpTemplates,[]);
  const [preview,setPreview]=useState<MCPTemplate>();
  const [error,setError]=useState<string>();
  const pinned=catalogue.data?.find(t=>t.id===input.template?.id&&t.version===input.template.version);
  const latest=catalogue.data?.filter(t=>t.id===input.template?.id).reduce<MCPTemplate|undefined>((a,b)=>!a||b.version>a.version?b:a,undefined);
  function apply(template:MCPTemplate,parameters:Record<string,string>={}) {
    try { onChange(templateDraft(input,template,parameters));setError(undefined);setPreview(undefined); }
    catch(e){setError(errorMessage(e));}
  }
  return <section aria-label="Pinned template settings">
    <p>Template: {input.template?.id} v{input.template?.version}. Changes remain in this form until you save; agent permissions are unchanged.</p>
    <ErrorBox error={error??catalogue.error}/>
    {catalogue.error&&<Btn onClick={()=>void catalogue.reload()}>Reload template settings</Btn>}
    {pinned&&<>
      <p>Authentication: {pinned.authentication}. {pinned.guidance}</p>
      {(pinned.parameters??[]).map(parameter=><TemplateParameter key={`${pinned.id}-${pinned.version}-${parameter.name}`} label={parameter.label} inputType={parameter.inputType} value={input.templateParameters?.[parameter.name]??parameter.default} options={parameter.options} onApply={value=>apply(pinned,{...input.templateParameters,[parameter.name]:value})}/>)}
      {latest&&latest.version>pinned.version&&<Btn onClick={()=>{setPreview(latest);setError(undefined);}}>Review template update to v{latest.version}</Btn>}
    </>}
    {preview&&<section role="dialog" aria-label="Review template update">
      <h4>{preview.name}: v{input.template?.version} → v{preview.version}</h4>
      <dl><dt>Current endpoint</dt><dd>{input.endpoint}</dd><dt>Proposed endpoint</dt><dd>{templateEndpoint(preview)}</dd><dt>Supported authentication</dt><dd>{preview.authentication}</dd></dl>
      <p>{preview.guidance}</p><p>{preview.validation}</p>
      <p>Your authentication mode, client configuration and scopes stay unchanged. Endpoint changes require reviewing credentials and authorization. No tools are granted. Cancel keeps your current draft.</p>
      <Btn onClick={()=>apply(preview)}>Apply update to draft</Btn><Btn onClick={()=>setPreview(undefined)}>Cancel template update</Btn>
    </section>}
    <Btn onClick={()=>onChange({...input,template:undefined,templateParameters:undefined})}>Customize without template</Btn>
  </section>;
}
function TemplateParameter({label,value,options,inputType,onApply}:{label:string;inputType?:'url'|'text';value:string;options?:string[];onApply:(value:string)=>void}) {
  const [draft,setDraft]=useState(value);
  return <div><label>{label}{options?<select aria-label={label} value={draft} onChange={e=>setDraft(e.target.value)}>{options.map(option=><option key={option}>{option}</option>)}</select>:<input type={inputType??"url"} value={draft} onChange={e=>setDraft(e.target.value)}/>}</label><Btn onClick={()=>onApply(draft)}>Apply {label}</Btn></div>;
}
