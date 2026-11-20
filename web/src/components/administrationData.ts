import type {EnvironmentCleanupItem,NetworkPolicyView,ToolInfo} from '../api/types';
export function filterAdminTools(items:ToolInfo[],query:string,capability:string) {
 const term=query.trim().toLowerCase();return items.filter(item=>(!capability||item.capability===capability)&&`${item.name} ${item.description} ${item.capability}`.toLowerCase().includes(term)).sort((a,b)=>a.name.localeCompare(b.name));
}
export function filterNetworkUsage(items:NonNullable<NetworkPolicyView['usage']>,query:string,status:string,kind:string) {
 const term=query.trim().toLowerCase();return items.filter(item=>`${item.graph} ${item.reason??''} ${item.hosts.join(' ')}`.toLowerCase().includes(term)&&(status==='all'||item.blocked===(status==='blocked'))&&(kind==='all'||item.graph.startsWith('task-run-')===(kind==='execution'))).sort((a,b)=>Number(b.blocked)-Number(a.blocked)||a.graph.localeCompare(b.graph));
}
export function resourceState(item:EnvironmentCleanupItem) {return item.error?'error':item.usedBy?.length?'in-use':item.eligible&&!item.missing&&!item.removed?'unused':'retained';}
export function filterAdminResources(items:EnvironmentCleanupItem[],query:string,status:string) {
 const term=query.trim().toLowerCase();return items.filter(item=>`${item.id} ${item.image??''} ${(item.usedBy??[]).join(' ')} ${(item.caches??[]).map(cache=>cache.volume+' '+cache.path).join(' ')}`.toLowerCase().includes(term)&&(status==='all'||resourceState(item)===status)).sort((a,b)=>a.id.localeCompare(b.id));
}
export function changedHosts(before:string[]=[],after:string[]=[]) {return {added:after.filter(value=>!before.includes(value)),removed:before.filter(value=>!after.includes(value))};}
