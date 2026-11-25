import { Children, cloneElement, isValidElement, useEffect, useId, useRef, useState } from 'react';
import type { ReactElement, ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { CircleHelp } from 'lucide-react';
import { Btn } from './common';

export function FieldHelp({ label, children }: { label: string; children: ReactNode }) {
  const id = useId();
  const trigger = useRef<HTMLAnchorElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    if (open && dialog.current && !dialog.current.open) dialog.current.showModal();
  }, [open]);

  return <>
    {/* A link with button semantics stays available inside a disabled form fieldset. */}
    <a ref={trigger} href={`#${id}`} role="button" className="field-help-trigger"
      aria-label={`Help for ${label}`} aria-haspopup="dialog" aria-expanded={open}
      onClick={event => {
        event.preventDefault();
        setOpen(true);
      }}
      onKeyDown={event => {
        if (event.key === ' ') {
          event.preventDefault();
          setOpen(true);
        }
      }}>
      <CircleHelp size={16} strokeWidth={1.75} aria-hidden="true" />
    </a>
    {open && createPortal(
      <dialog ref={dialog} id={id} className="field-help-dialog"
        aria-labelledby={`${id}-title`} aria-describedby={`${id}-body`}
        onKeyDown={event => event.stopPropagation()}
        onClose={() => {
          setOpen(false);
          trigger.current?.focus();
        }}>
        <h2 id={`${id}-title`}>{label}</h2>
        <div id={`${id}-body`} className="field-help-body">{children}</div>
        <Btn autoFocus onClick={() => dialog.current?.close()}>Close</Btn>
      </dialog>,
      document.body,
    )}
  </>;
}

type ControlProps = { id?: string; type?: string; value?: string | number | readonly string[]; checked?: boolean; 'aria-describedby'?: string; 'aria-invalid'?: boolean | 'true' | 'false' | 'grammar' | 'spelling' };

/** One form control with a label and a separate action to read field help. */
export function HelpField({ label, help, descriptionId, error, className = '', children }: {
  label: string;
  help: ReactNode;
  descriptionId?: string;
  error?: string;
  className?: string;
  children: ReactNode;
}) {
  const id = useId();
  const content = Children.toArray(children);
  const container = useRef<HTMLDivElement>(null);
  const [nativeError, setNativeError] = useState<string>();
  const control = content.find((child): child is ReactElement<ControlProps> =>
    isValidElement<ControlProps>(child) && ['input', 'select', 'textarea'].includes(String(child.type)));
  const controlId = control?.props.id || id;
  const message = error || nativeError;
  const errorId = message ? `${id}-error` : undefined;
  const describedBy = [control?.props['aria-describedby'], descriptionId, errorId].filter(Boolean).join(' ') || undefined;
  const named = control && cloneElement(control, { id: controlId, 'aria-describedby': describedBy, 'aria-invalid': message ? true : control.props['aria-invalid'] });
  useEffect(() => {
    if (!nativeError) return;
    const field = container.current?.querySelector('input, select, textarea');
    if (field instanceof HTMLInputElement || field instanceof HTMLSelectElement || field instanceof HTMLTextAreaElement) {
      setNativeError(field.validity.valid ? undefined : field.validationMessage);
    }
  }, [control?.props.value, control?.props.checked, nativeError]);
  const checkbox = control?.props.type === 'checkbox' || control?.props.type === 'radio';

  return <div ref={container} className={`help-field ${className}`}
    onBlurCapture={event => {
      const target = event.target;
      if ((target instanceof HTMLInputElement || target instanceof HTMLSelectElement || target instanceof HTMLTextAreaElement) && target.willValidate && !target.validity.valid) {
        setNativeError(target.validationMessage);
      }
    }}
    onInvalidCapture={event => {
      const target = event.target;
      if (target instanceof HTMLInputElement || target instanceof HTMLSelectElement || target instanceof HTMLTextAreaElement) {
        setNativeError(target.validationMessage);
      }
    }}
    onChangeCapture={event => {
      const target = event.target;
      if (nativeError && (target instanceof HTMLInputElement || target instanceof HTMLSelectElement || target instanceof HTMLTextAreaElement)) {
        setNativeError(target.validity.valid ? undefined : target.validationMessage);
      }
    }}>
    <div className="field-help-caption">
      {checkbox && named}
      <label htmlFor={controlId}>{label}</label>
      <FieldHelp label={label}>{help}</FieldHelp>
    </div>
    {content.map(child => child === control ? (checkbox ? null : named) : child)}
    {message && <p className="field-error" id={errorId} role="alert">{message}</p>}
  </div>;
}
