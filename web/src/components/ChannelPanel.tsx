import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { Channel, ChannelType, GraphStatus, GraphSummary, MessageRecord } from '../api/types';
import { useAsync, useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import { consumers } from './canvas/model';
import type { ElementIssue } from './canvas/edit';
import { Badge, Btn, ErrorBox, Icon, JsonView, StateDot, fmtTime, stateLabel } from './common';
import { ElementIssues, FieldIssues } from './GraphIssues';

const SEMANTICS: Record<string, string> = {
  queue: 'Point-to-point: every consumer (all target nodes and their replicas) competes over one backlog.',
  topic: 'Publish-subscribe: each subscribing node gets a copy; replicas of a node compete over it.',
  fanout: 'Static fan-out: a copy per listed target node; replicas of a target compete.',
  broadcast: 'Broadcast: every replica of every target node gets its own copy.',
};

export function ChannelPanel({
  summary,
  status,
  channelId,
  onShowCorrelation,
  onUpdateChannel,
  issues,
}: {
  summary: GraphSummary;
  status?: GraphStatus;
  channelId: string;
  onShowCorrelation: (correlationId: string) => void;
  onUpdateChannel?: (channelId: string, update: Partial<Channel>) => void;
  /** Validation findings for this channel. */
  issues?: ElementIssue[];
}) {
  const g = summary.graph;
  const gid = g.metadata.id;
  const ch = g.spec.channels?.find((c) => c.id === channelId);
  const cs = status?.channels?.find((c) => c.channelId === channelId);
  const msgs = useAsync(() => api.messages(gid, { channel: channelId, limit: 50 }), [gid, channelId]);
  const reload = useThrottled(() => void msgs.reload(), 700);
  useEventStream({ graph: gid, prefixes: ['message.'] }, reload);

  if (!ch) return <p className="muted">Channel “{channelId}” is not in this graph version.</p>;
  const type = ch.type ?? 'queue';
  const subs = consumers(g, ch);

  return (
    <div className="panel-body">
      <div className="panel-title">
        <Icon name={type} size={18} />
        <h2>{ch.label || ch.id}</h2>
        <Badge>{type}</Badge>
        {ch.approval && (
          <Badge tone="warn" title="Messages are held until a human approves them">
            <Icon name="lock" size={11} /> gated
          </Badge>
        )}
      </div>
      <p className="muted small">{SEMANTICS[type]}</p>
      <ElementIssues issues={issues} label="Channel validation" />
      <dl className="kv">
        <dt>from</dt>
        <dd>{ch.from || <span className="muted">external ingress (API / injection)</span>}</dd>
        <dt>to</dt>
        <dd>{subs.join(', ') || <span className="muted">no subscribers</span>}</dd>
        {type === 'topic' && (
          <>
            <dt>topic</dt>
            <dd className="mono">{ch.topic || ch.id}</dd>
          </>
        )}
        {ch.types?.length ? (
          <>
            <dt>types</dt>
            <dd className="mono">{ch.types.join(', ')}</dd>
          </>
        ) : null}
        {ch.when && (
          <>
            <dt>when</dt>
            <dd className="mono">{ch.when}</dd>
          </>
        )}
        <dt>delivery</dt>
        <dd>
          max {ch.maxDeliver || 5} attempts · ack wait {ch.ackWait || '10m'} · retention {ch.retention || '7d'}
        </dd>
        {cs?.subject && (
          <>
            <dt>subject</dt>
            <dd className="mono">{cs.subject}</dd>
          </>
        )}
      </dl>

      {onUpdateChannel && (
        <section className="form">
          <h3>Channel settings</h3>
          <label>
            Type
            <select
              value={type}
              onChange={(event) => onUpdateChannel(ch.id, { type: event.target.value as ChannelType })}
            >
              {(['queue', 'topic', 'fanout', 'broadcast'] as ChannelType[]).map((channelType) => (
                <option key={channelType} value={channelType}>
                  {channelType}
                </option>
              ))}
            </select>
            <FieldIssues issues={issues} field="type" />
          </label>
          {type === 'topic' && (
            <label>
              Topic
              <input
                value={ch.topic ?? ''}
                onChange={(event) => onUpdateChannel(ch.id, { topic: event.target.value })}
                placeholder={ch.id}
              />
              <FieldIssues issues={issues} field="topic" />
            </label>
          )}
          <TypesField channel={ch} onChange={(types) => onUpdateChannel(ch.id, { types })} />
          <label>
            Route condition
            <input
              value={ch.when ?? ''}
              onChange={(event) => onUpdateChannel(ch.id, { when: event.target.value || undefined })}
              placeholder='payload.approved == true'
            />
            <FieldIssues issues={issues} field="when" />
          </label>
          <label className="check">
            <input
              type="checkbox"
              checked={!!ch.approval}
              onChange={(event) => onUpdateChannel(ch.id, { approval: event.target.checked })}
            />
            Require approval before delivery
          </label>
        </section>
      )}

      <section>
        <h3>Consumer groups</h3>
        <table className="table small">
          <thead>
            <tr>
              <th>group</th>
              <th>node</th>
              <th>pending</th>
              <th>in flight</th>
              <th>dead</th>
            </tr>
          </thead>
          <tbody>
            {(cs?.groups ?? []).map((gr) => (
              <tr key={gr.group}>
                <td className="mono">{gr.group}</td>
                <td>
                  {gr.node ?? 'all (competing)'}
                  {gr.replica !== undefined ? ` n${gr.replica}` : ''}
                </td>
                <td className={gr.pending > 0 ? 'warn' : ''}>{gr.pending}</td>
                <td>{gr.inFlight}</td>
                <td className={gr.dead > 0 ? 'bad' : ''}>{gr.dead}</td>
              </tr>
            ))}
            {(cs?.groups ?? []).length === 0 && (
              <tr>
                <td colSpan={5} className="muted">
                  No groups declared yet (graph not deployed).
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      <InjectForm graphId={gid} channelId={ch.id} targets={subs} gated={!!ch.approval} onSent={() => void msgs.reload()} />

      <section>
        <div className="section-head">
          <h3>History</h3>
          <Btn small tone="ghost" onClick={() => void msgs.reload()} aria-label="Refresh">
            <Icon name="refresh" />
          </Btn>
        </div>
        <ErrorBox error={msgs.error} />
        <MessageList records={msgs.data ?? []} onShowCorrelation={onShowCorrelation} />
      </section>
    </div>
  );
}

export function MessageList({
  records,
  onShowCorrelation,
}: {
  records: MessageRecord[];
  onShowCorrelation?: (c: string) => void;
}) {
  if (records.length === 0) return <p className="muted small">No messages.</p>;
  return (
    <ul className="msg-list">
      {records.map((r) => {
        const corr = r.message.metadata?.correlation_id as string | undefined;
        return (
          <li key={`${r.message.id}:${r.channel}:${r.group ?? ''}`}>
            <div className="msg-head">
              <StateDot state={r.status} />
              <code>{r.message.type}</code>
              <span className="muted small">
                {r.message.source || 'external'} → {r.message.target || r.channel}
              </span>
              <span className="muted small">{fmtTime(r.createdAt)}</span>
              <Badge>{stateLabel(r.status)}</Badge>
              {r.attempt > 1 && <Badge tone="warn">attempt {r.attempt}</Badge>}
            </div>
            {r.error && <div className="bad small">{r.error}</div>}
            <JsonView value={r.message.payload} label="payload" />
            <JsonView value={r.message.metadata} label="metadata" />
            {corr && onShowCorrelation && (
              <button type="button" className="link small" onClick={() => onShowCorrelation(corr)}>
                trace workflow {corr.slice(0, 18)}…
              </button>
            )}
          </li>
        );
      })}
    </ul>
  );
}

function InjectForm({
  graphId,
  channelId,
  targets,
  gated,
  onSent,
}: {
  graphId: string;
  channelId: string;
  targets: string[];
  gated: boolean;
  onSent: () => void;
}) {
  const [type, setType] = useState('');
  const [payload, setPayload] = useState('{\n  \n}');
  const [target, setTarget] = useState('');
  const [error, setError] = useState<string>();
  const [ok, setOk] = useState<string>();
  let parsed: unknown;
  let jsonError: string | undefined;
  try {
    parsed = JSON.parse(payload);
  } catch (e) {
    jsonError = (e as Error).message;
  }

  const send = async () => {
    try {
      const m = await api.injectMessage(graphId, channelId, {
        type: type.trim(),
        payload: parsed,
        target: target || undefined,
      });
      setOk(`Injected ${m.id}${gated ? ' – held at the approval gate' : ''}`);
      setError(undefined);
      onSent();
    } catch (e) {
      setError(errorMessage(e));
      setOk(undefined);
    }
  };

  return (
    <section>
      <h3>Inject message</h3>
      <div className="form">
        <label>
          type
          <input value={type} onChange={(e) => setType(e.target.value)} placeholder="feature.request" />
        </label>
        {targets.length > 1 && (
          <label>
            target
            <select value={target} onChange={(e) => setTarget(e.target.value)}>
              <option value="">(channel routing)</option>
              {targets.map((t) => (
                <option key={t}>{t}</option>
              ))}
            </select>
          </label>
        )}
        <label>
          payload (JSON)
          <textarea
            className="mono"
            rows={6}
            value={payload}
            onChange={(e) => setPayload(e.target.value)}
            aria-invalid={!!jsonError}
          />
        </label>
        {jsonError && <div className="bad small">{jsonError}</div>}
        <Btn tone="primary" small disabled={!type.trim() || !!jsonError} onClick={send}>
          Inject
        </Btn>
        {ok && <div className="ok small">{ok}</div>}
        <ErrorBox error={error} />
      </div>
    </section>
  );
}

/** Comma-separated message types. The text is kept as typed so a trailing comma can start the next type. */
function TypesField({ channel, onChange }: { channel: Channel; onChange: (types: string[] | undefined) => void }) {
  const joined = (channel.types ?? []).join(', ');
  const [text, setText] = useState(joined);
  const parsed = (value: string) => value.split(',').map((item) => item.trim()).filter(Boolean);
  useEffect(() => {
    // Replace the text only when the channel changed outside this field.
    setText((current) => (parsed(current).join(', ') === joined ? current : joined));
  }, [channel.id, joined]);
  return (
    <label>
      Message types (comma-separated)
      <input
        value={text}
        onChange={(event) => {
          setText(event.target.value);
          const types = parsed(event.target.value);
          onChange(types.length ? types : undefined);
        }}
        placeholder="task.*, review.*"
      />
    </label>
  );
}
