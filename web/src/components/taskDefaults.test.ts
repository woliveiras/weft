import {expect,it} from 'vitest';
import type {Graph} from '../api/types';
import type {TaskInput,TaskDraft,TaskReview} from '../api/tasks';
import {requestPayload,updateTaskInput,workflowTaskDefaults,outcomeRequest,outcomeRequests,promptTitle,submissionDraftFeedback,taskConfigurationFieldErrors} from './taskDefaults';
const graph={metadata:{id:'factory'},spec:{nodes:[{id:'planner'},{id:'integrator'}],channels:[{id:'requests',to:['planner'],types:['feature.request']},{id:'handoff',from:'planner',to:['integrator']}]}} as Graph;
it('uses authored task bindings and resource names before topology inference without granting capabilities',()=>{
 const configured={...graph,spec:{...graph.spec,task:{nodeId:'integrator',resultNodeId:'planner',projectId:'project-a',scmConnection:'team-scm'}}};
 expect(workflowTaskDefaults(configured,'Title','Request')).toEqual({nodeId:'integrator',resultNodeId:'planner',channelId:undefined,messageType:undefined,payloadJson:undefined,projectId:'project-a',scmConnection:'team-scm'});
 const channel={...configured,spec:{...configured.spec,task:{nodeId:'planner',channelId:'requests',messageType:'feature.request'}}};
 expect(workflowTaskDefaults(channel,'Title','Request')).toEqual({nodeId:'planner',resultNodeId:'integrator',channelId:'requests',messageType:'feature.request',payloadJson:requestPayload('Title','Request')});
 expect(workflowTaskDefaults(configured,'','')).not.toHaveProperty('authorization');
});
it('explains draft submission outcomes without claiming execution or hiding missing configuration',()=>{
 const draft={status:'draft',graphId:'flow',graphVersion:1,nodeId:'worker',resolution:{state:'resolved',message:'Interpreted'}} as TaskDraft;
 expect(submissionDraftFeedback({...draft,graphVersion:0,nodeId:''})).toEqual({message:expect.stringContaining('Complete workflow version, entry agent'),configure:true});
 expect(submissionDraftFeedback(draft,{blockers:['Environment approval is required.']} as TaskReview)).toEqual({message:expect.stringContaining('Environment approval is required.'),configure:false});
 expect(submissionDraftFeedback({...draft,resolution:{state:'needs_clarification',message:'Choose a workflow.'}})).toEqual({message:expect.stringContaining('Choose a workflow.'),configure:true});
 expect(submissionDraftFeedback(draft)).toEqual({message:'Task has not started. Select Run task to check the saved configuration and continue.',configure:false});
 for(const status of ['running','succeeded','failed','cancelled'])expect(submissionDraftFeedback({...draft,status})).toBeUndefined();
});
it('suggests only unique document-defined entry, type and result',()=>{
 expect(workflowTaskDefaults(graph,'Title','Request')).toEqual({nodeId:'planner',resultNodeId:'integrator',channelId:'requests',messageType:'feature.request',payloadJson:requestPayload('Title','Request')});
 const ambiguous={...graph,spec:{...graph.spec,channels:[...graph.spec.channels!,{id:'other',to:['integrator']}]}};
 expect(workflowTaskDefaults(ambiguous,'','').nodeId).toBe('');
 const wildcard={...graph,spec:{...graph.spec,channels:[{id:'requests',to:['planner'],types:['feature.*']}]}};
 expect(workflowTaskDefaults(wildcard,'','').messageType).toBe('');expect(workflowTaskDefaults(wildcard,'','').resultNodeId).toBe('');
});
it('does not select disabled or absent agents or grant tools',()=>{
 expect(workflowTaskDefaults(undefined,'','').nodeId).toBe('');
 const disabled={...graph,spec:{nodes:[{id:'planner',disabled:true}],channels:graph.spec.channels}};
 const defaults=workflowTaskDefaults(disabled,'','');expect(defaults.nodeId).toBe('');expect(defaults.resultNodeId).toBe('');expect(defaults).not.toHaveProperty('tools');
});
it('keeps generated input in sync without replacing customized JSON',()=>{
 const input={title:'Old',request:'Before',channelId:'requests',payloadJson:requestPayload('Old','Before')} as TaskInput;
 expect(updateTaskInput(input,{title:'New'}).payloadJson).toBe(requestPayload('New','Before'));
 expect(updateTaskInput({...input,payloadJson:'{"custom":true}'},{request:'After'}).payloadJson).toBe('{"custom":true}');
 expect(updateTaskInput(input,{payloadJson:'{"explicit":true}',title:'New'}).payloadJson).toBe('{"explicit":true}');
});

it('offers editable goal instructions while preserving a customized request',()=>{
 const initial={outcome:'freeform',request:''} as TaskInput;
 expect(outcomeRequest(initial,'review')).toBe(outcomeRequests.review);
 expect(outcomeRequest({...initial,outcome:'review',request:outcomeRequests.review},'implement')).toBe(outcomeRequests.implement);
 expect(outcomeRequest({...initial,request:'My own instructions'},'review')).toBe('My own instructions');
});

it('derives a bounded display title without interpreting prompt instructions',()=>{
 expect(promptTitle('  Fix the parser\nDo not push')).toBe('Fix the parser');
 expect(promptTitle('   ')).toBe('');
 expect(promptTitle('😀'.repeat(100))).toBe('😀'.repeat(50));
 expect(promptTitle('a'.repeat(201))).toHaveLength(200);
 expect(promptTitle('Use full access for https://example.com/repo')).toBe('Use full access for https://example.com/repo');
});

it('identifies incomplete saved bindings without requiring a separate result agent',async()=>{
 const {missingTaskConfiguration}=await import('./taskDefaults');
 const input={graphId:'flow',graphVersion:1,nodeId:'worker'} as TaskInput;
 expect(missingTaskConfiguration(input)).toEqual([]);
 expect(missingTaskConfiguration({...input,graphId:'',graphVersion:0,nodeId:''})).toEqual(['workflow','workflow version','entry agent']);
 expect(missingTaskConfiguration({...input,graphVersion:NaN})).toEqual(['workflow version']);
 expect(missingTaskConfiguration({...input,channelId:'requests'})).toEqual(['channel message type and payload']);
 expect(missingTaskConfiguration({...input,channelId:'requests',messageType:'request',payloadJson:'{}'})).toEqual([]);
});

it('identifies only missing required configuration fields with actionable messages',()=>{
 const input={graphId:'flow',graphVersion:1,nodeId:'worker'} as TaskInput;
 expect(taskConfigurationFieldErrors(input)).toEqual({});
 expect(taskConfigurationFieldErrors({...input,graphId:'',graphVersion:0,nodeId:''})).toEqual({graphId:'Choose a workflow.',graphVersion:'Choose a workflow to select its version.',nodeId:'Choose the entry agent that will receive this task.'});
 expect(taskConfigurationFieldErrors({...input,graphVersion:NaN})).toEqual({graphVersion:expect.stringContaining('Choose a saved workflow version')});
 expect(taskConfigurationFieldErrors({...input,channelId:'requests',payloadJson:'{}'})).toEqual({messageType:expect.stringContaining('message type')});
 expect(taskConfigurationFieldErrors({...input,channelId:'requests',messageType:'request'})).toEqual({payloadJson:expect.stringContaining('JSON payload')});
 expect(taskConfigurationFieldErrors({...input,channelId:'requests',messageType:'request',payloadJson:'{}'})).toEqual({});
 expect(taskConfigurationFieldErrors({...input,channelId:'requests',messageType:'request',payloadJson:'{broken'})).toEqual({payloadJson:'Enter valid JSON for the selected channel.'});
 expect(taskConfigurationFieldErrors({...input,channelId:'requests',messageType:'request',payloadJson:'  '})).toEqual({payloadJson:expect.stringContaining('JSON payload')});
 expect(taskConfigurationFieldErrors({...input,payloadJson:'{broken'})).toEqual({});
});

it('identifies project snapshots using repository, reference and commit with local fallbacks',async()=>{
 const {projectChoiceLabel}=await import('./taskDefaults');
 const project={id:'p',name:'weft.git',path:'/server/repo',head:'abc123def4567890'} as import('../api/tasks').Project;
 expect(projectChoiceLabel(project)).toBe('weft.git · /server/repo · registered commit · abc123def456');
 expect(projectChoiceLabel({...project,repositoryUrl:'https://example.com/r.git',repositoryRef:'main'})).toBe('weft.git · https://example.com/r.git · main · abc123def456');
 expect(projectChoiceLabel({...project,path:'',head:''})).toBe('weft.git · p · registered commit · p');
});

it('defaults only unselected workflow versions and preserves pins or unavailable workflow selections',async()=>{
 const {defaultTaskWorkflowVersion}=await import('./taskDefaults');
 const input={graphId:'flow',graphVersion:0,nodeId:''} as TaskInput;
 const current={...graph,metadata:{id:'flow',name:'Flow',version:3},spec:{nodes:[]}};
 expect(defaultTaskWorkflowVersion(input,current).graphVersion).toBe(3);
 const pinned={...input,graphVersion:1};
 expect(defaultTaskWorkflowVersion(pinned,current)).toBe(pinned);
 expect(defaultTaskWorkflowVersion(input,undefined)).toBe(input);
 expect(defaultTaskWorkflowVersion(input,{...current,metadata:{id:'other',name:'Other',version:4}})).toBe(input);
 expect(defaultTaskWorkflowVersion(input,{...current,metadata:{id:'flow',name:'Flow'}})).toBe(input);
});

it('gives task-local preparation guidance only when the server permits environment preparation',async()=>{
 const {isTaskEnvironmentPreparationNotice}=await import('./taskDefaults');
 const task={status:'draft',graphId:'flow',graphVersion:1,nodeId:'worker'} as TaskDraft;
 const blocker='spec.environment: environment not ready: it has not been built yet; build it in Graph settings → Environment (needed by worker)';
 const review={task,canPrepareEnvironment:true,blockers:[blocker]} as TaskReview;
 expect(submissionDraftFeedback(task,review)?.message).toContain('Review and prepare its Docker environment here');
 expect(submissionDraftFeedback(task,{...review,canPrepareEnvironment:false})?.message).toContain(blocker);
 expect(isTaskEnvironmentPreparationNotice(blocker)).toBe(true);
 expect(isTaskEnvironmentPreparationNotice('environment not ready: absent')).toBe(true);
 expect(isTaskEnvironmentPreparationNotice('Prepare and review the task environment before starting agents.')).toBe(true);
 expect(isTaskEnvironmentPreparationNotice('spec.environment: network policy refused')).toBe(false);
 expect(isTaskEnvironmentPreparationNotice('Task authorization is stale.')).toBe(false);
});
