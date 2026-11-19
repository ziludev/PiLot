#!/bin/sh
# Renders the pictures the build embeds, from the one master that is the mark
# (assets/brand/PiLot_fox.jpg, 2048 square):
#   resources/icon.png          1024 squircle tile, what mygo turns into AppIcon.icns
#   internal/app/avatar.png     128 head, drawn beside every answer
#
# Both are cut out of the same square with the same numbers, so the Dock icon and
# the face in the transcript keep the proportions the master was drawn with. The
# squircle is generated rather than copied from the outgoing icon: it is the shape
# macOS asks for, and a checked-in mask would only drift from the art.
set -e
cd "$(dirname "$0")/.."
exec /usr/bin/python3 - "$@" <<'PY'
from PIL import Image
import numpy as np

MASTER = "../assets/brand/PiLot_fox.jpg"
TILE = 1024
# The tile crops down to the body's flat bottom edge (the master leaves navy
# below it) so the fox is cut by the icon's edge instead of floating above it.
TILE_BOX = (174, 0, 1974, 1800)
# The avatar is the whole head: ears in frame, muzzle at the bottom, centred.
HEAD_BOX = (294, 230, 1854, 1790)
EXPONENT = 5.0  # macOS's corner is a superellipse; 5 is close enough at 1024


def tile(src):
    art = src.crop(TILE_BOX).resize((TILE, TILE), Image.LANCZOS).convert("RGBA")
    u = np.abs(np.linspace(-1, 1, TILE))[:, None]
    v = np.abs(np.linspace(-1, 1, TILE))[None, :]
    inside = (u**EXPONENT + v**EXPONENT) <= 1.0
    arr = np.array(art, dtype=np.uint8)
    arr[:, :, 3] = np.where(inside, 255, 0)
    return Image.fromarray(arr)


src = Image.open(MASTER).convert("RGB")
tile(src).save("resources/icon.png", optimize=True)
src.crop(HEAD_BOX).resize((128, 128), Image.LANCZOS).save("internal/app/avatar.png")
PY
