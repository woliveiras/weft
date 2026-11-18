import { HelpField } from './FieldHelp';
import type { MCPLocalProcess } from '../api/types';

export function parseProcessEnvironment(value:string):Record<string,string>|undefined {
  if(!value.trim())return undefined;
  let parsed:unknown;
  try{parsed=JSON.parse(value);}catch{throw new Error('Environment must be a JSON object with string values.');}
  if(!parsed||Array.isArray(parsed)||typeof parsed!=='object'||Object.entries(parsed).some(([key,item])=>!/^[_a-zA-Z][_a-zA-Z0-9]*$/.test(key)||typeof item!=='string'))throw new Error('Environment must be a JSON object with valid names and string values.');
  return parsed as Record<string,string>;
}

export function LocalMCPFields({value,onChange,environment,onEnvironment,reviewed,onReview,storedKeys}:{value:MCPLocalProcess;onChange:(value:MCPLocalProcess)=>void;environment:string;onEnvironment:(value:string)=>void;reviewed:boolean;onReview:(value:boolean)=>void;storedKeys?:string[]}) {
  return <fieldset><legend>Local MCP process</legend>
    <p>Runs on the native Weft host as your OS user. This is not a sandbox. Install the server yourself, then review its executable and arguments. Saving an enabled connection starts this process to discover tools.</p>
    <HelpField label={"Executable path"} help={"The server-side executable for this stdio MCP connection. It runs directly on the Weft host under the host\u2019s Weft user. Use only a reviewed executable."}><input required value={value.command} onChange={e=>onChange({...value,command:e.target.value})}/></HelpField>
    <HelpField label={"Working directory"} help={"The directory, on the Weft host, where the MCP process starts. Use a server-local absolute path."}><input required value={value.directory} onChange={e=>onChange({...value,directory:e.target.value})}/></HelpField>
    <HelpField label={"Process arguments"} help={"Arguments passed directly to the executable, one per line. Add flags as separate lines; shell expansion does not apply."}><textarea value={value.args.join('\n')} onChange={e=>onChange({...value,args:e.target.value?e.target.value.split('\n'):[]})}/></HelpField>
    <p>Use absolute paths and one argument per line. No shell expansion occurs. Keep credentials out of arguments.</p>
    <HelpField label={"Environment values (JSON)"} help={"Use a JSON object of environment variable names and string values passed to the local process. A saved environment is not displayed again."}><input type="password" autoComplete="new-password" value={environment} onChange={e=>onEnvironment(e.target.value)}/></HelpField>
    <p>No environment variables are inherited. Supply required values explicitly, including PATH if needed. Values are encrypted and never returned. Blank keeps stored values; {'{}'} clears them. Changing the process requires replacing or clearing stored values.</p>
    {!!storedKeys?.length&&<p>Stored variable names: {storedKeys.join(', ')}</p>}
    <label><input type="checkbox" checked={reviewed} onChange={e=>onReview(e.target.checked)}/>I reviewed this process and allow it to run on the Weft host.</label>
    <p>Uses session-based MCP 2025-06-18. A fresh process is started for each discovery or call and stopped afterward.</p>
    <p>Discovered tools still require explicit permission for each agent.</p>
  </fieldset>;
}
