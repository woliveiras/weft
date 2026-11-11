import type { Execution, MessageRecord } from '../api/types';

/** Only persisted identifiers establish causality; a shared agent is insufficient. */
export function belongsToExecution(message: MessageRecord, execution: Execution): boolean {
  return message.graphId === execution.graphId && (
    execution.inputIds?.includes(message.message.id) === true ||
    execution.outputIds?.includes(message.message.id) === true ||
    message.message.metadata?.execution_id === execution.id
  );
}

export function executionProgress(execution: Execution): string {
  switch (execution.status) {
    case 'pending': return 'Waiting to start';
    case 'running': return 'In progress';
    case 'awaiting_approval': return 'Waiting for approval';
    case 'succeeded': return 'Completed';
    case 'failed': return 'Failed';
    case 'cancelled': return 'Cancelled';
    case 'interrupted': return 'Interrupted';
    default: return 'Unknown state';
  }
}
