import { describe, expect, it } from 'vitest';
import { parseRoute, routeHash } from './App';

describe('installation destination routes', () => {
  it('round-trips dedicated delegation profile destinations', () => {
    expect(parseRoute('#/administration/delegation/profiles/%invalid/edit')).toEqual({view:'administration',section:'delegation'});
    for (const hash of ['#/administration/delegation/profiles/new', '#/administration/delegation/profiles/reviewer/edit']) {
      expect(routeHash(parseRoute(hash))).toBe(hash);
    }
  });
  it('round-trips project creation and import destinations', () => {
    for (const hash of ['#/projects', '#/projects/new', '#/projects/import']) {
      expect(routeHash(parseRoute(hash))).toBe(hash);
    }
    expect(parseRoute('#/projects/unknown')).toEqual({ view: 'projects' });
  });
  it('supports independent destinations and safe administration fallback', () => {
    expect(parseRoute('#/models')).toEqual({ view: 'models' });
    expect(parseRoute('#/administration/network')).toEqual({ view: 'administration', section: 'network' });
    expect(parseRoute('#/administration/unknown')).toEqual({ view: 'administration', section: 'health' });
    expect(routeHash({ view: 'administration', section: 'retention' })).toBe('#/administration/retention');
    expect(routeHash({ view: 'connections' })).toBe('#/connections');
  });
  it('preserves old and new template and OAuth query payloads without interpreting them', () => {
    for (const prefix of ['integrations', 'connections']) {
      expect(parseRoute(`#/${prefix}/connect?template=github&version=1`)).toEqual({ view: 'connections', connectQuery: 'template=github&version=1', oauthQuery: undefined });
      expect(parseRoute(`#/${prefix}/oauth?state=example`)).toEqual({ view: 'connections', connectQuery: undefined, oauthQuery: 'state=example' });
      expect(parseRoute(`#/${prefix}/connect`)).toMatchObject({ connectQuery: '' });
    }
  });
  it('routes the Activity view with its filters', () => {
    expect(parseRoute('#/activity')).toEqual({ view: 'activity', activity: { window: '24h', status: '', workflow: '', agent: '', focus: '', tab: '' } });
    expect(routeHash(parseRoute('#/activity?window=6h&status=failed&focus=history'))).toBe('#/activity?window=6h&status=failed&focus=history');
    expect(routeHash(parseRoute('#/activity?window=7d&tab=health'))).toBe('#/activity?window=7d&tab=health');
  });
  it('preserves graph correlation links', () => {
    const route = parseRoute('#/g/example/messages?c=sample');
    expect(routeHash(route)).toBe('#/g/example/messages?c=sample');
  });
});
it('round-trips connection destinations and falls back from unknown sections', () => {
 for (const hash of ['#/connections/new', '#/connections/source-control/new', '#/connections/history', '#/connections/c/team/tools', '#/connections/c/team/settings']) expect(routeHash(parseRoute(hash))).toBe(hash);
 expect(parseRoute('#/connections/c/team/unknown')).toMatchObject({connectionID:'team',connectionTab:'overview'});
});

it('round-trips native issue connection editors without reserving a valid connection name', () => {
 for (const hash of ['#/connections/issues/new', '#/connections/issues/c/jira-team', '#/connections/issues/c/new']) expect(routeHash(parseRoute(hash))).toBe(hash);
});
it('round-trips agent configuration deep links and rejects unknown sections',()=>{
 const route=parseRoute('#/g/example/agent?agent=reviewer&section=tools');
 expect(route).toMatchObject({tab:'agent',agentId:'reviewer',agentSection:'tools'});
 expect(routeHash(route)).toBe('#/g/example/agent?agent=reviewer&section=tools');
 expect(parseRoute('#/g/example/agent?agent=reviewer&section=unknown')).toMatchObject({agentSection:'general'});
});
it('preserves execution and section deep links',()=>{
 const hash='#/g/example/runs?execution=exec-1&section=timeline';expect(routeHash(parseRoute(hash))).toBe(hash);
 expect(parseRoute('#/g/example/runs?execution=exec-1&section=unknown')).toMatchObject({tab:'runs',executionId:'exec-1',runSection:'result'});
 expect(routeHash(parseRoute('#/g/example/runs'))).toBe('#/g/example/runs');
});
it('preserves canvas selection links',()=>{
 const route=parseRoute('#/g/example/canvas?sel=node%3Areviewer');
 expect(route).toMatchObject({selectionKey:'node:reviewer'});
 expect(routeHash(route)).toBe('#/g/example/canvas?sel=node%3Areviewer');
});
it('retains a scoped connection return destination without accepting an external redirect',()=>{
 const route=parseRoute('#/g/g/agent?agent=reader&section=tools&connection=team');
 expect(routeHash(route)).toBe('#/g/g/agent?agent=reader&section=tools&connection=team');
 expect(routeHash({...route,view:'graph',id:'g',tab:'runs'})).toBe('#/g/g/runs?connection=team');
});

it('round trips settings deep links and falls back to General for an unknown section', () => {
  const network = '#/g/example/settings/network';
  expect(parseRoute(network)).toMatchObject({ tab: 'settings', settingsSection: 'network' });
  expect(routeHash(parseRoute(network))).toBe(network);
  expect(routeHash(parseRoute('#/g/example/settings/unknown'))).toBe('#/g/example/settings/general');
  expect(routeHash(parseRoute('#/g/example/settings'))).toBe('#/g/example/settings/general');
});

it('round-trips native issue transition inspection without treating it as connection editing',()=>{
 const hash='#/connections/issues/c/native/transitions';
 expect(parseRoute(hash)).toEqual({view:'connections',issueConnectionID:'native',issueTransitions:true});
 expect(routeHash(parseRoute(hash))).toBe(hash);
});
