# Veil — Design Thinking

## Core Philosophy

Veil is an **editorial inbox** — email reimagined as a reading experience, not a task list. The UI should disappear. Content breathes. Every interaction feels intentional, never decorative.

---

## What We Reject

### The "AI Product" Trap
- No sparkle icons on everything
- No colored priority dots scattered across the UI (red/amber/green circles are visual noise)
- No "AI Summary" badges with borders and backgrounds competing for attention
- AI features should be **ambient** — a quiet italic line, not a branded card
- Suggested replies are plain pills, not gradient buttons

### The "Dashboard" Trap
- No oversized avatars with heavy overlapping stacks
- No pill-shaped badges with background colors for unread counts (just plain numbers)
- No chunky rounded-full buttons that look like they belong in a mobile app
- No border-left accent gimmicks on active states — just a subtle background shift

### The "Component Library Demo" Trap
- Don't use shadcn components at full default size — they're designed for forms, not dense productivity UIs
- Default Button, Input, Avatar sizes are too large for a mail client sidebar
- Always reduce: `h-8` → `h-7`, `text-sm` → `text-[13px]`, `py-2` → `py-1.5`

---

## What We Embrace

### Typography as the Hero
- **Instrument Serif** for the reading pane — makes email bodies feel like articles
- **DM Sans** at 13px for all UI chrome — compact, warm, geometric
- Subject lines in the detail header use serif at `text-xl` — the one moment of display type
- Everything else is deliberately small and quiet

### Density
- Sidebar nav items: `py-1.5`, `text-[13px]`, `space-y-px`
- Mail list items: `py-3`, three lines (sender, subject, snippet)
- The sidebar is `w-52` not `w-64` — every pixel matters
- The mail list is `w-80` — enough to read subjects, not more

### Warm Darkness
- The taupe dark theme (oklch hue ~43) creates warmth unlike cold blue-black
- Borders are `oklch(1 0 0 / 10%)` — white at 10% opacity, not gray
- Muted text is `oklch(0.714)` — readable but recessive
- Reading text (`--veil-ink`) is `oklch(0.92 0.01 67)` — warm white, not pure white

### Hierarchy Through Restraint
- Only **one** visual indicator per state:
  - Unread = `border-l-2 border-primary` + bold sender name (not bold + dot + background + badge)
  - Active nav = `bg-accent` background only (not background + border + icon color change)
  - Starred = small amber star icon (not star + highlight + row tint)
- The detail header participants are plain text, not an avatar stack (avatars are for the message bubbles)

### Motion That Serves
- Sidebar collapse: CSS `transition-[width]` — not framer-motion (no layout thrash)
- Mail list items: brief `opacity + y` stagger on folder change
- Thread messages: sequential fade-in with spring
- Command palette: native Dialog, not a custom animated overlay
- **Never animate what doesn't need it** — the sidebar nav items don't need staggered entry on every render

---

## Component Size Reference

| Element | Height | Font | Padding |
|---------|--------|------|---------|
| Sidebar nav item | auto (~32px) | 13px | px-2 py-1.5 |
| Sidebar compose btn | h-8 | 13px medium | centered |
| Sidebar account | auto (~40px) | 13px / 11px | px-2 py-1.5 |
| Sidebar label item | auto (~28px) | 13px | px-2 py-1 |
| Mail list item | auto (~72px) | 13-14px | px-4 py-3 |
| Detail action btn | h-7 | 12px | px-2 |
| Detail icon btn | h-7 w-7 | — | centered |

---

## Color Usage

- **Primary (indigo)**: Compose button, unread border, active icon — used sparingly
- **Foreground**: Active nav text, sender names, subjects
- **Muted-foreground**: Everything else — timestamps, snippets, inactive nav, badges
- **Muted-foreground/70 or /60**: Labels section header, collapsed toggle, metadata
- **Amber-400/80**: Starred indicator only
- **Destructive**: Trash hover state only
- No category colors in the mail list — only in the split tab underlines

---

## Layout Architecture

```
┌─────────┬──────────────┬─────────────────────────────┐
│ Sidebar │  Mail List   │      Detail Pane             │
│  w-52   │    w-80      │       flex-1                 │
│ shrink-0│  shrink-0    │      min-w-0                 │
│         │  border-r    │                              │
│         │              │  ┌─────────────────────┐     │
│         │              │  │ Header (shrink-0)   │     │
│         │              │  ├─────────────────────┤     │
│         │              │  │ Content (overflow-y) │    │
│         │              │  │  max-w-3xl centered  │    │
│         │              │  └─────────────────────┘     │
└─────────┴──────────────┴─────────────────────────────┘
```

- No ResizablePanelGroup — it fights with flex layout after hydration
- Sidebar width is CSS-transitioned, not framer-motion animated
- Detail content scrolls natively (`overflow-y-auto`), not via ScrollArea
- Three fixed columns via flexbox: sidebar (shrink-0) + list (shrink-0) + detail (flex-1)

---

## Decisions Log

1. **Dropped ResizablePanelGroup** — caused layout collapse after hydration. Plain flex is reliable.
2. **Dropped cmdk CommandDialog** — store context lost through portal. Built custom palette with native Dialog + manual keyboard nav.
3. **Dropped AvatarStack from detail header** — overlapping circles for 2 people looks bad. Plain text names are clearer.
4. **Dropped priority dots from mail list** — colored circles on every row is visual noise. Priority info lives in the detail view AI summary.
5. **Dropped AI summary from mail list items** — too much text per row. Summary shows in the reading pane where you actually read.
6. **Dropped framer-motion from sidebar** — CSS transitions are simpler and don't cause layout recalculation issues.
7. **Always show action labels** — Reply/Reply All/Forward show both icon + text. Icon-only is ambiguous for Reply vs Reply All.
