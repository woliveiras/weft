import type { InstructionEntry, InstructionSnapshot } from "../api/instructions";

const policyLabels = {
  graph_off: "Disabled by graph",
  agent_disabled: "Disabled by agent",
  agent_restricted: "Inherited with agent restrictions",
  inherited: "Inherited from graph",
};

function Provenance({ entry }: { entry: InstructionEntry }) {
  return (
    <article>
      <h4>{entry.order !== undefined && `${entry.order}. `}{entry.scope} · {entry.source}</h4>
      <p>
        {entry.stage && `${entry.stage} · `}
        {entry.included ? (entry.delivery || "Included") : `Omitted: ${entry.reason}`}
        {entry.revision ? ` · v${entry.revision}` : ""}
      </p>
      {entry.description && <p>{entry.description}</p>}
      {entry.canonical && entry.canonical !== entry.source && <p>Canonical path <code>{entry.canonical}</code></p>}
      {entry.lockStatus && <p>Lock {entry.lockStatus}{entry.declaredVersion && <> · <code>{entry.declaredVersion}</code></>}</p>}
      {entry.sha256 && <p>SHA-256 <code>{entry.sha256}</code></p>}
      {entry.body && <pre style={{ whiteSpace: "pre-wrap" }}>{entry.body}</pre>}
    </article>
  );
}

export function InstructionManifest({ snapshot }: { snapshot: InstructionSnapshot }) {
  return (
    <section aria-label="Instruction manifest">
      <h3>{snapshot.executionId ? `Execution ${snapshot.executionId}` : "Current saved configuration (not an execution)"}</h3>
      {snapshot.repositoryPolicy && <p>Repository guidance · {policyLabels[snapshot.repositoryPolicy]}</p>}
      {!!snapshot.blockedSources?.length && <p>Blocked sources · {snapshot.blockedSources.join(", ")}</p>}
      <p>Remaining conservative guidance budget · {snapshot.budget} bytes</p>
      {snapshot.entries.length === 0 && <p>No guidance entries recorded.</p>}
      {snapshot.entries.map((entry, index) => <Provenance key={entry.record ?? index} entry={entry}/>)}
      {!!snapshot.reads?.length && (
        <section aria-label="On-demand reads">
          <h3>On-demand reads</h3>
          {snapshot.reads.map((entry, index) => <Provenance key={entry.record ?? index} entry={entry}/>)}
        </section>
      )}
    </section>
  );
}
