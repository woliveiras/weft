import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import type { MessageRecord } from '../api/types';
import { MessageList } from './ChannelPanel';

afterEach(cleanup);

const record: MessageRecord = {
  message: { id: 'msg-1', type: 'review.requested', source: '', target: '', payload: 'test payload', metadata: { correlation_id: 'trace-1' } },
  graphId: 'review', channel: 'requests', status: 'published', attempt: 1,
  createdAt: '2026-11-18T12:00:00Z', updatedAt: '2026-11-18T12:00:00Z',
};

it('opens the trace from the card and keeps the trace button as a separate control', () => {
  const openTrace = vi.fn();
  render(<MessageList records={[record]} onShowCorrelation={openTrace} />);
  fireEvent.click(screen.getByText('review.requested'));
  fireEvent.click(screen.getByRole('button', { name: /trace workflow/ }));
  expect(openTrace.mock.calls).toEqual([['trace-1'], ['trace-1']]);
});

it('keeps payload and metadata inspection independent of trace navigation', () => {
  const openTrace = vi.fn();
  render(<MessageList records={[record]} onShowCorrelation={openTrace} />);
  for (const field of ['payload', 'metadata']) {
    const button = screen.getByRole('button', { name: field });
    fireEvent.click(button);
    expect(button.getAttribute('aria-expanded')).toBe('true');
  }
  fireEvent.click(screen.getByText('test payload'));
  expect(openTrace).not.toHaveBeenCalled();
});

it('leaves the card inactive if its message has no usable correlation ID', () => {
  const openTrace = vi.fn();
  render(<MessageList records={[{...record, message: {...record.message, metadata: {correlation_id: 42}}}]} onShowCorrelation={openTrace} />);
  fireEvent.click(screen.getByText('review.requested'));
  expect(screen.queryByRole('button', { name: /trace workflow/ })).toBeNull();
  expect(openTrace).not.toHaveBeenCalled();
});
