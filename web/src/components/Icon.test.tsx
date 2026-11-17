import {cleanup, render, screen} from '@testing-library/react';
import {afterEach, expect, it} from 'vitest';
import {Icon, type IconName} from './common';
import {PrimaryNavigation} from './PrimaryNavigation';

afterEach(cleanup);
const names: IconName[] = ['play', 'stop', 'pause', 'bolt', 'fit', 'lock', 'queue', 'topic', 'fanout', 'broadcast', 'check', 'x', 'refresh', 'external',
  'tasks', 'projects', 'approvals', 'workflows', 'connections', 'models', 'skills', 'administration', 'chevron-down', 'chevron-right'];

it('renders every icon as a decorative line glyph of the requested size', () => {
  for (const name of names) {
    const {container, unmount} = render(<Icon name={name} size={20}/>);
    const svg = container.querySelector('svg')!;
    expect(svg, name).not.toBeNull();
    expect(svg.getAttribute('aria-hidden')).toBe('true');
    expect(svg.getAttribute('width')).toBe('20');
    expect(svg.getAttribute('stroke-width')).toBe('1.75');
    expect(svg.getAttribute('fill')).toBe(['play', 'stop', 'bolt'].includes(name) ? 'currentColor' : 'none');
    unmount();
  }
});

it('gives each navigation destination an icon without changing accessible names', () => {
  render(<PrimaryNavigation active="models" pending={3} onNavigate={() => {}}/>);
  for (const name of ['Tasks', 'Projects', 'Approvals 3', 'Workflows', 'Connections', 'Models', 'Skills & instructions', 'Administration']) {
    const button = screen.getByRole('button', {name});
    expect(button.querySelector('svg.icon[aria-hidden="true"]'), name).not.toBeNull();
    expect(button.classList.contains('nav-item')).toBe(true);
  }
  expect(screen.getByRole('button', {name: 'Models'}).getAttribute('aria-current')).toBe('page');
  expect(screen.getByRole('button', {name: 'Configuration'}).querySelector('svg')).not.toBeNull();
});
