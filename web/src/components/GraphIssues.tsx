import type { Selection } from './canvas/GraphCanvas';
import { fieldIssues, type ElementIssue, type IssueIndex } from './canvas/edit';

/** Messages for one field, rendered under its input. */
export function FieldIssues({ issues, field }: { issues?: ElementIssue[]; field: string }) {
  const list = fieldIssues(issues, field);
  if (list.length === 0) return null;
  return (
    <>
      {list.map((issue, i) => (
        <span key={i} className={issue.severity === 'error' ? 'field-error' : 'field-warning'} role={issue.severity === 'error' ? 'alert' : undefined}>
          {issue.field !== field ? `${issue.field}: ` : ''}
          {issue.message}
        </span>
      ))}
    </>
  );
}

/** Every finding of one element, for fields the panel does not edit inline. */
export function ElementIssues({ issues, label }: { issues?: ElementIssue[]; label: string }) {
  if (!issues?.length) return null;
  return (
    <ul className="issue-list" aria-label={label}>
      {issues.map((issue, i) => (
        <li key={i} className={issue.severity === 'error' ? 'bad' : 'warn'}>
          <code>{issue.field || '(element)'}</code> {issue.message}
        </li>
      ))}
    </ul>
  );
}

/** Summary of a validation run, with links to the affected elements. */
export function IssueSummary({
  index,
  onSelect,
  onOpenSettings,
}: {
  index: IssueIndex;
  onSelect: (s: Selection) => void;
  /** Opens graph settings, where graph-level findings are edited. */
  onOpenSettings?: () => void;
}) {
  const rows: { key: string; severity: ElementIssue['severity']; text: string; target?: Selection }[] = [];
  for (const issue of index.graph) rows.push({ key: `g${rows.length}`, severity: issue.severity, text: `${issue.path}: ${issue.message}` });
  for (const [id, list] of index.nodes)
    for (const issue of list)
      rows.push({ key: `n${rows.length}`, severity: issue.severity, text: `${issue.field || 'agent'}: ${issue.message}`, target: { kind: 'node', id } });
  for (const [id, list] of index.channels)
    for (const issue of list)
      rows.push({
        key: `c${rows.length}`,
        severity: issue.severity,
        text: `${issue.field || 'channel'}: ${issue.message}`,
        target: { kind: 'channel', id },
      });
  if (rows.length === 0) return null;
  const errors = rows.filter((row) => row.severity === 'error').length;
  return (
    <div role={errors ? 'alert' : 'status'}>
      <ul className={`issue-list ${errors ? '' : 'warnings-only'}`} aria-label="Graph validation">
        <li>
          <strong>
            {errors ? `${errors} error${errors > 1 ? 's' : ''} must be fixed before saving` : 'Valid, with warnings'}
          </strong>
        </li>
        {rows.map((row) => (
          <li key={row.key} className={row.severity === 'error' ? 'bad' : 'warn'}>
            {row.target ? (
              <button type="button" className="link" onClick={() => onSelect(row.target!)}>
                {row.target.kind === 'node' ? 'Agent' : 'Channel'} {row.target.id}
              </button>
            ) : onOpenSettings ? (
              <button type="button" className="link" onClick={onOpenSettings}>
                Graph settings
              </button>
            ) : null}{' '}
            {row.text}
          </li>
        ))}
      </ul>
    </div>
  );
}
