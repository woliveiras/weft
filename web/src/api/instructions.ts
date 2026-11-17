export interface SkillInput {
  id: string;
  title: string;
  description: string;
  body: string;
  enabled: boolean;
  global: boolean;
}
export interface SkillVersion extends SkillInput {
  revision: number;
  actor: string;
  at: string;
  deleted: boolean;
}
export interface Skill extends SkillVersion {
  history: SkillVersion[];
  /** Read-only role skill shipped with Weft (IDs starting with weft-). */
  builtin?: boolean;
}
export const repositorySources = ["agents", "claude", "github"] as const;
export type RepositorySource = (typeof repositorySources)[number];
export interface RepositoryGuidancePolicy {
  disabled?: boolean;
  blockedSources?: RepositorySource[];
}
export interface InstructionRefs {
  repositoryGuidance?: RepositoryGuidancePolicy;
  skillIds?: string[];
  projectSteering?: boolean;
  installationDefaults?: boolean;
}
export interface InstructionEntry {
  scope: string;
  source: string;
  revision?: number;
  sha256?: string;
  body?: string;
  included: boolean;
  reason?: string;
  order?: number;
  record?: number;
  stage?: string;
  delivery?: string;
  selection?: string;
  description?: string;
  canonical?: string;
  declaredVersion?: string;
  lockStatus?: string;
}
export interface InstructionSnapshot {
  graphId: string;
  nodeId: string;
  executionId?: string;
  budget: number;
  repositoryPolicy?: "graph_off" | "agent_disabled" | "agent_restricted" | "inherited";
  blockedSources?: RepositorySource[];
  entries: InstructionEntry[];
  reads?: InstructionEntry[];
}
