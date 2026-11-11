import {afterEach, describe, expect, it} from 'vitest';
import {
  applyTheme, isThemePreference, readThemePreference, resolveTheme, storeThemePreference, systemPrefersDark,
  THEME_COLORS, THEME_STORAGE_KEY, ThemeStorageError,
} from './theme';

function memoryStorage(initial: Record<string, string> = {}) {
  const data = new Map(Object.entries(initial));
  return {
    data,
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => void data.set(k, v),
    removeItem: (k: string) => void data.delete(k),
  };
}
const failing = {
  getItem: () => { throw new DOMException('denied', 'SecurityError'); },
  setItem: () => { throw new DOMException('quota', 'QuotaExceededError'); },
  removeItem: () => { throw new DOMException('denied', 'SecurityError'); },
};

afterEach(() => {
  delete document.documentElement.dataset.theme;
  document.head.innerHTML = '';
});

describe('isThemePreference', () => {
  it('accepts only known preferences', () => {
    expect(['system', 'light', 'dark'].every(isThemePreference)).toBe(true);
    expect(isThemePreference('Dark')).toBe(false);
    expect(isThemePreference('')).toBe(false);
    expect(isThemePreference(null)).toBe(false);
    expect(isThemePreference(1)).toBe(false);
  });
});

describe('readThemePreference', () => {
  it('returns the stored preference', () => {
    expect(readThemePreference(memoryStorage({[THEME_STORAGE_KEY]: 'dark'}))).toBe('dark');
    expect(readThemePreference(memoryStorage({[THEME_STORAGE_KEY]: 'light'}))).toBe('light');
  });
  it('resolves missing, unknown or unreadable values to system', () => {
    expect(readThemePreference(memoryStorage())).toBe('system');
    expect(readThemePreference(memoryStorage({[THEME_STORAGE_KEY]: 'sepia'}))).toBe('system');
    expect(readThemePreference(undefined)).toBe('system');
    expect(readThemePreference(failing)).toBe('system');
  });
});

describe('storeThemePreference', () => {
  it('stores explicit choices and removes the key for system', () => {
    const s = memoryStorage();
    storeThemePreference(s, 'dark');
    expect(s.data.get(THEME_STORAGE_KEY)).toBe('dark');
    storeThemePreference(s, 'system');
    expect(s.data.has(THEME_STORAGE_KEY)).toBe(false);
  });
  it('reports storage failures with the cause', () => {
    let caught: unknown;
    try { storeThemePreference(failing, 'light'); } catch (err) { caught = err; }
    expect(caught).toBeInstanceOf(ThemeStorageError);
    expect((caught as ThemeStorageError).cause).toBeInstanceOf(DOMException);
    expect(() => storeThemePreference(undefined, 'dark')).toThrow(ThemeStorageError);
  });
  it('rejects unknown preferences without writing', () => {
    const s = memoryStorage();
    expect(() => storeThemePreference(s, 'sepia' as never)).toThrow(TypeError);
    expect(s.data.size).toBe(0);
  });
});

describe('resolveTheme and systemPrefersDark', () => {
  it('follows the system only for the system preference', () => {
    expect(resolveTheme('system', true)).toBe('dark');
    expect(resolveTheme('system', false)).toBe('light');
    expect(resolveTheme('light', true)).toBe('light');
    expect(resolveTheme('dark', false)).toBe('dark');
  });
  it('treats a missing matchMedia as light', () => {
    expect(systemPrefersDark(undefined)).toBe(false);
    expect(systemPrefersDark({} as Window)).toBe(false);
    expect(systemPrefersDark({matchMedia: () => ({matches: true})} as unknown as Window)).toBe(true);
  });
});

describe('applyTheme', () => {
  it('sets and clears data-theme and updates theme-color', () => {
    document.head.innerHTML = '<meta name="theme-color" content="#5b55d9">';
    const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')!;
    applyTheme('dark', document, false);
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(meta.content).toBe(THEME_COLORS.dark);
    applyTheme('system', document, false);
    expect(document.documentElement.dataset.theme).toBeUndefined();
    expect(meta.content).toBe(THEME_COLORS.light);
    applyTheme('system', document, true);
    expect(meta.content).toBe(THEME_COLORS.dark);
  });
  it('works without a theme-color meta element', () => {
    applyTheme('light', document, true);
    expect(document.documentElement.dataset.theme).toBe('light');
  });
});
