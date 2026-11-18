import type { GraphNode } from './types';

export interface AgentParameter {
  name: string;
  description?: string;
  default?: string;
  required?: boolean;
}

export interface AgentBinding {
  id: string;
  version: number;
  parameters?: Record<string, string>;
}

export interface AgentDefinition extends Pick<GraphNode, 'name' | 'description' | 'runtime' | 'model' | 'prompt' | 'tools' | 'isolation' | 'skillIds' | 'repositoryGuidance'> {
  name: string;
  readOnly?: boolean;
  parameters?: AgentParameter[];
}

export interface AgentVersion {
  version: number;
  definition: AgentDefinition;
  createdAt: string;
  createdBy: string;
}

export interface AgentOrigin {
  workflow: string;
  node: string;
}

export interface Agent {
  id: string;
  versions: AgentVersion[];
  origin?: AgentOrigin;
}

export interface AgentSummary {
  id: string;
  name: string;
  description?: string;
  latest: number;
  origin?: AgentOrigin;
}

export function workflowAgentLink(workflow: string, node: string) {
  return `#/g/${encodeURIComponent(workflow)}/agent?${new URLSearchParams({agent:node, section:'general'})}`;
}

export function nodeWithAgent(node: GraphNode, binding: AgentBinding, definition: AgentDefinition): GraphNode {
  const {readOnly, parameters:_parameters, ...fields} = definition;
  return {
    ...node, ...fields, agent: binding, tools: definition.tools ?? [],
    description: definition.description, model: definition.model, isolation: definition.isolation,
    prompt: definition.prompt, skillIds: definition.skillIds, repositoryGuidance: definition.repositoryGuidance,
    workspace: {...node.workspace, readOnly: node.workspace?.readOnly || readOnly},
  };
}

export function nodeWithAgentVersion(node: GraphNode, agent: Agent, version: number): GraphNode {
  const binding = node.agent;
  if (!binding || agent.id !== binding.id || binding.version === version) return node;
  const selected = agent.versions.find(candidate => candidate.version === version);
  if (!selected) return node;
  const declared = new Set(selected.definition.parameters?.map(parameter => parameter.name));
  const parameters = Object.fromEntries(Object.entries(binding.parameters ?? {}).filter(([name]) => declared.has(name)));
  return nodeWithAgent(node, {...binding, version, parameters}, selected.definition);
}
