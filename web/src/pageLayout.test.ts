/// <reference types="node" />
import {afterEach,expect,it} from 'vitest';
import {readFileSync} from 'node:fs';
const baseStyles=readFileSync('src/styles.css','utf8');
const designStyles=readFileSync('src/design-system.css','utf8');

let sheet:HTMLStyleElement|undefined;
afterEach(()=>{sheet?.remove();document.body.replaceChildren();});

it('gives every main destination the available width, including legacy constrained classes',()=>{
 sheet=document.createElement('style');sheet.textContent=baseStyles+'\n'+designStyles;document.head.append(sheet);
 const main=document.createElement('main');document.body.append(main);
 for(const className of ['page activity-page','page','page approvals-page','page installation-page projects-page','page workflows-page','page installation-page connections-page','page installation-page models-page','page skills-page','page administration-page']){
  const page=document.createElement('section');page.className=className;main.append(page);
  const style=getComputedStyle(page);
  expect(style.width,className).toBe('100%');
  expect(style.maxWidth,className).toBe('none');
  expect(parseFloat(style.marginInline),className).toBe(0);
 }
});

it('keeps nested content and fill workspaces outside the destination-width rule',()=>{
 sheet=document.createElement('style');sheet.textContent=baseStyles+'\n'+designStyles;document.head.append(sheet);
 const main=document.createElement('main');document.body.append(main);
 const workspace=document.createElement('section');workspace.className='page fill';main.append(workspace);
 const content=document.createElement('section');content.className='page';workspace.append(content);
 expect(getComputedStyle(workspace).display).toBe('flex');
 expect(getComputedStyle(workspace).width).not.toBe('100%');
 expect(getComputedStyle(content).width).not.toBe('100%');
});
