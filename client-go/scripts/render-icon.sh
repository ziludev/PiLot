#!/bin/sh
# Renders the icons the build embeds, from the vector master in resources/:
#   resources/icon.png  (1024x1024) from resources/pilot-app-icon.svg
#   resources/tray.png  (32x32, ink black, transparent) from resources/pilot-mark.svg
# Chrome renders the SVGs with a transparent background; without the flag its
# white page would fill the rounded corners of the tile and the arrow's cutout.
set -e
cd "$(dirname "$0")/.."
chrome=${CHROME:-"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

shoot() { # shoot <svg> <size> <out>
	"$chrome" --headless --disable-gpu --hide-scrollbars \
		--default-background-color=00000000 --window-size="$2,$2" \
		--screenshot="$tmp/shot.png" "file://$PWD/$1" >/dev/null 2>&1
	sips -s format png --out "$3" "$tmp/shot.png" >/dev/null
}

shoot resources/pilot-app-icon.svg 1024 resources/icon.png

# The bar draws the mark at 16 points, so the tray gets the mark trimmed to its
# ink and centred in 32 px with a little air around it.
shoot resources/pilot-mark.svg 350 "$tmp/mark.png"
python3 - "$tmp/mark.png" resources/tray.png <<'PY'
import sys
from PIL import Image

mark = Image.open(sys.argv[1]).convert("RGBA")
mark = mark.crop(mark.getbbox())
side = 28
scale = min(side / mark.width, side / mark.height)
mark = mark.resize((max(1, round(mark.width * scale)), max(1, round(mark.height * scale))), Image.LANCZOS)
tray = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
tray.alpha_composite(mark, ((32 - mark.width) // 2, (32 - mark.height) // 2))
tray.save(sys.argv[2])
PY
