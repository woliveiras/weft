// Only the in-flight return needs to survive the trip to the provider. Store
// its state fingerprint and two console IDs, never a token, callback or URL.
const storageKey = 'weft.chat.connectionReturn';
const chatID = /^[0-9a-f]{32}$/;
const replyID = /^reply-[0-9a-f]{32}$/;
type Origin = {chat: string; message: string};
type Pending = Origin & {connection: string; fingerprint: string; expires: number};
let origin: Origin | undefined;
let focus: Origin | undefined;

export function leaveChatForConnection(hash: string, message: string) {
  const match = /^#\/chat\/([0-9a-f]{32})(?:\?archived=1)?$/.exec(hash);
  origin = match && replyID.test(message) ? {chat: match[1], message} : undefined;
}

export function leaveConnections() {
  origin = undefined;
}

async function fingerprint(state: string) {
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(state));
  return Array.from(new Uint8Array(bytes), byte => byte.toString(16).padStart(2, '0')).join('');
}

export async function saveChatOAuthReturn(connection: string, state: string) {
  const from = origin;
  try {
    sessionStorage.removeItem(storageKey);
    if (!from || !/^[A-Za-z0-9_-]{43}$/.test(state)) return;
    const pending: Pending = {...from, connection, fingerprint: await fingerprint(state), expires: Date.now() + 15 * 60_000};
    sessionStorage.setItem(storageKey, JSON.stringify(pending));
  } catch {
    // Browsers that disable tab storage or Web Crypto still complete OAuth.
  }
}

export async function takeChatOAuthReturn(connection: string, state: string): Promise<Origin | undefined> {
  try {
    const raw = sessionStorage.getItem(storageKey);
    if (!raw) return;
    const p: Pending = JSON.parse(raw);
    if (!p || !chatID.test(p.chat) || !replyID.test(p.message) || !Number.isFinite(p.expires) || p.expires <= Date.now()) {
      sessionStorage.removeItem(storageKey);
      return;
    }
    if (connection !== p.connection || await fingerprint(state) !== p.fingerprint) return;
    sessionStorage.removeItem(storageKey);
    return {chat: p.chat, message: p.message};
  } catch { return; }
}

export function focusOnChatReturn(from: Origin) { focus = from; }
export function takeChatReturnFocus(id: string) {
  if (focus?.chat !== id) return;
  const target = 'chat-navigation-' + focus.message;
  focus = undefined;
  return target;
}
