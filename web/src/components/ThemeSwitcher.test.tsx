import {cleanup, render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, expect, it, vi} from 'vitest';
import {ThemeSwitcher} from './ThemeSwitcher';
import {BrandMark} from './BrandMark';
import {THEME_STORAGE_KEY} from '../theme';

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.restoreAllMocks();
  delete document.documentElement.dataset.theme;
});

it('starts from the stored preference and switches theme', async () => {
  localStorage.setItem(THEME_STORAGE_KEY, 'dark');
  render(<ThemeSwitcher/>);
  const select = screen.getByRole('combobox', {name: 'Theme'}) as HTMLSelectElement;
  expect(select.value).toBe('dark');
  await userEvent.selectOptions(select, 'light');
  expect(document.documentElement.dataset.theme).toBe('light');
  expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
  await userEvent.selectOptions(select, 'system');
  expect(document.documentElement.dataset.theme).toBeUndefined();
  expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  expect(screen.queryByRole('status')).toBeNull();
});

it('applies the theme for the tab and says so when it cannot be saved', async () => {
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('quota', 'QuotaExceededError'); });
  render(<ThemeSwitcher/>);
  await userEvent.selectOptions(screen.getByRole('combobox', {name: 'Theme'}), 'dark');
  expect(document.documentElement.dataset.theme).toBe('dark');
  expect(screen.getByRole('status').textContent).toMatch(/could not be saved/);
  vi.restoreAllMocks();
  await userEvent.selectOptions(screen.getByRole('combobox', {name: 'Theme'}), 'light');
  expect(screen.queryByRole('status')).toBeNull();
});

it('renders brand marks with unique mask ids', () => {
  const {container} = render(<><BrandMark/><BrandMark size={16}/></>);
  const ids = [...container.querySelectorAll('mask')].map(m => m.id);
  expect(new Set(ids).size).toBe(4);
  for (const el of container.querySelectorAll('[mask]')) {
    const ref = el.getAttribute('mask')!.match(/^url\(#(.+)\)$/)![1];
    expect(ids).toContain(ref);
  }
  expect(container.querySelector('svg')!.getAttribute('aria-hidden')).toBe('true');
});
