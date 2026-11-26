import { render, screen, within } from '@testing-library/react';
import { expect, it } from 'vitest';
import { ChatMarkdown } from './ChatMarkdown';

it('renders the titles and IDs of an ordered task list inside their respective items', () => {
  render(<ChatMarkdown text={'Duas tarefas.\n\n1. **Título:** Primeiro\n   **ID:** `tsk_1`\n\n2. **Título:** Segundo\n   **ID:** `tsk_2`'}/>);
  const items = within(screen.getByRole('list')).getAllByRole('listitem');
  expect(items).toHaveLength(2);
  expect(items[0].querySelector('strong')?.textContent).toBe('Título:');
  expect(items.map(item => item.querySelector('code')?.textContent)).toEqual(['tsk_1', 'tsk_2']);
});

it('keeps HTML, image destinations and model-provided navigation inert', () => {
  const { container, rerender } = render(<ChatMarkdown text={'<button>Run</button>\n\n![diagram](https://example.com/image)\n\n[link](https://example.com) [action](javascript:alert(1))'}/>);
  expect(container.querySelector('button, a, img')).toBeNull();
  expect(container.textContent).toContain('diagram');
  expect(container.textContent).toContain('link');
  rerender(<ChatMarkdown text={'```html\n<img src="example">\n```'}/>);
  expect(container.querySelector('pre code')?.textContent?.trim()).toBe('<img src="example">');
});
