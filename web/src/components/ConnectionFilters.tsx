import { useRef } from 'react';
import { Btn } from './common';

export const emptyConnectionFilters = { search: '', status: '' };

export function ConnectionFilters({ value, onChange, placeholder, statusLabel, statuses }: {
  value: typeof emptyConnectionFilters;
  onChange: (value: typeof emptyConnectionFilters) => void;
  placeholder: string;
  statusLabel: string;
  statuses: Readonly<Record<string, { label: string }>>;
}) {
  const searchField = useRef<HTMLInputElement>(null);
  return <div className="connection-filter-fields">
    <label>Search connections
      <input ref={searchField} type="search" placeholder={placeholder} value={value.search}
        onChange={event => onChange({ ...value, search: event.target.value })} />
    </label>
    <label>{statusLabel}
      <select value={value.status} onChange={event => onChange({ ...value, status: event.target.value })}>
        <option value="">All statuses</option>
        {Object.entries(statuses).map(([status, { label }]) => <option key={status} value={status}>{label}</option>)}
      </select>
    </label>
    {(value.search || value.status) && <Btn tone="ghost" onClick={() => {
      onChange(emptyConnectionFilters);
      searchField.current?.focus();
    }}>Clear filters</Btn>}
  </div>;
}
