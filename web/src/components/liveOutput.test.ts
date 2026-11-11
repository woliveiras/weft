import { describe, expect, it } from 'vitest';
import type { WeftEvent } from '../api/types';
import { describeEvent, foldLiveOutput } from './describe';

let seq = 0;
function out(data: Record<string, unknown>): WeftEvent {
  seq += 1;
  return { seq, id: `evt_${seq}`, type: 'agent.output', time: '2026-11-11T10:00:00Z', executionId: 'exe_1', data };
}
const tool: WeftEvent = { seq: 99, id: 'evt_tool', type: 'tool.invoked', time: '2026-11-11T10:00:01Z', data: { tool: 'filesystem.read' } };

describe('live output of streamed answers', () => {
  it('folds the live pieces of one answer into a single line while it streams', () => {
    const note = out({ text: 'note' });
    const a = out({ text: 'Hel', stream: 'stm_1', partial: true });
    const b = out({ text: 'lo, wor', stream: 'stm_1', partial: true });
    const folded = foldLiveOutput([note, a, tool, b]);
    expect(folded.map((e) => e.id)).toEqual([note.id, a.id, 'evt_tool']);
    expect(describeEvent(folded[1])).toBe('Hello, wor…');
  });

  it('replaces the live line with the final output of the same stream', () => {
    const events = [
      out({ text: 'Hel', stream: 'stm_2', partial: true }),
      out({ text: 'lo', stream: 'stm_2', partial: true }),
      out({ text: 'Hello, world!', stream: 'stm_2' }),
      out({ text: 'Next', stream: 'stm_3', partial: true }),
    ];
    const folded = foldLiveOutput(events);
    expect(folded.map(describeEvent)).toEqual(['Hello, world!', 'Next…']);
  });

  it('keeps the shown text of a stream that failed and leaves other events alone', () => {
    const plain = out({ text: 'whole answer' });
    const failed = out({ text: 'cut', stream: 'stm_4', partial: true });
    const folded = foldLiveOutput([plain, failed, tool]);
    expect(folded).toHaveLength(3);
    expect(folded[0]).toBe(plain);
    expect(describeEvent(folded[1])).toBe('cut…');
    expect(folded[2]).toBe(tool);
  });

  it('describes a single live piece as unfinished', () => {
    expect(describeEvent(out({ text: 'Thinking about it', stream: 's', partial: true }))).toBe('Thinking about it…');
    expect(describeEvent(out({ text: 'Done.' }))).toBe('Done.');
  });
});
