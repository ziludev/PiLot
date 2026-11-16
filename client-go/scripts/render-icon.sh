#!/bin/sh
# Renders resources/icon.png (1024x1024) from the logo pitta-gpui ships.
# qlmanage is macOS's SVG renderer; sips keeps the PNG at 1024 with alpha.
set -e
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
qlmanage -t -s 1024 -o "$tmp" resources/pitta-logo-dark.svg >/dev/null 2>&1
sips -s format png "$tmp/pitta-logo-dark.svg.png" --out resources/icon.png >/dev/null
rm -rf "$tmp"
