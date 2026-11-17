import type { Graph } from '../api/types';

export const guideSteps = [
  { id: 'concepts', title: 'Understand the workspace', text: 'A graph coordinates agents. Each agent performs work; channels carry messages between agents. Tools give agents specific capabilities. An execution is one recorded turn. An approval pauses protected work for a human decision.' },
  { id: 'health', title: 'Check installation health', text: 'Open System health to inspect services and failures. A reachable API alone does not prove that every dependency is healthy.' },
  { id: 'models', title: 'Choose a model', text: 'In Models, configure or select a provider and inspect its test result. Listing models is not proof of successful inference. Choose a model in Agent configuration → General, then inspect an actual execution. The echo example needs no model and does not validate inference.' },
  { id: 'connections', title: 'Connect tools and grant access', text: 'Connections belong to the installation. Saving an enabled, authorized MCP connection discovers tools; OAuth may require consent first. Inspect discovery errors and retry there. Discovery and OAuth never grant agent access. In Agent configuration → Tools & permissions, select only the tools needed and review the graph before saving. No test agent is required to discover tools. This step is optional for the echo example.' },
  { id: 'graph', title: 'Create and review a graph', text: 'Create the safe echo example below, or open an existing graph. On the canvas, use Add agent, configure its instructions and runtime, and drag between agent handles to create a channel. Review channel settings. Validate and review/save the graph before running it. Existing drafts stay in their graph while you visit this guide.' },
  { id: 'run', title: 'Run and inspect results', text: 'For the echo example, select Start on its graph card or Run all on the canvas. It runs once without a model or external tools. Open Runs, select the execution, and inspect Result, Timeline and Tool calls. Completed does not guarantee every tool succeeded. For other agents, review instructions, grants and possible costs before starting. Messages and Activity expose related work; Approvals contains pending decisions. Files contains workspace files and Git changes when available. Delegation and first-class task submission are separate workstreams, not prerequisites for this guide.' },
] as const;
export type GuideStep = typeof guideSteps[number]['id'];
export interface GuideProgress { version: 1; current: number; reviewed: GuideStep[] }
export const freshProgress = (): GuideProgress => ({ version: 1, current: 0, reviewed: [] });
export function parseProgress(raw: string | null): GuideProgress {
  if (!raw) return freshProgress();
  const value = JSON.parse(raw);
  if (value?.version !== 1 || !Number.isInteger(value.current) || value.current < 0 || value.current >= guideSteps.length ||
      !Array.isArray(value.reviewed) || value.reviewed.some((id: unknown) => !guideSteps.some(s => s.id === id))) {
    throw new Error('Saved guide progress is invalid or from an unsupported version. Restart the guide to replace it.');
  }
  return { version: 1, current: value.current, reviewed: [...new Set<GuideStep>(value.reviewed)] };
}
// No credentials, graph IDs, connection IDs, or payloads are stored here.
export const progressKey = (name: string, role: string) => 'weft.guide.v1:' + JSON.stringify([name, role]);
export function reviewStep(progress: GuideProgress): GuideProgress {
  return { ...progress, reviewed: [...new Set([...progress.reviewed, guideSteps[progress.current].id])],
    current: Math.min(progress.current + 1, guideSteps.length - 1) };
}
export function exampleGraph(id: string): Graph {
  return { apiVersion: 'weft.io/v1', kind: 'Graph', metadata: { id, name: 'First steps · Echo', description: 'Optional onboarding example. No model or external tools; echo forwards messages without accessing repositories.' },
    spec: { nodes: [{ id: 'echo', name: 'Echo', runtime: { kind: 'echo' }, prompt: 'Hello from Weft.', loop: { mode: 'once' }, tools: [] }], channels: [] } };
}
