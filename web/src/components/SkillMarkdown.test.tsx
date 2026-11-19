import {cleanup,render,screen} from '@testing-library/react';
import {afterEach,expect,it} from 'vitest';
import {SkillMarkdown} from './SkillMarkdown';
afterEach(cleanup);
it('previews headings, emphasis, lists and code without executing embedded content',()=>{
  const {container}=render(<SkillMarkdown body={'# Review\n**Security**\n- Verify inputs\n- Check grants\n```html\n<img src=x onerror=alert(1)>\n```\n<script>bad()</script>\n![image](https://example.com/tracker)\n[bad](javascript:alert(1))\n[Docs](https://example.com/docs)'}/>);
  expect(screen.getByRole('heading',{name:'Review'})).toBeTruthy();expect(container.querySelector('strong')?.textContent).toBe('Security');
  expect(screen.getAllByRole('listitem')).toHaveLength(2);
  expect(container.querySelector('script,img')).toBeNull();expect(container.querySelector('a[href^="javascript"]')).toBeNull();
  expect(screen.getByRole('link',{name:'Docs'}).getAttribute('rel')).toContain('noopener');
});
it('explains empty text and handles unfinished fenced code',()=>{
  const {rerender,container}=render(<SkillMarkdown body=""/>);expect(screen.getByText('Nothing to preview yet.')).toBeTruthy();
  rerender(<SkillMarkdown body={'```\nconst value = 1'}/>);expect(container.querySelector('code')?.textContent).toBe('const value = 1');
});
