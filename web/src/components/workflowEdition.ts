import {api} from '../api/client';
import type {JiraCloudProfile, WorkflowConnection, WorkflowEdition} from '../api/types';

export const cloudProfileLink = (name:string) => `#/connections/jira-cloud/c/${encodeURIComponent(name)}`;
export const workflowConnectionLink = (edition:WorkflowEdition,name:string) => edition==='jira'?cloudProfileLink(name):`#/connections/issues/c/${encodeURIComponent(name)}`;
export const cloudWorkflowConnection = (p:JiraCloudProfile):WorkflowConnection => ({name:p.name,siteUrl:p.siteUrl,projectId:p.projectId,enabled:p.enabled,projectRestricted:p.projectRestricted,revision:p.revision,credentialConfigured:true});
export function workflowAPI(edition:WorkflowEdition) {
  return edition==='jira'? {
    connection:async(name:string)=>cloudWorkflowConnection(await api.jiraCloudProfile(name)),
    mappings:api.cloudWorkflowMappings,mapping:api.cloudWorkflowMapping,save:api.saveCloudWorkflowMapping,discover:api.discoverCloudTransitions,
  } : {connection:api.issueConnection,mappings:api.issueWorkflowMappings,mapping:api.issueWorkflowMapping,save:api.saveIssueWorkflowMapping,discover:api.discoverIssueTransitions};
}
