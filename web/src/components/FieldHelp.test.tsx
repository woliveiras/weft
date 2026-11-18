import { fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { HelpField } from './FieldHelp';

const dialogMethod = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal');

afterEach(() => {
  if (dialogMethod) Object.defineProperty(HTMLDialogElement.prototype, 'showModal', dialogMethod);
  else Reflect.deleteProperty(HTMLDialogElement.prototype, 'showModal');
});

it('opens separate field help inside a read-only form and returns focus without editing', () => {
  // jsdom does not implement the browser's dialog top layer.
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
    configurable: true,
    value(this: HTMLDialogElement) { this.setAttribute('open', ''); },
  });
  const onChange = vi.fn();
  const onSubmit = vi.fn(event => event.preventDefault());
  render(<form onSubmit={onSubmit}>
    <fieldset disabled>
      <HelpField label="Required" help="The workflow needs a nonblank value.">
        <input type="checkbox" onChange={onChange} />
      </HelpField>
    </fieldset>
  </form>);
  const field = screen.getByRole<HTMLInputElement>('checkbox', { name: 'Required' });
  expect(field.matches(':disabled')).toBe(true);
  const help = screen.getByRole('button', { name: 'Help for Required' });
  help.focus();
  fireEvent.keyDown(help, { key: ' ' });
  const popup = screen.getByRole('dialog', { name: 'Required' });
  expect(within(popup).getByText('The workflow needs a nonblank value.')).toBeTruthy();
  expect((within(popup).getByRole<HTMLButtonElement>('button', { name: 'Close' })).matches(':disabled')).toBe(false);
  fireEvent(popup, new Event('close'));
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(document.activeElement).toBe(help);
  expect(field.checked).toBe(false);
  expect(onChange).not.toHaveBeenCalled();
  expect(onSubmit).not.toHaveBeenCalled();
});
