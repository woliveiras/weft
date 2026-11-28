import type {KeyboardEvent} from 'react';

export function handleChatSubmitShortcut(event: KeyboardEvent, submit: () => void) {
  if (event.key !== 'Enter' || !(event.ctrlKey || event.metaKey)) return;
  if (event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return;

  event.preventDefault();
  event.stopPropagation();
  if (!event.repeat) submit();
}
