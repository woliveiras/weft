import { afterEach, expect, it } from 'vitest';
import { leaveChatForConnection, leaveConnections, saveChatOAuthReturn, takeChatOAuthReturn } from './chatConnectionReturn';

const chat = 'a'.repeat(32), message = 'reply-' + 'b'.repeat(32), state = 'c'.repeat(43);
afterEach(() => { leaveConnections(); sessionStorage.clear(); });
it('binds a same-tab return to the connection, callback and original message once', async () => {
  leaveChatForConnection('#/chat/' + chat, message);
  await saveChatOAuthReturn('selected', state);
  expect(JSON.stringify(sessionStorage)).not.toContain(state);
  expect(await takeChatOAuthReturn('another', state)).toBeUndefined();
  expect(await takeChatOAuthReturn('selected', 'd'.repeat(43))).toBeUndefined();
  expect(await takeChatOAuthReturn('selected', state)).toEqual({ chat, message });
  expect(await takeChatOAuthReturn('selected', state)).toBeUndefined();
});
it('ignores external return URLs, malformed storage and unrelated navigation', async () => {
  leaveChatForConnection('https://example.org/#/chat/' + chat, message);
  await saveChatOAuthReturn('selected', state);
  expect(sessionStorage.length).toBe(0);
  leaveChatForConnection('#/chat/' + chat, message);
  leaveConnections();
  await saveChatOAuthReturn('selected', state);
  expect(await takeChatOAuthReturn('selected', state)).toBeUndefined();
  sessionStorage.setItem('weft.chat.connectionReturn', 'null');
  expect(await takeChatOAuthReturn('selected', state)).toBeUndefined();
});
