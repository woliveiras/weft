export const statusLabels: Record<string, string> = {
  draft: 'Draft', preparing: 'Preparing', environment_pending: 'Waiting for environment',
  environment_build_requested: 'Environment build requested', environment_cancel_requested: 'Cancelling environment',
  starting: 'Starting', running: 'Running', awaiting_approval: 'Awaiting approval', blocked: 'Blocked',
  succeeded: 'Succeeded', failed: 'Failed', interrupted: 'Interrupted', cancel_requested: 'Stopping', cancelled: 'Cancelled',
};
export const statusTones: Record<string, string> = {
  running: 'info', awaiting_approval: 'warn', blocked: 'warn', succeeded: 'ok', failed: 'bad', interrupted: 'warn',
};

export const statusLabel = (status: string) => statusLabels[status] ?? status.replaceAll('_', ' ');
