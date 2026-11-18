# PiLot brand

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

`client-go/resources/` holds copies because `go:embed` and `go tool mygo build` read only from the
module:

| App file | From | Note |
| --- | --- | --- |
| `pilot-app-icon.svg` | `PiLot_app_icon.svg` | rendered to `icon.png` (1024) by `client-go/scripts/render-icon.sh`, which mygo turns into `AppIcon.icns` |
| `pilot-logo.svg` | `PiLot_brand_master.svg` | |
| `pilot-logo-dark.svg` | `PiLot_monochrome_white.svg` | |
| `pilot-mark.svg` | `PiLot_symbol_master.svg` | symbol only, recoloured to a single ink, viewBox squared for the rail |
| `tray.png` | `pilot-mark.svg` | 32×32 template icon, rendered by the same script |

The `.icns` and the `PiLot.iconset/` in the kit are not stored: the build regenerates the icon from
the vector, and a checked-in `.icns` would only drift from it.
