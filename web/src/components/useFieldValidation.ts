import {useEffect, useRef, useState} from 'react';

/** Reveal existing domain validation only after an explicit save/review attempt. */
export function useFieldValidation<T extends string>(issues: Partial<Record<T, string | undefined>>) {
  const [attempt, setAttempt] = useState(0);
  const root = useRef<HTMLElement | null>(null);
  const pendingFocus = useRef(false);
  // A route/disclosure can mount the field after the validation commit.
  // Keep one focus request until it appears; edits must not repeatedly steal focus.
  useEffect(() => {
    if (!pendingFocus.current) return;
    const field = root.current?.querySelector<HTMLElement>('[aria-invalid="true"]');
    if (!field) return;
    let parent = field.parentElement;
    while (parent && parent !== root.current) {
      if (parent instanceof HTMLDetailsElement) parent.open = true;
      parent = parent.parentElement;
    }
    pendingFocus.current = false;
    field.focus();
  });
  return {
    ref: (element: HTMLElement | null) => { root.current = element; },
    errors: attempt ? issues : {} as Partial<Record<T, string | undefined>>,
    validate: () => {
      const invalid = Object.values(issues).some(Boolean);
      pendingFocus.current = invalid;
      setAttempt(value => value + 1);
      return !invalid;
    },
  };
}
