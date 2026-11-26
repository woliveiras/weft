import { memo } from 'react';
import Markdown from 'react-markdown';

// Navigation and action controls are rendered from verified Chat metadata.
const elements = ['p', 'strong', 'em', 'ol', 'ul', 'li', 'code', 'pre', 'blockquote',
  'br', 'hr', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'img'];

// The transcript refreshes while a session is open. Parse each reply again only when its text changes.
export const ChatMarkdown = memo(function ChatMarkdown({ text }: { text: string }) {
  return <div className="chat-markdown">
    <Markdown allowedElements={elements} unwrapDisallowed components={{img: ({alt}) => <>{alt}</>}}>
      {text}
    </Markdown>
  </div>;
});
