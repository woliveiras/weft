import { describe, expect, it } from 'vitest';
import { mcpTemplateLink, parseMCPLink } from './mcpLinks';
describe('MCP connection links', () => {
 it('round trips a pinned template without credentials or endpoint overrides', () => {
  const ref = {id:'deepwiki', version:1};
  expect(parseMCPLink(mcpTemplateLink(ref).split('?')[1])).toEqual(ref);
 });
 it.each(['','template=deepwiki','template=deepwiki&version=0','template=deepwiki&version=1&token=secret','template=deepwiki&version=1&endpoint=https://evil.example','template=a&template=b&version=1','template=a&version=1&version=2','template=%00&version=1','template=a&version=1.5', 'template=a&version=9999999'])( 'rejects invalid input %s', query => {
  expect(() => parseMCPLink(query)).toThrow();
 });
});
