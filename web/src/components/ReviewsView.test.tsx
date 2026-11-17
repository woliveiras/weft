import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, it, expect, vi } from 'vitest';
import { ReviewCard } from './ReviewsView';
import type { HumanReview } from '../api/types';
import { api } from '../api/client';
afterEach(() => {cleanup(); vi.restoreAllMocks();});
const review: HumanReview = {id:'review',graphId:'g',nodeId:'n',replica:0,request:{title:'Update React',instructions:'Check navigation'},candidate:'abcdef',state:'pending',session:'ready',generation:1,revision:3,url:'http://review.localhost:8082',sessionExpires:'2026-10-16T12:00:00Z',expiresAt:'2026-10-17T12:00:00Z'};
describe('Human review', () => {
 it('records explicit acceptance without manual execution and the reviewed revision', async () => {
  const decide=vi.spyOn(api,'reviewAction').mockResolvedValue({...review,state:'approved'});const reload=vi.fn();render(<ReviewCard review={review} reload={reload}/>);
  await userEvent.selectOptions(screen.getByLabelText('Review method'),'without_execution');await userEvent.type(screen.getByLabelText('Review feedback'),'Checks reviewed');await userEvent.click(screen.getByRole('button',{name:'Approve candidate'}));
  expect(decide).toHaveBeenCalledWith('review','decide',{state:'approved',method:'without_execution',comment:'Checks reviewed',revision:3});expect(reload).toHaveBeenCalled();decide.mockRestore();
 });
 it('shows failures and cannot open an expired session', async () => {
  render(<ReviewCard review={{...review,session:'expired'}} reload={vi.fn()}/>);expect((screen.getByRole('button',{name:'Open preview'}) as HTMLButtonElement).disabled).toBe(true);
  const action=vi.spyOn(api,'reviewAction').mockRejectedValue(new Error('No sandbox capacity'));await userEvent.click(screen.getByRole('button',{name:'Restart preview'}));expect(await screen.findByText('No sandbox capacity')).toBeTruthy();action.mockRestore();
 });
});

it('sends the generation decision through the real API client and surfaces conflicts', async () => {
 const {fakeApi,apiError}=await import('../test/fakeApi');
 const backend=fakeApi({'POST /api/v1/reviews/review/decide':()=>apiError(409,'Candidate changed','conflict')});
 render(<ReviewCard review={review} reload={vi.fn()}/>);await userEvent.click(screen.getByRole('button',{name:'Request changes'}));
 expect(await screen.findByText(/Candidate changed/)).toBeTruthy();expect(backend.calls[0].body).toEqual({state:'changes_requested',method:'managed',comment:'',revision:3});
});
