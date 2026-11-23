import {expect,it} from 'vitest';
import type {NodeStatus} from '../api/types';
import {workspaceReplicas} from './FilesView';

it('retains recorded replicas after scale-down without inventing intermediate historical replicas',()=>{
  const node = {nodeId:'worker',desiredReplicas:1,replicas:[{graphId:'g',nodeId:'worker',replica:3}]} as NodeStatus;
  expect(workspaceReplicas('g','worker',node)).toEqual([0,3]);
});

it('includes configured replica slots and ignores foreign, invalid and duplicate records',()=>{
  const node = {nodeId:'worker',desiredReplicas:2,replicas:[
    {graphId:'g',nodeId:'worker',replica:1},{graphId:'g',nodeId:'worker',replica:1},
    {graphId:'other',nodeId:'worker',replica:4},{graphId:'g',nodeId:'other',replica:5},
    {graphId:'g',nodeId:'worker',replica:-1},{graphId:'g',nodeId:'worker',replica:1.5},
  ]} as NodeStatus;
  expect(workspaceReplicas('g','worker',node)).toEqual([0,1]);
});

it('keeps the initial workspace slot when a general workspace has no runtime status',()=>{
  expect(workspaceReplicas('g','worker')).toEqual([0]);
  expect(workspaceReplicas('g','worker',{nodeId:'worker',state:'stopped',pending:0,desiredReplicas:0,replicas:[]})).toEqual([0]);
});
