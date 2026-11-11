export interface Project {importId?:string;repositoryUrl?:string;repositoryRef?:string;executionMode?: "workflow"|"local"|"sandbox";id:string;revision:number;name:string;path:string;head:string;createdAt:string}
import type {Approval, ValidationResult, Execution, ToolGrant, ModelRef } from './types';
export interface TaskInput {
 source?: {kind:"github"|"jira";connectionId:string;reference:string;cloudId?:string};
  executionMode?: "workflow"|"local"|"sandbox";
  channelId?: string;
  messageType?: string;
  payloadJson?: string;
  projectId?: string;
  baseCommit?: string;
  title: string;
  request: string;
  outcome: string;
  graphId: string;
  graphVersion: number;
  nodeId: string;
  resultNodeId?: string;
  /** SCM connection the result agent pushes to and opens a pull request on. */
  scmConnection?: string;
}
export interface TaskDraft extends TaskInput {
 submitted?:boolean;
 resolution?: {state:string;message:string;repository?:string};
  id: string;
  revision: number;
  status: string;
  observedState?: string;
  executionGraphId?: string;
  launchError?: string;
  createdBy: string;
  updatedBy: string;
  createdAt: string;
  updatedAt: string;
}
export interface TaskPage {items: TaskDraft[]; next?: string}
export interface TaskReview {canPrepareEnvironment?:boolean;environmentDefinition?:import('./types').EnvironmentSpec;environment?:import('./types').EnvironmentView;project?:Project;workspacePath?:string;task: TaskDraft; currentGraphVersion: number; validation: ValidationResult; blockers: string[]; canStart: boolean; agents?: {id:string;model:ModelRef;grants:ToolGrant[];isolation:string}[]; executionGraphId: string; grants: ToolGrant[]; model: ModelRef; isolation: string}

export interface TaskWorkspace {nodeId:string;replica:number;path:string;branch?:string;inspectionNode:string;subdir:string}
/** What a task's SCM hand-off did. */
export interface TaskDelivery {connection:string;branch?:string;repository?:string;pullRequestUrl?:string;pullRequestNumber?:number;failures?:string[]}
export interface TaskProgress {delivery?:TaskDelivery;decisions?:Approval[];environment?:import('./types').EnvironmentView;workspaces?:TaskWorkspace[];pendingApprovals?:Approval[]|null;workspacePath?:string; task: TaskDraft; state: string; executions: Execution[]; result?: Execution; blockers: string[] }

export interface CleanupPreview {path:string;branch:string;head:string;status:string;clean:boolean;fingerprint:string}

export interface RepositoryImportRecord {authentication?:string;id:string;url:string;ref:string;state:string;message:string;revision:number;attempt:string;projectId?:string;commit?:string;bytes:number;createdAt:string;updatedAt:string;deadline:string}

export type AuthorizationMode = "ask"|"workspace"|"full";
export interface TaskAuthorization {projectId?:string;preference?:{mode:AuthorizationMode;revision:number};capabilities?:{id:string;isolation:string;grants:ToolGrant[]}[];scope:string;record?:{taskId:string;mode:AuthorizationMode;scope:string;actor:string;policyVersion:number;revision:number}}
