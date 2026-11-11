// Colour theme preference for the console (docs/brand/brand-guidelines.md §12).
// "system" follows prefers-color-scheme through CSS; "light" and "dark" pin the
// theme with the data-theme attribute on <html>. The preference is per browser.

export type ThemePreference = 'system' | 'light' | 'dark';

export const THEME_STORAGE_KEY = 'weftTheme';
export const THEME_PREFERENCES: readonly ThemePreference[] = ['system', 'light', 'dark'];

// Browser chrome colours (meta theme-color) per resolved theme.
export const THEME_COLORS = {light: '#0f6e56', dark: '#141413'} as const;

export function isThemePreference(value: unknown): value is ThemePreference {
  return typeof value === 'string' && (THEME_PREFERENCES as readonly string[]).includes(value);
}

/**
 * Reads the stored preference. Unknown values mean "no explicit choice" and
 * resolve to "system". Storage can be unavailable (for example blocked by the
 * browser); that also resolves to "system" because no choice can have been saved.
 */
export function readThemePreference(storage: Pick<Storage, 'getItem'> | undefined): ThemePreference {
  if (!storage) return 'system';
  let value: string | null;
  try {
    value = storage.getItem(THEME_STORAGE_KEY);
  } catch {
    return 'system';
  }
  return isThemePreference(value) ? value : 'system';
}

export class ThemeStorageError extends Error {
  constructor(cause: unknown) {
    super('The theme preference could not be saved in this browser; it applies to this tab only.');
    this.name = 'ThemeStorageError';
    this.cause = cause;
  }
}

/** Persists the preference. "system" removes the key. Throws ThemeStorageError on failure. */
export function storeThemePreference(storage: Pick<Storage, 'setItem' | 'removeItem'> | undefined, preference: ThemePreference): void {
  if (!isThemePreference(preference)) throw new TypeError(`unknown theme preference: ${String(preference)}`);
  if (!storage) throw new ThemeStorageError(new Error('storage unavailable'));
  try {
    if (preference === 'system') storage.removeItem(THEME_STORAGE_KEY);
    else storage.setItem(THEME_STORAGE_KEY, preference);
  } catch (err) {
    throw new ThemeStorageError(err);
  }
}

export function resolveTheme(preference: ThemePreference, prefersDark: boolean): 'light' | 'dark' {
  if (preference === 'system') return prefersDark ? 'dark' : 'light';
  return preference;
}

export function systemPrefersDark(win: Pick<Window, 'matchMedia'> | undefined): boolean {
  return typeof win?.matchMedia === 'function' && win.matchMedia('(prefers-color-scheme: dark)').matches;
}

/** Applies the preference to the document root and the browser theme-color. */
export function applyTheme(preference: ThemePreference, doc: Document, prefersDark: boolean): void {
  const root = doc.documentElement;
  if (preference === 'system') delete root.dataset.theme;
  else root.dataset.theme = preference;
  const meta = doc.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (meta) meta.content = THEME_COLORS[resolveTheme(preference, prefersDark)];
}

function browserStorage(): Storage | undefined {
  try {
    return typeof localStorage === 'undefined' ? undefined : localStorage;
  } catch {
    return undefined;
  }
}

/** Applies the stored preference; call before the first render to avoid a theme flash. */
export function applyStoredTheme(): ThemePreference {
  const preference = readThemePreference(browserStorage());
  applyTheme(preference, document, systemPrefersDark(window));
  return preference;
}

export function saveTheme(preference: ThemePreference): void {
  applyTheme(preference, document, systemPrefersDark(window));
  storeThemePreference(browserStorage(), preference);
}
