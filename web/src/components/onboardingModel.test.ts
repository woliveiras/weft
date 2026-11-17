import { expect, it } from 'vitest';
import { exampleGraph, freshProgress, guideSteps, parseProgress, progressKey, reviewStep } from './onboardingModel';

it('round trips reviewed topics and resumes without completing skipped topics', () => {
  const progress = reviewStep(freshProgress());
  expect(parseProgress(JSON.stringify(progress))).toEqual({ version: 1, current: 1, reviewed: ['concepts'] });
  expect(reviewStep({ ...progress, current: 5 }).reviewed).toEqual(['concepts', 'run']);
  expect(reviewStep({ ...progress, current: 0 }).reviewed).toEqual(['concepts']);
  expect(parseProgress(null)).toEqual(freshProgress());
});
it.each(['{', '{}', '{"version":2}', '{"version":1,"current":-1,"reviewed":[]}', '{"version":1,"current":6,"reviewed":[]}', '{"version":1,"current":0,"reviewed":["unknown"]}'])('rejects corrupt progress %s', raw => {
  expect(() => parseProgress(raw)).toThrow();
});
it('separates identities and roles and stores only the guide schema', () => {
  expect(progressKey('one', 'admin')).not.toBe(progressKey('two', 'admin'));
  expect(progressKey('one', 'admin')).not.toBe(progressKey('one', 'viewer'));
  expect(Object.keys(freshProgress())).toEqual(['version', 'current', 'reviewed']);
  let progress = freshProgress();
  for (const _step of guideSteps) progress = reviewStep(progress);
  expect(progress.reviewed).toHaveLength(6);
  expect(progress.current).toBe(5);
});
it('builds a finite echo example without grants or model dependencies', () => {
  const graph = exampleGraph('example');
  expect(graph.spec.nodes).toEqual([expect.objectContaining({ runtime: {kind:'echo'}, loop:{mode:'once'}, tools:[] })]);
  expect(graph.spec.nodes[0].model).toBeUndefined();
});
