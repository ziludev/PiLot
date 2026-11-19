# PiLot brand

**The mark is the fox.** `PiLot_fox.jpg` (2048², navy tile and the head) is what the app
ships as its icon and as the face in the transcript; it is a raster, so `client-go/scripts/render-icon.sh`
cuts both from it. Everything below is the retired vector kit, kept because the wordmark and the
web icons have no raster equivalent.

The vector masters, as they came from the brand kit (`README_vector_assets.txt` is the kit's own
inventory). Nothing here is drawn by hand at build time; the app copies from these files.

Palette: blue `#1069EB`, coral `#FD713C`, purple `#8654FC`, yellow `#FEAE2C`; the icon tile and the
`pi` stem are navy `#182235`. The wordmark is outlined as paths, so no font ships with it.

| File | What it is |
| --- | --- |
| `PiLot_brand_master.svg` | Full-colour horizontal lockup (symbol + wordmark) |
| `PiLot_symbol_master.svg` | The symbol alone, five modules and the arrow |
| `PiLot_app_icon.svg` | Rounded navy tile with the symbol, 1024 |
| `PiLot_monochrome_white.svg` / `_black.svg` | One-colour lockup, the arrow cut out of the symbol with a mask |
| `PiLot_monochrome_app_icon.svg` | One-colour app icon |
| `favicon.svg`, `favicon.ico`, `favicon-16/32/48.png`, `apple-touch-icon.png`, `android-chrome-192/512.png` | Web icons, for a site or a docs page |

## What the app uses, and how it is derived

The fox is the icon, and the wordmark is still this kit's lettering — the one place both appear
together is the README lockup.

| Output | From | Script |
| --- | --- | --- |
| `client-go/resources/icon.png` (1024) | `PiLot_fox.jpg` — head + tile, squircle generated | `client-go/scripts/render-icon.sh` |
| `client-go/internal/app/avatar.png` (128) | the same master, head only | `client-go/scripts/render-icon.sh` |
| `assets/readme/logo.png`, `logo-dark.png` | the icon above + the wordmark out of `PiLot_brand_master.svg` / `PiLot_monochrome_white.svg` | `scripts/render-brand` |

Both scripts cut from the master rather than copying a checked-in mask: a stored `.icns` or a stored
squircle alpha would only drift from the art.

Nothing in `client-go/resources/` is a stale copy of the vector kit any more — the SVGs and the
32×32 template `tray.png` were removed with the old monochrome mark, because nothing read them and
`go tool mygo build` copies that directory into the bundle wholesale.
