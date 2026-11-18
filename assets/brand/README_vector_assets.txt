PiLot Vector Brand Assets
========================

Vector masters (editable paths; no embedded raster images):
- PiLot_brand_master.svg           Full-color horizontal brand lockup
- PiLot_symbol_master.svg          Standalone symbol
- PiLot_app_icon.svg               Rounded-square app icon
- PiLot_monochrome_black.svg       Black logo for light backgrounds
- PiLot_monochrome_white.svg       White logo for dark backgrounds
- PiLot_monochrome_app_icon.svg    Monochrome app icon
- favicon.svg                      Scalable web favicon

macOS:
- PiLot_macos.icns                 ICNS with 16/32/64/128/256/512/1024 PNG-backed representations
- PiLot.iconset/                   Apple iconutil-ready iconset source directory

Web:
- favicon.ico                     Multi-size ICO (16, 32, 48, 64, 128, 256)
- favicon-16x16.png, favicon-32x32.png, favicon-48x48.png
- apple-touch-icon.png             180x180
- android-chrome-192x192.png, android-chrome-512x512.png

HTML usage:
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<link rel="icon" href="/favicon.ico" sizes="any">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">

Notes:
- SVGs are actual vector paths reconstructed from the selected raster reference.
- The source artwork uses the following flat palette: blue #1069EB, coral #FD713C, purple #8654FC, yellow #FEAE2C, navy #182235.
- The wordmark is outlined as paths so no external font is required.
- For best app icon results, use the ICNS file in Xcode/macOS packaging.
- On macOS, if desired, rebuild the icon with: iconutil -c icns PiLot.iconset
