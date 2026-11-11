# Weft brand

Weft is the thread that crosses the warp, over and under, and turns loose
threads into cloth. The mark is a W (the agents and their structure) with
one thread passing over its outer legs and under its middle V: separate
agents woven into one piece of work under the same rules.

| File | Use |
|---|---|
| `weft-mark.svg` | Default mark; teal W, amber thread; switches to the light palette in dark mode |
| `weft-mark-mono.svg` | One colour (`currentColor`), for inline use, favicons on any background and print |
| `weft-app-icon.svg` | Rounded-square app icon (teal tile, white W, light amber thread) |
| `weft-lockup.svg` | Mark and lowercase name, for headers and the README |

## Construction

- 64 × 64 grid, 6.5-unit strokes, round caps and joins.
- W points: (10,14) (21,48) (32,24) (43,48) (54,14). Thread: y = 30 from x = 4 to 60.
- The over/under crossings are gaps cut with masks, not overlaps, so the mark
  works on any background and in one colour.
- Legible down to 16 px.

## Colours

| | Light | Dark |
|---|---|---|
| W (warp) | `#0F6E56` | `#5DCAA5` |
| Thread (weft) | `#BA7517` | `#EF9F27` |
| App tile | `#0F6E56` with `#FFFFFF` and `#FAC775` | |

The lockup sets the name in the system sans-serif font. Convert it to
outlines in a design tool before using it where fonts may differ.
