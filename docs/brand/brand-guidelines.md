# Weft brand guidelines

Reference for the console redesign. It extends [the brand README](README.md)
(mark construction and asset files) with voice, colour system, typography,
layout and component rules. When this document and the current
`web/src/styles.css` disagree, this document is the target state.

> Status: draft for the redesign. Values marked *proposed* are new and should be
> validated in the first redesigned screens before being treated as final.

---

## 1. Brand idea

**Weft is the thread that crosses the warp and turns loose threads into cloth.**

The warp is structure: agents, graphs, channels, permissions. The weft is the
work moving through that structure: messages, tasks, results. The product takes
separate autonomous agents and weaves them into one dependable piece of work,
under rules the agents cannot bypass.

Every design decision should support three ideas:

| Principle | Meaning | In the interface |
|---|---|---|
| **Woven, not tangled** | Many moving parts, one coherent result. | Clear hierarchy, aligned grids, consistent spacing. Complexity is organised, never hidden. |
| **Under control** | Default-deny, approvals, observability. Operators stay in charge. | State is always explicit: status, permissions and pending approvals are visible and labelled, not implied by colour alone. |
| **Open and grounded** | Vendor-neutral, local-first, runs on a laptop. | Calm, utilitarian look. No hype, no gradients-for-effect, no vendor imagery. |

### Personality

- **Calm** rather than flashy. A tool you keep open all day.
- **Precise** rather than clever. Words and numbers mean exactly what they say.
- **Crafted** rather than corporate. Warm neutrals and the thread accent give it
  a hand-made feel without becoming playful.

## 2. Name and wordmark

- The product is **Weft**. Capitalise it in prose ("Weft runs agents…").
- The wordmark is always lowercase: **weft**. Never set the wordmark in caps
  or title case.
- The server binary and CLI keep their technical names (`weftd`, `weft`) in
  `code` formatting.
- Do not use the former name (LeForge) in new UI, docs or assets.
- Do not add taglines inside the lockup. When a descriptor is needed, use
  "Agent Factory" set separately in the secondary text style.

## 3. Logo

Assets live in `docs/brand/` (see the [README](README.md) for construction).

| Asset | Use in the console |
|---|---|
| `weft-lockup.svg` | Sidebar/header at full width, login, empty-state hero, README |
| `weft-mark.svg` | Collapsed sidebar, loading indicator, small headers |
| `weft-mark-mono.svg` | Inline icon on coloured backgrounds, print, single-colour contexts |
| `weft-app-icon.svg` | Favicon, PWA icon, OS/app tiles |

Rules:

- **Clear space**: at least the height of the mark's horizontal thread gap
  (≈ ¼ of the mark height) on every side.
- **Minimum size**: mark 16 px; lockup 80 px wide.
- Use only the supplied colour pairs (light / dark / mono / app tile).
- Do not recolour the W and thread independently, rotate, outline, add shadows
  or place the colour mark on busy imagery or on mid-tone teal/amber.
- On teal or amber surfaces use the mono mark in white or `neutral-950`.

> **Redesign action:** `web/public/favicon.svg` still shows the old "L" mark in
> the old indigo. Replace it with a 32 px export of `weft-app-icon.svg`.

## 4. Colour

### 4.1 Brand colours (fixed)

These come from the logo and are not negotiable.

| Name | Light theme | Dark theme | Role |
|---|---|---|---|
| **Warp teal** | `#0F6E56` | `#5DCAA5` | Primary brand colour. Structure, primary actions, links, focus, selection. |
| **Weft amber** | `#BA7517` | `#EF9F27` | Accent. The thread: work in motion, highlights, the one thing to look at. |
| **Tile amber** | `#FAC775` | — | Light amber on teal (app icon, badges on teal). |
| **Ink** | `#1F1E1D` | `#F1EFE8` | Wordmark and primary text. |

**Usage ratio**: roughly 80 % neutrals, 15 % teal, 5 % amber. Amber is an accent;
if a screen has more amber than teal, it is wrong.

### 4.2 Tonal scales (*proposed*)

Generated around the fixed brand values (marked ●). Use tokens, never raw hex,
in components.

**Teal**

| 50 | 100 | 200 | 300 ● | 400 | 500 | 600 ● | 700 | 800 | 900 |
|---|---|---|---|---|---|---|---|---|---|
| `#E8F5F0` | `#CDEBE0` | `#9FD8C3` | `#5DCAA5` | `#2FA883` | `#168A6A` | `#0F6E56` | `#0B5948` | `#08463A` | `#06342B` |

**Amber**

| 50 | 100 | 200 ● | 300 | 400 ● | 500 | 600 ● | 700 | 800 | 900 |
|---|---|---|---|---|---|---|---|---|---|
| `#FDF5E6` | `#FAE6C2` | `#FAC775` | `#F5B54E` | `#EF9F27` | `#D88A1C` | `#BA7517` | `#965E12` | `#73480E` | `#52330A` |

**Neutral (warm)** — slightly warm greys, continuing the current console's
paper feel but aligned to the Ink colours.

| 0 | 25 | 50 | 100 ● | 200 | 300 | 400 | 500 | 600 | 700 | 800 | 900 ● | 950 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `#FFFFFF` | `#FCFBF8` | `#F7F6F2` | `#F1EFE8` | `#E4E1D8` | `#CFCBC0` | `#A9A59A` | `#7D7A71` | `#67645C` | `#403E39` | `#2B2A27` | `#1F1E1D` | `#141413` |

### 4.3 Semantic tokens (*proposed*)

Components consume semantic tokens only. Theme switching swaps the token values.

| Token | Light | Dark | Use |
|---|---|---|---|
| `--surface-app` | neutral-50 `#F7F6F2` | `#141413` | Page background |
| `--surface-raised` | neutral-0 `#FFFFFF` | `#1C1B19` | Cards, panels, inputs |
| `--surface-sunken` | neutral-100 `#F1EFE8` | `#262522` | Sidebar, table headers, code blocks |
| `--surface-canvas` | neutral-25 `#FCFBF8` | `#171715` | Graph editor canvas |
| `--canvas-grid` | neutral-200 `#E4E1D8` | `#2B2A27` | Canvas dots/grid |
| `--border` | neutral-200 `#E4E1D8` | `#333230` | Default borders, dividers |
| `--border-strong` | neutral-300 `#CFCBC0` | `#4A4843` | Input borders, hover borders |
| `--text` | neutral-900 `#1F1E1D` | neutral-100 `#F1EFE8` | Primary text |
| `--text-muted` | neutral-600 `#67645C` | neutral-400 `#A9A59A` | Secondary text, labels |
| `--text-subtle` | neutral-500 `#7D7A71` | `#8C887E` | Placeholders, disabled (non-essential text only) |
| `--primary` | teal-600 `#0F6E56` | teal-300 `#5DCAA5` | Primary buttons, links, active nav |
| `--primary-hover` | teal-700 `#0B5948` | teal-200 `#9FD8C3` | Hover/pressed |
| `--on-primary` | `#FFFFFF` | neutral-950 `#141413` | Text/icons on `--primary` |
| `--primary-soft` | teal-50 `#E8F5F0` | `#0F2E26` | Selected rows, active nav background |
| `--accent` | amber-600 `#BA7517` | amber-400 `#EF9F27` | Thread highlights (non-text or large text) |
| `--accent-text` | amber-700 `#965E12` | amber-300 `#F5B54E` | Amber text on normal backgrounds |
| `--accent-soft` | amber-50 `#FDF5E6` | `#33240C` | Highlighted backgrounds |
| `--focus-ring` | teal-600 | teal-300 | 2 px outline, 2 px offset |

### 4.4 Status colours (*proposed*)

Status must be distinguishable from brand colours and must **always** be paired
with an icon or label (never colour alone).

| Status | Light (text / fill) | Dark | Mapped states |
|---|---|---|---|
| Success | `#2E7D32` / `#EAF4EA` | `#6FCF7A` | completed, healthy, approved |
| Info / running | `#2F6DB5` / `#EAF1FA` | `#7FB2F0` | running, connecting, in progress |
| Warning | `#B25E09` / `#FCF0E3` | `#F5B54E` | waiting approval, degraded, near quota |
| Danger | `#B3261E` / `#FBECEA` | `#F2867E` | failed, error, denied, destructive actions |
| Neutral | neutral-600 / neutral-100 | neutral-400 | idle, stopped, draft, interrupted |

Brand teal is **not** the success colour, and brand amber is **not** the warning
colour. Teal means "Weft / act here"; amber means "the work thread". Keeping them
out of status avoids "is this selected or is this OK?" ambiguity.

### 4.5 Data visualisation

For charts (costs, metrics, queue depth) use this categorical order, which
starts from the brand and stays distinct for common colour-vision deficiencies:

1. teal-600 `#0F6E56` 2. amber-500 `#D88A1C` 3. `#2F6DB5` 4. `#8E5BB5`
5. `#C2185B` 6. neutral-500 `#7D7A71`

Single-series charts use teal; highlight one value with amber.

### 4.6 Contrast (checked, WCAG 2.2)

| Pair | Ratio | Result |
|---|---|---|
| Ink `#1F1E1D` on `#F7F6F2` | 15.4 : 1 | AAA |
| Muted `#67645C` on `#FFFFFF` | ≈ 5.9 : 1 | AA |
| Teal `#0F6E56` on white (and white on teal) | 6.2 : 1 | AA |
| Amber `#BA7517` on white | 3.7 : 1 | Large text / non-text only |
| Amber-700 `#965E12` on white | 5.4 : 1 | AA — use for amber text |
| Dark teal `#5DCAA5` on `#1C1B19` | 8.6 : 1 | AAA |
| Dark amber `#EF9F27` on `#1C1B19` | 7.9 : 1 | AAA |
| Danger `#B3261E` on white | 6.5 : 1 | AA |

Rule: body text ≥ 4.5 : 1, large text and UI boundaries ≥ 3 : 1.
`--accent` (amber-600) is never used for small text in light mode.

## 5. Typography

| Role | Family | Notes |
|---|---|---|
| UI and headings | **Inter** (variable), fallback `ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif` | Self-host the font files (local-first, no CDN). Enable `font-feature-settings: "cv11", "ss01"` and `tabular-nums` in tables and metrics. |
| Code, IDs, YAML, logs | **JetBrains Mono**, fallback `ui-monospace, SFMono-Regular, Menlo, monospace` | Already used; keep. Self-host. |
| Wordmark | Lockup asset only | Never re-type "weft" in a font to imitate the logo. |

Type scale (*proposed*, 14 px base, 1.25 ratio for headings):

| Token | Size / line-height | Weight | Use |
|---|---|---|---|
| `display` | 28 / 36 | 600 | Empty states, onboarding hero |
| `h1` | 24 / 32 | 600 | Page title |
| `h2` | 18 / 26 | 600 | Section title |
| `h3` | 15 / 22 | 600 | Card / panel title |
| `body` | 14 / 20 | 400 | Default |
| `body-strong` | 14 / 20 | 500 | Labels, table emphasis |
| `small` | 12 / 16 | 400 | Meta, helper text, timestamps |
| `overline` | 11 / 16 | 600, uppercase, +0.06 em | Group labels (e.g. "Work", "Configuration") |
| `code` | 12.5 / 18 | 400 | Mono |

Letter-spacing: −0.01 em for `h1`/`display`; 0 otherwise. Use sentence case for
all headings, buttons and menu items ("New task", not "New Task").

## 6. Iconography

- Line icons, **1.75 px stroke at 20 px**, round caps and joins — matching the
  mark's round geometry. Recommended set: Lucide (MIT, tree-shakeable).
- Sizes: 16 px (inline, dense tables), 20 px (buttons, nav), 24 px (empty states).
- Icons inherit `currentColor`. Never colour an icon amber unless it marks the
  active thread (see §8).
- Domain icons should be consistent everywhere: agent, graph/workflow, channel,
  queue, approval, tool, connection, model, skill.

## 7. Layout, shape and depth

| Token | Value | Notes |
|---|---|---|
| Spacing scale | 4, 8, 12, 16, 20, 24, 32, 40, 48, 64 px | 4 px base grid |
| `--radius-sm` | 6 px | Inputs, chips, small buttons |
| `--radius-md` | 10 px | Buttons, cards, panels |
| `--radius-lg` | 16 px | Dialogs, app icon proportion (16 / 72) |
| `--radius-full` | 999 px | Pills, avatars, status dots |
| Border | 1 px `--border` | Prefer borders over shadows |
| `--shadow-1` | `0 1px 2px rgb(20 20 19 / 0.06)` | Raised cards on hover |
| `--shadow-2` | `0 8px 24px rgb(20 20 19 / 0.10)` | Popovers, menus, dialogs |

Layout:

- App shell: left sidebar (240 px, collapsible to 64 px showing the mark),
  top bar with page title and primary action, content max-width 1280 px for
  forms and lists; graph editor and logs use full width.
- Density: default row height 40 px, compact 32 px for tables, logs and
  message history.
- Breakpoints: 640 / 960 / 1280 / 1600 px.

## 8. Signature motif: the thread

The one distinctive element of the system is the **amber thread** crossing a
**teal structure**, taken directly from the mark. Use it deliberately and rarely:

- **Graph editor**: nodes are teal-outlined structure; an edge that is currently
  carrying messages (or is selected) is drawn amber. Idle edges are neutral-400.
- **Navigation**: the active item gets a 2 px amber bar on the leading edge and
  a `--primary-soft` background.
- **Progress**: progress bars and run timelines use a teal track with an amber
  "thread" marker for the current position.
- **Empty states / onboarding**: a single horizontal amber line passing behind
  a teal illustration.

Do not use the thread as decoration on every card. One thread per view.

## 9. Components (direction)

| Component | Direction |
|---|---|
| Primary button | `--primary` fill, `--on-primary` text, `--radius-md`, 36 px height. One per view region. |
| Secondary button | `--surface-raised`, `--border-strong`, `--text`. |
| Ghost button | No border; `--primary` text; for toolbars and tertiary actions. |
| Destructive button | Danger fill, only in confirmation dialogs; elsewhere danger-text ghost. |
| Inputs | `--surface-raised`, `--border-strong`, `--radius-sm`, focus ring teal. Error: danger border + message below, `aria-invalid`. |
| Status badge | Pill, soft status background + status text + icon or dot + label. |
| Cards / panels | `--surface-raised`, 1 px border, `--radius-md`, 16–20 px padding, no shadow at rest. |
| Tables | `--surface-sunken` header, `overline` labels, tabular numbers, row hover `--surface-sunken`, selected row `--primary-soft`. |
| Code / YAML | `--surface-sunken`, mono, `--radius-sm`; syntax colours from teal/amber/info/danger only. |
| Graph nodes | `--surface-raised`, 1.5 px border coloured by status, title + runtime + replica count; selected node gets teal ring. |
| Approvals | Warning-soft banner with count in the top bar; approval cards use warning accents, actions are primary (approve) and secondary (reject). |

## 10. Motion

- Durations: 120 ms (hover, press), 200 ms (panels, menus), 320 ms (page/drawer).
- Easing: `cubic-bezier(0.2, 0, 0, 1)` for enter, `cubic-bezier(0.4, 0, 1, 1)` for exit.
- The only ambient animation is the active-edge flow in the graph (a slow
  dash offset along amber edges). Everything else animates only in response to
  user action.
- Respect `prefers-reduced-motion`: disable edge flow and reduce transitions to
  opacity.

## 11. Voice and tone

Weft speaks like a careful senior operator: direct, specific, never alarmist.

| Do | Don't |
|---|---|
| "Agent `reviewer` is waiting for approval to run `git push`." | "Oops! Something needs your attention 🙈" |
| "Model test failed: connection refused at `localhost:11434`." | "Something went wrong." |
| "Delete workflow *Software Factory*? Runs and messages are kept." | "Are you sure?" |
| Name the actual object, action and consequence. | Use marketing words: "magic", "supercharge", "AI-powered". |

- Sentence case everywhere; no exclamation marks in system messages.
- Use the domain vocabulary consistently: *workflow* (UI) / *graph* (API),
  *agent*, *channel*, *task*, *run*, *approval*, *connection*, *model*.
- Errors state what happened, why if known, and the next step.
- Never expose secrets, tokens or credentials in messages or screenshots.

## 12. Accessibility

- WCAG 2.2 AA is the minimum for all redesigned screens.
- Colour is never the only carrier of meaning (status, diff, validation).
- Visible focus on every interactive element (teal 2 px ring, 2 px offset).
- Hit targets ≥ 24 × 24 px (≥ 32 px in primary nav and toolbars).
- Light and dark themes both supported; default follows
  `prefers-color-scheme`, with a manual override stored per browser.

## 13. Redesign checklist

Gaps between the console and this guide. Tokens live at the top of
`web/src/styles.css`; the theme preference is handled by `web/src/theme.ts`
(stored per browser as `weftTheme`).

- [x] Replace the old indigo accent (`--accent: #5b55d9`) with teal tokens.
- [x] Replace `web/public/favicon.svg` with the Weft app icon.
- [x] Introduce semantic tokens (§4.3) and remove raw hex values from
      component styles.
- [x] Add dark theme: follows `prefers-color-scheme`, with a System / Light /
      Dark selector in the top bar.
- [x] Separate status colours from brand colours (§4.4).
- [x] Add the mark and wordmark to the app shell.
- [x] Thread motif on graph edges: flowing and selected channels are amber (§8).
- [x] Thread motif on active navigation (§8).
- [x] Self-host Inter and JetBrains Mono (`@fontsource` packages, bundled by Vite).
- [x] Apply the type scale (§5), spacing and radius tokens (§7): `web/src/design-system.css`.
- [x] Adopt one icon set (§6): Lucide, through the `Icon` component in `web/src/components/common.tsx`.
- [x] Redesign page layouts (app shell, lists, forms, graph editor) on top of the tokens.
- [x] Add visual regression screenshots (Playwright) for light and dark themes
      on the main destinations: `pnpm run test:e2e:visual`; regenerate baselines
      with `--update-snapshots` in the Linux Playwright container and review them.
