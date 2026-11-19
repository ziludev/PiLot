#!/usr/bin/env python3
"""Generate 6 PiLot fennec-fox IP candidates as flat SVG (3 semantic colors).

Palette: fox orange #F28A55, soft cream #F4E7D2, midnight navy #202D40 background.
Depth is a single continuous, barely-there gradient masked to the whole fur
silhouette, so overlapping shapes never show a seam.
"""
import math
import os

ORANGE = "#F28A55"
ORANGE_HI = "#F5A070"  # same family, only the barely-there depth
CREAM = "#F4E7D2"
NAVY = "#202D40"
F = ORANGE  # flat fur fill: overlapping shapes never show a seam

OUT = os.path.dirname(os.path.abspath(__file__))

# ---------------- shared geometry (internal coords, placement applied later) ----------------

# Tall broad fennec ear: wide base, thick mass, blunt rounded tip (no sharp point).
EAR_C = ("M -128 96 Q -122 -70 -78 -392 C -56 -520 56 -520 78 -392 Q 122 -70 128 96 Q 0 142 -128 96 Z")

EAR = ("M -172 90 C -186 -90 -140 -300 -76 -420 "
       "C -44 -480 44 -480 76 -420 "
       "C 140 -300 186 -90 172 90 "
       "Q 0 132 -172 90 Z")

# Fox head: wide cheeks, softly tapered rounded chin.
HEAD = ("M 700 760 C 830 760 940 838 958 950 C 968 1030 940 1140 866 1212 "
        "C 812 1264 756 1292 700 1292 C 644 1292 588 1264 534 1212 "
        "C 460 1140 432 1030 442 950 C 460 838 570 760 700 760 Z")

# Squarer head for direction C: flat crown spanning the two upright ears.
HEAD_C = ("M 700 816 L 840 816 C 872 816 896 836 902 868 C 930 960 916 1074 856 1152 "
          "C 806 1228 754 1276 700 1276 C 646 1276 594 1228 544 1152 "
          "C 484 1074 470 960 498 868 C 504 836 528 816 560 816 Z")

SNOUT = ("M 612 1086 C 662 1068 738 1068 788 1086 "
         "C 786 1150 756 1196 700 1224 C 644 1196 614 1150 612 1086 Z")


def ear(x, y, angle, sx=1.0, sy=1.0, inner=True, inner_scale=0.60, inner_dy=-50, path=None):
    p = path or EAR
    """Return (fur_paths, cream_paths) for one ear."""
    fur = [f'<g transform="translate({x} {y}) rotate({angle}) scale({sx} {sy})">'
           f'<path d="{EAR}" fill="{F}"/></g>']
    cream = []
    if inner:
        cream.append(f'<g transform="translate({x} {y}) rotate({angle}) scale({sx} {sy})">'
                     f'<g transform="translate(0 {inner_dy}) scale({inner_scale})">'
                     f'<path d="{EAR}" fill="{CREAM}"/></g></g>')
    return fur, cream


def eyes(lx=628, rx=772, y=1022, r=44):
    return [f'<circle cx="{lx}" cy="{y}" r="{r}" fill="{NAVY}"/>',
            f'<circle cx="{rx}" cy="{y}" r="{r}" fill="{NAVY}"/>']


def snout(dx=0, dy=0, k=1.0):
    return [f'<g transform="translate({700 + dx} {1120 + dy}) scale({k}) '
            f'translate(-700 -1120)"><path d="{SNOUT}" fill="{CREAM}"/></g>']


def nose(cx=700, cy=1096, r=17):
    return [f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="{NAVY}"/>']


def body(cx=700, cy=1424, rx=182, ry=210):
    return [f'<ellipse cx="{cx}" cy="{cy}" rx="{rx}" ry="{ry}" fill="{F}"/>']


def pie_tail(cx, cy, r, a0, a1):
    """Rounded tail with a single cream pie-slice wedge and no extra division lines."""
    p0 = (cx + r * math.cos(math.radians(a0)), cy + r * math.sin(math.radians(a0)))
    p1 = (cx + r * math.cos(math.radians(a1)), cy + r * math.sin(math.radians(a1)))
    wedge = (f'M {cx:.1f} {cy:.1f} L {p0[0]:.1f} {p0[1]:.1f} '
             f'A {r} {r} 0 0 1 {p1[0]:.1f} {p1[1]:.1f} Z')
    fur = [f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="{F}"/>']
    cream = [f'<path d="{wedge}" fill="{CREAM}"/>']
    return fur, cream


def blob(cx, cy, rx, ry):
    return [f'<ellipse cx="{cx}" cy="{cy}" rx="{rx}" ry="{ry}" fill="{F}"/>']


def doc(fur, marks, tx=-430, ty=-519, scale=1.42, tilt=None, pivot="700 1200"):
    g = f'<g transform="translate({tx} {ty}) scale({scale})">'
    if tilt:
        g += f'<g transform="rotate({tilt} {pivot})">'
    g += "".join(fur) + "".join(marks)
    if tilt:
        g += "</g>"
    g += "</g>"
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1536 1536" width="1536" height="1536">
<rect width="1536" height="1536" fill="{NAVY}"/>
{g}
</svg>
'''


def merge(*parts):
    """Flatten (fur, cream) tuples and plain lists into (fur, cream)."""
    fur, cream = [], []
    for part in parts:
        if isinstance(part, tuple):
            fur += part[0]
            cream += part[1]
        else:
            cream += part  # eyes / snout / nose are non-fur marks
    return fur, cream


# ---------------- Direction A: The Big-Eared Navigator ----------------
A1 = merge(body(),
           ear(592, 906, -26), ear(808, 906, 26),
           [f'<path d="{HEAD}" fill="{F}"/>'],
           snout(), eyes(), nose())

A2 = merge(body(716, 1430, 176, 204),
           ear(584, 900, -27, 1.0, 1.0), ear(826, 884, 16, 0.99, 1.01),
           [f'<path d="{HEAD}" fill="{F}"/>'],
           snout(16, 10), eyes(650, 782, 1008, 43), nose(716, 1104))

# ---------------- Direction B: The Pie-Tail Explorer ----------------
B1 = merge(pie_tail(1004, 1300, 238, -96, -30),
           body(676, 1420, 176, 206),
           ear(586, 908, -25), ear(806, 902, 27),
           [f'<path d="{HEAD}" fill="{F}"/>'],
           snout(-12, 10), eyes(618, 752, 1030), nose(688, 1106))

B2 = merge(body(730, 1442, 176, 206),
           pie_tail(996, 1430, 226, -64, -2),
           ear(636, 894, -22), ear(872, 908, 31),
           [f'<path d="{HEAD}" fill="{F}"/>'],
           snout(28, 12), eyes(658, 794, 1028), nose(728, 1108))

# ---------------- Direction C: The Pi-Inspired Fox ----------------
# Two parallel upright ears of equal height plus a flat crown: the outer contour reads as a gate.
C1 = merge(body(700, 1424, 174, 206),
           ear(628, 916, -6, 0.80, 0.98, inner_scale=0.58, inner_dy=-138), ear(772, 916, 6, 0.80, 0.98, inner_scale=0.58, inner_dy=-138),
           [f'<path d="{HEAD_C}" fill="{F}"/>'],
           snout(), eyes(624, 776, 1022, 42), nose())

C2 = merge(body(724, 1428, 172, 204),
           ear(628, 916, -5, 0.74, 1.04, inner_scale=0.54, inner_dy=-150), ear(766, 918, 6, 0.75, 1.06, inner_scale=0.54, inner_dy=-156),
           [f'<path d="{HEAD_C}" fill="{F}"/>'],
           snout(14, 8), eyes(634, 790, 1018, 41), nose(714, 1104))

files = {
    "A1.svg": doc(*A1),
    "A2.svg": doc(*A2, tx=-336, tilt=-4),
    "B1.svg": doc(*B1),
    "B2.svg": doc(*B2, tilt=5),
    "C1.svg": doc(*C1, ty=-471),
    "C2.svg": doc(*C2, ty=-471, tilt=-5),
}

for name, content in files.items():
    with open(os.path.join(OUT, name), "w") as fh:
        fh.write(content)
print("wrote", ", ".join(files))
