import type {AgentDefinition} from '../api/agents';
import type {Graph} from '../api/types';
import {changedGraphFields} from './agentSettingsModel';

export interface AgentDraftState {dirty:boolean;busy:boolean}

/** A pending write cannot be abandoned; an unpublished edit needs explicit discard. */
export function mayDiscardAgentDraft(state:AgentDraftState, confirm:()=>boolean):boolean {
  return !state.busy && (!state.dirty || confirm());
}

/** Shared sections only: workflow-local settings are retained, not compared as replacements. */
export function agentDefinitionSectionsChanged(before:AgentDefinition, after:AgentDefinition):string[] {
  return [...new Set(changedGraphFields(before,after).map(path=>path.split(/[.[]/,1)[0]))];
}

export function associationChanges(before:Graph, after:Graph):string[] {
  const selected=(graph:Graph)=>({spec:{instructions:graph.spec.instructions,nodes:graph.spec.nodes.map(node=>({id:node.id,skillIds:node.skillIds,repositoryGuidance:node.repositoryGuidance}))}});
  return changedGraphFields(selected(before),selected(after));
}
