ALTER TABLE executions ADD COLUMN parent_execution_id TEXT NOT NULL DEFAULT '';
UPDATE executions SET parent_execution_id = COALESCE(json_extract(doc, '$.parentExecutionId'), '');
CREATE INDEX executions_parent_started ON executions (parent_execution_id, started_at);
