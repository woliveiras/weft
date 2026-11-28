import {useId, useLayoutEffect, useRef} from 'react';
import {handleChatSubmitShortcut} from './chatSubmitShortcut';
import {ChatResourcePicker} from './ChatResourcePicker';
import {guideAction, guideActions, moreCategories, primaryCategories, type ChatCategory, type GuideDraft} from './chatGuide';

export function ChatActionGuide({draft, admin, onChange, onSendPrepared}: {
  draft:GuideDraft; admin:boolean; onChange:(draft:GuideDraft)=>void; onSendPrepared?:()=>void;
}) {
  const groupId = useId();
  const root = useRef<HTMLElement>(null);
  const focusAfter = useRef<string | null>(null);
  const lastCandidate = useRef<string | undefined>(undefined);
  const returnLink = useRef<HTMLAnchorElement | null>(null);
  const action = guideAction(draft,admin);
  const category = draft.category;
  const target = draft.target;
  const picking = action?.kind === 'pick' || action?.kind === 'project';
  const readyPrompt = action?.kind === 'prompt' && !action.detailsHint;

  // Chat stays mounted in an Activity while a manual editor is open.
  useLayoutEffect(() => {
    returnLink.current?.focus();
    returnLink.current = null;
  }, []);
  useLayoutEffect(() => {
    if (!focusAfter.current) return;
    const key = focusAfter.current;
    focusAfter.current = null;
    Array.from(root.current?.querySelectorAll<HTMLElement>('[data-guide-focus]') ?? [])
      .find(el => el.dataset.guideFocus === key)?.focus();
  }, [draft]);
  function change(next:GuideDraft, focus:string) {
    focusAfter.current = focus;
    onChange(next);
  }
  function back() {
    if (target && picking) {
      lastCandidate.current = target.id;
      change({...draft,target:undefined}, 'search');
    } else if (action && category) {
      change({category,more:draft.more}, action.label);
    } else if (category) {
      change({more:draft.more}, category);
    } else {
      change({}, 'More');
    }
  }
  function categoryButton(item: ChatCategory) {
    return <button type="button" key={item} data-guide-focus={item}
      onClick={()=>change({category:item,more:moreCategories.some(c=>c===item)}, 'heading')}>{item}</button>;
  }
  return <section ref={root} className={'chat-guide'+(picking ? target ? ' guide-resource-review' : ' guide-resource-search' : readyPrompt ? ' guide-resource-review' : '')} aria-label="Chat actions"
    onKeyDown={event=>{
      if (onSendPrepared && event.target instanceof HTMLElement && event.target.dataset.guideFocus === 'heading') {
        handleChatSubmitShortcut(event,onSendPrepared);
      }
      if (event.key === 'Escape' && (category || draft.more)) {event.preventDefault();back();}
    }}>
    {!category ? <>
      <div className="guide-pills">{primaryCategories.map(categoryButton)}
        <button type="button" data-guide-focus="More" aria-expanded={!!draft.more} aria-controls={groupId}
          onClick={()=>change({more:!draft.more},'More')}>More</button>
      </div>
      <div id={groupId} hidden={!draft.more}><div className="guide-pills">{moreCategories.map(categoryButton)}</div></div>
    </> : <>
      {picking ? <ChatResourcePicker key={action.area} area={action.area} label={action.label}
        question={action.kind === 'project' ? action.label : action.follow ? 'Which task do you want to follow?' : undefined}
        query={draft.query ?? ''} target={target} restoreId={lastCandidate.current} onBack={back}
        onQuery={query=>onChange({...draft,query})} onPick={target=>change({...draft,target},'heading')}/>
        : <div className="guide-heading">
          <strong tabIndex={-1} data-guide-focus="heading">{readyPrompt ? action.prompt : action ? action.label : category}</strong>
          <button type="button" onClick={back}>Back</button>
        </div>}
      {!action && <div className="guide-pills">
        {guideActions(category,admin).map(item=>item.kind==='link'
          ? <a key={item.label} href={item.href} onClick={event=>{returnLink.current=event.currentTarget;}}>{item.label}</a>
          : <button type="button" key={item.label} data-guide-focus={item.label}
              onClick={()=>{
                lastCandidate.current = undefined;
                change({...draft,action:item.label},item.kind==='prompt'?'heading':'search');
              }}>{item.label}</button>)}
      </div>}
      {action?.kind==='project' && target && <a href={'#/projects?project='+encodeURIComponent(target.id)}
        onClick={event=>{returnLink.current=event.currentTarget;}}>Open project settings</a>}
      {(target || action?.kind==='prompt') && <button type="button" onClick={()=>change({}, 'Tasks')}>Clear selection</button>}
    </>}
  </section>;
}
