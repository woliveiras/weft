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
export interface InstructionRefs {
  skillIds?: string[];
  projectSteering?: boolean;
  installationDefaults?: boolean;
}
export interface InstructionSnapshot {
  graphId: string;
  nodeId: string;
  executionId?: string;
  budget: number;
  entries: {
    scope: string;
    source: string;
    revision?: number;
    sha256?: string;
    body?: string;
    included: boolean;
    reason?: string;
  }[];
}
