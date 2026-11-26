ALTER TABLE operator_messages ADD COLUMN run_id TEXT NOT NULL DEFAULT '';
CREATE INDEX operator_messages_pending_run ON operator_messages (graph_id, node_id, run_id, replica)
  WHERE consumed_at IS NULL;
