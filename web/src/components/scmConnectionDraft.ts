import type { SCMConnectionInput } from '../api/types';

export type SCMDraft = {
  name: string;
  kind: string;
  repository: string;
  apiUrl: string;
  gitUrl: string;
  branchPrefix: string;
  approval: boolean;
  authType: 'bearer' | 'api-token';
  username: string;
  token: string;
};

export const emptySCMDraft: SCMDraft = {
  name: '', kind: 'github', repository: '', apiUrl: '', gitUrl: '', branchPrefix: '',
  approval: true, authType: 'bearer', username: '', token: '',
};

/** A provider switch never carries a credential into another service. */
export function changeSCMKind(draft: SCMDraft, kind: string): SCMDraft {
  return { ...draft, kind, apiUrl: '', gitUrl: '', authType: kind === 'bitbucket-cloud' ? 'api-token' : 'bearer', username: '', token: '' };
}

/** Switching credential types requires entering the credential again. */
export function changeSCMAuthType(draft: SCMDraft, authType: SCMDraft['authType']): SCMDraft {
  return { ...draft, authType, username: '', token: '' };
}

export function scmConnectionInput(draft: SCMDraft): SCMConnectionInput {
  const input: SCMConnectionInput = {
    name: draft.name.trim(), kind: draft.kind, repository: draft.repository.trim(),
    apiUrl: draft.apiUrl.trim() || undefined, gitUrl: draft.gitUrl.trim() || undefined,
    branchPrefix: draft.branchPrefix.trim() || undefined,
    requirePullRequestApproval: draft.approval ? undefined : false, token: draft.token,
  };
  if (draft.kind === 'bitbucket-cloud') {
    input.authType = draft.authType;
    if (draft.authType === 'api-token') input.username = draft.username.trim();
  }
  return input;
}
