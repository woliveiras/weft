import {useState} from 'react';
import {isThemePreference, readThemePreference, saveTheme, ThemeStorageError, type ThemePreference} from '../theme';

const LABELS: Record<ThemePreference, string> = {system: 'System theme', light: 'Light theme', dark: 'Dark theme'};

function initialPreference(): ThemePreference {
  try {
    return readThemePreference(localStorage);
  } catch {
    return 'system';
  }
}

export function ThemeSwitcher() {
  const [preference, setPreference] = useState<ThemePreference>(initialPreference);
  const [warning, setWarning] = useState<string>();

  const change = (value: string) => {
    if (!isThemePreference(value)) return;
    setPreference(value);
    try {
      saveTheme(value);
      setWarning(undefined);
    } catch (err) {
      if (!(err instanceof ThemeStorageError)) throw err;
      setWarning(err.message);
    }
  };

  return (
    <span className="theme-switcher">
      <select aria-label="Theme" value={preference} onChange={e => change(e.target.value)}
        aria-describedby={warning ? 'theme-warning' : undefined}>
        {(Object.keys(LABELS) as ThemePreference[]).map(p => <option key={p} value={p}>{LABELS[p]}</option>)}
      </select>
      {warning && <span id="theme-warning" role="status" className="small warn">{warning}</span>}
    </span>
  );
}
