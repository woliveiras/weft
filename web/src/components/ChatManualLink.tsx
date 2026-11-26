import { useState } from 'react';
import { api, errorMessage } from '../api/client';
import { Btn, ErrorBox } from './common';
import { downloadYaml } from './downloadYaml';
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
  if (destination === 'configuration-export') return <InstallationDownload/>;
  const target = destinations.get(destination);
  if (!target) return null;
  const [label, href] = target;
  const anchor = `chat-navigation-${message}`;
  return <a id={anchor} href={href} onClick={() => {
    if (destination === 'connections' || destination === 'connection-setup') leaveChatForConnection(location.hash, message);
    onView(anchor);
  }}>Open {label}</a>;
}

function InstallationDownload() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  async function download() {
    setBusy(true);
    setError(undefined);
    try {
      downloadYaml(await api.exportInstallation(), 'installation.yaml');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }
  return <><Btn disabled={busy} onClick={() => void download()}>Download installation YAML</Btn>
    {error && <ErrorBox error={error}/>}</>;
}
