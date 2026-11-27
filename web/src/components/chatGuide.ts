import type {ChatCandidate, ChatPickerArea} from '../api/chat';

export const primaryCategories = ['Tasks', 'Agents', 'Workflows'] as const;
export const moreCategories = ['Projects', 'Connections', 'Models', 'Skills & instructions'] as const;
export type ChatCategory = (typeof primaryCategories)[number] | (typeof moreCategories)[number];

export type GuideAction = {
  label: string;
  admin?: boolean;
} & (
  | {kind: 'pick'; area: ChatPickerArea; follow?: boolean}
  | {kind: 'project'; area: 'projects'}
  | {kind: 'prompt'; prompt: string; command?: string; detailsHint?:string}
  | {kind: 'link'; href: string}
);

const actions: Record<ChatCategory, GuideAction[]> = {
  Tasks: [
    {label:'Follow a task', kind:'pick', area:'tasks', follow:true},
    {label:'Find a task', kind:'pick', area:'tasks'},
    {label:'See running tasks', kind:'prompt', prompt:'List the tasks currently running.'},
  ],
  Agents: [
    {label:'Find a catalog agent', kind:'pick', area:'agents'},
    {label:'Create a catalog agent', kind:'prompt', prompt:'Prepare a new catalog agent for review.', detailsHint:'What should the agent be called, and what should it do?', admin:true},
  ],
  Workflows: [
    {label:'Find a workflow', kind:'pick', area:'workflows'},
    {label:'See active workflows', kind:'prompt', prompt:'Which workflows are active?'},
  ],
  Projects: [
    {label:'Open project registration', kind:'link', href:'#/projects/new', admin:true},
    {label:'Configure a project', kind:'project', area:'projects', admin:true},
    {label:'Find a project', kind:'pick', area:'projects'},
  ],
  Connections: [
    {label:'Inspect MCP connections', kind:'prompt', command:'/inspect mcp', prompt:'Inspect configured MCP connections.'},
    {label:'Inspect source control connections', kind:'prompt', command:'/inspect scm', prompt:'Inspect configured source control connections.'},
    {label:'Inspect issue connections', kind:'prompt', command:'/inspect issue-connections', prompt:'Inspect configured issue connections.'},
    {label:'Open connection settings', kind:'link', href:'#/connections', admin:true},
  ],
  Models: [
    {label:'View configured models', kind:'prompt', command:'/inspect models', prompt:'Which models are configured?'},
    {label:'Open model settings', kind:'link', href:'#/models', admin:true},
  ],
  'Skills & instructions': [
    {label:'Find a skill', kind:'pick', area:'skills'},
    {label:'Open Skills & instructions editor', kind:'link', href:'#/skills', admin:true},
  ],
};

export type GuideDraft = {more?:boolean; category?:ChatCategory; action?:string; query?:string; target?:ChatCandidate};

export function guideActions(category: ChatCategory, admin: boolean) {
  return actions[category].filter(action => !action.admin || admin);
}

export function guideAction(guide: GuideDraft, admin: boolean) {
  return guide.category && guideActions(guide.category, admin).find(action => action.label === guide.action);
}

export function guidedMessage(action: GuideAction | undefined, target: ChatCandidate | undefined, text: string) {
  let prefix: string;
  if (action?.kind === 'pick' && target) {
    prefix = action.follow ? '/follow ' + target.id : '/lookup ' + action.area + ' ' + target.id;
  } else if (action?.kind === 'prompt') {
    if (action.detailsHint && !text.trim()) return text;
    prefix = (!text.trim() && action.command) || action.prompt;
  } else {
    return text;
  }
  return text.trim() ? prefix + '\n' + text : prefix;
}
