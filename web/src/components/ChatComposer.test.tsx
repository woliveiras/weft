import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { ChatComposer } from './ChatComposer';

afterEach(cleanup);
it('completes authorized commands without sending and submits only explicit shortcuts', () => {
  const submit = vi.fn();
  function Editor() {
    const [draft, change] = useState('/');
    return <ChatComposer draft={draft} onChange={change} onSend={submit} sending={false} disabled={!draft.trim()} admin={false} choicePending={false}/>;
  }
  render(<Editor/>);
  const field = screen.getByRole('textbox',{name:'Message'});
  fireEvent.focus(field);
  expect(screen.getAllByRole('option')).toHaveLength(2);
  fireEvent.keyDown(field,{key:'Enter'});
  expect(field).toHaveProperty('value','/inspect ');
  fireEvent.click(screen.getByRole('option',{name:'/inspect tasks'}));
  expect(field).toHaveProperty('value','/inspect tasks');
  fireEvent.keyDown(field,{key:'Enter'});
  expect(submit).toHaveBeenCalledTimes(0);
  for (const modifier of ['ctrlKey','metaKey']) fireEvent.keyDown(field,{key:'Enter',[modifier]:true});
  expect(submit).toHaveBeenCalledTimes(2);
  fireEvent.change(field,{target:{value:' '}});
  fireEvent.keyDown(field,{key:'Enter',ctrlKey:true});
  expect(submit).toHaveBeenCalledTimes(2);
});
