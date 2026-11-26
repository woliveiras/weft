import { leaveChatForConnection } from './chatConnectionReturn';

const destinations = new Map<string, [string, string]>([
  ['tasks', ['Tasks', '#/tasks']],
  ['workflows', ['Workflows', '#/graphs']],
  ['agents', ['Agents', '#/agents']],
  ['projects', ['Projects', '#/projects']],
  ['activity', ['Activity', '#/activity']],
  ['approvals', ['Approvals', '#/approvals']],
  ['models', ['Providers', '#/models']],
  ['chat-model', ['Chat model', '#/models/chat']],
  ['plugins', ['Inference plugins', '#/models/plugins']],
  ['usage', ['Usage and costs', '#/models/usage']],
  ['budgets', ['Budgets', '#/models/budgets']],
  ['prices', ['Model prices', '#/models/prices']],
  ['connections', ['Connections', '#/connections']],
  ['connection-setup', ['Connection setup', '#/connections/new']],
  ['skills', ['Skills and instructions', '#/skills']],
  ['administration', ['Administration', '#/administration/health']],
]);

export function ChatManualLink({ destination, message, onView }: {
  destination: string;
  message: string;
  onView: (anchor: string) => void;
}) {
  const target = destinations.get(destination);
  if (!target) return null;
  const [label, href] = target;
  const anchor = `chat-navigation-${message}`;
  return <a id={anchor} href={href} onClick={() => {
    if (destination === 'connections' || destination === 'connection-setup') leaveChatForConnection(location.hash, message);
    onView(anchor);
  }}>Open {label}</a>;
}
