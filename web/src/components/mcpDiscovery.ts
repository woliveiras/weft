import { api, errorMessage } from '../api/client';
import type { MCPConnection, MCPConnectionInput } from '../api/types';

export function canDiscover(connection: MCPConnection) {
  return connection.enabled && (!connection.oauth || connection.oauth.status === 'authorized');
}

/** Save errors reject; discovery errors never turn a successful save into a failure. */
export async function saveAndDiscover(input: MCPConnectionInput, revision: number | undefined,
  saved: (connection: MCPConnection) => void,
  client = {save: api.saveMCP, refresh: api.refreshMCP}) {
  const connection = await client.save(input, revision);
  saved(connection);
  if (!canDiscover(connection)) return {connection};
  try {
    return {connection: await client.refresh(connection.id, 'tools/refresh')};
  } catch (error) {
    return {connection, discoveryError: errorMessage(error)};
  }
}
