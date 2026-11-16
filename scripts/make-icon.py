"""Draws resources/icon.png: Pitta's mark, the same amber face the rail shows.

Run it after changing the palette: python3 scripts/make-icon.py
"""
import math

from PIL import Image, ImageDraw

SIZE = 1024
SS = 4  # supersample, then shrink: the only way to get clean edges with PIL
W = SIZE * SS


def rounded(draw, box, radius, fill):
    draw.rounded_rectangle([box[0] * SS, box[1] * SS, box[2] * SS, box[3] * SS],
                           radius=radius * SS, fill=fill)


def main():
    img = Image.new("RGBA", (W, W), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    # The tile: an amber gradient inside a macOS-sized rounded square.
    top, bottom = (244, 186, 94), (226, 141, 46)  # #F4BA5E -> #E28D2E
    radius = 228
    mask = Image.new("L", (W, W), 0)
    ImageDraw.Draw(mask).rounded_rectangle([0, 0, W - 1, W - 1], radius=radius * SS, fill=255)
    gradient = Image.new("RGBA", (W, W))
    grad = ImageDraw.Draw(gradient)
    for y in range(W):
        t = y / (W - 1)
        grad.line([(0, y), (W, y)], fill=tuple(
            int(top[i] + (bottom[i] - top[i]) * t) for i in range(3)) + (255,))
    img.paste(gradient, (0, 0), mask)
    draw = ImageDraw.Draw(img)

    ink = (42, 26, 12, 255)  # #2A1A0C: the eyes' colour in the app avatar
    # Two eyes and a smile: the agent's face, readable down to 16 DIP.
    for cx in (344, 680):
        rounded(draw, (cx - 56, 300, cx + 56, 536), 56, ink)

    # The smile is a filled band between two arcs, with round caps: PIL's arc
    # has square ends and its stroke inset puts knobs on the caps.
    cx, cy, rx, ry, thick = 512, 600, 182, 120, 58
    angles = [20 + i * (140 / 48) for i in range(49)]
    outer = [(cx + (rx + thick / 2) * math.cos(math.radians(a)),
              cy + (ry + thick / 2) * math.sin(math.radians(a))) for a in angles]
    inner = [(cx + (rx - thick / 2) * math.cos(math.radians(a)),
              cy + (ry - thick / 2) * math.sin(math.radians(a))) for a in reversed(angles)]
    draw.polygon([(x * SS, y * SS) for x, y in outer + inner], fill=ink)
    for a in (angles[0], angles[-1]):
        x = cx + rx * math.cos(math.radians(a))
        y = cy + ry * math.sin(math.radians(a))
        draw.ellipse([(x - thick / 2) * SS, (y - thick / 2) * SS,
                      (x + thick / 2) * SS, (y + thick / 2) * SS], fill=ink)

    img.resize((SIZE, SIZE), Image.LANCZOS).save("resources/icon.png")


if __name__ == "__main__":
    main()
