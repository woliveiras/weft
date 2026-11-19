import {expect,it} from 'vitest';
import {parseRoute,routeHash} from '../App';
import {modelsHash,parseModelsRoute} from './modelsNavigation';

it('preserves the models entry point and round trips explicit configuration destinations',()=>{
  expect(parseRoute('#/models')).toEqual({view:'models'});
  for(const modelSection of ['plugins','usage','budgets','prices'] as const) {
    const route={view:'models' as const,modelSection};
    expect(parseRoute(routeHash(route))).toEqual(route);
  }
  for(const providerEditor of [{mode:'create' as const},{mode:'edit' as const,name:'team-east'}]) {
    const route={view:'models' as const,providerEditor};
    expect(parseRoute(routeHash(route))).toEqual(route);
  }
  expect(parseModelsRoute(['models','unknown'])).toEqual({});
  expect(modelsHash({modelSection:'providers'})).toBe('#/models');
});
