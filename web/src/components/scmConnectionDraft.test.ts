import { describe, expect, it } from 'vitest';
import { changeSCMAuthType, changeSCMKind, emptySCMDraft, scmConnectionInput } from './scmConnectionDraft';

describe('SCM credential draft', () => {
  it('defaults new Cloud connections to API tokens and clears prior service credentials and addresses', () => {
    const previous = { ...emptySCMDraft, name: 'team', repository: 'team/app', username: 'old@example.com', token: 'synthetic', apiUrl: 'https://old.example', gitUrl: 'https://old.example/app.git' };
    const cloud = changeSCMKind(previous, 'bitbucket-cloud');
    expect(cloud).toEqual({ ...previous, kind: 'bitbucket-cloud', authType: 'api-token', username: '', token: '', apiUrl: '', gitUrl: '' });
    expect(previous.token).toBe('synthetic');
    for (const kind of ['github', 'bitbucket-dc']) {
      expect(changeSCMKind({ ...cloud, username: 'new@example.com', token: 'new-synthetic' }, kind)).toMatchObject({ kind, authType: 'bearer', username: '', token: '' });
    }
  });

  it('clears the entered email and secret in both credential-mode directions', () => {
    const draft = { ...emptySCMDraft, kind: 'bitbucket-cloud', username: 'old@example.com', token: 'synthetic' };
    for (const authType of ['api-token', 'bearer'] as const) {
      expect(changeSCMAuthType(draft, authType)).toEqual({ ...draft, authType, username: '', token: '' });
    }
  });

  it('serializes the explicit API-token account without trimming or encoding the secret', () => {
    const draft = { ...changeSCMKind(emptySCMDraft, 'bitbucket-cloud'), name: ' team ', repository: ' acme/app ', username: ' owner@example.com ', token: 'fixture:token', approval: false };
    expect(JSON.parse(JSON.stringify(scmConnectionInput(draft)))).toEqual({ name: 'team', kind: 'bitbucket-cloud', repository: 'acme/app', authType: 'api-token', username: 'owner@example.com', token: 'fixture:token', requirePullRequestApproval: false });
  });

  it('omits irrelevant account fields for OAuth and non-Cloud services', () => {
    const draft = { ...emptySCMDraft, username: 'stale@example.com', token: 'synthetic', name: 'team', repository: 'acme/app' };
    for (const kind of ['github', 'bitbucket-dc']) {
      const input = scmConnectionInput({ ...draft, kind, authType: 'api-token' });
      expect(input).not.toHaveProperty('authType');
      expect(input).not.toHaveProperty('username');
    }
    const oauth = scmConnectionInput({ ...draft, kind: 'bitbucket-cloud', authType: 'bearer' });
    expect(oauth.authType).toBe('bearer');
    expect(oauth).not.toHaveProperty('username');
  });
});
