// A stand-in for EventSource used when VITE_MOCK=1. It stays tiny and has no
// static imports so it costs nothing in the production bundle; the fake
// server (loaded lazily) dispatches events on a shared window-level bus.

export const MOCK_BUS_KEY = '__weftMockBus';

export function mockBus(): EventTarget {
  const w = window as unknown as Record<string, EventTarget | undefined>;
  if (!w[MOCK_BUS_KEY]) w[MOCK_BUS_KEY] = new EventTarget();
  return w[MOCK_BUS_KEY]!;
}

export class MockEventSource {
  onopen: ((ev: Event) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  private listeners = new Map<string, Set<EventListener>>();
  private relay = (e: Event) => {
    const { type, data, seq } = (e as CustomEvent<{ type: string; data: string; seq: number }>).detail;
    const me = new MessageEvent(type, { data, lastEventId: String(seq) });
    this.listeners.get(type)?.forEach((fn) => fn(me));
  };

  constructor(public url: string) {
    mockBus().addEventListener('sse', this.relay);
    void import('./server').then((m) => m.startSimulation());
    setTimeout(() => this.onopen?.(new Event('open')), 50);
  }

  addEventListener(type: string, fn: EventListener) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(fn);
  }

  close() {
    mockBus().removeEventListener('sse', this.relay);
  }
}
