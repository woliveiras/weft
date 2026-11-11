import {expect,it} from 'vitest';
import { validDelegationPolicy, validDelegationProfile, type DelegationPolicy } from './delegation';
const policy:DelegationPolicy={enabled:true,approvalMode:'required',maxDepth:1,maxChildrenPerExecution:3,maxConcurrentChildren:4,maxDurationSeconds:120,maxOutputTokens:1024,maxObjectiveBytes:4096,maxResultBytes:16384,maxTreeTokens:65536};
it('validates finite limits and supported approval/depth choices',()=>{
 expect(validDelegationPolicy(policy)).toBe(true);
 for(const patch of [{maxDepth:5},{maxConcurrentChildren:33},{maxDurationSeconds:0},{maxOutputTokens:1.5},{maxChildrenPerExecution:NaN}])expect(validDelegationPolicy({...policy,...patch})).toBe(false);
 expect(validDelegationPolicy({...policy,approvalMode:'automatic'})).toBe(true);
});
it('uses byte limits for profile text and rejects invalid identifiers',()=>{
 const p={id:'review',name:'Review',instructions:'Review supplied text',enabled:true,maxOutputTokens:100};
 expect(validDelegationProfile(p)).toBe(true);
 for(const patch of [{id:'../bad'},{instructions:' '},{instructions:'\0'},{instructions:'é'.repeat(2049)},{maxOutputTokens:4097}])expect(validDelegationProfile({...p,...patch})).toBe(false);
});
it('validates explicit inherited capabilities without allowing control or wildcard grants',()=>{
 const profile={id:'reader',name:'Reader',instructions:'Read',enabled:true,maxOutputTokens:10};
 expect(validDelegationProfile({...profile,tools:['filesystem.read','mcp.personal.get_me']})).toBe(true);
 for(const tools of [['*'],['mcp.personal.*'],['environment.propose'],['review.request'],['filesystem.read','filesystem.read'],['']])expect(validDelegationProfile({...profile,tools})).toBe(false);
});
