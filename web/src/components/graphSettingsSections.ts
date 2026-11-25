export const settingsSections = {
  general: 'General',
  task: 'Task defaults',
  workspace: 'Workspace',
  environment: 'Environment',
  network: 'Network',
  triggers: 'Triggers',
  parameters: 'Parameters',
  safety: 'Safety',
} as const;

export type SettingsSection = keyof typeof settingsSections;
export const settingsSectionIds = Object.keys(settingsSections) as SettingsSection[];

export function settingsSection(value: string | undefined): SettingsSection {
  return settingsSectionIds.find((id) => id === value) ?? 'general';
}

export function settingsPath(graphId: string, section: SettingsSection = 'general'): string {
  return `#/g/${encodeURIComponent(graphId)}/settings/${section}`;
}

export function settingsSectionForIssue(path: string): SettingsSection {
  const matches = (field: string) =>
    path === field || path.startsWith(`${field}.`) || path.startsWith(`${field}[`);

  if (matches('spec.environment.network') || matches('spec.network')) return 'network';
  if (matches('spec.environment')) return 'environment';
  if (matches('spec.workspace')) return 'workspace';
  if (matches('spec.triggers') || matches('spec.issueTriggers') || matches('triggers')) return 'triggers';
  if (matches('spec.parameters')) return 'parameters';
  if (matches('spec.task')) return 'task';
  if (matches('spec.safety') || matches('spec.defaults')) return 'safety';
  return 'general';
}
