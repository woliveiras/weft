import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { ChatConfigurationReview } from '../api/chat';
import { ChatConfigurationCard } from './ChatConfigurationCard';

afterEach(cleanup);

const review: ChatConfigurationReview = {
  id:'op-1', digest:'digest', expiresAt:'2026-12-01T00:00:00Z', state:'awaiting_confirmation',
  configuration: { document:'example', intakeConsent:true, objects:[
    {key:'IssueTrigger/notes',action:'update',revision:'rev1',consent:'Enable polling of labels on the selected repository.',
      changes:[{path:'spec.label',before:'ready',after:'release'}]},
  ]},
};

it('presents the revision, affected fields and specific intake consent before confirmation', () => {
  const decide = vi.fn();
  render(<ChatConfigurationCard review={review} now={0} canConfirm onDecide={decide}/>);
  expect(screen.getByText('IssueTrigger/notes')).toBeTruthy();
  expect(screen.getByText('spec.label')).toBeTruthy();
  expect(screen.getByText('Enable polling of labels on the selected repository.')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', {name:'Consent to intake and apply configuration'}));
  expect(decide).toHaveBeenCalledWith('confirm');
});

it('retains object-level receipts after a partial write and offers no replay control', () => {
  render(<ChatConfigurationCard review={{...review,state:'incomplete', configReceipts:[
    {key:'CostPolicy/installation',result:'applied',revision:'new'},
    {key:'Budget/installation/*',result:'failed'},
  ]}} now={0} canConfirm onDecide={vi.fn()}/>);
  expect(screen.getByRole('status').textContent).toContain('stopped before completing');
  expect(screen.getByRole('list',{name:'Configuration results'}).textContent).toContain('Budget/installation/* · failed');
  expect(screen.queryByRole('button')).toBeNull();
});
