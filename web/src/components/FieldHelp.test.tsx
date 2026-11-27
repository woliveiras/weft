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

it('associates inline errors with the control and clears them without dropping existing descriptions',()=>{
 const {rerender}=render(<><p id="hint">Existing hint</p><p id="context">Context</p><HelpField label="Entry" help="Choose an agent." descriptionId="context" error="An entry is required."><select id="entry" aria-describedby="hint"><option value="">Choose</option></select></HelpField></>);
 const control=screen.getByRole('combobox',{name:'Entry'});
 const error=screen.getByText('An entry is required.');
 expect(control.id).toBe('entry');
 expect(control.getAttribute('aria-invalid')).toBe('true');
 expect(control.getAttribute('aria-describedby')?.split(' ')).toEqual(['hint','context',error.id]);
 rerender(<><p id="hint">Existing hint</p><p id="context">Context</p><HelpField label="Entry" help="Choose an agent." descriptionId="context"><select id="entry" aria-describedby="hint"><option value="worker">Worker</option></select></HelpField></>);
 expect(control.getAttribute('aria-invalid')).toBeNull();
 expect(control.getAttribute('aria-describedby')).toBe('hint context');
 expect(screen.queryByText('An entry is required.')).toBeNull();
});

it('preserves caller-provided invalid state when no inline error is supplied',()=>{
 render(<HelpField label="Name" help="Choose a name."><input aria-invalid="grammar"/></HelpField>);
 expect(screen.getByRole('textbox',{name:'Name'}).getAttribute('aria-invalid')).toBe('grammar');
});

it('shows native constraint errors only after interaction and keeps callbacks and optional fields intact',()=>{
 const changed=vi.fn();const invalid=vi.fn();
 const {rerender}=render(<HelpField label="Port" help="A number"><input type="number" required min={1} max={90} value={100} onChange={changed} onInvalid={invalid}/></HelpField>);
 const field=screen.getByLabelText('Port');
 expect(field.getAttribute('aria-invalid')).toBeNull();
 fireEvent.invalid(field);
 expect(invalid).toHaveBeenCalledOnce();
 expect(screen.getByRole('alert').textContent).toBe('Enter a value of 90 or lower.');
 expect(field.getAttribute('aria-invalid')).toBe('true');
 expect(field.getAttribute('aria-describedby')).toBe(screen.getByRole('alert').id);
 fireEvent.change(field,{target:{value:'99'}});
 expect(changed).toHaveBeenCalledOnce();
 expect(field.getAttribute('aria-invalid')).toBe('true');
 rerender(<HelpField label="Port" help="A number"><input type="number" required min={1} max={90} value={50} onChange={changed}/></HelpField>);
 expect(field.getAttribute('aria-invalid')).toBeNull();
 expect(screen.queryByRole('alert')).toBeNull();
 rerender(<HelpField label="Port" help="Optional"><input type="number" min={1} value="" onChange={changed}/></HelpField>);
 fireEvent.blur(field);
 expect(field.getAttribute('aria-invalid')).toBeNull();
});

it('uses English inline copy for built-in constraints and preserves custom validation details',()=>{
 render(<HelpField label="Email" help="Account email"><input type="email" required/></HelpField>);
 const field=screen.getByRole<HTMLInputElement>('textbox',{name:'Email'});
 Object.defineProperty(field,'validationMessage',{configurable:true,value:'Preencha este campo.'});
 fireEvent.blur(field);
 expect(screen.getByRole('alert').textContent).toBe('Fill in this field.');
 fireEvent.change(field,{target:{value:'invalid'}});
 expect(screen.getByRole('alert').textContent).toBe('Enter a valid email address.');
 Reflect.deleteProperty(field,'validationMessage');
 field.setCustomValidity('Use your verified email address.');
 fireEvent.invalid(field);
 expect(screen.getByRole('alert').textContent).toBe('Use your verified email address.');
 field.setCustomValidity('');
 fireEvent.change(field,{target:{value:'reader@example.com'}});
 expect(screen.queryByRole('alert')).toBeNull();
});
