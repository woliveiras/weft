import type { Graph, GraphNode } from '../api/types';
import type { AgentDefinition } from '../api/agents';
import { effectiveLoopMode } from './canvas/model';
export const agentSections = ['general', 'instructions', 'tools', 'advanced'] as const;
export type AgentSection = typeof agentSections[number];
export const agentSectionTitles: Record<AgentSection, string> = { general:'General', instructions:'Instructions', tools:'Tools & permissions', advanced:'Advanced' };
export function agentSection(value?: string | null): AgentSection {
  return agentSections.includes(value as AgentSection) ? value as AgentSection : 'general';
}
/** Mirror graph default resolution; an explicit isolation mode replaces the default block. */
export function agentBoundaries(graph: Graph, node: GraphNode) {
  const isolation = node.agent || node.isolation?.mode ? node.isolation : graph.spec.defaults?.isolation;
  return { isolation: isolation?.mode || 'process', network: graph.spec.environment?.network?.run ?? isolation?.network ?? 'none',
    readOnly: !!node.workspace?.readOnly, tools: node.tools ?? (node.agent ? [] : graph.spec.defaults?.tools ?? []), loop: effectiveLoopMode(graph,node) };
}
/** List every changed document path without copying potentially sensitive field values. */
export function changedGraphFields(before: unknown, after: unknown, path = ''): string[] {
  if (Object.is(before, after)) return [];
  if (before && after && typeof before === 'object' && typeof after === 'object' && Array.isArray(before) === Array.isArray(after)) {
    const left = before as Record<string, unknown>, right = after as Record<string, unknown>;
    return [...new Set([...Object.keys(left), ...Object.keys(right)])].sort().flatMap(key => changedGraphFields(left[key], right[key], Array.isArray(before) ? `${path}[${key}]` : path ? `${path}.${key}` : key));
  }
  return [path || 'document'];
}

export function agentDefinitionChanged(initial: AgentDefinition, draft: AgentDefinition): boolean {
  // Match the catalog's persisted representation for optional values cleared in the form.
  const comparable = (definition: AgentDefinition): AgentDefinition => ({
    ...definition,
    description: definition.description || undefined,
    prompt: definition.prompt || undefined,
    readOnly: definition.readOnly || undefined,
    tools: definition.tools?.length ? definition.tools : undefined,
    parameters: definition.parameters?.length ? definition.parameters.map(parameter => ({
      ...parameter,
      description: parameter.description || undefined,
      default: parameter.default || undefined,
      required: parameter.required || undefined,
    })) : undefined,
  });
  return changedGraphFields(comparable(initial), comparable(draft)).length > 0;
}
