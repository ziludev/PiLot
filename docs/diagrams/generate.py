#!/usr/bin/env python3
"""Generate the README diagrams as self-contained HTML with inline SVG.

Tokens match docs/diagrams (paper #F7F7F4, ink #182235, muted #46536B,
accent #1069EB). Node heights come from bh() so text can never sit on a
border. Render with headless Chrome at --force-device-scale-factor=2.
"""
import pathlib

INK = "#182235"
MUTED = "#46536B"
SOFT = "#5C6B85"
ACCENT = "#1069EB"
PAPER = "#F7F7F4"

MONO = "Geist Mono, ui-monospace, monospace"
SANS = "Geist, system-ui, sans-serif"
SERIF = "Instrument Serif, Georgia, serif"


def bh(tag, lines):
    """Box height for a tag chip, a name, and n mono lines."""
    return (58 if tag else 34) + 22 + 19 * lines + 12


def head(title, w):
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>{title} · PiLot</title>
<link href="https://fonts.googleapis.com/css2?family=Instrument+Serif:ital@0;1&family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500&display=swap" rel="stylesheet">
<style>
  *,*::before,*::after {{ box-sizing: border-box; margin: 0; padding: 0; }}
  html, body {{ background: {PAPER}; }}
  body {{ width: {w}px; height: {{H}}px; overflow: hidden; }}
  svg {{ display: block; }}
</style>
</head>
<body>
<svg width="{w}" height="{{H}}" viewBox="0 0 {w} {{H}}" xmlns="http://www.w3.org/2000/svg">
<defs>
  <marker id="ar" markerWidth="9" markerHeight="7" refX="8" refY="3.5" orient="auto">
    <polygon points="0 0, 9 3.5, 0 7" fill="{MUTED}"/>
  </marker>
  <marker id="ar-a" markerWidth="9" markerHeight="7" refX="8" refY="3.5" orient="auto">
    <polygon points="0 0, 9 3.5, 0 7" fill="{ACCENT}"/>
  </marker>
</defs>
<rect width="{w}" height="{{H}}" fill="{PAPER}"/>
"""


FOOT = "</svg>\n</body>\n</html>\n"


def eyebrow(x, y, text):
    return (f'<text x="{x}" y="{y}" fill="{SOFT}" font-size="10" font-family="{MONO}" '
            f'letter-spacing="2.6">{text}</text>\n')


def title(x, y, text, size=33):
    return (f'<text x="{x}" y="{y}" fill="{INK}" font-size="{size}" font-family="{SERIF}" '
            f'letter-spacing="-0.4">{text}</text>\n')


def sub(x, y, text):
    return (f'<text x="{x}" y="{y}" fill="{MUTED}" font-size="13" font-family="{SANS}">{text}</text>\n')


def box(x, y, w, tag="", name="", lines=(), accent=False, dashed=False, radius=7,
        fill=None, name_size=16, mono_size=11.5):
    h = bh(tag, len(lines))
    stroke = ACCENT if accent else SOFT
    if fill is None:
        fill = "rgba(16,105,235,0.07)" if accent else "#FFFFFF"
    dash = ' stroke-dasharray="5 4"' if dashed else ""
    s = (f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{radius}" fill="{fill}" '
         f'stroke="{stroke}" stroke-width="{1.5 if accent else 1}"{dash}/>\n')
    if tag:
        tw = len(tag) * 6.4 + 14
        s += (f'<rect x="{x+16}" y="{y+14}" width="{tw:.0f}" height="15" rx="2" fill="none" '
              f'stroke="{stroke}" stroke-opacity="0.45" stroke-width="0.9"/>\n')
        s += (f'<text x="{x+16+tw/2:.0f}" y="{y+25.5}" fill="{stroke}" font-size="8" '
              f'font-family="{MONO}" text-anchor="middle" letter-spacing="1">{tag}</text>\n')
        ny = y + 58
    else:
        ny = y + 34
    if name:
        s += (f'<text x="{x+16}" y="{ny}" fill="{INK}" font-size="{name_size}" '
              f'font-family="{SANS}" font-weight="600">{name}</text>\n')
        ny += 22
    for ln in lines:
        s += (f'<text x="{x+16}" y="{ny}" fill="{MUTED}" font-size="{mono_size}" '
              f'font-family="{MONO}">{ln}</text>\n')
        ny += 19
    return s, h


def arrow(x1, y1, x2, y2, marker="ar", color=None):
    c = color or MUTED
    return (f'<line x1="{x1}" y1="{y1}" x2="{x2}" y2="{y2}" stroke="{c}" '
            f'stroke-width="1.3" marker-end="url(#{marker})"/>\n')


def elbow(points, marker="ar", color=None):
    c = color or MUTED
    d = f"M {points[0][0]},{points[0][1]} " + " ".join(f"L {x},{y}" for (x, y) in points[1:])
    return (f'<path d="{d}" fill="none" stroke="{c}" stroke-width="1.3" '
            f'stroke-linejoin="round" marker-end="url(#{marker})"/>\n')


def note(x, y, label, lines):
    s = (f'<text x="{x}" y="{y}" fill="{SOFT}" font-size="8.5" font-family="{MONO}" '
         f'letter-spacing="2">{label}</text>\n')
    yy = y + 22
    for ln in lines:
        s += (f'<text x="{x}" y="{yy}" fill="{MUTED}" font-size="12" font-family="{SANS}">{ln}</text>\n')
        yy += 18
    return s


# ---------------------------------------------------------------- architecture
def architecture():
    w = 1240
    s = head("Architecture", w)

    body = eyebrow(60, 48, "ARCHITECTURE")
    body += title(60, 88, "Three surfaces, three processes, one SQLite file")
    body += sub(60, 116, "One conversation seen from three places. Every model request "
                         "and tool call writes its checkpoint before it runs.")
    y = 156
    h1 = bh("SURFACE", 1)
    for i, (tag, name, lines) in enumerate([
        ("SURFACE", "Feishu app", ["custom app · long connection"]),
        ("SURFACE", "Desktop app", ["Go + mygo/ui · Metal, no webview"]),
        ("SURFACE", "CLI", ["durable ask | chat | doctor"]),
    ]):
        b, _ = box(60 + i * 290, y, 260, tag, name, lines)
        body += b
    y += h1 + 38

    hg = bh("PROCESS · GO", 2)
    body += arrow(190, y - 38, 190, y - 4)
    b, _ = box(60, y, 260, "PROCESS · GO", "Feishu gateway",
               ["own launchd job · :8788", "no public IP, domain or cert"],
               fill="rgba(70,83,107,0.05)")
    body += b
    y += hg + 38

    body += arrow(190, y - 38, 190, y - 4)
    body += arrow(480, 156 + h1, 480, y - 4)
    body += arrow(770, 156 + h1, 770, y - 4)
    hb = bh("PROCESS · NODE", 2)
    b, _ = box(60, y, 860, "PROCESS · NODE", "Bridge",
               ["127.0.0.1:8787 · HTTP + WS, local only",
                "one writer · one conversation · every surface reads the same state"])
    body += b
    y += hb + 38

    body += arrow(190, y - 38, 190, y - 4)
    hh = bh("PROCESS · NODE", 2)
    b, _ = box(60, y, 280, "PROCESS · NODE", "Harness",
               ["one durable task per request,", "per tool call, per compaction"])
    body += b
    b, _ = box(400, y, 250, "GATE", "Guard",
               ["a dangerous call waits for", "your tap — it is not a sandbox"])
    body += b
    b, _ = box(710, y, 210, "PROCESS · GO", "Toolbox",
               ["bash · read · write · edit", "on a unix socket"])
    body += b
    mid = y + hh / 2
    body += arrow(340, mid, 396, mid)
    body += arrow(650, mid, 706, mid)
    y += hh + 38

    body += arrow(190, y - 38, 190, y - 4)
    body += arrow(800, y - 38, 800, y - 4)
    b, _ = box(60, y, 860, "STORE", "SQLite",
               [".data/agent.sqlite — conversations · memory · approvals · audit · checkpoints"],
               mono_size=11)
    body += b
    y += bh("STORE", 1) + 30

    body += note(960, 480, "AFTER A KILL -9", [
        "The turn does not start over.",
        "The next process reads the last",
        "checkpoint and carries on.",
    ])
    body += note(960, 596, "WHY A GATEWAY", [
        "A turn runs for minutes. The",
        "socket has to be free the",
        "moment the next event lands.",
    ])
    body += note(960, 712, "WHY A TOOLBOX", [
        "Coding tools live in Go, behind",
        "the names and result shapes the",
        "model already knew.",
    ])
    return "pilot-architecture.html", s, y, body


# ----------------------------------------------------------------------- turn
def turn():
    w = 1240
    s = head("One turn", w)

    body = eyebrow(60, 48, "ONE TURN")
    body += title(60, 88, "Message in, verdict out — and what a kill -9 costs")
    body += sub(60, 116, "Every step is a durable task: written down before it runs, so a "
                         "process that was not there at the start can finish the loop.")
    x, bw = 80, 380
    cx = x + bw / 2
    y = 160
    hm = bh("", 1)
    b, _ = box(x, y, bw, "", "Message arrives", ["Feishu · desktop · CLI"], radius=39)
    body += b
    y += hm + 40

    b, ht = box(x, y, bw, "DURABLE TASK", "Checkpoint, then run",
                ["the row is written before the call", "leaves the process"], accent=True)
    body += b
    body += arrow(cx, y - 40, cx, y - 4)
    y += ht + 40

    b, hmo = box(x, y, bw, "", "Model request",
                 ["pi-ai · this conversation's model", "and thinking level"])
    body += b
    body += arrow(cx, y - 40, cx, y - 4)
    y += hmo + 36

    hd = 58
    body += (f'<rect x="{cx-110}" y="{y}" width="220" height="{hd}" rx="29" fill="#FFFFFF" '
             f'stroke="{INK}" stroke-width="1.1"/>\n')
    body += (f'<text x="{cx}" y="{y+36}" fill="{INK}" font-size="15" font-family="{SANS}" '
             f'text-anchor="middle" font-weight="600">tool call?</text>\n')
    body += arrow(cx, y - 36, cx, y - 4)
    body += elbow([(cx + 110, y + hd / 2), (596, y + hd / 2)])
    body += (f'<rect x="{cx+128}" y="{y+hd/2-15}" width="30" height="16" rx="2" fill="{PAPER}"/>\n'
             f'<text x="{cx+143}" y="{y+hd/2-3}" fill="{MUTED}" font-size="9.5" font-family="{MONO}" '
             f'text-anchor="middle" letter-spacing="1">NO</text>\n')
    b, ha = box(596, y - hd / 2 - 14, 300, "", "Answer",
                ["the card says how it ended:", "Done · Goal met · Goal missed"])
    body += b
    y += hd + 38

    body += arrow(cx, y - 38, cx, y - 4)
    body += (f'<rect x="{cx+12}" y="{y-30}" width="34" height="16" rx="2" fill="{PAPER}"/>\n'
             f'<text x="{cx+29}" y="{y-18}" fill="{MUTED}" font-size="9.5" font-family="{MONO}" '
             f'text-anchor="middle" letter-spacing="1">YES</text>\n')
    b, hgu = box(x, y, bw, "GATE", "Is it dangerous?",
                 ["rm · force-push · write outside", "the workspace"])
    body += b
    gmid = y + hgu / 2
    body += elbow([(x + bw, gmid), (596, gmid)])
    b, hap = box(596, gmid - bh("", 3) / 2, 300, "", "Approval card in your DM",
                 ["only the owner's open_ids can tap", "nobody taps in 10 min → denied",
                  "allow-all is kept per risk class"])
    body += b
    body += (f'<rect x="{x+bw+18}" y="{gmid-22}" width="96" height="16" rx="2" fill="{PAPER}"/>\n'
             f'<text x="{x+bw+66}" y="{gmid-10}" fill="{MUTED}" font-size="8.5" font-family="{MONO}" '
             f'text-anchor="middle" letter-spacing="0.9">IF DANGEROUS</text>\n')
    y += hgu + 38

    body += arrow(cx, y - 38, cx, y - 4)
    b, he = box(x, y, bw, "", "Execute in the toolbox",
                ["bash · read · write · edit · delegate"])
    body += b
    loop_out = y + he - 14
    body += elbow([(x, loop_out), (44, loop_out), (44, 160 + hm + 40 + ht + 40 + hmo / 2),
                   (x, 160 + hm + 40 + ht + 40 + hmo / 2)], marker="ar-a", color=ACCENT)
    y += he + 34

    body += note(950, 300, "WHERE A CRASH LANDS", [
        "Kill the process mid-call and",
        "the next one marks that call",
        "interrupted, the model retries it,",
        "and the turn ends where it would",
        "have ended anyway.",
    ])
    body += note(950, 470, "WHY THE WAIT IS DRAWN", [
        "Approval is a step, not a modal.",
        "A twelve-second turn can be",
        "twelve seconds of waiting — the",
        "timeline says so instead of",
        "hiding it.",
    ])
    body += note(950, 720, "WHEN IT RUNS AWAY", [
        "240 tool calls stops the turn as",
        "Stopped at 240 steps, not as a",
        "failure: the transcript stays and",
        "a Continue picks it up.",
    ])
    return "pilot-turn.html", s, y, body


# ---------------------------------------------------------------------- trust
def trust():
    w = 1240
    s = head("Access", w)

    body = eyebrow(60, 48, "ACCESS")
    body += title(60, 88, "Who can talk, what they reach, what is asked first")
    body += sub(60, 116, "One level per chat, read by validation, the guest policy, the tool "
                         "surface and every command. The fence is code, not prompt text.")
    cols = [
        (60, "OWNER", "you", "FEISHU_ALLOWED_SENDER_IDS", [
            "Talks in any chat that passes",
            "validation. A dangerous call",
            "raises a card in your DM first.",
        ], ["every tool and MCP server", "all three memory layers"], True),
        (450, "GROUP", "anyone else", "the chat was opened with /allow", [
            "Answers only when @-mentioned,",
            "inside a topic — the main",
            "timeline stays clean.",
        ], ["read · skill · recall · remind", "project_search · logs_query · db_query",
            "this chat's memory only"], False),
        (840, "CLOSED", "nobody", "until you authorise the chat", [
            "No reply at all. You get one",
            "card with a single button, and",
            "that is the whole surface.",
        ], ["no tools", "no memory"], False),
    ]
    top, card_h = 160, 286
    for x, tag, name, when, bl, surface, accent in cols:
        stroke = ACCENT if accent else SOFT
        fill = ("rgba(16,105,235,0.06)" if accent
                else ("#FFFFFF" if x < 800 else "rgba(70,83,107,0.045)"))
        dash = "" if x < 800 else ' stroke-dasharray="5 4"'
        body += (f'<rect x="{x}" y="{top}" width="340" height="{card_h}" rx="9" fill="{fill}" '
                 f'stroke="{stroke}" stroke-width="{1.5 if accent else 1}"{dash}/>\n')
        body += (f'<text x="{x+24}" y="{top+32}" fill="{stroke}" font-size="9" font-family="{MONO}" '
                 f'letter-spacing="2.2">{tag}</text>\n')
        body += (f'<text x="{x+24}" y="{top+68}" fill="{INK}" font-size="22" font-family="{MONO}">{name}</text>\n')
        body += (f'<text x="{x+24}" y="{top+92}" fill="{MUTED}" font-size="9.5" font-family="{MONO}">{when}</text>\n')
        yy = top + 128
        for ln in bl:
            body += (f'<text x="{x+24}" y="{yy}" fill="{MUTED}" font-size="12" font-family="{SANS}">{ln}</text>\n')
            yy += 19
        body += (f'<line x1="{x+24}" y1="{yy+2}" x2="{x+316}" y2="{yy+2}" '
                 f'stroke="rgba(24,34,53,0.12)" stroke-width="0.9"/>\n')
        yy += 26
        for ln in surface:
            body += (f'<text x="{x+24}" y="{yy}" fill="{INK}" font-size="11" font-family="{MONO}">{ln}</text>\n')
            yy += 18

    y = top + card_h + 40
    body += (f'<line x1="60" y1="{y}" x2="1180" y2="{y}" stroke="rgba(24,34,53,0.12)" '
             f'stroke-width="0.9"/>\n')
    y += 34
    body += eyebrow(60, y, "THE APPROVAL PATH")
    y += 24

    hp = bh("", 1)
    l1, _ = box(60, y, 236, "", "A call looks dangerous", ["rm -rf · force-push · chmod"], mono_size=10.5)
    l2, _ = box(330, y, 200, "GATE", "Guard asks", ["beforeTool, in code"])
    l3, _ = box(570, y, 240, "", "Card in your DM", ["not in the group"])
    body += l1 + l2 + l3
    mid = y + hp / 2
    body += arrow(296, mid, 326, mid)
    body += arrow(530, mid, 566, mid)

    hh = bh("", 1) - 20
    body += (f'<rect x="850" y="{y}" width="200" height="{hh}" rx="7" fill="rgba(16,105,235,0.07)" '
             f'stroke="{ACCENT}" stroke-width="1.5"/>\n')
    body += (f'<text x="866" y="{y+32}" fill="{INK}" font-size="15" font-family="{SANS}" '
             f'font-weight="600">allow</text>\n')
    body += (f'<text x="866" y="{y+53}" fill="{MUTED}" font-size="10.5" font-family="{MONO}">'
             f'runs, and is counted</text>\n')
    y2 = y + hh + 14
    body += (f'<rect x="850" y="{y2}" width="200" height="{hh}" rx="7" fill="#FFFFFF" '
             f'stroke="{SOFT}" stroke-width="1"/>\n')
    body += (f'<text x="866" y="{y2+32}" fill="{INK}" font-size="15" font-family="{SANS}" '
             f'font-weight="600">deny / 10 min</text>\n')
    body += (f'<text x="866" y="{y2+53}" fill="{MUTED}" font-size="10.5" font-family="{MONO}">'
             f'same callback, same answer</text>\n')
    # split from the card into allow and deny
    body += elbow([(810, mid), (830, mid), (830, y + hh / 2), (846, y + hh / 2)])
    body += elbow([(810, mid), (830, mid), (830, y2 + hh / 2), (846, y2 + hh / 2)])

    body += note(1080, y - 4, "EVERY DECISION", [
        "one line in the",
        "app.audit ring (300),",
        "read with",
        "durable audit",
    ])
    y = y2 + hh + 30
    return "pilot-access.html", s, y, body


if __name__ == "__main__":
    out = pathlib.Path(__file__).parent
    for fn in (architecture, turn, trust):
        name, shell, h, body = fn()
        # inject the body just before </svg>, and size the svg to the computed height
        html = (shell + body + FOOT).replace("{H}", str(round(h)))
        (out / name).write_text(html)
        print("wrote", name, "h=", round(h))
