import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChatPRDraftComment, ChatPRPublicationReview } from '../api/chat';
import { ChatPRCommentSelection, ChatPRPublicationCard } from './ChatPRPublicationCard';

afterEach(cleanup);

const comments: ChatPRDraftComment[] = [
  {id:1,path:'file.md',side:'RIGHT',line:2,body:'Add a guard.'},
  {id:2,path:'file.md',side:'LEFT',line:1,body:'Explain this removal.'},
];

it('prepares only checked draft comments and requires a nonempty selection', () => {
  const prepare = vi.fn();
  render(<ChatPRCommentSelection comments={comments} enabled onPrepare={prepare}/>);
  const review = screen.getByRole('button', {name:'Review selected comments'});
  expect(review).toHaveProperty('disabled',true);
  fireEvent.click(screen.getByRole('checkbox',{name:/removed line 1/}));
  expect(prepare).not.toHaveBeenCalled();
  fireEvent.click(review);
  expect(prepare).toHaveBeenCalledExactlyOnceWith([2]);
});

it('reconciles uncertain receipts by a separate action and keeps known outcomes visible', () => {
  const review: ChatPRPublicationReview = {
    id:'operation',digest:'digest',expiresAt:'2027-01-01',state:'incomplete',
    prPublication:{analysisId:'analysis',executionId:'execution',comments,analysis:{number:418,provider:'github',connection:'origin',repository:'team/repo',head:'head',base:'base',graph:'reviews',node:'worker',version:3,modelProvider:'fixture',modelName:'reader'}},
    prReceipts:[{state:'published',id:991},{state:'unknown'}],
  };
  const reconcile = vi.fn(), decide = vi.fn();
  render(<ChatPRPublicationCard review={review} now={0} canConfirm onDecide={decide} onReconcile={reconcile}/>);
  expect(screen.getByText('Published · Provider comment 991')).toBeTruthy();
  expect(screen.getByText(/Unknown · Publication receipt/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button',{name:'Reconcile comment receipts'}));
  expect(reconcile).toHaveBeenCalledExactlyOnceWith();
  expect(decide).not.toHaveBeenCalled();
});
