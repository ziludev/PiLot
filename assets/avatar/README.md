# Agent avatar

A [open-bot-avatar](https://github.com/DominikScholz/open-bot-avatar) definition: one silhouette plus
where the eyes sit, and that renderer gives it a soft volume. Ours is the **pie** — the π / pie pun:
a round crimped pie seen from above, two pill eyes on it.

| File | What it is |
| --- | --- |
| `pie.json` | The definition (v1, `validateAvatar` clean, 385 characters of path). Paste it into the [editor](https://dominikscholz.github.io/open-bot-avatar/) to recolor, turn, animate or export. |
| `pie-amber.svg` / `.png` | Amber. The default. |
| `pie-berry.svg` / `.png` | Berry. |
| `pie-teal.svg` / `.png` | Teal. |
| `pie-ink.svg` / `.png` | Ink body with mint eyes, for light surfaces that want weight. |
| `pie-idle.gif` | One state on a loop, 1.6 s, 26 frames at 16 fps, 256 px. |
| `pie-thinking.gif` | Same loop length, eyes shifted — use while a job runs. |
| `pie-happy.gif` | Same loop length, happy arcs — use when it finished. |
| `pie-states.gif` | All three in one loop (3 s, 48 frames), for a single place that has only one slot. |
| `pie-state-idle.gif` | The run-state set, drawn here rather than exported from the renderer: idle — breathing, one blink per loop. |
| `pie-state-working.gif` | **A flame on the crust**: a turn is running. Two out-of-step waves drive the flicker, so it never looks like a metronome. |
| `pie-state-done.gif` | Happy arcs and one warm beat — the turn settled. |
| `pie-state-stalled.gif` | The flame dies into smoke and the eyes go flat — nothing is coming. |
| `pie-states-all.gif` | The four in one loop (6.5 s, 104 frames, 192 px). |

The crimped rim is what makes it a pie rather than a cookie or a blob, and it is the one cue worth
keeping: the outline carries the recognition, since the renderer paints a single flat colour.

The PNGs are 512×512 with a transparent background, which is what the Feishu console wants for 头像
（开发者后台 → 应用信息 → 头像). Transparent on purpose: Feishu draws avatars on light and dark chrome
alike, and a filled square just becomes a second frame.

The GIFs render the renderer's three moods frame by frame with `time` and `motion`; the combined one
blends over the first 0.4 s of each switch. Frames share one palette so a loop does not shimmer. GIF transparency is a single bit,
so the frames are composited on white: an anti-aliased edge would fringe against any other colour.

The `pie-state-*.gif` set answers a different question than the renderer's three moods: what is the
agent *doing*, at a glance, from a corner of a screen. Fire is the whole vocabulary — lit means a turn
is live, out means it is not — so the pie itself never has to change shape. `states.html` is the
source (one page, the state and frame in the URL hash) and `scripts/render-avatar-states` renders it
frame by frame with headless Chrome; the animation is computed from the frame number, so 26 separate
launches still make one seamless loop.

The desktop client draws the same definition from Go: `client-go/internal/avatar` holds a copy of the
geometry as an SVG and renders it with mygo/ui — thinking while a turn or a task runs, happy when it
stops, idle otherwise. Two copies of the geometry is one too many, but the client cannot read this
folder at build time; if the shape changes, change both.

`prompt.md` is the prompt this was designed with — the schema, the numeric limits and the craft rules,
self-contained, so another model can produce more candidates.

Tried and dropped: a stroke-drawn π on a dark square (a logo, not a character), the π as a solid slab
with a face on the brim (a crate), a pie with one slice lifted out (read as a cookie with a bite), and
a pie chart (read as an icon rather than this character).
