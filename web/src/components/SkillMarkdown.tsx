import type {ReactNode} from 'react';

// A deliberately bounded preview: React text nodes keep HTML, images and unsupported syntax inert.
function inline(text:string):ReactNode[] {
  return text.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*|\*[^*\n]+\*|(?<!!)\[[^\]\n]+\]\([^\s)]+\))/g).map((part,index)=>{
    if(part.startsWith('`')&&part.endsWith('`')) return <code key={index}>{part.slice(1,-1)}</code>;
    if(part.startsWith('**')&&part.endsWith('**')) return <strong key={index}>{part.slice(2,-2)}</strong>;
    if(part.startsWith('*')&&part.endsWith('*')) return <em key={index}>{part.slice(1,-1)}</em>;
    const link=part.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
    if(link && /^https?:\/\//i.test(link[2])) return <a key={index} href={link[2]} target="_blank" rel="noreferrer noopener">{link[1]}</a>;
    return part;
  });
}
export function SkillMarkdown({body}:{body:string}) {
  const blocks:ReactNode[]=[];const lines=body.split(/\r?\n/);let index=0;
  while(index<lines.length) {
    const line=lines[index];const key=index;
    if(!line.trim()){index++;continue;}
    if(line.startsWith('```')) {
      const code:string[]=[];index++;
      while(index<lines.length&&!lines[index].startsWith('```')) code.push(lines[index++]);
      if(index<lines.length) index++;
      blocks.push(<pre key={key}><code>{code.join('\n')}</code></pre>);continue;
    }
    const heading=line.match(/^(#{1,6})\s+(.+)$/);
    if(heading){const Tag=`h${Math.min(heading[1].length+2,6)}` as 'h3'|'h4'|'h5'|'h6';blocks.push(<Tag key={key}>{inline(heading[2])}</Tag>);index++;continue;}
    if(/^\s*([-*]|\d+\.)\s+/.test(line)) {
      const ordered=/^\s*\d+\./.test(line);const pattern=ordered?/^\s*\d+\.\s+/:/^\s*[-*]\s+/;const items:ReactNode[]=[];
      while(index<lines.length&&pattern.test(lines[index])) {items.push(<li key={index}>{inline(lines[index].replace(pattern,''))}</li>);index++;}
      blocks.push(ordered?<ol key={key}>{items}</ol>:<ul key={key}>{items}</ul>);continue;
    }
    blocks.push(<p key={key}>{inline(line)}</p>);index++;
  }
  return <div className="skill-markdown" aria-label="Markdown preview">{blocks.length?blocks:<p className="muted">Nothing to preview yet.</p>}</div>;
}
