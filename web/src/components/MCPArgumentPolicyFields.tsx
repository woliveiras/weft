import type {MCPConnectionInput, MCPArgumentRule} from '../api/types';
import {Btn} from './common';

export function MCPArgumentPolicyFields({input,onChange}:{input:MCPConnectionInput;onChange:(value:MCPConnectionInput)=>void}) {
 const rules=input.argumentPolicy??[];
 const change=(next:MCPArgumentRule[])=>onChange({...input,argumentPolicy:next.length?next:undefined});
 const update=(index:number,patch:Partial<MCPArgumentRule>)=>change(rules.map((rule,i)=>i===index?{...rule,...patch}:rule));
 return <details open={rules.length>0||undefined}><summary>Tool argument restrictions</summary>
  <p>Optional exact matches for top-level string arguments. Once any rule is added, only listed tools are eligible. Every rule for a tool must match. Agent grants and approvals still apply.</p>
  <p>Use non-secret resource identifiers from the discovered schema. Values are case-sensitive, with no wildcards, URL resolution or project/space ownership checks. This does not filter returned content or limit query costs.</p>
  {rules.map((rule,index)=><fieldset key={index}><legend>Argument rule {index+1}</legend>
   <label>Remote tool name<input value={rule.tool} onChange={e=>update(index,{tool:e.target.value})} required maxLength={128}/></label>
   {rule.arguments ? <>
    <label>Argument names (one per line)<textarea required value={rule.arguments.join('\n')} onChange={e=>{
     const names=e.target.value.split('\n');
     update(index,{arguments:names,combinations:(rule.combinations??[]).map(row=>names.map((_,i)=>row[i]??''))});
    }}/></label>
    <p>Every value in a combination must match together. Values from different combinations cannot be mixed.</p>
    {(rule.combinations??[]).map((row,rowIndex)=><fieldset key={rowIndex}><legend>Allowed combination {rowIndex+1}</legend>
     {(rule.arguments??[]).map((name,column)=><label key={column}>{name||`Argument ${column+1}`}<input required maxLength={256} value={row[column]??''} onChange={e=>update(index,{combinations:(rule.combinations??[]).map((entry,i)=>i===rowIndex?entry.map((value,j)=>j===column?e.target.value:value):entry)})}/></label>)}
     <Btn onClick={()=>update(index,{combinations:(rule.combinations??[]).filter((_,i)=>i!==rowIndex)})}>Remove combination {rowIndex+1}</Btn>
    </fieldset>)}
    <Btn disabled={(rule.combinations?.length??0)>=32} onClick={()=>update(index,{combinations:[...(rule.combinations??[]),(rule.arguments??[]).map(()=>'')]})}>Add combination</Btn>
   </> : <>
   <label>Argument name<input value={rule.argument??''} onChange={e=>update(index,{argument:e.target.value})} required maxLength={128}/></label>
   <label>Allowed values (one per line)<textarea value={(rule.values??[]).join('\n')} onChange={e=>update(index,{values:e.target.value.split('\n')})} required/></label>
   </>}
   <Btn onClick={()=>change(rules.filter((_,i)=>i!==index))}>Remove rule {index+1}</Btn>
  </fieldset>)}
  <Btn disabled={rules.length>=64} onClick={()=>change([...rules,{tool:'',argument:'',values:[]}])}>Add argument rule</Btn>
  <Btn disabled={rules.length>=64} onClick={()=>change([...rules,{tool:'',arguments:['workspace','repo'],combinations:[['','']]}])}>Add combination rule</Btn>
  {rules.length>0&&<p>Saving policy changes clears discovery. Review the refreshed catalogue; tools with missing or non-string argument schemas are excluded.</p>}
 </details>;
}
