import {cleanup, fireEvent, render, screen} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import {useState} from 'react';
import {HelpField} from './FieldHelp';
import {useFieldValidation} from './useFieldValidation';

afterEach(cleanup);

it('reveals errors after review, opens the owning section, and focuses the first invalid field',()=>{
 const save=vi.fn();
 function Form(){
  const [name,setName]=useState('');
  const [amount,setAmount]=useState('');
  const validation=useFieldValidation({name:!name?'Choose a name.':undefined,amount:!amount?'Enter an amount.':undefined});
  return <form ref={validation.ref} onSubmit={event=>{event.preventDefault();if(validation.validate())save();}}>
   <details><summary>Settings</summary><HelpField label="Name" help="Name" error={validation.errors.name}><input value={name} onChange={event=>setName(event.target.value)}/></HelpField></details>
   <HelpField label="Amount" help="Amount" error={validation.errors.amount}><input value={amount} onChange={event=>setAmount(event.target.value)}/></HelpField>
   <button>Review</button>
  </form>;
 }
 render(<Form/>);
 expect(screen.queryByRole('alert')).toBeNull();
 fireEvent.click(screen.getByRole('button',{name:'Review'}));
 expect(save).not.toHaveBeenCalled();
 expect(screen.getByText('Settings').closest('details')?.open).toBe(true);
 expect(document.activeElement).toBe(screen.getByLabelText('Name'));
 fireEvent.change(screen.getByLabelText('Name'),{target:{value:'New'}});
 expect(screen.queryByText('Choose a name.')).toBeNull();
 expect(screen.getByLabelText('Amount').getAttribute('aria-invalid')).toBe('true');
 fireEvent.click(screen.getByRole('button',{name:'Review'}));
 expect(document.activeElement).toBe(screen.getByLabelText('Amount'));
 fireEvent.change(screen.getByLabelText('Amount'),{target:{value:'2'}});
 fireEvent.click(screen.getByRole('button',{name:'Review'}));
 expect(save).toHaveBeenCalledOnce();
});

it('keeps a single focus request until a deferred panel mounts',()=>{
 function Form(){
  const [visible,setVisible]=useState(false);
  const [value,setValue]=useState('');
  const validation=useFieldValidation({name:!value?'Enter a name.':undefined});
  return <div ref={validation.ref}>
   <button onClick={()=>validation.validate()}>Save</button>
   <button onClick={()=>setVisible(true)}>Mount content</button>
   <button onClick={()=>setValue('Changed externally')}>Change elsewhere</button>
   {visible&&<HelpField label="Name" help="Name" error={validation.errors.name}><input value={value} onChange={event=>setValue(event.target.value)}/></HelpField>}
  </div>;
 }
 render(<Form/>);
 fireEvent.click(screen.getByRole('button',{name:'Save'}));
 fireEvent.click(screen.getByRole('button',{name:'Mount content'}));
 expect(document.activeElement).toBe(screen.getByLabelText('Name'));
 const elsewhere=screen.getByRole('button',{name:'Change elsewhere'});
 elsewhere.focus();
 fireEvent.click(elsewhere);
 expect(document.activeElement).toBe(elsewhere);
 expect(screen.queryByRole('alert')).toBeNull();
});
